package strands

import (
	"context"
	"fmt"
)

// ToolSpec defines a tool's interface for the model.
type ToolSpec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

// Tool is the interface for executable tools.
type Tool interface {
	// Spec returns the tool's specification for the model.
	Spec() ToolSpec
	// Execute runs the tool with the given input and returns a result.
	Execute(ctx context.Context, toolUseID string, input map[string]any) ToolResult
}

// ToolFunc is a function signature for tool implementations.
type ToolFunc func(ctx context.Context, input map[string]any) (any, error)

// FuncTool wraps a plain function as a Tool.
type FuncTool struct {
	spec ToolSpec
	fn   ToolFunc
}

// NewFuncTool creates a tool from a function, name, description, and JSON Schema.
func NewFuncTool(name, description string, fn ToolFunc, inputSchema any) *FuncTool {
	return &FuncTool{
		spec: ToolSpec{
			Name:        name,
			Description: description,
			InputSchema: inputSchema,
		},
		fn: fn,
	}
}

func (t *FuncTool) Spec() ToolSpec { return t.spec }

func (t *FuncTool) Execute(ctx context.Context, toolUseID string, input map[string]any) ToolResult {
	result, err := t.fn(ctx, input)
	if err != nil {
		return ErrorResult(toolUseID, err.Error())
	}
	switch v := result.(type) {
	case string:
		return TextResult(toolUseID, v)
	case nil:
		return TextResult(toolUseID, "")
	default:
		return TextResult(toolUseID, fmt.Sprintf("%v", v))
	}
}
