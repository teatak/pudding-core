# 轨道 A:SQLite store

> 背景:本仓库是 local-first 多 session AI daemon,M0 已交付契约与行走骨架。
> 当前 main 用内存 store(`internal/store/memstore`)顶着,你的任务是交付持久化实现。
> 通用纪律见 docs/phase-1-plan.md 第 6 节;硬约束见 AGENTS.md。

## 范围

交付 `internal/store/sqlitestore` 包,实现 `store.Store` 接口,与 memstore 可互换。

## 契约(只读,不得修改)

- 接口与语义:`internal/store/store.go`(方法注释即规范)
- schema:`internal/store/schema.sql`(嵌入执行,不另写建表语句)
- 行为参照:`internal/store/memstore/memstore.go`(两实现语义必须一致)

## 实现要求

1. driver 用 `github.com/mattn/go-sqlite3`(旧项目验证过,cgo 可接受)。
2. 打开时执行 `PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`,并把写串行化
   (单写连接或包内 mutex)。为什么:SQLite 并发写直接冲突,架构定的是单 writer。
3. **每个接口方法是一个完整事务**。尤其 `BeginTurn` / `FinishTurn`:message、
   turns 状态、events 必须同事务落库。为什么:不允许出现"events 说 turn 完成
   而 messages 没落库"的中间状态(AGENTS.md 硬约束 15)。
4. 事件 seq 在事务内分配:`SELECT COALESCE(MAX(seq),0)+1 FROM events WHERE session_id=?`。
   为什么:seq 是 SSE Last-Event-ID 续传的基准,必须 per-session 单调无空洞。
5. `BeginTurn` 的检查顺序:幂等优先于并发——同 `clientMessageID` 重放永远返回
   `Duplicate=true` + 原 turn,即使该 turn 还在 running;然后才查 running 冲突
   返回 `store.ErrTurnRunning`。为什么:客户端网络重试不能因为 409 而丢消息。
6. turn 状态只存 `turns` 表,不塞进 messages / events。为什么:cancel、409、
   幂等判断全部查 `turns`,塞别处会出现两个事实源。
7. 时间存 unix 毫秒(schema 注释),读出转 `time.Time`。
8. 接线:可以修改 `cmd/puddingd/main.go` 中 store 的构造处(memstore → sqlitestore,
   路径用 `home.DBPath(dir)`),保留 memstore 包本身(engine 单测在用)。

## 验收

- `go test ./...` 全绿;新增 `internal/store/sqlitestore` 单测覆盖:
  - 幂等重放、ErrTurnRunning、FinishTurn 三种终态、EventsAfter 续传窗口;
  - **持久性**:写入 → Close → 重开同一文件 → sessions/messages/events/seq 完整,
    seq 续接不回退;
  - 外键级联:DeleteSession 后 turns/messages/events 同步消失。
- 手工:`make dev && ./bin/puddingd --mock`,submit 几轮后重启 daemon,
  `GET /sessions/{id}/messages` 数据仍在。

## 禁区

- 不改 `store.go` / `schema.sql` / `event` / `engine` / `api` / web。
  契约有问题在 PR 里提出,由主线修改后你 rebase。
- 不引入 mattn/go-sqlite3 之外的新依赖。
- 测试不得触碰真实 home(`~/.pudding*`),一律 `t.TempDir()`(AGENTS.md 硬约束 18)。

分支:`track/store`。
