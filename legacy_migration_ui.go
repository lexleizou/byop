package main

import (
	"context"

	"byop/locales"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components/chat"
)

func (s *appState) legacyMigrationPromptVisible() bool {
	return s.legacyScanned &&
		!s.legacyScanning &&
		!s.legacyScanFailed &&
		s.legacyScan.canImport &&
		len(s.legacyScan.Actions) > 0 &&
		!s.preferences.LegacyMigrationPromptDismissed
}

func (s *appState) legacyMigrationReimportVisible() bool {
	return s.legacyScanned &&
		!s.legacyScanning &&
		!s.legacyScanFailed &&
		legacyMigrationAppSourceFound(s.legacyScan)
}

func legacyMigrationAppSourceFound(scan legacyScan) bool {
	for _, source := range scan.Sources {
		if source.Source == "app-dir" && source.Status == "found" {
			return true
		}
	}
	return false
}

func (s *appState) scanLegacyMigration() {
	s.startLegacyMigrationScan(false)
}

func (s *appState) reimportLegacyMigration() {
	s.startLegacyMigrationScan(true)
}

func (s *appState) startLegacyMigrationScan(importWhenAvailable bool) {
	if s.legacyScanning {
		return
	}
	s.legacyScanning = true
	s.legacyScanFailed = false
	sources := s.legacySources
	preferences := cloneByopPreferences(s.preferences)
	go func() {
		scan, err := scanLegacyDesktop(context.Background(), sources, preferences)
		s.postUI(func() {
			s.legacyScanning = false
			if err != nil {
				s.legacyScan = legacyScan{}
				s.legacyScanned = false
				s.legacyScanFailed = true
				return
			}
			s.legacyScan = scan
			s.legacyScanned = true
			if importWhenAvailable {
				if scan.canImport && len(scan.Actions) > 0 {
					s.completeLegacyImport(scan, true)
				} else {
					s.showNotice(text("legacyUpToDate"))
				}
			}
		})
	}()
}

func (s *appState) importLegacyMigration() {
	if s.legacyScanning || !s.legacyScanned || s.legacyScanFailed ||
		!s.legacyScan.canImport || len(s.legacyScan.Actions) == 0 {
		return
	}
	s.completeLegacyImport(s.legacyScan, true)
}

func (s *appState) completeLegacyImport(scan legacyScan, rescan bool) bool {
	if len(scan.Actions) == 0 {
		return false
	}
	next, result := applyLegacyImport(scan, s.preferences)
	next.LegacyMigrationPromptDismissed = true
	persistence, err := persistLegacyImport(s.configDir, s.preferencesStore, next, result)
	if persistence.PreferencesSaved {
		s.preferences = next
		s.preferencesWarning = s.preferencesStore.warningSnapshot()
		if result.ThemeChanged {
			s.applyTheme(result.Theme)
		}
	}
	if err != nil {
		if persistence.PreferencesSaved && !persistence.HistorySaved {
			s.showNotice(text("legacyImportedHistoryFailed"))
			if rescan {
				s.startLegacyMigrationScan(false)
			}
			return true
		}
		s.showNotice(text("legacyImportFailed"))
		return false
	}
	if result.WorkspaceChange {
		s.savedWorkspaceMiss = false
		s.showNotice(text("legacyWorkspaceSaved"))
	} else {
		s.showNotice(text("legacyImported"))
	}
	if rescan {
		s.startLegacyMigrationScan(false)
	}
	return true
}

func (s *appState) dismissLegacyMigrationPrompt() {
	if s.preferences.LegacyMigrationPromptDismissed {
		return
	}
	previous := s.preferences.LegacyMigrationPromptDismissed
	s.preferences.LegacyMigrationPromptDismissed = true
	if err := s.persistPreferencePatch(func(preferences *byopPreferences) {
		preferences.LegacyMigrationPromptDismissed = true
	}); err != nil {
		s.preferences.LegacyMigrationPromptDismissed = previous
		s.showNotice(text("legacyPromptDismissFailed"))
	}
}

func (s *appState) legacyMigrationPrompt(c *ui.Context, p palette) {
	if !s.legacyMigrationPromptVisible() {
		return
	}
	ui.Row(c).FillWidth().Justify(ui.Center).MarginY(8).Children(func() {
		ui.Row(c).FillWidth().MaxWidth(chat.ContentWidth).MinWidth(0).Padding(9, 12).Gap(10).
			Radius(12).Border(runtimeTokens.Dimension.Border.Value, p.border).Background(p.surface).
			AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon("info")).Size(16, 16).Shrink(0).TextColor(p.accent)
			ui.Text(c, text("legacyPromptDiscovered")).FontSize(fontBody).LineHeight(1.4).
				TextColor(p.text).Grow(1).MinWidth(0).MaxLines(2).
				Tooltip(text("legacyPromptDiscovered"))
			legacyPromptAction(c, p, "legacy-prompt-import", text("legacyImport"), true, func() {
				s.importLegacyMigration()
			})
			legacyPromptAction(c, p, "legacy-prompt-dismiss", text("legacyPromptDismiss"), false, func() {
				s.dismissLegacyMigrationPrompt()
			})
		})
	})
}

