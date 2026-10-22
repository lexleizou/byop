package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

type navItem struct {
	id    pageID
	label string
	icon  string
}

var navItems = []navItem{
	{pageChat, "navChat", "chat"},
	{pagePlugins, "navPlugins", "plugins"},
	{pageAutomations, "navAutomations", "automations"},
}

func (s *appState) view(c *ui.Context) {
	dark := currentAppearanceDark()
	high := s.highContrastActive(currentAppearanceHighContrast())
	s.syncPluginWebViewTheme(dark, high)
	t, p := nativeThemeWithContrast(dark, high)
	c.SetTheme(t)
	c.Root().Background(p.background)
	c.OnShortcut(ui.Cmd, ui.KeyB, func() {
		s.setNavCollapsed(!s.navCollapsed)
	})
	c.OnShortcut(ui.Cmd|ui.Alt, ui.KeyB, func() {
		s.setTasksOpen(!s.tasksOpen)
	})

	ui.Column(c).Fill().Background(p.background).Children(func() {
		s.titleBar(c, p)
		ui.Row(c).Grow(1).FillWidth().AlignItems(ui.Stretch).Children(func() {
			s.navigation(c, p)
			s.main(c, p)
			s.taskPanel(c, p)
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
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			brandMark(c, p, 30)
			ui.Column(c).Gap(0).Children(func() {
				ui.Text(c, appShort).FontSize(fontUILarge).FontWeight(650).TextColor(p.text)
				ui.Text(c, "Build You Own Pi").FontSize(fontCaption).TextColor(p.muted)
			})
		})

		ui.Spacer(c)

		statusPill(c, p, text("headerPhase"), p.accent)
		ui.Spacer(c)

		panelToggleGroup(c, p, func() {
			s.setNavCollapsed(!s.navCollapsed)
		}, func() {
			s.setTasksOpen(!s.tasksOpen)
		}, s.navCollapsed, s.tasksOpen)
	})
}

func panelToggleGroup(c *ui.Context, p palette, toggleNavigation, toggleTasks func(), navigationCollapsed, tasksOpen bool) {
	modifier := "Ctrl"
	if runtime.GOOS == "darwin" {
		modifier = "⌘"
	}
	ui.Row(c).Height(34).Radius(8).Border(1, p.border).Background(p.surface).
		AlignItems(ui.Center).Children(func() {
		panelToggleButton(c, p, "nav-toggle", "panel-left",
			textArgs("titleToggleNavigationShortcut", map[string]any{"shortcut": modifier + "B"}),
			navigationCollapsed, toggleNavigation)
		ui.Box(c).Width(1).Height(18).Shrink(0).Background(p.border)
		taskShortcut := modifier + "+B"
		if runtime.GOOS == "darwin" {
			taskShortcut = modifier + "⌥B"
		} else {
			taskShortcut = "Ctrl+Alt+B"
		}
		panelToggleButton(c, p, "tasks-toggle", "panel-right",
			textArgs("titleToggleTaskPanelShortcut", map[string]any{"shortcut": taskShortcut}),
			tasksOpen, toggleTasks)
	})
}

