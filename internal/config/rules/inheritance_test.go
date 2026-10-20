// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package rules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func writeInheritanceFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeInheritanceConfig(t *testing.T, dir string, config ProjectRule) string {
	t.Helper()
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".opencodereview", "rule.json")
	writeInheritanceFile(t, path, string(data))
	return path
}

func inheritanceTree(t *testing.T) (home, parent, repo string) {
	t.Helper()
	home = t.TempDir()
	setTestHome(t, home)
	parent = filepath.Join(t.TempDir(), "parent")
	repo = filepath.Join(parent, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	return home, parent, repo
}

func inheritanceResolver(t *testing.T, repo, custom string) (Resolver, *FileFilter) {
	t.Helper()
	resolver, filter, err := NewResolver(repo, custom, ResolverOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return resolver, filter
}

func assertInheritanceDetail(t *testing.T, resolver Resolver, path, rule, source, pattern string) {
	t.Helper()
	detail := resolver.(DetailResolver).ResolveDetail(path)
	if detail.Rule != rule || detail.Source != source || detail.Pattern != pattern {
		t.Fatalf("ResolveDetail(%q) = %+v, want rule %q, source %q, pattern %q", path, detail, rule, source, pattern)
	}
	if got := resolver.Resolve(path); got != detail.Rule {
		t.Fatalf("Resolve(%q) = %q, ResolveDetail rule = %q", path, got, detail.Rule)
	}
}

func TestNewResolver_RuleInheritanceComposition(t *testing.T) {
	home, parent, repo := inheritanceTree(t)
	writeInheritanceConfig(t, home, ProjectRule{Rules: []ProjectRuleEntry{
		{Path: "**/*.go", Rule: "GLOBAL POLICY"},
	}})
	writeInheritanceConfig(t, parent, ProjectRule{Rules: []ProjectRuleEntry{
		{Path: "**/*.go", Rule: "PARENT POLICY", MergeParentRules: true},
		{Path: "src/**/*.go", Rule: "UNUSED PARENT POLICY"},
	}})
	writeInheritanceConfig(t, repo, ProjectRule{Rules: []ProjectRuleEntry{
		{Path: "**/*.go", Rule: "LOCAL POLICY", MergeParentRules: true},
		{Path: "src/**/*.go", Rule: "UNUSED LOCAL POLICY"},
	}})
	custom := writeInheritanceConfig(t, t.TempDir(), ProjectRule{Rules: []ProjectRuleEntry{
		{Path: "**/*.go", Rule: ""},
		{Path: "SRC/**/*.GO", Rule: "CUSTOM POLICY", MergeParentRules: true},
		{Path: "src/main.go", Rule: "UNUSED CUSTOM POLICY"},
	}})
	resolver, _ := inheritanceResolver(t, repo, custom)
	want := strings.Join([]string{"GLOBAL POLICY", "PARENT POLICY", "LOCAL POLICY", "CUSTOM POLICY"}, "\n\n---\n\n")
	assertInheritanceDetail(t, resolver, "src/main.go", want, "custom", "SRC/**/*.GO")
	detail := resolver.(DetailResolver).ResolveDetail("README.md")
	if detail.Source != "system" || detail.Rule == "" || detail.Rule != resolver.Resolve("README.md") {
		t.Fatalf("unmatched entries must retain system fallback: %+v", detail)
	}
}

func TestNewResolver_RuleInheritanceTraversal(t *testing.T) {
	cases := []struct {
		name   string
		local  []ProjectRuleEntry
		parent []ProjectRuleEntry
		custom []ProjectRuleEntry
		want   string
		source string
	}{
		{
			name:   "unmatched parent does not stop inheritance",
			local:  []ProjectRuleEntry{{Path: "**/*.go", Rule: "LOCAL", MergeParentRules: true}},
			parent: []ProjectRuleEntry{{Path: "**/*.py", Rule: "PARENT"}},
			want:   "GLOBAL\n\n---\n\nLOCAL", source: "project",
		},
		{
			name:   "parent replacement stops inheritance",
			local:  []ProjectRuleEntry{{Path: "**/*.go", Rule: "LOCAL", MergeParentRules: true}},
			parent: []ProjectRuleEntry{{Path: "**/*.go", Rule: "PARENT"}},
			want:   "PARENT\n\n---\n\nLOCAL", source: "project",
		},
		{
			name:   "local replacement stops inheritance",
			local:  []ProjectRuleEntry{{Path: "**/*.go", Rule: "LOCAL"}},
			parent: []ProjectRuleEntry{{Path: "**/*.go", Rule: "PARENT", MergeParentRules: true}},
			want:   "LOCAL", source: "project",
		},
		{
			name:   "empty entry can inherit",
			local:  []ProjectRuleEntry{{Path: "**/*.go", MergeParentRules: true}, {Path: "**/*.go", Rule: "UNUSED"}},
			parent: []ProjectRuleEntry{{Path: "**/*.go", Rule: "PARENT", MergeParentRules: true}},
			want:   "GLOBAL\n\n---\n\nPARENT", source: "project",
		},
		{
			name:  "empty parent config does not stop inheritance",
			local: []ProjectRuleEntry{{Path: "**/*.go", Rule: "LOCAL", MergeParentRules: true}},
			want:  "GLOBAL\n\n---\n\nLOCAL", source: "project",
		},
		{
			name:   "unmatched local uses legacy global fallback",
			local:  []ProjectRuleEntry{{Path: "**/*.py", Rule: "LOCAL", MergeParentRules: true}},
			parent: []ProjectRuleEntry{{Path: "**/*.go", Rule: "PARENT"}},
			want:   "GLOBAL", source: "global",
		},
		{
			name:   "empty local uses legacy global fallback",
			parent: []ProjectRuleEntry{{Path: "**/*.go", Rule: "PARENT"}},
			want:   "GLOBAL", source: "global",
		},
		{
			name:   "custom inheritance crosses unmatched local",
			custom: []ProjectRuleEntry{{Path: "**/*.go", Rule: "CUSTOM", MergeParentRules: true}},
			local:  []ProjectRuleEntry{{Path: "**/*.py", Rule: "LOCAL"}},
			parent: []ProjectRuleEntry{{Path: "**/*.go", Rule: "PARENT", MergeParentRules: true}},
			want:   "GLOBAL\n\n---\n\nPARENT\n\n---\n\nCUSTOM", source: "custom",
		},
		{
			name:   "unmatched custom preserves legacy project fallback",
			custom: []ProjectRuleEntry{{Path: "**/*.py", Rule: "CUSTOM", MergeParentRules: true}},
			local:  []ProjectRuleEntry{{Path: "**/*.py", Rule: "LOCAL"}},
			parent: []ProjectRuleEntry{{Path: "**/*.go", Rule: "PARENT"}},
			want:   "GLOBAL", source: "global",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home, parent, repo := inheritanceTree(t)
			writeInheritanceConfig(t, home, ProjectRule{Rules: []ProjectRuleEntry{{Path: "**/*.go", Rule: "GLOBAL"}}})
			writeInheritanceConfig(t, parent, ProjectRule{Rules: tc.parent})
			writeInheritanceConfig(t, repo, ProjectRule{Rules: tc.local})
			custom := ""
			if tc.custom != nil {
				custom = writeInheritanceConfig(t, t.TempDir(), ProjectRule{Rules: tc.custom})
			}
			resolver, _ := inheritanceResolver(t, repo, custom)
			assertInheritanceDetail(t, resolver, "src/main.go", tc.want, tc.source, "**/*.go")
		})
	}
}

func TestNewResolver_InheritanceSystemRule(t *testing.T) {
	for _, flag := range []string{"none", "global", "parent", "local", "custom", "all", "unmatched"} {
		t.Run(flag, func(t *testing.T) {
			home, parent, repo := inheritanceTree(t)
			config := func(name string, mergeParent bool) ProjectRule {
				return ProjectRule{Rules: []ProjectRuleEntry{
					{Path: "**/*.py", Rule: "UNUSED", MergeSystemRule: flag == "unmatched"},
					{Path: "**/*.go", Rule: strings.ToUpper(name), MergeParentRules: mergeParent, MergeSystemRule: flag == name || flag == "all"},
				}}
			}
			writeInheritanceConfig(t, home, config("global", false))
			writeInheritanceConfig(t, parent, config("parent", true))
			writeInheritanceConfig(t, repo, config("local", true))
			custom := writeInheritanceConfig(t, t.TempDir(), config("custom", true))
			resolver, _ := inheritanceResolver(t, repo, custom)
			want := "GLOBAL\n\n---\n\nPARENT\n\n---\n\nLOCAL\n\n---\n\nCUSTOM"
			if flag != "none" && flag != "unmatched" {
				system, err := LoadDefault()
				if err != nil {
					t.Fatal(err)
				}
				systemText := system.Resolve("src/main.go")
				if got := strings.Count(resolver.Resolve("src/main.go"), systemText); got != 1 {
					t.Fatalf("system rule count = %d, want 1", got)
				}
			}
			detail := resolver.(DetailResolver).ResolveDetail("src/main.go")
			if !strings.Contains(detail.Rule, want) || detail.Source != "custom" || detail.Pattern != "**/*.go" {
				t.Fatalf("unexpected inherited rule: %+v", detail)
			}
			if resolver.Resolve("src/main.go") != detail.Rule {
				t.Fatal("Resolve and ResolveDetail disagree")
			}
			if flag == "none" || flag == "unmatched" {
				assertInheritanceDetail(t, resolver, "src/main.go", want, "custom", "**/*.go")
			}
		})
	}

	t.Run("empty inheritance does not enable system rules", func(t *testing.T) {
		_, _, repo := inheritanceTree(t)
		writeInheritanceConfig(t, repo, ProjectRule{Rules: []ProjectRuleEntry{{Path: "**/*.go", MergeParentRules: true}}})
		resolver, _ := inheritanceResolver(t, repo, "")
		assertInheritanceDetail(t, resolver, "main.go", "", "project", "**/*.go")
	})

	t.Run("empty inheritance can enable system rules explicitly", func(t *testing.T) {
		_, _, repo := inheritanceTree(t)
		writeInheritanceConfig(t, repo, ProjectRule{Rules: []ProjectRuleEntry{{Path: "**/*.go", MergeParentRules: true, MergeSystemRule: true}}})
		resolver, _ := inheritanceResolver(t, repo, "")
		system, err := LoadDefault()
		if err != nil {
			t.Fatal(err)
		}
		assertInheritanceDetail(t, resolver, "main.go", system.Resolve("main.go"), "project", "**/*.go")
	})

	t.Run("empty system-only parent stops inheritance", func(t *testing.T) {
		home, parent, repo := inheritanceTree(t)
		writeInheritanceConfig(t, home, ProjectRule{Rules: []ProjectRuleEntry{{Path: "**/*.go", Rule: "UNUSED GLOBAL"}}})
		writeInheritanceConfig(t, parent, ProjectRule{Rules: []ProjectRuleEntry{{Path: "**/*.go", MergeSystemRule: true}}})
		writeInheritanceConfig(t, repo, ProjectRule{Rules: []ProjectRuleEntry{{Path: "**/*.go", Rule: "LOCAL", MergeParentRules: true}}})
		resolver, _ := inheritanceResolver(t, repo, "")
		system, err := LoadDefault()
		if err != nil {
			t.Fatal(err)
		}
		detail := resolver.(DetailResolver).ResolveDetail("main.go")
		if !strings.Contains(detail.Rule, system.Resolve("main.go")) || !strings.Contains(detail.Rule, "LOCAL") ||
			strings.Contains(detail.Rule, "UNUSED GLOBAL") || detail.Source != "project" || detail.Pattern != "**/*.go" {
			t.Fatalf("unexpected system-only replacement: %+v", detail)
		}
		if resolver.Resolve("main.go") != detail.Rule {
			t.Fatal("Resolve and ResolveDetail disagree")
		}
	})
}

func TestNewResolver_FilterInheritance(t *testing.T) {
	cases := []struct {
		name    string
		custom  *ProjectRule
		local   ProjectRule
		parent  ProjectRule
		include []string
		exclude []string
	}{
		{
			name:    "merge every layer and deduplicate case",
			custom:  &ProjectRule{FilterMode: "merge", Include: []string{"CUSTOM/**", "SRC/**"}, Exclude: []string{"CUSTOM/TMP/**", "**/CACHE/**"}},
			local:   ProjectRule{FilterMode: "merge", Include: []string{"LOCAL/**", "src/**"}, Exclude: []string{"LOCAL/TMP/**", "**/cache/**"}},
			parent:  ProjectRule{FilterMode: "merge", Include: []string{"PARENT/**", "Src/**"}, Exclude: []string{"PARENT/TMP/**"}},
			include: []string{"custom/**", "src/**", "local/**", "parent/**", "global/**"},
			exclude: []string{"custom/tmp/**", "**/cache/**", "local/tmp/**", "parent/tmp/**", "global/tmp/**"},
		},
		{
			name:    "empty merge enables ancestors",
			local:   ProjectRule{FilterMode: "merge"},
			parent:  ProjectRule{Include: []string{"PARENT/**"}},
			include: []string{"parent/**"},
		},
		{
			name:    "empty default parent is skipped during merge",
			local:   ProjectRule{FilterMode: "merge", Include: []string{"LOCAL/**"}},
			include: []string{"local/**", "global/**"}, exclude: []string{"global/tmp/**"},
		},
		{
			name:    "default parent with lists replaces global",
			local:   ProjectRule{FilterMode: "merge", Include: []string{"LOCAL/**"}},
			parent:  ProjectRule{Exclude: []string{"PARENT/**"}},
			include: []string{"local/**"}, exclude: []string{"parent/**"},
		},
		{
			name:    "explicit parent replace stops merge",
			local:   ProjectRule{FilterMode: "merge", Include: []string{"LOCAL/**"}},
			parent:  ProjectRule{FilterMode: "replace", Include: []string{"PARENT/**"}},
			include: []string{"local/**", "parent/**"},
		},
		{
			name:    "empty parent replace stops merge",
			local:   ProjectRule{FilterMode: "merge", Include: []string{"LOCAL/**"}},
			parent:  ProjectRule{FilterMode: "replace"},
			include: []string{"local/**"},
		},
		{
			name:   "empty local replace clears lower filters",
			local:  ProjectRule{FilterMode: "replace"},
			parent: ProjectRule{Include: []string{"PARENT/**"}},
		},
		{
			name:   "empty custom replace clears lower filters",
			custom: &ProjectRule{FilterMode: "replace"},
			local:  ProjectRule{Include: []string{"LOCAL/**"}},
			parent: ProjectRule{Include: []string{"PARENT/**"}},
		},
		{
			name:    "empty default local skips ancestors",
			parent:  ProjectRule{Include: []string{"PARENT/**"}},
			include: []string{"global/**"}, exclude: []string{"global/tmp/**"},
		},
		{
			name:    "default local replaces lower filters",
			local:   ProjectRule{Include: []string{"LOCAL/**"}},
			parent:  ProjectRule{Include: []string{"PARENT/**"}},
			include: []string{"local/**"},
		},
		{
			name:    "custom merge stops at default local filters",
			custom:  &ProjectRule{FilterMode: "merge", Include: []string{"CUSTOM/**"}},
			local:   ProjectRule{Include: []string{"LOCAL/**"}},
			parent:  ProjectRule{FilterMode: "merge", Include: []string{"PARENT/**"}},
			include: []string{"custom/**", "local/**"},
		},
		{
			name:    "empty custom merge crosses empty local boundary",
			custom:  &ProjectRule{FilterMode: "merge"},
			parent:  ProjectRule{FilterMode: "merge", Include: []string{"PARENT/**"}},
			include: []string{"parent/**", "global/**"}, exclude: []string{"global/tmp/**"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home, parent, repo := inheritanceTree(t)
			writeInheritanceConfig(t, home, ProjectRule{Include: []string{"GLOBAL/**"}, Exclude: []string{"GLOBAL/TMP/**"}})
			writeInheritanceConfig(t, parent, tc.parent)
			writeInheritanceConfig(t, repo, tc.local)
			custom := ""
			if tc.custom != nil {
				custom = writeInheritanceConfig(t, t.TempDir(), *tc.custom)
			}
			_, filter := inheritanceResolver(t, repo, custom)
			if filter == nil {
				if len(tc.include) != 0 || len(tc.exclude) != 0 {
					t.Fatalf("missing filter, want include %v and exclude %v", tc.include, tc.exclude)
				}
				return
			}
			if !slices.Equal(filter.Include, tc.include) || !slices.Equal(filter.Exclude, tc.exclude) {
				t.Fatalf("filter = %+v, want include %v and exclude %v", filter, tc.include, tc.exclude)
			}
			for _, pattern := range tc.include {
				path := strings.ReplaceAll(strings.ToUpper(pattern), "**", "MAIN.GO")
				if !filter.IsUserIncluded(path) {
					t.Errorf("effective include %q does not include %q", pattern, path)
				}
			}
			for _, pattern := range tc.exclude {
				path := strings.ReplaceAll(strings.ToUpper(pattern), "**", "CACHE")
				if !filter.IsUserExcluded(path) {
					t.Errorf("effective exclude %q does not exclude %q", pattern, path)
				}
			}
		})
	}
}

func TestNewResolver_HomeAncestorIsGlobalOnce(t *testing.T) {
	for _, symlinkHome := range []bool{false, true} {
		name := "direct home"
		if symlinkHome {
			name = "canonical home"
		}
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			homeEnv := home
			if symlinkHome {
				homeEnv = filepath.Join(t.TempDir(), "home")
				if err := os.Symlink(home, homeEnv); err != nil {
					t.Skipf("cannot create symlink: %v", err)
				}
			}
			setTestHome(t, homeEnv)
			parent := filepath.Join(home, "work")
			repo := filepath.Join(parent, "repo")
			writeInheritanceFile(t, filepath.Join(home, ".opencodereview", "shared.md"), "GLOBAL REFERENCE")
			writeInheritanceFile(t, filepath.Join(home, "shared.md"), "WRONG REFERENCE")
			writeInheritanceConfig(t, home, ProjectRule{Rules: []ProjectRuleEntry{{Path: "**/*.go", Rule: "shared.md", MergeParentRules: true}}})
			writeInheritanceConfig(t, parent, ProjectRule{Rules: []ProjectRuleEntry{{Path: "**/*.go", Rule: "PARENT", MergeParentRules: true}}})
			writeInheritanceConfig(t, repo, ProjectRule{Rules: []ProjectRuleEntry{{Path: "**/*.go", Rule: "LOCAL", MergeParentRules: true}}})
			resolver, _ := inheritanceResolver(t, repo, "")
			assertInheritanceDetail(t, resolver, "main.go", "GLOBAL REFERENCE\n\n---\n\nPARENT\n\n---\n\nLOCAL", "project", "**/*.go")
			projects, err := loadProjectRules(repo)
			if err != nil {
				t.Fatal(err)
			}
			if len(projects) != 2 || projects[0].Rules[0].Rule != "LOCAL" || projects[1].Rules[0].Rule != "PARENT" {
				t.Fatalf("project configs must exclude the home config: %+v", projects)
			}
		})
	}
}

