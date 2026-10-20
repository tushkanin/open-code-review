// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package presentation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alibaba/open-code-review/acp/internal/contract"
	"github.com/alibaba/open-code-review/acp/internal/orchestrator"
)

func TestVerificationAuditPrivacyAndPublication(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "source.go"), []byte("package p\nunsafe()\n"), 0600); err != nil {
		t.Fatal(err)
	}
	claim := map[string]any{"path": "source.go", "content": "Equal original claim.", "suggestion_code": "safe()", "existing_code": "unsafe()", "start_line": 2, "end_line": 2, "category": "security", "severity": "high", "thinking": "PRIVATE_THINKING", "reasoning_content": "PRIVATE_PROVIDER", "private_reasoning": map[string]any{"text": "PRIVATE_NESTED"}}
	reason := map[string]any{"code": "caller_unavailable", "explanation": "The caller is unavailable."}
	published := map[string]any{}
	for key, value := range claim {
		published[key] = value
	}
	published["candidate_id"] = "kept"
	published["verification"] = map[string]any{"verdict": "uncertain", "state": "complete", "reason": reason}
	records := []any{}
	for _, item := range []struct{ id, verdict, state string }{{"refuted", "refuted", "complete"}, {"kept", "uncertain", "complete"}, {"interrupted", "uncertain", "incomplete"}} {
		records = append(records, map[string]any{"version": 1, "candidate": map[string]any{"id": item.id, "finding": claim, "origin": map[string]any{"session_id": "same-session", "group_id": "group", "round": 2, "checkpoint": "source.go"}, "context_id": "context"}, "verdict": item.verdict, "state": item.state, "reason": reason, "contradiction": "The caller has a guard.", "confirmation": map[string]any{"trigger": "input", "failure_path": "call", "consequence": "failure"}, "citations": []any{map[string]any{"id": "citation", "context_id": "context", "state": "head", "path": "caller.go", "start_line": 1, "end_line": 3, "digest": "digest", "excerpt": "guard()\n```\n# not markup"}}, "provenance": map[string]any{"provider": "fixture", "model": "scripted", "contract_version": "v1"}, "submissions": []any{map[string]any{"candidate_id": item.id, "verdict": item.verdict, "reason": reason, "provider_private_reasoning": "PRIVATE_SUBMISSION"}}})
	}
	envelope := map[string]any{"status": "complete", "comments": []any{published}, "verification_report": map[string]any{"version": 1, "records": records}, "verification_completion": map[string]any{"complete": 2, "incomplete": 1, "skipped": 0, "reasons": []any{reason}}, "manifest": map[string]any{"terminal_state": "complete", "coverage": map[string]any{"selected": []any{map[string]any{"path": "source.go"}}, "completed": []any{map[string]any{"path": "source.go"}}}}}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	var review contract.ReviewResult
	if err := json.Unmarshal(data, &review); err != nil {
		t.Fatal(err)
	}
	rendered := FormatResult(&orchestrator.Result{Review: &review}, root)
	for _, forbidden := range []string{"PRIVATE_", "thinking", "reasoning_content", "provider_private_reasoning"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("private field leaks: %s", forbidden)
		}
	}
	split := strings.SplitN(rendered, "## Complete verification audit", 2)
	if len(split) != 2 {
		t.Fatal("audit is unavailable")
	}
	if !strings.Contains(split[0], "1 finding") || strings.Contains(split[0], "Candidate: refuted") || !strings.Contains(split[0], "Candidate: kept") || !strings.Contains(split[0], "uncertain (complete)") || !strings.Contains(split[0], "The caller is unavailable.") || !strings.Contains(split[0], "1 incomplete") || !strings.Contains(split[0], "1/1 files completed") {
		t.Fatalf("publication or completion changes: %s", split[0])
	}
	if !strings.Contains(split[0], "file://") || !strings.Contains(split[0], "```go\nsafe()\n```") {
		t.Fatal("safe navigation or suggestion is lost")
	}
	for _, value := range []string{`"id": "refuted"`, `"id": "interrupted"`, `"session_id": "same-session"`, `"round": 2`, `"context_id": "context"`, `"digest": "digest"`, `"failure_path": "call"`, `"submissions":`, `"contract_version": "v1"`, `guard()\n`} {
		if !strings.Contains(split[1], value) {
			t.Fatalf("audit omits %s", value)
		}
	}
	if !strings.Contains(split[1], "````json\n") {
		t.Fatal("citation closes the audit code block")
	}
	review.Comments = nil
	rendered = FormatResult(&orchestrator.Result{Review: &review}, root)
	if !strings.Contains(rendered, "0 findings") || !strings.Contains(rendered, `"id": "refuted"`) || strings.Contains(rendered, "### 1.") {
		t.Fatal("refutation-only report loses audit or invents findings")
	}
}

func TestVerificationUnknownAndLegacyRemainUnverified(t *testing.T) {
	for _, version := range []int{1, 999} {
		review := &contract.ReviewResult{Status: "complete", Comments: []contract.Comment{{Content: "Kept original", CandidateID: "kept", Verification: &contract.VerificationSummary{Verdict: "future_verdict", State: "future_state", Reason: contract.VerificationReason{Code: "future_reason", Explanation: "A future reason."}}}}, VerificationReport: &contract.VerificationReport{Version: version}}
		got := FormatResult(&orchestrator.Result{Review: review}, "")
		if !strings.Contains(got, "unverified") || !strings.Contains(got, "Kept original") || strings.Contains(got, "confirmed (complete)") {
			t.Fatal("unknown metadata establishes trust")
		}
	}
	unsupported := &contract.ReviewResult{Status: "complete", Comments: []contract.Comment{{Content: "Keep the claim", Verification: &contract.VerificationSummary{Verdict: "confirmed", State: "complete", Reason: contract.VerificationReason{Code: "provided_reason", Explanation: "Keep the delivered explanation."}}}}, VerificationReport: &contract.VerificationReport{Version: 999}}
	unsupportedText := FormatResult(&orchestrator.Result{Review: unsupported}, "")
	if !strings.Contains(unsupportedText, "unverified (unsupported report)") || !strings.Contains(unsupportedText, "Keep the delivered explanation.") || strings.Contains(unsupportedText, "confirmed (complete)") {
		t.Fatal("unsupported reports establish confirmation or discard the delivered reason")
	}
	for _, state := range []string{"incomplete", "skipped"} {
		review := &contract.ReviewResult{Status: "complete", Comments: []contract.Comment{{Content: "Kept", Verification: &contract.VerificationSummary{Verdict: "uncertain", State: state, Reason: contract.VerificationReason{Code: "disabled", Explanation: "Reason remains visible."}}}}}
		got := FormatResult(&orchestrator.Result{Review: review}, "")
		if !strings.Contains(got, "("+state+")") || !strings.Contains(got, "Reason remains visible.") {
			t.Fatal("uncertainty reason or execution state is hidden")
		}
	}
	legacy := FormatResult(&orchestrator.Result{Review: &contract.ReviewResult{Status: "complete", Comments: []contract.Comment{{Content: "Historical claim"}}}}, "")
	scan := FormatResult(&orchestrator.Result{Scan: &contract.ScanResult{Status: "success", Comments: []contract.Comment{{Content: "Scan claim"}}}}, "")
	if !strings.Contains(legacy, "unverified") || strings.Contains(scan, "Verification:") {
		t.Fatal("legacy or scan boundary changes")
	}
}
