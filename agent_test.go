package strands

import (
	"context"
	"errors"
	"testing"
)

func TestNewAgent_Defaults(t *testing.T) {
	agent := NewAgent()

	if agent.Model != nil {
		t.Error("Model should be nil by default")
	}
	if agent.MaxCycles != 20 {
		t.Errorf("MaxCycles = %d, want 20", agent.MaxCycles)
	}
	if agent.Messages == nil {
		t.Error("Messages should be initialised")
	}
	if agent.State == nil {
		t.Error("State should be initialised")
	}
	if agent.Tools == nil {
		t.Error("Tools should be initialised")
	}
	if agent.Hooks == nil {
		t.Error("Hooks should be initialised")
	}
	if agent.Executor == nil {
		t.Error("Executor should be initialised")
	}
	if agent.Conversation == nil {
		t.Error("Conversation should be initialised")
	}
}

func TestNewAgent_WithOptions(t *testing.T) {
	model := NewMockModel(textResponse("ok"))
	state := map[string]any{"key": "value"}

	agent := NewAgent(
		WithModel(model),
		WithSystemPrompt("Be helpful"),
		WithTools(echoTool()),
		WithMaxCycles(5),
		WithSequentialExecution(),
		WithState(state),
	)

	if agent.Model == nil {
		t.Error("Model should be set")
	}
	if agent.SystemPrompt != "Be helpful" {
		t.Errorf("SystemPrompt = %q", agent.SystemPrompt)
	}
	if agent.MaxCycles != 5 {
		t.Errorf("MaxCycles = %d, want 5", agent.MaxCycles)
	}
	if _, ok := agent.Executor.(*SequentialExecutor); !ok {
		t.Errorf("Executor type = %T, want *SequentialExecutor", agent.Executor)
	}
	if agent.State["key"] != "value" {
		t.Errorf("State[key] = %v", agent.State["key"])
	}
	if _, found := agent.Tools.Get("echo"); !found {
		t.Error("echo tool not registered")
	}
}

func TestNewAgent_WithConversationManager(t *testing.T) {
	cm := &NullManager{}
	agent := NewAgent(WithConversationManager(cm))
	if _, ok := agent.Conversation.(*NullManager); !ok {
		t.Errorf("Conversation type = %T, want *NullManager", agent.Conversation)
	}
}

func TestAgent_Invoke_NoModel(t *testing.T) {
	agent := NewAgent()
	_, err := agent.Invoke(context.Background(), "hello")
	if !errors.Is(err, ErrNoModel) {
		t.Errorf("error = %v, want ErrNoModel", err)
	}
}

