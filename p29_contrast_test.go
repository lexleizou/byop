package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// P30 1.3: the contrast matrix. Every tone the process draws with is checked
// against the background it really sits on, for each of the palettes the
// design names (P31 adds the BoardUI light and dark rows). A failure means
// the tone must be raised, never the threshold lowered, so this test is what
// fixes the final values in p29_tones.go.

func p30ContrastPalettes() map[string]palette {
	return map[string]palette{
		"byop-light": themePaletteForAppearance(
			appearancePresetBYOP, false, false, false),
		"byop-dark": themePaletteForAppearance(
			appearancePresetBYOP, true, false, false),
		"jagent-glass": themePaletteForAppearance(
			appearancePresetJAgent, true, false, false),
		"jagent-reduce-transparency": themePaletteForAppearance(
			appearancePresetJAgent, true, false, true),
		"byop-high-contrast": themePaletteForAppearance(
			appearancePresetBYOP, false, true, false),
		"jagent-high-contrast": themePaletteForAppearance(
			appearancePresetJAgent, true, true, false),
		// P31 §2: the BoardUI preset draws its process on the opaque chat
		// card, so the same tones must clear against its effective surface
		// in light and dark too.
		"boardui-light": themePaletteForAppearance(
			appearancePresetBoardUI, false, false, false),
		"boardui-dark": themePaletteForAppearance(
			appearancePresetBoardUI, true, false, false),
	}
}

// p30ToneRoles names each tone and whether it carries text or a graphic, so
// the matrix below reads as the P30 1.2 table it enforces.
func p30ToneRoles(tones p29Tones) map[string]struct {
	color     ui.Color
	threshold float64
	start     float32
} {
	return map[string]struct {
		color     ui.Color
		threshold float64
		start     float32
	}{
		"label":      {tones.label, p29ContrastText, p29AlphaLabel},
		"meta":       {tones.meta, p29ContrastText, p29AlphaMeta},
		"statTitle":  {tones.statTitle, p29ContrastText, p29AlphaStatTitle},
		"status":     {tones.status, p29ContrastText, p29AlphaStatus},
		"statusLive": {tones.statusLive, p29ContrastText, p29AlphaStatusLive},
		"cardTitle":  {tones.cardTitle, p29ContrastText, p29AlphaCardTitle},
		"cardBody":   {tones.cardBody, p29ContrastText, p29AlphaCardBody},
		"muted":      {tones.muted, p29ContrastText, 0},
		// danger is the one role P30 1.2 fixes to an exact colour: p.danger,
		// now at full opacity. Alpha is the only lever 1.3 allows, and it is
		// spent, so the matrix holds it to the graphic threshold and
		// TestP30FailureColourStaysReadable states the rest.
		"danger":   {tones.danger, p29ContrastGraphic, 0},
		"statIcon": {tones.statIcon, p29ContrastGraphic, p29AlphaStatIcon},
		"chevron":  {tones.chevron, p29ContrastGraphic, p29AlphaChevron},
		"icon":     {tones.icon, p29ContrastGraphic, p29AlphaIcon},
		"iconLive": {tones.iconLive, p29ContrastGraphic, p29AlphaIconLive},
		"glyph":    {tones.glyph, p29ContrastGraphic, p29AlphaGlyph},
		"dot":      {tones.dot, p29ContrastGraphic, p29AlphaDot},
		"dotLive":  {tones.dotLive, p29ContrastGraphic, p29AlphaDotLive},
		"dash":     {tones.dash, p29ContrastGraphic, p29AlphaDash},
		"rail":     {tones.rail, p29ContrastGraphic, p29AlphaRail},
	}
}

