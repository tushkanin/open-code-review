// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package rules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNewResolver_RuleArraySources(t *testing.T) {
	for _, layer := range []string{"project", "ancestor", "global", "custom"} {
		t.Run(layer, func(t *testing.T) {
			home, parent, repo := inheritanceTree(t)
			dir, base, source := repo, repo, "project"
			switch layer {
			case "ancestor":
				dir, base = parent, parent
			case "global":
				dir, base, source = home, filepath.Join(home, ".opencodereview"), "global"
			case "custom":
				dir = t.TempDir()
				base, source = filepath.Join(dir, ".opencodereview"), "custom"
			}
			writeInheritanceFile(t, filepath.Join(base, "account.md"), "Check account isolation.\n")
			writeInheritanceFile(t, filepath.Join(base, "retry.txt"), "Check retry safety.\n")
			path := filepath.Join(dir, ".opencodereview", "rule.json")
			writeInheritanceFile(t, path, `{"rules":[
				{"path":"**/*.go","rule":["account.md","Check field access.","retry.txt"]},
				{"path":"**/*.go","rule":"UNUSED RULE"}
			]}`)
			custom := ""
			if layer == "custom" {
				custom = path
			}
			resolver, _ := inheritanceResolver(t, repo, custom)
			assertInheritanceDetail(t, resolver, "src/main.go",
				"Check account isolation.\n\n---\n\nCheck field access.\n\n---\n\nCheck retry safety.", source, "**/*.go")
		})
	}
}

func TestNewResolver_RuleArrayEmptyAndUnavailableSources(t *testing.T) {
	for _, value := range []string{`[]`, `["", "  "]`, `["missing.md"]`, `["", "missing.md", ""]`} {
		t.Run(value, func(t *testing.T) {
			_, _, repo := inheritanceTree(t)
			writeInheritanceFile(t, filepath.Join(repo, ".opencodereview", "rule.json"),
				`{"rules":[{"path":"**/*.go","rule":`+value+`},{"path":"**/*.go","rule":"FALLBACK RULE"}]}`)
			resolver, _ := inheritanceResolver(t, repo, "")
			assertInheritanceDetail(t, resolver, "main.go", "FALLBACK RULE", "project", "**/*.go")
		})
	}
	t.Run("valid sources survive missing sources", func(t *testing.T) {
		_, _, repo := inheritanceTree(t)
		writeInheritanceFile(t, filepath.Join(repo, ".opencodereview", "rule.json"),
			`{"rules":[{"path":"**/*.go","rule":["Check access.","missing.md","","Check retries."]}]}`)
		resolver, _ := inheritanceResolver(t, repo, "")
		assertInheritanceDetail(t, resolver, "main.go", "Check access.\n\n---\n\nCheck retries.", "project", "**/*.go")
	})
	t.Run("file content does not become another reference", func(t *testing.T) {
		_, _, repo := inheritanceTree(t)
		writeInheritanceFile(t, filepath.Join(repo, "outer.md"), "inner.md")
		writeInheritanceFile(t, filepath.Join(repo, "inner.md"), "WRONG CONTENT")
		writeInheritanceFile(t, filepath.Join(repo, ".opencodereview", "rule.json"),
			`{"rules":[{"path":"**/*.go","rule":["outer.md"]}]}`)
		resolver, _ := inheritanceResolver(t, repo, "")
		assertInheritanceDetail(t, resolver, "main.go", "inner.md", "project", "**/*.go")
	})
}

func TestNewResolver_RuleArrayInheritance(t *testing.T) {
	home, parent, repo := inheritanceTree(t)
	writeInheritanceFile(t, filepath.Join(home, ".opencodereview", "rule.json"),
		`{"rules":[{"path":"**/*.go","rule":["GLOBAL FIRST","GLOBAL SECOND"],"merge_system_rule":true}]}`)
	writeInheritanceFile(t, filepath.Join(parent, ".opencodereview", "rule.json"),
		`{"rules":[{"path":"**/*.go","rule":["PARENT FIRST","PARENT SECOND"],"merge_parent_rules":true}]}`)
	writeInheritanceFile(t, filepath.Join(repo, ".opencodereview", "rule.json"),
		`{"rules":[{"path":"**/*.go","rule":["LOCAL FIRST","LOCAL SECOND"],"merge_parent_rules":true,"merge_system_rule":true}]}`)
	resolver, _ := inheritanceResolver(t, repo, "")
	detail := resolver.(DetailResolver).ResolveDetail("main.go")
	if detail.Source != "project" || detail.Pattern != "**/*.go" || resolver.Resolve("main.go") != detail.Rule {
		t.Fatalf("unexpected inherited array rule: %+v", detail)
	}
	offset := 0
	for _, text := range []string{"GLOBAL FIRST", "GLOBAL SECOND", "PARENT FIRST", "PARENT SECOND", "LOCAL FIRST", "LOCAL SECOND"} {
		index := strings.Index(detail.Rule[offset:], text)
		if index < 0 || strings.Count(detail.Rule, text) != 1 {
			t.Fatalf("missing, repeated, or unordered source %q in %q", text, detail.Rule)
		}
		offset += index + len(text)
	}
	system, err := LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(detail.Rule, system.Resolve("main.go")) != 1 {
		t.Fatal("system rule must appear once")
	}

	writeInheritanceFile(t, filepath.Join(repo, ".opencodereview", "rule.json"),
		`{"rules":[{"path":"**/*.go","rule":[],"merge_parent_rules":true}]}`)
	resolver, _ = inheritanceResolver(t, repo, "")
	if rule := resolver.Resolve("main.go"); !strings.Contains(rule, "GLOBAL FIRST") || !strings.Contains(rule, "PARENT SECOND") || strings.Contains(rule, "LOCAL FIRST") {
		t.Fatalf("empty array must retain explicit inheritance: %q", rule)
	}
}

