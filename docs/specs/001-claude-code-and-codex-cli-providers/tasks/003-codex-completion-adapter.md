---
schema_version: 13
title: "Codex completion adapter"
type: delivery
status: "completed"
order: 30
requirements: [PROVIDER-02, PROVIDER-04, PROVIDER-05, PROVIDER-06, PROVIDER-07, PROVIDER-08, PROVIDER-09, PROVIDER-10, PROVIDER-11]
dependencies: [TASK-001]
created_at: "2026-10-12"
updated_at: "2026-10-12T13:45:02.553864Z"
approved_at: "2026-10-12T07:50:01Z"
status_reason: null
---

## Outcome

A complete Codex client implements the existing provider contract through `codex exec`, using the approved capability restrictions.

## Scope

- The existing shared completion format, response schema, and complete request transcript.
- Codex command invocation and JSONL result decoding.
- Native model catalog export and a temporary catalog with the approved capability restrictions.
- Installed-version compatibility for the required catalog, feature, and isolation controls.
- CLI authentication and isolation from optional configuration, hooks, rules, and tool sources.
- Model selection, applicable request limits, and actual token usage.
- Cancellation, deadlines, bounded process cleanup, and cleanup of temporary files.
- Focused regression coverage, real Codex exercises, and adapter documentation.

### Excluded

- Provider registry, resolver, setup, or graphical provider selection changes.
- IDE authentication-root changes.
- Changes to existing review orchestration.
- Independent reviews or native review-tool execution by Codex.
- New credential management, custom executable paths, or custom command arguments.

## Acceptance

- A real Codex invocation through the client returns a text-only completion with no OCR tool calls.
- A real two-turn exercise returns an OCR tool request, executes it through the existing registry, and uses the returned result.
- The exercise preserves call identifiers and contains no native review-tool events.
- The transcript retains messages, supported content blocks, tool definitions, previous calls, result identifiers, tool choice, and request limits.
- The client uses the resolved model and CLI authentication without an OCR API key or credential helper.
- The temporary catalog preserves the selected model identifier and other exported metadata while applying the approved capability restrictions.
- The client rejects missing model metadata or unsupported required controls instead of inventing metadata or weakening restrictions.
- The invocation applies the approved isolation controls and read-only sandbox while preserving CLI authentication.
- The client accepts final structured assistant output only after a successful terminal event.
- Missing commands, authentication failures, failed events, native review-tool events, and malformed or incomplete output return provider errors.
- Actual CLI token usage reaches the existing usage fields, including cache counts that the CLI reports.
- Request limits remain in the transcript. Documentation does not describe prompt guidance as an enforced Codex output-token cap.
- Cancellation and deadlines bound both catalog export and completion execution without blocked pipe waits or retained temporary files.
- Focused regressions cover catalog preservation, compatibility failures, event decoding, native-tool rejection, and cancellation cleanup.
- Existing Claude and API clients retain their behavior.

## Design reference

Follow the approved [CLI completion adapters Design](../design.md), including its Codex adapter, process lifecycle, and usage rules.

## Plan

### Execution boundary

Follow the approved Design of `2026-10-12T11:35:36Z` without changing the Task contract.
Use the current integration checkout at `/home/oleksii/Projects/personal/open-code-review`.
Preserve the candidate, completed Task evidence, and untracked Specs outside implementation commits.
Continue the retained-config candidate in `internal/llm/codex_client.go` and `internal/llm/codex_config.go`.
Repair its MCP override encoding and subprocess fixture before a real completion.
Passing regressions and native preflight proof do not establish successful authenticated completion.

### Steps

1. Reuse `internal/llm/cli_completion.go` for the schema, transcript encoder, instructions, and completion decoder.
   Repair the existing `NewCodexClient` candidate in `internal/llm/codex_client.go` within the `LLMClient` and `ClientConfig` boundary.
   Keep `NewLLMClient`, the provider registry, the resolver, and IDE code unchanged.

2. Establish release compatibility and prepare the controlled request inputs before a model request.
   Use `codex-cli 0.162.1`, native tag `rust-v0.162.1`, and source commit `092d3acd6bec3e3a14bdc7e7a2810ab628ab759d` as the reviewed baseline.
   Accept no other release without equivalent command, projection, normalization, and registration proof.
   Resolve one executable from PATH and use it for every request command.
   Keep the original environment, `CODEX_HOME`, native base configuration, and one absolute scratch directory outside the repository.
   Create the shared schema and nonempty shared instructions within the request cleanup boundary.
   Export complete metadata with the installed `codex debug models --bundled` command.
   This native route needs no model turn or remote catalog refresh.
   Require one exact matching identifier for the resolved OCR model and compatible capability fields.
   Reject missing metadata, duplicate matches, invalid field types, and unsupported controls.
   Preserve the native envelope and every unrelated model field through JSON values rather than a reduced struct.
   Apply only these seven capability replacements:

   | Field | Replacement |
   | --- | --- |
   | `shell_type` | `"disabled"` |
   | `apply_patch_tool_type` | `null` |
   | `experimental_supported_tools` | `[]` |
   | `tool_mode` | `null` |
   | `node_repl_disabled` | `true` |
   | `supports_search_tool` | `false` |
   | `multi_agent_version` | `null` |

   Store the restricted catalog within the request directory.
   Pass its absolute path through the startup `model_catalog_json` override; a per-thread override does not replace it.
   Validate that the effective catalog path remains this controlled path.
   Reject an incompatible managed catalog instead of substituting its unrestricted metadata.

