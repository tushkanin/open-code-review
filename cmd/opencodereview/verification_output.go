// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/alibaba/open-code-review/internal/model"
	"github.com/alibaba/open-code-review/internal/session"
	v "github.com/alibaba/open-code-review/internal/verification"
)

type verificationCompletion struct {
	Complete   int        `json:"complete"`
	Incomplete int        `json:"incomplete"`
	Skipped    int        `json:"skipped"`
	Reasons    []v.Reason `json:"reasons"`
}

func summarizeVerification(report v.Report) verificationCompletion {
	summary := verificationCompletion{Reasons: []v.Reason{}}
	seen := make(map[v.Reason]bool)
	for _, record := range report.Records {
		switch record.State {
		case v.Complete:
			summary.Complete++
		case v.Incomplete:
			summary.Incomplete++
		case v.Skipped:
			summary.Skipped++
		}
		if record.State != v.Complete && !seen[record.Reason] {
			summary.Reasons = append(summary.Reasons, record.Reason)
			seen[record.Reason] = true
		}
	}
	return summary
}

func (p jsonOutput) MarshalJSON() ([]byte, error) {
	type plain jsonOutput
	if p.findings == nil {
		return json.Marshal(plain(p))
	}
	return json.Marshal(struct {
		plain
		Comments []findingOutput `json:"comments"`
	}{plain(p), p.findings})
}

// Presentation copies retain occurrence order and never alter the saved claims.
func projectFindings(comments []model.LlmComment, published []v.PublishedFinding) []findingOutput {
	out := make([]findingOutput, len(comments))
	for i, comment := range comments {
		out[i].LlmComment = comment
		if i < len(published) {
			summary := published[i].Verification
			out[i].CandidateID = published[i].CandidateID
			out[i].Verification = &summary
		}
	}
	return out
}

func renderFinding(finding findingOutput, out io.Writer) {
	renderComment(finding.LlmComment, out)
	if finding.Verification == nil {
		fmt.Fprintln(out, "Unverified: historical finding has no verification record.")
	} else if finding.Verification.Verdict == v.Uncertain {
		fmt.Fprintf(out, "Uncertain (%s, %s): %s\n", finding.Verification.State, sanitizeTerminal(finding.Verification.Reason.Code), sanitizeTerminal(finding.Verification.Reason.Explanation))
	}
}

func outputVerificationCompletion(out io.Writer, report v.Report) {
	completion := summarizeVerification(report)
	fmt.Fprintf(out, "Verification: %d complete, %d incomplete, %d skipped.\n", completion.Complete, completion.Incomplete, completion.Skipped)
	for _, reason := range completion.Reasons {
		fmt.Fprintf(out, "  %s: %s\n", sanitizeTerminal(reason.Code), sanitizeTerminal(reason.Explanation))
	}
}

func loadSessionFindings(repo, id string) ([]findingOutput, v.Report, error) {
	state, err := session.LoadReviewResumeState(repo, id)
	if err != nil {
		return nil, v.Report{}, err
	}
	published, report, auditErr := state.SavedFindings()
	if auditErr != nil {
		fmt.Fprintf(os.Stderr, "[ocr] WARNING verification audit: %s\n", sanitizeTerminal(auditErr.Error()))
	}
	comments := make([]model.LlmComment, len(published))
	for i, finding := range published {
		comments[i] = finding.Finding
	}
	return projectFindings(comments, published), report, nil
}

func findingClaims(findings []findingOutput) []model.LlmComment {
	claims := make([]model.LlmComment, len(findings))
	for i, finding := range findings {
		claims[i] = finding.LlmComment
	}
	return claims
}

func selectFindingMetadata(claims []model.LlmComment, findings []findingOutput) []findingOutput {
	pool := make(map[model.LlmComment][]findingOutput, len(findings))
	for _, finding := range findings {
		pool[finding.LlmComment] = append(pool[finding.LlmComment], finding)
	}
	out := make([]findingOutput, 0, len(claims))
	for _, claim := range claims {
		matches := pool[claim]
		if len(matches) > 0 {
			out = append(out, matches[0])
			pool[claim] = matches[1:]
		} else {
			out = append(out, findingOutput{LlmComment: claim})
		}
	}
	return out
}

type comparisonOutput struct {
	New         []findingOutput `json:"new"`
	Persisting  []findingOutput `json:"persisting"`
	Resolved    []findingOutput `json:"resolved"`
	NotReviewed []findingOutput `json:"not_reviewed"`
}

func projectComparison(result session.CompareResult, before, after []findingOutput) comparisonOutput {
	afterClaims := append(append([]model.LlmComment{}, result.New...), result.Persisting...)
	beforeClaims := append(append([]model.LlmComment{}, result.Resolved...), result.NotReviewed...)
	a := selectFindingMetadata(afterClaims, after)
	b := selectFindingMetadata(beforeClaims, before)
	return comparisonOutput{New: a[:len(result.New)], Persisting: a[len(result.New):], Resolved: b[:len(result.Resolved)], NotReviewed: b[len(result.Resolved):]}
}
