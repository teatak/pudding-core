package tool

import (
	"context"
	"testing"
)

func TestBuiltinDefinitionsFollowRuntimeImplementations(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []BuiltinOption
		want map[string]bool
	}{
		{name: "unavailable", want: map[string]bool{}},
		{name: "screen only", opts: []BuiltinOption{WithDesktopScreen(fakeToolScreen{})}, want: map[string]bool{DesktopScreenshot: true}},
		{name: "native", opts: []BuiltinOption{WithComputer(&fakeComputerController{}), WithCamera(fakeToolCamera{}), WithDesktopScreen(fakeToolScreen{})}, want: map[string]bool{ComputerListApps: true, ComputerUseApp: true, ComputerQuitApp: true, ComputerObserve: true, ComputerAct: true, CameraCapture: true, DesktopScreenshot: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := NewBuiltinRunner(tc.opts...)
			defs, err := runner.Definitions(context.Background(), "session")
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{ComputerListApps, ComputerUseApp, ComputerQuitApp, ComputerObserve, ComputerAct, CameraCapture, DesktopScreenshot} {
				if HasDefinition(defs, name) != tc.want[name] {
					t.Errorf("%s availability = %v, want %v", name, HasDefinition(defs, name), tc.want[name])
				}
			}
			if !HasDefinition(defs, FileRead) {
				t.Fatal("unrelated core tool hidden")
			}
		})
	}
}
