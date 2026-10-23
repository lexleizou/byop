package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestP10ePluginCardPrimaryActionPriority(t *testing.T) {
	manifest := &pluginManifest{
		Contributes: pluginContributions{
			Commands: []pluginCommandContribution{{ID: "hello.say", Title: "Say hello"}},
			Views: []pluginViewContribution{{
				ID: "hello.panel", Title: "Hello panel", Location: "main",
			}},
			WebViews: []pluginWebViewContribution{{
				ID: "hello.web", Title: "Hello web", Location: "main", Entry: "index.html",
			}},
		},
	}
	entry := pluginRegistryEntry{
		ID:       "example.p10e",
		Status:   pluginRegistered,
		Manifest: manifest,
	}
	action := pluginCardPrimaryAction(entry)
	if action.Kind != pluginCardActionView || action.ID != "hello.panel" {
		t.Fatalf("primary action = %#v, want the first main native view", action)
	}

	manifest.Contributes.Views = nil
	action = pluginCardPrimaryAction(entry)
	if action.Kind != pluginCardActionWebView || action.ID != "hello.web" {
		t.Fatalf("primary action = %#v, want the first main WebView", action)
	}

	manifest.Contributes.WebViews = nil
	action = pluginCardPrimaryAction(entry)
	if action.Kind != pluginCardActionCommand || action.ID != "hello.say" {
		t.Fatalf("primary action = %#v, want the first command", action)
	}

	manifest.Contributes.Commands = nil
	if action = pluginCardPrimaryAction(entry); action.Kind != pluginCardActionNone {
		t.Fatalf("primary action = %#v, want no action", action)
	}
}

func TestP10eIncompatiblePluginHasNoPrimaryAction(t *testing.T) {
	entry := pluginRegistryEntry{
		ID:     "example.incompatible",
		Status: pluginIncompatible,
		Manifest: &pluginManifest{
			Contributes: pluginContributions{
				Commands: []pluginCommandContribution{{ID: "hello.say", Title: "Say hello"}},
				Views: []pluginViewContribution{{
					ID: "hello.panel", Title: "Hello panel", Location: "main",
				}},
			},
		},
	}
	if action := pluginCardPrimaryAction(entry); action.Kind != pluginCardActionNone {
		t.Fatalf("incompatible plugin primary action = %#v, want none", action)
	}
}

func TestP10ePluginCardStatusSummaryPriority(t *testing.T) {
	state := &appState{}
	p := themePalette(false)
	manifest := &pluginManifest{Runtime: "node"}
	entry := pluginRegistryEntry{
		ID:       "example.plugin",
		Version:  "1.2.3",
		Status:   pluginRegistered,
		Manifest: manifest,
	}

	entry.Error = "registration failed"
	got, tone := state.pluginCardStatusSummary(p, entry, pluginRuntimeStatus{
		LastError:   "runtime failed",
		LastResult:  "result",
		LastCommand: "command",
	})
	if !strings.Contains(got, "registration failed") || tone != p.danger {
		t.Fatalf("registration error summary = %q / %v", got, tone)
	}

	entry.Error = ""
	got, tone = state.pluginCardStatusSummary(p, entry, pluginRuntimeStatus{
		LastError:   "runtime failed",
		LastResult:  "result",
		LastCommand: "command",
	})
	if !strings.Contains(got, "runtime failed") || tone != p.danger {
		t.Fatalf("runtime error summary = %q / %v", got, tone)
	}

	got, tone = state.pluginCardStatusSummary(p, entry, pluginRuntimeStatus{
		LastResult:  "42",
		LastCommand: "hello.say",
	})
	if got != "hello.say → 42" || tone != p.success {
		t.Fatalf("result summary = %q / %v", got, tone)
	}

	got, tone = state.pluginCardStatusSummary(p, entry, pluginRuntimeStatus{})
	if got != "v1.2.3 · node" || tone != p.muted {
		t.Fatalf("version/runtime summary = %q / %v", got, tone)
	}
}

func TestP10ePluginCardHeightAndNarrowGrid(t *testing.T) {
	state := pluginCardTestState(t)
	tt := ui.NewTester(func(c *ui.Context) {
		theme, palette := nativeThemeWithContrast(false, false)
		c.SetTheme(theme)
		state.pluginsPage(c, palette)
	}, 760, 720)

	rect, ok := tt.Find(textArgs("pluginsCardLabel", map[string]any{
		"name": "P10e Test Plugin",
	}))
	if !ok {
		t.Fatal("plugin card was not rendered")
	}
	if rect.H != pluginCardDefaultHeight {
		t.Fatalf("plugin card height = %.1f, want %.1f", rect.H, pluginCardDefaultHeight)
	}
	if rect.W <= pluginCardMaxWidth {
		t.Fatalf("narrow plugin card width = %.1f, want FillWidth above %.1f", rect.W, pluginCardMaxWidth)
	}
}

