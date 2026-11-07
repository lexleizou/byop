package main

import (
	"math"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

func deepLiquidGlassActive(dark, high, reduceTransparency bool) bool {
	return dark && !high && !reduceTransparency
}

func navGlassTint(base ui.Color, transparency int) ui.Color {
	transparency = min(max(transparency, 0), 100)
	alpha := float64(100-transparency) / 100 * runtimeTokens.Glass.NavTintMaxAlpha
	return ui.RGBA(base.R, base.G, base.B, float32(math.Round(alpha*255))/255)
}

func navGlassCard(
	c *ui.Context,
	p palette,
	dark, high, reduceTransparency bool,
	radius float32,
	transparency, blur int,
	refraction bool,
	content func(),
) ui.Element {
	if p.preset == appearancePresetBoardUI {
		// P31 §5: the BoardUI sidebar card is opaque and skips glassChrome;
		// the sidebar card shadows come from BoardUI's public token rows.
		card := ui.Column(c).FillHeight().FillWidth().MinWidth(0).Radius(p.mainRadius).
			Border(0.5, p.border).Background(p.surface)
		if p.dark {
			card.BorderColor(ui.Hex("#27272a"))
		} else {
			card.BorderColor(ui.Hex("#ffffff"))
		}
		sh1 := ui.RGBA(0, 0, 0, .02)
		sh2 := ui.RGBA(0, 0, 0, .06)
		sh3 := ui.RGBA(0, 0, 0, .32)
		if p.dark {
			sh1, sh2, sh3 = ui.RGBA(0, 0, 0, .10), ui.RGBA(0, 0, 0, .16), ui.RGBA(0, 0, 0, .42)
		}
		card.Shadow(0, 1, 0, 0, sh1).Shadow(0, 1, 12, 0, sh2).Shadow(0, 0, 1, 0, sh3)
		card.Children(func() {
			ui.Column(c).FillHeight().FillWidth().MinHeight(0).
				Radius(p.mainRadius - 0.5).Clip().Children(content)
		})
		return card
	}
	card := ui.Column(c).FillHeight().FillWidth().MinWidth(0).Radius(radius)
	if glassAllowed(p) && deepLiquidGlassActive(dark, high, reduceTransparency) {
		if blur > 0 {
			card.Material(glass.Blur{Radius: float32(blur)})
		}
		card.Children(func() {
			inner := ui.Column(c).Absolute().Top(0).Right(0).Bottom(0).Left(0).
				Radius(radius)
			if refraction {
				material := productGlass()
				material.Style = glass.Clear
				material.Tint = navGlassTint(p.surface, transparency)
				inner.Material(material)
			} else {
				inner.Background(navGlassTint(p.surface, transparency))
			}
			inner = styleGlassEdge(inner, p)
			inner.Clip().PassThrough()

			ui.Column(c).Absolute().Top(0).Right(0).Bottom(0).Left(0).MinHeight(0).
				Radius(max(0, radius-runtimeTokens.Dimension.Border.Value)).
				Clip().Children(content)
		})
		return card
	}

	glassChrome(card, p, dark, high, reduceTransparency)
	card.Children(func() {
		ui.Column(c).FillHeight().FillWidth().MinHeight(0).
			Radius(max(0, radius-runtimeTokens.Dimension.Border.Value)).
			Clip().Children(content)
	})
	return card
}
