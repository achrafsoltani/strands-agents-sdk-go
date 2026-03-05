package strands

import "context"

// ConverseInput holds the parameters for a model invocation.
type ConverseInput struct {
	Messages     []Message
	SystemPrompt string
	ToolSpecs    []ToolSpec
	ToolChoice   *ToolChoice
}

// ConverseOutput holds the result of a model invocation.
type ConverseOutput struct {
	StopReason StopReason
	Message    Message
	Usage      Usage
	Metrics    Metrics
}

// StreamHandler receives incremental text from the model during streaming.
type StreamHandler func(text string)

// Model is the interface that all model providers must implement.
// Converse performs a synchronous (non-streaming) invocation.
// ConverseStream streams text deltas via the handler and returns the final output.
type Model interface {
	Converse(ctx context.Context, input *ConverseInput) (*ConverseOutput, error)
	ConverseStream(ctx context.Context, input *ConverseInput, handler StreamHandler) (*ConverseOutput, error)
}
