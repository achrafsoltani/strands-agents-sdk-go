package strands

import (
	"context"
	"fmt"
	"time"
)

// runLoop is the core agent execution cycle. It calls the model, processes tool
// calls, and loops until the model returns end_turn or an error occurs.
// The optional handler receives streaming events as they occur.
func (a *Agent) runLoop(ctx context.Context, handler func(Event)) (*AgentResult, error) {
	maxCycles := a.MaxCycles
	if maxCycles <= 0 {
		maxCycles = 20
	}

	for cycle := 0; cycle < maxCycles; cycle++ {
		output, err := a.callModel(ctx, handler)
		if err != nil {
			return nil, fmt.Errorf("model call failed (cycle %d): %w", cycle+1, err)
		}

		a.accumulatedUsage.Add(output.Usage)

		// Ensure the assistant message has content. Some models return
		// end_turn with no content blocks after a tool-use cycle (the text
		// was already in the tool-use response). Bedrock and other APIs
		// reject messages with empty content arrays.
		if len(output.Message.Content) == 0 {
			output.Message.Content = []ContentBlock{TextBlock(" ")}
		}

		// Append the assistant's message to conversation history.
		a.appendMessage(output.Message)

		switch output.StopReason {
		case StopReasonEndTurn:
			return &AgentResult{
				StopReason: StopReasonEndTurn,
				Message:    output.Message,
				Usage:      a.accumulatedUsage,
				State:      a.State,
			}, nil

		case StopReasonMaxTokens:
			return nil, ErrMaxTokensReached

		case StopReasonToolUse:
			toolUses := output.Message.ToolUses()
			if len(toolUses) == 0 {
				return nil, fmt.Errorf("strands: model returned tool_use but no tool use blocks found")
			}

			// Notify streaming callers about tool execution.
			for _, tu := range toolUses {
				if handler != nil {
					handler(Event{
						Type:      EventToolStart,
						ToolName:  tu.Name,
						ToolInput: tu.Input,
					})
				}
			}

			// Execute tools.
			results := a.Executor.Execute(ctx, a, toolUses)

			// Notify streaming callers about tool completion.
			for _, tu := range toolUses {
				if handler != nil {
					handler(Event{
						Type:     EventToolEnd,
						ToolName: tu.Name,
					})
				}
			}

			// Build the tool results message and append.
			var blocks []ContentBlock
			for _, r := range results {
				blocks = append(blocks, ToolResultBlock(r))
			}
			toolMsg := Message{Role: RoleUser, Content: blocks}
			a.appendMessage(toolMsg)

			// Continue to next cycle — model will see the tool results.
			continue

		default:
			return nil, fmt.Errorf("strands: unexpected stop reason: %q", output.StopReason)
		}
	}

	return nil, ErrMaxCycles
}

// callModel invokes the model with retry support via hooks.
func (a *Agent) callModel(ctx context.Context, handler func(Event)) (*ConverseOutput, error) {
	input := &ConverseInput{
		Messages:     a.Messages,
		SystemPrompt: a.SystemPrompt,
		ToolSpecs:    a.Tools.Specs(),
	}

	const maxRetries = 6
	for attempt := 0; attempt < maxRetries; attempt++ {
		// Fire BeforeModelCallEvent.
		a.Hooks.invokeBeforeModelCall(&BeforeModelCallEvent{
			Agent:    a,
			Messages: a.Messages,
		})

		start := time.Now()

		// Build a stream handler that forwards text deltas to the caller.
		var streamHandler StreamHandler
		if handler != nil {
			streamHandler = func(text string) {
				handler(Event{Type: EventTextDelta, Text: text})
			}
		}

		output, err := a.Model.ConverseStream(ctx, input, streamHandler)

		if output != nil {
			output.Metrics.LatencyMs = time.Since(start).Milliseconds()
		}

		// Fire AfterModelCallEvent.
		afterEvent := &AfterModelCallEvent{
			Agent:    a,
			Response: output,
			Err:      err,
		}
		a.Hooks.invokeAfterModelCall(afterEvent)

		if afterEvent.Retry {
			continue
		}

		if err != nil {
			return nil, err
		}
		return output, nil
	}

	return nil, fmt.Errorf("strands: model call failed after %d retries", maxRetries)
}

// appendMessage adds a message to conversation history and fires the hook.
func (a *Agent) appendMessage(msg Message) {
	a.Messages = append(a.Messages, msg)
	a.Hooks.invokeMessageAdded(&MessageAddedEvent{Agent: a, Message: msg})
}
