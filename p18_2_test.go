package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"byop/locales"

	"github.com/egoist/mygo/ui"
)

func p18_2HUDState(t *testing.T) *appState {
	t.Helper()
	state := layoutTestState(t)
	state.piModelProvider = "custom"
	state.piModelID = "deepseek-v4.1-flash"
	state.piModelName = "DeepSeek V4.1 Flash"
	used, percent := int64(282_000), 45.4
	state.hud.autoCompact = true
	state.hud.contextWindowFallback = 1_000_000
	state.hud.stats = &piSessionStats{
		Tokens: piSessionTokens{
			Input:      12_000,
			Output:     3_000,
			CacheRead:  400,
			CacheWrite: 100,
		},
		Cost: 0.012,
		ContextUsage: &piContextUsage{
			Tokens:        &used,
			ContextWindow: 1_000_000,
			Percent:       &percent,
		},
	}
	state.hud.currentTPS = 51.2
	state.hud.currentTPSKnown = true
	state.hud.currentTPSUpdatedAt = time.Now()
	state.hud.avgOutputTokens = 100
	state.hud.avgGenerationSeconds = 2
	state.hud.extStatus = map[string]string{"ov": "OV ✓"}
	state.chat = []chatMessage{
		{
			role:   chatUser,
			status: "Complete",
			blocks: []chatBlock{{kind: chatBlockText, text: "Explain the HUD."}},
		},
		{
			role:         chatAssistant,
			status:       "Complete",
			outputTokens: 152,
			thinking:     "Check the final layout.",
			blocks:       []chatBlock{{kind: chatBlockText, text: "Final answer marker."}},
		},
	}
	return state
}

func TestP18_3HUDSplitsTranscriptAndComposerFooterItems(t *testing.T) {
	state := p18_2HUDState(t)

	message := &state.chat[1]
	transcript := state.piHUDTurnItems(message)
	if len(transcript) != 3 {
		t.Fatalf("turn HUD item count = %d, want model/tokens/TPS", len(transcript))
	}
	for index, kind := range []hudItemKind{
		hudItemModel, hudItemTurnTokens, hudItemTPS,
	} {
		if transcript[index].kind != kind {
			t.Fatalf("transcript item %d kind = %d, want %d", index, transcript[index].kind, kind)
		}
	}

	footer := state.piHUDFooterItems()
	if len(footer) == 0 || footer[0].kind != hudItemContextCapacity {
		t.Fatalf("footer did not start with context capacity: %#v", footer)
	}
	for _, item := range footer {
		if item.kind == hudItemModel || item.kind == hudItemTurnTokens ||
			item.kind == hudItemTPS {
			t.Fatalf("primary HUD item leaked into composer footer: %#v", item)
		}
	}
	left, right := splitHUDItems(footer)
	if len(left) < 6 || len(right) != 1 {
		t.Fatalf("footer grouping left=%#v right=%#v", left, right)
	}
	if left[0].kind != hudItemContextCapacity ||
		left[0].value != piHUDContextSummary(state.hud.stats, state.hud.contextWindowFallback, state.hud.autoCompact) ||
		left[1].value != "↑12k ↓3.0k" ||
		left[2].label != "Cache hit" ||
		left[3].value != textArgs("hudAverageSpeed", map[string]any{"speed": "50.0"}) ||
		left[4].value != "main" ||
		left[5].value != "$0.012" ||
		right[0].value != "OV ✓" {
		t.Fatalf("footer left order or right status = left:%#v right:%#v", left, right)
	}
	footerLabel := state.piHUDFooterLabel()
	for _, unwanted := range []string{"DeepSeek V4.1 Flash", "↓152", "51.2 t/s"} {
		if strings.Contains(footerLabel, unwanted) {
			t.Fatalf("footer label contains primary item %q: %q", unwanted, footerLabel)
		}
	}
	for _, wanted := range []string{
		"1M", "↑12k", "↓3.0k", "Cache hit", "Avg 50.0 t/s", "$0.012", "OV ✓",
	} {
		if !strings.Contains(footerLabel, wanted) {
			t.Fatalf("footer label %q does not contain %q", footerLabel, wanted)
		}
	}
	if got := locales.In("zh-Hans").HudCache("24"); got != "缓存命中 24%" {
		t.Fatalf("Chinese cache label = %q, want 缓存命中 24%%", got)
	}
}

