package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

type navItem struct {
	id    pageID
	label string
	icon  string
}

var navItems = []navItem{
	{pageChat, "Chat", "chat"},
	{pagePlugins, "Plugins", "plugins"},
	{pageLibrary, "Library", "library"},
	{pageDocument, "Document", "document"},
	{pageAutomations, "Automations", "automations"},
}

func (s *appState) view(c *ui.Context) {
	t, p := nativeTheme(currentAppearanceDark())
	c.SetTheme(t)
	c.Root().Background(p.background)

	ui.Column(c).Fill().Background(p.background).Children(func() {
		s.titleBar(c, p)
		ui.Row(c).Grow(1).FillWidth().AlignItems(ui.Stretch).Children(func() {
			s.navigation(c, p)
			s.main(c, p)
			if s.tasksOpen {
				s.taskPanel(c, p)
			}
		})
	})
}

func (s *appState) titleBar(c *ui.Context, p palette) {
	bar := c.TitleBar()
	height := max(bar.Height, float32(52))
	start := max(bar.Left+12, float32(78))
	if s.navCollapsed && bar.Left < 60 {
		start = max(start, 72)
	}

	ui.Row(c).Height(height).FillWidth().Padding(0, 14, 0, start).
		AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.border).
		Background(p.background).DragWindow().Children(func() {
		ui.Row(c).Gap(9).AlignItems(ui.Center).Children(func() {
			brandMark(c, p, 30)
			ui.Column(c).Gap(0).Children(func() {
				ui.Text(c, appShort).FontSize(fontUILarge).FontWeight(650).TextColor(p.text)
				ui.Text(c, "Build You Own Pi").FontSize(fontCaption).TextColor(p.muted)
			})
		})

		ui.Spacer(c)

		statusPill(c, p, "P4.1 · PLUGIN LIFECYCLE", p.accent)
		ui.Spacer(c)

		iconButton(c, p, "nav-toggle", "panel-left", "Toggle navigation", s.navCollapsed, func() {
			s.navCollapsed = !s.navCollapsed
		})
		iconButton(c, p, "tasks-toggle", "panel-right", "Toggle task panel", s.tasksOpen, func() {
			s.tasksOpen = !s.tasksOpen
		})
	})
}

func brandMark(c *ui.Context, p palette, size float32) ui.Element {
	box := ui.Row(c).Size(size, size).Radius(9).Background(p.accent).
		Center().Label("Build You Own Pi")
	box.Children(func() {
		ui.Icon(c, icon("pi")).Size(size*0.58, size*0.58).TextColor(p.accentText)
	})
	return box
}

func (s *appState) navigation(c *ui.Context, p palette) {
	width := float32(200)
	if s.navCollapsed {
		width = 56
	}

	ui.Column(c).Width(width).FillHeight().Shrink(0).Padding(12, 8).
		BorderWidth(0, 1, 0, 0).BorderColor(p.border).Background(p.background).
		Children(func() {
			if !s.navCollapsed {
				ui.Text(c, "WORKSPACE").FontSize(fontCaption).FontWeight(650).TextColor(p.muted).
					Padding(2, 10, 8, 10)
			}
			ui.Column(c).Gap(3).Children(func() {
				for _, item := range navItems {
					s.navButton(c, p, item)
				}
			})
			ui.Spacer(c)
			ui.Divider(c).MarginY(8)
			s.navButton(c, p, navItem{pageSettings, "Settings", "settings"})
			ui.Spacer(c)
			s.integrationStatus(c, p)
		})
}

func (s *appState) navButton(c *ui.Context, p palette, item navItem) {
	selected := s.page == item.id
	bg := ui.Color{}
	text := p.muted
	if selected {
		bg = p.surfaceHover
		text = p.text
	}
	btn := ui.ButtonBase(c.Key("nav-"+item.id)).Height(38).FillWidth().Padding(0, 10).
		Radius(8).Background(bg).Cursor(ui.CursorPointer).Label(item.label)
	if s.navCollapsed {
		btn.Tooltip(item.label)
		btn.Justify(ui.Center)
	} else {
		btn.Justify(ui.Start)
	}
	if !selected && btn.Hovered() {
		btn.Background(p.surfaceHover)
	}
	btn.Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			// Keep a fixed optical slot so every Lucide path shares the same
			// left edge, including expanded and collapsed navigation states.
			ui.Row(c).Size(20, 20).Shrink(0).Center().Children(func() {
				ic := ui.Icon(c, icon(item.icon)).Size(17, 17).TextColor(text)
				if selected {
					ic.TextColor(p.accent)
				}
			})
			if !s.navCollapsed {
				ui.Text(c, item.label).FontSize(fontBody).FontWeight(550).TextColor(text)
			}
		})
	})
	if selected {
		btn.Draw(func(painter *ui.Painter, r ui.Rect) {
			painter.Fill(ui.Rect{X: 0, Y: (r.H - 18) / 2, W: 3, H: 18}, p.accent, 2)
		})
	}
	if btn.Clicked() {
		s.setPage(item.id)
	}
}

