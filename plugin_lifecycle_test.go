package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPluginDisabledStorePersistsAndRefusesToOverwriteDamage(t *testing.T) {
	store := newPluginDisabledStore(t.TempDir())
	if ids := store.load(); len(ids) != 0 {
		t.Fatalf("missing file disabled = %v, want empty", ids)
	}
	if err := store.save([]string{"example.b", "example.a", "example.a"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	ids, ok := store.loadChecked()
	if !ok || strings.Join(ids, ",") != "example.a,example.b" {
		t.Fatalf("loadChecked = (%v, %v), want sorted unique pair", ids, ok)
	}

	// A damaged file keeps the last known states and refuses a blind rewrite.
	if err := os.WriteFile(store.path(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.loadChecked(); ok {
		t.Fatal("damaged file reported readable")
	}
	if err := store.save([]string{"example.c"}); err == nil {
		t.Fatal("save over a damaged file succeeded; it would re-enable plugins")
	}
	data, err := os.ReadFile(store.path())
	if err != nil || !strings.Contains(string(data), "not json") {
		t.Fatalf("damaged file changed by a refused save: %q, %v", data, err)
	}

	// A read-modify-write through update keeps the other ids.
	if err := os.WriteFile(store.path(),
		[]byte(`{"version":1,"disabled":["example.a","example.b"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.update(func(current []string) []string {
		next := []string{}
		for _, id := range current {
			if id != "example.a" {
				next = append(next, id)
			}
		}
		return append(next, "example.d")
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	ids, _ = store.loadChecked()
	if strings.Join(ids, ",") != "example.b,example.d" {
		t.Fatalf("after update ids = %v", ids)
	}
}

func TestScanPluginRegistryWithDisabledWithholdsContributions(t *testing.T) {
	root := t.TempDir()
	manifestPath := writePluginManifest(t, root, "example", validPluginManifestMap())
	writePluginWebViewFixture(t, filepath.Dir(manifestPath))
	configDir := t.TempDir()
	if err := newPluginDisabledStore(configDir).save([]string{"example.git-workspace"}); err != nil {
		t.Fatal(err)
	}

	enabled := scanPluginRegistryWithDisabled([]pluginRootSpec{{Path: root, Explicit: true}}, "")
	if enabled.registeredCount() != 1 {
		t.Fatalf("empty configDir registered = %d, want 1", enabled.registeredCount())
	}
	disabled := scanPluginRegistryWithDisabled(
		[]pluginRootSpec{{Path: root, Explicit: true}}, configDir)
	if disabled.registeredCount() != 0 {
		t.Fatalf("disabled registered = %d, want 0", disabled.registeredCount())
	}
	if disabled.contributionCount() != 0 {
		t.Fatalf("disabled contributions = %d, want 0", disabled.contributionCount())
	}
	entry := disabled.Entries[0]
	if entry.Status != pluginDisabled || entry.Manifest == nil || entry.Error != "" {
		t.Fatalf("disabled entry = %#v", entry)
	}
}

func TestPluginRuntimeStateMarkerRoundTripAndSelfHeal(t *testing.T) {
	configDir := t.TempDir()
	pluginID := "example.hello-native"

	// A missing marker means enabled.
	if !readPluginRuntimeStateEnabled(configDir, pluginID) {
		t.Fatal("missing marker reported disabled")
	}
	if err := writePluginRuntimeStateMarker(configDir, pluginID, false); err != nil {
		t.Fatalf("write false: %v", err)
	}
	if readPluginRuntimeStateEnabled(configDir, pluginID) {
		t.Fatal("marker did not persist enabled=false")
	}
	path, err := pluginRuntimeStatePath(configDir, pluginID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var marker pluginRuntimeStateMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		t.Fatalf("marker JSON: %v", err)
	}
	if marker.Version != 1 || marker.PluginID != pluginID || marker.Enabled ||
		marker.UpdatedAt == "" {
		t.Fatalf("marker = %#v", marker)
	}
	// The marker is a separate file from the generation marker.
	if filepath.Base(path) != pluginRuntimeStateFilename ||
		filepath.Base(path) == pluginRuntimeGenerationFilename {
		t.Fatalf("marker filename = %q", path)
	}

	// Self-heal repairs enabled extension plugins and leaves the rest alone.
	extension := filepath.Join(t.TempDir(), "pi-extension.mjs")
	if err := os.WriteFile(extension, []byte("export default function () {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry := pluginRegistry{Entries: []pluginRegistryEntry{
		{ID: "example.extension", Status: pluginRegistered,
			Manifest: &pluginManifest{Contributes: pluginContributions{
				AgentExtension: filepath.Base(extension),
			}},
			SourceDirectory: filepath.Dir(extension)},
		{ID: "example.web", Status: pluginRegistered,
			Manifest: &pluginManifest{}},
		{ID: "example.disabled", Status: pluginDisabled,
			Manifest: &pluginManifest{Contributes: pluginContributions{
				AgentExtension: filepath.Base(extension),
			}},
			SourceDirectory: filepath.Dir(extension)},
	}}
	for _, id := range []string{"example.extension", "example.web", "example.disabled"} {
		if err := writePluginRuntimeStateMarker(configDir, id, false); err != nil {
			t.Fatal(err)
		}
	}
	if repaired := selfHealPluginRuntimeStateMarkers(configDir, registry); repaired != 1 {
		t.Fatalf("repaired = %d, want 1", repaired)
	}
	if !readPluginRuntimeStateEnabled(configDir, "example.extension") {
		t.Fatal("loaded extension marker was not repaired")
	}
	if readPluginRuntimeStateEnabled(configDir, "example.disabled") {
		t.Fatal("self-heal overwrote a deliberately disabled marker")
	}
	if readPluginRuntimeStateEnabled(configDir, "example.web") {
		t.Fatal("self-heal touched a plugin with no Pi extension")
	}
}

func TestPiRefreshDecisionUsesLoadedExtensionSet(t *testing.T) {
	directory := t.TempDir()
	extension := filepath.Join(directory, "pi-extension.mjs")
	if err := os.WriteFile(extension, []byte("export default function () {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := pluginRegistryEntry{
		ID:              "example.extension",
		Status:          pluginRegistered,
		SourceDirectory: directory,
		Manifest: &pluginManifest{Contributes: pluginContributions{
			AgentExtension:          "pi-extension.mjs",
			AgentExtensionLifecycle: pluginAgentExtensionLifecycleCooperative,
		}},
	}
	loaded := piLoadedExtensionSet(pluginRegistry{Entries: []pluginRegistryEntry{entry}})
	if _, ok := loaded[entry.ID]; !ok {
		t.Fatalf("loaded set = %v, want the registered extension", loaded)
	}
	if !loaded[entry.ID].cooperative {
		t.Fatalf("loaded snapshot = %#v, want the declared cooperative lifecycle", loaded[entry.ID])
	}

	// Disabling a loaded plugin must not ask for a Pi refresh: its extension is
	// still in memory and revokes itself through the runtime-state marker.
	disabledEntry := entry
	disabledEntry.Status = pluginDisabled
	if pluginRefreshNeededForExtensions(loaded,
		pluginRegistry{Entries: []pluginRegistryEntry{disabledEntry}}) {
		t.Fatal("disable raised a Pi refresh")
	}
	// Re-enabling the same unchanged file must not either.
	if pluginRefreshNeededForExtensions(loaded,
		pluginRegistry{Entries: []pluginRegistryEntry{entry}}) {
		t.Fatal("re-enable of a loaded extension raised a Pi refresh")
	}
	// A changed extension file the running Pi does not have does need one.
	if err := os.WriteFile(extension, []byte("export default function v2() {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !pluginRefreshNeededForExtensions(loaded,
		pluginRegistry{Entries: []pluginRegistryEntry{entry}}) {
		t.Fatal("changed extension file did not raise a Pi refresh")
	}
	// A newly enabled plugin whose extension Pi never loaded needs one too.
	other := entry
	other.ID = "example.second"
	if !pluginRefreshNeededForExtensions(loaded,
		pluginRegistry{Entries: []pluginRegistryEntry{entry, other}}) {
		t.Fatal("extension missing from the loaded set did not raise a Pi refresh")
	}
}

// TestPiRefreshDecisionRevokesNonCooperativeExtensions covers review X5: an
// extension Pi loaded without the cooperative declaration keeps its tools in
// memory, so every way of taking the plugin away — disabled, removed, failed
// validation, or the extension field dropped — must raise the Pi refresh, and
// re-enabling one after that refresh (it is then missing from the loaded set)
// must raise it again so Pi loads it anew.
func TestPiRefreshDecisionRevokesNonCooperativeExtensions(t *testing.T) {
	directory := t.TempDir()
	extension := filepath.Join(directory, "pi-extension.mjs")
	if err := os.WriteFile(extension, []byte("export default function () {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := pluginRegistryEntry{
		ID:              "example.plain",
		Status:          pluginRegistered,
		SourceDirectory: directory,
		Manifest: &pluginManifest{Contributes: pluginContributions{
			AgentExtension: "pi-extension.mjs",
		}},
	}
	loaded := piLoadedExtensionSet(pluginRegistry{Entries: []pluginRegistryEntry{entry}})
	if loaded[entry.ID].cooperative {
		t.Fatal("an undeclared lifecycle must default to non-cooperative")
	}
	// While it stays registered and enabled, nothing changes in Pi.
	if pluginRefreshNeededForExtensions(loaded,
		pluginRegistry{Entries: []pluginRegistryEntry{entry}}) {
		t.Fatal("an unchanged non-cooperative extension raised a Pi refresh")
	}
	disabledEntry := entry
	disabledEntry.Status = pluginDisabled
	droppedEntry := entry
	droppedManifest := *entry.Manifest
	droppedManifest.Contributes.AgentExtension = ""
	droppedEntry.Manifest = &droppedManifest
	for name, registry := range map[string]pluginRegistry{
		"disabled": {Entries: []pluginRegistryEntry{disabledEntry}},
		"removed":  {},
		"failed": {Entries: []pluginRegistryEntry{
			{ID: entry.ID, Status: pluginInvalidManifest, SourceDirectory: directory},
		}},
		"extension dropped": {Entries: []pluginRegistryEntry{droppedEntry}},
	} {
		if !pluginRefreshNeededForExtensions(loaded, registry) {
			t.Fatalf("%s did not raise a Pi refresh for a non-cooperative loaded extension", name)
		}
	}
	// After that refresh Pi no longer has the extension, so re-enabling needs
	// the prompt again, covered by the missing-from-loaded rule.
	if !pluginRefreshNeededForExtensions(map[string]piLoadedExtension{},
		pluginRegistry{Entries: []pluginRegistryEntry{entry}}) {
		t.Fatal("re-enable after the unload refresh did not raise a Pi refresh")
	}
}

// TestReconcileLoadedCooperativeMarkers covers the X5 edge: a loaded
// cooperative extension that leaves the registry outside a deliberate toggle
// must be revoked through its marker by the scan that notices it, and a marker
// the host cannot write falls back to the existing Pi refresh prompt. These
// cases need no real Pi process.
func TestReconcileLoadedCooperativeMarkers(t *testing.T) {
	directory := t.TempDir()
	extension := filepath.Join(directory, "pi-extension.mjs")
	if err := os.WriteFile(extension, []byte("export default function () {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := pluginRegistryEntry{
		ID:              "example.coop",
		Status:          pluginRegistered,
		SourceDirectory: directory,
		Manifest: &pluginManifest{Contributes: pluginContributions{
			AgentExtension:          "pi-extension.mjs",
			AgentExtensionLifecycle: pluginAgentExtensionLifecycleCooperative,
		}},
	}
	loadedRegistry := pluginRegistry{Entries: []pluginRegistryEntry{entry}}
	client := &piClient{
		loadedExtensions:      piLoadedExtensionSet(loadedRegistry),
		loadedExtensionsKnown: true,
	}

	// (a) The folder left the registry outside the app: the scan that notices
	// it marks the extension revoked, and a cooperative extension still needs
	// no Pi refresh.
	host := newTestPluginHost(t)
	if err := writePluginRuntimeStateMarker(host.configDir, entry.ID, true); err != nil {
		t.Fatal(err)
	}
	state := &appState{plugins: host, pi: client, piConn: piConnected}
	state.setPiLoadedExtensions(client)
	empty := pluginRegistry{}
	state.applyPluginScan(loadedRegistry, empty, planPluginScan(loadedRegistry, empty), false)
	if readPluginRuntimeStateEnabled(host.configDir, entry.ID) {
		t.Fatal("a removed cooperative extension stayed marked enabled")
	}
	if state.pluginPiRefreshRequired {
		t.Fatal("the marker revoke of a cooperative extension raised a Pi refresh")
	}

	// (b) The same extension reappears with the same file: the loaded code is
	// the right code again, so the marker goes back to enabled.
	if err := writePluginRuntimeStateMarker(host.configDir, entry.ID, false); err != nil {
		t.Fatal(err)
	}
	state.applyPluginScan(empty, loadedRegistry, planPluginScan(empty, loadedRegistry), false)
	if !readPluginRuntimeStateEnabled(host.configDir, entry.ID) {
		t.Fatal("a cooperative extension that reappeared stayed revoked")
	}

	// (c) A marker the host cannot write revokes nothing, so the existing Pi
	// refresh prompt is the honest fallback.
	host = newTestPluginHost(t)
	markerPath, err := pluginRuntimeStatePath(host.configDir, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(markerPath, 0o700); err != nil {
		t.Fatal(err)
	}
	state = &appState{plugins: host, pi: client, piConn: piConnected}
	state.setPiLoadedExtensions(client)
	state.applyPluginScan(loadedRegistry, empty, planPluginScan(loadedRegistry, empty), false)
	if !state.pluginPiRefreshRequired {
		t.Fatal("a cooperative marker that could not be written did not fall back to the Pi refresh")
	}

	// (d) A stale scan result that still lists the plugin as registered must
	// not write the marker back to enabled while the host has it disabled in
	// memory: setPluginDisabledState does not bump the scan sequence.
	host = newTestPluginHost(t)
	if err := host.setPluginDisabledState(entry.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := writePluginRuntimeStateMarker(host.configDir, entry.ID, false); err != nil {
		t.Fatal(err)
	}
	state = &appState{plugins: host, pi: client, piConn: piConnected}
	state.setPiLoadedExtensions(client)
	state.applyPluginScan(empty, loadedRegistry, planPluginScan(empty, loadedRegistry), false)
	if readPluginRuntimeStateEnabled(host.configDir, entry.ID) {
		t.Fatal("a stale scan re-enabled the marker of a plugin disabled in memory")
	}

	// (e) A same-id entry that is not live — here a duplicate id — never counts
	// as the live entry, so the marker is revoked rather than kept alive by the
	// duplicate's matching file.
	host = newTestPluginHost(t)
	duplicate := entry
	duplicate.Status = pluginDuplicateID
	if err := writePluginRuntimeStateMarker(host.configDir, entry.ID, true); err != nil {
		t.Fatal(err)
	}
	state = &appState{plugins: host, pi: client, piConn: piConnected}
	state.setPiLoadedExtensions(client)
	duplicateRegistry := pluginRegistry{Entries: []pluginRegistryEntry{duplicate}}
	state.applyPluginScan(loadedRegistry, duplicateRegistry,
		planPluginScan(loadedRegistry, duplicateRegistry), false)
	if readPluginRuntimeStateEnabled(host.configDir, entry.ID) {
		t.Fatal("a non-registered duplicate kept the cooperative marker enabled")
	}
}

// TestPiRefreshBaselineIsTheLaunchedClientSnapshot pins review H1: the refresh
// TestSetPluginEnabledNoticeFollowsLoadedLifecycle is the X5 wording check: a
// disable whose loaded extension is non-cooperative says the Pi refresh is what
// unloads it, while a cooperative one keeps the plain notice. The decision
// reads this plugin's entry in the loaded snapshot, not a global flag.
func TestSetPluginEnabledNoticeFollowsLoadedLifecycle(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "pi-extension.mjs"),
		[]byte("export default function () {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	newEntry := func(id, lifecycle string) pluginRegistryEntry {
		return pluginRegistryEntry{
			ID:              id,
			Status:          pluginRegistered,
			SourceDirectory: directory,
			Manifest: &pluginManifest{Contributes: pluginContributions{
				AgentExtension:          "pi-extension.mjs",
				AgentExtensionLifecycle: lifecycle,
			}},
		}
	}
	plain := newEntry("example.plain", "")
	coop := newEntry("example.coop", pluginAgentExtensionLifecycleCooperative)
	registry := pluginRegistry{Entries: []pluginRegistryEntry{plain, coop}}

	host := newTestPluginHost(t)
	client := &piClient{
		loadedExtensions:      piLoadedExtensionSet(registry),
		loadedExtensionsKnown: true,
	}
	state := &appState{plugins: host, pi: client, piConn: piConnected}
	state.setPiLoadedExtensions(client)

	state.setPluginEnabled(plain, false)
	if state.notice != textArgs("pluginsDisabledRefreshRequired",
		map[string]any{"name": plain.ID}) {
		t.Fatalf("non-cooperative disable notice = %q", state.notice)
	}
	state.setPluginEnabled(coop, false)
	if state.notice != textArgs("pluginsDisabled", map[string]any{"name": coop.ID}) {
		t.Fatalf("cooperative disable notice = %q", state.notice)
	}
}

// TestPiRefreshBaselineIsTheLaunchedClientSnapshot pins review H1: the refresh
// decision compares against the immutable snapshot a Pi client started with,
// and no scan overwrites it. Disable then re-enable in the same session stays
// silent for a cooperative extension, which the marker revokes in place; a
// changed extension file the running Pi lacks does prompt.
func TestPiRefreshBaselineIsTheLaunchedClientSnapshot(t *testing.T) {
	directory := t.TempDir()
	extension := filepath.Join(directory, "pi-extension.mjs")
	if err := os.WriteFile(extension, []byte("export default function () {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := pluginRegistryEntry{
		ID:              "example.extension",
		Status:          pluginRegistered,
		SourceDirectory: directory,
		Manifest: &pluginManifest{Contributes: pluginContributions{
			AgentExtension:          "pi-extension.mjs",
			AgentExtensionLifecycle: pluginAgentExtensionLifecycleCooperative,
		}},
	}
	disabledEntry := entry
	disabledEntry.Status = pluginDisabled
	enabledRegistry := pluginRegistry{Entries: []pluginRegistryEntry{entry}}
	disabledRegistry := pluginRegistry{Entries: []pluginRegistryEntry{disabledEntry}}

	host := newTestPluginHost(t)
	client := &piClient{
		loadedExtensions:      piLoadedExtensionSet(enabledRegistry),
		loadedExtensionsKnown: true,
	}
	state := &appState{plugins: host, pi: client, piConn: piConnected}
	state.setPluginRegistry(enabledRegistry)
	state.setPiLoadedExtensions(client)

	state.applyPluginScan(enabledRegistry, disabledRegistry,
		planPluginScan(enabledRegistry, disabledRegistry), false)
	if state.pluginPiRefreshRequired {
		t.Fatal("disable raised a Pi refresh")
	}
	state.applyPluginScan(disabledRegistry, enabledRegistry,
		planPluginScan(disabledRegistry, enabledRegistry), false)
	if state.pluginPiRefreshRequired {
		t.Fatal("re-enabling a plugin the running Pi still loads raised a Pi refresh")
	}
	if _, ok := state.piLoadedExtensions[entry.ID]; !ok {
		t.Fatal("a scan overwrote the snapshot the running Pi started with")
	}

	// The same extension file changed on disk, so the running Pi lacks the
	// current code: the prompt is correct now.
	if err := os.WriteFile(extension, []byte("export default function v2() {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	state.applyPluginScan(enabledRegistry, enabledRegistry, pluginScanPlan{}, false)
	if !state.pluginPiRefreshRequired {
		t.Fatal("a changed extension file did not raise a Pi refresh")
	}
}

// TestSetPluginEnabledAppliesWhenMarkerWriteFails pins review H3: a marker
// write failure never aborts the toggle halfway. The persistent choice, the
// cached WebView release and the rescan still happen, and the last notice the
// user sees is the marker warning rather than a success line.
func TestSetPluginEnabledAppliesWhenMarkerWriteFails(t *testing.T) {
	root := t.TempDir()
	manifestPath := writePluginManifest(t, root, "example", validPluginManifestMap())
	writePluginWebViewFixture(t, filepath.Dir(manifestPath))
	t.Setenv("BYOP_PLUGINS_DIR", root)
	configDir := t.TempDir()
	// A regular file where plugin-data would go makes every marker write fail.
	if err := os.WriteFile(filepath.Join(configDir, pluginDataDirectoryName), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The host resolves its config directory from the environment at
	// construction, so the persistent enable-state file lands here too and the
	// real ~/.byop stays untouched.
	t.Setenv("BYOP_CONFIG_DIR", configDir)
	host := newPluginHost(pluginHostVersion, nil, pluginHostWorkspace(t.TempDir()))
	pluginID := "example.git-workspace"
	state := &appState{
		page:    pagePlugins,
		plugins: host,
		pluginWebViews: map[string]*pluginWebViewState{
			pluginViewCacheKey(pluginID, "git.diff"): {pluginID: pluginID, viewID: "git.diff"},
		},
	}
	registry := scanPluginRegistryForRuntime()
	if registry.registeredCount() != 1 {
		t.Fatalf("fixture registered %d plugins", registry.registeredCount())
	}

	state.setPluginEnabled(registry.Entries[0], false)

	if ids := newPluginDisabledStore(configDir).load(); len(ids) != 1 || ids[0] != pluginID {
		t.Fatalf("the persistent disable did not happen: %v", ids)
	}
	if len(state.pluginWebViews) != 0 {
		t.Fatalf("cached WebViews survived the half-failed disable: %v", state.pluginWebViews)
	}
	applied := state.pluginRegistrySnapshot()
	withheld := false
	for _, entry := range applied.Entries {
		if entry.ID == pluginID && entry.Status == pluginDisabled {
			withheld = true
		}
	}
	if !withheld {
		t.Fatalf("the rescan did not publish the disabled state: %+v", applied.Entries)
	}
	name := fallback(registry.Entries[0].Name, pluginID)
	if state.notice == textArgs("pluginsDisabled", map[string]any{"name": name}) {
		t.Fatalf("the plain success notice buried the marker warning: %q", state.notice)
	}
	if !strings.Contains(state.notice, name) {
		t.Fatalf("the notice does not name the plugin: %q", state.notice)
	}
}

// TestScanFallsBackToLastKnownDisabledSet pins review H2: after a good read is
// remembered, a damaged file falls back to that set, so a scan never silently
// re-enables plugins the user disabled while the store is unreadable.
func TestScanFallsBackToLastKnownDisabledSet(t *testing.T) {
	root := t.TempDir()
	manifestPath := writePluginManifest(t, root, "example", validPluginManifestMap())
	writePluginWebViewFixture(t, filepath.Dir(manifestPath))
	configDir := t.TempDir()
	store := newPluginDisabledStore(configDir)
	if err := store.save([]string{"example.git-workspace"}); err != nil {
		t.Fatal(err)
	}
	roots := []pluginRootSpec{{Path: root, Explicit: true}}
	if got := scanPluginRegistryWithDisabled(roots, configDir).registeredCount(); got != 0 {
		t.Fatalf("registered with the saved set = %d, want 0", got)
	}
	// The good read is now remembered. Damage the file: the scan must still
	// withhold the plugin instead of re-enabling it.
	if err := os.WriteFile(store.path(), []byte("{damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := scanPluginRegistryWithDisabled(roots, configDir).registeredCount(); got != 0 {
		t.Fatalf("registered over the damaged file = %d, want the last known set to hold", got)
	}
}

// TestProblemCountIgnoresDisabled pins review H4: a deliberate disable is a
// user choice, not a scan problem.
func TestProblemCountIgnoresDisabled(t *testing.T) {
	registry := pluginRegistry{Entries: []pluginRegistryEntry{
		{ID: "a", Status: pluginDisabled},
		{ID: "b", Status: pluginInvalidManifest},
		{ID: "c", Status: pluginRegistered},
	}}
	if got := registry.problemCount(); got != 1 {
		t.Fatalf("problemCount = %d, want 1 for the invalid manifest only", got)
	}
}

func TestPluginHostDisableBlocksActivationUntilEnabled(t *testing.T) {
	entry, _ := writeNativeHelperPlugin(t, []string{"onCommand:hello.say"}, false)
	host := newTestPluginHost(t)

	if err := host.setPluginDisabledState(entry.ID, true); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if !host.isPluginDisabled(entry.ID) {
		t.Fatal("in-memory mirror missed the disable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := host.invokeCommand(ctx, entry, "hello.say", nil); err == nil ||
		!strings.Contains(err.Error(), "disabled") {
		t.Fatalf("disabled invoke = %v, want a disabled error", err)
	}
	if host.activeCount() != 0 {
		t.Fatalf("disabled plugin started a process: %d", host.activeCount())
	}
	if err := host.setPluginDisabledState(entry.ID, false); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if host.isPluginDisabled(entry.ID) {
		t.Fatal("enable left the mirror disabled")
	}
	if _, err := host.invokeCommand(ctx, entry, "hello.say", nil); err != nil {
		t.Fatalf("invoke after enable: %v", err)
	}

	// The stop gate blocks activation the same way, and releases again.
	host.beginPluginStop(entry.ID)
	if _, err := host.ensureActive(ctx, entry, "onCommand:hello.other"); !strings.Contains(
		err.Error(), "stopping") {
		t.Fatalf("stop gate error = %v, want stopping", err)
	}
	host.endPluginStop(entry.ID)
	if _, err := host.ensureActive(ctx, entry, "onCommand:hello.other"); err != nil {
		t.Fatalf("ensureActive after the gate released: %v", err)
	}
}

func TestStopPluginGenerationKeepsOtherGenerations(t *testing.T) {
	entry, _ := writeNativeHelperPlugin(t, []string{"onCommand:hello.say"}, false)
	host := newTestPluginHost(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := host.invokeCommand(ctx, entry, "hello.say", nil); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	current := host.process(entry.ID)
	if current == nil {
		t.Fatal("helper process was not registered")
	}

	// A captured generation that no longer owns the id must never be stopped.
	if host.stopPluginGeneration(entry.ID, &pluginProcess{}) {
		t.Fatal("stop claimed a process that does not own the id")
	}
	if host.process(entry.ID) != current {
		t.Fatal("the wrong-generation stop detached the live process")
	}

	// A held lease defers the stop, and releasing it lets it through.
	host.beginLease(entry.ID)
	if host.stopPluginGeneration(entry.ID, current) {
		t.Fatal("stop detached a process with a live lease")
	}
	host.endLease(entry.ID)
	if !host.stopPluginGeneration(entry.ID, current) {
		t.Fatal("stop declined the matching idle generation")
	}
	if host.process(entry.ID) != nil {
		t.Fatal("the stopped generation stayed registered")
	}
	if state := host.status(entry.ID).State; state != pluginStateStopped {
		t.Fatalf("state after stop = %s, want stopped", state)
	}
}

func TestPluginReclaimRoundReclaimsAfterWindowAndSparesVisible(t *testing.T) {
	entry, _ := writeNativeHelperPlugin(t, []string{"onCommand:hello.say"}, false)
	host := newTestPluginHost(t)
	host.idleReclaimDelay = 60 * time.Second
	state := &appState{
		page:    pagePlugins,
		plugins: host,
		pluginWebViews: map[string]*pluginWebViewState{
			pluginViewCacheKey(entry.ID, "main"):    {pluginID: entry.ID, viewID: "main"},
			pluginViewCacheKey("other.web", "main"): {pluginID: "other.web", viewID: "main"},
		},
		pluginBridgeSlots: make(map[string]chan struct{}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := host.invokeCommand(ctx, entry, "hello.say", nil); err != nil {
		t.Fatalf("invoke: %v", err)
	}

	now := time.Now()
	// The first round only starts the clock, even with a cached WebView.
	if got := state.pluginReclaimRound(now); got != 0 {
		t.Fatalf("first round reclaimed %d, want 0", got)
	}
	// Hide the view: a visible plugin is never reclaimed, and its cached WebView
	// survives, while the other hidden plugin passes on its own clock.
	state.page = pagePluginView
	state.pluginViewPluginID = entry.ID
	state.pluginViewKind = pluginViewWebView
	state.clearPluginHiddenSince(entry.ID)
	if got := state.pluginReclaimRound(now.Add(time.Hour)); got != 1 {
		t.Fatalf("round with a visible plugin reclaimed %d, want 1 for other.web", got)
	}
	if _, ok := state.pluginWebViews[pluginViewCacheKey(entry.ID, "main")]; !ok {
		t.Fatalf("visible plugin lost its cached WebView: %v", state.pluginWebViews)
	}
	// Back to hidden with both windows genuinely elapsed: the worker idle clock
	// and the hidden clock are both pushed two hours into the past, so the next
	// round is the one that acts.
	state.page = pagePlugins
	state.pluginViewPluginID = ""
	state.pluginViewKind = ""
	live := host.process(entry.ID)
	host.mu.Lock()
	idleStatus := host.statuses[entry.ID]
	idleStatus.UpdatedAt = time.Now().Add(-2 * time.Hour)
	host.statuses[entry.ID] = idleStatus
	host.mu.Unlock()
	state.pluginHiddenSinceByPlugin[entry.ID] = time.Now().Add(-2 * time.Hour)
	if got := state.pluginReclaimRound(time.Now()); got != 1 {
		t.Fatalf("elapsed round reclaimed %d, want 1", got)
	}
	if len(state.pluginWebViews) != 0 {
		t.Fatalf("cached WebViews survived: %v", state.pluginWebViews)
	}
	if live == nil {
		t.Fatal("helper reported no pid before the stop")
	}
	deadline := time.Now().Add(10 * time.Second)
	for host.process(entry.ID) != nil {
		if time.Now().After(deadline) {
			t.Fatal("idle worker was never stopped")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if state := host.status(entry.ID).State; state != pluginStateStopped {
		t.Fatalf("state after reclaim = %s, want stopped", state)
	}
}

func TestPluginReclaimTimerKeepsFiringUntilStopped(t *testing.T) {
	host := newTestPluginHost(t)
	host.idleReclaimDelay = time.Second // tick clamps to 1s
	state := &appState{page: pagePlugins, plugins: host}
	state.startPluginReclaim()
	defer state.stopPluginReclaim()

	deadline := time.Now().Add(10 * time.Second)
	for state.pluginReclaimRounds.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("timer fired %d rounds, want at least 2", state.pluginReclaimRounds.Load())
		}
		time.Sleep(20 * time.Millisecond)
	}
	state.stopPluginReclaim()
	seen := state.pluginReclaimRounds.Load()
	time.Sleep(2200 * time.Millisecond)
	if got := state.pluginReclaimRounds.Load(); got != seen {
		t.Fatalf("rounds kept running after stop: %d -> %d", seen, got)
	}
}
