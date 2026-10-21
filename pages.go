package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

func (s *appState) pluginsPage(c *ui.Context, p palette) {
	registry := s.pluginRegistrySnapshot()
	entries := s.filteredPluginEntries()
	ui.Scroll(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Justify(ui.Center).Padding(30, 28, 38, 28).Children(func() {
			ui.Column(c).Grow(1).MaxWidth(880).Gap(22).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.End).Gap(16).Children(func() {
					ui.Column(c).Grow(1).Children(func() {
						pageHeader(c, p, "WORKBENCH", "Plugins", "Discover local plugins, open typed native views, or host isolated HTML in a capability-checked WebView. Native and Node runtimes start lazily and share the same host API.")
					})
					primaryAction(c, p, "plugins-scan", "Scan plugins", "package", func() {
						s.scanPlugins(true)
					})
				})

				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Column(c).Width(300).MinWidth(180).Children(func() {
						searchField(c, p, "plugin-search", &s.pluginSearch, "Search plugins")
					})
					tone := p.success
					if registry.problemCount() > 0 {
						tone = p.warning
					}
					statusPill(c, p, fmt.Sprintf("%d REGISTERED · %d CONTRIBUTIONS",
						registry.registeredCount(), registry.contributionCount()), tone)
					ui.Spacer(c)
					secondaryAction(c, p, "plugin-dir", "Plugin folders", func() {
						s.showNotice("Plugin roots · " + strings.Join(registry.Roots, " · "))
					})
				})

				if len(registry.Issues) > 0 {
					s.pluginRegistryIssues(c, p, registry.Issues)
				}

				sectionLabel(c, p, "DISCOVERED PLUGINS")
				if len(entries) == 0 {
					s.emptyPluginRegistry(c, p)
				} else {
					ui.Row(c).Wrap().AlignItems(ui.Start).GapX(12).GapY(12).Children(func() {
						for _, entry := range entries {
							s.pluginCard(c, p, entry)
						}
					})
				}

				sectionLabel(c, p, "PLUGIN CONTRACT PREVIEW")
				ui.Column(c).FillWidth().Gap(10).Padding(14).Radius(11).
					Border(1, p.border).Background(p.surface).Children(func() {
					ui.Text(c, fmt.Sprintf("Host %s validates schema %d and plugin API %s. P5 activates native and Node plugins on demand, renders typed native views, and hosts isolated WebViews through the byop-plugin scheme with a strict CSP. Page calls cross one identity-checked bridge into the same capability-checked host API.",
						registry.HostVersion, pluginManifestSchemaVersion, registry.PluginAPI)).
						FontSize(fontBody).LineHeight(1.5).TextColor(p.text)
					ui.Row(c).Wrap().Gap(7).Children(func() {
						contractChip(c, p, "commands")
						contractChip(c, p, "views")
						contractChip(c, p, "webviews")
						contractChip(c, p, "tasks")
						contractChip(c, p, "settings")
						contractChip(c, p, "host calls")
						contractChip(c, p, "node permissions")
						contractChip(c, p, "custom scheme CSP")
					})
				})
			})
		})
	})
}

func (s *appState) filteredPluginEntries() []pluginRegistryEntry {
	query := strings.ToLower(strings.TrimSpace(s.pluginSearch))
	registry := s.pluginRegistrySnapshot()
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
			ui.Text(c, "Registry scan issues").FontSize(fontBody).FontWeight(650).TextColor(p.text)
		})
		for _, issue := range issues {
			ui.Text(c, issue.Path+" · "+issue.Message).Font("monospace").FontSize(fontCaption).
				TextColor(p.danger).Selectable()
		}
	})
}

func (s *appState) emptyPluginRegistry(c *ui.Context, p palette) {
	message := "No plugin.json manifests were found in the configured roots."
	if strings.TrimSpace(s.pluginSearch) != "" {
		message = "No registered plugin matches “" + s.pluginSearch + "”."
	}
	ui.Column(c).FillWidth().Gap(10).Padding(18).Radius(11).
		Border(1, p.border).Background(p.surface).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Size(34, 34).Radius(9).Background(p.surfaceHover).Center().Children(func() {
				ui.Icon(c, icon("package")).Size(17, 17).TextColor(p.muted)
			})
			ui.Column(c).Gap(2).Grow(1).Children(func() {
				ui.Text(c, "Registry is empty").FontSize(fontBody).FontWeight(650).TextColor(p.text)
				ui.Text(c, message).FontSize(fontSmall).TextColor(p.muted)
			})
		})
	})
}

