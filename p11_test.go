package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestP11ModelFamiliesSelectContextVariant(t *testing.T) {
	models := []piModel{
		{Provider: "magpie", ID: "deepseek-256k", Name: "DeepSeek V4.1 Flash", ContextWindow: 256_000},
		{Provider: "magpie", ID: "deepseek-1m", Name: "DeepSeek V4.1 Flash", ContextWindow: 1_000_000},
		{Provider: "other", ID: "deepseek-other", Name: "DeepSeek V4.1 Flash", ContextWindow: 128_000},
	}
	state := piModelChoiceState{
		ActiveProvider: "magpie",
		ActiveID:       "deepseek-1m",
		Remembered: map[string]modelTuple{
			"magpie/DeepSeek V4.1 Flash": {VariantID: "deepseek-256k"},
		},
	}
	options, choices := buildPiModelChoices(models, state)
	if len(options) != 2 {
		t.Fatalf("model rows = %d, want 2 provider families", len(options))
	}
	if options[0].ID != "magpie/DeepSeek V4.1 Flash" {
		t.Fatalf("family row ID = %q", options[0].ID)
	}
	if options[0].Detail != "" {
		t.Fatalf("family row detail = %q, want empty", options[0].Detail)
	}
	if options[0].Description != "deepseek-256k" {
		t.Fatalf("remembered variant description = %q", options[0].Description)
	}
	if got := choices[options[0].ID].selected.ID; got != "deepseek-256k" {
		t.Fatalf("remembered selected variant = %q", got)
	}

	options, choices = buildPiModelChoices(models, piModelChoiceState{
		ActiveProvider: "magpie",
		ActiveID:       "deepseek-1m",
	})
	if options[0].Description != "deepseek-1m" {
		t.Fatalf("active variant description = %q", options[0].Description)
	}
	if got := choices[options[0].ID].selected.ID; got != "deepseek-1m" {
		t.Fatalf("active selected variant = %q", got)
	}
}

func TestP11ModelFamiliesMergeOnlyDistinctKnownContexts(t *testing.T) {
	tests := []struct {
		name  string
		a     piModel
		b     piModel
		rows  int
		rowID string
	}{
		{
			name: "different known windows",
			a: piModel{Provider: "magpie", ID: "deepseek-256k",
				Name: "DeepSeek V4.1 Flash", ContextWindow: 256_000},
			b: piModel{Provider: "magpie", ID: "deepseek-1m",
				Name: "DeepSeek V4.1 Flash", ContextWindow: 1_000_000},
			rows:  1,
			rowID: "magpie/DeepSeek V4.1 Flash",
		},
		{
			name: "same known window",
			a: piModel{Provider: "magpie", ID: "deepseek-1m-a",
				Name: "DeepSeek V4.1 Flash", ContextWindow: 1_000_000},
			b: piModel{Provider: "magpie", ID: "deepseek-1m-b",
				Name: "DeepSeek V4.1 Flash", ContextWindow: 1_000_000},
			rows:  2,
			rowID: "magpie/DeepSeek V4.1 Flash/deepseek-1m-a",
		},
		{
			name: "unknown window",
			a: piModel{Provider: "magpie", ID: "deepseek-unknown",
				Name: "DeepSeek V4.1 Flash"},
			b: piModel{Provider: "magpie", ID: "deepseek-128k",
				Name: "DeepSeek V4.1 Flash", ContextWindow: 128_000},
			rows:  2,
			rowID: "magpie/DeepSeek V4.1 Flash/deepseek-unknown",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			families := piModelFamilies([]piModel{test.a, test.b})
			options, choices := buildPiModelChoicesForFamilies(families)
			if len(families) != test.rows || len(options) != test.rows {
				t.Fatalf("families = %d, options = %d, want %d", len(families), len(options), test.rows)
			}
			if options[0].ID != test.rowID {
				t.Fatalf("first row ID = %q, want %q", options[0].ID, test.rowID)
			}
			if options[0].Detail != "" {
				t.Fatalf("row detail = %q, want empty", options[0].Detail)
			}
			selected := choices[test.rowID].selected.ID
			if options[0].Description != selected {
				t.Fatalf("description = %q, selected ID = %q", options[0].Description, selected)
			}
		})
	}
}

