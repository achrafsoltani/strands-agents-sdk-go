# API Reference

Complete reference for all exported types, interfaces, functions, and constants in the `strands` package.

## Agent

### Types

```go
// Agent is the top-level orchestrator.
type Agent struct {
    Model        Model
    Messages     []Message
    SystemPrompt string
    State        map[string]any
    Tools        *ToolRegistry
    Hooks        *HookRegistry
    Executor     ToolExecutor
    Conversation ConversationManager
    MaxCycles    int // Default: 20
}

// AgentResult is the outcome of a single invocation.
type AgentResult struct {
    StopReason StopReason
    Message    Message
    Usage      Usage
    State      map[string]any
}

// Option configures an Agent.
type Option func(*Agent)
```

### Constructor

```go
func NewAgent(opts ...Option) *Agent
```

Creates a new Agent with defaults:
- `Executor`: `ConcurrentExecutor`
- `Conversation`: `SlidingWindowManager{WindowSize: 100}`
- `MaxCycles`: 20

### Methods

```go
func (a *Agent) Invoke(ctx context.Context, prompt string) (*AgentResult, error)
func (a *Agent) Stream(ctx context.Context, prompt string) <-chan Event
```

### Options

```go
func WithModel(m Model) Option
func WithSystemPrompt(prompt string) Option
func WithTools(tools ...Tool) Option
func WithMaxCycles(n int) Option
func WithSequentialExecution() Option
func WithConversationManager(cm ConversationManager) Option
func WithState(state map[string]any) Option
```

---

## Model

### Interface

```go
type Model interface {
    Converse(ctx context.Context, input *ConverseInput) (*ConverseOutput, error)
    ConverseStream(ctx context.Context, input *ConverseInput, handler StreamHandler) (*ConverseOutput, error)
}

type StreamHandler func(text string)
```

### Input/Output

```go
type ConverseInput struct {
    Messages     []Message
    SystemPrompt string
    ToolSpecs    []ToolSpec
    ToolChoice   *ToolChoice
}

type ConverseOutput struct {
    StopReason StopReason
    Message    Message
    Usage      Usage
    Metrics    Metrics
}
```

---

## Messages

### Types

```go
type Role string
const (
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
)

type Message struct {
    Role    Role
    Content []ContentBlock
}

type ContentBlock struct {
    Type       ContentType
    Text       string
    ToolUse    *ToolUse
    ToolResult *ToolResult
}

type ContentType string
const (
    ContentTypeText       ContentType = "text"
    ContentTypeToolUse    ContentType = "tool_use"
    ContentTypeToolResult ContentType = "tool_result"
)
```

### Message Methods

```go
func (m Message) Text() string          // Concatenated text content
func (m Message) ToolUses() []ToolUse   // All tool use blocks
```

### Constructors

```go
func UserMessage(text string) Message
func TextBlock(text string) ContentBlock
func ToolUseBlock(id, name string, input map[string]any) ContentBlock
func ToolResultBlock(result ToolResult) ContentBlock
```

---

## Tool Use and Results

```go
type ToolUse struct {
    ID    string
    Name  string
    Input map[string]any
}

type ToolResult struct {
    ToolUseID string
    Status    ToolResultStatus
    Content   []ToolResultContent
}

type ToolResultStatus string
const (
    ToolResultSuccess ToolResultStatus = "success"
    ToolResultError   ToolResultStatus = "error"
)

type ToolResultContent struct {
    Text string
}
```

### Result Constructors

```go
func TextResult(toolUseID, text string) ToolResult
func ErrorResult(toolUseID, errMsg string) ToolResult
```

---

## Tools

### Interface

```go
type Tool interface {
    Spec() ToolSpec
    Execute(ctx context.Context, toolUseID string, input map[string]any) ToolResult
}

type ToolSpec struct {
    Name        string
    Description string
    InputSchema any    // JSON Schema
}
```

### FuncTool

```go
type ToolFunc func(ctx context.Context, input map[string]any) (any, error)

func NewFuncTool(name, description string, fn ToolFunc, inputSchema any) *FuncTool
```

### ToolRegistry

