package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	strands "github.com/achrafsoltani/strands-agents-sdk-go"
)

// mockAnthropicHandler returns an http.HandlerFunc that responds with pre-configured
// Anthropic API responses. It captures the request body for assertions.
type mockServer struct {
	server      *httptest.Server
	lastRequest *apiRequest
}

func newMockServer(handler http.HandlerFunc) *mockServer {
	ms := &mockServer{}
	ms.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req apiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			ms.lastRequest = &req
		}
		handler(w, r)
	}))
	return ms
}

func (ms *mockServer) close() {
	ms.server.Close()
}

func (ms *mockServer) provider() *Provider {
	return New(
		WithAPIKey("test-key"),
		WithBaseURL(ms.server.URL),
		WithModel("test-model"),
		WithMaxTokens(1024),
	)
}

// --- Non-streaming tests ---

func TestProvider_Converse_TextResponse(t *testing.T) {
	ms := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		resp := apiResponse{
			ID:         "msg_123",
			Type:       "message",
			Role:       "assistant",
			Content:    []apiContent{{Type: "text", Text: "Hello!"}},
			StopReason: "end_turn",
			Usage:      apiUsage{InputTokens: 10, OutputTokens: 5},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer ms.close()

	p := ms.provider()
	input := &strands.ConverseInput{
		Messages:     []strands.Message{strands.UserMessage("Hi")},
		SystemPrompt: "Be helpful",
	}

	output, err := p.Converse(context.Background(), input)
	if err != nil {
		t.Fatalf("Converse failed: %v", err)
	}
	if output.StopReason != strands.StopReasonEndTurn {
		t.Errorf("StopReason = %q", output.StopReason)
	}
	if output.Message.Text() != "Hello!" {
		t.Errorf("Message = %q", output.Message.Text())
	}
	if output.Usage.InputTokens != 10 || output.Usage.OutputTokens != 5 {
		t.Errorf("Usage = %+v", output.Usage)
	}
}

func TestProvider_Converse_ToolUseResponse(t *testing.T) {
	ms := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		resp := apiResponse{
			ID:   "msg_456",
			Role: "assistant",
			Content: []apiContent{
				{Type: "tool_use", ID: "tu_1", Name: "calculator", Input: map[string]any{"expr": "2+2"}},
			},
			StopReason: "tool_use",
			Usage:      apiUsage{InputTokens: 15, OutputTokens: 8},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer ms.close()

	p := ms.provider()
	output, err := p.Converse(context.Background(), &strands.ConverseInput{
		Messages: []strands.Message{strands.UserMessage("Calculate 2+2")},
	})
	if err != nil {
		t.Fatalf("Converse failed: %v", err)
	}
	if output.StopReason != strands.StopReasonToolUse {
		t.Errorf("StopReason = %q", output.StopReason)
	}
	uses := output.Message.ToolUses()
	if len(uses) != 1 {
		t.Fatalf("got %d tool uses, want 1", len(uses))
	}
	if uses[0].Name != "calculator" {
		t.Errorf("tool name = %q", uses[0].Name)
	}
}

func TestProvider_Converse_APIError(t *testing.T) {
	ms := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"invalid api key"}}`)
	})
	defer ms.close()

	p := ms.provider()
	_, err := p.Converse(context.Background(), &strands.ConverseInput{
		Messages: []strands.Message{strands.UserMessage("Hi")},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error = %v, should contain status code", err)
	}
}

func TestProvider_Converse_RequestFormat(t *testing.T) {
	ms := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers.
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("anthropic-version = %q", r.Header.Get("anthropic-version"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}

		resp := apiResponse{
			ID:         "msg_789",
			Role:       "assistant",
			Content:    []apiContent{{Type: "text", Text: "ok"}},
			StopReason: "end_turn",
			Usage:      apiUsage{InputTokens: 5, OutputTokens: 1},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer ms.close()

	p := ms.provider()
	_, _ = p.Converse(context.Background(), &strands.ConverseInput{
		Messages:     []strands.Message{strands.UserMessage("test")},
		SystemPrompt: "system prompt",
		ToolSpecs: []strands.ToolSpec{
			{Name: "echo", Description: "echoes", InputSchema: map[string]any{"type": "object"}},
		},
	})

	if ms.lastRequest == nil {
		t.Fatal("no request captured")
	}
	if ms.lastRequest.Model != "test-model" {
		t.Errorf("model = %q", ms.lastRequest.Model)
	}
	if ms.lastRequest.MaxTokens != 1024 {
		t.Errorf("max_tokens = %d", ms.lastRequest.MaxTokens)
	}
	if ms.lastRequest.System != "system prompt" {
		t.Errorf("system = %q", ms.lastRequest.System)
	}
	if len(ms.lastRequest.Tools) != 1 {
		t.Fatalf("tools count = %d", len(ms.lastRequest.Tools))
	}
	if ms.lastRequest.Tools[0].Name != "echo" {
		t.Errorf("tool name = %q", ms.lastRequest.Tools[0].Name)
	}
	if ms.lastRequest.Stream {
		t.Error("non-streaming request should have stream=false")
	}
}

func TestProvider_Converse_ToolResultInMessage(t *testing.T) {
	ms := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		resp := apiResponse{
			ID:         "msg_tr",
			Role:       "assistant",
			Content:    []apiContent{{Type: "text", Text: "The result was 4"}},
			StopReason: "end_turn",
			Usage:      apiUsage{InputTokens: 20, OutputTokens: 5},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer ms.close()

	p := ms.provider()
	toolResult := strands.TextResult("tu_1", "4")
	_, err := p.Converse(context.Background(), &strands.ConverseInput{
		Messages: []strands.Message{
			strands.UserMessage("Calculate 2+2"),
			{
				Role: strands.RoleAssistant,
				Content: []strands.ContentBlock{
					strands.ToolUseBlock("tu_1", "calc", map[string]any{"expr": "2+2"}),
				},
			},
			{
				Role:    strands.RoleUser,
				Content: []strands.ContentBlock{strands.ToolResultBlock(toolResult)},
			},
		},
	})
	if err != nil {
		t.Fatalf("Converse failed: %v", err)
	}

	// Verify the tool result was serialised correctly.
	if ms.lastRequest == nil {
		t.Fatal("no request captured")
	}
	if len(ms.lastRequest.Messages) != 3 {
		t.Fatalf("messages count = %d, want 3", len(ms.lastRequest.Messages))
	}
	toolResultMsg := ms.lastRequest.Messages[2]
	if toolResultMsg.Role != "user" {
		t.Errorf("tool result message role = %q", toolResultMsg.Role)
	}
	if len(toolResultMsg.Content) != 1 {
		t.Fatalf("content count = %d", len(toolResultMsg.Content))
	}
	if toolResultMsg.Content[0].Type != "tool_result" {
		t.Errorf("content type = %q", toolResultMsg.Content[0].Type)
	}
	if toolResultMsg.Content[0].ToolUseID != "tu_1" {
		t.Errorf("tool_use_id = %q", toolResultMsg.Content[0].ToolUseID)
	}
}

// --- Streaming tests ---

func TestProvider_ConverseStream_TextResponse(t *testing.T) {
	ms := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		events := []string{
			`event: message_start` + "\n" + `data: {"type":"message_start","message":{"id":"msg_s1","role":"assistant","usage":{"input_tokens":12}}}`,
			`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
			`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`,
			`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":0}`,
			`event: message_delta` + "\n" + `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
			`event: message_stop` + "\n" + `data: {"type":"message_stop"}`,
		}

		for _, e := range events {
			fmt.Fprintf(w, "%s\n\n", e)
			if flusher != nil {
				flusher.Flush()
			}
		}
	})
	defer ms.close()

	p := ms.provider()

	var chunks []string
	handler := func(text string) {
		chunks = append(chunks, text)
	}

	output, err := p.ConverseStream(context.Background(), &strands.ConverseInput{
		Messages: []strands.Message{strands.UserMessage("Hi")},
	}, handler)
	if err != nil {
		t.Fatalf("ConverseStream failed: %v", err)
	}

	if output.StopReason != strands.StopReasonEndTurn {
		t.Errorf("StopReason = %q", output.StopReason)
	}
	if output.Message.Text() != "Hello world" {
		t.Errorf("Message = %q", output.Message.Text())
	}
	if len(chunks) != 2 {
		t.Errorf("chunks = %v, want [Hello, world]", chunks)
	}
	if output.Usage.InputTokens != 12 {
		t.Errorf("InputTokens = %d", output.Usage.InputTokens)
	}
	if output.Usage.OutputTokens != 7 {
		t.Errorf("OutputTokens = %d", output.Usage.OutputTokens)
	}
}

func TestProvider_ConverseStream_ToolUseResponse(t *testing.T) {
	ms := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		events := []string{
			`event: message_start` + "\n" + `data: {"type":"message_start","message":{"id":"msg_s2","role":"assistant","usage":{"input_tokens":20}}}`,
			`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tu_1","name":"calc"}}`,
			`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"expr\":"}}`,
			`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"2+2\"}"}}`,
			`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":0}`,
			`event: message_delta` + "\n" + `data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":15}}`,
			`event: message_stop` + "\n" + `data: {"type":"message_stop"}`,
		}

		for _, e := range events {
			fmt.Fprintf(w, "%s\n\n", e)
			if flusher != nil {
				flusher.Flush()
			}
		}
	})
	defer ms.close()

	p := ms.provider()
	output, err := p.ConverseStream(context.Background(), &strands.ConverseInput{
		Messages: []strands.Message{strands.UserMessage("Calculate")},
	}, nil)
	if err != nil {
		t.Fatalf("ConverseStream failed: %v", err)
	}

	if output.StopReason != strands.StopReasonToolUse {
		t.Errorf("StopReason = %q", output.StopReason)
	}
	uses := output.Message.ToolUses()
	if len(uses) != 1 {
		t.Fatalf("got %d tool uses, want 1", len(uses))
	}
	if uses[0].ID != "tu_1" || uses[0].Name != "calc" {
		t.Errorf("tool use = %+v", uses[0])
	}
	if uses[0].Input["expr"] != "2+2" {
		t.Errorf("tool input = %v", uses[0].Input)
	}
}

