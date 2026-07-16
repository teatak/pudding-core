// Package home 解析数据目录与通道默认值。
// 隔离规则见 docs/technology-decisions.md 第 10 节:dev 一律 ~/.pudding-dev,
// release 一律 ~/.pudding。
package home

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	for _, d := range []string{dir, filepath.Join(dir, "data"), filepath.Join(dir, "config"), filepath.Join(dir, "logs"), filepath.Join(dir, "apps"), filepath.Join(dir, "skills"), filepath.Join(dir, "temp")} {
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

func TempPath(dir string) string { return filepath.Join(dir, "temp") }

// CodeScratchPath returns the session-isolated workspace used by Code mode
// when the session is not bound to a Project.
func CodeScratchPath(dir, sessionID string) string {
	dir = strings.TrimSpace(dir)
	sessionID = strings.TrimSpace(sessionID)
	if dir == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(TempPath(dir), ".code", safePathComponent(sessionID))
}

func PrepareCodeScratch(dir, sessionID string) (string, error) {
	if strings.TrimSpace(dir) == "" || strings.TrimSpace(sessionID) == "" {
		return "", errors.New("home: code scratch requires home and session id")
	}
	base, err := codeScratchBase(dir, true)
	if err != nil {
		return "", err
	}
	path := filepath.Join(base, safePathComponent(sessionID))
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", fmt.Errorf("home: prepare code scratch: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("home: code scratch must be a directory, not a symlink")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("home: resolve code scratch: %w", err)
	}
	if !pathWithin(resolved, base) {
		return "", errors.New("home: code scratch escapes temp directory")
	}
	return resolved, nil
}

func RemoveCodeScratch(dir, sessionID string) error {
	if strings.TrimSpace(dir) == "" || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	base, err := codeScratchBase(dir, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	path := filepath.Join(base, safePathComponent(sessionID))
	return os.RemoveAll(path)
}

func codeScratchBase(dir string, create bool) (string, error) {
	tempRoot := TempPath(strings.TrimSpace(dir))
	if create {
		if err := os.MkdirAll(tempRoot, 0o700); err != nil {
			return "", fmt.Errorf("home: prepare temp root: %w", err)
		}
	}
	tempInfo, err := os.Lstat(tempRoot)
	if err != nil {
		return "", err
	}
	if tempInfo.Mode()&os.ModeSymlink != 0 || !tempInfo.IsDir() {
		return "", errors.New("home: temp root must be a directory, not a symlink")
	}
	resolvedTemp, err := filepath.EvalSymlinks(tempRoot)
	if err != nil {
		return "", fmt.Errorf("home: resolve temp root: %w", err)
	}

	base := filepath.Join(tempRoot, ".code")
	if create {
		if err := os.MkdirAll(base, 0o700); err != nil {
			return "", fmt.Errorf("home: prepare code scratch root: %w", err)
		}
	}
	baseInfo, err := os.Lstat(base)
	if err != nil {
		return "", err
	}
	if baseInfo.Mode()&os.ModeSymlink != 0 || !baseInfo.IsDir() {
		return "", errors.New("home: code scratch root must be a directory, not a symlink")
	}
	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", fmt.Errorf("home: resolve code scratch root: %w", err)
	}
	if !pathWithin(resolvedBase, resolvedTemp) {
		return "", errors.New("home: code scratch root escapes temp directory")
	}
	return resolvedBase, nil
}

func pathWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func safePathComponent(value string) string {
	if len(value) <= 128 {
		safe := value != ""
		for _, r := range value {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
				continue
			}
			safe = false
			break
		}
		if safe {
			return value
		}
	}
	sum := sha256.Sum256([]byte(value))
	return "session-" + hex.EncodeToString(sum[:16])
}
