package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components/chat"
)

func TestP9cRunningSendKeyDecision(t *testing.T) {
	tests := []struct {
		name        string
		key         ui.Key
		mods        ui.Modifiers
		hasText     bool
		running     bool
		defaultMode runningSendMode
		want        composerSendDecision
	}{
		{
			name:        "plain enter uses default queue mode",
			key:         ui.KeyEnter,
			hasText:     true,
			running:     true,
			defaultMode: runningSendQueue,
			want:        composerSendDecision{handled: true, running: true, mode: runningSendQueue},
		},
		{
			name:        "command enter uses opposite steer mode",
			key:         ui.KeyEnter,
			mods:        ui.Cmd,
			hasText:     true,
			running:     true,
			defaultMode: runningSendQueue,
			want:        composerSendDecision{handled: true, running: true, mode: runningSendSteer},
		},
		{
			name:        "command enter uses opposite queue mode",
			key:         ui.KeyEnter,
			mods:        ui.Cmd,
			hasText:     true,
			running:     true,
			defaultMode: runningSendSteer,
			want:        composerSendDecision{handled: true, running: true, mode: runningSendQueue},
		},
		{
			name:        "shift enter remains a newline",
			key:         ui.KeyEnter,
			mods:        ui.Shift,
			hasText:     true,
			running:     true,
			defaultMode: runningSendQueue,
		},
		{
			name:        "unrelated modifier is untouched",
			key:         ui.KeyEnter,
			mods:        ui.Alt,
			hasText:     true,
			running:     true,
			defaultMode: runningSendQueue,
		},
		{
			name:        "plain enter sends idle draft",
			key:         ui.KeyEnter,
			hasText:     true,
			defaultMode: runningSendQueue,
			want:        composerSendDecision{handled: true},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := runningSendKeyDecision(test.key, test.mods, test.hasText, test.running, test.defaultMode)
			if got != test.want {
				t.Fatalf("decision = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestAssistantToolOnly(t *testing.T) {
	tests := []struct {
		name    string
		message *chatMessage
		want    bool
	}{
		{
			name: "tool groups only",
			message: &chatMessage{
				role: chatAssistant,
				blocks: []chatBlock{
					{kind: chatBlockText, text: " \n"},
					{kind: chatBlockTool},
				},
			},
			want: true,
		},
		{
			name: "assistant text breaks grouping",
			message: &chatMessage{
				role: chatAssistant,
				blocks: []chatBlock{
					{kind: chatBlockTool},
					{kind: chatBlockText, text: "answer"},
				},
			},
		},
		{
			name:    "user message",
			message: &chatMessage{role: chatUser},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := assistantToolOnly(test.message); got != test.want {
				t.Fatalf("assistantToolOnly() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestP16EmptyTranscriptForegroundsOnlyForJAgentWallpaper(t *testing.T) {
	title, hint, wallpaperVisible := emptyTranscriptForegrounds(
		appearancePresetJAgent, "jagent-misty-mountains", true, false, false,
	)
	if !wallpaperVisible {
		t.Fatal("J Agent wallpaper should use dark transcript foregrounds")
	}
	p := themePaletteForAppearance(appearancePresetJAgent, true, false, false)
	if title != ui.Hex("#20242B") || hint != ui.Hex("#353B43") {
		t.Fatalf("empty transcript foregrounds = %v / %v", title, hint)
	}
	if title == p.text {
		t.Fatal("wallpaper title color must differ from the original light title color")
	}
	if hint == p.muted {
		t.Fatal("wallpaper hint color must differ from the original muted hint color")
	}

	tests := []struct {
		name         string
		preset       appearancePreset
		wallpaper    string
		high, reduce bool
	}{
		{name: "BYOP wallpaper", preset: appearancePresetBYOP, wallpaper: "ember"},
		{name: "J Agent without wallpaper", preset: appearancePresetJAgent, wallpaper: "none"},
		{name: "J Agent high contrast", preset: appearancePresetJAgent, wallpaper: "jagent-misty-mountains", high: true},
		{name: "J Agent reduced transparency", preset: appearancePresetJAgent, wallpaper: "jagent-misty-mountains", reduce: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			title, hint, got := emptyTranscriptForegrounds(
				test.preset, test.wallpaper, true, test.high, test.reduce,
			)
			if got || title != (ui.Color{}) || hint != (ui.Color{}) {
				t.Fatal("foreground override should remain limited to visible J Agent wallpaper")
			}
		})
	}
}

func TestPromptToolGroupEndIncludesAdjacentToolOnlyMessages(t *testing.T) {
	messages := []chatMessage{
		{role: chatUser},
		{role: chatAssistant, blocks: []chatBlock{{kind: chatBlockTool}}},
		{role: chatAssistant, blocks: []chatBlock{{kind: chatBlockTool}}},
		{role: chatAssistant, blocks: []chatBlock{{kind: chatBlockText, text: "answer"}}},
		{role: chatUser},
		{role: chatAssistant, blocks: []chatBlock{{kind: chatBlockText, text: "answer"}}},
	}
	if got, ok := promptToolGroupEnd(messages, 0); !ok || got != 2 {
		t.Fatalf("prompt tool group end = %d, %t; want 2, true", got, ok)
	}
	if got, ok := promptToolGroupEnd(messages, 4); ok || got != 4 {
		t.Fatalf("text answer should not group: end=%d, ok=%t", got, ok)
	}
}

func TestP9cQueueDisplayItems(t *testing.T) {
	items, overflow := queueDisplayItems(
		[]string{"steer one", " ", "steer two"},
		[]string{"follow one", "follow two", ""},
		3,
	)
	if len(items) != 3 {
		t.Fatalf("items = %#v", items)
	}
	if overflow != 1 {
		t.Fatalf("overflow = %d, want 1", overflow)
	}
	if items[0].text != "steer one" || items[2].text != "follow one" {
		t.Fatalf("item order/content = %#v", items)
	}
	if !items[0].steering || items[2].steering {
		t.Fatalf("item mode semantics = %#v", items)
	}
}

func TestP9cRunningComposerEnterQueuesWithoutSend(t *testing.T) {
	draft := "next"
	queued := 0
	sends := 0
	stops := 0
	tt := ui.NewTester(func(c *ui.Context) {
		palette := themePalette(c.Theme().Dark)
		result := opaqueComposerV12(c, palette, &draft, chat.ComposerOptions{
			Label:       "Message",
			Placeholder: "Message",
			SendLabel:   "Stop",
			StopLabel:   "Stop",
			Running:     true,
		})
		if result.Input.Focused() && strings.TrimSpace(draft) != "" {
			composing := result.Input.Composing()
			result.Input.HandleInput(func(ev ui.InputEvent) bool {
				return handleRunningComposerInput(
					ev,
					composing,
					draft,
					runningSendQueue,
					func(prompt string, mode runningSendMode) {
						queued++
					},
				)
			})
		}
		if result.Send {
			sends++
		}
		if result.Stop {
			stops++
		}
	}, 400, 240)
	if err := tt.Click("Message"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEnter)
	if queued != 1 || sends != 0 || stops != 0 {
		t.Fatalf("plain Enter: queued=%d sends=%d stops=%d", queued, sends, stops)
	}
}

func TestP9cRunningSendCompletionPreservesActiveRequest(t *testing.T) {
	client := &piClient{}
	state := &appState{
		pi:                 client,
		piEpoch:            4,
		sessionGen:         7,
		run:                runStreaming,
		activeRequest:      "active",
		runID:              "active",
		runningSendPending: true,
		composerDraft:      "",
	}
	if !state.completeRunningSend(client, "queued prompt", runningSendQueue, 4, 7,
		"active", "active", runStreaming,
		piPromptResult{RequestID: "queued", Disposition: "queued"}, nil) {
		t.Fatal("expected running send to complete")
	}
	if state.activeRequest != "active" || state.runID != "active" {
		t.Fatalf("active run changed: request=%q run=%q", state.activeRequest, state.runID)
	}
}

func TestP10cRejectedRunningSendLeavesDraftIntact(t *testing.T) {
	tests := []struct {
		name  string
		state *appState
	}{
		{
			name: "disconnected client",
			state: &appState{
				run:           runStreaming,
				composerDraft: "keep this draft",
			},
		},
		{
			name: "session operation in progress",
			state: &appState{
				pi:            &piClient{},
				piConn:        piConnected,
				piSessionBusy: true,
				run:           runStreaming,
				composerDraft: "keep this draft",
				activeRequest: "active",
			},
		},
		{
			name: "compacting",
			state: &appState{
				pi:            &piClient{},
				piConn:        piConnected,
				run:           runStreaming,
				compacting:    true,
				composerDraft: "keep this draft",
				activeRequest: "active",
			},
		},
		{
			name: "not running",
			state: &appState{
				pi:            &piClient{},
				piConn:        piConnected,
				run:           runIdle,
				composerDraft: "keep this draft",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.state.sendRunningPrompt(test.state.composerDraft, runningSendQueue)
			if test.state.composerDraft != "keep this draft" {
				t.Fatalf("draft = %q, want rejected draft preserved", test.state.composerDraft)
			}
			if test.state.runningSendPending {
				t.Fatal("rejected running send entered pending state")
			}
		})
	}
}

func TestP9cRunningSendFailureRestoresDraft(t *testing.T) {
	client := &piClient{}
	state := &appState{
		pi:                 client,
		piEpoch:            4,
		sessionGen:         7,
		run:                runStreaming,
		activeRequest:      "active",
		runID:              "active",
		runningSendPending: true,
	}
	if state.completeRunningSend(client, "retry me", runningSendSteer, 4, 7,
		"active", "active", runStreaming,
		piPromptResult{RequestID: "failed", Disposition: "queued"}, errors.New("queue unavailable")) {
		t.Fatal("failed running send should not report success")
	}
	if state.composerDraft != "retry me" {
		t.Fatalf("draft = %q", state.composerDraft)
	}
	if state.activeRequest != "active" || state.runID != "active" {
		t.Fatalf("active run changed: request=%q run=%q", state.activeRequest, state.runID)
	}
}

func TestP9cRunningSendStaleResultIgnored(t *testing.T) {
	client := &piClient{}
	state := &appState{
		pi:                 client,
		piEpoch:            5,
		sessionGen:         8,
		run:                runStreaming,
		activeRequest:      "new",
		runID:              "new",
		runningSendPending: true,
		composerDraft:      "keep this draft",
	}
	if state.completeRunningSend(client, "stale prompt", runningSendQueue, 4, 7,
		"old", "old", runStreaming,
		piPromptResult{RequestID: "stale", Disposition: "queued"}, errors.New("stale")) {
		t.Fatal("stale result should not complete")
	}
	if state.composerDraft != "keep this draft" || state.activeRequest != "new" {
		t.Fatalf("stale result mutated state: %#v", state)
	}
}

func TestP9cRunningSendStartedStartsNewTurn(t *testing.T) {
	client := &piClient{}
	state := &appState{
		pi:                  client,
		piEpoch:             4,
		sessionGen:          7,
		run:                 runStreaming,
		activeRequest:       "active",
		runID:               "active",
		runningSendPending:  true,
		runningSendPrompt:   "start a new run",
		runningSendEchoSeen: false,
	}
	if !state.completeRunningSend(client, "start a new run", runningSendQueue, 4, 7,
		"active", "active", runStreaming,
		piPromptResult{RequestID: "next", Disposition: "started"}, nil) {
		t.Fatal("expected started response to complete")
	}
	if state.activeRequest != "next" || state.runID != "next" || state.activePrompt != "start a new run" {
		t.Fatalf("active run = request:%q run:%q prompt:%q", state.activeRequest, state.runID, state.activePrompt)
	}
	if len(state.chat) != 2 || state.chat[0].role != chatUser || state.chat[1].role != chatAssistant {
		t.Fatalf("started response did not create a user/assistant turn: %#v", state.chat)
	}
}

func TestP9cRunningSendEchoCreatesTurnWithoutDuplicate(t *testing.T) {
	client := &piClient{}
	state := &appState{
		pi:                  client,
		piEpoch:             4,
		sessionGen:          7,
		run:                 runStreaming,
		activeRequest:       "active",
		runID:               "active",
		runningSendPending:  true,
		runningSendPrompt:   "steer now",
		runningSendEchoSeen: false,
		chat: []chatMessage{{
			role:    chatAssistant,
			status:  "Streaming",
			blocks:  []chatBlock{{kind: chatBlockText, text: "previous answer"}},
			endedAt: time.Now(),
		}},
	}
	state.appendRunningUserMessage("steer now")
	if !state.runningSendEchoSeen {
		t.Fatal("running-send echo was not recorded")
	}
	if !state.completeRunningSend(client, "steer now", runningSendSteer, 4, 7,
		"active", "active", runStreaming,
		piPromptResult{RequestID: "next", Disposition: "started"}, nil) {
		t.Fatal("expected started response to complete")
	}
	if len(state.chat) != 3 {
		t.Fatalf("echo created %d messages, want 3: %#v", len(state.chat), state.chat)
	}
	if state.chat[0].role != chatAssistant || state.chat[1].role != chatUser || state.chat[2].role != chatAssistant {
		t.Fatalf("unexpected timeline roles: %#v", state.chat)
	}
	if got := messageText(&state.chat[0]); got != "previous answer" {
		t.Fatalf("previous answer = %q", got)
	}
}

func TestP9cStaleRunningSendFailureRestoresDraftAndNotice(t *testing.T) {
	client := &piClient{}
	state := &appState{
		pi:                 client,
		piEpoch:            5,
		sessionGen:         8,
		run:                runStreaming,
		activeRequest:      "new",
		runID:              "new",
		runningSendPending: true,
		runningSendPrompt:  "retry stale",
	}
	if state.completeRunningSend(client, "retry stale", runningSendQueue, 4, 7,
		"old", "old", runStreaming,
		piPromptResult{RequestID: "stale"}, errors.New("stale failure")) {
		t.Fatal("stale failure should not report success")
	}
	if state.composerDraft != "retry stale" {
		t.Fatalf("draft = %q, want restored prompt", state.composerDraft)
	}
	if state.notice == "" {
		t.Fatal("stale failure did not show a notice")
	}
	if state.activeRequest != "new" || state.runID != "new" {
		t.Fatalf("stale failure changed active run: %#v", state)
	}
}

func TestP9cQueueClearSuccessZeroesQueue(t *testing.T) {
	state := &appState{
		queueSteering:      nil,
		queueFollowUp:      nil,
		queueSteeringCount: 0,
		queueFollowUpCount: 0,
	}
	steering := []string{"steer"}
	followUp := []string{"follow", "later"}
	if got := clearedQueueCount(steering, followUp); got != 3 {
		t.Fatalf("cleared count = %d, want 3", got)
	}
	state.finishQueueClear()
	if len(state.queueSteering) != 0 || len(state.queueFollowUp) != 0 ||
		state.queueSteeringCount != 0 || state.queueFollowUpCount != 0 {
		t.Fatalf("queue was not cleared: %#v", state)
	}
}
