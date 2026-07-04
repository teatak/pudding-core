package portaudio

/*
#cgo darwin CFLAGS: -x objective-c -fobjc-arc -fblocks
#cgo darwin LDFLAGS: -framework AVFoundation -framework Foundation

#import <AVFoundation/AVFoundation.h>
#import <Foundation/Foundation.h>

static int PuddingRequestMicrophoneAccess(int timeoutSeconds) {
	AVAuthorizationStatus status = [AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeAudio];
	if (status == AVAuthorizationStatusAuthorized) {
		return 0;
	}
	if (status == AVAuthorizationStatusDenied || status == AVAuthorizationStatusRestricted) {
		return 1;
	}

	__block BOOL granted = NO;
	dispatch_semaphore_t semaphore = dispatch_semaphore_create(0);
	[AVCaptureDevice requestAccessForMediaType:AVMediaTypeAudio completionHandler:^(BOOL ok) {
		granted = ok;
		dispatch_semaphore_signal(semaphore);
	}];
	dispatch_time_t deadline = dispatch_time(DISPATCH_TIME_NOW, (int64_t)timeoutSeconds * NSEC_PER_SEC);
	if (dispatch_semaphore_wait(semaphore, deadline) != 0) {
		return 2;
	}
	return granted ? 0 : 1;
}
*/
import "C"

import (
	"context"
	"errors"
	"log/slog"
)

var (
	errMicrophonePermissionDenied  = errors.New("microphone permission denied")
	errMicrophonePermissionTimeout = errors.New("microphone permission request timed out")
)

func requestMicrophonePermission(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	slog.Info("portaudio: checking microphone permission")
	done := make(chan int, 1)
	go func() {
		done <- int(C.PuddingRequestMicrophoneAccess(60))
	}()
	select {
	case <-ctx.Done():
		slog.Warn("portaudio: microphone permission cancelled", "err", ctx.Err())
		return ctx.Err()
	case code := <-done:
		switch code {
		case 0:
			slog.Info("portaudio: microphone permission granted")
			return nil
		case 1:
			slog.Warn("portaudio: microphone permission denied")
			return errMicrophonePermissionDenied
		default:
			slog.Warn("portaudio: microphone permission timed out")
			return errMicrophonePermissionTimeout
		}
	}
}