func TestProvider_ConverseStream_StreamError(t *testing.T) {
	ms := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		events := []string{
			`event: message_start` + "\n" + `data: {"type":"message_start","message":{"id":"msg_err","role":"assistant","usage":{"input_tokens":5}}}`,
			`event: error` + "\n" + `data: {"type":"error","error":{"type":"overloaded_error","message":"server overloaded"}}`,
		}

		for _, e := range events {
			fmt.Fprintf(w, "%s\n\n", e)
			if flusher != nil {
				flusher.Flush()
			}
		}
	})
	defer ms.close()

	p := ms.provider()
	_, err := p.ConverseStream(context.Background(), &strands.ConverseInput{
		Messages: []strands.Message{strands.UserMessage("test")},
	}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "server overloaded") {
		t.Errorf("error = %v", err)
	}
}

func TestProvider_ConverseStream_NilHandler(t *testing.T) {
	ms := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		events := []string{
			`event: message_start` + "\n" + `data: {"type":"message_start","message":{"id":"msg_nil","role":"assistant","usage":{"input_tokens":5}}}`,
			`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
			`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":0}`,
			`event: message_delta` + "\n" + `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
			`event: message_stop` + "\n" + `data: {"type":"message_stop"}`,
		}

		for _, e := range events {
			fmt.Fprintf(w, "%s\n\n", e)
		}
	})
	defer ms.close()

	p := ms.provider()
	// nil handler should not panic.
	output, err := p.ConverseStream(context.Background(), &strands.ConverseInput{
		Messages: []strands.Message{strands.UserMessage("test")},
	}, nil)
	if err != nil {
		t.Fatalf("ConverseStream failed: %v", err)
	}
	if output.Message.Text() != "ok" {
		t.Errorf("Message = %q", output.Message.Text())
	}
}

func TestProvider_ConverseStream_RequestIsStreaming(t *testing.T) {
	ms := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		events := []string{
			`event: message_start` + "\n" + `data: {"type":"message_start","message":{"id":"msg_s","role":"assistant","usage":{"input_tokens":5}}}`,
			`event: message_delta` + "\n" + `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":0}}`,
			`event: message_stop` + "\n" + `data: {"type":"message_stop"}`,
		}
		for _, e := range events {
			fmt.Fprintf(w, "%s\n\n", e)
		}
	})
	defer ms.close()

	p := ms.provider()
	_, _ = p.ConverseStream(context.Background(), &strands.ConverseInput{
		Messages: []strands.Message{strands.UserMessage("test")},
	}, nil)

	if ms.lastRequest == nil {
		t.Fatal("no request captured")
	}
	if !ms.lastRequest.Stream {
		t.Error("streaming request should have stream=true")
	}
}

