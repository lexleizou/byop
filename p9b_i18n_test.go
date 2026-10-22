package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"byop/locales"

	"github.com/egoist/mygo/i18n"
)

func TestP9bLanguagePackParityAndMessages(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("locales", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no language packs found")
	}

	type packData struct {
		messages     map[string]string
		descriptions map[string]string
	}
	readPack := func(t *testing.T, filename string) packData {
		t.Helper()
		data, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("parse %s: %v", filename, err)
		}
		pack := packData{
			messages:     make(map[string]string),
			descriptions: make(map[string]string),
		}
		for key, value := range raw {
			if key == "@@locale" {
				continue
			}
			if strings.HasPrefix(key, "@") {
				var metadata struct {
					Description string `json:"description"`
				}
				if err := json.Unmarshal(value, &metadata); err != nil {
					t.Fatalf("parse %s metadata %s: %v", filename, key, err)
				}
				pack.descriptions[strings.TrimPrefix(key, "@")] = strings.TrimSpace(metadata.Description)
				continue
			}
			var message string
			if err := json.Unmarshal(value, &message); err != nil {
				t.Fatalf("parse %s key %s: %v", filename, key, err)
			}
			pack.messages[key] = message
		}
		return pack
	}

	englishPath := filepath.Join("locales", "en.json")
	english := readPack(t, englishPath)
	englishKeys := sortedMessageKeys(english.messages)
	for _, filename := range files {
		filename := filename
		t.Run(filepath.Base(filename), func(t *testing.T) {
			pack := readPack(t, filename)
			difference(t, filepath.Base(filename), englishKeys, sortedMessageKeys(pack.messages))
			for key, message := range pack.messages {
				if _, err := i18n.ParseMessage(message); err != nil {
					t.Errorf("%s key %s does not parse: %v", filepath.Base(filename), key, err)
				}
				if pack.descriptions[key] == "" {
					t.Errorf("%s key %s has no @%s description", filepath.Base(filename), key, key)
				}
			}
			for key := range pack.descriptions {
				if _, ok := pack.messages[key]; !ok {
					t.Errorf("%s has metadata @%s without a message", filepath.Base(filename), key)
				}
			}
		})
	}
}

func sortedMessageKeys(pack map[string]string) []string {
	keys := make([]string, 0, len(pack))
	for key := range pack {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func difference(t *testing.T, name string, english, translated []string) {
	t.Helper()
	if reflect.DeepEqual(english, translated) {
		return
	}
	englishSet := make(map[string]struct{}, len(english))
	translatedSet := make(map[string]struct{}, len(translated))
	for _, key := range english {
		englishSet[key] = struct{}{}
	}
	for _, key := range translated {
		translatedSet[key] = struct{}{}
	}
	var missing, extra []string
	for _, key := range english {
		if _, ok := translatedSet[key]; !ok {
			missing = append(missing, key)
		}
	}
	for _, key := range translated {
		if _, ok := englishSet[key]; !ok {
			extra = append(extra, key)
		}
	}
	t.Fatalf("%s key mismatch: missing=%v extra=%v", name, missing, extra)
}

func TestP9bQueueLabelsResolve(t *testing.T) {
	for _, key := range []string{"queueSteerLabel", "queueFollowUpLabel"} {
		for _, locale := range []string{"en", "zh-Hans"} {
			if got := locales.Catalog.Format(locale, key, nil); got == key {
				t.Errorf("locale %s key %s resolved to raw key", locale, key)
			}
		}
	}
}

func TestP9bUILanguagePreferenceNormalization(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{value: "system", want: "system"},
		{value: "zh-Hans", want: "zh-Hans"},
		{value: "en", want: "en"},
		{value: "garbage", want: "system"},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			preferences := defaultByopPreferences()
			preferences.UILanguage = test.value
			normalizeByopPreferences(&preferences)
			if preferences.UILanguage != test.want {
				t.Fatalf("UILanguage = %q, want %q", preferences.UILanguage, test.want)
			}
		})
	}
}

func TestP9bPluginLocalizationResolution(t *testing.T) {
	root := t.TempDir()
	writeLocalePack := func(locale, value string) {
		t.Helper()
		l10nDirectory := filepath.Join(root, "l10n")
		if err := os.MkdirAll(l10nDirectory, 0o700); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(map[string]string{"title": value})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(l10nDirectory, locale+".json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeLocalePack("en", "English title")
	writeLocalePack("zh-Hans", "简体标题")

	manifest, err := loadPluginManifest(writeP9bManifest(t, root))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		locale string
		raw    string
		want   string
	}{
		{name: "locale match", locale: "zh-Hans", raw: "%title%", want: "简体标题"},
		{name: "English fallback", locale: "ja", raw: "%title%", want: "English title"},
		{name: "raw fallback", locale: "zh-Hans", raw: "%missing%", want: "%missing%"},
		{name: "plain text", locale: "zh-Hans", raw: "Plain title", want: "Plain title"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := pluginLocalizedTextForLocale(manifest, test.locale, test.raw); got != test.want {
				t.Fatalf("localized text = %q, want %q", got, test.want)
			}
		})
	}
}

func TestP9bPluginLocalizationRejectsPathEscape(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "plugin")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := &pluginManifest{
		L10n:            "../outside",
		sourceDirectory: root,
		localization: &pluginLocalizationCache{
			locales: make(map[string]map[string]string),
		},
	}
	if got := pluginLocalizedTextForLocale(manifest, "en", "%title%"); got != "%title%" {
		t.Fatalf("path escape resolved to %q, want raw fallback", got)
	}
}

func TestP9bPluginLocalizationRejectsOversizeFile(t *testing.T) {
	root := t.TempDir()
	l10nDirectory := filepath.Join(root, "l10n")
	if err := os.MkdirAll(l10nDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]string{
		"title": strings.Repeat("x", maxPluginLocaleFileBytes),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l10nDirectory, "en.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := loadPluginManifest(writeP9bManifest(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if got := pluginLocalizedTextForLocale(manifest, "en", "%title%"); got != "%title%" {
		t.Fatalf("oversize locale file resolved to %q, want raw fallback", got)
	}
}

func writeP9bManifest(t *testing.T, root string) string {
	t.Helper()
	manifest := map[string]any{
		"schemaVersion": 1,
		"id":            "example.p9b",
		"name":          "%title%",
		"version":       "0.1.0",
		"l10n":          "l10n",
		"runtime":       "node",
		"entry":         "plugin.mjs",
		"compatibility": map[string]any{"host": ">=0.9.0 <1.0.0", "pluginApi": "1"},
		"contributes":   map[string]any{},
		"capabilities":  map[string]any{},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(root, pluginManifestFilename)
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return filename
}
