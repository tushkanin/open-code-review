// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/alibaba/open-code-review/internal/llmloop"
	"github.com/alibaba/open-code-review/internal/model"
	"github.com/alibaba/open-code-review/internal/session"
	v "github.com/alibaba/open-code-review/internal/verification"
)

type groupVerification struct {
	mu      sync.Mutex
	context v.Context
	id      string
	core    *v.Core
}

func (a *Agent) groupVerification(g FileGroup) (*groupVerification, error) {
	key := fileGroupKey(g.Diffs)
	a.verificationMu.Lock()
	defer a.verificationMu.Unlock()
	if state := a.verificationGroups[key]; state != nil {
		return state, nil
	}
	contract, err := a.runner.VerificationContract()
	if err != nil {
		contract.Fingerprint = hashFields("unavailable_verifier_contract", err.Error())
	}
	mode := v.Enabled
	if a.args.SkipFilter {
		mode = v.Disabled
	}
	binding, err := v.NewContext(a.evidence, v.ContextInputs{Diffs: g.Diffs, GroupContext: key, Scope: a.reviewMode(), Rules: a.resolveGroupSystemRule(g.Diffs), Background: a.args.Background, ContractFingerprint: contract.Fingerprint, Mode: mode})
	if err != nil {
		return nil, err
	}
	id, err := binding.Identity()
	if err != nil {
		return nil, err
	}
	core, err := v.New(a.evidence, id, v.Provenance{Provider: a.args.Provider, Model: a.args.Model, ContractVersion: contract.Fingerprint})
	if err != nil {
		return nil, err
	}
	state := &groupVerification{context: binding, id: id, core: core}
	if a.verificationGroups == nil {
		a.verificationGroups = make(map[string]*groupVerification)
	}
	a.verificationGroups[key] = state
	return state, nil
}

func (s *groupVerification) result() v.Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.core.Result()
}

func (s *groupVerification) feedback() string {
	records := s.result().Report.Records
	if len(records) == 0 {
		return ""
	}
	data, _ := json.Marshal(records)
	return "Do not repeat these earlier claims. Their actual verification dispositions follow:\n" + string(data)
}

func (a *Agent) VerificationResult() v.Result {
	a.verificationMu.Lock()
	defer a.verificationMu.Unlock()
	keys := make([]string, 0, len(a.verificationGroups))
	for key := range a.verificationGroups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	report := v.Report{Version: v.RecordVersion, Records: []v.Record{}}
	for _, key := range keys {
		report.Records = append(report.Records, a.verificationGroups[key].result().Report.Records...)
	}
	return v.ResultFromReport(report)
}

func (a *Agent) publishedComments() []model.LlmComment {
	result := a.VerificationResult()
	comments := make([]model.LlmComment, 0, len(result.Published))
	for _, finding := range result.Published {
		comments = append(comments, finding.Finding)
	}
	return comments
}

func (a *Agent) verificationError(err error, group string) {
	if err != nil {
		a.recordWarning("verification_persistence", group, err.Error())
	}
}

func (a *Agent) captureRound(ctx context.Context, g FileGroup, s *groupVerification, round int, claims []model.LlmComment, completed bool, stop llmloop.MainLoopStop, discoveryErr error) {
	s.mu.Lock()
	var candidates []v.Candidate
	for _, claim := range claims {
		checkpoint := claim.Path
		if checkpoint == "" {
			checkpoint = g.Diffs[0].NewPath
		}
		candidates = append(candidates, s.core.Capture([]model.LlmComment{claim}, v.Origin{SessionID: a.session.SessionID, GroupID: fileGroupKey(g.Diffs), Round: round, Checkpoint: checkpoint})...)
	}
	all := s.core.Result()
	s.mu.Unlock()
	ids := make([]string, 0, len(candidates))
	selected := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
		selected[candidate.ID] = true
	}
	capture := v.Report{Version: v.RecordVersion, Records: []v.Record{}}
	for _, record := range all.Report.Records {
		if selected[record.Candidate.ID] {
			capture.Records = append(capture.Records, record)
		}
	}
	captureErr := a.session.CaptureVerification(s.context, capture)
	a.verificationError(captureErr, fileGroupKey(g.Diffs))
	if len(candidates) > 0 {
		switch {
		case captureErr != nil:
			s.mu.Lock()
			s.core.Fail("Candidate persistence fails: "+captureErr.Error(), ids...)
			s.mu.Unlock()
		case a.args.SkipFilter:
			s.mu.Lock()
			s.core.Bypass(ids...)
			s.mu.Unlock()
		case discoveryErr != nil || ctx.Err() != nil || !completed:
			reason := "Discovery stops before verification: " + stop.Reason()
			if discoveryErr != nil {
				reason = "Discovery fails before verification: " + discoveryErr.Error()
			} else if ctx.Err() != nil {
				reason = ctx.Err().Error()
			}
			s.mu.Lock()
			s.core.Fail(reason, ids...)
			s.mu.Unlock()
		default:
			_, err := a.runner.RunVerification(ctx, llmloop.VerifierInput{Core: s.core, CoreMu: &s.mu, View: a.evidence, Candidates: candidates, GroupID: fileGroupKey(g.Diffs), Round: round, Diffs: g.Diffs, Scope: s.context.Scope, Rules: s.context.Rules, Background: s.context.Background, OnResult: a.session.RecordVerificationSnapshot})
			if err != nil {
				s.mu.Lock()
				s.core.Fail(err.Error(), ids...)
				s.mu.Unlock()
				a.recordWarning("verification_incomplete", fileGroupKey(g.Diffs), err.Error())
			}
		}
	}
	a.verificationError(a.session.RecordVerificationSnapshot(s.result()), fileGroupKey(g.Diffs))
	a.verificationError(a.session.RecordVerificationTerminal(s.id), fileGroupKey(g.Diffs))
}

