package main

import (
	"strconv"
	"strings"
	"time"
)

// P29 ordered-block data layer.
//
// The turn row keeps one chatBlock per Pi content part, in the order the
// protocol produced them: thinking and text parts become chatBlockThinking /
// chatBlockText blocks and tool calls become chatBlockTool blocks. Each block
// records the internal identity fields epoch/contentIndex so streaming deltas
// land on exactly the right block without merging separate parts back into a
// single snapshot block, while message_end merges the final content
// idempotently onto the same blocks.
//
// The aggregate message.thinking and its timestamps stay updated for
// compatibility with the older presentation; the P29 UI reads the ordered
// blocks and never sees the same thinking text twice.

// resetAssistantOrderedIdentity starts a fresh ordered-block identity after
// the transcript was replaced (history restore, seed demo or a new user
// turn).
func (s *appState) resetAssistantOrderedIdentity() {
	s.assistantSourceEpoch = 0
	s.assistantMaxIndex = 0
	s.assistantEpochClosed = false
}

// noteAssistantContentIndex records the highest contentIndex of the current
// source message. It deliberately never starts a new epoch: Pi restarts
// indices at 0 per message and interleaves thinking_end at a lower index
// inside one message, so source boundaries come exclusively from the explicit
// assistant message_start (and from epoch close when a source message ends
// without one).
func (s *appState) noteAssistantContentIndex(index int) {
	if index < 0 {
		return
	}
	if s.assistantEpochClosed {
		s.assistantSourceEpoch++
		s.assistantMaxIndex = 0
		s.assistantEpochClosed = false
	}
	if index > s.assistantMaxIndex {
		s.assistantMaxIndex = index
	}
}

// assistantStreamTarget returns the assistant row streaming writes to,
// starting a new row when the previous one already ended, exactly like the
// old setAssistantText path did. Live rows are stamped with the model that is
// current at message start; the authoritative message_end values overwrite it
// later, so a live row is never mislabelled with a model it did not run on.
func (s *appState) assistantStreamTarget() *chatMessage {
	message := s.currentAssistant()
	if !message.endedAt.IsZero() {
		s.chat = append(s.chat, chatMessage{
			role:      chatAssistant,
			status:    text("assistantWaiting"),
			startedAt: time.Now(),
		})
		message = &s.chat[len(s.chat)-1]
	}
	if message.model == "" {
		message.model = s.piModelID
	}
	if message.provider == "" {
		message.provider = s.piModelProvider
	}
	return message
}

// eventPartText resolves the text this event carries for one content part.
// Real RPC events mark PartSet and carry the exact per-part text; synthetic
// events (older tests, fixtures) fall back to the aggregate Text.
func eventPartText(event piEvent) string {
	if event.PartSet {
		return event.PartText
	}
	return event.Text
}

// streamAssistantTextBlock writes one ordered text block for a streaming text
// event. Empty parts (text_start) keep the row status but create no block.
func (s *appState) streamAssistantTextBlock(event piEvent) {
	message := s.assistantStreamTarget()
	s.noteAssistantContentIndex(event.ContentIndex)
	message.status = "Streaming"
	part := eventPartText(event)
	if strings.TrimSpace(part) == "" {
		return
	}
	s.writeAssistantOrderedBlock(message, chatBlockText, part, "", event.ContentIndex)
}

// streamAssistantThinkingBlock writes one ordered thinking block for a
// streaming thinking event. The per-part text never mixes earlier thinking
// segments into the current one; message.thinking keeps the full aggregate.
func (s *appState) streamAssistantThinkingBlock(event piEvent) {
	message := s.assistantStreamTarget()
	s.noteAssistantContentIndex(event.ContentIndex)
	part := eventPartText(event)
	if strings.TrimSpace(part) == "" {
		return
	}
	s.writeAssistantOrderedBlock(message, chatBlockThinking, part, "", event.ContentIndex)
}

// writeAssistantOrderedBlock updates the existing block with the same kind
// and contentIndex inside the current source message's epoch (the streaming
// replacement path; Pi can deliver a late delta to an earlier index after a
// later part already streamed) and appends a new ordered block otherwise.
func (s *appState) writeAssistantOrderedBlock(message *chatMessage, kind chatBlockKind, value, phase string, index int) {
	// Walk back across every block of the current source message: Pi can
	// deliver a late delta to an earlier contentIndex after a later part
	// already streamed (thinking_end arrives after text events).
	for i := len(message.blocks) - 1; i >= 0 && message.blocks[i].epoch == s.assistantSourceEpoch; i-- {
		last := &message.blocks[i]
		if last.kind != kind || last.contentIndex != index {
			continue
		}
		last.text = value
		if phase != "" {
			last.phase = phase
		}
		return
	}
	message.blocks = append(message.blocks, chatBlock{
		kind:         kind,
		text:         value,
		phase:        phase,
		contentIndex: index,
		epoch:        s.assistantSourceEpoch,
	})
}

