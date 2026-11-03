package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// coreTestDialogEvent builds a dialog request event the way piClient emits one,
// so the tests drive the same shape Pi sends without a process or a model.
func coreTestDialogEvent(client *piClient, id, method string) piEvent {
	event := piEvent{
		Kind:            piEventExtensionDialog,
		Source:          client,
		RequestID:       id,
		ExtensionMethod: method,
		Text:            "Allow dangerous command?",
	}
	switch method {
	case extensionDialogSelect:
		event.ExtensionOptions = []string{"Allow", "Block"}
	case extensionDialogConfirm:
		event.ExtensionMessage = "All messages will be lost."
	case extensionDialogInput:
		event.ExtensionPlaceholder = "type something"
	case extensionDialogEditor:
		event.ExtensionPrefill = "Line 1\nLine 2"
	}
	return event
}

func TestCoreExtensionDialogSelectAnswersOriginalIDWithValue(t *testing.T) {
	client, writer := newCoreTestClient()
	state := &appState{pi: client, piEpoch: 3}
	state.handlePiEvent(coreTestDialogEvent(client, "uuid-1", extensionDialogSelect), 3)
	if len(state.extensionDialogs) != 1 {
		t.Fatalf("pending = %#v", state.extensionDialogs)
	}
	dialog := state.currentExtensionDialog()
	if dialog == nil || dialog.client != client || dialog.epoch != 3 {
		t.Fatalf("dialog = %#v", dialog)
	}
	if !state.answerExtensionDialog(dialog.requestID,
		extensionUIValueResponse(dialog.requestID, dialog.options[0])) {
		t.Fatal("answer was refused")
	}
	record := writer.lastLine(t)
	if record["type"] != "extension_ui_response" || record["id"] != "uuid-1" ||
		record["value"] != "Allow" {
		t.Fatalf("response = %#v", record)
	}
	if _, hasCancelled := record["cancelled"]; hasCancelled {
		t.Fatalf("value response also carried cancelled: %#v", record)
	}
	if _, hasConfirmed := record["confirmed"]; hasConfirmed {
		t.Fatalf("value response also carried confirmed: %#v", record)
	}
	if len(state.extensionDialogs) != 0 {
		t.Fatalf("answered request stayed pending: %#v", state.extensionDialogs)
	}
	// A second answer for the same ID never reaches Pi twice.
	if state.answerExtensionDialog("uuid-1", extensionUIValueResponse("uuid-1", "again")) {
		t.Fatal("stale answer was accepted")
	}
	if writer.count() != 1 {
		t.Fatalf("Pi received %d responses, want 1", writer.count())
	}
}

func TestCoreExtensionDialogQueueKeepsEveryRequest(t *testing.T) {
	client, writer := newCoreTestClient()
	state := &appState{pi: client, piEpoch: 1}
	state.handlePiEvent(coreTestDialogEvent(client, "a", extensionDialogSelect), 1)
	state.handlePiEvent(coreTestDialogEvent(client, "b", extensionDialogInput), 1)
	state.handlePiEvent(coreTestDialogEvent(client, "a", extensionDialogSelect), 1)
	if len(state.extensionDialogs) != 2 {
		t.Fatalf("queue = %#v", state.extensionDialogs)
	}
	if state.extensionDialogs[0].requestID != "a" || state.extensionDialogs[1].requestID != "b" {
		t.Fatalf("queue order = %#v", state.extensionDialogs)
	}
	// The draft belongs to the dialog the panel shows, and follows the queue.
	if state.extensionDialogDraft != "" {
		t.Fatalf("draft = %q, want empty for select", state.extensionDialogDraft)
	}
	state.answerExtensionDialog("a", extensionUIValueResponse("a", "Allow"))
	if state.extensionDialogDraft != "" {
		t.Fatalf("draft = %q, want empty for the queued input", state.extensionDialogDraft)
	}
	// Answering the second request while the first is gone still uses its ID.
	state.answerExtensionDialog("b", extensionUIValueResponse("b", "typed"))
	if writer.count() != 2 {
		t.Fatalf("Pi received %d responses, want 2", writer.count())
	}
	if len(state.extensionDialogs) != 0 {
		t.Fatalf("queue = %#v", state.extensionDialogs)
	}
}

