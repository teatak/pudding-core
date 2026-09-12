# Code 模式工具与终端能力报告

> 状态:记录型文档,按会话累积;保留原始问题与建议,当前落地情况见第 5、6、7 节
> 日期:2026-09-12
> 范围:Code 模式下文件读写、补丁、命令与终端、Git、审批、运行时验证能力;样本为 2026-09-12 一次真实改动会话。
> 结论:文件读写、代码导航、Git 结构化工具、命令沙箱默认值都够用;摩擦集中在**沙箱与主机工具链断链、审批粒度、补丁工具的定位反馈、前端运行时验证回路**四块。

## 0. 样本与方法

样本是一次完整改动会话:Go 侧压缩对账修复(engine/api/store + 测试)、Web 侧四处 UI 修复(贴底按钮、文件卡片窄态、模型厂商图标、面板最小宽度)、一笔最小窗口调整(electron + smoke + 文档)、3 笔提交与 push,以及一轮前端运行时排障(抽屉入场把会话横向推走)。

数据来源是会话内的命令输出与工具返回,次数为逐条计数,不含估算;行号偏差以当时的工具报错为准。

## 1. 结论摘要

| # | 问题 | 本次影响 | 建议 | 优先级 |
| --- | --- | --- | --- | --- |
| 1 | 补丁工具报错不给定位上下文 | 6 次 `old_lines do not match` 重试 + 1 次参数截断 | 失败时返回最近匹配块与建议行号;支持锚点式 hunk | P0 |
| 2 | 沙箱内跑不了 Go 测试 | 2 次 `go test` 全走 host + 审批 | 主机模块缓存只读挂载,`GOPROXY=off` | P0 |
| 3 | 审批粒度粗且不记忆 | 4 次 host 执行、3 笔提交共 6 次 stage/commit 审批 | 只读命令白名单 + 会话级同类授权 + git 合并 stage/commit | P1 |
| 4 | 前端运行时验证回路缺失 | 需要人工注入探针、回传 221KB 日志,再走 host 解析 | 受限的"页面注入 + 读回 JSON"能力;附件按字节窗口读 | P1 |
| 5 | 输出噪音与校验入口 | 每次 sandbox git 都带无关 stderr;TS 检查只能整包 build | 沙箱内 Git 环境收敛;改后优先 code diagnostics | P2 |

## 2. 问题清单

### 2.1 沙箱与主机工具链断链

现象:`go test` 在沙箱内编译失败,只能走 host 执行。

```
$ go env GOCACHE GOMODCACHE GOPROXY          # 沙箱内
/Users/yanggang/.pudding/runtime/command-sandbox/<hash>/cache/go-build
/Users/yanggang/.pudding/runtime/command-sandbox/<hash>/go/pkg/mod
https://proxy.golang.org,direct

$ go test -tags 'sqlite_fts5 webrtcaec' ./internal/engine/
gopkg.in/yaml.v3@v3.0.1: Get "https://proxy.golang.org/gopkg.in/yaml.v3/@v/v3.0.1.zip":
  tls: failed to verify certificate: x509: OSStatus -26276
FAIL github.com/teatak/pudding-core/internal/engine [setup failed]
```

`GOCACHE` 与 `GOMODCACHE` 被重定向到沙箱私有空目录,`GOPROXY` 仍指向公网,而沙箱内网络不可用(证书校验失败)。主机上 `~/go/pkg/mod` 已有完整依赖。同类问题还出现在诊断命令上:本会话早些时候定位空载 CPU 需要 `ps`/`top`/`sample`,沙箱内不可用,只能 host 执行。

影响:每轮 Go 验证都要一次 host 审批;诊断类命令同理。

### 2.2 审批粒度与记忆

现象(本次计数):

- host 执行 4 次:`go test` ×2、`git push` ×1、附件解析 `find ~/.pudding ... python3` ×1。
- Git 写操作 6 次调用:3 笔提交各自的 stage + commit。
- 同类操作之间没有会话级记忆:第二条 `go test` 与第一条同样要审批。

用户在本会话更早也明确提出过"sh 还是有很多审批",当时的结论是:tool-call 审批没有会话级"同类记住",且 `execution=host` 一律按非低风险处理。

### 2.3 补丁工具定位反馈

现象:补丁失败只返回 `hunk N old_lines do not match at start_line X`,不给出实际匹配位置。

| 位置 | 传入 start_line | 实际行号 |
| --- | --- | --- |
| `internal/api/server.go`(compact 调用) | 1083 | 1082 |
| `web/src/components/transcript/TurnFileChanges.tsx`(import) | 12 | 11 |
| `web/src/components/ComposerQueue.tsx`(section) | 51 | 53 |
| `web/src/components/ComposerQueue.tsx`(row) | 70 | 75 |
| `web/src/App.tsx`(session stage) | 805 | 821 |

`web/src/provider/presets.ts` 那次同尺寸 hunk 首次失败、拆成两段后通过,报错未说明差异原因。另有 1 次多文件大补丁参数被截断,返回 `unexpected end of JSON input`,需要改成分批提交。

