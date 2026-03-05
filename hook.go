package strands

import "sync"

// BeforeModelCallEvent fires before each model invocation.
type BeforeModelCallEvent struct {
	Agent    *Agent
	Messages []Message
}

// AfterModelCallEvent fires after each model invocation.
// Set Retry to true to discard the response and re-invoke the model.
type AfterModelCallEvent struct {
	Agent    *Agent
	Response *ConverseOutput
	Err      error
	Retry    bool
}

// BeforeToolCallEvent fires before each tool execution.
// Set Cancel to true to skip execution and return an error result.
type BeforeToolCallEvent struct {
	Agent     *Agent
	ToolUse   ToolUse
	Cancel    bool
	CancelMsg string
}

// AfterToolCallEvent fires after each tool execution.
// Set Retry to true to discard the result and re-execute the tool.
type AfterToolCallEvent struct {
	Agent   *Agent
	ToolUse ToolUse
	Result  ToolResult
	Err     error
	Retry   bool
}

// MessageAddedEvent fires whenever a message is appended to the conversation.
type MessageAddedEvent struct {
	Agent   *Agent
	Message Message
}

// HookRegistry manages lifecycle event callbacks.
type HookRegistry struct {
	mu              sync.RWMutex
	beforeModelCall []func(*BeforeModelCallEvent)
	afterModelCall  []func(*AfterModelCallEvent)
	beforeToolCall  []func(*BeforeToolCallEvent)
	afterToolCall   []func(*AfterToolCallEvent)
	messageAdded    []func(*MessageAddedEvent)
}

// NewHookRegistry creates an empty hook registry.
func NewHookRegistry() *HookRegistry {
	return &HookRegistry{}
}

func (h *HookRegistry) OnBeforeModelCall(fn func(*BeforeModelCallEvent)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.beforeModelCall = append(h.beforeModelCall, fn)
}

func (h *HookRegistry) OnAfterModelCall(fn func(*AfterModelCallEvent)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.afterModelCall = append(h.afterModelCall, fn)
}

func (h *HookRegistry) OnBeforeToolCall(fn func(*BeforeToolCallEvent)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.beforeToolCall = append(h.beforeToolCall, fn)
}

func (h *HookRegistry) OnAfterToolCall(fn func(*AfterToolCallEvent)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.afterToolCall = append(h.afterToolCall, fn)
}

func (h *HookRegistry) OnMessageAdded(fn func(*MessageAddedEvent)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messageAdded = append(h.messageAdded, fn)
}

func (h *HookRegistry) invokeBeforeModelCall(e *BeforeModelCallEvent) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, fn := range h.beforeModelCall {
		fn(e)
	}
}

func (h *HookRegistry) invokeAfterModelCall(e *AfterModelCallEvent) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	// After events fire in reverse order (LIFO), matching the Python SDK.
	for i := len(h.afterModelCall) - 1; i >= 0; i-- {
		h.afterModelCall[i](e)
	}
}

func (h *HookRegistry) invokeBeforeToolCall(e *BeforeToolCallEvent) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, fn := range h.beforeToolCall {
		fn(e)
	}
}

func (h *HookRegistry) invokeAfterToolCall(e *AfterToolCallEvent) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for i := len(h.afterToolCall) - 1; i >= 0; i-- {
		h.afterToolCall[i](e)
	}
}

func (h *HookRegistry) invokeMessageAdded(e *MessageAddedEvent) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, fn := range h.messageAdded {
		fn(e)
	}
}