func legacyPromptAction(
	c *ui.Context,
	p palette,
	key, label string,
	primary bool,
	click func(),
) {
	background := p.surface
	foreground := p.text
	border := p.border
	if primary {
		background = p.accent
		foreground = p.onAccent
		border = p.accent
	}
	button := ui.ButtonBase(c.Key(key)).Shrink(0).Padding(7, 10).Radius(8).
		Border(runtimeTokens.Dimension.Border.Value, border).Background(background).Cursor(ui.CursorPointer).Label(label).
		Tooltip(label)
	if button.Hovered() {
		if primary {
			button.Background(p.accentHover)
		} else {
			button.Background(p.surfaceHover)
		}
	}
	button.Children(func() {
		ui.Text(c, label).FontSize(fontSmall).FontWeight(600).TextColor(foreground)
	})
	if button.Clicked() {
		click()
	}
}

func (s *appState) legacyMigrationSettingsControl(c *ui.Context, p palette) {
	if s.legacyScanFailed {
		ui.Text(c, text("legacyScanFailed")).FontSize(fontCaption).LineHeight(1.4).TextColor(p.warningText)
		return
	}
	if s.legacyScanning {
		ui.Text(c, text("legacyScanning")).FontSize(fontCaption).TextColor(p.muted)
		return
	}
	if !s.legacyMigrationReimportVisible() {
		return
	}
	button := ui.ButtonBase(c.Key("settings-legacy-reimport")).Justify(ui.Start).Shrink(0).
		Padding(0).Background(ui.Color{}).Cursor(ui.CursorPointer).Label(text("legacyReimport"))
	if button.Hovered() {
		button.Background(p.surfaceHover)
	}
	button.Children(func() {
		ui.Text(c, text("legacyReimport")).FontSize(fontSmall).FontWeight(600).TextColor(p.accentText)
	})
	if button.Clicked() {
		s.reimportLegacyMigration()
	}
}

func (s *appState) diagnosticsSettingsCard(
	c *ui.Context,
	p palette,
	registry pluginRegistry,
) {
	ui.Column(c).FillWidth().Gap(13).Padding(16).Radius(12).
		Border(runtimeTokens.Dimension.Border.Value, p.border).Background(p.surface).Children(func() {
		header := ui.ButtonBase(c.Key("settings-diagnostics-toggle")).FillWidth().Padding(0).
			Background(ui.Color{}).Cursor(ui.CursorPointer).
			Label(text("settingsDiagnosticsTitle")).Tooltip(text("settingsDiagnosticsToggle"))
		header.Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, text("settingsDiagnosticsTitle")).FontSize(fontBody).FontWeight(650).
					TextColor(p.text).Grow(1)
				iconName := "chevron-right"
				if s.diagnosticsOpen {
					iconName = "chevron-down"
				}
				ui.Icon(c, icon(iconName)).Size(14, 14).TextColor(p.muted)
			})
		})
		if header.Clicked() {
			s.diagnosticsOpen = !s.diagnosticsOpen
		}
		if !s.diagnosticsOpen {
			return
		}

		ui.Text(c, text("settingsDiagnosticsDescription")).FontSize(fontSmall).
			LineHeight(1.45).TextColor(p.muted)
		if s.preferencesWarning != "" {
			ui.Text(c, preferencesWarningLabel(s.preferencesWarning)).FontSize(fontCaption).
				LineHeight(1.4).TextColor(p.dangerText)
		}
		if s.savedWorkspaceMiss {
			ui.Text(c, text("legacyWorkspaceMissing")).FontSize(fontCaption).
				LineHeight(1.4).TextColor(p.warningText)
		}
		if s.legacyScan.ScanWarning != "" {
			ui.Text(c, legacyScanWarningLabel(s.legacyScan.ScanWarning)).FontSize(fontCaption).
				LineHeight(1.4).TextColor(p.warningText)
		}
		ui.Column(c).Gap(8).Children(func() {
			settingsValue(c, p, locales.SettingsNativeRuntime(),
				locales.SettingsNativeRuntimeValue())
			settingsValue(c, p, locales.SettingsRpcTransport(),
				locales.SettingsRpcTransportValue())
			settingsValue(c, p, locales.SettingsPiOwnership(),
				locales.SettingsPiOwnershipValue())
			settingsValue(c, p, locales.SettingsPluginRegistry(),
				textArgs("settingsPluginRegistryValue", map[string]any{
					"registered":    registry.registeredCount(),
					"contributions": registry.contributionCount(),
				}))
			settingsValue(c, p, locales.SettingsPluginRuntime(),
				locales.SettingsPluginRuntimeValue())
			settingsValue(c, p, locales.SettingsWebViewBoundary(),
				locales.SettingsWebViewBoundaryValue())
		})
	})
}
