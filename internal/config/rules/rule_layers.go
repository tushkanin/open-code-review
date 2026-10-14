// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alibaba/open-code-review/internal/pathutil"
)

type ruleLayer struct {
	rule     *ProjectRule
	source   string
	fallback int
}

func buildRuleLayers(custom *ProjectRule, projects []*ProjectRule, global *ProjectRule) []ruleLayer {
	count := len(projects)
	if custom != nil {
		count++
	}
	if global != nil {
		count++
	}
	if count == 0 {
		return nil
	}

	layers := make([]ruleLayer, 0, count)
	if custom != nil {
		layers = append(layers, ruleLayer{rule: custom, source: "custom", fallback: 1})
	}
	projectStart := len(layers)
	for _, project := range projects {
		if project != nil {
			layers = append(layers, ruleLayer{rule: project, source: "project"})
		}
	}
	globalIndex := len(layers)
	for i := projectStart; i < globalIndex; i++ {
		// A no-match lookup skips ancestors unless inheritance is active.
		layers[i].fallback = globalIndex
	}
	if global != nil {
		layers = append(layers, ruleLayer{rule: global, source: "global", fallback: globalIndex + 1})
	}
	return layers
}

func loadProjectRules(repoDir string) ([]*ProjectRule, error) {
	root, err := pathutil.CanonicalPath(repoDir)
	if err != nil {
		return nil, fmt.Errorf("resolve project directory %s: %w", repoDir, err)
	}

	var home string
	if userHome, err := os.UserHomeDir(); err == nil {
		home, _ = pathutil.CanonicalPath(userHome)
	}

	var projects []*ProjectRule
	for dir := root; ; {
		// The global loader supplies HOME, but configs above HOME remain project layers.
		if dir != home {
			project, err := loadProjectRuleAt(dir)
			if err != nil {
				return nil, err
			}
			if project != nil {
				projects = append(projects, project)
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return projects, nil
}

func buildFileFilter(layers []ruleLayer) *FileFilter {
	for i := 0; i < len(layers); i = layers[i].fallback {
		config := layers[i].rule
		if config.FilterMode == "" && len(config.Include) == 0 && len(config.Exclude) == 0 {
			continue
		}
		if config.FilterMode != "merge" {
			if len(config.Include) == 0 && len(config.Exclude) == 0 {
				return nil
			}
			return &FileFilter{
				Include: normalizeFilterPatterns(config.Include),
				Exclude: normalizeFilterPatterns(config.Exclude),
			}
		}

		var filter FileFilter
		var includeSeen, excludeSeen map[string]struct{}
		for ; i < len(layers); i++ {
			config = layers[i].rule
			if config.FilterMode == "" && len(config.Include) == 0 && len(config.Exclude) == 0 {
				continue
			}
			if includeSeen == nil && len(config.Include) > 0 {
				includeSeen = make(map[string]struct{}, len(config.Include))
			}
			if excludeSeen == nil && len(config.Exclude) > 0 {
				excludeSeen = make(map[string]struct{}, len(config.Exclude))
			}
			filter.Include = appendFilterPatterns(filter.Include, config.Include, includeSeen)
			filter.Exclude = appendFilterPatterns(filter.Exclude, config.Exclude, excludeSeen)
			if config.FilterMode != "merge" {
				break
			}
		}
		if len(filter.Include) == 0 && len(filter.Exclude) == 0 {
			return nil
		}
		return &filter
	}
	return nil
}

func normalizeFilterPatterns(patterns []string) []string {
	if len(patterns) == 0 {
		return nil
	}
	if len(patterns) == 1 {
		pattern := strings.ToLower(patterns[0])
		if pattern == patterns[0] {
			return patterns
		}
		return []string{pattern}
	}

	seen := make(map[string]struct{}, len(patterns))
	var normalized []string
	for i, pattern := range patterns {
		lower := strings.ToLower(pattern)
		_, duplicate := seen[lower]
		if normalized == nil && (duplicate || lower != pattern) {
			normalized = make([]string, 0, len(patterns))
			normalized = append(normalized, patterns[:i]...)
		}
		if duplicate {
			continue
		}
		seen[lower] = struct{}{}
		if normalized != nil {
			normalized = append(normalized, lower)
		}
	}
	// Unchanged config slices need no copy because filters do not mutate them.
	if normalized == nil {
		return patterns
	}
	return normalized
}

func appendFilterPatterns(target, patterns []string, seen map[string]struct{}) []string {
	for _, pattern := range patterns {
		lower := strings.ToLower(pattern)
		if _, duplicate := seen[lower]; duplicate {
			continue
		}
		seen[lower] = struct{}{}
		target = append(target, lower)
	}
	return target
}

func validateRuleConfig(config *ProjectRule) error {
	switch config.FilterMode {
	case "", "merge", "replace":
		return nil
	default:
		return fmt.Errorf("invalid filter_mode %q: expected merge or replace", config.FilterMode)
	}
}
