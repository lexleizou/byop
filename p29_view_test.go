package main

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components/chat"
	lu "github.com/oneo-me/onevra/source/icons/lucide"
)

// P29 seat-B tests: the turn model, the reveal and throttle timing, the
// HUD capsule binding across the fold, and the tool/label mappings.

func p29ThinkBlock(value string, phase string) chatBlock {
	return chatBlock{kind: chatBlockThinking, text: value, phase: phase}
}

func p29TextBlock(value string, phase string) chatBlock {
	return chatBlock{kind: chatBlockText, text: value, phase: phase}
}

func p29ToolBlock(id, name string, status toolCallStatus) chatBlock {
	return chatBlock{kind: chatBlockTool, tool: toolCallView{
		id: id, name: name, status: status,
	}}
}

func p29Assistant(blocks ...chatBlock) *chatMessage {
	return &chatMessage{role: chatAssistant, status: "Complete", blocks: blocks}
}

func TestP29PhaseTurnKeepsOrderCountsAndFinal(t *testing.T) {
	message := p29Assistant(
		p29ThinkBlock("first thought", ""),
		p29TextBlock("checking now", p29PhaseCommentary),
		p29ToolBlock("t1", "read", toolCallCompleted),
		p29ThinkBlock("second thought", ""),
		p29TextBlock("the answer", p29PhaseFinalAnswer),
	)
	turn := buildP29Turn([]*chatMessage{message}, false)
	if turn.thinkingCount != 2 || turn.toolCount != 1 {
		t.Fatalf("counts: thinking=%d tool=%d, want 2/1", turn.thinkingCount, turn.toolCount)
	}
	wantKinds := []p29NodeKind{p29NodeActivity, p29NodeCommentary, p29NodeActivity}
	if len(turn.nodes) != len(wantKinds) {
		t.Fatalf("nodes=%d (%#v), want %d", len(turn.nodes), turn.nodes, len(wantKinds))
	}
	for i, want := range wantKinds {
		if turn.nodes[i].kind != want {
			t.Fatalf("node %d kind=%d, want %d", i, turn.nodes[i].kind, want)
		}
	}
	if len(turn.nodes[0].items) != 1 || len(turn.nodes[2].items) != 2 {
		t.Fatalf("activity runs not grouped: %#v", turn.nodes)
	}
	if turn.finalKind != p29FinalAnswer || turn.finalText != "the answer" {
		t.Fatalf("final=%d text=%q, want Answer/the answer", turn.finalKind, turn.finalText)
	}
}

func TestP29EarlierPhasesDoNotSuppressPlainAnswer(t *testing.T) {
	// A provider that spoke phases earlier and stays silent on the final
	// message must still get its trailing text as the answer.
	first := p29Assistant(p29TextBlock("working on it", p29PhaseCommentary))
	last := p29Assistant(p29TextBlock("Plain answer.", ""))
	turn := buildP29Turn([]*chatMessage{first, last}, false)
	if turn.finalKind != p29FinalAnswer || turn.finalText != "Plain answer." {
		t.Fatalf("final=%d text=%q, want Answer/Plain answer.", turn.finalKind, turn.finalText)
	}
	if len(turn.nodes) != 1 || turn.nodes[0].kind != p29NodeCommentary {
		t.Fatalf("commentary node missing: %#v", turn.nodes)
	}
}

func TestP29LiveTextStaysProcessUntilSettled(t *testing.T) {
	message := p29Assistant(p29TextBlock("drafting...", ""))
	message.status = "Streaming"
	live := buildP29Turn([]*chatMessage{message}, true)
	if live.finalKind != p29FinalNone {
		t.Fatal("live turn dressed unconfirmed text up as the answer")
	}
	if len(live.nodes) != 1 || live.nodes[0].kind != p29NodeCommentary {
		t.Fatalf("live commentary not in process: %#v", live.nodes)
	}
	settled := buildP29Turn([]*chatMessage{message}, false)
	if settled.finalKind != p29FinalAnswer || settled.finalText != "drafting..." {
		t.Fatalf("settled final=%d text=%q", settled.finalKind, settled.finalText)
	}
	if len(settled.nodes) != 0 {
		t.Fatalf("settled turn kept the answer inside the process: %#v", settled.nodes)
	}
}

func TestP29CommentaryPlusThinkingIsNotLiftedAsAnswer(t *testing.T) {
	// Prose (even as commentary) rules out thinking-as-answer.
	message := p29Assistant(
		p29ThinkBlock("pondering", ""),
		p29TextBlock("side note", p29PhaseCommentary),
	)
	turn := buildP29Turn([]*chatMessage{message}, false)
	if turn.finalKind != p29FinalNoText {
		t.Fatalf("final=%d, want NoText", turn.finalKind)
	}
	if len(turn.nodes) == 0 || turn.nodes[0].kind != p29NodeActivity {
		t.Fatalf("thinking dropped from process: %#v", turn.nodes)
	}
}

