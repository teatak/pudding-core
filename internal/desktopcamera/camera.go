package desktopcamera

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	CodeFailed           = "camera_failed"
	CodePermissionDenied = "camera_permission_denied"
	CodeTimeout          = "camera_timeout"
	CodeUnavailable      = "camera_unavailable"
	CodeUnsupported      = "camera_unsupported"
)

const DefaultTimeout = 15 * time.Second

type Photo struct {
	Data []byte
	MIME string
	Name string
}

type Capturer interface {
	CapturePhoto(ctx context.Context) (*Photo, error)
}

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

func NewError(code, message string) error {
	return &Error{Code: code, Message: message}
}

func Code(err error) string {
	if err == nil {
		return ""
	}
	var cameraErr *Error
	if errors.As(err, &cameraErr) && cameraErr.Code != "" {
		return cameraErr.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return CodeTimeout
	}
	if errors.Is(err, context.Canceled) {
		return CodeTimeout
	}
	return CodeFailed
}

func Filename(now time.Time) string {
	return "Photo " + now.Format("2006-01-02 15.04.05") + ".jpg"
}

func timeoutContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, DefaultTimeout)
}

func emptyPhotoError() error {
	return NewError(CodeFailed, "camera returned empty photo")
}

func unsupportedError() error {
	return NewError(CodeUnsupported, "camera capture unsupported on this platform")
}

func wrapFailed(err error) error {
	if err == nil {
		return nil
	}
	var cameraErr *Error
	if errors.As(err, &cameraErr) {
		return err
	}
	return NewError(CodeFailed, fmt.Sprintf("capture photo: %v", err))
}
