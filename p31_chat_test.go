package main

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// P31 §11 tests 7–11 (seat B): the BoardUI user bubble, the code-block
// header and its copy feedback for every preset, the p29 contrast matrix
// (updated in p29_contrast_test.go), the composer alignment list (updated in
// composer_alignment_test.go), and the BoardUI keyword ink. Wording is always
// located through text("key"), never a hard-coded language: the three
// chatCode* keys arrive with seat A's locales.

// p31BubblePaddingX/Y mirror the §6 bubble inset; the geometry test adds
// them back to the body text's rect to recover the bubble's rect.
const (
	p31BubblePaddingX float32 = 12
	p31BubblePaddingY float32 = 11
)

// p31UserBubbleTestState renders two BoardUI user messages, one short and
// one long enough to hit the bubble's width cap.
func p31UserBubbleTestState(t *testing.T) (*appState, *ui.Tester, string) {
	t.Helper()
	state := layoutTestState(t)
	state.preferences.AppearancePreset = string(appearancePresetBoardUI)
	longText := strings.TrimSuffix(strings.Repeat("filler ", 160), " ")
	state.chat = []chatMessage{
		{role: chatUser, status: "Complete",
			blocks: []chatBlock{p29TextBlock("Short one", "")}},
		{role: chatUser, status: "Complete",
			blocks: []chatBlock{p29TextBlock(longText, "")}},
	}
	tt := ui.NewTester(state.view, 1180, 1400)
	return state, tt, longText
}

func TestP31BoardUIUserBubbleGeometry(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	state, tt, longText := p31UserBubbleTestState(t)

	you := text("p29UserLabel")
	col, ok := tt.Find(you)
	if !ok {
		t.Fatalf("the bubble column lost its accessible You label: %#v", tt.Texts())
	}
	// The label names the outer column: it spans the content column (§6),
	// not a 35×35 avatar or a header row — BoardUI draws neither, and at
	// 1180px the column is the full 720 DIP wide (the P30 header test uses
	// the same reference width).
	if delta := col.W - 720; delta < -1 || delta > 1 {
		t.Fatalf("the You label rect is %.1f DIP wide, want the whole 720 DIP column", col.W)
	}
	if col.H <= 40 {
		t.Fatalf("the You label rect is only %.1f DIP tall: §6 keeps the label on the outer column, never on a header row", col.H)
	}
	// Every user message labels exactly its own outer column: no avatar or
	// header row carries the name on this preset (a surviving header row
	// would add a match per message, and Find already returns the widest,
	// tallest node checked above).
	matches := 0
	for _, value := range tt.Texts() {
		if value == you {
			matches++
		}
	}
	if matches != 2 {
		t.Fatalf("the localized You labels %d nodes, want the two user columns and nothing else", matches)
	}

	// Bubble rect from the body text: the bubble insets it by exactly the
	// §6 padding.
	bubble := func(body ui.Rect) ui.Rect {
		return ui.Rect{
			X: body.X - p31BubblePaddingX, Y: body.Y - p31BubblePaddingY,
			W: body.W + 2*p31BubblePaddingX, H: body.H + 2*p31BubblePaddingY,
		}
	}
	colRight := col.X + col.W

	short, ok := tt.Find("Short one")
	if !ok {
		t.Fatalf("short user message missing: %#v", tt.Texts())
	}
	shortBubble := bubble(short)
	if delta := shortBubble.X + shortBubble.W - colRight; delta < -1 || delta > 1 {
		t.Fatalf("short bubble right edge %.1f, want the column right edge %.1f ±1",
			shortBubble.X+shortBubble.W, colRight)
	}
	if !(shortBubble.X > col.X) {
		t.Fatalf("short bubble left edge %.1f did not stay right of the column's %.1f",
			shortBubble.X, col.X)
	}
	// No minimum width: the short bubble shrinks to its text.
	if !(shortBubble.W < 240) {
		t.Fatalf("short bubble is %.1f DIP wide, want under 240: §6 sets no MinWidth",
			shortBubble.W)
	}

	long, ok := tt.Find(longText)
	if !ok {
		t.Fatalf("long user message missing: %#v", tt.Texts())
	}
	longBubble := bubble(long)
	// Same formula as §6, through the fixture's own appState so the nav and
	// tasks preferences are the ones that were rendered.
	colW := min(float32(720), state.chatMainWidth(1180)-40)
	bubbleMax := max(min(colW*0.8, 560), 240)
	if longBubble.W > bubbleMax+1 {
		t.Fatalf("long bubble is %.1f DIP wide, want at most %.1f", longBubble.W, bubbleMax)
	}
	// The text overflows the cap, so the bubble must sit exactly on it —
	// this is also what makes the right-edge check below meaningful.
	if delta := longBubble.W - bubbleMax; delta < -1 || delta > 1 {
		t.Fatalf("long bubble is %.1f DIP wide, want the %.1f cap ±1", longBubble.W, bubbleMax)
	}
	if delta := longBubble.X + longBubble.W - colRight; delta < -1 || delta > 1 {
		t.Fatalf("long bubble right edge %.1f, want the column right edge %.1f ±1",
			longBubble.X+longBubble.W, colRight)
	}
}

