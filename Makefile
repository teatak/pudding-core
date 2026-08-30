MODULE := github.com/teatak/pudding-core
LDFLAGS_RELEASE := -X $(MODULE)/internal/buildinfo.channel=release
BUILDTAGS := sqlite_fts5 webrtcaec
PUDDING_NOTARY_PROFILE ?= pudding-notary
PUDDING_NOTARY_APPLE_ID ?= yangglivecn@icloud.com
PUDDING_NOTARY_TEAM_ID ?= 7K47HJ79JA

.PHONY: test schema-check tidy clean embed brand-assets language-servers language-servers-ready computer-use-helper-dev computer-use-helper-test computer-use-fixture-dev computer-use-fixture-smoke computer-use-product-smoke computer-use-calculator-smoke computer-use-calculator-existing-smoke desktop desktop-dev desktop-release desktop-runtime-arm64 desktop-runtime-x64 desktop-runtimes desktop-bundle desktop-verify desktop-update-test desktop-computer-use-update-test desktop-notary-check desktop-notary-store desktop-publish desktop-preview-bundle desktop-preview-verify desktop-preview-update-test desktop-preview-computer-use-update-test desktop-preview-publish desktop-publish-from-tag desktop-publish-upload-resume desktop-release-status desktop-release-finalize daemon daemon-dev daemon-release prompt tools-report tools-eval agent-eval

brand-assets:
	@bash scripts/render-brand-assets.sh

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

# macOS Computer Use C0 Helper。开发构建使用稳定 identifier 的本地签名。
computer-use-helper-dev:
	@swift build --package-path native/macos/computer-use-helper --scratch-path bin/computer-use-helper-build -c debug
	@bash packaging/macos/create-computer-use-app.sh bin/computer-use-helper-build/debug/PuddingComputerUseHelper "bin/Pudding Computer Use.app" com.teatak.pudding.dev.computer-use-helper "Pudding Computer Use (Dev)"
	@identity="$${PUDDING_COMPUTER_USE_DEV_IDENTITY:-Pudding Dev Local}"; \
	if ! security find-identity -v -p codesigning | grep -Fq "\"$$identity\""; then \
		echo "warning: Computer Use dev signing identity '$$identity' not found; using ad-hoc signing (Accessibility must be added manually)." >&2; \
		identity="-"; \
	fi; \
	codesign --force --deep --sign "$$identity" "bin/Pudding Computer Use.app"
	@rm -f bin/PuddingComputerUseHelper

computer-use-helper-test:
	@swift test --package-path native/macos/computer-use-helper --scratch-path bin/computer-use-helper-build

# 确定性 macOS GUI fixture,只用于 Computer Use 开发和真实 TCC smoke。
computer-use-fixture-dev: computer-use-helper-dev
	@bash packaging/macos/create-computer-use-app.sh \
		bin/computer-use-helper-build/debug/PuddingComputerUseFixture \
		"bin/Pudding Computer Use Fixture.app" \
		com.teatak.pudding.computer-use-fixture \
		"Pudding Computer Use Fixture" \
		native/macos/computer-use-helper/Sources/PuddingComputerUseFixture/Info.plist
	@identity="$${PUDDING_COMPUTER_USE_DEV_IDENTITY:-Pudding Dev Local}"; \
	if ! security find-identity -v -p codesigning | grep -Fq "\"$$identity\""; then identity="-"; fi; \
	codesign --force --deep --sign "$$identity" "bin/Pudding Computer Use Fixture.app"

computer-use-fixture-smoke: computer-use-fixture-dev
	@node scripts/computer-use-fixture-smoke.cjs

# 产品链路 smoke: scripted model -> session approval -> Engine/Manager -> Electron bridge -> Helper -> fixture。
computer-use-product-smoke: computer-use-fixture-dev
	@node scripts/computer-use-product-smoke.cjs

# 真实系统 App smoke:在 Calculator 中执行 1+1=2,并验证 session-owned quit。
computer-use-calculator-smoke: computer-use-helper-dev
	@node scripts/computer-use-product-smoke.cjs --calculator

# 已运行 App smoke:session 可以操作 Calculator,但不能取得 launchID 或关闭它。
computer-use-calculator-existing-smoke: computer-use-helper-dev
	@node scripts/computer-use-product-smoke.cjs --calculator-existing

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

# 要求升级前后都包含 Computer Use Helper,并比较完整 designated requirement。
desktop-computer-use-update-test: desktop-verify
	@PUDDING_RELEASE_CHANNEL=stable PUDDING_UPDATE_TEST_REQUIRE_COMPUTER_USE_IDENTITY=1 node scripts/run-update-test.cjs

# 验证 Keychain 中的公证 profile 存在且 Apple 凭据有效。
desktop-notary-check:
	@xcrun notarytool history \
		--keychain-profile "$(PUDDING_NOTARY_PROFILE)" \
		--output-format json >/dev/null
	@echo "Notarization credentials are valid: $(PUDDING_NOTARY_PROFILE)"

# 重新保存公证凭据。应用专用密码由 notarytool 安全提示输入,不进入 Makefile。
desktop-notary-store:
	@xcrun notarytool store-credentials "$(PUDDING_NOTARY_PROFILE)" \
		--apple-id "$(PUDDING_NOTARY_APPLE_ID)" \
		--team-id "$(PUDDING_NOTARY_TEAM_ID)"

# 本机完成测试、tag、签名、公证，并上传 Draft Release。
desktop-publish:
	@PUDDING_RELEASE_CHANNEL=stable node scripts/release-local.cjs start

# Preview 与正式版共用 Pudding.app / appId / ~/.pudding,仅发布为 GitHub Prerelease beta 通道。
desktop-preview-bundle: desktop-runtimes
	@PUDDING_PACKAGING_PIPELINE=1 PUDDING_RELEASE_CHANNEL=preview node scripts/package-desktop.cjs

desktop-preview-verify:
	@PUDDING_PACKAGING_PIPELINE=1 PUDDING_RELEASE_CHANNEL=preview node scripts/package-desktop.cjs --verify-only

desktop-preview-update-test: desktop-preview-verify
	@PUDDING_RELEASE_CHANNEL=preview node scripts/run-update-test.cjs

# Preview beta 间升级时要求 Computer Use Helper 的完整 designated requirement 保持不变。
desktop-preview-computer-use-update-test: desktop-preview-verify
	@PUDDING_RELEASE_CHANNEL=preview PUDDING_UPDATE_TEST_REQUIRE_COMPUTER_USE_IDENTITY=1 node scripts/run-update-test.cjs

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

agent-eval:
	go run -tags "$(BUILDTAGS)" ./cmd/puddingd agent eval $(RUNARGS)

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
