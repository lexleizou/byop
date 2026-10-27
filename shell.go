package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

type navItem struct {
	id    pageID
	label string
	icon  string
}

var navItems = []navItem{
	{pageChat, "navChat", "message-square"},
	{pagePlugins, "navPlugins", "blocks"},
	{pageAutomations, "navAutomations", "zap"},
}

const (
	shellTitleBarHeight float32 = 56
)

var (
	shellGap    float32 = 8
	shellRadius         = runtimeTokens.Dimension.Radius["shell"].Value
)

const narrowShellWidth float32 = 960

func (s *appState) view(c *ui.Context) {
	dark := currentAppearanceDark()
	high := s.highContrastActive(currentAppearanceHighContrast())
	reduceTransparency := currentAppearanceReduceTransparency()
	s.syncPluginWebViewTheme(dark, high, reduceTransparency)
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
		s.wallpaperLayer(c, p, dark, high, reduceTransparency)
		ui.Row(c).Absolute().Top(0).Right(0).Bottom(0).Left(0).
			Fill().Padding(shellGap).Gap(0).
			AlignItems(ui.Stretch).Children(func() {
			s.navigation(c, p)
			s.main(c, p)
			s.taskPanel(c, p)
		})
	})
}

func (s *appState) titleBar(c *ui.Context, p palette) {
	bar := c.TitleBar()
	high := s.highContrastActive(currentAppearanceHighContrast())
	reduceTransparency := currentAppearanceReduceTransparency()
	height := float32(shellTitleBarHeight)
	rightPadding := float32(12)
	if runtime.GOOS != "darwin" {
		height = max(bar.Height, shellTitleBarHeight)
		rightPadding = bar.Right + 12
	}

	// The whole bar drags the window; the controls inside it take their own
	// presses first, and the spacers and capsule are explicit handles for the
	// areas that would otherwise show no hit box.
	ui.Row(c).Height(height).FillWidth().Padding(0, rightPadding).DragWindow().
		AlignItems(ui.Center).Children(func() {
		ui.Box(c).Width(36).Shrink(0).FillHeight().DragWindow()
		ui.Spacer(c).Grow(1).FillHeight().DragWindow()
		// The capsule drags the window too, so presses on its title text and
		// other non-interactive content move the window; its buttons take
		// their own presses first.
		capsule := ui.Row(c).Height(36).Padding(0, 12).Radius(12).
			AlignItems(ui.Center).DragWindow()
		if high || reduceTransparency {
			capsule.Border(runtimeTokens.Dimension.Border.Value, p.border).
				Background(p.surface)
		} else {
			capsule.Material(productGlassWithTint(p.glassTint))
		}
		capsule.Children(func() {
			s.titleBarHeading(c, p)
		})
		ui.Spacer(c).Grow(1).FillHeight().DragWindow()
		right := ui.Row(c).Absolute().Top((height-36)/2).Right(12).
			Height(36).Padding(0, 4).Radius(12)
		if high || reduceTransparency {
			right.Border(runtimeTokens.Dimension.Border.Value, p.border).Background(p.surfaceSubtle)
		} else {
			right.Material(productGlassWithTint(p.glassTint))
		}
		right.Children(func() {
			taskPanelToggleButton(c, p, s.tasksOpen, func() {
				s.setTasksOpen(!s.tasksOpen)
			})
		})
	})
}

func glassChrome(card ui.Element, p palette, dark, high, reduceTransparency bool) ui.Element {
	card.Radius(shellRadius)
	if high || reduceTransparency {
		return card.Border(runtimeTokens.Dimension.Border.Value, p.border).Background(p.surface)
	}
	return card.Material(productGlassWithTint(p.glassTint))
}

func opaqueContent(card ui.Element, p palette) ui.Element {
	return card.Border(runtimeTokens.Dimension.Border.Value, p.border).Radius(shellRadius).
		Background(p.surface)
}

func (s *appState) wallpaperLayer(c *ui.Context, p palette, dark, high, reduceTransparency bool) {
	s.wallpaperBackdrop(c, dark, high, reduceTransparency)
}

func (s *appState) titleBarHeading(c *ui.Context, p palette) {
	width, _ := c.Size()
	maxWidth := s.titleCapsuleMaxWidth(width)
	if s.page != pageChat {
		ui.Text(c, s.headerPageTitle()).MaxWidth(maxWidth).FontSize(fontBody).
			FontWeight(600).TextColor(p.text).SingleLine().Tooltip(s.headerPageTitle())
		return
	}
	if s.sessionRenameEditing {
		renameRow := ui.Row(c).Height(34).MaxWidth(maxWidth).MinWidth(0).Gap(4).
			AlignItems(ui.Center)
		renameRow.Children(func() {
			ui.Row(c).Height(32).Grow(1).MinWidth(0).Padding(0, 8).Radius(8).
				Border(runtimeTokens.Dimension.Border.Value, p.focus).Background(p.background).
				AlignItems(ui.Center).Children(func() {
				input := ui.TextInputBase(c.Key("session-title-rename"), &s.sessionRenameDraft).
					Grow(1).Placeholder(text("titleNewChat")).FontSize(fontBody).
					TextColor(p.text).Background(ui.Color{}).Border(0, ui.Color{}).
					Padding(0).Label(text("titleRenameSession")).AutoFocus()
				input.OnTextKey(func(k ui.TextKey) bool {
					switch k.Key {
					case ui.KeyEnter:
						if s.saveSessionRename() {
							k.SetText("")
							return true
						}
					case ui.KeyEscape:
						s.cancelSessionRename()
						return true
					}
					return false
				})
			})
			iconButtonDisabled(c, p, "session-title-rename-save", "check",
				text("titleSaveSessionName"), false,
				s.sessionNamingCommitInFlight || s.sessionRenameInFlight, func() {
					s.saveSessionRename()
				})
		})
		if renameRow.PressedOutside() {
			s.cancelSessionRename()
		}
		return
	}

	titleColor := p.text
	if s.sessionNamingInProgress || s.sessionRenameInFlight {
		titleColor = p.muted
	}
	title := s.sessionDisplayTitle()
	ui.Row(c).MaxWidth(maxWidth).MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).Shrink(1).MinWidth(0).FontSize(fontBody).FontWeight(600).
			TextColor(titleColor).SingleLine().Tooltip(title)
		path := ui.Row(c).Shrink(1).MinWidth(0).Gap(4).AlignItems(ui.Center)
		path.Children(func() {
			workspace := s.workspaceDisplayPath()
			ui.Text(c, "· "+workspace).FontSize(fontCaption).TextColor(p.muted).
				Shrink(1).MinWidth(0).SingleLine().Tooltip(workspace)
			if branch := strings.TrimSpace(s.hud.branch); branch != "" {
				ui.Icon(c, icon("git-branch")).Size(12, 12).Shrink(0).TextColor(p.muted)
				ui.Text(c, branch).FontSize(fontCaption).TextColor(p.muted).
					Shrink(1).MinWidth(0).SingleLine().Tooltip(branch)
			}
		})
		if s.piSessionID != "" || s.piSessionFile != "" {
			s.sessionTitleMenu(c, p)
		}
	})
}

func (s *appState) titleCapsuleMaxWidth(windowWidth float32) float32 {
	return max(float32(0), min(520, s.chatMainWidth(windowWidth)*.60))
}