func panelToggleButton(c *ui.Context, p palette, key, iconName, label string, selected bool, click func()) {
	bg := ui.Color{}
	fg := p.muted
	if selected {
		bg = p.surfaceHover
		fg = p.accent
	}
	btn := ui.ButtonBase(c.Key(key)).Width(36).Height(32).Background(bg).Radius(7).
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

func brandMark(c *ui.Context, p palette, size float32) ui.Element {
	return ui.Icon(c, icon("byop")).Size(size, size).TextColor(p.text).
		Label("Build You Own Pi")
}

func (s *appState) navigation(c *ui.Context, p palette) {
	width := float32(200)
	if s.navCollapsed {
		width = 56
	}

	nav := ui.Column(c)
	duration := s.panelAnimationDuration(currentAppearanceReduceMotion())
	animatedWidth := nav.AnimateWith("navigation-width", width, duration, ui.EaseOut)
	nav.Width(animatedWidth).FillHeight().Shrink(0).Padding(12, 8).
		BorderWidth(0, 1, 0, 0).BorderColor(p.border).Background(p.background).
		Children(func() {
			if !s.navCollapsed {
				ui.Text(c, text("navWorkspace")).FontSize(fontCaption).FontWeight(650).TextColor(p.muted).
					Padding(2, 10, 8, 10)
			}
			ui.Column(c).Gap(3).Children(func() {
				for _, item := range navItems {
					s.navButton(c, p, item)
					if item.id == pageChat && !s.navCollapsed && s.chatCapablePage() {
						s.sessionsNavigation(c, p)
					}
				}
				if s.navCollapsed && s.chatCapablePage() {
					s.collapsedNewSessionButton(c, p)
				}
				pluginItems := s.pluginNavigationItems()
				if len(pluginItems) > 0 {
					if !s.navCollapsed {
						ui.Text(c, text("navPluginsSection")).FontSize(fontCaption).FontWeight(700).
							TextColor(p.muted).Padding(8, 10, 4, 10)
					}
					for _, item := range pluginItems {
						s.pluginNavButton(c, p, item)
					}
				}
			})
			ui.Spacer(c)
			ui.Divider(c).MarginY(8)
			s.navButton(c, p, navItem{pageSettings, "navSettings", "settings"})
			ui.Spacer(c)
			s.integrationStatus(c, p)
		})
}

func (s *appState) pluginNavButton(c *ui.Context, p palette, item pluginNavigationItem) {
	viewID := item.nav.View
	selected := s.page == pagePluginView && s.pluginViewPluginID == item.entry.ID &&
		s.pluginViewID == viewID
	bg := ui.Color{}
	foreground := p.muted
	if selected {
		bg = p.surfaceHover
		foreground = p.text
	}
	label := fallback(item.nav.Title, viewID)
	key := "nav-plugin-" + item.entry.ID + "-" + item.nav.ID
	btn := ui.ButtonBase(c.Key(key)).Height(38).FillWidth().Padding(0, 10).
		Radius(8).Background(bg).Cursor(ui.CursorPointer).Label(label)
	if s.navCollapsed {
		btn.Tooltip(label)
		btn.Justify(ui.Center)
	} else {
		btn.Justify(ui.Start)
	}
	if !selected && btn.Hovered() {
		btn.Background(p.surfaceHover)
	}
	btn.Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Size(20, 20).Shrink(0).Center().Children(func() {
				ic := ui.Icon(c, iconOr(item.nav.Icon, "plugins")).Size(17, 17).TextColor(foreground)
				if selected {
					ic.TextColor(p.accent)
				}
			})
			if !s.navCollapsed {
				ui.Text(c, label).FontSize(fontBody).FontWeight(550).TextColor(foreground)
			}
		})
	})
	if selected {
		btn.Draw(func(painter *ui.Painter, r ui.Rect) {
			painter.Fill(ui.Rect{X: 0, Y: (r.H - 18) / 2, W: 3, H: 18}, p.accent, 2)
		})
	}
	if btn.Clicked() {
		s.openPluginNavigation(item.entry, item)
	}
}

func (s *appState) navButton(c *ui.Context, p palette, item navItem) {
	selected := s.page == item.id
	bg := ui.Color{}
	foreground := p.muted
	if selected {
		bg = p.surfaceHover
		foreground = p.text
	}
	label := text(item.label)
	btn := ui.ButtonBase(c.Key("nav-"+item.id)).Height(38).FillWidth().Padding(0, 10).
		Radius(8).Background(bg).Cursor(ui.CursorPointer).Label(label)
	if s.navCollapsed {
		btn.Tooltip(label)
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
				ic := ui.Icon(c, icon(item.icon)).Size(17, 17).TextColor(foreground)
				if selected {
					ic.TextColor(p.accent)
				}
			})
			if !s.navCollapsed {
				ui.Text(c, label).FontSize(fontBody).FontWeight(550).TextColor(foreground)
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

const sessionDragFormat transfer.Format = "application/vnd.byop.session+json"

type sessionDragPayload struct {
	Key string `json:"key"`
}

func (s *appState) sessionsNavigation(c *ui.Context, p palette) {
	cwd, err := s.sessionWorkspaceCwd()
	if err == nil {
		cwd = canonicalPiPath(cwd)
		if s.piSessionsOrgWorkspace != cwd {
			s.refreshSessionOrganization(time.Now())
		}
	}
	groups := s.piSessionsGroups
	ui.Column(c).Gap(2).FillWidth().Children(func() {
		ui.Row(c).Height(30).FillWidth().Padding(0, 10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, text("navSessions")).FontSize(fontCaption).FontWeight(700).TextColor(p.muted).Grow(1)
			iconButton(c, p, "sessions-new", "plus", text("navNewSession"), false, func() {
				s.newPiSession()
			})
		})
		if len(s.piSessions) == 0 {
			return
		}
		ui.Scroll(c).FillWidth().Gap(3).MaxHeight(18 * 44).Children(func() {
			dragging := s.draggingSessionKey != ""
			if len(groups.pinned) > 0 || dragging {
				s.sessionSection(c, p, "pinned", text("navPinned"), groups.pinned,
					sessionStatePinned, false, true)
			}
			s.sessionSection(c, p, "sessions", text("navRecentSessions"), groups.sessions,
				"", true, true)
			if len(groups.later) > 0 || dragging {
				s.sessionSection(c, p, "later", text("navLater"), groups.later,
					sessionStateLater, false, true)
			}
			if len(groups.archived) > 0 || dragging {
				s.archivedSessionSection(c, p, groups)
			}
		})
	})
}

