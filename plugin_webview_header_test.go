package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestPluginWebViewHeaderHidesRepeatedPluginTitle(t *testing.T) {
	entry := pluginRegistryEntry{ID: "example.fixture", Name: "Fixture"}
	view := pluginWebViewContribution{ID: "main", Title: "Fixture"}
	state := &appState{}
	tester := ui.NewTester(func(c *ui.Context) {
		theme, p := nativeThemeWithContrast(false, false)
		c.SetTheme(theme)
		state.pluginWebViewHeader(c, p, entry, view)
	}, 800, 120)
	count := 0
	for _, value := range tester.Texts() {
		if value == "Fixture" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("plugin title rendered %d times, want 1: %#v", count, tester.Texts())
	}
}
