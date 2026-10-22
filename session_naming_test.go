package main

import (
	"context"
	"strings"
	"testing"
)

type fakeSessionNameRPC struct {
	state           piSessionState
	setNameCalls    int
	lastWrittenName string
}

func (f *fakeSessionNameRPC) GetState(context.Context) (*piSessionState, error) {
	state := f.state
	return &state, nil
}

func (f *fakeSessionNameRPC) SetSessionName(_ context.Context, name string) error {
	f.setNameCalls++
	f.lastWrittenName = name
	return nil
}

func TestLocalTemporarySessionTitle(t *testing.T) {
	if got := localTemporarySessionTitle("  First\n\tmessage  "); got != "First message" {
		t.Fatalf("unexpected single-line title: %q", got)
	}
	got := localTemporarySessionTitle(strings.Repeat("界", localSessionTitleLimit+1))
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("long title does not end in an ellipsis: %q", got)
	}
}

func TestSanitizeSessionName(t *testing.T) {
	if got := sanitizeSessionName("**修复输入框截断问题。**"); got != "修复输入框截断问题" {
		t.Fatalf("unexpected Chinese title: %q", got)
	}
	if got := sanitizeSessionName("`Fix the composer cutoff.`\nignored"); got != "Fix the composer cutoff" {
		t.Fatalf("unexpected English title: %q", got)
	}
	if got := sanitizeSessionName("one two three four five six seven eight nine"); got != "one two three four five six seven eight" {
		t.Fatalf("English title was not limited to eight words: %q", got)
	}
	if got := sanitizeSessionName("标题：《修复输入框（P10b）》"); got != "修复输入框（P10b）" {
		t.Fatalf("unexpected prefixed and enclosed title: %q", got)
	}
	if got := sanitizeSessionName(`Title: "Preserve parentheses)"。`); got != "Preserve parentheses)" {
		t.Fatalf("sentence punctuation stripping removed a closing parenthesis: %q", got)
	}
}

func TestNamingTurnsFromChatUsesRecentMessages(t *testing.T) {
	chat := make([]chatMessage, 0, 10)
	for i := 0; i < 5; i++ {
		chat = append(chat,
			chatMessage{role: chatUser, blocks: []chatBlock{{kind: chatBlockText, text: "user " + string(rune('a'+i))}}},
			chatMessage{role: chatAssistant, blocks: []chatBlock{{kind: chatBlockText, text: "assistant " + string(rune('a'+i))}}},
		)
	}
	turns := namingTurnsFromChat(chat, namingContextLimit)
	if len(turns) != namingContextLimit {
		t.Fatalf("got %d turns, want %d", len(turns), namingContextLimit)
	}
	if turns[0].Text != "user c" || turns[len(turns)-1].Text != "assistant e" {
		t.Fatalf("unexpected recent turns: %#v", turns)
	}
	if prompt := buildSessionNamingPrompt(turns); !strings.Contains(prompt, "Assistant: assistant e") {
		t.Fatalf("prompt does not contain the latest reply: %q", prompt)
	}
	longPrompt := buildSessionNamingPrompt([]namingTurn{{
		Role: "Assistant",
		Text: strings.Repeat("界", namingTranscriptLimit+10),
	}})
	if strings.Count(longPrompt, "界") != namingTranscriptLimit {
		t.Fatalf("naming context was not truncated to %d runes", namingTranscriptLimit)
	}
}

