// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/alibaba/open-code-review/internal/model"
	"github.com/alibaba/open-code-review/internal/session"
	v "github.com/alibaba/open-code-review/internal/verification"
)

type liveVerifierEndpoint struct {
	mu            sync.Mutex
	mode          string
	mainCalls     int
	verifierCalls int
	prior         string
	cancel        context.CancelFunc
}

func liveStrings(value any, out *[]string) {
	switch value := value.(type) {
	case string:
		*out = append(*out, value)
	case []any:
		for _, child := range value {
			liveStrings(child, out)
		}
	case map[string]any:
		for _, child := range value {
			liveStrings(child, out)
		}
	}
}

func (s *liveVerifierEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(data, &body)
	texts := []string{}
	liveStrings(body["messages"], &texts)
	toolNames := []string{}
	liveStrings(body["tools"], &toolNames)
	verifier := false
	for _, name := range toolNames {
		if name == "verification_submit" {
			verifier = true
		}
	}
	respond := func(blocks ...map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		inputTokens := 10
		if s.mode == "budget" {
			inputTokens = 2000000
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "live-message", "type": "message", "role": "assistant", "model": "claude-test", "content": blocks, "stop_reason": "tool_use", "usage": map[string]int{"input_tokens": inputTokens, "output_tokens": 5}})
	}
	call := func(name string, input any) map[string]any {
		return map[string]any{"type": "tool_use", "id": fmt.Sprintf("call-%d-%d-%s", s.mainCalls, s.verifierCalls, name), "name": name, "input": input}
	}
	fail := func() {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"scripted failure"}}`))
	}
	if !verifier {
		s.mainCalls++
		if s.mainCalls > 1 {
			s.prior = strings.Join(texts, "\n")
			if s.mode == "round_duplicate" {
				respond(call("code_comment", map[string]any{"comments": []model.LlmComment{liveDuplicateClaim()}}), call("task_done", map[string]any{"state": "DONE"}))
				return
			}
			if s.mode == "discovery_cancel" && s.cancel != nil {
				s.cancel()
				fail()
				return
			}
			if s.mode == "later_failure" || s.mode == "discovery_failure" {
				fail()
				return
			}
			respond(call("task_done", map[string]any{"state": "DONE"}))
			return
		}
		if s.mode == "empty" {
			respond(call("task_done", map[string]any{"state": "DONE"}))
			return
		}
		claims := []model.LlmComment{
			{Path: "handler.go", Content: "A nil request reaches the dereference.", ExistingCode: "return req.Value", Severity: "high", Category: "bug"},
			{Path: "handler.go", Content: "The caller omits its authorization guard.", ExistingCode: "return req.Value", Severity: "medium", Category: "security"},
			{Path: "handler.go", Content: "The remote caller contract is unavailable.", ExistingCode: "return req.Value", Severity: "low", Category: "other"},
			{Path: "handler.go", Content: "An unsupported confirmation must remain visible.", ExistingCode: "return req.Value", Severity: "medium", Category: "bug"},
		}
		if s.mode == "refutation_only" {
			claims = claims[1:2]
		}
		if s.mode == "round_duplicate" {
			claims = []model.LlmComment{liveDuplicateClaim()}
		}
		if s.mode == "duplicates" {
			claims = []model.LlmComment{liveDuplicateClaim(), liveDuplicateClaim(), liveDuplicateClaim()}
		}
		if s.mode == "discovery_failure" || s.mode == "discovery_cancel" {
			respond(call("code_comment", map[string]any{"comments": claims}))
			return
		}
		respond(call("code_comment", map[string]any{"comments": claims}), call("task_done", map[string]any{"state": "DONE"}))
		return
	}
	s.verifierCalls++
	if s.mode == "discovery_failure" {
		fail()
		return
	}
	var candidates []v.Candidate
	var citation *v.Citation
	for _, text := range texts {
		var payload struct {
			Candidates []v.Candidate `json:"candidates"`
			Sources    []struct {
				Citation *v.Citation `json:"citation"`
			} `json:"sources"`
		}
		if json.Unmarshal([]byte(text), &payload) == nil {
			if len(payload.Candidates) > 0 {
				candidates = payload.Candidates
			}
			for _, source := range payload.Sources {
				if source.Citation != nil {
					citation = source.Citation
				}
			}
		}
	}
	if s.verifierCalls == 1 {
		respond(call("file_read", map[string]any{"file_path": "caller.go", "start_line": 1, "end_line": 6}))
		return
	}
	if s.verifierCalls >= 3 {
		if s.mode == "cancel" && s.cancel != nil {
			s.cancel()
		}
		fail()
		return
	}
	if len(candidates) == 0 || citation == nil {
		fail()
		return
	}
	decisions := make([]v.Decision, 0, len(candidates))
	for index, candidate := range candidates {
		decision := v.Decision{CandidateID: candidate.ID}
		switch candidate.Finding.Content {
		case "A nil request reaches the dereference.":
			decision.Verdict = v.Confirmed
			decision.Reason = v.Reason{Code: "nil_trigger", Explanation: "A nil request reaches the dereference."}
			decision.Confirmation = &v.Confirmation{Trigger: "The request is nil.", FailurePath: "The caller passes the request to handler.", Consequence: "The handler panics."}
			decision.Citations = []v.Citation{*citation}
		case "The caller omits its authorization guard.":
			decision.Verdict = v.Refuted
			decision.Reason = v.Reason{Code: "caller_guard", Explanation: "The caller checks authorization before handler."}
			decision.Contradiction = "The caller applies the claimed missing guard."
			decision.Citations = []v.Citation{*citation}
		case "The remote caller contract is unavailable.":
			decision.Verdict = v.Uncertain
			decision.Reason = v.Reason{Code: "caller_unavailable", Explanation: "The remote caller contract is unavailable."}
		default:
			decision.Verdict = v.Confirmed
			decision.Reason = v.Reason{Code: "unsupported", Explanation: "This decision lacks supporting evidence."}
		}
		if s.mode == "duplicates" && index > 0 {
			decision.Verdict = v.Uncertain
			decision.Reason = v.Reason{Code: "caller_unavailable", Explanation: "The remote caller contract is unavailable."}
			decision.Contradiction = ""
			decision.Citations = nil
		}
		decisions = append(decisions, decision)
	}
	complete := true
	if s.mode == "partial" || s.mode == "cancel" {
		decisions = decisions[:2]
		complete = false
	}
	respond(call("verification_submit", map[string]any{"decisions": decisions, "complete": complete}))
}

func setupLiveVerification(t *testing.T, mode string) (string, *liveVerifierEndpoint) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	endpoint := &liveVerifierEndpoint{mode: mode}
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)
	t.Setenv("OCR_LLM_URL", server.URL+"/v1/messages")
	t.Setenv("OCR_LLM_TOKEN", "test-token")
	t.Setenv("OCR_LLM_MODEL", "claude-test")
	t.Setenv("OCR_LLM_PROTOCOL", "anthropic")
	t.Setenv("OCR_LLM_AUTH_HEADER", "x-api-key")
	repo := t.TempDir()
	retryTestGit(t, repo, "init", "-q", "-b", "main")
	for name, source := range map[string]string{"handler.go": "package p\ntype Request struct { Value int }\nfunc handler(req *Request) int {\nreturn 1\n}\n", "caller.go": "package p\nfunc caller(authorized bool, req *Request) int {\nif !authorized { return 0 }\nreturn handler(req)\n}\n"} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	retryTestGit(t, repo, "add", ".")
	retryTestGit(t, repo, "commit", "-qm", "base")
	if err := os.WriteFile(filepath.Join(repo, "handler.go"), []byte("package p\ntype Request struct { Value int }\nfunc handler(req *Request) int {\nreturn req.Value\n}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	retryTestGit(t, repo, "add", ".")
	retryTestGit(t, repo, "commit", "-qm", "change")
	return repo, endpoint
}

func runLiveVerification(t *testing.T, ctx context.Context, repo, format, resume string, bypass bool, background string) ([]byte, error) {
	t.Helper()
	audience := "agent"
	if format == "human" {
		audience = "human"
		format = "text"
	}
	path := filepath.Join(t.TempDir(), "result."+format)
	budget := 0
	if background == "budget" {
		budget = 1000000
	}
	err := executeReviewContext(ctx, reviewOptions{repoDir: repo, from: "HEAD~1", to: "HEAD", outputFormat: format, audience: audience, outputPath: path, concurrency: 1, maxGitProcs: 2, noFilter: bypass, resume: resume, background: background, effort: "medium", maxTokensBudget: budget})
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read result: %v (review: %v)", readErr, err)
	}
	return data, err
}

type liveJSONResult struct {
	Comments   []findingOutput        `json:"comments"`
	Report     v.Report               `json:"verification_report"`
	Completion verificationCompletion `json:"verification_completion"`
	SessionID  string                 `json:"session_id"`
	Manifest   *session.RunManifest   `json:"manifest"`
	Resume     *session.ResumeInfo    `json:"resume"`
}

func decodeLiveResult(t *testing.T, data []byte) liveJSONResult {
	t.Helper()
	var result liveJSONResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("result JSON: %v\n%s", err, data)
	}
	return result
}

func assertLiveMixed(t *testing.T, result liveJSONResult) {
	t.Helper()
	if len(result.Comments) != 3 || len(result.Report.Records) != 4 {
		t.Fatalf("publication=%+v report=%+v", result.Comments, result.Report)
	}
	verdicts := []v.Verdict{v.Confirmed, v.Refuted, v.Uncertain, v.Uncertain}
	for i, record := range result.Report.Records {
		if record.Verdict != verdicts[i] || record.Candidate.ID == "" || record.Candidate.Origin.Round != 1 || record.Provenance.Model != "claude-test" {
			t.Fatalf("record %d: %+v", i, record)
		}
	}
	for i, index := range []int{0, 2, 3} {
		record := result.Report.Records[index]
		finding := result.Comments[i]
		if finding.CandidateID != record.Candidate.ID || finding.Content != record.Candidate.Finding.Content || finding.Severity != record.Candidate.Finding.Severity || finding.Verification == nil || finding.Verification.Verdict != record.Verdict {
			t.Fatalf("finding %d: %+v", i, finding)
		}
	}
	if result.Report.Records[1].Citations[0].Path != "caller.go" || !strings.Contains(result.Report.Records[1].Citations[0].Excerpt, "if !authorized") {
		t.Fatal("refutation lacks the captured caller guard")
	}
	if result.Completion.Complete != 4 || result.Completion.Incomplete != 0 {
		t.Fatalf("completion=%+v", result.Completion)
	}
}

func TestVerifiedReviewCommandAndResume(t *testing.T) {
	oldCommentsRepo, oldCommentsJSON, oldSeverity, oldCategory := sessionCommentsRepoDir, sessionCommentsJSON, sessionCommentsSeverity, sessionCommentsCategory
	oldShowRepo, oldShowJSON := sessionShowRepoDir, sessionShowJSON
	oldCompareRepo, oldCompareJSON := sessionCompareRepoDir, sessionCompareJSON
	oldExportRepo, oldExportJSON, oldExportOutput := sessionExportRepoDir, sessionExportJSON, sessionExportOutput
	t.Cleanup(func() {
		sessionCommentsRepoDir, sessionCommentsJSON, sessionCommentsSeverity, sessionCommentsCategory = oldCommentsRepo, oldCommentsJSON, oldSeverity, oldCategory
		sessionShowRepoDir, sessionShowJSON = oldShowRepo, oldShowJSON
		sessionCompareRepoDir, sessionCompareJSON = oldCompareRepo, oldCompareJSON
		sessionExportRepoDir, sessionExportJSON, sessionExportOutput = oldExportRepo, oldExportJSON, oldExportOutput
	})
	repo, endpoint := setupLiveVerification(t, "mixed")
	data, err := runLiveVerification(t, context.Background(), repo, "json", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	original := decodeLiveResult(t, data)
	assertLiveMixed(t, original)
	saved, err := session.LoadVerificationResult(repo, original.SessionID)
	if err != nil || !reflect.DeepEqual(saved.Report, original.Report) {
		t.Fatalf("saved report: %v\n%+v", err, saved.Report)
	}
	endpoint.mu.Lock()
	calls := endpoint.verifierCalls
	endpoint.mu.Unlock()
	if !strings.Contains(endpoint.prior, `"verdict":"refuted"`) || !strings.Contains(endpoint.prior, `"verdict":"uncertain"`) {
		t.Fatalf("later round loses dispositions: %s", endpoint.prior)
	}
	data, err = runLiveVerification(t, context.Background(), repo, "json", original.SessionID, false, "")
	if err != nil {
		t.Fatal(err)
	}
	resumed := decodeLiveResult(t, data)
	assertLiveMixed(t, resumed)
	if !reflect.DeepEqual(resumed.Report, original.Report) || resumed.Resume == nil || resumed.Resume.ReusedFiles != 1 {
		t.Fatalf("resume=%+v", resumed)
	}
	endpoint.mu.Lock()
	if endpoint.verifierCalls != calls {
		t.Error("completed reuse issues a verifier request")
	}
	endpoint.mu.Unlock()
	sessionCompareRepoDir = repo
	sessionCompareJSON = true
	comparison := captureStdout(t, func() {
		if err := runSessionCompare(original.SessionID, resumed.SessionID); err != nil {
			t.Error(err)
		}
	})
	var compared comparisonOutput
	if err := json.Unmarshal([]byte(comparison), &compared); err != nil || len(compared.Persisting) != 3 || compared.New == nil || compared.Resolved == nil || compared.NotReviewed == nil {
		t.Fatalf("saved comparison loses metadata or bucket arrays: %v %s", err, comparison)
	}
	for _, finding := range compared.Persisting {
		matched := false
		for _, expected := range original.Comments {
			if finding.CandidateID == expected.CandidateID && reflect.DeepEqual(finding.Verification, expected.Verification) {
				matched = true
				break
			}
		}
		if !matched {
			t.Fatalf("comparison changes candidate metadata: %+v", finding)
		}
	}
	sessionCompareJSON = false
	comparison = captureStdout(t, func() {
		if err := runSessionCompare(original.SessionID, resumed.SessionID); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(comparison, "Uncertain (complete, caller_unavailable)") {
		t.Fatalf("comparison text omits uncertainty: %s", comparison)
	}
	parentPath, _ := session.SessionFilePath(repo, original.SessionID)
	if err := os.Remove(parentPath); err != nil {
		t.Fatal(err)
	}
	child, err := session.LoadVerificationResult(repo, resumed.SessionID)
	if err != nil || !reflect.DeepEqual(child.Report, original.Report) {
		t.Fatalf("child replay depends on parent: %v", err)
	}
	sessionCommentsRepoDir = repo
	sessionCommentsJSON = true
	sessionCommentsSeverity = ""
	sessionCommentsCategory = ""
	output := captureStdout(t, func() {
		if err := runSessionComments(resumed.SessionID); err != nil {
			t.Error(err)
		}
	})
	var comments []findingOutput
	if err := json.Unmarshal([]byte(output), &comments); err != nil || len(comments) != 3 || comments[1].CandidateID != original.Comments[1].CandidateID {
		t.Fatalf("session comments: %v %s", err, output)
	}
	sessionCommentsSeverity = "low"
	sessionCommentsCategory = "other"
	output = captureStdout(t, func() {
		if err := runSessionComments(resumed.SessionID); err != nil {
			t.Error(err)
		}
	})
	if err := json.Unmarshal([]byte(output), &comments); err != nil || len(comments) != 1 || comments[0].Verification.Reason.Code != "caller_unavailable" {
		t.Fatalf("finding filter loses metadata: %v %s", err, output)
	}
	sessionCommentsSeverity = "critical"
	output = captureStdout(t, func() {
		if err := runSessionComments(resumed.SessionID); err != nil {
			t.Error(err)
		}
	})
	if strings.TrimSpace(output) != "[]" {
		t.Fatalf("empty filter changes array contract: %s", output)
	}
	sessionCommentsSeverity = ""
	sessionCommentsCategory = ""
	sessionShowRepoDir = repo
	sessionShowJSON = true
	output = captureStdout(t, func() {
		if err := runSessionShow(resumed.SessionID); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(output, `"caller_guard"`) || !strings.Contains(output, `"refuted"`) {
		t.Fatalf("session detail omits audit: %s", output)
	}
	sessionExportRepoDir = repo
	sessionExportJSON = true
	sessionExportOutput = filepath.Join(t.TempDir(), "session.json")
	if err := runSessionExport(resumed.SessionID); err != nil {
		t.Fatal(err)
	}
	exported, _ := os.ReadFile(sessionExportOutput)
	if !bytes.Contains(exported, []byte(original.Report.Records[1].Candidate.ID)) {
		t.Fatal("export omits refuted candidate")
	}
}

func TestVerifiedReviewCommandPresentationAndFailures(t *testing.T) {
	for _, scenario := range []string{"mixed", "partial", "cancel", "later_failure", "discovery_failure", "discovery_cancel", "bypass", "empty", "persistence", "budget"} {
		t.Run(scenario, func(t *testing.T) {
			repo, endpoint := setupLiveVerification(t, scenario)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			endpoint.cancel = cancel
			if scenario == "persistence" {
				root := filepath.Join(os.Getenv("HOME"), ".opencodereview")
				if err := os.MkdirAll(root, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "sessions"), []byte("unavailable"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			background := ""
			if scenario == "budget" {
				background = "budget"
			}
			data, _ := runLiveVerification(t, ctx, repo, "json", "", scenario == "bypass", background)
			result := decodeLiveResult(t, data)
			switch scenario {
			case "mixed", "later_failure":
				assertLiveMixed(t, result)
			case "partial", "cancel":
				if len(result.Report.Records) != 4 || len(result.Comments) != 3 || result.Report.Records[1].Verdict != v.Refuted || result.Report.Records[2].State != v.Incomplete || result.Completion.Incomplete != 2 {
					t.Fatalf("partial record loss: %+v", result)
				}
			case "bypass":
				if len(result.Comments) != 4 || result.Completion.Skipped != 4 || endpoint.verifierCalls != 0 {
					t.Fatalf("bypass: %+v calls=%d", result, endpoint.verifierCalls)
				}
				for _, record := range result.Report.Records {
					if record.Reason.Code != "verification_disabled" || record.Verdict != v.Uncertain {
						t.Fatalf("bypass record: %+v", record)
					}
				}
			case "empty":
				if len(result.Report.Records) != 0 || endpoint.verifierCalls != 0 {
					t.Fatalf("empty: %+v", result)
				}
			case "persistence":
				if len(result.Comments) != 4 || result.Completion.Incomplete != 4 || endpoint.verifierCalls != 0 {
					t.Fatalf("persistence: %+v", result)
				}
			case "discovery_failure", "discovery_cancel", "budget":
				if len(result.Comments) != 4 || result.Completion.Incomplete != 4 || endpoint.verifierCalls != 0 {
					t.Fatalf("interrupted capture: %+v calls=%d", result, endpoint.verifierCalls)
				}
			}
		})
	}
	for _, format := range []string{"text", "human", "sarif"} {
		t.Run(format, func(t *testing.T) {
			repo, _ := setupLiveVerification(t, "mixed")
			data, err := runLiveVerification(t, context.Background(), repo, format, "", false, "")
			if err != nil {
				t.Fatal(err)
			}
			if format == "text" || format == "human" {
				text := string(data)
				if !strings.Contains(text, "Uncertain (complete, caller_unavailable): The remote caller contract is unavailable.") || strings.Contains(text, "The caller omits its authorization guard.") || !strings.Contains(text, "Verification: 4 complete, 0 incomplete, 0 skipped.") {
					t.Fatalf("text: %s", data)
				}
			} else {
				var report sarifReport
				if err := json.Unmarshal(data, &report); err != nil {
					t.Fatal(err)
				}
				if len(report.Runs) != 1 || len(report.Runs[0].Results) != 3 || !strings.Contains(report.Runs[0].Results[1].Message.Text, "Uncertain: The remote caller contract is unavailable.") || report.Runs[0].Results[0].Properties["candidate_id"] == nil || report.Runs[0].Properties["verification_report"] == nil {
					t.Fatalf("SARIF: %s", data)
				}
			}
		})
	}
}

func TestVerifiedReviewResumeRecheckAndInvalidContexts(t *testing.T) {
	for _, scenario := range []string{"incomplete", "background", "mode", "legacy", "corrupt", "refutation_only"} {
		t.Run(scenario, func(t *testing.T) {
			mode := "mixed"
			if scenario == "incomplete" {
				mode = "partial"
			}
			if scenario == "refutation_only" {
				mode = scenario
			}
			repo, endpoint := setupLiveVerification(t, mode)
			data, err := runLiveVerification(t, context.Background(), repo, "json", "", false, "")
			if err != nil {
				t.Fatal(err)
			}
			parent := decodeLiveResult(t, data)
			path, _ := session.SessionFilePath(repo, parent.SessionID)
			if scenario == "legacy" || scenario == "corrupt" {
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
				changed := []byte{}
				for _, line := range lines {
					var event map[string]any
					if err := json.Unmarshal(line, &event); err != nil {
						t.Fatal(err)
					}
					kind, _ := event["type"].(string)
					if scenario == "legacy" && strings.HasPrefix(kind, "verification_") {
						continue
					}
					if kind == "review_item_done" || kind == "review_item_reused" {
						if scenario == "legacy" {
							delete(event, "verification")
						} else if binding, ok := event["verification"].(map[string]any); ok {
							binding["context_id"] = "corrupt-context"
						}
					}
					encoded, _ := json.Marshal(event)
					changed = append(changed, encoded...)
					changed = append(changed, '\n')
				}
				if err := os.WriteFile(path, changed, 0600); err != nil {
					t.Fatal(err)
				}
			}
			endpoint.mu.Lock()
			endpoint.mode = "mixed"
			endpoint.mainCalls = 0
			endpoint.verifierCalls = 0
			endpoint.mu.Unlock()
			background := ""
			if scenario == "background" {
				background = "The caller contract changes."
			}
			data, err = runLiveVerification(t, context.Background(), repo, "json", parent.SessionID, scenario == "mode", background)
			if err != nil {
				t.Fatal(err)
			}
			child := decodeLiveResult(t, data)
			if scenario == "incomplete" || scenario == "refutation_only" {
				if child.Resume == nil || child.Resume.ReusedFiles != 1 || len(child.Report.Records) != len(parent.Report.Records) || endpoint.mainCalls != 0 {
					t.Fatalf("recheck repeats discovery: %+v", child)
				}
				for i, record := range child.Report.Records {
					if record.Candidate != parent.Report.Records[i].Candidate {
						t.Fatal("recheck changes candidate identity")
					}
				}
				if scenario == "incomplete" && (child.Completion.Incomplete != 0 || child.Report.Records[0].Provenance != parent.Report.Records[0].Provenance || child.Report.Records[1].Verdict != v.Refuted) {
					t.Fatalf("recheck loses accepted work: %+v", child)
				}
				if scenario == "refutation_only" && (len(child.Comments) != 0 || len(child.Report.Records) != 1 || endpoint.verifierCalls != 0) {
					t.Fatalf("refutation-only reuse: %+v", child)
				}
			} else {
				if child.Resume == nil || child.Resume.RerunFiles != 1 || endpoint.mainCalls == 0 {
					t.Fatalf("invalid context reuses: %+v", child)
				}
				if child.Report.Records[0].Candidate.ID == parent.Report.Records[0].Candidate.ID {
					t.Fatal("fresh review imports old candidate")
				}
			}
		})
	}
}

func liveDuplicateClaim() model.LlmComment {
	return model.LlmComment{Path: "handler.go", Content: "The caller omits its authorization guard.", ExistingCode: "return req.Value", SuggestionCode: "return guarded(req)", Thinking: "Inspect the caller before the handler.", Severity: "medium", Category: "security"}
}

func assertLiveSavedPublication(t *testing.T, repo, id string, expected []findingOutput) {
	t.Helper()
	commentsRepo, commentsJSON, severity, category := sessionCommentsRepoDir, sessionCommentsJSON, sessionCommentsSeverity, sessionCommentsCategory
	compareRepo, compareJSON := sessionCompareRepoDir, sessionCompareJSON
	exportRepo, exportJSON, exportOutput := sessionExportRepoDir, sessionExportJSON, sessionExportOutput
	t.Cleanup(func() {
		sessionCommentsRepoDir, sessionCommentsJSON, sessionCommentsSeverity, sessionCommentsCategory = commentsRepo, commentsJSON, severity, category
		sessionCompareRepoDir, sessionCompareJSON = compareRepo, compareJSON
		sessionExportRepoDir, sessionExportJSON, sessionExportOutput = exportRepo, exportJSON, exportOutput
	})
	check := func(label string, findings []findingOutput) {
		t.Helper()
		if len(findings) != len(expected) {
			t.Fatalf("%s occurrence count: got %+v, want %+v", label, findings, expected)
		}
		used := make([]bool, len(expected))
		for _, finding := range findings {
			matched := false
			for i, want := range expected {
				if !used[i] && reflect.DeepEqual(finding, want) {
					used[i] = true
					matched = true
					break
				}
			}
			if !matched {
				t.Fatalf("%s changes an original field, candidate identifier, or summary: %+v", label, finding)
			}
		}
	}
	sessionCommentsRepoDir, sessionCommentsJSON, sessionCommentsSeverity, sessionCommentsCategory = repo, true, "", ""
	output := captureStdout(t, func() {
		if err := runSessionComments(id); err != nil {
			t.Fatal(err)
		}
	})
	var comments []findingOutput
	if err := json.Unmarshal([]byte(output), &comments); err != nil {
		t.Fatal(err)
	}
	check("comments", comments)
	sessionCompareRepoDir, sessionCompareJSON = repo, true
	output = captureStdout(t, func() {
		if err := runSessionCompare(id, id); err != nil {
			t.Fatal(err)
		}
	})
	var compared comparisonOutput
	if err := json.Unmarshal([]byte(output), &compared); err != nil {
		t.Fatal(err)
	}
	check("comparison", compared.Persisting)
	if len(compared.New) != 0 || len(compared.Resolved) != 0 || len(compared.NotReviewed) != 0 {
		t.Fatalf("comparison changes matching semantics: %+v", compared)
	}
	sessionExportRepoDir, sessionExportJSON, sessionExportOutput = repo, true, filepath.Join(t.TempDir(), "export.json")
	if err := runSessionExport(id); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(sessionExportOutput)
	if err != nil {
		t.Fatal(err)
	}
	var exported struct {
		Comments []findingOutput `json:"comments"`
	}
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatal(err)
	}
	check("export", exported.Comments)
}

func TestVerifiedSavedPublicationKeepsDuplicateCandidateIdentities(t *testing.T) {
	for _, mode := range []string{"round_duplicate", "duplicates"} {
		t.Run(mode, func(t *testing.T) {
			repo, _ := setupLiveVerification(t, mode)
			data, err := runLiveVerification(t, context.Background(), repo, "json", "", false, "")
			if err != nil {
				t.Fatal(err)
			}
			result := decodeLiveResult(t, data)
			want := 1
			if mode == "duplicates" {
				want = 2
			}
			if len(result.Comments) != want || len(result.Report.Records) != want+1 || result.Report.Records[0].Verdict != v.Refuted {
				t.Fatalf("live publication: %+v", result)
			}
			for _, finding := range result.Comments {
				if finding.CandidateID == result.Report.Records[0].Candidate.ID || finding.Verification.Verdict != v.Uncertain || finding.LlmComment != result.Report.Records[1].Candidate.Finding {
					t.Fatalf("live duplicate claim changes: %+v", finding)
				}
			}
			if mode == "round_duplicate" && (result.Comments[0].Verification.State != v.Incomplete || result.Report.Records[1].Candidate.Origin.Round != 2) {
				t.Fatalf("later failure loses its candidate: %+v", result)
			}
			assertLiveSavedPublication(t, repo, result.SessionID, result.Comments)
		})
	}
}

func TestLegacySavedPublicationWithoutFingerprints(t *testing.T) {
	repo, _ := setupLiveVerification(t, "mixed")
	data, err := runLiveVerification(t, context.Background(), repo, "json", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	result := decodeLiveResult(t, data)
	path, err := session.SessionFilePath(repo, result.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var legacy []byte
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatal(err)
		}
		kind, _ := record["type"].(string)
		if strings.HasPrefix(kind, "verification_") {
			continue
		}
		if kind == "review_item_done" || kind == "review_item_reused" {
			delete(record, "verification")
			delete(record, "fingerprint")
		}
		encoded, _ := json.Marshal(record)
		legacy = append(legacy, encoded...)
		legacy = append(legacy, '\n')
	}
	if err := os.WriteFile(path, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	state, err := session.LoadReviewResumeState(repo, result.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if state.CompletedCount() != 0 {
		t.Fatal("legacy display enables checkpoint reuse")
	}
	expected := make([]findingOutput, len(result.Comments))
	for i, finding := range result.Comments {
		expected[i] = findingOutput{LlmComment: finding.LlmComment, Verification: &v.Summary{Verdict: v.Uncertain, State: v.Incomplete, Reason: v.Reason{Code: "unverified_session", Explanation: "The saved finding has no valid verification checkpoint."}}}
	}
	assertLiveSavedPublication(t, repo, result.SessionID, expected)
}
