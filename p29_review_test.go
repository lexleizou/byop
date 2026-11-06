package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components/chat"
)

// Ports of the independent review's reproduction cases (zz_ui_review_test
// .go, U1–U8), written as assertions against the fixed behaviour rather
// than logs.

func p29revLive() (*appState, *piClient) {
	state := &appState{piConn: piConnected, run: runStreaming, chat: []chatMessage{
		{role: chatUser, blocks: []chatBlock{{kind: chatBlockText, text: "q"}}},
		{role: chatAssistant, status: "Streaming"},
	}}
	client := &piClient{streamParts: map[int]string{}, thinkingParts: map[int]string{}}
	client.events = func(e piEvent) { e.Source = nil; state.handlePiEvent(e, 0) }
	return state, client
}

func p29revFeed(client *piClient, lines ...string) {
	for _, line := range lines {
		client.handleRecord([]byte(line))
	}
}

func p29revAssistants(chat []chatMessage) []*chatMessage {
	var out []*chatMessage
	for i := range chat {
		if chat[i].role == chatAssistant {
			out = append(out, &chat[i])
		}
	}
	return out
}

func TestP29ReviewU1ThinkingOnlyLiveMatchesHistory(t *testing.T) {
	state, client := p29revLive()
	p29revFeed(client,
		`{"type":"agent_start"}`,
		`{"type":"message_start","message":{"role":"assistant","content":[]}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"only thought"}}`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"thinking","thinking":"only thought"}],"stopReason":"stop"}}`,
		`{"type":"agent_settled"}`,
	)
	live := buildP29Turn(p29revAssistants(state.chat), false)
	restored, err := rebuildChatFromPiMessages([]json.RawMessage{
		json.RawMessage(`{"role":"user","content":"q"}`),
		json.RawMessage(`{"role":"assistant","content":[{"type":"thinking","thinking":"only thought"}],"stopReason":"stop"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	history := buildP29Turn(p29revAssistants(restored), false)
	if live.finalKind != p29FinalThinkingOnly ||
		history.finalKind != p29FinalThinkingOnly {
		t.Fatalf("live=%d history=%d, want both ThinkingOnly", live.finalKind, history.finalKind)
	}
	if live.finalText != "only thought" || history.finalText != "only thought" {
		t.Fatalf("live=%q history=%q", live.finalText, history.finalText)
	}
	if len(live.noticeLines) != 0 || len(history.noticeLines) != 0 {
		t.Fatalf("thinking-only turns must not carry notices: %v / %v",
			live.noticeLines, history.noticeLines)
	}
}

