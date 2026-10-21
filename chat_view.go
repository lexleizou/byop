package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/components/chat"
	lu "github.com/oneo-me/onevra/source/icons/lucide"
)

// chatPresentation retains Onevra presentation state across frames. Pi owns
// the conversation data; these values only remember scroll and disclosure.
type chatPresentation struct {
	conversation chat.ConversationState
	layout       chat.FloatingConversationState
	modelPicker  chat.PickerState
	effortPicker chat.EffortState
	markdown     markdown.Options
	turns        map[int]*chat.TurnState
	groups       map[string]*chat.GroupState
	activities   map[string]*activityPresentation

	thinkingPending     string
	thinkingCommitTimer *time.Timer
}

type activityPresentation struct {
	state  chat.ActivityState
	synced bool
	last   bool
}

func (p *chatPresentation) turn(index int) *chat.TurnState {
	if p.turns == nil {
		p.turns = map[int]*chat.TurnState{}
	}
	if p.turns[index] == nil {
		p.turns[index] = &chat.TurnState{}
	}
	return p.turns[index]
}

func (p *chatPresentation) group(key string) *chat.GroupState {
	if p.groups == nil {
		p.groups = map[string]*chat.GroupState{}
	}
	if p.groups[key] == nil {
		p.groups[key] = &chat.GroupState{}
	}
	return p.groups[key]
}

// activity follows BYOP's lifecycle default (running open, settled closed)
// whenever it changes, and otherwise keeps the user's own choice.
func (p *chatPresentation) activity(key string, expanded bool) *chat.ActivityState {
	if p.activities == nil {
		p.activities = map[string]*activityPresentation{}
	}
	a := p.activities[key]
	if a == nil {
		a = &activityPresentation{}
		p.activities[key] = a
	}
	if !a.synced || a.last != expanded {
		a.state.Expanded = expanded
		a.synced, a.last = true, expanded
	}
	return &a.state
}

// turnPlanStep is either one text block or a run of consecutive tools.
type turnPlanStep struct {
	text  int
	tools []int
}

type turnPlan struct {
	steps []turnPlanStep
	final int
}

// planAssistantTurn keeps Pi's block order. A settled turn's trailing text
// becomes the final answer; while live, every text block stays a step.
func planAssistantTurn(message *chatMessage, live bool) turnPlan {
	plan := turnPlan{final: -1}
	last := len(message.blocks) - 1
	if !live && last >= 0 && message.blocks[last].kind == chatBlockText && strings.TrimSpace(message.blocks[last].text) != "" {
		plan.final = last
	}
	for i := range message.blocks {
		block := &message.blocks[i]
		switch block.kind {
		case chatBlockText:
			if i == plan.final || strings.TrimSpace(block.text) == "" {
				continue
			}
			plan.steps = append(plan.steps, turnPlanStep{text: i})
		case chatBlockTool:
			if n := len(plan.steps); n > 0 && plan.steps[n-1].tools != nil {
				plan.steps[n-1].tools = append(plan.steps[n-1].tools, i)
				continue
			}
			plan.steps = append(plan.steps, turnPlanStep{text: -1, tools: []int{i}})
		}
	}
	return plan
}

func assistantTurnStatus(message *chatMessage, live bool) chat.Status {
	if live {
		return chat.Running
	}
	switch strings.ToLower(message.status) {
	case "stopped":
		return chat.Cancelled
	case "interrupted", "failed":
		return chat.Failed
	default:
		return chat.Completed
	}
}

func activityStatus(status toolCallStatus) chat.Status {
	switch status {
	case toolCallRunning:
		return chat.Running
	case toolCallFailed, toolCallInterrupted:
		return chat.Failed
	case toolCallCancelled:
		return chat.Cancelled
	default:
		return chat.Completed
	}
}

func (s *appState) runActive() bool {
	return s.run == runSubmitting || s.run == runStreaming || s.run == runStopping
}

func (s *appState) chatPage(c *ui.Context, _ palette) {
	if s.runActive() {
		c.After(time.Second)
	}
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).MinWidth(0).Children(func() {
		chat.FloatingConversation(c, &s.chatView.layout, &s.chatView.conversation, "Back to latest",
			func(c *ui.Context) { s.transcript(c) },
			func(c *ui.Context) { s.chatComposer(c) })
	})
}

