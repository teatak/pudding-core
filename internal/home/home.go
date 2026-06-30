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
	for _, d := range []string{dir, filepath.Join(dir, "data"), filepath.Join(dir, "config"), filepath.Join(dir, "logs"), filepath.Join(dir, "apps"), filepath.Join(dir, "skills")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("home: mkdir %s: %w", d, err)
		}
	}
	return nil
}

// DefaultAddr 返回通道默认监听地址(release 9669 / dev 9679)。
// 每通道**只有一个端口**:CLI 与桌面壳是同一 daemon 的两种宿主
// (同 home 同库,单写者),绝不并存双跑;壳启动时端口被活的
// pudding daemon 占用则 attach(直连 + 读 daemon.token),保证
// loopback origin 稳定(localStorage 按 origin 含端口隔离)。
func DefaultAddr() string {
	if buildinfo.IsRelease() {
		return "127.0.0.1:9669"
	}
	return "127.0.0.1:9679"
}

func DBPath(dir string) string { return filepath.Join(dir, "data", "pudding.db") }

func TokenPath(dir string) string { return filepath.Join(dir, "daemon.token") }

func MobileDevicesPath(dir string) string { return filepath.Join(dir, "config", "mobile-devices.json") }

func AppsPath(dir string) string { return filepath.Join(dir, "apps") }

func SkillsPath(dir string) string { return filepath.Join(dir, "skills") }
