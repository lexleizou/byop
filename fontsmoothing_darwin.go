//go:build darwin

package main

import (
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// configureFontSmoothing matches the prototype's -webkit-font-smoothing by
// registering an in-memory CoreText default before MyGo initializes its text engine.
func configureFontSmoothing() {
	if _, err := purego.Dlopen(
		"/System/Library/Frameworks/Foundation.framework/Foundation",
		purego.RTLD_GLOBAL|purego.RTLD_NOW,
	); err != nil {
		return
	}
	key := objc.ID(objc.GetClass("NSString")).Send(
		objc.RegisterName("stringWithUTF8String:"), "AppleFontSmoothing\x00",
	)
	value := objc.ID(objc.GetClass("NSNumber")).Send(
		objc.RegisterName("numberWithInt:"), int32(0),
	)
	defaults := objc.Send[objc.ID](
		objc.ID(objc.GetClass("NSUserDefaults")),
		objc.RegisterName("standardUserDefaults"),
	)
	registered := objc.Send[objc.ID](
		objc.ID(objc.GetClass("NSDictionary")),
		objc.RegisterName("dictionaryWithObject:forKey:"), value, key,
	)
	defaults.Send(objc.RegisterName("registerDefaults:"), registered)
}
