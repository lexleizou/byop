package main

import (
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/components/chat"
	lu "github.com/oneo-me/onevra/source/icons/lucide"
)

const (
	v12BorderWidth float32 = 0.5
	v12ChipRadius  float32 = 6
	v12InputRadius float32 = 8
)

// popoverV12 replaces Onevra's glass-and-1px popover while preserving MyGo's
// positioning, focus order and Escape/outside-press dismissal.
func popoverV12(c *ui.Context, anchor ui.Element, open *bool, opaque bool, build func(panel ui.Element)) ui.Element {
	return popoverWidthV12(c, anchor, open, opaque, 260, build)
}

// popoverWidthV12 is popoverV12 with an explicit panel width, for popovers
// whose content is wider than the shared default.
func popoverWidthV12(c *ui.Context, anchor ui.Element, open *bool, opaque bool, width float32,
	build func(panel ui.Element),
) ui.Element {
	t := c.Theme()
	p := themePalette(t.Dark)
	return ui.PopoverBase(c, anchor, open, func(panel ui.Element) {
		panel.Column().Width(width).Padding(6).Gap(0).Margin(4, 0, 0, 0).
			Radius(12).TextColor(t.Text).
			Shadow(runtimeTokens.Shadow.Overlay.OffsetX.Value, runtimeTokens.Shadow.Overlay.OffsetY.Value,
				runtimeTokens.Shadow.Overlay.Blur.Value, runtimeTokens.Shadow.Overlay.Spread.Value,
				p.shadowOverlay)
		if opaque {
			panel.Border(runtimeTokens.Dimension.Border.Value, t.Border).Background(t.Surface)
		} else {
			panel.Material(productGlassWithTint(p.surface.Alpha(.94)))
		}
		if build != nil {
			build(panel)
		}
	})
}

func searchFieldV12(c *ui.Context, value *string, label, placeholder string) ui.Element {
	t := c.Theme()
	border := t.Border
	input := ui.TextInputBase(c, value).FillWidth().Height(36).Padding(0, 10, 0, 34).
		Radius(v12InputRadius).Border(v12BorderWidth, border).Background(t.Surface).
		TextColor(t.Text).Label(label).Placeholder(placeholder).FontSize(fontBody).AutoFocus()
	if input.Focused() {
		input.BorderColor(t.Focus)
	} else if input.Hovered() {
		input.BorderColor(t.Border.Mix(t.Text, .16))
	}
	input.DrawOver(func(p *ui.Painter, r ui.Rect) {
		p.Icon(lu.Search(), ui.Rect{X: r.X + 10, Y: r.Y + (r.H-14)/2, W: 14, H: 14}, t.TextMuted)
	})
	return input
}

// chipV12 is the small control used by composer accessories. Its label stays
// whole; accessory rows can wrap it rather than silently hiding identity.
func chipV12(c *ui.Context, label string, options chat.ChipOptions) ui.Element {
	t := c.Theme()
	var b ui.Element
	if options.ReadOnly {
		b = ui.Row(c).Justify(ui.Center).Shrink(0).Cursor(ui.CursorNotAllowed)
	} else {
		b = ui.ButtonBase(c)
	}
	b.Label(label).Height(26).MinWidth(0).Shrink(1).Padding(0, 7).Radius(v12ChipRadius).
		AlignItems(ui.Center).Gap(6).Background(ui.Color{}).TextColor(t.TextMuted)
	if !options.ReadOnly && (b.Hovered() || b.Focused()) {
		b.Background(t.SurfaceHover)
	}
	if b.IsDisabled() {
		b.Opacity(.7)
	}
	b.Children(func() {
		switch {
		case options.Leading != nil:
			options.Leading(c, 14)
		case options.Image != nil:
			ui.Image(c, options.Image).Size(14, 14).Fit(ui.Contain).Radius(3).Shrink(0)
		case options.Icon != nil:
			components.Icon(c, options.Icon).Size(12, 12).Shrink(0).TextColor(t.TextMuted)
		}
		components.Text(c, label).MinWidth(0).Wrap().
			FontSize(fontSmall).TextColor(t.TextMuted)
		if options.Caret && !options.ReadOnly {
			components.Icon(c, lu.ChevronDown).Size(10.5, 10.5).Shrink(0).
				TextColor(t.TextMuted.Alpha(.7))
		}
	})
	return b
}

