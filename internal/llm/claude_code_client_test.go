// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package llm

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func cliTestRequest() ChatRequest {
	return ChatRequest{Model: "selected-model", Tools: []ToolDef{{Type: "function", Function: FunctionDef{Name: "file_read", Parameters: map[string]any{"type": "object"}}}}}
}

func TestCLICompletionTranscript(t *testing.T) {
	req := cliTestRequest()
	req.ToolChoice = "auto"
	req.MaxTokens = 1234
	temp := 0.2
	req.Temperature = &temp
	req.Messages = []Message{
		{Role: "system", Content: "system rule"},
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "read the file"}}},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "call-original", Type: "function", Function: FunctionCall{Name: "file_read", Arguments: `{"file_path":"fixture.txt"}`}}}},
		{Role: "tool", ToolCallID: "call-original", Content: "actual result"},
	}
	data, err := cliCompletionPrompt(req)
	if err != nil {
		t.Fatal(err)
	}
	var got ChatRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	var expected, actual any
	if err := json.Unmarshal(data, &expected); err != nil {
		t.Fatal(err)
	}
	roundTrip, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(roundTrip, &actual); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("transcript differs: %s", roundTrip)
	}
	if got.Messages[2].ToolCalls[0].ID != got.Messages[3].ToolCallID {
		t.Fatal("tool relationship differs")
	}
	req.Messages[0].Content = make(chan int)
	if _, err := cliCompletionPrompt(req); err == nil {
		t.Fatal("unsupported content succeeds")
	}
}

func TestClaudeCodeResult(t *testing.T) {
	req := cliTestRequest()
	data := []byte(`{"type":"result","subtype":"success","is_error":false,"session_id":"session","structured_output":{"content":"read","tool_calls":[{"id":"call_1","name":"file_read","arguments":"{\"file_path\":\"fixture.txt\"}"}]},"usage":{"input_tokens":12,"output_tokens":7,"cache_read_input_tokens":8,"cache_creation_input_tokens":3}}`)
	resp, err := parseClaudeCodeResult(data, req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content() != "read" || resp.ID != "session" || resp.Model != req.Model {
		t.Fatalf("response: %+v", resp)
	}
	want := &UsageInfo{PromptTokens: 23, CompletionTokens: 7, TotalTokens: 30, CacheReadTokens: 8, CacheWriteTokens: 3}
	if !reflect.DeepEqual(resp.Usage, want) {
		t.Fatalf("usage: %+v", resp.Usage)
	}
	call := resp.ToolCalls()[0]
	if call.ID != "call_1" || call.Type != "function" || call.Function.Name != "file_read" || call.Function.Arguments != `{"file_path":"fixture.txt"}` {
		t.Fatalf("call: %+v", call)
	}
	if resp.Choices[0].FinishReason != "tool_calls" {
		t.Fatal("wrong finish reason")
	}
}

func TestClaudeCodeMalformedResult(t *testing.T) {
	for _, data := range []string{
		`not json`, `{}`, `{"type":"result","subtype":"error_max_turns","is_error":false}`,
		`{"type":"result","subtype":"success","is_error":true}`,
		`{"type":"result","subtype":"success"}`,
		`{"type":"result","subtype":"success","structured_output":null}`,
		`{"type":"result","subtype":"success","structured_output":{"content":"ok"}}`,
	} {
		if resp, err := parseClaudeCodeResult([]byte(data), cliTestRequest()); err == nil || resp != nil {
			t.Fatalf("accepted %s", data)
		}
	}
	for _, raw := range []string{
		`{}`, `{"content":null,"tool_calls":[]}`, `{"content":"ok","tool_calls":null}`,
		`{"content":"","tool_calls":[]}`, `{"content":"ok","tool_calls":[],"extra":true}`,
		`{"content":"ok","tool_calls":[]} {}`,
		`{"content":"","tool_calls":[{"id":"","name":"file_read","arguments":"{}"}]}`,
		`{"content":"","tool_calls":[{"id":"1","name":"Bash","arguments":"{}"}]}`,
		`{"content":"","tool_calls":[{"id":"1","name":"file_read","arguments":"null"}]}`,
		`{"content":"","tool_calls":[{"id":"1","name":"file_read","arguments":"[]"}]}`,
		`{"content":"","tool_calls":[{"id":"1","name":"file_read","arguments":"broken"}]}`,
		`{"content":"","tool_calls":[{"id":"1","name":"file_read","arguments":"{}"},{"id":"1","name":"file_read","arguments":"{}"}]}`,
	} {
		if resp, err := decodeCLICompletion(json.RawMessage(raw), cliTestRequest()); err == nil || resp != nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	for _, choice := range []string{"none", "required"} {
		req := cliTestRequest()
		req.ToolChoice = choice
		raw := `{"content":"ok","tool_calls":[]}`
		if choice == "none" {
			raw = `{"content":"","tool_calls":[{"id":"1","name":"file_read","arguments":"{}"}]}`
		}
		if _, err := decodeCLICompletion(json.RawMessage(raw), req); err == nil {
			t.Fatalf("ignored %s", choice)
		}
	}
	resp, err := parseClaudeCodeResult([]byte(`{"type":"result","subtype":"success","structured_output":{"content":"ok","tool_calls":[]}}`), ChatRequest{})
	if err != nil || resp.Usage != nil || resp.Choices[0].FinishReason != "stop" {
		t.Fatalf("text result: %+v %v", resp, err)
	}
}

func installClaudeTestCommand(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell subprocess fixture requires Unix")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	return tmp
}

func assertClaudeCleanup(t *testing.T, tmp string) {
	t.Helper()
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("retained temporary entries: %v", entries)
	}
}

func TestClaudeCodeInvocation(t *testing.T) {
	capture := t.TempDir()
	t.Setenv("CAPTURE", capture)
	t.Setenv("CLAUDE_CONFIG_DIR", "existing-auth-root")
	tmp := installClaudeTestCommand(t, `printf '%s\n' "$@" > "$CAPTURE/args"
printf '%s' "$CLAUDE_CONFIG_DIR" > "$CAPTURE/auth"
printf '%s' "$CLAUDE_CODE_MAX_OUTPUT_TOKENS" > "$CAPTURE/limit"
pwd > "$CAPTURE/cwd"
printf '%s\n%s\n%s\n' "$TMPDIR" "$TMP" "$TEMP" > "$CAPTURE/temp"
mkdir "$TMPDIR/claude-scratch"
printf 'scratch' > "$TMPDIR/claude-scratch/data"
cat > "$CAPTURE/input"
printf '%s' '{"type":"result","subtype":"success","structured_output":{"content":"ok","tool_calls":[]}}'
`)
	client := NewClaudeCodeClient(ClientConfig{Model: "fallback", APIKey: "must-not-be-used"})
	req := ChatRequest{Model: "chosen", Messages: []Message{{Role: "user", Content: "message"}}, MaxTokens: 2048}
	resp, err := client.CompletionsWithCtx(context.Background(), req)
	if err != nil || resp.Content() != "ok" {
		t.Fatalf("completion: %v %v", resp, err)
	}
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(capture, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	args := read("args")
	for _, fragment := range []string{"-p\n", "--model\nchosen\n", "--tools\n\n", "--safe-mode\n", "--strict-mcp-config\n", "--mcp-config\n{\"mcpServers\":{}}\n", "--permission-prompts\nnone\n", "--permission-mode\ndontAsk\n", "--no-session-persistence\n", "--json-schema\n" + cliCompletionSchema + "\n", "--system-prompt\n" + cliCompletionInstructions + "\n"} {
		if !strings.Contains(args, fragment) {
			t.Errorf("missing invocation fragment %q", fragment)
		}
	}
	if read("auth") != "existing-auth-root" || read("limit") != "2048" {
		t.Fatal("auth or output limit differs")
	}
	if !strings.HasPrefix(strings.TrimSpace(read("cwd")), tmp+string(os.PathSeparator)) {
		t.Fatal("child does not use the temporary directory")
	}
	if read("temp") != strings.Repeat(read("cwd"), 3) {
		t.Fatal("child scratch directories do not use the request directory")
	}
	expected, _ := cliCompletionPrompt(req)
	if read("input") != string(expected) {
		t.Fatal("stdin differs")
	}
	assertClaudeCleanup(t, tmp)
}

func TestClaudeCodeProcessFailures(t *testing.T) {
	for _, scenario := range []struct{ name, script string }{
		{"exit", "exit 9\n"},
		{"result", `printf '%s' '{"type":"result","subtype":"error_during_execution","is_error":true}'`},
		{"malformed", "printf 'not-json'\n"},
		{"pipe", "sleep 5 &\nprintf '%s' '{\"type\":\"result\",\"subtype\":\"success\",\"structured_output\":{\"content\":\"ok\",\"tool_calls\":[]}}'\n"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			tmp := installClaudeTestCommand(t, scenario.script)
			start := time.Now()
			resp, err := NewClaudeCodeClient(ClientConfig{Model: "model"}).CompletionsWithCtx(context.Background(), ChatRequest{})
			if err == nil || resp != nil {
				t.Fatalf("failure succeeds: %v %v", resp, err)
			}
			if time.Since(start) > 3*time.Second {
				t.Fatal("pipe wait is not bounded")
			}
			assertClaudeCleanup(t, tmp)
		})
	}
	t.Run("missing", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		resp, err := NewClaudeCodeClient(ClientConfig{Model: "model"}).CompletionsWithCtx(context.Background(), ChatRequest{})
		if err == nil || resp != nil {
			t.Fatal("missing command succeeds")
		}
	})
	if _, err := NewClaudeCodeClient(ClientConfig{}).CompletionsWithCtx(context.Background(), ChatRequest{}); err == nil {
		t.Fatal("missing model succeeds")
	}
}

