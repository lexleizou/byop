package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/oneo-me/onevra/source/components"
	lu "github.com/oneo-me/onevra/source/icons/lucide"
)

// P30 2: a closed Mermaid block renders in place. The card is the host's own
// chrome — header, toolbar, states — and its body is the system WebView seat
// A drives. Nothing here depends on the Mermaid plugin: the host built the
// renderer in, and the plugin page stays available as the standalone view for
// whoever installed it.
//
// The card keeps only presentation state (which toolbar switches are on, the
// width the body was drawn at, where the card sat last frame). The diagram's
// own state — status, natural size, view, scale for the page — is seat A's.

const (
	diagramCardMaxWidth      float32 = 720 // the renderer's own maxWidth
	diagramHeaderHeight      float32 = 32
	diagramHeaderPaddingY    float32 = 6
	diagramHeaderPaddingX    float32 = 10
	diagramToolbarButtonSize float32 = 26
	diagramToolbarIconSize   float32 = 14
	diagramToolbarGap        float32 = 2
	diagramToolbarFadeIn             = 150 * time.Millisecond
	diagramBodyMinHeight     float32 = 48
	diagramBodyMaxHeight     float32 = 560
	diagramBodyUnknownHeight float32 = 160 // no cached size yet
	diagramZoomFactor        float32 = 1.25
	diagramScaleMin          float32 = 0.25
	diagramScaleMax          float32 = 4
	diagramViewportBuildBand float32 = 1 // viewport heights above/below
	diagramViewportKeepBand  float32 = 2 // release band, P30 2.7
	diagramSourcePaddingY    float32 = 8
	diagramSourcePaddingX    float32 = 4
	diagramMessagePaddingY   float32 = 10
	diagramMessagePaddingX   float32 = 12
)

// diagramCardState is one card's UI state across frames, kept per source so a
// history reload and a live turn get the same card, and so a released view
// comes back at the zoom the reader left it at.
type diagramCardState struct {
	fitWidth   bool    // the default: fit the body to the card width
	showSource bool    // the "view source" switch
	scale      float32 // the scale the zoom buttons drive, when not fitting
	// scaleValue and scaleSent are what the page was last told. The scale is
	// re-sent whenever the value moves — a resize, or the natural size
	// arriving — and never on a frame that changed nothing.
	scaleValue float32
	scaleSent  bool
	innerWidth float32 // the width the body row really drew at
	// widthKnown separates "the body drew at this width" from the zero value,
	// so a card is never fitted to a width it has not been given yet.
	widthKnown bool
	cardHeight float32 // the height the body row last drew
	// bodyY is the body's window Y from the previous frame, used to keep a
	// native page out of the composer's band.
	bodyY float32
	// contentY is the card's distance from the transcript content's top edge,
	// taken from the previous frame's layout (Element.Bounds). Unlike a paint
	// rect it exists for a card the viewport does not show, which is what makes
	// the reclaim rule work for the cards above and below the reading point.
	contentY     float32
	contentKnown bool
	// bodyHeight is the height the card asked for last frame. A card that
	// changes it while wholly above the reader shifts the text they are
	// looking at, and the card compensates by that difference.
	bodyHeight float32
	lastFrame  int // freed with the transcript, like the file previews
}

func (s *appState) diagramCardState(source string) *diagramCardState {
	if s.chatView.diagramCards == nil {
		s.chatView.diagramCards = map[string]*diagramCardState{}
	}
	key := inlineDiagramKey(source)
	state := s.chatView.diagramCards[key]
	if state == nil {
		state = &diagramCardState{fitWidth: true}
		s.chatView.diagramCards[key] = state
	}
	return state
}

// dropStaleDiagramCardStates frees the card states the transcript stopped
// building. It runs with dropStaleChatFilePreviews at the start of a frame, on
// the same frame-number rule: a card that did not draw this frame loses its
// state, so a session that scrolls past fifty diagrams keeps only the live
// ones. The diagram's own size cache belongs to seat A and stays.
func (s *appState) dropStaleDiagramCardStates() {
	for key, state := range s.chatView.diagramCards {
		if state.lastFrame != s.chatView.previewFrame {
			delete(s.chatView.diagramCards, key)
		}
	}
}

// chatScrollY is the transcript's scroll offset as a number the layout can use.
// The conversation keeps +Inf there while it follows the latest turn, before
// its own layout has measured the content, so an unset or pinned offset reads
// as the bottom.
func (s *appState) chatScrollY() float32 {
	scroll := s.chatView.conversation.Scroll.Y
	maxY := s.chatView.conversation.Scroll.MaxY
	if math.IsInf(float64(scroll), 1) || math.IsNaN(float64(scroll)) || scroll > maxY {
		scroll = maxY
	}
	if scroll < 0 || math.IsInf(float64(scroll), 0) || math.IsNaN(float64(scroll)) {
		scroll = 0
	}
	return float32(scroll)
}

