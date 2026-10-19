// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package diff

import (
	"fmt"
	"strings"
)

type evidenceAttributeRequirement struct{ name, state, value string }
type evidencePathspec struct {
	patterns   []string
	attributes []evidenceAttributeRequirement
	exclude    bool
}

func capturedPathspecs(patterns []string) ([]evidencePathspec, []evidencePathspec, error) {
	specs := make([]evidencePathspec, 0, len(patterns))
	hasAttributes := false
	for _, pattern := range patterns {
		spec := evidencePathspec{}
		var magic []string
		body := pattern
		if strings.HasPrefix(pattern, ":(") {
			end := strings.IndexByte(pattern, ')')
			if end < 0 {
				return nil, nil, fmt.Errorf("invalid pathspec magic")
			}
			body = pattern[end+1:]
			for _, word := range strings.Split(pattern[2:end], ",") {
				switch {
				case word == "exclude" || word == "!" || word == "^":
					spec.exclude = true
				case strings.HasPrefix(word, "attr:"):
					hasAttributes = true
					requirements := strings.Fields(strings.TrimPrefix(word, "attr:"))
					if len(requirements) == 0 {
						return nil, nil, fmt.Errorf("empty pathspec attribute requirement")
					}
					for _, token := range requirements {
						require := evidenceAttributeRequirement{state: "set", name: token}
						switch token[0] {
						case '-':
							require.state = "unset"
							require.name = token[1:]
						case '!':
							require.state = "unspecified"
							require.name = token[1:]
						default:
							if name, value, ok := strings.Cut(token, "="); ok {
								require.name = name
								require.value = value
								require.state = "value"
							}
						}
						if !validAttributeName(require.name) {
							return nil, nil, fmt.Errorf("invalid pathspec attribute name")
						}
						spec.attributes = append(spec.attributes, require)
					}
				default:
					if word != "" {
						magic = append(magic, word)
					}
				}
			}
		} else if strings.HasPrefix(pattern, ":") && pattern != ":" {
			i := 1
			for i < len(pattern) {
				if pattern[i] == '/' {
					magic = append(magic, "top")
				} else if pattern[i] == '!' || pattern[i] == '^' {
					spec.exclude = true
				} else {
					break
				}
				i++
			}
			if i > 1 {
				if i < len(pattern) && pattern[i] == ':' {
					i++
				}
				body = pattern[i:]
			}
		}
		if len(magic) > 0 {
			body = ":(" + strings.Join(magic, ",") + ")" + body
		} else if body == "" {
			body = "."
		}
		spec.patterns = []string{body}
		specs = append(specs, spec)
	}
	if !hasAttributes {
		return []evidencePathspec{{patterns: patterns}}, nil, nil
	}
	var include, exclude []evidencePathspec
	for _, spec := range specs {
		if spec.exclude {
			exclude = append(exclude, spec)
		} else {
			include = append(include, spec)
		}
	}
	if len(include) == 0 {
		include = []evidencePathspec{{}}
	}
	return include, exclude, nil
}

func validAttributeName(name string) bool {
	if name == "" || name[0] == '-' {
		return false
	}
	for _, c := range name {
		if c != '_' && c != '.' && c != '-' && !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func (p evidencePathspec) matches(entry evidenceEntry) bool {
	for _, require := range p.attributes {
		attribute, exists := entry.attributes[require.name]
		if require.state == "unspecified" {
			if exists {
				return false
			}
			continue
		}
		if !exists || attribute.state != require.state {
			return false
		}
		if require.state == "value" && attribute.value != require.value {
			return false
		}
	}
	return true
}
