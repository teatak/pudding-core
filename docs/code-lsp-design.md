# Pudding LSP 语言智能设计

> 状态:已完成(2026-07-10)。
> 对应阶段:C10。  
> 首版语言:Go、TypeScript / JavaScript。  
> 目标:在不引入后端 focus、隐式上下文或完整 IDE 状态的前提下,为 Project mode
> 提供结构化的符号、定义、引用和诊断能力。

## 1. 结论

Pudding 不自行实现语言解析器,而是复用成熟 Language Server Protocol 服务。

首版架构:

```text
LLM tool call
  -> engine 显式补 sessionID / ProjectDirs
  -> internal/tool code renderer contract
  -> internal/lsp Manager
  -> project-scoped language server over stdio JSON-RPC
  -> bounded structured tool_result
  -> canonical messages.parts
  -> transcript / Canvas 文件定位
```

关键决策:

1. LSP process 由 daemon 管理,按 `languageRoot + serverKind` 复用。
2. LSP process 不属于 session,也不形成 daemon 级 current Project / focus。
3. 所有 LSP tool call 都显式携带目标路径,并受当前 Project roots / turn grant 约束。
4. daemon 与 language server 使用 stdio JSON-RPC;前端不直接连接 LSP。
5. LSP 输出只通过正常 tool result 进入 canonical context。
6. 首版只做只读能力,不做 rename、code action 自动应用和格式化写入。
7. 只检测已有 language server,不自动下载、安装或联网修复依赖。

## 2. 产品范围

### 2.1 首版能力

- workspace symbol search
- go to definition
- find references
- file diagnostics
- 结构化位置列表
- 点击位置后在 Canvas 文件 tab 定位
- Project 内路径过滤
- request cancel、timeout、server crash recovery

### 2.2 非目标

- 不做完整编辑器。
- 不保存未落盘 editor buffer。
- 不做 completion / signature help。
- 不做 rename / code action / organize imports。
- 不允许模型传入任意 language server executable 或启动参数。
- 不自动安装 `gopls`、`typescript-language-server` 或 npm package。
- 不把 language server diagnostics 持久化到 SQLite。
- 不新增无 session scope 的 `/lsp/*` 业务 API。

## 3. 资源归属

### 3.1 Daemon-managed, language-root keyed

Language server 是 daemon 管理的临时进程资源:

```text
LSPProcessKey = canonicalLanguageRoot + serverKind
```

例如:

```text
/repo + gopls
/repo/web + typescript-language-server
```

多个 session 绑定同一个 Project,或多个 Project root 最终解析到同一个 language
root 时,可复用同一个进程。复用只减少启动成本,不会产生 current session 状态。

### 3.2 Session 边界

Session 只拥有当前 tool call:

- `sessionID`
- `turnID`
- `callID`
- target path / position
- 当前 Project roots 与 turn-scoped grant roots

Session 删除时不立即杀死共享 LSP process。进程按 idle timeout 回收;daemon 关闭时
统一退出。

### 3.3 不持久化运行态

以下状态只存在内存:

- language server process
- JSON-RPC request sequence
- initialize capabilities
- open document hash / version
- 最新 publishDiagnostics cache
- stderr bounded ring buffer
- crash / restart counters

SQLite 不保存 LSP process、document version 或 diagnostics。

## 4. 组件边界

建议新增:

```text
internal/lsp/
  manager.go          # process registry, idle lifecycle
  process.go          # spawn, initialize, request routing
  jsonrpc.go          # Content-Length framing
  documents.go        # didOpen / didChange / didClose
  diagnostics.go      # push/pull diagnostics cache
  resolver.go         # language root and executable resolution
  protocol.go         # bounded LSP payload types
  process_unix.go     # process group termination
  process_windows.go
```

`internal/tool` 只依赖一个窄接口:

```go
type LanguageService interface {
    Symbols(ctx context.Context, scope Scope, in SymbolInput) (SymbolResult, error)
    Definition(ctx context.Context, scope Scope, in PositionInput) (LocationResult, error)
    References(ctx context.Context, scope Scope, in ReferenceInput) (LocationResult, error)
    Diagnostics(ctx context.Context, scope Scope, in DiagnosticInput) (DiagnosticResult, error)
}
```

`BuiltinRunner` 通过 `WithLanguageService(...)` 注入服务。daemon 创建并关闭 Manager;
tool package 不拥有进程生命周期。

