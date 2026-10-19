// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package diff

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/alibaba/open-code-review/internal/pathutil"
)

const maxUntrackedFileSize = 64 << 20

var errWorkspaceFileTooLarge = errors.New("workspace file exceeds size limit")

func readWorkspaceFileForDiff(repoDir, relPath string) ([]byte, error) {
	return readWorkspaceFileForDiffWithLimit(repoDir, relPath, 0)
}

func readWorkspaceFileForDiffWithLimit(repoDir, relPath string, maxSize int64) ([]byte, error) {
	repoRoot, err := pathutil.CanonicalPath(repoDir)
	if err != nil {
		return nil, fmt.Errorf("resolve repository path %q: %w", repoDir, err)
	}
	if filepath.IsAbs(relPath) {
		return nil, fmt.Errorf("file path %q must be relative, not absolute", relPath)
	}

	fullPath := filepath.Join(repoRoot, relPath)
	if !pathutil.WithinBase(repoRoot, fullPath) {
		return nil, fmt.Errorf("file path %q is outside repository", relPath)
	}

	parent, err := filepath.EvalSymlinks(filepath.Dir(fullPath))
	if err != nil {
		return nil, fmt.Errorf("resolve parent path for %q: %w", relPath, err)
	}
	if !pathutil.WithinBase(repoRoot, parent) {
		return nil, fmt.Errorf("file path %q is outside repository", relPath)
	}

	info, err := os.Lstat(fullPath)
	if err != nil {
		return nil, fmt.Errorf("stat file %q: %w", relPath, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("file path %q is a directory", relPath)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(fullPath)
		if err != nil {
			return nil, fmt.Errorf("read symlink %q: %w", relPath, err)
		}
		return []byte(target), nil
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("file path %q is not a regular file", relPath)
	}
	if maxSize > 0 && info.Size() > maxSize {
		return nil, errWorkspaceFileTooLarge
	}

	resolvedPath, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		return nil, fmt.Errorf("resolve file %q: %w", relPath, err)
	}
	if !pathutil.WithinBase(repoRoot, resolvedPath) {
		return nil, fmt.Errorf("file path %q is outside repository", relPath)
	}
	root, err := os.OpenRoot(repoRoot)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.Open(relPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("file path %q is not a regular file", relPath)
	}
	var reader io.Reader = file
	if maxSize > 0 {
		reader = io.LimitReader(file, maxSize+1)
	}
	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read file %q: %w", relPath, err)
	}
	if maxSize > 0 && int64(len(content)) > maxSize {
		return nil, errWorkspaceFileTooLarge
	}
	return content, nil
}