3. Continue retained native configuration and the fixed two-pass preflight.
   Build argument arrays directly; do not invoke a shell.
   Encode string values and inline-table MCP keys with native TOML quoting.
   Native override paths split at every dot and do not interpret quoted path segments.
   Apply the same common scalar and feature controls to both feature checks, both configuration processes, and completion.
   Change only the discovered per-server MCP controls between the initial and final passes.
   Preserve native auth storage, keyring/backend selection, provider routing, login policy, and workspace requirements.
   Do not add a profile selector, arbitrary native arguments, credential helper, or credential export.
   Do not use `--ignore-user-config`, unset `CODEX_API_KEY`, or force a login method.
   Native workspace-policy bootstrap and runtime serving auth can use different permitted identities; do not force them to match.

   Use these common scalar overrides:

   | Key | Value |
   | --- | --- |
   | `approval_policy` | `"never"` |
   | `approvals_reviewer` | `"user"` |
   | `sandbox_mode` | `"read-only"` |
   | `web_search` | `"disabled"` |
   | `notify` | `[]` |
   | `orchestrator.mcp.enabled` | `false` |
   | `cloud.skills.enabled` | `false` |
   | `tools.update_plan.enabled` | `false` |
   | `tools.experimental_request_user_input.enabled` | `false` |
   | `project_doc_max_bytes` | `0` |
   | `skills.include_instructions` | `false` |
   | `agents.enabled` | `false` |
   | `instructions` | `""` |
   | `developer_instructions` | `""` |
   | `compact_prompt` | The existing nonempty shared completion instructions |
   | `experimental_compact_prompt_file` | The absolute shared instructions path |
   | `include_permissions_instructions` | `false` |
   | `include_apps_instructions` | `false` |
   | `include_collaboration_mode_instructions` | `false` |
   | `include_environment_context` | `false` |
   | `suppress_unstable_features_warning` | `true` |
   | `model` | The exact OCR-resolved model |
   | `model_catalog_json` | The absolute restricted catalog path |
   | `model_instructions_file` | The absolute shared instructions path |

   An empty `compact_prompt` permits the inherited compact-file fallback.
   Both compact controls therefore refer to controlled, nonempty shared instructions.
   Preserve mandatory native instructions; discard their response text rather than copying it into the OCR prompt.

   Disable every feature in this reviewed list and require its effective value to be false:

   - Shell: `shell_tool`, `shell_snapshot`, `shell_snapshot_v2`, `unified_exec_tty`.
   - Images: `view_image`, `image_generation`.
   - Browser: `browser_use`, `browser_use_external`, `browser_use_full_cdp_access`, `browser_annotation_api`.
   - Computer: `computer_use`.
   - Code: `code_mode`, `code_mode_host`, `code_mode_only`, `code_mode_only_strict_3p_tools`, `code_mode_interrupt`, `code_mode_prewarm`, `code_mode_tool_search`.
   - Agents: `multi_agent`, `multi_agent_v2`, `multi_agent_v2_dynamic_tools`, `agent_message_board`, `goals`.
   - Optional sources: `plugins`, `remote_plugin`, `apps`, `enable_mcp_apps`, `tool_suggest`.
   - Skills and dependencies: `skill_search`, `skill_mcp_dependency_install`, `workspace_dependencies`, `executor_capability_discovery`.
   - Other tools: `memories`, `hooks`, `guardianv2`, `deferred_executor`, `request_permissions_tool`, `token_budget`, `current_time_reminder`, `sleep_tool`, `send_message_to_user_async`, `standalone_web_search`.

   Enable `skip_host_skill_discovery` and require its effective value to be true.
   Leave authentication features such as `secret_auth_storage` unchanged.
   Require each relevant feature key exactly once with a valid native boolean.
   Reject missing keys, malformed values, and incompatible managed pins.
   Do not require `unified_exec=false`: native normalization can restore it to true.
   Effective `shell_tool=false` and model `shell_type="disabled"` prevent shell registration independently.

   Execute exactly this sequence:

   1. Run `codex [initial common controls] features list`.
      Validate effective optional gates before any configuration app-server starts.
      Do not pass the unsupported `--strict-config` flag to `features list`.
   2. Start `codex [initial common controls] app-server --listen stdio:// --strict-config`.
      Set `CODEX_INTERNAL_APP_SERVER_REMOTE_CONTROL_DISABLED=1` through the request child environment.
      Use this same environment for the request commands.
      This pinned internal control prevents preflight remote control without a persisted preference change.
      Send `initialize` with `capabilities.experimentalApi=true`, then send the `initialized` notification.
      Request only `config/read` with `includeLayers=false` and the exact scratch `cwd`, plus `configRequirements/read`.
      Start no thread, model turn, hook, MCP transport, or optional extension action.
      Do not request MCP status, hook inventory, plugin inventory, daemon operations, or remote mode changes.
      Extract the safety projection and every merged MCP identifier, then stop this process.
   3. Build one aggregate inline-table override for every discovered identifier.
      Append one `-c` argument and one `mcp_servers={"fixture.server"={enabled=false},"other"={enabled=false}}` value.
      Use the sorted identifiers from the initial snapshot, not the example identifiers.
      Quote each complete identifier as a TOML key inside the value, including dots, quotes, backslashes, and equals signs.
      Set only `enabled=false`; copy no transport, credential, or private configuration values into this argument.
      Omit the override when the initial snapshot contains no servers.
      Do not put an identifier in the dotted override path.
      Do not emit separate `mcp_servers` overrides; the native override layer keeps only the last value at that root.
      Native recursive merge preserves each server's other fields and applies the false leaf.
      An empty MCP table does not remove recursively merged entries.
      An omitted native `enabled` field means true, not false.
   4. Run `codex [final common controls] features list` and validate the effective gates again.
      Start a second fresh strict stdio configuration process with these exact final controls and disabled remote control.
      Request the same configuration and managed-requirements response pair.
      Require every final MCP entry to contain explicit `enabled=false`.
      Reject a new identifier or a changed relevant context instead of repeating discovery.
      Validate every direct scalar control, controlled file selector, managed constraint, and permitted provenance exception below.
      Stop this process before completion.
   5. Run `codex [final common controls] exec --json --ephemeral --output-schema <absolute-schema-path> --model <resolved-model> --ignore-rules --skip-git-repo-check --strict-config --sandbox read-only -`.
      Send the complete shared transcript through stdin.
      Use no resumed session, dynamic tools, selected capability roots, runtime attachments, or selected plugin roots.

   Apply these final validation rules:

   - Require successful native strict configuration construction and exact effective values for the exported required controls.
   - Check model, catalog, base instructions, compact instructions, and their controlled absolute file paths.
   - Require the legacy read-only permission branch; a raw `sandbox_mode` string does not prove resolved permissions.
   - Reject inherited or managed named-permission selection that replaces this branch; add no permission-profile override.
   - Require managed approval, reviewer, sandbox, and web-search constraints to permit `never`, `user`, `read-only`, and `disabled`.
   - Reject managed hook handlers and directory sources, including `managedDir` and `windowsManagedDir`.
   - Preserve native auth/provider constraints and mandatory instructions; do not use a config-write API to bypass a conflict.
   - Compare the narrow relevant selectors, safety values, and non-secret origins between passes.
   - Permit only the expected per-ID MCP override differences; reject other detected relevant changes.

   The typed `ToolsV2` API exports `web_search`, not the two utility booleans.
   Prove only these omitted leaves through exact final native origins:

   - `tools.update_plan.enabled`.
   - `tools.experimental_request_user_input.enabled`.

   Each leaf receives a known false session override.
   Require its winning origin to identify `ConfigLayerSource::SessionFlags`, serialized as `type=sessionFlags`.
   The native origin collector visits enabled layers from low to high precedence and replaces the origin for each scalar leaf.
   The runtime resolvers read these same merged leaves directly.
   The exact managed projection does not replace either leaf; higher legacy-managed layers change the winning origin.
   Reject missing, ambiguous, or non-session origins, even if an unknown higher-layer value could also be false.
   Do not claim that the API exposes either omitted boolean or generalize this proof to other hidden values.

   Parse the native configuration responses only within the approved local-memory boundary.
   Retain only safety fields, MCP identifiers and states, model/catalog identity, relevant selectors, and non-secret provenance.
   Discard unrelated values after each check; request no raw layers.
   Keep raw responses, unknown value text, native protocol errors, and private instruction text out of every output path.
   This includes logs, provider errors, prompts, review output, temporary files, and telemetry.
   Use fixed field names and failure categories in diagnostics.
   Do not read `auth.json`, query the keyring, or use credential values in OCR.
   The native API has no selective-field or guaranteed secret-redaction option; sensitive bytes can enter local memory.

   Rely only on the approved stable-input condition from first load through exec and thread startup.
   Native commands reload configuration and managed policy independently.
   Snapshot comparison detects some changes but does not close the later race.
   Add no retry loop, atomic-token claim, policy bypass, or invented config lock.
   Event rejection cannot undo a native action that already runs.

