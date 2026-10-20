// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package verification_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alibaba/open-code-review/internal/diff"
	v "github.com/alibaba/open-code-review/internal/verification"
)

func TestContextIncludesCapturedChangesOutsideSelectedDiff(t *testing.T) {
	repo, firstView := capture(t)
	inputs := v.ContextInputs{GroupContext: "source.go", Scope: "source.go", Rules: "Check guards.", ContractFingerprint: "effective-contract", Mode: v.Enabled}
	firstProvider := diff.NewWorkspaceProvider(repo, nil)
	selected, err := firstProvider.GetDiffSet(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	inputs.Diffs = selected.Included
	first, err := v.NewContext(firstView, inputs)
	if err != nil {
		t.Fatal(err)
	}
	firstID, err := first.Identity()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "caller.go"), []byte("package example\nfunc caller() int { return divide(0) }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	nextProvider := diff.NewWorkspaceProvider(repo, nil)
	if _, err := nextProvider.GetDiffSet(context.Background()); err != nil {
		t.Fatal(err)
	}
	next, err := v.NewContext(nextProvider.Evidence(), inputs)
	if err != nil {
		t.Fatal(err)
	}
	nextID, err := next.Identity()
	if err != nil {
		t.Fatal(err)
	}
	if first.Input != next.Input || first.EvidenceState == next.EvidenceState || firstID == nextID {
		t.Fatal("unchanged endpoints and selected diff hide changed captured repository context")
	}
}
