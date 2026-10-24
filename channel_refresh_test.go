package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

type channelRefreshFakeClient struct {
	mu sync.Mutex

	raw          *piClient
	initial      piSessionState
	stateResults []piSessionState
	models       []piModel
	levels       []string
	messages     []json.RawMessage
	cancelled    bool
	switchErr    error
	setModelErr  error
	setThinkErr  error
	readbackErr  error
	modelsErr    error
	messagesErr  error

	calls  []string
	closed bool
	events func(piEvent)
}

type channelRefreshUIQueue struct {
	callbacks chan func()
}

func newChannelRefreshUIQueue() *channelRefreshUIQueue {
	return &channelRefreshUIQueue{
		callbacks: make(chan func(), 16),
	}
}

func (q *channelRefreshUIQueue) post(fn func()) {
	q.callbacks <- fn
}

func (q *channelRefreshUIQueue) flush() {
	for {
		select {
		case fn := <-q.callbacks:
			fn()
		default:
			return
		}
	}
}

func installChannelRefreshTestUI(state *appState) *channelRefreshUIQueue {
	ui := newChannelRefreshUIQueue()
	state.channelRefresh.post = ui.post
	return ui
}

func (f *channelRefreshFakeClient) GetState(context.Context) (*piSessionState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "get_state")
	if f.readbackErr != nil {
		err := f.readbackErr
		f.readbackErr = nil
		return nil, err
	}
	state := f.initial
	if len(f.stateResults) > 0 {
		state = f.stateResults[0]
		f.stateResults = f.stateResults[1:]
	}
	return &state, nil
}

func (f *channelRefreshFakeClient) SwitchSession(_ context.Context, path string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "switch_session:"+path)
	return f.cancelled, f.switchErr
}

func (f *channelRefreshFakeClient) SetModel(_ context.Context, provider, id string) (piModel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "set_model:"+provider+"/"+id)
	return piModel{Provider: provider, ID: id}, f.setModelErr
}

func (f *channelRefreshFakeClient) SetThinkingLevel(_ context.Context, level string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "set_thinking_level:"+level)
	return f.setThinkErr
}

func (f *channelRefreshFakeClient) GetAvailableModels(context.Context) ([]piModel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "get_available_models")
	return clonePiModels(f.models), f.modelsErr
}

func (f *channelRefreshFakeClient) GetAvailableThinkingLevels(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "get_available_thinking_levels")
	return append([]string(nil), f.levels...), nil
}

func (f *channelRefreshFakeClient) GetMessages(context.Context) ([]json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "get_messages")
	return append([]json.RawMessage(nil), f.messages...), f.messagesErr
}

func (f *channelRefreshFakeClient) Close() {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
}

func (f *channelRefreshFakeClient) rawClient() *piClient {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.raw == nil {
		waitDone := make(chan struct{})
		close(waitDone)
		f.raw = &piClient{
			path:        "/tmp/pi",
			cwd:         "/tmp",
			stdin:       channelRefreshTestWriteCloser{},
			events:      f.events,
			pending:     make(map[string]chan piRPCResponse),
			waitDone:    waitDone,
			streamParts: make(map[int]string),
		}
	}
	return f.raw
}

func (f *channelRefreshFakeClient) callNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func TestChannelRefreshBusyWaitsAndStartsOnceWhenIdle(t *testing.T) {
	state, oldClient := newChannelRefreshTestState()
	ui := installChannelRefreshTestUI(state)
	state.run = runStreaming
	launches := 0
	state.channelRefresh.launch = func(_ string, _ string, _ func(piEvent)) (channelRefreshClient, error) {
		launches++
		return newChannelRefreshFakeClient(), nil
	}

	state.markChannelRefreshPending()
	state.pumpChannelRefresh()
	if launches != 0 {
		t.Fatalf("busy refresh launched %d Pi process(es)", launches)
	}
	if got := state.channelRefreshStatus(); got.Phase != channelRefreshPhasePending || !got.Pending {
		t.Fatalf("busy refresh status = %#v", got)
	}
	if state.pi != oldClient {
		t.Fatal("busy refresh replaced the active Pi client")
	}

	state.run = runIdle
	state.pumpChannelRefresh()
	state.pumpChannelRefresh()
	waitForChannelRefreshPhase(t, state, channelRefreshPhaseSucceeded, ui)
	if launches != 1 {
		t.Fatalf("idle refresh launched %d Pi process(es), want 1", launches)
	}
}

