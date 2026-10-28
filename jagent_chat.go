package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/components/chat"
	lu "github.com/oneo-me/onevra/source/icons/lucide"
)

func (s *appState) jAgentChatPage(c *ui.Context, p palette) {
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).MinWidth(0).Children(func() {
		if s.piConnectionErrorActive() {
			s.piConnectionAlert(c, p)
		}
		s.legacyMigrationPrompt(c, p)
		s.jAgentConversation(c, p)
	})
}

func (s *appState) jAgentConversation(c *ui.Context, p palette) {
	state := &s.chatView.conversation
	if s.chatView.jAgentPaused && state.Scroll.Y != s.chatView.jAgentPauseY && state.AtLatest() {
		s.chatView.jAgentPaused = false
	}
	follow := state.AtLatest() && !s.chatView.jAgentPaused
	s.chatView.jAgentBuildY = state.Scroll.Y
	if follow {
		state.JumpToLatest()
	}
	composerHeight := s.chatView.composerHeight
	if composerHeight <= 0 {
		composerHeight = p.composerMinHeight + p.chatBottomPadding
	}
	padding := p.chatHorizontalPadding

	ui.Column(c).Key("j-agent-conversation").FillWidth().Grow(1).MinHeight(0).MinWidth(0).Clip().Children(func() {
		viewport := components.ScrollView(c, components.ScrollViewOptions{}).
			Key("transcript").FillWidth().Grow(1).MinHeight(0).
			ScrollbarInsets(0, 0, composerHeight+8, 0).
			TrackScroll(&state.Scroll)
		viewport.Children(func() {
			ui.Column(c).FillWidth().MinWidth(0).Shrink(0).AlignItems(ui.Center).
				Padding(22, padding, 0, padding).Children(func() {
				ui.Column(c).FillWidth().MaxWidth(p.chatContentWidth).MinWidth(0).Gap(24).
					Children(func() {
						s.transcript(c)
					})
				ui.Box(c).Key("composer-clearance").Height(composerHeight + 22).Shrink(0)
			})
		})
		viewport.TrackScroll(&state.Scroll)
		components.ScrollEdge(c, components.ScrollEdgeOptions{
			Style:  components.ScrollEdgeBlur,
			Bottom: true,
			Extent: 24,
		}).Key("bottom-edge")

		if !follow {
			label := text("chatBackToLatest")
			button := ui.ButtonBase(c.Key("chat-jump-latest")).Label(label).
				Absolute().Attach(ui.AnchorBottom, ui.AnchorBottom).
				Bottom(composerHeight+12).Size(32, 32).Padding(0).Radius(16).
				Background(p.surface).Border(0.5, p.border).Center()
			if button.Hovered() {
				button.Background(p.surfaceHover)
			}
			button.Children(func() {
				components.Icon(c, lu.ArrowDown).Size(16, 16).TextColor(p.text)
			})
			components.Tooltip(c, button, label)
			if button.Clicked() {
				s.chatView.jAgentPaused = false
				state.JumpToLatest()
				c.Invalidate()
			}
		}

		composer := ui.Row(c).Key("floating-composer").Absolute().
			Attach(ui.AnchorBottom, ui.AnchorBottom).FillWidth().
			Justify(ui.Center).Padding(0, padding, p.chatBottomPadding, padding)
		composer.Draw(func(_ *ui.Painter, r ui.Rect) {
			if s.chatView.composerHeight != r.H {
				s.chatView.composerHeight = r.H
				c.Invalidate()
			}
		})
		composer.Children(func() {
			ui.Column(c).FillWidth().MaxWidth(p.chatContentWidth).MinWidth(0).
				Children(func() {
					s.jAgentChatComposer(c, p)
				})
		})
	})
}

