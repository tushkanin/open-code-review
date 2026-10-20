// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package session

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alibaba/open-code-review/internal/diff"
	"github.com/alibaba/open-code-review/internal/model"
	v "github.com/alibaba/open-code-review/internal/verification"
)

const verificationCode = "package sample\nfunc divide(n int) int {\n if n == 0 { return 0 }\n return 10 / n\n}\n"

var savedProvenance = v.Provenance{Provider: "anthropic", Model: "claude", ContractVersion: "fixture-contract"}

type verificationFixture struct {
	repo    string
	view    *diff.EvidenceView
	context v.Context
	id      string
	core    *v.Core
	history *SessionHistory
}

func durableFixture(t *testing.T) *verificationFixture {
	t.Helper()
	setTestHome(t, t.TempDir())
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = repo
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull)
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	git("config", "user.email", "fixture@example.com")
	git("config", "user.name", "Fixture")
	if err := os.WriteFile(filepath.Join(repo, "source.go"), []byte("package sample\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "base")
	if err := os.WriteFile(filepath.Join(repo, "source.go"), []byte(verificationCode), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "guard")
	provider := diff.NewCommitProvider(repo, "HEAD", nil)
	set, err := provider.GetDiffSet(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	input := v.ContextInputs{Diffs: set.Included, GroupContext: "arithmetic helpers", Scope: "selected source", Rules: "Check runtime correctness.", Background: "Zero is an allowed input.", ContractFingerprint: "effective-prompts-and-tools", Mode: v.Enabled}
	saved, err := v.NewContext(provider.Evidence(), input)
	if err != nil {
		t.Fatal(err)
	}
	id, err := saved.Identity()
	if err != nil {
		t.Fatal(err)
	}
	core, err := v.New(provider.Evidence(), id, savedProvenance)
	if err != nil {
		t.Fatal(err)
	}
	history := New(repo, "fixture", "claude", SessionOptions{ReviewMode: ReviewModeCommit, Operation: OperationReview})
	t.Cleanup(func() { _ = history.Finalize() })
	return &verificationFixture{repo, provider.Evidence(), saved, id, core, history}
}

func (f *verificationFixture) capture(t *testing.T, labels ...string) []v.Candidate {
	t.Helper()
	findings := make([]model.LlmComment, len(labels))
	for i, label := range labels {
		findings[i] = model.LlmComment{Path: "source.go", Content: label, ExistingCode: "10 / n", SuggestionCode: "guard n", StartLine: 2, EndLine: 4, Severity: "low", Category: "style", Thinking: "Inspect the caller."}
	}
	candidates := f.core.Capture(findings, v.Origin{SessionID: f.history.SessionID, GroupID: "arithmetic", Round: 1, Checkpoint: "source.go"})
	result := f.core.Result()
	result.Report.Records = result.Report.Records[len(result.Report.Records)-len(labels):]
	requireDurable(t, f.history.CaptureVerification(f.context, result.Report))
	return candidates
}

func requireDurable(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func (f *verificationFixture) citation(t *testing.T) v.Citation {
	t.Helper()
	source, err := f.core.Issue(context.Background(), "source.go", 2, 4)
	if err != nil || source.Citation == nil {
		t.Fatalf("source=%+v error=%v", source, err)
	}
	return *source.Citation
}

func durableDecision(id string, verdict v.Verdict, citation v.Citation) v.Decision {
	return v.Decision{CandidateID: id, Verdict: verdict, Reason: v.Reason{Code: "source_trace", Explanation: "The source determines the claim."}, Confirmation: &v.Confirmation{Trigger: "n reaches the division", FailurePath: "divide evaluates 10 / n", Consequence: "The quotient leaves the function."}, Contradiction: "The function rejects zero before division.", Citations: []v.Citation{citation}}
}

func (f *verificationFixture) item() ResumeItem {
	return ResumeItem{FilePath: "source.go", OldPath: "source.go", NewPath: "source.go", Fingerprint: "selected-diff"}
}

func (f *verificationFixture) close(t *testing.T, history *SessionHistory) {
	t.Helper()
	b := history.Manifest()
	b.SetRepository(ManifestRepository{IdentitySHA256: parentIdentity.RepositorySHA256})
	b.SetInput(ManifestInput{Mode: InputModeCommit, SourceArtifactSHA256: parentIdentity.SourceArtifactSHA256, ResolvedBase: f.view.Input().ResolvedBase, ResolvedHead: f.view.Input().ResolvedHead})
	b.SetExecution(ManifestExecution{Provider: "anthropic", Model: "claude", RuleConfigSHA256: parentIdentity.RuleConfigSHA256})
	item := CoverageItem{ItemID: ItemID(OperationReview, InputModeCommit, "source.go", "source.go"), Path: "source.go", OldPath: "source.go", Fingerprint: f.item().Fingerprint}
	requireDurable(t, b.RegisterSelected(item))
	requireDurable(t, b.SealSelected())
	requireDurable(t, b.MarkCompleted(item.ItemID))
	manifest, err := b.Finalize(0)
	requireDurable(t, err)
	history.SetFinalManifest(&manifest)
	requireDurable(t, history.Finalize())
}

func (f *verificationFixture) request() VerificationReuseRequest {
	admission := request(nil)
	admission.Identity.Mode = InputModeCommit
	return VerificationReuseRequest{Options: SessionOptions{ReviewMode: ReviewModeCommit}, Admission: admission, Fingerprints: []string{f.item().Fingerprint}, Context: f.context, View: f.view, Provenance: v.Provenance{Provider: "next", Model: "next", ContractVersion: "fixture-contract"}}
}

func (f *verificationFixture) load(t *testing.T) *ResumeState {
	t.Helper()
	state, err := LoadReviewResumeState(f.repo, f.history.SessionID)
	requireDurable(t, err)
	return state
}

func assertDurableResult(t *testing.T, state *ResumeState, want v.Result) {
	t.Helper()
	got, err := state.VerificationResult()
	requireDurable(t, err)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("replay differs\ngot: %#v\nwant: %#v", got, want)
	}
}

func TestVerificationDurableMixedReplayAndChildReuse(t *testing.T) {
	f := durableFixture(t)
	candidates := f.capture(t, "confirmed", "refuted", "unresolved", "invalid", "conflicting", "interrupted")
	initial := f.core.Result()
	state := f.load(t)
	assertDurableResult(t, state, initial)
	if _, err := state.ReuseVerification(context.Background(), f.request()); err == nil {
		t.Fatal("crash records bypass manifest admission")
	}
	citation := f.citation(t)
	f.core.Submit([]v.Decision{durableDecision(candidates[0].ID, v.Confirmed, citation), durableDecision(candidates[1].ID, v.Refuted, citation)})
	partial := f.core.Result()
	scoped := partial
	scoped.Report.Records = scoped.Report.Records[:2]
	requireDurable(t, f.history.RecordVerificationSnapshot(scoped))
	assertDurableResult(t, f.load(t), partial)
	invalid := durableDecision(candidates[3].ID, v.Refuted, citation)
	invalid.Citations[0].Excerpt = "not delivered"
	conflict := durableDecision(candidates[4].ID, v.Refuted, citation)
	opposite := durableDecision(candidates[4].ID, v.Confirmed, citation)
	f.core.Submit([]v.Decision{{CandidateID: candidates[2].ID, Verdict: v.Uncertain, Reason: v.Reason{Code: "caller_unavailable", Explanation: "The caller is unavailable."}}, invalid, conflict, opposite})
	f.core.Fail("The provider disconnects.", candidates[5].ID)
	want := f.core.Result()
	requireDurable(t, f.history.RecordVerificationSnapshot(want))
	requireDurable(t, f.history.RecordVerificationTerminal(f.id))
	requireDurable(t, f.history.RecordVerificationCheckpoint(f.item(), f.id, ""))
	f.close(t, f.history)
	state = f.load(t)
	assertDurableResult(t, state, want)
	req := f.request()
	req.Admission.Provider = "next"
	req.Admission.ProviderExplicit = true
	req.Admission.Model = "next"
	reused, err := state.ReuseVerification(context.Background(), req)
	requireDurable(t, err)
	if reused.Disposition != VerificationRecheck || !reflect.DeepEqual(reused.Recheck, []v.Candidate{candidates[5]}) || !reflect.DeepEqual(reused.Result, want) {
		t.Fatalf("reuse=%+v", reused)
	}
	requireDurable(t, reused.Core.BindInvocation(f.view, reused.Recheck, req.Provenance))
	completed := reused.Core.Result().Report.Records[0]
	if completed.Provenance != savedProvenance || completed.Citations[0].ID != citation.ID {
		t.Fatal("reuse rewrites completed provenance or receipts")
	}
	reused.Core.Submit([]v.Decision{{CandidateID: candidates[5].ID, Verdict: v.Uncertain, Reason: v.Reason{Code: "unresolved", Explanation: "The caller remains unavailable."}}})
	child := New(f.repo, "fixture", "claude", SessionOptions{ReviewMode: ReviewModeCommit, Operation: OperationReview})
	childResult := reused.Core.Result()
	requireDurable(t, child.RecordVerificationReuse(f.context, childResult))
	requireDurable(t, child.RecordVerificationCheckpoint(f.item(), f.id, f.history.SessionID))
	f.close(t, child)
	path, err := SessionFilePath(f.repo, f.history.SessionID)
	requireDurable(t, err)
	requireDurable(t, os.Remove(path))
	childState, err := LoadReviewResumeState(f.repo, child.SessionID)
	requireDurable(t, err)
	assertDurableResult(t, childState, childResult)
	again, err := childState.ReuseVerification(context.Background(), f.request())
	requireDurable(t, err)
	if again.Disposition != VerificationReusable || !reflect.DeepEqual(again.Result, childResult) {
		t.Fatalf("child reuse=%+v", again)
	}
}

func TestVerificationRefutationOnlyAndLaterInterruption(t *testing.T) {
	f := durableFixture(t)
	candidates := f.capture(t, "refuted")
	f.core.Submit([]v.Decision{durableDecision(candidates[0].ID, v.Refuted, f.citation(t))})
	requireDurable(t, f.history.RecordVerificationSnapshot(f.core.Result()))
	requireDurable(t, f.history.RecordVerificationTerminal(f.id))
	requireDurable(t, f.history.RecordVerificationCheckpoint(f.item(), f.id, ""))
	before := f.load(t)
	item, ok := before.Item(f.item().Fingerprint)
	if !ok || len(item.Comments) != 0 {
		t.Fatalf("refutation checkpoint=%+v", item)
	}
	assertDurableResult(t, before, f.core.Result())
	f.capture(t, "later round interrupted")
	f.close(t, f.history)
	state := f.load(t)
	reused, err := state.ReuseVerification(context.Background(), f.request())
	requireDurable(t, err)
	result := f.core.Result()
	if reused.Disposition != VerificationRecheck || !reflect.DeepEqual(reused.Result, result) || !reflect.DeepEqual(reused.Recheck, []v.Candidate{result.Report.Records[1].Candidate}) {
		t.Fatalf("later interruption=%+v", reused)
	}
}

func finishedVerification(t *testing.T) *verificationFixture {
	t.Helper()
	f := durableFixture(t)
	candidates := f.capture(t, "refuted")
	f.core.Submit([]v.Decision{durableDecision(candidates[0].ID, v.Refuted, f.citation(t))})
	requireDurable(t, f.history.RecordVerificationSnapshot(f.core.Result()))
	requireDurable(t, f.history.RecordVerificationTerminal(f.id))
	requireDurable(t, f.history.RecordVerificationCheckpoint(f.item(), f.id, ""))
	f.close(t, f.history)
	return f
}

func TestVerificationContextChangesRequireFreshReview(t *testing.T) {
	f := finishedVerification(t)
	state := f.load(t)
	reused, err := state.ReuseVerification(context.Background(), f.request())
	requireDurable(t, err)
	if reused.Disposition != VerificationReusable || len(reused.Result.Published) != 0 || !reflect.DeepEqual(reused.Result, f.core.Result()) {
		t.Fatalf("refutation-only reuse=%+v", reused)
	}
	changes := map[string]func(*v.Context){
		"diff":       func(c *v.Context) { c.Diffs = append([]model.Diff{}, c.Diffs...); c.Diffs[0].Diff += "\ncontext" },
		"content":    func(c *v.Context) { c.Diffs = append([]model.Diff{}, c.Diffs...); c.Diffs[0].NewFileContent += "\n" },
		"path":       func(c *v.Context) { c.Diffs = append([]model.Diff{}, c.Diffs...); c.Diffs[0].OldPath = "prior.go" },
		"flags":      func(c *v.Context) { c.Diffs = append([]model.Diff{}, c.Diffs...); c.Diffs[0].IsRenamed = true },
		"group":      func(c *v.Context) { c.GroupContext += " caller" },
		"scope":      func(c *v.Context) { c.Scope += " more" },
		"rules":      func(c *v.Context) { c.Rules += " extra" },
		"background": func(c *v.Context) { c.Background += " extra" },
		"contract":   func(c *v.Context) { c.ContractFingerprint = "different-prompts-or-tools" },
		"mode":       func(c *v.Context) { c.Mode = v.Disabled },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			req := f.request()
			change(&req.Context)
			got, err := state.ReuseVerification(context.Background(), req)
			requireDurable(t, err)
			if got.Disposition != VerificationFresh || got.Core != nil {
				t.Fatalf("changed context reuses result: %+v", got)
			}
		})
	}
	for _, change := range []func(*v.Context){func(c *v.Context) { c.Input.ResolvedBase = "other" }, func(c *v.Context) { c.Input.ResolvedHead = "other" }, func(c *v.Context) { c.Input.ExactRange = "other" }, func(c *v.Context) { c.EvidenceState = "other" }} {
		changed := f.context
		change(&changed)
		id, err := changed.Identity()
		requireDurable(t, err)
		if id == f.id {
			t.Fatal("code state is absent from context identity")
		}
		req := f.request()
		req.Context = changed
		if _, err := state.ReuseVerification(context.Background(), req); err == nil {
			t.Fatal("context does not bind the supplied captured view")
		}
	}
}

