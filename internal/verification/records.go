// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package verification

import "github.com/alibaba/open-code-review/internal/model"

const RecordVersion = 1

type Verdict string

const (
	Confirmed Verdict = "confirmed"
	Refuted   Verdict = "refuted"
	Uncertain Verdict = "uncertain"
)

type ExecutionState string

const (
	Complete   ExecutionState = "complete"
	Incomplete ExecutionState = "incomplete"
	Skipped    ExecutionState = "skipped"
)

type Origin struct {
	SessionID  string `json:"session_id"`
	GroupID    string `json:"group_id"`
	Round      int    `json:"round"`
	Checkpoint string `json:"checkpoint"`
}

type Provenance struct {
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	ContractVersion string `json:"contract_version"`
}

type Candidate struct {
	ID        string           `json:"id"`
	Finding   model.LlmComment `json:"finding"`
	Origin    Origin           `json:"origin"`
	ContextID string           `json:"context_id"`
}

type Reason struct {
	Code        string `json:"code"`
	Explanation string `json:"explanation"`
}

type Confirmation struct {
	Trigger     string `json:"trigger"`
	FailurePath string `json:"failure_path"`
	Consequence string `json:"consequence"`
}

type Citation struct {
	ID        string `json:"id"`
	ContextID string `json:"context_id"`
	State     string `json:"state"`
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Digest    string `json:"digest"`
	Excerpt   string `json:"excerpt"`
}

type Decision struct {
	CandidateID   string        `json:"candidate_id"`
	Verdict       Verdict       `json:"verdict"`
	Reason        Reason        `json:"reason"`
	Confirmation  *Confirmation `json:"confirmation,omitempty"`
	Contradiction string        `json:"contradiction,omitempty"`
	Citations     []Citation    `json:"citations,omitempty"`
}

type Record struct {
	Version       int            `json:"version"`
	Candidate     Candidate      `json:"candidate"`
	Verdict       Verdict        `json:"verdict"`
	State         ExecutionState `json:"state"`
	Reason        Reason         `json:"reason"`
	Confirmation  *Confirmation  `json:"confirmation,omitempty"`
	Contradiction string         `json:"contradiction,omitempty"`
	Citations     []Citation     `json:"citations,omitempty"`
	Provenance    Provenance     `json:"provenance"`
	Submissions   []Decision     `json:"submissions,omitempty"`
}

type Summary struct {
	Verdict Verdict        `json:"verdict"`
	State   ExecutionState `json:"state"`
	Reason  Reason         `json:"reason"`
}

type PublishedFinding struct {
	CandidateID  string           `json:"candidate_id"`
	Finding      model.LlmComment `json:"finding"`
	Verification Summary          `json:"verification"`
}

type Report struct {
	Version int      `json:"version"`
	Records []Record `json:"records"`
}

type Result struct {
	Published []PublishedFinding `json:"published"`
	Report    Report             `json:"report"`
}