行号偏差来自"上一次读取之后文件被其他编辑改动过",这是多文件原子补丁的正常情况,但当前报错不提供任何可供自我修正的信息,因此每次都要整轮重读。

### 2.4 前端运行时验证回路缺失

排障(抽屉入场时 `.pudding-session-stage` 的 `scrollLeft` 0→125→0)最终靠:让用户手工粘贴探针脚本 → 点击复现 → 把日志作为附件回传 → 我再解析。日志本身暴露两个能力缺口:

- 附件是 221 KB 的**单行** JSON,项目文件工具按行读取、单行上限 64 KiB,无法整块读取;最后用 host 命令 `python3` 解析 `~/.pudding/temp/attachments/<file>.txt`。
- 第一版探针把时间戳纳入变化键,导致每帧都记录(1442 行),说明缺少可复用的探针模板与结果上限约定。

结论:前端布局/滚动这类"必须看真实运行几何"的问题,目前没有 Agent 可自闭环的手段,必然产生人工往返。

### 2.5 输出噪音与校验入口

- 沙箱内每次 `git` 调用都输出与结果无关的 stderr,本次出现 10 次以上:
  ```
  git: error: couldn't create cache file '/var/folders/.../xcrun_db-*' (errno=Operation not permitted)
  xcodebuild[...]: DVTFilePathFSEvents: Failed to start fs event stream.
  warning: unable to access '/Users/yanggang/.config/git/ignore': Operation not permitted
  ```
  结果正确,但需要逐次人工过滤,也容易掩盖真正的错误。
- 仓库根没有 `tsc`,`npx tsc -b web --force` 在根目录失败;TS 静态检查只能走 `npm --prefix web run build`(整包 build)。本次改动多为单文件,理想入口是语言服务的 diagnostics。
- 校验产物 CSS(确认 Tailwind 是否真的生成了某个工具类)没有现成手段,需要自己写脚本在单行大 CSS 里定位;这类"类名写错就静默失效"的检查值得内建。
- 仓库侧小事:`web/test/input-flow-transcript.test.ts` 首跑失败、随后两次通过(该用例自建 vite server),属于 flake。

### 2.6 使用侧问题(Agent 自身,非平台)

- 未在改完 TS 后先用 `builtin_code_diagnostics` 做快速静态检查,而是直接跑整包 build。
- 多文件大补丁没有按文件拆分,触发参数截断。
- 附件处理绕开沙箱边界走 host,而更合适的路径是先 `builtin_attachment_export` 落到项目内再脚本处理。
- 探针首版设计有缺陷,浪费了一轮往返。

## 3. 建议与验收

1. **补丁报错带上下文(P0)** — 改动点:`internal/tool/patch.go`。失败时返回"最接近的匹配块首行号 + 该处真实文本(带编号)",并明确**只提示、不自动套用**(错位落笔比报错危险)。验收:构造 5 个错位样本(偏移 1 行、整段移动、重复行、空白差异、删行),断言返回位置与文本正确,且代码中不存在模糊应用路径。
2. **Go 模块缓存只读挂载(P0)** — 改动点:命令沙箱配置层。把主机 `GOMODCACHE` 与 go-build cache 只读挂入沙箱,设 `GOFLAGS=-mod=mod`、`GOPROXY=off`。验收:沙箱内 `go test -tags 'sqlite_fts5 webrtcaec' ./internal/engine` 通过且无网络访问;`go mod download` 在沙箱内应明确失败(仅离线可用)。
3. **只读白名单 + 会话级授权 + git 合并提交(P1)** — 改动点:`internal/tool/policy.go` 的只读 host 命令族(`ps`、`top -l 1`、`sample`、`vm_stat`、`netstat -an`、`lsof -n`、`pgrep`)、仿 `HasComputerAppGrant` 的 session 级授权、审批菜单增加"本会话允许同类"、`git commit` 支持直接指定待提交路径。验收:只读族单测覆盖危险变体(`top -o`、`lsof +D` 等仍审批);授权后同类命令不再弹;一笔提交由 2 次审批降为 1 次。
4. **前端运行时验证回路(P1)** — 改动点:受限的"在本地页面注入脚本并读回 JSON"能力(仅 localhost 与项目 URL、结果大小与超时上限、不读取凭据),并附标准探针模板;同时让附件支持按字节窗口读取,或允许导入项目临时目录供脚本处理。验收:一次真实采样(打开 dev 页面 → 采样 60 帧几何 → 读回 JSON)全程不需要人工粘贴。
5. **输出与入口收敛(P2)** — 沙箱内 Git 环境收敛(`GIT_CONFIG_GLOBAL=/dev/null`、`GIT_OPTIONAL_LOCKS=0`、`TMPDIR` 指向沙箱可写目录)以消除无关 stderr;改完源文件后优先用语言服务 diagnostics;为"CSS 工具类是否真的生成"这类检查提供内建手段。