// effortPickerV12 keeps Onevra's discrete slider interaction while replacing
// its violet effects with the product's semantic accent.
func (s *appState) effortPickerV12(c *ui.Context, state *chat.EffortState, options chat.EffortPickerOptions,
	opaque bool,
) chat.PickerResult {
	if state == nil || len(options.Levels) == 0 {
		return chat.PickerResult{}
	}
	t := c.Theme()
	selected := 0
	for i, option := range options.Levels {
		if option.ID == options.Selected {
			selected = i
		}
	}
	level := options.Levels[selected].Label
	trigger := chipV12(c, level, chat.ChipOptions{Icon: options.Icon, Caret: true})
	trigger.Label(options.Label).Value(level)
	if state.Open {
		trigger.Background(t.SurfaceHover)
	}
	tooltip := options.Tooltip
	if tooltip == "" {
		tooltip = options.Label
	}
	components.Tooltip(c, trigger, tooltip)
	if trigger.Clicked() {
		state.Open = !state.Open
	}

	var chosen string
	width := options.Width
	if width <= 0 {
		width = 280
	}
	popoverV12(c, trigger, &state.Open, opaque, func(panel ui.Element) {
		const padding float32 = 14
		names := make([][]ui.Glyph, len(options.Levels))
		slots := make([]float32, len(options.Levels))
		total := float32(0)
		for i, option := range options.Levels {
			names[i] = ui.Shape(option.Label, effortNameFontV12(i == selected))
			slots[i] = glyphAdvanceV12(names[i])
			total += slots[i]
		}
		width = max(width, total+float32(len(options.Levels)-1)*12+2*padding)
		inner := width - 2*padding
		starts, ends := make([]float32, len(options.Levels)), make([]float32, len(options.Levels))
		gap := float32(0)
		if len(options.Levels) > 1 {
			gap = (inner - total) / float32(len(options.Levels)-1)
		}
		for i, pos := 0, float32(0); i < len(options.Levels); i++ {
			starts[i], ends[i] = pos, (pos+slots[i])/inner
			pos += slots[i] + gap
		}
		ends[len(ends)-1] = 1
		if options.Off {
			ends[0] = 0
		}
		panel.Width(width).Padding(padding).Gap(10)
		panel.Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				components.Text(c, options.Label).Grow(1).MinWidth(0).MaxLines(2).
					FontSize(fontBody).TextColor(t.Text)
				components.Text(c, level).Shrink(0).FontSize(fontSmall).
					FontWeight(600).TextColor(themePalette(t.Dark).accentText)
			})
			value := float64(selected)
			bar := ui.SliderBase(c, &value, 0, float64(len(options.Levels)-1)).
				Step(1).Label(options.Label).Value(level).
				Size(inner, effortBarHeightV12+effortNameGapV12+effortNameHeightV12).
				FocusRing(false).Cursor(ui.CursorPointer).AutoFocus()
			if bar.Changed() {
				if i := int(math.Round(value)); i >= 0 && i < len(options.Levels) &&
					options.Levels[i].ID != options.Selected {
					chosen = options.Levels[i].ID
				}
			}
			bar.Draw(func(p *ui.Painter, r ui.Rect) {
				paintEffortV12(p, r, effortPaintV12{
					fraction: effortEndV12(ends, bar.Animate("fill", float32(selected),
						s.effortFillDuration(currentAppearanceReduceMotion()))),
					hovered:  bar.Hovered(),
					focus:    bar.FocusVisible(),
					rtl:      c.Direction() == ui.RightToLeft,
					names:    names,
					starts:   starts,
					slots:    slots,
					selected: selected,
				})
			})
		})
	})
	return chat.PickerResult{Trigger: trigger, Chosen: chosen}
}

func (s *appState) effortFillDuration(systemReduceMotion bool) time.Duration {
	if s.reducedMotionActive(systemReduceMotion) {
		return 0
	}
	return runtimeMotionDuration()
}

func runtimeMotionDuration() time.Duration {
	duration := runtimeTokens.Motion.Duration.Value
	if duration <= 0 {
		return 0
	}
	return time.Duration(duration * float32(time.Millisecond))
}

func effortNameFontV12(selected bool) ui.Font {
	if selected {
		return ui.Font{Size: fontSmall, Weight: 600}
	}
	return ui.Font{Size: fontSmall}
}

