package main

import (
	"strings"
	"time"
)

type runningSendMode string

const (
	runningSendQueue runningSendMode = "queue"
	runningSendSteer runningSendMode = "steer"
)

type contrastMode string

const (
	contrastSystem contrastMode = "system"
	contrastNormal contrastMode = "standard"
	contrastHigh   contrastMode = "high"
)

type motionMode string

const (
	motionSystem   motionMode = "system"
	motionStandard motionMode = "standard"
	motionReduced  motionMode = "reduced"

	defaultPanelAnimationMS = 250
)

func normalizeRunningSendMode(value string) runningSendMode {
	switch runningSendMode(strings.ToLower(strings.TrimSpace(value))) {
	case runningSendSteer:
		return runningSendSteer
	default:
		return runningSendQueue
	}
}

func (s *appState) currentRunningSendMode() runningSendMode {
	return normalizeRunningSendMode(s.preferences.RunningSendMode)
}

func oppositeRunningSendMode(mode runningSendMode) runningSendMode {
	if normalizeRunningSendMode(string(mode)) == runningSendSteer {
		return runningSendQueue
	}
	return runningSendSteer
}

func runningSendBehavior(mode runningSendMode) string {
	if normalizeRunningSendMode(string(mode)) == runningSendSteer {
		return "steer"
	}
	return "followUp"
}

func normalizeContrastMode(value string) contrastMode {
	switch contrastMode(strings.ToLower(strings.TrimSpace(value))) {
	case contrastNormal:
		return contrastNormal
	case contrastHigh:
		return contrastHigh
	default:
		return contrastSystem
	}
}

func normalizeMotionMode(value string) motionMode {
	switch motionMode(strings.ToLower(strings.TrimSpace(value))) {
	case motionStandard:
		return motionStandard
	case motionReduced:
		return motionReduced
	default:
		return motionSystem
	}
}

func normalizePanelAnimationMS(value int) int {
	return defaultPanelAnimationMS
}

func (s *appState) highContrastActive(systemHighContrast bool) bool {
	switch normalizeContrastMode(s.preferences.Contrast) {
	case contrastHigh:
		return true
	case contrastNormal:
		return false
	default:
		return systemHighContrast
	}
}

func (s *appState) reducedMotionActive(systemReduceMotion bool) bool {
	switch normalizeMotionMode(s.preferences.Motion) {
	case motionReduced:
		return true
	case motionStandard:
		return false
	default:
		return systemReduceMotion
	}
}

func (s *appState) panelAnimationDuration(systemReduceMotion bool) time.Duration {
	if s.reducedMotionActive(systemReduceMotion) {
		return 0
	}
	return time.Duration(normalizePanelAnimationMS(s.preferences.PanelAnimationMS)) * time.Millisecond
}
