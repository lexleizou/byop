package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPluginManifestRegistersMetadataWithoutExecutingEntry(t *testing.T) {
	manifestPath := writePluginManifest(t, t.TempDir(), "example", validPluginManifestMap())

	manifest, err := loadPluginManifest(manifestPath)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if manifest.ID != "example.git-workspace" || manifest.Name != "Git Workspace" {
		t.Fatalf("unexpected manifest identity: %#v", manifest)
	}
	if manifest.contributionCount() != 6 {
		t.Fatalf("contribution count = %d, want 6", manifest.contributionCount())
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(manifestPath), manifest.Entry)); !os.IsNotExist(err) {
		t.Fatalf("fixture entry unexpectedly exists: %v", err)
	}
}

func TestLoadPluginManifestRejectsInvalidAndUnsafeDeclarations(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(map[string]any)
		wantErr string
	}{
		{
			name:    "unknown field",
			mutate:  func(manifest map[string]any) { manifest["unexpected"] = true },
			wantErr: "unknown field",
		},
		{
			name: "single character id",
			mutate: func(manifest map[string]any) {
				manifest["id"] = "a"
			},
			wantErr: "id must be 2-128",
		},
		{
			name: "empty name",
			mutate: func(manifest map[string]any) {
				manifest["name"] = " "
			},
			wantErr: "name must contain 1-80",
		},
		{
			name: "parent entry",
			mutate: func(manifest map[string]any) {
				manifest["entry"] = "../escape.js"
			},
			wantErr: "entry must be a safe relative path",
		},
		{
			name: "normalized parent segment",
			mutate: func(manifest map[string]any) {
				manifest["entry"] = "dist/../plugin.js"
			},
			wantErr: "entry must be a safe relative path",
		},
		{
			name: "absolute entry",
			mutate: func(manifest map[string]any) {
				manifest["entry"] = "/tmp/plugin.js"
			},
			wantErr: "entry must be a safe relative path",
		},
		{
			name: "backslash entry",
			mutate: func(manifest map[string]any) {
				manifest["entry"] = `dist\plugin.js`
			},
			wantErr: "entry must be a safe relative path",
		},
		{
			name: "duplicate contribution within plugin",
			mutate: func(manifest map[string]any) {
				contributes := manifest["contributes"].(map[string]any)
				contributes["commands"] = []any{
					map[string]any{"id": "git.refresh", "title": "Refresh"},
					map[string]any{"id": "git.refresh", "title": "Refresh again"},
				}
			},
			wantErr: "contribution id is duplicated: git.refresh",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := validPluginManifestMap()
			test.mutate(manifest)
			manifestPath := writePluginManifest(t, t.TempDir(), "example", manifest)
			_, err := loadPluginManifest(manifestPath)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("load error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestLoadPluginManifestRejectsMalformedAndMultipleJSONValues(t *testing.T) {
	valid := marshalPluginManifest(t, validPluginManifestMap())
	tests := []struct {
		name    string
		content string
	}{
		{name: "malformed", content: `{"schemaVersion":`},
		{name: "multiple values", content: valid + "\n{}"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			manifestPath := filepath.Join(directory, pluginManifestFilename)
			if err := os.WriteFile(manifestPath, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadPluginManifest(manifestPath); err == nil {
				t.Fatal("invalid manifest was accepted")
			}
		})
	}
}

func TestScanPluginRegistryRegistersCompatibleManifest(t *testing.T) {
	root := t.TempDir()
	writePluginManifest(t, root, "example", validPluginManifestMap())

	registry := scanPluginRegistry([]pluginRootSpec{{Path: root}})
	if registry.registeredCount() != 1 || registry.contributionCount() != 6 || registry.problemCount() != 0 {
		t.Fatalf("unexpected registry counts: %#v", registry)
	}
	entry := registry.Entries[0]
	if entry.Status != pluginRegistered || entry.ID != "example.git-workspace" ||
		entry.Manifest == nil || entry.Error != "" {
		t.Fatalf("unexpected registry entry: %#v", entry)
	}
}

func TestScanPluginRegistryMarksIncompatiblePlugins(t *testing.T) {
	root := t.TempDir()

	apiManifest := validPluginManifestMap()
	apiManifest["id"] = "example.future-api"
	apiManifest["compatibility"].(map[string]any)["pluginApi"] = "2"
	writePluginManifest(t, root, "api", apiManifest)

	hostManifest := validPluginManifestMap()
	hostManifest["id"] = "example.future-host"
	hostManifest["compatibility"].(map[string]any)["host"] = ">=1.0.0 <2.0.0"
	writePluginManifest(t, root, "host", hostManifest)

	registry := scanPluginRegistry([]pluginRootSpec{{Path: root}})
	if registry.registeredCount() != 0 || registry.problemCount() != 2 {
		t.Fatalf("unexpected registry counts: %#v", registry)
	}
	for _, entry := range registry.Entries {
		if entry.Status != pluginIncompatible {
			t.Fatalf("entry %s status = %s, want %s", entry.ID, entry.Status, pluginIncompatible)
		}
	}
}

func TestScanPluginRegistryMarksDuplicatePluginIDs(t *testing.T) {
	root := t.TempDir()
	writePluginManifest(t, root, "first", validPluginManifestMap())
	writePluginManifest(t, root, "second", validPluginManifestMap())

	registry := scanPluginRegistry([]pluginRootSpec{{Path: root}})
	if len(registry.Entries) != 2 || registry.registeredCount() != 1 || registry.problemCount() != 1 {
		t.Fatalf("unexpected registry counts: %#v", registry)
	}
	if registry.Entries[0].Status != pluginRegistered || registry.Entries[1].Status != pluginDuplicateID {
		t.Fatalf("unexpected duplicate states: %#v", registry.Entries)
	}
}

func TestScanPluginRegistryMarksCrossPluginContributionConflicts(t *testing.T) {
	root := t.TempDir()
	first := validPluginManifestMap()
	first["id"] = "example.first"
	second := validPluginManifestMap()
	second["id"] = "example.second"
	writePluginManifest(t, root, "first", first)
	writePluginManifest(t, root, "second", second)

	registry := scanPluginRegistry([]pluginRootSpec{{Path: root}})
	if len(registry.Entries) != 2 || registry.registeredCount() != 1 || registry.problemCount() != 1 {
		t.Fatalf("unexpected registry counts: %#v", registry)
	}
	if registry.Entries[0].Status != pluginRegistered ||
		registry.Entries[1].Status != pluginDuplicateContribution ||
		!strings.Contains(registry.Entries[1].Error, "already registered by example.first") {
		t.Fatalf("unexpected contribution conflict: %#v", registry.Entries)
	}
}

func TestScanPluginRegistryReportsMalformedAndIgnoresMissingManifests(t *testing.T) {
	root := t.TempDir()
	invalidDirectory := filepath.Join(root, "invalid")
	if err := os.MkdirAll(invalidDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(invalidDirectory, pluginManifestFilename), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "missing"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "loose-file.txt"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}

	registry := scanPluginRegistry([]pluginRootSpec{{Path: root}})
	if len(registry.Entries) != 1 || registry.Entries[0].Status != pluginInvalidManifest {
		t.Fatalf("unexpected malformed manifest result: %#v", registry)
	}
	if registry.Entries[0].ID != "invalid" || registry.Entries[0].Error == "" {
		t.Fatalf("invalid manifest lost its directory identity or error: %#v", registry.Entries[0])
	}
}

func TestScanPluginRegistryHandlesMissingRootsByExplicitness(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")

	implicitRegistry := scanPluginRegistry([]pluginRootSpec{{Path: missing}})
	if len(implicitRegistry.Issues) != 0 || implicitRegistry.problemCount() != 0 {
		t.Fatalf("implicit missing root reported an issue: %#v", implicitRegistry)
	}

	explicitRegistry := scanPluginRegistry([]pluginRootSpec{{Path: missing, Explicit: true}})
	if len(explicitRegistry.Issues) != 1 || explicitRegistry.problemCount() != 1 {
		t.Fatalf("explicit missing root did not report an issue: %#v", explicitRegistry)
	}
}

func TestPluginRegistrySearchAndCounts(t *testing.T) {
	manifest := validPluginManifestMap()
	state := &appState{
		pluginRegistry: pluginRegistry{Entries: []pluginRegistryEntry{
			{
				ID:                "example.git-workspace",
				Name:              "Git Workspace",
				Version:           "0.1.0",
				Description:       "Repository status and review.",
				Manifest:          manifestFromValue(t, manifest),
				Status:            pluginRegistered,
				ContributionCount: 6,
			},
			{
				ID:     "broken.plugin",
				Name:   "Broken Plugin",
				Status: pluginInvalidManifest,
				Error:  "parse manifest",
			},
		}},
		pluginSearch: "review",
	}

	entries := state.filteredPluginEntries()
	if len(entries) != 1 || entries[0].ID != "example.git-workspace" {
		t.Fatalf("search result = %#v", entries)
	}
	if state.pluginRegistry.registeredCount() != 1 ||
		state.pluginRegistry.contributionCount() != 6 ||
		state.pluginRegistry.problemCount() != 1 {
		t.Fatalf("unexpected counts: %#v", state.pluginRegistry)
	}
}

func validPluginManifestMap() map[string]any {
	return map[string]any{
		"schemaVersion": 1,
		"id":            "example.git-workspace",
		"name":          "Git Workspace",
		"version":       "0.1.0",
		"description":   "Repository status and review.",
		"runtime":       "node",
		"entry":         "dist/plugin.js",
		"icon":          "git",
		"compatibility": map[string]any{
			"host":      ">=0.4.0 <1.0.0",
			"pluginApi": "1",
		},
		"contributes": map[string]any{
			"navigation": []any{
				map[string]any{"id": "git", "title": "Git", "icon": "git"},
			},
			"commands": []any{
				map[string]any{"id": "git.refresh", "title": "Refresh repository"},
			},
			"views": []any{
				map[string]any{"id": "git.status", "title": "Git status", "location": "main", "view": "native"},
			},
			"taskPanels": []any{
				map[string]any{"id": "git.tasks", "title": "Git tasks"},
			},
			"settings": []any{
				map[string]any{"id": "git.settings", "title": "Git"},
			},
			"webviews": []any{
				map[string]any{"id": "git.diff", "title": "Git diff", "location": "main"},
			},
		},
		"activation": map[string]any{
			"events": []any{"onCommand:git.refresh"},
			"lazy":   true,
		},
		"capabilities": map[string]any{
			"workspace": "read",
			"files":     "read",
			"network":   false,
			"process":   "none",
		},
	}
}

func writePluginManifest(t *testing.T, root, directory string, manifest map[string]any) string {
	t.Helper()
	pluginDirectory := filepath.Join(root, directory)
	if err := os.MkdirAll(pluginDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(pluginDirectory, pluginManifestFilename)
	if err := os.WriteFile(filename, []byte(marshalPluginManifest(t, manifest)), 0o600); err != nil {
		t.Fatal(err)
	}
	return filename
}

func marshalPluginManifest(t *testing.T, manifest map[string]any) string {
	t.Helper()
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func manifestFromValue(t *testing.T, value map[string]any) *pluginManifest {
	t.Helper()
	var manifest pluginManifest
	if err := json.Unmarshal([]byte(marshalPluginManifest(t, value)), &manifest); err != nil {
		t.Fatal(err)
	}
	return &manifest
}
