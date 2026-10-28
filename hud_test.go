package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestP10cFormatPiTokens(t *testing.T) {
	tests := []struct {
		count int64
		want  string
	}{
		{count: -1, want: "0"},
		{count: 0, want: "0"},
		{count: 999, want: "999"},
		{count: 1000, want: "1.0k"},
		{count: 9999, want: "10.0k"},
		{count: 10000, want: "10k"},
		{count: 999999, want: "1000k"},
		{count: 1000000, want: "1.0M"},
		{count: 9999999, want: "10.0M"},
		{count: 10000000, want: "10M"},
	}
	for _, test := range tests {
		if got := formatPiTokens(test.count); got != test.want {
			t.Errorf("formatPiTokens(%d) = %q, want %q", test.count, got, test.want)
		}
	}
}

func TestP10cHUDLabelOmitsCwdAndKeepsBranch(t *testing.T) {
	contextPercent := 50.0
	state := &appState{
		piConn: piConnected,
		hud: hudState{
			cwd:    "/Users/example/Documents/private/byop-prototype",
			branch: "main",
			stats: &piSessionStats{
				Tokens: piSessionTokens{
					Input:      1200,
					Output:     300,
					CacheRead:  400,
					CacheWrite: 100,
				},
				Cost: 0.012,
				ContextUsage: &piContextUsage{
					ContextWindow: 128000,
					Percent:       &contextPercent,
				},
			},
		},
	}
	got := state.piHUDLabel()
	want := "Context 50.0% · 128K  main  ↑1.2k ↓300  $0.012  Cache 24%"
	if got != want {
		t.Fatalf("HUD label = %q, want %q", got, want)
	}
	if strings.Contains(got, "byop-prototype") || strings.Contains(got, "~") {
		t.Fatalf("HUD label exposed cwd: %q", got)
	}
}

func TestP10cHUDAccumulatorsAndSeverity(t *testing.T) {
	stats := &piSessionStats{
		Tokens: piSessionTokens{
			Input:      600,
			CacheRead:  300,
			CacheWrite: 100,
		},
	}
	if rate, ok := piCacheHitRate(stats); !ok || rate != 30 {
		t.Fatalf("cache hit rate = %.1f, %t; want 30, true", rate, ok)
	}
	if _, ok := piCacheHitRate(&piSessionStats{}); ok {
		t.Fatal("empty token stats produced a cache hit rate")
	}
	tests := []struct {
		percent    float64
		hasPercent bool
		want       int
	}{
		{percent: 70, hasPercent: true, want: 0},
		{percent: 70.1, hasPercent: true, want: 1},
		{percent: 90, hasPercent: true, want: 1},
		{percent: 90.1, hasPercent: true, want: 2},
		{percent: 100, hasPercent: false, want: 0},
	}
	for _, test := range tests {
		if got := hudContextSeverity(test.percent, test.hasPercent); got != test.want {
			t.Errorf("hudContextSeverity(%v, %t) = %d, want %d",
				test.percent, test.hasPercent, got, test.want)
		}
	}
}

func TestP10cHUDLabelOmitsUnknownContext(t *testing.T) {
	state := &appState{piConn: piConnected}
	if got := state.piHUDLabel(); got != "" {
		t.Fatalf("empty HUD = %q, want no label", got)
	}
	state.hud.contextWindowFallback = 128000
	if got := state.piHUDLabel(); got != "" {
		t.Fatalf("unknown-context HUD = %q, want no label", got)
	}
}

func TestP10cHUDStateAndStatusEvents(t *testing.T) {
	events := make(chan piEvent, 8)
	client := &piClient{events: func(event piEvent) { events <- event }}

	client.handleRecord([]byte(`{
		"type":"extension_ui_request",
		"id":"status-1",
		"method":"setStatus",
		"statusKey":"lint",
		"statusText":"  checking\r\nall\tfiles  "
	}`))
	setEvent := <-events
	if setEvent.Kind != piEventExtensionStatus || setEvent.StatusKey != "lint" ||
		setEvent.StatusText == nil || *setEvent.StatusText != "checking all files" {
		t.Fatalf("unexpected setStatus event: %#v", setEvent)
	}

	client.handleRecord([]byte(`{
		"type":"extension_ui_request",
		"id":"status-2",
		"method":"setStatus",
		"statusKey":"lint",
		"statusText":null
	}`))
	clearEvent := <-events
	if clearEvent.Kind != piEventExtensionStatus || clearEvent.StatusKey != "lint" ||
		clearEvent.StatusText != nil {
		t.Fatalf("unexpected clearStatus event: %#v", clearEvent)
	}

	state := &appState{
		piConn: piConnected,
		hud: hudState{
			autoCompact:           true,
			contextWindowFallback: 200000,
			extStatus:             map[string]string{"z": "last", "a": "first"},
		},
	}
	state.applyPiHUDState(&piSessionState{AutoCompactionEnabled: true})
	if got := state.piHUDLabel(); got != "first  last" {
		t.Fatalf("HUD label = %q", got)
	}
}

func TestP10cApplyHUDStatsRejectsStaleGenerationAndEpoch(t *testing.T) {
	client := &piClient{}
	state := &appState{
		pi:      client,
		piEpoch: 7,
		hud:     hudState{generation: 11},
	}
	staleGeneration := &piSessionStats{SessionID: "old-generation"}
	state.hud.statsRequest = 1
	state.applyPiHUDStats(10, 7, 1, client, staleGeneration, nil)
	if state.hud.stats != nil {
		t.Fatal("stale generation replaced HUD stats")
	}
	staleEpoch := &piSessionStats{SessionID: "old-epoch"}
	state.applyPiHUDStats(11, 6, 1, client, staleEpoch, nil)
	if state.hud.stats != nil {
		t.Fatal("stale epoch replaced HUD stats")
	}
	current := &piSessionStats{SessionID: "current"}
	state.applyPiHUDStats(11, 7, 1, client, current, nil)
	if state.hud.stats != current {
		t.Fatalf("current stats = %#v", state.hud.stats)
	}
	state.applyPiHUDStats(11, 7, 1, client, nil, errors.New("temporary stats failure"))
	if state.hud.stats != current {
		t.Fatal("temporary stats failure replaced the last successful stats")
	}
}

func TestP10cExtensionUICancelResponse(t *testing.T) {
	response := extensionUICancelledResponse("request-7")
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Type      string `json:"type"`
		ID        string `json:"id"`
		Cancelled bool   `json:"cancelled"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Type != "extension_ui_response" || decoded.ID != "request-7" || !decoded.Cancelled {
		t.Fatalf("unexpected extension response: %#v", decoded)
	}
}

func TestP10cRunningModeControlsAndDescription(t *testing.T) {
	if got, want := runningSendModeDescription(runningSendQueue), text("runningSendQueueDescription"); got != want {
		t.Fatalf("queue description = %q, want %q", got, want)
	}
	if got, want := runningSendModeDescription(runningSendSteer), text("runningSendSteerDescription"); got != want {
		t.Fatalf("steer description = %q, want %q", got, want)
	}
}
