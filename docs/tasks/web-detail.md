# 轨道 E4:逐页打磨

> 背景:provider 管理(E3)与设计底座已落地。本轨道是 docs/design.md 的
> 逐页执行,纯前端,不改契约、不改 Go 代码。
> 验收基线:docs/design.md 第 7 节 checklist 每条都过。

## 范围(按优先级)

1. **session 标题自动生成**
   - 首条消息提交成功后,若 title 仍为空/Untitled,`PATCH /sessions/{id}`
     title = 首条消息文本截断(~24 字符,去换行);
   - 后续用户手动改名(列表项重命名入口,inline 编辑或对话框)优先级更高,
     自动命名不覆盖手动值。
2. **消息时间戳 + 复制按钮**(design.md 第 3 节)
   - hover 显示相对时间(刚刚 / 5 分钟前 / 昨天 14:02)与复制按钮,常态隐藏;
   - 复制成功 toast 回执(sonner,需 `npx shadcn@latest add sonner`)。
3. **streaming 光标**:assistant streaming 中文末渲染闪烁 ▍(design.md 第 4 节),
   `turn.completed` 后消失;尊重 reduced-motion(降级为常亮)。
4. **session 列表信息密度**:列表项第二行显示相对时间(`updatedAt`);
   选中态左侧 2px `--primary` 指示条。
5. **空态与欢迎页**(design.md 第 4 节)
   - 无 session:居中 mascot/图标 + 一句话 + "开始第一段对话"主按钮;
   - 有 session 无消息:轻量版空态;
   - mascot 可搬旧仓库 `transcript/MascotHint.tsx`(只出现在空态,克制)。
6. **消息进入动效**:新气泡 150ms 淡入上移 4px;delta 追加不动效;
   reduced-motion 全降级。

## 范围外

- 代码高亮(shiki 单独评估)、消息搜索、虚拟滚动、最后消息预览
  (需后端字段,主线另排)。

## 验收

- design.md 第 7 节 checklist 全过(token / 暗色 / 四态 / 文案 / reduced-motion)。
- 新会话发首条消息后标题自动变为消息摘要;手动改名后不被覆盖。
- 双 session 并行、双击幂等、cancel 等既有验收不回归。
- `cd web && npm run build` 通过。

分支:`track/web-detail`。