func TestP18_2TurnOutputTokensAttachToFirstAssistantOfPrompt(t *testing.T) {
	state := &appState{chat: []chatMessage{
		{role: chatUser},
		{role: chatAssistant},
		{role: chatAssistant},
	}}
	state.addPiTurnOutputTokens(120)
	state.addPiTurnOutputTokens(32)
	if got := state.chat[1].outputTokens; got != 152 {
		t.Fatalf("first assistant output tokens = %d, want 152", got)
	}
	if got := state.chat[2].outputTokens; got != 0 {
		t.Fatalf("later assistant inherited turn output tokens: %d", got)
	}
}

func TestP18_3LiveTurnOutputTokensCombineExactAndNewEstimate(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	state := p18_2HUDState(t)
	message := &state.chat[1]
	message.outputTokens = 0
	state.run = runStreaming
	state.addPiTurnOutputTokens(40)
	if got := message.outputTokens; got != 40 {
		t.Fatalf("first exact output tokens = %d, want 40", got)
	}
	if got := state.hud.liveTurnOutputTokens; got != 0 {
		t.Fatalf("first exact left estimate %.1f, want 0", got)
	}
	if got := p18_2TurnTokenValue(state.piHUDTurnItems(message)); got != "↓40" {
		t.Fatalf("running HUD after first exact = %q, want ↓40", got)
	}

	start := time.Unix(100, 0)
	state.observePiHUDSpeedAt("abcd", start)
	state.observePiHUDSpeedAt("efghijkl", start.Add(time.Second))

	if got := state.hud.liveTurnOutputTokens; got != 3 {
		t.Fatalf("live turn output estimate = %.1f, want 3", got)
	}
	liveItems := state.piHUDTurnItems(message)
	if got := p18_2TurnTokenValue(liveItems); got != "↓43" {
		t.Fatalf("live turn HUD tokens = %q, want ↓43", got)
	}

	state.addPiTurnOutputTokens(7)
	if got := message.outputTokens; got != 47 {
		t.Fatalf("cumulative exact output tokens = %d, want 47", got)
	}
	if got := state.hud.liveTurnOutputTokens; got != 0 {
		t.Fatalf("second exact left estimate %.1f, want 0", got)
	}
	if got := p18_2TurnTokenValue(state.piHUDTurnItems(message)); got != "↓47" {
		t.Fatalf("running HUD after second exact = %q, want ↓47", got)
	}

	state.run = runCompleted
	if got := p18_2TurnTokenValue(state.piHUDTurnItems(message)); got != "↓47" {
		t.Fatalf("completed turn HUD tokens = %q, want exact ↓47", got)
	}

	theme, _ := nativeThemeWithContrast(false, false)
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme)
		state.piHUDTurn(c, message)
	}, 720, 40)
	if _, ok := tt.Find("↓47"); !ok {
		t.Fatalf("completed HUD did not render exact tokens: %#v", tt.Texts())
	}
	if _, ok := tt.Find("↓43"); ok {
		t.Fatalf("completed HUD retained the live estimate: %#v", tt.Texts())
	}
}

func p18_2TurnTokenValue(items []hudItem) string {
	for _, item := range items {
		if item.kind == hudItemTurnTokens {
			return item.value
		}
	}
	return ""
}

