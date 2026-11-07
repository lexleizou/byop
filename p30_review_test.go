package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func p30rSource(i int) string {
	return fmt.Sprintf("graph TD\n  A%d[Start %d] --> B%d[End %d]\n", i, i, i, i)
}

func p30rFiller(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "Paragraph %d of filler text that keeps the transcript long enough to scroll.\n\n", i)
	}
	return b.String()
}

func p30rHistory(t *testing.T, diagrams int, filler int, preset appearancePreset) *appState {
	t.Helper()
	restore := stableTestAppearance(t)
	t.Cleanup(restore)
	state := layoutTestState(t)
	state.preferences.AppearancePreset = string(preset)
	for i := 0; i < diagrams; i++ {
		fence := "```mermaid\n" + p30rSource(i) + "```"
		state.chat = append(state.chat,
			chatMessage{role: chatUser, status: "Complete", blocks: []chatBlock{p29TextBlock(fmt.Sprintf("Q%d", i), "")}},
			chatMessage{role: chatAssistant, status: "Complete", stopReason: "stop",
				blocks: []chatBlock{p29TextBlock(p30rFiller(filler)+fence, p29PhaseFinalAnswer)}},
		)
	}
	return state
}

func p30rAlive(state *appState) int {
	n := 0
	for _, d := range state.inlineDiagrams {
		if inlineDiagramViewAlive(d.WebView) {
			n++
		}
	}
	return n
}

// R1: a long history with more diagrams than the view cap: do views thrash?
func TestR30HistoryManyDiagramsThrash(t *testing.T) {
	for _, filler := range []int{0, 12} {
		calls := p30WireDiagrams(t)
		state := p30rHistory(t, 10, filler, appearancePresetBYOP)
		tt := ui.NewTester(state.view, 1180, 760)
		p30Settle(tt)
		c0, r0 := calls.creates, calls.releases
		for i := 0; i < 10; i++ {
			tt.Frame()
			tt.Advance(16 * time.Millisecond)
		}
		placed := 0
		for _, st := range state.chatView.diagramCards {
			if st.contentKnown {
				placed++
			}
		}
		alive := p30rAlive(state)
		t.Logf("filler=%d: after settle creates=%d releases=%d; after 10 more frames creates=%d releases=%d alive=%d cards placed=%d/%d",
			filler, c0, r0, calls.creates, calls.releases, alive, placed, len(state.chatView.diagramCards))
		if calls.creates-c0 > 0 || calls.releases-r0 > 0 {
			t.Errorf("filler=%d: idle frames kept rebuilding views: +%d creates, +%d releases",
				filler, calls.creates-c0, calls.releases-r0)
		}
		// A quiescent transcript at the bottom still shows the diagram the
		// reader is looking at: passing by building nothing is not a fix.
		if alive == 0 {
			t.Errorf("filler=%d: no view stayed alive for the card in view", filler)
		}
		if alive > maxInlineDiagramViews {
			t.Errorf("filler=%d: %d views alive, cap is %d", filler, alive, maxInlineDiagramViews)
		}
	}
}

// R2: scroll a built card far away: is its view released?
func TestR30FarCardReleased(t *testing.T) {
	calls := p30WireDiagrams(t)
	state := p30rHistory(t, 2, 60, appearancePresetBYOP)
	tt := ui.NewTester(state.view, 1180, 760)
	p30Settle(tt)
	conv := &state.chatView.conversation
	first := state.inlineDiagramFor(p30rSource(0))
	last := state.inlineDiagramFor(p30rSource(1))
	// At the bottom the reader is looking at the last diagram, so it holds a
	// view: a test that only proves the far card is released would pass with
	// nothing rendered at all.
	if !inlineDiagramViewAlive(last.WebView) {
		t.Fatalf("at bottom the card in view has no view (creates=%d alive=%d)",
			calls.creates, p30rAlive(state))
	}
	conv.Scroll.Y = 0
	// A tester frame only rebuilds when something invalidates it, so the jump
	// is applied by a layout change (one DIP) rather than by hoping for a
	// redraw the view would not do on its own.
	tt.SetSize(1180, 759)
	for i := 0; i < 3; i++ {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
	}
	if inlineDiagramViewAlive(last.WebView) {
		t.Errorf("a card %v DIP away kept its view (releases=%d)", conv.Scroll.MaxY, calls.releases)
	}
	if alive := p30rAlive(state); alive > maxInlineDiagramViews {
		t.Errorf("%d views alive after the jump, cap is %d", alive, maxInlineDiagramViews)
	}
	_ = first
}

