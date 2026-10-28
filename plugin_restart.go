package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

type pluginRestartView struct {
	open   bool
	viewID string
	kind   pluginViewKind
}

func (s *appState) restartPlugin(entry pluginRegistryEntry) {
	entry = pluginLocalizedEntry(entry)
	if s == nil || s.plugins == nil || entry.Status != pluginRegistered ||
		entry.Manifest == nil || strings.TrimSpace(entry.SourceDirectory) == "" {
		return
	}
	if s.pluginRestarting == nil {
		s.pluginRestarting = make(map[string]bool)
	}
	if s.pluginRestarting[entry.ID] {
		return
	}
	view := pluginRestartView{}
	if s.page == pagePluginView && s.pluginViewPluginID == entry.ID {
		view = pluginRestartView{
			open:   true,
			viewID: s.pluginViewID,
			kind:   s.pluginViewKind,
		}
	}
	s.pluginRestarting[entry.ID] = true
	s.plugins.beginPluginRestart(entry.ID)
	s.destroyPluginWebViews(entry.ID)
	name := fallback(entry.Name, entry.ID)
	s.showNotice(textArgs("pluginsRestarting", map[string]any{"name": name}))

	go func() {
		if err := s.plugins.restartPluginProcess(entry.ID); err != nil {
			s.postUI(func() {
				s.finishPluginRestartFailure(entry.ID, name, err)
			})
			return
		}
		refreshed := scanPluginRegistry(defaultPluginRoots())
		replacement, ok := pluginEntryBySourceDirectory(refreshed, entry.SourceDirectory)
		if !ok {
			s.postUI(func() {
				s.finishPluginRestartFailure(entry.ID, name,
					errors.New("plugin folder is no longer in the configured roots"))
			})
			return
		}
		if replacement.ID != entry.ID {
			s.postUI(func() {
				s.finishPluginRestartFailure(entry.ID, name, fmt.Errorf(
					"plugin ID changed from %s to %s; run a full plugin scan",
					entry.ID, replacement.ID,
				))
			})
			return
		}
		s.postUI(func() {
			s.finishPluginRestart(entry, replacement, view)
		})
	}()
}

func pluginEntryBySourceDirectory(
	registry pluginRegistry,
	sourceDirectory string,
) (pluginRegistryEntry, bool) {
	cleaned := filepath.Clean(sourceDirectory)
	for _, entry := range registry.Entries {
		if filepath.Clean(entry.SourceDirectory) == cleaned {
			return entry, true
		}
	}
	return pluginRegistryEntry{}, false
}

func replacePluginRegistryEntry(
	registry pluginRegistry,
	sourceDirectory string,
	replacement pluginRegistryEntry,
) (pluginRegistry, error) {
	cleaned := filepath.Clean(sourceDirectory)
	registry.Entries = append([]pluginRegistryEntry(nil), registry.Entries...)
	for index := range registry.Entries {
		if filepath.Clean(registry.Entries[index].SourceDirectory) != cleaned {
			continue
		}
		registry.Entries[index] = replacement
		return registry, nil
	}
	return registry, fmt.Errorf("plugin folder %s is not registered", sourceDirectory)
}

func (s *appState) finishPluginRestart(
	previous pluginRegistryEntry,
	replacement pluginRegistryEntry,
	view pluginRestartView,
) {
	name := fallback(pluginLocalizedEntry(replacement).Name, previous.ID)
	if s.pluginRestarting == nil {
		s.pluginRestarting = make(map[string]bool)
	}
	defer func() {
		s.pluginRestarting[previous.ID] = false
		s.plugins.endPluginRestart(previous.ID)
	}()

	next, err := replacePluginRegistryEntry(
		s.pluginRegistrySnapshot(),
		previous.SourceDirectory,
		replacement,
	)
	if err != nil {
		s.showNotice(textArgs("pluginsRestartFailed", map[string]any{
			"name":  name,
			"error": uiErrorSummary(err, text("errorUnknown"), 200),
		}))
		return
	}

	s.destroyPluginWebViews(previous.ID)
	s.setPluginRegistry(next)
	s.plugins.endPluginRestart(previous.ID)
	s.plugins.invalidatePluginSettings(previous.ID)
	for key := range s.pluginTasks {
		if pluginID, _, _ := strings.Cut(key, "\x00"); pluginID == previous.ID {
			delete(s.pluginTasks, key)
		}
	}
	for key := range s.pluginViewModels {
		if pluginID, _, _ := strings.Cut(key, "\x00"); pluginID == previous.ID {
			delete(s.pluginViewModels, key)
		}
	}
	s.syncPluginSettingForms()
	if s.automations != nil {
		s.automations.refreshSessionLoopKeys(registeredSessionLoopKeys(next))
	}

	if previousSignature, currentSignature :=
		registeredPluginPiEntrySignature(previous),
		registeredPluginPiEntrySignature(replacement); previousSignature != currentSignature {
		s.pluginPiRefreshRequired = true
	}

	if view.open {
		s.reopenPluginAfterRestart(replacement, view)
	}

	if replacement.Status != pluginRegistered || replacement.Manifest == nil {
		s.showNotice(textArgs("pluginsRestartFailed", map[string]any{
			"name":  name,
			"error": fallback(replacement.Error, text("pluginsFailedFallback")),
		}))
		return
	}
	if _, err := writePluginRuntimeGeneration(s.plugins.configDir, previous.ID); err != nil {
		s.showNotice(textArgs("pluginsRestartFailed", map[string]any{
			"name":  name,
			"error": uiErrorSummary(err, text("errorUnknown"), 200),
		}))
		return
	}
	s.showNotice(textArgs("pluginsRestarted", map[string]any{"name": name}))
}

func (s *appState) finishPluginRestartFailure(pluginID, name string, err error) {
	if s.pluginRestarting != nil {
		s.pluginRestarting[pluginID] = false
	}
	if s.plugins != nil {
		s.plugins.endPluginRestart(pluginID)
	}
	s.showNotice(textArgs("pluginsRestartFailed", map[string]any{
		"name":  name,
		"error": uiErrorSummary(err, text("errorUnknown"), 200),
	}))
}

func (s *appState) reopenPluginAfterRestart(
	entry pluginRegistryEntry,
	view pluginRestartView,
) {
	s.page = pagePlugins
	s.pluginViewPluginID = ""
	s.pluginViewID = ""
	s.pluginViewKind = ""
	s.pluginViewLoading = false
	s.pluginViewError = ""
	if !view.open || entry.Status != pluginRegistered || entry.Manifest == nil {
		return
	}
	switch view.kind {
	case pluginViewNative:
		contribution, ok := pluginViewByID(entry.Manifest, view.viewID)
		if ok {
			s.openPluginView(entry, contribution)
		}
	case pluginViewWebView:
		contribution, ok := pluginWebViewContributionByID(entry.Manifest, view.viewID)
		if ok && contribution.Location == "main" {
			s.openPluginWebView(entry, contribution)
		}
	}
}