func TestP18_3TranscriptHUDRendersOnceWithBrandTokensTPSAndFooter(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	state := p18_2HUDState(t)
	tt := ui.NewTester(state.view, 1180, 760)
	turnLabel := state.piHUDTurnLabel(&state.chat[1])

	count := 0
	for _, value := range tt.Texts() {
		if value == turnLabel {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("transcript HUD rendered %d times, want exactly once; want=%q texts=%#v",
			count, turnLabel, tt.Texts())
	}
	if rect, ok := tt.Find("conversation-hud-model-logo"); !ok || rect.W <= 0 || rect.H <= 0 {
		t.Fatalf("model logo did not render: %#v", rect)
	}
	if rect, ok := tt.Find("DeepSeek V4.1 Flash"); !ok || rect.W <= 0 || rect.H <= 0 {
		t.Fatalf("model name did not render: %#v", rect)
	}
	if rect, ok := tt.Find("↓152"); !ok || rect.W <= 0 || rect.H <= 0 {
		t.Fatalf("turn output tokens did not render: %#v", rect)
	}
	for _, unwanted := range []string{"上下文", "Context", "45.4%", "282K / 1M"} {
		if strings.Contains(turnLabel, unwanted) {
			t.Fatalf("transcript HUD label %q contains footer context %q", turnLabel, unwanted)
		}
	}
	if _, ok := tt.Find(textArgs("hudTokensPerSecond", map[string]any{"speed": "51.2"})); !ok {
		t.Fatal("transcript TPS did not render")
	}
	if _, ok := tt.Find(piHUDContextSummary(state.hud.stats, state.hud.contextWindowFallback, state.hud.autoCompact)); !ok {
		t.Fatal("composer footer full context usage disappeared")
	}
	if _, ok := tt.Find("pi-hud-row"); !ok {
		t.Fatal("composer secondary HUD row did not render")
	}
	if _, ok := tt.Find(state.piHUDFooterLabel()); !ok {
		t.Fatal("composer secondary HUD label did not render")
	}
}

func TestP18_2HUDLeadsThinkingAndToolsInLiveAndSettledTurns(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	for _, live := range []bool{false, true} {
		name := "settled"
		status := "Complete"
		run := runCompleted
		toolStatus := toolCallCompleted
		toolTitle := textArgs("chatRanTools", map[string]any{"count": 1})
		if live {
			name = "live"
			status = "Streaming"
			run = runStreaming
			toolStatus = toolCallRunning
			toolTitle = "read"
		}
		t.Run(name, func(t *testing.T) {
			state := p18_2HUDState(t)
			state.run = run
			state.chat = []chatMessage{
				{
					role:   chatUser,
					status: "Complete",
					blocks: []chatBlock{{kind: chatBlockText, text: "Run the tool"}},
				},
				{
					role:         chatAssistant,
					status:       status,
					outputTokens: 21,
					thinking:     "Inspect the request.",
					blocks: []chatBlock{{
						kind: chatBlockTool,
						tool: toolCallView{id: "tool-1", name: "read", status: toolStatus},
					}},
				},
			}
			tt := ui.NewTester(state.view, 1180, 760)
			hud, ok := tt.Find(state.piHUDTurnLabel(&state.chat[1]))
			if !ok {
				t.Fatal("turn HUD did not render")
			}
			thinking, ok := tt.Find("Inspect the request.")
			if !ok {
				t.Fatal("thinking line did not render")
			}
			tool, ok := tt.Find(toolTitle)
			if !ok {
				t.Fatalf("tool step %q did not render: %#v", toolTitle, tt.Texts())
			}
			prompt, ok := tt.Find("Run the tool")
			if !ok {
				t.Fatal("prompt did not render")
			}
			if !(prompt.Y < hud.Y && hud.Y < thinking.Y && thinking.Y < tool.Y) {
				t.Fatalf("unexpected turn order: prompt=%#v hud=%#v thinking=%#v tool=%#v",
					prompt, hud, thinking, tool)
			}
		})
	}
}

func TestP18_2HUDRendersOnlyForLatestPromptTurn(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	for _, preset := range []appearancePreset{appearancePresetBYOP, appearancePresetJAgent} {
		t.Run(string(preset), func(t *testing.T) {
			state := p18_2HUDState(t)
			state.preferences.AppearancePreset = string(preset)
			state.chat = []chatMessage{
				{
					role:   chatUser,
					blocks: []chatBlock{{kind: chatBlockText, text: "First prompt"}},
				},
				{
					role:         chatAssistant,
					status:       "Complete",
					outputTokens: 152,
					blocks: []chatBlock{{
						kind: chatBlockTool,
						tool: toolCallView{id: "tool-1", name: "read", status: toolCallCompleted},
					}},
				},
				{
					role:   chatAssistant,
					status: "Complete",
					blocks: []chatBlock{{kind: chatBlockText, text: "First final"}},
				},
				{
					role:   chatUser,
					blocks: []chatBlock{{kind: chatBlockText, text: "Second prompt"}},
				},
				{
					role:         chatAssistant,
					status:       "Complete",
					outputTokens: 75,
					blocks:       []chatBlock{{kind: chatBlockText, text: "Second final"}},
				},
			}
			tt := ui.NewTester(state.view, 1180, 760)
			oldLabel := state.piHUDTurnLabel(&state.chat[1])
			oldCount := 0
			latestLabel := state.piHUDTurnLabel(&state.chat[4])
			latestCount := 0
			for _, value := range tt.Texts() {
				if value == oldLabel {
					oldCount++
				}
				if value == latestLabel {
					latestCount++
				}
			}
			if oldCount != 0 {
				t.Fatalf("historical turn HUD rendered %d times, want zero; texts=%#v",
					oldCount, tt.Texts())
			}
			if latestCount != 1 {
				t.Fatalf("latest turn HUD rendered %d times, want once; texts=%#v",
					latestCount, tt.Texts())
			}
			if _, ok := tt.Find("pi-hud-row"); !ok {
				t.Fatal("current-state composer footer disappeared")
			}
		})
	}
}

func TestP18_2JAgentHUDPrecedesWorkSummaryAndThinking(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	state := p18_2HUDState(t)
	state.preferences.AppearancePreset = string(appearancePresetJAgent)
	state.chat = []chatMessage{
		{
			role:   chatUser,
			blocks: []chatBlock{{kind: chatBlockText, text: "Explain this."}},
		},
		{
			role:         chatAssistant,
			status:       "Complete",
			outputTokens: 18,
			thinking:     "Check the answer.",
			blocks: []chatBlock{
				{kind: chatBlockTool, tool: toolCallView{
					id: "tool-1", name: "read", status: toolCallCompleted,
				}},
				{kind: chatBlockText, text: "Final answer."},
			},
		},
	}
	tt := ui.NewTester(state.view, 900, 600)
	hud, ok := tt.Find(state.piHUDTurnLabel(&state.chat[1]))
	if !ok {
		t.Fatal("J Agent HUD did not render")
	}
	summary, ok := tt.Find(textArgs("chatWorkedFor", map[string]any{"duration": "0s"}))
	if !ok {
		t.Fatalf("J Agent work summary did not render: %#v", tt.Texts())
	}
	thinking, ok := tt.Find("Check the answer.")
	if !ok {
		t.Fatal("J Agent thinking line did not render")
	}
	if !(hud.Y < summary.Y && summary.Y < thinking.Y) {
		t.Fatalf("J Agent order is hud=%#v summary=%#v thinking=%#v", hud, summary, thinking)
	}
}

func TestP18_3ConversationHUDKeepsTokensAndTPSAtNarrowWidths(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	appearances := []struct {
		name   string
		preset appearancePreset
		dark   bool
	}{
		{name: "light", preset: appearancePresetBYOP, dark: false},
		{name: "dark", preset: appearancePresetBYOP, dark: true},
		{name: "j-agent", preset: appearancePresetJAgent, dark: true},
	}
	sizes := []struct {
		width, height int
	}{
		{width: 1100, height: 560},
		{width: 1180, height: 760},
		{width: 1440, height: 900},
	}
	for _, appearance := range appearances {
		for _, size := range sizes {
			t.Run(fmt.Sprintf("%s-%dx%d", appearance.name, size.width, size.height), func(t *testing.T) {
				currentAppearanceDark = func() bool { return appearance.dark }
				state := p18_2HUDState(t)
				state.preferences.AppearancePreset = string(appearance.preset)
				state.tasksOpen = true
				tt := ui.NewTester(state.view, size.width, size.height)

				hud, ok := tt.Find(state.piHUDTurnLabel(&state.chat[1]))
				if !ok || hud.W <= 0 || hud.H <= 0 {
					t.Fatalf("conversation HUD did not render: %#v", hud)
				}
				if hud.X < 0 || hud.X+hud.W > float32(size.width) ||
					hud.Y < 0 || hud.Y+hud.H > float32(size.height) {
					t.Fatalf("conversation HUD escaped viewport: %#v", hud)
				}
				for _, label := range []string{
					"↓152",
					textArgs("hudTokensPerSecond", map[string]any{"speed": "51.2"}),
				} {
					rect, ok := tt.Find(label)
					if !ok || rect.W <= 0 || rect.H <= 0 {
						t.Fatalf("required transcript HUD item %q did not render: %#v", label, rect)
					}
					if rect.X < hud.X || rect.X+rect.W > hud.X+hud.W {
						t.Fatalf("required transcript HUD item %q escaped its row: item=%#v row=%#v",
							label, rect, hud)
					}
				}
				footer, ok := tt.Find("pi-hud-row")
				if !ok {
					t.Fatal("composer footer HUD did not render")
				}
				if footer.H != 20 {
					t.Fatalf("composer footer height = %vpx, want 20px", footer.H)
				}
				if footer.X < 0 || footer.X+footer.W > float32(size.width) ||
					footer.Y < 0 || footer.Y+footer.H > float32(size.height) {
					t.Fatalf("composer footer escaped viewport: %#v", footer)
				}
				requiredFooter := []string{
					piHUDContextSummary(state.hud.stats, state.hud.contextWindowFallback, state.hud.autoCompact),
					"↑12k ↓3.0k",
					"Cache hit",
					textArgs("hudAverageSpeed", map[string]any{"speed": "50.0"}),
					"$0.012",
				}
				previousX := footer.X - 1
				firstFooter := ui.Rect{}
				for _, label := range requiredFooter {
					rect, ok := tt.Find(label)
					if !ok || rect.W <= 0 || rect.H <= 0 {
						t.Fatalf("required footer item %q did not render: %#v", label, rect)
					}
					if firstFooter.W == 0 {
						firstFooter = rect
					}
					if rect.X < footer.X || rect.X+rect.W > footer.X+footer.W {
						t.Fatalf("footer item %q escaped row: item=%#v row=%#v", label, rect, footer)
					}
					if rect.X < previousX {
						t.Fatalf("footer item %q is out of order: previousX=%v item=%#v",
							label, previousX, rect)
					}
					previousX = rect.X
				}
				ov, ok := tt.Find("OV ✓")
				if !ok || ov.X < footer.X || ov.X+ov.W > footer.X+footer.W {
					t.Fatalf("OV status escaped footer row: row=%#v ov=%#v", footer, ov)
				}
				if firstFooter.X > footer.X+1 ||
					ov.X+ov.W < footer.X+footer.W-1 {
					t.Fatalf("footer groups are not aligned to opposite edges: row=%#v first=%#v ov=%#v",
						footer, firstFooter, ov)
				}
				if size.width >= 1440 {
					if rect, ok := tt.Find("DeepSeek V4.1 Flash"); !ok || rect.W <= 0 {
						t.Fatalf("wide layout omitted model name: %#v", rect)
					}
				}
			})
		}
	}
}

func TestP18_3ConversationHUDRendersAcrossAppearanceThemes(t *testing.T) {
	state := p18_2HUDState(t)
	tests := []struct {
		name   string
		preset appearancePreset
		dark   bool
	}{
		{name: "light", preset: appearancePresetBYOP, dark: false},
		{name: "dark", preset: appearancePresetBYOP, dark: true},
		{name: "j-agent", preset: appearancePresetJAgent, dark: true},
	}
	oldDark := currentAppearanceDark
	oldHigh := currentAppearanceHighContrast
	defer func() {
		currentAppearanceDark = oldDark
		currentAppearanceHighContrast = oldHigh
	}()
	currentAppearanceHighContrast = func() bool { return false }

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			currentAppearanceDark = func() bool { return test.dark }
			state.preferences.AppearancePreset = string(test.preset)
			theme, _ := nativeThemeForAppearance(test.preset, test.dark, false, false)
			tt := ui.NewTester(func(c *ui.Context) {
				c.SetTheme(theme)
				state.piHUDTurn(c, &state.chat[1])
			}, 720, 40)
			if _, ok := tt.Find(state.piHUDTurnLabel(&state.chat[1])); !ok {
				t.Fatalf("%s transcript HUD did not render", test.name)
			}
			if _, ok := tt.Find("conversation-hud-model-logo"); !ok {
				t.Fatalf("%s model logo did not render", test.name)
			}
			if _, ok := tt.Find(state.piHUDContextLabel()); ok {
				t.Fatalf("%s transcript repeated full context usage", test.name)
			}
			if _, ok := tt.Find("↓152"); !ok {
				t.Fatalf("%s output tokens did not render", test.name)
			}
			if _, ok := tt.Find(textArgs("hudTokensPerSecond", map[string]any{"speed": "51.2"})); !ok {
				t.Fatalf("%s TPS did not render", test.name)
			}
		})
	}
}

