//go:build !darwin

package desktopcamera

import "context"

type nativeCapturer struct{}

func New() Capturer {
	return nativeCapturer{}
}

func (nativeCapturer) CapturePhoto(context.Context) (*Photo, error) {
	return nil, unsupportedError()
}
