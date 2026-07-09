# 开发进度

> 单一现状文档:给接手的 agent / 上下文压缩后的自己看,避免重新推导。
> 已完成只记结论(细节在 git 历史与 commit message);重点是**进行中**与
> **待决项**。改动时同步更新本文,不积压。
> 最近更新:2026-07-09。

## 现状一句话

local-first 多 session AI daemon + 桌面壳,端到端可用:多会话文本对话
(SQLite 持久化/恢复/幂等/cancel/SSE 续传)、四类真实 provider
(openai-responses / openai-compatible / google / anthropic)+ mock,经 registry 按 profile
路由、agent shell UI、Electron 桌面壳 + Go daemon(token 握手)、
LLM 自动标题。

## 已完成(只记结论)

**后端**
- store/config:SQLite 单 writer 只管 sessions/messages/turns/events;
  provider profiles 与模型元数据事实源为 `<home>/config/profiles.yaml`,
  web tools 配置事实源为 `<home>/config/web.yaml`,settings 标量事实源为
  `<home>/config/settings.yaml`;主对话提示词由 `internal/prompt` 组装,
  用户补充读取 `<home>/pudding.md`。
- engine:per-session turn 状态机(begin/finish/recover、幂等、并发 409、
  cancel 保留 partial);provider/model/effective model config 提交时刻快照进 turns。
- provider:registry 按 YAML profile 路由 + 指纹缓存;五实现
  openai-responses(Responses API)/ openai-compatible(手写 SSE)/ google(Gemini 原生)/
  anthropic(Messages API)/ mock。
  各家流解析有固化帧单测。provider 只产模型流,turn lifecycle 归 engine。
- 自动标题:空标题会话首条消息后 engine 写 provisional + 异步裸 LLM 调用
  生成正式标题,手动改名优先;session.titled 事件(不落库)推前端刷新。
  见 internal/engine/titler.go。
- 事件协议:per-session seq、SSE tail 语义、Last-Event-ID 续传、events
  retention 1000/session。
- daemon:internal/daemon 启动包;单端口/通道(release 9669 / dev 9679),
  CLI 与 Electron shell 共用协议;桌面壳启动时端口被占则退出,不 attach 旧实例。

**桌面壳(Electron)**
- Electron shell 负责窗口、Tray/Menu Bar、preload IPC、主题/窗口状态、`<webview>` 浏览器承载。
- 业务 API 直连 daemon HTTP/SSE/WS;desktop native/system capabilities 走 Electron IPC。
- 已定规则:Electron 只托管 UI 资源/Vite HMR,不反代业务 API。
- 浏览器 external/passkey 路线暂停,当前主线只收敛 internal `<webview>` + LLM 工具生命周期。

**Web UI(design.md v2 切片 S1–S8 + E4)** —— 已实现并经 preview / 真实桌面
窗口验证:
- 中性表面 + indigo 强调 tokens(sRGB hex,规避 WKWebView 宽色域伪影);
  中性 focus ring。
- 可折叠 rail(hover popover,收起不误弹)、composer 内两层模型选择器
  (只读 profile.models)+ 品牌图标、单行 header 运行态、响应式自动折叠。
- 上下分屏(?split=,双 pane 独立 SSE,比例可拖拽)、rail/canvas 可拖拽调宽。
- 手动重命名(铅笔弹框)、复制绿对勾反馈、markdown 表格通栏、消息入场动效。
- 桌面 chrome:红绿灯对齐 / 双击 zoom / 全屏 inset / 拖拽区(真窗口验过)。

**骨架就位、待内容**(切片交付了插槽/渲染位,内容随对应能力解封):
- canvas 栏(S7):布局插槽 + 开合 + 空态占位,**无内容**(等 canvas/widgets)。
- parts 任务流(S4):text part 渲染完成;**thought / tool part 渲染是
  switch 占位**,等工具调用落地(见进行中)。
- header 步数进度(S5):`estimatedSteps / turn.progress` 协议预留,
  **进度条未渲染**,当前只有状态点。

## 进行中

### 工具调用 / MCP —— T2 待开工(docs/design-tools.md)

设计草案已入库,T1 provider 协议扩展已落地。地基决策已拍板:

1. **canonical 形状 = 已定**:turn 是分页/事务边界,一个 turn 可包含多条
   canonical messages。每条 assistant/tool message 通常承载一个语义
   `part`;`role` ∈ `user|assistant|tool|summary`;`kind/turn_index` 用于
   定位、压缩和 turn 内排序。event log 只做旁路审计,不反推 canonical。
2. **thought 落库 = 已定:落 canonical**(供历史回看)。两条边界:
   contextbuilder 跨 turn 组装时剥离 thought(provider 不要陈旧推理);
   turn 内工具循环的 reasoning replay 走工作态、不读 canonical(带 provider
   专属签名)。见 design-tools 第 1 节。
3. **MCP 依赖 = 待定**:先只做内置工具(web_fetch)端到端跑通、MCP 后置
   接 `modelcontextprotocol/go-sdk`,还是一上来就接。倾向:先内置后 MCP。

切片 T1–T6 见 design-tools.md 第 8 节;T1 已完成:
`Request.Tools/Message.Parts/Chunk.Part/Chunk.Tool/Chunk.Finish` + 四家
provider 的 text/thought/tool_use 解析测试。下一步 T2:messages.parts 列
与 contextbuilder 双向翻译。T1–T4 是最小垂直切片(内置 web_fetch 即可端到端
演示工具调用)。

## 队列(未开工,按优先级)

1. **工具调用 / MCP**(进行中,见上)——agent 工作台核心,所有铺垫已就位。
2. **compaction**:contextbuilder 现在全量发送 messages,长会话迟早爆 context。
   独立于工具调用;老项目有 `/compact` 摘要先例可移植。
3. **桌面正式打包**:基础 `make desktop-bundle` 已补 macOS `.app`
   生成入口(Info.plist/icon/release daemon/pudding:// scheme),Electron Tray
   已补显示/隐藏/退出入口;签名、公证、自动更新和正式 crash/log 收集后置。
4. **mascot 交互动画重做**:空态静态 mascot 已上;老项目 MascotHint.tsx
   有 680 行状态机(眨眼/打字/说话),需按新架构裁剪移植。

## 待用户处理

- 两把 API key(DeepSeek / Gemini)在会话历史出现过,建议轮换。
- gemini-3.5-flash 上游容量恢复后在 UI 发一条确认。

## 关键约定索引

- 硬约束:AGENTS.md(编号稳定,PR 引用)。
- 决策定形:docs/technology-decisions.md。
- 字段对照:docs/contracts-checklist.md(改契约必查)。
- 设计底座:docs/design.md(新组件消费其 token)。
- 踩坑记忆:`~/.claude/.../memory/`(desktop bridge、provider 路由、pre-launch 等)。
- **老项目 `pudding-core-old` 只读参考**(标题生成、桌面 chrome、compact 等
  有先例),不照搬 Runtime 结构,不修改它。
