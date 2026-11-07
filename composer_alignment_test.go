package main

import (
	"image"
	"testing"

	"github.com/egoist/mygo/ui"
)

const composerAlignmentProbe = "帮我看看"

// composerAlignmentRuns lists the composer appearances the P29.1 alignment
// design must hold for; P31 §11 adds the BoardUI preset with every assertion
// unchanged.
var composerAlignmentRuns = []struct {
	name   string
	preset appearancePreset
}{
	{name: "byop", preset: appearancePresetBYOP},
	{name: "j-agent", preset: appearancePresetJAgent},
	{name: "board-ui", preset: appearancePresetBoardUI},
}

func composerAlignmentPalette(t *testing.T, preset appearancePreset) palette {
	t.Helper()
	_, p := nativeThemeForAppearance(preset, false, false, false)
	return p
}

// composerInputRect locates the composer input through its accessibility
// label, whose rect equals the input element's rect.
func composerInputRect(t *testing.T, tt *ui.Tester) ui.Rect {
	t.Helper()
	input, ok := tt.Find(text("chatMessageLabel"))
	if !ok {
		t.Fatal("composer input did not render")
	}
	return input
}

// composerInkRegion is the 48px-wide first-line window the ink probe scans,
// anchored to the input element's rect (coordinator probe reference).
func composerInkRegion(input ui.Rect) image.Rectangle {
	return image.Rect(int(input.X+2), int(input.Y-24), int(input.X+50), int(input.Y+input.H))
}

// composerInkRows scans the region for the first and last row that carry ink
// differing from the row background.
func composerInkRows(img *image.RGBA, r image.Rectangle) (int, int) {
	top, bottom := -1, -1
	for y := r.Min.Y; y < r.Max.Y; y++ {
		bg := img.RGBAAt(r.Min.X, y)
		found := false
		for x := r.Min.X; x < r.Max.X; x++ {
			c := img.RGBAAt(x, y)
			dr := int(c.R) - int(bg.R)
			if dr < 0 {
				dr = -dr
			}
			dg := int(c.G) - int(bg.G)
			if dg < 0 {
				dg = -dg
			}
			db := int(c.B) - int(bg.B)
			if db < 0 {
				db = -db
			}
			if dr > 40 || dg > 40 || db > 40 {
				found = true
				break
			}
		}
		if found {
			if top < 0 {
				top = y
			}
			bottom = y
		}
	}
	return top, bottom
}

// composerInkRun returns the first and last column of the first continuous
// ink run inside the horizontal band [x0,x1) × [y0,y1), sampling each row's
// background at bgX (a column left of the text). A gap of 4 or more empty
// columns ends the run, which keeps the ghost row's Tab chip out of the
// measured glyph width.
func composerInkRun(img *image.RGBA, x0, x1, y0, y1, bgX int) (int, int) {
	inked := make([]bool, x1-x0)
	for i := range inked {
		x := x0 + i
		for y := y0; y < y1; y++ {
			bg := img.RGBAAt(bgX, y)
			c := img.RGBAAt(x, y)
			dr := int(c.R) - int(bg.R)
			if dr < 0 {
				dr = -dr
			}
			dg := int(c.G) - int(bg.G)
			if dg < 0 {
				dg = -dg
			}
			db := int(c.B) - int(bg.B)
			if db < 0 {
				db = -db
			}
			if dr > 40 || dg > 40 || db > 40 {
				inked[i] = true
				break
			}
		}
	}
	first, last := -1, -1
	for i, has := range inked {
		if has {
			first = i
			break
		}
	}
	if first < 0 {
		return -1, -1
	}
	gap := 0
	for i := first + 1; i < len(inked); i++ {
		if inked[i] {
			last = i
			gap = 0
		} else {
			gap++
			if gap >= 4 {
				break
			}
		}
	}
	if last < 0 {
		last = first
	}
	return x0 + first, x0 + last
}

// composerLineBand is the pixel band of the input's first shared line.
func composerLineBand(input ui.Rect, lh float32) (x0, x1, y0, y1 int) {
	return int(input.X), int(input.X + input.W - 2), int(input.Y), int(input.Y+lh) + 1
}

// composerCardTopEdge scans upward from just above the input text to find the
// topmost rendered row of the composer card (its 0.5px border row).
func composerCardTopEdge(img *image.RGBA, x, startY int) int {
	reference := img.RGBAAt(x, startY)
	for y := startY; y > startY-60; y-- {
		c := img.RGBAAt(x, y)
		dr := int(c.R) - int(reference.R)
		if dr < 0 {
			dr = -dr
		}
		dg := int(c.G) - int(reference.G)
		if dg < 0 {
			dg = -dg
		}
		db := int(c.B) - int(reference.B)
		if db < 0 {
			db = -db
		}
		if dr > 12 || dg > 12 || db > 12 {
			return y
		}
	}
	return startY
}

