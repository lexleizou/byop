package main

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
)

//go:embed assets/wallpapers/ember.jpg
var wallpaperEmberJPEG []byte

//go:embed assets/wallpapers/jade.jpg
var wallpaperJadeJPEG []byte

//go:embed assets/wallpapers/graphite.jpg
var wallpaperGraphiteJPEG []byte

//go:embed assets/wallpapers/jagent-misty-mountains.webp
var wallpaperJAgentMistyMountainsWebP []byte

var wallpaperBitmaps = map[string]*ui.Bitmap{}

func init() {
	for name, data := range map[string][]byte{
		"ember":                  wallpaperEmberJPEG,
		"jade":                   wallpaperJadeJPEG,
		"graphite":               wallpaperGraphiteJPEG,
		"jagent-misty-mountains": wallpaperJAgentMistyMountainsWebP,
	} {
		if bitmap, err := ui.DecodeBitmap(data); err == nil {
			wallpaperBitmaps[name] = bitmap
		}
	}
}

func wallpaperBitmap(name string) *ui.Bitmap {
	switch name {
	case "jagent-misty-mountains":
		return wallpaperBitmaps[name]
	default:
		return wallpaperBitmaps[normalizeWallpaper(name)]
	}
}

func (s *appState) activeWallpaper() string {
	if s.jAgentPresetActive() {
		return normalizeJAgentWallpaper(s.preferences.JAgentWallpaper)
	}
	return normalizeWallpaper(s.preferences.Wallpaper)
}

// wallpaperBackdrop paints the shell's atmospheric layer. The shell and the
// appearance preview share this function so their transparency response and
// wallpaper identity cannot drift apart.
func (s *appState) wallpaperBackdrop(
	c *ui.Context,
	dark, high, reduceTransparency bool,
) bool {
	if !deepLiquidGlassActive(dark, high, reduceTransparency) ||
		s.activeWallpaper() == "none" {
		return false
	}
	bitmap := wallpaperBitmap(s.activeWallpaper())
	if bitmap == nil {
		return false
	}
	ui.Image(c, bitmap).Absolute().Top(0).Right(0).Bottom(0).Left(0).
		Fit(ui.Cover).PassThrough()
	return true
}
