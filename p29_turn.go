package main

import (
	"fmt"
	"strings"

	"github.com/oneo-me/onevra/source/components/chat"
)

// P29 turn model: the commercial conversation process aggregated from the
// assistant messages that answer one real user prompt. Pi stays the data
// owner; this file only re-shapes the ordered blocks A's protocol keeps, so
// p29_view.go can render the verified 0.21.45 behaviour: consecutive
// thinking/tool blocks form one activity, commentary text splits activities,
// and the final answer sits below the process, never inside it. The model is
// locale-free on purpose; the view localises the counts and labels.
//
// Source identity matters for the no-phase rules. One live chatMessage row
// aggregates several assistant source messages, and block.epoch is what
// tells them apart; restored history keeps source messages apart by row and
// may carry epoch 0 everywhere. A block's source is therefore the pair
// (message position, epoch). The trailing-text answer, the thinking-only
// fallback and the phase suppression all read the LAST source, never the
// whole turn, so an earlier source's commentary cannot block a later plain
// answer.

// p29ItemKind separates the two block kinds an activity can hold.
type p29ItemKind uint8

const (
	p29ItemThinking p29ItemKind = iota
	p29ItemTool
)

// p29Item is one thinking or tool block, keeping the identity the protocol
// updates it by (a tool stays the same item across start/update/end).
type p29Item struct {
	id     string
	source string
	kind   p29ItemKind
	// text carries the thinking body for thinking items.
	text string
	tool toolCallView
}

// p29NodeKind separates timeline nodes.
type p29NodeKind uint8

const (
	p29NodeActivity p29NodeKind = iota
	// p29NodeCommentary is visible assistant prose that splits activities.
	// It stays Markdown, per the commercial behaviour.
	p29NodeCommentary
)

// p29Node is one timeline node: an activity run or a commentary block.
type p29Node struct {
	id    string
	kind  p29NodeKind
	items []p29Item
	text  string
	// synthetic marks a commentary node that is system prose (a
	// placeholder or a history-injected error), not model text.
	synthetic bool
}

// p29FinalKind says what sits below the process, and how the turn got there.
// Error and stop outcomes never pretend to be a success.
type p29FinalKind uint8

const (
	// p29FinalNone: nothing settled below the process (a live turn, or a
	// stopped one whose status line carries the outcome).
	p29FinalNone p29FinalKind = iota
	// p29FinalAnswer: confirmed text — a final_answer phase block, or a
	// settled no-phase turn's trailing text after a normal stop.
	p29FinalAnswer
	// p29FinalIncomplete: the trailing text of a turn that did not end
	// normally (stop, error, length, tool-use, pending), labelled
	// unfinished output, never the answer.
	p29FinalIncomplete
	// p29FinalThinkingOnly: the LAST source message ended normally with
	// thinking and nothing else; that thinking shows as the body with an
	// explicit note.
	p29FinalThinkingOnly
	// p29FinalNoText: the turn ended normally without any answer text.
	p29FinalNoText
)

// The optional block phases A's contract carries; an empty phase means the
// source message sent none and the honest fallback rules decide instead.
const (
	p29PhaseCommentary  = "commentary"
	p29PhaseFinalAnswer = "final_answer"
)

// p29Turn is one aggregated assistant turn ready for presentation.
// thinkingCount counts non-empty thinking blocks, toolCount counts unique
// tool call IDs, and the elapsed time stays the turn's own start.
// endedNormal is strict: with meta present only stop counts, so length,
// tooluse and pending never let trailing text pose as the final answer.
type p29Turn struct {
	nodes     []p29Node
	finalKind p29FinalKind
	finalText string
	// noticeLines carries the app-injected reason texts (a failed run's
	// 429, a stop or exit notice). A failed or cancelled turn shows them
	// under the process, outside the fold, so the reason never hides
	// inside the collapsed activity list or duplicates the note below a
	// completed turn.
	noticeLines   []string
	thinkingCount int
	toolCount     int
	endedNormal   bool
	status        chat.Status
	live          bool
}

