// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package llmloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/alibaba/open-code-review/internal/llm"
	"github.com/alibaba/open-code-review/internal/tool"
	"github.com/alibaba/open-code-review/internal/verification"
)

const verifierResultBytes = 64 << 10

type verifierEvidenceResult struct {
	Sources     []verification.Source `json:"sources"`
	Truncated   bool                  `json:"truncated"`
	Unavailable []string              `json:"unavailable,omitempty"`
	Coverage    string                `json:"coverage"`
}

func (v *verifierRun) source(ctx context.Context, path string, start, end int) (verification.Source, error) {
	v.input.CoreMu.Lock()
	defer v.input.CoreMu.Unlock()
	return v.input.Core.Issue(ctx, path, start, end)
}

// Receipt and excerpt form one atomic response unit. A byte cap must never cut either field.
func (v *verifierRun) evidenceJSON(result verifierEvidenceResult) (string, error) {
	encoded := struct {
		Sources     []json.RawMessage `json:"sources"`
		Truncated   bool              `json:"truncated"`
		Unavailable []string          `json:"unavailable,omitempty"`
		Coverage    string            `json:"coverage"`
	}{Sources: []json.RawMessage{}, Truncated: result.Truncated, Coverage: result.Coverage}
	unavailableBytes := 0
	for _, path := range result.Unavailable {
		data, err := json.Marshal(path)
		if err != nil {
			return "", err
		}
		unavailableBytes += len(data) + 1
		if unavailableBytes > verifierResultBytes/4 {
			encoded.Truncated = true
			break
		}
		encoded.Unavailable = append(encoded.Unavailable, path)
	}
	header, err := json.Marshal(encoded)
	if err != nil {
		return "", err
	}
	used := len(header)
	for _, source := range result.Sources {
		if len(source.Evidence.Content) > verifierResultBytes {
			encoded.Truncated = true
			break
		}
		data, err := json.Marshal(source)
		if err != nil {
			return "", err
		}
		size := len(data)
		if len(encoded.Sources) > 0 {
			size++
		}
		if used+size > verifierResultBytes {
			encoded.Truncated = true
			break
		}
		used += size
		encoded.Sources = append(encoded.Sources, data)
	}
	data, err := json.Marshal(encoded)
	if err != nil {
		return "", err
	}
	for _, source := range result.Sources[:len(encoded.Sources)] {
		if source.Citation != nil {
			v.pending[source.Citation.ID] = *source.Citation
		}
	}
	return string(data), nil
}

