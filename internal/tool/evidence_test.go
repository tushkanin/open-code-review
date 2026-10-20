// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alibaba/open-code-review/internal/diff"
)

func TestEvidenceProvidersKeepCapturedSource(t *testing.T) {
	for _, mode := range []string{"workspace", "commit", "range"} {
		t.Run(mode, func(t *testing.T) {
			repo := setupTestRepo(t)
			write := func(name, text string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(repo, name), []byte(text), 0644); err != nil {
					t.Fatal(err)
				}
			}
			git := func(args ...string) {
				t.Helper()
				cmd := exec.Command("git", args...)
				cmd.Dir = repo
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git: %s %v", out, err)
				}
			}
			write("hello.go", "package main\n\nfunc Captured() {}\n")
			write("context.go", "captured context\n")
			write(".gitattributes", "context.go diff\n")
			p := diff.NewWorkspaceProvider(repo, nil)
			if mode != "workspace" {
				git("add", ".")
				git("commit", "-m", "capture")
				p = diff.NewCommitProvider(repo, "HEAD", nil)
				if mode == "range" {
					p = diff.NewProvider(repo, "HEAD~1", "HEAD", nil)
				}
			}
			diffs, err := p.GetDiff(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			fr := &FileReader{RepoDir: repo, Evidence: p.Evidence()}
			dm := make(map[string]string)
			for _, d := range diffs {
				dm[d.NewPath] = d.Diff
			}
			read := NewFileRead(fr)
			search := NewCodeSearch(fr)
			find := NewFileFind(fr)
			readDiff := NewFileReadDiff(NewDiffMap(dm))
			write("hello.go", "later bytes\n")
			write("context.go", "later context\n")
			write(".gitattributes", "context.go -diff\n")
			write("after.go", "later file\n")
			git("add", ".")
			git("commit", "-m", "move source")
			for _, scenario := range []struct {
				p      Provider
				args   map[string]any
				want   string
				absent string
			}{
				{read, map[string]any{"file_path": "hello.go", "start_line": float64(3), "end_line": float64(3)}, "3|func Captured() {}", "later"},
				{read, map[string]any{"file_path": "context.go"}, "captured context", "later"},
				{search, map[string]any{"search_text": "Captured", "case_sensitive": true}, "3|func Captured() {}", "later"},
				{search, map[string]any{"search_text": "captured context", "file_patterns": []any{":(attr:diff)*.go"}}, "1|captured context", "later"},
				{find, map[string]any{"query_name": "context.go"}, "context.go", "after.go"},
				{readDiff, map[string]any{"path_array": []any{"hello.go"}}, "+func Captured() {}", "later"},
			} {
				out, err := scenario.p.Execute(context.Background(), scenario.args)
				if err != nil || !strings.Contains(out, scenario.want) || strings.Contains(out, scenario.absent) {
					t.Fatalf("%s: %q %v", scenario.p.Tool().Name(), out, err)
				}
			}
			out, err := find.Execute(context.Background(), map[string]any{"query_name": "after.go"})
			if err != nil || strings.Contains(out, "after.go") {
				t.Fatalf("new path appears: %q %v", out, err)
			}
			out, err = search.Execute(context.Background(), map[string]any{"search_text": "later"})
			if err != nil || out != "No matches found" {
				t.Fatalf("later source appears: %q %v", out, err)
			}
		})
	}
}

func TestEvidenceProviderLimitsAndLiveScanSource(t *testing.T) {
	repo := setupTestRepo(t)
	for i := range 101 {
		if err := os.WriteFile(filepath.Join(repo, fmt.Sprintf("match%03d.go", i)), []byte("needle\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	p := diff.NewWorkspaceProvider(repo, nil)
	if _, err := p.GetDiff(context.Background()); err != nil {
		t.Fatal(err)
	}
	fr := &FileReader{RepoDir: repo, Evidence: p.Evidence()}
	out, err := NewFileFind(fr).Execute(context.Background(), map[string]any{"query_name": "match"})
	if err != nil || !strings.Contains(out, "Coverage is limited") || strings.Contains(out, "match100.go") {
		t.Fatalf("lookup limit: %q %v", out, err)
	}
	out, err = NewCodeSearch(fr).Execute(context.Background(), map[string]any{"search_text": "needle"})
	if err != nil || !strings.Contains(out, "Coverage is limited") {
		t.Fatalf("search limit: %q %v", out, err)
	}
	plain := t.TempDir()
	name := filepath.Join(plain, "scan.go")
	if err := os.WriteFile(name, []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}
	live := &FileReader{RepoDir: plain}
	if err := os.WriteFile(name, []byte("live scan\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out, err = NewFileRead(live).Execute(context.Background(), map[string]any{"file_path": "scan.go"})
	if err != nil || !strings.Contains(out, "live scan") {
		t.Fatalf("scan read: %q %v", out, err)
	}
	out, err = NewCodeSearch(live).Execute(context.Background(), map[string]any{"search_text": "live scan"})
	if err != nil || !strings.Contains(out, "live scan") {
		t.Fatalf("scan search: %q %v", out, err)
	}
}
