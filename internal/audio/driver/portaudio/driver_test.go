package portaudio

import (
	"reflect"
	"testing"
	"time"

	pa "github.com/gordonklaus/portaudio"
)

func TestCaptureCallbackKeepsCurrentBufferAfterInputOverflow(t *testing.T) {
	health := &captureHealth{}
	frames := make(chan []int16, 1)
	now := int64(100)
	input := []int16{11, -22, 33}

	enqueueCaptureFrame(health, frames, input, pa.InputOverflow, now)
	input[0] = 99

	select {
	case got := <-frames:
		if !reflect.DeepEqual(got, []int16{11, -22, 33}) {
			t.Fatalf("captured frame = %v", got)
		}
	default:
		t.Fatal("overflow callback dropped the current input buffer")
	}
	if got := health.overflows.Load(); got != 1 {
		t.Fatalf("overflow count = %d, want 1", got)
	}
	if got := health.lastCallback.Load(); got != now {
		t.Fatalf("last callback = %d", got)
	}
}

func TestCaptureStreamStalledRequiresStartupGraceAndMissingCallbacks(t *testing.T) {
	now := int64(10 * time.Second)
	health := &captureHealth{}
	health.startedAt.Store(now - captureStallAfter.Nanoseconds() - 1)
	if !health.stalled(now) {
		t.Fatal("stream without callbacks after startup grace should be stale")
	}
	health.startedAt.Store(now - captureStallAfter.Nanoseconds()/2)
	if health.stalled(now) {
		t.Fatal("stream should not be stale during startup grace")
	}
	health.startedAt.Store(now - captureStallAfter.Nanoseconds() - 1)
	health.lastCallback.Store(now - captureStallAfter.Nanoseconds()/2)
	if health.stalled(now) {
		t.Fatal("stream with a recent callback should be healthy")
	}
	health.lastCallback.Store(now - captureStallAfter.Nanoseconds() - 1)
	if !health.stalled(now) {
		t.Fatal("stream whose callbacks stopped should be stale")
	}
}

func TestCaptureQueuePressureDoesNotLookLikeAStalledDevice(t *testing.T) {
	now := int64(10 * time.Second)
	health := &captureHealth{}
	health.startedAt.Store(now - captureStallAfter.Nanoseconds() - 1)
	frames := make(chan []int16, 1)
	frames <- []int16{1}

	enqueueCaptureFrame(health, frames, []int16{2}, 0, now)

	if got := health.queueDrops.Load(); got != 1 {
		t.Fatalf("queue drops = %d, want 1", got)
	}
	if health.stalled(now) {
		t.Fatal("a live callback must keep the device healthy when the frame queue is full")
	}
}

func TestDetachedCaptureCallbackCannotKeepCurrentStreamHealthy(t *testing.T) {
	now := int64(10 * time.Second)
	oldHealth := &captureHealth{}
	currentHealth := &captureHealth{}
	oldHealth.startedAt.Store(now - captureStallAfter.Nanoseconds() - 1)
	currentHealth.startedAt.Store(now - captureStallAfter.Nanoseconds() - 1)

	enqueueCaptureFrame(oldHealth, make(chan []int16, 1), []int16{1}, 0, now)

	if got := currentHealth.lastCallback.Load(); got != 0 {
		t.Fatalf("current stream callback timestamp = %d, want 0", got)
	}
	if !currentHealth.stalled(now) {
		t.Fatal("old stream callback must not update current stream health")
	}
}
