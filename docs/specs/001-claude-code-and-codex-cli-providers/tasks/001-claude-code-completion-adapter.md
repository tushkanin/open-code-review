---
schema_version: 13
title: "Claude Code completion adapter"
type: delivery
status: "completed"
order: 10
requirements: [PROVIDER-02, PROVIDER-04, PROVIDER-05, PROVIDER-06, PROVIDER-07, PROVIDER-08, PROVIDER-09, PROVIDER-10, PROVIDER-11]
created_at: "2026-10-12"
updated_at: "2026-10-12T07:03:43.745568Z"
approved_at: "2026-10-12T06:34:59Z"
---

## Outcome

A complete Claude Code client implements the existing `LLMClient` contract through `claude -p`, with `ocr` retaining tool execution.

## Scope

- The shared completion format and response schema from the Design.
- Request context, including prior tool calls and results.
- The Claude command invocation and result parser.
- Native tool restrictions and CLI authentication.
- Model selection, applicable output limits, and actual token usage.
- Cancellation, deadlines, bounded process cleanup, and temporary-file cleanup.
- Focused regression coverage and documentation for this adapter.

### Excluded

- Codex invocation and model catalog handling.
- Provider registry, resolver, setup, or graphical provider selection changes.
- IDE connection-test environment changes.
- Changes to the existing review orchestration.

## Acceptance

- A real Claude invocation through the client returns a text-only completion with no OCR tool calls.
- A real two-turn exercise returns an OCR tool request, receives its result, and uses that result in the next completion.
- The exercise preserves tool-call identifiers and does not execute native review tools.
- A request with previous assistant calls and corresponding results retains their relationships.
- The client uses the selected model and existing CLI authentication without an `ocr` API key.
- Actual CLI usage reaches the existing usage fields, including reported cache counts.
- Missing commands, failed CLI results, and malformed completion output return provider errors rather than successful empty responses.
- Cancellation and deadlines return promptly without a blocked pipe wait or retained temporary files.
- Focused regression tests cover malformed output and cancellation cleanup.
- Existing API clients and their behavior remain unchanged.

## Execution evidence

### Completion

#### Delivery

Task 001/001 implements the Claude Code completion adapter through the existing LLMClient contract.
The Task has no Plan section.
The approved Task body and Design remain unchanged.

Repository: /home/oleksii/Projects/personal/open-code-review
Branch: main
Base commit: e96c79d4d64195230d8af1d0aabe7031d0cf901f
Source commit: 6cbef414c243a1122983cb29c52b81c622cb3aae
Commit subject: feat(llm): add Claude Code completion adapter

Verifier: pass after 0 fix rounds.
The independent verifier reports no findings at agent://VerifyClaude001.
Its read-only inspection covers all seven Task files, the approved contract, the relevant Design, and referenced Requirements.
The verifier does not execute checks or live completions.
The implementation run supplies the command evidence below.
No source changes occur after the verifier pass.

#### Verification commands

- `claude --help`: pass. The installed CLI supports the approved controls.
- `make license-add`: pass. All source files have valid SPDX license headers.
- Initial `make check PACKAGES=./internal/llm`: fails because the smoke test creates a transitive test import cycle.
  The smoke test uses external package llm_test to remove that cycle.
- Next `make check PACKAGES=./internal/llm`: pass.
- Initial `make test PACKAGES='./internal/llm ./internal/llmloop ./internal/tool'`: fails on a JSON byte-order assertion.
  The regression compares decoded JSON values to preserve semantic equality.
  Existing llmloop and tool tests pass in that run.
- Initial `OCR_CLAUDE_SMOKE=1 OCR_CLAUDE_SMOKE_MODEL=sonnet make test PACKAGES='./internal/llm -run=TestClaudeCodeRealSmoke'`: real text completion succeeds.
  The cleanup assertion detects Claude scratch files outside the request directory.
  The client assigns child TMPDIR, TMP, and TEMP to the request directory.
- Final `OCR_CLAUDE_SMOKE=1 OCR_CLAUDE_SMOKE_MODEL=sonnet make test PACKAGES='./internal/llm -run=TestClaudeCodeRealSmoke'`: pass under the race detector.
  The real smoke test takes 9.74 seconds.
- Final `make check PACKAGES=./internal/llm && make test PACKAGES='./internal/llm ./internal/llmloop ./internal/tool'`: pass.
  License and English checks pass.
  go mod tidy and gofmt complete without changes to existing tracked files.
  Focused go vet passes.
  Package tests pass under the race detector: llm 9.143 seconds; llmloop 1.675 seconds; tool 1.812 seconds.
