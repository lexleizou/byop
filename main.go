package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	appTitle = "Build You Own Pi"
	appShort = "BYOP"
)

func main() {
	state := newAppState()
	mygo.Theme.SetSource(themeSource(state.themeMode))

	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:           appTitle,
			Width:           1180,
			Height:          760,
			MinWidth:        760,
			MinHeight:       560,
			TitleBarStyle:   mygo.TitleBarHiddenInset,
			BackgroundColor: "light-dark(#fcfbf9, #211f1c)",
			StateKey:        "byop-p2-pi-rpc",
			Content:         ui.View(state.view),
		})
		state.window = win
		win.OnClosed(state.shutdownPi)
		switch strings.ToLower(os.Getenv("BYOP_PI_CONNECT")) {
		case "0", "false", "off":
		default:
			state.connectPi()
		}
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

type pageID string

const (
	pageChat        pageID = "chat"
	pagePlugins     pageID = "plugins"
	pageLibrary     pageID = "library"
	pageDocument    pageID = "document"
	pageAutomations pageID = "automations"
	pageSettings    pageID = "settings"
)

type themeMode string

const (
	themeSystem themeMode = "system"
	themeLight  themeMode = "light"
	themeDark   themeMode = "dark"
)

type piConnectionState string

const (
	piDisconnected  piConnectionState = "disconnected"
	piConnecting    piConnectionState = "connecting"
	piConnected     piConnectionState = "connected"
	piConnectionErr piConnectionState = "error"
)

type runState string

const (
	runIdle        runState = "idle"
	runSubmitting  runState = "submitting"
	runStreaming   runState = "streaming"
	runStopping    runState = "stopping"
	runCompleted   runState = "completed"
	runStopped     runState = "stopped"
	runInterrupted runState = "interrupted"
	runFailed      runState = "failed"
)

type chatRole string

const (
	chatUser      chatRole = "user"
	chatAssistant chatRole = "assistant"
)

type toolCallStatus string

const (
	toolCallRunning     toolCallStatus = "running"
	toolCallCompleted   toolCallStatus = "completed"
	toolCallFailed      toolCallStatus = "failed"
	toolCallCancelled   toolCallStatus = "cancelled"
	toolCallInterrupted toolCallStatus = "interrupted"
)

type toolCallView struct {
	id         string
	name       string
	arguments  string
	result     string
	partial    string
	status     toolCallStatus
	durationMS int64
	expanded   bool
}

type chatBlockKind string

const (
	chatBlockText chatBlockKind = "text"
	chatBlockTool chatBlockKind = "tool"
)

type chatBlock struct {
	kind chatBlockKind
	text string
	tool toolCallView
}

type chatMessage struct {
	role   chatRole
	status string
	blocks []chatBlock
}

type appState struct {
	page          pageID
	themeMode     themeMode
	navCollapsed  bool
	tasksOpen     bool
	notice        string
	composerDraft string
	pluginSearch  string
	librarySearch string

	window        *mygo.Window
	closed        atomic.Bool
	pi            *piClient
	piConn        piConnectionState
	piPath        string
	piVersion     string
	piSessionID   string
	piModel       string
	piError       string
	piConnecting  bool
	piEpoch       uint64
	run           runState
	runID         string
	activePrompt  string
	activeRequest string
	chat          []chatMessage
}

func newAppState() *appState {
	s := &appState{
		page:      pageChat,
		themeMode: themeSystem,
		piConn:    piDisconnected,
		run:       runIdle,
	}
	if p := pageID(strings.ToLower(os.Getenv("BYOP_PAGE"))); validPage(p) {
		s.page = p
	}
	switch themeMode(strings.ToLower(os.Getenv("BYOP_THEME"))) {
	case themeLight:
		s.themeMode = themeLight
	case themeDark:
		s.themeMode = themeDark
	}
	switch strings.ToLower(os.Getenv("BYOP_TASKS")) {
	case "1", "true", "open":
		s.tasksOpen = true
	}
	switch strings.ToLower(os.Getenv("BYOP_NAV")) {
	case "collapsed", "collapsed-true":
		s.navCollapsed = true
	}
	switch strings.ToLower(os.Getenv("BYOP_DEMO_TOOLS")) {
	case "1", "true", "on":
		s.seedToolDemo()
	}
	return s
}

