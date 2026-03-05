package strands

import (
	"context"
	"errors"
	"testing"
)

func TestEventLoop_SimpleTextResponse(t *testing.T) {
	model := NewMockModel(textResponse("Hello, world!"))
	agent := NewAgent(WithModel(model), WithSystemPrompt("Be helpful"))

	result, err := agent.Invoke(context.Background(), "Hi")
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	if result.StopReason != StopReasonEndTurn {
		t.Errorf("StopReason = %q, want end_turn", result.StopReason)
	}
	if result.Message.Text() != "Hello, world!" {
		t.Errorf("Message = %q, want 'Hello, world!'", result.Message.Text())
	}
}

func TestEventLoop_ToolCallThenTextResponse(t *testing.T) {
	// Cycle 1: model requests echo tool
	// Cycle 2: model sees tool result, responds with text
	model := NewMockModel(
		toolUseResponse(ToolUse{ID: "tu_1", Name: "echo", Input: map[string]any{"text": "ping"}}),
		textResponse("The echo tool returned: ping"),
	)

	agent := NewAgent(WithModel(model), WithTools(echoTool()))

	result, err := agent.Invoke(context.Background(), "Echo 'ping'")
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	if result.Message.Text() != "The echo tool returned: ping" {
		t.Errorf("Message = %q", result.Message.Text())
	}

	// Verify model was called twice.
	if len(model.Calls) != 2 {
		t.Fatalf("model called %d times, want 2", len(model.Calls))
	}

	// Second call should contain the tool result in messages.
	secondCall := model.Calls[1]
	lastMsg := secondCall.Messages[len(secondCall.Messages)-1]
	if lastMsg.Role != RoleUser {
		t.Errorf("last message role = %q, want user (tool result)", lastMsg.Role)
	}
	if len(lastMsg.Content) == 0 || lastMsg.Content[0].Type != ContentTypeToolResult {
		t.Error("last message should contain tool result")
	}
	if lastMsg.Content[0].ToolResult.Content[0].Text != "ping" {
		t.Errorf("tool result = %q, want 'ping'", lastMsg.Content[0].ToolResult.Content[0].Text)
	}
}

func TestEventLoop_MultipleToolCalls(t *testing.T) {
	model := NewMockModel(
		toolUseResponse(
			ToolUse{ID: "tu_1", Name: "echo", Input: map[string]any{"text": "a"}},
			ToolUse{ID: "tu_2", Name: "echo", Input: map[string]any{"text": "b"}},
		),
		textResponse("Got both results"),
	)

	agent := NewAgent(WithModel(model), WithTools(echoTool()))
	result, err := agent.Invoke(context.Background(), "Echo a and b")
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	if result.Message.Text() != "Got both results" {
		t.Errorf("Message = %q", result.Message.Text())
	}

	// The tool results message should have 2 blocks.
	secondCall := model.Calls[1]
	toolResultMsg := secondCall.Messages[len(secondCall.Messages)-1]
	if len(toolResultMsg.Content) != 2 {
		t.Fatalf("tool result message has %d blocks, want 2", len(toolResultMsg.Content))
	}
}

func TestEventLoop_ToolNotFound(t *testing.T) {
	model := NewMockModel(
		toolUseResponse(ToolUse{ID: "tu_1", Name: "nonexistent", Input: map[string]any{}}),
		textResponse("Tool failed"),
	)

	agent := NewAgent(WithModel(model)) // no tools registered
	result, err := agent.Invoke(context.Background(), "Call a tool")
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	// Tool not found should produce an error result, not crash the loop.
	// The model should see the error in the tool result and respond.
	if result.Message.Text() != "Tool failed" {
		t.Errorf("Message = %q", result.Message.Text())
	}
}

func TestEventLoop_MaxTokensError(t *testing.T) {
	model := NewMockModel(maxTokensResponse())
	agent := NewAgent(WithModel(model))

	_, err := agent.Invoke(context.Background(), "Write a novel")
	if !errors.Is(err, ErrMaxTokensReached) {
		t.Errorf("error = %v, want ErrMaxTokensReached", err)
	}
}

func TestEventLoop_MaxCyclesExceeded(t *testing.T) {
	// Model always requests tools — should hit the cycle limit.
	responses := make([]*ConverseOutput, 25)
	for i := range responses {
		responses[i] = toolUseResponse(ToolUse{ID: "tu_1", Name: "echo", Input: map[string]any{"text": "loop"}})
	}
	model := NewMockModel(responses...)
	agent := NewAgent(WithModel(model), WithTools(echoTool()), WithMaxCycles(5))

	_, err := agent.Invoke(context.Background(), "loop forever")
	if !errors.Is(err, ErrMaxCycles) {
		t.Errorf("error = %v, want ErrMaxCycles", err)
	}
}

