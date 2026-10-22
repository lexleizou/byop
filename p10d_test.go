package main

import (
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestP10dLegacyMigrationPromptVisibility(t *testing.T) {
	newPromptState := func() *appState {
		return &appState{
			legacyScanned: true,
			legacyScan: legacyScan{
				canImport: true,
				Actions:   []legacyImportAction{{Kind: legacyActionTheme}},
				Sources: []legacySourcePreview{{
					Source: "app-dir",
					Status: "found",
				}},
			},
			preferences: defaultByopPreferences(),
		}
	}
	state := newPromptState()
	if !state.legacyMigrationPromptVisible() {
		t.Fatal("importable scan should show the migration prompt")
	}
	if !state.legacyMigrationReimportVisible() {
		t.Fatal("found app source should show the settings re-import link")
	}

	dismissed := newPromptState()
	dismissed.preferences.LegacyMigrationPromptDismissed = true
	if dismissed.legacyMigrationPromptVisible() {
		t.Fatal("dismissed migration prompt should stay hidden")
	}

	failed := newPromptState()
	failed.legacyScanFailed = true
	if failed.legacyMigrationPromptVisible() || failed.legacyMigrationReimportVisible() {
		t.Fatal("failed scan should hide both migration prompt and re-import link")
	}

	empty := newPromptState()
	empty.legacyScan.Actions = nil
	empty.legacyScan.canImport = false
	if empty.legacyMigrationPromptVisible() {
		t.Fatal("up-to-date scan should not show the migration prompt")
	}
}

func TestP10dDismissLegacyMigrationPromptPersists(t *testing.T) {
	newP8MigrationFixture(t)
	state := newAppState()
	state.dismissLegacyMigrationPrompt()
	if !state.preferences.LegacyMigrationPromptDismissed {
		t.Fatal("dismissal was not applied in memory")
	}
	saved, warning := state.preferencesStore.load()
	if warning != "" {
		t.Fatalf("load preferences: %s", warning)
	}
	if !saved.LegacyMigrationPromptDismissed {
		t.Fatal("dismissal was not persisted")
	}
}

func TestP10dLegacyImportDismissesMigrationPrompt(t *testing.T) {
	originalSetThemeSource := setMygoThemeSource
	originalCurrentAppearanceDark := currentAppearanceDark
	setMygoThemeSource = func(mygo.ThemeSource) {}
	currentAppearanceDark = func() bool { return false }
	t.Cleanup(func() {
		setMygoThemeSource = originalSetThemeSource
		currentAppearanceDark = originalCurrentAppearanceDark
	})

	fixture := newP8MigrationFixture(t)
	state := newAppState()
	scan := fixture.scan(t, state.preferences)
	if !scan.canImport || len(scan.Actions) == 0 {
		t.Fatalf("fixture scan is not importable: %#v", scan)
	}
	if !state.completeLegacyImport(scan, false) {
		t.Fatal("legacy import failed")
	}
	if !state.preferences.LegacyMigrationPromptDismissed {
		t.Fatal("successful legacy import did not dismiss the prompt")
	}
	saved, warning := state.preferencesStore.load()
	if warning != "" {
		t.Fatalf("load preferences: %s", warning)
	}
	if !saved.LegacyMigrationPromptDismissed {
		t.Fatal("successful legacy import did not persist prompt dismissal")
	}
}

func TestP10dPiErrorDetailsOnlyRenderForConnectionErrors(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	renderSettings := func(t *testing.T, setup func(*appState)) *ui.Tester {
		t.Helper()
		state := layoutTestState(t)
		state.page = pageSettings
		setup(state)
		tester := ui.NewTester(state.view, 1180, 1100)
		tester.Scroll(590, 550, 0, 620)
		return tester
	}

	t.Run("connection error can expand raw details", func(t *testing.T) {
		const raw = "Pi executable rejected by the operating system"
		tester := renderSettings(t, func(state *appState) {
			state.piConn = piConnectionErr
			state.piErrorCode = piErrorCodeNotExecutable
			state.piError = raw
			state.piErrorDetailsOpen = true
		})
		if _, ok := tester.Find(text("settingsHidePiDetails")); !ok {
			t.Fatal("connection error did not render the details control")
		}
		if _, ok := tester.Find(raw); !ok {
			t.Fatal("expanded connection error did not render raw details")
		}
	})

	t.Run("connected state hides stale details", func(t *testing.T) {
		const raw = "stale connection detail"
		tester := renderSettings(t, func(state *appState) {
			state.piConn = piConnected
			state.piError = raw
			state.piErrorDetailsOpen = true
		})
		if _, ok := tester.Find(text("settingsHidePiDetails")); ok {
			t.Fatal("connected state rendered the connection details control")
		}
		if _, ok := tester.Find(raw); ok {
			t.Fatal("connected state rendered stale connection details")
		}
	})

	t.Run("connected business error hides details", func(t *testing.T) {
		const raw = "business error detail"
		tester := renderSettings(t, func(state *appState) {
			state.piConn = piConnected
			state.piErrorCode = piErrorCodeUnknown
			state.piError = raw
			state.piErrorDetailsOpen = true
		})
		if _, ok := tester.Find(text("settingsHidePiDetails")); ok {
			t.Fatal("connected business error rendered the connection details control")
		}
		if _, ok := tester.Find(raw); ok {
			t.Fatal("connected business error rendered raw connection details")
		}
	})
}
