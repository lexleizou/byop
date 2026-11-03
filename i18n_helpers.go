package main

import (
	"os"
	"strings"

	"byop/locales"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
)

const uiLanguageSystem = "system"

// appLocaleResources is the one overlay of plugin locale resources the running
// app uses. A scan replaces its snapshot; nothing here mutates the built-in
// catalog, which has no way to replace or release a language.
var appLocaleResources = &pluginLocaleResources{}

// availableUILanguages lists the languages the app can show: the built-in
// languages with the source language first, then the canonical tags of the
// enabled plugin resources. Normalisation, system matching, the settings
// choices and Application.SetLanguages all use this one list, so a plugin
// language is selectable the moment its resource is published and stops being
// selectable the moment it is not.
func availableUILanguages() []string {
	return appLocaleResources.availableLanguages()
}

// withPluginLocaleResources publishes the locale resources of a freshly
// scanned registry and hands the resulting language list to the application,
// which matches SetLocale only against the list it was given. Any resource the
// scan could not use comes back recorded in the registry's own issues, so the
// plugins page reports it through the mechanism every other plugin problem
// already uses. Passing nil clears the overlay, which releases every plugin
// translation at once.
func withPluginLocaleResources(registry pluginRegistry) pluginRegistry {
	snapshot, issues := buildPluginLocaleSnapshot(registry.Entries)
	appLocaleResources.publish(snapshot)
	appLocaleResources.applyLanguages(availableUILanguages())
	if len(issues) > 0 {
		registry.Issues = append(append([]pluginRegistryIssue(nil),
			registry.Issues...), issues...)
	}
	return registry
}

func normalizeUILanguage(value string) string {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, uiLanguageSystem) {
		return uiLanguageSystem
	}
	for _, tag := range availableUILanguages() {
		if strings.EqualFold(value, tag) {
			return tag
		}
	}
	return uiLanguageSystem
}

// savedUILanguage keeps a valid language choice even when its resource is not
// published right now. The choice is a tag the application can offer again;
// only the effective language resolves against the current list.
func savedUILanguage(value string) string {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, uiLanguageSystem) {
		return uiLanguageSystem
	}
	if i18n.Normalize(value) != "" {
		return i18n.Normalize(value)
	}
	return uiLanguageSystem
}

// availableUILanguage returns the saved tag only when the resources can show
// it now; system is returned in every other case, including when the saved
// choice already means system.
func availableUILanguage(value string) string {
	if value == uiLanguageSystem {
		return ""
	}
	for _, tag := range availableUILanguages() {
		if strings.EqualFold(value, tag) {
			return tag
		}
	}
	return ""
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
	supported := availableUILanguages()
	if mygo.App.IsReady() {
		return i18n.Match(mygo.App.PreferredLanguages(), supported)
	}
	if tag := i18n.Match(strings.Fields(os.Getenv("LANGUAGE")), supported); tag != "" {
		return tag
	}
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if tag := i18n.Match([]string{os.Getenv(name)}, supported); tag != "" {
			return tag
		}
	}
	return i18n.Match([]string{os.Getenv("MYGO_LOCALE")}, supported)
}

func text(key string) string {
	return textIn(i18n.Locale(), key, nil)
}

func textArgs(key string, args map[string]any) string {
	return textIn(i18n.Locale(), key, args)
}

// textIn says key in locale, preferring the published plugin resources and
// falling back to the built-in catalog. text and textArgs use the language the
// app shows; the explicit form exists so the resolution can be checked without
// an application, which is what owns the shown language.
func textIn(locale, key string, args map[string]any) string {
	if value, ok := appLocaleResources.text(locale, key, args); ok {
		return value
	}
	return locales.Catalog.Format(locale, key, args)
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
