# ai

## Purpose

The ai layer defines the LLM provider contract (interface, message types,
stream event types, tool schema) and shared HTTP infrastructure. Concrete
provider implementations (Ollama, OpenAI, Anthropic) live in the
`bin/extensions/core-provider/` extension binary, not in this package.

## Ownership

- `provider.go` - Provider interface, ToolSchema type, SSEData SSE line
  reader, shared httpClient with SetHTTPTimeout, exported HTTPClient /
  RetryHTTP / SSEData for extension use, ThinkingLevels / ThinkingEnabled
- `extension_provider.go` - ExtensionProvider implementing Provider by
  delegating to a ProviderDispatcher interface (satisfied by agent.StdioManager)
- `messages.go` - Message sealed interface; System, User (with optional
  Images), Assistant, ToolResult, ModelChange, ThinkingLevelChange concrete
  types; ImageContent struct; timestamp helpers; `EncodeMessage`/
  `DecodeMessage` (role discriminator + JSON payload serialization,
  shared by gateway store, proxy store, and core-store extension)
- `events.go` - StreamEvent sealed interface; ThinkingChunk, ResponseChunk,
  ToolCallEvent, StreamEnd concrete types; ToolCall struct
- `retry.go` - retryHTTP with exponential backoff and jitter,
  retryableStatus, maxRetries (OMEGA_MAX_RETRIES env)
- `fake_provider.go` - FakeProvider for deterministic agent loop tests;
  scripted or per-call scripts

## Local Contracts

- **Errors are stream events, not Go errors.** Provider failures are
  encoded as StreamEnd with FinishReason="error" and Error set. The
  channel is always closed; callers never receive a Go error from Stream.
- **`model_name` everywhere.** ModelName uses `modelName`, never `model`
  or `providerModel`.
- **Messages and events are sealed interfaces.** Consumers dispatch via
  type switch on concrete types. New message or event types implement the
  marker method (`isMessage` / `isStreamEvent`).
- **Provider implementations are in the core-provider extension.**
  Message conversion, streaming, and API-specific logic live in
  `bin/extensions/core-provider/main.go`, not here. This package exports
  HTTPClient, RetryHTTP, and SSEData for the extension to import.
- **Retry is transparent to providers.** All HTTP requests route through
  retryHTTP (exported as RetryHTTP). 429 and 5xx are retried with backoff;
  other 4xx and context cancellation return immediately.
- **No re-exports.** Types defined here are imported from `ai`
  by the agent and gateway layers.

## Work Guidance

- The core-provider extension implements all three providers. To add a
  new provider, add a `streamX` function in the extension binary and a
  case in the `handleStream` switch.
- OpenAI and Anthropic tool-call arguments arrive as fragmented deltas
  keyed by index. Accumulate into a pending map and flush in index order
  after the stream ends.
- Anthropic requires consecutive ToolResult messages folded into a single
  user message of tool_result content blocks.
- FakeProvider supports both single-script and per-call-script modes.
  Use NewFakeProviderScripts for multi-turn agent loop tests where each
  turn needs different events.

## Verification

```bash
go test ./ai/              # unit tests
go build ./...             # everything compiles
go vet ./...               # no suspicious constructs
```

## Child DOX Index

No sub-packages.