// diagramMinVisibleRow is the least a clipped page may show: below it the card
// draws nothing rather than a sliver of diagram.
const diagramMinVisibleRow float32 = 8

// composerRoom reports how much of the card's body the reader can see above the
// floating composer, and whether that is less than the body's height. A body
// that does not reach the composer keeps its whole height and is not clipped.
func (state *diagramCardState) composerRoom(s *appState) (float32, bool) {
	if state == nil || state.bodyY <= 0 || !s.chatView.composerTopKnown {
		return 0, false
	}
	height := state.cardHeight
	if height <= 0 {
		height = diagramBodyUnknownHeight
	}
	room := s.chatView.composerTop - state.bodyY
	if room >= height {
		return height, false
	}
	return room, true
}

// diagramCardNearViewport decides whether a card is close enough to the
// reading area to hold a WebView. Positions are viewport-relative, so the card
// is near when it intersects the viewport grown by one viewport height on each
// side (P30 2.7); a card that already holds a view keeps it until it is two
// viewport heights away, so scrolling back does not re-render a diagram.
func diagramCardNearViewport(cardY, cardHeight, viewportHeight float32, built bool) bool {
	if viewportHeight <= 0 {
		// No viewport measured yet: keep the card, since dropping every view
		// on a frame that cannot answer would cost more than it saves.
		return true
	}
	band := diagramViewportBuildBand
	if built {
		band = diagramViewportKeepBand
	}
	top := -band * viewportHeight
	bottom := (band + 1) * viewportHeight
	return cardY < bottom && cardY+cardHeight > top
}

// diagramFitScale is the "fit width" scale: never above 1, because a diagram
// smaller than the card stays at its natural size.
func diagramFitScale(innerWidth, naturalWidth float32) float32 {
	if innerWidth <= 0 || naturalWidth <= 0 {
		return 1
	}
	if innerWidth >= naturalWidth {
		return 1
	}
	return diagramClampScale(innerWidth / naturalWidth)
}

func diagramClampScale(scale float32) float32 {
	if math.IsNaN(float64(scale)) || scale <= 0 {
		return 1
	}
	return min(max(scale, diagramScaleMin), diagramScaleMax)
}

// diagramZoomScale steps the manual zoom by 1.25 in either direction.
func diagramZoomScale(scale float32, zoomIn bool) float32 {
	if zoomIn {
		return diagramClampScale(scale * diagramZoomFactor)
	}
	return diagramClampScale(scale / diagramZoomFactor)
}

// diagramBodyHeight is the height the body row holds: the diagram's natural
// height at the current scale, bounded so one huge chart cannot push the
// transcript off the screen. unknown is what a card shows before the renderer
// reports a size — the cached height if there is one, 160 otherwise.
func diagramBodyHeight(naturalHeight, scale, unknown float32) float32 {
	if naturalHeight <= 0 {
		return unknown
	}
	return min(max(naturalHeight*scale, diagramBodyMinHeight), diagramBodyMaxHeight)
}

// diagramBodyScale resolves the scale the body draws with, and reports the one
// the page must be told about.
func diagramBodyScale(state *diagramCardState, innerWidth, naturalWidth float32) float32 {
	if state == nil {
		return 1
	}
	if state.fitWidth {
		return diagramFitScale(innerWidth, naturalWidth)
	}
	return diagramClampScale(state.scale)
}

// diagramFontFamily names the font the page draws with: the body family a
// theme resource selected, or the system UI font. The size stays the process
// body size, so the diagram reads at the same scale as its own answer.
func diagramFontFamily() string {
	if body, _ := themeFontsForSelection(currentPluginThemeOverlay()); body != "" {
		return body
	}
	return "system-ui, sans-serif"
}

