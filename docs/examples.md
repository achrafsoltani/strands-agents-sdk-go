# Examples

## Basic Agent (No Tools)

```go
package main

import (
    "context"
    "fmt"
    "log"

    strands "github.com/achrafsoltani/strands-agents-sdk-go"
    "github.com/achrafsoltani/strands-agents-sdk-go/provider/anthropic"
)

func main() {
    agent := strands.NewAgent(
        strands.WithModel(anthropic.New()),
        strands.WithSystemPrompt("Answer in exactly one sentence."),
    )

    result, err := agent.Invoke(context.Background(), "What is Go?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(result.Message.Text())
}
```

## Multi-Tool Agent

```go
package main

import (
    "context"
    "fmt"
    "log"
    "math"
    "strconv"
    "strings"

    strands "github.com/achrafsoltani/strands-agents-sdk-go"
    "github.com/achrafsoltani/strands-agents-sdk-go/provider/anthropic"
)

func main() {
    wordCount := strands.NewFuncTool(
        "word_count", "Count words in text",
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
    )

    calculator := strands.NewFuncTool(
        "calculator", "Evaluate basic math (add, subtract, multiply, divide, sqrt)",
        func(_ context.Context, input map[string]any) (any, error) {
            op, _ := input["operation"].(string)
            a, _ := input["a"].(float64)
            b, _ := input["b"].(float64)
            switch op {
            case "add":
                return a + b, nil
            case "subtract":
                return a - b, nil
            case "multiply":
                return a * b, nil
            case "divide":
                if b == 0 {
                    return nil, fmt.Errorf("division by zero")
                }
                return a / b, nil
            case "sqrt":
                return math.Sqrt(a), nil
            default:
                return nil, fmt.Errorf("unknown operation: %s", op)
            }
        },
        map[string]any{
            "type": "object",
            "properties": map[string]any{
                "operation": map[string]any{
                    "type": "string",
                    "enum": []string{"add", "subtract", "multiply", "divide", "sqrt"},
                },
                "a": map[string]any{"type": "number"},
                "b": map[string]any{"type": "number", "description": "Second operand (ignored for sqrt)"},
            },
            "required": []string{"operation", "a"},
        },
    )

    agent := strands.NewAgent(
        strands.WithModel(anthropic.New()),
        strands.WithTools(wordCount, calculator),
        strands.WithSystemPrompt("You have access to a word counter and calculator."),
    )

    result, err := agent.Invoke(context.Background(),
        "How many words are in 'the quick brown fox' and what is the square root of that number?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(result.Message.Text())
    fmt.Printf("Tokens: %d input, %d output\n",
        result.Usage.InputTokens, result.Usage.OutputTokens)
}
```

## Streaming with All Event Types

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    strands "github.com/achrafsoltani/strands-agents-sdk-go"
    "github.com/achrafsoltani/strands-agents-sdk-go/provider/anthropic"
)

func main() {
    agent := strands.NewAgent(
        strands.WithModel(anthropic.New()),
        strands.WithTools(/* your tools */),
    )

    for event := range agent.Stream(context.Background(), os.Args[1]) {
        switch event.Type {
        case strands.EventTextDelta:
            fmt.Print(event.Text)

        case strands.EventToolStart:
            fmt.Printf("\n--- calling %s(%v) ---\n", event.ToolName, event.ToolInput)

        case strands.EventToolEnd:
            fmt.Printf("--- %s complete ---\n", event.ToolName)

        case strands.EventComplete:
            r := event.Result
            fmt.Printf("\n\n[done] stop=%s tokens=%d/%d state=%v\n",
                r.StopReason, r.Usage.InputTokens, r.Usage.OutputTokens, r.State)

        case strands.EventError:
            log.Fatalf("Error: %v", event.Error)
        }
    }
}
```

## Hooks: Logging

```go
agent.Hooks.OnBeforeModelCall(func(e *strands.BeforeModelCallEvent) {
    log.Printf("[model] calling with %d messages", len(e.Messages))
})

agent.Hooks.OnAfterModelCall(func(e *strands.AfterModelCallEvent) {
    if e.Err != nil {
        log.Printf("[model] error: %v", e.Err)
    } else {
        log.Printf("[model] stop=%s tokens=%d/%d latency=%dms",
            e.Response.StopReason,
            e.Response.Usage.InputTokens,
            e.Response.Usage.OutputTokens,
            e.Response.Metrics.LatencyMs)
    }
})

