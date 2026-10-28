package main

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/egoist/mygo/ui"
)

type hudState struct {
	cwd                   string
	branch                string
	stats                 *piSessionStats
	autoCompact           bool
	contextWindowFallback int
	usingSubscription     bool
	extStatus             map[string]string
	generation            uint64
	branchGeneration      uint64
	avgOutputTokens       int64
	avgGenerationSeconds  float64
	statsRequest          uint64
	statsRequestedAt      time.Time
	statsLoading          bool
	currentTPS            float64
	currentTPSKnown       bool
	currentTPSUpdatedAt   time.Time
	streamStartedAt       time.Time
	streamTokens          float64
}

func (s *appState) beginPiHUD(cwd string, state *piSessionState) {
	s.hud.generation++
	s.hud.branchGeneration++
	s.hud.cwd = strings.TrimSpace(cwd)
	s.hud.branch = ""
	s.hud.stats = nil
	s.hud.statsLoading = false
	s.hud.extStatus = make(map[string]string)
	s.hud.usingSubscription = false
	s.hud.avgOutputTokens = 0
	s.hud.avgGenerationSeconds = 0
	s.resetPiHUDSpeed()
	s.applyPiHUDState(state)
	s.refreshPiHUDBranch()
	s.refreshPiHUD()
}

func (s *appState) resetPiHUDData() {
	s.hud.generation++
	s.hud.stats = nil
	s.hud.statsRequestedAt = time.Time{}
	s.hud.statsLoading = false
	s.hud.extStatus = make(map[string]string)
	s.hud.avgOutputTokens = 0
	s.hud.avgGenerationSeconds = 0
	s.resetPiHUDSpeed()
}

func (s *appState) clearPiHUD() {
	s.hud.generation++
	s.hud.branchGeneration++
	s.hud.cwd = ""
	s.hud.branch = ""
	s.hud.stats = nil
	s.hud.statsRequestedAt = time.Time{}
	s.hud.statsLoading = false
	s.hud.autoCompact = false
	s.hud.contextWindowFallback = 0
	s.hud.usingSubscription = false
	s.hud.extStatus = nil
	s.hud.avgOutputTokens = 0
	s.hud.avgGenerationSeconds = 0
	s.resetPiHUDSpeed()
}

func (s *appState) applyPiHUDState(state *piSessionState) {
	if state == nil {
		return
	}
	s.hud.autoCompact = state.AutoCompactionEnabled
	s.hud.contextWindowFallback = state.Model.ContextWindow
}

func (s *appState) refreshPiHUDBranch() {
	cwd := strings.TrimSpace(s.hud.cwd)
	if cwd == "" {
		return
	}
	generation := s.hud.generation
	s.hud.branchGeneration++
	branchGeneration := s.hud.branchGeneration
	go func() {
		branch := piGitBranch(cwd)
		s.postUI(func() {
			if generation != s.hud.generation ||
				branchGeneration != s.hud.branchGeneration ||
				cwd != s.hud.cwd {
				return
			}
			s.hud.branch = branch
		})
	}()
}

func piGitBranch(cwd string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", cwd, "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	branch := strings.TrimSpace(string(output))
	if branch == "" || branch == "HEAD" {
		hashCtx, hashCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer hashCancel()
		hashCmd := exec.CommandContext(hashCtx, "git", "-C", cwd, "rev-parse", "--short", "HEAD")
		hash, hashErr := hashCmd.Output()
		if hashErr != nil {
			return ""
		}
		return strings.TrimSpace(string(hash))
	}
	return branch
}

func (s *appState) refreshPiHUD() {
	if s.pi == nil || s.piConn != piConnected {
		return
	}
	client := s.pi
	epoch := s.piEpoch
	generation := s.hud.generation
	s.hud.statsRequest++
	request := s.hud.statsRequest
	s.hud.statsRequestedAt = time.Now()
	s.hud.statsLoading = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		stats, err := client.GetSessionStats(ctx)
		s.postUI(func() { s.applyPiHUDStats(generation, epoch, request, client, stats, err) })
	}()
}

func (s *appState) applyPiHUDStats(generation, epoch, request uint64, client *piClient,
	stats *piSessionStats, err error,
) {
	if generation != s.hud.generation ||
		epoch != s.piEpoch ||
		request != s.hud.statsRequest ||
		client != s.pi {
		return
	}
	s.hud.statsLoading = false
	if err != nil || stats == nil {
		return
	}
	s.hud.stats = stats
}

func (s *appState) recordPiHUDSample(outputTokens int64, seconds float64) {
	if outputTokens <= 0 || seconds <= 0 {
		return
	}
	s.hud.avgOutputTokens += outputTokens
	s.hud.avgGenerationSeconds += seconds
	s.hud.currentTPS = float64(outputTokens) / seconds
	s.hud.currentTPSKnown = true
	s.hud.currentTPSUpdatedAt = time.Now()
	s.hud.streamStartedAt = time.Time{}
	s.hud.streamTokens = 0
}