func TestClaudeCodeCancellationCleanup(t *testing.T) {
	for _, mode := range []string{"cancel", "caller_deadline", "request_timeout"} {
		t.Run(mode, func(t *testing.T) {
			tmp := installClaudeTestCommand(t, "sleep 20 &\nwait\n")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cfg := ClientConfig{Model: "model", Timeout: 10 * time.Second}
			want := context.Canceled
			if mode == "request_timeout" {
				cfg.Timeout = 100 * time.Millisecond
				want = context.DeadlineExceeded
			}
			if mode == "caller_deadline" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 100*time.Millisecond)
				defer stop()
				want = context.DeadlineExceeded
			}
			if mode == "cancel" {
				timer := time.AfterFunc(100*time.Millisecond, cancel)
				defer timer.Stop()
			}
			start := time.Now()
			resp, err := NewClaudeCodeClient(cfg).CompletionsWithCtx(ctx, ChatRequest{})
			if !errors.Is(err, want) || resp != nil {
				t.Fatalf("completion: %v %v", resp, err)
			}
			if mode == "request_timeout" && !errors.Is(err, ErrRequestTimeout) {
				t.Fatalf("missing request timeout: %v", err)
			}
			if mode == "caller_deadline" && errors.Is(err, ErrRequestTimeout) {
				t.Fatalf("caller deadline becomes request timeout: %v", err)
			}
			if time.Since(start) > 3*time.Second {
				t.Fatal("cancellation is not bounded")
			}
			assertClaudeCleanup(t, tmp)
		})
	}
}