func TestBeginPiSessionOperationInvalidatesPendingSessionNaming(t *testing.T) {
	client := &piClient{}
	state := &appState{
		pi:                          client,
		piConn:                      piConnected,
		piEpoch:                     7,
		piSessionFile:               "old-session.jsonl",
		piSessionID:                 "old-session",
		sessionNamingGen:            11,
		sessionNamingInProgress:     true,
		sessionNamingCommitInFlight: false,
	}
	pendingGen := state.sessionNamingGen

	if _, _, _, ok := state.beginPiSessionOperation("switch session"); !ok {
		t.Fatal("session operation was unexpectedly rejected")
	}
	if state.sessionNamingGen != pendingGen+1 {
		t.Fatalf("naming generation = %d, want %d", state.sessionNamingGen, pendingGen+1)
	}
	if state.sessionNamingInProgress {
		t.Fatal("pending naming stayed active after the session operation began")
	}
	if state.sessionNamingResultCurrent(
		pendingGen, state.piEpoch, client, "old-session.jsonl", "old-session",
	) {
		t.Fatal("stale naming result remains writable after switching sessions")
	}
}

func TestInvalidateSessionNamingKeepsCommitOwnershipUntilCallback(t *testing.T) {
	state := &appState{
		sessionNamingGen:            11,
		sessionNamingInProgress:     true,
		sessionNamingCommitInFlight: true,
		sessionNamingCommitSeq:      7,
		sessionNamingCommitOwner:    sessionNameCommitAuto,
	}
	state.invalidateSessionNaming()
	if state.sessionNamingGen != 12 {
		t.Fatalf("naming generation = %d, want 12", state.sessionNamingGen)
	}
	if state.sessionNamingInProgress {
		t.Fatal("naming result incorrectly stayed in progress")
	}
	if !state.sessionNamingCommitInFlight ||
		state.sessionNamingCommitSeq != 7 ||
		state.sessionNamingCommitOwner != sessionNameCommitAuto {
		t.Fatalf("invalidation cleared an in-flight commit: %#v", state)
	}
	state.finishSessionNameCommit(7, sessionNameCommitAuto)
	if state.sessionNamingCommitInFlight || state.sessionNamingCommitOwner != sessionNameCommitNone {
		t.Fatalf("matching commit callback did not finish ownership: %#v", state)
	}
}

func TestStaleCommitCallbackCannotClearNewCommitState(t *testing.T) {
	state := &appState{
		sessionNamingGen:            13,
		sessionNamingCommitSeq:      8,
		sessionNamingCommitOwner:    sessionNameCommitAuto,
		sessionNamingCommitInFlight: true,
	}
	state.finishSessionNameCommit(7, sessionNameCommitAuto)
	if !state.sessionNamingCommitInFlight ||
		state.sessionNamingCommitSeq != 8 ||
		state.sessionNamingCommitOwner != sessionNameCommitAuto {
		t.Fatalf("stale callback cleared the current commit: %#v", state)
	}
	state.finishSessionNameCommit(8, sessionNameCommitAuto)
	if state.sessionNamingCommitInFlight || state.sessionNamingCommitOwner != sessionNameCommitNone {
		t.Fatalf("current callback did not finish commit ownership: %#v", state)
	}
}

func TestPendingSessionNamingDoesNotWriteAfterSessionSwitch(t *testing.T) {
	client := &piClient{}
	state := &appState{
		pi:                      client,
		piConn:                  piConnected,
		piEpoch:                 7,
		piSessionFile:           "old-session.jsonl",
		piSessionID:             "old-session",
		sessionNamingGen:        11,
		sessionNamingInProgress: true,
	}
	pendingGen := state.sessionNamingGen
	pendingEpoch := state.piEpoch

	if _, _, _, ok := state.beginPiSessionOperation("switch session"); !ok {
		t.Fatal("session operation was unexpectedly rejected")
	}
	rpc := &fakeSessionNameRPC{state: piSessionState{
		SessionID:   "new-session",
		SessionFile: "new-session.jsonl",
	}}
	_, _, err := writeSessionName(
		rpc, "Old session title", "old-session.jsonl", "old-session", false,
	)
	if err == nil {
		t.Fatal("stale naming request was accepted after switching sessions")
	}
	if pendingGen == state.sessionNamingGen {
		t.Fatal("session switch did not advance the naming generation")
	}
	if pendingEpoch != state.piEpoch {
		t.Fatal("session switch unexpectedly changed the Pi connection epoch")
	}
	if rpc.setNameCalls != 0 || rpc.lastWrittenName != "" {
		t.Fatalf("stale naming request wrote %q %d time(s)", rpc.lastWrittenName, rpc.setNameCalls)
	}
}

