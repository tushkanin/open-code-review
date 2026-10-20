// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package viewer

import (
	"encoding/json"

	"github.com/alibaba/open-code-review/internal/model"
	"github.com/alibaba/open-code-review/internal/session"
	v "github.com/alibaba/open-code-review/internal/verification"
)

type verificationCompletion struct {
	Complete   int
	Incomplete int
	Skipped    int
	Reasons    []v.Reason
}

func managedReviewMode(mode string) bool {
	switch mode {
	case "", session.ReviewModeWorkspace, session.ReviewModeRange, session.ReviewModeCommit:
		return true
	default:
		return false
	}
}

func (c *ReviewComment) claim() model.LlmComment {
	return model.LlmComment{Path: c.FilePath, Content: c.Content, SuggestionCode: c.SuggestionCode, ExistingCode: c.ExistingCode, StartLine: c.StartLine, EndLine: c.EndLine, Thinking: c.Thinking, Category: c.Category, Severity: c.Severity}
}

func reviewComment(claim model.LlmComment) *ReviewComment {
	return &ReviewComment{FilePath: claim.Path, Content: claim.Content, SuggestionCode: claim.SuggestionCode, ExistingCode: claim.ExistingCode, StartLine: claim.StartLine, EndLine: claim.EndLine, Thinking: claim.Thinking, Category: claim.Category, Severity: claim.Severity}
}

func (vs *ViewSession) applySavedFindings(state *session.ResumeState) {
	findings, report, err := state.SavedFindings()
	vs.ManagedReview = true
	vs.VerificationReport = report
	if err != nil {
		vs.VerificationError = err.Error()
	}
	pool := make(map[model.LlmComment][]*ReviewComment)
	for _, comment := range vs.Comments {
		pool[comment.claim()] = append(pool[comment.claim()], comment)
	}
	vs.Comments = make([]*ReviewComment, 0, len(findings))
	for _, finding := range findings {
		comment := reviewComment(finding.Finding)
		if matches := pool[finding.Finding]; len(matches) > 0 {
			comment.MarkID = matches[0].MarkID
			pool[finding.Finding] = matches[1:]
		} else {
			comment.MarkID = finding.CandidateID
		}
		comment.CandidateID = finding.CandidateID
		summary := finding.Verification
		comment.Verification = &summary
		vs.Comments = append(vs.Comments, comment)
	}
	seen := make(map[v.Reason]bool)
	for _, record := range report.Records {
		switch record.State {
		case v.Complete:
			vs.VerificationCompletion.Complete++
		case v.Incomplete:
			vs.VerificationCompletion.Incomplete++
		case v.Skipped:
			vs.VerificationCompletion.Skipped++
		}
		if record.State != v.Complete && !seen[record.Reason] {
			vs.VerificationCompletion.Reasons = append(vs.VerificationCompletion.Reasons, record.Reason)
			seen[record.Reason] = true
		}
	}
}

func structuredDetails(value any) (string, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	return string(data), err
}

// One pool spans each side's buckets because equal claims are separate occurrences.
func selectCommentMetadata(claims []model.LlmComment, pool map[model.LlmComment][]*ReviewComment) []*ReviewComment {
	out := make([]*ReviewComment, 0, len(claims))
	for _, claim := range claims {
		if matches := pool[claim]; len(matches) > 0 {
			out = append(out, matches[0])
			pool[claim] = matches[1:]
		} else {
			out = append(out, reviewComment(claim))
		}
	}
	return out
}

func commentPool(comments []*ReviewComment) map[model.LlmComment][]*ReviewComment {
	pool := make(map[model.LlmComment][]*ReviewComment, len(comments))
	for _, comment := range comments {
		pool[comment.claim()] = append(pool[comment.claim()], comment)
	}
	return pool
}
