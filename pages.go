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

const (
	pluginCardDefaultHeight      float32 = 212
	pluginCardMinWidth           float32 = 320
	pluginCardMaxWidth           float32 = 360
	pluginCardsSingleColumnWidth float32 = pluginCardMaxWidth*2 + 12 + 56
)

func (s *appState) pluginsPage(c *ui.Context, p palette) {
	registry := s.pluginRegistrySnapshot()
	entries := s.filteredPluginEntries()
	windowWidth, _ := c.Size()
	narrowCards := s.chatMainWidth(windowWidth) < pluginCardsSingleColumnWidth
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
					if s.pluginPiRefreshRequired {
						statusPill(c, p, text("pluginsPiRefreshRequired"), p.warning)
						secondaryAction(c, p, "plugin-pi-refresh",
							text("pluginsPiRefresh"), s.refreshPiForPluginRuntime)
					}
					ui.Spacer(c)
					secondaryAction(c, p, "plugin-dir", text("pluginsFolders"), func() {
						s.showNotice(textArgs("pluginsRoots", map[string]any{
							"roots": strings.Join(registry.Roots, "; "),
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
						for index, entry := range entries {
							s.pluginCard(c, p, entry, index, narrowCards)
						}
					})
				}

			})
		})
	})
}

func pluginEntryKey(entry pluginRegistryEntry, index int) string {
	if sourceDirectory := strings.TrimSpace(entry.SourceDirectory); sourceDirectory != "" {
		return sourceDirectory
	}
	return fmt.Sprintf("%s-%d", entry.ID, index)
}

func (s *appState) pluginEntryExists(entryKey string) bool {
	for index, entry := range s.pluginRegistrySnapshot().Entries {
		if pluginEntryKey(entry, index) == entryKey {
			return true
		}
	}
	return false
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
	card := ui.Column(c).FillWidth().Gap(8).Padding(14)
	card = styleRaisedSurface(card, p, 12)
	card.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon("x")).Size(14, 14).TextColor(p.danger)
			ui.Text(c, text("pluginsRegistryScanIssues")).FontSize(fontBody).FontWeight(650).TextColor(p.text)
		})
		for _, issue := range issues {
			ui.Text(c, issue.Path+": "+issue.Message).Font("monospace").FontSize(fontCaption).
				TextColor(p.dangerText).Selectable()
		}
	})
}

func (s *appState) emptyPluginRegistry(c *ui.Context, p palette) {
	message := text("pluginsManifestsNotFound")
	if strings.TrimSpace(s.pluginSearch) != "" {
		message = textArgs("pluginsNoMatch", map[string]any{"query": s.pluginSearch})
	}
	card := ui.Column(c).FillWidth().Gap(10).Padding(18)
	card = styleRaisedSurface(card, p, 12)
	card.Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Size(34, 34).Radius(8).Background(p.surfaceHover).Center().Children(func() {
				ui.Icon(c, icon("package")).Size(17, 17).TextColor(p.muted)
			})
			ui.Column(c).Gap(2).Grow(1).Children(func() {
				ui.Text(c, text("pluginsRegistryEmpty")).FontSize(fontBody).FontWeight(650).TextColor(p.text)
				ui.Text(c, message).FontSize(fontSmall).TextColor(p.muted)
			})
		})
	})
}

type pluginCardActionKind string

const (
	pluginCardActionNone    pluginCardActionKind = ""
	pluginCardActionView    pluginCardActionKind = "view"
	pluginCardActionWebView pluginCardActionKind = "webview"
	pluginCardActionCommand pluginCardActionKind = "command"
)

type pluginCardAction struct {
	Kind  pluginCardActionKind
	ID    string
	Title string
}

