package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func p29TestState() *appState {
	state := projectionTestState()
	state.resetAssistantOrderedIdentity()
	return state
}

// thinking → text → tool → thinking → final keeps the real Pi order, each
// thinking segment stays its own block, and the final merge is idempotent.
func TestP29OrderedBlocksKeepRealTimeOrder(t *testing.T) {
	state := p29TestState()

	state.handlePiEvent(piEvent{
		Kind: piEventThinking, Text: "segment one",
		PartText: "segment one", PartSet: true, ContentIndex: 0,
	}, 0)
	state.handlePiEvent(piEvent{
		Kind: piEventText, Text: "checking",
		PartText: "checking", PartSet: true, ContentIndex: 1, Delta: "checking",
	}, 0)
	state.startToolCall(piEvent{
		Kind: piEventToolStart, ToolCallID: "call_1", ToolName: "bash",
		ToolArgs: json.RawMessage(`{"command":"go test"}`),
	})
	state.endToolCall(piEvent{
		Kind: piEventToolEnd, ToolCallID: "call_1", ToolName: "bash",
		ToolResult:     json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`),
		ToolDurationMS: 42,
	})
	state.handlePiEvent(piEvent{
		Kind: piEventThinking, Text: "segment one\n\ntool worked",
		PartText: "tool worked", PartSet: true, ContentIndex: 2,
	}, 0)
	state.handlePiEvent(piEvent{
		Kind: piEventText, Text: "final answer",
		PartText: "final answer", PartSet: true, ContentIndex: 3, Delta: "final answer",
	}, 0)
	state.handlePiEvent(piEvent{
		Kind:     piEventMessage,
		Text:     "segment one\n\ntool worked\n\nfinal answer",
		Thinking: "segment one\n\ntool worked",
		OrderedContent: []piContentEntry{
			{Kind: "thinking", Text: "segment one"},
			{Kind: "text", Text: "checking"},
			{Kind: "toolCall", ToolCallID: "call_1", ToolName: "bash"},
			{Kind: "thinking", Text: "tool worked"},
			{Kind: "text", Text: "final answer"},
		},
		StopReason: "stop",
		Model:      "cursor/claude-opus-5-5@300k",
		Provider:   "magpie",
	}, 0)

	message := state.currentAssistant()
	if len(message.blocks) != 5 {
		t.Fatalf("block count = %d, want 5: %#v", len(message.blocks), describeP29Blocks(message))
	}
	want := []string{"thinking:segment one", "text:checking", "tool:call_1", "thinking:tool worked", "text:final answer"}
	if got := strings.Join(describeP29Blocks(message), ","); strings.Join(want, ",") != got {
		t.Fatalf("order = %q, want %q", got, strings.Join(want, ","))
	}
	if message.thinking != "segment one\n\ntool worked" {
		t.Fatalf("compat thinking = %q", message.thinking)
	}
	if message.model != "cursor/claude-opus-5-5@300k" || message.provider != "magpie" || message.stopReason != "stop" {
		t.Fatalf("identity = %q/%q/%q", message.model, message.provider, message.stopReason)
	}
	if message.status != "Received" {
		t.Fatalf("status = %q", message.status)
	}
	if tool, _ := state.findToolBlock(message, "call_1"); tool == nil || tool.status != toolCallCompleted || tool.result != "ok" {
		t.Fatalf("tool result lost after merge: %#v", tool)
	}
}

func describeP29Blocks(message *chatMessage) []string {
	out := make([]string, 0, len(message.blocks))
	for i := range message.blocks {
		block := &message.blocks[i]
		switch block.kind {
		case chatBlockThinking:
			out = append(out, "thinking:"+block.text)
		case chatBlockText:
			out = append(out, "text:"+block.text)
		case chatBlockTool:
			out = append(out, "tool:"+block.tool.id)
		}
	}
	return out
}

// message_end must not duplicate content the stream already delivered and
// must not concatenate separate text parts back into the last block.
func TestP29FinalMergeIsIdempotent(t *testing.T) {
	state := p29TestState()
	state.handlePiEvent(piEvent{
		Kind: piEventText, Text: "first", PartText: "first", PartSet: true, ContentIndex: 0,
	}, 0)
	state.handlePiEvent(piEvent{
		Kind: piEventText, Text: "first\n\nsecond", PartText: "second", PartSet: true, ContentIndex: 1,
	}, 0)
	state.handlePiEvent(piEvent{
		Kind: piEventMessage,
		Text: "first\n\nsecond",
		OrderedContent: []piContentEntry{
			{Kind: "text", Text: "first"},
			{Kind: "text", Text: "second"},
		},
	}, 0)
	state.handlePiEvent(piEvent{
		Kind: piEventMessage,
		Text: "first\n\nsecond",
		OrderedContent: []piContentEntry{
			{Kind: "text", Text: "first"},
			{Kind: "text", Text: "second"},
		},
	}, 0)
	message := state.currentAssistant()
	if len(message.blocks) != 2 {
		t.Fatalf("blocks = %#v", describeP29Blocks(message))
	}
	if message.blocks[0].text != "first" || message.blocks[1].text != "second" {
		t.Fatalf("texts = %q / %q", message.blocks[0].text, message.blocks[1].text)
	}
}

// A second assistant source message restarts contentIndex at 0 but must not
// merge into the first message's block of the same index.
func TestP29SecondMessageSameIndexDoesNotOverwrite(t *testing.T) {
	state := p29TestState()
	state.handlePiEvent(piEvent{
		Kind: piEventText, Text: "msg one", PartText: "msg one", PartSet: true, ContentIndex: 0,
	}, 0)
	state.handlePiEvent(piEvent{
		Kind: piEventMessage, Text: "msg one",
		OrderedContent: []piContentEntry{{Kind: "text", Text: "msg one"}},
	}, 0)
	state.handlePiEvent(piEvent{
		Kind: piEventText, Text: "msg two", PartText: "msg two", PartSet: true, ContentIndex: 0,
	}, 0)
	message := state.currentAssistant()
	if len(message.blocks) != 2 {
		t.Fatalf("blocks = %#v", describeP29Blocks(message))
	}
	if message.blocks[0].text != "msg one" || message.blocks[1].text != "msg two" {
		t.Fatalf("texts = %q / %q", message.blocks[0].text, message.blocks[1].text)
	}
	if message.blocks[0].epoch == message.blocks[1].epoch {
		t.Fatal("the second source message must start a new epoch")
	}
}

// A toolCall entry in the final content does not lose the tool result that
// the execution events delivered, and does not create a duplicate block.
func TestP29FinalMergeKeepsToolResults(t *testing.T) {
	state := p29TestState()
	state.startToolCall(piEvent{Kind: piEventToolStart, ToolCallID: "call_9", ToolName: "read"})
	state.endToolCall(piEvent{
		Kind: piEventToolEnd, ToolCallID: "call_9", ToolName: "read",
		ToolResult: json.RawMessage(`{"content":[{"type":"text","text":"file body"}]}`),
	})
	state.handlePiEvent(piEvent{
		Kind: piEventMessage,
		OrderedContent: []piContentEntry{
			{Kind: "toolCall", ToolCallID: "call_9", ToolName: "read"},
		},
	}, 0)
	message := state.currentAssistant()
	tools := 0
	for i := range message.blocks {
		if message.blocks[i].kind == chatBlockTool {
			tools++
			if message.blocks[i].tool.result != "file body" || message.blocks[i].tool.status != toolCallCompleted {
				t.Fatalf("tool state overwritten: %#v", message.blocks[i].tool)
			}
		}
	}
	if tools != 1 {
		t.Fatalf("tool block count = %d", tools)
	}
}

// Synthetic (legacy) message_end events keep the old aggregate behaviour.
func TestP29LegacyMessageEndStillWorks(t *testing.T) {
	state := p29TestState()
	state.handlePiEvent(piEvent{Kind: piEventThinking, Text: "private planning"}, 0)
	state.handlePiEvent(piEvent{Kind: piEventText, Text: "visible answer"}, 0)
	state.handlePiEvent(piEvent{
		Kind: piEventMessage, Text: "visible answer", Thinking: "private planning refined",
	}, 0)
	message := state.currentAssistant()
	if message.thinking != "private planning refined" {
		t.Fatalf("thinking = %q", message.thinking)
	}
	if got := messageText(message); got != "visible answer" {
		t.Fatalf("text = %q", got)
	}
}

// Stopped/failed turns mark status and stopReason honestly, without faking a
// final answer.
func TestP29StopAndFailureKeepHonestState(t *testing.T) {
	state := p29TestState()
	state.handlePiEvent(piEvent{
		Kind: piEventText, Text: "partial", PartText: "partial", PartSet: true, ContentIndex: 0,
	}, 0)
	state.startToolCall(piEvent{Kind: piEventToolStart, ToolCallID: "call_x", ToolName: "bash"})
	state.handlePiEvent(piEvent{Kind: piEventSettled, Aborted: true}, 0)
	message := state.currentAssistant()
	if message.status != "Stopped" || message.endedAt.IsZero() {
		t.Fatalf("stopped turn status = %q", message.status)
	}
	if message.stopReason != "" {
		t.Fatalf("stopped turn must not invent a stop reason: %q", message.stopReason)
	}
	if tool, _ := state.findToolBlock(message, "call_x"); tool == nil || tool.status != toolCallCancelled {
		t.Fatalf("running tool not cancelled: %#v", tool)
	}
	if !strings.HasSuffix(messageText(message), "partial") {
		t.Fatalf("partial text lost: %q", messageText(message))
	}

	failed := p29TestState()
	failed.handlePiEvent(piEvent{Kind: piEventText, Text: "half answer"}, 0)
	failed.handlePiEvent(piEvent{
		Kind: piEventMessage, Error: "boom", OrderedContent: []piContentEntry{{Kind: "text", Text: "half answer"}},
	}, 0)
	failed.handlePiEvent(piEvent{Kind: piEventSettled}, 0)
	if failed.currentAssistant().status != "failed" {
		t.Fatalf("failed turn status = %q", failed.currentAssistant().status)
	}
}

// Phase survives streaming and final merge; live rows get the current model,
// and the authoritative message_end model wins.
func TestP29PhaseAndModelIdentity(t *testing.T) {
	state := p29TestState()
	state.piModelID = "local-litellm/GLM5.3-Flash-Maya-M"
	state.piModelProvider = "magpie"
	state.handlePiEvent(piEvent{
		Kind: piEventText, Text: "working on it", PartText: "working on it", PartSet: true, ContentIndex: 0,
	}, 0)
	state.handlePiEvent(piEvent{
		Kind: piEventMessage,
		OrderedContent: []piContentEntry{
			{Kind: "text", Text: "working on it", Phase: "commentary"},
			{Kind: "text", Text: "done", Phase: "final_answer"},
		},
		StopReason: "stop", Model: "cursor/claude-opus-5-5@300k", Provider: "magpie",
	}, 0)
	message := state.currentAssistant()
	if len(message.blocks) != 2 {
		t.Fatalf("blocks = %#v", describeP29Blocks(message))
	}
	if message.blocks[0].phase != "commentary" || message.blocks[1].phase != "final_answer" {
		t.Fatalf("phases = %q / %q", message.blocks[0].phase, message.blocks[1].phase)
	}
	if message.blocks[0].text != "working on it" || message.blocks[1].text != "done" {
		t.Fatalf("final merge replaced instead of appended: %#v", describeP29Blocks(message))
	}
	if message.model != "cursor/claude-opus-5-5@300k" {
		t.Fatalf("model = %q", message.model)
	}
}

// History restore keeps thinking blocks, phase and per-message identity in
// the same ordered shape the live path produces.
func TestP29HistoryRestoreKeepsOrderedShape(t *testing.T) {
	records := []json.RawMessage{
		json.RawMessage(`{"role":"user","content":[{"type":"text","text":"prompt"}],"timestamp":1000}`),
		json.RawMessage(`{"role":"assistant","content":[
			{"type":"thinking","thinking":"let me look"},
			{"type":"text","text":"checking now","phase":"commentary"},
			{"type":"toolCall","id":"call_h","name":"bash","arguments":{"command":"ls"}}
		],"stopReason":"tooluse","model":"cursor/claude-opus-5-5@300k","provider":"magpie","timestamp":2000}`),
		json.RawMessage(`{"role":"toolResult","toolCallId":"call_h","toolName":"bash","content":[{"type":"text","text":"listing"}],"timestamp":2500}`),
		json.RawMessage(`{"role":"assistant","content":[{"type":"text","text":"final words"}],"stopReason":"stop","model":"cursor/claude-opus-5-5@300k","provider":"magpie","timestamp":3000}`),
	}
	chat, err := rebuildChatFromPiMessages(records)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if len(chat) != 3 {
		t.Fatalf("message count = %d", len(chat))
	}
	first := &chat[1]
	kinds := []string{}
	for i := range first.blocks {
		switch first.blocks[i].kind {
		case chatBlockThinking:
			kinds = append(kinds, "thinking:"+first.blocks[i].text)
		case chatBlockText:
			kinds = append(kinds, "text:"+first.blocks[i].text)
		case chatBlockTool:
			kinds = append(kinds, "tool:"+first.blocks[i].tool.id)
		}
	}
	want := strings.Join([]string{"thinking:let me look", "text:checking now", "tool:call_h"}, ",")
	if got := strings.Join(kinds, ","); got != want {
		t.Fatalf("history order = %q, want %q", got, want)
	}
	if first.model != "cursor/claude-opus-5-5@300k" || first.provider != "magpie" || first.stopReason != "tooluse" {
		t.Fatalf("history identity = %q/%q/%q", first.model, first.provider, first.stopReason)
	}
	if first.blocks[1].phase != "commentary" {
		t.Fatalf("history phase lost: %q", first.blocks[1].phase)
	}
	if tool := findP29Tool(first, "call_h"); tool == nil || tool.result != "listing" {
		t.Fatalf("history tool result lost")
	}
	second := &chat[2]
	if second.model == "" || second.stopReason != "stop" {
		t.Fatalf("second message identity = %q/%q", second.model, second.stopReason)
	}
	if !second.startedAt.Equal(piMessageTime(3000)) {
		t.Fatalf("history timestamp = %v", second.startedAt)
	}
}

func findP29Tool(message *chatMessage, id string) *toolCallView {
	tool, _ := (&appState{}).findToolBlock(message, id)
	return tool
}

// The RPC layer still streams aggregates for the old presentation and the
// HUD: thinking/text snapshots, deltas and usage stay untouched.
func TestP29RPCEventsKeepAggregateContract(t *testing.T) {
	var thinking, text, messageEvent piEvent
	client := &piClient{streamParts: map[int]string{}, thinkingParts: map[int]string{}}
	client.events = func(e piEvent) {
		switch e.Kind {
		case piEventThinking:
			thinking = e
		case piEventText:
			text = e
		case piEventMessage:
			messageEvent = e
		}
	}
	client.handleRecord([]byte(`{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"abc"}}`))
	client.handleRecord([]byte(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"hi"}}`))
	if thinking.Text != "abc" || thinking.PartText != "abc" || !thinking.PartSet || thinking.ContentIndex != 0 {
		t.Fatalf("thinking event = %#v", thinking)
	}
	if text.Text != "hi" || text.PartText != "hi" || text.ContentIndex != 1 {
		t.Fatalf("text event = %#v", text)
	}
	client.handleRecord([]byte(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"thinking","thinking":"abc"},{"type":"text","text":"hi"}],"stopReason":"stop","model":"m1","provider":"magpie","usage":{"output":7}}}`))
	message := messageEvent
	if message.Kind != piEventMessage {
		t.Fatal("message_end did not emit")
	}
	if message.StopReason != "stop" || message.Model != "m1" || message.Provider != "magpie" {
		t.Fatalf("message identity = %q/%q/%q", message.StopReason, message.Model, message.Provider)
	}
	if len(message.OrderedContent) != 2 || message.OrderedContent[0].Kind != "thinking" || message.OrderedContent[1].Kind != "text" {
		t.Fatalf("ordered content = %#v", message.OrderedContent)
	}
	if message.OutputTokens != 7 {
		t.Fatalf("usage = %d", message.OutputTokens)
	}
}

// Reduced-scope guard: a message_end carrying only identity must not create
// a phantom assistant row.
func TestP29MetadataOnlyMessageEndCreatesNoRow(t *testing.T) {
	state := p29TestState()
	before := len(state.chat)
	state.handlePiEvent(piEvent{
		Kind: piEventMessage, StopReason: "stop", Model: "m1", Provider: "magpie",
	}, 0)
	if len(state.chat) != before {
		t.Fatalf("metadata-only end created rows: %d -> %d", before, len(state.chat))
	}
}

// Coordinator reproduction 1: only text (index 1) streamed; message_end then
// delivers a thinking part (index 0) that never streamed plus the same text.
// The final snapshot is authoritative and must rebuild in final order
// [thinking, text], not keep the streamed fragment first.
func TestP29CoordinatorFinalSnapshotOrder(t *testing.T) {
	state := p29TestState()
	state.handlePiEvent(piEvent{
		Kind: piEventText, Text: "answer", PartText: "answer", PartSet: true, ContentIndex: 1,
	}, 0)
	state.handlePiEvent(piEvent{
		Kind: piEventMessage,
		OrderedContent: []piContentEntry{
			{Kind: "thinking", Text: "late thought", ContentIndex: 0},
			{Kind: "text", Text: "answer", ContentIndex: 1},
		},
	}, 0)
	blocks := state.currentAssistant().blocks
	if len(blocks) != 2 || blocks[0].kind != chatBlockThinking || blocks[1].kind != chatBlockText {
		t.Fatalf("final snapshot order lost: %#v", describeP29Blocks(state.currentAssistant()))
	}
	if blocks[0].text != "late thought" || blocks[1].text != "answer" {
		t.Fatalf("final snapshot texts = %q / %q", blocks[0].text, blocks[1].text)
	}
}

// Coordinator reproduction 2: final content holds an empty thinking (index 0)
// and text 'answer' (index 1); the stream delivered partial 'ans' at index 1.
// The real contentIndex must survive the empty-entry filter so the streamed
// fragment is replaced, not duplicated.
func TestP29CoordinatorEmptyContentIndex(t *testing.T) {
	state := p29TestState()
	state.handlePiEvent(piEvent{
		Kind: piEventText, Text: "ans", PartText: "ans", PartSet: true, ContentIndex: 1,
	}, 0)
	final := piOrderedContent(json.RawMessage(`[{"type":"thinking","thinking":""},{"type":"text","text":"answer"}]`))
	if len(final) != 1 || final[0].ContentIndex != 1 {
		t.Fatalf("empty entry shifted indexes: %#v", final)
	}
	state.handlePiEvent(piEvent{Kind: piEventMessage, OrderedContent: final}, 0)
	blocks := state.currentAssistant().blocks
	if len(blocks) != 1 || blocks[0].kind != chatBlockText || blocks[0].text != "answer" {
		t.Fatalf("partial text duplicated: %#v", describeP29Blocks(state.currentAssistant()))
	}
}

func reviewWired() (*appState, *piClient) {
	state := p29TestState()
	// A user row ahead of the assistant row lets per-turn token counting
	// behave like a real prompt turn.
	state.chat = append([]chatMessage{{role: chatUser, blocks: []chatBlock{{kind: chatBlockText, text: "q"}}}}, state.chat...)
	client := &piClient{streamParts: map[int]string{}, thinkingParts: map[int]string{}}
	client.events = func(e piEvent) {
		e.Source = nil
		state.handlePiEvent(e, 0)
	}
	return state, client
}

func feed(client *piClient, lines ...string) {
	for _, line := range lines {
		client.handleRecord([]byte(line))
	}
}

// Review R1: the second assistant source message is toolCall-only. The
// explicit assistant message_start boundary must keep the first message's
// blocks and append the new tool, and the final text must not lose data.
func TestReviewR1ToolOnlySecondMessage(t *testing.T) {
	state, client := reviewWired()
	feed(client,
		`{"type":"agent_start"}`,
		`{"type":"message_start","message":{"role":"assistant","content":[]}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"plan"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"I will read"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_delta","contentIndex":2,"delta":"{}"}}`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"thinking","thinking":"plan"},{"type":"text","text":"I will read"},{"type":"toolCall","id":"A","name":"read","arguments":{"path":"a"}}],"stopReason":"toolUse","usage":{"output":10}}}`,
		`{"type":"tool_execution_start","toolCallId":"A","toolName":"read","args":{"path":"a"}}`,
		`{"type":"tool_execution_end","toolCallId":"A","toolName":"read","result":{"content":[{"type":"text","text":"A-body"}]}}`,
		`{"type":"message_end","message":{"role":"toolResult","toolCallId":"A","content":[{"type":"text","text":"A-body"}]}}`,
		`{"type":"message_start","message":{"role":"assistant","content":[]}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_delta","contentIndex":0,"delta":"{}"}}`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"toolCall","id":"B","name":"bash","arguments":{"command":"ls"}}],"stopReason":"toolUse","usage":{"output":5}}}`,
		`{"type":"tool_execution_start","toolCallId":"B","toolName":"bash","args":{"command":"ls"}}`,
		`{"type":"tool_execution_end","toolCallId":"B","toolName":"bash","result":{"content":[{"type":"text","text":"B-out"}]}}`,
		`{"type":"message_start","message":{"role":"assistant","content":[]}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"done"}}`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"done"}],"stopReason":"stop","usage":{"output":3}}}`,
		`{"type":"agent_settled"}`,
	)
	got := strings.Join(describeP29Blocks(state.currentAssistant()), ",")
	want := "thinking:plan,text:I will read,tool:A,tool:B,text:done"
	if got != want {
		t.Fatalf("R1 order/data loss: got %q want %q", got, want)
	}
	if state.currentAssistant().outputTokens != 18 {
		t.Fatalf("R1 turn output tokens = %d, want 18", state.currentAssistant().outputTokens)
	}
}

