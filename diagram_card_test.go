package main

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// P30 seat B: the inline diagram card. These tests drive the real transcript
// through ui.Tester, and seat A's own seams (the view factory, the liveness
// check, the release and the page calls) stand in for a window, so the card's
// states, geometry and rendering traffic are checked as the reader sees them.

const (
	p30DiagramSource = "graph TD\n  A[Start] --> B{Done?}\n  B -->|yes| C[Finish]\n"
	p30DiagramFence  = "```mermaid\n" + p30DiagramSource + "```"
)

// p30DiagramCalls is what the card sent to the page: view creations with the
// theme they were created with, released views, and the scale values handed to
// window.byopDiagram.setScale.
type p30DiagramCalls struct {
	creates   int
	releases  int
	scales    []float32
	themes    []inlineDiagramTheme
	published []string
}

// p30WireDiagrams replaces seat A's seams for one test and restores them
// afterwards, exactly as seat A's own tests do.
func p30WireDiagrams(t *testing.T) *p30DiagramCalls {
	t.Helper()
	calls := &p30DiagramCalls{}
	originalFactory := inlineDiagramViewFactory
	originalAlive := inlineDiagramViewAlive
	originalRelease := inlineDiagramReleaseView
	originalApply := inlineDiagramApplyEval
	inlineDiagramViewFactory = func(
		s *appState, d *inlineDiagram, theme inlineDiagramTheme, gen uint64,
	) (*mygo.WebView, error) {
		calls.creates++
		calls.themes = append(calls.themes, theme)
		return &mygo.WebView{}, nil
	}
	inlineDiagramViewAlive = func(v *mygo.WebView) bool { return v != nil }
	inlineDiagramReleaseView = func(v *mygo.WebView) { calls.releases++ }
	inlineDiagramApplyEval = func(webview *mygo.WebView, code string, timeout time.Duration) {
		calls.published = append(calls.published, code)
		if rest, ok := strings.CutPrefix(code, "return window.byopDiagram.setScale("); ok {
			value, err := strconv.ParseFloat(strings.TrimSuffix(rest, ")"), 64)
			if err != nil {
				t.Fatalf("card published an unparsable scale %q: %v", code, err)
			}
			calls.scales = append(calls.scales, float32(value))
		}
	}
	t.Cleanup(func() {
		inlineDiagramViewFactory = originalFactory
		inlineDiagramViewAlive = originalAlive
		inlineDiagramReleaseView = originalRelease
		inlineDiagramApplyEval = originalApply
	})
	return calls
}

// p30DiagramState builds a settled turn whose answer is one Mermaid block.
func p30DiagramState(t *testing.T, fence string) *appState {
	t.Helper()
	restore := stableTestAppearance(t)
	t.Cleanup(restore)
	state := layoutTestState(t)
	state.chat = []chatMessage{
		{role: chatUser, status: "Complete",
			blocks: []chatBlock{p29TextBlock("Draw the flow", "")}},
		{role: chatAssistant, status: "Complete",
			blocks: []chatBlock{p29TextBlock(fence, p29PhaseFinalAnswer)}},
	}
	return state
}

// p30Settle runs the frames a card needs: Markdown's streaming pass shows its
// content on the frame after the block first appears, and the body width is
// learned from the first draw.
func p30Settle(tt *ui.Tester) {
	tt.Frame()
	tt.Advance(16 * time.Millisecond)
	tt.Frame()
}

// p30LiveDiagram seeds the state seat A would hand the card: a diagram that is
// still rendering owns a live (here sentinel) view, while a rendered or failed
// one is seeded without a view, which is what a card sees after its view was
// released off screen. Only the rendering card publishes scales, as seat A
// forwards page calls to a live view alone.
func p30LiveDiagram(t *testing.T, state *appState, source string,
	status inlineDiagramStatus, width, height float32,
) *inlineDiagram {
	t.Helper()
	diagram := state.inlineDiagramFor(source)
	diagram.Status = status
	diagram.NaturalW = width
	diagram.NaturalH = height
	if status == inlineDiagramLoading {
		diagram.WebView = &mygo.WebView{}
	}
	return diagram
}

func p30DiagramCard(t *testing.T, fence string) (*appState, *ui.Tester, *p30DiagramCalls) {
	return p30CardWithSeed(t, fence, nil)
}