func pluginCardPrimaryAction(entry pluginRegistryEntry) pluginCardAction {
	if entry.Status != pluginRegistered || entry.Manifest == nil {
		return pluginCardAction{}
	}
	manifest := entry.Manifest
	for _, view := range manifest.Contributes.Views {
		if view.Location == "main" {
			return pluginCardAction{
				Kind:  pluginCardActionView,
				ID:    view.ID,
				Title: fallback(view.Title, view.ID),
			}
		}
	}
	for _, webView := range manifest.Contributes.WebViews {
		if webView.Location == "main" {
			return pluginCardAction{
				Kind:  pluginCardActionWebView,
				ID:    webView.ID,
				Title: fallback(webView.Title, webView.ID),
			}
		}
	}
	if len(manifest.Contributes.Commands) > 0 {
		command := manifest.Contributes.Commands[0]
		return pluginCardAction{
			Kind:  pluginCardActionCommand,
			ID:    command.ID,
			Title: fallback(command.Title, command.ID),
		}
	}
	return pluginCardAction{}
}

func (s *appState) pluginCardStatusSummary(
	p palette,
	entry pluginRegistryEntry,
	runtime pluginRuntimeStatus,
) (string, ui.Color) {
	if entry.Error != "" {
		return summarizeLine(uiErrorFromText(entry.Error, text("pluginsFailedFallback")), 240),
			p.dangerText
	}
	if runtime.LastError != "" {
		return summarizeLine(uiErrorFromText(runtime.LastError, text("pluginsFailedFallback")), 240),
			p.dangerText
	}
	if runtime.LastResult != "" {
		return pluginRuntimeResultSummary(entry, runtime), p.successText
	}
	if entry.Manifest != nil {
		return textArgs("pluginsVersionRuntime", map[string]any{
			"version": fallback(entry.Version, entry.Manifest.Version),
			"runtime": entry.Manifest.Runtime,
		}), p.muted
	}
	return text("pluginsManifestRejected"), p.dangerText
}

func pluginRuntimeResultSummary(entry pluginRegistryEntry, runtime pluginRuntimeStatus) string {
	command := runtime.LastCommand
	if entry.Manifest != nil {
		if contribution, ok := pluginCommandByID(entry.Manifest, command); ok {
			command = fallback(contribution.Title, contribution.ID)
		}
	}
	if command == "" {
		command = text("pluginsCommandFallback")
	}
	return textArgs("pluginsLastResult", map[string]any{
		"command": command,
		"result":  runtime.LastResult,
	})
}

