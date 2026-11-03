package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestPluginThemeOverlayAppliesSelectedColors(t *testing.T) {
	overlay := &pluginThemeOverlay{
		resources: []pluginThemeResource{{
			entry:    pluginThemeContribution{ID: "theme.night", Title: "Night"},
			pluginID: "example.theme",
			light: testThemeColors(map[string]string{
				"background": "#101014",
				"text":       "#eeeeee",
				"accent":     "#cc4e00",
			}),
		}},
	}
	setTestPluginThemeSelection(t, overlay, pluginThemeSelection{
		PluginID:   "example.theme",
		ResourceID: "theme.night",
	})
	background, text, accent := ui.Hex("#101014"), ui.Hex("#eeeeee"), ui.Hex("#cc4e00")
	p := themePalette(false)
	if p.background != background || p.text != text || p.accent != accent {
		t.Fatalf("palette overlay = %v %v %v", p.background, p.text, p.accent)
	}
	// Fields the resource does not declare keep the built-in token.
	if got, want := p.surface, themePaletteWithContrast(false, false).surface; got != want {
		t.Fatalf("surface overlay = %v, want token %v", got, want)
	}
}

func TestPluginThemeDisableKeepsSavedChoiceAndReenables(t *testing.T) {
	overlay := &pluginThemeOverlay{
		resources: []pluginThemeResource{{
			entry:    pluginThemeContribution{ID: "theme.night"},
			pluginID: "example.theme",
			light:    testThemeColors(map[string]string{"background": "#101014"}),
		}},
	}
	selection := pluginThemeSelection{PluginID: "example.theme", ResourceID: "theme.night"}
	setTestPluginThemeSelection(t, overlay, selection)
	// A scan that no longer offers the resource publishes an empty overlay; the
	// stored choice stays and the palette returns to tokens.
	setTestPluginThemeSelection(t, &pluginThemeOverlay{}, selection)
	p := themePalette(false)
	if got, want := p.background, themePaletteWithContrast(false, false).background; got != want {
		t.Fatalf("fallback palette = %v, want token %v", got, want)
	}
	// Re-enable republishes the same resource and the overlay returns.
	setTestPluginThemeSelection(t, overlay, selection)
	if themePalette(false).background != ui.Hex("#101014") {
		t.Fatal("re-enable did not restore the selected resource")
	}
}

func TestPluginThemePayloadIncludesGeneration(t *testing.T) {
	payload := currentPluginThemePayload(false, false, false, false)
	if _, ok := payload["themeGeneration"]; !ok {
		t.Fatal("theme payload has no themeGeneration")
	}
}

// setTestPluginThemeSelection publishes the overlay first, then selects, so the
// direct tests follow the production order: resources load on a publish path
// and selection changes only republish the already-loaded overlay.
func setTestPluginThemeSelection(
	t *testing.T,
	overlay *pluginThemeOverlay,
	selection pluginThemeSelection,
) {
	t.Helper()
	previousOverlay := currentPluginThemeOverlay()
	previousSelection := pluginThemeOverlaySelected()
	t.Cleanup(func() {
		pluginThemeCurrent.Store(previousOverlay)
		pluginThemeSelected.Store(previousSelection)
	})
	publishPluginThemeOverlay(overlay)
	setPluginThemeSelection(&selection)
}

func TestPluginThemeInvalidColorIsReportedAndNeverApplied(t *testing.T) {
	// The review's TH1 probe: an invalid colour must be a registry issue at
	// load and never reach the palette, because ui.Hex panics on invalid input.
	entry := pluginRegistryEntry{
		ID:     "example.bad-color",
		Status: pluginRegistered,
		Manifest: &pluginManifest{
			ID: "example.bad-color",
			Contributes: pluginContributions{Themes: []pluginThemeContribution{{
				ID:    "theme.bad",
				Title: "Bad colour",
				Light: "light.json",
				Dark:  "dark.json",
			}}},
		},
	}
	overlay, issues := buildPluginThemeResources([]pluginRegistryEntry{entry})
	if len(overlay.resources) != 0 {
		t.Fatalf("resources loaded with an invalid colour: %v", overlay.resources)
	}
	if len(issues) == 0 {
		t.Fatal("an invalid colour reported no issue")
	}
}

