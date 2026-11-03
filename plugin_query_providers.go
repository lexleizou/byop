package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// Shared shape for the two query contribution slots, mentionProviders and
// paletteProviders. Both ask a declared command for items and both are consumed
// through the same leased invoke path, so they share one item type and one
// decoder; each consumer uses the fields its own slot needs.

// pluginQueryItem is one result a mention or palette provider returned. Text is
// what a chosen mention inserts; Command and Args are what a chosen palette row
// runs, so the same shape serves both query slots and the host only ever uses
// the fields its own slot needs.
type pluginQueryItem struct {
	ID      string         `json:"id"`
	Label   string         `json:"label"`
	Text    string         `json:"text,omitempty"`
	Command string         `json:"command,omitempty"`
	Args    map[string]any `json:"args,omitempty"`
}

// pluginQueryContributionByID resolves a declared provider by id, so a reply can
// check that its provider is still declared by a registered plugin.
func pluginQueryContributionByID(manifest *pluginManifest, id string) *pluginQueryContribution {
	if manifest == nil {
		return nil
	}
	return pluginQueryContributionIn(manifest.Contributes.MentionProviders, id)
}

// pluginPaletteContributionByID resolves a declared palette provider, which a
// palette reply checks the same way a mention reply checks its provider.
func pluginPaletteContributionByID(manifest *pluginManifest, id string) *pluginQueryContribution {
	if manifest == nil {
		return nil
	}
	return pluginQueryContributionIn(manifest.Contributes.PaletteProviders, id)
}

func pluginQueryContributionIn(contributions []pluginQueryContribution, id string) *pluginQueryContribution {
	for index := range contributions {
		if contributions[index].ID == id {
			return &contributions[index]
		}
	}
	return nil
}

// pluginEntryByID resolves one plugin from the current registry, which is how a
// merged reply or a chosen palette row re-checks that the plugin it came from is
// still registered. The registry is read without localizing so the check stays as
// light as the other provider checks.
func (s *appState) pluginEntryByID(pluginID string) (pluginRegistryEntry, bool) {
	for _, entry := range s.pluginRegistrySnapshot().Entries {
		if entry.ID == pluginID && entry.Status == pluginRegistered {
			return entry, true
		}
	}
	return pluginRegistryEntry{}, false
}

// decodePluginQueryItems reads the {items:[...]} envelope a query command
// returns, capping the list and dropping entries with no usable label.
func decodePluginQueryItems(raw []byte) ([]pluginQueryItem, error) {
	var envelope struct {
		Items []pluginQueryItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	items := make([]pluginQueryItem, 0, maxPluginQueryItems)
	for _, item := range envelope.Items {
		item.ID = strings.TrimSpace(item.ID)
		item.Label = truncateRunes(strings.TrimSpace(item.Label), 120)
		item.Text = truncateRunes(item.Text, maxPluginViewRunes)
		if item.Label == "" {
			continue
		}
		if item.ID == "" {
			item.ID = filepath.Base(item.Label)
		}
		items = append(items, item)
		if len(items) >= maxPluginQueryItems {
			break
		}
	}
	return items, nil
}

// pluginWorkspaceRoot is the workspace the host started plugin workers with,
// which is what a query command is told about. It is read-only here: a provider
// that wants file contents asks the host through its declared capability.
func (h *pluginHost) pluginWorkspaceRoot() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.workspaceRoot
}
