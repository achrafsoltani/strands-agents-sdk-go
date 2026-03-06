# Anthropic Provider

**Source**: `provider/anthropic/anthropic.go`

The Anthropic provider implements the Strands `Model` interface for the [Anthropic Messages API](https://docs.anthropic.com/en/api/messages). It uses only the Go standard library — `net/http` for requests and `bufio` for SSE streaming.

## Usage

```go
import "github.com/achrafsoltani/strands-agents-sdk-go/provider/anthropic"

model := anthropic.New()
// or with options:
model := anthropic.New(
    anthropic.WithAPIKey("sk-ant-..."),
    anthropic.WithModel("claude-sonnet-4-20250514"),
    anthropic.WithMaxTokens(8192),
    anthropic.WithBaseURL("https://custom-proxy.example.com"),
)
```

## Options

| Option | Default | Description |
|--------|---------|-------------|
| `WithAPIKey(key)` | `$ANTHROPIC_API_KEY` | API key for authentication |
| `WithModel(model)` | `claude-sonnet-4-20250514` | Model ID |
| `WithMaxTokens(n)` | `4096` | Maximum tokens in the response |
| `WithBaseURL(url)` | `https://api.anthropic.com` | API base URL (for proxies) |

## Authentication

The provider reads `ANTHROPIC_API_KEY` from the environment by default. The key is sent as the `x-api-key` header on every request.

## API Mapping

### Request Format

The provider translates Strands types to the Anthropic Messages API format:

| Strands Type | Anthropic API |
|-------------|---------------|
| `Message{Role: "user", Content: [TextBlock("hi")]}` | `{"role": "user", "content": [{"type": "text", "text": "hi"}]}` |
| `ToolUseBlock(id, name, input)` | `{"type": "tool_use", "id": "...", "name": "...", "input": {...}}` |
| `ToolResultBlock(result)` | `{"type": "tool_result", "tool_use_id": "...", "content": "..."}` |
| `ToolSpec{Name, Description, InputSchema}` | `{"name": "...", "description": "...", "input_schema": {...}}` |
| `SystemPrompt` | Top-level `"system"` field |

### Tool Choice

| Strands | Anthropic |
|---------|-----------|
| `ToolChoiceAuto` | `{"type": "auto"}` |
| `ToolChoiceAny` | `{"type": "any"}` |
| `ToolChoiceTool` | `{"type": "tool", "name": "..."}` |

### Stop Reasons

Anthropic returns these directly as strings, and they map 1:1 to Strands `StopReason` constants: `end_turn`, `tool_use`, `max_tokens`.

## Streaming (SSE)

### Protocol

The streaming endpoint returns Server-Sent Events (SSE). Each event is a pair of lines:

```
event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}
```

### Event Types

| SSE Event | Purpose |
|-----------|---------|
| `message_start` | Contains input token count |
| `content_block_start` | New text or tool_use block |
| `content_block_delta` | Incremental text (`text_delta`) or tool input JSON (`input_json_delta`) |
| `content_block_stop` | Block complete — finalise and store |
| `message_delta` | Stop reason and output token count |
| `message_stop` | End of stream |
| `error` | Stream error from the API |

### Tool Use Streaming

Tool inputs arrive as `input_json_delta` events — partial JSON strings that are concatenated and parsed on `content_block_stop`:

```
event: content_block_start
data: {..., "content_block": {"type": "tool_use", "id": "tu_1", "name": "calc"}}

event: content_block_delta
data: {..., "delta": {"type": "input_json_delta", "partial_json": "{\"expr\":"}}

event: content_block_delta
data: {..., "delta": {"type": "input_json_delta", "partial_json": "\"2+2\"}"}}

event: content_block_stop
data: {...}
```

The provider concatenates `{"expr":` + `"2+2"}` = `{"expr":"2+2"}` and unmarshals on stop.

## HTTP Details

- **Endpoint**: `POST {baseURL}/v1/messages`
- **Headers**: `Content-Type: application/json`, `x-api-key: {key}`, `anthropic-version: 2023-06-01`
- **Timeout**: 5 minutes (for long-running model calls)
- **Streaming**: Request includes `"stream": true`; non-streaming omits it

## Error Handling

Non-200 responses return an error with the status code and body:

```
anthropic: API error 401: {"error":{"message":"invalid api key"}}
```

Stream errors are detected via the `error` SSE event type.

## Testing

14 tests using `httptest.Server` to mock the Anthropic API:

- Non-streaming: text response, tool use, API error, request format, tool results
- Streaming: text, tool use, stream error, nil handler, stream flag
- Construction: defaults, options
- Helpers: `jsonInt`, `convertToolChoice`
