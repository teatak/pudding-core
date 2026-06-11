MODULE := github.com/teatak/pudding-core
LDFLAGS_RELEASE := -X $(MODULE)/internal/buildinfo.channel=release

.PHONY: dev release run test tidy clean web

# 构建前端并装填进 daemon 的 embed 目录(产物不进 git)
web:
	cd web && npm run build
	rm -rf internal/webui/dist
	mkdir -p internal/webui/dist
	cp -R web/dist/ internal/webui/dist/
	touch internal/webui/dist/.gitkeep

dev:
	go build -o bin/puddingd ./cmd/puddingd

release:
	go build -ldflags "$(LDFLAGS_RELEASE)" -o bin/puddingd ./cmd/puddingd

run:
	go run ./cmd/puddingd --mock

test:
	go test ./...

tidy:
	go mod tidy

clean:
	rm -rf bin