func (s *appState) piHUDAverageTokensPerSecond() (float64, bool) {
	if s.hud.avgOutputTokens <= 0 || s.hud.avgGenerationSeconds <= 0 {
		return 0, false
	}
	return float64(s.hud.avgOutputTokens) / s.hud.avgGenerationSeconds, true
}

func (s *appState) beginPiHUDSpeed() {
	s.hud.currentTPS = 0
	s.hud.currentTPSKnown = false
	s.hud.currentTPSUpdatedAt = time.Time{}
	s.hud.streamStartedAt = time.Time{}
	s.hud.streamTokens = 0
}

func (s *appState) resetPiHUDSpeed() {
	s.hud.currentTPS = 0
	s.hud.currentTPSKnown = false
	s.hud.currentTPSUpdatedAt = time.Time{}
	s.hud.streamStartedAt = time.Time{}
	s.hud.streamTokens = 0
}

func (s *appState) observePiHUDSpeed(delta string) {
	s.observePiHUDSpeedAt(delta, time.Now())
}

func piHUDEstimatedTokens(value string) float64 {
	var wide, narrow int
	for _, r := range value {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
			wide++
		} else {
			narrow++
		}
	}
	return max(1, float64(wide)+float64(narrow)/4)
}

func (s *appState) observePiHUDSpeedAt(delta string, now time.Time) {
	delta = strings.TrimSpace(delta)
	if delta == "" {
		return
	}
	if s.hud.streamStartedAt.IsZero() {
		s.hud.streamStartedAt = now
	}
	s.hud.streamTokens += piHUDEstimatedTokens(delta)
	elapsed := now.Sub(s.hud.streamStartedAt)
	if elapsed < 250*time.Millisecond || s.hud.streamTokens <= 0 {
		return
	}
	s.hud.currentTPS = s.hud.streamTokens / elapsed.Seconds()
	s.hud.currentTPSKnown = true
	s.hud.currentTPSUpdatedAt = now
	s.hud.streamStartedAt = now
	s.hud.streamTokens = 0
}

func (s *appState) refreshPiHUDIfStale(now time.Time) {
	if s.hud.statsLoading {
		return
	}
	if !s.hud.statsRequestedAt.IsZero() && now.Sub(s.hud.statsRequestedAt) < 2*time.Second {
		return
	}
	s.refreshPiHUD()
}

func formatPiTokens(count int64) string {
	if count < 0 {
		count = 0
	}
	switch {
	case count < 1000:
		return strconv.FormatInt(count, 10)
	case count < 10000:
		return fmt.Sprintf("%.1fk", float64(count)/1000)
	case count < 1000000:
		return fmt.Sprintf("%dk", int64((float64(count)/1000)+0.5))
	case count < 10000000:
		return fmt.Sprintf("%.1fM", float64(count)/1000000)
	default:
		return fmt.Sprintf("%dM", int64((float64(count)/1000000)+0.5))
	}
}

func piCacheHitRate(stats *piSessionStats) (float64, bool) {
	if stats == nil {
		return 0, false
	}
	cacheRead := max(int64(0), stats.Tokens.CacheRead)
	total := max(int64(0), stats.Tokens.Input) + cacheRead + max(int64(0), stats.Tokens.CacheWrite)
	if total <= 0 {
		return 0, false
	}
	return float64(cacheRead) / float64(total) * 100, true
}

func piContextPercent(stats *piSessionStats) (float64, bool) {
	if stats == nil || stats.ContextUsage == nil || stats.ContextUsage.Percent == nil {
		return 0, false
	}
	return *stats.ContextUsage.Percent, true
}

func piContextWindow(stats *piSessionStats, fallback int) int {
	if stats != nil && stats.ContextUsage != nil && stats.ContextUsage.ContextWindow > 0 {
		return stats.ContextUsage.ContextWindow
	}
	return max(0, fallback)
}

func hudContextSeverity(percent float64, hasPercent bool) int {
	if !hasPercent {
		return 0
	}
	switch {
	case percent > 90:
		return 2
	case percent > 70:
		return 1
	default:
		return 0
	}
}

func truncateHUDStatus(value string, limit int) string {
	value = singleLineText(value)
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit == 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}

func formatHUDContextTokens(count int64) string {
	if count < 0 {
		count = 0
	}
	switch {
	case count < 1000:
		return strconv.FormatInt(count, 10)
	case count < 1000000:
		return compactContextWindow(int(count), 1000) + "K"
	default:
		return compactContextWindow(int(count), 1000000) + "M"
	}
}

