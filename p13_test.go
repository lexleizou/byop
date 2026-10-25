package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestP13NormalizeGlassPreferences(t *testing.T) {
	preferences := defaultByopPreferences()
	preferences.Wallpaper = "  UNKNOWN  "
	preferences.NavTransparency = -5
	preferences.GlassBlur = 99

	normalizeByopPreferences(&preferences)
	if preferences.Wallpaper != "ember" {
		t.Fatalf("wallpaper = %q, want ember", preferences.Wallpaper)
	}
	if preferences.NavTransparency != 0 {
		t.Fatalf("nav transparency = %d, want 0", preferences.NavTransparency)
	}
	if preferences.GlassBlur != runtimeTokens.Glass.BlurMax {
		t.Fatalf("glass blur = %d, want %d", preferences.GlassBlur, runtimeTokens.Glass.BlurMax)
	}

	preferences.NavTransparency = 140
	normalizeByopPreferences(&preferences)
	if preferences.NavTransparency != 100 {
		t.Fatalf("nav transparency = %d, want 100", preferences.NavTransparency)
	}
}

func TestP13GlassPreferencesRoundTripAndDefaults(t *testing.T) {
	configDir := t.TempDir()
	store := newByopPreferencesStore(configDir)
	preferences := defaultByopPreferences()
	preferences.NavTransparency = 0
	preferences.GlassBlur = 0
	preferences.GlassRefraction = false
	if err := store.save(preferences); err != nil {
		t.Fatalf("save preferences: %v", err)
	}

	loaded, warning := store.load()
	if warning != "" {
		t.Fatalf("load preferences: %s", warning)
	}
	if loaded.NavTransparency != 0 || loaded.GlassBlur != 0 || loaded.GlassRefraction {
		t.Fatalf("explicit glass values were not preserved: %#v", loaded)
	}

	legacyPath := filepath.Join(configDir, "preferences.json")
	if err := os.WriteFile(legacyPath, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatalf("write legacy preferences: %v", err)
	}
	loaded, warning = store.load()
	if warning != "" {
		t.Fatalf("load legacy preferences: %s", warning)
	}
	if loaded.Wallpaper != "ember" ||
		loaded.NavTransparency != runtimeTokens.Glass.NavTransparencyDefault ||
		loaded.GlassBlur != runtimeTokens.Glass.BlurDefault ||
		!loaded.GlassRefraction {
		t.Fatalf("missing glass fields did not use defaults: %#v", loaded)
	}
}

func TestP13NavGlassTint(t *testing.T) {
	for _, test := range []struct {
		transparency int
		alpha        uint8
	}{
		{transparency: 0, alpha: 235},
		{transparency: 55, alpha: 106},
		{transparency: 100, alpha: 0},
	} {
		color := navGlassTint(test.transparency)
		if color.R != 25 || color.G != 25 || color.B != 24 {
			t.Errorf("navGlassTint(%d) RGB = (%d,%d,%d), want (25,25,24)",
				test.transparency, color.R, color.G, color.B)
		}
		if color.A != test.alpha {
			t.Errorf("navGlassTint(%d) alpha = %d, want %d",
				test.transparency, color.A, test.alpha)
		}
	}
}
