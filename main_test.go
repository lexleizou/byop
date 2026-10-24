package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestToolCallProjectionLifecycle(t *testing.T) {
	state := projectionTestState()

	state.handlePiEvent(piEvent{
		Kind:       piEventToolStart,
		ToolCallID: "call_1",
		ToolName:   "bash",
		ToolArgs:   json.RawMessage(`{"command":"printf BYOP_OK"}`),
	}, 0)
	tool := state.latestToolCall()
	if tool == nil || tool.status != toolCallRunning || tool.arguments != "printf BYOP_OK" {
		t.Fatalf("unexpected tool start projection: %#v", tool)
	}

	state.handlePiEvent(piEvent{
		Kind:        piEventToolUpdate,
		ToolCallID:  "call_1",
		ToolName:    "bash",
		ToolPartial: json.RawMessage(`{"content":[{"type":"text","text":"partial"}]}`),
	}, 0)
	if tool = state.latestToolCall(); tool == nil || tool.partial != "partial" || tool.status != toolCallRunning {
		t.Fatalf("unexpected tool update projection: %#v", tool)
	}

	state.handlePiEvent(piEvent{
		Kind:           piEventToolEnd,
		ToolCallID:     "call_1",
		ToolName:       "bash",
		ToolResult:     json.RawMessage(`{"content":[{"type":"text","text":"BYOP_OK"}]}`),
		ToolIsError:    false,
		ToolDurationMS: 42,
	}, 0)
	if tool = state.latestToolCall(); tool == nil || tool.status != toolCallCompleted ||
		tool.result != "BYOP_OK" || tool.durationMS != 42 {
		t.Fatalf("unexpected tool end projection: %#v", tool)
	}
}

func TestAssistantTimelinePreservesBlockOrder(t *testing.T) {
	state := projectionTestState()

	state.setAssistantText("text:one", "Streaming")
	state.startToolCall(piEvent{
		ToolCallID: "call_a",
		ToolName:   "bash",
		ToolArgs:   json.RawMessage(`{"command":"printf ONE"}`),
	})
	state.endToolCall(piEvent{
		ToolCallID:     "call_a",
		ToolName:       "bash",
		ToolResult:     json.RawMessage(`{"content":[{"type":"text","text":"ONE"}]}`),
		ToolDurationMS: 12,
	})
	state.setAssistantText("text:two", "Streaming")
	state.startToolCall(piEvent{
		ToolCallID: "call_b",
		ToolName:   "bash",
		ToolArgs:   json.RawMessage(`{"command":"printf TWO"}`),
	})
	state.setAssistantText("text:three", "Received")

	assistant := state.chat[len(state.chat)-1]
	if len(assistant.blocks) != 5 {
		t.Fatalf("timeline has %d blocks, want 5: %#v", len(assistant.blocks), assistant.blocks)
	}

	gotKinds := make([]string, 0, len(assistant.blocks))
	for i := range assistant.blocks {
		block := &assistant.blocks[i]
		switch block.kind {
		case chatBlockText:
			gotKinds = append(gotKinds, "text:"+block.text)
		case chatBlockTool:
			gotKinds = append(gotKinds, "tool:"+block.tool.id)
		}
	}
	got := strings.Join(gotKinds, ",")
	want := "text:text:one,tool:call_a,text:text:two,tool:call_b,text:text:three"
	if got != want {
		t.Fatalf("timeline order = %q, want %q", got, want)
	}

	firstTool := &assistant.blocks[1].tool
	if firstTool.status != toolCallCompleted || firstTool.expanded || firstTool.result != "ONE" {
		t.Fatalf("completed tool should be collapsed in place: %#v", firstTool)
	}
	runningTool := &assistant.blocks[3].tool
	if runningTool.status != toolCallRunning || !runningTool.expanded {
		t.Fatalf("running tool should stay expanded: %#v", runningTool)
	}
	if latest := state.latestToolCall(); latest == nil || latest.id != "call_b" {
		t.Fatalf("latest tool = %#v, want call_b", latest)
	}
}

