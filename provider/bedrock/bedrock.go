// Package bedrock implements a Strands Model provider for the AWS Bedrock
// Converse API using only the standard library (net/http + SigV4 + event stream).
package bedrock

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

	strands "github.com/achrafsoltani/strands-agents-sdk-go"
)

// Provider implements strands.Model for AWS Bedrock's Converse API.
type Provider struct {
	region       string
	model        string
	maxTokens    int
	accessKey    string
	secretKey    string
	sessionToken string
	endpointURL  string
	client       *http.Client
}

// Option configures the Bedrock provider.
type Option func(*Provider)

// New creates a Bedrock provider. It resolves credentials in order:
// 1. Explicit WithCredentials option
// 2. Environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY)
// 3. Shared credentials file (~/.aws/credentials)
func New(opts ...Option) *Provider {
	p := &Provider{
		model:     "anthropic.claude-3-5-sonnet-20241022-v2:0",
		maxTokens: 4096,
		client:    &http.Client{Timeout: 5 * time.Minute},
	}
	for _, opt := range opts {
		opt(p)
	}
	// Resolve region.
	if p.region == "" {
		p.region = envOr("AWS_DEFAULT_REGION", envOr("AWS_REGION", ""))
	}
	if p.region == "" {
		p.region = readAWSConfig("region")
	}
	if p.region == "" {
		p.region = "us-east-1"
	}
	// Resolve credentials: env vars, then shared credentials file.
	if p.accessKey == "" {
		p.accessKey = os.Getenv("AWS_ACCESS_KEY_ID")
		p.secretKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
		p.sessionToken = os.Getenv("AWS_SESSION_TOKEN")
	}
	if p.accessKey == "" {
		p.accessKey, p.secretKey, p.sessionToken = readAWSCredentials()
	}
	return p
}

func WithRegion(region string) Option  { return func(p *Provider) { p.region = region } }
func WithModel(model string) Option    { return func(p *Provider) { p.model = model } }
func WithMaxTokens(n int) Option       { return func(p *Provider) { p.maxTokens = n } }
func WithEndpointURL(url string) Option { return func(p *Provider) { p.endpointURL = url } }

// WithCredentials sets explicit AWS credentials.
func WithCredentials(accessKey, secretKey, sessionToken string) Option {
	return func(p *Provider) {
		p.accessKey = accessKey
		p.secretKey = secretKey
		p.sessionToken = sessionToken
	}
}

func (p *Provider) baseURL() string {
	if p.endpointURL != "" {
		return strings.TrimRight(p.endpointURL, "/")
	}
	return fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", p.region)
}

// Converse performs a non-streaming invocation via the Bedrock Converse API.
func (p *Provider) Converse(ctx context.Context, input *strands.ConverseInput) (*strands.ConverseOutput, error) {
	body, err := p.buildRequestBody(input)
	if err != nil {
		return nil, err
	}
	path := "/model/" + p.model + "/converse"
	resp, err := p.doRequest(ctx, path, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, p.readError(resp)
	}

	var apiResp converseResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("bedrock: failed to decode response: %w", err)
	}
	return p.convertResponse(&apiResp), nil
}

// ConverseStream performs a streaming invocation via the Bedrock ConverseStream API.
func (p *Provider) ConverseStream(ctx context.Context, input *strands.ConverseInput, handler strands.StreamHandler) (*strands.ConverseOutput, error) {
	body, err := p.buildRequestBody(input)
	if err != nil {
		return nil, err
	}
	path := "/model/" + p.model + "/converse-stream"
	resp, err := p.doRequest(ctx, path, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, p.readError(resp)
	}

	return p.parseEventStream(resp.Body, handler)
}

// --- HTTP layer ---

