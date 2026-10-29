package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"byop/locales"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestP16AppearancePresetNormalization(t *testing.T) {
	preferences := defaultByopPreferences()
	if got := normalizeAppearancePreset(preferences.AppearancePreset); got != appearancePresetBYOP {
		t.Fatalf("default preset = %q, want BYOP", got)
	}
	for _, value := range []string{"", "unknown", "  "} {
		if got := normalizeAppearancePreset(value); got != appearancePresetBYOP {
			t.Fatalf("normalize %q = %q, want BYOP", value, got)
		}
	}
	if got := normalizeAppearancePreset(" JAGENT "); got != appearancePresetJAgent {
		t.Fatalf("J Agent normalization = %q", got)
	}

	preferences.AppearancePreset = "unknown"
	normalizeByopPreferences(&preferences)
	if preferences.AppearancePreset != string(appearancePresetBYOP) {
		t.Fatalf("unknown persisted preset = %q", preferences.AppearancePreset)
	}
}

func TestP16PresetPersistenceAndBYOPModeRestoration(t *testing.T) {
	oldSource := setMygoThemeSource
	t.Cleanup(func() { setMygoThemeSource = oldSource })
	var sources []mygo.ThemeSource
	setMygoThemeSource = func(source mygo.ThemeSource) {
		sources = append(sources, source)
	}

	configDir := t.TempDir()
	store := newByopPreferencesStore(configDir)
	preferences := defaultByopPreferences()
	preferences.Theme = string(themeLight)
	preferences.NavExpandedWidth = 173
	preferences.JAgentNavExpandedWidth = 275
	if err := store.save(preferences); err != nil {
		t.Fatal(err)
	}
	state := &appState{
		themeMode:          themeLight,
		preferences:        preferences,
		preferencesStore:   store,
		navWidth:           173,
		jAgentNavWidth:     275,
		preferencesWarning: "",
	}

	state.setAppearancePreset(appearancePresetJAgent)
	if state.currentAppearancePreset() != appearancePresetJAgent {
		t.Fatalf("preset = %q, want J Agent", state.currentAppearancePreset())
	}
	if state.themeMode != themeLight {
		t.Fatalf("saved BYOP theme changed to %q", state.themeMode)
	}
	if len(sources) == 0 || sources[len(sources)-1] != mygo.ThemeDark {
		t.Fatalf("J Agent theme sources = %v, want effective dark", sources)
	}
	saved, warning := store.load()
	if warning != "" {
		t.Fatal(warning)
	}
	if saved.AppearancePreset != string(appearancePresetJAgent) ||
		saved.Theme != string(themeLight) ||
		saved.NavExpandedWidth != 173 ||
		saved.JAgentNavExpandedWidth != 275 {
		t.Fatalf("saved J Agent preferences = %#v", saved)
	}

	state.setAppearancePreset(appearancePresetBYOP)
	if len(sources) == 0 || sources[len(sources)-1] != mygo.ThemeLight {
		t.Fatalf("restored theme sources = %v, want saved Light", sources)
	}
	saved, warning = store.load()
	if warning != "" {
		t.Fatal(warning)
	}
	if saved.AppearancePreset != string(appearancePresetBYOP) ||
		saved.NavExpandedWidth != 173 ||
		saved.JAgentNavExpandedWidth != 275 {
		t.Fatalf("BYOP return lost isolated values: %#v", saved)
	}
}

func TestP16PresetSaveFailureRollsBackEffectiveAppearance(t *testing.T) {
	oldSource := setMygoThemeSource
	t.Cleanup(func() { setMygoThemeSource = oldSource })
	var sources []mygo.ThemeSource
	setMygoThemeSource = func(source mygo.ThemeSource) {
		sources = append(sources, source)
	}

	configDir := t.TempDir()
	blockedPath := filepath.Join(configDir, "preferences.json")
	if err := os.Mkdir(blockedPath, 0o700); err != nil {
		t.Fatal(err)
	}
	preferences := defaultByopPreferences()
	preferences.Theme = string(themeLight)
	state := &appState{
		themeMode:        themeLight,
		preferences:      preferences,
		preferencesStore: &byopPreferencesStore{path: blockedPath},
	}

	state.setAppearancePreset(appearancePresetJAgent)
	if state.currentAppearancePreset() != appearancePresetBYOP {
		t.Fatalf("failed save left preset = %q, want BYOP", state.currentAppearancePreset())
	}
	if len(sources) == 0 || sources[len(sources)-1] != mygo.ThemeLight {
		t.Fatalf("rollback sources = %v, want saved Light", sources)
	}
	if state.notice != text("noticePreferenceSaveAppearance") {
		t.Fatalf("rollback notice = %q", state.notice)
	}
}

