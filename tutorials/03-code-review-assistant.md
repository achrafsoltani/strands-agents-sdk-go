# Tutorial 3: Code Review Assistant

Build a multi-turn code review assistant that reads source files, analyses them, and provides feedback. Uses sequential tool execution and a conversation window to manage context.

## What You'll Learn

- Multi-turn conversations with `agent.Invoke()` called multiple times
- Sequential execution with `WithSequentialExecution()` (tools run in order, not in parallel)
- Managing conversation context with `SlidingWindowManager`
- Designing tools that operate on the local filesystem

## The Scenario

You're building an assistant for a development team that reviews Go source files. The agent can list files in a directory, read file contents, and check for common style issues. Sequential execution ensures file operations happen in a predictable order — you don't want the agent to read a file before listing the directory to find it.

## Complete Code

```go
package main

import (
	"bufio"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"strings"

	strands "github.com/achrafsoltani/strands-agents-sdk-go"
	"github.com/achrafsoltani/strands-agents-sdk-go/provider/anthropic"
)

func main() {
	// --- Tools ---

	listFiles := strands.NewFuncTool(
		"list_files",
		"List Go source files in a directory. Returns file names with line counts.",
		func(_ context.Context, input map[string]any) (any, error) {
			dir, _ := input["directory"].(string)
			if dir == "" {
				dir = "."
			}

			// Resolve to absolute path and validate.
			absDir, err := filepath.Abs(dir)
			if err != nil {
				return nil, fmt.Errorf("invalid directory: %w", err)
			}

			entries, err := os.ReadDir(absDir)
			if err != nil {
				return nil, fmt.Errorf("cannot read directory: %w", err)
			}

			var files []string
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
					continue
				}
				// Count lines.
				data, err := os.ReadFile(filepath.Join(absDir, entry.Name()))
				if err != nil {
					continue
				}
				lineCount := strings.Count(string(data), "\n") + 1
				files = append(files, fmt.Sprintf("%-30s %4d lines", entry.Name(), lineCount))
			}

			if len(files) == 0 {
				return "No Go files found in " + absDir, nil
			}
			return fmt.Sprintf("Directory: %s\n\n%s", absDir, strings.Join(files, "\n")), nil
		},
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"directory": map[string]any{
					"type":        "string",
					"description": "Path to the directory to scan (default: current directory)",
				},
			},
		},
	)

	readFile := strands.NewFuncTool(
		"read_file",
		"Read the contents of a Go source file. Returns the file content with line numbers.",
		func(_ context.Context, input map[string]any) (any, error) {
			path, _ := input["path"].(string)
			if path == "" {
				return nil, fmt.Errorf("path is required")
			}

			absPath, err := filepath.Abs(path)
			if err != nil {
				return nil, fmt.Errorf("invalid path: %w", err)
			}

			data, err := os.ReadFile(absPath)
			if err != nil {
				return nil, fmt.Errorf("cannot read file: %w", err)
			}

			// Add line numbers.
			lines := strings.Split(string(data), "\n")

			// Cap at 200 lines to avoid overwhelming the model.
			truncated := false
			if len(lines) > 200 {
				lines = lines[:200]
				truncated = true
			}

			var numbered []string
			for i, line := range lines {
				numbered = append(numbered, fmt.Sprintf("%4d | %s", i+1, line))
			}

			result := fmt.Sprintf("File: %s\n\n%s", absPath, strings.Join(numbered, "\n"))
			if truncated {
				result += "\n\n... (file truncated at 200 lines)"
			}
			return result, nil
		},
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path to the Go source file",
				},
			},
			"required": []string{"path"},
		},
	)

	checkStyle := strands.NewFuncTool(
		"check_style",
		"Run basic style checks on a Go source file. Checks for parsing errors, "+
			"long functions, and exported symbols without documentation.",
		func(_ context.Context, input map[string]any) (any, error) {
			path, _ := input["path"].(string)
			if path == "" {
				return nil, fmt.Errorf("path is required")
			}

			absPath, err := filepath.Abs(path)
			if err != nil {
				return nil, fmt.Errorf("invalid path: %w", err)
			}

			// Parse the Go source file.
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, absPath, nil, parser.ParseComments)
			if err != nil {
				return fmt.Sprintf("Parse error: %v", err), nil
			}

			var issues []string

			// Check for exported functions without doc comments.
			for _, decl := range f.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok {
					if fn.Name.IsExported() && fn.Doc == nil {
						pos := fset.Position(fn.Pos())
						issues = append(issues,
							fmt.Sprintf("Line %d: exported function %s has no doc comment",
								pos.Line, fn.Name.Name))
					}
				}
			}

			// Check for long functions (rough heuristic: > 50 lines).
			data, err := os.ReadFile(absPath)
			if err == nil {
				lines := strings.Split(string(data), "\n")
				if len(lines) > 300 {
					issues = append(issues,
						fmt.Sprintf("File is %d lines — consider splitting into smaller files", len(lines)))
				}
			}

			if len(issues) == 0 {
				return "No style issues found.", nil
			}
			return fmt.Sprintf("Found %d issue(s):\n\n%s", len(issues), strings.Join(issues, "\n")), nil
		},
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path to the Go source file to check",
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
		strands.WithTools(listFiles, readFile, checkStyle),
		strands.WithSystemPrompt(`You are a Go code review assistant.

