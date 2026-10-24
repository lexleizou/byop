package main

import (
	"runtime"
	"strings"

	"byop/locales"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
)

type settingsCategoryID string

const (
	settingsCategoryGeneral      settingsCategoryID = "general"
	settingsCategoryAppearance   settingsCategoryID = "appearance"
	settingsCategoryModels       settingsCategoryID = "models"
	settingsCategoryConversation settingsCategoryID = "conversation"
	settingsCategoryWorkspace    settingsCategoryID = "workspace"
	settingsCategoryExtensions   settingsCategoryID = "extensions"
	settingsCategoryData         settingsCategoryID = "data"
	settingsCategoryAbout        settingsCategoryID = "about"
)

// SettingsPageState is owned by appState. Keeping it separate lets the page
// preserve its selection and query while the surrounding shell redraws.
type SettingsPageState struct {
	ActiveCategory settingsCategoryID
	Search         string
	AboutOpen      bool
	// ChannelContent is an optional test seam. Production leaves it nil and
	// renders the channel backend's settings entry directly.
	ChannelContent func(c *ui.Context, p palette) ui.Element
}

type settingsCategorySpec struct {
	id       settingsCategoryID
	labelKey string
	iconName string
}

var settingsCategorySpecs = []settingsCategorySpec{
	{settingsCategoryGeneral, "settingsCategoryGeneral", "settings"},
	{settingsCategoryAppearance, "settingsCategoryAppearance", "sun"},
	{settingsCategoryModels, "settingsCategoryModels", "activity"},
	{settingsCategoryConversation, "settingsCategoryConversation", "chat"},
	{settingsCategoryWorkspace, "settingsCategoryWorkspace", "folder"},
	{settingsCategoryExtensions, "settingsCategoryExtensions", "plugins"},
	{settingsCategoryData, "settingsCategoryData", "database"},
	{settingsCategoryAbout, "settingsCategoryAbout", "info"},
}

func (s *appState) settingsPageV12(c *ui.Context, p palette, state *SettingsPageState) {
	if state == nil {
		state = &SettingsPageState{}
	}
	active := s.settingsPageActiveCategory(state)
	narrow := s.settingsPageNarrowV12(c)

	ui.Scroll(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Justify(ui.Center).Padding(24, 20, 32, 20).Children(func() {
			ui.Column(c).Grow(1).MaxWidth(1040).Gap(16).Children(func() {
				s.settingsPageHeaderV12(c, p)
				s.settingsSearchV12(c, p, state)
				if narrow {
					s.settingsNarrowLayoutV12(c, p, state, active)
				} else {
					s.settingsWideLayoutV12(c, p, state, active)
				}
			})
		})
	})
}

const (
	settingsPageHorizontalPaddingV12 float32 = 20
	settingsPageMaxWidthV12          float32 = 1040
	settingsPageNarrowWidthV12       float32 = 720
)

func (s *appState) settingsPageNarrowV12(c *ui.Context) bool {
	windowWidth, _ := c.Size()
	return s.settingsContentWidthV12(windowWidth) < settingsPageNarrowWidthV12
}

func (s *appState) settingsContentWidthV12(windowWidth float32) float32 {
	available := max(0, s.chatMainWidth(windowWidth)-2*settingsPageHorizontalPaddingV12)
	return min(settingsPageMaxWidthV12, available)
}

func (s *appState) settingsPageHeaderV12(c *ui.Context, p palette) {
	ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
		backLabel := text("settingsBackToChat")
		back := ui.ButtonBase(c.Key("settings-back-chat")).Padding(7, 10).
			Radius(runtimeTokens.Dimension.Radius["input"].Value).
			Border(runtimeTokens.Dimension.Border.Value, p.border).Background(p.surface).
			Cursor(ui.CursorPointer).Label(backLabel)
		if back.Hovered() {
			back.Background(p.surfaceHover)
		}
		back.Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icon("corner-up-left")).Size(14, 14).TextColor(p.muted)
				ui.Text(c, backLabel).FontSize(fontBody).FontWeight(600).TextColor(p.text)
			})
		})
		if back.Clicked() {
			s.setPage(pageChat)
		}
		ui.Text(c, text("settingsTitle")).FontSize(fontTitle).FontWeight(650).
			TextColor(p.text).Grow(1)
	})
}

