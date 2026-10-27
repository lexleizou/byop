package main

import (
	"sync/atomic"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	fontMicro     float32 = 12
	fontCaption   float32 = 12
	fontSmall     float32 = 12
	fontBody      float32 = 14
	fontBodyLarge float32 = 14
	fontUILarge   float32 = 14
	fontTitle     float32 = 18
	fontPageTitle float32 = 18
	fontDisplay   float32 = 18
)

// palette is the semantic view consumed by native views. Its values come from
// the canonical design tokens; views should not introduce another palette.
type palette struct {
	dark            bool
	background      ui.Color
	surface         ui.Color
	surfaceSubtle   ui.Color
	surfaceHover    ui.Color
	surfacePressed  ui.Color
	border          ui.Color
	borderStrong    ui.Color
	text            ui.Color
	muted           ui.Color
	accent          ui.Color
	accentHover     ui.Color
	accentPressed   ui.Color
	accentText      ui.Color
	onAccent        ui.Color
	danger          ui.Color
	warning         ui.Color
	success         ui.Color
	dangerText      ui.Color
	warningText     ui.Color
	successText     ui.Color
	selection       ui.Color
	focus           ui.Color
	scrollbar       ui.Color
	glassTint       ui.Color
	readingSurface  ui.Color
	raisedSurface   ui.Color
	composerSurface ui.Color
	popoverTint     ui.Color
	popoverBorder   ui.Color
	shadow          ui.Color
	shadowCard      ui.Color
	shadowOverlay   ui.Color
	shadowStage     ui.Color
}

func themePalette(dark bool) palette {
	return themePaletteWithContrast(dark, false)
}

func themePaletteWithContrast(dark, high bool) palette {
	colors := runtimeTokens.Color.Light
	if dark {
		colors = runtimeTokens.Color.Dark
	}
	p := palette{
		dark:            dark,
		background:      tokenColor(colors, "background"),
		surface:         tokenColor(colors, "surface"),
		surfaceSubtle:   tokenColor(colors, "surfaceSubtle"),
		surfaceHover:    tokenColor(colors, "controlHover"),
		surfacePressed:  tokenColor(colors, "controlPressed"),
		border:          tokenColor(colors, "border"),
		borderStrong:    tokenColor(colors, "borderStrong"),
		text:            tokenColor(colors, "text"),
		muted:           tokenColor(colors, "textMuted"),
		accent:          tokenColor(colors, "accent"),
		accentHover:     tokenColor(colors, "accentHover"),
		accentPressed:   tokenColor(colors, "accentPressed"),
		accentText:      tokenColor(colors, "accentText"),
		onAccent:        tokenColor(colors, "onAccent"),
		danger:          tokenColor(colors, "danger"),
		warning:         tokenColor(colors, "warning"),
		success:         tokenColor(colors, "success"),
		dangerText:      tokenColor(colors, "dangerText"),
		warningText:     tokenColor(colors, "warningText"),
		successText:     tokenColor(colors, "successText"),
		selection:       tokenColor(colors, "selection"),
		focus:           tokenColor(colors, "focus"),
		scrollbar:       tokenColor(colors, "scrollbar"),
		glassTint:       tokenColor(colors, "glassTint"),
		readingSurface:  tokenColor(colors, "readingSurface"),
		raisedSurface:   tokenColor(colors, "raisedSurface"),
		composerSurface: tokenColor(colors, "composerSurface"),
		popoverTint:     tokenColor(colors, "popoverTint"),
		popoverBorder:   tokenColor(colors, "popoverBorder"),
		shadow:          ui.Hex(runtimeTokens.Shadow.Overlay.Color),
		shadowCard:      ui.Color{},
		shadowOverlay:   ui.Hex(runtimeTokens.Shadow.Overlay.Color),
		shadowStage:     ui.Hex(runtimeTokens.Shadow.Stage.Color),
	}
	if high {
		contrast := runtimeTokens.Color.Contrast.Light
		if dark {
			contrast = runtimeTokens.Color.Contrast.Dark
		}
		p.background = ui.Hex(contrast["background"])
		p.surface = ui.Hex(contrast["surface"])
		p.surfaceHover = ui.Hex(contrast["surfaceHover"])
		p.border = ui.Hex(contrast["border"])
		p.muted = ui.Hex(contrast["muted"])
		p.focus = ui.Hex(contrast["focus"])
		p.readingSurface = p.background
		p.raisedSurface = p.surface
		p.composerSurface = p.surface
		p.popoverTint = p.surface
		p.popoverBorder = p.border
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
	t.AccentText = p.onAccent
	t.Danger = p.danger
	t.Warning = p.warning
	t.Success = p.success
	t.Selection = p.selection
	t.Focus = p.focus
	t.Scrollbar = p.scrollbar
	t.Radius = runtimeTokens.Dimension.Radius["input"].Value
	t.Spacing = 4
	t.FontSize = runtimeTokens.Font.Body.Value
	return t, p
}

var currentAppearanceDark = appearanceDark

var appearanceReady atomic.Bool

var currentAppearanceHighContrast = appearanceHighContrast

var currentAppearanceReduceMotion = appearanceReduceMotion

var currentAppearanceReduceTransparency = appearanceReduceTransparency

func appearanceDark() bool {
	if !appearanceReady.Load() {
		return false
	}
	return mygo.Theme.IsDark()
}

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

func appearanceReduceTransparency() bool {
	if !appearanceReady.Load() {
		return false
	}
	return mygo.Theme.Preferences().ReduceTransparency
}
