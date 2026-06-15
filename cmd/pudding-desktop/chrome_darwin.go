//go:build darwin

package main

// chrome_darwin.go 只保留 Wails 暂无等价入口的 macOS 窗口补充逻辑:
//
//  1. 双击 toolbar → zoom(填屏,非 fullscreen):FullSizeContentView 下
//     webview 吃掉全部鼠标事件,macOS 自带的双击 titlebar→zoom 收不到,
//     必须用 NSEvent local monitor 在 native 层拦截。
//  2. zoom 动画 swizzle 已临时停用(#5),仅作为踩坑记录保留。
//
// 已移除:
//
//  6. fullscreen 进出时手改 styleMask / titlebar / toolbar。
//  7. 退出 fullscreen 时手动隐藏红绿灯。
//
// 这两项改由 Wails 系统窗口事件驱动前端安全区布局:
// common:WindowFullscreen / mac:WindowWillExitFullScreen /
// common:WindowUnFullscreen。
//
// 历史背景:原版 [NSWindow zoom:] 动画 200-300ms,webview
//     reflow 总在动画后触发,内容滞后窗框明显;swizzle 成 80ms animator
//     版本,所有 zoom 入口(双击 / 菜单缩放 / Option+绿灯)统一收益。
//
// 红绿灯定位本身**只用 MacTitleBarHiddenInset preset**,绝不 cgo setFrame
// standardWindowButton(旧项目踩坑:reposition 多事件源累加漂移,159 行
// cgo 才勉强压住,换 preset 一行解决)。

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit

#import <AppKit/AppKit.h>
#import <objc/runtime.h>
#include <stdlib.h>

// kPuddingToolbarHeight 与 main.go 的 InvisibleTitleBarHeight、web 侧
// --toolbar-h(html[data-shell="mac"])三处同值:同一条"工具条带"语义,
// 不同步会让可拖区 / 双击区 / 视觉工具条割裂。
static const CGFloat kPuddingToolbarHeight = 54;

// 左上角保护带:红绿灯(inset 72px)+ rail 折叠/展开按钮(x≈80..116)。
// 这一带内的双击放行给按钮自身,不触发 zoom。旧项目用前端上报 no-zoom
// rects 做精确判定;新壳顶部交互件只有这一处,先用固定带,将来工具条
// 加 tabs 之类再升级上报机制。
static const CGFloat kPuddingNoZoomLeftWidth = 130;

// === zoom: 的 80ms 动画替换(method swizzle)===
//
// 复制 [zoom:] 的 toggle 语义:已 zoom → 还原缓存 frame(缓存失效退到
// 默认尺寸居中);未 zoom → 缓存当前,切到 standard frame。动画走
// animator proxy,时长压到 80ms,webview reflow 滞后肉眼无感。
// 兜底:PUDDING_NO_ZOOM_SWIZZLE=1 跳过;异常 fallback 原实现。
//
// 临时停用(#5):这是自定义 NSWindow swizzle,不是 Wails 正统窗口能力。
// 先保留原实现作为踩坑记录,但不编译、不安装,避免影响后续 chrome 归正。
#if 0
static NSMutableDictionary *gPuddingZoomFrames = nil;
static IMP gPuddingOriginalZoomIMP = NULL;

static void puddingZoomImpl(NSWindow *w) {
	if (!w) return;
	dispatch_async(dispatch_get_main_queue(), ^{
		if (gPuddingZoomFrames == nil) {
			gPuddingZoomFrames = [[NSMutableDictionary alloc] init];
		}
		NSValue *key = [NSValue valueWithNonretainedObject:w];

		NSRect targetFrame;
		if ([w isZoomed]) {
			NSValue *savedValue = [gPuddingZoomFrames objectForKey:key];
			BOOL haveValid = NO;
			if (savedValue) {
				NSRect saved = [savedValue rectValue];
				for (NSScreen *s in [NSScreen screens]) {
					if (NSIntersectsRect([s visibleFrame], saved)) {
						targetFrame = saved;
						haveValid = YES;
						break;
					}
				}
				[gPuddingZoomFrames removeObjectForKey:key];
			}
			if (!haveValid) {
				NSScreen *screen = [w screen] ?: [NSScreen mainScreen];
				NSRect vf = [screen visibleFrame];
				// 默认还原尺寸与 main.go 的初始窗口一致
				targetFrame = NSMakeRect(vf.origin.x + (vf.size.width - 1200) / 2,
				                         vf.origin.y + (vf.size.height - 800) / 2,
				                         1200, 800);
			}
		} else {
			NSScreen *screen = [w screen] ?: [NSScreen mainScreen];
			targetFrame = [screen visibleFrame];
			id<NSWindowDelegate> delegate = (id<NSWindowDelegate>)[w delegate];
			if ([delegate respondsToSelector:@selector(windowWillUseStandardFrame:defaultFrame:)]) {
				targetFrame = [delegate windowWillUseStandardFrame:w defaultFrame:targetFrame];
			}
			[gPuddingZoomFrames setObject:[NSValue valueWithRect:[w frame]] forKey:key];
		}

		[NSAnimationContext beginGrouping];
		[[NSAnimationContext currentContext] setDuration:0.08];
		[[w animator] setFrame:targetFrame display:YES];
		[NSAnimationContext endGrouping];
	});
}

