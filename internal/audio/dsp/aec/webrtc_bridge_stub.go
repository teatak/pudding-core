//go:build !webrtcaec || !cgo || !darwin

package aec

func newWebRTCAECBridge(WebRTCAECConfig) (webrtcAECBridge, error) {
	return nil, errBridgeNotLinked
}