// p29Flat is one block while the turn is still a flat, ordered list.
// destination is decided per block; synthetic blocks (system placeholders
// and history-injected error text) never count as model text for any
// final-answer rule, though they stay visible in the process.
type p29Flat struct {
	kind        p29ItemKind
	isText      bool
	id          string
	text        string
	tool        toolCallView
	phase       string
	synthetic   bool
	source      string
	destination uint8 // 0 process, 1 commentary, 2 final
}

const (
	p29DestProcess uint8 = iota
	p29DestCommentary
	p29DestFinal
)

// buildP29Turn aggregates the assistant messages answering one prompt, in
// receive order. Tool results never start a turn of their own: they join the
// activity they belong to.
func buildP29Turn(assistants []*chatMessage, live bool) p29Turn {
	turn := p29Turn{live: live}
	turn.status, turn.endedNormal = p29TurnStatus(assistants, live)
	flats, sourceHasPhase := flattenP29Blocks(assistants)
	collectP29Notices(flats, &turn)
	classifyP29Texts(flats, &turn, sourceHasPhase)
	countP29ActivityItems(flats, &turn)
	buildP29Nodes(flats, &turn)
	placeP29Final(flats, &turn)
	return turn
}

// p29BlockSource names the source message a block belongs to: the row
// position plus the block's epoch.
func p29BlockSource(messageIndex, epoch int) string {
	return fmt.Sprintf("%d/%d", messageIndex, epoch)
}

// flattenP29Blocks copies every block of the turn into one ordered list and
// records which sources spoke phases. A message that predates ordered
// thinking blocks, or a restore that lost them, still has the legacy
// single-line thinking: it enters once, at the head of that message, and
// never alongside real thinking blocks.
func flattenP29Blocks(assistants []*chatMessage) ([]p29Flat, map[string]bool) {
	var flats []p29Flat
	sourceHasPhase := map[string]bool{}
	for m, message := range assistants {
		if message == nil {
			continue
		}
		messageStart := len(flats)
		messageEpoch := 0
		if len(message.blocks) > 0 {
			messageEpoch = message.blocks[0].epoch
		}
		hasThinking := false
		for _, block := range message.blocks {
			source := p29BlockSource(m, block.epoch)
			if block.phase != "" {
				sourceHasPhase[source] = true
			}
			if block.kind == chatBlockThinking {
				hasThinking = true
			}
		}
		for b := range message.blocks {
			block := &message.blocks[b]
			source := p29BlockSource(m, block.epoch)
			switch block.kind {
			case chatBlockThinking:
				if strings.TrimSpace(block.text) == "" {
					continue
				}
				flats = append(flats, p29Flat{
					kind:      p29ItemThinking,
					id:        fmt.Sprintf("m%d-b%d-think", m, b),
					text:      block.text,
					synthetic: block.synthetic,
					source:    source,
				})
			case chatBlockText:
				if strings.TrimSpace(block.text) == "" {
					continue
				}
				flats = append(flats, p29Flat{
					kind:      p29ItemThinking, // kind unused for text
					isText:    true,
					id:        fmt.Sprintf("m%d-b%d-text", m, b),
					text:      block.text,
					phase:     block.phase,
					synthetic: block.synthetic,
					source:    source,
				})
			case chatBlockTool:
				id := strings.TrimSpace(block.tool.id)
				if id == "" {
					id = fmt.Sprintf("m%d-b%d", m, b)
				}
				flats = append(flats, p29Flat{
					kind:      p29ItemTool,
					id:        "call-" + id,
					tool:      block.tool,
					synthetic: block.synthetic,
					source:    source,
				})
			}
		}
		if !hasThinking && strings.TrimSpace(message.thinking) != "" {
			flats = append(flats[:messageStart], append([]p29Flat{{
				kind:   p29ItemThinking,
				id:     fmt.Sprintf("m%d-legacy-think", m),
				text:   message.thinking,
				source: p29BlockSource(m, messageEpoch),
			}}, flats[messageStart:]...)...)
		}
	}
	return flats, sourceHasPhase
}