// R3: two identical diagrams in the transcript.
func TestR30DuplicateDiagram(t *testing.T) {
	calls := p30WireDiagrams(t)
	restore := stableTestAppearance(t)
	defer restore()
	state := layoutTestState(t)
	fence := "```mermaid\n" + p30rSource(1) + "```"
	state.chat = []chatMessage{
		{role: chatUser, status: "Complete", blocks: []chatBlock{p29TextBlock("Q", "")}},
		{role: chatAssistant, status: "Complete", stopReason: "stop", blocks: []chatBlock{p29TextBlock(fence, p29PhaseFinalAnswer)}},
		{role: chatUser, status: "Complete", blocks: []chatBlock{p29TextBlock("again", "")}},
		{role: chatAssistant, status: "Complete", stopReason: "stop", blocks: []chatBlock{p29TextBlock(fence, p29PhaseFinalAnswer)}},
	}
	var perr any
	func() {
		defer func() { perr = recover() }()
		tt := ui.NewTester(state.view, 1180, 1400)
		p30Settle(tt)
	}()
	t.Logf("duplicate diagram: panic=%v creates=%d", perr, calls.creates)
	if perr != nil {
		t.Errorf("two identical diagrams panicked: %v", perr)
	}
}

// R5: TooLong then dropped then rebuilt?
func TestR30TooLongNeverBuilds(t *testing.T) {
	calls := p30WireDiagrams(t)
	big := "graph TD\n" + strings.Repeat("  A-->B\n", 3000)
	state := p30DiagramState(t, "```mermaid\n"+big+"```")
	tt := ui.NewTester(state.view, 1180, 1400)
	for i := 0; i < 5; i++ {
		p30Settle(tt)
	}
	t.Logf("too long: creates=%d visibleNotice=%v", calls.creates, tt.HasText(text("diagramTooLong")))
	if calls.creates != 0 {
		t.Errorf("too-long source built %d views", calls.creates)
	}
}

// R6: session switch clears views.
func TestR30SessionSwitchReleases(t *testing.T) {
	calls := p30WireDiagrams(t)
	state := p30rHistory(t, 3, 0, appearancePresetBYOP)
	tt := ui.NewTester(state.view, 1180, 1400)
	p30Settle(tt)
	aliveBefore := p30rAlive(state)
	state.chatView = chatPresentation{}
	state.chat = []chatMessage{{role: chatUser, status: "Complete", blocks: []chatBlock{p29TextBlock("new", "")}}}
	p30Settle(tt)
	t.Logf("session switch: alive %d -> %d releases=%d", aliveBefore, p30rAlive(state), calls.releases)
	if p30rAlive(state) != 0 {
		t.Errorf("views from the previous session stayed alive")
	}
}

var _ = mygo.WebView{}