// TestCoreExtensionDialogDraftSurvivesQueueAndExpiry covers review finding C1:
// a request that only queues behind the front dialog, and a request that
// expires behind it, must not touch the draft the user is typing on screen.
func TestCoreExtensionDialogDraftSurvivesQueueAndExpiry(t *testing.T) {
	client, writer := newCoreTestClient()
	state := &appState{pi: client, piEpoch: 1}

	// A typed answer to the front input survives a select that queues behind it.
	state.handlePiEvent(coreTestDialogEvent(client, "a", extensionDialogInput), 1)
	state.extensionDialogDraft = "half typed"
	state.handlePiEvent(coreTestDialogEvent(client, "b", extensionDialogSelect), 1)
	if state.extensionDialogDraft != "half typed" {
		t.Fatalf("queued select reset the draft to %q", state.extensionDialogDraft)
	}
	// So does an editor whose prefill would otherwise replace the draft, and
	// submitting the front input still sends what the user typed.
	state.handlePiEvent(coreTestDialogEvent(client, "c", extensionDialogEditor), 1)
	if state.extensionDialogDraft != "half typed" {
		t.Fatalf("queued editor reset the draft to %q", state.extensionDialogDraft)
	}
	state.answerExtensionDialogValue(state.currentExtensionDialog())
	record := writer.lastLine(t)
	if record["id"] != "a" || record["value"] != "half typed" {
		t.Fatalf("front answer = %#v", record)
	}
	// Clear the requests that queued behind "a" so the next case starts alone.
	state.answerExtensionDialog("b", extensionUICancelledResponse("b"))
	state.answerExtensionDialog("c", extensionUICancelledResponse("c"))

	// A timed confirm expiring behind the front input leaves its draft alone.
	state.handlePiEvent(coreTestDialogEvent(client, "d", extensionDialogInput), 1)
	timed := coreTestDialogEvent(client, "e", extensionDialogConfirm)
	timed.ExtensionTimeoutMS = 100
	state.handlePiEvent(timed, 1)
	state.extensionDialogDraft = "still typing"
	state.pruneExtensionDialogs(time.Now().Add(150 * time.Millisecond))
	if len(state.extensionDialogs) != 1 || state.extensionDialogs[0].requestID != "d" {
		t.Fatalf("queue after expiry = %#v", state.extensionDialogs)
	}
	if state.extensionDialogDraft != "still typing" {
		t.Fatalf("non-front expiry reset the draft to %q", state.extensionDialogDraft)
	}

	// When the front request itself changes, the draft follows the new dialog.
	state.answerExtensionDialog("d", extensionUICancelledResponse("d"))
	if state.extensionDialogDraft != "" {
		t.Fatalf("draft = %q, want empty with an empty queue", state.extensionDialogDraft)
	}
	if len(state.extensionDialogs) != 0 {
		t.Fatalf("queue = %#v", state.extensionDialogs)
	}
}

