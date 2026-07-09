MODULE := github.com/teatak/pudding-core
LDFLAGS_RELEASE := -X $(MODULE)/internal/buildinfo.channel=release
BUILDTAGS := sqlite_fts5 webrtcaec

.PHONY: test tidy clean embed desktop desktop-dev desktop-release desktop-bundle daemon daemon-dev daemon-release prompt

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

# 热更循环:Electron shell + Vite(HMR) + daemon
desktop-dev:
	@BUILDTAGS="$(BUILDTAGS)" bash scripts/desktop-dev.sh

# 发布桌面 daemon(release 通道:端口 9669 / ~/.pudding;供 .app bundle 使用)
desktop-release: embed
	go build -tags "$(BUILDTAGS)" -ldflags "$(LDFLAGS_RELEASE)" -o bin/puddingd ./cmd/puddingd

# macOS .app bundle(签名/公证后续补齐)
desktop-bundle: desktop-release
	@bash scripts/desktop-bundle-macos.sh

# —— headless daemon(无窗口,浏览器访问)——
# 构建(dev 通道;含 embed web)
daemon: embed
	go build -tags "$(BUILDTAGS)" -o bin/puddingd ./cmd/puddingd

# 热更循环:停旧实例 → 起 puddingd + Vite,浏览器开脚本打印的带 token URL
daemon-dev:
	@BUILDTAGS="$(BUILDTAGS)" bash scripts/dev.sh daemon

prompt:
	go run -tags "$(BUILDTAGS)" ./cmd/puddingd prompt $(RUNARGS)

# 发布构建(release 通道:端口 9669 / ~/.pudding;含 embed web)
daemon-release: embed
	go build -tags "$(BUILDTAGS)" -ldflags "$(LDFLAGS_RELEASE)" -o bin/puddingd ./cmd/puddingd

test:
	go test -tags "$(BUILDTAGS)" ./...

tidy:
	go mod tidy

clean:
	rm -rf bin
