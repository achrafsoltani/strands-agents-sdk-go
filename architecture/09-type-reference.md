# Type Reference — Key Interfaces for Go Port

This document catalogues every significant type/interface in the SDK as a reference for implementing a Go port.

## Core Agent Types

### Agent
```
Agent {
    model: Model
    messages: Messages
    system_prompt: str | None
    system_prompt_content: list[SystemContentBlock]
    state: AgentState
    tool_registry: ToolRegistry
    tool_executor: ToolExecutor
    hooks: HookRegistry
    callback_handler: CallbackHandler
    conversation_manager: ConversationManager
    session_manager: SessionManager | None
    retry_strategy: ModelRetryStrategy
    event_loop_metrics: EventLoopMetrics
    plugins: list[Plugin]
    trace_span: Span
    trace_attributes: dict[str, AttributeValue]
    interrupt_state: InterruptState
    invocation_lock: Lock
    concurrent_invocation_mode: THROW | UNSAFE_REENTRANT

    __call__(prompt) -> AgentResult
    invoke_async(prompt) -> AgentResult
    stream_async(prompt) -> AsyncIterator[TypedEvent]
    structured_output(prompt, model) -> T
    tool -> ToolCaller
    tool_names -> list[str]
    cleanup()
}
```

### AgentResult
```
AgentResult {
    stop_reason: StopReason
    message: Message
    metrics: EventLoopMetrics
    state: Any
    interrupts: list[Interrupt] | None
    structured_output: BaseModel | None

    to_dict() -> dict
    from_dict(dict) -> AgentResult
}
```

### AgentState
```
AgentState = dict[str, Any]  // persistent key-value store
```

## Message Types

### Message
```
Message {
    role: "user" | "assistant"
    content: list[ContentBlock]
}
```

### ContentBlock (union)
```
ContentBlock =
    | {"text": str}
    | {"toolUse": ToolUse}
    | {"toolResult": ToolResult}
    | {"image": ImageBlock}
    | {"document": DocumentBlock}
    | {"guardContent": GuardContentBlock}
    | {"cachePoint": CachePointBlock}
```

### SystemContentBlock
```
SystemContentBlock =
    | {"text": str}
    | {"guardContent": GuardContentBlock}
    | {"cachePoint": CachePointBlock}
```

## Tool Types

### ToolSpec
```
ToolSpec {
    name: str           // ^[a-zA-Z0-9_-]{1,64}$
    description: str
    inputSchema: {
        json: JSONSchema
    }
}
```

### ToolUse
```
ToolUse {
    toolUseId: str
    name: str
    input: dict[str, Any]
}
```

### ToolResult
```
ToolResult {
    toolUseId: str
    status: "success" | "error"
    content: list[ToolResultContent]
}
```

### ToolResultContent (union)
```
ToolResultContent =
    | {"text": str}
    | {"image": ImageBlock}
    | {"json": dict}
    | {"document": DocumentBlock}
```

### ToolChoice (union)
```
ToolChoice =
    | {"auto": ToolChoiceAuto{}}
    | {"any": ToolChoiceAny{}}
    | {"tool": ToolChoiceTool{name: str}}
```

### AgentTool (interface)
```
AgentTool {
    tool_name: str
    tool_spec: ToolSpec
    stream(tool_use: ToolUse, state: dict) -> AsyncGenerator[event]
}
```

## Model Types

### Model (interface)
```
Model {
    update_config(**kwargs)
    get_config() -> Any
    stream(messages, tool_specs, system_prompt, ...) -> AsyncIterable[StreamEvent]
    structured_output(output_model, prompt, system_prompt) -> AsyncGenerator
}
```

### StreamEvent
```
StreamEvent = dict with eventual "stop" key:
    stop: (StopReason, Message, Usage, Metrics)
```

### StopReason
```
StopReason = "end_turn" | "tool_use" | "max_tokens" | "interrupt"
```

### Usage
```
Usage {
    inputTokens: int
    outputTokens: int
    totalTokens: int
    cacheReadInputTokens?: int
    cacheWriteInputTokens?: int
}
```

### Metrics
```
Metrics {
    latencyMs: int
}
```

### CacheConfig
```
CacheConfig {
    strategy: "auto"
}
```

## Hook Types

### HookEvent (base)
```
HookEvent {
    agent: Agent
}
```

### Event Types
```
AgentInitializedEvent {}
BeforeInvocationEvent { invocation_state, messages (writable) }
AfterInvocationEvent { invocation_state, result }  // reverse order
MessageAddedEvent { message }
BeforeModelCallEvent { invocation_state }
AfterModelCallEvent { invocation_state, stop_response?, exception?, retry (writable) }  // reverse
BeforeToolCallEvent { selected_tool (writable), tool_use (writable), invocation_state, cancel_tool (writable) }
AfterToolCallEvent { selected_tool, tool_use, invocation_state, result (writable), exception?, retry (writable) }  // reverse
```

### Multi-Agent Events (base: BaseHookEvent, no agent field)
```
MultiAgentInitializedEvent { source: MultiAgentBase, invocation_state }
BeforeMultiAgentInvocationEvent { source, invocation_state }
AfterMultiAgentInvocationEvent { source, invocation_state }  // reverse
BeforeNodeCallEvent { source, node_id, invocation_state, cancel_node (writable) }
AfterNodeCallEvent { source, node_id, invocation_state }  // reverse
```

## Multi-Agent Types

### MultiAgentBase (interface)
```
MultiAgentBase {
    id: str
    invoke_async(task, invocation_state) -> MultiAgentResult
    stream_async(task, invocation_state) -> AsyncIterator[dict]
    __call__(task, invocation_state) -> MultiAgentResult  // sync wrapper
    serialize_state() -> dict
    deserialize_state(dict)
}
```

### MultiAgentResult
```
MultiAgentResult {
    status: Status
    results: dict[str, NodeResult]
    accumulated_usage: Usage
    accumulated_metrics: Metrics
    execution_count: int
    execution_time: int
    interrupts: list[Interrupt]
}
```

### NodeResult
```
NodeResult {
    result: AgentResult | MultiAgentResult | Exception
    execution_time: int
    status: Status
    accumulated_usage: Usage
    accumulated_metrics: Metrics
    execution_count: int
    interrupts: list[Interrupt]
}
```

### Status (enum)
```
Status = PENDING | EXECUTING | COMPLETED | FAILED | INTERRUPTED
```

## Session Types

### SessionManager (interface, HookProvider)
```
SessionManager {
    initialize(agent)
    append_message(message, agent)
    sync_agent(agent)
    redact_latest_message(message, agent)
    initialize_multi_agent(source)
    sync_multi_agent(source)
}
```

### ConversationManager (interface)
```
ConversationManager {
    reduce_context(agent)
}
```

## Interrupt Types

### Interrupt
```
Interrupt {
    id: str
    name: str
    data: Any
}
```

### InterruptState (internal)
```
InterruptState {
    activated: bool
    context: dict  // tool_use_message, tool_results
    activate()
    deactivate()
}
```

## Telemetry Types

### EventLoopMetrics
```
EventLoopMetrics {
    start_cycle(attributes) -> (start_time, Trace)
    end_cycle(start_time, trace, attributes)
    update_usage(usage)
    update_metrics(metrics)
    add_tool_usage(tool_use, duration, trace, success, message)
}
```

### Trace (internal)
```
Trace {
    id: str
    name: str
    parent_id: str | None
    children: list[Trace]
    start_time: float
    end_time: float
    add_child(trace)
    add_message(message)
    end()
}
```
