package main

import (
	"strings"
	"testing"
)

func modelLabelManifestMap(pluginID string) map[string]any {
	return map[string]any{
		"schemaVersion": 1,
		"id":            pluginID,
		"name":          "Model Labels",
		"version":       "0.1.0",
		"runtime":       "node",
		"entry":         "plugin.mjs",
		"compatibility": map[string]any{"host": ">=0.18.7 <1.0.0", "pluginApi": "1"},
		"contributes":   map[string]any{"modelLabels": []any{}},
		"activation":    map[string]any{"events": []any{}},
		"capabilities": map[string]any{
			"workspace": "none", "files": "none", "network": false, "process": "none",
		},
	}
}

func modelLabelContribution(id, badge string, match map[string]any) map[string]any {
	return map[string]any{"id": id, "badge": badge, "match": match}
}

func TestLoadPluginManifestAcceptsModelLabels(t *testing.T) {
	manifest := modelLabelManifestMap("example.labels")
	manifest["contributes"].(map[string]any)["modelLabels"] = []any{
		modelLabelContribution("deepseek-flash-1017", "1017", map[string]any{
			"providers": []any{"deepseek", "magpie"}, "modelId": "deepseek-flash",
		}),
		modelLabelContribution("qwen-latest", "IQ3", map[string]any{
			"providers": []any{"local"}, "modelName": "Qwen3.8 Flash Next",
		}),
	}

	loaded, err := loadPluginManifest(writePluginManifest(t, t.TempDir(), "labels", manifest))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if loaded.contributionCount() != 2 {
		t.Fatalf("contribution count = %d, want 2", loaded.contributionCount())
	}
	labels := loaded.Contributes.ModelLabels
	if len(labels) != 2 || labels[0].Badge != "1017" || labels[1].Match.ModelName != "Qwen3.8 Flash Next" {
		t.Fatalf("unexpected model labels: %#v", labels)
	}
	if providers := labels[0].Match.Providers; len(providers) != 2 || providers[1] != "magpie" {
		t.Fatalf("unexpected providers: %#v", providers)
	}
}

func TestLoadPluginManifestRejectsInvalidModelLabels(t *testing.T) {
	tests := []struct {
		name    string
		match   map[string]any
		badge   string
		id      string
		wantErr string
	}{
		{
			name:    "empty badge",
			id:      "labels.one",
			badge:   " ",
			match:   map[string]any{"providers": []any{"deepseek"}, "modelId": "deepseek-flash"},
			wantErr: "badge must contain 1-12 printable characters",
		},
		{
			name:    "surrounding space in badge",
			id:      "labels.one",
			badge:   " 1017",
			match:   map[string]any{"providers": []any{"deepseek"}, "modelId": "deepseek-flash"},
			wantErr: "badge must contain 1-12 printable characters",
		},
		{
			name:    "badge too long",
			id:      "labels.one",
			badge:   "abcdefghij klm",
			match:   map[string]any{"providers": []any{"deepseek"}, "modelId": "deepseek-flash"},
			wantErr: "badge must contain 1-12 printable characters",
		},
		{
			name:    "no providers",
			id:      "labels.one",
			badge:   "1017",
			match:   map[string]any{"modelId": "deepseek-flash"},
			wantErr: "match needs at least one provider",
		},
		{
			name:    "provider with space",
			id:      "labels.one",
			badge:   "1017",
			match:   map[string]any{"providers": []any{"bad provider"}, "modelId": "deepseek-flash"},
			wantErr: "match provider is invalid",
		},
		{
			name:  "duplicated provider",
			id:    "labels.one",
			badge: "1017",
			match: map[string]any{
				"providers": []any{"magpie", "Magpie"}, "modelId": "deepseek-flash",
			},
			wantErr: "match provider is duplicated",
		},
		{
			name:    "model fields missing",
			id:      "labels.one",
			badge:   "1017",
			match:   map[string]any{"providers": []any{"magpie"}},
			wantErr: "match needs modelId or modelName",
		},
		{
			name:    "label id invalid",
			id:      "Labels One",
			badge:   "1017",
			match:   map[string]any{"providers": []any{"magpie"}, "modelId": "deepseek-flash"},
			wantErr: "id is invalid",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := modelLabelManifestMap("example.labels")
			manifest["contributes"].(map[string]any)["modelLabels"] = []any{
				modelLabelContribution(test.id, test.badge, test.match),
			}
			_, err := loadPluginManifest(writePluginManifest(t, t.TempDir(), "labels", manifest))
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, test.wantErr)
			}
		})
	}
}

func TestLoadPluginManifestRejectsDuplicatedModelLabelID(t *testing.T) {
	manifest := modelLabelManifestMap("example.labels")
	label := modelLabelContribution("deepseek-flash-1017", "1017", map[string]any{
		"providers": []any{"deepseek"}, "modelId": "deepseek-flash",
	})
	manifest["contributes"].(map[string]any)["modelLabels"] = []any{label, label}

	_, err := loadPluginManifest(writePluginManifest(t, t.TempDir(), "labels", manifest))
	if err == nil || !strings.Contains(err.Error(), "contribution id is duplicated") {
		t.Fatalf("error = %v, want duplicated contribution id", err)
	}
}

