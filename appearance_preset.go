package main

import (
	"strings"

	"github.com/egoist/mygo"
)

type appearancePreset string

const (
	appearancePresetBYOP   appearancePreset = "byop"
	appearancePresetJAgent appearancePreset = "jagent"
	// appearancePresetBoardUI is the P31 preset: BoardUI's public blue and
	// neutral tokens, Inter with Noto Sans SC, opaque surfaces and a right-side
	// user bubble. See docs/P31-design.md.
	appearancePresetBoardUI appearancePreset = "boardui"
)

type visualMetrics struct {
	bodyFont              float32
	codeFont              float32
	captionFont           float32
	uiFont                float32
	titleFont             float32
	navFont               float32
	navItemHeight         float32
	sessionRowHeight      float32
	navGap                float32
	sessionGap            float32
	navRadius             float32
	navPaddingX           float32
	inputRadius           float32
	navDefaultWidth       float32
	navMinWidth           float32
	navMaxWidth           float32
	shellSpacing          float32
	mainRadius            float32
	chatContentWidth      float32
	chatHorizontalPadding float32
	chatBottomPadding     float32
	// chatContentMaxWidth is the ceiling of the content column when the
	// column follows the window (P32 §5). chatContentWidth keeps its old
	// meaning as the fixed column width on presets that do not follow.
	chatContentMaxWidth float32
	// chatContentFollowsWindow selects the adaptive column: the column is
	// min(chatContentMaxWidth, main width - 2×chatHorizontalPadding). BYOP
	// stays false because Onevra's FloatingConversationWith hard-codes its
	// 720 column and an external caller cannot widen it.
	chatContentFollowsWindow bool
	// turnGap is the vertical gap between turns inside the content column
	// of the self-drawn floating conversation.
	turnGap           float32
	composerRadius    float32
	composerMinHeight float32
	composerPadding   float32
	// userBubbleRadius is the BoardUI right-side user bubble's radius; the
	// other presets draw no bubble and leave it zero.
	userBubbleRadius float32
	transitionMS     int
}

func normalizeAppearancePreset(value string) appearancePreset {
	switch appearancePreset(strings.ToLower(strings.TrimSpace(value))) {
	case appearancePresetJAgent:
		return appearancePresetJAgent
	case appearancePresetBoardUI:
		return appearancePresetBoardUI
	default:
		return appearancePresetBYOP
	}
}

func (s *appState) currentAppearancePreset() appearancePreset {
	if s == nil {
		return appearancePresetBYOP
	}
	return normalizeAppearancePreset(s.preferences.AppearancePreset)
}

func (s *appState) jAgentPresetActive() bool {
	return s.currentAppearancePreset() == appearancePresetJAgent
}

func (s *appState) boardUIPresetActive() bool {
	return s.currentAppearancePreset() == appearancePresetBoardUI
}

func (s *appState) effectiveDark() bool {
	if s.jAgentPresetActive() {
		return true
	}
	return currentAppearanceDark()
}

func (s *appState) effectiveThemeSource() mygo.ThemeSource {
	if s.jAgentPresetActive() {
		return mygo.ThemeDark
	}
	return themeSource(s.themeMode)
}