func (a *Agent) checkpointVerification(g FileGroup, d model.Diff, source string) {
	s, err := a.groupVerification(g)
	if err != nil {
		a.verificationError(err, fileGroupKey(g.Diffs))
		return
	}
	a.verificationError(a.session.RecordVerificationCheckpoint(session.ResumeItem{FilePath: d.NewPath, OldPath: d.OldPath, NewPath: d.NewPath, Fingerprint: reviewItemFingerprint(a.reviewMode(), d)}, s.id, source), fileGroupKey(g.Diffs))
}

func (a *Agent) reuseGroup(ctx context.Context, g FileGroup) bool {
	if a.args.Resume == nil || a.args.ResumeAdmission == nil || a.args.SkipFilter {
		return false
	}
	s, err := a.groupVerification(g)
	if err != nil {
		a.recordWarning("verification_context", fileGroupKey(g.Diffs), err.Error())
		return false
	}
	fingerprints := make([]string, 0, len(g.Diffs))
	for _, d := range g.Diffs {
		fingerprints = append(fingerprints, reviewItemFingerprint(a.reviewMode(), d))
	}
	reuse, err := a.args.Resume.ReuseVerification(ctx, session.VerificationReuseRequest{Options: session.SessionOptions{ReviewMode: a.reviewMode(), DiffFrom: a.args.From, DiffTo: a.args.To, DiffCommit: a.args.Commit}, Admission: *a.args.ResumeAdmission, Fingerprints: fingerprints, Context: s.context, View: a.evidence, Provenance: v.Provenance{Provider: a.args.Provider, Model: a.args.Model, ContractVersion: s.context.ContractFingerprint}})
	if err != nil || reuse.Disposition == session.VerificationFresh {
		return false
	}
	s.mu.Lock()
	s.core = reuse.Core
	s.mu.Unlock()
	persistenceErr := a.session.RecordVerificationReuse(s.context, reuse.Result)
	a.verificationError(persistenceErr, fileGroupKey(g.Diffs))
	byOrigin := make(map[string][]v.Candidate)
	if persistenceErr == nil {
		for _, candidate := range reuse.Recheck {
			key := fmt.Sprintf("%s/%d", candidate.Origin.GroupID, candidate.Origin.Round)
			byOrigin[key] = append(byOrigin[key], candidate)
		}
	} else {
		s.mu.Lock()
		s.core.Fail("Candidate persistence fails: " + persistenceErr.Error())
		s.mu.Unlock()
	}
	keys := make([]string, 0, len(byOrigin))
	for key := range byOrigin {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		candidates := byOrigin[key]
		_, err := a.runner.RunVerification(ctx, llmloop.VerifierInput{Core: s.core, CoreMu: &s.mu, View: a.evidence, Candidates: candidates, GroupID: candidates[0].Origin.GroupID, Round: candidates[0].Origin.Round, Diffs: g.Diffs, Scope: s.context.Scope, Rules: s.context.Rules, Background: s.context.Background, OnResult: a.session.RecordVerificationSnapshot})
		if err != nil {
			a.recordWarning("verification_incomplete", fileGroupKey(g.Diffs), err.Error())
		}
	}
	a.verificationError(a.session.RecordVerificationSnapshot(s.result()), fileGroupKey(g.Diffs))
	a.verificationError(a.session.RecordVerificationTerminal(s.id), fileGroupKey(g.Diffs))
	for _, d := range g.Diffs {
		a.markReused(d)
		a.checkpointVerification(g, d, a.args.Resume.SessionID)
	}
	a.verificationMu.Lock()
	if a.resumeInfo == nil {
		a.resumeInfo = &ResumeInfo{ResumedFrom: a.args.Resume.SessionID, PreviousModel: a.args.Resume.Model, CurrentModel: a.args.Model}
	}
	a.resumeInfo.ReusedFiles += int64(len(g.Diffs))
	a.verificationMu.Unlock()
	return true
}
