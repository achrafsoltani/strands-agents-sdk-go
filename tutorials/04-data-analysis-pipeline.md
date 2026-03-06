# Tutorial 4: Data Analysis Pipeline

Build a data analysis agent that queries in-memory datasets, computes statistics, and generates formatted reports. Demonstrates concurrent tool execution, state tracking, and shows how to swap between Anthropic and Bedrock providers.

## What You'll Learn

- Concurrent tool execution (default behaviour — multiple tools run in parallel)
- Tracking intermediate results with `WithState`
- Setting `WithMaxCycles` for complex multi-step analyses
- Swapping providers (Anthropic vs Bedrock) with a flag

## The Scenario

You have a dataset of monthly sales records. The agent can query the data with filters, compute statistics (mean, median, sum, min, max), and format results as markdown tables. When the model asks for statistics on multiple metrics simultaneously, the tools execute in parallel via goroutines.

## Complete Code

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"

	strands "github.com/achrafsoltani/strands-agents-sdk-go"
	"github.com/achrafsoltani/strands-agents-sdk-go/provider/anthropic"
	// Uncomment for Bedrock:
	// "github.com/achrafsoltani/strands-agents-sdk-go/provider/bedrock"
)

// SalesRecord represents one month's data.
type SalesRecord struct {
	Month    string  `json:"month"`
	Region   string  `json:"region"`
	Product  string  `json:"product"`
	Units    int     `json:"units"`
	Revenue  float64 `json:"revenue"`
	Cost     float64 `json:"cost"`
	Profit   float64 `json:"profit"`
}

// Simulated dataset.
var salesData = []SalesRecord{
	{"2025-01", "EMEA", "Widget A", 150, 45000, 27000, 18000},
	{"2025-01", "EMEA", "Widget B", 80, 32000, 20800, 11200},
	{"2025-01", "APAC", "Widget A", 200, 60000, 36000, 24000},
	{"2025-01", "APAC", "Widget B", 120, 48000, 31200, 16800},
	{"2025-01", "AMER", "Widget A", 300, 90000, 54000, 36000},
	{"2025-01", "AMER", "Widget B", 180, 72000, 46800, 25200},
	{"2025-02", "EMEA", "Widget A", 170, 51000, 30600, 20400},
	{"2025-02", "EMEA", "Widget B", 90, 36000, 23400, 12600},
	{"2025-02", "APAC", "Widget A", 220, 66000, 39600, 26400},
	{"2025-02", "APAC", "Widget B", 130, 52000, 33800, 18200},
	{"2025-02", "AMER", "Widget A", 310, 93000, 55800, 37200},
	{"2025-02", "AMER", "Widget B", 190, 76000, 49400, 26600},
	{"2025-03", "EMEA", "Widget A", 160, 48000, 28800, 19200},
	{"2025-03", "EMEA", "Widget B", 95, 38000, 24700, 13300},
	{"2025-03", "APAC", "Widget A", 240, 72000, 43200, 28800},
	{"2025-03", "APAC", "Widget B", 140, 56000, 36400, 19600},
	{"2025-03", "AMER", "Widget A", 280, 84000, 50400, 33600},
	{"2025-03", "AMER", "Widget B", 200, 80000, 52000, 28000},
}

func filterRecords(region, product, month string) []SalesRecord {
	var result []SalesRecord
	for _, r := range salesData {
		if region != "" && !strings.EqualFold(r.Region, region) {
			continue
		}
		if product != "" && !strings.EqualFold(r.Product, product) {
			continue
		}
		if month != "" && r.Month != month {
			continue
		}
		result = append(result, r)
	}
	return result
}