func (s *appState) sessionSection(c *ui.Context, p palette, key, label string, sessions []piSessionSummary,
	state string, collapsible, showAll bool,
) {
	visible := sessions
	if showAll && !s.piSessionsExpanded && len(visible) > 8 {
		visible = visible[:8]
	}
	// The parent navigation row already names the default Sessions group.
	// Keep its drop area and list, but avoid rendering a duplicate header.
	if key != "sessions" {
		s.sessionSectionHeader(c, p, key, label, state, len(sessions), collapsible)
	}
	s.sessionDropArea(c, p, "sessions-drop-"+key, state, label, len(sessions) == 0, func() {
		for _, summary := range visible {
			s.sessionNavRow(c, p, summary)
		}
	})
	if showAll && len(sessions) > 8 {
		actionLabel := text("navShowAll")
		if s.piSessionsExpanded {
			actionLabel = text("navShowRecent")
		}
		secondarySessionAction(c, p, "sessions-show-all-"+key, actionLabel, func() {
			s.piSessionsExpanded = !s.piSessionsExpanded
		})
	}
}

func (s *appState) archivedSessionSection(c *ui.Context, p palette, groups sessionGroups) {
	s.sessionSectionHeader(c, p, "archived", textArgs("navArchived", map[string]any{
		"count": len(groups.archived),
	}), sessionStateArchived, len(groups.archived), true)
	expanded := s.archivedExpanded || s.draggingSessionKey != ""
	target := float32(0)
	if expanded {
		target = float32(len(groups.archived))*42 + 8
	}
	body := ui.Column(c)
	duration := s.panelAnimationDuration(currentAppearanceReduceMotion())
	animatedHeight := body.AnimateWith("archived-sessions-height", target, duration, ui.EaseOut)
	body.FillWidth().MaxHeight(animatedHeight).Clip().Children(func() {
		if !expanded {
			return
		}
		s.sessionDropArea(c, p, "sessions-drop-archived", sessionStateArchived,
			text("navArchivedAction"), len(groups.archived) == 0, func() {
				for _, summary := range groups.archived {
					s.sessionNavRow(c, p, summary)
				}
			})
	})
}

func (s *appState) sessionSectionHeader(c *ui.Context, p palette, key, label, state string,
	count int, collapsible bool,
) {
	header := ui.ButtonBase(c.Key("session-section-"+key)).Height(28).FillWidth().
		Padding(0, 10).Radius(6).Background(ui.Color{}).Cursor(ui.CursorPointer).Label(label)
	if header.Hovered() {
		header.Background(p.surfaceHover)
	}
	header.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Text(c, label).FontSize(fontCaption).FontWeight(700).TextColor(p.muted).Grow(1)
			if collapsible {
				iconName := "chevron-down"
				if key == "archived" && !s.archivedExpanded {
					iconName = "chevron-right"
				}
				ui.Icon(c, icon(iconName)).Size(13, 13).TextColor(p.muted)
			}
		})
	})
	if _, over := ui.DragOver[sessionDragPayload](header); over {
		header.Border(1, p.accent)
	}
	if drop, ok := ui.Drop[sessionDragPayload](header); ok {
		s.applySessionDrop(drop, state, "")
	}
	opts := sessionDropOptions()
	if _, over := ui.DataDragOver(header, opts); over {
		header.Border(1, p.accent)
	}
	if drop, ok := ui.DropData(header, opts); ok {
		if payload, valid := decodeSessionDrop(drop); valid {
			s.applySessionDrop(payload, state, "")
		}
	}
	if header.Clicked() && key == "archived" {
		s.archivedExpanded = !s.archivedExpanded
	}
}

