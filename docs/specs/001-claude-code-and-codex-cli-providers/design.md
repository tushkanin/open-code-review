---
schema_version: 13
title: "CLI completion adapters"
created_at: "2026-10-12"
updated_at: "2026-10-12T11:35:36Z"
approved_at: "2026-10-12T11:35:36Z"
---

## Provider boundary

Add built-in provider IDs `claude-code` and `codex`. Use distinct CLI protocols so the existing client factory selects the correct adapter.

Both adapters implement `LLMClient.CompletionsWithCtx`. They share a completion format, but each adapter owns its command arguments and output parser.

The resolver retains existing provider and model precedence. CLI protocols use CLI-managed authentication and require no HTTP URL or `ocr` credential helper.

Authentication behavior follows the effective protocol. An override to an HTTP protocol retains that protocol's credential requirements.

## Completion flow

Each completion request starts a fresh CLI invocation. The adapter sends the complete request context through stdin.

The request includes:

- Ordered messages and their roles.
- Message content, including supported content blocks.
- Previous assistant tool calls.
- Tool results and their call identifiers.
- Current tool definitions and parameter schemas.
- Tool choice and relevant request limits.

The adapter treats this transcript as the conversation to continue. It does not use CLI session resume or let earlier CLI runs add context.

The shared response format is:

```json
{
  "content": "Assistant text",
  "tool_calls": [
    {
      "id": "call_1",
      "name": "file_read",
      "arguments": "{\"path\":\"internal/llm/client.go\"}"
    }
  ]
}
```

A JSON Schema constrains the response shape. The adapter translates this response into the existing `ChatResponse`.

Requests without tools use an empty `tool_calls` array. This also supports planning and context-compression requests through the same adapter.

`ocr` executes returned calls and adds their results to the next request. For example, `file_read` runs through the existing tool registry.

