package strands

import (
	"context"
	"sync"
)

// ToolExecutor controls how multiple tool calls are executed.
type ToolExecutor interface {
	Execute(ctx context.Context, agent *Agent, toolUses []ToolUse) []ToolResult
}

// SequentialExecutor executes tools one at a time, in order.
type SequentialExecutor struct{}

func (e *SequentialExecutor) Execute(ctx context.Context, agent *Agent, toolUses []ToolUse) []ToolResult {
	results := make([]ToolResult, 0, len(toolUses))
	for _, tu := range toolUses {
		result := executeSingleTool(ctx, agent, tu)
		results = append(results, result)
	}
	return results
}

// ConcurrentExecutor executes all tools in parallel. This is the default.
type ConcurrentExecutor struct{}

func (e *ConcurrentExecutor) Execute(ctx context.Context, agent *Agent, toolUses []ToolUse) []ToolResult {
	if len(toolUses) == 1 {
		return []ToolResult{executeSingleTool(ctx, agent, toolUses[0])}
	}

	results := make([]ToolResult, len(toolUses))
	var wg sync.WaitGroup
	for i, tu := range toolUses {
		wg.Add(1)
		go func(i int, tu ToolUse) {
			defer wg.Done()
			results[i] = executeSingleTool(ctx, agent, tu)
		}(i, tu)
	}
	wg.Wait()
	return results
}

// executeSingleTool runs one tool with hook support.
func executeSingleTool(ctx context.Context, agent *Agent, tu ToolUse) ToolResult {
	// Fire BeforeToolCallEvent.
	beforeEvent := &BeforeToolCallEvent{Agent: agent, ToolUse: tu}
	agent.Hooks.invokeBeforeToolCall(beforeEvent)
	if beforeEvent.Cancel {
		msg := beforeEvent.CancelMsg
		if msg == "" {
			msg = "tool call cancelled by hook"
		}
		return ErrorResult(tu.ID, msg)
	}

	// Look up tool.
	tool, ok := agent.Tools.Get(tu.Name)
	if !ok {
		return ErrorResult(tu.ID, "tool not found: "+tu.Name)
	}

	// Execute with retry support.
	const maxRetries = 3
	var result ToolResult
	for attempt := 0; attempt < maxRetries; attempt++ {
		result = tool.Execute(ctx, tu.ID, tu.Input)

		afterEvent := &AfterToolCallEvent{
			Agent:   agent,
			ToolUse: tu,
			Result:  result,
		}
		agent.Hooks.invokeAfterToolCall(afterEvent)
		result = afterEvent.Result // hooks may modify

		if !afterEvent.Retry {
			break
		}
	}
	return result
}
