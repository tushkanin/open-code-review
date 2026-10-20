// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func codexTestProjection(t *testing.T) (map[string]any, map[string]any, []codexControl) {
	t.Helper()
	controls := codexControls("selected-model", t.TempDir())
	cfg, origins := map[string]any{}, map[string]any{}
	for _, control := range controls {
		setCodexTestField(cfg, control.key, control.value)
		origins[control.key] = map[string]any{"name": map[string]any{"type": "sessionFlags"}, "version": "first"}
	}
	cfg["tools"] = map[string]any{"web_search": nil}
	cfg["mcp_servers"] = map[string]any{"fixture.server": map[string]any{"enabled": false, "headers": map[string]any{"Authorization": "sensitive-sentinel"}}}
	cfg["cli_auth_credentials_store"] = "keyring"
	cfg["model_provider"] = "native-provider"
	return map[string]any{"config": cfg, "origins": origins}, map[string]any{"requirements": nil}, controls
}

func encodeCodexTestProjection(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCodexEffectiveFeatures(t *testing.T) {
	var rows strings.Builder
	for _, name := range codexDisabledFeatures {
		rows.WriteString(name + " stable false\n")
	}
	rows.WriteString("skip_host_skill_discovery under development true\nunified_exec stable true\n")
	good := rows.String()
	if err := checkCodexFeatures([]byte(good)); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]string{
		"managed image":  strings.Replace(good, "view_image stable false", "view_image stable true", 1),
		"managed skills": strings.Replace(good, "skip_host_skill_discovery under development true", "skip_host_skill_discovery under development false", 1),
		"missing":        strings.Replace(good, "shell_tool stable false\n", "", 1),
		"duplicate":      good + "shell_tool stable false\n",
		"malformed":      strings.Replace(good, "shell_tool stable false", "shell_tool stable sensitive-sentinel", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if err := checkCodexFeatures([]byte(bad)); err == nil || strings.Contains(err.Error(), "sensitive-sentinel") {
				t.Fatalf("unsafe feature result: %v", err)
			}
		})
	}
}

func TestCodexNativeConfigurationConflicts(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(map[string]any, map[string]any)
	}{
		{"image policy", func(_, req map[string]any) { req["featureRequirements"] = map[string]any{"view_image": true} }},
		{"skill policy", func(_, req map[string]any) {
			req["featureRequirements"] = map[string]any{"skip_host_skill_discovery": false}
		}},
		{"approval policy", func(_, req map[string]any) { req["allowedApprovalPolicies"] = []any{"on-request"} }},
		{"reviewer policy", func(_, req map[string]any) { req["allowedApprovalsReviewers"] = []any{"guardian"} }},
		{"sandbox policy", func(_, req map[string]any) { req["allowedSandboxModes"] = []any{} }},
		{"search policy", func(_, req map[string]any) { req["allowedWebSearchModes"] = []any{"enabled"} }},
		{"named permissions", func(cfg, _ map[string]any) { cfg["default_permissions"] = "sensitive-sentinel" }},
		{"permissions table", func(cfg, _ map[string]any) { cfg["permissions"] = map[string]any{"custom": map[string]any{}} }},
		{"managed permissions", func(_, req map[string]any) { req["allowedPermissionProfiles"] = map[string]any{"custom": true} }},
		{"managed default", func(_, req map[string]any) { req["defaultPermissions"] = "custom" }},
		{"managed hook", func(_, req map[string]any) {
			req["hooks"] = map[string]any{"SessionStart": []any{map[string]any{"hooks": []any{map[string]any{"command": "sensitive-sentinel"}}}}}
		}},
		{"managed directory", func(_, req map[string]any) { req["hooks"] = map[string]any{"managedDir": "/private"} }},
		{"managed windows directory", func(_, req map[string]any) { req["hooks"] = map[string]any{"windowsManagedDir": "private"} }},
		{"managed catalog", func(_, req map[string]any) { req["modelCatalogJson"] = "/private/catalog" }},
		{"notification", func(cfg, _ map[string]any) { cfg["notify"] = []any{"sensitive-sentinel"} }},
		{"instruction", func(cfg, _ map[string]any) { cfg["developer_instructions"] = "sensitive-sentinel" }},
		{"compact fallback", func(cfg, _ map[string]any) { cfg["compact_prompt"] = "" }},
		{"compact file", func(cfg, _ map[string]any) { cfg["experimental_compact_prompt_file"] = "/private" }},
		{"MCP default enabled", func(cfg, _ map[string]any) {
			cfg["mcp_servers"] = map[string]any{"sensitive-sentinel": map[string]any{"command": "private"}}
		}},
		{"MCP enabled", func(cfg, _ map[string]any) {
			cfg["mcp_servers"] = map[string]any{"server": map[string]any{"enabled": true}}
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			response, requirements, controls := codexTestProjection(t)
			req := map[string]any{}
			requirements["requirements"] = req
			scenario.change(response["config"].(map[string]any), req)
			_, err := validateCodexConfig(encodeCodexTestProjection(t, response), encodeCodexTestProjection(t, requirements), controls, true)
			if err == nil || strings.Contains(err.Error(), "sensitive-sentinel") {
				t.Fatalf("unsafe configuration result: %v", err)
			}
		})
	}
}

