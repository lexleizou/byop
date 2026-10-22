package main

import (
	"os"
	"strconv"
	"strings"

	"byop/locales"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
)

const uiLanguageSystem = "system"

func normalizeUILanguage(value string) string {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, uiLanguageSystem) {
		return uiLanguageSystem
	}
	for _, tag := range locales.Catalog.Languages() {
		if strings.EqualFold(value, tag) {
			return tag
		}
	}
	return uiLanguageSystem
}

func effectiveUILanguage(value string) string {
	value = normalizeUILanguage(value)
	if value != uiLanguageSystem {
		return value
	}
	if tag := systemLocaleTag(); tag != "" {
		return tag
	}
	return locales.Catalog.Source()
}

func systemLocaleTag() string {
	if mygo.App.IsReady() {
		return i18n.Match(mygo.App.PreferredLanguages(), locales.Catalog.Languages())
	}
	if tag := i18n.Match(strings.Fields(os.Getenv("LANGUAGE")), locales.Catalog.Languages()); tag != "" {
		return tag
	}
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if tag := i18n.Match([]string{os.Getenv(name)}, locales.Catalog.Languages()); tag != "" {
			return tag
		}
	}
	return i18n.Match([]string{os.Getenv("MYGO_LOCALE")}, locales.Catalog.Languages())
}

func text(key string) string {
	return locales.Catalog.Format(i18n.Locale(), key, nil)
}

func textArgs(key string, args map[string]any) string {
	return locales.Catalog.Format(i18n.Locale(), key, args)
}

func preferencesWarningLabel(warning string) string {
	switch warning {
	case "Preferences could not be read; defaults are in use.":
		return text("preferencesWarningRead")
	case "Preferences are empty; defaults are in use.":
		return text("preferencesWarningEmpty")
	case "Preferences file is damaged; defaults are in use and the original file is preserved.":
		return text("preferencesWarningDamaged")
	default:
		return warning
	}
}

func legacySourceSummaryLabel(summary string) string {
	switch summary {
	case "No app directory":
		return text("legacySourceNoAppDirectory")
	case "App directory found":
		return text("legacySourceAppDirectoryFound")
	case "No legacy localStorage database":
		return text("legacySourceNoLocalStorage")
	case "sqlite3 is unavailable":
		return text("legacySourceSQLiteUnavailable")
	case "Legacy localStorage could not be read":
		return text("legacySourceLocalStorageUnreadable")
	case "No legacy scheduled tasks":
		return text("legacySourceNoScheduledTasks")
	case "Window state present":
		return text("legacySummaryWindowState")
	}
	if version, ok := strings.CutPrefix(summary, "version "); ok {
		return textArgs("legacySummaryVersion", map[string]any{"version": version})
	}
	if dimensions, ok := strings.CutPrefix(summary, "Window "); ok {
		if width, height, found := strings.Cut(dimensions, " × "); found {
			return textArgs("legacySummaryWindow", map[string]any{
				"width":  width,
				"height": height,
			})
		}
	}
	if strings.HasSuffix(summary, " preference key(s)") {
		raw := strings.TrimSuffix(summary, " preference key(s)")
		parts := strings.Split(raw, " · ")
		if count, err := strconv.Atoi(parts[0]); err == nil {
			localized := textArgs("legacySummaryPreferenceKeys", map[string]any{"count": count})
			for _, detail := range parts[1:] {
				switch {
				case detail == "undecodable" || strings.HasSuffix(detail, " undecodable"):
					number := strings.TrimSuffix(detail, " undecodable")
					if value, err := strconv.Atoi(number); err == nil {
						localized += " · " + textArgs("legacySummaryUndecodable", map[string]any{"count": value})
					}
				case detail == "latest":
				case strings.HasPrefix(detail, "latest of "):
					number := strings.TrimSuffix(strings.TrimPrefix(detail, "latest of "), " databases")
					if value, err := strconv.Atoi(number); err == nil {
						localized += " · " + textArgs("legacySummaryLatestDatabase", map[string]any{"count": value})
					}
				}
			}
			return localized
		}
	}
	if suffix, ok := strings.CutSuffix(summary, " scheduled task(s) found — not imported in P8 (P7 automations are plugin-owned)"); ok {
		if count, err := strconv.Atoi(suffix); err == nil {
			return textArgs("legacySummaryScheduledTasks", map[string]any{"count": count})
		}
	}
	return summary
}

