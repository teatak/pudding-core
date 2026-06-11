MODULE := github.com/teatak/pudding-core
LDFLAGS_RELEASE := -X $(MODULE)/internal/buildinfo.channel=release

.PHONY: dev release run test tidy clean

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
