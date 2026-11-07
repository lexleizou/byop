package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestP31BoardUIGlassAndWallpaperOff pins §4: BoardUI draws no wallpaper,
// hides the wallpaper and glass rows in the settings page, shows no glass
// status and builds no sidebar glass button; BYOP keeps all of them.
func TestP31BoardUIGlassAndWallpaperOff(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	boardUIState := layoutTestState(t)
	boardUIState.preferences.AppearancePreset = string(appearancePresetBoardUI)
	if got := boardUIState.activeWallpaper(); got != "none" {
		t.Fatalf("BoardUI active wallpaper = %q, want none", got)
	}
	if glassAllowed(boardUIThemePalette(false, false, false)) {
		t.Fatal("glassAllowed must be false for BoardUI")
	}

	byopState := layoutTestState(t)
	byopState.preferences.Wallpaper = "ember"
	if got := byopState.activeWallpaper(); got != "ember" {
		t.Fatalf("BYOP active wallpaper = %q, want ember", got)
	}

	// Settings page: BoardUI hides the wallpaper row and the three glass rows.
	boardUIPage := renderSettingsPageForPreset(t, boardUIState, appearancePresetBoardUI, 1100, 900)
	for _, key := range []string{
		"settingsWallpaperTitle",
		"settingsNavTransparencyTitle",
		"settingsGlassBlurTitle",
		"settingsGlassRefractionTitle",
	} {
		if boardUIPage.HasText(text(key)) {
			t.Fatalf("BoardUI settings page shows %q", text(key))
		}
	}
	if status := boardUIState.glassStatusText(); status != "" {
		t.Fatalf("BoardUI glass status = %q, want empty", status)
	}
	if boardUIPage.HasText(text("settingsGlassDarkOnly")) {
		t.Fatal("BoardUI settings page shows the glass status line")
	}

	// BYOP keeps the wallpaper and glass rows on its own settings page.
	byopPage := renderSettingsPageForPreset(t, byopState, appearancePresetBYOP, 1100, 900)
	for _, key := range []string{
		"settingsWallpaperTitle",
		"settingsNavTransparencyTitle",
		"settingsGlassBlurTitle",
		"settingsGlassRefractionTitle",
	} {
		if !byopPage.HasText(text(key)) {
			t.Fatalf("BYOP settings page lost %q", text(key))
		}
	}

	// Sidebar footer: BoardUI builds no glass button; BYOP with active liquid
	// glass does.
	boardUIFooter := ui.NewTester(func(c *ui.Context) {
		theme, p := nativeThemeForAppearance(appearancePresetBoardUI, true, false, false)
		c.SetTheme(theme)
		ui.Column(c).Width(400).Children(func() {
			boardUIState.navigationFooter(c, p, true, false, false)
		})
	}, 400, 640)
	boardUIFooter.Frame()
	if boardUIFooter.HasText(text("navGlassButton")) {
		t.Fatal("BoardUI sidebar built the glass button")
	}

	byopFooter := ui.NewTester(func(c *ui.Context) {
		theme, p := nativeThemeForAppearance(appearancePresetBYOP, true, false, false)
		c.SetTheme(theme)
		ui.Column(c).Width(400).Children(func() {
			byopState.navigationFooter(c, p, true, false, false)
		})
	}, 400, 640)
	byopFooter.Frame()
	if !byopFooter.HasText(text("navGlassButton")) {
		t.Fatal("BYOP sidebar lost the glass button under active liquid glass")
	}
}

// renderSettingsPageForPreset renders the settings page with the palette of
// the given preset at dark so the appearance body renders its cards.
func renderSettingsPageForPreset(
	t *testing.T,
	state *appState,
	preset appearancePreset,
	width, height int,
) *ui.Tester {
	t.Helper()
	page := &SettingsPageState{ActiveCategory: settingsCategoryAppearance}
	return ui.NewTester(func(c *ui.Context) {
		theme, p := nativeThemeForAppearance(preset, true, false, false)
		c.SetTheme(theme)
		state.settingsPageV12(c, p, page)
	}, width, height)
}

