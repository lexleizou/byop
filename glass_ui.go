package main

import (
	"math"

	"github.com/egoist/mygo/ui"
)

func (s *appState) navigationGlassButton(c *ui.Context, p palette) {
	label := text("navGlassButton")
	selected := s.navGlassPopoverOpen
	bg, fg := ui.Color{}, navSecondaryText(p)
	if selected {
		bg, fg = p.text.Alpha(.10), p.text
	}
	button := ui.ButtonBase(c.Key("nav-glass-button")).Size(28, 28).Radius(8).
		Background(bg).Center().Label(label).Tooltip(label).Cursor(ui.CursorPointer)
	if !selected && button.Hovered() {
		button.Background(p.surfaceHover)
	}
	button.Children(func() {
		ui.Icon(c, icon("sliders-horizontal")).Size(16, 16).TextColor(fg)
	})
	if button.Clicked() {
		s.navGlassPopoverOpen = !s.navGlassPopoverOpen
	}
	opaque := s.highContrastActive(currentAppearanceHighContrast()) ||
		currentAppearanceReduceTransparency()
	popoverV12(c, button, &s.navGlassPopoverOpen, opaque, func(panel ui.Element) {
		panel.AttachTo(button, ui.AnchorBottomRight, ui.AnchorBottomLeft).
			Margin(0, 0, 90, 0).MarginStart(12)
		panel.Children(func() {
			s.navigationGlassPopover(c, p)
		})
	})
}

func (s *appState) navigationGlassPopover(c *ui.Context, p palette) {
	ui.Column(c).FillWidth().Gap(0).Children(func() {
		ui.Text(c, text("navGlassTitle")).FontSize(fontCaption).FontWeight(400).
			TextColor(p.muted).Padding(9, 8, 7, 8)
		s.navTransparencyControl(c, p, "", "popover")
		s.glassBlurControl(c, p, "", "popover")
		s.glassRefractionControl(c, p, "")
		if status := s.glassStatusText(); status != "" {
			ui.Text(c, status).FontSize(fontCaption).LineHeight(1.4).TextColor(p.muted)
		}
		ui.Box(c).FillWidth().Height(runtimeTokens.Dimension.Border.Value).
			Margin(6, 4).Background(p.text.Alpha(.12))
		moreLabel := text("navGlassMoreSettings")
		more := ui.ButtonBase(c.Key("nav-glass-more-settings")).FillWidth().
			Height(32).Justify(ui.Start).Padding(8).Radius(8).Background(ui.Color{}).
			Cursor(ui.CursorPointer).Label(moreLabel)
		if more.Hovered() {
			more.Background(p.surfaceHover)
		}
		more.Children(func() {
			ui.Text(c, moreLabel).FontSize(fontBody).TextColor(p.text).Grow(1)
			ui.Icon(c, icon("chevron-right")).Size(12, 12).TextColor(p.muted)
		})
		if more.Clicked() {
			s.navGlassPopoverOpen = false
			s.setPage(pageSettings)
			s.settingsSelectCategory(&s.settingsPageState, settingsCategoryAppearance)
		}
	})
}

func (s *appState) navTransparencyControl(c *ui.Context, p palette, description, instance string) {
	titleKey, valueKey := "settingsNavTransparencyTitle", "settingsValuePercent"
	if instance == "popover" {
		titleKey = "navGlassTransparencyTitle"
	}
	s.glassSliderControl(c, p,
		text(titleKey),
		textArgs(valueKey, map[string]any{
			"value": s.preferences.NavTransparency,
		}),
		description,
		&s.navTransparencySlider, 0, 100,
		text(titleKey),
		func(slider ui.Element) {
			s.trackNavTransparencySlider(slider, instance)
		},
	)
}

