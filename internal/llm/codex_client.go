// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// CodexClient uses CLI-managed authentication, not HTTP credentials.
type CodexClient struct{ cfg ClientConfig }

var _ LLMClient = (*CodexClient)(nil)

func NewCodexClient(cfg ClientConfig) *CodexClient {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Minute
	}
	return &CodexClient{cfg: cfg}
}

// These gates correspond to the native tool registry in codex-cli 0.162.1.
var codexDisabledFeatures = []string{
	"shell_tool", "shell_snapshot", "shell_snapshot_v2", "unified_exec_tty",
	"view_image", "image_generation", "browser_use", "browser_use_external", "browser_use_full_cdp_access", "browser_annotation_api",
	"computer_use", "code_mode", "code_mode_host", "code_mode_only", "code_mode_only_strict_3p_tools", "code_mode_interrupt", "code_mode_prewarm", "code_mode_tool_search",
	"multi_agent", "multi_agent_v2", "multi_agent_v2_dynamic_tools", "agent_message_board", "hooks", "guardianv2", "plugins", "remote_plugin", "apps", "enable_mcp_apps", "tool_suggest",
	"skill_search", "skill_mcp_dependency_install", "workspace_dependencies", "executor_capability_discovery", "deferred_executor", "request_permissions_tool", "token_budget", "current_time_reminder", "sleep_tool", "send_message_to_user_async", "standalone_web_search", "memories", "goals",
}

func (c *CodexClient) CompletionsWithCtx(ctx context.Context, req ChatRequest) (resp *ChatResponse, err error) {
	callerCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	defer func() { err = describeTimeout(callerCtx, err) }()
	if req.Model == "" {
		req.Model = c.cfg.Model
	}
	if req.Model == "" {
		return nil, fmt.Errorf("Codex completion requires a model")
	}
	prompt, err := cliCompletionPrompt(req)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "ocr-codex-")
	if err != nil {
		return nil, fmt.Errorf("Codex temporary directory: %w", err)
	}
	defer func() {
		if e := os.RemoveAll(dir); e != nil && err == nil {
			resp = nil
			err = fmt.Errorf("Codex cleanup: %w", e)
		}
	}()
	binary, err := exec.LookPath("codex")
	if err != nil {
		return nil, fmt.Errorf("Codex command: %w", err)
	}
	version, err := runCodexCommand(ctx, binary, dir, nil, "--version")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(version)) != "codex-cli 0.162.1" {
		return nil, fmt.Errorf("Codex incompatible version %q: supported release is 0.162.1", strings.TrimSpace(string(version)))
	}
	native, err := runCodexCommand(ctx, binary, dir, nil, "debug", "models", "--bundled")
	if err != nil {
		return nil, err
	}
	catalog, err := restrictCodexCatalog(native, req.Model)
	if err != nil {
		return nil, err
	}
	for name, data := range map[string][]byte{"catalog.json": catalog, "schema.json": []byte(cliCompletionSchema), "instructions.txt": []byte(cliCompletionInstructions)} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			return nil, fmt.Errorf("Codex request file: %w", err)
		}
	}
	controls := codexControls(req.Model, dir)
	if c.cfg.ReasoningEffort != "" {
		controls = append(controls, codexControl{"model_reasoning_effort", c.cfg.ReasoningEffort})
	}
	args := codexControlArgs(controls)
	var first *codexConfigSnapshot
	for pass := range 2 {
		features, err := runCodexCommand(ctx, binary, dir, nil, append(args, "features", "list")...)
		if err != nil {
			return nil, fmt.Errorf("Codex feature preflight: %w", err)
		}
		if err := checkCodexFeatures(features); err != nil {
			return nil, err
		}
		snapshot, err := readCodexConfig(ctx, binary, dir, args, controls, pass == 1)
		if err != nil {
			return nil, err
		}
		if pass == 0 {
			first = snapshot
			if len(snapshot.serverIDs) != 0 {
				args = append(args, "-c", codexMCPOverride(snapshot.serverIDs))
			}
		} else if !first.sameContext(snapshot) {
			return nil, fmt.Errorf("Codex configuration context changes during preflight")
		}
	}
	args = append(args, "exec", "--model", req.Model, "--json", "--output-schema", filepath.Join(dir, "schema.json"), "--ephemeral", "--ignore-rules", "--skip-git-repo-check", "--strict-config", "--sandbox", "read-only", "-")
	output, err := runCodexCommand(ctx, binary, dir, prompt, args...)
	if err != nil {
		return nil, fmt.Errorf("Codex completion: %w", err)
	}
	return parseCodexResult(output, req)
}

