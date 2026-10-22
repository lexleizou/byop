package main

import (
	"fmt"
	"runtime"
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

func (s *appState) chatPage(c *ui.Context, p palette) {
	if s.runActive() {
		c.After(time.Second)
	}
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).MinWidth(0).Children(func() {
		if s.piConnectionErrorActive() {
			s.piConnectionAlert(c, p)
		}
		chat.FloatingConversation(c, &s.chatView.layout, &s.chatView.conversation, text("chatBackToLatest"),
			func(c *ui.Context) { s.transcript(c) },
			func(c *ui.Context) { s.chatComposer(c) })
	})
}

func (s *appState) piConnectionAlert(c *ui.Context, p palette) {
	message := s.piConnectionErrorText()
	ui.Row(c).FillWidth().Justify(ui.Center).MarginY(8).Children(func() {
		ui.Row(c).FillWidth().MaxWidth(chat.ContentWidth).MinWidth(0).Padding(9, 12).Gap(10).Radius(8).
			Border(1, p.danger.Alpha(.35)).Background(p.danger.Alpha(.08)).
			AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon("info")).Size(16, 16).Shrink(0).TextColor(p.danger)
			ui.Text(c, message).FontSize(fontBody).LineHeight(1.4).TextColor(p.text).
				Grow(1).MinWidth(0).MaxLines(2).Tooltip(message)
			secondaryAction(c, p, "chat-pi-reconnect", text("settingsReconnect"), s.reconnectPi)
		})
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
		if end, grouped := promptToolGroupEnd(s.chat, i); grouped {
			ui.Column(c).Key(fmt.Sprintf("prompt-tool-turn-group-%d", start)).
				FillWidth().MinWidth(0).Gap(8).Children(func() {
				s.turnView(c, start, &s.chat[start], nil, false)
				for j := start + 1; j <= end; j++ {
					s.turnView(c, j, nil, &s.chat[j], j == lastAssistant && s.runActive())
				}
			})
			i = end
			continue
		}
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
		if prompt == nil && assistantToolOnly(message) {
			end := i
			for end+1 < len(s.chat) &&
				s.chat[end+1].role == chatAssistant &&
				assistantToolOnly(&s.chat[end+1]) {
				end++
			}
			if end > i {
				ui.Column(c).Key(fmt.Sprintf("tool-turn-group-%d", start)).
					FillWidth().MinWidth(0).Gap(8).Children(func() {
					for j := start; j <= end; j++ {
						s.turnView(c, j, nil, &s.chat[j], j == lastAssistant && s.runActive())
					}
				})
				i = end
				continue
			}
		}
		s.turnView(c, start, prompt, message, message != nil && i == lastAssistant && s.runActive())
	}
}

func promptToolGroupEnd(messages []chatMessage, start int) (int, bool) {
	if start < 0 || start+1 >= len(messages) || messages[start].role != chatUser ||
		!assistantToolOnly(&messages[start+1]) {
		return start, false
	}
	end := start + 1
	for end+1 < len(messages) && assistantToolOnly(&messages[end+1]) {
		end++
	}
	return end, true
}

func assistantToolOnly(message *chatMessage) bool {
	if message == nil || message.role != chatAssistant {
		return false
	}
	hasTool := false
	for _, block := range message.blocks {
		switch block.kind {
		case chatBlockText:
			if strings.TrimSpace(block.text) != "" {
				return false
			}
		case chatBlockTool:
			hasTool = true
		}
	}
	return hasTool
}