func (p *Provider) doRequest(ctx context.Context, path string, body []byte) (*http.Response, error) {
	base := p.baseURL()
	req, err := http.NewRequestWithContext(ctx, "POST", base+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("bedrock: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	signRequest(req, p.accessKey, p.secretKey, p.sessionToken, p.region, "bedrock", body)
	return p.client.Do(req)
}

func (p *Provider) readError(resp *http.Response) error {
	data, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("bedrock: API error %d: %s", resp.StatusCode, string(data))
}

// --- Request building ---

func (p *Provider) buildRequestBody(input *strands.ConverseInput) ([]byte, error) {
	req := converseRequest{
		Messages:        convertMessages(input.Messages),
		InferenceConfig: &inferenceConfig{MaxTokens: p.maxTokens},
	}
	if input.SystemPrompt != "" {
		req.System = []systemContent{{Text: input.SystemPrompt}}
	}
	if len(input.ToolSpecs) > 0 {
		tc := &toolConfig{Tools: convertToolSpecs(input.ToolSpecs)}
		if input.ToolChoice != nil {
			tc.ToolChoice = convertToolChoice(input.ToolChoice)
		}
		req.ToolConfig = tc
	}
	return json.Marshal(req)
}

// --- Event stream parsing ---

func (p *Provider) parseEventStream(r io.Reader, handler strands.StreamHandler) (*strands.ConverseOutput, error) {
	br := bufio.NewReaderSize(r, 64*1024)

	var (
		blocks     []strands.ContentBlock
		usage      strands.Usage
		stopReason strands.StopReason
		metrics    strands.Metrics

		currentBlockType string
		currentText      strings.Builder
		currentToolID    string
		currentToolName  string
		currentToolInput strings.Builder
	)

	for {
		msg, err := decodeEventStream(br)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, fmt.Errorf("bedrock: stream decode error: %w", err)
		}

		msgType := msg.Headers[":message-type"]
		if msgType == "exception" {
			exType := msg.Headers[":exception-type"]
			return nil, fmt.Errorf("bedrock: %s: %s", exType, string(msg.Payload))
		}

		eventType := msg.Headers[":event-type"]
		switch eventType {
		case "messageStart":
			// Role info; always assistant.

		case "contentBlockStart":
			var ev contentBlockStartEvent
			if err := json.Unmarshal(msg.Payload, &ev); err != nil {
				continue
			}
			if ev.Start.ToolUse != nil {
				currentBlockType = "tool_use"
				currentToolID = ev.Start.ToolUse.ToolUseID
				currentToolName = ev.Start.ToolUse.Name
				currentToolInput.Reset()
			} else {
				currentBlockType = "text"
				currentText.Reset()
			}

		case "contentBlockDelta":
			var ev contentBlockDeltaEvent
			if err := json.Unmarshal(msg.Payload, &ev); err != nil {
				continue
			}
			if ev.Delta.Text != nil {
				currentText.WriteString(*ev.Delta.Text)
				if handler != nil {
					handler(*ev.Delta.Text)
				}
			}
			if ev.Delta.ToolUse != nil {
				currentToolInput.WriteString(ev.Delta.ToolUse.Input)
			}

		case "contentBlockStop":
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

		case "messageStop":
			var ev messageStopEvent
			if err := json.Unmarshal(msg.Payload, &ev); err == nil {
				stopReason = strands.StopReason(ev.StopReason)
			}

		case "metadata":
			var ev metadataEvent
			if err := json.Unmarshal(msg.Payload, &ev); err == nil {
				usage.InputTokens = ev.Usage.InputTokens
				usage.OutputTokens = ev.Usage.OutputTokens
				metrics.LatencyMs = ev.Metrics.LatencyMs
			}
		}
	}

	usage.TotalTokens = usage.InputTokens + usage.OutputTokens

	return &strands.ConverseOutput{
		StopReason: stopReason,
		Message: strands.Message{
			Role:    strands.RoleAssistant,
			Content: blocks,
		},
		Usage:   usage,
		Metrics: metrics,
	}, nil
}

// --- Type conversion ---

func convertMessages(msgs []strands.Message) []bedrockMessage {
	out := make([]bedrockMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, bedrockMessage{
			Role:    string(m.Role),
			Content: convertContentBlocks(m.Content),
		})
	}
	return out
}

func convertContentBlocks(blocks []strands.ContentBlock) []bedrockContent {
	out := make([]bedrockContent, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case strands.ContentTypeText:
			text := b.Text
			out = append(out, bedrockContent{Text: &text})
		case strands.ContentTypeToolUse:
			if b.ToolUse != nil {
				out = append(out, bedrockContent{
					ToolUse: &bedrockToolUse{
						ToolUseID: b.ToolUse.ID,
						Name:      b.ToolUse.Name,
						Input:     b.ToolUse.Input,
					},
				})
			}
		case strands.ContentTypeToolResult:
			if b.ToolResult != nil {
				var content []bedrockToolResultContent
				for _, c := range b.ToolResult.Content {
					content = append(content, bedrockToolResultContent{Text: c.Text})
				}
				out = append(out, bedrockContent{
					ToolResult: &bedrockToolResult{
						ToolUseID: b.ToolResult.ToolUseID,
						Content:   content,
						Status:    string(b.ToolResult.Status),
					},
				})
			}
		}
	}
	return out
}

func convertToolSpecs(specs []strands.ToolSpec) []bedrockTool {
	out := make([]bedrockTool, 0, len(specs))
	for _, s := range specs {
		out = append(out, bedrockTool{
			ToolSpec: &bedrockToolSpec{
				Name:        s.Name,
				Description: s.Description,
				InputSchema: bedrockInputSchema{JSON: s.InputSchema},
			},
		})
	}
	return out
}

func convertToolChoice(tc *strands.ToolChoice) any {
	switch tc.Type {
	case strands.ToolChoiceAuto:
		return map[string]any{"auto": map[string]any{}}
	case strands.ToolChoiceAny:
		return map[string]any{"any": map[string]any{}}
	case strands.ToolChoiceTool:
		return map[string]any{"tool": map[string]any{"name": tc.Name}}
	default:
		return nil
	}
}

