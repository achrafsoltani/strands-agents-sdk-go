# Strands Agents SDK for Go — Documentation

Implementation documentation for the Go port of the Strands Agents SDK.

For the original Python SDK's architecture analysis, see [`architecture/`](../architecture/).

## Documents

| Document | Description |
|----------|-------------|
| [Getting Started](getting-started.md) | Installation, credentials, first agent, streaming |
| [Core Concepts](core-concepts.md) | Agent lifecycle, event loop, messages, tools, hooks, executors |
| [Providers Overview](providers/README.md) | Model interface, available providers, writing custom providers |
| [Anthropic Provider](providers/anthropic.md) | Direct Anthropic API with SSE streaming |
| [Bedrock Provider](providers/bedrock.md) | AWS Bedrock Converse API with SigV4 and binary event stream |
| [API Reference](api-reference.md) | All exported types, interfaces, functions, and constants |
| [Examples](examples.md) | Streaming, multi-tool, hooks, Bedrock, multi-turn conversations |

## Go vs Python SDK

| Aspect | Python SDK | Go SDK |
|--------|-----------|--------|
| Event loop | Recursive (`recurse_event_loop()`) | Iterative (`for` loop) — Go lacks tail-call optimisation |
| Streaming | `AsyncGenerator` yields `StreamEvent` | `chan Event` from `Agent.Stream()` |
| Model interface | `stream()` yields events | `Converse()` + `ConverseStream()` with callback |
| Tool execution | `asyncio.gather()` | `sync.WaitGroup` goroutines |
| Dependencies | boto3, httpx, pydantic, etc. | Zero — stdlib only |
| Hook ordering | LIFO for After* (decorator stacking) | LIFO for After* (matching Python semantics) |
| Providers | 13+ via pip packages | Anthropic, Bedrock (more planned) |
| Structured output | Pydantic model enforcement | Planned (generics) |
| MCP | First-class `MCPClient` | Planned |

## Design Decisions

1. **Iterative event loop** — Go has no tail-call optimisation, so deep recursion risks stack overflow. The loop is a simple `for` with a max-cycles guard.

2. **Channels for streaming, callbacks for providers** — `Agent.Stream()` returns a `chan Event` (idiomatic Go concurrency). Model providers use a `StreamHandler` callback because it avoids goroutine management inside providers.

3. **ConcurrentExecutor as default** — Goroutines are cheap. Parallel tool execution is the default, with `WithSequentialExecution()` available when ordering matters.

4. **Zero dependencies** — The entire SDK, including HTTP clients, SigV4 signing, SSE parsing, and binary event stream decoding, uses only the Go standard library. This eliminates version conflicts and reduces binary size.

5. **Functional options pattern** — Both the Agent and providers use `Option` functions for configuration, following the standard Go idiom for constructors with many optional parameters.
