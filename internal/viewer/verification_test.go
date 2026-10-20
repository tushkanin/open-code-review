// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package viewer

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alibaba/open-code-review/internal/diff"
	"github.com/alibaba/open-code-review/internal/model"
	"github.com/alibaba/open-code-review/internal/session"
	v "github.com/alibaba/open-code-review/internal/verification"
)

type savedViewerFixture struct {
	root   string
	repo   string
	id     string
	path   string
	result v.Result
}

func savedViewerReview(t *testing.T, mode string) savedViewerFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v: %s", err, out)
		}
	}
	git("init", "-q")
	git("config", "user.email", "fixture@example.com")
	git("config", "user.name", "Fixture")
	source := filepath.Join(repo, "source.go")
	if err := os.WriteFile(source, []byte("package p\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "base")
	if err := os.WriteFile(source, []byte("package p\nfunc caller(ok bool) int {\nif !ok { return 0 }\nreturn 1\n}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "guard")
	provider := diff.NewCommitProvider(repo, "HEAD", nil)
	set, err := provider.GetDiffSet(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	verificationContext, err := v.NewContext(provider.Evidence(), v.ContextInputs{Diffs: set.Included, Scope: "selected source", ContractFingerprint: "fixture", Mode: v.Enabled})
	if err != nil {
		t.Fatal(err)
	}
	contextID, err := verificationContext.Identity()
	if err != nil {
		t.Fatal(err)
	}
	core, err := v.New(provider.Evidence(), contextID, v.Provenance{Provider: "fixture", Model: "scripted", ContractVersion: "fixture-v1"})
	if err != nil {
		t.Fatal(err)
	}
	history := session.New(repo, "main", "scripted", session.SessionOptions{ReviewMode: session.ReviewModeCommit})
	claim := model.LlmComment{Path: "source.go", Content: "A caller lacks its guard. <script>bad()</script>", ExistingCode: "return 1", SuggestionCode: "guard()", StartLine: 4, EndLine: 4, Category: "security", Severity: "high", Thinking: "Saved private analysis"}
	if mode == "pathless" {
		claim.Path = ""
	}
	count := 4
	if mode == "refuted" {
		count = 1
	}
	claims := make([]model.LlmComment, count)
	for i := range claims {
		claims[i] = claim
	}
	candidates := core.Capture(claims, v.Origin{SessionID: history.SessionID, GroupID: "guard", Round: 1, Checkpoint: "source.go"})
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(history.CaptureVerification(verificationContext, core.Result().Report))
	if mode == "bypass" {
		core.Bypass()
	} else if mode != "interrupted" {
		source, err := core.Issue(context.Background(), "source.go", 2, 4)
		must(err)
		decisions := []v.Decision{{CandidateID: candidates[0].ID, Verdict: v.Refuted, Reason: v.Reason{Code: "caller_guard", Explanation: "The caller checks the guard."}, Contradiction: "The caller rejects the unsafe input.", Citations: []v.Citation{*source.Citation}}}
		if count > 1 {
			decisions = append(decisions, v.Decision{CandidateID: candidates[1].ID, Verdict: v.Confirmed, Reason: v.Reason{Code: "source_trace", Explanation: "The trace supports this separate claim."}, Confirmation: &v.Confirmation{Trigger: "Allowed input", FailurePath: "Return path", Consequence: "Incorrect result"}, Citations: []v.Citation{*source.Citation}})
			for _, candidate := range candidates[2:] {
				decisions = append(decisions, v.Decision{CandidateID: candidate.ID, Verdict: v.Uncertain, Reason: v.Reason{Code: "caller_unavailable", Explanation: "The remote caller is unavailable. <img src=x onerror=bad()>"}})
			}
		}
		core.Submit(decisions)
	}
	if mode != "interrupted" {
		must(history.RecordVerificationSnapshot(core.Result()))
		must(history.RecordVerificationTerminal(contextID))
		must(history.RecordVerificationCheckpoint(session.ResumeItem{FilePath: "source.go", Fingerprint: "selected"}, contextID, ""))
		must(history.Finalize())
	} else {
		t.Cleanup(func() { _ = history.Finalize() })
	}
	path, err := session.SessionFilePath(repo, history.SessionID)
	must(err)
	archive := t.TempDir()
	archivedRepo := "archive"
	must(os.MkdirAll(filepath.Join(archive, archivedRepo), 0700))
	data, err := os.ReadFile(path)
	must(err)
	archivedPath := filepath.Join(archive, archivedRepo, history.SessionID+".jsonl")
	must(os.WriteFile(archivedPath, data, 0600))
	// A conflicting default record cannot affect an archive selected by the caller.
	must(os.WriteFile(path, []byte("{\"type\":\"session_start\"}\n{\"type\":\"review_item_done\",\"comments\":[{\"content\":\"WRONG ROOT\"}]}\n"), 0600))
	must(os.RemoveAll(repo))
	return savedViewerFixture{archive, archivedRepo, history.SessionID, archivedPath, core.Result()}
}

func TestViewerSavedVerificationPublicationAndAudit(t *testing.T) {
	for _, mode := range []string{"mixed", "refuted", "interrupted", "bypass", "pathless"} {
		t.Run(mode, func(t *testing.T) {
			fixture := savedViewerReview(t, mode)
			page, err := LoadSession(fixture.root, fixture.repo, fixture.id)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(fixture.path)
			if err != nil {
				t.Fatal(err)
			}
			state, err := session.ReplayReviewSession(bytes.NewReader(data), fixture.id)
			if err != nil {
				t.Fatal(err)
			}
			want, report, err := state.SavedFindings()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(page.VerificationReport, report) || !reflect.DeepEqual(report, fixture.result.Report) {
				t.Fatal("audit loses saved records")
			}
			if len(page.Comments) != len(want) || page.Summary.CommentCount != len(want) {
				t.Fatalf("publication: got %d want %d", len(page.Comments), len(want))
			}
			marks := map[string]bool{}
			for i, comment := range page.Comments {
				if comment.claim() != want[i].Finding || comment.CandidateID != want[i].CandidateID || *comment.Verification != want[i].Verification {
					t.Fatalf("finding %d differs: %+v", i, comment)
				}
				if comment.MarkID == "" || marks[comment.MarkID] {
					t.Fatal("duplicate candidates share a mark")
				}
				marks[comment.MarkID] = true
			}
			summaries, err := ListSessions(fixture.root, fixture.repo)
			if err != nil || len(summaries) != 1 || summaries[0].CommentCount != len(want) {
				t.Fatalf("summary: %+v %v", summaries, err)
			}
			response := httptest.NewRecorder()
			newMux(fixture.root).ServeHTTP(response, httptest.NewRequest("GET", "/r/"+fixture.repo+"/"+fixture.id, nil))
			if response.Code != 200 {
				t.Fatalf("HTTP %d: %s", response.Code, response.Body.String())
			}
			var offline bytes.Buffer
			if err := ExportSession(&offline, fixture.root, fixture.repo, fixture.id); err != nil {
				t.Fatal(err)
			}
			for _, html := range []string{response.Body.String(), offline.String()} {
				if strings.Contains(html, "<script>bad()") || strings.Contains(html, "<img src=x onerror=bad()>") || strings.Contains(html, "WRONG ROOT") {
					t.Fatal("unsafe or unrelated content")
				}
				for _, record := range report.Records {
					if !strings.Contains(html, record.Candidate.ID) || !strings.Contains(html, record.Reason.Code) {
						t.Fatalf("audit omits %s", record.Candidate.ID)
					}
				}
				if !strings.Contains(html, "Complete verification audit") || !strings.Contains(html, "Saved private analysis") {
					t.Fatal("complete saved audit unavailable")
				}
			}
			if mode == "refuted" && len(page.Comments) != 0 {
				t.Fatal("refuted claim appears as a finding")
			}
			if mode == "interrupted" && page.VerificationCompletion.Incomplete != 4 {
				t.Fatal("interruption appears complete")
			}
			if mode == "bypass" && page.VerificationCompletion.Skipped != 4 {
				t.Fatal("bypass appears complete")
			}
		})
	}
}

func TestViewerInvalidAuditPreservesMultiplicity(t *testing.T) {
	for _, corruption := range []string{"binding", "unsupported", "torn", "malformed"} {
		t.Run(corruption, func(t *testing.T) {
			fixture := savedViewerReview(t, "mixed")
			data, err := os.ReadFile(fixture.path)
			if err != nil {
				t.Fatal(err)
			}
			switch corruption {
			case "binding":
				data = bytes.ReplaceAll(data, []byte(`"context_id":`), []byte(`"context_id":"wrong","unused_context":`))
			case "unsupported":
				data = bytes.ReplaceAll(data, []byte("ocr.verification/v1"), []byte("ocr.verification/v999"))
			case "torn":
				data = append(data, []byte(`{"type":"verification_decision"`)...)
			case "malformed":
				data = append(data, []byte("not-json\n")...)
			}
			if err := os.WriteFile(fixture.path, data, 0600); err != nil {
				t.Fatal(err)
			}
			page, err := LoadSession(fixture.root, fixture.repo, fixture.id)
			if err != nil {
				t.Fatal(err)
			}
			state, err := session.ReplayReviewSession(bytes.NewReader(data), fixture.id)
			if err != nil {
				t.Fatal(err)
			}
			want, report, auditErr := state.SavedFindings()
			if auditErr == nil || page.VerificationError == "" {
				t.Fatal("audit corruption is hidden")
			}
			if len(page.Comments) != len(want) || !reflect.DeepEqual(page.VerificationReport, report) {
				t.Fatal("recovery drops usable claims")
			}
			for i, comment := range page.Comments {
				if comment.claim() != want[i].Finding || comment.CandidateID != want[i].CandidateID || comment.Verification.Verdict != v.Uncertain {
					t.Fatal("corruption establishes trust or changes claims")
				}
			}
			var offline bytes.Buffer
			if err := ExportSession(&offline, fixture.root, fixture.repo, fixture.id); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(offline.String(), "Verification audit error:") {
				t.Fatal("offline audit error is hidden")
			}
		})
	}
}

func TestViewerHistoricalMarksAndExcludedScan(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "repo"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"commit", "full_scan"} {
		records := []any{
			map[string]any{"type": "session_start", "reviewMode": mode},
			map[string]any{"type": "review_item_done", "uuid": "first", "filePath": "source.go", "comments": []model.LlmComment{{Content: "Equal"}, {Content: "Equal"}}},
			map[string]any{"type": "review_item_reused", "uuid": "second", "filePath": "other.go", "comments": []model.LlmComment{{Content: "Equal"}}},
		}
		if mode == "commit" {
			checkpoint := records[2].(map[string]any)
			delete(checkpoint, "filePath")
			checkpoint["newPath"] = "other.go"
		}
		var data bytes.Buffer
		for _, record := range records {
			if err := json.NewEncoder(&data).Encode(record); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(root, "repo", mode+".jsonl"), data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		page, err := LoadSession(root, "repo", mode)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Comments) != 3 {
			t.Fatal("fingerprint-less claims lose multiplicity")
		}
		if page.Comments[2].FilePath != "other.go" {
			t.Fatal("pathless historical claim loses checkpoint attribution")
		}
		for i, mark := range []string{"first#0", "first#1", "second#0"} {
			if page.Comments[i].MarkID != mark {
				t.Fatal("historical mark changes")
			}
			if mode == "commit" && page.Comments[i].Verification.Verdict != v.Uncertain {
				t.Fatal("legacy finding appears confirmed")
			}
			if mode == "full_scan" && page.Comments[i].Verification != nil {
				t.Fatal("scan receives review metadata")
			}
		}
	}
}

func TestViewerComparisonRetainsCandidateOccurrences(t *testing.T) {
	mixed := savedViewerReview(t, "mixed")
	bypass := savedViewerReview(t, "bypass")
	refuted := savedViewerReview(t, "refuted")
	root := t.TempDir()
	repoDir := filepath.Join(root, "repo")
	if err := os.Mkdir(repoDir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []savedViewerFixture{mixed, bypass, refuted} {
		data, err := os.ReadFile(fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		var rewritten bytes.Buffer
		for _, line := range bytes.Split(data, []byte{'\n'}) {
			if len(line) == 0 {
				continue
			}
			var record map[string]any
			if err := json.Unmarshal(line, &record); err != nil {
				t.Fatal(err)
			}
			if record["type"] == "session_start" {
				record["cwd"] = "/unavailable/shared-repository"
			}
			if record["type"] == "session_end" && fixture.id == refuted.id {
				record["run_manifest"] = map[string]any{"schema_version": "ocr.run-manifest/v1", "operation": "review", "terminal_state": "partial", "coverage": map[string]any{"selected": []any{map[string]any{"path": "source.go"}}, "completed": []any{}, "failed": []any{map[string]any{"path": "source.go"}}}}
			}
			if err := json.NewEncoder(&rewritten).Encode(record); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(repoDir, fixture.id+".jsonl"), rewritten.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, reverse := range []bool{false, true} {
		before, after := mixed, bypass
		if reverse {
			before, after = after, before
		}
		response := httptest.NewRecorder()
		newMux(root).ServeHTTP(response, httptest.NewRequest("GET", "/r/repo/compare?before="+before.id+"&after="+after.id, nil))
		if response.Code != 200 {
			t.Fatalf("comparison: %s", response.Body.String())
		}
		html := response.Body.String()
		bucket := func(name string) string {
			marker := "bucket-" + name + "\""
			_, rest, ok := strings.Cut(html, marker)
			if !ok {
				t.Fatalf("missing bucket %s", name)
			}
			section, _, _ := strings.Cut(rest, "</section>")
			return section
		}
		if !reverse {
			for _, record := range bypass.result.Report.Records[:3] {
				if !strings.Contains(bucket("persisting"), record.Candidate.ID) {
					t.Fatal("persisting metadata does not follow comparison occurrence consumption")
				}
			}
			if !strings.Contains(bucket("new"), bypass.result.Report.Records[3].Candidate.ID) {
				t.Fatal("new remainder takes a persisting identity")
			}
		} else {
			if !strings.Contains(bucket("resolved"), bypass.result.Report.Records[3].Candidate.ID) || !strings.Contains(bucket("resolved"), "verification_disabled") {
				t.Fatal("resolved finding loses before-side bypass metadata")
			}
			for _, record := range mixed.result.Report.Records[1:] {
				if !strings.Contains(bucket("persisting"), record.Candidate.ID) {
					t.Fatal("persisting finding loses after-side metadata")
				}
			}
		}
	}
	response := httptest.NewRecorder()
	newMux(root).ServeHTTP(response, httptest.NewRequest("GET", "/r/repo/compare?before="+mixed.id+"&after="+refuted.id, nil))
	if response.Code != 200 {
		t.Fatalf("partial comparison: %s", response.Body.String())
	}
	_, notReviewed, ok := strings.Cut(response.Body.String(), "bucket-not-reviewed\"")
	if !ok {
		t.Fatal("partial coverage loses its not-reviewed bucket")
	}
	for _, record := range mixed.result.Report.Records[1:] {
		if !strings.Contains(notReviewed, record.Candidate.ID) || !strings.Contains(notReviewed, record.Reason.Code) {
			t.Fatal("not-reviewed finding loses its before-side identity or reason")
		}
	}
}