```go
func NewToolRegistry() *ToolRegistry

func (r *ToolRegistry) Register(tool Tool) error
func (r *ToolRegistry) Get(name string) (Tool, bool)
func (r *ToolRegistry) Specs() []ToolSpec
func (r *ToolRegistry) Names() []string
func (r *ToolRegistry) Len() int
```

Tool names must match `^[a-zA-Z0-9_-]{1,64}$`.

---

## Tool Choice

```go
type ToolChoice struct {
    Type ToolChoiceType
    Name string         // Only for ToolChoiceTool
}

type ToolChoiceType string
const (
    ToolChoiceAuto ToolChoiceType = "auto"
    ToolChoiceAny  ToolChoiceType = "any"
    ToolChoiceTool ToolChoiceType = "tool"
)
```

---

## Hooks

### Event Types

```go
type BeforeModelCallEvent struct {
    Agent    *Agent
    Messages []Message
}

type AfterModelCallEvent struct {
    Agent    *Agent
    Response *ConverseOutput
    Err      error
    Retry    bool            // Set to true to retry
}

type BeforeToolCallEvent struct {
    Agent     *Agent
    ToolUse   ToolUse
    Cancel    bool           // Set to true to cancel
    CancelMsg string
}

type AfterToolCallEvent struct {
    Agent   *Agent
    ToolUse ToolUse
    Result  ToolResult      // Can be modified
    Err     error
    Retry   bool            // Set to true to retry
}

type MessageAddedEvent struct {
    Agent   *Agent
    Message Message
}
```

### HookRegistry

```go
func NewHookRegistry() *HookRegistry

func (h *HookRegistry) OnBeforeModelCall(fn func(*BeforeModelCallEvent))
func (h *HookRegistry) OnAfterModelCall(fn func(*AfterModelCallEvent))
func (h *HookRegistry) OnBeforeToolCall(fn func(*BeforeToolCallEvent))
func (h *HookRegistry) OnAfterToolCall(fn func(*AfterToolCallEvent))
func (h *HookRegistry) OnMessageAdded(fn func(*MessageAddedEvent))
```

Before\* hooks fire FIFO. After\* hooks fire LIFO.

---

## Executors

```go
type ToolExecutor interface {
    Execute(ctx context.Context, agent *Agent, toolUses []ToolUse) []ToolResult
}

type ConcurrentExecutor struct{}  // Default — goroutines via sync.WaitGroup
type SequentialExecutor struct{}  // One at a time, in order
```

---

## Conversation Management

```go
type ConversationManager interface {
    ReduceContext(messages []Message) []Message
}

type SlidingWindowManager struct {
    WindowSize int  // Default: 100
}

type NullManager struct{}  // No trimming
```

---

## Streaming Events

```go
type Event struct {
    Type      EventType
    Text      string         // EventTextDelta
    ToolName  string         // EventToolStart, EventToolEnd
    ToolInput map[string]any // EventToolStart
    Result    *AgentResult   // EventComplete
    Error     error          // EventError
}

type EventType string
const (
    EventTextDelta EventType = "text_delta"
    EventToolStart EventType = "tool_start"
    EventToolEnd   EventType = "tool_end"
    EventComplete  EventType = "complete"
    EventError     EventType = "error"
)

func (e Event) String() string
```

---

## Usage and Metrics

```go
type Usage struct {
    InputTokens  int
    OutputTokens int
    TotalTokens  int
}

func (u *Usage) Add(other Usage) // Accumulate

type Metrics struct {
    LatencyMs int64
}
```

---

## Stop Reasons

```go
type StopReason string
const (
    StopReasonEndTurn   StopReason = "end_turn"
    StopReasonToolUse   StopReason = "tool_use"
    StopReasonMaxTokens StopReason = "max_tokens"
)
```

---

## Errors

```go
var (
    ErrMaxTokensReached = errors.New("strands: model response truncated (max_tokens reached)")
    ErrContextOverflow  = errors.New("strands: context window overflow")
    ErrToolNotFound     = errors.New("strands: tool not found")
    ErrNoModel          = errors.New("strands: no model configured")
    ErrMaxCycles        = errors.New("strands: maximum event loop cycles exceeded")
)
```
