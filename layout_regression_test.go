package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components/chat"
)

func TestP10aNavigationFooterVisibleWithManySessions(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	newState := func(sessionCount int) *appState {
		state := layoutTestState(t)
		state.piSessions = make([]piSessionSummary, sessionCount)
		for i := range state.piSessions {
			state.piSessions[i] = piSessionSummary{
				Path:    fmt.Sprintf("/tmp/byop-layout-session-%02d.jsonl", i),
				ID:      fmt.Sprintf("session-%02d", i),
				Title:   fmt.Sprintf("Session %02d", i),
				Updated: time.Now().Add(-time.Duration(i) * time.Minute),
			}
		}
		state.piSessionsExpanded = true
		state.refreshSessionOrganization(time.Now())
		return state
	}

	t.Run("three sessions remain fully visible", func(t *testing.T) {
		state := newState(3)
		tt := ui.NewTester(state.view, 1100, 560)
		for _, title := range []string{"Session 00", "Session 01", "Session 02"} {
			assertInsideViewport(t, tt, title, 560)
		}
		assertNavigationFooterInsideViewport(t, tt, state, 560)
	})

	t.Run("many sessions leave first rows and footer visible", func(t *testing.T) {
		state := newState(40)
		tt := ui.NewTester(state.view, 1100, 560)
		for _, title := range []string{"Session 00", "Session 01", "Session 02"} {
			rect, ok := tt.Find(title)
			if !ok {
				t.Fatalf("layout label %q was not rendered", title)
			}
			if rect.H <= 0 {
				t.Fatalf("layout label %q has no visible height: %#v", title, rect)
			}
		}
		assertNavigationFooterInsideViewport(t, tt, state, 560)
	})
}

func assertNavigationFooterInsideViewport(t *testing.T, tt *ui.Tester, state *appState, height int) {
	t.Helper()
	assertInsideViewport(t, tt, textArgs("piStatusLabel", map[string]any{
		"status": state.connectionStateLabel(),
	}), height)
	assertInsideViewport(t, tt, text("navSettings"), height)
}

func TestP10aComposerControlsStayInsideViewport(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	for _, size := range []struct {
		width, height int
	}{
		{width: 1100, height: 560},
		{width: 1180, height: 760},
		{width: 1440, height: 900},
	} {
		for _, scenario := range []struct {
			name  string
			setup func(*appState)
			mark  func(*appState) string
		}{
			{
				name: "completed follow-up card",
				setup: func(state *appState) {
					state.run = runCompleted
					state.followUpSuggestion = "Add a focused regression test for the composer layout."
				},
				mark: func(*appState) string { return text("followUpSuggestedNext") },
			},
			{
				name: "running queue",
				setup: func(state *appState) {
					state.run = runStreaming
					state.composerDraft = "Keep the current response focused."
					state.queueSteeringCount = 1
					state.queueSteering = []string{"Keep the transcript scrollable."}
				},
				mark: func(*appState) string { return text("queueTitle") },
			},
			{
				name: "connection error",
				setup: func(state *appState) {
					state.piConn = piConnectionErr
					state.piErrorCode = piErrorCodeUnknown
					state.piError = strings.Repeat("socket detail ", 20)
				},
				mark: func(state *appState) string { return state.piConnectionErrorText() },
			},
			{
				name: "task panel open",
				setup: func(state *appState) {
					state.tasksOpen = true
				},
				mark: func(*appState) string { return text("tasksTitle") },
			},
		} {
			t.Run(fmt.Sprintf("%s/%dx%d", scenario.name, size.width, size.height), func(t *testing.T) {
				state := layoutTestState(t)
				scenario.setup(state)
				tt := ui.NewTester(state.view, size.width, size.height)

				assertInsideViewport(t, tt, scenario.mark(state), size.height)
				assertInsideViewport(t, tt, text("chatMessageLabel"), size.height)
				assertInsideViewport(t, tt, text("chatAttachmentUnavailable"), size.height)
				assertComposerFooterInsideViewport(t, tt, "composer-footer", size.height)
				assertInsideViewport(t, tt, text("chatSend"), size.height)
				if scenario.name == "running queue" {
					assertInsideViewport(t, tt, text("runningSendQueue"), size.height)
					assertInsideViewport(t, tt, text("runningSendSteer"), size.height)
					assertNotRendered(t, tt, text("chatStopCurrentRun"))
				} else {
					assertNotRendered(t, tt, text("chatStopCurrentRun"))
				}
			})
		}
	}
}

