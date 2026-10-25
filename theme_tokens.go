package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

//go:embed design-system/pi-workbench/tokens.json
var canonicalTokensJSON []byte

type dimensionToken struct {
	Value float32
	Unit  string
}

type colorSetToken map[string]string

type shadowToken struct {
	Color   string
	OffsetX dimensionToken
	OffsetY dimensionToken
	Blur    dimensionToken
	Spread  dimensionToken
}

type productTokens struct {
	Meta struct {
		Version string `json:"version"`
		Glass   struct {
			Interactive bool `json:"interactive"`
		} `json:"glass"`
	}
	Color struct {
		Light    colorSetToken `json:"light"`
		Dark     colorSetToken `json:"dark"`
		Contrast struct {
			Light map[string]string `json:"light"`
			Dark  map[string]string `json:"dark"`
		} `json:"contrast"`
	} `json:"color"`
	Font struct {
		Caption dimensionToken `json:"caption"`
		Body    dimensionToken `json:"body"`
		Title   dimensionToken `json:"title"`
		Code    dimensionToken `json:"code"`
	} `json:"font"`
	Dimension struct {
		Border dimensionToken            `json:"border"`
		Radius map[string]dimensionToken `json:"radius"`
	} `json:"dimension"`
	Motion struct {
		Duration        dimensionToken `json:"duration"`
		ReducedDuration dimensionToken `json:"reducedDuration"`
		Easing          string         `json:"easing"`
	} `json:"motion"`
	Glass struct {
		NavTransparencyDefault int
		NavTintMaxAlpha        float64
		BlurDefault            int
		BlurMax                int
		RefractionDefault      bool
		WallpaperDefault       string
	}
	Shadow struct {
		Overlay shadowToken `json:"overlay"`
		Stage   shadowToken `json:"stage"`
	} `json:"shadow"`
}

var runtimeTokens = mustLoadProductTokens()

func mustLoadProductTokens() productTokens {
	var root map[string]any
	if err := json.Unmarshal(canonicalTokensJSON, &root); err != nil {
		panic("parse canonical design tokens: " + err.Error())
	}
	raw := flattenTokenValues(root)
	var tokens productTokens
	tokens.Meta.Version = nestedString(root, "meta", "version")
	tokens.Meta.Glass.Interactive = nestedBool(root, "meta", "glass", "interactive")
	tokens.Color.Light = loadColorSet(raw, "color", "light")
	tokens.Color.Dark = loadColorSet(raw, "color", "dark")
	tokens.Color.Contrast.Light = loadPlainColorSet(raw, "color", "contrast", "light")
	tokens.Color.Contrast.Dark = loadPlainColorSet(raw, "color", "contrast", "dark")
	tokens.Font.Caption = loadDimension(raw, "font", "caption")
	tokens.Font.Body = loadDimension(raw, "font", "body")
	tokens.Font.Title = loadDimension(raw, "font", "title")
	tokens.Font.Code = loadDimension(raw, "font", "code")
	tokens.Dimension.Border = loadDimension(raw, "dimension", "border")
	tokens.Dimension.Radius = map[string]dimensionToken{}
	for _, name := range []string{"chip", "input", "overlay", "shell"} {
		tokens.Dimension.Radius[name] = loadDimension(raw, "dimension", "radius", name)
	}
	tokens.Motion.Duration = loadDimension(raw, "motion", "duration")
	tokens.Motion.ReducedDuration = loadDimension(raw, "motion", "reducedDuration")
	tokens.Motion.Easing = nestedString(raw, "motion", "easing")
	tokens.Glass.NavTransparencyDefault = int(math.Round(nestedNumber(raw, "glass", "navTransparencyDefault")))
	tokens.Glass.NavTintMaxAlpha = nestedNumber(raw, "glass", "navTintMaxAlpha")
	tokens.Glass.BlurDefault = int(math.Round(nestedNumber(raw, "glass", "blurDefault")))
	tokens.Glass.BlurMax = int(math.Round(nestedNumber(raw, "glass", "blurMax")))
	tokens.Glass.RefractionDefault = nestedBool(raw, "glass", "refractionDefault")
	tokens.Glass.WallpaperDefault = nestedString(raw, "glass", "wallpaperDefault")
	if err := validateProductTokens(tokens); err != nil {
		panic(err.Error())
	}
	tokens.Shadow.Overlay = loadShadow(raw, "shadow", "overlay")
	tokens.Shadow.Stage = loadShadow(raw, "shadow", "stage")
	return tokens
}