// p30CardWithSeed seeds the diagram before the first frame, because building
// the tester itself runs one: a card that is already loading, rendered or
// failed must not first be seen empty and then rebuild a view.
func p30CardWithSeed(t *testing.T, fence string,
	seed func(*appState),
) (*appState, *ui.Tester, *p30DiagramCalls) {
	t.Helper()
	calls := p30WireDiagrams(t)
	state := p30DiagramState(t, fence)
	if seed != nil {
		seed(state)
	}
	// A tall window keeps the whole turn inside the viewport, so zooming the
	// card taller cannot scroll it away between two clicks.
	tt := ui.NewTester(state.view, 1180, 1400)
	p30Settle(tt)
	return state, tt, calls
}

func p30LastScale(t *testing.T, calls *p30DiagramCalls) float32 {
	t.Helper()
	if len(calls.scales) == 0 {
		t.Fatal("the card never sent a scale to the page")
	}
	return calls.scales[len(calls.scales)-1]
}

func TestP30ClosedMermaidBlockBecomesCard(t *testing.T) {
	state, tt, calls := p30DiagramCard(t, p30DiagramFence)
	if _, ok := tt.Find(text("diagramCardLabel")); !ok {
		t.Fatalf("closed mermaid block did not become a diagram card: %#v", tt.Texts())
	}
	// The card replaces the block: no source, and no P28 preview button.
	if tt.HasText("graph TD") {
		t.Fatalf("the card still shows the source: %#v", tt.Texts())
	}
	if tt.HasText("Preview diagram") || tt.HasText("预览图解") {
		t.Fatalf("the removed P28 preview button is still drawn: %#v", tt.Texts())
	}
	if calls.creates != 1 {
		t.Fatalf("views created = %d, want one", calls.creates)
	}
	// The theme the page was created with is opaque and follows the palette.
	if len(calls.themes) != 1 {
		t.Fatalf("themes handed over = %d, want one", len(calls.themes))
	}
	if want := pluginColorHex(compositeOver(state.chatView.fileCardPalette.surface,
		state.chatView.fileCardPalette.surface)); calls.themes[0].Background != want {
		t.Fatalf("page background = %q, want the opaque surface %q",
			calls.themes[0].Background, want)
	}
}

func TestP30OpenMermaidBlockStaysSource(t *testing.T) {
	calls := p30WireDiagrams(t)
	restore := stableTestAppearance(t)
	defer restore()
	state := layoutTestState(t)
	state.run = runStreaming
	state.chat = []chatMessage{
		{role: chatUser, status: "Complete",
			blocks: []chatBlock{p29TextBlock("Draw the flow", "")}},
		{role: chatAssistant, status: "Streaming",
			blocks: []chatBlock{p29TextBlock("```mermaid\ngraph TD\n  A-->B", "")}},
	}
	tt := ui.NewTester(state.view, 1180, 1400)
	p30Settle(tt)
	if !tt.HasText("graph") {
		t.Fatalf("streaming block lost its source: %#v", tt.Texts())
	}
	if tt.HasText(text("diagramCardLabel")) {
		t.Fatalf("an unclosed fence was rendered as a diagram: %#v", tt.Texts())
	}
	if len(state.inlineDiagrams) != 0 || calls.creates != 0 {
		t.Fatalf("an open fence created diagram state: %d entries, %d views",
			len(state.inlineDiagrams), calls.creates)
	}

	// Closing the fence in the same turn turns that position into a card.
	state.chat[1].status = "Complete"
	state.chat[1].blocks[0].text = p30DiagramFence
	state.chat[1].blocks[0].phase = p29PhaseFinalAnswer
	state.run = runCompleted
	p30Settle(tt)
	if _, ok := tt.Find(text("diagramCardLabel")); !ok {
		t.Fatalf("closing the fence did not produce a card: %#v", tt.Texts())
	}
	if calls.creates != 1 {
		t.Fatalf("the closed fence built %d views, want one", calls.creates)
	}
}

// TestP30StreamingDoesNotRebuildDiagramView is the "只渲染一次" rule: a settled
// card builds its view once across frames, and two identical blocks share one
// diagram.
func TestP30StreamingDoesNotRebuildDiagramView(t *testing.T) {
	state, tt, calls := p30DiagramCard(t, p30DiagramFence)
	for i := 0; i < 5; i++ {
		tt.Advance(60 * time.Millisecond)
	}
	if calls.creates != 1 {
		t.Fatalf("the view was built %d times across frames, want once", calls.creates)
	}
	if len(state.inlineDiagrams) != 1 {
		t.Fatalf("one source produced %d diagram states", len(state.inlineDiagrams))
	}
	for _, diagram := range state.inlineDiagrams {
		if diagram.Source != p30DiagramSource {
			t.Fatalf("the card keyed the wrong source %q", diagram.Source)
		}
	}

	// Two identical blocks in one answer are the same diagram, built once.
	duplicate := p30DiagramFence + "\n\n" + p30DiagramFence
	calls2 := p30WireDiagrams(t)
	state2 := p30DiagramState(t, duplicate)
	tt2 := ui.NewTester(state2.view, 1180, 1400)
	p30Settle(tt2)
	if len(state2.inlineDiagrams) != 1 {
		t.Fatalf("two identical blocks made %d states, want one", len(state2.inlineDiagrams))
	}
	if calls2.creates != 1 {
		t.Fatalf("identical blocks built %d views, want one", calls2.creates)
	}
}

