package main

import (
	"context"
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
)

func (s *appState) legacyMigrationSection(c *ui.Context, p palette) {
	settingsCard(c, p, text("legacyTitle"), text("legacyDescription"),
		func() {
			if s.preferencesWarning != "" {
				ui.Text(c, preferencesWarningLabel(s.preferencesWarning)).FontSize(fontCaption).LineHeight(1.4).
					TextColor(p.danger)
			}
			if s.savedWorkspaceMiss {
				ui.Text(c, text("legacyWorkspaceMissing")).FontSize(fontCaption).LineHeight(1.4).
					TextColor(p.warning)
			}
			ui.Row(c).Wrap().GapX(8).GapY(8).AlignItems(ui.Center).Children(func() {
				label := text("legacyScan")
				if s.legacyScanning {
					label = text("legacyScanning")
				}
				migrationAction(c, p, "legacy-migration-scan", label, false,
					s.legacyScanning, func() {
						s.scanLegacyMigration()
					})
				if s.legacyScanned {
					if s.legacyScan.canImport {
						migrationAction(c, p, "legacy-migration-import", text("legacyImport"), true,
							s.legacyScanning, func() {
								s.importLegacyMigration()
							})
					} else {
						ui.Text(c, text("legacyUpToDate")).FontSize(fontSmall).
							FontWeight(600).TextColor(p.success)
					}
				}
			})

			if s.legacyImportSet {
				ui.Text(c, textArgs("legacyLastImport", map[string]any{
					"time":   legacyImportTimestamp(s.legacyImport.At),
					"counts": legacyImportCountsSummary(s.legacyImport.Counts),
				})).
					FontSize(fontCaption).LineHeight(1.4).TextColor(p.muted)
			}
			if s.legacyScan.ScanWarning != "" {
				ui.Text(c, legacyScanWarningLabel(s.legacyScan.ScanWarning)).FontSize(fontCaption).
					LineHeight(1.4).TextColor(p.warning)
			}
			if s.legacyScanned {
				s.legacyMigrationPreview(c, p)
			}
		})
}

func (s *appState) scanLegacyMigration() {
	if s.legacyScanning {
		return
	}
	s.legacyScanning = true
	s.legacyScan = legacyScan{}
	s.legacyScanned = false
	sources := s.legacySources
	preferences := cloneByopPreferences(s.preferences)
	go func() {
		scan, err := scanLegacyDesktop(context.Background(), sources, preferences)
		s.postUI(func() {
			s.legacyScanning = false
			if err != nil {
				s.showNotice(text("legacyScanFailed"))
				return
			}
			s.legacyScan = scan
			s.legacyScanned = true
		})
	}()
}

func (s *appState) importLegacyMigration() {
	if s.legacyScanning || !s.legacyScanned || !s.legacyScan.canImport ||
		len(s.legacyScan.Actions) == 0 {
		return
	}
	scan := s.legacyScan
	s.completeLegacyImport(scan)
}

func (s *appState) completeLegacyImport(scan legacyScan) {
	if len(scan.Actions) == 0 {
		return
	}
	next, result := applyLegacyImport(scan, s.preferences)
	persistence, err := persistLegacyImport(s.configDir, s.preferencesStore, next, result)
	if persistence.PreferencesSaved {
		s.preferences = next
		s.preferencesWarning = ""
		s.legacyImport = result.Record
		s.legacyImportSet = true
		if result.ThemeChanged {
			s.applyTheme(result.Theme)
		}
	}
	if err != nil {
		if persistence.PreferencesSaved && !persistence.HistorySaved {
			s.showNotice(text("legacyImportedHistoryFailed"))
			s.startLegacyRescan()
			return
		}
		s.showNotice(text("legacyImportFailed"))
		return
	}
	if result.WorkspaceChange {
		s.savedWorkspaceMiss = false
		s.showNotice(text("legacyWorkspaceSaved"))
	} else {
		s.showNotice(text("legacyImported"))
	}
	s.startLegacyRescan()
}

func (s *appState) startLegacyRescan() {
	s.legacyScanning = true
	s.legacyScan = legacyScan{}
	s.legacyScanned = false
	sources := s.legacySources
	preferences := cloneByopPreferences(s.preferences)
	go func() {
		scan, err := scanLegacyDesktop(context.Background(), sources, preferences)
		s.postUI(func() {
			s.legacyScanning = false
			if err != nil {
				s.showNotice(text("legacyScanFailed"))
				return
			}
			s.legacyScan = scan
			s.legacyScanned = true
		})
	}()
}

