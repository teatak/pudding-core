package applog

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDailyWriterRotatesAndKeepsTenCalendarDays(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 16, 23, 59, 0, 0, time.Local)
	old := filepath.Join(dir, "puddingd-2026-07-06.log")
	boundary := filepath.Join(dir, "puddingd-2026-07-07.log")
	unrelated := filepath.Join(dir, "notes.log")
	for _, path := range []string{old, boundary, unrelated} {
		if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	w, err := newDailyWriter(dir, "puddingd", RetentionDays, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := w.Write([]byte("first\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("expired log still exists: %v", err)
	}
	for _, path := range []string{boundary, unrelated} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("retained file %s: %v", path, err)
		}
	}

	now = time.Date(2026, 7, 17, 0, 1, 0, 0, time.Local)
	if _, err := w.Write([]byte("second\n")); err != nil {
		t.Fatal(err)
	}
	assertFileContains(t, filepath.Join(dir, "puddingd-2026-07-16.log"), "first")
	assertFileContains(t, filepath.Join(dir, "puddingd-2026-07-17.log"), "second")
}

func TestDailyWriterCleansExpiredLegacyLogs(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "daemon.log")
	if err := os.WriteFile(legacy, []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2026, 6, 1, 0, 0, 0, 0, time.Local)
	if err := os.Chtimes(legacy, old, old); err != nil {
		t.Fatal(err)
	}
	w, err := newDailyWriter(dir, "puddingd", RetentionDays, func() time.Time {
		return time.Date(2026, 7, 16, 12, 0, 0, 0, time.Local)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("expired legacy log still exists: %v", err)
	}
}

func TestRedactAttrRemovesCredentialsAndUserText(t *testing.T) {
	if got := redactAttr(nil, slog.String("token", "secret")).Value.String(); got != "[REDACTED]" {
		t.Fatalf("token = %q", got)
	}
	if got := redactAttr(nil, slog.String("text", "private message")).Value.String(); got != "[REDACTED]" {
		t.Fatalf("text = %q", got)
	}
	got := redactText("GET /?token=abc&x=1 Authorization: Bearer xyz")
	if strings.Contains(got, "abc") || strings.Contains(got, "xyz") {
		t.Fatalf("credentials were not redacted: %s", got)
	}
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("%s = %q, want %q", path, data, want)
	}
}
