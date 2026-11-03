package main

import (
	"github.com/egoist/mygo/ui"
)

// Composer suggestion menus. Both the slash list and the @ mention list are the
// same shape — a short list above the composer whose rows the user picks
// explicitly — so they share the renderer and the key handling here. Nothing in
// this file runs anything by itself: a row either calls what the caller wired in
// choose, and a typed prefix never executes a contribution.

// composerMenuItem is one visible row of a composer menu. Title is the text the
// row stands for, Detail the description, and Tag names where the row came from,
// because a plugin row and a Pi row do different things when chosen.
type composerMenuItem struct {
	Key    string
	Title  string
	Detail string
	Tag    string
}

// composerMenuKind says which menu the composer is showing, if any. Only one
// menu is open at a time, because both are keyed off the same draft.
type composerMenuKind int

const (
	composerMenuNone composerMenuKind = iota
	composerMenuSlash
	composerMenuMention
)

// composerMenu reports the active menu with its rows and selection, so the
// render, the key handlers, and the send path all agree on one thing.
func (s *appState) composerMenu() (kind composerMenuKind, rows []composerMenuItem, selection int, choose func()) {
	if rows, selection, ok := s.mentionMenuRows(); ok {
		return composerMenuMention, rows, selection, s.chooseMentionRow
	}
	if rows, selection, ok := s.slashMenuRows(); ok {
		return composerMenuSlash, rows, selection, s.chooseSlashRow
	}
	return composerMenuNone, nil, 0, nil
}

// composerMenuVisible reports whether a menu is on screen, which the send path
// uses so Enter picks a row instead of sending the partial token.
func (s *appState) composerMenuVisible() bool {
	kind, _, _, _ := s.composerMenu()
	return kind != composerMenuNone
}

// composerMenus draws whichever menu is open, above the composer card, so the
// input keeps focus and the draft stays visible while the user chooses.
func (s *appState) composerMenus(c *ui.Context, p palette) {
	kind, rows, selection, choose := s.composerMenu()
	if kind == composerMenuNone {
		return
	}
	label := text("slashMenuLabel")
	if kind == composerMenuMention {
		label = text("mentionMenuLabel")
	}
	columnKey := "composer-slash-menu"
	if kind == composerMenuMention {
		columnKey = "composer-mention-menu"
	}
	ui.Column(c).Key(columnKey).Label(label).
		FillWidth().MinWidth(0).Gap(2).Children(func() {
		for index, row := range rows {
			button := ui.ButtonBase(c).Key(row.Key).Label(row.Title).
				FillWidth().MinWidth(0).Height(28).Padding(0, 8).
				Radius(v12InputRadius).Justify(ui.Start).AlignItems(ui.Center).Gap(8).
				Background(ui.Color{}).TextColor(p.text)
			if index == selection {
				button = button.Selected(true).Background(p.surfaceHover)
			} else if button.Hovered() {
				button = button.Background(p.surfaceHover)
			}
			button.Children(func() {
				ui.Text(c, row.Title).FontSize(fontBody).TextColor(p.text).Shrink(0)
				ui.Text(c, row.Detail).SingleLine().Ellipsis("…").
					FontSize(fontCaption).TextColor(p.muted).MinWidth(0).Shrink(1)
				if row.Tag != "" {
					ui.Text(c, row.Tag).FontSize(fontCaption).TextColor(p.muted).Shrink(0)
				}
			})
			if button.Clicked() {
				// A click chooses the row under the pointer, so the selection
				// moves there first and one code path handles both pointer and
				// keyboard choices.
				s.setComposerMenuSelection(kind, rows, index)
				if choose != nil {
					choose()
				}
			}
		}
	})
}

// handleComposerMenuTextKey takes the keys an open menu needs from the text
// input: Up and Down move the selection, Tab moves it forward like Down does,
// and Escape closes the menu for what the user has typed so far. Enter is not
// handled here because the composer claims it first, so the send path asks
// chooseComposerMenuRow instead.
func (s *appState) handleComposerMenuTextKey(k ui.Key, mods ui.Modifiers) bool {
	if mods != 0 {
		return false
	}
	kind, rows, selection, _ := s.composerMenu()
	if kind == composerMenuNone {
		return false
	}
	switch k {
	case ui.KeyDown, ui.KeyTab:
		s.setComposerMenuSelection(kind, rows, selection+1)
		return true
	case ui.KeyUp:
		s.setComposerMenuSelection(kind, rows, selection-1)
		return true
	case ui.KeyEscape:
		s.dismissComposerMenu(kind)
		return true
	default:
		return false
	}
}

func (s *appState) setComposerMenuSelection(kind composerMenuKind, rows []composerMenuItem, next int) {
	if len(rows) == 0 {
		return
	}
	wrapped := next % len(rows)
	if wrapped < 0 {
		wrapped += len(rows)
	}
	switch kind {
	case composerMenuSlash:
		s.pluginSlashSelection = wrapped
	case composerMenuMention:
		s.mentionSelection = wrapped
	}
}

