package main

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/components/chat"
	lu "github.com/oneo-me/onevra/source/icons/lucide"
)

// P29 view: the commercial conversation process rendered natively. One
// aggregated turn shows its header (model/time plus the preserved output
// tokens and live t/s capsule), the process summary, the timeline of
// activities and commentary, and the final answer below the process. Both
// appearance presets take this path; only the prompt block and the outer
// colours stay preset-specific.

// The process scale authorised for this stage. Everything outside the
// process keeps the old 12/14/18 rule.
const (
	p29FontBody   float32 = 15
	p29FontTitle  float32 = 16
	p29FontSub    float32 = 12
	p29LineHeight float32 = 1.72
)

// p29TurnState keeps one turn's presentation across frames, so folds,
// detail choices, status throttles and scroll anchors survive every rebuild.
// The whole chatPresentation resets when the session changes.
type p29TurnState struct {
	choice       uint8 // whole process: 0 auto, 1 open, 2 closed
	folded       bool  // the settle fold ran once
	seen         bool  // the turn has been rendered at least once
	seenLive     bool  // the turn was watched while running
	answerShown  bool  // the answer block exists; the reveal was armed
	answerPoseAt time.Time
	revealDone   bool
	coord        *p29Coord // shared-parent reveal in flight
	coordStarted bool
	coordFaded   bool
	wasLive      bool
	revealH      float32 // height the reveal wrapper drew last frame
	naturalH     float32 // natural height of the wrapper's content
	answerH      float32 // natural height of the answer column alone
	appeared     map[string]bool
	activityOpen map[string]bool
	rowOpen      map[string]bool
	thinkOpen    map[string]bool
	statuses     map[string]*p29StatusState

	coordAnchorY       float32 // scroll when the settle coordination armed
	coordProcessBefore float32 // process height the coordination starts from
	coordTopOut        bool    // wrapper top already left the viewport
	nodeProbeH         float32 // observed native-transition box height
	nodeProbeMoving    bool
	detailProbeH       float32
	detailProbeMoving  bool
	// railCuts is filled during the build with the previous-frame rect of each
	// marker, and the rail's paint reads it to stop the thread there.
	railCuts []ui.Rect
}

func (p *chatPresentation) p29Turn(key string) *p29TurnState {
	if p.p29 == nil {
		p.p29 = map[string]*p29TurnState{}
	}
	state := p.p29[key]
	if state == nil {
		state = &p29TurnState{
			appeared:     map[string]bool{},
			activityOpen: map[string]bool{},
			rowOpen:      map[string]bool{},
			thinkOpen:    map[string]bool{},
			statuses:     map[string]*p29StatusState{},
		}
		p.p29[key] = state
	}
	return state
}

// processOpen resolves the whole-process fold: running turns stay open,
// settled turns close once the reading position allows it, and an explicit
// choice always wins and stays for later review.
func (st *p29TurnState) processOpen(status chat.Status, live, holdOpen bool) bool {
	if st.choice != 0 {
		return st.choice == 1
	}
	if live || status == chat.Running {
		return true
	}
	if !holdOpen {
		st.folded = true
	}
	return !st.folded
}

func (st *p29TurnState) statusFor(key string) *p29StatusState {
	if st.statuses[key] == nil {
		st.statuses[key] = &p29StatusState{}
	}
	return st.statuses[key]
}

func (st *p29TurnState) thinkingOpen(key string) bool { return st.thinkOpen[key] }

func (st *p29TurnState) setThinkingOpen(key string, open bool) { st.thinkOpen[key] = open }

// p29TurnCardTint and the turn card geometry are P30 1.1: the J Agent preset
// carries each turn on a dark glass card again, so the text never sits on the
// wallpaper itself. The BYOP preset keeps the commercial card-less layout.
var p29TurnCardTint = ui.RGBA(27, 27, 27, .80)

const (
	p29TurnCardRadius   float32 = 12
	p29TurnCardBlur     float32 = 14
	p29TurnCardPaddingY float32 = 12
	p29TurnCardPaddingX float32 = 16
)

// p29TurnCard wraps one side of a turn in the glass card, or hands back the
// content unchanged on the BYOP preset. The card itself never animates: the
// reveal and fold animations keep running on the container inside it, and the
// card only grows and shrinks with its content.
func p29TurnCard(c *ui.Context, p palette, content func()) {
	if p.preset != appearancePresetJAgent {
		content()
		return
	}
	card := ui.Column(c).FillWidth().MinWidth(0).
		Padding(p29TurnCardPaddingY, p29TurnCardPaddingX)
	card = jAgentGlassSurface(card, p, p29TurnCardRadius, p29TurnCardBlur, p29TurnCardTint)
	card.Children(content)
}

// p29Transcript groups the transcript by real user input: a user message
// starts a turn and the assistant messages that follow — including the
// tool-only ones, which never start a turn of their own — answer it. Both
// presets share the commercial chrome: a 35px header row per side and the
// body indented 46px under it.
func (s *appState) p29Transcript(c *ui.Context) {
	lastAssistant := -1
	for i := range s.chat {
		if s.chat[i].role == chatAssistant {
			lastAssistant = i
		}
	}
	p := s.p29Palette(c)
	tones := p29TonesFor(p)
	// The content's own top, read from the previous frame's layout. The inline
	// diagram cards measure their distance to it, which is the same in every
	// coordinate space and — unlike a paint rect — is known for a card the
	// viewport does not show.
	origin := ui.Box(c).Key("p29-transcript-origin").Size(0, 0)
	if s.chatView.previewFrame > 0 {
		s.chatView.transcriptOriginY = origin.Bounds().Y
		s.chatView.transcriptOriginKnown = true
	}
	for i := 0; i < len(s.chat); {
		start := i
		var prompt *chatMessage
		if s.chat[i].role == chatUser {
			prompt = &s.chat[i]
			i++
		}
		var assistants []*chatMessage
		for i < len(s.chat) && s.chat[i].role == chatAssistant {
			assistants = append(assistants, &s.chat[i])
			i++
		}
		live := s.runActive() && len(assistants) > 0 && i-1 == lastAssistant
		turnKey := fmt.Sprintf("p29-turn-%d", start)
		ui.Column(c).Key(turnKey).FillWidth().MinWidth(0).Shrink(0).Gap(10).
			Margin(8, 0).Children(func() {
			if prompt != nil {
				p29TurnCard(c, p, func() {
					s.p29UserTurn(c, p, tones, turnKey, prompt)
				})
			}
			if len(assistants) > 0 {
				p29TurnCard(c, p, func() {
					s.p29AssistantTurn(c, p, tones, turnKey, prompt, assistants, live)
				})
			}
		})
	}
}

