package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

// Pi extension dialogs (select, confirm, input, editor) are answered by the
// app instead of being cancelled on arrival. The request keeps its own ID, and
// the answer follows the installed SDK contract exactly: `value` for
// select/input/editor, `confirmed` for confirm, and `cancelled: true` for any
// of them. Nothing is approved automatically and no permission mode is added.
const (
	extensionDialogSelect  = "select"
	extensionDialogConfirm = "confirm"
	extensionDialogInput   = "input"
	extensionDialogEditor  = "editor"
)

// extensionUIDialog is one pending dialog request.
type extensionUIDialog struct {
	requestID   string
	method      string
	title       string
	message     string
	placeholder string
	prefill     string
	options     []string
	expiresAt   time.Time
	// client and epoch name the Pi connection the request came from, so a
	// response can never reach a process that replaced it.
	client *piClient
	epoch  uint64
}

func extensionUIDialogFromEvent(event piEvent) extensionUIDialog {
	dialog := extensionUIDialog{
		requestID:   strings.TrimSpace(event.RequestID),
		method:      event.ExtensionMethod,
		title:       strings.TrimSpace(event.Text),
		message:     event.ExtensionMessage,
		placeholder: event.ExtensionPlaceholder,
		prefill:     event.ExtensionPrefill,
		options:     append([]string(nil), event.ExtensionOptions...),
	}
	if event.ExtensionTimeoutMS > 0 {
		dialog.expiresAt = time.Now().Add(time.Duration(event.ExtensionTimeoutMS) * time.Millisecond)
	}
	return dialog
}

func extensionUIValueResponse(id, value string) map[string]any {
	return map[string]any{"id": id, "value": value}
}

func extensionUIConfirmedResponse(id string, confirmed bool) map[string]any {
	return map[string]any{"id": id, "confirmed": confirmed}
}

// rootView wraps the shell so an extension dialog stays reachable on every page
// in both appearances, without the shell owning that state.
func (s *appState) rootView(c *ui.Context) {
	s.view(c)
	dark := s.effectiveDark()
	high := s.highContrastActive(currentAppearanceHighContrast())
	reduceTransparency := currentAppearanceReduceTransparency()
	_, p := nativeThemeForAppearance(s.currentAppearancePreset(), dark, high, reduceTransparency)
	s.workspaceChangesPanel(c, p)
	s.extensionUIDialogs(c)
	s.commandPaletteDialog(c)
}

// queueExtensionDialog takes one dialog request. Simultaneous requests are kept
// in arrival order rather than overwriting each other, so none is left hanging.
func (s *appState) queueExtensionDialog(event piEvent, epoch uint64) {
	dialog := extensionUIDialogFromEvent(event)
	if dialog.requestID == "" {
		return
	}
	dialog.client = event.Source
	dialog.epoch = epoch
	for _, pending := range s.extensionDialogs {
		if pending.requestID == dialog.requestID {
			return
		}
	}
	// The draft belongs to the dialog on screen, so a request that queues
	// behind the front one must not touch it. Only a new front request sets it.
	becomesFront := len(s.extensionDialogs) == 0
	s.extensionDialogs = append(s.extensionDialogs, dialog)
	if !becomesFront {
		return
	}
	if dialog.method == extensionDialogEditor {
		s.extensionDialogDraft = dialog.prefill
	} else {
		s.extensionDialogDraft = ""
	}
}

// currentExtensionDialog is the dialog the panel shows, nil when none. A copy is
// returned because answering one request shifts the queue, which would otherwise
// move the struct the panel is reading.
func (s *appState) currentExtensionDialog() *extensionUIDialog {
	if len(s.extensionDialogs) == 0 {
		return nil
	}
	dialog := s.extensionDialogs[0]
	return &dialog
}

// pruneExtensionDialogs retires requests whose timeout Pi has already resolved
// on its own, so the app never shows a dialog that would answer nothing.
func (s *appState) pruneExtensionDialogs(now time.Time) {
	if len(s.extensionDialogs) == 0 {
		return
	}
	frontBefore := s.extensionDialogs[0].requestID
	kept := s.extensionDialogs[:0]
	for _, dialog := range s.extensionDialogs {
		if !dialog.expiresAt.IsZero() && !now.Before(dialog.expiresAt) {
			continue
		}
		kept = append(kept, dialog)
	}
	s.extensionDialogs = kept
	if len(s.extensionDialogs) == 0 {
		s.extensionDialogs = nil
		s.extensionDialogDraft = ""
		return
	}
	// The draft follows the dialog on screen, so it is reset only when the
	// front request itself changed, not when a request behind it expired.
	if s.extensionDialogs[0].requestID != frontBefore {
		s.resetExtensionDialogDraft()
	}
}

