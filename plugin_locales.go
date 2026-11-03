package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing/fstest"

	"byop/locales"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
)

// Plugin locales are standalone i18n catalogs. i18n.NewCatalog does not
// Register a catalog, so a plugin can never change the built-in one; the host
// instead keeps one immutable snapshot of the enabled resources and replaces it
// wholesale on every scan, which is what lets a disabled or removed plugin's
// translations be released at once.

// pluginLocaleCatalog is one enabled locale resource of one plugin.
type pluginLocaleCatalog struct {
	PluginID     string
	Contribution string
	Tag          string
	catalog      *i18n.Catalog
}

// pluginLocaleSnapshot is the immutable resource overlay of one scan.
type pluginLocaleSnapshot struct {
	// tags lists every language the app can show: the built-in catalog's
	// languages with its source language first, then the canonical tags of
	// the resources in catalogs. Application.SetLanguages replaces the
	// supported list with this, so it is also what normalisation, system
	// matching and the settings choices use.
	tags []string
	// catalogs maps a canonical tag to the catalog that provides it.
	catalogs map[string]*i18n.Catalog
	// owned lists the tags a plugin contributed, for diagnostics.
	owned []pluginLocaleCatalog
}

// buildPluginLocaleSnapshot reads the locale resources of the enabled entries
// in registry order. It reports one issue per resource it cannot use, in the
// existing plugin issue form, and keeps going so one bad file cannot take the
// other languages with it. A tag the built-in catalog already ships keeps the
// built-in catalog, so a plugin can add languages but not shadow the app's own.
func buildPluginLocaleSnapshot(
	entries []pluginRegistryEntry,
) (*pluginLocaleSnapshot, []pluginRegistryIssue) {
	builtin := locales.Catalog.Languages()
	snapshot := &pluginLocaleSnapshot{
		tags:     append([]string(nil), builtin...),
		catalogs: make(map[string]*i18n.Catalog),
	}
	var issues []pluginRegistryIssue
	for _, entry := range entries {
		if entry.Status != pluginRegistered || entry.Manifest == nil {
			continue
		}
		for _, contribution := range entry.Manifest.Contributes.Locales {
			canonical, err := pluginLocaleTagCanonical(contribution.Tag)
			if err != nil {
				issues = append(issues, pluginRegistryIssue{
					Path:    entry.ManifestPath,
					Message: "locale contribution tag is invalid: " + contribution.ID,
				})
				continue
			}
			if stringListContains(snapshot.tags, canonical) {
				// Already a language the app can show, either a built-in one or
				// one an earlier plugin provided. The first owner wins so a
				// rescan cannot flip a language between two plugins.
				continue
			}
			catalog, err := loadPluginLocaleCatalog(entry.SourceDirectory, contribution)
			if err != nil {
				issues = append(issues, pluginRegistryIssue{
					Path: entry.ManifestPath,
					Message: fmt.Sprintf("locale contribution %s cannot be used: %v",
						contribution.ID, err),
				})
				continue
			}
			snapshot.tags = append(snapshot.tags, canonical)
			snapshot.catalogs[canonical] = catalog
			snapshot.owned = append(snapshot.owned, pluginLocaleCatalog{
				PluginID:     entry.ID,
				Contribution: contribution.ID,
				Tag:          canonical,
				catalog:      catalog,
			})
		}
	}
	return snapshot, issues
}

// loadPluginLocaleCatalog reads one locale file into a standalone catalog. The
// file is named for its canonical tag because that is what a catalog reads its
// languages from; the @@locale value inside the file is not consulted.
func loadPluginLocaleCatalog(sourceDirectory string,
	contribution pluginLocaleContribution) (*i18n.Catalog, error) {
	canonical, err := pluginLocaleTagCanonical(contribution.Tag)
	if err != nil {
		return nil, err
	}
	filename, info, err := resolvePluginPath(sourceDirectory, contribution.Entry)
	if err != nil {
		return nil, fmt.Errorf("entry %s cannot be read", contribution.Entry)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("entry %s is not a regular file", contribution.Entry)
	}
	if info.Size() > maxPluginLocaleFileBytes {
		return nil, fmt.Errorf("entry %s is larger than %d bytes",
			contribution.Entry, maxPluginLocaleFileBytes)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("entry %s cannot be read", contribution.Entry)
	}
	if len(data) > maxPluginLocaleFileBytes {
		return nil, fmt.Errorf("entry %s is larger than %d bytes",
			contribution.Entry, maxPluginLocaleFileBytes)
	}
	// A catalog reads a language file when it first formats a message in it and
	// silently omits what does not parse, so the file is checked here instead: a
	// resource the host cannot use at all must be an ordinary plugin issue now,
	// not a language that appears to work and says nothing.
	if err := pluginLocaleFileBody(data); err != nil {
		return nil, fmt.Errorf("entry %s %w", contribution.Entry, err)
	}
	catalog, err := i18n.NewCatalog(fstest.MapFS{
		canonical + ".json": &fstest.MapFile{Data: data},
	}, canonical)
	if err != nil {
		return nil, err
	}
	return catalog, nil
}

