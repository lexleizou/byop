package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"byop/locales"
)

func (s *appState) pluginsPage(c *ui.Context, p palette) {
	registry := s.pluginRegistrySnapshot()
	entries := s.filteredPluginEntries()
	ui.Scroll(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Justify(ui.Center).Padding(30, 28, 38, 28).Children(func() {
			ui.Column(c).Grow(1).MaxWidth(880).Gap(22).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.End).Gap(16).Children(func() {
					ui.Column(c).Grow(1).Children(func() {
						pageHeader(c, p, text("pluginsWorkbench"), text("pluginsTitle"),
							text("pluginsDescription"))
					})
					primaryAction(c, p, "plugins-scan", text("pluginsScan"), "package", func() {
						s.scanPlugins(true)
					})
				})

				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Column(c).Width(300).MinWidth(180).Children(func() {
						searchField(c, p, "plugin-search", &s.pluginSearch, text("pluginsSearch"))
					})
					tone := p.success
					if registry.problemCount() > 0 {
						tone = p.warning
					}
					statusPill(c, p, textArgs("pluginsRegistrySummary", map[string]any{
						"registered":    registry.registeredCount(),
						"contributions": registry.contributionCount(),
					}), tone)
					ui.Spacer(c)
					secondaryAction(c, p, "plugin-dir", text("pluginsFolders"), func() {
						s.showNotice(textArgs("pluginsRoots", map[string]any{
							"roots": strings.Join(registry.Roots, " · "),
						}))
					})
				})

				if len(registry.Issues) > 0 {
					s.pluginRegistryIssues(c, p, registry.Issues)
				}

				sectionLabel(c, p, text("pluginsDiscovered"))
				if len(entries) == 0 {
					s.emptyPluginRegistry(c, p)
				} else {
					ui.Row(c).Wrap().AlignItems(ui.Start).GapX(12).GapY(12).Children(func() {
						for _, entry := range entries {
							s.pluginCard(c, p, entry)
						}
					})
				}

				sectionLabel(c, p, text("pluginsContractPreview"))
				ui.Column(c).FillWidth().Gap(10).Padding(14).Radius(11).
					Border(1, p.border).Background(p.surface).Children(func() {
					ui.Text(c, textArgs("pluginsContractDescription", map[string]any{
						"host":   registry.HostVersion,
						"schema": pluginManifestSchemaVersion,
						"api":    registry.PluginAPI,
					})).
						FontSize(fontBody).LineHeight(1.5).TextColor(p.text)
					ui.Row(c).Wrap().Gap(7).Children(func() {
						contractChip(c, p, text("pluginsContractCommands"))
						contractChip(c, p, text("pluginsContractViews"))
						contractChip(c, p, text("pluginsContractWebViews"))
						contractChip(c, p, text("pluginsContractTasks"))
						contractChip(c, p, text("pluginsContractSettings"))
						contractChip(c, p, text("pluginsContractHostCalls"))
						contractChip(c, p, text("pluginsContractNodePermissions"))
						contractChip(c, p, text("pluginsContractSchemeCSP"))
					})
				})
			})
		})
	})
}

func (s *appState) filteredPluginEntries() []pluginRegistryEntry {
	query := strings.ToLower(strings.TrimSpace(s.pluginSearch))
	registry := s.pluginRegistrySnapshot()
	for index := range registry.Entries {
		registry.Entries[index] = pluginLocalizedEntry(registry.Entries[index])
	}
	if query == "" {
		return registry.Entries
	}
	entries := make([]pluginRegistryEntry, 0, len(registry.Entries))
	for _, entry := range registry.Entries {
		text := strings.ToLower(strings.Join([]string{
			entry.ID,
			entry.Name,
			entry.Version,
			entry.Description,
			string(entry.Status),
		}, " "))
		if entry.Manifest != nil {
			text += " " + strings.ToLower(pluginContributionSearchText(entry.Manifest))
		}
		if strings.Contains(text, query) {
			entries = append(entries, entry)
		}
	}
	return entries
}

func pluginContributionSearchText(manifest *pluginManifest) string {
	var parts []string
	for _, contribution := range manifest.Contributes.Navigation {
		parts = append(parts, contribution.ID, contribution.Title)
	}
	for _, contribution := range manifest.Contributes.Commands {
		parts = append(parts, contribution.ID, contribution.Title, contribution.Description)
	}
	for _, contribution := range manifest.Contributes.Views {
		parts = append(parts, contribution.ID, contribution.Title)
	}
	for _, contribution := range manifest.Contributes.TaskPanels {
		parts = append(parts, contribution.ID, contribution.Title)
	}
	for _, contribution := range manifest.Contributes.Settings {
		parts = append(parts, contribution.ID, contribution.Title)
	}
	for _, contribution := range manifest.Contributes.WebViews {
		parts = append(parts, contribution.ID, contribution.Title)
	}
	for _, contribution := range manifest.Contributes.Automations {
		parts = append(parts, contribution.ID, contribution.Title, contribution.Description)
	}
	return strings.Join(parts, " ")
}

func (s *appState) pluginRegistryIssues(c *ui.Context, p palette, issues []pluginRegistryIssue) {
	ui.Column(c).FillWidth().Gap(8).Padding(14).Radius(11).
		Border(1, p.border).Background(p.surface).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon("x")).Size(14, 14).TextColor(p.danger)
			ui.Text(c, text("pluginsRegistryScanIssues")).FontSize(fontBody).FontWeight(650).TextColor(p.text)
		})
		for _, issue := range issues {
			ui.Text(c, issue.Path+" · "+issue.Message).Font("monospace").FontSize(fontCaption).
				TextColor(p.danger).Selectable()
		}
	})
}

func (s *appState) emptyPluginRegistry(c *ui.Context, p palette) {
	message := text("pluginsManifestsNotFound")
	if strings.TrimSpace(s.pluginSearch) != "" {
		message = textArgs("pluginsNoMatch", map[string]any{"query": s.pluginSearch})
	}
	ui.Column(c).FillWidth().Gap(10).Padding(18).Radius(11).
		Border(1, p.border).Background(p.surface).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Size(34, 34).Radius(9).Background(p.surfaceHover).Center().Children(func() {
				ui.Icon(c, icon("package")).Size(17, 17).TextColor(p.muted)
			})
			ui.Column(c).Gap(2).Grow(1).Children(func() {
				ui.Text(c, text("pluginsRegistryEmpty")).FontSize(fontBody).FontWeight(650).TextColor(p.text)
				ui.Text(c, message).FontSize(fontSmall).TextColor(p.muted)
			})
		})
	})
}

