package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

const (
	// pluginChatMinWebViewWidth is the narrowest the plugin web view may get
	// while the chat panel sits beside it. Deciding by window width alone put
	// a 430 DIP web view next to the panel on a 1000 DIP window, which broke
	// the table layout; the panel floats instead once the web view would drop
	// below this (P32 §2.3).
	pluginChatMinWebViewWidth float32 = 640
	// pluginChatDividerWidth is the draggable hit area of the divider; the
	// painted separator stays 1px inside it.
	pluginChatDividerWidth float32 = 8
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
		if !ok {
			// The creation runs just outside this frame, so the first render
			// shows the header with an empty body instead of the unavailable
			// card that would flash for one frame.
			ui.Column(c).FillWidth().Grow(1).MinHeight(0).MinWidth(0).Children(func() {
				s.pluginWebViewHeader(c, p, entry, view)
			})
			return
		}
		s.pluginWebViewFallback(c, p, entry, view, snapshot.error)
		return
	}

	windowWidth, _ := c.Size()
	panelWidth := normalizePluginChatWidth(s.pluginChatWidth)
	// Side by side only while the web view keeps a usable width; otherwise the
	// panel floats above it so the table keeps its own viewport.
	mainWidth := windowWidth - s.navigationWidth(windowWidth)
	if s.navCollapsed {
		mainWidth = windowWidth - navCollapsedWidth
	}
	chatOverlay := s.pluginChatOpen &&
		mainWidth-panelWidth-pluginChatDividerWidth < pluginChatMinWebViewWidth

	ui.Column(c).FillWidth().Grow(1).MinHeight(0).MinWidth(0).Children(func() {
		s.pluginWebViewHeader(c, p, entry, view)
		ui.Row(c).FillWidth().Grow(1).MinHeight(0).Padding(16).Background(p.background).
			Children(func() {
				ui.WebView(c, snapshot.webView).FillWidth().Grow(1).MinWidth(0).Radius(
					runtimeTokens.Dimension.Radius["overlay"].Value,
				)
				if s.pluginChatOpen && !chatOverlay {
					s.pluginChatResizeHandle(c, p)
					s.pluginChatPanelCard(c, p, panelWidth, false)
				}
				if chatOverlay {
					// Same content, floated over the web view so a narrow
					// window keeps the table full width underneath (P32 §2.3).
					layer := ui.Row(c).Key("plugin-chat-overlay").Absolute().
						Top(0).Right(0).Bottom(0).FillHeight().Shrink(0).MinWidth(0)
					layer.Children(func() {
						s.pluginChatResizeHandle(c, p)
						s.pluginChatPanelCard(c, p, panelWidth, true)
					})
				}
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

// pluginChatResizeHandle is the 8px divider between the web view and the chat
// panel (P32 §6.5): dragging steps the panel width (a left drag widens it),
// a double click resets it to the default 420, and release persists the
// value. The painted separator stays 1px inside the hit area.
func (s *appState) pluginChatResizeHandle(c *ui.Context, p palette) {
	handle := ui.Box(c).Key("plugin-chat-resize").Width(pluginChatDividerWidth).
		FillHeight().Shrink(0).Cursor(ui.CursorResizeEW).
		Label(text("pluginChatPanelResize"))
	dx, _, held := handle.Dragged()
	if held {
		if next := normalizePluginChatWidth(s.pluginChatWidth - dx); next != s.pluginChatWidth {
			s.pluginChatWidth = next
			s.preferences.PluginChatWidth = next
			s.pluginChatDragging = true
		}
	} else {
		if s.pluginChatDragging {
			s.pluginChatDragging = false
			if err := s.persistPluginChatPanel(s.pluginChatOpen, s.pluginChatWidth); err != nil {
				s.showNotice(text("noticePreferenceSaveChatPanel"))
			}
		}
		if handle.DoubleClicked() && s.pluginChatWidth != pluginChatDefaultWidth {
			s.pluginChatWidth = pluginChatDefaultWidth
			s.preferences.PluginChatWidth = pluginChatDefaultWidth
			if err := s.persistPluginChatPanel(s.pluginChatOpen, pluginChatDefaultWidth); err != nil {
				s.showNotice(text("noticePreferenceSaveChatPanel"))
			}
		}
	}
	active := held || handle.Hovered()
	handle.Draw(func(painter *ui.Painter, r ui.Rect) {
		if r.H <= 16 {
			return
		}
		line := p.border
		if active {
			line = p.accent
		}
		painter.Fill(ui.Rect{X: r.X + r.W/2 - .5, Y: r.Y + 8, W: 1, H: r.H - 16}, line, 0)
	})
}

// pluginChatPanelCard is the chat column: a small header, the main
// conversation, and the composer. It is the same s.chat, s.run, draft, queue
// and compression state the Chat page draws — the panel never starts its own
// session or process (P32 §6.5).
func (s *appState) pluginChatPanelCard(c *ui.Context, p palette, width float32, overlay bool) {
	panel := ui.Column(c).Key("plugin-chat-panel").Width(width).FillHeight().
		Shrink(0).MinWidth(0)
	if overlay {
		panel = panel.Radius(runtimeTokens.Dimension.Radius["overlay"].Value).Clip().
			Border(runtimeTokens.Dimension.Border.Value, p.border).
			Background(p.surface).
			Shadow(0, 1, 12, 0, ui.RGBA(0, 0, 0, .14))
	}
	panel.Children(func() {
		s.pluginChatPanelHeader(c, p)
		s.pluginChatPanelConversation(c, p)
	})
}

func (s *appState) pluginChatPanelHeader(c *ui.Context, p palette) {
	ui.Row(c).Key("plugin-chat-header").FillWidth().Shrink(0).MinWidth(0).Height(36).
		Padding(0, 4, 0, 12).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, text("pluginChatPanelTitle")).FontSize(fontCaption).FontWeight(650).
			TextColor(p.muted).Tooltip(text("pluginChatPanelSameSessionHint"))
		if model := s.piHUDModelName(); model != "" {
			ui.Row(c).Padding(2, 6).Radius(runtimeTokens.Dimension.Radius["chip"].Value).
				Border(runtimeTokens.Dimension.Border.Value, p.border).
				Background(p.surface).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
				ui.Box(c).Size(6, 6).Radius(3).Background(p.success)
				ui.Text(c, model).FontSize(fontCaption).FontWeight(650).TextColor(p.muted).
					SingleLine().Ellipsis("…")
			})
		}
		ui.Spacer(c).Grow(1)
		iconButton(c, p, "plugin-chat-collapse", "chevron-right",
			text("pluginChatPanelCollapse"), false, func() {
				s.setPluginChatOpen(false)
			})
	})
}