func TestStopCancelsRunningTool(t *testing.T) {
	state := projectionTestState()
	state.startToolCall(piEvent{ToolCallID: "call_stop", ToolName: "bash"})

	state.handlePiEvent(piEvent{Kind: piEventSettled, Aborted: true}, 0)
	tool := state.latestToolCall()
	if tool == nil || tool.status != toolCallCancelled {
		t.Fatalf("aborted settlement did not cancel the running tool: %#v", tool)
	}
	if state.run != runStopped {
		t.Fatalf("run = %q, want %q", state.run, runStopped)
	}
}

func TestProcessExitInterruptsRunningTool(t *testing.T) {
	state := projectionTestState()
	state.pi = &piClient{}
	state.startToolCall(piEvent{ToolCallID: "call_exit", ToolName: "bash"})

	state.handlePiEvent(piEvent{Kind: piEventProcessExit, Error: "exit"}, 0)
	tool := state.latestToolCall()
	if tool == nil || tool.status != toolCallInterrupted {
		t.Fatalf("process exit did not interrupt the running tool: %#v", tool)
	}
	if state.run != runInterrupted {
		t.Fatalf("run = %q, want %q", state.run, runInterrupted)
	}
}

func TestClassifyPiConnectionError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "missing path", err: os.ErrNotExist, want: piErrorCodeNotFound},
		{name: "missing executable", err: exec.ErrNotFound, want: piErrorCodeNotFound},
		{name: "permission", err: os.ErrPermission, want: piErrorCodeNotExecutable},
		{name: "exec format", err: syscall.ENOEXEC, want: piErrorCodeNotExecutable},
		{name: "deadline", err: context.DeadlineExceeded, want: piErrorCodeHandshakeTimeout},
		{name: "process exited", err: fmt.Errorf("handshake: %w", errPiProcessExited), want: piErrorCodeExited},
		{name: "unknown", err: errors.New("socket closed"), want: piErrorCodeUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyPiConnectionError(test.err); got != test.want {
				t.Fatalf("classifyPiConnectionError(%v) = %q, want %q", test.err, got, test.want)
			}
		})
	}
}

func TestClearPiConnectingNoticeLeavesOtherNotices(t *testing.T) {
	state := &appState{notice: text("noticePiConnecting"), noticeActionLabel: "Retry"}
	state.clearPiConnectingNotice()
	if state.notice != "" || state.noticeActionLabel != "" {
		t.Fatalf("connecting notice was not cleared: %#v", state)
	}

	state.notice = text("noticePiConnected")
	state.noticeActionLabel = "Keep"
	state.clearPiConnectingNotice()
	if state.notice != text("noticePiConnected") || state.noticeActionLabel != "Keep" {
		t.Fatalf("ordinary notice was cleared: notice=%q action=%q", state.notice, state.noticeActionLabel)
	}
}

func TestResolvePiPathDirectoryIsNotExecutable(t *testing.T) {
	t.Setenv("BYOP_PI_PATH", t.TempDir())
	_, err := resolvePiPath(context.Background())
	if err == nil {
		t.Fatal("resolvePiPath accepted a directory")
	}
	if got := classifyPiConnectionError(err); got != piErrorCodeNotExecutable {
		t.Fatalf("directory classification = %q, want %q: %v", got, piErrorCodeNotExecutable, err)
	}
}