func (s *appState) pluginCard(c *ui.Context, p palette, entry pluginRegistryEntry) {
	tone := s.pluginStatusTone(p, entry.Status)
	title := fallback(entry.Name, entry.ID)
	description := fallback(entry.Description, text("pluginsNoDescription"))
	surface := text("pluginsManifestRejected")
	iconName := "plugins"
	if entry.Manifest != nil {
		surface = pluginContributionSummary(entry.Manifest)
		if entry.Manifest.Icon != "" {
			iconName = entry.Manifest.Icon
		}
	}
	runtime := s.plugins.status(entry.ID)
	pillLabel := pluginStatusLabel(entry.Status)
	pillTone := tone
	if entry.Status == pluginRegistered && runtime.State != pluginStateInactive {
		pillLabel = pluginRuntimeLabel(runtime.State)
		pillTone = pluginRuntimeTone(p, runtime.State)
	}
	ui.Column(c).Width(260).MinWidth(220).AlignItems(ui.Start).Gap(10).Padding(14).Radius(11).
		Border(1, p.border).Background(p.surface).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Row(c).Size(34, 34).Radius(9).Background(p.surfaceHover).Center().Children(func() {
				ui.Icon(c, iconOr(iconName, "plugins")).Size(17, 17).TextColor(tone)
			})
			ui.Spacer(c)
			statusPill(c, p, pillLabel, pillTone)
		})
		ui.Text(c, title).FontSize(fontBody).FontWeight(650).TextColor(p.text)
		ui.Text(c, description).FontSize(fontSmall).LineHeight(1.45).TextColor(p.muted)
		ui.Text(c, entry.ID).Font("monospace").FontSize(fontCaption).TextColor(p.muted)
		ui.Text(c, surface).FontSize(fontCaption).TextColor(tone)
		meta := textArgs("pluginsContributionCount", map[string]any{
			"count": entry.ContributionCount,
		})
		if entry.Version != "" {
			meta = "v" + entry.Version + " · " + meta
		}
		if entry.Manifest != nil {
			meta = textArgs("pluginsRuntimeMeta", map[string]any{
				"runtime": entry.Manifest.Runtime,
				"count":   entry.ContributionCount,
			})
			if entry.Version != "" {
				meta = "v" + entry.Version + " · " + meta
			}
		}
		ui.Text(c, meta).FontSize(fontCaption).TextColor(p.muted)
		if entry.Manifest != nil {
			ui.Text(c, pluginPermissionsLine(entry.Manifest)).FontSize(fontCaption).
				LineHeight(1.35).TextColor(p.muted)
			ui.Text(c, pluginEnforcementLine(entry.Manifest.Runtime)).FontSize(fontCaption).
				LineHeight(1.35).TextColor(p.muted)
			for _, webView := range entry.Manifest.Contributes.WebViews {
				ui.Text(c, textArgs("pluginsWebViewMeta", map[string]any{
					"title": fallback(webView.Title, webView.ID),
					"entry": webView.Entry,
				})).
					FontSize(fontCaption).LineHeight(1.35).TextColor(p.muted)
			}
			var secondaryViews []string
			for _, view := range entry.Manifest.Contributes.Views {
				if view.Location == "secondary" {
					secondaryViews = append(secondaryViews, fallback(view.Title, view.ID))
				}
			}
			if len(secondaryViews) > 0 {
				ui.Text(c, textArgs("pluginsSecondaryViews", map[string]any{
					"views": strings.Join(secondaryViews, ", "),
				})).
					FontSize(fontCaption).LineHeight(1.35).TextColor(p.muted)
			}
		}
		if entry.Error != "" {
			ui.Text(c, summarizeLine(uiErrorFromText(entry.Error, text("pluginsFailedFallback")), 240)).FontSize(fontCaption).
				LineHeight(1.4).TextColor(p.danger)
		}
		if entry.Status != pluginRegistered || entry.Manifest == nil {
			return
		}
		if runtime.LastResult != "" && runtime.LastError == "" {
			ui.Text(c, runtime.LastCommand+" → "+runtime.LastResult).FontSize(fontCaption).
				LineHeight(1.4).TextColor(p.success)
		}
		if runtime.LastError != "" {
			ui.Text(c, summarizeLine(uiErrorFromText(runtime.LastError, text("pluginsFailedFallback")), 240)).FontSize(fontCaption).
				LineHeight(1.4).TextColor(p.danger)
		}
		if runtime.HostCalls > 0 {
			ui.Text(c, textArgs("pluginsHostCallCount", map[string]any{
				"count": runtime.HostCalls,
			})).FontSize(fontCaption).
				TextColor(p.muted)
		}
		if runtime.DeniedCalls > 0 {
			ui.Text(c, textArgs("pluginsDenied", map[string]any{
				"detail": summarizeLine(runtime.LastDenied, 180),
			})).
				FontSize(fontCaption).LineHeight(1.35).TextColor(p.danger)
		}
		busy := runtime.State == pluginStateStarting || runtime.State == pluginStateRunning ||
			runtime.State == pluginStateStopping
		ui.Row(c).Wrap().GapX(6).GapY(6).Children(func() {
			for _, command := range entry.Manifest.Contributes.Commands {
				command := command
				pluginCommandButton(c, p, "plugin-cmd-"+entry.ID+"-"+command.ID, command, busy, func() {
					s.runPluginCommand(entry, command)
				})
			}
			if runtime.State == pluginStateActive {
				secondaryAction(c, p, "plugin-stop-"+entry.ID, text("pluginsDeactivate"), func() {
					s.deactivatePlugin(entry)
				})
			}
			for _, view := range entry.Manifest.Contributes.Views {
				view := view
				if view.Location != "main" {
					continue
				}
				secondaryAction(c, p, "plugin-open-view-"+entry.ID+"-"+view.ID,
					textArgs("pluginsOpen", map[string]any{"title": fallback(view.Title, view.ID)}), func() {
						s.openPluginView(entry, view)
					})
			}
			for _, webView := range entry.Manifest.Contributes.WebViews {
				webView := webView
				if webView.Location != "main" {
					continue
				}
				secondaryAction(c, p, "plugin-open-webview-"+entry.ID+"-"+webView.ID,
					textArgs("pluginsOpen", map[string]any{"title": fallback(webView.Title, webView.ID)}), func() {
						s.openPluginWebView(entry, webView)
					})
			}
		})
	})
}

