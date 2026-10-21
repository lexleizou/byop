package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	appTitle = "Build You Own Pi"
	appShort = "BYOP"
)

var setMygoThemeSource = mygo.Theme.SetSource

func main() {
	state := newAppState()
	mygo.Theme.SetSource(themeSource(state.themeMode))
	if err := mygo.Protocol.Handle(pluginWebViewScheme, newPluginWebViewSchemeHandler(state)); err != nil {
		log.Fatal(err)
	}
	mygo.BindAs("BYOPWebView", &pluginWebViewBridge{state: state})

	mygo.App.WhenReady(func() {
		state.syncPluginWebViewTheme(currentAppearanceDark())
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
		state.setAppIcon(win)
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
	pageAutomations pageID = "automations"
	pageSettings    pageID = "settings"
	pagePluginView  pageID = "plugin-view"
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
	role      chatRole
	status    string
	blocks    []chatBlock
	startedAt time.Time
	endedAt   time.Time
}

type appState struct {
	page               pageID
	themeMode          themeMode
	navCollapsed       bool
	tasksOpen          bool
	configDir          string
	preferences        byopPreferences
	preferencesStore   *byopPreferencesStore
	preferencesWarning string
	savedWorkspaceMiss bool
	legacySources      legacyMigrationSources
	legacyScan         legacyScan
	legacyScanned      bool
	legacyScanning     bool
	legacyImport       legacyImportRecord
	legacyImportSet    bool
	notice             string
	composerDraft      string
	pluginSearch       string
	pluginRegistry     pluginRegistry
	pluginRegistryMu   sync.RWMutex
	plugins            *pluginHost
	pluginSettingForms map[string]*pluginSettingForm
	pluginTasks        map[string]pluginTaskUpdate
	pluginViewPluginID string
	pluginViewID       string
	pluginViewKind     pluginViewKind
	pluginViewModels   map[string]pluginViewModel
	pluginViewLoading  bool
	pluginViewError    string
	pluginViewEpoch    uint64
	pluginWebViews     map[string]*pluginWebViewState
	pluginWebViewsMu   sync.RWMutex
	pluginWebViewDark  bool
	pluginBridgeSlots  map[string]chan struct{}
	pluginBridgeMu     sync.Mutex
	automations        *automationStore

	window                 *mygo.Window
	closed                 atomic.Bool
	pi                     *piClient
	piConn                 piConnectionState
	piPath                 string
	piCwd                  string
	piVersion              string
	piSessionID            string
	piModel                string
	piModelProvider        string
	piModelID              string
	piModelName            string
	piThinkingLevel        string
	piThinkingLevels       []string
	piModels               []piModel
	piSessionFile          string
	piSessions             []piSessionSummary
	piSessionsExpanded     bool
	piSessionsRefresh      atomic.Uint64
	piSessionsTimer        *time.Timer
	piSessionBusy          bool
	sessionGen             uint64
	thinkingCommitInFlight bool
	thinkingCommitLevel    string
	thinkingCommitGen      uint64
	thinkingCommitSeq      uint64
	piError                string
	piConnecting           bool
	piEpoch                uint64
	run                    runState
	runID                  string
	activePrompt           string
	activeRequest          string
	chat                   []chatMessage
	chatView               chatPresentation

	queueSteeringCount int
	queueFollowUpCount int
	compacting         bool
	compactionReason   string
	compactionResult   string
	compactionError    string
	retrying           bool
	retryAttempt       int
	retryMax           int
	retryDelayMS       int64
	retryStartedAt     time.Time
	retryError         string
	extensionError     string
	extensionPath      string
	extensionEvent     string
	agentWillRetry     bool
}

func newAppState() *appState {
	configDir, configErr := pluginConfigDir()
	if configErr != nil {
		configDir = filepath.Join(os.TempDir(), "byop-config")
	}
	preferencesStore := newByopPreferencesStore(configDir)
	preferences, preferencesWarning := preferencesStore.load()
	s := &appState{
		page:               configuredPage(),
		themeMode:          themeMode(preferences.Theme),
		navCollapsed:       preferences.NavCollapsed,
		tasksOpen:          preferences.TasksOpen,
		configDir:          configDir,
		preferences:        preferences,
		preferencesStore:   preferencesStore,
		preferencesWarning: preferencesWarning,
		piConn:             piDisconnected,
		run:                runIdle,
		pluginSettingForms: make(map[string]*pluginSettingForm),
		pluginTasks:        make(map[string]pluginTaskUpdate),
		pluginViewModels:   make(map[string]pluginViewModel),
		pluginWebViews:     make(map[string]*pluginWebViewState),
		pluginBridgeSlots:  make(map[string]chan struct{}),
	}
	if mode, ok := parsePreferenceTheme(os.Getenv("BYOP_THEME")); ok {
		s.themeMode = mode
	}
	if value, ok := parsePreferenceBool(os.Getenv("BYOP_TASKS"), "open"); ok {
		s.tasksOpen = value
	}
	if value, ok := parsePreferenceBool(os.Getenv("BYOP_NAV"), "collapsed", "collapsed-true"); ok {
		s.navCollapsed = value
	}
	if saved := strings.TrimSpace(preferences.Workspace); saved != "" &&
		strings.TrimSpace(os.Getenv("BYOP_WORKSPACE")) == "" {
		info, err := os.Stat(saved)
		s.savedWorkspaceMiss = err != nil || !info.IsDir()
	}
	switch strings.ToLower(os.Getenv("BYOP_DEMO_TOOLS")) {
	case "1", "true", "on":
		s.seedToolDemo()
	}
	workspace, workspaceErr := piWorkspaceCwd(preferences.Workspace)
	if workspaceErr != nil {
		workspace, _ = os.Getwd()
	}
	s.plugins = newPluginHost(pluginHostVersion, func() { s.postUI(func() {}) },
		pluginHostWorkspace(workspace),
		pluginHostTheme(currentAppearanceDark),
		pluginHostNoticeHandler(func(pluginID, pluginName, text, tone string) {
			s.postUI(func() {
				s.notice = pluginName + ": " + text
			})
		}),
		pluginHostTaskHandler(func(update pluginTaskUpdate) {
			s.postUI(func() {
				s.pluginTasks[update.PluginID+"\x00"+update.TaskID] = update
			})
		}),
		pluginHostRefreshHandler(func(pluginID, viewID string) {
			s.postUI(func() {
				if s.page == pagePluginView && s.pluginViewPluginID == pluginID &&
					s.pluginViewID == viewID && s.pluginViewKind != "" {
					s.refreshPluginView(pluginID, viewID)
				}
			})
		}),
	)
	s.setPluginRegistry(scanPluginRegistry(defaultPluginRoots()))
	s.automations = newAutomationStore(configDir, registeredSessionLoopKeys(s.pluginRegistrySnapshot()))
	s.plugins.invalidateSettingsCache()
	s.syncPluginSettingForms()
	if sources, err := defaultLegacyMigrationSources(); err == nil {
		s.legacySources = sources
	}
	if record, ok := latestLegacyImport(configDir); ok {
		s.legacyImport = record
		s.legacyImportSet = true
	}
	return s
}

func parsePreferenceTheme(value string) (themeMode, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(themeSystem), "auto":
		return themeSystem, true
	case string(themeLight):
		return themeLight, true
	case string(themeDark):
		return themeDark, true
	default:
		return themeSystem, false
	}
}

func parsePreferenceBool(value string, trueValues ...string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return false, false
	case "1", "true", "on":
		return true, true
	case "0", "false", "off", "closed", "expanded":
		return false, true
	default:
		for _, candidate := range trueValues {
			if strings.EqualFold(strings.TrimSpace(value), candidate) {
				return true, true
			}
		}
		return false, false
	}
}