## 5. Language Root 解析

Project roots 仍是授权事实源。language root 是一次 tool call 内从 target path 派生的
运行目录,不是新 Project 实体。

### 5.1 Go

从 target directory 向授权 Project root 查找:

1. 最近的 `go.work`。
2. 若无,最近的 `go.mod`。
3. 若仍无,使用授权 Project root,但返回 `rootFallback:true`。

language server:

```text
executable: gopls from PATH
args:       serve
transport:  stdio
```

### 5.2 TypeScript / JavaScript

从 target directory 向授权 Project root 查找:

1. 最近的 `tsconfig.json`。
2. 最近的 `jsconfig.json`。
3. 最近的 `package.json`。
4. 若仍无,使用授权 Project root,并返回 `rootFallback:true`。

language server 解析顺序:

1. `<languageRoot>/node_modules/.bin/typescript-language-server`。
2. 授权 Project root 下最近 package 的本地 binary。
3. PATH 中的 `typescript-language-server`。

固定参数:

```text
--stdio
```

不接受 tool input 覆盖 executable 或 args。

### 5.3 多 root Project

target path 必须先由现有 Project resolver 唯一解析到一个授权 root。language root
不得越过该授权 root。不同 root 的结果不能在一个 language server process 中隐式
合并。

## 6. Process Manager

### 6.1 生命周期

建议默认值:

| 项目 | 默认值 |
| --- | --- |
| initialize timeout | 15 秒 |
| 普通 request timeout | 20 秒 |
| diagnostics timeout | 30 秒 |
| idle timeout | 10 分钟 |
| daemon 最大 LSP process | 6 |
| 单条 JSON-RPC message | 8 MiB |
| stderr ring buffer | 64 KiB |
| 单次 crash 自动重启 | 最多 1 次 |

启动使用 singleflight:同一个 key 的并发首请求只能创建一个 process。

### 6.2 JSON-RPC framing

使用标准 `Content-Length` header framing,禁止用 `bufio.Scanner` 按行解析 payload。

读取约束:

- header 总长有上限。
- `Content-Length` 必须为正整数且不超过 8 MiB。
- JSON 必须是 object。
- 未知 response id 记录 debug 日志后丢弃。
- server request / notification 使用 allowlist 处理。

首版 client 需要响应:

- `workspace/configuration`
- `client/registerCapability`
- `window/workDoneProgress/create`
- `workspace/workspaceFolders`
- `window/showMessageRequest` 使用空响应,不弹隐式 UI

首版 notification 至少处理:

- `textDocument/publishDiagnostics`
- `window/logMessage`
- `$/progress`

### 6.3 Initialize

initialize 参数必须显式设置:

- daemon process id
- `rootUri`
- 单个 `workspaceFolders`
- client name/version
- UTF-16 position encoding 优先,若 server 协商其他 encoding 则记录
- workspace symbol / definition / references / diagnostics capabilities
- 不声明未实现的 applyEdit、codeAction 或 dynamic workspace editing

收到 initialize response 后发送 `initialized`。只有完成后才能接业务请求。

### 6.4 Cancel

Session cancel 只取消本次 JSON-RPC request:

```text
context cancelled
  -> send $/cancelRequest with request id
  -> return cancelled tool result
```

不能因为一个 session cancel 就杀掉共享 process。只有以下情况重启 process:

- stdio EOF
- framing / protocol 失效
- process exit
- initialize 失败
- request 写入失败

server 在 cancel 后仍返回 response 时,按已完成 request id 丢弃。

## 7. Document 同步

Pudding 首版没有 editor buffer,磁盘文件是唯一文本事实源。

Manager 为每个已打开文档记录:

```text
canonical path
language id
content hash
LSP version
last used time
```

每次 position-based tool call 前:

1. 校验 target 仍在授权 Project root。
2. 读取有界 UTF-8 regular file。
3. 计算内容 hash。
4. 未打开则发送 `textDocument/didOpen`。
5. hash 变化则 version + 1,发送 full-content `didChange`。
6. 请求结束后保留短期 open 状态;文档 idle 时发送 `didClose`。

选择 full-content sync,原因是 Pudding 没有可靠的增量 editor edit stream。依赖文件监听
只能作为 language server 自身优化,不能替代请求前 hash 同步。

