// Package applog provides low-volume, daily application logs with bounded
// retention. It intentionally keeps logging independent from daemon state so
// startup failures (including listen errors) are recorded too.
package applog

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const RetentionDays = 10

var (
	querySecretPattern = regexp.MustCompile(`(?i)([?&](?:token|access_token|refresh_token|id_token|code)=)[^&\s"'<>]+`)
	bearerPattern      = regexp.MustCompile(`(?i)\b(Bearer\s+)[A-Za-z0-9._~+/=-]+`)
	fieldSecretPattern = regexp.MustCompile(`(?i)((?:token|access_token|refresh_token|id_token|api[_-]?key|authorization|password|secret)\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s,;]+)`)
)

// Install configures the process-wide slog logger to write to stderr and a
// daily file for the lifetime of the process.
func Install(logsDir, prefix string) error {
	writer, err := newDailyWriter(logsDir, prefix, RetentionDays, time.Now)
	if err != nil {
		return err
	}
	handler := slog.NewTextHandler(io.MultiWriter(os.Stderr, writer), &slog.HandlerOptions{
		Level:       slog.LevelInfo,
		ReplaceAttr: redactAttr,
	})
	slog.SetDefault(slog.New(handler))
	return nil
}

type dailyWriter struct {
	mu            sync.Mutex
	dir           string
	prefix        string
	retentionDays int
	now           func() time.Time
	day           string
	file          *os.File
}

func newDailyWriter(dir, prefix string, retentionDays int, now func() time.Time) (*dailyWriter, error) {
	if strings.TrimSpace(prefix) == "" {
		return nil, fmt.Errorf("applog: empty log prefix")
	}
	if retentionDays < 1 {
		return nil, fmt.Errorf("applog: retention days must be positive")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("applog: create logs directory: %w", err)
	}
	w := &dailyWriter{dir: dir, prefix: prefix, retentionDays: retentionDays, now: now}
	if err := w.rotateLocked(now()); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *dailyWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.rotateLocked(w.now()); err != nil {
		return 0, err
	}
	return w.file.Write(p)
}

func (w *dailyWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *dailyWriter) rotateLocked(now time.Time) error {
	day := now.Format("2006-01-02")
	if w.file != nil && w.day == day {
		return nil
	}
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	cleanupErr := cleanupExpired(w.dir, w.prefix, w.retentionDays, now)
	path := filepath.Join(w.dir, fmt.Sprintf("%s-%s.log", w.prefix, day))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("applog: open daily log: %w", err)
	}
	w.day = day
	w.file = file
	if cleanupErr != nil {
		fmt.Fprintf(os.Stderr, "puddingd: cleanup logs: %v\n", cleanupErr)
	}
	return nil
}

func cleanupExpired(dir, prefix string, retentionDays int, now time.Time) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("applog: read logs directory: %w", err)
	}
	cutoff := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(retentionDays - 1))
	filePrefix := prefix + "-"
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, filePrefix) && strings.HasSuffix(name, ".log") {
			dayText := strings.TrimSuffix(strings.TrimPrefix(name, filePrefix), ".log")
			day, parseErr := time.ParseInLocation("2006-01-02", dayText, now.Location())
			if parseErr == nil && day.Before(cutoff) {
				if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("applog: remove expired log %s: %w", name, err)
				}
			}
			continue
		}
		// Remove the two file names used by the retired desktop logger once
		// they are outside the same retention window.
		if prefix == "puddingd" && (name == "daemon.log" || name == "puddingd.log") {
			info, statErr := entry.Info()
			if statErr != nil {
				return fmt.Errorf("applog: stat legacy log %s: %w", name, statErr)
			}
			if info.ModTime().Before(cutoff) {
				if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("applog: remove legacy log %s: %w", name, err)
				}
			}
		}
	}
	return nil
}

func redactAttr(_ []string, attr slog.Attr) slog.Attr {
	key := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(attr.Key, "_", ""), "-", ""))
	if strings.Contains(key, "token") || strings.Contains(key, "secret") || strings.Contains(key, "password") ||
		strings.Contains(key, "authorization") || strings.Contains(key, "cookie") || strings.Contains(key, "apikey") ||
		key == "text" || key == "prompt" || key == "content" || key == "transcript" {
		return slog.String(attr.Key, "[REDACTED]")
	}
	switch attr.Value.Kind() {
	case slog.KindString:
		return slog.String(attr.Key, redactText(attr.Value.String()))
	case slog.KindAny:
		if err, ok := attr.Value.Any().(error); ok {
			return slog.String(attr.Key, redactText(err.Error()))
		}
	}
	return attr
}

func redactText(value string) string {
	value = querySecretPattern.ReplaceAllString(value, `${1}[REDACTED]`)
	value = bearerPattern.ReplaceAllString(value, `${1}[REDACTED]`)
	return fieldSecretPattern.ReplaceAllString(value, `${1}[REDACTED]`)
}