// TestP31BoardUIMetricsAndSpacing pins §3: the BoardUI metric values, the
// chat main width math reading the 12px spacing, and the navigation column
// shifting right by exactly the spacing difference against BYOP.
func TestP31BoardUIMetricsAndSpacing(t *testing.T) {
	boardMetrics := metricsForAppearancePreset(appearancePresetBoardUI)
	if boardMetrics.shellSpacing != 12 || boardMetrics.mainRadius != 24 ||
		boardMetrics.composerRadius != 24 || boardMetrics.userBubbleRadius != 16 {
		t.Fatalf("BoardUI metrics = %#v", boardMetrics)
	}
	byopMetrics := metricsForAppearancePreset(appearancePresetBYOP)
	if byopMetrics.shellSpacing != shellGap || byopMetrics.mainRadius != shellRadius ||
		byopMetrics.composerRadius != runtimeTokens.Dimension.Radius["input"].Value ||
		byopMetrics.userBubbleRadius != 0 {
		t.Fatalf("BYOP metrics moved: %#v", byopMetrics)
	}

	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	byopState := layoutTestState(t)
	byopState.navCollapsed = false
	byopState.navWidth = navExpandedWidth
	boardState := layoutTestState(t)
	boardState.navCollapsed = false
	boardState.navWidth = navExpandedWidth
	boardState.preferences.AppearancePreset = string(appearancePresetBoardUI)

	byopMain := byopState.chatMainWidth(1180)
	boardMain := boardState.chatMainWidth(1180)
	// BoardUI: two 12px outer gaps plus the 12px nav-to-main gap (nav width
	// unchanged), BYOP keeps its 8px triple.
	if want := float32(1180) - 3*boardMetrics.shellSpacing - navExpandedWidth; boardMain != want {
		t.Fatalf("BoardUI chat main width = %v, want %v at 1180px", boardMain, want)
	}
	if want := float32(1180) - 3*byopMetrics.shellSpacing - navExpandedWidth; byopMain != want {
		t.Fatalf("BYOP chat main width = %v, want %v at 1180px", byopMain, want)
	}

	byopNav := ui.NewTester(byopState.view, 1180, 760)
	byopNav.Frame()
	byopChat, ok := byopNav.Find(text("navChat"))
	if !ok {
		t.Fatal("BYOP navigation did not render the chat item")
	}
	boardNav := ui.NewTester(boardState.view, 1180, 760)
	boardNav.Frame()
	boardChat, ok := boardNav.Find(text("navChat"))
	if !ok {
		t.Fatal("BoardUI navigation did not render the chat item")
	}
	if diff := boardChat.X - byopChat.X; diff < 3.5 || diff > 4.5 {
		t.Fatalf("navigation moved by %v, want the 4px spacing difference (8→12)", diff)
	}
}

// boarduiDarkView forces the dark appearance for state.view: the headless
// tester cannot drive mygo.Theme, so the appearance seams return dark.
func boarduiDarkView(t *testing.T) func() {
	t.Helper()
	restore := stableTestAppearance(t)
	t.Cleanup(restore)
	oldDark := currentAppearanceDark
	oldHigh := currentAppearanceHighContrast
	oldReduce := currentAppearanceReduceTransparency
	currentAppearanceDark = func() bool { return true }
	currentAppearanceHighContrast = func() bool { return false }
	currentAppearanceReduceTransparency = func() bool { return false }
	return func() {
		currentAppearanceDark = oldDark
		currentAppearanceHighContrast = oldHigh
		currentAppearanceReduceTransparency = oldReduce
	}
}