func TestP12EffortPickerUsesAppReducedMotion(t *testing.T) {
	state := &appState{preferences: byopPreferences{
		Motion: string(motionReduced),
	}}
	if got := state.effortFillDuration(false); got != 0 {
		t.Fatalf("effort fill duration with reduced app motion = %s, want 0", got)
	}

	state.preferences.Motion = string(motionStandard)
	if got := state.effortFillDuration(true); got != 250*time.Millisecond {
		t.Fatalf("effort fill duration with standard app motion = %s, want 250ms", got)
	}
	if got := state.effortFillDuration(false); got != 250*time.Millisecond {
		t.Fatalf("effort fill duration with standard motion = %s, want 250ms", got)
	}

	state.preferences.Motion = string(motionSystem)
	if got := state.effortFillDuration(true); got != 0 {
		t.Fatalf("effort fill duration with reduced system motion = %s, want 0", got)
	}
}

func TestP10cBodyAndTaskPanelLayoutInvariants(t *testing.T) {
	if fontBody != 14 {
		t.Fatalf("body font = %vpx, want 14px", fontBody)
	}
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	for _, size := range []struct {
		width, height int
	}{
		{width: 1100, height: 560},
		{width: 1180, height: 760},
	} {
		state := layoutTestState(t)
		state.tasksOpen = true
		tt := ui.NewTester(state.view, size.width, size.height)
		assertInsideViewport(t, tt, text("tasksTitle"), size.height)
		assertInsideViewport(t, tt, text("tasksOwnership"), size.height)
	}
}

func TestP10cTranscriptHUDRemainsVisibleInNarrowChatMainArea(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	state := p18_2HUDState(t)
	state.tasksOpen = true
	if got := state.chatMainWidth(1100); got >= chat.ContentWidth {
		t.Fatalf("test chat main width = %vpx, want < %vpx", got, chat.ContentWidth)
	}

	tt := ui.NewTester(state.view, 1100, 560)
	rect, ok := tt.Find(state.piHUDTurnLabel(&state.chat[1]))
	if !ok {
		t.Fatal("transcript HUD was not rendered with a narrow chat main area")
	}
	if rect.H <= 0 || rect.Y < 0 || rect.Y+rect.H > 560 {
		t.Fatalf("transcript HUD is outside the 1100x560 viewport: %#v", rect)
	}
	if got := state.chatMainWidth(1100); got >= 720 {
		t.Fatalf("chat main width = %vpx, want the narrow-transcript path below 720px", got)
	}
	assertInsideViewport(t, tt, "↓152", 560)
	assertInsideViewport(t, tt, textArgs("hudTokensPerSecond", map[string]any{"speed": "51.2"}), 560)
}

func TestP10cRunningSendModeControlUpdatesPreference(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	state := layoutTestState(t)
	state.run = runStreaming
	state.composerDraft = "Keep the current response focused."
	state.preferences.RunningSendMode = string(runningSendQueue)
	tt := ui.NewTester(state.view, 1180, 760)

	if err := tt.Click(text("runningSendSteer")); err != nil {
		t.Fatal(err)
	}
	if got := state.currentRunningSendMode(); got != runningSendSteer {
		t.Fatalf("running-send mode = %q, want %q", got, runningSendSteer)
	}
	assertInsideViewport(t, tt, text("chatSend"), 760)
	assertNotRendered(t, tt, text("chatStopCurrentRun"))
}

func TestP10bTitleMenuReceivesClicks(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	state := layoutTestState(t)
	tt := ui.NewTester(func(c *ui.Context) {
		theme, palette := nativeThemeWithContrast(false, false)
		c.SetTheme(theme)
		state.titleBar(c, palette)
	}, 1100, 80)

	if err := tt.Click(text("titleSessionMenu")); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(text("titleRenameSession")); err != nil {
		t.Fatal(err)
	}
	if !state.sessionRenameEditing {
		t.Fatal("choosing Rename did not enter inline rename mode")
	}
}

func TestP13TitleCapsuleMaxWidthFitsStageAt760(t *testing.T) {
	state := layoutTestState(t)
	state.navCollapsed = false
	state.tasksOpen = true

	const windowWidth float32 = 760
	stageWidth := state.chatMainWidth(windowWidth)
	maxWidth := state.titleCapsuleMaxWidth(windowWidth)
	if maxWidth < 0 {
		t.Fatalf("title capsule max width = %vpx, want >= 0", maxWidth)
	}
	if used := maxWidth + 36 + 12; used > stageWidth {
		t.Fatalf("title bar uses %vpx of the %vpx stage", used, stageWidth)
	}
	if want := min(float32(520), stageWidth*.6); maxWidth != want {
		t.Fatalf("title capsule max width = %vpx at %vpx, want %vpx", maxWidth, windowWidth, want)
	}
}

