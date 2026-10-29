package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

func (s *appState) pluginWebViewPage(c *ui.Context, p palette) {
	pluginID := s.pluginViewPluginID
	viewID := s.pluginViewID
	entry, view, ok := s.registeredPluginWebView(pluginID, viewID)
	if !ok {
		s.pluginViewUnavailable(c, p, pluginID, viewID)
		return
	}
	if s.pluginRestarting != nil && s.pluginRestarting[entry.ID] {
		s.pluginWebViewRestarting(c, p, entry, view)
		return
	}

	snapshot, ok := s.pluginWebViewSnapshot(pluginID, viewID)
	if !ok || snapshot.webView == nil || snapshot.webView.IsDestroyed() ||
		snapshot.status == pluginWebViewFailed {
		s.pluginWebViewFallback(c, p, entry, view, snapshot.error)
		return
	}

	ui.Column(c).FillWidth().Grow(1).MinHeight(0).MinWidth(0).Children(func() {
		s.pluginWebViewHeader(c, p, entry, view)
		ui.Row(c).FillWidth().Grow(1).Padding(16).Background(p.background).
			Children(func() {
				ui.WebView(c, snapshot.webView).FillWidth().Grow(1).Radius(
					runtimeTokens.Dimension.Radius["overlay"].Value,
				)
			})
	})
}

func (s *appState) pluginWebViewRestarting(
	c *ui.Context,
	p palette,
	entry pluginRegistryEntry,
	view pluginWebViewContribution,
) {
	label := textArgs("pluginsRestarting", map[string]any{
		"name": fallback(entry.Name, entry.ID),
	})
	ui.Column(c).Key("plugin-webview-restarting").Label("plugin-webview-restarting").
		FillWidth().Grow(1).MinHeight(0).MinWidth(0).Center().Gap(8).
		Tooltip(fallback(view.Title, view.ID)).Children(func() {
		ui.Icon(c, icon("clock")).Size(18, 18).TextColor(p.muted)
		ui.Text(c, label).FontSize(fontBody).FontWeight(600).TextColor(p.text)
	})
}

func (s *appState) pluginWebViewHeader(
	c *ui.Context,
	p palette,
	entry pluginRegistryEntry,
	view pluginWebViewContribution,
) {
	title := fallback(view.Title, view.ID)
	pluginName := fallback(entry.Name, entry.ID)
	sameName := strings.EqualFold(strings.TrimSpace(pluginName), strings.TrimSpace(title))
	runtime := pluginRuntimeStatus{State: pluginStateInactive}
	if s.plugins != nil {
		runtime = s.plugins.status(entry.ID)
	}
	restarting := s.pluginRestarting != nil && s.pluginRestarting[entry.ID]
	restartDisabled := restarting || runtime.State == pluginStateStarting ||
		runtime.State == pluginStateRunning || runtime.State == pluginStateStopping
	ui.Row(c).FillWidth().MinHeight(64).Padding(12, 16).
		BorderWidth(0, 0, runtimeTokens.Dimension.Border.Value, 0).
		BorderColor(p.border).Background(p.surface).AlignItems(ui.Center).
		Gap(12).Children(func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(fontTitle).FontWeight(650).TextColor(p.text)
			if !sameName {
				ui.Text(c, pluginName).FontSize(fontCaption).TextColor(p.muted)
			}
		})
		if s.pluginPiRefreshRequired {
			pluginWebViewStatusPill(c, p, text("pluginsPiRefreshRequired"), p.warning)
			pluginSecondaryAction(c, p, "plugin-webview-pi-refresh-"+entry.ID+"-"+view.ID,
				text("pluginsPiRefresh"), s.refreshPiForPluginRuntime)
		} else {
			pluginWebViewStatusPill(c, p, text("pluginsWebViewIsolated"), p.success)
		}
		pluginSecondaryActionDisabled(c, p, "plugin-webview-restart-"+entry.ID+"-"+view.ID,
			text("pluginsRestart"), restartDisabled, func() {
				s.restartPlugin(entry)
			})
		pluginSecondaryAction(c, p, "plugin-webview-reload-"+entry.ID+"-"+view.ID,
			text("pluginsWebViewReload"), func() {
				s.recreatePluginWebView(entry.ID, view.ID)
			})
		pluginSecondaryAction(c, p, "plugin-webview-back-"+entry.ID+"-"+view.ID,
			text("pluginsWebViewBack"), s.leavePluginWebView)
	})
}

func (s *appState) pluginWebViewFallback(
	c *ui.Context,
	p palette,
	entry pluginRegistryEntry,
	view pluginWebViewContribution,
	message string,
) {
	if message == "" {
		message = text("pluginsWebViewUnavailableFallback")
	}
	title := fallback(view.Title, view.ID)
	ui.Scroll(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Justify(ui.Center).Padding(24).Children(func() {
			ui.Column(c).Grow(1).MaxWidth(880).Gap(16).Children(func() {
				pluginPageHeader(c, p, fallback(entry.Name, entry.ID), title, "")
				ui.Column(c).FillWidth().Gap(12).Padding(16).Radius(
					runtimeTokens.Dimension.Radius["overlay"].Value,
				).Border(runtimeTokens.Dimension.Border.Value, p.warning).
					Background(p.surface).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
						ui.Icon(c, icon("info")).Size(16, 16).TextColor(p.warning)
						ui.Text(c, text("pluginsWebViewUnavailable")).FontSize(fontBody).
							FontWeight(650).
							TextColor(p.warningText)
					})
					ui.Text(c, message).Selectable().FontSize(fontBody).LineHeight(1.5).
						TextColor(p.text)
					ui.Row(c).Gap(8).Children(func() {
						pluginSecondaryAction(c, p, "plugin-webview-fallback-restart-"+entry.ID+"-"+view.ID,
							text("pluginsRestart"), func() {
								s.restartPlugin(entry)
							})
						pluginSecondaryAction(c, p, "plugin-webview-fallback-reload-"+entry.ID+"-"+view.ID,
							text("pluginsWebViewReload"), func() {
								s.recreatePluginWebView(entry.ID, view.ID)
							})
						pluginSecondaryAction(c, p, "plugin-webview-fallback-back-"+entry.ID+"-"+view.ID,
							text("pluginsWebViewBack"), s.leavePluginWebView)
					})
				})
			})
		})
	})
}

func pluginWebViewStatusPill(c *ui.Context, p palette, label string, tone ui.Color) {
	ui.Row(c).Padding(4, 8).Radius(
		runtimeTokens.Dimension.Radius["chip"].Value,
	).Border(runtimeTokens.Dimension.Border.Value, p.border).
		Background(p.background).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Background(tone)
		ui.Text(c, label).FontSize(fontCaption).FontWeight(650).TextColor(p.muted)
	})
}

func (s *appState) leavePluginWebView() {
	s.page = pagePlugins
	s.pluginViewPluginID = ""
	s.pluginViewID = ""
	s.pluginViewKind = ""
	s.pluginViewLoading = false
	s.pluginViewError = ""
	s.notice = ""
}
