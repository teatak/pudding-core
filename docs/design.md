# Web UI 设计底座 v2 —— Agent Shell

> 状态:v2,经设计稿评审定稿(2026-06-12)。v1 的"聊天气泡"形态废弃。  
> 定位:**现代 agent 工作台**,不是聊天室。UI 的基本单位是"工作过程"
> (思考 / 工具调用 / 产出 / 长任务运行),不是消息气泡。  
> 纪律:新组件一律消费本文 token,硬编码色值 / 任意间距是 review 驳回项;
> 亮暗双套同一 PR 交付;每个轨道验收自带第 9 节 checklist。

## 1. 色调:冷中性 + 焦糖强调

整体转**冷色调**:中性色 hue 250–255(蓝灰系);焦糖琥珀只作为品牌强调色
(运行态、主按钮、用户消息条),在冷底上更突出。

```css
:root {
  --radius: 0.625rem;
  --background: oklch(0.985 0.003 250);   /* 冷白 */
  --foreground: oklch(0.24 0.012 255);
  --primary: oklch(0.66 0.13 65);          /* 焦糖,品牌强调 */
  --primary-foreground: oklch(0.99 0.01 85);
  --muted-foreground: oklch(0.50 0.015 255);
  --border: oklch(0.91 0.008 250);
  --sidebar: oklch(0.955 0.005 250);
}
.dark {
  --background: oklch(0.21 0.012 255);     /* 冷深灰 */
  --foreground: oklch(0.93 0.008 250);
  --primary: oklch(0.75 0.12 70);
  --primary-foreground: oklch(0.22 0.03 60);
  --border: oklch(0.30 0.01 255);
  --sidebar: oklch(0.165 0.012 255);
}
```

- 状态色:running = `--primary`(琥珀脉冲),done = teal/green 系,
  failed = destructive;运行态是品牌强调色的主要使用场景。
- 对比度 WCAG AA(正文 4.5:1,辅助 3:1),亮暗都查。
- 字体:`"Inter Variable", "PingFang SC", "Noto Sans SC", system-ui`;
  正文 14px/1.65,辅助 12px,标题 16/18 两档;代码用 mono 栈。

## 2. Shell 骨架:可收起 rail + 会话区(可分屏) + canvas 栏

```text
┌─[rail 268px,可收起]─┬─ 会话区(1–2 个 pane,上下分屏)─┬─[canvas 栏,默认收起]─┐
│ 新建会话             │ pane = header + 任务流 + composer │ 将来:小组件/产物/  │
│ 会话列表(带运行态)  │                                   │ 工具大块输出        │
│ …                   │ ──────── 分屏分隔条 ────────      │                     │
│ 脚部:主题/语言/设置 │ pane 2(可选)                     │                     │
└─────────────────────┴───────────────────────────────────┴─────────────────────┘
```

### 2.1 rail(会话栏)

- **不放 "Pudding" 字标**,品牌走窗口标题/图标。
- 顶部第一行:折叠按钮 + 新建会话。
- 列表项两行:第一行 状态点 + 标题;第二行 状态文案 · 相对时间(muted)。
  - 状态点:琥珀脉冲 = 正在生成;teal 实心 = 近期完成(自上次查看后,本地态);
    无点 = 空闲。
- **可收起**:收起后整栏缩为窄条(仅图标);hover 折叠图标时以
  **popover 浮出完整面板**(参照 Claude Code 桌面端):新建、列表、脚部入口。
  点击列表项即跳转并保持收起。展开/收起状态存 localStorage(UI 偏好)。
- 脚部:主题 / 语言 / 设置(应用级配置只住这里)。

### 2.2 会话区与分屏

- 默认单 pane;支持**上下分屏**同时显示两个会话(旧项目已有先例,
  也是"无 focus 多会话"架构的正面展示:两个 pane 各自独立 SSE、独立 composer,
  可同时 streaming)。
- 每个 pane 自带完整三件套:header / 任务流 / composer,组件完全复用,
  pane 只是容器。
- 路由形状:`?session=A&split=B`(都在 URL,刷新/分享可还原);
  关闭分屏即去掉 `split`。
- 分隔条可拖动调整比例(初版可固定 50/50,拖动后续)。
- 左右分屏、更多 pane 不在本版范围,但 pane 容器抽象要支持方向参数。

### 2.3 macOS 窗口 chrome(红绿灯 inset 与拖拽区)

桌面壳的窗口用 **HiddenInset 标题栏**(无系统标题栏,红绿灯悬浮在内容上),
应用顶部一行自兼标题栏职责。规则:

- **inset 变量单点控制**:组件不感知平台,只消费 CSS 变量。

```css
:root { --traffic-inset: 0px; }                 /* 浏览器:无 inset */
:root[data-shell="mac"] { --traffic-inset: 78px; }            /* 红绿灯区宽 */
:root[data-shell="mac"][data-fullscreen] { --traffic-inset: 0px; }  /* 全屏隐藏 */
```

- **占位落点**:rail 展开时,inset 作用于 rail 顶行(折叠/新建按钮右移);
  rail 收起时作用于窄条顶部;inset 变化带 200ms 过渡,
  进出全屏不跳动。
- **运行模式识别**:壳加载 URL 附 `?shell=mac`,前端写入
  `<html data-shell="mac">`(与 token 一样进 sessionStorage,刷新保持);
  浏览器访问无此参数,零 inset。
- **全屏事件**:壳监听 macOS 进/出全屏(Wails window 事件),
  `ExecJS` 切换 `data-fullscreen` 属性——页面不引入 wails runtime JS,
  保持"壳只是浏览器"的边界。
