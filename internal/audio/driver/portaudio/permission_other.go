//go:build !darwin

package portaudio

import "context"

func requestMicrophonePermission(context.Context) error {
	return nil
}
