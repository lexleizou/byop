package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// P29 motion: the layered durations measured from the commercial 0.21.45
// conversation process. The user authorised this stage to follow the
// commercial values, so inside the process they overrule the old global
// 12/14/18 and 250 ms conventions. With the app's reduced-motion choice on,
// every duration here collapses to zero and every state wait resolves on
// the spot, as the authorisation requires.
const (
	// p29MotionDetail is the activity detail expansion; the tool rows
	// inside it enter over the same duration, the first ten staggered.
	p29MotionDetail     = 150 * time.Millisecond
	p29MotionRowStagger = 30 * time.Millisecond
	p29MotionStaggerMax = 10
	// p29MotionArrow is the disclosure chevron's rotation.
	p29MotionArrow = 150 * time.Millisecond
	// p29MotionNodeEnter is a new process node: height and opacity along
	// the commercial cubic-bezier(.22, 1, .36, 1).
	p29MotionNodeEnter = 320 * time.Millisecond
	// p29MotionTurnCollapse is the manual whole-turn fold: height and
	// opacity, ease-in-out.
	p29MotionTurnCollapse = 420 * time.Millisecond
	// p29MotionStatusCrossfade is the 420 ms four-pixel up/down crossfade
	// between status labels.
	p29MotionStatusCrossfade = 420 * time.Millisecond
	// p29StatusMinInterval is the shortest gap between status label
	// changes; the newest value always wins.
	p29StatusMinInterval = 900 * time.Millisecond
	// p29MotionRevealDelay and p29MotionAnswerReveal move the final
	// answer in after the process settles.
	p29MotionRevealDelay  = 150 * time.Millisecond
	p29MotionAnswerReveal = 500 * time.Millisecond
)

// p29EaseReveal is the commercial (0.22, 1, 0.36, 1) curve.
func p29EaseReveal() ui.Easing {
	return ui.CubicBezier(.22, 1, .36, 1)
}

// p29Animation builds one of the stage's curves, already reduced for the
// app's reduced-motion choice. A zero duration means "arrive now".
func p29Animation(d time.Duration, reduced bool, ease ui.Easing) ui.Animation {
	if reduced || d <= 0 {
		return ui.Animation{}
	}
	return ui.Animation{Duration: d, Ease: ease}
}

// p29StatusLabelTransition crossfades a status label swap: the old value
// rises and fades out, the new one comes from 4 DIPs below. Both halves
// take the stage's 420 ms.
func p29StatusLabelTransition(reduced bool) ui.ElementTransition {
	duration := p29MotionStatusCrossfade
	if reduced {
		duration = 0
	}
	if duration == 0 {
		// Reduced motion: only a fade in place, no travel.
		return ui.ElementTransition{}
	}
	return ui.ElementTransition{
		Duration: duration,
		Ease:     ui.EaseOut,
		Enter:    &ui.Motion{Y: 4},
		Exit:     &ui.Motion{Y: -4},
	}
}

// p29TurnCollapseAnimation drives the manual whole-turn fold through a
// 0..1 progress value (height and opacity), ease-in-out over 420 ms.
func p29TurnCollapseAnimation(reduced bool) ui.Animation {
	if reduced {
		return ui.Animation{}
	}
	return ui.Animation{Duration: p29MotionTurnCollapse, Ease: ui.EaseInOut}
}

// p29ArrowRotation returns the chevron angle for an open/closed control,
// animating through the element's own channel over 150 ms.
func p29ArrowRotation(el ui.Element, open, reduced bool) float32 {
	target := float32(0)
	if open {
		target = 90
	}
	if reduced {
		return target
	}
	return el.AnimateTo("p29-arrow", target,
		ui.Animation{Duration: p29MotionArrow, Ease: ui.EaseOut})
}

// p29StatusState throttles one activity's status label: the newest value
// wins, but a shown value stays at least 900 ms unless it is a failure or
// the turn is settling. Cross-check with the commercial constants: S1e=900
// and the error path's zero delay.
type p29StatusState struct {
	shown      string
	pending    string
	hasPending bool
	shownAt    time.Time
	anchored   bool
}

// advance returns the label to show this frame, whether it just changed,
// and how long the caller should wait before the pending value may win.
// immediate covers the honest fast paths: an error state, and a turn that
// stopped streaming and must show its final state without delay.
func (st *p29StatusState) advance(desired string, now time.Time, reduced, immediate bool) (
	label string, changed bool, wait time.Duration,
) {
	if desired == "" {
		if st.shown == "" {
			return "", false, 0
		}
		return st.shown, false, 0
	}
	if !st.anchored {
		st.shown, st.shownAt, st.anchored = desired, now, true
		return desired, true, 0
	}
	if desired == st.shown {
		st.pending, st.hasPending = "", false
		return st.shown, false, 0
	}
	if reduced || immediate {
		st.shown, st.shownAt = desired, now
		st.pending, st.hasPending = "", false
		return st.shown, true, 0
	}
	if elapsed := now.Sub(st.shownAt); elapsed >= p29StatusMinInterval {
		st.shown, st.shownAt = desired, now
		st.pending, st.hasPending = "", false
		return st.shown, true, 0
	} else {
		st.pending = desired
		st.hasPending = true
		return st.shown, false, p29StatusMinInterval - elapsed
	}
}

// p29RevealProgress drives one 0-to-1 reveal channel. A fresh MyGo channel
// starts at its first target (ui/motion.go: AnimateTo "starts at the first
// target"), so a reveal that must actually run jumps the channel to 0 on
// the element's first build and then lets the curve carry it to 1. The
// appeared set records which reveals are armed; closing a disclosure
// removes its entry so reopening runs the motion again. Reduced motion
// bypasses the channel and reports the settled end.
func p29RevealProgress(el ui.Element, appeared map[string]bool, key string,
	animation ui.Animation, reduced bool,
) float32 {
	if reduced {
		return 1
	}
	if !appeared[key] {
		appeared[key] = true
		el.Jump(key, 0)
	}
	return el.AnimateTo(key, 1, animation)
}

// p29Coord arms a shared-parent reveal: the wrapper that holds the process
// and the answer measures its natural height before and after the change
// (the frame it saw against the frame the layout makes now) and animates
// the difference. A manual fold takes 420 ms height+opacity ease-in-out;
// the coordination at settle time — the process folding while the answer
// appears — takes the 150 ms delay and 500 ms commercial curve, with the
// answer's own 4 DIP rise and fade layered on top through AnimatePose.
type p29Coord struct {
	at     time.Time
	manual bool
}

func (co p29Coord) animation() ui.Animation {
	if co.manual {
		return ui.Animation{Duration: p29MotionTurnCollapse, Ease: ui.EaseInOut}
	}
	animation := ui.Animation{Duration: p29MotionAnswerReveal, Ease: p29EaseReveal()}
	animation.Delay = p29MotionRevealDelay
	return animation
}

// expired reports the end of the coordination window, a few frames after
// the curve settles so late change events still animate inside it.
func (co p29Coord) expired(now time.Time) bool {
	animation := co.animation()
	return now.Sub(co.at) >= animation.Delay+animation.Duration+80*time.Millisecond
}
