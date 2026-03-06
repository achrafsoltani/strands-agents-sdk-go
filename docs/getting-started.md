# Getting Started

## Installation

```bash
go get github.com/achrafsoltani/strands-agents-sdk-go
```

Requires Go 1.23 or later. No external dependencies.

## Credentials

### Anthropic

Set the `ANTHROPIC_API_KEY` environment variable:

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
```

Or pass it explicitly:

```go
anthropic.New(anthropic.WithAPIKey("sk-ant-..."))
```

### AWS Bedrock

The Bedrock provider resolves credentials in order:

1. **Explicit** — `bedrock.WithCredentials(accessKey, secretKey, sessionToken)`
2. **Environment variables** — `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`
3. **Shared credentials file** — `~/.aws/credentials` (respects `AWS_PROFILE`)

Region is resolved similarly:

1. **Explicit** — `bedrock.WithRegion("eu-west-1")`
2. **Environment** — `AWS_DEFAULT_REGION` or `AWS_REGION`
3. **Config file** — `~/.aws/config`
4. **Fallback** — `us-east-1`

If you have the AWS CLI configured, Bedrock works out of the box with no extra setup.

## Your First Agent

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
        strands.WithSystemPrompt("You are a helpful assistant."),
    )

    result, err := agent.Invoke(context.Background(), "What is the capital of France?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(result.Message.Text())
}
```

## Adding Tools

Tools are defined with a name, description, handler function, and JSON Schema:

```go
weather := strands.NewFuncTool(
    "get_weather",
    "Get the current weather for a city",
    func(ctx context.Context, input map[string]any) (any, error) {
        city, _ := input["city"].(string)
        // In production, call a real weather API.
        return fmt.Sprintf("It's 22C and sunny in %s", city), nil
    },
    map[string]any{
        "type": "object",
        "properties": map[string]any{
            "city": map[string]any{
                "type":        "string",
                "description": "City name",
            },
        },
        "required": []string{"city"},
    },
)

agent := strands.NewAgent(
    strands.WithModel(anthropic.New()),
    strands.WithTools(weather),
    strands.WithSystemPrompt("You are a weather assistant."),
)

result, err := agent.Invoke(ctx, "What's the weather in London?")
```

When the model decides to use the `get_weather` tool, the SDK:

1. Extracts the tool use from the model's response
2. Looks up `get_weather` in the tool registry
3. Calls the handler with the model's input (`{"city": "London"}`)
4. Sends the result back to the model
5. The model generates a natural language response incorporating the tool result

## Streaming

Use `Agent.Stream()` to receive events as they happen:

```go
for event := range agent.Stream(ctx, "Tell me about Go concurrency") {
    switch event.Type {
    case strands.EventTextDelta:
        fmt.Print(event.Text) // Incremental text from the model
    case strands.EventToolStart:
        fmt.Printf("\n[calling %s...]\n", event.ToolName)
    case strands.EventToolEnd:
        fmt.Printf("[%s done]\n", event.ToolName)
    case strands.EventComplete:
        fmt.Printf("\nTokens: %d in, %d out\n",
            event.Result.Usage.InputTokens,
            event.Result.Usage.OutputTokens)
    case strands.EventError:
        log.Fatal(event.Error)
    }
}
```

The channel is closed when the invocation completes.

## Using Bedrock Instead of Anthropic

Swap the provider — the rest of the code stays the same:

```go
import "github.com/achrafsoltani/strands-agents-sdk-go/provider/bedrock"

agent := strands.NewAgent(
    strands.WithModel(bedrock.New(
        bedrock.WithRegion("eu-west-1"),
        bedrock.WithModel("anthropic.claude-3-5-sonnet-20241022-v2:0"),
    )),
    strands.WithTools(weather),
)
```

## Configuration Options

```go
agent := strands.NewAgent(
    strands.WithModel(model),                  // Required: model provider
    strands.WithTools(tool1, tool2),            // Register tools
    strands.WithSystemPrompt("..."),            // System prompt
    strands.WithMaxCycles(10),                  // Max event loop cycles (default: 20)
    strands.WithSequentialExecution(),           // Sequential tool execution (default: concurrent)
    strands.WithConversationManager(cm),         // Custom conversation manager
    strands.WithState(map[string]any{"k": "v"}), // Initial state
)
```

## Next Steps

- [Core Concepts](core-concepts.md) — Deep dive into the agent lifecycle
- [Providers](providers/README.md) — Provider configuration and internals
- [Examples](examples.md) — Extended usage patterns
- [API Reference](api-reference.md) — Complete type documentation
