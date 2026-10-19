// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package diff

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// The private index exposes captured attribute blobs without a repository copy.
// Git resolves macros, precedence, and boolean states against this index.
func (v *EvidenceView) attributeIndex(ctx context.Context, names []string, temp string) ([]string, error) {
	gitDir, err := v.provider.runGit(ctx, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, err
	}
	objects, err := v.provider.runGit(ctx, "rev-parse", "--path-format=absolute", "--git-path", "objects")
	if err != nil {
		return nil, err
	}
	objectDir := filepath.Join(temp, "objects")
	worktree := filepath.Join(temp, "worktree")
	if err := os.MkdirAll(objectDir, 0700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(worktree, 0700); err != nil {
		return nil, err
	}
	env := os.Environ()
	kept := env[:0]
	for _, variable := range env {
		if !strings.HasPrefix(variable, "GIT_ATTR_SOURCE=") {
			kept = append(kept, variable)
		}
	}
	env = append(kept, "GIT_DIR="+strings.TrimSuffix(gitDir, "\n"), "GIT_WORK_TREE="+worktree, "GIT_INDEX_FILE="+filepath.Join(temp, "index"), "GIT_OBJECT_DIRECTORY="+objectDir, "GIT_ALTERNATE_OBJECT_DIRECTORIES="+strconv.Quote(strings.TrimSuffix(objects, "\n")))
	empty, err := v.attributeCommand(ctx, worktree, env, "", "hash-object", "--stdin")
	if err != nil {
		return nil, err
	}
	empty = strings.TrimSpace(empty)
	var index strings.Builder
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entry := v.entries[name]
		object := entry.object
		if path.Base(name) == ".gitattributes" && entry.captured {
			content, err := v.fileContent(ctx, name)
			if err != nil {
				return nil, err
			}
			object, err = v.attributeCommand(ctx, worktree, env, content, "hash-object", "-w", "--stdin")
			if err != nil {
				return nil, err
			}
			object = strings.TrimSpace(object)
		}
		if object == "" {
			object = empty
		}
		mode := entry.mode
		if mode == "" {
			mode = "100644"
		}
		fmt.Fprintf(&index, "%s %s\t%s%c", mode, object, name, 0)
		if index.Len() > evidenceCaptureLimit {
			return nil, fmt.Errorf("attribute index exceeds capture limit")
		}
	}
	if _, err := v.attributeCommand(ctx, worktree, env, index.String(), "update-index", "-z", "--index-info"); err != nil {
		return nil, err
	}
	return env, nil
}

func (v *EvidenceView) attributeCommand(ctx context.Context, dir string, env []string, input string, args ...string) (string, error) {
	out := evidenceOutput{limit: 4 << 20}
	diagnostic := evidenceOutput{limit: gitDiagLimit}
	if err := v.provider.runGitIO(ctx, dir, env, strings.NewReader(input), &out, &diagnostic, args...); err != nil {
		return "", fmt.Errorf("capture search attributes: %w: %s", err, strings.TrimSpace(diagnostic.String()))
	}
	if out.truncated {
		return "", fmt.Errorf("search attributes exceed capture limit")
	}
	return out.String(), nil
}
