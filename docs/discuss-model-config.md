# 讨论:model / profile 配置形状与 preset

> 状态:讨论稿,征求评审。范围:provider profile 下 per-model 参数的存储形状、
> 配置存放位置、preset 升级。结论会并入 docs/design-tools.md 并落一个前置切片 T0。
> 背景文档:docs/technology-decisions.md 第 5 节(provider 路由)、
> docs/design.md 第 4 节(模型选择器)、docs/design-tools.md(工具调用)。

## 0. 给评审的一句话

我们要给 provider profile 下的每个模型加"可调参数"(`max_tool_loops`、
`context_window`、`capabilities` 等),preset 负责把合理默认烤进去让用户基本
不用手填。**已经收敛的不必再议;请重点拍下面第 4 节的四个开放问题。**

## 1. 现状

- **存储**:SQLite `provider_profiles { name, type, base_url, api_key,
  default_model, models[]string, extra(JSON) }`。`models` 只是模型 id 字符串列表,
  **没有 per-model 参数**。
- **preset**(`web/src/provider/presets.ts`):选厂商 → 预填 `baseURL` +
  `defaultModel` + `models[]string`。粘 key 即可建 profile。
- **provider 实现**:三家 + mock;`max_tokens` 之类目前**硬编码在 provider 包**
  (如 anthropic.go 写死 64000),不可配。

## 2. 老项目怎么做的(参考,不照搬)

YAML 配置,三层嵌套,每个 `ProfileModel` 带:
- `context_window`、`context_policy`(8 字段裁剪预算)、`capabilities{image,audio}`(三态 `*bool`)
- per-provider 子块:`openai{temperature,max_completion_tokens,max_tool_loops,reasoning_effort,...}`、
  `google{thinking{include,level},...}`、`anthropic{temperature,max_tokens,top_p,top_k,anthropic_version}`

preset 的 `buildPatch()` 产出**完整带参数的 models 列表**(DeepSeek 预设直接给
`max_tool_loops:64 / temperature:0.7 / context_window:1.05M / capabilities`)。

**教训**:语义模型(per-model 调参 + 三态能力 + preset 烤默认)有价值;
YAML 存储 + 保序重写的 save.go + 8 字段 context_policy + live/composed mode
是过度工程,不搬。

## 3. 已收敛(不再议)

1. **配置留 SQLite,UI 可写(CRUD)**。这是产品,profile/model 编辑是必需能力。
   "DB 增加手工调整复杂度"的顾虑由 **preset** 解决——常见场景选预设+粘 key,
   不手编。配置只读手编(文件)方向**否掉**。
2. **per-model 参数要有**,但由 preset 兜默认,不逼用户填。
3. **preset 升级**:从填 `model id 字符串` 升级为填**完整 ModelEntry**。
4. **设置表单两档**:基础(选 preset + 粘 key)/ 高级(per-model 微调)。
5. **保留老 preset UX**:`apiKeyUrl`(前往获取链接)、`apiKeyOptional`(Ollama);
   `variants(live/composed)` 是 audio 概念,跳过。
6. **时机**:`max_tool_loops` 本就是工具调用参数,schema 富化 + preset 升级
   作为前置切片 **T0**,在工具调用 T1 之前(pre-launch 改 schema 便宜)。

## 4. 开放问题(请评审拍板)

### Q1. ModelEntry 的 params:扁平中立集 vs per-provider 子块?

```jsonc
// 方案 A:扁平中立集(provider 层各取所需,不认识的忽略)
{ "id": "deepseek-v4-flash", "label": "DeepSeek V4 Flash",
  "contextWindow": 1050000,
  "capabilities": { "image": false, "audio": false },
  "params": { "maxOutputTokens": 8192, "maxToolLoops": 64,
              "temperature": 0.7, "thinking": "off" } }

// 方案 B:per-provider 子块(老项目路线,typed 但绑 provider)
{ "id": "...", "contextWindow": ...,
  "openai": { "temperature": 0.7, "maxCompletionTokens": 8192, "maxToolLoops": 64 } }
```

