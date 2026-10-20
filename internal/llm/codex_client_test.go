// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCodexTokenAccountingBoundary(t *testing.T) {
	for _, input := range []string{"9223372036854775800", "9223372036854775807"} {
		t.Run(input, func(t *testing.T) {
			stream := strings.Replace(codexTestStream, `"input_tokens":20`, `"input_tokens":`+input, 1)
			tmp := installCodexTestCommand(t, "completion", "printf '%s' '"+stream+"'")
			resp, err := NewCodexClient(ClientConfig{Model: "selected-model"}).CompletionsWithCtx(t.Context(), ChatRequest{})
			if input == "9223372036854775807" {
				if err == nil || resp != nil {
					t.Fatal("overflowing usage reaches accounting")
				}
			} else if err != nil || resp.Usage.TotalTokens != 9223372036854775807 {
				t.Fatalf("largest valid total: response=%v error=%v", resp, err)
			}
			assertCodexCleanup(t, tmp)
		})
	}
}

const codexTestCatalog = `{"revision":"native","models":[{"slug":"selected-model","extra":{"future":[1,true,"native"]},"shell_type":"unified_exec","apply_patch_tool_type":"freeform","experimental_supported_tools":["clock"],"tool_mode":"code_mode_only","node_repl_disabled":false,"supports_search_tool":true,"multi_agent_version":"v2"},{"slug":"other","shell_type":"native-other"}]}`
const codexTestStream = `{"type":"thread.started","thread_id":"thread"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item","type":"agent_message","text":"{\"content\":\"ok\",\"tool_calls\":[]}"}}
{"type":"turn.completed","usage":{"input_tokens":20,"cached_input_tokens":5,"cache_write_input_tokens":3,"output_tokens":7,"reasoning_output_tokens":2}}
`

func TestCodexCatalogPreservation(t *testing.T) {
	data, err := restrictCodexCatalog([]byte(codexTestCatalog), "selected-model")
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]any
	if err := json.Unmarshal([]byte(codexTestCatalog), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &after); err != nil {
		t.Fatal(err)
	}
	a := after["models"].([]any)[0].(map[string]any)
	for key, want := range map[string]any{"shell_type": "disabled", "apply_patch_tool_type": nil, "experimental_supported_tools": []any{}, "tool_mode": nil, "node_repl_disabled": true, "supports_search_tool": false, "multi_agent_version": nil} {
		if !reflect.DeepEqual(a[key], want) {
			t.Fatalf("capability %s=%v", key, a[key])
		}
		a[key] = before["models"].([]any)[0].(map[string]any)[key]
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("native metadata differs outside approved capabilities")
	}
	for _, input := range []string{
		`{}`, `not JSON`,
		strings.Replace(codexTestCatalog, `"node_repl_disabled":false,`, "", 1),
		strings.Replace(codexTestCatalog, `"slug":"other"`, `"slug":"selected-model"`, 1),
		strings.Replace(codexTestCatalog, `"node_repl_disabled":false`, `"node_repl_disabled":"false"`, 1),
		strings.Replace(codexTestCatalog, `"experimental_supported_tools":["clock"]`, `"experimental_supported_tools":null`, 1),
	} {
		if _, err := restrictCodexCatalog([]byte(input), "selected-model"); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	if _, err := restrictCodexCatalog([]byte(codexTestCatalog), "alias"); err == nil {
		t.Fatal("missing model succeeds")
	}
}

