package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/egoist/mygo"
)

func TestP8UTF16LEKeyMappingAndIdempotentImport(t *testing.T) {
	fixture := newP8MigrationFixture(t)
	preferences := defaultByopPreferences()

	scan := fixture.scan(t, preferences)
	assertP8Status(t, scan, "pi-theme", legacyStatusNew)
	assertP8Status(t, scan, "pi-workspace-v1", legacyStatusNew)
	assertP8Status(t, scan, "pi-web:last-open-by-workspace/"+filepath.Base(fixture.workspace), legacyStatusLinked)
	assertP8Status(t, scan, "pi-web:last-open-by-workspace/missing", legacyStatusMissing)
	assertP8Status(t, scan, "pi-chat-content-font-size", legacyStatusKept)
	assertP8Status(t, scan, "pi-sidebar-scale", legacyStatusKept)
	assertP8Status(t, scan, "pi-web:archived-projects", legacyStatusKept)
	assertP8Status(t, scan, "unrecognized-key", legacyStatusUnknown)
	if scan.Version != "0.7.4" || !scan.canImport {
		t.Fatalf("scan version/importability = %q/%v", scan.Version, scan.canImport)
	}

	next, result := applyLegacyImport(scan, preferences)
	if result.Theme != themeDark || next.Theme != string(themeDark) {
		t.Fatalf("imported theme = %q / %q, want dark", result.Theme, next.Theme)
	}
	canonicalWorkspace := canonicalPiPath(fixture.workspace)
	if next.Workspace != canonicalWorkspace {
		t.Fatalf("imported workspace = %q, want %q", next.Workspace, canonicalWorkspace)
	}
	if got := next.LastSessionByWorkspace[canonicalWorkspace]; got != "session-present" {
		t.Fatalf("linked session = %q, want session-present", got)
	}
	if got := next.Drafts["chat"].Text; got != "Draft body" {
		t.Fatalf("imported draft = %q", got)
	}
	if string(next.Legacy["pi-chat-content-font-size"].Value) != `"15"` ||
		string(next.Legacy["pi-sidebar-scale"].Value) != `"1.1"` {
		t.Fatalf("legacy numeric values = %#v", next.Legacy)
	}

	store := newByopPreferencesStore(fixture.configDir)
	if _, err := persistLegacyImport(fixture.configDir, store, next, result); err != nil {
		t.Fatalf("first import: %v", err)
	}
	saved, warning := store.load()
	if warning != "" {
		t.Fatalf("load saved preferences: %s", warning)
	}
	if saved.Theme != string(themeDark) || saved.Workspace != canonicalWorkspace {
		t.Fatalf("saved preferences = %#v", saved)
	}
	firstCounts := mapLengths(saved)
	firstImportedAt := saved.Legacy["pi-chat-content-font-size"].ImportedAt

	secondScan := fixture.scan(t, saved)
	assertP8Status(t, secondScan, "pi-web:archived-projects", legacyStatusSame)
	if secondScan.canImport || secondScan.importableCount != 0 {
		t.Fatalf("second scan should not offer another import: %#v", secondScan.Entries)
	}
	for _, entry := range secondScan.Entries {
		if entry.Status == legacyStatusConflict {
			t.Fatalf("second scan still reports a conflict: %#v", entry)
		}
	}
	second, secondResult := applyLegacyImport(secondScan, saved)
	if secondResult.WorkspaceChange || secondResult.Theme != themeMode(saved.Theme) {
		t.Fatalf("second import changed result: %#v", secondResult)
	}
	if got := mapLengths(second); got != firstCounts {
		t.Fatalf("preference map lengths changed: before %v, after %v", firstCounts, got)
	}
	if got := second.Legacy["pi-chat-content-font-size"].ImportedAt; got != firstImportedAt {
		t.Fatalf("same value ImportedAt changed from %q to %q", firstImportedAt, got)
	}
	if got := countP8NonEmptyLines(t, filepath.Join(fixture.configDir, "migration", "imports.jsonl")); got != 1 {
		t.Fatalf("imports.jsonl lines after unchanged rescan = %d, want 1", got)
	}
}

