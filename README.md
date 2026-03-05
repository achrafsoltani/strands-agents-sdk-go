# Strands Agents SDK for Go

A Go implementation of the [Strands Agents SDK](https://github.com/strands-agents) — a model-driven framework for building AI agents where the LLM controls the execution loop.

Rather than encoding decision trees or state machines, you define **tools** and a **system prompt**; the model decides which tools to call, in what order, and when to stop.

[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Zero Dependencies](https://img.shields.io/badge/dependencies-0-brightgreen)](#design-principles)
[![Tests](https://img.shields.io/badge/tests-79%20passing-brightgreen)](#testing)
[![Licence](https://img.shields.io/badge/licence-Apache%202.0-blue)](#licence)

---

## Features

- **Model-driven control flow** — the LLM decides what to do; no explicit orchestration code
- **Zero external dependencies** — entire SDK including the Anthropic provider uses only the Go standard library
- **First-class streaming** — channel-based streaming API with text deltas, tool events, and completion signals
- **Extensible hook system** — lifecycle callbacks for logging, guardrails, retry logic, and result modification
- **Concurrent tool execution** — parallel tool calls by default with sequential option available
- **Idiomatic Go** — interfaces, `context.Context`, channels, functional options pattern

## Quick Start

```bash
go get github.com/achrafsoltani/strands-agents-sdk-go
```

```go
package main

import (
    "context"
    "fmt"
    "log"
    "strings"

    strands "github.com/achrafsoltani/strands-agents-sdk-go"
    "github.com/achrafsoltani/strands-agents-sdk-go/provider/anthropic"
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

The SDK is built from seven cooperating subsystems:

| Subsystem | File(s) | Description |
|-----------|---------|-------------|
| **Agent** | `agent.go` | Top-level orchestrator — holds model, tools, hooks, messages, and state. Entry points: `Invoke()` (sync) and `Stream()` (channel-based) |
| **Event Loop** | `event_loop.go` | Iterative cycle: call model → route by stop reason → execute tools → loop. Supports retry via hooks, max cycle guard |
| **Model** | `model.go` | `Model` interface with `Converse` (sync) and `ConverseStream` (streaming) methods |
| **Tools** | `tool.go`, `registry.go` | `Tool` interface, `FuncTool` helper for wrapping functions, thread-safe `ToolRegistry` with name validation |
| **Hooks** | `hook.go` | 5 event types: BeforeModelCall, AfterModelCall, BeforeToolCall, AfterToolCall, MessageAdded. LIFO ordering for After* hooks |
| **Executors** | `executor.go` | `SequentialExecutor` and `ConcurrentExecutor` with hook integration and retry support |
| **Session** | `conversation.go`, `session.go` | `ConversationManager` (sliding window, null) and `SessionManager` interface for persistence |

See [`architecture/`](architecture/) for detailed design documentation with 8 SVG/PNG diagrams and a [complete PDF](architecture/pdf/strands-agents-architecture-complete.pdf).

### How the Event Loop Works

```
User prompt
    │
    ▼
┌─────────────────────────┐
│   Append user message   │
└───────────┬─────────────┘
            ▼
┌─────────────────────────┐
│   Call model (stream)   │◄──── BeforeModelCall hook
└───────────┬─────────────┘
            │                    AfterModelCall hook
            ▼
      ┌─────────────┐
      │ Stop reason? │
      └──────┬──────┘
             │
    ┌────────┼────────┐
    ▼        ▼        ▼
 end_turn  tool_use  max_tokens
    │        │        │
    │        ▼        ▼
    │   Execute     Error
    │   tools
    │     │
    │     ▼
    │   Append results
    │     │
    │     └──► next cycle
    ▼
  Return AgentResult
```

## Streaming

```go
for event := range agent.Stream(ctx, "What is 2 + 2?") {
    switch event.Type {
    case strands.EventTextDelta:
        fmt.Print(event.Text)
    case strands.EventToolStart:
        fmt.Printf("\n[calling %s...]\n", event.ToolName)
    case strands.EventToolEnd:
        fmt.Printf("[%s done]\n", event.ToolName)
    case strands.EventComplete:
        fmt.Printf("\n\nTokens used: %d input, %d output\n",
            event.Result.Usage.InputTokens, event.Result.Usage.OutputTokens)
    case strands.EventError:
        log.Fatal(event.Error)
    }
}
```

## Tools

Tools are defined with a name, description, handler function, and JSON Schema for the input:

```go
calculator := strands.NewFuncTool(
    "calculator",
    "Evaluate a mathematical expression",
    func(ctx context.Context, input map[string]any) (any, error) {
        expr, _ := input["expression"].(string)
        // ... evaluate expression
        return result, nil
    },
    map[string]any{
        "type": "object",
        "properties": map[string]any{
            "expression": map[string]any{
                "type":        "string",
                "description": "Mathematical expression to evaluate",
            },
        },
        "required": []string{"expression"},
    },
)

agent := strands.NewAgent(
    strands.WithModel(anthropic.New()),
    strands.WithTools(calculator),
)
```

## Hooks

The hook system provides lifecycle callbacks for cross-cutting concerns:

```go
// Logging
agent.Hooks.OnBeforeModelCall(func(e *strands.BeforeModelCallEvent) {
    log.Printf("Calling model with %d messages", len(e.Messages))
})

// Guardrails — block dangerous tools
agent.Hooks.OnBeforeToolCall(func(e *strands.BeforeToolCallEvent) {
    if e.ToolUse.Name == "delete_file" {
        e.Cancel = true
        e.CancelMsg = "file deletion is not permitted"
    }
})

// Automatic retry on transient errors
agent.Hooks.OnAfterModelCall(func(e *strands.AfterModelCallEvent) {
    if e.Err != nil {
        e.Retry = true
    }
})

// Post-process tool results
agent.Hooks.OnAfterToolCall(func(e *strands.AfterToolCallEvent) {
    log.Printf("Tool %s returned: %s", e.ToolUse.Name, e.Result.Content[0].Text)
})

// Track conversation growth
agent.Hooks.OnMessageAdded(func(e *strands.MessageAddedEvent) {
    log.Printf("Message added: role=%s", e.Message.Role)
})
```

## Configuration

```go
agent := strands.NewAgent(
    // Model provider (required)
    strands.WithModel(anthropic.New(
        anthropic.WithAPIKey("sk-..."),
        anthropic.WithModel("claude-sonnet-4-20250514"),
        anthropic.WithMaxTokens(8192),
    )),

    // Tools
    strands.WithTools(tool1, tool2, tool3),

    // System prompt
    strands.WithSystemPrompt("You are a code review assistant."),

    // Max event loop cycles per invocation (default: 20)
    strands.WithMaxCycles(10),

    // Sequential tool execution instead of concurrent (default)
    strands.WithSequentialExecution(),

    // Custom conversation manager
    strands.WithConversationManager(&strands.SlidingWindowManager{WindowSize: 50}),

    // Initial agent state (accessible in hooks and returned in results)
    strands.WithState(map[string]any{"user_id": "123"}),
)
```

## Project Structure

```
strands-agents-sdk-go/
├── go.mod                    # Zero dependencies, Go 1.23+
├── agent.go                  # Agent struct, NewAgent(), Invoke(), Stream()
├── event_loop.go             # Core execution loop with retry support
├── types.go                  # Message, ContentBlock, ToolUse, ToolResult, Usage, Event
├── model.go                  # Model interface (Converse, ConverseStream)
├── tool.go                   # Tool interface, FuncTool
├── registry.go               # Thread-safe ToolRegistry with name validation
├── hook.go                   # HookRegistry with 5 event types
├── executor.go               # Sequential and Concurrent executors
├── conversation.go           # ConversationManager (SlidingWindow, Null)
├── session.go                # SessionManager interface (persistence stub)
├── errors.go                 # Sentinel errors
├── provider/
│   └── anthropic/
│       ├── anthropic.go      # Anthropic Messages API (HTTP + SSE streaming)
│       └── anthropic_test.go # 14 tests with mock HTTP server
├── examples/
│   └── basic/
│       └── main.go           # Working example with word_count and calculator
├── architecture/             # Design documentation
│   ├── *.md                  # 9 detailed architecture documents
│   ├── diagrams/             # 8 SVG + 8 PNG architecture diagrams
│   └── pdf/                  # Compiled PDFs including complete document
├── *_test.go                 # 10 test files, 79 tests total
└── README.md
```

## Testing

The SDK has comprehensive test coverage across all subsystems:

```bash
go test ./...
```

```
ok   strands-agents-sdk-go                   0.054s   # 65 tests
ok   strands-agents-sdk-go/provider/anthropic 0.007s   # 14 tests
```

**79 tests** covering:

| Test File | Tests | Coverage |
|-----------|-------|----------|
| `types_test.go` | 9 | Message accessors, constructors, Usage arithmetic |
| `tool_test.go` | 5 | FuncTool spec, execution, error handling, type coercion |
| `registry_test.go` | 7 | Registration, lookup, validation, duplicates |
| `hook_test.go` | 7 | All 5 event types, LIFO ordering, cancel/retry flags |
| `executor_test.go` | 7 | Sequential/concurrent execution, parallelism verification, hooks |
| `conversation_test.go` | 4 | Sliding window trimming, edge cases |
| `event_loop_test.go` | 11 | Full lifecycle: text, tool calls, errors, usage, hooks sequence |
| `agent_test.go` | 15 | Construction, Invoke, Stream, context cancellation |
| `mock_test.go` | — | Test infrastructure (MockModel, response builders, tool fixtures) |
| `anthropic_test.go` | 14 | HTTP mock server: non-streaming, SSE, errors, request format |

All tests use `go test` with no external test dependencies.

## Design Principles

- **Model-driven** — The LLM decides the control flow; no explicit state machines or decision trees
- **Zero dependencies** — Core SDK and Anthropic provider use only the Go standard library (`net/http`, `encoding/json`, `bufio`, `sync`)
- **Idiomatic Go** — Interfaces for abstraction, `context.Context` for cancellation, channels for streaming, functional options for configuration
- **Iterative event loop** — Go lacks tail-call optimisation, so the loop is iterative (not recursive like the Python SDK)
- **Testable** — MockModel + response builders make it straightforward to test agent behaviour without API calls

## Roadmap

- [ ] AWS Bedrock provider
- [ ] OpenAI-compatible provider
- [ ] Ollama provider (local models)
- [ ] MCP (Model Context Protocol) client integration
- [ ] Session persistence (file, S3, DynamoDB)
- [ ] Structured output enforcement via generics
- [ ] Multi-agent patterns (Graph, Swarm, A2A)
- [ ] OpenTelemetry tracing
- [ ] Interrupt/resume (human-in-the-loop)
- [ ] Context overflow recovery with automatic conversation reduction

## Licence

Apache 2.0
