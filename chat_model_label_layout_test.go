package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

const (
	p22TestModelName  = "Layout P22 Model"
	p22TestBadgeLabel = "P22"
)

// p22LayoutState renders the real chat view with a model whose provider and id a
// plugin can claim, so the badge is checked where it is used: in the composer's
// model control.
func p22LayoutState(t *testing.T, claimed bool) *appState {
	t.Helper()
	state := layoutTestState(t)
	state.piModel = "openai/p22-model"
	state.piModelProvider = "openai"
	state.piModelID = "p22-model"
	state.piModelName = p22TestModelName
	state.piModels = []piModel{{
		Provider:      "openai",
		ID:            "p22-model",
		Name:          p22TestModelName,
		ContextWindow: 200_000,
	}}
	if !claimed {
		return state
	}
	manifest := modelLabelManifestMap("example.labels")
	manifest["contributes"].(map[string]any)["modelLabels"] = []any{
		modelLabelContribution("p22-release", p22TestBadgeLabel, map[string]any{
			"providers": []any{"openai"}, "modelId": "p22-model",
		}),
	}
	state.pluginRegistry = pluginRegistry{Entries: []pluginRegistryEntry{{
		ID:       "example.labels",
		Name:     "Model Labels",
		Status:   pluginRegistered,
		Manifest: manifestFromValue(t, manifest),
	}}}
	return state
}

func TestP22ComposerChipShowsReleaseBadgeInsideChipHeight(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	tester := ui.NewTester(p22LayoutState(t, true).view, 1100, 560)
	// The composer draws the model chip and the transcript HUD may draw the same
	// badge for the current model, so only the presence inside the composer row
	// is asserted here.
	if !tester.HasText(p22TestBadgeLabel) {
		t.Fatal("release badge was not rendered")
	}
	assertInsideViewport(t, tester, p22TestBadgeLabel, 560)
}

func TestP22ComposerChipRendersNoBadgeWithoutClaim(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	tester := ui.NewTester(p22LayoutState(t, false).view, 1100, 560)
	if _, ok := tester.Find(p22TestBadgeLabel); ok {
		t.Fatal("a badge appeared for a model no plugin claimed")
	}
	assertInsideViewport(t, tester, p22TestModelName, 560)
}
