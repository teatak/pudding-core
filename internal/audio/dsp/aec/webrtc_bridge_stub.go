//go:build !webrtcaec || !cgo || !darwin || (!arm64 && !amd64)

package aec

func newWebRTCAECBridge(WebRTCAECConfig) (webrtcAECBridge, error) {
	return nil, errBridgeNotLinked
}
