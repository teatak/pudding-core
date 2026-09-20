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
