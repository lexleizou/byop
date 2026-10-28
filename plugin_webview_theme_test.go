package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPluginThemePayloadIncludesSemanticTextColors(t *testing.T) {
	tests := []struct {
		name string
		dark bool
		want map[string]string
	}{
		{
			name: "light",
			want: map[string]string{
				"onAccent":    "#21201C",
				"successText": "#1D3B31",
				"warningText": "#4F3422",
				"dangerText":  "#CE2C31",
				"focus":       "#CC4E00",
			},
		},
		{
			name: "dark",
			dark: true,
			want: map[string]string{
				"onAccent":    "#21201C",
				"successText": "#1FD8A4",
				"warningText": "#FFCA16",
				"dangerText":  "#FF9592",
				"focus":       "#F76B15",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := currentPluginThemePayload(test.dark, false, false, false)
			colors, ok := payload["colors"].(map[string]string)
			if !ok {
				t.Fatalf("theme colors have type %T", payload["colors"])
			}
			for name, want := range test.want {
				if got := colors[name]; got != want {
					t.Errorf("theme color %s = %q, want %q", name, got, want)
				}
			}
		})
	}
}

func TestPluginThemePayloadRespectsReducedMotion(t *testing.T) {
	originalReduceMotion := currentAppearanceReduceMotion
	currentAppearanceReduceMotion = func() bool { return true }
	t.Cleanup(func() { currentAppearanceReduceMotion = originalReduceMotion })

	motion, ok := currentPluginThemePayload(false, false, false, true)["motion"].(map[string]any)
	if !ok {
		t.Fatal("theme motion has an unexpected type")
	}
	if got := motion["duration"]; got != float32(0) {
		t.Fatalf("reduced-motion theme duration = %v, want 0", got)
	}
}

func TestPluginThemePayloadUsesActivePreset(t *testing.T) {
	payload := currentPluginThemePayload(false, false, false, false, appearancePresetJAgent)
	if payload["mode"] != "dark" {
		t.Fatalf("J Agent mode = %v, want dark", payload["mode"])
	}
	colors, ok := payload["colors"].(map[string]string)
	if !ok {
		t.Fatalf("theme colors have type %T", payload["colors"])
	}
	for name, want := range map[string]string{
		"background":   "#111315",
		"surfaceHover": "#FFFFFF12",
		"border":       "#FFFFFF1A",
		"successText":  "#A4D6A3",
	} {
		if got := colors[name]; got != want {
			t.Errorf("J Agent color %s = %q, want %q", name, got, want)
		}
	}
	fontSize, ok := payload["fontSize"].(map[string]float32)
	if !ok {
		t.Fatalf("theme font sizes have type %T", payload["fontSize"])
	}
	if fontSize["body"] != 15 || fontSize["code"] != 13 || fontSize["caption"] != 12 {
		t.Fatalf("J Agent font sizes = %#v", fontSize)
	}
	radius, ok := payload["radius"].(map[string]float32)
	if !ok {
		t.Fatalf("theme radii have type %T", payload["radius"])
	}
	if radius["input"] != 10 {
		t.Fatalf("J Agent input radius = %v, want 10", radius["input"])
	}
	motion, ok := payload["motion"].(map[string]any)
	if !ok {
		t.Fatalf("theme motion has type %T", payload["motion"])
	}
	if motion["duration"] != float32(125) {
		t.Fatalf("J Agent motion duration = %v, want 125", motion["duration"])
	}
}

func TestPluginHostThemeUsesSynchronizedReducedMotion(t *testing.T) {
	state := &appState{
		preferences: defaultByopPreferences(),
		plugins:     &pluginHost{},
	}
	state.preferences.Motion = string(motionReduced)
	state.syncPluginWebViewTheme(false, false, false)

	entry := pluginRegistryEntry{Manifest: &pluginManifest{}}
	result := state.plugins.handleHostCall(entry, "host.getTheme", json.RawMessage(`{}`))
	if result.Err != nil {
		t.Fatalf("host.getTheme failed: %s", result.Err.Message)
	}
	payload, ok := result.Value.(map[string]any)
	if !ok {
		t.Fatalf("host.getTheme payload has type %T", result.Value)
	}
	motion, ok := payload["motion"].(map[string]any)
	if !ok {
		t.Fatalf("theme motion has type %T", payload["motion"])
	}
	if got := motion["duration"]; got != float32(0) {
		t.Fatalf("host.getTheme reduced-motion duration = %v, want 0", got)
	}
}

func TestPluginHostThemeUsesSynchronizedPreset(t *testing.T) {
	state := &appState{
		preferences: defaultByopPreferences(),
		plugins:     &pluginHost{},
	}
	state.preferences.AppearancePreset = string(appearancePresetJAgent)
	state.syncPluginWebViewTheme(false, false, false)

	entry := pluginRegistryEntry{Manifest: &pluginManifest{}}
	result := state.plugins.handleHostCall(entry, "host.getTheme", json.RawMessage(`{}`))
	if result.Err != nil {
		t.Fatalf("host.getTheme failed: %s", result.Err.Message)
	}
	payload, ok := result.Value.(map[string]any)
	if !ok {
		t.Fatalf("host.getTheme payload has type %T", result.Value)
	}
	if payload["mode"] != "dark" {
		t.Fatalf("host.getTheme mode = %v, want J Agent dark", payload["mode"])
	}
	colors := payload["colors"].(map[string]string)
	if colors["background"] != "#111315" {
		t.Fatalf("host.getTheme background = %q, want J Agent", colors["background"])
	}
}

func TestPluginWebViewBridgeScriptUsesPreset(t *testing.T) {
	script := pluginWebViewBridgeScript(false, false, false, false, "en", appearancePresetJAgent)
	if !strings.Contains(script, `"mode":"dark"`) ||
		!strings.Contains(script, `"background":"#111315"`) ||
		!strings.Contains(script, `"body":15`) {
		t.Fatal("preload bridge did not embed the active J Agent theme")
	}
}

func TestPluginWebViewReduceTransparencyKeepsRootBackground(t *testing.T) {
	script := pluginWebViewBridgeScript(false, false, true, false, "en")
	if strings.Contains(script, `root.style.setProperty("--byop-background", colors.surface)`) {
		t.Fatal("reduce transparency replaced the WebView root background with the surface color")
	}
}

func TestPluginWebViewBridgeMapsSemanticThemeColors(t *testing.T) {
	script := pluginWebViewBridgeScript(false, false, false, false, "en")
	for _, variable := range []string{
		"--byop-on-accent",
		"--byop-success-text",
		"--byop-warning-text",
		"--byop-danger-text",
		"--byop-focus",
	} {
		if !strings.Contains(script, variable) {
			t.Errorf("preload bridge does not map %s", variable)
		}
	}
}
