# 轨道 E3:Provider 管理 UI + 设计底座落地

> 背景:后端 provider registry 已交付——provider profile 是命名的 LLM 端点
> 实例(`/providers` CRUD),session 通过 `provider + model` 二元组选择 LLM,
> 提交时刻快照。本轨道交付对应 UI,并且是 **docs/design.md 设计底座的首个
> 实施轨道**:所有新界面按底座实现,顺手把既有界面迁到 token 上。
> 通用纪律见 docs/phase-1-plan.md 第 6 节;硬约束见 AGENTS.md。

## 范围

1. **设计底座落地**(先做,后续条目都长在它上面)
   - 按 docs/design.md 第 2 节更新 `web/src/styles.css` 的亮暗双套 token;
   - transcript 改版:assistant 消息去气泡(无边框无底色直接排版),
     用户气泡换 `--primary`;hover 显示时间戳 + 复制按钮;
   - 空态 / 加载态按第 4 节规范改造(session 列表、transcript、settings)。
2. **Provider 管理页**(settings 对话框升级为多页签,或独立对话框)
   - 列表:name / type / baseURL / `apiKeySet`(已设置 ✓ 或 未设置),
     **永远不显示 key 明文**(API 也不会返回);
   - 创建/编辑表单(RHF + zod,契约 schema 直接 import):
     name、type 下拉(`openai-compatible` / `google`)、baseURL、
     apiKey(password 输入,编辑时留空 = 不修改,占位文案说明)、
     删除带确认(复用 alert-dialog);
   - `web/src/provider/presets.ts` 改造:点击 preset 改为**预填创建表单**
     (name/type/baseURL/建议模型),不再写 settings 的 `provider.openai.*` 键;
   - 默认 provider / 默认 model:表单化(下拉 + 输入),写 settings 的
     `provider.default` / `model.default` 键;system_prompt 给独立多行文本域。
3. **session 头部 provider/model 选择器**
   - header 里当前会话的 provider/model 可视、可改(下拉,
     `PATCH /sessions/{id}`);改动只影响后续 turn(后端快照语义,无需特判);
   - session.provider 为空显示"默认(跟随设置)"。

## 契约(只读,不得修改)

- `web/contracts/api.ts`:`providerProfile` / `createProviderRequest` /
  `patchProviderRequest` / `listProvidersResponse` / session 的 `provider` 字段;
- 端点与错误码:docs/contracts-checklist.md(`profile_exists` 409 要给重名提示);
- query key:`["providers"]` 列表、`["providers", name]` 单个。

## 注意

- settings 里的 `provider.openai.*` 是过渡键,**本轨道不要读写它**;
  legacy 回落链由主线在本轨道合并后删除,你不用动 Go 代码。
- 删除 profile 不级联改 session(后端语义):引用它的 session 下次 submit
  会收到 turn.failed,UI 无需额外处理,但删除确认文案要提示
  "正在被 N 个会话使用"可不做(无此 API),提示"引用该 Provider 的会话将无法发送"即可。

## 验收

- Provider CRUD 全流程可在 UI 完成;key 输入后界面只显示"已设置";
  重名创建有明确提示;删除有确认。
- preset 点击 → 表单预填 → 保存 → 列表出现;不再写过渡 settings 键
  (network 面板验证无 `provider.openai.*` 的 PUT)。
- 双 session 指向不同 provider,各自对话互不影响(实测);
  session 头部改 provider 后,下一条消息走新端点。
- docs/design.md 第 7 节 checklist 全过(token、暗色、四态、文案、reduced-motion)。
- `cd web && npm run build` 通过。

分支:`track/web-providers`。
