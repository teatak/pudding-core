# Agent 工具调用问题清单（2026-09-23 会话）

> 归属：core 工具契约、沙箱边界与 agent 观测能力的问题记录；`internal/` 路径相对 core，`web/`、`electron/` 路径相对 `pudding-desktop`。
> 状态：记录型文档，按会话累积；行号、报错串与调用次数对应记录时的会话，不代表当前目录结构或待办状态。
> 断代：本文件是 [`code-agent-tooling-report.md`](code-agent-tooling-report.md)（2026-09-12 样本）之后的又一次同类样本，编号独立，不覆盖前一份结论。
> 视角：本次样本从**工具调用失败**入手——每条都给出原始返回、失败原因（区分平台/契约与使用侧）和可落地建议。

## 0. 样本与方法

一次 Code 模式下的**前端时序排障会话**：目标是定位并修复 `pudding-desktop` 中“打开工作区拖动中间分隔线时会话区跳动 / 贴底内容先溢出再被拉回”的问题。项目有 3 个授权 root（`pudding-core`、`pudding`、`pudding-desktop`）。

实际结果：全程**无法复现 UI**，只能静态阅读源码推理；一次改动方向错误被用户撤销。本次记录的失败集中在**文件编辑入口**（`builtin_file_patch` 连续失败 3 次中的 2 次）、**文件搜索的作用域解析**、以及**沙箱内的探测面**。

数据来源为会话内的工具返回与命令输出；报错串为原文摘录；“原因”一栏区分【事实】（返回体/代码可直接验证）与【推断】（我基于现象的解释）。

## 1. 结论摘要

| # | 调用 | 返回 | 原因归类 | 建议 | 优先级 |
| --- | --- | --- | --- | --- | --- |
| F1 | `builtin_file_search(scope="project", path="web/src")` | `unknown scope` / `path_not_allowed`（2 次） | 契约：多 root 相对路径解析 + 报错不指导 | 报错带合法取值与命中 root；歧义时显式要求 root | P1 |
| F2 | `builtin_file_search(context_lines=14)` | `invalid_context_lines` | 使用侧（忽略上限）+ 工具不 clamp | 数值越界自动 clamp 或回显可用区间 | P2 |
| F3 | `builtin_file_patch`（长 hunk） | `truncated_json` | 契约：入参体积上限不可预知 | 提交前体积预检；支持分片/锚点式 hunk | P1 |
| F4 | `builtin_file_patch`（`docs/README.md`） | `hunk_lines_mismatch` | 契约+使用侧：行号来源不一致（搜索的 context 行不带行号） | 搜索上下文带各自行号；允许锚点式 hunk 或按 `candidateStartLines` 重放 | P1 |
| F5 | `builtin_file_patch`（重发） | `path_required` | 使用侧：漏字段 + 校验回显不足 | 校验失败回显收到的字段名；单文件支持顶层 `path` | P2 |
| F6 | `builtin_command_run`（`ps` / `lsof`） | `Operation not permitted` | 沙箱：探测面 | 提供只读端口/进程查询或写明替代手段 | P2 |
| F7 | `builtin_command_run`（`git`） | 无关 stderr（xcrun/gitignore） | 沙箱：Git 环境未收敛 | 收敛沙箱 Git 环境 | P3 |
| F8 | `builtin_attachment_export` | 只能 `scope="project"` | 契约：缺少中性产物区 | 增加 `temp` 作用域 | P2 |
| G1 | （能力缺口）UI 观测/注入回路 | 无法注入/无法读回 | 能力缺口，直接导致盲改 | dev 壳开放 loopback CDP + `make dev-attach` | P0 |

## 2. 调用失败明细

### 2.1 F1 文件搜索：`scope` 语义与多 root 解析（P1）

调用（要点）：`builtin_file_search(scope="project", path="web/src", query=...)`，项目有 3 个 root。

返回（原文）：

```json
{"detail":"unknown scope","ok":false,"reason":"path_not_allowed"}
```

【事实】同一查询改用绝对路径后成功；**同一会话稍后同样的相对路径 `web/src` 又成功了**，返回体里 `root` 为 `pudding-desktop`。【推断】多 root 下相对路径的“作用域判定”和“路径解析”是两段逻辑，前者先于后者，因此报了 `unknown scope` 而不是“路径歧义”。

问题：报错既不列合法取值，也不说哪个 root 有歧义，也不说路径相对谁解析；`scope` 这个名字在四类工具里含义还不同——文件工具是“文件区”（`app`/`skill`/`temp`/`project`）、`builtin_command_run`/Git 工具是“哪个项目”（只有 `project`）、`builtin_attachment_export` 只接受 `project`（`internal/tool/attachment_export.go:24`）、`builtin_media_read` 里只有 `source="file"` 时才参与定位。