func (s *appState) seedToolDemo() {
	s.piConn = piConnected
	s.piVersion = "1.3.0"
	s.piModel = "Pi default"
	s.run = runStreaming
	s.runID = "demo-run"
	s.activeRequest = "demo-run"
	s.activePrompt = "Review the sample project."
	s.chat = []chatMessage{
		{
			role: chatUser,
			blocks: []chatBlock{{
				kind: chatBlockText,
				text: "Review the sample project.",
			}},
			status: "sent",
		},
		{
			role:   chatAssistant,
			status: "Streaming",
			blocks: []chatBlock{
				{
					kind: chatBlockText,
					text: "I’m checking the project files.",
				},
				{
					kind: chatBlockTool,
					tool: toolCallView{
						id:         "demo-tool-complete",
						name:       "bash",
						arguments:  "git status --short",
						result:     " M app.go",
						status:     toolCallCompleted,
						durationMS: 84,
					},
				},
				{
					kind: chatBlockText,
					text: "The working tree is clean except for the sample app. I’m reading the remaining documentation now.",
				},
				{
					kind: chatBlockTool,
					tool: toolCallView{
						id:        "demo-tool-running",
						name:      "read",
						arguments: "README.md",
						partial:   "Reading 3 sections…",
						status:    toolCallRunning,
						expanded:  true,
					},
				},
			},
		},
	}
}

func validPage(p pageID) bool {
	switch p {
	case pageChat, pagePlugins, pageLibrary, pageDocument, pageAutomations, pageSettings:
		return true
	default:
		return false
	}
}

func themeSource(mode themeMode) mygo.ThemeSource {
	switch mode {
	case themeLight:
		return mygo.ThemeLight
	case themeDark:
		return mygo.ThemeDark
	default:
		return mygo.ThemeSystem
	}
}

func (s *appState) setTheme(mode themeMode) {
	if s.themeMode == mode {
		return
	}
	s.themeMode = mode
	mygo.Theme.SetSource(themeSource(mode))
}

func (s *appState) setPage(p pageID) {
	s.page = p
	s.notice = ""
}

func (s *appState) showNotice(text string) {
	s.notice = text
}

func (s *appState) postUI(fn func()) {
	if s.closed.Load() || s.window == nil {
		return
	}
	s.window.Update(fn)
}

func (s *appState) connectPi() {
	if s.piConnecting || s.pi != nil {
		return
	}
	s.piEpoch++
	epoch := s.piEpoch
	path, err := resolvePiPath()
	if err != nil {
		s.piConn = piConnectionErr
		s.piError = err.Error()
		s.notice = err.Error()
		return
	}

	s.piConnecting = true
	s.piConn = piConnecting
	s.piPath = path
	s.piError = ""
	s.notice = "Connecting to external Pi CLI…"

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		version := piCLIVersion(ctx, path)
		client, err := startPiClient(path, "", func(event piEvent) {
			s.postUI(func() { s.handlePiEvent(event, epoch) })
		})
		if err != nil {
			s.postUI(func() {
				s.piConnecting = false
				s.piConn = piConnectionErr
				s.piError = err.Error()
				s.notice = "Pi connection failed: " + err.Error()
			})
			return
		}
		state, err := client.GetState(ctx)
		if err != nil {
			client.Close()
			s.postUI(func() {
				s.piConnecting = false
				s.piConn = piConnectionErr
				s.piError = err.Error()
				s.notice = "Pi RPC handshake failed: " + err.Error()
			})
			return
		}
		s.postUI(func() {
			s.pi = client
			s.piConnecting = false
			s.piConn = piConnected
			s.piVersion = version
			s.piSessionID = state.SessionID
			s.piModel = strings.TrimSpace(strings.Join(nonEmpty(state.Model.Provider, state.Model.Name), " / "))
			s.piError = ""
			s.notice = "External Pi CLI connected."
		})
	}()
}

func (s *appState) reconnectPi() {
	if s.run == runSubmitting || s.run == runStreaming || s.run == runStopping {
		s.markOpenTools(toolCallInterrupted)
		s.setAssistantStatus("Interrupted")
	}
	if s.pi != nil {
		old := s.pi
		s.pi = nil
		old.Close()
	}
	s.piConn = piDisconnected
	s.run = runIdle
	s.piConnecting = false
	s.connectPi()
}

func (s *appState) shutdownPi() {
	s.closed.Store(true)
	if s.pi != nil {
		s.pi.Close()
		s.pi = nil
	}
}