// TestP31ComposerCardRadiusFollowsPreset renders the real chat page with the
// merged Composer (§11.5) and probes the card corner: with BoardUI's 24px
// radius the point 4px inside the corner is outside the card, while BYOP's
// 8px radius keeps it inside. Reverting ui_v12.go to v12InputRadius fails it.
func TestP31ComposerCardRadiusFollowsPreset(t *testing.T) {
	restore := boarduiDarkView(t)
	defer restore()

	cornerDiff := func(preset appearancePreset) int {
		t.Helper()
		state := layoutTestState(t)
		state.preferences.AppearancePreset = string(preset)
		state.navCollapsed = false
		tt := ui.NewTester(state.view, 1180, 760)
		tt.Frame()
		input, ok := tt.Find(text("chatMessageLabel"))
		if !ok {
			t.Fatalf("%v: composer input was not rendered", preset)
		}
		cx, cy := int(input.X-12), int(input.Y-12)
		img := tt.Image()
		r1, g1, b1 := boarduiPixel(img, cx+4, cy+4, 1180)
		r2, g2, b2 := boarduiPixel(img, cx+30, cy+30, 1180)
		return max(max(boarduiDiff(r1, r2), boarduiDiff(g1, g2)), boarduiDiff(b1, b2))
	}
	if got := cornerDiff(appearancePresetBoardUI); got < 8 {
		t.Fatalf("BoardUI composer corner blends with the card (diff %d), want a 24px radius", got)
	}
	if got := cornerDiff(appearancePresetBYOP); got > 3 {
		t.Fatalf("BYOP composer corner left the 8px card (diff %d)", got)
	}
}

// TestP31TitleHeadingPrefersTitle pins §8 for all three presets: with a long
// workspace path and a short title the title renders whole and the path shows
// only its last component (the full path keeps living in the tooltip).
func TestP31TitleHeadingPrefersTitle(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	workspace := filepath.Join("/Users", "someone", "a-very-long-workspace-directory-name-that-keeps-going-and-going")
	for _, preset := range []appearancePreset{
		appearancePresetBYOP,
		appearancePresetBoardUI,
		appearancePresetJAgent,
	} {
		state := layoutTestState(t)
		state.hud.cwd = workspace
		state.piSessionName = "Refactor parser"
		state.page = pageChat
		title := state.sessionDisplayTitle()
		var measured float32
		tt := ui.NewTester(func(c *ui.Context) {
			theme, p := nativeThemeForAppearance(preset, false, false, false)
			c.SetTheme(theme)
			state.titleBarHeading(c, p)
			measured, _ = c.MeasureText(0, ui.Span{Text: title, Size: p.bodyFont})
		}, 760, 60)
		tt.Frame()
		rendered, ok := tt.Find(title)
		if !ok {
			t.Fatalf("%v: title was not rendered", preset)
		}
		base := filepath.Base(workspace)
		if _, ok := tt.Find("· " + base); !ok {
			t.Fatalf("%v: path segment %q was not rendered", preset, "· "+base)
		}
		if _, ok := tt.Find(workspace); ok {
			t.Fatalf("%v: the full workspace path leaked into the title bar", preset)
		}
		if _, ok := tt.Find("/Users/someone/workspaces"); ok {
			t.Fatalf("%v: a path prefix leaked into the title bar", preset)
		}
		maxWidth := state.titleCapsuleMaxWidth(760)
		if rendered.W < measured-1 {
			t.Fatalf("%v: title truncated to %v, measured %v", preset, rendered.W, measured)
		}
		if rendered.W > maxWidth*0.6+0.5 {
			t.Fatalf("%v: title width %v exceeds 60%% of the capsule %v", preset, rendered.W, maxWidth)
		}
	}
}