func (s *appState) glassBlurControl(c *ui.Context, p palette, description, instance string) {
	titleKey, valueKey := "settingsGlassBlurTitle", "settingsValuePixels"
	if instance == "popover" {
		titleKey, valueKey = "navGlassBlurTitle", "navGlassValuePixels"
	}
	s.glassSliderControl(c, p,
		text(titleKey),
		textArgs(valueKey, map[string]any{
			"value": s.preferences.GlassBlur,
		}),
		description,
		&s.glassBlurSlider, 0, runtimeTokens.Glass.BlurMax,
		text(titleKey),
		func(slider ui.Element) {
			s.trackGlassBlurSlider(slider, instance)
		},
	)
}

func (s *appState) glassSliderControl(
	c *ui.Context,
	p palette,
	title, valueText, description string,
	value *float64,
	lo, hi int,
	label string,
	track func(ui.Element),
) {
	ui.Column(c).FillWidth().Padding(8, 8, 4, 8).Gap(8).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).FontSize(fontBody).FontWeight(400).
				TextColor(p.text).Grow(1)
			ui.Text(c, valueText).FontSize(fontBody).
				TextColor(p.text)
		})
		slider := glassSliderV12(c, p, value, lo, hi, label)
		track(slider)
		if description != "" {
			ui.Text(c, description).FontSize(fontCaption).LineHeight(1.4).
				TextColor(p.muted)
		}
	})
}

func glassSliderV12(c *ui.Context, p palette, value *float64, lo, hi int, label string) ui.Element {
	slider := ui.SliderBase(c, value, float64(lo), float64(hi)).Step(1).
		Height(16).PaddingX(8).FillWidth().Label(label)
	slider.Draw(func(painter *ui.Painter, rect ui.Rect) {
		const thumbSize float32 = 16
		contentX := rect.X + thumbSize/2
		contentW := max(float32(0), rect.W-thumbSize)
		fraction := float32(0)
		if hi > lo {
			fraction = float32((*value - float64(lo)) / float64(hi-lo))
			fraction = min(max(fraction, 0), 1)
		}
		centerY := rect.Y + rect.H/2
		trackRect := ui.Rect{X: contentX, Y: centerY - 2, W: contentW, H: 4}
		painter.Fill(trackRect, p.text.Alpha(.16), 2)
		thumbX := contentX + contentW*fraction
		painter.Fill(ui.Rect{X: contentX, Y: centerY - 2, W: thumbX - contentX, H: 4},
			p.accent, 2)
		thumb := ui.Rect{
			X: thumbX - thumbSize/2,
			Y: centerY - thumbSize/2,
			W: thumbSize,
			H: thumbSize,
		}
		painter.Fill(thumb, ui.Hex("#f5f5f3"), thumbSize/2)
		painter.Stroke(thumb, p.text.Alpha(.18), thumbSize/2, 1)
	})
	return slider
}

func (s *appState) navTransparencyInlineV12(c *ui.Context, p palette) {
	s.glassSliderInlineV12(c, p,
		textArgs("settingsValuePercent", map[string]any{
			"value": s.preferences.NavTransparency,
		}),
		&s.navTransparencySlider, 0, 100,
		text("settingsNavTransparencyTitle"),
		func(slider ui.Element) {
			s.trackNavTransparencySlider(slider, "settings")
		},
	)
}

func (s *appState) glassBlurInlineV12(c *ui.Context, p palette) {
	s.glassSliderInlineV12(c, p,
		textArgs("settingsValuePixels", map[string]any{
			"value": s.preferences.GlassBlur,
		}),
		&s.glassBlurSlider, 0, runtimeTokens.Glass.BlurMax,
		text("settingsGlassBlurTitle"),
		func(slider ui.Element) {
			s.trackGlassBlurSlider(slider, "settings")
		},
	)
}

