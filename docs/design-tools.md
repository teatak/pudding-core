# 工具调用 / MCP 契约设计 v1

> 状态:草案,T1 provider 协议扩展已落地。范围:工具调用全链路(provider 流 → engine 循环 →
> canonical 落库 → 事件协议 → 前端任务流)与 MCP 接入。
> 纪律延续:provider 只产模型流(AGENTS.md 硬约束 17),工具执行、turn
> 生命周期、落库全部归 engine;context 只来自 canonical messages(硬约束 8)。

## 0. 模型:turn 内多步循环

一个 turn 不再是"一次模型调用",而是一个循环:

```text
user 消息落库
  └─ 循环(≤ maxSteps,默认 16):
       1. contextbuilder 组装(canonical parts → provider wire 形状)
       2. provider.Stream:产出 text/thought 增量 与 tool_use 块
       3. finish = stop        → 收尾,assistant 消息落库,turn.completed
          finish = tool_calls  → engine 执行工具 → tool_result 追加进
                                 当前 turn 的 parts → 回到 1
```

cancel 在循环任意点生效(turnCtx);崩溃恢复语义不变(running → failed,
未落库的中间 parts 随进程丢失)。

## 1. canonical 形状:一 turn 一条 assistant 消息,ContentPart 序列

借鉴旧项目的**语义 ContentPart 模型**(其最有价值处),但不照搬它的
`session_messages` 宽表与 eventHub 反推 canonical 的链路。落地更干净:

- `messages.parts` 是**一等 JSON 结构**;`text` 列保留为派生纯文本(标题、
  列表预览、text-only provider 兼容)。
- `role` ∈ `user | assistant | tool | summary`。
- **一个 assistant turn 最终只落一条 canonical assistant 消息**;tool
  call / result 是它的 **parts**,不是宽表列、也不是多行。
- compaction 产物是 `role: summary` 消息;`tool` 角色见下。
- **event log 只做旁路审计**,不反推 canonical(硬约束 8:context 只来自
  messages 表)。

```jsonc
// role=assistant 消息的 parts:落库的 turn 过程,顺序即时间序。
[
  { "type": "thought", "text": "推理摘要..." },                  // 落库,供历史回看
  { "type": "tool_use", "id": "call_1", "name": "web_fetch",
    "args": { "url": "..." } },
  { "type": "tool_result", "id": "call_1", "ok": true,
    "content": "..." },                                          // 截断存储,上限 ~32KB/条
  { "type": "text", "text": "最终回答..." }
]
```

**`tool` 角色的定位**:canonical 里 assistant turn 仍是单条 role=assistant
(tool_use + tool_result 都作 parts)。`role: tool` 不产生独立 canonical 行,
只在 **contextbuilder 翻译到 wire 时按需合成**——OpenAI 要 `role:"tool"`
消息、Anthropic 要 user 消息里的 `tool_result` block,各家从 assistant 消息
的 tool_result parts 现翻。(若后续确实需要独立 tool 行再升级,此处先按
"不落独立行"实现。)

> **thought 落 canonical(供历史回看)**:思考流 streaming 期间经
> `turn.delta(part=thought)` 实时显示,turn 收尾**写进 parts**,刷新/重开
> 会话仍可展开(前端任务流的折叠思考行,design.md 3.2)。但有两条边界:
>
> - **contextbuilder 跨 turn 组装时剥离 thought**:provider 不要陈旧推理
>   (Anthropic 的 thinking 只在同一 turn 工具往返内有意义,跨 user turn
>   重放既费 token 又可能干扰)。即 thought 进 canonical 是给**用户看**的,
>   不进**后续模型上下文**。
> - **turn 内工具循环的 replay 走工作态,不读 canonical**:循环里 tool_use
>   之前的 reasoning,续跑那次请求可能必须原样回传(Anthropic 强制带签名的
>   thinking block)。这用**循环期间的工作 messages**(含 provider 专属
>   签名)完成;canonical 的 thought part 只存 provider-neutral 文本,
>   不承载签名等 wire 细节。

**为什么不按 OpenAI / Anthropic 的多消息交替形状落库**:那是两套互不兼容的
wire 格式;canonical 必须 provider 无关。一 turn 一条消息保持"turn ↔
assistant 消息 1:1"的现有不变量,前端任务流(parts 渲染模型,design.md 3.2)
零转换直读。代价是 contextbuilder 的翻译层变厚——这正是它存在的意义。

user 消息 parts 暂时只有 text(多模态来了再扩)。

> **从旧项目存档、后续阶段再用**(本版不做,记下免得丢):
> - **`source_event_ids` 审计链**:canonical 消息记录它从哪些原始 event
>   蒸馏而来,调试"这条为什么长这样"很有用。
> - **FTS5 trigram tokenizer**(历史搜索阶段):`unicode61` 把整段中文当
>   一个 token,中文子串搜索基本废;`trigram` 才能搜任意位置子串,需
>   `sqlite_fts5` build tag。

## 2. provider 契约扩展(internal/provider)