// pluginLocaleFileBody checks the shape of one locale file: a JSON object whose
// values are all strings. The @@locale field is not consulted, because a catalog
// names its languages by file name.
func pluginLocaleFileBody(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("is not a JSON object: %w", err)
	}
	if len(fields) == 0 {
		return errors.New("holds no messages")
	}
	for key, raw := range fields {
		if strings.HasPrefix(key, "@@") {
			continue
		}
		if strings.HasPrefix(key, "@") {
			continue
		}
		var message string
		if err := json.Unmarshal(raw, &message); err != nil {
			return fmt.Errorf("has a message %s that is not a string", key)
		}
	}
	return nil
}

// text says key in locale from the resources this snapshot carries, reporting
// false when none of them has it so the caller falls back to the built-in
// catalog. The lookup is the catalog's own, so parent-language and source
// fallback inside one resource stay as MyGo defines them.
func (snapshot *pluginLocaleSnapshot) text(
	locale, key string, args map[string]any,
) (string, bool) {
	if snapshot == nil || key == "" {
		return "", false
	}
	// Match against every language the app can show, not only the tags a
	// plugin provides: CLDR would let a regional variant shadow the built-in
	// language it matches. The matched tag must then be a plugin tag, so the
	// built-in catalog keeps its own languages.
	tag := i18n.Match([]string{locale}, snapshot.tags)
	if tag == "" || i18n.IsPseudo(locale) || snapshot.catalogs[tag] == nil {
		return "", false
	}
	catalog := snapshot.catalogs[tag]
	if _, _, exists := catalog.Message(locale, key); !exists {
		return "", false
	}
	return catalog.Format(locale, key, args), true
}

// pluginTags returns only the tags a plugin provides, without the built-in
// languages.
func (snapshot *pluginLocaleSnapshot) pluginTags() []string {
	if snapshot == nil {
		return nil
	}
	tags := make([]string, 0, len(snapshot.owned))
	for _, owned := range snapshot.owned {
		tags = append(tags, owned.Tag)
	}
	return tags
}

// pluginLocaleResources holds the current overlay behind one atomic pointer, so
// a scan replaces it wholesale and every reader sees either the old snapshot or
// the new one, never a partially updated one.
type pluginLocaleResources struct {
	current atomic.Pointer[pluginLocaleSnapshot]
	// published records the language list last handed to the application, so a
	// scan that changes no language costs nothing. A nil value means the
	// application still has its default list.
	publishedMu sync.Mutex
	published   []string
}

// snapshot returns the current overlay, or nil before the first one.
func (resources *pluginLocaleResources) snapshot() *pluginLocaleSnapshot {
	if resources == nil {
		return nil
	}
	return resources.current.Load()
}

// publish replaces the overlay. A nil snapshot means no plugin resource is in
// use, which every consumer then answers from the built-in catalog.
func (resources *pluginLocaleResources) publish(snapshot *pluginLocaleSnapshot) {
	if resources == nil {
		return
	}
	if snapshot == nil {
		snapshot = &pluginLocaleSnapshot{
			tags:     append([]string(nil), locales.Catalog.Languages()...),
			catalogs: make(map[string]*i18n.Catalog),
		}
	}
	resources.current.Store(snapshot)
}

// applyLanguages hands tags to the application so SetLocale and the shown
// locale can choose among them, and reports whether the list changed. The
// application replaces its supported list rather than adding to it, so this is
// the only place that states which languages the app can show.
func (resources *pluginLocaleResources) applyLanguages(tags []string) bool {
	if resources == nil {
		return false
	}
	resources.publishedMu.Lock()
	defer resources.publishedMu.Unlock()
	if resources.published != nil && stringListsEqual(resources.published, tags) {
		return false
	}
	mygo.App.SetLanguages(tags...)
	resources.published = append([]string(nil), tags...)
	return true
}

// availableLanguages lists the languages the app can show right now: the
// built-in languages when no plugin resource is published.
func (resources *pluginLocaleResources) availableLanguages() []string {
	if snapshot := resources.snapshot(); snapshot != nil && len(snapshot.tags) > 0 {
		return append([]string(nil), snapshot.tags...)
	}
	return append([]string(nil), locales.Catalog.Languages()...)
}

// text formats key in locale from the published resources, reporting false when
// no plugin resource has the key so the caller uses the built-in catalog.
func (resources *pluginLocaleResources) text(
	locale, key string, args map[string]any,
) (string, bool) {
	return resources.snapshot().text(locale, key, args)
}

// stringListContains reports whether values already hold value, ignoring case
// so a tag written another way by a manifest cannot slip past the guard.
func stringListContains(values []string, value string) bool {
	for _, candidate := range values {
		if strings.EqualFold(candidate, value) {
			return true
		}
	}
	return false
}

func stringListsEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
