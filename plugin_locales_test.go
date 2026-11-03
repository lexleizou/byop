package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"byop/locales"

	"github.com/egoist/mygo/i18n"
)

// The locale consumer is checked without the UI framework. textIn takes the
// language on purpose: the running application owns the shown language, so the
// overlay resolution is the part that can be proven here.

const localeFixtureTag = "fr"

const localeFixtureFrench = `{
  "@@locale": "fr",
  "appWindowTitle": "Atelier Pi",
  "pluginScanFound": "{registered, plural, one {# module enregistré} other {# modules enregistrés}}, {contributions, plural, one {# contribution} other {# contributions}}",
  "fixtureOnlyKey": "Traduction du fixture"
}`

// localeFixtureFile is one declared locale resource and the file body the
// fixture writes for it. An empty body declares the contribution but writes no
// file, which is how a missing or unreadable entry is reproduced.
type localeFixtureFile struct {
	id    string
	tag   string
	entry string
	body  string
}

// writeLocaleFixturePlugin writes one plugin folder that declares the given
// locale resources, and scans it. No resource declared means no locale slot at
// all, which is how a rescan loses every plugin language.
func writeLocaleFixturePlugin(t *testing.T, files ...localeFixtureFile) pluginRegistry {
	t.Helper()
	root := t.TempDir()
	manifest := validPluginManifestMap()
	declared := manifest["contributes"].(map[string]any)
	delete(declared, "navigation")
	delete(declared, "views")
	delete(declared, "taskPanels")
	delete(declared, "settings")
	delete(declared, "webviews")
	manifest["activation"] = map[string]any{"events": []any{}}
	manifest["description"] = "Locale consumer fixture."
	directory := filepath.Join(root, "locale-fixture")
	if len(files) > 0 {
		contributions := make([]any, 0, len(files))
		for _, file := range files {
			contributions = append(contributions, map[string]any{
				"id": file.id, "title": strings.ToUpper(file.tag),
				"tag": file.tag, "entry": file.entry,
			})
			target := filepath.Join(directory, filepath.FromSlash(file.entry))
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				t.Fatal(err)
			}
			if file.body == "" {
				continue
			}
			if err := os.WriteFile(target, []byte(file.body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		declared["locales"] = contributions
	}
	writePluginManifest(t, root, "locale-fixture", manifest)
	return scanPluginRegistry([]pluginRootSpec{{Path: root, Explicit: true}})
}

// publishLocaleFixture publishes one scanned registry for the duration of the
// test and restores the previous overlay afterwards, so no other test sees a
// plugin language or a changed supported list.
func publishLocaleFixture(t *testing.T, registry pluginRegistry) pluginRegistry {
	t.Helper()
	previous := appLocaleResources.snapshot()
	previousPublished := appLocaleResources.published
	registry = withPluginLocaleResources(registry)
	t.Cleanup(func() {
		appLocaleResources.publish(previous)
		appLocaleResources.publishedMu.Lock()
		appLocaleResources.published = previousPublished
		appLocaleResources.publishedMu.Unlock()
	})
	return registry
}

func TestPluginLocaleFixtureManifestIsValid(t *testing.T) {
	directory := filepath.Join("examples", "locale-fixture")
	manifest, err := loadPluginManifest(filepath.Join(directory, pluginManifestFilename))
	if err != nil {
		t.Fatalf("load fixture manifest: %v", err)
	}
	if len(manifest.Contributes.Locales) != 1 {
		t.Fatalf("fixture locale contributions = %d, want 1", len(manifest.Contributes.Locales))
	}
	contribution := manifest.Contributes.Locales[0]
	if contribution.Tag != "fr" || contribution.Entry != "locales/fr.json" {
		t.Fatalf("fixture locale contribution = %+v", contribution)
	}
	catalog, err := loadPluginLocaleCatalog(directory, contribution)
	if err != nil {
		t.Fatalf("load fixture catalog: %v", err)
	}
	if got := catalog.Format("fr", "fixtureOnlyKey", nil); got != "Traduction du fixture" {
		t.Fatalf("fixture key = %q", got)
	}
	// Nothing declares an activation event, so a resource-only plugin has no way
	// to start a worker.
	if len(manifest.Activation.Events) != 0 {
		t.Fatalf("fixture activation events = %v, want none", manifest.Activation.Events)
	}
}

func TestPluginLocaleOverlayProvidesLanguageAndFallsBack(t *testing.T) {
	registry := publishLocaleFixture(t, writeLocaleFixturePlugin(t,
		localeFixtureFile{"locale.fr", "fr", "locales/fr.json", localeFixtureFrench}))
	if len(registry.Issues) != 0 {
		t.Fatalf("fixture issues = %v", registry.Issues)
	}
	if !stringListContains(availableUILanguages(), localeFixtureTag) {
		t.Fatalf("available languages = %v, want fr", availableUILanguages())
	}
	// A published language is what makes it selectable at all: the application
	// matches SetLocale only against the list it was given.
	if got := i18n.Match([]string{"fr"}, availableUILanguages()); got != "fr" {
		t.Fatalf("match fr = %q", got)
	}
	// The list still starts with the built-in source language, so an unusable
	// choice keeps falling back to English.
	if languages := availableUILanguages(); len(languages) == 0 ||
		languages[0] != locales.Catalog.Source() {
		t.Fatalf("available languages = %v, want %s first",
			languages, locales.Catalog.Source())
	}
	if got := textIn(localeFixtureTag, "appWindowTitle", nil); got != "Atelier Pi" {
		t.Fatalf("overridden text = %q, want the plugin translation", got)
	}
	// A missing argument still renders the plugin message, which is what a
	// language that translates only some keys does in practice.
	if got := textIn(localeFixtureTag, "pluginScanFound", nil); got == "pluginScanFound" {
		t.Fatalf("plugin plural message did not format: %q", got)
	}
	// A key no plugin resource has still comes from the built-in catalog.
	if got, want := textIn(localeFixtureTag, "settingsLanguageTitle", nil),
		locales.In(localeFixtureTag).SettingsLanguageTitle(); got != want {
		t.Fatalf("fallback text = %q, want %q", got, want)
	}
	if got := textIn(localeFixtureTag, "fixtureOnlyKey", nil); got != "Traduction du fixture" {
		t.Fatalf("plugin-only text = %q", got)
	}
}

func TestPluginLocaleOverlayFormatsArguments(t *testing.T) {
	publishLocaleFixture(t, writeLocaleFixturePlugin(t,
		localeFixtureFile{"locale.fr", "fr", "locales/fr.json", localeFixtureFrench}))
	if got := textIn(localeFixtureTag, "pluginScanFound",
		map[string]any{"registered": 3, "contributions": 1}); got != "3 modules enregistrés, 1 contribution" {
		t.Fatalf("formatted text = %q", got)
	}
	if got := textIn(localeFixtureTag, "pluginScanFound",
		map[string]any{"registered": 1, "contributions": 2}); got != "1 module enregistré, 2 contributions" {
		t.Fatalf("singular text = %q", got)
	}
}

func TestPluginLocaleOverlayReplacesSameTagOnRescan(t *testing.T) {
	publishLocaleFixture(t, writeLocaleFixturePlugin(t,
		localeFixtureFile{"locale.fr", "fr", "locales/fr.json", localeFixtureFrench}))
	if got := textIn(localeFixtureTag, "appWindowTitle", nil); got != "Atelier Pi" {
		t.Fatalf("first overlay text = %q", got)
	}
	// A rescan that edited the same tag in place must show the new text and drop
	// the keys the new file no longer has, so a live update retains nothing.
	rescanned := publishLocaleFixture(t, writeLocaleFixturePlugin(t,
		localeFixtureFile{"locale.fr", "fr", "locales/fr.json",
			`{"@@locale":"fr","appWindowTitle":"Atelier Pi V2"}`}))
	if len(rescanned.Issues) != 0 {
		t.Fatalf("rescan issues = %v", rescanned.Issues)
	}
	if got := textIn(localeFixtureTag, "appWindowTitle", nil); got != "Atelier Pi V2" {
		t.Fatalf("replaced text = %q, want the rescan's translation", got)
	}
	// fixtureOnlyKey exists in no built-in language, so the dropped key answers
	// with the key itself: the fallback path, not a retained translation.
	if got := textIn(localeFixtureTag, "fixtureOnlyKey", nil); got != "fixtureOnlyKey" {
		t.Fatalf("dropped key = %q, want the key itself", got)
	}
}

func TestPluginLocaleOverlayDropsRemovedLanguage(t *testing.T) {
	publishLocaleFixture(t, writeLocaleFixturePlugin(t,
		localeFixtureFile{"locale.fr", "fr", "locales/fr.json", localeFixtureFrench}))
	if !stringListContains(availableUILanguages(), localeFixtureTag) {
		t.Fatal("fixture language is not available before removal")
	}
	// A removed or disabled plugin is the same publish step with no resource, so
	// its language and its translations go at once.
	publishLocaleFixture(t, writeLocaleFixturePlugin(t))
	if stringListContains(availableUILanguages(), localeFixtureTag) {
		t.Fatalf("languages after removal = %v, want no fr", availableUILanguages())
	}
	if got, want := textIn(localeFixtureTag, "appWindowTitle", nil),
		locales.In(localeFixtureTag).AppWindowTitle(); got != want {
		t.Fatalf("text after removal = %q, want the built-in fallback %q", got, want)
	}
}

func TestPluginLocaleInvalidFileReportsIssueAndFallsBack(t *testing.T) {
	registry := publishLocaleFixture(t, writeLocaleFixturePlugin(t,
		localeFixtureFile{"locale.fr", "fr", "locales/fr.json", "{ not json"}))
	if len(registry.Issues) == 0 {
		t.Fatal("an unusable locale file reported no issue")
	}
	if !strings.Contains(registry.Issues[0].Message, "locale contribution") {
		t.Fatalf("issue message = %q", registry.Issues[0].Message)
	}
	// The language is never offered, so the shown text stays the built-in one.
	if stringListContains(availableUILanguages(), localeFixtureTag) {
		t.Fatalf("languages = %v, want no fr for an unusable file", availableUILanguages())
	}
	if got, want := textIn(localeFixtureTag, "appWindowTitle", nil),
		locales.In(localeFixtureTag).AppWindowTitle(); got != want {
		t.Fatalf("text with an unusable file = %q, want %q", got, want)
	}
}

func TestPluginLocaleMissingFileReportsIssue(t *testing.T) {
	registry := publishLocaleFixture(t, writeLocaleFixturePlugin(t,
		localeFixtureFile{"locale.fr", "fr", "locales/fr.json", ""}))
	if len(registry.Issues) == 0 {
		t.Fatal("a missing locale file reported no issue")
	}
	if stringListContains(availableUILanguages(), localeFixtureTag) {
		t.Fatalf("languages = %v, want no fr for a missing file", availableUILanguages())
	}
}

func TestPluginLocaleBuiltinTagCannotBeShadowed(t *testing.T) {
	registry := publishLocaleFixture(t, writeLocaleFixturePlugin(t,
		localeFixtureFile{"locale.en", "en", "locales/en.json",
			`{"@@locale":"en","appWindowTitle":"Shadowed"}`}))
	if len(registry.Issues) != 0 {
		t.Fatalf("built-in tag issues = %v", registry.Issues)
	}
	// The app's own language stays the app's own: a plugin adds languages, it does
	// not silently rewrite the source language.
	if got := textIn("en", "appWindowTitle", nil); got != locales.In("en").AppWindowTitle() {
		t.Fatalf("built-in tag text = %q, want the built-in message", got)
	}
}

func TestPluginLocaleRegionalVariantCannotShadowBuiltin(t *testing.T) {
	registry := publishLocaleFixture(t, writeLocaleFixturePlugin(t,
		localeFixtureFile{"locale.en-gb", "en-GB", "locales/en-GB.json",
			`{"@@locale":"en-GB","appWindowTitle":"Shadowed-GB"}`}))
	if len(registry.Issues) != 0 {
		t.Fatalf("regional fixture issues = %v", registry.Issues)
	}
	if got := textIn("en-GB", "appWindowTitle", nil); got != "Shadowed-GB" {
		t.Fatalf("regional plugin text = %q, want the plugin resource", got)
	}
	// A regional variant of a built-in language resolves to the built-in tag,
	// so the plugin overlay cannot answer for the built-in language itself.
	if got, want := textIn("en", "appWindowTitle", nil),
		locales.In("en").AppWindowTitle(); got != want {
		t.Fatalf("built-in text after en-GB = %q, want %q", got, want)
	}
	// CLDR resolves zh to zh-Hans, the built-in Simplified Chinese tag.
	if got, want := textIn("zh-CN", "appWindowTitle", nil),
		locales.In("zh-Hans").AppWindowTitle(); got != want {
		t.Fatalf("zh-CN fallback = %q, want built-in zh-Hans %q", got, want)
	}
}

func TestSavedUILanguageKeepsUnavailableChoice(t *testing.T) {
	previous := appLocaleResources.snapshot()
	previousPublished := appLocaleResources.published
	registry := writeLocaleFixturePlugin(t,
		localeFixtureFile{"locale.fr", "fr", "locales/fr.json", localeFixtureFrench})
	withPluginLocaleResources(registry)
	t.Cleanup(func() {
		appLocaleResources.publish(previous)
		appLocaleResources.publishedMu.Lock()
		appLocaleResources.published = previousPublished
		appLocaleResources.publishedMu.Unlock()
	})
	state := &appState{uiLanguage: localeFixtureTag}
	if got := availableUILanguage(state.uiLanguage); got != localeFixtureTag {
		t.Fatalf("available language = %q, want fr", got)
	}
	// Removing the resource leaves the saved choice untouched; the shown
	// language resolves to system until the tag is offered again.
	withPluginLocaleResources(writeLocaleFixturePlugin(t))
	if got := savedUILanguage(localeFixtureTag); got != localeFixtureTag {
		t.Fatalf("saved choice after removal = %q, want fr still saved", got)
	}
	if got := availableUILanguage(localeFixtureTag); got != "" {
		t.Fatalf("unavailable choice resolved to %q, want system", got)
	}
	// Re-enabling restores the saved tag without a preference write.
	withPluginLocaleResources(registry)
	if got := availableUILanguage(savedUILanguage(localeFixtureTag)); got != localeFixtureTag {
		t.Fatalf("choice after re-enable = %q, want fr", got)
	}
}

func TestPluginLocaleResourcesWithoutSnapshotUseBuiltinCatalog(t *testing.T) {
	resources := &pluginLocaleResources{}
	if got, ok := resources.text("fr", "appWindowTitle", nil); ok {
		t.Fatalf("empty overlay answered %q", got)
	}
	if languages := resources.availableLanguages(); len(languages) == 0 ||
		languages[0] != locales.Catalog.Source() {
		t.Fatalf("available languages = %v, want the built-in list", languages)
	}
	if got, want := textIn("zh-Hans", "appWindowTitle", nil),
		locales.In("zh-Hans").AppWindowTitle(); got != want {
		t.Fatalf("text without overlay = %q, want %q", got, want)
	}
}
