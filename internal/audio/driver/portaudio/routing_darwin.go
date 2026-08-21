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

	"github.com/teatak/pudding-core/internal/audio/driver"
	"github.com/teatak/pudding-core/internal/audio/frame"
)

const routingArbitrationTimeoutSeconds = 5

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

// PrimeInputRoute creates real output activity before capture starts. That is
// the signal AirPods automatic switching uses to move playback from iPhone to
// this Mac before the microphone stream is opened.
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
	if err := d.WritePlayback(ctx, pcm); err != nil {
		_ = d.StopPlayback(context.Background())
		d.cancelInputRoutePrime()
		return fmt.Errorf("portaudio prime input route: %w", err)
	}
	return nil
}

func (d *Driver) cancelInputRoutePrime() {
	d.mu.Lock()
	d.leaveCaptureRoutingArbitrationLocked()
	d.mu.Unlock()
}
