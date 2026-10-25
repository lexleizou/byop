package main

import (
	"testing"

	"byop/locales"

	"github.com/egoist/mygo/ui"
)

func settingsPageV12TestState(t *testing.T) (*appState, *SettingsPageState) {
	t.Helper()
	state := layoutTestState(t)
	state.composerDraft = "Keep this draft."
	page := &SettingsPageState{
		ChannelContent: func(c *ui.Context, p palette) ui.Element {
			return ui.Text(c, "Injected channel settings")
		},
	}
	return state, page
}

func renderSettingsPageV12(
	t *testing.T,
	state *appState,
	page *SettingsPageState,
	width, height int,
) *ui.Tester {
	t.Helper()
	return ui.NewTester(func(c *ui.Context) {
		theme, palette := nativeThemeWithContrast(false, false)
		c.SetTheme(theme)
		state.settingsPageV12(c, palette, page)
	}, width, height)
}

func TestSettingsPageV12SearchFindsLocalizedCategories(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		category     settingsCategoryID
		categoryText string
	}{
		{
			name:         "English setting keyword",
			query:        "contrast",
			category:     settingsCategoryGeneral,
			categoryText: "General",
		},
		{
			name:         "English category keyword",
			query:        "workspace",
			category:     settingsCategoryWorkspace,
			categoryText: "Workspace",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state, page := settingsPageV12TestState(t)
			page.Search = test.query
			tt := renderSettingsPageV12(t, state, page, 1100, 900)

			label := textArgs("settingsSearchOpenCategory", map[string]any{
				"category": test.categoryText,
			})
			if _, ok := tt.Find(label); !ok {
				t.Fatalf("search result %q was not rendered; texts=%v", label, tt.Texts())
			}
		})
	}
}

func TestSettingsPageV12LayoutUsesMainContentWidth(t *testing.T) {
	tests := []struct {
		name         string
		navCollapsed bool
		tasksOpen    bool
		windowWidth  float32
		wantWidth    float32
		wantNarrow   bool
	}{
		{
			name:        "expanded navigation at breakpoint",
			windowWidth: 984,
			wantWidth:   settingsPageNarrowWidthV12,
			wantNarrow:  false,
		},
		{
			name:        "expanded navigation below breakpoint",
			windowWidth: 983,
			wantWidth:   settingsPageNarrowWidthV12 - 1,
			wantNarrow:  true,
		},
		{
			name:         "collapsed navigation frees content width",
			navCollapsed: true,
			windowWidth:  900,
			wantWidth:    780,
			wantNarrow:   false,
		},
		{
			name:        "task panel narrows content",
			tasksOpen:   true,
			windowWidth: 1100,
			wantWidth:   508,
			wantNarrow:  true,
		},
		{
			name:        "large content stops at measured maximum",
			windowWidth: 2000,
			wantWidth:   settingsPageMaxWidthV12,
			wantNarrow:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := &appState{
				navCollapsed: test.navCollapsed,
				tasksOpen:    test.tasksOpen,
			}
			got := state.settingsContentWidthV12(test.windowWidth)
			if got != test.wantWidth {
				t.Fatalf("settings content width = %v, want %v", got, test.wantWidth)
			}
			if narrow := got < settingsPageNarrowWidthV12; narrow != test.wantNarrow {
				t.Fatalf("narrow = %t, want %t for content width %v", narrow, test.wantNarrow, got)
			}
		})
	}
}

func TestSettingsPageV12SearchResultAndEnterKeepDraft(t *testing.T) {
	t.Run("click result", func(t *testing.T) {
		state, page := settingsPageV12TestState(t)
		tt := renderSettingsPageV12(t, state, page, 1100, 900)
		if err := tt.Click(text("settingsSearchPlaceholder")); err != nil {
			t.Fatal(err)
		}
		tt.Type("models.json")
		label := textArgs("settingsSearchOpenCategory", map[string]any{
			"category": text("settingsCategoryModels"),
		})
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		if page.ActiveCategory != settingsCategoryModels || page.Search != "" {
			t.Fatalf("search result selected %q with query %q", page.ActiveCategory, page.Search)
		}
		if state.composerDraft != "Keep this draft." {
			t.Fatalf("clicking search result changed composer draft to %q", state.composerDraft)
		}
		if _, ok := tt.Find("Injected channel settings"); !ok {
			t.Fatal("models category did not render the injected channel content")
		}
	})

	t.Run("press Enter", func(t *testing.T) {
		state, page := settingsPageV12TestState(t)
		tt := renderSettingsPageV12(t, state, page, 1100, 900)
		if err := tt.Click(text("settingsSearchPlaceholder")); err != nil {
			t.Fatal(err)
		}
		tt.Type("motion")
		tt.Key(0, ui.KeyEnter)
		if page.ActiveCategory != settingsCategoryGeneral || page.Search != "" {
			t.Fatalf("Enter selected %q with query %q", page.ActiveCategory, page.Search)
		}
		if state.composerDraft != "Keep this draft." {
			t.Fatalf("Enter search changed composer draft to %q", state.composerDraft)
		}
	})
}