func (s *appState) pluginCard(c *ui.Context, p palette, entry pluginRegistryEntry) {
	tone := s.pluginStatusTone(p, entry.Status)
	title := fallback(entry.Name, entry.ID)
	description := fallback(entry.Description, "No description declared.")
	surface := "Manifest rejected"
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
		meta := fmt.Sprintf("%d contribution(s)", entry.ContributionCount)
		if entry.Version != "" {
			meta = "v" + entry.Version + " · " + meta
		}
		if entry.Manifest != nil {
			meta = "runtime " + entry.Manifest.Runtime + " · " + meta
		}
		ui.Text(c, meta).FontSize(fontCaption).TextColor(p.muted)
		if entry.Manifest != nil {
			ui.Text(c, pluginPermissionsLine(entry.Manifest)).FontSize(fontCaption).
				LineHeight(1.35).TextColor(p.muted)
			ui.Text(c, pluginEnforcementLine(entry.Manifest.Runtime)).FontSize(fontCaption).
				LineHeight(1.35).TextColor(p.muted)
			for _, webView := range entry.Manifest.Contributes.WebViews {
				ui.Text(c, fmt.Sprintf("%s · %s · Isolated · byop-plugin scheme · CSP",
					fallback(webView.Title, webView.ID), webView.Entry)).
					FontSize(fontCaption).LineHeight(1.35).TextColor(p.muted)
			}
			var secondaryViews []string
			for _, view := range entry.Manifest.Contributes.Views {
				if view.Location == "secondary" {
					secondaryViews = append(secondaryViews, fallback(view.Title, view.ID))
				}
			}
			if len(secondaryViews) > 0 {
				ui.Text(c, "Secondary views · "+strings.Join(secondaryViews, ", ")).
					FontSize(fontCaption).LineHeight(1.35).TextColor(p.muted)
			}
		}
		if entry.Error != "" {
			ui.Text(c, summarizeLine(entry.Error, 240)).FontSize(fontCaption).
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
			ui.Text(c, summarizeLine(runtime.LastError, 240)).FontSize(fontCaption).
				LineHeight(1.4).TextColor(p.danger)
		}
		if runtime.HostCalls > 0 {
			ui.Text(c, fmt.Sprintf("%d host call(s)", runtime.HostCalls)).FontSize(fontCaption).
				TextColor(p.muted)
		}
		if runtime.DeniedCalls > 0 {
			ui.Text(c, fmt.Sprintf("Denied: %s", summarizeLine(runtime.LastDenied, 180))).
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
				secondaryAction(c, p, "plugin-stop-"+entry.ID, "Deactivate", func() {
					s.deactivatePlugin(entry)
				})
			}
			for _, view := range entry.Manifest.Contributes.Views {
				view := view
				if view.Location != "main" {
					continue
				}
				secondaryAction(c, p, "plugin-open-view-"+entry.ID+"-"+view.ID,
					"Open "+fallback(view.Title, view.ID), func() {
						s.openPluginView(entry, view)
					})
			}
			for _, webView := range entry.Manifest.Contributes.WebViews {
				webView := webView
				if webView.Location != "main" {
					continue
				}
				secondaryAction(c, p, "plugin-open-webview-"+entry.ID+"-"+webView.ID,
					"Open "+fallback(webView.Title, webView.ID), func() {
						s.openPluginWebView(entry, webView)
					})
			}
		})
	})
}

func pluginPermissionsLine(manifest *pluginManifest) string {
	workspace := fallback(manifest.Capabilities.Workspace, "none")
	files := fallback(manifest.Capabilities.Files, "none")
	network := "off"
	if manifest.Capabilities.Network {
		network = "on"
	}
	process := fallback(manifest.Capabilities.Process, "none")
	return fmt.Sprintf("workspace %s · files %s · network %s · process %s",
		workspace, files, network, process)
}

func pluginEnforcementLine(runtime string) string {
	if runtime == "node" {
		return "Node permission model + host checks"
	}
	return "Host checks only · process unsandboxed"
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
	s.showNotice("Running " + title + "…")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), pluginLifecycleTimeout+pluginCommandTimeout)
		defer cancel()
		result, err := s.plugins.invokeCommand(ctx, entry, command.ID, args)
		s.postUI(func() {
			if err != nil {
				s.showNotice(title + " failed: " + summarizeLine(err.Error(), 200))
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
				s.showNotice(name + " stopped with an error: " + summarizeLine(err.Error(), 200))
				return
			}
			s.showNotice(name + " deactivated and its process exited.")
		})
	}()
}

