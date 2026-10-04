package plugin

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBuiltinCatalogFiltersUnavailableToolsBeforeEnablement(t *testing.T) {
	ctx := context.Background()
	cfg := &fakePluginConfig{enabled: map[string]bool{BuiltinComputerUseID: true, BuiltinCaptureID: true}}
	svc := NewService(t.TempDir(), cfg).WithBuiltinToolAvailability(func(name string) bool {
		return name != toolCameraCapture && !strings.HasPrefix(name, "builtin_computer_")
	})
	defs, err := svc.ListDefinitions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if definitionByID(defs, BuiltinComputerUseID) != nil {
		t.Fatal("unavailable plugin exposed")
	}
	capture := definitionByID(defs, BuiltinCaptureID)
	if capture == nil || !capture.Enabled || len(capture.Tools) != 1 || capture.Tools[0].Name != toolDesktopScreenshot {
		t.Fatalf("partial capture capability: %+v", capture)
	}
	if _, err := svc.SetEnabled(ctx, BuiltinComputerUseID, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("enabled unavailable plugin: %v", err)
	}
	if _, err := svc.ReadSkill(ctx, BuiltinComputerUseID, BuiltinComputerUseID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("read unavailable skill: %v", err)
	}
	if !cfg.enabled[BuiltinComputerUseID] {
		t.Fatal("runtime availability overwrote user preference")
	}
	// Filtering must not mutate the global registry or another daemon's catalog.
	full, _ := NewService(t.TempDir(), cfg).ListDefinitions(ctx)
	if definitionByID(full, BuiltinComputerUseID) == nil || len(definitionByID(full, BuiltinCaptureID).Tools) != 2 {
		t.Fatal("shared catalog was mutated")
	}
	svc.WithBuiltinToolAvailability(func(name string) bool { return name != toolDesktopScreenshot && name != toolCameraCapture })
	defs, err = svc.ListDefinitions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if definitionByID(defs, BuiltinCaptureID) != nil {
		t.Fatal("empty capture plugin exposed")
	}
}
