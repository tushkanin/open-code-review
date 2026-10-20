// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package llm

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

const codexOutputLimit = 16 << 20

type codexControl struct {
	key   string
	value any
}

func codexControls(model, dir string) []codexControl {
	instructions := filepath.Join(dir, "instructions.txt")
	return []codexControl{
		{"approval_policy", "never"}, {"approvals_reviewer", "user"}, {"sandbox_mode", "read-only"}, {"web_search", "disabled"},
		{"notify", []string{}}, {"orchestrator.mcp.enabled", false}, {"cloud.skills.enabled", false},
		{"tools.update_plan.enabled", false}, {"tools.experimental_request_user_input.enabled", false},
		{"project_doc_max_bytes", 0}, {"skills.include_instructions", false}, {"agents.enabled", false},
		{"instructions", ""}, {"developer_instructions", ""}, {"compact_prompt", cliCompletionInstructions},
		{"experimental_compact_prompt_file", instructions}, {"include_permissions_instructions", false},
		{"include_apps_instructions", false}, {"include_collaboration_mode_instructions", false}, {"include_environment_context", false},
		{"suppress_unstable_features_warning", true}, {"model", model},
		{"model_catalog_json", filepath.Join(dir, "catalog.json")}, {"model_instructions_file", instructions},
	}
}

// TOML basic strings permit JSON escapes except the short slash and control escapes.
func codexTOMLString(value string) string {
	var out strings.Builder
	out.Grow(len(value) + 2)
	writeCodexTOMLString(&out, value)
	return out.String()
}

func writeCodexTOMLString(out *strings.Builder, value string) {
	out.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"', '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(out, `\u%04X`, r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
}

func codexMCPOverride(ids []string) string {
	var out strings.Builder
	out.WriteString("mcp_servers={")
	for i, id := range ids {
		if i != 0 {
			out.WriteByte(',')
		}
		writeCodexTOMLString(&out, id)
		out.WriteString("={enabled=false}")
	}
	out.WriteByte('}')
	return out.String()
}

func codexControlArgs(controls []codexControl) []string {
	args := make([]string, 0, 2*(len(controls)+len(codexDisabledFeatures)+1))
	for _, control := range controls {
		var value string
		switch v := control.value.(type) {
		case string:
			value = codexTOMLString(v)
		case bool:
			value = strconv.FormatBool(v)
		case int:
			value = strconv.Itoa(v)
		case []string:
			value = "[]"
		}
		args = append(args, "-c", control.key+"="+value)
	}
	for _, name := range codexDisabledFeatures {
		args = append(args, "--disable", name)
	}
	return append(args, "--enable", "skip_host_skill_discovery")
}

func codexCommandEnv(cmd *exec.Cmd, dir string) []string {
	return append(cmd.Environ(), "TMPDIR="+dir, "TMP="+dir, "TEMP="+dir, "CODEX_INTERNAL_APP_SERVER_REMOTE_CONTROL_DISABLED=1")
}

type codexConfigSnapshot struct {
	serverIDs []string
	context   [sha256.Size]byte
}

func (s *codexConfigSnapshot) sameContext(other *codexConfigSnapshot) bool {
	return reflect.DeepEqual(s.serverIDs, other.serverIDs) && s.context == other.context
}