func (v *verifierRun) dispatch(ctx context.Context, call llm.ToolCall) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	switch call.Function.Name {
	case "file_read", "code_search", "file_find", "file_read_diff", "verification_submit":
	default:
		return "", false, fmt.Errorf("tool %q is forbidden in verification", call.Function.Name)
	}
	if call.Function.Name == "verification_submit" {
		return v.submit(call.Function.Arguments)
	}
	var args struct {
		FilePath      string   `json:"file_path"`
		StartLine     int      `json:"start_line"`
		EndLine       int      `json:"end_line"`
		SearchText    string   `json:"search_text"`
		CaseSensitive bool     `json:"case_sensitive"`
		Perl          bool     `json:"use_perl_regexp"`
		Patterns      []string `json:"file_patterns"`
		Query         string   `json:"query_name"`
		Paths         []string `json:"path_array"`
	}
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return "", false, err
	}
	switch call.Function.Name {
	case "file_read":
		if args.FilePath == "" {
			return "", false, errors.New("file_path is required")
		}
		start := args.StartLine
		if start <= 0 {
			start = 1
		}
		if args.EndLine > 0 && args.EndLine < start {
			return "", false, errors.New("end_line precedes start_line")
		}
		count := 500
		if args.EndLine > 0 {
			count = min(count, args.EndLine-start+1)
		}
		if start > math.MaxInt-count-1 {
			return "", false, errors.New("start_line exceeds the supported range")
		}
		evidence, err := v.input.View.Read(ctx, args.FilePath, start, count+1)
		if err != nil {
			return "", false, err
		}
		result := verifierEvidenceResult{Sources: []verification.Source{}, Coverage: "Only the returned source range has coverage. Missing or unavailable content cannot establish absence."}
		if !evidence.Available || evidence.EndLine < start {
			result.Sources = append(result.Sources, verification.Source{Evidence: evidence})
		} else {
			end := min(evidence.EndLine, start+count-1)
			result.Truncated = evidence.EndLine > end
			source, err := v.source(ctx, args.FilePath, start, end)
			if err != nil {
				return "", false, err
			}
			result.Sources = append(result.Sources, source)
		}
		text, err := v.evidenceJSON(result)
		return text, false, err
	case "code_search":
		if strings.TrimSpace(args.SearchText) == "" {
			return "", false, errors.New("search_text is required")
		}
		searchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		found, err := v.input.View.Search(searchCtx, args.SearchText, args.CaseSensitive, args.Perl, args.Patterns, 100)
		if err != nil {
			return "", false, err
		}
		result := verifierEvidenceResult{Sources: []verification.Source{}, Truncated: found.Truncated, Unavailable: found.Unavailable, Coverage: "Search covers available captured source only. Limited or unavailable coverage cannot establish absence."}
		for _, match := range found.Matches {
			source, err := v.source(searchCtx, match.Path, match.Line, match.Line)
			if err != nil {
				return "", false, err
			}
			result.Sources = append(result.Sources, source)
		}
		text, err := v.evidenceJSON(result)
		return text, false, err
	case "file_find":
		// The built-in lookup uses captured Paths when Evidence is present, with no live fallback.
		provider := tool.NewFileFind(&tool.FileReader{Evidence: v.input.View})
		text, err := provider.Execute(ctx, map[string]any{"query_name": args.Query, "case_sensitive": args.CaseSensitive})
		const coverage = "Captured paths only; file names are not source citations and cannot establish absence.\n"
		if len(text)+len(coverage) > verifierResultBytes {
			return "Coverage is limited: file lookup exceeds the byte limit. Narrow query_name. Paths cannot establish absence.", false, nil
		}
		return coverage + text, false, err
	case "file_read_diff":
		result := make(map[string]string)
		for _, path := range args.Paths {
			if err := ctx.Err(); err != nil {
				return "", false, err
			}
			for _, d := range v.input.Diffs {
				if d.NewPath == path || d.OldPath == path {
					result[path] = d.Diff
				}
			}
		}
		data, err := json.Marshal(struct {
			Diffs    map[string]string `json:"diffs"`
			Coverage string            `json:"coverage"`
		}{result, "Only supplied group diffs have coverage. Diff text is not a source citation."})
		if len(data) > verifierResultBytes {
			return "Coverage is limited: captured diffs exceed the byte limit. Narrow path_array. No source receipt is available.", false, nil
		}
		return string(data), false, err
	}
	return "", false, errors.New("unreachable verifier tool")
}

func (v *verifierRun) submit(raw string) (string, bool, error) {
	var envelope struct {
		Decisions []json.RawMessage `json:"decisions"`
		Complete  *bool             `json:"complete"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return "", false, err
	}
	if envelope.Complete == nil || envelope.Decisions == nil {
		return "", false, errors.New("decisions and complete are required")
	}
	decisions := make([]verification.Decision, 0, len(envelope.Decisions))
	problems := []string{}
	for _, rawDecision := range envelope.Decisions {
		var identity struct {
			CandidateID string `json:"candidate_id"`
		}
		if err := json.Unmarshal(rawDecision, &identity); err != nil || !v.allowed[identity.CandidateID] {
			problems = append(problems, "A decision has an unknown or unavailable candidate identifier.")
			continue
		}
		var decision verification.Decision
		if err := json.Unmarshal(rawDecision, &decision); err != nil {
			decision = verification.Decision{CandidateID: identity.CandidateID, Reason: verification.Reason{Code: "malformed_decision", Explanation: "The structured decision is malformed."}}
			problems = append(problems, "A candidate decision is malformed.")
		}
		for _, citation := range decision.Citations {
			if delivered, ok := v.delivered[citation.ID]; !ok || delivered != citation {
				// The core must not accept a shared receipt that this conversation never receives.
				decision.Verdict = "invalid_citation"
				problems = append(problems, "A decision cites undelivered or altered evidence.")
				break
			}
		}
		decisions = append(decisions, decision)
	}
	v.input.CoreMu.Lock()
	v.input.Core.Submit(decisions)
	v.input.CoreMu.Unlock()
	summaries := make(map[string]verification.Summary)
	for _, record := range v.snapshot().Report.Records {
		summaries[record.Candidate.ID] = verification.Summary{Verdict: record.Verdict, State: record.State, Reason: record.Reason}
	}
	data, err := json.Marshal(struct {
		Results  map[string]verification.Summary `json:"results"`
		Problems []string                        `json:"problems"`
	}{summaries, problems})
	if err != nil {
		return "", false, err
	}
	// The callback carries full records. Tool acknowledgments stay bounded without source receipts.
	if len(data) > verifierResultBytes {
		return "Decisions pass through candidate-local validation. Full results remain available to the caller.", *envelope.Complete, nil
	}
	return string(data), *envelope.Complete, nil
}
