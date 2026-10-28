package main

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestP18ThinkingDeltaStreamsSeparatelyFromAssistantText(t *testing.T) {
	events := make(chan piEvent, 16)
	client := &piClient{
		events:        func(event piEvent) { events <- event },
		streamParts:   make(map[int]string),
		thinkingParts: make(map[int]string),
	}
	records := []string{
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_start","contentIndex":0}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"check the evidence"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_end","contentIndex":0,"content":"check the evidence"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_start","contentIndex":1}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"answer"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_end","contentIndex":1,"content":"answer"}}`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"thinking","thinking":"check the evidence"},{"type":"text","text":"answer"}],"usage":{"output":3}}}`,
	}
	for _, record := range records {
		client.handleRecord([]byte(record))
	}
	close(events)

	var thinking, text, message piEvent
	for event := range events {
		switch event.Kind {
		case piEventThinking:
			thinking = event
		case piEventText:
			text = event
		case piEventMessage:
			message = event
		}
	}
	if thinking.Text != "check the evidence" || thinking.Delta != "" || !thinking.ThinkingEnded {
		t.Fatalf("thinking stream = %#v", thinking)
	}
	if strings.Contains(text.Text, "check the evidence") || text.Text != "answer" {
		t.Fatalf("assistant stream included thinking: %q", text.Text)
	}
	if message.Thinking != "check the evidence" || message.Text != "answer" {
		t.Fatalf("final message = %#v", message)
	}
}

func TestP18_1RunningQueueUsesCompactRowContractWithoutOverflow(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	state := &appState{
		preferences: byopPreferences{
			Motion: string(motionReduced),
		},
		run:                runStreaming,
		queueSteering:      []string{"Keep the current answer focused.", "Check the second source."},
		queueFollowUp:      []string{"Then summarize the result.", "Finally list the remaining risks."},
		queueSteeringCount: 2,
		queueFollowUpCount: 2,
	}
	const width, height = 360, 180
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).FillWidth().Padding(16).Children(func() {
			state.runningQueueBar(c)
		})
	}, width, height)
	tt.Advance(300 * time.Millisecond)

	for _, label := range []string{
		text("queueTitle"),
		state.queueSteering[0],
		state.queueFollowUp[0],
		text("queueGuideLabel"),
		text("queueFollowUpLabel"),
		text("queueRemove"),
		text("queueMoreActions"),
		text("queueClear"),
		textArgs("queueMore", map[string]any{"count": 1}),
	} {
		rect, ok := tt.Find(label)
		if !ok || rect.W <= 0 || rect.H <= 0 {
			t.Fatalf("queue did not render %q: %#v", label, rect)
		}
		if rect.X < 0 || rect.X+rect.W > width {
			t.Fatalf("queue label %q escaped %dpx width: %#v", label, width, rect)
		}
	}

	remove, _ := tt.Find(text("queueRemove"))
	tt.Move(remove.X+remove.W/2, remove.Y+remove.H/2)
	tt.Advance(700 * time.Millisecond)
	if !tt.HasText(text("queueItemActionsUnavailable")) {
		t.Fatal("disabled per-item control did not expose the whole-queue-only explanation")
	}
}

func TestP18ThinkingNeverBecomesAssistantMessageText(t *testing.T) {
	state := &appState{
		piConn: piConnected,
		chat: []chatMessage{{
			role:      chatAssistant,
			status:    text("assistantWaiting"),
			startedAt: time.Now(),
		}},
	}
	state.handlePiEvent(piEvent{Kind: piEventThinking, Text: "private planning"}, 0)
	state.handlePiEvent(piEvent{Kind: piEventText, Text: "visible answer"}, 0)
	state.handlePiEvent(piEvent{
		Kind:     piEventMessage,
		Text:     "visible answer",
		Thinking: "private planning refined",
	}, 0)

	message := state.currentAssistant()
	if message.thinking != "private planning refined" {
		t.Fatalf("thinking = %q", message.thinking)
	}
	if got := messageText(message); got != "visible answer" {
		t.Fatalf("assistant text = %q, want visible answer only", got)
	}
}

func TestP18CurrentTPSThresholdsUseSemanticThemeColors(t *testing.T) {
	tests := []struct {
		rate float64
		tier piTPSTier
	}{
		{rate: 80, tier: piTPSExcellent},
		{rate: 79.9, tier: piTPSFast},
		{rate: 40, tier: piTPSFast},
		{rate: 39.9, tier: piTPSModerate},
		{rate: 20, tier: piTPSModerate},
		{rate: 19.9, tier: piTPSLow},
		{rate: 5, tier: piTPSLow},
		{rate: 4.9, tier: piTPSPoor},
	}
	theme := ui.LightTheme()
	for _, test := range tests {
		if got := piTPSTierFor(test.rate); got != test.tier {
			t.Errorf("piTPSTierFor(%.1f) = %d, want %d", test.rate, got, test.tier)
		}
		foreground, _, _ := piTPSStyle(theme, test.rate)
		want := theme.Danger
		switch test.tier {
		case piTPSExcellent:
			want = theme.Success
		case piTPSFast:
			want = theme.Success.Mix(theme.TextMuted, .30)
		case piTPSModerate:
			want = theme.Warning
		case piTPSLow:
			want = theme.TextMuted
		}
		if foreground != want {
			t.Errorf("piTPSStyle(%.1f) foreground = %#v, want %#v",
				test.rate, foreground, want)
		}
	}

	state := &appState{}
	start := time.Unix(100, 0)
	state.observePiHUDSpeedAt("a", start)
	state.observePiHUDSpeedAt(strings.Repeat("x", 76), start.Add(time.Second))
	if !state.hud.currentTPSKnown || state.hud.currentTPS != 20 {
		t.Fatalf("live TPS = %.1f, known=%t; want 20, true",
			state.hud.currentTPS, state.hud.currentTPSKnown)
	}
}

