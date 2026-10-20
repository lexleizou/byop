package main

import (
	"context"
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

type chatMessage struct {
	role   chatRole
	text   string
	status string
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
	toolActivity  string
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
	return s
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
	s.toolActivity = "Waiting for Pi to accept the prompt"
	s.chat = append(s.chat,
		chatMessage{role: chatUser, text: text, status: "sent"},
		chatMessage{role: chatAssistant, text: "", status: "Waiting for Pi…"},
	)
}

func (s *appState) stopPrompt() {
	if s.pi == nil || (s.run != runSubmitting && s.run != runStreaming) {
		return
	}
	client := s.pi
	s.run = runStopping
	s.toolActivity = "Clearing queued messages, then aborting"
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
			s.finishAssistant(fallback(event.Error, "Prompt rejected."), "failed")
			return
		}
		if event.Disposition == "handled" {
			s.run = runCompleted
			s.toolActivity = ""
			s.finishAssistant("Pi handled this as an extension command.", "handled")
			return
		}
		s.run = runStreaming
		s.toolActivity = "Waiting for the first message event"
	case piEventRunStarted:
		s.run = runStreaming
		s.toolActivity = "Preparing a response"
	case piEventText:
		s.run = runStreaming
		s.toolActivity = ""
		s.setAssistantText(event.Text, "Streaming")
	case piEventMessage:
		s.run = runStreaming
		s.toolActivity = ""
		s.setAssistantText(fallback(event.Text, "Pi completed a turn without assistant text."), "Received")
		if event.Error != "" {
			s.piError = event.Error
		}
	case piEventTool:
		s.run = runStreaming
		s.toolActivity = fallback(event.ToolName, "Pi tool activity")
	case piEventSettled:
		s.toolActivity = ""
		s.activeRequest = ""
		s.activePrompt = ""
		if event.Aborted || s.run == runStopping {
			s.run = runStopped
			s.setAssistantStatus("Stopped")
		} else {
			s.run = runCompleted
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
	if len(s.chat) == 0 || s.chat[len(s.chat)-1].role != chatAssistant {
		s.chat = append(s.chat, chatMessage{role: chatAssistant})
	}
	s.chat[len(s.chat)-1].text = text
	s.chat[len(s.chat)-1].status = status
}

func (s *appState) setAssistantStatus(status string) {
	if len(s.chat) > 0 && s.chat[len(s.chat)-1].role == chatAssistant {
		s.chat[len(s.chat)-1].status = status
	}
}

func (s *appState) finishAssistant(text, status string) {
	s.setAssistantText(text, status)
	s.activeRequest = ""
	s.activePrompt = ""
	s.toolActivity = ""
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
