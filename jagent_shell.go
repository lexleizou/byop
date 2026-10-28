package main

import (
	"runtime"

	"github.com/egoist/mygo/ui"
)

func (s *appState) jAgentShell(c *ui.Context, p palette, dark, high, reduceTransparency bool) {
	ui.Column(c).Fill().Background(p.background).Children(func() {
		s.wallpaperLayer(c, p, dark, high, reduceTransparency)
		ui.Row(c).Absolute().Top(0).Right(0).Bottom(0).Left(0).
			Fill().Gap(0).AlignItems(ui.Stretch).Children(func() {
			s.jAgentNavigation(c, p, high, reduceTransparency)
			s.jAgentMain(c, p)
			s.taskPanel(c, p)
		})
	})
}

func (s *appState) jAgentNavigation(c *ui.Context, p palette, high, reduceTransparency bool) {
	windowWidth, _ := c.Size()
	collapsed := s.navigationCollapsedForWidth(windowWidth)
	width := s.navigationWidth(windowWidth)
	if collapsed && s.navResizeDragging {
		s.navResizeDragging = false
		if err := s.persistNavigationWidth(); err != nil {
			s.showNotice(text("noticePreferenceSaveNavigationWidth"))
		}
	}
	duration := s.panelAnimationDuration(currentAppearanceReduceMotion())
	if s.navResizeDragging {
		duration = 0
	}
	nav := ui.Column(c).Key("j-agent-navigation")
	animatedWidth := nav.AnimateWith("navigation-width", width, duration, ui.EaseOut)
	nav.Width(animatedWidth).FillHeight().Shrink(0)
	nav.Children(func() {
		surface := ui.Column(c).FillHeight().FillWidth().MinHeight(0).
			BorderWidth(0, 1, 0, 0).BorderColor(ui.Hex("#32353c"))
		if high || reduceTransparency {
			surface.Background(ui.Hex("#171c26"))
		} else {
			surface.Material(blurredTintMaterial{blur: 24, tint: ui.RGBA(23, 28, 38, .70)})
		}
		surface.Children(func() {
			ui.Column(c).FillHeight().FillWidth().MinHeight(0).Children(func() {
				s.jAgentNavigationHeader(c, p, collapsed)
				ui.Column(c).Grow(1).FillWidth().MinHeight(0).Padding(0, 8).
					Children(func() {
						ui.Column(c).FillWidth().Shrink(0).Gap(p.navGap).Children(func() {
							if !collapsed {
								s.newChatNavButton(c, p)
							}
							for _, item := range navItems {
								s.navButton(c, p, item)
							}
							pluginItems := s.pluginNavigationItems()
							if len(pluginItems) > 0 {
								if !collapsed {
									ui.Text(c, text("navPluginsSection")).FontSize(p.captionFont).
										TextColor(p.muted).Padding(4, 8)
								}
								for _, item := range pluginItems {
									s.pluginNavButton(c, p, item)
								}
							}
						})
						if !collapsed && s.chatCapablePage() {
							s.sessionsNavigation(c, p)
						} else {
							ui.Spacer(c).Grow(1)
						}
					})
				s.jAgentNavigationFooter(c, p)
			})
		})
		if !collapsed {
			s.jAgentNavigationResizeHandle(c, p, windowWidth)
		}
	})
}

func (s *appState) jAgentNavigationHeader(c *ui.Context, p palette, collapsed bool) {
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

func (s *appState) jAgentNavigationFooter(c *ui.Context, p palette) {
	collapsed := s.navigationVisuallyCollapsed(c)
	ui.Column(c).FillWidth().Shrink(0).Children(func() {
		ui.Box(c).Height(1).FillWidth().Background(ui.Hex("#32353c"))
		footer := ui.Column(c).FillWidth().Shrink(0)
		if collapsed {
			footer.Padding(8).Gap(8).AlignItems(ui.Center).Children(func() {
				s.navigationGlassButton(c, p)
				s.settingsGearButton(c, p)
			})
			return
		}
		footer.Padding(8, 8, 8, 16).Gap(4).Children(func() {
			ui.Row(c).FillWidth().Gap(4).AlignItems(ui.Center).Children(func() {
				s.integrationStatus(c, p)
				s.navigationGlassButton(c, p)
				s.settingsGearButton(c, p)
			})
		})
	})
}

func (s *appState) jAgentNavigationResizeHandle(c *ui.Context, p palette, windowWidth float32) {
	handle := ui.Box(c).Key("navigation-resize").Absolute().Top(0).Bottom(0).
		Right(0).Width(6).Cursor(ui.CursorResizeEW).
		Label(text("navigationResize"))
	dx, _, held := handle.Dragged()
	if held && !s.navResizeDragging {
		s.navResizeDragging = true
	}
	if held {
		shown := s.navigationWidth(windowWidth)
		next := normalizeJAgentNavExpandedWidth(min(shown+dx, s.navExpandedWidthLimit(windowWidth)))
		if next != shown {
			s.jAgentNavWidth = next
			s.preferences.JAgentNavExpandedWidth = next
		}
	} else if s.navResizeDragging {
		s.navResizeDragging = false
		if err := s.persistNavigationWidth(); err != nil {
			s.showNotice(text("noticePreferenceSaveNavigationWidth"))
		}
	}
	if held {
		handle.Draw(func(painter *ui.Painter, r ui.Rect) {
			painter.Fill(ui.Rect{X: r.X + r.W - 1, Y: r.Y, W: 1, H: r.H}, p.focus, 0)
		})
	}
}

func (s *appState) jAgentMain(c *ui.Context, p palette) {
	ui.Column(c).Key("j-agent-main").Grow(1).MinWidth(0).MinHeight(0).FillHeight().Children(func() {
		s.jAgentTitleBar(c, p)
		content := ui.Column(c).Grow(1).FillWidth().MinHeight(0)
		if s.page != pageChat {
			content.Background(p.background)
		}
		content.Children(func() {
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
		})
	})
}

func (s *appState) jAgentTitleBar(c *ui.Context, p palette) {
	bar := c.TitleBar()
	height := float32(shellTitleBarHeight)
	rightPadding := float32(12)
	if runtime.GOOS != "darwin" {
		height = max(bar.Height, shellTitleBarHeight)
		rightPadding = bar.Right + 12
	}
	titleBar := ui.Row(c).Height(height).FillWidth().Padding(0, rightPadding).DragWindow().
		AlignItems(ui.Center).Gap(8)
	if s.page == pageChat {
		titleBar.LinearGradient(ui.LinearGradient{
			Angle: 180,
			Stops: []ui.GradientStop{
				{Color: ui.RGBA(10, 12, 15, .62), At: 0},
				{Color: ui.RGBA(10, 12, 15, .34), At: .72},
				{Color: ui.Transparent, At: 1},
			},
		})
	} else {
		titleBar.Background(p.background).
			BorderWidth(0, 0, 0.5, 0).BorderColor(p.border)
	}
	titleBar.Children(func() {
		ui.Box(c).Width(4).Shrink(0).FillHeight().DragWindow()
		brandMark(c, p, 17)
		ui.Column(c).Grow(1).MinWidth(0).FillHeight().Justify(ui.Center).
			DragWindow().Children(func() {
			s.titleBarHeading(c, p)
		})
		taskPanelToggleButton(c, p, s.tasksOpen, func() {
			s.setTasksOpen(!s.tasksOpen)
		})
	})
}