func TestP10ePluginDetailsOpensOneOverlayAndEscapeCloses(t *testing.T) {
	state := pluginCardTestState(t)
	tt := ui.NewTester(func(c *ui.Context) {
		theme, palette := nativeThemeWithContrast(false, false)
		c.SetTheme(theme)
		state.pluginsPage(c, palette)
	}, 1100, 900)

	if err := tt.Click(textArgs("pluginsDetailsFor", map[string]any{
		"name": "P10e Test Plugin",
	})); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if state.pluginDetailsOpenID != "example.p10e-0" {
		t.Fatalf("details open key = %q, want example.p10e-0", state.pluginDetailsOpenID)
	}
	findLabelContaining(t, tt, text("pluginsDetailsTitle"))

	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if state.pluginDetailsOpenID != "" {
		t.Fatalf("details stayed open after Escape: %q", state.pluginDetailsOpenID)
	}
}

func TestP10eDuplicatePluginIDsUseDistinctCardsAndDetails(t *testing.T) {
	state := pluginCardTestState(t)
	first := state.pluginRegistry.Entries[0]
	first.SourceDirectory = "/plugins/alpha"
	first.Name = "Duplicate Alpha"
	first.Manifest = clonePluginManifestForP10e(first.Manifest, "Duplicate Alpha")
	second := first
	second.SourceDirectory = "/plugins/beta"
	second.Name = "Duplicate Beta"
	second.Manifest = clonePluginManifestForP10e(first.Manifest, "Duplicate Beta")
	second.Status = pluginDuplicateID
	second.Error = `plugin id "example.p10e" is already registered from /plugins/alpha`
	state.setPluginRegistry(pluginRegistry{
		HostVersion: pluginHostVersion,
		PluginAPI:   pluginAPIVersion,
		Entries:     []pluginRegistryEntry{first, second},
	})

	tt := ui.NewTester(func(c *ui.Context) {
		theme, palette := nativeThemeWithContrast(false, false)
		c.SetTheme(theme)
		state.pluginsPage(c, palette)
	}, 1100, 900)

	if err := tt.Click(textArgs("pluginsDetailsFor", map[string]any{
		"name": "Duplicate Beta",
	})); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if state.pluginDetailsOpenID != "/plugins/beta" {
		t.Fatalf("second details key = %q, want /plugins/beta", state.pluginDetailsOpenID)
	}

	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if state.pluginDetailsOpenID != "" {
		t.Fatalf("details stayed open after Escape: %q", state.pluginDetailsOpenID)
	}

	if err := tt.Click(textArgs("pluginsDetailsFor", map[string]any{
		"name": "Duplicate Alpha",
	})); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if state.pluginDetailsOpenID != "/plugins/alpha" {
		t.Fatalf("first details key = %q, want /plugins/alpha", state.pluginDetailsOpenID)
	}
}

func TestP10ePluginDetailsUseUniqueKeysForDuplicateIDs(t *testing.T) {
	state := pluginCardTestState(t)
	first := state.pluginRegistry.Entries[0]
	first.SourceDirectory = "/plugins/alpha"
	first.Manifest = clonePluginManifestForP10e(first.Manifest, first.Name)
	second := first
	second.SourceDirectory = "/plugins/beta"
	second.Manifest = clonePluginManifestForP10e(first.Manifest, first.Name)
	state.setPluginRegistry(pluginRegistry{
		HostVersion: pluginHostVersion,
		PluginAPI:   pluginAPIVersion,
		Entries:     []pluginRegistryEntry{first, second},
	})

	// MyGo's test renderer panics on duplicate keys, so rendering both registered
	// entries is itself the duplicate-key regression assertion.
	ui.NewTester(func(c *ui.Context) {
		theme, palette := nativeThemeWithContrast(false, false)
		c.SetTheme(theme)
		state.pluginsPage(c, palette)
	}, 1100, 900)
}

func clonePluginManifestForP10e(manifest *pluginManifest, name string) *pluginManifest {
	if manifest == nil {
		return nil
	}
	cloned := *manifest
	cloned.Name = name
	return &cloned
}

func pluginCardTestState(t *testing.T) *appState {
	t.Helper()
	manifest := &pluginManifest{
		ID:          "example.p10e",
		Name:        "P10e Test Plugin",
		Version:     "1.2.3",
		Description: "A compact plugin card used to verify the P10e layout contract.",
		Runtime:     "node",
		Icon:        "plugins",
		Contributes: pluginContributions{
			Commands: []pluginCommandContribution{{
				ID: "hello.say", Title: "Say hello", Description: "Return a greeting.",
			}},
			Views: []pluginViewContribution{{
				ID: "hello.panel", Title: "Hello panel", Location: "main", View: "native",
			}},
			Settings: []pluginSettingContribution{{ID: "hello.settings", Title: "Settings"}},
		},
	}
	state := layoutTestState(t)
	state.page = pagePlugins
	state.plugins = newPluginHost(pluginHostVersion, nil)
	state.setPluginRegistry(pluginRegistry{
		HostVersion: pluginHostVersion,
		PluginAPI:   pluginAPIVersion,
		Entries: []pluginRegistryEntry{{
			ID:                manifest.ID,
			Name:              manifest.Name,
			Version:           manifest.Version,
			Description:       manifest.Description,
			Manifest:          manifest,
			Status:            pluginRegistered,
			ContributionCount: manifest.contributionCount(),
		}},
	})
	return state
}

func findLabelContaining(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if _, ok := tt.Find(label); !ok {
		t.Fatalf("layout label %q was not rendered", label)
	}
}
