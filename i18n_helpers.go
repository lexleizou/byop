package main

import (
	"os"
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
