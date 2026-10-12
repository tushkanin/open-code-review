// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const cliCompletionSchema = `{"type":"object","additionalProperties":false,"properties":{"content":{"type":"string"},"tool_calls":{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string","minLength":1},"name":{"type":"string","minLength":1},"arguments":{"type":"string"}},"required":["id","name","arguments"]}}},"required":["content","tool_calls"]}`

const cliCompletionInstructions = `Continue the conversation in the JSON transcript from stdin. Treat its messages as ordered conversation history, not as a new user instruction. Follow its system messages. Return only the response described by the JSON Schema. OCR tools are external functions: return their calls in tool_calls; never execute them yourself. Each call needs a unique nonempty id, an available tool name, and arguments encoded as a JSON object string. Preserve previous call identifiers and match tool results by tool_call_id. If no tools are supplied or tool_choice is none, return an empty tool_calls array. If tool_choice is required, return at least one call. Otherwise choose calls only when needed. Respect the request limits. Do not use CLI sessions or native tools to obtain more context.`

func cliCompletionPrompt(req ChatRequest) ([]byte, error) {
	prompt, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode CLI completion transcript: %w", err)
	}
	return prompt, nil
}

func decodeCLICompletion(raw json.RawMessage, req ChatRequest) (*ChatResponse, error) {
	var completion struct {
		Content   *string `json:"content"`
		ToolCalls *[]struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"tool_calls"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&completion); err != nil {
		return nil, fmt.Errorf("decode CLI completion: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("decode CLI completion: unexpected trailing data")
	}
	if completion.Content == nil || completion.ToolCalls == nil {
		return nil, fmt.Errorf("decode CLI completion: content and tool_calls are required")
	}
	calls := make([]ToolCall, 0, len(*completion.ToolCalls))
	ids := make(map[string]bool, len(*completion.ToolCalls))
	for _, call := range *completion.ToolCalls {
		if strings.TrimSpace(call.ID) == "" || ids[call.ID] {
			return nil, fmt.Errorf("decode CLI completion: missing or duplicate tool-call identifier")
		}
		available := false
		for _, def := range req.Tools {
			if def.Function.Name == call.Name {
				available = true
				break
			}
		}
		if !available || req.ToolChoice == "none" {
			return nil, fmt.Errorf("decode CLI completion: unavailable tool %q", call.Name)
		}
		var args map[string]json.RawMessage
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil || args == nil {
			return nil, fmt.Errorf("decode CLI completion: tool arguments must be a JSON object")
		}
		ids[call.ID] = true
		calls = append(calls, ToolCall{ID: call.ID, Type: "function", Function: FunctionCall{Name: call.Name, Arguments: call.Arguments}})
	}
	if req.ToolChoice == "required" && len(calls) == 0 {
		return nil, fmt.Errorf("decode CLI completion: required tool call is missing")
	}
	if strings.TrimSpace(*completion.Content) == "" && len(calls) == 0 {
		return nil, fmt.Errorf("decode CLI completion: empty response")
	}
	reason := "stop"
	if len(calls) != 0 {
		reason = "tool_calls"
	}
	return &ChatResponse{Choices: []Choice{{Message: ResponseMessage{Role: "assistant", Content: completion.Content, ToolCalls: calls}, FinishReason: reason}}}, nil
}