func (s *appState) workspaceDisplayPath() string {
	workspace := strings.TrimSpace(s.hud.cwd)
	if workspace == "" {
		if cwd, err := s.sessionWorkspaceCwd(); err == nil {
			workspace = canonicalPiPath(cwd)
		}
	}
	if workspace == "" {
		return ""
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if workspace == home {
			return "~"
		}
		if strings.HasPrefix(workspace, home+string(filepath.Separator)) {
			return "~" + strings.TrimPrefix(workspace, home)
		}
	}
	return workspace
}

func (s *appState) sessionTitleMenu(c *ui.Context, p palette) {
	label := text("titleSessionMenu")
	button := ui.ButtonBase(c.Key("session-title-menu")).Absolute().
		Top(0).Right(0).Bottom(0).Left(0).Radius(8).
		Background(ui.Color{}).Label(label).Tooltip(label).Cursor(ui.CursorPointer)
	connected := s.piConn == piConnected
	available := connected && !s.piSessionBusy &&
		!s.sessionNamingCommitInFlight && !s.sessionRenameInFlight
	button.Menu(func(m *ui.Menu) {
		if m.Item(text("titleRenameSession")).Disabled(!available || s.piSessionID == "").Chosen() {
			s.beginSessionRename()
		}
		if m.Item(text("titleRenameSessionFromContext")).
			Disabled(!available || len(s.chat) == 0).Chosen() {
			s.startContextSessionNaming()
		}
	})
}

func (s *appState) headerPageTitle() string {
	switch s.page {
	case pageChat:
		return s.sessionDisplayTitle()
	case pagePlugins:
		return text("navPlugins")
	case pageAutomations:
		return text("navAutomations")
	case pageSettings:
		return text("navSettings")
	case pagePluginView:
		entry, ok := s.registeredPluginEntry(s.pluginViewPluginID)
		if ok {
			if view, found := pluginViewByID(entry.Manifest, s.pluginViewID); found {
				model := s.pluginViewModels[pluginViewCacheKey(entry.ID, view.ID)]
				return pluginLocalizedText(entry.Manifest, pluginViewTitle(view, model))
			}
		}
		return text("navPlugins")
	default:
		return text("navChat")
	}
}

func panelShortcutModifier() string {
	modifier := "Ctrl"
	if runtime.GOOS == "darwin" {
		modifier = "⌘"
	}
	return modifier
}

func navSecondaryText(p palette) ui.Color {
	return p.text.Mix(p.muted, .45)
}

func navigationToggleButton(c *ui.Context, p palette, collapsed bool, click func()) {
	shortcut := panelShortcutModifier() + "B"
	panelToggleButton(c, p, "nav-toggle", "panel-left",
		textArgs("titleToggleNavigationShortcut", map[string]any{"shortcut": shortcut}),
		false, click)
}

func taskPanelToggleButton(c *ui.Context, p palette, open bool, click func()) {
	shortcut := "Ctrl+Alt+B"
	if runtime.GOOS == "darwin" {
		shortcut = panelShortcutModifier() + "⌥B"
	}
	panelToggleButton(c, p, "tasks-toggle", "panel-right",
		textArgs("titleToggleTaskPanelShortcut", map[string]any{"shortcut": shortcut}),
		open, click)
}

func panelToggleButton(c *ui.Context, p palette, key, iconName, label string, selected bool, click func()) {
	bg := ui.Color{}
	fg := navSecondaryText(p)
	if selected {
		bg = p.text.Alpha(.10)
		fg = p.text
	}
	btn := ui.ButtonBase(c.Key(key)).Size(28, 28).Background(bg).Radius(8).
		Center().Cursor(ui.CursorPointer).Label(label).Tooltip(label)
	if !selected && btn.Hovered() {
		btn.Background(p.surfaceHover)
	}
	btn.Children(func() {
		ui.Icon(c, icon(iconName)).Size(16, 16).TextColor(fg)
	})
	if btn.Clicked() {
		click()
	}
}

// Shared shell widths. chatMainWidth uses the same values to decide HUD wrapping.
const (
	navExpandedWidth    = 200
	navCollapsedWidth   = 56
	navMinExpandedWidth = 160
	navMaxExpandedWidth = 360
	taskPanelWidth      = 320
	// chatMinMainWidth is the narrowest the chat stage may become before the
	// navigation stops growing.
	chatMinMainWidth = 400

	// Session rows share one height and one gap so the archived panel can size
	// its animated body without clipping the last row.
	sessionRowHeight = 34
	sessionRowGap    = 4
)

// normalizeNavExpandedWidth clamps a saved navigation width to the widths the
// resize handle offers, and falls back to the default when none was saved.
func normalizeNavExpandedWidth(value float32) float32 {
	if value <= 0 {
		return navExpandedWidth
	}
	return min(max(value, navMinExpandedWidth), navMaxExpandedWidth)
}

// navExpandedWidthLimit is the widest the navigation may grow in a window: it
// leaves chatMinMainWidth for the stage once the shell gaps and the docked task
// panel take their room, and never narrows the navigation past its minimum.
func (s *appState) navExpandedWidthLimit(windowWidth float32) float32 {
	var tasksWidth float32
	if s.tasksOpen && windowWidth >= narrowShellWidth {
		tasksWidth = taskPanelWidth + shellGap
	}
	// The window keeps two outer gaps, the gap after the navigation, and the
	// stage's own room.
	room := windowWidth - 3*shellGap - tasksWidth - chatMinMainWidth
	return max(navMinExpandedWidth, min(navMaxExpandedWidth, room))
}

// navigationExpandedWidth is the width the user chose, clamped to the widths
// the handle offers. It is the saved preference, not what a narrow layout
// shows.
func (s *appState) navigationExpandedWidth() float32 {
	return normalizeNavExpandedWidth(s.navWidth)
}

// navigationWidth is the width of the navigation at a window width, as the
// shell lays it out and as chatMainWidth accounts for it. A narrow window or a
// docked task panel can narrow it below the chosen width without changing it.
func (s *appState) navigationWidth(windowWidth float32) float32 {
	if s.navigationCollapsedForWidth(windowWidth) {
		return navCollapsedWidth
	}
	return min(s.navigationExpandedWidth(), s.navExpandedWidthLimit(windowWidth))
}

func brandMark(c *ui.Context, p palette, size float32) ui.Element {
	if byopLogoMarkBitmap != nil {
		return ui.Image(c, byopLogoMarkBitmap).Size(size, size).Fit(ui.Contain).
			Label("Build You Own Pi")
	}
	return ui.Icon(c, icon("byop")).Size(size, size).TextColor(p.text).Label("Build You Own Pi")
}

