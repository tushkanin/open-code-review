// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package diff

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/alibaba/open-code-review/internal/gitcmd"
)

func TestEvidenceAttributesUseSelectedSource(t *testing.T) {
	for _, mode := range []string{"fallback", "commit", "range"} {
		for _, state := range []string{"set", "unset"} {
			t.Run(mode+"/"+state, func(t *testing.T) {
				repo := initRepoWithChange(t)
				selected, opposite := "selected", "-selected"
				if state == "unset" {
					selected, opposite = opposite, selected
				}
				captured := "context.go " + selected + " diff\nbinary.go -diff\n"
				disk := "context.go " + opposite + " -diff\nbinary.go diff\n"
				evidenceWrite(t, repo, "context.go", "captured needle\x00\n")
				evidenceWrite(t, repo, "binary.go", "binary needle\n")
				evidenceWrite(t, repo, ".gitattributes", disk)
				runGitTest(t, repo, "add", ".")
				runGitTest(t, repo, "commit", "-qm", "base attributes")
				evidenceWrite(t, repo, ".gitattributes", captured)
				runGitTest(t, repo, "add", ".")
				p := NewWorkspaceProvider(repo, nil)
				if mode != "fallback" {
					runGitTest(t, repo, "commit", "-qm", "selected attributes")
					p = NewCommitProvider(repo, "HEAD", nil)
					if mode == "range" {
						p = NewProvider(repo, "HEAD~1", "HEAD", nil)
					}
				}
				evidenceWrite(t, repo, ".gitattributes", disk)
				if _, err := p.GetDiff(context.Background()); err != nil {
					t.Fatal(err)
				}
				evidenceRead(t, p.Evidence(), ".gitattributes", captured)
				for _, query := range []struct {
					patterns []string
					want     int
				}{
					{nil, 1},
					{[]string{":(attr:" + selected + ")*.go"}, 1},
					{[]string{":(attr:" + opposite + ")*.go"}, 0},
					{[]string{":(attr:selected=set)*.go"}, 0},
					{[]string{":(attr:selected=unset)*.go"}, 0},
				} {
					result, err := p.Evidence().Search(context.Background(), "needle", true, false, query.patterns, 100)
					if err != nil || len(result.Matches) != query.want || result.Truncated || len(result.Unavailable) != 0 {
						t.Fatalf("%v: %+v %v", query.patterns, result, err)
					}
					if query.want == 1 && (result.Matches[0].Path != "context.go" || result.Matches[0].Content != "captured needle\x00") {
						t.Fatalf("selected text classification: %+v", result)
					}
				}
			})
		}
	}
}

func TestEvidenceIgnoresReplacementRefs(t *testing.T) {
	for _, mode := range []string{"workspace", "commit", "range"} {
		for _, shared := range []bool{false, true} {
			label := mode + "/direct"
			if shared {
				label = mode + "/shared"
			}
			t.Run(label, func(t *testing.T) {
				repo := initRepoWithChange(t)
				evidenceWrite(t, repo, "source.go", "base needle\n")
				runGitTest(t, repo, "add", ".")
				runGitTest(t, repo, "commit", "-qm", "base source")
				evidenceWrite(t, repo, "source.go", "captured needle\n")
				runGitTest(t, repo, "add", ".")
				runGitTest(t, repo, "commit", "-qm", "captured source")
				var runner *gitcmd.Runner
				if shared {
					runner = gitcmd.New(2)
				}
				p := NewWorkspaceProvider(repo, runner)
				if mode == "workspace" {
					evidenceWrite(t, repo, "sample.txt", "local review\n")
				} else if mode == "commit" {
					p = NewCommitProvider(repo, "HEAD", runner)
				} else {
					p = NewProvider(repo, "HEAD~1", "HEAD", runner)
				}
				object := strings.TrimSpace(evidenceGit(t, repo, "rev-parse", "HEAD:source.go"))
				evidenceWrite(t, repo, "replacement.txt", "initial replacement needle\n")
				replacement := strings.TrimSpace(evidenceGit(t, repo, "hash-object", "-w", "replacement.txt"))
				runGitTest(t, repo, "replace", object, replacement)
				diffs, err := p.GetDiff(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				before := evidenceRead(t, p.Evidence(), "source.go", "captured needle\n")
				input := p.ResolveInput(context.Background())
				evidenceWrite(t, repo, "replacement.txt", "later replacement needle\n")
				replacement = strings.TrimSpace(evidenceGit(t, repo, "hash-object", "-w", "replacement.txt"))
				runGitTest(t, repo, "replace", "-f", object, replacement)
				for _, target := range []string{"blob", "tree"} {
					if target == "tree" {
						evidenceWrite(t, repo, "source.go", "later tree needle\n")
						runGitTest(t, repo, "add", ".")
						tree := strings.TrimSpace(evidenceGit(t, repo, "write-tree"))
						endpoint := input.ResolvedHead
						if mode == "workspace" {
							endpoint = input.ResolvedBase
						}
						original := strings.TrimSpace(evidenceGit(t, repo, "rev-parse", endpoint+"^{tree}"))
						runGitTest(t, repo, "replace", original, tree)
					}
					after := evidenceRead(t, p.Evidence(), "source.go", "captured needle\n")
					if before != after || p.ResolveInput(context.Background()) != input {
						t.Fatalf("%s replacement changes identity: %+v %+v", target, before, after)
					}
					result, err := p.Evidence().Search(context.Background(), "needle", true, false, []string{"source.go"}, 100)
					if err != nil || len(result.Matches) != 1 || result.Matches[0].Content != "captured needle" || result.Truncated || len(result.Unavailable) != 0 {
						t.Fatalf("%s replacement changes search: %+v %v", target, result, err)
					}
					again, err := p.GetDiff(context.Background())
					if err != nil || !reflect.DeepEqual(diffs, again) {
						t.Fatalf("%s replacement changes diff: %+v %v", target, again, err)
					}
				}
			})
		}
	}
}
