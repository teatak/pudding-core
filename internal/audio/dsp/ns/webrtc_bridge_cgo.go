//go:build webrtcaec && cgo && darwin

package ns

/*
#cgo CFLAGS: -I${SRCDIR}/../webrtc_bridge/include
#cgo LDFLAGS: -L${SRCDIR}/../webrtc_bridge/lib -lkopi_webrtc_aec_bridge -lc++ -framework CoreFoundation -framework Foundation
#cgo pkg-config: absl_base absl_flags absl_strings absl_numeric absl_synchronization absl_bad_optional_access
#include "kopi_webrtc_aec_bridge.h"
*/
import "C"

import (
	"fmt"
	"unsafe"
)

type nativeWebRTCNSBridge struct {
	handle unsafe.Pointer
}

func newWebRTCNSBridge(sampleRate int, channels int, level string) (webrtcNSBridge, error) {
	handle := C.kopi_webrtc_ns_create(C.int(sampleRate), C.int(channels), C.int(webrtcNSLevelCode(level)))
	if handle == nil {
		return nil, fmt.Errorf("webrtc ns bridge create failed")
	}
	return &nativeWebRTCNSBridge{handle: handle}, nil
}

func (b *nativeWebRTCNSBridge) Process(in []int16, out []int16) error {
	if len(in) == 0 {
		return nil
	}
	if len(out) < len(in) {
		return fmt.Errorf("webrtc ns output buffer too small: got %d need %d", len(out), len(in))
	}
	rc := C.kopi_webrtc_ns_process(
		b.handle,
		(*C.int16_t)(unsafe.Pointer(&in[0])),
		C.int(len(in)),
		(*C.int16_t)(unsafe.Pointer(&out[0])),
	)
	if rc != 0 {
		return fmt.Errorf("webrtc ns process failed: rc=%d", int(rc))
	}
	return nil
}

func (b *nativeWebRTCNSBridge) SetLevel(level string) error {
	rc := C.kopi_webrtc_ns_set_level(b.handle, C.int(webrtcNSLevelCode(level)))
	if rc != 0 {
		return fmt.Errorf("webrtc ns set level failed: rc=%d", int(rc))
	}
	return nil
}

func (b *nativeWebRTCNSBridge) Reset() {
	if b == nil || b.handle == nil {
		return
	}
	C.kopi_webrtc_ns_reset(b.handle)
}

func (b *nativeWebRTCNSBridge) Close() error {
	if b == nil || b.handle == nil {
		return nil
	}
	C.kopi_webrtc_ns_destroy(b.handle)
	b.handle = nil
	return nil
}

func webrtcNSLevelCode(level string) int {
	switch NormalizeLevel(level) {
	case LevelOff:
		return 0
	case LevelLow:
		return 1
	case LevelHigh:
		return 3
	case LevelVeryHigh:
		return 4
	default:
		return 2
	}
}