func TestVerificationReuseRetainsExistingAdmission(t *testing.T) {
	f := finishedVerification(t)
	for name, change := range map[string]func(*VerificationReuseRequest){
		"workspace":  func(r *VerificationReuseRequest) { r.Options.ReviewMode = ReviewModeWorkspace },
		"mode":       func(r *VerificationReuseRequest) { r.Admission.Identity.Mode = InputModeRange },
		"repository": func(r *VerificationReuseRequest) { r.Admission.Identity.RepositorySHA256 = "other" },
		"input":      func(r *VerificationReuseRequest) { r.Admission.Identity.SourceArtifactSHA256 = "other" },
		"rules":      func(r *VerificationReuseRequest) { r.Admission.Identity.RuleConfigSHA256 = "other" },
		"provider":   func(r *VerificationReuseRequest) { r.Admission.Provider = "implicit" },
		"model":      func(r *VerificationReuseRequest) { r.Admission.Model = "implicit" },
	} {
		t.Run(name, func(t *testing.T) {
			req := f.request()
			change(&req)
			if _, err := f.load(t).ReuseVerification(context.Background(), req); err == nil {
				t.Fatal("existing admission is bypassed")
			}
		})
	}
	for _, explicit := range []string{"provider", "model"} {
		req := f.request()
		if explicit == "provider" {
			req.Admission.Provider = "next"
			req.Admission.ProviderExplicit = true
		} else {
			req.Admission.Model = "next"
			req.Admission.ModelExplicit = true
		}
		got, err := f.load(t).ReuseVerification(context.Background(), req)
		requireDurable(t, err)
		if got.Disposition != VerificationReusable || got.Result.Report.Records[0].Provenance != savedProvenance {
			t.Fatal("explicit transition loses valid provenance")
		}
	}
	for _, kind := range []string{"missing", "unsupported", "failed coverage"} {
		state := f.load(t)
		switch kind {
		case "missing":
			state.Manifest = nil
		case "unsupported":
			state.Manifest.SchemaVersion = "future"
		case "failed coverage":
			state.Manifest.Coverage.Completed = nil
		}
		got, err := state.ReuseVerification(context.Background(), f.request())
		if kind == "failed coverage" {
			if err != nil || got.Disposition != VerificationFresh {
				t.Fatalf("coverage bypass: %+v %v", got, err)
			}
		} else if err == nil {
			t.Fatal("invalid manifest bypasses admission")
		}
	}
}

