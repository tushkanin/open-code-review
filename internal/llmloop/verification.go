// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package llmloop

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/alibaba/open-code-review/internal/config/template"
	"github.com/alibaba/open-code-review/internal/config/toolsconfig"
	"github.com/alibaba/open-code-review/internal/diff"
	"github.com/alibaba/open-code-review/internal/llm"
	"github.com/alibaba/open-code-review/internal/model"
	"github.com/alibaba/open-code-review/internal/session"
	"github.com/alibaba/open-code-review/internal/verification"
	"github.com/google/uuid"
)

const VerifierDispatchVersion = "captured-verifier-v1"

type VerifierContract struct {
	Prompts         []template.ChatMessage `json:"prompts"`
	Tools           []llm.ToolDef          `json:"tools"`
	DispatchVersion string                 `json:"dispatch_version"`
	Fingerprint     string                 `json:"fingerprint"`
}

func (r *Runner) VerificationContract() (VerifierContract, error) {
	contract := VerifierContract{DispatchVersion: VerifierDispatchVersion}
	if r.deps.Template.VerificationTask == nil || len(r.deps.Template.VerificationTask.Messages) == 0 {
		return contract, errors.New("verification requires an effective verifier prompt")
	}
	contract.Prompts = append([]template.ChatMessage(nil), r.deps.Template.VerificationTask.Messages...)
	if err := json.Unmarshal(toolsconfig.VerifierDefinitions(), &contract.Tools); err != nil {
		return contract, err
	}
	material, err := json.Marshal(contract)
	if err != nil {
		return contract, err
	}
	contract.Fingerprint = fmt.Sprintf("%x", sha256.Sum256(material))
	return contract, nil
}

// CoreMu must also guard every caller access to Core. OnResult runs outside that lock.
// The caller keeps its group concurrency slot and deadline for this operation.
type VerifierInput struct {
	Core       *verification.Core              `json:"-"`
	CoreMu     *sync.Mutex                     `json:"-"`
	View       *diff.EvidenceView              `json:"-"`
	Candidates []verification.Candidate        `json:"candidates"`
	GroupID    string                          `json:"group_id"`
	Round      int                             `json:"round"`
	Diffs      []model.Diff                    `json:"diffs"`
	Scope      string                          `json:"scope"`
	Rules      string                          `json:"rules"`
	Background string                          `json:"background"`
	OnResult   func(verification.Result) error `json:"-"`
}

type VerifierDisposition string

const (
	VerifierComplete VerifierDisposition = "complete"
	VerifierFailed   VerifierDisposition = "failed"
	VerifierCanceled VerifierDisposition = "canceled"
	VerifierLimited  VerifierDisposition = "limit"
)

type VerifierResult struct {
	Result         verification.Result
	Disposition    VerifierDisposition
	Reason         string
	TaskKey        string
	ConversationID string
	ContractID     string
}

type verifierRun struct {
	input     VerifierInput
	ids       []string
	allowed   map[string]bool
	delivered map[string]verification.Citation
	pending   map[string]verification.Citation
}

func (v *verifierRun) snapshot() verification.Result {
	v.input.CoreMu.Lock()
	defer v.input.CoreMu.Unlock()
	all := v.input.Core.Result()
	result := verification.Result{Published: []verification.PublishedFinding{}, Report: verification.Report{Version: verification.RecordVersion, Records: []verification.Record{}}}
	for _, record := range all.Report.Records {
		if v.allowed[record.Candidate.ID] {
			result.Report.Records = append(result.Report.Records, record)
		}
	}
	for _, finding := range all.Published {
		if v.allowed[finding.CandidateID] {
			result.Published = append(result.Published, finding)
		}
	}
	return result
}

func (v *verifierRun) publish() error {
	if v.input.OnResult != nil {
		return v.input.OnResult(v.snapshot())
	}
	return nil
}

