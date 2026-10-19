// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package session

import (
	"context"
	"reflect"
	"testing"

	"github.com/alibaba/open-code-review/internal/model"
	v "github.com/alibaba/open-code-review/internal/verification"
)

func TestVerificationScopedUpdatesRetainOtherGroupsAndRounds(t *testing.T) {
	f := durableFixture(t)
	first := f.capture(t, "first group")
	f.core.Submit([]v.Decision{durableDecision(first[0].ID, v.Refuted, f.citation(t))})
	requireDurable(t, f.history.RecordVerificationSnapshot(f.core.Result()))
	requireDurable(t, f.history.RecordVerificationTerminal(f.id))
	requireDurable(t, f.history.RecordVerificationCheckpoint(f.item(), f.id, ""))
	otherContext := f.context
	otherContext.GroupContext = "second group"
	otherID, err := otherContext.Identity()
	requireDurable(t, err)
	other, err := v.New(f.view, otherID, savedProvenance)
	requireDurable(t, err)
	other.Capture([]model.LlmComment{{Path: "source.go", Content: "second group interrupted"}}, v.Origin{SessionID: f.history.SessionID, GroupID: "other", Round: 1, Checkpoint: "source.go"})
	requireDurable(t, f.history.CaptureVerification(otherContext, other.Result().Report))
	next := f.core.Capture([]model.LlmComment{{Path: "source.go", Content: "second discovery round"}}, v.Origin{SessionID: f.history.SessionID, GroupID: "arithmetic", Round: 2, Checkpoint: "source.go"})
	latest := f.core.Result()
	requireDurable(t, f.history.CaptureVerification(f.context, v.Report{Version: v.RecordVersion, Records: latest.Report.Records[1:]}))
	other.Fail("The other group stops.")
	requireDurable(t, f.history.RecordVerificationSnapshot(other.Result()))
	f.core.Submit([]v.Decision{{CandidateID: next[0].ID, Verdict: v.Uncertain, Reason: v.Reason{Code: "unresolved", Explanation: "The caller is unavailable."}}})
	requireDurable(t, f.history.RecordVerificationSnapshot(f.core.Result()))
	// No terminal event covers the second round.
	f.close(t, f.history)
	state := f.load(t)
	want := v.Report{Version: v.RecordVersion, Records: []v.Record{f.core.Result().Report.Records[0], other.Result().Report.Records[0], f.core.Result().Report.Records[1]}}
	assertDurableResult(t, state, v.ResultFromReport(want))
	restoredContext, ok := state.VerificationContext(otherID)
	if !ok || !reflect.DeepEqual(restoredContext, otherContext) {
		t.Fatal("group context does not survive replay")
	}
	result, err := state.ReuseVerification(context.Background(), f.request())
	requireDurable(t, err)
	if result.Disposition != VerificationRecheck || !reflect.DeepEqual(result.Result, f.core.Result()) {
		t.Fatalf("later unfinished round falls back to prior checkpoint: %+v", result)
	}
	item, ok := state.Item(f.item().Fingerprint)
	if !ok || !reflect.DeepEqual(item.Comments, []model.LlmComment{first[0].Finding, next[0].Finding}) {
		t.Fatalf("checkpoint uses another group's publication: %+v", item)
	}
}

func TestVerificationMissingBindingCannotBorrowEarlierTerminal(t *testing.T) {
	f := finishedVerification(t)
	rewriteVerificationFile(t, f, func(records []map[string]any) []map[string]any {
		for _, record := range records {
			if record["type"] == "review_item_done" {
				delete(record, "verification")
			}
		}
		return records
	}, false)
	state := f.load(t)
	result, err := state.ReuseVerification(context.Background(), f.request())
	requireDurable(t, err)
	if result.Disposition != VerificationFresh {
		t.Fatal("unbound checkpoint borrows verification from other records")
	}
}