func (s *appState) sessionDropArea(c *ui.Context, p palette, key, state, label string, empty bool, children func()) {
	area := ui.Column(c).FillWidth().MinHeight(8).Gap(2)
	_, localOver := ui.DragOver[sessionDragPayload](area)
	_, nativeOver := ui.DataDragOver(area, sessionDropOptions())
	over := localOver || nativeOver
	if over {
		area.Border(1, p.accent).Padding(3).Background(p.surface)
	}
	area.Children(func() {
		if over && len(s.piSessions) > 0 && empty {
			s.sessionDropPlaceholder(c, p, label)
		}
		children()
		if over {
			ui.Row(c).FillWidth().Justify(ui.End).Padding(0, 5).Children(func() {
				s.sessionDropBadge(c, p, state)
			})
		}
	})
	if drop, ok := ui.Drop[sessionDragPayload](area); ok {
		s.applySessionDrop(drop, state, "")
	}
	if drop, ok := ui.DropData(area, sessionDropOptions()); ok {
		if payload, valid := decodeSessionDrop(drop); valid {
			s.applySessionDrop(payload, state, "")
		}
	}
}

func (s *appState) sessionDropPlaceholder(c *ui.Context, p palette, label string) {
	ui.Row(c).Height(32).FillWidth().Padding(0, 9).AlignItems(ui.Center).
		Radius(7).Border(1, p.accent).Children(func() {
		ui.Text(c, label).FontSize(fontCaption).TextColor(p.muted)
	})
}

func (s *appState) sessionDropBadge(c *ui.Context, p palette, state string) {
	label := text("sessionMoveToSessions")
	switch state {
	case sessionStatePinned:
		label = text("sessionMoveToPinned")
	case sessionStateLater:
		label = text("sessionMoveToLater")
	case sessionStateArchived:
		label = text("sessionMoveToArchived")
	}
	ui.Row(c).Padding(3, 7).Radius(999).Background(p.accent).Children(func() {
		ui.Text(c, label).FontSize(fontMicro).FontWeight(700).TextColor(p.accentText)
	})
}

func sessionDropOptions() transfer.DropOptions {
	return transfer.DropOptions{
		Formats:    []transfer.Format{sessionDragFormat},
		Operations: transfer.Copy,
	}
}

func decodeSessionDrop(drop transfer.Drop) (sessionDragPayload, bool) {
	data, err := drop.Data.Read(sessionDragFormat)
	if err != nil {
		return sessionDragPayload{}, false
	}
	var payload sessionDragPayload
	if json.Unmarshal(data, &payload) != nil {
		return sessionDragPayload{}, false
	}
	payload.Key = filepath.Clean(strings.TrimSpace(payload.Key))
	return payload, payload.Key != "" && payload.Key != "."
}

func (s *appState) sessionSummaryByKey(key string) (piSessionSummary, bool) {
	for _, summary := range s.piSessions {
		if sessionOrgKey(summary) == key {
			return summary, true
		}
	}
	return piSessionSummary{}, false
}

func (s *appState) applySessionDrop(payload sessionDragPayload, state string, beforeKey string) {
	summary, ok := s.sessionSummaryByKey(payload.Key)
	if !ok {
		return
	}
	entries := s.sessionOrganizationEntries()
	snapshot := cloneSessionOrgEntries(entries)
	now := time.Now()
	if state == sessionStatePinned && beforeKey != "" {
		reorderPinnedSessions(entries, s.piSessions, payload.Key, beforeKey, now)
	} else {
		setSessionOrgState(entries, summary, state, "", now)
	}
	if state == sessionStateArchived || state == sessionStateLater {
		undoKey := sessionOrgKey(summary)
		if state == sessionStatePinned && beforeKey != "" {
			undoKey = ""
		}
		s.recordSessionUndo(untitledSessionLabel(summary.Title), undoKey, snapshot)
	}
	if err := s.persistPreferences(); err != nil {
		replaceSessionOrgEntries(entries, snapshot)
		s.showNotice(text("noticeSessionOrganizationSave"))
		return
	}
	s.refreshSessionOrganization(now)
	if state == sessionStateArchived || state == sessionStateLater {
		s.showSessionUndoNotice(state, untitledSessionLabel(summary.Title))
	}
}

func (s *appState) chatCapablePage() bool {
	if piEphemeralSessions() {
		return false
	}
	switch s.page {
	case pagePluginView:
		return false
	default:
		return true
	}
}

