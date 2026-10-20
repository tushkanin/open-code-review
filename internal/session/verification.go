// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package session

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"

	"github.com/alibaba/open-code-review/internal/model"
	v "github.com/alibaba/open-code-review/internal/verification"
)

const VerificationEventVersion = "ocr.verification/v1"

type VerificationRevision struct {
	CandidateID string `json:"candidate_id"`
	Revision    int    `json:"revision"`
	Digest      string `json:"digest"`
}

type VerificationBinding struct {
	Version   string                 `json:"version"`
	ContextID string                 `json:"context_id"`
	Revisions []VerificationRevision `json:"revisions"`
}

type verificationEvent struct {
	Version   string                 `json:"version"`
	Context   *v.Context             `json:"context,omitempty"`
	Records   []v.Record             `json:"records,omitempty"`
	Revisions []VerificationRevision `json:"revisions"`
	ContextID string                 `json:"context_id"`
}

type storedVerification struct {
	record   v.Record
	revision int
	digest   string
	terminal bool
}

type verificationState struct {
	contexts map[string]v.Context
	records  map[string]storedVerification
	order    []string
	invalid  error
}

func (s *verificationState) init() {
	if s.contexts == nil {
		s.contexts = make(map[string]v.Context)
		s.records = make(map[string]storedVerification)
	}
}

func (s *verificationState) fail(err error) {
	if s.invalid == nil {
		s.invalid = err
	}
}

func (s *verificationState) references(contextID string) []VerificationRevision {
	refs := []VerificationRevision{}
	for _, id := range s.order {
		record := s.records[id]
		if record.record.Candidate.ContextID == contextID {
			refs = append(refs, VerificationRevision{id, record.revision, record.digest})
		}
	}
	return refs
}

func (s *verificationState) apply(kind string, event verificationEvent) error {
	s.init()
	if event.Version != VerificationEventVersion || event.ContextID == "" {
		return errors.New("unsupported or incomplete verification event")
	}
	if kind == "verification_capture" {
		if event.Context == nil {
			return errors.New("capture lacks verification context")
		}
		id, err := event.Context.Identity()
		if err != nil || id != event.ContextID {
			return errors.New("capture context identity does not match")
		}
	} else if _, ok := s.contexts[event.ContextID]; !ok {
		return errors.New("verification event lacks prior capture")
	}
	if kind != "verification_capture" && event.Context != nil {
		return errors.New("only capture can define verification context")
	}
	if kind == "verification_terminal" {
		if event.Context != nil || len(event.Records) != 0 {
			return errors.New("terminal event contains unexpected records")
		}
		seen := make(map[string]bool)
		for _, ref := range event.Revisions {
			record, ok := s.records[ref.CandidateID]
			if !ok || seen[ref.CandidateID] || record.record.Candidate.ContextID != event.ContextID || record.revision != ref.Revision || record.digest != ref.Digest {
				return errors.New("terminal event references an unknown revision")
			}
			seen[ref.CandidateID] = true
		}
		for _, ref := range event.Revisions {
			record := s.records[ref.CandidateID]
			record.terminal = true
			s.records[ref.CandidateID] = record
		}
		return nil
	}
	if kind != "verification_capture" && kind != "verification_decision" {
		return errors.New("unknown verification event")
	}
	if len(event.Records) != len(event.Revisions) {
		return errors.New("verification revisions do not match records")
	}
	seen := make(map[string]bool)
	for i, record := range event.Records {
		if err := v.ValidateRecord(record); err != nil {
			return err
		}
		id := record.Candidate.ID
		ref := event.Revisions[i]
		if seen[id] || ref.CandidateID != id || record.Candidate.ContextID != event.ContextID || ref.Digest != recordDigest(record) {
			return errors.New("verification record identity does not match event")
		}
		seen[id] = true
		previous, exists := s.records[id]
		if kind == "verification_capture" {
			if exists || ref.Revision != 1 || record.State != v.Incomplete || record.Reason.Code != "not_verified" {
				return errors.New("capture is not a new undecided claim")
			}
		} else {
			if !exists || ref.Revision != previous.revision+1 || previous.record.Candidate != record.Candidate {
				return errors.New("decision changes its immutable claim or revision")
			}
			if len(record.Submissions) < len(previous.record.Submissions) || !reflect.DeepEqual(record.Submissions[:len(previous.record.Submissions)], previous.record.Submissions) {
				// Empty JSON arrays and nil slices both represent no submissions.
				if len(previous.record.Submissions) != 0 {
					return errors.New("decision erases prior submissions")
				}
			}
			if previous.record.State != v.Incomplete && (record.State != v.Complete || record.Provenance != previous.record.Provenance) {
				return errors.New("decision changes completed work or provenance")
			}
		}
		context := s.contexts[event.ContextID]
		if event.Context != nil {
			context = *event.Context
		}
		for _, citation := range record.Citations {
			if citation.State != context.EvidenceState {
				return errors.New("citation uses another captured state")
			}
		}
	}
	if event.Context != nil {
		context := *event.Context
		context.Diffs = slices.Clone(context.Diffs)
		s.contexts[event.ContextID] = context
	}
	for i, record := range event.Records {
		id := record.Candidate.ID
		if kind == "verification_capture" {
			s.order = append(s.order, id)
		}
		owned := v.CloneRecord(record)
		s.records[id] = storedVerification{record: owned, revision: event.Revisions[i].Revision, digest: event.Revisions[i].Digest}
	}
	return nil
}

