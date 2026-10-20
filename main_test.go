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
