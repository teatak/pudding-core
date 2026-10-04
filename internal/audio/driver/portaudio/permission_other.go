//go:build !darwin && !windows

package portaudio

import "context"

func requestMicrophonePermission(context.Context) error {
	return nil
}
