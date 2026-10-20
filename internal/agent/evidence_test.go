// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alibaba/open-code-review/internal/tool"
)

func TestReviewEvidenceIncludesUnselectedContext(t *testing.T) {
	repo := sealRepo(t)
	commitIn(t, repo, "context.txt", "base context\n", "context")
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.go", "package main\n\nfunc captured() {}\n")
	write("context.txt", "local context\n")
	fr := &tool.FileReader{RepoDir: repo}
	registry := tool.NewRegistry()
	registry.Register(tool.NewFileRead(fr))
	registry.Register(tool.NewCodeSearch(fr))
	registry.Register(tool.NewFileFind(fr))
	registry.Register(tool.NewFileReadDiff(tool.NewDiffMap(nil)))
	a := &Agent{args: Args{RepoDir: repo, Tools: registry}}
	ctx := context.Background()
	if err := a.loadDiffs(ctx); err != nil {
		t.Fatal(err)
	}
	a.injectDiffMap()
	registry.Freeze()
	kept, _ := summarizeSelection(a.selectFiles(a.diffs))
	for _, d := range kept {
		if d.NewPath == "context.txt" {
			t.Fatal("context fixture is selected for review")
		}
	}
	write("context.txt", "later context\n")
	out, err := tool.NewFileRead(fr).Execute(ctx, map[string]any{"file_path": "context.txt"})
	if err != nil || !strings.Contains(out, "local context") || strings.Contains(out, "later context") {
		t.Fatalf("context read: %q %v", out, err)
	}
	out, err = tool.NewCodeSearch(fr).Execute(ctx, map[string]any{"search_text": "local context"})
	if err != nil || !strings.Contains(out, "context.txt") {
		t.Fatalf("context search: %q %v", out, err)
	}
}
