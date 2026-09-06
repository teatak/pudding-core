// Native events for isolated source-Electron smoke tests. Unlike renderer
// sendInputEvent/insertText, these pass through the macOS input method and chrome.
#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#import <Carbon/Carbon.h>
#include <unistd.h>

static void emit(NSDictionary *value) {
  NSData *data = [NSJSONSerialization dataWithJSONObject:value options:0 error:NULL];
  puts([[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding].UTF8String);
}

static void mouse(CGEventType type, CGPoint point, int clicks) {
  CGEventRef event = CGEventCreateMouseEvent(NULL, type, point, kCGMouseButtonLeft);
  CGEventSetIntegerValueField(event, kCGMouseEventClickState, clicks);
  // Window movement is handled by WindowServer, before events reach the app.
  CGEventPost(kCGHIDEventTap, event);
  CFRelease(event);
  usleep(60000);
}

int main(int argc, const char *argv[]) {
  @autoreleasepool {
    if (argc < 2) return 2;
    NSString *action = @(argv[1]);
    if ([action isEqualToString:@"status"]) {
      TISInputSourceRef source = TISCopyCurrentKeyboardInputSource();
      emit(@{@"inputSource": (__bridge NSString *)TISGetInputSourceProperty(source, kTISPropertyInputSourceID),
             @"postEventsAllowed": @(CGPreflightPostEventAccess())});
      CFRelease(source);
      return 0;
    }
    if (argc < 3) return 2;
    pid_t pid = atoi(argv[2]);
    NSRunningApplication *target = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
    if ([action isEqualToString:@"activate"] && pid == getppid() &&
        [target.executableURL.path hasSuffix:@"/web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron"]) {
      return [target activateWithOptions:NSApplicationActivateIgnoringOtherApps] ? 0 : 3;
    }
    if (argc < 4) return 2;
    // Never inject into an existing user window: the smoke's own Electron must
    // launch this helper and own the foreground before every gesture.
    if (pid != getppid() || ![target.executableURL.path hasSuffix:@"/web/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron"] ||
        NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier != pid || !CGPreflightPostEventAccess()) {
      fputs("Native input requires the foreground parent source Electron and event-post permission.\n", stderr);
      return 3;
    }
    if ([action isEqualToString:@"key"]) {
      CGKeyCode code = (CGKeyCode)atoi(argv[3]);
      CGEventFlags flags = argc > 4 ? strtoull(argv[4], NULL, 10) : 0;
      for (int down = 1; down >= 0; down--) {
        CGEventRef event = CGEventCreateKeyboardEvent(NULL, code, down);
        CGEventSetFlags(event, flags);
        CGEventPostToPid(pid, event);
        CFRelease(event);
        usleep(60000);
      }
    } else if ([action isEqualToString:@"drag"] && argc == 7) {
      CGPoint start = CGPointMake(atof(argv[3]), atof(argv[4]));
      CGPoint end = CGPointMake(atof(argv[5]), atof(argv[6]));
      mouse(kCGEventMouseMoved, start, 0);
      mouse(kCGEventLeftMouseDown, start, 1);
      for (int step = 1; step <= 8; step++) {
        mouse(kCGEventLeftMouseDragged, CGPointMake(start.x + (end.x - start.x) * step / 8, start.y + (end.y - start.y) * step / 8), 1);
      }
      mouse(kCGEventLeftMouseUp, end, 1);
    } else if ([action isEqualToString:@"double-click"] && argc == 5) {
      CGPoint point = CGPointMake(atof(argv[3]), atof(argv[4]));
      mouse(kCGEventMouseMoved, point, 0);
      for (int click = 1; click <= 2; click++) {
        mouse(kCGEventLeftMouseDown, point, click);
        mouse(kCGEventLeftMouseUp, point, click);
      }
    } else return 2;
  }
  return 0;
}