func metricsForAppearancePreset(preset appearancePreset) visualMetrics {
	preset = normalizeAppearancePreset(string(preset))
	if preset == appearancePresetBoardUI {
		// P31 §3: BYOP's metrics with the four BoardUI values the BYOP render
		// path reads (outer spacing, main radius, composer radius, bubble).
		// P32 §5: the content column follows the window up to 1280 and the
		// turn gap tightens to 12 to match the BoardUI chat card rhythm.
		m := metricsForAppearancePreset(appearancePresetBYOP)
		m.shellSpacing = 12
		m.mainRadius = 24
		m.composerRadius = 24
		m.userBubbleRadius = 16
		m.chatContentWidth = 1280
		m.chatContentMaxWidth = 1280
		m.chatContentFollowsWindow = true
		m.chatHorizontalPadding = 24
		m.turnGap = 12
		return m
	}
	if preset == appearancePresetJAgent {
		return visualMetrics{
			bodyFont:                 15,
			codeFont:                 13,
			captionFont:              12,
			uiFont:                   13,
			titleFont:                18,
			navFont:                  14,
			navItemHeight:            28,
			sessionRowHeight:         28,
			navGap:                   2,
			sessionGap:               2,
			navRadius:                10,
			navPaddingX:              8,
			inputRadius:              10,
			navDefaultWidth:          261,
			navMinWidth:              220,
			navMaxWidth:              320,
			shellSpacing:             0,
			mainRadius:               0,
			chatContentWidth:         768,
			chatContentMaxWidth:      768,
			chatContentFollowsWindow: true,
			turnGap:                  24,
			chatHorizontalPadding:    48,
			chatBottomPadding:        8,
			composerRadius:           12,
			composerMinHeight:        92,
			composerPadding:          11,
			transitionMS:             125,
		}
	}
	return visualMetrics{
		bodyFont:                 fontBody,
		codeFont:                 runtimeTokens.Font.Code.Value,
		captionFont:              fontCaption,
		uiFont:                   fontBody,
		titleFont:                fontTitle,
		navFont:                  fontBody,
		navItemHeight:            32,
		sessionRowHeight:         sessionRowHeight,
		navGap:                   2,
		sessionGap:               sessionRowGap,
		navRadius:                8,
		navPaddingX:              8,
		inputRadius:              runtimeTokens.Dimension.Radius["input"].Value,
		navDefaultWidth:          navExpandedWidth,
		navMinWidth:              navMinExpandedWidth,
		navMaxWidth:              navMaxExpandedWidth,
		shellSpacing:             shellGap,
		mainRadius:               runtimeTokens.Dimension.Radius["shell"].Value,
		chatContentWidth:         720,
		chatContentMaxWidth:      720,
		chatContentFollowsWindow: false,
		turnGap:                  24,
		chatHorizontalPadding:    20,
		chatBottomPadding:        8,
		composerRadius:           runtimeTokens.Dimension.Radius["input"].Value,
		composerMinHeight:        96,
		composerPadding:          12,
		transitionMS:             defaultPanelAnimationMS,
	}
}

func (s *appState) visualMetrics() visualMetrics {
	return metricsForAppearancePreset(s.currentAppearancePreset())
}

func (s *appState) shellSpacing() float32 {
	return s.visualMetrics().shellSpacing
}

func (p palette) controlFont() float32 {
	if p.preset == appearancePresetJAgent {
		return p.uiFont
	}
	return fontSmall
}

func (s *appState) trafficLightY() int {
	if s.jAgentPresetActive() {
		return (48 - 14) / 2
	}
	// P32 §3.3: read the active preset's shell spacing so BoardUI's 12px
	// shell gap aligns the buttons with its title bar (the P31 known limit
	// of a 4px offset). MyGo 0.5.5 only accepts TrafficLightPosition when
	// the window is created, so this runs once at startup; switching
	// presets while running keeps the old position until the next launch.
	return int(s.shellSpacing()) + (int(shellTitleBarHeight)-14)/2
}

// chatContentColumnWidth is the width of the conversation content column for
// a palette at a given main-area width (P32 §5.1): presets that follow the
// window take min(chatContentMaxWidth, mainWidth - 2×padding) floored at 0;
// the BYOP preset keeps its fixed chatContentWidth because the Onevra
// component ignores anything wider.
func chatContentColumnWidth(p palette, mainWidth float32) float32 {
	if !p.chatContentFollowsWindow {
		return p.chatContentWidth
	}
	return max(0, min(p.chatContentMaxWidth, mainWidth-2*p.chatHorizontalPadding))
}
