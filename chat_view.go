package main

import (
	"fmt"
	"math"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/components/chat"
	lu "github.com/oneo-me/onevra/source/icons/lucide"
)

// chatPresentation retains Onevra presentation state across frames. Pi owns
// the conversation data; these values only remember scroll and disclosure.
type chatPresentation struct {
	conversation  chat.ConversationState
	layout        chat.FloatingConversationState
	modelPicker   chat.PickerState
	contextPicker chat.PickerState
	effortPicker  chat.EffortState
	markdown      markdown.Options
	turns         map[int]*chat.TurnState
	groups        map[string]*chat.GroupState
	activities    map[string]*activityPresentation
	toolGroups    map[string]*toolGroupPresentation

	thinkingPending     string
	thinkingCommitTimer *time.Timer
}

type activityPresentation struct {
	state  chat.ActivityState
	synced bool
	last   bool
}

type toolGroupPresentation struct {
	choice uint8
}

func (g *toolGroupPresentation) expanded(live bool) bool {
	if g.choice == 0 {
		return live
	}
	return g.choice == 1
}

func (g *toolGroupPresentation) set(open bool) {
	g.choice = 2
	if open {
		g.choice = 1
	}
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

func (p *chatPresentation) toolGroup(key string) *toolGroupPresentation {
	if p.toolGroups == nil {
		p.toolGroups = map[string]*toolGroupPresentation{}
	}
	if p.toolGroups[key] == nil {
		p.toolGroups[key] = &toolGroupPresentation{}
	}
	return p.toolGroups[key]
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
	s.configureMarkdownV12(c, p)
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).MinWidth(0).Children(func() {
		if s.piConnectionErrorActive() {
			s.piConnectionAlert(c, p)
		}
		s.legacyMigrationPrompt(c, p)
		chat.FloatingConversationWith(c, &s.chatView.layout, &s.chatView.conversation, chat.FloatingConversationOptions{
			JumpLabel: text("chatBackToLatest"),
			Content:   func(c *ui.Context) { s.transcript(c) },
			Composer:  func(c *ui.Context) { s.chatComposer(c, p) },
		})
	})
}

func (s *appState) configureMarkdownV12(c *ui.Context, p palette) {
	theme := markdown.DefaultTheme(c.Theme())
	theme.FontSize = fontBody
	theme.LineHeight = 1.5
	theme.BlockSpacing = 12.0 / fontBody
	theme.ItemSpacing = 4.0 / fontBody
	theme.ListIndent = 22.0 / fontBody
	theme.Code = p.text
	theme.CodeBackground = p.text.Alpha(.09)
	theme.CodeSize = 1
	theme.CodeBlockBackground = p.raisedSurface
	theme.CodeBlockBorder = p.border
	theme.CodeBlockRadius = 8
	theme.CodeBlockPadding = 12.0 / fontBody
	theme.CodeLineHeight = 1.4
	codeColors := editor.DefaultTheme(c.Theme())
	codeColors.Background = p.raisedSurface
	codeColors.Text = p.text
	for i := range codeColors.Tokens {
		codeColors.Tokens[i] = editor.TokenStyle{Color: p.text}
	}
	codeColors.Tokens[editor.TokenKeyword] = editor.TokenStyle{Color: p.accentPressed}
	codeColors.Tokens[editor.TokenType] = editor.TokenStyle{Color: p.accentPressed}
	codeColors.Tokens[editor.TokenFunction] = editor.TokenStyle{Color: p.accentText}
	codeColors.Tokens[editor.TokenString] = editor.TokenStyle{Color: p.successText}
	codeColors.Tokens[editor.TokenComment] = editor.TokenStyle{Color: p.muted}
	theme.CodeColors = codeColors
	s.chatView.markdown.Theme = theme
	s.chatView.markdown.Block = func(c *ui.Context, doc *parser.Document, index int) bool {
		if markdownListNeedsItemGapV12(doc, index) {
			ui.Box(c).FillWidth().Height(4)
		}
		return false
	}
	s.chatView.markdown.CodeBlock = func(c *ui.Context, block *markdown.CodeBlockView) {
		card := ui.Column(c).FillWidth().MinWidth(0)
		card = styleRaisedSurface(card, p, 8).Clip()
		card.Children(func() {
			// Body contributes 3.5px above its first line; this brings the
			// visual top inset to the prototype's 12px.
			ui.Row(c).FillWidth().Padding(8, 4, 0, 4).Children(func() {
				block.Body(c)
			})
		})
	}
}