func readCodexConfig(ctx context.Context, binary, dir string, args []string, controls []codexControl, final bool) (*codexConfigSnapshot, error) {
	cmd := exec.CommandContext(ctx, binary, append(args, "app-server", "--listen", "stdio://", "--strict-config")...)
	cmd.Dir = dir
	cmd.Env = codexCommandEnv(cmd, dir)
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	configureCLIProcess(cmd)
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("Codex preflight input unavailable")
	}
	reader, writer := io.Pipe()
	cmd.Stdout = writer
	if err := cmd.Start(); err != nil {
		input.Close()
		reader.Close()
		writer.Close()
		return nil, fmt.Errorf("Codex preflight command failed")
	}
	done := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		writer.CloseWithError(err)
		done <- err
	}()
	stop := context.AfterFunc(ctx, func() { reader.Close() })
	defer func() {
		stop()
		input.Close()
		reader.Close()
		writer.Close()
		cmd.Cancel()
		<-done
	}()
	encoder := json.NewEncoder(input)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), codexOutputLimit)
	total := 0
	call := func(id int, method string, params any) (json.RawMessage, error) {
		if err := encoder.Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
			return nil, fmt.Errorf("Codex preflight request failed")
		}
		for scanner.Scan() {
			total += len(scanner.Bytes()) + 1
			if total > codexOutputLimit {
				return nil, fmt.Errorf("Codex preflight output exceeds limit")
			}
			var frame struct {
				ID     *int            `json:"id"`
				Method string          `json:"method"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if json.Unmarshal(scanner.Bytes(), &frame) != nil {
				return nil, fmt.Errorf("Codex preflight response is malformed")
			}
			if frame.ID == nil && frame.Method != "" {
				continue
			}
			if frame.ID == nil || *frame.ID != id || len(frame.Error) != 0 || len(frame.Result) == 0 || string(frame.Result) == "null" {
				return nil, fmt.Errorf("Codex preflight response failed")
			}
			return frame.Result, nil
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("Codex preflight: %w", ctx.Err())
		}
		return nil, fmt.Errorf("Codex preflight response is incomplete")
	}
	if _, err := call(0, "initialize", map[string]any{
		"clientInfo":   map[string]string{"name": "ocr", "version": "1"},
		"capabilities": map[string]bool{"experimentalApi": true},
	}); err != nil {
		return nil, err
	}
	if err := encoder.Encode(map[string]string{"method": "initialized"}); err != nil {
		return nil, fmt.Errorf("Codex preflight initialization failed")
	}
	config, err := call(1, "config/read", map[string]any{"includeLayers": false, "cwd": dir})
	if err != nil {
		return nil, err
	}
	requirements, err := call(2, "configRequirements/read", map[string]any{})
	if err != nil {
		return nil, err
	}
	return validateCodexConfig(config, requirements, controls, final)
}

func codexField(root map[string]any, key string) (any, bool) {
	var value any = root
	for _, part := range strings.Split(key, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return value, true
}

func validateCodexConfig(configData, requirementsData []byte, controls []codexControl, final bool) (*codexConfigSnapshot, error) {
	var response struct {
		Config  map[string]any `json:"config"`
		Origins map[string]struct {
			Name struct {
				Type string `json:"type"`
			} `json:"name"`
		} `json:"origins"`
	}
	var policy map[string]any
	if json.Unmarshal(configData, &response) != nil || response.Config == nil || response.Origins == nil || json.Unmarshal(requirementsData, &policy) != nil {
		return nil, fmt.Errorf("Codex configuration response is incompatible")
	}
	cfg := response.Config
	requirements, exists := policy["requirements"]
	if !exists {
		return nil, fmt.Errorf("Codex managed requirements response is incompatible")
	}
	var req map[string]any
	if requirements != nil {
		var ok bool
		req, ok = requirements.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Codex managed requirements response is incompatible")
		}
	}
	contextValues := make(map[string]any, len(controls)+16)
	contextOrigins := make(map[string]string, len(controls))
	for _, control := range controls {
		contextOrigins[control.key] = response.Origins[control.key].Name.Type
		if control.key == "tools.update_plan.enabled" || control.key == "tools.experimental_request_user_input.enabled" {
			origin, ok := response.Origins[control.key]
			if !ok || origin.Name.Type != "sessionFlags" {
				return nil, fmt.Errorf("Codex configuration control %s lacks session provenance", control.key)
			}
			contextValues[control.key] = false
			continue
		}
		got, ok := codexField(cfg, control.key)
		want := control.value
		switch value := want.(type) {
		case int:
			want = float64(value)
		case []string:
			want = []any{}
		}
		if !ok || !reflect.DeepEqual(got, want) {
			return nil, fmt.Errorf("Codex configuration control %s conflicts with isolation", control.key)
		}
		contextValues[control.key] = got
	}
	for _, key := range []string{"default_permissions", "permissions"} {
		if value, ok := cfg[key]; ok && value != nil {
			permissions, isTable := value.(map[string]any)
			if key == "default_permissions" || !isTable || len(permissions) != 0 {
				return nil, fmt.Errorf("Codex named permissions conflict with the read-only route")
			}
		}
	}
	for field, want := range map[string]string{
		"allowedApprovalPolicies": "never", "allowedApprovalsReviewers": "user", "allowedSandboxModes": "read-only", "allowedWebSearchModes": "disabled",
	} {
		value, ok := req[field]
		if !ok || value == nil {
			continue
		}
		values, ok := value.([]any)
		allowed := false
		for _, item := range values {
			if item == want {
				allowed = true
			}
		}
		if !ok || !allowed {
			return nil, fmt.Errorf("Codex managed constraint %s conflicts with isolation", field)
		}
	}
	for _, key := range []string{"allowedPermissionProfiles", "defaultPermissions"} {
		if value, ok := req[key]; ok && value != nil {
			return nil, fmt.Errorf("Codex managed permission profiles conflict with isolation")
		}
	}
	if value, ok := req["hooks"]; ok && value != nil && !emptyCodexValue(value) {
		return nil, fmt.Errorf("Codex managed hooks conflict with isolation")
	}
	if value, ok := req["modelCatalogJson"]; ok && value != nil && value != cfg["model_catalog_json"] {
		return nil, fmt.Errorf("Codex managed catalog conflicts with isolation")
	}
	if value, ok := req["featureRequirements"]; ok && value != nil {
		features, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Codex managed features are incompatible")
		}
		for _, name := range codexDisabledFeatures {
			if value, exists := features[name]; exists && value != false {
				return nil, fmt.Errorf("Codex managed features conflict with isolation")
			}
		}
		if value, exists := features["skip_host_skill_discovery"]; exists && value != true {
			return nil, fmt.Errorf("Codex managed skill discovery conflicts with isolation")
		}
	}
	var ids []string
	if value, exists := cfg["mcp_servers"]; exists && value != nil {
		servers, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Codex MCP configuration is incompatible")
		}
		ids = make([]string, 0, len(servers))
		for id, value := range servers {
			server, ok := value.(map[string]any)
			if !ok || (final && server["enabled"] != false) {
				return nil, fmt.Errorf("Codex MCP server is not disabled")
			}
			ids = append(ids, id)
		}
		sort.Strings(ids)
	}
	// Only digests survive the response scope; unknown values never enter diagnostics.
	for _, key := range []string{"model_reasoning_effort", "model_provider", "model_providers", "cli_auth_credentials_store", "chatgpt_base_url", "forced_login_method", "forced_chatgpt_workspace_id", "features"} {
		contextValues[key] = cfg[key]
	}
	contextValues["requirements"] = req
	for key, origin := range response.Origins {
		root, _, _ := strings.Cut(key, ".")
		if _, relevant := contextValues[root]; relevant {
			contextOrigins[key] = origin.Name.Type
		}
	}
	contextValues["origins"] = contextOrigins
	encoded, err := json.Marshal(contextValues)
	if err != nil {
		return nil, fmt.Errorf("Codex configuration comparison is incompatible")
	}
	return &codexConfigSnapshot{serverIDs: ids, context: sha256.Sum256(encoded)}, nil
}

func emptyCodexValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case []any:
		return len(v) == 0
	case map[string]any:
		for _, item := range v {
			if !emptyCodexValue(item) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
