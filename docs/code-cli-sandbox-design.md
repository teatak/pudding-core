# Code CLI 沙箱设计

> 状态:macOS 第一版已完成(2026-07-13);Windows 沙箱适配后续单独实施。
> 范围:仅覆盖 LLM 调用的 `builtin_command_run` 与
> `builtin_command_start` 及其子进程。

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
| `ask` | 所有 Code 操作先审批 | 批准的该次命令在项目沙箱外运行 |
| `auto` | 仅风险规则命中时审批 | 低风险命令在项目沙箱内运行;批准的风险命令仅该次绕过 |
| `full` | 不弹普通审批 | CLI 不套项目沙箱 |

这里的“仅该次”是完整工具调用级授权:命令、参数、cwd 与 env 不得在批准后变更。
后台命令也只在启动时使用该授权,不会把绕过权留给后续模型调用。

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

`command_run` 与 `command_start` 必须使用同一个 `CommandRunner`,避免前台命令
受限而后台命令绕过。Runner 只负责准备进程和执行元数据;输出流、超时、取消、
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

Auto 从命令白名单改为风险规则。以下操作仍需审批:

- 明确的删除、磁盘、提权、系统配置和进程控制操作。
- shell script、管道、重定向和动态解释执行。
- 明确访问 Project roots 之外的路径。
- Git `push`、发布、上传、凭证和其他外部写入副作用。
- 显式通配监听或请求放宽文件系统边界。

未知的直接 `argv` 不再仅因命令名未知而审批。沙箱不能保护 Project 自身免受恶意
写入,因此 destructive 规则与 patch/git 审阅仍然保留。

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
- 本地开发服务器可启动、轮询并停止。
- timeout/cancel 能终止整个沙箱进程组。
- 已运行后台进程不受后续权限变化影响。
- 风险命令批准后仅当前调用绕过沙箱,后续同类调用仍重新经过策略判断。
- 无 Project 的 Code session 使用隔离临时工作区,且删除 session 时清理。
- `full` 保持无沙箱执行语义。
- 沙箱不可用或策略生成失败时明确失败,不静默降级。
