package main

import (
	"math"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestP13bNavigationWidthBoundsAndStageWidth(t *testing.T) {
	state := layoutTestState(t)
	state.navCollapsed = false
	state.navWidth = 0
	if got := state.navigationExpandedWidth(); got != navExpandedWidth {
		t.Fatalf("default expanded navigation width = %v, want %v", got, navExpandedWidth)
	}

	for _, test := range []struct {
		saved, want float32
	}{
		{saved: 10, want: navMinExpandedWidth},
		{saved: navMaxExpandedWidth + 100, want: navMaxExpandedWidth},
		{saved: 260, want: 260},
	} {
		if got := normalizeNavExpandedWidth(test.saved); got != test.want {
			t.Errorf("normalizeNavExpandedWidth(%v) = %v, want %v", test.saved, got, test.want)
		}
	}

	// The stage width follows the resized navigation, and a narrow window keeps
	// the collapsed width whatever was saved.
	state.navWidth = 300
	if got, want := state.chatMainWidth(1180), float32(1180-2*shellGap-300-shellGap); got != want {
		t.Fatalf("chat main width = %v at 1180, want %v", got, want)
	}
	if got := state.navigationWidth(760); got != navCollapsedWidth {
		t.Fatalf("navigation width = %v at 760, want the collapsed %v", got, navCollapsedWidth)
	}
	if got, want := state.chatMainWidth(760), float32(760-2*shellGap-navCollapsedWidth-shellGap); got != want {
		t.Fatalf("chat main width = %v at 760, want %v", got, want)
	}
	// A saved width wider than the window allows is limited, not clipped.
	state.navWidth = navMaxExpandedWidth
	if got := state.navigationWidth(800); got != navCollapsedWidth {
		t.Fatalf("navigation width = %v at 800, want the collapsed %v", got, navCollapsedWidth)
	}
}

func TestP13bNavigationWidthKeepsMainAreaWithTaskPanel(t *testing.T) {
	state := layoutTestState(t)
	state.navCollapsed = false
	state.navWidth = navMaxExpandedWidth
	state.preferences.NavExpandedWidth = navMaxExpandedWidth
	state.tasksOpen = true

	// The widest the sidebar may grow at 960 leaves the chat stage its minimum
	// once the task panel and the shell gaps take their room.
	const windowWidth float32 = 960
	limit := state.navExpandedWidthLimit(windowWidth)
	if limit < navMinExpandedWidth {
		t.Fatalf("width limit = %v at %v, want at least %v", limit, windowWidth, navMinExpandedWidth)
	}
	main := state.chatMainWidth(windowWidth)
	if main < chatMinMainWidth {
		t.Fatalf("chat main width = %v at %v with the task panel open, want at least %v",
			main, windowWidth, chatMinMainWidth)
	}

	// The clamp narrows what the layout shows without rewriting the preference.
	shown := state.navigationWidth(windowWidth)
	if shown >= navMaxExpandedWidth {
		t.Fatalf("navigation width = %v at %v with the task panel open, want it narrowed below %v",
			shown, windowWidth, navMaxExpandedWidth)
	}
	if got := state.navigationExpandedWidth(); got != navMaxExpandedWidth {
		t.Fatalf("saved navigation width = %v, want the chosen %v kept", got, navMaxExpandedWidth)
	}
	if got := state.preferences.NavExpandedWidth; got != navMaxExpandedWidth {
		t.Fatalf("preference width = %v, want the chosen %v kept", got, navMaxExpandedWidth)
	}

	// Settings reserve the same width, so its content area stays usable too.
	if got := state.settingsContentWidthV12(windowWidth); got <= 0 {
		t.Fatalf("settings content width = %v at %v, want a usable area", got, windowWidth)
	}

	// The rendered stage matches that width: the transcript HUD starts after
	// the navigation and ends before the task panel, inside the window.
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()
	tt := ui.NewTester(state.view, int(windowWidth), 760)
	hud, ok := tt.Find(state.piHUDLabel())
	if !ok {
		t.Fatal("transcript HUD was not rendered")
	}
	navRight := shellGap + shown
	if hud.X < navRight {
		t.Fatalf("transcript HUD starts at %v, want after the %vpx navigation", hud.X, shown)
	}
	if hud.X+hud.W > windowWidth-shellGap-taskPanelWidth-shellGap {
		t.Fatalf("transcript HUD ends at %v, want it before the task panel",
			hud.X+hud.W)
	}
}

func TestP13bNavigationWidthPersistsAcrossRelaunch(t *testing.T) {
	configDir := t.TempDir()
	store := newByopPreferencesStore(configDir)
	state := layoutTestState(t)
	state.preferencesStore = store
	state.preferences = defaultByopPreferences()

	state.navWidth = 284
	state.preferences.NavExpandedWidth = 284
	if err := state.persistNavigationWidth(); err != nil {
		t.Fatalf("persist navigation width: %v", err)
	}

	loaded, warning := store.load()
	if warning != "" {
		t.Fatalf("load preferences: %s", warning)
	}
	if loaded.NavExpandedWidth != 284 {
		t.Fatalf("saved navigation width = %v, want 284", loaded.NavExpandedWidth)
	}

	// Collapsing and expanding keeps the saved expanded width.
	state.setNavCollapsed(true)
	state.setNavCollapsed(false)
	if got := state.navigationExpandedWidth(); got != 284 {
		t.Fatalf("navigation width = %v after collapse and expand, want 284", got)
	}

	// A resize still in progress when the navigation collapses keeps the width
	// it reached, instead of losing it with the handle.
	state.navWidth = 300
	state.preferences.NavExpandedWidth = 300
	state.navResizeDragging = true
	state.navCollapsed = true
	tt := ui.NewTester(state.view, 1180, 760)
	tt.Frame()
	if state.navResizeDragging {
		t.Fatal("a collapsed navigation kept the resize in progress")
	}
	if loaded, _ := store.load(); loaded.NavExpandedWidth != 300 {
		t.Fatalf("saved width = %v after collapsing mid-resize, want 300", loaded.NavExpandedWidth)
	}
	if got := normalizeNavExpandedWidth(loaded.NavExpandedWidth); got != 284 {
		t.Fatalf("restored navigation width = %v, want 284", got)
	}
}

func TestP13bModelPopoverFitsCommonNames(t *testing.T) {
	names := []string{
		"DeepSeek V4.1 Flash · routing group",
		"OpenCode Go",
		"Claude Opus 5.5 · routing group",
	}
	for _, test := range []struct {
		windowWidth float32
	}{
		{windowWidth: 1180},
		{windowWidth: 760},
	} {
		var width, widest float32
		tt := ui.NewTester(func(c *ui.Context) {
			width = modelPopoverWidthV12(c, names, test.windowWidth)
			for _, name := range names {
				if w, _ := c.MeasureText(0, ui.Span{Text: name, Size: fontBody}); w > widest {
					widest = w
				}
			}
		}, int(test.windowWidth), 760)
		tt.Frame()

		if limit := test.windowWidth - 2*modelPopoverSideMarginV12; width > limit {
			t.Fatalf("popover width = %v at %vpx, want at most the viewport's %v",
				width, test.windowWidth, limit)
		}
		if width < widest+modelPopoverChromeV12 {
			t.Fatalf("popover width = %v at %vpx, want at least %v to show the widest name whole",
				width, test.windowWidth, widest+modelPopoverChromeV12)
		}
		if got := modelPopoverListHeightV12(560); got < modelPopoverRowHeightV12 || got > 260 {
			t.Fatalf("model list height = %v at 560px, want a bounded scroll area", got)
		}
		if got := modelPopoverListHeightV12(1400); math.Abs(float64(got-520)) > 0.01 {
			t.Fatalf("model list height = %v at 1400px, want it to stop growing at 520", got)
		}
	}

	// A name wider than the window is capped, keeping its tooltip and ellipsis.
	tt := ui.NewTester(func(c *ui.Context) {
		width := modelPopoverWidthV12(c, []string{strings.Repeat("Exceptional model name ", 12)}, 760)
		if limit := float32(760 - 2*modelPopoverSideMarginV12); width != limit {
			t.Fatalf("popover width = %v, want the viewport limit %v", width, limit)
		}
	}, 760, 760)
	tt.Frame()
}

func TestP13bTitleChromeButtonsStayInteractive(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	state := layoutTestState(t)
	state.sessionRenameEditing = false
	state.tasksOpen = false
	tt := ui.NewTester(func(c *ui.Context) {
		theme, palette := nativeThemeWithContrast(false, false)
		c.SetTheme(theme)
		state.titleBar(c, palette)
	}, 1100, 80)

	// With a session loaded the title text is the session menu button, so a
	// press there opens the menu, not a window drag: the menu stays the
	// behaviour of that region. Window dragging comes from the blank chrome
	// around it (the bar, its spacers and the capsule padding), which the
	// headless tester cannot observe because its startDrag is a no-op; the
	// native gesture is verified in the real window instead.
	title, ok := tt.Find(state.sessionDisplayTitle())
	if !ok {
		t.Fatal("title bar heading was not rendered")
	}
	tt.ClickAt(title.X+title.W/2, title.Y+title.H/2)
	if menu := tt.Menu(); !slices.Contains(menu, text("titleRenameSession")) {
		t.Fatalf("pressing the title text opened %v, want the session menu", menu)
	}
	tt.CloseMenu()

	// The task panel toggle in the title chrome still works.
	shortcut := "Ctrl+Alt+B"
	if runtime.GOOS == "darwin" {
		shortcut = panelShortcutModifier() + "⌥B"
	}
	label := textArgs("titleToggleTaskPanelShortcut", map[string]any{"shortcut": shortcut})
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	if !state.tasksOpen {
		t.Fatal("the title bar task toggle did not open the task panel")
	}
}

func TestP13bNavigationResizeHandleDrags(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	state := layoutTestState(t)
	state.navCollapsed = false
	state.navWidth = navExpandedWidth
	state.preferences.Motion = string(motionReduced)
	tt := ui.NewTester(state.view, 1180, 760)

	handle, ok := tt.Find(text("navigationResize"))
	if !ok {
		t.Fatal("navigation resize handle was not rendered")
	}
	startX := handle.X + handle.W/2
	startY := handle.Y + handle.H/2
	tt.Move(startX, startY)
	if got := tt.Cursor(); got != ui.CursorResizeEW {
		t.Fatalf("cursor over the resize handle = %v, want the horizontal resize cursor", got)
	}
	tt.Press(startX, startY)
	tt.Move(startX+64, startY)
	tt.Frame()
	if got := state.navigationExpandedWidth(); got <= navExpandedWidth {
		t.Fatalf("navigation width = %v after dragging right, want more than %v", got, navExpandedWidth)
	}
	tt.Release(startX+64, startY)
	tt.Frame()

	// Dragging left narrows it again, and it never passes the limits.
	handle, ok = tt.Find(text("navigationResize"))
	if !ok {
		t.Fatal("navigation resize handle was not rendered after the drag")
	}
	startX = handle.X + handle.W/2
	tt.Press(startX, startY)
	tt.Move(startX-150, startY)
	tt.Frame()
	tt.Release(startX-150, startY)
	if got := state.navigationExpandedWidth(); got != navMinExpandedWidth {
		t.Fatalf("navigation width = %v after dragging far left, want %v", got, navMinExpandedWidth)
	}
	if state.navWidth != state.preferences.NavExpandedWidth {
		t.Fatalf("saved width = %v, want the shown %v", state.preferences.NavExpandedWidth, state.navWidth)
	}
}

func TestP13bSessionGroupsSpacingAndArchiveNotClipped(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	now := time.Now()
	state := layoutTestState(t)
	state.piSessions = nil
	state.piSessionsGroups = sessionGroups{}
	state.piSessions = []piSessionSummary{
		{Path: "/tmp/byop-p13b-pinned.jsonl", ID: "pinned", Title: "Pinned note", Updated: now},
		{Path: "/tmp/byop-p13b-today.jsonl", ID: "today", Title: "Today note", Updated: now},
		{Path: "/tmp/byop-p13b-archived-1.jsonl", ID: "archived-1", Title: "Archived one", Updated: now},
		{Path: "/tmp/byop-p13b-archived-2.jsonl", ID: "archived-2", Title: "Archived two", Updated: now},
	}
	state.preferences.SessionOrganization = map[string]map[string]sessionOrgEntry{
		canonicalPiPath(state.piCwd): {
			"/tmp/byop-p13b-pinned.jsonl":     {State: sessionStatePinned},
			"/tmp/byop-p13b-archived-1.jsonl": {State: sessionStateArchived},
			"/tmp/byop-p13b-archived-2.jsonl": {State: sessionStateArchived},
		},
	}
	state.refreshSessionOrganization(now)
	state.archivedExpanded = true
	state.piSessionsExpanded = true

	tt := ui.NewTester(state.view, 1180, 900)
	pinned, ok := tt.Find("Pinned note")
	if !ok {
		t.Fatalf("pinned session row was not rendered; texts=%v", tt.Texts())
	}
	today, ok := tt.Find("Today note")
	if !ok {
		t.Fatal("recent session row was not rendered")
	}
	if gap := today.Y - (pinned.Y + pinned.H); gap < 8 {
		t.Fatalf("gap between pinned and recent groups = %v, want at least 8", gap)
	}
	// Rows keep the token spacing above and below their text.
	if pinned.H < sessionRowHeight {
		t.Fatalf("session row height = %v, want at least %v", pinned.H, sessionRowHeight)
	}
	// Every archived row is fully visible once the section is expanded.
	for _, title := range []string{"Archived one", "Archived two"} {
		rect, ok := tt.Find(title)
		if !ok {
			t.Fatalf("archived row %q was not rendered", title)
		}
		if rect.H <= 0 {
			t.Fatalf("archived row %q has no visible height: %#v", title, rect)
		}
	}
	// The default recent group keeps one heading at most: the sidebar row names
	// it, so the list must not repeat the title above its rows.
	headings := 0
	for _, shown := range tt.Texts() {
		if shown == text("navRecentSessions") {
			headings++
		}
	}
	if headings > 1 {
		t.Fatalf("recent sessions heading rendered %d times, want at most 1", headings)
	}
	// Date groups appear once each, above their own rows.
	dates := 0
	for _, shown := range tt.Texts() {
		if shown == text("navToday") {
			dates++
		}
	}
	if dates != 1 {
		t.Fatalf("today date group rendered %d times, want 1", dates)
	}
}

func TestP13bModelListScrollsToLastRowAtMinimumWindow(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	state := layoutTestState(t)
	state.piModels = nil
	for i := 0; i < 24; i++ {
		state.piModels = append(state.piModels, piModel{
			Provider:      "opencode-go",
			ID:            "model-" + strconv.Itoa(i),
			Name:          "Model " + strings.ToUpper(string(rune('a'+i))),
			ContextWindow: 100_000 + i,
		})
	}
	state.piModelProvider = "opencode-go"
	state.piModelID = "model-0"
	state.piModelName = "Model A"
	// This test targets the Composer picker rather than the transcript HUD.
	state.chat = nil

	const width, height = 760, 560
	tt := ui.NewTester(state.view, width, height)
	if err := tt.Click(text("Model A")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if rect, ok := tt.Find(text("modelsManage")); !ok || rect.Y+rect.H > height {
		t.Fatalf("model popover footer = %#v, want it inside the %dpx window", rect, height)
	}

	first, ok := tt.Find("Model D")
	if !ok {
		t.Fatal("the model list did not render its rows")
	}
	tt.Scroll(first.X+first.W/2, first.Y+first.H/2, 0, 4000)
	tt.Frame()
	last, ok := tt.Find("Model X")
	if !ok || last.H <= 0 {
		t.Fatalf("last model row = %#v, want it reachable by scrolling", last)
	}
	if last.Y < 0 || last.Y+last.H > height {
		t.Fatalf("last model row is outside the %dpx window: %#v", height, last)
	}
}