func TestP29ThinkingOnlyTurnLiftsLastThinking(t *testing.T) {
	message := p29Assistant(
		p29ThinkBlock("first thought", ""),
		p29ThinkBlock("the real thought", ""),
	)
	turn := buildP29Turn([]*chatMessage{message}, false)
	if turn.finalKind != p29FinalThinkingOnly || turn.finalText != "the real thought" {
		t.Fatalf("final=%d text=%q", turn.finalKind, turn.finalText)
	}
	remaining := 0
	for _, node := range turn.nodes {
		remaining += len(node.items)
	}
	if remaining != 1 || turn.nodes[0].items[0].text != "first thought" {
		t.Fatalf("lifted thinking duplicated in process: %#v", turn.nodes)
	}
	if turn.thinkingCount != 2 {
		t.Fatalf("thinking count=%d, want 2 (counted before lift)", turn.thinkingCount)
	}
}

func TestP29StoppedAndFailedTurnsDoNotClaimSuccess(t *testing.T) {
	stopped := &chatMessage{role: chatAssistant, status: "stopped",
		blocks: []chatBlock{p29TextBlock("half a sentence", "")}}
	turn := buildP29Turn([]*chatMessage{stopped}, false)
	if turn.status != chat.Cancelled {
		t.Fatalf("stopped status=%v", turn.status)
	}
	if turn.finalKind != p29FinalIncomplete {
		t.Fatalf("stopped final=%d, want Incomplete", turn.finalKind)
	}
	errored := &chatMessage{role: chatAssistant, status: "Complete", stopReason: "error",
		blocks: []chatBlock{p29TextBlock("truncated", "")}}
	turn = buildP29Turn([]*chatMessage{errored}, false)
	if turn.status != chat.Failed || turn.finalKind != p29FinalIncomplete {
		t.Fatalf("stopReason error: status=%v final=%d", turn.status, turn.finalKind)
	}
	aborted := &chatMessage{role: chatAssistant, status: "Complete", stopReason: "aborted",
		blocks: []chatBlock{p29TextBlock("cut", "")}}
	turn = buildP29Turn([]*chatMessage{aborted}, false)
	if turn.status != chat.Cancelled || turn.finalKind != p29FinalIncomplete {
		t.Fatalf("stopReason aborted: status=%v final=%d", turn.status, turn.finalKind)
	}
}

func TestP29ToolCountIsUniqueByCallID(t *testing.T) {
	// A protocol echo of the same call ID stays one tool; empty IDs each
	// count once.
	message := p29Assistant(
		p29ToolBlock("same", "bash", toolCallCompleted),
		p29ToolBlock("same", "bash", toolCallCompleted),
		p29ToolBlock("", "bash", toolCallCompleted),
		p29ToolBlock("", "bash", toolCallCompleted),
		p29ThinkBlock("  ", ""), // whitespace thinking never counts
	)
	turn := buildP29Turn([]*chatMessage{message}, false)
	if turn.toolCount != 3 {
		t.Fatalf("toolCount=%d, want 3 (2 unique IDs + 2 empty)", turn.toolCount)
	}
	if turn.thinkingCount != 0 {
		t.Fatalf("thinkingCount=%d, want 0", turn.thinkingCount)
	}
}

func TestP29StatusLabelThrottle(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	state := &p29StatusState{}
	if shown, changed, wait := state.advance("Reading a", base, false, false); !changed ||
		shown != "Reading a" || wait != 0 {
		t.Fatalf("anchor: shown=%q changed=%v wait=%v", shown, changed, wait)
	}
	shown, changed, wait := state.advance("Reading b", base.Add(100*time.Millisecond), false, false)
	if shown != "Reading a" || changed || wait <= 0 || wait > 900*time.Millisecond {
		t.Fatalf("throttle: shown=%q changed=%v wait=%v", shown, changed, wait)
	}
	// The newest value wins once the interval passes, not the pending one
	// that arrived first.
	state.advance("Reading c", base.Add(200*time.Millisecond), false, false)
	shown, changed, _ = state.advance("Reading c", base.Add(950*time.Millisecond), false, false)
	if shown != "Reading c" || !changed {
		t.Fatalf("latest-wins: shown=%q changed=%v", shown, changed)
	}
	// Failures resolve immediately.
	state.advance("Reading d", base.Add(960*time.Millisecond), false, false)
	if shown, _, _ := state.advance("Tool failed", base.Add(970*time.Millisecond), false, true); shown != "Tool failed" {
		t.Fatalf("immediate: shown=%q", shown)
	}
	// Reduced motion never waits.
	reduced := &p29StatusState{}
	reduced.advance("one", base, true, false)
	if shown, changed, wait := reduced.advance("two", base.Add(time.Millisecond), true, false); shown != "two" || !changed || wait != 0 {
		t.Fatalf("reduced: shown=%q changed=%v wait=%v", shown, changed, wait)
	}
}

