package strands

import (
	"context"
	"fmt"
	"sync"
)

// MockModel is a test double for the Model interface. It returns pre-configured
// responses in sequence and records all calls for assertions.
type MockModel struct {
	mu        sync.Mutex
	responses []*ConverseOutput
	errors    []error // optional per-call errors (nil = no error)
	Calls     []*ConverseInput
	callIdx   int
}

// NewMockModel creates a mock that returns the given responses in order.
func NewMockModel(responses ...*ConverseOutput) *MockModel {
	return &MockModel{responses: responses}
}

// NewMockModelWithErrors creates a mock with per-call error control.
func NewMockModelWithErrors(responses []*ConverseOutput, errors []error) *MockModel {
	return &MockModel{responses: responses, errors: errors}
}

func (m *MockModel) Converse(ctx context.Context, input *ConverseInput) (*ConverseOutput, error) {
	return m.ConverseStream(ctx, input, nil)
}

func (m *MockModel) ConverseStream(ctx context.Context, input *ConverseInput, handler StreamHandler) (*ConverseOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Deep copy messages to capture state at call time.
	inputCopy := &ConverseInput{
		SystemPrompt: input.SystemPrompt,
		ToolSpecs:    input.ToolSpecs,
		ToolChoice:   input.ToolChoice,
	}
	inputCopy.Messages = make([]Message, len(input.Messages))
	copy(inputCopy.Messages, input.Messages)
	m.Calls = append(m.Calls, inputCopy)

	idx := m.callIdx
	m.callIdx++

	// Check for error.
	if idx < len(m.errors) && m.errors[idx] != nil {
		return nil, m.errors[idx]
	}

	if idx >= len(m.responses) {
		return nil, fmt.Errorf("mock: no more responses (call %d)", idx+1)
	}

	resp := m.responses[idx]

	// Stream text deltas via handler.
	if handler != nil {
		for _, b := range resp.Message.Content {
			if b.Type == ContentTypeText {
				handler(b.Text)
			}
		}
	}

	return resp, nil
}

// --- Helper functions for building test responses ---

// textResponse creates a ConverseOutput with a simple text response (end_turn).
func textResponse(text string) *ConverseOutput {
	return &ConverseOutput{
		StopReason: StopReasonEndTurn,
		Message: Message{
			Role:    RoleAssistant,
			Content: []ContentBlock{TextBlock(text)},
		},
		Usage: Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}
}

// toolUseResponse creates a ConverseOutput requesting one or more tool calls.
func toolUseResponse(toolUses ...ToolUse) *ConverseOutput {
	var blocks []ContentBlock
	for _, tu := range toolUses {
		blocks = append(blocks, ToolUseBlock(tu.ID, tu.Name, tu.Input))
	}
	return &ConverseOutput{
		StopReason: StopReasonToolUse,
		Message: Message{
			Role:    RoleAssistant,
			Content: blocks,
		},
		Usage: Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}
}

// maxTokensResponse creates a ConverseOutput with max_tokens stop reason.
func maxTokensResponse() *ConverseOutput {
	return &ConverseOutput{
		StopReason: StopReasonMaxTokens,
		Message: Message{
			Role:    RoleAssistant,
			Content: []ContentBlock{TextBlock("truncat")},
		},
	}
}

// echoTool creates a tool that returns its "text" input as the result.
func echoTool() *FuncTool {
	return NewFuncTool(
		"echo",
		"Echoes the input text back",
		func(_ context.Context, input map[string]any) (any, error) {
			text, _ := input["text"].(string)
			return text, nil
		},
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string"},
			},
			"required": []string{"text"},
		},
	)
}

// failTool creates a tool that always returns an error.
func failTool() *FuncTool {
	return NewFuncTool(
		"fail",
		"Always fails",
		func(_ context.Context, input map[string]any) (any, error) {
			return nil, fmt.Errorf("intentional error")
		},
		map[string]any{"type": "object", "properties": map[string]any{}},
	)
}
