// Package anthropic implements a Strands Model provider for the Anthropic
// Messages API using only the standard library (net/http + SSE parsing).
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	strands "github.com/Dr-H-PhD/strands-agents-sdk-go"
)

// Provider implements strands.Model for the Anthropic Messages API.
type Provider struct {
	apiKey    string
	model     string
	maxTokens int
	baseURL   string
	client    *http.Client
}

// Option configures the Anthropic provider.
type Option func(*Provider)

// New creates an Anthropic provider. By default it reads the API key from
// ANTHROPIC_API_KEY and uses claude-sonnet-4-20250514.
func New(opts ...Option) *Provider {
	p := &Provider{
		apiKey:    os.Getenv("ANTHROPIC_API_KEY"),
		model:     "claude-sonnet-4-20250514",
		maxTokens: 4096,
		baseURL:   "https://api.anthropic.com",
		client:    &http.Client{Timeout: 5 * time.Minute},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func WithAPIKey(key string) Option     { return func(p *Provider) { p.apiKey = key } }
func WithModel(model string) Option    { return func(p *Provider) { p.model = model } }
func WithMaxTokens(n int) Option       { return func(p *Provider) { p.maxTokens = n } }
func WithBaseURL(url string) Option    { return func(p *Provider) { p.baseURL = url } }

// Converse performs a non-streaming model invocation.
func (p *Provider) Converse(ctx context.Context, input *strands.ConverseInput) (*strands.ConverseOutput, error) {
	body, err := p.buildRequestBody(input, false)
	if err != nil {
		return nil, err
	}
	resp, err := p.doRequest(ctx, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, p.readError(resp)
	}

	var apiResp apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("anthropic: failed to decode response: %w", err)
	}
	return p.convertResponse(&apiResp), nil
}

// ConverseStream performs a streaming model invocation, calling the handler
// for each text delta, and returns the final assembled output.
func (p *Provider) ConverseStream(ctx context.Context, input *strands.ConverseInput, handler strands.StreamHandler) (*strands.ConverseOutput, error) {
	body, err := p.buildRequestBody(input, true)
	if err != nil {
		return nil, err
	}
	resp, err := p.doRequest(ctx, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, p.readError(resp)
	}

	return p.parseSSEStream(resp.Body, handler)
}

// --- HTTP layer ---

func (p *Provider) doRequest(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("anthropic: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	return p.client.Do(req)
}

func (p *Provider) readError(resp *http.Response) error {
	data, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("anthropic: API error %d: %s", resp.StatusCode, string(data))
}

// --- Request building ---

func (p *Provider) buildRequestBody(input *strands.ConverseInput, stream bool) ([]byte, error) {
	req := apiRequest{
		Model:     p.model,
		MaxTokens: p.maxTokens,
		Messages:  convertMessages(input.Messages),
		Stream:    stream,
	}
	if input.SystemPrompt != "" {
		req.System = input.SystemPrompt
	}
	if len(input.ToolSpecs) > 0 {
		req.Tools = convertToolSpecs(input.ToolSpecs)
	}
	if input.ToolChoice != nil {
		req.ToolChoice = convertToolChoice(input.ToolChoice)
	}
	return json.Marshal(req)
}

// --- SSE streaming ---

func (p *Provider) parseSSEStream(r io.Reader, handler strands.StreamHandler) (*strands.ConverseOutput, error) {
	scanner := bufio.NewScanner(r)
	// Increase buffer for large JSON deltas.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var (
		eventType string
		blocks    []strands.ContentBlock
		usage     strands.Usage
		stopReason strands.StopReason

		// Per-block accumulation state.
		currentBlockType string
		currentText      strings.Builder
		currentToolID    string
		currentToolName  string
		currentToolInput strings.Builder
	)

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		var event map[string]any
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch eventType {
		case "message_start":
			if msg, ok := event["message"].(map[string]any); ok {
				if u, ok := msg["usage"].(map[string]any); ok {
					usage.InputTokens = jsonInt(u, "input_tokens")
				}
			}

		case "content_block_start":
			cb, _ := event["content_block"].(map[string]any)
			currentBlockType, _ = cb["type"].(string)
			if currentBlockType == "tool_use" {
				currentToolID, _ = cb["id"].(string)
				currentToolName, _ = cb["name"].(string)
				currentToolInput.Reset()
			} else {
				currentText.Reset()
			}

		case "content_block_delta":
			delta, _ := event["delta"].(map[string]any)
			deltaType, _ := delta["type"].(string)
			switch deltaType {
			case "text_delta":
				text, _ := delta["text"].(string)
				currentText.WriteString(text)
				if handler != nil {
					handler(text)
				}
			case "input_json_delta":
				partial, _ := delta["partial_json"].(string)
				currentToolInput.WriteString(partial)
			}

		case "content_block_stop":
			switch currentBlockType {
			case "text":
				if currentText.Len() > 0 {
					blocks = append(blocks, strands.TextBlock(currentText.String()))
				}
			case "tool_use":
				var input map[string]any
				if currentToolInput.Len() > 0 {
					_ = json.Unmarshal([]byte(currentToolInput.String()), &input)
				}
				if input == nil {
					input = make(map[string]any)
				}
				blocks = append(blocks, strands.ToolUseBlock(currentToolID, currentToolName, input))
			}

		case "message_delta":
			if delta, ok := event["delta"].(map[string]any); ok {
				if sr, ok := delta["stop_reason"].(string); ok {
					stopReason = strands.StopReason(sr)
				}
			}
			if u, ok := event["usage"].(map[string]any); ok {
				usage.OutputTokens = jsonInt(u, "output_tokens")
			}

		case "message_stop":
			// End of stream.

		case "error":
			errData, _ := event["error"].(map[string]any)
			msg, _ := errData["message"].(string)
			return nil, fmt.Errorf("anthropic: stream error: %s", msg)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("anthropic: stream read error: %w", err)
	}

	usage.TotalTokens = usage.InputTokens + usage.OutputTokens

	return &strands.ConverseOutput{
		StopReason: stopReason,
		Message: strands.Message{
			Role:    strands.RoleAssistant,
			Content: blocks,
		},
		Usage: usage,
	}, nil
}

// --- Type conversion ---

func convertMessages(msgs []strands.Message) []apiMessage {
	out := make([]apiMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, apiMessage{
			Role:    string(m.Role),
			Content: convertContentBlocks(m.Content),
		})
	}
	return out
}