func (s *appState) piConnectionAlert(c *ui.Context, p palette) {
	message := s.piConnectionErrorText()
	ui.Row(c).FillWidth().Justify(ui.Center).MarginY(8).Children(func() {
		ui.Row(c).FillWidth().MaxWidth(chat.ContentWidth).MinWidth(0).Padding(9, 12).Gap(10).Radius(8).
			Border(runtimeTokens.Dimension.Border.Value, p.danger.Alpha(.35)).Background(p.danger.Alpha(.08)).
			AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon("info")).Size(16, 16).Shrink(0).TextColor(p.dangerText)
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
		components.Text(c, text("chatEmptyHint")).FontSize(fontSmall).TextColor(t.TextMuted)
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
		tools := 0
		for _, block := range message.blocks {
			if block.kind == chatBlockTool {
				tools++
			}
		}
		options.Working = textArgs("chatWorkingFor", map[string]any{"duration": elapsed})
		plan := planAssistantTurn(message, live)
		if live {
			for stepIndex, step := range plan.steps {
				isLast := stepIndex == len(plan.steps)-1
				options.Steps = append(options.Steps, s.turnStep(options.ID, message, step, isLast))
			}
		} else if len(plan.steps) > 0 || plan.final >= 0 {
			options.Final = func(c *ui.Context) {
				s.settledTurnContentV12(c, options.ID, message, plan)
			}
		}
		if options.Status == chat.Failed || options.Status == chat.Cancelled {
			label, failed := turnEndLabel(message.status), options.Status == chat.Failed
			options.Footer = func(c *ui.Context) { turnFooter(c, label+" - "+elapsed, failed) }
		} else if options.Status == chat.Completed {
			var promptText string
			if prompt != nil {
				promptText = messageText(prompt)
			}
			options.Footer = func(c *ui.Context) {
				s.turnCompletionFooter(c, message, elapsed, tools, promptText)
			}
		}
	}
	chat.Turn(c, s.chatView.turn(index), options)
}

func (s *appState) settledTurnContentV12(c *ui.Context, turnID string, message *chatMessage, plan turnPlan) {
	ui.Column(c).FillWidth().MinWidth(0).Gap(12).Children(func() {
		lastStepWasTools := false
		for _, step := range plan.steps {
			view := s.turnStep(turnID, message, step, false)
			if view.Message != nil {
				view.Message(c)
			}
			lastStepWasTools = step.tools != nil
		}
		if plan.final >= 0 {
			final := markdown.View(c, message.blocks[plan.final].text, &s.chatView.markdown).FillWidth()
			if lastStepWasTools {
				final.Margin(2.5, 0, 0, 0)
			}
		}
	})
}

func markdownListNeedsItemGapV12(doc *parser.Document, index int) bool {
	block := doc.Block(index)
	if block.Kind != parser.List {
		return false
	}
	previous := -1
	for i := block.Parent + 1; i < index; i = doc.Block(i).Next {
		previous = i
	}
	return previous >= 0 && doc.Block(previous).Kind == parser.Paragraph
}