func TestP30DiagramCardStates(t *testing.T) {
	longSource := strings.Repeat("  A[label] --> B[label]\n", 1500) // > 20,000 units
	tests := []struct {
		name       string
		status     inlineDiagramStatus
		source     string
		fence      string
		err        string
		errLine    int
		wantText   string
		wantSource bool
		wantViews  int
	}{
		{
			name:      "waiting",
			source:    p30DiagramSource,
			fence:     p30DiagramFence,
			wantText:  "Rendering diagram…",
			wantViews: 1,
		},
		{
			name:      "loading",
			status:    inlineDiagramLoading,
			source:    p30DiagramSource,
			fence:     p30DiagramFence,
			wantText:  "Rendering diagram…",
			wantViews: 0, // the seeded view stays
		},
		{
			name:     "failed with line",
			status:   inlineDiagramFailed,
			source:   p30DiagramSource,
			fence:    p30DiagramFence,
			err:      "Unexpected token",
			errLine:  3,
			wantText: "Diagram could not render: Unexpected token (line 3)",
			// A failure shows the source under the notice.
			wantSource: true,
			wantViews:  0,
		},
		{
			name:       "failed without detail",
			status:     inlineDiagramFailed,
			source:     p30DiagramSource,
			fence:      p30DiagramFence,
			wantText:   "Diagram could not render: no details reported",
			wantSource: true,
			wantViews:  0,
		},
		{
			name:       "too long",
			source:     longSource,
			fence:      "```mermaid\n" + longSource + "```",
			wantText:   "Diagram too long to render.",
			wantSource: true,
			wantViews:  0, // never given a view
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, tt, calls := p30CardWithSeed(t, test.fence, func(state *appState) {
				if test.status == inlineDiagramIdle {
					return
				}
				diagram := p30LiveDiagram(t, state, test.source, test.status, 0, 0)
				diagram.Err = test.err
				diagram.ErrLine = test.errLine
			})
			if !tt.HasText(test.wantText) {
				t.Fatalf("state %q shows none of %q: %#v", test.name, test.wantText,
					tt.Texts())
			}
			if hasSource := tt.HasText("graph TD") || tt.HasText("A[label]"); hasSource != test.wantSource {
				t.Fatalf("state %q source shown = %v, want %v", test.name, hasSource,
					test.wantSource)
			}
			if calls.creates != test.wantViews {
				t.Fatalf("state %q built %d views, want %d", test.name, calls.creates,
					test.wantViews)
			}
		})
	}
}

func TestP30FailedCardIsNotRebuiltEveryFrame(t *testing.T) {
	_, tt, calls := p30CardWithSeed(t, p30DiagramFence, func(state *appState) {
		p30LiveDiagram(t, state, p30DiagramSource, inlineDiagramFailed, 0, 0).Err = "boom"
	})
	for i := 0; i < 4; i++ {
		tt.Advance(60 * time.Millisecond)
	}
	if calls.creates != 0 {
		t.Fatalf("a failed card built %d views, want none", calls.creates)
	}
	if !tt.HasText("Diagram could not render: boom") {
		t.Fatalf("failed card lost its notice: %#v", tt.Texts())
	}
}

func TestP30ReadyDiagramDoesNotClaimToStillRender(t *testing.T) {
	_, tt, calls := p30CardWithSeed(t, p30DiagramFence, func(state *appState) {
		diagram := p30LiveDiagram(t, state, p30DiagramSource, inlineDiagramReady, 600, 300)
		// A card that finished rendering holds the view that drew it.
		diagram.WebView = &mygo.WebView{}
	})
	if tt.HasText("Rendering diagram…") {
		t.Fatalf("a rendered diagram still says it is rendering: %#v", tt.Texts())
	}
	if calls.creates != 0 {
		t.Fatalf("a rendered diagram built %d views, want none", calls.creates)
	}
}

