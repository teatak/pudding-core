//go:build webrtcaec && cgo && darwin && arm64

package aec

/*
#cgo CFLAGS: -I${SRCDIR}/../webrtc_bridge/include
#cgo LDFLAGS: -L${SRCDIR}/../webrtc_bridge/lib/darwin-arm64 -lkopi_webrtc_aec_bridge -lc++ -framework CoreFoundation -framework Foundation
#cgo pkg-config: absl_base absl_flags absl_strings absl_numeric absl_synchronization absl_bad_optional_access
#include "kopi_webrtc_aec_bridge.h"
*/
import "C"

import (
	"fmt"
	"unsafe"
)

type nativeWebRTCAECBridge struct {
	handle unsafe.Pointer
}

func newWebRTCAECBridge(cfg WebRTCAECConfig) (webrtcAECBridge, error) {
	handle := C.kopi_webrtc_aec_create(C.int(cfg.SampleRate), C.int(cfg.Channels))
	if handle == nil {
		return nil, fmt.Errorf("webrtc aec bridge create failed")
	}
	return &nativeWebRTCAECBridge{handle: handle}, nil
}

func (b *nativeWebRTCAECBridge) AnalyzeRender(frame []int16) error {
	if len(frame) == 0 {
		return nil
	}
	rc := C.kopi_webrtc_aec_analyze_render(
		b.handle,
		(*C.int16_t)(unsafe.Pointer(&frame[0])),
		C.int(len(frame)),
	)
	if rc != 0 {
		return fmt.Errorf("webrtc aec analyze render failed: rc=%d", int(rc))
	}
	return nil
}

func (b *nativeWebRTCAECBridge) ProcessCapture(in []int16, out []int16) error {
	if len(in) == 0 {
		return nil
	}
	if len(out) < len(in) {
		return fmt.Errorf("webrtc aec output buffer too small: got %d need %d", len(out), len(in))
	}
	rc := C.kopi_webrtc_aec_process_capture(
		b.handle,
		(*C.int16_t)(unsafe.Pointer(&in[0])),
		C.int(len(in)),
		(*C.int16_t)(unsafe.Pointer(&out[0])),
	)
	if rc != 0 {
		return fmt.Errorf("webrtc aec process capture failed: rc=%d", int(rc))
	}
	return nil
}

func (b *nativeWebRTCAECBridge) Reset() {
	if b == nil || b.handle == nil {
		return
	}
	C.kopi_webrtc_aec_reset(b.handle)
}

func (b *nativeWebRTCAECBridge) Close() error {
	if b == nil || b.handle == nil {
		return nil
	}
	C.kopi_webrtc_aec_destroy(b.handle)
	b.handle = nil
	return nil
}