func TestVerificationLegacyAndBypassRemainUnverified(t *testing.T) {
	f := durableFixture(t)
	f.history.RecordReviewItemDone("source.go", "source.go", "source.go", f.item().Fingerprint, []model.LlmComment{{Path: "source.go", Content: "legacy"}})
	f.close(t, f.history)
	state := f.load(t)
	got, err := state.ReuseVerification(context.Background(), f.request())
	requireDurable(t, err)
	if got.Disposition != VerificationFresh {
		t.Fatal("legacy comments imply verification")
	}
	comments, err := LoadComments(f.repo, f.history.SessionID)
	requireDurable(t, err)
	if !reflect.DeepEqual(comments, []model.LlmComment{{Path: "source.go", Content: "legacy"}}) {
		t.Fatal("legacy history changes")
	}
	child := New(f.repo, "fixture", "claude", SessionOptions{ReviewMode: ReviewModeCommit, Operation: OperationReview})
	f.history = child
	f.capture(t, "bypassed")
	f.core.Bypass()
	requireDurable(t, child.RecordVerificationSnapshot(f.core.Result()))
	requireDurable(t, child.RecordVerificationTerminal(f.id))
	requireDurable(t, child.RecordVerificationCheckpoint(f.item(), f.id, ""))
	f.close(t, child)
	got, err = f.load(t).ReuseVerification(context.Background(), f.request())
	requireDurable(t, err)
	if got.Disposition != VerificationFresh || len(f.core.Result().Published) != 1 {
		t.Fatal("bypassed work becomes verified")
	}
}