This implements [PROVIDER-03](spec.md#provider-03-existing-orchestration), [PROVIDER-04](spec.md#provider-04-request-context), [PROVIDER-05](spec.md#provider-05-tool-execution), and [PROVIDER-06](spec.md#provider-06-response-compatibility).

## Claude Code adapter

Invoke `claude -p` with:

- The resolved model.
- JSON output and the shared response schema.
- An empty built-in tool set.
- Strict MCP configuration with no supplied servers.
- Safe mode to disable optional repository customizations.
- No permission prompts.
- No session persistence.

Read the completion from `structured_output`. Check the CLI result status before accepting it.

Use safe mode rather than bare mode. Bare mode excludes OAuth and keychain authentication.

Native review tools remain disabled. Administrative policies still apply; the adapter does not bypass them.

## Codex adapter

Invoke `codex exec` with JSONL events, the shared response schema, the resolved model, and an ephemeral session.

Use a temporary working directory outside the repository. Retain normal Codex user configuration and authentication. Do not use `--ignore-user-config`.

The adapter preserves `CODEX_HOME`, authentication storage, provider routing, and native authentication policy. It adds no OCR profile control or credential logic.

Codex owns credentials and refresh. The adapter does not read `auth.json`, query the keyring, copy credentials, or change authentication selectors.

Native workspace-policy bootstrap and runtime authentication can use different permitted identities. Preserve this native behavior rather than forcing those identities to match.

### Restricted native capabilities

Codex lacks a public universal tool allowlist. Feature flags alone do not exclude patch or model-selected subagent tools.

Export complete metadata for the exact resolved model from the installed CLI before inference. Create a temporary native catalog that preserves its identity and unrelated metadata.

Disable its shell, patch, experimental tools, code execution, search, and subagent capabilities. Do not invent missing metadata or substitute a model alias.

Combine this catalog with effective native controls for tools, hooks, rules, skills, plugins, apps, notifications, repository instructions, and other optional sources.

Disable model-facing utility tools and unsolicited user-input tools. Replace optional user instructions with the shared completion instructions.

Use the native legacy read-only sandbox route. Do not select a custom permission profile. Reject inherited or managed profiles that replace the validated route.

The sandbox is an additional constraint, not a replacement for tool controls.

Explicitly disable every merged MCP server. An empty configuration table cannot clear entries because native tables merge recursively.

Disable legacy notifications separately from structured hooks. Reject required managed hook definitions or directories instead of bypassing them.

Mandatory native instructions and administrative policies still apply. Reject a policy that prevents a required control. Do not rewrite that policy.

### Native configuration validation

Use native `features list`, `config/read`, and `configRequirements/read` before completion. Check effective values, not only feature names or submitted flags.

Use the same executable, environment, working directory, configuration context, and startup overrides for validation and completion.

The first validation pass identifies merged MCP servers. The final pass validates explicit per-server disablement and every other required control.

Check effective optional-feature gates before starting each configuration app-server. Disable preflight remote control with the reviewed native release control.

Use fresh stdio app-server processes without daemon or remote-mode requests. Start no thread, model turn, hook, MCP transport, or optional extension action during preflight.

Native authentication and workspace-policy bootstrap remain permitted. Do not request inventories or status APIs that initialize optional sources.

Read configuration with `includeLayers=false`. Check managed requirements separately. Reject a new MCP identifier, a changed relevant context, or an unmet control.

This is a fixed two-pass strategy, not an automatic retry loop. Stop the preflight process before completion.

Some native API projections omit non-feature control values. Accept those values only through a constrained native provenance proof for known startup overrides.

Require each affected leaf's winning origin to identify the submitted session override. Native source must map that field directly to the required runtime value.

Reject missing, ambiguous, or higher-precedence origins. Do not infer hidden values from a reduced API response or claim that it exposes those values.

### Sensitive response boundary

Native configuration responses can contain credentials and private instructions. OCR may receive these response bytes in local memory for the safety checks.

Retain only safety fields, MCP identifiers and states, model/catalog identity, relevant selectors, and non-secret provenance. Discard unrelated values after the check.

Do not put raw responses or unknown value text in logs, errors, prompts, review output, temporary files, or telemetry.

Use bounded protocol frames and the request deadline. Report fixed field names and failure categories without raw response content.

This boundary does not prevent secrets from entering process memory. It does not claim native redaction or a field-selective configuration API.

Do not copy mandatory instruction text from a response into an OCR prompt. Codex applies its native mandatory instructions itself.

### No-tool proof and compatibility

Support only reviewed native releases whose configuration, metadata, provenance, and registration behavior establish an empty native model-tool set.

The proof combines restricted model metadata, effective native controls, disabled MCP entries, and absent dynamic or extension inputs.

It covers core tools, model-selected tools, hosted tools, MCP resource handlers, and ordinary extension tool contributors. A full tool-schema export API is not required.

Native normalization can enable `unified_exec` despite a disable flag. It remains inert when the shell feature and selected model shell capability are disabled.

Reject an unsupported release, incomplete metadata, unavailable proof, or conflicting managed requirement before a model request.

Keep exact release controls and their source gate map in Task 001/003's Plan. Actual authentication and completion evidence remain required by that Task.

### Configuration stability and race limit

The no-tool proof assumes stable configuration and managed policy from the first preflight load through exec and thread startup.

This condition requires approval with this Design. Do not change configuration, authentication selectors, provider routing, profile context, or managed policy during that interval.

Codex reloads configuration after preflight. Its public exec route provides no atomic handoff or expected-configuration token.

A later configuration or policy change can invalidate the checks. For example, a new managed requirement can enable a prohibited feature.

The two-pass comparison rejects detected changes. It does not close the later race window.

Do not claim race-free isolation, add an automatic retry, or bypass a changed policy. Event rejection cannot undo a native tool action that already runs.

### Completion result

Accept the final structured `agent_message` only after a successful terminal event and process exit. Treat native review-tool events as a provider failure.

Prevent the native informational feature-stage notice through its narrow native control. This control changes no feature value.

Do not suppress configuration warnings, model reroutes, failed turns, or fatal errors. Reject unclassified JSONL error items because their original notification kind is unavailable.

Do not accept an error by its position, message text, or a broad ignore rule.

## Process lifecycle

Each request owns its process, output streams, schema file, and temporary working directory.

The process inherits normal CLI authentication context. The adapter passes the prompt through stdin and executes the binary directly, without a shell.

Cancellation and request deadlines stop the process. Process wait and pipe cleanup must remain bounded.

The adapter captures stdout for parsing. Raw child output does not enter standard review output. Failures use the existing provider error path.

Temporary files and directories are removed on success, failure, and cancellation.

## Configuration and IDE integration

Use the existing provider configuration and selection interfaces. Generate frontend and IntelliJ provider catalogs from the existing registry.

CLI providers expose the normal model controls without API-key or URL requirements. Connection checks use the same adapters, including their existing tool round trip.

IDE connection checks currently replace `HOME` with a temporary directory. Preserve the original CLI authentication roots before that replacement:

- Respect an existing `CLAUDE_CONFIG_DIR`; otherwise use the original home's `.claude` directory.
- Respect an existing `CODEX_HOME`; otherwise use the original home's `.codex` directory.

Do not copy credentials into temporary directories.

## Usage and request limits

Map actual CLI token usage into the existing usage fields. Include cache counts where the CLI reports them. Existing aggregate token budgets then use this data.

Claude exposes a native output-token setting. Codex exposes no documented equivalent hard output cap. Prompt guidance must not be reported as an enforced cap.

Existing HTTP retry and raw-capture mechanisms remain HTTP-specific. The adapters do not fabricate HTTP attempts, status codes, or request bodies.
