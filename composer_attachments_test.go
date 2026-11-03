package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// coreTestWriter records the JSONL lines an app send writes to Pi, so payload
// checks read exactly what the RPC would carry. No process and no model runs.
type coreTestWriter struct {
	mu    sync.Mutex
	lines []string
}

func (w *coreTestWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lines = append(w.lines, string(p))
	return len(p), nil
}

func (w *coreTestWriter) Close() error { return nil }

func (w *coreTestWriter) lastLine(t *testing.T) map[string]any {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.lines) == 0 {
		t.Fatal("no Pi RPC line was written")
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(w.lines[len(w.lines)-1])), &record); err != nil {
		t.Fatalf("parse written line: %v", err)
	}
	return record
}

func (w *coreTestWriter) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.lines)
}

// linesSnapshot decodes every line written so far, oldest first.
func (w *coreTestWriter) linesSnapshot(t *testing.T) []map[string]any {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	records := make([]map[string]any, 0, len(w.lines))
	for _, line := range w.lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &record); err != nil {
			t.Fatalf("parse written line: %v", err)
		}
		records = append(records, record)
	}
	return records
}

func newCoreTestClient() (*piClient, *coreTestWriter) {
	writer := &coreTestWriter{}
	client := &piClient{
		stdin:       writer,
		pending:     make(map[string]chan piRPCResponse),
		waitDone:    make(chan struct{}),
		streamParts: make(map[int]string),
	}
	return client, writer
}

