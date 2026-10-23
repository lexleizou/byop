package main

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/components/chat"
	lu "github.com/oneo-me/onevra/source/icons/lucide"
)

const thinkingCommitDelay = 400 * time.Millisecond

type modelTuple struct {
	VariantID string
	Thinking  string
	Fast      bool
}

type piModelFamily struct {
	Provider string
	Key      string
	Label    string
	Variants []piModel
}

type piModelChoice struct {
	family   piModelFamily
	selected piModel
}

type piModelChoiceState struct {
	ActiveProvider string
	ActiveID       string
	Remembered     map[string]modelTuple
}

func piModelChoiceID(provider, id string) string {
	if provider == "" && id == "" {
		return ""
	}
	return provider + "/" + id
}

func piModelFamilyKey(model piModel) string {
	return fallback(strings.TrimSpace(model.Name), strings.TrimSpace(model.ID))
}

func piModelFamilies(models []piModel) []piModelFamily {
	families := make([]piModelFamily, 0, len(models))
	groups := make([]piModelFamily, 0, len(models))
	index := make(map[string]int, len(models))
	for _, model := range models {
		key := piModelFamilyKey(model)
		groupKey := model.Provider + "\x00" + key
		if i, ok := index[groupKey]; ok {
			groups[i].Variants = append(groups[i].Variants, model)
			continue
		}
		index[groupKey] = len(groups)
		groups = append(groups, piModelFamily{
			Provider: model.Provider,
			Key:      key,
			Label:    fallback(key, model.ID),
			Variants: []piModel{model},
		})
	}
	for _, group := range groups {
		if piContextFamilyMergeable(group.Variants) {
			families = append(families, group)
			continue
		}
		used := make(map[string]struct{}, len(group.Variants))
		for _, variant := range group.Variants {
			key := group.Key
			if len(group.Variants) > 1 {
				key = piModelAliasFamilyKey(group.Key, variant.ID)
			}
			for suffix := 2; ; suffix++ {
				if _, exists := used[key]; !exists {
					break
				}
				key = piModelAliasFamilyKey(group.Key, variant.ID+"#"+strconv.Itoa(suffix))
			}
			used[key] = struct{}{}
			families = append(families, piModelFamily{
				Provider: variant.Provider,
				Key:      key,
				Label:    fallback(group.Label, variant.ID),
				Variants: []piModel{variant},
			})
		}
	}
	return families
}

func piContextFamilyMergeable(models []piModel) bool {
	if len(models) < 2 {
		return false
	}
	seen := make(map[int]struct{}, len(models))
	for _, model := range models {
		if model.ContextWindow <= 0 {
			return false
		}
		if _, exists := seen[model.ContextWindow]; exists {
			return false
		}
		seen[model.ContextWindow] = struct{}{}
	}
	return true
}

func piModelAliasFamilyKey(name, id string) string {
	return fallback(strings.TrimSpace(name), strings.TrimSpace(id)) + "/" + strings.TrimSpace(id)
}

func selectPiModelVariant(family piModelFamily, remembered modelTuple, hasRemembered bool,
	activeProvider, activeID string,
) piModel {
	if len(family.Variants) == 0 {
		return piModel{}
	}
	if hasRemembered {
		for _, variant := range family.Variants {
			if variant.ID == remembered.VariantID {
				return variant
			}
		}
	}
	if activeProvider == family.Provider {
		for _, variant := range family.Variants {
			if variant.ID == activeID {
				return variant
			}
		}
	}
	return family.Variants[0]
}

// buildPiModelChoices collapses same-provider models with the same display
// name into families. The optional state chooses the variant represented by
// each row while keeping every variant ID available to the custom picker's
// search.
func buildPiModelChoices(models []piModel, states ...piModelChoiceState) ([]chat.Option, map[string]piModelChoice) {
	return buildPiModelChoicesForFamilies(piModelFamilies(models), states...)
}

func buildPiModelChoicesForFamilies(families []piModelFamily,
	states ...piModelChoiceState,
) ([]chat.Option, map[string]piModelChoice) {
	options := make([]chat.Option, 0, len(families))
	choices := make(map[string]piModelChoice, len(families))
	var state piModelChoiceState
	if len(states) > 0 {
		state = states[0]
	}
	for _, family := range families {
		rowID := piModelChoiceID(family.Provider, family.Key)
		remembered, hasRemembered := state.Remembered[rowID]
		selected := selectPiModelVariant(family, remembered, hasRemembered, state.ActiveProvider, state.ActiveID)
		brand := modelBrand(family.Provider, selected.ID, family.Label)
		options = append(options, chat.Option{
			ID:          rowID,
			Label:       family.Label,
			Group:       family.Provider,
			Description: selected.ID,
			Leading:     modelBrandLeading(brand),
		})
		choices[rowID] = piModelChoice{family: family, selected: selected}
	}
	return options, choices
}