// --- Provider construction tests ---

func TestNew_Defaults(t *testing.T) {
	p := New()
	if p.model != "claude-sonnet-4-20250514" {
		t.Errorf("model = %q", p.model)
	}
	if p.maxTokens != 4096 {
		t.Errorf("maxTokens = %d", p.maxTokens)
	}
	if p.baseURL != "https://api.anthropic.com" {
		t.Errorf("baseURL = %q", p.baseURL)
	}
}

func TestNew_WithOptions(t *testing.T) {
	p := New(
		WithAPIKey("key123"),
		WithModel("claude-opus-4-20250514"),
		WithMaxTokens(8192),
		WithBaseURL("https://custom.api.com"),
	)
	if p.apiKey != "key123" {
		t.Errorf("apiKey = %q", p.apiKey)
	}
	if p.model != "claude-opus-4-20250514" {
		t.Errorf("model = %q", p.model)
	}
	if p.maxTokens != 8192 {
		t.Errorf("maxTokens = %d", p.maxTokens)
	}
	if p.baseURL != "https://custom.api.com" {
		t.Errorf("baseURL = %q", p.baseURL)
	}
}

// --- Helper conversion tests ---

func TestJsonInt(t *testing.T) {
	m := map[string]any{
		"count": float64(42),
		"name":  "test",
	}
	if v := jsonInt(m, "count"); v != 42 {
		t.Errorf("jsonInt(count) = %d", v)
	}
	if v := jsonInt(m, "name"); v != 0 {
		t.Errorf("jsonInt(name) = %d, want 0", v)
	}
	if v := jsonInt(m, "missing"); v != 0 {
		t.Errorf("jsonInt(missing) = %d, want 0", v)
	}
}

func TestConvertToolChoice(t *testing.T) {
	tests := []struct {
		input    *strands.ToolChoice
		wantType string
	}{
		{&strands.ToolChoice{Type: strands.ToolChoiceAuto}, "auto"},
		{&strands.ToolChoice{Type: strands.ToolChoiceAny}, "any"},
		{&strands.ToolChoice{Type: strands.ToolChoiceTool, Name: "calc"}, "tool"},
	}

	for _, tc := range tests {
		result := convertToolChoice(tc.input)
		m, ok := result.(map[string]any)
		if !ok {
			t.Errorf("convertToolChoice(%v) returned %T", tc.input, result)
			continue
		}
		if m["type"] != tc.wantType {
			t.Errorf("type = %v, want %q", m["type"], tc.wantType)
		}
	}
}