func rewriteVerificationFile(t *testing.T, f *verificationFixture, mutate func([]map[string]any) []map[string]any, torn bool) {
	t.Helper()
	path, err := SessionFilePath(f.repo, f.history.SessionID)
	requireDurable(t, err)
	records := mutate(readJSONLRecords(t, path))
	var data []byte
	for _, record := range records {
		line, err := json.Marshal(record)
		requireDurable(t, err)
		data = append(data, line...)
		data = append(data, '\n')
	}
	if torn {
		data = append(data, []byte(`{"type":"verification_decision"`)...)
	}
	requireDurable(t, os.WriteFile(path, data, 0600))
}

func TestVerificationCorruptDataNeverSuppressesOrReuses(t *testing.T) {
	for _, kind := range []string{"version", "claim", "revision", "missing terminal", "missing capture", "binding", "citation", "malformed", "torn"} {
		t.Run(kind, func(t *testing.T) {
			f := finishedVerification(t)
			rewriteVerificationFile(t, f, func(records []map[string]any) []map[string]any {
				out := []map[string]any{}
				for _, record := range records {
					typ := record["type"]
					if kind == "missing terminal" && typ == "verification_terminal" || kind == "missing capture" && typ == "verification_capture" {
						continue
					}
					if typ == "verification_decision" {
						event := record["verification"].(map[string]any)
						switch kind {
						case "version":
							event["version"] = "future"
						case "claim":
							event["records"].([]any)[0].(map[string]any)["candidate"].(map[string]any)["finding"].(map[string]any)["content"] = "altered claim"
						case "revision":
							event["revisions"].([]any)[0].(map[string]any)["revision"] = 99
						case "citation":
							saved := event["records"].([]any)[0].(map[string]any)
							saved["citations"].([]any)[0].(map[string]any)["excerpt"] = "altered"
							saved["submissions"].([]any)[0].(map[string]any)["citations"].([]any)[0].(map[string]any)["excerpt"] = "altered"
						case "malformed":
							record["verification"] = "bad"
						}
					}
					if kind == "binding" && typ == "review_item_done" {
						record["verification"].(map[string]any)["revisions"] = []any{}
					}
					out = append(out, record)
				}
				return out
			}, kind == "torn")
			state := f.load(t)
			got, err := state.ReuseVerification(context.Background(), f.request())
			requireDurable(t, err)
			if got.Disposition != VerificationFresh {
				t.Fatalf("corrupt data reused: %+v", got)
			}
			report, replayErr := state.VerificationResult()
			if kind != "missing capture" && (replayErr == nil || len(report.Published) != 1 || report.Published[0].Finding.Content != "refuted") {
				t.Fatalf("intact claim lost or suppressed: %+v %v", report, replayErr)
			}
			if kind == "malformed" {
				if _, err := LoadResumeState(f.repo, f.history.SessionID); err == nil {
					t.Fatal("strict replay accepts malformed verification")
				}
			}
		})
	}
}