func (s *appState) turnStep(turnID string, message *chatMessage, step turnPlanStep, live bool) chat.Step {
	if step.tools == nil {
		text := message.blocks[step.text].text
		return chat.Step{ID: fmt.Sprintf("text-%d", step.text), Message: func(c *ui.Context) {
			if live {
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
	group := s.chatView.toolGroup(turnID + "/" + groupID)
	title := activityGroupTitle(activities, live)
	return chat.Step{ID: groupID, Message: func(c *ui.Context) {
		t := c.Theme()
		open := group.expanded(live)
		secondary := t.Text.Mix(t.TextMuted, .45)
		ui.Column(c).FillWidth().MinWidth(0).Gap(8).Children(func() {
			button := ui.ButtonBase(c).Label(title).Expanded(open).
				Height(26).Justify(ui.Start).AlignItems(ui.Center).Gap(6).
				Padding(0).Background(ui.Color{}).Cursor(ui.CursorPointer)
			if button.Hovered() || button.Focused() {
				button.Background(t.Text.Alpha(.04))
			}
			button.Children(func() {
				chevron := lu.ChevronRight
				if open {
					chevron = lu.ChevronDown
				}
				components.Icon(c, chevron).Size(12, 12).Shrink(0).TextColor(t.TextMuted)
				components.Text(c, title).FontSize(fontBody).TextColor(secondary).
					Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
			})
			if button.Clicked() {
				group.set(!open)
				open = !open
			}
			if open {
				ui.Column(c).FillWidth().MinWidth(0).Gap(8).Padding(18, 0, 0, 0).
					Children(func() {
						for _, activity := range activities {
							chat.ActivityRow(c, activity)
						}
					})
			}
		})
	}}
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
		note = strings.TrimPrefix(note+" - "+formatToolDuration(tool.durationMS), " - ")
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
		return textArgs("chatDurationSeconds", map[string]any{"seconds": 0})
	}
	end := message.endedAt
	if live || end.IsZero() {
		end = c.Now()
	}
	seconds := max(0, int(end.Sub(message.startedAt).Seconds()))
	if seconds < 60 {
		return textArgs("chatDurationSeconds", map[string]any{"seconds": seconds})
	}
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
		components.Text(c, label).FontSize(fontSmall).FontWeight(500).TextColor(color)
	})
}

func (s *appState) turnCompletionFooter(c *ui.Context, message *chatMessage,
	elapsed string, tools int, prompt string,
) {
	t := c.Theme()
	ui.Row(c).Height(26).Margin(1, 0, 0, 0).AlignItems(ui.Center).Gap(4).Children(func() {
		components.Icon(c, lu.Check).Size(12, 12).TextColor(t.Success)
		components.Text(c, text("chatCompleted")).FontSize(fontSmall).
			FontWeight(500).TextColor(t.Success)
		meta := elapsed
		if tools > 0 {
			meta += " · " + textArgs("chatToolCount", map[string]any{"count": tools})
		}
		components.Text(c, meta).FontSize(fontSmall).TextColor(t.TextMuted)
		if iconActionV12(c, text("chatCopy"), lu.Copy).Clicked() {
			c.WriteClipboard(messageText(message))
		}
		if prompt != "" {
			if iconActionV12(c, text("retry"), lu.RotateCcw).Clicked() {
				s.composerDraft = prompt
			}
		}
	})
}

func (s *appState) chatComposer(c *ui.Context, p palette) {
	t := c.Theme()
	visible := s.followUpSuggestionVisible()
	style := s.currentFollowUpStyle()
	placeholder := composerPlaceholder(s.piConn)
	if visible && style == followUpStyleGhost {
		placeholder = ""
	}
	cardVisible := visible && style == followUpStyleCard
	running := s.runActive() && s.run != runStopping
	width, _ := c.Size()
	compactHUD := s.chatMainWidth(width) < chat.ContentWidth
	hudVisible := s.piHUDLabel() != ""
	composerFooter := func(c *ui.Context) {
		switch {
		case s.run == runStopping:
			s.statusShimmer(c, text("chatStoppingPi"), t)
		case s.retrying:
			s.statusShimmer(c, s.retryStatusLabel(c), t)
		case s.compacting:
			s.statusShimmer(c, text("chatCompactingContext"), t)
		case s.runActive():
			s.statusShimmer(c, text("chatPiWorking"), t)
		}
	}
	ui.Column(c).FillWidth().MinWidth(0).Padding(24, 0, 4, 0).Gap(8).Children(func() {
		s.runningQueueBar(c)
		s.followUpAnimatedCard(c, cardVisible)
		var placeholderOverlay []func(*ui.Context)
		if visible && style == followUpStyleGhost {
			placeholderOverlay = append(placeholderOverlay, func(c *ui.Context) {
				s.followUpGhostPlaceholder(c)
			})
		}
		footer := composerFooter
		if !s.runActive() && !s.retrying && !s.compacting {
			footer = nil
		}
		result := opaqueComposerV12(c, p, &s.composerDraft, chat.ComposerOptions{
			Label:        text("chatMessageLabel"),
			Placeholder:  placeholder,
			SendLabel:    text("chatSend"),
			StopLabel:    text("chatStop"),
			Running:      running,
			SendDisabled: !running && (s.piConn != piConnected || s.piSessionBusy || s.modelStateUnconfirmed),
			Accessory: func(c *ui.Context) {
				ui.Row(c).FillWidth().MinWidth(0).Wrap().AlignItems(ui.Start).Gap(8).Children(func() {
					if composerAddButtonV12(c).Clicked() {
						s.showNotice(text("chatAttachmentNotice"))
					}
					s.composerChoices(c)
					s.composerTableContextChip(c)
					s.composerContextChip(c)
					if running && strings.TrimSpace(s.composerDraft) != "" {
						ui.Spacer(c).Grow(1)
						s.runningSendModeControl(c)
					}
				})
			},
			Footer: footer,
		}, placeholderOverlay...)
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
			s.sendRunningPrompt(strings.TrimSpace(s.composerDraft), s.currentRunningSendMode())
		} else if result.Send {
			s.sendPrompt()
		}
		if result.Stop {
			s.stopPrompt()
		}
		if hudVisible && compactHUD {
			ui.Row(c).Key("pi-hud-row").Label("pi-hud-row").FillWidth().MinWidth(0).Height(20).
				Padding(0).Justify(ui.Center).AlignItems(ui.Center).Children(func() { s.piHUD(c) })
		}
		if hudVisible && !compactHUD {
			ui.Row(c).Key("pi-hud-row").Label("pi-hud-row").FillWidth().MinWidth(0).Height(20).
				Padding(0).Justify(ui.Center).AlignItems(ui.Center).
				Children(func() { s.piHUD(c) })
		}
	})
}