// TestComposerGhostInkAlignsTypedInk renders the same short text once as the
// ghost suggestion and once as typed draft, then checks the first and last
// inked rows share the input's first line (≤1px).
func TestComposerGhostInkAlignsTypedInk(t *testing.T) {
	for _, run := range composerAlignmentRuns {
		t.Run(run.name, func(t *testing.T) {
			restore := stableTestAppearance(t)
			defer restore()

			ghostState := layoutTestState(t)
			ghostState.preferences.AppearancePreset = string(run.preset)
			ghostState.preferences.FollowUpStyle = string(followUpStyleGhost)
			ghostState.followUpSuggestion = composerAlignmentProbe
			ghost := ui.NewTester(ghostState.view, 1180, 760)
			ghostInput := composerInputRect(t, ghost)
			gTop, gBottom := composerInkRows(ghost.Image(), composerInkRegion(ghostInput))
			if gTop < 0 {
				t.Fatal("ghost suggestion drew no ink in the first line")
			}

			typedState := layoutTestState(t)
			typedState.preferences.AppearancePreset = string(run.preset)
			typedState.composerDraft = composerAlignmentProbe
			typed := ui.NewTester(typedState.view, 1180, 760)
			typedInput := composerInputRect(t, typed)
			tTop, tBottom := composerInkRows(typed.Image(), composerInkRegion(typedInput))
			if tTop < 0 {
				t.Fatal("typed draft drew no ink in the first line")
			}

			if diff := gTop - tTop; diff < -1 || diff > 1 {
				t.Fatalf("ghost first ink row %d vs typed %d (delta %d)", gTop, tTop, diff)
			}
			if diff := gBottom - tBottom; diff < -1 || diff > 1 {
				t.Fatalf("ghost last ink row %d vs typed %d (delta %d)", gBottom, tBottom, diff)
			}
		})
	}
}

// TestComposerCardHeightFollowsEmptyAttachments checks that the input's top
// sits one composer inset (padding + border) below the card with no
// attachments, and that one attachment keeps its row.
func TestComposerCardHeightFollowsEmptyAttachments(t *testing.T) {
	for _, run := range composerAlignmentRuns {
		t.Run(run.name, func(t *testing.T) {
			restore := stableTestAppearance(t)
			defer restore()
			p := composerAlignmentPalette(t, run.preset)

			state := layoutTestState(t)
			state.preferences.AppearancePreset = string(run.preset)
			state.composerDraft = ""
			tt := ui.NewTester(state.view, 1180, 760)
			input := composerInputRect(t, tt)
			// The card's top edge is detected from the pixels just above
			// the input text (inside the card's top border row).
			img := tt.Image()
			cardTop := float32(composerCardTopEdge(img, int(input.X)+30, int(input.Y)-2))
			want := composerTextInset(p)
			// The scan returns a whole pixel row while the input top may sit on a
			// half pixel, so allow one pixel; the regressions this guards against
			// (an empty attachment row) move the input by about 12px.
			if diff := input.Y - cardTop; diff < want-1 || diff > want+1 {
				t.Fatalf("input top - card top = %v, want %v±1 (cardTop=%v)", diff, want, cardTop)
			}

			withAttachment := layoutTestState(t)
			withAttachment.preferences.AppearancePreset = string(run.preset)
			withAttachment.composerAttachments = []composerAttachment{{
				path: "/tmp/notes.txt", name: "notes.txt", kind: attachmentFile,
			}}
			attached := ui.NewTester(withAttachment.view, 1180, 760)
			if _, ok := attached.Find("notes.txt"); !ok {
				t.Fatalf("attachment chip missing: %#v", attached.Texts())
			}
		})
	}
}

// TestComposerStatusRowsAlignWithInput checks the HUD row and the running
// status text start at the input's left edge, and stay inside their rows.
func TestComposerStatusRowsAlignWithInput(t *testing.T) {
	for _, run := range composerAlignmentRuns {
		t.Run(run.name, func(t *testing.T) {
			restore := stableTestAppearance(t)
			defer restore()

			state := p18_2HUDState(t)
			state.preferences.AppearancePreset = string(run.preset)
			tt := ui.NewTester(state.view, 1180, 760)
			input := composerInputRect(t, tt)
			hudRow, ok := tt.Find("pi-hud-row")
			if !ok {
				t.Fatal("HUD row did not render")
			}
			ctx, ok := tt.Find(state.piHUDContextLabel())
			if !ok {
				t.Fatalf("HUD capacity %q did not render: %#v",
					state.piHUDContextLabel(), tt.Texts())
			}
			if diff := ctx.X - input.X; diff < -0.5 || diff > 0.5 {
				t.Fatalf("HUD text x=%v vs input x=%v (delta %v)", ctx.X, input.X, diff)
			}
			if ctx.Y < hudRow.Y || ctx.Y+ctx.H > hudRow.Y+hudRow.H {
				t.Fatalf("HUD text escaped its row: text=%#v row=%#v", ctx, hudRow)
			}

			state.run = runStreaming
			running := ui.NewTester(state.view, 1180, 760)
			input2 := composerInputRect(t, running)
			working, ok := running.Find(text("chatPiWorking"))
			if !ok {
				t.Fatalf("working status did not render: %#v", running.Texts())
			}
			if diff := working.X - input2.X; diff < -0.5 || diff > 0.5 {
				t.Fatalf("working text x=%v vs input x=%v (delta %v)", working.X, input2.X, diff)
			}
			footer, ok := running.Find("composer-footer")
			if !ok {
				t.Fatal("composer footer did not render")
			}
			if working.Y < footer.Y || working.Y+working.H > footer.Y+footer.H {
				t.Fatalf("working text escaped its footer row: text=%#v row=%#v", working, footer)
			}
		})
	}
}

