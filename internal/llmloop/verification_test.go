// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package llmloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alibaba/open-code-review/internal/config/template"
	"github.com/alibaba/open-code-review/internal/diff"
	"github.com/alibaba/open-code-review/internal/llm"
	"github.com/alibaba/open-code-review/internal/model"
	"github.com/alibaba/open-code-review/internal/session"
	"github.com/alibaba/open-code-review/internal/tool"
	v "github.com/alibaba/open-code-review/internal/verification"
)

type verifierScript struct {
	run      func(context.Context, llm.ChatRequest, int) (*llm.ChatResponse, error)
	requests []llm.ChatRequest
	keys     []string
	metas    []llm.RequestMeta
}

func (s *verifierScript) CompletionsWithCtx(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	n := len(s.requests)
	s.requests = append(s.requests, req)
	s.keys = append(s.keys, llm.SessionKeyFromContext(ctx))
	meta, _ := llm.RequestMetaFromContext(ctx)
	s.metas = append(s.metas, meta)
	return s.run(ctx, req, n)
}

func verifierCall(name string, value any) llm.ToolCall {
	data, _ := json.Marshal(value)
	return llm.ToolCall{ID: "call-" + name, Type: "function", Function: llm.FunctionCall{Name: name, Arguments: string(data)}, ExtraContent: json.RawMessage(`{"signature":"opaque"}`)}
}
func verifierResponse(calls ...llm.ToolCall) *llm.ChatResponse {
	return &llm.ChatResponse{Model: "resolved", Usage: &llm.UsageInfo{PromptTokens: 11, CompletionTokens: 7, CacheReadTokens: 3, CacheWriteTokens: 2}, Choices: []llm.Choice{{Message: llm.ResponseMessage{ToolCalls: calls, ReasoningContent: "private trace", Native: llm.NativeTurn{Family: "openai-chat-completions", Payload: llm.ReasoningPayload("private trace")}}}}}
}
func verifierSubmission(decisions any, complete bool) llm.ToolCall {
	return verifierCall("verification_submit", map[string]any{"decisions": decisions, "complete": complete})
}
func verifierDecision(id string, verdict v.Verdict, citation v.Citation) v.Decision {
	return v.Decision{CandidateID: id, Verdict: verdict, Reason: v.Reason{Code: "source_trace", Explanation: "The captured source determines the claim."}, Citations: []v.Citation{citation}, Confirmation: &v.Confirmation{Trigger: "n is zero", FailurePath: "divide reaches 10 / n", Consequence: "The process panics."}, Contradiction: "The caller rejects zero before it calls divide."}
}

const callerGuard = "package sample\n\nfunc caller(n int) int {\n\tif n == 0 {\n\t\treturn 0\n\t}\n\treturn divide(n)\n}\n"