func TestChannelRefreshRestoresSessionAndSwapsAfterReadback(t *testing.T) {
	state, oldClient := newChannelRefreshTestState()
	ui := installChannelRefreshTestUI(state)
	state.composerDraft = "keep this draft"
	state.modelTuples["session-1"] = map[string]modelTuple{
		"deepseek/fast": {VariantID: "deepseek-chat", Thinking: "high"},
	}
	restored := *channelRefreshTestStateValue()
	newClient := newChannelRefreshFakeClient()
	newClient.stateResults = []piSessionState{restored, restored}
	newClient.models = []piModel{{Provider: "deepseek", ID: "deepseek-chat"}}
	newClient.messages = channelRefreshTestMessages()
	state.channelRefresh.launch = func(_ string, _ string, events func(piEvent)) (channelRefreshClient, error) {
		newClient.events = events
		return newClient, nil
	}

	state.markChannelRefreshPending()
	waitForChannelRefreshPhase(t, state, channelRefreshPhaseSucceeded, ui)

	calls := newClient.callNames()
	want := []string{
		"get_state",
		"switch_session:/tmp/byop-refresh-session.jsonl",
		"set_model:deepseek/deepseek-chat",
		"set_thinking_level:high",
		"get_state",
		"get_available_models",
		"get_available_thinking_levels",
		"get_messages",
	}
	if len(calls) != len(want) {
		t.Fatalf("RPC calls = %#v, want %#v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("RPC call %d = %q, want %q", i, calls[i], want[i])
		}
	}
	if state.pi == oldClient || state.pi != newClient.rawClient() {
		t.Fatal("refresh did not atomically replace the old Pi client")
	}
	if state.composerDraft != "keep this draft" {
		t.Fatalf("composer draft changed to %q", state.composerDraft)
	}
	if got := state.modelTuples["session-1"]["deepseek/fast"]; got.VariantID != "deepseek-chat" || got.Thinking != "high" {
		t.Fatalf("tuple memory changed: %#v", got)
	}
	if state.piModelProvider != "deepseek" || state.piModelID != "deepseek-chat" ||
		state.piThinkingLevel != "high" || state.piSessionFile != "/tmp/byop-refresh-session.jsonl" {
		t.Fatalf("restored state was not applied: %#v", state)
	}
	if len(state.chat) != 2 || len(state.chat[1].blocks) != 1 ||
		state.chat[1].blocks[0].text != "restored reply" {
		t.Fatalf("restored transcript = %#v", state.chat)
	}
	if !oldClient.closed.Load() {
		t.Fatal("old Pi client remained open after successful swap")
	}
}