func TestNewResolver_InheritedReferences(t *testing.T) {
	t.Run("each project resolves its own references", func(t *testing.T) {
		_, parent, repo := inheritanceTree(t)
		writeInheritanceFile(t, filepath.Join(parent, "docs", "shared.md"), "PARENT REFERENCE")
		writeInheritanceFile(t, filepath.Join(repo, "docs", "shared.md"), "LOCAL REFERENCE")
		writeInheritanceConfig(t, parent, ProjectRule{Rules: []ProjectRuleEntry{{Path: "**/*.go", Rule: "docs/shared.md"}}})
		writeInheritanceConfig(t, repo, ProjectRule{Rules: []ProjectRuleEntry{{Path: "**/*.go", Rule: "docs/shared.md", MergeParentRules: true}}})
		resolver, _ := inheritanceResolver(t, repo, "")
		assertInheritanceDetail(t, resolver, "main.go", "PARENT REFERENCE\n\n---\n\nLOCAL REFERENCE", "project", "**/*.go")
	})

	for _, escape := range []string{"relative", "absolute", "symlink"} {
		t.Run("each project confines "+escape+" references", func(t *testing.T) {
			home, parent, repo := inheritanceTree(t)
			outside := filepath.Join(filepath.Dir(parent), "outside.md")
			writeInheritanceFile(t, outside, "SECRET CONTENT")
			writeInheritanceConfig(t, home, ProjectRule{Rules: []ProjectRuleEntry{{Path: "**/*.go", Rule: "GLOBAL"}}})
			for _, dir := range []string{parent, repo} {
				reference := outside
				switch escape {
				case "relative":
					var err error
					reference, err = filepath.Rel(dir, outside)
					if err != nil {
						t.Fatal(err)
					}
				case "symlink":
					reference = "escape.md"
					if err := os.Symlink(outside, filepath.Join(dir, reference)); err != nil {
						t.Skipf("cannot create symlink: %v", err)
					}
				}
				writeInheritanceConfig(t, dir, ProjectRule{Rules: []ProjectRuleEntry{{Path: "**/*.go", Rule: reference, MergeParentRules: true}}})
			}
			resolver, _ := inheritanceResolver(t, repo, "")
			assertInheritanceDetail(t, resolver, "main.go", "GLOBAL", "project", "**/*.go")
		})
	}

	t.Run("custom relative reference uses its config directory", func(t *testing.T) {
		_, _, repo := inheritanceTree(t)
		writeInheritanceConfig(t, repo, ProjectRule{Rules: []ProjectRuleEntry{{Path: "**/*.go", Rule: "LOCAL"}}})
		customDir := t.TempDir()
		writeInheritanceFile(t, filepath.Join(customDir, ".opencodereview", "shared.md"), "CUSTOM REFERENCE")
		custom := writeInheritanceConfig(t, customDir, ProjectRule{Rules: []ProjectRuleEntry{{Path: "**/*.go", Rule: "shared.md", MergeParentRules: true}}})
		resolver, _ := inheritanceResolver(t, repo, custom)
		assertInheritanceDetail(t, resolver, "main.go", "LOCAL\n\n---\n\nCUSTOM REFERENCE", "custom", "**/*.go")
	})
}

