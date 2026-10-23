package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

type pluginNavigationItem struct {
	entry      pluginRegistryEntry
	nav        pluginNavigationContribution
	nativeView *pluginViewContribution
	webView    *pluginWebViewContribution
}

type pluginViewKind string

const (
	pluginViewNative  pluginViewKind = "native"
	pluginViewWebView pluginViewKind = "webview"
)

func (s *appState) pluginNavigationItems() []pluginNavigationItem {
	var items []pluginNavigationItem
	for _, entry := range s.pluginRegistrySnapshot().Entries {
		entry = pluginLocalizedEntry(entry)
		if entry.Status != pluginRegistered || entry.Manifest == nil {
			continue
		}
		for _, navigation := range entry.Manifest.Contributes.Navigation {
			if navigation.View == "" {
				continue
			}
			if view, ok := pluginViewByID(entry.Manifest, navigation.View); ok &&
				view.Location == "main" {
				items = append(items, pluginNavigationItem{
					entry:      entry,
					nav:        navigation,
					nativeView: &view,
				})
			} else if webView, ok := pluginWebViewContributionByID(entry.Manifest, navigation.View); ok &&
				webView.Location == "main" {
				items = append(items, pluginNavigationItem{
					entry:   entry,
					nav:     navigation,
					webView: &webView,
				})
			}
		}
	}
	return items
}

func (s *appState) openPluginView(entry pluginRegistryEntry, view pluginViewContribution) {
	if entry.Status != pluginRegistered || entry.Manifest == nil || view.Location != "main" {
		return
	}
	s.pluginDetailsOpenID = ""
	s.page = pagePluginView
	s.pluginViewPluginID = entry.ID
	s.pluginViewID = view.ID
	s.pluginViewKind = pluginViewNative
	s.notice = ""
	s.loadPluginViewByID(entry.ID, view.ID)
}

func (s *appState) openPluginNavigation(entry pluginRegistryEntry, item pluginNavigationItem) {
	switch {
	case item.nativeView != nil:
		s.openPluginView(entry, *item.nativeView)
	case item.webView != nil:
		s.openPluginWebView(entry, *item.webView)
	}
}

func (s *appState) loadPluginViewByID(pluginID, viewID string) {
	entry, ok := s.registeredPluginEntry(pluginID)
	if !ok {
		s.page = pagePlugins
		return
	}
	if _, ok := pluginViewByID(entry.Manifest, viewID); !ok {
		s.page = pagePlugins
		return
	}
	s.pluginViewEpoch++
	epoch := s.pluginViewEpoch
	s.pluginViewLoading = true
	s.pluginViewError = ""
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), pluginLifecycleTimeout)
		defer cancel()
		model, err := s.plugins.renderView(ctx, entry, viewID)
		s.postUI(func() {
			if epoch != s.pluginViewEpoch || s.page != pagePluginView ||
				s.pluginViewPluginID != pluginID || s.pluginViewID != viewID ||
				s.pluginViewKind != pluginViewNative {
				return
			}
			s.pluginViewLoading = false
			if err != nil {
				s.pluginViewError = uiErrorText(err, text("errorPluginViewRender"))
				return
			}
			s.pluginViewModels[pluginViewCacheKey(pluginID, viewID)] = model
		})
	}()
}

func (s *appState) pluginViewStillRegistered() bool {
	entry, ok := s.registeredPluginEntry(s.pluginViewPluginID)
	if !ok || entry.Manifest == nil {
		return false
	}
	if s.pluginViewKind == pluginViewWebView {
		_, ok = pluginWebViewContributionByID(entry.Manifest, s.pluginViewID)
		return ok
	}
	if s.pluginViewKind != pluginViewNative {
		return false
	}
	_, ok = pluginViewByID(entry.Manifest, s.pluginViewID)
	return ok
}

func (s *appState) registeredPluginEntry(pluginID string) (pluginRegistryEntry, bool) {
	for _, entry := range s.pluginRegistrySnapshot().Entries {
		if entry.ID == pluginID && entry.Status == pluginRegistered {
			return pluginLocalizedEntry(entry), true
		}
	}
	return pluginRegistryEntry{}, false
}

func pluginViewCacheKey(pluginID, viewID string) string {
	return pluginID + "\x00" + viewID
}