func (s *appState) navigation(c *ui.Context, p palette) {
	windowWidth, _ := c.Size()
	collapsed := s.navigationCollapsedForWidth(windowWidth)
	width := s.navigationWidth(windowWidth)
	dark := currentAppearanceDark()
	high := s.highContrastActive(currentAppearanceHighContrast())
	reduceTransparency := currentAppearanceReduceTransparency()
	navPalette := p
	if deepLiquidGlassActive(dark, high, reduceTransparency) &&
		s.preferences.NavTransparency > 75 {
		navPalette.muted = p.text
	}

	nav := ui.Column(c)
	duration := s.panelAnimationDuration(currentAppearanceReduceMotion())
	if collapsed && s.navResizeDragging {
		// A narrow window or a collapse ends a resize in progress: keep the
		// width it reached before the handle went away.
		s.navResizeDragging = false
		if err := s.persistNavigationWidth(); err != nil {
			s.showNotice(text("noticePreferenceSaveNavigationWidth"))
		}
	}
	if s.navResizeDragging {
		// Follow the pointer without easing while the handle is dragged.
		duration = 0
	}
	animatedWidth := nav.AnimateWith("navigation-width", width+shellGap, duration, ui.EaseOut)
	nav.Width(animatedWidth).FillHeight().Shrink(0).Padding(0, shellGap, 0, 0).Children(func() {
		navGlassCard(c, p, dark, high, reduceTransparency, shellRadius,
			s.preferences.NavTransparency, s.preferences.GlassBlur,
			s.preferences.GlassRefraction, func() {
				ui.Column(c).FillHeight().FillWidth().MinHeight(0).Children(func() {
					s.navigationTrafficArea(c, navPalette)
					s.navigationBrandRow(c, navPalette)

					ui.Column(c).Grow(1).FillWidth().MinHeight(0).Padding(0, 8).
						Children(func() {
							ui.Column(c).FillWidth().Shrink(0).Gap(2).Children(func() {
								if !collapsed {
									s.newChatNavButton(c, navPalette)
								}
								for _, item := range navItems {
									s.navButton(c, navPalette, item)
								}
								pluginItems := s.pluginNavigationItems()
								if len(pluginItems) > 0 {
									if !collapsed {
										ui.Text(c, text("navPluginsSection")).FontSize(fontCaption).
											TextColor(navPalette.muted).Padding(4, 8)
									}
									for _, item := range pluginItems {
										s.pluginNavButton(c, navPalette, item)
									}
								}
							})
							if !collapsed && s.chatCapablePage() {
								s.sessionsNavigation(c, navPalette)
							} else {
								ui.Spacer(c).Grow(1)
							}
						})
					s.navigationFooter(c, navPalette, dark, high, reduceTransparency)
				})
			})
		if !collapsed {
			s.navigationResizeHandle(c, p, windowWidth)
		}
	})
}

// navigationResizeHandle is the strip in the gap between the navigation and
// the stage, which the pointer drags to resize the expanded navigation.
func (s *appState) navigationResizeHandle(c *ui.Context, p palette, windowWidth float32) {
	handle := ui.Box(c).Key("navigation-resize").Absolute().Top(0).Bottom(0).
		Right(0).Width(shellGap).Cursor(ui.CursorResizeEW).
		Label(text("navigationResize"))
	// Dragged reports the pointer's move in this frame, so each frame steps
	// the width from the one the previous frame shows.
	dx, _, held := handle.Dragged()
	if held && !s.navResizeDragging {
		s.navResizeDragging = true
	}
	if held {
		shown := s.navigationWidth(windowWidth)
		next := normalizeNavExpandedWidth(min(shown+dx, s.navExpandedWidthLimit(windowWidth)))
		if next != shown {
			s.navWidth = next
			s.preferences.NavExpandedWidth = next
		}
	} else if s.navResizeDragging {
		s.navResizeDragging = false
		if err := s.persistNavigationWidth(); err != nil {
			s.showNotice(text("noticePreferenceSaveNavigationWidth"))
		}
	}
	active := held || handle.Hovered()
	handle.Draw(func(painter *ui.Painter, r ui.Rect) {
		if !active || r.H <= 16 {
			return
		}
		line := p.border
		if held {
			line = p.accent
		}
		painter.Fill(ui.Rect{X: r.X + r.W/2 - .25, Y: r.Y + 8, W: .5, H: r.H - 16}, line, 0)
	})
}

func (s *appState) navigationTrafficArea(c *ui.Context, p palette) {
	collapsed := s.navigationVisuallyCollapsed(c)
	row := ui.Row(c).Height(48).FillWidth().Shrink(0).Padding(0, 8, 0, 14).
		AlignItems(ui.Center)
	if collapsed {
		row.Padding(0, 14).Justify(ui.Center)
	}
	row.Children(func() {
		ui.Spacer(c).Grow(1).FillHeight().DragWindow()
		navigationToggleButton(c, p, collapsed, func() {
			s.setNavCollapsed(!collapsed)
		})
	})
}

func (s *appState) navigationBrandRow(c *ui.Context, p palette) {
	collapsed := s.navigationVisuallyCollapsed(c)
	row := ui.Row(c).Height(46).FillWidth().Shrink(0).Padding(4, 16, 12, 16).
		AlignItems(ui.Center)
	if collapsed {
		row.Height(34).Padding(4, 0, 12, 0).Justify(ui.Center)
	} else {
		row.Gap(8).Justify(ui.Start)
	}
	row.Children(func() {
		brandMark(c, p, 18)
		if !collapsed {
			ui.Text(c, appShort).FontSize(20).FontWeight(650).
				FixedLineHeight(30).TextColor(p.text).Tooltip(appTitle)
		}
	})
}

func (s *appState) navigationFooter(c *ui.Context, p palette, dark, high, reduceTransparency bool) {
	glass := deepLiquidGlassActive(dark, high, reduceTransparency)
	collapsed := s.navigationVisuallyCollapsed(c)
	ui.Column(c).FillWidth().Shrink(0).Children(func() {
		if glass && !collapsed {
			ui.Box(c).Height(runtimeTokens.Dimension.Border.Value).FillWidth().
				Background(p.border.Alpha(.45))
		}
		collapsedPadding := float32(8)
		footer := ui.Column(c).FillWidth().Shrink(0).
			Padding(collapsedPadding, collapsedPadding, collapsedPadding, collapsedPadding)
		if collapsed {
			footer.Gap(8).AlignItems(ui.Center).Children(func() {
				s.navigationGlassButton(c, p)
				s.settingsGearButton(c, p)
			})
			return
		}
		footer.Padding(8, 8, 8, 16).Gap(4)
		footer.Children(func() {
			ui.Row(c).FillWidth().Gap(4).AlignItems(ui.Center).Children(func() {
				s.integrationStatus(c, p)
				s.navigationGlassButton(c, p)
				s.settingsGearButton(c, p)
			})
		})
	})
}

func (s *appState) navigationCollapsedForWidth(windowWidth float32) bool {
	return s.navCollapsed || windowWidth < narrowShellWidth
}

func (s *appState) navigationVisuallyCollapsed(c *ui.Context) bool {
	width, _ := c.Size()
	return s.navigationCollapsedForWidth(width)
}

func (s *appState) newChatNavButton(c *ui.Context, p palette) {
	label := text("navNewChat")
	foreground := navSecondaryText(p)
	shortcut := "⌘N"
	if runtime.GOOS != "darwin" {
		shortcut = "Ctrl N"
	}
	btn := ui.ButtonBase(c.Key("nav-new-chat")).Height(32).FillWidth().Padding(0, 8).
		Radius(8).Background(ui.Color{}).Cursor(ui.CursorPointer).Label(label)
	btn.Justify(ui.Start)
	if btn.Hovered() {
		btn.Background(p.surfaceHover)
	}
	btn.Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon("square-pen")).Size(16, 16).TextColor(foreground)
			ui.Text(c, label).FontSize(fontBody).TextColor(foreground).Grow(1).SingleLine()
			ui.Text(c, shortcut).FontSize(fontSmall).TextColor(p.muted)
		})
	})
	if btn.Clicked() {
		s.newPiSession()
	}
}

