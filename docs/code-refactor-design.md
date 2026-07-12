# Pudding 安全语义重构设计

> 阶段:C11
> 状态:已完成(2026-07-10),总进度 100%
> 首版范围:Go 与 TypeScript / JavaScript 的语义 rename proposal

## 1. 目标

C10 已让 agent 能通过统一 LSP 工具搜索符号、查找定义和引用、读取诊断。C11
进一步提供语义重命名,但不允许 language server 或新工具直接修改工作树。

首版增加一个统一工具:

```text
builtin_code_rename
```

它接收源文件位置与新名称,向对应 language server 请求 `WorkspaceEdit`,验证并转换为
现有 Patch Proposal。模型随后只能通过已有 `builtin_patch_apply` 请求用户审批并应用。

## 2. 非目标

- 不直接执行 LSP `workspace/applyEdit`。
- 不做文件或目录 rename resource operation。
- 不做 code action、organize imports、格式化或 completion。
- 不新增 Go / TypeScript 专属用户工具。
- 不让模型指定 language server executable、参数或环境变量。
- 不新增无 session scope API、backend focus 或隐式 context source。
- 不把 proposal 或 language server 状态写入 SQLite。

## 3. 主流程

```text
builtin_code_rename(path, line, column, new_name)
  -> resolve authorized Project path and language root
  -> sync target document from current disk content
  -> textDocument/prepareRename
  -> textDocument/rename
  -> parse WorkspaceEdit
  -> reject unsafe or partial edit sets
  -> apply TextEdits to in-memory snapshots
  -> existing Patch Proposal creation
  -> canonical tool result with proposalID + diff

builtin_patch_apply(proposal_id)
  -> existing project approval policy
  -> show proposal diff
  -> revalidate source hashes
  -> atomic apply / rollback
```

`builtin_code_rename` 本身不写项目文件。它创建 session-scoped、两小时有效的内存
proposal,与 `builtin_patch_propose` 使用同一存储、diff、漂移检测和事务应用逻辑。

## 4. 工具契约

输入:

```jsonc
{
  "scope": "project",
  "path": "internal/tool/code.go",
  "line": 103,
  "column": 18,
  "new_name": "boundedSemanticResults",
  "language": "go"
}
```

规则:

- `path`、`line`、`column` 与 C10 position tools 使用同一套 1-based 语义。
- `language` 可省略;只允许 `go` 或 `typescript`。
- `new_name` 必填,去除首尾空白后不得为空、包含控制字符或超过 256 个 Unicode 字符。
- 标识符语法由 language server 判断,后端不复制语言专属 parser。
- 工具必须带 `sessionID`,因为 proposal 不能跨 session 使用。

成功输出复用 Patch Proposal 字段,并增加 rename 元信息:

```jsonc
{
  "ok": true,
  "status": "proposed",
  "operation": "rename",
  "oldName": "boundedCodeResults",
  "newName": "boundedSemanticResults",
  "language": "go",
  "server": "gopls",
  "proposalID": "patch_...",
  "fileCount": 3,
  "editCount": 8,
  "additions": 8,
  "deletions": 8,
  "files": [],
  "diff": "...",
  "expiresAt": "..."
}
```

## 5. LSP 协议

初始化 capability 增加:

- `workspace.workspaceEdit.documentChanges=true`。
- `textDocument.rename.prepareSupport=true`。
- 不声明 `resourceOperations`,避免 server 选择 create / rename / delete file 操作。

请求顺序:

1. `textDocument/prepareRename`:获得 placeholder 并确认位置可重命名。
2. 若 server 返回 method not found,允许降级直接调用 rename。
3. `textDocument/rename`:传入相同 document position 与 `newName`。

`prepareRename=null`、`rename=null` 或空 edit 都作为正常的结构化失败返回,不会生成
空 proposal。

## 6. WorkspaceEdit 安全转换

首版接受:

- `WorkspaceEdit.changes`。
- `WorkspaceEdit.documentChanges` 中的 `TextDocumentEdit`。
- 普通 `TextEdit` 与带 annotation id 的文本 edit。

首版整单拒绝:

