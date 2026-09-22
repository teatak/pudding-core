# 模型预设

桌面仓库 `pudding-desktop/web/src/provider/presets.ts` 仅提供创建、导入模型时的模板。运行时仍以用户保存的 `<home>/config/*.yaml` 为准，更新预设不会迁移已有配置。

2026-09-12 核对的更新（OpenRouter 于 2026-09-14 复核）：

| 厂商 | 预设变化 | 官方依据 |
| --- | --- | --- |
| DeepSeek | Flash 使用 `deepseek-flash`，开启图片能力；保留 `deepseek-v4-pro`；增加 Responses 协议 | [模型说明](https://api-docs.deepseek.com/zh-cn/quick_start/pricing/)、[Responses API](https://api-docs.deepseek.com/zh-cn/api/create-response/) |
| OpenAI | Responses 增加 `gpt-6-astra`；保留 5.6 系列和较低成本选项；修正 5.5/5.4 的上下文和音频标记 | [Astra 指南](https://developers.openai.com/api/docs/guides/latest-model)、[GPT-5.5](https://developers.openai.com/api/docs/models/gpt-5.5)、[GPT-5.4](https://developers.openai.com/api/docs/models/gpt-5.4) |
| Anthropic | 更新为 Fable 5.1、Opus 5、Sonnet 5，保留 Haiku 4.5 | [模型目录](https://platform.claude.com/docs/en/models/overview) |
| Gemini | 更新为 3.8 Flash、3.5 Flash-Lite、3.1 Pro Preview；修正 token 限额 | [模型目录](https://ai.google.dev/gemini-api/docs/models) |
| Qwen | 更新为 3.8 Flash、3.8 Max，保留 3.7 Plus；补齐图片能力和输出限制 | [更新记录](https://help.aliyun.com/zh/model-studio/newly-released-models)、[Max 参数](https://help.aliyun.com/zh/model-studio/qwen3-8-max) |
| Kimi | 增加 K2.7 Code；补齐视觉能力，修正 K2.6 的上下文 | [模型目录](https://platform.kimi.com/docs/models)、[K2.7 Code](https://www.kimi.com/resources/kimi-k2-7-code) |
| GLM | 增加 5.3 Flash；5.2/5.1 标记为纯文本，5.1 上下文修正为 200K | [5.3 Flash](https://docs.bigmodel.cn/cn/guide/models/vlm/glm-5.3-flash)、[5.1](https://docs.z.ai/guides/llm/glm-5.1) |
| OpenRouter | 保留 Free 自动路由，加入六家常用付费模型；补齐视觉、音频、上下文和输出限额，移除原来的三个固定免费模型预设 | [在线模型目录](https://openrouter.ai/api/v1/models) |

同一模型的能力与限额跨协议共享；输出限制直接放在 `limits.maxOutputTokens`，由适配器转换为协议字段。预设和模型导入均不再指定 `temperature=0.2`，遵循供应商默认采样行为；用户可关闭“使用供应商默认”后通过滑块设置温度，重新开启则移除温度参数。

Astra 的工具调用要求 Responses，所以仅加入 Responses 模板。MiMo 包含 V2.6 Flash / Pro 和 V2.5 系列；BuzzHive/Ollama 继续动态发现模型。

## MiMo V2.6（2026-09-22）

标准及 Plan 的 OpenAI / Anthropic 模板均增加 `mimo-v2.6-flash`、`mimo-v2.6-pro`；显示名统一使用 `MiMo`。两者标记图像、音频和工具能力，上下文按官方 1M 保守设置为 1,000,000，最大输出 131,072。依据：[V2.6 发布说明](https://mimo.mi.com/docs/en-US/news/latest/v2-6)、[模型参数](https://mimo.mi.com/models/en-US/mimo-v2.6-pro)、[Responses API](https://mimo.mi.com/docs/en-US/api/chat/responses)。

思考强度在 provider 请求边界按版本化 MiMo 推理模型家族适配，不再枚举 V2.5 两个 ID：`xhigh/max` 发送为 `high`，其余值原样保留；包括 `xiaomi/`、`xiaomimimo/` 厂商前缀，不匹配 ASR/TTS 或任意自定义别名。会话五档偏好和已保存配置不变。官方目前说明非 `none` 档位均启用思考、实际强度不区分；此映射避免兼容端点拒绝扩展档位。

回归使用模拟上游检查 Chat、Responses、Anthropic 实际序列化请求及配置不变性，不代表已完成付费 API 实测。已有模型显示名和配置不会随模板更新自动迁移。

## 从端点导入模型

候选目录返回结构化模型信息，新建动态配置和在已有配置中添加候选都采用相同规则：端点明确返回的字段优先，缺失字段由完整模型 ID 精确匹配的预设补齐；不会依据品牌图标或模型家族猜测限额。仅有 ID 的端点仍可正常导入，未知上下文和输出上限留空。能力缺失时沿用原有表单默认（图像、音频关闭，工具开启），不代表已经确认支持；端点明确返回的 `false` 会覆盖预设。

- OpenRouter：读取 `name`、`context_length`、`architecture.input_modalities` 和 `supported_parameters`。[目录协议](https://openrouter.ai/docs/api/api-reference/models/get-models)
- BuzzHive：读取 `/v1/models` 中管理员已保存的显示名、上下文、输出限额及 `vision/audio_input/tools` 能力。BuzzHive 需更新到包含该目录扩展的版本；不会从路由名称推测能力。
- Gemini：读取 `displayName`、`inputTokenLimit` 和 `outputTokenLimit`，只列出支持 `generateContent` 的模型。[目录协议](https://ai.google.dev/api/models)
- Anthropic：读取 `display_name`、`max_input_tokens`、`max_tokens` 和 `capabilities.image_input.supported`。[目录协议](https://platform.claude.com/docs/en/api/models/list)

候选仅是导入来源，选择添加后才写入用户配置；刷新目录不会覆盖已保存模型。采样参数保持当前协议默认，Astra 的 Chat Completions 工具限制继续生效。

模型选择器中的 BuzzHive/OpenRouter 自动或手动同步会更新上下文、能力等目录元数据，但不改写已有模型的显示名，包括用户清空后的空值。BuzzHive 同步新发现的模型时仍采用上游名称；OpenRouter 不自动追加目录中的其他模型。

## OpenRouter 模型精选

使用 `https://openrouter.ai/api/v1` 的 OpenAI Chat Completions 兼容协议。模型 ID、能力与限额以 OpenRouter 目录为准，不直接套用厂商直连端点的元数据。

| 显示名 | 模型 ID | 类型 |
| --- | --- | --- |
| Free | `openrouter/free` | 免费自动路由 |
| GPT 5.6 Sol | `openai/gpt-5.6-sol` | 付费 |
| Claude Sonnet 5 | `anthropic/claude-sonnet-5` | 付费 |
| Gemini 3.8 Flash | `google/gemini-3.8-flash` | 付费 |
| DeepSeek V4.1 Flash | `deepseek/deepseek-v4.1-flash` | 付费 |
| Qwen3.8 Flash | `qwen/qwen3.8-flash` | 付费 |
| Kimi K3 | `moonshotai/kimi-k3` | 付费 |

Free 保持列表首项，输出上限由实际路由到的模型决定，预设不填未知上限。其它模型均支持图像输入和工具调用；Gemini 另支持音频输入。移除的固定免费项不再作为创建模板提供，已有配置不变，仍可通过模型发现按需导入。

## OpenRouter 应用归属

Chat Completions、Responses、Anthropic 和 Gemini 的模型请求统一携带 Pudding 应用标识，直连 OpenRouter 或经 BuzzHive 转发时均可用于归属统计，无需修改已保存的提供方配置：

```http
HTTP-Referer: https://x-t.top
X-OpenRouter-Title: Pudding
X-OpenRouter-Categories: programming-app,personal-agent
```

分类包括编程应用（`programming-app`）、个人助手（`personal-agent`）、通用聊天（`general-chat`）和创意写作（`creative-writing`），对应 Coding、Productivity 与 Creative 三个大类。

按官方已公布的单次两个分类限制，正常模型请求轮换携带 `programming-app,personal-agent` 与 `general-chat,creative-writing`，由 OpenRouter 合并应用分类。轮换仅使用进程内原子计数，不持久化，也不额外发送模型请求；两个分组都到达 OpenRouter 后才能完成四类归属。2026-09-14 核对时，官方页面的上限数字显示缺失，搜索索引中的官方文档仍明确写明单次两个分类、累计最多十个。

标识统一定义在 `internal/provider/attribution.go`，不按模型名或代理地址推断。OpenRouter 收到带标识的实际调用后可建立应用条目；榜单只统计经过 OpenRouter 的调用。规则见 [OpenRouter App Attribution](https://openrouter.ai/docs/app-attribution)。

## DeepSeek Responses

复用 `internal/provider/openai/responses.go`，配置协议为 `openai-responses`、base URL 为 `https://api.deepseek.com`，实际请求 `POST /responses`。保留现有 Chat Completions 默认选项，用户可显式选择 Responses。

DeepSeek 不保存服务端会话，每次请求需携带完整历史。适配器从显式请求消息重建输入，将返回的明文 reasoning 和 function call 原样作为 continuation 回传，再附加对应 `function_call_output`；不会使用 `previous_response_id`。`store`、`include` 等未实现参数按官方约定被忽略，无需增加厂商分支。详见 [Responses 指南](https://api-docs.deepseek.com/zh-cn/guides/responses_api/)。

协议回归：`internal/provider/openai/deepseek_responses_test.go`。测试使用模拟 HTTP 响应，覆盖工具循环、明文推理续传、图片、用量、终止事件、取消及不同会话的历史隔离，不代表已完成付费线上 API 调用。

并行工具的流式事件通过 `output_index` 传递独立索引，避免引擎将不同调用及参数合并。`internal/engine/responses_parallel_tools_test.go` 覆盖两个同名工具的交错参数、实际执行、结果配对及下一轮 canonical 历史回放。旧版本已产生的缺失结果不会被自动补造；遇到此类失败记录时，在修复后的开发进程中用新会话重试。
