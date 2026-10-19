// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package diff

import (
	"context"
	"sort"
	"strings"
	"testing"
)

func TestEvidenceSearchKeepsAttributePathspecSemantics(t *testing.T) {
	for _, mode := range []string{"workspace", "commit", "range"} {
		t.Run(mode, func(t *testing.T) {
			repo := initRepoWithChange(t)
			paths := []string{"base.go", "local.go", "unset.go", "value.go", "none.go", "word.go", "nested/Child.go"}
			for _, name := range paths {
				evidenceWrite(t, repo, name, "captured "+name+" needle\n")
			}
			evidenceWrite(t, repo, ".gitattributes", "*.go diff\nbase.go tag\nfresh.go tag\nlocal.go tag=set\nunset.go -tag\nvalue.go tag=unset\nnone.go !tag\nword.go tag=unspecified\nnested/* tag=blue\n")
			runGitTest(t, repo, "add", ".")
			runGitTest(t, repo, "commit", "-qm", "attribute source")
			evidenceWrite(t, repo, "local.go", "local local.go needle\n")
			evidenceWrite(t, repo, "fresh.go", "captured fresh.go needle\n")
			p := NewWorkspaceProvider(repo, nil)
			if mode != "workspace" {
				runGitTest(t, repo, "add", ".")
				runGitTest(t, repo, "commit", "-qm", "review source")
				p = NewCommitProvider(repo, "HEAD", nil)
				if mode == "range" {
					p = NewProvider(repo, "HEAD~1", "HEAD", nil)
				}
			}
			if _, err := p.GetDiff(context.Background()); err != nil {
				t.Fatal(err)
			}
			scenarios := []struct {
				patterns []string
				want     string
				limit    int
			}{
				{[]string{":(attr:tag)*.go"}, "base.go,fresh.go", 100},
				{[]string{":(attr:tag=set)*.go"}, "local.go", 100},
				{[]string{":(attr:-tag)*.go"}, "unset.go", 100},
				{[]string{":(attr:tag=unset)*.go"}, "value.go", 100},
				{[]string{":(attr:!tag)*.go"}, "none.go", 100},
				{[]string{":(attr:tag=unspecified)*.go"}, "word.go", 100},
				{[]string{":(top,icase,glob,attr:tag=blue)nested/*.GO"}, "nested/Child.go", 100},
				{[]string{":(literal,attr:tag)base.go"}, "base.go", 100},
				{[]string{":(attr:tag diff)*.go"}, "base.go,fresh.go", 100},
				{[]string{":(attr:tag)*.go", ":(attr:tag=blue)*.go"}, "base.go,fresh.go,nested/Child.go", 100},
				{[]string{"*.go", ":(exclude,attr:tag)*.go", ":!local.go", ":(exclude,attr:-tag)*.go"}, "nested/Child.go,none.go,value.go,word.go", 100},
				{[]string{":(exclude,attr:!tag)*"}, "base.go,fresh.go,local.go,nested/Child.go,unset.go,value.go,word.go", 100},
				{[]string{":(attr:tag)*.go", ":(attr:tag)base.go"}, "base.go,fresh.go", 2},
			}
			for round := range 2 {
				for _, scenario := range scenarios {
					result, err := p.Evidence().Search(context.Background(), "needle", true, false, scenario.patterns, scenario.limit)
					if err != nil {
						t.Fatalf("round %d, %v: %v", round, scenario.patterns, err)
					}
					got := make([]string, 0, len(result.Matches))
					for _, match := range result.Matches {
						got = append(got, match.Path)
						wantContent := "captured " + match.Path + " needle"
						if match.Path == "local.go" {
							wantContent = "local local.go needle"
						}
						if match.Content != wantContent || match.Line != 1 {
							t.Fatalf("captured match: %+v", match)
						}
					}
					sort.Strings(got)
					if strings.Join(got, ",") != scenario.want || result.Truncated || len(result.Unavailable) != 0 {
						t.Fatalf("round %d, %v: %+v; want %s", round, scenario.patterns, result, scenario.want)
					}
				}
				if round == 0 {
					evidenceWrite(t, repo, ".gitattributes", "*.go -tag -diff\n")
					evidenceWrite(t, repo, ".git/info/attributes", "*.go tag=later\n")
					evidenceWrite(t, repo, "base.go", "later bytes\n")
					runGitTest(t, repo, "add", ".")
					runGitTest(t, repo, "commit", "-qm", "move attribute source")
				}
			}
		})
	}
}

func TestEvidenceSearchUsesCapturedAttributeOverlay(t *testing.T) {
	for _, source := range []string{"unstaged", "staged", "untracked", "unborn"} {
		t.Run(source, func(t *testing.T) {
			repo := initRepoWithChange(t)
			evidenceWrite(t, repo, "context.go", "overlay needle\n")
			runGitTest(t, repo, "add", ".")
			runGitTest(t, repo, "commit", "-qm", "context source")
			if source == "unborn" {
				repo = t.TempDir()
				runGitTest(t, repo, "init", "-q")
				evidenceWrite(t, repo, "context.go", "overlay needle\n")
			}
			evidenceWrite(t, repo, ".gitattributes", "context.go selected diff\n")
			if source == "unstaged" {
				evidenceWrite(t, repo, ".gitattributes", "context.go -selected diff\n")
				runGitTest(t, repo, "add", ".")
				runGitTest(t, repo, "commit", "-qm", "base attributes")
				evidenceWrite(t, repo, ".gitattributes", "context.go selected diff\n")
			} else if source == "staged" {
				runGitTest(t, repo, "add", ".")
			}
			p := NewWorkspaceProvider(repo, nil)
			if _, err := p.GetDiff(context.Background()); err != nil {
				t.Fatal(err)
			}
			evidenceWrite(t, repo, ".gitattributes", "context.go -selected -diff\n")
			result, err := p.Evidence().Search(context.Background(), "overlay needle", true, false, []string{":(attr:selected)*.go"}, 100)
			if err != nil || len(result.Matches) != 1 || result.Matches[0].Path != "context.go" {
				t.Fatalf("captured attribute overlay: %+v %v", result, err)
			}
		})
	}
}