func (s *verificationState) report(contextID string) v.Report {
	report := v.Report{Version: v.RecordVersion, Records: []v.Record{}}
	for _, id := range s.order {
		record := s.records[id].record
		if contextID == "" || record.Candidate.ContextID == contextID {
			report.Records = append(report.Records, record)
		}
	}
	return report
}

func (s *verificationState) bindingValid(binding *VerificationBinding) bool {
	if binding == nil || s.invalid != nil || binding.Version != VerificationEventVersion {
		return false
	}
	if _, ok := s.contexts[binding.ContextID]; !ok {
		return false
	}
	if !slices.Equal(binding.Revisions, s.references(binding.ContextID)) {
		return false
	}
	for _, ref := range binding.Revisions {
		if !s.records[ref.CandidateID].terminal {
			return false
		}
	}
	return true
}

func recordDigest(record v.Record) string {
	data, _ := json.Marshal(record)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func (sh *SessionHistory) writeVerificationLocked(kind string, event verificationEvent, fields map[string]any) error {
	if sh.verificationErr != nil {
		return sh.verificationErr
	}
	if sh.persist == nil {
		sh.verificationErr = errors.New("verification persistence is unavailable")
		if sh.persistInitErr != nil {
			sh.verificationErr = sh.persistInitErr
		}
		return sh.verificationErr
	}
	if fields == nil {
		fields = map[string]any{"verification": event}
	}
	err := sh.persist.writeVerification(kind, fields)
	if err != nil {
		sh.verificationErr = err
	}
	return err
}

func (sh *SessionHistory) CaptureVerification(context v.Context, report v.Report) error {
	if sh == nil {
		return errors.New("verification persistence is unavailable")
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if sh.verificationErr != nil {
		return sh.verificationErr
	}
	id, err := context.Identity()
	if err != nil {
		return err
	}
	if report.Version != v.RecordVersion {
		return errors.New("unsupported verification report")
	}
	event := verificationEvent{Version: VerificationEventVersion, Context: &context, ContextID: id, Records: report.Records}
	for _, record := range report.Records {
		event.Revisions = append(event.Revisions, VerificationRevision{record.Candidate.ID, 1, recordDigest(record)})
	}
	if err := sh.verification.apply("verification_capture", event); err != nil {
		return err
	}
	return sh.writeVerificationLocked("verification_capture", event, nil)
}

// RecordVerificationSnapshot accepts a scoped OnResult snapshot, not a whole-session replacement.
func (sh *SessionHistory) RecordVerificationSnapshot(result v.Result) error {
	if sh == nil {
		return errors.New("verification persistence is unavailable")
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if sh.verificationErr != nil {
		return sh.verificationErr
	}
	if result.Report.Version != v.RecordVersion {
		return errors.New("unsupported verification report")
	}
	if len(result.Report.Records) == 0 {
		return nil
	}
	event := verificationEvent{Version: VerificationEventVersion, ContextID: result.Report.Records[0].Candidate.ContextID}
	for _, record := range result.Report.Records {
		previous, ok := sh.verification.records[record.Candidate.ID]
		if !ok {
			return errors.New("snapshot lacks prior candidate capture")
		}
		if reflect.DeepEqual(previous.record, record) {
			continue
		}
		event.Records = append(event.Records, record)
		event.Revisions = append(event.Revisions, VerificationRevision{record.Candidate.ID, previous.revision + 1, recordDigest(record)})
	}
	if len(event.Records) == 0 {
		return nil
	}
	if err := sh.verification.apply("verification_decision", event); err != nil {
		return err
	}
	return sh.writeVerificationLocked("verification_decision", event, nil)
}

func (sh *SessionHistory) RecordVerificationTerminal(contextID string, candidateIDs ...string) error {
	if sh == nil {
		return errors.New("verification persistence is unavailable")
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	refs := sh.verification.references(contextID)
	if len(candidateIDs) > 0 {
		refs = nil
		for _, id := range candidateIDs {
			record := sh.verification.records[id]
			refs = append(refs, VerificationRevision{id, record.revision, record.digest})
		}
	}
	event := verificationEvent{Version: VerificationEventVersion, ContextID: contextID, Revisions: refs}
	if err := sh.verification.apply("verification_terminal", event); err != nil {
		return err
	}
	return sh.writeVerificationLocked("verification_terminal", event, nil)
}

func (sh *SessionHistory) RecordVerificationCheckpoint(item ResumeItem, contextID, sourceSessionID string) error {
	if sh == nil {
		return errors.New("verification persistence is unavailable")
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if sh.verificationErr != nil {
		return sh.verificationErr
	}
	binding := &VerificationBinding{Version: VerificationEventVersion, ContextID: contextID, Revisions: sh.verification.references(contextID)}
	if item.Fingerprint == "" || !sh.verification.bindingValid(binding) {
		return errors.New("verification checkpoint lacks an exact terminal inventory")
	}
	if item.FilePath == "" {
		item.FilePath = item.NewPath
	}
	item.Comments = verificationComments(v.ResultFromReport(sh.verification.report(contextID)), item.FilePath)
	kind := "review_item_done"
	if sourceSessionID != "" {
		kind = "review_item_reused"
	}
	fields := map[string]any{"filePath": item.FilePath, "oldPath": item.OldPath, "newPath": item.NewPath, "fingerprint": item.Fingerprint, "comments": item.Comments, "verification": binding}
	if sourceSessionID != "" {
		fields["sourceSessionId"] = sourceSessionID
	}
	return sh.writeVerificationLocked(kind, verificationEvent{}, fields)
}

func (s *ResumeState) VerificationContext(contextID string) (v.Context, bool) {
	if s == nil {
		return v.Context{}, false
	}
	saved, ok := s.verification.contexts[contextID]
	saved.Diffs = slices.Clone(saved.Diffs)
	return saved, ok
}

func verificationComments(result v.Result, filePath string) []model.LlmComment {
	comments := []model.LlmComment{}
	checkpoints := make(map[string]string, len(result.Report.Records))
	for _, record := range result.Report.Records {
		checkpoints[record.Candidate.ID] = record.Candidate.Origin.Checkpoint
	}
	for _, finding := range result.Published {
		if finding.Finding.Path == filePath || (finding.Finding.Path == "" && checkpoints[finding.CandidateID] == filePath) {
			comments = append(comments, finding.Finding)
		}
	}
	return comments
}

func (s *ResumeState) applyVerification(kind string, raw json.RawMessage) error {
	var event verificationEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		s.verification.fail(err)
		return err
	}
	if err := s.verification.apply(kind, event); err != nil {
		s.verification.fail(err)
		return fmt.Errorf("invalid verification event: %w", err)
	}
	return nil
}

// VerificationResult returns intact audit records even when corruption prevents trusted reuse.
func (s *ResumeState) VerificationResult() (v.Result, error) {
	if s == nil {
		return v.Result{}, errors.New("session is unavailable")
	}
	result := v.ResultFromReport(s.verification.report(""))
	if s.verification.invalid != nil {
		result.Published = []v.PublishedFinding{}
		for _, record := range result.Report.Records {
			result.Published = append(result.Published, v.PublishedFinding{CandidateID: record.Candidate.ID, Finding: record.Candidate.Finding, Verification: v.Summary{Verdict: v.Uncertain, State: v.Incomplete, Reason: v.Reason{Code: "invalid_persistence", Explanation: "The session contains invalid verification data."}}})
		}
	}
	return result, s.verification.invalid
}

func LoadVerificationResult(repoDir, sessionID string) (v.Result, error) {
	state, err := LoadReviewResumeState(repoDir, sessionID)
	if err != nil {
		return v.Result{}, err
	}
	return state.VerificationResult()
}

// savedCheckpoints keeps display order separate from fingerprint-based reuse admission.
func (s *ResumeState) savedCheckpoints() []ResumeItem {
	items := make([]ResumeItem, 0, len(s.Items)+len(s.legacyItems))
	if s.displayOrder != nil {
		legacy := 0
		for _, fingerprint := range s.displayOrder {
			if fingerprint == "" {
				items = append(items, s.legacyItems[legacy])
				legacy++
				continue
			}
			if item, ok := s.Items[fingerprint]; ok {
				items = append(items, item)
			}
		}
		return items
	}
	keys := make([]string, 0, len(s.Items))
	for key := range s.Items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		items = append(items, s.Items[key])
	}
	return items
}

// SavedFindings preserves candidate identity and includes claims without a final checkpoint.
func (s *ResumeState) SavedFindings() ([]v.PublishedFinding, v.Report, error) {
	result, auditErr := s.VerificationResult()
	if s == nil {
		return nil, result.Report, auditErr
	}
	items := s.savedCheckpoints()
	trusted := make(map[string]bool)
	bound := make(map[string]bool)
	invalid := make(map[string]bool)
	legacyPaths := make(map[string]bool)
	for _, item := range items {
		if item.Verification == nil {
			legacyPaths[item.FilePath] = true
			continue
		}
		bound[item.Verification.ContextID] = true
		if s.verification.bindingValid(item.Verification) {
			trusted[item.Verification.ContextID] = true
		} else {
			for _, ref := range item.Verification.Revisions {
				invalid[ref.CandidateID] = true
			}
		}
	}
	unverified := v.Summary{Verdict: v.Uncertain, State: v.Incomplete, Reason: v.Reason{Code: "unverified_session", Explanation: "The saved finding has no valid verification checkpoint."}}
	pathOf := func(candidate *v.Candidate) string {
		if candidate.Finding.Path != "" {
			return candidate.Finding.Path
		}
		return candidate.Origin.Checkpoint
	}
	isTrusted := func(record *v.Record) bool {
		return auditErr == nil && !invalid[record.Candidate.ID] && (trusted[record.Candidate.ContextID] || (!bound[record.Candidate.ContextID] && !legacyPaths[pathOf(&record.Candidate)] && s.verification.records[record.Candidate.ID].terminal))
	}
	records := result.Report.Records
	used := make([]bool, len(records))
	out := []v.PublishedFinding{}
	appendCandidate := func(index int) {
		record := &records[index]
		finding := v.PublishedFinding{CandidateID: record.Candidate.ID, Finding: record.Candidate.Finding, Verification: unverified}
		if isTrusted(record) {
			finding.Verification = v.Summary{Verdict: record.Verdict, State: record.State, Reason: record.Reason}
		}
		finding.Finding.Path = pathOf(&record.Candidate)
		out = append(out, finding)
		used[index] = true
	}
	for i := range records {
		if isTrusted(&records[i]) && records[i].Verdict == v.Refuted {
			used[i] = true
		}
	}
	for _, item := range items {
		if auditErr == nil && s.verification.bindingValid(item.Verification) {
			for i := range records {
				if !used[i] && records[i].Candidate.ContextID == item.Verification.ContextID && pathOf(&records[i].Candidate) == item.FilePath {
					appendCandidate(i)
				}
			}
			continue
		}
		for _, claim := range item.Comments {
			matched := false
			for i := range records {
				if !used[i] && !isTrusted(&records[i]) && records[i].Candidate.Finding == claim && pathOf(&records[i].Candidate) == item.FilePath {
					appendCandidate(i)
					matched = true
					break
				}
			}
			if !matched {
				if claim.Path == "" {
					claim.Path = item.FilePath
				}
				out = append(out, v.PublishedFinding{Finding: claim, Verification: unverified})
			}
		}
	}
	for i := range records {
		if !used[i] {
			appendCandidate(i)
		}
	}
	return out, result.Report, auditErr
}