func (s *appState) sessionNavRow(c *ui.Context, p palette, summary piSessionSummary) {
	active := s.isActivePiSession(summary)
	title := untitledSessionLabel(summary.Title)
	key := sessionOrgKey(summary)
	entry, organized := s.sessionOrganizationState(summary)
	bg := ui.Color{}
	foreground := p.muted
	if active {
		bg = p.surfaceHover
		foreground = p.text
	}
	btn := ui.ButtonBase(c.Key("session-"+summary.Path)).Height(40).FillWidth().Padding(0, 10).
		Radius(8).Background(bg).Cursor(ui.CursorPointer).Label(title)
	btn.Justify(ui.Start)
	payload := sessionDragPayload{Key: key}
	btn.Drag(payload).DragDataFrom(func() transfer.Data {
		s.draggingSessionKey = key
		data, _ := json.Marshal(payload)
		return transfer.New(transfer.NewItem(transfer.Bytes(sessionDragFormat, data)))
	}, transfer.DragOptions{
		Operations: transfer.Copy,
		Done: func(transfer.Result) {
			if s.draggingSessionKey == key {
				s.draggingSessionKey = ""
				if s.window != nil {
					s.window.Invalidate()
				}
			}
		},
	})
	if btn.Hovered() && !active {
		btn.Background(p.surfaceHover)
	}
	if btn.Dragging() {
		btn.Opacity(0.5)
	}
	btn.Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Size(20, 20).Shrink(0).Center().Children(func() {
				ui.Icon(c, icon("chat")).Size(15, 15).TextColor(foreground)
			})
			ui.Text(c, title).FontSize(fontBody).TextColor(foreground).SingleLine().Grow(1)
			if s.piSessionsGroups.due[key] {
				ui.Box(c).Size(7, 7).Radius(4).Background(p.accent).Tooltip(text("sessionLaterDue"))
			}
			if btn.Hovered() || btn.Focused() {
				pinned := organized && entry.State == sessionStatePinned
				pinLabel, pinIcon := text("sessionPin"), "pin"
				if pinned {
					pinLabel, pinIcon = text("sessionUnpin"), "pin-off"
				}
				s.sessionRowAction(c, p, "pin-"+key, pinIcon, pinLabel, func() {
					next := sessionStatePinned
					if pinned {
						next = ""
					}
					s.setSessionOrganization(summary, next, "", false)
				})
				archived := organized && entry.State == sessionStateArchived
				archiveLabel, archiveIcon := text("sessionArchive"), "archive"
				next := sessionStateArchived
				if archived {
					archiveLabel, archiveIcon, next = text("sessionRestore"), "archive-restore", ""
				}
				s.sessionRowAction(c, p, "archive-"+key, archiveIcon, archiveLabel, func() {
					s.setSessionOrganization(summary, next, "", next == sessionStateArchived)
				})
			} else {
				ui.Text(c, relativeSessionTime(summary.Updated)).FontSize(fontCaption).
					TextColor(p.muted).SingleLine()
			}
		})
	})
	if _, over := ui.DragOver[sessionDragPayload](btn); over {
		btn.Border(1, p.accent).Background(p.surfaceHover)
		targetState := ""
		if organized {
			targetState = entry.State
		}
		ui.Row(c).Justify(ui.End).Children(func() {
			s.sessionDropBadge(c, p, targetState)
		})
	}
	if drop, ok := ui.Drop[sessionDragPayload](btn); ok {
		targetState := ""
		if organized {
			targetState = entry.State
		}
		s.applySessionDrop(drop, targetState, key)
	}
	btn.ContextMenu(func(m *ui.Menu) {
		pinned := organized && entry.State == sessionStatePinned
		pinLabel := text("sessionPin")
		if pinned {
			pinLabel = text("sessionUnpin")
		}
		if m.Item(pinLabel).Chosen() {
			next := sessionStatePinned
			if pinned {
				next = ""
			}
			s.setSessionOrganization(summary, next, "", false)
		}
		m.Submenu(text("sessionLaterMenu"), func(later *ui.Menu) {
			now := time.Now()
			if later.Item(text("sessionLaterHour")).Chosen() {
				s.setSessionOrganization(summary, sessionStateLater, laterAtHour(now, false), true)
			}
			if later.Item(text("sessionLaterTomorrow")).Chosen() {
				s.setSessionOrganization(summary, sessionStateLater, laterAtHour(now, true), true)
			}
			if later.Item(text("sessionLaterMonday")).Chosen() {
				s.setSessionOrganization(summary, sessionStateLater, laterNextMonday(now), true)
			}
			if later.Item(text("sessionLaterClear")).Chosen() {
				s.setSessionOrganization(summary, sessionStateLater, "", true)
			}
		})
		m.Separator()
		archived := organized && entry.State == sessionStateArchived
		archiveLabel := text("sessionArchive")
		next := sessionStateArchived
		if archived {
			archiveLabel, next = text("sessionRestore"), ""
		}
		if m.Item(archiveLabel).Chosen() {
			s.setSessionOrganization(summary, next, "", next == sessionStateArchived)
		}
	})
	if active {
		btn.Draw(func(painter *ui.Painter, r ui.Rect) {
			painter.Fill(ui.Rect{X: 0, Y: (r.H - 18) / 2, W: 3, H: 18}, p.accent, 2)
		})
	}
	if btn.Clicked() {
		s.switchPiSession(summary)
	}
}