// inlineDiagramThemeFor maps the palette onto the page's colour variables
// (P30 2.5). The page paints on an opaque surface even under the J Agent
// wallpaper, so every translucent colour is flattened onto that surface
// before it is handed over as a "#rrggbb".
func inlineDiagramThemeFor(p palette, tones p29Tones) inlineDiagramTheme {
	surface := compositeOver(p.surface, p.surface)
	ink := func(color ui.Color) string {
		return pluginColorHex(compositeOver(color, surface))
	}
	border := p.border
	if p.highContrast {
		// High contrast draws lines and borders in the text colour.
		border = p.text
	}
	return inlineDiagramTheme{
		Background: ink(p.surface),
		Text:       ink(p.text),
		Muted:      ink(tones.meta),
		Surface:    ink(p.raisedSurface),
		SurfaceAlt: ink(p.surfaceHover),
		Border:     ink(border),
		Accent:     ink(p.accent),
		Danger:     ink(p.danger),
		// The palette has no chart ramp, so the five slots take the semantic
		// colours in the order the design names, ending in a softened text.
		Chart: [5]string{
			ink(p.accent), ink(p.success), ink(p.warning), ink(p.danger),
			ink(p.text.Mix(p.surface, .4)),
		},
		FontFamily:   diagramFontFamily(),
		FontSize:     p.chatFont,
		HighContrast: p.highContrast,
	}
}