func (s *appState) emptyTranscript(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).FillWidth().Padding(96, 0, 0, 0).AlignItems(ui.Center).Gap(10).Children(func() {
		components.Text(c, text("chatEmptyTitle")).FontSize(fontTitle).FontWeight(600).TextColor(t.Text)
		components.Text(c, text("chatEmptyHint")).FontSize(13.5).TextColor(t.TextMuted)
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
		options.Summary = textArgs("chatWorkedFor", map[string]any{"duration": elapsed})
		options.Working = textArgs("chatWorkingFor", map[string]any{"duration": elapsed})
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
		note = text("chatWaitingForTool")
		if tool.partial != "" {
			note = ""
		}
	case toolCallInterrupted:
		note = text("chatInterruptedWhenDisconnected")
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
	return textArgs("chatRanTools", map[string]any{"count": len(activities)})
}

func turnElapsed(c *ui.Context, message *chatMessage, live bool) string {
	if message.startedAt.IsZero() {
		return textArgs("chatDuration", map[string]any{"minutes": 0, "seconds": 0})
	}
	end := message.endedAt
	if live || end.IsZero() {
		end = c.Now()
	}
	seconds := max(0, int(end.Sub(message.startedAt).Seconds()))
	return textArgs("chatDuration", map[string]any{
		"minutes": seconds / 60,
		"seconds": seconds % 60,
	})
}

func turnEndLabel(status string) string {
	switch strings.ToLower(status) {
	case "stopped":
		return text("chatStopped")
	case "interrupted":
		return text("chatInterrupted")
	default:
		return text("chatFailed")
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
	running := s.runActive() && s.run != runStopping
	sendLabel := text("chatSend")
	if running {
		// GlassComposer only reserves its built-in Stop action while the draft is
		// empty. With text present it renders the primary action as Send, so use
		// that slot for Stop and keep queue/steer in the trailing control.
		sendLabel = text("chatStop")
	}
	ui.Column(c).FillWidth().MinWidth(0).Children(func() {
		s.runningQueueBar(c)
		s.followUpAnimatedCard(c, cardVisible)
		result := chat.GlassComposer(c, &s.chatView.layout, &s.composerDraft, chat.ComposerOptions{
			Label:        text("chatMessageLabel"),
			Placeholder:  placeholder,
			SendLabel:    sendLabel,
			StopLabel:    text("chatStop"),
			Running:      running,
			SendDisabled: !running && s.piConn != piConnected,
			Accessory: func(c *ui.Context) {
				s.composerChoices(c)
			},
			Trailing: func(c *ui.Context) {
				if chat.IconAction(c, text("chatAttachmentUnavailable"), lu.Paperclip).Clicked() {
					s.showNotice(text("chatAttachmentNotice"))
				}
				s.runningSendButton(c)
			},
			Footer: func(c *ui.Context) {
				chat.Chip(c, s.piCompactLabel(), chat.ChipOptions{Icon: lu.Terminal, ReadOnly: true})
				ui.Spacer(c)
				switch {
				case visible && style == followUpStyleGhost:
					components.Text(c, text("chatFollowUpHint")).FontSize(fontSmall).TextColor(t.TextMuted)
				case s.run == runStopping:
					s.statusShimmer(c, text("chatStoppingPi"), t)
				case s.retrying:
					s.statusShimmer(c, s.retryStatusLabel(c), t)
				case s.compacting:
					s.statusShimmer(c, text("chatCompactingContext"), t)
				case s.runActive():
					s.statusShimmer(c, text("chatPiWorking"), t)
				}
			},
		})
		result.Root.Key("chat-composer")
		result.Input.FontSize(fontBody)
		if !cardVisible {
			s.applyPendingFollowUpAdoption(result.Input)
		}
		if result.Input.Focused() {
			result.Input.OnTextKey(func(k ui.TextKey) bool {
				return s.handleComposerTextKey(k)
			})
		}
		if visible && result.Input.Focused() {
			composing := result.Input.Composing()
			result.Input.HandleInput(func(ev ui.InputEvent) bool {
				return s.handleFollowUpComposerInput(ev, composing)
			})
		} else if running && result.Input.Focused() && strings.TrimSpace(s.composerDraft) != "" {
			composing := result.Input.Composing()
			result.Input.HandleInput(func(ev ui.InputEvent) bool {
				return handleRunningComposerInput(
					ev,
					composing,
					s.composerDraft,
					s.currentRunningSendMode(),
					s.sendRunningPrompt,
				)
			})
		}
		if result.Send && running {
			s.stopPrompt()
		} else if result.Send {
			s.sendPrompt()
		}
		if result.Stop {
			s.stopPrompt()
		}
	})
}

func handleRunningComposerInput(ev ui.InputEvent, composing bool, draft string,
	defaultMode runningSendMode, send func(string, runningSendMode),
) bool {
	prompt := strings.TrimSpace(draft)
	decision := runningSendKeyDecision(ev.Key, ev.Mods, prompt != "", true, defaultMode)
	if composing || ev.Kind != ui.InputKeyDown || !decision.handled || !decision.running {
		return false
	}
	send(prompt, decision.mode)
	return true
}

func (s *appState) runningSendButton(c *ui.Context) {
	if !s.runActive() || s.run == runStopping || strings.TrimSpace(s.composerDraft) == "" {
		return
	}
	mode := s.currentRunningSendMode()
	key := oppositeShortcutText()
	label := runningSendModeLabel(mode)
	button := ui.ButtonBase(c.Key("running-send-"+string(mode))).Height(26).Padding(0, 9).
		Radius(13).Background(ui.Color{}).Cursor(ui.CursorPointer).Label(label).
		Tooltip(textArgs("runningSendOppositeTooltip", map[string]any{
			"mode":     runningSendModeLabel(oppositeRunningSendMode(mode)),
			"shortcut": key,
		}))
	if button.Hovered() {
		button.Background(c.Theme().SurfaceHover)
	}
	button.Children(func() {
		components.Text(c, label).FontSize(fontSmall).FontWeight(600).TextColor(c.Theme().TextMuted)
	})
	if button.Clicked() {
		s.sendRunningPrompt(strings.TrimSpace(s.composerDraft), mode)
	}
}

func oppositeShortcutText() string {
	if runtime.GOOS == "darwin" {
		return "⌘+Enter"
	}
	return "Ctrl+Enter"
}

type queueDisplayItem struct {
	label string
	text  string
}

func queueDisplayItems(steering, followUp []string, limit int) ([]queueDisplayItem, int) {
	filtered := func(values []string) []string {
		result := make([]string, 0, len(values))
		for _, value := range values {
			if value = strings.TrimSpace(value); value != "" {
				result = append(result, value)
			}
		}
		return result
	}
	steering = filtered(steering)
	followUp = filtered(followUp)
	total := len(steering) + len(followUp)
	if limit <= 0 {
		return nil, total
	}
	items := make([]queueDisplayItem, 0, min(limit, total))
	appendItems := func(values []string, key string) {
		for _, value := range values {
			if len(items) < limit {
				items = append(items, queueDisplayItem{label: text(key), text: value})
			}
		}
	}
	appendItems(steering, "queueSteerLabel")
	appendItems(followUp, "queueFollowUpLabel")
	return items, max(0, total-len(items))
}

func (s *appState) queueItemCount() int {
	return s.queueSteeringCount + s.queueFollowUpCount
}

func (s *appState) runningQueueBar(c *ui.Context) {
	t := c.Theme()
	bar := ui.Column(c).Key("running-queue")
	duration := s.panelAnimationDuration(currentAppearanceReduceMotion())
	target := float32(0)
	if s.queueItemCount() > 0 {
		target = 1
	}
	height := float32(0)
	if duration == 0 {
		height = target
	} else {
		height = bar.AnimateWith("running-queue-height", target, duration, ui.EaseOut)
	}
	bar.FillWidth().MinWidth(0).MaxHeight(88 * height).Clip()
	if height == 0 {
		return
	}
	bar.Margin(8, 0).Padding(8, 10).Radius(9).Border(1, t.Border).Background(t.Surface).
		Gap(5).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Icon(c, icon("list")).Size(13, 13).TextColor(t.TextMuted)
			ui.Text(c, text("queueTitle")).FontSize(fontSmall).FontWeight(650).TextColor(t.Text).Grow(1)
			label := text("queueClear")
			if s.queueClearPending {
				label = text("queueClearPending")
			}
			action := ui.ButtonBase(c.Key("queue-clear")).Padding(3, 7).Radius(6).
				Background(ui.Color{}).Cursor(ui.CursorPointer).Label(label)
			if action.Hovered() && !s.queueClearPending {
				action.Background(t.SurfaceHover)
			}
			action.Children(func() {
				components.Text(c, label).FontSize(fontCaption).FontWeight(600).TextColor(t.TextMuted)
			})
			if action.Clicked() && !s.queueClearPending {
				s.queueClear()
			}
		})
		items, overflow := queueDisplayItems(s.queueSteering, s.queueFollowUp, 3)
		for index, item := range items {
			ui.Row(c).Key(fmt.Sprintf("queue-item-%d", index)).FillWidth().MinWidth(0).
				Gap(7).AlignItems(ui.Center).Children(func() {
				ui.Text(c, item.label).FontSize(fontMicro).FontWeight(700).TextColor(t.Accent)
				ui.Text(c, item.text).FontSize(fontSmall).TextColor(t.TextMuted).SingleLine().Grow(1)
			})
		}
		if overflow > 0 {
			ui.Text(c, textArgs("queueMore", map[string]any{"count": overflow})).
				FontSize(fontCaption).TextColor(t.TextMuted)
		}
	})
}

