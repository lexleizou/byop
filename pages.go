package main

import (
	"context"
	"fmt"
	"strings"

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
	ui.Scroll(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Justify(ui.Center).Padding(30, 28, 38, 28).Children(func() {
			ui.Column(c).Grow(1).MaxWidth(880).Gap(22).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.End).Gap(16).Children(func() {
					ui.Column(c).Grow(1).Children(func() {
						pageHeader(c, p, "AUTOMATE", "Automations", "Scheduled and event-driven work routed through installed plugins and the external Pi runtime.")
					})
					primaryAction(c, p, "automation-create", "New automation", "plus", func() {
						s.showNotice("Automation creation is not connected yet.")
					})
				})

				ui.Row(c).Wrap().Gap(12).Children(func() {
					s.automationCard(c, p, "Morning brief", "Weekdays · 08:30", "Chat", true)
					s.automationCard(c, p, "Repository digest", "On demand", "Git plugin", false)
				})

				ui.Column(c).FillWidth().Gap(14).Padding(18).Radius(12).
					Border(1, p.border).Background(p.surface).Children(func() {
					ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, icon("clock")).Size(18, 18).TextColor(p.accent)
						ui.Text(c, "No scheduler connected").FontSize(fontBody).FontWeight(650).TextColor(p.text)
					})
					ui.Text(c, "The P0 inventory found only scheduler.lock. BYOP will present no job as runnable until a plugin runtime and permission model exist.").
						FontSize(fontSmall).LineHeight(1.5).TextColor(p.muted)
				})
			})
		})
	})
}

func (s *appState) automationCard(c *ui.Context, p palette, title, schedule, owner string, enabled bool) {
	ui.Column(c).Width(280).Grow(1).MinWidth(220).Gap(12).Padding(14).Radius(11).
		Border(1, p.border).Background(p.surface).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Row(c).Size(34, 34).Radius(9).Background(p.surfaceHover).Center().Children(func() {
				ui.Icon(c, icon("play")).Size(15, 15).TextColor(p.accent)
			})
			ui.Spacer(c)
			if enabled {
				statusPill(c, p, "PREVIEW ON", p.warning)
			} else {
				statusPill(c, p, "OFF", p.muted)
			}
		})
		ui.Text(c, title).FontSize(fontBody).FontWeight(650).TextColor(p.text)
		ui.Text(c, schedule).FontSize(fontSmall).TextColor(p.muted)
		ui.Text(c, "Owner · "+owner).FontSize(fontCaption).TextColor(p.accent)
	})
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
							ui.Text(c, "P6 sessions · 2026-10-21").FontSize(fontCaption).TextColor(p.muted)
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
