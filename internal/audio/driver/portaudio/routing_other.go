//go:build !darwin

package portaudio

import "context"

func beginCaptureRoutingArbitration(context.Context) (bool, error) {
	return false, nil
}

func leaveCaptureRoutingArbitration() {}
