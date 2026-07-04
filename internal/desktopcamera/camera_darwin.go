package desktopcamera

/*
#cgo darwin CFLAGS: -x objective-c -fobjc-arc -fblocks
#cgo darwin LDFLAGS: -framework AVFoundation -framework Foundation -framework CoreMedia -framework CoreVideo -framework CoreImage -framework CoreGraphics -framework ImageIO

#import <AVFoundation/AVFoundation.h>
#import <CoreFoundation/CoreFoundation.h>
#import <CoreGraphics/CoreGraphics.h>
#import <CoreImage/CoreImage.h>
#import <CoreMedia/CoreMedia.h>
#import <CoreVideo/CoreVideo.h>
#import <Foundation/Foundation.h>
#import <ImageIO/ImageIO.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
	void *data;
	long length;
	char *code;
	char *message;
} PuddingCameraResult;

static char *puddingCString(NSString *value) {
	if (value == nil) {
		return strdup("");
	}
	return strdup([value UTF8String]);
}

static void puddingCameraError(PuddingCameraResult *result, const char *code, NSString *message) {
	result->code = strdup(code);
	result->message = puddingCString(message);
}

@interface PuddingFrameDelegate : NSObject<AVCaptureVideoDataOutputSampleBufferDelegate>
@property dispatch_semaphore_t semaphore;
@property NSData *data;
@property NSError *error;
@property NSInteger frameCount;
@property CFAbsoluteTime firstFrameAt;
@property BOOL done;
@end

@implementation PuddingFrameDelegate
- (void)captureOutput:(AVCaptureOutput *)output
 didOutputSampleBuffer:(CMSampleBufferRef)sampleBuffer
        fromConnection:(AVCaptureConnection *)connection {
	if (self.done) {
		return;
	}
	self.frameCount += 1;
	if (self.frameCount == 1) {
		self.firstFrameAt = CFAbsoluteTimeGetCurrent();
	}
	CFTimeInterval warmup = CFAbsoluteTimeGetCurrent() - self.firstFrameAt;
	if (self.frameCount < 12 || warmup < 1.0) {
		return;
	}
	self.done = YES;

	CVImageBufferRef pixelBuffer = CMSampleBufferGetImageBuffer(sampleBuffer);
	if (pixelBuffer == NULL) {
		self.error = [NSError errorWithDomain:@"PuddingCamera" code:1 userInfo:@{NSLocalizedDescriptionKey: @"Camera frame has no image buffer"}];
		dispatch_semaphore_signal(self.semaphore);
		return;
	}

	CIImage *image = [CIImage imageWithCVPixelBuffer:pixelBuffer];
	CIContext *context = [CIContext contextWithOptions:nil];
	CGColorSpaceRef colorSpace = CGColorSpaceCreateDeviceRGB();
	if (colorSpace == NULL) {
		self.error = [NSError errorWithDomain:@"PuddingCamera" code:2 userInfo:@{NSLocalizedDescriptionKey: @"Unable to create camera color space"}];
		dispatch_semaphore_signal(self.semaphore);
		return;
	}
	NSDictionary *options = @{(id)kCGImageDestinationLossyCompressionQuality: @0.92};
	NSData *jpeg = [context JPEGRepresentationOfImage:image colorSpace:colorSpace options:options];
	CGColorSpaceRelease(colorSpace);
	if (jpeg == nil || jpeg.length == 0) {
		self.error = [NSError errorWithDomain:@"PuddingCamera" code:3 userInfo:@{NSLocalizedDescriptionKey: @"Unable to encode camera frame"}];
		dispatch_semaphore_signal(self.semaphore);
		return;
	}

	self.data = jpeg;
	dispatch_semaphore_signal(self.semaphore);
}
@end

static BOOL puddingRequestCameraAccess(int timeoutSeconds) {
	AVAuthorizationStatus status = [AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeVideo];
	if (status == AVAuthorizationStatusAuthorized) {
		return YES;
	}
	if (status == AVAuthorizationStatusDenied || status == AVAuthorizationStatusRestricted) {
		return NO;
	}

	__block BOOL granted = NO;
	dispatch_semaphore_t semaphore = dispatch_semaphore_create(0);
	[AVCaptureDevice requestAccessForMediaType:AVMediaTypeVideo completionHandler:^(BOOL ok) {
		granted = ok;
		dispatch_semaphore_signal(semaphore);
	}];
	dispatch_time_t deadline = dispatch_time(DISPATCH_TIME_NOW, (int64_t)timeoutSeconds * NSEC_PER_SEC);
	if (dispatch_semaphore_wait(semaphore, deadline) != 0) {
		return NO;
	}
	return granted;
}

static PuddingCameraResult PuddingCapturePhotoDarwin(int timeoutSeconds) {
	PuddingCameraResult result;
	memset(&result, 0, sizeof(result));
	@autoreleasepool {
		AVAuthorizationStatus status = [AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeVideo];
		if (status != AVAuthorizationStatusAuthorized && !puddingRequestCameraAccess(timeoutSeconds)) {
			puddingCameraError(&result, "camera_permission_denied", @"Camera permission denied");
			return result;
		}

		AVCaptureDevice *device = [AVCaptureDevice defaultDeviceWithMediaType:AVMediaTypeVideo];
		if (device == nil) {
			puddingCameraError(&result, "camera_unavailable", @"No camera device is available");
			return result;
		}

		NSError *inputError = nil;
		AVCaptureDeviceInput *input = [AVCaptureDeviceInput deviceInputWithDevice:device error:&inputError];
		if (input == nil || inputError != nil) {
			puddingCameraError(&result, "camera_failed", inputError.localizedDescription ?: @"Unable to open camera");
			return result;
		}

		AVCaptureSession *session = [[AVCaptureSession alloc] init];
		if ([session canSetSessionPreset:AVCaptureSessionPresetHigh]) {
			session.sessionPreset = AVCaptureSessionPresetHigh;
		}
		if (![session canAddInput:input]) {
			puddingCameraError(&result, "camera_failed", @"Unable to add camera input");
			return result;
		}
		[session addInput:input];

		PuddingFrameDelegate *delegate = [[PuddingFrameDelegate alloc] init];
		delegate.semaphore = dispatch_semaphore_create(0);

		AVCaptureVideoDataOutput *output = [[AVCaptureVideoDataOutput alloc] init];
		output.alwaysDiscardsLateVideoFrames = YES;
		output.videoSettings = @{(id)kCVPixelBufferPixelFormatTypeKey: @(kCVPixelFormatType_32BGRA)};
		dispatch_queue_t queue = dispatch_queue_create("com.teatak.pudding.camera.frame", DISPATCH_QUEUE_SERIAL);
		[output setSampleBufferDelegate:delegate queue:queue];
		if (![session canAddOutput:output]) {
			[output setSampleBufferDelegate:nil queue:NULL];
			puddingCameraError(&result, "camera_failed", @"Unable to add camera output");
			return result;
		}
		[session addOutput:output];

		[session startRunning];
		if (!session.running) {
			puddingCameraError(&result, "camera_failed", @"Camera session did not start");
			return result;
		}

		dispatch_time_t deadline = dispatch_time(DISPATCH_TIME_NOW, (int64_t)timeoutSeconds * NSEC_PER_SEC);
		if (dispatch_semaphore_wait(delegate.semaphore, deadline) != 0) {
			[output setSampleBufferDelegate:nil queue:NULL];
			[session stopRunning];
			puddingCameraError(&result, "camera_timeout", @"Camera timed out");
			return result;
		}
		[output setSampleBufferDelegate:nil queue:NULL];
		[session stopRunning];

		if (delegate.error != nil) {
			puddingCameraError(&result, "camera_failed", delegate.error.localizedDescription ?: @"Frame capture failed");
			return result;
		}
		if (delegate.data == nil || delegate.data.length == 0) {
			puddingCameraError(&result, "camera_failed", @"Frame capture returned no data");
			return result;
		}

		result.length = (long)delegate.data.length;
		result.data = malloc((size_t)result.length);
		if (result.data == NULL) {
			puddingCameraError(&result, "camera_failed", @"Unable to allocate photo buffer");
			result.length = 0;
			return result;
		}
		memcpy(result.data, delegate.data.bytes, (size_t)result.length);
		return result;
	}
}

static void PuddingCameraFreeResult(PuddingCameraResult result) {
	if (result.data != NULL) {
		free(result.data);
	}
	if (result.code != NULL) {
		free(result.code);
	}
	if (result.message != NULL) {
		free(result.message);
	}
}
*/
import "C"

import (
	"context"
	"time"
)

type nativeCapturer struct{}

func New() Capturer {
	return nativeCapturer{}
}

func (nativeCapturer) CapturePhoto(ctx context.Context) (*Photo, error) {
	ctx, cancel := timeoutContext(ctx)
	defer cancel()

	type captureResult struct {
		data []byte
		err  error
	}
	done := make(chan captureResult, 1)
	go func() {
		result := C.PuddingCapturePhotoDarwin(C.int(int(DefaultTimeout.Seconds())))
		defer C.PuddingCameraFreeResult(result)
		if result.code != nil {
			done <- captureResult{err: NewError(C.GoString(result.code), C.GoString(result.message))}
			return
		}
		if result.data == nil || result.length <= 0 {
			done <- captureResult{err: emptyPhotoError()}
			return
		}
		data := C.GoBytes(result.data, C.int(result.length))
		done <- captureResult{data: data}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-done:
		if result.err != nil {
			return nil, wrapFailed(result.err)
		}
		if len(result.data) == 0 {
			return nil, emptyPhotoError()
		}
		return &Photo{Data: result.data, MIME: "image/jpeg", Name: Filename(time.Now())}, nil
	}
}