func verifierFixture(t *testing.T, count int) (string, VerifierInput) {
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
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	git("config", "user.name", "Fixture")
	git("config", "user.email", "fixture@example.com")
	write("caller.go", callerGuard)
	write("source.go", "package sample\nfunc divide(n int) int { return n }\n")
	write("many.go", strings.Repeat("guard\n", 700))
	write("wide.go", strings.Repeat("x", verifierResultBytes+1))
	if err := os.Symlink("caller.go", filepath.Join(repo, "link.go")); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "base")
	write("source.go", "package sample\nfunc divide(n int) int { return 10 / n }\n")
	provider := diff.NewWorkspaceProvider(repo, nil)
	set, err := provider.GetDiffSet(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	core, err := v.New(provider.Evidence(), "context", v.Provenance{})
	if err != nil {
		t.Fatal(err)
	}
	findings := make([]model.LlmComment, count)
	for i := range findings {
		findings[i] = model.LlmComment{Path: "source.go", Content: fmt.Sprintf("claim-%d", i), ExistingCode: "return 10 / n", SuggestionCode: "check n", StartLine: 2, EndLine: 2, Severity: "low", Category: "style", Thinking: "The caller lacks a guard."}
	}
	input := VerifierInput{Core: core, CoreMu: &sync.Mutex{}, View: provider.Evidence(), GroupID: "group", Round: 2, Scope: "source.go", Rules: "Check runtime behavior.", Background: "The caller must reject zero."}
	input.Candidates = core.Capture(findings, v.Origin{GroupID: input.GroupID, Round: input.Round, SessionID: "session", Checkpoint: "source.go"})
	input.Diffs = set.Included
	return repo, input
}
func verifierRunner(t *testing.T, script *verifierScript) *Runner {
	t.Helper()
	tpl, err := template.LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	deps := newTestDeps(script)
	deps.Template = *tpl
	deps.Model = "resolved"
	deps.Template.MaxToolRequestTimes = 10
	deps.NewRequestMeta = metaFactory("script", deps.Model)
	return NewRunner(deps)
}
func deliveredSources(t *testing.T, req llm.ChatRequest) []v.Source {
	t.Helper()
	sources := []v.Source{}
	for _, message := range req.Messages {
		if message.Role != "tool" {
			continue
		}
		var result verifierEvidenceResult
		if json.Unmarshal([]byte(message.ExtractText()), &result) == nil {
			sources = append(sources, result.Sources...)
		}
	}
	return sources
}
func guardReceipt(t *testing.T, req llm.ChatRequest) v.Citation {
	t.Helper()
	for _, source := range deliveredSources(t, req) {
		if source.Citation != nil && source.Citation.Path == "caller.go" && source.Citation.StartLine == 4 && source.Citation.EndLine == 6 {
			expected := "\tif n == 0 {\n\t\treturn 0\n\t}"
			if source.Citation.Excerpt != expected || source.Evidence.Content != expected || source.Evidence.StartLine != 4 || source.Evidence.EndLine != 6 {
				t.Fatalf("source and receipt differ: %+v", source)
			}
			return *source.Citation
		}
	}
	t.Fatal("the provider receives no guard receipt")
	return v.Citation{}
}

type verifierTrap struct {
	name  tool.Tool
	calls *int
}

func (p verifierTrap) Tool() tool.Tool { return p.name }
func (p verifierTrap) Execute(context.Context, map[string]any) (string, error) {
	*p.calls++
	return "trap", nil
}

