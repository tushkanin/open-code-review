// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewAndDelegatePreviewKeepTheirObjectPolicies(t *testing.T) {
	for _, mode := range []string{"commit", "range"} {
		t.Run(mode, func(t *testing.T) {
			freshOCRHome(t)
			repo := initTestGitRepo(t)
			git := func(args ...string) string {
				t.Helper()
				out, err := runGitCmdStdout(repo, args...)
				if err != nil {
					t.Fatalf("git %v: %v", args, err)
				}
				return strings.TrimSpace(string(out))
			}
			root := git("rev-parse", "HEAD")
			gitCommitFile(t, repo, "main.go", "package main\nconst Value = 0\n", "base source")
			base := git("rev-parse", "HEAD")
			gitCommitFile(t, repo, "main.go", "package main\nconst Value = 1\n", "original review")
			original := git("rev-parse", "HEAD")
			git("checkout", "--detach", root)
			gitCommitFile(t, repo, "other.go", "package other\n", "replacement review")
			replacement := git("rev-parse", "HEAD")
			git("replace", original, replacement)
			review := reviewOptions{commit: original, outputFormat: "json"}
			delegated := delegateOptions{repoDir: repo, commit: original, format: "json"}
			ordinary := git("show", "--format=", "--name-only", original)
			if mode == "range" {
				review.commit = ""
				review.from = base
				review.to = original
				delegated.commit = ""
				delegated.from = base
				delegated.to = original
				ordinary = git("diff", "--name-only", git("merge-base", base, original), original)
			}
			if ordinary != "other.go" {
				t.Fatalf("ordinary Git paths: %q", ordinary)
			}
			cc, err := loadCommonContext(repo, "", "", 0, 0, true)
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if err := runPreviewContext(context.Background(), cc, review, &output); err != nil {
				t.Fatal(err)
			}
			managed := decodeSinglePreviewJSON(t, output.String())
			if len(managed.Entries) != 1 || managed.Entries[0].Path != "main.go" || !managed.Entries[0].WillReview {
				t.Fatalf("managed CLI preview: %+v", managed)
			}
			raw := captureDelegateStdout(t, func() {
				if err := executeDelegatePreview(delegated); err != nil {
					t.Fatal(err)
				}
			})
			var host delegatePreviewJSON
			if err := json.Unmarshal(raw, &host); err != nil {
				t.Fatal(err)
			}
			if len(host.ReviewableFiles) != 1 || host.ReviewableFiles[0].Path != ordinary {
				t.Fatalf("delegation differs from host Git: %+v", host)
			}
			if mode == "range" && host.MergeBase != git("merge-base", base, original) {
				t.Fatalf("delegated merge-base: %q", host.MergeBase)
			}
		})
	}
}

func TestAutomaticDiffDriverSupportsReviewAndDelegation(t *testing.T) {
	for _, mode := range []string{"workspace", "commit", "range"} {
		t.Run(mode, func(t *testing.T) {
			freshOCRHome(t)
			repo := initTestGitRepo(t)
			write := func(name, content string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}
			write("context.go", "package context\nconst Needle = true\n")
			write("binary.go", "binary Needle\x00\n")
			write(".gitattributes", "context.go diff=custom\nbinary.go diff=custom\n")
			gitCommitFile(t, repo, "main.go", "package main\nfunc Base() {}\n", "base")
			if _, err := runGitCmdStdout(repo, "config", "diff.custom.binary", "auto"); err != nil {
				t.Fatal(err)
			}
			review := reviewOptions{outputFormat: "json"}
			delegated := delegateOptions{repoDir: repo, format: "json"}
			if mode == "workspace" {
				write("main.go", "package main\nfunc Review() {}\n")
			} else {
				gitCommitFile(t, repo, "main.go", "package main\nfunc Review() {}\n", "review")
				if mode == "commit" {
					review.commit = "HEAD"
					delegated.commit = "HEAD"
				} else {
					review.from = "HEAD~1"
					review.to = "HEAD"
					delegated.from = review.from
					delegated.to = review.to
				}
			}
			cc, err := loadCommonContext(repo, "", "", 0, 0, true)
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if err := runPreviewContext(context.Background(), cc, review, &output); err != nil {
				t.Fatal(err)
			}
			managed := decodeSinglePreviewJSON(t, output.String())
			if len(managed.Entries) != 1 || managed.Entries[0].Path != "main.go" || !managed.Entries[0].WillReview {
				t.Fatalf("automatic driver changes managed selection: %+v", managed)
			}
			raw := captureDelegateStdout(t, func() {
				if err := executeDelegatePreview(delegated); err != nil {
					t.Fatal(err)
				}
			})
			var host delegatePreviewJSON
			if err := json.Unmarshal(raw, &host); err != nil {
				t.Fatal(err)
			}
			if len(host.ReviewableFiles) != 1 || host.ReviewableFiles[0].Path != "main.go" {
				t.Fatalf("automatic driver changes delegation: %+v", host)
			}
		})
	}
}