type failedVerificationSink struct{}

func (failedVerificationSink) Write([]byte) (int, error) {
	return 0, errors.New("injected storage failure")
}

func TestVerificationPersistenceFailuresRemainVisible(t *testing.T) {
	for _, phase := range []string{"capture", "decision", "terminal", "checkpoint"} {
		for _, failure := range []string{"write", "flush"} {
			t.Run(phase+"/"+failure, func(t *testing.T) {
				f := durableFixture(t)
				candidates := f.capture(t, "retained audit")
				before := f.core.Result()
				if phase == "terminal" || phase == "checkpoint" {
					f.core.Submit([]v.Decision{durableDecision(candidates[0].ID, v.Refuted, f.citation(t))})
					requireDurable(t, f.history.RecordVerificationSnapshot(f.core.Result()))
					before = f.core.Result()
				}
				if phase == "checkpoint" {
					requireDurable(t, f.history.RecordVerificationTerminal(f.id))
				}
				size := 1 << 20
				if failure == "write" {
					size = 16
				}
				f.history.persist.writer = bufio.NewWriterSize(failedVerificationSink{}, size)
				var err error
				switch phase {
				case "capture":
					extra := f.core.Capture([]model.LlmComment{{Path: "source.go", Content: strings.Repeat("unwritten", 100)}}, v.Origin{SessionID: f.history.SessionID})
					record := f.core.Result().Report.Records[1]
					if record.Candidate.ID != extra[0].ID {
						t.Fatal("fixture identity differs")
					}
					err = f.history.CaptureVerification(f.context, v.Report{Version: v.RecordVersion, Records: []v.Record{record}})
				case "decision":
					f.core.Fail("The request stops.")
					err = f.history.RecordVerificationSnapshot(f.core.Result())
				case "terminal":
					err = f.history.RecordVerificationTerminal(f.id)
				case "checkpoint":
					err = f.history.RecordVerificationCheckpoint(f.item(), f.id, "")
				}
				if err == nil || !strings.Contains(err.Error(), "injected storage failure") {
					t.Fatalf("failure hidden: %v", err)
				}
				if err := f.history.RecordVerificationCheckpoint(f.item(), f.id, ""); err == nil {
					t.Fatal("later checkpoint conceals persistence failure")
				}
				if err := f.history.Finalize(); err == nil {
					t.Fatal("finalization conceals persistence failure")
				}
				state := f.load(t)
				assertDurableResult(t, state, before)
				if _, err := state.ReuseVerification(context.Background(), f.request()); err == nil {
					t.Fatal("failed delivery bypasses manifest admission")
				}
			})
		}
	}
}

