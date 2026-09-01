# Pudding 0.2.9 发版报告

日期：2026-09-01

对比基线：`v0.2.8`（`51ec408d`）

候选提交：`5dbbf019`

## 发版结论

**可以发布 0.2.9 正式版。** 本版完成 Pudding 源码公开后的桌面分发适配，在安装包中加入完整许可证与第三方声明，升级存在安全问题的依赖，并包含若干编辑器与交互细节修复。
SQLite schema、自动更新协议、系统权限归属、签名身份和稳定通道规则均未变化。

## 改动摘要

- 将 `pudding-core` 公开为 AGPL-3.0-only 源码仓库，补齐贡献、安全、行为准则和商标政策。
- 构建时从锁定依赖生成第三方声明，并将 Pudding 许可证、Electron/Chromium 许可证和第三方声明写入每个 App、ZIP 与 DMG。
- 发布校验器拒绝缺少或内容不完整的许可证文件，确保下载产物与开源分发规则一致。
- 升级 go-git、x/sys、Wrangler、Cloudflare 类型和相关传递依赖，修复已发现的依赖安全问题。
- 更新 Monaco Editor 并简化语言加载路径，改善项目编辑器的初始化与打包结构。
- 为消息复制按钮增加状态提示，修正浏览器空白标签图标尺寸。
- 设置页和品牌文案更新为开源 macOS AI 工作区定位。
- 新增公开仓库 CI、秘密扫描和可复现 npm/pnpm 工具链约束。

## 影响范围

| 范围 | 影响 | 说明 |
| --- | --- | --- |
| 开源与许可证 | 中 | 源码采用 AGPL-3.0-only；正式安装包新增完整许可证和第三方声明。 |
| 依赖与安全 | 中 | Go、Monaco 和 OAuth Worker 工具链依赖升级，锁文件同步更新。 |
| 项目编辑器 | 中 | Monaco 入口和语言注册方式调整，生产构建与 CI 已覆盖。 |
| 桌面界面 | 低 | 复制提示、浏览器图标和开源品牌文案调整。 |
| SQLite | 无 | schema 仍为 v13，本版没有迁移。 |
| 自动更新 | 无 | 更新检查、下载、安装、架构选择和通道逻辑未变化。 |
| 权限与签名 | 无 | Bundle identity、TCC 权限归属、Developer ID 签名与公证要求未变化。 |

## 数据与兼容性

- SQLite schema 保持 v13，本版不创建数据库迁移备份，也不执行迁移或备份清理。
- canonical sessions、messages、turns、projects、usage 和 provider 配置均不重写。
- 稳定版继续使用 `~/.pudding`，数据目录和更新通道规则不变。
- OAuth Worker 的网站文案和依赖更新不属于桌面安装包，需按 Worker 流程单独部署。
- 正式数据库只读检查结果为 `PRAGMA user_version = 13`、`PRAGMA quick_check = ok`。

## 已完成验证

- 最新 `main` GitHub CI 通过，包含 Go、Electron、Web build、OAuth Worker typecheck、秘密扫描和许可证生成。
- `npm run check:secrets` 通过：946 个 tracked files 已检查。
- Electron 全量测试：194 项通过。
- `web/` 生产构建通过。
- `make schema-check` 通过，schema v13 release contract 有效。
- 正式数据库只读检查：`PRAGMA user_version = 13`，`PRAGMA quick_check = ok`。
- `make desktop-notary-check` 通过。
- `git diff --check` 通过。

## 剩余发版门禁

- 固定发布流水线重新运行全量 Go 与 Electron 测试。
- 生成许可证文件后完成 arm64/x64 构建、Developer ID 签名、公证和 Gatekeeper 校验。
- 九个正式通道产物完整上传到唯一 Draft Release 后再显式发布。

## Release Notes

### Open Source Distribution

- Publish the Pudding source under AGPL-3.0-only with contribution, security, conduct, and trademark policies.
- Bundle the Pudding license, Electron and Chromium licenses, and generated third-party notices in every macOS package.
- Verify legal notices in staged apps and the copies extracted from ZIP and DMG artifacts.

### Security and Dependencies

- Update go-git, x/sys, Wrangler, Cloudflare types, Monaco Editor, and affected transitive dependencies.
- Add public CI, tracked-file secret scanning, and reproducible npm and pnpm toolchain versions.

### Editor and Interface

- Simplify Monaco Editor initialization and language registration for project files.
- Add copy-state tooltips to transcript messages and refine blank browser-tab icon sizing.
- Update in-app positioning to describe Pudding as an open-source AI workspace for macOS.

### Data and Compatibility

- Keep SQLite schema v13 unchanged with no migration, backup, cleanup, or canonical-data rewrite.
- Preserve existing automatic updates, app identity, permissions, signing, notarization, and stable-channel behavior.