func (s *appState) transcript(c *ui.Context) {
	if len(s.chat) == 0 {
		s.emptyTranscript(c)
		return
	}
	lastAssistant := -1
	for i := range s.chat {
		if s.chat[i].role == chatAssistant {
			lastAssistant = i
		}
	}
	for i := 0; i < len(s.chat); i++ {
		start := i
		message := &s.chat[i]
		var prompt *chatMessage
		if message.role == chatUser {
			prompt = message
			message = nil
			if i+1 < len(s.chat) && s.chat[i+1].role == chatAssistant {
				i++
				message = &s.chat[i]
			}
		}
		s.turnView(c, start, prompt, message, message != nil && i == lastAssistant && s.runActive())
	}
}

func (s *appState) emptyTranscript(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).FillWidth().Padding(96, 0, 0, 0).AlignItems(ui.Center).Gap(10).Children(func() {
		components.Text(c, "What should Pi work on?").FontSize(fontTitle).FontWeight(600).TextColor(t.Text)
		components.Text(c, s.piDiagnostic()).FontSize(13.5).TextColor(t.TextMuted)
	})
}

func (s *appState) turnView(c *ui.Context, index int, prompt, message *chatMessage, live bool) {
	options := chat.TurnOptions{ID: fmt.Sprintf("turn-%d", index), Status: chat.Completed, HoldOpen: !s.chatView.conversation.AtLatest()}
	if prompt != nil {
		text := messageText(prompt)
		options.Prompt = func(c *ui.Context) { components.Text(c, text).Selectable() }
	}
	if message != nil {
		options.Status = assistantTurnStatus(message, live)
		elapsed := turnElapsed(c, message, live)
		options.Summary = "Worked for " + elapsed
		options.Working = "Working · " + elapsed
		plan := planAssistantTurn(message, live)
		for stepIndex, step := range plan.steps {
			isLast := stepIndex == len(plan.steps)-1
			options.Steps = append(options.Steps, s.turnStep(options.ID, message, step, live && isLast))
		}
		if plan.final >= 0 {
			text := message.blocks[plan.final].text
			options.Final = func(c *ui.Context) { markdown.View(c, text, &s.chatView.markdown).FillWidth() }
		}
		if options.Status == chat.Failed || options.Status == chat.Cancelled {
			label, failed := turnEndLabel(message.status), options.Status == chat.Failed
			options.Footer = func(c *ui.Context) { turnFooter(c, label+" · "+elapsed, failed) }
		}
	}
	chat.Turn(c, s.chatView.turn(index), options)
}

func (s *appState) turnStep(turnID string, message *chatMessage, step turnPlanStep, live bool) chat.Step {
	if step.tools == nil {
		text := message.blocks[step.text].text
		streaming := live
		return chat.Step{ID: fmt.Sprintf("text-%d", step.text), Message: func(c *ui.Context) {
			if streaming {
				markdown.Stream(c, text, &s.chatView.markdown).FillWidth()
			} else {
				markdown.View(c, text, &s.chatView.markdown).FillWidth()
			}
		}}
	}
	activities := make([]chat.Activity, 0, len(step.tools))
	for _, blockIndex := range step.tools {
		tool := &message.blocks[blockIndex].tool
		activities = append(activities, s.toolActivity(turnID, tool))
	}
	groupID := fmt.Sprintf("tools-%d", step.tools[0])
	return chat.Step{
		ID:         groupID,
		Title:      activityGroupTitle(activities, live),
		Activities: activities,
		Group:      s.chatView.group(turnID + "/" + groupID),
	}
}

func (s *appState) toolActivity(turnID string, tool *toolCallView) chat.Activity {
	view := *tool
	return chat.Activity{
		ID:        "call-" + view.id,
		Action:    view.name,
		Detail:    summarizeLine(view.arguments, 96),
		Icon:      toolCallIcon(view.name),
		Status:    activityStatus(view.status),
		Mono:      true,
		MaxHeight: 240,
		State:     s.chatView.activity(turnID+"/"+view.id, view.expanded),
		Content:   func(c *ui.Context) { toolActivityContent(c, view) },
	}
}

