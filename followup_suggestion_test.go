package main

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

func TestSanitizeFollowUpSuggestion(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "first nonempty line and decoration",
			input: "\n  - \"Suggestion: Can you show the test output?\"\nAnother line",
			want:  "Can you show the test output?",
		},
		{
			name:  "ordered bullet and whitespace",
			input: "1)   Review   the\tfailure",
			want:  "Review the failure",
		},
		{
			name:  "cap runes",
			input: strings.Repeat("界", followUpOutputRunes+10),
			want:  strings.Repeat("界", followUpOutputRunes),
		},
		{
			name:  "empty",
			input: " \n\t",
			want:  "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sanitizeFollowUpSuggestion(test.input); got != test.want {
				t.Fatalf("suggestion = %q, want %q", got, test.want)
			}
		})
	}
}

func TestBuildFollowUpInputTruncatesPromptHeadAndReplyTail(t *testing.T) {
	prompt := strings.Repeat("p", followUpContextRunes+7)
	reply := strings.Repeat("r", followUpContextRunes+9)
	input := buildFollowUpInput(prompt, reply)

	promptContext, _, ok := strings.Cut(input, "\n\nAssistant reply:\n")
	if !ok {
		t.Fatal("input is missing the assistant reply label")
	}
	if got := strings.TrimPrefix(promptContext, "User message:\n"); len([]rune(got)) != followUpContextRunes {
		t.Fatalf("prompt has %d runes, want %d", len([]rune(got)), followUpContextRunes)
	}
	if !strings.Contains(input, "\n\nAssistant reply:\n"+strings.Repeat("r", followUpContextRunes)) {
		t.Fatal("reply was not truncated from the tail")
	}
	if strings.Contains(input, strings.Repeat("r", followUpContextRunes+1)) {
		t.Fatal("reply exceeded its rune limit")
	}
}

func TestBuildFollowUpInputSeparatesInstruction(t *testing.T) {
	input := buildFollowUpInput("Question", "Answer")
	want := "\n\nTask:\n" + followUpInstruction
	if !strings.Contains(input, want) {
		t.Fatalf("input is missing separated task instruction: %q", input)
	}
}

func TestFollowUpPiArgs(t *testing.T) {
	withModel := followUpPiArgs("openai", "team/model")
	wantPrefix := []string{
		"--print", "--no-session", "--no-tools", "--no-extensions",
		"--no-skills", "--no-context-files", "--no-prompt-templates",
		"--no-mcp", "--thinking", "off", "--model", "openai/team/model",
	}
	if got := strings.Join(withModel[:len(wantPrefix)], " "); got != strings.Join(wantPrefix, " ") {
		t.Fatalf("args with model prefix = %q, want %q", got, strings.Join(wantPrefix, " "))
	}
	if len(withModel) != len(wantPrefix) {
		t.Fatalf("args with model count = %d, want %d: %#v", len(withModel), len(wantPrefix), withModel)
	}
	if slices.Contains(withModel, followUpInstruction) {
		t.Fatalf("instruction must be in stdin, not argv: %#v", withModel)
	}
	withoutModel := followUpPiArgs("", "")
	if slices.Contains(withoutModel, "--model") {
		t.Fatalf("args without model include --model: %#v", withoutModel)
	}
	wantWithoutModel := []string{
		"--print", "--no-session", "--no-tools", "--no-extensions",
		"--no-skills", "--no-context-files", "--no-prompt-templates",
		"--no-mcp", "--thinking", "off",
	}
	if got := strings.Join(withoutModel, " "); got != strings.Join(wantWithoutModel, " ") {
		t.Fatalf("args without model = %q, want %q", got, strings.Join(wantWithoutModel, " "))
	}
}