func TestChannelRefreshFailureRetainsOldClientAndRetries(t *testing.T) {
	state, oldClient := newChannelRefreshTestState()
	ui := installChannelRefreshTestUI(state)
	failedClient := newChannelRefreshFakeClient()
	failedClient.setThinkErr = errors.New("thinking rejected")
	launches := 0
	state.channelRefresh.launch = func(_ string, _ string, _ func(piEvent)) (channelRefreshClient, error) {
		launches++
		if launches == 1 {
			return failedClient, nil
		}
		return newChannelRefreshFakeClient(), nil
	}

	state.markChannelRefreshPending()
	waitForChannelRefreshPhase(t, state, channelRefreshPhaseFailed, ui)
	status := state.channelRefreshStatus()
	if !status.Retryable || status.Pending || status.Failure != channelRefreshFailureThinkingUnavailable {
		t.Fatalf("failed status = %#v", status)
	}
	if state.pi != oldClient || !failedClient.closed {
		t.Fatal("failure did not preserve the old client and close the candidate")
	}
	if state.piSessionFile != "/tmp/byop-refresh-session.jsonl" ||
		state.piModelID != "deepseek-chat" || state.piThinkingLevel != "high" {
		t.Fatal("failure mutated the visible session")
	}

	state.retryChannelRefresh()
	waitForChannelRefreshPhase(t, state, channelRefreshPhaseSucceeded, ui)
	if launches != 2 {
		t.Fatalf("retry launched %d times, want 2", launches)
	}
}

func TestChannelRefreshFailureWaitsForRetry(t *testing.T) {
	state, oldClient := newChannelRefreshTestState()
	ui := installChannelRefreshTestUI(state)
	launches := 0
	state.channelRefresh.launch = func(_ string, _ string, _ func(piEvent)) (channelRefreshClient, error) {
		launches++
		client := newChannelRefreshFakeClient()
		client.switchErr = errors.New("session unavailable")
		return client, nil
	}

	state.markChannelRefreshPending()
	waitForChannelRefreshPhase(t, state, channelRefreshPhaseFailed, ui)
	state.pumpChannelRefresh()
	state.pumpChannelRefresh()
	if launches != 1 {
		t.Fatalf("failed refresh relaunched %d times without user action", launches)
	}
	if got := state.channelRefreshStatus(); got.Pending || !got.Retryable {
		t.Fatalf("failed status = %#v, want pending=false retryable=true", got)
	}
	if state.pi != oldClient {
		t.Fatal("failed refresh replaced the old Pi client")
	}

	state.retryChannelRefresh()
	waitForChannelRefreshPhase(t, state, channelRefreshPhaseFailed, ui)
	if launches != 2 {
		t.Fatalf("retry launched %d times, want 2", launches)
	}
}

func TestChannelRefreshRejectsEmptyRestoredConversation(t *testing.T) {
	state, oldClient := newChannelRefreshTestState()
	ui := installChannelRefreshTestUI(state)
	oldChat := append([]chatMessage(nil), state.chat...)
	newClient := newChannelRefreshFakeClient()
	newClient.messages = nil
	state.channelRefresh.launch = func(_ string, _ string, _ func(piEvent)) (channelRefreshClient, error) {
		return newClient, nil
	}

	state.markChannelRefreshPending()
	waitForChannelRefreshPhase(t, state, channelRefreshPhaseFailed, ui)
	if got := state.channelRefreshStatus(); got.Failure != channelRefreshFailureTranscript ||
		got.Pending || !got.Retryable {
		t.Fatalf("empty transcript status = %#v", got)
	}
	if state.pi != oldClient {
		t.Fatal("empty transcript replaced the old Pi client")
	}
	if !newClient.closed {
		t.Fatal("empty transcript candidate was not closed")
	}
	if len(state.chat) != len(oldChat) || state.chat[0].blocks[0].text != "hello" {
		t.Fatalf("empty transcript replaced visible chat: %#v", state.chat)
	}
}

func TestChannelRefreshSecondSaveQueuesAnotherAttempt(t *testing.T) {
	state, _ := newChannelRefreshTestState()
	ui := installChannelRefreshTestUI(state)
	release := make(chan struct{})
	var mu sync.Mutex
	launches := 0
	state.channelRefresh.launch = func(_ string, _ string, _ func(piEvent)) (channelRefreshClient, error) {
		mu.Lock()
		launches++
		call := launches
		mu.Unlock()
		if call == 1 {
			<-release
		}
		return newChannelRefreshFakeClient(), nil
	}

	state.markChannelRefreshPending()
	deadline := time.Now().Add(time.Second)
	for state.channelRefreshStatus().Phase != channelRefreshPhaseRunning && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	state.markChannelRefreshPending()
	close(release)
	waitForChannelRefreshPhase(t, state, channelRefreshPhaseSucceeded, ui)
	mu.Lock()
	got := launches
	mu.Unlock()
	if got != 2 {
		t.Fatalf("refresh saves launched %d attempts, want 2", got)
	}
}