func (s *appState) pluginNavButton(c *ui.Context, p palette, item pluginNavigationItem) {
	viewID := item.nav.View
	selected := s.page == pagePluginView && s.pluginViewPluginID == item.entry.ID &&
		s.pluginViewID == viewID
	bg := ui.Color{}
	foreground := navSecondaryText(p)
	if selected {
		bg = p.text.Alpha(.10)
		foreground = p.text
	}
	label := fallback(item.nav.Title, viewID)
	key := "nav-plugin-" + item.entry.ID + "-" + item.nav.ID
	btn := ui.ButtonBase(c.Key(key)).Height(32).FillWidth().Padding(0, 8).
		Radius(8).Background(bg).Cursor(ui.CursorPointer).Label(label)
	collapsed := s.navigationVisuallyCollapsed(c)
	if collapsed {
		btn.Tooltip(label)
		btn.Justify(ui.Center)
	} else {
		btn.Justify(ui.Start)
	}
	if !selected && btn.Hovered() {
		btn.Background(p.text.Alpha(.09))
	}
	btn.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Size(20, 20).Shrink(0).Center().Children(func() {
				ui.Icon(c, iconOr(item.nav.Icon, "plugins")).Size(16, 16).TextColor(foreground)
			})
			if !collapsed {
				ui.Text(c, label).FontSize(fontBody).TextColor(foreground).SingleLine()
			}
		})
	})
	if btn.Clicked() {
		s.openPluginNavigation(item.entry, item)
	}
}

func (s *appState) navButton(c *ui.Context, p palette, item navItem) {
	selected := s.page == item.id
	bg := ui.Color{}
	foreground := navSecondaryText(p)
	if selected {
		bg = p.text.Alpha(.10)
		foreground = p.text
	}
	label := text(item.label)
	btn := ui.ButtonBase(c.Key("nav-"+item.id)).Height(32).FillWidth().Padding(0, 8).
		Radius(8).Background(bg).Cursor(ui.CursorPointer).Label(label)
	collapsed := s.navigationVisuallyCollapsed(c)
	if collapsed {
		btn.Tooltip(label)
		btn.Justify(ui.Center)
	} else {
		btn.Justify(ui.Start)
	}
	if !selected && btn.Hovered() {
		btn.Background(p.text.Alpha(.09))
	}
	btn.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Size(20, 20).Shrink(0).Center().Children(func() {
				ui.Icon(c, icon(item.icon)).Size(16, 16).TextColor(foreground)
			})
			if !collapsed {
				ui.Text(c, label).FontSize(fontBody).TextColor(foreground).SingleLine()
			}
		})
	})
	if btn.Clicked() {
		s.setPage(item.id)
	}
}

const sessionDragFormat transfer.Format = "application/vnd.byop.session+json"

type sessionDragPayload struct {
	Key string `json:"key"`
}

func (s *appState) sessionDragDone(key string, result transfer.Result) {
	if s.draggingSessionKey != key {
		return
	}
	if result.Canceled || result.Err != nil || result.Operation == transfer.None {
		s.clearSessionDrag(key)
		return
	}
	// MyGo consumes a drop in the frame after Done. Keep drag-only targets
	// rendered for that frame so DropData can still find them.
	s.draggingSessionClear = true
	if s.window != nil {
		s.window.Invalidate()
	}
}

func (s *appState) clearSessionDrag(key string) {
	if key != "" && s.draggingSessionKey != key {
		return
	}
	s.draggingSessionKey = ""
	s.draggingSessionClear = false
	if s.window != nil {
		s.window.Invalidate()
	}
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
	ui.Column(c).Grow(1).MinHeight(0).FillWidth().Padding(12, 0, 8, 0).Children(func() {
		defer func() {
			if !s.draggingSessionClear {
				return
			}
			key := s.draggingSessionKey
			s.draggingSessionClear = false
			s.postUI(func() { s.clearSessionDrag(key) })
		}()
		if len(s.piSessions) == 0 {
			return
		}
		// Group spacing keeps pinned, recent, later and archived sessions apart
		// without adding a second heading for the default recent group.
		ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Padding(8, 0, 8, 0).Children(func() {
			dragging := s.draggingSessionKey != ""
			draggedFromArchived := false
			if dragging {
				for _, summary := range groups.archived {
					if sessionOrgKey(summary) == s.draggingSessionKey {
						draggedFromArchived = true
						break
					}
				}
			}
			renderDragOnlyGroups := func() {
				if len(groups.pinned) == 0 {
					s.sessionSection(c, p, "pinned", text("navPinned"), groups.pinned,
						sessionStatePinned, false, true)
				}
				if len(groups.sessions) == 0 {
					s.sessionSection(c, p, "sessions", text("navRecentSessions"), groups.sessions,
						"", true, true)
				}
				if len(groups.later) == 0 {
					s.sessionSection(c, p, "later", text("navLater"), groups.later,
						sessionStateLater, false, true)
				}
			}
			if len(groups.pinned) > 0 {
				s.sessionSection(c, p, "pinned", text("navPinned"), groups.pinned,
					sessionStatePinned, false, true)
			}
			if len(groups.sessions) > 0 {
				if len(groups.pinned) > 0 {
					sessionGroupSeparator(c, p)
				}
				s.sessionSection(c, p, "sessions", text("navRecentSessions"), groups.sessions,
					"", true, true)
			} else if !dragging {
				// Keep the normal empty-group footprint without inserting the
				// taller drag placeholder above rows that are already visible.
				s.sessionDropArea(c, p, "sessions-drop-sessions", "", text("navRecentSessions"),
					false, func() {})
			} else {
				ui.Box(c).Height(8).FillWidth()
			}
			if len(groups.later) > 0 {
				sessionGroupSeparator(c, p)
				s.sessionSection(c, p, "later", text("navLater"), groups.later,
					sessionStateLater, false, true)
			}
			if !dragging {
				if len(groups.archived) > 0 {
					s.archivedSessionSection(c, p, groups)
				}
				return
			}
			// Keep drag-only groups below visible source rows and above the
			// archived section unless the source itself is archived.
			if !draggedFromArchived {
				renderDragOnlyGroups()
			}
			if len(groups.archived) > 0 || dragging {
				s.archivedSessionSection(c, p, groups)
			}
			if draggedFromArchived {
				renderDragOnlyGroups()
			}
		})
	})
}

