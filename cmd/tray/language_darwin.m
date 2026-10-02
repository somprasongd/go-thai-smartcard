//go:build darwin && cgo

#import <Foundation/Foundation.h>

int preferredLanguageIsThai(void) {
    @autoreleasepool {
        NSString *first = [[NSLocale preferredLanguages] firstObject];
        NSString *primary = [[first componentsSeparatedByCharactersInSet:
            [NSCharacterSet characterSetWithCharactersInString:@"-_"]] firstObject];
        return primary != nil && [primary caseInsensitiveCompare:@"th"] == NSOrderedSame;
    }
}
