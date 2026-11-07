package main

import (
	"fmt"
	"image"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// boarduiPalettes builds the four BoardUI palettes the design's contrast
// matrix covers: light/dark × standard/high contrast.
func boarduiPalettes() []palette {
	return []palette{
		boardUIThemePalette(false, false, false),
		boardUIThemePalette(false, true, false),
		boardUIThemePalette(true, false, false),
		boardUIThemePalette(true, true, false),
	}
}

// TestP31BoardUIContrastMatrix walks the design §2 matrix: text pairs need
// 4.5:1 and graphic pairs 3:1 on every light/dark × standard/high palette.
// contrastRatio and relativeLuminance come from theme_tokens_test.go.
func TestP31BoardUIContrastMatrix(t *testing.T) {
	textPairs := []struct {
		fg          string
		backgrounds []string
	}{
		{fg: "text", backgrounds: []string{"background", "surface", "surfaceSubtle", "raisedSurface"}},
		{fg: "muted", backgrounds: []string{"background", "surface", "surfaceSubtle", "raisedSurface"}},
		{fg: "accentText", backgrounds: []string{"background", "surface", "surfaceSubtle", "raisedSurface"}},
		{fg: "dangerText", backgrounds: []string{"background", "surface", "surfaceSubtle", "raisedSurface"}},
		{fg: "successText", backgrounds: []string{"background", "surface", "surfaceSubtle", "raisedSurface"}},
		{fg: "warningText", backgrounds: []string{"background", "surface", "surfaceSubtle", "raisedSurface"}},
		{fg: "onAccent", backgrounds: []string{"accent", "accentHover", "accentPressed"}},
	}
	graphicPairs := []struct {
		fg          string
		backgrounds []string
	}{
		{fg: "focus", backgrounds: []string{"background", "surface", "surfaceSubtle"}},
		{fg: "accent", backgrounds: []string{"background", "surface"}},
	}
	colors := func(p palette, name string) ui.Color {
		switch name {
		case "background":
			return p.background
		case "surface":
			return p.surface
		case "surfaceSubtle":
			return p.surfaceSubtle
		case "raisedSurface":
			return p.raisedSurface
		case "text":
			return p.text
		case "muted":
			return p.muted
		case "accentText":
			return p.accentText
		case "dangerText":
			return p.dangerText
		case "successText":
			return p.successText
		case "warningText":
			return p.warningText
		case "onAccent":
			return p.onAccent
		case "accent":
			return p.accent
		case "accentHover":
			return p.accentHover
		case "accentPressed":
			return p.accentPressed
		case "focus":
			return p.focus
		default:
			t.Fatalf("unknown palette field %q", name)
			return ui.Color{}
		}
	}
	describe := func(p palette) string {
		return fmt.Sprintf("dark=%v high=%v", p.dark, p.highContrast)
	}
	for _, p := range boarduiPalettes() {
		for _, pair := range textPairs {
			for _, bg := range pair.backgrounds {
				got := contrastRatio(colors(p, pair.fg), colors(p, bg))
				if got < 4.5 {
					t.Errorf("%s: %s on %s = %.3f, want ≥ 4.5", describe(p), pair.fg, bg, got)
				}
			}
		}
		for _, pair := range graphicPairs {
			for _, bg := range pair.backgrounds {
				got := contrastRatio(colors(p, pair.fg), colors(p, bg))
				if got < 3 {
					t.Errorf("%s: %s on %s = %.3f, want ≥ 3", describe(p), pair.fg, bg, got)
				}
			}
		}
	}
}

// TestP31BoardUIPaletteValues pins the design §2 table: the deviations
// (accent 600, darkened muted), the non-zero overlay/stage shadows, the
// preset and metrics tags, and the plugin overlay still applying last.
func TestP31BoardUIPaletteValues(t *testing.T) {
	light := boardUIThemePalette(false, false, false)
	dark := boardUIThemePalette(true, false, false)
	if light.background != ui.Hex("#ffffff") || dark.background != ui.Hex("#121212") {
		t.Fatalf("background = %v/%v", light.background, dark.background)
	}
	if light.surface != ui.Hex("#f7f7f7") || dark.surface != ui.Hex("#171717") {
		t.Fatalf("surface = %v/%v", light.surface, dark.surface)
	}
	if light.accent != ui.Hex("#155dfc") || dark.accent != ui.Hex("#155dfc") {
		t.Fatalf("accent = %v/%v, want blue-600 on both", light.accent, dark.accent)
	}
	if dark.accentText != ui.Hex("#8ec5ff") {
		t.Fatalf("dark accentText = %v, want blue-300", dark.accentText)
	}
	if light.shadowOverlay.A == 0 || light.shadowStage.A == 0 ||
		dark.shadowOverlay.A == 0 || dark.shadowStage.A == 0 {
		t.Fatalf("overlay/stage shadows must be non-zero: %v/%v %v/%v",
			light.shadowOverlay, light.shadowStage, dark.shadowOverlay, dark.shadowStage)
	}
	if light.preset != appearancePresetBoardUI || dark.preset != appearancePresetBoardUI {
		t.Fatal("palette preset must be boardui")
	}
	if light.composerRadius != 24 || light.mainRadius != 24 ||
		light.shellSpacing != 12 || light.userBubbleRadius != 16 {
		t.Fatalf("BoardUI metrics = %v", light.visualMetrics)
	}
	// Reduced transparency changes nothing: BoardUI never uses the glass.
	reduced := boardUIThemePalette(true, false, true)
	if reduced.background != dark.background || reduced.raisedSurface != dark.raisedSurface {
		t.Fatal("reduced transparency changed the BoardUI palette")
	}
	// High contrast replaces muted and the borders instead of BYOP's tokens.
	lightHigh := boardUIThemePalette(false, true, false)
	if lightHigh.muted != ui.Hex("#3a3a3a") || lightHigh.border != ui.Hex("#737373") {
		t.Fatalf("light high contrast = %v/%v", lightHigh.muted, lightHigh.border)
	}
	darkHigh := boardUIThemePalette(true, true, false)
	if darkHigh.muted != ui.Hex("#cecece") || darkHigh.border != ui.Hex("#a1a1a1") {
		t.Fatalf("dark high contrast = %v/%v", darkHigh.muted, darkHigh.border)
	}
	if darkHigh.accent != ui.Hex("#155dfc") {
		t.Fatal("high contrast must not inject the BYOP orange accent")
	}
}

// TestP31BoardUIFontRegistration pins §1.2: the embedded fonts register once
// into the "BYOP Inter" and "BYOP Noto Sans SC" families, the BoardUI theme
// uses the family list, and the BYOP and J Agent branches never ask for
// registration.
func TestP31BoardUIFontRegistration(t *testing.T) {
	if err := ensureBoardUIFonts(); err != nil {
		t.Fatalf("ensureBoardUIFonts: %v", err)
	}
	found := map[string]bool{}
	for _, family := range ui.FontFamilies() {
		if family.Name == "BYOP Inter" || family.Name == "BYOP Noto Sans SC" {
			if !family.Registered {
				t.Fatalf("family %q exists but is not Registered", family.Name)
			}
			found[family.Name] = true
		}
	}
	if !found["BYOP Inter"] || !found["BYOP Noto Sans SC"] {
		t.Fatalf("font families missing after registration: %v", found)
	}
	theme, _ := nativeThemeForAppearance(appearancePresetBoardUI, false, false, false)
	if theme.Font != boardUIFontFamily {
		t.Fatalf("BoardUI theme font = %q, want %q", theme.Font, boardUIFontFamily)
	}

	// The count seam: BYOP and J Agent never touch the registration.
	oldEnsure := ensureBoardUIFonts
	t.Cleanup(func() { ensureBoardUIFonts = oldEnsure })
	calls := 0
	ensureBoardUIFonts = func() error { calls++; return nil }
	_, _ = nativeThemeForAppearance(appearancePresetBYOP, false, false, false)
	_, _ = nativeThemeForAppearance(appearancePresetJAgent, true, false, false)
	if calls != 0 {
		t.Fatalf("BYOP/J Agent themes triggered %d font registrations", calls)
	}
	_, _ = nativeThemeForAppearance(appearancePresetBoardUI, false, false, false)
	if calls != 1 {
		t.Fatalf("BoardUI theme triggered %d font registrations, want 1", calls)
	}
}

// TestP31PresetSwitchPersistsBoardUI walks BYOP → BoardUI → J Agent → BYOP
// and reads the persisted preference back after every step (§11.3).
func TestP31PresetSwitchPersistsBoardUI(t *testing.T) {
	oldSource := setMygoThemeSource
	t.Cleanup(func() { setMygoThemeSource = oldSource })
	setMygoThemeSource = func(source mygo.ThemeSource) {}

	store := newByopPreferencesStore(t.TempDir())
	state := &appState{
		preferences:      defaultByopPreferences(),
		preferencesStore: store,
	}

	state.setAppearancePreset(appearancePresetBoardUI)
	if state.currentAppearancePreset() != appearancePresetBoardUI {
		t.Fatalf("preset = %q, want BoardUI", state.currentAppearancePreset())
	}
	saved, warning := store.load()
	if warning != "" {
		t.Fatal(warning)
	}
	if saved.AppearancePreset != string(appearancePresetBoardUI) {
		t.Fatalf("saved preset = %q, want boardui", saved.AppearancePreset)
	}

	state.setAppearancePreset(appearancePresetJAgent)
	saved, _ = store.load()
	if saved.AppearancePreset != string(appearancePresetJAgent) {
		t.Fatalf("saved preset = %q, want jagent", saved.AppearancePreset)
	}

	state.setAppearancePreset(appearancePresetBYOP)
	saved, _ = store.load()
	if saved.AppearancePreset != string(appearancePresetBYOP) {
		t.Fatalf("saved preset = %q, want byop", saved.AppearancePreset)
	}
}

// boarduiPixel reads a pixel of the last frame at logical coordinates,
// scaled to the tester's image.
func boarduiPixel(img *image.RGBA, x, y, logicalWidth int) (uint8, uint8, uint8) {
	scale := img.Bounds().Dx() / logicalWidth
	c := img.At(x*scale, y*scale)
	r, g, b, _ := c.RGBA()
	return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)
}

func boarduiDiff(a, b uint8) int {
	if a > b {
		return int(a) - int(b)
	}
	return int(b) - int(a)
}

// boarduiPixel3 wraps boarduiPixel as an array so helpers can return it.
func boarduiPixel3(img *image.RGBA, x, y, logicalWidth int) [3]uint8 {
	r, g, b := boarduiPixel(img, x, y, logicalWidth)
	return [3]uint8{r, g, b}
}
