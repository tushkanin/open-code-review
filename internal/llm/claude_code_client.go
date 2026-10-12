// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// ClaudeCodeClient uses CLI-managed authentication, not HTTP credentials.
type ClaudeCodeClient struct {
	cfg ClientConfig
}

var _ LLMClient = (*ClaudeCodeClient)(nil)

func NewClaudeCodeClient(cfg ClientConfig) *ClaudeCodeClient {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Minute
	}
	return &ClaudeCodeClient{cfg: cfg}
}

func (c *ClaudeCodeClient) CompletionsWithCtx(ctx context.Context, req ChatRequest) (resp *ChatResponse, err error) {
	callerCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	defer func() { err = describeTimeout(callerCtx, err) }()
	if req.Model == "" {
		req.Model = c.cfg.Model
	}
	if req.Model == "" {
		return nil, fmt.Errorf("Claude Code completion requires a model")
	}
	prompt, err := cliCompletionPrompt(req)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "ocr-claude-")
	if err != nil {
		return nil, fmt.Errorf("Claude Code temporary directory: %w", err)
	}
	defer func() {
		if cleanupErr := os.RemoveAll(dir); cleanupErr != nil && err == nil {
			resp = nil
			err = fmt.Errorf("Claude Code cleanup: %w", cleanupErr)
		}
	}()
	cmd := exec.CommandContext(ctx, "claude", "-p", "--model", req.Model,
		"--output-format", "json", "--json-schema", cliCompletionSchema,
		"--tools", "", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`,
		"--safe-mode", "--permission-mode", "dontAsk", "--permission-prompts", "none",
		"--no-session-persistence", "--disable-slash-commands", "--no-chrome",
		"--system-prompt", cliCompletionInstructions)
	cmd.Dir = dir
	cmd.Stdin = bytes.NewReader(prompt)
	cmd.Stderr = io.Discard
	output := &cappedBuffer{max: 16 << 20}
	cmd.Stdout = output
	cmd.WaitDelay = time.Second
	configureCLIProcess(cmd)
	// Claude's own scratch files belong to the request cleanup boundary.
	cmd.Env = append(cmd.Environ(), "TMPDIR="+dir, "TMP="+dir, "TEMP="+dir)
	if req.MaxTokens > 0 {
		cmd.Env = append(cmd.Env, "CLAUDE_CODE_MAX_OUTPUT_TOKENS="+strconv.Itoa(req.MaxTokens))
	}
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("Claude Code completion: %w", ctx.Err())
	}
	if output.overflow {
		return nil, fmt.Errorf("Claude Code output exceeds 16MiB")
	}
	if runErr != nil {
		return nil, fmt.Errorf("Claude Code command failed: %w", runErr)
	}
	resp, err = parseClaudeCodeResult(output.buf.Bytes(), req)
	return resp, err
}

func parseClaudeCodeResult(data []byte, req ChatRequest) (*ChatResponse, error) {
	var result struct {
		Type             string          `json:"type"`
		Subtype          string          `json:"subtype"`
		IsError          bool            `json:"is_error"`
		SessionID        string          `json:"session_id"`
		StructuredOutput json.RawMessage `json:"structured_output"`
		Usage            *struct {
			InputTokens      int64 `json:"input_tokens"`
			OutputTokens     int64 `json:"output_tokens"`
			CacheReadTokens  int64 `json:"cache_read_input_tokens"`
			CacheWriteTokens int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode Claude Code result: %w", err)
	}
	if result.Type != "result" || result.Subtype != "success" || result.IsError {
		return nil, fmt.Errorf("Claude Code completion failed (type %q, subtype %q, is_error %t)", result.Type, result.Subtype, result.IsError)
	}
	resp, err := decodeCLICompletion(result.StructuredOutput, req)
	if err != nil {
		return nil, fmt.Errorf("Claude Code result: %w", err)
	}
	resp.ID = result.SessionID
	resp.Model = req.Model
	if result.Usage != nil {
		u := result.Usage
		resp.Usage = &UsageInfo{
			PromptTokens:     u.InputTokens + u.CacheReadTokens + u.CacheWriteTokens,
			CompletionTokens: u.OutputTokens,
			CacheReadTokens:  u.CacheReadTokens,
			CacheWriteTokens: u.CacheWriteTokens,
			TotalTokens:      u.InputTokens + u.CacheReadTokens + u.CacheWriteTokens + u.OutputTokens,
		}
	}
	return resp, nil
}
