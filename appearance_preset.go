package main

import (
	"strings"

	"github.com/egoist/mygo"
)

type appearancePreset string

const (
	appearancePresetBYOP   appearancePreset = "byop"
	appearancePresetJAgent appearancePreset = "jagent"
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
	composerRadius        float32
	composerMinHeight     float32
	composerPadding       float32
	transitionMS          int
}

func normalizeAppearancePreset(value string) appearancePreset {
	switch appearancePreset(strings.ToLower(strings.TrimSpace(value))) {
	case appearancePresetJAgent:
		return appearancePresetJAgent
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
	if normalizeAppearancePreset(string(preset)) == appearancePresetJAgent {
		return visualMetrics{
			bodyFont:              15,
			codeFont:              13,
			captionFont:           12,
			uiFont:                13,
			titleFont:             18,
			navFont:               14,
			navItemHeight:         28,
			sessionRowHeight:      28,
			navGap:                2,
			sessionGap:            2,
			navRadius:             10,
			navPaddingX:           8,
			inputRadius:           10,
			navDefaultWidth:       261,
			navMinWidth:           220,
			navMaxWidth:           320,
			shellSpacing:          0,
			mainRadius:            0,
			chatContentWidth:      768,
			chatHorizontalPadding: 48,
			chatBottomPadding:     8,
			composerRadius:        12,
			composerMinHeight:     92,
			composerPadding:       11,
			transitionMS:          125,
		}
	}
	return visualMetrics{
		bodyFont:              fontBody,
		codeFont:              runtimeTokens.Font.Code.Value,
		captionFont:           fontCaption,
		uiFont:                fontBody,
		titleFont:             fontTitle,
		navFont:               fontBody,
		navItemHeight:         32,
		sessionRowHeight:      sessionRowHeight,
		navGap:                2,
		sessionGap:            sessionRowGap,
		navRadius:             8,
		navPaddingX:           8,
		inputRadius:           runtimeTokens.Dimension.Radius["input"].Value,
		navDefaultWidth:       navExpandedWidth,
		navMinWidth:           navMinExpandedWidth,
		navMaxWidth:           navMaxExpandedWidth,
		shellSpacing:          shellGap,
		mainRadius:            runtimeTokens.Dimension.Radius["shell"].Value,
		chatContentWidth:      720,
		chatHorizontalPadding: 20,
		chatBottomPadding:     8,
		composerRadius:        runtimeTokens.Dimension.Radius["input"].Value,
		composerMinHeight:     96,
		composerPadding:       12,
		transitionMS:          defaultPanelAnimationMS,
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
	return int(shellGap) + (int(shellTitleBarHeight)-14)/2
}
