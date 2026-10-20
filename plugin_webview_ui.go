package main

import (
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

	snapshot, ok := s.pluginWebViewSnapshot(pluginID, viewID)
	if !ok || snapshot.webView == nil || snapshot.webView.IsDestroyed() ||
		snapshot.status == pluginWebViewFailed {
		s.pluginWebViewFallback(c, p, entry, view, snapshot.error)
		return
	}

	ui.Column(c).Fill().Children(func() {
		s.pluginWebViewHeader(c, p, entry, view)
		ui.Row(c).FillWidth().Grow(1).Padding(12).Children(func() {
			ui.WebView(c, snapshot.webView).FillWidth().Grow(1).Radius(9)
		})
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
	ui.Row(c).FillWidth().Height(58).Padding(0, 16).
		BorderWidth(0, 0, 1, 0).BorderColor(p.border).
		Background(p.surface).AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(fontBodyLarge).FontWeight(650).TextColor(p.text)
			ui.Text(c, pluginName).FontSize(fontBody).TextColor(p.muted)
		})
		statusPill(c, p, "ISOLATED", p.success)
		secondaryAction(c, p, "plugin-webview-reload-"+entry.ID+"-"+view.ID,
			"Reload", func() {
				s.recreatePluginWebView(entry.ID, view.ID)
			})
		secondaryAction(c, p, "plugin-webview-back-"+entry.ID+"-"+view.ID,
			"Back to Plugins", s.leavePluginWebView)
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
		message = "The isolated WebView is unavailable."
	}
	title := fallback(view.Title, view.ID)
	ui.Scroll(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Justify(ui.Center).Padding(30, 28, 38, 28).Children(func() {
			ui.Column(c).Grow(1).MaxWidth(880).Gap(18).Children(func() {
				pageHeader(c, p, "ISOLATED WEBVIEW", title,
					fallback(entry.Name, entry.ID)+" · byop-plugin scheme · strict CSP")
				ui.Column(c).FillWidth().Gap(12).Padding(18).Radius(12).
					Border(1, p.warning).Background(p.surface).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(9).Children(func() {
						ui.Icon(c, icon("info")).Size(17, 17).TextColor(p.warning)
						ui.Text(c, "WebView unavailable").FontSize(fontBody).FontWeight(650).
							TextColor(p.warning)
					})
					ui.Text(c, message).Selectable().FontSize(fontBody).LineHeight(1.5).
						TextColor(p.text)
					ui.Row(c).Gap(8).Children(func() {
						secondaryAction(c, p, "plugin-webview-fallback-reload-"+entry.ID+"-"+view.ID,
							"Reload", func() {
								s.recreatePluginWebView(entry.ID, view.ID)
							})
						secondaryAction(c, p, "plugin-webview-fallback-back-"+entry.ID+"-"+view.ID,
							"Back to Plugins", s.leavePluginWebView)
					})
				})
			})
		})
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