func (s *appState) settingsSearchV12(c *ui.Context, p palette, state *SettingsPageState) {
	placeholder := text("settingsSearchPlaceholder")
	ui.Row(c).Height(36).Gap(7).Padding(0, 10).
		Radius(runtimeTokens.Dimension.Radius["input"].Value).
		Border(runtimeTokens.Dimension.Border.Value, p.border).
		Background(p.surface).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icon("search")).Size(15, 15).TextColor(p.muted)
		input := ui.TextInputBase(c.Key("settings-search"), &state.Search).Grow(1).
			Placeholder(placeholder).FontSize(fontBody).TextColor(p.text).
			Background(ui.Color{}).Border(0, ui.Color{}).Padding(0).Label(placeholder)
		input.OnTextKey(func(k ui.TextKey) bool {
			if k.Key != ui.KeyEnter || k.Mods != 0 {
				return false
			}
			match, ok := s.settingsFirstSearchMatch(k.Text())
			if !ok {
				return false
			}
			s.settingsSelectCategory(state, match.id)
			state.Search = ""
			k.SetText("")
			return true
		})
	})

	query := strings.TrimSpace(state.Search)
	if query == "" {
		return
	}
	matches := s.settingsSearchMatches(query)
	ui.Column(c).FillWidth().Gap(6).Padding(10).
		Radius(runtimeTokens.Dimension.Radius["overlay"].Value).
		Border(runtimeTokens.Dimension.Border.Value, p.border).
		Background(p.surface).Children(func() {
		ui.Text(c, text("settingsSearchResults")).FontSize(fontSmall).
			FontWeight(650).TextColor(p.muted)
		if len(matches) == 0 {
			ui.Text(c, text("settingsSearchNoResults")).FontSize(fontBody).
				TextColor(p.muted)
			return
		}
		for _, spec := range matches {
			s.settingsSearchResultV12(c, p, state, spec)
		}
	})
}

func (s *appState) settingsSearchResultV12(
	c *ui.Context,
	p palette,
	state *SettingsPageState,
	spec settingsCategorySpec,
) {
	label := text(spec.labelKey)
	accessibleLabel := textArgs("settingsSearchOpenCategory", map[string]any{
		"category": label,
	})
	button := ui.ButtonBase(c.Key("settings-search-result-"+string(spec.id))).
		FillWidth().Justify(ui.Start).Padding(7, 9).
		Radius(runtimeTokens.Dimension.Radius["input"].Value).
		Border(runtimeTokens.Dimension.Border.Value, p.border).
		Background(p.background).Cursor(ui.CursorPointer).Label(accessibleLabel)
	if button.Hovered() {
		button.Background(p.surfaceHover)
	}
	button.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon(spec.iconName)).Size(15, 15).TextColor(p.accentText)
			ui.Text(c, label).FontSize(fontBody).FontWeight(600).TextColor(p.text)
		})
	})
	if button.Clicked() {
		s.settingsSelectCategory(state, spec.id)
		state.Search = ""
	}
}

func (s *appState) settingsWideLayoutV12(
	c *ui.Context,
	p palette,
	state *SettingsPageState,
	active settingsCategoryID,
) {
	ui.Row(c).FillWidth().Gap(24).AlignItems(ui.Start).Children(func() {
		s.settingsNavigationV12(c, p, state, active, false)
		ui.Column(c).Key("settings-content-" + string(active)).Grow(1).MinWidth(0).
			Children(func() {
				s.settingsCategoryContentV12(c, p, active, state)
			})
	})
}

func (s *appState) settingsNarrowLayoutV12(
	c *ui.Context,
	p palette,
	state *SettingsPageState,
	active settingsCategoryID,
) {
	ui.Column(c).FillWidth().Gap(16).Children(func() {
		s.settingsNavigationV12(c, p, state, active, true)
		ui.Column(c).Key("settings-content-" + string(active)).FillWidth().MinWidth(0).
			Children(func() {
				s.settingsCategoryContentV12(c, p, active, state)
			})
	})
}

