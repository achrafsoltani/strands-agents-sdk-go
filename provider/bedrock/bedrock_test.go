package bedrock

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	strands "github.com/achrafsoltani/strands-agents-sdk-go"
)

// --- Event stream encoding helpers (test only) ---

func encodeEvent(eventType string, payload []byte) []byte {
	return encodeEventMsg("event", eventType, "", payload)
}

func encodeException(exceptionType string, payload []byte) []byte {
	return encodeEventMsg("exception", "", exceptionType, payload)
}

func encodeEventMsg(messageType, eventType, exceptionType string, payload []byte) []byte {
	var headersBuf bytes.Buffer
	writeStringHeader(&headersBuf, ":content-type", "application/json")
	if eventType != "" {
		writeStringHeader(&headersBuf, ":event-type", eventType)
	}
	if exceptionType != "" {
		writeStringHeader(&headersBuf, ":exception-type", exceptionType)
	}
	writeStringHeader(&headersBuf, ":message-type", messageType)
	headers := headersBuf.Bytes()

	totalLength := uint32(12 + len(headers) + len(payload) + 4)
	headersLength := uint32(len(headers))

	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, totalLength)
	binary.Write(&buf, binary.BigEndian, headersLength)
	preludeCRC := eventCRC(buf.Bytes())
	binary.Write(&buf, binary.BigEndian, preludeCRC)
	buf.Write(headers)
	buf.Write(payload)
	messageCRC := eventCRC(buf.Bytes())
	binary.Write(&buf, binary.BigEndian, messageCRC)

	return buf.Bytes()
}

func writeStringHeader(buf *bytes.Buffer, name, value string) {
	buf.WriteByte(byte(len(name)))
	buf.WriteString(name)
	buf.WriteByte(7) // String type
	binary.Write(buf, binary.BigEndian, uint16(len(value)))
	buf.WriteString(value)
}

// --- Mock server ---

type mockServer struct {
	server      *httptest.Server
	lastRequest *converseRequest
	lastPath    string
	lastHeaders http.Header
}

func newMockServer(handler http.HandlerFunc) *mockServer {
	ms := &mockServer{}
	ms.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ms.lastPath = r.URL.Path
		ms.lastHeaders = r.Header.Clone()
		var req converseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			ms.lastRequest = &req
		}
		handler(w, r)
	}))
	return ms
}

func (ms *mockServer) close() { ms.server.Close() }

func (ms *mockServer) provider() *Provider {
	return New(
		WithCredentials("AKIDEXAMPLE", "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", ""),
		WithEndpointURL(ms.server.URL),
		WithModel("test-model"),
		WithMaxTokens(1024),
		WithRegion("us-east-1"),
	)
}