```go
type Request struct {
    Model    string
    System   string
    Messages []Message
    Tools    []ToolDef        // 新增;空 = text-only,老 provider 行为不变
}

type ToolDef struct {
    Name        string
    Description string
    InputSchema json.RawMessage // JSON Schema
}

type Message struct {
    Role  Role
    Text  string  // 快捷路径:纯文本消息(向后兼容)
    Parts []Part  // 非空时优先;tool_use / tool_result 必须走 Parts
}

type Part struct {
    Type    PartType // text | thought | tool_use | tool_result
    Text    string          // text / thought 文本
    CallID  string          // tool_use / tool_result
    Name    string          // tool_use
    Args    json.RawMessage // tool_use
    Ok      bool            // tool_result
    Content string          // tool_result
}

// Chunk 升级为 part 维度
type Chunk struct {
    Part  PartKind // text | thought | tool(零值 text,老实现不改也兼容)
    Delta string   // text / thought 的增量
    Tool  *ToolCallChunk
    Done  bool
    // Done 时必填:stop | tool_calls(engine 据此决定续跑或收尾)
    Finish FinishReason
    Err    error
}

type ToolCallChunk struct {
    CallID    string
    Name      string // 首帧携带;后续帧可空
    ArgsDelta string // 参数 JSON 增量,engine 累积后整体解析
}
```

三家 wire 映射(实现时各自单测固化帧):

| canonical | openai-compatible | anthropic | google |
| --- | --- | --- | --- |
| ToolDef | `tools[].function` | `tools[]` | `tools[].functionDeclarations` |
| tool_use 流 | `delta.tool_calls[]`(index+id+name+arguments 增量) | `content_block_start(tool_use)` + `input_json_delta` | `functionCall`(整块,无增量) |
| tool_result 回传 | `role:"tool"` 消息 | user 消息 `tool_result` block | `functionResponse` part |
| finish | `finish_reason: tool_calls` | `stop_reason: tool_use` | `functionCall` 出现即视为需调用 |
| thought | reasoning 字段(部分实现) | thinking block(已跳过→改发 Part=thought) | thought part(同左) |

thought 顺带升级:三家现在丢弃的思考流改为 `Part: thought` 发出。
engine streaming 期间透传 `turn.delta(part=thought)`,turn 收尾把累积的
thought 写进 canonical assistant 消息的 `thought` part(第 1 节)。

## 3. 工具执行层(internal/tool)

```go
type Runner interface {
    Definitions(ctx) []provider.ToolDef
    Call(ctx, name string, args json.RawMessage) (content string, ok bool)
}
```

- 聚合 Runner:内置工具 + 各 MCP server 的工具(名字冲突加 `mcp__<server>__` 前缀)。
- 内置第一批只做**只读安全工具**:`web_fetch`(http GET,大小/超时限制)、
  `current_time`。文件/shell 等危险工具等权限确认框架(第 7 节)后置。
- 工具执行超时默认 60s,结果截断 32KB;错误以 ok=false + 错误文本回填
  (LLM 可自行调整),不打断 turn。

## 4. MCP 接入

- 官方 `modelcontextprotocol/go-sdk` 做 client;transport 支持 stdio
  (本地命令)与 streamable HTTP。
- 新表 `mcp_servers`:name(唯一)/ transport(stdio|http)/ command / args /
  url / env(JSON,**只进不出**,同 api_key 纪律)/ enabled / 时间戳。
- daemon 启动与配置变更时建立连接,`tools/list` 聚合进 Runner;
  `tools/call` 透传。连接失败不阻塞 daemon,该 server 工具缺席并在
  API 状态字段中暴露原因。
- API:`/mcp-servers` CRUD + 状态(connected/error);设置弹窗加 MCP tab。

## 5. 事件协议演进(internal/event)

```text
turn.delta      + part 字段("text"|"thought";默认 text,老客户端兼容)
turn.tool       新 kind,落库:{turnID, callID, name, phase, summary?}
                phase: running | ok | error;summary 为结果摘要(≤200 字)
```

- `turn.delta` 仍不落库;`turn.tool` 落库(回放时能还原工具时间线,
  delta 丢了由 completed 后 refetch parts 兜底)。
- web 契约镜像同步;overlayStore 从单 text 串升级为 parts 数组
  (text 增量追加到最后一个 text part;tool 事件插入/更新 tool part)。

## 6. 前端任务流

- TurnParts switch 补 `thought`(折叠行,streaming 时展开)与 `tool`
  (卡片:图标 + name + 状态徽标 + 摘要;点击展开 args / result)。
- 设置弹窗加 MCP tab(列表 + 新建/编辑表单 + 连接状态)。

## 7. 权限确认(后置框架,本版不实现)

危险工具(文件写、shell)需要 per-call 确认:engine 暂停循环发
`turn.tool` phase=awaiting,前端弹确认,新 API `POST /turns/{id}/tool-decision`。
本版只预留 phase 枚举位,不实现。

## 8. 切片

| 切片 | 内容 | 依赖 |
| --- | --- | --- |
| T1 provider | **已完成**:Chunk/Request 扩展 + 四家 tool/thought 流解析(固化帧单测) | — |
| T2 store | messages.parts 列 + contextbuilder 双向翻译 | T1 形状 |
| T3 engine | 工具循环 + turn.tool 事件 + 内置 Runner(web_fetch) | T1 T2 |
| T4 web | overlay parts 化 + thought 折叠行 + tool 卡片 | T3 |
| T5 MCP | go-sdk client + mcp_servers 表 + /mcp-servers API + 设置 tab | T3 |
| T6 权限 | 确认流(awaiting + decision API + 前端弹层) | T3,后置 |

T1–T4 是最小垂直切片(内置 web_fetch 即可端到端演示);T5 紧随。