func (s *appState) scanPlugins(announce bool) {
	if s.plugins != nil && s.plugins.activeCount() > 0 {
		s.notice = "Stopping active plugins before rescanning…"
		go func() {
			s.plugins.stopAll()
			s.postUI(func() { s.scanPlugins(announce) })
		}()
		return
	}
	s.setPluginRegistry(scanPluginRegistry(defaultPluginRoots()))
	s.reconcilePluginWebViews()
	s.prunePluginState()
	s.plugins.invalidateSettingsCache()
	s.syncPluginSettingForms()
	if s.page == pagePluginView && !s.pluginViewStillRegistered() {
		s.page = pagePlugins
		s.pluginViewPluginID = ""
		s.pluginViewID = ""
		s.pluginViewKind = ""
	}
	if s.automations != nil {
		s.automations.refreshSessionLoopKeys(registeredSessionLoopKeys(s.pluginRegistrySnapshot()))
	}
	if !announce {
		return
	}
	registry := s.pluginRegistrySnapshot()
	registered := registry.registeredCount()
	contributions := registry.contributionCount()
	problems := registry.problemCount()
	switch {
	case problems > 0:
		s.notice = fmt.Sprintf("Plugin scan found %d registered plugin(s), %d contribution(s), and %d issue(s).",
			registered, contributions, problems)
	case registered == 0:
		s.notice = "Plugin scan completed. No plugin manifests were found."
	default:
		s.notice = fmt.Sprintf("Plugin scan found %d registered plugin(s) and %d contribution(s).",
			registered, contributions)
	}
}

