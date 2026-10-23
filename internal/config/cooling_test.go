package config

import (
	"context"
	"testing"
)

func TestCoolingSettingsDefaultsPersistenceAndValidation(t *testing.T) {
	ctx, home := context.Background(), t.TempDir()
	m := NewManager(home)
	if err := m.Prepare(); err != nil {
		t.Fatal(err)
	}
	defaults, err := m.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if defaults[SettingToolResultCoolingEnabled] != "true" || defaults[SettingToolResultCoolingKeepTurns] != "2" {
		t.Fatal(defaults)
	}
	if err := m.SetSettings(ctx, map[string]string{SettingToolResultCoolingEnabled: "false", SettingToolResultCoolingKeepTurns: "0"}); err != nil {
		t.Fatal(err)
	}
	m = NewManager(home)
	if err := m.Prepare(); err != nil {
		t.Fatal(err)
	}
	got, err := m.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got[SettingToolResultCoolingEnabled] != "false" || got[SettingToolResultCoolingKeepTurns] != "0" {
		t.Fatal(got)
	}
	for _, value := range []string{"-1", "51", "1.5", "invalid"} {
		if err := m.SetSettings(ctx, map[string]string{SettingToolResultCoolingKeepTurns: value}); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	if err := m.SetSettings(ctx, map[string]string{SettingToolResultCoolingEnabled: "invalid"}); err == nil {
		t.Fatal("accepted invalid boolean")
	}
	reset, err := m.ResetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if reset[SettingToolResultCoolingEnabled] != "true" || reset[SettingToolResultCoolingKeepTurns] != "2" {
		t.Fatal(reset)
	}

}
