package main

import (
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

type blurredTintMaterial struct {
	blur float32
	tint ui.Color
}

func (m blurredTintMaterial) PaintMaterial(p *ui.Painter, box ui.Rect, radii [4]float32) {
	if m.blur > 0 {
		glass.Blur{Radius: m.blur}.PaintMaterial(p, box, radii)
	}
	p.Fill(box, m.tint, radii[0])
}

func jAgentGlassSurface(
	element ui.Element,
	p palette,
	radius, blur float32,
	tint ui.Color,
) ui.Element {
	element.Radius(radius).Border(0.5, p.border)
	if p.highContrast || p.reduceTransparency {
		return element.Background(p.surface)
	}
	return element.Material(blurredTintMaterial{blur: blur, tint: tint})
}

func boardUIRaisedShadow(p palette) ui.Color {
	if p.dark {
		return ui.RGBA(0, 0, 0, .18)
	}
	return ui.RGBA(0, 0, 0, .05)
}

func styleRaisedSurface(element ui.Element, p palette, radius float32) ui.Element {
	if p.preset == appearancePresetJAgent {
		return jAgentGlassSurface(element, p, radius, 14, p.raisedSurface)
	}
	if p.preset == appearancePresetBoardUI {
		// P31 §5: an opaque raised card with BoardUI's subtle shadow.
		return element.Radius(radius).
			Border(0.5, p.border).
			Background(p.raisedSurface).
			Shadow(0, 1, 2, 0, boardUIRaisedShadow(p))
	}
	return element.Radius(radius).
		Border(runtimeTokens.Dimension.Border.Value, p.border).
		Background(p.raisedSurface)
}

func styleComposerSurface(element ui.Element, p palette, radius float32) ui.Element {
	if p.preset == appearancePresetJAgent {
		return jAgentGlassSurface(element, p, radius, 14, p.composerSurface)
	}
	if p.preset == appearancePresetBoardUI {
		// P31 §5: the BoardUI composer is an opaque card with the preset
		// radius the caller passes.
		return element.Radius(radius).
			Border(0.5, p.border).
			Background(p.composerSurface).
			Shadow(0, 1, 2, 0, boardUIRaisedShadow(p))
	}
	return element.Radius(radius).
		Border(runtimeTokens.Dimension.Border.Value, p.border).
		Background(p.composerSurface)
}

func stylePopoverOpaque(
	element ui.Element,
	p palette,
	radius float32,
	surface, border ui.Color,
) ui.Element {
	element = popoverShadow(element, p, radius)
	return element.Border(runtimeTokens.Dimension.Border.Value, border).
		Background(surface)
}

func stylePopoverGlass(element ui.Element, p palette, radius float32) ui.Element {
	if p.preset == appearancePresetBoardUI {
		// P31 §5: BoardUI's popover is opaque, and its shadows are applied
		// here so popoverShadow never stacks its own stage shadow on top.
		first, second := ui.RGBA(0, 0, 0, .04), ui.RGBA(0, 0, 0, .02)
		if p.dark {
			first, second = ui.RGBA(0, 0, 0, .14), ui.RGBA(0, 0, 0, .10)
		}
		return element.Radius(radius).
			Border(0.5, p.popoverBorder).
			Background(p.popoverTint).
			Shadow(0, 1, 1, 0, first).
			Shadow(0, 4, 4, 0, second)
	}
	element = popoverShadow(element, p, radius)
	if p.preset == appearancePresetJAgent {
		return jAgentGlassSurface(element, p, radius, 16, p.popoverTint)
	}
	element.Material(productGlassWithTint(p.popoverTint))
	if p.dark {
		element.Border(runtimeTokens.Dimension.Border.Value, p.popoverBorder)
	}
	return element
}

func popoverShadow(element ui.Element, p palette, radius float32) ui.Element {
	element.Radius(radius)
	shadow := p.shadowOverlay
	if p.dark {
		shadow = p.shadowStage.Alpha(.72)
	}
	element.Shadow(
		runtimeTokens.Shadow.Overlay.OffsetX.Value,
		runtimeTokens.Shadow.Overlay.OffsetY.Value,
		runtimeTokens.Shadow.Overlay.Blur.Value,
		runtimeTokens.Shadow.Overlay.Spread.Value,
		shadow,
	)
	return element
}

func styleGlassEdge(element ui.Element, p palette) ui.Element {
	return element.Border(runtimeTokens.Dimension.Border.Value, ui.Color{}).
		BorderLinearGradient(ui.LinearGradient{
			Angle: 155,
			Stops: []ui.GradientStop{
				{Color: p.text.Alpha(.20), At: 0},
				{Color: p.text.Alpha(.06), At: .22},
				{Color: ui.Transparent, At: .48},
				{Color: p.text.Alpha(.04), At: .72},
				{Color: p.text.Alpha(.12), At: 1},
			},
		})
}