func TestCodexEvents(t *testing.T) {
	resp, err := parseCodexResult([]byte(codexTestStream), ChatRequest{Model: "selected-model"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.ID != "thread" || resp.Model != "selected-model" || resp.Content() != "ok" || !reflect.DeepEqual(resp.Usage, &UsageInfo{PromptTokens: 20, CompletionTokens: 7, TotalTokens: 27, CacheReadTokens: 5, CacheWriteTokens: 3}) {
		t.Fatalf("response=%+v", resp)
	}
	noUsage := strings.Replace(codexTestStream, `,"usage":{"input_tokens":20,"cached_input_tokens":5,"cache_write_input_tokens":3,"output_tokens":7,"reasoning_output_tokens":2}`, "", 1)
	resp, err = parseCodexResult([]byte(noUsage), ChatRequest{})
	if err != nil || resp.Usage != nil {
		t.Fatalf("absent usage=%+v err=%v", resp, err)
	}
	for _, native := range []string{"command_execution", "file_change", "mcp_tool_call", "collab_tool_call", "web_search", "todo_list", "error", "unknown_native_tool"} {
		bad := strings.Replace(codexTestStream, `"type":"agent_message"`, `"type":"`+native+`"`, 1)
		if _, err := parseCodexResult([]byte(bad), ChatRequest{}); err == nil {
			t.Fatalf("native item %s succeeds", native)
		}
	}
	for _, bad := range []string{"", `{}`, `not JSON`, codexTestStream + `{}`, strings.Replace(codexTestStream, "turn.completed", "turn.failed", 1), strings.Replace(codexTestStream, "turn.completed", "error", 1), strings.Split(codexTestStream, `{"type":"turn.completed"`)[0], strings.Replace(codexTestStream, `\"content\":\"ok\"`, `\"content\":\"\"`, 1), strings.Replace(codexTestStream, `"input_tokens":20`, `"input_tokens":-1`, 1)} {
		if _, err := parseCodexResult([]byte(bad), ChatRequest{}); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	raw := `{"content":"","tool_calls":[{"id":"original","name":"file_read","arguments":"{}"}]}`
	encoded, _ := json.Marshal(raw)
	toolStream := strings.Replace(codexTestStream, `"{\"content\":\"ok\",\"tool_calls\":[]}"`, string(encoded), 1)
	resp, err = parseCodexResult([]byte(toolStream), cliTestRequest())
	if err != nil || len(resp.ToolCalls()) != 1 || resp.ToolCalls()[0].ID != "original" {
		t.Fatalf("tool response=%+v err=%v", resp, err)
	}
}

func TestCodexResidualConditions(t *testing.T) {
	for _, message := range []string{
		"Under-development features enabled: skip_host_skill_discovery",
		"Unknown configuration field",
		"model rerouted: selected-model -> other",
		"unclassified startup condition",
	} {
		raw, err := json.Marshal(map[string]any{
			"type": "item.completed",
			"item": map[string]any{"id": "notice", "type": "error", "message": message},
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, stream := range []string{
			string(raw) + "\n" + codexTestStream,
			strings.Replace(codexTestStream, `{"type":"turn.completed"`, string(raw)+"\n"+`{"type":"turn.completed"`, 1),
		} {
			if resp, err := parseCodexResult([]byte(stream), ChatRequest{}); err == nil || resp != nil {
				t.Fatalf("unclassified condition succeeds: %s", stream)
			}
		}
	}
	for _, kind := range []string{"command_execution", "file_change", "mcp_tool_call", "collab_tool_call", "web_search", "todo_list"} {
		raw := `{"type":"item.completed","item":{"id":"native","type":"` + kind + `"}}`
		stream := strings.Replace(codexTestStream, `{"type":"turn.completed"`, raw+"\n"+`{"type":"turn.completed"`, 1)
		if resp, err := parseCodexResult([]byte(stream), ChatRequest{}); err == nil || resp != nil {
			t.Fatalf("native event after valid output succeeds: %s", kind)
		}
	}
}

func installCodexTestCommand(t *testing.T, stage, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixture commands require a POSIX shell")
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("OCR_CODEX_TEST_HELPER", "1")
	t.Setenv("OCR_CODEX_TEST_BINARY", binary)
	t.Setenv("OCR_CODEX_TEST_STAGE", stage)
	t.Setenv("OCR_CODEX_TEST_SCRIPT", script)
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	body := "#!/bin/sh\n# SPDX-License-Identifier: Apache-2.0\nexec \"$OCR_CODEX_TEST_BINARY\" -test.run=^TestCodexProcessHelper$ -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	return tmp
}

func TestCodexProcessHelper(t *testing.T) {
	if os.Getenv("OCR_CODEX_TEST_HELPER") != "1" {
		t.Skip("subprocess fixture")
	}
	args := os.Args
	for i, arg := range args {
		if arg == "--" {
			args = args[i+1:]
			break
		}
	}
	cfg := map[string]any{}
	origins := map[string]any{}
	servers := map[string]any{}
	for _, id := range []string{"fixture.server", `quote"slash\equals=fixture`, "other", "comma,brace}server"} {
		servers[id] = map[string]any{"command": "unused", "args": []any{"transport", id}, "env": map[string]any{"PRIVATE": "sensitive-sentinel"}}
	}
	var mcpOverride map[string]bool
	set := func(key string, value any) {
		setCodexTestField(cfg, key, value)
		origins[key] = map[string]any{"name": map[string]any{"type": "sessionFlags"}, "version": "fixture"}
	}
	stage := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-c":
			i++
			key, raw, _ := strings.Cut(args[i], "=")
			if key == "mcp_servers" {
				var err error
				mcpOverride, err = parseCodexTestMCPOverride(raw)
				if err != nil {
					os.Exit(12)
				}
				continue
			}
			if strings.HasPrefix(key, "mcp_servers.") {
				os.Exit(12)
			}
			var value any
			if json.Unmarshal([]byte(raw), &value) != nil {
				os.Exit(13)
			}
			set(key, value)
		case "debug":
			stage = "catalog"
		case "features":
			stage = "features"
		case "app-server":
			stage = "preflight"
		case "exec":
			stage = "completion"
		}
	}
	for id, enabled := range mcpOverride {
		server, ok := servers[id].(map[string]any)
		if !ok {
			os.Exit(17)
		}
		server["enabled"] = enabled
	}
	if stage == os.Getenv("OCR_CODEX_TEST_STAGE") && os.Getenv("OCR_CODEX_TEST_SCRIPT") != "" {
		cmd := exec.Command("/bin/sh", "-c", os.Getenv("OCR_CODEX_TEST_SCRIPT"))
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if cmd.Run() != nil {
			os.Exit(8)
		}
		os.Exit(0)
	}
	switch stage {
	case "catalog":
		fmt.Print(codexTestCatalog)
	case "features":
		for _, name := range codexDisabledFeatures {
			fmt.Println(name, "stable", "false")
		}
		fmt.Println("skip_host_skill_discovery under development true")
		fmt.Println("unified_exec stable true")
	case "completion":
		for id, value := range servers {
			server := value.(map[string]any)
			if server["enabled"] != false || server["command"] != "unused" ||
				!reflect.DeepEqual(server["args"], []any{"transport", id}) ||
				!reflect.DeepEqual(server["env"], map[string]any{"PRIVATE": "sensitive-sentinel"}) {
				os.Exit(18)
			}
		}
		if marker := os.Getenv("OCR_CODEX_COMPLETION_MARKER"); marker != "" {
			os.WriteFile(marker, []byte("started"), 0600)
		}
		ioCopyCodexTestInput()
		fmt.Print(codexTestStream)
	case "preflight":
		if os.Getenv("CODEX_INTERNAL_APP_SERVER_REMOTE_CONTROL_DISABLED") != "1" {
			os.Exit(14)
		}
		cfg["mcp_servers"] = servers
		cfg["tools"] = map[string]any{"web_search": nil}
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			var call struct {
				ID     *int   `json:"id"`
				Method string `json:"method"`
			}
			if json.Unmarshal(scanner.Bytes(), &call) != nil {
				os.Exit(15)
			}
			var result any
			switch call.Method {
			case "initialize":
				result = map[string]any{"userAgent": "fixture"}
			case "initialized":
				continue
			case "config/read":
				if os.Getenv("OCR_CODEX_CONFIG_CONFLICT") == "1" {
					cfg["notify"] = []any{"sensitive-sentinel"}
				}
				result = map[string]any{"config": cfg, "origins": origins}
			case "configRequirements/read":
				result = map[string]any{"requirements": nil}
			default:
				os.Exit(16)
			}
			json.NewEncoder(os.Stdout).Encode(map[string]any{"id": call.ID, "result": result})
		}
	}
	os.Exit(0)
}

func parseCodexTestMCPOverride(raw string) (map[string]bool, error) {
	if !strings.HasPrefix(raw, "{") || !strings.HasSuffix(raw, "}") {
		return nil, errors.New("unsupported fixture MCP value")
	}
	remaining := raw[1 : len(raw)-1]
	servers := map[string]bool{}
	for remaining != "" {
		decoder := json.NewDecoder(strings.NewReader(remaining))
		var id string
		if err := decoder.Decode(&id); err != nil {
			return nil, errors.New("unsupported fixture MCP key")
		}
		remaining = remaining[decoder.InputOffset():]
		if _, exists := servers[id]; exists || !strings.HasPrefix(remaining, "={enabled=false}") {
			return nil, errors.New("unsupported fixture MCP entry")
		}
		servers[id] = false
		remaining = strings.TrimPrefix(remaining, "={enabled=false}")
		if remaining == "" {
			break
		}
		if !strings.HasPrefix(remaining, ",") || len(remaining) == 1 {
			return nil, errors.New("unsupported fixture MCP separator")
		}
		remaining = remaining[1:]
	}
	return servers, nil
}

func setCodexTestField(root map[string]any, key string, value any) {
	parts := strings.Split(key, ".")
	for _, part := range parts[:len(parts)-1] {
		next, ok := root[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			root[part] = next
		}
		root = next
	}
	root[parts[len(parts)-1]] = value
}

func ioCopyCodexTestInput() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
	}
}