func piHUDContextSummary(stats *piSessionStats, fallbackWindow int, autoCompact bool) string {
	percent, hasPercent := piContextPercent(stats)
	window := piContextWindow(stats, fallbackWindow)
	var used *int64
	if stats != nil && stats.ContextUsage != nil {
		used = stats.ContextUsage.Tokens
	}

	body := ""
	switch {
	case used != nil && window > 0 && hasPercent:
		body = fmt.Sprintf("%.1f%% · %s / %s",
			percent, formatHUDContextTokens(*used), formatContextWindow(window))
	case used != nil && window > 0:
		body = formatHUDContextTokens(*used) + " / " + formatContextWindow(window)
	case hasPercent && window > 0:
		body = fmt.Sprintf("%.1f%% · %s", percent, formatContextWindow(window))
	case used != nil:
		body = formatHUDContextTokens(*used)
	case hasPercent:
		body = fmt.Sprintf("%.1f%%", percent)
	default:
		return ""
	}
	if autoCompact {
		body += text("hudAutoSuffix")
	}
	return textArgs("hudContextUsage", map[string]any{"usage": body})
}

func (s *appState) piHUDModelName() string {
	if name := strings.TrimSpace(s.piModelName); name != "" {
		return name
	}
	for _, model := range s.piModels {
		if model.Provider == s.piModelProvider && model.ID == s.piModelID {
			return fallback(strings.TrimSpace(model.Name), s.piModelID)
		}
	}
	if model := strings.TrimSpace(s.piModel); model != "" {
		return model
	}
	return ""
}

const (
	piTPSExcellentThreshold = 80.0
	piTPSFastThreshold      = 40.0
	piTPSModerateThreshold  = 20.0
	piTPSLowThreshold       = 5.0
	piTPSStaleAfter         = 30 * time.Second
)

type piTPSTier uint8

const (
	piTPSExcellent piTPSTier = iota
	piTPSFast
	piTPSModerate
	piTPSLow
	piTPSPoor
)

func piTPSTierFor(rate float64) piTPSTier {
	switch {
	case rate >= piTPSExcellentThreshold:
		return piTPSExcellent
	case rate >= piTPSFastThreshold:
		return piTPSFast
	case rate >= piTPSModerateThreshold:
		return piTPSModerate
	case rate >= piTPSLowThreshold:
		return piTPSLow
	default:
		return piTPSPoor
	}
}

func piTPSStyle(t *ui.Theme, rate float64) (foreground, background, border ui.Color) {
	switch piTPSTierFor(rate) {
	case piTPSExcellent:
		foreground = t.Success
	case piTPSFast:
		foreground = t.Success.Mix(t.TextMuted, .30)
	case piTPSModerate:
		foreground = t.Warning
	case piTPSLow:
		foreground = t.TextMuted
	default:
		foreground = t.Danger
	}
	return foreground, foreground.Alpha(.10), foreground.Alpha(.30)
}

func (s *appState) piHUDCurrentTPS() (float64, bool) {
	if !s.hud.currentTPSKnown {
		return 0, false
	}
	if !s.hud.currentTPSUpdatedAt.IsZero() &&
		time.Since(s.hud.currentTPSUpdatedAt) > piTPSStaleAfter {
		return 0, false
	}
	return s.hud.currentTPS, true
}

type hudItemKind uint8

const (
	hudItemMeta hudItemKind = iota
	hudItemModel
	hudItemContext
	hudItemTPS
)

type hudItem struct {
	label, value string
	flexible     bool
	kind         hudItemKind
}