func TestP30ToneContrastMatrix(t *testing.T) {
	// alphaByte mirrors how ui rounds an alpha into a colour channel.
	alphaByte := func(a float32) uint8 { return uint8(a*255 + 0.5) }
	for name, p := range p30ContrastPalettes() {
		p := p
		t.Run(name, func(t *testing.T) {
			bg := p29EffectiveBackground(p)
			for role, entry := range p30ToneRoles(p29TonesFor(p)) {
				got := wcagContrast(entry.color, bg)
				if got < entry.threshold {
					t.Errorf("%s on %s: contrast %.2f < %.1f (color %#v, background %#v)",
						role, name, got, entry.threshold, entry.color, bg)
				}
				// A tone may be raised above its starting alpha, never
				// lowered below it (P30 1.3). The comparison is on the
				// stored byte, because ui rounds an alpha to 8 bits.
				if entry.start > 0 {
					if want := alphaByte(entry.start); entry.color.A < want {
						t.Errorf("%s on %s: alpha %d/255 is below the P30 start %d/255",
							role, name, entry.color.A, want)
					}
				}
			}
		})
	}
}

func TestP30EffectiveBackgroundPerPreset(t *testing.T) {
	sameRGB := func(a, b ui.Color) bool {
		return a.R == b.R && a.G == b.G && a.B == b.B && a.A == b.A
	}
	// BYOP paints on the page background.
	light := themePaletteForAppearance(appearancePresetBYOP, false, false, false)
	if got := p29EffectiveBackground(light); !sameRGB(got, light.background) {
		t.Fatalf("byop background = %#v, want the page background %#v", got, light.background)
	}
	dark := themePaletteForAppearance(appearancePresetBYOP, true, false, false)
	if got := p29EffectiveBackground(dark); !sameRGB(got, dark.background) {
		t.Fatalf("byop dark background = %#v, want %#v", got, dark.background)
	}
	// The J Agent card tint over pure white: 0.80 × 27 + 0.20 × 255 = 72.6 → 73.
	glass := themePaletteForAppearance(appearancePresetJAgent, true, false, false)
	want := ui.Color{R: 73, G: 73, B: 73, A: 255}
	if got := p29EffectiveBackground(glass); !sameRGB(got, want) {
		t.Fatalf("jagent effective background = %#v, want %#v", got, want)
	}
	// Reduced transparency and high contrast fall back to the opaque surface.
	plain := themePaletteForAppearance(appearancePresetJAgent, true, false, true)
	if got := p29EffectiveBackground(plain); !sameRGB(got, plain.surface) {
		t.Fatalf("jagent reduced-transparency background = %#v, want the surface %#v",
			got, plain.surface)
	}
	high := themePaletteForAppearance(appearancePresetJAgent, true, true, false)
	if got := p29EffectiveBackground(high); !sameRGB(got, high.surface) {
		t.Fatalf("jagent high-contrast background = %#v, want the surface %#v", got, high.surface)
	}
	// P31 §2: BoardUI has no glass card and no wallpaper; the process sits
	// on the opaque chat card, whose fill is the palette surface.
	for _, boardui := range []palette{
		themePaletteForAppearance(appearancePresetBoardUI, false, false, false),
		themePaletteForAppearance(appearancePresetBoardUI, true, false, false),
	} {
		if got := p29EffectiveBackground(boardui); !sameRGB(got, boardui.surface) {
			t.Fatalf("boardui background = %#v, want the surface %#v", got, boardui.surface)
		}
	}
}

func TestP30FailureColourStaysReadable(t *testing.T) {
	// The failure colour stays p.danger at full opacity (P30 1.2). It must
	// clear 3:1 everywhere as a graphic, and where it carries the failure
	// text on the J Agent turn card — the case the user reported — it must
	// clear the text threshold too.
	for name, p := range p30ContrastPalettes() {
		tones := p29TonesFor(p)
		bg := p29EffectiveBackground(p)
		if tones.danger != p.danger || tones.danger.A != 255 {
			t.Fatalf("%s: failure colour = %#v, want p.danger %#v at full opacity",
				name, tones.danger, p.danger)
		}
		if got := wcagContrast(tones.danger, bg); got < p29ContrastGraphic {
			t.Errorf("%s: failure colour reads %.2f, want >= %.1f", name, got,
				p29ContrastGraphic)
		}
		if p.preset == appearancePresetJAgent {
			if got := wcagContrast(tones.danger, bg); got < p29ContrastText {
				t.Errorf("%s: failure text on the turn card reads %.2f, want >= %.1f",
					name, got, p29ContrastText)
			}
		}
	}
}