func TestP16JAgentMetricsAndOpaqueFallback(t *testing.T) {
	metrics := metricsForAppearancePreset(appearancePresetJAgent)
	if metrics.bodyFont != 15 || metrics.codeFont != 13 || metrics.uiFont != 13 ||
		metrics.navItemHeight != 28 || metrics.navDefaultWidth != 261 ||
		metrics.chatContentWidth != 768 || metrics.chatHorizontalPadding != 48 ||
		metrics.composerMinHeight != 92 || metrics.transitionMS != 125 {
		t.Fatalf("J Agent metrics = %#v", metrics)
	}

	theme, p := nativeThemeForAppearance(appearancePresetJAgent, false, false, false)
	if !p.dark || !theme.Dark || theme.FontSize != 13 {
		t.Fatalf("J Agent native theme = %#v, dark=%t", theme, p.dark)
	}
	if p.glassTint.A < 170 || p.glassTint.A > 185 {
		t.Fatalf("J Agent nav tint alpha = %d, want translucent .70", p.glassTint.A)
	}
	if p.composerSurface.A < 175 || p.composerSurface.A > 190 {
		t.Fatalf("J Agent composer alpha = %d, want translucent .72", p.composerSurface.A)
	}

	_, opaque := nativeThemeForAppearance(appearancePresetJAgent, true, false, true)
	if opaque.composerSurface.A != 255 || opaque.raisedSurface.A != 255 {
		t.Fatalf("reduced transparency fallback is not opaque: %#v", opaque)
	}
	if fontBody != 14 || runtimeTokens.Font.Body.Value != 14 {
		t.Fatal("J Agent metrics changed the global BYOP font constants")
	}
}

func TestP16WallpaperChoiceIsIsolatedByPreset(t *testing.T) {
	if wallpaperBitmap("jagent-misty-mountains") == nil {
		t.Fatal("embedded J Agent wallpaper did not decode")
	}
	preferences := defaultByopPreferences()
	preferences.Wallpaper = "ember"
	preferences.JAgentWallpaper = "jagent-misty-mountains"
	preferences.AppearancePreset = string(appearancePresetJAgent)
	state := &appState{preferences: preferences}

	if got := state.activeWallpaper(); got != "jagent-misty-mountains" {
		t.Fatalf("J Agent wallpaper = %q", got)
	}
	state.setWallpaper("none")
	if state.preferences.JAgentWallpaper != "none" || state.preferences.Wallpaper != "ember" {
		t.Fatalf("J wallpaper edit leaked into BYOP: %#v", state.preferences)
	}

	state.preferences.AppearancePreset = string(appearancePresetBYOP)
	if got := state.activeWallpaper(); got != "ember" {
		t.Fatalf("BYOP wallpaper after J edit = %q", got)
	}
	state.setWallpaper("jade")
	state.preferences.AppearancePreset = string(appearancePresetJAgent)
	if got := state.activeWallpaper(); got != "none" {
		t.Fatalf("BYOP wallpaper edit leaked into J Agent: %q", got)
	}
}