func TestP29ReviewU1bToolOnlyEndShowsOneNote(t *testing.T) {
	state, client := p29revLive()
	p29revFeed(client,
		`{"type":"agent_start"}`,
		`{"type":"message_start","message":{"role":"assistant","content":[]}}`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"toolCall","id":"A","name":"read","arguments":{}}],"stopReason":"toolUse"}}`,
		`{"type":"tool_execution_start","toolCallId":"A","toolName":"read","args":{}}`,
		`{"type":"tool_execution_end","toolCallId":"A","toolName":"read","result":{"content":[{"type":"text","text":"x"}]}}`,
		`{"type":"message_start","message":{"role":"assistant","content":[]}}`,
		`{"type":"message_end","message":{"role":"assistant","content":[],"stopReason":"stop"}}`,
		`{"type":"agent_settled"}`,
	)
	turn := buildP29Turn(p29revAssistants(state.chat), false)
	if turn.finalKind != p29FinalNoText {
		t.Fatalf("final=%d, want NoText", turn.finalKind)
	}
	// Review P2-2: the completed placeholder leaves the process and
	// never turns into a notice; only the bottom note states the end.
	for _, node := range turn.nodes {
		if node.kind == p29NodeCommentary {
			t.Fatalf("placeholder duplicated in the process: %q", node.text)
		}
	}
	if len(turn.noticeLines) != 0 {
		t.Fatalf("completed placeholder became a notice: %v", turn.noticeLines)
	}
	if turn.toolCount != 1 {
		t.Fatalf("toolCount=%d, want 1", turn.toolCount)
	}
}

func TestP29ReviewU2CopyPrefersFinalAnswer(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	message := p29Assistant(
		p29TextBlock("I will check the file first.", p29PhaseCommentary),
		p29ToolBlock("c1", "read", toolCallCompleted),
		p29TextBlock("Final answer.", p29PhaseFinalAnswer),
	)
	message.stopReason = "stop"
	state := p18_2HUDState(t)
	state.run = runCompleted
	state.chat = []chatMessage{
		{role: chatUser, status: "Complete", blocks: []chatBlock{p29TextBlock("Q", "")}},
		*message,
	}
	tt := ui.NewTester(state.view, 1180, 760)
	if _, ok := tt.Find(text("chatCopy")); !ok {
		t.Fatalf("copy action missing: %#v", tt.Texts())
	}
	if err := tt.Click(text("chatCopy")); err != nil {
		t.Fatal(err)
	}
	if got := tt.Clipboard(); got != "Final answer." {
		t.Fatalf("clipboard=%q, want the final answer without commentary", got)
	}
}

// p29revSettleLong builds a long, live final turn for scroll probes.
func p29revSettleLong(t *testing.T) *appState {
	t.Helper()
	state := p29LiveCapsuleState(t)
	state.preferences.Motion = string(motionStandard)
	var earlier []chatMessage
	for i := 0; i < 8; i++ {
		earlier = append(earlier,
			chatMessage{role: chatUser, status: "Complete",
				blocks: []chatBlock{p29TextBlock("older question", "")}},
			chatMessage{role: chatAssistant, status: "Complete", stopReason: "stop",
				blocks: []chatBlock{p29TextBlock(strings.Repeat("older answer line. ", 40), "")}},
		)
	}
	state.chat = append(earlier, state.chat...)
	return state
}

func p29revSettleLastTurn(state *appState) {
	last := len(state.chat) - 1
	state.chat[last].blocks[1] = p29ToolBlock("call-1", "read", toolCallCompleted)
	state.chat[last].blocks = append(state.chat[last].blocks,
		p29TextBlock(strings.Repeat("This is the final answer text. ", 30), ""))
	state.chat[last].status = "Complete"
	state.chat[last].stopReason = "stop"
	state.run = runCompleted
}

func TestP29ReviewU3SettleNearBottomKeepsScroll(t *testing.T) {
	for _, preset := range []appearancePreset{appearancePresetBYOP, appearancePresetJAgent} {
		restore := stableTestAppearance(t)
		state := p29revSettleLong(t)
		state.preferences.AppearancePreset = string(preset)
		tt := ui.NewTester(state.view, 1180, 760)
		tt.Advance(400 * time.Millisecond)
		conv := &state.chatView.conversation
		conv.Scroll.Y = max(0, conv.Scroll.MaxY-120)
		tt.Frame()
		tt.Frame()
		before := conv.Scroll.Y
		p29revSettleLastTurn(state)
		tt.Frame()
		tt.Advance(1200 * time.Millisecond)
		tt.Frame()
		if d := conv.Scroll.Y - before; d < -2 || d > 2 {
			t.Errorf("%s: reading position moved by %v while the answer grew below it",
				preset, d)
		}
		restore()
	}
}

func TestP29ReviewU3bSettleScrolledOutCompensatesProcessOnly(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	state := p29revSettleLong(t)
	tt := ui.NewTester(state.view, 1180, 760)
	tt.Advance(400 * time.Millisecond)
	conv := &state.chatView.conversation
	// Deep scroll-up: the final turn's header and process are far below
	// the viewport; nothing in the settling turn is visible.
	conv.Scroll.Y = 40
	tt.Frame()
	tt.Frame()
	before := conv.Scroll.Y
	p29revSettleLastTurn(state)
	tt.Frame()
	tt.Advance(1200 * time.Millisecond)
	tt.Frame()
	// Nothing above the reading point changed except through the
	// process compensation; with the wrapper out of view the anchor may
	// shift by at most the process shrink — never by the answer growth.
	moved := conv.Scroll.Y - before
	if moved > 2 || moved < -90 {
		t.Fatalf("scroll moved %v on settle while scrolled far up; only the process shrink may compensate", moved)
	}
}

func p29revManualFoldState(t *testing.T) *appState {
	t.Helper()
	state := p29LiveCapsuleState(t)
	state.preferences.Motion = string(motionStandard)
	state.chat[1].status = "Complete"
	state.chat[1].stopReason = "stop"
	state.chat[1].blocks[1] = p29ToolBlock("call-1", "read", toolCallCompleted)
	state.chat[1].blocks = append(state.chat[1].blocks,
		p29TextBlock(strings.Repeat("Answer body. ", 300), ""))
	state.run = runCompleted
	return state
}

func TestP29ReviewU4ManualFoldKeepsClickedRowStill(t *testing.T) {
	for _, preset := range []appearancePreset{appearancePresetBYOP, appearancePresetJAgent} {
		for _, startOpen := range []bool{false, true} {
			restore := stableTestAppearance(t)
			state := p29revManualFoldState(t)
			state.preferences.AppearancePreset = string(preset)
			tt := ui.NewTester(state.view, 1180, 760)
			tt.Advance(300 * time.Millisecond)
			st := state.chatView.p29["p29-turn-0"]
			st.choice = 2
			if startOpen {
				st.choice = 1
			}
			tt.Advance(300 * time.Millisecond)
			conv := &state.chatView.conversation
			conv.Scroll.Y = 60
			tt.Frame()
			tt.Frame()
			summary := p29ProcessTitleParts(1, 1, false, "0s")
			r0, ok := tt.Find(summary)
			if !ok {
				t.Fatalf("%s startOpen=%v: summary missing", preset, startOpen)
			}
			scrollBefore := conv.Scroll.Y
			if err := tt.Click(summary); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			tt.Advance(900 * time.Millisecond)
			tt.Frame()
			r1, _ := tt.Find(summary)
			if d := r1.Y - r0.Y; d < -2 || d > 2 {
				t.Errorf("%s startOpen=%v: clicked summary moved by %v", preset, startOpen, d)
			}
			if conv.Scroll.Y != scrollBefore {
				t.Errorf("%s startOpen=%v: manual fold wrote Scroll.Y %v → %v",
					preset, startOpen, scrollBefore, conv.Scroll.Y)
			}
			restore()
		}
	}
}

func TestP29ReviewU4cReducedMotionFoldKeepsRowStill(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	state := p29revManualFoldState(t)
	state.preferences.Motion = string(motionReduced)
	tt := ui.NewTester(state.view, 1180, 760)
	tt.Frame()
	st := state.chatView.p29["p29-turn-0"]
	st.choice = 1
	tt.Frame()
	tt.Frame()
	conv := &state.chatView.conversation
	conv.Scroll.Y = 60
	tt.Frame()
	tt.Frame()
	summary := p29ProcessTitleParts(1, 1, false, "0s")
	r0, ok := tt.Find(summary)
	if !ok {
		t.Fatal("summary missing")
	}
	if err := tt.Click(summary); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	r1, _ := tt.Find(summary)
	if d := r1.Y - r0.Y; d < -2 || d > 2 {
		t.Fatalf("reduced-motion fold moved the summary by %v", d)
	}
	if conv.Scroll.Y != 60 {
		t.Fatalf("reduced-motion fold wrote Scroll.Y to %v", conv.Scroll.Y)
	}
}

func TestP29ReviewU5LatestHistoryTurnKeepsOwnModel(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	state := p18_2HUDState(t)
	state.run = runCompleted
	state.piModels = append(state.piModels, piModel{
		Provider: "custom", ID: "historian-7b", Name: "Historian 7B",
	})
	state.chat = []chatMessage{
		{role: chatUser, status: "Complete", blocks: []chatBlock{p29TextBlock("Q", "")}},
		{role: chatAssistant, status: "Complete", stopReason: "stop",
			model: "historian-7b", provider: "custom",
			blocks: []chatBlock{p29TextBlock("A", "")}},
	}
	tt := ui.NewTester(state.view, 1180, 760)
	if !tt.HasText("Historian 7B") {
		t.Fatalf("latest restored turn labelled with the current model %q: %#v",
			state.piHUDModelName(), tt.Texts())
	}
}

func TestP29ReviewU6AbortedCommentaryStaysInProcess(t *testing.T) {
	state, client := p29revLive()
	p29revFeed(client,
		`{"type":"agent_start"}`,
		`{"type":"message_start","message":{"role":"assistant","content":[]}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"Let me read."}}`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Let me read."},{"type":"toolCall","id":"A","name":"read","arguments":{"path":"x"}}],"stopReason":"toolUse"}}`,
		`{"type":"tool_execution_start","toolCallId":"A","toolName":"read","args":{"path":"x"}}`,
		`{"type":"agent_settled","aborted":true}`,
	)
	turn := buildP29Turn(p29revAssistants(state.chat), false)
	if turn.status != chat.Cancelled {
		t.Fatalf("status=%v, want cancelled", turn.status)
	}
	if turn.finalKind != p29FinalNone {
		t.Fatalf("final=%d, want None for an aborted mid-run turn", turn.finalKind)
	}
	kept := false
	for _, node := range turn.nodes {
		if node.kind == p29NodeCommentary && node.text == "Let me read." {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("real commentary dropped from the process: %#v", turn.nodes)
	}
}