func glyphAdvanceV12(glyphs []ui.Glyph) float32 {
	if len(glyphs) == 0 {
		return 0
	}
	last := glyphs[len(glyphs)-1]
	return last.X + last.Advance
}

func effortEndV12(ends []float32, index float32) float32 {
	i := int(math.Floor(float64(index)))
	if i < 0 {
		return ends[0] * max(0, 1+index)
	}
	if i >= len(ends)-1 {
		return ends[len(ends)-1]
	}
	f := index - float32(i)
	return ends[i] + (ends[i+1]-ends[i])*f
}

const (
	effortBarHeightV12  float32 = 18
	effortNameGapV12    float32 = 8
	effortNameHeightV12 float32 = 16
)

type effortPaintV12 struct {
	fraction            float32
	hovered, focus, rtl bool
	names               [][]ui.Glyph
	starts, slots       []float32
	selected            int
}

func paintEffortV12(p *ui.Painter, r ui.Rect, e effortPaintV12) {
	t := p.Theme()
	track := ui.Rect{X: r.X, Y: r.Y, W: r.W, H: effortBarHeightV12}
	radius := track.H / 2
	at := func(u float32) float32 {
		if e.rtl {
			return track.X + track.W - u
		}
		return track.X + u
	}
	span := func(u0, u1 float32) ui.Rect {
		x0, x1 := at(u0), at(u1)
		if x0 > x1 {
			x0, x1 = x1, x0
		}
		return ui.Rect{X: x0, Y: track.Y, W: x1 - x0, H: track.H}
	}
	trackColor := t.Text.Alpha(.08)
	if e.hovered {
		trackColor = t.Text.Alpha(.12)
	}
	p.Fill(track, trackColor, radius)
	if end := track.W * clampUnitV12(e.fraction); end > 0 {
		body := span(0, max(end, 2*radius))
		p.Fill(body, t.Accent, radius)
	}
	if e.focus {
		p.FocusRing(track, [4]float32{radius, radius, radius, radius})
	}

	n := len(e.names)
	metrics := effortNameFontV12(false).Metrics()
	baseline := track.Y + track.H + effortNameGapV12 +
		(effortNameHeightV12-metrics.Ascent-metrics.Descent)/2 + metrics.Ascent
	for i, glyphs := range e.names {
		ink := t.TextMuted
		if i == e.selected {
			ink = t.Accent
		}
		w := glyphAdvanceV12(glyphs)
		u := e.starts[i] + (e.slots[i]-w)/2
		switch {
		case i == 0:
			u = e.starts[i]
		case i == n-1:
			u = e.starts[i] + e.slots[i] - w
		}
		x := track.X + u
		if e.rtl {
			x = track.X + track.W - u - w
		}
		p.Glyphs(glyphs, x, baseline, ink)
	}
}

func clampUnitV12(value float32) float32 {
	switch {
	case value < 0:
		return 0
	case value > 1:
		return 1
	default:
		return value
	}
}

// pickerV12 keeps Onevra's grouped, searchable picker behavior while using the
// shared product surface and type scale.
func pickerV12(c *ui.Context, state *chat.PickerState, options chat.PickerOptions,
	opaque bool,
) chat.PickerResult {
	if state == nil {
		return chat.PickerResult{}
	}
	t := c.Theme()
	trigger := chipV12(c, options.Label, chat.ChipOptions{
		Icon: options.Icon, Image: options.Image, Leading: options.Leading, Caret: true,
	})
	if state.Open {
		trigger.Background(t.SurfaceHover)
	}
	if trigger.Clicked() {
		state.Open, state.Query = !state.Open, ""
	}
	var chosen string
	width := options.Width
	if width <= 0 {
		width = 260
	}
	threshold := options.Searchable
	if threshold <= 0 {
		threshold = 8
	}
	popoverV12(c, trigger, &state.Open, opaque, func(panel ui.Element) {
		panel.Label(options.Label).Width(width).Padding(0).Gap(0).Children(func() {
			if options.SearchLabel != "" && len(options.Options) > threshold {
				ui.Row(c).FillWidth().Padding(8, 8, 0, 8).Children(func() {
					searchFieldV12(c, &state.Query, options.SearchLabel, options.SearchLabel)
				})
			}
			query := strings.ToLower(strings.TrimSpace(state.Query))
			var visible []chat.Option
			leading := false
			for _, option := range options.Options {
				leading = leading || option.Leading != nil || option.Image != nil || option.Icon != nil
				if query == "" || strings.Contains(strings.ToLower(
					option.Label+" "+option.Group+" "+option.Detail+" "+option.ID,
				), query) {
					visible = append(visible, option)
				}
			}
			if len(visible) == 0 {
				components.Text(c, options.EmptyText).FontSize(fontSmall).
					TextColor(t.TextMuted).Padding(12)
				return
			}
			components.ScrollView(c, components.ScrollViewOptions{}).FillWidth().
				MaxHeight(348).Padding(4).Gap(1).Children(func() {
				group := ""
				for i, option := range visible {
					if option.Group != "" && (i == 0 || option.Group != group) {
						components.Text(c, option.Group).FontSize(fontSmall).FontWeight(500).
							TextColor(t.TextMuted).Padding(8, 8, 3, 8)
					}
					group = option.Group
					if pickerRowV12(c, option, option.ID == options.Selected, leading) {
						chosen = option.ID
						state.Open = false
					}
				}
			})
		})
	})
	return chat.PickerResult{Trigger: trigger, Chosen: chosen}
}