func TestCodexUtilityProvenance(t *testing.T) {
	for _, key := range []string{"tools.update_plan.enabled", "tools.experimental_request_user_input.enabled"} {
		for _, kind := range []string{"missing", "legacyManagedConfigTomlFromFile", "user", "ambiguous"} {
			t.Run(key+"/"+kind, func(t *testing.T) {
				response, requirements, controls := codexTestProjection(t)
				origins := response["origins"].(map[string]any)
				delete(origins, key)
				if kind != "missing" {
					origins[key] = map[string]any{"name": map[string]any{"type": kind}}
				}
				if _, err := validateCodexConfig(encodeCodexTestProjection(t, response), encodeCodexTestProjection(t, requirements), controls, true); err == nil {
					t.Fatal("unproved utility value succeeds")
				}
			})
		}
	}
}

func TestCodexConfigurationContext(t *testing.T) {
	for _, store := range []string{"file", "keyring", "auto", "ephemeral"} {
		t.Run(store, func(t *testing.T) {
			response, requirements, controls := codexTestProjection(t)
			cfg := response["config"].(map[string]any)
			cfg["cli_auth_credentials_store"] = store
			cfg["model_reasoning_effort"] = "high"
			origins := response["origins"].(map[string]any)
			origins["model_reasoning_effort"] = map[string]any{"name": map[string]any{"type": "user"}}
			setCodexTestField(cfg, "features.secret_auth_storage", true)
			cfg["chatgpt_base_url"] = "https://native.invalid"
			servers := cfg["mcp_servers"].(map[string]any)
			servers["fixture.server"].(map[string]any)["enabled"] = true
			first, err := validateCodexConfig(encodeCodexTestProjection(t, response), encodeCodexTestProjection(t, requirements), controls, false)
			if err != nil {
				t.Fatal(err)
			}
			servers["fixture.server"].(map[string]any)["enabled"] = false
			second, err := validateCodexConfig(encodeCodexTestProjection(t, response), encodeCodexTestProjection(t, requirements), controls, true)
			if err != nil || !first.sameContext(second) {
				t.Fatalf("expected server override changes context: %v", err)
			}
			cfg["model_reasoning_effort"] = "low"
			changedEffort, err := validateCodexConfig(encodeCodexTestProjection(t, response), encodeCodexTestProjection(t, requirements), controls, true)
			if err != nil || first.sameContext(changedEffort) {
				t.Fatal("native reasoning effort change remains undetected")
			}
			cfg["model_reasoning_effort"] = "high"
			origins["model_reasoning_effort"] = map[string]any{"name": map[string]any{"type": "system"}}
			changedEffortOrigin, err := validateCodexConfig(encodeCodexTestProjection(t, response), encodeCodexTestProjection(t, requirements), controls, true)
			if err != nil || first.sameContext(changedEffortOrigin) {
				t.Fatal("native reasoning effort origin change remains undetected")
			}
			origins["model_reasoning_effort"] = map[string]any{"name": map[string]any{"type": "user"}}
			originalOrigin := origins["approval_policy"]
			origins["approval_policy"] = map[string]any{"name": map[string]any{"type": "system"}}
			changedOrigin, err := validateCodexConfig(encodeCodexTestProjection(t, response), encodeCodexTestProjection(t, requirements), controls, true)
			if err != nil || first.sameContext(changedOrigin) {
				t.Fatal("configuration origin change remains undetected")
			}
			origins["approval_policy"] = originalOrigin
			cfg["cli_auth_credentials_store"] = "changed"
			changed, err := validateCodexConfig(encodeCodexTestProjection(t, response), encodeCodexTestProjection(t, requirements), controls, true)
			if err != nil || first.sameContext(changed) {
				t.Fatal("authentication change remains undetected")
			}
			cfg["cli_auth_credentials_store"] = store
			servers["new.server"] = map[string]any{"enabled": false}
			changed, err = validateCodexConfig(encodeCodexTestProjection(t, response), encodeCodexTestProjection(t, requirements), controls, true)
			if err != nil || first.sameContext(changed) {
				t.Fatal("new server remains undetected")
			}
		})
	}
}

func TestCodexMissingManagedRequirements(t *testing.T) {
	response, _, controls := codexTestProjection(t)
	config := encodeCodexTestProjection(t, response)
	for _, data := range []string{`{}`, `null`, `{"requirements":[]}`, `{"requirements":"sensitive-sentinel"}`} {
		if _, err := validateCodexConfig(config, []byte(data), controls, true); err == nil || strings.Contains(err.Error(), "sensitive-sentinel") {
			t.Fatalf("unsafe managed requirements result: %v", err)
		}
	}
}

func TestCodexConfigurationResponseBounds(t *testing.T) {
	for _, script := range []string{
		"printf 'not JSON\n'",
		"printf '{\"id\":0,\"result\":null}\n'",
		"exit 7",
		"dd if=/dev/zero bs=1048576 count=17 2>/dev/null",
		"printf '{\"id\":0,\"error\":{\"message\":\"sensitive-sentinel\"}}\n'; printf 'sensitive-sentinel' >&2",
	} {
		t.Run(script, func(t *testing.T) {
			tmp := installCodexTestCommand(t, "preflight", script)
			_, err := NewCodexClient(ClientConfig{Model: "selected-model"}).CompletionsWithCtx(t.Context(), ChatRequest{})
			if err == nil || strings.Contains(err.Error(), "sensitive-sentinel") {
				t.Fatalf("unsafe protocol result: %v", err)
			}
			assertCodexCleanup(t, tmp)
		})
	}
}