// p29UserTurn is the commercial user row: the neutral 35px avatar, the
// localized You, the message time, and the text indented under the header.
// P31 §6: the BoardUI preset draws no header at all and hands the message
// to p29UserBubble instead.
func (s *appState) p29UserTurn(c *ui.Context, p palette, tones p29Tones, turnKey string, prompt *chatMessage) {
	if p.preset == appearancePresetBoardUI {
		s.p29UserBubble(c, p, turnKey, prompt)
		return
	}
	you := text("p29UserLabel")
	ui.Column(c).Key(turnKey + "/user").FillWidth().MinWidth(0).Shrink(0).
		Gap(10).Children(func() {
		ui.Row(c).Key(turnKey + "/user-header").Label(you).FillWidth().
			MinWidth(0).Height(35).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(35, 35).Shrink(0).Radius(8.75).Center().
				Background(p.text.Alpha(.06)).Children(func() {
				components.Icon(c, lu.User).Size(16, 16).TextColor(tones.glyph)
			})
			texts := ui.Column(c).MinWidth(0).Shrink(1).Gap(0)
			texts.Children(func() {
				components.Text(c, you).FontSize(p29FontBody).FontWeight(600).
					LineHeight(p29LineHeight).TextColor(tones.label).
					SingleLine()
				if !prompt.startedAt.IsZero() {
					components.Text(c, prompt.startedAt.Format("15:04")).
						FontSize(fontSmall).LineHeight(1.2).
						TextColor(tones.meta).SingleLine()
				}
			})
		})
		ui.Column(c).FillWidth().MinWidth(0).Padding(0, 0, 0, p29BodyIndent).Gap(6).
			Children(func() {
				s.chatAttachmentChipRow(c, prompt.attachments, turnKey+"-attachments", ui.Start)
				components.Text(c, messageText(prompt)).
					FontSize(p29FontBody).LineHeight(p29LineHeight).
					TextColor(p.text).Selectable()
			})
	})
}

// p29UserBubble is the BoardUI user message (P31 §6): no avatar, no header
// row. The outer column keeps the accessible "You" name, and the message
// rides on the right of that column as a raised bubble. The bubble takes no
// minimum width, so a short message shrinks to its text; attachments that
// travelled with the message line up above it, end-aligned. The body reads
// at the assistant's 15px/1.72 — the P29 conversation-area exception.
func (s *appState) p29UserBubble(c *ui.Context, p palette, turnKey string, prompt *chatMessage) {
	windowWidth, _ := c.Size()
	// The column the bubble rides in: the transcript's content column, which
	// is the chat content width or the main area minus its horizontal
	// padding, whichever is smaller (§6).
	colW := min(p.chatContentWidth, s.chatMainWidth(windowWidth)-2*p.chatHorizontalPadding)
	bubbleMax := max(min(colW*0.8, 560), 240)
	shadow := ui.RGBA(0, 0, 0, .05)
	if p.dark {
		shadow = ui.RGBA(0, 0, 0, .14)
	}
	ui.Column(c).Key(turnKey + "/user").Label(text("p29UserLabel")).
		FillWidth().MinWidth(0).Shrink(0).Gap(10).Children(func() {
		// The row end-aligns its own chips (§6); an outer Justify cannot,
		// because the chip row fills the column width.
		s.chatAttachmentChipRow(c, prompt.attachments, turnKey+"-attachments", ui.End)
		bubble := ui.Column(c).AlignSelf(ui.End).MaxWidth(bubbleMax).
			Padding(11, 12).Radius(p.userBubbleRadius).
			Background(p.raisedSurface).Shadow(0, 1, 1, 0, shadow).Clip()
		bubble.Children(func() {
			components.Text(c, messageText(prompt)).
				FontSize(p29FontBody).LineHeight(p29LineHeight).
				TextColor(p.text).Selectable()
		})
	})
}

func (s *appState) p29Palette(c *ui.Context) palette {
	t := c.Theme()
	return themePaletteForAppearance(
		s.currentAppearancePreset(),
		t.Dark,
		s.highContrastActive(currentAppearanceHighContrast()),
		currentAppearanceReduceTransparency(),
	)
}

// p29AssistantTurn renders one aggregated turn under the commercial R4/L2
// chrome: the 35px header (avatar, model, time, and the preserved token
// and t/s extras), then the body indented 46px — process summary, the
// shared process+answer wrapper that the settle coordination animates as
// one measured parent, and the action row.
func (s *appState) p29AssistantTurn(
	c *ui.Context,
	p palette,
	tones p29Tones,
	turnKey string,
	prompt *chatMessage,
	assistants []*chatMessage,
	live bool,
) {
	reduced := s.reducedMotionActive(currentAppearanceReduceMotion())
	turn := buildP29Turn(assistants, live)
	state := s.chatView.p29Turn(turnKey)
	firstPaint := !state.seen
	// Nodes grow in only while the user watches the turn take shape; a
	// history load never replays the entrance, per the commercial rule.
	animateNodes := !firstPaint && (live || state.seenLive)
	last := assistants[len(assistants)-1]
	elapsed := p29TurnElapsed(c, assistants, live)

	// The header sits above everything and never folds with the process,
	// so the tokens and the live t/s capsule survive every collapse.
	s.p29TurnHeader(c, p, tones, turnKey, assistants, live)

	hasProcess := len(turn.nodes) > 0 || turn.status == chat.Running
	open := state.processOpen(turn.status, live, !s.chatView.conversation.AtLatest())

	body := ui.Column(c).FillWidth().MinWidth(0).Gap(10).Padding(0, 0, 0, p29BodyIndent)
	body.Children(func() {
		if hasProcess {
			s.p29ProcessSummary(c, p, tones, turnKey, state, &turn, open, reduced, elapsed)
		}
		// The settle coordination: when the watched turn stops streaming,
		// the wrapper that holds both the process and the answer measures
		// its previous and new natural heights and animates the
		// difference over the 150 ms + 500 ms curve.
		if state.wasLive && !live && state.coord == nil && !reduced &&
			(hasProcess || turn.finalKind != p29FinalNone) {
			state.coord = &p29Coord{at: c.Now()}
			state.coordStarted = false
			// The reading anchor compensates only the PROCESS shrink,
			// and only while the wrapper top has scrolled out above the
			// viewport (checked in the Draw below). Answer growth is new
			// content at or below the reading point and must never move
			// the viewport.
			state.coordAnchorY = s.chatView.conversation.Scroll.Y
			state.coordProcessBefore = max(0, state.naturalH-state.answerH)
			state.coordTopOut = false
		}
		coordOn := state.coord != nil
		if coordOn && state.coord.expired(c.Now()) {
			state.coord = nil
			state.coordStarted = false
			state.coordFaded = false
			coordOn = false
		}
		reveal := ui.Column(c).Key(turnKey + "/reveal").FillWidth().MinWidth(0).Gap(10)
		if coordOn {
			animation := state.coord.animation()
			if !state.coordStarted {
				state.coordStarted = true
				// The measured before height is what the wrapper drew
				// last frame; the after height is the content's natural
				// size, measured through the Shrink(0) child below. The
				// channel runs between them on the coordination curve,
				// exactly once per change, never restarting per token.
				reveal.Jump("p29-reveal-h", max(1, state.revealH))
				if state.coord.manual && !open {
					state.coordFaded = true
					reveal.Jump("p29-reveal-fade", 1)
				}
			}
			// Height, not MaxHeight: the box must hold the measured
			// before-height while the content already sits at its after
			// height, so the collapse and expand both run visibly.
			// During a manual fold the fading process stays built (so
			// it can fade), which means the content's natural height
			// would not shrink at all; the after-height for that window
			// is the answer column alone, measured beside the fade.
			target := state.naturalH
			if state.coord.manual && !open {
				target = state.answerH
			}
			height := reveal.AnimateTo("p29-reveal-h", target, animation)
			reveal = reveal.Height(max(1, height)).Clip()
			if state.coord.manual && !open && state.coordFaded {
				reveal.Opacity(reveal.AnimateTo("p29-reveal-fade", 0, animation))
			}
		}
		reveal.Draw(func(_ *ui.Painter, r ui.Rect) {
			state.revealH = r.H
			if state.coord == nil || state.coord.manual {
				return
			}
			if s.chatView.conversation.AtLatest() {
				return
			}
			// The drawn rect is in viewport coordinates: the top is out
			// only once it has scrolled past the window's upper edge.
			state.coordTopOut = r.Y <= 0
			// A still-open process never collapsed on settle: the drop
			// in natural−answer is text moving into the answer below
			// the viewport, which must not be "compensated" by moving
			// the reading point.
			if !state.coordTopOut || open {
				return
			}
			processNow := max(0, state.naturalH-state.answerH)
			y := state.coordAnchorY - (state.coordProcessBefore - processNow)
			if y < 0 {
				y = 0
			}
			if y != s.chatView.conversation.Scroll.Y {
				s.chatView.conversation.Scroll.Y = y
				c.Invalidate()
			}
		})
		// A manual fold keeps the process built (clipped and fading)
		// through its window; at every other time a fold builds nothing.
		showProcess := open || (coordOn && state.coord != nil &&
			state.coord.manual && !open)
		reveal.Children(func() {
			inner := ui.Column(c).FillWidth().MinWidth(0).Shrink(0)
			inner.Draw(func(_ *ui.Painter, r ui.Rect) {
				if r.H > 0 {
					state.naturalH = r.H
				}
			})
			inner.Children(func() {
				if showProcess {
					s.p29Timeline(c, p, tones, turnKey, state, &turn, live, animateNodes, reduced)
				}
				s.p29Final(c, p, tones, turnKey, state, &turn, reduced)
			})
		})
		if turn.status == chat.Failed || turn.status == chat.Cancelled {
			// The reason of a failed or cancelled turn sits under the
			// process, outside the fold, visible by default: an injected
			// 429 must never hide inside the collapsed activity list. A
			// live failure keeps its concrete error in the session slot
			// until the next send, so the latest turn surfaces it too;
			// older turns only ever show their own notices.
			noticeLines := turn.noticeLines
			if turn.status == chat.Failed && s.piError != "" &&
				latestAssistantForPrompt(s.chat, last) {
				// The concrete error replaces both the duplicate of
				// itself and the generic "see runtime details"
				// placeholder the fallback injects alongside it.
				deduped := noticeLines[:0:0]
				for _, line := range noticeLines {
					if line == s.piError || line == text("assistantErrorFallback") {
						continue
					}
					deduped = append(deduped, line)
				}
				noticeLines = append([]string{s.piError}, deduped...)
			}
			noticeColor := tones.muted
			if turn.status == chat.Failed {
				noticeColor = tones.dangerText
			}
			if len(noticeLines) > 0 {
				ui.Column(c).FillWidth().MinWidth(0).Gap(2).Children(func() {
					for _, line := range noticeLines {
						components.Text(c, line).FontSize(fontSmall).
							LineHeight(1.4).TextColor(noticeColor).Selectable()
					}
				})
			}
		}
		if !turn.live && turn.status != chat.Running {
			// The action row aligns with the body at the same 46px
			// inset (the commercial pl-[46px] action line).
			var promptText string
			var promptAttachments []chatAttachmentMeta
			if prompt != nil {
				promptText = messageText(prompt)
				promptAttachments = prompt.attachments
			}
			s.p29Actions(c, tones, &turn, last, promptText, promptAttachments)
		}
	})

	state.seen = true
	state.wasLive = live
	if live {
		state.seenLive = true
	}
}