func piModelChoiceMatches(choice piModelChoice, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	values := []string{choice.family.Label, choice.family.Provider, choice.family.Key}
	for _, variant := range choice.family.Variants {
		values = append(values, variant.ID, variant.Name)
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func findPiModelFamily(models []piModel, provider, id, name string) (piModelFamily, bool) {
	return findPiModelFamilyInFamilies(piModelFamilies(models), provider, id, name)
}

func findPiModelFamilyInFamilies(families []piModelFamily, provider, id, name string) (piModelFamily, bool) {
	for _, family := range families {
		if family.Provider != provider {
			continue
		}
		for _, variant := range family.Variants {
			if variant.ID == id {
				return family, true
			}
		}
	}
	key := strings.TrimSpace(name)
	for _, family := range families {
		if family.Provider == provider && family.Key == key {
			return family, true
		}
	}
	return piModelFamily{}, false
}

func piContextVariants(family piModelFamily) []piModel {
	variants := make([]piModel, 0, len(family.Variants))
	for _, variant := range family.Variants {
		if variant.ContextWindow > 0 {
			variants = append(variants, variant)
		}
	}
	if len(variants) < 2 {
		return nil
	}
	sort.SliceStable(variants, func(i, j int) bool {
		if variants[i].ContextWindow == variants[j].ContextWindow {
			return variants[i].ID < variants[j].ID
		}
		return variants[i].ContextWindow < variants[j].ContextWindow
	})
	return variants
}

func buildPiContextOptions(variants []piModel) []chat.Option {
	options := make([]chat.Option, 0, len(variants))
	for _, variant := range variants {
		options = append(options, chat.Option{
			ID:          variant.ID,
			Label:       formatContextWindow(variant.ContextWindow),
			Description: variant.ID,
		})
	}
	return options
}

func piModelDisplayName(name, model string) string {
	return fallback(firstNonEmpty(strings.TrimSpace(name), strings.TrimSpace(model)), text("settingsPiDefault"))
}

func formatContextWindow(tokens int) string {
	if tokens <= 0 {
		return ""
	}
	switch {
	case tokens >= 1_000_000:
		return compactContextWindow(tokens, 1_000_000) + "M"
	case tokens >= 1_000:
		return compactContextWindow(tokens, 1_000) + "K"
	default:
		return strconv.Itoa(tokens)
	}
}

func compactContextWindow(value, unit int) string {
	scaled := float64(value) / float64(unit)
	if math.Abs(scaled-math.Round(scaled)) < 0.000001 {
		return strconv.FormatInt(int64(math.Round(scaled)), 10)
	}
	text := strconv.FormatFloat(scaled, 'f', 1, 64)
	return strings.TrimSuffix(text, ".0")
}

func modelBrand(provider, id, name string) string {
	value := strings.ToLower(strings.TrimSpace(id) + " " + strings.TrimSpace(name))
	for _, brand := range []string{"deepseek", "claude", "qwen"} {
		if strings.Contains(value, brand) {
			return brand
		}
	}
	return ""
}

func modelBrandLeading(brand string) func(c *ui.Context, size float32) {
	return func(c *ui.Context, size float32) {
		symbol := lu.Sparkles
		if brand != "" {
			symbol = func() *ui.SVG { return icon("brand-" + brand) }
		}
		components.Icon(c, symbol).Size(size, size).Shrink(0).TextColor(c.Theme().Text)
	}
}

func piFastModeSupported() bool {
	// Reserved capability hook. Fast stays hidden in P11 because the Pi 1.4.0
	// RPC surface has no fast-mode command.
	return false
}

func buildPiThinkingOptions(levels []string) []chat.Option {
	seen := make(map[string]struct{}, len(levels))
	byID := make(map[string]string, len(levels))
	for _, raw := range levels {
		level := strings.TrimSpace(raw)
		if level == "" {
			continue
		}
		if _, exists := seen[level]; exists {
			continue
		}
		seen[level] = struct{}{}
		byID[level] = level
	}

	options := make([]chat.Option, 0, len(byID))
	for _, level := range []string{"off", "minimal", "low", "medium", "high", "xhigh"} {
		if _, ok := byID[level]; !ok {
			continue
		}
		options = append(options, chat.Option{ID: level, Label: thinkingLevelLabel(level)})
		delete(byID, level)
	}
	for _, raw := range levels {
		level := strings.TrimSpace(raw)
		if _, ok := byID[level]; !ok {
			continue
		}
		options = append(options, chat.Option{ID: level, Label: thinkingLevelLabel(level)})
		delete(byID, level)
	}
	return options
}

func thinkingLevelLabel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "off":
		return text("thinkingOff")
	case "minimal":
		return text("thinkingMinimal")
	case "low":
		return text("thinkingLow")
	case "medium":
		return text("thinkingMedium")
	case "high":
		return text("thinkingHigh")
	case "xhigh":
		return text("thinkingXHigh")
	}
	words := strings.FieldsFunc(strings.TrimSpace(level), func(r rune) bool {
		return r == '_' || r == '-' || unicode.IsSpace(r)
	})
	for i, word := range words {
		runes := []rune(strings.ToLower(word))
		if len(runes) > 0 {
			runes[0] = unicode.ToUpper(runes[0])
		}
		words[i] = string(runes)
	}
	if len(words) == 0 {
		return strings.TrimSpace(level)
	}
	return strings.Join(words, " ")
}

func reasoningLevelsVisible(levels []string) bool {
	nonempty := make([]string, 0, len(levels))
	for _, level := range levels {
		if level = strings.TrimSpace(level); level != "" {
			nonempty = append(nonempty, level)
		}
	}
	return len(nonempty) > 0 && !(len(nonempty) == 1 && strings.EqualFold(nonempty[0], "off"))
}

func shouldCommitThinking(pending, confirmed string, allowed bool) bool {
	return allowed && strings.TrimSpace(pending) != "" && pending != confirmed
}

func thinkingLevelAvailable(levels []string, level string) bool {
	level = strings.TrimSpace(level)
	if level == "" {
		return false
	}
	for _, candidate := range levels {
		if strings.TrimSpace(candidate) == level {
			return true
		}
	}
	return false
}

func piThinkingRestore(current string, remembered modelTuple, hasRemembered bool,
	levels []string,
) (restore string, defaulted bool) {
	if !hasRemembered || strings.TrimSpace(remembered.Thinking) == "" {
		return "", false
	}
	if !thinkingLevelAvailable(levels, remembered.Thinking) {
		return "", true
	}
	if remembered.Thinking == current {
		return "", false
	}
	return remembered.Thinking, false
}