func (s *appState) integrationStatus(c *ui.Context, p palette) {
	if s.navCollapsed {
		box := ui.ButtonBase(c.Key("pi-connection-icon")).Height(36).Width(36).Radius(8).Center().
			Label(s.piConnectionLabel()).Tooltip(s.piConnectionLabel()).Cursor(ui.CursorPointer)
		box.Children(func() {
			ui.Row(c).Size(20, 20).Center().Children(func() {
				ui.Box(c).Size(8, 8).Radius(4).Background(s.piTone(p))
			})
		})
		if box.Clicked() && s.piConn != piConnected {
			s.reconnectPi()
		}
		return
	}
	box := ui.ButtonBase(c.Key("pi-connection")).FillWidth().Column().AlignItems(ui.Start).
		Gap(4).Padding(10, 8).Radius(8).Background(p.surface).
		Label(s.piConnectionLabel()).Tooltip("Reconnect external Pi CLI").Cursor(ui.CursorPointer)
	if box.Hovered() {
		box.Background(p.surfaceHover)
	}
	box.Children(func() {
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(7, 7).Radius(4).Background(s.piTone(p))
			ui.Text(c, "Pi CLI").FontSize(fontSmall).FontWeight(600).TextColor(p.text)
		})
		ui.Text(c, s.piSecondaryLabel()).FontSize(fontCaption).TextColor(p.muted).SingleLine()
	})
	if box.Clicked() && s.piConn != piConnected {
		s.reconnectPi()
	}
}

func (s *appState) main(c *ui.Context, p palette) {
	ui.Column(c).Grow(1).MinWidth(0).FillHeight().Background(p.background).Children(func() {
		if s.notice != "" {
			s.noticeBanner(c, p)
		}
		switch s.page {
		case pagePlugins:
			s.pluginsPage(c, p)
		case pageLibrary:
			s.libraryPage(c, p)
		case pageDocument:
			s.documentPage(c, p)
		case pageAutomations:
			s.automationsPage(c, p)
		case pageSettings:
			s.settingsPage(c, p)
		default:
			s.chatPage(c, p)
		}
	})
}

func (s *appState) noticeBanner(c *ui.Context, p palette) {
	ui.Row(c).FillWidth().Padding(9, 14).Gap(10).AlignItems(ui.Center).
		Background(p.surfaceHover).BorderWidth(0, 0, 1, 0).BorderColor(p.border).
		Children(func() {
			ui.Icon(c, icon("info")).Size(16, 16).TextColor(p.accent)
			ui.Text(c, s.notice).FontSize(fontBody).TextColor(p.text).Grow(1)
			iconButton(c, p, "notice-close", "x", "Dismiss notice", false, func() {
				s.notice = ""
			})
		})
}

func (s *appState) taskPanel(c *ui.Context, p palette) {
	ui.Column(c).Width(320).MinWidth(230).FillHeight().Shrink(0).
		BorderWidth(0, 0, 0, 1).BorderColor(p.border).Background(p.surface).
		Children(func() {
			ui.Row(c).Height(52).FillWidth().Padding(0, 14).AlignItems(ui.Center).
				BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
				ui.Column(c).Gap(1).Grow(1).Children(func() {
					ui.Text(c, "Tasks").FontSize(fontBodyLarge).FontWeight(650).TextColor(p.text)
					ui.Text(c, "Run activity and background work").FontSize(fontCaption).TextColor(p.muted)
				})
				iconButton(c, p, "tasks-close", "x", "Close task panel", false, func() {
					s.tasksOpen = false
				})
			})

			ui.Scroll(c).Grow(1).Padding(12).Children(func() {
				ui.Column(c).Gap(9).Children(func() {
					s.taskCard(c, p, "Pi JSONL RPC", s.piSecondaryLabel(), s.connectionStateLabel(), s.piTone(p))
					s.taskCard(c, p, "Current run", s.currentRunTaskDetail(), s.runLabel(), s.runTone(p))
					s.taskCard(c, p, "Runtime diagnostics", s.runtimeDiagnosticDetail(), s.runtimeDiagnosticLabel(), s.runtimeDiagnosticTone(p))
					s.taskCard(c, p, "Plugin registry", s.pluginRegistryTaskDetail(), s.pluginRegistryTaskLabel(), s.pluginRegistryTaskTone(p))
				})
			})

			ui.Row(c).Padding(12, 14).Gap(8).AlignItems(ui.Center).
				BorderWidth(1, 0, 0, 0).BorderColor(p.border).Children(func() {
				ui.Icon(c, icon("info")).Size(14, 14).TextColor(p.muted)
				ui.Text(c, "Pi owns sessions, credentials, models, and extensions").FontSize(fontCaption).TextColor(p.muted)
			})
		})
}