// p29TurnHeader is the commercial R4/L2 message header: a 35px avatar at
// 25% radius, the model at 15px semibold on .60 and the time at 12px on
// .38 in two lines inside 35px, and the preserved output-token and live
// t/s extras beside them. The latest answer keeps binding the current HUD
// data (unknown stays unknown, never the turn average); older turns label
// themselves from their own model and provider — resolved through the
// model catalog when the ID matches — never from the current selection.
func (s *appState) p29TurnHeader(
	c *ui.Context,
	p palette,
	tones p29Tones,
	turnKey string,
	assistants []*chatMessage,
	live bool,
) {
	t := c.Theme()
	last := assistants[len(assistants)-1]
	latest := latestAssistantForPrompt(s.chat, last)
	if latest {
		// The header renders before this turn's Markdown, so it marks the
		// message whose output-file cards may preview on their own, just
		// as the HUD row did before P29.
		s.chatView.previewMessage = last
	}
	// The current selection names the header only for a live turn, or
	// when the record carries no model of its own: a restored latest
	// turn after a composer switch keeps the model that actually
	// answered it, resolved through the catalog by provider+ID. The
	// token and t/s capsules below stay bound exactly as before.
	var model, provider, rawModel string
	for _, message := range assistants {
		if message != nil && model == "" && strings.TrimSpace(message.model) != "" {
			rawModel = strings.TrimSpace(message.model)
			provider = strings.TrimSpace(message.provider)
			model = s.p29HistoryModelName(provider, rawModel)
		}
	}
	if model == "" && (latest || live) {
		model = s.piHUDModelName()
		provider = s.piModelProvider
		rawModel = s.piModelID
	}
	var startedAt time.Time
	var tokens int64
	for _, message := range assistants {
		if message == nil {
			continue
		}
		if startedAt.IsZero() && !message.startedAt.IsZero() {
			startedAt = message.startedAt
		}
		tokens += message.outputTokens
	}
	// The latest row keeps the exact combined HUD label the tests and
	// accessibility paths expect; a history row composes its own so it
	// can never be mistaken for the live HUD string.
	label := strings.TrimSpace(model + " " + p29HeaderTimeText(startedAt))
	if latest {
		label = s.piHUDTurnLabel(last)
	}
	header := ui.Row(c).Key(turnKey + "/header").Label(label).
		FillWidth().MinWidth(0).Height(35).Gap(10).AlignItems(ui.Center).Clip()
	if latest {
		header = header.Tooltip(s.piHUDTooltip())
	}
	header.Children(func() {
		brand := modelBrand(provider, rawModel, model)
		logo := icon("pi")
		if brand != "" {
			logo = icon("brand-" + brand)
		}
		ui.Box(c).Size(35, 35).Shrink(0).Radius(8.75).Center().
			Background(p.text.Alpha(.06)).Children(func() {
			// The legacy accessibility name stays so HUD-structure tests
			// and screen-reader anchors keep working on the new chrome.
			ui.Icon(c, logo).Key("conversation-hud-model-logo").
				Label("conversation-hud-model-logo").Size(16, 16).Shrink(0).
				TextColor(p.text)
		})
		texts := ui.Column(c).MinWidth(0).Shrink(1).Gap(0)
		texts.Children(func() {
			if model != "" {
				line := ui.Row(c).MinWidth(0).Gap(4).AlignItems(ui.Center)
				line.Children(func() {
					components.Text(c, model).FontSize(p29FontBody).FontWeight(600).
						LineHeight(1.4).TextColor(tones.label).Shrink(1).
						MinWidth(0).SingleLine().Ellipsis("…").Tooltip(model)
					if latest {
						modelReleaseBadgeNode(c, s.composerModelLabelBadge(
							s.piModelProvider, s.piModelID, model))
					}
				})
			}
			if !startedAt.IsZero() {
				components.Text(c, startedAt.Format("15:04")).FontSize(fontSmall).
					LineHeight(1.2).TextColor(tones.meta).SingleLine()
			}
		})
		if latest {
			for _, item := range s.piHUDTurnItems(last) {
				switch item.kind {
				case hudItemTurnTokens:
					components.Text(c, item.value).FontSize(fontSmall).LineHeight(1.2).
						FontWeight(500).FontFeatures("tnum").TextColor(tones.muted).
						SingleLine().Shrink(0)
				case hudItemTPS:
					rate, ok := s.piHUDCurrentTPS()
					if !ok {
						continue
					}
					color, background, border := piTPSCapsuleStyle(t, p, rate)
					ui.Row(c).Key("conversation-hud-tps").Label(item.value).
						Height(20).Padding(0, 7).Radius(10).Shrink(0).
						Background(background).Border(v12BorderWidth, border).
						AlignItems(ui.Center).Children(func() {
						components.Text(c, item.value).FontSize(fontSmall).LineHeight(1).
							FontWeight(600).FontFeatures("tnum").TextColor(color)
					})
				}
			}
		} else if tokens > 0 {
			components.Text(c, "↓"+formatPiTokens(tokens)).FontSize(fontSmall).
				LineHeight(1.2).FontFeatures("tnum").TextColor(tones.muted).
				SingleLine().Shrink(0)
		}
	})
}