func pickerRowV12(c *ui.Context, option chat.Option, selected, leading bool) bool {
	t := c.Theme()
	row := ui.ButtonBase(c).Key(option.ID).Label(option.Label).Selected(selected).
		FillWidth().MinWidth(0).Padding(6, 8).Radius(v12ChipRadius).
		Justify(ui.Start).AlignItems(ui.Center).Gap(8).
		Background(ui.Color{}).TextColor(t.Text)
	if row.Hovered() || row.Focused() {
		row.Background(t.SurfaceHover)
	}
	row.Children(func() {
		switch {
		case option.Leading != nil:
			option.Leading(c, 16)
		case option.Image != nil:
			ui.Image(c, option.Image).Size(16, 16).Fit(ui.Contain).Radius(3).Shrink(0)
		case option.Icon != nil:
			ui.Row(c).Size(16, 16).Shrink(0).Center().Children(func() {
				components.Icon(c, option.Icon).Size(13, 13).TextColor(t.TextMuted)
			})
		case leading:
			ui.Box(c).Size(16, 16).Shrink(0)
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			components.Text(c, option.Label).MinWidth(0).Wrap().FontSize(fontBody)
			if option.Description != "" {
				components.Text(c, option.Description).MinWidth(0).Wrap().
					FontSize(fontSmall).TextColor(t.TextMuted)
			}
		})
		if option.Detail != "" {
			components.Text(c, option.Detail).Shrink(0).FontSize(fontSmall).TextColor(t.TextMuted)
		}
		mark := ui.Box(c).Size(12, 12).Shrink(0)
		if selected {
			mark.Children(func() {
				components.Icon(c, lu.Check).Size(12, 12).TextColor(t.Accent)
			})
		}
	})
	return row.Clicked()
}

type composerEnterKeyV12 struct{}

func iconActionV12(c *ui.Context, label string, iconValue func() *ui.SVG) ui.Element {
	t := c.Theme()
	button := ui.ButtonBase(c).Label(label).Size(26, 26).Shrink(0).Radius(v12ChipRadius).
		Center().Background(ui.Color{}).TextColor(t.TextMuted)
	if button.Hovered() || button.Focused() {
		button.Background(t.SurfaceHover).TextColor(t.Text)
	}
	button.Children(func() { components.Icon(c, iconValue).Size(14, 14) })
	components.Tooltip(c, button, label)
	return button
}

