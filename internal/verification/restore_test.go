// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package verification_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/alibaba/open-code-review/internal/model"
	v "github.com/alibaba/open-code-review/internal/verification"
)

func TestRestoreRetainsClaimsSupportAndIncompleteWork(t *testing.T) {
	_, view := capture(t)
	core := newCore(t, view, "saved-context")
	candidates := core.Capture([]model.LlmComment{finding("confirmed"), finding("refuted"), finding("invalid"), finding("conflict"), finding("incomplete"), finding("skipped")}, origin)
	citation := issue(t, core)
	invalid := decision(candidates[2].ID, v.Refuted, citation)
	invalid.Citations[0].Digest = "untrusted submission"
	core.Submit([]v.Decision{decision(candidates[0].ID, v.Confirmed, citation), decision(candidates[1].ID, v.Refuted, citation), invalid, decision(candidates[3].ID, v.Confirmed, citation), decision(candidates[3].ID, v.Refuted, citation)})
	core.Fail("The request stops.", candidates[4].ID)
	core.Bypass(candidates[5].ID)
	saved := core.Result()
	restored := newCore(t, view, "saved-context")
	recheck, err := restored.Restore(context.Background(), saved.Report)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(recheck, []v.Candidate{candidates[4]}) || !reflect.DeepEqual(restored.Result(), saved) {
		t.Fatalf("restored=%+v recheck=%+v", restored.Result(), recheck)
	}
	next := v.Provenance{Provider: "other", Model: "other", ContractVersion: "1"}
	if err := restored.BindInvocation(view, recheck, next); err != nil {
		t.Fatal(err)
	}
	records := restored.Result().Report.Records
	if records[0].Provenance != provenance || records[4].Provenance != next || records[0].Citations[0].ID != citation.ID {
		t.Fatal("restoration rewrites completed provenance or source identifiers")
	}
	saved.Report.Records[0].Citations[0].Excerpt = "caller mutation"
	if restored.Result().Report.Records[0].Citations[0].Excerpt != citation.Excerpt {
		t.Fatal("restoration aliases caller support")
	}
	restored.Submit([]v.Decision{decision(candidates[4].ID, v.Refuted, citation)})
	if restored.Result().Report.Records[4].Verdict != v.Refuted {
		t.Fatal("restoration replaces historical receipt identifiers")
	}
}

func TestRestoreRejectsMalformedInputAtomically(t *testing.T) {
	_, view := capture(t)
	source := newCore(t, view, "saved-context")
	candidates := source.Capture([]model.LlmComment{finding("first"), finding("second")}, origin)
	receipt := issue(t, source)
	source.Submit([]v.Decision{decision(candidates[0].ID, v.Confirmed, receipt), decision(candidates[1].ID, v.Refuted, receipt)})
	original := source.Result().Report
	changes := map[string]func(*v.Report){
		"report version":     func(r *v.Report) { r.Version++ },
		"record version":     func(r *v.Report) { r.Records[1].Version++ },
		"context":            func(r *v.Report) { r.Records[1].Candidate.ContextID = "other" },
		"duplicate":          func(r *v.Report) { r.Records[1] = v.CloneRecord(r.Records[0]) },
		"execution":          func(r *v.Report) { r.Records[1].State = v.Incomplete },
		"missing submission": func(r *v.Report) { r.Records[1].Submissions = nil },
		"reason":             func(r *v.Report) { r.Records[1].Reason.Explanation = "" },
		"verdict":            func(r *v.Report) { r.Records[1].Verdict = "unknown" },
	}
	for name, change := range map[string]func(*v.Citation){
		"context":     func(c *v.Citation) { c.ContextID = "other" },
		"state":       func(c *v.Citation) { c.State = "other" },
		"path":        func(c *v.Citation) { c.Path = "missing.go" },
		"unsafe path": func(c *v.Citation) { c.Path = "../source.go" },
		"start":       func(c *v.Citation) { c.StartLine++ },
		"end":         func(c *v.Citation) { c.EndLine++ },
		"digest":      func(c *v.Citation) { c.Digest = "other" },
		"excerpt":     func(c *v.Citation) { c.Excerpt += "altered" },
	} {
		changes["citation "+name] = func(r *v.Report) {
			change(&r.Records[1].Citations[0])
			change(&r.Records[1].Submissions[0].Citations[0])
		}
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			target := newCore(t, view, "saved-context")
			target.Capture([]model.LlmComment{finding("existing valid claim")}, origin)
			before := target.Result()
			report := v.ResultFromReport(original).Report
			change(&report)
			if _, err := target.Restore(context.Background(), report); err == nil {
				t.Fatal("malformed input becomes trusted")
			}
			if !reflect.DeepEqual(target.Result(), before) {
				t.Fatal("failed restoration partially installs trusted records")
			}
		})
	}
}

func TestContextIdentityPreservesInputOrderAndCapturedState(t *testing.T) {
	repo, view := capture(t)
	inputs := v.ContextInputs{Diffs: []model.Diff{{OldPath: "first", NewPath: "first", Diff: "one"}, {OldPath: "second", NewPath: "second", Diff: "two"}}, GroupContext: "group", Scope: "scope", Rules: "rules", Background: "background", ContractFingerprint: "effective-contract", Mode: v.Enabled}
	saved, err := v.NewContext(view, inputs)
	if err != nil {
		t.Fatal(err)
	}
	id, err := saved.Identity()
	if err != nil {
		t.Fatal(err)
	}
	inputs.Diffs[0].Diff = "caller mutation"
	same, err := saved.Identity()
	if err != nil || same != id {
		t.Fatal("context aliases mutable input")
	}
	reordered := saved
	reordered.Diffs = []model.Diff{saved.Diffs[1], saved.Diffs[0]}
	other, err := reordered.Identity()
	if err != nil || other == id {
		t.Fatal("context discards meaningful diff order")
	}
	writeSource(t, repo, "later filesystem state")
	if view.State() != saved.EvidenceState || saved.Input != view.Input() || !saved.Matches(view) {
		t.Fatal("context recaptures mutable source")
	}
	if saved.Input.ResolvedHead != "" {
		t.Fatal("workspace context fabricates a head endpoint")
	}
	saved.Version++
	if _, err := saved.Identity(); err == nil {
		t.Fatal("unsupported context version becomes trusted")
	}
}

func TestRestorePreservesCompletedUncertaintyWithDiagnosticReason(t *testing.T) {
	_, view := capture(t)
	core := newCore(t, view, "saved-context")
	candidates := core.Capture([]model.LlmComment{finding("unresolved")}, origin)
	core.Submit([]v.Decision{{CandidateID: candidates[0].ID, Verdict: v.Uncertain, Reason: v.Reason{Code: "invalid_decision", Explanation: "The verifier cannot settle the claim."}}})
	saved := core.Result()
	restored := newCore(t, view, "saved-context")
	recheck, err := restored.Restore(context.Background(), saved.Report)
	if err != nil {
		t.Fatal(err)
	}
	if len(recheck) != 0 || !reflect.DeepEqual(restored.Result(), saved) {
		t.Fatal("restoration interprets a reason code as a different verdict or execution state")
	}
}