func toolActivityContent(c *ui.Context, tool toolCallView) {
	if args := strings.TrimSpace(tool.arguments); args != "" && (strings.Contains(args, "\n") || len([]rune(args)) > 96) {
		components.Text(c, args).Selectable()
	}
	if output := fallback(tool.result, tool.partial); output != "" {
		components.Text(c, output).Selectable()
	}
	note := toolCallStatusLabel(tool.status)
	switch tool.status {
	case toolCallRunning:
		note = "Waiting for tool output…"
		if tool.partial != "" {
			note = ""
		}
	case toolCallInterrupted:
		note = "Interrupted when Pi disconnected"
	}
	if tool.durationMS > 0 {
		note = strings.TrimPrefix(note+" · "+formatToolDuration(tool.durationMS), " · ")
	}
	if note != "" {
		components.Text(c, note).TextColor(c.Theme().TextMuted)
	}
}

func activityGroupTitle(activities []chat.Activity, live bool) string {
	if len(activities) == 0 {
		return ""
	}
	if live {
		latest := activities[len(activities)-1]
		return strings.TrimSpace(latest.Action + " " + latest.Detail)
	}
	if len(activities) == 1 {
		return "Ran 1 tool"
	}
	return fmt.Sprintf("Ran %d tools", len(activities))
}

func turnElapsed(c *ui.Context, message *chatMessage, live bool) string {
	if message.startedAt.IsZero() {
		return "0m 0s"
	}
	end := message.endedAt
	if live || end.IsZero() {
		end = c.Now()
	}
	seconds := max(0, int(end.Sub(message.startedAt).Seconds()))
	return fmt.Sprintf("%dm %ds", seconds/60, seconds%60)
}

func turnEndLabel(status string) string {
	switch strings.ToLower(status) {
	case "stopped":
		return "Stopped"
	case "interrupted":
		return "Interrupted"
	default:
		return "Failed"
	}
}

func turnFooter(c *ui.Context, label string, failed bool) {
	t := c.Theme()
	color, symbol := t.TextMuted, lu.CircleStop
	if failed {
		color, symbol = t.Danger, lu.X
	}
	ui.Row(c).Height(22).AlignItems(ui.Center).Gap(6).Children(func() {
		components.Icon(c, symbol).Size(12, 12).TextColor(color)
		components.Text(c, label).FontSize(13.5).FontWeight(500).TextColor(color)
	})
}

func (s *appState) chatComposer(c *ui.Context) {
	t := c.Theme()
	visible := s.followUpSuggestionVisible()
	style := s.currentFollowUpStyle()
	placeholder := composerPlaceholder(s.piConn)
	if visible && style == followUpStyleGhost {
		placeholder = s.followUpSuggestion
	}
	cardVisible := visible && style == followUpStyleCard
	ui.Column(c).FillWidth().MinWidth(0).Children(func() {
		if cardVisible {
			s.followUpSuggestionCard(c)
		}
		result := chat.GlassComposer(c, &s.chatView.layout, &s.composerDraft, chat.ComposerOptions{
			Label:        "Message",
			Placeholder:  placeholder,
			SendLabel:    "Send",
			StopLabel:    "Stop",
			Running:      s.runActive() && s.run != runStopping,
			SendDisabled: s.piConn != piConnected,
			Accessory: func(c *ui.Context) {
				s.composerChoices(c)
			},
			Trailing: func(c *ui.Context) {
				if chat.IconAction(c, "Attachments are not connected yet", lu.Paperclip).Clicked() {
					s.showNotice("Attachments remain outside the current RPC boundary.")
				}
			},
			Footer: func(c *ui.Context) {
				chat.Chip(c, s.piCompactLabel(), chat.ChipOptions{Icon: lu.Terminal, ReadOnly: true})
				ui.Spacer(c)
				switch {
				case visible && style == followUpStyleGhost:
					components.Text(c, "Tab to use · Esc to dismiss").FontSize(fontSmall).TextColor(t.TextMuted)
				case s.run == runStopping:
					chat.Shimmer(c, "Stopping Pi", 12.5, t.TextMuted, t.Text)
				case s.retrying:
					chat.Shimmer(c, s.retryStatusLabel(c), 12.5, t.TextMuted, t.Text)
				case s.compacting:
					chat.Shimmer(c, "Compacting context", 12.5, t.TextMuted, t.Text)
				case s.runActive():
					chat.Shimmer(c, "Pi is working", 12.5, t.TextMuted, t.Text)
				}
			},
		})
		result.Root.Key("chat-composer")
		result.Input.FontSize(fontBody)
		if !cardVisible {
			s.applyPendingFollowUpAdoption(result.Input)
		}
		if visible && result.Input.Focused() {
			composing := result.Input.Composing()
			result.Input.HandleInput(func(ev ui.InputEvent) bool {
				return s.handleFollowUpComposerInput(ev, composing)
			})
		}
		if result.Send {
			s.sendPrompt()
		}
		if result.Stop {
			s.stopPrompt()
		}
	})
}