func (s *appState) sendPrompt() {
	text := strings.TrimSpace(s.composerDraft)
	if text == "" {
		s.showNotice("Type a message before sending.")
		return
	}
	if s.pi == nil || s.piConn != piConnected {
		s.showNotice("Pi CLI is not connected. Reconnect from Settings.")
		return
	}
	if s.run == runSubmitting || s.run == runStreaming || s.run == runStopping {
		s.showNotice("Pi is already working. Stop the current run before sending another message.")
		return
	}

	requestID, err := s.pi.Prompt(text)
	if err != nil {
		s.run = runFailed
		s.piError = err.Error()
		s.showNotice("Could not send to Pi: " + err.Error())
		return
	}
	s.composerDraft = ""
	s.notice = ""
	s.run = runSubmitting
	s.runID = requestID
	s.activeRequest = requestID
	s.activePrompt = text
	s.chat = append(s.chat,
		chatMessage{
			role: chatUser,
			blocks: []chatBlock{{
				kind: chatBlockText,
				text: text,
			}},
			status: "sent",
		},
		chatMessage{role: chatAssistant, status: "Waiting for Pi…"},
	)
}

func (s *appState) stopPrompt() {
	if s.pi == nil || (s.run != runSubmitting && s.run != runStreaming) {
		return
	}
	client := s.pi
	s.run = runStopping
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		note, err := client.Stop(ctx)
		s.postUI(func() {
			if err != nil {
				s.piError = err.Error()
				s.notice = "Stop was sent, but " + err.Error()
			} else if note != "" {
				s.notice = "Stop requested. " + note + "."
			} else {
				s.notice = "Stop requested. Queue cleared and abort sent."
			}
		})
	}()
}

func (s *appState) handlePiEvent(event piEvent, epoch uint64) {
	if epoch != s.piEpoch {
		return
	}
	if event.Source != nil && s.pi != nil && event.Source != s.pi {
		return
	}
	switch event.Kind {
	case piEventResponse:
		if event.Command != "prompt" || event.RequestID != s.activeRequest {
			return
		}
		if !event.Success {
			s.run = runFailed
			s.piError = event.Error
			s.notice = "Pi rejected the prompt: " + fallback(event.Error, "unknown error")
			s.markOpenTools(toolCallFailed)
			s.finishAssistant(fallback(event.Error, "Prompt rejected."), "failed")
			return
		}
		if event.Disposition == "handled" {
			s.run = runCompleted
			s.finishAssistant("Pi handled this as an extension command.", "handled")
			return
		}
		s.run = runStreaming
	case piEventRunStarted:
		s.run = runStreaming
	case piEventText:
		s.run = runStreaming
		s.setAssistantText(event.Text, "Streaming")
	case piEventMessage:
		s.run = runStreaming
		if event.Text != "" {
			s.setAssistantText(event.Text, "Received")
		}
		if event.Error != "" {
			s.piError = event.Error
		}
	case piEventToolStart:
		s.run = runStreaming
		s.startToolCall(event)
	case piEventToolUpdate:
		s.run = runStreaming
		s.updateToolCall(event)
	case piEventToolEnd:
		s.run = runStreaming
		s.endToolCall(event)
	case piEventSettled:
		s.activeRequest = ""
		s.activePrompt = ""
		if event.Aborted || s.run == runStopping {
			s.run = runStopped
			s.markOpenTools(toolCallCancelled)
			s.ensureAssistantFallback("Pi stopped before adding response text.")
			s.setAssistantStatus("Stopped")
		} else {
			s.run = runCompleted
			s.ensureAssistantFallback("Pi completed without assistant text.")
			s.setAssistantStatus("Complete")
			if s.notice == "" {
				s.notice = "Pi finished and is ready for another message."
			}
		}
	case piEventParseError:
		s.piError = event.Error
		s.notice = "Pi protocol warning: " + event.Error
	case piEventProcessExit:
		s.pi = nil
		s.piConnecting = false
		s.piConn = piDisconnected
		s.piError = fallback(event.StderrTail, event.Error)
		if s.run == runSubmitting || s.run == runStreaming || s.run == runStopping {
			s.run = runInterrupted
			s.markOpenTools(toolCallInterrupted)
			s.ensureAssistantFallback("Pi exited before adding response text.")
			s.setAssistantStatus("Interrupted")
			s.notice = "Pi process exited before the run completed."
		} else if s.closed.Load() {
			return
		} else {
			s.notice = "Pi CLI disconnected."
		}
	}
}

func (s *appState) setAssistantText(text, status string) {
	message := s.currentAssistant()
	message.status = status
	if strings.TrimSpace(text) == "" {
		return
	}
	if len(message.blocks) > 0 {
		last := &message.blocks[len(message.blocks)-1]
		if last.kind == chatBlockText {
			last.text = text
			return
		}
	}
	message.blocks = append(message.blocks, chatBlock{kind: chatBlockText, text: text})
}