func validateProductTokens(tokens productTokens) error {
	for name, expected := range map[string]dimensionToken{
		"caption": {Value: 12, Unit: "px"},
		"body":    {Value: 14, Unit: "px"},
		"title":   {Value: 18, Unit: "px"},
		"code":    {Value: 14, Unit: "px"},
	} {
		actual := map[string]dimensionToken{
			"caption": tokens.Font.Caption,
			"body":    tokens.Font.Body,
			"title":   tokens.Font.Title,
			"code":    tokens.Font.Code,
		}[name]
		if actual != expected {
			return fmt.Errorf("font.%s = %g%s, want %g%s", name, actual.Value, actual.Unit, expected.Value, expected.Unit)
		}
	}
	if tokens.Dimension.Border != (dimensionToken{Value: .5, Unit: "px"}) {
		return fmt.Errorf("dimension.border = %g%s, want 0.5px", tokens.Dimension.Border.Value, tokens.Dimension.Border.Unit)
	}
	for name, expected := range map[string]float32{"chip": 6, "input": 8, "overlay": 12} {
		actual, ok := tokens.Dimension.Radius[name]
		if !ok || actual != (dimensionToken{Value: expected, Unit: "px"}) {
			return fmt.Errorf("dimension.radius.%s = %g%s, want %gpx", name, actual.Value, actual.Unit, expected)
		}
	}
	if tokens.Motion.Duration != (dimensionToken{Value: 250, Unit: "ms"}) {
		return fmt.Errorf("motion.duration = %g%s, want 250ms", tokens.Motion.Duration.Value, tokens.Motion.Duration.Unit)
	}
	if tokens.Motion.Easing != "ease-out" {
		return fmt.Errorf("motion.easing = %q, want %q", tokens.Motion.Easing, "ease-out")
	}
	if tokens.Meta.Glass.Interactive {
		return fmt.Errorf("canonical glass must stay non-interactive")
	}
	if strings.TrimSpace(tokens.Meta.Version) == "" {
		return fmt.Errorf("meta.version is empty")
	}
	return nil
}

func nestedValue(raw map[string]any, path ...string) any {
	var current any = raw
	for _, key := range path {
		node, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = node[key]
	}
	return current
}

func nestedString(raw map[string]any, path ...string) string {
	switch value := nestedValue(raw, path...).(type) {
	case string:
		return value
	case map[string]any:
		wrapped, _ := value["value"].(string)
		return wrapped
	default:
		return ""
	}
}

func nestedBool(raw map[string]any, path ...string) bool {
	switch value := nestedValue(raw, path...).(type) {
	case bool:
		return value
	case map[string]any:
		wrapped, _ := value["value"].(bool)
		return wrapped
	default:
		return false
	}
}

func nestedNumber(raw map[string]any, path ...string) float64 {
	switch value := nestedValue(raw, path...).(type) {
	case float64:
		return value
	case map[string]any:
		wrapped, _ := value["value"].(float64)
		return wrapped
	default:
		return 0
	}
}

func loadDimension(raw map[string]any, path ...string) dimensionToken {
	node, ok := nestedValue(raw, path...).(map[string]any)
	if !ok {
		panic("missing dimension token " + strings.Join(path, "."))
	}
	value, ok := node["value"].(map[string]any)
	if !ok {
		panic("invalid dimension token " + strings.Join(path, "."))
	}
	number, ok := value["value"].(float64)
	if !ok {
		panic("invalid dimension number " + strings.Join(path, "."))
	}
	unit, _ := value["unit"].(string)
	return dimensionToken{Value: float32(number), Unit: unit}
}