func TestP18_2ComposerFooterTooltipIncludesCacheAndAverageSpeed(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	state := p18_2HUDState(t)
	state.hud.avgOutputTokens = 100
	state.hud.avgGenerationSeconds = 2
	tt := ui.NewTester(func(c *ui.Context) {
		theme, _ := nativeThemeWithContrast(false, false)
		c.SetTheme(theme)
		state.piHUDFooter(c)
	}, 900, 28)
	row, ok := tt.Find(state.piHUDFooterLabel())
	if !ok {
		t.Fatal("footer HUD did not render")
	}
	tt.Move(row.X+row.W/2, row.Y+row.H/2)
	tt.Advance(700 * time.Millisecond)
	for _, label := range []string{
		textArgs("hudCacheHitCumulative", map[string]any{"percent": "3.2"}),
		textArgs("hudCacheTokens", map[string]any{"read": "400", "write": "100"}),
		textArgs("hudAverageSpeedTooltip", map[string]any{"speed": "50.0"}),
	} {
		if !tt.HasText(label) {
			t.Fatalf("footer tooltip missing %q; texts=%#v", label, tt.Texts())
		}
	}
}

func TestP18_2PluginRestartUsesDedicatedLightweightState(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	entry := p18_2WebViewRestartEntry("local.lightweight-table", "main")
	state := &appState{
		page:               pagePluginView,
		pluginViewPluginID: entry.ID,
		pluginViewID:       "main",
		pluginViewKind:     pluginViewWebView,
		pluginRegistry:     pluginRegistry{Entries: []pluginRegistryEntry{entry}},
		pluginRestarting:   map[string]bool{entry.ID: true},
		preferences:        defaultByopPreferences(),
		pluginWebViews:     make(map[string]*pluginWebViewState),
		pluginBridgeSlots:  make(map[string]chan struct{}),
	}
	tt := ui.NewTester(func(c *ui.Context) {
		theme, p := nativeThemeWithContrast(false, false)
		c.SetTheme(theme)
		state.pluginWebViewPage(c, p)
	}, 900, 600)

	if _, ok := tt.Find("plugin-webview-restarting"); !ok {
		t.Fatalf("dedicated restart state did not render: %#v", tt.Texts())
	}
	if !tt.HasText(textArgs("pluginsRestarting", map[string]any{"name": "Fuyao"})) {
		t.Fatalf("restart state was not readable: %#v", tt.Texts())
	}
	if tt.HasText(text("pluginsWebViewUnavailable")) {
		t.Fatalf("restart used the yellow WebView warning state: %#v", tt.Texts())
	}
}