func TestP16SettingsSwitchToJAgentShowsActualPresetControls(t *testing.T) {
	oldSource := setMygoThemeSource
	t.Cleanup(func() { setMygoThemeSource = oldSource })
	setMygoThemeSource = func(mygo.ThemeSource) {}

	state, page := settingsPageV12TestState(t)
	state.preferences.AppearancePreset = string(appearancePresetBYOP)
	state.preferences.Theme = string(themeLight)
	state.themeMode = themeLight
	page.ActiveCategory = settingsCategoryAppearance
	render := func() *ui.Tester {
		return ui.NewTester(func(c *ui.Context) {
			theme, palette := nativeThemeForAppearance(
				state.currentAppearancePreset(), state.effectiveDark(), false, false,
			)
			c.SetTheme(theme)
			state.settingsPageV12(c, palette, page)
		}, 1100, 900)
	}
	tt := render()
	if err := tt.Click(text("settingsAppearancePresetJAgent")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if state.currentAppearancePreset() != appearancePresetJAgent {
		t.Fatalf("preset = %q, want J Agent", state.currentAppearancePreset())
	}
	for _, label := range []string{
		text("settingsAppearancePresetTitle"),
		text("settingsAppearancePresetJAgent"),
		text("settingsCategoryAppearanceJAgentDescription"),
		text("settingsWallpaperJAgent"),
		text("settingsGlassJAgentFixed"),
		text("settingsThemeDescriptionJAgent"),
		text("settingsThemeEffectiveDark"),
	} {
		if _, ok := tt.Find(label); !ok {
			t.Fatalf("J Agent settings did not render %q", label)
		}
	}
	for _, label := range []string{
		text("settingsNavTransparencyTitle"),
		text("settingsGlassBlurTitle"),
		text("settingsGlassRefractionTitle"),
		locales.SettingsAppearanceLight(),
		locales.SettingsAppearanceDark(),
		locales.SettingsAppearanceSystem(),
	} {
		if _, ok := tt.Find(label); ok {
			t.Fatalf("J Agent settings rendered BYOP-only control %q", label)
		}
	}
	if state.themeMode != themeLight || state.preferences.Theme != string(themeLight) {
		t.Fatalf("J Agent settings changed saved BYOP theme: mode=%q preference=%q",
			state.themeMode, state.preferences.Theme)
	}
	if err := tt.Click(text("settingsAppearancePresetBYOP")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, label := range []string{
		locales.SettingsAppearanceLight(),
		locales.SettingsAppearanceDark(),
		locales.SettingsAppearanceSystem(),
	} {
		if _, ok := tt.Find(label); !ok {
			t.Fatalf("BYOP return did not restore theme choice %q", label)
		}
	}
}

func TestP16JAgentShellKeepsRuntimeControlsAndTableContext(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()
	for _, size := range []struct {
		width, height int
	}{
		{width: 1180, height: 760},
		{width: 760, height: 600},
	} {
		state := layoutTestState(t)
		state.preferences.AppearancePreset = string(appearancePresetJAgent)
		state.preferences.JAgentWallpaper = "jagent-misty-mountains"
		state.composerTableContext = &tableContextAttachment{
			ContextID: "ctx_123",
			Label:     "Selected 3 rows",
		}
		state.run = runStreaming
		state.composerDraft = "Keep this response focused."
		state.queueSteeringCount = 1
		state.queueSteering = []string{"Keep this response focused."}
		tt := ui.NewTester(state.view, size.width, size.height)
		tt.Advance(500 * time.Millisecond)

		for _, label := range []string{
			text("chatMessageLabel"),
			text("chatSend"),
			text("navSettings"),
			text("queueTitle"),
			"Selected 3 rows",
		} {
			if rect, ok := tt.Find(label); !ok || rect.H <= 0 {
				t.Fatalf("%dx%d J Agent shell did not render %q: %#v",
					size.width, size.height, label, rect)
			}
		}
		input, _ := tt.Find(text("chatMessageLabel"))
		navWidth := state.navigationWidth(float32(size.width))
		if input.X <= navWidth || input.X+input.W > float32(size.width) {
			t.Fatalf("%dx%d J Agent composer escaped the main stage: nav=%v input=%#v",
				size.width, size.height, navWidth, input)
		}
	}
}

func TestP16ComposerHeightIncludesQueueAndHUD(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()
	state := layoutTestState(t)
	state.preferences.AppearancePreset = string(appearancePresetJAgent)
	state.run = runStreaming
	state.composerDraft = "Keep this response focused."
	state.queueSteeringCount = 1
	state.queueSteering = []string{"Keep this response focused."}
	state.hud.contextWindowFallback = 200_000
	state.hud.stats = &piSessionStats{
		Tokens: piSessionTokens{Input: 12_000, Output: 3_000},
	}

	var stackHeight float32
	ui.NewTester(func(c *ui.Context) {
		theme, palette := nativeThemeForAppearance(appearancePresetJAgent, true, false, false)
		c.SetTheme(theme)
		stack := state.jAgentChatComposer(c, palette)
		stack.Draw(func(_ *ui.Painter, rect ui.Rect) {
			stackHeight = rect.H
		})
	}, int(metricsForAppearancePreset(appearancePresetJAgent).chatContentWidth), 400)
	if stackHeight <= state.visualMetrics().composerMinHeight {
		t.Fatalf("composer stack height = %v, want queue/HUD beyond the editor minimum", stackHeight)
	}

	tt := renderP16JAgentChat(t, state, 900, 500)
	tt.Advance(500 * time.Millisecond)
	assertInsideViewport(t, tt, "pi-hud-row", 500)
	if state.chatView.composerHeight < stackHeight {
		t.Fatalf("reserved composer height = %v, stack = %v",
			state.chatView.composerHeight, stackHeight)
	}
}

func TestP16LiveWorkSummaryHonorsCollapse(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()
	const marker = "LIVE_STEP_MARKER"
	state := p16JAgentChatState(t)
	state.run = runStreaming
	state.chat = []chatMessage{{
		role:   chatAssistant,
		status: "streaming",
		blocks: []chatBlock{{kind: chatBlockText, text: marker}},
	}}
	tt := renderP16JAgentChat(t, state, 760, 500)
	label := textArgs("chatWorkingFor", map[string]any{"duration": "0s"})
	if _, ok := tt.Find(label); !ok {
		t.Fatalf("live work summary %q not rendered; texts=%v", label, tt.Texts())
	}
	if !tt.HasText(marker) {
		t.Fatal("live step was not rendered before collapse")
	}
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText(marker) {
		t.Fatal("live step remained visible after explicit collapse")
	}
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText(marker) {
		t.Fatal("live step did not return after explicit expansion")
	}
}

func TestP16ExpandingWorkSummaryPreservesReadingPosition(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()
	state := p16JAgentChatState(t)
	state.chat = []chatMessage{
		{
			role:   chatUser,
			status: "complete",
			blocks: []chatBlock{{kind: chatBlockText, text: "Explain the long result."}},
		},
		{
			role:   chatAssistant,
			status: "complete",
			blocks: []chatBlock{
				{kind: chatBlockText, text: strings.Repeat("Long intermediate detail.\n\n", 100)},
				{kind: chatBlockText, text: "Final answer marker."},
			},
		},
	}
	tt := renderP16JAgentChat(t, state, 700, 500)
	label := textArgs("chatWorkedFor", map[string]any{"duration": "0s"})
	before, ok := tt.Find(label)
	if !ok {
		t.Fatalf("completed work summary %q not rendered; texts=%v", label, tt.Texts())
	}
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	after, ok := tt.Find(label)
	if !ok {
		t.Fatal("expanded work summary disappeared")
	}
	if delta := after.Y - before.Y; delta < -1 || delta > 1 {
		t.Fatalf("work summary moved from y=%v to y=%v during expansion", before.Y, after.Y)
	}
	if !state.chatView.jAgentPaused || state.chatView.conversation.AtLatest() {
		t.Fatalf("expansion did not pause following: paused=%t scroll=%+v",
			state.chatView.jAgentPaused, state.chatView.conversation.Scroll)
	}
	if err := tt.Click(text("chatBackToLatest")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if state.chatView.jAgentPaused || !state.chatView.conversation.AtLatest() {
		t.Fatalf("jump-to-latest did not resume following: paused=%t scroll=%+v",
			state.chatView.jAgentPaused, state.chatView.conversation.Scroll)
	}
}

func p16JAgentChatState(t *testing.T) *appState {
	t.Helper()
	state := layoutTestState(t)
	state.page = pageChat
	state.preferences.AppearancePreset = string(appearancePresetJAgent)
	state.preferences.FollowUpStyle = string(followUpStyleGhost)
	state.followUpSuggestion = ""
	state.chatView = chatPresentation{}
	state.composerDraft = ""
	return state
}

func renderP16JAgentChat(t *testing.T, state *appState, width, height int) *ui.Tester {
	t.Helper()
	return ui.NewTester(func(c *ui.Context) {
		theme, palette := nativeThemeForAppearance(appearancePresetJAgent, true, false, false)
		c.SetTheme(theme)
		state.jAgentChatPage(c, palette)
	}, width, height)
}