func (s *appState) resetExtensionDialogDraft() {
	if dialog := s.currentExtensionDialog(); dialog != nil && dialog.method == extensionDialogEditor {
		s.extensionDialogDraft = dialog.prefill
		return
	}
	s.extensionDialogDraft = ""
}

// answerExtensionDialog replies to one pending request with the original ID and
// forgets it, so a late second answer cannot reach Pi twice. The value goes out
// exactly as entered, whitespace and newlines included.
func (s *appState) answerExtensionDialog(requestID string, response map[string]any) bool {
	index := -1
	for i, dialog := range s.extensionDialogs {
		if dialog.requestID == requestID {
			index = i
			break
		}
	}
	if index < 0 {
		return false
	}
	dialog := s.extensionDialogs[index]
	s.extensionDialogs = append(s.extensionDialogs[:index], s.extensionDialogs[index+1:]...)
	if len(s.extensionDialogs) == 0 {
		s.extensionDialogs = nil
	}
	s.resetExtensionDialogDraft()
	if dialog.client == nil {
		return true
	}
	if s.pi != dialog.client || dialog.epoch != s.piEpoch {
		// The request's own process is gone; its answer is stale.
		return true
	}
	if err := dialog.client.SendExtensionUIResponse(response); err != nil {
		s.showNotice(textArgs("noticePiProtocolWarning", map[string]any{
			"error": uiErrorText(err, text("errorUnknown")),
		}))
	}
	return true
}

// declineExtensionDialog answers a confirm request with confirmed: false, which
// the SDK keeps distinct from a cancelled dialog.
func (s *appState) declineExtensionDialog(requestID string) bool {
	return s.answerExtensionDialog(requestID, extensionUIConfirmedResponse(requestID, false))
}

// answerExtensionDialogValue sends the draft exactly as the user left it.
func (s *appState) answerExtensionDialogValue(dialog *extensionUIDialog) {
	s.answerExtensionDialog(dialog.requestID,
		extensionUIValueResponse(dialog.requestID, s.extensionDialogDraft))
}

// cancelExtensionDialogsFor answers pending requests that belong to a Pi
// connection before that connection is replaced or closed.
func (s *appState) cancelExtensionDialogsFor(client *piClient) {
	if client == nil {
		return
	}
	frontBefore := ""
	if len(s.extensionDialogs) > 0 {
		frontBefore = s.extensionDialogs[0].requestID
	}
	var pending []extensionUIDialog
	kept := make([]extensionUIDialog, 0, len(s.extensionDialogs))
	for _, dialog := range s.extensionDialogs {
		if dialog.client == client {
			pending = append(pending, dialog)
			continue
		}
		kept = append(kept, dialog)
	}
	if len(pending) == 0 {
		return
	}
	s.extensionDialogs = kept
	if len(s.extensionDialogs) == 0 {
		s.extensionDialogs = nil
		s.extensionDialogDraft = ""
	} else if s.extensionDialogs[0].requestID != frontBefore {
		s.resetExtensionDialogDraft()
	}
	for _, dialog := range pending {
		_ = client.SendExtensionUIResponse(extensionUICancelledResponse(dialog.requestID))
	}
}

// cancelExtensionDialogs answers every pending request with cancelled: true,
// which is what a user cancel, a session or process replacement, and closing
// the app all mean for the extension.
func (s *appState) cancelExtensionDialogs() {
	client := s.pi
	pending := s.extensionDialogs
	s.extensionDialogs = nil
	s.extensionDialogDraft = ""
	if client == nil {
		return
	}
	for _, dialog := range pending {
		_ = client.SendExtensionUIResponse(extensionUICancelledResponse(dialog.requestID))
	}
}

// discardExtensionDialogs forgets pending requests without answering, for the
// case where the Pi process is already gone.
func (s *appState) discardExtensionDialogs() {
	s.extensionDialogs = nil
	s.extensionDialogDraft = ""
}

// extensionUIDialogs renders the head of the queue over the window. Backdrop
// clicks and Escape cancel it, as the SDK's cancelled response means.
func (s *appState) extensionUIDialogs(c *ui.Context) {
	s.pruneExtensionDialogs(c.Now())
	dialog := s.currentExtensionDialog()
	if dialog == nil {
		return
	}
	if !dialog.expiresAt.IsZero() {
		// Ask for one more frame when Pi resolves the request on its own, so the
		// stale dialog leaves the screen without polling.
		c.After(time.Until(dialog.expiresAt))
	}
	open := true
	_, p := nativeThemeForAppearance(s.currentAppearancePreset(), s.effectiveDark(),
		s.highContrastActive(currentAppearanceHighContrast()),
		currentAppearanceReduceTransparency())
	panel := ui.DialogBase(c, &open, func(backdrop ui.Element, body ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, .4))
		body.MaxWidth(520).Padding(16).Gap(12).Radius(runtimeTokens.Dimension.Radius["overlay"].Value).
			Background(p.surface).Border(v12BorderWidth, p.borderStrong).
			Shadow(0, 10, 30, 0, p.shadowOverlay).Children(func() {
			s.extensionDialogBody(c, p, dialog)
		})
	})
	if panel.Dismissed() {
		s.answerExtensionDialog(dialog.requestID,
			extensionUICancelledResponse(dialog.requestID))
	}
}