func jsonBytes(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// --- Provider construction tests ---

func TestNew_Defaults(t *testing.T) {
	p := New()
	if p.model != "anthropic.claude-3-5-sonnet-20241022-v2:0" {
		t.Errorf("model = %q", p.model)
	}
	if p.maxTokens != 4096 {
		t.Errorf("maxTokens = %d", p.maxTokens)
	}
}

func TestNew_WithOptions(t *testing.T) {
	p := New(
		WithCredentials("ak", "sk", "st"),
		WithModel("anthropic.claude-3-haiku-20240307-v1:0"),
		WithMaxTokens(2048),
		WithRegion("eu-west-1"),
		WithEndpointURL("https://custom.endpoint.com"),
	)
	if p.accessKey != "ak" {
		t.Errorf("accessKey = %q", p.accessKey)
	}
	if p.secretKey != "sk" {
		t.Errorf("secretKey = %q", p.secretKey)
	}
	if p.sessionToken != "st" {
		t.Errorf("sessionToken = %q", p.sessionToken)
	}
	if p.model != "anthropic.claude-3-haiku-20240307-v1:0" {
		t.Errorf("model = %q", p.model)
	}
	if p.maxTokens != 2048 {
		t.Errorf("maxTokens = %d", p.maxTokens)
	}
	if p.region != "eu-west-1" {
		t.Errorf("region = %q", p.region)
	}
	if p.endpointURL != "https://custom.endpoint.com" {
		t.Errorf("endpointURL = %q", p.endpointURL)
	}
}

func TestBaseURL_Default(t *testing.T) {
	p := New(WithRegion("ap-southeast-1"))
	if got := p.baseURL(); got != "https://bedrock-runtime.ap-southeast-1.amazonaws.com" {
		t.Errorf("baseURL = %q", got)
	}
}

func TestBaseURL_Override(t *testing.T) {
	p := New(WithEndpointURL("https://custom.api.com/"))
	if got := p.baseURL(); got != "https://custom.api.com" {
		t.Errorf("baseURL = %q (trailing slash should be trimmed)", got)
	}
}

// --- Non-streaming Converse tests ---

func TestProvider_Converse_TextResponse(t *testing.T) {
	text := "Hello from Bedrock!"
	ms := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		resp := converseResponse{
			Output: converseOutput{
				Message: &bedrockMessage{
					Role:    "assistant",
					Content: []bedrockContent{{Text: &text}},
				},
			},
			StopReason: "end_turn",
			Usage:      bedrockUsage{InputTokens: 10, OutputTokens: 5},
			Metrics:    bedrockMetrics{LatencyMs: 200},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer ms.close()

	p := ms.provider()
	output, err := p.Converse(context.Background(), &strands.ConverseInput{
		Messages:     []strands.Message{strands.UserMessage("Hi")},
		SystemPrompt: "Be helpful",
	})
	if err != nil {
		t.Fatalf("Converse failed: %v", err)
	}
	if output.StopReason != strands.StopReasonEndTurn {
		t.Errorf("StopReason = %q", output.StopReason)
	}
	if output.Message.Text() != "Hello from Bedrock!" {
		t.Errorf("Message = %q", output.Message.Text())
	}
	if output.Usage.InputTokens != 10 || output.Usage.OutputTokens != 5 || output.Usage.TotalTokens != 15 {
		t.Errorf("Usage = %+v", output.Usage)
	}
	if output.Metrics.LatencyMs != 200 {
		t.Errorf("LatencyMs = %d", output.Metrics.LatencyMs)
	}
}

func TestProvider_Converse_ToolUseResponse(t *testing.T) {
	ms := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		resp := converseResponse{
			Output: converseOutput{
				Message: &bedrockMessage{
					Role: "assistant",
					Content: []bedrockContent{
						{ToolUse: &bedrockToolUse{
							ToolUseID: "tu_1",
							Name:      "calculator",
							Input:     map[string]any{"expr": "2+2"},
						}},
					},
				},
			},
			StopReason: "tool_use",
			Usage:      bedrockUsage{InputTokens: 15, OutputTokens: 8},
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
	if uses[0].Name != "calculator" || uses[0].ID != "tu_1" {
		t.Errorf("tool use = %+v", uses[0])
	}
}

func TestProvider_Converse_APIError(t *testing.T) {
	ms := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Access denied"}`))
	})
	defer ms.close()

	p := ms.provider()
	_, err := p.Converse(context.Background(), &strands.ConverseInput{
		Messages: []strands.Message{strands.UserMessage("Hi")},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error = %v, should contain status code", err)
	}
}

func TestProvider_Converse_RequestFormat(t *testing.T) {
	ms := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		text := "ok"
		resp := converseResponse{
			Output: converseOutput{
				Message: &bedrockMessage{
					Role:    "assistant",
					Content: []bedrockContent{{Text: &text}},
				},
			},
			StopReason: "end_turn",
			Usage:      bedrockUsage{InputTokens: 5, OutputTokens: 1},
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
			{Name: "echo", Description: "echoes input", InputSchema: map[string]any{"type": "object"}},
		},
		ToolChoice: &strands.ToolChoice{Type: strands.ToolChoiceAuto},
	})

	if ms.lastRequest == nil {
		t.Fatal("no request captured")
	}
	// Verify path.
	if ms.lastPath != "/model/test-model/converse" {
		t.Errorf("path = %q", ms.lastPath)
	}
	// Verify SigV4 headers.
	if auth := ms.lastHeaders.Get("Authorization"); !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=") {
		t.Errorf("Authorization = %q", auth)
	}
	if ms.lastHeaders.Get("X-Amz-Date") == "" {
		t.Error("missing X-Amz-Date header")
	}
	// Verify system prompt.
	if len(ms.lastRequest.System) != 1 || ms.lastRequest.System[0].Text != "system prompt" {
		t.Errorf("system = %+v", ms.lastRequest.System)
	}
	// Verify inference config.
	if ms.lastRequest.InferenceConfig == nil || ms.lastRequest.InferenceConfig.MaxTokens != 1024 {
		t.Errorf("inferenceConfig = %+v", ms.lastRequest.InferenceConfig)
	}
	// Verify tools.
	if ms.lastRequest.ToolConfig == nil || len(ms.lastRequest.ToolConfig.Tools) != 1 {
		t.Fatalf("toolConfig = %+v", ms.lastRequest.ToolConfig)
	}
	tool := ms.lastRequest.ToolConfig.Tools[0]
	if tool.ToolSpec == nil || tool.ToolSpec.Name != "echo" {
		t.Errorf("tool = %+v", tool)
	}
	// Verify messages.
	if len(ms.lastRequest.Messages) != 1 {
		t.Fatalf("messages count = %d", len(ms.lastRequest.Messages))
	}
	if ms.lastRequest.Messages[0].Role != "user" {
		t.Errorf("message role = %q", ms.lastRequest.Messages[0].Role)
	}
}