func (s *appState) pluginCard(
	c *ui.Context,
	p palette,
	entry pluginRegistryEntry,
	index int,
	narrow bool,
) {
	tone := s.pluginStatusTone(p, entry.Status)
	title := fallback(entry.Name, entry.ID)
	description := fallback(entry.Description, text("pluginsNoDescription"))
	contribution := text("pluginsManifestRejected")
	iconName := "plugins"
	if entry.Manifest != nil {
		contribution = pluginContributionSummary(entry.Manifest)
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
	statusSummary, statusTone := s.pluginCardStatusSummary(p, entry, runtime)
	restarting := s.pluginRestarting != nil && s.pluginRestarting[entry.ID]
	busy := runtime.State == pluginStateStarting || runtime.State == pluginStateRunning ||
		runtime.State == pluginStateStopping || restarting
	action := pluginCardPrimaryAction(entry)
	entryKey := pluginEntryKey(entry, index)

	card := ui.Column(c).Key("plugin-card-" + entryKey).
		Label(textArgs("pluginsCardLabel", map[string]any{"name": title})).
		Height(pluginCardDefaultHeight).MinHeight(pluginCardDefaultHeight).
		MaxHeight(pluginCardDefaultHeight).AlignItems(ui.Start).Gap(6).Padding(16).
		Radius(12)
	card = styleRaisedSurface(card, p, 12).Clip()
	if narrow {
		card.FillWidth()
	} else {
		card.Width(pluginCardMaxWidth).MinWidth(pluginCardMinWidth).MaxWidth(pluginCardMaxWidth)
	}
	duration := s.panelAnimationDuration(currentAppearanceReduceMotion())
	if duration > 0 {
		card.Transition(ui.ElementTransition{Duration: duration, Colors: true})
	}
	if card.Hovered() {
		card.Background(p.surfaceHover).Border(runtimeTokens.Dimension.Border.Value, p.muted)
	}
	var detailsButton ui.Element
	card.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Row(c).Size(34, 34).Radius(8).Background(p.surfaceHover).Center().Children(func() {
				ui.Icon(c, iconOr(iconName, "plugins")).Size(17, 17).TextColor(tone)
			})
			ui.Spacer(c)
			statusPill(c, p, pillLabel, pillTone)
		})
		ui.Text(c, title).FontSize(fontBody).FontWeight(650).TextColor(p.text).
			SingleLine().Ellipsis("…")
		ui.Text(c, description).FontSize(fontSmall).LineHeight(1.4).TextColor(p.muted).
			MaxLines(2).Ellipsis("…")
		ui.Text(c, contribution).FontSize(fontCaption).TextColor(p.muted).
			SingleLine().Ellipsis("…")
		ui.Text(c, statusSummary).FontSize(fontCaption).TextColor(statusTone).
			SingleLine().Ellipsis("…")
		ui.Spacer(c)
		ui.Row(c).FillWidth().Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
			switch action.Kind {
			case pluginCardActionView:
				view, ok := pluginViewByID(entry.Manifest, action.ID)
				if ok {
					s.pluginCardPrimaryButton(c, p, "plugin-open-primary-"+entryKey,
						textArgs("pluginsOpen", map[string]any{"title": action.Title}), "chevron-right",
						false, func() {
							s.openPluginView(entry, view)
						})
				}
			case pluginCardActionWebView:
				webView, ok := pluginWebViewContributionByID(entry.Manifest, action.ID)
				if ok {
					s.pluginCardPrimaryButton(c, p, "plugin-open-primary-"+entryKey,
						textArgs("pluginsOpen", map[string]any{"title": action.Title}), "chevron-right",
						false, func() {
							s.openPluginWebView(entry, webView)
						})
				}
			case pluginCardActionCommand:
				command, ok := pluginCommandByID(entry.Manifest, action.ID)
				if ok {
					s.pluginCardPrimaryButton(c, p, "plugin-run-primary-"+entryKey,
						fallback(command.Title, command.ID), "play", busy, func() {
							s.runPluginCommand(entry, command)
						})
				}
			}
			ui.Spacer(c)
			detailsButton = ui.ButtonBase(c.Key("plugin-details-"+entryKey)).
				Padding(6, 9).Radius(8).Background(ui.Color{}).
				Cursor(ui.CursorPointer).Label(textArgs("pluginsDetailsFor", map[string]any{
				"name": title,
			})).Shrink(0)
			if detailsButton.Hovered() {
				detailsButton.Background(p.surfaceHover)
			}
			detailsButton.Children(func() {
				ui.Text(c, text("pluginsDetails")).FontSize(fontSmall).FontWeight(600).
					TextColor(p.text)
			})
			if detailsButton.Clicked() {
				s.pluginDetailsOpenID = entryKey
			}
		})
	})
	if s.pluginDetailsOpenID == entryKey && detailsButton.Valid() {
		open := true
		panel := ui.PopoverBase(c, detailsButton, &open, func(panel ui.Element) {
			s.pluginDetailsPanel(c, p, panel, entry, entryKey, runtime, busy)
		})
		if panel.Dismissed() || !open {
			s.pluginDetailsOpenID = ""
		}
	}
}

