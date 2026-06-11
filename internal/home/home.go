// Package home 解析数据目录与通道默认值。
// 隔离规则见 docs/technology-decisions.md 第 10 节:dev 一律 ~/.pudding-core-dev,
// 旧版的 ~/.pudding 与 ~/.pudding-dev 一律不接触。
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
	return filepath.Join(usr, ".pudding-core-dev"), nil
}

// Prepare 创建 home 目录结构(第一阶段只有根目录与 logs/)。
func Prepare(dir string) error {
	for _, d := range []string{dir, filepath.Join(dir, "logs")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("home: mkdir %s: %w", d, err)
		}
	}
	return nil
}

// DefaultAddr 返回通道默认监听地址,两个通道可同时运行。
func DefaultAddr() string {
	if buildinfo.IsRelease() {
		return "127.0.0.1:9669"
	}
	return "127.0.0.1:9670"
}

func DBPath(dir string) string { return filepath.Join(dir, "pudding.db") }

func TokenPath(dir string) string { return filepath.Join(dir, "daemon.token") }