- `git status --short --branch && git diff --stat && git diff --check`: pass before the commit.
  Only the seven new Task files and the existing untracked docs/specs/ appear.
- The final scoped context reports schema 13 with no errors or warnings before completion.
- The resumed Task run creates one source commit with only the seven approved Task paths.
  The resumed run does not repeat successful checks or invoke an external review workflow.

The final ordinary package run skips the opt-in live test.
The separate successful smoke command supplies the real completion evidence.

#### Acceptance evidence

1. Real text-only completion: verified.
   TestClaudeCodeRealSmoke executes /home/oleksii/.local/bin/claude through NewClaudeCodeClient.
   The selected model is sonnet.
   The response content is OCR_TEXT_OK and tool_calls is empty.

2. Real two-turn OCR tool exercise: verified.
   The first completion requests file_read with arguments {"file_path":"fixture.txt"}.
   The existing OCR Registry executes FileReadProvider once.
   The second completion returns the exact random fixture value c8ed4637a5a483fcc31f12f5d8f1f49b.
   The initial request does not contain that value.

3. Tool identifiers and native tool isolation: verified.
   The returned identifier is call_file_read_1.
   The next request uses it in the assistant call and corresponding tool result.
   The invocation disables native review tools with --tools ''.
   Strict empty MCP configuration and safe mode disable optional tool sources.
   TestClaudeCodeInvocation checks these command controls.
   OCR executes the file_read request through its existing registry.

4. Prior assistant calls and corresponding results: verified.
   TestCLICompletionTranscript verifies semantic JSON preservation of the full transcript.
   The test checks matching identifiers for the assistant call and tool result.
   The real second completion also consumes that relationship.

5. Selected model and CLI authentication without an OCR API key: verified.
   The real smoke client supplies Model=sonnet and no APIKey.
   All three real completions succeed through the installed CLI.
   TestClaudeCodeInvocation checks request-model precedence and the model argument.
   It also checks preservation of CLAUDE_CONFIG_DIR.

6. Actual usage, including cache counts: verified.
   Text-only usage: PromptTokens=2850, CompletionTokens=87, TotalTokens=2937, CacheReadTokens=1132, CacheWriteTokens=1716.
   Tool-request usage: PromptTokens=2994, CompletionTokens=122, TotalTokens=3116, CacheReadTokens=820, CacheWriteTokens=2172.
   Continuation usage: PromptTokens=3182, CompletionTokens=101, TotalTokens=3283, CacheReadTokens=820, CacheWriteTokens=2360.
   TestClaudeCodeResult checks exact field translation.
   Prompt accounting follows the existing Anthropic cache convention.

7. Provider errors for missing commands, failed results, and malformed output: verified.
   TestClaudeCodeProcessFailures covers missing executables, nonzero exits, failed results, malformed JSON, and inherited pipe failures.
   TestClaudeCodeMalformedResult covers missing fields, empty responses, invalid arguments, unavailable tools, duplicate identifiers, and tool choice.
   These paths return errors rather than successful empty responses.

8. Prompt cancellation, deadlines, pipe cleanup, and temporary cleanup: verified.
   TestClaudeCodeCancellationCleanup covers cancellation, caller deadlines, and configured request timeouts.
   Each case returns in about 0.10 seconds and leaves no temporary entries.
   The inherited-pipe regression returns in about 1.01 seconds.
   Every real smoke request also leaves the request temporary root empty.

9. Focused malformed-output and cancellation regression coverage: verified.
   TestClaudeCodeMalformedResult and TestClaudeCodeCancellationCleanup pass in the final race-test run.

10. Existing API behavior: verified.
    No existing API source files change.
    The complete internal/llm package tests pass, including existing API client tests.
    Existing llmloop and tool tests also pass.

#### Deviations

- Claude creates scratch files outside its working directory when it inherits TMPDIR.
  The client places child scratch directories inside the request cleanup boundary.
  Regression and real smoke cleanup then pass.
- The smoke test uses external package llm_test to avoid a transitive import cycle.
- The transcript regression uses semantic JSON equality instead of byte-order equality.

These choices remain inside the approved Scope and Design.

#### Not fixed

No known defects remain from this Task.
The integration host exercises Unix process controls.
Windows process controls do not receive a runtime exercise.

#### Filesystem and remaining scope

The source commit adds seven files under internal/llm.
It changes no existing tracked files.
The main docs/specs/ root remains untracked and uncommitted.
The helper owns all Task lifecycle and execution-evidence changes.
The approved Task contract, Design, and user files remain intact.

The shared completion helpers support the later Codex adapter Task.
Provider registry, resolver, IDE authentication roots, and orchestration integration remain outside this Task.
The Spec remains active.
