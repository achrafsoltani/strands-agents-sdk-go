package strands

// ConversationManager handles context window overflow by trimming messages.
type ConversationManager interface {
	ReduceContext(messages []Message) []Message
}

// SlidingWindowManager keeps only the most recent messages.
type SlidingWindowManager struct {
	WindowSize int
}

func (m *SlidingWindowManager) ReduceContext(messages []Message) []Message {
	if m.WindowSize <= 0 || len(messages) <= m.WindowSize {
		return messages
	}
	return messages[len(messages)-m.WindowSize:]
}

// NullManager performs no context management.
type NullManager struct{}

func (m *NullManager) ReduceContext(messages []Message) []Message {
	return messages
}
