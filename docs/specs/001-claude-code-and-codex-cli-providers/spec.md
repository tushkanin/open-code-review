---
schema_version: 13
title: "Claude Code and Codex CLI providers"
status: "completed"
status_reason: null
created_at: "2026-10-12"
updated_at: "2026-10-13T06:17:51Z"
approved_at: "2026-10-12T05:46:22Z"
---

## Problem

Users cannot select `claude -p` or `codex exec` as providers in `ocr`. The existing provider factory creates API clients, not CLI clients.

## Outcome

Users can select either CLI as an additional provider and run the existing `ocr` workflow with CLI authentication.

Source: [SRC-001](sources.md#src-001-provider-behavior-clarification), [SRC-002](sources.md#src-002-final-clarification-confirmation).

## Scope

### Included

- Claude Code and Codex as built-in providers.
- Existing provider selection, configuration, model selection, and connection checks.
- Command-line, terminal setup, and IDE provider interfaces.
- CLI requests and responses within the existing provider contract.

### Excluded

- Independent reviews controlled by the CLIs.
- A separate review mode.
- New credential management.
- Custom executable paths or command arguments.
- CLI-specific review controls.

## Requirements

### PROVIDER-01: Provider selection

`ocr` MUST expose Claude Code and Codex as distinct built-in choices in every existing provider selection interface.

Source: [SRC-003](sources.md#src-003-built-in-provider-catalog), [SRC-008](sources.md#src-008-graphical-provider-selection).

### PROVIDER-02: CLI invocation

The Claude Code provider MUST invoke `claude -p`, and the Codex provider MUST invoke `codex exec`, through executables found on `PATH`.

### PROVIDER-03: Existing orchestration

Both providers MUST use the existing `ocr` orchestration for task groups, review scope, prompts, rules, and completion.

Source: [SRC-006](sources.md#src-006-review-conversation-and-tool-execution).

### PROVIDER-04: Request context

Both providers MUST preserve the supplied conversation and tool definitions across completion requests, including assistant tool calls and their results.

### PROVIDER-05: Tool execution

`ocr` MUST execute requested tools through its existing tool system, rather than letting either CLI independently execute review tools.

Examples:

- A requested `file_read` returns through `ocr`. The CLI does not replace it with its native file tool.

### PROVIDER-06: Response compatibility

Both providers MUST return responses through the existing provider response contract, including assistant text and tool-call identifiers, names, and arguments.

Source: [SRC-004](sources.md#src-004-provider-request-and-response-contract).

### PROVIDER-07: CLI authentication

Both providers MUST use authentication managed by their CLI without requiring an API key or credential helper in `ocr`.

Source: [SRC-007](sources.md#src-007-provider-setup-and-connection-checks).

### PROVIDER-08: Model selection

Both providers MUST pass the model resolved through existing `ocr` configuration and flags to the CLI without a separate selection mechanism.

Source: [SRC-005](sources.md#src-005-provider-and-model-resolution).

### PROVIDER-09: Request lifecycle

Both providers MUST honor cancellation and applicable timeouts from the existing request lifecycle.

### PROVIDER-10: Provider failures

Both providers MUST report command, authentication, execution, and response-decoding failures through the existing provider error path.

### PROVIDER-11: Existing behavior

The addition MUST preserve existing provider behavior and existing review result formats.
