# Strands Agents SDK — Architecture Documentation

Comprehensive reverse-engineering of the [Strands Agents SDK](https://github.com/strands-agents) architecture, based on analysis of the Python reference implementation.

## Documents

| # | Document | Diagram | Description |
|---|----------|---------|-------------|
| 1 | [Overview](01-overview.md) | [High-Level Architecture](diagrams/01-high-level-architecture.svg) | SDK overview, design principles, package structure |
| 2 | [Event Loop](02-event-loop.md) | [Event Loop Flow](diagrams/02-event-loop.svg) | Core execution cycle — the heart of the agent |
| 3 | [Model Providers](03-model-providers.md) | [Provider Architecture](diagrams/03-model-provider.svg) | Abstract Model interface and 13+ backends |
| 4 | [Tool System](04-tool-system.md) | [Tool Architecture](diagrams/04-tool-system.svg) | Registration, execution, MCP, hot-reload |
| 5 | [Hooks & Plugins](05-hooks-and-plugins.md) | [Lifecycle Events](diagrams/05-hooks-lifecycle.svg) | Event-driven extension system |
| 6 | [Multi-Agent](06-multi-agent.md) | [Multi-Agent Patterns](diagrams/06-multi-agent.svg) | Graph, Swarm, Agents-as-Tools, A2A |
| 7 | [Session & State](07-session-and-state.md) | [Session Management](diagrams/07-session-state.svg) | Persistence, conversation management |
| 8 | [Data Flow](08-data-flow.md) | [End-to-End Flow](diagrams/08-data-flow.svg) | Complete invocation trace |
| 9 | [Type Reference](09-type-reference.md) | — | All interfaces and types for Go port |

## Diagrams

All diagrams are SVG files in the `diagrams/` folder. Convert to PNG:

```bash
cd diagrams && for f in *.svg; do rsvg-convert -w 1800 "$f" -o "${f%.svg}.png"; done
```

## Key Architectural Insights

1. **The event loop is recursive, not iterative** — each tool execution triggers `recurse_event_loop()` which creates a new cycle. This builds a trace tree.

2. **Everything is async** — the core loop is `AsyncGenerator[TypedEvent, None]`. Sync wrappers (`__call__`) use `asyncio.run()`.

3. **Hooks control flow** — retry (model and tool), cancel, and interrupt are all driven by hook events, not hard-coded logic.

4. **Tools are the only capability** — even structured output is implemented as a hidden tool that the model must call.

5. **The model drives everything** — there is no explicit state machine. The LLM decides which tools to call and when to stop.

6. **MCP is a first-class citizen** — MCP servers are treated as tool providers, not a bolt-on.

7. **Multi-agent builds on single-agent** — Graph and Swarm both compose `Agent` instances, reusing the same event loop and hook infrastructure.
