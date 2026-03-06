# Tutorial 2: Customer Support Agent

Build a customer support agent with a knowledge base, order lookup, ticket creation, and a guardrail hook that blocks PII from being sent to external systems.

## What You'll Learn

- Using `WithState` to pass and track data across the agent lifecycle
- Building guardrail hooks with `OnBeforeToolCall` (cancel flag)
- Automatic retry on transient errors with `OnAfterModelCall`
- Working with multiple cooperating tools

## The Scenario

You're building a first-line support agent for an e-commerce company. It can search a FAQ knowledge base, look up order status, and create support tickets. A guardrail hook blocks any tool call whose input contains email addresses or credit card patterns — preventing PII from leaking into ticket descriptions.

## Complete Code

```go
package main

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	strands "github.com/achrafsoltani/strands-agents-sdk-go"
	"github.com/achrafsoltani/strands-agents-sdk-go/provider/anthropic"
)

// Simulated data stores.
var knowledgeBase = map[string]string{
	"returns": "Items can be returned within 30 days of delivery. " +
		"Start a return from your order page or contact support. " +
		"Refunds are processed within 5-7 business days.",
	"shipping": "Standard shipping takes 3-5 business days. " +
		"Express shipping (next day) is available for orders over $50. " +
		"Free shipping on orders over $100.",
	"payment": "We accept Visa, Mastercard, and PayPal. " +
		"Payment is charged when the order ships. " +
		"For failed payments, update your card in account settings.",
	"warranty": "All electronics carry a 2-year manufacturer warranty. " +
		"Clothing and accessories: 90-day quality guarantee. " +
		"File a warranty claim through the support portal.",
}

var orders = map[string]map[string]string{
	"ORD-1001": {"status": "shipped", "tracking": "1Z999AA10123456784", "eta": "2026-03-08", "items": "Wireless Headphones x1"},
	"ORD-1002": {"status": "processing", "tracking": "", "eta": "2026-03-10", "items": "USB-C Hub x1, HDMI Cable x2"},
	"ORD-1003": {"status": "delivered", "tracking": "1Z999AA10123456785", "eta": "", "items": "Mechanical Keyboard x1"},
}

var piiPatterns = []*regexp.Regexp{
	regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`),          // email
	regexp.MustCompile(`\b\d{4}[- ]?\d{4}[- ]?\d{4}[- ]?\d{4}\b`),                  // credit card
	regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),                                      // SSN
}

func containsPII(text string) (bool, string) {
	for _, p := range piiPatterns {
		if p.MatchString(text) {
			return true, p.String()
		}
	}
	return false, ""
}