func (s *appState) pluginChatPanelConversation(c *ui.Context, p palette) {
	state := &s.chatView.conversation
	// The follow flag lives with the Chat page's state on purpose: the two
	// views show one conversation, and only one of them is built per frame.
	follow := state.AtLatest() && !s.chatView.jAgentPaused
	s.chatView.jAgentBuildY = state.Scroll.Y
	if follow {
		state.JumpToLatest()
	}
	viewport := components.ScrollView(c, components.ScrollViewOptions{}).
		Key("plugin-chat-transcript").FillWidth().Grow(1).MinHeight(0).
		TrackScroll(&state.Scroll)
	viewport.Children(func() {
		ui.Column(c).FillWidth().MinWidth(0).Padding(4, 12, 12, 12).
			Children(func() {
				s.transcript(c)
			})
	})
	viewport.TrackScroll(&state.Scroll)
	ui.Box(c).FillWidth().Shrink(0).Padding(0, 12, 8, 12).
		Children(func() { s.chatComposer(c, p) })
}

// applyPluginChatPanel answers host.setChatPanel on the UI thread. The host
// sets the panel to the requested state — never a toggle — and only the
// plugin whose view is on screen may ask (P32 contract §2.1).
func (s *appState) applyPluginChatPanel(pluginID string, open bool) (map[string]any, *pluginRPCError) {
	if s.page != pagePluginView || s.pluginViewPluginID != pluginID {
		return nil, &pluginRPCError{
			Code:    pluginRPCPermissionDenied,
			Message: "permission denied: chat panel is only available from the plugin view",
		}
	}
	s.setPluginChatOpen(open)
	return map[string]any{
		"open":  s.pluginChatOpen,
		"width": float64(s.pluginChatWidth),
	}, nil
}