func TestSessionNamingEntryPointsReturnWhilePiSessionBusy(t *testing.T) {
	state := &appState{
		pi:                   &piClient{},
		piConn:               piConnected,
		piSessionID:          "session",
		piSessionBusy:        true,
		sessionRenameEditing: true,
		sessionRenameDraft:   "Manual name",
		sessionNamingGen:     19,
		chat:                 []chatMessage{{role: chatUser, blocks: []chatBlock{{kind: chatBlockText, text: "prompt"}}}},
	}
	state.startContextSessionNaming()
	if state.sessionNamingGen != 19 || state.sessionNamingInProgress {
		t.Fatalf("context naming started while Pi session was busy: %#v", state)
	}
	if state.saveSessionRename() {
		t.Fatal("manual rename was saved while Pi session was busy")
	}
	if !state.sessionRenameEditing {
		t.Fatal("busy-session rename was cancelled despite being rejected")
	}
	state.startSessionNaming([]namingTurn{{Role: "User", Text: "prompt"}}, true)
	if state.sessionNamingGen != 19 || state.sessionNamingInProgress {
		t.Fatalf("auto naming started while Pi session was busy: %#v", state)
	}
}

func TestSessionNameCommitUIEntryRejectsBusyBeforeStartingWorker(t *testing.T) {
	state := &appState{
		piSessionBusy:               true,
		sessionNamingCommitSeq:      12,
		sessionNamingCommitOwner:    sessionNameCommitAuto,
		sessionNamingCommitInFlight: true,
		sessionNamingInProgress:     true,
	}
	started := state.startSessionNameCommit(nil, 3, 7, sessionNameCommitAuto, 12,
		"Name", "session.jsonl", "session", false, false)
	if started {
		t.Fatal("session-name worker started while the Pi session was busy")
	}
	if state.sessionNamingCommitInFlight || state.sessionNamingInProgress ||
		state.sessionNamingCommitOwner != sessionNameCommitNone {
		t.Fatalf("busy UI entry did not finish commit ownership: %#v", state)
	}
}

func TestManualSessionRenameDisabledDuringAutoNameCommit(t *testing.T) {
	state := &appState{
		pi:                          &piClient{},
		piConn:                      piConnected,
		piSessionID:                 "session",
		sessionNamingCommitInFlight: true,
	}
	state.beginSessionRename()
	if state.sessionRenameEditing {
		t.Fatal("manual rename opened while an automatic name commit was in flight")
	}
	state.sessionRenameEditing = true
	state.sessionRenameDraft = "Manual name"
	if state.saveSessionRename() {
		t.Fatal("manual rename saved while an automatic name commit was in flight")
	}
	if !state.sessionRenameEditing || state.sessionRenameDraft != "Manual name" {
		t.Fatal("rejected manual rename did not retain its draft")
	}
}

func TestAutoNamingAttemptMapIsBounded(t *testing.T) {
	state := &appState{
		piSessionFile:                "new-session.jsonl",
		piSessionID:                  "new-session",
		autoNamingAttemptedBySession: make(map[string]bool, autoNamingSessionLimit),
	}
	for i := 0; i < autoNamingSessionLimit; i++ {
		state.autoNamingAttemptedBySession[string(rune(i))] = true
	}
	state.maybeStartAutoSessionNaming("prompt", "reply")
	key := state.activePiSessionKey()
	if len(state.autoNamingAttemptedBySession) != 1 ||
		!state.autoNamingAttemptedBySession[key] {
		t.Fatalf("attempt map was not reset and bounded: %#v", state.autoNamingAttemptedBySession)
	}
}
