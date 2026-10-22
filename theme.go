package main

import (
	"sync/atomic"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	fontMicro     float32 = 10
	fontCaption   float32 = 11
	fontSmall     float32 = 12
	fontBody      float32 = 14
	fontBodyLarge float32 = 15
	fontUILarge   float32 = 16
	fontTitle     float32 = 18
	fontPageTitle float32 = 30
	fontDisplay   float32 = 32
)

// palette is the small subset of Onevra semantics the native shell needs.
// It keeps the design source visible in Go instead of scattering literals
// through the views.
type palette struct {
	background     ui.Color
	surface        ui.Color
	surfaceHover   ui.Color
	surfacePressed ui.Color
	border         ui.Color
	text           ui.Color
	muted          ui.Color
	accent         ui.Color
	accentHover    ui.Color
	accentPressed  ui.Color
	accentText     ui.Color
	danger         ui.Color
	warning        ui.Color
	success        ui.Color
	selection      ui.Color
	focus          ui.Color
	scrollbar      ui.Color
	shadow         ui.Color
	shadowCard     ui.Color
	shadowOverlay  ui.Color
}

func themePalette(dark bool) palette {
	return themePaletteWithContrast(dark, false)
}

func themePaletteWithContrast(dark bool, high bool) palette {
	if dark {
		p := palette{
			background:     ui.Hex("#211f1c"),
			surface:        ui.Hex("#2b2925"),
			surfaceHover:   ui.Hex("#36332e"),
			surfacePressed: ui.Hex("#403c36"),
			border:         ui.Hex("#504b43"),
			text:           ui.Hex("#eeeae2"),
			muted:          ui.Hex("#bdb6aa"),
			accent:         ui.Hex("#ed986e"),
			accentHover:    ui.Hex("#efa47f"),
			accentPressed:  ui.Hex("#d18661"),
			accentText:     ui.Hex("#291b14"),
			danger:         ui.Hex("#e58f95"),
			warning:        ui.Hex("#e6b567"),
			success:        ui.Hex("#79bea1"),
			selection:      ui.RGBA(237, 152, 110, 61),
			focus:          ui.RGBA(237, 152, 110, 204),
			scrollbar:      ui.RGBA(238, 234, 226, 89),
			shadow:         ui.RGBA(0, 0, 0, 56),
			shadowCard:     ui.RGBA(0, 0, 0, 72),
			shadowOverlay:  ui.RGBA(0, 0, 0, 112),
		}
		if high {
			p.background = ui.Hex("#141311")
			p.surface = ui.Hex("#201e1a")
			p.surfaceHover = ui.Hex("#37332d")
			p.surfacePressed = ui.Hex("#48433b")
			p.border = ui.Hex("#9a9387")
			p.text = ui.Hex("#fffdf8")
			p.muted = ui.Hex("#d9d3c8")
			p.focus = p.accent
			p.selection = ui.RGBA(237, 152, 110, 102)
		}
		return p
	}
	p := palette{
		background:     ui.Hex("#fcfbf9"),
		surface:        ui.Hex("#fefefd"),
		surfaceHover:   ui.Hex("#eceae4"),
		surfacePressed: ui.Hex("#d9d7d2"),
		border:         ui.Hex("#deded5"),
		text:           ui.Hex("#33332f"),
		muted:          ui.Hex("#72726a"),
		accent:         ui.Hex("#bd481b"),
		accentHover:    ui.Hex("#a63f18"),
		accentPressed:  ui.Hex("#8e3614"),
		accentText:     ui.Hex("#ffffff"),
		danger:         ui.Hex("#b4444c"),
		warning:        ui.Hex("#b45309"),
		success:        ui.Hex("#059669"),
		selection:      ui.RGBA(230, 101, 43, 56),
		focus:          ui.RGBA(185, 71, 22, 140),
		scrollbar:      ui.RGBA(51, 51, 47, 77),
		shadow:         ui.RGBA(51, 43, 34, 20),
		shadowCard:     ui.RGBA(51, 43, 34, 16),
		shadowOverlay:  ui.RGBA(51, 43, 34, 32),
	}
	if high {
		p.background = ui.Hex("#ffffff")
		p.surface = ui.Hex("#f5f3ef")
		p.surfaceHover = ui.Hex("#d9d5cc")
		p.surfacePressed = ui.Hex("#c2bdb3")
		p.border = ui.Hex("#74726b")
		p.text = ui.Hex("#11110f")
		p.muted = ui.Hex("#55534d")
		p.focus = p.accent
		p.selection = ui.RGBA(189, 72, 27, 89)
	}
	return p
}

func nativeTheme(dark bool) (*ui.Theme, palette) {
	return nativeThemeWithContrast(dark, false)
}

func nativeThemeWithContrast(dark, high bool) (*ui.Theme, palette) {
	p := themePaletteWithContrast(dark, high)
	t := ui.LightTheme()
	if dark {
		t = ui.DarkTheme()
	}
	t.Dark = dark
	t.Background = p.background
	t.Surface = p.surface
	t.SurfaceHover = p.surfaceHover
	t.SurfacePressed = p.surfacePressed
	t.Border = p.border
	t.Text = p.text
	t.TextMuted = p.muted
	t.Accent = p.accent
	t.AccentHover = p.accentHover
	t.AccentPressed = p.accentPressed
	t.AccentText = p.accentText
	t.Danger = p.danger
	t.Warning = p.warning
	t.Success = p.success
	t.Selection = p.selection
	t.Focus = p.focus
	t.Scrollbar = p.scrollbar
	t.Radius = 8
	t.Spacing = 4
	t.FontSize = fontBody
	return t, p
}

var currentAppearanceDark = func() bool {
	return mygo.Theme.IsDark()
}

var appearanceReady atomic.Bool

var currentAppearanceHighContrast = appearanceHighContrast

var currentAppearanceReduceMotion = appearanceReduceMotion

func appearanceHighContrast() bool {
	if !appearanceReady.Load() {
		return false
	}
	return mygo.Theme.Preferences().HighContrast
}

func appearanceReduceMotion() bool {
	if !appearanceReady.Load() {
		return false
	}
	return mygo.Theme.Preferences().ReduceMotion
}