func (s *appState) followUpGhostPlaceholder(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).MinWidth(0).AlignItems(ui.Center).Gap(8).Children(func() {
		components.Text(c, s.followUpSuggestion).FontSize(fontBody).
			TextColor(t.TextMuted).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		ui.Row(c).Shrink(0).Height(22).Padding(0, 6).Radius(v12ChipRadius).
			Border(v12BorderWidth, t.Border).Children(func() {
			components.Text(c, text("chatFollowUpKeyTab")).FontSize(fontCaption).
				TextColor(t.TextMuted)
		})
	})
}

func composerAddButtonV12(c *ui.Context) ui.Element {
	label := text("chatAttachmentUnavailable")
	t := c.Theme()
	button := ui.ButtonBase(c).Size(26, 26).Shrink(0).Radius(v12ChipRadius).
		Center().Background(ui.Color{}).TextColor(t.TextMuted).Label(label)
	if button.Hovered() || button.Focused() {
		button.Background(t.SurfaceHover).TextColor(t.Text)
	}
	button.Children(func() {
		components.Icon(c, lu.Plus).Size(15, 15).TextColor(t.TextMuted)
	})
	components.Tooltip(c, button, label)
	return button
}

func (s *appState) chatMainWidth(windowWidth float32) float32 {
	navigationWidth := s.navigationWidth(windowWidth) + shellGap
	var tasksWidth float32
	if s.tasksOpen && windowWidth >= narrowShellWidth {
		tasksWidth = taskPanelWidth + shellGap
	}
	return max(0, windowWidth-2*shellGap-navigationWidth-tasksWidth)
}

const (
	modelPopoverMinWidthV12 float32 = 300
	// modelPopoverChromeV12 is the room a row takes around its model name:
	// panel padding, row padding, the leading icon, both gaps and the check.
	modelPopoverChromeV12     float32 = 96
	modelPopoverSideMarginV12 float32 = 16
	modelPopoverRowHeightV12  float32 = 56
)

// modelPopoverWidthV12 sizes the model popover to its longest model name, so
// the common long names read whole on one line, and keeps it within the
// window: names longer than that keep their tooltip and ellipsis.
func modelPopoverWidthV12(c *ui.Context, labels []string, windowWidth float32) float32 {
	widest := float32(0)
	for _, label := range labels {
		if w, _ := c.MeasureText(0, ui.Span{Text: label, Size: fontBody}); w > widest {
			widest = w
		}
	}
	available := max(0, windowWidth-2*modelPopoverSideMarginV12)
	if available <= modelPopoverMinWidthV12 {
		return available
	}
	return min(max(modelPopoverMinWidthV12, widest+modelPopoverChromeV12), available)
}

// modelPopoverListHeightV12 is the height the model list may take: it grows
// with the window and never past the room the popover has, so its last row
// stays reachable by scrolling.
func modelPopoverListHeightV12(windowHeight float32) float32 {
	return max(modelPopoverRowHeightV12, min(windowHeight-300, 520))
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

func (s *appState) runningSendModeControl(c *ui.Context) {
	t := c.Theme()
	mode := s.currentRunningSendMode()
	ui.Row(c).Key("running-send-mode").Shrink(0).Height(26).Padding(2).Gap(2).
		Radius(8).Background(t.Text.Alpha(.06)).Children(func() {
		segment := func(value runningSendMode) {
			selected := mode == value
			label := runningSendModeLabel(value)
			background := ui.Color{}
			foreground := t.TextMuted
			if selected {
				background = t.SurfaceHover
				foreground = t.Text
			}
			button := ui.ButtonBase(c.Key("running-send-mode-"+string(value))).
				Height(24).Padding(0, 10).Radius(6).Background(background).
				Cursor(ui.CursorPointer).Label(label).
				Tooltip(runningSendModeDescription(value) + " - " +
					textArgs("runningSendOppositeTooltip", map[string]any{
						"mode":     runningSendModeLabel(oppositeRunningSendMode(mode)),
						"shortcut": oppositeShortcutText(),
					}))
			if button.Hovered() && !selected {
				button.Background(t.SurfaceHover.Alpha(.7))
			}
			button.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
					components.Text(c, label).SingleLine().FontSize(fontSmall).FontWeight(600).TextColor(foreground)
				})
			})
			if button.Clicked() {
				s.setRunningSendMode(value)
			}
		}
		segment(runningSendQueue)
		segment(runningSendSteer)
	})
	if mode == runningSendSteer {
		components.Text(c, runningSendModeDescription(mode)).SingleLine().FontSize(fontCaption).
			TextColor(t.TextMuted).Grow(1).MinWidth(0).Ellipsis("…").
			Tooltip(runningSendModeDescription(mode))
	} else {
		ui.Spacer(c)
	}
}

func runningSendModeDescription(mode runningSendMode) string {
	if normalizeRunningSendMode(string(mode)) == runningSendSteer {
		return text("runningSendSteerDescription")
	}
	return text("runningSendQueueDescription")
}