func main() {
	// --- Tools ---

	queryData := strands.NewFuncTool(
		"query_data",
		"Query the sales dataset. Filter by region (EMEA, APAC, AMER), product (Widget A, Widget B), "+
			"and/or month (YYYY-MM). Returns matching records as JSON.",
		func(_ context.Context, input map[string]any) (any, error) {
			region, _ := input["region"].(string)
			product, _ := input["product"].(string)
			month, _ := input["month"].(string)

			records := filterRecords(region, product, month)
			if len(records) == 0 {
				return "No records match the given filters.", nil
			}

			data, err := json.MarshalIndent(records, "", "  ")
			if err != nil {
				return nil, err
			}
			return fmt.Sprintf("Found %d records:\n%s", len(records), string(data)), nil
		},
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"region":  map[string]any{"type": "string", "description": "Filter by region: EMEA, APAC, or AMER"},
				"product": map[string]any{"type": "string", "description": "Filter by product name"},
				"month":   map[string]any{"type": "string", "description": "Filter by month (YYYY-MM format)"},
			},
		},
	)

	computeStats := strands.NewFuncTool(
		"compute_stats",
		"Compute statistics (sum, mean, median, min, max, count) on a numeric field from the sales data. "+
			"Supports the same filters as query_data.",
		func(_ context.Context, input map[string]any) (any, error) {
			field, _ := input["field"].(string)
			region, _ := input["region"].(string)
			product, _ := input["product"].(string)
			month, _ := input["month"].(string)

			records := filterRecords(region, product, month)
			if len(records) == 0 {
				return "No records match the given filters.", nil
			}

			// Extract the numeric values.
			var values []float64
			for _, r := range records {
				switch strings.ToLower(field) {
				case "units":
					values = append(values, float64(r.Units))
				case "revenue":
					values = append(values, r.Revenue)
				case "cost":
					values = append(values, r.Cost)
				case "profit":
					values = append(values, r.Profit)
				default:
					return nil, fmt.Errorf("unknown field %q — use units, revenue, cost, or profit", field)
				}
			}

			sort.Float64s(values)

			sum := 0.0
			for _, v := range values {
				sum += v
			}
			mean := sum / float64(len(values))

			median := values[len(values)/2]
			if len(values)%2 == 0 {
				median = (values[len(values)/2-1] + values[len(values)/2]) / 2
			}

			return fmt.Sprintf("Statistics for %s (n=%d):\n  Sum:    %.2f\n  Mean:   %.2f\n  Median: %.2f\n  Min:    %.2f\n  Max:    %.2f",
				field, len(values), sum, mean, median, values[0], values[len(values)-1]), nil
		},
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"field":   map[string]any{"type": "string", "description": "Numeric field: units, revenue, cost, or profit"},
				"region":  map[string]any{"type": "string", "description": "Optional region filter"},
				"product": map[string]any{"type": "string", "description": "Optional product filter"},
				"month":   map[string]any{"type": "string", "description": "Optional month filter (YYYY-MM)"},
			},
			"required": []string{"field"},
		},
	)

	formatReport := strands.NewFuncTool(
		"format_report",
		"Format data as a markdown table. Takes a title, headers, and rows.",
		func(_ context.Context, input map[string]any) (any, error) {
			title, _ := input["title"].(string)
			headersRaw, _ := input["headers"].([]any)
			rowsRaw, _ := input["rows"].([]any)

			var headers []string
			for _, h := range headersRaw {
				headers = append(headers, fmt.Sprintf("%v", h))
			}

			var sb strings.Builder
			if title != "" {
				sb.WriteString("## " + title + "\n\n")
			}

			// Header row.
			sb.WriteString("| " + strings.Join(headers, " | ") + " |\n")
			// Separator.
			seps := make([]string, len(headers))
			for i := range seps {
				seps[i] = "---"
			}
			sb.WriteString("| " + strings.Join(seps, " | ") + " |\n")

			// Data rows.
			for _, rowRaw := range rowsRaw {
				row, ok := rowRaw.([]any)
				if !ok {
					continue
				}
				var cells []string
				for _, cell := range row {
					switch v := cell.(type) {
					case float64:
						if v == math.Trunc(v) {
							cells = append(cells, fmt.Sprintf("%.0f", v))
						} else {
							cells = append(cells, fmt.Sprintf("%.2f", v))
						}
					default:
						cells = append(cells, fmt.Sprintf("%v", cell))
					}
				}
				sb.WriteString("| " + strings.Join(cells, " | ") + " |\n")
			}

			return sb.String(), nil
		},
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":   map[string]any{"type": "string", "description": "Report title"},
				"headers": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Column headers"},
				"rows":    map[string]any{"type": "array", "items": map[string]any{"type": "array"}, "description": "Data rows (array of arrays)"},
			},
			"required": []string{"headers", "rows"},
		},
	)

	// --- Provider selection ---

	var model strands.Model

	if os.Getenv("USE_BEDROCK") == "1" {
		// Uncomment this block and the import above to use Bedrock:
		// model = bedrock.New(
		//     bedrock.WithRegion("us-east-1"),
		//     bedrock.WithModel("us.anthropic.claude-3-5-sonnet-20241022-v2:0"),
		// )
		log.Fatal("Bedrock usage: uncomment the bedrock import and New() call above")
	} else {
		model = anthropic.New(
			anthropic.WithModel("claude-sonnet-4-20250514"),
			anthropic.WithMaxTokens(4096),
		)
	}

	// --- Agent ---

	agent := strands.NewAgent(
		strands.WithModel(model),
		strands.WithTools(queryData, computeStats, formatReport),
		strands.WithSystemPrompt(`You are a data analyst assistant.

You have access to a sales dataset with monthly records across 3 regions (EMEA, APAC, AMER)
and 2 products (Widget A, Widget B) for Q1 2025. Each record has: units, revenue, cost, profit.

When analysing data:
1. Query the data to understand what's available.
2. Compute relevant statistics using compute_stats.
3. Present findings in formatted tables using format_report.
4. Provide insights and recommendations based on the numbers.

When multiple statistics are needed, request them in the same turn so they execute concurrently.`),
		strands.WithState(map[string]any{
			"analysis_id": "ANL-001",
			"queries_run": 0,
		}),
		strands.WithMaxCycles(15), // Data analysis can require many tool calls.
	)

	// Track query count in state.
	agent.Hooks.OnAfterToolCall(func(e *strands.AfterToolCallEvent) {
		count, _ := e.Agent.State["queries_run"].(int)
		e.Agent.State["queries_run"] = count + 1
	})

	// --- Run analysis ---

	ctx := context.Background()

	queries := []string{
		"Give me a Q1 2025 overview: total revenue and profit by region, formatted as a table.",
		"Which product has better profit margins? Compare Widget A vs Widget B across all regions.",
		"What trends do you see month-over-month? Which region is growing fastest?",
	}

	for i, q := range queries {
		fmt.Printf("\n{'='*60}\nAnalysis %d: %s\n{'='*60}\n\n", i+1, q)

		result, err := agent.Invoke(ctx, q)
		if err != nil {
			log.Fatalf("Error: %v", err)
		}

		fmt.Println(result.Message.Text())
		fmt.Printf("\n[tokens: %d in / %d out | queries run: %v]\n",
			result.Usage.InputTokens, result.Usage.OutputTokens,
			result.State["queries_run"])
	}
}
```

## How It Works

### Concurrent Tool Execution

The default `ConcurrentExecutor` runs tools in parallel using goroutines. When the model requests multiple `compute_stats` calls in a single turn (e.g. "compute revenue stats for EMEA" and "compute revenue stats for APAC" simultaneously), they execute concurrently:

```
Model returns: tool_use with 3 compute_stats calls
  → goroutine 1: compute_stats(field=revenue, region=EMEA)
  → goroutine 2: compute_stats(field=revenue, region=APAC)
  → goroutine 3: compute_stats(field=revenue, region=AMER)