func TestCoreExtensionDialogConfirmDistinguishesDeclineAndCancel(t *testing.T) {
	client, writer := newCoreTestClient()
	state := &appState{pi: client, piEpoch: 1}
	state.handlePiEvent(coreTestDialogEvent(client, "c1", extensionDialogConfirm), 1)
	state.declineExtensionDialog("c1")
	declined := writer.lastLine(t)
	confirmed, ok := declined["confirmed"].(bool)
	if !ok || confirmed || declined["id"] != "c1" {
		t.Fatalf("decline response = %#v", declined)
	}
	if _, hasCancelled := declined["cancelled"]; hasCancelled {
		t.Fatalf("confirmed:false must not also be a cancellation: %#v", declined)
	}

	state.handlePiEvent(coreTestDialogEvent(client, "c2", extensionDialogConfirm), 1)
	state.answerExtensionDialog("c2", extensionUIConfirmedResponse("c2", true))
	accepted := writer.lastLine(t)
	if confirmed, ok := accepted["confirmed"].(bool); !ok || !confirmed {
		t.Fatalf("confirm response = %#v", accepted)
	}

	state.handlePiEvent(coreTestDialogEvent(client, "c3", extensionDialogConfirm), 1)
	state.answerExtensionDialog("c3", extensionUICancelledResponse("c3"))
	cancelled := writer.lastLine(t)
	if cancelled["cancelled"] != true || cancelled["id"] != "c3" {
		t.Fatalf("cancel response = %#v", cancelled)
	}
	if _, hasConfirmed := cancelled["confirmed"]; hasConfirmed {
		t.Fatalf("cancelled response also carried confirmed: %#v", cancelled)
	}
}

func TestCoreExtensionDialogEditorReturnsExactText(t *testing.T) {
	client, writer := newCoreTestClient()
	state := &appState{pi: client, piEpoch: 1}
	state.handlePiEvent(coreTestDialogEvent(client, "e1", extensionDialogEditor), 1)
	dialog := state.currentExtensionDialog()
	if dialog == nil || state.extensionDialogDraft != "Line 1\nLine 2" {
		t.Fatalf("draft = %q", state.extensionDialogDraft)
	}
	// The user edits with leading/trailing whitespace and blank lines kept.
	state.extensionDialogDraft = "  Line 1\n\nLine 3 (edited)  \n"
	state.answerExtensionDialogValue(dialog)
	record := writer.lastLine(t)
	if record["id"] != "e1" || record["value"] != "  Line 1\n\nLine 3 (edited)  \n" {
		t.Fatalf("editor response = %#v", record)
	}
	if _, hasCancelled := record["cancelled"]; hasCancelled {
		t.Fatalf("an edited value must not look cancelled: %#v", record)
	}
	// An empty editor answer is a real empty value, not a cancellation.
	state.handlePiEvent(coreTestDialogEvent(client, "e2", extensionDialogEditor), 1)
	dialog = state.currentExtensionDialog()
	state.extensionDialogDraft = ""
	state.answerExtensionDialogValue(dialog)
	record = writer.lastLine(t)
	value, present := record["value"].(string)
	if !present || value != "" || record["id"] != "e2" {
		t.Fatalf("empty editor response = %#v", record)
	}
}

func TestCoreExtensionDialogStaleClientNeverAnswers(t *testing.T) {
	oldClient, oldWriter := newCoreTestClient()
	newClient, newWriter := newCoreTestClient()
	state := &appState{pi: oldClient, piEpoch: 4}
	state.handlePiEvent(coreTestDialogEvent(oldClient, "s1", extensionDialogSelect), 4)

	// Reconnection: the process that asked is gone, so its answer is dropped and
	// no response is written to either client.
	state.pi = newClient
	state.piEpoch = 5
	if !state.answerExtensionDialog("s1", extensionUIValueResponse("s1", "Allow")) {
		t.Fatal("stale answer should still retire the request")
	}
	if oldWriter.count() != 0 || newWriter.count() != 0 {
		t.Fatalf("stale answer reached Pi: old=%d new=%d", oldWriter.count(), newWriter.count())
	}
	if len(state.extensionDialogs) != 0 {
		t.Fatalf("stale request stayed pending: %#v", state.extensionDialogs)
	}

	// Same client but older epoch also stays behind.
	state.handlePiEvent(coreTestDialogEvent(newClient, "s2", extensionDialogInput), 5)
	state.piEpoch = 6
	state.answerExtensionDialog("s2", extensionUIValueResponse("s2", "typed"))
	if newWriter.count() != 0 {
		t.Fatalf("epoch mismatch wrote %d lines", newWriter.count())
	}
}