func (s *appState) sessionRowAction(c *ui.Context, p palette, key, iconName, label string, click func()) {
	btn := ui.ButtonBase(c.Key(key)).Size(22, 22).Radius(5).Background(ui.Color{}).
		Center().Cursor(ui.CursorPointer).Label(label).Tooltip(label)
	if btn.Hovered() {
		btn.Background(p.surfacePressed)
	}
	btn.Children(func() {
		ui.Icon(c, icon(iconName)).Size(14, 14).TextColor(p.muted)
	})
	if btn.Clicked() {
		click()
	}
}

func (s *appState) isActivePiSession(summary piSessionSummary) bool {
	if s.piSessionFile != "" && summary.Path != "" {
		return filepath.Clean(summary.Path) == filepath.Clean(s.piSessionFile)
	}
	return s.piSessionID != "" && summary.ID == s.piSessionID
}

func (s *appState) collapsedNewSessionButton(c *ui.Context, p palette) {
	iconButton(c, p, "sessions-new-collapsed", "plus", text("navNewSession"), false, func() {
		s.newPiSession()
	})
}

func secondarySessionAction(c *ui.Context, p palette, key, label string, click func()) {
	btn := ui.ButtonBase(c.Key(key)).Height(28).FillWidth().Padding(0, 10).Radius(7).
		Background(ui.Color{}).Cursor(ui.CursorPointer).Label(label)
	if btn.Hovered() {
		btn.Background(c.Theme().SurfaceHover)
	}
	btn.Children(func() {
		ui.Text(c, label).FontSize(fontSmall).TextColor(c.Theme().TextMuted)
	})
	if btn.Clicked() {
		click()
	}
}

func relativeSessionTime(updated time.Time) string {
	if updated.IsZero() {
		return ""
	}
	elapsed := time.Since(updated)
	switch {
	case elapsed < time.Minute:
		return text("relativeNow")
	case elapsed < time.Hour:
		return textArgs("relativeMinutes", map[string]any{"count": int(elapsed.Minutes())})
	case elapsed < 24*time.Hour:
		return textArgs("relativeHours", map[string]any{"count": int(elapsed.Hours())})
	case elapsed < 7*24*time.Hour:
		return textArgs("relativeDays", map[string]any{"count": int(elapsed.Hours() / 24)})
	default:
		return i18n.FormatDate(i18n.Locale(), updated, i18n.DateOptions{Skeleton: "MMMd"})
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
		Label(s.piConnectionLabel()).Tooltip(text("titleReconnectPi")).Cursor(ui.CursorPointer)
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
		case pageAutomations:
			s.automationsPage(c, p)
		case pageSettings:
			s.settingsPage(c, p)
		case pagePluginView:
			s.pluginViewPage(c, p)
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
			if s.noticeAction != nil && s.noticeActionLabel != "" {
				secondaryAction(c, p, "notice-action", s.noticeActionLabel, s.noticeAction)
			}
			iconButton(c, p, "notice-close", "x", text("titleDismissNotice"), false, func() {
				s.notice = ""
				s.noticeActionLabel = ""
				s.noticeAction = nil
			})
		})
}

