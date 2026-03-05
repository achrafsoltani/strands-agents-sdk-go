package strands

import "errors"

var (
	// ErrMaxTokensReached is returned when the model's response was truncated.
	ErrMaxTokensReached = errors.New("strands: model response truncated (max_tokens reached)")

	// ErrContextOverflow is returned when the conversation exceeds the model's context window.
	ErrContextOverflow = errors.New("strands: context window overflow")

	// ErrToolNotFound is returned when the model requests a tool that is not registered.
	ErrToolNotFound = errors.New("strands: tool not found")

	// ErrNoModel is returned when an agent is invoked without a configured model.
	ErrNoModel = errors.New("strands: no model configured")

	// ErrMaxCycles is returned when the event loop exceeds the maximum number of cycles.
	ErrMaxCycles = errors.New("strands: maximum event loop cycles exceeded")
)
