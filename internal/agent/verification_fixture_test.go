// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package agent

import (
	"context"
	"os/exec"
	"testing"

	"github.com/alibaba/open-code-review/internal/diff"
)

func prepareVerificationFixture(t *testing.T, a *Agent) {
	t.Helper()
	if a.evidence != nil {
		return
	}
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=OCR", "-c", "user.email=ocr@example.test", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", "fixture"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if data, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v: %s", err, data)
		}
	}
	provider := diff.NewCommitProvider(dir, "HEAD", nil)
	if _, err := provider.GetDiff(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.bindEvidence(provider.Evidence())
}