When reviewing code:
1. First list the files in the project to understand its structure.
2. Read the files the user asks about (or the most relevant ones).
3. Run style checks.
4. Provide actionable feedback: what's good, what to improve, and specific suggestions.

Focus on:
- Idiomatic Go patterns (error handling, naming, interfaces)
- Potential bugs or race conditions
- Code organisation and readability
- Missing error checks`),
		strands.WithSequentialExecution(),
		strands.WithConversationManager(&strands.SlidingWindowManager{
			WindowSize: 30,
		}),
	)

	// --- Interactive multi-turn review ---

	scanner := bufio.NewScanner(os.Stdin)
	ctx := context.Background()

	fmt.Println("Code Review Assistant (type 'quit' to exit)")
	fmt.Println("Try: 'Review the Go files in the current directory'")
	fmt.Println()

	for {
		fmt.Print("You: ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "quit" || input == "exit" {
			break
		}

		result, err := agent.Invoke(ctx, input)
		if err != nil {
			log.Printf("Error: %v", err)
			continue
		}

		fmt.Printf("\nAssistant: %s\n\n", result.Message.Text())
		fmt.Printf("[messages in history: %d | tokens: %d in / %d out]\n\n",
			len(agent.Messages),
			result.Usage.InputTokens, result.Usage.OutputTokens)
	}
}
```


## How It Works

### Sequential Execution

`WithSequentialExecution()` swaps the default `ConcurrentExecutor` for `SequentialExecutor`. When the model requests multiple tool calls in one turn (e.g. "list files, then read main.go"), they execute one at a time, in the order the model specified.

This matters for file operations: you want `list_files` to complete before `read_file`, so the model can use the listing to choose which file to read. With concurrent execution, both would run simultaneously and the model wouldn't benefit from the listing.

### Conversation Window

`SlidingWindowManager{WindowSize: 30}` keeps only the last 30 messages. As the conversation grows (each `Invoke` adds a user message, the model response, and potentially tool result messages), older messages are trimmed.

This prevents context overflow on long review sessions. The trade-off is that the agent loses memory of early exchanges — if you reviewed `agent.go` on turn 1 and ask about it on turn 20, it may need to re-read the file.

### Multi-Turn Flow

A typical review session:

```
Turn 1: "Review the Go files in the current directory"
  → Agent calls list_files, then reads key files, runs check_style
  → Provides initial review

Turn 2: "What about error handling in event_loop.go?"
  → Agent reads event_loop.go (already has context from turn 1)
  → Gives focused error handling feedback

Turn 3: "How would you refactor the retry logic?"
  → Agent uses its memory of the file content to suggest specific changes
```

Each turn's `Invoke` adds to the conversation history, so the agent builds up context naturally.

### Go Source Analysis

The `check_style` tool uses Go's built-in `go/parser` and `go/ast` packages to parse source files without external dependencies. It finds:

- **Exported functions without doc comments** — a common Go style issue
- **Very large files** — suggests splitting

This is intentionally simple. In production, you'd integrate with `go vet`, `staticcheck`, or `golangci-lint`.

## Running

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
# Run in any Go project directory:
cd /path/to/your/go/project
go run /path/to/this/main.go
```

## Extending This

- **Add a `run_tests` tool** — execute `go test ./...` and report results
- **Add a `git_diff` tool** — review only changed files
- **Add a `suggest_fix` tool** — propose concrete code changes
- **Switch to streaming** — replace `Invoke` with `Stream` for real-time feedback on large files
