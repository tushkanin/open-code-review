// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package diff

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/alibaba/open-code-review/internal/gitcmd"
)

func TestEvidenceSearchKeepsCapturedAttributes(t *testing.T) {
	for _, mode := range []string{"workspace", "commit", "range"} {
		for _, initial := range []string{"auto", "binary", "driver", "driver-text", "driver-auto-text", "driver-auto-binary", "text"} {
			t.Run(mode+"/"+initial, func(t *testing.T) {
				repo := initRepoWithChange(t)
				content := "attribute needle\n"
				attribute := ""
				want := 1
				switch initial {
				case "binary":
					attribute = "context.go -diff\n"
					want = 0
				case "driver":
					attribute = "context.go diff=captured\n"
					want = 0
					runGitTest(t, repo, "config", "diff.captured.binary", "true")
				case "driver-text":
					attribute = "context.go diff=captured\n"
					content = "attribute needle\x00\n"
					runGitTest(t, repo, "config", "diff.captured.binary", "off")
				case "driver-auto-text", "driver-auto-binary":
					attribute = "context.go diff=captured\n"
					runGitTest(t, repo, "config", "diff.captured.binary", "auto")
					if initial == "driver-auto-binary" {
						content = "attribute needle\x00\n"
						want = 0
					}
				case "text":
					attribute = "context.go diff\n"
					content = "attribute needle\x00\n"
				}
				evidenceWrite(t, repo, "context.go", content)
				evidenceWrite(t, repo, ".gitattributes", attribute)
				runGitTest(t, repo, "add", ".")
				runGitTest(t, repo, "commit", "-qm", "context")
				if mode == "workspace" && strings.HasPrefix(initial, "driver-auto-") {
					content = "local " + content
					evidenceWrite(t, repo, "context.go", content)
				}
				evidenceWrite(t, repo, "sample.txt", "review change\n")
				p := NewWorkspaceProvider(repo, nil)
				if mode != "workspace" {
					runGitTest(t, repo, "add", ".")
					runGitTest(t, repo, "commit", "-qm", "review")
					p = NewCommitProvider(repo, "HEAD", nil)
					if mode == "range" {
						p = NewProvider(repo, "HEAD~1", "HEAD", nil)
					}
				}
				if _, err := p.GetDiff(context.Background()); err != nil {
					t.Fatal(err)
				}
				before, err := p.Evidence().Search(context.Background(), "attribute needle", true, false, nil, 100)
				if err != nil || len(before.Matches) != want {
					t.Fatalf("captured classification: %+v %v", before, err)
				}
				later := "context.go -diff\n"
				if want == 0 {
					later = "context.go diff\n"
				}
				evidenceWrite(t, repo, ".gitattributes", later)
				evidenceWrite(t, repo, ".git/info/attributes", later)
				runGitTest(t, repo, "config", "diff.captured.binary", "false")
				after, err := p.Evidence().Search(context.Background(), "attribute needle", true, false, nil, 100)
				if err != nil || len(after.Matches) != want {
					t.Fatalf("later attributes change search: %+v %v", after, err)
				}
				if len(after.Matches) == 1 && after.Matches[0].Content != strings.TrimSuffix(content, "\n") {
					t.Fatalf("search content: %+v", after)
				}
				evidenceRead(t, p.Evidence(), "context.go", content)
			})
		}
	}
}

func TestEvidenceSearchUsesLocalAttributeOverrides(t *testing.T) {
	repo := initRepoWithChange(t)
	evidenceWrite(t, repo, "context.go", "local attribute needle\n")
	evidenceWrite(t, repo, ".gitattributes", "context.go -diff\n")
	runGitTest(t, repo, "add", ".")
	runGitTest(t, repo, "commit", "-qm", "base attributes")
	evidenceWrite(t, repo, ".gitattributes", "context.go diff\n")
	p := NewWorkspaceProvider(repo, nil)
	if _, err := p.GetDiff(context.Background()); err != nil {
		t.Fatal(err)
	}
	evidenceWrite(t, repo, ".gitattributes", "context.go -diff\n")
	result, err := p.Evidence().Search(context.Background(), "local attribute needle", true, false, nil, 100)
	if err != nil || len(result.Matches) != 1 || result.Matches[0].Path != "context.go" {
		t.Fatalf("local attribute override: %+v %v", result, err)
	}
}

func TestEvidenceSearchHonorsSharedProcessLimit(t *testing.T) {
	for _, source := range []string{"base", "overlay"} {
		t.Run(source, func(t *testing.T) {
			repo := t.TempDir()
			runGitTest(t, repo, "init", "-q")
			evidenceWrite(t, repo, "context.go", "slot needle\n")
			runner := gitcmd.New(1)
			p := NewWorkspaceProvider(repo, runner)
			if source == "base" {
				runGitTest(t, repo, "config", "user.email", "test@example.com")
				runGitTest(t, repo, "config", "user.name", "Test")
				runGitTest(t, repo, "add", ".")
				runGitTest(t, repo, "-c", "commit.gpgsign=false", "commit", "-qm", "base")
				p = NewCommitProvider(repo, "HEAD", runner)
			}
			if _, err := p.GetDiff(context.Background()); err != nil {
				t.Fatal(err)
			}
			acquired := make(chan struct{})
			release := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				done <- runner.Stream(context.Background(), repo, func(stdout io.Reader) error {
					close(acquired)
					<-release
					_, err := io.Copy(io.Discard, stdout)
					return err
				}, "status", "--porcelain")
			}()
			<-acquired
			defer func() {
				close(release)
				if err := <-done; err != nil {
					t.Error(err)
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			result, err := p.Evidence().Search(ctx, "slot needle", true, false, nil, 100)
			if !errors.Is(err, context.DeadlineExceeded) || len(result.Matches) != 0 {
				t.Fatalf("search bypasses held process slot: %+v %v", result, err)
			}
		})
	}
}