func runCodexCommand(ctx context.Context, binary, dir string, prompt []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir
	cmd.Stdin = bytes.NewReader(prompt)
	cmd.Stderr = io.Discard
	output := &cappedBuffer{max: 16 << 20}
	cmd.Stdout = output
	cmd.WaitDelay = time.Second
	configureCLIProcess(cmd)
	cmd.Env = codexCommandEnv(cmd, dir)
	err := cmd.Run()
	if err != nil && cmd.Process != nil {
		_ = cmd.Cancel()
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("Codex command: %w", ctx.Err())
	}
	if output.overflow {
		return nil, fmt.Errorf("Codex output exceeds 16MiB")
	}
	if err != nil {
		return nil, fmt.Errorf("Codex command failed: %w", err)
	}
	return output.buf.Bytes(), nil
}

func checkCodexFeatures(data []byte) error {
	required := make(map[string]bool, len(codexDisabledFeatures)+1)
	for _, name := range codexDisabledFeatures {
		required[name] = false
	}
	required["skip_host_skill_discovery"] = true
	seen := make(map[string]bool, len(required))
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		want, relevant := required[fields[0]]
		if !relevant {
			continue
		}
		if seen[fields[0]] || len(fields) < 3 {
			return fmt.Errorf("Codex required feature has invalid output")
		}
		got, err := strconv.ParseBool(fields[len(fields)-1])
		if err != nil || (fields[len(fields)-1] != "true" && fields[len(fields)-1] != "false") || got != want {
			return fmt.Errorf("Codex required feature %s conflicts with isolation", fields[0])
		}
		seen[fields[0]] = true
	}
	if len(seen) != len(required) {
		return fmt.Errorf("Codex required feature is unavailable")
	}
	return nil
}

func restrictCodexCatalog(data []byte, model string) ([]byte, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("Codex catalog: %w", err)
	}
	var models []map[string]json.RawMessage
	if err := json.Unmarshal(root["models"], &models); err != nil {
		return nil, fmt.Errorf("Codex model metadata: %w", err)
	}
	found := false
	restrictions := map[string]json.RawMessage{"shell_type": json.RawMessage(`"disabled"`), "apply_patch_tool_type": json.RawMessage(`null`), "experimental_supported_tools": json.RawMessage(`[]`), "tool_mode": json.RawMessage(`null`), "node_repl_disabled": json.RawMessage(`true`), "supports_search_tool": json.RawMessage(`false`), "multi_agent_version": json.RawMessage(`null`)}
	for _, entry := range models {
		var slug string
		if err := json.Unmarshal(entry["slug"], &slug); err != nil {
			return nil, fmt.Errorf("Codex catalog model identifier: %w", err)
		}
		if slug != model {
			continue
		}
		if found {
			return nil, fmt.Errorf("Codex duplicate model metadata for %q", model)
		}
		found = true
		for key, value := range restrictions {
			raw, ok := entry[key]
			if !ok || !validCodexCapability(key, raw) {
				return nil, fmt.Errorf("Codex model lacks compatible capability %s", key)
			}
			entry[key] = value
		}
	}
	if !found {
		return nil, fmt.Errorf("Codex native catalog lacks model %q", model)
	}
	raw, err := json.Marshal(models)
	if err != nil {
		return nil, fmt.Errorf("Codex model metadata: %w", err)
	}
	root["models"] = raw
	return json.Marshal(root)
}

