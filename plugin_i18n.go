package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"strings"

	"byop/locales"

	"github.com/egoist/mygo/i18n"
)

const maxPluginLocaleFileBytes = 256 << 10

func pluginLocalizedText(manifest *pluginManifest, raw string) string {
	return pluginLocalizedTextForLocale(manifest, i18n.Locale(), raw)
}

func pluginLocalizedTextForLocale(manifest *pluginManifest, locale, raw string) string {
	if manifest == nil || manifest.L10n == "" || !pluginPlaceholderKey(raw) {
		return raw
	}
	key := raw[1 : len(raw)-1]
	if translations := pluginLocaleTranslations(manifest, locale); translations != nil {
		if value, ok := translations[key]; ok {
			return value
		}
	}
	if translations := pluginLocaleTranslations(manifest, locales.Catalog.Source()); translations != nil {
		if value, ok := translations[key]; ok {
			return value
		}
	}
	return raw
}

func pluginPlaceholderKey(raw string) bool {
	if len(raw) < 3 || raw[0] != '%' || raw[len(raw)-1] != '%' {
		return false
	}
	key := raw[1 : len(raw)-1]
	return key != "" && !strings.ContainsRune(key, '%')
}

func pluginLocaleTranslations(manifest *pluginManifest, locale string) map[string]string {
	if manifest == nil || manifest.L10n == "" || manifest.localization == nil {
		return nil
	}
	tag := i18n.Match([]string{locale}, locales.Catalog.Languages())
	if tag == "" {
		return nil
	}
	cache := manifest.localization
	cache.mu.Lock()
	if translations, ok := cache.locales[tag]; ok {
		cache.mu.Unlock()
		return translations
	}
	cache.mu.Unlock()

	translations := loadPluginLocaleFile(manifest, tag)
	cache.mu.Lock()
	if existing, ok := cache.locales[tag]; ok {
		translations = existing
	} else {
		cache.locales[tag] = translations
	}
	cache.mu.Unlock()
	return translations
}

func loadPluginLocaleFile(manifest *pluginManifest, tag string) map[string]string {
	filename, info, err := resolvePluginPath(manifest.sourceDirectory,
		path.Join(manifest.L10n, tag+".json"))
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxPluginLocaleFileBytes {
		return nil
	}
	data, err := os.ReadFile(filename)
	if err != nil || len(data) > maxPluginLocaleFileBytes {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	translations := make(map[string]string)
	if err := decoder.Decode(&translations); err != nil {
		return nil
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil
	}
	for key, value := range translations {
		if strings.TrimSpace(key) == "" || pluginTextInvalid(value, 500) {
			return nil
		}
	}
	return translations
}

func pluginLocalizedEntry(entry pluginRegistryEntry) pluginRegistryEntry {
	if entry.Manifest == nil {
		return entry
	}
	manifest := localizedPluginManifest(entry.Manifest)
	entry.Manifest = manifest
	entry.Name = manifest.Name
	entry.Description = manifest.Description
	return entry
}

func localizedPluginManifest(manifest *pluginManifest) *pluginManifest {
	if manifest == nil {
		return nil
	}
	localized := *manifest
	localized.Name = pluginLocalizedText(manifest, manifest.Name)
	localized.Description = pluginLocalizedText(manifest, manifest.Description)
	localized.Contributes.Navigation = append([]pluginNavigationContribution(nil),
		manifest.Contributes.Navigation...)
	for index := range localized.Contributes.Navigation {
		localized.Contributes.Navigation[index].Title =
			pluginLocalizedText(manifest, manifest.Contributes.Navigation[index].Title)
	}
	localized.Contributes.Commands = append([]pluginCommandContribution(nil),
		manifest.Contributes.Commands...)
	for index := range localized.Contributes.Commands {
		command := &localized.Contributes.Commands[index]
		command.Title = pluginLocalizedText(manifest, command.Title)
		command.Description = pluginLocalizedText(manifest, command.Description)
	}
	localized.Contributes.Views = append([]pluginViewContribution(nil),
		manifest.Contributes.Views...)
	for index := range localized.Contributes.Views {
		localized.Contributes.Views[index].Title =
			pluginLocalizedText(manifest, manifest.Contributes.Views[index].Title)
	}
	localized.Contributes.TaskPanels = append([]pluginTaskPanelContribution(nil),
		manifest.Contributes.TaskPanels...)
	for index := range localized.Contributes.TaskPanels {
		localized.Contributes.TaskPanels[index].Title =
			pluginLocalizedText(manifest, manifest.Contributes.TaskPanels[index].Title)
	}
	localized.Contributes.Settings = append([]pluginSettingContribution(nil),
		manifest.Contributes.Settings...)
	for index := range localized.Contributes.Settings {
		settings := &localized.Contributes.Settings[index]
		settings.Title = pluginLocalizedText(manifest, settings.Title)
		settings.Description = pluginLocalizedText(manifest, settings.Description)
		settings.Fields = append([]pluginSettingField(nil), settings.Fields...)
		for fieldIndex := range settings.Fields {
			field := &settings.Fields[fieldIndex]
			field.Label = pluginLocalizedText(manifest, field.Label)
			field.Description = pluginLocalizedText(manifest, field.Description)
			field.Placeholder = pluginLocalizedText(manifest, field.Placeholder)
			field.Options = append([]string(nil), field.Options...)
		}
	}
	localized.Contributes.WebViews = append([]pluginWebViewContribution(nil),
		manifest.Contributes.WebViews...)
	for index := range localized.Contributes.WebViews {
		localized.Contributes.WebViews[index].Title =
			pluginLocalizedText(manifest, manifest.Contributes.WebViews[index].Title)
	}
	localized.Contributes.Automations = append([]pluginAutomationContribution(nil),
		manifest.Contributes.Automations...)
	for index := range localized.Contributes.Automations {
		automation := &localized.Contributes.Automations[index]
		automation.Title = pluginLocalizedText(manifest, automation.Title)
		automation.Description = pluginLocalizedText(manifest, automation.Description)
	}
	return &localized
}