// p29HistoryModelName resolves a history turn's model through the model
// catalog by provider+ID when it matches, and otherwise shows the recorded
// name honestly. The current composer selection is never borrowed.
func (s *appState) p29HistoryModelName(provider, model string) string {
	for _, candidate := range s.piModels {
		if strings.TrimSpace(candidate.Provider) != "" &&
			strings.EqualFold(candidate.Provider, provider) &&
			strings.EqualFold(candidate.ID, model) &&
			strings.TrimSpace(candidate.Name) != "" {
			return strings.TrimSpace(candidate.Name)
		}
	}
	return model
}

// p29ProcessSummary is the fold control: the ListTree mark, the counts in
// 16px semibold at alpha .55, and the elapsed time that refreshes each
// second while the turn runs.
func (s *appState) p29ProcessSummary(
	c *ui.Context,
	p palette,
	tones p29Tones,
	turnKey string,
	state *p29TurnState,
	turn *p29Turn,
	open bool,
	reduced bool,
	elapsed string,
) {
	title := p29ProcessTitle(turn, elapsed)
	button := ui.ButtonBase(c.Key(turnKey+"/p29-summary")).Label(title).
		Expanded(open).FillWidth().MinWidth(0).MinHeight(30).Padding(2, 4).
		Justify(ui.Start).AlignItems(ui.Center).Gap(8).
		Background(ui.Color{}).Cursor(ui.CursorPointer)
	if button.Hovered() || button.Focused() {
		button.Background(p.text.Alpha(.04))
	}
	button.Children(func() {
		components.Icon(c, lu.ListTree).Size(18, 18).Shrink(0).
			TextColor(tones.statIcon)
		components.Text(c, title).FontSize(p29FontTitle).FontWeight(600).
			LineHeight(p29LineHeight).TextColor(tones.statTitle).
			Shrink(1).MinWidth(0).SingleLine().Ellipsis("…")
		chevron := components.Icon(c, lu.ChevronRight).Size(14, 14).Shrink(0)
		chevron.Rotate(p29ArrowRotation(chevron, open, reduced))
		chevron.TextColor(tones.chevron)
		// The commercial fold row carries no permanent arrow: it sits
		// 8px after the label, hidden, and fades in over 150 ms on
		// hover or focus of the row. Rotation keeps its own curve.
		revealed := float32(0)
		if button.Hovered() || button.Focused() {
			revealed = 1
		}
		if reduced {
			chevron.Opacity(revealed)
		} else {
			chevron.Opacity(chevron.AnimateTo("p29-chevron-in", revealed,
				ui.Animation{Duration: p29MotionArrow, Ease: ui.EaseOut}))
		}
	})
	if button.Clicked() {
		state.choice = 2
		if !open {
			state.choice = 1
		}
		if !reduced {
			// A manual fold animates its own height only. It never
			// writes Scroll.Y: the clicked summary must not move, so the
			// click keeps the scroll exactly where it was and only
			// pauses follow.
			state.coord = &p29Coord{at: c.Now(), manual: true}
			state.coordStarted = false
			state.coordFaded = false
		}
		if s.jAgentPresetActive() {
			// Pin the follow anchor the way the legacy fold does, so
			// the pending-jump sentinel cannot resume following.
			s.pauseJAgentConversation(c)
		} else {
			c.Invalidate()
		}
	}
}

func p29ProcessTitle(turn *p29Turn, elapsed string) string {
	return p29ProcessTitleParts(turn.thinkingCount, turn.toolCount,
		turn.live || turn.status == chat.Running, elapsed)
}

// p29ProcessTitleParts builds the fold title from the raw counts; tests
// share the one composition rule.
func p29ProcessTitleParts(thinking, tools int, live bool, elapsed string) string {
	parts := make([]string, 0, 3)
	if thinking > 0 {
		parts = append(parts, textArgs("p29ProcessThinking", map[string]any{
			"count": thinking,
		}))
	}
	if tools > 0 {
		parts = append(parts, textArgs("p29ProcessTools", map[string]any{
			"count": tools,
		}))
	}
	if len(parts) == 0 {
		// Nothing counted — a commentary-only process. Running turns keep
		// the working label and settled turns the worked-for label, the
		// commercial empty-stats fallbacks.
		if live {
			return textArgs("chatWorkingFor", map[string]any{"duration": elapsed})
		}
		return textArgs("chatWorkedFor", map[string]any{"duration": elapsed})
	}
	parts = append(parts, textArgs("p29ProcessDuration", map[string]any{
		"duration": elapsed,
	}))
	return strings.Join(parts, text("p29ProcessJoin"))
}

// p29BodyIndent is the commercial content indent under a turn's header row.
// It is padding on the body column, not a margin: a margin left the column at
// the full column width, so its content — the answer, a node's chevron, an
// inline diagram — hung 46 DIP past the right edge, which showed on the
// J Agent glass card and stayed invisible on the borderless BYOP page.
const (
	p29BodyIndent       float32 = 46
	p29RailInset        float32 = 7  // the rail's own distance from the body edge
	p29RailContentInset float32 = 16 // the node content's distance from the rail
)

// p29RailSegments cuts the marker ranges out of the rail's vertical span, so
// the thread stops at each marker instead of running behind it. Ranges that do
// not overlap the rail, and ones still unknown, change nothing.
func p29RailSegments(top, bottom float32, cuts []ui.Rect) [][2]float32 {
	segments := [][2]float32{{top, bottom}}
	for _, cut := range cuts {
		if cut.H <= 0 {
			continue
		}
		start, end := cut.Y, cut.Y+cut.H
		if end <= top || start >= bottom {
			continue
		}
		next := make([][2]float32, 0, len(segments)+1)
		for _, segment := range segments {
			if end <= segment[0] || start >= segment[1] {
				next = append(next, segment)
				continue
			}
			if start > segment[0] {
				next = append(next, [2]float32{segment[0], start})
			}
			if end < segment[1] {
				next = append(next, [2]float32{end, segment[1]})
			}
		}
		segments = next
	}
	return segments
}