func TestP13NarrowNavigationCollapsesAndTasksOverlayStage(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()
	currentAppearanceDark = func() bool { return true }

	state := layoutTestState(t)
	state.navCollapsed = false
	state.tasksOpen = false
	state.preferences.Wallpaper = "ember"
	state.preferences.Motion = string(motionStandard)

	tt := ui.NewTester(state.view, 1180, 760)
	state.navCollapsed = true
	tt.Frame()
	tt.Advance(500 * time.Millisecond)
	state.navCollapsed = false
	state.tasksOpen = true
	tt.SetSize(760, 600)
	tt.Advance(500 * time.Millisecond)
	navToggle := textArgs("titleToggleNavigationShortcut", map[string]any{
		"shortcut": panelShortcutModifier() + "B",
	})
	rects := make(map[string]ui.Rect)
	for _, label := range []string{navToggle, text("tasksTitle")} {
		rect, ok := tt.Find(label)
		if !ok {
			t.Fatalf("layout label %q was not rendered", label)
		}
		rects[label] = rect
	}
	if rect := rects[navToggle]; rect.X > 64 {
		t.Fatalf("navigation toggle x = %vpx at 760px, want collapsed navigation", rect.X)
	}
	if got, want := state.chatMainWidth(760), float32(680); got != want {
		t.Fatalf("chat main width = %vpx at 760px, want %vpx with tasks overlaying", got, want)
	}
	if rect := rects[text("tasksTitle")]; rect.X < 440 || rect.W < 240 {
		t.Fatalf("task panel title rect = %#v at 760px, want overlay panel", rect)
	}
}

func layoutTestState(t *testing.T) *appState {
	t.Helper()
	cwd := t.TempDir()
	state := &appState{
		page:            pageChat,
		piConn:          piConnected,
		piCwd:           cwd,
		piModel:         "openai/gpt-5",
		piModelProvider: "openai",
		piModelID:       "gpt-5",
		piModelName:     "GPT-5",
		piModels: []piModel{{
			Provider:      "openai",
			ID:            "gpt-5",
			Name:          "GPT-5",
			ContextWindow: 200_000,
		}},
		piThinkingLevelsKnown: true,
		piSessionFile:         cwd + "/current.jsonl",
		piSessionID:           "current",
		preferences:           byopPreferences{FollowUpStyle: string(followUpStyleCard), Motion: string(motionReduced)},
		hud:                   hudState{cwd: cwd, branch: "main"},
		run:                   runCompleted,
		composerDraft:         "",
		followUpSuggestion:    "",
	}
	for i := 0; i < 24; i++ {
		state.chat = append(state.chat,
			chatMessage{
				role:   chatUser,
				status: "Complete",
				blocks: []chatBlock{{kind: chatBlockText, text: fmt.Sprintf("Question %02d about the long transcript.", i)}},
			},
			chatMessage{
				role:   chatAssistant,
				status: "Complete",
				blocks: []chatBlock{{
					kind: chatBlockText,
					text: fmt.Sprintf("Answer %02d.\n\n%s", i, strings.Repeat("The transcript scrolls independently. ", 8)),
				}},
			},
		)
	}
	return state
}

func stableTestAppearance(t *testing.T) func() {
	t.Helper()
	oldDark := currentAppearanceDark
	oldHighContrast := currentAppearanceHighContrast
	oldReduceMotion := currentAppearanceReduceMotion
	currentAppearanceDark = func() bool { return false }
	currentAppearanceHighContrast = func() bool { return false }
	currentAppearanceReduceMotion = func() bool { return false }
	return func() {
		currentAppearanceDark = oldDark
		currentAppearanceHighContrast = oldHighContrast
		currentAppearanceReduceMotion = oldReduceMotion
	}
}

func assertInsideViewport(t *testing.T, tt *ui.Tester, label string, height int) {
	t.Helper()
	rect, ok := tt.Find(label)
	if !ok {
		t.Fatalf("layout label %q was not rendered", label)
	}
	if rect.H <= 0 {
		t.Fatalf("layout label %q has no visible height: %#v", label, rect)
	}
	if rect.Y < 0 || rect.Y+rect.H > float32(height) {
		t.Fatalf("layout label %q is outside the %dpx viewport: %#v", label, height, rect)
	}
}

func assertComposerFooterInsideViewport(t *testing.T, tt *ui.Tester, label string, height int) {
	t.Helper()
	_, ok := tt.Find(text("chatMessageLabel"))
	if !ok {
		t.Fatalf("composer was not rendered")
	}
	rect, ok := tt.Find(label)
	if !ok {
		t.Fatalf("layout label %q was not rendered", label)
	}
	if rect.H <= 0 {
		t.Fatalf("layout label %q has no visible height: %#v", label, rect)
	}
	if rect.Y < 0 || rect.Y+rect.H > float32(height) {
		t.Fatalf("composer footer %q bottom is outside the %dpx viewport: %#v", label, height, rect)
	}
	if height == 560 && rect.Y+rect.H >= float32(height) {
		t.Fatalf("composer footer %q is not fully above the %dpx viewport: %#v", label, height, rect)
	}
}

func assertNotRendered(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if rect, ok := tt.Find(label); ok {
		t.Fatalf("layout label %q should not be rendered: %#v", label, rect)
	}
}
