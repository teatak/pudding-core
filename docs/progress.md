# 开发进度

> 单一现状文档:给接手的 agent / 上下文压缩后的自己看,避免重新推导。
> 已完成只记结论(细节在 git 历史与 commit message);重点是**进行中**与
> **待决项**。改动时同步更新本文,不积压。
> 最近更新:2026-06-13。

## 现状一句话

local-first 多 session AI daemon + 桌面壳,端到端可用:多会话文本对话
(SQLite 持久化/恢复/幂等/cancel/SSE 续传)、四 provider(openai-compatible /
google / anthropic / 经 registry 按 profile 路由)、agent shell UI、单二进制
(embed web + token 握手)、Wails 桌面壳、LLM 自动标题。

## 已完成(只记结论)

**后端**
- store:SQLite 单 writer,sessions/messages/turns/events/settings/provider_profiles;
  schema 直改无迁移(pre-launch)。
- engine:per-session turn 状态机(begin/finish/recover、幂等、并发 409、
  cancel 保留 partial);provider/model 提交时刻快照进 turns。
- provider:registry 按 profile 路由 + 指纹缓存;四实现
  openai-compatible(手写 SSE)/ google(Gemini 原生)/ anthropic(Messages API)/ mock。
  各家流解析有固化帧单测。provider 只产模型流,turn lifecycle 归 engine。
- 自动标题:空标题会话首条消息后 engine 写 provisional + 异步裸 LLM 调用
  生成正式标题,手动改名优先;session.titled 事件(不落库)推前端刷新。
  见 internal/engine/titler.go。
- 事件协议:per-session seq、SSE tail 语义、Last-Event-ID 续传、events
  retention 1000/session。
- daemon:internal/daemon 可嵌入启动包;单端口/通道(release 9669 / dev 9679),
  CLI 与桌面壳 attach-or-start 共用(见 home.DefaultAddr,无 AutoPort)。

**桌面壳(cmd/pudding-desktop)**
- Wails v3 alpha;HiddenInset 标题栏 + 红绿灯 inset 让位 + 双击 zoom +
  全屏检测,解法移植自老项目 chrome_darwin.go。
- 壳与页面无 ExecJS/window.wails 桥(loopback 跨 origin),只走 native cgo
  与 web 视口启发式(见 memory: wails-no-runtime-bridge)。

**Web UI(design.md v2 全部切片 S1–S8 + E4)**
- 中性表面 + indigo 强调 tokens(sRGB hex,规避 WKWebView 宽色域伪影);
  中性 focus ring。
- 可折叠 rail(hover popover)、composer 内两层模型选择器 + 品牌图标、
  parts 任务流、单行 header 运行态、响应式。
- 上下分屏(?split=,双 pane 独立 SSE,比例可拖拽)、canvas 栏插槽(S7,
  空态占位)、rail/canvas 可拖拽调宽。
- 手动重命名、复制绿对勾反馈、markdown 表格通栏、消息入场动效。

## 进行中

### 工具调用 / MCP —— 契约设计待评审(docs/design-tools.md)

设计草案已写,**未提交、未开工**,卡在三个待用户拍板的决策:

1. **canonical 落库形状**:一 turn 一条 assistant 消息 + `parts` 数组
   (thought/tool_use/tool_result/text 按时间序),而非 OpenAI/Anthropic
   的多消息交替格式。地基决策,定了难改。倾向:一 turn 一条。
2. **MCP 依赖**:用官方 `modelcontextprotocol/go-sdk`(引入新依赖)还是先
   只做内置工具、MCP 后置。倾向:先内置工具(web_fetch)端到端跑通,MCP 紧随。
3. **thought 落不落库**:当前设计不落库(只走事件实时显示,同 delta 级)。
   备选:落进 parts 永久保留。倾向:不落库。

切片 T1–T6 见 design-tools.md 第 8 节;T1–T4 是最小垂直切片
(内置 web_fetch 即可端到端演示工具调用)。

## 队列(未开工,按优先级)

1. **工具调用 / MCP**(进行中,见上)——agent 工作台核心,所有铺垫已就位。
2. **compaction**:contextbuilder 现在全量发送 messages,长会话迟早爆 context。
   独立于工具调用;老项目有 `/compact` 摘要先例可移植。
3. **桌面正式打包**:临时 .app bundle 已验证形态(/tmp/Pudding.app),正式化
   = Makefile 出 bundle + Info.plist + icon(签名公证后置)。小工程,适合
   subagent;**注意 subagent 必须显式指定 opus(默认 fable 不可用)**。
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
- 踩坑记忆:`~/.claude/.../memory/`(wails 桥、provider 路由、pre-launch 等)。
- **老项目 `pudding-core-old` 只读参考**(标题生成、桌面 chrome、compact 等
  有先例),不照搬 Runtime 结构,不修改它。
