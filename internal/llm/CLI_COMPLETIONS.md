# CLI completion adapters

`NewClaudeCodeClient` implements `LLMClient` through the installed `claude` executable on `PATH`.
The adapter uses CLI-managed authentication.
It does not use `ClientConfig.APIKey`, `URL`, credential helpers, HTTP retries, or HTTP raw capture.
Select `claude-code` or `codex` in `ocr config provider` or either IDE provider form.
Both built-in providers use the normal OCR review workflow.

```sh
ocr config set providers.claude-code.model sonnet
ocr config set provider claude-code
ocr llm test
```

For Codex, set `providers.codex.model` to `gpt-6.1-sol` and select `codex`.
Install and authenticate the native CLI before the connection check.
OCR requires a model but no API key, credential helper, or HTTP URL for either native protocol.
The model list contains suggestions. Users can save other model names.
Native compatibility checks still validate the selected model.
An HTTP protocol override requires its applicable URL and OCR credentials.
The setup form follows the effective protocol and restores the API-key field for an HTTP override.
Native provider selection does not copy another provider's saved API key.
HTTP overrides retain the selected provider's saved key.
Existing Official and Custom selections retain only that entry's saved key, including entries that use ambient authentication.
Ambient protocols ignore that key and do not execute its credential helper.
Configure a URL override with `ocr config set providers.<name>.url <url>`.

## Native reasoning effort

Set `providers.claude-code.reasoning_effort` or `providers.codex.reasoning_effort` in OCR's `config.json`.
Custom entries with native CLI protocols accept the same field.

```sh
ocr config set providers.claude-code.reasoning_effort high
ocr config set providers.codex.reasoning_effort high
```

The selected native model determines which effort values it supports.
OCR passes the value to the native CLI without a separate effort list.
HTTP providers continue to use `extra_body` for model-specific request options.

An explicit Claude value replaces the inherited `CLAUDE_CODE_EFFORT_LEVEL`.
An explicit Codex value sets `model_reasoning_effort` for both preflight passes and completion.
OCR checks that Codex applies the requested value.
Codex effort values and origins also participate in the preflight context comparison.

Omit the field or set it to an empty string to retain the native CLI setting.
For example, `ocr config set providers.codex.reasoning_effort ""` removes the override.
This field does not change OCR's top-level `effort` or `--effort`, which controls review rounds.

## Request and response

Each request starts a fresh `claude -p` process in a temporary directory outside the repository.
The process receives the complete `ChatRequest` as JSON through stdin.
The transcript retains message roles, content blocks, assistant calls, result identifiers, tool definitions, tool choice, and request limits.
The adapter uses the request model, with the configured model as its fallback.

A shared JSON Schema requires `content` and `tool_calls`.
Each call contains `id`, `name`, and a JSON object encoded as an `arguments` string.
The adapter reads `structured_output` only from a successful Claude result.
It rejects missing fields, empty responses, invalid arguments, duplicate identifiers, and unavailable tools.
OCR executes returned calls through its existing tool registry.
The next request supplies those calls and their corresponding results.

The invocation disables native tools, optional customizations, skills, Chrome, external MCP servers, permission prompts, and session persistence.
Safe mode preserves CLI authentication and administrative policies.
The adapter does not use bare mode or permission bypass flags.

## Limits and lifecycle

A positive `MaxTokens` sets Claude's native `CLAUDE_CODE_MAX_OUTPUT_TOKENS` environment variable.
The transcript also carries the requested temperature and tool choice.
The adapter does not claim a native temperature control.

Usage reflects Claude's actual input, output, cache-read, and cache-creation counts.
Prompt tokens include uncached input and both cache counts.
Total tokens include prompt tokens and output tokens.
Missing CLI usage remains absent rather than an estimate.

The configured request timeout defaults to five minutes.
Caller cancellation and deadlines stop the command.
On Unix, cancellation stops its process group.
The child uses that directory for `TMPDIR`, `TMP`, and `TEMP`, so cleanup also removes Claude's scratch files.
A one-second wait bound prevents inherited output pipes from blocking cleanup.
The adapter captures stdout and discards stderr instead of exposing raw child output in a review.
The adapter removes the temporary directory on success, failure, and cancellation.

## Verification

Run the focused checks after a coherent source change:

```sh
make license-add
make check PACKAGES=./internal/llm
make test PACKAGES='./internal/llm ./internal/llmloop ./internal/tool'
```

The opt-in smoke exercise uses the real installed Claude command and its current authentication.
It incurs normal CLI usage.
It first requests text without tools.
It then requests `file_read`, executes that call through the OCR registry, and supplies its result to a second completion.
A random fixture value proves that the continuation uses the OCR result.
The fixture remains outside the CLI's temporary directory.

```sh
OCR_CLAUDE_SMOKE=1 OCR_CLAUDE_SMOKE_MODEL=sonnet \
  make test PACKAGES='./internal/llm -run=TestClaudeCodeRealSmoke'
```

## Codex

`NewCodexClient` implements the same contract through `codex exec`.
The adapter does not restrict the Codex CLI version.
It checks the bundled model catalog, effective features, configuration, and event stream at runtime.
These checks do not establish that every release disables all native tools.