func (s *appState) glassSliderInlineV12(
	c *ui.Context,
	p palette,
	valueText string,
	value *float64,
	lo, hi int,
	label string,
	track func(ui.Element),
) {
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		slider := ui.Slider(c, value, float64(lo), float64(hi)).Step(1).
			Width(180).Label(label)
		track(slider)
		ui.Text(c, valueText).FontSize(fontBody).
			TextColor(p.text).Width(44).TextAlign(ui.End)
	})
}

func (s *appState) glassRefractionControl(c *ui.Context, p palette, description string) {
	value := s.preferences.GlassRefraction
	title := text("settingsGlassRefractionTitle")
	ui.Column(c).FillWidth().Padding(8, 8).Gap(8).Children(func() {
		ui.Row(c).FillWidth().MinHeight(18).AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).FontSize(fontBody).TextColor(p.text).Grow(1)
			toggle := ui.Switch(c, &value).Label(title)
			if toggle.Changed() {
				s.setGlassRefraction(value)
			}
		})
		if description != "" {
			ui.Text(c, description).FontSize(fontCaption).LineHeight(1.4).
				TextColor(p.muted)
		}
	})
}

func (s *appState) glassRefractionInlineV12(c *ui.Context, p palette) {
	value := s.preferences.GlassRefraction
	title := text("settingsGlassRefractionTitle")
	toggle := ui.Switch(c, &value).Size(30, 18).Radius(9).Label(title)
	if toggle.Changed() {
		s.setGlassRefraction(value)
	}
}

func (s *appState) reduceTransparencyInlineV12(c *ui.Context, p palette) {
	value := currentAppearanceReduceTransparency()
	title := text("settingsReduceTransparencyTitle")
	ui.Switch(c, &value).Size(30, 18).Radius(9).Label(title).
		Tooltip(text("settingsReduceTransparencyDescription")).Disabled(true)
}

func (s *appState) trackNavTransparencySlider(slider ui.Element, instance string) {
	value := min(max(int(math.Round(s.navTransparencySlider)), 0), 100)
	_, _, held := slider.Dragged()
	if slider.Changed() {
		s.navTransparencySlider = float64(value)
		if held {
			if s.navTransparencyDragOwner != instance {
				s.navTransparencyDragOwner = instance
				s.navTransparencyDragBase = s.preferences.NavTransparency
			}
			s.preferences.NavTransparency = value
			s.invalidateUI()
			return
		}
		previous := s.preferences.NavTransparency
		s.preferences.NavTransparency = value
		s.invalidateUI()
		if s.navTransparencyDragOwner == instance {
			s.navTransparencyDragOwner = ""
			previous = s.navTransparencyDragBase
		}
		s.persistNavTransparencySlider(previous)
		return
	}
	if s.navTransparencyDragOwner == instance && !held {
		s.navTransparencyDragOwner = ""
		s.persistNavTransparencySlider(s.navTransparencyDragBase)
	}
}

func (s *appState) persistNavTransparencySlider(previous int) {
	if err := s.persistPreferences(); err != nil {
		s.preferences.NavTransparency = previous
		s.navTransparencySlider = float64(previous)
		s.invalidateUI()
		s.showNotice(text("noticePreferenceSaveGlass"))
	}
}

func (s *appState) trackGlassBlurSlider(slider ui.Element, instance string) {
	value := min(max(int(math.Round(s.glassBlurSlider)), 0), runtimeTokens.Glass.BlurMax)
	_, _, held := slider.Dragged()
	if slider.Changed() {
		s.glassBlurSlider = float64(value)
		if held {
			if s.glassBlurDragOwner != instance {
				s.glassBlurDragOwner = instance
				s.glassBlurDragBase = s.preferences.GlassBlur
			}
			s.preferences.GlassBlur = value
			s.invalidateUI()
			return
		}
		previous := s.preferences.GlassBlur
		s.preferences.GlassBlur = value
		s.invalidateUI()
		if s.glassBlurDragOwner == instance {
			s.glassBlurDragOwner = ""
			previous = s.glassBlurDragBase
		}
		s.persistGlassBlurSlider(previous)
		return
	}
	if s.glassBlurDragOwner == instance && !held {
		s.glassBlurDragOwner = ""
		s.persistGlassBlurSlider(s.glassBlurDragBase)
	}
}