func TestP29RevealProgressRunsTheCurve(t *testing.T) {
	appeared := map[string]bool{}
	progress := float32(-1)
	tt := ui.NewTester(func(c *ui.Context) {
		box := ui.Box(c).Key("probe").Size(10, 10)
		progress = p29RevealProgress(box, appeared, "k",
			ui.Animation{Duration: p29MotionNodeEnter, Ease: p29EaseReveal()}, false)
	}, 60, 60)
	if progress != 0 {
		t.Fatalf("reveal must start at 0 after the jump, got %v", progress)
	}
	tt.Advance(160 * time.Millisecond)
	if progress <= 0.1 || progress >= 1 {
		t.Fatalf("reveal mid-curve expected, got %v", progress)
	}
	tt.Advance(400 * time.Millisecond)
	if progress != 1 {
		t.Fatalf("reveal must end at 1, got %v", progress)
	}
}

func TestP29RevealProgressHonoursDelayAndReducedMotion(t *testing.T) {
	appeared := map[string]bool{}
	progress := float32(-1)
	tt := ui.NewTester(func(c *ui.Context) {
		box := ui.Box(c).Key("probe").Size(10, 10)
		animation := ui.Animation{
			Duration: p29MotionAnswerReveal,
			Delay:    p29MotionRevealDelay,
			Ease:     p29EaseReveal(),
		}
		progress = p29RevealProgress(box, appeared, "k", animation, false)
	}, 60, 60)
	tt.Advance(60 * time.Millisecond)
	if progress != 0 {
		t.Fatalf("delayed reveal moved before its 150 ms delay: %v", progress)
	}
	tt.Advance(120 * time.Millisecond)
	if progress <= 0 || progress >= 1 {
		t.Fatalf("delayed reveal not running after the delay: %v", progress)
	}
	tt.Advance(600 * time.Millisecond)
	if progress != 1 {
		t.Fatalf("delayed reveal must end at 1, got %v", progress)
	}

	// Reduced motion bypasses the channel entirely and reports the end.
	reducedSeen := map[string]bool{}
	ui.NewTester(func(c *ui.Context) {
		element := ui.Box(c).Key("probe").Size(10, 10)
		progress = p29RevealProgress(element, reducedSeen, "k",
			ui.Animation{Duration: p29MotionNodeEnter}, true)
	}, 60, 60)
	if progress != 1 {
		t.Fatalf("reduced reveal must arrive at once, got %v", progress)
	}
	if len(reducedSeen) != 0 {
		t.Fatal("reduced reveal must not arm channels")
	}
}

func p29LiveCapsuleState(t *testing.T) *appState {
	t.Helper()
	state := p18_2HUDState(t)
	state.run = runStreaming
	state.chat = []chatMessage{
		{role: chatUser, status: "Complete",
			blocks: []chatBlock{p29TextBlock("Read it for me", "")}},
		{
			role:         chatAssistant,
			status:       "Streaming",
			outputTokens: 9,
			blocks: []chatBlock{
				p29ThinkBlock("Where to read?", ""),
				p29ToolBlock("call-1", "read", toolCallRunning),
			},
		},
	}
	return state
}

func TestP29CapsuleStaysWhileProcessFolds(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	state := p29LiveCapsuleState(t)
	tt := ui.NewTester(state.view, 1180, 760)
	capsule := textArgs("hudTokensPerSecond", map[string]any{"speed": "51.2"})
	if _, ok := tt.Find(capsule); !ok {
		t.Fatalf("live t/s capsule missing: %#v", tt.Texts())
	}
	summary := p29ProcessTitleParts(1, 1, true, "0s")
	status := text("p29ReadLive")
	if _, ok := tt.Find(status); !ok {
		t.Fatal("live activity status missing while running")
	}
	if err := tt.Click(summary); err != nil {
		t.Fatal(err)
	}
	tt.Advance(700 * time.Millisecond)
	if _, ok := tt.Find(status); ok {
		t.Fatal("folded process still rendered its timeline")
	}
	if _, ok := tt.Find(capsule); !ok {
		t.Fatal("capsule vanished with the fold")
	}
	if _, ok := tt.Find(summary); !ok {
		t.Fatal("fold control vanished")
	}
	if err := tt.Click(summary); err != nil {
		t.Fatal(err)
	}
	tt.Advance(700 * time.Millisecond)
	if _, ok := tt.Find(status); !ok {
		t.Fatal("timeline did not return after re-expanding")
	}
}