4. Decode the complete JSONL result in the Codex adapter.
   Accept final structured `agent_message` output only after a successful terminal event and process exit.
   Release 0.162.1 defines error items as non-fatal notices, not fatal thread errors.
   JSONL error items lose the original notification kind; configuration warnings and model reroutes use the same item shape.
   Reject every remaining `item.*` event with `item.type="error"` as an unclassified provider condition.
   Do not describe the native item type alone as proof of fatal execution.
   Do not accept these items by startup position, warning text, or a broad ignore rule.
   Treat top-level `error` and `turn.failed` events as fatal failures.
   Reject native tool events, malformed data, truncated streams, and missing final output.
   Pass the structured completion through `decodeCLICompletion` and retain the resolved model identifier.
   Map actual usage into `UsageInfo`; keep absent counts absent and avoid duplicate cache accounting.
   Keep request limits in the transcript without claiming a native hard output-token cap.
   Keep HTTP retry and raw-capture code outside this adapter.
   Use `suppress_unstable_features_warning=true` only for the native feature-stage notice generator.
   This option changes no feature value and suppresses no configuration warning, model reroute, or fatal event.

5. Apply existing timeout and process ownership patterns to every request phase.
   Reuse `describeTimeout`, `configureCLIProcess`, and bounded output from `claude_code_client.go`.
   Give version/catalog export, both feature checks, both configuration processes, and completion one request deadline.
   Preserve bounded frames, the existing 16 MiB output ceiling, and a one-second `WaitDelay` where applicable.
   Bound protocol reads and pipe waits; cancellation must stop each active child.
   Stop each preflight child before the next phase, including failure paths.
   Keep schema, instructions, catalog, working directory, and child scratch files inside one cleanup boundary.
   Remove that boundary after success, failure, or cancellation.
   Do not retain raw configuration or stderr in diagnostics or persistent files.

