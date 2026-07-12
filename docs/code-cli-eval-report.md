# Code CLI-first Eval 报告

> 日期:2026-07-12
> 平台:darwin/arm64
> 结论:CLI 作为默认路径;专用工具转 lazy fallback,当前不删除实现

## 1. 可重复命令

```bash
make tools-eval
make tools-eval RUNARGS="--json"
make tools-report RUNARGS="--days 30 --all"
```

`tools-eval` 不调用外部模型,会创建临时 Git fixture,分别通过
`builtin_command_run` 与专用工具完成相同只读任务并比较语义结果。

## 2. 确定性结果

| Case | Domain | 结果 | 专用结果 | CLI 结果 |
| --- | --- | --- | ---: | ---: |
| file_list | file | 通过 | 838 B | 346 B |
| file_stat | file | 通过 | 337 B | 291 B |
| file_search | file | 通过 | 864 B | 374 B |
| file_slice | file | 通过 | 435 B | 314 B |
| git_status | git | 通过 | 594 B | 335 B |
| git_diff | git | 通过 | 505 B | 324 B |
| git_log | git | 通过 | 437 B | 297 B |

合计 7/7 通过;专用结果 4010 B,CLI 结果 2281 B,CLI 少约 43%。
`file_slice` 与 `sed` 只存在单个终止换行差异。

## 3. 30 天使用基线

本机开发数据共 145 turns,其中 Code 39 turns:

- `builtin_command_run`:28 calls,成功率 64.3%,重复率 82.1%。历史失败包含
  旧能力测试和仓库路径错误,不能直接视为真实 coding 失败率。
- `builtin_file_read`:19 calls,成功率 84.2%。继续作为 Core,承担文件预览、
  图片和稳定的小文本读取。
- `builtin_file_list`:12 calls,成功率 100%;CLI fixture 已覆盖等价只读路径。
- `builtin_file_stat`:3 calls;`file_search`:0;`file_slice`:1 且失败。
- Git read、LSP 工具均为 0 calls,没有足够真实样本支持删除。

## 4. 最终矩阵

| 工具 | M4 位置 | 当前决定 |
| --- | --- | --- |
| project inspect/instructions | Core | 保留默认 |
| command run | Core | 保留默认,CLI-first |
| file read | Core | 保留默认 |
| patch propose/apply | Core | 保留默认 |
| file list/stat/search/slice | `code.files-read` | 默认隐藏,CLI 失败时加载 |
| file write/patch/delete/move/copy | `code.files-write` | 默认隐藏 |
| Git status/diff/log | `code.git-read` | 默认隐藏,CLI 失败时加载 |
| Git stage/unstage/commit | `code.git-write` | 默认隐藏,保留结构化审批 |
| LSP tools | `code.lsp` | 默认隐藏,语义任务时加载 |
| background process | Terminal App | 默认隐藏,dev server 等任务时加载 |
| skill validate/submit | `code.skill` | 默认隐藏 |

## 5. M5 删除结论

M5 不删除现有专用工具实现。确定性 Eval 支持把文件与 Git 只读能力改为 CLI-first,
但 30 天数据中 Git read 与 LSP 均为 0 次,尚不足以证明删除后没有回归。它们已转入
lazy toolkit,默认不占工具 schema;后续积累至少 10 个真实模型固定任务后再重新评估。

可执行文件缺失必须返回正常 CLI 结果并通过 lazy toolkit fallback 恢复,不能成为
turn 级错误。当前默认 Code 暴露 17/58 个 schema,JSON 从 41941 B 降至
13574 B,减少 67.6%。
