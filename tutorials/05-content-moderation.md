# Tutorial 5: Content Moderation Pipeline

Build a content moderation system using all five hook types. This tutorial demonstrates the complete hook lifecycle, cancel and retry flags, LIFO ordering for After* hooks, and how hooks interact with the event loop.

## What You'll Learn

- All 5 hook types: `OnBeforeModelCall`, `OnAfterModelCall`, `OnBeforeToolCall`, `OnAfterToolCall`, `OnMessageAdded`
- Cancelling tool calls with the `Cancel` flag
- Retrying model calls with the `Retry` flag
- LIFO ordering for After* hooks
- Modifying tool results in hooks
- Building a complete moderation pipeline

## The Scenario

You're building a content review system. Users submit text for moderation. The agent analyses the content using classification and scoring tools, then produces a moderation decision. Hooks enforce policies at every stage:

- **BeforeModelCall** — injects moderation guidelines into the system prompt
- **AfterModelCall** — validates the model's response doesn't contain prohibited patterns
- **BeforeToolCall** — blocks tools when the content is flagged as severely toxic
- **AfterToolCall** — redacts sensitive terms from tool results
- **MessageAdded** — maintains an audit log of every message

## Complete Code

```go
package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	strands "github.com/achrafsoltani/strands-agents-sdk-go"
	"github.com/achrafsoltani/strands-agents-sdk-go/provider/anthropic"
)

// AuditLog stores all messages for compliance review.
type AuditLog struct {
	mu      sync.Mutex
	entries []AuditEntry
}

type AuditEntry struct {
	Timestamp time.Time
	Role      string
	Preview   string
	Blocks    int
}

func (a *AuditLog) Add(role, preview string, blocks int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = append(a.entries, AuditEntry{
		Timestamp: time.Now(),
		Role:      string(role),
		Preview:   preview,
		Blocks:    blocks,
	})
}

func (a *AuditLog) Print() {
	a.mu.Lock()
	defer a.mu.Unlock()
	fmt.Println("\n=== Audit Log ===")
	for i, e := range a.entries {
		preview := e.Preview
		if len(preview) > 60 {
			preview = preview[:60] + "..."
		}
		fmt.Printf("%d. [%s] role=%s blocks=%d: %s\n",
			i+1, e.Timestamp.Format("15:04:05"), e.Role, e.Blocks, preview)
	}
}

func main() {
	// --- Tools ---

	classifyContent := strands.NewFuncTool(
		"classify_content",
		"Classify text content into categories: safe, mild, moderate, severe. "+
			"Returns the classification with confidence score.",
		func(_ context.Context, input map[string]any) (any, error) {
			text, _ := input["text"].(string)
			text = strings.ToLower(text)

			// Simple keyword-based classification for the tutorial.
			severeWords := []string{"hack", "exploit", "attack", "destroy"}
			moderateWords := []string{"angry", "frustrated", "terrible", "awful"}
			mildWords := []string{"dislike", "boring", "mediocre", "disappointed"}

			for _, w := range severeWords {
				if strings.Contains(text, w) {
					return `{"category": "severe", "confidence": 0.92, "reason": "contains high-risk terminology"}`, nil
				}
			}
			for _, w := range moderateWords {
				if strings.Contains(text, w) {
					return `{"category": "moderate", "confidence": 0.85, "reason": "contains negative sentiment"}`, nil
				}
			}
			for _, w := range mildWords {
				if strings.Contains(text, w) {
					return `{"category": "mild", "confidence": 0.78, "reason": "contains mildly negative language"}`, nil
				}
			}
			return `{"category": "safe", "confidence": 0.95, "reason": "no concerning content detected"}`, nil
		},
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{
					"type":        "string",
					"description": "The text content to classify",
				},
			},
			"required": []string{"text"},
		},
	)

	scoreSentiment := strands.NewFuncTool(
		"score_sentiment",
		"Score the sentiment of text on a scale from -1.0 (very negative) to +1.0 (very positive).",
		func(_ context.Context, input map[string]any) (any, error) {
			text, _ := input["text"].(string)
			text = strings.ToLower(text)

			// Simple scoring for the tutorial.
			score := 0.0
			positive := []string{"good", "great", "excellent", "love", "happy", "wonderful"}
			negative := []string{"bad", "terrible", "hate", "angry", "awful", "horrible"}

			for _, w := range positive {
				if strings.Contains(text, w) {
					score += 0.3
				}
			}
			for _, w := range negative {
				if strings.Contains(text, w) {
					score -= 0.3
				}
			}
			if score > 1.0 {
				score = 1.0
			}
			if score < -1.0 {
				score = -1.0
			}

			return fmt.Sprintf(`{"score": %.2f, "label": "%s"}`, score, sentimentLabel(score)), nil
		},
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{
					"type":        "string",
					"description": "The text to score for sentiment",
				},
			},
			"required": []string{"text"},
		},
	)

	makeDecision := strands.NewFuncTool(
		"make_decision",
		"Record the final moderation decision for a piece of content.",
		func(_ context.Context, input map[string]any) (any, error) {
			decision, _ := input["decision"].(string)
			reason, _ := input["reason"].(string)
			return fmt.Sprintf("Decision recorded: %s — %s", decision, reason), nil
		},
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"decision": map[string]any{
					"type":        "string",
					"enum":        []string{"approve", "flag_for_review", "reject"},
					"description": "The moderation decision",
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "Explanation for the decision",
				},
			},
			"required": []string{"decision", "reason"},
		},
	)

	// --- Agent ---

	agent := strands.NewAgent(
		strands.WithModel(anthropic.New(
			anthropic.WithModel("claude-sonnet-4-20250514"),
		)),
		strands.WithTools(classifyContent, scoreSentiment, makeDecision),
		strands.WithSystemPrompt(`You are a content moderation agent.