6. Repair and extend the existing regressions in `internal/llm/codex_client_test.go`.
   Keep native effective-boolean rows and the retained-config, fixed two-pass fixtures.
   Repair MCP handling in `TestCodexProcessHelper` to follow native path splitting and inline-table merge semantics.
   Use a small parser for the generated inline-table grammar if the fixture has no TOML parser.
   Add no dependency solely for this fixture.
   Reject unsupported fixture syntax rather than accepting quoted dotted override keys as complete server identifiers.
   Cover full metadata preservation, exact model selection, unsupported releases/controls, and missing or duplicate metadata.
   Cover managed feature conflicts, missing/duplicate/malformed rows, normalized `unified_exec`, and required host-skill isolation.
   Cover complete MCP identifiers with dots, quotes, backslashes, and equals signs inside the aggregate value.
   Cover recursive merge, retained transport fields, omitted/default enablement, final disablement, and a new identifier in the second pass.
   Include multiple discovered servers so a last-root-wins defect cannot leave an earlier server enabled.
   Assert effective disablement and unchanged identifiers, not incidental argument formatting.
   Cover both omitted utility leaves and missing, ambiguous, legacy-managed, or otherwise non-session origins.
   Cover managed scalar/catalog/hook/permission conflicts and changed relevant context without retries.
   Cover optional base/developer/compact instructions, including the empty compact-prompt fallback and controlled compact-file path.
   Cover retained auth storage/backend selectors, provider routing, and unchanged authentication environment through fixture-visible native inputs.
   Do not replace real authentication evidence with fixture assertions or read credentials from the fixture host.
   Assert effective-feature validation before each app-server starts and remote control disablement in each preflight child.
   Assert that preflight sends only initialization and the two approved configuration requests, without a thread or inventory action.
   Put sensitive sentinels in config, requirement, protocol-error, and stderr fixtures; require no persistent or output leak.
   Cover bounded oversized frames/output, failed initialization, missing replies, cancellation in every phase, and cleanup.
   Retain terminal-event, native-tool, malformed-output, actual-usage, and incomplete-stream coverage.
   Retain rejection of unclassified error items, configuration warnings, model reroutes, and fatal events despite valid assistant output.
   Keep existing Claude and API behavior unchanged.