// R4b: reader looking at text BELOW a card while it renders (both in view).
func TestR30ReadyHeightJumpsVisibleReader(t *testing.T) {
	for _, preset := range []appearancePreset{appearancePresetBYOP, appearancePresetJAgent} {
		p30WireDiagrams(t)
		restore := stableTestAppearance(t)
		state := layoutTestState(t)
		state.preferences.AppearancePreset = string(preset)
		fence := "```mermaid\n" + p30rSource(0) + "```"
		state.chat = []chatMessage{
			{role: chatUser, status: "Complete", blocks: []chatBlock{p29TextBlock("Q", "")}},
			{role: chatAssistant, status: "Complete", stopReason: "stop",
				blocks: []chatBlock{p29TextBlock(p30rFiller(4)+fence+"\n\nANCHOR after the diagram.\n\n"+p30rFiller(30), p29PhaseFinalAnswer)}},
		}
		tt := ui.NewTester(state.view, 1180, 760)
		p30Settle(tt)
		conv := &state.chatView.conversation
		conv.Scroll.Y = 0
		tt.Frame()
		tt.Frame()
		r0, ok := tt.Find("ANCHOR after the diagram.")
		if !ok {
			t.Fatalf("anchor missing")
		}
		d := state.inlineDiagramFor(p30rSource(0))
		state.applyInlineDiagramRender(d, d.renderSeq, map[string]any{"ok": true, "width": 600.0, "height": 480.0}, nil)
		tt.Frame()
		tt.Frame()
		r1, _ := tt.Find("ANCHOR after the diagram.")
		t.Logf("%s: Y=%v atLatest=%v anchor screen %v -> %v (delta %v)", preset, conv.Scroll.Y, conv.AtLatest(), r0.Y, r1.Y, r1.Y-r0.Y)
		restore()
	}
}

// R7: streaming: block closes mid-stream; later tokens must not rebuild the view.
func TestR30StreamingAfterClose(t *testing.T) {
	calls := p30WireDiagrams(t)
	restore := stableTestAppearance(t)
	defer restore()
	state := layoutTestState(t)
	state.run = runStreaming
	src := p30rSource(3)
	state.chat = []chatMessage{
		{role: chatUser, status: "Complete", blocks: []chatBlock{p29TextBlock("Q", "")}},
		{role: chatAssistant, status: "Streaming", blocks: []chatBlock{p29TextBlock("Here:\n```mermaid\n"+src, "")}},
	}
	tt := ui.NewTester(state.view, 1180, 1400)
	p30Settle(tt)
	openCreates := calls.creates
	txt := "Here:\n```mermaid\n" + src + "```\n\nAfter"
	for i := 0; i < 30; i++ {
		txt += " more"
		state.chat[1].blocks[0].text = txt
		tt.Advance(33 * time.Millisecond)
	}
	t.Logf("stream: creates while open=%d, after close+30 tokens=%d releases=%d", openCreates, calls.creates, calls.releases)
	if openCreates != 0 || calls.creates != 1 {
		t.Errorf("streaming rebuilt views: open=%d total=%d", openCreates, calls.creates)
	}
}

// R8: contrast of everything the P30 path paints that the matrix may not cover.
func TestR30ContrastGaps(t *testing.T) {
	type pal struct {
		name string
		p    palette
	}
	pals := []pal{
		{"byop-light", themePaletteForAppearance(appearancePresetBYOP, false, false, false)},
		{"byop-dark", themePaletteForAppearance(appearancePresetBYOP, true, false, false)},
		{"jagent", themePaletteForAppearance(appearancePresetJAgent, true, false, false)},
		{"jagent-rt", themePaletteForAppearance(appearancePresetJAgent, true, false, true)},
		{"hc", themePaletteForAppearance(appearancePresetBYOP, false, true, false)},
	}
	white := ui.Color{R: 255, G: 255, B: 255, A: 255}
	for _, pp := range pals {
		p := pp.p
		tones := p29TonesFor(p)
		bg := p29EffectiveBackground(p)
		// J Agent raised surface (code blocks, diagram card header) over white wallpaper
		raised := compositeOver(p.raisedSurface, white)
		if p.preset != appearancePresetJAgent || p.highContrast || p.reduceTransparency {
			raised = compositeOver(p.raisedSurface, bg)
		}
		// raised card nested inside turn card on JAgent: raised over turn-card-over-white
		nested := compositeOver(p.raisedSurface, bg)
		t.Logf("%-10s body p.text on bg=%.2f | diagram label tones.meta on raisedOverWhite=%.2f nested=%.2f | prompt text p.text on bg=%.2f | danger on bg=%.2f | tps-capsule ok", pp.name,
			wcagContrast(p.text, bg), wcagContrast(tones.meta, raised), wcagContrast(tones.meta, nested), wcagContrast(p.text, bg), wcagContrast(tones.danger, bg))
		// markdown answer body in J Agent uses theme default text color?
	}
}