func (s *appState) prunePluginState() {
	registered := make(map[string]struct{})
	for _, entry := range s.pluginRegistrySnapshot().Entries {
		if entry.Status == pluginRegistered {
			registered[entry.ID] = struct{}{}
		}
	}
	for key := range s.pluginTasks {
		pluginID, _, _ := strings.Cut(key, "\x00")
		if _, ok := registered[pluginID]; !ok {
			delete(s.pluginTasks, key)
		}
	}
	for key := range s.pluginViewModels {
		pluginID, _, _ := strings.Cut(key, "\x00")
		if _, ok := registered[pluginID]; !ok {
			delete(s.pluginViewModels, key)
		}
	}
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
			role:      chatAssistant,
			status:    "Streaming",
			startedAt: time.Now().Add(-14 * time.Second),
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
	case pageChat, pagePlugins, pageAutomations, pageSettings:
		return true
	default:
		return false
	}
}

func configuredPage() pageID {
	p := pageID(strings.ToLower(strings.TrimSpace(os.Getenv("BYOP_PAGE"))))
	if validPage(p) {
		return p
	}
	return pageChat
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
	s.preferences.Theme = string(mode)
	s.applyTheme(mode)
	if err := s.persistPreferences(); err != nil {
		s.showNotice("Theme changed, but preferences could not be saved.")
	}
}

func (s *appState) applyTheme(mode themeMode) {
	s.themeMode = mode
	setMygoThemeSource(themeSource(mode))
	s.syncPluginWebViewTheme(currentAppearanceDark())
}

func (s *appState) setNavCollapsed(collapsed bool) {
	s.preferences.NavCollapsed = collapsed
	s.navCollapsed = collapsed
	if err := s.persistPreferences(); err != nil {
		s.showNotice("Navigation changed, but preferences could not be saved.")
	}
}

func (s *appState) setTasksOpen(open bool) {
	s.preferences.TasksOpen = open
	s.tasksOpen = open
	if err := s.persistPreferences(); err != nil {
		s.showNotice("Task panel changed, but preferences could not be saved.")
	}
}

func (s *appState) persistPreferences() error {
	if s.preferencesStore == nil {
		return nil
	}
	if err := s.preferencesStore.save(s.preferences); err != nil {
		return err
	}
	s.preferencesWarning = s.preferencesStore.warningSnapshot()
	return nil
}

func (s *appState) setPage(p pageID) {
	s.page = p
	s.notice = ""
}

func (s *appState) showNotice(text string) {
	s.notice = text
}

func (s *appState) setAppIcon(win *mygo.Window) {
	if err := mygo.App.Dock.SetIcon(byopLogoPNG); err != nil {
		log.Printf("set Dock icon: %v", err)
	}
	if win != nil {
		if err := win.SetIcon(byopLogoPNG); err != nil {
			log.Printf("set window icon: %v", err)
		}
	}
}

func (s *appState) applyPiSessionState(state *piSessionState) {
	if state == nil {
		return
	}
	s.piSessionID = state.SessionID
	if state.SessionFile == "" {
		s.piSessionFile = ""
	} else {
		s.piSessionFile = canonicalPiPath(state.SessionFile)
	}
	s.piThinkingLevel = state.ThinkingLevel
	s.piModelProvider = state.Model.Provider
	s.piModelID = state.Model.ID
	s.piModelName = state.Model.Name
	s.piModel = strings.TrimSpace(strings.Join(nonEmpty(state.Model.Provider, state.Model.Name), " / "))
	if state.SessionID != "" {
		s.recordLastSession(state.SessionID)
	}
}

func (s *appState) refreshPiSessionState() {
	if s.pi == nil || s.piConn != piConnected {
		return
	}
	client := s.pi
	epoch := s.piEpoch
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		state, err := client.GetState(ctx)
		if err != nil {
			return
		}
		s.postUI(func() {
			if epoch != s.piEpoch || s.pi != client {
				return
			}
			if s.sessionGen != 0 && (s.piSessionBusy || s.thinkingCommitInFlight) {
				return
			}
			s.applyPiSessionState(state)
		})
	}()
}

func (s *appState) refreshPiSessions() {
	if piEphemeralSessions() {
		s.piSessionsRefresh.Add(1)
		if s.piSessionsTimer != nil {
			s.piSessionsTimer.Stop()
			s.piSessionsTimer = nil
		}
		s.piSessions = nil
		s.piSessionsExpanded = false
		return
	}
	refresh := s.piSessionsRefresh.Add(1)
	if s.piSessionsTimer != nil {
		s.piSessionsTimer.Stop()
	}
	cwd, cwdErr := s.sessionWorkspaceCwd()
	s.piSessionsTimer = time.AfterFunc(250*time.Millisecond, func() {
		if s.piSessionsRefresh.Load() != refresh || s.closed.Load() || piEphemeralSessions() {
			return
		}
		var sessions []piSessionSummary
		var err error
		if cwdErr == nil {
			sessions, err = piSessionsForWorkspace(cwd)
		} else {
			err = cwdErr
		}
		s.postUI(func() {
			if refresh != s.piSessionsRefresh.Load() || piEphemeralSessions() {
				return
			}
			if err == nil {
				s.piSessions = sessions
			}
		})
	})
}

