# CLI completion adapters

`NewClaudeCodeClient` implements `LLMClient` through the installed `claude` executable on `PATH`.
The adapter uses CLI-managed authentication.
It does not use `ClientConfig.APIKey`, `URL`, credential helpers, HTTP retries, or HTTP raw capture.
The provider registry and IDE interfaces do not select this adapter yet.

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