func TestFollowUpSuggestionAdoptClearAndDismiss(t *testing.T) {
	state := followUpTestState()
	state.followUpSuggestion = "Show the failing assertion."

	if !state.followUpSuggestionVisible() {
		t.Fatal("suggestion should be visible with an empty composer")
	}
	if !state.adoptFollowUpSuggestion(false) {
		t.Fatal("adopt returned false")
	}
	if state.composerDraft != state.followUpSuggestion {
		t.Fatalf("draft = %q, want adopted suggestion", state.composerDraft)
	}
	if !state.followUpAdoptionPending || state.followUpAdoptionFocus {
		t.Fatal("Tab adoption did not queue a caret update without card focus")
	}
	if state.followUpSuggestionVisible() {
		t.Fatal("suggestion remained visible with a non-empty draft")
	}

	state.composerDraft = ""
	if !state.followUpSuggestionVisible() {
		t.Fatal("clearing the adopted draft should reveal the suggestion again")
	}

	state.dismissFollowUpSuggestion()
	if state.followUpSuggestion != "" || state.followUpSuggestionVisible() {
		t.Fatal("dismiss did not remove the suggestion")
	}
}

func TestFollowUpComposerInput(t *testing.T) {
	state := followUpTestState()
	state.followUpSuggestion = "Try the focused test."

	if !state.handleFollowUpComposerInput(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyTab}, false) {
		t.Fatal("Tab was not consumed")
	}
	if state.composerDraft != state.followUpSuggestion {
		t.Fatalf("Tab draft = %q", state.composerDraft)
	}

	state.composerDraft = ""
	if !state.handleFollowUpComposerInput(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyEnter}, false) {
		t.Fatal("plain Enter was not preserved")
	}
	if state.composerDraft != "" {
		t.Fatal("plain Enter changed the draft")
	}
	if state.handleFollowUpComposerInput(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyEnter, Mods: ui.Shift}, false) {
		t.Fatal("Shift+Enter should pass through")
	}
	if state.handleFollowUpComposerInput(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyTab}, true) {
		t.Fatal("Tab during composition should pass through")
	}
	if !state.handleFollowUpComposerInput(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyEscape}, false) {
		t.Fatal("Esc was not consumed")
	}
	if state.followUpSuggestionVisible() {
		t.Fatal("Esc did not dismiss the suggestion")
	}
}

func TestFollowUpCardAdoptionSetsTrailingCaretAndFocus(t *testing.T) {
	state := followUpTestState()
	state.followUpSuggestion = "检查 café"
	if !state.adoptFollowUpSuggestion(true) {
		t.Fatal("adopt returned false")
	}

	var start, end int
	var focused bool
	showCard := true
	pendingWhileCardVisible := false
	tester := ui.NewTester(func(c *ui.Context) {
		cardVisible := showCard
		if cardVisible {
			ui.Column(c.Key("follow-up-card")).Height(24)
		}
		ui.Column(c.Key("chat-composer")).Children(func() {
			input := ui.TextArea(c.Key("follow-up-draft"), &state.composerDraft).Label("Follow-up draft")
			if !cardVisible {
				state.applyPendingFollowUpAdoption(input)
			}
			start, end = input.TextSelection()
			focused = input.Focused()
		})
		if cardVisible {
			pendingWhileCardVisible = state.followUpAdoptionPending
			showCard = false
		}
	}, 320, 120)
	tester.Frame()

	caret := utf8.RuneCountInString(state.composerDraft)
	if !pendingWhileCardVisible {
		t.Fatal("card frame applied adoption before the composer layout settled")
	}
	if start != caret || end != caret {
		t.Fatalf("selection = %d–%d, want caret %d", start, end, caret)
	}
	if !focused {
		t.Fatal("Use did not focus the composer")
	}
	if state.followUpAdoptionPending || state.followUpAdoptionFocus {
		t.Fatal("adoption flags were not cleared after applying")
	}
}