func (s *appState) persistGlassBlurSlider(previous int) {
	if err := s.persistPreferences(); err != nil {
		s.preferences.GlassBlur = previous
		s.glassBlurSlider = float64(previous)
		s.invalidateUI()
		s.showNotice(text("noticePreferenceSaveGlass"))
	}
}

func (s *appState) glassStatusText() string {
	dark := currentAppearanceDark()
	high := s.highContrastActive(currentAppearanceHighContrast())
	reduceTransparency := currentAppearanceReduceTransparency()
	if !dark {
		return text("settingsGlassDarkOnly")
	}
	if high || reduceTransparency {
		return text("settingsGlassSolidFallback")
	}
	return ""
}

func (s *appState) settingsWallpaperChoicesV12(c *ui.Context, p palette) {
	ui.Row(c).Gap(8).Children(func() {
		for _, choice := range []struct {
			value string
			key   string
		}{
			{"ember", "settingsWallpaperEmber"},
			{"jade", "settingsWallpaperJade"},
			{"graphite", "settingsWallpaperGraphite"},
			{"none", "settingsWallpaperNone"},
		} {
			s.wallpaperChoiceV12(c, p, choice.value, text(choice.key))
		}
	})
}

func (s *appState) wallpaperChoiceV12(c *ui.Context, p palette, value, label string) {
	selected := s.preferences.Wallpaper == value
	border := p.border
	borderWidth := runtimeTokens.Dimension.Border.Value
	if selected {
		border = p.accent
		borderWidth = 2
	}
	button := ui.ButtonBase(c.Key("settings-wallpaper-"+value)).Size(56, 38).
		Radius(8).Clip().BorderWidth(borderWidth).BorderColor(border).
		Background(p.background).Cursor(ui.CursorPointer).Label(label).Tooltip(label)
	button.Children(func() {
		if bitmap := wallpaperBitmap(value); bitmap != nil {
			ui.Image(c, bitmap).FillWidth().FillHeight().Fit(ui.Cover).Radius(8)
			return
		}
		ui.Box(c).Fill().Radius(8).Background(p.background)
	})
	if button.Clicked() {
		s.setWallpaper(value)
	}
}

func (s *appState) settingsGlassPreviewV12(c *ui.Context, p palette) {
	dark := currentAppearanceDark()
	high := s.highContrastActive(currentAppearanceHighContrast())
	reduceTransparency := currentAppearanceReduceTransparency()
	preview := ui.Row(c).Height(120).FillWidth().Radius(shellRadius).
		Border(runtimeTokens.Dimension.Border.Value, p.border).
		Background(p.readingSurface).Clip()
	preview.Children(func() {
		s.wallpaperBackdrop(c, dark, high, reduceTransparency)
		ui.Row(c).Absolute().Top(0).Right(0).Bottom(0).Left(0).
			Padding(8).Gap(8).Children(func() {
			navGlassCard(c, p, dark, high, reduceTransparency, shellRadius,
				s.preferences.NavTransparency, s.preferences.GlassBlur,
				s.preferences.GlassRefraction, func() {
					ui.Column(c).FillWidth().FillHeight().Padding(10).Gap(6).Children(func() {
						ui.Text(c, appShort).FontSize(12).FontWeight(650).TextColor(p.text)
						for range 3 {
							ui.Box(c).FillWidth().Height(6).Radius(4).
								Background(ui.Hex("#eeeeec").Alpha(.25))
						}
					})
				}).Width(120)
			ui.Column(c).Grow(1).FillHeight().Radius(8).
				Border(runtimeTokens.Dimension.Border.Value, p.border).
				Background(p.readingSurface)
		})
	})
}
