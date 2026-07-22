# 本地工具使用率报告

`puddingd tools report` 从本地 SQLite 的 canonical `messages.parts` 与 `turns`
派生统计,不新增埋点、远程遥测或统计事实表。数据库通过 SQLite `mode=ro` 打开,
报告不会输出工具参数、项目路径或结果正文。

## 使用

开发构建默认读取 `~/.pudding-dev`:

```bash
./bin/puddingd tools report --days 30
```

也可以直接通过 Make 运行:

```bash
make tools-report RUNARGS="--days 30"
```

指定数据目录或输出 JSON:

```bash
./bin/puddingd tools report --home ~/.pudding --days 90
./bin/puddingd tools report --days 30 --json
./bin/puddingd tools report --days 30 --all
```

`--days` 范围为 1-3650。未指定 `--home` 时继续遵守 channel 隔离:开发构建读取
`~/.pudding-dev`,release 构建读取 `~/.pudding`。

默认只展示窗口内被调用过的工具。`--all` 还会列出当前内置定义中零调用的工具,
适合盘点删除或合并候选。JSON 输出同样支持 `--all`。

## 指标

| 列 | 含义 |
| --- | --- |
| `CALLS` | 窗口内 canonical tool use 数量 |
| `TURN%` | 使用该工具的 Turn / 当前工具有资格出现的 Turn |
| `OK%` | 成功 tool result / 已完成 tool result |
| `REPEAT%` | 同一 Turn 内第二次及后续同名调用 / 调用数 |
| `CLI-FB%` | 专用工具失败后,同一 Turn 改用同领域 CLI / 失败调用数 |
| `P95 RESULT` | canonical tool result UTF-8 字节数的 P95 |

Turn 分母按当前工具 capability 累积计算:Chat 工具使用全部 Turn,Work 工具使用
Work + Code Turn,Code 工具只使用 Code Turn。已经不在当前内置工具定义中的历史、
App 或 UI 工具仍统计调用数,但 `TURN%` 显示 `-`,避免猜测其历史可见范围。

CLI 回退只识别明确的同领域命令:

- Git:`git`
- 文件:`rg`、`find`、`ls`、`cat`、`sed`、`head`、`tail` 等
- Code:`gopls`、`typescript-language-server`、`tsserver`

这个指标用于发现专用工具失败后的替代路径,不是所有命令调用的等价性证明。

`builtin_command_run/session` 统一归入 `command` 组;
`builtin_app_load` 单列为 `app` 组,便于观察 CLI 使用与会话级能力扩展频率。

## 使用原则

报告用于发现合并和 CLI 替代候选,不能单独决定删除工具。审批、Patch 原子应用、
Project 授权、LSP WorkspaceEdit 验证与系统能力即使调用率低,仍可能具有不可替代的
安全价值。决策前应同时抽查真实 Turn。