func TestP18_2PluginRestartGuardsWebViewCreation(t *testing.T) {
	entry := p18_2WebViewRestartEntry("local.lightweight-table", "main")
	view := entry.Manifest.Contributes.WebViews[0]
	state := &appState{
		page:             pagePlugins,
		pluginRegistry:   pluginRegistry{Entries: []pluginRegistryEntry{entry}},
		pluginRestarting: map[string]bool{entry.ID: true},
		pluginWebViews:   make(map[string]*pluginWebViewState),
	}

	state.openPluginWebView(entry, view)
	if state.page != pagePluginView || state.pluginViewPluginID != entry.ID ||
		state.pluginViewID != view.ID {
		t.Fatalf("restart navigation did not preserve the requested view: %#v", state)
	}
	if len(state.pluginWebViews) != 0 {
		t.Fatalf("openPluginWebView created a WebView while restarting: %#v", state.pluginWebViews)
	}
	state.recreatePluginWebView(entry.ID, view.ID)
	if len(state.pluginWebViews) != 0 {
		t.Fatalf("recreatePluginWebView created a WebView while restarting: %#v", state.pluginWebViews)
	}
}

func TestP18_2PluginRestartReopensCurrentView(t *testing.T) {
	entry := p18_2WebViewRestartEntry("local.lightweight-table", "first", "second")
	state := &appState{
		page:               pagePluginView,
		pluginViewPluginID: entry.ID,
		pluginViewID:       "first",
		pluginViewKind:     pluginViewWebView,
		pluginRegistry:     pluginRegistry{Entries: []pluginRegistryEntry{entry}},
		pluginRestarting:   map[string]bool{entry.ID: true},
		pluginWebViews:     make(map[string]*pluginWebViewState),
		preferences:        defaultByopPreferences(),
	}
	// The user switched from the initial view while the restart was running.
	state.pluginViewID = "second"
	state.completePluginRestart(entry.ID)
	state.reopenCurrentPluginAfterRestart(entry)

	if state.page != pagePluginView || state.pluginViewPluginID != entry.ID ||
		state.pluginViewID != "second" || state.pluginViewKind != pluginViewWebView {
		t.Fatalf("restart did not reopen the current view: page=%s plugin=%q view=%q kind=%q",
			state.page, state.pluginViewPluginID, state.pluginViewID, state.pluginViewKind)
	}
}

