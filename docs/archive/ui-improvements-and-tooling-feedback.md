# UI 改进与工具体验反馈

> 状态:历史体验反馈快照。事项可能已实现或被后续设计取代,不作为当前问题清单。

**日期**: 2026-07-21  
**作者**: AI Assistant  
**范围**: Web UI 组件优化、架构改进建议、开发工具体验反馈

---

## 目录

1. [已修复的问题](#已修复的问题)
2. [架构层面的问题](#架构层面的问题)
3. [开发工具体验反馈](#开发工具体验反馈)
4. [优先级建议](#优先级建议)

---

## 已修复的问题

本次任务中修复了以下 8 个 UI 相关问题：

### 1. 侧边栏自动关闭逻辑

**问题**: 关闭右侧侧边栏最后一个窗口时，整个侧边栏不会自动关闭  
**根因**: 缺少资源清空检测机制  
**修复**: 在 `WorkspacePane` 中添加 `totalResourceCount` 和 `hadResourcesRef` 监听，当所有资源（浏览器标签、终端、画布项、文件预览、项目标签）都被关闭时自动关闭侧边栏

### 2. Git Diff 显示优化

**问题**: Git diff 视图显示所有未改动的行，信息密度低  
**根因**: `showDiffOnly={false}` 配置  
**修复**:

- 改为 `showDiffOnly={true}`，默认只显示变更区域
- 添加展开/收起按钮，允许用户切换完整视图
- 添加 i18n 支持（中文、繁体中文、英文）

### 3. 输入框打字卡顿

**问题**: 在输入框打字时出现明显卡顿  
**根因**: `form.watch("text")` 每次按键触发整个 `Composer` 组件（~1400 行）重渲染  
**修复**:

- 拆分出 `ComposerTextArea` 子组件
- 在子组件内部使用 `useWatch` 订阅文本变化
- 将 mention、slash command、textarea 事件处理等逻辑移入子组件
- 父组件通过回调接收状态更新

### 4. Enter 键提交失效

**问题**: 重构后按 Enter 键无法提交消息  
**根因**: `sendEnabled` 依赖异步回调更新的 state（`canSend`、`mentionMenuOpen`、`slashMenuOpen`），按键时这些值还是初始值  
**修复**: `onEnter` 回调传递子组件实时计算的值，避免依赖父组件的异步状态

### 5. 函数未导出错误

**问题**: `parseSlashSubmitCommand is not defined` 运行时错误  
**根因**: 函数从 `Composer.tsx` 移到 `ComposerTextArea.tsx` 但未导出  
**修复**: 添加 `export` 关键字，并在 `Composer.tsx` 中导入

### 6. 附件输入处理丢失

**问题**: 无法通过文件选择器添加附件  
**根因**: 重构时误删 `handleAttachmentInputChange` 函数  
**修复**: 重新添加函数定义

### 7. 中文输入法回车误触发

**问题**: 使用中文输入法时，按回车确认候选词会直接触发 session 改名  
**根因**: 未检查 `event.nativeEvent.isComposing`，macOS 上 `compositionend` 先于 `keydown` 触发  
**修复**: 在 `SessionRail.tsx` 和 `ChatPane.tsx` 的改名输入框中添加 `!event.nativeEvent.isComposing` 检查

### 8. 模型选择器滚动问题

**问题**: 模型选择器列表过长时无法滚动查看  
**根因**: `max-h-56` 高度限制导致内容被截断  
**修复**: 移除固定高度限制，让列表自然展开

---

## 架构层面的问题

### 1. IME 兼容性不统一

**现状**:

- 项目有 `useImeCompositionGuard` hook（带 30ms grace period）
- 但很多地方只用了裸的 `event.nativeEvent.isComposing` 检查
- macOS 上 `compositionend` 事件先于 `keydown` 触发，导致检查失效

**影响文件**:

- `ChatPane.tsx`（toolbar 改名）
- `SessionRail.tsx`（内联改名 + Dialog 改名）
- `ProjectSearch.tsx`
- `VoiceSettings.tsx`
- `GeneralSettings.tsx`

**建议**:
全局搜索所有 `event.key === "Enter"` 的输入框，统一使用 `useImeCompositionGuard` hook 而不是裸的 `isComposing` 检查。

### 2. Composer 组件过大

**现状**:

- 单个组件 ~1400 行代码
- 承载了表单、附件、mention、slash command、mutation、mascot 等所有逻辑
- 虽然已拆出 `ComposerTextArea`，但 `Composer` 仍然很重

**建议继续拆分**:

- `ComposerAttachments` — 附件/文件夹/project reference 的 chip 展示
- `ComposerToolbar` — 底部工具栏（add button、model picker、send button）
- `ComposerApprovalBar` 已经是独立组件，这个模式可以推广

### 5. 缺少编译检查的 CI 习惯

**现状**:

- 多次出现函数丢失、未导出等编译错误
- 都是 `tsc --noEmit` 能立即发现的

**建议**:
开发流程中每次改动后跑一次 `npx tsc --noEmit`，或者配置 IDE 实时 TypeScript 检查。

---

## 开发工具体验反馈

### 好的方面

✅ **CLI 命令**（grep、sed、cat、find）很稳定，速度快，沙箱隔离让人放心  
✅ **TypeScript 编译检查**（`npx tsc --noEmit`）响应快，能即时发现问题  
✅ **文件创建**（`builtin_file_write`）一次成功，没有坑  
✅ **项目检查**（`builtin_project_inspect`）能快速了解项目结构

### 痛点

#### 1. `builtin_file_patch` 的 `old_string` 匹配太脆弱

**问题**:

- 多次出现 `old_string was not found`
- 原因是 `cat` 输出的文本和文件实际内容有细微差异（编码、空白字符）
- 每次都要重新 `sed` 精确读取才能匹配成功
- 匹配失败时没有提示"最接近的匹配是什么"，只能盲猜

**建议**:
匹配失败时返回文件中相似行的行号和内容，帮助定位。

#### 2. `builtin_patch_propose` 大文件 patch 反复报 JSON 错误

**问题**:

- 多次出现 `patch proposal arguments must be a JSON object`，即使传的是合法 JSON
- 大重构时被迫拆成十几个小 `builtin_file_patch` 调用，效率很低

**建议**:
改进 JSON 解析逻辑，或者提供更清晰的错误信息说明哪里格式不对。

#### 3. `builtin_file_read` 对 .ts 文件报 `binary_file`

**问题**:

- `panelLayout.ts`、`canvasRevealStore.ts` 等正常 TypeScript 文件被识别为 `video/mp2t`
- 原因是 `.ts` 扩展名被当成 MPEG Transport Stream
- 只能 fallback 到 `cat`

**建议**:
根据文件内容而非扩展名判断 MIME type，或者对 `web/src/` 下的 `.ts` 文件强制按文本处理。

#### 4. 可选工具曾经需要每个 turn 重新加载（已解决）

**问题**:

- 旧的结构化文件能力每个 turn 只活一次
- 频繁重构时需要反复加载，增加了不必要的步骤

**建议**:
已统一迁移为 session 级 App；加载一次后在会话内持续可用，并可从 Apps 页面关闭。

#### 5. 管道操作不支持

**问题**:

- `grep ... | head -5` 这种基本操作在 `argv` 模式下不工作（`|` 被当成文件名）
- 必须用 `script` 模式，但 `script` 模式又标记为 risky 需要审批

**建议**:
对常见的安全管道模式（`| head`、`| tail`、`| wc`）在 `argv` 模式下做特殊处理。

#### 6. 重构后缺少"影响分析"能力

**问题**:

- 这次删了 `handleAttachmentInputChange`、`parseSlashSubmitCommand` 没导出
- 都是因为无法快速知道哪些地方引用了这些函数
- 如果有 LSP 的 "find references" 能力，重构会安全很多

**建议**:
Code Intelligence App 的 references 功能应该在重构场景中更主动地使用。

---

## 优先级建议

### P1 - 重要（提升可维护性）

3. **Composer 继续拆分**
   - 当前 ~1400 行，难以维护
   - 拆分为 `ComposerAttachments`、`ComposerToolbar` 等
   - 工作量：大

### P2 - 改进（提升开发效率）

5. **开发工具改进**
   - `builtin_file_patch` 匹配失败提示优化
   - `builtin_patch_propose` JSON 解析改进
   - `.ts` 文件 MIME type 识别修复
   - 工作量：中等（需要修改工具代码）

6. **CI 编译检查**
   - 添加 `tsc --noEmit` 到 CI 流程
   - 或者配置 pre-commit hook
   - 工作量：小
