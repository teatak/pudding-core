# Pudding Core

Pudding 的本地优先、多会话 Agent daemon，使用 Go、SQLite 和 loopback HTTP。
桌面产品源码在 [pudding-desktop](https://github.com/teatak/pudding-desktop)，安装包和更新仍由
[teatak/pudding](https://github.com/teatak/pudding/releases) 分发。

## 保留能力

- 模型接入、流式输出、上下文构建与压缩、工具循环。
- 多会话、消息历史、输入队列、取消、审批和可续传 SSE。
- 项目与文件、CLI / 后台进程、Git、LSP、权限与沙箱。
- Skills、Apps、MCP、附件、资源库、画布数据、用量统计。
- Go 音频与相机能力；浏览器和电脑操作保留协议及会话管理。

没有 desktop 执行端时，不提供其可视浏览器、原生电脑操作和前端画布工具。
现有无头 Chrome 能力需要本机 Chrome；硬件及原生依赖仍受现有平台支持范围限制。

## 开发

macOS 需要 Go 1.25.1、Xcode command-line tools 和 PortAudio。
构建 daemon 不需要 Web、Electron 或私有仓库。

```sh
brew install portaudio
make daemon
make test
make schema-check
```

`make daemon-dev` 启动开发服务。开发数据为 `~/.pudding-dev`，发布构建为 `~/.pudding`；
测试必须使用临时目录。CLI 仅监听 loopback，API 需要 `<home>/daemon.token` 中的启动令牌。
可选语言服务通过 `make language-servers` 准备，这一步另需 Node/npm。

独立使用示例见 [API 快速开始](docs/api-quickstart.md)。协议与客户端校验定义位于
[contracts](contracts/README.md)，设计与功能说明见 [文档索引](docs/README.md)。

## 桌面集成

`pudding-desktop/core.lock.json` 锁定 core 的准确提交。桌面构建负责前端、Swift helper、
应用装配、签名与更新；core 只构建 daemon 和其语言服务等依赖。

桌面可以通过 `-ui-dir /absolute/path/to/web/dist` 提供外置静态资源，沿用原本的页面来源与 API 地址。
该选项为空时根路径返回 404，仅提供 API；没有内嵌前端或占位页面。

## 历史与许可证

保留完整 Git 历史，历史版本中包含旧桌面源码。仓库是否公开由单独的发布操作决定。
本次工程拆分保留 [AGPL-3.0](LICENSE) 声明，后续 desktop 授权方案另行确定。
第三方组件保留各自许可证。参与开发请阅读 [AGENTS.md](AGENTS.md) 和 [CONTRIBUTING.md](CONTRIBUTING.md)。