func TestNewResolver_RejectsInvalidFilterMode(t *testing.T) {
	for _, layer := range []string{"global", "parent", "project", "custom"} {
		t.Run(layer, func(t *testing.T) {
			home, parent, repo := inheritanceTree(t)
			custom := ""
			config := ProjectRule{FilterMode: "invalid"}
			switch layer {
			case "global":
				writeInheritanceConfig(t, home, config)
			case "parent":
				writeInheritanceConfig(t, parent, config)
				writeInheritanceConfig(t, repo, ProjectRule{})
			case "project":
				writeInheritanceConfig(t, repo, config)
			case "custom":
				custom = writeInheritanceConfig(t, t.TempDir(), config)
			}
			if _, _, err := NewResolver(repo, custom, ResolverOptions{}); err == nil || !strings.Contains(err.Error(), "filter_mode") {
				t.Fatalf("invalid filter_mode must return a config error, got %v", err)
			}
		})
	}
}

func TestNewResolver_InheritanceCanonicalConfig(t *testing.T) {
	canonical := func(t *testing.T, repo string) []string {
		t.Helper()
		resolver, _ := inheritanceResolver(t, repo, "")
		return resolver.(canonicalConfigurer).CanonicalConfig()
	}

	t.Run("merge flag changes canonical config", func(t *testing.T) {
		_, _, repo := inheritanceTree(t)
		config := ProjectRule{Rules: []ProjectRuleEntry{{Path: "**/*.go", Rule: "LOCAL"}}}
		writeInheritanceConfig(t, repo, config)
		before := canonical(t, repo)
		config.Rules[0].MergeParentRules = true
		writeInheritanceConfig(t, repo, config)
		if reflect.DeepEqual(before, canonical(t, repo)) {
			t.Fatal("merge_parent_rules does not change canonical config")
		}
	})

	for _, location := range []string{"nearest", "ancestor"} {
		t.Run("empty "+location+" boundary changes canonical config", func(t *testing.T) {
			_, parent, repo := inheritanceTree(t)
			before := canonical(t, repo)
			dir := repo
			if location == "ancestor" {
				dir = parent
			}
			writeInheritanceConfig(t, dir, ProjectRule{})
			after := canonical(t, repo)
			if reflect.DeepEqual(before, after) {
				t.Fatal("empty project boundary does not change canonical config")
			}
			if !reflect.DeepEqual(after, canonical(t, repo)) {
				t.Fatal("canonical config is not deterministic")
			}
		})
	}

	t.Run("empty nearest boundary preserves ancestor entries", func(t *testing.T) {
		_, parent, repo := inheritanceTree(t)
		writeInheritanceConfig(t, repo, ProjectRule{})
		config := ProjectRule{Rules: []ProjectRuleEntry{{Path: "**/*.go", Rule: "PARENT ONE"}}}
		writeInheritanceConfig(t, parent, config)
		before := canonical(t, repo)
		config.Rules[0].Rule = "PARENT TWO"
		writeInheritanceConfig(t, parent, config)
		if reflect.DeepEqual(before, canonical(t, repo)) {
			t.Fatal("ancestor entry does not change canonical config")
		}
	})

	t.Run("layer order changes canonical config", func(t *testing.T) {
		_, parent, repo := inheritanceTree(t)
		first := ProjectRule{Rules: []ProjectRuleEntry{{Path: "**/*.go", Rule: "FIRST", MergeParentRules: true}}}
		second := ProjectRule{Rules: []ProjectRuleEntry{{Path: "**/*.go", Rule: "SECOND", MergeParentRules: true}}}
		writeInheritanceConfig(t, parent, first)
		writeInheritanceConfig(t, repo, second)
		before := canonical(t, repo)
		writeInheritanceConfig(t, parent, second)
		writeInheritanceConfig(t, repo, first)
		if reflect.DeepEqual(before, canonical(t, repo)) {
			t.Fatal("layer order does not change canonical config")
		}
	})

	t.Run("effective filters do not enter rule canonical config", func(t *testing.T) {
		_, _, repo := inheritanceTree(t)
		config := ProjectRule{Rules: []ProjectRuleEntry{{Path: "**/*.go", Rule: "LOCAL"}}}
		writeInheritanceConfig(t, repo, config)
		before := canonical(t, repo)
		config.FilterMode = "replace"
		config.Include = []string{"src/**"}
		config.Exclude = []string{"src/generated/**"}
		writeInheritanceConfig(t, repo, config)
		if !reflect.DeepEqual(before, canonical(t, repo)) {
			t.Fatal("filters change rule canonical config")
		}
	})
}