func TestSendPromptClearsSuggestionBeforeDispatch(t *testing.T) {
	state := followUpTestState()
	state.followUpSuggestion = "Continue with the fix."
	state.composerDraft = "Send this message."
	state.pi = &piClient{
		stdin:   &followUpTestWriter{},
		pending: make(map[string]chan piRPCResponse),
	}

	state.sendPrompt()

	if state.followUpSuggestion != "" {
		t.Fatalf("suggestion after send = %q", state.followUpSuggestion)
	}
	if state.run != runSubmitting || state.composerDraft != "" {
		t.Fatalf("unexpected dispatch state: run=%q draft=%q", state.run, state.composerDraft)
	}
}

func TestSessionSwitchAndNewSessionClearSuggestion(t *testing.T) {
	state := followUpTestState()
	state.followUpSuggestion = "Check the session."

	state.newPiSession()
	if state.followUpSuggestion != "" {
		t.Fatalf("new session kept suggestion %q", state.followUpSuggestion)
	}

	state.followUpSuggestion = "Check the other session."
	state.switchPiSession(piSessionSummary{Path: "/tmp/session.jsonl", ID: "session"})
	if state.followUpSuggestion != "" {
		t.Fatalf("session switch kept suggestion %q", state.followUpSuggestion)
	}
}

func TestStaleFollowUpResultsAreDiscarded(t *testing.T) {
	state := followUpTestState()
	state.followUpGeneration = 4
	state.piSessionID = "current"
	state.piSessionFile = "/tmp/current.jsonl"

	if state.applyFollowUpSuggestionResult(3, "current", "/tmp/current.jsonl", "old", nil) {
		t.Fatal("old generation was accepted")
	}
	if state.applyFollowUpSuggestionResult(4, "other", "/tmp/current.jsonl", "other session", nil) {
		t.Fatal("other session ID was accepted")
	}
	if state.applyFollowUpSuggestionResult(4, "current", "/tmp/other.jsonl", "other file", nil) {
		t.Fatal("other session file was accepted")
	}
	if !state.applyFollowUpSuggestionResult(4, "current", "/tmp/current.jsonl", "current suggestion", nil) {
		t.Fatal("current result was rejected")
	}
	if state.followUpSuggestion != "current suggestion" {
		t.Fatalf("suggestion = %q", state.followUpSuggestion)
	}

	state.followUpGeneration++
	if !state.applyFollowUpSuggestionResult(state.followUpGeneration, "", "", "uncaptured session", nil) {
		t.Fatal("result with empty captured session was rejected")
	}

	state.followUpGeneration++
	state.compacting = true
	state.retrying = true
	state.piSessionBusy = true
	state.thinkingCommitInFlight = true
	if !state.applyFollowUpSuggestionResult(state.followUpGeneration, "current", "/tmp/current.jsonl", "deferred display", nil) {
		t.Fatal("valid result was rejected because the UI was temporarily busy")
	}
	if state.followUpSuggestion != "deferred display" || state.followUpSuggestionVisible() {
		t.Fatalf("deferred suggestion state = %q, visible=%v", state.followUpSuggestion, state.followUpSuggestionVisible())
	}
}

func TestResetFollowUpCancelsInFlightGeneration(t *testing.T) {
	state := followUpTestState()
	ctx, cancel := context.WithCancel(context.Background())
	state.followUpCancel = cancel
	state.followUpSuggestion = "Old suggestion"
	state.followUpAdoptionPending = true
	state.followUpAdoptionFocus = true
	state.resetFollowUpSuggestion()

	select {
	case <-ctx.Done():
	default:
		t.Fatal("reset did not cancel the in-flight generation")
	}
	if state.followUpCancel != nil || state.followUpSuggestion != "" ||
		state.followUpAdoptionPending || state.followUpAdoptionFocus {
		t.Fatalf("reset left stale state: %#v", state)
	}
}