// beginAssistantSourceMessage starts the ordered-block epoch of one assistant
// source message on the explicit message_start boundary, and stamps the row
// with the identity the start event carries (falling back to the live model
// only for a row that is still being streamed).
func (s *appState) beginAssistantSourceMessage(event piEvent) {
	s.assistantSourceEpoch++
	s.assistantMaxIndex = 0
	s.assistantEpochClosed = false
	message := s.assistantStreamTarget()
	if event.Model != "" {
		message.model = event.Model
	}
	if event.Provider != "" {
		message.provider = event.Provider
	}
}

// mergeAssistantFinalContent applies one final assistant message: it stamps
// the message identity (stopReason/model/provider), then merges the final
// content onto the ordered blocks. Events without ordered content fall back
// to the legacy aggregate paths so synthetic events and unparseable content
// keep the old behaviour.
//
// Source boundaries when message_start is missing: a closed epoch whose
// final content differs from the incoming entries starts a new epoch (two
// consecutive final-only source messages never overwrite each other), while
// an identical final content of the same source merges idempotently in place.
// Identity-only message ends never create a row.
func (s *appState) mergeAssistantFinalContent(event piEvent) {
	empty := len(event.OrderedContent) == 0 && event.Text == "" && event.Thinking == "" && event.Error == ""
	if empty {
		// A boundary, not content: stamp identity on an existing row only.
		if n := len(s.chat); n > 0 && s.chat[n-1].role == chatAssistant {
			s.stampMessageIdentity(&s.chat[n-1], event)
			s.assistantEpochClosed = true
		}
		return
	}
	message := s.currentAssistant()
	s.stampMessageIdentity(message, event)
	if len(event.OrderedContent) > 0 {
		if s.assistantEpochClosed && !sameSourceContentSignature(message, s.assistantSourceEpoch, event.OrderedContent) {
			s.assistantSourceEpoch++
			s.assistantMaxIndex = 0
			s.assistantEpochClosed = false
		}
		s.mergeAssistantOrderedContent(message, event)
	} else {
		// Legacy aggregate path: exactly what the pre-P29 handler did.
		if event.Thinking != "" {
			s.setAssistantThinkingFallback(event.Thinking)
		}
		if event.Text != "" {
			s.setAssistantText(event.Text, "Received")
		}
	}
	s.assistantEpochClosed = true
}

// stampMessageIdentity records the true per-message identity. Message-provided
// values win; when the protocol omitted them the live current model stands in
// for a row that is still being streamed, never for a finished one.
func (s *appState) stampMessageIdentity(message *chatMessage, event piEvent) {
	if event.StopReason != "" {
		message.stopReason = event.StopReason
	}
	if event.Model != "" {
		message.model = event.Model
	} else if message.model == "" && message.endedAt.IsZero() {
		message.model = s.piModelID
	}
	if event.Provider != "" {
		message.provider = event.Provider
	} else if message.provider == "" && message.endedAt.IsZero() {
		message.provider = s.piModelProvider
	}
}

// sameSourceContentSignature compares the current epoch's blocks with the
// incoming final content (kind, contentIndex, phase and text; tool entries by
// call ID). Equal signatures mean the same source message delivered its final
// content again, which must stay idempotent rather than start a new epoch.
func sameSourceContentSignature(message *chatMessage, epoch int, entries []piContentEntry) bool {
	blockSignature := make([]string, 0, len(message.blocks))
	for i := range message.blocks {
		block := &message.blocks[i]
		if block.epoch != epoch {
			continue
		}
		switch block.kind {
		case chatBlockText:
			blockSignature = append(blockSignature, "t"+strconv.Itoa(block.contentIndex)+":"+block.phase+":"+block.text)
		case chatBlockThinking:
			blockSignature = append(blockSignature, "k"+strconv.Itoa(block.contentIndex)+":"+block.phase+":"+block.text)
		case chatBlockTool:
			blockSignature = append(blockSignature, "tool:"+block.tool.id)
		}
	}
	entrySignature := make([]string, 0, len(entries))
	for _, entry := range entries {
		switch entry.Kind {
		case "text":
			entrySignature = append(entrySignature, "t"+strconv.Itoa(entry.ContentIndex)+":"+entry.Phase+":"+entry.Text)
		case "thinking", "reasoning":
			entrySignature = append(entrySignature, "k"+strconv.Itoa(entry.ContentIndex)+":"+entry.Phase+":"+entry.Text)
		case "toolCall":
			entrySignature = append(entrySignature, "tool:"+entry.ToolCallID)
		}
	}
	if len(blockSignature) != len(entrySignature) {
		return false
	}
	if len(blockSignature) == 0 {
		return true
	}
	for i := range blockSignature {
		if blockSignature[i] != entrySignature[i] {
			return false
		}
	}
	return true
}

