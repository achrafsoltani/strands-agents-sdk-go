# Model Providers

## The Model Interface

Every provider implements the `Model` interface:

```go
type Model interface {
    Converse(ctx context.Context, input *ConverseInput) (*ConverseOutput, error)
    ConverseStream(ctx context.Context, input *ConverseInput, handler StreamHandler) (*ConverseOutput, error)
}
```

### ConverseInput

```go
type ConverseInput struct {
    Messages     []Message   // Conversation history
    SystemPrompt string      // System instructions
    ToolSpecs    []ToolSpec  // Available tools (JSON Schema)
    ToolChoice   *ToolChoice // How the model should select tools
}
```

### ConverseOutput

```go
type ConverseOutput struct {
    StopReason StopReason // end_turn, tool_use, max_tokens
    Message    Message    // The assistant's response
    Usage      Usage      // Token counts
    Metrics    Metrics    // Latency
}
```

### StreamHandler

```go
type StreamHandler func(text string)
```

Called for each text delta during streaming. Pass `nil` for fire-and-forget streaming (the final output is still assembled and returned).

## Available Providers

| Provider | Package | Default Model | Streaming | Auth |
|----------|---------|---------------|-----------|------|
| [Anthropic](anthropic.md) | `provider/anthropic` | `claude-sonnet-4-20250514` | SSE | API key |
| [Bedrock](bedrock.md) | `provider/bedrock` | `anthropic.claude-3-5-sonnet-20241022-v2:0` | Binary event stream | SigV4 |

### Planned

- **OpenAI-compatible** — GPT models, Groq, Together, any OpenAI-compatible API
- **Ollama** — Local models via Ollama

## Converse vs ConverseStream

The agent's event loop always calls `ConverseStream()`, even for synchronous `Invoke()` calls. This ensures consistent behaviour and allows hooks to operate on streaming events.

`Converse()` exists for use cases where streaming is not needed (batch processing, testing, simple scripts). Both methods return the same `ConverseOutput`.

## Writing a Custom Provider

Implement the `Model` interface:

```go
package myprovider

import (
    "context"
    strands "github.com/achrafsoltani/strands-agents-sdk-go"
)

type Provider struct {
    // your config
}

func (p *Provider) Converse(ctx context.Context, input *strands.ConverseInput) (*strands.ConverseOutput, error) {
    // 1. Convert input.Messages, input.ToolSpecs to your API format
    // 2. Send request to your LLM API
    // 3. Parse response into ConverseOutput
    // 4. Map stop reason: "end_turn", "tool_use", "max_tokens"
    return output, nil
}

func (p *Provider) ConverseStream(ctx context.Context, input *strands.ConverseInput, handler strands.StreamHandler) (*strands.ConverseOutput, error) {
    // Same as Converse, but:
    // - Use streaming API
    // - Call handler(text) for each text delta
    // - Assemble the full response as you go
    // - Return the complete ConverseOutput at the end
    return output, nil
}
```

### Key Requirements

1. **Map stop reasons correctly** — `end_turn` means the model is done, `tool_use` means it wants to call tools, `max_tokens` means truncated
2. **Populate tool use blocks** — When `StopReason` is `tool_use`, the `Message.Content` must contain `ContentTypeToolUse` blocks with ID, name, and parsed input
3. **Handle tool results** — Your message converter must handle `ContentTypeToolResult` blocks in the input messages
4. **Call the StreamHandler** — In `ConverseStream`, call `handler(text)` for each incremental text chunk (check for nil first)
5. **Respect context cancellation** — Pass `ctx` to your HTTP client