func (s *appState) taskPanel(c *ui.Context, p palette) {
	target := float32(0)
	if s.tasksOpen {
		target = 320
	}
	panel := ui.Column(c)
	duration := s.panelAnimationDuration(currentAppearanceReduceMotion())
	animatedWidth := panel.AnimateWith("task-panel-width", target, duration, ui.EaseOut)
	panel.Width(animatedWidth).MinWidth(0).FillHeight().Shrink(0).Clip().
		BorderWidth(0, 0, 0, 1).BorderColor(p.border).Background(p.surface).
		Children(func() {
			ui.Row(c).Height(52).FillWidth().Padding(0, 14).AlignItems(ui.Center).
				BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
				ui.Column(c).Gap(1).Grow(1).Children(func() {
					ui.Text(c, text("tasksTitle")).FontSize(fontBodyLarge).FontWeight(650).TextColor(p.text)
					ui.Text(c, text("tasksDescription")).FontSize(fontCaption).TextColor(p.muted)
				})
				iconButton(c, p, "tasks-close", "x", text("titleCloseTaskPanel"), false, func() {
					s.setTasksOpen(false)
				})
			})

			ui.Scroll(c).Grow(1).Padding(12).Children(func() {
				ui.Column(c).Gap(9).Children(func() {
					s.taskCard(c, p, text("tasksPiRpc"), s.piSecondaryLabel(), s.connectionStateLabel(), s.piTone(p))
					s.taskCard(c, p, text("tasksCurrentRun"), s.currentRunTaskDetail(), s.runLabel(), s.runTone(p))
					s.taskCard(c, p, text("tasksRuntimeDiagnostics"), s.runtimeDiagnosticDetail(), s.runtimeDiagnosticLabel(), s.runtimeDiagnosticTone(p))
					s.taskCard(c, p, text("tasksPluginRegistry"), s.pluginRegistryTaskDetail(), s.pluginRegistryTaskLabel(), s.pluginRegistryTaskTone(p))
					for _, key := range s.pluginTaskKeys() {
						update := s.pluginTasks[key]
						status := fallback(update.Status, strings.ToUpper(update.Tone))
						s.taskCard(c, p, update.Title, update.Detail, status,
							pluginBlockTone(p, update.Tone))
					}
				})
			})

			ui.Row(c).Padding(12, 14).Gap(8).AlignItems(ui.Center).
				BorderWidth(1, 0, 0, 0).BorderColor(p.border).Children(func() {
				ui.Icon(c, icon("info")).Size(14, 14).TextColor(p.muted)
				ui.Text(c, text("tasksOwnership")).FontSize(fontCaption).TextColor(p.muted)
			})
		})
}