The adapter exports the bundled native catalog through `codex debug models --bundled`.
The resolved model must match an exported identifier exactly.
The request catalog preserves all native metadata except these approved capabilities:

- `shell_type` becomes `disabled`.
- `apply_patch_tool_type`, `tool_mode`, and `multi_agent_version` become null.
- `experimental_supported_tools` becomes empty.
- `node_repl_disabled` becomes true.
- `supports_search_tool` becomes false.

The startup `model_catalog_json` override uses this catalog without a remote refresh.
Missing model metadata, missing capability fields, and unavailable feature controls return provider errors.
The adapter does not invent model metadata or substitute a model alias.

The invocation retains normal Codex user configuration, `CODEX_HOME`, and native authentication policy.
Codex owns credentials and refresh.
The adapter preserves authentication storage, backend selection, and provider routing.
It uses no OCR API key, credential helper, or separate profile selector.
It does not read `auth.json`, query the keyring, or copy credentials.

Each request uses a fixed two-pass preflight before completion.
Both passes validate effective feature values before a fresh strict stdio app-server starts.
The first pass discovers MCP servers through `config/read` with `includeLayers=false`.
The second pass applies one aggregate inline-table override with explicit `enabled=false` for every server.
Recursive merge preserves each server's transport fields.
The final check rejects new server identifiers, changed relevant context, and conflicting managed requirements from `configRequirements/read`.

Both passes use the completion executable, environment, working directory, and common startup overrides.
Preflight starts no thread, model turn, MCP transport, hook, or optional extension action.
The reviewed `CODEX_INTERNAL_APP_SERVER_REMOTE_CONTROL_DISABLED=1` marker disables preflight remote control.
Strict configuration, disabled exec-policy rules, and the legacy read-only sandbox apply to completion.
Inherited or managed permission profiles cannot replace that sandbox route.

The temporary instructions file replaces native model instructions.
Controlled nonempty compact instructions prevent an inherited compact-file fallback.
Project instructions and automatic skill instructions remain disabled.
Cloud skills, workspace dependencies, plugins, apps, hooks, and host skill discovery remain disabled.
An empty notification list disables legacy external notifications separately from structured hooks.
Mandatory native instructions and compatible administrative policies still apply.

Native configuration responses can contain credentials and private instructions.
The adapter receives bounded responses in local memory.
It retains safety fields, server identifiers, non-secret origin types, and a digest of the relevant configuration context.
It puts no raw configuration response, private instruction text, or unknown native protocol error in logs, errors, prompts, files, or telemetry.
The native API does not guarantee secret redaction.

The native API omits two utility booleans: `tools.update_plan.enabled` and `tools.experimental_request_user_input.enabled`.
Their exact winning origins must identify the known false session overrides.
The reviewed native source connects those leaves directly to their runtime values.
Missing or non-session origins cause a provider error.

The isolation proof assumes stable configuration and managed policy from the first preflight through exec and thread startup.
Codex reloads configuration after preflight.
The comparison detects some changes but does not close the later race.
The adapter adds no retry, policy bypass, or race-free isolation claim.

Native feature controls disable shell, image, browser, computer, code execution, subagent, and tool-suggestion categories.
Additional controls disable utility tools: plans, user questions, permissions, deferred environments, token budgets, clocks, sleep, and asynchronous messages.
The read-only sandbox adds a constraint; it does not replace these tool gates.
The compatibility basis is the 0.162.1 native tool registry and configuration schema.
See `codex-rs/core/src/tools/spec_plan.rs` and `codex-rs/config/src/config_toml.rs` in that release.

The decoder accepts structured `agent_message` output only after `turn.completed` and a successful process exit.
It rejects native tool items, failed events, unknown events, malformed output, and incomplete streams.
The native `suppress_unstable_features_warning=true` option prevents the informational feature-stage notice before Codex creates it.
The option does not change feature values or suppress configuration warnings, model reroutes, or fatal events.
The required `skip_host_skill_discovery` feature remains enabled.
Remaining `error` items represent unclassified provider conditions.
The decoder rejects them because JSONL loses their original notification kind.
The decoder does not accept notices by their position or message text.
Actual input and output counts determine usage.
Reported cached input and cache-write counts reach their existing fields without another addition to prompt tokens.
Absent usage remains absent.

The transcript retains request limits, including `MaxTokens`.
Codex has no documented native hard output-token cap.
The adapter supplies prompt guidance, not an enforced output-token cap.
The same request deadline covers compatibility commands, catalog export, both preflight passes, and completion.
Every command uses bounded output, a one-second pipe wait, and the request scratch directory.
Unix cancellation stops the process group.
Windows process controls need a separate runtime exercise.

The opt-in exercise checks text output and a two-turn OCR registry call with a random fixture.
The decoder rejects native tool events during all real requests.
The exercise also checks actual usage, model selection, call identifiers, and temporary cleanup.
The retained-config exercise succeeds on codex-cli 0.162.1 with `gpt-6.1-sol` and no OCR API key.
OCR executes `file_read` once, and the next completion returns the exact random fixture value.

```sh
OCR_CODEX_SMOKE=1 OCR_CODEX_SMOKE_MODEL=gpt-6.1-sol \
  make test PACKAGES='./internal/llm -run=TestCodexRealSmoke'
```