## 4. 边界

- 本文只记录本次会话可复现的摩擦与建议,不代表工具能力的完整清单;`computer-use`、`browser`、Canvas、App 加载等本次未作为主线使用,未纳入评估。
- 优先级依据"本次实测的时间损耗 + 改动成本",不是发布计划;落地时以各改动点的验收标准为准。

## 5. 首轮优化:补丁错误反馈(2026-09-12)

保持现有 JSON 参数、严格行号与原文校验,不引入锚点语法或模糊应用。`hunk_lines_mismatch`
和 `hunk_line_out_of_range` 在原有 `reason/detail` 外返回 `recovery` 与 `hint`:

- `path`、`hunk`、`startLine`、`fileLineCount` 标明失败位置;`hunk` 为文件内从 1 开始的序号。
- `mismatch` 包含第一个不匹配行的行号、预期原文与实际原文;越界时不虚构实际行。
- `candidateStartLines` 是整个非空 `old_lines` 逐行精确匹配的起始位置,按行号升序最多返回 5 个;
  `exactMatchCount` 是完整匹配数,超过展示上限时标记 `candidatesTruncated`。重复文本不自动选最近位置。
- `context` 最多返回失败位置附近 5 行编号文本;越界时返回文件末尾上下文,空文件返回空数组。
  每段文本预览最多保留 1024 字节的 UTF-8 前缀,超限另附截断标记并设置对应的 `truncated` 字段。
- 候选位置只用于导航。确认上下文并重新读取后再提交修正的 patch;诊断文本被截断时不得用于 `old_lines`。
  失败的整批 patch 不写入文件,审批前错误返回保留上述恢复信息。

提示词与工具描述同步说明恢复流程。回归覆盖行偏移、整段移动、重复块、空白差异、删行、越界、
CRLF、无末尾换行、超长文本、重叠候选与多文件失败不落盘。其他建议尚未在本轮实现。

## 6. 第二轮优化:Go 沙箱 TLS(2026-09-12)

原文第 2.1 节的失败已复现,但“沙箱内网络不可用”不是准确归因:文件沙箱原本允许出站网络,
缺少的是 macOS 原生证书验证访问 `com.apple.trustd.agent` 的权限。

- 修复前:host 能执行系统信任评估,沙箱返回 `OSStatus -26276`;冷缓存模块下载在 TLS 验证时失败。
- 对照实验:单独允许 `com.apple.trustd` 仍失败;允许 `com.apple.trustd.agent` 后恢复。
- 最终修复:仅增加后者的 Mach 服务访问。`securityd`、私人钥匙串文件权限不变;无效证书仍被拒绝。
- 已验证:临时项目的 HTTPS 模块下载与校验、冷缓存 `go test`、新 runner 的热缓存复用,
  以及项目外文件隔离。`GOPROXY=off` 仅用于验证热缓存,未设为产品默认。

第 3 节的只读构建缓存、强制 `GOFLAGS=-mod=mod` 与全局离线建议未采用;
继续使用沙箱私有可写缓存。`sum.golang.org` 的 host 连通故障另行区分,
本次联网验收使用官方 `sum.golang.google.cn` 别名,没有关闭 TLS/校验或增加自动重试路径。
复现与验收入口见 [Code CLI 沙箱设计](code-cli-sandbox-design.md#8-go-tls-回归2026-09-12)。

## 7. 第三轮优化:减少本地 Git 重复审批(2026-09-12)

已确认第 2.3 节 stage/commit 反复审批的代码原因:三个结构化 Git 写工具均未标为
`LowRisk`,而 Auto 直接把非低风险写入交给人工。调整为受限的低风险本地写入,
沿用现有审批引擎,不增加会话白名单、合并工具或自动审批模型。

- Auto:结构化暂存、取消暂存和普通本地提交不再额外弹窗;Ask 仍逐次审批。
- Ask:低风险只读工具直接通过,包括 Git status/diff/log 和代码导航/诊断;
  写入、重命名和命令执行仍审批。界面三种语言的说明与提示词同步,Computer Use
  app 授权保持独立。新增回归验证这些查询不产生审批事件,未知/非低风险读取仍需审批。
- 所有模式仍执行项目/仓库/显式路径检查、staged diff 准备和 HEAD/index 漂移校验。
- 提示词要求用户提出提交任务后才提交、先检查 staged diff、保留无关修改;
  已授权的正常检查/编辑/验证不再额外进行对话式确认。
- 任意 Git CLI 写入、push、强推、丢弃修改与 host 执行的规则未放宽。
- 回归:临时仓库中 stage → unstage → stage → commit,Auto/Full 为 0 个审批事件,
  Ask 为 4 个;非法路径、Git 元数据路径与空提交在执行前拒绝,不弹无意义审批。
  原有提交漂移和 hooks/filters 禁用回归、`make test` 均通过。

本节只解决明确的低风险误分类与提示词重复询问;其他命令的静态风险规则、
自动审批模型、桌面打包和重启不在本轮交付中。