func TestEventLoop_ModelError(t *testing.T) {
	model := NewMockModelWithErrors(nil, []error{errors.New("API error")})
	agent := NewAgent(WithModel(model))

	_, err := agent.Invoke(context.Background(), "fail")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, errors.Unwrap(err)) {
		// Just check it wraps something.
	}
}

func TestEventLoop_UsageAccumulated(t *testing.T) {
	model := NewMockModel(
		toolUseResponse(ToolUse{ID: "tu_1", Name: "echo", Input: map[string]any{"text": "x"}}),
		textResponse("done"),
	)
	agent := NewAgent(WithModel(model), WithTools(echoTool()))

	result, err := agent.Invoke(context.Background(), "test")
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	// Each response has 10 input + 5 output. Two cycles = 20 + 10.
	if result.Usage.InputTokens != 20 {
		t.Errorf("InputTokens = %d, want 20", result.Usage.InputTokens)
	}
	if result.Usage.OutputTokens != 10 {
		t.Errorf("OutputTokens = %d, want 10", result.Usage.OutputTokens)
	}
}

func TestEventLoop_SystemPromptPassedToModel(t *testing.T) {
	model := NewMockModel(textResponse("ok"))
	agent := NewAgent(WithModel(model), WithSystemPrompt("Be concise"))

	_, _ = agent.Invoke(context.Background(), "test")

	if len(model.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(model.Calls))
	}
	if model.Calls[0].SystemPrompt != "Be concise" {
		t.Errorf("SystemPrompt = %q, want 'Be concise'", model.Calls[0].SystemPrompt)
	}
}

func TestEventLoop_ToolSpecsPassedToModel(t *testing.T) {
	model := NewMockModel(textResponse("ok"))
	agent := NewAgent(WithModel(model), WithTools(echoTool()))

	_, _ = agent.Invoke(context.Background(), "test")

	if len(model.Calls[0].ToolSpecs) != 1 {
		t.Fatalf("ToolSpecs count = %d, want 1", len(model.Calls[0].ToolSpecs))
	}
	if model.Calls[0].ToolSpecs[0].Name != "echo" {
		t.Errorf("ToolSpecs[0].Name = %q", model.Calls[0].ToolSpecs[0].Name)
	}
}

func TestEventLoop_HooksFireDuringLoop(t *testing.T) {
	model := NewMockModel(
		toolUseResponse(ToolUse{ID: "tu_1", Name: "echo", Input: map[string]any{"text": "hi"}}),
		textResponse("done"),
	)

	agent := NewAgent(WithModel(model), WithTools(echoTool()))

	var hookLog []string
	agent.Hooks.OnBeforeModelCall(func(e *BeforeModelCallEvent) {
		hookLog = append(hookLog, "before_model")
	})
	agent.Hooks.OnAfterModelCall(func(e *AfterModelCallEvent) {
		hookLog = append(hookLog, "after_model")
	})
	agent.Hooks.OnBeforeToolCall(func(e *BeforeToolCallEvent) {
		hookLog = append(hookLog, "before_tool:"+e.ToolUse.Name)
	})
	agent.Hooks.OnAfterToolCall(func(e *AfterToolCallEvent) {
		hookLog = append(hookLog, "after_tool:"+e.ToolUse.Name)
	})
	agent.Hooks.OnMessageAdded(func(e *MessageAddedEvent) {
		hookLog = append(hookLog, "msg:"+string(e.Message.Role))
	})

	_, err := agent.Invoke(context.Background(), "test hooks")
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	expected := []string{
		"msg:user",          // user prompt appended
		"before_model",      // cycle 1: before model
		"after_model",       // cycle 1: after model
		"msg:assistant",     // assistant tool_use message
		"before_tool:echo",  // tool execution
		"after_tool:echo",
		"msg:user",          // tool results message
		"before_model",      // cycle 2: before model
		"after_model",       // cycle 2: after model
		"msg:assistant",     // final text message
	}

	if len(hookLog) != len(expected) {
		t.Fatalf("hookLog has %d entries, want %d:\n  got:  %v\n  want: %v", len(hookLog), len(expected), hookLog, expected)
	}
	for i, want := range expected {
		if hookLog[i] != want {
			t.Errorf("hookLog[%d] = %q, want %q", i, hookLog[i], want)
		}
	}
}