func TestProvider_Converse_ToolResultInMessage(t *testing.T) {
	ms := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		text := "The result was 4"
		resp := converseResponse{
			Output: converseOutput{
				Message: &bedrockMessage{
					Role:    "assistant",
					Content: []bedrockContent{{Text: &text}},
				},
			},
			StopReason: "end_turn",
			Usage:      bedrockUsage{InputTokens: 20, OutputTokens: 5},
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

	if ms.lastRequest == nil {
		t.Fatal("no request captured")
	}
	if len(ms.lastRequest.Messages) != 3 {
		t.Fatalf("messages count = %d, want 3", len(ms.lastRequest.Messages))
	}
	// Verify assistant message contains tool use.
	assistantMsg := ms.lastRequest.Messages[1]
	if assistantMsg.Role != "assistant" {
		t.Errorf("role = %q", assistantMsg.Role)
	}
	if len(assistantMsg.Content) != 1 || assistantMsg.Content[0].ToolUse == nil {
		t.Fatalf("assistant content = %+v", assistantMsg.Content)
	}
	if assistantMsg.Content[0].ToolUse.ToolUseID != "tu_1" {
		t.Errorf("toolUseId = %q", assistantMsg.Content[0].ToolUse.ToolUseID)
	}
	// Verify user message contains tool result.
	toolMsg := ms.lastRequest.Messages[2]
	if toolMsg.Role != "user" {
		t.Errorf("role = %q", toolMsg.Role)
	}
	if len(toolMsg.Content) != 1 || toolMsg.Content[0].ToolResult == nil {
		t.Fatalf("tool result content = %+v", toolMsg.Content)
	}
	tr := toolMsg.Content[0].ToolResult
	if tr.ToolUseID != "tu_1" || tr.Status != "success" {
		t.Errorf("tool result = %+v", tr)
	}
}

func TestProvider_Converse_SessionToken(t *testing.T) {
	ms := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		text := "ok"
		resp := converseResponse{
			Output: converseOutput{
				Message: &bedrockMessage{
					Role:    "assistant",
					Content: []bedrockContent{{Text: &text}},
				},
			},
			StopReason: "end_turn",
			Usage:      bedrockUsage{InputTokens: 5, OutputTokens: 1},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer ms.close()

	p := New(
		WithCredentials("AKID", "SECRET", "SESSION_TOKEN"),
		WithEndpointURL(ms.server.URL),
		WithModel("test-model"),
		WithRegion("us-east-1"),
	)
	_, err := p.Converse(context.Background(), &strands.ConverseInput{
		Messages: []strands.Message{strands.UserMessage("test")},
	})
	if err != nil {
		t.Fatalf("Converse failed: %v", err)
	}
	if ms.lastHeaders.Get("X-Amz-Security-Token") != "SESSION_TOKEN" {
		t.Errorf("X-Amz-Security-Token = %q", ms.lastHeaders.Get("X-Amz-Security-Token"))
	}
}

// --- Streaming ConverseStream tests ---

func TestProvider_ConverseStream_TextResponse(t *testing.T) {
	ms := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		w.WriteHeader(http.StatusOK)

		events := [][]byte{
			encodeEvent("messageStart", jsonBytes(map[string]any{"role": "assistant"})),
			encodeEvent("contentBlockStart", jsonBytes(map[string]any{
				"contentBlockIndex": 0,
				"start":            map[string]any{},
			})),
			encodeEvent("contentBlockDelta", jsonBytes(map[string]any{
				"contentBlockIndex": 0,
				"delta":            map[string]any{"text": "Hello"},
			})),
			encodeEvent("contentBlockDelta", jsonBytes(map[string]any{
				"contentBlockIndex": 0,
				"delta":            map[string]any{"text": " world"},
			})),
			encodeEvent("contentBlockStop", jsonBytes(map[string]any{
				"contentBlockIndex": 0,
			})),
			encodeEvent("messageStop", jsonBytes(map[string]any{
				"stopReason": "end_turn",
			})),
			encodeEvent("metadata", jsonBytes(map[string]any{
				"usage":   map[string]any{"inputTokens": 12, "outputTokens": 7},
				"metrics": map[string]any{"latencyMs": 150},
			})),
		}

		for _, ev := range events {
			w.Write(ev)
		}
	})
	defer ms.close()

	p := ms.provider()
	var chunks []string
	handler := func(text string) { chunks = append(chunks, text) }

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
	if len(chunks) != 2 || chunks[0] != "Hello" || chunks[1] != " world" {
		t.Errorf("chunks = %v", chunks)
	}
	if output.Usage.InputTokens != 12 || output.Usage.OutputTokens != 7 || output.Usage.TotalTokens != 19 {
		t.Errorf("Usage = %+v", output.Usage)
	}
	if output.Metrics.LatencyMs != 150 {
		t.Errorf("LatencyMs = %d", output.Metrics.LatencyMs)
	}
	// Verify streaming endpoint path.
	if ms.lastPath != "/model/test-model/converse-stream" {
		t.Errorf("path = %q", ms.lastPath)
	}
}