func pluginRuntimeLabel(state pluginRuntimeState) string {
	return strings.ToUpper(string(state))
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
		return "REGISTERED"
	case pluginInvalidManifest:
		return "INVALID"
	case pluginIncompatible:
		return "INCOMPATIBLE"
	case pluginDuplicateID:
		return "DUPLICATE ID"
	case pluginDuplicateContribution:
		return "CONFLICT"
	default:
		return "UNKNOWN"
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
	appendPart := func(count int, singular, plural string) {
		if count == 0 {
			return
		}
		label := plural
		if count == 1 {
			label = singular
		}
		parts = append(parts, fmt.Sprintf("%d %s", count, label))
	}
	appendPart(len(manifest.Contributes.Navigation), "navigation", "navigation")
	appendPart(len(manifest.Contributes.Commands), "command", "commands")
	appendPart(len(manifest.Contributes.Views), "native view", "native views")
	appendPart(len(manifest.Contributes.TaskPanels), "task panel", "task panels")
	appendPart(len(manifest.Contributes.Settings), "setting", "settings")
	appendPart(len(manifest.Contributes.WebViews), "WebView", "WebViews")
	appendPart(len(manifest.Contributes.Automations), "automation", "automations")
	if len(parts) == 0 {
		return "No contributions declared"
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
				pageHeader(c, p, "AUTOMATE", "Automations",
					"Plugin-registered automations and Loops. Declared rules are validated and displayed; P7 runs them manually.")

				ui.Row(c).Wrap().AlignItems(ui.Center).GapX(9).GapY(7).Children(func() {
					statusPill(c, p, fmt.Sprintf("%d REGISTERED", len(items)), p.accent)
					statusPill(c, p, fmt.Sprintf("%d PAUSED", paused), p.muted)
					ui.Text(c, "Timed triggers arrive in a later phase; runs are manual in P7.").
						FontSize(fontSmall).TextColor(p.muted)
				})

				if s.automations != nil {
					for _, warning := range s.automations.warningsSnapshot() {
						ui.Text(c, warning).FontSize(fontCaption).TextColor(p.danger)
					}
				}

				sectionLabel(c, p, "REGISTERED AUTOMATIONS")
				if len(items) == 0 {
					ui.Column(c).FillWidth().Gap(7).Padding(18).Radius(11).
						Border(1, p.border).Background(p.surface).Children(func() {
						ui.Text(c, "No automation contributions").FontSize(fontBody).
							FontWeight(650).TextColor(p.text)
						ui.Row(c).AlignItems(ui.Center).Children(func() {
							ui.Text(c, "Plugins declare ").FontSize(fontSmall).TextColor(p.muted)
							ui.Text(c, "contributes.automations").Font("monospace").
								FontSize(fontSmall).TextColor(p.text)
							ui.Text(c, " in plugin.json.").FontSize(fontSmall).TextColor(p.muted)
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
	description := fallback(contribution.Description, "No description declared.")
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
			statusPill(c, p, fmt.Sprintf("FAILURES %d/%d",
				state.ConsecutiveFailures, automationMaxConsecutiveFailures),
				automationFailureTone(p, state.ConsecutiveFailures))
			if state.LastRunAt != "" {
				ui.Text(c, "Last run "+automationLocalTimestamp(state.LastRunAt)+" · "+
					fallback(strings.ToUpper(state.LastState), "UNKNOWN")).
					FontSize(fontCaption).TextColor(p.muted)
			} else {
				ui.Text(c, "No runs yet").FontSize(fontCaption).TextColor(p.muted)
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
				Cursor(ui.CursorPointer).Label("Run now").Disabled(runDisabled)
			if runButton.Hovered() && !runDisabled {
				runButton.Background(p.surfaceHover)
			}
			runButton.Children(func() {
				ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, icon("play")).Size(11, 11).
						TextColor(automationActionTone(p, runDisabled))
					ui.Text(c, "Run now").FontSize(fontSmall).FontWeight(600).
						TextColor(automationActionTone(p, runDisabled))
				})
			})
			if runButton.Clicked() && !runDisabled {
				s.runAutomation(entry, contribution)
			}
			if state.Enabled {
				secondaryAction(c, p, "automation-pause-"+key, "Pause", func() {
					s.setAutomationPaused(key, true)
				})
			} else {
				secondaryAction(c, p, "automation-resume-"+key, "Resume", func() {
					s.setAutomationPaused(key, false)
				})
			}
		})
		if len(runs) > 0 {
			ui.Divider(c).MarginY(2)
			sectionLabel(c, p, "RECENT RUNS")
			for _, run := range runs {
				s.automationRunRow(c, p, run)
			}
		}
	})
}