func (s *appState) runningStopButton(c *ui.Context) {
	t := c.Theme()
	label := text("chatStopCurrentRun")
	button := ui.ButtonBase(c.Key("running-stop")).Size(26, 26).Radius(13).
		Center().Background(t.Danger.Alpha(.12)).TextColor(t.Danger).
		Cursor(ui.CursorPointer).Label(label).Tooltip(text("chatStopTooltip"))
	if button.Hovered() {
		button.Background(t.Danger.Alpha(.18))
	}
	button.Children(func() {
		components.Icon(c, lu.CircleStop).Size(14, 14).TextColor(t.Danger)
	})
	if button.Clicked() {
		s.stopPrompt()
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
	bar.Margin(8, 0).Padding(8, 10).Radius(9).
		Border(runtimeTokens.Dimension.Border.Value, t.Border).Background(t.Surface).
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
		components.Text(c, label).FontSize(fontSmall).TextColor(t.TextMuted)
		return
	}
	chat.Shimmer(c, label, fontSmall, t.TextMuted, t.Text)
}

func (s *appState) composerChoices(c *ui.Context) {
	ui.Row(c).Key("composer-model-thinking").Shrink(0).Height(26).
		AlignItems(ui.Center).Gap(4).Children(func() {
		s.composerChoicesContent(c)
	})
}

func (s *appState) composerChoicesContent(c *ui.Context) {
	readOnly := s.runActive() || s.compacting || s.piSessionBusy || s.modelStateUnconfirmed
	if (s.runActive() || s.compacting) && !s.piSessionBusy {
		s.cancelThinkingPending()
	}

	families := piModelFamilies(s.piModels)
	activeFamily, activeFamilyOK := findPiModelFamilyInFamilies(
		families, s.piModelProvider, s.piModelID, s.piModelName,
	)
	contextWindow := piActiveContextWindow(
		activeFamily, activeFamilyOK, s.piModelID, s.hud.contextWindowFallback,
	)
	levels := buildPiThinkingOptions(s.piThinkingLevels)
	if !s.piThinkingLevelsKnown || !reasoningLevelsVisible(s.piThinkingLevels) || len(levels) == 0 {
		s.chatView.effortPicker.Open = false
		s.cancelThinkingPending()
	} else {
		s.normalizeThinkingPending(levels)
	}
	s.composerModelControl(c, readOnly, families, activeFamily, activeFamilyOK,
		contextWindow, composerThinkingLabel(levels, s.piThinkingLevel, s.chatView.thinkingPending))
}

func composerThinkingLabel(levels []chat.Option, current, pending string) string {
	if len(levels) == 0 {
		return ""
	}
	selected := current
	if pending != "" {
		selected = pending
	}
	if selected == "" {
		selected = levels[0].ID
	}
	for _, level := range levels {
		if level.ID == selected {
			return level.Label
		}
	}
	return levels[0].Label
}

func (s *appState) composerContextChip(c *ui.Context) {
	families := piModelFamilies(s.piModels)
	activeFamily, activeFamilyOK := findPiModelFamilyInFamilies(
		families, s.piModelProvider, s.piModelID, s.piModelName,
	)
	contextWindow := piActiveContextWindow(
		activeFamily, activeFamilyOK, s.piModelID, s.hud.contextWindowFallback,
	)
	capacity := formatContextWindow(contextWindow)
	if capacity == "" {
		return
	}
	t := c.Theme()
	ui.Row(c).Key("composer-context").Shrink(0).Height(26).Padding(0, 8).
		AlignItems(ui.Center).Children(func() {
		components.Text(c, capacity).SingleLine().FontSize(fontSmall).TextColor(t.TextMuted)
	})
}

func (s *appState) composerTableContextChip(c *ui.Context) {
	attachment := s.composerTableContext
	if attachment == nil {
		return
	}
	t := c.Theme()
	removeLabel := text("chatRemoveTableContext")
	ui.Row(c).Key("composer-table-context").Label(attachment.Label).Shrink(0).
		MaxWidth(260).Height(26).Padding(0, 4, 0, 8).Radius(v12ChipRadius).
		Border(v12BorderWidth, t.Border).AlignItems(ui.Center).Gap(4).Children(func() {
		components.Text(c, attachment.Label).SingleLine().Ellipsis("…").
			FontSize(fontSmall).TextColor(t.TextMuted).MinWidth(0).Shrink(1)
		remove := ui.ButtonBase(c.Key("remove-table-context")).Size(18, 18).
			Center().Radius(v12ChipRadius).Background(ui.Color{}).
			TextColor(t.TextMuted).Label(removeLabel)
		if remove.Hovered() || remove.Focused() {
			remove.Background(t.SurfaceHover).TextColor(t.Text)
		}
		remove.Children(func() {
			components.Icon(c, lu.X).Size(11, 11).TextColor(t.TextMuted)
		})
		components.Tooltip(c, remove, removeLabel)
		if remove.Clicked() {
			s.composerTableContext = nil
		}
	})
}

