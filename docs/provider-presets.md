# 模型预设

`web/src/provider/presets.ts` 仅提供创建、导入模型时的模板。运行时仍以用户保存的 `<home>/config/*.yaml` 为准，更新预设不会迁移已有配置。

2026-09-12 核对的更新：

| 厂商 | 预设变化 | 官方依据 |
| --- | --- | --- |
| DeepSeek | Flash 使用 `deepseek-flash`，开启图片能力；保留 `deepseek-v4-pro`；增加 Responses 协议 | [模型说明](https://api-docs.deepseek.com/zh-cn/quick_start/pricing/)、[Responses API](https://api-docs.deepseek.com/zh-cn/api/create-response/) |
| OpenAI | Responses 增加 `gpt-6-astra`；保留 5.6 系列和较低成本选项；修正 5.5/5.4 的上下文和音频标记 | [Astra 指南](https://developers.openai.com/api/docs/guides/latest-model)、[GPT-5.5](https://developers.openai.com/api/docs/models/gpt-5.5)、[GPT-5.4](https://developers.openai.com/api/docs/models/gpt-5.4) |
| Anthropic | 更新为 Fable 5.1、Opus 5、Sonnet 5，保留 Haiku 4.5 | [模型目录](https://platform.claude.com/docs/en/models/overview) |
| Gemini | 更新为 3.8 Flash、3.5 Flash-Lite、3.1 Pro Preview；修正 token 限额 | [模型目录](https://ai.google.dev/gemini-api/docs/models) |
| Qwen | 更新为 3.8 Flash、3.8 Max 0902，保留 3.7 Plus；补齐图片能力和输出限制 | [更新记录](https://help.aliyun.com/zh/model-studio/newly-released-models)、[Max 参数](https://help.aliyun.com/zh/model-studio/qwen3-8-max) |
| Kimi | 增加 K2.7 Code；补齐视觉能力，修正 K2.6 的上下文 | [模型目录](https://platform.kimi.com/docs/models)、[K2.7 Code](https://www.kimi.com/resources/kimi-k2-7-code) |
| GLM | 增加 5.3 Flash；5.2/5.1 标记为纯文本，5.1 上下文修正为 200K | [5.3 Flash](https://docs.bigmodel.cn/cn/guide/models/vlm/glm-5.3-flash)、[5.1](https://docs.z.ai/guides/llm/glm-5.1) |
| OpenRouter | 增加 Nemotron 3.5 Lightning Free，移除 Owl Alpha 和 Laguna 临时预设 | [在线模型目录](https://openrouter.ai/api/v1/models)、[Owl 端点](https://openrouter.ai/api/v1/models/openrouter/owl-alpha/endpoints) |

同一模型的能力与限额跨协议共享；输出限制直接放在 `limits.maxOutputTokens`，由适配器转换为协议字段。预设和模型导入均不再指定 `temperature=0.2`，遵循供应商默认采样行为；用户可关闭“使用供应商默认”后通过滑块设置温度，重新开启则移除温度参数。

Astra 的工具调用要求 Responses，所以仅加入 Responses 模板。MiMo 保留已开放的 V2.5 系列；BuzzHive/Ollama 继续动态发现模型。

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