func convertContentBlocks(blocks []strands.ContentBlock) []apiContent {
	out := make([]apiContent, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case strands.ContentTypeText:
			out = append(out, apiContent{Type: "text", Text: b.Text})
		case strands.ContentTypeToolUse:
			if b.ToolUse != nil {
				out = append(out, apiContent{
					Type:  "tool_use",
					ID:    b.ToolUse.ID,
					Name:  b.ToolUse.Name,
					Input: b.ToolUse.Input,
				})
			}
		case strands.ContentTypeToolResult:
			if b.ToolResult != nil {
				content := ""
				if len(b.ToolResult.Content) > 0 {
					content = b.ToolResult.Content[0].Text
				}
				out = append(out, apiContent{
					Type:      "tool_result",
					ToolUseID: b.ToolResult.ToolUseID,
					Content:   content,
					IsError:   b.ToolResult.Status == strands.ToolResultError,
				})
			}
		}
	}
	return out
}

func convertToolSpecs(specs []strands.ToolSpec) []apiTool {
	out := make([]apiTool, 0, len(specs))
	for _, s := range specs {
		out = append(out, apiTool{
			Name:        s.Name,
			Description: s.Description,
			InputSchema: s.InputSchema,
		})
	}
	return out
}

func convertToolChoice(tc *strands.ToolChoice) any {
	switch tc.Type {
	case strands.ToolChoiceAuto:
		return map[string]any{"type": "auto"}
	case strands.ToolChoiceAny:
		return map[string]any{"type": "any"}
	case strands.ToolChoiceTool:
		return map[string]any{"type": "tool", "name": tc.Name}
	default:
		return nil
	}
}

func (p *Provider) convertResponse(resp *apiResponse) *strands.ConverseOutput {
	var blocks []strands.ContentBlock
	for _, c := range resp.Content {
		switch c.Type {
		case "text":
			blocks = append(blocks, strands.TextBlock(c.Text))
		case "tool_use":
			blocks = append(blocks, strands.ToolUseBlock(c.ID, c.Name, c.Input))
		}
	}
	return &strands.ConverseOutput{
		StopReason: strands.StopReason(resp.StopReason),
		Message: strands.Message{
			Role:    strands.RoleAssistant,
			Content: blocks,
		},
		Usage: strands.Usage{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
			TotalTokens:  resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}
}

// --- Anthropic API types (internal) ---

type apiRequest struct {
	Model      string       `json:"model"`
	MaxTokens  int          `json:"max_tokens"`
	Messages   []apiMessage `json:"messages"`
	System     string       `json:"system,omitempty"`
	Tools      []apiTool    `json:"tools,omitempty"`
	ToolChoice any          `json:"tool_choice,omitempty"`
	Stream     bool         `json:"stream,omitempty"`
}

type apiMessage struct {
	Role    string       `json:"role"`
	Content []apiContent `json:"content"`
}

type apiContent struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   any            `json:"content,omitempty"`
	IsError   bool           `json:"is_error,omitempty"`
}

type apiTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"`
}

type apiResponse struct {
	ID         string       `json:"id"`
	Type       string       `json:"type"`
	Role       string       `json:"role"`
	Content    []apiContent `json:"content"`
	Model      string       `json:"model"`
	StopReason string       `json:"stop_reason"`
	Usage      apiUsage     `json:"usage"`
}

type apiUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// jsonInt safely extracts an integer from a JSON-decoded map value.
func jsonInt(m map[string]any, key string) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return 0
}