func TestP30ViewSourceToggleShowsCodeUnderDiagram(t *testing.T) {
	state, tt, _ := p30CardWithSeed(t, p30DiagramFence, func(state *appState) {
		p30LiveDiagram(t, state, p30DiagramSource, inlineDiagramLoading, 600, 300)
	})
	if tt.HasText("graph TD") {
		t.Fatal("the source is shown before the reader asks for it")
	}
	if err := tt.Click(text("diagramViewSource")); err != nil {
		t.Fatal(err)
	}
	p30Settle(tt)
	if !tt.HasText("graph TD") {
		t.Fatalf("the source switch did not show the code block: %#v", tt.Texts())
	}
	// The diagram is not replaced: the card keeps its header.
	if _, ok := tt.Find(text("diagramCardLabel")); !ok {
		t.Fatal("showing the source dropped the diagram")
	}
	if !state.diagramCardState(p30DiagramSource).showSource {
		t.Fatal("the switch state did not survive the frame")
	}
}

// TestP30DiagramFitWidthAndZoom measures the body row through the frame the
// layout really drew, and reads the scales the card published to the page, so
// the scale rule and the height clamp are checked as geometry and traffic —
// not as arithmetic on constants.
func TestP30DiagramFitWidthAndZoom(t *testing.T) {
	state, tt, calls := p30CardWithSeed(t, p30DiagramFence, func(state *appState) {
		p30LiveDiagram(t, state, p30DiagramSource, inlineDiagramLoading, 1440, 800)
	})
	diagram := state.inlineDiagramFor(p30DiagramSource)
	p30Settle(tt)

	card := state.diagramCardState(p30DiagramSource)
	// The card stops at diagramCardMaxWidth and sits under the turn's 46 DIP
	// content indent (P29.1 moved it from a margin to padding, so the card is
	// narrower than the column by exactly that indent). The body fills the card
	// minus its hairline border.
	wantWidth := diagramCardMaxWidth - p29BodyIndent
	if d := absFloat32(card.innerWidth - wantWidth); d > 2 {
		t.Fatalf("body drew %.1f DIP wide, want %.1f (max %.0f minus the indent)",
			card.innerWidth, wantWidth, diagramCardMaxWidth)
	}
	wantFit := card.innerWidth / diagram.NaturalW
	if diff := absFloat32(p30LastScale(t, calls) - wantFit); diff > 0.01 {
		t.Fatalf("fit-width scale = %.3f, want the fitted %.3f",
			p30LastScale(t, calls), wantFit)
	}
	wantHeight := diagram.NaturalH * wantFit
	if diff := absFloat32(card.cardHeight - wantHeight); diff > 2 {
		t.Fatalf("body height = %.1f, want %.1f (%.0f × %.3f)",
			card.cardHeight, wantHeight, diagram.NaturalH, wantFit)
	}

	// Zooming in leaves fit-width and grows the body by 1.25.
	header, ok := tt.Find(text("diagramCardLabel"))
	if !ok {
		t.Fatal("card header missing")
	}
	tt.Move(header.X+header.W/2, header.Y+header.H/2) // reveal the toolbar
	tt.Frame()
	if err := tt.Click(text("diagramZoomIn")); err != nil {
		t.Fatal(err)
	}
	p30Settle(tt)
	if diff := absFloat32(p30LastScale(t, calls) - wantFit*diagramZoomFactor); diff > 0.01 {
		t.Fatalf("after zoom in scale = %.3f, want %.3f", p30LastScale(t, calls),
			wantFit*diagramZoomFactor)
	}
	wantHeight = diagram.NaturalH * wantFit * diagramZoomFactor
	if diff := absFloat32(card.cardHeight - wantHeight); diff > 2 {
		t.Fatalf("body height = %.1f, want %.1f", card.cardHeight, wantHeight)
	}
	if card.fitWidth {
		t.Fatal("zooming must leave fit-width")
	}

	// Fit-width again returns to the fitted scale.
	if err := tt.Click(text("diagramFitWidth")); err != nil {
		t.Fatal(err)
	}
	p30Settle(tt)
	if !card.fitWidth {
		t.Fatal("the fit-width switch did not take")
	}
	if diff := absFloat32(p30LastScale(t, calls) - wantFit); diff > 0.01 {
		t.Fatalf("fit width again scale = %.3f, want %.3f", p30LastScale(t, calls), wantFit)
	}

	// Zooming out stops at the floor, and the body at its minimum height.
	for i := 0; i < 6; i++ {
		if err := tt.Click(text("diagramZoomOut")); err != nil {
			t.Fatal(err)
		}
		p30Settle(tt)
	}
	if got := absFloat32(p30LastScale(t, calls) - diagramScaleMin); got > 0.001 {
		t.Fatalf("scale = %.3f, want the %.2f floor", p30LastScale(t, calls),
			diagramScaleMin)
	}
	if card.cardHeight < diagramBodyMinHeight-1 {
		t.Fatalf("body height %.1f fell below the %.0f floor", card.cardHeight,
			diagramBodyMinHeight)
	}
	// The page was told every value the card drew with, in order.
	if len(calls.scales) < 9 {
		t.Fatalf("the card published %d scales, want one per change", len(calls.scales))
	}
	if diagram.scale != diagramScaleMin {
		t.Fatalf("seat A holds scale %.3f, want the value the card last sent", diagram.scale)
	}
}