func TestP30HighContrastTonesAreFullText(t *testing.T) {
	for _, p := range []palette{
		themePaletteForAppearance(appearancePresetBYOP, false, true, false),
		themePaletteForAppearance(appearancePresetBYOP, true, true, false),
		themePaletteForAppearance(appearancePresetJAgent, true, true, false),
	} {
		tones := p29TonesFor(p)
		if tones.label != p.text || tones.meta != p.text || tones.cardBody != p.text {
			t.Fatalf("high contrast must paint text roles in p.text: %#v", tones)
		}
		if tones.rail != p.text || tones.icon != p.text || tones.dash != p.text {
			t.Fatalf("high contrast must paint graphic roles in p.text: %#v", tones)
		}
		if tones.danger != p.danger || tones.danger.A != 255 {
			t.Fatalf("failure colour = %#v, want p.danger at full opacity", tones.danger)
		}
	}
}

// TestP30TonesRaiseAlphaInsteadOfDrawingWeakInk is the "可上调不可下调" rule
// seen from the other side: on the palette the starting values were written
// for, the tone keeps the starting alpha; on a palette they cannot serve, it
// moves up. The J Agent glass is that palette for the quietest roles.
func TestP30TonesRaiseAlphaInsteadOfDrawingWeakInk(t *testing.T) {
	light := themePaletteForAppearance(appearancePresetBYOP, false, false, false)
	scaled := p29AlphaMeta*255 + 0.5
	want := uint8(scaled)
	if got := p29TonesFor(light).meta.A; got != want {
		t.Fatalf("byop light meta alpha = %d/255, want the starting %d/255: the starting values were set for this palette",
			p29TonesFor(light).meta.A, want)
	}
	glass := themePaletteForAppearance(appearancePresetJAgent, true, false, false)
	if got, want := p29EffectiveBackground(glass), (ui.Color{R: 73, G: 73, B: 73, A: 255}); got != want {
		t.Fatalf("glass background = %#v, want %#v", got, want)
	}
	if wcagContrast(glass.text.Alpha(p29AlphaMeta), p29EffectiveBackground(glass)) >= p29ContrastText {
		t.Skip("the starting alpha already clears the threshold on the glass card")
	}
	raised := p29ToneAlpha(glass, p29AlphaMeta, p29ContrastText)
	if raised <= p29AlphaMeta {
		t.Fatalf("meta alpha on the glass card = %.2f, want more than %.2f",
			raised, p29AlphaMeta)
	}
	if got := wcagContrast(glass.text.Alpha(raised), p29EffectiveBackground(glass)); got < p29ContrastText {
		t.Fatalf("raised meta still reads at %.2f, want >= %.1f", got, p29ContrastText)
	}
}

func TestP30AccessibleInkKeepsReadableColours(t *testing.T) {
	for name, p := range p30ContrastPalettes() {
		bg := p29EffectiveBackground(p)
		for _, color := range []ui.Color{p.muted, p.success, p.warning, p.danger} {
			got := p29AccessibleInk(p, color, p29ContrastText)
			if wcagContrast(got, bg) < p29ContrastText {
				t.Errorf("%s: %#v nudged to %#v still reads %.2f on %#v",
					name, color, got, wcagContrast(got, bg), bg)
			}
			if wcagContrast(color, bg) >= p29ContrastText && got != color {
				t.Errorf("%s: %#v already passed and must not change, got %#v",
					name, color, got)
			}
		}
	}
}

