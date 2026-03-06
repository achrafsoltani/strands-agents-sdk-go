# Tutorial 6: Interactive CLI Chat

Build a fully interactive multi-turn terminal chatbot with streaming output, graceful shutdown, per-turn timeouts, and conversation window management.

## What You'll Learn

- Building an interactive `Invoke`/`Stream` loop with `bufio.Scanner`
- Graceful shutdown with `os/signal` and context cancellation
- Per-turn timeouts with `context.WithTimeout`
- Managing conversation length with `SlidingWindowManager`
- Tracking conversation statistics with `OnMessageAdded`

## The Scenario

You're building a general-purpose CLI assistant — similar to ChatGPT in the terminal. It supports multi-turn conversations with streaming output, handles Ctrl+C gracefully (interrupts the current response without killing the process), and automatically trims old messages when the conversation gets long.

## Complete Code

```go
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	strands "github.com/achrafsoltani/strands-agents-sdk-go"
	"github.com/achrafsoltani/strands-agents-sdk-go/provider/anthropic"
)

func main() {
	// --- Agent ---

	agent := strands.NewAgent(
		strands.WithModel(anthropic.New(
			anthropic.WithModel("claude-sonnet-4-20250514"),
			anthropic.WithMaxTokens(4096),
		)),
		strands.WithSystemPrompt(`You are a helpful assistant in a terminal.
Keep responses concise — users are in a CLI, not reading a blog post.
Use markdown formatting sparingly (it renders as plain text here).
For code, use short snippets. For lists, use dashes.`),
		strands.WithConversationManager(&strands.SlidingWindowManager{
			WindowSize: 40, // Keep last 40 messages.
		}),
	)

	// --- Statistics tracking ---

	var totalTokensIn, totalTokensOut int64
	var messageCount int64

	agent.Hooks.OnMessageAdded(func(e *strands.MessageAddedEvent) {
		atomic.AddInt64(&messageCount, 1)
	})

	// --- Signal handling ---

	// Main context — cancelled on second Ctrl+C (full shutdown).
	mainCtx, mainCancel := context.WithCancel(context.Background())
	defer mainCancel()

	// Track whether we're currently generating a response.
	var generating atomic.Bool

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for range sigChan {
			if generating.Load() {
				// First Ctrl+C during generation: handled by per-turn cancel.
				fmt.Println("\n[interrupted]")
				continue
			}
			// Ctrl+C when idle: shut down.
			fmt.Println("\nGoodbye!")
			mainCancel()
			return
		}
	}()

	// --- Interactive loop ---

	scanner := bufio.NewScanner(os.Stdin)
	// Increase buffer for long pastes.
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	fmt.Println("Chat (Ctrl+C to interrupt, type 'quit' to exit)")
	fmt.Println("Commands: /stats, /clear, /history")
	fmt.Println()

	for {
		fmt.Print("> ")

		// Check if main context is done.
		select {
		case <-mainCtx.Done():
			return
		default:
		}

		if !scanner.Scan() {
			break // EOF
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		// Handle commands.
		switch input {
		case "quit", "exit", "/quit", "/exit":
			fmt.Println("Goodbye!")
			return

		case "/stats":
			fmt.Printf("Messages: %d | Tokens: %d in / %d out | History: %d messages\n\n",
				atomic.LoadInt64(&messageCount),
				atomic.LoadInt64(&totalTokensIn),
				atomic.LoadInt64(&totalTokensOut),
				len(agent.Messages))
			continue

		case "/clear":
			agent.Messages = agent.Messages[:0]
			fmt.Println("[conversation cleared]")
			fmt.Println()
			continue

		case "/history":
			fmt.Printf("Conversation history (%d messages):\n", len(agent.Messages))
			for i, msg := range agent.Messages {
				preview := msg.Text()
				if len(preview) > 80 {
					preview = preview[:80] + "..."
				}
				if preview == "" {
					preview = fmt.Sprintf("(%d content blocks)", len(msg.Content))
				}
				fmt.Printf("  %d. [%s] %s\n", i+1, msg.Role, preview)
			}
			fmt.Println()
			continue
		}

		// Create a per-turn context with timeout.
		// Ctrl+C cancels this context without killing the main loop.
		turnCtx, turnCancel := context.WithTimeout(mainCtx, 2*time.Minute)

		generating.Store(true)

		// Stream the response.
		fmt.Println()
		charCount := 0

		for event := range agent.Stream(turnCtx, input) {
			switch event.Type {
			case strands.EventTextDelta:
				fmt.Print(event.Text)
				charCount += len(event.Text)

			case strands.EventComplete:
				atomic.AddInt64(&totalTokensIn, int64(event.Result.Usage.InputTokens))
				atomic.AddInt64(&totalTokensOut, int64(event.Result.Usage.OutputTokens))

			case strands.EventError:
				if turnCtx.Err() != nil {
					fmt.Print("\n[response interrupted or timed out]")
				} else {
					fmt.Printf("\n[error: %v]", event.Error)
				}
			}
		}

		generating.Store(false)
		turnCancel()

		if charCount > 0 {
			fmt.Println()
		}
		fmt.Println()
	}
}
```