// TestComposerContextChipYieldsToHUD checks the capacity chip only renders
// when the HUD row does not already show a capacity item. Both assertions
// count the exact capacity text in tt.Texts() (the chip renders the capacity
// as a standalone string).
func TestComposerContextChipYieldsToHUD(t *testing.T) {
	for _, run := range composerAlignmentRuns {
		t.Run(run.name, func(t *testing.T) {
			restore := stableTestAppearance(t)
			defer restore()

			// (a) The HUD row itself carries the capacity item: the chip's
			// fallback capacity ("1M") must not appear as its own text.
			state := p18_2HUDState(t)
			state.preferences.AppearancePreset = string(run.preset)
			if !state.piHUDShowsContextCapacity() {
				t.Fatal("test state must show capacity in the HUD row")
			}
			tt := ui.NewTester(state.view, 1180, 760)
			countHUDCapacity := 0
			for _, value := range tt.Texts() {
				if value == formatContextWindow(state.hud.contextWindowFallback) {
					countHUDCapacity++
				}
			}
			if countHUDCapacity != 0 {
				t.Fatalf("standalone capacity chip rendered while the HUD row shows capacity (count %d)", countHUDCapacity)
			}
			if !tt.HasText(state.piHUDContextLabel()) {
				t.Fatal("HUD capacity did not render")
			}

			// (b) The HUD row only shows the branch (no capacity item), so
			// the chip must show the model's 200K capacity on its own.
			quiet := layoutTestState(t)
			quiet.preferences.AppearancePreset = string(run.preset)
			if quiet.piHUDShowsContextCapacity() {
				t.Fatal("layout test state unexpectedly shows capacity in the HUD row")
			}
			quietTT := ui.NewTester(quiet.view, 1180, 760)
			countQuietCapacity := 0
			for _, value := range quietTT.Texts() {
				if value == formatContextWindow(200_000) {
					countQuietCapacity++
				}
			}
			if countQuietCapacity != 1 {
				t.Fatalf("capacity chip missing without HUD capacity (count %d)", countQuietCapacity)
			}
		})
	}
}

// TestComposerGhostTextTracksInputFont checks the ghost text box height
// equals the shared line height, and that the ghost glyphs use the input's
// font size (same ink width as the typed draft, within 1px).
func TestComposerGhostTextTracksInputFont(t *testing.T) {
	for _, run := range composerAlignmentRuns {
		t.Run(run.name, func(t *testing.T) {
			restore := stableTestAppearance(t)
			defer restore()
			_, p := nativeThemeForAppearance(run.preset, false, false, false)
			font := fontBody
			if run.preset == appearancePresetJAgent {
				font = p.bodyFont
			}
			lh := composerLineHeight(font)

			ghostState := layoutTestState(t)
			ghostState.preferences.AppearancePreset = string(run.preset)
			ghostState.preferences.FollowUpStyle = string(followUpStyleGhost)
			ghostState.followUpSuggestion = composerAlignmentProbe
			ghost := ui.NewTester(ghostState.view, 1180, 760)
			ghostText, ok := ghost.Find(composerAlignmentProbe)
			if !ok {
				t.Fatal("ghost suggestion text did not render")
			}
			if diff := ghostText.H - lh; diff < -0.5 || diff > 0.5 {
				t.Fatalf("ghost text box height %v, want %v±0.5", ghostText.H, lh)
			}
			ghostInput := composerInputRect(t, ghost)
			gx0, gx1, gy0, gy1 := composerLineBand(ghostInput, lh)
			gFirst, gLast := composerInkRun(ghost.Image(), gx0, gx1, gy0, gy1, gx0-6)
			if gFirst < 0 {
				t.Fatal("ghost suggestion drew no ink")
			}

			typedState := layoutTestState(t)
			typedState.preferences.AppearancePreset = string(run.preset)
			typedState.composerDraft = composerAlignmentProbe
			typed := ui.NewTester(typedState.view, 1180, 760)
			typedInput := composerInputRect(t, typed)
			tx0, tx1, ty0, ty1 := composerLineBand(typedInput, lh)
			tFirst, tLast := composerInkRun(typed.Image(), tx0, tx1, ty0, ty1, tx0-6)
			if tFirst < 0 {
				t.Fatal("typed draft drew no ink")
			}
			gWidth := float32(gLast - gFirst + 1)
			tWidth := float32(tLast - tFirst + 1)
			if diff := gWidth - tWidth; diff < -1 || diff > 1 {
				t.Fatalf("ghost ink width %v vs typed ink width %v (delta %v)", gWidth, tWidth, diff)
			}
		})
	}
}
