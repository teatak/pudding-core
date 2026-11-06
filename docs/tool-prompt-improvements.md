# 工具与提示词改进清单

记录 2026-11-06 会话中暴露的工具契约、提示词与交互缺口，并给出可直接落地的修改建议。按优先级排列：P0 影响任务正确性，P1 造成明显浪费，P2 影响可观测性。

## 证据来源

- 会话：`sess_b36d4951990fb2d2`，覆盖阿里云证书签发与 OSS 部署、Cloudflare DNS 记录、Buzz 用量核对、双邮箱简报、三城天气对比、话题标记讨论。
- 三类可复现现象：
  - 用户明确要求「切一个话题」，模型口头确认但未调用 `builtin_set_turn_topic`，连续两次。
  - 新增 Cloudflare DNS 记录时，记录类型下拉框三次误选 `TLSA`；页面滚动后坐标系整体偏移，多次点击落在空元素上。
  - 整个证书与 DNS 流程截图约 18 次，其中至少 7 次对推进任务没有贡献。

## P0：明确指令必须当轮执行

### 1. `builtin_set_turn_topic` 缺少强制措辞

当前描述以「Optionally …」「Omit when unnecessary」为主，全程可选。用户在 10:12 说「那切一个话题吧」，模型输出了讨论内容却没有调用工具；10:13 被追问后才补调用。对照 `builtin_assistant_rename` 的「Carry out a naming request in the same turn」，两者执行率差异明显。

建议补充：

> If the user explicitly asks to start a new topic, call this tool in that same turn. Saying you did it is not enough.

### 2. `ends the turn` 的语义歧义

现描述为「These UI-only calls end the turn without another model request.」模型据此担心调用会截断正文，于是在需要继续讨论时选择不调用。

建议补充：

> The answer text and this call are delivered together; it ends only the current turn, not the answer.

### 3. 区分「主动标记」与「被要求标记」

现描述同时包含「Do not proactively mark every turn」和「omit when unnecessary」。用户说过「不用特意做」后，模型把该偏好泛化到所有场景，连明确指令也缩手。

建议补充：

> Do not proactively mark every turn. An explicit user request always overrides that default.

### 4. `builtin_set_next_prompt` 存在同样缺口

同样缺少「用户要求时必调」的措辞。另外应显式说明「Tab 只填充草稿，不等于用户已发送」，避免模型把建议当作已授权的执行。

## P1：浏览器操作

| 现象 | 影响 | 建议 |
| --- | --- | --- |
| 滚动后坐标失效 | 页面 `scrollY` 变化后整页坐标系偏移，旧坐标点到相邻元素 | 坐标改用文档坐标并自动换算，或在 `observe` 结果中标注坐标系基准 |
| 原生下拉只能按坐标点选项 | Cloudflare 记录类型下拉三次误选 `TLSA` | 提供按显示文本或 value 选择的入口，避免逐项点击 |
| 输入框设值不触发事件 | `datetime-local`、自定义 combobox 用文本输入后不生效 | 支持设值并 `dispatch` `input` / `change` / `blur` 后回读校验 |
| 截图优先于 DOM 快照 | 单个流程截图 18 次，7 次无效 | 描述中写死：优先使用 `observe` 返回的 element ref；截图坐标是最后手段 |
| `ok: true` 被当作业务成功 | 输入已送达但业务未生效时继续推进 | 强调返回只代表输入已送达，必须重新观测或读取业务结果 |

同时建议在工具描述之外补一条行为准则：重复性、结构化操作（DNS 记录、云资源增删改、批量配置）优先走官方 API 或插件连接；缺少连接时先建议接入，而不是直接用 UI 点击完成。

## P2：可观测性与工具细节

1. `builtin_set_turn_topic` 返回的 `turnID` 无法反查：`builtin_history_get_message` 只接受 `msg_` 前缀消息 ID，模型无法确认话题起点。建议返回 `startMessageID` 或可读的轮次描述。
2. 命令退出码被管道掩盖的问题已有 `pipeline_exit_status` 与 `masked_exit_status` 警告，建议同样覆盖 `cmd || exit 0` 一类写法。
3. 权限边界需要在拒绝时说明具体缺哪一层：本次会话中 `request_capability` 一度返回 `already_available`，但文件工具仍报 `path_not_authorized`，模型只能靠试错判断，建议直接给出当前生效的目录列表。

## 建议的提示词片段

可直接复用到工具描述中的最小集合：

> - If the user explicitly asks to start a new topic, call this tool in that same turn; saying you did it is not enough.
> - The answer text and this call are delivered together; it ends only the current turn, not the answer.
> - Do not proactively mark every turn; an explicit user request overrides that default.
> - Prefer element refs from `observe`. Screenshot coordinates are a last resort and are invalid after scrolling.
> - Delivered input does not prove the business operation succeeded; verify the outcome.

## 验证方式

- 在 `agent-eval` 场景中加入两个用例：一是用户显式要求切换话题，检查是否当轮调用工具；二是原生下拉选择任务，检查是否按文本选中而不是按坐标。
- 复核指标：单任务截图次数、坐标点击重试次数、`ok: true` 后业务未变更的比例。