All 3 complete → results sent back to model
```

This is safe because each tool call operates on read-only data. The `sync.WaitGroup` in `ConcurrentExecutor` ensures all results are collected before the next model call.

### MaxCycles

`WithMaxCycles(15)` allows up to 15 event loop iterations. Data analysis often requires many sequential steps:

1. Query data (1 cycle)
2. Compute multiple stats (1 cycle, but multiple tool calls)
3. Format report (1 cycle)
4. Model generates final analysis (1 cycle)

That's 4 cycles minimum per question, and with 3 questions, the conversation can easily exceed the default 20 cycles. Increasing it gives the agent room to be thorough.

### State Tracking

The `queries_run` counter increments in an `AfterToolCall` hook after every tool execution. This gives visibility into how many API calls the agent makes — useful for cost tracking and rate limiting.

### Provider Swapping

The code shows how to swap between Anthropic and Bedrock. Both implement the same `Model` interface, so the agent code doesn't change:

```go
// Anthropic (direct API)
model = anthropic.New(anthropic.WithModel("claude-sonnet-4-20250514"))

// Bedrock (AWS — reads ~/.aws/credentials)
model = bedrock.New(bedrock.WithModel("us.anthropic.claude-3-5-sonnet-20241022-v2:0"))
```

Use `USE_BEDROCK=1` to switch. Bedrock is useful when you're running in AWS and want to use IAM credentials instead of API keys.

## Running

```bash
# With Anthropic:
export ANTHROPIC_API_KEY="sk-ant-..."
go run main.go

# With Bedrock (after uncommenting the import):
USE_BEDROCK=1 go run main.go
```

## Extending This

- **Load real CSV data** — replace the hardcoded slice with `encoding/csv` parsing
- **Add a `plot_chart` tool** — generate ASCII charts or write SVG files
- **Add an `export_csv` tool** — write filtered results to a file
- **Add cost tracking** — use token counts from `AgentResult.Usage` to estimate API spend
