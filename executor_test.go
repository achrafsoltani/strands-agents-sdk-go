package strands

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func newTestAgent(tools ...Tool) *Agent {
	a := NewAgent(WithTools(tools...))
	return a
}

func TestSequentialExecutor_Execute(t *testing.T) {
	agent := newTestAgent(echoTool())
	exec := &SequentialExecutor{}

	toolUses := []ToolUse{
		{ID: "tu_1", Name: "echo", Input: map[string]any{"text": "first"}},
		{ID: "tu_2", Name: "echo", Input: map[string]any{"text": "second"}},
	}

	results := exec.Execute(context.Background(), agent, toolUses)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Content[0].Text != "first" {
		t.Errorf("results[0] = %q", results[0].Content[0].Text)
	}
	if results[1].Content[0].Text != "second" {
		t.Errorf("results[1] = %q", results[1].Content[0].Text)
	}
}

func TestConcurrentExecutor_Execute(t *testing.T) {
	agent := newTestAgent(echoTool())
	exec := &ConcurrentExecutor{}

	toolUses := []ToolUse{
		{ID: "tu_1", Name: "echo", Input: map[string]any{"text": "alpha"}},
		{ID: "tu_2", Name: "echo", Input: map[string]any{"text": "beta"}},
	}

	results := exec.Execute(context.Background(), agent, toolUses)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	// Results should be in the same order as tool uses (index-based).
	if results[0].Content[0].Text != "alpha" {
		t.Errorf("results[0] = %q, want alpha", results[0].Content[0].Text)
	}
	if results[1].Content[0].Text != "beta" {
		t.Errorf("results[1] = %q, want beta", results[1].Content[0].Text)
	}
}

func TestConcurrentExecutor_RunsInParallel(t *testing.T) {
	var running atomic.Int32
	var maxConcurrent atomic.Int32

	slowTool := NewFuncTool("slow", "takes time",
		func(_ context.Context, input map[string]any) (any, error) {
			cur := running.Add(1)
			for {
				old := maxConcurrent.Load()
				if cur <= old {
					break
				}
				if maxConcurrent.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			running.Add(-1)
			return "done", nil
		},
		map[string]any{"type": "object", "properties": map[string]any{}},
	)

	agent := newTestAgent(slowTool)
	exec := &ConcurrentExecutor{}

	toolUses := []ToolUse{
		{ID: "tu_1", Name: "slow", Input: map[string]any{}},
		{ID: "tu_2", Name: "slow", Input: map[string]any{}},
		{ID: "tu_3", Name: "slow", Input: map[string]any{}},
	}

	results := exec.Execute(context.Background(), agent, toolUses)
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	if maxConcurrent.Load() < 2 {
		t.Errorf("max concurrent = %d, expected at least 2", maxConcurrent.Load())
	}
}

func TestExecutor_ToolNotFound(t *testing.T) {
	agent := newTestAgent() // no tools registered
	exec := &SequentialExecutor{}

	results := exec.Execute(context.Background(), agent, []ToolUse{
		{ID: "tu_1", Name: "nonexistent", Input: map[string]any{}},
	})

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Status != ToolResultError {
		t.Errorf("Status = %q, want error", results[0].Status)
	}
}

func TestExecutor_ToolError(t *testing.T) {
	agent := newTestAgent(failTool())
	exec := &SequentialExecutor{}

	results := exec.Execute(context.Background(), agent, []ToolUse{
		{ID: "tu_1", Name: "fail", Input: map[string]any{}},
	})

	if results[0].Status != ToolResultError {
		t.Errorf("Status = %q, want error", results[0].Status)
	}
	if results[0].Content[0].Text != "intentional error" {
		t.Errorf("Content = %q", results[0].Content[0].Text)
	}
}

func TestExecutor_CancelViaHook(t *testing.T) {
	agent := newTestAgent(echoTool())
	agent.Hooks.OnBeforeToolCall(func(e *BeforeToolCallEvent) {
		e.Cancel = true
		e.CancelMsg = "blocked"
	})

	exec := &SequentialExecutor{}
	results := exec.Execute(context.Background(), agent, []ToolUse{
		{ID: "tu_1", Name: "echo", Input: map[string]any{"text": "hi"}},
	})

	if results[0].Status != ToolResultError {
		t.Errorf("Status = %q, want error", results[0].Status)
	}
	if results[0].Content[0].Text != "blocked" {
		t.Errorf("Content = %q, want 'blocked'", results[0].Content[0].Text)
	}
}

func TestExecutor_RetryViaHook(t *testing.T) {
	callCount := 0
	countingTool := NewFuncTool("counter", "counts calls",
		func(_ context.Context, input map[string]any) (any, error) {
			callCount++
			return callCount, nil
		},
		map[string]any{"type": "object", "properties": map[string]any{}},
	)

	agent := newTestAgent(countingTool)
	retried := false
	agent.Hooks.OnAfterToolCall(func(e *AfterToolCallEvent) {
		if !retried {
			retried = true
			e.Retry = true
		}
	})

	exec := &SequentialExecutor{}
	results := exec.Execute(context.Background(), agent, []ToolUse{
		{ID: "tu_1", Name: "counter", Input: map[string]any{}},
	})

	if callCount != 2 {
		t.Errorf("tool called %d times, want 2 (original + retry)", callCount)
	}
	// The final result should be from the retry (call 2).
	if results[0].Content[0].Text != "2" {
		t.Errorf("final result = %q, want '2'", results[0].Content[0].Text)
	}
}