func (s *appState) settingsNavigationV12(
	c *ui.Context,
	p palette,
	state *SettingsPageState,
	active settingsCategoryID,
	narrow bool,
) {
	if narrow {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Row(c).FillWidth().Wrap().Gap(8).Children(func() {
				s.settingsCategoryButtonsV12(c, p, state, active, false)
			})
			ui.Box(c).FillWidth().Height(runtimeTokens.Dimension.Border.Value).
				Background(p.border)
			ui.Row(c).FillWidth().Wrap().Children(func() {
				s.settingsCategoryButtonV12(c, p, state, settingsCategoryAbout,
					active == settingsCategoryAbout, false)
			})
		})
		return
	}

	ui.Column(c).Width(184).Shrink(0).Gap(8).Children(func() {
		ui.Column(c).FillWidth().Gap(4).Children(func() {
			s.settingsCategoryButtonsV12(c, p, state, active, true)
		})
		ui.Box(c).FillWidth().Height(runtimeTokens.Dimension.Border.Value).
			Background(p.border)
		s.settingsCategoryButtonV12(c, p, state, settingsCategoryAbout,
			active == settingsCategoryAbout, true)
	})
}

func (s *appState) settingsCategoryButtonsV12(
	c *ui.Context,
	p palette,
	state *SettingsPageState,
	active settingsCategoryID,
	fillWidth bool,
) {
	for _, spec := range settingsCategorySpecs {
		if spec.id == settingsCategoryAbout {
			continue
		}
		s.settingsCategoryButtonV12(c, p, state, spec.id, active == spec.id, fillWidth)
	}
}

func (s *appState) settingsCategoryButtonV12(
	c *ui.Context,
	p palette,
	state *SettingsPageState,
	id settingsCategoryID,
	selected bool,
	fillWidth bool,
) {
	spec, ok := settingsCategory(id)
	if !ok {
		return
	}
	label := text(spec.labelKey)
	bg, fg := ui.Color{}, p.muted
	if selected {
		bg, fg = p.surfaceHover, p.accentText
	}
	button := ui.ButtonBase(c.Key("settings-nav-"+string(id))).
		Padding(8, 10).Radius(runtimeTokens.Dimension.Radius["input"].Value).
		Justify(ui.Start).Background(bg).Cursor(ui.CursorPointer).Label(label)
	if fillWidth {
		button.FillWidth()
	}
	if selected {
		button.Border(runtimeTokens.Dimension.Border.Value, p.border)
	}
	if button.Hovered() && !selected {
		button.Background(p.surfaceHover)
	}
	button.Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Size(20, 20).Shrink(0).Center().Children(func() {
				ui.Icon(c, icon(spec.iconName)).Size(17, 17).TextColor(fg)
			})
			ui.Text(c, label).FontSize(fontBody).FontWeight(600).TextColor(fg)
		})
	})
	if button.Clicked() {
		s.settingsSelectCategory(state, id)
	}
}

func (s *appState) settingsCategoryContentV12(
	c *ui.Context,
	p palette,
	active settingsCategoryID,
	state *SettingsPageState,
) {
	switch active {
	case settingsCategoryAppearance:
		s.settingsAppearanceV12(c, p)
	case settingsCategoryModels:
		if state.ChannelContent != nil {
			state.ChannelContent(c, p)
		} else {
			s.channelSettingsContent(c, p)
		}
	case settingsCategoryConversation:
		s.settingsConversationV12(c, p)
	case settingsCategoryWorkspace:
		s.settingsWorkspaceV12(c, p)
	case settingsCategoryExtensions:
		s.settingsExtensionsV12(c, p)
	case settingsCategoryData:
		s.settingsDataV12(c, p)
	case settingsCategoryAbout:
		s.settingsAboutV12(c, p)
	default:
		s.settingsGeneralV12(c, p)
	}
}