func TestResolvePiPathUsesCommonInstallDirectories(t *testing.T) {
	// Tests in this group swap package variables and must not run in parallel.
	t.Setenv("BYOP_PI_PATH", "")
	t.Setenv("PATH", t.TempDir())
	binDir := t.TempDir()
	piPath := filepath.Join(binDir, "pi")
	if err := os.WriteFile(piPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	originalDirs := piCommonInstallDirectories
	piCommonInstallDirectories = func() []string { return []string{binDir} }
	t.Cleanup(func() {
		piCommonInstallDirectories = originalDirs
	})

	got, err := resolvePiPathWithLoginPath(context.Background(), func(context.Context) string { return "" })
	if err != nil {
		t.Fatalf("resolvePiPath() error = %v", err)
	}
	if got != piPath {
		t.Fatalf("resolvePiPath() = %q, want %q", got, piPath)
	}
}

func TestPiProcessEnvAddsPiRuntimeDirectory(t *testing.T) {
	// Tests in this group swap package variables and must not run in parallel.
	piDir := t.TempDir()
	piPath := filepath.Join(piDir, "pi")
	env := piProcessEnvWithLoginPath(piPath, []string{"HOME=/tmp/home", "PATH=/usr/bin:/bin"},
		func() []string { return nil }, func(context.Context) string { return "/usr/bin:/bin" })
	var got string
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "PATH="); ok {
			got = value
			break
		}
	}
	if !strings.HasPrefix(got, piDir+string(os.PathListSeparator)) {
		t.Fatalf("Pi runtime directory missing from child PATH: %q", got)
	}
}

func TestPiProcessEnvPathOrderAndDedup(t *testing.T) {
	piDir := t.TempDir()
	piPath := filepath.Join(piDir, "pi")
	env := piProcessEnvWithLoginPath(piPath,
		[]string{"HOME=/tmp/home", "PATH=/inherited/a:/shared:/inherited/b"},
		func() []string { return []string{"/shared", "/common", "/login"} },
		func(context.Context) string { return "/inherited/b:/login:/login/duplicate" })

	var got string
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "PATH="); ok {
			got = value
			break
		}
	}
	want := strings.Join([]string{
		piDir,
		"/inherited/a",
		"/shared",
		"/inherited/b",
		"/login",
		"/login/duplicate",
		"/common",
	}, string(os.PathListSeparator))
	if got != want {
		t.Fatalf("PATH = %q, want %q", got, want)
	}
}

func TestCachedLoginShellPATHRunsOnce(t *testing.T) {
	// Tests in this group swap package variables and must not run in parallel.
	originalLoginPath := piLoginShellPATH
	originalSync := piLoginShellPATHSync
	calls := 0
	piLoginShellPATH = func() string {
		calls++
		return "/login/one:/login/two"
	}
	piLoginShellPATHSync = &loginShellPATHState{}
	t.Cleanup(func() {
		piLoginShellPATH = originalLoginPath
		piLoginShellPATHSync = originalSync
	})

	first := cachedPiLoginShellPATH(context.Background())
	second := cachedPiLoginShellPATH(context.Background())
	if first != "/login/one:/login/two" || second != first {
		t.Fatalf("cached PATH = %q, %q", first, second)
	}
	if calls != 1 {
		t.Fatalf("login shell probe called %d times, want 1", calls)
	}
}

func TestCancelledLoginShellProbeDoesNotPoisonCache(t *testing.T) {
	// Tests in this group swap package variables and must not run in parallel.
	originalLoginPath := piLoginShellPATH
	originalSync := piLoginShellPATHSync
	release := make(chan struct{})
	calls := 0
	piLoginShellPATH = func() string {
		calls++
		<-release
		return "/login/recovered"
	}
	piLoginShellPATHSync = &loginShellPATHState{}
	t.Cleanup(func() {
		piLoginShellPATH = originalLoginPath
		piLoginShellPATHSync = originalSync
	})

	firstCtx, firstCancel := context.WithCancel(context.Background())
	firstCancel()
	if got := cachedPiLoginShellPATH(firstCtx); got != "" {
		t.Fatalf("cancelled call returned %q", got)
	}

	secondCtx, secondCancel := context.WithTimeout(context.Background(), time.Second)
	defer secondCancel()
	secondDone := make(chan string, 1)
	go func() {
		secondDone <- cachedPiLoginShellPATH(secondCtx)
	}()
	close(release)
	if got := <-secondDone; got != "/login/recovered" {
		t.Fatalf("second call returned %q, want recovered PATH", got)
	}
	if calls != 1 {
		t.Fatalf("login shell probe called %d times, want 1 shared probe", calls)
	}
}