func TestVerificationMarshalFailureInvalidatesEarlierBinding(t *testing.T) {
	f := durableFixture(t)
	candidates := f.capture(t, "intact")
	f.core.Submit([]v.Decision{durableDecision(candidates[0].ID, v.Refuted, f.citation(t))})
	requireDurable(t, f.history.RecordVerificationSnapshot(f.core.Result()))
	requireDurable(t, f.history.RecordVerificationTerminal(f.id))
	requireDurable(t, f.history.RecordVerificationCheckpoint(f.item(), f.id, ""))
	if err := f.history.persist.writeVerification("verification_decision", map[string]any{"invalid": make(chan int)}); err == nil {
		t.Fatal("marshal failure hidden")
	}
	b := f.history.Manifest()
	b.SetInput(ManifestInput{Mode: InputModeCommit, SourceArtifactSHA256: parentIdentity.SourceArtifactSHA256})
	b.SetExecution(ManifestExecution{Provider: "anthropic", Model: "claude", RuleConfigSHA256: parentIdentity.RuleConfigSHA256})
	b.SetRepository(ManifestRepository{IdentitySHA256: parentIdentity.RepositorySHA256})
	item := CoverageItem{ItemID: "source", Path: "source.go", Fingerprint: f.item().Fingerprint}
	requireDurable(t, b.RegisterSelected(item))
	requireDurable(t, b.MarkCompleted(item.ItemID))
	manifest, err := b.Finalize(0)
	requireDurable(t, err)
	f.history.SetFinalManifest(&manifest)
	if err := f.history.Finalize(); err == nil {
		t.Fatal("finalization conceals marshal failure")
	}
	state := f.load(t)
	result, err := state.VerificationResult()
	if err == nil || len(result.Report.Records) != 1 || result.Report.Records[0].Verdict != v.Refuted || len(result.Published) != 1 {
		t.Fatal("failure hides audit or suppresses its claim")
	}
	got, err := state.ReuseVerification(context.Background(), f.request())
	requireDurable(t, err)
	if got.Disposition != VerificationFresh {
		t.Fatal("earlier binding conceals failed persistence")
	}
}

