// Package buildinfo 暴露构建期注入的通道信息。
package buildinfo

// channel 由发布构建注入:
//
//	go build -ldflags "-X github.com/teatak/pudding-core/internal/buildinfo.channel=release"
//
// 本地 go build / go run 不注入,默认 dev,保证开发期误操作只落在 dev home
// (docs/technology-decisions.md 第 10 节)。
var channel = "dev"

func Channel() string { return channel }

func IsRelease() bool { return channel == "release" }
