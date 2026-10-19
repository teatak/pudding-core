# summarize_results 离线评测档案

实验已于 2026-10-19 停止并撤回。这里仅保留离线分析／冻结样本脚本；不再是可构建的 Go 命令，不会在正常构建中注册功能或触发模型请求。联网评测实现及 `compare.py` 已移入 [源码恢复补丁](../../docs/validation/summarize-results-experiment-core-2026-10-19.patch)。

原始数据位于 [`docs/validation/`](../../docs/validation/)，结论及边界见 [失败总结](../../docs/tool-result-summaries-withdrawal.md)。从 Core 仓库根目录可离线复核，例如：

```bash
python3 cmd/memo-prototype/repository_analyze.py \
  docs/validation/summarize-results-repository-fixtures-2026-10-19.json \
  docs/validation/summarize-results-repository-deepseek-2026-10-19.json \
  --output /tmp/summarize-results-repository-review.json

python3 cmd/memo-prototype/brief_analyze.py docs/validation \
  --output /tmp/summarize-results-brief-review.json

python3 cmd/memo-prototype/trigger_analyze.py docs/validation \
  --probes --output /tmp/summarize-results-trigger-review.json
```

输出路径须尚不存在，不覆盖历史记录。`repository_fixtures.py` 是当时制作样本的脚本；复核请使用已冻结的 JSON，不用当前源码重新生成后冒充同一份样本。

`checkpoint_analyze.py` 要求八次运行全部完成。该实验由用户在第八次中叫停，当前记录不满足条件；保留校验，不能删去中断样本或伪造完成状态来生成完整对照。

恢复补丁同时涉及 Core 和 Desktop，仅用于追溯或显式恢复实验。原始各轮报告对应不同提示版本，最新恢复补丁不能冒充所有历史版本；具体身份以各轮元数据的源码／二进制哈希为准。