func TestP11bComposerShowsCapacityWithoutStandaloneContextControl(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	state := layoutTestState(t)
	state.piModelProvider = "magpie"
	state.piModelID = "deepseek-1m"
	state.piModelName = "DeepSeek V4.1 Flash"
	state.piModels = []piModel{
		{Provider: "magpie", ID: "deepseek-256k", Name: "DeepSeek V4.1 Flash", ContextWindow: 256_000},
		{Provider: "magpie", ID: "deepseek-1m", Name: "DeepSeek V4.1 Flash", ContextWindow: 1_000_000},
	}

	tt := ui.NewTester(state.view, 1180, 760)
	capacityLabels := 0
	for _, label := range tt.Texts() {
		if label == "1M" {
			capacityLabels++
		}
	}
	if capacityLabels != 1 {
		t.Fatalf("capacity label count = %d, want one inside the composer model control", capacityLabels)
	}
	if _, ok := tt.Find(text("contextLabel")); ok {
		t.Fatalf("standalone %q control rendered before opening the model picker", text("contextLabel"))
	}
	if err := tt.Click("DeepSeek V4.1 Flash"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find(text("contextLabel")); !ok {
		t.Fatalf("model picker did not render %q", text("contextLabel"))
	}
}

func TestP11bHUDContextFallbackClearsWhenPiReportsUnknown(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	state := layoutTestState(t)
	state.piModelProvider = "magpie"
	state.piModelID = "deepseek-unknown"
	state.piModelName = "DeepSeek V4.1 Flash"
	state.piModels = []piModel{{
		Provider: "magpie",
		ID:       "deepseek-unknown",
		Name:     "DeepSeek V4.1 Flash",
	}}
	known := &piSessionState{}
	known.Model.ContextWindow = 1_000_000
	state.applyPiHUDState(known)

	tt := ui.NewTester(state.view, 1180, 760)
	if _, ok := tt.Find("1M"); !ok {
		t.Fatal("composer did not show the initial 1M capacity")
	}

	state.applyPiHUDState(&piSessionState{})
	tt = ui.NewTester(state.view, 1180, 760)
	if _, ok := tt.Find("1M"); ok {
		t.Fatal("composer retained 1M after Pi reported an unknown capacity")
	}
}

func TestP11ModelBrandMatching(t *testing.T) {
	tests := []struct {
		provider string
		id       string
		name     string
		want     string
	}{
		{provider: "openai-compatible", id: "vendor/deepseek-v4", want: "deepseek"},
		{provider: "anthropic", id: "claude-opus", want: "claude"},
		{provider: "custom", name: "QWEN3 Max", want: "qwen"},
		{provider: "deepseek", id: "unknown", name: "Unknown", want: ""},
	}
	for _, test := range tests {
		if got := modelBrand(test.provider, test.id, test.name); got != test.want {
			t.Errorf("modelBrand(%q, %q, %q) = %q, want %q",
				test.provider, test.id, test.name, got, test.want)
		}
	}
}

func TestP11ThinkingRestoreUsesLevelsNotIndexes(t *testing.T) {
	remembered := modelTuple{Thinking: "high"}
	restore, defaulted := piThinkingRestore("low", remembered, true, []string{"low", "high"})
	if restore != "high" || defaulted {
		t.Fatalf("available remembered level = %q, defaulted=%t", restore, defaulted)
	}
	restore, defaulted = piThinkingRestore("low", remembered, true, []string{"low", "max"})
	if restore != "" || !defaulted {
		t.Fatalf("unavailable remembered level = %q, defaulted=%t", restore, defaulted)
	}
	restore, defaulted = piThinkingRestore("high", remembered, true, []string{"low", "high"})
	if restore != "" || defaulted {
		t.Fatalf("already-current level = %q, defaulted=%t", restore, defaulted)
	}
}

func TestP11AverageTokensPerSecond(t *testing.T) {
	state := &appState{}
	state.recordPiHUDSample(100, 2)
	state.recordPiHUDSample(150, 3)
	if got, ok := state.piHUDAverageTokensPerSecond(); !ok || got != 50 {
		t.Fatalf("average tok/s = %.1f, %t; want 50, true", got, ok)
	}
	state.recordPiHUDSample(10, 0)
	if got, _ := state.piHUDAverageTokensPerSecond(); got != 50 {
		t.Fatalf("invalid sample changed average to %.1f", got)
	}
}

func TestP11AssistantMessageTiming(t *testing.T) {
	events := make(chan piEvent, 2)
	client := &piClient{
		events:      func(event piEvent) { events <- event },
		streamParts: make(map[int]string),
	}
	nextMessage := func() piEvent {
		for event := range events {
			if event.Kind == piEventMessage {
				return event
			}
		}
		return piEvent{}
	}
	client.handleRecord([]byte(
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_start"}}`,
	))
	client.handleRecord([]byte(
		`{"type":"message_update","assistantMessageEvent":{"type":"text_start","contentIndex":0}}`,
	))
	client.streamMu.Lock()
	startedEarly := !client.generationStartedAt.IsZero()
	client.streamMu.Unlock()
	if startedEarly {
		t.Fatal("message timing started before the first thinking or text delta")
	}

	client.handleRecord([]byte(
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","delta":"checking"}}`,
	))
	time.Sleep(10 * time.Millisecond)
	client.handleRecord([]byte(
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"answer"}],"usage":{"output":42}}}`,
	))
	event := nextMessage()
	if event.Kind != piEventMessage || event.OutputTokens != 42 || event.GenerationTime <= 0 {
		t.Fatalf("assistant message timing = %#v", event)
	}

	client.handleRecord([]byte(
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"next"}],"usage":{"output":7}}}`,
	))
	event = nextMessage()
	if event.GenerationTime != 0 {
		t.Fatalf("assistant message inherited prior timing: %s", event.GenerationTime)
	}
}