// sessionGroupSeparator sets one session group apart from the one above it:
// 12 DIPs of breathing room, a 0.5px rule inset from the card edges, and 8
// DIPs before the next group's first label.
func sessionGroupSeparator(c *ui.Context, p palette) {
	ui.Box(c).Height(12).FillWidth().Shrink(0)
	ui.Row(c).FillWidth().Shrink(0).Padding(0, 8).Children(func() {
		ui.Box(c).Height(runtimeTokens.Dimension.Border.Value).FillWidth().Background(p.border)
	})
	ui.Box(c).Height(8).FillWidth().Shrink(0)
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
		group := ""
		for index, summary := range visible {
			if key == "sessions" {
				next := sessionDateGroup(summary.Updated, time.Now())
				if index == 0 || next != group {
					top := float32(12)
					if index == 0 {
						top = 0
					}
					ui.Text(c, sessionDateGroupLabel(next)).FontSize(fontCaption).
						TextColor(p.muted).Padding(top, 8, 4, 8)
				}
				group = next
			}
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
	sessionGroupSeparator(c, p)
	s.sessionSectionHeader(c, p, "archived", textArgs("navArchived", map[string]any{
		"count": len(groups.archived),
	}), sessionStateArchived, len(groups.archived), true)
	expanded := s.archivedExpanded || s.draggingSessionKey != ""
	target := float32(0)
	if expanded {
		// Rows, their gaps, and the room a drop highlight adds around them.
		target = float32(len(groups.archived))*(sessionRowHeight+sessionRowGap) -
			sessionRowGap + 16
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
		Padding(0, 8).Radius(8).Background(ui.Color{}).Cursor(ui.CursorPointer).Label(label)
	if header.Hovered() {
		header.Background(p.surfaceHover)
	}
	header.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Text(c, label).FontSize(fontCaption).TextColor(p.muted).Grow(1)
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
		header.Border(runtimeTokens.Dimension.Border.Value, p.accent)
	}
	if drop, ok := ui.Drop[sessionDragPayload](header); ok {
		s.applySessionDrop(drop, state, "")
	}
	opts := sessionDropOptions()
	if _, over := ui.DataDragOver(header, opts); over {
		header.Border(runtimeTokens.Dimension.Border.Value, p.accent)
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
	area := ui.Column(c).FillWidth().MinHeight(8).Gap(sessionRowGap)
	_, localOver := ui.DragOver[sessionDragPayload](area)
	_, nativeOver := ui.DataDragOver(area, sessionDropOptions())
	over := localOver || nativeOver
	if over {
		area.Border(runtimeTokens.Dimension.Border.Value, p.accent).Padding(4).Background(p.surface)
	}
	area.Children(func() {
		showPlaceholder := over || (state == "" && s.draggingSessionKey != "")
		if showPlaceholder && len(s.piSessions) > 0 && empty {
			s.sessionDropPlaceholder(c, p, label)
		}
		children()
		if over {
			ui.Row(c).FillWidth().Justify(ui.End).Padding(0, 4).Children(func() {
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
	ui.Row(c).Height(32).FillWidth().Padding(0, 8).AlignItems(ui.Center).
		Radius(8).Border(runtimeTokens.Dimension.Border.Value, p.accent).Children(func() {
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
	ui.Row(c).Padding(4, 8).Radius(8).Background(p.accent).Children(func() {
		ui.Text(c, label).FontSize(fontMicro).FontWeight(700).TextColor(p.onAccent)
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

func (s *appState) displayedSessionState(summary piSessionSummary, entry sessionOrgEntry, organized bool) string {
	if !organized || s.piSessionsGroups.due[sessionOrgKey(summary)] {
		return ""
	}
	return entry.State
}

func (s *appState) applySessionDrop(payload sessionDragPayload, state string, beforeKey string) {
	summary, ok := s.sessionSummaryByKey(payload.Key)
	if !ok {
		return
	}
	entry, organized := s.sessionOrganizationState(summary)
	currentState := s.displayedSessionState(summary, entry, organized)
	if state == sessionStatePinned {
		if beforeKey == payload.Key || (beforeKey == "" && currentState == sessionStatePinned) {
			s.clearSessionDrag("")
			return
		}
	} else if currentState == state {
		s.clearSessionDrag("")
		return
	}
	s.clearSessionDrag("")
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
	if active {
		title = s.sessionDisplayTitle()
	}
	key := sessionOrgKey(summary)
	entry, organized := s.sessionOrganizationState(summary)
	displayedState := s.displayedSessionState(summary, entry, organized)
	bg := ui.Color{}
	foreground := navSecondaryText(p)
	if active {
		bg = p.text.Alpha(.10)
		foreground = p.text
	}
	btn := ui.ButtonBase(c.Key("session-"+summary.Path)).Height(sessionRowHeight).FillWidth().
		Padding(0, 8).Radius(8).Background(bg).Cursor(ui.CursorPointer).Label(title)
	btn.Justify(ui.Start)
	payload := sessionDragPayload{Key: key}
	btn.Drag(payload).DragDataFrom(func() transfer.Data {
		s.draggingSessionKey = key
		data, _ := json.Marshal(payload)
		return transfer.New(transfer.NewItem(transfer.Bytes(sessionDragFormat, data)))
	}, transfer.DragOptions{
		Operations: transfer.Copy,
		Done: func(result transfer.Result) {
			s.sessionDragDone(key, result)
		},
	})
	if btn.Hovered() && !active {
		btn.Background(p.text.Alpha(.09))
	}
	if btn.Dragging() {
		btn.Opacity(0.5)
	}
	btn.Children(func() {
		ui.Row(c).FillWidth().MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).FontSize(fontBody).TextColor(foreground).SingleLine().
				Grow(1).MinWidth(0)
			if s.piSessionsGroups.due[key] {
				ui.Box(c).Size(7, 7).Shrink(0).Radius(4).Background(p.accent).
					Tooltip(text("sessionLaterDue"))
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
			} else if active && s.runActive() {
				ui.Box(c).Size(6, 6).Shrink(0).Radius(3).Background(p.accent).
					Tooltip(text("chatPiWorking"))
			} else if updated := todaySessionTime(summary.Updated); updated != "" {
				ui.Text(c, updated).FontSize(fontCaption).TextColor(p.muted).
					Shrink(0).SingleLine()
			}
		})
	})
	if _, over := ui.DragOver[sessionDragPayload](btn); over {
		btn.Border(runtimeTokens.Dimension.Border.Value, p.accent).Background(p.surfaceHover)
		ui.Row(c).Justify(ui.End).Children(func() {
			s.sessionDropBadge(c, p, displayedState)
		})
	}
	if drop, ok := ui.Drop[sessionDragPayload](btn); ok {
		s.applySessionDrop(drop, displayedState, key)
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
	if btn.Clicked() {
		s.switchPiSession(summary)
	}
}

func sessionDateGroup(updated, now time.Time) string {
	if updated.IsZero() {
		return "earlier"
	}
	local := updated.In(time.Local)
	today := now.In(time.Local)
	y, m, d := local.Date()
	ty, tm, td := today.Date()
	switch {
	case y == ty && m == tm && d == td:
		return "today"
	case y == ty && m == tm && d == td-1:
		return "yesterday"
	default:
		previous := time.Date(ty, tm, td-1, 0, 0, 0, 0, today.Location())
		if y == previous.Year() && m == previous.Month() && d == previous.Day() {
			return "yesterday"
		}
		return "earlier"
	}
}

func sessionDateGroupLabel(group string) string {
	switch group {
	case "today":
		return text("navToday")
	case "yesterday":
		return text("navYesterday")
	default:
		return text("navEarlier")
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
	btn := ui.ButtonBase(c.Key(key)).Height(28).FillWidth().Padding(0, 8).Radius(8).
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

func todaySessionTime(updated time.Time) string {
	if updated.IsZero() {
		return ""
	}
	now := time.Now()
	updated = updated.Local()
	if updated.Year() != now.Year() || updated.YearDay() != now.YearDay() {
		return ""
	}
	return updated.Format("15:04")
}

func (s *appState) integrationStatus(c *ui.Context, p palette) {
	label := textArgs("piStatusLabel", map[string]any{"status": s.connectionStateLabel()})
	var box ui.Element
	if s.navigationVisuallyCollapsed(c) {
		box = ui.ButtonBase(c.Key("pi-connection-icon")).Size(28, 28).Radius(8).Center().
			Label(label).Tooltip(label).Cursor(ui.CursorPointer)
		box.Children(func() {
			ui.Row(c).Size(20, 20).Center().Children(func() {
				ui.Box(c).Size(6, 6).Radius(3).Background(s.piTone(p))
			})
		})
	} else {
		box = ui.ButtonBase(c.Key("pi-connection")).Grow(1).MinWidth(0).Height(28).
			Padding(0).Radius(8).Justify(ui.Start).AlignItems(ui.Center).Gap(4).
			Label(label).Tooltip(label).Cursor(ui.CursorPointer)
		if box.Hovered() {
			box.Background(p.surfaceHover)
		}
		box.Children(func() {
			ui.Row(c).Size(6, 18).Shrink(0).Center().Children(func() {
				ui.Box(c).Size(6, 6).Radius(3).Background(s.piTone(p))
			})
			version := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s.piVersion), "Pi "))
			textValue := textArgs("navPiStatus", map[string]any{
				"version": version,
				"status":  s.connectionStateLabel(),
			})
			if version == "" {
				textValue = textArgs("navPiStatusNoVersion", map[string]any{
					"status": s.connectionStateLabel(),
				})
			}
			ui.Text(c, textValue).FontSize(fontCaption).TextColor(p.muted).
				MinWidth(0).SingleLine()
		})
	}
	if box.Clicked() {
		s.piStatusPopoverOpen = !s.piStatusPopoverOpen
	}
	popoverV12(c, box, &s.piStatusPopoverOpen,
		s.highContrastActive(currentAppearanceHighContrast()) || currentAppearanceReduceTransparency(),
		func(panel ui.Element) {
			panel.Children(func() { s.piConnectionPopover(c, p) })
		})
}

func (s *appState) piConnectionPopover(c *ui.Context, p palette) {
	ui.Column(c).Width(280).Gap(8).Children(func() {
		ui.Text(c, text("piCliDetailsTitle")).FontSize(fontBody).FontWeight(650).TextColor(p.text)
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(8, 8).Radius(4).Background(s.piTone(p))
			ui.Text(c, s.connectionStateLabel()).FontSize(fontSmall).TextColor(p.muted)
		})
		field := func(label, value string) {
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, label).FontSize(fontCaption).FontWeight(650).TextColor(p.muted)
				ui.Text(c, value).Font("monospace").FontSize(fontSmall).TextColor(p.text).
					SingleLine().Tooltip(value)
			})
		}
		field(text("settingsCliPath"), s.piPathLabel())
		if s.piVersion != "" {
			field(text("settingsVersion"), s.piVersion)
		}
		if message := s.piConnectionErrorText(); message != "" {
			ui.Text(c, message).FontSize(fontSmall).LineHeight(1.4).TextColor(p.dangerText)
		}
		if detail := strings.TrimSpace(s.piError); detail != "" && detail != s.piConnectionErrorText() {
			ui.Text(c, detail).Font("monospace").FontSize(fontCaption).LineHeight(1.4).
				TextColor(p.muted)
		}
		if s.piConn != piConnecting {
			secondaryAction(c, p, "pi-popover-reconnect", text("settingsReconnect"), func() {
				s.piStatusPopoverOpen = false
				s.reconnectPi()
			})
		}
	})
}

func (s *appState) settingsGearButton(c *ui.Context, p palette) {
	label := text("navSettings")
	selected := s.page == pageSettings
	bg, fg := ui.Color{}, navSecondaryText(p)
	if selected {
		bg, fg = p.text.Alpha(.10), p.text
	}
	button := ui.ButtonBase(c.Key("nav-settings-gear")).Size(28, 28).Radius(8).
		Background(bg).Center().Label(label).Tooltip(label).Cursor(ui.CursorPointer)
	if !selected && button.Hovered() {
		button.Background(p.surfaceHover)
	}
	button.Children(func() {
		ui.Icon(c, icon("settings")).Size(16, 16).TextColor(fg)
	})
	if button.Clicked() {
		s.setPage(pageSettings)
	}
}

func (s *appState) main(c *ui.Context, p palette) {
	dark := currentAppearanceDark()
	high := s.highContrastActive(currentAppearanceHighContrast())
	reduceTransparency := currentAppearanceReduceTransparency()
	stageBackground := mainStageBackground(p, dark)
	card := ui.Column(c).Grow(1).MinWidth(0).MinHeight(0).FillHeight().
		Radius(shellRadius).Border(runtimeTokens.Dimension.Border.Value, p.border).
		Background(stageBackground)
	if deepLiquidGlassActive(dark, high, reduceTransparency) {
		card.Shadow(runtimeTokens.Shadow.Stage.OffsetX.Value,
			runtimeTokens.Shadow.Stage.OffsetY.Value,
			runtimeTokens.Shadow.Stage.Blur.Value,
			runtimeTokens.Shadow.Stage.Spread.Value, p.shadowStage)
	}
	card.Children(func() {
		ui.Column(c).FillHeight().FillWidth().MinHeight(0).Radius(shellRadius - .5).Clip().
			Children(func() {
				s.titleBar(c, p)
				ui.Column(c).Grow(1).FillWidth().MinHeight(0).Children(func() {
					if s.notice != "" {
						s.noticeBanner(c, p)
					}
					switch s.page {
					case pagePlugins:
						s.pluginsPage(c, p)
					case pageAutomations:
						s.automationsPage(c, p)
					case pageSettings:
						s.settingsPageV12(c, p, &s.settingsPageState)
					case pagePluginView:
						s.pluginViewPage(c, p)
					default:
						s.chatPage(c, p)
					}
					ui.Box(c).Absolute().Top(0).Right(0).Left(0).Height(24).
						LinearGradient(ui.LinearGradient{
							Angle: 180,
							Stops: []ui.GradientStop{
								{Color: stageBackground.Alpha(.32), At: 0},
								{Color: stageBackground.Alpha(0), At: 1},
							},
						}).PassThrough()
				})
			})
	})
}

func mainStageBackground(p palette, dark bool) ui.Color {
	if dark {
		return p.readingSurface
	}
	return p.surface
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
	windowWidth, _ := c.Size()
	target := float32(0)
	if s.tasksOpen {
		target = taskPanelWidth + shellGap
	}
	panel := ui.Column(c)
	duration := s.panelAnimationDuration(currentAppearanceReduceMotion())
	animatedWidth := panel.AnimateWith("task-panel-width", target, duration, ui.EaseOut)
	if animatedWidth < 1 {
		panel.Width(0).Shrink(0)
		return
	}
	leftGap := min(shellGap, animatedWidth)
	panel.Width(animatedWidth).MinWidth(0).FillHeight().Shrink(0).Padding(0, 0, 0, leftGap)
	if windowWidth < narrowShellWidth {
		panel.Absolute().Top(8).Right(8).Bottom(8)
	}
	panel.Children(func() {
		card := styleRaisedSurface(
			ui.Column(c).FillWidth().FillHeight().MinWidth(0),
			p,
			shellRadius,
		)
		if !p.dark {
			card.BorderColor(p.text.Alpha(.10))
		}
		card.Children(func() {
			ui.Column(c).FillHeight().FillWidth().MinHeight(0).Radius(shellRadius - .5).Clip().
				Children(func() {
					ui.Row(c).Height(48).FillWidth().Padding(0, 8, 0, 16).AlignItems(ui.Center).Children(func() {
						ui.Text(c, text("tasksTitle")).FontSize(fontBody).FontWeight(600).
							TextColor(p.text).Grow(1)
						iconButton(c, p, "tasks-close", "x", text("titleCloseTaskPanel"), false, func() {
							s.setTasksOpen(false)
						})
					})

					ui.Scroll(c).Grow(1).Padding(0, 16, 16, 16).Children(func() {
						ui.Column(c).FillWidth().Gap(24).Children(func() {
							s.taskCurrentRunV12(c, p)
							s.taskQueueV12(c, p)
							s.taskUsageV12(c, p)
							s.taskOtherSessionsV12(c, p)
							s.taskDiagnosticsV12(c, p)
							ui.Text(c, text("tasksOwnership")).FontSize(fontCaption).
								LineHeight(1.5).TextColor(p.muted)
						})
					})
				})
		})
	})
}

func (s *appState) taskSectionV12(c *ui.Context, p palette, title string, build func()) {
	ui.Column(c).FillWidth().Gap(0).Children(func() {
		ui.Text(c, title).FontSize(fontSmall).FontWeight(500).LineHeight(1.5).
			TextColor(p.muted).Margin(0, 0, 8, 0)
		build()
	})
}

func taskRowV12(c *ui.Context, p palette, title, value string) {
	ui.Row(c).FillWidth().MinHeight(28).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, title).FontSize(fontBody).LineHeight(1.5).TextColor(p.text).
			Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
		if value != "" {
			ui.Text(c, value).FontSize(fontCaption).FontFeatures("tnum").
				TextColor(p.muted).Shrink(0)
		}
	})
}

func taskSubRowV12(c *ui.Context, p palette, iconValue func() *ui.SVG, title, value string) {
	ui.Row(c).FillWidth().MinHeight(28).Padding(0, 0, 0, 14).
		AlignItems(ui.Center).Gap(8).Children(func() {
		if iconValue != nil {
			ui.Icon(c, iconValue()).Size(14, 14).Shrink(0).
				TextColor(p.muted)
		}
		ui.Text(c, title).FontSize(fontBody).LineHeight(1.5).
			TextColor(navSecondaryText(p)).
			Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
		if value != "" {
			ui.Text(c, value).FontSize(fontCaption).FontFeatures("tnum").
				TextColor(p.muted).Shrink(0)
		}
	})
}

func taskKVRowV12(c *ui.Context, p palette, key, value string) {
	ui.Row(c).FillWidth().MinHeight(21).AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Text(c, key).FontSize(fontBody).LineHeight(1.5).
			TextColor(navSecondaryText(p)).Grow(1).MinWidth(0)
		ui.Text(c, value).FontSize(fontBody).LineHeight(1.5).
			FontFeatures("tnum").TextColor(p.text).Shrink(0).TextAlign(ui.End)
	})
}