func TestProvider_ConverseStream_ToolUseResponse(t *testing.T) {
	ms := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")

		events := [][]byte{
			encodeEvent("messageStart", jsonBytes(map[string]any{"role": "assistant"})),
			encodeEvent("contentBlockStart", jsonBytes(map[string]any{
				"contentBlockIndex": 0,
				"start": map[string]any{
					"toolUse": map[string]any{"toolUseId": "tu_1", "name": "calc"},
				},
			})),
			encodeEvent("contentBlockDelta", jsonBytes(map[string]any{
				"contentBlockIndex": 0,
				"delta": map[string]any{
					"toolUse": map[string]any{"input": `{"expr":`},
				},
			})),
			encodeEvent("contentBlockDelta", jsonBytes(map[string]any{
				"contentBlockIndex": 0,
				"delta": map[string]any{
					"toolUse": map[string]any{"input": `"2+2"}`},
				},
			})),
			encodeEvent("contentBlockStop", jsonBytes(map[string]any{
				"contentBlockIndex": 0,
			})),
			encodeEvent("messageStop", jsonBytes(map[string]any{
				"stopReason": "tool_use",
			})),
			encodeEvent("metadata", jsonBytes(map[string]any{
				"usage": map[string]any{"inputTokens": 20, "outputTokens": 15},
			})),
		}

		for _, ev := range events {
			w.Write(ev)
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

func TestProvider_ConverseStream_Exception(t *testing.T) {
	ms := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		w.Write(encodeException("throttlingException", []byte(`{"message":"rate limit exceeded"}`)))
	})
	defer ms.close()

	p := ms.provider()
	_, err := p.ConverseStream(context.Background(), &strands.ConverseInput{
		Messages: []strands.Message{strands.UserMessage("test")},
	}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "throttlingException") {
		t.Errorf("error = %v", err)
	}
}