func (s *appState) setAssistantStatus(status string) {
	if len(s.chat) > 0 && s.chat[len(s.chat)-1].role == chatAssistant {
		s.chat[len(s.chat)-1].status = status
	}
}

func (s *appState) ensureAssistantFallback(text string) {
	message := s.currentAssistant()
	for i := len(message.blocks) - 1; i >= 0; i-- {
		block := &message.blocks[i]
		if block.kind == chatBlockText && strings.TrimSpace(block.text) != "" {
			return
		}
	}
	if strings.TrimSpace(text) == "" {
		return
	}
	message.blocks = append(message.blocks, chatBlock{kind: chatBlockText, text: text})
}

func (s *appState) finishAssistant(text, status string) {
	s.setAssistantText(text, status)
	s.activeRequest = ""
	s.activePrompt = ""
}

func (s *appState) currentAssistant() *chatMessage {
	for i := len(s.chat) - 1; i >= 0; i-- {
		if s.chat[i].role == chatAssistant {
			return &s.chat[i]
		}
	}
	s.chat = append(s.chat, chatMessage{role: chatAssistant})
	return &s.chat[len(s.chat)-1]
}

func (s *appState) startToolCall(event piEvent) {
	message := s.currentAssistant()
	id := event.ToolCallID
	for i := range message.blocks {
		block := &message.blocks[i]
		if block.kind != chatBlockTool || block.tool.id != id {
			continue
		}
		block.tool.name = fallback(event.ToolName, block.tool.name)
		block.tool.arguments = formatToolArguments(event.ToolArgs)
		block.tool.status = toolCallRunning
		block.tool.expanded = true
		return
	}
	message.blocks = append(message.blocks, chatBlock{
		kind: chatBlockTool,
		tool: toolCallView{
			id:        id,
			name:      fallback(event.ToolName, "tool"),
			arguments: formatToolArguments(event.ToolArgs),
			status:    toolCallRunning,
			expanded:  true,
		},
	})
}

func (s *appState) updateToolCall(event piEvent) {
	message := s.currentAssistant()
	for i := range message.blocks {
		block := &message.blocks[i]
		if block.kind != chatBlockTool || block.tool.id != event.ToolCallID {
			continue
		}
		if event.ToolName != "" {
			block.tool.name = event.ToolName
		}
		if len(event.ToolArgs) > 0 {
			block.tool.arguments = formatToolArguments(event.ToolArgs)
		}
		if len(event.ToolPartial) > 0 {
			block.tool.partial = formatToolResult(event.ToolPartial)
		}
		block.tool.status = toolCallRunning
		return
	}
	s.startToolCall(event)
	message = s.currentAssistant()
	if tool, _ := s.findToolBlock(message, event.ToolCallID); tool != nil {
		tool.partial = formatToolResult(event.ToolPartial)
	}
}

func (s *appState) endToolCall(event piEvent) {
	message := s.currentAssistant()
	tool, _ := s.findToolBlock(message, event.ToolCallID)
	if tool == nil {
		message.blocks = append(message.blocks, chatBlock{
			kind: chatBlockTool,
			tool: toolCallView{
				id:       event.ToolCallID,
				name:     fallback(event.ToolName, "tool"),
				status:   toolCallCompleted,
				expanded: false,
			},
		})
		tool, _ = s.findToolBlock(message, event.ToolCallID)
	}
	if event.ToolName != "" {
		tool.name = event.ToolName
	}
	tool.result = formatToolResult(event.ToolResult)
	tool.durationMS = event.ToolDurationMS
	tool.status = toolCallCompleted
	if event.ToolIsError {
		tool.status = toolCallFailed
	}
	tool.expanded = false
}

func (s *appState) markOpenTools(status toolCallStatus) {
	for messageIndex := range s.chat {
		message := &s.chat[messageIndex]
		if message.role != chatAssistant {
			continue
		}
		for blockIndex := range message.blocks {
			block := &message.blocks[blockIndex]
			if block.kind == chatBlockTool && block.tool.status == toolCallRunning {
				block.tool.status = status
				block.tool.expanded = false
			}
		}
	}
}