func TestCodexPreflightBlocksCompletion(t *testing.T) {
	for _, conflict := range []string{"feature", "configuration", "protocol"} {
		t.Run(conflict, func(t *testing.T) {
			stage, script := "", ""
			switch conflict {
			case "feature":
				stage, script = "features", "printf 'view_image stable true\\n'"
			case "configuration":
				t.Setenv("OCR_CODEX_CONFIG_CONFLICT", "1")
			case "protocol":
				stage, script = "preflight", "printf '{\"id\":0,\"error\":{\"message\":\"sensitive-sentinel\"}}\\n'"
			}
			tmp := installCodexTestCommand(t, stage, script)
			marker := filepath.Join(t.TempDir(), "completion")
			t.Setenv("OCR_CODEX_COMPLETION_MARKER", marker)
			resp, err := NewCodexClient(ClientConfig{Model: "selected-model"}).CompletionsWithCtx(context.Background(), ChatRequest{})
			if err == nil || resp != nil || strings.Contains(err.Error(), "sensitive-sentinel") {
				t.Fatalf("unsafe result: response=%v error=%v", resp, err)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("completion starts despite failed preflight")
			}
			assertCodexCleanup(t, tmp)
		})
	}
}

func TestCodexDisabledServersAllowCompletion(t *testing.T) {
	tmp := installCodexTestCommand(t, "", "")
	resp, err := NewCodexClient(ClientConfig{Model: "selected-model"}).CompletionsWithCtx(context.Background(), ChatRequest{})
	if err != nil || resp.Content() != "ok" || resp.Model != "selected-model" {
		t.Fatalf("response=%v error=%v", resp, err)
	}
	assertCodexCleanup(t, tmp)
}