func TestAgent_Invoke_ReturnsResult(t *testing.T) {
	model := NewMockModel(textResponse("response"))
	agent := NewAgent(WithModel(model))

	result, err := agent.Invoke(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	if result.StopReason != StopReasonEndTurn {
		t.Errorf("StopReason = %q", result.StopReason)
	}
	if result.Message.Text() != "response" {
		t.Errorf("Message = %q", result.Message.Text())
	}
}

func TestAgent_Invoke_MessagesAccumulate(t *testing.T) {
	model := NewMockModel(textResponse("first"), textResponse("second"))
	agent := NewAgent(WithModel(model))

	_, err := agent.Invoke(context.Background(), "one")
	if err != nil {
		t.Fatalf("first Invoke failed: %v", err)
	}
	// After first invoke: [user("one"), assistant("first")]
	if len(agent.Messages) != 2 {
		t.Fatalf("Messages count after 1st invoke = %d, want 2", len(agent.Messages))
	}

	_, err = agent.Invoke(context.Background(), "two")
	if err != nil {
		t.Fatalf("second Invoke failed: %v", err)
	}
	// After second invoke: [user("one"), assistant("first"), user("two"), assistant("second")]
	if len(agent.Messages) != 4 {
		t.Fatalf("Messages count after 2nd invoke = %d, want 4", len(agent.Messages))
	}
}

func TestAgent_Invoke_StateReturnedInResult(t *testing.T) {
	model := NewMockModel(textResponse("ok"))
	agent := NewAgent(WithModel(model), WithState(map[string]any{"counter": 1}))

	result, err := agent.Invoke(context.Background(), "test")
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	if result.State["counter"] != 1 {
		t.Errorf("State[counter] = %v, want 1", result.State["counter"])
	}
}

func TestAgent_Invoke_UsageResetBetweenInvocations(t *testing.T) {
	model := NewMockModel(textResponse("a"), textResponse("b"))
	agent := NewAgent(WithModel(model))

	result1, _ := agent.Invoke(context.Background(), "first")
	result2, _ := agent.Invoke(context.Background(), "second")

	// Each textResponse has 10 input + 5 output. Single-cycle invocation.
	if result1.Usage.InputTokens != 10 {
		t.Errorf("result1 InputTokens = %d, want 10", result1.Usage.InputTokens)
	}
	if result2.Usage.InputTokens != 10 {
		t.Errorf("result2 InputTokens = %d, want 10 (should reset)", result2.Usage.InputTokens)
	}
}

func TestAgent_Stream_NoModel(t *testing.T) {
	agent := NewAgent()
	ch := agent.Stream(context.Background(), "hello")

	var events []Event
	for e := range ch {
		events = append(events, e)
	}

	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Type != EventError {
		t.Errorf("event type = %q, want error", events[0].Type)
	}
	if !errors.Is(events[0].Error, ErrNoModel) {
		t.Errorf("error = %v, want ErrNoModel", events[0].Error)
	}
}

func TestAgent_Stream_TextResponse(t *testing.T) {
	model := NewMockModel(textResponse("streamed text"))
	agent := NewAgent(WithModel(model))

	ch := agent.Stream(context.Background(), "hello")

	var events []Event
	for e := range ch {
		events = append(events, e)
	}

	// Should have at least a text delta and a complete event.
	hasTextDelta := false
	hasComplete := false
	for _, e := range events {
		if e.Type == EventTextDelta {
			hasTextDelta = true
		}
		if e.Type == EventComplete {
			hasComplete = true
			if e.Result == nil {
				t.Error("EventComplete should have Result")
			} else if e.Result.Message.Text() != "streamed text" {
				t.Errorf("Result.Message = %q", e.Result.Message.Text())
			}
		}
	}
	if !hasTextDelta {
		t.Error("expected at least one EventTextDelta")
	}
	if !hasComplete {
		t.Error("expected EventComplete")
	}
}

func TestAgent_Stream_WithToolCall(t *testing.T) {
	model := NewMockModel(
		toolUseResponse(ToolUse{ID: "tu_1", Name: "echo", Input: map[string]any{"text": "hi"}}),
		textResponse("done"),
	)
	agent := NewAgent(WithModel(model), WithTools(echoTool()))

	ch := agent.Stream(context.Background(), "test")

	var eventTypes []EventType
	for e := range ch {
		eventTypes = append(eventTypes, e.Type)
	}

	// Should contain tool_start and tool_end events.
	hasToolStart := false
	hasToolEnd := false
	for _, et := range eventTypes {
		if et == EventToolStart {
			hasToolStart = true
		}
		if et == EventToolEnd {
			hasToolEnd = true
		}
	}
	if !hasToolStart {
		t.Error("expected EventToolStart")
	}
	if !hasToolEnd {
		t.Error("expected EventToolEnd")
	}
}

func TestAgent_Stream_Error(t *testing.T) {
	model := NewMockModelWithErrors(nil, []error{errors.New("stream error")})
	agent := NewAgent(WithModel(model))

	ch := agent.Stream(context.Background(), "fail")

	var events []Event
	for e := range ch {
		events = append(events, e)
	}

	hasError := false
	for _, e := range events {
		if e.Type == EventError {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected EventError")
	}
}

func TestAgent_Stream_ContextCancelled(t *testing.T) {
	// Model that always returns tool use — loop would run forever.
	responses := make([]*ConverseOutput, 100)
	for i := range responses {
		responses[i] = toolUseResponse(ToolUse{ID: "tu_1", Name: "echo", Input: map[string]any{"text": "x"}})
	}
	model := NewMockModel(responses...)
	agent := NewAgent(WithModel(model), WithTools(echoTool()), WithMaxCycles(50))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := agent.Stream(ctx, "loop")

	// Read a few events then cancel.
	count := 0
	for range ch {
		count++
		if count >= 3 {
			cancel()
			break
		}
	}
	// Drain remaining events.
	for range ch {
	}
	// If we got here without hanging, the test passes.
}

func TestAgent_DefaultExecutor_IsConcurrent(t *testing.T) {
	agent := NewAgent()
	if _, ok := agent.Executor.(*ConcurrentExecutor); !ok {
		t.Errorf("default Executor = %T, want *ConcurrentExecutor", agent.Executor)
	}
}

func TestAgent_DefaultConversation_IsSlidingWindow(t *testing.T) {
	agent := NewAgent()
	sw, ok := agent.Conversation.(*SlidingWindowManager)
	if !ok {
		t.Fatalf("default Conversation = %T, want *SlidingWindowManager", agent.Conversation)
	}
	if sw.WindowSize != 100 {
		t.Errorf("WindowSize = %d, want 100", sw.WindowSize)
	}
}