func TestVerifierMixedDecisionsEvidenceAndProtocol(t *testing.T) {
	repo, input := verifierFixture(t, 7)
	snapshots := []v.Result{}
	input.OnResult = func(result v.Result) error { snapshots = append(snapshots, result); return nil }
	var accepted []v.Decision
	script := &verifierScript{}
	script.run = func(ctx context.Context, req llm.ChatRequest, n int) (*llm.ChatResponse, error) {
		if req.Model != "resolved" || req.MaxTokens != 321 {
			t.Fatalf("request limits: %+v", req)
		}
		switch n {
		case 0:
			if len(req.Messages) != 2 || !strings.Contains(req.Messages[1].ExtractText(), input.Background) || !strings.Contains(req.Messages[1].ExtractText(), input.Rules) || !strings.Contains(req.Messages[1].ExtractText(), input.Scope) {
				t.Fatalf("initial context: %+v", req.Messages)
			}
			if err := os.WriteFile(filepath.Join(repo, "caller.go"), []byte("later bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			return verifierResponse(verifierCall("file_read", map[string]any{"file_path": "caller.go", "start_line": 4, "end_line": 6}), verifierCall("code_search", map[string]any{"search_text": "divide"}), verifierCall("file_find", map[string]any{"query_name": "caller"}), verifierCall("file_read_diff", map[string]any{"path_array": []string{"source.go"}}), verifierCall("code_comment", map[string]any{}), verifierCall("external_mutate", map[string]any{}), verifierCall("shell", map[string]any{}), verifierCall("file_write", map[string]any{}), verifierCall("unknown", map[string]any{})), nil
		case 1:
			receipt := guardReceipt(t, req)
			if len(deliveredSources(t, req)) != 3 {
				t.Fatalf("search sources: %+v", deliveredSources(t, req))
			}
			var capturedDiffs struct {
				Diffs map[string]string `json:"diffs"`
			}
			if err := json.Unmarshal([]byte(req.Messages[6].ExtractText()), &capturedDiffs); err != nil || capturedDiffs.Diffs["source.go"] != input.Diffs[0].Diff || !strings.Contains(req.Messages[5].ExtractText(), "caller.go") {
				t.Fatal("captured diff or file lookup differs")
			}
			previous := req.Messages[2]
			if previous.ExtractText() != "" || previous.ReasoningContent != "private trace" || previous.Native.Payload != llm.ReasoningPayload("private trace") || string(previous.ToolCalls[0].ExtraContent) != `{"signature":"opaque"}` {
				t.Fatalf("private protocol state: %+v", previous)
			}
			for i, call := range previous.ToolCalls {
				if req.Messages[3+i].ToolCallID != call.ID {
					t.Fatal("tool result loses its call identity")
				}
			}
			accepted = []v.Decision{verifierDecision(input.Candidates[0].ID, v.Confirmed, receipt), verifierDecision(input.Candidates[1].ID, v.Refuted, receipt), {CandidateID: input.Candidates[2].ID, Verdict: v.Uncertain, Reason: v.Reason{Code: "unresolved", Explanation: "The evidence does not settle the claim."}}, verifierDecision(input.Candidates[4].ID, v.Refuted, receipt), verifierDecision(input.Candidates[5].ID, v.Refuted, receipt)}
			accepted[3].Citations[0].Excerpt = "altered"
			raw := []any{accepted[0], accepted[1], accepted[2], map[string]any{"candidate_id": input.Candidates[3].ID, "reason": 42}, accepted[3], accepted[4], map[string]any{"candidate_id": "unknown"}, 42}
			return verifierResponse(verifierSubmission(raw, false)), nil
		case 2:
			return verifierResponse(verifierSubmission([]v.Decision{{CandidateID: input.Candidates[5].ID, Verdict: v.Uncertain, Reason: v.Reason{Code: "conflict", Explanation: "Another decision contradicts the first."}}}, true)), nil
		default:
			t.Fatal("unexpected extra request")
			return nil, nil
		}
	}
	runner := verifierRunner(t, script)
	runner.deps.Template.MaxCompletionTokens = 321
	calls := 0
	runner.deps.Tools.Register(verifierTrap{tool.CodeComment, &calls})
	runner.deps.Tools.Register(verifierTrap{tool.Dynamic("external_mutate"), &calls})
	result, err := runner.RunVerification(context.Background(), input)
	if err != nil || result.Disposition != VerifierComplete {
		t.Fatalf("run: %+v %v", result, err)
	}
	if calls != 0 || len(runner.CollectPendingComments()) != 0 {
		t.Fatal("forbidden tools reach the registry or collector")
	}
	expectedVerdicts := []v.Verdict{v.Confirmed, v.Refuted, v.Uncertain, v.Uncertain, v.Uncertain, v.Uncertain, v.Uncertain}
	expectedReasons := []string{"source_trace", "source_trace", "unresolved", "invalid_decision", "invalid_decision", "conflicting_decisions", "missing_decision"}
	expectedPublished := []v.PublishedFinding{}
	for i, record := range result.Result.Report.Records {
		if record.Candidate != input.Candidates[i] || record.Verdict != expectedVerdicts[i] || record.State != v.Complete || record.Reason.Code != expectedReasons[i] || record.Reason.Explanation == "" || record.Provenance != (v.Provenance{Provider: "script", Model: "resolved", ContractVersion: result.ContractID}) {
			t.Fatalf("record %d: %+v", i, record)
		}
		if i != 1 {
			expectedPublished = append(expectedPublished, v.PublishedFinding{CandidateID: input.Candidates[i].ID, Finding: input.Candidates[i].Finding, Verification: v.Summary{Verdict: record.Verdict, State: record.State, Reason: record.Reason}})
		}
	}
	if len(result.Result.Report.Records) != 7 || !reflect.DeepEqual(result.Result.Published, expectedPublished) {
		t.Fatalf("exact publication: %+v", result.Result)
	}
	if !reflect.DeepEqual(result.Result.Report.Records[0].Submissions, []v.Decision{accepted[0]}) || !reflect.DeepEqual(result.Result.Report.Records[1].Citations, accepted[1].Citations) || result.Result.Report.Records[1].Contradiction != accepted[1].Contradiction {
		t.Fatal("accepted support differs")
	}
	if len(snapshots) != 3 || snapshots[0].Report.Records[6].State != v.Incomplete || snapshots[0].Report.Records[1].Verdict != v.Refuted {
		t.Fatalf("incremental results: %+v", snapshots)
	}
	if runner.TotalInputTokens() != 33 || runner.TotalOutputTokens() != 21 || runner.TotalCacheReadTokens() != 9 || runner.TotalCacheWriteTokens() != 6 {
		t.Fatal("usage does not enter shared counters")
	}
	records := runner.deps.Session.GetOrCreateFileSession(result.TaskKey).TaskRecords[session.VerificationTask]
	if len(records) != 3 || len(records[0].ToolResults) != 9 || records[0].ToolResults[4].OK || !records[0].ToolResults[0].OK || records[0].Response == nil || records[2].Response == nil {
		t.Fatalf("request records: %+v", records)
	}
	for i, req := range script.requests {
		if req.SessionID != result.ConversationID || script.keys[i] != script.keys[0] || script.metas[i] != (llm.RequestMeta{Provider: "script", Model: "resolved", FilePath: result.TaskKey, TaskType: string(session.VerificationTask), RequestNo: i + 1}) {
			t.Fatal("request identity differs from the task record")
		}
	}
}

func TestVerifierCitationBoundary(t *testing.T) {
	_, fixture := verifierFixture(t, 1)
	cases := []struct {
		name  string
		alter func(*v.Decision, v.Citation)
	}{
		{"omitted", func(d *v.Decision, _ v.Citation) { d.Citations = nil }},
		{"identifier", func(d *v.Decision, _ v.Citation) { d.Citations[0].ID = "unknown" }},
		{"context", func(d *v.Decision, _ v.Citation) { d.Citations[0].ContextID = "stale" }},
		{"state", func(d *v.Decision, _ v.Citation) { d.Citations[0].State = "stale" }},
		{"path", func(d *v.Decision, _ v.Citation) { d.Citations[0].Path = "other.go" }},
		{"start", func(d *v.Decision, _ v.Citation) { d.Citations[0].StartLine++ }},
		{"end", func(d *v.Decision, _ v.Citation) { d.Citations[0].EndLine++ }},
		{"digest", func(d *v.Decision, _ v.Citation) { d.Citations[0].Digest = "bad" }},
		{"excerpt", func(d *v.Decision, _ v.Citation) { d.Citations[0].Excerpt = "bad" }},
		{"undelivered", func(d *v.Decision, foreign v.Citation) { d.Citations = []v.Citation{foreign} }},
		{"unavailable", func(d *v.Decision, _ v.Citation) { d.Citations[0] = v.Citation{Path: "link.go", ID: "unavailable"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			core, err := v.New(fixture.View, "context", v.Provenance{})
			if err != nil {
				t.Fatal(err)
			}
			input := fixture
			input.Core = core
			input.Candidates = core.Capture([]model.LlmComment{fixture.Candidates[0].Finding}, fixture.Candidates[0].Origin)
			undelivered, err := core.Issue(context.Background(), "caller.go", 4, 6)
			if err != nil {
				t.Fatal(err)
			}
			script := &verifierScript{}
			script.run = func(_ context.Context, req llm.ChatRequest, n int) (*llm.ChatResponse, error) {
				if n == 0 {
					return verifierResponse(verifierCall("file_read", map[string]any{"file_path": "caller.go", "start_line": 4, "end_line": 6})), nil
				}
				decision := verifierDecision(input.Candidates[0].ID, v.Refuted, guardReceipt(t, req))
				tc.alter(&decision, *undelivered.Citation)
				return verifierResponse(verifierSubmission([]v.Decision{decision}, true)), nil
			}
			result, err := verifierRunner(t, script).RunVerification(context.Background(), input)
			if err != nil || result.Disposition != VerifierComplete || result.Result.Report.Records[0].Verdict != v.Uncertain || result.Result.Report.Records[0].Reason.Code != "invalid_decision" || len(result.Result.Published) != 1 {
				t.Fatalf("unsupported refutation: %+v %v", result, err)
			}
		})
	}
}

func TestVerifierEvidenceCoverageAndBoundaries(t *testing.T) {
	_, input := verifierFixture(t, 1)
	script := &verifierScript{}
	script.run = func(_ context.Context, req llm.ChatRequest, n int) (*llm.ChatResponse, error) {
		if n == 0 {
			return verifierResponse(verifierCall("file_read", map[string]any{"file_path": "many.go"}), verifierCall("file_read", map[string]any{"file_path": "wide.go"}), verifierCall("file_read", map[string]any{"file_path": "link.go"}), verifierCall("file_read", map[string]any{"file_path": "../caller.go"}), verifierCall("code_search", map[string]any{"search_text": "guard", "file_patterns": []string{"many.go"}}), verifierCall("code_search", map[string]any{"search_text": "guard", "file_patterns": []string{"../*"}}), verifierCall("file_read", map[string]any{"file_path": "missing.go"})), nil
		}
		results := req.Messages[3:]
		var bounded, wide, link, search, missing verifierEvidenceResult
		for i, target := range map[int]*verifierEvidenceResult{0: &bounded, 1: &wide, 2: &link, 4: &search, 6: &missing} {
			if err := json.Unmarshal([]byte(results[i].ExtractText()), target); err != nil {
				t.Fatal(err)
			}
		}
		if !bounded.Truncated || bounded.Sources[0].Citation.EndLine != 500 || !wide.Truncated || len(wide.Sources) != 0 || link.Sources[0].Evidence.Content != "caller.go" || link.Sources[0].Evidence.Mode != "120000" || missing.Sources[0].Citation != nil || missing.Sources[0].Evidence.Available || !search.Truncated || len(search.Sources) != 100 {
			t.Fatal("Captured reads or searches lose their source bounds, byte limits, or symlink boundary.")
		}
		if !strings.HasPrefix(results[3].ExtractText(), "Error:") || !strings.HasPrefix(results[5].ExtractText(), "Error:") {
			t.Fatal("traversal reaches captured evidence")
		}
		for _, message := range results {
			if len(message.ExtractText()) > verifierResultBytes {
				t.Fatal("result exceeds the byte limit")
			}
		}
		return verifierResponse(verifierSubmission([]v.Decision{}, true)), nil
	}
	result, err := verifierRunner(t, script).RunVerification(context.Background(), input)
	if err != nil || result.Result.Report.Records[0].Reason.Code != "missing_decision" {
		t.Fatalf("run: %+v %v", result, err)
	}
}

func TestVerifierInterruptionsRetainPartialDecisions(t *testing.T) {
	for _, mode := range []string{"provider", "cancel", "deadline", "rounds", "budget", "prompt", "observer"} {
		t.Run(mode, func(t *testing.T) {
			_, input := verifierFixture(t, 2)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "deadline" {
				var deadlineCancel context.CancelFunc
				ctx, deadlineCancel = context.WithTimeout(ctx, 5*time.Second)
				defer deadlineCancel()
			}
			script := &verifierScript{}
			script.run = func(requestCtx context.Context, req llm.ChatRequest, n int) (*llm.ChatResponse, error) {
				if mode == "deadline" {
					got, _ := requestCtx.Deadline()
					want, _ := ctx.Deadline()
					if got != want {
						t.Fatal("runner replaces the caller deadline")
					}
				}
				if n == 0 {
					return verifierResponse(verifierCall("file_read", map[string]any{"file_path": "caller.go", "start_line": 4, "end_line": 6})), nil
				}
				if n == 1 {
					resp := verifierResponse(verifierSubmission([]v.Decision{verifierDecision(input.Candidates[0].ID, v.Refuted, guardReceipt(t, req))}, false))
					if mode == "prompt" {
						resp.Choices[0].Message.Native.Payload = llm.ReasoningPayload(strings.Repeat("opaque", 100000))
					}
					return resp, nil
				}
				if mode == "deadline" {
					<-requestCtx.Done()
					return nil, requestCtx.Err()
				}
				return nil, errors.New("scripted provider failure")
			}
			runner := verifierRunner(t, script)
			if mode == "rounds" {
				runner.deps.Template.MaxToolRequestTimes = 2
			}
			if mode == "budget" {
				runner.deps.MaxTokensBudget = 36
			}
			if mode == "prompt" {
				runner.deps.Template.MaxTokens = 10000
			}
			input.OnResult = func(result v.Result) error {
				if result.Report.Records[0].Verdict == v.Refuted {
					if mode == "cancel" {
						cancel()
					}
					if mode == "observer" {
						return errors.New("observer cannot retain the snapshot")
					}
				}
				return nil
			}
			result, err := runner.RunVerification(ctx, input)
			expected := VerifierFailed
			reason := "verification_failed"
			count := 3
			switch mode {
			case "cancel":
				expected = VerifierCanceled
				count = 2
			case "deadline":
				expected = VerifierCanceled
			case "rounds", "budget", "prompt":
				expected = VerifierLimited
				reason = "limit_exhausted"
				count = 2
			case "observer":
				count = 2
			}
			if result.Disposition != expected || len(script.requests) != count || (mode == "provider" && err == nil) {
				t.Fatalf("stop: %+v %v requests=%d", result, err, len(script.requests))
			}
			records := result.Result.Report.Records
			if records[0].Verdict != v.Refuted || records[0].State != v.Complete || records[1].Verdict != v.Uncertain || records[1].State != v.Incomplete || records[1].Reason.Code != reason || len(result.Result.Published) != 1 || result.Result.Published[0].CandidateID != input.Candidates[1].ID {
				t.Fatalf("partial results: %+v", result.Result)
			}
			if runner.TotalTokensUsed() != 36 {
				t.Fatalf("aggregate usage=%d", runner.TotalTokensUsed())
			}
			taskRecords := runner.deps.Session.GetOrCreateFileSession(result.TaskKey).TaskRecords[session.VerificationTask]
			if mode == "provider" || mode == "deadline" {
				if taskRecords[2].Error == "" || taskRecords[2].Response != nil {
					t.Fatal("failed request has no error record")
				}
			}
			if mode == "budget" {
				before := len(script.requests)
				again, err := runner.RunVerification(context.Background(), input)
				if err != nil || again.Disposition != VerifierLimited || len(script.requests) != before || again.Result.Report.Records[0].Verdict != v.Refuted {
					t.Fatal("an exhausted shared budget admits another request")
				}
			}
		})
	}
}

func TestVerifierAdmissionAndIndependentInvocations(t *testing.T) {
	_, input := verifierFixture(t, 1)
	script := &verifierScript{run: func(context.Context, llm.ChatRequest, int) (*llm.ChatResponse, error) {
		return verifierResponse(verifierSubmission([]v.Decision{}, true)), nil
	}}
	runner := verifierRunner(t, script)
	empty, err := runner.RunVerification(context.Background(), VerifierInput{})
	if err != nil || empty.Disposition != VerifierComplete || len(script.requests) != 0 || len(empty.Result.Report.Records) != 0 {
		t.Fatal("empty input sends a provider request")
	}
	contract, err := runner.VerificationContract()
	if err != nil {
		t.Fatal(err)
	}
	contract.Tools[0].Function.Parameters["changed"] = true
	same, err := runner.VerificationContract()
	if err != nil || same.Fingerprint != contract.Fingerprint || same.Tools[0].Function.Parameters["changed"] != nil {
		t.Fatal("contract identity depends on caller mutation")
	}
	runner.deps.Template.ApplyLanguage("de")
	translated, err := runner.VerificationContract()
	if err != nil || translated.Fingerprint == same.Fingerprint {
		t.Fatal("effective language does not enter contract identity")
	}
	for _, mode := range []string{"prompt", "schemas", "rounds", "budget", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			limited := verifierRunner(t, script)
			ctx := context.Background()
			switch mode {
			case "prompt":
				limited.deps.Template.MaxTokens = 1
			case "schemas":
				payload, err := json.Marshal(struct {
					VerifierInput
					Evidence diff.InputResolution `json:"evidence"`
				}{input, input.View.Input()})
				if err != nil {
					t.Fatal(err)
				}
				initial := []llm.Message{llm.NewTextMessage("system", same.Prompts[0].Content), llm.NewTextMessage("user", string(payload))}
				limited.deps.Template.MaxTokens = (CountMessagesTokens(initial)+50)*5/4 + 1
				if CountMessagesTokens(initial) >= PromptTokenLimit(limited.deps.Template.MaxTokens) {
					t.Fatal("schema fixture does not fit without tools")
				}
			case "rounds":
				limited.deps.Template.MaxToolRequestTimes = 0
			case "budget":
				limited.deps.MaxTokensBudget = 18
				limited.RecordUsage(&llm.UsageInfo{PromptTokens: 18})
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			before := len(script.requests)
			result, _ := limited.RunVerification(ctx, input)
			want := VerifierLimited
			if mode == "canceled" {
				want = VerifierCanceled
			}
			if result.Disposition != want || len(script.requests) != before || result.Result.Report.Records[0].State != v.Incomplete {
				t.Fatalf("admission: %+v", result)
			}
		})
	}
	var oldReceipt v.Citation
	script.run = func(_ context.Context, req llm.ChatRequest, n int) (*llm.ChatResponse, error) {
		if n == 0 {
			return verifierResponse(verifierCall("file_read", map[string]any{"file_path": "caller.go", "start_line": 4, "end_line": 6})), nil
		}
		if n == 1 {
			oldReceipt = guardReceipt(t, req)
			return verifierResponse(verifierSubmission([]v.Decision{verifierDecision(input.Candidates[0].ID, v.Refuted, oldReceipt)}, true)), nil
		}
		if len(req.Messages) != 2 || strings.Contains(req.Messages[1].ExtractText(), oldReceipt.ID) {
			t.Fatal("another invocation leaks conversation history")
		}
		return verifierResponse(verifierSubmission([]v.Decision{verifierDecision(input.Candidates[0].ID, v.Refuted, oldReceipt)}, true)), nil
	}
	first, err := runner.RunVerification(context.Background(), input)
	if err != nil || first.Result.Report.Records[0].Verdict != v.Refuted {
		t.Fatalf("first: %+v %v", first, err)
	}
	input.Round = 3
	input.GroupID = "other-group"
	input.Candidates = input.Core.Capture([]model.LlmComment{input.Candidates[0].Finding}, v.Origin{Round: input.Round, GroupID: input.GroupID})
	second, err := runner.RunVerification(context.Background(), input)
	if err != nil || second.Result.Report.Records[0].Verdict != v.Uncertain || second.Result.Report.Records[0].Reason.Code != "invalid_decision" || first.TaskKey == second.TaskKey || first.ConversationID == second.ConversationID || first.ContractID != second.ContractID || script.keys[0] == script.keys[2] {
		t.Fatalf("independence: %+v %v", second, err)
	}
	if input.Core.Result().Report.Records[0].Verdict != v.Refuted {
		t.Fatal("terminal operation changes another group's accepted record")
	}
}

func TestVerifierRejectsMismatchedCapturedInputs(t *testing.T) {
	_, input := verifierFixture(t, 1)
	_, other := verifierFixture(t, 1)
	script := &verifierScript{run: func(context.Context, llm.ChatRequest, int) (*llm.ChatResponse, error) {
		t.Fatal("invalid binding reaches provider")
		return nil, nil
	}}
	runner := verifierRunner(t, script)
	for _, mode := range []string{"view", "candidate", "group", "round", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			bad := input
			bad.Candidates = append([]v.Candidate(nil), input.Candidates...)
			switch mode {
			case "view":
				bad.View = other.View
			case "candidate":
				bad.Candidates[0].Finding.Content = "altered"
			case "group":
				bad.GroupID = "wrong"
			case "round":
				bad.Round++
			case "duplicate":
				bad.Candidates = append(bad.Candidates, bad.Candidates[0])
			}
			result, err := runner.RunVerification(context.Background(), bad)
			if err == nil || result.Disposition != VerifierFailed || len(script.requests) != 0 {
				t.Fatalf("invalid binding: %+v %v", result, err)
			}
		})
	}
}