func (s *appState) followUpAnimatedCard(c *ui.Context, visible bool) {
	card := ui.Column(c).Key("follow-up-suggestion-slot")
	duration := s.panelAnimationDuration(currentAppearanceReduceMotion())
	target := float32(0)
	if visible {
		target = 1
	}
	height := float32(0)
	if duration == 0 {
		height = target
	} else {
		height = card.AnimateWith("follow-up-suggestion-height", target, duration, ui.EaseOut)
	}
	card.FillWidth().MinWidth(0).MaxHeight(220 * height).Clip()
	if height == 0 {
		return
	}
	card.Children(func() {
		s.followUpSuggestionCard(c)
	})
}

func (s *appState) handleComposerTextKey(k ui.TextKey) bool {
	textValue := strings.TrimSpace(k.Text())
	decision := runningSendKeyDecision(
		k.Key,
		k.Mods,
		textValue != "",
		s.runActive() && s.run != runStopping,
		s.currentRunningSendMode(),
	)
	if !decision.handled {
		return false
	}
	if decision.running {
		s.composerDraft = textValue
		s.sendRunningPrompt(textValue, decision.mode)
		k.SetText("")
		return true
	}
	s.composerDraft = textValue
	s.sendPrompt()
	if s.composerDraft == "" {
		k.SetText("")
	}
	return true
}