func loadColorSet(raw map[string]any, path ...string) colorSetToken {
	colors := loadPlainColorSet(raw, path...)
	result := make(colorSetToken, len(colors))
	for name, value := range colors {
		result[name] = value
	}
	return result
}

func loadPlainColorSet(raw map[string]any, path ...string) map[string]string {
	node, ok := nestedValue(raw, path...).(map[string]any)
	if !ok {
		panic("missing color token " + strings.Join(path, "."))
	}
	result := make(map[string]string, len(node))
	for name, value := range node {
		color, _ := value.(string)
		if wrapped, ok := value.(map[string]any); ok {
			color, _ = wrapped["value"].(string)
		}
		if color != "" {
			result[name] = color
		}
	}
	return result
}

func loadShadow(raw map[string]any, path ...string) shadowToken {
	node, ok := nestedValue(raw, path...).(map[string]any)
	if !ok {
		panic("missing shadow token " + strings.Join(path, "."))
	}
	value, ok := node["value"].(map[string]any)
	if !ok {
		panic("invalid shadow token " + strings.Join(path, "."))
	}
	offsetX, _ := value["offsetX"].(map[string]any)
	offsetY, _ := value["offsetY"].(map[string]any)
	blur, _ := value["blur"].(map[string]any)
	spread, _ := value["spread"].(map[string]any)
	color, _ := value["color"].(string)
	dimension := func(node map[string]any) dimensionToken {
		number, _ := node["value"].(float64)
		unit, _ := node["unit"].(string)
		return dimensionToken{Value: float32(number), Unit: unit}
	}
	return shadowToken{
		Color: color, OffsetX: dimension(offsetX), OffsetY: dimension(offsetY),
		Blur: dimension(blur), Spread: dimension(spread),
	}
}

// flattenTokenValues strips DTCG metadata so the runtime consumes token values
// rather than maintaining a second handwritten palette.
func flattenTokenValues(value any) map[string]any {
	node, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	if tokenValue, exists := node["$value"]; exists {
		if nested, ok := tokenValue.(map[string]any); ok {
			return map[string]any{"value": nested}
		}
		return map[string]any{"value": tokenValue}
	}
	result := make(map[string]any, len(node))
	for key, child := range node {
		if strings.HasPrefix(key, "$") {
			continue
		}
		if nested := flattenTokenValues(child); nested != nil {
			result[key] = nested
		}
	}
	return result
}

func tokenColor(tokens colorSetToken, name string) ui.Color {
	value := tokens[name]
	if value == "" {
		panic("missing color token " + name)
	}
	return ui.Hex(value)
}

func productGlass() glass.Glass {
	if runtimeTokens.Meta.Glass.Interactive {
		panic("canonical glass must stay non-interactive")
	}
	return glass.Glass{}
}

func productGlassWithTint(tint ui.Color) glass.Glass {
	g := productGlass()
	g.Tint = tint
	return g
}

func tokenGlassTint(dark, high, reduceTransparency bool) ui.Color {
	if high || reduceTransparency {
		if dark {
			return tokenColor(runtimeTokens.Color.Dark, "surface")
		}
		return tokenColor(runtimeTokens.Color.Light, "surface")
	}
	if dark {
		return tokenColor(runtimeTokens.Color.Dark, "glassTint")
	}
	return tokenColor(runtimeTokens.Color.Light, "glassTint")
}

func applyChromeMaterial(element ui.Element, dark, high, reduceTransparency bool) ui.Element {
	element.Radius(runtimeTokens.Dimension.Radius["shell"].Value).
		Border(runtimeTokens.Dimension.Border.Value, themePaletteWithContrast(dark, high).border)
	if high || reduceTransparency {
		return element.Background(tokenGlassTint(dark, high, reduceTransparency))
	}
	return element.Material(productGlassWithTint(tokenGlassTint(dark, high, false)))
}

func applyOpaqueSurface(element ui.Element, dark, high bool) ui.Element {
	p := themePaletteWithContrast(dark, high)
	return element.Background(p.surface).Border(runtimeTokens.Dimension.Border.Value, p.border)
}

func tokenDimensionCSS(token dimensionToken) string {
	return fmt.Sprintf("%g%s", token.Value, token.Unit)
}