func (s *appState) composerChoices(c *ui.Context) {
	readOnly := s.runActive() || s.compacting || s.piSessionBusy
	if (s.runActive() || s.compacting) && !s.piSessionBusy {
		s.cancelThinkingPending()
	}

	modelLabel := piModelDisplayName(s.piModelName, s.piModel)
	if len(s.piModels) == 0 {
		s.chatView.modelPicker.Open = false
		if s.piModel != "" {
			chat.Chip(c, modelLabel, chat.ChipOptions{Icon: lu.Sparkles, ReadOnly: true})
		}
	} else if readOnly {
		s.chatView.modelPicker.Open = false
		chat.Chip(c, modelLabel, chat.ChipOptions{Icon: lu.Sparkles, ReadOnly: true})
	} else {
		options, choices := buildPiModelChoices(s.piModels)
		result := chat.Picker(c.Key("model-picker"), &s.chatView.modelPicker, chat.PickerOptions{
			Label:       modelLabel,
			Icon:        lu.Sparkles,
			Options:     options,
			Selected:    piModelChoiceID(s.piModelProvider, s.piModelID),
			Searchable:  5,
			SearchLabel: "Search models",
			EmptyText:   "No models",
			Width:       300,
		})
		if result.Chosen != "" {
			choice, ok := choices[result.Chosen]
			if ok && (choice.model.Provider != s.piModelProvider || choice.model.ID != s.piModelID) {
				s.selectModel(choice.model.Provider, choice.model.ID)
			}
		}
	}

	levels := buildPiThinkingOptions(s.piThinkingLevels)
	if !reasoningLevelsVisible(s.piThinkingLevels) || len(levels) == 0 {
		s.chatView.effortPicker.Open = false
		s.cancelThinkingPending()
		return
	}
	s.normalizeThinkingPending(levels)
	selected := s.piThinkingLevel
	if s.chatView.thinkingPending != "" {
		selected = s.chatView.thinkingPending
	} else if s.thinkingCommitInFlight && s.thinkingCommitLevel != "" {
		selected = s.thinkingCommitLevel
	}
	if selected == "" {
		selected = levels[0].ID
	}
	if readOnly {
		s.chatView.effortPicker.Open = false
		label := thinkingLevelLabel(selected)
		chip := chat.Chip(c, label, chat.ChipOptions{Icon: lu.Brain, ReadOnly: true})
		components.Tooltip(c, chip, "Thinking · "+label)
		return
	}

	wasOpen := s.chatView.effortPicker.Open
	result := chat.EffortPicker(c.Key("thinking-picker"), &s.chatView.effortPicker, chat.EffortPickerOptions{
		Label:    "Thinking",
		Tooltip:  "Thinking · " + thinkingLevelLabel(selected),
		Icon:     lu.Brain,
		Levels:   levels,
		Selected: selected,
		Off:      levels[0].ID == "off",
	})
	if result.Chosen != "" {
		s.setThinkingPending(result.Chosen)
	}
	if wasOpen && !s.chatView.effortPicker.Open {
		s.commitThinkingPending()
	}
}

