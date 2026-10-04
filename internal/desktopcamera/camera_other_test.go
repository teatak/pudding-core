//go:build !darwin

package desktopcamera

import "testing"

func TestUnsupportedCameraHasNoImplementation(t *testing.T) {
	if New() != nil {
		t.Fatal("unsupported platform must not expose a camera implementation")
	}
}