func (s *appState) schedulePiSessionsRefresh() {
	s.refreshPiSessions()
}

func (s *appState) sessionMutationAllowed() bool {
	return s.piMutationAllowed("sessions")
}

func (s *appState) piMutationAllowed(change string) bool {
	if s.piSessionBusy || s.thinkingCommitInFlight {
		s.showNotice("Wait for the current Pi session operation before changing " + change + ".")
		return false
	}
	if s.runActive() || s.compacting {
		s.showNotice("Stop the active Pi run before changing " + change + ".")
		return false
	}
	if s.pi == nil || s.piConn != piConnected {
		s.showNotice("Pi CLI is not connected. Reconnect from Settings.")
		return false
	}
	return true
}

func (s *appState) beginPiSessionOperation(change string) (*piClient, uint64, uint64, bool) {
	if !s.piMutationAllowed(change) {
		return nil, 0, 0, false
	}
	client := s.pi
	epoch := s.piEpoch
	s.sessionGen++
	gen := s.sessionGen
	s.piSessionBusy = true
	return client, epoch, gen, true
}

func (s *appState) finishPiSessionOperation(client *piClient, epoch, gen uint64) bool {
	if gen != s.sessionGen {
		return false
	}
	s.piSessionBusy = false
	return epoch == s.piEpoch && s.pi == client && !s.runActive()
}

func (s *appState) beginThinkingCommit(level string) (*piClient, uint64, uint64, bool) {
	if !s.piMutationAllowed("thinking level") {
		return nil, 0, 0, false
	}
	client := s.pi
	epoch := s.piEpoch
	s.thinkingCommitGen++
	gen := s.thinkingCommitGen
	s.thinkingCommitInFlight = true
	s.thinkingCommitLevel = level
	return client, epoch, gen, true
}

func (s *appState) finishThinkingCommit(client *piClient, epoch, gen uint64) bool {
	if gen != s.thinkingCommitGen {
		return false
	}
	s.thinkingCommitInFlight = false
	s.thinkingCommitLevel = ""
	return epoch == s.piEpoch && s.pi == client && !s.runActive()
}

func (s *appState) cancelThinkingCommit() {
	s.thinkingCommitGen++
	s.thinkingCommitInFlight = false
	s.thinkingCommitLevel = ""
}

func (s *appState) newPiSession() {
	s.cancelThinkingPending()
	client, epoch, gen, ok := s.beginPiSessionOperation("sessions")
	if !ok {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cancelled, err := client.NewSession(ctx)
		if err != nil {
			s.postUI(func() {
				if s.finishPiSessionOperation(client, epoch, gen) {
					s.showNotice("Could not create a Pi session: " + uiErrorText(err, "unknown error"))
				}
			})
			return
		}
		if cancelled {
			s.postUI(func() {
				if s.finishPiSessionOperation(client, epoch, gen) {
					s.showNotice("Pi canceled the new session.")
				}
			})
			return
		}
		stateCtx, stateCancel := context.WithTimeout(context.Background(), 5*time.Second)
		state, stateErr := client.GetState(stateCtx)
		stateCancel()
		if stateErr != nil {
			s.postUI(func() {
				if s.finishPiSessionOperation(client, epoch, gen) {
					s.showNotice("New Pi session was created, but its state could not be confirmed: " +
						uiErrorText(stateErr, "unknown error"))
				}
			})
			return
		}
		s.loadPiSession(client, epoch, gen, state.SessionFile, state.SessionID, "New Pi session created.")
	}()
}

func (s *appState) switchPiSession(summary piSessionSummary) {
	if piEphemeralSessions() {
		s.showNotice("Session switching is unavailable while BYOP_PI_EPHEMERAL is enabled.")
		return
	}
	s.cancelThinkingPending()
	client, epoch, gen, ok := s.beginPiSessionOperation("sessions")
	if !ok {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cancelled, err := client.SwitchSession(ctx, summary.Path)
		if err != nil {
			s.postUI(func() {
				if s.finishPiSessionOperation(client, epoch, gen) {
					s.showNotice("Could not switch Pi session: " + uiErrorText(err, "unknown error"))
				}
			})
			return
		}
		if cancelled {
			s.postUI(func() {
				if s.finishPiSessionOperation(client, epoch, gen) {
					s.showNotice("Pi canceled the session switch.")
				}
			})
			return
		}
		s.loadPiSession(client, epoch, gen, summary.Path, summary.ID, "Switched Pi session.")
	}()
}

