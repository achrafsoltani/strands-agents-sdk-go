package strands

import "testing"

func TestMessage_Text(t *testing.T) {
	msg := Message{
		Role: RoleAssistant,
		Content: []ContentBlock{
			TextBlock("Hello "),
			TextBlock("world"),
		},
	}
	if got := msg.Text(); got != "Hello world" {
		t.Errorf("Text() = %q, want %q", got, "Hello world")
	}
}

func TestMessage_Text_Empty(t *testing.T) {
	msg := Message{Role: RoleAssistant}
	if got := msg.Text(); got != "" {
		t.Errorf("Text() = %q, want empty", got)
	}
}

func TestMessage_Text_IgnoresNonTextBlocks(t *testing.T) {
	msg := Message{
		Role: RoleAssistant,
		Content: []ContentBlock{
			TextBlock("text"),
			ToolUseBlock("id1", "tool", map[string]any{"x": 1}),
		},
	}
	if got := msg.Text(); got != "text" {
		t.Errorf("Text() = %q, want %q", got, "text")
	}
}

func TestMessage_ToolUses(t *testing.T) {
	msg := Message{
		Role: RoleAssistant,
		Content: []ContentBlock{
			TextBlock("Let me call some tools"),
			ToolUseBlock("id1", "calculator", map[string]any{"expr": "2+2"}),
			ToolUseBlock("id2", "search", map[string]any{"q": "go"}),
		},
	}
	uses := msg.ToolUses()
	if len(uses) != 2 {
		t.Fatalf("ToolUses() returned %d, want 2", len(uses))
	}
	if uses[0].Name != "calculator" {
		t.Errorf("ToolUses()[0].Name = %q, want %q", uses[0].Name, "calculator")
	}
	if uses[1].Name != "search" {
		t.Errorf("ToolUses()[1].Name = %q, want %q", uses[1].Name, "search")
	}
}

func TestMessage_ToolUses_Empty(t *testing.T) {
	msg := Message{Role: RoleAssistant, Content: []ContentBlock{TextBlock("no tools")}}
	if uses := msg.ToolUses(); len(uses) != 0 {
		t.Errorf("ToolUses() returned %d, want 0", len(uses))
	}
}

func TestUserMessage(t *testing.T) {
	msg := UserMessage("hello")
	if msg.Role != RoleUser {
		t.Errorf("Role = %q, want %q", msg.Role, RoleUser)
	}
	if len(msg.Content) != 1 || msg.Content[0].Type != ContentTypeText || msg.Content[0].Text != "hello" {
		t.Errorf("unexpected content: %+v", msg.Content)
	}
}

func TestTextResult(t *testing.T) {
	r := TextResult("id1", "success value")
	if r.ToolUseID != "id1" {
		t.Errorf("ToolUseID = %q, want %q", r.ToolUseID, "id1")
	}
	if r.Status != ToolResultSuccess {
		t.Errorf("Status = %q, want %q", r.Status, ToolResultSuccess)
	}
	if len(r.Content) != 1 || r.Content[0].Text != "success value" {
		t.Errorf("unexpected content: %+v", r.Content)
	}
}

func TestErrorResult(t *testing.T) {
	r := ErrorResult("id2", "something broke")
	if r.Status != ToolResultError {
		t.Errorf("Status = %q, want %q", r.Status, ToolResultError)
	}
	if r.Content[0].Text != "something broke" {
		t.Errorf("Content = %q, want %q", r.Content[0].Text, "something broke")
	}
}

func TestUsage_Add(t *testing.T) {
	u := Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}
	u.Add(Usage{InputTokens: 20, OutputTokens: 10, TotalTokens: 30})
	if u.InputTokens != 30 || u.OutputTokens != 15 || u.TotalTokens != 45 {
		t.Errorf("Usage.Add produced %+v", u)
	}
}
