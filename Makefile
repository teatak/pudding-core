MODULE := github.com/teatak/pudding-core
BUILDTAGS := sqlite_fts5 webrtcaec
DAEMON_OUT ?= bin/puddingd
ifeq ($(shell uname -s),Darwin)
SDKROOT ?= $(shell xcrun --sdk macosx --show-sdk-path 2>/dev/null)
export SDKROOT
endif

.PHONY: daemon daemon-dev daemon-release test test-code-unit test-code-host schema-check tidy clean language-servers language-servers-ready prompt tools-report tools-eval agent-eval runtime

daemon:
	go build -tags "$(BUILDTAGS)" -o "$(DAEMON_OUT)" ./cmd/puddingd

daemon-dev:
	go run -tags "$(BUILDTAGS)" ./cmd/puddingd $(RUNARGS)

daemon-release: schema-check
	go build -tags "$(BUILDTAGS)" -ldflags "-X $(MODULE)/internal/buildinfo.channel=release" -o "$(DAEMON_OUT)" ./cmd/puddingd

runtime:
	@bash scripts/build-runtime.sh $(ARCH) $(OUT)

language-servers:
	@bash scripts/prepare-language-servers.sh

language-servers-ready:
	@bash scripts/prepare-language-servers.sh --ensure

prompt:
	go run -tags "$(BUILDTAGS)" ./cmd/puddingd prompt $(RUNARGS)

tools-report:
	go run -tags "$(BUILDTAGS)" ./cmd/puddingd tools report $(RUNARGS)

tools-eval:
	go run -tags "$(BUILDTAGS)" ./cmd/puddingd tools eval $(RUNARGS)

# Launch outside go run so project commands inherit the caller's Go environment.
agent-eval: daemon
	"$(abspath $(DAEMON_OUT))" agent eval $(RUNARGS)

test:
	go test -tags "$(BUILDTAGS)" ./...

# Pure command/result regression subset, suitable inside a project sandbox.
test-code-unit:
	go test -tags "$(BUILDTAGS)" ./internal/tool ./internal/modeloutput -run '^(Test(CommandPreview|CompletedPollPreview|CommandSession|CommandProcessOutputText|LargeCommandPreview|BackgroundProcessOutputRing|SandboxGoDownload|MacOSCommandSandboxState|MacOSCommandSandboxMigrates|ExactText|InvalidDeclarations|WholeText|PresentationContract))' $(TESTARGS)

# Native command execution, PTY and Seatbelt integration require a host runner.
test-code-host:
	go test -tags "$(BUILDTAGS)" ./internal/tool ./internal/engine $(TESTARGS)

schema-check:
	go test -tags "$(BUILDTAGS)" ./internal/store/sqlitestore -run '^TestSchemaReleaseContract$$'

tidy:
	go mod tidy

clean:
	rm -rf bin
