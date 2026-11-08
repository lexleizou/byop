package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestP32ChatFontFollowsPreset(t *testing.T) {
	cases := []struct {
		preset   appearancePreset
		font     float32
		line     float32
		codeFont float32
	}{
		{appearancePresetBYOP, 14, 1.5, 13},
		{appearancePresetJAgent, 15, 1.72, 13},
		{appearancePresetBoardUI, 15, 1.72, 14},
	}
	for _, want := range cases {
		p := themePaletteForAppearance(want.preset, false, false, false)
		if p.chatFont != want.font || p.chatLineHeight != want.line || p.chatCodeFont != want.codeFont {
			t.Fatalf("%s chat typography = %v/%v/%v, want %v/%v/%v",
				want.preset, p.chatFont, p.chatLineHeight, p.chatCodeFont,
				want.font, want.line, want.codeFont)
		}
	}
	// The superseded P29 constants stay for spec reference and must match the
	// J Agent / BoardUI large tier they used to feed.
	if p29FontBody != chatFontJAgent || p29LineHeight != chatLineJAgent {
		t.Fatalf("P29 constants drifted: %v/%v", p29FontBody, p29LineHeight)
	}
}

func TestP32ThemeFontSizeOverridesChatFont(t *testing.T) {
	body := 17
	overlay := &pluginThemeOverlay{
		resources: []pluginThemeResource{{
			entry:    pluginThemeContribution{ID: "theme.font", Title: "Large text"},
			pluginID: "example.theme",
			fontSize: pluginThemeFontSize{Body: &body},
			light:    pluginThemeModeColors{colors: map[string]ui.Color{}},
			dark:     pluginThemeModeColors{colors: map[string]ui.Color{}},
		}},
	}
	setTestPluginThemeSelection(t, overlay, pluginThemeSelection{
		PluginID:   "example.theme",
		ResourceID: "theme.font",
	})
	p := themePaletteForAppearance(appearancePresetBYOP, false, false, false)
	if p.chatFont != 17 {
		t.Fatalf("chatFont = %v, want the theme's 17", p.chatFont)
	}
	// code was not provided, so the preset default stays.
	if p.chatCodeFont != chatCodeBYOP {
		t.Fatalf("chatCodeFont = %v, want the BYOP default %v", p.chatCodeFont, chatCodeBYOP)
	}
	if p.chatLineHeight != chatLineBYOP {
		t.Fatalf("chatLineHeight = %v, want the preset default %v", p.chatLineHeight, chatLineBYOP)
	}
}

func TestP32ThemeFontSizeRejectsOutOfRange(t *testing.T) {
	directory := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(directory, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("low.json", `{"fontSize": {"body": 10, "code": 13}}`)
	write("high.json", `{"fontSize": {"body": 25}}`)
	write("valid.json", `{"fontSize": {"body": 17, "code": 14}}`)

	_, lowIssues := readPluginThemeResource(directory, "low.json")
	if len(lowIssues) == 0 {
		t.Fatal("body 10 reported no issue")
	}
	theme, highIssues := readPluginThemeResource(directory, "high.json")
	if len(highIssues) == 0 {
		t.Fatal("body 25 reported no issue")
	}
	if theme.FontSize.Body != nil {
		t.Fatal("the rejected body size must be dropped, not silently accepted")
	}
	if theme.ValidFontSize {
		t.Fatal("an invalid fontSize must not report as valid")
	}
	valid, validIssues := readPluginThemeResource(directory, "valid.json")
	if len(validIssues) != 0 {
		t.Fatalf("valid fontSize reported issues: %v", validIssues)
	}
	if valid.FontSize.Body == nil || *valid.FontSize.Body != 17 || valid.FontSize.Code == nil || *valid.FontSize.Code != 14 {
		t.Fatalf("valid fontSize parsed = %+v", valid.FontSize)
	}
	if !valid.ValidFontSize {
		t.Fatal("a valid fontSize must report as valid")
	}
}

func TestP32OtherUIFontsUnchanged(t *testing.T) {
	before := themePaletteForAppearance(appearancePresetBYOP, false, false, false)
	body := 20
	overlay := &pluginThemeOverlay{
		resources: []pluginThemeResource{{
			entry:    pluginThemeContribution{ID: "theme.font", Title: "Very large text"},
			pluginID: "example.theme",
			fontSize: pluginThemeFontSize{Body: &body},
			light:    pluginThemeModeColors{colors: map[string]ui.Color{}},
			dark:     pluginThemeModeColors{colors: map[string]ui.Color{}},
		}},
	}
	setTestPluginThemeSelection(t, overlay, pluginThemeSelection{
		PluginID:   "example.theme",
		ResourceID: "theme.font",
	})
	after := themePaletteForAppearance(appearancePresetBYOP, false, false, false)
	for _, field := range []struct {
		name          string
		before, after float32
	}{
		{"bodyFont", before.bodyFont, after.bodyFont},
		{"captionFont", before.captionFont, after.captionFont},
		{"titleFont", before.titleFont, after.titleFont},
		{"navFont", before.navFont, after.navFont},
		{"codeFont", before.codeFont, after.codeFont},
	} {
		if field.before != field.after {
			t.Fatalf("%s changed from %v to %v; the P32 fontSize override must stay\n"+
				"scoped to the conversation", field.name, field.before, field.after)
		}
	}
	if after.chatFont != 20 {
		t.Fatalf("chatFont = %v, want the theme override 20", after.chatFont)
	}
}

// TestP32PiLoginProviderIDRejectsShellHostileInput covers the review finding
// that the provider id reaches both a shell command and an AppleScript string
// literal: only Pi's own id alphabet may pass, so a newline can never split the
// AppleScript statement and no other character survives either layer.
func TestP32PiLoginProviderIDRejectsShellHostileInput(t *testing.T) {
	accepted := []string{
		"github-copilot", "openai-codex", "xai", "my.provider_2", "a", strings.Repeat("a", 64),
	}
	for _, value := range accepted {
		if !validPiLoginProviderID(value) {
			t.Fatalf("validPiLoginProviderID(%q) = false, want true", value)
		}
	}
	rejected := []string{
		"", " ", "x\nrm -rf ~", "a;b", "a b", "a$(id)", "a`id`", "a'b", `a"b`, "a\\b",
		"中文", strings.Repeat("a", 65),
	}
	for _, value := range rejected {
		if validPiLoginProviderID(value) {
			t.Fatalf("validPiLoginProviderID(%q) = true, want false", value)
		}
	}
	// The escaping helpers stay correct for the ids that do pass.
	if got := piLoginShellCommand("openai-codex"); got != "pi '/login openai-codex'" {
		t.Fatalf("shell command = %q", got)
	}
	script := piLoginTerminalScript("openai-codex")
	if strings.ContainsAny(script, "\n") != true {
		t.Fatalf("script lost its statement separator: %q", script)
	}
	if strings.Count(script, "do script") != 1 {
		t.Fatalf("script must contain exactly one do script: %q", script)
	}
}
