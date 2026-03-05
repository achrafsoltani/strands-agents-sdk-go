// Basic example demonstrating a Strands agent with tools.
//
// Set ANTHROPIC_API_KEY in your environment before running:
//
//	go run ./examples/basic
package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"

	strands "github.com/achrafsoltani/strands-agents-sdk-go"
	"github.com/achrafsoltani/strands-agents-sdk-go/provider/anthropic"
)

func main() {
	// Create the model provider.
	model := anthropic.New(
		anthropic.WithModel("claude-sonnet-4-20250514"),
		anthropic.WithMaxTokens(4096),
	)

	// Define tools.
	wordCount := strands.NewFuncTool(
		"word_count",
		"Count the number of words in the given text.",
		func(_ context.Context, input map[string]any) (any, error) {
			text, _ := input["text"].(string)
			return len(strings.Fields(text)), nil
		},
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{
					"type":        "string",
					"description": "The text to count words in",
				},
			},
			"required": []string{"text"},
		},
	)

	calculator := strands.NewFuncTool(
		"calculator",
		"Evaluate a mathematical expression. Supports: add, subtract, multiply, divide, sqrt.",
		func(_ context.Context, input map[string]any) (any, error) {
			op, _ := input["operation"].(string)
			a, _ := input["a"].(float64)
			b, _ := input["b"].(float64)

			switch op {
			case "add":
				return a + b, nil
			case "subtract":
				return a - b, nil
			case "multiply":
				return a * b, nil
			case "divide":
				if b == 0 {
					return nil, fmt.Errorf("division by zero")
				}
				return a / b, nil
			case "sqrt":
				return math.Sqrt(a), nil
			default:
				return nil, fmt.Errorf("unknown operation: %s", op)
			}
		},
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operation": map[string]any{
					"type":        "string",
					"description": "The operation to perform",
					"enum":        []string{"add", "subtract", "multiply", "divide", "sqrt"},
				},
				"a": map[string]any{
					"type":        "number",
					"description": "First operand",
				},
				"b": map[string]any{
					"type":        "number",
					"description": "Second operand (not used for sqrt)",
				},
			},
			"required": []string{"operation", "a"},
		},
	)

	// Create the agent.
	agent := strands.NewAgent(
		strands.WithModel(model),
		strands.WithTools(wordCount, calculator),
		strands.WithSystemPrompt("You are a helpful assistant. Use the available tools when needed."),
	)

	// --- Example 1: Synchronous invocation ---
	fmt.Println("=== Synchronous Invocation ===")
	result, err := agent.Invoke(context.Background(), "What is the square root of 1764?")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Message.Text())
	fmt.Printf("Tokens: %d in / %d out\n\n", result.Usage.InputTokens, result.Usage.OutputTokens)

	// --- Example 2: Streaming ---
	fmt.Println("=== Streaming ===")
	for event := range agent.Stream(context.Background(), "Count the words in 'The quick brown fox jumps over the lazy dog' and then calculate 42 * 17.") {
		switch event.Type {
		case strands.EventTextDelta:
			fmt.Print(event.Text)
		case strands.EventToolStart:
			fmt.Printf("\n[calling %s...]\n", event.ToolName)
		case strands.EventToolEnd:
			fmt.Printf("[%s done]\n", event.ToolName)
		case strands.EventComplete:
			fmt.Printf("\n\nTokens: %d in / %d out\n", event.Result.Usage.InputTokens, event.Result.Usage.OutputTokens)
		case strands.EventError:
			log.Fatal(event.Error)
		}
	}

}
