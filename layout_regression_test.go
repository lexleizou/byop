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
				assertInsideViewport(t, tt, state.piHUDLabel(), size.height)
				assertInsideViewport(t, tt, text("chatSend"), size.height)
				assertComposerFooterInsideViewport(t, tt, state.piHUDLabel(), size.height)
				if scenario.name == "running queue" {
					assertInsideViewport(t, tt, text("runningSendQueue"), size.height)
					assertInsideViewport(t, tt, text("runningSendSteer"), size.height)
					assertInsideViewport(t, tt, text("chatStopCurrentRun"), size.height)
				} else {
					assertNotRendered(t, tt, text("chatStopCurrentRun"))
				}
			})
		}
	}
}

func TestP10cBodyAndTaskPanelLayoutInvariants(t *testing.T) {
	if fontBody != 14 {
		t.Fatalf("body font = %vpx, want 14px", fontBody)
	}
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	state := layoutTestState(t)
	state.tasksOpen = true
	tt := ui.NewTester(state.view, 1100, 560)
	assertInsideViewport(t, tt, text("tasksTitle"), 560)
	assertInsideViewport(t, tt, text("tasksOwnership"), 560)
}

func TestP10cHUDSwitchesUsingChatMainAreaWidth(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	state := layoutTestState(t)
	state.tasksOpen = true
	if got := state.chatMainWidth(1100); got >= chat.ContentWidth {
		t.Fatalf("test chat main width = %vpx, want < %vpx", got, chat.ContentWidth)
	}

	tt := ui.NewTester(state.view, 1100, 560)
	rect, ok := tt.Find("pi-hud-row")
	if !ok {
		t.Fatal("compact HUD row was not rendered with a narrow chat main area")
	}
	if rect.H <= 0 || rect.Y < 0 || rect.Y+rect.H > 560 {
		t.Fatalf("compact HUD row is outside the 1100x560 viewport: %#v", rect)
	}
	if got := state.chatMainWidth(1100); got >= 720 {
		t.Fatalf("chat main width = %vpx, want the compact-HUD path below 720px", got)
	}
}

func TestP10cRunningSendModeControlUpdatesPreference(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	state := layoutTestState(t)
	state.run = runStreaming
	state.preferences.RunningSendMode = string(runningSendQueue)
	tt := ui.NewTester(state.view, 1180, 760)

	if err := tt.Click(text("runningSendSteer")); err != nil {
		t.Fatal(err)
	}
	if got := state.currentRunningSendMode(); got != runningSendSteer {
		t.Fatalf("running-send mode = %q, want %q", got, runningSendSteer)
	}
	assertInsideViewport(t, tt, text("chatSend"), 760)
	assertInsideViewport(t, tt, text("chatStopCurrentRun"), 760)
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

func layoutTestState(t *testing.T) *appState {
	t.Helper()
	cwd := t.TempDir()
	state := &appState{
		page:               pageChat,
		piConn:             piConnected,
		piCwd:              cwd,
		piModel:            "openai/gpt-5",
		piModelName:        "GPT-5",
		piSessionFile:      cwd + "/current.jsonl",
		piSessionID:        "current",
		preferences:        byopPreferences{FollowUpStyle: string(followUpStyleCard), Motion: string(motionReduced)},
		hud:                hudState{cwd: cwd, branch: "main"},
		run:                runCompleted,
		composerDraft:      "",
		followUpSuggestion: "",
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