func TestP18_2PluginRestartFailureDropsStaleWebView(t *testing.T) {
	entry := p18_2WebViewRestartEntry("local.lightweight-table", "main")
	state := &appState{
		pluginRestarting: map[string]bool{entry.ID: true},
		pluginWebViews: map[string]*pluginWebViewState{
			pluginViewCacheKey(entry.ID, "main"): {
				pluginID: entry.ID,
				viewID:   "main",
			},
		},
	}
	state.finishPluginRestartFailure(entry.ID, entry.Name, errP18Restart)
	if len(state.pluginWebViews) != 0 {
		t.Fatalf("restart failure retained stale WebView state: %#v", state.pluginWebViews)
	}
	if state.pluginRestarting[entry.ID] {
		t.Fatal("restart failure left the plugin marked as restarting")
	}
}

var errP18Restart = fmt.Errorf("restart failed")

func p18_2WebViewRestartEntry(id string, viewIDs ...string) pluginRegistryEntry {
	views := make([]pluginWebViewContribution, 0, len(viewIDs))
	for _, viewID := range viewIDs {
		views = append(views, pluginWebViewContribution{
			ID:       viewID,
			Title:    "Fuyao",
			Location: "main",
			Entry:    "web/" + viewID + ".html",
		})
	}
	return pluginRegistryEntry{
		ID:     id,
		Name:   "Fuyao",
		Status: pluginRegistered,
		Manifest: &pluginManifest{
			ID:   id,
			Name: "Fuyao",
			Contributes: pluginContributions{
				WebViews: views,
			},
		},
	}
}
