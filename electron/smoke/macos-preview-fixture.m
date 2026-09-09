#import <AppKit/AppKit.h>

// A second, disposable App identity for real multi-App preview regression.
@interface PreviewFixtureDelegate : NSObject <NSApplicationDelegate>
@property(strong) NSWindow *window;
@end

@implementation PreviewFixtureDelegate
- (void)applicationDidFinishLaunching:(NSNotification *)notification {
  self.window = [[NSWindow alloc] initWithContentRect:NSMakeRect(400, 240, 320, 400)
    styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable
    backing:NSBackingStoreBuffered defer:NO];
  self.window.title = @"Computer Use Fixture";
  self.window.backgroundColor = [NSColor colorWithRed:0.15 green:0.35 blue:0.7 alpha:1];
  NSTextField *label = [NSTextField labelWithString:@"Second App Preview"];
  label.textColor = NSColor.whiteColor;
  label.font = [NSFont systemFontOfSize:22 weight:NSFontWeightMedium];
  label.frame = NSMakeRect(36, 185, 270, 40);
  [self.window.contentView addSubview:label];
  [self.window orderFront:nil];
}
@end

int main(void) {
  @autoreleasepool {
    NSApplication *app = NSApplication.sharedApplication;
    static PreviewFixtureDelegate *delegate;
    delegate = [PreviewFixtureDelegate new];
    app.delegate = delegate;
    [app setActivationPolicy:NSApplicationActivationPolicyRegular];
    [app run];
  }
  return 0;
}