// p29Timeline draws the 1px rail at 7px and lays out the nodes: 16px
// markers overlap the rail, the content is inset 16px, and nodes keep a
// 4px vertical gap.
func (s *appState) p29Timeline(
	c *ui.Context,
	p palette,
	tones p29Tones,
	turnKey string,
	state *p29TurnState,
	turn *p29Turn,
	live bool,
	animate bool,
	reduced bool,
) {
	// The cuts are collected for this frame only: a node that is no longer
	// built must not leave a gap in the thread.
	state.railCuts = nil
	rail := ui.Column(c).FillWidth().MinWidth(0).Gap(4).
		Padding(2, 0, 2, p29RailInset+p29RailContentInset)
	rail.Draw(func(pn *ui.Painter, r ui.Rect) {
		// The rail stops where a marker sits, so nothing has to be painted over
		// it: a filled disc looked right on an opaque page and wrong on the
		// J Agent glass card, where it read as a dark dot.
		x := r.X + p29RailInset
		for _, segment := range p29RailSegments(r.Y, r.Y+r.H, state.railCuts) {
			pn.Line(x, segment[0], x, segment[1], p29RailWidth, tones.rail)
		}
	})
	rail.Children(func() {
		for n := range turn.nodes {
			node := &turn.nodes[n]
			nodeKey := turnKey + "/" + node.id
			row := ui.Row(c).Key(nodeKey).FillWidth().MinWidth(0).Gap(8).
				AlignItems(ui.Start)
			if animate && !reduced {
				// Commercial node entrance: height and opacity over
				// 320 ms on the stage curve, run by MyGo's native
				// collapse transition. The transition measures the
				// node's natural height itself — the framework's own
				// tests show the box running 0 → half → full in
				// headless frames — so no hand-measured MaxHeight
				// chain is involved. A brand-new parent shows its
				// children at once, which keeps history silent.
				animation := p29Animation(p29MotionNodeEnter, false, p29EaseReveal())
				row = row.Transition(ui.ElementTransition{
					Animation: &animation,
					Colors:    true,
					Enter:     &ui.Motion{Collapse: true},
					Exit:      &ui.Motion{Collapse: true},
				})
				state.nodeProbeMoving = row.Transitioning()
				row.Draw(func(_ *ui.Painter, r ui.Rect) {
					state.nodeProbeH = r.H
				})
			}
			row.Children(func() {
				if node.kind == p29NodeActivity {
					s.p29ActivityNode(c, p, tones, turnKey, state, turn, node,
						live, reduced)
				} else {
					s.p29Commentary(c, node, live)
				}
			})
		}
	})
}

// p29ActivityNode renders one activity: its marker on the rail, the
// throttled status line and, on click, the detail below it.
func (s *appState) p29ActivityNode(
	c *ui.Context,
	p palette,
	tones p29Tones,
	turnKey string,
	state *p29TurnState,
	turn *p29Turn,
	node *p29Node,
	live bool,
	reduced bool,
) {
	actKey := turnKey + "/" + node.id
	isLatest := p29ActivityIsLatest(turn.nodes, p29ActivityIndexOf(turn.nodes, node))
	desired, immediate := p29ActivityStatus(turn, node, isLatest, live)
	if desired == "" {
		return
	}
	shown, _, wait := state.statusFor(actKey).advance(desired, c.Now(), reduced, immediate)
	if wait > 0 {
		c.After(wait)
	}
	open := state.activityOpen[actKey]
	steps := p29ActivityStepCount(*node)
	suffix := ""
	if !open && !live && steps > 1 {
		suffix = " " + textArgs("p29ProcessSteps", map[string]any{"count": steps})
	}

	// The 16px marker sits on the rail; the content keeps its 16px inset.
	marker := ui.Box(c).Size(16, 16).Shrink(0).Margin(5, 0, 0, -24).Center()
	// The rail leaves a gap where this marker stands, taken from the previous
	// frame's layout, which is known whether or not the marker painted.
	if bounds := marker.Bounds(); bounds.H > 0 {
		state.railCuts = append(state.railCuts, bounds)
	}
	marker.Children(func() {
		s.p29ActivityMarker(c, tones, *node, isLatest && live, reduced)
	})

	content := ui.Column(c).Grow(1).MinWidth(0).Gap(4)
	content.Children(func() {
		header := ui.ButtonBase(c.Key(actKey+"-head")).Label(shown+suffix).
			Expanded(open).FillWidth().MinWidth(0).MinHeight(24).Padding(2, 4).
			Justify(ui.Start).AlignItems(ui.Center).Gap(6).
			Background(ui.Color{}).Cursor(ui.CursorPointer)
		if header.Hovered() || header.Focused() {
			header.Background(p.text.Alpha(.04))
		}
		header.Children(func() {
			foreground := tones.status
			if isLatest && live {
				foreground = tones.statusLive
			}
			label := components.Text(c, shown+suffix).FontSize(p29FontBody).
				FontWeight(500).LineHeight(p29LineHeight).TextColor(foreground).
				Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
			if !reduced {
				// Keyed by the shown value, so a swap crossfades: the
				// old label rises and fades out, the new one comes from
				// 4 DIPs below, over the stage's 420 ms.
				label = label.Key(actKey + "-status-" + shown).
					Transition(p29StatusLabelTransition(reduced))
			}
			chevron := components.Icon(c, lu.ChevronRight).Size(11, 11).Shrink(0)
			chevron.Rotate(p29ArrowRotation(chevron, open, reduced))
			chevron.TextColor(tones.chevron)
		})
		if header.Clicked() {
			state.activityOpen[actKey] = !open
			if open {
				// Closing disarms the row stagger so the next
				// expansion really runs it again; the detail itself
				// re-enters through its transition on remount.
				for _, item := range node.items {
					delete(state.appeared, actKey+"/"+item.id+"-reveal")
				}
			}
			c.Invalidate()
		}
		if open {
			detail := ui.Column(c).FillWidth().MinWidth(0)
			if !reduced {
				// 150 ms height+opacity enter through the same native
				// collapse motion; reopening replays it on remount.
				animation := p29Animation(p29MotionDetail, false, ui.EaseOut)
				detail = detail.Transition(ui.ElementTransition{
					Animation: &animation,
					Colors:    true,
					Enter:     &ui.Motion{Collapse: true},
					Exit:      &ui.Motion{Collapse: true},
				})
				state.detailProbeMoving = detail.Transitioning()
				detail.Draw(func(_ *ui.Painter, r ui.Rect) {
					state.detailProbeH = r.H
				})
			}
			detail.Children(func() {
				body := ui.Column(c).FillWidth().MinWidth(0).Gap(8).
					Radius(8).Background(p.text.Alpha(.05)).Padding(12, 10)
				body.Children(func() {
					for i := range node.items {
						item := node.items[i]
						if item.kind == p29ItemThinking {
							s.p29ThinkingCard(c, p, tones, state, actKey+"-d"+
								fmt.Sprint(i), item.text, false, reduced)
							continue
						}
						s.p29ToolRow(c, p, tones, state, actKey, item, i, reduced)
					}
				})
			})
		}
	})
}

// p29ActivityMarker picks the rail marker: a spinner for a running tool,
// CircleX for a failure, the mapped icon otherwise, and the 6px dot for a
// thinking activity. Completion never forces a green check.
func (s *appState) p29ActivityMarker(
	c *ui.Context,
	tones p29Tones,
	node p29Node,
	active bool,
	reduced bool,
) {
	item, ok := p29ActivityLabelItem(node)
	if !ok {
		return
	}
	color := tones.icon
	if active {
		color = tones.iconLive
	}
	if item.kind == p29ItemThinking {
		dotColor := tones.dot
		if active {
			dotColor = tones.dotLive
		}
		ui.Box(c).Size(6, 6).Radius(3).Background(dotColor)
		return
	}
	switch item.tool.status {
	case toolCallRunning:
		iconEl := components.Icon(c, lu.LoaderCircle).Size(14, 14).TextColor(color)
		if !reduced {
			iconEl.Rotate(iconEl.Loop("p29-spin", time.Second, ui.Linear) * 360)
		}
	case toolCallFailed, toolCallInterrupted:
		components.Icon(c, lu.CircleX).Size(14, 14).TextColor(tones.danger)
	default:
		components.Icon(c, p29ToolIcon(item.tool.name)).Size(14, 14).TextColor(color)
	}
}