func TestP11HUDLabelHasNoMiddleDot(t *testing.T) {
	percent := 3.7
	state := &appState{
		piConn: piConnected,
		hud: hudState{
			branch: "main",
			stats: &piSessionStats{
				Tokens:       piSessionTokens{Input: 12_000, Output: 3_000},
				Cost:         0.02,
				ContextUsage: &piContextUsage{Percent: &percent},
			},
			avgOutputTokens:      96,
			avgGenerationSeconds: 2,
			extStatus:            map[string]string{"lint": "checking"},
		},
	}
	label := state.piHUDLabel()
	if strings.Contains(label, "·") {
		t.Fatalf("HUD label contains a middle dot: %q", label)
	}
	if !strings.HasPrefix(label, "main  ↑12k ↓3.0k  $0.020  ") ||
		!strings.Contains(label, "  Context 3.7%  checking") {
		t.Fatalf("HUD label order or spacing = %q", label)
	}
}

func TestP11ModelTuplesMigrateFromSessionIDToFile(t *testing.T) {
	sessionID := "session-id"
	sessionFile := filepath.Join(t.TempDir(), "session.jsonl")
	familyID := piModelChoiceID("magpie", "DeepSeek V4.1 Flash")
	want := modelTuple{VariantID: "deepseek-1m", Thinking: "high"}
	state := &appState{
		piSessionID: sessionID,
		modelTuples: map[string]map[string]modelTuple{
			sessionID: {familyID: want},
		},
	}
	state.applyPiSessionState(&piSessionState{
		SessionID:   sessionID,
		SessionFile: sessionFile,
	})
	sessionKey := canonicalPiPath(sessionFile)
	if got := state.modelTuples[sessionKey][familyID]; got != want {
		t.Fatalf("migrated tuple = %#v, want %#v", got, want)
	}
	if _, exists := state.modelTuples[sessionID]; exists {
		t.Fatal("session-ID tuple key survived migration")
	}
}

