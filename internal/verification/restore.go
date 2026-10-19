// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package verification

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ValidateRecord checks stored structure without trusting rejected submissions as support.
func ValidateRecord(record Record) error {
	if record.Version != RecordVersion || record.Candidate.ID == "" || record.Candidate.ContextID == "" || strings.TrimSpace(record.Reason.Code) == "" || strings.TrimSpace(record.Reason.Explanation) == "" {
		return errors.New("unsupported or incomplete verification record")
	}
	if record.Verdict != Confirmed && record.Verdict != Refuted && record.Verdict != Uncertain {
		return errors.New("unknown stored verdict")
	}
	if record.State != Complete && record.State != Incomplete && record.State != Skipped {
		return errors.New("unknown stored execution state")
	}
	for _, decision := range record.Submissions {
		if decision.CandidateID != record.Candidate.ID {
			return errors.New("stored submission belongs to another candidate")
		}
	}
	noSupport := record.Confirmation == nil && record.Contradiction == "" && len(record.Citations) == 0
	if record.State != Complete {
		if record.Verdict != Uncertain || !noSupport || len(record.Submissions) != 0 {
			return errors.New("unfinished record contains an accepted decision")
		}
		if record.State == Skipped && record.Reason.Code != "verification_disabled" {
			return errors.New("skipped record lacks bypass reason")
		}
		return nil
	}
	if len(record.Submissions) == 0 {
		if record.Verdict != Uncertain || !noSupport || record.Reason.Code != "missing_decision" {
			return errors.New("complete record lacks a submitted decision")
		}
		return nil
	}
	conflict := false
	for _, other := range record.Submissions[1:] {
		conflict = conflict || !sameDecision(record.Submissions[0], other)
	}
	if conflict {
		if record.Verdict != Uncertain || !noSupport || record.Reason.Code != "conflicting_decisions" {
			return errors.New("conflicting submissions contain an accepted decision")
		}
		return nil
	}
	checker := &Core{receipts: make(map[string]Citation)}
	for _, citation := range record.Citations {
		if citation.ID == "" || citation.ContextID != record.Candidate.ContextID || citation.State == "" || citation.Path == "" || citation.StartLine < 1 || citation.EndLine < citation.StartLine || citation.Digest == "" {
			return errors.New("invalid stored citation identity")
		}
		if previous, ok := checker.receipts[citation.ID]; ok && previous != citation {
			return errors.New("conflicting stored citation identities")
		}
		checker.receipts[citation.ID] = citation
	}
	decision := record.Submissions[0]
	if record.Reason.Code == "invalid_decision" && record.Verdict == Uncertain && noSupport && checker.invalidReason(decision) != "" {
		return nil
	}
	if checker.invalidReason(decision) != "" || decision.Verdict != record.Verdict || decision.Reason != record.Reason || decision.Contradiction != record.Contradiction || !slices.Equal(decision.Citations, record.Citations) {
		return errors.New("stored decision does not match accepted support")
	}
	if (decision.Confirmation == nil) != (record.Confirmation == nil) || (decision.Confirmation != nil && *decision.Confirmation != *record.Confirmation) {
		return errors.New("stored confirmation differs from its submission")
	}
	return nil
}

// Restore installs all records atomically. Historical receipts do not grant delivery to a new conversation.
func (c *Core) Restore(ctx context.Context, report Report) ([]Candidate, error) {
	if report.Version != RecordVersion {
		return nil, errors.New("unsupported verification report")
	}
	staged, err := New(c.view, c.contextID, c.provenance)
	if err != nil {
		return nil, err
	}
	staged.records = make([]Record, len(c.records), len(c.records)+len(report.Records))
	copy(staged.records, c.records)
	for id, index := range c.indices {
		staged.indices[id] = index
	}
	for id, citation := range c.receipts {
		staged.receipts[id] = citation
	}
	recheck := []Candidate{}
	for _, record := range report.Records {
		if err := ValidateRecord(record); err != nil {
			return nil, err
		}
		if record.Candidate.ContextID != c.contextID {
			return nil, errors.New("stored candidate uses another verification context")
		}
		if _, exists := staged.indices[record.Candidate.ID]; exists {
			return nil, errors.New("duplicate stored candidate identity")
		}
		for _, citation := range record.Citations {
			if previous, ok := staged.receipts[citation.ID]; ok {
				if previous != citation {
					return nil, errors.New("stored receipt conflicts with an existing receipt")
				}
				continue
			}
			evidence, err := c.view.Read(ctx, citation.Path, citation.StartLine, citation.EndLine-citation.StartLine+1)
			if err != nil {
				return nil, err
			}
			if !evidence.Available || citation.ContextID != c.contextID || evidence.State != citation.State || evidence.Path != citation.Path || evidence.StartLine != citation.StartLine || evidence.EndLine != citation.EndLine || evidence.Digest != citation.Digest || evidence.Content != citation.Excerpt {
				return nil, fmt.Errorf("stored citation %q does not match captured source", citation.ID)
			}
			staged.receipts[citation.ID] = citation
		}
		staged.indices[record.Candidate.ID] = len(staged.records)
		staged.records = append(staged.records, CloneRecord(record))
		if record.State == Incomplete {
			recheck = append(recheck, record.Candidate)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.records, c.indices, c.receipts = staged.records, staged.indices, staged.receipts
	return recheck, nil
}

func ResultFromReport(report Report) Result {
	result := Result{Published: make([]PublishedFinding, 0, len(report.Records)), Report: Report{Version: report.Version, Records: make([]Record, len(report.Records))}}
	for i, record := range report.Records {
		result.Report.Records[i] = CloneRecord(record)
		if record.Verdict != Refuted {
			result.Published = append(result.Published, PublishedFinding{CandidateID: record.Candidate.ID, Finding: record.Candidate.Finding, Verification: Summary{Verdict: record.Verdict, State: record.State, Reason: record.Reason}})
		}
	}
	return result
}