// p29Commentary keeps intermediate prose in Markdown, at the process scale.
func (s *appState) p29Commentary(c *ui.Context, node *p29Node, live bool) {
	content := ui.Column(c).Grow(1).MinWidth(0)
	content.Children(func() {
		if live {
			markdown.Stream(c, node.text, &s.chatView.p29Markdown).FillWidth()
		} else {
			markdown.View(c, node.text, &s.chatView.p29Markdown).FillWidth()
		}
	})
}

// p29ToolRow is one row inside an activity detail: the mapped icon, the
// call name with its semantic summary at 15px, the argument summary at
// 12px, an optional coloured tag, and the expandable full input and result.
func (s *appState) p29ToolRow(
	c *ui.Context,
	p palette,
	tones p29Tones,
	state *p29TurnState,
	actKey string,
	item p29Item,
	index int,
	reduced bool,
) {
	tool := item.tool
	rowKey := actKey + "/" + item.id
	row := ui.Column(c).FillWidth().MinWidth(0).Gap(4)
	if !reduced {
		// Rows take 150 ms each, the first ten staggered 30 ms apart.
		delay := time.Duration(min(index, p29MotionStaggerMax-1)) * p29MotionRowStagger
		animation := p29Animation(p29MotionDetail, false, ui.EaseOut)
		animation.Delay = delay
		progress := p29RevealProgress(row, state.appeared, rowKey+"-reveal",
			animation, reduced)
		if progress < 1 {
			row = row.Opacity(progress).Clip()
		}
	}
	row.Children(func() {
		ui.Row(c).FillWidth().MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() {
			color := tones.icon
			switch tool.status {
			case toolCallRunning:
				iconEl := components.Icon(c, lu.LoaderCircle).Size(16, 16).
					TextColor(tones.iconLive)
				if !reduced {
					iconEl.Rotate(iconEl.Loop("p29-row-spin", time.Second, ui.Linear) * 360)
				}
			case toolCallFailed, toolCallInterrupted:
				components.Icon(c, lu.CircleX).Size(16, 16).TextColor(tones.danger)
			default:
				components.Icon(c, p29ToolIcon(tool.name)).Size(16, 16).TextColor(color)
			}
			components.Text(c, fallback(strings.TrimSpace(tool.name),
				text("errorToolFallback"))).
				FontSize(p29FontBody).FontWeight(500).LineHeight(p29LineHeight).
				TextColor(tones.cardBody).Shrink(0)
			if tag, tagColor, tagged := p29ToolTag(tool.name); tagged {
				ui.Row(c).Shrink(0).Padding(1, 6).Radius(4).
					Background(tagColor.Alpha(.12)).AlignItems(ui.Center).
					Children(func() {
						components.Text(c, tag).FontSize(11).FontWeight(600).
							TextColor(tagColor)
					})
			}
			if summary := p29ToolSummary(tool); summary != "" {
				components.Text(c, summary).FontSize(p29FontSub).LineHeight(1.4).
					TextColor(tones.muted).Grow(1).MinWidth(0).SingleLine().
					Ellipsis("…").Tooltip(summary)
			} else {
				ui.Spacer(c)
			}
		})
		if summary, ok := p29EditSummary(tool); ok {
			components.Text(c, summary).FontSize(p29FontSub).TextColor(tones.muted).
				Padding(0, 0, 0, 22)
		}
		hasDetail := strings.TrimSpace(tool.arguments) != "" ||
			fallback(tool.result, tool.partial) != ""
		if !hasDetail {
			return
		}
		open := state.rowOpen[rowKey]
		detailLabel := text(p29Pick(open, "p29HideDetail", "p29ShowDetail"))
		detail := ui.ButtonBase(c.Key(rowKey+"-more")).Label(detailLabel).
			Height(22).Padding(0, 4, 0, 22).Justify(ui.Start).
			Background(ui.Color{}).Cursor(ui.CursorPointer)
		if detail.Hovered() || detail.Focused() {
			detail.Background(p.text.Alpha(.04))
		}
		detail.Children(func() {
			components.Text(c, detailLabel).FontSize(p29FontSub).TextColor(tones.muted)
		})
		if detail.Clicked() {
			state.rowOpen[rowKey] = !open
			c.Invalidate()
		}
		if open {
			ui.Column(c).FillWidth().MinWidth(0).Padding(2, 4, 2, 22).
				Children(func() {
					toolActivityContent(c, tool)
				})
		}
	})
}

// p29ThinkingCard is the dashed thinking card: Brain plus "Thinking" over
// the text, 8px radius, 14x10 padding and a 1.5px dashed border at the
// commercial 8/6 pattern. Standalone thinking shown as the turn's body
// clamps to about four lines until expanded.
func (s *appState) p29ThinkingCard(
	c *ui.Context,
	p palette,
	tones p29Tones,
	state *p29TurnState,
	cardKey string,
	value string,
	standalone bool,
	reduced bool,
) {
	expanded := standalone && state.thinkingOpen(cardKey)
	card := ui.Column(c).FillWidth().MinWidth(0).Gap(8).Padding(14, 10).Radius(8).
		Background(p.text.Alpha(.03))
	card.Draw(func(pn *ui.Painter, r ui.Rect) {
		p29PaintDashedRoundedRect(pn, ui.Rect{
			X: r.X + 0.5, Y: r.Y + 0.5, W: r.W - 1, H: r.H - 1,
		}, 8, 1.5, 8, 6, tones.dash)
	})
	card.Children(func() {
		ui.Row(c).FillWidth().MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() {
			components.Icon(c, lu.Brain).Size(16, 16).Shrink(0).
				TextColor(tones.icon)
			components.Text(c, text("p29ThinkingLabel")).FontSize(p29FontBody).
				FontWeight(500).TextColor(tones.cardTitle)
		})
		textEl := components.Text(c, value).FontSize(p29FontBody).
			LineHeight(p29LineHeight).TextColor(tones.cardBody).
			Selectable().FillWidth().MinWidth(0)
		if standalone && !expanded {
			textEl.MaxLines(4)
		}
		if standalone {
			toggleLabel := text(p29Pick(expanded, "p29ShowLess", "p29ShowMore"))
			toggle := ui.ButtonBase(c.Key(cardKey + "-more")).Label(toggleLabel).
				Height(22).Padding(0).Justify(ui.Start).Background(ui.Color{}).
				Cursor(ui.CursorPointer)
			toggle.Children(func() {
				components.Text(c, toggleLabel).FontSize(p29FontSub).
					TextColor(tones.muted)
			})
			if toggle.Clicked() {
				state.setThinkingOpen(cardKey, !expanded)
				c.Invalidate()
			}
		}
	})
}