func (s *appState) pluginCardPrimaryButton(
	c *ui.Context,
	p palette,
	key, label, iconName string,
	disabled bool,
	click func(),
) {
	btn := ui.ButtonBase(c.Key(key)).Padding(7, 11).Radius(8).Background(p.accent).
		Cursor(ui.CursorPointer).Label(label).Disabled(disabled).
		Shrink(1).MinWidth(0).MaxWidth(pluginCardMaxWidth)
	if btn.Hovered() && !disabled {
		btn.Background(p.accentHover)
	}
	btn.Children(func() {
		ui.Row(c).FillWidth().MinWidth(0).Gap(5).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon(iconName)).Size(13, 13).Shrink(0).TextColor(p.onAccent)
			ui.Text(c, label).FontSize(fontSmall).FontWeight(650).TextColor(p.onAccent).
				Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
		})
	})
	if !disabled && btn.Clicked() {
		click()
	}
}

func (s *appState) pluginDetailsPanel(
	c *ui.Context,
	p palette,
	panel ui.Element,
	entry pluginRegistryEntry,
	entryKey string,
	runtime pluginRuntimeStatus,
	busy bool,
) {
	registered := entry.Status == pluginRegistered && entry.Manifest != nil
	restarting := s.pluginRestarting != nil && s.pluginRestarting[entry.ID]
	busy = busy || restarting
	_, windowHeight := c.Size()
	maxHeight := windowHeight - 80
	if maxHeight > 520 {
		maxHeight = 520
	}
	if maxHeight < 240 {
		maxHeight = 240
	}
	panel.Width(420).MinWidth(360).MaxWidth(420).MaxHeight(maxHeight).Padding(16)
	high := s.highContrastActive(currentAppearanceHighContrast())
	reduceTransparency := currentAppearanceReduceTransparency()
	switch {
	case !p.dark:
		panel = stylePluginDetailsLight(panel, p)
	case pluginDetailsUsesGlass(p.dark, high, reduceTransparency):
		panel = stylePopoverGlass(panel, p, 12)
	default:
		panel = stylePopoverOpaque(panel, p, 12, p.raisedSurface, p.border)
	}
	panel.Children(func() {
		ui.Scroll(c).Fill().Children(func() {
			ui.Column(c).FillWidth().Gap(12).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						ui.Text(c, fallback(entry.Name, entry.ID)).FontSize(fontBody).
							FontWeight(650).TextColor(p.text).SingleLine().Ellipsis("…")
						ui.Text(c, text("pluginsDetailsTitle")).FontSize(fontCaption).
							TextColor(p.muted)
					})
					statusLabel := pluginStatusLabel(entry.Status)
					statusTone := s.pluginStatusTone(p, entry.Status)
					if entry.Status == pluginRegistered && runtime.State != pluginStateInactive {
						statusLabel = pluginRuntimeLabel(runtime.State)
						statusTone = pluginRuntimeTone(p, runtime.State)
					}
					statusPill(c, p, statusLabel, statusTone)
				})

				ui.Column(c).FillWidth().Gap(7).Children(func() {
					pluginDetailRow(c, p, text("pluginsDetailsIdentity"), entry.ID, true)
					pluginDetailRow(c, p, text("pluginsDetailsVersion"),
						fallback(entry.Version, text("pluginsValueNone")), false)
					if entry.Manifest != nil {
						pluginDetailRow(c, p, text("pluginsDetailsPermissions"),
							pluginPermissionsLine(entry.Manifest), false)
					} else {
						pluginDetailRow(c, p, text("pluginsDetailsPermissions"),
							text("pluginsValueNone"), false)
					}
					if registered {
						pluginDetailRow(c, p, text("pluginsDetailsRuntime"),
							fallback(entry.Manifest.Runtime, text("pluginsValueNone")), true)
						pluginDetailRow(c, p, text("pluginsDetailsHostChecks"),
							pluginEnforcementLine(entry.Manifest.Runtime), false)
					}
				})

				if registered && len(entry.Manifest.Contributes.Commands) > 0 {
					pluginDetailsSection(c, p, text("pluginsDetailsCommands"))
					ui.Row(c).Wrap().GapX(6).GapY(6).Children(func() {
						for _, command := range entry.Manifest.Contributes.Commands {
							command := command
							pluginCommandButton(c, p, "plugin-details-cmd-"+entryKey+"-"+command.ID,
								command, busy, func() {
									s.runPluginCommand(entry, command)
								})
						}
					})
				}

				if registered {
					pluginSecondaryActionDisabled(c, p, "plugin-details-restart-"+entryKey,
						text("pluginsRestart"), busy, func() {
							s.restartPlugin(entry)
						})
				}

				if registered && runtime.State == pluginStateActive {
					secondaryAction(c, p, "plugin-details-stop-"+entryKey,
						text("pluginsDeactivate"), func() {
							s.deactivatePlugin(entry)
						})
				}

				if s.pluginPiRefreshRequired {
					pluginDetailBlock(c, p, text("pluginsPiRefreshRequired"),
						text("pluginsPiRefreshRequiredDetail"), false)
					secondaryAction(c, p, "plugin-details-pi-refresh-"+entryKey,
						text("pluginsPiRefresh"), s.refreshPiForPluginRuntime)
				}

				if registered {
					var mainViews, secondaryViews []pluginViewContribution
					for _, view := range entry.Manifest.Contributes.Views {
						if view.Location == "main" {
							mainViews = append(mainViews, view)
						} else {
							secondaryViews = append(secondaryViews, view)
						}
					}
					if len(mainViews) > 0 {
						pluginDetailsSection(c, p, text("pluginsDetailsViews"))
						ui.Row(c).Wrap().GapX(6).GapY(6).Children(func() {
							for _, view := range mainViews {
								view := view
								secondaryAction(c, p, "plugin-details-view-"+entryKey+"-"+view.ID,
									textArgs("pluginsOpen", map[string]any{
										"title": fallback(view.Title, view.ID),
									}), func() {
										s.openPluginView(entry, view)
									})
							}
						})
					}
					if len(secondaryViews) > 0 {
						var titles []string
						for _, view := range secondaryViews {
							titles = append(titles, fallback(view.Title, view.ID))
						}
						pluginDetailBlock(c, p, text("pluginsDetailsSecondaryViews"),
							strings.Join(titles, ", "), false)
					}
					if len(entry.Manifest.Contributes.WebViews) > 0 {
						pluginDetailsSection(c, p, text("pluginsDetailsWebViews"))
						ui.Column(c).FillWidth().Gap(8).Children(func() {
							for _, webView := range entry.Manifest.Contributes.WebViews {
								webView := webView
								ui.Column(c).FillWidth().Gap(4).Padding(10).Radius(8).
									Background(p.background).Children(func() {
									ui.Text(c, fallback(webView.Title, webView.ID)).
										FontSize(fontSmall).FontWeight(600).TextColor(p.text)
									ui.Text(c, textArgs("pluginsWebViewMeta", map[string]any{
										"entry": webView.Entry,
									})).FontSize(fontCaption).LineHeight(1.4).TextColor(p.muted)
									if webView.Location == "main" {
										secondaryAction(c, p,
											"plugin-details-webview-"+entryKey+"-"+webView.ID,
											textArgs("pluginsOpen", map[string]any{
												"title": fallback(webView.Title, webView.ID),
											}), func() {
												s.openPluginWebView(entry, webView)
											})
									}
								})
							}
						})
					}
				}

				if entry.Error != "" {
					pluginDetailBlock(c, p, text("pluginsDetailsRegistrationError"),
						uiErrorText(errors.New(entry.Error), text("pluginsFailedFallback")), false)
				}
				if registered {
					pluginDetailsSection(c, p, text("pluginsDetailsDiagnostics"))
					pluginDetailBlock(c, p, text("pluginsDetailsHostCalls"),
						textArgs("pluginsHostCallCount", map[string]any{"count": runtime.HostCalls}), true)
					if runtime.DeniedCalls > 0 {
						pluginDetailBlock(c, p, text("pluginsDetailsDeniedCalls"),
							textArgs("pluginsDenied", map[string]any{"detail": runtime.LastDenied}), false)
					}
					if runtime.LastResult != "" {
						pluginDetailBlock(c, p, text("pluginsDetailsRecentResult"),
							pluginRuntimeResultSummary(entry, runtime), false)
					}
					if runtime.LastError != "" {
						pluginDetailBlock(c, p, text("pluginsDetailsRecentError"),
							uiErrorText(errors.New(runtime.LastError), text("pluginsFailedFallback")), false)
					}
				}
			})
		})
	})
}