func (s *appState) piHUDItems() []hudItem {
	if s.piConn != piConnected || s.piConnectionErrorActive() {
		return nil
	}
	items := make([]hudItem, 0, 9)
	if model := s.piHUDModelName(); model != "" {
		items = append(items, hudItem{value: model, flexible: true, kind: hudItemModel})
	}
	if context := piHUDContextSummary(s.hud.stats, s.hud.contextWindowFallback, s.hud.autoCompact); context != "" {
		items = append(items, hudItem{value: context, kind: hudItemContext})
	}
	if rate, ok := s.piHUDCurrentTPS(); ok {
		items = append(items, hudItem{
			value: textArgs("hudTokensPerSecond", map[string]any{
				"speed": fmt.Sprintf("%.1f", rate),
			}),
			kind: hudItemTPS,
		})
	}
	if branch := strings.TrimSpace(s.hud.branch); branch != "" {
		items = append(items, hudItem{value: branch, flexible: true})
	}
	if stats := s.hud.stats; stats != nil {
		tokens := make([]string, 0, 4)
		if value := stats.Tokens.Input; value > 0 {
			tokens = append(tokens, "↑"+formatPiTokens(value))
		}
		if value := stats.Tokens.Output; value > 0 {
			tokens = append(tokens, "↓"+formatPiTokens(value))
		}
		if len(tokens) > 0 {
			items = append(items, hudItem{value: strings.Join(tokens, " ")})
		}
		if stats.Cost > 0 || s.hud.usingSubscription {
			cost := fmt.Sprintf("$%.3f", stats.Cost)
			if s.hud.usingSubscription {
				cost += text("hudSubscriptionSuffix")
			}
			items = append(items, hudItem{value: cost, flexible: true})
		}
		if rate, ok := piCacheHitRate(stats); ok {
			value := fmt.Sprintf("%d%%", int64(math.Round(rate)))
			label := strings.TrimSpace(strings.TrimSuffix(
				textArgs("hudCache", map[string]any{"percent": int64(math.Round(rate))}),
				value,
			))
			items = append(items, hudItem{label: label, value: value, flexible: true})
		}
	}
	if len(s.hud.extStatus) > 0 {
		keys := make([]string, 0, len(s.hud.extStatus))
		for key, value := range s.hud.extStatus {
			if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		statuses := make([]string, 0, len(keys))
		for _, key := range keys {
			statuses = append(statuses, truncateHUDStatus(s.hud.extStatus[key], 40))
		}
		if len(statuses) > 0 {
			for _, status := range statuses {
				items = append(items, hudItem{value: status, flexible: true})
			}
		}
	}
	return items
}

func (s *appState) piHUDLabel() string {
	items := s.piHUDItems()
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if item.label == "" {
			parts = append(parts, item.value)
			continue
		}
		parts = append(parts, item.label+" "+item.value)
	}
	return strings.Join(parts, "  ")
}

func (s *appState) piHUDContextLabel() string {
	return piHUDContextSummary(s.hud.stats, s.hud.contextWindowFallback, s.hud.autoCompact)
}

func (s *appState) piHUD(c *ui.Context) {
	label := s.piHUDLabel()
	if label == "" {
		return
	}
	t := c.Theme()
	ui.Row(c).Key("pi-hud").Label(label).Grow(1).MinWidth(0).Clip().
		Justify(ui.Center).AlignItems(ui.Center).Gap(12).
		Tooltip(s.piHUDTooltip()).Children(func() {
		for _, item := range s.piHUDItems() {
			row := ui.Row(c)
			if item.flexible && item.kind != hudItemTPS {
				row.Shrink(1).MinWidth(0)
			} else {
				row.Shrink(0)
			}
			row.AlignItems(ui.Center).Gap(4).Children(func() {
				color := t.Text.Mix(t.TextMuted, .45)
				var background, border ui.Color
				switch item.kind {
				case hudItemModel:
					color = t.Text
				case hudItemContext:
					if percent, ok := piContextPercent(s.hud.stats); ok {
						switch hudContextSeverity(percent, ok) {
						case 2:
							color = t.Danger
						case 1:
							color = t.Warning
						}
					}
				case hudItemTPS:
					rate, _ := s.piHUDCurrentTPS()
					color, background, border = piTPSStyle(t, rate)
					row.Height(18).Padding(0, 7).Radius(9).
						Background(background).Border(v12BorderWidth, border)
				}
				if item.label != "" {
					label := ui.Text(c, item.label).FontSize(fontCaption).LineHeight(1.2).
						FontWeight(400).TextColor(t.TextMuted).SingleLine()
					if item.flexible {
						label.MinWidth(0).Ellipsis("…")
					}
				}
				value := ui.Text(c, item.value).FontSize(fontCaption).LineHeight(1.2).
					FontWeight(500).FontFeatures("tnum").TextColor(color).SingleLine()
				if item.flexible {
					value.Grow(1).MinWidth(0).Ellipsis("…")
				}
			})
		}
	})
}

func (s *appState) piHUDTooltip() string {
	label := s.piHUDLabel()
	if rate, ok := piCacheHitRate(s.hud.stats); ok {
		label += "\n" + textArgs("hudCacheHitCumulative", map[string]any{
			"percent": fmt.Sprintf("%.1f", rate),
		})
	}
	if s.hud.stats != nil && (s.hud.stats.Tokens.CacheRead > 0 || s.hud.stats.Tokens.CacheWrite > 0) {
		label += "\n" + textArgs("hudCacheTokens", map[string]any{
			"read":  formatPiTokens(s.hud.stats.Tokens.CacheRead),
			"write": formatPiTokens(s.hud.stats.Tokens.CacheWrite),
		})
	}
	if speed, ok := s.piHUDAverageTokensPerSecond(); ok {
		label += "\n" + textArgs("hudAverageSpeedTooltip", map[string]any{
			"speed": fmt.Sprintf("%.1f", speed),
		})
	}
	if rate, ok := s.piHUDCurrentTPS(); ok {
		label += "\n" + textArgs("hudCurrentSpeedTooltip", map[string]any{
			"speed": fmt.Sprintf("%.1f", rate),
		})
	}
	return label
}