func TestP8ThemeConflictPreservesCurrentValue(t *testing.T) {
	fixture := newP8MigrationFixture(t)
	preferences := defaultByopPreferences()
	preferences.Theme = string(themeLight)

	scan := fixture.scan(t, preferences)
	assertP8Status(t, scan, "pi-theme", legacyStatusConflict)
	next, result := applyLegacyImport(scan, preferences)
	if next.Theme != string(themeLight) || result.Theme != themeLight {
		t.Fatalf("conflict changed theme to %q / %q", next.Theme, result.Theme)
	}
	legacy, exists := next.Legacy["pi-theme"]
	if !exists || string(legacy.Value) != `"dark"` {
		t.Fatalf("conflicting theme was not kept: %#v", legacy)
	}
	rescan := fixture.scan(t, next)
	assertP8Status(t, rescan, "pi-theme", legacyStatusSame)
}

func TestP8ThemeChangedReflectsAppliedImport(t *testing.T) {
	_, noTheme := applyLegacyImport(legacyScan{}, defaultByopPreferences())
	if noTheme.ThemeChanged {
		t.Fatal("import without a legacy theme reported a theme change")
	}

	_, importedTheme := applyLegacyImport(legacyScan{
		Actions: []legacyImportAction{{
			Kind: legacyActionTheme,
			Text: string(themeDark),
		}},
	}, defaultByopPreferences())
	if !importedTheme.ThemeChanged {
		t.Fatal("imported legacy theme was not reported as a theme change")
	}
}

func TestP8ImportRechecksConflictsAgainstCurrentPreferences(t *testing.T) {
	fixture := newP8MigrationFixture(t)
	scan := fixture.scan(t, defaultByopPreferences())

	current := defaultByopPreferences()
	current.Theme = string(themeLight)
	current.Workspace = t.TempDir()
	current.Drafts["chat"] = legacyDraft{Text: "Current draft"}
	canonicalWorkspace := canonicalPiPath(fixture.workspace)
	current.LastSessionByWorkspace[canonicalWorkspace] = "current-session"

	next, _ := applyLegacyImport(scan, current)
	if next.Theme != string(themeLight) || next.Workspace != current.Workspace {
		t.Fatalf("stale import replaced current theme/workspace: %#v", next)
	}
	if got := next.Drafts["chat"].Text; got != "Current draft" {
		t.Fatalf("stale import replaced current draft: %q", got)
	}
	if got := next.LastSessionByWorkspace[canonicalWorkspace]; got != "current-session" {
		t.Fatalf("stale import replaced current session link: %q", got)
	}
	if got := string(next.Legacy["pi-theme"].Value); got != `"dark"` {
		t.Fatalf("stale theme conflict was not preserved: %q", got)
	}
	if got := string(next.Legacy["pi-web:drafts:chat"].Value); got != `"Draft body"` {
		t.Fatalf("stale draft conflict was not preserved: %q", got)
	}
}

