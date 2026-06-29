MODULE := github.com/teatak/pudding-core
LDFLAGS_RELEASE := -X $(MODULE)/internal/buildinfo.channel=release

.PHONY: test tidy clean embed desktop desktop-dev daemon daemon-dev daemon-release package prompt

# 共享:构建前端并装填进 daemon 的 embed 目录(产物不进 git)
embed:
	cd web && npm run build
	rm -rf internal/webui/dist
	mkdir -p internal/webui/dist
	cp -R web/dist/ internal/webui/dist/
	touch internal/webui/dist/.gitkeep

# —— 桌面壳(内嵌 daemon + 原生窗口)——
# 构建(dev 通道;含 embed web)
desktop: embed
	go build -o bin/pudding-desktop ./cmd/pudding-desktop

# 热更循环:Wails AssetServer 托管 Vite(HMR),业务 API 直连 daemon;停旧实例后 detached 拉起
desktop-dev:
	@bash scripts/dev.sh desktop

# —— headless daemon(无窗口,浏览器访问)——
# 构建(dev 通道;含 embed web)
daemon: embed
	go build -o bin/puddingd ./cmd/puddingd

# 热更循环:停旧实例 → 起 puddingd + Vite,浏览器开脚本打印的带 token URL
daemon-dev:
	@bash scripts/dev.sh daemon

prompt:
	go run ./cmd/puddingd prompt $(RUNARGS)

# 发布构建(release 通道:端口 9669 / ~/.pudding;含 embed web)
daemon-release: embed
	go build -ldflags "$(LDFLAGS_RELEASE)" -o bin/puddingd ./cmd/puddingd

# 打包桌面壳为 unsigned .app + .dmg(release 通道)。用法: make package VERSION=v0.1.0
package:
	@bash scripts/package-macos.sh $(VERSION)

test:
	go test ./...

tidy:
	go mod tidy

clean:
	rm -rf bin
