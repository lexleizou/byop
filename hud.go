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
}

func (s *appState) beginPiHUD(cwd string, state *piSessionState) {
	s.hud.generation++
	s.hud.branchGeneration++
	s.hud.cwd = strings.TrimSpace(cwd)
	s.hud.branch = ""
	s.hud.stats = nil
	s.hud.extStatus = make(map[string]string)
	s.hud.usingSubscription = false
	s.hud.avgOutputTokens = 0
	s.hud.avgGenerationSeconds = 0
	s.applyPiHUDState(state)
	s.refreshPiHUDBranch()
	s.refreshPiHUD()
}

func (s *appState) resetPiHUDData() {
	s.hud.generation++
	s.hud.stats = nil
	s.hud.extStatus = make(map[string]string)
	s.hud.avgOutputTokens = 0
	s.hud.avgGenerationSeconds = 0
}

func (s *appState) clearPiHUD() {
	s.hud.generation++
	s.hud.branchGeneration++
	s.hud.cwd = ""
	s.hud.branch = ""
	s.hud.stats = nil
	s.hud.autoCompact = false
	s.hud.contextWindowFallback = 0
	s.hud.usingSubscription = false
	s.hud.extStatus = nil
	s.hud.avgOutputTokens = 0
	s.hud.avgGenerationSeconds = 0
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
}

func (s *appState) piHUDAverageTokensPerSecond() (float64, bool) {
	if s.hud.avgOutputTokens <= 0 || s.hud.avgGenerationSeconds <= 0 {
		return 0, false
	}
	return float64(s.hud.avgOutputTokens) / s.hud.avgGenerationSeconds, true
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

func (s *appState) piHUDLabel() string {
	if s.piConn != piConnected || s.piConnectionErrorActive() {
		return textArgs("hudPiStatus", map[string]any{"status": s.connectionStateLabel()})
	}
	parts := make([]string, 0, 8)
	if branch := strings.TrimSpace(s.hud.branch); branch != "" {
		parts = append(parts, branch)
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
			parts = append(parts, strings.Join(tokens, " "))
		}
		if stats.Cost > 0 || s.hud.usingSubscription {
			cost := fmt.Sprintf("$%.3f", stats.Cost)
			if s.hud.usingSubscription {
				cost += text("hudSubscriptionSuffix")
			}
			parts = append(parts, cost)
		}
		if rate, ok := piCacheHitRate(stats); ok {
			parts = append(parts, textArgs("hudCache", map[string]any{"percent": int64(math.Round(rate))}))
		}
	}
	if rate, ok := s.piHUDAverageTokensPerSecond(); ok {
		parts = append(parts, textArgs("hudAverageSpeed", map[string]any{"speed": int64(math.Round(rate))}))
	}
	if context := s.piHUDContextLabel(); context != "" {
		parts = append(parts, context)
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
			parts = append(parts, strings.Join(statuses, "  "))
		}
	}
	return strings.Join(parts, "  ")
}

func (s *appState) piHUDContextLabel() string {
	percent, hasPercent := piContextPercent(s.hud.stats)
	if !hasPercent {
		return ""
	}
	context := textArgs("hudContext", map[string]any{"percent": fmt.Sprintf("%.1f", percent)})
	if s.hud.autoCompact {
		context += text("hudAutoSuffix")
	}
	return context
}

func (s *appState) piHUD(c *ui.Context) {
	label := s.piHUDLabel()
	if label == "" {
		return
	}
	t := c.Theme()
	color := t.TextMuted
	if s.piConnectionErrorActive() || s.piConn == piConnectionErr {
		color = t.Danger
	}
	if percent, ok := piContextPercent(s.hud.stats); ok {
		switch hudContextSeverity(percent, ok) {
		case 2:
			color = t.Danger
		case 1:
			color = t.Warning
		}
	}
	ui.Text(c, label).Key("pi-hud").FontSize(fontCaption).LineHeight(1.2).TextColor(color).
		Grow(1).MinWidth(0).SingleLine().Ellipsis("…").Tooltip(s.piHUDTooltip())
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
	return label
}
