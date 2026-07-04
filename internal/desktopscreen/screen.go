package desktopscreen

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/kbinani/screenshot"
)

const (
	CodeFailed            = "screenshot_failed"
	CodeNoActiveDisplay   = "screenshot_no_active_display"
	CodeDisplayOutOfRange = "screenshot_display_out_of_range"
	CodeUnsupported       = "screenshot_unsupported"
)

type Screenshot struct {
	Data         []byte
	MIME         string
	Name         string
	Display      int
	DisplayCount int
	Width        int
	Height       int
	CapturedAt   time.Time
}

type Capturer interface {
	CaptureScreenshots(ctx context.Context, display *int) ([]Screenshot, error)
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
	var screenErr *Error
	if errors.As(err, &screenErr) && screenErr.Code != "" {
		return screenErr.Code
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return CodeFailed
	}
	return CodeFailed
}

type nativeCapturer struct{}

func New() Capturer {
	return nativeCapturer{}
}

func (nativeCapturer) CaptureScreenshots(ctx context.Context, display *int) ([]Screenshot, error) {
	if unsupportedScreenSession() {
		return nil, NewError(CodeUnsupported, "desktop screenshot unsupported in this session")
	}
	count := screenshot.NumActiveDisplays()
	if count <= 0 {
		return nil, NewError(CodeNoActiveDisplay, "no active display")
	}
	displays := make([]int, 0, count)
	if display != nil {
		if *display < 0 || *display >= count {
			return nil, NewError(CodeDisplayOutOfRange, fmt.Sprintf("display %d out of range, active displays: %d", *display, count))
		}
		displays = append(displays, *display)
	} else {
		for i := 0; i < count; i++ {
			displays = append(displays, i)
		}
	}

	now := time.Now().UTC()
	out := make([]Screenshot, 0, len(displays))
	for _, idx := range displays {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		img, err := screenshot.CaptureDisplay(idx)
		if err != nil {
			return nil, NewError(CodeFailed, err.Error())
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil, NewError(CodeFailed, err.Error())
		}
		name := Filename(now)
		if count > 1 {
			name = fmt.Sprintf("%s Display %d.png", strings.TrimSuffix(Filename(now), ".png"), idx+1)
		}
		bounds := img.Bounds()
		out = append(out, Screenshot{
			Data:         buf.Bytes(),
			MIME:         "image/png",
			Name:         name,
			Display:      idx,
			DisplayCount: count,
			Width:        bounds.Dx(),
			Height:       bounds.Dy(),
			CapturedAt:   now,
		})
	}
	return out, nil
}

func Filename(now time.Time) string {
	return "Screenshot " + now.Format("2006-01-02 15.04.05") + ".png"
}

func unsupportedScreenSession() bool {
	return runtime.GOOS == "linux" && os.Getenv("WAYLAND_DISPLAY") != "" && os.Getenv("DISPLAY") == ""
}