func TestReconnectInterruptsRunningTool(t *testing.T) {
	t.Setenv("BYOP_PI_PATH", filepath.Join(t.TempDir(), "missing-pi"))
	state := projectionTestState()
	state.startToolCall(piEvent{ToolCallID: "call_reconnect", ToolName: "bash"})

	state.reconnectPi()
	tool := state.latestToolCall()
	if tool == nil || tool.status != toolCallInterrupted {
		t.Fatalf("reconnect did not interrupt the running tool: %#v", tool)
	}
}

func TestToolResultFormatting(t *testing.T) {
	result := formatToolResult(json.RawMessage(
		`{"content":[{"type":"text","text":"first"},{"type":"text","text":"second"}],"details":{}}`,
	))
	if result != "first\n\nsecond" {
		t.Fatalf("result = %q", result)
	}
	long := formatToolResult(json.RawMessage(`"` + strings.Repeat("x", 1400) + `"`))
	if !strings.HasSuffix(long, "output truncated") {
		t.Fatal("long tool result was not truncated")
	}
}

func TestPiClientParsesToolEventShapes(t *testing.T) {
	events := make(chan piEvent, 3)
	client := &piClient{events: func(event piEvent) {
		events <- event
	}}
	lines := []string{
		`{"type":"tool_execution_start","toolCallId":"call_1","toolName":"bash","args":{"command":"printf BYOP_OK"}}`,
		`{"type":"tool_execution_update","toolCallId":"call_1","toolName":"bash","args":{"command":"printf BYOP_OK"},"partialResult":{"content":[{"type":"text","text":"partial"}]}}`,
		`{"type":"tool_execution_end","toolCallId":"call_1","toolName":"bash","result":{"content":[{"type":"text","text":"BYOP_OK"}]},"isError":true,"durationMs":42}`,
	}
	for _, line := range lines {
		client.handleRecord([]byte(line))
	}

	start := <-events
	update := <-events
	end := <-events
	if start.Kind != piEventToolStart || start.ToolCallID != "call_1" ||
		start.ToolName != "bash" || !strings.Contains(string(start.ToolArgs), "BYOP_OK") {
		t.Fatalf("unexpected tool start: %#v", start)
	}
	if update.Kind != piEventToolUpdate || update.ToolCallID != start.ToolCallID ||
		formatToolResult(update.ToolPartial) != "partial" {
		t.Fatalf("unexpected tool update: %#v", update)
	}
	if end.Kind != piEventToolEnd || end.ToolCallID != start.ToolCallID ||
		!end.ToolIsError || end.ToolDurationMS != 42 {
		t.Fatalf("unexpected tool end: %#v", end)
	}
}

func TestToolErrorProjection(t *testing.T) {
	state := projectionTestState()
	state.startToolCall(piEvent{ToolCallID: "call_error", ToolName: "bash"})
	state.endToolCall(piEvent{
		ToolCallID:     "call_error",
		ToolName:       "bash",
		ToolResult:     json.RawMessage(`{"content":[{"type":"text","text":"permission denied"}]}`),
		ToolIsError:    true,
		ToolDurationMS: 12,
	})

	tool := state.latestToolCall()
	if tool == nil || tool.status != toolCallFailed || tool.durationMS != 12 {
		t.Fatalf("unexpected failed tool projection: %#v", tool)
	}
}