func automationKindLabel(contribution pluginAutomationContribution) string {
	switch contribution.Kind {
	case "scheduledLoop":
		return "Scheduled loop · " + automationRuleLabel(contribution.Rule)
	case "sessionLoop":
		return "Session loop · " + automationRuleLabel(contribution.Rule)
	case "once":
		return "Once · " + automationRuleLabel(contribution.Rule)
	default:
		return "Manual"
	}
}

func automationRuleLabel(rule *pluginAutomationRule) string {
	if rule == nil {
		return "No rule"
	}
	switch rule.Type {
	case "interval":
		return fmt.Sprintf("every %d min", rule.Minutes)
	case "cron":
		return "cron " + rule.Cron
	case "at":
		return rule.At
	case "afterSettled":
		return "after settled"
	default:
		return rule.Type
	}
}

func automationStatusLabel(p palette, state automationStoredState, running bool) (string, ui.Color) {
	if running {
		return "RUNNING", p.accent
	}
	if !state.Enabled {
		if state.AutoPausedReason != "" {
			return "AUTO-PAUSED", p.danger
		}
		return "PAUSED", p.muted
	}
	return "READY", p.success
}

func automationPausedReason(reason string) string {
	switch reason {
	case "consecutiveFailures":
		return "5 consecutive failures"
	case "appRestarted":
		return "App restarted"
	default:
		return reason
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
		return "running"
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
	detail := run.Result
	if run.ErrorCode != "" || run.Error != "" {
		detail = strings.TrimSpace(strings.Join(nonEmpty(run.ErrorCode, run.Error), " · "))
	}
	if detail == "" {
		detail = "No result summary"
	}
	startedAt := run.StartedAt
	if parsed, err := time.Parse(time.RFC3339Nano, run.StartedAt); err == nil {
		startedAt = parsed.Local().Format("15:04:05")
	}
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icon(iconName)).Size(12, 12).TextColor(tone)
		ui.Text(c, fmt.Sprintf("%s · %s · %s · %s", strings.ToUpper(string(run.State)),
			run.Trigger, startedAt, automationRunDuration(run))).
			Font("monospace").FontSize(fontCaption).TextColor(tone).Grow(1).SingleLine()
		ui.Text(c, summarizeLine(detail, 150)).FontSize(fontCaption).TextColor(p.muted).
			MaxWidth(380).SingleLine()
	})
}