func assertCodexCleanup(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary entries=%v error=%v", entries, err)
	}
}

func TestCodexCompatibilityAndProcessFailures(t *testing.T) {
	for _, scenario := range []struct{ stage, script string }{
		{"features", "printf 'shell_tool stable false'"}, {"catalog", "printf '{}'"}, {"catalog", "exit 8"}, {"completion", "exit 9"}, {"completion", "printf 'not JSON'"}, {"completion", "sleep 5 &\nprintf '%s' '" + codexTestStream + "'"},
	} {
		t.Run(scenario.stage+scenario.script, func(t *testing.T) {
			tmp := installCodexTestCommand(t, scenario.stage, scenario.script)
			start := time.Now()
			resp, err := NewCodexClient(ClientConfig{Model: "selected-model"}).CompletionsWithCtx(context.Background(), ChatRequest{})
			if err == nil || resp != nil {
				t.Fatal("failure succeeds")
			}
			if time.Since(start) > 3*time.Second {
				t.Fatal("pipe wait is not bounded")
			}
			assertCodexCleanup(t, tmp)
		})
	}
	t.Run("missing", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if _, err := NewCodexClient(ClientConfig{Model: "model"}).CompletionsWithCtx(context.Background(), ChatRequest{}); err == nil {
			t.Fatal("missing command succeeds")
		}
	})
	if _, err := NewCodexClient(ClientConfig{}).CompletionsWithCtx(context.Background(), ChatRequest{}); err == nil {
		t.Fatal("missing model succeeds")
	}
}

func TestCodexCancellationCleanup(t *testing.T) {
	for _, stage := range []string{"catalog", "features", "preflight", "completion"} {
		for _, mode := range []string{"cancel", "caller_deadline", "request_timeout"} {
			t.Run(stage+"/"+mode, func(t *testing.T) {
				tmp := installCodexTestCommand(t, stage, "touch scratch\nsleep 20 &\nwait\n")
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				cfg := ClientConfig{Model: "selected-model", Timeout: 10 * time.Second}
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
				resp, err := NewCodexClient(cfg).CompletionsWithCtx(ctx, ChatRequest{})
				if !errors.Is(err, want) || resp != nil {
					t.Fatalf("response=%+v error=%v", resp, err)
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
				assertCodexCleanup(t, tmp)
			})
		}
	}
}
