// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package llmloop

import (
	"context"
	"testing"

	"github.com/alibaba/open-code-review/internal/llm"
	v "github.com/alibaba/open-code-review/internal/verification"
)

func TestRestoredReceiptsRequireDeliveryToNewConversation(t *testing.T) {
	_, input := verifierFixture(t, 2)
	source, err := input.Core.Issue(context.Background(), "caller.go", 4, 6)
	if err != nil || source.Citation == nil {
		t.Fatalf("source=%+v error=%v", source, err)
	}
	input.Core.Submit([]v.Decision{verifierDecision(input.Candidates[0].ID, v.Refuted, *source.Citation)})
	saved := input.Core.Result()
	restored, err := v.New(input.View, "context", v.Provenance{})
	if err != nil {
		t.Fatal(err)
	}
	recheck, err := restored.Restore(context.Background(), saved.Report)
	if err != nil {
		t.Fatal(err)
	}
	input.Core, input.Candidates = restored, recheck
	script := &verifierScript{}
	script.run = func(context.Context, llm.ChatRequest, int) (*llm.ChatResponse, error) {
		submission := verifierDecision(recheck[0].ID, v.Refuted, *source.Citation)
		return verifierResponse(verifierSubmission([]v.Decision{submission}, true)), nil
	}
	result, err := verifierRunner(t, script).RunVerification(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	record := result.Result.Report.Records[0]
	if record.Verdict != v.Uncertain || record.Reason.Code != "invalid_decision" || record.Candidate != recheck[0] || len(result.Result.Published) != 1 {
		t.Fatalf("historical receipt authorizes undelivered support: %+v", result)
	}
	accepted := restored.Result().Report.Records[0]
	if accepted.Candidate != saved.Report.Records[0].Candidate || accepted.Citations[0].ID != source.Citation.ID || accepted.Provenance != saved.Report.Records[0].Provenance {
		t.Fatal("new invocation rewrites the saved completed decision")
	}
}
