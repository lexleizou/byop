package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
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

	if state.compactionResult != "Context compacted · 12.0k → 2.4k tokens" {
		t.Fatalf("unexpected compaction label: %q", state.compactionResult)
	}
	if state.extensionError != "bad tool output" || state.extensionPath != "/tmp/demo-extension.ts" {
		t.Fatalf("extension error was not kept separate: %#v", state)
	}
	if state.runtimeDiagnosticLabel() != "ATTENTION" ||
		!strings.Contains(state.runtimeDiagnosticDetail(), "Extension error · demo-extension.ts · tool_call") {
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