- 同时返回 `changes` 与 `documentChanges`。
- `CreateFile`、`RenameFile`、`DeleteFile` 或无法识别的 document change。
- 非 `file:` URI、带 query / fragment 的 URI。
- Project 授权范围外或当前 language root 外的 URI。
- 通过符号链接越过 Project、目录、二进制、超限文件或不同语言源文件的 edit。
- 无效位置、非 Unicode 边界、反向 range、重叠或重复 edit。
- 超过现有 Patch Proposal 的 16 文件、单文件 512 KiB、总文本 2 MiB 或 diff
  256 KiB 限制。

不能过滤掉危险 edit 后继续生成部分 proposal。语义 rename 必须全有或全无,否则可能让
声明与引用不一致。

每个文件按当前磁盘内容重新读取,将协商后的 UTF-8 / UTF-16 / UTF-32 LSP position
严格换算为 byte offset。edit 先检查不重叠,再从文件尾向前应用到内存文本。最终把每个
文件的完整新文本交给现有 Patch Proposal,由其再次完成路径、类型、大小和内容校验。

## 7. 并发与一致性

- rename 请求前同步目标 document,确保光标文件与磁盘一致。
- language server 返回 edit 后读取所有目标文件并创建 proposal 快照。
- proposal 创建后任何目标文件发生变化,`builtin_patch_apply` 都以
  `proposal_stale` 拒绝,不覆盖外部修改。
- 多文件 apply 继续使用同目录临时文件、backup + rename 和逆序回滚。
- turn cancel 会取消正在等待的 LSP request;未完成转换时不保存 proposal。
- proposal 仍按 session 隔离、两小时过期、一次性应用。

## 8. 审批语义

- rename proposal generation 是低风险语义分析;Auto 模式自动执行,Ask 模式沿用
  Project 的读操作审批规则。
- `builtin_patch_apply` 仍是 Project write risk;Auto 对已验证且不含文件删除的
  proposal 自动放行,删除仍请求审批。Ask 的审批 payload 必须包含完整 diff、
  文件路径与 additions / deletions。
- Full Access 可以跳过普通审批,但仍执行 Project path、proposal hash 与事务校验。

## 9. Transcript

折叠行显示名:

- 简体中文:`重命名符号`
- 繁体中文:`重新命名符號`
- 英文:`Rename symbol`

详情复用 Patch Proposal card,顶部增加:

```text
oldName -> newName · Go / TypeScript · language server
```

下面继续显示文件数、增删行、文件列表和 diff。错误使用结构化原因与三语文案,不暴露
`builtin_code_rename` snake_case 名称。

## 10. 错误分类

新增稳定原因:

- `rename_not_available`:光标位置不可重命名。
- `rename_rejected`:language server 拒绝新名称。
- `rename_no_changes`:server 没有返回文本修改。
- `unsafe_workspace_edit`:包含资源操作、混合表示、重叠 edit 或不支持结构。
- `rename_outside_project`:任一 edit 越过 Project 或 language root。
- `rename_too_large`:超出 proposal 文件数、文件大小或 edit 数限制。

已有 language server crash、timeout、cancel、protocol、path 和 proposal 错误继续复用。

## 11. 实施切片

| 切片 | 内容 | 状态 |
| --- | --- | --- |
| C11.0 | 设计、工具与 WorkspaceEdit 安全契约 | 已完成 |
| C11.1 | LSP capability、rename 请求、WorkspaceEdit 转换、proposal 复用 | 已完成 |
| C11.2 | transcript、图标、三语 i18n、Project prompt | 已完成 |
| C11.3 | fake tests、真实 gopls / TypeScript 集成、全量验证 | 已完成 |

## 12. 验收标准

- Go 与 TypeScript / JavaScript 共用 `builtin_code_rename`。
- 工具调用后、Patch Apply 审批前,工作树完全不变。
- 任一 edit 越权或不支持时整单失败,不产生部分 proposal。
- UTF-8 / UTF-16 / UTF-32 与 CRLF edit 坐标转换正确。
- 重叠 edit、资源操作、跨 root、符号链接和超限结果被稳定拒绝。
- apply 前文件漂移不会被覆盖,多文件失败不留下部分写入。
- transcript 显示 rename 元信息和完整 diff,不暴露 snake_case。
- 真实 gopls 与 TypeScript server 均完成 proposal -> apply 验证。
- `go test ./...`、Web build 与 `git diff --check` 通过。

## 13. 后续

rename 稳定后再评估 code action。Code action 仍只能接受可转换为相同 Patch Proposal
的 `WorkspaceEdit`;command-only action、server command 和 `workspace/applyEdit` 继续拒绝。