// Review R2 (asserted form): a tool-only message_end carries usage that must
// flow into the per-turn output token count, and the tool-only message must
// not be dropped.
func TestReviewR2ToolOnlyUsageCounted(t *testing.T) {
	state := &appState{piConn: piConnected, run: runStreaming, chat: []chatMessage{
		{role: chatUser, blocks: []chatBlock{{kind: chatBlockText, text: "q"}}},
		{role: chatAssistant, status: "Streaming"},
	}}
	client := &piClient{streamParts: map[int]string{}, thinkingParts: map[int]string{}}
	client.events = func(e piEvent) {
		e.Source = nil
		state.handlePiEvent(e, 0)
	}
	feed(client,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"toolCall","id":"B","name":"bash","arguments":{}}],"stopReason":"toolUse","usage":{"output":50}}}`,
	)
	if state.chat[1].outputTokens != 50 {
		t.Fatalf("tool-only usage lost: %d", state.chat[1].outputTokens)
	}
	if tool, _ := state.findToolBlock(&state.chat[1], "B"); tool == nil {
		t.Fatal("tool-only message dropped")
	}
}

// Review R7: real Pi stores the phase inside textSignature (TextSignatureV1);
// both the live ordered parse and the history restore must read it, keeping
// the explicit phase field compatible.
func TestReviewR7PhaseFromTextSignature(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":"looking","textSignature":"{\"v\":1,\"id\":\"msg_1\",\"phase\":\"commentary\"}"},{"type":"text","text":"answer","textSignature":"{\"v\":1,\"id\":\"msg_2\",\"phase\":\"final_answer\"}"}]`)
	live := piOrderedContent(raw)
	hist, err := piAssistantBlocks(raw)
	if err != nil {
		t.Fatal(err)
	}
	if live[0].Phase != "commentary" || live[1].Phase != "final_answer" {
		t.Fatalf("live phases = %q / %q", live[0].Phase, live[1].Phase)
	}
	if hist[0].phase != "commentary" || hist[1].phase != "final_answer" {
		t.Fatalf("history phases = %q / %q", hist[0].phase, hist[1].phase)
	}
	if phase := piTextPhase("", `{"v":2,"phase":"final_answer"}`); phase != "" {
		t.Fatalf("unknown signature version accepted: %q", phase)
	}
	if phase := piTextPhase("commentary", ""); phase != "commentary" {
		t.Fatal("explicit phase must keep priority")
	}
}