func (s *appState) settingsGeneralV12(c *ui.Context, p palette) {
	ui.Column(c).FillWidth().Gap(16).Children(func() {
		settingsCardV12(c, p, locales.SettingsLanguageTitle(), "", func() {
			ui.Row(c).Gap(8).Wrap().Children(func() {
				s.languageButton(c, p, uiLanguageSystem, locales.SettingsLanguageSystem())
				for _, tag := range locales.Catalog.Languages() {
					s.languageButton(c, p, tag, i18n.Autonym(tag))
				}
			})
		})
		settingsCardV12(c, p, text("settingsShortcutsTitle"), "", func() {
			settingsValueV12(c, p, text("settingsShortcutNavigationAction"),
				settingsNavigationShortcut())
			settingsValueV12(c, p, text("settingsShortcutTasksAction"),
				settingsTasksShortcut())
		})
	})
}

func (s *appState) settingsAppearanceV12(c *ui.Context, p palette) {
	settingsCardV12(c, p, locales.SettingsAppearanceTitle(), "", func() {
		ui.Row(c).Gap(8).Wrap().Children(func() {
			s.themeButton(c, p, themeSystem, "monitor", locales.SettingsAppearanceSystem())
			s.themeButton(c, p, themeLight, "sun", locales.SettingsAppearanceLight())
			s.themeButton(c, p, themeDark, "moon", locales.SettingsAppearanceDark())
		})
		s.settingsChoiceRow(c, p, "contrast",
			text("settingsContrastTitle"), "",
			[]settingsChoice{
				{"system", "monitor", text("settingsContrastSystem"), func() { s.setContrast(contrastSystem) }},
				{"standard", "sun", text("settingsContrastStandard"), func() { s.setContrast(contrastNormal) }},
				{"high", "contrast", text("settingsContrastHigh"), func() { s.setContrast(contrastHigh) }},
			},
		)
		s.settingsChoiceRow(c, p, "motion",
			text("settingsMotionTitle"), "",
			[]settingsChoice{
				{"system", "monitor", text("settingsMotionSystem"), func() { s.setMotion(motionSystem) }},
				{"standard", "activity", text("settingsMotionStandard"), func() { s.setMotion(motionStandard) }},
				{"reduced", "stop", text("settingsMotionReduced"), func() { s.setMotion(motionReduced) }},
			},
		)
	})
}

func (s *appState) settingsConversationV12(c *ui.Context, p palette) {
	settingsCardV12(c, p, locales.SettingsFollowUpTitle(), "", func() {
		ui.Row(c).Gap(8).Wrap().Children(func() {
			s.followUpStyleButton(c, p, followUpStyleGhost, "chat",
				locales.SettingsFollowUpGhost())
			s.followUpStyleButton(c, p, followUpStyleCard, "document",
				locales.SettingsFollowUpCard())
		})
		ui.Text(c, text("settingsFollowUpKeyboardHint")).FontSize(fontSmall).
			TextColor(p.muted)
	})
}

func (s *appState) settingsWorkspaceV12(c *ui.Context, p palette) {
	settingsCardV12(c, p, text("settingsWorkspaceTitle"), "", func() {
		workspace, err := s.sessionWorkspaceCwd()
		if err != nil {
			ui.Text(c, text("errorWorkspaceFailed")).FontSize(fontBody).
				TextColor(p.dangerText)
			return
		}
		settingsValueV12(c, p, text("settingsWorkspacePath"), workspace)
	})
}

func (s *appState) settingsExtensionsV12(c *ui.Context, p palette) {
	settingsCardV12(c, p, text("settingsExtensionsTitle"), "", func() {
		ui.Text(c, text("settingsPiExtensionsReadOnly")).FontSize(fontBody).
			TextColor(p.muted)
	})
	s.pluginSettingsSection(c, p)
	secondaryAction(c, p, "settings-manage-plugins", text("navPlugins"), func() {
		s.setPage(pagePlugins)
	})
}