// p31CodeFixtureBlock is a closed fence; p31CodeFixtureUnclosed stays open,
// the streaming case §7 requires to be copyable too.
const (
	p31CodeFixtureBlock    = "Look:\n\n```json\n{\"copied\": true}\n```\n"
	p31CodeFixtureBody     = "{\"copied\": true}\n"
	p31CodeFixtureUnclosed = "Look:\n\n```json\n{\"partial\": true}\n"
	p31CodeFixtureOpenBody = "{\"partial\": true}\n"
)

// TestReviewBoardUIAttachmentChipsEndAligned is the review P2-2 check: the
// chip row fills the bubble column, so the row must end-align its own chips;
// an outer Justify wrapper does not reach through a FillWidth child.
func TestReviewBoardUIAttachmentChipsEndAligned(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	state := layoutTestState(t)
	state.preferences.AppearancePreset = string(appearancePresetBoardUI)
	state.chat = []chatMessage{{role: chatUser, status: "Complete",
		attachments: []chatAttachmentMeta{{Name: "notes.txt", Kind: attachmentFile}},
		blocks:      []chatBlock{p29TextBlock("Short one", "")}}}
	tt := ui.NewTester(state.view, 1180, 900)
	tt.Frame()
	col, _ := tt.Find(text("p29UserLabel"))
	chip, ok := tt.Find(text("chatAttachmentFileLabel") + ": notes.txt")
	if !ok {
		t.Fatal("no chip")
	}
	if d := (col.X + col.W) - (chip.X + chip.W); d < -1 || d > 1 {
		t.Fatalf("chip right %.1f, want column right %.1f", chip.X+chip.W, col.X+col.W)
	}
}

// TestP31BoardUIUserBubbleClipsUnbrokenText is the review P2-3 check: a
// ~400-character run without a single break point must not push the bubble
// past its width cap or past the column edge (the §6 Clip holds it).
func TestP31BoardUIUserBubbleClipsUnbrokenText(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	state := layoutTestState(t)
	state.preferences.AppearancePreset = string(appearancePresetBoardUI)
	unbroken := strings.Repeat("x", 400)
	state.chat = []chatMessage{{role: chatUser, status: "Complete",
		blocks: []chatBlock{p29TextBlock(unbroken, "")}}}
	tt := ui.NewTester(state.view, 1180, 900)
	col, ok := tt.Find(text("p29UserLabel"))
	if !ok {
		t.Fatalf("bubble column missing: %#v", tt.Texts())
	}
	body, ok := tt.Find(unbroken)
	if !ok {
		t.Fatalf("unbroken message missing: %#v", tt.Texts())
	}
	colW := min(float32(720), state.chatMainWidth(1180)-40)
	bubbleMax := max(min(colW*0.8, 560), 240)
	if bubbleW := body.W + 2*p31BubblePaddingX; bubbleW > bubbleMax+1 {
		t.Fatalf("the unbroken run made the bubble %.1f DIP wide, want at most %.1f",
			bubbleW, bubbleMax)
	}
	if right := body.X + body.W + p31BubblePaddingX; right > col.X+col.W+1 {
		t.Fatalf("the bubble right edge %.1f passed the column right edge %.1f",
			right, col.X+col.W)
	}
}

// TestP31ThinkingLevelInkFollowsPreset is the review P3 check for §2's
// current-level colour: BoardUI reads the accent text, the other presets
// keep the theme accent they always used.
func TestP31ThinkingLevelInkFollowsPreset(t *testing.T) {
	theme, boardui := nativeThemeForAppearance(appearancePresetBoardUI, true, false, false)
	if boardui.accentText == theme.Accent {
		t.Fatal("the palette must distinguish accent text from the accent, " +
			"or this test cannot tell the §2 override apart")
	}
	if got := thinkingLevelInk(theme, boardui); got != boardui.accentText {
		t.Fatalf("boardui current level = %#v, want the accent text %#v", got, boardui.accentText)
	}
	for _, preset := range []appearancePreset{appearancePresetBYOP, appearancePresetJAgent} {
		for _, dark := range []bool{false, true} {
			t2, p2 := nativeThemeForAppearance(preset, dark, false, false)
			if got := thinkingLevelInk(t2, p2); got != t2.Accent {
				t.Fatalf("%s (dark %v) current level = %#v, want the theme accent %#v",
					preset, dark, got, t2.Accent)
			}
		}
	}
}

