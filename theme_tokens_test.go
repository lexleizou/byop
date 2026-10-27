package main

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestP12aCanonicalPaletteLoadsLightAndDark(t *testing.T) {
	light := themePalette(false)
	dark := themePalette(true)
	if light.background == dark.background {
		t.Fatal("light and dark palettes resolved to the same background")
	}
	if light.text == dark.text {
		t.Fatal("light and dark palettes resolved to the same text color")
	}
	if light.border == dark.border {
		t.Fatal("light and dark palettes resolved to the same border color")
	}
}

func TestP12aOnAccentContrast(t *testing.T) {
	for name, colors := range map[string]colorSetToken{
		"light": runtimeTokens.Color.Light,
		"dark":  runtimeTokens.Color.Dark,
	} {
		t.Run(name, func(t *testing.T) {
			if got := colors["onAccent"]; got != "#21201c" {
				t.Fatalf("onAccent = %q, want #21201c", got)
			}
			got := contrastRatio(tokenColor(colors, "accent"), tokenColor(colors, "onAccent"))
			if math.Abs(got-5.49) > .01 {
				t.Fatalf("onAccent contrast = %.2f, want 5.49:1", got)
			}
		})
	}
}

func TestP12aSemanticTextAndLightFocusTokens(t *testing.T) {
	tests := []struct {
		colors map[string]string
		want   map[string]string
	}{
		{
			colors: runtimeTokens.Color.Light,
			want: map[string]string{
				"dangerText": "#ce2c31", "successText": "#1d3b31", "warningText": "#4f3422",
			},
		},
		{
			colors: runtimeTokens.Color.Dark,
			want: map[string]string{
				"dangerText": "#ff9592", "successText": "#1fd8a4", "warningText": "#ffca16",
			},
		},
	}
	for _, tc := range tests {
		for name, want := range tc.want {
			if got := tc.colors[name]; got != want {
				t.Errorf("%s = %q, want %q", name, got, want)
			}
		}
	}
	for _, background := range []string{"background", "surface"} {
		got := contrastRatio(
			tokenColor(runtimeTokens.Color.Light, "focus"),
			tokenColor(runtimeTokens.Color.Light, background),
		)
		if got < 3 {
			t.Errorf("light focus on %s contrast = %.2f, want at least 3:1", background, got)
		}
	}
}

func TestP12aLightSemanticTextContrast(t *testing.T) {
	for _, semantic := range []string{"successText", "warningText"} {
		for _, background := range []string{"background", "surface"} {
			got := contrastRatio(
				tokenColor(runtimeTokens.Color.Light, semantic),
				tokenColor(runtimeTokens.Color.Light, background),
			)
			if got < 4.5 {
				t.Errorf("light %s on %s contrast = %.2f, want at least 4.5:1", semantic, background, got)
			}
		}
	}
}

func TestP12aRuntimeTokensMatchFoundation(t *testing.T) {
	if err := validateProductTokens(runtimeTokens); err != nil {
		t.Fatal(err)
	}
	if runtimeTokens.Motion.Easing != "ease-out" {
		t.Fatalf("motion.easing = %q, want ease-out", runtimeTokens.Motion.Easing)
	}
}

func TestP14MaterialRolesKeepDarkHierarchy(t *testing.T) {
	dark := themePalette(true)
	if dark.readingSurface != ui.Hex("#111110") {
		t.Fatalf("dark reading surface = %#v, want #111110", dark.readingSurface)
	}
	if dark.composerSurface != ui.Hex("#191918") {
		t.Fatalf("dark composer surface = %#v, want #191918", dark.composerSurface)
	}
	if dark.raisedSurface != ui.Hex("#222221") {
		t.Fatalf("dark raised surface = %#v, want #222221", dark.raisedSurface)
	}
	if dark.popoverTint.R != 0x22 || dark.popoverTint.G != 0x22 ||
		dark.popoverTint.B != 0x21 || dark.popoverTint.A != 0xb8 {
		t.Fatalf("dark popover tint = %#v, want #222221b8", dark.popoverTint)
	}
	if dark.popoverBorder != ui.Hex("#62605b") {
		t.Fatalf("dark popover border = %#v, want #62605b", dark.popoverBorder)
	}

	light := themePalette(false)
	if light.readingSurface != light.background ||
		light.raisedSurface != light.surface ||
		light.composerSurface != light.surface {
		t.Fatal("light material roles changed the existing surface appearance")
	}
}