func (s *appState) taskCurrentRunV12(c *ui.Context, p palette) {
	title := text("tasksCurrentRun") + " · " + s.sessionDisplayTitle()
	s.taskSectionV12(c, p, title, func() {
		message := s.latestAssistantMessage()
		status := s.runLabel()
		var statusIcon *ui.SVG
		if message != nil && !message.endedAt.IsZero() && !s.runActive() {
			status = text("chatCompleted")
			statusIcon = icon("check")
		}
		duration := ""
		if message != nil && !message.startedAt.IsZero() {
			end := message.endedAt
			if end.IsZero() {
				end = c.Now()
			}
			seconds := max(0, int(end.Sub(message.startedAt).Seconds()))
			duration = textArgs("chatDurationSeconds", map[string]any{"seconds": seconds})
		}
		ui.Row(c).FillWidth().MinHeight(28).AlignItems(ui.Center).Gap(8).Children(func() {
			if statusIcon != nil {
				ui.Icon(c, statusIcon).Size(12, 12).Shrink(0).TextColor(p.success)
			}
			ui.Text(c, status).FontSize(fontBody).LineHeight(1.5).
				TextColor(p.text).Grow(1).
				MinWidth(0).SingleLine().Ellipsis("…")
			if duration != "" {
				ui.Text(c, duration).FontSize(fontCaption).FontFeatures("tnum").
					TextColor(p.muted).Shrink(0)
			}
		})
		if message == nil {
			return
		}
		for _, block := range message.blocks {
			if block.kind != chatBlockTool {
				continue
			}
			taskSubRowV12(c, p, toolCallIcon(block.tool.name),
				taskToolDescription(block.tool), taskToolMetric(block.tool))
		}
	})
}