// mergeAssistantOrderedContent reconciles the final content of the assistant
// source message that just ended with the blocks streaming already wrote. The
// final content is authoritative: the current source message's blocks are
// rebuilt in final content order, reusing and updating the streamed block for
// the same content part (with an exact-text fallback), so partial fragments
// are replaced instead of lingering as duplicates. Unmentioned current-epoch
// text/thinking fragments are dropped; unmentioned current-epoch tool blocks
// stay, because losing a tool result would be worse than a stale row.
//
//   - text/thinking entries: match by kind+contentIndex first, then by equal
//     text, update text/phase in place, never duplicate and never concatenate
//     separate text parts into one block;
//   - toolCall entries: reuse the tool block with the same call ID (tool
//     execution events keep owning status and result) or create it here when
//     the events have not arrived yet;
//   - blocks from earlier source messages of the same turn stay untouched.
func (s *appState) mergeAssistantOrderedContent(message *chatMessage, event piEvent) {
	epoch := s.assistantSourceEpoch
	tailStart := len(message.blocks)
	for i := len(message.blocks) - 1; i >= 0; i-- {
		if message.blocks[i].epoch != epoch {
			break
		}
		tailStart = i
	}
	tailStart = max(tailStart, 0)

	// Work on snapshots so rebuilding cannot invalidate the blocks the
	// matching loops are walking.
	current := make([]chatBlock, len(message.blocks)-tailStart)
	copy(current, message.blocks[tailStart:])
	consumed := make([]bool, len(current))
	earlier := make([]chatBlock, tailStart)
	copy(earlier, message.blocks[:tailStart])

	var out []chatBlock
	hasText := false
	hasThinking := false
	for _, entry := range event.OrderedContent {
		switch entry.Kind {
		case "text", "thinking", "reasoning":
			kind := chatBlockText
			if entry.Kind != "text" {
				kind = chatBlockThinking
			}
			if kind == chatBlockText {
				hasText = true
			} else {
				hasThinking = true
			}
			pos := -1
			// 1. The streaming block for the same content part.
			for i := range current {
				if consumed[i] || current[i].kind != kind || current[i].contentIndex != entry.ContentIndex {
					continue
				}
				pos = i
				break
			}
			// 2. Idempotency net: identical text already streamed must not
			// become a second block.
			if pos < 0 {
				for i := range current {
					if consumed[i] || current[i].kind != kind || current[i].text != entry.Text {
						continue
					}
					pos = i
					break
				}
			}
			if pos >= 0 {
				current[pos].text = entry.Text
				current[pos].contentIndex = entry.ContentIndex
				if entry.Phase != "" {
					current[pos].phase = entry.Phase
				}
				consumed[pos] = true
				out = append(out, current[pos])
				continue
			}
			// 3. Content the stream never showed: a new ordered block.
			out = append(out, chatBlock{
				kind:         kind,
				text:         entry.Text,
				phase:        entry.Phase,
				contentIndex: entry.ContentIndex,
				epoch:        epoch,
			})
		case "toolCall":
			if entry.ToolCallID == "" {
				continue
			}
			pos := -1
			for i := range current {
				if current[i].kind != chatBlockTool || current[i].tool.id != entry.ToolCallID {
					continue
				}
				pos = i
				break
			}
			if pos >= 0 {
				if entry.ToolName != "" {
					current[pos].tool.name = entry.ToolName
				}
				consumed[pos] = true
				out = append(out, current[pos])
				continue
			}
			// A call ID from an earlier source message keeps its own block.
			toolFound := false
			for i := range earlier {
				if earlier[i].kind != chatBlockTool || earlier[i].tool.id != entry.ToolCallID {
					continue
				}
				if entry.ToolName != "" {
					earlier[i].tool.name = entry.ToolName
				}
				toolFound = true
				break
			}
			if toolFound {
				continue
			}
			out = append(out, chatBlock{
				kind:  chatBlockTool,
				epoch: epoch,
				tool: toolCallView{
					id:        entry.ToolCallID,
					name:      fallback(entry.ToolName, text("errorToolFallback")),
					arguments: formatToolArguments(entry.ToolArgs),
					status:    toolCallRunning,
					expanded:  true,
				},
			})
		}
	}

	// Current-epoch tool blocks the final content does not mention keep their
	// results at the end of the rebuilt row.
	for i := range current {
		if !consumed[i] && current[i].kind == chatBlockTool {
			out = append(out, current[i])
		}
	}

	message.blocks = append(earlier, out...)

	// Keep the compatibility aggregate aligned with the ordered truth of the
	// message that just ended.
	if hasThinking {
		if thinking := joinMessageThinkingBlocks(message, epoch); thinking != "" {
			message.thinking = thinking
		}
		if message.thinkingStartedAt.IsZero() {
			message.thinkingStartedAt = message.startedAt
		}
		if message.thinkingEndedAt.IsZero() {
			message.thinkingEndedAt = time.Now()
		}
	}
	if hasText {
		message.status = "Received"
	}
}

// joinMessageThinkingBlocks joins the thinking text of the current source
// message only — the compat aggregate previously held one message's snapshot,
// not the whole turn.
func joinMessageThinkingBlocks(message *chatMessage, epoch int) string {
	var parts []string
	for i := range message.blocks {
		block := &message.blocks[i]
		if block.kind != chatBlockThinking || block.epoch != epoch {
			continue
		}
		if strings.TrimSpace(block.text) == "" {
			continue
		}
		parts = append(parts, block.text)
	}
	return strings.Join(parts, "\n\n")
}
