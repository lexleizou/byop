package main

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// P30 1.2: the process chrome's text, icon and line colours, resolved per
// palette. P29 scattered p.text.Alpha(x) literals across the view, values
// copied from a commercial app that draws on an opaque background; on a
// wallpaper the same alphas read as washed-out grey. This file is the single
// place those colours are defined: p29TonesFor(p) returns the set a palette
// must draw with, and the contrast test in p29_contrast_test.go proves every
// entry clears WCAG against that palette's effective background.
//
// The alpha constants below are the P30 starting values from the design.
// They are floors, never ceilings: p29ToneAlpha raises one until it clears
// the contrast requirement for the palette in use, so a palette the starting
// value cannot serve shows the same role darker or lighter, never weaker.

// p29ContrastMinimums are the WCAG 2.x thresholds the design sets: body-size
// text clears 4.5:1, and lines, icons and other graphics clear 3:1.
const (
	p29ContrastText    float64 = 4.5
	p29ContrastGraphic float64 = 3
)

// The starting alphas of P30 1.2. Roles the table left at their P29 value
// (the avatar glyph, the live marker) keep that value and still get raised
// when the palette needs it.
const (
	p29AlphaLabel      float32 = .84 // model name, the user's "You"
	p29AlphaMeta       float32 = .64 // times, sub-summaries, action status words
	p29AlphaStatTitle  float32 = .80 // process summary title
	p29AlphaStatIcon   float32 = .72 // the ListTree mark
	p29AlphaChevron    float32 = .62 // fold and row chevrons
	p29AlphaStatus     float32 = .72 // activity line, not the live one
	p29AlphaStatusLive float32 = .94 // activity line, live
	p29AlphaIcon       float32 = .72 // rail markers, node icons
	p29AlphaIconLive   float32 = .70 // live marker, P29 value
	p29AlphaGlyph      float32 = .70 // avatar glyph, P29 value
	p29AlphaDot        float32 = .62 // thinking dot
	p29AlphaDotLive    float32 = .86 // live thinking dot
	p29AlphaDash       float32 = .38 // thinking-card dashed border
	p29AlphaCardTitle  float32 = .72 // the "Thinking" title
	p29AlphaCardBody   float32 = .78 // thinking body, tool row name
	p29AlphaRail       float32 = .46 // the timeline rail
)

// p29RailWidth is the P30 rail width. P29 drew 1px, which vanished on a
// light wallpaper.
const p29RailWidth float32 = 1.5

// p29Tones is the resolved colour set for one palette. Every field is a
// colour the view paints; the roles match the P30 1.2 table one to one.
type p29Tones struct {
	label      ui.Color
	meta       ui.Color
	statTitle  ui.Color
	statIcon   ui.Color
	chevron    ui.Color
	status     ui.Color
	statusLive ui.Color
	icon       ui.Color
	iconLive   ui.Color
	glyph      ui.Color
	dot        ui.Color
	dotLive    ui.Color
	dash       ui.Color
	cardTitle  ui.Color
	cardBody   ui.Color
	rail       ui.Color
	danger     ui.Color
	// dangerText is the failure colour for text. P30 1.2 fixes the failure
	// colour to p.danger at full opacity, but on the light palette that reads
	// 3.85:1, under the 4.5:1 P30 1.3 asks of every text role, and alpha has
	// nothing left to give. The palette's own darker danger text token carries
	// the same meaning and clears the threshold, so icons keep p.danger and
	// failure prose uses this.
	dangerText ui.Color
	// muted is not a row of the P30 1.2 table: it is the palette's own muted
	// ink (p.muted) nudged until readable, which is all P30 1.2 asks of the
	// sub-summaries, the token line and the action status word. Roles keep
	// their colour; only the displayed value moves.
	muted ui.Color
}

// p29TonesFor resolves the tones for one palette. High contrast drops the
// alpha game entirely and paints every role in full p.text, per P30 1.2; the
// failure colour stays p.danger, now at full opacity.
func p29TonesFor(p palette) p29Tones {
	t := p29Tones{danger: p.danger, dangerText: p29AccessibleInk(p, p.dangerText, p29ContrastText)}
	if p.highContrast {
		solid := p.text
		t.label, t.meta, t.statTitle, t.statIcon = solid, solid, solid, solid
		t.chevron, t.status, t.statusLive = solid, solid, solid
		t.icon, t.iconLive, t.glyph = solid, solid, solid
		t.dot, t.dotLive, t.dash = solid, solid, solid
		t.cardTitle, t.cardBody, t.rail = solid, solid, solid
		t.muted, t.dangerText = solid, solid
		return t
	}
	t.label = p.text.Alpha(p29ToneAlpha(p, p29AlphaLabel, p29ContrastText))
	t.meta = p.text.Alpha(p29ToneAlpha(p, p29AlphaMeta, p29ContrastText))
	t.statTitle = p.text.Alpha(p29ToneAlpha(p, p29AlphaStatTitle, p29ContrastText))
	t.statIcon = p.text.Alpha(p29ToneAlpha(p, p29AlphaStatIcon, p29ContrastGraphic))
	t.chevron = p.text.Alpha(p29ToneAlpha(p, p29AlphaChevron, p29ContrastGraphic))
	t.status = p.text.Alpha(p29ToneAlpha(p, p29AlphaStatus, p29ContrastText))
	t.statusLive = p.text.Alpha(p29ToneAlpha(p, p29AlphaStatusLive, p29ContrastText))
	t.icon = p.text.Alpha(p29ToneAlpha(p, p29AlphaIcon, p29ContrastGraphic))
	t.iconLive = p.text.Alpha(p29ToneAlpha(p, p29AlphaIconLive, p29ContrastGraphic))
	t.glyph = p.text.Alpha(p29ToneAlpha(p, p29AlphaGlyph, p29ContrastGraphic))
	t.dot = p.text.Alpha(p29ToneAlpha(p, p29AlphaDot, p29ContrastGraphic))
	t.dotLive = p.text.Alpha(p29ToneAlpha(p, p29AlphaDotLive, p29ContrastGraphic))
	t.dash = p.text.Alpha(p29ToneAlpha(p, p29AlphaDash, p29ContrastGraphic))
	t.cardTitle = p.text.Alpha(p29ToneAlpha(p, p29AlphaCardTitle, p29ContrastText))
	t.cardBody = p.text.Alpha(p29ToneAlpha(p, p29AlphaCardBody, p29ContrastText))
	t.rail = p.text.Alpha(p29ToneAlpha(p, p29AlphaRail, p29ContrastGraphic))
	t.muted = p29AccessibleInk(p, p.muted, p29ContrastText)
	return t
}

