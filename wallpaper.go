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

var wallpaperBitmaps = map[string]*ui.Bitmap{}

func init() {
	for name, data := range map[string][]byte{
		"ember":    wallpaperEmberJPEG,
		"jade":     wallpaperJadeJPEG,
		"graphite": wallpaperGraphiteJPEG,
	} {
		if bitmap, err := ui.DecodeBitmap(data); err == nil {
			wallpaperBitmaps[name] = bitmap
		}
	}
}

func wallpaperBitmap(name string) *ui.Bitmap {
	return wallpaperBitmaps[normalizeWallpaper(name)]
}
