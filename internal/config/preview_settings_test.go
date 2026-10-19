package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviewSettingsAreIndependentAndPersistAcrossRestart(t *testing.T) {
	home := t.TempDir()
	m := NewManager(home)
	if err := m.Prepare(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	keys := []string{SettingShowPluginPreviewVersions, SettingShowWidgetPreviewVersions}
	initial, err := m.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if initial[key] != "false" {
			t.Fatal(initial)
		}
	}
	// The former frontend key was rejected, which made the plugin toggle fail.
	if err := m.SetSettings(ctx, map[string]string{"show_app_preview_versions": "true"}); !errors.Is(err, ErrInvalidSetting) {
		t.Fatal(err)
	}
	for _, key := range keys {
		if err := m.SetSettings(ctx, map[string]string{key: "true"}); err != nil {
			t.Fatal(err)
		}
		m = NewManager(home)
		got, err := m.Settings(ctx)
		if err != nil || got[key] != "true" {
			t.Fatal(got, err)
		}
	}
	if err := m.SetSettings(ctx, map[string]string{SettingShowPluginPreviewVersions: "false"}); err != nil {
		t.Fatal(err)
	}
	got, err := NewManager(home).Settings(ctx)
	if err != nil || got[SettingShowPluginPreviewVersions] != "false" || got[SettingShowWidgetPreviewVersions] != "true" {
		t.Fatal(got, err)
	}
	before, err := os.ReadFile(filepath.Join(home, "config/settings.yaml"))
	if err != nil || !strings.Contains(string(before), "widgets:") {
		t.Fatal(string(before), err)
	}
	if err := m.SetSettings(ctx, map[string]string{SettingShowWidgetPreviewVersions: "invalid"}); !errors.Is(err, ErrInvalidSetting) {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(home, "config/settings.yaml"))
	if err != nil || string(before) != string(after) {
		t.Fatal("invalid setting changed saved preferences", err)
	}
	reset, err := m.ResetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if reset[key] != "false" {
			t.Fatal(reset)
		}
	}
	got, err = NewManager(home).Settings(ctx)
	if err != nil || got[SettingShowPluginPreviewVersions] != "false" || got[SettingShowWidgetPreviewVersions] != "false" {
		t.Fatal(got, err)
	}
}
