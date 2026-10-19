// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/alibaba/open-code-review/internal/tool"
)

func TestManagedReviewAndHostPreviewUseDistinctObjectPolicies(t *testing.T) {
	for _, mode := range []string{"commit", "range"} {
		t.Run(mode, func(t *testing.T) {
			repo := sealRepo(t)
			gitIn(t, repo, "branch", "evidence-base")
			commitIn(t, repo, "main.go", "package main\n\nfunc Captured() {}\n", "original review")
			gitIn(t, repo, "branch", "evidence-original")
			gitIn(t, repo, "checkout", "--detach", "evidence-base")
			commitIn(t, repo, "other.go", "package other\n", "replacement review")
			gitIn(t, repo, "replace", "evidence-original", "HEAD")
			fr := &tool.FileReader{RepoDir: repo}
			registry := tool.NewRegistry()
			registry.Register(tool.NewFileRead(fr))
			registry.Register(tool.NewCodeSearch(fr))
			args := Args{RepoDir: repo, Tools: registry}
			if mode == "commit" {
				args.Commit = "evidence-original"
			} else {
				args.From = "evidence-base"
				args.To = "evidence-original"
			}
			ctx := context.Background()
			host, err := Preview(ctx, args)
			if err != nil || len(host.Entries) != 1 || host.Entries[0].Path != "other.go" || !host.Entries[0].WillReview {
				t.Fatalf("host preview: %+v %v", host, err)
			}
			args.ManagedPreview = true
			managed, err := Preview(ctx, args)
			if err != nil || len(managed.Entries) != 1 || managed.Entries[0].Path != "main.go" || !managed.Entries[0].WillReview {
				t.Fatalf("managed preview: %+v %v", managed, err)
			}
			a := &Agent{args: args}
			if err := a.loadDiffs(ctx); err != nil {
				t.Fatal(err)
			}
			if len(a.diffs) != 1 || a.diffs[0].NewPath != managed.Entries[0].Path {
				t.Fatalf("managed review differs from its preview: %+v", a.diffs)
			}
			before, err := fr.Evidence.Read(ctx, "main.go", 1, 0)
			if err != nil || !before.Available || !strings.Contains(before.Content, "func Captured()") {
				t.Fatalf("managed content: %+v %v", before, err)
			}
			gitIn(t, repo, "replace", "-f", "evidence-original", "evidence-base")
			after, err := fr.Evidence.Read(ctx, "main.go", 1, 0)
			if err != nil || after != before {
				t.Fatalf("replacement changes managed identity: %+v %v", after, err)
			}
			for _, provider := range []tool.Provider{tool.NewFileRead(fr), tool.NewCodeSearch(fr)} {
				arguments := map[string]any{"file_path": "main.go", "search_text": "Captured"}
				out, err := provider.Execute(ctx, arguments)
				if err != nil || !strings.Contains(out, "3|func Captured() {}") {
					t.Fatalf("%s loses captured evidence: %q %v", provider.Tool().Name(), out, err)
				}
			}
		})
	}
}
