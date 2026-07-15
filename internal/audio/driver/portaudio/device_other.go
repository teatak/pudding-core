//go:build !darwin

package portaudio

func installCoreAudioListener() {}

func coreAudioDeviceChanged() bool { return false }

func captureRoutingArbitrationNeeded() bool { return false }