// p29Final renders what the turn leaves below the process, never folded
// with it. The body shares the wrapper whose height the shared-parent
// coordination animates; the answer itself adds the commercial opacity
// 0-to-1 and 4 DIP rise over the same 150 ms + 500 ms curve, and only for
// a turn the user watched settle. History never replays it.
func (s *appState) p29Final(
	c *ui.Context,
	p palette,
	tones p29Tones,
	turnKey string,
	state *p29TurnState,
	turn *p29Turn,
	reduced bool,
) {
	if turn.finalKind == p29FinalNone {
		return
	}
	answer := ui.Column(c).Key(turnKey + "/final").FillWidth().MinWidth(0).Gap(6)
	answer.Draw(func(_ *ui.Painter, r ui.Rect) {
		if r.H > 0 {
			state.answerH = r.H
		}
	})
	if state.seenLive && !reduced {
		reveal := ui.Animation{
			Duration: p29MotionAnswerReveal,
			Delay:    p29MotionRevealDelay,
			Ease:     p29EaseReveal(),
		}
		if !state.answerShown {
			state.answerShown = true
			state.answerPoseAt = c.Now()
			// Pose starts armed off-screen: Fade 1 is gone and the block
			// sits 4 DIPs below its place; the next frames bring it up
			// along the same delayed curve the wrapper uses.
			answer.AnimatePose(ui.Pose{Y: 4, Fade: 1}, ui.Animation{})
			c.After(p29MotionRevealDelay + p29MotionAnswerReveal + 40*time.Millisecond)
		} else if !state.revealDone {
			answer.AnimatePose(ui.Pose{}, reveal)
			if c.Now().Sub(state.answerPoseAt) >=
				p29MotionRevealDelay+p29MotionAnswerReveal {
				state.revealDone = true
			}
		}
	}
	answer.Children(func() {
		switch turn.finalKind {
		case p29FinalIncomplete:
			components.Text(c, text("p29IncompleteOutput")).FontSize(fontSmall).
				FontWeight(500).TextColor(tones.muted)
			markdown.View(c, turn.finalText, &s.chatView.p29Markdown).FillWidth()
		case p29FinalAnswer:
			// The final answer reads at the commercial 15px/1.72 too
			// (spec item 10), on the same hook-configured options so
			// Mermaid buttons, file cards and plugin renderers stay live.
			markdown.View(c, turn.finalText, &s.chatView.p29Markdown).FillWidth()
		case p29FinalThinkingOnly:
			components.Text(c, text("p29ThinkingAsAnswerNote")).FontSize(fontSmall).
				TextColor(tones.muted)
			s.p29ThinkingCard(c, p, tones, state, turnKey+"/final-thinking",
				turn.finalText, true, reduced)
		case p29FinalNoText:
			components.Text(c, text("p29NoAnswerNote")).FontSize(fontSmall).
				TextColor(tones.muted)
		}
	})
}

// p29Actions is the settled action row: copy and retry, plus an honest
// status word for a turn that failed or stopped. The old green "Completed
// + elapsed + tool count" summary is gone — the process line above already
// carries those counts, and the commercial action area repeats nothing.
func (s *appState) p29Actions(
	c *ui.Context,
	tones p29Tones,
	turn *p29Turn,
	last *chatMessage,
	promptText string,
	promptAttachments []chatAttachmentMeta,
) {
	ui.Row(c).FillWidth().MinWidth(0).Height(26).Margin(1, 0, 0, 0).
		AlignItems(ui.Center).Gap(4).Children(func() {
		switch turn.status {
		case chat.Failed:
			components.Text(c, turnEndLabel(last.status)).FontSize(fontSmall).
				FontWeight(500).TextColor(tones.dangerText)
		case chat.Cancelled:
			components.Text(c, turnEndLabel(last.status)).FontSize(fontSmall).
				FontWeight(500).TextColor(tones.muted)
		}
		if iconActionV12(c, text("chatCopy"), lu.Copy).Clicked() {
			// Copy carries the answer, not the intermediate commentary
			// that may share the aggregated row.
			c.WriteClipboard(fallback(strings.TrimSpace(turn.finalText),
				messageText(last)))
		}
		if promptText != "" {
			if iconActionV12(c, text("retry"), lu.RotateCcw).Clicked() {
				s.composerDraft = promptText
				// Retry re-adds the files that went with the turn,
				// through the same path the picker uses, so the
				// bytes are read fresh and a file that no longer
				// exists is reported instead of dropped silently.
				// History-restored images carry no path (Pi keeps
				// no name for them), so they are the one honest gap.
				retryPaths := make([]string, 0, len(promptAttachments))
				for _, attachment := range promptAttachments {
					if attachment.Path != "" {
						retryPaths = append(retryPaths, attachment.Path)
					}
				}
				s.addComposerAttachmentPaths(retryPaths)
			}
		}
	})
}

// p29ActivityStatus turns one activity into its semantic line: running,
// completed, failed and unfinished states read as prose, never as raw
// JSON. immediate marks the labels that must not wait out the throttle.
func p29ActivityStatus(turn *p29Turn, node *p29Node, isLatest bool, live bool) (string, bool) {
	item, ok := p29ActivityLabelItem(*node)
	if !ok {
		return "", false
	}
	if item.kind == p29ItemThinking {
		if live && isLatest && !p29ActivityRunning(*node) {
			return text("p29ThinkingLive"), false
		}
		return text("p29ThinkingDone"), true
	}
	tool := item.tool
	switch tool.status {
	case toolCallRunning:
		return p29ToolLiveLabel(tool), false
	case toolCallFailed, toolCallInterrupted:
		return textArgs("p29ToolFailed", map[string]any{"name": tool.name}), true
	case toolCallCancelled:
		return textArgs("p29ToolIncomplete", map[string]any{"name": tool.name}), true
	default:
		if turn.status == chat.Failed || turn.status == chat.Cancelled {
			return textArgs("p29ToolIncomplete", map[string]any{"name": tool.name}), true
		}
		return p29ToolDoneLabel(tool), true
	}
}

// p29ToolFamily groups Pi's tool names onto the commercial label sets.
func p29ToolFamily(name string) string {
	switch strings.ToLower(name) {
	case "read":
		return "read"
	case "write":
		return "write"
	case "edit", "multiedit":
		return "edit"
	case "bash", "shell", "terminal":
		return "bash"
	case "glob":
		return "glob"
	case "grep", "search":
		return "grep"
	case "agent":
		return "agent"
	case "task":
		return "task"
	case "websearch":
		return "websearch"
	case "webfetch":
		return "webfetch"
	case "skill":
		return "skill"
	}
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "mcp") || strings.Contains(lower, "__") {
		return "mcp"
	}
	return "tool"
}

// p29ToolLiveLabel composes the running line from the family verb and the
// semantic target, staying grammatical when the target is unknown.
func p29ToolLiveLabel(tool toolCallView) string {
	target := p29ToolSummary(tool)
	var base string
	switch p29ToolFamily(tool.name) {
	case "read":
		base = text("p29ReadLive")
	case "write":
		base = text("p29WriteLive")
	case "edit":
		base = text("p29EditLive")
	case "bash":
		base = text("p29BashLive")
	case "glob":
		base = text("p29GlobLive")
	case "grep":
		base = text("p29GrepLive")
	case "agent":
		base = text("p29AgentLive")
	case "task":
		base = text("p29TaskLive")
	case "websearch":
		base = text("p29WebSearchLive")
	case "webfetch":
		base = text("p29WebFetchLive")
	case "mcp":
		base = text("p29McpLive")
	default:
		base = text("p29ToolLive")
		if target == "" {
			target = fallback(strings.TrimSpace(tool.name), text("errorToolFallback"))
		}
	}
	if target != "" {
		base += " " + target
	}
	return base
}