## How It Works

### Signal Handling

The programme handles Ctrl+C differently depending on state:

- **During generation** (`generating == true`): the per-turn context is cancelled, which interrupts the streaming model call. The main loop continues, and the user can type another prompt.
- **When idle** (`generating == false`): the main context is cancelled, and the programme exits cleanly.

This two-level approach is common in interactive CLI tools. The `atomic.Bool` ensures thread-safe state tracking between the signal goroutine and the main loop.

### Per-Turn Timeouts

Each turn gets its own context with a 2-minute timeout:

```go
turnCtx, turnCancel := context.WithTimeout(mainCtx, 2*time.Minute)
```

If the model takes longer than 2 minutes (e.g. a very complex query with many tool calls), the context expires and the stream returns an error. The main loop continues.

The per-turn context is derived from `mainCtx`, so if the main context is cancelled (Ctrl+C when idle), all active turns are also cancelled.

### Conversation Window

`SlidingWindowManager{WindowSize: 40}` trims the oldest messages when the history exceeds 40. This is essential for long chat sessions — without it, the context window eventually overflows and the API returns an error.

The sliding window is applied by the event loop before each model call:

```
Messages: [1, 2, 3, ..., 38, 39, 40, 41, 42]
After ReduceContext(WindowSize=40): [3, 4, ..., 40, 41, 42]
```

### Streaming

`agent.Stream()` returns a `<-chan Event`. The `range` loop processes events as they arrive:

- **TextDelta**: printed immediately — gives a typewriter effect
- **Complete**: accumulates token usage statistics
- **Error**: reports the error (or notes if it was a cancellation/timeout)

### Built-in Commands

The CLI supports meta-commands that don't go to the model:

| Command | Effect |
|---------|--------|
| `/stats` | Show token usage and message count |
| `/clear` | Reset conversation history |
| `/history` | Show all messages with role and preview |
| `quit` | Exit the programme |

## Running

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
go run main.go
```

Example session:

```
Chat (Ctrl+C to interrupt, type 'quit' to exit)
Commands: /stats, /clear, /history

> What is a goroutine?

A goroutine is a lightweight thread managed by the Go runtime. You start one
with the `go` keyword:

  go myFunction()

Key differences from OS threads:
- Much cheaper (a few KB of stack vs 1-8 MB)
- Multiplexed onto OS threads by the Go scheduler
- Communicate via channels, not shared memory

> How does the scheduler work?

The Go scheduler uses an M:N model — M goroutines on N OS threads...

> /stats
Messages: 4 | Tokens: 847 in / 312 out | History: 4 messages

> ^C
Goodbye!
```

## Extending This

- **Add tools** — pass `strands.WithTools(...)` for a tool-using chatbot
- **Add colours** — use ANSI escape codes to colour assistant text differently
- **Persist conversation** — save/load `agent.Messages` as JSON for session continuity
- **Add `/model` command** — switch models mid-conversation
- **Add `/system` command** — change the system prompt dynamically
