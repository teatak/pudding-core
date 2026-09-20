# Pudding Core

Pudding 的本地优先、多会话 Agent daemon，使用 Go、SQLite 和 loopback HTTP。
桌面产品在私有仓库 `pudding-desktop` 中独立开发，安装包和更新由
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

macOS 开发环境使用 Apple Silicon，需要 Go 1.25.1、Xcode command-line tools、PortAudio 和固定版本 Abseil。
构建 daemon 不需要 Web、Electron 或私有仓库。

```sh
brew install portaudio cmake pkgconf
bash scripts/prepare-abseil.sh
export PKG_CONFIG_PATH="$PWD/dist/deps/abseil/lib/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
make daemon
make test
make schema-check
```

Abseil 版本由准备脚本锁定，与现有 arm64 WebRTC 库的 ABI 一致；不要用系统最新版替代。
重新打开终端时需再次设置上述 `PKG_CONFIG_PATH`。Intel 发布构建使用独立的 `make runtime ARCH=x64 OUT=/absolute/path` 依赖链路。

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

Copyright 2026 Pudding Core contributors.

当前版本的 Pudding Core 采用 [Apache License 2.0](LICENSE)，允许在遵守协议的条件下商用、修改和分发，
也可作为闭源产品的组件。第三方依赖及其版权、许可证声明仍按各自条款保留。

保留完整 Git 历史，历史提交和 tag 中包含旧桌面源码及当时的 AGPL 声明；本次许可证变更不重写历史，
也不撤回历史版本已经授予的权利。独立的 `pudding-desktop` 不属于本仓库 Apache-2.0 授权范围。
参与开发请阅读 [AGENTS.md](AGENTS.md) 和 [CONTRIBUTING.md](CONTRIBUTING.md)。