func TestP30DiagramBodyHeightStaysBounded(t *testing.T) {
	tests := []struct {
		name   string
		height float32
		want   float32
	}{
		{name: "huge diagram", height: 5000, want: 560},
		{name: "tiny diagram", height: 20, want: 48},
		// 600 natural at the fitted width (720 card minus the 46 DIP indent
		// over a 1440 natural width) draws about 281 DIP.
		{name: "in range", height: 600, want: 281},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state, tt, _ := p30CardWithSeed(t, p30DiagramFence, func(state *appState) {
				// A natural width of 1440 in the ~720 DIP body fits at about
				// 0.5, so the row draws half the natural height, clamped.
				p30LiveDiagram(t, state, p30DiagramSource, inlineDiagramLoading, 1440, test.height)
			})
			p30Settle(tt)
			card := state.diagramCardState(p30DiagramSource)
			if diff := absFloat32(card.cardHeight - test.want); diff > 3 {
				t.Fatalf("body height = %.1f, want %.1f", card.cardHeight, test.want)
			}
		})
	}
}

func TestP30DiagramUnknownSizeHoldsPlaceholderHeight(t *testing.T) {
	state, tt, _ := p30CardWithSeed(t, p30DiagramFence, func(state *appState) {
		p30LiveDiagram(t, state, p30DiagramSource, inlineDiagramLoading, 0, 0)
	})
	p30Settle(tt)
	card := state.diagramCardState(p30DiagramSource)
	if diff := absFloat32(card.cardHeight - diagramBodyUnknownHeight); diff > 1 {
		t.Fatalf("placeholder height = %.1f, want %.1f", card.cardHeight,
			diagramBodyUnknownHeight)
	}
}

func TestP30DiagramCardStaysInsideContentColumn(t *testing.T) {
	_, tt, _ := p30DiagramCard(t, p30DiagramFence)
	header, ok := tt.Find(text("diagramCardLabel"))
	if !ok {
		t.Fatal("card missing")
	}
	if header.W > diagramCardMaxWidth {
		t.Fatalf("card header is %.1f DIP wide, want at most %.0f", header.W,
			diagramCardMaxWidth)
	}
	// The card fills the content column minus the turn's indent, so its header
	// is that width whatever the window (P29.1 made the indent padding).
	if want := diagramCardMaxWidth - p29BodyIndent; absFloat32(header.W-want) > 1 {
		t.Fatalf("card header is %.1f DIP wide in a 1180 DIP window, want %.0f",
			header.W, want)
	}
	if absFloat32(header.H-diagramHeaderHeight) > 0.5 {
		t.Fatalf("header height = %.1f, want %.0f", header.H, diagramHeaderHeight)
	}
}

func TestP30DiagramNearViewportRule(t *testing.T) {
	const viewport = 600
	tests := []struct {
		name       string
		cardY      float32
		cardHeight float32
		built      bool
		want       bool
	}{
		{name: "on screen", cardY: 100, cardHeight: 200, want: true},
		{name: "partly above", cardY: -80, cardHeight: 200, want: true},
		{name: "one viewport above", cardY: -620, cardHeight: 200, want: true},
		{name: "far above", cardY: -1400, cardHeight: 200, want: false},
		{name: "keep band above", cardY: -1300, cardHeight: 200, built: true, want: true},
		{name: "far above past keep band", cardY: -1900, cardHeight: 200, built: true, want: false},
		{name: "just below", cardY: 700, cardHeight: 200, want: true},
		{name: "far below", cardY: 1900, cardHeight: 200, want: false},
		{name: "keep band below", cardY: 1700, cardHeight: 200, built: true, want: true},
		{name: "far below past keep band", cardY: 2400, cardHeight: 200, built: true, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := diagramCardNearViewport(test.cardY, test.cardHeight, viewport, test.built)
			if got != test.want {
				t.Fatalf("near(y=%.0f, h=%.0f, built=%v) = %v, want %v",
					test.cardY, test.cardHeight, test.built, got, test.want)
			}
		})
	}
	// Without a measured viewport nothing is dropped.
	if !diagramCardNearViewport(0, 100, 0, true) {
		t.Fatal("an unknown viewport must not release views")
	}
}