func TestP8DamagedPreferencesArePreserved(t *testing.T) {
	configDir := t.TempDir()
	path := filepath.Join(configDir, "preferences.json")
	original := []byte(`{"version":1,"theme":`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	store := newByopPreferencesStore(configDir)
	preferences, warning := store.load()
	if warning == "" || preferences.Version != byopPreferencesVersion {
		t.Fatalf("damaged preferences = %#v, warning %q", preferences, warning)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != string(original) {
		t.Fatalf("load modified damaged file: %q, %v", data, err)
	}

	scan := legacyScan{ImportedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if _, err := persistLegacyImport(configDir, store, preferences, legacyImportResult{
		Record: legacyImportRecord{At: scan.ImportedAt},
	}); err != nil {
		t.Fatalf("persist after damaged load: %v", err)
	}
	corrupt, err := filepath.Glob(filepath.Join(configDir, "preferences.corrupt-*.json"))
	if err != nil || len(corrupt) != 1 {
		t.Fatalf("corrupt backups = %v, %v", corrupt, err)
	}
	if data, err := os.ReadFile(corrupt[0]); err != nil || string(data) != string(original) {
		t.Fatalf("corrupt backup changed: %q, %v", data, err)
	}
}

func TestP8ImportKeepsSavedPreferencesWhenHistoryAppendFails(t *testing.T) {
	configDir := t.TempDir()
	preferencesPath := filepath.Join(configDir, "preferences.json")
	original := []byte(`{"version":1,"theme":`)
	if err := os.WriteFile(preferencesPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	importHistoryPath := filepath.Join(configDir, "migration", "imports.jsonl")
	if err := os.MkdirAll(importHistoryPath, 0o700); err != nil {
		t.Fatal(err)
	}

	store := newByopPreferencesStore(configDir)
	preferences, warning := store.load()
	if warning == "" {
		t.Fatal("expected damaged preferences warning")
	}
	next := preferences
	next.Theme = string(themeDark)
	scan := legacyScan{ImportedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	persistence, err := persistLegacyImport(configDir, store, next, legacyImportResult{
		Record: legacyImportRecord{At: scan.ImportedAt},
		Theme:  themeDark,
	})
	if err == nil || !persistence.PreferencesSaved || persistence.HistorySaved {
		t.Fatalf("persistence/error = %#v / %v", persistence, err)
	}
	saved, savedWarning := store.load()
	if savedWarning != "" || saved.Theme != string(themeDark) {
		t.Fatalf("saved preferences = %#v, warning %q", saved, savedWarning)
	}
	corrupt, globErr := filepath.Glob(filepath.Join(configDir, "preferences.corrupt-*.json"))
	if globErr != nil || len(corrupt) != 1 {
		t.Fatalf("corrupt backups = %v, %v", corrupt, globErr)
	}
}

func TestP8LocalStorageScanDoesNotModifySource(t *testing.T) {
	fixture := newP8MigrationFixture(t)
	if _, err := os.Stat(fixture.localStorage + "-shm"); !os.IsNotExist(err) {
		t.Fatalf("WAL fixture unexpectedly has -shm: %v", err)
	}
	header := make([]byte, 20)
	file, err := os.Open(fixture.localStorage)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Read(header); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if header[18] != 2 || header[19] != 2 {
		t.Fatalf("fixture journal mode bytes = %02x %02x, want 02 02", header[18], header[19])
	}
	before, err := os.Stat(fixture.localStorage)
	if err != nil {
		t.Fatal(err)
	}
	scan := fixture.scan(t, defaultByopPreferences())
	after, err := os.Stat(fixture.localStorage)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("source sqlite changed: before %d/%s, after %d/%s",
			before.Size(), before.ModTime(), after.Size(), after.ModTime())
	}
	assertP8Status(t, scan, "pi-theme", legacyStatusNew)
	assertP8Status(t, scan, "pi-workspace-v1", legacyStatusNew)
	if scan.ScanWarning != "" {
		t.Fatalf("WAL scan warning = %q", scan.ScanWarning)
	}
}

func TestP8EnvironmentThemeOverridesPreferences(t *testing.T) {
	t.Setenv("BYOP_CONFIG_DIR", t.TempDir())
	t.Setenv("BYOP_THEME", "dark")
	t.Setenv("BYOP_PLUGINS_DIR", t.TempDir())
	t.Setenv("BYOP_LEGACY_APP_DIR", filepath.Join(t.TempDir(), "app"))
	t.Setenv("BYOP_LEGACY_LOCALSTORAGE", filepath.Join(t.TempDir(), "localstorage.sqlite3"))
	t.Setenv("BYOP_LEGACY_PI_AGENT_DIR", filepath.Join(t.TempDir(), "agent"))
	store := newByopPreferencesStore(os.Getenv("BYOP_CONFIG_DIR"))
	preferences := defaultByopPreferences()
	preferences.Theme = string(themeLight)
	if err := store.save(preferences); err != nil {
		t.Fatal(err)
	}
	if state := newAppState(); state.themeMode != themeDark {
		t.Fatalf("theme mode = %q, want dark", state.themeMode)
	}
}

func TestP8PreferenceWritesDoNotPersistEnvironmentState(t *testing.T) {
	originalSetThemeSource := setMygoThemeSource
	originalCurrentAppearanceDark := currentAppearanceDark
	setMygoThemeSource = func(mygo.ThemeSource) {}
	currentAppearanceDark = func() bool { return false }
	t.Cleanup(func() {
		setMygoThemeSource = originalSetThemeSource
		currentAppearanceDark = originalCurrentAppearanceDark
	})

	t.Setenv("BYOP_CONFIG_DIR", t.TempDir())
	t.Setenv("BYOP_THEME", "light")
	t.Setenv("BYOP_TASKS", "1")
	t.Setenv("BYOP_PLUGINS_DIR", t.TempDir())
	t.Setenv("BYOP_LEGACY_APP_DIR", filepath.Join(t.TempDir(), "app"))
	t.Setenv("BYOP_LEGACY_LOCALSTORAGE", filepath.Join(t.TempDir(), "localstorage.sqlite3"))
	t.Setenv("BYOP_LEGACY_PI_AGENT_DIR", filepath.Join(t.TempDir(), "agent"))

	state := newAppState()
	state.setTheme(themeDark)
	saved, warning := state.preferencesStore.load()
	if warning != "" {
		t.Fatalf("load preferences: %s", warning)
	}
	if saved.Theme != string(themeDark) || saved.TasksOpen {
		t.Fatalf("theme write persisted unrelated environment state: %#v", saved)
	}

	state.setNavCollapsed(true)
	saved, warning = state.preferencesStore.load()
	if warning != "" {
		t.Fatalf("load navigation preferences: %s", warning)
	}
	if !saved.NavCollapsed || saved.TasksOpen {
		t.Fatalf("navigation write persisted unrelated environment state: %#v", saved)
	}

	state.setTasksOpen(true)
	saved, warning = state.preferencesStore.load()
	if warning != "" {
		t.Fatalf("load task preferences: %s", warning)
	}
	if !saved.NavCollapsed || !saved.TasksOpen {
		t.Fatalf("task write lost independently persisted fields: %#v", saved)
	}
}

func TestP8DamagedPreferencesBlockAutomaticSessionWrites(t *testing.T) {
	configDir := t.TempDir()
	path := filepath.Join(configDir, "preferences.json")
	original := []byte(`{"version":1,"theme":`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	store := newByopPreferencesStore(configDir)
	preferences, warning := store.load()
	if warning == "" {
		t.Fatal("expected damaged preferences warning")
	}
	workspace := t.TempDir()
	state := &appState{
		configDir:          configDir,
		preferences:        preferences,
		preferencesStore:   store,
		preferencesWarning: warning,
		piCwd:              workspace,
	}
	state.recordLastSession("session-fake")
	if got := state.preferences.LastSessionByWorkspace[canonicalPiPath(workspace)]; got != "session-fake" {
		t.Fatalf("in-memory session link = %q", got)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != string(original) {
		t.Fatalf("automatic session link changed damaged file: %q, %v", data, err)
	}
	if backups, err := filepath.Glob(filepath.Join(configDir, "preferences.corrupt-*.json")); err != nil ||
		len(backups) != 0 {
		t.Fatalf("automatic session link renamed damaged file: %v, %v", backups, err)
	}
}

func TestP8UIErrorTextHidesLocalPaths(t *testing.T) {
	got := uiErrorText(errors.New("open /Users/private/example/file.json: permission denied"), "failed")
	if strings.Contains(got, "/Users/") || !strings.Contains(got, "file.json") {
		t.Fatalf("UI error text = %q", got)
	}
}

func TestP8UIErrorPathRedactionIsAnchored(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "model path",
			text: "model anthropic/claude-sonnet not found",
			want: "model anthropic/claude-sonnet not found",
		},
		{
			name: "https URL",
			text: "HTTP 401 from https://api.example.com/v1/chat",
			want: "HTTP 401 from https://api.example.com/v1/chat",
		},
		{
			name: "fraction",
			text: "retry 2/3 failed: timeout",
			want: "retry 2/3 failed: timeout",
		},
		{
			name: "absolute path",
			text: "open /Users/x/secret/file.json: no such file",
			want: "open file.json: no such file",
		},
		{
			name: "file URL",
			text: "file:///Users/x/a.txt",
			want: "a.txt",
		},
		{
			name: "windows path",
			text: `open "C:\Users\x\secret\file.json": access denied`,
			want: `open "file.json": access denied`,
		},
		{
			name: "equals delimiter",
			text: "cwd=/Users/x/proj: exit 1",
			want: "cwd=proj: exit 1",
		},
		{
			name: "bracket delimiter",
			text: "[/Users/x/a.txt,",
			want: "[a.txt,",
		},
		{
			name: "windows file URL",
			text: "file:///C:/Users/x/a.txt",
			want: "a.txt",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := uiErrorText(errors.New(test.text), "failed"); got != test.want {
				t.Fatalf("uiErrorText(%q) = %q, want %q", test.text, got, test.want)
			}
		})
	}
}

func TestP8LegacyConflictKeepsExistingValueAndConverges(t *testing.T) {
	preferences := defaultByopPreferences()
	preferences.Legacy["pi-sidebar-width"] = legacyValue{
		Value: json.RawMessage(`"300"`), Source: "localStorage", ImportedAt: "original",
	}
	raw := json.RawMessage(`"360"`)
	scan := legacyScan{}
	addLegacyLocalValue(
		&scan, preferences, "pi-sidebar-width", raw, "localStorage", "pi-sidebar-width")
	scan.canImport, scan.importableCount = legacyScanImportability(scan.Entries)

	entry := findP8Entry(t, scan, "pi-sidebar-width")
	if entry.Status != legacyStatusConflict || entry.action == nil {
		t.Fatalf("conflict entry = %#v", entry)
	}
	if got := entry.statusLabel(); got != "IMPORT AS CONFLICT COPY" {
		t.Fatalf("conflict status label = %q", got)
	}
	direct := legacyPreviewEntry{
		Status: legacyStatusConflict,
		action: &legacyImportAction{
			Kind: legacyActionLegacyValue, Key: "pi-sidebar-width",
			Raw: json.RawMessage(`"360"`),
		},
	}
	if got := direct.statusLabel(); got != "IMPORT CONFLICTING VALUE" {
		t.Fatalf("direct conflict status label = %q", got)
	}
	if !scan.canImport || scan.importableCount != 1 {
		t.Fatalf("conflict importability = %v/%d", scan.canImport, scan.importableCount)
	}

	next, _ := applyLegacyImport(scan, preferences)
	if got := string(next.Legacy["pi-sidebar-width"].Value); got != `"300"` {
		t.Fatalf("existing legacy value changed to %q", got)
	}
	if got := string(next.Legacy["pi-sidebar-width:conflict"].Value); got != `"360"` {
		t.Fatalf("conflict copy = %q", got)
	}

	rescan := legacyScan{}
	addLegacyLocalValue(
		&rescan, next, "pi-sidebar-width", raw, "localStorage", "pi-sidebar-width")
	entry = findP8Entry(t, rescan, "pi-sidebar-width")
	if entry.Status != legacyStatusSame || entry.action != nil {
		t.Fatalf("rescan conflict = %#v", entry)
	}
}

func TestP8LegacyRawEqualAfterPreferencesRoundTrip(t *testing.T) {
	configDir := t.TempDir()
	preferences := defaultByopPreferences()
	preferences.Legacy["pi-sidebar-width"] = legacyValue{
		Value: json.RawMessage(`"300"`), Source: "localStorage", ImportedAt: "original",
	}

	scan := legacyScan{}
	addLegacyLocalValue(
		&scan, preferences, "pi-sidebar-width", json.RawMessage(`"a&b"`),
		"localStorage", "pi-sidebar-width")
	addLegacyDrafts(
		&scan, preferences, json.RawMessage(`{"chat":"if a && b <x>"}`), legacyDraftsKey)
	scan.canImport, scan.importableCount = legacyScanImportability(scan.Entries)

	next, _ := applyLegacyImport(scan, preferences)
	store := newByopPreferencesStore(configDir)
	if err := store.save(next); err != nil {
		t.Fatalf("save preferences: %v", err)
	}
	saved, warning := store.load()
	if warning != "" {
		t.Fatalf("load preferences: %s", warning)
	}

	rescan := legacyScan{}
	addLegacyLocalValue(
		&rescan, saved, "pi-sidebar-width", json.RawMessage(`"a&b"`),
		"localStorage", "pi-sidebar-width")
	addLegacyDrafts(
		&rescan, saved, json.RawMessage(`{"chat":"if a && b <x>"}`), legacyDraftsKey)
	for _, id := range []string{"pi-sidebar-width", legacyDraftsKey + "/chat"} {
		entry := findP8Entry(t, rescan, id)
		if entry.Status != legacyStatusSame || entry.action != nil {
			t.Fatalf("%s rescan = %#v, want same without action", id, entry)
		}
	}
	rescan.canImport, rescan.importableCount = legacyScanImportability(rescan.Entries)
	if rescan.canImport || rescan.importableCount != 0 {
		t.Fatalf("rescan importability = %v/%d", rescan.canImport, rescan.importableCount)
	}
}

func TestP8ThemeAndWorkspaceConflictsUseSeparateCopies(t *testing.T) {
	preferences := defaultByopPreferences()
	preferences.Theme = string(themeDark)
	preferences.Legacy["pi-theme"] = legacyValue{
		Value: json.RawMessage(`"light"`), Source: "localStorage", ImportedAt: "original",
	}
	preferences.Legacy["pi-workspace-v1.rightPanelOpen"] = legacyValue{
		Value: json.RawMessage(`false`), Source: "localStorage", ImportedAt: "original",
	}

	scan := legacyScan{}
	addLegacyTheme(&scan, preferences, json.RawMessage(`"sepia"`), "localStorage")
	addLegacyWorkspaceWithSession(
		&scan, preferences, json.RawMessage(`{"rightPanelOpen":true}`), nil)
	scan.canImport, scan.importableCount = legacyScanImportability(scan.Entries)
	if !scan.canImport || scan.importableCount != 2 {
		t.Fatalf("conflict importability = %v/%d", scan.canImport, scan.importableCount)
	}

	next, _ := applyLegacyImport(scan, preferences)
	if next.Theme != string(themeDark) {
		t.Fatalf("theme conflict changed current theme to %q", next.Theme)
	}
	if got := string(next.Legacy["pi-theme"].Value); got != `"light"` {
		t.Fatalf("existing theme value changed to %q", got)
	}
	if got := string(next.Legacy["pi-theme:conflict"].Value); got != `"sepia"` {
		t.Fatalf("theme conflict copy = %q", got)
	}
	if got := string(next.Legacy["pi-workspace-v1.rightPanelOpen"].Value); got != `false` {
		t.Fatalf("existing workspace field changed to %q", got)
	}
	if got := string(next.Legacy["pi-workspace-v1.rightPanelOpen:conflict"].Value); got != `true` {
		t.Fatalf("workspace conflict copy = %q", got)
	}
}

func TestP8CanonicalizesLastSessionLookupKeys(t *testing.T) {
	workspace := t.TempDir()
	alias := filepath.Join(t.TempDir(), "workspace-link")
	if err := os.Symlink(workspace, alias); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	workspaceRaw, err := json.Marshal(workspace)
	if err != nil {
		t.Fatal(err)
	}
	sessionRaw := json.RawMessage(`"session-present"`)
	scan := legacyScan{}
	addLegacyWorkspaceWithSession(
		&scan,
		defaultByopPreferences(),
		json.RawMessage(`{"cwd":`+string(workspaceRaw)+`,"sessionId":"session-present"}`),
		map[string]json.RawMessage{alias: sessionRaw},
	)
	entry := findP8Entry(t, scan, "pi-workspace-v1/sessionId")
	if entry.Status != legacyStatusInfo ||
		entry.Summary != "Workspace session also exists in last-open map" {
		t.Fatalf("canonical session lookup entry = %#v", entry)
	}
}

func TestP8EmptyWorkspaceDoesNotCanonicalizeToCurrentDirectory(t *testing.T) {
	if _, found := canonicalLegacySessionValue(
		map[string]json.RawMessage{"": json.RawMessage(`"session"`)}, ""); found {
		t.Fatal("empty workspace matched an empty lastSessions key")
	}
}

type p8MigrationFixture struct {
	configDir      string
	workspace      string
	localStorage   string
	sessionRoot    string
	legacyAppDir   string
	legacyAgentDir string
}

func newP8MigrationFixture(t *testing.T) p8MigrationFixture {
	t.Helper()
	sqlite, err := exec.LookPath("/usr/bin/sqlite3")
	if err != nil {
		sqlite, err = exec.LookPath("sqlite3")
	}
	if err != nil {
		t.Skip("sqlite3 is unavailable")
	}

	root := t.TempDir()
	fixture := p8MigrationFixture{
		configDir:      filepath.Join(root, "config"),
		workspace:      filepath.Join(root, "workspace"),
		localStorage:   filepath.Join(root, "legacy", "localstorage.sqlite3"),
		sessionRoot:    filepath.Join(root, "pi-sessions"),
		legacyAppDir:   filepath.Join(root, "legacy-app"),
		legacyAgentDir: filepath.Join(root, "legacy-agent"),
	}
	for _, directory := range []string{
		fixture.configDir, fixture.workspace, filepath.Dir(fixture.localStorage),
		fixture.sessionRoot, fixture.legacyAppDir, fixture.legacyAgentDir,
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	writeP8File(t, filepath.Join(fixture.legacyAppDir, "last-version.json"), `{"version":"0.7.4"}`)
	writeP8File(t, filepath.Join(fixture.legacyAppDir, ".window-state.json"), `{"width":1280,"height":720}`)
	writeP8File(t, filepath.Join(fixture.legacyAppDir, "ui-prefs.json"),
		`{"theme":"light","locale":"zh-CN","serverPort":1234}`)

	presentID := "session-present"
	missingID := "session-missing"
	session := `{"type":"session","id":"` + presentID + `","timestamp":"2026-10-21T00:00:00Z","cwd":"` +
		fixture.workspace + `"}` + "\n"
	writeP8File(t, filepath.Join(fixture.sessionRoot, "session.jsonl"), session)

	lastOpen, err := json.Marshal(map[string]string{
		fixture.workspace:              presentID,
		filepath.Join(root, "missing"): missingID,
	})
	if err != nil {
		t.Fatal(err)
	}
	workspaceValue, err := json.Marshal(map[string]any{
		"cwd":            fixture.workspace,
		"rightPanelOpen": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	drafts := json.RawMessage(`{"chat":"Draft body"}`)
	values := map[string]string{
		"pi-theme":                      `dark`,
		"pi-workspace-v1":               string(workspaceValue),
		"pi-web:last-open-by-workspace": string(lastOpen),
		"pi-chat-content-font-size":     `"15"`,
		"pi-sidebar-scale":              `"1.1"`,
		"pi-web:archived-projects":      `["project-a","project-b"]`,
		"pi-web:drafts":                 string(drafts),
		"unrecognized-key":              `"unknown"`,
	}
	createP8SQLite(t, sqlite, fixture.localStorage, values)

	t.Setenv("BYOP_CONFIG_DIR", fixture.configDir)
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", fixture.sessionRoot)
	t.Setenv("BYOP_LEGACY_APP_DIR", fixture.legacyAppDir)
	t.Setenv("BYOP_LEGACY_LOCALSTORAGE", fixture.localStorage)
	t.Setenv("BYOP_LEGACY_PI_AGENT_DIR", fixture.legacyAgentDir)
	t.Setenv("BYOP_WORKSPACE", "")
	return fixture
}

func (fixture p8MigrationFixture) scan(t *testing.T, preferences byopPreferences) legacyScan {
	t.Helper()
	sources, err := defaultLegacyMigrationSources()
	if err != nil {
		t.Fatal(err)
	}
	scan, err := scanLegacyDesktop(context.Background(), sources, preferences)
	if err != nil {
		t.Fatal(err)
	}
	return scan
}

func createP8SQLite(t *testing.T, sqlite, path string, values map[string]string) {
	t.Helper()
	var statements []string
	statements = append(statements,
		"PRAGMA journal_mode=WAL;",
		"CREATE TABLE ItemTable(key TEXT PRIMARY KEY, value BLOB);")
	for key, value := range values {
		hexValue := hex.EncodeToString(p8UTF16LEBytes(value))
		statements = append(statements, "INSERT INTO ItemTable(key, value) VALUES("+
			p8SQLString(key)+", X'"+hexValue+"');")
	}
	statements = append(statements, "PRAGMA wal_checkpoint(TRUNCATE);")
	cmd := exec.Command(sqlite, path, strings.Join(statements, "\n"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create sqlite fixture: %v: %s", err, output)
	}
	shmPath := path + "-shm"
	if _, err := os.Stat(shmPath); err == nil {
		if err := os.Rename(shmPath, shmPath+".fixture-moved"); err != nil {
			t.Fatalf("move sqlite fixture -shm: %v", err)
		}
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat sqlite fixture -shm: %v", err)
	}
}

func p8UTF16LEBytes(value string) []byte {
	units := utf16.Encode([]rune(value))
	data := make([]byte, 0, len(units)*2)
	for _, unit := range units {
		data = append(data, byte(unit), byte(unit>>8))
	}
	return data
}

func p8SQLString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func writeP8File(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertP8Status(t *testing.T, scan legacyScan, id string, want legacyImportStatus) {
	t.Helper()
	entry := findP8Entry(t, scan, id)
	if entry.Status != want {
		t.Fatalf("%s status = %q, want %q", id, entry.Status, want)
	}
}

func findP8Entry(t *testing.T, scan legacyScan, id string) legacyPreviewEntry {
	t.Helper()
	for _, entry := range scan.Entries {
		if entry.ID == id {
			return entry
		}
	}
	t.Fatalf("preview entry %q not found: %#v", id, scan.Entries)
	return legacyPreviewEntry{}
}

func mapLengths(preferences byopPreferences) [3]int {
	return [3]int{
		len(preferences.Drafts),
		len(preferences.Legacy),
		len(preferences.LastSessionByWorkspace),
	}
}

func countP8NonEmptyLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}
