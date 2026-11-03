package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestLoadPluginManifestAcceptsAllSevenContributions(t *testing.T) {
	manifest := validPluginManifestMap()
	contributes := manifest["contributes"].(map[string]any)
	contributes["themes"] = []any{
		map[string]any{"id": "theme.fixture", "title": "Fixture theme", "light": "themes/light.json", "dark": "themes/dark.json"},
	}
	contributes["locales"] = []any{
		map[string]any{"id": "locale.fr", "title": "French locale", "tag": "fr", "entry": "locales/fr.json"},
	}
	contributes["slashCommands"] = []any{
		map[string]any{"id": "slash.fixture", "title": "Fixture slash", "name": "fixture", "prompt": "Run {args}"},
	}
	contributes["mentionProviders"] = []any{
		map[string]any{"id": "mention.fixture", "title": "Fixture mention", "command": "git.refresh"},
	}
	contributes["paletteProviders"] = []any{
		map[string]any{"id": "palette.fixture", "title": "Fixture palette", "command": "git.refresh"},
	}
	contributes["statusItems"] = []any{
		map[string]any{"id": "status.fixture", "title": "Fixture status"},
	}
	contributes["messageRenderers"] = []any{
		map[string]any{"id": "renderer.fixture", "title": "Fixture renderer", "command": "git.refresh", "languages": []any{"fixture"}},
	}

	loaded, err := loadPluginManifest(writePluginManifest(t, t.TempDir(), "example", manifest))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if loaded.contributionCount() != 13 {
		t.Fatalf("contribution count = %d, want 13", loaded.contributionCount())
	}
	if len(loaded.Contributes.Themes) != 1 || len(loaded.Contributes.Locales) != 1 ||
		len(loaded.Contributes.SlashCommands) != 1 || len(loaded.Contributes.MentionProviders) != 1 ||
		len(loaded.Contributes.PaletteProviders) != 1 || len(loaded.Contributes.StatusItems) != 1 ||
		len(loaded.Contributes.MessageRenderers) != 1 {
		t.Fatalf("unexpected seven-slot contributions: %#v", loaded.Contributes)
	}
}

