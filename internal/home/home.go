// Package home 解析数据目录与通道默认值。
// 隔离规则见 docs/technology-decisions.md 第 10 节:dev 一律 ~/.pudding-dev,
// release 一律 ~/.pudding。
package home

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/teatak/pudding-core/internal/buildinfo"
)

// Resolve 按 flag > PUDDING_HOME > 通道默认值的顺序确定 home 目录。
func Resolve(flagValue string) (string, error) {
	if flagValue != "" {
		return filepath.Abs(flagValue)
	}
	if env := os.Getenv("PUDDING_HOME"); env != "" {
		return filepath.Abs(env)
	}
	usr, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home: resolve user home: %w", err)
	}
	if buildinfo.IsRelease() {
		return filepath.Join(usr, ".pudding"), nil
	}
	return filepath.Join(usr, ".pudding-dev"), nil
}

// Prepare 创建 home 目录结构。
func Prepare(dir string) error {
	for _, d := range []string{dir, filepath.Join(dir, "data"), filepath.Join(dir, "logs")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("home: mkdir %s: %w", d, err)
		}
	}
	return nil
}

// 端口规划:release 占紧凑主段(9669 CLI / 9670 壳),dev 挪远避让
// (9679 CLI / 9680 壳)。同一通道的 CLI 与壳是同一 daemon 的两种宿主,
// 正常使用不并存(同 home 同库);分端口只为开发期偶发并行不打架,
// 以及壳的 loopback origin 稳定(localStorage 按 origin 含端口隔离,
// 端口漂移会让 UI 偏好整体丢失)。

// DefaultAddr 返回 CLI daemon 的通道默认监听地址。
func DefaultAddr() string {
	if buildinfo.IsRelease() {
		return "127.0.0.1:9669"
	}
	return "127.0.0.1:9679"
}

// DefaultDesktopAddr 返回桌面壳的通道默认地址。
func DefaultDesktopAddr() string {
	if buildinfo.IsRelease() {
		return "127.0.0.1:9670"
	}
	return "127.0.0.1:9680"
}

func DBPath(dir string) string { return filepath.Join(dir, "data", "pudding.db") }

func TokenPath(dir string) string { return filepath.Join(dir, "daemon.token") }
