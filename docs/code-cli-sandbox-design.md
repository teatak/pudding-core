# Code CLI 沙箱设计

> 状态:macOS 第一版已完成(2026-07-13);Windows 沙箱适配后续单独实施。
> 范围:仅覆盖 LLM 调用的 `builtin_command_run` 及其前台/后台子进程。

## 1. 目标

Pudding 的 Auto 审批应允许大多数项目内 CLI 工作流,同时用操作系统边界阻止
命令静默访问项目之外的用户数据。CLI 沙箱与审批是两层机制:

- 审批决定一条命令是否可以启动。
- 沙箱决定已启动命令实际可以访问哪些资源。

本次不沙箱化用户手动打开的交互终端、LSP、Browser、Canvas、App MCP 或整个
Electron/daemon 进程。

## 2. 模式语义

| Project 审批模式 | 启动前 | CLI 执行边界 |
| --- | --- | --- |
| `ask` | 项目低风险只读操作免审批;写入和命令执行仍审批 | 命令默认仍在项目沙箱内运行;仅明确需要 host access 的调用绕过 |
| `auto` | 仅风险规则命中时审批 | 低风险命令自动在项目沙箱内运行;批准不等于自动绕过沙箱 |
| `full` | 不弹普通审批 | CLI 不套项目沙箱 |

完整访问是工具调用级授权:命令、参数、cwd 与 env 不得在批准后变更。需要确认但
不需要 host access 的删除、动态 shell 等命令,批准后仍受项目沙箱保护。后台命令
也只在启动时使用该授权,不会把绕过权留给后续模型调用。

进程权限在启动时固定。切换模式、撤销 Code 能力或修改 Project roots 不改变已经
运行的进程,也不主动终止进程。

## 3. 执行架构

```text
engine approval policy
  -> tool.Call(ProjectDirs + command execution policy)
  -> CommandRunner.Prepare
       -> direct runner(full)
       -> macOS sandbox runner(ask / auto)
       -> unsupported-platform rejection(ask / auto)
  -> exec.Cmd lifecycle
  -> stdout / stderr / timeout / process-group cancellation
```

`command_run` 的前台与后台路径必须使用同一个 `CommandRunner`,避免后台命令
绕过沙箱。Runner 只负责准备进程和执行元数据;输出流、输入、超时、取消、
后台保留与 session ownership 仍由现有 tool 层负责。

desktop daemon 会在继承的 `PATH` 后补充 Homebrew 和常见用户工具链目录。沙箱与
direct runner 都按该合并值解析可执行文件,因此从 Finder 启动 Electron 时也能找到
`brew`、Node、Go、Rust 等常用工具。

## 4. macOS 第一版策略

第一版使用独立的 Darwin runner 生成逐次执行的 Seatbelt profile:

- Project roots:可读写。
- 没有 Project/turn grant 时,session 隔离的 Pudding Code 临时工作区作为唯一
  Project root,可读写。
- Pudding 管理的命令临时目录和构建缓存:可读写。
- `TMPDIR` / `TMP` / `TEMP` 指向 Pudding 管理的私有临时目录;不开放共享 `/tmp`。
- Python user base 和字节码缓存写入 Pudding 管理的隔离状态,不污染用户目录。
- Python/pip/Requests/curl 默认使用可读的系统 CA bundle,禁止用 `--trusted-host`
  绕过 TLS 校验。
- Go 等使用 macOS 原生证书验证的客户端可连接 `com.apple.trustd.agent` 做系统信任评估;
  不因此开放 `securityd` 或用户私人钥匙串文件。`GOCACHE` / `GOMODCACHE` 仍是稳定的
  沙箱私有可写缓存,不强制更改 `GOPROXY` / `GOSUMDB` / `GOFLAGS`。
- npm global prefix、pnpm home、Yarn global、Corepack cache 和 Node REPL history
  写入 Pudding 管理的隔离状态;不读取用户 npm 配置。
- 系统运行库、已解析的工具链目录:只读。
- 为离线构建兼容性,常见语言包缓存只读;不开放用户级配置、凭证或偏好设置。
- 用户其他目录:禁止读写。
- 子进程继承相同限制。
- 不允许静默退回无沙箱执行。

网络由审批风险规则控制,不由文件沙箱二次否决。通过审批策略的命令允许外部出站
网络;本地服务只允许 loopback 监听。常规依赖下载及 Git `clone/fetch/pull` 可在
`auto` 下运行,Git `push`、发布、上传、登录和凭证操作仍需审批。

macOS Seatbelt 将 `0.0.0.0` bind 也归入 `localhost` 规则,无法在保留开发服务器的
同时强制监听地址。Auto 风险规则会拦截常见的显式通配监听参数和环境变量,但项目
代码内部自行绑定通配地址仍是第一版残余限制;开发服务应默认监听 `127.0.0.1`。

`sandbox-exec` 已被 macOS 标记为 deprecated,因此调用必须封装在
`CommandRunner` 后面,不能渗透到 engine、审批或 transcript 协议。未来可以替换
为其他系统 runner,而不改变 LLM 工具契约。