func TestCoreExtensionDialogReplacementCancelsPendingRequests(t *testing.T) {
	client, writer := newCoreTestClient()
	state := &appState{pi: client, piEpoch: 1}
	state.handlePiEvent(coreTestDialogEvent(client, "r1", extensionDialogSelect), 1)
	state.handlePiEvent(coreTestDialogEvent(client, "r2", extensionDialogInput), 1)

	// A session replacement cancels both before the process goes away.
	state.cancelExtensionDialogs()
	if writer.count() != 2 || len(state.extensionDialogs) != 0 {
		t.Fatalf("after cancel: lines=%d pending=%#v", writer.count(), state.extensionDialogs)
	}
	for _, line := range writer.linesSnapshot(t) {
		if line["id"] == "" || line["cancelled"] != true {
			t.Fatalf("cancel response = %#v", line)
		}
	}

	// Channel refresh cancels only its own client's requests. A request from the
	// incoming client is queued directly, because handlePiEvent ignores events
	// that do not come from the live client.
	other, otherWriter := newCoreTestClient()
	state.handlePiEvent(coreTestDialogEvent(client, "r3", extensionDialogConfirm), 1)
	state.queueExtensionDialog(coreTestDialogEvent(other, "r4", extensionDialogConfirm), 1)
	state.cancelExtensionDialogsFor(client)
	if writer.count() != 3 {
		t.Fatalf("own client lines = %d, want 3", writer.count())
	}
	if otherWriter.count() != 0 {
		t.Fatalf("other client received %d lines", otherWriter.count())
	}
	if len(state.extensionDialogs) != 1 || state.extensionDialogs[0].requestID != "r4" {
		t.Fatalf("remaining queue = %#v", state.extensionDialogs)
	}

	// Cancelling the last request still answers it: an empty queue after the
	// removal must not skip the cancellation response (review C1-R1).
	state.cancelExtensionDialogsFor(other)
	if otherWriter.count() != 1 {
		t.Fatalf("last cancel wrote %d lines, want 1", otherWriter.count())
	}
	last := otherWriter.lastLine(t)
	if last["id"] != "r4" || last["cancelled"] != true {
		t.Fatalf("last cancel response = %#v", last)
	}
	if len(state.extensionDialogs) != 0 || state.extensionDialogDraft != "" {
		t.Fatalf("after cancelling all: pending=%#v draft=%q",
			state.extensionDialogs, state.extensionDialogDraft)
	}

	// A process that already exited gets no answer, and nothing lingers.
	state.discardExtensionDialogs()
	if len(state.extensionDialogs) != 0 || state.extensionDialogDraft != "" {
		t.Fatalf("after discard = %#v draft=%q", state.extensionDialogs, state.extensionDialogDraft)
	}
}

func TestCoreExtensionDialogTimeoutRetiresStaleUI(t *testing.T) {
	client, writer := newCoreTestClient()
	state := &appState{pi: client, piEpoch: 1}
	event := coreTestDialogEvent(client, "t1", extensionDialogSelect)
	event.ExtensionTimeoutMS = 100
	state.handlePiEvent(event, 1)
	if state.extensionDialogs[0].expiresAt.IsZero() {
		t.Fatal("timeout did not set an expiry")
	}
	// Pi resolves an expired request itself, so the app only closes the dialog;
	// it must not answer late.
	state.pruneExtensionDialogs(time.Now().Add(150 * time.Millisecond))
	if len(state.extensionDialogs) != 0 {
		t.Fatalf("expired dialog stayed: %#v", state.extensionDialogs)
	}
	if writer.count() != 0 {
		t.Fatalf("expired dialog was answered %d times", writer.count())
	}
	// A request without a timeout stays until the user answers.
	state.handlePiEvent(coreTestDialogEvent(client, "t2", extensionDialogInput), 1)
	state.pruneExtensionDialogs(time.Now().Add(time.Hour))
	if len(state.extensionDialogs) != 1 || state.extensionDialogs[0].requestID != "t2" {
		t.Fatalf("queue = %#v", state.extensionDialogs)
	}
}