func TestR30DangerValues(t *testing.T) {
	for _, c := range []struct {
		n string
		p palette
	}{
		{"byop-light", themePaletteForAppearance(appearancePresetBYOP, false, false, false)},
		{"hc-light", themePaletteForAppearance(appearancePresetBYOP, false, true, false)},
	} {
		bg := p29EffectiveBackground(c.p)
		t.Logf("%s danger=%#v dangerText=%#v bg=%#v danger=%.2f dangerText=%.2f", c.n, c.p.danger, c.p.dangerText, bg, wcagContrast(c.p.danger, bg), wcagContrast(c.p.dangerText, bg))
	}
}

// R9: J Agent glass card does not break the P29 settle compensation / fold.
func TestR30JAgentCardScrollRegression(t *testing.T) {
	for _, preset := range []appearancePreset{appearancePresetBYOP, appearancePresetJAgent} {
		restore := stableTestAppearance(t)
		state := p29revManualFoldState(t)
		state.preferences.AppearancePreset = string(preset)
		tt := ui.NewTester(state.view, 1180, 760)
		tt.Advance(300 * time.Millisecond)
		st := state.chatView.p29["p29-turn-0"]
		st.choice = 1
		tt.Advance(300 * time.Millisecond)
		conv := &state.chatView.conversation
		conv.Scroll.Y = 60
		tt.Frame()
		tt.Frame()
		summary := p29ProcessTitleParts(1, 1, false, "0s")
		r0, _ := tt.Find(summary)
		_ = tt.Click(summary)
		tt.Frame()
		tt.Advance(900 * time.Millisecond)
		r1, _ := tt.Find(summary)
		t.Logf("%s fold: summary %v -> %v Y=%v", preset, r0.Y, r1.Y, conv.Scroll.Y)
		if d := r1.Y - r0.Y; d > 2 || d < -2 {
			t.Errorf("%s: fold moved summary %v", preset, d)
		}
		restore()
	}
}

func TestR30ThrashTrace(t *testing.T) {
	calls := p30WireDiagrams(t)
	state := p30rHistory(t, 10, 0, appearancePresetBYOP)
	tt := ui.NewTester(state.view, 1180, 760)
	p30Settle(tt)
	for f := 0; f < 3; f++ {
		c0 := calls.creates
		tt.Frame()
		var row []string
		for i := 0; i < 10; i++ {
			d := state.inlineDiagramFor(p30rSource(i))
			st := state.chatView.diagramCards[d.Key]
			placed, y := false, float32(0)
			if st != nil {
				placed, y = st.contentKnown, st.contentY
			}
			row = append(row, fmt.Sprintf("%d:s%d/p%v/y%.0f/f%d", i, d.Status, placed, y, d.lastFrame))
		}
		t.Logf("frame %d (+%d creates) previewFrame=%d %v", f, calls.creates-c0, state.chatView.previewFrame, row)
	}
}

// R10: scale sent from Draw: repaint-only frames (AnimatePose / toolbar fade) must not spam setScale.
func TestR30ScaleSpam(t *testing.T) {
	calls := p30WireDiagrams(t)
	state := p30DiagramState(t, p30DiagramFence)
	p30LiveDiagram(t, state, p30DiagramSource, inlineDiagramLoading, 1440, 600)
	tt := ui.NewTester(state.view, 1180, 1400)
	p30Settle(tt)
	n0 := len(calls.scales)
	for i := 0; i < 30; i++ {
		tt.Advance(16 * time.Millisecond)
	}
	t.Logf("scales after settle=%d, after 30 idle frames=%d values=%v", n0, len(calls.scales), calls.scales)
	if len(calls.scales)-n0 > 0 {
		t.Errorf("idle frames resent scale %d times", len(calls.scales)-n0)
	}
}