func (s *appState) pluginRegistryTaskDetail() string {
	registry := s.pluginRegistry
	active := 0
	if s.plugins != nil {
		active = s.plugins.activeCount()
	}
	return fmt.Sprintf("%d registered · %d active · %d contribution(s) · %d issue(s)",
		registry.registeredCount(), active, registry.contributionCount(), registry.problemCount())
}

func (s *appState) pluginRegistryTaskLabel() string {
	registry := s.pluginRegistry
	switch {
	case registry.problemCount() > 0:
		return "ATTENTION"
	case registry.registeredCount() > 0:
		return "REGISTERED"
	default:
		return "EMPTY"
	}
}

func (s *appState) pluginRegistryTaskTone(p palette) ui.Color {
	switch s.pluginRegistryTaskLabel() {
	case "ATTENTION":
		return p.warning
	case "REGISTERED":
		return p.success
	default:
		return p.muted
	}
}

func (s *appState) taskCard(c *ui.Context, p palette, title, detail, status string, statusColor ui.Color) {
	ui.Column(c).FillWidth().Gap(7).Padding(12).Radius(10).
		Border(1, p.border).Background(p.background).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).FontSize(fontBody).FontWeight(600).TextColor(p.text).Grow(1)
			ui.Box(c).Size(7, 7).Radius(4).Background(statusColor)
		})
		ui.Text(c, detail).FontSize(fontSmall).TextColor(p.muted)
		ui.Text(c, status).FontSize(fontCaption).FontWeight(600).TextColor(statusColor)
	})
}

func pageHeader(c *ui.Context, p palette, eyebrow, title, description string) {
	ui.Column(c).FillWidth().Gap(5).Children(func() {
		if eyebrow != "" {
			ui.Text(c, eyebrow).FontSize(fontCaption).FontWeight(700).TextColor(p.accent)
		}
		ui.Text(c, title).FontSize(fontPageTitle).FontWeight(650).TextColor(p.text)
		ui.Text(c, description).FontSize(fontBody).LineHeight(1.45).TextColor(p.muted).MaxWidth(680)
	})
}

func statusPill(c *ui.Context, p palette, label string, ccolor ui.Color) {
	ui.Row(c).Padding(4, 9).Radius(999).Border(1, p.border).
		Background(p.surface).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Background(ccolor)
		ui.Text(c, label).FontSize(fontMicro).FontWeight(700).TextColor(p.muted)
	})
}

func iconButton(c *ui.Context, p palette, key, iconName, label string, selected bool, click func()) {
	bg := ui.Color{}
	fg := p.muted
	if selected {
		bg = p.surfaceHover
		fg = p.accent
	}
	btn := ui.ButtonBase(c.Key(key)).Size(34, 34).Radius(8).Background(bg).
		Center().Cursor(ui.CursorPointer).Label(label).Tooltip(label)
	if !selected && btn.Hovered() {
		btn.Background(p.surfaceHover)
	}
	btn.Children(func() {
		ui.Icon(c, icon(iconName)).Size(17, 17).TextColor(fg)
	})
	if btn.Clicked() {
		click()
	}
}

func sendButton(c *ui.Context, p palette, click func()) {
	btn := ui.ButtonBase(c.Key("send")).Size(34, 34).Radius(8).Background(p.accent).
		Center().Cursor(ui.CursorPointer).Label("Send message").Tooltip("Send to Pi")
	if btn.Hovered() {
		btn.Background(p.accentHover)
	}
	btn.Children(func() {
		ui.Icon(c, icon("send")).Size(16, 16).TextColor(p.accentText)
	})
	if btn.Clicked() {
		click()
	}
}

func stopButton(c *ui.Context, p palette, click func()) {
	btn := ui.ButtonBase(c.Key("stop")).Size(34, 34).Radius(8).Background(p.danger).
		Center().Cursor(ui.CursorPointer).Label("Stop current Pi run").Tooltip("Clear queue and abort")
	btn.Children(func() {
		ui.Icon(c, icon("stop")).Size(15, 15).TextColor(p.accentText)
	})
	if btn.Clicked() {
		click()
	}
}