func pluginDetailsSection(c *ui.Context, p palette, label string) {
	ui.Text(c, label).FontSize(fontCaption).FontWeight(700).TextColor(p.muted)
}

func pluginDetailRow(c *ui.Context, p palette, label, value string, mono bool) {
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Start).Children(func() {
		ui.Text(c, label).Width(96).Shrink(0).FontSize(fontCaption).TextColor(p.muted)
		text := ui.Text(c, value).FontSize(fontCaption).TextColor(p.text).Grow(1)
		if mono {
			text.Font("monospace").Selectable()
		}
	})
}

func pluginDetailBlock(c *ui.Context, p palette, label, value string, success bool) {
	ui.Column(c).FillWidth().Gap(3).Padding(9).Radius(8).
		Background(p.background).Children(func() {
		ui.Text(c, label).FontSize(fontCaption).FontWeight(650).TextColor(p.muted)
		tone := p.text
		if success {
			tone = p.successText
		}
		ui.Text(c, value).Selectable().FontSize(fontSmall).LineHeight(1.4).TextColor(tone)
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
	btn := ui.ButtonBase(c.Key(key)).Padding(6, 10).Radius(8).Border(runtimeTokens.Dimension.Border.Value, p.border).
		Background(p.background).Cursor(ui.CursorPointer).Label(label).
		Tooltip(fallback(command.Description, command.ID)).Disabled(busy)
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

func pluginDetailsUsesGlass(dark, high, reduceTransparency bool) bool {
	return dark && !high && !reduceTransparency
}

func stylePluginDetailsLight(element ui.Element, p palette) ui.Element {
	return element.Radius(12).Border(runtimeTokens.Dimension.Border.Value, p.muted).
		Background(p.surface).
		Shadow(0, 8, 24, 0, p.shadowOverlay)
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
			s.showNotice(title + ": " + pluginResultSummary(result))
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
	return strings.Join(parts, "; ")
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
							TextColor(p.dangerText)
					}
				}

				sectionLabel(c, p, text("automationsRegisteredSection"))
				if len(items) == 0 {
					card := ui.Column(c).FillWidth().Gap(7).Padding(18)
					card = styleRaisedSurface(card, p, 12)
					card.Children(func() {
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
	card := ui.Column(c).FillWidth().Gap(12).Padding(16)
	card = styleRaisedSurface(card, p, 12)
	card.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Column(c).Gap(2).Grow(1).Children(func() {
				ui.Text(c, title).FontSize(fontBody).FontWeight(650).TextColor(p.text)
				ui.Text(c, fallback(entry.Name, entry.ID)).FontSize(fontCaption).
					TextColor(p.accentText)
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
				TextColor(p.dangerText)
		}
		ui.Row(c).Wrap().GapX(7).GapY(7).AlignItems(ui.Center).Children(func() {
			runDisabled := running || !state.Enabled
			runButton := ui.ButtonBase(c.Key("automation-run-"+key)).
				Padding(7, 11).Radius(8).Border(runtimeTokens.Dimension.Border.Value, p.border).Background(p.surface).
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
	textTone := p.muted
	iconName := "clock"
	switch run.State {
	case automationRunSucceeded:
		tone = p.success
		textTone = p.successText
		iconName = "check"
	case automationRunFailed, automationRunAborted:
		tone = p.danger
		textTone = p.dangerText
		iconName = "x"
	case automationRunRunning:
		tone = p.accent
		textTone = p.accentText
	}
	detail := pluginResultSummaryLabel(run.Result)
	if run.ErrorCode != "" || run.Error != "" {
		errorText := run.Error
		if run.ErrorCode == "interrupted" {
			errorText = text("automationsErrorInterrupted")
		} else if errorText != "" {
			errorText = uiErrorFromText(errorText, text("automationsFailedFallback"))
		}
		detail = strings.TrimSpace(strings.Join(nonEmpty(automationErrorCodeLabel(run.ErrorCode), errorText), ": "))
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
		ui.Text(c, fmt.Sprintf("%s, %s, %s, %s", automationRunStateLabel(string(run.State)),
			automationTriggerLabel(run.Trigger), startedAt, automationRunDuration(run))).
			Font("monospace").FontSize(fontCaption).TextColor(textTone).Grow(1).SingleLine()
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

func settingsCard(c *ui.Context, p palette, title, description string, body func()) {
	card := ui.Column(c).FillWidth().Gap(13).Padding(16)
	card = styleRaisedSurface(card, p, 12)
	card.Children(func() {
		ui.Column(c).Gap(4).Children(func() {
			ui.Text(c, title).FontSize(p.bodyFont).FontWeight(650).TextColor(p.text)
			ui.Text(c, description).FontSize(p.captionFont).LineHeight(1.45).TextColor(p.muted)
		})
		body()
	})
}

func settingsValue(c *ui.Context, p palette, label, value string) {
	ui.Row(c).FillWidth().Padding(8, 10).Radius(8).Background(p.background).
		AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(p.captionFont).TextColor(p.muted).Width(180)
		ui.Text(c, value).FontSize(p.captionFont).FontWeight(600).TextColor(p.text).Grow(1)
	})
}

func (s *appState) themeButton(c *ui.Context, p palette, mode themeMode, iconName, label string) {
	selected := s.themeMode == mode
	bg := p.background
	fg := p.muted
	border := p.border
	if selected {
		bg = p.surfaceHover
		fg = p.accentText
		border = p.accent
	}
	btn := ui.ButtonBase(c.Key("theme-"+mode)).Padding(8, 12).Radius(8).
		Border(runtimeTokens.Dimension.Border.Value, border).Background(bg).Cursor(ui.CursorPointer).Label(label)
	if btn.Hovered() && !selected {
		btn.Background(p.surfaceHover)
	}
	btn.Children(func() {
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon(iconName)).Size(15, 15).TextColor(fg)
			ui.Text(c, label).FontSize(p.controlFont()).FontWeight(600).TextColor(fg)
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
		fg = p.accentText
		border = p.accent
	}
	btn := ui.ButtonBase(c.Key("language-"+value)).Padding(8, 12).Radius(8).
		Border(runtimeTokens.Dimension.Border.Value, border).Background(bg).Cursor(ui.CursorPointer).Label(label)
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
		fg = p.accentText
		border = p.accent
	}
	btn := ui.ButtonBase(c.Key("follow-up-style-"+string(style))).Padding(8, 12).Radius(8).
		Border(runtimeTokens.Dimension.Border.Value, border).Background(bg).Cursor(ui.CursorPointer).Label(label)
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
				}
				s.choiceButton(c, p, key+"-"+choice.id, choice.iconName, choice.label, selected, choice.click)
			}
		})
	})
}

func (s *appState) choiceButton(c *ui.Context, p palette, key, iconName, label string, selected bool, click func()) {
	bg, fg, border := p.background, p.muted, p.border
	if selected {
		bg, fg, border = p.surfaceHover, p.accentText, p.accent
	}
	button := ui.ButtonBase(c.Key(key)).Padding(8, 12).Radius(8).
		Border(runtimeTokens.Dimension.Border.Value, border).Background(bg).Cursor(ui.CursorPointer).Label(label)
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
