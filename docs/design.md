# Web UI 设计底座

> 状态:v1,随 E3 生效。  
> 定位:对标 ChatGPT / Claude 网页版的**基础体验水位**,不追求超越;
> local-first 桌面工具感,将来直接进 Wails 壳。  
> 纪律:新组件一律消费本文 token,硬编码色值 / 像素间距是 review 驳回项;
> 暗色与亮色必须同一 PR 交付;每个功能轨道验收自带"底座合规"项。

## 1. 品牌

- 名字是 Pudding:主色定**焦糖琥珀**(caramel),暖、贴名字,
  与满街紫蓝色 AI 产品区分。主色只通过 `--primary` 单点引用,后续想换一处改。
- 个性载体:旧项目 `MascotHint.tsx` 吉祥物可搬,只出现在两处——
  空态插画、streaming 时的小型状态指示。其余界面保持克制,不卖萌。
- logo / 标题:侧栏头部 "Pudding" 文字字标 + 小图标位(占位即可,图标后补)。

## 2. Design tokens

落点:`web/src/styles.css` 的 shadcn CSS 变量(亮暗双套)。
以下为基准值,允许 ±5% 微调,但**语义结构不可变**:

```css
:root {
  --radius: 0.625rem;                      /* 卡片/输入框;气泡 0.75rem;按钮 0.5rem */
  --background: oklch(0.99 0.004 85);      /* 暖白,不用纯白 */
  --foreground: oklch(0.24 0.01 60);
  --primary: oklch(0.66 0.13 65);          /* 焦糖琥珀 */
  --primary-foreground: oklch(0.99 0.01 85);
  --muted-foreground: oklch(0.52 0.02 60);
  --border: oklch(0.91 0.01 80);
}
.dark {
  --background: oklch(0.21 0.01 60);       /* 暖深灰,不用纯黑 */
  --foreground: oklch(0.93 0.01 80);
  --primary: oklch(0.75 0.12 70);          /* 暗色下提亮一档 */
  --primary-foreground: oklch(0.22 0.03 60);
  --border: oklch(0.32 0.01 60);
}
```

- 对比度:正文 ≥ 4.5:1,辅助文字 ≥ 3:1(WCAG AA),亮暗都查。
- 间距:4px 基数,只用 1/2/3/4/5/6/8/10/12 档(Tailwind 默认即是),禁任意值。
- 阴影:三档——`shadow-sm`(气泡/卡片)、`shadow-md`(悬浮按钮/popover)、
  `shadow-lg`(对话框),不再多。
- 字体栈:`"Inter Variable", "PingFang SC", "Noto Sans SC", system-ui, sans-serif`;
  代码 `ui-monospace, "SF Mono", Menlo, monospace`。
- 字号:正文 14px/1.65(中文行高要松),辅助 12px,标题只用 16/18 两档。

## 3. 布局骨架

```text
┌ 侧栏 264px(可折叠)┬ 主区(flex-1)────────────┐
│ 字标 + 新建        │ header 56px:会话名/模型/操作 │
│ 会话列表           │ transcript:内容列 max-w-3xl │
│                   │ composer:与内容列同宽       │
└───────────────────┴─────────────────────────────┘
```

- 消息列与 composer 共用同一 `max-w-3xl mx-auto`,左右呼吸 20px。
- 用户气泡右对齐、`--primary` 底、最大宽 78%;assistant 左对齐、
  无边框无底色(直接排版,markdown 阅读优先),与气泡区分靠头像与对齐。
- 信息密度:session 列表项 = 标题 + 相对时间(次行,muted);
  消息 hover 显示时间戳与复制按钮,常态不显示。

## 4. 状态规范(每个界面四态齐全)

| 态 | 形状 |
| --- | --- |
| 空态 | 居中:mascot/图标 + 一句话 + 一个主操作按钮;禁止裸文案 |
| 加载 | 列表用 skeleton(2-3 行),按钮用内嵌 spinner;禁全屏 spinner |
| 错误 | inline Alert + 具体原因 + 重试动作;toast(sonner)只用于操作回执 |
| streaming | assistant 文末闪烁光标块(▍);Stop 按钮可达 |

## 5. 动效

- 时长三档:120ms(hover/按压)、200ms(展开/淡入)、300ms(对话框/抽屉)。
- ease-out 进、ease-in 出;只做功能性动效(指示状态变化),不做装饰动效。
- 消息进入:新气泡 150ms 淡入上移 4px;delta 追加不做动效(会闪)。
- 尊重 `prefers-reduced-motion`:动效全部降级为瞬时。

## 6. 文案

- 界面文案中文优先(i18n 已就位),短句、不用叹号、不拟人化。
- 错误文案必须含下一步动作:"Provider 未配置,去 设置 → Provider 添加" 
  而不是 "请求失败"。
- 空态文案一句话说清能做什么:"还没有会话,点击右上角开始第一段对话"。

## 7. 底座合规 checklist(轨道验收用)

- [ ] 无硬编码色值 / 任意间距值,全部走 token
- [ ] 暗色模式逐界面检查,无白底漏光、对比度达标
- [ ] 空态 / 加载 / 错误 / streaming 四态齐全
- [ ] 中英文案齐全,无 key 裸奔
- [ ] 动效尊重 reduced-motion
