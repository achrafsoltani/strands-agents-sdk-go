# Strands Agents SDK for Go

A Go port of the [Strands Agents SDK](https://github.com/strands-agents) — an open-source, model-driven framework for building AI agents.

Rather than encoding decision trees, you define **tools** and a **system prompt**; the LLM itself drives the execution loop, deciding which tools to call and when to stop.

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"
    "strings"

    strands "github.com/Dr-H-PhD/strands-agents-sdk-go"
    "github.com/Dr-H-PhD/strands-agents-sdk-go/provider/anthropic"
)

func main() {
    agent := strands.NewAgent(
        strands.WithModel(anthropic.New()),
        strands.WithTools(
            strands.NewFuncTool("word_count", "Count words in text",
                func(_ context.Context, input map[string]any) (any, error) {
                    text, _ := input["text"].(string)
                    return len(strings.Fields(text)), nil
                },
                map[string]any{
                    "type": "object",
                    "properties": map[string]any{
                        "text": map[string]any{"type": "string", "description": "Text to count"},
                    },
                    "required": []string{"text"},
                },
            ),
        ),
        strands.WithSystemPrompt("You are a helpful assistant."),
    )

    result, err := agent.Invoke(context.Background(), "How many words in 'hello world'?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(result.Message.Text())
}
```

Set `ANTHROPIC_API_KEY` in your environment before running.

## Architecture

The SDK consists of seven subsystems:

| Subsystem | Description |
|-----------|-------------|
| **Agent** | Top-level orchestrator — holds model, tools, hooks, messages, and state |
| **Event Loop** | Core cycle: send to model → receive response → execute tools → loop |
| **Model** | `Model` interface with provider implementations (Anthropic, etc.) |
| **Tools** | `Tool` interface, `FuncTool` helper, `ToolRegistry` |
| **Hooks** | Event-driven lifecycle callbacks (before/after model, before/after tool) |
| **Executors** | Sequential and concurrent tool execution strategies |
| **Session** | Conversation management and persistence interfaces |

See [`architecture/`](architecture/) for detailed design documentation with diagrams.

## Design Principles

- **Model-driven** — The LLM decides the control flow; no explicit state machines
- **Zero dependencies** — Core SDK and Anthropic provider use only the standard library
- **Idiomatic Go** — Interfaces, channels, context, functional options
- **Streaming** — First-class streaming via channels (`agent.Stream()`)
- **Extensible** — Hook system for lifecycle events, pluggable model providers

## Package Structure

```
├── agent.go              # Agent struct, NewAgent(), Invoke(), Stream()
├── types.go              # Message, ContentBlock, ToolUse, ToolResult, Usage, etc.
├── model.go              # Model interface (Converse, ConverseStream)
├── tool.go               # Tool interface, FuncTool
├── hook.go               # Hook events and HookRegistry
├── registry.go           # ToolRegistry
├── executor.go           # SequentialExecutor, ConcurrentExecutor
├── event_loop.go         # Core execution loop
├── conversation.go       # ConversationManager (SlidingWindow, Null)
├── session.go            # SessionManager interface
├── errors.go             # Error types
├── provider/
│   └── anthropic/        # Anthropic Messages API (HTTP + SSE, zero deps)
└── examples/
    └── basic/            # Working example with tools
```

## Streaming

```go
for event := range agent.Stream(ctx, "What is 2 + 2?") {
    switch event.Type {
    case strands.EventTextDelta:
        fmt.Print(event.Text)
    case strands.EventToolStart:
        fmt.Printf("\n[calling %s...]\n", event.ToolName)
    case strands.EventComplete:
        fmt.Println("\nDone!")
    case strands.EventError:
        log.Fatal(event.Error)
    }
}
```

## Hooks

```go
agent.Hooks.OnBeforeToolCall(func(e *strands.BeforeToolCallEvent) {
    fmt.Printf("About to call tool: %s\n", e.ToolUse.Name)
})

agent.Hooks.OnAfterModelCall(func(e *strands.AfterModelCallEvent) {
    if e.Err != nil {
        e.Retry = true // retry on transient errors
    }
})
```

## Roadmap

- [ ] Bedrock, OpenAI, Gemini, Ollama providers
- [ ] MCP (Model Context Protocol) client integration
- [ ] Session persistence (file, S3, DynamoDB)
- [ ] Structured output enforcement (generics)
- [ ] Multi-agent patterns (Graph, Swarm, A2A)
- [ ] OpenTelemetry tracing
- [ ] Interrupt/resume (human-in-the-loop)
- [ ] Context overflow recovery with conversation managers

## Licence

Apache 2.0