func TestSettingsPageV12NavigationAboutAndBackKeepDraft(t *testing.T) {
	state, page := settingsPageV12TestState(t)
	tt := renderSettingsPageV12(t, state, page, 1100, 900)

	if err := tt.Click(text("settingsCategoryWorkspace")); err != nil {
		t.Fatal(err)
	}
	if page.ActiveCategory != settingsCategoryWorkspace {
		t.Fatalf("category = %q, want workspace", page.ActiveCategory)
	}

	if err := tt.Click(text("settingsCategoryAbout")); err != nil {
		t.Fatal(err)
	}
	if !page.AboutOpen || page.ActiveCategory == settingsCategoryAbout {
		t.Fatalf("About must be a separate bottom entry; active=%q open=%t",
			page.ActiveCategory, page.AboutOpen)
	}
	if _, ok := tt.Find(text("settingsBuild") + " " + appBuildVersion()); !ok {
		t.Fatal("About content did not open")
	}
	aboutRect, _ := tt.Find(text("settingsCategoryAbout"))
	lastCategoryRect, _ := tt.Find(text("settingsCategoryData"))
	if aboutRect.Y <= lastCategoryRect.Y+lastCategoryRect.H {
		t.Fatalf("About must stay below the main categories; about=%#v last=%#v",
			aboutRect, lastCategoryRect)
	}

	if err := tt.Click(text("settingsCategoryGeneral")); err != nil {
		t.Fatal(err)
	}
	if page.AboutOpen || page.ActiveCategory != settingsCategoryGeneral {
		t.Fatalf("returning to General left active=%q open=%t",
			page.ActiveCategory, page.AboutOpen)
	}

	if err := tt.Click(text("settingsBackToChat")); err != nil {
		t.Fatal(err)
	}
	if state.page != pageChat {
		t.Fatalf("back action opened %q, want chat", state.page)
	}
	if state.composerDraft != "Keep this draft." {
		t.Fatalf("settings actions changed composer draft to %q", state.composerDraft)
	}
}

func TestSettingsPageV12AppearanceHasNoDurationChoices(t *testing.T) {
	state, page := settingsPageV12TestState(t)
	page.ActiveCategory = settingsCategoryAppearance
	tt := renderSettingsPageV12(t, state, page, 1100, 900)

	for _, forbidden := range []string{"0 ms", "150 ms", "250 ms", "400 ms"} {
		if tt.HasText(forbidden) {
			t.Fatalf("appearance rendered duration choice %q", forbidden)
		}
	}
	for _, label := range []string{
		text("settingsAppearanceTitle"),
		text("settingsThemeTitle"),
		text("settingsWallpaperTitle"),
		text("settingsNavTransparencyTitle"),
		text("settingsGlassBlurTitle"),
		text("settingsGlassRefractionTitle"),
		text("settingsReduceTransparencyTitle"),
	} {
		if _, ok := tt.Find(label); !ok {
			t.Fatalf("appearance did not render %q", label)
		}
	}
	for _, label := range []string{
		text("settingsContrastTitle"),
		text("settingsMotionTitle"),
	} {
		if _, ok := tt.Find(label); ok {
			t.Fatalf("appearance rendered moved control %q", label)
		}
	}
}

func TestSettingsPageV12GeneralCardsUseGroupSpacing(t *testing.T) {
	state, page := settingsPageV12TestState(t)
	tt := renderSettingsPageV12(t, state, page, 1100, 900)

	language, ok := tt.Find(locales.SettingsLanguageTitle())
	if !ok {
		t.Fatal("language card was not rendered")
	}
	shortcuts, ok := tt.Find(text("settingsShortcutsTitle"))
	if !ok {
		t.Fatal("shortcut card was not rendered")
	}
	if gap := shortcuts.Y - (language.Y + language.H); gap < 16 {
		t.Fatalf("General card visual gap = %.1f, want at least 16; language=%#v shortcuts=%#v",
			gap, language, shortcuts)
	}
}

