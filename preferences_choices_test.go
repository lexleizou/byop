package main

import (
	"testing"
	"time"
)

func TestP9cPreferenceNormalization(t *testing.T) {
	if got := normalizeRunningSendMode("garbage"); got != runningSendQueue {
		t.Fatalf("running mode = %q", got)
	}
	if got := normalizeContrastMode("garbage"); got != contrastSystem {
		t.Fatalf("contrast = %q", got)
	}
	if got := normalizeMotionMode("garbage"); got != motionSystem {
		t.Fatalf("motion = %q", got)
	}
	if got := normalizePanelAnimationMS(99); got != defaultPanelAnimationMS {
		t.Fatalf("panel animation = %d", got)
	}
	for _, value := range []int{0, 150, 250, 400} {
		if got := normalizePanelAnimationMS(value); got != value {
			t.Fatalf("panel animation %d = %d", value, got)
		}
	}
}

func TestP9cRunningSendChoice(t *testing.T) {
	if got := oppositeRunningSendMode(runningSendQueue); got != runningSendSteer {
		t.Fatalf("opposite queue = %q", got)
	}
	if got := oppositeRunningSendMode(runningSendSteer); got != runningSendQueue {
		t.Fatalf("opposite steer = %q", got)
	}
	if got := runningSendBehavior(runningSendQueue); got != "followUp" {
		t.Fatalf("queue behavior = %q", got)
	}
	if got := runningSendBehavior(runningSendSteer); got != "steer" {
		t.Fatalf("steer behavior = %q", got)
	}
}

func TestP9cAppearanceChoices(t *testing.T) {
	state := &appState{preferences: defaultByopPreferences()}

	state.preferences.Contrast = string(contrastSystem)
	if !state.highContrastActive(true) || state.highContrastActive(false) {
		t.Fatal("system contrast did not follow the system value")
	}
	state.preferences.Contrast = string(contrastNormal)
	if state.highContrastActive(true) {
		t.Fatal("standard contrast should override system high contrast")
	}
	state.preferences.Contrast = string(contrastHigh)
	if !state.highContrastActive(false) {
		t.Fatal("high contrast should override system standard contrast")
	}

	state.preferences.Motion = string(motionSystem)
	state.preferences.PanelAnimationMS = 250
	if got := state.panelAnimationDuration(false); got != 250*time.Millisecond {
		t.Fatalf("standard duration = %s", got)
	}
	if got := state.panelAnimationDuration(true); got != 0 {
		t.Fatalf("system reduced duration = %s", got)
	}
	state.preferences.Motion = string(motionReduced)
	if got := state.panelAnimationDuration(false); got != 0 {
		t.Fatalf("reduced duration = %s", got)
	}
}