// R11: ready on the latest turn while following the bottom (streaming answer then card renders).
func TestR30ReadyWhileFollowing(t *testing.T) {
	for _, preset := range []appearancePreset{appearancePresetBYOP, appearancePresetJAgent} {
		p30WireDiagrams(t)
		state := p30rHistory(t, 1, 30, preset)
		tt := ui.NewTester(state.view, 1180, 760)
		p30Settle(tt)
		conv := &state.chatView.conversation
		before := conv.AtLatest()
		d := state.inlineDiagramFor(p30rSource(0))
		state.applyInlineDiagramRender(d, d.renderSeq, map[string]any{"ok": true, "width": 600.0, "height": 480.0}, nil)
		tt.Frame()
		tt.Frame()
		t.Logf("%s: atLatest before=%v after=%v", preset, before, conv.AtLatest())
		if before && !conv.AtLatest() {
			t.Errorf("%s: card growth broke bottom following", preset)
		}
	}
}

// R12: the card is ABOVE the viewport (inside the 1-viewport build band) and renders.
func TestR30ReadyAboveReader(t *testing.T) {
	for _, preset := range []appearancePreset{appearancePresetBYOP, appearancePresetJAgent} {
		p30WireDiagrams(t)
		restore := stableTestAppearance(t)
		state := layoutTestState(t)
		state.preferences.AppearancePreset = string(preset)
		fence := "```mermaid\n" + p30rSource(0) + "```"
		state.chat = []chatMessage{
			{role: chatUser, status: "Complete", blocks: []chatBlock{p29TextBlock("Q", "")}},
			{role: chatAssistant, status: "Complete", stopReason: "stop",
				blocks: []chatBlock{p29TextBlock(fence+"\n\n"+p30rFiller(14)+"ANCHOR well below.\n\n"+p30rFiller(30), p29PhaseFinalAnswer)}},
		}
		tt := ui.NewTester(state.view, 1180, 760)
		p30Settle(tt)
		conv := &state.chatView.conversation
		conv.Scroll.Y = 0
		tt.Frame()
		tt.Frame()
		r, _ := tt.Find("ANCHOR well below.")
		conv.Scroll.Y += r.Y - 300
		tt.Frame()
		tt.Frame()
		d := state.inlineDiagramFor(p30rSource(0))
		st := state.diagramCardState(p30rSource(0))
		r0, _ := tt.Find("ANCHOR well below.")
		t.Logf("pre: placed=%v cardY=%v cardH=%v atLatest=%v", st.contentKnown, st.contentY, st.cardHeight, conv.AtLatest())
		state.applyInlineDiagramRender(d, d.renderSeq, map[string]any{"ok": true, "width": 600.0, "height": 480.0}, nil)
		tt.Frame()
		tt.Frame()
		r1, _ := tt.Find("ANCHOR well below.")
		t.Logf("%s: card status=%d cardY(last paint)=%v Y=%v anchor %v -> %v (delta %v)", preset, d.Status, st.contentY, conv.Scroll.Y, r0.Y, r1.Y, r1.Y-r0.Y)
		// A card finishing its render above the reader must not move the text
		// they are looking at: this is the jump that review round 1 measured at
		// 320 DIP, and it stayed unnoticed because nothing asserted it.
		if d.Status != inlineDiagramReady {
			t.Errorf("%s: card never finished rendering (status=%d)", preset, d.Status)
		}
		if delta := r1.Y - r0.Y; delta > 1 || delta < -1 {
			t.Errorf("%s: the reading point moved %v DIP when the card above finished", preset, delta)
		}
		restore()
	}
}