func TestPiClientParsesRuntimeEventShapes(t *testing.T) {
	events := make(chan piEvent, 8)
	client := &piClient{events: func(event piEvent) {
		events <- event
	}}
	lines := []string{
		`{"type":"queue_update","steering":["adjust"],"followUp":["after"]}`,
		`{"type":"compaction_start","reason":"threshold"}`,
		`{"type":"compaction_end","reason":"threshold","result":{"summary":"kept","tokensBefore":12000,"estimatedTokensAfter":2400},"aborted":false,"willRetry":true}`,
		`{"type":"auto_retry_start","attempt":2,"maxAttempts":3,"delayMs":1500,"errorMessage":"provider overloaded"}`,
		`{"type":"auto_retry_end","success":false,"attempt":2,"finalError":"provider unavailable"}`,
		`{"type":"agent_end","willRetry":true}`,
		`{"type":"extension_error","extensionPath":"/tmp/demo-extension.ts","event":"tool_call","error":"bad tool output"}`,
	}
	for _, line := range lines {
		client.handleRecord([]byte(line))
	}

	queue := <-events
	if queue.Kind != piEventQueueUpdate || len(queue.QueueSteering) != 1 || len(queue.QueueFollowUp) != 1 {
		t.Fatalf("unexpected queue event: %#v", queue)
	}
	compactStart := <-events
	if compactStart.Kind != piEventCompactStart || compactStart.CompactReason != "threshold" {
		t.Fatalf("unexpected compaction start: %#v", compactStart)
	}
	compactEnd := <-events
	if compactEnd.Kind != piEventCompactEnd || compactEnd.CompactBefore != 12000 ||
		compactEnd.CompactAfter != 2400 || !compactEnd.WillRetry || compactEnd.CompactSummary != "kept" {
		t.Fatalf("unexpected compaction end: %#v", compactEnd)
	}
	retryStart := <-events
	if retryStart.Kind != piEventRetryStart || retryStart.RetryAttempt != 2 ||
		retryStart.RetryMax != 3 || retryStart.RetryDelayMS != 1500 ||
		retryStart.RetryError != "provider overloaded" {
		t.Fatalf("unexpected retry start: %#v", retryStart)
	}
	retryEnd := <-events
	if retryEnd.Kind != piEventRetryEnd || retryEnd.RetrySucceeded ||
		retryEnd.RetryAttempt != 2 || retryEnd.RetryError != "provider unavailable" {
		t.Fatalf("unexpected retry end: %#v", retryEnd)
	}
	agentEnd := <-events
	if agentEnd.Kind != piEventAgentEnd || !agentEnd.WillRetry {
		t.Fatalf("unexpected agent end: %#v", agentEnd)
	}
	extensionError := <-events
	if extensionError.Kind != piEventExtensionErr ||
		extensionError.ExtensionPath != "/tmp/demo-extension.ts" ||
		extensionError.ExtensionEvent != "tool_call" ||
		extensionError.Error != "bad tool output" {
		t.Fatalf("unexpected extension error: %#v", extensionError)
	}
}

func TestRuntimeDiagnosticsProjection(t *testing.T) {
	state := projectionTestState()
	state.handlePiEvent(piEvent{
		Kind:          piEventQueueUpdate,
		QueueSteering: []string{"steer"},
		QueueFollowUp: []string{"follow"},
	}, 0)
	state.handlePiEvent(piEvent{
		Kind:         piEventRetryStart,
		RetryAttempt: 1,
		RetryMax:     3,
		RetryDelayMS: 2000,
		RetryError:   "rate limited",
	}, 0)
	if !state.retrying || state.runtimeDiagnosticLabel() != "ACTIVE" {
		t.Fatalf("retry state was not projected: %#v", state)
	}

	state.handlePiEvent(piEvent{
		Kind:           piEventCompactEnd,
		CompactReason:  "overflow",
		CompactBefore:  12000,
		CompactAfter:   2400,
		CompactSummary: "summary",
	}, 0)
	state.handlePiEvent(piEvent{
		Kind:           piEventExtensionErr,
		ExtensionPath:  "/tmp/demo-extension.ts",
		ExtensionEvent: "tool_call",
		Error:          "bad tool output",
	}, 0)

	if state.compactionResult != "Context compacted; 12.0k → 2.4k tokens" {
		t.Fatalf("unexpected compaction label: %q", state.compactionResult)
	}
	if state.extensionError != "bad tool output" || state.extensionPath != "/tmp/demo-extension.ts" {
		t.Fatalf("extension error was not kept separate: %#v", state)
	}
	if state.runtimeDiagnosticLabel() != "ATTENTION" ||
		!strings.Contains(state.runtimeDiagnosticDetail(), "Extension error; demo-extension.ts - tool_call") {
		t.Fatalf("unexpected runtime diagnostics: %s", state.runtimeDiagnosticDetail())
	}
}