// Review R8: a late delta to an earlier contentIndex inside the SAME message
// (thinking_end arrives after text events) must update its own block, not
// duplicate the thinking segment.
func TestReviewR8LateDeltaEarlierIndex(t *testing.T) {
	state, client := reviewWired()
	feed(client,
		`{"type":"message_start","message":{"role":"assistant","content":[]}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"a"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"b"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"2"}}`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"thinking","thinking":"a2"},{"type":"text","text":"b"}],"stopReason":"stop"}}`,
	)
	got := strings.Join(describeP29Blocks(state.currentAssistant()), ",")
	if got != "thinking:a2,text:b" {
		t.Fatalf("stale fragment kept: %q", got)
	}
}

// Real Pi 1.5 metadata order (rpc-event-order.json): one thinking segment and
// one text segment inside a single source message — the thinking_end arrives
// at index 0 after text events at index 1, and the thinking must stay one
// block, not two.
func TestP29RealPiEventOrderSingleThinkingSingleText(t *testing.T) {
	state, client := reviewWired()
	feed(client,
		`{"type":"agent_start"}`,
		`{"type":"message_start","message":{"role":"assistant"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_start","contentIndex":0}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"think "}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"more"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_start","contentIndex":1}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"answer"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_end","contentIndex":0}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_end","contentIndex":1}}`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"thinking","thinking":"think more"},{"type":"text","text":"answer"}],"stopReason":"stop","usage":{"output":9}}}`,
		`{"type":"agent_settled"}`,
	)
	blocks := describeP29Blocks(state.currentAssistant())
	if len(blocks) != 2 || blocks[0] != "thinking:think more" || blocks[1] != "text:answer" {
		t.Fatalf("real order regression: %v", blocks)
	}
	message := state.currentAssistant()
	if message.thinking != "think more" {
		t.Fatalf("compat thinking = %q", message.thinking)
	}
	if message.stopReason != "stop" {
		t.Fatalf("stop reason = %q", message.stopReason)
	}
}