func (s *appState) latestToolCall() *toolCallView {
	var mostRecent *toolCallView
	for messageIndex := len(s.chat) - 1; messageIndex >= 0; messageIndex-- {
		message := &s.chat[messageIndex]
		if message.role != chatAssistant {
			continue
		}
		for blockIndex := len(message.blocks) - 1; blockIndex >= 0; blockIndex-- {
			block := &message.blocks[blockIndex]
			if block.kind != chatBlockTool {
				continue
			}
			if block.tool.status == toolCallRunning {
				return &block.tool
			}
			if mostRecent == nil {
				mostRecent = &block.tool
			}
		}
	}
	return mostRecent
}

func (s *appState) findToolBlock(message *chatMessage, id string) (*toolCallView, int) {
	for blockIndex := range message.blocks {
		block := &message.blocks[blockIndex]
		if block.kind == chatBlockTool && block.tool.id == id {
			return &block.tool, blockIndex
		}
	}
	return nil, -1
}

func (s *appState) currentRunTaskDetail() string {
	if s.run != runSubmitting && s.run != runStreaming && s.run != runStopping {
		return s.runDetail()
	}
	tool := s.latestToolCall()
	if tool == nil {
		return s.runDetail()
	}
	detail := tool.name + " · " + toolCallStatusLabel(tool.status)
	if tool.durationMS > 0 {
		detail += " in " + formatToolDuration(tool.durationMS)
	}
	return detail
}

func formatToolArguments(raw json.RawMessage) string {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return truncateToolText(oneLine(string(raw)), 240)
	}
	if text, ok := value.(string); ok {
		return truncateToolText(text, 1200)
	}
	if object, ok := value.(map[string]any); ok {
		for _, key := range []string{"command", "path", "filePath", "pattern", "query", "url", "prompt", "message", "description"} {
			if text, ok := object[key].(string); ok && strings.TrimSpace(text) != "" {
				return truncateToolText(text, 1200)
			}
		}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return truncateToolText(oneLine(string(raw)), 1200)
	}
	return truncateToolText(string(data), 1200)
}

func formatToolResult(raw json.RawMessage) string {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return ""
	}
	text, ok := piContentText(raw)
	if ok {
		return truncateToolText(text, 1200)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err == nil {
		if text, ok := value.(string); ok {
			return truncateToolText(text, 1200)
		}
		if data, err := json.Marshal(value); err == nil {
			return truncateToolText(string(data), 1200)
		}
	}
	return truncateToolText(oneLine(string(raw)), 1200)
}

func piContentText(raw json.RawMessage) (string, bool) {
	var payload struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || len(payload.Content) == 0 {
		return "", false
	}
	parts := make([]string, 0, len(payload.Content))
	for _, blockRaw := range payload.Content {
		var text string
		if json.Unmarshal(blockRaw, &text) == nil {
			if text != "" {
				parts = append(parts, text)
			}
			continue
		}
		var block struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			MimeType string `json:"mimeType"`
		}
		if err := json.Unmarshal(blockRaw, &block); err != nil {
			continue
		}
		switch block.Type {
		case "text":
			if block.Text != "" {
				parts = append(parts, block.Text)
			}
		case "image":
			parts = append(parts, "["+fallback(block.MimeType, "image")+"]")
		default:
			data, _ := json.Marshal(block)
			if len(data) > 2 {
				parts = append(parts, string(data))
			}
		}
	}
	return strings.Join(parts, "\n\n"), len(parts) > 0
}

func truncateToolText(text string, limit int) string {
	text = strings.Trim(text, "\r\n")
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "\n… output truncated"
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func toolCallStatusLabel(status toolCallStatus) string {
	switch status {
	case toolCallRunning:
		return "Running"
	case toolCallCompleted:
		return "Completed"
	case toolCallFailed:
		return "Failed"
	case toolCallCancelled:
		return "Cancelled"
	case toolCallInterrupted:
		return "Interrupted"
	default:
		return "Unknown"
	}
}

func formatToolDuration(durationMS int64) string {
	if durationMS < 1000 {
		return fmt.Sprintf("%d ms", durationMS)
	}
	return fmt.Sprintf("%.1f s", float64(durationMS)/1000)
}

func nonEmpty(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, value)
		}
	}
	return result
}

func (s *appState) piPathLabel() string {
	if s.piPath == "" {
		return "Not resolved"
	}
	if strings.TrimSpace(os.Getenv("BYOP_PI_PATH")) == "" {
		return "pi (resolved from PATH)"
	}
	return s.piPath
}

func (s *appState) DebugSummary() string {
	return fmt.Sprintf("%s:%s:nav=%t:tasks=%t:pi=%s:run=%s", appShort, s.page, !s.navCollapsed, s.tasksOpen, s.piConn, s.run)
}