func pluginPermissionsLine(manifest *pluginManifest) string {
	workspace := fallback(manifest.Capabilities.Workspace, text("pluginsValueNone"))
	files := fallback(manifest.Capabilities.Files, text("pluginsValueNone"))
	network := text("pluginsValueOff")
	if manifest.Capabilities.Network {
		network = text("pluginsValueOn")
	}
	process := fallback(manifest.Capabilities.Process, text("pluginsValueNone"))
	return textArgs("pluginsPermissions", map[string]any{
		"workspace": workspace,
		"files":     files,
		"network":   network,
		"process":   process,
	})
}

func pluginEnforcementLine(runtime string) string {
	if runtime == "node" {
		return text("pluginsEnforcementNode")
	}
	return text("pluginsEnforcementHost")
}

func pluginCommandButton(c *ui.Context, p palette, key string, command pluginCommandContribution, busy bool, click func()) {
	label := fallback(command.Title, command.ID)
	btn := ui.ButtonBase(c.Key(key)).Padding(6, 10).Radius(7).Border(1, p.border).
		Background(p.background).Cursor(ui.CursorPointer).Label(label).
		Tooltip(fallback(command.Description, command.ID))
	if btn.Hovered() && !busy {
		btn.Background(p.surfaceHover)
	}
	btn.Children(func() {
		ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon("play")).Size(11, 11).TextColor(p.accent)
			ui.Text(c, label).FontSize(fontSmall).FontWeight(600).TextColor(p.text)
		})
	})
	if btn.Clicked() && !busy {
		click()
	}
}

func (s *appState) runPluginCommand(entry pluginRegistryEntry, command pluginCommandContribution) {
	s.runPluginCommandWithArgs(entry, command, map[string]any{})
}

func (s *appState) runPluginCommandWithArgs(
	entry pluginRegistryEntry,
	command pluginCommandContribution,
	args map[string]any,
) {
	title := fallback(command.Title, command.ID)
	s.showNotice(textArgs("pluginsRunning", map[string]any{"title": title}))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), pluginLifecycleTimeout+pluginCommandTimeout)
		defer cancel()
		result, err := s.plugins.invokeCommand(ctx, entry, command.ID, args)
		s.postUI(func() {
			if err != nil {
				s.showNotice(textArgs("pluginsCommandFailed", map[string]any{
					"title": title,
					"error": uiErrorSummary(err, text("errorUnknown"), 200),
				}))
				return
			}
			s.showNotice(title + " · " + pluginResultSummary(result))
			if s.page == pagePluginView && s.pluginViewPluginID == entry.ID &&
				s.pluginViewID != "" && s.pluginViewKind != "" {
				s.refreshPluginView(entry.ID, s.pluginViewID)
			}
		})
	}()
}

func (s *appState) deactivatePlugin(entry pluginRegistryEntry) {
	name := fallback(entry.Name, entry.ID)
	s.destroyPluginWebViews(entry.ID)
	go func() {
		err := s.plugins.deactivate(entry.ID)
		s.postUI(func() {
			if err != nil {
				s.showNotice(textArgs("pluginsStoppedWithError", map[string]any{
					"name":  name,
					"error": uiErrorSummary(err, text("errorUnknown"), 200),
				}))
				return
			}
			s.showNotice(textArgs("pluginsDeactivated", map[string]any{"name": name}))
		})
	}()
}

func pluginRuntimeLabel(state pluginRuntimeState) string {
	switch state {
	case pluginStateInactive:
		return text("pluginsStatusInactive")
	case pluginStateStarting:
		return text("pluginsStatusStarting")
	case pluginStateActive:
		return text("pluginsStatusActive")
	case pluginStateRunning:
		return text("pluginsStatusRunning")
	case pluginStateStopping:
		return text("pluginsStatusStopping")
	case pluginStateFailed:
		return text("pluginsStatusFailed")
	default:
		return text("pluginsStatusUnknown")
	}
}

func pluginRuntimeTone(p palette, state pluginRuntimeState) ui.Color {
	switch state {
	case pluginStateActive:
		return p.success
	case pluginStateStarting, pluginStateRunning, pluginStateStopping:
		return p.accent
	case pluginStateFailed:
		return p.danger
	default:
		return p.muted
	}
}

func pluginStatusLabel(status pluginRegistryStatus) string {
	switch status {
	case pluginRegistered:
		return text("pluginsStatusRegistered")
	case pluginInvalidManifest:
		return text("pluginsStatusInvalid")
	case pluginIncompatible:
		return text("pluginsStatusIncompatible")
	case pluginDuplicateID:
		return text("pluginsStatusDuplicateID")
	case pluginDuplicateContribution:
		return text("pluginsStatusConflict")
	default:
		return text("pluginsStatusUnknown")
	}
}

func (s *appState) pluginStatusTone(p palette, status pluginRegistryStatus) ui.Color {
	switch status {
	case pluginRegistered:
		return p.success
	case pluginIncompatible:
		return p.warning
	default:
		return p.danger
	}
}

func pluginContributionSummary(manifest *pluginManifest) string {
	var parts []string
	appendPart := func(count int, key string) {
		if count == 0 {
			return
		}
		parts = append(parts, textArgs(key, map[string]any{"count": count}))
	}
	appendPart(len(manifest.Contributes.Navigation), "pluginsSummaryNavigation")
	appendPart(len(manifest.Contributes.Commands), "pluginsSummaryCommand")
	appendPart(len(manifest.Contributes.Views), "pluginsSummaryNativeView")
	appendPart(len(manifest.Contributes.TaskPanels), "pluginsSummaryTaskPanel")
	appendPart(len(manifest.Contributes.Settings), "pluginsSummarySetting")
	appendPart(len(manifest.Contributes.WebViews), "pluginsSummaryWebView")
	appendPart(len(manifest.Contributes.Automations), "pluginsSummaryAutomation")
	if len(parts) == 0 {
		return textArgs("pluginsContributionCount", map[string]any{"count": 0})
	}
	return strings.Join(parts, " · ")
}

func contractChip(c *ui.Context, p palette, label string) {
	ui.Row(c).Padding(4, 8).Radius(999).Background(p.surfaceHover).Gap(5).
		AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icon("check")).Size(11, 11).TextColor(p.success)
		ui.Text(c, label).Font("monospace").FontSize(fontMicro).TextColor(p.text)
	})
}