// classifyP29Texts decides each text block's destination. A final_answer
// phase the provider confirmed goes to the body. Everything else starts as
// process prose; the no-phase fallback then promotes one trailing text: the
// LAST model text of the turn, only when its own source spoke no phases,
// nothing model-made follows it, the turn is settled, and system-synthetic
// text never poses as the answer.
func classifyP29Texts(flats []p29Flat, turn *p29Turn, sourceHasPhase map[string]bool) {
	for i := range flats {
		entry := &flats[i]
		if !entry.isText {
			entry.destination = p29DestProcess
			continue
		}
		switch {
		case entry.phase == p29PhaseFinalAnswer && !entry.synthetic:
			entry.destination = p29DestFinal
		default:
			entry.destination = p29DestCommentary
		}
	}
	last := -1
	for i := range flats {
		if flats[i].isText && !flats[i].synthetic {
			last = i
		}
	}
	if last < 0 || turn.status == chat.Running {
		return
	}
	if sourceHasPhase[flats[last].source] {
		return
	}
	if !p29NothingModelAfter(flats, last) {
		return
	}
	flats[last].destination = p29DestFinal
}

// p29NothingModelAfter reports whether no tool, thinking or model text
// follows index; synthetic blocks are system prose and never block the
// trailing rule.
func p29NothingModelAfter(flats []p29Flat, index int) bool {
	for i := index + 1; i < len(flats); i++ {
		entry := &flats[i]
		if entry.synthetic {
			continue
		}
		if entry.kind == p29ItemTool || entry.kind == p29ItemThinking || entry.isText {
			return false
		}
	}
	return true
}

// countP29ActivityItems counts non-empty thinking blocks and unique tool
// call IDs, the two numbers of the process summary. Synthetic blocks are
// not model work and count for nothing.
func countP29ActivityItems(flats []p29Flat, turn *p29Turn) {
	seenToolIDs := make(map[string]struct{})
	for i := range flats {
		entry := &flats[i]
		if entry.synthetic {
			continue
		}
		switch {
		case entry.kind == p29ItemThinking && !entry.isText:
			if strings.TrimSpace(entry.text) != "" {
				turn.thinkingCount++
			}
		case entry.kind == p29ItemTool:
			toolID := strings.TrimSpace(entry.tool.id)
			if toolID == "" {
				toolID = entry.id
			}
			if _, dup := seenToolIDs[toolID]; !dup {
				seenToolIDs[toolID] = struct{}{}
				turn.toolCount++
			}
		}
	}
}

// collectP29Notices pulls app-injected synthetic text out of the process.
// A failed or cancelled turn keeps its reason (the 429, the stop notice)
// as notice lines shown under the process by default; a completed turn's
// placeholder is replaced by the settled note instead of appearing twice.
func collectP29Notices(flats []p29Flat, turn *p29Turn) {
	if turn.status != chat.Failed && turn.status != chat.Cancelled {
		return
	}
	for i := range flats {
		entry := &flats[i]
		if entry.isText && entry.synthetic && strings.TrimSpace(entry.text) != "" {
			turn.noticeLines = append(turn.noticeLines, entry.text)
		}
	}
}