func (s *appState) pluginViewPage(c *ui.Context, p palette) {
	if s.pluginViewKind == pluginViewWebView {
		s.pluginWebViewPage(c, p)
		return
	}
	pluginID := s.pluginViewPluginID
	viewID := s.pluginViewID
	entry, ok := s.registeredPluginEntry(pluginID)
	if !ok {
		s.pluginViewUnavailable(c, p, pluginID, viewID)
		return
	}
	view, ok := pluginViewByID(entry.Manifest, viewID)
	if !ok || view.Location != "main" {
		s.pluginViewUnavailable(c, p, pluginID, viewID)
		return
	}
	model := s.pluginViewModels[pluginViewCacheKey(entry.ID, view.ID)]
	title := pluginLocalizedText(entry.Manifest, pluginViewTitle(view, model))
	ui.Scroll(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Justify(ui.Center).Padding(30, 28, 38, 28).Children(func() {
			ui.Column(c).Grow(1).MaxWidth(880).Gap(22).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.End).Gap(16).Children(func() {
					ui.Column(c).Grow(1).Children(func() {
						pageHeader(c, p, strings.ToUpper(entry.Name), title,
							fallback(entry.Description, text("pluginsViewDefaultDescription")))
					})
					secondaryAction(c, p, "plugin-view-refresh-"+entry.ID+"-"+view.ID,
						text("pluginsViewRefresh"), func() {
							s.loadPluginViewByID(entry.ID, view.ID)
						})
					if s.plugins.status(entry.ID).State == pluginStateActive {
						secondaryAction(c, p, "plugin-view-stop-"+entry.ID,
							text("pluginsDeactivate"), func() {
								s.deactivatePlugin(entry)
							})
					}
				})
				switch {
				case s.pluginViewLoading:
					s.pluginViewLoadingState(c, p)
				case s.pluginViewError != "":
					s.pluginViewErrorState(c, p, entry, view)
				default:
					s.pluginViewBlocks(c, p, entry, model.Blocks)
				}
			})
		})
	})
}

func (s *appState) pluginViewUnavailable(
	c *ui.Context,
	p palette,
	pluginID string,
	viewID string,
) {
	s.postUI(func() {
		if s.page != pagePluginView || s.pluginViewPluginID != pluginID || s.pluginViewID != viewID {
			return
		}
		s.page = pagePlugins
		s.pluginViewPluginID = ""
		s.pluginViewID = ""
		s.pluginViewKind = ""
		s.pluginViewLoading = false
		s.pluginViewError = ""
	})
	ui.Scroll(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Justify(ui.Center).Padding(30, 28, 38, 28).Children(func() {
			ui.Column(c).Grow(1).MaxWidth(880).Gap(14).Children(func() {
				pageHeader(c, p, text("pluginsViewUnavailableSection"),
					text("pluginsViewUnavailableTitle"), text("pluginsViewUnavailableDescription"))
			})
		})
	})
}

func (s *appState) pluginViewLoadingState(c *ui.Context, p palette) {
	ui.Row(c).FillWidth().Padding(16).Radius(12).Border(1, p.border).
		Background(p.surface).AlignItems(ui.Center).Gap(9).Children(func() {
		ui.Icon(c, icon("clock")).Size(15, 15).TextColor(p.accent)
		ui.Text(c, text("pluginsViewRendering")).FontSize(fontBody).TextColor(p.muted)
	})
}

func (s *appState) pluginViewErrorState(
	c *ui.Context,
	p palette,
	entry pluginRegistryEntry,
	view pluginViewContribution,
) {
	ui.Column(c).FillWidth().Gap(10).Padding(16).Radius(12).
		Border(1, p.border).Background(p.surface).Children(func() {
		ui.Text(c, text("pluginsViewFailed")).FontSize(fontBody).FontWeight(650).
			TextColor(p.danger)
		ui.Text(c, s.pluginViewError).FontSize(fontSmall).LineHeight(1.45).TextColor(p.muted)
		secondaryAction(c, p, "plugin-view-retry-"+entry.ID+"-"+view.ID,
			text("pluginsViewRetry"), func() {
				s.loadPluginViewByID(entry.ID, view.ID)
			})
	})
}

