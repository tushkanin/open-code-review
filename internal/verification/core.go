// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package verification

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/alibaba/open-code-review/internal/diff"
	"github.com/alibaba/open-code-review/internal/model"
	"github.com/google/uuid"
)

// Core binds receipts and decisions to one captured view. Callers serialize access.
type Core struct {
	view       *diff.EvidenceView
	contextID  string
	provenance Provenance
	records    []Record
	indices    map[string]int
	receipts   map[string]Citation
}

func New(view *diff.EvidenceView, contextID string, provenance Provenance) (*Core, error) {
	if view == nil || strings.TrimSpace(contextID) == "" {
		return nil, errors.New("verification requires a captured view and context identity")
	}
	return &Core{
		view: view, contextID: contextID, provenance: provenance,
		indices: make(map[string]int), receipts: make(map[string]Citation),
	}, nil
}

// BindInvocation checks the caller's captured inputs before a verifier uses this core.
// Completed records retain the provenance of their accepted work.
func (c *Core) BindInvocation(view *diff.EvidenceView, candidates []Candidate, provenance Provenance) error {
	if view != c.view {
		return errors.New("verification core uses another captured view")
	}
	seen := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		index, ok := c.indices[candidate.ID]
		if !ok || seen[candidate.ID] || c.records[index].Candidate != candidate {
			return errors.New("verification candidate does not match the captured claim")
		}
		seen[candidate.ID] = true
	}
	for _, candidate := range candidates {
		record := &c.records[c.indices[candidate.ID]]
		if record.State == Incomplete && len(record.Submissions) == 0 {
			record.Provenance = provenance
		}
	}
	return nil
}

func (c *Core) Capture(findings []model.LlmComment, origin Origin) []Candidate {
	candidates := make([]Candidate, len(findings))
	for i, finding := range findings {
		candidate := Candidate{ID: uuid.NewString(), Finding: finding, Origin: origin, ContextID: c.contextID}
		c.indices[candidate.ID] = len(c.records)
		c.records = append(c.records, Record{
			Version: RecordVersion, Candidate: candidate, Verdict: Uncertain, State: Incomplete,
			Reason:     Reason{Code: "not_verified", Explanation: "No verification decision is available."},
			Provenance: c.provenance,
		})
		candidates[i] = candidate
	}
	return candidates
}

type Source struct {
	Evidence diff.Evidence `json:"evidence"`
	Citation *Citation     `json:"citation,omitempty"`
}

// Issue uses inclusive source lines. It never turns a partial range into a receipt.
func (c *Core) Issue(ctx context.Context, path string, start, end int) (Source, error) {
	if start < 1 || end < start {
		return Source{}, errors.New("citation requires a positive inclusive line range")
	}
	evidence, err := c.view.Read(ctx, path, start, end-start+1)
	result := Source{Evidence: evidence}
	if err != nil || !evidence.Available {
		return result, err
	}
	if evidence.StartLine != start || evidence.EndLine != end {
		return result, errors.New("citation range is outside captured source")
	}
	citation := Citation{
		ID: uuid.NewString(), ContextID: c.contextID, State: evidence.State,
		Path: evidence.Path, StartLine: evidence.StartLine, EndLine: evidence.EndLine,
		Digest: evidence.Digest, Excerpt: evidence.Content,
	}
	c.receipts[citation.ID] = citation
	result.Citation = &citation
	return result, nil
}

// Submit accepts partial batches. Conflicts remain uncertain across batches as well.
func (c *Core) Submit(decisions []Decision) {
	changed := make(map[int]struct{}, len(decisions))
	for _, decision := range decisions {
		index, ok := c.indices[decision.CandidateID]
		if !ok {
			continue
		}
		c.records[index].Submissions = append(c.records[index].Submissions, cloneDecision(decision))
		changed[index] = struct{}{}
	}
	for index := range changed {
		c.validate(&c.records[index])
	}
}

func (c *Core) validate(record *Record) {
	record.Verdict, record.State = Uncertain, Complete
	record.Confirmation, record.Contradiction, record.Citations = nil, "", nil
	decision := record.Submissions[0]
	for _, other := range record.Submissions[1:] {
		if !sameDecision(decision, other) {
			record.Reason = Reason{Code: "conflicting_decisions", Explanation: "The verifier submits conflicting decisions for this candidate."}
			return
		}
	}
	if reason := c.invalidReason(decision); reason != "" {
		record.Reason = Reason{Code: "invalid_decision", Explanation: reason}
		return
	}
	record.Verdict, record.Reason = decision.Verdict, decision.Reason
	record.Confirmation, record.Contradiction, record.Citations = decision.Confirmation, decision.Contradiction, decision.Citations
}