// Two consecutive final-only source messages (no deltas in between, each
// without its own message_start... the second still has one in real Pi) must
// never overwrite the first message; a duplicate final of the SAME source
// stays idempotent.
func TestP29ConsecutiveFinalOnlySourcesAndDuplicate(t *testing.T) {
	state := p29TestState()
	state.handlePiEvent(piEvent{
		Kind: piEventMessage,
		OrderedContent: []piContentEntry{
			{Kind: "text", Text: "first message", ContentIndex: 0},
		},
	}, 0)
	// Same source delivers its final content again: idempotent.
	state.handlePiEvent(piEvent{
		Kind: piEventMessage,
		OrderedContent: []piContentEntry{
			{Kind: "text", Text: "first message", ContentIndex: 0},
		},
	}, 0)
	blocks := describeP29Blocks(state.currentAssistant())
	if len(blocks) != 1 || blocks[0] != "text:first message" {
		t.Fatalf("duplicate final not idempotent: %v", blocks)
	}
	// A genuinely new final-only source message appends without message_start.
	state.handlePiEvent(piEvent{
		Kind: piEventMessage,
		OrderedContent: []piContentEntry{
			{Kind: "text", Text: "second message", ContentIndex: 0},
		},
	}, 0)
	blocks = describeP29Blocks(state.currentAssistant())
	if len(blocks) != 2 || blocks[0] != "text:first message" || blocks[1] != "text:second message" {
		t.Fatalf("second final-only source overwrote the first: %v", blocks)
	}
}