func (s *appState) legacyMigrationPreview(c *ui.Context, p palette) {
	version := fallback(s.legacyScan.Version, text("legacyUnknown"))
	ui.Row(c).Wrap().GapX(8).GapY(7).AlignItems(ui.Center).Children(func() {
		statusPill(c, p, textArgs("legacyVersion", map[string]any{"version": version}), p.muted)
		counts := legacyScanCounts(s.legacyScan.Entries)
		for _, count := range []struct {
			label string
			value int
			tone  ui.Color
		}{
			{text("legacyStatusNew"), counts.New, p.accent},
			{text("legacyStatusSame"), counts.Same, p.success},
			{text("legacyStatusConflict"), counts.Conflict, p.warning},
			{text("legacyStatusKept"), counts.Kept, p.muted},
			{text("legacyStatusLinked"), counts.Linked, p.success},
			{text("legacyStatusMissing"), counts.Missing, p.warning},
			{text("legacyStatusUnknown"), counts.Unknown, p.muted},
			{text("legacyStatusIgnored"), counts.Ignored, p.muted},
			{text("legacyStatusInfo"), counts.Info, p.muted},
		} {
			if count.value == 0 {
				continue
			}
			statusPill(c, p, fmt.Sprintf("%s %d", count.label, count.value), count.tone)
		}
	})

	ui.Column(c).Gap(5).Children(func() {
		for _, source := range s.legacyScan.Sources {
			tone := p.muted
			switch source.Status {
			case "found":
				tone = p.success
			case "unavailable":
				tone = p.warning
			}
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(7, 7).Radius(4).Background(tone)
				ui.Text(c, source.Source).Font("monospace").FontSize(fontCaption).
					TextColor(p.text).Width(92)
				ui.Text(c, legacySourceStatusLabel(source.Status)).FontSize(fontCaption).FontWeight(650).
					TextColor(tone).Width(78)
				ui.Text(c, legacySourceSummaryLabel(source.Summary)).FontSize(fontCaption).TextColor(p.muted).Grow(1)
			})
		}
	})

	if len(s.legacyScan.Entries) == 0 {
		ui.Text(c, text("legacyNoPreferences")).FontSize(fontSmall).
			TextColor(p.muted)
		return
	}
	ui.Column(c).FillWidth().Gap(0).Border(1, p.border).Radius(8).
		Background(p.background).Children(func() {
		for index, entry := range s.legacyScan.Entries {
			if index > 0 {
				ui.Divider(c)
			}
			ui.Row(c).FillWidth().Padding(8, 10).Gap(9).AlignItems(ui.Center).Children(func() {
				ui.Text(c, entry.Source).Font("monospace").FontSize(fontMicro).
					TextColor(p.muted).Width(78)
				ui.Text(c, legacyEntrySummaryLabel(entry.Summary)).FontSize(fontCaption).LineHeight(1.35).
					TextColor(p.text).Grow(1)
				ui.Text(c, legacyTargetLabel(entry.Target)).FontSize(fontMicro).TextColor(p.muted).Width(82)
				statusPill(c, p, entry.statusLabel(),
					legacyStatusTone(p, entry.Status))
			})
		}
	})
}

func migrationAction(
	c *ui.Context,
	p palette,
	key, label string,
	primary, disabled bool,
	click func(),
) {
	background := p.surface
	foreground := p.text
	border := p.border
	if primary {
		background = p.accent
		foreground = p.accentText
		border = p.accent
	}
	if disabled {
		background = p.surface
		border = p.border
		foreground = p.muted
	}
	btn := ui.ButtonBase(c.Key(key)).Padding(7, 11).Radius(7).Border(1, border).
		Background(background).Cursor(ui.CursorPointer).Label(label).Disabled(disabled)
	if btn.Hovered() && !disabled {
		if primary {
			btn.Background(p.accentHover)
		} else {
			btn.Background(p.surfaceHover)
		}
	}
	btn.Children(func() {
		ui.Text(c, label).FontSize(fontSmall).FontWeight(600).TextColor(foreground)
	})
	if btn.Clicked() && !disabled {
		click()
	}
}

func legacyStatusTone(p palette, status legacyImportStatus) ui.Color {
	switch status {
	case legacyStatusNew:
		return p.accent
	case legacyStatusSame, legacyStatusLinked:
		return p.success
	case legacyStatusConflict, legacyStatusMissing:
		return p.warning
	case legacyStatusUnknown:
		return p.danger
	default:
		return p.muted
	}
}

func legacyImportTimestamp(value string) string {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return fallback(value, text("legacyUnknown"))
	}
	return parsed.Local().Format("2006-01-02 15:04:05")
}

func legacyImportCountsSummary(counts legacyImportCounts) string {
	return textArgs("legacyCountsSummary", map[string]any{
		"new": counts.New, "same": counts.Same, "conflict": counts.Conflict,
		"kept": counts.Kept, "linked": counts.Linked, "missing": counts.Missing,
		"unknown": counts.Unknown, "ignored": counts.Ignored, "info": counts.Info,
	})
}

func legacySourceStatusLabel(status string) string {
	switch status {
	case "found":
		return text("legacyFound")
	case "unavailable":
		return text("legacyUnavailable")
	default:
		return text("legacyNotFound")
	}
}