func p31CodeBlockTester(t *testing.T, preset appearancePreset, textValue string) *ui.Tester {
	t.Helper()
	state := layoutTestState(t)
	state.preferences.AppearancePreset = string(preset)
	state.chat = []chatMessage{
		{role: chatUser, status: "Complete",
			blocks: []chatBlock{p29TextBlock("Show me.", "")}},
		{role: chatAssistant, status: "Complete",
			blocks: []chatBlock{p29TextBlock(textValue, p29PhaseFinalAnswer)}},
	}
	return ui.NewTester(state.view, 1180, 1400)
}

func p31Presets() []appearancePreset {
	return []appearancePreset{
		appearancePresetBYOP, appearancePresetJAgent, appearancePresetBoardUI,
	}
}

func TestP31CodeBlockHeaderOnEveryPreset(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	for _, preset := range p31Presets() {
		tt := p31CodeBlockTester(t, preset, p31CodeFixtureBlock)
		t.Run(string(preset), func(t *testing.T) {
			if _, ok := tt.Find("json"); !ok {
				t.Fatalf("the fence language is missing from the header: %#v", tt.Texts())
			}
			if _, ok := tt.Find(text("chatCodeCopy")); !ok {
				t.Fatalf("the copy button is missing: %#v", tt.Texts())
			}
			if err := tt.Click(text("chatCodeCopy")); err != nil {
				t.Fatalf("clicking the copy button: %v", err)
			}
			if got := tt.Clipboard(); got != p31CodeFixtureBody {
				t.Fatalf("clipboard = %q, want the raw block %q", got, p31CodeFixtureBody)
			}
			if !tt.HasText(text("chatCodeCopied")) {
				t.Fatalf("the checked state did not appear: %#v", tt.Texts())
			}
			if tt.HasText(text("chatCodeCopy")) {
				t.Fatal("the copy label must switch to the copied wording while checked")
			}
			// The feedback expires 1200 ms after the click (§7).
			tt.Advance(1300 * time.Millisecond)
			if !tt.HasText(text("chatCodeCopy")) {
				t.Fatalf("the copy button did not restore after the window: %#v", tt.Texts())
			}
			if tt.HasText(text("chatCodeCopied")) {
				t.Fatal("the checked state outlived its 1200 ms window")
			}
		})
	}
}

// TestP31CodeBlockCopySurvivesStreaming is review N1: a press on an open
// block's copy button spans frames while the block streams. The button key
// must not follow the code text, or the press and the release land on two
// different buttons. BYOP stands for the shared default renderer.
func TestP31CodeBlockCopySurvivesStreaming(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	state := layoutTestState(t)
	state.chat = []chatMessage{
		{role: chatUser, status: "Complete",
			blocks: []chatBlock{p29TextBlock("Show me.", "")}},
		{role: chatAssistant, status: "Complete",
			blocks: []chatBlock{p29TextBlock("Look:\n\n```json\n{\"first\": 1}\n", p29PhaseFinalAnswer)}},
	}
	tt := ui.NewTester(state.view, 1180, 1400)
	button, ok := tt.Find(text("chatCodeCopy"))
	if !ok {
		t.Fatalf("the open block has no copy button: %#v", tt.Texts())
	}
	x, y := button.X+button.W/2, button.Y+button.H/2
	tt.Press(x, y)
	// A token arrives while the button is held: the block gains a line and
	// the transcript rebuilds around the still-held pointer.
	state.chat[1].blocks[0].text += "{\"second\": 2}\n"
	tt.Frame()
	tt.Release(x, y)
	want := "{\"first\": 1}\n{\"second\": 2}\n"
	if got := tt.Clipboard(); got != want {
		t.Fatalf("clipboard = %q, want the latest block content %q", got, want)
	}
	if !tt.HasText(text("chatCodeCopied")) {
		t.Fatalf("the checked state did not appear after the streaming click: %#v", tt.Texts())
	}
	// Another streamed line must not drop the check before its window ends.
	state.chat[1].blocks[0].text += "{\"third\": 3}\n"
	tt.Frame()
	if !tt.HasText(text("chatCodeCopied")) {
		t.Fatalf("the next streamed token dropped the checked state: %#v", tt.Texts())
	}
	tt.Advance(1300 * time.Millisecond)
	if !tt.HasText(text("chatCodeCopy")) {
		t.Fatalf("the copy button did not restore after the window: %#v", tt.Texts())
	}
	if tt.HasText(text("chatCodeCopied")) {
		t.Fatal("the checked state outlived its 1200 ms window")
	}
}

