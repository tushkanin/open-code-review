// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package diff

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alibaba/open-code-review/internal/model"
)

const evidenceCaptureLimit = 256 << 20

type evidenceEntry struct {
	object      string
	mode        string
	content     string
	captured    bool
	unavailable string
	searchClass string
	attributes  map[string]evidenceAttribute
}

// Evidence identifies source bytes without exposing mutable storage.
type Evidence struct {
	Path      string
	State     string
	Mode      string
	StartLine int
	EndLine   int
	Digest    string
	Content   string
	Available bool
	Reason    string
}

// EvidenceView retains local overrides and immutable Git object identities.
// The provider constructs it before concurrent access starts.
type EvidenceView struct {
	provider    *Provider
	input       InputResolution
	entries     map[string]evidenceEntry
	baseObjects map[string]string
	untracked   []string
	state       string
	used        int
}

func validEvidencePath(name string) bool {
	return name != "" && name != "." && !strings.ContainsRune(name, 0) && !path.IsAbs(name) && !filepath.IsAbs(name) && filepath.VolumeName(name) == "" && path.Clean(name) == name && filepath.Clean(filepath.FromSlash(name)) == filepath.FromSlash(name) && name != ".." && !strings.HasPrefix(name, "../") && name != ".git" && !strings.HasPrefix(name, ".git/")
}

func (v *EvidenceView) Input() InputResolution { return v.input }

func (v *EvidenceView) State() string { return v.state }

func (v *EvidenceView) Paths(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(v.entries))
	for name := range v.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (v *EvidenceView) Read(ctx context.Context, name string, start, limit int) (Evidence, error) {
	result := Evidence{Path: name, State: v.state}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !validEvidencePath(name) {
		return result, fmt.Errorf("invalid repository path %q", name)
	}
	entry, ok := v.entries[name]
	if !ok {
		result.Reason = "path is absent from captured source"
		return result, nil
	}
	result.Mode = entry.mode
	if entry.unavailable != "" {
		result.Reason = entry.unavailable
		return result, nil
	}
	content := entry.content
	if !entry.captured {
		var err error
		content, err = v.readObject(ctx, entry.object)
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err != nil {
			result.Reason = "captured Git object is unavailable"
			return result, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.Digest = fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
	result.Available = true
	if start <= 0 {
		start = 1
	}
	if limit <= 0 {
		result.Content = content
		result.StartLine = 1
		result.EndLine = len(strings.Split(content, "\n"))
		return result, nil
	}
	lines := strings.Split(content, "\n")
	result.StartLine = start
	end := min(len(lines), start-1+limit)
	result.EndLine = end
	if start <= len(lines) {
		result.Content = strings.Join(lines[start-1:end], "\n")
	}
	return result, nil
}

func (v *EvidenceView) readObject(ctx context.Context, object string) (string, error) {
	size, err := v.provider.runGit(ctx, "cat-file", "-s", object)
	if err != nil {
		return "", err
	}
	var n int64
	if _, err := fmt.Sscan(size, &n); err != nil || n > maxUntrackedFileSize {
		return "", fmt.Errorf("evidence file exceeds size limit")
	}
	out, _, err := v.provider.runGitSplit(ctx, "cat-file", "blob", object)
	return out, err
}

func (p *Provider) newEvidence(ctx context.Context, input InputResolution, index bool) (*EvidenceView, error) {
	v := &EvidenceView{provider: p, input: input, entries: make(map[string]evidenceEntry), baseObjects: make(map[string]string)}
	ref := input.ResolvedHead
	if p.mode == ModeWorkspace {
		ref = input.ResolvedBase
	}
	if ref != "" {
		out, err := p.runGit(ctx, "ls-tree", "-rz", "--full-tree", ref)
		if err != nil {
			return nil, err
		}
		for _, record := range strings.Split(out, "\x00") {
			header, name, ok := strings.Cut(record, "\t")
			fields := strings.Fields(header)
			if !ok || len(fields) != 3 || !validEvidencePath(name) {
				continue
			}
			entry := evidenceEntry{mode: fields[0], object: fields[2]}
			if fields[1] != "blob" {
				entry.unavailable = "path is not a regular Git blob"
			}
			v.entries[name] = entry
			v.baseObjects[name] = fields[2]
		}
	}
	if p.mode == ModeWorkspace {
		args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--name-only", "-z"}
		if index {
			args = append(args, "--cached")
		}
		if input.ResolvedBase != "" {
			args = append(args, input.ResolvedBase)
		}
		args = append(args, "--")
		changed, err := p.runGit(ctx, args...)
		if err != nil {
			return nil, err
		}
		for _, name := range strings.Split(changed, "\x00") {
			if name == "" {
				continue
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if index {
				v.captureIndex(ctx, name)
			} else {
				v.captureDisk(name)
			}
		}
		untracked, err := p.untrackedFilesList(ctx)
		if err != nil {
			return nil, err
		}
		v.untracked = untracked
		for _, name := range untracked {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			v.captureDisk(name)
		}
	}
	if err := v.captureSearchAttributes(ctx); err != nil {
		return nil, err
	}
	return v, nil
}

func (v *EvidenceView) seal(ctx context.Context) error {
	names, err := v.Paths(ctx)
	if err != nil {
		return err
	}
	h := sha256.New()
	fmt.Fprintf(h, "%q%q", v.input.ResolvedBase, v.input.ResolvedHead)
	attributeNames := make([]string, 0)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		e := v.entries[name]
		fmt.Fprintf(h, "%q%q%q%q%q%d:", name, e.object, e.mode, e.unavailable, e.searchClass, len(e.content))
		_, _ = h.Write([]byte(e.content))
		fmt.Fprintf(h, "%d:", len(e.attributes))
		attributeNames = attributeNames[:0]
		for name := range e.attributes {
			attributeNames = append(attributeNames, name)
		}
		sort.Strings(attributeNames)
		for _, name := range attributeNames {
			attribute := e.attributes[name]
			fmt.Fprintf(h, "%q%q%q", name, attribute.state, attribute.value)
		}
	}
	v.state = fmt.Sprintf("%x", h.Sum(nil))
	return nil
}

func (v *EvidenceView) captureIndex(ctx context.Context, name string) {
	if !validEvidencePath(name) {
		return
	}
	out, err := v.provider.runGit(ctx, "ls-files", "--stage", "-z", "--", ":(literal)"+name)
	if err != nil {
		v.entries[name] = evidenceEntry{unavailable: "index entry is unavailable"}
		return
	}
	if out == "" {
		delete(v.entries, name)
		return
	}
	header, _, _ := strings.Cut(out, "\t")
	fields := strings.Fields(header)
	if len(fields) != 3 || fields[2] != "0" {
		v.entries[name] = evidenceEntry{unavailable: "index entry is unresolved"}
		return
	}
	e := evidenceEntry{object: fields[1], mode: fields[0]}
	if e.mode == "160000" {
		e.unavailable = "path is a submodule"
	}
	v.entries[name] = e
}

func (v *EvidenceView) captureDisk(name string) {
	if !validEvidencePath(name) {
		return
	}
	e := evidenceEntry{captured: true, mode: "100644"}
	content, err := readWorkspaceFileForDiffWithLimit(v.provider.repoDir, name, maxUntrackedFileSize)
	if os.IsNotExist(err) {
		delete(v.entries, name)
		return
	}
	if err != nil {
		e.unavailable = "local content is unavailable"
		if errors.Is(err, errWorkspaceFileTooLarge) {
			e.unavailable = "local content exceeds size limit"
		}
	} else if v.used+len(content) > evidenceCaptureLimit {
		e.unavailable = "evidence capture exceeds size limit"
	} else {
		e.content = string(content)
		v.used += len(content)
		if info, err := os.Lstat(filepath.Join(v.provider.repoDir, filepath.FromSlash(name))); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				e.mode = "120000"
			} else if info.Mode()&0111 != 0 {
				e.mode = "100755"
			}
		}
	}
	v.entries[name] = e
}