// TestP30RateCapsuleStaysReadable covers the live t/s capsule: the tier
// grading is p18's test, so only the displayed ink is checked here, against
// the surface the capsule is drawn on.
func TestP30RateCapsuleStaysReadable(t *testing.T) {
	rates := []float64{80, 45, 25, 8, 1}
	for name, p := range p30ContrastPalettes() {
		t.Run(name, func(t *testing.T) {
			theme, _ := nativeThemeForAppearance(p.preset, p.dark,
				p.highContrast, p.reduceTransparency)
			bg := p29EffectiveBackground(p)
			for _, rate := range rates {
				foreground, background, border := piTPSCapsuleStyle(theme, p, rate)
				if got := wcagContrast(foreground, bg); got < p29ContrastText {
					t.Errorf("rate %.0f t/s capsule reads %.2f, want >= %.1f (%#v on %#v)",
						rate, got, p29ContrastText, foreground, bg)
				}
				// The ink still comes from the tier the grading picked, so
				// the colour keeps saying which speed band the turn is in.
				tier, _, _ := piTPSStyle(theme, rate)
				if want := p29AccessibleInk(p, tier, p29ContrastText); foreground != want {
					t.Fatalf("rate %.0f capsule ink %#v does not come from the tier colour %#v",
						rate, foreground, want)
				}
				if background != foreground.Alpha(.10) || border != foreground.Alpha(.30) {
					t.Fatalf("rate %.0f capsule fill/edge must follow the final ink: %#v %#v",
						rate, background, border)
				}
			}
		})
	}
}

func TestWCAGContrastKnownValues(t *testing.T) {
	black := ui.Color{R: 0, G: 0, B: 0, A: 255}
	white := ui.Color{R: 255, G: 255, B: 255, A: 255}
	if got := wcagContrast(black, white); got < 20.9 || got > 21.1 {
		t.Fatalf("black on white = %.2f, want 21", got)
	}
	if got := wcagContrast(white, black); got < 20.9 || got > 21.1 {
		t.Fatalf("the ratio must not depend on order, got %.2f", got)
	}
	if got := wcagContrast(white, white); got != 1 {
		t.Fatalf("white on white = %.2f, want 1", got)
	}
	// #767676 on white is the standard example just above the 4.5 threshold.
	gray := ui.Hex("#767676")
	if got := wcagContrast(gray, white); got < 4.5 || got > 4.6 {
		t.Fatalf("#767676 on white = %.2f, want about 4.54", got)
	}
	// A translucent colour is judged as the colour it paints: white text at
	// full strength on a background, and a fully transparent one is the
	// background itself.
	if got := wcagContrast(ui.Hex("#808080").Alpha(1), ui.Hex("#808080")); got != 1 {
		t.Fatalf("an opaque colour on itself = %.2f, want 1", got)
	}
	if got := wcagContrast(ui.Hex("#000000").Alpha(0), white); got != 1 {
		t.Fatalf("nothing painted on white = %.2f, want 1", got)
	}
	if got := wcagContrast(black.Alpha(.5), white); got < 3.9 || got > 4.1 {
		// Half-strength black paints #808080, and mid grey on white is 4.0:1.
		// The check exists to prove the composite happens before the ratio.
		t.Fatalf("half-strength black on white should read about 4.0, got %.2f", got)
	}
}

func TestP30TurnCardGeometryConstants(t *testing.T) {
	// The card geometry P30 1.1 fixes; the layout test in p29_view_test.go
	// proves the content really is inset by these values.
	if p29TurnCardPaddingY != 12 || p29TurnCardPaddingX != 16 {
		t.Fatalf("turn card padding = %v/%v, want 12/16",
			p29TurnCardPaddingY, p29TurnCardPaddingX)
	}
	if p29TurnCardTint.A != 204 { // 0.80 × 255, rounded
		t.Fatalf("turn card tint = %#v, want 80%% opacity", p29TurnCardTint)
	}
	if p29RailWidth != 1.5 {
		t.Fatalf("rail width = %v, want 1.5", p29RailWidth)
	}
}