agent.Hooks.OnBeforeToolCall(func(e *strands.BeforeToolCallEvent) {
    log.Printf("[tool] calling %s with %v", e.ToolUse.Name, e.ToolUse.Input)
})

agent.Hooks.OnAfterToolCall(func(e *strands.AfterToolCallEvent) {
    log.Printf("[tool] %s result: %s", e.ToolUse.Name, e.Result.Content[0].Text)
})

agent.Hooks.OnMessageAdded(func(e *strands.MessageAddedEvent) {
    log.Printf("[message] role=%s blocks=%d", e.Message.Role, len(e.Message.Content))
})
```

## Hooks: Guardrails

```go
// Block specific tools
agent.Hooks.OnBeforeToolCall(func(e *strands.BeforeToolCallEvent) {
    blocked := map[string]bool{"delete_file": true, "run_command": true}
    if blocked[e.ToolUse.Name] {
        e.Cancel = true
        e.CancelMsg = fmt.Sprintf("tool %q is not permitted", e.ToolUse.Name)
    }
})

// Rate limit tool calls
var toolCallCount int
agent.Hooks.OnBeforeToolCall(func(e *strands.BeforeToolCallEvent) {
    toolCallCount++
    if toolCallCount > 50 {
        e.Cancel = true
        e.CancelMsg = "too many tool calls"
    }
})
```

## Hooks: Retry on Transient Errors

```go
import (
    "strings"
    "time"
)

agent.Hooks.OnAfterModelCall(func(e *strands.AfterModelCallEvent) {
    if e.Err != nil {
        errMsg := e.Err.Error()
        if strings.Contains(errMsg, "429") || strings.Contains(errMsg, "throttl") {
            time.Sleep(2 * time.Second)
            e.Retry = true
        }
    }
})
```

## Bedrock with Cross-Region Inference

```go
import "github.com/achrafsoltani/strands-agents-sdk-go/provider/bedrock"

// Cross-region inference profile routes to nearest region
model := bedrock.New(
    bedrock.WithRegion("us-east-1"),
    bedrock.WithModel("us.anthropic.claude-3-5-sonnet-20241022-v2:0"),
)

agent := strands.NewAgent(
    strands.WithModel(model),
    strands.WithTools(myTools...),
)
```

## Multi-Turn Conversation

The agent maintains conversation history across `Invoke` calls:

```go
agent := strands.NewAgent(
    strands.WithModel(anthropic.New()),
    strands.WithSystemPrompt("You are a helpful tutor."),
)

ctx := context.Background()

// First turn
result, _ := agent.Invoke(ctx, "What is a goroutine?")
fmt.Println(result.Message.Text())

// Second turn — the agent remembers the previous exchange
result, _ = agent.Invoke(ctx, "How is it different from a thread?")
fmt.Println(result.Message.Text())

// Third turn — still has full context
result, _ = agent.Invoke(ctx, "Show me a simple example")
fmt.Println(result.Message.Text())

// Check conversation length
fmt.Printf("Messages in history: %d\n", len(agent.Messages))
```

## Custom Conversation Manager

Limit context to keep costs down:

```go
agent := strands.NewAgent(
    strands.WithModel(anthropic.New()),
    strands.WithConversationManager(&strands.SlidingWindowManager{
        WindowSize: 10, // Keep only last 10 messages
    }),
)
```

Or keep everything:

```go
agent := strands.NewAgent(
    strands.WithModel(anthropic.New()),
    strands.WithConversationManager(&strands.NullManager{}),
)
```

## Context Cancellation

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

result, err := agent.Invoke(ctx, "Write a long essay about Go")
if err != nil {
    // Will be context.DeadlineExceeded if it took too long
    log.Fatal(err)
}
```

## Sequential Tool Execution

When tool order matters:

```go
agent := strands.NewAgent(
    strands.WithModel(anthropic.New()),
    strands.WithTools(readFile, writeFile, runTests),
    strands.WithSequentialExecution(), // Tools run one at a time
)
```

## Agent State

Pass data through the agent lifecycle:

```go
agent := strands.NewAgent(
    strands.WithModel(anthropic.New()),
    strands.WithState(map[string]any{
        "user_id":    "usr_123",
        "session_id": "sess_456",
    }),
)

// Access state in hooks
agent.Hooks.OnBeforeToolCall(func(e *strands.BeforeToolCallEvent) {
    userID := e.Agent.State["user_id"].(string)
    log.Printf("User %s calling tool %s", userID, e.ToolUse.Name)
})

// State is returned in the result
result, _ := agent.Invoke(ctx, "Hello")
fmt.Println(result.State["user_id"])
```