For each piece of content:
1. Classify it using classify_content
2. Score its sentiment using score_sentiment
3. Based on both results, make a final decision using make_decision

Decision guidelines:
- safe + positive/neutral sentiment → approve
- mild/moderate + any sentiment → flag_for_review
- severe → reject`),
	)

	// --- HOOK 1: BeforeModelCall — inject moderation context ---

	agent.Hooks.OnBeforeModelCall(func(e *strands.BeforeModelCallEvent) {
		log.Printf("[hook:before_model] sending %d messages, injecting moderation context",
			len(e.Messages))
		// In a real system, you might dynamically adjust the system prompt
		// based on current policy rules loaded from a database.
	})

	// --- HOOK 2: AfterModelCall — validate response ---

	// First AfterModelCall hook: check for policy violations.
	agent.Hooks.OnAfterModelCall(func(e *strands.AfterModelCallEvent) {
		if e.Err != nil {
			return
		}
		text := e.Response.Message.Text()
		// Block responses that might leak internal classification details.
		if strings.Contains(strings.ToLower(text), "confidence: 0.") {
			log.Println("[hook:after_model:policy] response leaks confidence score, retrying")
			e.Retry = true
		}
	})

	// Second AfterModelCall hook: log latency.
	// This fires BEFORE the policy hook due to LIFO ordering.
	agent.Hooks.OnAfterModelCall(func(e *strands.AfterModelCallEvent) {
		if e.Response != nil {
			log.Printf("[hook:after_model:metrics] latency=%dms tokens=%d/%d",
				e.Response.Metrics.LatencyMs,
				e.Response.Usage.InputTokens,
				e.Response.Usage.OutputTokens)
		}
	})

	// --- HOOK 3: BeforeToolCall — block tools for severe content ---

	var severeDetected bool

	agent.Hooks.OnBeforeToolCall(func(e *strands.BeforeToolCallEvent) {
		log.Printf("[hook:before_tool] tool=%s", e.ToolUse.Name)

		// If severe content was already detected, block all further tool calls
		// except make_decision (so the agent can still record a rejection).
		if severeDetected && e.ToolUse.Name != "make_decision" {
			e.Cancel = true
			e.CancelMsg = "further analysis blocked — content already classified as severe"
			log.Printf("[hook:before_tool] BLOCKED %s — severe content policy", e.ToolUse.Name)
		}
	})

	// --- HOOK 4: AfterToolCall — redact and track ---

	// First AfterToolCall hook: redact sensitive terms from results.
	agent.Hooks.OnAfterToolCall(func(e *strands.AfterToolCallEvent) {
		// Track severe classification.
		if e.ToolUse.Name == "classify_content" {
			resultText := ""
			if len(e.Result.Content) > 0 {
				resultText = e.Result.Content[0].Text
			}
			if strings.Contains(resultText, `"severe"`) {
				severeDetected = true
				log.Println("[hook:after_tool] SEVERE content detected — restricting further tools")
			}
		}
	})

	// Second AfterToolCall hook: redact confidence scores from results.
	// Fires BEFORE the tracking hook due to LIFO ordering.
	agent.Hooks.OnAfterToolCall(func(e *strands.AfterToolCallEvent) {
		if len(e.Result.Content) > 0 {
			text := e.Result.Content[0].Text
			// Redact precise confidence values (keep the category).
			if strings.Contains(text, "confidence") {
				redacted := strings.ReplaceAll(text, `"confidence": 0.92`, `"confidence": "HIGH"`)
				redacted = strings.ReplaceAll(redacted, `"confidence": 0.85`, `"confidence": "HIGH"`)
				redacted = strings.ReplaceAll(redacted, `"confidence": 0.78`, `"confidence": "MEDIUM"`)
				redacted = strings.ReplaceAll(redacted, `"confidence": 0.95`, `"confidence": "HIGH"`)
				e.Result.Content[0].Text = redacted
				if text != redacted {
					log.Println("[hook:after_tool:redact] redacted confidence scores")
				}
			}
		}
	})

	// --- HOOK 5: MessageAdded — audit log ---

	auditLog := &AuditLog{}

	agent.Hooks.OnMessageAdded(func(e *strands.MessageAddedEvent) {
		preview := e.Message.Text()
		if preview == "" && len(e.Message.Content) > 0 {
			preview = fmt.Sprintf("(%s content)", e.Message.Content[0].Type)
		}
		auditLog.Add(string(e.Message.Role), preview, len(e.Message.Content))
		log.Printf("[hook:message_added] role=%s blocks=%d", e.Message.Role, len(e.Message.Content))
	})

	// --- Run moderation ---

	ctx := context.Background()

	contents := []string{
		"This product is wonderful! Great quality and fast shipping. Love it!",
		"Terrible experience. The item was broken and customer service was awful.",
		"Someone should hack into the system and exploit the vulnerability to attack it.",
	}

	for i, content := range contents {
		severeDetected = false // Reset for each content item.

		fmt.Printf("\n{'='*60}\nContent %d: %q\n{'='*60}\n\n", i+1, content)

		result, err := agent.Invoke(ctx, fmt.Sprintf("Moderate this content: %q", content))
		if err != nil {
			log.Printf("Error: %v", err)
			continue
		}

		fmt.Printf("Result: %s\n", result.Message.Text())
	}

	// Print the full audit log.
	auditLog.Print()
}

func sentimentLabel(score float64) string {
	switch {
	case score > 0.3:
		return "positive"
	case score < -0.3:
		return "negative"
	default:
		return "neutral"
	}
}
```