func TestMessageErrorStaysOutOfAssistantBody(t *testing.T) {
	events := make(chan piEvent, 1)
	client := &piClient{events: func(event piEvent) {
		events <- event
	}}
	client.handleRecord([]byte(
		`{"type":"message_end","message":{"role":"assistant","content":[],"stopReason":"error","errorMessage":"provider unavailable"}}`,
	))
	event := <-events
	if event.Kind != piEventMessage || event.Text != "" || event.Error != "provider unavailable" {
		t.Fatalf("unexpected message error event: %#v", event)
	}

	state := projectionTestState()
	event.Source = nil
	state.handlePiEvent(event, 0)
	if len(state.currentAssistant().blocks) != 0 {
		t.Fatalf("provider error became assistant content: %#v", state.currentAssistant().blocks)
	}
	state.handlePiEvent(piEvent{Kind: piEventAgentEnd, WillRetry: true}, 0)
	if state.run != runStreaming {
		t.Fatalf("agent_end(willRetry=true) settled the run: %q", state.run)
	}
	state.handlePiEvent(piEvent{Kind: piEventSettled}, 0)
	if state.run != runFailed {
		t.Fatalf("settled error run = %q, want failed", state.run)
	}
	message := state.currentAssistant()
	if len(message.blocks) != 1 || strings.Contains(message.blocks[0].text, "provider unavailable") {
		t.Fatalf("failed run exposed provider error in the assistant body: %#v", message.blocks)
	}
}

func TestSettledOnlyAdvancesSessionGenerationForActiveRun(t *testing.T) {
	state := projectionTestState()
	state.run = runCompleted
	state.sessionGen = 7

	state.handlePiEvent(piEvent{Kind: piEventSettled}, 0)

	if state.sessionGen != 7 {
		t.Fatalf("settled idle session generation = %d, want 7", state.sessionGen)
	}
}