// handleComposerMenuInput takes plain Enter while a menu is open, which is how a
// row is chosen with the keyboard. Only plain Enter is taken, so Cmd or Ctrl
// Enter keeps the running-send meaning and Shift Enter still adds a newline.
func (s *appState) handleComposerMenuInput(ev ui.InputEvent, composing bool) bool {
	if composing || ev.Kind != ui.InputKeyDown || ev.Mods != 0 || ev.Key != ui.KeyEnter {
		return false
	}
	return s.chooseComposerMenuRow()
}

// chooseComposerMenuRow picks the selected row of the open menu, and reports
// whether one was picked. The send path calls this before sending, so Enter
// chooses while a menu is open instead of submitting a half-typed token.
func (s *appState) chooseComposerMenuRow() bool {
	kind, rows, selection, _ := s.composerMenu()
	if kind == composerMenuNone {
		return false
	}
	if selection >= len(rows) {
		selection = 0
	}
	// The selection is passed to the chooser rather than reset first, because
	// choosing is what reads it. Each chooser clears the menu state when it acts.
	switch kind {
	case composerMenuSlash:
		s.pluginSlashSelection = selection
		s.chooseSlashRow()
	case composerMenuMention:
		s.mentionSelection = selection
		s.chooseMentionRow()
	}
	return true
}

// dismissComposerMenu closes one menu for the text typed so far. Typing more
// raises it again, which is what an Escape dismissal means here: the slash menu
// resets when the draft changes, and the mention menu remembers the token the
// dismissal was raised on so a later token is not silenced for the session.
func (s *appState) dismissComposerMenu(kind composerMenuKind) {
	switch kind {
	case composerMenuSlash:
		s.pluginSlashSelection = 0
		s.pluginSlashDismissed = true
	case composerMenuMention:
		s.dismissMentionMenu()
	}
}

// composerMenuHooks is what a composer needs to let a suggestion menu take plain
// Enter. visible says whether a menu is on screen and choose picks the selected
// row, so the composer can hold its own send meaning back without knowing what a
// menu is.
type composerMenuHooks struct {
	visible func() bool
	choose  func()
}

// takeComposerMenuEnter reports whether a menu took the Enter key, in which case
// the composer must not treat it as a send.
func (m *composerMenuHooks) takeEnter() bool {
	if m == nil || m.visible == nil || !m.visible() {
		return false
	}
	if m.choose != nil {
		m.choose()
	}
	return true
}

// composerComposerInputHandler is the input handler a composer registers while a
// follow-up suggestion shows or a turn is running. MyGo keeps one handler per
// element, so this replaces the composer's own Enter handler for that frame and
// must keep its meaning: a menu takes plain Enter first, then the follow-up or
// running-send behaviour applies, and anything else is left to the editor.
func (s *appState) composerInputHandler(composing, followUpVisible, running bool) func(ui.InputEvent) bool {
	return func(ev ui.InputEvent) bool {
		if s.handleComposerMenuInput(ev, composing) {
			return true
		}
		switch {
		case followUpVisible:
			return s.handleFollowUpComposerInput(ev, composing)
		case running:
			return handleRunningComposerInput(
				ev,
				composing,
				s.composerDraft,
				s.currentRunningSendMode(),
				s.sendRunningPrompt,
			)
		}
		return false
	}
}

// refreshComposerMenus schedules any query a menu needs. It only starts tasks, so
// calling it every frame does no file, process, or network work in the frame.
func (s *appState) refreshComposerMenus() {
	s.maybeRefreshPiCommands()
	s.maybeRefreshMentions()
}

// composerMenus draws whichever menu is open above the composer card, after
// scheduling whatever the menus need. It is the one call site a composer makes,
// so both the BYOP and JAgent composer get the same behaviour.
func (s *appState) composerMenusWithRefresh(c *ui.Context, p palette) {
	s.refreshComposerMenus()
	s.composerMenus(c, p)
}

// applyPendingComposerMenus places the caret after text a menu inserted.
func (s *appState) applyPendingComposerMenus(input ui.Element) {
	s.applyPendingComposerSlashCaret(input)
	s.applyPendingComposerMentionCaret(input)
}

// composerMenuHooksFor returns the hooks a composer passes so a suggestion menu
// can take plain Enter. It is nil for a draft that cannot open a menu, so the
// composer's Enter keeps its ordinary meaning without any extra check.
func (s *appState) composerMenuHooksFor() *composerMenuHooks {
	if _, ok := pluginSlashToken(s.composerDraft); !ok {
		if _, _, ok := pluginMentionToken(s.composerDraft); !ok {
			return nil
		}
	}
	return &composerMenuHooks{
		visible: s.composerMenuVisible,
		choose:  func() { s.chooseComposerMenuRow() },
	}
}