func TestNewResolver_MultipleProjectAncestors(t *testing.T) {
	for _, emptyParent := range []bool{false, true} {
		name := "all ancestors participate"
		if emptyParent {
			name = "empty ancestor retains a layer boundary"
		}
		t.Run(name, func(t *testing.T) {
			home, parent, repo := inheritanceTree(t)
			grandparent := filepath.Dir(parent)
			writeInheritanceConfig(t, home, ProjectRule{Rules: []ProjectRuleEntry{{Path: "**/*.go", Rule: "GLOBAL"}}})
			writeInheritanceConfig(t, grandparent, ProjectRule{Rules: []ProjectRuleEntry{{Path: "**/*.go", Rule: "GRANDPARENT", MergeParentRules: true}}})
			parentConfig := ProjectRule{}
			if !emptyParent {
				parentConfig.Rules = []ProjectRuleEntry{{Path: "**/*.go", Rule: "PARENT", MergeParentRules: true}}
			}
			writeInheritanceConfig(t, parent, parentConfig)
			writeInheritanceConfig(t, repo, ProjectRule{Rules: []ProjectRuleEntry{{Path: "**/*.go", Rule: "LOCAL", MergeParentRules: true}}})
			resolver, _ := inheritanceResolver(t, repo, "")
			want := "GLOBAL\n\n---\n\nGRANDPARENT\n\n---\n\n"
			if !emptyParent {
				want += "PARENT\n\n---\n\n"
			}
			assertInheritanceDetail(t, resolver, "main.go", want+"LOCAL", "project", "**/*.go")
			projects, err := loadProjectRules(repo)
			if err != nil {
				t.Fatal(err)
			}
			if len(projects) != 3 {
				t.Fatalf("project config count = %d, want 3", len(projects))
			}
			if projects[0].Rules[0].Rule != "LOCAL" || projects[2].Rules[0].Rule != "GRANDPARENT" {
				t.Fatalf("project configs do not preserve nearest-first order: %+v", projects)
			}
			if !reflect.DeepEqual(projects[1].Rules, parentConfig.Rules) {
				t.Fatalf("parent boundary = %+v, want %+v", projects[1], parentConfig)
			}
		})
	}
}
