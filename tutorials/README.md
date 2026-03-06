# Tutorials

Hands-on tutorials for building real-world agents with the Strands Agents SDK for Go. Each tutorial is self-contained, with complete runnable code and detailed explanations.

## Tutorials

| # | Tutorial | SDK Features | Scenario |
|---|----------|-------------|----------|
| 1 | [DevOps Chatbot](01-devops-chatbot.md) | Tools, streaming, logging hooks | System health monitoring agent |
| 2 | [Customer Support Agent](02-customer-support-agent.md) | Guardrail hooks, state, multi-tool | Support agent with KB and tickets |
| 3 | [Code Review Assistant](03-code-review-assistant.md) | Sequential execution, multi-turn, conversation manager | File-aware code reviewer |
| 4 | [Data Analysis Pipeline](04-data-analysis-pipeline.md) | Concurrent tools, state, Bedrock provider | CSV analysis and reporting |
| 5 | [Content Moderation](05-content-moderation.md) | All 5 hook types, cancel/retry, LIFO | Full moderation pipeline |
| 6 | [Interactive CLI Chat](06-interactive-cli-chat.md) | Streaming, context cancellation, signals | Multi-turn terminal chatbot |
| 7 | [Custom Tool Implementation](07-custom-tool-implementation.md) | Tool interface, struct tools, registries | HTTP API client tool |

## Prerequisites

- Go 1.23+
- `ANTHROPIC_API_KEY` environment variable (or AWS credentials for Tutorial 4's Bedrock example)

## Running

Each tutorial includes a complete `main.go` that you can copy into a directory and run:

```bash
mkdir my-agent && cd my-agent
go mod init my-agent
go get github.com/achrafsoltani/strands-agents-sdk-go
# paste main.go from the tutorial
go run main.go
```

## Feature Coverage

| Feature | Tutorials |
|---------|-----------|
| `NewFuncTool` | 1, 2, 3, 4 |
| `Tool` interface (struct) | 7 |
| `agent.Invoke()` | 1, 2, 3, 4, 5 |
| `agent.Stream()` | 1, 6 |
| `OnBeforeModelCall` | 1, 5 |
| `OnAfterModelCall` | 2, 5 |
| `OnBeforeToolCall` | 2, 5 |
| `OnAfterToolCall` | 5 |
| `OnMessageAdded` | 5, 6 |
| `WithSequentialExecution` | 3 |
| `WithConversationManager` | 3, 6 |
| `WithState` | 2, 4 |
| `WithMaxCycles` | 4 |
| Context cancellation | 6 |
| Bedrock provider | 4 |
| Multi-turn conversation | 3, 6 |