7. Complete the existing real exercise in `internal/llm/codex_smoke_test.go`, external package `llm_test`.
   Follow `claude_code_smoke_test.go` with `TestCodexRealSmoke` and `OCR_CODEX_SMOKE=1`.
   Let `OCR_CODEX_SMOKE_MODEL` select an exported model for the exercise, not a new product configuration path.
   Prove real retained-config text output and a two-turn OCR registry exercise with a random fixture outside the CLI working directory.
   Record call identifiers, actual usage, native-tool absence, selected-model use, and temporary cleanup.
   Update `internal/llm/CLI_COMPLETIONS.md` after implementation.
   Describe retained native auth/config, effective validation, per-server MCP controls, sensitive-memory receipt, reviewed compatibility, and the stable-input race limit.
   Explain the specific feature-stage notice option and conservative treatment of all remaining error items.
   Report the actual retained-config exercise result; do not reuse ignored-config smoke results as proof of this route.

### Native control and registration evidence

Use the pinned `rust-v0.162.1` sources for this reviewed route:

- [CLI commands](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/cli/src/main.rs): effective `features list`, native bundled metadata, and stdio app-server dispatch.
- [Feature normalization](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/core/src/config/managed_features.rs): managed pins and `unified_exec` dependency normalization.
- [Exec flags](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/exec/src/cli.rs): retained-config, rules, ephemeral, and sandbox semantics.
- [Exec startup](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/exec/src/lib.rs), [CLI policy bootstrap](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/cli/src/cloud_config.rs), and [in-process startup](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/app-server/src/in_process.rs): native stored-auth workspace policy and separately permitted serving auth.
- [Auth selectors](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/core/src/config/auth_keyring.rs): native auth storage, backend, login, and routing selectors.
- [Configuration protocol](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/app-server-protocol/src/protocol/v2/config.rs): sensitive response shape, `ToolsV2` omission, managed constraints, and `SessionFlags` representation.
- [Configuration response](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/app-server/src/config_manager_service.rs): merged config, exact requirements, and origins without raw layers.
- [Exact requirements](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/config/src/config_requirements.rs): `apply_exact_to_config` does not replace the two utility leaves.
- [Origin order](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/config/src/state.rs) and [origin keys](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/config/src/fingerprint.rs): enabled layers from low to high and exact scalar-leaf origin replacement.
- [Runtime config](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/core/src/config/mod.rs): utility resolvers, catalog load, permission branches, instruction precedence, compact-file fallback, and managed MCP filtering.
- [Recursive merge](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/config/src/merge.rs): an empty MCP table preserves existing entries.
- [Override value parser](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/utils/cli/src/config_override.rs): the right-hand value supports native TOML inline tables.
- [Override layer](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/config/src/overrides.rs): paths split literally at dots; repeated root overrides replace the earlier root value.
- [Fresh MCP connections](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/codex-mcp/src/connection_manager.rs): enabled-only connection creation and `has_servers=false` for a disabled-only map.
- [Remote marker](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/app-server-transport/src/transport/remote_control/mod.rs) and [remote controller](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/app-server-transport/src/transport/remote_control/controller.rs): the marker selects `DisabledEphemeral` and disabled desired state.
- [Core router](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/core/src/tools/spec_plan.rs): core, model-selected, hosted, MCP resource, dynamic, and extension sources.
- [Installed extensions](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/app-server/src/extensions.rs): ordinary contributor inventory, not proof that disabled features remove registry roots.
- [Skill tools](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/ext/skills/src/tools/mod.rs), [skill inputs](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/ext/skills/src/extension.rs), and [history notes](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/ext/history-notes/src/extension.rs): cloud/executor and token-budget gates.
- [Plugin startup](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/app-server/src/message_processor.rs) and [plugin manager](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/core-plugins/src/manager.rs): effective plugin gates prevent activation and startup tasks under stable inputs.
- [Structured hooks](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/hooks/src/engine/mod.rs) and [legacy notify](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/hooks/src/registry.rs): disabled hooks need no plugin sources, while `notify=[]` closes a separate external command.

The release proof closes these registration paths:

| Source | Required gates and inputs |
| --- | --- |
| Shell and patch | False shell gate, disabled model shell, null patch metadata |
| Model-selected experimental tools | Empty `experimental_supported_tools` |
| Core utilities | Both false utility leaves and disabled permission, deferred-executor, token-budget, clock, sleep, and async-message gates |
| Agents and code | Disabled agents/code features, false `agents.enabled`, null model agent/tool modes, disabled Node REPL/search metadata |
| Hosted and extension search | Disabled web-search mode and standalone search feature |
| Core and extension images | Disabled view-image and image-generation gates |
| MCP tools and resources | Explicitly disabled entries, fresh empty connection set, and no `CodeModeOnly` resource exception |
| Ordinary goal, memories, agent board | Disabled goals, memories, agent-message-board, and multi-agent gates |
| Ordinary history notes | Disabled `token_budget` |
| Ordinary skills | Disabled cloud skills, absent executor query, and no selected capability roots |
| Plugins and apps | Effective plugin/app gates off; no extra MCP entries or plugin hook sources |
| Dynamic/runtime inputs | No dynamic tools, attachments, selected roots, or resumed session |
| Automatic reviewers | Never approval and user reviewer; guardian tools belong to isolated reviewer sessions |
| Other installed extensions | The reviewed ordinary registry has no additional tool contributor |