非 macOS 平台在 `ask` / `auto` 下明确拒绝 CLI,不会退回无沙箱执行;`full` 仍按
完整访问语义直接执行。Windows/Linux runner 后续独立适配。

## 5. 审批规则

Ask 不再询问明确标记为低风险的只读工具,包括结构化 Git status/diff/log 与代码
导航/诊断。`LowRisk` 写入、符号重命名、任意命令执行及未明确分类为低风险的读取仍需
审批,不把 Ask 变成 Auto。Computer Use 的首次 app 授权及项目访问边界不受此豁免影响。

Auto 从命令白名单改为风险规则。以下操作仍需审批:

- 明确的删除、磁盘、提权、系统配置和进程控制操作。
- 隐藏实际命令的 shell wrapper 和无法静态判断目标的动态结构。
- 明确访问 Project roots 之外的路径。
- Git `push`、发布、上传、凭证和其他外部写入副作用。
- 显式通配监听或请求放宽文件系统边界。

未知的直接 `argv` 不再仅因命令名未知而审批。沙箱不能保护 Project 自身免受恶意
写入,因此 destructive 规则与 patch/Git 准备校验仍然保留。

结构化的 `builtin_git_stage`、`builtin_git_unstage`、`builtin_git_commit` 属于受限的
本地项目写入,在 `auto` 下不再额外弹窗,`ask` 仍逐次审批。它们始终验证项目/仓库边界、
显式文件路径,提交仍准备 staged diff 并校验 HEAD/index 没有漂移;不执行仓库 hooks 或
clean filters。免弹窗不意味着任务授权:模型只应在用户要求提交时创建提交,不能把无关
修改一并暂存。任意 Git CLI 写入不继承这项豁免,`push`、强推、`reset --hard`、`clean`
等仍受原有风险规则保护。本次没有加入自动审批模型或放宽 host 执行边界。

## 6. 失败与展示

命令结果增加不破坏旧解析的可选字段:

```jsonc
{
  "sandboxed": true,
  "sandboxKind": "macos-seatbelt",
  "sandboxDenied": false
}
```

疑似由系统策略拒绝时返回 `sandboxDenied=true`,并在终端卡片中显示受限执行原因。
不得自动使用 full access 重跑。需要更高权限时,由 LLM 发起新的显式审批;批准后
只重试审批卡中展示的同一条调用。

## 7. 验收标准

- 未知直接 CLI 在 Auto 下无需仅因命令名未知而审批。
- 命令可以正常读写授权 Project。
- 命令不能通过绝对路径、`..` 或符号链接读写 Project 外文件。
- `go test`、前端 build/test、Git 只读和 Python test 可运行。
- Git `clone/fetch/pull` 与依赖下载可在 Auto 沙箱中访问外部网络。
- 结构化 Git 暂存、取消暂存和普通提交在 Auto 下不产生审批事件,Ask 下仍需审批;
  两种模式均不能绕过路径、空提交和提交漂移校验。
- 本地开发服务器可启动、轮询并停止。
- timeout/cancel 能终止整个沙箱进程组。
- 已运行后台进程不受后续权限变化影响。
- 只有明确需要项目外路径、系统工具或用户凭证的命令在批准后绕过沙箱。
- 仅需确认的风险命令批准后仍在沙箱内运行。
- 无 Project 的 Code session 使用隔离临时工作区,且删除 session 时清理。
- `full` 保持无沙箱执行语义。
- 沙箱不可用或策略生成失败时明确失败,不静默降级。

## 8. Go TLS 回归(2026-09-12)

已复现:同一自签名测试证书在 host 返回正常的不受信任错误,沙箱返回
`x509: OSStatus -26276`;真实冷缓存 `go mod download` 在访问公共模块元数据时出现同一错误。
逐项对照发现仅增加 `com.apple.trustd` 无效,增加 `com.apple.trustd.agent` 后恢复系统信任评估。
最终只加入后者,不关闭 TLS 或模块校验,不引入 host 重试或缓存共享路径。

- `TestMacOSCommandSandboxSystemTrust`:不联网、不安装 CA,验证 host 与 sandbox 都正常拒绝
  未受信任的自签名证书,避免把证书校验失败误判为信任服务不可达。
- `TestMacOSCommandSandboxGoModuleTLS`:显式启用的联网验收,在临时项目中下载 `yaml.v3`,验证
  模块校验、冷缓存测试、跨 runner 的缓存路径稳定性,以及 `GOPROXY=off` 时的热缓存测试。
  临时模块缓存用 Go 自带的清理命令移除,不触及真实项目或用户缓存。

该联网用例默认跳过;联网验收单独启用:

```sh
PUDDING_SANDBOX_NETWORK_TEST=1 go test -tags 'sqlite_fts5 webrtcaec' ./internal/tool -run '^TestMacOSCommandSandboxGoModuleTLS$' -count=1 -v
```

本次环境的 `sum.golang.org` 在 host 上也有独立的 TLS 连接失败;联网验收显式设置
`GOSUMDB=sum.golang.google.cn`,使用 Go 支持的官方别名和同一签名校验身份后通过。
该设置只用于测试,不改变产品默认网络配置,也不是失败后的自动切换。
