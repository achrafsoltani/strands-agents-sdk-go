package strands

import "context"

// Agent is the top-level orchestrator. It holds a model, tools, hooks,
// conversation history, and state. The LLM drives the control flow by
// deciding which tools to call and when to stop.
type Agent struct {
	Model        Model
	Messages     []Message
	SystemPrompt string
	State        map[string]any
	Tools        *ToolRegistry
	Hooks        *HookRegistry
	Executor     ToolExecutor
	Conversation ConversationManager
	MaxCycles    int // Maximum event loop cycles per invocation (default 20).

	accumulatedUsage Usage
}

// AgentResult is the outcome of a single agent invocation.
type AgentResult struct {
	StopReason StopReason     `json:"stopReason"`
	Message    Message        `json:"message"`
	Usage      Usage          `json:"usage"`
	State      map[string]any `json:"state,omitempty"`
}

// Option configures an Agent during construction.
type Option func(*Agent)

// NewAgent creates a new Agent with the given options.
func NewAgent(opts ...Option) *Agent {
	a := &Agent{
		Messages:     make([]Message, 0),
		State:        make(map[string]any),
		Tools:        NewToolRegistry(),
		Hooks:        NewHookRegistry(),
		Executor:     &ConcurrentExecutor{},
		Conversation: &SlidingWindowManager{WindowSize: 100},
		MaxCycles:    20,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Invoke sends a prompt to the agent and returns the final result synchronously.
func (a *Agent) Invoke(ctx context.Context, prompt string) (*AgentResult, error) {
	if a.Model == nil {
		return nil, ErrNoModel
	}

	// Reset accumulated usage for this invocation.
	a.accumulatedUsage = Usage{}

	// Append the user's prompt as a message.
	a.appendMessage(UserMessage(prompt))

	return a.runLoop(ctx, nil)
}

// Stream sends a prompt and returns a channel of streaming events.
// The channel is closed when the invocation completes. The final event
// is either EventComplete (with the AgentResult) or EventError.
func (a *Agent) Stream(ctx context.Context, prompt string) <-chan Event {
	ch := make(chan Event, 64)

	go func() {
		defer close(ch)

		if a.Model == nil {
			ch <- Event{Type: EventError, Error: ErrNoModel}
			return
		}

		a.accumulatedUsage = Usage{}
		a.appendMessage(UserMessage(prompt))

		result, err := a.runLoop(ctx, func(e Event) {
			select {
			case ch <- e:
			case <-ctx.Done():
			}
		})

		if err != nil {
			select {
			case ch <- Event{Type: EventError, Error: err}:
			case <-ctx.Done():
			}
			return
		}

		select {
		case ch <- Event{Type: EventComplete, Result: result}:
		case <-ctx.Done():
		}
	}()

	return ch
}

// WithModel sets the model provider.
func WithModel(m Model) Option {
	return func(a *Agent) { a.Model = m }
}

// WithSystemPrompt sets the system prompt.
func WithSystemPrompt(prompt string) Option {
	return func(a *Agent) { a.SystemPrompt = prompt }
}

// WithTools registers one or more tools.
func WithTools(tools ...Tool) Option {
	return func(a *Agent) {
		for _, t := range tools {
			_ = a.Tools.Register(t) // ignore duplicate errors during init
		}
	}
}

// WithMaxCycles sets the maximum number of event loop cycles per invocation.
func WithMaxCycles(n int) Option {
	return func(a *Agent) { a.MaxCycles = n }
}

// WithSequentialExecution configures the agent to execute tools sequentially.
func WithSequentialExecution() Option {
	return func(a *Agent) { a.Executor = &SequentialExecutor{} }
}

// WithConversationManager sets the conversation manager.
func WithConversationManager(cm ConversationManager) Option {
	return func(a *Agent) { a.Conversation = cm }
}

// WithState sets the initial agent state.
func WithState(state map[string]any) Option {
	return func(a *Agent) { a.State = state }
}