func (s *appState) loadPiSession(client *piClient, epoch, gen uint64, expectedPath, expectedID, notice string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		rawMessages, err := client.GetMessages(ctx)
		if err != nil {
			s.postUI(func() {
				if s.finishPiSessionOperation(client, epoch, gen) {
					s.showNotice("Session changed, but its transcript could not be restored: " +
						uiErrorText(err, "unknown error"))
				}
			})
			return
		}
		chat, err := rebuildChatFromPiMessages(rawMessages)
		if err != nil {
			s.postUI(func() {
				if s.finishPiSessionOperation(client, epoch, gen) {
					s.showNotice("Session transcript could not be read: " + uiErrorText(err, "unknown error"))
				}
			})
			return
		}
		stateCtx, stateCancel := context.WithTimeout(context.Background(), 5*time.Second)
		state, stateErr := client.GetState(stateCtx)
		stateCancel()
		modelsCtx, modelsCancel := context.WithTimeout(context.Background(), 10*time.Second)
		models, modelsErr := client.GetAvailableModels(modelsCtx)
		modelsCancel()
		levelsCtx, levelsCancel := context.WithTimeout(context.Background(), 5*time.Second)
		levels, levelsErr := client.GetAvailableThinkingLevels(levelsCtx)
		levelsCancel()
		s.postUI(func() {
			if !s.finishPiSessionOperation(client, epoch, gen) {
				return
			}
			s.cancelThinkingPending()
			s.chatView = chatPresentation{}
			s.chat = chat
			s.run = runIdle
			s.activeRequest = ""
			s.activePrompt = ""
			if stateErr == nil {
				s.applyPiSessionState(state)
			}
			if modelsErr == nil {
				s.piModels = models
			}
			if levelsErr == nil {
				s.piThinkingLevels = levels
			}
			s.piError = ""
			s.notice = notice
			if stateErr != nil {
				s.notice = notice + " Pi state could not be confirmed: " +
					uiErrorText(stateErr, "unknown error")
			} else if (strings.TrimSpace(expectedPath) != "" || strings.TrimSpace(expectedID) != "") &&
				!samePiSession(canonicalPiPathIfPresent(expectedPath), expectedID, s.piSessionFile, state.SessionID) {
				s.notice = notice + " Pi reported a different session."
			}
			s.page = pageChat
			s.refreshPiSessions()
		})
	}()
}

func (s *appState) recordLastSession(sessionID string) {
	cwd, err := s.sessionWorkspaceCwd()
	if err != nil {
		return
	}
	cwd = canonicalPiPath(cwd)
	sessionID = strings.TrimSpace(sessionID)
	if cwd == "" || sessionID == "" {
		return
	}
	if s.preferences.LastSessionByWorkspace == nil {
		s.preferences.LastSessionByWorkspace = make(map[string]string)
	}
	if s.preferences.LastSessionByWorkspace[cwd] == sessionID {
		return
	}
	s.preferences.LastSessionByWorkspace[cwd] = sessionID
	if s.preferencesWarning != "" {
		return
	}
	if err := s.persistPreferences(); err != nil {
		s.showNotice("Pi session changed, but its preference could not be saved.")
	}
}

func (s *appState) sessionWorkspaceCwd() (string, error) {
	if s.piCwd != "" {
		return canonicalPiPath(s.piCwd), nil
	}
	return piWorkspaceCwd(s.preferences.Workspace)
}

func samePiSession(expectedPath, expectedID, actualPath, actualID string) bool {
	if expectedPath != "" && actualPath != "" &&
		filepath.Clean(expectedPath) == filepath.Clean(actualPath) {
		return true
	}
	return strings.TrimSpace(expectedID) != "" && expectedID == actualID
}

func canonicalPiPathIfPresent(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return canonicalPiPath(value)
}

func (s *appState) selectModel(provider, id string) {
	s.cancelThinkingPending()
	client, epoch, gen, ok := s.beginPiSessionOperation("model")
	if !ok {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		model, err := client.SetModel(ctx, provider, id)
		if err != nil {
			s.postUI(func() {
				if s.finishPiSessionOperation(client, epoch, gen) {
					s.showNotice("Could not change Pi model: " + uiErrorText(err, "unknown error"))
				}
			})
			return
		}
		levelsCtx, levelsCancel := context.WithTimeout(context.Background(), 5*time.Second)
		levels, levelsErr := client.GetAvailableThinkingLevels(levelsCtx)
		levelsCancel()
		stateCtx, stateCancel := context.WithTimeout(context.Background(), 5*time.Second)
		state, stateErr := client.GetState(stateCtx)
		stateCancel()
		s.postUI(func() {
			if !s.finishPiSessionOperation(client, epoch, gen) {
				return
			}
			s.piModelProvider = model.Provider
			s.piModelID = model.ID
			s.piModelName = model.Name
			s.piModel = strings.TrimSpace(strings.Join(nonEmpty(model.Provider, model.Name), " / "))
			if levelsErr == nil {
				s.piThinkingLevels = levels
			}
			if stateErr == nil {
				s.applyPiSessionState(state)
			} else if levelsErr != nil {
				s.showNotice("Model changed, but thinking levels could not be refreshed.")
				return
			}
		})
	}()
}