func (s *appState) automationsPage(c *ui.Context, p palette) {
	registry := s.pluginRegistrySnapshot()
	type automationItem struct {
		entry        pluginRegistryEntry
		contribution pluginAutomationContribution
		key          string
		state        automationStoredState
		runs         []automationRunRecord
		running      bool
	}
	var items []automationItem
	paused := 0
	for _, entry := range registry.Entries {
		entry = pluginLocalizedEntry(entry)
		if entry.Status != pluginRegistered || entry.Manifest == nil {
			continue
		}
		for _, contribution := range entry.Manifest.Contributes.Automations {
			key := automationKey(entry.ID, contribution.ID)
			state := automationStoredState{Enabled: true}
			var runs []automationRunRecord
			running := false
			if s.automations != nil {
				snapshot := s.automations.snapshotFor(key, 5)
				state = snapshot.State
				runs = snapshot.Runs
				running = snapshot.Running
			}
			if !state.Enabled {
				paused++
			}
			items = append(items, automationItem{
				entry:        entry,
				contribution: contribution,
				key:          key,
				state:        state,
				runs:         runs,
				running:      running,
			})
		}
	}
	sort.SliceStable(items, func(left, right int) bool {
		leftName := fallback(items[left].entry.Name, items[left].entry.ID)
		rightName := fallback(items[right].entry.Name, items[right].entry.ID)
		if leftName != rightName {
			return leftName < rightName
		}
		if items[left].entry.ID != items[right].entry.ID {
			return items[left].entry.ID < items[right].entry.ID
		}
		return items[left].contribution.ID < items[right].contribution.ID
	})

	ui.Scroll(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Justify(ui.Center).Padding(30, 28, 38, 28).Children(func() {
			ui.Column(c).Grow(1).MaxWidth(880).Gap(22).Children(func() {
				pageHeader(c, p, text("automationsSection"), text("automationsTitle"),
					text("automationsDescription"))

				ui.Row(c).Wrap().AlignItems(ui.Center).GapX(9).GapY(7).Children(func() {
					statusPill(c, p, textArgs("automationsRegistered", map[string]any{
						"count": len(items),
					}), p.accent)
					statusPill(c, p, textArgs("automationsPausedCount", map[string]any{
						"count": paused,
					}), p.muted)
					ui.Text(c, text("automationsManualNote")).
						FontSize(fontSmall).TextColor(p.muted)
				})

				if s.automations != nil {
					for _, warning := range s.automations.warningsSnapshot() {
						ui.Text(c, automationWarningLabel(warning)).FontSize(fontCaption).
							TextColor(p.danger)
					}
				}

				sectionLabel(c, p, text("automationsRegisteredSection"))
				if len(items) == 0 {
					ui.Column(c).FillWidth().Gap(7).Padding(18).Radius(11).
						Border(1, p.border).Background(p.surface).Children(func() {
						ui.Text(c, text("automationsEmptyTitle")).FontSize(fontBody).
							FontWeight(650).TextColor(p.text)
						ui.Row(c).AlignItems(ui.Center).Children(func() {
							ui.Text(c, text("automationsEmptyBefore")).FontSize(fontSmall).TextColor(p.muted)
							ui.Text(c, text("automationsEmptyField")).Font("monospace").
								FontSize(fontSmall).TextColor(p.text)
							ui.Text(c, text("automationsEmptyAfter")).FontSize(fontSmall).TextColor(p.muted)
						})
					})
				} else {
					ui.Column(c).FillWidth().Gap(12).Children(func() {
						for _, item := range items {
							s.automationContributionCard(c, p, item.entry, item.contribution,
								item.state, item.runs, item.running)
						}
					})
				}
			})
		})
	})
}

func (s *appState) automationContributionCard(
	c *ui.Context,
	p palette,
	entry pluginRegistryEntry,
	contribution pluginAutomationContribution,
	state automationStoredState,
	runs []automationRunRecord,
	running bool,
) {
	key := automationKey(entry.ID, contribution.ID)
	title := fallback(contribution.Title, contribution.ID)
	description := fallback(contribution.Description, text("pluginsNoDescription"))
	status, tone := automationStatusLabel(p, state, running)
	ui.Column(c).FillWidth().Gap(12).Padding(16).Radius(11).
		Border(1, p.border).Background(p.surface).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Column(c).Gap(2).Grow(1).Children(func() {
				ui.Text(c, title).FontSize(fontBody).FontWeight(650).TextColor(p.text)
				ui.Text(c, fallback(entry.Name, entry.ID)).FontSize(fontCaption).
					TextColor(p.accent)
			})
			ui.Spacer(c)
			statusPill(c, p, status, tone)
		})
		ui.Row(c).Wrap().GapX(7).GapY(5).Children(func() {
			statusPill(c, p, automationKindLabel(contribution), p.muted)
			statusPill(c, p, textArgs("automationsFailures", map[string]any{
				"count": state.ConsecutiveFailures,
				"max":   automationMaxConsecutiveFailures,
			}),
				automationFailureTone(p, state.ConsecutiveFailures))
			if state.LastRunAt != "" {
				ui.Text(c, textArgs("automationsLastRun", map[string]any{
					"time":  automationLocalTimestamp(state.LastRunAt),
					"state": automationRunStateLabel(state.LastState),
				})).
					FontSize(fontCaption).TextColor(p.muted)
			} else {
				ui.Text(c, text("automationsNoRuns")).FontSize(fontCaption).TextColor(p.muted)
			}
		})
		ui.Text(c, description).FontSize(fontSmall).LineHeight(1.45).TextColor(p.muted)
		if !state.Enabled && state.AutoPausedReason != "" {
			ui.Text(c, automationPausedReason(state.AutoPausedReason)).FontSize(fontCaption).
				TextColor(p.danger)
		}
		ui.Row(c).Wrap().GapX(7).GapY(7).AlignItems(ui.Center).Children(func() {
			runDisabled := running || !state.Enabled
			runButton := ui.ButtonBase(c.Key("automation-run-"+key)).
				Padding(7, 11).Radius(7).Border(1, p.border).Background(p.surface).
				Cursor(ui.CursorPointer).Label(text("automationsRunNow")).Disabled(runDisabled)
			if runButton.Hovered() && !runDisabled {
				runButton.Background(p.surfaceHover)
			}
			runButton.Children(func() {
				ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, icon("play")).Size(11, 11).
						TextColor(automationActionTone(p, runDisabled))
					ui.Text(c, text("automationsRunNow")).FontSize(fontSmall).FontWeight(600).
						TextColor(automationActionTone(p, runDisabled))
				})
			})
			if runButton.Clicked() && !runDisabled {
				s.runAutomation(entry, contribution)
			}
			if state.Enabled {
				secondaryAction(c, p, "automation-pause-"+key, text("automationsPause"), func() {
					s.setAutomationPaused(key, true)
				})
			} else {
				secondaryAction(c, p, "automation-resume-"+key, text("automationsResume"), func() {
					s.setAutomationPaused(key, false)
				})
			}
		})
		if len(runs) > 0 {
			ui.Divider(c).MarginY(2)
			sectionLabel(c, p, text("automationsRecentRuns"))
			for _, run := range runs {
				s.automationRunRow(c, p, run)
			}
		}
	})
}