No full tool-schema export is required.
Unsupported releases or missing effective inputs cannot reuse this source proof.

### Native event evidence

Use the pinned `rust-v0.162.1` source for this invocation and decoder policy:

- [Feature-stage notice generator](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/features/src/lib.rs): `unstable_features_warning_event` returns no notice when the native option is true.
- [Startup notice wiring](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/core/src/session/session.rs): the session passes the effective option to that generator.
- [Configuration field](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/config/src/config_toml.rs): `suppress_unstable_features_warning` controls only the feature-stage notice.
- [JSONL event types](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/exec/src/exec_events.rs): error items are non-fatal; top-level errors are unrecoverable.
- [Notification mapping](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/exec/src/event_processor_with_jsonl_output.rs): warning, configuration-warning, and model-reroute notifications share error items. Fatal errors and failed turns retain distinct events.

### Verification

Run these commands only after the source change is complete:

```sh
make check PACKAGES=./internal/llm
make test PACKAGES='./internal/llm ./internal/llmloop ./internal/tool'
OCR_CODEX_SMOKE=1 OCR_CODEX_SMOKE_MODEL=gpt-6.1-sol \
  make test PACKAGES='./internal/llm -run=TestCodexRealSmoke'
```

The live exercise requires the installed Codex CLI, a compatible exported model, and existing CLI authentication.
A fixture process does not replace the real completion evidence.
Report platform limits and each unsupported-version failure without claiming unexercised behavior.

## Execution evidence

### Unblocked 2026-10-12T11:46:37.346598Z

- Cleared condition: The inspected codex-cli 0.162.1 lacks an established supported native route that preserves complete authentication selectors without exporting secret values and proves effective required tool gates under the completion isolated loader and policy context before any model request. Release requires a supported route with source-backed equivalence for authentication bootstrap, managed requirements, dependency normalization, model metadata, and final completion controls. The inspected features-list default loader, incomplete doctor selectors, potentially secret config/read and configRequirements/read responses, and prompt-input route do not satisfy this prerequisite. F1 and F2 remain unresolved. This condition concerns the inspected release; it does not claim that every Codex release lacks support.
- Evidence: The approved Design at 2026-10-12T11:35:36Z supersedes the ignored-user-config and no-sensitive-response prerequisite. It permits bounded local-memory native config/read and configRequirements/read receipt while retaining native authentication and provider routing. Pinned rust-v0.162.1 source and the architect handoff establish a fixed two-pass route: actual managed/normalized feature checks before fresh local configuration processes, native merged scalar/requirement checks, per-ID MCP disablement, exact SessionFlags origins for the two omitted utility leaves, controlled catalog/instruction selectors, disabled preflight remote control, and the complete ordinary registration gate map. The approved stable-input condition addresses the separate native reloads without an atomic-handoff claim. Core config source also proves the compact-file fallback; the Plan closes it with the nonempty shared compact prompt and controlled compact-file path. No external prerequisite remains for implementation under these approved boundaries. F1/F2 remain defects in the preserved candidate until implementation and independent verification clear them; no real retained-config authentication or completion evidence is claimed.

### Completion

#### Delivery

Task 001/003 implements the standalone Codex completion adapter through LLMClient.CompletionsWithCtx.
The implementation follows the repaired Task Plan and the approved Design of 2026-10-12T11:35:36Z.
The approved Task contract and Plan remain unchanged during this execution run.

Repository: /home/oleksii/Projects/personal/open-code-review
Branch: main
Base commit: 8e8e3d1966d8673357c24acd7fe3d410a80cd16c
Source commit: da0dd55c6accc66a9aa8ab43ac793919f1913ce9
Commit subject: feat(llm): add Codex completion adapter

The source commit contains six Task paths under internal/llm.
It adds codex_client.go, codex_config.go, codex_client_test.go, codex_config_test.go, and codex_smoke_test.go.
It updates CLI_COMPLETIONS.md.
It contains no Specs files or unrelated repository changes.

#### Verification commands

- make license-add: passes in the preceding candidate implementation run; all new source files have SPDX headers.
- Initial make check PACKAGES=./internal/llm: fails on a pointer mismatch in the extracted TOML string writer.
  The writer uses its existing *strings.Builder directly in fmt.Fprintf.
- The initial opt-in smoke command also stops at that compile error before any native request.
- Subsequent make check PACKAGES=./internal/llm: passes.
- make test PACKAGES='./internal/llm ./internal/llmloop ./internal/tool': passes under the race detector.
- Initial executable retained-config smoke: native authentication and completion succeed, but the model returns OCR_TEXT_OK. with a period.
  The original request contains a final period after the requested token.
  The smoke prompt explicitly requests no punctuation; the expected output remains OCR_TEXT_OK.