func (s *appState) settingsDataV12(c *ui.Context, p palette) {
	settingsCardV12(c, p, locales.SettingsPiTitle(), "", func() {
		ui.Row(c).Gap(9).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(8, 8).Radius(4).Background(s.piTone(p))
			statusLabel := s.piConnectionLabel()
			if message := s.piConnectionErrorText(); message != "" {
				statusLabel = message
			}
			ui.Text(c, statusLabel).FontSize(fontSmall).TextColor(p.text).Grow(1).
				MinWidth(0).MaxLines(2).Tooltip(statusLabel)
			label := locales.SettingsConnect()
			if s.piConn == piConnected {
				label = locales.SettingsReconnect()
			}
			secondaryAction(c, p, "settings-pi-connect", label, func() {
				s.reconnectPi()
			})
		})
		ui.Column(c).Gap(8).Children(func() {
			settingsValueV12(c, p, locales.SettingsCliPath(), s.piPathLabel())
			settingsValueV12(c, p, locales.SettingsVersion(),
				fallback(s.piVersion, locales.SettingsPendingHandshake()))
		})
		if s.piConnectionErrorActive() {
			label := text("settingsShowPiDetails")
			if s.piErrorDetailsOpen {
				label = text("settingsHidePiDetails")
			}
			button := ui.ButtonBase(c.Key("settings-pi-error-details")).Justify(ui.Start).
				Shrink(0).Padding(0).Background(ui.Color{}).Cursor(ui.CursorPointer).Label(label)
			if button.Hovered() {
				button.Background(p.surfaceHover)
			}
			button.Children(func() {
				ui.Text(c, label).FontSize(fontSmall).FontWeight(600).TextColor(p.muted)
			})
			if button.Clicked() {
				s.piErrorDetailsOpen = !s.piErrorDetailsOpen
			}
			if s.piErrorDetailsOpen {
				ui.Row(c).FillWidth().Padding(9, 10).
					Radius(runtimeTokens.Dimension.Radius["input"].Value).
					Background(p.background).Children(func() {
					ui.Text(c, s.piError).Font("monospace").FontSize(fontBody).
						LineHeight(1.4).TextColor(p.dangerText).FillWidth().MinWidth(0)
				})
			}
		}
	})
	s.legacyMigrationSettingsControl(c, p)
	s.diagnosticsSettingsCard(c, p, s.pluginRegistrySnapshot())
}

func (s *appState) settingsAboutV12(c *ui.Context, p palette) {
	settingsCardV12(c, p, text("settingsCategoryAbout"), "", func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			brandMark(c, p, 38)
			ui.Column(c).Gap(3).Children(func() {
				ui.Text(c, appShort).FontSize(fontTitle).FontWeight(650).
					TextColor(p.text)
				ui.Text(c, locales.SettingsVersion()+" "+appVersion).
					FontSize(fontSmall).TextColor(p.muted)
				ui.Text(c, text("settingsBuild")+" "+appBuildVersion()).
					FontSize(fontSmall).TextColor(p.muted)
			})
		})
	})
}

func settingsCardV12(c *ui.Context, p palette, title, description string, body func()) {
	ui.Column(c).FillWidth().Gap(12).Padding(16).
		Radius(runtimeTokens.Dimension.Radius["overlay"].Value).
		Border(runtimeTokens.Dimension.Border.Value, p.border).
		Background(p.surface).Children(func() {
		if title != "" {
			ui.Text(c, title).FontSize(fontBody).FontWeight(650).TextColor(p.text)
		}
		if description != "" {
			ui.Text(c, description).FontSize(fontSmall).LineHeight(1.4).
				TextColor(p.muted)
		}
		body()
	})
}

func settingsValueV12(c *ui.Context, p palette, label, value string) {
	ui.Row(c).FillWidth().Padding(8, 10).
		Radius(runtimeTokens.Dimension.Radius["input"].Value).Background(p.background).
		Wrap().AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Text(c, label).FontSize(fontSmall).TextColor(p.muted).MinWidth(72).
			MaxWidth(150)
		ui.Text(c, value).Font("monospace").FontSize(fontBody).FontWeight(600).
			TextColor(p.text).Grow(1).MinWidth(0).MaxLines(2).Tooltip(value)
	})
}

func (s *appState) settingsPageActiveCategory(state *SettingsPageState) settingsCategoryID {
	if state.AboutOpen {
		return settingsCategoryAbout
	}
	if _, ok := settingsCategory(state.ActiveCategory); ok &&
		state.ActiveCategory != settingsCategoryAbout {
		return state.ActiveCategory
	}
	return settingsCategoryGeneral
}