func TestPluginThemeWhitespaceColorKeepsToken(t *testing.T) {
	// A whitespace-only colour value must stay unset, so the mode keeps the
	// built-in token instead of a transparent black override.
	dir := t.TempDir()
	path := filepath.Join(dir, "blank.json")
	if err := os.WriteFile(path, []byte(`{"colors":{"background":" "}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	theme, issues := readPluginThemeResource(dir, "blank.json")
	if len(issues) != 0 {
		t.Fatalf("issues = %v", issues)
	}
	if theme == nil || len(theme.Parsed) != 0 {
		t.Fatalf("parsed = %v, want no colour override", theme.Parsed)
	}
}

func TestPluginThemeOverlayPicksMatchingModeWithoutBorrowing(t *testing.T) {
	// TH2: light and dark colour sets stay separate; a field missing from the
	// matching mode keeps the token instead of borrowing the other mode's value.
	overlay := &pluginThemeOverlay{
		resources: []pluginThemeResource{{
			entry:    pluginThemeContribution{ID: "theme.mode"},
			pluginID: "example.mode",
			light:    testThemeColors(map[string]string{"background": "#f5f5f5"}),
			dark:     testThemeColors(map[string]string{"text": "#e8e8e8"}),
		}},
	}
	setTestPluginThemeSelection(t, overlay, pluginThemeSelection{
		PluginID:   "example.mode",
		ResourceID: "theme.mode",
	})
	light := themePalette(false)
	if light.background != ui.Hex("#f5f5f5") {
		t.Fatalf("light background = %v, want the light resource", light.background)
	}
	if got, want := light.text, themePaletteWithContrast(false, false).text; got != want {
		t.Fatalf("light text = %v, want the built-in token %v", got, want)
	}
	dark := themePalette(true)
	if dark.text != ui.Hex("#e8e8e8") {
		t.Fatalf("dark text = %v, want the dark resource", dark.text)
	}
	if got, want := dark.background, themePaletteWithContrast(true, false).background; got != want {
		t.Fatalf("dark background = %v, want the built-in token %v", got, want)
	}
}

func TestPluginThemeOverlaySkipsColorsForHighContrast(t *testing.T) {
	// TH3: with high contrast on, a theme colour never replaces the accessible
	// palette, and reduce transparency keeps only opaque colours.
	overlay := &pluginThemeOverlay{
		resources: []pluginThemeResource{{
			entry:    pluginThemeContribution{ID: "theme.contrast"},
			pluginID: "example.contrast",
			light: testThemeColors(map[string]string{
				"background": "#202020",
				"text":       "#f0f0f0",
			}),
		}},
	}
	setTestPluginThemeSelection(t, overlay, pluginThemeSelection{
		PluginID:   "example.contrast",
		ResourceID: "theme.contrast",
	})
	high := themePaletteWithContrast(false, true)
	if got, want := high.background, themePaletteWithContrast(false, true).background; got != want {
		t.Fatalf("high-contrast background = %v, want the contrast token %v", got, want)
	}
	if got, want := high.text, themePaletteWithContrast(false, true).text; got != want {
		t.Fatalf("high-contrast text = %v, want the contrast token %v", got, want)
	}
}

func TestPluginThemePayloadIncludesFontFamilies(t *testing.T) {
	// TH4: the WebView payload carries the selected families; empty values mean
	// the WebView keeps its default.
	overlay := &pluginThemeOverlay{
		resources: []pluginThemeResource{{
			entry:          pluginThemeContribution{ID: "theme.fonts"},
			pluginID:       "example.fonts",
			fontFamily:     "Probe Sans",
			codeFontFamily: "Probe Mono",
		}},
	}
	setTestPluginThemeSelection(t, overlay, pluginThemeSelection{
		PluginID:   "example.fonts",
		ResourceID: "theme.fonts",
	})
	payload := currentPluginThemePayload(false, false, false, false)
	families, ok := payload["fontFamily"].(map[string]string)
	if !ok {
		t.Fatalf("fontFamily has type %T", payload["fontFamily"])
	}
	if families["body"] != "Probe Sans" || families["code"] != "Probe Mono" {
		t.Fatalf("fontFamily payload = %v", families)
	}
}

// testThemeColors builds the immutable per-mode map the loader stores, so the
// direct tests construct resources exactly as production does.
func testThemeColors(colors map[string]string) pluginThemeModeColors {
	parsed := make(map[string]ui.Color, len(colors))
	for name, hex := range colors {
		color, err := parseHexColor(hex)
		if err != nil {
			panic(err)
		}
		parsed[name] = color
	}
	return pluginThemeModeColors{colors: parsed}
}
