# Sources

### SRC-001: Provider behavior clarification

- Kind: user-statement
- Origin: user-provided
- Location: conversation
- Provided or retrieved: 2026-10-12
- Statement: "CLIs should simply be additional providers. Their operation and invocation should work the same way as with other providers."

### SRC-002: Final clarification confirmation

- Kind: user-statement
- Origin: user-provided
- Location: conversation
- Provided or retrieved: 2026-10-12
- Statement: "confirm"

### SRC-003: Built-in provider catalog

- Kind: repository-file
- Origin: discovered
- Location: internal/llm/providers.go
- Provided or retrieved: 2026-10-12
- Version or revision: e96c79d4d64195230d8af1d0aabe7031d0cf901f
- Relevant locations: Provider, registry, ListProviders, and generated frontend and Kotlin catalogs.

### SRC-004: Provider request and response contract

- Kind: repository-file
- Origin: discovered
- Location: internal/llm/client.go
- Provided or retrieved: 2026-10-12
- Version or revision: e96c79d4d64195230d8af1d0aabe7031d0cf901f
- Relevant locations: LLMClient, Message, ToolCall, ChatResponse, NewLLMClient, and request timeout configuration.

### SRC-005: Provider and model resolution

- Kind: repository-file
- Origin: discovered
- Location: internal/llm/resolver.go
- Provided or retrieved: 2026-10-12
- Version or revision: e96c79d4d64195230d8af1d0aabe7031d0cf901f
- Relevant locations: ResolveEndpointWithOptions and preset authentication resolution.

### SRC-006: Review conversation and tool execution

- Kind: repository-file
- Origin: discovered
- Location: internal/llmloop/loop.go
- Provided or retrieved: 2026-10-12
- Version or revision: e96c79d4d64195230d8af1d0aabe7031d0cf901f
- Relevant locations: Completion requests, provider errors, response tool calls, and subsequent conversation messages.

### SRC-007: Provider setup and connection checks

- Kind: repository-file
- Origin: discovered
- Location: cmd/opencodereview/provider_cmd.go
- Provided or retrieved: 2026-10-12
- Version or revision: e96c79d4d64195230d8af1d0aabe7031d0cf901f
- Relevant locations: checkAPIKeyRequirement, applyOfficialProviderConfig, and runLLMTestPath calls.

### SRC-008: Graphical provider selection

- Kind: repository-file
- Origin: discovered
- Location: extensions/frontend/src/webview/views/ConfigView.tsx
- Provided or retrieved: 2026-10-12
- Version or revision: e96c79d4d64195230d8af1d0aabe7031d0cf901f
- Relevant locations: OfficialForm, provider choices, model selection, and ambient authentication controls.