func (s *appState) pluginTaskKeys() []string {
	keys := make([]string, 0, len(s.pluginTasks))
	for key := range s.pluginTasks {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (s *appState) pluginRegistryTaskDetail() string {
	registry := s.pluginRegistrySnapshot()
	active := 0
	if s.plugins != nil {
		active = s.plugins.activeCount()
	}
	return textArgs("tasksPluginRegistryDetail", map[string]any{
		"registered":    registry.registeredCount(),
		"active":        active,
		"contributions": registry.contributionCount(),
		"issues":        registry.problemCount(),
	})
}

func (s *appState) pluginRegistryTaskLabel() string {
	registry := s.pluginRegistrySnapshot()
	switch {
	case registry.problemCount() > 0:
		return text("statusAttention")
	case registry.registeredCount() > 0:
		return text("statusRegistered")
	default:
		return text("statusEmpty")
	}
}

func (s *appState) pluginRegistryTaskTone(p palette) ui.Color {
	registry := s.pluginRegistrySnapshot()
	switch {
	case registry.problemCount() > 0:
		return p.warning
	case registry.registeredCount() > 0:
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
		Center().Cursor(ui.CursorPointer).Label(text("chatSendMessage")).Tooltip(text("chatSendToPi"))
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
		Center().Cursor(ui.CursorPointer).Label(text("chatStopCurrentRun")).Tooltip(text("chatStopTooltip"))
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
		return text("piConnected")
	case piConnecting:
		return text("piConnecting")
	case piConnectionErr:
		return text("piConnectionError")
	default:
		return text("piDisconnected")
	}
}

func (s *appState) piCompactLabel() string {
	label := "Pi CLI · " + s.connectionStateLabel()
	if s.piVersion != "" && s.piConn == piConnected {
		label = textArgs("piCliConnectedVersion", map[string]any{"version": s.piVersion})
	}
	return label
}

func (s *appState) piSecondaryLabel() string {
	switch s.piConn {
	case piConnected:
		if s.piModel != "" {
			return textArgs("piSecondaryConnectedModel", map[string]any{"model": s.piModel})
		}
		return text("piSecondaryRPCReady")
	case piConnecting:
		return text("piSecondaryStarting")
	case piConnectionErr:
		return fallback(s.piError, text("piSecondaryConnectionFailed"))
	default:
		return text("piSecondaryClickToConnect")
	}
}

func (s *appState) piDiagnostic() string {
	switch s.piConn {
	case piConnected:
		return text("piDiagnosticConnected")
	case piConnecting:
		return text("piDiagnosticConnecting")
	case piConnectionErr:
		return textArgs("piDiagnosticError", map[string]any{
			"error": fallback(s.piError, text("errorUnknown")),
		})
	default:
		return text("piDiagnosticDisconnected")
	}
}

func (s *appState) connectionStateLabel() string {
	switch s.piConn {
	case piConnected:
		return text("connectionConnected")
	case piConnecting:
		return text("connectionConnecting")
	case piConnectionErr:
		return text("connectionError")
	default:
		return text("connectionDisconnected")
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
		return text("runSending")
	case runStreaming:
		return text("runPiRunning")
	case runStopping:
		return text("runStopping")
	case runCompleted:
		return text("runComplete")
	case runStopped:
		return text("runStopped")
	case runInterrupted:
		return text("runInterrupted")
	case runFailed:
		return text("runFailed")
	default:
		if s.piConn == piConnected {
			return text("runReady")
		}
		return text("runOffline")
	}
}

func (s *appState) runDetail() string {
	switch s.run {
	case runSubmitting:
		return text("runDetailSubmitting")
	case runStreaming:
		return text("runDetailStreaming")
	case runStopping:
		return text("runDetailStopping")
	case runCompleted:
		return text("runDetailCompleted")
	case runStopped:
		return text("runDetailStopped")
	case runInterrupted:
		return text("runDetailInterrupted")
	case runFailed:
		return fallback(s.piError, text("runDetailFailed"))
	default:
		if s.piConn == piConnected {
			return text("runDetailIdle")
		}
		return text("runDetailOffline")
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
		parts = append(parts, textArgs("diagnosticQueued", map[string]any{
			"steering": s.queueSteeringCount,
			"followUp": s.queueFollowUpCount,
		}))
	}
	if s.retrying {
		retry := text("diagnosticRetrying")
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
		parts = append(parts, textArgs("diagnosticRetryFailed", map[string]any{
			"error": summarizeLine(s.retryError, 120),
		}))
	}
	if s.compacting {
		parts = append(parts, textArgs("diagnosticCompacting", map[string]any{
			"reason": fallback(s.compactionReason, text("diagnosticAutomatic")),
		}))
	} else if s.compactionResult != "" {
		parts = append(parts, s.compactionResult)
	}
	if s.compactionError != "" {
		parts = append(parts, textArgs("diagnosticCompactionError", map[string]any{
			"error": summarizeLine(s.compactionError, 120),
		}))
	}
	if s.extensionError != "" {
		source := extensionDisplayName(s.extensionPath)
		if source != "" && s.extensionEvent != "" {
			source += " · " + s.extensionEvent
		}
		parts = append(parts, textArgs("diagnosticExtensionError", map[string]any{
			"source": fallback(source, text("diagnosticUnknownExtension")),
			"error":  summarizeLine(s.extensionError, 120),
		}))
	}
	if len(parts) == 0 {
		return text("diagnosticClear")
	}
	return strings.Join(parts, "\n")
}

func (s *appState) runtimeDiagnosticLabel() string {
	switch {
	case s.extensionError != "" || s.compactionError != "" || (s.retryError != "" && !s.retrying):
		return text("statusAttention")
	case s.retrying || s.compacting || s.queueSteeringCount+s.queueFollowUpCount > 0:
		return text("statusActive")
	default:
		return text("statusClear")
	}
}

func (s *appState) runtimeDiagnosticTone(p palette) ui.Color {
	switch s.runtimeDiagnosticLabel() {
	case text("statusAttention"):
		return p.danger
	case text("statusActive"):
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
		return text("compactionCancelled")
	}
	if event.CompactBefore <= 0 && event.CompactAfter <= 0 {
		if event.CompactSummary != "" {
			return text("compactionCompleted")
		}
		return ""
	}
	return textArgs("compactionCompletedTokens", map[string]any{
		"before": formatTokenCount(event.CompactBefore),
		"after":  formatTokenCount(event.CompactAfter),
	})
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