func TestP29ReviewU7NodeEnterAndDetailUseNativeTransition(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	state := p29LiveCapsuleState(t)
	state.preferences.Motion = string(motionStandard)
	tt := ui.NewTester(state.view, 1180, 760)
	tt.Advance(400 * time.Millisecond)
	body := "Commentary line that just streamed in."
	state.chat[1].blocks = append(state.chat[1].blocks,
		p29TextBlock(body, ""), p29ThinkBlock("next", ""))
	tt.Frame()
	st := state.chatView.p29["p29-turn-0"]
	if !st.nodeProbeMoving {
		t.Fatal("the new node did not start a native transition")
	}
	tt.Advance(40 * time.Millisecond)
	midH := st.nodeProbeH
	tt.Advance(120 * time.Millisecond)
	laterH := st.nodeProbeH
	tt.Advance(300 * time.Millisecond)
	endH := st.nodeProbeH
	// The probe box height runs in flight (0 < mid < full) and lands at
	// the node's natural height; the framework's own transition test
	// shows the same 0 → half → full curve in headless frames.
	if !(midH < laterH && laterH <= endH) || endH < 20 || midH >= endH {
		t.Fatalf("node height must climb to full: mid=%v later=%v end=%v", midH, laterH, endH)
	}
	if st.nodeProbeMoving {
		t.Fatal("the node transition never stopped")
	}
	if !tt.HasText(body) {
		t.Fatal("the new commentary is not shown after entering")
	}

	// A settled turn's first detail open animates through the native
	// transition too.
	state2 := p29revManualFoldState(t)
	state2.chat[1].blocks = state.chat[1].blocks[:0] // unused long body
	state2.chat[1].blocks = []chatBlock{
		p29ThinkBlock("Where to read?", ""),
		p29ToolBlock("call-1", "read", toolCallCompleted),
		p29TextBlock("done", ""),
	}
	state2.run = runCompleted
	tt2 := ui.NewTester(state2.view, 1180, 760)
	st2 := state2.chatView.p29["p29-turn-0"]
	if st2.nodeProbeMoving {
		t.Fatal("history armed a node transition on its first frames")
	}
	summary := p29ProcessTitleParts(1, 1, false, "0s")
	if err := tt2.Click(summary); err != nil {
		t.Fatal(err)
	}
	tt2.Advance(700 * time.Millisecond)
	head := text("p29ReadDone") + " " + textArgs("p29ProcessSteps", map[string]any{"count": 2})
	if err := tt2.Click(head); err != nil {
		t.Fatal(err)
	}
	tt2.Frame()
	// The appearing frame draws the collapsed box (zero height); the
	// motion then climbs to the natural height within 150 ms.
	openStart := st2.detailProbeH
	tt2.Advance(40 * time.Millisecond)
	if !st2.detailProbeMoving {
		t.Fatal("the detail did not run a native transition")
	}
	openMid := st2.detailProbeH
	tt2.Advance(200 * time.Millisecond)
	openEnd := st2.detailProbeH
	if !(openStart < openMid && openMid < openEnd) || openEnd < 40 {
		t.Fatalf("detail height must climb from collapsed to full: %v %v %v",
			openStart, openMid, openEnd)
	}
	if st2.detailProbeMoving {
		t.Fatal("the detail transition never stopped")
	}
	if !tt2.HasText("Where to read?") {
		t.Fatal("thinking detail missing after opening")
	}
}