func (p *Provider) convertResponse(resp *converseResponse) *strands.ConverseOutput {
	var blocks []strands.ContentBlock
	if resp.Output.Message != nil {
		for _, c := range resp.Output.Message.Content {
			if c.Text != nil {
				blocks = append(blocks, strands.TextBlock(*c.Text))
			}
			if c.ToolUse != nil {
				blocks = append(blocks, strands.ToolUseBlock(
					c.ToolUse.ToolUseID, c.ToolUse.Name, c.ToolUse.Input,
				))
			}
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
		Metrics: strands.Metrics{
			LatencyMs: resp.Metrics.LatencyMs,
		},
	}
}

// --- Bedrock Converse API types (internal) ---

type converseRequest struct {
	Messages        []bedrockMessage `json:"messages"`
	System          []systemContent  `json:"system,omitempty"`
	InferenceConfig *inferenceConfig `json:"inferenceConfig,omitempty"`
	ToolConfig      *toolConfig      `json:"toolConfig,omitempty"`
}

type systemContent struct {
	Text string `json:"text"`
}

type inferenceConfig struct {
	MaxTokens int `json:"maxTokens"`
}

type toolConfig struct {
	Tools      []bedrockTool `json:"tools"`
	ToolChoice any           `json:"toolChoice,omitempty"`
}

type bedrockTool struct {
	ToolSpec *bedrockToolSpec `json:"toolSpec"`
}

type bedrockToolSpec struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	InputSchema bedrockInputSchema `json:"inputSchema"`
}

type bedrockInputSchema struct {
	JSON any `json:"json"`
}

type bedrockMessage struct {
	Role    string           `json:"role"`
	Content []bedrockContent `json:"content"`
}

type bedrockContent struct {
	Text       *string            `json:"text,omitempty"`
	ToolUse    *bedrockToolUse    `json:"toolUse,omitempty"`
	ToolResult *bedrockToolResult `json:"toolResult,omitempty"`
}

type bedrockToolUse struct {
	ToolUseID string         `json:"toolUseId"`
	Name      string         `json:"name"`
	Input     map[string]any `json:"input"`
}

type bedrockToolResult struct {
	ToolUseID string                     `json:"toolUseId"`
	Content   []bedrockToolResultContent `json:"content"`
	Status    string                     `json:"status"`
}

type bedrockToolResultContent struct {
	Text string `json:"text,omitempty"`
}

type converseResponse struct {
	Output     converseOutput `json:"output"`
	StopReason string         `json:"stopReason"`
	Usage      bedrockUsage   `json:"usage"`
	Metrics    bedrockMetrics `json:"metrics"`
}

type converseOutput struct {
	Message *bedrockMessage `json:"message"`
}

type bedrockUsage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

type bedrockMetrics struct {
	LatencyMs int64 `json:"latencyMs"`
}

// --- Streaming event types ---

type contentBlockStartEvent struct {
	Start contentBlockStart `json:"start"`
}

type contentBlockStart struct {
	ToolUse *streamToolUseStart `json:"toolUse,omitempty"`
}

type streamToolUseStart struct {
	ToolUseID string `json:"toolUseId"`
	Name      string `json:"name"`
}

type contentBlockDeltaEvent struct {
	Delta contentBlockDelta `json:"delta"`
}

type contentBlockDelta struct {
	Text    *string             `json:"text,omitempty"`
	ToolUse *streamToolUseDelta `json:"toolUse,omitempty"`
}

type streamToolUseDelta struct {
	Input string `json:"input"`
}

type messageStopEvent struct {
	StopReason string `json:"stopReason"`
}

type metadataEvent struct {
	Usage   bedrockUsage   `json:"usage"`
	Metrics bedrockMetrics `json:"metrics"`
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// --- Shared credentials/config file support ---

// readAWSCredentials reads the [default] profile from ~/.aws/credentials.
func readAWSCredentials() (accessKey, secretKey, sessionToken string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	path := envOr("AWS_SHARED_CREDENTIALS_FILE", home+"/.aws/credentials")
	sections := parseINI(path)
	profile := envOr("AWS_PROFILE", "default")
	sec := sections[profile]
	return sec["aws_access_key_id"], sec["aws_secret_access_key"], sec["aws_session_token"]
}

// readAWSConfig reads a value from the [default] profile in ~/.aws/config.
func readAWSConfig(key string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := envOr("AWS_CONFIG_FILE", home+"/.aws/config")
	sections := parseINI(path)
	profile := envOr("AWS_PROFILE", "default")
	// In ~/.aws/config, the default profile is [default] but named profiles are [profile name].
	sec := sections[profile]
	if sec[key] == "" && profile != "default" {
		sec = sections["profile "+profile]
	}
	return sec[key]
}

// parseINI reads a simple INI file into section -> key -> value.
func parseINI(path string) map[string]map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	sections := make(map[string]map[string]string)
	current := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' && line[len(line)-1] == ']' {
			current = strings.TrimSpace(line[1 : len(line)-1])
			if sections[current] == nil {
				sections[current] = make(map[string]string)
			}
			continue
		}
		if i := strings.IndexByte(line, '='); i > 0 && current != "" {
			k := strings.TrimSpace(line[:i])
			v := strings.TrimSpace(line[i+1:])
			sections[current][k] = v
		}
	}
	return sections
}
