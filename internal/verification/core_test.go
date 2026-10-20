// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package verification_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/alibaba/open-code-review/internal/diff"
	"github.com/alibaba/open-code-review/internal/model"
	v "github.com/alibaba/open-code-review/internal/verification"
)

const capturedCode = "package example\nfunc divide(n int) int {\n\treturn 10 / n\n}\n"

var provenance = v.Provenance{Provider: "fixture", Model: "verifier", ContractVersion: "1"}
var origin = v.Origin{SessionID: "session", GroupID: "group", Round: 2, Checkpoint: "source.go"}

func writeSource(t *testing.T, repo, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, "source.go"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func capture(t *testing.T) (string, *diff.EvidenceView) {
	t.Helper()
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	git("config", "user.email", "fixture@example.com")
	git("config", "user.name", "Fixture")
	writeSource(t, repo, "package example\n")
	git("add", ".")
	git("commit", "-qm", "base")
	writeSource(t, repo, capturedCode)
	provider := diff.NewWorkspaceProvider(repo, nil)
	if _, err := provider.GetDiffSet(context.Background()); err != nil {
		t.Fatal(err)
	}
	return repo, provider.Evidence()
}

func newCore(t *testing.T, view *diff.EvidenceView, contextID string) *v.Core {
	t.Helper()
	core, err := v.New(view, contextID, provenance)
	if err != nil {
		t.Fatal(err)
	}
	return core
}

func issue(t *testing.T, core *v.Core) v.Citation {
	t.Helper()
	source, err := core.Issue(context.Background(), "source.go", 2, 3)
	if err != nil || source.Citation == nil {
		t.Fatalf("issue: %+v, %v", source, err)
	}
	return *source.Citation
}

func finding(label string) model.LlmComment {
	return model.LlmComment{
		Path: "source.go", Content: label, SuggestionCode: "check n", ExistingCode: "return 10 / n",
		StartLine: 2, EndLine: 3, Thinking: "The caller can pass zero.", Category: "style", Severity: "low",
	}
}

func decision(id string, verdict v.Verdict, citation v.Citation) v.Decision {
	return v.Decision{
		CandidateID: id, Verdict: verdict,
		Reason:        v.Reason{Code: "source_trace", Explanation: "The source determines the claim."},
		Confirmation:  &v.Confirmation{Trigger: "n is zero", FailurePath: "divide reaches 10 / n", Consequence: "The program panics."},
		Contradiction: "The cited caller guards the operation that the claim describes as unguarded.",
		Citations:     []v.Citation{citation},
	}
}

func TestMixedDecisionsPreserveClaimsAndPublication(t *testing.T) {
	_, view := capture(t)
	core := newCore(t, view, "context")
	labels := []string{"confirmed", "refuted", "unresolved", "invalid", "failed", "bypassed", "missing"}
	findings := make([]model.LlmComment, len(labels))
	for i, label := range labels {
		findings[i] = finding(label)
	}
	candidates := core.Capture(findings, origin)
	citation := issue(t, core)
	confirmed := decision(candidates[0].ID, v.Confirmed, citation)
	refuted := decision(candidates[1].ID, v.Refuted, citation)
	unresolved := v.Decision{CandidateID: candidates[2].ID, Verdict: v.Uncertain, Reason: v.Reason{Code: "caller_unavailable", Explanation: "The caller is unavailable."}}
	invalid := decision(candidates[3].ID, v.Refuted, citation)
	invalid.Citations[0].Excerpt = "altered"
	core.Submit([]v.Decision{confirmed, refuted, unresolved, invalid})
	core.Fail("The provider fails.", candidates[0].ID, candidates[4].ID)
	core.Bypass(candidates[5].ID)
	core.Finish(candidates[6].ID)
	result := core.Result()
	if result.Report.Version != v.RecordVersion || len(result.Report.Records) != len(findings) {
		t.Fatalf("report: %+v", result.Report)
	}
	wantVerdicts := []v.Verdict{v.Confirmed, v.Refuted, v.Uncertain, v.Uncertain, v.Uncertain, v.Uncertain, v.Uncertain}
	wantStates := []v.ExecutionState{v.Complete, v.Complete, v.Complete, v.Complete, v.Incomplete, v.Skipped, v.Complete}
	wantReasons := []string{"source_trace", "source_trace", "caller_unavailable", "invalid_decision", "verification_failed", "verification_disabled", "missing_decision"}
	seen := make(map[string]bool)
	for i, record := range result.Report.Records {
		if record.Candidate != candidates[i] || record.Candidate.Finding != findings[i] || record.Candidate.ContextID != "context" || record.Candidate.Origin != origin {
			t.Fatalf("claim %d changes: %+v", i, record)
		}
		if record.Candidate.ID == "" || seen[record.Candidate.ID] {
			t.Fatalf("candidate identity is empty or duplicated: %+v", record.Candidate)
		}
		seen[record.Candidate.ID] = true
		if record.Version != v.RecordVersion || record.Provenance != provenance || record.Verdict != wantVerdicts[i] || record.State != wantStates[i] || record.Reason.Code != wantReasons[i] || record.Reason.Explanation == "" {
			t.Fatalf("record %d: %+v", i, record)
		}
	}
	for i, submitted := range []v.Decision{confirmed, refuted, unresolved, invalid} {
		if !reflect.DeepEqual(result.Report.Records[i].Submissions, []v.Decision{submitted}) {
			t.Fatalf("submission %d is lost: %+v", i, result.Report.Records[i])
		}
	}
	if !reflect.DeepEqual(result.Report.Records[0].Confirmation, confirmed.Confirmation) || !reflect.DeepEqual(result.Report.Records[0].Citations, confirmed.Citations) || result.Report.Records[1].Contradiction != refuted.Contradiction {
		t.Fatal("accepted support is lost")
	}
	wantPublished := make([]v.PublishedFinding, 0, len(findings)-1)
	for i, candidate := range candidates {
		if i == 1 {
			continue
		}
		record := result.Report.Records[i]
		wantPublished = append(wantPublished, v.PublishedFinding{CandidateID: candidate.ID, Finding: findings[i], Verification: v.Summary{Verdict: wantVerdicts[i], State: wantStates[i], Reason: record.Reason}})
	}
	if !reflect.DeepEqual(result.Published, wantPublished) {
		t.Fatalf("publication: %+v; want %+v", result.Published, wantPublished)
	}
}

func TestCitationIntegrityAndCapturedBytes(t *testing.T) {
	repo, view := capture(t)
	core := newCore(t, view, "context")
	citation := issue(t, core)
	wantDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(capturedCode)))
	if citation.Path != "source.go" || citation.ContextID != "context" || citation.State == "" || citation.StartLine != 2 || citation.EndLine != 3 || citation.Digest != wantDigest || citation.Excerpt != "func divide(n int) int {\n\treturn 10 / n" {
		t.Fatalf("receipt: %+v", citation)
	}
	writeSource(t, repo, "later bytes\n")
	again := issue(t, core)
	again.ID = citation.ID
	if again != citation {
		t.Fatalf("receipt changes after an edit: %+v", again)
	}
	foreign := issue(t, newCore(t, view, "other-context"))
	sameContextForeign := issue(t, newCore(t, view, "context"))
	mutations := []struct {
		name string
		edit func(*v.Citation)
	}{
		{"unknown", func(c *v.Citation) { c.ID = "unknown" }},
		{"context", func(c *v.Citation) { c.ContextID = "stale" }},
		{"state", func(c *v.Citation) { c.State = "another-state" }},
		{"path", func(c *v.Citation) { c.Path = "other.go" }},
		{"start", func(c *v.Citation) { c.StartLine = 1 }},
		{"end", func(c *v.Citation) { c.EndLine = 4 }},
		{"digest", func(c *v.Citation) { c.Digest = "wrong" }},
		{"excerpt", func(c *v.Citation) { c.Excerpt = "changed bytes" }},
		{"foreign", func(c *v.Citation) { *c = foreign }},
		{"foreign matching context", func(c *v.Citation) { *c = sameContextForeign }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			candidate := core.Capture([]model.LlmComment{finding(mutation.name)}, origin)[0]
			submitted := decision(candidate.ID, v.Refuted, citation)
			mutation.edit(&submitted.Citations[0])
			core.Submit([]v.Decision{submitted})
			result := core.Result()
			record := result.Report.Records[len(result.Report.Records)-1]
			if record.Verdict != v.Uncertain || record.State != v.Complete || record.Reason.Code != "invalid_decision" {
				t.Fatalf("invalid citation suppresses claim: %+v", record)
			}
		})
	}
	candidate := core.Capture([]model.LlmComment{finding("captured")}, origin)[0]
	core.Submit([]v.Decision{decision(candidate.ID, v.Confirmed, citation)})
	result := core.Result()
	last := result.Report.Records[len(result.Report.Records)-1]
	if last.Verdict != v.Confirmed || !reflect.DeepEqual(last.Citations, []v.Citation{citation}) {
		t.Fatalf("captured receipt is not accepted: %+v", last)
	}
}