type composerSendDecision struct {
	handled bool
	running bool
	mode    runningSendMode
}

func runningSendKeyDecision(key ui.Key, mods ui.Modifiers, hasText, running bool, defaultMode runningSendMode) composerSendDecision {
	if key != ui.KeyEnter || mods == ui.Shift {
		return composerSendDecision{}
	}
	if mods != 0 && mods != ui.Cmd {
		return composerSendDecision{}
	}
	if !hasText {
		return composerSendDecision{}
	}
	if running {
		mode := defaultMode
		if mods == ui.Cmd {
			mode = oppositeRunningSendMode(mode)
		}
		return composerSendDecision{handled: true, running: true, mode: mode}
	}
	if mods != 0 {
		return composerSendDecision{}
	}
	return composerSendDecision{handled: true}
}

func (s *appState) statusShimmer(c *ui.Context, label string, t *ui.Theme) {
	if s.reducedMotionActive(currentAppearanceReduceMotion()) {
		components.Text(c, label).FontSize(12.5).TextColor(t.TextMuted)
		return
	}
	chat.Shimmer(c, label, 12.5, t.TextMuted, t.Text)
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
			SearchLabel: text("modelsSearch"),
			EmptyText:   text("modelsNone"),
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
		components.Tooltip(c, chip, textArgs("thinkingTooltip", map[string]any{"level": label}))
		return
	}

	wasOpen := s.chatView.effortPicker.Open
	result := chat.EffortPicker(c.Key("thinking-picker"), &s.chatView.effortPicker, chat.EffortPickerOptions{
		Label:    text("thinkingTitle"),
		Tooltip:  textArgs("thinkingTooltip", map[string]any{"level": thinkingLevelLabel(selected)}),
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
	label := text("diagnosticRetrying")
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
	return textArgs("retryStatus", map[string]any{
		"label":   label,
		"seconds": (remaining + 999) / 1000,
	})
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
		return text("chatConnectionConnected")
	case piConnecting:
		return text("chatConnectionConnecting")
	default:
		return text("chatConnectionNotConnected")
	}
}