// TestP31TitleHeadingKeepsHomeTilde pins §8's tilde rule: a workspace that
// displays as "~" shows the segment "· ~", not a basename of the home path.
func TestP31TitleHeadingKeepsHomeTilde(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	state := layoutTestState(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	state.hud.cwd = home
	state.piCwd = home
	state.preferences.Workspace = home
	state.page = pageChat
	tt := ui.NewTester(func(c *ui.Context) {
		theme, p := nativeThemeForAppearance(appearancePresetBoardUI, false, false, false)
		c.SetTheme(theme)
		state.titleBarHeading(c, p)
	}, 1100, 60)
	tt.Frame()
	if _, ok := tt.Find("· ~"); !ok {
		t.Fatal(`home workspace did not render the "~" segment`)
	}
	if base := filepath.Base(home); base != "~" {
		if _, ok := tt.Find("· " + base); ok {
			t.Fatal(`home workspace rendered a basename instead of "~"`)
		}
	}
}

// boarduiStylePixels renders a 120×80 card styled by apply on a white root
// with the dark BoardUI palette, and returns the interior pixel, the pixel
// 2px below the bottom edge (the shadow band) and the far background pixel.
func boarduiStylePixels(
	t *testing.T,
	apply func(el ui.Element, p palette, radius float32) ui.Element,
	radius float32,
) (interior, below, far [3]uint8) {
	t.Helper()
	tt := ui.NewTester(func(c *ui.Context) {
		theme, p := nativeThemeForAppearance(appearancePresetBoardUI, true, false, false)
		c.SetTheme(theme)
		c.Root().Background(ui.RGB(255, 255, 255))
		ui.Column(c).Fill().Padding(30).Children(func() {
			apply(ui.Box(c).Width(120).Height(80), p, radius)
		})
	}, 200, 160)
	tt.Frame()
	img := tt.Image()
	interior = [3]uint8(boarduiPixel3(img, 50, 50, 200))
	below = [3]uint8(boarduiPixel3(img, 90, 112, 200))
	far = [3]uint8(boarduiPixel3(img, 90, 150, 200))
	return interior, below, far
}

func boarduiMaxDiff(a, b [3]uint8) int {
	return max(max(boarduiDiff(a[0], b[0]), boarduiDiff(a[1], b[1])), boarduiDiff(a[2], b[2]))
}

// TestP31BoardUIRaisedAndComposerSurfaceStyles pins §5's opaque raised and
// composer cards: BoardUI's surface color plus the (0,1,2,0) shadow. Deleting
// the BoardUI branch sends these calls down BYOP's path, which paints no
// shadow on the same palette, so the shadow band assertion fails then.
func TestP31BoardUIRaisedAndComposerSurfaceStyles(t *testing.T) {
	for name, apply := range map[string]func(el ui.Element, p palette, radius float32) ui.Element{
		"raised":   styleRaisedSurface,
		"composer": styleComposerSurface,
	} {
		interior, below, far := boarduiStylePixels(t, apply, 24)
		if boarduiMaxDiff(interior, [3]uint8{0x26, 0x26, 0x26}) > 4 {
			t.Fatalf("%s: interior %v is not the BoardUI dark surface #262626", name, interior)
		}
		if d := boarduiMaxDiff(below, far); d < 3 || d > 70 {
			t.Fatalf("%s: shadow band vs background diff %d, want the subtle .18 shadow", name, d)
		}
	}
}

// TestP31BoardUIPopoverSurfaceStyles pins §5's opaque popover: interior
// #262626, 0.5px border, and the .14+.10 shadow stack. Deleting the BoardUI
// branch runs BYOP's glass path, whose translucent tint (#222221b8 over
// white) and 0.72-alpha stage shadow both miss these assertions.
func TestP31BoardUIPopoverSurfaceStyles(t *testing.T) {
	interior, below, far := boarduiStylePixels(t, stylePopoverGlass, 12)
	if boarduiMaxDiff(interior, [3]uint8{0x26, 0x26, 0x26}) > 4 {
		t.Fatalf("popover interior %v is not the opaque BoardUI dark tint #262626", interior)
	}
	if d := boarduiMaxDiff(below, far); d < 4 || d > 70 {
		t.Fatalf("popover shadow band vs background diff %d, want the .14+.10 stack", d)
	}
}

// TestReviewBYOPKeepsGlassButtonInEveryMode pins the review fix: BYOP builds
// the navigation glass button in light, high-contrast and reduced-
// transparency modes, as 0.23.0 did; only the BoardUI preset omits it.
func TestReviewBYOPKeepsGlassButtonInEveryMode(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	for _, m := range []struct{ dark, high, reduce bool }{{false, false, false}, {true, true, false}, {true, false, true}} {
		state := layoutTestState(t)
		tt := ui.NewTester(func(c *ui.Context) {
			theme, p := nativeThemeForAppearance(appearancePresetBYOP, m.dark, m.high, m.reduce)
			c.SetTheme(theme)
			ui.Column(c).Width(400).Children(func() { state.navigationFooter(c, p, m.dark, m.high, m.reduce) })
		}, 400, 640)
		tt.Frame()
		if !tt.HasText(text("navGlassButton")) {
			t.Errorf("BYOP %+v lost the navigation glass button (0.23.0 always built it)", m)
		}
	}
}
