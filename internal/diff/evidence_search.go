// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package diff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (p *Provider) Evidence() *EvidenceView { return p.evidence }

func (p *Provider) hashContent(ctx context.Context, content string) (string, error) {
	out := evidenceOutput{limit: 1024}
	err := p.runGitIO(ctx, p.repoDir, nil, strings.NewReader(content), &out, io.Discard, "hash-object", "--stdin")
	return out.String(), err
}

type EvidenceMatch struct {
	Path    string
	Line    int
	Content string
}
type EvidenceSearch struct {
	Matches     []EvidenceMatch
	Truncated   bool
	Unavailable []string
}

// Search uses Git's literal, PCRE, and pathspec semantics for both source layers.
// Only local overrides need temporary files; immutable base content stays in Git.
func (v *EvidenceView) Search(parent context.Context, text string, sensitive, perl bool, patterns []string, limit int) (EvidenceSearch, error) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	result := EvidenceSearch{}
	if limit <= 0 {
		return result, fmt.Errorf("search limit must be positive")
	}
	for _, pattern := range patterns {
		for _, part := range strings.Split(strings.ReplaceAll(pattern, "\\", "/"), "/") {
			if part == ".." {
				return result, fmt.Errorf("pathspec contains traversal")
			}
		}
	}
	includes, excludes, err := capturedPathspecs(patterns)
	if err != nil {
		return result, err
	}
	args := []string{"--no-pager", "-c", "core.quotepath=false", "grep", "-n", "-z", "--text", "--no-color", "--max-count", strconv.Itoa(limit + 1)}
	if !sensitive {
		args = append(args, "-i")
	}
	if perl {
		args = append(args, "-P")
	} else {
		args = append(args, "-F")
	}
	args = append(args, "-e", text)
	ref := v.input.ResolvedHead
	if v.provider.mode == ModeWorkspace {
		ref = v.input.ResolvedBase
	}
	classified := make(map[string]bool)
	searchable := make(map[string]bool)
	excluded := make(map[string]bool)
	type matchLocation struct {
		path string
		line int
	}
	seen := make(map[matchLocation]bool)
	resolve := func(name string, base bool) (string, evidenceEntry, bool) {
		if base {
			name = strings.TrimPrefix(name, ref+":")
		} else {
			name = strings.TrimPrefix(name, "./")
		}
		entry, ok := v.entries[name]
		if !ok || entry.unavailable != "" || (base && entry.captured) {
			return name, entry, false
		}
		if base && ref != v.input.ResolvedHead {
			object, ok := v.baseObjects[name]
			if !ok || object != entry.object {
				return name, entry, false
			}
		}
		return name, entry, true
	}
	collect := func(out string, base bool, spec evidencePathspec) {
		for out != "" {
			name, rest, ok := strings.Cut(out, "\x00")
			if !ok {
				break
			}
			number, rest, ok := strings.Cut(rest, "\x00")
			if !ok {
				break
			}
			content, next, _ := strings.Cut(rest, "\n")
			out = next
			name, e, visible := resolve(name, base)
			if !visible || excluded[name] || !spec.matches(e) {
				continue
			}
			if !classified[name] {
				classified[name] = true
				switch e.searchClass {
				case "binary":
				case "text":
					searchable[name] = true
				default:
					content := e.content
					var err error
					if !e.captured {
						prefix := evidenceOutput{limit: binarySniffWindow}
						err = v.provider.runGitIO(ctx, v.provider.repoDir, nil, nil, &prefix, io.Discard, "cat-file", "blob", e.object)
						content = prefix.String()
					}
					if err != nil {
						result.Unavailable = append(result.Unavailable, name)
					} else {
						searchable[name] = strings.IndexByte(content[:min(len(content), binarySniffWindow)], 0) < 0
					}
				}
			}
			if !searchable[name] {
				continue
			}
			line, err := strconv.Atoi(number)
			if err != nil {
				continue
			}
			location := matchLocation{name, line}
			if seen[location] {
				continue
			}
			if len(result.Matches) == limit {
				result.Truncated = true
				continue
			}
			seen[location] = true
			result.Matches = append(result.Matches, EvidenceMatch{Path: name, Line: line, Content: content})
		}
	}
	names, err := v.Paths(ctx)
	if err != nil {
		return result, err
	}
	temp := ""
	defer func() {
		if temp != "" {
			_ = os.RemoveAll(temp)
		}
	}()
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		e := v.entries[name]
		if e.unavailable != "" {
			result.Unavailable = append(result.Unavailable, name)
			continue
		}
		if !e.captured && v.baseObjects[name] == e.object {
			continue
		}
		if temp == "" {
			temp, err = os.MkdirTemp("", "ocr-evidence-search-")
			if err != nil {
				return result, err
			}
		}
		content, err := v.fileContent(ctx, name)
		if err != nil {
			result.Unavailable = append(result.Unavailable, name)
			continue
		}
		target := filepath.Join(temp, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return result, err
		}
		if err := os.WriteFile(target, []byte(content), 0600); err != nil {
			return result, err
		}
	}
	query := func(spec evidencePathspec, base, namesOnly bool) (string, bool, error) {
		dir := v.provider.repoDir
		queryArgs := append([]string{}, args...)
		if namesOnly {
			queryArgs = append(queryArgs, "-l")
		}
		if base {
			if ref == "" {
				return "", false, nil
			}
			queryArgs = append(queryArgs, ref, "--")
		} else {
			if temp == "" {
				return "", false, nil
			}
			dir = temp
			queryArgs = append(queryArgs, "--no-index", "--")
		}
		if len(spec.patterns) == 0 && !base {
			queryArgs = append(queryArgs, ".")
		} else {
			queryArgs = append(queryArgs, spec.patterns...)
		}
		return v.grep(ctx, dir, queryArgs, namesOnly)
	}
	for _, spec := range excludes {
		for _, base := range []bool{true, false} {
			out, cut, err := query(spec, base, true)
			if err != nil {
				return result, err
			}
			if cut {
				return result, fmt.Errorf("pathspec exclusion coverage exceeds search limit")
			}
			for _, name := range strings.Split(strings.TrimSuffix(out, "\x00"), "\x00") {
				name, entry, visible := resolve(name, base)
				if visible && spec.matches(entry) {
					excluded[name] = true
				}
			}
		}
	}
	for _, spec := range includes {
		for _, base := range []bool{true, false} {
			out, cut, err := query(spec, base, false)
			result.Truncated = result.Truncated || cut
			if err != nil {
				return result, err
			}
			collect(out, base, spec)
		}
	}
	return result, ctx.Err()
}

func (v *EvidenceView) grep(ctx context.Context, dir string, args []string, namesOnly bool) (string, bool, error) {
	stdout := evidenceOutput{limit: 4 << 20}
	stderr := evidenceOutput{limit: gitDiagLimit}
	err := v.provider.runGitIO(ctx, dir, nil, nil, &stdout, &stderr, args...)
	if ctx.Err() != nil {
		return "", false, ctx.Err()
	}
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return "", false, fmt.Errorf("evidence search failed: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
	}
	out := stdout.String()
	if stdout.truncated {
		delimiter := byte('\n')
		if namesOnly {
			delimiter = 0
		}
		if end := strings.LastIndexByte(out, delimiter); end >= 0 {
			out = out[:end+1]
		} else {
			out = ""
		}
	}
	return out, stdout.truncated, nil
}

type evidenceOutput struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *evidenceOutput) String() string { return b.buffer.String() }

func (b *evidenceOutput) Write(data []byte) (int, error) {
	keep := min(len(data), b.limit-b.buffer.Len())
	_, _ = b.buffer.Write(data[:keep])
	b.truncated = b.truncated || keep < len(data)
	return len(data), nil
}