func TestUndeliveredSourcesCannotIssueCitations(t *testing.T) {
	_, view := capture(t)
	core := newCore(t, view, "context")
	for _, bounds := range [][2]int{{0, 1}, {-1, 2}, {3, 2}, {1, 99}, {99, 100}} {
		source, err := core.Issue(context.Background(), "source.go", bounds[0], bounds[1])
		if err == nil || source.Citation != nil {
			t.Fatalf("invalid range %v receives a citation: %+v, %v", bounds, source, err)
		}
	}
	source, err := core.Issue(context.Background(), "absent.go", 1, 1)
	if err != nil || source.Citation != nil || source.Evidence.Available || source.Evidence.Reason == "" {
		t.Fatalf("missing source: %+v, %v", source, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if source, err := core.Issue(ctx, "source.go", 2, 3); err != context.Canceled || source.Citation != nil {
		t.Fatalf("canceled read: %+v, %v", source, err)
	}
	if source, err := core.Issue(context.Background(), "../source.go", 1, 1); err == nil || source.Citation != nil {
		t.Fatalf("unsafe path: %+v, %v", source, err)
	}
}

func TestUnsupportedDecisionsRemainCandidateLocal(t *testing.T) {
	_, view := capture(t)
	core := newCore(t, view, "context")
	citation := issue(t, core)
	cases := []struct {
		name string
		edit func(*v.Decision)
	}{
		{"missing reason", func(d *v.Decision) { d.Reason.Explanation = " \t" }},
		{"missing code", func(d *v.Decision) { d.Reason.Code = "" }},
		{"unknown verdict", func(d *v.Decision) { d.Verdict = "approved" }},
		{"missing citations", func(d *v.Decision) { d.Citations = nil }},
		{"missing confirmation", func(d *v.Decision) { d.Confirmation = nil }},
		{"missing trigger", func(d *v.Decision) { d.Confirmation.Trigger = "" }},
		{"missing path", func(d *v.Decision) { d.Confirmation.FailurePath = "" }},
		{"missing consequence", func(d *v.Decision) { d.Confirmation.Consequence = " \t" }},
		{"recommendation disagreement", func(d *v.Decision) {
			d.Verdict = v.Refuted
			d.Contradiction = ""
			d.Reason.Explanation = "Use another recommendation."
		}},
		{"severity disagreement", func(d *v.Decision) {
			d.Verdict = v.Refuted
			d.Contradiction = ""
			d.Reason.Explanation = "The severity is low."
		}},
		{"unsupported refutation", func(d *v.Decision) { d.Verdict = v.Refuted; d.Citations = nil }},
	}
	valid := core.Capture([]model.LlmComment{finding("valid")}, origin)[0]
	validDecision := decision(valid.ID, v.Refuted, citation)
	core.Submit([]v.Decision{validDecision})
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			candidate := core.Capture([]model.LlmComment{finding(test.name)}, origin)[0]
			submitted := decision(candidate.ID, v.Confirmed, citation)
			test.edit(&submitted)
			core.Submit([]v.Decision{submitted, {CandidateID: "unknown", Verdict: v.Refuted}})
			result := core.Result()
			record := result.Report.Records[len(result.Report.Records)-1]
			if record.Verdict != v.Uncertain || record.Reason.Code != "invalid_decision" || record.Reason.Explanation == "" || record.Confirmation != nil || record.Citations != nil || record.Contradiction != "" {
				t.Fatalf("unsupported decision is accepted: %+v", record)
			}
			if result.Report.Records[0].Verdict != v.Refuted || !reflect.DeepEqual(result.Report.Records[0].Submissions, []v.Decision{validDecision}) {
				t.Fatal("candidate-local failure changes another decision")
			}
		})
	}
}