func TestP29SettledTurnFoldsStepsAndDetail(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	state := p18_2HUDState(t)
	state.chat = []chatMessage{
		{role: chatUser, status: "Complete",
			blocks: []chatBlock{p29TextBlock("Show me the steps", "")}},
		{
			role:   chatAssistant,
			status: "Complete",
			blocks: []chatBlock{
				p29ThinkBlock("ponder this", ""),
				{kind: chatBlockTool, tool: toolCallView{
					id: "call-9", name: "read", status: toolCallCompleted,
					arguments: `{"path":"internal/pi.go"}`, result: "package main\n",
				}},
				p29TextBlock("Done reading it.", ""),
			},
		},
	}
	tt := ui.NewTester(state.view, 1180, 760)
	// History arrives folded and never arms a reveal channel.
	if pst := state.chatView.p29["p29-turn-0"]; pst == nil || len(pst.appeared) != 0 {
		t.Fatalf("history armed reveals: %#v", state.chatView.p29)
	}
	done := text("p29ReadDone")
	if _, ok := tt.Find(done); ok {
		t.Fatal("settled process was not folded by default")
	}
	summary := p29ProcessTitleParts(1, 1, false, "0s")
	if _, ok := tt.Find(summary); !ok {
		t.Fatalf("fold control missing: %#v", tt.Texts())
	}
	if err := tt.Click(summary); err != nil {
		t.Fatal(err)
	}
	tt.Advance(600 * time.Millisecond)
	head := done + " " + textArgs("p29ProcessSteps", map[string]any{"count": 2})
	if err := tt.Click(head); err != nil {
		t.Fatalf("activity head %q: %v; texts=%#v", head, err, tt.Texts())
	}
	tt.Advance(300 * time.Millisecond)
	if _, ok := tt.Find("ponder this"); !ok {
		t.Fatalf("thinking detail missing: %#v", tt.Texts())
	}
	if _, ok := tt.Find("internal/pi.go"); !ok {
		t.Fatalf("tool summary missing: %#v", tt.Texts())
	}
	if _, ok := tt.Find(text("p29ShowDetail")); !ok {
		t.Fatal("tool detail disclosure missing")
	}
	if err := tt.Click(text("p29ShowDetail")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("package main") {
		t.Fatalf("expanded tool result missing: %#v", tt.Texts())
	}
	// The final answer sits below the process and never folds with it.
	if _, ok := tt.Find("Done reading it."); !ok {
		t.Fatal("final answer missing")
	}
}

func TestP29ToolFamiliesIconsAndTags(t *testing.T) {
	cases := []struct {
		name   string
		family string
		icon   func() *ui.SVG
	}{
		{"Read", "read", lu.FileText},
		{"Write", "write", lu.FilePenLine},
		{"Edit", "edit", lu.Pencil},
		{"Bash", "bash", lu.Terminal},
		{"Glob", "glob", lu.FolderSearch},
		{"Grep", "grep", lu.Search},
		{"Agent", "agent", lu.Bot},
		{"Task", "task", lu.GitBranch},
		{"WebSearch", "websearch", lu.Globe},
		{"WebFetch", "webfetch", lu.Download},
		{"mcp__files__open", "mcp", lu.Plug},
		{"totally-unknown", "tool", lu.Wrench},
	}
	for _, test := range cases {
		if family := p29ToolFamily(test.name); family != test.family {
			t.Fatalf("%s family=%q, want %q", test.name, family, test.family)
		}
		if icon := p29ToolIcon(test.name); &icon == nil || icon() == nil {
			t.Fatalf("%s icon missing", test.name)
		}
	}
	if tag, _, ok := p29ToolTag("mcp__files__open"); !ok || tag != text("p29McpTag") {
		t.Fatalf("MCP tag=%q ok=%v", tag, ok)
	}
	if _, _, ok := p29ToolTag("read"); ok {
		t.Fatal("plain tools carry no tag")
	}
}

func TestP29ToolSummariesStayHonest(t *testing.T) {
	read := toolCallView{name: "read", arguments: `{"path":"a/b.go"}`}
	if got := p29ToolSummary(read); got != "a/b.go" {
		t.Fatalf("read summary=%q", got)
	}
	bash := toolCallView{name: "bash",
		arguments: `{"command":"go test ./...","description":"run tests"}`}
	if got := p29ToolSummary(bash); got != "run tests" {
		t.Fatalf("bash summary=%q", got)
	}
	grep := toolCallView{name: "grep", arguments: `{"pattern":"p29"}`}
	if got := p29ToolSummary(grep); got != "p29" {
		t.Fatalf("grep summary=%q", got)
	}
	broken := toolCallView{name: "read", arguments: `not json at all`}
	if got := p29ToolSummary(broken); got == "" || !strings.Contains(got, "not json") {
		t.Fatalf("broken JSON summary=%q", got)
	}
	empty := toolCallView{name: "read", arguments: ``}
	if got := p29ToolSummary(empty); got != "" {
		t.Fatalf("missing arguments invented %q", got)
	}
	if _, ok := p29EditSummary(toolCallView{name: "edit", result: "ok"}); ok {
		t.Fatal("edit invented a summary from an unclear result")
	}
	summary, ok := p29EditSummary(toolCallView{
		name: "edit", result: "Attempted to make 2 replacements."})
	if !ok || !strings.Contains(summary, "2") {
		t.Fatalf("edit summary=%q ok=%v", summary, ok)
	}
}

func TestP29ActivityStatusSemantics(t *testing.T) {
	runningNode := p29Node{items: []p29Item{
		{kind: p29ItemTool, tool: toolCallView{id: "1", name: "read", status: toolCallRunning}},
	}}
	live := &p29Turn{live: true, status: chat.Running}
	label, immediate := p29ActivityStatus(live, &runningNode, true, true)
	if label != text("p29ReadLive") || immediate {
		t.Fatalf("running label=%q immediate=%v", label, immediate)
	}
	doneNode := p29Node{items: []p29Item{{kind: p29ItemThinking, text: "ok"}}}
	label, immediate = p29ActivityStatus(live, &doneNode, false, true)
	if label != text("p29ThinkingDone") || !immediate {
		t.Fatalf("stale thinking label=%q immediate=%v", label, immediate)
	}
	failed := p29Node{items: []p29Item{
		{kind: p29ItemTool, tool: toolCallView{id: "1", name: "bash", status: toolCallFailed}},
	}}
	settled := &p29Turn{status: chat.Completed}
	label, _ = p29ActivityStatus(settled, &failed, true, false)
	if want := textArgs("p29ToolFailed", map[string]any{"name": "bash"}); label != want {
		t.Fatalf("failed label=%q, want %q", label, want)
	}
	cancelled := p29Node{items: []p29Item{
		{kind: p29ItemTool, tool: toolCallView{id: "1", name: "bash", status: toolCallCancelled}},
	}}
	label, _ = p29ActivityStatus(settled, &cancelled, true, false)
	if want := textArgs("p29ToolIncomplete", map[string]any{"name": "bash"}); label != want {
		t.Fatalf("cancelled label=%q, want %q", label, want)
	}
}

func TestP29ActivityLabelItemPrefersRunningTool(t *testing.T) {
	node := p29Node{items: []p29Item{
		{kind: p29ItemTool, tool: toolCallView{id: "1", name: "read", status: toolCallCompleted}},
		{kind: p29ItemTool, tool: toolCallView{id: "2", name: "bash", status: toolCallRunning}},
		{kind: p29ItemThinking, text: "later thought"},
	}}
	item, ok := p29ActivityLabelItem(node)
	if !ok || item.kind != p29ItemTool || item.tool.id != "2" {
		t.Fatalf("label item=%#v", item)
	}
}

func TestP29ProcessTitlePartsJoin(t *testing.T) {
	joined := p29ProcessTitleParts(2, 3, false, "8s")
	want := strings.Join([]string{
		textArgs("p29ProcessThinking", map[string]any{"count": 2}),
		textArgs("p29ProcessTools", map[string]any{"count": 3}),
		textArgs("p29ProcessDuration", map[string]any{"duration": "8s"}),
	}, text("p29ProcessJoin"))
	if joined != want {
		t.Fatalf("joined=%q want=%q", joined, want)
	}
	if got := p29ProcessTitleParts(0, 0, false, "8s"); got != textArgs("chatWorkedFor", map[string]any{"duration": "8s"}) {
		t.Fatalf("empty settled title=%q", got)
	}
	if got := p29ProcessTitleParts(0, 0, true, "0s"); got != textArgs("chatWorkingFor", map[string]any{"duration": "0s"}) {
		t.Fatalf("empty live title=%q", got)
	}
}

func TestP29SettledHistoryHeaderUsesOwnModel(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	state := p18_2HUDState(t)
	state.piModels = append(state.piModels, piModel{
		Provider: "custom", ID: "historian-7b", Name: "Historian 7B",
	})
	started := time.Date(2026, 11, 6, 9, 41, 0, 0, time.Local)
	state.chat = []chatMessage{
		{role: chatUser, status: "Complete", startedAt: started,
			blocks: []chatBlock{p29TextBlock("Earlier question", "")}},
		{
			role: chatAssistant, status: "Complete", model: "historian-7b",
			provider: "custom", outputTokens: 120, startedAt: started,
			blocks: []chatBlock{p29TextBlock("An older answer.", "")},
		},
		{role: chatUser, status: "Complete",
			blocks: []chatBlock{p29TextBlock("Current question", "")}},
		{
			role: chatAssistant, status: "Complete",
			model: "DeepSeek V4.1 Flash", provider: "custom", outputTokens: 21,
			blocks: []chatBlock{p29TextBlock("Fresh answer.", "")},
		},
	}
	tt := ui.NewTester(state.view, 1180, 760)
	// The catalog resolves the friendly name from provider+ID; the raw ID
	// never shows, and the current model is never borrowed for history.
	if _, ok := tt.Find("Historian 7B"); !ok {
		t.Fatalf("history turn did not resolve its catalog name: %#v", tt.Texts())
	}
	if tt.HasText("historian-7b") {
		t.Fatal("raw history model ID leaked into the header")
	}
	if _, ok := tt.Find("↓120"); !ok {
		t.Fatal("history turn output tokens missing")
	}
	if _, ok := tt.Find(started.Format("15:04")); !ok {
		t.Fatal("history turn time missing")
	}
	// The user row carries the commercial chrome: You plus the time.
	if _, ok := tt.Find(text("p29UserLabel")); !ok {
		t.Fatal("user header missing the localized You label")
	}
	// Action area: copy and retry, no repeated green completion.
	copyButton, ok := tt.Find(text("chatCopy"))
	if !ok {
		t.Fatal("copy action missing")
	}
	// The action row shares the body's 46px inset: it must start at the
	// same X as the user text, not at the header's left edge.
	userText, ok := tt.Find("Earlier question")
	if !ok {
		t.Fatal("user body text missing")
	}
	if delta := copyButton.X - userText.X; delta < -1 || delta > 1 {
		t.Fatalf("copy action at x=%v, user body at x=%v: action row is not aligned", copyButton.X, userText.X)
	}
	if _, ok := tt.Find(text("retry")); !ok {
		t.Fatal("retry action missing")
	}
	if tt.HasText(text("chatCompleted")) {
		t.Fatal("old green completion summary repeated under the new chrome")
	}
}

func TestP29StoppedActionStatesStayHonest(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	state := p18_2HUDState(t)
	state.chat = []chatMessage{
		{role: chatUser, status: "Complete",
			blocks: []chatBlock{p29TextBlock("Long task", "")}},
		{
			role: chatAssistant, status: "stopped",
			blocks: []chatBlock{p29TextBlock("half a thought", "")},
		},
	}
	tt := ui.NewTester(state.view, 1180, 760)
	if _, ok := tt.Find(text("chatStopped")); !ok {
		t.Fatalf("stopped state not shown: %#v", tt.Texts())
	}
	if tt.HasText(text("chatCompleted")) {
		t.Fatal("a stopped turn claimed completion")
	}
	if _, ok := tt.Find("half a thought"); !ok {
		t.Fatal("unfinished trailing text missing")
	}
	if !tt.HasText(text("p29IncompleteOutput")) {
		t.Fatal("unfinished trailing text lacks its honest label")
	}
}

func TestP29LiveMultiEpochPlainAnswer(t *testing.T) {
	// One aggregated live row whose earlier source spoke commentary and
	// whose LAST source carries a plain, normally stopped text: that text
	// is the final answer once settled.
	message := &chatMessage{role: chatAssistant, status: "Complete",
		stopReason: "stop", blocks: []chatBlock{
			{kind: chatBlockThinking, text: "plan", epoch: 0},
			{kind: chatBlockText, text: "working on it",
				phase: p29PhaseCommentary, epoch: 0},
			{kind: chatBlockText, text: "Final epoch answer.", epoch: 1},
		}}
	turn := buildP29Turn([]*chatMessage{message}, false)
	if turn.finalKind != p29FinalAnswer || turn.finalText != "Final epoch answer." {
		t.Fatalf("final=%d text=%q", turn.finalKind, turn.finalText)
	}
	prose := 0
	for _, node := range turn.nodes {
		if node.kind == p29NodeCommentary {
			prose++
		}
	}
	if prose != 1 || turn.nodes[0].kind != p29NodeActivity {
		t.Fatalf("commentary split wrong: %#v", turn.nodes)
	}
	live := buildP29Turn([]*chatMessage{{role: chatAssistant, status: "Streaming",
		blocks: message.blocks}}, true)
	if live.finalKind != p29FinalNone {
		t.Fatal("live turn promoted the trailing text too early")
	}
	// History stores the same content as separate rows with epoch 0; the
	// model must reach the identical verdict.
	first := &chatMessage{role: chatAssistant, status: "Complete",
		blocks: []chatBlock{
			{kind: chatBlockThinking, text: "plan"},
			{kind: chatBlockText, text: "working on it", phase: p29PhaseCommentary},
		}}
	second := &chatMessage{role: chatAssistant, status: "Complete",
		stopReason: "stop",
		blocks:     []chatBlock{{kind: chatBlockText, text: "Final epoch answer."}}}
	history := buildP29Turn([]*chatMessage{first, second}, false)
	if history.finalKind != p29FinalAnswer || history.finalText != turn.finalText {
		t.Fatalf("history verdict differs: %d %q", history.finalKind, history.finalText)
	}
}

func TestP29LastSourceThinkingFallbackDespiteEarlierWork(t *testing.T) {
	// The last source holds only thinking; earlier text and tools must not
	// block the commercial thinking-as-answer fallback.
	message := &chatMessage{role: chatAssistant, status: "Complete",
		stopReason: "stop", blocks: []chatBlock{
			{kind: chatBlockText, text: "mid-run prose", epoch: 0},
			{kind: chatBlockTool, epoch: 0,
				tool: toolCallView{id: "t1", name: "bash", status: toolCallCompleted}},
			{kind: chatBlockThinking, text: "the closing thought", epoch: 1},
		}}
	turn := buildP29Turn([]*chatMessage{message}, false)
	if turn.finalKind != p29FinalThinkingOnly || turn.finalText != "the closing thought" {
		t.Fatalf("final=%d text=%q", turn.finalKind, turn.finalText)
	}
	if turn.thinkingCount != 1 || turn.toolCount != 1 {
		t.Fatalf("counts: thinking=%d tool=%d, want 1/1", turn.thinkingCount, turn.toolCount)
	}
}

func TestP29StopReasonsNeverPoseAsNormalAnswers(t *testing.T) {
	// With meta present, only stop ends normally; length, tooluse and
	// pending mark trailing text unfinished instead of a confirmed answer.
	for _, reason := range []string{"length", "tooluse", "pending", "unknown-yet"} {
		message := &chatMessage{role: chatAssistant, status: "Complete",
			stopReason: reason,
			blocks:     []chatBlock{{kind: chatBlockText, text: "trailing"}}}
		turn := buildP29Turn([]*chatMessage{message}, false)
		if turn.endedNormal {
			t.Fatalf("reason %q treated as a normal stop", reason)
		}
		if turn.finalKind != p29FinalIncomplete {
			t.Fatalf("reason %q final=%d, want Incomplete", reason, turn.finalKind)
		}
	}
	message := &chatMessage{role: chatAssistant, status: "Complete",
		stopReason: "stop", blocks: []chatBlock{{kind: chatBlockText, text: "trailing"}}}
	turn := buildP29Turn([]*chatMessage{message}, false)
	if !turn.endedNormal || turn.finalKind != p29FinalAnswer {
		t.Fatalf("stop: normal=%v final=%d", turn.endedNormal, turn.finalKind)
	}
}

func TestP29SyntheticTextNeverAnswersAndNeverBlocks(t *testing.T) {
	// A history-injected error line is system prose: it stays visible in
	// the process, never becomes the answer, and does not block the
	// thinking-only fallback of its own source.
	message := &chatMessage{role: chatAssistant, status: "Complete",
		blocks: []chatBlock{
			{kind: chatBlockThinking, text: "the only thought"},
			{kind: chatBlockText, text: "Pi exited before adding response text.",
				synthetic: true},
		}}
	turn := buildP29Turn([]*chatMessage{message}, false)
	if turn.finalKind != p29FinalThinkingOnly || turn.finalText != "the only thought" {
		t.Fatalf("final=%d text=%q", turn.finalKind, turn.finalText)
	}
	if turn.thinkingCount != 1 {
		t.Fatalf("counts wrong: thinking=%d", turn.thinkingCount)
	}
	// Review P2-1/P2-2: a completed turn's placeholder leaves the process
	// entirely — the settled note below replaces it — and it never
	// becomes a notice line (only failed or cancelled turns do).
	if len(turn.nodes) != 0 {
		t.Fatalf("synthetic placeholder still in the process: %#v", turn.nodes)
	}
	if len(turn.noticeLines) != 0 {
		t.Fatalf("completed placeholder became a notice: %#v", turn.noticeLines)
	}
}

func TestP29SettleCoordinationAnimatesWrapperHeight(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	state := p29LiveCapsuleState(t)
	state.preferences.Motion = string(motionStandard)
	state.chat[1].blocks = append(state.chat[1].blocks,
		p29TextBlock("streamed prose...", ""))
	tt := ui.NewTester(state.view, 1180, 760)
	tt.Advance(400 * time.Millisecond)
	state.chat[1].blocks[len(state.chat[1].blocks)-1].tool =
		state.chat[1].blocks[1].tool
	// Settle the turn: the tool finishes and the streamed prose becomes
	// the answer, so the process folds while the answer appears.
	state.chat[1].blocks[1] = p29ToolBlock("call-1", "read", toolCallCompleted)
	state.chat[1].status = "Complete"
	state.chat[1].stopReason = "stop"
	state.run = runCompleted
	tt.Frame()
	st := state.chatView.p29["p29-turn-0"]
	if st == nil || st.coord == nil {
		t.Fatalf("settle did not arm the shared-parent coordination: %#v", st)
	}
	heightBefore := st.revealH
	if heightBefore <= 0 {
		t.Fatalf("wrapper height was never measured: %v", heightBefore)
	}
	tt.Advance(100 * time.Millisecond) // still inside the 150 ms delay
	if st.revealH < heightBefore-1 {
		t.Fatalf("wrapper moved during its delay: %v → %v", heightBefore, st.revealH)
	}
	tt.Advance(250 * time.Millisecond) // mid-curve
	hMid := st.revealH
	tt.Advance(500 * time.Millisecond) // settled
	tt.Frame()
	hFinal := st.revealH
	if st.coord != nil {
		t.Fatal("coordination window never closed")
	}
	if !(hFinal < hMid && hMid < heightBefore) {
		t.Fatalf("wrapper height must run between %v and %v, saw mid=%v final=%v",
			hFinal, heightBefore, hMid, hFinal)
	}
}

func TestP29ManualFoldAnimatesWrapperHeight(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	state := p29LiveCapsuleState(t)
	state.preferences.Motion = string(motionStandard)
	state.chat[1].status = "Complete"
	state.chat[1].stopReason = "stop"
	state.chat[1].blocks[1] = p29ToolBlock("call-1", "read", toolCallCompleted)
	state.chat[1].blocks = append(state.chat[1].blocks,
		p29TextBlock("The read finished.", ""))
	state.run = runCompleted
	tt := ui.NewTester(state.view, 1180, 760)
	summary := p29ProcessTitleParts(1, 1, false, "0s")
	if err := tt.Click(summary); err != nil {
		t.Fatal(err)
	}
	tt.Advance(700 * time.Millisecond)
	st := state.chatView.p29["p29-turn-0"]
	open := st.revealH
	if err := tt.Click(summary); err != nil {
		t.Fatal(err)
	}
	// One frame retargets the channel to the content's new natural
	// height; the next window samples the curve in flight.
	tt.Frame()
	tt.Advance(210 * time.Millisecond)
	mid := st.revealH
	tt.Advance(500 * time.Millisecond)
	tt.Frame()
	closed := st.revealH
	if st.coord != nil {
		t.Fatal("manual fold window never closed")
	}
	if !(closed < mid && mid < open) {
		t.Fatalf("manual fold must animate the container: open=%v mid=%v closed=%v",
			open, mid, closed)
	}
	capsule := textArgs("hudTokensPerSecond", map[string]any{"speed": "51.2"})
	if _, ok := tt.Find(capsule); !ok {
		t.Fatal("capsule must survive the fold animation")
	}
}

// TestP30JAgentTurnCardsAndByopHasNone checks P30 1.1 as geometry. The J Agent
// preset wraps each side of a turn in the dark glass card, so the turn's
// content sits inside 12×16 of card padding on a 0.5 edge; the BYOP preset
// keeps the commercial card-less layout, so the same content fills the column.
// The turn is rendered twice in one tester and only the preset changes, so the
// measured difference is the card and nothing else.
func TestP30JAgentTurnCardsAndByopHasNone(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	state := layoutTestState(t)
	state.chat = []chatMessage{
		{role: chatUser, status: "Complete"},
		{role: chatAssistant, status: "Complete",
			blocks: []chatBlock{p29TextBlock("Answer.", p29PhaseFinalAnswer)}},
	}
	tt := ui.NewTester(state.view, 1180, 1400)

	// Without a card each header fills the content column, and the two sides
	// of the turn are separated by the turn's own gaps alone.
	byop := themePaletteForAppearance(appearancePresetBYOP, false, false, false)
	user, answer := p30TurnHeaders(t, tt)
	if absFloat32(user.W-byop.chatContentWidth) > 1 ||
		absFloat32(answer.W-byop.chatContentWidth) > 1 {
		t.Fatalf("byop headers are %.1f/%.1f DIP wide, want the whole %.0f DIP column: no card may inset them",
			user.W, answer.W, byop.chatContentWidth)
	}
	byopGap := answer.Y - (user.Y + user.H)

	// With the cards, each header loses one padding and one edge on both
	// sides, and the two cards put their own bottom and top padding between
	// the headers.
	state.preferences.AppearancePreset = string(appearancePresetJAgent)
	p30Settle(tt)
	jagent := themePaletteForAppearance(appearancePresetJAgent, true, false, false)
	user, answer = p30TurnHeaders(t, tt)
	wantWidth := jagent.chatContentWidth - 2*p29TurnCardPaddingX - 1 // two 0.5 edges
	if absFloat32(user.W-wantWidth) > 1 || absFloat32(answer.W-wantWidth) > 1 {
		t.Fatalf("jagent headers are %.1f/%.1f DIP wide, want %.0f: each card insets its content by %.0f on both sides",
			user.W, answer.W, wantWidth, p29TurnCardPaddingX)
	}
	jagentGap := answer.Y - (user.Y + user.H)
	// P32 §5.4: the bubble between the two headers follows the preset chat
	// typography, so the expected extra gap also includes the height delta of
	// that single-line bubble between BYOP (14/1.5) and J Agent (15/1.72).
	bubbleDelta := jagent.chatFont*jagent.chatLineHeight -
		byop.chatFont*byop.chatLineHeight
	wantExtra := 2*(p29TurnCardPaddingY+0.5) + bubbleDelta // facing paddings, edges, typography delta
	if diff := absFloat32((jagentGap - byopGap) - wantExtra); diff > 1 {
		t.Fatalf("the cards added %.1f DIP between the turn's two headers, want %.1f",
			jagentGap-byopGap, wantExtra)
	}
}

// p30TurnHeaders returns the two header rows of one turn: the user row,
// labelled with the localized "You", and the assistant row, labelled with the
// HUD text that carries the model name — "GPT-5" is the fixture model.
func p30TurnHeaders(t *testing.T, tt *ui.Tester) (ui.Rect, ui.Rect) {
	t.Helper()
	user, ok := tt.Find(text("p29UserLabel"))
	if !ok {
		t.Fatalf("user header missing: %#v", tt.Texts())
	}
	answer, ok := tt.Find("GPT-5")
	if !ok {
		t.Fatalf("assistant header missing: %#v", tt.Texts())
	}
	return user, answer
}