// TestP30DiagramViewsFreedWhenCardLeavesViewport covers the card side of the
// reclaim: a card the transcript stops building is no longer ensured, so seat
// A frees its view on the next frame.
func TestP30DiagramViewsFreedWhenCardLeavesViewport(t *testing.T) {
	state, tt, calls := p30DiagramCard(t, p30DiagramFence)
	if calls.creates != 1 {
		t.Fatalf("views created = %d, want one", calls.creates)
	}
	diagram := state.inlineDiagramFor(p30DiagramSource)
	if diagram.Status != inlineDiagramLoading {
		t.Fatalf("fresh card status = %d, want loading", diagram.Status)
	}
	// A transcript that stops building the card — here, by dropping the block
	// — frees the view, and the card state with the same grace.
	state.chat[1].blocks[0].text = "No diagram here."
	p30Settle(tt)
	if calls.releases == 0 {
		t.Fatal("the abandoned card kept its view")
	}
	if diagram.Status != inlineDiagramIdle {
		t.Fatalf("released status = %d, want idle", diagram.Status)
	}
	if len(state.chatView.diagramCards) != 0 {
		t.Fatalf("stale card states survived: %#v", state.chatView.diagramCards)
	}
	// The size cache belongs to seat A and outlives the view, so the card comes
	// back at the same height instead of jumping.
	diagram.NaturalH = 320
	if kept := state.inlineDiagramFor(p30DiagramSource); kept != diagram || kept.NaturalH != 320 {
		t.Fatal("the natural size did not survive a released view")
	}
}

func TestP30OpenInPageOnlyWhenPluginUsable(t *testing.T) {
	for _, test := range []struct {
		name  string
		entry bool
		want  bool
	}{
		{name: "plugin missing", entry: false, want: false},
		{name: "plugin installed", entry: true, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := p30WireDiagrams(t)
			restore := stableTestAppearance(t)
			defer restore()
			state := p30DiagramState(t, p30DiagramFence)
			if test.entry {
				state.setPluginRegistry(pluginRegistry{Entries: []pluginRegistryEntry{
					p28MermaidEntry(p28MermaidManifest("0.2.0"), pluginRegistered),
				}})
			}
			tt := ui.NewTester(state.view, 1180, 1400)
			p30Settle(tt)
			got := tt.HasText(text("diagramOpenInPage"))
			if got != test.want {
				t.Fatalf("open-in-page shown = %v, want %v: %#v", got, test.want, tt.Texts())
			}
			if calls.creates != 1 {
				t.Fatalf("views created = %d, want one", calls.creates)
			}
		})
	}

	// An oversized diagram is out of the plugin's reach too, so the card does
	// not offer the page for it.
	calls := p30WireDiagrams(t)
	restore := stableTestAppearance(t)
	defer restore()
	longSource := strings.Repeat("  A[label] --> B[label]\n", 1500)
	state := p30DiagramState(t, "```mermaid\n"+longSource+"```")
	state.setPluginRegistry(pluginRegistry{Entries: []pluginRegistryEntry{
		p28MermaidEntry(p28MermaidManifest("0.2.0"), pluginRegistered),
	}})
	tt := ui.NewTester(state.view, 1180, 1400)
	p30Settle(tt)
	if tt.HasText(text("diagramOpenInPage")) {
		t.Fatal("an oversized diagram offers the plugin page, which rejects it too")
	}
	if !tt.HasText(text("diagramTooLong")) {
		t.Fatalf("an oversized diagram did not say so: %#v", tt.Texts())
	}
	if calls.creates != 0 {
		t.Fatalf("an oversized diagram built %d views, want none", calls.creates)
	}
}

func TestP30DiagramCopyWritesSourceToClipboard(t *testing.T) {
	_, tt, _ := p30DiagramCard(t, p30DiagramFence)
	header, ok := tt.Find(text("diagramCardLabel"))
	if !ok {
		t.Fatal("card missing")
	}
	tt.Move(header.X+header.W/2, header.Y+header.H/2)
	tt.Frame()
	if err := tt.Click(text("diagramCopySource")); err != nil {
		t.Fatal(err)
	}
	if got := tt.Clipboard(); got != p30DiagramSource {
		t.Fatalf("clipboard = %q, want the block source %q", got, p30DiagramSource)
	}
}