func validCodexCapability(key string, raw json.RawMessage) bool {
	switch key {
	case "node_repl_disabled", "supports_search_tool":
		return bytes.Equal(raw, []byte("true")) || bytes.Equal(raw, []byte("false"))
	case "experimental_supported_tools":
		var tools []string
		return !bytes.Equal(raw, []byte("null")) && json.Unmarshal(raw, &tools) == nil
	default:
		if bytes.Equal(raw, []byte("null")) {
			return key != "shell_type"
		}
		var value string
		return json.Unmarshal(raw, &value) == nil && value != ""
	}
}

func parseCodexResult(data []byte, req ChatRequest) (*ChatResponse, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 16<<20)
	var threadID, final string
	started, turn, completed := false, false, false
	var usage *UsageInfo
	for scanner.Scan() {
		var event struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
			Item     *struct {
				ID      string  `json:"id"`
				Type    string  `json:"type"`
				Text    *string `json:"text"`
				Message string  `json:"message"`
			} `json:"item"`
			Usage *struct {
				Input  *int64 `json:"input_tokens"`
				Output *int64 `json:"output_tokens"`
				Cached int64  `json:"cached_input_tokens"`
				Write  int64  `json:"cache_write_input_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, fmt.Errorf("Codex event: %w", err)
		}
		if completed {
			return nil, fmt.Errorf("Codex event follows terminal event")
		}
		switch event.Type {
		case "thread.started":
			if started || event.ThreadID == "" {
				return nil, fmt.Errorf("Codex invalid thread start")
			}
			started = true
			threadID = event.ThreadID
		case "turn.started":
			if !started || turn {
				return nil, fmt.Errorf("Codex invalid turn start")
			}
			turn = true
		case "item.started", "item.updated", "item.completed":
			if event.Item != nil && event.Item.Type == "error" {
				return nil, fmt.Errorf("Codex unclassified provider condition")
			}
			if !turn || event.Item == nil || event.Item.ID == "" {
				return nil, fmt.Errorf("Codex invalid item event")
			}
			if event.Item.Type != "agent_message" && event.Item.Type != "reasoning" {
				return nil, fmt.Errorf("Codex forbidden native item %q", event.Item.Type)
			}
			if event.Item.Text == nil {
				return nil, fmt.Errorf("Codex item lacks text")
			}
			if event.Type == "item.completed" && event.Item.Type == "agent_message" {
				final = *event.Item.Text
			}
		case "turn.completed":
			if !turn || final == "" {
				return nil, fmt.Errorf("Codex missing final assistant output")
			}
			completed = true
			if event.Usage != nil {
				u := event.Usage
				if u.Input == nil || u.Output == nil || *u.Input < 0 || *u.Output < 0 || u.Cached < 0 || u.Write < 0 || *u.Input > math.MaxInt64-*u.Output {
					return nil, fmt.Errorf("Codex invalid token usage")
				}
				usage = &UsageInfo{PromptTokens: *u.Input, CompletionTokens: *u.Output, TotalTokens: *u.Input + *u.Output, CacheReadTokens: u.Cached, CacheWriteTokens: u.Write}
			}
		default:
			return nil, fmt.Errorf("Codex failed or unsupported event %q", event.Type)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("Codex event stream: %w", err)
	}
	if !completed {
		return nil, fmt.Errorf("Codex incomplete event stream")
	}
	resp, err := decodeCLICompletion(json.RawMessage(final), req)
	if err != nil {
		return nil, fmt.Errorf("Codex result: %w", err)
	}
	resp.ID = threadID
	resp.Model = req.Model
	resp.Usage = usage
	return resp, nil
}