func TestP14MainStagePreservesLightBaseline(t *testing.T) {
	for _, high := range []bool{false, true} {
		p := themePaletteWithContrast(false, high)
		if got := mainStageBackground(p, false); got != p.surface {
			t.Fatalf("high=%t light stage = %#v, want original surface %#v", high, got, p.surface)
		}
	}
	dark := themePalette(true)
	if got := mainStageBackground(dark, true); got != dark.readingSurface {
		t.Fatalf("dark stage = %#v, want reading surface %#v", got, dark.readingSurface)
	}
}

func TestP14PluginDetailsPreserveLightBaseline(t *testing.T) {
	for _, high := range []bool{false, true} {
		p := themePaletteWithContrast(false, high)
		img := ui.Render(func(c *ui.Context) {
			c.Root().Background(ui.RGB(255, 0, 255))
			panel := ui.Box(c).Absolute().Top(10).Left(10).Size(80, 80)
			stylePluginDetailsLight(panel, p)
		}, 100, 100, 1)
		got := img.RGBAAt(50, 50)
		if got.R != p.surface.R || got.G != p.surface.G || got.B != p.surface.B {
			t.Fatalf("high=%t light details center = %#v, want surface %#v", high, got, p.surface)
		}
		if pluginDetailsUsesGlass(false, high, false) ||
			pluginDetailsUsesGlass(false, high, true) {
			t.Fatalf("high=%t light details selected glass", high)
		}
	}
	if !pluginDetailsUsesGlass(true, false, false) {
		t.Fatal("normal dark details did not select glass")
	}
	for _, test := range []struct {
		high, reduce bool
	}{
		{high: true},
		{reduce: true},
	} {
		if pluginDetailsUsesGlass(true, test.high, test.reduce) {
			t.Fatalf("dark high=%t reduce=%t details did not use the opaque fallback",
				test.high, test.reduce)
		}
	}
}

func TestP12aRuntimeTokenValidationRejectsFoundationDrift(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*productTokens)
	}{
		{name: "version", mutate: func(tokens *productTokens) { tokens.Meta.Version = "" }},
		{name: "interactive glass", mutate: func(tokens *productTokens) { tokens.Meta.Glass.Interactive = true }},
		{name: "caption", mutate: func(tokens *productTokens) { tokens.Font.Caption.Value = 11 }},
		{name: "body", mutate: func(tokens *productTokens) { tokens.Font.Body.Value = 13 }},
		{name: "title", mutate: func(tokens *productTokens) { tokens.Font.Title.Value = 17 }},
		{name: "code", mutate: func(tokens *productTokens) { tokens.Font.Code.Value = 12 }},
		{name: "border", mutate: func(tokens *productTokens) { tokens.Dimension.Border.Value = 1 }},
		{name: "chip radius", mutate: func(tokens *productTokens) { tokens.Dimension.Radius["chip"] = dimensionToken{Value: 7, Unit: "px"} }},
		{name: "input radius", mutate: func(tokens *productTokens) { tokens.Dimension.Radius["input"] = dimensionToken{Value: 7, Unit: "px"} }},
		{name: "overlay radius", mutate: func(tokens *productTokens) { tokens.Dimension.Radius["overlay"] = dimensionToken{Value: 8, Unit: "px"} }},
		{name: "motion duration", mutate: func(tokens *productTokens) { tokens.Motion.Duration.Value = 150 }},
		{name: "motion easing", mutate: func(tokens *productTokens) { tokens.Motion.Easing = "linear" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tokens := cloneProductTokens(runtimeTokens)
			tc.mutate(&tokens)
			if err := validateProductTokens(tokens); err == nil {
				t.Fatal("invalid runtime tokens passed validation")
			}
		})
	}
}

func cloneProductTokens(tokens productTokens) productTokens {
	clone := tokens
	clone.Color.Light = cloneColorSet(tokens.Color.Light)
	clone.Color.Dark = cloneColorSet(tokens.Color.Dark)
	clone.Dimension.Radius = make(map[string]dimensionToken, len(tokens.Dimension.Radius))
	for name, value := range tokens.Dimension.Radius {
		clone.Dimension.Radius[name] = value
	}
	return clone
}

func cloneColorSet(colors colorSetToken) colorSetToken {
	clone := make(colorSetToken, len(colors))
	for name, value := range colors {
		clone[name] = value
	}
	return clone
}

func contrastRatio(a, b ui.Color) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + .05) / (lb + .05)
}

func relativeLuminance(c ui.Color) float64 {
	channel := func(value uint8) float64 {
		v := float64(value) / 255
		if v <= .04045 {
			return v / 12.92
		}
		return math.Pow((v+.055)/1.055, 2.4)
	}
	return .2126*channel(c.R) + .7152*channel(c.G) + .0722*channel(c.B)
}
