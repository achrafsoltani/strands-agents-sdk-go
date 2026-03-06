# AWS Bedrock Provider

**Source**: `provider/bedrock/bedrock.go`, `sigv4.go`, `eventstream.go`

The Bedrock provider implements the Strands `Model` interface for the [AWS Bedrock Converse API](https://docs.aws.amazon.com/bedrock/latest/APIReference/API_runtime_Converse.html). It uses only the Go standard library — including a from-scratch implementation of AWS Signature V4 signing and the AWS binary event stream protocol.

## Usage

```go
import "github.com/achrafsoltani/strands-agents-sdk-go/provider/bedrock"

// Uses ~/.aws/credentials and ~/.aws/config automatically
model := bedrock.New()

// Or with explicit configuration:
model := bedrock.New(
    bedrock.WithRegion("eu-west-1"),
    bedrock.WithModel("anthropic.claude-3-5-sonnet-20241022-v2:0"),
    bedrock.WithMaxTokens(4096),
    bedrock.WithCredentials("AKIA...", "secret...", ""),
)
```

## Options

| Option | Default | Description |
|--------|---------|-------------|
| `WithRegion(r)` | `$AWS_DEFAULT_REGION` / `us-east-1` | AWS region |
| `WithModel(m)` | `anthropic.claude-3-5-sonnet-20241022-v2:0` | Bedrock model ID |
| `WithMaxTokens(n)` | `4096` | Maximum tokens in response |
| `WithCredentials(ak, sk, st)` | Env vars / `~/.aws/credentials` | Explicit credentials |
| `WithEndpointURL(url)` | `https://bedrock-runtime.{region}.amazonaws.com` | Custom endpoint |

## Credential Chain

The provider resolves credentials in priority order:

```
1. WithCredentials() option          ← explicit, highest priority
2. AWS_ACCESS_KEY_ID env var         ← standard env vars
   AWS_SECRET_ACCESS_KEY
   AWS_SESSION_TOKEN
3. ~/.aws/credentials file           ← shared credentials (AWS CLI)
   [default] profile
   (or $AWS_PROFILE)
```

Region resolution follows the same pattern:

```
1. WithRegion() option
2. AWS_DEFAULT_REGION or AWS_REGION env var
3. ~/.aws/config file
4. Fallback: us-east-1
```

The shared credentials file parser supports:
- `[default]` and named profiles via `$AWS_PROFILE`
- Custom file locations via `$AWS_SHARED_CREDENTIALS_FILE` and `$AWS_CONFIG_FILE`
- Standard INI format with `#` and `;` comments

## API Mapping

### Endpoints

| Method | Path |
|--------|------|
| `Converse()` | `POST /model/{modelId}/converse` |
| `ConverseStream()` | `POST /model/{modelId}/converse-stream` |

### Request Format

The Bedrock Converse API uses a different JSON structure from the Anthropic API:

```json
{
  "messages": [
    {
      "role": "user",
      "content": [{"text": "Hello"}]
    }
  ],
  "system": [{"text": "Be helpful"}],
  "inferenceConfig": {"maxTokens": 4096},
  "toolConfig": {
    "tools": [
      {
        "toolSpec": {
          "name": "calc",
          "description": "Calculator",
          "inputSchema": {"json": {"type": "object", ...}}
        }
      }
    ],
    "toolChoice": {"auto": {}}
  }
}
```

Key differences from the Anthropic API:
- **No `type` discriminator** — content blocks use field presence (`text`, `toolUse`, `toolResult`) instead of a `type` field
- **System prompt** is an array of objects: `[{"text": "..."}]`
- **Tool schema** is wrapped: `{"inputSchema": {"json": {schema}}}` instead of `{"input_schema": {schema}}`
- **Tool choice** uses nested empty objects: `{"auto": {}}` instead of `{"type": "auto"}`

### Tool Choice Mapping

| Strands | Bedrock |
|---------|---------|
| `ToolChoiceAuto` | `{"auto": {}}` |
| `ToolChoiceAny` | `{"any": {}}` |
| `ToolChoiceTool` | `{"tool": {"name": "..."}}` |

### Non-Streaming Response

```json
{
  "output": {
    "message": {
      "role": "assistant",
      "content": [{"text": "Hello!"}]
    }
  },
  "stopReason": "end_turn",
  "usage": {"inputTokens": 10, "outputTokens": 5},
  "metrics": {"latencyMs": 200}
}
```

## SigV4 Signing

**Source**: `sigv4.go`

Every request to Bedrock must be signed with [AWS Signature V4](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_aws-signing.html). The implementation uses only `crypto/hmac`, `crypto/sha256`, and `encoding/hex`.

### Signing Steps

```
1. Canonical Request
   POST
   /model/anthropic.claude-3-5-sonnet-20241022-v2%3A0/converse
   (empty query string)
   content-type:application/json
   host:bedrock-runtime.eu-west-1.amazonaws.com
   x-amz-date:20260305T110000Z

   content-type;host;x-amz-date
   {sha256 of request body}

2. String to Sign
   AWS4-HMAC-SHA256
   20260305T110000Z
   20260305/eu-west-1/bedrock/aws4_request
   {sha256 of canonical request}

3. Signing Key Derivation
   HMAC-SHA256("AWS4" + secret, date)
     → HMAC-SHA256(result, region)
       → HMAC-SHA256(result, service)
         → HMAC-SHA256(result, "aws4_request")

4. Signature
   HMAC-SHA256(signing_key, string_to_sign) → hex

5. Authorization Header
   AWS4-HMAC-SHA256 Credential=AKIA.../20260305/eu-west-1/bedrock/aws4_request,
   SignedHeaders=content-type;host;x-amz-date,
   Signature=abc123...
```

### Path Encoding

Model IDs like `anthropic.claude-3-5-sonnet-20241022-v2:0` contain colons which must be percent-encoded (`%3A`) in the canonical URI. The `canonicalizePath()` function encodes each path segment using AWS's URI encoding rules (only unreserved characters `A-Za-z0-9-_.~` pass through unencoded).

### Service Name

The SigV4 signing service name for Bedrock Runtime is `bedrock` (not `bedrock-runtime`).

### Session Tokens

When using temporary credentials (STS, SSO), the session token is sent via the `X-Amz-Security-Token` header and included in the signature.

## Binary Event Stream

**Source**: `eventstream.go`

The `ConverseStream` endpoint returns responses in the [AWS event stream binary protocol](https://docs.aws.amazon.com/transcribe/latest/dg/event-stream.html), not text-based SSE like the Anthropic provider.

### Frame Format

```
┌────────────────────────────────────────────────────────┐
│ Prelude (12 bytes)                                      │
│   total_length:  4 bytes (big-endian uint32)            │
│   headers_length: 4 bytes (big-endian uint32)           │
│   prelude_crc:   4 bytes (big-endian uint32, CRC32)     │
├────────────────────────────────────────────────────────┤
│ Headers (variable length)                               │
│   Sequence of: name_len(1) + name + type(1) + value     │
│   String values: type=7, value_len(2) + value           │
├────────────────────────────────────────────────────────┤
│ Payload (variable length)                               │
│   JSON-encoded event data                               │
├────────────────────────────────────────────────────────┤
│ Message CRC (4 bytes, CRC32)                            │
│   Covers everything above (prelude + headers + payload) │
└────────────────────────────────────────────────────────┘
```

### CRC Algorithm

**IMPORTANT**: The event stream uses CRC32 **IEEE** (polynomial 0xEDB88320), NOT CRC32C (Castagnoli). This is a common source of confusion — some AWS documentation references "CRC32" without specifying the variant. The `aws-sdk-go-v2` source code confirms IEEE (`crc32.IEEETable`).

In Go:
```go
import "hash/crc32"
crc := crc32.ChecksumIEEE(data) // Correct
// NOT: crc32.MakeTable(crc32.Castagnoli) — this will fail against real Bedrock responses
```

### Header Types

| Type ID | Type | Size |
|---------|------|------|
| 0 | Bool true | 0 |
| 1 | Bool false | 0 |
| 2 | Byte | 1 |
| 3 | Short | 2 |
| 4 | Int | 4 |
| 5 | Long | 8 |
| 6 | Bytes | 2 (length) + N |
| 7 | String | 2 (length) + N |
| 8 | Timestamp | 8 |
| 9 | UUID | 16 |

The three headers used by Bedrock:
- `:message-type` — `"event"` or `"exception"`
- `:event-type` — event name (see below)
- `:content-type` — always `"application/json"`

### Event Types

| Event | Payload | Purpose |
|-------|---------|---------|
| `messageStart` | `{"role": "assistant"}` | Stream begins |
| `contentBlockStart` | `{"contentBlockIndex": N, "start": {...}}` | New text or tool block |
| `contentBlockDelta` | `{"contentBlockIndex": N, "delta": {...}}` | Incremental content |
| `contentBlockStop` | `{"contentBlockIndex": N}` | Block complete |
| `messageStop` | `{"stopReason": "end_turn"}` | Stream ends |
| `metadata` | `{"usage": {...}, "metrics": {...}}` | Token counts and latency |

### Text Delta

```json
{"contentBlockIndex": 0, "delta": {"text": "Hello"}}
```

### Tool Use Delta

Tool input arrives as partial JSON strings:

```json
{"contentBlockIndex": 0, "delta": {"toolUse": {"input": "{\"expr\":"}}}
{"contentBlockIndex": 0, "delta": {"toolUse": {"input": "\"2+2\"}"}}}
```

The provider concatenates these and parses the full JSON on `contentBlockStop`.

### Exceptions

Exceptions use `:message-type: "exception"` with `:exception-type` identifying the error:

```
:message-type: exception
:exception-type: throttlingException
Payload: {"message": "rate limit exceeded"}
```

## Model IDs

### Standard Models

```go
bedrock.WithModel("anthropic.claude-3-5-sonnet-20241022-v2:0")
bedrock.WithModel("anthropic.claude-3-haiku-20240307-v1:0")
bedrock.WithModel("anthropic.claude-sonnet-4-20250514-v1:0")
```

### Cross-Region Inference Profiles

Prefix with a region code for automatic routing:

```go
bedrock.WithModel("us.anthropic.claude-3-5-sonnet-20241022-v2:0")
bedrock.WithModel("eu.anthropic.claude-3-5-sonnet-20241022-v2:0")
```

## Error Handling

### HTTP Errors

Non-200 responses return the status code and body:

```
bedrock: API error 403: {"message":"Access denied"}
```

### Stream Exceptions

Event stream exceptions include the exception type:

```
bedrock: throttlingException: {"message":"rate limit exceeded"}
```

### Common Errors

| Error | Cause | Fix |
|-------|-------|-----|
| `API error 403` | Bad credentials or missing Bedrock access | Check IAM policy |
| `throttlingException` | Rate limit | Use `AfterModelCall` retry hook |
| `validationException` | Invalid request (bad model ID, etc.) | Check model ID format |
| `prelude CRC mismatch` | Wrong CRC algorithm | Must use CRC32 IEEE, not Castagnoli |

## Testing

25 tests using `httptest.Server` with binary event stream encoding:

- Construction: defaults, options, baseURL
- Converse: text, tool use, API error, request format, tool results, session token
- ConverseStream: text, tool use, exception, nil handler
- SigV4: header format, determinism, session token, no session token
- Path/URI: canonicalize path, URI encoding
- Event stream: valid decode, CRC mismatch, multiple messages
- Conversions: tool choice, messages

## File Structure

```
provider/bedrock/
├── bedrock.go        # Provider, request building, event parsing, type conversion, API types
├── sigv4.go          # AWS Signature V4 signing (HMAC-SHA256, canonical request)
├── eventstream.go    # AWS binary event stream decoder (CRC32 IEEE, header parsing)
└── bedrock_test.go   # 25 tests with binary event stream encoding helpers
```
