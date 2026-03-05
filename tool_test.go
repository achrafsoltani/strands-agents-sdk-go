package strands

import (
	"context"
	"testing"
)

func TestFuncTool_Spec(t *testing.T) {
	tool := echoTool()
	spec := tool.Spec()
	if spec.Name != "echo" {
		t.Errorf("Name = %q, want %q", spec.Name, "echo")
	}
	if spec.Description != "Echoes the input text back" {
		t.Errorf("Description = %q", spec.Description)
	}
	if spec.InputSchema == nil {
		t.Error("InputSchema is nil")
	}
}

func TestFuncTool_Execute_Success(t *testing.T) {
	tool := echoTool()
	result := tool.Execute(context.Background(), "tu_1", map[string]any{"text": "hello"})
	if result.Status != ToolResultSuccess {
		t.Errorf("Status = %q, want success", result.Status)
	}
	if result.ToolUseID != "tu_1" {
		t.Errorf("ToolUseID = %q, want tu_1", result.ToolUseID)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "hello" {
		t.Errorf("Content = %+v, want [{Text:hello}]", result.Content)
	}
}

func TestFuncTool_Execute_Error(t *testing.T) {
	tool := failTool()
	result := tool.Execute(context.Background(), "tu_2", map[string]any{})
	if result.Status != ToolResultError {
		t.Errorf("Status = %q, want error", result.Status)
	}
	if result.Content[0].Text != "intentional error" {
		t.Errorf("Content = %q, want 'intentional error'", result.Content[0].Text)
	}
}

func TestFuncTool_Execute_NilResult(t *testing.T) {
	tool := NewFuncTool("noop", "does nothing",
		func(_ context.Context, input map[string]any) (any, error) {
			return nil, nil
		},
		map[string]any{"type": "object"},
	)
	result := tool.Execute(context.Background(), "tu_3", map[string]any{})
	if result.Status != ToolResultSuccess {
		t.Errorf("Status = %q, want success", result.Status)
	}
}

func TestFuncTool_Execute_NumericResult(t *testing.T) {
	tool := NewFuncTool("count", "counts",
		func(_ context.Context, input map[string]any) (any, error) {
			return 42, nil
		},
		map[string]any{"type": "object"},
	)
	result := tool.Execute(context.Background(), "tu_4", map[string]any{})
	if result.Content[0].Text != "42" {
		t.Errorf("Content = %q, want '42'", result.Content[0].Text)
	}
}