func TestChannelRefreshSaveImmediatelyAfterSwapIsStable(t *testing.T) {
	state, _ := newChannelRefreshTestState()
	ui := installChannelRefreshTestUI(state)
	launches := 0
	state.channelRefresh.launch = func(_ string, _ string, _ func(piEvent)) (channelRefreshClient, error) {
		launches++
		return newChannelRefreshFakeClient(), nil
	}

	state.markChannelRefreshPending()
	waitForChannelRefreshPhase(t, state, channelRefreshPhaseSucceeded, ui)
	state.pumpChannelRefresh()
	if launches != 1 {
		t.Fatalf("stable saved refresh launched %d Pi process(es), want 1", launches)
	}
	if got := state.channelRefreshStatus(); got.Phase != channelRefreshPhaseSucceeded || got.Pending {
		t.Fatalf("stable saved refresh status = %#v", got)
	}
}

func TestChannelRefreshIgnoresPreCommitAndOldClientEvents(t *testing.T) {
	state, oldClient := newChannelRefreshTestState()
	ui := installChannelRefreshTestUI(state)
	oldChat := append([]chatMessage(nil), state.chat...)
	newClient := newChannelRefreshFakeClient()
	newRaw := newClient.rawClient()
	state.channelRefresh.launch = func(_ string, _ string, events func(piEvent)) (channelRefreshClient, error) {
		events(piEvent{
			Kind:   piEventUserMessage,
			Source: newRaw,
			Text:   "pre-commit event",
		})
		return newClient, nil
	}

	state.markChannelRefreshPending()
	waitForChannelRefreshPhase(t, state, channelRefreshPhaseSucceeded, ui)
	if len(state.chat) != len(oldChat)+1 ||
		state.chat[len(state.chat)-1].blocks[0].text != "restored reply" {
		t.Fatalf("pre-commit event entered the restored transcript: %#v", state.chat)
	}

	beforeOldEvent := len(state.chat)
	state.handlePiEvent(piEvent{
		Kind:   piEventUserMessage,
		Source: oldClient,
		Text:   "old client event",
	}, state.piEpoch)
	if len(state.chat) != beforeOldEvent {
		t.Fatalf("old client event changed the session transcript: %#v", state.chat)
	}
}

func TestChannelRefreshRestoresTupleAndAppliesPostCommitEvent(t *testing.T) {
	state, _ := newChannelRefreshTestState()
	ui := installChannelRefreshTestUI(state)
	state.modelTuples["session-1"] = map[string]modelTuple{
		"deepseek/fast": {VariantID: "deepseek-chat", Thinking: "high"},
	}
	newClient := newChannelRefreshFakeClient()
	newRaw := newClient.rawClient()
	state.channelRefresh.launch = func(_ string, _ string, events func(piEvent)) (channelRefreshClient, error) {
		newClient.events = func(event piEvent) {
			events(event)
		}
		return newClient, nil
	}

	state.markChannelRefreshPending()
	waitForChannelRefreshPhase(t, state, channelRefreshPhaseSucceeded, ui)
	if got := state.modelTuples["session-1"]["deepseek/fast"]; got.VariantID != "deepseek-chat" || got.Thinking != "high" {
		t.Fatalf("restored tuple memory = %#v", got)
	}

	newClient.events(piEvent{
		Kind:   piEventUserMessage,
		Source: newRaw,
		Text:   "post commit event",
	})
	ui.flush()
	if len(state.chat) < 2 || len(state.chat[len(state.chat)-2].blocks) == 0 ||
		state.chat[len(state.chat)-2].blocks[0].text != "post commit event" {
		t.Fatalf("post-commit event was not applied: %#v", state.chat)
	}
}