func (s *appState) pluginViewBlocks(
	c *ui.Context,
	p palette,
	entry pluginRegistryEntry,
	blocks []pluginViewBlock,
) {
	if len(blocks) == 0 {
		ui.Text(c, text("pluginsViewEmpty")).FontSize(fontBody).TextColor(p.muted)
		return
	}
	ui.Column(c).FillWidth().Gap(14).Children(func() {
		for index, block := range blocks {
			block.Text = pluginLocalizedText(entry.Manifest, block.Text)
			switch block.Type {
			case "heading":
				ui.Text(c, block.Text).FontSize(fontTitle).FontWeight(650).TextColor(p.text)
			case "text":
				ui.Text(c, block.Text).Selectable().FontSize(fontBody).LineHeight(1.55).
					TextColor(p.text)
			case "notice":
				tone := pluginBlockTone(p, block.Tone)
				ui.Row(c).FillWidth().Gap(9).Padding(12).Radius(9).
					Border(1, p.border).Background(p.surface).Children(func() {
					ui.Box(c).Width(3).MinHeight(22).Radius(2).Background(tone)
					ui.Text(c, block.Text).FontSize(fontBody).LineHeight(1.45).
						TextColor(p.text).Grow(1)
				})
			case "keyValue":
				ui.Column(c).FillWidth().Gap(2).Children(func() {
					for _, item := range block.Items {
						item.Label = pluginLocalizedText(entry.Manifest, item.Label)
						item.Value = pluginLocalizedText(entry.Manifest, item.Value)
						settingsValue(c, p, item.Label, item.Value)
					}
				})
			case "list":
				ui.Column(c).FillWidth().Gap(7).Children(func() {
					for _, item := range block.Items {
						item.Title = pluginLocalizedText(entry.Manifest, item.Title)
						item.Detail = pluginLocalizedText(entry.Manifest, item.Detail)
						ui.Column(c).FillWidth().Gap(3).Padding(11).Radius(9).
							Border(1, p.border).Background(p.surface).Children(func() {
							if item.Title != "" {
								ui.Text(c, item.Title).FontSize(fontBody).FontWeight(600).
									TextColor(p.text)
							}
							if item.Detail != "" {
								ui.Text(c, item.Detail).FontSize(fontSmall).LineHeight(1.45).
									TextColor(p.muted)
							}
						})
					}
				})
			case "code":
				ui.Column(c).FillWidth().Padding(12).Radius(9).Border(1, p.border).
					Background(p.surface).Children(func() {
					ui.Text(c, block.Text).Selectable().Font("monospace").FontSize(fontSmall).
						LineHeight(1.45).TextColor(p.text)
				})
			case "button":
				s.pluginViewButton(c, p, entry, index, block)
			default:
				ui.Text(c, text("pluginsViewUnsupportedBlock")).FontSize(fontSmall).
					TextColor(p.muted)
			}
		}
	})
}

func (s *appState) pluginViewButton(
	c *ui.Context,
	p palette,
	entry pluginRegistryEntry,
	index int,
	block pluginViewBlock,
) {
	declared := pluginDeclaresCommand(entry.Manifest, block.Command)
	label := fallback(pluginLocalizedText(entry.Manifest, block.Label),
		fallback(block.Command, text("pluginsCommandFallback")))
	key := fmt.Sprintf("plugin-view-button-%s-%d", entry.ID, index)
	btn := ui.ButtonBase(c.Key(key)).Padding(6, 10).Radius(7).Border(1, p.border).
		Background(p.background).Cursor(ui.CursorPointer).Label(label)
	if !declared {
		btn.Disabled(true).Tooltip(text("pluginsUndeclaredCommand"))
	} else {
		btn.Tooltip(block.Command)
		if btn.Hovered() {
			btn.Background(p.surfaceHover)
		}
	}
	btn.Children(func() {
		ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon("play")).Size(11, 11).TextColor(p.accent)
			ui.Text(c, label).FontSize(fontSmall).FontWeight(600).TextColor(p.text)
		})
	})
	if declared && btn.Clicked() {
		var args map[string]any
		if len(block.Args) > 0 {
			_ = json.Unmarshal(block.Args, &args)
		}
		if args == nil {
			args = map[string]any{}
		}
		command, _ := pluginCommandByID(entry.Manifest, block.Command)
		s.runPluginCommandWithArgs(entry, command, args)
	}
}

func pluginCommandByID(manifest *pluginManifest, id string) (pluginCommandContribution, bool) {
	for _, command := range manifest.Contributes.Commands {
		if command.ID == id {
			return command, true
		}
	}
	return pluginCommandContribution{}, false
}

func pluginBlockTone(p palette, tone string) ui.Color {
	switch tone {
	case "success":
		return p.success
	case "warning":
		return p.warning
	case "danger":
		return p.danger
	default:
		return p.accent
	}
}
