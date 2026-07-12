# 第一阶段开发规划

> 范围:[technology-decisions.md](technology-decisions.md) 第一阶段(文本多会话)。  
> 验收:technology-decisions.md 第 12 节全部通过。  
> **状态:已完成(2026-06-11)。** 后端项由 `go test -race` 与 smoke 钉死,
> 前端项经真实浏览器验收(含双 session 并行、offline 重连、双击幂等)。
> 超出原计划交付:settings 驱动的 provider 配置(免重启生效)、events retention、
> SSE tail 语义、真端点 smoke(Ollama)。

## 1. 模块拆分

```text
cmd/puddingd/            # 入口、wiring、--mock 模式
internal/store/          # SQLite:sessions / turns / messages / events / settings,单 writer
internal/event/          # 事件类型、per-session seq、hub(广播 + 续传窗口)
internal/provider/       # llm Client 接口定义
internal/provider/openai/  # OpenAI-compatible streaming 实现
internal/provider/mock/    # 脚本回放 provider(可控时序 / 失败 / cancel 注入)
internal/contextbuilder/ # canonical messages → provider 输入
internal/engine/         # per-session turn 状态机(submit / cancel / 幂等 / 落库)
internal/api/            # cart v3 路由、REST handler、SSE handler、loopback + token
web/                     # 前端
```

模块间只通过接口依赖,不允许跨包伸手:

```text
api → engine → provider
api → store(只读快照)      engine → store(唯一写入方)
engine → contextbuilder → store(只读 canonical messages)
api → event ← engine
```

## 2. 契约先行(并行的前提)

并行拆分能否成立,完全取决于先冻结四份契约。契约期单线完成,不并行,约 1–2 天:

1. HTTP API 形状 —— 已定稿(technology-decisions.md 第 7 节)。
2. 事件协议 —— 已定稿(第 8 节);第一阶段手写 `internal/event/types.go` 与 web 端 zod schema,事件名 / 字段名必须一一对应,M0 建一份字段对照 checklist 随契约提交;代码生成(如从单一 schema 出发)后续再考虑。
3. SQL schema —— 参考旧仓库 `internal/storage/sqlite/schema.sql` 裁剪,留 sessions / messages / events / settings。
4. Go 接口 —— `store.Store`、`provider.Client`、`engine` 对外方法签名。

契约冻结后改动必须单独 commit、说明影响哪些轨道;不允许某条轨道顺手改契约。

## 3. 并行轨道

契约冻结后,五条轨道可同时开。各轨道只改自己的包,互不依赖实现,依赖处用 fake 顶住:

| 轨道 | 内容 | 解耦手段 | 人日 |
| --- | --- | --- | --- |
| A. store | SQLite 实现、当前 schema 初始化、WAL、单 writer、事务规则 | 只依赖 schema | 2 |
| B. provider | OpenAI streaming、cancel、mock provider | 只依赖 `provider.Client` 接口 | 2–3 |
| C. api + event | 路由、REST/SSE handler、hub、seq、`Last-Event-ID` 续传、token | engine/store 用 fake 实现顶着 | 3–4 |
| D. engine | per-session turn 状态机、submit→stream→落库、cancel、`clientMessageID` 幂等、并发 409 | 单测全部跑在 mock provider + 内存 store 上 | 4–5 |
| E. web | 脚手架、session 列表、transcript(query + overlay)、composer、settings 页 | 对 `--mock` daemon 开发,不等真 provider | 13–17 |

建议:D(engine)是唯一没有参考实现、决定整体形状的模块,放在主注意力上做;A/B/C/E 参考旧代码成分高,适合分给并行 agent 会话(各自 worktree)。

## 4. mock provider 是并行的关键

- provider 只产出模型流:`delta* → finish | error`。turn lifecycle 事件(`turn.started / completed / failed / cancelled`)由 engine 生成,provider 不拥有业务生命周期。
- `internal/provider/mock`:按脚本回放模型流,延迟、失败、cancel 时序可注入。
- `puddingd --mock`:全链路真实(api/event/engine/store),仅 provider 为 mock。web 轨道从第一天就有可联调的后端。
- engine 与 SSE 续传的集成测试全部基于 mock provider,不依赖网络。

## 5. 里程碑与时间线

| 里程碑 | 内容 | 验收 |
| --- | --- | --- |
| M0(d1–2) | 契约冻结:四份契约 + mock provider 骨架 | 接口编译通过,事件协议两端同源 |
| M1(week 1 末) | 垂直切片:单 session `submit → OpenAI → SSE → 落库` | curl 全流程可验证 |
| M2(week 2) | 多会话核心:并行 turn、cancel、幂等、seq 续传、重启恢复 | 验收第 12 节后端项全过(集成测试钉死) |
| M3(week 3) | web 主体对真 daemon 联调:列表 / transcript / composer / cancel | 双 session 并行 streaming 页面可演示 |
| M4(week 4) | settings 页、token 握手、回归 | 验收第 12 节全过 |

工期:总量 30–40 人日;契约先行 + 轨道并行后,关键路径 ≈ M0(1.5d)+ engine(4–5d)+ 集成(2–3d)与 web 轨道(13–17d)取大者,日历约 3–4 周;并行 agent 开发可再压缩。

## 6. 集成纪律

- 合并顺序:store → event types → provider mock → engine → api → web。api 的 submit/cancel handler 必须贴着 engine 真实接口写,fake 只用于轨道期开发,合并前替换。
- 每条轨道独立分支/worktree,只改自己的包;契约文件改动单独提交。
- M1 之后,messages 的唯一写入方是 engine;任何轨道不得绕过 engine 直接写库。
- 每个里程碑收口时跑一遍验收清单已实现部分,不积攒回归债。
