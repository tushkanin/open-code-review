// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package session

import (
	"context"
	"errors"

	"github.com/alibaba/open-code-review/internal/diff"
	v "github.com/alibaba/open-code-review/internal/verification"
)

type VerificationDisposition string

const (
	VerificationReusable VerificationDisposition = "reusable"
	VerificationRecheck  VerificationDisposition = "recheck"
	VerificationFresh    VerificationDisposition = "fresh_review"
)

type VerificationReuseRequest struct {
	Options      SessionOptions
	Admission    ResumeRequest
	Fingerprints []string
	Context      v.Context
	View         *diff.EvidenceView
	Provenance   v.Provenance
}

type VerificationReuse struct {
	Disposition VerificationDisposition
	Reason      string
	Item        ResumeItem
	Result      v.Result
	Core        *v.Core
	Recheck     []v.Candidate
}

// ReuseVerification retains coverage admission before it examines verification data.
func (s *ResumeState) ReuseVerification(ctx context.Context, req VerificationReuseRequest) (VerificationReuse, error) {
	fresh := VerificationReuse{Disposition: VerificationFresh}
	if s == nil {
		return fresh, errors.New("resume session is unavailable")
	}
	if err := s.ValidateOptions(req.Options); err != nil {
		return fresh, err
	}
	if err := s.ValidateResume(req.Admission); err != nil {
		return fresh, err
	}
	if len(req.Fingerprints) == 0 {
		return fresh, errors.New("verification reuse requires group checkpoints")
	}
	items := make([]ResumeItem, 0, len(req.Fingerprints))
	for _, fingerprint := range req.Fingerprints {
		item, ok := s.ReusableItem(fingerprint)
		if !ok {
			fresh.Reason = "The parent manifest does not authorize every group checkpoint."
			return fresh, nil
		}
		items = append(items, item)
	}
	item := items[0]
	id, err := req.Context.Identity()
	if err != nil {
		return fresh, err
	}
	if !req.Context.Matches(req.View) {
		return fresh, errors.New("current verification context does not match captured evidence")
	}
	for _, member := range items {
		if req.Context.Mode != v.Enabled || s.verification.invalid != nil || member.Verification == nil || member.Verification.Version != VerificationEventVersion || member.Verification.ContextID != id {
			fresh.Reason = "The checkpoint lacks valid matching verification context."
			return fresh, nil
		}
	}
	if _, ok := s.verification.contexts[id]; !ok {
		fresh.Reason = "The checkpoint lacks supported candidate capture."
		return fresh, nil
	}
	report := s.verification.report(id)
	for _, record := range report.Records {
		if record.State == v.Skipped {
			fresh.Reason = "The checkpoint contains unverified bypass records."
			return fresh, nil
		}
	}
	core, err := v.New(req.View, id, req.Provenance)
	if err != nil {
		return fresh, err
	}
	recheck, err := core.Restore(ctx, report)
	if err != nil {
		if ctx.Err() != nil {
			return fresh, ctx.Err()
		}
		fresh.Reason = "Stored verification support does not match captured evidence."
		return fresh, nil
	}
	result := VerificationReuse{Disposition: VerificationReusable, Item: item, Result: core.Result(), Core: core, Recheck: recheck}
	bindingsValid := true
	for _, member := range items {
		bindingsValid = bindingsValid && s.verification.bindingValid(member.Verification)
	}
	if len(recheck) > 0 || !bindingsValid {
		result.Disposition = VerificationRecheck
		result.Reason = "The candidate context remains valid, but verification requires completion."
	}
	return result, nil
}

// RecordVerificationReuse makes a child's audit independent of the parent's file.
func (sh *SessionHistory) RecordVerificationReuse(context v.Context, result v.Result) error {
	initial := v.ResultFromReport(result.Report).Report
	for i := range initial.Records {
		record := &initial.Records[i]
		record.Verdict, record.State = v.Uncertain, v.Incomplete
		record.Reason = v.Reason{Code: "not_verified", Explanation: "No verification decision is available."}
		record.Confirmation, record.Contradiction, record.Citations, record.Submissions = nil, "", nil, nil
	}
	if err := sh.CaptureVerification(context, initial); err != nil {
		return err
	}
	if err := sh.RecordVerificationSnapshot(result); err != nil {
		return err
	}
	id, err := context.Identity()
	if err != nil {
		return err
	}
	return sh.RecordVerificationTerminal(id)
}