// TestP30DiagramToolbarStaysReachableWhileHidden keeps the accessibility rule:
// the buttons are in the tree and clickable even before the fade-in, so a
// keyboard reader reaches a card it cannot see yet.
func TestP30DiagramToolbarStaysReachableWhileHidden(t *testing.T) {
	state, tt, calls := p30CardWithSeed(t, p30DiagramFence, func(state *appState) {
		p30LiveDiagram(t, state, p30DiagramSource, inlineDiagramLoading, 1440, 800)
	})
	p30Settle(tt)
	// The pointer never enters the card, so the toolbar is at zero opacity.
	tt.Move(20, 20)
	p30Settle(tt)
	if err := tt.Click(text("diagramViewSource")); err != nil {
		t.Fatalf("a faded-out toolbar is unreachable: %v", err)
	}
	if !state.diagramCardState(p30DiagramSource).showSource {
		t.Fatal("the faded-out switch did not take the click")
	}
	if calls.creates != 0 {
		t.Fatalf("using the toolbar rebuilt the view %d times", calls.creates)
	}
}

func TestP30DiagramThemeIsOpaqueAndFollowsPalette(t *testing.T) {
	for _, preset := range []appearancePreset{appearancePresetBYOP, appearancePresetJAgent} {
		for _, dark := range []bool{false, true} {
			p := themePaletteForAppearance(preset, dark, false, false)
			theme := inlineDiagramThemeFor(p, p29TonesFor(p))
			for name, value := range map[string]string{
				"background": theme.Background, "text": theme.Text, "muted": theme.Muted,
				"surface": theme.Surface, "surfaceAlt": theme.SurfaceAlt,
				"border": theme.Border, "accent": theme.Accent, "danger": theme.Danger,
			} {
				if !strings.HasPrefix(value, "#") || len(value) != 7 {
					t.Fatalf("%s on %s dark=%v: %q is not an opaque #rrggbb",
						name, preset, dark, value)
				}
			}
			for i, color := range theme.Chart {
				if len(color) != 7 {
					t.Fatalf("chart %d = %q, want #rrggbb", i, color)
				}
			}
			// The page paints on an opaque surface even under the wallpaper.
			if want := pluginColorHex(compositeOver(p.surface, p.surface)); theme.Background != want {
				t.Fatalf("background = %q, want the opaque surface %q", theme.Background, want)
			}
			if theme.FontSize != p.chatFont || theme.FontFamily == "" {
				t.Fatalf("font = %q/%v, want the process body size with a family",
					theme.FontFamily, theme.FontSize)
			}
			if theme.HighContrast {
				t.Fatal("these palettes are not high contrast")
			}
			// The chart ramp is the semantic order the design names.
			want := [5]string{
				pluginColorHex(compositeOver(p.accent, p.surface)),
				pluginColorHex(compositeOver(p.success, p.surface)),
				pluginColorHex(compositeOver(p.warning, p.surface)),
				pluginColorHex(compositeOver(p.danger, p.surface)),
				pluginColorHex(compositeOver(p.text.Mix(p.surface, .4), p.surface)),
			}
			if theme.Chart != want {
				t.Fatalf("chart ramp = %v, want %v", theme.Chart, want)
			}
		}
	}
	high := themePaletteForAppearance(appearancePresetBYOP, false, true, false)
	theme := inlineDiagramThemeFor(high, p29TonesFor(high))
	if !theme.HighContrast {
		t.Fatal("high contrast must reach the page")
	}
	if want := pluginColorHex(compositeOver(high.text, high.surface)); theme.Border != want {
		t.Fatalf("high contrast border = %q, want the text colour %q", theme.Border, want)
	}
}

