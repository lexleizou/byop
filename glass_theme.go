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
	card := ui.Column(c).FillHeight().FillWidth().MinWidth(0).Radius(radius)
	if deepLiquidGlassActive(dark, high, reduceTransparency) {
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