- A 优点:provider-neutral,canonical/contextbuilder 不必认 provider;一个
  `thinking: off|low|high` 由各 provider 翻成自己的字段。缺点:中立抽象会丢失
  provider 独有旋钮(google voice_name、anthropic_version)。
- B 优点:精确表达每家差异、可校验。缺点:绑 provider type,加一家要动 schema;
  与 canonical 中立原则不一致。
- **我的倾向:A**,加一个 `extra: {}`(per-model JSON 逃生口)兜 provider 独有旋钮。
  中立集覆盖 90%,逃生口接 10% 长尾,不为长尾绑 type。**请评审权衡。**

### Q2. 现在(text + tools 阶段)暴露哪些 params?

候选:`maxOutputTokens`、`maxToolLoops`、`contextWindow`、`capabilities`、
`temperature`、`thinking`。

- 我倾向**先只上工具/文本阶段真用得到的**:`maxOutputTokens`、`maxToolLoops`、
  `contextWindow`(compaction 预留)、`capabilities`(多模态预留可先放着)。
  `thinking`、`temperature` 见 Q3。**请评审定最小集。**

### Q3. temperature 这类"老有、新模型开始拒收"的参数怎么办?

事实:最新 Anthropic(Opus 4.7+)**拒收** temperature/top_p/top_k 与 budget_tokens;
部分 OpenAI reasoning 模型 `reasoning_effort` 才吃、temperature 被忽略。

- 选项:(a) 完全不暴露 temperature,简化;(b) 暴露但 preset 只给该模型接受的值,
  provider 层对拒收参数兜底丢弃;(c) 放进 Q1 的 per-model `extra` 逃生口,默认不填。
- **我的倾向:(b)+(c)**——中立集不含 temperature(避免误导),需要的人走 extra;
  provider 层统一兜底"丢弃模型拒收的参数,不报 400"。**请评审。**

### Q4. params 在哪消费 + 要不要随 turn 快照?

现有:turn 创建时快照 `provider + model` 进 turns 表(硬约束 15),改配置不影响
进行中的 turn。加了 params 后:

- contextbuilder 消费 `contextWindow`(compaction)、engine 工具循环消费
  `maxToolLoops`、provider 消费 `maxOutputTokens`/`thinking`。
- 问题:params 要不要也快照进 turns?若不快照,turn 跑一半用户改了 `maxToolLoops`
  会半途变；若快照,turns 表加列/加 JSON。
- **我的倾向:params 随 turn 快照**(与 provider/model 同理,turn 内一致)。
  存形式:turns 加一个 `params(JSON)` 列,Submit 时连同 provider/model 一起快照。
  **请评审:快照粒度是否过重,还是只快照 engine 当场要用的几个?**

## 5. 评审请回答

1. Q1:扁平中立集 + extra 逃生口(我倾向),还是 per-provider 子块?
2. Q2:工具/文本阶段的 params 最小集是哪几个?
3. Q3:temperature 丢弃兜底(我倾向)还是别的?
4. Q4:params 随 turn 快照(我倾向)还是不快照 / 部分快照?
5. 有没有遗漏的形状风险(尤其 pre-launch 之后改 schema 会变贵的地方)?

## 6. Codex 评审意见

### 6.1 先给结论

我同意把这件事作为 tools 前置 T0,也同意 preset 升级成完整 `ModelEntry`。
但有两个地方建议不要过早拍死:

1. **不建议现在把"文件配置方向"完全否掉**。DB 可以做运行态存储和 UI CRUD,
   但 provider/profile 是用户会手工调的本地配置,至少要保留无损 export/import,
   或者后续切到 file-first 仍不破坏 schema。
2. **不建议用"扁平中立集 + extra"承载主要 provider 差异**。`extra` 适合兜底,
   不适合变成高级配置的事实来源;否则 UI 校验、preset diff、provider 兼容性都会变弱。

### 6.2 对 Q1:ModelEntry 形状

建议用"中立核心 + provider typed options"的混合结构:

```jsonc
{
  "id": "deepseek-v4-flash",
  "label": "DeepSeek V4 Flash",
  "contextWindow": 1050000,
  "capabilities": { "image": false, "audio": false },
  "limits": {
    "maxOutputTokens": 8192,
    "maxToolLoops": 64
  },
  "providerOptions": {
    "openai": {
      "temperature": 0.7,
      "reasoningEffort": "medium",
      "enableThinking": false
    }
  },
  "extra": {}
}
```

原则:

- `contextWindow`、`capabilities`、`limits.maxOutputTokens`、`limits.maxToolLoops`
  是跨 provider 的一等字段。
- `temperature`、`thinking`、`reasoningEffort`、`anthropicVersion`、
  `googleThinking` 这类放 `providerOptions.<type>`。
- `extra` 只做临时逃生口,不能作为 preset 的主路径字段。

理由:canonical/contextbuilder 只读中立核心;provider 读自己的 typed options。
这样不会把不同厂商的语义硬压成一个假中立字段,也不会回到老项目那种大 Runtime。

### 6.3 对 Q2:文本 + tools 阶段最小集

T0 最小集建议:

- `id`
- `label`
- `contextWindow`
- `capabilities`
- `limits.maxOutputTokens`
- `limits.maxToolLoops`
- `providerOptions`

`temperature` 不进中立核心。需要时只进入对应 provider options。

### 6.4 对 Q3:拒收参数怎么处理

不建议静默丢弃。

建议 provider 做三层处理:

1. preset 默认不填高风险参数。
2. provider adapter 只发送当前 provider/model 明确允许的参数。
3. 被过滤的参数写 debug log 或 diagnostic event,不要直接 400,但也不要完全无声。

这样用户高级配置写错时能查到原因,不会误以为参数生效了。

### 6.5 对 Q4:turn 快照

同意快照,但快照的应该是**resolved effective model config**,不是整份 profile。

建议 turns 表加 JSON 列,例如 `model_config_json`:

```jsonc
{
  "profile": "openai-main",
  "providerType": "openai",
  "model": "gpt-5.4",
  "contextWindow": 128000,
  "limits": {
    "maxOutputTokens": 8192,
    "maxToolLoops": 32
  },
  "providerOptions": {
    "openai": {
      "reasoningEffort": "medium"
    }
  }
}
```

不要快照 API key。turn 内所有 engine/provider/contextbuilder 决策只读这份 resolved config,
避免用户中途改 profile 导致 turn 行为漂移。

### 6.6 preset 与 profile 的边界

建议把 preset 定义成"模板/catalog",不是用户配置本身:

- bundled preset 版本化,可随 app 更新。
- 用户点击 preset 后生成自己的 provider profile。
- 生成后的 profile 不被 preset 自动覆盖。
- 后续可以做"从 preset 刷新/对比 diff",但必须显式确认。

这能保留老项目 preset 的价值,同时避免 app 升级悄悄改用户配置。

### 6.7 存储建议

短期可以继续 SQLite,但 schema 要按"可导出配置"设计:

- DB 存 user profiles 和 sessions 引用。
- preset catalog 不进 DB,放代码/资源文件。
- 提供无损 export/import 或后续 file-first 的迁移空间。
- API key 后续应能迁到 Keychain/native secret,不要和 preset/model schema 绑定死。

如果必须现在拍板,我的偏好是:

> SQLite 作为产品内编辑与运行态存储;profile schema 必须保持可序列化、可导入导出,
> 不把手工编辑能力押死在 sqlite3 里。

### 6.8 建议拍板版

1. Q1:采用"中立核心 + provider typed options + extra 兜底"。
2. Q2:T0 上 `contextWindow/capabilities/maxOutputTokens/maxToolLoops/providerOptions`。
3. Q3:`temperature` 不进中立核心;provider 过滤拒收参数并留下诊断。
4. Q4:turn 快照 resolved effective model config,不快照 secret。
5. storage:SQLite 可继续,但必须保留无损 export/import/file-first 迁移余地。
