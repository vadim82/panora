//go:build wails && darwin

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import <objc/runtime.h>
#include <stdio.h>

static id panoraReopenObserver;

static NSWindow *panoraMainWindow(NSApplication *application) {
    if (application.mainWindow) return application.mainWindow;
    // A hidden or minimized app can have no mainWindow. Its single titled,
    // resizable Panora window is still in the list of owned windows.
    for (NSWindow *window in application.windows) {
        NSWindowStyleMask mainStyle = NSWindowStyleMaskTitled | NSWindowStyleMaskResizable;
        if ((window.styleMask & mainStyle) == mainStyle &&
            !window.sheetParent && ![window isKindOfClass:[NSPanel class]]) {
            return window;
        }
    }
    return nil;
}

// Called synchronously from the native Open action on the AppKit main thread.
static void panoraShowWindow(void) {
    NSApplication *application = NSApp;
    NSWindow *window = panoraMainWindow(application);
    if (!window) return;

    NSWindowCollectionBehavior move = NSWindowCollectionBehaviorMoveToActiveSpace;
    if (!window.onActiveSpace && !(window.styleMask & NSWindowStyleMaskFullScreen) &&
        !(window.collectionBehavior & (NSWindowCollectionBehaviorCanJoinAllSpaces | move))) {
        // Give AppKit time to finish asynchronous activation. Clear only the
        // temporary flag so normal window switching keeps its usual behavior.
        window.collectionBehavior |= move;
        dispatch_after(dispatch_time(DISPATCH_TIME_NOW, NSEC_PER_SEC), dispatch_get_main_queue(), ^{
            window.collectionBehavior &= ~move;
        });
    }
    // Reopen also restores a window hidden with Command-H.
    if (application.hidden) [application unhideWithoutActivation];
    if (window.miniaturized) [window deminiaturize:nil];
    [window makeKeyAndOrderFront:nil];
    if (@available(macOS 14.0, *)) {
        [application activate];
    } else {
        [application activateIgnoringOtherApps:YES];
    }
}

@interface PanoraSettingsMenu : NSObject
+ (void)openSettings:(NSMenuItem *)sender;
+ (void)openAbout:(NSMenuItem *)sender;
@end

static void panoraShowWindowAndRunScript(NSString *script) {
    panoraShowWindow();
    // Wails 2.17's native ExecJS releases its autoreleased script when
    // delivering EventsEmit. Dispatch this fixed command from the owned webview.
    NSWindow *window = panoraMainWindow(NSApp);
    for (NSView *view in window.contentView.subviews) {
        if ([view isKindOfClass:[WKWebView class]]) {
            [(WKWebView *)view evaluateJavaScript:script completionHandler:nil];
            break;
        }
    }
}

@implementation PanoraSettingsMenu
+ (void)openSettings:(NSMenuItem *)sender {
    panoraShowWindowAndRunScript(@"window.dispatchEvent(new Event('panora:open-settings'))");
}
+ (void)openAbout:(NSMenuItem *)sender {
    panoraShowWindowAndRunScript(@"window.dispatchEvent(new Event('panora:open-about'))");
}
@end

void panoraNotifyAboutLaunch(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        panoraShowWindowAndRunScript(@"window.dispatchEvent(new Event('panora:launch-about'))");
    });
}

void panoraInstallSettingsMenu(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        // Wails builds the standard application menu as the first submenu.
        // Extend it in place so the standard commands and shortcuts stay intact.
        NSMenu *menu = NSApp.mainMenu.itemArray.firstObject.submenu;
        if (!menu ||
            [menu indexOfItemWithTarget:[PanoraSettingsMenu class] andAction:@selector(openSettings:)] >= 0) {
            return;
        }
        NSUInteger index = [menu.itemArray indexOfObjectPassingTest:
            ^BOOL(NSMenuItem *item, NSUInteger itemIndex, BOOL *stop) {
                return item.action == @selector(hide:);
            }];
        if (index == NSNotFound) return;
        NSMenuItem *settings = [[NSMenuItem alloc] initWithTitle:@"Settings…"
            action:@selector(openSettings:) keyEquivalent:@","];
        settings.keyEquivalentModifierMask = NSEventModifierFlagCommand;
        settings.target = [PanoraSettingsMenu class];
        [menu insertItem:settings atIndex:index];
        [menu insertItem:[NSMenuItem separatorItem] atIndex:index + 1];
        [settings release];
        NSMenuItem *about = [[NSMenuItem alloc] initWithTitle:@"About Panora"
            action:@selector(openAbout:) keyEquivalent:@""];
        about.target = [PanoraSettingsMenu class];
        [menu insertItem:about atIndex:0];
        [menu insertItem:[NSMenuItem separatorItem] atIndex:1];
        [about release];
    });
}

// Finder/Dock reopen targets the existing process. Wails' second-instance
// callback only runs when a new process reaches its single-instance lock.
static BOOL panoraHandleReopen(id delegate, SEL command, NSApplication *application, BOOL hasVisibleWindows) {
    panoraShowWindow();
    return YES;
}

static void panoraAddReopenHandler(id delegate) {
    SEL selector = @selector(applicationShouldHandleReopen:hasVisibleWindows:);
    if (!delegate || [delegate respondsToSelector:selector]) return;
    static char signature[16];
    snprintf(signature, sizeof(signature), "%s@:@%s", @encode(BOOL), @encode(BOOL));
    class_addMethod(object_getClass(delegate), selector, (IMP)panoraHandleReopen, signature);
}

static void panoraAddReopenObserver(void) {
    if (NSApp.delegate) {
        panoraAddReopenHandler(NSApp.delegate);
        return;
    }
    // OnStartup is a Go goroutine; wait for Wails' delegate if its native
    // application initialization has not reached the main queue yet.
    panoraReopenObserver = [[[NSNotificationCenter defaultCenter]
        addObserverForName:NSApplicationDidFinishLaunchingNotification object:nil
        queue:[NSOperationQueue mainQueue] usingBlock:^(NSNotification *notification) {
            panoraAddReopenHandler(NSApp.delegate);
            [[NSNotificationCenter defaultCenter] removeObserver:panoraReopenObserver];
            [panoraReopenObserver release];
            panoraReopenObserver = nil;
        }] retain];
}

void panoraInstallReopenHandler(void) {
    dispatch_async(dispatch_get_main_queue(), ^{ panoraAddReopenObserver(); });
}