func (s *appState) runAutomation(entry pluginRegistryEntry, contribution pluginAutomationContribution) {
	if s.automations == nil || s.plugins == nil {
		s.showNotice("Automation runtime is unavailable.")
		return
	}
	key := automationKey(entry.ID, contribution.ID)
	snapshot := s.automations.snapshotFor(key, 0)
	if !snapshot.State.Enabled {
		s.showNotice("Resume the automation before running it.")
		return
	}
	if snapshot.Running {
		s.showNotice("This automation is already running.")
		return
	}
	record, err := s.automations.beginRun(key, entry.ID, contribution.ID, contribution.Command)
	if err != nil {
		s.showNotice("Could not start automation: " + summarizeLine(err.Error(), 200))
		return
	}
	if s.window != nil {
		s.window.Invalidate()
	}
	s.showNotice("Running " + fallback(contribution.Title, contribution.ID) + "…")
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
			errorSummary = invokeErr.Error()
		} else {
			resultSummary = pluginResultSummary(result)
		}
		autoPaused, finishErr := s.automations.finishRun(key, record.RunID, outcome,
			errorSummary, resultSummary)
		s.postUI(func() {
			if finishErr != nil {
				s.showNotice("Automation finished, but its run record could not be saved: " +
					summarizeLine(finishErr.Error(), 200))
			} else {
				title := fallback(contribution.Title, contribution.ID)
				switch outcome {
				case automationOutcomeSuccess:
					s.showNotice(title + " · " + fallback(resultSummary, "Command completed"))
				case automationOutcomeTimeout:
					s.showNotice(title + " timed out after " + fmt.Sprintf("%d", timeoutMinutes) +
						" minute(s).")
				case automationOutcomeStopped:
					s.showNotice(title + " stopped because its plugin was deactivated.")
				default:
					s.showNotice(title + " failed: " + summarizeLine(errorSummary, 200))
				}
				if autoPaused {
					s.showNotice(fmt.Sprintf("%s paused after %d consecutive failures.",
						title, automationMaxConsecutiveFailures))
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
		s.showNotice("Automation state could not be saved: " + summarizeLine(err.Error(), 200))
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
				pageHeader(c, p, "PREFERENCES", "Settings", "Theme and external runtime controls for the native BYOP workbench.")

				settingsCard(c, p, "Appearance", "System, Light, and Dark share the Onevra semantic colors and native text scale.", func() {
					ui.Row(c).Gap(8).Wrap().Children(func() {
						s.themeButton(c, p, themeSystem, "monitor", "System")
						s.themeButton(c, p, themeLight, "sun", "Light")
						s.themeButton(c, p, themeDark, "moon", "Dark")
					})
				})

				settingsCard(c, p, "External Pi CLI", "BYOP starts a separately installed Pi process and uses its versioned JSONL RPC protocol. It does not replace Pi with an embedded SDK.", func() {
					ui.Row(c).Gap(9).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Size(8, 8).Radius(4).Background(s.piTone(p))
						ui.Text(c, s.piConnectionLabel()).FontSize(fontSmall).TextColor(p.text).Grow(1)
						label := "Connect"
						if s.piConn == piConnected {
							label = "Reconnect"
						}
						secondaryAction(c, p, "settings-pi-connect", label, func() {
							s.reconnectPi()
						})
					})
					ui.Column(c).Gap(8).Children(func() {
						settingsValue(c, p, "CLI path", s.piPathLabel())
						settingsValue(c, p, "Version", fallback(s.piVersion, "Pending handshake"))
						settingsValue(c, p, "Model", s.modelSettingsLabel())
						settingsValue(c, p, "Session", s.sessionLabel())
						settingsValue(c, p, "Run state", s.runLabel()+" · "+s.runDetail())
					})
					if s.piError != "" {
						ui.Text(c, s.piError).FontSize(fontCaption).LineHeight(1.4).TextColor(p.danger)
					}
				})

				sectionLabel(c, p, "PLUGIN SETTINGS")
				s.pluginSettingsSection(c, p)

				settingsCard(c, p, "Workbench preferences", "Layout, external Pi, and plugin platform status for this native workbench.", func() {
					ui.Column(c).Gap(8).Children(func() {
						settingsValue(c, p, "Native runtime", "MyGo · fixed baseline")
						settingsValue(c, p, "RPC transport", "Strict JSONL over stdin/stdout")
						settingsValue(c, p, "Pi ownership", "Sessions, models, credentials, extensions")
						settingsValue(c, p, "Plugin registry", fmt.Sprintf("%d registered · %d contribution(s)",
							registry.registeredCount(), registry.contributionCount()))
						settingsValue(c, p, "Plugin runtime", "Native + Node · native and WebView surfaces")
						settingsValue(c, p, "WebView boundary", "byop-plugin · strict CSP · locked navigation")
					})
				})

				settingsCard(c, p, "About", "Build You Own Pi is a multi-plugin workbench shell for an externally installed Pi CLI.", func() {
					ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
						brandMark(c, p, 38)
						ui.Column(c).Gap(1).Children(func() {
							ui.Text(c, appShort).FontSize(fontTitle).FontWeight(650).TextColor(p.text)
							ui.Text(c, "P7 automations · 2026-10-21").FontSize(fontCaption).TextColor(p.muted)
						})
					})
				})
			})
		})
	})
}

func (s *appState) sessionLabel() string {
	if s.piSessionID == "" {
		return "Not assigned"
	}
	if s.piSessionFile == "" {
		return s.piSessionID + " · ephemeral"
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
		model = fallback(s.piModel, "Pi default")
	}
	level := fallback(s.piThinkingLevel, "off")
	return model + " · thinking " + level
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