func (v *EvidenceView) fileContent(ctx context.Context, name string) (string, error) {
	e, err := v.Read(ctx, name, 1, 0)
	if err != nil {
		return "", err
	}
	if !e.Available {
		return "", fmt.Errorf("evidence unavailable: %s", e.Reason)
	}
	return e.Content, nil
}

func (v *EvidenceView) reconcile(ctx context.Context, diffs []model.Diff, raw string) {
	type expectedSource struct{ object, mode string }
	expected := make(map[string]expectedSource)
	name := ""
	inHunk := false
	for _, line := range splitDiffLines(raw) {
		if old, next, ok := parseDiffHeaderLine(line); ok {
			name = next
			expected[name] = expectedSource{object: v.baseObjects[old]}
			inHunk = false
			continue
		}
		if strings.HasPrefix(line, "@@") {
			inHunk = true
		}
		if name == "" || inHunk {
			continue
		}
		source := expected[name]
		switch {
		case strings.HasPrefix(line, "index "):
			fields := strings.Fields(line)
			if len(fields) > 1 {
				_, source.object, _ = strings.Cut(fields[1], "..")
			}
		case strings.HasPrefix(line, "new mode "):
			source.mode = strings.TrimPrefix(line, "new mode ")
		case strings.HasPrefix(line, "new file mode "):
			source.mode = strings.TrimPrefix(line, "new file mode ")
		}
		expected[name] = source
	}
	for i := range diffs {
		d := &diffs[i]
		if d.IsDeleted {
			delete(v.entries, d.OldPath)
			continue
		}
		if d.IsRenamed {
			delete(v.entries, d.OldPath)
		}
		name := d.NewPath
		entry, ok := v.entries[name]
		if !ok {
			v.entries[name] = evidenceEntry{unavailable: "reviewed content is absent"}
			continue
		}
		want := expected[name]
		mismatch := want.mode != "" && want.mode != entry.mode
		if want.object != "" && strings.Trim(want.object, "0") != "" {
			content, err := v.fileContent(ctx, name)
			if err == nil {
				got, err := v.provider.hashContent(ctx, content)
				mismatch = mismatch || err != nil || strings.TrimSpace(got) != want.object
			}
		}
		if mismatch {
			entry.unavailable = "captured content does not match reviewed diff"
			v.entries[name] = entry
			d.NewFileContent = ""
		}
	}
}
