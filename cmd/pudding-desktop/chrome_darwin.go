//go:build darwin

package main

// chrome_darwin.go 只保留 Wails 暂无等价入口的 macOS 窗口补充逻辑:
//
//  1. 双击 toolbar → zoom(填屏,非 fullscreen):FullSizeContentView 下
//     webview 吃掉全部鼠标事件,macOS 自带的双击 titlebar→zoom 收不到,
//     必须用 NSEvent local monitor 在 native 层拦截。
//  2. 退出 fullscreen 动画期间临时隐藏标准红绿灯,did-exit 后恢复。
//  3. zoom 动画 swizzle 已临时停用(#5),仅作为踩坑记录保留。
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
#include <stdbool.h>
#include <stdlib.h>
#include <string.h>

// kPuddingToolbarHeight 与 web 侧 --toolbar-h(html[data-shell="mac"])同值:
// 同一条"工具条带"语义,不同步会让双击区与视觉工具条割裂。
static const CGFloat kPuddingToolbarHeight = 54;

static void puddingApplyTrafficLightsHidden(NSWindow *window, bool hidden) {
	NSArray *btnKinds = @[@(NSWindowCloseButton),
	                      @(NSWindowMiniaturizeButton),
	                      @(NSWindowZoomButton)];
	for (NSNumber *kind in btnKinds) {
		NSButton *btn = [window standardWindowButton:kind.integerValue];
		if (btn) {
			[btn setHidden:hidden];
		}
	}
}

static void puddingSetTrafficLightsHidden(void *nsWindowPtr, bool hidden) {
	NSWindow *window = (NSWindow *)nsWindowPtr;
	if (!window) return;
	dispatch_async(dispatch_get_main_queue(), ^{
		puddingApplyTrafficLightsHidden(window, hidden);
	});
}static void puddingSetUseToolbar(void *nsWindowPtr, bool useToolbar) {
	NSWindow *window = (NSWindow *)nsWindowPtr;
	if (!window) return;
	dispatch_async(dispatch_get_main_queue(), ^{
		if (useToolbar) {
			NSToolbar *toolbar = [[NSToolbar alloc] initWithIdentifier:@"wails.toolbar"];
			[toolbar autorelease];
			[window setToolbar:toolbar];
		} else {
			[window setToolbar:nil];
		}
	});
}


static void puddingSetHideTitle(void *nsWindowPtr, bool hideTitle) {
	NSWindow *window = (NSWindow *)nsWindowPtr;
	if (!window) return;
	dispatch_async(dispatch_get_main_queue(), ^{
		if (hideTitle) {
			[window setTitleVisibility:NSWindowTitleHidden];
		} else {
			[window setTitleVisibility:NSWindowTitleVisible];
		}
	});
}

static void puddingSetWindowAppearance(void *nsWindowPtr, const char* theme) {
	NSWindow *window = (NSWindow *)nsWindowPtr;
	if (!window) return;
	// 必须复制字符串！Go 的 defer C.free 会在 dispatch_async 执行前释放原指针
	char *themeCopy = theme ? strdup(theme) : NULL;
	dispatch_async(dispatch_get_main_queue(), ^{
		if (themeCopy == NULL || strcmp(themeCopy, "system") == 0) {
			window.appearance = nil;
		} else if (strcmp(themeCopy, "dark") == 0) {
			window.appearance = [NSAppearance appearanceNamed:NSAppearanceNameDarkAqua];
		} else {
			window.appearance = [NSAppearance appearanceNamed:NSAppearanceNameAqua];
		}
		free(themeCopy);
	});
}

static void puddingSetToolbarStyle(void *nsWindowPtr, int style) {
	NSWindow *window = (NSWindow *)nsWindowPtr;
	if (!window) return;
#if MAC_OS_X_VERSION_MAX_ALLOWED >= 110000
	if (@available(macOS 11.0, *)) {
		dispatch_async(dispatch_get_main_queue(), ^{
			[window setToolbarStyle:style];
		});
	}
#endif
}

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
static NSRect *gPuddingNoZoomRects = NULL;
static int gPuddingNoZoomCount = 0;

