package main

import (
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/oneo-me/onevra/source/components/chat"
	lu "github.com/oneo-me/onevra/source/icons/lucide"
)

const thinkingCommitDelay = 400 * time.Millisecond

type piModelChoice struct {
	model piModel
}

func piModelChoiceID(provider, id string) string {
	if provider == "" && id == "" {
		return ""
	}
	return provider + "/" + id
}

func buildPiModelChoices(models []piModel) ([]chat.Option, map[string]piModelChoice) {
	options := make([]chat.Option, 0, len(models))
	choices := make(map[string]piModelChoice, len(models))
	labels := make([]string, len(models))
	labelCounts := make(map[string]int, len(models))
	for i, model := range models {
		label := fallback(model.Name, model.ID)
		labels[i] = label
		labelCounts[label]++
	}
	for i, model := range models {
		id := piModelChoiceID(model.Provider, model.ID)
		description := ""
		// Duplicate labels are common across providers and aliases. The model
		// ID disambiguates those rows without repeating it for unique names.
		if labelCounts[labels[i]] > 1 {
			description = model.ID
		}
		options = append(options, chat.Option{
			ID:          id,
			Label:       labels[i],
			Group:       model.Provider,
			Detail:      formatContextWindow(model.ContextWindow),
			Description: description,
			Icon:        lu.Sparkles,
		})
		choices[id] = piModelChoice{model: model}
	}
	return options, choices
}

func piModelDisplayName(name, model string) string {
	return fallback(firstNonEmpty(strings.TrimSpace(name), strings.TrimSpace(model)), "Pi default")
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
		return "Off"
	case "minimal":
		return "Minimal"
	case "low":
		return "Low"
	case "medium":
		return "Medium"
	case "high":
		return "High"
	case "xhigh":
		return "XHigh"
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