// opaqueComposerV12 is the repository-owned composer used by the floating
// conversation. It mirrors chat.Composer's input contract while keeping the
// card opaque and free of Onevra's dependency shadow.
func opaqueComposerV12(c *ui.Context, draft *string,
	options chat.ComposerOptions,
	placeholderOverlays ...func(*ui.Context),
) chat.ComposerResult {
	var result chat.ComposerResult
	t := c.Theme()
	empty := strings.TrimSpace(*draft) == ""
	canSend := !empty && !options.SendDisabled
	result.Root = ui.Column(c).FillWidth().MinWidth(0).Shrink(0).Children(func() {
		card := ui.Column(c).FillWidth().MinWidth(0).MinHeight(96).
			Padding(12, 12, 10, 12).Gap(8).Radius(v12InputRadius).
			Border(v12BorderWidth, t.Border).Background(t.Surface).Children(func() {
			if options.Attachments != nil {
				ui.Row(c).Key("attachments").FillWidth().Wrap().Gap(8).
					Padding(0, 0, 4, 0).Children(func() { options.Attachments(c) })
			}
			result.Input = ui.TextAreaBase(c.Key("input"), draft).FillWidth().Lines(1, 10).
				MinHeight(36).Label(options.Label).Placeholder(options.Placeholder).
				Border(0, ui.Color{}).Background(ui.Color{}).Padding(0).
				FontSize(fontBody).TextColor(t.Text)
			if empty && len(placeholderOverlays) > 0 {
				ui.Row(c).Absolute().Top(12).Left(12).Right(12).Height(36).
					AlignItems(ui.Start).PassThrough().Children(func() {
					placeholderOverlays[0](c)
				})
			}
			if options.OnPaste != nil {
				result.Input.OnPaste(options.OnPaste)
			}
			ui.Row(c).FillWidth().MinWidth(0).MinHeight(28).
				AlignItems(ui.Center).Gap(4).Children(func() {
				ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(4).Children(func() {
					if options.Accessory != nil {
						options.Accessory(c)
					}
				})
				if options.Trailing != nil {
					ui.Row(c).Key("trailing").Shrink(0).AlignItems(ui.Center).Gap(4).
						Children(func() { options.Trailing(c) })
				}
				if options.Running && empty {
					result.Stop = composerIconActionV12(c, "running-stop", options.StopLabel,
						lu.CircleStop, t.Danger.Alpha(.12), t.Danger).Clicked()
				}
				if !options.Running || !empty {
					background, foreground := t.Accent, t.AccentText
					if !canSend {
						background, foreground = t.Text.Alpha(.08), t.TextMuted
					}
					ui.Box(c).Disabled(!canSend).Children(func() {
						if composerIconActionV12(c, "chatSend", options.SendLabel,
							lu.ArrowUp, background, foreground).Clicked() {
							result.Send = true
						}
					})
				}
			})
		})
		result.Card = card
		pending := ui.Local(result.Input, composerEnterKeyV12{}, func() bool { return false })
		composing := result.Input.Composing()
		result.Input.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputKeyDown || ev.Key != ui.KeyEnter || ev.Mods != 0 || composing {
				return false
			}
			*pending = true
			return true
		})
		if *pending {
			*pending = false
			result.Send = result.Send || canSend && !composing
		}
		if options.AcceptFiles {
			result.Dropped = card.DroppedFiles()
			result.Dragging = card.FileDragOver()
		}
		if len(options.DropOptions.Formats) > 0 {
			if drop, ok := ui.DropData(card, options.DropOptions); ok {
				result.Drop = &drop
			}
			_, over := ui.DataDragOver(card, options.DropOptions)
			result.Dragging = result.Dragging || over
		}
		if result.Dragging {
			card.BorderColor(t.Focus.Alpha(.7)).Background(t.Surface.Mix(t.Text, .06))
		} else if result.Input.Focused() {
			card.BorderColor(t.Focus)
		}
		if options.Footer != nil {
			ui.Row(c).Key("composer-footer").Label("composer-footer").FillWidth().MinWidth(0).Height(24).
				Padding(8, 0, 0, 0).AlignItems(ui.Center).Gap(2).
				Children(func() { options.Footer(c) })
		} else {
			// Keep the layout-test seam present without changing the resting
			// composer height when there is no status line to show.
			ui.Row(c).Key("composer-footer").Label("composer-footer").Absolute().
				Bottom(0).Left(0).Right(0).Height(1).PassThrough()
		}
	})
	return result
}

func composerIconActionV12(c *ui.Context, key, label string, iconValue func() *ui.SVG,
	background, foreground ui.Color,
) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Label(label).Size(28, 28).Shrink(0).
		Radius(v12InputRadius).Center().Background(background).TextColor(foreground)
	if b.Pressed() {
		b.Opacity(.8)
	} else if b.Hovered() {
		b.Opacity(.9)
	}
	b.Children(func() {
		components.Icon(c, iconValue).Size(16, 16).TextColor(foreground)
	})
	return b
}