// coreTestFiles makes one image, one ordinary document, one binary file, and a
// folder, all synthetic, under a temporary directory.
func coreTestFiles(t *testing.T) (image, document, binary, folder string) {
	t.Helper()
	root := t.TempDir()
	image = filepath.Join(root, "diagram.png")
	if err := os.WriteFile(image, []byte("\x89PNG-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	document = filepath.Join(root, "notes.txt")
	if err := os.WriteFile(document, []byte("synthetic notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	binary = filepath.Join(root, "payload.bin")
	if err := os.WriteFile(binary, []byte{0x00, 0x01, 0x02, 0xff}, 0o600); err != nil {
		t.Fatal(err)
	}
	folder = filepath.Join(root, "a-folder")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	return image, document, binary, folder
}

func TestCoreComposerAttachmentsImageRidesInRPCImagesArray(t *testing.T) {
	image, document, _, _ := coreTestFiles(t)
	client, writer := newCoreTestClient()
	state := &appState{
		pi:              client,
		piConn:          piConnected,
		piModels:        []piModel{{Provider: "qa", ID: "vision", Input: []string{"text", "image"}}},
		piModelProvider: "qa",
		piModelID:       "vision",
		composerDraft:   "read these",
		composerTableContext: &tableContextAttachment{
			ContextID: "ctx_1",
			Label:     "Selected rows",
		},
	}
	state.addComposerAttachmentPaths([]string{image, document})
	if len(state.composerAttachments) != 2 {
		t.Fatalf("attachments = %#v", state.composerAttachments)
	}
	if state.composerAttachments[0].kind != attachmentImage ||
		state.composerAttachments[0].mimeType != "image/png" {
		t.Fatalf("image attachment = %#v", state.composerAttachments[0])
	}
	if state.composerAttachments[1].kind != attachmentFile {
		t.Fatalf("document attachment = %#v", state.composerAttachments[1])
	}

	state.sendPromptWithMode(runningSendQueue)
	record := writer.lastLine(t)
	if record["type"] != "prompt" {
		t.Fatalf("record = %#v", record)
	}
	// The image travels as payload; the document stays a local path reference
	// that Pi's tools can read, and the table context still rides along.
	message, _ := record["message"].(string)
	if !strings.Contains(message, document) || strings.Contains(message, image) {
		t.Fatalf("message = %q", message)
	}
	if !strings.Contains(message, "[byop-table-context:ctx_1]") {
		t.Fatalf("table context lost from %q", message)
	}
	rawImages, ok := record["images"].([]any)
	if !ok || len(rawImages) != 1 {
		t.Fatalf("images = %#v", record["images"])
	}
	sentImage, _ := rawImages[0].(map[string]any)
	if sentImage["type"] != "image" || sentImage["mimeType"] != "image/png" {
		t.Fatalf("sent image = %#v", sentImage)
	}
	data, _ := sentImage["data"].(string)
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil || string(decoded) != "\x89PNG-fixture" {
		t.Fatalf("decoded image = %q, err %v", decoded, err)
	}
	if len(state.chat) != 2 || len(state.chat[0].attachments) != 2 {
		t.Fatalf("timeline attachments = %#v", state.chat)
	}
	if state.chat[0].attachments[0].Kind != attachmentImage ||
		strings.Contains(messageText(&state.chat[0]), document) {
		t.Fatalf("user row = %#v", state.chat[0])
	}
	if state.composerAttachments != nil {
		t.Fatalf("sent attachments stayed selected: %#v", state.composerAttachments)
	}
}

func TestCoreComposerAttachmentsKeepImageAsPathWhenModelIsTextOnly(t *testing.T) {
	image, _, binary, folder := coreTestFiles(t)
	state := &appState{
		piModels:        []piModel{{Provider: "qa", ID: "text", Input: []string{"text"}}},
		piModelProvider: "qa",
		piModelID:       "text",
	}
	state.addComposerAttachmentPaths([]string{image})
	if state.notice != text("chatImageUnsupported") {
		t.Fatalf("notice = %q, want the unsupported-image notice", state.notice)
	}
	if len(state.composerAttachments) != 1 || state.composerAttachments[0].kind != attachmentFile ||
		state.composerAttachments[0].path != image {
		t.Fatalf("image stayed unsupported silently: %#v", state.composerAttachments)
	}
	state.addComposerAttachmentPaths([]string{binary})
	if len(state.composerAttachments) != 2 || state.composerAttachments[1].kind != attachmentFile {
		t.Fatalf("binary attachment = %#v", state.composerAttachments)
	}
	// A folder cannot be attached, so it is reported rather than dropped in
	// silence.
	state.notice = ""
	state.addComposerAttachmentPaths([]string{folder})
	if state.notice == "" || len(state.composerAttachments) != 2 {
		t.Fatalf("folder drop: notice=%q attachments=%#v", state.notice, state.composerAttachments)
	}
	if got := attachmentPromptText("compare these", state.composerAttachments); !strings.Contains(got, binary) {
		t.Fatalf("prompt text = %q", got)
	}
}

func TestCoreComposerAttachmentsPromptBlockStaysOutOfDisplay(t *testing.T) {
	_, document, _, _ := coreTestFiles(t)
	state := &appState{}
	state.addComposerAttachmentPaths([]string{document})
	wire := attachmentPromptText("summarize this", state.composerAttachments)
	if !strings.Contains(wire, document) || !strings.Contains(wire, attachmentBlockStart) {
		t.Fatalf("wire prompt = %q", wire)
	}
	// Every display path goes through displayTableContextPrompt, including the
	// table-context combination and the transcript restore.
	combined := tableContextPrompt(wire, &tableContextAttachment{ContextID: "ctx_2", Label: "Rows"})
	// The table-context label stays visible, as it always has; only the
	// attachment block is stripped.
	if got := displayTableContextPrompt(combined); got != "summarize this\n\nRows" {
		t.Fatalf("display prompt = %q", got)
	}
	restored, err := rebuildChatFromPiMessages([]json.RawMessage{
		json.RawMessage(`{"role":"user","content":"summarize this\n\n` + attachmentBlockStart +
			`\n` + filepath.ToSlash(document) + `\n` + attachmentBlockEnd + `","timestamp":1}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != 1 || messageText(&restored[0]) != "summarize this" {
		t.Fatalf("restored = %#v", restored)
	}
}

func TestCoreComposerAttachmentsFailureKeepsSelectionAndNewPicks(t *testing.T) {
	image, document, binary, _ := coreTestFiles(t)
	state := &appState{}
	state.addComposerAttachmentPaths([]string{image, document})
	sent := cloneComposerAttachments(state.composerAttachments)
	// Files added while an asynchronous send was in flight stay selected.
	state.addComposerAttachmentPaths([]string{binary})
	state.clearComposerAttachments(sent)
	if len(state.composerAttachments) != 1 || state.composerAttachments[0].path != binary {
		t.Fatalf("after clear = %#v", state.composerAttachments)
	}
	state.restoreComposerAttachments(sent)
	if len(state.composerAttachments) != 3 || state.composerAttachments[0].path != image ||
		state.composerAttachments[2].path != binary {
		t.Fatalf("after restore = %#v", state.composerAttachments)
	}
}

func TestCoreComposerAttachmentsRejectedSendKeepsSelection(t *testing.T) {
	_, document, _, _ := coreTestFiles(t)
	state := &appState{piConn: piConnected, composerDraft: "still here"}
	state.addComposerAttachmentPaths([]string{document})
	state.sendPromptWithMode(runningSendQueue)
	if len(state.composerAttachments) != 1 {
		t.Fatalf("disconnected send cleared attachments: %#v", state.composerAttachments)
	}
	if state.composerDraft != "still here" {
		t.Fatalf("draft = %q", state.composerDraft)
	}
}

func TestCoreComposerAttachmentsFailedQueuedSendRestoresSelection(t *testing.T) {
	_, document, _, _ := coreTestFiles(t)
	client, _ := newCoreTestClient()
	state := &appState{
		pi:                 client,
		piEpoch:            4,
		sessionGen:         7,
		run:                runStreaming,
		activeRequest:      "active",
		runID:              "active",
		runningSendPending: true,
		runningSendAttachments: []composerAttachment{
			{path: document, name: filepath.Base(document), kind: attachmentFile},
		},
	}
	if state.completeRunningSend(client, "queued", runningSendQueue, 4, 7,
		"active", "active", runStreaming,
		piPromptResult{}, errors.New("queue unavailable")) {
		t.Fatal("failed queued send reported success")
	}
	if len(state.composerAttachments) != 1 || state.composerAttachments[0].path != document {
		t.Fatalf("failed queue lost attachments: %#v", state.composerAttachments)
	}
}

func TestCoreComposerAttachmentsDropOnlyReachesChat(t *testing.T) {
	_, document, _, _ := coreTestFiles(t)
	for _, test := range []struct {
		name     string
		page     pageID
		attached bool
	}{
		{name: "chat", page: pageChat, attached: true},
		{name: "plugins", page: pagePlugins},
		{name: "settings", page: pageSettings},
		{name: "automations", page: pageAutomations},
		{name: "plugin view", page: pagePluginView},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := &appState{page: test.page}
			state.handleFileDrop(&mygo.FileDropEvent{Paths: []string{document}, X: 10, Y: 10})
			if got := len(state.composerAttachments) > 0; got != test.attached {
				t.Fatalf("attached = %v, want %v (%#v)", got, test.attached, state.composerAttachments)
			}
		})
	}
}

func TestCoreComposerAttachmentPickerReplacesPlaceholder(t *testing.T) {
	_, document, _, _ := coreTestFiles(t)
	var askedTitle string
	var pickerCalls int
	state := &appState{composerAttachmentsPickerOverride: func(parent *mygo.Window, title string) ([]string, error) {
		askedTitle = title
		pickerCalls++
		return []string{document}, nil
	}}
	state.pickComposerAttachments()
	if askedTitle != text("chatAttachmentPickerTitle") {
		t.Fatalf("picker title = %q", askedTitle)
	}
	if len(state.composerAttachments) != 1 || state.composerAttachments[0].path != document {
		t.Fatalf("picked = %#v", state.composerAttachments)
	}
	// Cancelling the picker changes nothing.
	// A completed pick clears the in-flight flag, so the next click opens
	// another picker instead of being swallowed by the first sheet.
	cancel := func(*mygo.Window, string) ([]string, error) {
		pickerCalls++
		return nil, nil
	}
	state.composerAttachmentsPickerOverride = cancel
	state.pickComposerAttachments()
	state.pickComposerAttachments()
	if len(state.composerAttachments) != 1 {
		t.Fatalf("cancelled picker changed selection: %#v", state.composerAttachments)
	}
	if pickerCalls != 3 {
		t.Fatalf("picker calls = %d, want the second click to open again", pickerCalls)
	}
}

func TestCoreComposerAttachmentChipsAreVisibleAndRemovable(t *testing.T) {
	image, document, _, _ := coreTestFiles(t)
	state := layoutTestState(t)
	state.addComposerAttachmentPaths([]string{image, document})
	state.composerTableContext = &tableContextAttachment{ContextID: "ctx_3", Label: "Selected rows"}
	tt := ui.NewTester(state.view, 1180, 760)
	tt.Frame()
	for _, name := range []string{filepath.Base(image), filepath.Base(document)} {
		if !tt.HasText(name) {
			t.Fatalf("chip %q was not rendered", name)
		}
	}
	// The table-context chip coexists with the attachment chips.
	if !tt.HasText("Selected rows") {
		t.Fatal("table context chip disappeared beside attachments")
	}
	if err := tt.Click(text("chatAttachmentRemove")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(state.composerAttachments) != 1 {
		t.Fatalf("remove left %#v", state.composerAttachments)
	}
}

// TestCoreSentAttachmentsStayVisibleAsNames walks the timeline of a sent
// TestCoreQueuedSendEchoKeepsAttachmentNames covers review finding C2: Pi
// echoes a queued message only when its turn starts, after the ack has
// released the running-send state. The metas must wait keyed by the exact wire
// payload, keep their order for identical payloads, and land on the echo row
// without leaking paths into its text.
func TestCoreQueuedSendEchoKeepsAttachmentNames(t *testing.T) {
	_, document, _, _ := coreTestFiles(t)
	client := &piClient{}
	state := &appState{
		pi:                 client,
		piEpoch:            1,
		sessionGen:         1,
		run:                runStreaming,
		activeRequest:      "active",
		runID:              "active",
		runningSendPending: true,
	}
	state.addComposerAttachmentPaths([]string{document})
	wire := attachmentPromptText("look", state.composerAttachments)
	state.runningSendPrompt = wire
	state.runningSendDisplay = "look"
	state.runningSendAttachments = cloneComposerAttachments(state.composerAttachments)
	if !state.completeRunningSend(client, wire, runningSendQueue, 1, 1,
		"active", "active", runStreaming,
		piPromptResult{RequestID: "req", Disposition: "queued"}, nil) {
		t.Fatal("queued ack did not complete")
	}
	if len(state.pendingEchoAttachments[wire]) != 1 {
		t.Fatalf("queued ack kept no metas for the delayed echo: %#v",
			state.pendingEchoAttachments)
	}
	state.appendRunningUserMessage(wire)
	if len(state.chat) < 1 || len(state.chat[0].attachments) != 1 ||
		state.chat[0].attachments[0].Name != filepath.Base(document) {
		t.Fatalf("delayed echo row = %#v", state.chat)
	}
	if got := messageText(&state.chat[0]); strings.Contains(got, document) {
		t.Fatalf("delayed echo leaked paths: %q", got)
	}

	// Identical payloads keep their own attachments in order.
	dupState := &appState{}
	dupState.pendingEchoAttachments = map[string][][]chatAttachmentMeta{
		"same": {
			{{Name: "first.txt", Kind: attachmentFile}},
			{{Name: "second.txt", Kind: attachmentFile}},
		},
	}
	if got := dupState.takePendingEchoAttachments("same"); len(got) != 1 ||
		got[0].Name != "first.txt" {
		t.Fatalf("first duplicate = %#v", got)
	}
	if got := dupState.takePendingEchoAttachments("same"); len(got) != 1 ||
		got[0].Name != "second.txt" {
		t.Fatalf("second duplicate = %#v", got)
	}
	if got := dupState.takePendingEchoAttachments("same"); got != nil {
		t.Fatalf("exhausted queue = %#v", got)
	}
}

// TestCoreStartedSendDoesNotRecordEchoMetas covers review finding CF1: a
// running send whose ack says "started" already emitted (or de-duplicated) its
// user row, so it must NOT leave attachment metas waiting for a delayed echo.
// Because every attachment-only send carries the same localized fallback text,
// a stale "started" entry would be consumed by the next queued image-only send
// with the same wire payload, showing that earlier file's name instead.
func TestCoreStartedSendDoesNotRecordEchoMetas(t *testing.T) {
	imageX, _, _, _ := coreTestFiles(t)
	imageY := filepath.Join(filepath.Dir(imageX), "second-image.png")
	if err := os.WriteFile(imageY, []byte("\x89PNG-fixture-y"), 0o600); err != nil {
		t.Fatal(err)
	}
	fallback := text("chatAttachmentOnlyFallback")
	state := &appState{
		pi:            &piClient{},
		piEpoch:       1,
		sessionGen:    1,
		run:           runStreaming,
		activeRequest: "active",
		runID:         "active",
	}

	// Send X (image only) acks "started" with its echo already de-duplicated.
	state.addComposerAttachmentPaths([]string{imageX})
	state.runningSendPending = true
	state.runningSendEchoSeen = true
	state.runningSendPrompt = fallback
	state.runningSendDisplay = ""
	state.runningSendAttachments = cloneComposerAttachments(state.composerAttachments)
	state.composerAttachments = nil
	if !state.completeRunningSend(state.pi, fallback, runningSendQueue, 1, 1,
		"active", "active", runStreaming,
		piPromptResult{RequestID: "active", Disposition: "started"}, nil) {
		t.Fatal("started ack did not complete")
	}
	if len(state.pendingEchoAttachments) != 0 {
		t.Fatalf("started ack must not record echo metas: %#v", state.pendingEchoAttachments)
	}

	// Send Y (image only, same fallback wire) acks "queued".
	state.addComposerAttachmentPaths([]string{imageY})
	state.runningSendPending = true
	state.runningSendEchoSeen = false
	state.runningSendPrompt = fallback
	state.runningSendDisplay = ""
	state.runningSendAttachments = cloneComposerAttachments(state.composerAttachments)
	state.composerAttachments = nil
	if !state.completeRunningSend(state.pi, fallback, runningSendQueue, 1, 1,
		"active", "active", runStreaming,
		piPromptResult{RequestID: "queued-req", Disposition: "queued"}, nil) {
		t.Fatal("queued ack did not complete")
	}
	if len(state.pendingEchoAttachments[fallback]) != 1 {
		t.Fatalf("queued ack kept wrong metas: %#v", state.pendingEchoAttachments)
	}

	// Y's delayed echo must land with Y's own file, and consume the queue.
	state.appendRunningUserMessage(fallback)
	if len(state.chat) < 1 || len(state.chat[0].attachments) != 1 ||
		state.chat[0].attachments[0].Name != filepath.Base(imageY) {
		t.Fatalf("delayed echo row = %#v", state.chat)
	}
	if len(state.pendingEchoAttachments) != 0 {
		t.Fatalf("stale entry left after echo: %#v", state.pendingEchoAttachments)
	}
}

// TestCoreRestoredSessionShowsAttachmentNames covers review finding C3: a
// TestCoreRestoreTruncatedFailureRestoreNotices covers review finding C6: when
// a failed send's items plus newer picks exceed the limit, the newest picks
// are cut with a notice rather than silently.
func TestCoreRestoreTruncatedFailureRestoreNotices(t *testing.T) {
	root := t.TempDir()
	makeFile := func(name string) string {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	var sent []composerAttachment
	for index := 0; index < maxComposerAttachments; index++ {
		sent = append(sent, composerAttachment{
			path: makeFile(fmt.Sprintf("sent-%d.txt", index)),
			name: fmt.Sprintf("sent-%d.txt", index),
			kind: attachmentFile,
		})
	}
	state := &appState{}
	var newer []string
	for index := 0; index < 2; index++ {
		newer = append(newer, makeFile(fmt.Sprintf("newer-%d.txt", index)))
	}
	state.addComposerAttachmentPaths(newer)
	state.notice = ""
	state.restoreComposerAttachments(sent)
	if len(state.composerAttachments) != maxComposerAttachments {
		t.Fatalf("restored = %d, want the limit", len(state.composerAttachments))
	}
	if state.notice != textArgs("chatAttachmentSkipped", map[string]any{"count": 2}) {
		t.Fatalf("truncation notice = %q", state.notice)
	}
}

// TestCoreRestoredSessionShowsAttachmentNames covers review finding C3: a
// restored user message keeps the file names of its path block, and one image
// meta per image part, because Pi stores no name for image payloads.
func TestCoreRestoredSessionShowsAttachmentNames(t *testing.T) {
	_, document, _, _ := coreTestFiles(t)
	raw := json.RawMessage(`{"role":"user","content":[` +
		`{"type":"text","text":"look\n\n` + attachmentBlockStart + `\n` +
		filepath.ToSlash(document) + `\n` + attachmentBlockEnd + `"},` +
		`{"type":"image","data":"Zm9v","mimeType":"image/png"}],` +
		`"timestamp":1}`)
	restored, err := rebuildChatFromPiMessages([]json.RawMessage{raw})
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != 1 {
		t.Fatalf("restored = %#v", restored)
	}
	row := &restored[0]
	if got := messageText(row); got != "look" {
		t.Fatalf("restored text = %q", got)
	}
	if len(row.attachments) != 2 {
		t.Fatalf("restored attachments = %#v", row.attachments)
	}
	if row.attachments[0].Name != filepath.Base(document) ||
		row.attachments[0].Kind != attachmentFile {
		t.Fatalf("restored file meta = %#v", row.attachments[0])
	}
	if row.attachments[1].Kind != attachmentImage ||
		!strings.HasPrefix(row.attachments[1].Name, text("chatAttachmentImageLabel")) {
		t.Fatalf("restored image meta = %#v", row.attachments[1])
	}
}

// TestCoreSentAttachmentsStayVisibleAsNames walks the timeline of a sent
// TestCoreAttachmentOnlySendUsesFallbackLine covers review finding C10: an
// explicit send with files but no text goes out with one localized fallback
// line, because Pi's message field needs a non-empty string. Without files the
// empty notice still refuses the send.
func TestCoreAttachmentOnlySendUsesFallbackLine(t *testing.T) {
	_, document, _, _ := coreTestFiles(t)
	client, writer := newCoreTestClient()
	state := &appState{
		pi:              client,
		piConn:          piConnected,
		piModels:        []piModel{{Provider: "qa", ID: "vision", Input: []string{"text", "image"}}},
		piModelProvider: "qa",
		piModelID:       "vision",
	}
	state.sendPrompt()
	if state.notice != text("noticeMessageEmpty") || writer.count() != 0 {
		t.Fatalf("empty send without files: notice=%q lines=%d", state.notice, writer.count())
	}
	state.addComposerAttachmentPaths([]string{document})
	state.sendPrompt()
	record := writer.lastLine(t)
	message, _ := record["message"].(string)
	if !strings.Contains(message, document) ||
		!strings.Contains(message, text("chatAttachmentOnlyFallback")) {
		t.Fatalf("attachment-only message = %q", message)
	}
	if len(state.chat) < 1 || messageText(&state.chat[0]) != text("chatAttachmentOnlyFallback") ||
		len(state.chat[0].attachments) != 1 ||
		state.chat[0].attachments[0].Name != filepath.Base(document) {
		t.Fatalf("timeline row = %#v", state.chat)
	}
	if state.composerAttachments != nil {
		t.Fatalf("sent files stayed selected: %#v", state.composerAttachments)
	}
}

// TestCoreSentAttachmentsStayVisibleAsNames walks the timeline of a sent
// message: the file names stay readable, the path block stays hidden, and the
// row keeps no image bytes.
func TestCoreSentAttachmentsStayVisibleAsNames(t *testing.T) {
	image, document, _, _ := coreTestFiles(t)
	client, _ := newCoreTestClient()
	state := layoutTestState(t)
	state.pi = client
	state.piConn = piConnected
	state.piModels = []piModel{{Provider: "qa", ID: "vision", Input: []string{"text", "image"}}}
	state.piModelProvider = "qa"
	state.piModelID = "vision"
	state.chat = nil
	state.composerDraft = "look at these"
	state.addComposerAttachmentPaths([]string{image, document})
	state.sendPromptWithMode(runningSendQueue)

	tt := ui.NewTester(state.view, 1180, 760)
	tt.Frame()
	for _, name := range []string{filepath.Base(image), filepath.Base(document)} {
		if !tt.HasText(name) {
			t.Fatalf("sent attachment %q is missing from the timeline", name)
		}
	}
	for _, textValue := range tt.Texts() {
		if strings.Contains(textValue, attachmentBlockStart) || strings.Contains(textValue, document) {
			t.Fatalf("attachment paths leaked into the timeline: %q", textValue)
		}
	}
	if len(state.chat) == 0 || len(state.chat[0].attachments) != 2 {
		t.Fatalf("timeline row = %#v", state.chat)
	}
}

// TestCoreComposerImageSelectedBeforeModelSwitchStaysUsable walks the case where
// the model changes to a text-only one after an image was picked: the image goes
// out as a path reference with a notice, never silently dropped.
func TestCoreComposerImageSelectedBeforeModelSwitchStaysUsable(t *testing.T) {
	image, _, _, _ := coreTestFiles(t)
	client, writer := newCoreTestClient()
	state := &appState{
		pi:     client,
		piConn: piConnected,
		piModels: []piModel{
			{Provider: "qa", ID: "vision", Input: []string{"text", "image"}},
			{Provider: "qa", ID: "text", Input: []string{"text"}},
		},
		piModelProvider: "qa",
		piModelID:       "vision",
		composerDraft:   "describe this",
	}
	state.addComposerAttachmentPaths([]string{image})
	if state.composerAttachments[0].kind != attachmentImage {
		t.Fatalf("picked attachment = %#v", state.composerAttachments[0])
	}
	state.piModelID = "text"
	state.sendPromptWithMode(runningSendQueue)
	record := writer.lastLine(t)
	if _, hasImages := record["images"]; hasImages {
		t.Fatalf("text-only model received images: %#v", record)
	}
	message, _ := record["message"].(string)
	if !strings.Contains(message, image) {
		t.Fatalf("image was dropped from %q", message)
	}
	if state.notice != text("chatImageUnsupported") {
		t.Fatalf("notice = %q", state.notice)
	}
	if len(state.chat) != 2 || len(state.chat[0].attachments) != 1 ||
		state.chat[0].attachments[0].Kind != attachmentFile {
		t.Fatalf("timeline row = %#v", state.chat)
	}
}
