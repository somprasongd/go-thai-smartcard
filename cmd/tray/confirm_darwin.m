//go:build darwin && cgo

#import <Cocoa/Cocoa.h>

static void trayRunOnMain(void (^action)(void)) {
    if ([NSThread isMainThread]) {
        action();
    } else {
        dispatch_sync(dispatch_get_main_queue(), action);
    }
}

int trayConfirmNative(const char *title, const char *message, const char *stop, const char *cancel) {
    __block int confirmed = 0;
    trayRunOnMain(^{
        @autoreleasepool {
            NSAlert *alert = [[NSAlert alloc] init];
            [alert setAlertStyle:NSAlertStyleWarning];
            [alert setMessageText:[NSString stringWithUTF8String:title]];
            [alert setInformativeText:[NSString stringWithUTF8String:message]];
            // Return defaults to Cancel; Escape also cancels.
            [alert addButtonWithTitle:[NSString stringWithUTF8String:cancel]];
            [alert addButtonWithTitle:[NSString stringWithUTF8String:stop]];
            NSButton *cancelButton = [[alert buttons] firstObject];
            [[alert window] setDefaultButtonCell:[cancelButton cell]];
            [cancelButton setKeyEquivalent:@"\r"];
            // Cocoa's title-based Escape shortcut does not recognize Thai.
            id escapeMonitor = [NSEvent addLocalMonitorForEventsMatchingMask:NSEventMaskKeyDown
                handler:^NSEvent *(NSEvent *event) {
                    if ([event keyCode] == 53 && [NSApp modalWindow] == [alert window]) {
                        [NSApp stopModalWithCode:NSAlertFirstButtonReturn];
                        return nil;
                    }
                    return event;
                }];
            confirmed = [alert runModal] == NSAlertSecondButtonReturn;
            [NSEvent removeMonitor:escapeMonitor];
            [alert release];
        }
    });
    return confirmed;
}

void trayAlertNative(const char *title, const char *message, const char *ok) {
    trayRunOnMain(^{
        @autoreleasepool {
            NSAlert *alert = [[NSAlert alloc] init];
            [alert setAlertStyle:NSAlertStyleCritical];
            [alert setMessageText:[NSString stringWithUTF8String:title]];
            [alert setInformativeText:[NSString stringWithUTF8String:message]];
            [alert addButtonWithTitle:[NSString stringWithUTF8String:ok]];
            [alert runModal];
            [alert release];
        }
    });
}