func TestVerificationUnavailablePersistenceDoesNotClaimCapture(t *testing.T) {
	f := durableFixture(t)
	f.history.persist.flushAndClose()
	f.history.persist = nil
	f.core.Capture([]model.LlmComment{{Path: "source.go", Content: "not persisted"}}, v.Origin{})
	if err := f.history.CaptureVerification(f.context, f.core.Result().Report); err == nil {
		t.Fatal("unavailable persistence acknowledges capture")
	}
	if err := f.history.RecordVerificationTerminal(f.id); err == nil {
		t.Fatal("terminal write conceals unavailable persistence")
	}
	if err := f.history.Finalize(); err == nil {
		t.Fatal("finalization conceals unavailable persistence")
	}
	state := f.load(t)
	result, err := state.VerificationResult()
	requireDurable(t, err)
	if !reflect.DeepEqual(result.Report.Records, []v.Record{}) {
		t.Fatal("failed capture appears on disk")
	}
}

func TestVerificationInvalidBindingPreservesRecoverableCheckpointClaims(t *testing.T) {
	for _, corruption := range []string{"unknown context", "different known context", "missing capture", "malformed binding"} {
		t.Run(corruption, func(t *testing.T) {
			f := durableFixture(t)
			candidates := f.capture(t, "confirmed", "refuted", "incomplete", "uncertain")
			citation := f.citation(t)
			f.core.Submit([]v.Decision{
				durableDecision(candidates[0].ID, v.Confirmed, citation),
				durableDecision(candidates[1].ID, v.Refuted, citation),
				{CandidateID: candidates[3].ID, Verdict: v.Uncertain, Reason: v.Reason{Code: "unresolved", Explanation: "The caller is unavailable."}},
			})
			f.core.Fail("The request stops.", candidates[2].ID)
			requireDurable(t, f.history.RecordVerificationSnapshot(f.core.Result()))
			requireDurable(t, f.history.RecordVerificationTerminal(f.id))
			requireDurable(t, f.history.RecordVerificationCheckpoint(f.item(), f.id, ""))
			otherContext := f.context
			otherContext.GroupContext = "another group"
			otherID, err := otherContext.Identity()
			requireDurable(t, err)
			requireDurable(t, f.history.CaptureVerification(otherContext, v.Report{Version: v.RecordVersion, Records: []v.Record{}}))
			f.close(t, f.history)
			saved := verificationComments(f.core.Result(), f.item().FilePath)
			want := append(copyLlmComments(saved), candidates[1].Finding)
			if corruption == "missing capture" {
				want = saved
			}
			rewriteVerificationFile(t, f, func(records []map[string]any) []map[string]any {
				kept := records[:0]
				for _, record := range records {
					if corruption == "missing capture" && record["type"] == "verification_capture" {
						continue
					}
					if record["type"] == "review_item_done" {
						switch corruption {
						case "unknown context":
							record["verification"].(map[string]any)["context_id"] = "unknown-context"
						case "different known context":
							record["verification"].(map[string]any)["context_id"] = otherID
						case "malformed binding":
							record["verification"] = "invalid"
						}
					}
					kept = append(kept, record)
				}
				return kept
			}, false)
			state := f.load(t)
			audit, auditErr := state.VerificationResult()
			if auditErr == nil {
				t.Fatal("corrupt verification data appears valid")
			}
			if corruption != "missing capture" {
				for i, finding := range audit.Published {
					if finding.Finding != candidates[i].Finding || finding.Verification.Verdict != v.Uncertain || finding.Verification.State != v.Incomplete {
						t.Fatal("recovered audit claims do not remain unverified")
					}
				}
			}
			item, ok := state.Item(f.item().Fingerprint)
			if !ok || !reflect.DeepEqual(item.Comments, want) {
				t.Fatalf("corrupt binding loses or duplicates checkpoint claims: got=%+v want=%+v", item.Comments, want)
			}
			reuse, err := state.ReuseVerification(context.Background(), f.request())
			requireDurable(t, err)
			if reuse.Disposition != VerificationFresh {
				t.Fatal("fallback comments authorize verification reuse")
			}
		})
	}
}