func TestP18HUDShowsCurrentModelContextAndResetsStaleSpeed(t *testing.T) {
	state := &appState{
		piConn:          piConnected,
		piModelProvider: "magpie",
		piModelID:       "deepseek-1m",
		piModelName:     "DeepSeek V4.1 Flash",
	}
	used := int64(282_000)
	percent := 45.4
	state.hud = hudState{
		branch:      "main",
		autoCompact: true,
		stats: &piSessionStats{ContextUsage: &piContextUsage{
			Tokens:        &used,
			ContextWindow: 1_000_000,
			Percent:       &percent,
		}},
		currentTPS:      51.2,
		currentTPSKnown: true,
	}
	label := state.piHUDLabel()
	for _, want := range []string{
		"DeepSeek V4.1 Flash",
		"Context 45.4% · 282K / 1M (auto)",
		"51.2 t/s",
	} {
		if !strings.Contains(label, want) {
			t.Fatalf("HUD label %q does not contain %q", label, want)
		}
	}
	state.hud.currentTPSUpdatedAt = time.Now().Add(-piTPSStaleAfter - time.Second)
	if label = state.piHUDLabel(); strings.Contains(label, "51.2 t/s") {
		t.Fatalf("HUD retained stale TPS: %q", label)
	}

	state.resetPiHUDData()
	if state.hud.currentTPSKnown || state.hud.stats != nil {
		t.Fatal("HUD reset retained stale stats or speed")
	}
	session := &piSessionState{ThinkingLevel: "high"}
	session.Model.Provider = "other"
	session.Model.ID = "claude-new"
	session.Model.Name = "Claude New"
	session.Model.ContextWindow = 200_000
	state.applyPiSessionState(session)
	label = state.piHUDLabel()
	if !strings.Contains(label, "Claude New") || strings.Contains(label, "DeepSeek V4.1 Flash") ||
		strings.Contains(label, "51.2 t/s") {
		t.Fatalf("HUD retained stale model or speed: %q", label)
	}
}

func TestP18ThinkingEffortOptionsFollowPiCapability(t *testing.T) {
	levels := buildPiThinkingOptions([]string{"max", "low", "off", "high", "low"})
	if len(levels) != 4 {
		t.Fatalf("effort option count = %d, want 4", len(levels))
	}
	got := make([]string, len(levels))
	for i, level := range levels {
		got[i] = level.ID
	}
	if want := []string{"off", "low", "high", "max"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("effort option order = %v, want %v", got, want)
	}
}

func TestP18ThinkingTracksOwnCompletionAndCJKTokenEstimate(t *testing.T) {
	state := &appState{
		piConn: piConnected,
		chat: []chatMessage{{
			role:      chatAssistant,
			status:    text("assistantWaiting"),
			startedAt: time.Now().Add(-time.Minute),
		}},
	}
	state.handlePiEvent(piEvent{Kind: piEventThinking, ThinkingStarted: true}, 0)
	state.handlePiEvent(piEvent{Kind: piEventThinking, Text: "核对上下文", Delta: "核对上下文"}, 0)
	state.handlePiEvent(piEvent{Kind: piEventThinking, Text: "核对上下文", ThinkingEnded: true}, 0)
	message := state.currentAssistant()
	if message.thinkingStartedAt.IsZero() || message.thinkingEndedAt.IsZero() {
		t.Fatal("thinking timestamps were not recorded")
	}
	if !message.thinkingEndedAt.After(message.thinkingStartedAt) {
		t.Fatalf("thinking interval = %v..%v", message.thinkingStartedAt, message.thinkingEndedAt)
	}
	if got := piHUDEstimatedTokens("你好世界"); got != 4 {
		t.Fatalf("CJK token estimate = %.1f, want 4", got)
	}
}

func TestP18EffortSliderFillIsVisibleForActiveTheme(t *testing.T) {
	theme := ui.LightTheme()
	if got := effortSliderFillColor(theme, appearancePresetBYOP); got != theme.Accent {
		t.Fatalf("BYOP effort fill = %#v, want accent", got)
	}
	if got := effortSliderFillColor(theme, appearancePresetJAgent); got != theme.Focus {
		t.Fatalf("J Agent effort fill = %#v, want focus", got)
	}
}