func TestProvider_ConverseStream_NilHandler(t *testing.T) {
	ms := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")

		events := [][]byte{
			encodeEvent("messageStart", jsonBytes(map[string]any{"role": "assistant"})),
			encodeEvent("contentBlockStart", jsonBytes(map[string]any{
				"contentBlockIndex": 0,
				"start":            map[string]any{},
			})),
			encodeEvent("contentBlockDelta", jsonBytes(map[string]any{
				"contentBlockIndex": 0,
				"delta":            map[string]any{"text": "ok"},
			})),
			encodeEvent("contentBlockStop", jsonBytes(map[string]any{
				"contentBlockIndex": 0,
			})),
			encodeEvent("messageStop", jsonBytes(map[string]any{
				"stopReason": "end_turn",
			})),
			encodeEvent("metadata", jsonBytes(map[string]any{
				"usage": map[string]any{"inputTokens": 5, "outputTokens": 1},
			})),
		}

		for _, ev := range events {
			w.Write(ev)
		}
	})
	defer ms.close()

	p := ms.provider()
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

// --- SigV4 tests ---

func TestSignRequest_HeaderFormat(t *testing.T) {
	req, _ := http.NewRequest("POST", "https://bedrock-runtime.us-east-1.amazonaws.com/model/test/converse", nil)
	req.Header.Set("Content-Type", "application/json")

	signRequestAt(req, "AKIDEXAMPLE", "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", "",
		"us-east-1", "bedrock", []byte("{}"),
		time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
	)

	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20240115/us-east-1/bedrock/aws4_request") {
		t.Errorf("Authorization = %q", auth)
	}
	if !strings.Contains(auth, "SignedHeaders=") {
		t.Errorf("missing SignedHeaders in %q", auth)
	}
	if !strings.Contains(auth, "Signature=") {
		t.Errorf("missing Signature in %q", auth)
	}
	if req.Header.Get("X-Amz-Date") != "20240115T120000Z" {
		t.Errorf("X-Amz-Date = %q", req.Header.Get("X-Amz-Date"))
	}
}

func TestSignRequest_Deterministic(t *testing.T) {
	sign := func() string {
		req, _ := http.NewRequest("POST", "https://host.example.com/path", nil)
		req.Header.Set("Content-Type", "application/json")
		signRequestAt(req, "AK", "SK", "", "us-east-1", "svc", []byte("body"),
			time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC))
		return req.Header.Get("Authorization")
	}

	sig1 := sign()
	sig2 := sign()
	if sig1 != sig2 {
		t.Errorf("signatures differ:\n  %s\n  %s", sig1, sig2)
	}
}

func TestSignRequest_SessionToken(t *testing.T) {
	req, _ := http.NewRequest("POST", "https://host.example.com/path", nil)
	signRequestAt(req, "AK", "SK", "TOKEN", "us-east-1", "svc", nil,
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))

	if req.Header.Get("X-Amz-Security-Token") != "TOKEN" {
		t.Errorf("X-Amz-Security-Token = %q", req.Header.Get("X-Amz-Security-Token"))
	}
}