// buildP29Nodes groups the ordered list: consecutive thinking and tool items
// become one activity, commentary closes the activity and stands alone.
// Synthetic text never joins the process: collectP29Notices turned the
// failure reasons into notice lines, and a completed placeholder is
// replaced by the settled note. Node IDs count positions, which only ever
// grow while a turn streams.
func buildP29Nodes(flats []p29Flat, turn *p29Turn) {
	nodeIndex := 0
	var activity []p29Item
	flush := func() {
		if len(activity) == 0 {
			return
		}
		turn.nodes = append(turn.nodes, p29Node{
			id:    fmt.Sprintf("a%d", nodeIndex),
			kind:  p29NodeActivity,
			items: activity,
		})
		nodeIndex++
		activity = nil
	}
	for i := range flats {
		entry := &flats[i]
		switch {
		case entry.isText && entry.destination == p29DestCommentary:
			if entry.synthetic {
				continue
			}
			flush()
			turn.nodes = append(turn.nodes, p29Node{
				id:   fmt.Sprintf("c%d", nodeIndex),
				kind: p29NodeCommentary,
				text: entry.text,
			})
			nodeIndex++
		case entry.kind == p29ItemThinking && !entry.isText:
			activity = append(activity, p29Item{
				id:     entry.id,
				source: entry.source,
				kind:   p29ItemThinking,
				text:   entry.text,
			})
		case entry.kind == p29ItemTool:
			activity = append(activity, p29Item{
				id:     entry.id,
				source: entry.source,
				kind:   p29ItemTool,
				tool:   entry.tool,
			})
		}
	}
	flush()
}

// placeP29Final settles what shows below the process. A turn that did not
// end normally keeps its trailing text as unfinished output, and the
// thinking-only fallback reads the LAST source message: earlier activity
// never blocks it.
func placeP29Final(flats []p29Flat, turn *p29Turn) {
	var finalParts []string
	for i := range flats {
		if flats[i].destination == p29DestFinal {
			finalParts = append(finalParts, flats[i].text)
		}
	}
	if len(finalParts) > 0 {
		turn.finalText = strings.Join(finalParts, "\n\n")
		if turn.endedNormal {
			turn.finalKind = p29FinalAnswer
		} else {
			turn.finalKind = p29FinalIncomplete
		}
		return
	}
	switch {
	case turn.live:
		turn.finalKind = p29FinalNone
	case turn.status == chat.Failed || turn.status == chat.Cancelled:
		turn.finalKind = p29FinalNone
	case turn.endedNormal && len(flats) > 0:
		// The last source — of the last block the MODEL produced; an
		// app-injected placeholder at the end never redefines which
		// source ended the turn — holds thinking and nothing else: lift
		// its last non-empty thinking into the body once.
		lastSource := ""
		for i := len(flats) - 1; i >= 0; i-- {
			if !flats[i].synthetic {
				lastSource = flats[i].source
				break
			}
		}
		if lastSource == "" {
			turn.finalKind = p29FinalNoText
			return
		}
		if p29SourceIsThinkingOnly(flats, lastSource) {
			if n, i, ok := p29FindLastThinking(turn.nodes, lastSource); ok {
				turn.finalText = turn.nodes[n].items[i].text
				turn.finalKind = p29FinalThinkingOnly
				items := turn.nodes[n].items
				turn.nodes[n].items = append(items[:i:i], items[i+1:]...)
				if len(turn.nodes[n].items) == 0 {
					turn.nodes = append(turn.nodes[:n:n], turn.nodes[n+1:]...)
				}
				return
			}
		}
		turn.finalKind = p29FinalNoText
	default:
		turn.finalKind = p29FinalNoText
	}
}

// p29SourceIsThinkingOnly reports whether the source holds thinking and no
// model text or tool; synthetic blocks do not count against it.
func p29SourceIsThinkingOnly(flats []p29Flat, source string) bool {
	thinking := false
	for i := range flats {
		entry := &flats[i]
		if entry.source != source || entry.synthetic {
			continue
		}
		switch {
		case entry.isText:
			return false
		case entry.kind == p29ItemTool:
			return false
		case entry.kind == p29ItemThinking && strings.TrimSpace(entry.text) != "":
			thinking = true
		}
	}
	return thinking
}