func textButton(c *ui.Context, p palette, key, label string, selected bool, click func()) {
	bg := ui.Color{}
	fg := p.muted
	if selected {
		bg = p.text
		fg = p.background
	}
	btn := ui.ButtonBase(c.Key(key)).Padding(6, 11).Radius(7).Background(bg).
		Cursor(ui.CursorPointer).Label(label)
	if !selected && btn.Hovered() {
		btn.Background(p.surfaceHover)
	}
	btn.Children(func() {
		ui.Text(c, label).FontSize(fontSmall).FontWeight(600).TextColor(fg)
	})
	if btn.Clicked() {
		click()
	}
}

func primaryAction(c *ui.Context, p palette, key, label string, iconName string, click func()) {
	btn := ui.ButtonBase(c.Key(key)).Padding(8, 13).Radius(8).Background(p.accent).
		Cursor(ui.CursorPointer).Label(label)
	if btn.Hovered() {
		btn.Background(p.accentHover)
	}
	btn.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon(iconName)).Size(14, 14).TextColor(p.accentText)
			ui.Text(c, label).FontSize(fontSmall).FontWeight(650).TextColor(p.accentText)
		})
	})
	if btn.Clicked() {
		click()
	}
}

func secondaryAction(c *ui.Context, p palette, key, label string, click func()) {
	btn := ui.ButtonBase(c.Key(key)).Padding(7, 11).Radius(7).Border(1, p.border).
		Background(p.surface).Cursor(ui.CursorPointer).Label(label)
	if btn.Hovered() {
		btn.Background(p.surfaceHover)
	}
	btn.Children(func() {
		ui.Text(c, label).FontSize(fontSmall).FontWeight(600).TextColor(p.text)
	})
	if btn.Clicked() {
		click()
	}
}

func searchField(c *ui.Context, p palette, key string, value *string, placeholder string) {
	ui.Row(c).Height(36).Gap(7).Padding(0, 10).Radius(8).Border(1, p.border).
		Background(p.surface).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icon("search")).Size(15, 15).TextColor(p.muted)
		ui.TextInputBase(c.Key(key), value).Grow(1).Placeholder(placeholder).
			FontSize(fontBody).TextColor(p.text).Background(ui.Color{}).
			Border(0, ui.Color{}).Padding(0).Label(placeholder)
	})
}

func sectionLabel(c *ui.Context, p palette, text string) {
	ui.Text(c, text).FontSize(fontCaption).FontWeight(700).TextColor(p.muted)
}

func (s *appState) piConnectionLabel() string {
	switch s.piConn {
	case piConnected:
		return "Pi CLI connected"
	case piConnecting:
		return "Pi CLI connecting"
	case piConnectionErr:
		return "Pi CLI connection error"
	default:
		return "Pi CLI disconnected"
	}
}

func (s *appState) piCompactLabel() string {
	label := "Pi CLI · " + s.connectionStateLabel()
	if s.piVersion != "" && s.piConn == piConnected {
		label = "Pi CLI " + s.piVersion + " · connected"
	}
	return label
}

func (s *appState) piSecondaryLabel() string {
	switch s.piConn {
	case piConnected:
		if s.piModel != "" {
			return "Connected · " + s.piModel
		}
		return "Connected · RPC ready"
	case piConnecting:
		return "Starting external process"
	case piConnectionErr:
		return fallback(s.piError, "Connection failed")
	default:
		return "Click to connect external CLI"
	}
}

func (s *appState) piDiagnostic() string {
	switch s.piConn {
	case piConnected:
		return "Connected to an external Pi CLI through JSONL RPC."
	case piConnecting:
		return "Starting Pi and waiting for a get_state response."
	case piConnectionErr:
		return "Connection failed: " + fallback(s.piError, "unknown error")
	default:
		return "Pi is not connected. Open Settings or click the sidebar status to retry."
	}
}

func (s *appState) connectionStateLabel() string {
	switch s.piConn {
	case piConnected:
		return "connected"
	case piConnecting:
		return "connecting"
	case piConnectionErr:
		return "error"
	default:
		return "disconnected"
	}
}

func (s *appState) piTone(p palette) ui.Color {
	switch s.piConn {
	case piConnected:
		return p.success
	case piConnecting:
		return p.warning
	case piConnectionErr:
		return p.danger
	default:
		return p.muted
	}
}

func (s *appState) runLabel() string {
	switch s.run {
	case runSubmitting:
		return "SENDING"
	case runStreaming:
		return "PI RUNNING"
	case runStopping:
		return "STOPPING"
	case runCompleted:
		return "COMPLETE"
	case runStopped:
		return "STOPPED"
	case runInterrupted:
		return "INTERRUPTED"
	case runFailed:
		return "FAILED"
	default:
		if s.piConn == piConnected {
			return "READY"
		}
		return "OFFLINE"
	}
}