func TestChannelRefreshEmptyStatusIsIdle(t *testing.T) {
	state, _ := newChannelRefreshTestState()
	state.channelRefresh = channelRefreshCoordinator{}
	if got := state.channelRefreshStatus(); got.Phase != channelRefreshPhaseIdle {
		t.Fatalf("zero refresh status = %#v, want idle", got)
	}
}

type channelRefreshTestWriteCloser struct{}

func (channelRefreshTestWriteCloser) Write(p []byte) (int, error) {
	return len(p), nil
}

func (channelRefreshTestWriteCloser) Close() error {
	return nil
}

func newChannelRefreshTestState() (*appState, *piClient) {
	waitDone := make(chan struct{})
	close(waitDone)
	oldClient := &piClient{
		stdin:       channelRefreshTestWriteCloser{},
		pending:     make(map[string]chan piRPCResponse),
		waitDone:    waitDone,
		streamParts: make(map[int]string),
	}
	state := &appState{
		pi:               oldClient,
		piConn:           piConnected,
		piPath:           "/tmp/pi",
		piCwd:            "/tmp",
		piVersion:        "1.4.0",
		piSessionFile:    "/tmp/byop-refresh-session.jsonl",
		piSessionID:      "session-1",
		piModelProvider:  "deepseek",
		piModelID:        "deepseek-chat",
		piModelName:      "DeepSeek Chat",
		piThinkingLevel:  "high",
		piModels:         []piModel{{Provider: "deepseek", ID: "deepseek-chat"}},
		piThinkingLevels: []string{"off", "high"},
		modelTuples:      map[string]map[string]modelTuple{},
		composerDraft:    "draft",
		chat:             []chatMessage{{role: chatUser, blocks: []chatBlock{{kind: chatBlockText, text: "hello"}}}},
		run:              runIdle,
	}
	return state, oldClient
}

func channelRefreshTestStateValue() *piSessionState {
	return &piSessionState{
		SessionID:     "session-1",
		SessionFile:   "/tmp/byop-refresh-session.jsonl",
		ThinkingLevel: "high",
		Model: struct {
			Provider      string   `json:"provider"`
			ID            string   `json:"id"`
			Name          string   `json:"name"`
			Reasoning     bool     `json:"reasoning"`
			ContextWindow int      `json:"contextWindow"`
			Input         []string `json:"input"`
		}{Provider: "deepseek", ID: "deepseek-chat", Name: "DeepSeek Chat"},
	}
}

func newChannelRefreshFakeClient() *channelRefreshFakeClient {
	return &channelRefreshFakeClient{
		initial:      *channelRefreshTestStateValue(),
		stateResults: []piSessionState{*channelRefreshTestStateValue()},
		models:       []piModel{{Provider: "deepseek", ID: "deepseek-chat"}},
		levels:       []string{"off", "high"},
		messages:     channelRefreshTestMessages(),
	}
}

func channelRefreshTestMessages() []json.RawMessage {
	return []json.RawMessage{
		json.RawMessage(`{"role":"user","content":"hello","timestamp":1}`),
		json.RawMessage(`{"role":"assistant","content":[{"type":"text","text":"restored reply"}],"stopReason":"stop","timestamp":2}`),
	}
}

func waitForChannelRefreshPhase(t *testing.T, state *appState, phase ChannelRefreshPhase,
	ui *channelRefreshUIQueue,
) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		ui.flush()
		if state.channelRefreshStatus().Phase == phase {
			return
		}
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	ui.flush()
	if got := state.channelRefreshStatus(); got.Phase != phase {
		t.Fatalf("refresh status = %#v, want phase %q", got, phase)
	}
}