func (c *Core) invalidReason(decision Decision) string {
	if strings.TrimSpace(decision.Reason.Code) == "" || strings.TrimSpace(decision.Reason.Explanation) == "" {
		return "The decision lacks a reason code or explanation."
	}
	if decision.Verdict != Confirmed && decision.Verdict != Refuted && decision.Verdict != Uncertain {
		return "The decision has an unknown verdict."
	}
	for _, citation := range decision.Citations {
		issued, ok := c.receipts[citation.ID]
		if !ok || citation != issued {
			return "The decision cites unknown, altered, or wrong-context evidence."
		}
	}
	if decision.Verdict == Uncertain {
		return ""
	}
	if len(decision.Citations) == 0 {
		return "The decision lacks an issued source citation."
	}
	if decision.Verdict == Confirmed {
		details := decision.Confirmation
		if details == nil || strings.TrimSpace(details.Trigger) == "" || strings.TrimSpace(details.FailurePath) == "" || strings.TrimSpace(details.Consequence) == "" {
			return "Confirmation lacks a trigger, failure path, or observable consequence."
		}
	}
	if decision.Verdict == Refuted && strings.TrimSpace(decision.Contradiction) == "" {
		return "Refutation lacks an explanation of how the evidence contradicts the central claim."
	}
	return ""
}

// Finish closes a complete investigation, including candidates without decisions.
func (c *Core) Finish(candidateIDs ...string) {
	c.undecided(Complete, Reason{Code: "missing_decision", Explanation: "The verifier completes work without a decision for this candidate."}, candidateIDs)
}

func (c *Core) Fail(explanation string, candidateIDs ...string) {
	c.undecided(Incomplete, Reason{Code: "verification_failed", Explanation: nonempty(explanation, "Verification fails before a decision is available.")}, candidateIDs)
}

func (c *Core) Exhaust(explanation string, candidateIDs ...string) {
	c.undecided(Incomplete, Reason{Code: "limit_exhausted", Explanation: nonempty(explanation, "Verification reaches its execution limit.")}, candidateIDs)
}

func (c *Core) Bypass(candidateIDs ...string) {
	c.undecided(Skipped, Reason{Code: "verification_disabled", Explanation: "Verification is disabled; this candidate is unverified."}, candidateIDs)
}

func (c *Core) undecided(state ExecutionState, reason Reason, candidateIDs []string) {
	apply := func(record *Record) {
		if record.State == Incomplete && len(record.Submissions) == 0 {
			record.Verdict, record.State, record.Reason = Uncertain, state, reason
		}
	}
	if len(candidateIDs) == 0 {
		for i := range c.records {
			apply(&c.records[i])
		}
		return
	}
	for _, id := range candidateIDs {
		if index, ok := c.indices[id]; ok {
			apply(&c.records[index])
		}
	}
}

func (c *Core) Result() Result {
	return ResultFromReport(Report{Version: RecordVersion, Records: c.records})
}

func nonempty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func sameDecision(a, b Decision) bool {
	if a.CandidateID != b.CandidateID || a.Verdict != b.Verdict || a.Reason != b.Reason || a.Contradiction != b.Contradiction || !slices.Equal(a.Citations, b.Citations) {
		return false
	}
	if a.Confirmation == nil || b.Confirmation == nil {
		return a.Confirmation == b.Confirmation
	}
	return *a.Confirmation == *b.Confirmation
}

func cloneDecision(decision Decision) Decision {
	decision.Citations = slices.Clone(decision.Citations)
	if decision.Confirmation != nil {
		confirmation := *decision.Confirmation
		decision.Confirmation = &confirmation
	}
	return decision
}

func CloneRecord(record Record) Record {
	record.Citations = slices.Clone(record.Citations)
	if record.Confirmation != nil {
		confirmation := *record.Confirmation
		record.Confirmation = &confirmation
	}
	record.Submissions = slices.Clone(record.Submissions)
	for i := range record.Submissions {
		record.Submissions[i] = cloneDecision(record.Submissions[i])
	}
	return record
}