func TestCoreExtensionDialogNotifyAndStatusStayUnanswered(t *testing.T) {
	client, writer := newCoreTestClient()
	events := make(chan piEvent, 4)
	client.events = func(event piEvent) { events <- event }
	client.handleRecord([]byte(`{
		"type":"extension_ui_request","id":"n1","method":"notify",
		"message":"Command blocked by user","notifyType":"warning"
	}`))
	notify := <-events
	if notify.Kind != piEventExtensionNotify || notify.NotifyType != "warning" ||
		notify.Text != "Command blocked by user" {
		t.Fatalf("notify event = %#v", notify)
	}
	client.handleRecord([]byte(`{
		"type":"extension_ui_request","id":"n2","method":"setStatus",
		"statusKey":"lint","statusText":"checking"
	}`))
	status := <-events
	if status.Kind != piEventExtensionStatus || status.StatusKey != "lint" ||
		status.StatusText == nil || *status.StatusText != "checking" {
		t.Fatalf("status event = %#v", status)
	}
	client.handleRecord([]byte(`{
		"type":"extension_ui_request","id":"n3","method":"select",
		"title":"Pick","options":["a","b"],"timeout":5000
	}`))
	dialog := <-events
	if dialog.Kind != piEventExtensionDialog || dialog.RequestID != "n3" ||
		dialog.ExtensionTimeoutMS != 5000 || len(dialog.ExtensionOptions) != 2 {
		t.Fatalf("dialog event = %#v", dialog)
	}
	// Nothing was auto-cancelled on arrival any more.
	if writer.count() != 0 {
		t.Fatalf("request was answered on arrival: %d lines", writer.count())
	}
}

// TestCoreExtensionDialogFractionalTimeoutStaysUsable covers review finding C4:
// the SDK types timeout as a number, so a fractional value must still reach the
// dialog instead of failing the whole request. It rounds up so the app never
// retires the dialog before Pi resolves its own default.
func TestCoreExtensionDialogFractionalTimeoutStaysUsable(t *testing.T) {
	client, writer := newCoreTestClient()
	events := make(chan piEvent, 1)
	client.events = func(event piEvent) { events <- event }
	client.handleRecord([]byte(`{
		"type":"extension_ui_request","id":"f1","method":"input",
		"title":"Name","timeout":1500.5
	}`))
	dialog := <-events
	if dialog.Kind != piEventExtensionDialog || dialog.RequestID != "f1" ||
		dialog.ExtensionTimeoutMS != 1501 {
		t.Fatalf("fractional timeout event = %#v", dialog)
	}
	if writer.count() != 0 {
		t.Fatalf("request was answered on arrival: %d lines", writer.count())
	}
}

