package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

func (s *appState) legacyMigrationSection(c *ui.Context, p palette) {
	settingsCard(c, p, "Migrate from Pi Agent Desktop",
		"Read-only scan of the old desktop app. Nothing is deleted; Pi sessions are linked, not copied.",
		func() {
			if s.preferencesWarning != "" {
				ui.Text(c, s.preferencesWarning).FontSize(fontCaption).LineHeight(1.4).
					TextColor(p.danger)
			}
			if s.savedWorkspaceMiss {
				ui.Text(c, "saved workspace missing").FontSize(fontCaption).LineHeight(1.4).
					TextColor(p.warning)
			}
			ui.Row(c).Wrap().GapX(8).GapY(8).AlignItems(ui.Center).Children(func() {
				label := "Scan"
				if s.legacyScanning {
					label = "Scanning…"
				}
				migrationAction(c, p, "legacy-migration-scan", label, false,
					s.legacyScanning, func() {
						s.scanLegacyMigration()
					})
				if s.legacyScanned {
					if s.legacyScan.canImport {
						migrationAction(c, p, "legacy-migration-import", "Import", true,
							s.legacyScanning, func() {
								s.importLegacyMigration()
							})
					} else {
						ui.Text(c, "Already up to date").FontSize(fontSmall).
							FontWeight(600).TextColor(p.success)
					}
				}
			})

			if s.legacyImportSet {
				ui.Text(c, "Last import "+legacyImportTimestamp(s.legacyImport.At)+
					" · "+legacyImportCountsSummary(s.legacyImport.Counts)).
					FontSize(fontCaption).LineHeight(1.4).TextColor(p.muted)
			}
			if s.legacyScan.ScanWarning != "" {
				ui.Text(c, s.legacyScan.ScanWarning).FontSize(fontCaption).
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
				s.showNotice("Legacy scan failed.")
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
			s.showNotice("Legacy preferences imported, but migration history could not be saved.")
			s.startLegacyRescan()
			return
		}
		s.showNotice("Legacy import failed.")
		return
	}
	if result.WorkspaceChange {
		s.savedWorkspaceMiss = false
		s.showNotice("Workspace saved; reconnect Pi to use it")
	} else {
		s.showNotice("Legacy preferences imported.")
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
				s.showNotice("Legacy scan failed.")
				return
			}
			s.legacyScan = scan
			s.legacyScanned = true
		})
	}()
}

func (s *appState) legacyMigrationPreview(c *ui.Context, p palette) {
	version := fallback(s.legacyScan.Version, "Unknown")
	ui.Row(c).Wrap().GapX(8).GapY(7).AlignItems(ui.Center).Children(func() {
		statusPill(c, p, "LEGACY "+version, p.muted)
		counts := legacyScanCounts(s.legacyScan.Entries)
		for _, count := range []struct {
			label string
			value int
			tone  ui.Color
		}{
			{"NEW", counts.New, p.accent},
			{"SAME", counts.Same, p.success},
			{"CONFLICT", counts.Conflict, p.warning},
			{"KEPT", counts.Kept, p.muted},
			{"LINKED", counts.Linked, p.success},
			{"MISSING", counts.Missing, p.warning},
			{"UNKNOWN", counts.Unknown, p.muted},
			{"IGNORED", counts.Ignored, p.muted},
			{"INFO", counts.Info, p.muted},
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
				ui.Text(c, source.Status).FontSize(fontCaption).FontWeight(650).
					TextColor(tone).Width(78)
				ui.Text(c, source.Summary).FontSize(fontCaption).TextColor(p.muted).Grow(1)
			})
		}
	})

	if len(s.legacyScan.Entries) == 0 {
		ui.Text(c, "No migratable legacy preferences were found.").FontSize(fontSmall).
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
				ui.Text(c, entry.Summary).FontSize(fontCaption).LineHeight(1.35).
					TextColor(p.text).Grow(1)
				ui.Text(c, entry.Target).FontSize(fontMicro).TextColor(p.muted).Width(82)
				statusPill(c, p, strings.ToUpper(string(entry.Status)),
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
		return fallback(value, "Unknown")
	}
	return parsed.Local().Format("2006-01-02 15:04:05")
}

func legacyImportCountsSummary(counts legacyImportCounts) string {
	return fmt.Sprintf("new %d · same %d · conflict %d · kept %d · linked %d · missing %d · unknown %d · ignored %d · info %d",
		counts.New, counts.Same, counts.Conflict, counts.Kept, counts.Linked,
		counts.Missing, counts.Unknown, counts.Ignored, counts.Info)
}