func automationKindLabel(contribution pluginAutomationContribution) string {
	switch contribution.Kind {
	case "scheduledLoop":
		return textArgs("automationsKindScheduledLoop", map[string]any{
			"rule": automationRuleLabel(contribution.Rule),
		})
	case "sessionLoop":
		return textArgs("automationsKindSessionLoop", map[string]any{
			"rule": automationRuleLabel(contribution.Rule),
		})
	case "once":
		return textArgs("automationsKindOnce", map[string]any{
			"rule": automationRuleLabel(contribution.Rule),
		})
	default:
		return text("automationsKindManual")
	}
}

func automationRuleLabel(rule *pluginAutomationRule) string {
	if rule == nil {
		return text("automationsRuleNone")
	}
	switch rule.Type {
	case "interval":
		return textArgs("automationsRuleEveryMinutes", map[string]any{"count": rule.Minutes})
	case "cron":
		return textArgs("automationsRuleCron", map[string]any{"rule": rule.Cron})
	case "at":
		return rule.At
	case "afterSettled":
		return text("automationsRuleAfterSettled")
	default:
		return rule.Type
	}
}

func automationStatusLabel(p palette, state automationStoredState, running bool) (string, ui.Color) {
	if running {
		return text("automationsStatusRunning"), p.accent
	}
	if !state.Enabled {
		if state.AutoPausedReason != "" {
			return text("automationsStatusAutoPaused"), p.danger
		}
		return text("automationsStatusPaused"), p.muted
	}
	return text("automationsStatusReady"), p.success
}

func automationPausedReason(reason string) string {
	switch reason {
	case "consecutiveFailures":
		return text("automationsPausedConsecutiveFailures")
	case "appRestarted":
		return text("automationsPausedAppRestarted")
	default:
		return reason
	}
}

func automationRunStateLabel(state string) string {
	switch state {
	case string(automationRunRunning):
		return text("automationsRunStateRunning")
	case string(automationRunSucceeded):
		return text("automationsRunStateSucceeded")
	case string(automationRunFailed):
		return text("automationsRunStateFailed")
	case string(automationRunAborted):
		return text("automationsRunStateAborted")
	default:
		return state
	}
}

func automationErrorCodeLabel(code string) string {
	switch code {
	case "timeLimit":
		return text("automationsErrorTimeLimit")
	case "pluginError":
		return text("automationsErrorPluginError")
	case "stopped":
		return text("automationsErrorStopped")
	case "interrupted":
		return text("automationsErrorInterrupted")
	default:
		return code
	}
}

func automationWarningLabel(warning string) string {
	switch warning {
	case "Automation storage could not be opened.":
		return text("automationsWarningStorageUnavailable")
	case "Automation run storage could not be opened.":
		return text("automationsWarningRunStorageUnavailable")
	case "Automation state could not be read; defaults are in use.":
		return text("automationsWarningStateUnreadable")
	case "Automation run history could not be loaded.":
		return text("automationsWarningRunHistoryUnavailable")
	case "Automation recovery state could not be saved.":
		return text("automationsWarningRecoveryUnsaved")
	case "Automation run history could not be scanned.":
		return text("automationsWarningHistoryScanFailed")
	case "An obsolete legacy automation run file was ignored.":
		return text("automationsWarningLegacyRunIgnored")
	case "An automation run file could not be read completely.":
		return text("automationsWarningRunIncomplete")
	case "An automation run file was corrupt and was quarantined.":
		return text("automationsWarningRunQuarantined")
	case "An automation run path could not be resolved.":
		return text("automationsWarningRunPathUnresolved")
	case "An interrupted automation run could not be recovered.":
		return text("automationsWarningInterruptedRecoveryFailed")
	case "Automation state rollback could not be saved.":
		return text("automationsWarningRollbackUnsaved")
	case "An oversized automation run file could not be trimmed.":
		return text("automationsWarningOversizedRunNotTrimmed")
	case "Corrupt automation state could not be quarantined before saving.":
		return text("automationsWarningStateQuarantineFailed")
	case "Automation state was corrupt and was quarantined; defaults are in use.":
		return text("automationsWarningStateQuarantined")
	case "Automation run history could not be read.":
		return text("automationsWarningHistoryUnreadable")
	case "An automation run file was corrupt and is ignored.":
		return text("automationsWarningRunIgnored")
	case "An automation run file could not be read.":
		return text("automationsWarningRunUnreadable")
	case "Corrupt automation run history could not be quarantined before saving.":
		return text("automationsWarningHistoryQuarantineFailed")
	default:
		return warning
	}
}

func automationFailureTone(p palette, failures int) ui.Color {
	if failures >= automationMaxConsecutiveFailures {
		return p.danger
	}
	if failures > 0 {
		return p.warning
	}
	return p.muted
}

func automationActionTone(p palette, disabled bool) ui.Color {
	if disabled {
		return p.muted
	}
	return p.text
}

func automationLocalTimestamp(value string) string {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	return parsed.Local().Format("2006-01-02 15:04:05")
}

func automationRunDuration(run automationRunRecord) string {
	if run.EndedAt == "" {
		return text("automationsRunningDuration")
	}
	started, startErr := time.Parse(time.RFC3339Nano, run.StartedAt)
	ended, endErr := time.Parse(time.RFC3339Nano, run.EndedAt)
	if startErr != nil || endErr != nil {
		return "—"
	}
	duration := ended.Sub(started)
	if duration < time.Second {
		return fmt.Sprintf("%d ms", duration.Milliseconds())
	}
	return duration.Round(time.Millisecond).String()
}

