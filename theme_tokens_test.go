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
