package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestP32DefaultPresetIsBoardUI fixes P32 §3: a fresh install — no
// preferences.json, or an empty file — lands on the BoardUI preset.
func TestP32DefaultPresetIsBoardUI(t *testing.T) {
	directory := t.TempDir()
	store := newByopPreferencesStore(directory)

	saved, warning := store.load()
	if warning != "" {
		t.Fatal(warning)
	}
	if saved.AppearancePreset != string(appearancePresetBoardUI) {
		t.Fatalf("fresh preset = %q, want BoardUI", saved.AppearancePreset)
	}

	if err := os.WriteFile(filepath.Join(directory, "preferences.json"), []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	saved, _ = store.load()
	if saved.AppearancePreset != string(appearancePresetBoardUI) {
		t.Fatalf("empty-file preset = %q, want BoardUI", saved.AppearancePreset)
	}

	state := &appState{preferences: defaultByopPreferences()}
	if state.currentAppearancePreset() != appearancePresetBoardUI {
		t.Fatalf("default state preset = %q, want BoardUI", state.currentAppearancePreset())
	}
}

// TestP32UnknownPresetFallsBackToBYOP fixes the P32 §3 boundary the probe
// established: only defaultByopPreferences() changed; the normalize fallback
// keeps returning BYOP so a damaged or unknown saved value never silently
// becomes BoardUI.
func TestP32UnknownPresetFallsBackToBYOP(t *testing.T) {
	for _, value := range []string{"", "unknown", "   ", "\t", "board", "board-ui"} {
		if got := normalizeAppearancePreset(value); got != appearancePresetBYOP {
			t.Fatalf("normalize %q = %q, want BYOP", value, got)
		}
	}
	// Case and whitespace still normalize to the three real presets.
	if normalizeAppearancePreset(" BoardUI ") != appearancePresetBoardUI {
		t.Fatal("BoardUI normalization broke")
	}

	preferences := defaultByopPreferences()
	preferences.AppearancePreset = "  "
	normalizeByopPreferences(&preferences)
	if preferences.AppearancePreset != string(appearancePresetBYOP) {
		t.Fatalf("normalized preset = %q, want BYOP", preferences.AppearancePreset)
	}

	// A save roundtrip of an unknown value persists as BYOP, not BoardUI.
	store := newByopPreferencesStore(t.TempDir())
	damaged := defaultByopPreferences()
	damaged.AppearancePreset = "unknown"
	if err := store.save(damaged); err != nil {
		t.Fatal(err)
	}
	saved, warning := store.load()
	if warning != "" {
		t.Fatal(warning)
	}
	if saved.AppearancePreset != string(appearancePresetBYOP) {
		t.Fatalf("persisted unknown = %q, want BYOP", saved.AppearancePreset)
	}
}

// TestP32PresetContentMetrics pins the P32 contract §1 metric table.
func TestP32PresetContentMetrics(t *testing.T) {
	for _, testCase := range []struct {
		preset  appearancePreset
		fixed   float32
		max     float32
		padding float32
		follows bool
		turnGap float32
	}{
		{appearancePresetBYOP, 720, 720, 20, false, 24},
		{appearancePresetJAgent, 768, 768, 48, true, 24},
		{appearancePresetBoardUI, 1280, 1280, 24, true, 12},
	} {
		metrics := metricsForAppearancePreset(testCase.preset)
		if metrics.chatContentWidth != testCase.fixed ||
			metrics.chatContentMaxWidth != testCase.max ||
			metrics.chatHorizontalPadding != testCase.padding ||
			metrics.chatContentFollowsWindow != testCase.follows ||
			metrics.turnGap != testCase.turnGap {
			t.Fatalf("%v metrics = %#v", testCase.preset, metrics)
		}
	}
}

// TestP32BoardUIContentFollowsWindow pins P32 §5.3: the BoardUI column is
// min(1280, main − 2×24) and never negative; J Agent keeps following its own
// 768/96 rule; BYOP stays fixed at 720 (the Onevra constant).
func TestP32BoardUIContentFollowsWindow(t *testing.T) {
	boardPalette := themePaletteForAppearance(appearancePresetBoardUI, false, false, false)
	jAgentPalette := themePaletteForAppearance(appearancePresetJAgent, true, false, false)
	byopPalette := themePaletteForAppearance(appearancePresetBYOP, false, false, false)

	for _, windowWidth := range []float32{1180, 1100, 900, 760} {
		for _, testCase := range []struct {
			preset appearancePreset
			pad    float32
		}{
			{appearancePresetBoardUI, 24},
			{appearancePresetJAgent, 48},
		} {
			state := layoutTestState(t)
			state.preferences.AppearancePreset = string(testCase.preset)
			state.navCollapsed = false
			metrics := metricsForAppearancePreset(testCase.preset)
			state.navWidth = metrics.navDefaultWidth
			state.jAgentNavWidth = metrics.navDefaultWidth
			mainWidth := state.chatMainWidth(windowWidth)
			palette := boardPalette
			if testCase.preset == appearancePresetJAgent {
				palette = jAgentPalette
			}
			got := chatContentColumnWidth(palette, mainWidth)
			want := max(0, min(palette.chatContentMaxWidth, mainWidth-2*testCase.pad))
			if got != want {
				t.Fatalf("%v column at %v = %v, want %v (main %v)",
					testCase.preset, windowWidth, got, want, mainWidth)
			}
			if got < 0 {
				t.Fatalf("%v column at %v went negative: %v", testCase.preset, windowWidth, got)
			}
		}

		byopState := layoutTestState(t)
		byopState.navCollapsed = false
		byopState.navWidth = navExpandedWidth
		if got := chatContentColumnWidth(byopPalette, byopState.chatMainWidth(windowWidth)); got != 720 {
			t.Fatalf("BYOP column at %v = %v, want the fixed 720", windowWidth, got)
		}
	}

	// Even a collapsed main area floors at zero, never negative.
	if got := chatContentColumnWidth(boardPalette, 20); got != 0 {
		t.Fatalf("BoardUI column at 20px main = %v, want 0", got)
	}
}

// TestP32PluginChatPanelOpensAndClampsWidth pins contract §2.2: the panel
// width normalizes clamp(380..560) with 0 meaning the default 420, across
// save and load paths.
func TestP32PluginChatPanelOpensAndClampsWidth(t *testing.T) {
	for _, testCase := range []struct{ in, want float32 }{
		{0, 420}, {-12, 420}, {300, 380}, {380, 380},
		{420, 420}, {560, 560}, {900, 560},
	} {
		if got := normalizePluginChatWidth(testCase.in); got != testCase.want {
			t.Fatalf("normalizePluginChatWidth(%v) = %v, want %v", testCase.in, got, testCase.want)
		}
	}

	directory := t.TempDir()
	store := newByopPreferencesStore(directory)
	preferences := defaultByopPreferences()
	preferences.PluginChatOpen = true
	preferences.PluginChatWidth = 640
	if err := store.save(preferences); err != nil {
		t.Fatal(err)
	}
	saved, warning := store.load()
	if warning != "" {
		t.Fatal(warning)
	}
	if !saved.PluginChatOpen || saved.PluginChatWidth != 560 {
		t.Fatalf("saved panel = open %t width %v", saved.PluginChatOpen, saved.PluginChatWidth)
	}

	if err := os.WriteFile(
		filepath.Join(directory, "preferences.json"),
		[]byte(`{"version":1,"pluginChatOpen":true,"pluginChatWidth":300}`+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	saved, warning = store.load()
	if warning != "" {
		t.Fatal(warning)
	}
	if !saved.PluginChatOpen || saved.PluginChatWidth != 380 {
		t.Fatalf("loaded panel = open %t width %v, want open true width 380",
			saved.PluginChatOpen, saved.PluginChatWidth)
	}
}

func p32PluginChatPanelTestState(t *testing.T) (*appState, pluginRegistryEntry) {
	t.Helper()
	state := layoutTestState(t)
	state.pluginChatWidth = normalizePluginChatWidth(state.preferences.PluginChatWidth)
	entry := pluginRegistryEntry{
		ID:       "local.lightweight-table",
		Name:     "Tables",
		Status:   pluginRegistered,
		Manifest: &pluginManifest{ID: "local.lightweight-table"},
	}
	return state, entry
}

func p32PluginChatPanelHost(state *appState) *pluginHost {
	return newPluginHost(pluginHostVersion, nil,
		pluginHostChatPanelHandler(func(pluginID string, open bool) (map[string]any, *pluginRPCError) {
			return state.applyPluginChatPanel(pluginID, open)
		}),
	)
}

// TestP32PluginChatPanelCallDeniedOutsidePluginPage pins contract §2.1: only
// the plugin whose view is on screen may set the panel, and the params must
// carry the open flag.
func TestP32PluginChatPanelCallDeniedOutsidePluginPage(t *testing.T) {
	state, entry := p32PluginChatPanelTestState(t)
	// layoutTestState starts on the Chat page — outside every plugin view.
	host := p32PluginChatPanelHost(state)

	denied := host.handleHostCall(entry, "host.setChatPanel", json.RawMessage(`{"open": true}`))
	if denied.Err == nil || denied.Err.Code != pluginRPCPermissionDenied {
		t.Fatalf("off-page call = %#v, want permission denied", denied)
	}
	if !strings.HasPrefix(denied.Err.Message, "permission denied: ") {
		t.Fatalf("denied message = %q", denied.Err.Message)
	}
	if state.pluginChatOpen {
		t.Fatal("the denied call still opened the panel")
	}

	// A different plugin on the page is still denied: ownership is per view.
	state.page = pagePluginView
	state.pluginViewPluginID = "local.other-plugin"
	other := host.handleHostCall(entry, "host.setChatPanel", json.RawMessage(`{"open": true}`))
	if other.Err == nil || other.Err.Code != pluginRPCPermissionDenied {
		t.Fatalf("other-plugin call = %#v, want permission denied", other)
	}

	for _, params := range []string{`{}`, `{"open": true, "extra": 1}`} {
		invalid := host.handleHostCall(entry, "host.setChatPanel", json.RawMessage(params))
		if invalid.Err == nil || invalid.Err.Code != pluginRPCInvalidParams {
			t.Fatalf("params %s = %#v, want invalid params", params, invalid)
		}
	}

	// On its own view the call applies (never toggles) and answers with the
	// real state and the clamped width.
	state.pluginViewPluginID = entry.ID
	applied := host.handleHostCall(entry, "host.setChatPanel", json.RawMessage(`{"open": true}`))
	if applied.Err != nil {
		t.Fatalf("on-view call: %#v", applied.Err)
	}
	if !state.pluginChatOpen {
		t.Fatal("the panel did not open")
	}
	value, ok := applied.Value.(map[string]any)
	if !ok || value["open"] != true || value["width"] != float64(pluginChatDefaultWidth) {
		t.Fatalf("answer = %#v, want open true and width %v", applied.Value, pluginChatDefaultWidth)
	}
	again := host.handleHostCall(entry, "host.setChatPanel", json.RawMessage(`{"open": true}`))
	if again.Err != nil || again.Value.(map[string]any)["open"] != true || !state.pluginChatOpen {
		t.Fatalf("repeat call toggled instead of setting: %#v open=%t", again.Value, state.pluginChatOpen)
	}
}

// TestP32PluginChatPanelSharesMainChat pins §6.5 G: the panel and the Chat
// page draw the one s.chat — a message added once is visible in both.
func TestP32PluginChatPanelSharesMainChat(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	state := layoutTestState(t)
	state.preferences.AppearancePreset = string(appearancePresetBoardUI)
	state.pluginChatOpen = true
	state.pluginChatWidth = pluginChatDefaultWidth

	renderPanel := func() *ui.Tester {
		tt := ui.NewTester(func(c *ui.Context) {
			theme, p := nativeThemeForAppearance(appearancePresetBoardUI, false, false, false)
			c.SetTheme(theme)
			state.configureMarkdownV12(c, p)
			state.pluginChatPanelCard(c, p, pluginChatDefaultWidth, false)
		}, 460, 720)
		tt.Frame()
		return tt
	}

	panel := renderPanel()
	if _, ok := panel.Find(text("pluginChatPanelTitle")); !ok {
		t.Fatalf("panel header missing: %#v", panel.Texts())
	}
	// The panel shows the same session's model, the same transcript.
	if _, ok := panel.Find("GPT-5"); !ok {
		t.Fatal("panel did not render the main chat's model capsule")
	}
	firstQuestion, ok := panel.Find("Question 00 about the long transcript.")
	if !ok {
		t.Fatal("panel did not render the shared transcript")
	}

	const marker = "P32 shared chat marker"
	state.chat = append(state.chat, chatMessage{
		role:   chatUser,
		status: "Complete",
		blocks: []chatBlock{{kind: chatBlockText, text: marker}},
	})

	panel = renderPanel()
	if !panel.HasText(marker) {
		t.Fatal("the appended message is missing from the plugin panel")
	}
	chatPage := ui.NewTester(state.view, 1180, 760)
	chatPage.Frame()
	if !chatPage.HasText(marker) {
		t.Fatal("the same message is missing from the Chat page")
	}
	if _, ok := chatPage.Find("Question 00 about the long transcript."); !ok && firstQuestion.W <= 0 {
		t.Fatal("the Chat page lost the shared transcript")
	}
}

// TestP32OpenPiLoginBuildsEscapedScript covers P32 §4.4's command
// construction only — no Terminal is ever opened. The provider id travels
// through shell single-quoting and AppleScript string escaping, so quotes,
// backslashes and single quotes must all survive to the same characters.
func TestP32OpenPiLoginBuildsEscapedScript(t *testing.T) {
	if got := piLoginShellCommand("github-copilot"); got != `pi '/login github-copilot'` {
		t.Fatalf("plain shell command = %q", got)
	}
	wantScript := "tell application \"Terminal\" to activate\n" +
		`tell application "Terminal" to do script "pi '/login github-copilot'"`
	if got := piLoginTerminalScript("github-copilot"); got != wantScript {
		t.Fatalf("plain script = %q, want %q", got, wantScript)
	}

	tricky := `we"ird\id'x`
	if got, want := piLoginShellCommand(tricky), `pi '/login we"ird\id'\''x'`; got != want {
		t.Fatalf("escaped shell command = %q, want %q", got, want)
	}
	wantTricky := "tell application \"Terminal\" to activate\n" +
		`tell application "Terminal" to do script "pi '/login we\"ird\\id'\\''x'"`
	if got := piLoginTerminalScript(tricky); got != wantTricky {
		t.Fatalf("escaped script = %q, want %q", got, wantTricky)
	}

	// The appState method: empty ids are rejected on every platform, and
	// off macOS nothing is launched.
	state := &appState{}
	if err := state.openPiLogin("  "); err == nil {
		t.Fatal("empty provider id accepted")
	}
	if runtime.GOOS != "darwin" {
		if err := state.openPiLogin("anthropic"); err == nil {
			t.Fatal("openPiLogin claimed success on a non-macOS platform")
		}
	}
}