func TestNewResolver_RuleArrayReferenceConfinement(t *testing.T) {
	for _, kind := range []string{"relative", "absolute", "symlink", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			_, parent, repo := inheritanceTree(t)
			outside := filepath.Join(parent, "outside.md")
			writeInheritanceFile(t, outside, "FORBIDDEN CONTENT")
			reference := outside
			switch kind {
			case "relative":
				reference = "../outside.md"
			case "symlink":
				reference = "escape.md"
				if err := os.Symlink(outside, filepath.Join(repo, reference)); err != nil {
					t.Skipf("cannot create symlink: %v", err)
				}
			case "oversized":
				reference = "large.md"
				writeInheritanceFile(t, filepath.Join(repo, reference), strings.Repeat("X", 513*1024))
			}
			value, err := json.Marshal([]string{reference, "Check safe behavior."})
			if err != nil {
				t.Fatal(err)
			}
			writeInheritanceFile(t, filepath.Join(repo, ".opencodereview", "rule.json"),
				`{"rules":[{"path":"**/*.go","rule":`+string(value)+`}]}`)
			resolver, _ := inheritanceResolver(t, repo, "")
			assertInheritanceDetail(t, resolver, "main.go", "Check safe behavior.", "project", "**/*.go")
		})
	}
}

func TestNewResolver_RuleArrayInvalidTypes(t *testing.T) {
	for _, value := range []string{`42`, `true`, `{}`, `[42]`, `[true]`, `[null]`, `[["nested"]]`, `["Valid text", {}]`} {
		t.Run(value, func(t *testing.T) {
			_, _, repo := inheritanceTree(t)
			writeInheritanceFile(t, filepath.Join(repo, ".opencodereview", "rule.json"),
				`{"rules":[{"path":"**/*.go","rule":`+value+`}]}`)
			if _, _, err := NewResolver(repo, "", ResolverOptions{}); err == nil {
				t.Fatal("invalid rule source type must return a config error")
			}
		})
	}
}

func TestNewResolver_RuleArrayCanonicalConfig(t *testing.T) {
	_, _, repo := inheritanceTree(t)
	path := filepath.Join(repo, ".opencodereview", "rule.json")
	writeInheritanceFile(t, filepath.Join(repo, "policy.md"), "Check original policy.")
	writeInheritanceFile(t, path, `{"rules":[{"path":"**/*.go","rule":["policy.md","Check second policy."]}]}`)
	canonical := func() []string {
		resolver, _ := inheritanceResolver(t, repo, "")
		return resolver.(canonicalConfigurer).CanonicalConfig()
	}
	before := canonical()
	writeInheritanceFile(t, filepath.Join(repo, "policy.md"), "Check revised policy.")
	if reflect.DeepEqual(before, canonical()) {
		t.Fatal("source content does not affect rule config hash input")
	}
	before = canonical()
	writeInheritanceFile(t, path, `{"rules":[{"path":"**/*.go","rule":["Check second policy.","policy.md"]}]}`)
	if reflect.DeepEqual(before, canonical()) {
		t.Fatal("source order does not affect rule config hash input")
	}
}

func TestProjectRuleEntry_RuleArrayRoundTrip(t *testing.T) {
	for _, value := range []string{`"Check access."`, `["first.md","Check retries."]`, `[]`} {
		t.Run(value, func(t *testing.T) {
			input := `{"path":"**/*.go","rule":` + value + `,"merge_parent_rules":true,"merge_system_rule":true}`
			var entry ProjectRuleEntry
			if err := json.Unmarshal([]byte(input), &entry); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(entry)
			if err != nil {
				t.Fatal(err)
			}
			var before, after map[string]any
			if err := json.Unmarshal([]byte(input), &before); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(encoded, &after); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("rule config loses source values or merge flags: %s", encoded)
			}
		})
	}
}