建议：按语义拆名（文件工具用 `area`，命令/Git 用 `cwd` 表达项目归属，attachment/media 用 `source` 或前缀）；失败信息带合法取值与命中的 root；多 root 下相对路径要么要求显式 root，要么直接报 ambiguous 并列出候选。

### 2.2 F2 搜索参数越界（P2）

调用：`context_lines=14`；返回 `invalid_context_lines`，提示合法区间为 0–5。

【事实】该上限在工具描述里已写明，属于我使用侧疏忽。【建议】数值参数越界自动 clamp（或回显区间与本次生效值），省掉一次往返；对高频参数在描述首句给出上限。

### 2.3 F3 补丁入参被截断（P1）

调用：`builtin_file_patch`，一个文件里两个 hunk（约 24 行替换 + 6 行插入），并附带一个 schema 里不存在的额外字段。

返回（原文）：

```json
{"errorKind":"truncated_json","hint":"The arguments appear truncated; resend the complete JSON object, or split a large change into smaller logical batches."}
```

【事实】重发（去掉额外字段、缩短内容）后成功。【推断】入参体积上限对其他工具不可见，我在组装 hunk 时无从预判。

建议：提交前做体积预检并给出“本次 X KB、上限 Y KB”；支持分片/追加式补丁；或支持锚点式 hunk（给唯一文本片段，不给行号），让单次体积显著变小。

### 2.4 F4 补丁行号与来源不一致（P1）

调用：`builtin_file_patch` 修改 `docs/README.md`，hunk 写 `start_line: 37`。

返回（原文要点）：

```json
{"detail":"hunk 1 old_lines do not match at start_line 37: docs/README.md",
 "recovery":{"hunk":1,"candidateStartLines":[36],"fileLineCount":81,"mismatch":{"line":37,"expected":"...","actual":""}}}
```

【事实】目标行实际在 36；`recovery.candidateStartLines` 给了正确候选，重发后成功。【推断】我的行号来自 `builtin_file_search` 的上下文摘要：摘要里只有匹配行带 `line` 元数据（匹配行是 37），上下文行的行号需要自己推算，因此差了一行。工具要求的是“pre-patch 文件的精确行号”，两者来源不一致。

好的部分：这次失败反馈是可操作的（给出候选行号与上下文），但代价仍是一轮往返。

建议：搜索返回的上下文行直接带各自行号（或在摘要里标注行号区间）；patch 支持“唯一文本锚点 + 可选多行上下文”的 hunk，不必强制行号；或允许按 `recovery.candidateStartLines` 一键重放（例如 `use_candidate: 36`）。

### 2.5 F5 重发补丁缺少必填字段（P2）

调用：重发上一个失败补丁时，`files[0]` 里漏了 `path`。

返回（原文）：

```json
{"errorKind":"path_required","expected":"non-empty string","field":"files[0].path"}
```

【事实】补上 `path` 后同一批变更一次成功（原子提交，无部分写入）。【推断】我的两次编辑里 `path` 位于数组元素末尾，重写时被漏掉；同一次会话里还有一次 `truncated_json`，说明“重写长 JSON 参数”本身是失败来源。

建议：校验失败时回显“收到的字段名清单”（让我一眼看到缺什么）；单文件补丁支持顶层 `path` 简写，减少嵌套；或在 schema 校验前先对齐键名。

### 2.6 F6/F7 沙箱内命令的探测面与噪音（P2/P3）

```bash
$ ps -Ao pid,command | grep -iE "pudding|electron|vite" | grep -v grep
/bin/sh: /bin/ps: Operation not permitted
$ lsof -nP -iTCP:5174 -sTCP:LISTEN
lsof: can't stat(/dev): Operation not permitted
$ git check-ignore -v web/node_modules/x.png
git: error: couldn't create cache file '/var/folders/.../T/xcrun_db-XXXX' (errno=Operation not permitted)
warning: unable to access '/Users/yanggang/.config/git/ignore': Operation not permitted
```

【事实】`ps`/`lsof` 被沙箱拒绝（属预期隔离），端口只能用 `curl` 探活（`curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:5174/` → `200` 可用）；沙箱内 Git 每次调用都带无关 stderr，`git check-ignore` 因此只拿到 stdout 的答案。

建议：提供只读的端口/进程查询（或文档里写明 `curl` 探活这类替代）；收敛沙箱 Git 环境（禁 xcrun 缓存、提供只读全局 ignore），避免“按 stderr 判断失败”的噪音。`code-agent-tooling-report.md` §2.5 记过同类问题，本次仍在。

### 2.7 F8 附件导出没有中性产物区（P2）

