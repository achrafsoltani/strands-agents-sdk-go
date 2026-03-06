# Core Concepts

## Agent Lifecycle

The `Agent` is the top-level orchestrator. It holds a model, tools, hooks, conversation history, and state. The LLM drives the control flow — there is no explicit state machine.

```
Agent.Invoke("prompt")
    |
    v
Append user message to Messages
    |
    v
runLoop() ──── iterative cycle ──────────────────┐
    |                                              |
    v                                              |
callModel() ─── BeforeModelCall hook               |
    |          Model.ConverseStream()              |
    |          AfterModelCall hook                  |
    v                                              |
StopReason?                                        |
    |                                              |
    ├── end_turn ──> return AgentResult             |
    ├── max_tokens ──> return ErrMaxTokensReached   |
    └── tool_use ──> Execute tools ─────────────────┘
                     Append tool results
                     Continue to next cycle
```

### Key Properties

| Field | Type | Description |
|-------|------|-------------|
| `Model` | `Model` | The LLM provider (Anthropic, Bedrock, etc.) |
| `Messages` | `[]Message` | Conversation history — grows across cycles |
| `SystemPrompt` | `string` | Sent with every model call |
| `State` | `map[string]any` | User-defined state, accessible in hooks and results |
| `Tools` | `*ToolRegistry` | Thread-safe registry of available tools |
| `Hooks` | `*HookRegistry` | Lifecycle event callbacks |
| `Executor` | `ToolExecutor` | Controls parallel vs sequential tool execution |
| `MaxCycles` | `int` | Safety limit on event loop iterations (default: 20) |

## Event Loop

**Source**: `event_loop.go`

The event loop is the heart of the agent. It is deliberately **iterative**, not recursive — Go lacks tail-call optimisation, and deep agent runs (many tool calls) would risk stack overflow with recursion.

### Cycle Mechanics

Each cycle:

1. **Build input** — current `Messages`, `SystemPrompt`, and tool specs
2. **Call model** — via `ConverseStream()` with retry support (up to 6 attempts)
3. **Route by stop reason**:
   - `end_turn` — model is done; return the result
   - `tool_use` — extract tool calls, execute them, append results, loop
   - `max_tokens` — response was truncated; return error

### Retry Strategy

The model call supports up to 6 retries, controlled by the `AfterModelCall` hook:

```go
agent.Hooks.OnAfterModelCall(func(e *strands.AfterModelCallEvent) {
    if e.Err != nil && isTransient(e.Err) {
        e.Retry = true // Will retry the model call
    }
})
```

Without a retry hook, errors propagate immediately.

### Max Cycles Guard

If the loop exceeds `MaxCycles` (default: 20), it returns `ErrMaxCycles`. This prevents runaway agents that keep calling tools indefinitely.

## Messages

**Source**: `types.go`

Messages are the conversation history between the user and the model.

```go
type Message struct {
    Role    Role           // "user" or "assistant"
    Content []ContentBlock // Text, tool use, or tool result blocks
}
```

### Content Blocks

Each message contains one or more content blocks:

| Type | Fields | Created By |
|------|--------|-----------|
| `text` | `Text string` | `TextBlock("hello")` |
| `tool_use` | `ToolUse *ToolUse` | `ToolUseBlock(id, name, input)` |
| `tool_result` | `ToolResult *ToolResult` | `ToolResultBlock(result)` |

A single assistant message can contain both text and tool use blocks (the model can explain its reasoning while requesting tool calls).

### Convenience Functions

```go
// Create a user message with text content
msg := strands.UserMessage("Hello")

// Access text content
text := msg.Text() // Concatenates all text blocks

// Access tool uses
uses := msg.ToolUses() // Returns []ToolUse
```

## Tools

**Source**: `tool.go`, `registry.go`

### Tool Interface

```go
type Tool interface {
    Spec() ToolSpec                                                    // Schema for the model
    Execute(ctx context.Context, toolUseID string, input map[string]any) ToolResult
}
```

### FuncTool

The simplest way to create a tool is `NewFuncTool`:

```go
tool := strands.NewFuncTool(
    "name",           // Must match ^[a-zA-Z0-9_-]{1,64}$
    "description",    // Shown to the model
    handler,          // func(ctx, input) (any, error)
    inputSchema,      // JSON Schema object
)
```

The handler's return value is automatically converted:
- `string` — used as-is
- `nil` — empty string
- Anything else — `fmt.Sprintf("%v", result)`