func (s *appState) automationRunRow(c *ui.Context, p palette, run automationRunRecord) {
	tone := p.muted
	iconName := "clock"
	switch run.State {
	case automationRunSucceeded:
		tone = p.success
		iconName = "check"
	case automationRunFailed, automationRunAborted:
		tone = p.danger
		iconName = "x"
	case automationRunRunning:
		tone = p.accent
	}
	detail := pluginResultSummaryLabel(run.Result)
	if run.ErrorCode != "" || run.Error != "" {
		errorText := run.Error
		if run.ErrorCode == "interrupted" {
			errorText = text("automationsErrorInterrupted")
		} else if errorText != "" {
			errorText = uiErrorFromText(errorText, text("automationsFailedFallback"))
		}
		detail = strings.TrimSpace(strings.Join(nonEmpty(automationErrorCodeLabel(run.ErrorCode), errorText), " · "))
	}
	if detail == "" {
		detail = text("automationsNoResult")
	}
	startedAt := run.StartedAt
	if parsed, err := time.Parse(time.RFC3339Nano, run.StartedAt); err == nil {
		startedAt = parsed.Local().Format("15:04:05")
	}
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icon(iconName)).Size(12, 12).TextColor(tone)
		ui.Text(c, fmt.Sprintf("%s · %s · %s · %s", automationRunStateLabel(string(run.State)),
			automationTriggerLabel(run.Trigger), startedAt, automationRunDuration(run))).
			Font("monospace").FontSize(fontCaption).TextColor(tone).Grow(1).SingleLine()
		ui.Text(c, summarizeLine(detail, 150)).FontSize(fontCaption).TextColor(p.muted).
			MaxWidth(380).SingleLine()
	})
}

func automationTriggerLabel(trigger string) string {
	switch trigger {
	case "manual":
		return text("automationTriggerManual")
	case "scheduled":
		return text("automationTriggerScheduled")
	default:
		return fallback(trigger, text("automationTriggerUnknown"))
	}
}

func (s *appState) runAutomation(entry pluginRegistryEntry, contribution pluginAutomationContribution) {
	if s.automations == nil || s.plugins == nil {
		s.showNotice(text("automationsRuntimeUnavailable"))
		return
	}
	key := automationKey(entry.ID, contribution.ID)
	snapshot := s.automations.snapshotFor(key, 0)
	if !snapshot.State.Enabled {
		s.showNotice(text("automationsResumeBeforeRun"))
		return
	}
	if snapshot.Running {
		s.showNotice(text("automationsAlreadyRunning"))
		return
	}
	record, err := s.automations.beginRun(key, entry.ID, contribution.ID, contribution.Command)
	if err != nil {
		s.showNotice(textArgs("automationsStartFailed", map[string]any{
			"error": uiErrorSummary(err, text("errorUnknown"), 200),
		}))
		return
	}
	if s.window != nil {
		s.window.Invalidate()
	}
	s.showNotice(textArgs("automationsRunningNotice", map[string]any{
		"title": fallback(contribution.Title, contribution.ID),
	}))
	timeoutMinutes := contribution.MaxDurationMinutes
	if timeoutMinutes == 0 {
		timeoutMinutes = automationDefaultMaxDurationMinutes
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMinutes)*time.Minute)
		defer cancel()
		result, invokeErr := s.plugins.invokeCommandTimeout(ctx, entry, contribution.Command,
			map[string]any{
				"automation": contribution.ID,
				"trigger":    "manual",
				"runId":      record.RunID,
			}, time.Duration(timeoutMinutes)*time.Minute)
		outcome := automationOutcomeSuccess
		errorSummary := ""
		resultSummary := ""
		if invokeErr != nil {
			outcome = automationOutcomeFailure
			switch {
			case errors.Is(invokeErr, errPluginStopped):
				outcome = automationOutcomeStopped
			case errors.Is(invokeErr, context.DeadlineExceeded):
				outcome = automationOutcomeTimeout
			}
			errorSummary = uiErrorText(invokeErr, text("errorUnknown"))
		} else {
			resultSummary = pluginResultSummary(result)
		}
		autoPaused, finishErr := s.automations.finishRun(key, record.RunID, outcome,
			errorSummary, resultSummary)
		s.postUI(func() {
			if finishErr != nil {
				s.showNotice(textArgs("automationsFinishSaveFailed", map[string]any{
					"error": uiErrorSummary(finishErr, text("errorUnknown"), 200),
				}))
			} else {
				title := fallback(contribution.Title, contribution.ID)
				switch outcome {
				case automationOutcomeSuccess:
					s.showNotice(textArgs("automationsCompleted", map[string]any{
						"title": title,
						"result": fallback(pluginResultSummaryLabel(resultSummary),
							text("automationsCommandCompleted")),
					}))
				case automationOutcomeTimeout:
					s.showNotice(textArgs("automationsTimedOut", map[string]any{
						"title": title,
						"count": timeoutMinutes,
					}))
				case automationOutcomeStopped:
					s.showNotice(textArgs("automationsStopped", map[string]any{"title": title}))
				default:
					s.showNotice(textArgs("automationsFailed", map[string]any{
						"title": title,
						"error": summarizeLine(errorSummary, 200),
					}))
				}
				if autoPaused {
					s.showNotice(textArgs("automationsAutoPausedNotice", map[string]any{
						"title": title,
						"count": automationMaxConsecutiveFailures,
					}))
				}
			}
		})
	}()
}

func (s *appState) setAutomationPaused(key string, paused bool) {
	if s.automations == nil {
		return
	}
	var err error
	if paused {
		err = s.automations.pause(key)
	} else {
		err = s.automations.resume(key)
	}
	if err != nil {
		s.showNotice(textArgs("automationsStateSaveFailed", map[string]any{
			"error": uiErrorSummary(err, text("errorUnknown"), 200),
		}))
		return
	}
	if s.window != nil {
		s.window.Invalidate()
	}
}

