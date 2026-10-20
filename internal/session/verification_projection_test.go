// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package session

import (
	"reflect"
	"testing"

	"github.com/alibaba/open-code-review/internal/model"
	v "github.com/alibaba/open-code-review/internal/verification"
)

func TestSavedFindingsPreservesMultiplicityPathlessClaimsAndTrust(t *testing.T) {
	f := durableFixture(t)
	candidates := f.capture(t, "same claim", "same claim", "interrupted")
	pathless := model.LlmComment{Content: "The remote contract is unavailable.", Severity: "medium", Category: "bug"}
	more := f.core.Capture([]model.LlmComment{pathless}, v.Origin{SessionID: f.history.SessionID, GroupID: "arithmetic", Round: 2, Checkpoint: "source.go"})
	capture := f.core.Result().Report
	capture.Records = capture.Records[len(capture.Records)-1:]
	requireDurable(t, f.history.CaptureVerification(f.context, capture))
	citation := f.citation(t)
	f.core.Submit([]v.Decision{durableDecision(candidates[0].ID, v.Confirmed, citation), durableDecision(candidates[1].ID, v.Refuted, citation), {CandidateID: more[0].ID, Verdict: v.Uncertain, Reason: v.Reason{Code: "caller_unavailable", Explanation: "The remote contract is unavailable."}}})
	f.core.Fail("The provider disconnects.", candidates[2].ID)
	requireDurable(t, f.history.RecordVerificationSnapshot(f.core.Result()))
	requireDurable(t, f.history.RecordVerificationTerminal(f.id))
	requireDurable(t, f.history.RecordVerificationCheckpoint(f.item(), f.id, ""))
	state := f.load(t)
	findings, report, err := state.SavedFindings()
	requireDurable(t, err)
	if len(findings) != 3 || !reflect.DeepEqual(report, f.core.Result().Report) {
		t.Fatalf("saved projection: %+v report=%+v", findings, report)
	}
	found := map[string]v.PublishedFinding{}
	for _, finding := range findings {
		found[finding.CandidateID] = finding
	}
	if found[candidates[0].ID].Verification.Verdict != v.Confirmed || found[candidates[2].ID].Verification.State != v.Incomplete || found[more[0].ID].Finding.Path != "source.go" || found[more[0].ID].Finding.Content != pathless.Content {
		t.Fatalf("saved identities or pathless claim change: %+v", findings)
	}
	item := state.Items[f.item().Fingerprint]
	item.Verification.ContextID = "corrupt-context"
	state.Items[f.item().Fingerprint] = item
	findings, _, err = state.SavedFindings()
	requireDurable(t, err)
	if len(findings) != 4 {
		t.Fatalf("corrupt binding loses a duplicate claim: %+v", findings)
	}
	for _, finding := range findings {
		if finding.Verification.Verdict != v.Uncertain || finding.Verification.State != v.Incomplete {
			t.Fatalf("corrupt binding trusts a decision: %+v", finding)
		}
	}
	item.Verification = nil
	state.Items[f.item().Fingerprint] = item
	findings, _, err = state.SavedFindings()
	requireDurable(t, err)
	if len(findings) != 4 {
		t.Fatalf("missing binding suppresses a recoverable claim: %+v", findings)
	}
	for _, finding := range findings {
		if finding.Verification.Verdict != v.Uncertain {
			t.Fatalf("missing binding trusts a decision: %+v", finding)
		}
	}
	legacy := &ResumeState{Items: map[string]ResumeItem{"legacy": {FilePath: "source.go", Comments: []model.LlmComment{pathless, pathless}}}}
	findings, _, err = legacy.SavedFindings()
	requireDurable(t, err)
	if len(findings) != 2 || findings[0].CandidateID != "" || findings[1].Verification.Verdict != v.Uncertain || findings[0].Finding.Path != "source.go" {
		t.Fatalf("legacy multiplicity: %+v", findings)
	}
}
