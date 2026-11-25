package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var ErrQuotaCLIUnavailable = errors.New("Antigravity CLI is unavailable")
var ErrQuotaCLITooOld = errors.New("Antigravity quota requires agy 1.1.11 or newer")

type QuotaSnapshot struct {
	Groups []QuotaGroup `json:"groups"`
}

type QuotaGroup struct {
	Name    string        `json:"name"`
	Windows []QuotaWindow `json:"windows"`
}

type QuotaWindow struct {
	Window            string     `json:"window"`
	RemainingFraction float64    `json:"remainingFraction"`
	ResetAt           *time.Time `json:"resetAt,omitempty"`
}

// ReadQuota uses the CLI's own sign-in and runs the native slash command in an
// empty directory. Validate the executable before passing /usage to print mode.
func ReadQuota(ctx context.Context, paths []string) (QuotaSnapshot, error) {
	if len(paths) == 0 {
		return QuotaSnapshot{}, ErrQuotaCLIUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("", "pudding-agy-quota-")
	if err != nil {
		return QuotaSnapshot{}, err
	}
	defer os.RemoveAll(dir)
	for _, path := range paths {
		version, err := runQuotaCLI(ctx, path, dir, "--version")
		if err != nil {
			if ctx.Err() != nil {
				return QuotaSnapshot{}, ctx.Err()
			}
			continue
		}
		if !supportsQuota(string(version)) {
			return QuotaSnapshot{}, ErrQuotaCLITooOld
		}
		output, err := runQuotaCLI(ctx, path, dir, "-p", "/usage", "--output-format", "json")
		if err != nil {
			return QuotaSnapshot{}, fmt.Errorf("Antigravity /usage failed: %w", err)
		}
		return parseQuota(output)
	}
	return QuotaSnapshot{}, ErrQuotaCLIUnavailable
}

var quotaVersion = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)\.([0-9]+)(?:\+[0-9A-Za-z.-]+)?$`)

func supportsQuota(version string) bool {
	parts := quotaVersion.FindStringSubmatch(strings.TrimSpace(version))
	if parts == nil {
		return false
	}
	for i, minimum := range []int{1, 1, 11} {
		actual, err := strconv.Atoi(parts[i+1])
		if err != nil {
			return false
		}
		if actual != minimum {
			return actual > minimum
		}
	}
	return true
}

func runQuotaCLI(ctx context.Context, path, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	configureProcess(cmd)
	cmd.Cancel = func() error { return terminateProcess(cmd) }
	cmd.WaitDelay = time.Second
	return cmd.Output()
}

func parseQuota(output []byte) (QuotaSnapshot, error) {
	var result struct {
		Status  string `json:"status"`
		Command struct {
			Name string `json:"name"`
			Data struct {
				Groups []struct {
					Name    string `json:"name"`
					Buckets []struct {
						Window            string     `json:"window"`
						RemainingFraction *float64   `json:"remaining_fraction"`
						ResetTime         *time.Time `json:"reset_time"`
					} `json:"buckets"`
				} `json:"groups"`
			} `json:"data"`
		} `json:"command"`
	}
	if err := json.Unmarshal(output, &result); err != nil || result.Status != "SUCCESS" || result.Command.Name != "usage" {
		return QuotaSnapshot{}, errors.New("Antigravity /usage returned an invalid response")
	}
	var snapshot QuotaSnapshot
	for _, group := range result.Command.Data.Groups {
		entry := QuotaGroup{Name: group.Name}
		for _, bucket := range group.Buckets {
			if bucket.Window != "5h" && bucket.Window != "weekly" {
				continue
			}
			if bucket.RemainingFraction == nil || *bucket.RemainingFraction < 0 || *bucket.RemainingFraction > 1 {
				return QuotaSnapshot{}, errors.New("Antigravity /usage returned an invalid quota")
			}
			entry.Windows = append(entry.Windows, QuotaWindow{
				Window: bucket.Window, RemainingFraction: *bucket.RemainingFraction, ResetAt: bucket.ResetTime,
			})
		}
		if entry.Name != "" && len(entry.Windows) > 0 {
			snapshot.Groups = append(snapshot.Groups, entry)
		}
	}
	if len(snapshot.Groups) == 0 {
		return QuotaSnapshot{}, errors.New("Antigravity /usage returned no quota windows")
	}
	return snapshot, nil
}