func (s *appState) latestAssistantMessage() *chatMessage {
	for index := len(s.chat) - 1; index >= 0; index-- {
		if s.chat[index].role == chatAssistant {
			return &s.chat[index]
		}
	}
	return nil
}

func taskToolDescription(tool toolCallView) string {
	target := singleLineText(strings.TrimSpace(tool.arguments))
	name := strings.ToLower(strings.TrimSpace(tool.name))
	switch name {
	case "read", "file":
		if target != "" {
			target = filepath.Base(target)
		}
		return textArgs("tasksToolRead", map[string]any{"target": target})
	case "grep", "find", "search", "ls":
		return textArgs("tasksToolSearch", map[string]any{"target": target})
	default:
		return textArgs("tasksToolGeneric", map[string]any{
			"tool":   tool.name,
			"target": target,
		})
	}
}

func taskToolMetric(tool toolCallView) string {
	output := strings.TrimSpace(fallback(tool.result, tool.partial))
	count := 0
	if output != "" {
		count = strings.Count(output, "\n") + 1
	}
	switch strings.ToLower(strings.TrimSpace(tool.name)) {
	case "read", "file":
		if count > 0 {
			return textArgs("tasksToolLines", map[string]any{"count": count})
		}
	case "grep", "find", "search", "ls":
		if count > 0 {
			return textArgs("tasksToolMatches", map[string]any{"count": count})
		}
	}
	if tool.status == toolCallRunning {
		return toolCallStatusLabel(tool.status)
	}
	if tool.durationMS > 0 {
		return formatToolDuration(tool.durationMS)
	}
	if tool.status != "" {
		return toolCallStatusLabel(tool.status)
	}
	return ""
}

func (s *appState) taskQueueV12(c *ui.Context, p palette) {
	s.taskSectionV12(c, p, text("tasksQueueTitle"), func() {
		count := s.queueItemCount()
		if count == 0 {
			ui.Text(c, text("tasksQueueEmpty")).FontSize(fontCaption).
				TextColor(p.muted)
			return
		}
		taskRowV12(c, p, textArgs("tasksQueueCount", map[string]any{"count": count}), "")
	})
}