func (s *appState) extensionDialogBody(c *ui.Context, p palette, dialog *extensionUIDialog) {
	title := dialog.title
	if strings.TrimSpace(title) == "" {
		title = text("extensionUIPromptMissing")
	}
	components.Text(c, title).FontSize(fontTitle).TextColor(p.text).Selectable()
	if message := strings.TrimSpace(dialog.message); message != "" {
		components.Text(c, message).FontSize(fontBody).TextColor(p.muted).Selectable()
	}
	switch dialog.method {
	case extensionDialogSelect:
		for index, option := range dialog.options {
			chosen := option
			if s.extensionDialogButton(c, p,
				fmt.Sprintf("extension-dialog-option-%d", index), chosen, true).Clicked() {
				s.answerExtensionDialog(dialog.requestID, extensionUIValueResponse(dialog.requestID, chosen))
				return
			}
		}
	case extensionDialogInput:
		ui.TextInputBase(c.Key("extension-dialog-input"), &s.extensionDialogDraft).
			FillWidth().Height(32).Padding(0, 8).Radius(v12InputRadius).
			Border(v12BorderWidth, p.border).Background(p.surfaceSubtle).
			FontSize(fontBody).TextColor(p.text).
			Placeholder(dialog.placeholder)
	case extensionDialogEditor:
		ui.TextAreaBase(c.Key("extension-dialog-editor"), &s.extensionDialogDraft).
			FillWidth().MinHeight(120).Lines(4, 16).Padding(8).Radius(v12InputRadius).
			Border(v12BorderWidth, p.border).Background(p.surfaceSubtle).
			FontSize(fontBody).TextColor(p.text).
			Placeholder(dialog.placeholder)
	}
	row := ui.Row(c).FillWidth().MinWidth(0).Gap(8).Justify(ui.End).Children(func() {
		if dialog.method == extensionDialogInput || dialog.method == extensionDialogEditor {
			// The key stays fixed while the user types, so the field keeps focus.
			if s.extensionDialogButton(c, p, "extension-dialog-submit",
				text("extensionUIDialogConfirm"), true).Clicked() {
				s.answerExtensionDialogValue(dialog)
				return
			}
		}
		if dialog.method == extensionDialogConfirm {
			// A decline answers confirmed: false, which the SDK keeps distinct
			// from cancelling the dialog.
			if s.extensionDialogButton(c, p, "extension-dialog-confirm",
				text("extensionUIDialogConfirm"), true).Clicked() {
				s.answerExtensionDialog(dialog.requestID,
					extensionUIConfirmedResponse(dialog.requestID, true))
				return
			}
			if s.extensionDialogButton(c, p, "extension-dialog-decline",
				text("extensionUIDialogDecline"), false).Clicked() {
				s.declineExtensionDialog(dialog.requestID)
			}
			return
		}
		if s.extensionDialogButton(c, p, "extension-dialog-cancel",
			text("extensionUIDialogCancel"), false).Clicked() {
			s.answerExtensionDialog(dialog.requestID,
				extensionUICancelledResponse(dialog.requestID))
		}
	})
	_ = row
}

// extensionDialogButton is one dialog button; the primary one carries the
// accent token and the rest stay quiet. The label doubles as the accessible
// label, and the element key stays independent of it.
func (s *appState) extensionDialogButton(c *ui.Context, p palette, key, label string,
	primary bool,
) ui.Element {
	button := ui.ButtonBase(c.Key(key)).
		Label(label).Height(30).Padding(0, 12).Radius(v12InputRadius).Shrink(0)
	foreground := p.text
	if primary {
		button = button.Background(p.accent)
		// The label sits on the accent fill, so it uses the contrast token for
		// filled accent controls, as the other filled buttons in the app do. The
		// palette's own accentText is an accent-tinted text colour for unstyled
		// text, and in the dark preset it equals the fill, which hid this label.
		foreground = p.onAccent
		if button.Hovered() || button.Focused() {
			button = button.Background(p.accentHover)
		}
	} else {
		button = button.Background(p.surfaceSubtle).Border(v12BorderWidth, p.border)
		if button.Hovered() || button.Focused() {
			button = button.Background(p.surfaceHover).Border(v12BorderWidth, p.borderStrong)
		}
	}
	if button.Pressed() {
		button.Opacity(.85)
	}
	button.Children(func() {
		components.Text(c, label).FontSize(fontBody).TextColor(foreground)
	})
	return button
}
