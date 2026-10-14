// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package rules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type projectRuleEntryJSON ProjectRuleEntry

func (e *ProjectRuleEntry) UnmarshalJSON(data []byte) error {
	var parsed ProjectRuleEntry
	decoded := struct {
		*projectRuleEntryJSON
		Rule json.RawMessage `json:"rule"`
	}{projectRuleEntryJSON: (*projectRuleEntryJSON)(&parsed)}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	raw := bytes.TrimSpace(decoded.Rule)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		*e = parsed
		return nil
	}
	switch raw[0] {
	case '"':
		if err := json.Unmarshal(raw, &parsed.Rule); err != nil {
			return err
		}
	case '[':
		var sources []json.RawMessage
		if err := json.Unmarshal(raw, &sources); err != nil {
			return err
		}
		parsed.sources = make([]string, len(sources))
		for i, source := range sources {
			source = bytes.TrimSpace(source)
			if len(source) == 0 || source[0] != '"' {
				return fmt.Errorf("rule source at index %d must be a string", i)
			}
			if err := json.Unmarshal(source, &parsed.sources[i]); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("rule must be a string or an array of strings")
	}
	*e = parsed
	return nil
}

func (e ProjectRuleEntry) MarshalJSON() ([]byte, error) {
	if e.sources == nil {
		return json.Marshal(projectRuleEntryJSON(e))
	}
	return json.Marshal(struct {
		projectRuleEntryJSON
		Rule []string `json:"rule"`
	}{projectRuleEntryJSON: projectRuleEntryJSON(e), Rule: e.sources})
}

func resolveRuleSources(sources []string, repoDir, confineRoot string) string {
	size, count := 0, 0
	var only string
	for i, source := range sources {
		if strings.TrimSpace(source) == "" {
			sources[i] = ""
			continue
		}
		if looksLikeFilePath(source) {
			content := tryReadRuleFile(source, repoDir, confineRoot)
			if content == nil {
				sources[i] = ""
				continue
			}
			source = *content
			sources[i] = source
		}
		if source != "" {
			size += len(source)
			count++
			only = source
		}
	}
	if count <= 1 {
		return only
	}
	const separator = "\n\n---\n\n"
	var text strings.Builder
	text.Grow(size + (count-1)*len(separator))
	for _, source := range sources {
		if source == "" {
			continue
		}
		if text.Len() != 0 {
			text.WriteString(separator)
		}
		text.WriteString(source)
	}
	return text.String()
}
