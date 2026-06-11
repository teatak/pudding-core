# 轨道 E2:Web UI 体验收口

> 背景:第一阶段验收已全部通过(docs/phase-1-plan.md),本轨道清理体验欠账。
> 契约、状态边界、禁区与 docs/tasks/web.md 完全一致,此处不重复;改动全部
> 限于 `web/src/`。

## 范围(按优先级)

1. **transcript 自动滚动**
   - streaming 时跟随底部;用户向上滚动后暂停跟随;回到底部恢复;
   - 可选:不在底部时显示"跳到最新"悬浮按钮;
   - 切换 session 时定位到底部。
2. **markdown 渲染**
   - 旧仓库 `../pudding-core-old/web/apps/pudding-web/src/canvas/markdown.ts`
     可整体搬运(自研、无依赖),放 `web/src/lib/markdown.ts`;
   - **安全要求**:渲染走 HTML 时必须确认输入已 escape——用
     `<script>alert(1)</script>`、`<img src=x onerror=alert(1)>` 作为消息内容
     验证不执行;搬运后补这两个用例的单测或手测记录;
   - 只渲染 assistant 消息;user 消息保持纯文本;
   - 代码高亮本轨道不做(后续单独评估 shiki)。
3. **删除 session 加确认对话框**(shadcn `alert-dialog`,官方安装)。
4. **Router typed search 收口**
   - 用 indexRoute 已有的 `validateSearch` + `useSearch` 读取 `session` 参数,
     替换 `App.tsx` 里的 `window.location` 解析与
     `routes/sessionSearch.ts` 的 popstate hack;导航统一走 `navigate()`。
5. **Stop 按钮窗口期显式化**
   - submit 后 500ms 内 Stop 当前是静默忽略点击;改为该窗口内渲染 disabled,
     窗口结束自动恢复,把"暂不可取消"显式传达给用户。
6. **暗黑模式(Light / Dark / System 三态)**
   - 旧仓库 `theme/theme.ts` 可整体搬运(localStorage 持久化 +
     `prefers-color-scheme` 跟随系统);
   - shadcn/Tailwind v4 已是 CSS 变量主题,确认 `styles.css` 的 dark 变量齐全即可;
   - 切换入口放 header(`ThemeToggle`,可搬旧仓库)。
7. **i18n 骨架(中 / 英)**
   - 趁界面字符串还少,先立骨架:旧仓库 `src/i18n/` 系统可搬;
   - 所有面向用户的字符串集中到 `web/src/i18n/`,组件内禁止裸字符串;
   - 默认跟随浏览器语言,手动切换存 localStorage(属 UI 偏好,合规);
   - 本轨道只要求中文、英文两份文案齐全,不做语言包动态加载。

## 验收

- streaming 长回复自动跟随;向上滚动查看历史时不被拽回;回底后恢复跟随。
- markdown:标题 / 列表 / 代码块 / 链接 / 表格正确渲染;两个 XSS 用例不执行。
- 删除 session 必经确认;取消确认无任何请求发出。
- 地址栏 `?session=` 行为与改造前一致:刷新保持、前进后退正常、切换无后端写请求。
- 双击 Send 行为不回归(user/assistant 各 1 条,无 interrupted)。
- 三态主题:手动 Light/Dark 即时生效并持久化;System 态跟随系统切换;
  暗色下 transcript / sidebar / dialog 无白底漏光。
- 语言切换即时生效;中英文案无缺漏(切换后无英文残留/key 裸奔);
  刷新后语言保持。
- `cd web && npm run build` 通过。

分支:`track/web-polish`。