// Placeholder texts are synthetic; real text blocks never are.
func TestP29SyntheticPlaceholderMarking(t *testing.T) {
	state, client := reviewWired()
	feed(client,
		`{"type":"agent_start"}`,
		`{"type":"message_start","message":{"role":"assistant"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"real words"}}`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"real words"}],"stopReason":"stop"}}`,
		`{"type":"agent_settled"}`,
	)
	blocks := state.currentAssistant().blocks
	if len(blocks) != 1 || blocks[0].synthetic {
		t.Fatalf("real text marked synthetic: %#v", blocks)
	}
	empty := p29TestState()
	empty.handlePiEvent(piEvent{Kind: piEventSettled}, 0)
	fallback := empty.currentAssistant().blocks
	if len(fallback) != 1 || fallback[0].kind != chatBlockText || !fallback[0].synthetic {
		t.Fatalf("fallback placeholder not synthetic: %#v", fallback)
	}
	history, err := rebuildChatFromPiMessages([]json.RawMessage{
		json.RawMessage(`{"role":"user","content":"q","timestamp":1000}`),
		json.RawMessage(`{"role":"assistant","content":[],"stopReason":"error","errorMessage":"boom","timestamp":2000}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	injected := history[len(history)-1].blocks
	if len(injected) != 1 || !injected[0].synthetic {
		t.Fatalf("history error text not synthetic: %#v", injected)
	}
	realHistory, err := rebuildChatFromPiMessages([]json.RawMessage{
		json.RawMessage(`{"role":"user","content":"q","timestamp":1000}`),
		json.RawMessage(`{"role":"assistant","content":[{"type":"text","text":"actual"}],"stopReason":"stop","timestamp":2000}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if realHistory[1].blocks[0].synthetic {
		t.Fatal("real history text marked synthetic")
	}
}

// setAssistantText is the plain real-text helper (legacy aggregate path and
// old direct callers): its text must stay synthetic=false. Only explicit
// app-injected notices (finishAssistant) and fallback placeholders are true.
func TestP29LegacyFinalTextIsRealContent(t *testing.T) {
	state := p29TestState()
	state.handlePiEvent(piEvent{Kind: piEventMessage, Text: "real legacy answer"}, 0)
	blocks := state.currentAssistant().blocks
	if len(blocks) != 1 || blocks[0].text != "real legacy answer" || blocks[0].synthetic {
		t.Fatalf("legacy final text must stay real: %#v", blocks)
	}
	// finishAssistant replaces the trailing text with an app notice and marks
	// it synthetic; a waiting row keeps the real text and appends the notice.
	state.finishAssistant("pi handled by an extension", "handled")
	notices := state.currentAssistant().blocks
	if len(notices) != 1 || notices[0].text != "pi handled by an extension" || !notices[0].synthetic {
		t.Fatalf("finish notice must be synthetic: %#v", notices)
	}
	// A waiting row (previous one ended) receives the notice as a fresh row
	// whose single block is synthetic.
	appended := p29TestState()
	appended.handlePiEvent(piEvent{Kind: piEventMessage, Text: "real legacy answer"}, 0)
	appended.chat[len(appended.chat)-1].endedAt = time.Now()
	appended.finishAssistant("second notice", "handled")
	row := appended.currentAssistant().blocks
	if len(row) != 1 || row[0].text != "second notice" || !row[0].synthetic {
		t.Fatalf("fresh notice row must be synthetic: %#v", row)
	}
}

// User rows carry real times: restored history keeps the stored Pi timestamp
// (zero when the session has none), live local submissions record the actual
// moment, and nothing backfills history with the restore time.
func TestP29UserMessageTimes(t *testing.T) {
	chat, err := rebuildChatFromPiMessages([]json.RawMessage{
		json.RawMessage(`{"role":"user","content":[{"type":"text","text":"timed"}],"timestamp":42000}`),
		json.RawMessage(`{"role":"user","content":[{"type":"text","text":"untimed"}]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(chat) != 2 {
		t.Fatalf("row count = %d", len(chat))
	}
	if !chat[0].startedAt.Equal(piMessageTime(42000)) || chat[0].startedAt.IsZero() {
		t.Fatalf("restored user timestamp lost: %v", chat[0].startedAt)
	}
	if !chat[1].startedAt.IsZero() {
		t.Fatalf("missing timestamp invented: %v", chat[1].startedAt)
	}

	state := p29TestState()
	state.startAssistantPrompt("hello", nil)
	submitted := lastUserRow(state.chat)
	if submitted < 0 || state.chat[submitted].startedAt.IsZero() {
		t.Fatalf("local submission time missing: %v", state.chat[submitted].startedAt)
	}

	echo := p29TestState()
	echo.appendRunningUserMessage("delayed echo")
	live := lastUserRow(echo.chat)
	if live < 0 || echo.chat[live].startedAt.IsZero() {
		t.Fatalf("echo row time missing: %v", echo.chat[live].startedAt)
	}
}

func lastUserRow(chat []chatMessage) int {
	for i := len(chat) - 1; i >= 0; i-- {
		if chat[i].role == chatUser {
			return i
		}
	}
	return -1
}