func (r *Runner) RunVerification(ctx context.Context, input VerifierInput) (VerifierResult, error) {
	out := VerifierResult{Disposition: VerifierComplete, Result: verification.Result{Published: []verification.PublishedFinding{}, Report: verification.Report{Version: verification.RecordVersion, Records: []verification.Record{}}}}
	if len(input.Candidates) == 0 {
		return out, nil
	}
	if input.Core == nil || input.CoreMu == nil || input.View == nil || r.deps.Session == nil || r.deps.LLMClient == nil {
		out.Disposition = VerifierFailed
		return out, errors.New("verification requires a core, shared lock, captured view, session, and provider")
	}
	contract, err := r.VerificationContract()
	if err != nil {
		out.Disposition = VerifierFailed
		return out, err
	}
	out.ContractID = contract.Fingerprint
	out.ConversationID = uuid.NewString()
	out.TaskKey = fmt.Sprintf("verification/%s/round/%d/%s", input.GroupID, input.Round, out.ConversationID)
	v := &verifierRun{input: input, allowed: make(map[string]bool), delivered: make(map[string]verification.Citation), pending: make(map[string]verification.Citation)}
	for _, candidate := range input.Candidates {
		if candidate.Origin.GroupID != input.GroupID || candidate.Origin.Round != input.Round {
			out.Disposition = VerifierFailed
			return out, errors.New("verification candidates belong to another group or round")
		}
		v.ids = append(v.ids, candidate.ID)
		v.allowed[candidate.ID] = true
	}
	provenance := verification.Provenance{Provider: r.deps.Session.LLMSource, Model: r.deps.Model, ContractVersion: contract.Fingerprint}
	input.CoreMu.Lock()
	err = input.Core.BindInvocation(input.View, input.Candidates, provenance)
	input.CoreMu.Unlock()
	if err != nil {
		out.Disposition = VerifierFailed
		return out, err
	}
	finish := func(disposition VerifierDisposition, reason string, cause error) (VerifierResult, error) {
		input.CoreMu.Lock()
		switch disposition {
		case VerifierComplete:
			input.Core.Finish(v.ids...)
		case VerifierLimited:
			input.Core.Exhaust(reason, v.ids...)
		default:
			input.Core.Fail(reason, v.ids...)
		}
		input.CoreMu.Unlock()
		out.Disposition, out.Reason = disposition, reason
		if notifyErr := v.publish(); notifyErr != nil {
			out.Disposition = VerifierFailed
			out.Reason = notifyErr.Error()
			cause = errors.Join(cause, notifyErr)
		}
		out.Result = v.snapshot()
		return out, cause
	}
	payload, err := json.Marshal(struct {
		VerifierInput
		Evidence diff.InputResolution `json:"evidence"`
	}{input, input.View.Input()})
	if err != nil {
		return finish(VerifierFailed, err.Error(), err)
	}
	messages := make([]llm.Message, 0, len(contract.Prompts)+1)
	for _, prompt := range contract.Prompts {
		messages = append(messages, llm.NewTextMessage(prompt.Role, prompt.Content))
	}
	messages = append(messages, llm.NewTextMessage("user", string(payload)))
	schemas, _ := json.Marshal(contract.Tools)
	schemaTokens := llm.CountTokens(string(schemas))
	ctx = llm.ContextWithSessionKey(ctx, llm.SessionTaskKey(r.deps.Session.SessionID, string(session.VerificationTask), out.TaskKey))
	for round := 0; ; round++ {
		if err := ctx.Err(); err != nil {
			return finish(VerifierCanceled, err.Error(), err)
		}
		if round >= r.deps.Template.MaxToolRequestTimes {
			return finish(VerifierLimited, "Verification reaches the tool-round limit.", nil)
		}
		if budget := r.deps.MaxTokensBudget; budget > 0 && r.TotalTokensUsed() >= budget {
			return finish(VerifierLimited, "Verification reaches the aggregate token budget.", nil)
		}
		if CountMessagesTokens(messages)+schemaTokens >= PromptTokenLimit(r.deps.Template.MaxTokens) {
			return finish(VerifierLimited, "Verification reaches the prompt token limit.", nil)
		}
		rec := r.deps.Session.GetOrCreateFileSession(out.TaskKey).AppendTaskRecord(session.VerificationTask, messages)
		reqCtx := r.requestCtx(ctx, out.TaskKey, session.VerificationTask, rec.RequestNo)
		if meta, ok := llm.RequestMetaFromContext(reqCtx); ok {
			provenance.Provider = meta.Provider
			input.CoreMu.Lock()
			err = input.Core.BindInvocation(input.View, input.Candidates, provenance)
			input.CoreMu.Unlock()
			if err != nil {
				rec.SetError(err, 0)
				return finish(VerifierFailed, err.Error(), err)
			}
		}
		for id, receipt := range v.pending {
			v.delivered[id] = receipt
		}
		clear(v.pending)
		start := time.Now()
		resp, requestErr := r.deps.LLMClient.CompletionsWithCtx(reqCtx, llm.ChatRequest{Model: r.deps.Model, Messages: messages, Tools: contract.Tools, MaxTokens: r.deps.Template.CompletionTokenLimit(), SessionID: out.ConversationID})
		duration := time.Since(start)
		if resp != nil {
			r.RecordUsage(resp.Usage)
		}
		if requestErr != nil {
			rec.SetError(requestErr, duration)
			disposition := VerifierFailed
			if ctx.Err() != nil {
				disposition = VerifierCanceled
			}
			return finish(disposition, requestErr.Error(), requestErr)
		}
		if resp == nil || len(resp.Choices) == 0 {
			err := errors.New("verifier provider returns an empty response")
			rec.SetError(err, duration)
			return finish(VerifierFailed, err.Error(), err)
		}
		rec.SetResponse(resp, duration)
		calls := resp.ToolCalls()
		messages = append(messages, llm.NewToolCallMessage(resp.VisibleContent(), calls, resp.Native(), resp.ReasoningContent()))
		complete := false
		for _, call := range calls {
			start := time.Now()
			result, done, toolErr := v.dispatch(ctx, call)
			if toolErr != nil {
				result = "Error: " + toolErr.Error()
				rec.AddToolFailure(call.Function.Name, call.Function.Arguments, result, time.Since(start))
			} else {
				rec.AddToolResult(call.Function.Name, call.Function.Arguments, result)
			}
			messages = append(messages, llm.NewToolResultMessage(call.ID, result))
			complete = complete || done
			if call.Function.Name == "verification_submit" && toolErr == nil {
				if err := v.publish(); err != nil {
					return finish(VerifierFailed, err.Error(), err)
				}
			}
		}
		if err := ctx.Err(); err != nil {
			return finish(VerifierCanceled, err.Error(), err)
		}
		if complete {
			return finish(VerifierComplete, "The verifier completes its investigation.", nil)
		}
		if len(calls) == 0 {
			messages = append(messages, llm.NewTextMessage("user", "Use the evidence tools or verification_submit to complete the investigation."))
		}
	}
}