func TestPiSessionStateCanonicalizesSessionFileOnce(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(target, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "linked-session.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	state := &appState{}
	state.applyPiSessionState(&piSessionState{SessionFile: link})
	if want := canonicalPiPath(target); state.piSessionFile != want {
		t.Fatalf("session file = %q, want canonical path %q", state.piSessionFile, want)
	}
}

func TestThinkingCommitSequenceSurvivesChatPresentationReset(t *testing.T) {
	state := &appState{}
	state.thinkingCommitSeq = 7
	state.stopThinkingCommitTimer()
	state.chatView = chatPresentation{}

	if state.thinkingCommitSeq != 8 {
		t.Fatalf("thinking commit sequence after reset = %d, want 8", state.thinkingCommitSeq)
	}
}

func TestThinkingDragBackWhileCommitInFlightKeepsPending(t *testing.T) {
	state := &appState{
		piThinkingLevel:        "low",
		thinkingCommitInFlight: true,
		thinkingCommitLevel:    "high",
	}
	state.chatView.thinkingPending = "high"

	state.setThinkingPending("low")
	defer state.stopThinkingCommitTimer()

	if state.chatView.thinkingPending != "low" {
		t.Fatalf("pending after drag back = %q, want %q", state.chatView.thinkingPending, "low")
	}

	state.setThinkingPending("high")
	if state.chatView.thinkingPending != "" {
		t.Fatalf("pending after returning to in-flight level = %q, want empty", state.chatView.thinkingPending)
	}
}

func TestPiModelChoicesKeepSameContextAliasesAndPreserveSlashIDs(t *testing.T) {
	models := []piModel{
		{Provider: "openai", ID: "team/model-a", Name: "Model A", ContextWindow: 200_000},
		{Provider: "openai", ID: "team/model-a-2", Name: "Model A", ContextWindow: 200_000},
		{Provider: "local", ID: "model-b", ContextWindow: 1_000_000},
		{Provider: "small", ID: "model-c", ContextWindow: 0},
	}
	options, choices := buildPiModelChoices(models)
	if got, want := options[0].ID, "openai/Model A/team/model-a"; got != want {
		t.Fatalf("model option ID = %q, want %q", got, want)
	}
	if got, want := options[0].Detail, ""; got != want {
		t.Fatalf("model row detail = %q, want empty", got)
	}
	if got, want := options[0].Description, "team/model-a"; got != want {
		t.Fatalf("selected model description = %q, want %q", got, want)
	}
	if got, want := options[1].ID, "openai/Model A/team/model-a-2"; got != want {
		t.Fatalf("second alias ID = %q, want %q", got, want)
	}
	if got, want := options[1].Description, "team/model-a-2"; got != want {
		t.Fatalf("second alias description = %q, want %q", got, want)
	}
	if got, want := options[2].ID, "local/model-b"; got != want {
		t.Fatalf("local model ID = %q, want %q", got, want)
	}
	if got, want := options[2].Description, "model-b"; got != want {
		t.Fatalf("local model description = %q, want %q", got, want)
	}
	if got, want := options[2].Label, "model-b"; got != want {
		t.Fatalf("fallback model label = %q, want %q", got, want)
	}
	if len(options) != 4 {
		t.Fatalf("model rows = %d, want 4 rows", len(options))
	}
	choice, ok := choices["openai/Model A/team/model-a"]
	if !ok || choice.selected.Provider != "openai" || choice.selected.ID != "team/model-a" {
		t.Fatalf("slash model lookup = %#v, %v", choice, ok)
	}
	if got, want := piModelDisplayName("", "provider / fallback"), "provider / fallback"; got != want {
		t.Fatalf("model display fallback = %q, want %q", got, want)
	}
	if got, want := piModelDisplayName("", ""), "Pi default"; got != want {
		t.Fatalf("default model display = %q, want %q", got, want)
	}
}

func TestPiThinkingOptionsOrderLabelsAndVisibility(t *testing.T) {
	options := buildPiThinkingOptions([]string{
		"very_high", "high", "off", "minimal", "low", "xhigh", "medium", "high",
	})
	got := make([]string, 0, len(options))
	for _, option := range options {
		got = append(got, option.ID+"="+option.Label)
	}
	want := []string{
		"off=Off",
		"minimal=Minimal",
		"low=Low",
		"medium=Medium",
		"high=High",
		"xhigh=XHigh",
		"very_high=Very High",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("thinking options = %#v, want %#v", got, want)
	}
	if reasoningLevelsVisible(nil) || reasoningLevelsVisible([]string{"off"}) {
		t.Fatal("empty and off-only reasoning levels should be hidden")
	}
	if !reasoningLevelsVisible([]string{"low"}) {
		t.Fatal("a single supported reasoning level should remain visible")
	}
}

func TestThinkingCommitDecision(t *testing.T) {
	tests := []struct {
		name               string
		pending, confirmed string
		allowed            bool
		want               bool
	}{
		{name: "changed", pending: "high", confirmed: "low", allowed: true, want: true},
		{name: "same", pending: "high", confirmed: "high", allowed: true},
		{name: "empty", confirmed: "low", allowed: true},
		{name: "disabled", pending: "high", confirmed: "low", allowed: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldCommitThinking(test.pending, test.confirmed, test.allowed); got != test.want {
				t.Fatalf("shouldCommitThinking(%q, %q, %v) = %v, want %v",
					test.pending, test.confirmed, test.allowed, got, test.want)
			}
		})
	}
}

func projectionTestState() *appState {
	return &appState{
		piConn: piConnected,
		run:    runStreaming,
		chat: []chatMessage{{
			role:   chatAssistant,
			status: "Streaming",
		}},
	}
}