## How It Works

### Hook Execution Order

The SDK fires hooks in a specific order:

- **Before\* hooks** fire in FIFO order (first registered, first called)
- **After\* hooks** fire in LIFO order (last registered, first called)

This means:

```
Register: AfterModelCall(policy_check)    ← index 0
Register: AfterModelCall(metrics_logger)  ← index 1

Execution order (LIFO):
  1. metrics_logger  (index 1 — fires first)
  2. policy_check    (index 0 — fires second)
```

LIFO ordering for After* hooks matches the Python SDK's decorator stacking pattern. The outermost wrapper (registered first) has the final say — it can override decisions made by inner hooks.

### Cancel Flag

In `BeforeToolCallEvent`, setting `Cancel = true` prevents the tool from executing. The tool result is replaced with an error message (from `CancelMsg`). The model sees this error and can adjust its behaviour:

```go
e.Cancel = true
e.CancelMsg = "further analysis blocked — content already classified as severe"
```

The model receives:
```json
{"status": "error", "content": [{"text": "further analysis blocked — ..."}]}
```

### Retry Flag

In `AfterModelCallEvent`, setting `Retry = true` discards the response and re-invokes the model. The event loop supports up to 6 retries per model call.

```go
if strings.Contains(text, "confidence: 0.") {
    e.Retry = true  // Model will be called again.
}
```

### Modifying Tool Results

`AfterToolCallEvent` exposes `Result` as a mutable field. Hooks can modify the result before it's sent back to the model:

```go
e.Result.Content[0].Text = redactedText
```

This is powerful for data sanitisation — you can redact, transform, or enrich tool results without changing the tool implementation.

### Audit Trail

The `MessageAdded` hook fires for every message appended to the conversation (user messages, assistant responses, tool results). The audit log captures a complete timeline of the moderation process.

## Running

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
go run main.go
```

Expected output: three pieces of content are moderated. The first (positive) is approved, the second (negative) is flagged, and the third (severe) is rejected with tool restrictions applied.

## Extending This

- **Add a `check_language` tool** — detect non-English content and route to specialised moderators
- **Add appeal handling** — use state to track overrides and multi-turn appeal conversations
- **Persist the audit log** — write to a database or structured log file
- **Add rate limiting** — use a `BeforeModelCall` hook to enforce requests-per-minute limits
- **Chain multiple agents** — first agent classifies, second agent handles appeals (multi-agent pattern)
