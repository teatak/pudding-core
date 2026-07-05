//go:build !webrtcaec || !cgo || !darwin

package ns

func newWebRTCNSBridge(_ int, _ int, _ string) (webrtcNSBridge, error) {
	return nil, errBridgeNotLinked
}
