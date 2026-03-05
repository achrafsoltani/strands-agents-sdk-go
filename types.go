package strands

import "fmt"

// Role represents the sender of a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// StopReason indicates why the model stopped generating.
type StopReason string

const (
	StopReasonEndTurn   StopReason = "end_turn"
	StopReasonToolUse   StopReason = "tool_use"
	StopReasonMaxTokens StopReason = "max_tokens"
)

// Message represents a conversation message.
type Message struct {
	Role    Role           `json:"role"`
	Content []ContentBlock `json:"content"`
}

// Text returns the concatenated text content of the message.
func (m Message) Text() string {
	var s string
	for _, b := range m.Content {
		if b.Type == ContentTypeText {
			s += b.Text
		}
	}
	return s
}

// ToolUses returns all tool use blocks in the message.
func (m Message) ToolUses() []ToolUse {
	var uses []ToolUse
	for _, b := range m.Content {
		if b.Type == ContentTypeToolUse && b.ToolUse != nil {
			uses = append(uses, *b.ToolUse)
		}
	}
	return uses
}

// ContentBlock represents a single content element in a message.
type ContentBlock struct {
	Type       ContentType `json:"type"`
	Text       string      `json:"text,omitempty"`
	ToolUse    *ToolUse    `json:"toolUse,omitempty"`
	ToolResult *ToolResult `json:"toolResult,omitempty"`
}

// ContentType identifies the kind of content block.
type ContentType string

const (
	ContentTypeText       ContentType = "text"
	ContentTypeToolUse    ContentType = "tool_use"
	ContentTypeToolResult ContentType = "tool_result"
)

// TextBlock creates a text content block.
func TextBlock(text string) ContentBlock {
	return ContentBlock{Type: ContentTypeText, Text: text}
}

// ToolUseBlock creates a tool use content block.
func ToolUseBlock(id, name string, input map[string]any) ContentBlock {
	return ContentBlock{
		Type:    ContentTypeToolUse,
		ToolUse: &ToolUse{ID: id, Name: name, Input: input},
	}
}

// ToolResultBlock creates a tool result content block.
func ToolResultBlock(result ToolResult) ContentBlock {
	return ContentBlock{
		Type:       ContentTypeToolResult,
		ToolResult: &result,
	}
}

// UserMessage creates a user message with text content.
func UserMessage(text string) Message {
	return Message{Role: RoleUser, Content: []ContentBlock{TextBlock(text)}}
}

// ToolUse represents the model's request to invoke a tool.
type ToolUse struct {
	ID    string         `json:"toolUseId"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// ToolResult represents the outcome of a tool execution.
type ToolResult struct {
	ToolUseID string           `json:"toolUseId"`
	Status    ToolResultStatus `json:"status"`
	Content   []ToolResultContent `json:"content"`
}

// ToolResultStatus indicates whether a tool executed successfully.
type ToolResultStatus string

const (
	ToolResultSuccess ToolResultStatus = "success"
	ToolResultError   ToolResultStatus = "error"
)

// ToolResultContent is a single piece of content in a tool result.
type ToolResultContent struct {
	Text string `json:"text,omitempty"`
}

// TextResult creates a successful text tool result.
func TextResult(toolUseID, text string) ToolResult {
	return ToolResult{
		ToolUseID: toolUseID,
		Status:    ToolResultSuccess,
		Content:   []ToolResultContent{{Text: text}},
	}
}

// ErrorResult creates an error tool result.
func ErrorResult(toolUseID, errMsg string) ToolResult {
	return ToolResult{
		ToolUseID: toolUseID,
		Status:    ToolResultError,
		Content:   []ToolResultContent{{Text: errMsg}},
	}
}

// Usage tracks token consumption.
type Usage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
	TotalTokens  int `json:"totalTokens"`
}

// Add accumulates usage from another Usage value.
func (u *Usage) Add(other Usage) {
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.TotalTokens += other.TotalTokens
}

// Metrics contains performance measurements.
type Metrics struct {
	LatencyMs int64 `json:"latencyMs"`
}

// ToolChoice controls how the model selects tools.
type ToolChoice struct {
	Type ToolChoiceType `json:"type"`
	Name string         `json:"name,omitempty"`
}

// ToolChoiceType identifies the tool selection strategy.
type ToolChoiceType string

const (
	ToolChoiceAuto ToolChoiceType = "auto"
	ToolChoiceAny  ToolChoiceType = "any"
	ToolChoiceTool ToolChoiceType = "tool"
)

// Event represents a streaming event emitted by the agent during execution.
type Event struct {
	Type      EventType
	Text      string         // EventTextDelta: incremental text from the model
	ToolName  string         // EventToolStart/End: name of the tool
	ToolInput map[string]any // EventToolStart: tool input
	Result    *AgentResult   // EventComplete: final result
	Error     error          // EventError: error that occurred
}

func (e Event) String() string {
	switch e.Type {
	case EventTextDelta:
		return e.Text
	case EventToolStart:
		return fmt.Sprintf("[tool: %s]", e.ToolName)
	case EventToolEnd:
		return fmt.Sprintf("[/tool: %s]", e.ToolName)
	case EventComplete:
		return "[complete]"
	case EventError:
		return fmt.Sprintf("[error: %v]", e.Error)
	default:
		return "[unknown]"
	}
}

// EventType identifies the kind of streaming event.
type EventType string

const (
	EventTextDelta EventType = "text_delta"
	EventToolStart EventType = "tool_start"
	EventToolEnd   EventType = "tool_end"
	EventComplete  EventType = "complete"
	EventError     EventType = "error"
)