Errors are converted to `ToolResultError` with the error message.

### Tool Registry

The `ToolRegistry` is thread-safe (uses `sync.RWMutex`):

```go
registry := strands.NewToolRegistry()
registry.Register(tool)           // Returns error if name is invalid or duplicate
tool, ok := registry.Get("name")  // Lookup by name
specs := registry.Specs()          // All specs for the model
names := registry.Names()          // All registered names
n := registry.Len()                // Count
```

Tool names are validated against `^[a-zA-Z0-9_-]{1,64}$`.

## Hooks

**Source**: `hook.go`

The hook system provides lifecycle callbacks for cross-cutting concerns like logging, guardrails, retry logic, and result modification.

### Event Types

| Event | When | Key Fields |
|-------|------|-----------|
| `BeforeModelCallEvent` | Before each model invocation | `Agent`, `Messages` |
| `AfterModelCallEvent` | After each model invocation | `Agent`, `Response`, `Err`, `Retry` |
| `BeforeToolCallEvent` | Before each tool execution | `Agent`, `ToolUse`, `Cancel`, `CancelMsg` |
| `AfterToolCallEvent` | After each tool execution | `Agent`, `ToolUse`, `Result`, `Retry` |
| `MessageAddedEvent` | When a message is appended | `Agent`, `Message` |

### Execution Order

- **Before\*** hooks fire in registration order (FIFO)
- **After\*** hooks fire in reverse registration order (LIFO)

The LIFO ordering for After\* hooks matches the Python SDK's decorator stacking semantics — the last-registered hook wraps the innermost layer.

### Flow Control

Hooks can modify the agent's behaviour:

```go
// Cancel a tool call
agent.Hooks.OnBeforeToolCall(func(e *strands.BeforeToolCallEvent) {
    if e.ToolUse.Name == "dangerous_tool" {
        e.Cancel = true
        e.CancelMsg = "not permitted"
    }
})

// Retry a failed model call
agent.Hooks.OnAfterModelCall(func(e *strands.AfterModelCallEvent) {
    if e.Err != nil {
        e.Retry = true
    }
})

// Modify a tool result
agent.Hooks.OnAfterToolCall(func(e *strands.AfterToolCallEvent) {
    // e.Result can be replaced
})
```

## Executors

**Source**: `executor.go`

Executors control how multiple tool calls from a single model response are executed.

### ConcurrentExecutor (default)

Launches a goroutine per tool call via `sync.WaitGroup`. Results are collected in order.

```go
// Single tool: no goroutine overhead — runs inline
// Multiple tools: parallel goroutines
```

### SequentialExecutor

Runs tools one at a time, in the order the model requested them.

```go
strands.WithSequentialExecution() // Use during agent construction
```

### Tool Retry

Each tool execution supports up to 3 retries, controlled by `AfterToolCall` hooks. If the hook sets `Retry = true`, the tool is re-executed.

## Conversation Management

**Source**: `conversation.go`

### SlidingWindowManager (default)

Keeps only the most recent `WindowSize` messages (default: 100). When the conversation exceeds this limit, older messages are dropped.

```go
strands.WithConversationManager(&strands.SlidingWindowManager{WindowSize: 50})
```

### NullManager

Performs no trimming — the full conversation is always sent to the model.

```go
strands.WithConversationManager(&strands.NullManager{})
```

### Custom Managers

Implement the `ConversationManager` interface:

```go
type ConversationManager interface {
    ReduceContext(messages []Message) []Message
}
```

## Error Handling

### Sentinel Errors

| Error | Meaning |
|-------|---------|
| `ErrNoModel` | `Invoke()` or `Stream()` called without a model |
| `ErrMaxTokensReached` | Model response was truncated |
| `ErrMaxCycles` | Event loop exceeded `MaxCycles` |
| `ErrToolNotFound` | Model requested an unregistered tool |
| `ErrContextOverflow` | Conversation exceeds context window |

### Provider Errors

Providers return descriptive errors with the HTTP status code and response body:

```
anthropic: API error 401: {"error":{"message":"invalid api key"}}
bedrock: API error 403: {"message":"Access denied"}
bedrock: throttlingException: {"message":"rate limit exceeded"}
```

### Streaming Errors

When using `Agent.Stream()`, errors are delivered as events:

```go
case strands.EventError:
    log.Printf("error: %v", event.Error)
```

The channel is always closed after the final event (either `EventComplete` or `EventError`).