func legacyEntrySummaryLabel(summary string) string {
	switch summary {
	case "Undecodable localStorage value":
		return text("legacySummaryUndecodableValue")
	case "Server port is managed by BYOP/Pi":
		return text("legacySummaryServerPort")
	case "Unknown preference key":
		return text("legacySummaryUnknownPreference")
	case "Theme also exists in localStorage; localStorage wins":
		return text("legacySummaryThemeLocalStorageWins")
	case "Duplicate app-directory theme; first value wins":
		return text("legacySummaryDuplicateAppTheme")
	case "Duplicate value in localStorage; localStorage wins":
		return text("legacySummaryDuplicateLocalValue")
	case "Unsupported theme value kept as legacy":
		return text("legacySummaryUnsupportedTheme")
	case "Workspace value is unrecognized":
		return text("legacySummaryWorkspaceValueUnknown")
	case "Workspace path is unrecognized":
		return text("legacySummaryWorkspacePathUnknown")
	case "Workspace folder is missing":
		return text("legacySummaryWorkspaceMissing")
	case "Workspace session also exists in last-open map":
		return text("legacySummaryWorkspaceSessionDuplicate")
	case "No workspace fields":
		return text("legacySummaryNoWorkspaceFields")
	case "Workspace session reference is unrecognized":
		return text("legacySummaryWorkspaceSessionUnknown")
	case "Right panel preference kept as legacy":
		return text("legacySummaryRightPanelKept")
	case "Open file tabs kept as legacy":
		return text("legacySummaryFileTabsKept")
	case "Active file tab kept as legacy":
		return text("legacySummaryActiveFileTabKept")
	case "Workspace session reference kept as legacy":
		return text("legacySummaryWorkspaceSessionKept")
	case "Workspace field kept as legacy":
		return text("legacySummaryWorkspaceFieldKept")
	case "Last-session map is unrecognized":
		return text("legacySummaryLastSessionMapUnknown")
	case "No saved sessions":
		return text("legacySummaryNoSavedSessions")
	case "Draft map is unrecognized":
		return text("legacySummaryDraftMapUnknown")
	case "No drafts":
		return text("legacySummaryNoDrafts")
	case "Unrecognized draft value":
		return text("legacySummaryDraftValueUnknown")
	case "Kept as legacy value (BYOP body stays 14px)":
		return text("legacySummaryKeptBodySize")
	case "Width kept as legacy value":
		return text("legacySummaryWidthKept")
	case "Preference kept as legacy value":
		return text("legacySummaryPreferenceKept")
	}
	if version, ok := strings.CutPrefix(summary, "Legacy version "); ok {
		return textArgs("legacySummaryVersion", map[string]any{"version": version})
	}
	if mode, ok := strings.CutPrefix(summary, "Theme: "); ok {
		return textArgs("legacySummaryTheme", map[string]any{"theme": mode})
	}
	if workspace, ok := strings.CutPrefix(summary, "Workspace: "); ok {
		return textArgs("legacySummaryWorkspace", map[string]any{"workspace": workspace})
	}
	if session, ok := strings.CutPrefix(summary, "Workspace session "); ok {
		if base, missing := strings.CutSuffix(session, " · workspace missing"); missing {
			return textArgs("legacySummaryWorkspaceSessionWorkspaceMissing", map[string]any{"session": base})
		}
		if base, missing := strings.CutSuffix(session, " · session missing"); missing {
			return textArgs("legacySummaryWorkspaceSessionMissing", map[string]any{"session": base})
		}
		return textArgs("legacySummaryWorkspaceSession", map[string]any{"session": session})
	}
	if summary, ok := strings.CutSuffix(summary, " · workspace missing"); ok {
		return textArgs("legacySummarySessionWorkspaceMissing", map[string]any{"session": summary})
	}
	if summary, ok := strings.CutSuffix(summary, " · session missing"); ok {
		return textArgs("legacySummarySessionMissing", map[string]any{"session": summary})
	}
	if strings.HasPrefix(summary, "Draft · ") && strings.HasSuffix(summary, " character(s)") {
		raw := strings.TrimSuffix(strings.TrimPrefix(summary, "Draft · "), " character(s)")
		if count, err := strconv.Atoi(raw); err == nil {
			return textArgs("legacySummaryDraftCharacters", map[string]any{"count": count})
		}
	}
	return summary
}

func legacyTargetLabel(target string) string {
	switch target {
	case "Info only":
		return text("legacyTargetInfoOnly")
	case "Not imported":
		return text("legacyTargetNotImported")
	case "Ignored":
		return text("legacyTargetIgnored")
	case "Legacy":
		return text("legacyTargetLegacy")
	case "Theme":
		return text("legacyTargetTheme")
	case "Workspace":
		return text("legacyTargetWorkspace")
	case "Last session":
		return text("legacyTargetLastSession")
	case "Session links":
		return text("legacyTargetSessionLinks")
	case "Drafts":
		return text("legacyTargetDrafts")
	default:
		return target
	}
}

func legacyScanWarningLabel(warning string) string {
	switch warning {
	case "Legacy localStorage could not be read.":
		return text("legacyWarningLocalStorageRead")
	default:
		return warning
	}
}

func untitledSessionLabel(title string) string {
	if title == "Untitled session" {
		return text("sessionUntitled")
	}
	return title
}

func pluginResultSummaryLabel(summary string) string {
	if summary == "Command completed" {
		return text("automationsCommandCompleted")
	}
	return summary
}
