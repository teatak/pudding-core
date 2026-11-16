# 独立 API 快速开始

先在 core 执行 `make daemon`，用临时目录启动 mock 服务：

```sh
PUDDING_TEST_HOME="$(mktemp -d)"
bin/puddingd -home "$PUDDING_TEST_HOME" -addr 127.0.0.1:19779 -mock
```

在另一个终端，将上述目录填入 `PUDDING_TEST_HOME`；令牌仅用于本地请求，不要贴进日志或文档：

```sh
PUDDING_TEST_HOME=/path/from/previous/terminal
PUDDING_TEST_TOKEN="$(cat "$PUDDING_TEST_HOME/daemon.token")"
curl -sS -H "Authorization: Bearer $PUDDING_TEST_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"title":"API smoke","provider":"mock","model":"mock"}' \
  http://127.0.0.1:19779/sessions
```

使用响应的会话 `id`，先订阅事件，再在另一个终端提交输入：

```sh
PUDDING_TEST_SESSION=session_id_from_response
curl -N -H "Authorization: Bearer $PUDDING_TEST_TOKEN" \
  "http://127.0.0.1:19779/sessions/$PUDDING_TEST_SESSION/events"

curl -sS -H "Authorization: Bearer $PUDDING_TEST_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"clientMessageID":"example-message-1","parts":[{"type":"text","text":"Hello"}]}' \
  "http://127.0.0.1:19779/sessions/$PUDDING_TEST_SESSION/submit"

curl -sS -X POST -H "Authorization: Bearer $PUDDING_TEST_TOKEN" \
  "http://127.0.0.1:19779/sessions/$PUDDING_TEST_SESSION/cancel"
```

续传 SSE 时携带上次收到的事件 ID：`Last-Event-ID: <seq>`。
最终历史由 `GET /sessions/<id>/messages` 读取。真实模型需要配置 provider；mock 示例不调用外部模型。
UI、语言服务和设备操作不是上述 API smoke 的前提。停止测试服务后再清理其临时目录。

## 本地 MCP 调试（Codex）

运行中的 daemon 在 `/mcp` 提供本地开发用 MCP 入口。它复用 Engine 的工具列表、分发和权限判断，包含 Core、Engine、已连接插件及当前 Desktop 注册的工具。调用返回标准 MCP 文本和图片内容。

### 连接运行中的 daemon

先启动开发版 Desktop，或运行 `make daemon-dev`。脚本只连接已有 daemon；同一数据目录不要另起第二个 daemon。开发通道默认使用 `~/.pudding-dev` 和 `127.0.0.1:9679`。

在 Codex 的 `~/.codex/config.toml` 中配置：

```toml
[mcp_servers.pudding]
command = "python3"
args = ["/absolute/path/pudding-core/scripts/pudding-mcp-stdio.py"]
tool_timeout_sec = 660
```

将脚本路径替换为本机绝对路径，需要 Python 3，无额外依赖。脚本从数据目录读取 `daemon.token`，无需将令牌复制到 Codex 配置。上述工具超时与脚本的 HTTP 超时均为 660 秒，以容纳现有前台命令最长 600 秒的执行窗口；等待审批也会占用调用时间。

数据目录的优先级为 `--home`、`PUDDING_HOME`、`~/.pudding-dev`。使用自定义地址时一并指定 `--addr`；地址支持本机 loopback 的 `host:port` 或 `http://host:port`。例如连接上文的临时 daemon：

```toml
args = ["/absolute/path/pudding-core/scripts/pudding-mcp-stdio.py", "--home", "/absolute/path/test-home", "--addr", "127.0.0.1:19779"]
```

连接发布版时，显式使用发布通道的数据目录和端口：

```toml
args = ["/absolute/path/pudding-core/scripts/pudding-mcp-stdio.py", "--home", "/absolute/path/.pudding", "--addr", "127.0.0.1:9669"]
```

没有指定 RuntimeID 时，服务端自动选择最新活跃 Desktop 连接。需要固定连接时给脚本加 `--runtime-id <runtimeID>`，通过现有 `X-Pudding-Runtime-ID` 请求头传递。没有 Desktop 连接时，Core 工具仍可用；Desktop / Widget 工具需要相应运行时在线。

### 工具列表与会话

初次请求默认创建或复用 `session_mcp_debug`，初始模式为 `code`。默认调试会话会准备当前可用的插件工具；之后可用现有 `builtin_plugin_load` / `builtin_plugin_unload` 调整加载范围。列表仍遵循当前模式、能力和连接条件，“全量”不表示忽略这些约束。

自动创建默认调试会话沿用已有模型配置选择逻辑，因此需要至少一个可用的模型配置；入口读取该配置，不因此发起模型请求。`-mock` 仅替换模型执行器，不会自动生成配置。使用空临时目录时，先配置模型（也可配置 mock profile），或先通过现有 API 创建会话，再用下面的元工具绑定它。

`pudding_mcp_session` 是始终可见的元工具。`tools/call` 的参数示例：

```json
{"name":"pudding_mcp_session","arguments":{}}
```

空参数查看当前绑定。先在 Pudding 打开个人助手，再切换到它的真实会话，以调试 `assistant_*`：

```json
{"name":"pudding_mcp_session","arguments":{"sessionID":"personal"}}
```

也可传已有会话的真实 ID。省略 `mode` 使用该会话现有模式；显式传 `chat`、`work` 或 `code` 会更新目标会话的实际模式，不是临时覆盖。切换到已有会话不会替它自动加载全部插件。

返回默认调试会话：

```json
{"name":"pudding_mcp_session","arguments":{"sessionID":"debug","mode":"code"}}
```

绑定在当前 daemon 内共享，切换会影响其他连接此入口的调试客户端；daemon 重启后绑定恢复默认。普通 `tools/call` 使用真实工具 Turn 并留下工具历史，入口本身不发起 LLM 对话；工具本身已有的外部调用和副作用照常执行。原有审批仍然生效，按 Turn 授予的权限不会跨独立调用延续。

前台 `command_run` 在 MCP 调用中会等待最终完成：发生常规 yield 后，Engine 在同一 Turn 内通过现有 `command_session` 继续轮询，再返回最终结果。需要跨调用轮询、交互或持续运行的服务时，使用已有 `background: true`，之后再调用 `command_session`；后台命令不会被自动等待至完成。

切换会话、加载插件或连接 Desktop 后，下一次 `tools/list` 返回新目录。Codex 可能缓存目录，此时需要显式重新发现 / 重连该 MCP 并确认新工具已列出；仅新开聊天不保证刷新。首版使用 JSON 响应，不声明主动 `tools/list_changed` 通知。

### 协议与正常验收

`/mcp` 支持 MCP `2025-06-18` 的 `initialize`、`notifications/initialized`、`ping`、`tools/list` 和 `tools/call`。HTTP 使用 `Authorization: Bearer <daemon.token>`；通知返回 `202`，请求返回 JSON，`GET /mcp` 返回 `405`。stdio 的 stdout 仅输出逐行 JSON-RPC，诊断写 stderr。转发不重试业务调用、不启动额外 daemon。

验收按正常主流程进行：连接后列出工具，调用一个普通工具，保持 Desktop 在线并调用一个 Desktop / Widget 工具（包含图片结果），切换 `personal` 后重新发现目录并调用适用的助手工具。涉及写操作或个人数据的工具按实际调试任务选择，审批在原会话中处理。