func (s *appState) composerModelControl(c *ui.Context, readOnly bool, families []piModelFamily,
	activeFamily piModelFamily, activeFamilyOK bool, contextWindow int, thinkingLabel string,
) {
	modelLabel := piModelDisplayName(s.piModelName, s.piModel)
	brand := modelBrand(s.piModelProvider, s.piModelID, s.piModelName)
	leading := modelBrandLeading(brand)
	if len(s.piModels) == 0 || readOnly {
		s.chatView.modelPicker.Open = false
		s.chatView.contextPicker.Open = false
		if s.piModel != "" || s.modelStateUnconfirmed {
			s.composerModelChip(c, modelLabel, s.piModelProvider, contextWindow, leading, readOnly,
				activeFamily, activeFamilyOK, thinkingLabel)
		}
		return
	}
	remembered := s.rememberedModelTuplesForSession()
	options, choices := buildPiModelChoicesForFamilies(families, piModelChoiceState{
		ActiveProvider: s.piModelProvider,
		ActiveID:       s.piModelID,
		Remembered:     remembered,
	})
	wasOpen := s.chatView.modelPicker.Open
	chosen := s.customModelPicker(c, options, choices, activeFamily, activeFamilyOK,
		contextWindow, thinkingLabel)
	if wasOpen && !s.chatView.modelPicker.Open {
		s.commitThinkingPending()
	}
	if chosen != "" {
		choice, ok := choices[chosen]
		if ok && (choice.selected.Provider != s.piModelProvider || choice.selected.ID != s.piModelID) {
			s.selectModel(choice.selected.Provider, choice.selected.ID)
		}
	}
}

func (s *appState) rememberedModelTuplesForSession() map[string]modelTuple {
	if s.modelTuples == nil {
		return nil
	}
	return s.modelTuples[s.piSessionKey()]
}

func (s *appState) composerModelChip(c *ui.Context, label, provider string, contextWindow int,
	leading func(c *ui.Context, size float32), readOnly bool, activeFamily piModelFamily,
	activeFamilyOK bool, thinkingLabel string,
) {
	t := c.Theme()
	retryReadback := readOnly && s.modelStateUnconfirmed
	accessibleLabel := label
	if thinkingLabel != "" {
		accessibleLabel += " · " + thinkingLabel
	}
	var chip ui.Element
	if retryReadback {
		chip = ui.ButtonBase(c.Key("model-chip")).Label(accessibleLabel).Tooltip(text("retryReadback")).
			Height(26).MinWidth(0).Padding(0, 8).Radius(6).AlignItems(ui.Center).Gap(4).
			Background(ui.Color{}).Cursor(ui.CursorPointer)
		if chip.Hovered() || chip.Focused() {
			chip.Background(t.SurfaceHover)
		}
	} else if readOnly {
		chip = ui.Row(c).Key("model-chip").Label(accessibleLabel).Height(26).MinWidth(0).Padding(0, 8).
			Radius(6).AlignItems(ui.Center).Gap(4).Cursor(ui.CursorNotAllowed)
	} else {
		chip = ui.ButtonBase(c.Key("model-chip")).Label(accessibleLabel).Height(26).MinWidth(0).Padding(0, 8).
			Radius(6).AlignItems(ui.Center).Gap(4).Background(ui.Color{}).
			Cursor(ui.CursorPointer)
		if chip.Hovered() || chip.Focused() {
			chip.Background(t.SurfaceHover)
		}
	}
	chip.Children(func() {
		components.Text(c, label).MinWidth(0).SingleLine().FontSize(fontSmall).TextColor(t.TextMuted)
		if thinkingLabel != "" {
			components.Text(c, "· "+thinkingLabel).Shrink(0).SingleLine().
				FontSize(fontSmall).TextColor(t.TextMuted)
		}
		if !readOnly {
			components.Icon(c, lu.ChevronDown).Size(10.5, 10.5).Shrink(0).TextColor(t.TextMuted.Alpha(.7))
		}
	})
	if retryReadback && chip.Clicked() {
		if client := s.pi; client != nil {
			s.retryModelStateReadback(client, s.piEpoch, s.piSessionKey())
		}
	} else if !readOnly && chip.Clicked() {
		s.chatView.modelPicker.Open = !s.chatView.modelPicker.Open
		s.chatView.modelPicker.Query = ""
	}
}