func (s *appState) setThinkingPending(level string) {
	level = strings.TrimSpace(level)
	if level == "" {
		return
	}
	if level == s.chatView.thinkingPending {
		return
	}
	s.stopThinkingCommitTimer()
	if s.thinkingCommitInFlight {
		if level == s.thinkingCommitLevel {
			s.chatView.thinkingPending = ""
			return
		}
		s.chatView.thinkingPending = level
		s.scheduleThinkingPendingCommit()
		return
	}
	if level == s.piThinkingLevel {
		s.chatView.thinkingPending = ""
		return
	}
	s.chatView.thinkingPending = level
	s.scheduleThinkingPendingCommit()
}

func (s *appState) scheduleThinkingPendingCommit() {
	level := s.chatView.thinkingPending
	if level == "" {
		return
	}
	s.stopThinkingCommitTimer()
	seq := s.thinkingCommitSeq
	s.chatView.thinkingCommitTimer = time.AfterFunc(thinkingCommitDelay, func() {
		s.postUI(func() {
			if seq != s.thinkingCommitSeq || s.chatView.thinkingPending != level {
				return
			}
			s.chatView.thinkingCommitTimer = nil
			if s.thinkingCommitInFlight {
				return
			}
			s.commitThinkingPending()
		})
	})
}

func (s *appState) commitThinkingPending() bool {
	level := s.chatView.thinkingPending
	if !shouldCommitThinking(level, s.piThinkingLevel,
		!s.runActive() && !s.compacting && !s.piSessionBusy && !s.thinkingCommitInFlight) {
		return false
	}
	s.stopThinkingCommitTimer()
	return s.selectThinkingLevel(level)
}

func (s *appState) normalizeThinkingPending(levels []chat.Option) {
	pending := s.chatView.thinkingPending
	if pending == "" {
		return
	}
	for _, level := range levels {
		if level.ID == pending {
			return
		}
	}
	s.cancelThinkingPending()
}

func (s *appState) cancelThinkingPending() {
	s.stopThinkingCommitTimer()
	s.chatView.thinkingPending = ""
}

func (s *appState) revertThinkingPending(level string) {
	s.stopThinkingCommitTimer()
	if s.chatView.thinkingPending == level {
		s.chatView.thinkingPending = ""
	}
}

func (s *appState) dropThinkingPending() {
	s.stopThinkingCommitTimer()
	s.chatView.thinkingPending = ""
}

func (s *appState) stopThinkingCommitTimer() {
	s.thinkingCommitSeq++
	if s.chatView.thinkingCommitTimer != nil {
		s.chatView.thinkingCommitTimer.Stop()
		s.chatView.thinkingCommitTimer = nil
	}
}

func (s *appState) retryStatusLabel(c *ui.Context) string {
	label := "Retrying"
	if s.retryAttempt > 0 {
		label += fmt.Sprintf(" %d", s.retryAttempt)
		if s.retryMax > 0 {
			label += fmt.Sprintf("/%d", s.retryMax)
		}
	}
	if s.retryDelayMS <= 0 || s.retryStartedAt.IsZero() {
		return label
	}
	remaining := s.retryDelayMS - c.Now().Sub(s.retryStartedAt).Milliseconds()
	if remaining < 0 {
		remaining = 0
	}
	return fmt.Sprintf("%s · %ds", label, (remaining+999)/1000)
}

func messageText(message *chatMessage) string {
	var parts []string
	for i := range message.blocks {
		block := &message.blocks[i]
		if block.kind == chatBlockText && strings.TrimSpace(block.text) != "" {
			parts = append(parts, block.text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func summarizeLine(text string, limit int) string {
	line := oneLine(text)
	runes := []rune(line)
	if len(runes) <= limit {
		return line
	}
	return string(runes[:limit]) + "…"
}

func toolCallIcon(name string) func() *ui.SVG {
	switch strings.ToLower(name) {
	case "bash", "shell", "terminal":
		return lu.Terminal
	case "read", "file":
		return lu.FileText
	case "write", "edit":
		return lu.FilePen
	case "grep", "find", "search", "ls":
		return lu.Search
	case "git":
		return lu.GitBranch
	default:
		return lu.Wrench
	}
}

func composerPlaceholder(conn piConnectionState) string {
	switch conn {
	case piConnected:
		return "Ask Pi. Enter sends, Shift+Enter adds a new line."
	case piConnecting:
		return "Connecting to Pi…"
	default:
		return "Pi CLI is not connected"
	}
}
