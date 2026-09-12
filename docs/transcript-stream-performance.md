# 流式回复卡顿优化与验收

日期：2026-09-12。

## 已确认的原因

在当前源码的隔离 Electron 窗口中，500 行 TypeScript 回复每次更新都会重新执行同步 Shiki 高亮；单次高亮平均约 338ms，CPU profile 总采样时间约 67% 落在 Shiki JavaScript 正则引擎。代码已输出完时，后续思考更新仍触发同一代码块高亮。

同一轮内的工具详情即使折叠，也会参与 React 渲染、结果解析和 JSON 格式化。200 条文件读取记录的场景中，每次思考更新平均渲染约 62ms。按 turn 虚拟化不能避免当前一个 turn 内的这些计算。

## 当前实现

- `MarkdownBody` 和代码块使用 memo，Markdown 元素组件定义保持稳定；不变代码的高亮 DOM 和结果随组件保留。链接和图片的动态参数通过局部 context 传递。
- Shiki 仅在独立 Worker 加载和执行，文件预览也使用同一入口。每次只派发一个高亮任务，组件更新或卸载会删除过期排队任务，并忽略已经执行中的旧结果；显示结果必须与当前代码和语言匹配。删除了主线程同步高亮路径。
- 关闭的 `TranscriptDisclosure` 不挂载详情。嵌套展开状态仍由已有 disclosure 状态持有，关闭父组后再次展开可恢复。工具结果解析和原始 JSON 格式化只在输入变化时重新计算。
- 同一 session、turn、part 的连续 `turn.delta` 在固定 50ms 窗口内合并。工具、引导、终态等其他事件会先刷新缓冲再立即交付；连接出错或关闭时也刷新。canonical messages、seq、取消和对账语义未改动。
- 吉祥物仅在窗口获得焦点、文档可见且自身位于视口内时运行动画和指针跟随。失焦（即使窗口仍露在屏幕上）、隐藏或移出视口时，停用 CSS 动画和过渡、取消点击/摇头动画，并取消未完成的转头动画帧；重新满足条件后恢复。去掉 scale、调整 will-change 和阶梯动画均未测出明显空载收益，未保留这些 CSS 试验。

高亮尚未返回时显示当前纯文本代码，因此快速增长的长代码块可能在暂停或结束后才完成着色。内容不会等高亮完成才显示。

## 性能数据

使用同一台机器、仓库的 Electron 二进制、当前 Vite 开发模式。40 条短历史 turn，每个场景测量 3 秒，每 20ms 尝试追加一个片段；主线程阻塞时定时器会延后。主进程每 120ms 注入键盘字符，并记录 React Profiler、16ms 心跳和 Chromium Event Timing。

| 场景与指标 | 优化前 | 优化后（重复实测范围） |
| --- | ---: | ---: |
| 500 行代码后继续输出：输入最大等待 | 418ms | 4–8ms |
| 同场景：单次 React 渲染均值 | 246ms | 4–5ms |
| 代码之后继续思考：输入最大等待 | 264ms | 1–4ms |
| 200 条折叠工具后继续思考：渲染均值 | 62ms | 1.5–2.1ms |
| 同场景：DOM 节点数 | 8,806 | 393 |
| 持续增长的 500 行代码：输入最大等待 | 未单独测量 | 1–10ms |

长代码与折叠工具的流式测量窗口内未观察到超过 50ms 的主线程长任务。11.5k 字符的复杂 Markdown 仍有约 30–45ms 的单次渲染，重复测试偶见约 50ms 长任务；该场景输入最大等待约 26–31ms。


每个场景发送的 24 个字符均收到。Event Timing 只上报达到采集阈值的事件，“输入等待”指已采集事件从产生到开始处理的时间，不能视为所有按键的整体分布。压力数据不是用户截图当时真实会话的精确复刻。

持续生成长代码仍消耗 Worker CPU；当前整段 Markdown 解析仍在主线程。吉祥物可见时也仍有常驻动画成本。本次消除了已复现的重复高亮和折叠详情阻塞，没有承诺所有会话规模下 CPU 恒低或零长任务。

## 验证

- `npm --prefix web test`：57 项通过，含 delta 合并的固定刷新期限、session/turn/part 边界、工具与引导顺序、终态刷新，以及过期高亮任务与 Worker 错误清理。
- `npm --prefix web run build`：通过；仍有现有的大 chunk 提示。
- `electron/smoke/transcript-stream-smoke.cjs`：六场景全部通过，覆盖真实键盘、Chromium 组合输入、长代码持续增长后的最新高亮、不变代码 DOM 复用、折叠详情按需挂载及嵌套状态恢复；生产构建 Worker 实际执行验证通过。
- `electron/smoke/transcript-disclosure-smoke.cjs`：鼠标、Enter、Space 展开收起和滚动位置回归通过。
- `electron/smoke/transcript-guides-smoke.cjs`：多次引导、附件、完成对账和 canonical 重载回归通过。
- 吉祥物隔离 A/B：移出视口后运行动画从 9 个变为 0，GPU 约 0.02%、renderer 约 0.002%；隐藏窗口停止动画，重新显示后恢复。该夹具使用 64px 场景及与真实主窗口一致的后台节流设置，不代表整应用总 CPU。
- 失焦补充回归 `electron/smoke/mascot-focus-smoke.cjs`：修改前原生窗口失焦但 `document.hidden=false` 时仍有 9 个运行动画；修改后为 0，待执行转头帧为 0。后台切换 idle/thinking/error、后台挂载、点击和转头中途失焦、最小化、隐藏、移出视口及恢复均通过。该补充改动的构建通过；Web 57 项测试串行通过（默认并行首次遇到现有 Vite 缓存目录竞争 `ENOTEMPTY`）。
- `git diff --check`：通过。

复现入口（仅使用临时目录，不连接 daemon 或真实会话）：

```sh
npm --prefix web run build
web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron electron/smoke/transcript-stream-smoke.cjs
web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron electron/smoke/mascot-focus-smoke.cjs
```

脚本输出临时目录位置，包含 `results.json`、各场景 `.cpuprofile` 和长代码截图。脚本也直接加载构建产物中的 Worker，验证生产打包路径可执行。未安装发布包或在用户原有会话内执行回归。