func (s *appState) customModelPicker(c *ui.Context, options []chat.Option,
	choices map[string]piModelChoice, activeFamily piModelFamily, activeFamilyOK bool,
	contextWindow int, thinkingLabel string,
) string {
	t := c.Theme()
	label := piModelDisplayName(s.piModelName, s.piModel)
	accessibleLabel := label
	if thinkingLabel != "" {
		accessibleLabel += " · " + thinkingLabel
	}
	chip := ui.ButtonBase(c.Key("model-picker")).Label(accessibleLabel).Height(26).MinWidth(0).Padding(0, 8).
		Radius(6).AlignItems(ui.Center).Gap(4).Background(ui.Color{}).Cursor(ui.CursorPointer)
	if s.chatView.modelPicker.Open {
		chip.Background(t.SurfaceHover)
	} else if chip.Hovered() || chip.Focused() {
		chip.Background(t.SurfaceHover)
	}
	chip.Children(func() {
		components.Text(c, label).MinWidth(0).SingleLine().
			FontSize(fontSmall).TextColor(t.TextMuted)
		if thinkingLabel != "" {
			components.Text(c, "· "+thinkingLabel).Shrink(0).SingleLine().
				FontSize(fontSmall).TextColor(t.TextMuted)
		}
		components.Icon(c, lu.ChevronDown).Size(10.5, 10.5).Shrink(0).
			TextColor(t.TextMuted.Alpha(.7))
	})
	if chip.Clicked() {
		s.chatView.modelPicker.Open = !s.chatView.modelPicker.Open
		s.chatView.modelPicker.Query = ""
	}
	chosen := ""
	variants := piContextVariants(activeFamily)
	if !s.chatView.modelPicker.Open || !activeFamilyOK || len(variants) == 0 {
		s.chatView.contextPicker.Open = false
	}
	windowWidth, windowHeight := c.Size()
	width := float32(modelPopoverMinWidthV12)
	if s.chatView.modelPicker.Open {
		// Measuring every name costs a layout each; the panel only needs it
		// while it shows.
		labels := make([]string, 0, len(options))
		for _, option := range options {
			labels = append(labels, option.Label)
		}
		width = modelPopoverWidthV12(c, labels, windowWidth)
	}
	popoverWidthV12(c, chip, &s.chatView.modelPicker.Open,
		s.highContrastActive(currentAppearanceHighContrast()) || currentAppearanceReduceTransparency(),
		width, func(panel ui.Element) {
			panel.Label(label).Padding(6).Gap(0).Children(func() {
				components.Text(c, text("modelsGroupTitle")).FontSize(fontCaption).
					FontWeight(500).TextColor(t.TextMuted).Padding(6, 8, 4, 8)
				if len(options) == 0 {
					components.Text(c, text("modelsNone")).FontSize(fontSmall).
						TextColor(t.TextMuted).Padding(8)
				} else {
					components.ScrollView(c, components.ScrollViewOptions{}).FillWidth().
						MaxHeight(modelPopoverListHeightV12(windowHeight)).Padding(8, 0).
						Gap(0).Children(func() {
						for _, option := range options {
							selected := activeFamilyOK &&
								option.ID == piModelChoiceID(activeFamily.Provider, activeFamily.Key)
							choice := choices[option.ID]
							if s.customModelPickerRow(c, option, choice, selected) {
								chosen = option.ID
								s.chatView.modelPicker.Open = false
							}
						}
					})
				}
				s.modelPopoverEffortV12(c, t)
				if activeFamilyOK && len(variants) > 0 {
					s.modelPopoverContextV12(c, t, activeFamily, variants)
				}
				s.popoverDividerV12(c, t)
				manage := ui.ButtonBase(c.Key("manage-models")).Label(text("modelsManage")).
					FillWidth().Height(40).Padding(8).Radius(8).
					Justify(ui.Start).AlignItems(ui.Center).Gap(12).
					Background(ui.Color{}).TextColor(t.Text)
				if manage.Hovered() || manage.Focused() {
					manage.Background(t.SurfaceHover)
				}
				manage.Children(func() {
					components.Text(c, text("modelsManage")).MinWidth(0).Wrap().
						FontSize(fontBody).TextColor(t.Text).Grow(1)
					components.Icon(c, lu.ChevronRight).Size(12, 12).
						Shrink(0).TextColor(t.TextMuted)
				})
				if manage.Clicked() {
					s.chatView.modelPicker.Open = false
					s.settingsSelectCategory(&s.settingsPageState, settingsCategoryModels)
					s.setPage(pageSettings)
				}
			})
		})
	return chosen
}