func TestVerificationConcurrentReuseRetainsAdmissionAndCandidateIdentities(t *testing.T) {
	f := durableFixture(t)
	candidates := f.capture(t, "confirmed", "refuted", "interrupted")
	citation := f.citation(t)
	f.core.Submit([]v.Decision{durableDecision(candidates[0].ID, v.Confirmed, citation), durableDecision(candidates[1].ID, v.Refuted, citation)})
	f.core.Fail("The provider disconnects.", candidates[2].ID)
	requireDurable(t, f.history.RecordVerificationSnapshot(f.core.Result()))
	requireDurable(t, f.history.RecordVerificationTerminal(f.id))
	requireDurable(t, f.history.RecordVerificationCheckpoint(f.item(), f.id, ""))
	f.close(t, f.history)
	state := f.load(t)
	req := f.request()
	type outcome struct {
		valid      VerificationReuse
		invalid    VerificationReuse
		validErr   error
		invalidErr error
	}
	const workers = 16
	start := make(chan struct{})
	results := make(chan outcome, workers)
	for range workers {
		go func() {
			<-start
			valid, validErr := state.ReuseVerification(context.Background(), req)
			invalidReq := req
			invalidReq.Fingerprints = []string{f.item().Fingerprint, "unsettled-group-member"}
			invalid, invalidErr := state.ReuseVerification(context.Background(), invalidReq)
			results <- outcome{valid, invalid, validErr, invalidErr}
		}()
	}
	close(start)
	want := f.core.Result()
	for range workers {
		result := <-results
		requireDurable(t, result.validErr)
		requireDurable(t, result.invalidErr)
		if result.valid.Disposition != VerificationRecheck || !reflect.DeepEqual(result.valid.Result, want) || !reflect.DeepEqual(result.valid.Recheck, []v.Candidate{candidates[2]}) {
			t.Fatalf("concurrent reuse changes candidates, decisions, or recheck admission: %+v", result.valid)
		}
		if result.invalid.Disposition != VerificationFresh || result.invalid.Core != nil {
			t.Fatalf("concurrent reuse admits an unsettled group member: %+v", result.invalid)
		}
	}
}