func (s *appState) taskUsageV12(c *ui.Context, p palette) {
	s.taskSectionV12(c, p, text("tasksUsageTitle"), func() {
		stats := s.hud.stats
		if stats == nil {
			ui.Text(c, text("tasksUsageEmpty")).FontSize(fontBody).TextColor(p.muted)
			return
		}
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			taskKVRowV12(c, p, text("tasksUsageInputOutput"),
				formatPiTokens(stats.Tokens.Input)+" / "+formatPiTokens(stats.Tokens.Output))
			if rate, ok := piCacheHitRate(stats); ok {
				taskKVRowV12(c, p, text("tasksUsageCache"),
					fmt.Sprintf("%d%%", int64(math.Round(rate))))
			}
			taskKVRowV12(c, p, text("tasksUsageCost"), fmt.Sprintf("$%.3f", stats.Cost))
			window := piContextWindow(stats, s.hud.contextWindowFallback)
			contextValue := ""
			if window > 0 {
				contextValue = formatPiTokens(stats.Tokens.Input) + " / " + formatPiTokens(int64(window))
			}
			taskKVRowV12(c, p, text("tasksUsageContext"), contextValue)
			if percent, ok := piContextPercent(stats); ok {
				ui.Box(c).FillWidth().Height(4).Radius(2).Margin(2, 0, 0, 0).
					Background(p.text.Alpha(.08)).Children(func() {
					ui.Box(c).WidthPercent(float32(min(max(percent, 0), 100))).
						Height(4).Radius(2).Background(p.accent)
				})
			}
		})
	})
}

func (s *appState) taskOtherSessionsV12(c *ui.Context, p palette) {
	others := make([]piSessionSummary, 0, len(s.piSessions))
	for _, summary := range s.piSessions {
		if s.isActivePiSession(summary) {
			continue
		}
		others = append(others, summary)
		if len(others) == 5 {
			break
		}
	}
	if len(others) == 0 {
		return
	}
	s.taskSectionV12(c, p, text("tasksOtherSessionsTitle"), func() {
		for _, summary := range others {
			taskSubRowV12(c, p, nil, untitledSessionLabel(summary.Title),
				text("chatCompleted"))
		}
	})
}

func (s *appState) taskDiagnosticsV12(c *ui.Context, p palette) {
	if s.piConnectionErrorActive() {
		s.taskSectionV12(c, p, text("tasksRuntimeDiagnostics"), func() {
			taskRowV12(c, p, s.runtimeDiagnosticLabel(), "")
		})
	}
	if len(s.pluginTasks) == 0 {
		return
	}
	s.taskSectionV12(c, p, text("tasksPluginRegistry"), func() {
		taskRowV12(c, p, s.pluginRegistryTaskLabel(), s.pluginRegistryTaskDetail())
		keys := s.pluginTaskKeys()
		for _, key := range keys {
			update := s.pluginTasks[key]
			taskRowV12(c, p, update.Title, fallback(update.Status, strings.ToUpper(update.Tone)))
		}
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
		Border(runtimeTokens.Dimension.Border.Value, p.border).Background(p.background).Children(func() {
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
			ui.Text(c, eyebrow).FontSize(fontCaption).FontWeight(700).TextColor(p.accentText)
		}
		ui.Text(c, title).FontSize(fontPageTitle).FontWeight(650).TextColor(p.text)
		ui.Text(c, description).FontSize(fontBody).LineHeight(1.45).TextColor(p.muted).MaxWidth(680)
	})
}

func statusPill(c *ui.Context, p palette, label string, ccolor ui.Color) {
	ui.Row(c).Padding(4, 9).Radius(999).
		Border(runtimeTokens.Dimension.Border.Value, p.border).
		Background(p.surface).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Background(ccolor)
		ui.Text(c, label).FontSize(fontMicro).FontWeight(700).TextColor(p.muted)
	})
}

func iconButton(c *ui.Context, p palette, key, iconName, label string, selected bool, click func()) {
	iconButtonDisabled(c, p, key, iconName, label, selected, false, click)
}

func iconButtonDisabled(
	c *ui.Context,
	p palette,
	key, iconName, label string,
	selected, disabled bool,
	click func(),
) {
	bg := ui.Color{}
	fg := p.muted
	if selected {
		bg = p.surfaceHover
		fg = p.accent
	}
	btn := ui.ButtonBase(c.Key(key)).Size(34, 34).Radius(8).Background(bg).
		Center().Cursor(ui.CursorPointer).Label(label).Tooltip(label).Disabled(disabled)
	if !selected && !disabled && btn.Hovered() {
		btn.Background(p.surfaceHover)
	}
	btn.Children(func() {
		ui.Icon(c, icon(iconName)).Size(17, 17).TextColor(fg)
	})
	if !disabled && btn.Clicked() {
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
		ui.Icon(c, icon("send")).Size(16, 16).TextColor(p.onAccent)
	})
	if btn.Clicked() {
		click()
	}
}

func stopButton(c *ui.Context, p palette, click func()) {
	btn := ui.ButtonBase(c.Key("stop")).Size(34, 34).Radius(8).Background(p.danger).
		Center().Cursor(ui.CursorPointer).Label(text("chatStopCurrentRun")).Tooltip(text("chatStopTooltip"))
	btn.Children(func() {
		ui.Icon(c, icon("stop")).Size(15, 15).TextColor(p.onAccent)
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
			ui.Icon(c, icon(iconName)).Size(14, 14).TextColor(p.onAccent)
			ui.Text(c, label).FontSize(fontSmall).FontWeight(650).TextColor(p.onAccent)
		})
	})
	if btn.Clicked() {
		click()
	}
}

func secondaryAction(c *ui.Context, p palette, key, label string, click func()) {
	btn := ui.ButtonBase(c.Key(key)).Padding(7, 11).Radius(7).
		Border(runtimeTokens.Dimension.Border.Value, p.border).
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
	ui.Row(c).Height(36).Gap(7).Padding(0, 10).Radius(8).
		Border(runtimeTokens.Dimension.Border.Value, p.border).
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

func (s *appState) piSecondaryLabel() string {
	if message := s.piConnectionErrorText(); message != "" {
		return message
	}
	switch s.piConn {
	case piConnected:
		if s.piModel != "" {
			return textArgs("piSecondaryConnectedModel", map[string]any{"model": s.piModel})
		}
		return text("piSecondaryRPCReady")
	case piConnecting:
		return text("piSecondaryStarting")
	case piConnectionErr:
		return text("piSecondaryConnectionFailed")
	default:
		return text("piSecondaryClickToConnect")
	}
}

func (s *appState) piConnectionErrorActive() bool {
	return s.piErrorCode != "" && (s.piConn == piConnectionErr || s.piConn == piDisconnected)
}

func (s *appState) piConnectionErrorText() string {
	switch s.piErrorCode {
	case "":
		return ""
	case piErrorCodeUnknown:
		raw := fallback(strings.TrimSpace(s.piError), text("errorUnknown"))
		return textArgs("errorPiUnknown", map[string]any{"error": raw})
	default:
		return text(s.piErrorCode)
	}
}

func (s *appState) connectionStateLabel() string {
	if s.piConnectionErrorActive() {
		return text("connectionError")
	}
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
	if s.piConnectionErrorActive() {
		return p.danger
	}
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
			retry += " - " + summarizeLine(s.retryError, 120)
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
			source += " - " + s.extensionEvent
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