static void puddingReplaceNoZoomRects(double *flat, int count) {
	if (gPuddingNoZoomRects != NULL) {
		free(gPuddingNoZoomRects);
		gPuddingNoZoomRects = NULL;
	}
	gPuddingNoZoomCount = 0;
	if (flat == NULL || count <= 0) return;

	gPuddingNoZoomRects = (NSRect *)calloc((size_t)count, sizeof(NSRect));
	if (gPuddingNoZoomRects == NULL) return;

	for (int i = 0; i < count; i++) {
		int offset = i * 4;
		gPuddingNoZoomRects[i] = NSMakeRect(flat[offset],
		                                    flat[offset + 1],
		                                    flat[offset + 2],
		                                    flat[offset + 3]);
	}
	gPuddingNoZoomCount = count;
}

static void puddingSetNoZoomRects(double *flat, int count) {
	int safeCount = count > 0 ? count : 0;
	int flatLength = safeCount * 4;
	double *copy = NULL;
	if (flat != NULL && flatLength > 0) {
		copy = (double *)malloc(sizeof(double) * (size_t)flatLength);
		if (copy == NULL) return;
		memcpy(copy, flat, sizeof(double) * (size_t)flatLength);
	}

	dispatch_async(dispatch_get_main_queue(), ^{
		puddingReplaceNoZoomRects(copy, safeCount);
		if (copy != NULL) {
			free(copy);
		}
	});
}

static bool puddingPointInNoZoomRect(NSPoint loc, NSWindow *w) {
	if (gPuddingNoZoomRects == NULL || gPuddingNoZoomCount <= 0 || w == nil) return false;
	NSView *contentView = [w contentView];
	if (contentView == nil) return false;

	NSPoint contentPoint = [contentView convertPoint:loc fromView:nil];
	CGFloat contentHeight = [contentView bounds].size.height;
	for (int i = 0; i < gPuddingNoZoomCount; i++) {
		NSRect rect = gPuddingNoZoomRects[i];
		NSRect appkitRect = NSMakeRect(rect.origin.x,
		                               contentHeight - rect.origin.y - rect.size.height,
		                               rect.size.width,
		                               rect.size.height);
		if (NSPointInRect(contentPoint, appkitRect)) return true;
	}
	return false;
}

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
		// 红绿灯 hit 区放行(双保险)
		for (NSNumber *kind in btnKinds) {
			NSButton *btn = [w standardWindowButton:kind.integerValue];
			if (btn && NSPointInRect(loc, [btn frame])) return event;
		}
		if (puddingPointInNoZoomRect(loc, w)) return event;
		[w zoom:nil];
		return nil; // 吞事件,避免 webview 同时响应
	}];
	[gPuddingDoubleClickMonitor retain];
}
*/
import "C"

import (
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

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

func setNoZoomRects(rects []noZoomRect) {
	if len(rects) == 0 {
		C.puddingSetNoZoomRects((*C.double)(nil), 0)
		return
	}
	flat := make([]C.double, len(rects)*4)
	for i, rect := range rects {
		offset := i * 4
		flat[offset] = C.double(rect.X)
		flat[offset+1] = C.double(rect.Y)
		flat[offset+2] = C.double(rect.W)
		flat[offset+3] = C.double(rect.H)
	}
	C.puddingSetNoZoomRects((*C.double)(unsafe.Pointer(&flat[0])), C.int(len(rects)))
}

func setTrafficLightsHidden(w *application.WebviewWindow, hidden bool) {
	if w == nil {
		return
	}
	nsWindow := w.NativeWindow()
	if nsWindow == nil {
		return
	}
	C.puddingSetTrafficLightsHidden(nsWindow, C.bool(hidden))
}



func setHideTitle(w *application.WebviewWindow, hide bool) {
	if w == nil {
		return
	}
	nsWindow := w.NativeWindow()
	if nsWindow == nil {
		return
	}
	C.puddingSetHideTitle(nsWindow, C.bool(hide))
}

func setWindowAppearance(w *application.WebviewWindow, theme string) {
	if w == nil {
		return
	}
	nsWindow := w.NativeWindow()
	if nsWindow == nil {
		return
	}
	cTheme := C.CString(theme)
	defer C.free(unsafe.Pointer(cTheme))
	C.puddingSetWindowAppearance(nsWindow, cTheme)
}

func setUseToolbar(w *application.WebviewWindow, use bool) {
	if w == nil {
		return
	}
	nsWindow := w.NativeWindow()
	if nsWindow == nil {
		return
	}
	C.puddingSetUseToolbar(nsWindow, C.bool(use))
}

func setToolbarStyle(w *application.WebviewWindow, style application.MacToolbarStyle) {
	if w == nil {
		return
	}
	nsWindow := w.NativeWindow()
	if nsWindow == nil {
		return
	}
	C.puddingSetToolbarStyle(nsWindow, C.int(style))
}