func TestCoreExtensionDialogPanelUsesNativeControls(t *testing.T) {
	client, writer := newCoreTestClient()
	state := layoutTestState(t)
	state.pi = client
	state.piEpoch = 1
	state.piConn = piConnected
	state.handlePiEvent(coreTestDialogEvent(client, "u1", extensionDialogSelect), 1)
	tt := ui.NewTester(state.rootView, 1180, 760)
	tt.Frame()
	if !tt.HasText("Allow dangerous command?") || !tt.HasText("Allow") || !tt.HasText("Block") {
		t.Fatalf("select dialog was not rendered: %#v", tt.Texts())
	}
	if err := tt.Click("Block"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	record := writer.lastLine(t)
	if record["id"] != "u1" || record["value"] != "Block" {
		t.Fatalf("panel response = %#v", record)
	}
	if len(state.extensionDialogs) != 0 {
		t.Fatalf("panel left the request pending: %#v", state.extensionDialogs)
	}
}

// TestCoreExtensionDialogPanelsInBothAppearances walks the native panels in the
// BYOP and J Agent appearances, which is what a real-window walkthrough would
// otherwise have to cover.
func TestCoreExtensionDialogPanelsInBothAppearances(t *testing.T) {
	for _, preset := range []appearancePreset{appearancePresetBYOP, appearancePresetJAgent} {
		t.Run(string(preset), func(t *testing.T) {
			client, writer := newCoreTestClient()
			state := layoutTestState(t)
			state.preferences.AppearancePreset = string(preset)
			state.pi = client
			state.piEpoch = 1
			state.piConn = piConnected

			state.handlePiEvent(coreTestDialogEvent(client, "i1", extensionDialogInput), 1)
			tt := ui.NewTester(state.rootView, 1180, 760)
			tt.Frame()
			if !tt.HasText("Allow dangerous command?") || !tt.HasText(text("extensionUIDialogConfirm")) ||
				!tt.HasText(text("extensionUIDialogCancel")) {
				t.Fatalf("input dialog was not rendered: %#v", tt.Texts())
			}
			if err := tt.Click(text("extensionUIDialogCancel")); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if record := writer.lastLine(t); record["cancelled"] != true || record["id"] != "i1" {
				t.Fatalf("cancel response = %#v", record)
			}

			// A backdrop or Escape dismissal cancels the same request.
			state.handlePiEvent(coreTestDialogEvent(client, "i2", extensionDialogInput), 1)
			tt.Frame()
			tt.Key(0, ui.KeyEscape)
			tt.Frame()
			if record := writer.lastLine(t); record["cancelled"] != true || record["id"] != "i2" {
				t.Fatalf("dismissal response = %#v", record)
			}
			if len(state.extensionDialogs) != 0 {
				t.Fatalf("dismissal left a pending request: %#v", state.extensionDialogs)
			}

			// Confirm offers both a decline and a confirm, and decline answers
			// confirmed: false.
			state.handlePiEvent(coreTestDialogEvent(client, "i3", extensionDialogConfirm), 1)
			tt.Frame()
			if !tt.HasText("All messages will be lost.") ||
				!tt.HasText(text("extensionUIDialogDecline")) {
				t.Fatalf("confirm dialog was not rendered: %#v", tt.Texts())
			}
			if err := tt.Click(text("extensionUIDialogDecline")); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			record := writer.lastLine(t)
			if confirmed, ok := record["confirmed"].(bool); !ok || confirmed || record["id"] != "i3" {
				t.Fatalf("decline response = %#v", record)
			}
		})
	}
}

// TestCoreComposerAttachmentChipsInJAgent walks the J Agent composer, where the
// same attachment row and picker replace the old placeholder.
func TestCoreComposerAttachmentChipsInJAgent(t *testing.T) {
	image, document, _, _ := coreTestFiles(t)
	state := layoutTestState(t)
	state.preferences.AppearancePreset = string(appearancePresetJAgent)
	state.addComposerAttachmentPaths([]string{image})
	state.addComposerAttachmentPaths([]string{document})
	state.composerTableContext = &tableContextAttachment{ContextID: "ctx_4", Label: "Selected rows"}
	tt := ui.NewTester(state.view, 1180, 760)
	tt.Frame()
	for _, name := range []string{filepath.Base(image), filepath.Base(document), "Selected rows"} {
		if !tt.HasText(name) {
			t.Fatalf("%q was not rendered in the J Agent composer", name)
		}
	}
	if !tt.HasText(text("chatAttachmentButton")) {
		t.Fatal("attachment picker missing in the J Agent composer")
	}
	if err := tt.Click(text("chatAttachmentRemove")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(state.composerAttachments) != 1 || state.composerAttachments[0].path != document {
		t.Fatalf("after remove = %#v", state.composerAttachments)
	}
}
