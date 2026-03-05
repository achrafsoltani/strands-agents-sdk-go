package strands

import "testing"

func TestHookRegistry_BeforeModelCall(t *testing.T) {
	h := NewHookRegistry()
	var called bool
	h.OnBeforeModelCall(func(e *BeforeModelCallEvent) {
		called = true
	})

	h.invokeBeforeModelCall(&BeforeModelCallEvent{})
	if !called {
		t.Error("BeforeModelCallEvent callback not invoked")
	}
}

func TestHookRegistry_AfterModelCall_LIFO(t *testing.T) {
	h := NewHookRegistry()
	var order []int
	h.OnAfterModelCall(func(e *AfterModelCallEvent) { order = append(order, 1) })
	h.OnAfterModelCall(func(e *AfterModelCallEvent) { order = append(order, 2) })
	h.OnAfterModelCall(func(e *AfterModelCallEvent) { order = append(order, 3) })

	h.invokeAfterModelCall(&AfterModelCallEvent{})
	if len(order) != 3 || order[0] != 3 || order[1] != 2 || order[2] != 1 {
		t.Errorf("After hooks should fire LIFO, got %v", order)
	}
}

func TestHookRegistry_AfterModelCall_RetryFlag(t *testing.T) {
	h := NewHookRegistry()
	h.OnAfterModelCall(func(e *AfterModelCallEvent) {
		if e.Err != nil {
			e.Retry = true
		}
	})

	event := &AfterModelCallEvent{Err: ErrMaxTokensReached}
	h.invokeAfterModelCall(event)
	if !event.Retry {
		t.Error("expected Retry to be set")
	}
}

func TestHookRegistry_BeforeToolCall_Cancel(t *testing.T) {
	h := NewHookRegistry()
	h.OnBeforeToolCall(func(e *BeforeToolCallEvent) {
		if e.ToolUse.Name == "dangerous" {
			e.Cancel = true
			e.CancelMsg = "blocked by policy"
		}
	})

	event := &BeforeToolCallEvent{
		ToolUse: ToolUse{Name: "dangerous"},
	}
	h.invokeBeforeToolCall(event)
	if !event.Cancel {
		t.Error("expected Cancel to be set")
	}
	if event.CancelMsg != "blocked by policy" {
		t.Errorf("CancelMsg = %q", event.CancelMsg)
	}
}

func TestHookRegistry_AfterToolCall_ModifyResult(t *testing.T) {
	h := NewHookRegistry()
	h.OnAfterToolCall(func(e *AfterToolCallEvent) {
		// Append annotation to the result.
		e.Result.Content = append(e.Result.Content, ToolResultContent{Text: " (reviewed)"})
	})

	result := TextResult("tu_1", "original")
	event := &AfterToolCallEvent{Result: result}
	h.invokeAfterToolCall(event)

	if len(event.Result.Content) != 2 {
		t.Fatalf("expected 2 content items, got %d", len(event.Result.Content))
	}
	if event.Result.Content[1].Text != " (reviewed)" {
		t.Errorf("Content[1] = %q", event.Result.Content[1].Text)
	}
}

func TestHookRegistry_MessageAdded(t *testing.T) {
	h := NewHookRegistry()
	var messages []string
	h.OnMessageAdded(func(e *MessageAddedEvent) {
		messages = append(messages, e.Message.Text())
	})

	h.invokeMessageAdded(&MessageAddedEvent{Message: UserMessage("hello")})
	h.invokeMessageAdded(&MessageAddedEvent{Message: UserMessage("world")})

	if len(messages) != 2 || messages[0] != "hello" || messages[1] != "world" {
		t.Errorf("messages = %v", messages)
	}
}

func TestHookRegistry_MultipleCallbacks(t *testing.T) {
	h := NewHookRegistry()
	count := 0
	h.OnBeforeModelCall(func(e *BeforeModelCallEvent) { count++ })
	h.OnBeforeModelCall(func(e *BeforeModelCallEvent) { count++ })
	h.OnBeforeModelCall(func(e *BeforeModelCallEvent) { count++ })

	h.invokeBeforeModelCall(&BeforeModelCallEvent{})
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
}
