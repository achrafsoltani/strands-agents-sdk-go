# Tutorial 7: Custom Tool Implementation

Build tools by implementing the `Tool` interface directly (not using `NewFuncTool`). This approach is better for tools that need configuration, internal state, or shared resources like HTTP clients and database connections.

## What You'll Learn

- Implementing the `Tool` interface with a struct
- Managing tool state (connection pools, rate limiters, caches)
- Building configurable, reusable tools
- Registering tools manually with `ToolRegistry`
- When to use struct tools vs `FuncTool`

## The Scenario

You're building an agent that interacts with a REST API — specifically, a simplified GitHub-like service for managing issues. The tool needs an HTTP client, a base URL, and an authentication token. Rather than closing over these in a `FuncTool` closure, you implement the `Tool` interface on a struct, making the tool configurable and testable.

## Complete Code

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	strands "github.com/achrafsoltani/strands-agents-sdk-go"
	"github.com/achrafsoltani/strands-agents-sdk-go/provider/anthropic"
)

// =============================================================================
// Custom Tool 1: IssueTracker — implements strands.Tool directly
// =============================================================================

// Issue represents a project issue.
type Issue struct {
	ID        int       `json:"id"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Status    string    `json:"status"`
	Priority  string    `json:"priority"`
	Labels    []string  `json:"labels"`
	CreatedAt time.Time `json:"created_at"`
}

// IssueTracker is a tool that manages project issues.
// It implements strands.Tool with its own internal state.
type IssueTracker struct {
	mu     sync.Mutex
	issues map[int]*Issue
	nextID int
}

// NewIssueTracker creates a new issue tracker.
func NewIssueTracker() *IssueTracker {
	return &IssueTracker{
		issues: make(map[int]*Issue),
		nextID: 1,
	}
}

// Spec returns the tool specification for the model.
func (t *IssueTracker) Spec() strands.ToolSpec {
	return strands.ToolSpec{
		Name:        "issue_tracker",
		Description: "Manage project issues. Supports creating, listing, updating, and closing issues.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"enum":        []string{"create", "list", "get", "update", "close"},
					"description": "The action to perform",
				},
				"id": map[string]any{
					"type":        "number",
					"description": "Issue ID (for get, update, close)",
				},
				"title": map[string]any{
					"type":        "string",
					"description": "Issue title (for create, update)",
				},
				"body": map[string]any{
					"type":        "string",
					"description": "Issue description (for create, update)",
				},
				"priority": map[string]any{
					"type":        "string",
					"enum":        []string{"low", "medium", "high", "critical"},
					"description": "Priority level (for create, update)",
				},
				"labels": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Labels to apply (for create, update)",
				},
				"status_filter": map[string]any{
					"type":        "string",
					"enum":        []string{"open", "closed", "all"},
					"description": "Filter by status (for list, default: open)",
				},
			},
			"required": []string{"action"},
		},
	}
}

// Execute runs the tool with the given input.
func (t *IssueTracker) Execute(ctx context.Context, toolUseID string, input map[string]any) strands.ToolResult {
	action, _ := input["action"].(string)

	switch action {
	case "create":
		return t.create(toolUseID, input)
	case "list":
		return t.list(toolUseID, input)
	case "get":
		return t.get(toolUseID, input)
	case "update":
		return t.update(toolUseID, input)
	case "close":
		return t.closeIssue(toolUseID, input)
	default:
		return strands.ErrorResult(toolUseID, fmt.Sprintf("unknown action: %s", action))
	}
}

func (t *IssueTracker) create(toolUseID string, input map[string]any) strands.ToolResult {
	title, _ := input["title"].(string)
	body, _ := input["body"].(string)
	priority, _ := input["priority"].(string)
	if priority == "" {
		priority = "medium"
	}

	var labels []string
	if labelsRaw, ok := input["labels"].([]any); ok {
		for _, l := range labelsRaw {
			labels = append(labels, fmt.Sprintf("%v", l))
		}
	}

	t.mu.Lock()
	issue := &Issue{
		ID:        t.nextID,
		Title:     title,
		Body:      body,
		Status:    "open",
		Priority:  priority,
		Labels:    labels,
		CreatedAt: time.Now(),
	}
	t.issues[t.nextID] = issue
	t.nextID++
	t.mu.Unlock()

	data, _ := json.MarshalIndent(issue, "", "  ")
	return strands.TextResult(toolUseID, fmt.Sprintf("Issue created:\n%s", string(data)))
}

func (t *IssueTracker) list(toolUseID string, input map[string]any) strands.ToolResult {
	statusFilter, _ := input["status_filter"].(string)
	if statusFilter == "" {
		statusFilter = "open"
	}

	t.mu.Lock()
	var filtered []*Issue
	for _, issue := range t.issues {
		if statusFilter == "all" || issue.Status == statusFilter {
			filtered = append(filtered, issue)
		}
	}
	t.mu.Unlock()

	if len(filtered) == 0 {
		return strands.TextResult(toolUseID, fmt.Sprintf("No %s issues found.", statusFilter))
	}

	var lines []string
	for _, issue := range filtered {
		lines = append(lines, fmt.Sprintf("#%d [%s] %s (priority: %s, labels: %s)",
			issue.ID, issue.Status, issue.Title, issue.Priority, strings.Join(issue.Labels, ", ")))
	}
	return strands.TextResult(toolUseID, strings.Join(lines, "\n"))
}

func (t *IssueTracker) get(toolUseID string, input map[string]any) strands.ToolResult {
	id := int(input["id"].(float64))

	t.mu.Lock()
	issue, ok := t.issues[id]
	t.mu.Unlock()

	if !ok {
		return strands.ErrorResult(toolUseID, fmt.Sprintf("issue #%d not found", id))
	}

	data, _ := json.MarshalIndent(issue, "", "  ")
	return strands.TextResult(toolUseID, string(data))
}

func (t *IssueTracker) update(toolUseID string, input map[string]any) strands.ToolResult {
	id := int(input["id"].(float64))

	t.mu.Lock()
	issue, ok := t.issues[id]
	if !ok {
		t.mu.Unlock()
		return strands.ErrorResult(toolUseID, fmt.Sprintf("issue #%d not found", id))
	}

	if title, ok := input["title"].(string); ok {
		issue.Title = title
	}
	if body, ok := input["body"].(string); ok {
		issue.Body = body
	}
	if priority, ok := input["priority"].(string); ok {
		issue.Priority = priority
	}
	if labelsRaw, ok := input["labels"].([]any); ok {
		var labels []string
		for _, l := range labelsRaw {
			labels = append(labels, fmt.Sprintf("%v", l))
		}
		issue.Labels = labels
	}
	t.mu.Unlock()

	data, _ := json.MarshalIndent(issue, "", "  ")
	return strands.TextResult(toolUseID, fmt.Sprintf("Issue updated:\n%s", string(data)))
}

func (t *IssueTracker) closeIssue(toolUseID string, input map[string]any) strands.ToolResult {
	id := int(input["id"].(float64))

	t.mu.Lock()
	issue, ok := t.issues[id]
	if !ok {
		t.mu.Unlock()
		return strands.ErrorResult(toolUseID, fmt.Sprintf("issue #%d not found", id))
	}
	issue.Status = "closed"
	t.mu.Unlock()

	return strands.TextResult(toolUseID, fmt.Sprintf("Issue #%d closed.", id))
}

// =============================================================================
// Custom Tool 2: HTTPClient — configurable HTTP request tool
// =============================================================================

// HTTPClientTool makes HTTP requests. It carries its own http.Client
// with custom timeouts, headers, and base URL.
type HTTPClientTool struct {
	client  *http.Client
	baseURL string
	headers map[string]string
}

// HTTPClientOption configures an HTTPClientTool.
type HTTPClientOption func(*HTTPClientTool)

func WithBaseURL(url string) HTTPClientOption {
	return func(t *HTTPClientTool) { t.baseURL = url }
}

func WithTimeout(d time.Duration) HTTPClientOption {
	return func(t *HTTPClientTool) { t.client.Timeout = d }
}

func WithHeader(key, value string) HTTPClientOption {
	return func(t *HTTPClientTool) { t.headers[key] = value }
}

func NewHTTPClientTool(opts ...HTTPClientOption) *HTTPClientTool {
	t := &HTTPClientTool{
		client:  &http.Client{Timeout: 10 * time.Second},
		headers: make(map[string]string),
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

func (t *HTTPClientTool) Spec() strands.ToolSpec {
	return strands.ToolSpec{
		Name:        "http_request",
		Description: "Make an HTTP GET request to a URL and return the response body (first 2000 chars).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "URL path (appended to base URL) or full URL if no base URL is set",
				},
			},
			"required": []string{"path"},
		},
	}
}

func (t *HTTPClientTool) Execute(ctx context.Context, toolUseID string, input map[string]any) strands.ToolResult {
	path, _ := input["path"].(string)

	url := path
	if t.baseURL != "" && !strings.HasPrefix(path, "http") {
		url = strings.TrimRight(t.baseURL, "/") + "/" + strings.TrimLeft(path, "/")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return strands.ErrorResult(toolUseID, fmt.Sprintf("invalid request: %v", err))
	}
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return strands.ErrorResult(toolUseID, fmt.Sprintf("request failed: %v", err))
	}
	defer resp.Body.Close()

	// Read up to 2000 bytes.
	buf := make([]byte, 2000)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])

	return strands.TextResult(toolUseID,
		fmt.Sprintf("Status: %d\nBody:\n%s", resp.StatusCode, body))
}

// =============================================================================
// Main
// =============================================================================

func main() {
	// Create tool instances with configuration.
	tracker := NewIssueTracker()

	httpTool := NewHTTPClientTool(
		WithBaseURL("https://httpbin.org"),
		WithTimeout(15*time.Second),
		WithHeader("User-Agent", "strands-agent/1.0"),
	)

	// Seed some initial issues.
	tracker.create("seed", map[string]any{
		"title":    "Fix login timeout on mobile",
		"body":     "Users report 30s timeout on 3G connections",
		"priority": "high",
		"labels":   []any{"bug", "mobile"},
	})
	tracker.create("seed", map[string]any{
		"title":    "Add dark mode support",
		"body":     "Feature request from multiple users",
		"priority": "medium",
		"labels":   []any{"feature", "ui"},
	})
	tracker.create("seed", map[string]any{
		"title":    "Update API documentation",
		"body":     "Several endpoints are undocumented",
		"priority": "low",
		"labels":   []any{"docs"},
	})

	// Create the agent with both custom tools.
	agent := strands.NewAgent(
		strands.WithModel(anthropic.New(
			anthropic.WithModel("claude-sonnet-4-20250514"),
		)),
		strands.WithTools(tracker, httpTool),
		strands.WithSystemPrompt(`You are a project management assistant.

You can manage issues (create, list, update, close) and make HTTP requests
for checking external services.

When managing issues:
- List existing issues before creating duplicates.
- Use appropriate priority levels.
- Apply relevant labels.`),
	)

	ctx := context.Background()

	// --- Demo: Issue management ---

	fmt.Println("=== Issue Management ===")
	fmt.Println()

	result, err := agent.Invoke(ctx,
		"List all open issues, then create a new critical issue about a database "+
			"connection pool leak in the payment service. Label it as 'bug' and 'backend'.")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Message.Text())

	fmt.Println()
	fmt.Println("=== Issue Update ===")
	fmt.Println()

	result, err = agent.Invoke(ctx,
		"Close the documentation issue and escalate the login timeout bug to critical priority.")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Message.Text())

	// --- Demo: HTTP tool ---

	fmt.Println()
	fmt.Println("=== HTTP Request ===")
	fmt.Println()

	result, err = agent.Invoke(ctx,
		"Check if httpbin.org is responding by hitting the /get endpoint.")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Message.Text())

	// --- Show final issue state ---

	fmt.Println("\n=== Final Issue State ===")
	tracker.mu.Lock()
	for _, issue := range tracker.issues {
		fmt.Printf("  #%d [%s] %s (priority: %s)\n",
			issue.ID, issue.Status, issue.Title, issue.Priority)
	}
	tracker.mu.Unlock()
}
```

## How It Works

### The Tool Interface

The `Tool` interface requires two methods:

```go
type Tool interface {
    Spec() ToolSpec
    Execute(ctx context.Context, toolUseID string, input map[string]any) ToolResult
}
```

**`Spec()`** returns the tool's name, description, and JSON Schema for the input. The model uses this to understand when and how to call the tool.

**`Execute()`** runs the tool. It receives the context (for cancellation), a unique ID for the tool call, and the input from the model. It returns a `ToolResult` — either success or error.

### Struct Tools vs FuncTool

| | `FuncTool` | Struct Tool |
|---|---|---|
| **Best for** | Simple, stateless operations | Stateful, configurable tools |
| **State** | Via closure (fragile) | Struct fields (explicit) |
| **Configuration** | Closure capture | Options pattern |
| **Testing** | Test the function | Test the struct methods |
| **Reuse** | Copy-paste the closure | Import and configure |

Use `NewFuncTool` for quick one-off tools. Use a struct implementation when the tool needs:
- Configuration (base URL, API keys, timeouts)
- Internal state (caches, connection pools, counters)
- Multiple actions routed through one tool
- Independent unit testing

### Thread Safety

The `IssueTracker` uses a `sync.Mutex` because the `ConcurrentExecutor` may call `Execute` from multiple goroutines simultaneously. If the model requests "create issue A" and "create issue B" in the same turn, both calls run in parallel, and the mutex prevents a data race on the `issues` map.

### Functional Options for Tools

The `HTTPClientTool` follows the same functional options pattern as the SDK itself:

```go
tool := NewHTTPClientTool(
    WithBaseURL("https://api.example.com"),
    WithTimeout(15*time.Second),
    WithHeader("Authorization", "Bearer "+token),
)
```

This makes tools configurable without exposing their internals, and mirrors the `anthropic.New(anthropic.WithModel(...))` pattern used by the providers.

### Action Routing

The `IssueTracker` exposes a single tool with an `action` parameter. The model sends `{"action": "create", "title": "..."}` and the `Execute` method routes to the appropriate handler. This is a common pattern — it gives the model one tool to reason about rather than five separate tools.

The trade-off is a more complex JSON Schema. For very different operations, separate tools may be clearer for the model.

## Running

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
go run main.go
```

## Extending This

- **Add database backing** — replace the in-memory map with SQLite or PostgreSQL
- **Add authentication** — pass an API token through `WithState` and verify in tools
- **Add a `WebhookTool`** — POST to external services when issues change state
- **Add caching** — cache HTTP responses in the tool struct with TTL-based expiry
- **Add batch operations** — extend the issue tracker with `bulk_update` and `bulk_close` actions