func TestPluginModelLabelMatches(t *testing.T) {
	match := pluginModelMatch{
		Providers: []string{"Magpie", "deepseek"},
		ModelID:   "deepseek-flash",
	}
	tests := []struct {
		name      string
		provider  string
		modelID   string
		modelName string
		want      bool
	}{
		{name: "provider and id match", provider: "magpie", modelID: "deepseek-flash", want: true},
		{name: "case insensitive provider", provider: " DEEPSEEK ", modelID: "deepseek-flash", want: true},
		{name: "provider missing", provider: "opencode-go", modelID: "deepseek-flash", want: false},
		{name: "model id differs", provider: "magpie", modelID: "deepseek-v4.1-flash", want: false},
		{name: "model name is wildcarded", provider: "magpie", modelID: "deepseek-flash",
			modelName: "DeepSeek V4.1 Flash · Magpie", want: true},
		{name: "channel prefix on the id", provider: "magpie", modelID: "opencode-go/deepseek-flash", want: true},
		{name: "prefixed id of another release", provider: "magpie",
			modelID: "opencode-go/deepseek-v4.1-flash", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := pluginModelLabelMatches(match, test.provider, test.modelID, test.modelName); got != test.want {
				t.Fatalf("matches(%q, %q) = %v, want %v", test.provider, test.modelID, got, test.want)
			}
		})
	}

	nameOnly := pluginModelMatch{Providers: []string{"local"}, ModelName: "Qwen3.8 Flash Next"}
	if !pluginModelLabelMatches(nameOnly, "local", "qwen-any", "qwen3.8 flash next") {
		t.Fatal("name-only rule should match ignoring case and wildcard the model id")
	}
}

func TestScanPluginRegistryModelLabelBadgeUsesFirstClaim(t *testing.T) {
	root := t.TempDir()

	first := modelLabelManifestMap("example.first-labels")
	first["contributes"].(map[string]any)["modelLabels"] = []any{
		modelLabelContribution("flash-1017", "1017", map[string]any{
			"providers": []any{"magpie", "deepseek"}, "modelId": "deepseek-flash",
		}),
	}
	writePluginManifest(t, root, "first", first)

	second := modelLabelManifestMap("example.second-labels")
	second["contributes"].(map[string]any)["modelLabels"] = []any{
		modelLabelContribution("flash-nightly", "NIGHTLY", map[string]any{
			"providers": []any{"magpie"}, "modelId": "deepseek-flash",
		}),
		modelLabelContribution("other-model", "R2", map[string]any{
			"providers": []any{"magpie"}, "modelId": "other-flash",
		}),
	}
	writePluginManifest(t, root, "second", second)

	registry := scanPluginRegistry([]pluginRootSpec{{Path: root}})
	if registry.registeredCount() != 2 || registry.contributionCount() != 3 {
		t.Fatalf("unexpected registry counts: registered=%d contributions=%d",
			registry.registeredCount(), registry.contributionCount())
	}
	if badge := registry.modelLabelBadge("magpie", "deepseek-flash", "DeepSeek V4.1 Flash"); badge != "1017" {
		t.Fatalf("badge = %q, want the first plugin's 1017", badge)
	}
	if badge := registry.modelLabelBadge("magpie", "other-flash", ""); badge != "R2" {
		t.Fatalf("badge = %q, want R2 for the unconflicted claim", badge)
	}
	if badge := registry.modelLabelBadge("magpie", "unclaimed-flash", ""); badge != "" {
		t.Fatalf("badge = %q, want no badge for an unclaimed model", badge)
	}
	conflicts := 0
	for _, issue := range registry.Issues {
		if strings.Contains(issue.Message, "overlaps example.first-labels contribution") {
			conflicts++
		}
	}
	if conflicts != 1 {
		t.Fatalf("conflict issues = %d, want 1: %#v", conflicts, registry.Issues)
	}
}

func TestShippedModelReleaseLabelsClaimLocalCatalog(t *testing.T) {
	manifest, err := loadPluginManifest("plugins/model-release-labels/plugin.json")
	if err != nil {
		t.Fatalf("load shipped manifest: %v", err)
	}
	if len(manifest.Contributes.ModelLabels) == 0 {
		t.Fatal("shipped manifest declares no model labels")
	}
	registry := pluginRegistry{Entries: []pluginRegistryEntry{{
		ID:       manifest.ID,
		Status:   pluginRegistered,
		Manifest: manifest,
	}}}
	// Ids as the local Pi catalog reports them on 2026-10-31.
	for _, modelID := range []string{
		"deepseek/deepseek-flash",
		"opencode-go/deepseek-flash",
		"workbuddy-ai/deepseek-flash",
		"auto/deepseek-v4.1-flash",
		"opencode-go/deepseek-v4.1-flash",
	} {
		if badge := registry.modelLabelBadge("magpie", modelID, "DeepSeek V4.1 Flash"); badge != "1017" {
			t.Fatalf("badge for %s = %q, want 1017", modelID, badge)
		}
	}
	if badge := registry.modelLabelBadge("magpie", "magpie/claude-opus-5-5", "Claude Opus 5.5"); badge != "" {
		t.Fatalf("badge for an unclaimed model = %q, want none", badge)
	}
}

func TestScanPluginRegistryIgnoresLabelsOfUnregisteredPlugins(t *testing.T) {
	root := t.TempDir()
	manifest := modelLabelManifestMap("example.future-api")
	manifest["compatibility"].(map[string]any)["pluginApi"] = "2"
	manifest["contributes"].(map[string]any)["modelLabels"] = []any{
		modelLabelContribution("flash-1017", "1017", map[string]any{
			"providers": []any{"magpie"}, "modelId": "deepseek-flash",
		}),
	}
	writePluginManifest(t, root, "future", manifest)

	registry := scanPluginRegistry([]pluginRootSpec{{Path: root}})
	if registry.registeredCount() != 0 {
		t.Fatalf("registered count = %d, want 0", registry.registeredCount())
	}
	if badge := registry.modelLabelBadge("magpie", "deepseek-flash", ""); badge != "" {
		t.Fatalf("badge = %q, want an incompatible plugin ignored", badge)
	}
}