static void puddingZoomReplacement(id self, SEL _cmd, id sender) {
	@try {
		puddingZoomImpl((NSWindow *)self);
	} @catch (NSException *e) {
		NSLog(@"pudding: zoom swizzle threw, falling back to system zoom: %@", e);
		if (gPuddingOriginalZoomIMP != NULL) {
			((void (*)(id, SEL, id))gPuddingOriginalZoomIMP)(self, _cmd, sender);
		}
	}
}

static void puddingInstallZoomSwizzle(void) {
	if (getenv("PUDDING_NO_ZOOM_SWIZZLE") != NULL) {
		NSLog(@"pudding: PUDDING_NO_ZOOM_SWIZZLE set, skipping zoom swizzle");
		return;
	}
	if (gPuddingOriginalZoomIMP != NULL) return;
	Class cls = [NSWindow class];
	SEL selector = @selector(zoom:);
	Method method = class_getInstanceMethod(cls, selector);
	if (method == NULL) {
		NSLog(@"pudding: NSWindow zoom: not found, skipping swizzle");
		return;
	}
	gPuddingOriginalZoomIMP = method_setImplementation(method, (IMP)puddingZoomReplacement);
}
#endif

// === 双击 toolbar → zoom 的 NSEvent local monitor ===
static id gPuddingDoubleClickMonitor = nil;
static NSMutableSet *gPuddingAttachedWindows = nil;

static void puddingAttachDoubleClickToZoom(void *nsWindowPtr) {
	NSWindow *window = (NSWindow *)nsWindowPtr;
	if (!window) return;
	if (gPuddingAttachedWindows == nil) {
		gPuddingAttachedWindows = [[NSMutableSet alloc] init];
	}
	if ([gPuddingAttachedWindows containsObject:window]) return;
	[gPuddingAttachedWindows addObject:window];

	if (gPuddingDoubleClickMonitor != nil) return; // 全局只 install 一次

	NSArray *btnKinds = @[@(NSWindowCloseButton),
	                      @(NSWindowMiniaturizeButton),
	                      @(NSWindowZoomButton)];
	gPuddingDoubleClickMonitor = [NSEvent
		addLocalMonitorForEventsMatchingMask:NSEventMaskLeftMouseDown
		                             handler:^NSEvent *(NSEvent *event) {
		if ([event clickCount] != 2) return event;
		NSWindow *w = [event window];
		if (!w || ![gPuddingAttachedWindows containsObject:w]) return event;
		NSPoint loc = [event locationInWindow];
		NSRect frame = [w frame];
		// 顶部 toolbar 带之外放行
		if (loc.y < frame.size.height - kPuddingToolbarHeight) return event;
		// 左上保护带(红绿灯 + rail 折叠按钮)放行
		if (loc.x < kPuddingNoZoomLeftWidth) return event;
		// 红绿灯 hit 区放行(双保险)
		for (NSNumber *kind in btnKinds) {
			NSButton *btn = [w standardWindowButton:kind.integerValue];
			if (btn && NSPointInRect(loc, [btn frame])) return event;
		}
		[w zoom:nil];
		return nil; // 吞事件,避免 webview 同时响应
	}];
	[gPuddingDoubleClickMonitor retain];
}
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

func installZoomSwizzle() {
	// Custom workaround #5 is intentionally disabled. Do not call the ObjC
	// swizzle while desktop window behavior is being moved back to Wails-native
	// APIs/events.
	// C.puddingInstallZoomSwizzle()
}

func attachDoubleClickToZoom(w *application.WebviewWindow) {
	if w == nil {
		return
	}
	nsWindow := w.NativeWindow()
	if nsWindow == nil {
		return
	}
	C.puddingAttachDoubleClickToZoom(nsWindow)
}