文件上限建议 2 MiB。超限返回 `document_too_large`,提示回退到 file search/slice。

## 8. Tool 协议

所有工具:

- `scope` 固定为 `project`。
- `language` 可选值为 `go | typescript`;文件路径通常可从扩展名推断。
- directory target 同时包含多种受支持语言时返回 `language_ambiguous`,要求显式传
  `language`,不能任意选择一个 server。
- target 必须位于当前 `ProjectDirs`。
- line / column 对模型使用 1-based。
- 内部 LSP position 根据协商 encoding 转换为 0-based。
- 结果路径统一返回 canonical absolute `path` 和 Project-relative `relativePath`。
- 只返回 `file://` URI。
- Project 外 location 不返回路径,只累计 `externalResultCount`。

### 8.1 `builtin_code_symbols`

```jsonc
{
  "scope": "project",
  "path": ".",
  "language": "go",
  "query": "ProjectInspect",
  "max_results": 100
}
```

结果:

```jsonc
{
  "ok": true,
  "server": "gopls",
  "languageRoot": "/repo",
  "query": "ProjectInspect",
  "symbols": [
    {
      "name": "projectInspect",
      "kind": "function",
      "containerName": "tool",
      "path": "/repo/internal/tool/project_inspect.go",
      "relativePath": "internal/tool/project_inspect.go",
      "line": 54,
      "column": 1,
      "endLine": 102,
      "endColumn": 2,
      "excerpt": "func (r *BuiltinRunner) projectInspect(...)"
    }
  ],
  "resultCount": 1,
  "externalResultCount": 0,
  "truncated": false
}
```

约束:

- query 必须非空。
- `path` 是目录或没有可识别扩展名时,混合语言 Project 应显式传 `language`。
- `max_results` 默认 100,上限 200。
- workspace/symbol 返回后按 Project scope 二次过滤。

### 8.2 `builtin_code_definition`

```jsonc
{
  "scope": "project",
  "path": "internal/tool/builtin.go",
  "line": 557,
  "column": 12
}
```

返回最多 20 个 location。若定义只存在于标准库或外部 module,返回:

```jsonc
{
  "locations": [],
  "externalResultCount": 1,
  "hint": "Definition is outside authorized Project roots."
}
```

不自动扩大 Project 授权范围。

### 8.3 `builtin_code_references`

```jsonc
{
  "scope": "project",
  "path": "internal/tool/builtin.go",
  "line": 557,
  "column": 12,
  "include_declaration": true,
  "max_results": 200
}
```

约束:

- 默认包含 declaration。
- 默认最多 100,上限 500。
- 结果按 `relativePath + line + column` 排序和去重。

### 8.4 `builtin_code_diagnostics`

```jsonc
{
  "scope": "project",
  "paths": ["internal/tool/patch.go"],
  "severity": ["error", "warning"]
}
```

结果:

```jsonc
{
  "ok": true,
  "server": "gopls",
  "diagnostics": [
    {
      "path": "/repo/internal/tool/patch.go",
      "relativePath": "internal/tool/patch.go",
      "line": 42,
      "column": 5,
      "endLine": 42,
      "endColumn": 10,
      "severity": "error",
      "code": "UndeclaredName",
      "source": "compiler",
      "message": "undefined: value",
      "excerpt": "..."
    }
  ],
  "diagnosticCount": 1,
  "fresh": true,
  "truncated": false
}
```

约束:

- 单次最多 32 个 path。
- 单次调用的所有 path 必须解析到同一个 `serverKind + languageRoot`;混合语言或跨
  language root 返回 `mixed_language_targets`,由 agent 拆成多次调用。
- 最多返回 500 条 diagnostics。
- 优先使用 server pull diagnostics capability。
- 对只支持 publishDiagnostics 的 server,在 didOpen/didChange 后等待一个短窗口,
  返回最新 generation,并用 `fresh` 表示是否观察到本次同步后的结果。

### 8.5 Common location

location excerpt 最多 5 行、每行最多 400 字符。完整文件内容不进入 tool result。

结果排序必须稳定:

```text
relativePath ASC
line ASC
column ASC
```

## 9. Diagnostics 一致性

LSP diagnostics 与 C8 command diagnostics 是两个来源:

| 来源 | 时机 | 优点 | 限制 |
| --- | --- | --- | --- |
| LSP | 编辑前后按需查询 | 快、位置结构化 | 依赖 server 可用性与索引 freshness |
| command | test/lint/build 完成后 | 接近真实 CI | 启动较慢、输出格式依赖工具 |

两者不互相覆盖。UI 可以使用同一 diagnostic row 组件,但 result 中必须保留
`sourceKind:"lsp" | "command"`。

Agent 汇报验证时:

- LSP diagnostics 只能描述静态诊断结果。
- 只有实际 command result 才能声称 test/build/lint 通过。

## 10. 安全与审批

Language server 虽然服务只读工具,仍是本地进程,可能扫描大量文件或调用编译工具。

首版约束:

- executable 来自固定 allowlist resolver。
- tool input 不能传 executable、args、environment。
- cwd 必须是解析后的 authorized language root。
- 使用 command runner 同等级的最小环境。
- Go 默认设置 `GOPROXY=off`、`GOTOOLCHAIN=local`,禁止隐式下载 module 或 toolchain。
- 不继承项目 `.env`。
- 不执行 server 建议的 arbitrary command / applyEdit。
- `workspace/applyEdit` 一律拒绝。
- server 返回的 Project 外 URI 被过滤。

审批分类:

| 模式 | LSP read tool |
| --- | --- |
| `ask` | 按 Project read 操作询问 |
| `auto` | allowlisted server + offline env 自动允许 |
| `full` | 自动允许,仍保留路径和 executable allowlist |

Project approval 是产品信任边界,不是 OS process sandbox。文档和 UI 不应声称 LSP
进程被操作系统完全隔离。

## 11. Server 可用性

未检测到 server 时返回结构化错误:

```jsonc
{
  "ok": false,
  "reason": "language_server_unavailable",
  "language": "go",
  "server": "gopls",
  "checked": ["PATH:gopls"],
  "hint": "Install or configure gopls, then retry. Pudding did not install it automatically."
}
```

其他稳定 reason:

- `language_not_supported`
- `language_ambiguous`
- `mixed_language_targets`
- `language_root_not_found`
- `language_server_start_failed`
- `language_server_initialize_failed`
- `language_server_crashed`
- `language_server_timeout`
- `language_server_capacity`
- `language_server_protocol_error`
- `document_too_large`
- `invalid_position`
- `path_not_authorized`
- `cancelled`

stderr 只用于结构化错误 detail 和 daemon debug log;不得把无限 stderr 放进 canonical
tool result。

## 12. Transcript 与 Canvas

新增 code tool renderer:

- symbols:名称、kind、container、文件位置。
- definition:定义位置列表。
- references:引用位置列表和截断状态。
- diagnostics:severity、文件、行列、code、message。

交互:

- 每个 Project 内 location 都可点击。
- 点击后复用现有 Canvas 文件 tab,使用 excerpt 和真实 lineStart 定位。
- Project 外结果只显示省略数量,不显示未授权绝对路径。
- copy 图标延续 hover-only 规则。
- 原始 JSON 延续独立“原始数据”card 和用户显示开关。
- 新工具必须补三语显示名与图标,折叠行不得暴露 snake_case。

不新增独立 IDE 面板。首版继续复用 transcript + Canvas。

## 13. API 与事件

首版不新增 REST / SSE / WebSocket contract:

- request 通过现有 model tool loop。
- running / ok / error 使用现有 `turn.tool`。
- canonical message parts 保存有界 tool use / result。
- session cancel 通过现有 turn context 传播到 LSP request。

Language server progress 不直接流到前端。若首次索引时间过长,tool 保持 running,超时后
返回结构化错误。后续是否增加 live-only `turn.tool_progress` 单独设计。

## 14. 并发与恢复

### 14.1 并发请求

- process request id 单调递增。
- pending map 由 process mutex 保护。
- stdio write 串行化。
- response 可乱序完成。
- diagnostics notification 独立更新 cache。

### 14.2 Crash recovery

同一 tool call 在以下条件可透明重试一次:

- server 在发送业务 request 前退出。
- initialize 后首 request 因 pipe closed 失败。

已发送并可能执行的 request 不透明重试,避免重复副作用。虽然首版工具只读,仍保持
通用 JSON-RPC 纪律。

### 14.3 Idle eviction

超过最大 process 数时,只驱逐:

- 无 pending request
- 无 initialize
- 最久未使用

驱逐顺序:

```text
shutdown request -> exit notification -> grace period -> terminate process group
```

## 15. 测试策略

### 15.1 Protocol 单测

使用内置 fake LSP subprocess,CI 不依赖真实 `gopls` 或 Node package:

- Content-Length partial read/write
- invalid / oversized frame
- initialize singleflight
- out-of-order response
- server request response
- publishDiagnostics cache
- request timeout / cancel
- crash / restart once
- graceful shutdown / forced kill

### 15.2 Resolver 单测

- Go `go.work` / `go.mod` precedence
- TS `tsconfig` / `jsconfig` / `package.json` precedence
- nested monorepo roots
- multi-root Project
- symlink escape
- missing executable
- project-local TypeScript binary precedence

### 15.3 Tool 单测

- 1-based / 0-based position conversion
- UTF-16 column conversion
- location sorting / dedupe / cap
- external URI filtering
- excerpt bounds
- diagnostics severity mapping
- canonical result size bounds
- unavailable / timeout / cancel errors

### 15.4 集成测试

可选环境变量开启真实 server 测试:

```text
PUDDING_LSP_INTEGRATION=1
```

覆盖:

- temporary Go module + gopls
- temporary TypeScript project + typescript-language-server
- edit file -> didChange -> definition/diagnostics refresh

默认 CI 仍只跑 fake server,避免网络和全局工具依赖。

### 15.5 前端

- symbol / location / diagnostic renderer 组件测试。
- 点击位置打开正确 session 的 Canvas 文件 tab。
- 390px 视口无横向溢出。
- 大结果稳定高度滚动。

## 16. 实施切片

### C10.0: Protocol 与 Manager

状态:已完成(2026-07-10)。

- `internal/lsp` JSON-RPC framing。
- process lifecycle、initialize、pending requests、cancel、idle close。
- fake LSP subprocess tests。
- daemon wiring 与 `WithLanguageService`。

验收:fake server 下并发、cancel、crash、shutdown 全部通过。

实际落地:

- `internal/lsp` 提供有界 `Content-Length` framing、initialize、乱序 response
  routing、server request allowlist 与 diagnostics cache。
- Manager 按 canonical `languageRoot + serverKind` singleflight 复用进程,支持最大
  process 数、LRU / idle 回收、crash 后重建和 spec conflict 检查。
- request context 取消只发送 `$/cancelRequest`,不终止共享进程。
- daemon 持有 Manager 生命周期,`BuiltinRunner` 通过 `WithLanguageService` 注入协议
  边界;C10.1 在此边界上增加 Go resolver 与用户可见工具。
- fake subprocess 覆盖 partial frame、并发乱序响应、server request、diagnostics、
  cancel 后复用、crash restart、graceful shutdown 与 forced kill。

### C10.1: Go / gopls

状态:已完成(2026-07-10)。

- Go language root resolver。
- gopls allowlist resolver 与 offline environment。
- symbols / definition / references / diagnostics tools。
- Project path filtering、excerpt、结构化错误。

验收:已有 gopls 环境能完成 Go 定义、引用和诊断;无 gopls 时返回明确 unavailable。

实际落地:

- 注册统一的 symbols、definition、references、diagnostics 四个 Code Tool;语言差异
  保留在内部 adapter,没有新增 Go 专属工具名。
- Go resolver 按 `go.work`、最近 `go.mod`、Project root fallback 的顺序解析
  language root,并固定从 PATH 检测 `gopls`。
- gopls 使用最小环境以及 `GOPROXY=off`、`GOTOOLCHAIN=local`,不自动安装或下载。
- 磁盘文档按 hash 与 version 发送 `didOpen` / full-content `didChange`,打开文档数量
  有界;位置支持 UTF-8 / UTF-16 / UTF-32 转换。
- 定义、引用、符号和诊断路径再次经过当前 Project roots 校验;Project 外结果只计数,
  不返回路径。
- fake service 单测覆盖 root、编码、过滤、去重和 publish diagnostics fallback;
  `PUDDING_LSP_INTEGRATION=1` 可使用真实 gopls 验证四个工具。
- transcript 已补统一工具显示名、图标、基础结构化位置/诊断列表和三语 i18n;
  C10.3 已完成视觉与可靠性收口。

### C10.2: TypeScript / JavaScript

状态:已完成(2026-07-10)。