// p29ToneAlpha returns the alpha a role draws with in this palette: the
// starting value, raised in small steps until the composited colour clears
// the threshold against the palette's effective background. One step is left
// at full strength when nothing else reaches the threshold, so a palette
// whose ink cannot clear it stays at the strongest available colour rather
// than looping.
func p29ToneAlpha(p palette, start float32, threshold float64) float32 {
	bg := p29EffectiveBackground(p)
	alpha := start
	for {
		if wcagContrast(p.text.Alpha(alpha), bg) >= threshold || alpha >= 1 {
			return alpha
		}
		next := alpha + 0.02
		if next > 1 {
			next = 1
		}
		alpha = next
	}
}

// p29AccessibleInk keeps a palette colour that is already opaque — p.muted,
// a rate tier — as it is when it clears the threshold, and otherwise mixes it
// toward p.text, which is the palette's strongest ink. Only the displayed
// colour changes; the role and the logic that picked the colour do not, so a
// tier colour keeps saying which band it grades.
func p29AccessibleInk(p palette, color ui.Color, threshold float64) ui.Color {
	bg := p29EffectiveBackground(p)
	ink := color
	for step := 0; step < 16; step++ {
		if wcagContrast(ink, bg) >= threshold {
			return ink
		}
		ink = ink.Mix(p.text, .12)
	}
	return ink
}

// p29EffectiveBackground answers "what is this text really sitting on?" for
// the contrast rule (P30 1.3).
//
//   - BYOP draws on the page background.
//   - The J Agent preset draws inside the turn card, whose tint is translucent
//     over the wallpaper. The lightest wallpaper pixel is white, and white is
//     also the worst case for light-on-light text, so the tint is composited
//     onto pure white.
//   - With high contrast or reduced transparency the card falls back to the
//     opaque surface, so that surface is the background.
func p29EffectiveBackground(p palette) ui.Color {
	if p.preset == appearancePresetJAgent && !p.highContrast && !p.reduceTransparency {
		return compositeOver(p29TurnCardTint, ui.Color{R: 255, G: 255, B: 255, A: 255})
	}
	if p.preset == appearancePresetJAgent {
		return p.surface
	}
	return p.background
}

// compositeOver flattens a translucent foreground onto an opaque background,
// the way the renderer would paint it.
func compositeOver(fg, bg ui.Color) ui.Color {
	a := float32(fg.A) / 255
	if a >= 1 {
		return ui.Color{R: fg.R, G: fg.G, B: fg.B, A: 255}
	}
	if a <= 0 {
		return ui.Color{R: bg.R, G: bg.G, B: bg.B, A: 255}
	}
	blend := func(f, b uint8) uint8 {
		return uint8(float32(f)*a + float32(b)*(1-a) + 0.5)
	}
	return ui.Color{R: blend(fg.R, bg.R), G: blend(fg.G, bg.G), B: blend(fg.B, bg.B), A: 255}
}

// wcagContrast is the WCAG 2.x contrast ratio between a foreground and a
// background: relative luminances, (L1 + 0.05) / (L2 + 0.05), always ≥ 1. A
// translucent foreground is composited onto the background first, because
// that flattened colour is what the eye actually compares.
func wcagContrast(fg, bg ui.Color) float64 {
	flat := compositeOver(fg, bg)
	fgL, bgL := wcagLuminance(flat), wcagLuminance(bg)
	if fgL < bgL {
		fgL, bgL = bgL, fgL
	}
	return (fgL + 0.05) / (bgL + 0.05)
}

// wcagLuminance is the WCAG 2.x relative luminance of a colour, composited
// onto white first so a translucent colour is judged on what it shows.
func wcagLuminance(c ui.Color) float64 {
	c = compositeOver(c, ui.Color{R: 255, G: 255, B: 255, A: 255})
	channel := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
}
