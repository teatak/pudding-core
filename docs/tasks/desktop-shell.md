# 主线:Wails 桌面壳(M-desktop-1)

> 执行者:主线(非 Codex 轨道)。边界:technology-decisions.md 第 4 节——
> 壳只做启动与系统集成,不碰 session runtime;业务协议仍是 HTTP/SSE。
> 前置已就绪:daemon 内嵌 web UI,`/?token=…` 一键握手。

## 最小可用范围(M-desktop-1)

1. `cmd/pudding-desktop`:Wails v3(alpha,锁定版本)单窗口应用;
   **daemon 同进程内嵌**(直接复用 puddingd 的 run 逻辑,单二进制),
   窗口加载 `http://127.0.0.1:<addr>/?token=…`(token 内存直传,消灭手贴)。
2. tray:显示/隐藏窗口、退出;关窗口不退进程(daemon 常驻)。
3. 端口占用处理:默认端口被占时自动换随机空闲端口(壳知道实际端口,无碍)。
4. `make desktop`:`make web` + wails 构建,产出 .app(先只管 macOS)。

## 暂不做(后续里程碑)

- 窗口状态持久化、系统通知、updater、签名/公证、Windows/Linux。

## 风险

- Wails v3 alpha API 漂移:go.mod 锁版本,升级单独 PR;
  若 v3 阻塞严重,回退方案是 menubar/systray 库 + 默认浏览器打开(降级形态)。