func TestP31CodeBlockUnclosedStaysCopyable(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	for _, preset := range p31Presets() {
		tt := p31CodeBlockTester(t, preset, p31CodeFixtureUnclosed)
		t.Run(string(preset), func(t *testing.T) {
			if _, ok := tt.Find(text("chatCodeCopy")); !ok {
				t.Fatalf("a block whose fence has not closed must still offer the copy button: %#v", tt.Texts())
			}
			if err := tt.Click(text("chatCodeCopy")); err != nil {
				t.Fatalf("clicking the copy button: %v", err)
			}
			if got := tt.Clipboard(); got != p31CodeFixtureOpenBody {
				t.Fatalf("clipboard = %q, want the streamed content %q", got, p31CodeFixtureOpenBody)
			}
		})
	}
}

func TestP31ClosedMermaidKeepsTheDiagramCard(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	for _, preset := range p31Presets() {
		fixture := "Diagram:\n\n```mermaid\n" + p30DiagramSource + "```\n"
		tt := p31CodeBlockTester(t, preset, fixture)
		t.Run(string(preset), func(t *testing.T) {
			if _, ok := tt.Find(text("diagramCopySource")); !ok {
				t.Fatalf("the closed Mermaid block left the diagram card path: %#v", tt.Texts())
			}
			if tt.HasText(text("chatCodeCopy")) {
				t.Fatal("the diagram card must not gain the code-block copy header")
			}
		})
	}
}

func TestP31BoardUIKeywordInkIsAccentText(t *testing.T) {
	oldDark := currentAppearanceDark
	oldHigh := currentAppearanceHighContrast
	oldReduce := currentAppearanceReduceMotion
	currentAppearanceDark = func() bool { return true }
	currentAppearanceHighContrast = func() bool { return false }
	currentAppearanceReduceMotion = func() bool { return false }
	defer func() {
		currentAppearanceDark = oldDark
		currentAppearanceHighContrast = oldHigh
		currentAppearanceReduceMotion = oldReduce
	}()

	state := layoutTestState(t)
	state.preferences.AppearancePreset = string(appearancePresetBoardUI)
	_ = ui.NewTester(state.view, 1180, 760) // renders one frame, running configureMarkdownV12
	p := themePaletteForAppearance(appearancePresetBoardUI, true, false, false)
	if p.accentText == p.accentPressed {
		t.Fatal("the palette must distinguish accent text from the pressed accent, " +
			"or this test cannot tell the §2 override apart")
	}
	theme := state.chatView.markdown.Theme
	if theme == nil {
		t.Fatal("configureMarkdownV12 left no markdown theme")
	}
	for _, role := range []editor.TokenKind{editor.TokenKeyword, editor.TokenType} {
		if got := theme.CodeColors.Tokens[role].Color; got != p.accentText {
			t.Fatalf("token %d reads %#v, want the accent text %#v (§2)", role, got, p.accentText)
		}
	}
	if theme.Link != p.accentText {
		t.Fatalf("links read %#v, want the accent text %#v (§2)", theme.Link, p.accentText)
	}
}

func TestP31OtherPresetsKeepThePressedAccentInk(t *testing.T) {
	restore := stableTestAppearance(t)
	defer restore()
	for _, preset := range []appearancePreset{appearancePresetBYOP, appearancePresetJAgent} {
		state := layoutTestState(t)
		state.preferences.AppearancePreset = string(preset)
		_ = ui.NewTester(state.view, 1180, 760) // renders one frame, running configureMarkdownV12
		p := themePaletteForAppearance(preset, false, false, false)
		theme := state.chatView.markdown.Theme
		if theme == nil {
			t.Fatalf("%s left no markdown theme", preset)
		}
		if got := theme.CodeColors.Tokens[editor.TokenKeyword].Color; got != p.accentPressed {
			t.Fatalf("%s keyword ink = %#v, want the unchanged %#v", preset, got, p.accentPressed)
		}
	}
}