func (s *appState) settingsSelectCategory(state *SettingsPageState, id settingsCategoryID) {
	if _, ok := settingsCategory(id); !ok {
		return
	}
	if id == settingsCategoryAbout {
		state.AboutOpen = true
		return
	}
	state.ActiveCategory = id
	state.AboutOpen = false
}

func settingsCategory(id settingsCategoryID) (settingsCategorySpec, bool) {
	for _, spec := range settingsCategorySpecs {
		if spec.id == id {
			return spec, true
		}
	}
	return settingsCategorySpec{}, false
}

func (s *appState) settingsFirstSearchMatch(query string) (settingsCategorySpec, bool) {
	matches := s.settingsSearchMatches(query)
	if len(matches) == 0 {
		return settingsCategorySpec{}, false
	}
	return matches[0], true
}

func (s *appState) settingsSearchMatches(query string) []settingsCategorySpec {
	return settingsSearchMatchesWithText(query, text)
}

func settingsSearchMatchesWithText(
	query string,
	localize func(string) string,
) []settingsCategorySpec {
	query = settingsSearchNormalize(query)
	if query == "" {
		return nil
	}
	matches := make([]settingsCategorySpec, 0, len(settingsCategorySpecs))
	for _, spec := range settingsCategorySpecs {
		for _, term := range settingsCategorySearchTermsWithText(spec, localize) {
			if settingsSearchTermMatches(term, query) {
				matches = append(matches, spec)
				break
			}
		}
	}
	return matches
}

func settingsCategorySearchTermsWithText(
	spec settingsCategorySpec,
	localize func(string) string,
) []string {
	terms := []string{localize(spec.labelKey)}
	switch spec.id {
	case settingsCategoryGeneral:
		terms = append(terms, localize("settingsLanguageTitle"),
			localize("settingsShortcutsTitle"), "keyboard", "shortcut")
	case settingsCategoryAppearance:
		terms = append(terms, localize("settingsAppearanceTitle"),
			localize("settingsContrastTitle"), localize("settingsMotionTitle"),
			"theme", "contrast", "motion")
	case settingsCategoryModels:
		terms = append(terms, "models.json", "provider", "model", "channel", "API")
	case settingsCategoryConversation:
		terms = append(terms, localize("settingsFollowUpTitle"),
			localize("chatFollowUpHint"), localize("settingsFollowUpKeyboardHint"),
			"chat", "follow-up")
	case settingsCategoryWorkspace:
		terms = append(terms, localize("navWorkspace"),
			localize("settingsWorkspaceTitle"), "path", "directory")
	case settingsCategoryExtensions:
		terms = append(terms, localize("settingsPluginSettings"),
			localize("settingsExtensionsTitle"), "plugin", "extension", "MCP")
	case settingsCategoryData:
		terms = append(terms, localize("settingsDiagnosticsTitle"),
			localize("settingsPiTitle"), localize("settingsCliPath"),
			localize("settingsVersion"), localize("settingsReconnect"),
			localize("settingsConnect"), localize("legacyReimport"),
			"diagnostics", "JSONL", "CLI", "Pi")
	case settingsCategoryAbout:
		terms = append(terms, localize("settingsAboutDescription"),
			appShort, appTitle, appVersion)
	}
	return terms
}

func settingsSearchNormalize(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func settingsSearchTermMatches(term, query string) bool {
	term = settingsSearchNormalize(term)
	offset := strings.Index(term, query)
	for offset >= 0 {
		if offset == 0 || query[0] < 'a' || query[0] > 'z' ||
			term[offset-1] < 'a' || term[offset-1] > 'z' {
			return true
		}
		next := strings.Index(term[offset+1:], query)
		if next < 0 {
			return false
		}
		offset += next + 1
	}
	return false
}

func settingsNavigationShortcut() string {
	if runtime.GOOS == "darwin" {
		return "⌘B"
	}
	return "Ctrl+B"
}

func settingsTasksShortcut() string {
	if runtime.GOOS == "darwin" {
		return "⌘⌥B"
	}
	return "Ctrl+Alt+B"
}