func TestConflictsNeverUseLastWriteWins(t *testing.T) {
	_, view := capture(t)
	for _, acrossBatches := range []bool{false, true} {
		t.Run(fmt.Sprint(acrossBatches), func(t *testing.T) {
			core := newCore(t, view, "context")
			citation := issue(t, core)
			candidates := core.Capture([]model.LlmComment{finding("conflict"), finding("valid")}, origin)
			first := decision(candidates[0].ID, v.Refuted, citation)
			second := decision(candidates[0].ID, v.Confirmed, citation)
			valid := decision(candidates[1].ID, v.Confirmed, citation)
			if acrossBatches {
				core.Submit([]v.Decision{first, valid})
				core.Submit([]v.Decision{second})
			} else {
				core.Submit([]v.Decision{second, first, valid})
			}
			core.Submit([]v.Decision{first})
			result := core.Result()
			if result.Report.Records[0].Verdict != v.Uncertain || result.Report.Records[0].Reason.Code != "conflicting_decisions" || result.Report.Records[1].Verdict != v.Confirmed {
				t.Fatalf("conflict changes publication: %+v", result)
			}
			if len(result.Published) != 2 || len(result.Report.Records[0].Submissions) != 3 {
				t.Fatalf("conflict loses a claim or decision: %+v", result)
			}
			core.Submit([]v.Decision{valid})
			if core.Result().Report.Records[1].Verdict != v.Confirmed {
				t.Fatal("an identical repeated decision creates a conflict")
			}
		})
	}
}