func (s *appState) customModelPickerRow(c *ui.Context, option chat.Option,
	choice piModelChoice, selected bool,
) bool {
	t := c.Theme()
	row := ui.ButtonBase(c).Key("model-choice-"+option.ID).Label(option.Label).Selected(selected).
		FillWidth().MinWidth(0).MinHeight(modelPopoverRowHeightV12).Padding(8, 8).
		Radius(8).Justify(ui.Start).AlignItems(ui.Center).Gap(8).
		Background(ui.Color{}).TextColor(t.Text)
	if row.Hovered() || row.Focused() {
		row.Background(t.SurfaceHover)
	}
	row.Children(func() {
		if option.Leading != nil {
			option.Leading(c, 16)
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			components.Text(c, option.Label).MinWidth(0).SingleLine().Ellipsis("…").
				FontSize(fontBody).Tooltip(option.Label)
			components.Text(c, modelPickerSubtitle(choice)).MinWidth(0).SingleLine().Ellipsis("…").
				FontSize(fontSmall).LineHeight(1.3).TextColor(t.TextMuted)
		})
		if selected {
			components.Icon(c, lu.Check).Size(12, 12).TextColor(t.Accent)
		}
	})
	return row.Clicked()
}

func modelPickerSubtitle(choice piModelChoice) string {
	var contexts []int
	seen := make(map[int]struct{})
	for _, variant := range choice.family.Variants {
		if variant.ContextWindow <= 0 {
			continue
		}
		if _, ok := seen[variant.ContextWindow]; ok {
			continue
		}
		seen[variant.ContextWindow] = struct{}{}
		contexts = append(contexts, variant.ContextWindow)
	}
	sort.Ints(contexts)
	parts := make([]string, 0, 2)
	if provider := strings.TrimSpace(choice.family.Provider); provider != "" {
		parts = append(parts, provider)
	}
	if len(contexts) > 0 {
		labels := make([]string, len(contexts))
		for i, context := range contexts {
			labels[i] = formatContextWindow(context)
		}
		parts = append(parts, strings.Join(labels, " / "))
	}
	return strings.Join(parts, " · ")
}

func (s *appState) modelPopoverEffortV12(c *ui.Context, t *ui.Theme) {
	levels := buildPiThinkingOptions(s.piThinkingLevels)
	if !s.piThinkingLevelsKnown || !reasoningLevelsVisible(s.piThinkingLevels) || len(levels) == 0 {
		return
	}
	selected := s.piThinkingLevel
	if s.chatView.thinkingPending != "" {
		selected = s.chatView.thinkingPending
	}
	selectedIndex := 0
	for i, level := range levels {
		if level.ID == selected {
			selectedIndex = i
		}
	}
	ui.Column(c).FillWidth().Padding(4, 8, 8, 8).Gap(4).Children(func() {
		components.Text(c, text("thinkingStrengthTitle")).FontSize(fontBody).TextColor(t.Text)
		value := float64(selectedIndex)
		slider := ui.Slider(c, &value, 0, float64(len(levels)-1)).Step(1).
			FillWidth().Label(text("thinkingStrengthTitle"))
		if slider.Changed() {
			index := min(max(int(math.Round(value)), 0), len(levels)-1)
			s.setThinkingPending(levels[index].ID)
		}
		// Stop labels sit under the slider stops: first flush left, last flush right.
		ui.Row(c).FillWidth().Margin(4, 0, 0, 0).Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
			for i, level := range levels {
				color := t.TextMuted
				if i == selectedIndex {
					color = t.Text
				}
				components.Text(c, level.Label).SingleLine().FontSize(fontSmall).
					TextColor(color)
			}
		})
	})
}

func (s *appState) modelPopoverContextV12(c *ui.Context, t *ui.Theme, family piModelFamily,
	variants []piModel,
) {
	ui.Column(c).FillWidth().Padding(6, 8).Gap(6).Children(func() {
		components.Text(c, text("contextLabel")).FontSize(fontBody).TextColor(t.Text)
		ui.Row(c).Gap(4).Children(func() {
			for _, option := range buildPiContextOptions(variants) {
				selected := option.ID == s.piModelID
				background := t.Text.Alpha(.06)
				color := t.TextMuted
				if selected {
					background = t.Text.Alpha(.14)
					color = t.Text
				}
				button := ui.ButtonBase(c.Key("context-segment-"+option.ID)).
					Height(26).Padding(0, 10).Radius(6).Background(background).
					Label(option.Label).TextColor(color)
				button.Children(func() {
					components.Text(c, option.Label).FontSize(fontSmall).
						FontWeight(500).TextColor(color)
				})
				if button.Clicked() && !selected {
					s.chatView.modelPicker.Open = false
					s.selectModel(family.Provider, option.ID)
				}
			}
		})
	})
}

func (s *appState) popoverDividerV12(c *ui.Context, t *ui.Theme) {
	ui.Box(c).FillWidth().Height(runtimeTokens.Dimension.Border.Value).
		Margin(6, 4).Background(t.Text.Alpha(.12))
}

func piActiveContextWindow(family piModelFamily, ok bool, modelID string, fallback int) int {
	if ok {
		for _, variant := range family.Variants {
			if variant.ID == modelID && variant.ContextWindow > 0 {
				return variant.ContextWindow
			}
		}
	}
	return fallback
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
