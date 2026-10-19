// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package diff

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type evidenceAttribute struct{ state, value string }

// Attribute and driver decisions belong to the captured source, not the live checkout.
func (v *EvidenceView) captureSearchAttributes(ctx context.Context) error {
	names, err := v.Paths(ctx)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return nil
	}
	temp, err := os.MkdirTemp("", "ocr-evidence-attributes-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	env, err := v.attributeIndex(ctx, names, temp)
	if err != nil {
		return err
	}
	worktree := filepath.Join(temp, "worktree")
	var input strings.Builder
	for _, name := range names {
		input.WriteString(name)
		input.WriteByte(0)
	}
	out, err := v.attributeCommand(ctx, worktree, env, input.String(), "check-attr", "--cached", "--all", "-z", "--stdin")
	if err != nil {
		return err
	}
	var fields []string
	if out != "" {
		fields = strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	}
	if len(fields)%3 != 0 {
		return fmt.Errorf("search attributes have incomplete coverage")
	}
	type ambiguousAttribute struct{ name, value string }
	ambiguous := make(map[ambiguousAttribute]bool)
	for i := 0; i < len(fields); i += 3 {
		name, attribute, value := fields[i], fields[i+1], fields[i+2]
		entry, ok := v.entries[name]
		if !ok {
			return fmt.Errorf("search attributes contain an invalid path")
		}
		if entry.attributes == nil {
			entry.attributes = make(map[string]evidenceAttribute)
		}
		entry.attributes[attribute] = evidenceAttribute{state: "value", value: value}
		v.entries[name] = entry
		if value == "set" || value == "unset" {
			ambiguous[ambiguousAttribute{attribute, value}] = true
		}
	}
	// check-attr prints the same word for a boolean state and a literal value.
	keys := make([]ambiguousAttribute, 0, len(ambiguous))
	for key := range ambiguous {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].name != keys[j].name {
			return keys[i].name < keys[j].name
		}
		return keys[i].value < keys[j].value
	})
	for _, key := range keys {
		selector := key.name
		if key.value == "unset" {
			selector = "-" + selector
		}
		matches, err := v.attributeCommand(ctx, worktree, env, "", "ls-files", "--cached", "-z", "--", ":(attr:"+selector+")")
		if err != nil {
			return err
		}
		for _, name := range strings.Split(strings.TrimSuffix(matches, "\x00"), "\x00") {
			entry, ok := v.entries[name]
			if !ok {
				continue
			}
			attribute, ok := entry.attributes[key.name]
			if ok && attribute.value == key.value {
				attribute.state = key.value
				entry.attributes[key.name] = attribute
			}
		}
	}
	drivers := make(map[string]string)
	for _, name := range names {
		entry := v.entries[name]
		attribute := entry.attributes["diff"]
		switch attribute.state {
		case "unset":
			entry.searchClass = "binary"
		case "set":
			entry.searchClass = "text"
		case "value":
			class, ok := drivers[attribute.value]
			if !ok {
				key := "diff." + attribute.value + ".binary"
				stdout, stderr, err := v.provider.runGitSplit(ctx, "config", "--get", key)
				if err == nil && strings.TrimSuffix(stdout, "\n") != "auto" {
					stdout, stderr, err = v.provider.runGitSplit(ctx, "config", "--type=bool", "--get", key)
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if err != nil {
					var exit *exec.ExitError
					if !errors.As(err, &exit) || exit.ExitCode() != 1 {
						return fmt.Errorf("capture search driver: %w: %s", err, strings.TrimSpace(stderr))
					}
				}
				switch strings.TrimSpace(stdout) {
				case "true":
					class = "binary"
				case "false":
					class = "text"
				default:
					class = "auto"
				}
				drivers[attribute.value] = class
			}
			entry.searchClass = class
		default:
			entry.searchClass = "auto"
		}
		v.entries[name] = entry
	}
	return nil
}

func (p *Provider) runGitIO(ctx context.Context, dir string, env []string, stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	if !p.useReplacementRefs {
		args = append([]string{"--no-replace-objects"}, args...)
	}
	if p.runner != nil {
		return p.runner.RunIO(ctx, dir, env, stdin, stdout, stderr, args...)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
