MODULE := github.com/teatak/pudding-core
LDFLAGS_RELEASE := -X $(MODULE)/internal/buildinfo.channel=release
BUILDTAGS := sqlite_fts5 webrtcaec

.PHONY: test schema-check tidy clean embed language-servers language-servers-ready desktop desktop-dev desktop-release desktop-runtime-arm64 desktop-runtime-x64 desktop-runtimes desktop-bundle desktop-verify desktop-update-test desktop-publish desktop-preview-bundle desktop-preview-verify desktop-preview-publish desktop-publish-from-tag desktop-publish-upload-resume desktop-release-status desktop-release-finalize daemon daemon-dev daemon-release prompt tools-report tools-eval

# 共享:构建前端并装填进 daemon 的 embed 目录(产物不进 git)
embed:
	cd web && npm run build
	rm -rf internal/webui/dist
	mkdir -p internal/webui/dist
	cp -R web/dist/ internal/webui/dist/
	touch internal/webui/dist/.gitkeep

# —— 桌面壳(Electron shell + daemon)——
# 构建(dev 通道;含 embed web 和 daemon binary,打包链路后续补齐)
desktop: embed
	go build -tags "$(BUILDTAGS)" -o bin/puddingd ./cmd/puddingd

# 热更循环:Electron shell + Vite(HMR) + daemon;首次自动准备固定版本语言服务
desktop-dev: language-servers-ready
	@BUILDTAGS="$(BUILDTAGS)" bash scripts/desktop-dev.sh

# 发布桌面 daemon(release 通道:端口 9669 / ~/.pudding;供 .app bundle 使用)
desktop-release: schema-check embed
	go build -tags "$(BUILDTAGS)" -ldflags "$(LDFLAGS_RELEASE)" -o bin/puddingd ./cmd/puddingd

# 发布包内置语言服务(固定版本;构建期下载,运行时不联网安装)
language-servers:
	@bash scripts/prepare-language-servers.sh

# 开发态只在缺失或版本变化时重新准备,避免每次启动都执行 npm/go install
language-servers-ready:
	@bash scripts/prepare-language-servers.sh --ensure

# 分架构准备发布 runtime。x64 在 Apple Silicon 上交叉编译。
desktop-runtime-arm64: schema-check embed
	@PUDDING_PACKAGING_PIPELINE=1 bash packaging/macos/prepare-runtime.sh arm64

desktop-runtime-x64: schema-check embed
	@PUDDING_PACKAGING_PIPELINE=1 bash packaging/macos/prepare-runtime.sh x64

desktop-runtimes: schema-check embed
	@PUDDING_PACKAGING_PIPELINE=1 bash packaging/macos/prepare-runtime.sh arm64
	@PUDDING_PACKAGING_PIPELINE=1 bash packaging/macos/prepare-runtime.sh x64

# macOS 双架构安装与更新产物(DMG + ZIP + latest-mac.yml;正式自动更新需要签名/公证)
desktop-bundle: desktop-runtimes
	@PUDDING_PACKAGING_PIPELINE=1 PUDDING_RELEASE_CHANNEL=stable node scripts/package-desktop.cjs

desktop-verify:
	@PUDDING_PACKAGING_PIPELINE=1 PUDDING_RELEASE_CHANNEL=stable node scripts/package-desktop.cjs --verify-only

desktop-update-test: desktop-verify
	@PUDDING_RELEASE_CHANNEL=stable node scripts/run-update-test.cjs

# 本机完成测试、tag、签名、公证，并上传 Draft Release。
desktop-publish:
	@PUDDING_RELEASE_CHANNEL=stable node scripts/release-local.cjs start

# Preview 与正式版共用 Pudding.app / appId / ~/.pudding,仅发布为 GitHub Prerelease beta 通道。
desktop-preview-bundle: desktop-runtimes
	@PUDDING_PACKAGING_PIPELINE=1 PUDDING_RELEASE_CHANNEL=preview node scripts/package-desktop.cjs

desktop-preview-verify:
	@PUDDING_PACKAGING_PIPELINE=1 PUDDING_RELEASE_CHANNEL=preview node scripts/package-desktop.cjs --verify-only

desktop-preview-publish:
	@PUDDING_RELEASE_CHANNEL=preview node scripts/release-local.cjs start

# tag 已推送但本地发布中断时，从同一提交恢复。
desktop-publish-from-tag:
	@node scripts/release-local.cjs resume

# 本地产物已完整验证、仅 Draft 创建或上传失败时续传,不重复构建和公证。
desktop-publish-upload-resume:
	@node scripts/release-local.cjs upload

# Draft 出现即表示签名、公证和产物上传均已完成；显式确认后才公开 Release。
desktop-release-status:
	@node scripts/release-draft.cjs status $(RELEASE_TAG)

desktop-release-finalize:
	@node scripts/release-draft.cjs publish $(RELEASE_TAG)

# —— headless daemon(无窗口,浏览器访问)——
# 构建(dev 通道;含 embed web)
daemon: embed
	go build -tags "$(BUILDTAGS)" -o bin/puddingd ./cmd/puddingd

# 热更循环:停旧实例 → 起 puddingd + Vite,浏览器开脚本打印的带 token URL
daemon-dev:
	@BUILDTAGS="$(BUILDTAGS)" bash scripts/dev.sh daemon

prompt:
	go run -tags "$(BUILDTAGS)" ./cmd/puddingd prompt $(RUNARGS)

tools-report:
	go run -tags "$(BUILDTAGS)" ./cmd/puddingd tools report $(RUNARGS)

tools-eval:
	go run -tags "$(BUILDTAGS)" ./cmd/puddingd tools eval $(RUNARGS)

# 发布构建(release 通道:端口 9669 / ~/.pudding;含 embed web)
daemon-release: schema-check embed
	go build -tags "$(BUILDTAGS)" -ldflags "$(LDFLAGS_RELEASE)" -o bin/puddingd ./cmd/puddingd

test:
	go test -tags "$(BUILDTAGS)" ./...

# 发布门禁:schema.sql 一旦变化,必须同步增加版本、迁移和发布指纹。
schema-check:
	go test -tags "$(BUILDTAGS)" ./internal/store/sqlitestore -run '^TestSchemaReleaseContract$$'

tidy:
	go mod tidy

clean:
	rm -rf bin