func TestFollowUpPreferenceNormalization(t *testing.T) {
	preferences := defaultByopPreferences()
	if preferences.FollowUpStyle != string(followUpStyleGhost) {
		t.Fatalf("default style = %q", preferences.FollowUpStyle)
	}
	for value, want := range map[string]followUpStyle{
		" CARD ":  followUpStyleCard,
		"ghost":   followUpStyleGhost,
		"unknown": followUpStyleGhost,
		"":        followUpStyleGhost,
	} {
		preferences.FollowUpStyle = value
		normalizeByopPreferences(&preferences)
		if got := followUpStyle(preferences.FollowUpStyle); got != want {
			t.Fatalf("normalize(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestInjectableFollowUpRunnerAndSettledTrigger(t *testing.T) {
	state := followUpTestState()
	const stalePrompt = "stale active prompt"
	state.activePrompt = stalePrompt
	state.chat = []chatMessage{
		{role: chatUser, blocks: []chatBlock{{kind: chatBlockText, text: "Why did the test fail?"}}},
		{role: chatAssistant, blocks: []chatBlock{{kind: chatBlockText, text: "The assertion expected 2."}}},
	}
	var gotPath string
	var gotArgs []string
	var gotStdin string
	state.followUpRunner = func(ctx context.Context, path, cwd string, args []string, stdin string) (string, error) {
		gotPath = path
		gotArgs = append([]string(nil), args...)
		gotStdin = stdin
		return "Suggestion: Show the assertion diff.", nil
	}

	state.handlePiEvent(piEvent{Kind: piEventSettled}, 0)

	if state.followUpSuggestion != "Show the assertion diff." {
		t.Fatalf("suggestion = %q", state.followUpSuggestion)
	}
	if gotPath != "/bin/echo" {
		t.Fatalf("runner path = %q, want the connected Pi executable", gotPath)
	}
	if !slices.Contains(gotArgs, "--model") || !slices.Contains(gotArgs, "provider/model") {
		t.Fatalf("runner args missing current model: %#v", gotArgs)
	}
	if !strings.Contains(gotStdin, "Why did the test fail?") || !strings.Contains(gotStdin, "The assertion expected 2.") {
		t.Fatalf("runner stdin missing context: %q", gotStdin)
	}
	if strings.Contains(gotStdin, stalePrompt) {
		t.Fatalf("runner stdin used activePrompt instead of the last user message: %q", gotStdin)
	}
}

func TestFollowUpSkipsAbortedAndSyntheticFallbackTurns(t *testing.T) {
	for _, test := range []struct {
		name  string
		event piEvent
	}{
		{name: "aborted", event: piEvent{Kind: piEventSettled, Aborted: true}},
		{name: "no assistant text", event: piEvent{Kind: piEventSettled}},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := followUpTestState()
			state.activePrompt = "Prompt"
			state.chat = []chatMessage{
				{role: chatUser, blocks: []chatBlock{{kind: chatBlockText, text: "Prompt"}}},
				{role: chatAssistant, status: "Waiting for Pi…"},
			}
			called := false
			state.followUpRunner = func(context.Context, string, string, []string, string) (string, error) {
				called = true
				return "unexpected", nil
			}
			state.handlePiEvent(test.event, 0)
			if called {
				t.Fatal("runner was called for an ineligible turn")
			}
		})
	}
}

func TestFollowUpRunnerErrorIsDiscarded(t *testing.T) {
	state := followUpTestState()
	state.followUpGeneration = 1
	err := errors.New(strings.Repeat("failure ", 40))
	if state.applyFollowUpSuggestionResult(1, state.piSessionID, state.piSessionFile, "", err) {
		t.Fatal("error result was accepted")
	}
	if state.followUpSuggestion != "" {
		t.Fatalf("error stored suggestion %q", state.followUpSuggestion)
	}
}

func followUpTestState() *appState {
	return &appState{
		piConn:          piConnected,
		piPath:          "/bin/echo",
		piCwd:           "/tmp",
		piModelProvider: "provider",
		piModelID:       "model",
		piSessionID:     "session",
		piSessionFile:   "/tmp/session.jsonl",
		run:             runCompleted,
		preferences:     byopPreferences{FollowUpStyle: string(followUpStyleGhost)},
	}
}

type followUpTestWriter struct {
	bytes.Buffer
}

func (*followUpTestWriter) Close() error {
	return nil
}