func TestP30DiagramScaleRules(t *testing.T) {
	if got := diagramFitScale(720, 1440); got != 0.5 {
		t.Fatalf("fit scale = %.2f, want 0.5", got)
	}
	if got := diagramFitScale(720, 400); got != 1 {
		t.Fatalf("a narrow diagram must not be enlarged, got %.2f", got)
	}
	if got := diagramFitScale(0, 400); got != 1 {
		t.Fatalf("an unmeasured width = %.2f, want 1", got)
	}
	if got := diagramZoomScale(0.5, true); got != 0.625 {
		t.Fatalf("zoom in = %.3f, want 0.625", got)
	}
	if got := diagramZoomScale(0.3, false); got != 0.25 {
		t.Fatalf("zoom out below the floor = %.3f, want 0.25", got)
	}
	if got := diagramZoomScale(3.5, true); got != 4 {
		t.Fatalf("zoom in above the ceiling = %.3f, want 4", got)
	}
	cases := []struct{ height, scale, unknown, want float32 }{
		{800, 0.5, 160, 400},
		{5000, 1, 160, 560},
		{20, 1, 160, 48},
		{0, 1, 160, 160},  // no size yet: the placeholder height
		{800, 0, 160, 48}, // a zero scale still keeps the minimum
	}
	for _, test := range cases {
		if got := diagramBodyHeight(test.height, test.scale, test.unknown); got != test.want {
			t.Fatalf("body height(%v, %v) = %v, want %v", test.height, test.scale, got,
				test.want)
		}
	}
	// The card's own scale stays inside the same bounds seat A enforces.
	if got := diagramClampScale(9); got != diagramScaleMax {
		t.Fatalf("clamp ceiling = %.2f, want %.2f", got, diagramScaleMax)
	}
	if got := diagramClampScale(0.01); got != diagramScaleMin {
		t.Fatalf("clamp floor = %.3f, want %.3f", got, diagramScaleMin)
	}
}

func TestP30DiagramFailureTextCarriesTheLine(t *testing.T) {
	diagram := &inlineDiagram{Err: "Parse error", ErrLine: 7}
	want := fmt.Sprintf("%s %s",
		textArgs("diagramFailed", map[string]any{"message": "Parse error"}),
		textArgs("diagramFailedLine", map[string]any{"line": 7}))
	if got := diagramFailureText(diagram); got != want {
		t.Fatalf("failure text = %q, want %q", got, want)
	}
	if got := diagramFailureText(&inlineDiagram{}); !strings.Contains(got,
		text("diagramFailedNoDetail")) {
		t.Fatalf("a failure without a reason = %q, want the stand-in text", got)
	}
}

func absFloat32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// TestP30DiagramThemeFollowsPaletteWithoutReparsing covers the last sentence of
// P30 2.5: switching the appearance hands the pages new CSS variables and
// re-renders nothing. A unit test has no real page, so what it checks is the
// card's own side of that promise — the theme the card reports as pushed, and
// the absence of any new view.
func TestP30DiagramThemeFollowsPaletteWithoutReparsing(t *testing.T) {
	state, tt, calls := p30CardWithSeed(t, p30DiagramFence, func(state *appState) {
		// Loading without a live view: seat A's theme push also repaints each
		// page's background through the WebView itself, and a sentinel view has
		// no window behind it, so only the card's side runs here.
		diagram := p30LiveDiagram(t, state, p30DiagramSource, inlineDiagramLoading, 600, 300)
		diagram.WebView = nil
	})
	light := state.chatView.diagramTheme
	if light.Background == "" {
		t.Fatal("the card never handed over a theme")
	}

	currentAppearanceDark = func() bool { return true }
	p30Settle(tt)
	if calls.creates != 0 {
		t.Fatalf("a theme change built %d views, want none", calls.creates)
	}
	if _, ok := tt.Find(text("diagramCardLabel")); !ok {
		t.Fatalf("the card vanished on a theme change: %#v", tt.Texts())
	}
	dark := themePaletteForAppearance(appearancePresetBYOP, true, false, false)
	want := pluginColorHex(compositeOver(dark.surface, dark.surface))
	if state.chatView.diagramTheme == light {
		t.Fatal("the card kept the old theme after the appearance changed")
	}
	if state.chatView.diagramTheme.Background != want {
		t.Fatalf("pushed background = %q, want the dark surface %q",
			state.chatView.diagramTheme.Background, want)
	}
}

// TestP30ComposerOcclusionHidesThePage covers fix-round item D: a native page
// paints above the window's content, so a body reaching into the composer's
// band must not show it.
func TestP30ComposerOcclusionHidesThePage(t *testing.T) {
	state := &appState{}
	state.chatView.composerTop = 600
	state.chatView.composerTopKnown = true
	for _, test := range []struct {
		name  string
		bodyY float32
		h     float32
		want  bool
	}{
		{"above the composer", 400, 150, false},
		{"touching the composer", 400, 200, false},
		{"crossing its top edge", 400, 260, true},
		{"wholly under it", 610, 120, true},
		{"no composer measured yet", 0, 120, false},
	} {
		st := &diagramCardState{bodyY: test.bodyY, cardHeight: test.h}
		room, clipped := st.composerRoom(state)
		if clipped != test.want {
			t.Errorf("%s: clipped(%v,%v) = %v, want %v", test.name, test.bodyY, test.h, clipped, test.want)
			continue
		}
		if test.want && room > test.h {
			t.Errorf("%s: room %v exceeds the body %v", test.name, room, test.h)
		}
	}
}
