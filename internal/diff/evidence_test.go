// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package diff

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/alibaba/open-code-review/internal/model"
)

func evidenceWrite(t *testing.T, repo, name, content string) {
	t.Helper()
	full := filepath.Join(repo, name)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func evidenceGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func evidenceRead(t *testing.T, v *EvidenceView, name, want string) Evidence {
	t.Helper()
	e, err := v.Read(context.Background(), name, 1, 0)
	if err != nil || !e.Available || e.Content != want {
		t.Fatalf("read %q: %+v, %v; want %q", name, e, err, want)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(want)))
	if e.Digest != digest || e.State == "" {
		t.Fatalf("invalid source identity: %+v", e)
	}
	return e
}

func TestEvidenceCommittedEndpointsStayFixed(t *testing.T) {
	for _, mode := range []string{"commit", "range"} {
		t.Run(mode, func(t *testing.T) {
			repo := initRepoWithChange(t)
			runGitTest(t, repo, "add", ".")
			runGitTest(t, repo, "commit", "-qm", "second")
			p := NewCommitProvider(repo, "HEAD", nil)
			if mode == "range" {
				p = NewProvider(repo, "HEAD~1", "HEAD", nil)
			}
			diffs, err := p.GetDiff(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			frozen := p.ResolveInput(context.Background())
			evidenceWrite(t, repo, "sample.txt", "later\n")
			runGitTest(t, repo, "add", ".")
			runGitTest(t, repo, "commit", "-qm", "third")
			evidenceRead(t, p.Evidence(), "sample.txt", "line1\nCHANGED\nline3\n")
			if got := p.ResolveInput(context.Background()); got != frozen {
				t.Fatalf("endpoints moved: %+v", got)
			}
			again, err := p.GetDiff(context.Background())
			if err != nil || !reflect.DeepEqual(diffs, again) {
				t.Fatalf("diff changed: %+v, %v", again, err)
			}
			result, err := p.Evidence().Search(context.Background(), "CHANGED", true, false, nil, 100)
			if err != nil || len(result.Matches) != 1 || result.Matches[0].Line != 2 {
				t.Fatalf("search: %+v %v", result, err)
			}
		})
	}
}

func TestEvidenceWorkspaceOverlaysAndRepositoryState(t *testing.T) {
	repo := initRepoWithChange(t)
	evidenceWrite(t, repo, "context.go", "base context\n")
	evidenceWrite(t, repo, "deleted.go", "obsolete needle\n")
	evidenceWrite(t, repo, "old.go", "rename needle\n")
	runGitTest(t, repo, "add", ".")
	runGitTest(t, repo, "commit", "-qm", "context")
	evidenceWrite(t, repo, "sample.txt", "review needle\n")
	evidenceWrite(t, repo, "context.go", "local needle\n")
	if err := os.Remove(filepath.Join(repo, "deleted.go")); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repo, "mv", "old.go", "renamed.go")
	evidenceWrite(t, repo, "new.go", "untracked needle")
	if err := os.Chmod(filepath.Join(repo, "context.go"), 0755); err != nil {
		t.Fatal(err)
	}
	beforeStatus := evidenceGit(t, repo, "status", "--porcelain=v1", "-z")
	beforeRefs := evidenceGit(t, repo, "show-ref")
	indexPath := strings.TrimSpace(evidenceGit(t, repo, "rev-parse", "--git-path", "index"))
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(repo, indexPath)
	}
	beforeIndex, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	p := NewWorkspaceProvider(repo, nil)
	diffs, err := p.GetDiff(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	v := p.Evidence()
	evidenceRead(t, v, "context.go", "local needle\n")
	e := evidenceRead(t, v, "new.go", "untracked needle")
	if e.Mode != "100644" {
		t.Fatal(e.Mode)
	}
	mode := evidenceRead(t, v, "context.go", "local needle\n")
	if mode.Mode != "100755" {
		t.Fatal(mode.Mode)
	}
	paths, err := v.Paths(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"deleted.go", "old.go"} {
		if slices.Contains(paths, name) {
			t.Fatalf("deleted path %s remains", name)
		}
		e, err := v.Read(context.Background(), name, 1, 0)
		if err != nil || e.Available {
			t.Fatalf("deleted read: %+v %v", e, err)
		}
	}
	result, err := v.Search(context.Background(), "needle", true, false, nil, 100)
	if err != nil || len(result.Matches) != 4 {
		t.Fatalf("overlay search: %+v %v", result, err)
	}
	if got := evidenceGit(t, repo, "status", "--porcelain=v1", "-z"); got != beforeStatus {
		t.Fatal("capture or search changes workspace status")
	}
	if got := evidenceGit(t, repo, "show-ref"); got != beforeRefs {
		t.Fatal("refs change")
	}
	afterIndex, err := os.ReadFile(indexPath)
	if err != nil || string(afterIndex) != string(beforeIndex) {
		t.Fatal("index changes")
	}
	evidenceWrite(t, repo, "context.go", "later\n")
	evidenceWrite(t, repo, "after.go", "later needle\n")
	runGitTest(t, repo, "add", ".")
	evidenceRead(t, v, "context.go", "local needle\n")
	after, err := v.Paths(context.Background())
	if err != nil || !reflect.DeepEqual(paths, after) {
		t.Fatalf("paths change: %v %v", after, err)
	}
	for _, d := range diffs {
		if d.NewPath == "context.go" && d.NewFileContent != "local needle\n" {
			t.Fatalf("diff context: %+v", d)
		}
	}
	result, err = v.Search(context.Background(), "later", true, false, nil, 100)
	if err != nil || len(result.Matches) != 0 {
		t.Fatalf("later bytes appear: %+v %v", result, err)
	}
}

