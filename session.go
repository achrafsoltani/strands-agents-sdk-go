package strands

// SessionManager persists agent state across process restarts.
// Implementations register as hook providers and react to lifecycle events.
type SessionManager interface {
	// Initialize loads an existing session or creates a new one.
	Initialize(agent *Agent) error
	// AppendMessage persists a new message.
	AppendMessage(msg Message, agent *Agent) error
	// Sync performs a full state sync (messages + state).
	Sync(agent *Agent) error
}
