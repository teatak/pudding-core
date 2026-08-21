package portaudio

/*
#cgo darwin CFLAGS: -x objective-c -fobjc-arc -fblocks
#cgo darwin LDFLAGS: -framework AVFAudio -framework Foundation

#import <AVFAudio/AVFAudio.h>
#import <Foundation/Foundation.h>

// Returns 0 on success, 1 when arbitration fails, and 2 on timeout.
static int PuddingBeginCaptureRoutingArbitration(int timeoutSeconds, int *defaultDeviceChanged) {
	__block BOOL changed = NO;
	__block NSError *arbitrationError = nil;
	dispatch_semaphore_t semaphore = dispatch_semaphore_create(0);

	[[AVAudioRoutingArbiter sharedRoutingArbiter]
		beginArbitrationWithCategory:AVAudioRoutingArbitrationCategoryPlayAndRecordVoice
		completionHandler:^(BOOL didChange, NSError *error) {
			changed = didChange;
			arbitrationError = error;
			dispatch_semaphore_signal(semaphore);
		}];

	dispatch_time_t deadline = dispatch_time(DISPATCH_TIME_NOW, (int64_t)timeoutSeconds * NSEC_PER_SEC);
	if (dispatch_semaphore_wait(semaphore, deadline) != 0) {
		[[AVAudioRoutingArbiter sharedRoutingArbiter] leaveArbitration];
		return 2;
	}
	if (arbitrationError != nil) {
		[[AVAudioRoutingArbiter sharedRoutingArbiter] leaveArbitration];
		return 1;
	}
	if (defaultDeviceChanged != NULL) {
		*defaultDeviceChanged = changed ? 1 : 0;
	}
	return 0;
}

static void PuddingLeaveCaptureRoutingArbitration(void) {
	[[AVAudioRoutingArbiter sharedRoutingArbiter] leaveArbitration];
}
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/teatak/pudding-core/internal/audio/driver"
	"github.com/teatak/pudding-core/internal/audio/frame"
)

const routingArbitrationTimeoutSeconds = 5
const inputRoutePrimeLead = 200 * time.Millisecond

var (
	errRoutingArbitrationFailed  = errors.New("audio routing arbitration failed")
	errRoutingArbitrationTimeout = errors.New("audio routing arbitration timed out")
)

func beginCaptureRoutingArbitration(ctx context.Context) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var changed C.int
	code := int(C.PuddingBeginCaptureRoutingArbitration(routingArbitrationTimeoutSeconds, &changed))
	if err := ctx.Err(); err != nil {
		C.PuddingLeaveCaptureRoutingArbitration()
		return false, err
	}
	switch code {
	case 0:
		return changed != 0, nil
	case 1:
		return false, errRoutingArbitrationFailed
	default:
		return false, errRoutingArbitrationTimeout
	}
}

func leaveCaptureRoutingArbitration() {
	C.PuddingLeaveCaptureRoutingArbitration()
}

// PrimeInputRoute creates real output activity, waits only for the route lead,
// then lets the rest of the prompt continue while capture starts.
func (d *Driver) PrimeInputRoute(ctx context.Context, pcm frame.PCM16) error {
	if err := requestMicrophonePermission(ctx); err != nil {
		return fmt.Errorf("portaudio prime input route: %w", err)
	}
	d.mu.Lock()
	if !d.initialized {
		d.mu.Unlock()
		return driver.ErrNotStarted
	}
	if err := d.arbitrateCaptureRoutingLocked(ctx); err != nil {
		d.mu.Unlock()
		return fmt.Errorf("portaudio prime input route: %w", err)
	}
	d.mu.Unlock()

	if err := d.StartPlayback(ctx); err != nil {
		d.cancelInputRoutePrime()
		return fmt.Errorf("portaudio prime input route: %w", err)
	}
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- d.WritePlayback(ctx, pcm)
	}()
	timer := time.NewTimer(inputRoutePrimeLead)
	defer timer.Stop()
	select {
	case err := <-writeDone:
		if err == nil {
			return nil
		}
		_ = d.StopPlayback(context.Background())
		d.cancelInputRoutePrime()
		return fmt.Errorf("portaudio prime input route: %w", err)
	case <-timer.C:
		go logInputRoutePromptResult(writeDone)
		return nil
	case <-ctx.Done():
		_ = d.StopPlayback(context.Background())
		d.cancelInputRoutePrime()
		return ctx.Err()
	}
}

func logInputRoutePromptResult(done <-chan error) {
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, driver.ErrNotStarted) {
		slog.Warn("portaudio input route prompt playback failed", "err", err)
	}
}

func (d *Driver) cancelInputRoutePrime() {
	d.mu.Lock()
	d.leaveCaptureRoutingArbitrationLocked()
	d.mu.Unlock()
}