- OCR_CODEX_SMOKE=1 OCR_CODEX_SMOKE_MODEL=gpt-6.1-sol make test PACKAGES='./internal/llm -run=TestCodexRealSmoke': passes after the prompt clarification.
- The repository-required ocr review --audience agent --background command completes with four findings across the two selected adapter source files.
  Its background describes Task 001/003, the native authentication boundary, two-pass controls, transcript preservation, cleanup, and the approved race limit.
  Review session: ca5e26b2-1b78-496b-8050-bd76b4317c9b.
- go doc os/exec.CommandContext: confirms that CommandContext initializes Cmd.Cancel to Process.Kill.
- Final make check PACKAGES=./internal/llm && make test PACKAGES='./internal/llm ./internal/llmloop ./internal/tool': passes after the token-overflow fix.
  License and English checks pass.
  go mod tidy and gofmt complete.
  Focused go vet passes.
  Race-test package results: internal/llm 13.131 seconds; internal/llmloop 1.840 seconds; internal/tool 2.007 seconds.
- Final OCR_CODEX_SMOKE=1 OCR_CODEX_SMOKE_MODEL=gpt-6.1-sol make test PACKAGES='./internal/llm -run=TestCodexRealSmoke': passes after the token-overflow fix.
  The exercise takes 23.24 seconds; the race-test package result takes 24.258 seconds.
- Scoped git add --renormalize and git diff --cached --check: pass before the source commit.
  Only the six Task source paths enter the commit.

The ordinary package runs skip the opt-in native smoke test.
The separate opt-in command provides real completion evidence.
No independent Specs verifier gate applies to this exact Task-mode run.

#### Acceptance evidence

1. Real text-only completion: verified.
   TestCodexRealSmoke invokes /home/oleksii/.local/share/mise/installs/node/22/bin/codex through NewCodexClient.
   The final response contains OCR_TEXT_OK and no OCR tool calls.

2. Real two-turn OCR tool exercise: verified.
   The first turn requests file_read with arguments {"file_path":"fixture.txt"}.
   The existing Registry executes FileReadProvider once outside the native working directory.
   The second turn returns the exact random fixture value 9be68661c4e93d5a96925f2fcf1e7e8c.
   The initial request does not contain that value.

3. Call identifiers and no native review-tool events: verified.
   The final exercise preserves call_fixture_1 in the assistant call and corresponding tool result.
   Both native responses pass the decoder that rejects native tool events.
   TestCodexEvents and TestCodexResidualConditions reject native tool items, including items after valid assistant output.
   The restricted catalog and effective configuration checks provide the approved native registration proof.

4. Complete transcript: verified.
   CompletionsWithCtx reuses cliCompletionPrompt without a separate transcript format.
   TestCLICompletionTranscript passes for messages, content blocks, tools, previous calls, result identifiers, tool choice, and request limits.
   The real second turn also consumes the preserved assistant-call and tool-result relationship.

5. Resolved model and native CLI authentication: verified.
   All three final real completions use gpt-6.1-sol with no OCR APIKey.
   The adapter retains native configuration, CODEX_HOME, provider routing, authentication storage, backend selection, and policy selectors.
   It adds no credential helper and reads no auth.json or keyring credentials.
   TestCodexConfigurationContext checks the retained file, keyring, auto, and ephemeral storage selectors through synthetic native responses.
   These fixtures do not claim a real keyring exercise.

6. Catalog preservation and capability restrictions: verified.
   restrictCodexCatalog preserves the native envelope, the exact selected slug, and unrelated model metadata.
   It changes only the seven approved capability fields.
   TestCodexCatalogPreservation checks semantic preservation and the required replacements.
   Real completion also consumes the restricted catalog exported from the installed CLI.

7. Missing metadata and unsupported controls: verified.
   Catalog regressions reject absent models, missing fields, duplicate selected models, incompatible booleans, and a null tools array.
   Compatibility regressions reject an unsupported 0.162.2 release and incomplete native feature output.
   TestCodexEffectiveFeatures rejects missing, duplicate, malformed, or conflicting effective feature rows.
   The adapter invents no catalog metadata and weakens no required control.

8. Isolation, read-only sandbox, and preserved authentication: verified.
   Both real preflights validate effective controls and managed requirements before exec.
   The final controls explicitly disable every discovered MCP server through one aggregate native inline table.
   Fixture completion checks all four identifiers, explicit false values, and unchanged transport fields.
   TestCodexNativeConfigurationConflicts rejects managed conflicts, named permissions, hooks, notifications, and unsafe instruction selectors.
   TestCodexUtilityProvenance checks the exact session origin of each omitted utility leaf.
   TestCodexMissingManagedRequirements rejects an absent or incompatible requirements response.
   The completion uses the reviewed legacy read-only route and succeeds with native authentication.