func TestEvidenceSelectedWorkspaceSource(t *testing.T) {
	for _, mode := range []string{"combined", "fallback", "unborn"} {
		t.Run(mode, func(t *testing.T) {
			repo := t.TempDir()
			runGitTest(t, repo, "init", "-q")
			runGitTest(t, repo, "config", "user.email", "test@example.com")
			runGitTest(t, repo, "config", "user.name", "Test")
			if mode != "unborn" {
				evidenceWrite(t, repo, "source.go", "base\n")
				runGitTest(t, repo, "add", ".")
				runGitTest(t, repo, "-c", "commit.gpgsign=false", "commit", "-qm", "base")
			}
			evidenceWrite(t, repo, "source.go", "index\n")
			runGitTest(t, repo, "add", ".")
			want := "index\n"
			switch mode {
			case "combined":
				want = "disk\n"
				evidenceWrite(t, repo, "source.go", want)
			case "fallback":
				evidenceWrite(t, repo, "source.go", "base\n")
			case "unborn":
				evidenceWrite(t, repo, "source.go", "disk\n")
			}
			p := NewWorkspaceProvider(repo, nil)
			diffs, err := p.GetDiff(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(diffs) != 1 || diffs[0].NewFileContent != want {
				t.Fatalf("effective source: %+v; want %q", diffs, want)
			}
			evidenceRead(t, p.Evidence(), "source.go", want)
			evidenceWrite(t, repo, "source.go", "later\n")
			runGitTest(t, repo, "add", ".")
			result, err := p.Evidence().Search(context.Background(), strings.TrimSpace(want), true, false, nil, 100)
			if err != nil || len(result.Matches) != 1 {
				t.Fatalf("source search: %+v %v", result, err)
			}
		})
	}
}

func TestEvidenceUnavailableAndReconciliation(t *testing.T) {
	repo := initRepoWithChange(t)
	p := NewWorkspaceProvider(repo, nil)
	if _, err := p.GetDiff(context.Background()); err != nil {
		t.Fatal(err)
	}
	v := p.Evidence()
	v.entries["sample.txt"] = evidenceEntry{captured: true, content: "raced\n", mode: "100644"}
	expected := strings.TrimSpace(evidenceGit(t, repo, "hash-object", "sample.txt"))
	diffs := []model.Diff{{OldPath: "sample.txt", NewPath: "sample.txt", NewFileContent: "raced\n"}}
	v.reconcile(context.Background(), diffs, "diff --git a/sample.txt b/sample.txt\nindex "+strings.Repeat("0", 40)+".."+expected+" 100644\n")
	e, err := v.Read(context.Background(), "sample.txt", 1, 0)
	if err != nil || e.Available || !strings.Contains(e.Reason, "does not match") || diffs[0].NewFileContent != "" {
		t.Fatalf("mismatch: %+v %v", e, err)
	}
	result, err := v.Search(context.Background(), "line1", true, false, nil, 100)
	if err != nil || len(result.Matches) != 0 || !slices.Contains(result.Unavailable, "sample.txt") {
		t.Fatalf("unavailable search: %+v %v", result, err)
	}
}

func TestEvidenceBoundariesCancellationAndSearchLimits(t *testing.T) {
	repo := initRepoWithChange(t)
	outside := t.TempDir()
	evidenceWrite(t, outside, "secret.go", "outside secret\n")
	if err := os.Symlink(filepath.Join(outside, "secret.go"), filepath.Join(repo, "link.go")); err != nil {
		t.Fatal(err)
	}
	evidenceWrite(t, repo, "many.go", strings.Repeat("Needle value\n", 110))
	p := NewWorkspaceProvider(repo, nil)
	if _, err := p.GetDiff(context.Background()); err != nil {
		t.Fatal(err)
	}
	v := p.Evidence()
	e := evidenceRead(t, v, "link.go", filepath.Join(outside, "secret.go"))
	if e.Mode != "120000" {
		t.Fatal(e.Mode)
	}
	for _, name := range []string{"../secret.go", "/etc/passwd", ".git/config", "a/../sample.txt"} {
		if _, err := v.Read(context.Background(), name, 1, 0); err == nil {
			t.Fatalf("unsafe path %q succeeds", name)
		}
	}
	result, err := v.Search(context.Background(), "(?<=Needle )value", true, true, []string{"*.go"}, 100)
	if err != nil || len(result.Matches) != 100 || !result.Truncated {
		t.Fatalf("PCRE limit: %+v %v", result, err)
	}
	result, err = v.Search(context.Background(), "needle", true, false, nil, 100)
	if err != nil || len(result.Matches) != 0 {
		t.Fatalf("case-sensitive: %+v %v", result, err)
	}
	result, err = v.Search(context.Background(), "outside secret", false, false, nil, 100)
	if err != nil || len(result.Matches) != 0 {
		t.Fatalf("symlink escape: %+v %v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := v.Read(ctx, "sample.txt", 1, 0); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := v.Search(ctx, "Needle", false, false, nil, 100); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := v.Paths(ctx); err != context.Canceled {
		t.Fatal(err)
	}
}

func TestEvidenceSearchByteLimitReportsPartialCoverage(t *testing.T) {
	repo := initRepoWithChange(t)
	evidenceWrite(t, repo, "long.go", "longmatch "+strings.Repeat("x", 5<<20)+"\n")
	p := NewWorkspaceProvider(repo, nil)
	if _, err := p.GetDiff(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err := p.Evidence().Search(context.Background(), "longmatch", true, false, nil, 100)
	if err != nil || !result.Truncated || len(result.Matches) != 0 {
		t.Fatalf("partial line is not safe evidence: matches=%d truncated=%t err=%v", len(result.Matches), result.Truncated, err)
	}
}

func TestEvidenceRenameReconciliationRejectsLaterBytes(t *testing.T) {
	repo := initRepoWithChange(t)
	p := NewWorkspaceProvider(repo, nil)
	if _, err := p.GetDiff(context.Background()); err != nil {
		t.Fatal(err)
	}
	v := p.Evidence()
	v.entries["renamed.txt"] = evidenceEntry{captured: true, content: "later\n", mode: "100644"}
	diffs := []model.Diff{{OldPath: "sample.txt", NewPath: "renamed.txt", IsRenamed: true, NewFileContent: "later\n"}}
	v.reconcile(context.Background(), diffs, "diff --git a/sample.txt b/renamed.txt\nsimilarity index 100%\nrename from sample.txt\nrename to renamed.txt\n")
	e, err := v.Read(context.Background(), "renamed.txt", 1, 0)
	if err != nil || e.Available || !strings.Contains(e.Reason, "does not match") || diffs[0].NewFileContent != "" {
		t.Fatalf("rename mismatch: %+v %v", e, err)
	}
}

func TestEvidenceStagedFallbackKeepsResolvedBase(t *testing.T) {
	repo := initRepoWithChange(t)
	p := NewWorkspaceProvider(repo, nil)
	input := p.ResolveInput(context.Background())
	p.input = &input
	evidenceWrite(t, repo, "sample.txt", "index content\n")
	runGitTest(t, repo, "add", ".")
	evidenceWrite(t, repo, "sample.txt", "line1\nline2\nline3\n")
	runGitTest(t, repo, "commit", "-qm", "move HEAD without the disk change")
	diffs, err := p.GetDiff(context.Background())
	if err != nil || len(diffs) != 1 || diffs[0].NewFileContent != "index content\n" {
		t.Fatalf("fallback changes its comparison base: %+v %v", diffs, err)
	}
	evidenceRead(t, p.Evidence(), "sample.txt", "index content\n")
}