【事实】`builtin_attachment_export` 只接受 `scope="project"`（`internal/tool/attachment_export.go:24`），而 `~/.pudding-dev` 在沙箱内不可读（`Operation not permitted`）。本次需要逐像素对比用户给的两张截图，最终只能导出到 `web/node_modules/.pudding-tmp/`（gitignored）再用 Python/PIL 差分，事后删除。

建议：为 attachment export 和分析产物增加 `temp` 作用域（或明确的分析 scratch 区），避免“为了分析而往仓库里写文件”；这条也是本次唯一一次“为了绕开工具边界而改变写入位置”的例子。

## 3. 能力缺口（不是调用失败，但直接决定成败）

### 3.1 dev 壳的 UI 观测/注入回路（G1，P0）

本次的叠加约束：开发壳在跑但**没有调试端口**（`curl http://127.0.0.1:9222/json/version` 无响应），Vite/daemon 可达；沙箱又查不到进程（见 2.6）。结果是：一个**一帧级**的视觉闪动问题，只能靠读源码推理，无法证伪，前后给出过互相矛盾的机制解释，其中一次直接改码、方向错误被撤销。

建议（按性价比排序）：

1. dev 通道默认开放 **loopback CDP**（或经菜单/快捷键在运行时打开），并在 `AGENTS.md`/`docs` 写明“agent 可用的观测入口”。
2. 提供 `make dev-attach`（或脚本）一键完成“起壳 + 暴露端口 + 打印 attach 说明”。
3. 壳启用单实例锁（`electron/main.cjs` 的 `requestSingleInstanceLock`），second-instance 建议支持复用现有窗口并接受新的调试参数，省掉“必须先退出再起”。
4. 若不开放 CDP，至少需要受限的“页面探针注入 + 读回 JSON”能力（`code-agent-tooling-report.md` §2.4 已提过同类需求，本次再次成为阻塞点）。

### 3.2 配套缺口（P1/P2）

- **UI 时序逻辑没有可测边界**：`web/test` 的 158 个用例全是纯逻辑/数据，没有 DOM 环境、也没有 ResizeObserver/rAF 假实现。本次要改的 `TranscriptList.tsx` 恰恰是“尺寸变化 → 重测量 → 重锚定 → 提交时机”的时序逻辑，我无法为“宽度变化后必须同帧提交尺寸与 scrollTop”写任何断言。建议补 happy-dom/jsdom + fake RO/rAF 脚手架，并把“重测量/重锚定决策”抽成依赖可注入的纯函数。
- **布局阈值与手势契约没有单点文档**：rail 折叠 688、工作区 dock→overlay 800、activity rail 1160、正文列宽 `min(100% - 2.5rem, 52rem)`、分隔线拖拽用绝对坐标比例且被 clamp 后不自纠——这些散在 `App.tsx`、`centeredLayout.ts`、`layoutConstants.ts`、`styles.css` 中。我拼接时判断错过一次方向。建议 desktop 侧补 `docs/layout-contracts.md`。

## 4. 失败原因归类

| 归类 | 条目 | 说明 |
| --- | --- | --- |
| 平台/契约 | F1、F3、F4、F6、F7、F8 | 报错不指导、体积上限不可预知、行号来源不一致、沙箱探测面与噪音、缺少中性产物区 |
| 使用侧（agent） | F2、F4 的行号来源、F5 | 忽略已写明的上限、从摘要推算行号、重写长参数时漏字段 |
| 流程 | 3.1、3.2 | 无复现手段即改码；把推断写成事实；没有先谈验证前提 |

可落到指令/流程的约束（针对第 3 行）：交互、布局、时序类改动要求先给出“可执行复现或观测手段 + 假设 + 判定标准”，无证据时只出方案不改文件。

## 5. 建议的验收项

1. `make dev-attach`（或等价入口）执行后，agent 能 `curl 127.0.0.1:<port>/json/list` 拿到页面目标并完成一次“注入 + 读回”。
2. `scope`（或改名后的 `area`）相关失败信息包含合法取值与命中的 root；多 root 相对路径的歧义有明确行为。
3. `builtin_file_patch` 在参数超过可接收体积时给出预检提示；失败的 hunk 可用反馈里的候选行号直接重放；单文件补丁支持顶层 `path`。
4. `web/test` 中存在带 DOM 与 fake RO/rAF 的时序用例，能断言“宽度变化后同帧提交尺寸与 scrollTop”。
5. `builtin_attachment_export` 支持 `temp` 作用域；沙箱内 `git` 不产生无关 stderr。

## 6. 边界

- 只覆盖本次会话实际发生的调用与报错；未使用的工具（history、skill、canvas、collaboration 等）不作评价。
- “原因”一栏已标注【事实】与【推断】；行号与报错串对应记录时的工作区状态，只用于定位。
- 样本内的往返次数与影响描述来自会话记录，不含估算。