func TestP11ThinkingFallbackKeepsRememberedLevel(t *testing.T) {
	models := []piModel{
		{Provider: "magpie", ID: "deepseek-256k", Name: "DeepSeek V4.1 Flash", ContextWindow: 256_000},
		{Provider: "magpie", ID: "deepseek-1m", Name: "DeepSeek V4.1 Flash", ContextWindow: 1_000_000},
	}
	families := piModelFamilies(models)
	sessionKey := "/tmp/p11-session.jsonl"
	remembered := modelTuple{VariantID: "deepseek-1m", Thinking: "max"}
	state := &appState{}
	state.recordModelTupleFor(families, sessionKey, "magpie", "deepseek-1m",
		"DeepSeek V4.1 Flash", "low")
	state.recordModelTupleFor(families, sessionKey, "magpie", "deepseek-1m",
		"DeepSeek V4.1 Flash", remembered.Thinking)
	tuple := state.modelTuples[sessionKey][piModelChoiceID("magpie", "DeepSeek V4.1 Flash")]
	if tuple.VariantID != remembered.VariantID || tuple.Thinking != remembered.Thinking {
		t.Fatalf("fallback tuple = %#v, want remembered tuple %#v", tuple, remembered)
	}
}

func TestP11ConfirmedThinkingWithoutRecordKeepsRememberedLevel(t *testing.T) {
	models := []piModel{{
		Provider: "magpie", ID: "deepseek-1m", Name: "DeepSeek V4.1 Flash", ContextWindow: 1_000_000,
	}}
	sessionFile := filepath.Join(t.TempDir(), "session.jsonl")
	familyID := piModelChoiceID("magpie", "DeepSeek V4.1 Flash")
	remembered := modelTuple{VariantID: "deepseek-1m", Thinking: "max"}
	state := &appState{
		piModels:      models,
		piSessionFile: sessionFile,
		modelTuples: map[string]map[string]modelTuple{
			sessionFile: {familyID: remembered},
		},
	}
	confirmed := &piSessionState{
		SessionFile:   sessionFile,
		ThinkingLevel: "low",
	}
	confirmed.Model.Provider = "magpie"
	confirmed.Model.ID = "deepseek-1m"
	confirmed.Model.Name = "DeepSeek V4.1 Flash"
	state.applyConfirmedThinkingStateWithoutRecord(confirmed, []string{"low", "high"}, true, true)
	if got := state.modelTuples[sessionFile][familyID]; got != remembered {
		t.Fatalf("pre-restore tuple = %#v, want remembered tuple %#v", got, remembered)
	}
}

func TestP11ConfirmedStateClearsUnconfirmed(t *testing.T) {
	state := &appState{modelStateUnconfirmed: true}
	state.applyPiSessionState(&piSessionState{})
	if state.modelStateUnconfirmed {
		t.Fatal("confirmed session state left model controls locked")
	}
}

func TestP11StaleSessionOperationDoesNotClearBusy(t *testing.T) {
	client := &piClient{}
	state := &appState{
		pi:            client,
		piConn:        piConnected,
		piEpoch:       3,
		piSessionID:   "session-id",
		piSessionBusy: false,
		sessionGen:    10,
	}
	_, capturedEpoch, capturedGen, ok := state.beginPiSessionOperation("read back")
	if !ok || !state.piSessionBusy {
		t.Fatal("session operation did not start")
	}
	state.sessionGen++
	if state.finishPiSessionOperation(client, capturedEpoch, capturedGen) {
		t.Fatal("stale session operation reported completion")
	}
	if !state.piSessionBusy {
		t.Fatal("stale session operation cleared busy")
	}
}