// inlineDiagramCard draws the card for one qualified Mermaid block. The block
// is still the source of the code body, so "view source" shows exactly what
// the model wrote, and the card never rewrites it.
func (s *appState) inlineDiagramCard(
	c *ui.Context,
	p palette,
	tones p29Tones,
	block *markdown.CodeBlockView,
	source string,
) {
	d := s.inlineDiagramFor(source)
	state := s.diagramCardState(source)
	state.lastFrame = s.chatView.previewFrame
	theme := inlineDiagramThemeFor(p, tones)
	// One WebView shows in one place per frame, so the first card of a source
	// claims it and any later card of the same source gives the reader the
	// source text instead of an empty box.
	if s.chatView.diagramClaims == nil {
		s.chatView.diagramClaims = map[string]int{}
	}
	s.chatView.diagramClaims[d.Key]++
	primary := s.chatView.diagramClaims[d.Key] == 1
	// A palette change reaches the pages through setTheme alone (P30 2.5): the
	// card that first sees a new theme hands it to seat A, which forwards it to
	// every alive page, and no diagram is re-parsed. A card's first frame only
	// records the theme, because the view it builds already carries it.
	if !s.chatView.diagramThemeKnown {
		s.chatView.diagramTheme = theme
		s.chatView.diagramThemeKnown = true
	} else if s.chatView.diagramTheme != theme {
		s.chatView.diagramTheme = theme
		s.setInlineDiagramTheme(theme)
	}
	reduced := s.reducedMotionActive(currentAppearanceReduceMotion())
	key := "diagram-" + d.Key[:12]

	// Build or keep the view when the card is near the reading area. The
	// position is the previous frame's layout, which MyGo reports for a card
	// the viewport clipped as well, so a history of ten diagrams does not
	// ensure, cap out at six views, release and ensure again on every idle
	// frame: the cards outside the band are simply not built, and seat A frees
	// their views. A card that has never been laid out is built at once, so a
	// diagram that appears on the first frame after a history load renders
	// without waiting for another scroll. A source over the shared render limit
	// is never given a view at all.
	card := ui.Column(c).Key(key).FillWidth().MinWidth(0).
		MaxWidth(diagramCardMaxWidth).Clip()
	card = styleRaisedSurface(card, p, 8)
	previous := card.Bounds()
	// contentY is the card's top in the transcript's content coordinates: the
	// distance from the content's own first row, which scrolls with it. The
	// viewport position is that minus the scroll, and it is what the build band
	// and the reclaim rule compare, so a card far down a long history is judged
	// by where the reader is, not by how far into the content it sits. Bounds
	// is reported for a card the viewport clipped as well, which is what makes
	// this work for cards the reader cannot see.
	if previous.H > 0 && s.chatView.transcriptOriginKnown {
		state.contentY = previous.Y - s.chatView.transcriptOriginY
		state.contentKnown = true
	}
	viewportY := state.contentY - s.chatScrollY()
	_, viewportHeight := c.Size()
	tooLong := utf16CodeUnits(source) > inlineDiagramMaxSourceUnits
	built := d.Status != inlineDiagramIdle
	near := !state.contentKnown || diagramCardNearViewport(
		viewportY, previous.H, viewportHeight, built)

	if near && !tooLong {
		s.ensureInlineDiagram(d, theme, s.chatView.previewFrame)
	}

	var toolbar ui.Element
	hot := false // any toolbar button hovered or focused this frame
	showBody := !tooLong && d.Status != inlineDiagramFailed
	_, _, _, pluginOpen := s.mermaidPreviewAvailability()
	// The body is the width the card really has, and it is not known until
	// the frame draws. Until then the diagram shows at its natural size, so a
	// card built in a narrow window is not scaled for the wrong width.
	knownWidth := state.innerWidth
	if !state.widthKnown {
		knownWidth = 0
	}
	scale := diagramBodyScale(state, knownWidth, d.NaturalW)

	card.Children(func() {
		label := text("diagramCardLabel")
		if d.Agentflow {
			label = text("diagramCardLabelFlow")
		}
		header := ui.Row(c).Key(key+"-header").Label(label).FillWidth().
			MinWidth(0).Height(diagramHeaderHeight).
			Padding(diagramHeaderPaddingY, diagramHeaderPaddingX).
			AlignItems(ui.Center)
		header.Children(func() {
			components.Text(c, label).FontSize(fontCaption).LineHeight(1.2).
				TextColor(tones.meta).Shrink(0).SingleLine()
			ui.Spacer(c).Grow(1)
			toolbar = ui.Row(c).Key(key + "-toolbar").MinWidth(0).Shrink(0).
				Gap(diagramToolbarGap).AlignItems(ui.Center)
			toolbar.Children(func() {
				if showBody {
					if diagramToolButton(c, p, tones, key+"-out",
						text("diagramZoomOut"), lu.ZoomOut, false).Clicked() {
						scale = diagramZoomScale(scale, false)
						state.fitWidth = false
						state.scale = scale
						s.sendDiagramScale(state, d, scale)
					}
					if diagramToolButton(c, p, tones, key+"-in",
						text("diagramZoomIn"), lu.ZoomIn, false).Clicked() {
						scale = diagramZoomScale(scale, true)
						state.fitWidth = false
						state.scale = scale
						s.sendDiagramScale(state, d, scale)
					}
					if diagramToolButton(c, p, tones, key+"-fit",
						text("diagramFitWidth"), lu.Maximize2, state.fitWidth).Clicked() {
						state.fitWidth = true
						scale = diagramFitScale(knownWidth, d.NaturalW)
						s.sendDiagramScale(state, d, scale)
					}
				}
				if diagramToolButton(c, p, tones, key+"-source",
					text("diagramViewSource"), lu.Code, state.showSource).Clicked() {
					state.showSource = !state.showSource
				}
				if diagramToolButton(c, p, tones, key+"-copy",
					text("diagramCopySource"), lu.Copy, false).Clicked() {
					c.Services().WriteClipboard(source)
					c.Toast(text("diagramCopied"))
				}
				if showBody && pluginOpen {
					if diagramToolButton(c, p, tones, key+"-page",
						text("diagramOpenInPage"), lu.ExternalLink, false).Clicked() {
						s.openMermaidPreview(source)
					}
				}
			})
			hot = toolbar.Hovered()
		})

		switch {
		case tooLong:
			diagramNotice(c, tones.meta, text("diagramTooLong"))
			diagramSourceBody(c, block)
		case d.Status == inlineDiagramFailed:
			diagramNotice(c, tones.dangerText, diagramFailureText(d))
			diagramSourceBody(c, block)
		case !primary:
			// The same source already renders in another card this frame.
			diagramSourceBody(c, block)
		default:
			bodyH := diagramBodyHeight(d.NaturalH, scale, diagramBodyUnknownHeight)
			// A card that sits wholly above the reading point and changes height
			// — the placeholder giving way to the rendered size, a cached size,
			// a zoom — would otherwise push the text the reader is looking at
			// down by that difference. Shift the scroll to keep it still; bottom
			// following, where the newest answer is the point of interest, is
			// left alone.
			if state.bodyHeight > 0 && bodyH != state.bodyHeight && state.contentKnown &&
				viewportY+previous.H <= 0 &&
				!s.chatView.conversation.AtLatest() {
				s.chatView.conversation.Scroll.Y += bodyH - state.bodyHeight
			}
			state.bodyHeight = bodyH
			body := ui.Row(c).Key(key + "-body").FillWidth().MinWidth(0).
				Height(bodyH).
				Background(compositeOver(p.surface, p.surface)).
				AlignItems(ui.Center).Justify(ui.Center).Clip()
			body.Draw(func(_ *ui.Painter, r ui.Rect) {
				state.cardHeight = r.H
				state.bodyY = r.Y
				if r.W <= 0 {
					return
				}
				state.innerWidth = r.W
				state.widthKnown = true
				// The page needs the scale for the width the body really has,
				// and the same scale the body height is drawn with. Sending it
				// whenever the computed value moves covers both the resize (a
				// narrow window) and the render completing (the natural size
				// arriving), and sends nothing on a frame that changed neither.
				desired := diagramBodyScale(state, r.W, d.NaturalW)
				state.innerWidth = r.W
				if !state.scaleSent || math.Abs(float64(desired-state.scaleValue)) > 0.001 {
					s.sendDiagramScale(state, d, desired)
				}
			})
			body.Children(func() {
				if d.Status == inlineDiagramReady {
					// The page holds the rendered SVG, so the body shows it. A
					// ready diagram whose view was released while off screen keeps
					// the surface blank at its height until the view comes back,
					// rather than claiming to still be working.
					if primary && inlineDiagramViewAlive(d.WebView) {
						// A native page paints above the window's content, so the
						// blur the transcript draws over the composer cannot
						// reach it. The page is cut to the room above the
						// composer's top edge instead: a card scrolling under the
						// composer keeps showing the part the reader can still
						// see, and one wholly under it shows nothing.
						if room, clipped := state.composerRoom(s); clipped {
							if room >= diagramMinVisibleRow {
								ui.Box(c).FillWidth().Height(room).Shrink(0).
									AlignItems(ui.Start).Clip().Children(func() {
									ui.WebView(c, d.WebView).FillWidth().Height(bodyH)
								})
							}
						} else {
							ui.WebView(c, d.WebView).FillWidth().Grow(1)
						}
					}
					return
				}
				// Waiting, rendering, or a view released while off screen: the
				// bounded placeholder keeps its height, so the transcript does
				// not jump when the view comes back.
				components.Text(c, text("diagramRendering")).FontSize(fontCaption).
					LineHeight(1.2).TextColor(tones.meta)
			})
			if state.showSource {
				diagramSourceBody(c, block)
			}
		}
	})

	// The toolbar stays in the accessibility tree and clickable at all times;
	// only its paint fades in when the pointer or the keyboard arrives.
	revealed := float32(0)
	if card.Hovered() || hot {
		revealed = 1
	}
	if reduced {
		toolbar.Opacity(revealed)
	} else {
		toolbar.Opacity(toolbar.AnimateTo(key+"-toolbar-fade", revealed,
			ui.Animation{Duration: diagramToolbarFadeIn, Ease: ui.EaseOut}))
	}
}

