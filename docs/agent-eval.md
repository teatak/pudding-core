# Agent Eval

`agent eval` 通过真实 Pudding daemon、provider、engine 和 Code tools 运行隔离的小型项目任务。
fixture 会复制到系统临时目录并初始化为独立 Git 仓库，不修改当前工作区，也不允许 agent commit 或 push。

默认从开发 home 读取显示名为 `BuzzHive` 的 profile，并按顺序选择
`deepseek-v4-flash`、`mimo-v2.5`、`deepseek-v4-pro`、`mimo-v2.5-pro` 中第一个支持 tools 的模型。
只把选中的 profile 写入临时 home，不复制会话、用户提示词或其他配置。临时 home 始终删除；
`--keep` 只保留不含凭证的 fixture。

## 使用

先跑单个案例控制 API 消耗：

```bash
make agent-eval RUNARGS="--case pagination-boundary --model deepseek-v4-flash"
```

跑全部案例并保存 JSON：

```bash
make agent-eval RUNARGS="--runs 3 --output /tmp/pudding-agent-eval.json"
```

常用参数：

- `--provider`：profile ID 或显示名，默认 `buzzhive`。
- `--model`：显式模型 ID。
- `--case`：逗号分隔的案例名；不传表示全部。
- `--runs`：每个案例重复次数，范围 1–10。
- `--keep`：保留临时 fixture，报告中记录其路径。
- `--mock`：只验证 runner 链路，不访问外部 provider；任务本身预期不会通过。
- `--json`：把完整报告输出到 stdout。

## 判分

每次运行必须同时满足：

- turn 正常完成；
- 外部验证命令通过；
- 修改只发生在案例允许路径；
- agent 没有创建 Git commit；
- 没有把失败的验证描述为通过。

报告同时记录 provider 请求次数、工具调用序列、工具失败摘要、失败文件工具的安全 `scope/path` 参数、命令尝试、重复调用、
agent 是否主动执行验证、cached/uncached token 用量和最终回复。
案例位于 `evals/cases`，fixture 位于 `evals/fixtures`。
