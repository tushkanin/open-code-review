// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

// Package rules loads system review rules and matches file paths against glob patterns.
package rules

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/alibaba/open-code-review/internal/gitcmd"
	"github.com/alibaba/open-code-review/internal/pathutil"
)

// Resolver resolves a review rule for a file path.
type Resolver interface {
	Resolve(path string) string
}

// PathRule is a single pattern→rule entry preserving declaration order.
type PathRule struct {
	Pattern string
	Rule    string
}

// SystemRule holds review rules loaded from an external JSON config.
type SystemRule struct {
	DefaultRule string     `json:"default_rule"`
	PathRules   []PathRule // ordered; first match wins
}

// UnmarshalJSON preserves the key order from JSON's path_rule_map object.
func (r *SystemRule) UnmarshalJSON(data []byte) error {
	// Decode default_rule normally.
	var wrapper struct {
		DefaultRule string `json:"default_rule"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return err
	}
	r.DefaultRule = wrapper.DefaultRule

	// Use json.Decoder with UseNumber to preserve order of path_rule_map keys.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	mapData, ok := raw["path_rule_map"]
	if !ok || len(mapData) == 0 || string(mapData) == "null" {
		return nil
	}

	// Parse ordered keys using a streaming decoder.
	dec := json.NewDecoder(strings.NewReader(string(mapData)))
	// Read opening '{'
	t, err := dec.Token()
	if err != nil {
		return fmt.Errorf("expected '{' in path_rule_map: %w", err)
	}
	if t != json.Delim('{') {
		return fmt.Errorf("expected '{' in path_rule_map, got %v", t)
	}
	for dec.More() {
		// Read key
		keyTok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("read path_rule_map key: %w", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return fmt.Errorf("expected string key in path_rule_map, got %T", keyTok)
		}
		// Read value
		var value string
		if err := dec.Decode(&value); err != nil {
			return fmt.Errorf("read path_rule_map value for %q: %w", key, err)
		}
		r.PathRules = append(r.PathRules, PathRule{Pattern: key, Rule: value})
	}
	return nil
}

//go:embed system_rules.json rule_docs/*
var rulesFS embed.FS

// LoadDefault parses the embedded system_rules.json and resolves rule file references.
func LoadDefault() (*SystemRule, error) {
	data, err := rulesFS.ReadFile("system_rules.json")
	if err != nil {
		return nil, fmt.Errorf("read embedded system_rules.json: %w", err)
	}
	var rule SystemRule
	if err := json.Unmarshal(data, &rule); err != nil {
		return nil, fmt.Errorf("unmarshal default system rules: %w", err)
	}
	content, err := rulesFS.ReadFile("rule_docs/" + rule.DefaultRule)
	if err != nil {
		return nil, fmt.Errorf("read default rule file %q: %w", rule.DefaultRule, err)
	}
	rule.DefaultRule = strings.TrimRight(string(content), "\n")
	for i := range rule.PathRules {
		content, err := rulesFS.ReadFile("rule_docs/" + rule.PathRules[i].Rule)
		if err != nil {
			return nil, fmt.Errorf("read rule file %q for pattern %q: %w", rule.PathRules[i].Rule, rule.PathRules[i].Pattern, err)
		}
		rule.PathRules[i].Rule = strings.TrimRight(string(content), "\n")
	}
	return &rule, nil
}

// loadObjCRule reads the embedded Objective-C rule doc used by the ".m"
// content sniff.
func loadObjCRule() (string, error) {
	content, err := rulesFS.ReadFile("rule_docs/objc.md")
	if err != nil {
		return "", fmt.Errorf("read objc rule file: %w", err)
	}
	return strings.TrimRight(string(content), "\n"), nil
}

// RuleDetail contains the resolved rule along with metadata about its source.
type RuleDetail struct {
	Rule    string // rule text
	Source  string // "custom" | "project" | "global" | "system"
	Pattern string // glob pattern that matched, or "default" for fallback — always a plain glob, never annotated
	// SniffedAs is "" for a plain path match, or the sniffed language (e.g.
	// "objc") when content sniffing overrode the path-based rule. Internal
	// only: callers that serialize RuleDetail (e.g. delegateRuleGroupJSON)
	// must not surface this, since Pattern is a versioned "the glob that
	// matched" contract that a sniff annotation would silently break.
	SniffedAs string `json:"-"`
}

// DetailResolver extends Resolver with source metadata.
type DetailResolver interface {
	ResolveDetail(path string) RuleDetail
}

// Resolve returns the rule text for a given file path.
// Patterns with brace expansion like "*.{go,py}" are expanded into "*.go", "*.py".
// The first match wins; if none match, it falls back to DefaultRule.
// Supports full glob syntax including ** for recursive directory matching.
func (r *SystemRule) Resolve(path string) string {
	return r.resolveDetail(path).Rule
}

// CanonicalConfig returns a deterministic, order-stable field list describing this
// rule set's effective rule-text configuration, for hashing into the run manifest's
// rule_config_sha256. It covers only rule-text resolution (default plus ordered
// pattern rules); include/exclude filtering is carried by FileFilter and hashed
// separately. Order is preserved because first match wins.
func (r *SystemRule) CanonicalConfig() []string {
	fields := []string{"layer", "system", "default", r.DefaultRule}
	for _, pr := range r.PathRules {
		fields = append(fields, "layer", "system", "pattern", pr.Pattern, "rule", pr.Rule)
	}
	return fields
}

func (r *SystemRule) resolveDetail(path string) RuleDetail {
	lowerPath := strings.ToLower(path)
	for _, pr := range r.PathRules {
		expanded := expandBraces(pr.Pattern)
		for _, p := range expanded {
			if matched, _ := doublestar.Match(strings.ToLower(p), lowerPath); matched {
				return RuleDetail{Rule: pr.Rule, Source: "system", Pattern: pr.Pattern}
			}
		}
	}
	return RuleDetail{Rule: r.DefaultRule, Source: "system", Pattern: "default"}
}

// expandBraces turns "{a,b,c}" style patterns into individual strings.
// e.g. "*.go.{java,kotlin}" → ["*.go.java", "*.go.kotlin"].
// If no braces exist, returns the original pattern unchanged.
func expandBraces(s string) []string {
	openIdx := strings.IndexByte(s, '{')
	if openIdx < 0 {
		return []string{s}
	}

	closeIdx := strings.IndexByte(s[openIdx:], '}')
	if closeIdx < 0 {
		return []string{s}
	}
	closeIdx += openIdx

	prefix := s[:openIdx]
	suffix := s[closeIdx+1:]
	options := strings.Split(s[openIdx+1:closeIdx], ",")

	results := make([]string, 0, len(options))
	for _, opt := range options {
		results = append(results, prefix+opt+suffix)
	}
	return results
}

// ProjectRuleEntry is a single entry in .opencodereview/rule.json.
type ProjectRuleEntry struct {
	Path             string `json:"path"`
	Rule             string `json:"rule"`
	MergeSystemRule  bool   `json:"merge_system_rule,omitempty"`
	MergeParentRules bool   `json:"merge_parent_rules,omitempty"`
	sources          []string
}

// ProjectRule holds the rules and file filters from one user config.
type ProjectRule struct {
	Rules      []ProjectRuleEntry `json:"rules"`
	Include    []string           `json:"include,omitempty"`
	Exclude    []string           `json:"exclude,omitempty"`
	FilterMode string             `json:"filter_mode,omitempty"`
}

// FileFilter holds the merged user-configured include/exclude glob patterns
// collected from all rule.json layers (custom, project, global).
type FileFilter struct {
	Include []string
	Exclude []string
}

// HasInclude reports whether any include patterns are configured.
func (f *FileFilter) HasInclude() bool {
	return len(f.Include) > 0
}

// IsUserExcluded reports whether the given path matches any user exclude pattern.
// The check is case-insensitive: both path and pattern are lowercased.
func (f *FileFilter) IsUserExcluded(path string) bool {
	lowerPath := strings.ToLower(path)
	for _, pattern := range f.Exclude {
		expanded := expandBraces(pattern)
		for _, p := range expanded {
			if matched, _ := doublestar.Match(strings.ToLower(p), lowerPath); matched {
				return true
			}
		}
	}
	return false
}

// IsUserIncluded reports whether the given path matches any user include pattern.
// The check is case-insensitive: both path and pattern are lowercased.
// Returns false when Include is empty (no user include restriction defined).
func (f *FileFilter) IsUserIncluded(path string) bool {
	if !f.HasInclude() {
		return false
	}
	lowerPath := strings.ToLower(path)
	for _, pattern := range f.Include {
		expanded := expandBraces(pattern)
		for _, p := range expanded {
			if matched, _ := doublestar.Match(strings.ToLower(p), lowerPath); matched {
				return true
			}
		}
	}
	return false
}

// composedResolver implements Resolver with layered priority.
type composedResolver struct {
	layers []ruleLayer
	system systemLayer
}

// ResolverOptions carries the optional git context the resolver needs to read
// file content when disambiguating extensions shared by several languages
// (currently only ".m": MATLAB vs Objective-C). The zero value is valid and
// makes content reads fall back to the working tree.
type ResolverOptions struct {
	// Ref is the git ref whose content should be inspected — the review head
	// (--to) in range mode, or --commit in commit mode. Empty reads the
	// working tree, which is what `ocr scan` and `ocr rules check` want.
	Ref string

	// Runner bounds concurrent git subprocesses. Optional; when nil the
	// resolver shells out to git directly.
	Runner *gitcmd.Runner
}

// NewResolver builds a Resolver with the following priority:
//  1. Custom rule file specified via --rule flag (first match wins)
//  2. Project configs from the repo root upward, nearest first
//  3. Global ~/.opencodereview/rule.json (first match wins)
//  4. Embedded system default rules
//
// The system layer is wrapped in a sniffer so ".m" files can be resolved as
// Objective-C when their content says so. Wrapping the *system* layer (rather
// than the composed resolver) keeps user layers outranking the sniff.
//
// Ancestor inheritance is opt-in. It also returns the effective file filter.
func NewResolver(repoDir, customRulePath string, opts ResolverOptions) (Resolver, *FileFilter, error) {
	sysRule, err := LoadDefault()
	if err != nil {
		return nil, nil, err
	}

	objcRule, err := loadObjCRule()
	if err != nil {
		return nil, nil, err
	}

	var customRule *ProjectRule
	if customRulePath != "" {
		cr, err := loadRuleFile(customRulePath)
		if err != nil {
			return nil, nil, err
		}
		customRule = cr
	}

	var projectRules []*ProjectRule
	if repoDir != "" {
		pr, err := loadProjectRules(repoDir)
		if err != nil {
			return nil, nil, err
		}
		projectRules = pr
	}

	globalRule, err := loadGlobalRule()
	if err != nil {
		return nil, nil, err
	}

	layers := buildRuleLayers(customRule, projectRules, globalRule)
	filter := buildFileFilter(layers)

	return &composedResolver{
		layers: layers,
		system: &sniffer{
			inner:    sysRule,
			repoDir:  repoDir,
			ref:      opts.Ref,
			runner:   opts.Runner,
			objcRule: objcRule,
		},
	}, filter, nil
}

func loadGlobalRule() (*ProjectRule, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil
	}
	path := filepath.Join(home, ".opencodereview", "rule.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read global rule %s: %w", path, err)
	}
	var pr ProjectRule
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("unmarshal global rule: %w", err)
	}
	if err := validateRuleConfig(&pr); err != nil {
		return nil, fmt.Errorf("global rule %s: %w", path, err)
	}
	resolveRuleEntries(pr.Rules, filepath.Dir(path), "")
	return &pr, nil
}

func loadRuleFile(path string) (*ProjectRule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read rule file %s: %w", path, err)
	}
	var pr ProjectRule
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("unmarshal rule file %s: %w", path, err)
	}
	if err := validateRuleConfig(&pr); err != nil {
		return nil, fmt.Errorf("rule file %s: %w", path, err)
	}
	resolveRuleEntries(pr.Rules, filepath.Dir(path), "")
	return &pr, nil
}

// Rule files and references must stay inside the directory that supplies the config.
func loadProjectRuleAt(repoDir string) (*ProjectRule, error) {
	confineRoot := repoDir

	path := filepath.Join(repoDir, ".opencodereview", "rule.json")
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("resolve project rule %s: %w", path, err)
	}
	if !pathutil.WithinBase(confineRoot, resolved) {
		fmt.Fprintf(os.Stderr, "[ocr] WARNING: project rule file escapes repo dir: %s\n", path)
		return nil, nil
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read project rule %s: %w", path, err)
	}
	var pr ProjectRule
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("unmarshal project rule: %w", err)
	}
	if err := validateRuleConfig(&pr); err != nil {
		return nil, fmt.Errorf("project rule %s: %w", path, err)
	}
	resolveRuleEntries(pr.Rules, repoDir, confineRoot)
	return &pr, nil
}

// Resolve returns the effective rule text after explicit inheritance.
func (c *composedResolver) Resolve(path string) string {
	return c.ResolveDetail(path).Rule
}

// CanonicalConfig returns a deterministic, order-stable field list describing the
// resolver's effective rule-text configuration across every layer (custom >
// project > global > system, each in declaration order), for hashing into the run
// manifest's rule_config_sha256. It covers only rule-text resolution; the
// include/exclude file filter is carried by FileFilter and hashed separately. Each
// field is tagged with its layer and role so two structurally different configs
// cannot collide once length-prefixed. Order is never sorted — first match wins.
func (c *composedResolver) CanonicalConfig() []string {
	var fields []string
	for _, layer := range c.layers {
		fields = append(fields, "layer", layer.source, "fallback", strconv.Itoa(layer.fallback))
		for _, e := range layer.rule.Rules {
			fields = append(fields, "path", e.Path, "rule", e.Rule,
				"merge_system", strconv.FormatBool(e.MergeSystemRule),
				"merge_parent", strconv.FormatBool(e.MergeParentRules))
		}
	}
	if c.system != nil {
		fields = append(fields, c.system.CanonicalConfig()...)
	}
	return fields
}

func (c *composedResolver) mergeWithSystemRule(path, rule string) string {
	systemRule := c.system.Resolve(path)

	if systemRule == "" {
		return rule
	}
	if rule == "" {
		return systemRule
	}

	return "## System-Specific Rules (Mandatory)\n\n" +
		systemRule +
		"\n\n---\n\n" +
		"## User-Specific Rules (Mandatory)\n\n" +
		rule
}

// ResolveDetail retains the source and pattern of the highest-priority match.
func (c *composedResolver) ResolveDetail(path string) RuleDetail {
	for i := 0; i < len(c.layers); i = c.layers[i].fallback {
		if entry := matchProjectRuleEntry(c.layers[i].rule, path); entry != nil {
			return c.resolveInheritedRule(path, i, entry)
		}
	}
	return c.system.resolveDetail(path)
}

func (c *composedResolver) resolveInheritedRule(path string, index int, first *ProjectRuleEntry) RuleDetail {
	var buffer [8]*ProjectRuleEntry
	entries := append(buffer[:0], first)
	mergeSystem := first.MergeSystemRule
	if first.MergeParentRules {
		for i := index + 1; i < len(c.layers); i++ {
			entry := matchProjectRuleEntry(c.layers[i].rule, path)
			if entry == nil {
				continue
			}
			entries = append(entries, entry)
			mergeSystem = mergeSystem || entry.MergeSystemRule
			if !entry.MergeParentRules {
				break
			}
		}
	}
	rule := combineUserRules(entries)
	if mergeSystem {
		rule = c.mergeWithSystemRule(path, rule)
	}
	return RuleDetail{Rule: rule, Source: c.layers[index].source, Pattern: first.Path}
}

func combineUserRules(entries []*ProjectRuleEntry) string {
	const separator = "\n\n---\n\n"
	size, count := 0, 0
	var only string
	for _, entry := range entries {
		if entry.Rule != "" {
			size += len(entry.Rule)
			count++
			only = entry.Rule
		}
	}
	if count <= 1 {
		return only
	}
	var text strings.Builder
	text.Grow(size + (count-1)*len(separator))
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Rule == "" {
			continue
		}
		if text.Len() != 0 {
			text.WriteString(separator)
		}
		text.WriteString(entries[i].Rule)
	}
	return text.String()
}

func matchProjectRuleEntry(pr *ProjectRule, path string) *ProjectRuleEntry {
	if pr == nil {
		return nil
	}
	lowerPath := strings.ToLower(path)
	for i := range pr.Rules {
		entry := &pr.Rules[i]
		if entry.Rule == "" && !entry.MergeSystemRule && !entry.MergeParentRules {
			continue
		}
		expanded := expandBraces(entry.Path)
		for _, p := range expanded {
			if matched, _ := doublestar.Match(strings.ToLower(p), lowerPath); matched {
				return entry
			}
		}
	}
	return nil
}

// allowedRuleExts is the set of file extensions permitted for rule file references.
var allowedRuleExts = map[string]bool{".md": true, ".txt": true, ".markdown": true}

// looksLikeFilePath returns true when s is likely a file path (not inline content).
// Heuristic: multi-line text is always inline; single-line text without spaces
// ending in .md/.txt/.markdown is treated as a file path. Values containing spaces
// (e.g. "Follow rules from team.md") are treated as inline to avoid false positives.
func looksLikeFilePath(s string) bool {
	if strings.Contains(s, "\n") {
		return false
	}
	if strings.Contains(s, " ") {
		return false
	}
	return allowedRuleExts[strings.ToLower(filepath.Ext(s))]
}

// resolveRuleEntries reads file references in each rule entry and replaces them with
// the file content. confineRoot limits the project layer to the config's parent
// directory (empty for trusted layers, meaning no confinement).
func resolveRuleEntries(entries []ProjectRuleEntry, repoDir string, confineRoot string) {
	for i := range entries {
		e := &entries[i]
		if e.sources != nil {
			e.Rule = resolveRuleSources(e.sources, repoDir, confineRoot)
			e.sources = nil
			continue
		}
		if strings.TrimSpace(e.Rule) == "" || !looksLikeFilePath(e.Rule) {
			continue
		}
		if content := tryReadRuleFile(e.Rule, repoDir, confineRoot); content != nil {
			e.Rule = *content
		} else {
			e.Rule = ""
		}
	}
}

// tryReadRuleFile reads a rule file reference. Absolute paths are used directly;
// relative paths resolve against repoDir. When confineRoot is non-empty, the resolved
// path must stay inside it. Returns nil when the file cannot be read safely.
func tryReadRuleFile(rule string, repoDir string, confineRoot string) *string {
	if repoDir == "" {
		if !filepath.IsAbs(rule) {
			fmt.Fprintf(os.Stderr, "[ocr] WARNING: cannot resolve relative rule path %q without a repo dir\n", rule)
			return nil
		}
	}
	if filepath.IsAbs(rule) {
		content, err := readRuleFileSafe(rule, confineRoot)
		if err == nil {
			return &content
		}
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "[ocr] WARNING: rule file not found: %s\n", rule)
		} else {
			fmt.Fprintf(os.Stderr, "[ocr] WARNING: cannot read rule file %s: %v\n", rule, err)
		}
		return nil
	}

	// Relative path: resolve against repoDir, validate no traversal.
	resolved := filepath.Clean(filepath.Join(repoDir, rule))
	cleanRepo := filepath.Clean(repoDir)
	if !pathutil.WithinBase(cleanRepo, resolved) {
		fmt.Fprintf(os.Stderr, "[ocr] WARNING: rule file path escapes repo dir: %s\n", rule)
		return nil
	}

	content, err := readRuleFileSafe(resolved, confineRoot)
	if err == nil {
		return &content
	}
	if os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "[ocr] WARNING: rule file not found: %s\n", rule)
	} else {
		fmt.Fprintf(os.Stderr, "[ocr] WARNING: cannot read rule file %s: %v\n", resolved, err)
	}
	return nil
}

// readRuleFileSafe reads and validates a rule file: extension whitelist, 512 KB cap,
// and symlink resolution. When confineRoot is non-empty, the resolved path must stay
// inside it. Returns the trimmed content on success.
func readRuleFileSafe(path string, confineRoot string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}

	if confineRoot != "" && !pathutil.WithinBase(confineRoot, resolved) {
		return "", fmt.Errorf("rule file path %q escapes repo dir %q", resolved, confineRoot)
	}

	if !allowedRuleExts[strings.ToLower(filepath.Ext(resolved))] {
		return "", fmt.Errorf("unsupported extension %q, only .md/.txt/.markdown allowed", filepath.Ext(resolved))
	}

	const maxSize = 512 * 1024
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if info.Size() > maxSize {
		return "", fmt.Errorf("file too large (%d bytes, max %d)", info.Size(), maxSize)
	}

	content, err := os.ReadFile(resolved)
	if err != nil {
		return "", err
	}

	return strings.TrimRight(string(content), "\n"), nil
}