// sendDiagramScale hands a scale to the page and remembers that this is what
// the page has, so the draw callback does not send the same value again.
func (s *appState) sendDiagramScale(state *diagramCardState, d *inlineDiagram, scale float32) {
	s.setInlineDiagramScale(d, scale)
	state.scaleValue = scale
	state.scaleSent = true
}

// diagramFailureText states what the renderer reported, with the line when it
// reported one.
func diagramFailureText(d *inlineDiagram) string {
	message := fallback(d.Err, text("diagramFailedNoDetail"))
	value := textArgs("diagramFailed", map[string]any{"message": message})
	if d.ErrLine > 0 {
		value += " " + textArgs("diagramFailedLine", map[string]any{"line": d.ErrLine})
	}
	return value
}

// diagramNotice is the one-line message that replaces the body.
func diagramNotice(c *ui.Context, color ui.Color, message string) {
	ui.Row(c).FillWidth().MinWidth(0).
		Padding(diagramMessagePaddingY, diagramMessagePaddingX).
		AlignItems(ui.Center).Children(func() {
		components.Text(c, message).FontSize(fontSmall).LineHeight(1.4).
			TextColor(color).Selectable().Grow(1).MinWidth(0)
	})
}

// diagramSourceBody shows the block's own code body under the diagram, the
// same renderer the plain code block uses, so the source is never retyped or
// reformatted by the card.
func diagramSourceBody(c *ui.Context, block *markdown.CodeBlockView) {
	if block == nil {
		return
	}
	ui.Row(c).FillWidth().MinWidth(0).
		Padding(diagramSourcePaddingY, diagramSourcePaddingX).
		Children(func() { block.Body(c) })
}

// diagramToolButton is one toolbar button: 26 DIP square, a 14 DIP icon, a
// tooltip naming it, and a filled state for the switches.
func diagramToolButton(
	c *ui.Context,
	p palette,
	tones p29Tones,
	key, label string,
	iconValue func() *ui.SVG,
	active bool,
) ui.Element {
	button := ui.ButtonBase(c.Key(key)).Label(label).Size(diagramToolbarButtonSize,
		diagramToolbarButtonSize).Shrink(0).Radius(7).Center().
		Background(ui.Color{}).Cursor(ui.CursorPointer)
	color := tones.meta
	switch {
	case button.Hovered() || button.Focused():
		button.Background(p.surfaceHover)
		color = p.text
	case active:
		button.Background(p.text.Alpha(.12))
		color = p.text
	}
	button.TextColor(color)
	button.Children(func() {
		components.Icon(c, iconValue).Size(diagramToolbarIconSize, diagramToolbarIconSize).
			TextColor(color)
	})
	components.Tooltip(c, button, label)
	return button
}
