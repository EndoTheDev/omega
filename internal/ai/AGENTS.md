# internal/ai

## Purpose

The ai layer abstracts LLM provider APIs behind a single streaming
interface. It defines the message types, stream event types, and tool
schema that the agent layer consumes, and implements concrete providers
for Ollama, OpenAI, and Anthropic. Each provider translates internal
messages into its API format, streams responses back as typed events on
a channel, and retries transient HTTP failures with exponential backoff.

## Ownership

- `provider.go` - Provider interface, NewProvider factory, ToolSchema type, sseData SSE line reader, shared httpClient with SetHTTPTimeout
- `messages.go` - Message sealed interface; System, User (with optional Images), Assistant, ToolResult, ModelChange, ThinkingLevelChange concrete types; ImageContent struct; timestamp helpers
- `events.go` - StreamEvent sealed interface; ThinkingChunk, ResponseChunk, ToolCallEvent, StreamEnd concrete types; ToolCall struct
- `retry.go` - retryHTTP with exponential backoff and jitter, retryableStatus, maxRetries (OMEGA_MAX_RETRIES env)
- `ollama.go` - OllamaProvider: message conversion, streaming chat, Bearer auth for Ollama Cloud
- `openai.go` - OpenAIProvider: message conversion, tool-call fragment reassembly, SSE streaming
- `anthropic.go` - AnthropicProvider: system prompt lift, tool_result block folding, content_block streaming
- `fake_provider.go` - FakeProvider for deterministic agent loop tests; scripted or per-call scripts
- `*_test.go` - self-check tests for provider factory, retry, each provider, and FakeProvider

## Local Contracts

- **Errors are stream events, not Go errors.** Provider failures are
  encoded as StreamEnd with FinishReason="error" and Error set. The
  channel is always closed; callers never receive a Go error from Stream.
- **`model_name` everywhere.** Provider constructors and ModelName use
  `modelName`, never `model` or `providerModel`.
- **Messages and events are sealed interfaces.** Consumers dispatch via
  type switch on concrete types. New message or event types implement the
  marker method (`isMessage` / `isStreamEvent`).
- **Each provider owns its message conversion.** `messagesToAPI` maps
  internal Message types to the provider-specific API shape. No shared
  conversion layer; providers differ in role naming, tool format,
  system prompt handling, and image content serialization (Ollama
  `images` array, OpenAI `image_url` blocks, Anthropic `image` source).
- **Retry is transparent to providers.** All providers route HTTP
  requests through retryHTTP. 429 and 5xx are retried with backoff;
  other 4xx and context cancellation return immediately.
- **No re-exports.** Types defined here are imported from `internal/ai`
  by the agent and gateway layers.

## Work Guidance

- Add a new provider by implementing the Provider interface (Stream and
  ModelName), registering it in NewProvider's switch, and adding a
  constructor that defaults baseURL and apiKey from env vars.
- OpenAI and Anthropic tool-call arguments arrive as fragmented deltas
  keyed by index. Accumulate into a pending map and flush in index order
  after the stream ends. See openai.go and anthropic.go for the pattern.
- Anthropic requires consecutive ToolResult messages folded into a single
  user message of tool_result content blocks. The folding logic lives in
  AnthropicProvider.messagesToAPI; do not pre-fold in the agent layer.
- OpenAI has no thinking field; Assistant.Thinking is dropped in
  OpenAIProvider.messagesToAPI. Ollama and Anthropic preserve it.
- emitError is shared across all providers; use it for any stream error
  to maintain the StreamEnd(error) contract.
- FakeProvider supports both single-script and per-call-script modes.
  Use NewFakeProviderScripts for multi-turn agent loop tests where each
  turn needs different events.
- The Ollama live stream test skips unless OLLAMA_HOST and OLLAMA_MODEL
  are set. OpenAI and Anthropic tests use httptest servers with canned
  SSE; they run without network or API keys.

## Verification

```bash
go test ./internal/ai/      # unit tests (Ollama live test skips without env)
go build ./...              # everything compiles
go vet ./...                # no suspicious constructs
```

## Child DOX Index

No sub-packages.