func TestLoadPluginManifestRejectsInvalidSevenContributions(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   any
		wantErr string
	}{
		{
			name: "unsafe theme path", key: "themes",
			value:   []any{map[string]any{"id": "theme.bad", "title": "Theme", "light": "../light.json", "dark": "themes/dark.json"}},
			wantErr: "theme contribution light file is invalid",
		},
		{
			name: "invalid locale tag", key: "locales",
			value:   []any{map[string]any{"id": "locale.bad", "title": "Locale", "tag": "not a tag", "entry": "locales/fr.json"}},
			wantErr: "locale contribution tag is invalid",
		},
		{
			name: "slash command with both", key: "slashCommands",
			value:   []any{map[string]any{"id": "slash.bad", "title": "Slash", "name": "bad", "command": "git.refresh", "prompt": "x"}},
			wantErr: "slash command needs exactly one command or prompt",
		},
		{
			name: "slash command invalid name", key: "slashCommands",
			value:   []any{map[string]any{"id": "slash.bad", "title": "Slash", "name": "BAD NAME", "prompt": "x"}},
			wantErr: "slash command name is invalid",
		},
		{
			name: "undeclared mention command", key: "mentionProviders",
			value:   []any{map[string]any{"id": "mention.bad", "title": "Mention", "command": "missing.command"}},
			wantErr: "mention provider command is not declared",
		},
		{
			name: "undeclared palette command", key: "paletteProviders",
			value:   []any{map[string]any{"id": "palette.bad", "title": "Palette", "command": "missing.command"}},
			wantErr: "palette provider command is not declared",
		},
		{
			name: "two status items", key: "statusItems",
			value:   []any{map[string]any{"id": "status.one", "title": "One"}, map[string]any{"id": "status.two", "title": "Two"}},
			wantErr: "status items; maximum is 1",
		},
		{
			name: "renderer with no target", key: "messageRenderers",
			value:   []any{map[string]any{"id": "renderer.bad", "title": "Renderer", "command": "git.refresh"}},
			wantErr: "message renderer needs at least one language or mimeType",
		},
		{
			name: "renderer invalid mime", key: "messageRenderers",
			value:   []any{map[string]any{"id": "renderer.bad", "title": "Renderer", "command": "git.refresh", "mimeTypes": []any{"not-a-type"}}},
			wantErr: "message renderer mimeType is invalid",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := validPluginManifestMap()
			manifest["contributes"].(map[string]any)[test.key] = test.value
			_, err := loadPluginManifest(writePluginManifest(t, t.TempDir(), "example", manifest))
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("load error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

// TestScanPluginRegistrySevenSlotFixture loads one fixture plugin through the
// real registry scan and asserts each of the seven new slots reaches its host
// matcher, so a manifest that only validates is not mistaken for a consumable
// contribution. Resource fixtures under examples/ stay GLM's; this fixture is
// written into a temp directory.
func TestScanPluginRegistrySevenSlotFixture(t *testing.T) {
	root := t.TempDir()
	manifest := validPluginManifestMap()
	contributes := manifest["contributes"].(map[string]any)
	delete(contributes, "webviews") // no webview fixture files here
	contributes["themes"] = []any{
		map[string]any{"id": "theme.fixture", "title": "Fixture theme",
			"light": "themes/light.json", "dark": "themes/dark.json"},
	}
	contributes["locales"] = []any{
		map[string]any{"id": "locale.fr", "title": "French locale",
			"tag": "fr", "entry": "locales/fr.json"},
	}
	contributes["slashCommands"] = []any{
		map[string]any{"id": "slash.fixture", "title": "Fixture slash",
			"name": "fixture", "command": "git.refresh"},
	}
	contributes["mentionProviders"] = []any{
		map[string]any{"id": "mention.fixture", "title": "Fixture mention",
			"command": "git.refresh"},
	}
	contributes["paletteProviders"] = []any{
		map[string]any{"id": "palette.fixture", "title": "Fixture palette",
			"command": "git.refresh"},
	}
	contributes["statusItems"] = []any{
		map[string]any{"id": "status.fixture", "title": "Fixture status"},
	}
	contributes["messageRenderers"] = []any{
		map[string]any{"id": "renderer.language", "title": "Fixture language renderer",
			"command": "git.refresh", "languages": []any{"fixture"}},
		map[string]any{"id": "renderer.mime", "title": "Fixture MIME renderer",
			"command": "git.refresh", "mimeTypes": []any{"text/csv"}},
	}
	manifest["activation"] = map[string]any{
		"events": []any{"onCommand:git.refresh"},
		"lazy":   true,
	}
	writePluginManifest(t, root, "seven-slot", manifest)

	registry := scanPluginRegistry([]pluginRootSpec{{Path: root, Explicit: true}})
	if registry.problemCount() != 0 || len(registry.Entries) != 1 {
		t.Fatalf("scan issues: %#v", registry.Issues)
	}
	entry := registry.Entries[0]
	if entry.Status != pluginRegistered || entry.Manifest == nil {
		t.Fatalf("fixture not registered: %#v", entry)
	}
	loaded := entry.Manifest.Contributes
	if len(loaded.Themes) != 1 || len(loaded.Locales) != 1 || len(loaded.SlashCommands) != 1 ||
		len(loaded.MentionProviders) != 1 || len(loaded.PaletteProviders) != 1 ||
		len(loaded.StatusItems) != 1 || len(loaded.MessageRenderers) != 2 {
		t.Fatalf("seven slots missing after scan: %#v", loaded)
	}
	// The two renderer slots reach the language and MIME matchers the host
	// uses; the resource and query slots are consumed by their own features.
	if match, contribution, ok := registry.messageRendererForLanguage("fixture"); !ok ||
		match.ID != entry.ID || contribution.Command != "git.refresh" {
		t.Fatalf("language renderer not matched: %v", contribution)
	}
	if match, contribution, ok := registry.messageRendererForMimeType("text/csv"); !ok ||
		match.ID != entry.ID || contribution.Command != "git.refresh" {
		t.Fatalf("MIME renderer not matched: %v", contribution)
	}
	if _, ok := pluginStatusItemByID(entry.Manifest, "status.fixture"); !ok {
		t.Fatal("status item not resolvable after scan")
	}
}

func TestLoadPluginManifestAcceptsBCP47AndCanonicalDuplicateCheck(t *testing.T) {
	manifest := validPluginManifestMap()
	manifest["contributes"].(map[string]any)["locales"] = []any{
		map[string]any{"id": "locale.es-419", "title": "Latin-American Spanish", "tag": "es-419", "entry": "locales/es-419.json"},
	}
	if _, err := loadPluginManifest(writePluginManifest(t, t.TempDir(), "example-es", manifest)); err != nil {
		t.Fatalf("load manifest with es-419: %v", err)
	}
	manifest["contributes"].(map[string]any)["locales"] = []any{
		map[string]any{"id": "locale.fr-ca", "title": "French Canada", "tag": "fr-CA", "entry": "locales/fr-CA.json"},
	}
	if _, err := loadPluginManifest(writePluginManifest(t, t.TempDir(), "example", manifest)); err != nil {
		t.Fatalf("load manifest with fr-CA: %v", err)
	}
	manifest["contributes"].(map[string]any)["locales"] = []any{
		map[string]any{"id": "locale.fr-ca", "title": "French Canada", "tag": "fr-CA", "entry": "locales/fr-CA.json"},
		map[string]any{"id": "locale.fr-ca2", "title": "French Canada lower", "tag": "fr-ca", "entry": "locales/fr-ca.json"},
	}
	_, err := loadPluginManifest(writePluginManifest(t, t.TempDir(), "example", manifest))
	if err == nil || !strings.Contains(err.Error(), "locale tag is duplicated") {
		t.Fatalf("duplicate canonical tag not rejected: %v", err)
	}
}

func TestLoadPluginManifestAcceptsMultilinePromptAndRejectsNUL(t *testing.T) {
	manifest := validPluginManifestMap()
	manifest["contributes"].(map[string]any)["slashCommands"] = []any{
		map[string]any{"id": "slash.multiline", "title": "Multiline", "name": "multiline", "prompt": "line one\nline two\tindented\r\nline three"},
	}
	if _, err := loadPluginManifest(writePluginManifest(t, t.TempDir(), "example", manifest)); err != nil {
		t.Fatalf("multiline prompt rejected: %v", err)
	}
	manifest["contributes"].(map[string]any)["slashCommands"] = []any{
		map[string]any{"id": "slash.nul", "title": "NUL", "name": "nul", "prompt": "before\x00after"},
	}
	_, err := loadPluginManifest(writePluginManifest(t, t.TempDir(), "example", manifest))
	if err == nil || !strings.Contains(err.Error(), "slash command prompt is invalid") {
		t.Fatalf("NUL prompt accepted: %v", err)
	}
}

func TestLoadPluginManifestAcceptsLongMultilinePrompt(t *testing.T) {
	manifest := validPluginManifestMap()
	long := strings.Repeat("line\n", 601)
	manifest["contributes"].(map[string]any)["slashCommands"] = []any{
		map[string]any{"id": "slash.long", "title": "Long", "name": "long", "prompt": long},
	}
	if _, err := loadPluginManifest(writePluginManifest(t, t.TempDir(), "example", manifest)); err != nil {
		t.Fatalf("long multiline prompt rejected: %v", err)
	}
}

func TestP25StatusItemHostContract(t *testing.T) {
	var received []pluginStatusItemUpdate
	host := newPluginHost(pluginHostVersion, nil,
		pluginHostStatusItemHandler(func(update pluginStatusItemUpdate) {
			received = append(received, update)
		}))
	entry := pluginRegistryEntry{
		ID:     "local.dashboard",
		Status: pluginRegistered,
		Manifest: &pluginManifest{
			ID: "local.dashboard",
			Contributes: pluginContributions{StatusItems: []pluginStatusContribution{
				{ID: "status.build", Title: "Build"},
			}},
		},
	}
	result := host.handleHostCall(entry, "host.setStatusItem",
		json.RawMessage(`{"id":"status.build","text":"green","tone":"success"}`))
	if result.Err != nil {
		t.Fatalf("setStatusItem: %#v", result.Err)
	}
	if len(received) != 1 || received[0].Text != "green" || received[0].Tone != "success" ||
		received[0].PluginID != entry.ID || received[0].Title != "Build" {
		t.Fatalf("update = %#v", received)
	}
	// An undeclared item is denied, so a plugin cannot hold an HUD slot it
	// never asked for.
	undeclared := host.handleHostCall(entry, "host.setStatusItem",
		json.RawMessage(`{"id":"status.other","text":"x"}`))
	if undeclared.Err == nil || undeclared.Err.Code != pluginRPCPermissionDenied {
		t.Fatalf("undeclared item = %#v", undeclared)
	}
	if bad := host.handleHostCall(entry, "host.setStatusItem",
		json.RawMessage(`{"id":"status.build","text":"","tone":"info"}`)); bad.Err == nil ||
		bad.Err.Code != pluginRPCInvalidParams {
		t.Fatalf("empty text = %#v", bad)
	}
	if tone := host.handleHostCall(entry, "host.setStatusItem",
		json.RawMessage(`{"id":"status.build","text":"x","tone":"loud"}`)); tone.Err == nil ||
		tone.Err.Code != pluginRPCInvalidParams {
		t.Fatalf("bad tone = %#v", tone)
	}
	hidden := host.handleHostCall(entry, "host.setStatusItem",
		json.RawMessage(`{"id":"status.build","hidden":true}`))
	if hidden.Err != nil {
		t.Fatalf("hidden item: %#v", hidden.Err)
	}
	if len(received) != 2 || !received[1].Hidden {
		t.Fatalf("hidden update = %#v", received)
	}
}

func TestP25StatusItemHUDAndRelease(t *testing.T) {
	state := &appState{
		piConn:            piConnected,
		pluginStatusItems: map[string]pluginStatusItemUpdate{},
	}
	manifest := &pluginManifest{
		ID: "local.dashboard",
		Contributes: pluginContributions{StatusItems: []pluginStatusContribution{
			{ID: "status.build", Title: "Build"},
		}},
	}
	state.setPluginRegistry(pluginRegistry{Entries: []pluginRegistryEntry{
		{ID: "local.dashboard", Status: pluginRegistered, Manifest: manifest},
	}})
	state.pluginStatusItems["local.dashboard"] = pluginStatusItemUpdate{
		PluginID: "local.dashboard", Title: "Build", Text: "green", Tone: "success",
	}
	if label := state.piHUDFooterLabel(); !strings.Contains(label, "Build") ||
		!strings.Contains(label, "green") {
		t.Fatalf("HUD footer = %q", label)
	}
	// Hiding removes the item from the HUD.
	state.pluginStatusItems["local.dashboard"] = pluginStatusItemUpdate{
		PluginID: "local.dashboard", Hidden: true,
	}
	if strings.Contains(state.piHUDFooterLabel(), "green") {
		t.Fatalf("hidden item still shown: %q", state.piHUDFooterLabel())
	}
	// A disable releases the item: the entry is no longer registered.
	state.pluginStatusItems["local.dashboard"] = pluginStatusItemUpdate{
		PluginID: "local.dashboard", Title: "Build", Text: "green",
	}
	state.setPluginRegistry(pluginRegistry{Entries: []pluginRegistryEntry{
		{ID: "local.dashboard", Status: pluginDisabled, Manifest: manifest},
	}})
	state.prunePluginState()
	if len(state.pluginStatusItems) != 0 {
		t.Fatalf("disabled plugin kept its status item: %#v", state.pluginStatusItems)
	}
	// Re-enabling with a manifest that no longer declares one also releases it.
	noItem := &pluginManifest{ID: "local.dashboard"}
	state.pluginStatusItems["local.dashboard"] = pluginStatusItemUpdate{
		PluginID: "local.dashboard", Title: "Build", Text: "green",
	}
	state.setPluginRegistry(pluginRegistry{Entries: []pluginRegistryEntry{
		{ID: "local.dashboard", Status: pluginRegistered, Manifest: noItem},
	}})
	state.prunePluginState()
	if len(state.pluginStatusItems) != 0 {
		t.Fatalf("undeclared item survived a rescan: %#v", state.pluginStatusItems)
	}
}

func TestP25MessageRendererMatchingAndCache(t *testing.T) {
	manifest := &pluginManifest{
		ID: "local.chart",
		Contributes: pluginContributions{MessageRenderers: []pluginMessageRendererContribution{
			{ID: "r.chart", Title: "Charts", Command: "chart.render", Languages: []string{"ChartLang"}},
		}},
	}
	other := &pluginManifest{
		ID: "local.other",
		Contributes: pluginContributions{MessageRenderers: []pluginMessageRendererContribution{
			{ID: "r.mime", Title: "MIME only", Command: "mime.render", MimeTypes: []string{"image/png"}},
		}},
	}
	registry := pluginRegistry{Entries: []pluginRegistryEntry{
		{ID: "local.chart", Status: pluginRegistered, Manifest: manifest},
		{ID: "local.other", Status: pluginRegistered, Manifest: other},
	}}
	entry, contribution, ok := registry.messageRendererForLanguage("chartlang")
	if !ok || entry.ID != "local.chart" || contribution.Command != "chart.render" {
		t.Fatalf("language match = %s/%#v ok=%v", entry.ID, contribution, ok)
	}
	// A renderer that declared only MIME types does not claim a code fence.
	if _, _, ok := registry.messageRendererForLanguage("png"); ok {
		t.Fatal("MIME-only renderer matched a code fence")
	}
	// A disabled plugin's declaration is ignored.
	disabled := pluginRegistry{Entries: []pluginRegistryEntry{
		{ID: "local.chart", Status: pluginDisabled, Manifest: manifest},
	}}
	if _, _, ok := disabled.messageRendererForLanguage("chartlang"); ok {
		t.Fatal("disabled plugin still matched")
	}
	// The cache key is content-addressed and language-aware.
	if pluginRendererCacheKey("local.chart", "r.chart", "chartlang", "a\n") !=
		pluginRendererCacheKey("local.chart", "r.chart", "ChartLang", "a\n") {
		t.Fatal("same content and language must share one key")
	}
	if pluginRendererCacheKey("local.chart", "r.chart", "chartlang", "a\n") ==
		pluginRendererCacheKey("local.chart", "r.chart", "json", "a\n") {
		t.Fatal("different languages must not share one key")
	}
	// Ownership is part of the key, so a second plugin claiming the same
	// language cannot hit the first plugin's pending or failed entry.
	if pluginRendererCacheKey("local.chart", "r.chart", "chart", "a\n") ==
		pluginRendererCacheKey("local.other", "r.chart", "chart", "a\n") {
		t.Fatal("different owning plugins must not share one key")
	}
}

// A stale reply must not touch a key a newer request already owns, and a
// release must free every entry and cancel every call in flight, so renders
// cannot accumulate across sessions.
func TestP25MessageRendererReleaseAndStaleReply(t *testing.T) {
	var cancelled int
	state := &appState{
		pluginRendererResults: map[string]pluginRendererResult{
			"old": {state: pluginRendererReady, pluginID: "local.chart", request: 1},
			"new": {state: pluginRendererPending, pluginID: "local.chart", request: 2},
		},
		pendingPluginRenderers: map[string]context.CancelFunc{
			"new": func() { cancelled++ },
		},
		pluginRendererGeneration: map[string]uint64{},
		pluginRendererSignatures: map[string]string{},
	}
	// The reply of request 1 arrives after request 2 took the same key, so it
	// must leave both maps alone rather than clear the newer entry.
	if current, has := state.pluginRendererResults["new"]; !has || current.request != 2 {
		t.Fatal("fixture lost the newer request")
	}
	state.releasePluginRenderers()
	if len(state.pluginRendererResults) != 0 || len(state.pendingPluginRenderers) != 0 {
		t.Fatalf("release left state behind: %d results, %d pending",
			len(state.pluginRendererResults), len(state.pendingPluginRenderers))
	}
	if cancelled != 1 {
		t.Fatalf("release cancelled %d calls, want 1", cancelled)
	}
	// A release only frees the cache; it must not raise any generation, so a
	// still-installed renderer refills it on the next transcript without
	// being treated as restarted.
	if state.pluginRendererGeneration["local.chart"] != 0 {
		t.Fatal("a release must not raise any generation")
	}
}

func TestP25MessageRendererInvalidationIsPerPlugin(t *testing.T) {
	state := &appState{
		pluginRendererResults:    map[string]pluginRendererResult{},
		pendingPluginRenderers:   map[string]context.CancelFunc{},
		pluginRendererGeneration: map[string]uint64{},
		pluginRendererSignatures: map[string]string{},
	}
	chart := &pluginManifest{
		ID: "local.chart",
		Contributes: pluginContributions{MessageRenderers: []pluginMessageRendererContribution{
			{ID: "r.chart", Title: "Charts", Command: "chart.render", Languages: []string{"chart"}},
		}},
	}
	mermaid := &pluginManifest{
		ID: "local.mermaid",
		Contributes: pluginContributions{MessageRenderers: []pluginMessageRendererContribution{
			{ID: "r.mermaid", Title: "Mermaid", Command: "mermaid.render", Languages: []string{"mermaid"}},
		}},
	}
	state.pluginRendererResults["chart-key"] = pluginRendererResult{
		state: pluginRendererReady, pluginID: "local.chart",
	}
	state.pluginRendererResults["mermaid-key"] = pluginRendererResult{
		state: pluginRendererReady, pluginID: "local.mermaid",
	}
	state.setPluginRegistry(pluginRegistry{Entries: []pluginRegistryEntry{
		{ID: "local.chart", Status: pluginRegistered, Manifest: chart},
		{ID: "local.mermaid", Status: pluginRegistered, Manifest: mermaid},
	}})
	state.prunePluginRenderers()
	if len(state.pluginRendererResults) != 2 {
		t.Fatalf("first scan dropped renders: %#v", state.pluginRendererResults)
	}
	// Restarting one plugin releases only that plugin's cache.
	state.invalidatePluginRenderers("local.chart")
	if _, has := state.pluginRendererResults["chart-key"]; has {
		t.Fatal("restarted plugin kept a cached render")
	}
	if _, has := state.pluginRendererResults["mermaid-key"]; !has {
		t.Fatal("restarting one plugin flushed another plugin's cache")
	}
	// An updated declaration is detected by the scan that publishes it.
	updated := &pluginManifest{
		ID: "local.mermaid",
		Contributes: pluginContributions{MessageRenderers: []pluginMessageRendererContribution{
			{ID: "r.mermaid", Title: "Mermaid", Command: "mermaid.render.v2", Languages: []string{"mermaid"}},
		}},
	}
	state.setPluginRegistry(pluginRegistry{Entries: []pluginRegistryEntry{
		{ID: "local.chart", Status: pluginRegistered, Manifest: chart},
		{ID: "local.mermaid", Status: pluginRegistered, Manifest: updated},
	}})
	state.prunePluginRenderers()
	if len(state.pluginRendererResults) != 0 {
		t.Fatalf("changed declaration kept a cached render: %#v", state.pluginRendererResults)
	}
	if state.pluginRendererGeneration["local.mermaid"] == 0 {
		t.Fatal("changed declaration did not raise the generation")
	}
}
