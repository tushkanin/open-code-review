// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package llm_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alibaba/open-code-review/internal/llm"
	"github.com/alibaba/open-code-review/internal/tool"
)

func TestClaudeCodeRealSmoke(t *testing.T) {
	if os.Getenv("OCR_CLAUDE_SMOKE") != "1" {
		t.Skip("set OCR_CLAUDE_SMOKE=1 to use the installed Claude CLI and its authentication")
	}
	binary, err := exec.LookPath("claude")
	if err != nil {
		t.Fatal(err)
	}
	model := os.Getenv("OCR_CLAUDE_SMOKE_MODEL")
	if model == "" {
		model = "sonnet"
	}
	t.Logf("real executable=%s selected model=%s; no OCR API key", binary, model)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	client := llm.NewClaudeCodeClient(llm.ClientConfig{Model: model, Timeout: 2 * time.Minute})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	check := func(label string, resp *llm.ChatResponse, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if resp.Model != model {
			t.Fatalf("%s model=%q", label, resp.Model)
		}
		if resp.Usage == nil || resp.Usage.PromptTokens <= 0 || resp.Usage.CompletionTokens <= 0 || resp.Usage.TotalTokens != resp.Usage.PromptTokens+resp.Usage.CompletionTokens {
			t.Fatalf("%s usage=%+v", label, resp.Usage)
		}
		t.Logf("%s: content=%q calls=%+v usage=%+v", label, resp.Content(), resp.ToolCalls(), resp.Usage)
		entries, err := os.ReadDir(tmp)
		if err != nil || len(entries) != 0 {
			t.Fatalf("temporary cleanup: entries=%v error=%v", entries, err)
		}
	}
	textReq := llm.ChatRequest{Messages: []llm.Message{{Role: "system", Content: "Return the requested text exactly."}, {Role: "user", Content: "Return OCR_TEXT_OK."}}}
	text, err := client.CompletionsWithCtx(ctx, textReq)
	check("text-only", text, err)
	if text.Content() != "OCR_TEXT_OK" || len(text.ToolCalls()) != 0 {
		t.Fatal("text-only exercise differs")
	}

	repo := t.TempDir()
	secretBytes := make([]byte, 16)
	if _, err := rand.Read(secretBytes); err != nil {
		t.Fatal(err)
	}
	secret := hex.EncodeToString(secretBytes)
	if err := os.WriteFile(filepath.Join(repo, "fixture.txt"), []byte(secret+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	registry := tool.NewRegistry()
	registry.Register(tool.NewFileRead(&tool.FileReader{RepoDir: repo, Mode: tool.ModeWorkspace}))
	registry.Freeze()
	req := llm.ChatRequest{ToolChoice: "required", Tools: []llm.ToolDef{{Type: "function", Function: llm.FunctionDef{
		Name: "file_read", Description: "Read a file through OCR. OCR executes this function and supplies the result.",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"file_path": map[string]any{"type": "string"}}, "required": []string{"file_path"}},
	}}}, Messages: []llm.Message{
		{Role: "system", Content: "Use only the supplied OCR functions. Do not read files with native tools. Request file_read for fixture.txt, then return the exact value from its result."},
		{Role: "user", Content: "Read fixture.txt through OCR and return its exact value."},
	}}
	first, err := client.CompletionsWithCtx(ctx, req)
	check("tool request", first, err)
	calls := first.ToolCalls()
	if len(calls) != 1 || calls[0].ID == "" || calls[0].Function.Name != "file_read" {
		t.Fatalf("wrong calls: %+v", calls)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatal(err)
	}
	if args["file_path"] != "fixture.txt" {
		t.Fatalf("wrong arguments: %+v", args)
	}
	provider, ok := registry.Get(calls[0].Function.Name)
	if !ok {
		t.Fatal("OCR registry does not contain the requested tool")
	}
	result, err := provider.Execute(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, secret) {
		t.Fatal("OCR tool result lacks the fixture value")
	}
	req.Messages = append(req.Messages,
		llm.Message{Role: "assistant", Content: first.VisibleContent(), ToolCalls: calls},
		llm.Message{Role: "tool", ToolCallID: calls[0].ID, Content: result})
	req.ToolChoice = "none"
	second, err := client.CompletionsWithCtx(ctx, req)
	check("tool result continuation", second, err)
	if second.Content() != secret || len(second.ToolCalls()) != 0 {
		t.Fatal("continuation does not use the OCR result")
	}
	t.Logf("OCR executes file_read once; preserved call identifier=%q; the continuation returns the random fixture value", calls[0].ID)
}