func TestSignRequest_NoSessionToken(t *testing.T) {
	req, _ := http.NewRequest("POST", "https://host.example.com/path", nil)
	signRequestAt(req, "AK", "SK", "", "us-east-1", "svc", nil,
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))

	if req.Header.Get("X-Amz-Security-Token") != "" {
		t.Errorf("X-Amz-Security-Token should be empty, got %q", req.Header.Get("X-Amz-Security-Token"))
	}
}

// --- Path and URI encoding tests ---

func TestCanonicalizePath(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"", "/"},
		{"/", "/"},
		{"/model/test/converse", "/model/test/converse"},
		{"/model/anthropic.claude-3-5-sonnet-20241022-v2:0/converse", "/model/anthropic.claude-3-5-sonnet-20241022-v2%3A0/converse"},
		{"/path with spaces/file", "/path%20with%20spaces/file"},
	}
	for _, tc := range tests {
		if got := canonicalizePath(tc.input); got != tc.want {
			t.Errorf("canonicalizePath(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestUriEncode(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"abc", "abc"},
		{"a b", "a%20b"},
		{"a:b", "a%3Ab"},
		{"a/b", "a%2Fb"},
		{"a.b", "a.b"},
		{"a~b", "a~b"},
		{"a-b", "a-b"},
		{"a_b", "a_b"},
	}
	for _, tc := range tests {
		if got := uriEncode(tc.input); got != tc.want {
			t.Errorf("uriEncode(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// --- Event stream decode tests ---

func TestDecodeEventStream_ValidMessage(t *testing.T) {
	data := encodeEvent("testEvent", []byte(`{"key":"value"}`))
	msg, err := decodeEventStream(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if msg.Headers[":event-type"] != "testEvent" {
		t.Errorf("event-type = %q", msg.Headers[":event-type"])
	}
	if msg.Headers[":message-type"] != "event" {
		t.Errorf("message-type = %q", msg.Headers[":message-type"])
	}
	if string(msg.Payload) != `{"key":"value"}` {
		t.Errorf("payload = %q", string(msg.Payload))
	}
}

func TestDecodeEventStream_CRCMismatch(t *testing.T) {
	data := encodeEvent("test", []byte("payload"))
	// Corrupt a byte in the payload area.
	data[len(data)-5] ^= 0xFF

	_, err := decodeEventStream(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected CRC error")
	}
	if !strings.Contains(err.Error(), "CRC mismatch") {
		t.Errorf("error = %v", err)
	}
}

func TestDecodeEventStream_MultipleMessages(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(encodeEvent("first", []byte(`{"n":1}`)))
	buf.Write(encodeEvent("second", []byte(`{"n":2}`)))

	msg1, err := decodeEventStream(&buf)
	if err != nil {
		t.Fatalf("decode msg1 failed: %v", err)
	}
	if msg1.Headers[":event-type"] != "first" {
		t.Errorf("msg1 event-type = %q", msg1.Headers[":event-type"])
	}

	msg2, err := decodeEventStream(&buf)
	if err != nil {
		t.Fatalf("decode msg2 failed: %v", err)
	}
	if msg2.Headers[":event-type"] != "second" {
		t.Errorf("msg2 event-type = %q", msg2.Headers[":event-type"])
	}
}

// --- Conversion tests ---

func TestConvertToolChoice(t *testing.T) {
	tests := []struct {
		input    *strands.ToolChoice
		wantKey  string
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
		if _, exists := m[tc.wantKey]; !exists {
			t.Errorf("missing key %q in %v", tc.wantKey, m)
		}
	}
}

func TestConvertMessages_TextOnly(t *testing.T) {
	msgs := convertMessages([]strands.Message{strands.UserMessage("hello")})
	if len(msgs) != 1 {
		t.Fatalf("got %d messages", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("role = %q", msgs[0].Role)
	}
	if len(msgs[0].Content) != 1 || msgs[0].Content[0].Text == nil || *msgs[0].Content[0].Text != "hello" {
		t.Errorf("content = %+v", msgs[0].Content)
	}
}

// --- Header sort helper for deterministic encoding ---
// (needed by encodeEventMsg; sort keys alphabetically)
var _ = sort.Strings
