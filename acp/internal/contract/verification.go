// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package contract

// AuditClaim excludes private reasoning, including fields from newer providers.
type AuditClaim struct {
	Path           string `json:"path"`
	Content        string `json:"content"`
	SuggestionCode string `json:"suggestion_code,omitempty"`
	ExistingCode   string `json:"existing_code,omitempty"`
	StartLine      int    `json:"start_line"`
	EndLine        int    `json:"end_line"`
	Category       string `json:"category,omitempty"`
	Severity       string `json:"severity,omitempty"`
}

type VerificationReason struct {
	Code        string `json:"code"`
	Explanation string `json:"explanation"`
}

type VerificationSummary struct {
	Verdict string             `json:"verdict"`
	State   string             `json:"state"`
	Reason  VerificationReason `json:"reason"`
}

type VerificationCompletion struct {
	Complete   int                  `json:"complete"`
	Incomplete int                  `json:"incomplete"`
	Skipped    int                  `json:"skipped"`
	Reasons    []VerificationReason `json:"reasons"`
}

type VerificationCandidate struct {
	ID        string             `json:"id"`
	Finding   AuditClaim         `json:"finding"`
	Origin    VerificationOrigin `json:"origin"`
	ContextID string             `json:"context_id"`
}

type VerificationOrigin struct {
	SessionID  string `json:"session_id"`
	GroupID    string `json:"group_id"`
	Round      int    `json:"round"`
	Checkpoint string `json:"checkpoint"`
}

type VerificationProvenance struct {
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	ContractVersion string `json:"contract_version"`
}

type VerificationConfirmation struct {
	Trigger     string `json:"trigger"`
	FailurePath string `json:"failure_path"`
	Consequence string `json:"consequence"`
}

type VerificationCitation struct {
	ID        string `json:"id"`
	ContextID string `json:"context_id"`
	State     string `json:"state"`
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Digest    string `json:"digest"`
	Excerpt   string `json:"excerpt"`
}

type VerificationDecision struct {
	CandidateID   string                    `json:"candidate_id"`
	Verdict       string                    `json:"verdict"`
	Reason        VerificationReason        `json:"reason"`
	Confirmation  *VerificationConfirmation `json:"confirmation,omitempty"`
	Contradiction string                    `json:"contradiction,omitempty"`
	Citations     []VerificationCitation    `json:"citations,omitempty"`
}

type VerificationRecord struct {
	Version       int                       `json:"version"`
	Candidate     VerificationCandidate     `json:"candidate"`
	Verdict       string                    `json:"verdict"`
	State         string                    `json:"state"`
	Reason        VerificationReason        `json:"reason"`
	Confirmation  *VerificationConfirmation `json:"confirmation,omitempty"`
	Contradiction string                    `json:"contradiction,omitempty"`
	Citations     []VerificationCitation    `json:"citations,omitempty"`
	Provenance    VerificationProvenance    `json:"provenance"`
	Submissions   []VerificationDecision    `json:"submissions,omitempty"`
}

type VerificationReport struct {
	Version int                  `json:"version"`
	Records []VerificationRecord `json:"records"`
}
