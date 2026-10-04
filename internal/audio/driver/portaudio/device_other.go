//go:build !darwin && !windows

package portaudio

func installCoreAudioListener() {}

func coreAudioDeviceChanged() bool { return false }

func captureRoutingArbitrationNeeded() bool { return false }