func p29ToolDoneLabel(tool toolCallView) string {
	switch p29ToolFamily(tool.name) {
	case "read":
		return text("p29ReadDone")
	case "write":
		return text("p29WriteDone")
	case "edit":
		return text("p29EditDone")
	case "bash":
		return text("p29BashDone")
	case "glob":
		return text("p29GlobDone")
	case "grep":
		return text("p29GrepDone")
	case "agent":
		return text("p29AgentDone")
	case "task":
		return text("p29TaskDone")
	case "websearch":
		return text("p29WebSearchDone")
	case "webfetch":
		return text("p29WebFetchDone")
	case "mcp":
		return text("p29McpDone")
	default:
		return text("p29ToolDone")
	}
}

// p29ToolIcon maps tool families onto the commercial Lucide choices.
func p29ToolIcon(name string) func() *ui.SVG {
	switch p29ToolFamily(name) {
	case "read":
		return lu.FileText
	case "write":
		return lu.FilePenLine
	case "edit":
		return lu.Pencil
	case "bash":
		return lu.Terminal
	case "glob":
		return lu.FolderSearch
	case "grep":
		return lu.Search
	case "agent":
		return lu.Bot
	case "task":
		return lu.GitBranch
	case "websearch":
		return lu.Globe
	case "webfetch":
		return lu.Download
	case "mcp":
		return lu.Plug
	case "skill":
		return lu.WandSparkles
	default:
		return lu.Wrench
	}
}

// p29ToolTag names the coloured tags the commercial activity rows show for
// MCP and Skill calls.
func p29ToolTag(name string) (string, ui.Color, bool) {
	switch p29ToolFamily(name) {
	case "mcp":
		return text("p29McpTag"), ui.Hex("#2FBF9E"), true
	case "skill":
		return text("p29SkillTag"), ui.Hex("#8B7BF0"), true
	}
	return "", ui.Color{}, false
}

// p29ToolSummary reads the semantic argument the row names: paths for file
// tools, a description or command for Bash, patterns and queries for
// searches. Missing data stays missing instead of inventing a summary.
func p29ToolSummary(tool toolCallView) string {
	arguments := strings.TrimSpace(tool.arguments)
	if arguments == "" {
		return ""
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(arguments), &fields); err == nil {
		var keys []string
		switch p29ToolFamily(tool.name) {
		case "bash":
			keys = []string{"description", "command"}
		case "glob", "grep":
			keys = []string{"pattern", "query", "path"}
		case "websearch":
			keys = []string{"query"}
		case "webfetch":
			keys = []string{"url"}
		case "mcp":
			keys = []string{"server", "uri", "path"}
		default:
			keys = []string{"path", "file_path", "filePath", "pattern", "query",
				"command", "url"}
		}
		for _, key := range keys {
			if value, ok := fields[key].(string); ok {
				if value = strings.TrimSpace(value); value != "" {
					return summarizeLine(value, 96)
				}
			}
		}
	}
	return summarizeLine(arguments, 96)
}

// p29EditSummary only reports replacement counts the result actually
// carries; nothing is guessed into a diff.
var p29EditCounts = regexp.MustCompile(`(\d+)\s+replacements?\b`)

func p29EditSummary(tool toolCallView) (string, bool) {
	if p29ToolFamily(tool.name) != "edit" {
		return "", false
	}
	result := fallback(tool.result, tool.partial)
	match := p29EditCounts.FindStringSubmatch(result)
	if match == nil {
		return "", false
	}
	return textArgs("p29EditReplacements", map[string]any{"count": match[1]}), true
}

// p29PaintDashedRoundedRect walks the rounded rectangle's centre line and
// emits 8-on, 6-off dashes at the given width.
func p29PaintDashedRoundedRect(pn *ui.Painter, r ui.Rect, radius, width, dash, gap float32, color ui.Color) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	inset := width / 2
	x0, y0 := r.X+inset, r.Y+inset
	x1, y1 := r.X+r.W-inset, r.Y+r.H-inset
	radius = min(radius, min(r.W, r.H)/2-inset)
	if radius < 0 {
		radius = 0
	}
	points := []ui.Point{
		{X: x0 + radius, Y: y0},
		{X: x1 - radius, Y: y0},
	}
	arc := func(cx, cy, from, to float32) {
		const steps = 10
		for i := 0; i <= steps; i++ {
			theta := from + (to-from)*float32(i)/steps
			points = append(points, ui.Point{
				X: cx + radius*float32(math.Cos(float64(theta))),
				Y: cy + radius*float32(math.Sin(float64(theta))),
			})
		}
	}
	arc(x1-radius, y0+radius, -math.Pi/2, 0)
	points = append(points, ui.Point{X: x1, Y: y1 - radius})
	arc(x1-radius, y1-radius, 0, math.Pi/2)
	points = append(points, ui.Point{X: x0 + radius, Y: y1})
	arc(x0+radius, y1-radius, math.Pi/2, math.Pi)
	points = append(points, ui.Point{X: x0, Y: y0 + radius})
	arc(x0+radius, y0+radius, math.Pi, math.Pi*1.5)
	points = append(points, ui.Point{X: x0 + radius, Y: y0})

	cycle := dash + gap
	mod := func(v float32) float32 {
		return float32(math.Mod(float64(v), float64(cycle)))
	}
	travel := float32(0)
	for i := 1; i < len(points); i++ {
		from, to := points[i-1], points[i]
		segment := float32(math.Hypot(float64(to.X-from.X), float64(to.Y-from.Y)))
		if segment <= 0 {
			continue
		}
		offset := mod(travel)
		remaining := segment
		cursor := float32(0)
		for remaining > 0 {
			if offset < dash {
				piece := min(dash-offset, remaining)
				t0 := cursor
				t1 := cursor + piece
				pn.Line(
					from.X+(to.X-from.X)*t0/segment,
					from.Y+(to.Y-from.Y)*t0/segment,
					from.X+(to.X-from.X)*t1/segment,
					from.Y+(to.Y-from.Y)*t1/segment,
					width, color,
				)
			}
			consumed := min(cycle-offset, remaining)
			cursor += consumed
			remaining -= consumed
			offset = mod(offset + consumed)
		}
		travel += segment
	}
}

// p29TurnElapsed reports the earliest start and latest end across the
// turn's messages, matching the commercial elapsed of the whole turn.
func p29TurnElapsed(c *ui.Context, assistants []*chatMessage, live bool) string {
	var startedAt, endedAt time.Time
	for _, message := range assistants {
		if message == nil {
			continue
		}
		if startedAt.IsZero() && !message.startedAt.IsZero() {
			startedAt = message.startedAt
		}
		if message.endedAt.After(endedAt) {
			endedAt = message.endedAt
		}
	}
	end := endedAt
	if startedAt.IsZero() {
		return textArgs("chatDurationSeconds", map[string]any{"seconds": 0})
	}
	if live || end.IsZero() {
		end = c.Now()
	}
	seconds := max(0, int(end.Sub(startedAt).Seconds()))
	if seconds < 60 {
		return textArgs("chatDurationSeconds", map[string]any{"seconds": seconds})
	}
	return textArgs("chatDuration", map[string]any{
		"minutes": seconds / 60,
		"seconds": seconds % 60,
	})
}

func p29Pick(cond bool, whenTrue, whenFalse string) string {
	if cond {
		return whenTrue
	}
	return whenFalse
}

func p29HeaderTimeText(startedAt time.Time) string {
	if startedAt.IsZero() {
		return ""
	}
	return startedAt.Format("15:04")
}
