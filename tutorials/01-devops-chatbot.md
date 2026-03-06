# Tutorial 1: DevOps Chatbot

Build a system health monitoring agent that checks disk usage, memory, running processes, and reads log files. The agent streams its responses to the terminal and logs every model and tool call.

## What You'll Learn

- Defining tools with `NewFuncTool`
- Streaming responses with `agent.Stream()`
- Handling all event types (`EventTextDelta`, `EventToolStart`, `EventToolEnd`, `EventComplete`, `EventError`)
- Adding logging hooks with `OnBeforeModelCall` and `OnBeforeToolCall`

## The Scenario

You're building an on-call assistant that DevOps engineers can query in natural language. Instead of remembering `df -h` flags or `grep` patterns, they ask "Is the disk full?" or "Show me the last 20 lines of the syslog" and the agent figures out which tools to use.

## Complete Code

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	strands "github.com/achrafsoltani/strands-agents-sdk-go"
	"github.com/achrafsoltani/strands-agents-sdk-go/provider/anthropic"
)

func main() {
	// --- Tools ---

	diskUsage := strands.NewFuncTool(
		"disk_usage",
		"Show disk usage for all mounted filesystems. Returns the output of df -h.",
		func(ctx context.Context, input map[string]any) (any, error) {
			out, err := exec.CommandContext(ctx, "df", "-h").Output()
			if err != nil {
				return nil, fmt.Errorf("df failed: %w", err)
			}
			return string(out), nil
		},
		map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	)

	memoryInfo := strands.NewFuncTool(
		"memory_info",
		"Show memory usage (total, used, free, available). Returns parsed /proc/meminfo fields.",
		func(ctx context.Context, input map[string]any) (any, error) {
			data, err := os.ReadFile("/proc/meminfo")
			if err != nil {
				return nil, fmt.Errorf("cannot read /proc/meminfo: %w", err)
			}
			// Extract the most useful lines.
			var result []string
			for _, line := range strings.Split(string(data), "\n") {
				for _, key := range []string{"MemTotal", "MemFree", "MemAvailable", "SwapTotal", "SwapFree"} {
					if strings.HasPrefix(line, key+":") {
						result = append(result, strings.TrimSpace(line))
					}
				}
			}
			return strings.Join(result, "\n"), nil
		},
		map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	)

	processList := strands.NewFuncTool(
		"process_list",
		"List running processes. Optionally filter by name. Returns ps output.",
		func(ctx context.Context, input map[string]any) (any, error) {
			args := []string{"aux"}
			out, err := exec.CommandContext(ctx, "ps", args...).Output()
			if err != nil {
				return nil, fmt.Errorf("ps failed: %w", err)
			}
			lines := strings.Split(string(out), "\n")

			// If a filter is given, keep only matching lines (plus the header).
			filter, _ := input["filter"].(string)
			if filter != "" {
				var filtered []string
				for i, line := range lines {
					if i == 0 || strings.Contains(strings.ToLower(line), strings.ToLower(filter)) {
						filtered = append(filtered, line)
					}
				}
				lines = filtered
			}

			// Cap output to 50 lines to avoid overwhelming the model.
			if len(lines) > 50 {
				lines = append(lines[:50], fmt.Sprintf("... (%d more lines)", len(lines)-50))
			}
			return strings.Join(lines, "\n"), nil
		},
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filter": map[string]any{
					"type":        "string",
					"description": "Optional process name filter (case-insensitive substring match)",
				},
			},
		},
	)

	readLog := strands.NewFuncTool(
		"read_log",
		"Read the last N lines of a log file. Only files under /var/log are permitted.",
		func(ctx context.Context, input map[string]any) (any, error) {
			path, _ := input["path"].(string)

			// Security: only allow /var/log paths, no traversal.
			if !strings.HasPrefix(path, "/var/log/") || strings.Contains(path, "..") {
				return nil, fmt.Errorf("access denied: only /var/log/ paths are allowed")
			}

			n := 20
			if v, ok := input["lines"].(float64); ok && v > 0 {
				n = int(v)
				if n > 200 {
					n = 200
				}
			}

			out, err := exec.CommandContext(ctx, "tail", "-n", fmt.Sprintf("%d", n), path).Output()
			if err != nil {
				return nil, fmt.Errorf("tail failed: %w", err)
			}
			return string(out), nil
		},
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Absolute path to the log file (must be under /var/log/)",
				},
				"lines": map[string]any{
					"type":        "number",
					"description": "Number of lines to read from the end (default: 20, max: 200)",
				},
			},
			"required": []string{"path"},
		},
	)

	// --- Agent ---

	agent := strands.NewAgent(
		strands.WithModel(anthropic.New(
			anthropic.WithModel("claude-sonnet-4-20250514"),
			anthropic.WithMaxTokens(4096),
		)),
		strands.WithTools(diskUsage, memoryInfo, processList, readLog),
		strands.WithSystemPrompt(`You are a DevOps assistant running on a Linux server.
Use the available tools to answer questions about the system's health.
When reporting numbers, use human-readable units (GB, MB, %).
Be concise — engineers don't need prose, they need facts.`),
	)

	// --- Logging Hooks ---

	agent.Hooks.OnBeforeModelCall(func(e *strands.BeforeModelCallEvent) {
		log.Printf("[model] sending %d messages to the model", len(e.Messages))
	})

	agent.Hooks.OnBeforeToolCall(func(e *strands.BeforeToolCallEvent) {
		log.Printf("[tool] executing %s", e.ToolUse.Name)
	})

	// --- Run with streaming ---

	query := "Give me a quick health check: disk usage, memory, and any Go processes running."
	if len(os.Args) > 1 {
		query = strings.Join(os.Args[1:], " ")
	}

	fmt.Printf("You: %s\n\nAssistant: ", query)

	for event := range agent.Stream(context.Background(), query) {
		switch event.Type {
		case strands.EventTextDelta:
			fmt.Print(event.Text)
		case strands.EventToolStart:
			fmt.Printf("\n  [running %s...]\n", event.ToolName)
		case strands.EventToolEnd:
			fmt.Printf("  [%s done]\n", event.ToolName)
		case strands.EventComplete:
			fmt.Printf("\n\n--- Tokens: %d input, %d output ---\n",
				event.Result.Usage.InputTokens, event.Result.Usage.OutputTokens)
		case strands.EventError:
			log.Fatalf("Error: %v", event.Error)
		}
	}
}
```

## How It Works

### Tools

The agent has four tools, each wrapping a simple system command:

| Tool | Command | Purpose |
|------|---------|---------|
| `disk_usage` | `df -h` | Filesystem space |
| `memory_info` | reads `/proc/meminfo` | RAM and swap |
| `process_list` | `ps aux` | Running processes (with optional filter) |
| `read_log` | `tail -n N` | Log file contents |

Each tool uses `exec.CommandContext` so the command is cancelled if the agent's context is cancelled.

### Security

The `read_log` tool restricts access to `/var/log/` and rejects path traversal (`..`). This is a simple guardrail — in production you'd also validate against symlinks and use a chroot or namespace.

### Streaming

`agent.Stream()` returns a `<-chan Event`. The `range` loop receives events as the model generates them:

1. **TextDelta** — incremental text, printed immediately for a typewriter effect
2. **ToolStart** — the model has decided to call a tool
3. **ToolEnd** — the tool has returned its result
4. **Complete** — the full invocation is finished, with token usage
5. **Error** — something went wrong

### Logging Hooks

Two hooks provide observability without cluttering the tool code:

- `OnBeforeModelCall` logs the number of messages being sent
- `OnBeforeToolCall` logs which tool is about to run

These fire synchronously before the operation, so the log output appears in order.

## Running

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
go run main.go
# or with a custom query:
go run main.go "Is swap being used? Show me the top memory consumers."
```

## Extending This

- **Add an `uptime` tool** — `uptime -p` for human-readable uptime
- **Add a `systemctl_status` tool** — check if a specific service is running
- **Add an `AfterModelCall` retry hook** — retry on rate limits (see Tutorial 5)
- **Restrict tools per user** — use `WithState` to pass a user role, check it in a `BeforeToolCall` hook