func TestSettingsPageV12CategoryLabelsShareLeftEdge(t *testing.T) {
	state, page := settingsPageV12TestState(t)
	tt := renderSettingsPageV12(t, state, page, 1100, 900)

	first, ok := tt.Find(text("settingsCategoryGeneral"))
	if !ok {
		t.Fatal("General category was not rendered")
	}
	for _, spec := range settingsCategorySpecs {
		if spec.id == settingsCategoryGeneral || spec.id == settingsCategoryAbout {
			continue
		}
		label, ok := tt.Find(text(spec.labelKey))
		if !ok {
			t.Fatalf("category %q was not rendered", spec.labelKey)
		}
		if delta := label.X - first.X; delta < -0.5 || delta > 0.5 {
			t.Fatalf("category %q starts at x=%.1f, General starts at x=%.1f",
				spec.labelKey, label.X, first.X)
		}
	}
}

func TestSettingsPageV12DataSearchOwnsPiAndCLI(t *testing.T) {
	for _, query := range []string{"Pi", "CLI", "version", "reconnect", "connect"} {
		if match, ok := (&appState{}).settingsFirstSearchMatch(query); !ok ||
			match.id != settingsCategoryData {
			t.Fatalf("search %q matched %#v, want Data first", query, match)
		}
	}
}

func TestSettingsPageV12NarrowNavigationWrapsAndContentStaysVisible(t *testing.T) {
	state, page := settingsPageV12TestState(t)
	tt := renderSettingsPageV12(t, state, page, 680, 900)

	first, ok := tt.Find(text("settingsCategoryGeneral"))
	if !ok {
		t.Fatal("first category was not rendered")
	}
	wrapped := false
	for _, spec := range settingsCategorySpecs {
		if spec.id == settingsCategoryAbout {
			continue
		}
		rect, found := tt.Find(text(spec.labelKey))
		if !found {
			t.Fatalf("narrow category %q was not rendered", spec.labelKey)
		}
		if rect.H <= 0 || rect.W <= 0 {
			t.Fatalf("narrow category %q has no visible size: %#v", spec.labelKey, rect)
		}
		if rect.Y > first.Y+first.H/2 {
			wrapped = true
		}
	}
	if !wrapped {
		t.Fatalf("narrow category buttons did not wrap; first=%#v", first)
	}
	if _, ok := tt.Find(text("settingsLanguageTitle")); !ok {
		t.Fatal("narrow layout did not render General content below the category bar")
	}
}

func TestSettingsPageV12UsesActualContentWidth(t *testing.T) {
	const width = 900

	t.Run("expanded navigation wraps categories", func(t *testing.T) {
		state, page := settingsPageV12TestState(t)
		state.navCollapsed = false
		tt := renderSettingsPageV12(t, state, page, width, 900)
		general, _ := tt.Find(text("settingsCategoryGeneral"))
		appearance, _ := tt.Find(text("settingsCategoryAppearance"))
		if appearance.Y > general.Y+general.H/2 {
			t.Fatalf("categorized navigation did not wrap at settings width %v; general=%#v appearance=%#v",
				state.settingsContentWidthV12(width), general, appearance)
		}
	})

	t.Run("collapsed navigation keeps side navigation", func(t *testing.T) {
		state, page := settingsPageV12TestState(t)
		state.navCollapsed = true
		tt := renderSettingsPageV12(t, state, page, width, 900)
		general, _ := tt.Find(text("settingsCategoryGeneral"))
		appearance, _ := tt.Find(text("settingsCategoryAppearance"))
		if appearance.Y <= general.Y+general.H/2 {
			t.Fatalf("side navigation did not stack at settings width %v; general=%#v appearance=%#v",
				state.settingsContentWidthV12(width), general, appearance)
		}
	})
}

func TestSettingsPageV12NarrowAboutIsBottomAndStartsClosed(t *testing.T) {
	state, page := settingsPageV12TestState(t)
	tt := renderSettingsPageV12(t, state, page, 900, 900)

	about, ok := tt.Find(text("settingsCategoryAbout"))
	if !ok {
		t.Fatal("narrow About entry was not rendered")
	}
	data, ok := tt.Find(text("settingsCategoryData"))
	if !ok {
		t.Fatal("narrow data category was not rendered")
	}
	if about.Y <= data.Y+data.H {
		t.Fatalf("narrow About entry is not at the bottom; about=%#v data=%#v", about, data)
	}
	if page.AboutOpen {
		t.Fatal("About started open")
	}
	if err := tt.Click(text("settingsCategoryAbout")); err != nil {
		t.Fatal(err)
	}
	if !page.AboutOpen {
		t.Fatal("clicking narrow About did not open it")
	}
}