9. Successful terminal event before final output: verified.
   parseCodexResult accepts structured assistant output only after turn.completed.
   runCodexCommand also requires a successful process exit.
   TestCodexEvents rejects missing terminal events and output after terminal completion.

10. Provider failures: verified.
    TestCodexCompatibilityAndProcessFailures covers missing commands, nonzero native exits, failed catalog export, malformed output, and inherited pipes.
    Native authentication failures use the same nonzero-exit provider error path; the adapter does not handle credentials itself.
    Event regressions reject failed turns, error events, native tool events, malformed streams, and incomplete streams.
    Sensitive protocol and stderr fixtures do not enter provider errors.

11. Actual usage and cache fields: verified.
    Final text usage: PromptTokens=1011, CompletionTokens=51, TotalTokens=1062, CacheReadTokens=0, CacheWriteTokens=0.
    Final tool-request usage: PromptTokens=1096, CompletionTokens=41, TotalTokens=1137, CacheReadTokens=0, CacheWriteTokens=0.
    Final continuation usage: PromptTokens=1209, CompletionTokens=41, TotalTokens=1250, CacheReadTokens=0, CacheWriteTokens=0.
    TestCodexEvents checks reported nonzero cache-read and cache-write counts without duplicate prompt accounting.
    Absent usage remains absent.
    TestCodexTokenAccountingBoundary accepts the largest representable total and rejects overflow through the complete client process path.

12. Request limits and accurate cap documentation: verified.
    The shared transcript retains MaxTokens and other request limits.
    CLI_COMPLETIONS.md identifies the Codex output-token limit as prompt guidance, not a native enforced cap.
    The implementation does not add a response-size rejection based on that prompt limit.

13. Cancellation, deadlines, bounded waits, and cleanup: verified.
    TestCodexCancellationCleanup covers version, catalog, feature, preflight, and completion phases.
    Each phase checks cancellation, caller deadlines, and configured request timeouts.
    These paths remain within the three-second bound and leave no temporary entries.
    The inherited-pipe regression returns in about one second.
    Failed commands also stop their owned process group on Unix.
    Every real completion checks that its request temporary root is empty.

14. Focused regression coverage: verified.
    Catalog, feature, configuration, provenance, protocol-limit, event, native-tool, compatibility, and cancellation regressions pass.
    The subprocess fixture uses the generated native inline-table grammar and last-root-wins semantics.
    It no longer treats quoted dotted override keys as complete MCP identifiers.

15. Existing Claude and API behavior: verified.
    No existing Claude, API client, provider factory, resolver, registry, IDE, or orchestration source changes.
    All existing internal/llm tests pass.
    The existing internal/llmloop and internal/tool package tests also pass.

#### Review disposition

- The two Windows nil-Cancel findings are invalid.
  Both adapter command paths use exec.CommandContext, which assigns Cmd.Cancel on every platform.
  internal/llm/cli_process_windows.go preserves that assignment.
- The token-total overflow finding receives a fix and a consumer-visible boundary regression.
  The decoder rejects a sum that exceeds math.MaxInt64 before it constructs UsageInfo.
- The proposed enforced MaxTokens behavior contradicts the approved Design and Task acceptance.
  The Design explicitly permits Codex prompt guidance and excludes an enforced native output-token cap claim.
  The implementation keeps that approved behavior.

#### Deviations and limits

- The TOML encoder uses one shared builder writer so aggregate MCP encoding does not allocate a separate quoted string for every identifier.
- Native requirements must explicitly contain the requirements field; null means no requirements.
- The context digest includes relevant non-secret origin types as well as selectors, safety values, and managed requirements.
- Failed native commands stop their owned process group before the request scratch directory is removed.
- The smoke prompt removes an ambiguous final period without relaxing its exact-output assertion.
- The repository requires its pre-commit OCR review, so this run performs that check before the commit.
  It adds no independent-review product workflow or orchestration change.

These choices stay within the approved Task Scope and Design.
Windows process controls do not receive a runtime exercise.
The approved stable-configuration condition still applies from the first load through exec and thread startup.
Native reloads provide no atomic handoff; the implementation claims no race-free isolation.
No known Task defects remain from the exercised Unix path.

#### Filesystem and remaining Spec scope

The current checkout and branch remain unchanged.
The source commit contains only the six Task paths.
The Specs root remains outside the source commit.
Only helper-owned Task lifecycle and execution-evidence fields change at completion.
The approved contract, Plan, Design, and completed dependency evidence remain intact.
Task 001/004 owns provider selection, factory, resolver, setup, and graphical integration.
Those interfaces do not select the standalone Codex adapter yet.
The Spec remains active.