func (s *appState) selectThinkingLevel(level string) bool {
	client, epoch, gen, ok := s.beginThinkingCommit(level)
	if !ok {
		s.revertThinkingPending(level)
		return false
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := client.SetThinkingLevel(ctx, level)
		if err != nil {
			s.postUI(func() {
				if s.finishThinkingCommit(client, epoch, gen) {
					s.dropThinkingPending()
					s.showNotice("Could not change Pi thinking level: " +
						uiErrorText(err, "unknown error"))
				}
			})
			return
		}
		stateCtx, stateCancel := context.WithTimeout(context.Background(), 5*time.Second)
		state, stateErr := client.GetState(stateCtx)
		stateCancel()
		levelsCtx, levelsCancel := context.WithTimeout(context.Background(), 5*time.Second)
		levels, levelsErr := client.GetAvailableThinkingLevels(levelsCtx)
		levelsCancel()
		s.postUI(func() {
			if !s.finishThinkingCommit(client, epoch, gen) {
				return
			}
			s.piThinkingLevel = level
			if stateErr == nil {
				s.applyPiSessionState(state)
			}
			if levelsErr == nil {
				s.piThinkingLevels = levels
			}
			if s.chatView.thinkingPending == level {
				s.chatView.thinkingPending = ""
			}
			if s.chatView.effortPicker.Open {
				s.scheduleThinkingPendingCommit()
			} else {
				s.commitThinkingPending()
			}
		})
	}()
	return true
}

func (s *appState) activePiSession() piSessionSummary {
	for _, session := range s.piSessions {
		if s.piSessionFile != "" && session.Path != "" &&
			filepath.Clean(session.Path) == filepath.Clean(s.piSessionFile) {
			return session
		}
		if s.piSessionID != "" && session.ID == s.piSessionID {
			return session
		}
	}
	return piSessionSummary{}
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
	s.sessionGen++
	epoch := s.piEpoch
	path, err := resolvePiPath()
	if err != nil {
		s.piConn = piConnectionErr
		s.piError = uiErrorText(err, "Pi CLI could not be resolved.")
		s.notice = s.piError
		return
	}

	s.piConnecting = true
	s.piConn = piConnecting
	s.piCwd = ""
	s.piPath = path
	s.piError = ""
	s.notice = "Connecting to external Pi CLI…"
	s.refreshPiSessions()

	savedWorkspace := s.preferences.Workspace
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		version := piCLIVersion(ctx, path)
		cancel()
		cwd, err := piWorkspaceCwd(savedWorkspace)
		if err != nil {
			s.postUI(func() {
				s.piConnecting = false
				s.piConn = piConnectionErr
				s.piError = uiErrorText(err, "Pi workspace failed.")
				s.notice = "Pi workspace failed: " + s.piError
			})
			return
		}
		client, err := startPiClient(path, cwd, func(event piEvent) {
			s.postUI(func() { s.handlePiEvent(event, epoch) })
		})
		if err != nil {
			s.postUI(func() {
				s.piConnecting = false
				s.piConn = piConnectionErr
				s.piError = uiErrorText(err, "Pi connection failed.")
				s.notice = "Pi connection failed: " + s.piError
			})
			return
		}
		handshakeCtx, handshakeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		state, err := client.GetState(handshakeCtx)
		handshakeCancel()
		if err != nil {
			client.Close()
			s.postUI(func() {
				s.piConnecting = false
				s.piConn = piConnectionErr
				s.piError = uiErrorText(err, "Pi RPC handshake failed.")
				s.notice = "Pi RPC handshake failed: " + s.piError
			})
			return
		}
		modelsCtx, modelsCancel := context.WithTimeout(context.Background(), 10*time.Second)
		models, modelsErr := client.GetAvailableModels(modelsCtx)
		modelsCancel()
		levelsCtx, levelsCancel := context.WithTimeout(context.Background(), 5*time.Second)
		levels, levelsErr := client.GetAvailableThinkingLevels(levelsCtx)
		levelsCancel()
		s.postUI(func() {
			s.pi = client
			s.piCwd = client.cwd
			s.piConnecting = false
			s.piConn = piConnected
			s.piVersion = version
			s.applyPiSessionState(state)
			if modelsErr == nil {
				s.piModels = models
			}
			if levelsErr == nil {
				s.piThinkingLevels = levels
			}
			s.piError = ""
			s.resetRuntimeDiagnostics()
			if modelsErr != nil || levelsErr != nil {
				s.notice = "External Pi CLI connected; model metadata was unavailable."
			} else {
				s.notice = "External Pi CLI connected."
			}
			s.refreshPiSessions()
		})
	}()
}