func (s *appState) settingsPage(c *ui.Context, p palette) {
	registry := s.pluginRegistrySnapshot()
	ui.Scroll(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Justify(ui.Center).Padding(30, 28, 38, 28).Children(func() {
			ui.Column(c).Grow(1).MaxWidth(820).Gap(22).Children(func() {
				pageHeader(c, p, locales.SettingsSection(), locales.SettingsTitle(),
					locales.SettingsDescription())

				settingsCard(c, p, locales.SettingsLanguageTitle(),
					locales.SettingsLanguageDescription(), func() {
						ui.Row(c).Gap(8).Wrap().Children(func() {
							s.languageButton(c, p, uiLanguageSystem, locales.SettingsLanguageSystem())
							for _, tag := range locales.Catalog.Languages() {
								s.languageButton(c, p, tag, i18n.Autonym(tag))
							}
						})
					})

				settingsCard(c, p, locales.SettingsAppearanceTitle(),
					locales.SettingsAppearanceDescription(), func() {
						ui.Row(c).Gap(8).Wrap().Children(func() {
							s.themeButton(c, p, themeSystem, "monitor", locales.SettingsAppearanceSystem())
							s.themeButton(c, p, themeLight, "sun", locales.SettingsAppearanceLight())
							s.themeButton(c, p, themeDark, "moon", locales.SettingsAppearanceDark())
						})
						s.settingsChoiceRow(c, p, "contrast",
							text("settingsContrastTitle"), text("settingsContrastDescription"),
							[]settingsChoice{
								{"system", "monitor", text("settingsContrastSystem"), func() { s.setContrast(contrastSystem) }},
								{"standard", "sun", text("settingsContrastStandard"), func() { s.setContrast(contrastNormal) }},
								{"high", "contrast", text("settingsContrastHigh"), func() { s.setContrast(contrastHigh) }},
							},
						)
						s.settingsChoiceRow(c, p, "motion",
							text("settingsMotionTitle"), text("settingsMotionDescription"),
							[]settingsChoice{
								{"system", "monitor", text("settingsMotionSystem"), func() { s.setMotion(motionSystem) }},
								{"standard", "activity", text("settingsMotionStandard"), func() { s.setMotion(motionStandard) }},
								{"reduced", "stop", text("settingsMotionReduced"), func() { s.setMotion(motionReduced) }},
							},
						)
						s.settingsChoiceRow(c, p, "panel-animation",
							text("settingsPanelAnimationTitle"), text("settingsPanelAnimationDescription"),
							[]settingsChoice{
								{"0", "", text("settingsPanelAnimation0"), func() { s.setPanelAnimationMS(0) }},
								{"150", "", text("settingsPanelAnimation150"), func() { s.setPanelAnimationMS(150) }},
								{"250", "", text("settingsPanelAnimation250"), func() { s.setPanelAnimationMS(250) }},
								{"400", "", text("settingsPanelAnimation400"), func() { s.setPanelAnimationMS(400) }},
							},
						)
					})

				settingsCard(c, p, text("settingsRunningSendTitle"),
					text("settingsRunningSendDescription"), func() {
						ui.Row(c).Gap(8).Wrap().Children(func() {
							s.choiceButton(c, p, "running-send-queue", "list",
								text("runningSendQueue"), s.currentRunningSendMode() == runningSendQueue, func() {
									s.setRunningSendMode(runningSendQueue)
								})
							s.choiceButton(c, p, "running-send-steer", "corner-up-left",
								text("runningSendSteer"), s.currentRunningSendMode() == runningSendSteer, func() {
									s.setRunningSendMode(runningSendSteer)
								})
						})
					})

				settingsCard(c, p, locales.SettingsFollowUpTitle(),
					locales.SettingsFollowUpDescription(), func() {
						ui.Row(c).Gap(8).Wrap().Children(func() {
							s.followUpStyleButton(c, p, followUpStyleGhost, "chat",
								locales.SettingsFollowUpGhost())
							s.followUpStyleButton(c, p, followUpStyleCard, "document",
								locales.SettingsFollowUpCard())
						})
						ui.Text(c, locales.ChatFollowUpHint()).FontSize(fontSmall).TextColor(p.muted)
					})

				s.legacyMigrationSection(c, p)

				settingsCard(c, p, locales.SettingsPiTitle(), locales.SettingsPiDescription(), func() {
					ui.Row(c).Gap(9).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Size(8, 8).Radius(4).Background(s.piTone(p))
						ui.Text(c, s.piConnectionLabel()).FontSize(fontSmall).TextColor(p.text).Grow(1)
						label := locales.SettingsConnect()
						if s.piConn == piConnected {
							label = locales.SettingsReconnect()
						}
						secondaryAction(c, p, "settings-pi-connect", label, func() {
							s.reconnectPi()
						})
					})
					ui.Column(c).Gap(8).Children(func() {
						settingsValue(c, p, locales.SettingsCliPath(), s.piPathLabel())
						settingsValue(c, p, locales.SettingsVersion(),
							fallback(s.piVersion, locales.SettingsPendingHandshake()))
						settingsValue(c, p, locales.SettingsModel(), s.modelSettingsLabel())
						settingsValue(c, p, locales.SettingsSession(), s.sessionLabel())
						settingsValue(c, p, locales.SettingsRunState(),
							s.runLabel()+" · "+s.runDetail())
					})
					if s.piError != "" {
						ui.Text(c, s.piError).FontSize(fontCaption).LineHeight(1.4).TextColor(p.danger)
					}
				})

				sectionLabel(c, p, locales.SettingsPluginSettings())
				s.pluginSettingsSection(c, p)

				settingsCard(c, p, locales.SettingsWorkbenchTitle(),
					locales.SettingsWorkbenchDescription(), func() {
						ui.Column(c).Gap(8).Children(func() {
							settingsValue(c, p, locales.SettingsNativeRuntime(),
								locales.SettingsNativeRuntimeValue())
							settingsValue(c, p, locales.SettingsRpcTransport(),
								locales.SettingsRpcTransportValue())
							settingsValue(c, p, locales.SettingsPiOwnership(),
								locales.SettingsPiOwnershipValue())
							settingsValue(c, p, locales.SettingsPluginRegistry(),
								textArgs("settingsPluginRegistryValue", map[string]any{
									"registered":    registry.registeredCount(),
									"contributions": registry.contributionCount(),
								}))
							settingsValue(c, p, locales.SettingsPluginRuntime(),
								locales.SettingsPluginRuntimeValue())
							settingsValue(c, p, locales.SettingsWebViewBoundary(),
								locales.SettingsWebViewBoundaryValue())
						})
					})

				settingsCard(c, p, locales.SettingsAboutTitle(),
					locales.SettingsAboutDescription(), func() {
						ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
							brandMark(c, p, 38)
							ui.Column(c).Gap(1).Children(func() {
								ui.Text(c, appShort).FontSize(fontTitle).FontWeight(650).TextColor(p.text)
								ui.Text(c, locales.SettingsAboutVersion()).FontSize(fontCaption).
									TextColor(p.muted)
							})
						})
					})
			})
		})
	})
}

func (s *appState) sessionLabel() string {
	if s.piSessionID == "" {
		return locales.SettingsNotAssigned()
	}
	if s.piSessionFile == "" {
		return s.piSessionID + " · " + locales.SettingsEphemeral()
	}
	active := s.activePiSession()
	if active.Title != "" {
		return active.Title + " · " + s.piSessionID
	}
	return s.piSessionID
}