- TS language root resolver。
- project-local / PATH server resolution。
- TypeScript UTF-16 position 与 diagnostics 适配。
- 与 Go 共用 tool contracts。

验收:已有 TypeScript server 环境能完成 TS 定义、引用和诊断。

实际落地:

- 四个 Code Tool 的 `language` 扩展为 `go | typescript`;JavaScript 与 JSX 统一使用
  TypeScript adapter,没有新增语言专属工具。
- 支持 `.ts`、`.tsx`、`.js`、`.jsx`、`.mts`、`.cts`、`.mjs`、`.cjs`,并发送
  `typescript`、`typescriptreact`、`javascript`、`javascriptreact` document ID。
- language root 按 `tsconfig.json`、`jsconfig.json`、`package.json`、Project root
  fallback 解析;目录同时命中等距 Go/TypeScript root 时返回 `language_ambiguous`。
- server resolver 从 language root 向 Project root 查找最近的
  `node_modules/.bin/typescript-language-server`,最后回退 PATH;不安装依赖。
- TypeScript/JavaScript 复用 UTF-16 转换、Project 二次过滤、diagnostics pull/publish
  fallback、稳定错误和统一 transcript renderer。
- fake service 测试覆盖 monorepo root、binary 优先级、TSX document ID、统一工具调用和
  unavailable;`PUDDING_LSP_TS_INTEGRATION=1` 可在已有 server 环境运行真实集成测试。

### C10.3: UI 与可靠性

状态:已完成(2026-07-10)。

- transcript code renderers 与三语 i18n。
- Canvas location jump。
- diagnostics common row。
- 全量 Go/Web 测试与文档收口。

实际落地:

- Manager 为普通 request 提供 20 秒默认超时,diagnostics request 使用 30 秒;用户
  cancel 仍只发送 `$/cancelRequest`,不会杀死共享 process。
- process 在业务 request 尚未发送前已退出时,Manager 丢弃旧实例并安全重建一次;
  已发送的 request 不透明重试。
- symbols、definition、references 与 diagnostics 的完整 JSON 结果统一限制为 256 KiB;
  长 symbol metadata 与 diagnostic message 先做单项截断,再按稳定前缀缩减结果并标记
  `truncated:true`。
- 修复 server 返回超出磁盘文件行数的位置时 excerpt 计算越界的问题;异常 location 仍
  可结构化返回,但不生成伪造 excerpt。
- transcript 展示语言、server、root fallback、freshness 与稳定本地化错误;技术 detail
  继续只在独立“原始数据”card 中可见。
- location 与 diagnostic row 使用统一 hover-only Canvas 入口;点击后复用已有文件 tab,
  并在 excerpt 中高亮目标行。长列表保持固定最大高度滚动,不静默丢弃 200 项后的结果。
- fake LSP race tests 覆盖默认 request timeout 后复用、process exit 后重建、结果大小上限
  与越界 location;真实 gopls 集成继续由 `PUDDING_LSP_INTEGRATION=1` 验证。

C10 至此完成。

## 17. 验收标准

- 没有 backend focus 或 current Project。
- 两个 session 可并发调用同一 language root,并安全复用 process。
- cancel 一个 session 的 request 不影响另一个 session。
- 所有返回路径都重新经过当前 Project roots 校验。
- Project 外定义和引用不泄露绝对路径。
- LSP 结果有界并作为正常 canonical tool result 保存。
- server 缺失、崩溃、超时都有稳定结构化错误。
- Go 和 TypeScript 使用同一组用户可见 tool names 与 renderer contract。
- 首版没有 rename、applyEdit 或隐式文件写入。
- 默认流程不触发网络安装或依赖下载。
- `go test ./...`、Web build 与 `git diff --check` 通过。

## 18. 后续方向

完成 C10 后再评估:

- 是否随 desktop bundle 分发 language server。
- hover / document symbols / call hierarchy。
- rename proposal:必须转成 Patch Proposal,不能由 LSP 直接写盘。
- code action proposal:只接受 WorkspaceEdit,转换为可审阅 diff。
- LSP progress live event。
- Project 级 language server 显式配置。

rename 与 code action 即使后续实现,也必须走:

```text
LSP WorkspaceEdit
  -> validate Project scope
  -> Patch Proposal
  -> user review / approval
  -> atomic apply
```

禁止 language server 通过 `workspace/applyEdit` 直接修改工作树。
