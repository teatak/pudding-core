package acp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const quotaResponse = `{
  "status":"SUCCESS",
  "command":{"name":"usage","data":{"groups":[
    {"name":"Gemini Models","buckets":[
      {"window":"weekly","remaining_fraction":0.8,"reset_time":"2026-11-30T12:00:00Z"},
      {"window":"5h","remaining_fraction":0},
      {"window":"credits","remaining_fraction":1}
    ]}
  ]}}
}`

func TestParseQuota(t *testing.T) {
	quota, err := parseQuota([]byte(quotaResponse))
	if err != nil || len(quota.Groups) != 1 {
		t.Fatalf("quota = %+v, err = %v", quota, err)
	}
	group := quota.Groups[0]
	if group.Name != "Gemini Models" || len(group.Windows) != 2 {
		t.Fatalf("group = %+v", group)
	}
	weekly, fiveHours := group.Windows[0], group.Windows[1]
	if weekly.Window != "weekly" || weekly.RemainingFraction != 0.8 || weekly.ResetAt == nil || weekly.ResetAt.Day() != 30 ||
		fiveHours.Window != "5h" || fiveHours.RemainingFraction != 0 || fiveHours.ResetAt != nil {
		t.Fatalf("windows = %+v", group.Windows)
	}
	if _, err := parseQuota([]byte(`{"status":"SUCCESS","response":"model prompt result"}`)); err == nil {
		t.Fatal("ordinary prompts must not be reported as quota")
	}
}

func TestReadQuotaChecksVersionBeforeInvokingUsage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX executable fixture")
	}
	dir := t.TempDir()
	called := filepath.Join(dir, "usage-invoked")
	t.Setenv("PUDDING_TEST_QUOTA_CALLED", called)
	for _, entry := range []struct {
		version string
		allowed bool
	}{{"1.1.10", false}, {"1.1.11", true}} {
		t.Run(entry.version, func(t *testing.T) {
			path := filepath.Join(dir, "agy")
			script := `#!/bin/sh
if [ "$1" = "--version" ]; then printf '%s\n' '` + entry.version + `'; exit 0; fi
: > "$PUDDING_TEST_QUOTA_CALLED"
if [ "$*" != '-p /usage --output-format json' ]; then exit 2; fi
cat <<'JSON'
` + quotaResponse + "\nJSON\n"
			if err := os.WriteFile(path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			quota, err := ReadQuota(context.Background(), []string{path})
			if entry.allowed {
				if err != nil || len(quota.Groups) != 1 {
					t.Fatalf("quota = %+v, err = %v", quota, err)
				}
			} else if !errors.Is(err, ErrQuotaCLITooOld) {
				t.Fatalf("expected a version error, got %v", err)
			}
			_, err = os.Stat(called)
			if entry.allowed && err != nil || !entry.allowed && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unexpected /usage invocation status: %v", err)
			}
		})
	}
}