func (s *appState) modelSettingsLabel() string {
	model := strings.TrimSpace(strings.Join(nonEmpty(s.piModelProvider, s.piModelName), " / "))
	if model == "" {
		model = fallback(s.piModel, locales.SettingsPiDefault())
	}
	level := fallback(s.piThinkingLevel, "off")
	return model + " · " + locales.SettingsThinking(level)
}

func settingsCard(c *ui.Context, p palette, title, description string, body func()) {
	ui.Column(c).FillWidth().Gap(13).Padding(16).Radius(12).
		Border(1, p.border).Background(p.surface).Children(func() {
		ui.Column(c).Gap(4).Children(func() {
			ui.Text(c, title).FontSize(fontBody).FontWeight(650).TextColor(p.text)
			ui.Text(c, description).FontSize(fontSmall).LineHeight(1.45).TextColor(p.muted)
		})
		body()
	})
}

func settingsValue(c *ui.Context, p palette, label, value string) {
	ui.Row(c).FillWidth().Padding(8, 10).Radius(7).Background(p.background).
		AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(fontCaption).TextColor(p.muted).Width(180)
		ui.Text(c, value).FontSize(fontCaption).FontWeight(600).TextColor(p.text).Grow(1)
	})
}

func (s *appState) themeButton(c *ui.Context, p palette, mode themeMode, iconName, label string) {
	selected := s.themeMode == mode
	bg := p.background
	fg := p.muted
	border := p.border
	if selected {
		bg = p.surfaceHover
		fg = p.accent
		border = p.accent
	}
	btn := ui.ButtonBase(c.Key("theme-"+mode)).Padding(8, 12).Radius(8).
		Border(1, border).Background(bg).Cursor(ui.CursorPointer).Label(label)
	if btn.Hovered() && !selected {
		btn.Background(p.surfaceHover)
	}
	btn.Children(func() {
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon(iconName)).Size(15, 15).TextColor(fg)
			ui.Text(c, label).FontSize(fontSmall).FontWeight(600).TextColor(fg)
			if selected {
				ui.Icon(c, icon("check")).Size(12, 12).TextColor(fg)
			}
		})
	})
	if btn.Clicked() {
		s.setTheme(mode)
	}
}

func (s *appState) languageButton(c *ui.Context, p palette, value, label string) {
	selected := normalizeUILanguage(s.uiLanguage) == value
	bg := p.background
	fg := p.muted
	border := p.border
	if selected {
		bg = p.surfaceHover
		fg = p.accent
		border = p.accent
	}
	btn := ui.ButtonBase(c.Key("language-"+value)).Padding(8, 12).Radius(8).
		Border(1, border).Background(bg).Cursor(ui.CursorPointer).Label(label)
	if btn.Hovered() && !selected {
		btn.Background(p.surfaceHover)
	}
	btn.Children(func() {
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon("languages")).Size(15, 15).TextColor(fg)
			ui.Text(c, label).FontSize(fontSmall).FontWeight(600).TextColor(fg)
			if selected {
				ui.Icon(c, icon("check")).Size(12, 12).TextColor(fg)
			}
		})
	})
	if btn.Clicked() {
		s.setUILanguage(value)
	}
}

func (s *appState) followUpStyleButton(c *ui.Context, p palette, style followUpStyle, iconName, label string) {
	selected := s.currentFollowUpStyle() == style
	bg := p.background
	fg := p.muted
	border := p.border
	if selected {
		bg = p.surfaceHover
		fg = p.accent
		border = p.accent
	}
	btn := ui.ButtonBase(c.Key("follow-up-style-"+string(style))).Padding(8, 12).Radius(8).
		Border(1, border).Background(bg).Cursor(ui.CursorPointer).Label(label)
	if btn.Hovered() && !selected {
		btn.Background(p.surfaceHover)
	}
	btn.Children(func() {
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon(iconName)).Size(15, 15).TextColor(fg)
			ui.Text(c, label).FontSize(fontSmall).FontWeight(600).TextColor(fg)
			if selected {
				ui.Icon(c, icon("check")).Size(12, 12).TextColor(fg)
			}
		})
	})
	if btn.Clicked() {
		s.setFollowUpStyle(style)
	}
}

type settingsChoice struct {
	id       string
	iconName string
	label    string
	click    func()
}

func (s *appState) settingsChoiceRow(c *ui.Context, p palette, key, title, description string,
	choices []settingsChoice,
) {
	ui.Column(c).Gap(7).Children(func() {
		ui.Text(c, title).FontSize(fontSmall).FontWeight(650).TextColor(p.text)
		ui.Text(c, description).FontSize(fontCaption).TextColor(p.muted)
		ui.Row(c).Gap(8).Wrap().Children(func() {
			for index, choice := range choices {
				selected := false
				switch key {
				case "contrast":
					values := []contrastMode{contrastSystem, contrastNormal, contrastHigh}
					selected = normalizeContrastMode(s.preferences.Contrast) == values[index]
				case "motion":
					values := []motionMode{motionSystem, motionStandard, motionReduced}
					selected = normalizeMotionMode(s.preferences.Motion) == values[index]
				case "panel-animation":
					values := []int{0, 150, 250, 400}
					selected = normalizePanelAnimationMS(s.preferences.PanelAnimationMS) == values[index]
				}
				s.choiceButton(c, p, key+"-"+choice.id, choice.iconName, choice.label, selected, choice.click)
			}
		})
	})
}

func (s *appState) choiceButton(c *ui.Context, p palette, key, iconName, label string, selected bool, click func()) {
	bg, fg, border := p.background, p.muted, p.border
	if selected {
		bg, fg, border = p.surfaceHover, p.accent, p.accent
	}
	button := ui.ButtonBase(c.Key(key)).Padding(8, 12).Radius(8).
		Border(1, border).Background(bg).Cursor(ui.CursorPointer).Label(label)
	if button.Hovered() && !selected {
		button.Background(p.surfaceHover)
	}
	button.Children(func() {
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			if iconName != "" {
				ui.Icon(c, icon(iconName)).Size(15, 15).TextColor(fg)
			}
			ui.Text(c, label).FontSize(fontSmall).FontWeight(600).TextColor(fg)
			if selected {
				ui.Icon(c, icon("check")).Size(12, 12).TextColor(fg)
			}
		})
	})
	if button.Clicked() {
		click()
	}
}