func TestFailureLimitAndBypassPreserveCompletedDecisions(t *testing.T) {
	_, view := capture(t)
	cases := []struct {
		name  string
		apply func(*v.Core)
		state v.ExecutionState
		code  string
	}{
		{"failure", func(c *v.Core) { c.Fail("") }, v.Incomplete, "verification_failed"},
		{"limit", func(c *v.Core) { c.Exhaust("Budget exhausted.") }, v.Incomplete, "limit_exhausted"},
		{"bypass", func(c *v.Core) { c.Bypass() }, v.Skipped, "verification_disabled"},
		{"complete", func(c *v.Core) { c.Finish() }, v.Complete, "missing_decision"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			core := newCore(t, view, "context")
			candidates := core.Capture([]model.LlmComment{finding("decided"), finding("undecided")}, origin)
			initial := core.Result()
			if initial.Published[1].Verification.Verdict != v.Uncertain || initial.Published[1].Verification.State != v.Incomplete || initial.Published[1].Verification.Reason.Code != "not_verified" {
				t.Fatal("absent verification data implies a decision")
			}
			core.Submit([]v.Decision{decision(candidates[0].ID, v.Refuted, issue(t, core))})
			before := core.Result().Report.Records[0]
			test.apply(core)
			after := core.Result()
			if !reflect.DeepEqual(before, after.Report.Records[0]) || after.Report.Records[1].Verdict != v.Uncertain || after.Report.Records[1].State != test.state || after.Report.Records[1].Reason.Code != test.code || after.Report.Records[1].Reason.Explanation == "" {
				t.Fatalf("interruption: %+v", after)
			}
			if len(after.Published) != 1 || after.Published[0].CandidateID != candidates[1].ID {
				t.Fatalf("undecided claim disappears: %+v", after)
			}
		})
	}
}

func TestCallerMutationCannotChangeStoredEvidenceOrDecisions(t *testing.T) {
	_, view := capture(t)
	core := newCore(t, view, "context")
	findings := []model.LlmComment{finding("original")}
	candidates := core.Capture(findings, origin)
	original := candidates[0]
	findings[0].Thinking = "changed"
	candidates[0].Finding.Content = "changed"
	candidates[0].ID = "changed"
	source, err := core.Issue(context.Background(), "source.go", 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	citation := *source.Citation
	source.Citation.Excerpt = "changed"
	submitted := decision(original.ID, v.Confirmed, citation)
	core.Submit([]v.Decision{submitted})
	before := core.Result()
	if before.Report.Records[0].Candidate != original || before.Report.Records[0].Verdict != v.Confirmed {
		t.Fatal("capture or receipt shares caller state")
	}
	submitted.Confirmation.Trigger = "changed"
	submitted.Citations[0].Digest = "changed"
	output := core.Result()
	output.Published[0].Finding.Content = "changed"
	output.Report.Records[0].Candidate.Finding.Content = "changed"
	output.Report.Records[0].Confirmation.Trigger = "changed"
	output.Report.Records[0].Citations[0].Digest = "changed"
	output.Report.Records[0].Submissions[0].Confirmation.Trigger = "changed"
	output.Report.Records[0].Submissions[0].Citations[0].Excerpt = "changed"
	if after := core.Result(); !reflect.DeepEqual(before, after) {
		t.Fatalf("caller mutation changes stored records: %+v", after)
	}
}