- **拖拽区**:rail 顶行与 pane header 的空白区设 `--wails-draggable: drag`,
  其中的按钮/输入显式 no-drag;浏览器模式下该样式无效且无害。
- Windows/Linux 后续按同机制扩展(`data-shell="win"` 走右上控件,无左上 inset)。

### 2.4 canvas 栏(预留)

- 第三栏,默认收起为窄条 + 图标;是将来 canvas / 小组件 / 工具大块输出 /
  artifacts 的展示位(暂缓清单里的 canvas/widgets 解封时落位)。
- 本版只交付:布局插槽、展开/收起交互、空态占位;不做内容。

## 3. 任务流(代替"聊天气泡")

### 3.1 用户消息

全宽块:左侧 2px `--primary` 细条 + 浅底,**不是聊天气泡**——语义是
"下达的任务指令"。hover 浮现时间戳/复制(块外下方,不占块内空间)。

### 3.2 assistant turn = parts 序列

```text
meta 行:   [model 徽标] [状态] [耗时]          ← 小号 muted
thought:   ▸ 思考过程 · 2.1s                    ← 折叠行,展开显示推理摘要
tool:      [icon] read_file path/to/file ✓      ← 工具调用卡(边框卡片)
text:      markdown 正文(无气泡直接排版)…▍    ← streaming 光标
```

- 渲染器按 **parts 模型**实现:`type Part = thought | tool | text`,
  text-only 阶段只产 text part,但组件结构就位——MCP 工具、thinking 流、
  多模态来了只是新增 part 渲染器,**不改动任务流骨架**。
- 事件协议演进对齐:`turn.delta` 将来带 part 维度(见 technology-decisions
  第 8 节"协议演进预留");旧项目 `thought_delta` / `tool_call_declared`
  是形状先例。
- 失败态:inline alert(含下一步动作文案);中断态:badge。

## 4. 模型选择器:两层 Accordion

- composer 底排的单一触发器,显示当前 `profile · model`。
- 点击展开 popover,内容是 **Accordion**:
  - 第一层 = provider profile(名称 + 类型徽标 + apiKeySet 状态);
  - 展开第二层 = 该 profile 的模型列表(default_model 置顶标注,
    preset 目录 + 当前值合并);
  - **默认展开当前 session 所用 profile**;
  - 顶部固定项"默认(跟随设置)"。
- 选中模型 = 一次 `PATCH /sessions/{id}` 同时写 provider + model。

## 5. header:单行状态 + 进度预留

- **一行**:左 = 标题(truncate);右 = 状态区
  `[● 状态点] 正在生成 · 3/8 [────▰▰▰────细进度条]`。
- 状态文案:空闲(不显示)/ 正在生成 / 已中断 / 失败。
- **步数进度**:协议预留——将来由工具让 LLM 每 turn 预估步数,
  事件带 `estimatedSteps / currentStep`,header 渲染细进度条;
  字段未出现时只显示状态点 + 文案,布局不变。
- canvas 栏开关按钮在 header 最右。

## 6. composer

- 卡片容器 + focus-within 焦糖 ring;placeholder "消息"。
- 底排:左 = [+ 附件(占位 disabled)] [模型选择器(第 4 节)];
  右 = 发送 / 停止(同位切换,500ms 锁定窗口期渲染 disabled)。
- 斜杠命令、@引用是将来的左排扩展,不实现只留位。

## 7. 状态规范(每界面四态齐全)

| 态 | 形状 |
| --- | --- |
| 空态 | 居中 图标/mascot + 一句话 + 主操作;禁止裸文案 |
| 加载 | skeleton(列表 2-3 行);按钮内嵌 spinner;禁全屏 spinner |
| 错误 | inline alert + 原因 + 重试动作;toast 只用于操作回执 |
| streaming | 文末 ▍ 光标 + rail/header 运行态 |

## 8. 动效与文案

- 时长三档 120/200/300ms,ease-out 进 ease-in 出;只做功能性动效;
  消息进入 150ms 淡入上移;delta 追加不动效;尊重 `prefers-reduced-motion`。
- rail 收起/展开 200ms;popover 120ms;分屏切换不做动效(瞬时)。
- 文案:中文优先、短句、无叹号;错误文案必须含下一步动作。

## 9. 底座合规 checklist(轨道验收用)

- [ ] 无硬编码色值 / 任意间距,全部走 token;冷中性 + 焦糖强调
- [ ] 暗色逐界面检查,无漏光、对比度达标
- [ ] 空态 / 加载 / 错误 / streaming 四态齐全
- [ ] 中英文案齐全,无 key 裸奔
- [ ] 动效尊重 reduced-motion
- [ ] 新能力(工具/thinking/canvas)只新增 part/插槽,不改任务流骨架

## 10. 实施切片

| 切片 | 内容 | 依赖 |
| --- | --- | --- |
| S1 后端 | `GET /sessions` 带 `running`;协议演进预留写进 technology-decisions | — |
| S2 tokens | 冷色调双套落 styles.css | — |
| S3 shell | rail 收起 + hover popover、去字标、运行态列表项 | S1 S2 |
| S4 任务流 | parts 渲染模型、用户消息块、header 单行状态 | S2 |
| S5 选择器 | 两层 Accordion 模型选择 | — |
| S6 分屏 | pane 容器抽象、`?split=`、上下分屏 | S3 S4 |
| S7 canvas 栏 | 插槽 + 收起/展开 + 空态 | S3 |
| S8 窗口 chrome | 壳 HiddenInset + `?shell=mac` + 全屏事件 ExecJS + 拖拽区 | S3 |

S1–S5 为一批(核心形态),S6–S7 紧随;细碎(mascot、动效细节、
标题自动生成)在形态稳定后移交 Codex。