func (s *appState) jAgentChatComposer(c *ui.Context, p palette) ui.Element {
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
	compactHUD := s.chatMainWidth(width) < p.chatContentWidth
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
	return ui.Column(c).FillWidth().MinWidth(0).Gap(8).Children(func() {
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
		result := jAgentComposerV12(c, p, &s.composerDraft, chat.ComposerOptions{
			Label:        text("chatMessageLabel"),
			Placeholder:  placeholder,
			SendLabel:    text("chatSend"),
			StopLabel:    text("chatStop"),
			Running:      running,
			SendDisabled: !running && (s.piConn != piConnected || s.piSessionBusy || s.modelStateUnconfirmed),
			Accessory: func(c *ui.Context) {
				ui.Row(c).FillWidth().MinWidth(0).Wrap().AlignItems(ui.Start).Gap(8).Children(func() {
					if jAgentComposerAddButton(c, p).Clicked() {
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
		result.Input.FontSize(p.bodyFont)
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

func jAgentComposerV12(
	c *ui.Context,
	p palette,
	draft *string,
	options chat.ComposerOptions,
	placeholderOverlays ...func(*ui.Context),
) chat.ComposerResult {
	var result chat.ComposerResult
	t := c.Theme()
	empty := strings.TrimSpace(*draft) == ""
	canSend := !empty && !options.SendDisabled
	result.Root = ui.Column(c).FillWidth().MinWidth(0).Shrink(0).Children(func() {
		card := ui.Column(c).FillWidth().MinWidth(0).MinHeight(p.composerMinHeight).
			Padding(p.composerPadding, p.composerPadding, p.composerPadding, p.composerPadding).
			Gap(8)
		card = styleComposerSurface(card, p, p.composerRadius)
		card.Children(func() {
			if options.Attachments != nil {
				ui.Row(c).Key("attachments").FillWidth().Wrap().Gap(8).
					Padding(0, 0, 4, 0).Children(func() { options.Attachments(c) })
			}
			result.Input = ui.TextAreaBase(c.Key("input"), draft).FillWidth().Lines(1, 10).
				MinHeight(36).Label(options.Label).Placeholder(options.Placeholder).
				Border(0, ui.Color{}).Background(ui.Color{}).Padding(0).
				FontSize(p.bodyFont).TextColor(t.Text)
			if empty && len(placeholderOverlays) > 0 {
				ui.Row(c).Absolute().Top(p.composerPadding).Left(p.composerPadding).
					Right(p.composerPadding).Height(36).AlignItems(ui.Start).
					PassThrough().Children(func() {
					placeholderOverlays[0](c)
				})
			}
			if options.OnPaste != nil {
				result.Input.OnPaste(options.OnPaste)
			}
			ui.Row(c).FillWidth().MinWidth(0).MinHeight(28).
				AlignItems(ui.Center).Gap(4).Children(func() {
				ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(4).Children(func() {
					if options.Accessory != nil {
						options.Accessory(c)
					}
				})
				if options.Trailing != nil {
					ui.Row(c).Key("trailing").Shrink(0).AlignItems(ui.Center).Gap(4).
						Children(func() { options.Trailing(c) })
				}
				if options.Running && empty {
					result.Stop = jAgentComposerAction(c, p, "running-stop", options.StopLabel,
						lu.CircleStop, t.Danger.Alpha(.12), t.Danger).Clicked()
				}
				if !options.Running || !empty {
					background, foreground := ui.Hex("#f1f1f1"), ui.Hex("#111111")
					if !canSend {
						background, foreground = p.text.Alpha(.08), p.muted
					}
					ui.Box(c).Disabled(!canSend).Children(func() {
						if jAgentComposerAction(c, p, "chatSend", options.SendLabel,
							lu.ArrowUp, background, foreground).Clicked() {
							result.Send = true
						}
					})
				}
			})
		})
		result.Card = card
		pending := ui.Local(result.Input, composerEnterKeyV12{}, func() bool { return false })
		composing := result.Input.Composing()
		result.Input.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputKeyDown || ev.Key != ui.KeyEnter || ev.Mods != 0 || composing {
				return false
			}
			*pending = true
			return true
		})
		if *pending {
			*pending = false
			result.Send = result.Send || canSend && !composing
		}
		if options.AcceptFiles {
			result.Dropped = card.DroppedFiles()
			result.Dragging = card.FileDragOver()
		}
		if len(options.DropOptions.Formats) > 0 {
			if drop, ok := ui.DropData(card, options.DropOptions); ok {
				result.Drop = &drop
			}
			_, over := ui.DataDragOver(card, options.DropOptions)
			result.Dragging = result.Dragging || over
		}
		if result.Dragging {
			card.BorderColor(t.Focus.Alpha(.7)).Background(t.Surface.Mix(t.Text, .06))
		} else if result.Input.Focused() {
			card.BorderColor(t.Focus)
		}
		if options.Footer != nil {
			ui.Row(c).Key("composer-footer").Label("composer-footer").FillWidth().
				MinWidth(0).Height(24).Padding(8, 0, 0, 0).
				AlignItems(ui.Center).Gap(2).Children(func() { options.Footer(c) })
		} else {
			ui.Row(c).Key("composer-footer").Label("composer-footer").Absolute().
				Bottom(0).Left(0).Right(0).Height(1).PassThrough()
		}
	})
	return result
}

func jAgentComposerAction(
	c *ui.Context,
	p palette,
	key, label string,
	iconValue func() *ui.SVG,
	background, foreground ui.Color,
) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Label(label).Size(32, 32).Shrink(0).
		Radius(10).Center().Background(background).TextColor(foreground)
	if b.Pressed() {
		b.Opacity(.8)
	} else if b.Hovered() {
		b.Opacity(.9)
	}
	b.Children(func() {
		components.Icon(c, iconValue).Size(16, 16).TextColor(foreground)
	})
	return b
}

func jAgentComposerAddButton(c *ui.Context, p palette) ui.Element {
	label := text("chatAttachmentUnavailable")
	button := ui.ButtonBase(c).Size(28, 28).Shrink(0).Radius(10).
		Center().Background(ui.Color{}).Label(label)
	if button.Hovered() || button.Focused() {
		button.Background(p.surfaceHover)
	}
	button.Children(func() {
		components.Icon(c, lu.Plus).Size(16, 16).TextColor(p.muted)
	})
	components.Tooltip(c, button, label)
	return button
}

func (s *appState) jAgentTurnView(
	c *ui.Context,
	index int,
	prompt, message *chatMessage,
	live bool,
) {
	p := themePaletteForAppearance(
		appearancePresetJAgent,
		true,
		s.highContrastActive(currentAppearanceHighContrast()),
		currentAppearanceReduceTransparency(),
	)
	ui.Column(c).Key(fmt.Sprintf("j-agent-turn-%d", index)).
		FillWidth().MinWidth(0).Shrink(0).Gap(12).Children(func() {
		promptText := ""
		if prompt != nil {
			s.jAgentPromptPill(c, p, prompt)
			promptText = messageText(prompt)
		}
		if message != nil {
			s.jAgentAssistantTurn(c, p, index, message, promptText, live)
		}
	})
}

func (s *appState) jAgentPromptPill(c *ui.Context, p palette, prompt *chatMessage) {
	textValue := messageText(prompt)
	ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
		pill := ui.Column(c).MinWidth(0).MaxWidth(540).
			Padding(10, 16, 10, 16).Gap(0)
		pill = jAgentGlassSurface(pill, p, 28, 14, ui.RGBA(27, 27, 27, .58))
		pill.Children(func() {
			components.Text(c, textValue).Selectable().FontSize(p.bodyFont).
				LineHeight(1.5).TextColor(p.text)
		})
	})
}

func (s *appState) jAgentAssistantTurn(
	c *ui.Context,
	p palette,
	index int,
	message *chatMessage,
	promptText string,
	live bool,
) {
	status := assistantTurnStatus(message, live)
	elapsed := turnElapsed(c, message, live)
	tools := 0
	for _, block := range message.blocks {
		if block.kind == chatBlockTool {
			tools++
		}
	}
	plan := planAssistantTurn(message, live)
	state := s.chatView.turn(index)
	foldable := status == chat.Completed && plan.final >= 0 && len(plan.steps) > 0
	open := true
	turnID := fmt.Sprintf("turn-%d", index)
	hasSteps := len(plan.steps) > 0
	clickableSummary := hasSteps && (status == chat.Running || foldable)
	if clickableSummary {
		open = state.Expanded(status, plan.final >= 0, !s.chatView.conversation.AtLatest())
	}

	if hasSteps || status == chat.Running {
		label := textArgs("chatWorkedFor", map[string]any{"duration": elapsed})
		if status == chat.Running {
			label = textArgs("chatWorkingFor", map[string]any{"duration": elapsed})
		}
		s.jAgentWorkSummary(c, p, turnID+"-work-summary", label, open, clickableSummary, func() {
			state.SetExpanded(!open)
			s.pauseJAgentConversation(c)
		})
	}

	hasCard := (hasSteps && open) || plan.final >= 0 ||
		status == chat.Failed || status == chat.Cancelled
	if hasCard {
		card := ui.Column(c).FillWidth().MinWidth(0).Padding(12).Gap(12)
		card = jAgentGlassSurface(card, p, 12, 14, ui.RGBA(27, 27, 27, .72))
		card.Children(func() {
			if hasSteps && open {
				ui.Column(c).FillWidth().MinWidth(0).Gap(10).Children(func() {
					for _, step := range plan.steps {
						view := s.turnStep(turnID, message, step, live)
						if view.Message != nil {
							view.Message(c)
						}
					}
				})
			}
			if plan.final >= 0 {
				markdown.View(c, message.blocks[plan.final].text, &s.chatView.markdown).
					FillWidth()
			}
			if status == chat.Failed || status == chat.Cancelled {
				label, failed := turnEndLabel(message.status), status == chat.Failed
				turnFooter(c, label+" - "+elapsed, failed)
			} else if status == chat.Completed {
				s.turnCompletionFooter(c, message, elapsed, tools, promptText)
			}
		})
	}
}

func (s *appState) pauseJAgentConversation(c *ui.Context) {
	s.chatView.jAgentPaused = true
	s.chatView.jAgentPauseY = s.chatView.jAgentBuildY
	s.chatView.conversation.Scroll.Y = s.chatView.jAgentBuildY
	c.Invalidate()
}

func (s *appState) jAgentWorkSummary(
	c *ui.Context,
	p palette,
	key string,
	label string,
	open, clickable bool,
	toggle func(),
) {
	var button ui.Element
	var row ui.Element
	if clickable {
		button = ui.ButtonBase(c.Key(key)).Label(label).
			Expanded(open).FillWidth().MinHeight(44).Padding(0, 12).
			AlignItems(ui.Center).Gap(8).Background(ui.Color{})
		if button.Hovered() || button.Focused() {
			button.Background(p.text.Alpha(.05))
		}
		row = jAgentGlassSurface(button, p, 12, 16, ui.RGBA(27, 27, 27, .82))
	} else {
		row = ui.Row(c).FillWidth().MinHeight(44).Padding(0, 12).
			AlignItems(ui.Center).Gap(8)
		row = jAgentGlassSurface(row, p, 12, 16,
			ui.RGBA(27, 27, 27, .82))
	}
	row.Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Background(p.text.Alpha(.55))
		components.Text(c, label).FontSize(p.bodyFont).FontWeight(500).
			TextColor(p.text).Grow(1).MinWidth(0).SingleLine()
		if clickable {
			chevron := lu.ChevronRight
			if open {
				chevron = lu.ChevronDown
			}
			components.Icon(c, chevron).Size(12, 12).TextColor(p.muted)
		}
	})
	if clickable && button.Clicked() {
		toggle()
	}
}