func main() {
	// --- Tools ---

	searchKB := strands.NewFuncTool(
		"search_knowledge_base",
		"Search the FAQ knowledge base by topic. Returns relevant FAQ content.",
		func(_ context.Context, input map[string]any) (any, error) {
			topic, _ := input["topic"].(string)
			topic = strings.ToLower(topic)

			var results []string
			for key, content := range knowledgeBase {
				if strings.Contains(key, topic) || strings.Contains(strings.ToLower(content), topic) {
					results = append(results, fmt.Sprintf("**%s**: %s", strings.Title(key), content))
				}
			}
			if len(results) == 0 {
				return "No FAQ articles found for that topic. Consider creating a support ticket.", nil
			}
			return strings.Join(results, "\n\n"), nil
		},
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"topic": map[string]any{
					"type":        "string",
					"description": "Search topic (e.g. 'returns', 'shipping', 'payment')",
				},
			},
			"required": []string{"topic"},
		},
	)

	getOrder := strands.NewFuncTool(
		"get_order_status",
		"Look up the status of an order by its order ID (e.g. ORD-1001).",
		func(_ context.Context, input map[string]any) (any, error) {
			orderID, _ := input["order_id"].(string)
			orderID = strings.ToUpper(strings.TrimSpace(orderID))

			order, ok := orders[orderID]
			if !ok {
				return nil, fmt.Errorf("order %s not found", orderID)
			}

			lines := []string{
				fmt.Sprintf("Order: %s", orderID),
				fmt.Sprintf("Status: %s", order["status"]),
				fmt.Sprintf("Items: %s", order["items"]),
			}
			if order["tracking"] != "" {
				lines = append(lines, fmt.Sprintf("Tracking: %s", order["tracking"]))
			}
			if order["eta"] != "" {
				lines = append(lines, fmt.Sprintf("ETA: %s", order["eta"]))
			}
			return strings.Join(lines, "\n"), nil
		},
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"order_id": map[string]any{
					"type":        "string",
					"description": "The order ID to look up (e.g. ORD-1001)",
				},
			},
			"required": []string{"order_id"},
		},
	)

	createTicket := strands.NewFuncTool(
		"create_ticket",
		"Create a support ticket for issues that cannot be resolved from the knowledge base.",
		func(_ context.Context, input map[string]any) (any, error) {
			subject, _ := input["subject"].(string)
			description, _ := input["description"].(string)
			priority, _ := input["priority"].(string)
			if priority == "" {
				priority = "normal"
			}

			ticketID := fmt.Sprintf("TKT-%d", time.Now().UnixMilli()%100000)

			return fmt.Sprintf(
				"Ticket created successfully.\nID: %s\nSubject: %s\nPriority: %s\nDescription: %s",
				ticketID, subject, priority, description,
			), nil
		},
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"subject": map[string]any{
					"type":        "string",
					"description": "Brief ticket subject",
				},
				"description": map[string]any{
					"type":        "string",
					"description": "Detailed description of the issue",
				},
				"priority": map[string]any{
					"type":        "string",
					"enum":        []string{"low", "normal", "high", "urgent"},
					"description": "Ticket priority level",
				},
			},
			"required": []string{"subject", "description"},
		},
	)

	// --- Agent ---

	agent := strands.NewAgent(
		strands.WithModel(anthropic.New(
			anthropic.WithModel("claude-sonnet-4-20250514"),
		)),
		strands.WithTools(searchKB, getOrder, createTicket),
		strands.WithSystemPrompt(`You are a customer support agent for ShopCo, an online electronics retailer.

Your approach:
1. First search the knowledge base to see if the customer's question is covered by FAQ.
2. If the customer asks about an order, look it up by order ID.
3. If the issue cannot be resolved, create a support ticket.

Be friendly, concise, and helpful. Always confirm the action you're taking.
Never include customer personal information (emails, card numbers) in ticket descriptions.`),
		strands.WithState(map[string]any{
			"customer_id":    "CUST-42",
			"tickets_created": 0,
		}),
	)

	// --- Guardrail: Block PII in tool inputs ---

	agent.Hooks.OnBeforeToolCall(func(e *strands.BeforeToolCallEvent) {
		// Serialise the entire input to check for PII.
		inputStr := fmt.Sprintf("%v", e.ToolUse.Input)
		if hasPII, pattern := containsPII(inputStr); hasPII {
			e.Cancel = true
			e.CancelMsg = fmt.Sprintf(
				"blocked: tool input contains PII matching pattern %s — "+
					"please rephrase without personal information", pattern)
			log.Printf("[guardrail] BLOCKED %s — PII detected in input", e.ToolUse.Name)
		}
	})

	// --- Logging ---

	agent.Hooks.OnBeforeToolCall(func(e *strands.BeforeToolCallEvent) {
		log.Printf("[tool] calling %s", e.ToolUse.Name)
	})

	agent.Hooks.OnAfterToolCall(func(e *strands.AfterToolCallEvent) {
		// Track ticket creation in state.
		if e.ToolUse.Name == "create_ticket" && e.Result.Status == strands.ToolResultSuccess {
			count, _ := e.Agent.State["tickets_created"].(int)
			e.Agent.State["tickets_created"] = count + 1
		}
	})

	// --- Retry on transient errors ---

	agent.Hooks.OnAfterModelCall(func(e *strands.AfterModelCallEvent) {
		if e.Err != nil && strings.Contains(e.Err.Error(), "429") {
			log.Println("[retry] rate limited, retrying in 2s...")
			time.Sleep(2 * time.Second)
			e.Retry = true
		}
	})

	// --- Run ---

	ctx := context.Background()

	queries := []string{
		"I ordered a USB-C hub, order ORD-1002. When will it arrive?",
		"What's your return policy?",
		"My keyboard from order ORD-1003 stopped working after a week. Can you help?",
	}

	for i, q := range queries {
		fmt.Printf("\n=== Turn %d ===\nCustomer: %s\n\n", i+1, q)

		result, err := agent.Invoke(ctx, q)
		if err != nil {
			log.Fatalf("Error: %v", err)
		}

		fmt.Printf("Agent: %s\n", result.Message.Text())
		fmt.Printf("[tokens: %d in / %d out | tickets created: %v]\n",
			result.Usage.InputTokens, result.Usage.OutputTokens,
			result.State["tickets_created"])
	}
}
```

## How It Works

### State Tracking

`WithState` initialises a `map[string]any` that persists across invocations. The `AfterToolCall` hook increments `tickets_created` whenever the `create_ticket` tool succeeds. The state is returned in every `AgentResult`, so the calling code can inspect it.

### Guardrail Hook

The `OnBeforeToolCall` hook runs **before** every tool execution. It serialises the tool input and checks for PII patterns (email, credit card, SSN). If a match is found:

1. `e.Cancel = true` — the tool is **not** executed
2. `e.CancelMsg` — returned to the model as an error result
3. The model sees the error and reformulates its approach (typically rephrasing without the PII)

Because `BeforeToolCall` hooks fire in FIFO order, the guardrail hook runs first (it was registered first), and the logging hook runs second — but only if the guardrail didn't cancel.

### Retry Hook

The `OnAfterModelCall` hook checks if the error message contains `429` (HTTP rate limit). If so, it waits 2 seconds and sets `e.Retry = true`. The event loop will re-invoke the model up to 6 times.

### Multi-Turn Conversation

The agent maintains conversation history across `Invoke` calls. By the third query, it has full context of the previous two exchanges, enabling it to connect "my keyboard from order ORD-1003" with the earlier order lookup.

## Running

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
go run main.go
```

## Extending This

- **Add a `transfer_to_human` tool** — escalate complex issues and stop the loop
- **Add sentiment tracking** — use an `AfterModelCall` hook to score sentiment and adjust priority
- **Add rate limiting per customer** — use state to count calls per `customer_id`, cancel if exceeded
- **Persist tickets** — swap the in-memory map for a database or API call
