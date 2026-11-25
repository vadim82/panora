#import <Cocoa/Cocoa.h>
#include "menu.h"

extern void panoraMenuAction(int action);
static NSStatusItem *statusItem;
static NSObject *target;

int panoraIsMenuBundle(void) {
    return [[NSBundle mainBundle].bundleIdentifier isEqualToString:@"org.panora.menu"];
}

static void openPanora(BOOL about) {
    NSURL *bundle = [NSBundle mainBundle].bundleURL;
    NSURL *client = bundle.URLByDeletingLastPathComponent.URLByDeletingLastPathComponent.URLByDeletingLastPathComponent;
    if (![[NSBundle bundleWithURL:client].bundleIdentifier isEqualToString:@"org.panora.app"]) return;
    NSWorkspaceOpenConfiguration *configuration = [NSWorkspaceOpenConfiguration configuration];
    configuration.activates = YES;
    if (about) {
        // Wails forwards About from the new process to an existing window.
        configuration.createsNewApplicationInstance = YES;
        configuration.arguments = @[@"--about"];
    }
    [[NSWorkspace sharedWorkspace] openApplicationAtURL:client configuration:configuration completionHandler:nil];
}

@interface PanoraMenuTarget : NSObject
- (void)activate:(NSMenuItem *)sender;
@end

@implementation PanoraMenuTarget
- (void)activate:(NSMenuItem *)sender {
    if (sender.tag == PanoraOpen || sender.tag == PanoraAbout) {
        openPanora(sender.tag == PanoraAbout);
        return;
    }
    if (sender.tag == PanoraStop) {
        NSAlert *alert = [[NSAlert alloc] init];
        alert.messageText = @"Stop the Panora service and close the menu?";
        alert.informativeText = @"The last wallpaper stays in place. Your start-at-login preference is kept.";
        [alert addButtonWithTitle:@"Stop Service & Quit"];
        [alert addButtonWithTitle:@"Cancel"];
        NSModalResponse result = [alert runModal];
        [alert release];
        if (result != NSAlertFirstButtonReturn) return;
        sender.enabled = NO;
    }
    panoraMenuAction((int)sender.tag);
}
@end

static void addItem(NSMenu *menu, NSString *title, NSInteger tag) {
    NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:title action:@selector(activate:) keyEquivalent:@""];
    item.tag = tag;
    item.target = target;
    [menu addItem:item];
    [item release];
}

// Monochrome adaptation of the approved Canyon Passage app SVG: the sun and
// foreground cliff paths retain its 1024-unit geometry. AppKit supplies tint.
static NSImage *panoraTrayImage(void) {
    NSImage *image = [NSImage imageWithSize:NSMakeSize(18, 18) flipped:YES drawingHandler:^BOOL(NSRect rect) {
        NSAffineTransform *transform = [NSAffineTransform transform];
        [transform scaleXBy:rect.size.width / 1024.0 yBy:rect.size.height / 1024.0];
        [transform concat];
        [[NSColor blackColor] setFill];
        [[NSBezierPath bezierPathWithOvalInRect:NSMakeRect(536, 221, 232, 232)] fill];
        NSPoint left[] = {
            {96, 465}, {211, 465}, {211, 529}, {282, 529}, {282, 605},
            {349, 605}, {349, 687}, {431, 758}, {403, 928}, {96, 928},
        };
        NSPoint right[] = {
            {928, 507}, {829, 507}, {829, 580}, {764, 580}, {764, 655},
            {705, 655}, {627, 765}, {655, 928}, {928, 928},
        };
        NSBezierPath *cliffs = [NSBezierPath bezierPath];
        [cliffs moveToPoint:left[0]];
        [cliffs appendBezierPathWithPoints:left + 1 count:sizeof(left) / sizeof(left[0]) - 1];
        [cliffs closePath];
        [cliffs moveToPoint:right[0]];
        [cliffs appendBezierPathWithPoints:right + 1 count:sizeof(right) / sizeof(right[0]) - 1];
        [cliffs closePath];
        [cliffs fill];
        return YES;
    }];
    image.template = YES;
    return image;
}

static void installMenu(void) {
    target = [[PanoraMenuTarget alloc] init];
    statusItem = [[[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength] retain];
    statusItem.button.image = panoraTrayImage();
    statusItem.button.accessibilityLabel = @"Panora";
    statusItem.button.toolTip = @"Panora wallpaper controls";
    NSMenu *menu = [[NSMenu alloc] initWithTitle:@"Panora"];
    menu.autoenablesItems = NO;
    addItem(menu, @"Panora", PanoraStateMessage);
    [menu itemWithTag:PanoraStateMessage].enabled = NO;
    addItem(menu, @"Open Panora", PanoraOpen);
    [menu addItem:[NSMenuItem separatorItem]];
    addItem(menu, @"Previous", PanoraPrevious);
    addItem(menu, @"Pause rotation", PanoraPause);
    addItem(menu, @"Next", PanoraNext);
    [menu addItem:[NSMenuItem separatorItem]];
    addItem(menu, @"About Panora", PanoraAbout);
    addItem(menu, @"Stop Service & Quit…", PanoraStop);
    statusItem.menu = menu;
    [menu release];
}

void panoraUpdateMenu(char *text, int previous, int pause, int paused, int next, int stop) {
    NSString *message = [[NSString alloc] initWithUTF8String:text];
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!statusItem) installMenu();
        NSMenu *menu = statusItem.menu;
        [menu itemWithTag:PanoraStateMessage].title = message;
        [menu itemWithTag:PanoraPrevious].enabled = previous;
        NSMenuItem *rotation = [menu itemWithTag:PanoraPause] ?: [menu itemWithTag:PanoraResume];
        rotation.enabled = pause;
        rotation.title = paused ? @"Resume rotation" : @"Pause rotation";
        rotation.tag = paused ? PanoraResume : PanoraPause;
        [menu itemWithTag:PanoraNext].enabled = next;
        [menu itemWithTag:PanoraStop].enabled = stop;
        [message release];
    });
}

void panoraRunMenu(void) {
    @autoreleasepool {
        NSApplication *app = [NSApplication sharedApplication];
        [app setActivationPolicy:NSApplicationActivationPolicyAccessory];
        [app run];
        if (statusItem) [[NSStatusBar systemStatusBar] removeStatusItem:statusItem];
        [statusItem release];
        statusItem = nil;
        [target release];
        target = nil;
    }
}

void panoraQuitMenu(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [NSApp stop:nil];
        // Wake the AppKit event loop if it is waiting for an event.
        NSEvent *wake = [NSEvent otherEventWithType:NSEventTypeApplicationDefined
            location:NSZeroPoint modifierFlags:0 timestamp:0 windowNumber:0
            context:nil subtype:0 data1:0 data2:0];
        [NSApp postEvent:wake atStart:YES];
    });
}