func (s *appState) runDetail() string {
	switch s.run {
	case runSubmitting:
		return "Prompt sent; waiting for acceptance"
	case runStreaming:
		return "Streaming Pi events"
	case runStopping:
		return "clear_queue followed by abort"
	case runCompleted:
		return "agent_settled received"
	case runStopped:
		return "Run aborted"
	case runInterrupted:
		return "Process exited before agent_settled"
	case runFailed:
		return fallback(s.piError, "Prompt rejected")
	default:
		if s.piConn == piConnected {
			return "No active agent run"
		}
		return "External Pi CLI is offline"
	}
}

func (s *appState) runTone(p palette) ui.Color {
	switch s.run {
	case runSubmitting, runStreaming, runStopping:
		return p.warning
	case runCompleted:
		return p.success
	case runStopped:
		return p.muted
	case runInterrupted, runFailed:
		return p.danger
	default:
		if s.piConn == piConnected {
			return p.success
		}
		return p.muted
	}
}

func (s *appState) runtimeDiagnosticDetail() string {
	parts := make([]string, 0, 5)
	queued := s.queueSteeringCount + s.queueFollowUpCount
	if queued > 0 {
		parts = append(parts, fmt.Sprintf("Queued %d steering and %d follow-up message(s)", s.queueSteeringCount, s.queueFollowUpCount))
	}
	if s.retrying {
		retry := "Retrying"
		if s.retryAttempt > 0 {
			retry += fmt.Sprintf(" %d", s.retryAttempt)
			if s.retryMax > 0 {
				retry += fmt.Sprintf("/%d", s.retryMax)
			}
		}
		if s.retryError != "" {
			retry += " · " + summarizeLine(s.retryError, 120)
		}
		parts = append(parts, retry)
	} else if s.retryError != "" {
		parts = append(parts, "Retry failed · "+summarizeLine(s.retryError, 120))
	}
	if s.compacting {
		parts = append(parts, "Compacting context · "+fallback(s.compactionReason, "automatic"))
	} else if s.compactionResult != "" {
		parts = append(parts, s.compactionResult)
	}
	if s.compactionError != "" {
		parts = append(parts, "Compaction error · "+summarizeLine(s.compactionError, 120))
	}
	if s.extensionError != "" {
		source := extensionDisplayName(s.extensionPath)
		if source != "" && s.extensionEvent != "" {
			source += " · " + s.extensionEvent
		}
		parts = append(parts, "Extension error · "+fallback(source, "unknown extension")+" · "+summarizeLine(s.extensionError, 120))
	}
	if len(parts) == 0 {
		return "No queue, compaction, retry, or extension events in this run"
	}
	return strings.Join(parts, "\n")
}

func (s *appState) runtimeDiagnosticLabel() string {
	switch {
	case s.extensionError != "" || s.compactionError != "" || (s.retryError != "" && !s.retrying):
		return "ATTENTION"
	case s.retrying || s.compacting || s.queueSteeringCount+s.queueFollowUpCount > 0:
		return "ACTIVE"
	default:
		return "CLEAR"
	}
}

func (s *appState) runtimeDiagnosticTone(p palette) ui.Color {
	switch s.runtimeDiagnosticLabel() {
	case "ATTENTION":
		return p.danger
	case "ACTIVE":
		return p.warning
	default:
		return p.success
	}
}

func extensionDisplayName(path string) string {
	path = strings.TrimRight(path, `/\`)
	if index := strings.LastIndexAny(path, `/\`); index >= 0 {
		return path[index+1:]
	}
	return path
}

func compactionResultLabel(event piEvent) string {
	if event.CompactError != "" {
		return ""
	}
	if event.CompactAborted {
		return "Context compaction cancelled"
	}
	if event.CompactBefore <= 0 && event.CompactAfter <= 0 {
		if event.CompactSummary != "" {
			return "Context compacted"
		}
		return ""
	}
	return fmt.Sprintf("Context compacted · %s → %s tokens",
		formatTokenCount(event.CompactBefore), formatTokenCount(event.CompactAfter))
}

func formatTokenCount(tokens int64) string {
	switch {
	case tokens >= 1_000_000:
		return fmt.Sprintf("%.1fm", float64(tokens)/1_000_000)
	case tokens >= 1_000:
		return fmt.Sprintf("%.1fk", float64(tokens)/1_000)
	default:
		return fmt.Sprintf("%d", tokens)
	}
}
