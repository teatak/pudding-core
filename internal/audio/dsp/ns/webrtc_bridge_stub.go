//go:build !webrtcaec || !cgo || !darwin || (!arm64 && !amd64)

package ns

func newWebRTCNSBridge(_ int, _ int, _ string) (webrtcNSBridge, error) {
	return nil, errBridgeNotLinked
}