func (s *appState) reconnectPi() {
	s.sessionGen++
	s.piSessionBusy = false
	s.cancelThinkingCommit()
	s.cancelThinkingPending()
	if s.piSessionsTimer != nil {
		s.piSessionsTimer.Stop()
		s.piSessionsTimer = nil
	}
	s.piSessionsRefresh.Add(1)
	if s.run == runSubmitting || s.run == runStreaming || s.run == runStopping {
		s.markOpenTools(toolCallInterrupted)
		s.setAssistantStatus("Interrupted")
	}
	s.resetRuntimeDiagnostics()
	if s.pi != nil {
		old := s.pi
		s.pi = nil
		old.Close()
	}
	s.piConn = piDisconnected
	s.piCwd = ""
	s.piSessionFile = ""
	s.piSessionID = ""
	s.piSessions = nil
	s.piSessionsExpanded = false
	s.run = runIdle
	s.piConnecting = false
	s.piModel = ""
	s.piModelProvider = ""
	s.piModelID = ""
	s.piModelName = ""
	s.piThinkingLevel = ""
	s.piThinkingLevels = nil
	s.piModels = nil
	s.connectPi()
}

func (s *appState) shutdownPi() {
	s.closed.Store(true)
	s.sessionGen++
	s.piSessionBusy = false
	s.cancelThinkingCommit()
	s.cancelThinkingPending()
	if s.piSessionsTimer != nil {
		s.piSessionsTimer.Stop()
		s.piSessionsTimer = nil
	}
	s.piSessionsRefresh.Add(1)
	s.destroyAllPluginWebViews()
	if s.pi != nil {
		s.pi.Close()
		s.pi = nil
	}
	if s.plugins != nil {
		s.plugins.stopAll()
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
	if s.piSessionBusy {
		s.showNotice("Wait for the current Pi session operation before sending a message.")
		return
	}
	if s.thinkingCommitInFlight {
		s.showNotice("Wait for the current thinking level update before sending a message.")
		return
	}
	if s.run == runSubmitting || s.run == runStreaming || s.run == runStopping {
		s.showNotice("Pi is already working. Stop the current run before sending another message.")
		return
	}

	s.cancelThinkingPending()
	s.sessionGen++
	gen := s.sessionGen
	s.piSessionBusy = true
	requestID, err := s.pi.Prompt(text)
	if err != nil {
		if gen == s.sessionGen {
			s.piSessionBusy = false
		}
		s.run = runFailed
		s.piError = uiErrorText(err, "Pi is unavailable.")
		s.showNotice("Could not send to Pi: " + s.piError)
		return
	}
	s.resetRuntimeDiagnostics()
	s.composerDraft = ""
	s.notice = ""
	s.piError = ""
	s.run = runSubmitting
	s.runID = requestID
	s.activeRequest = requestID
	s.activePrompt = text
	s.piSessionBusy = false
	s.chat = append(s.chat,
		chatMessage{
			role: chatUser,
			blocks: []chatBlock{{
				kind: chatBlockText,
				text: text,
			}},
			status: "sent",
		},
		chatMessage{role: chatAssistant, status: "Waiting for Pi…", startedAt: time.Now()},
	)
}

func (s *appState) resetRuntimeDiagnostics() {
	s.queueSteeringCount = 0
	s.queueFollowUpCount = 0
	s.compacting = false
	s.compactionReason = ""
	s.compactionResult = ""
	s.compactionError = ""
	s.retrying = false
	s.retryAttempt = 0
	s.retryMax = 0
	s.retryDelayMS = 0
	s.retryStartedAt = time.Time{}
	s.retryError = ""
	s.extensionError = ""
	s.extensionPath = ""
	s.extensionEvent = ""
	s.agentWillRetry = false
}

func (s *appState) stopPrompt() {
	if s.pi == nil || (s.run != runSubmitting && s.run != runStreaming) {
		return
	}
	client := s.pi
	abortRetry := s.retrying
	s.run = runStopping
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		note, err := client.Stop(ctx, abortRetry)
		s.postUI(func() {
			if err != nil {
				s.piError = uiErrorText(err, "Pi stop failed.")
				s.notice = "Stop was sent, but " + s.piError
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
	if event.Source != nil && event.Source != s.pi {
		return
	}
	switch event.Kind {
	case piEventResponse:
		if event.Command != "prompt" || event.RequestID != s.activeRequest {
			return
		}
		if !event.Success {
			s.run = runFailed
			s.piError = uiErrorFromText(event.Error, "Pi rejected the prompt.")
			s.notice = "Pi rejected the prompt: " + s.piError
			s.markOpenTools(toolCallFailed)
			s.finishAssistant(s.piError, "failed")
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
			s.piError = uiErrorFromText(event.Error, "Pi message failed.")
		}
	case piEventQueueUpdate:
		s.queueSteeringCount = len(event.QueueSteering)
		s.queueFollowUpCount = len(event.QueueFollowUp)
	case piEventCompactStart:
		s.compacting = true
		s.compactionReason = event.CompactReason
		s.compactionResult = ""
		s.compactionError = ""
		if s.runActive() {
			s.run = runStreaming
		}
	case piEventCompactEnd:
		s.compacting = false
		s.compactionReason = fallback(event.CompactReason, s.compactionReason)
		s.compactionError = event.CompactError
		if s.compactionError != "" {
			s.compactionError = uiErrorFromText(s.compactionError, "Context compaction failed.")
		}
		s.agentWillRetry = event.WillRetry
		s.compactionResult = compactionResultLabel(event)
		if event.CompactError != "" {
			s.piError = s.compactionError
		}
		if s.runActive() {
			s.run = runStreaming
		}
	case piEventRetryStart:
		s.retrying = true
		s.retryAttempt = event.RetryAttempt
		s.retryMax = event.RetryMax
		s.retryDelayMS = event.RetryDelayMS
		s.retryStartedAt = time.Now()
		s.retryError = event.RetryError
		if s.retryError != "" {
			s.retryError = uiErrorFromText(s.retryError, "Pi retry failed.")
		}
		s.piError = s.retryError
		if s.runActive() {
			s.run = runStreaming
		}
	case piEventRetryEnd:
		s.retrying = false
		s.retryAttempt = event.RetryAttempt
		if event.RetrySucceeded {
			s.retryError = ""
			s.piError = ""
		} else {
			s.retryError = fallback(event.RetryError, s.retryError)
			if s.retryError != "" {
				s.retryError = uiErrorFromText(s.retryError, "Pi retry failed.")
			}
			s.piError = s.retryError
		}
		if s.runActive() {
			s.run = runStreaming
		}
	case piEventAgentEnd:
		s.agentWillRetry = event.WillRetry
		if s.runActive() {
			s.run = runStreaming
		}
		if !event.WillRetry {
			s.schedulePiSessionsRefresh()
		}
	case piEventExtensionErr:
		s.extensionError = uiErrorFromText(event.Error, "Pi extension failed.")
		s.extensionPath = event.ExtensionPath
		s.extensionEvent = event.ExtensionEvent
		s.notice = "Pi extension error: " + s.extensionError
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
		if s.runActive() {
			s.sessionGen++
		}
		s.piSessionBusy = false
		s.activeRequest = ""
		s.activePrompt = ""
		s.queueSteeringCount = 0
		s.queueFollowUpCount = 0
		s.compacting = false
		s.retrying = false
		s.agentWillRetry = false
		if event.Aborted || s.run == runStopping {
			s.run = runStopped
			s.markOpenTools(toolCallCancelled)
			s.ensureAssistantFallback("Pi stopped before adding response text.")
			s.setAssistantStatus("Stopped")
		} else if s.piError != "" {
			s.run = runFailed
			s.markOpenTools(toolCallFailed)
			s.ensureAssistantFallback("Pi stopped after an error. See runtime details for the error.")
			s.setAssistantStatus("failed")
		} else {
			s.run = runCompleted
			s.ensureAssistantFallback("Pi completed without assistant text.")
			s.setAssistantStatus("Complete")
			if s.notice == "" {
				s.notice = "Pi finished and is ready for another message."
			}
		}
		s.refreshPiSessionState()
		s.schedulePiSessionsRefresh()
	case piEventParseError:
		s.piError = uiErrorFromText(event.Error, "Pi protocol warning.")
		s.notice = "Pi protocol warning: " + s.piError
	case piEventProcessExit:
		s.sessionGen++
		s.piSessionBusy = false
		s.cancelThinkingCommit()
		s.cancelThinkingPending()
		s.pi = nil
		s.piCwd = ""
		s.piConnecting = false
		s.piConn = piDisconnected
		s.piError = uiErrorFromText(fallback(event.StderrTail, event.Error), "Pi CLI disconnected.")
		s.piSessionFile = ""
		s.piSessionID = ""
		s.piModel = ""
		s.piModelProvider = ""
		s.piModelID = ""
		s.piModelName = ""
		s.piThinkingLevel = ""
		s.piThinkingLevels = nil
		s.piModels = nil
		s.resetRuntimeDiagnostics()
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
		switch status {
		case "Complete", "Stopped", "Interrupted", "failed", "handled":
			s.markAssistantEnded()
		}
	}
}

func (s *appState) markAssistantEnded() {
	if len(s.chat) > 0 && s.chat[len(s.chat)-1].role == chatAssistant && s.chat[len(s.chat)-1].endedAt.IsZero() {
		s.chat[len(s.chat)-1].endedAt = time.Now()
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
	s.markAssistantEnded()
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