func TestP29ReviewU8FailureReasonStaysVisible(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	state := p18_2HUDState(t)
	state.run = runCompleted
	restored, err := rebuildChatFromPiMessages([]json.RawMessage{
		json.RawMessage(`{"role":"user","content":"q"}`),
		json.RawMessage(`{"role":"assistant","content":[{"type":"thinking","thinking":"plan it"}],"stopReason":"error","errorMessage":"429 rate limit exceeded"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	state.chat = restored
	tt := ui.NewTester(state.view, 1180, 760)
	tt.Advance(300 * time.Millisecond)
	// The process folds by default; the 429 must still read below it.
	if !tt.HasText("429 rate limit exceeded") {
		t.Fatalf("error message hidden inside the folded process: %#v", tt.Texts())
	}
	if _, ok := tt.Find(text("p29ThinkingDone")); ok {
		t.Fatal("activity status should stay inside the fold")
	}
	// Live failure at the model level: the reason is a notice line, not
	// a process node.
	liveState, client := p29revLive()
	p29revFeed(client,
		`{"type":"agent_start"}`,
		`{"type":"message_start","message":{"role":"assistant","content":[]}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"plan"}}`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"thinking","thinking":"plan"}],"stopReason":"error","errorMessage":"429 rate limit exceeded"}}`,
		`{"type":"agent_settled"}`,
	)
	turn := buildP29Turn(p29revAssistants(liveState.chat), false)
	if turn.status != chat.Failed {
		t.Fatalf("live failure status=%v", turn.status)
	}
	// A live failure: the concrete error lands in the session slot, the
	// injected fallback in the notice lines, and the transcript shows
	// both below the fold for the latest turn.
	if len(turn.noticeLines) == 0 {
		t.Fatalf("live failure lost its fallback notice: %#v", turn.nodes)
	}
	if !strings.Contains(liveState.piError, "429 rate limit exceeded") {
		t.Fatalf("live piError=%q", liveState.piError)
	}
	visState := p18_2HUDState(t)
	visState.run = runCompleted
	visState.piError = liveState.piError
	visState.chat = p29revAssistantsClone(liveState.chat)
	ttLive := ui.NewTester(visState.view, 1180, 760)
	ttLive.Advance(300 * time.Millisecond)
	if !ttLive.HasText("429 rate limit exceeded") {
		t.Fatalf("live failure reason not visible: %#v", ttLive.Texts())
	}
	for _, node := range turn.nodes {
		if node.kind == p29NodeCommentary && strings.Contains(node.text, "429") {
			t.Fatalf("error reason duplicated inside the process: %#v", turn.nodes)
		}
	}
}

func p29revAssistantsClone(chat []chatMessage) []chatMessage {
	out := make([]chatMessage, len(chat))
	copy(out, chat)
	return out
}

// R3 (second re-review) regressions, ported from zz_r3_test.go as
// assertions. Pre-fix behaviour, recorded on the unfixed tree: the
// settle scenario panicked with "element from pass 108 used in pass
// 109" (dev build), streaming left the row transition moving on 36 of
// 40 frames and 106 px behind, the JAgent settle at a scrolled-up
// reading point jumped the anchor by 26.6 px, and the live failure
// showed the 429 next to the generic "see runtime details" placeholder.

// r3Stream runs one streaming session and reports the number of frames
// the node transition reported moving, plus the conversation extent
// when the stream stops and after it settles.
func r3Stream(t *testing.T, preset appearancePreset, motion motionMode) (moving int, stopMax, settledMax float32) {
	t.Helper()
	restore := stableTestAppearance(t)
	defer restore()
	state := p29LiveCapsuleState(t)
	state.preferences.AppearancePreset = string(preset)
	state.preferences.Motion = string(motion)
	state.chat[1].blocks = append(state.chat[1].blocks, p29TextBlock("Start.", ""))
	tt := ui.NewTester(state.view, 1180, 760)
	tt.Advance(600 * time.Millisecond)
	st := state.chatView.p29["p29-turn-0"]
	conv := &state.chatView.conversation
	idx := len(state.chat[1].blocks) - 1
	txt := "Start."
	for i := 0; i < 40; i++ {
		txt += " streaming words keep arriving one chunk at a time"
		state.chat[1].blocks[idx].text = txt
		tt.Advance(33 * time.Millisecond)
		if st.nodeProbeMoving {
			moving++
		}
	}
	stopMax = conv.Scroll.MaxY
	tt.Advance(600 * time.Millisecond)
	settledMax = conv.Scroll.MaxY
	return moving, stopMax, settledMax
}

func TestP29R3StreamingGrowthIsImmediate(t *testing.T) {
	for _, preset := range []appearancePreset{appearancePresetBYOP, appearancePresetJAgent} {
		moving, stopMax, settledMax := r3Stream(t, preset, motionStandard)
		_, redStop, redSettled := r3Stream(t, preset, motionReduced)
		// Size updates must not restart the enter curve: the row
		// transition only moves on a real enter/exit, never while the
		// same row grows.
		if moving > 2 {
			t.Errorf("%s: node transition moved on %d/40 streaming frames, want near 0",
				preset, moving)
		}
		// Growth lands the same frame: what the reader can reach when
		// the stream stops must not trail the reduced-motion run by
		// more than a wrapped line.
		if d := redStop - stopMax; d > 30 {
			t.Errorf("%s: extent trails reduced by %v px while streaming", preset, d)
		}
		if d := settledMax - redSettled; d > 2 || d < -2 {
			t.Errorf("%s: settled extent %v differs from reduced %v", preset, settledMax, redSettled)
		}
	}
}

func TestP29R3SettleOpenProcessKeepsReadingPoint(t *testing.T) {
	for _, preset := range []appearancePreset{appearancePresetBYOP, appearancePresetJAgent} {
		for _, readAt := range []float32{150, 400} {
			restore := stableTestAppearance(t)
			state := p29LiveCapsuleState(t)
			state.preferences.AppearancePreset = string(preset)
			state.preferences.Motion = string(motionStandard)
			for i := 0; i < 12; i++ {
				state.chat[1].blocks = append(state.chat[1].blocks,
					p29TextBlock("Commentary paragraph number "+string(rune('A'+i))+" "+strings.Repeat("with extra words ", 12), ""),
					p29ToolBlock("call-x"+string(rune('A'+i)), "bash", toolCallCompleted))
			}
			state.chat[1].blocks = append(state.chat[1].blocks,
				p29TextBlock("FINAL "+strings.Repeat("answer words ", 40), ""))
			tt := ui.NewTester(state.view, 1180, 760)
			tt.Advance(800 * time.Millisecond)
			conv := &state.chatView.conversation
			key := ""
			for _, s := range tt.Texts() {
				if strings.HasPrefix(s, "Commentary paragraph number F") {
					key = s
				}
			}
			if key == "" {
				t.Fatalf("%s: anchor paragraph not found", preset)
			}
			r, ok := tt.Find(key)
			if !ok {
				t.Fatalf("%s: anchor not findable", preset)
			}
			conv.Scroll.Y += r.Y - readAt
			tt.Frame()
			tt.Frame()
			r0, _ := tt.Find(key)
			// This exact settle sequence is the one that, with the
			// probe's Transitioning() read inside Draw, panicked with a
			// stale element handle on the first settle frame in dev
			// builds. A panic here fails the test outright.
			state.chat[1].blocks[1] = p29ToolBlock("call-1", "read", toolCallCompleted)
			state.chat[1].status = "Complete"
			state.chat[1].stopReason = "stop"
			state.run = runCompleted
			tt.Frame()
			for i := 0; i < 10; i++ {
				tt.Advance(80 * time.Millisecond)
				ri, ok := tt.Find(key)
				if !ok {
					continue
				}
				if d := ri.Y - r0.Y; d < -2 || d > 2 {
					t.Fatalf("%s readAt=%v frame %d: reading anchor moved by %v px on settle",
						preset, readAt, i, d)
				}
			}
			if conv.AtLatest() {
				t.Errorf("%s readAt=%v: reading point was yanked to the end", preset, readAt)
			}
			restore()
		}
	}
}

func TestP29R3LiveFailureShowsReasonOnce(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	state := p18_2HUDState(t)
	state.chat = []chatMessage{
		{role: chatUser, blocks: []chatBlock{{kind: chatBlockText, text: "q"}}},
		{role: chatAssistant, status: "Streaming"},
	}
	state.run = runStreaming
	state.handlePiEvent(piEvent{Kind: piEventAssistantStart}, state.piEpoch)
	state.handlePiEvent(piEvent{Kind: piEventThinking, Text: "plan", PartText: "plan", PartSet: true}, state.piEpoch)
	state.handlePiEvent(piEvent{Kind: piEventMessage, StopReason: "error", Error: "429 rate limit exceeded",
		OrderedContent: []piContentEntry{{Kind: "thinking", Text: "plan"}}}, state.piEpoch)
	state.pi = nil
	state.handlePiEvent(piEvent{Kind: piEventSettled}, state.piEpoch)
	tt := ui.NewTester(state.view, 1180, 760)
	tt.Advance(300 * time.Millisecond)
	reason := 0
	placeholder := 0
	for _, s := range tt.Texts() {
		if strings.Contains(s, "429 rate limit exceeded") {
			reason++
		}
		if strings.Contains(s, "runtime details") {
			placeholder++
		}
	}
	if reason != 1 {
		t.Fatalf("429 shown %d times, want once", reason)
	}
	if placeholder != 0 {
		t.Fatalf("generic placeholder still shown next to the real error (%d)", placeholder)
	}
}

func TestP29R3ClickDuringEnterNoStaleHandle(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	state := p29LiveCapsuleState(t)
	state.preferences.Motion = string(motionStandard)
	tt := ui.NewTester(state.view, 1180, 760)
	tt.Advance(400 * time.Millisecond)
	state.chat[1].blocks = append(state.chat[1].blocks, p29TextBlock("new commentary", ""))
	tt.Frame()
	// A click consumes an event, forcing a multi-pass build while the
	// node transition is in flight — the exact pass-reuse that turned
	// the old Draw-closure Transitioning() read into a stale handle.
	if err := tt.Click(p29ProcessTitleParts(1, 1, true, "0s")); err != nil {
		t.Fatal(err)
	}
	tt.Advance(100 * time.Millisecond)
	tt.Frame()
}