func TestSettingsPageV12ModelsRendersChannelRenderer(t *testing.T) {
	state, page := settingsPageV12TestState(t)
	page.ActiveCategory = settingsCategoryModels
	tt := renderSettingsPageV12(t, state, page, 1100, 900)

	if _, ok := tt.Find("Injected channel settings"); !ok {
		t.Fatal("models category did not call its channel settings renderer")
	}
}

func TestSettingsSearchMatchesCurrentLocaleAndAliases(t *testing.T) {
	en := map[string]string{
		"settingsCategoryGeneral":      "General",
		"settingsCategoryAppearance":   "Appearance",
		"settingsCategoryModels":       "Models and channels",
		"settingsCategoryConversation": "Conversation",
		"settingsCategoryWorkspace":    "Workspace",
		"settingsCategoryExtensions":   "Extensions",
		"settingsCategoryData":         "Data and diagnostics",
		"settingsCategoryAbout":        "About",
		"settingsLanguageTitle":        "Language",
		"settingsShortcutsTitle":       "Keyboard shortcuts",
		"settingsAppearanceTitle":      "Appearance",
		"settingsContrastTitle":        "Contrast",
		"settingsMotionTitle":          "Motion",
		"settingsPiTitle":              "External Pi CLI",
		"settingsFollowUpTitle":        "Follow-up suggestion",
		"chatFollowUpHint":             "Use Tab",
		"navWorkspace":                 "Workspace",
		"settingsWorkspaceTitle":       "Workspace",
		"settingsPluginSettings":       "Plugin settings",
		"settingsExtensionsTitle":      "Extensions",
		"settingsDiagnosticsTitle":     "Diagnostics",
		"settingsCliPath":              "CLI path",
		"legacyReimport":               "Re-import legacy data",
		"settingsAboutDescription":     "Product information",
	}
	zh := map[string]string{
		"settingsCategoryGeneral":      "通用",
		"settingsCategoryAppearance":   "外观",
		"settingsCategoryModels":       "模型与渠道",
		"settingsCategoryConversation": "对话",
		"settingsCategoryWorkspace":    "工作区",
		"settingsCategoryExtensions":   "扩展",
		"settingsCategoryData":         "数据与诊断",
		"settingsCategoryAbout":        "关于",
		"settingsLanguageTitle":        "界面语言",
		"settingsShortcutsTitle":       "快捷键",
		"settingsAppearanceTitle":      "外观",
		"settingsContrastTitle":        "对比度",
		"settingsMotionTitle":          "动效",
		"settingsPiTitle":              "外部 Pi CLI",
		"settingsFollowUpTitle":        "后续建议",
		"chatFollowUpHint":             "Tab 使用",
		"navWorkspace":                 "工作区",
		"settingsWorkspaceTitle":       "工作区",
		"settingsPluginSettings":       "插件设置",
		"settingsExtensionsTitle":      "扩展",
		"settingsDiagnosticsTitle":     "诊断",
		"settingsCliPath":              "CLI 路径",
		"legacyReimport":               "重新导入旧版数据",
		"settingsAboutDescription":     "产品信息",
	}

	localizer := func(messages map[string]string) func(string) string {
		return func(key string) string {
			if value := messages[key]; value != "" {
				return value
			}
			return key
		}
	}
	tests := []struct {
		query    string
		messages map[string]string
		want     settingsCategoryID
	}{
		{"workspace", en, settingsCategoryWorkspace},
		{"工作区", zh, settingsCategoryWorkspace},
		{"contrast", en, settingsCategoryGeneral},
		{"对比度", zh, settingsCategoryGeneral},
		{"plugin", en, settingsCategoryExtensions},
		{"插件", zh, settingsCategoryExtensions},
		{"diagnostics", en, settingsCategoryData},
		{"诊断", zh, settingsCategoryData},
		{"models.json", en, settingsCategoryModels},
		{"API", en, settingsCategoryModels},
		{"MCP", zh, settingsCategoryExtensions},
	}
	for _, test := range tests {
		matches := settingsSearchMatchesWithText(test.query, localizer(test.messages))
		if len(matches) == 0 || matches[0].id != test.want {
			t.Fatalf("query %q matched %#v, want first %q", test.query, matches, test.want)
		}
	}
}

func TestSettingsPageV12IconsExist(t *testing.T) {
	for _, name := range []string{
		"settings", "sun", "activity", "chat", "folder", "plugins",
		"database", "info", "corner-up-left", "search", "monitor",
		"moon", "contrast", "stop", "document", "check",
	} {
		if iconSet[name] == nil {
			t.Fatalf("settings page uses unknown icon %q", name)
		}
	}
}