// p29FindLastThinking locates the last non-empty thinking item of one
// source and returns its node and item positions.
func p29FindLastThinking(nodes []p29Node, source string) (int, int, bool) {
	for n := len(nodes) - 1; n >= 0; n-- {
		if nodes[n].kind != p29NodeActivity {
			continue
		}
		for i := len(nodes[n].items) - 1; i >= 0; i-- {
			item := nodes[n].items[i]
			if item.kind == p29ItemThinking && item.source == source &&
				strings.TrimSpace(item.text) != "" {
				return n, i, true
			}
		}
	}
	return 0, 0, false
}

// p29TurnStatus maps the aggregated turn onto the fold/label status and
// onto the strict "ended with a normal stop" question. With meta present,
// only stop counts as normal; length, tooluse and pending end the message
// without ever confirming a final answer. A meta-less legacy fixture keeps
// trusting a settled status. Errors and aborts outrank a Complete label,
// so a stopped turn can never pretend to be a success.
func p29TurnStatus(assistants []*chatMessage, live bool) (chat.Status, bool) {
	if live {
		return chat.Running, true
	}
	for i := len(assistants) - 1; i >= 0; i-- {
		message := assistants[i]
		if message == nil {
			continue
		}
		reason := strings.ToLower(strings.TrimSpace(message.stopReason))
		status := strings.ToLower(strings.TrimSpace(message.status))
		switch status {
		case "stopped":
			return chat.Cancelled, false
		case "interrupted", "failed", "error":
			return chat.Failed, false
		}
		switch reason {
		case "error":
			return chat.Failed, false
		case "aborted":
			return chat.Cancelled, false
		case "stop":
			return chat.Completed, true
		case "length", "tooluse", "tool_use", "pending":
			return chat.Completed, false
		case "":
			// No meta: the legacy compatibility path. The failure
			// statuses above already returned, so a settled row (the
			// old fixtures label them Complete or Streaming) is
			// trusted as a normal stop, exactly like the pre-P29 rule.
			return chat.Completed, true
		default:
			return chat.Completed, false
		}
	}
	return chat.Completed, true
}

// p29ActivityIsLatest reports whether node index is the turn's last
// activity: only the newest one may render as active.
func p29ActivityIsLatest(nodes []p29Node, index int) bool {
	for n := len(nodes) - 1; n >= 0; n-- {
		if nodes[n].kind == p29NodeActivity {
			return n == index
		}
	}
	return false
}

// p29ActivityStepCount counts an activity's items: the collapsed "· N steps"
// suffix needs it, and N covers thinking and tool items together.
func p29ActivityStepCount(node p29Node) int {
	return len(node.items)
}

// p29ActivityRunning reports whether any tool item still runs.
func p29ActivityRunning(node p29Node) bool {
	for _, item := range node.items {
		if item.kind == p29ItemTool && item.tool.status == toolCallRunning {
			return true
		}
	}
	return false
}

// p29ActivityLabelItem picks the item whose semantics name the activity:
// a running tool wins, else the last tool, else the thinking state.
func p29ActivityLabelItem(node p29Node) (p29Item, bool) {
	var lastTool, lastThinking p29Item
	haveTool, haveThinking := false, false
	for _, item := range node.items {
		switch item.kind {
		case p29ItemTool:
			if item.tool.status == toolCallRunning {
				return item, true
			}
			lastTool, haveTool = item, true
		case p29ItemThinking:
			lastThinking, haveThinking = item, true
		}
	}
	if haveTool {
		return lastTool, true
	}
	if haveThinking {
		return lastThinking, true
	}
	return p29Item{}, false
}

// p29ActivityIndexOf finds a node's position for the latest-activity rule
// after the view dropped the index from its call signature.
func p29ActivityIndexOf(nodes []p29Node, node *p29Node) int {
	for n := range nodes {
		if &nodes[n] == node {
			return n
		}
	}
	return -1
}
