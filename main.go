package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"byop/locales"
)

const (
	appTitle   = "Build You Own Pi"
	appShort   = "BYOP"
	appVersion = "0.16.2"
)

func appBuildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	for _, setting := range info.Settings {
		if setting.Key != "vcs.revision" {
			continue
		}
		revision := strings.TrimSpace(setting.Value)
		if len(revision) > 7 {
			revision = revision[:7]
		}
		if revision != "" {
			return revision
		}
	}
	return "dev"
}

var setMygoThemeSource = mygo.Theme.SetSource

func main() {
	configureFontSmoothing()
	state := newAppState()
	mygo.Theme.SetSource(state.effectiveThemeSource())
	if err := mygo.Protocol.Handle(pluginWebViewScheme, newPluginWebViewSchemeHandler(state)); err != nil {
		log.Fatal(err)
	}
	mygo.BindAs("BYOPWebView", &pluginWebViewBridge{state: state})

	mygo.App.OnLocaleChanged(func(tag string) {
		state.setEffectiveLocale(tag)
	})

	mygo.App.WhenReady(func() {
		appearanceReady.Store(true)
		state.applyUILanguage()
		state.syncPluginWebViewTheme(state.effectiveDark(), state.highContrastActive(currentAppearanceHighContrast()), currentAppearanceReduceTransparency())
		state.syncPluginWebViewLocale()
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:         locales.AppWindowTitle(),
			Width:         1180,
			Height:        760,
			MinWidth:      760,
			MinHeight:     560,
			TitleBarStyle: mygo.TitleBarHiddenInset,
			TrafficLightPosition: &mygo.Point{
				X: 18,
				Y: state.trafficLightY(),
			},
			BackgroundColor: "light-dark(#fcfbf9, #111110)",
			StateKey:        "byop-p2-pi-rpc",
			Content:         ui.View(state.view),
		})
		state.window = win
		state.setAppIcon(win)
		win.OnClosed(state.shutdownPi)
		state.scanLegacyMigration()
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

const (
	piErrorCodeNotFound         = "errorPiNotFound"
	piErrorCodeNotExecutable    = "errorPiNotExecutable"
	piErrorCodeHandshakeTimeout = "errorPiHandshakeTimeout"
	piErrorCodeExited           = "errorPiExited"
	piErrorCodeUnknown          = "errorPiUnknown"
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
	page                            pageID
	themeMode                       themeMode
	uiLanguage                      string
	effectiveLocale                 atomic.Pointer[string]
	navCollapsed                    bool
	navWidth                        float32
	jAgentNavWidth                  float32
	navResizeDragging               bool
	tasksOpen                       bool
	channelRefresh                  channelRefreshCoordinator
	configDir                       string
	preferences                     byopPreferences
	preferencesStore                *byopPreferencesStore
	preferencesWarning              string
	settingsPageState               SettingsPageState
	savedWorkspaceMiss              bool
	legacySources                   legacyMigrationSources
	legacyScan                      legacyScan
	legacyScanned                   bool
	legacyScanning                  bool
	legacyScanFailed                bool
	diagnosticsOpen                 bool
	notice                          string
	noticeActionLabel               string
	noticeAction                    func()
	navGlassPopoverOpen             bool
	navTransparencySlider           float64
	glassBlurSlider                 float64
	navTransparencyDragOwner        string
	navTransparencyDragBase         int
	glassBlurDragOwner              string
	glassBlurDragBase               int
	composerDraft                   string
	composerTableContext            *tableContextAttachment
	pluginSearch                    string
	pluginDetailsOpenID             string
	pluginRegistry                  pluginRegistry
	pluginRegistryMu                sync.RWMutex
	plugins                         *pluginHost
	pluginSettingForms              map[string]*pluginSettingForm
	pluginTasks                     map[string]pluginTaskUpdate
	pluginViewPluginID              string
	pluginViewID                    string
	pluginViewKind                  pluginViewKind
	pluginViewModels                map[string]pluginViewModel
	pluginViewLoading               bool
	pluginViewError                 string
	pluginViewEpoch                 uint64
	pluginWebViews                  map[string]*pluginWebViewState
	pluginWebViewsMu                sync.RWMutex
	pluginWebViewDark               bool
	pluginWebViewHighContrast       bool
	pluginWebViewReduceTransparency bool
	pluginWebViewPreset             appearancePreset
	pluginBridgeSlots               map[string]chan struct{}
	pluginBridgeMu                  sync.Mutex
	automations                     *automationStore

	window                  *mygo.Window
	closed                  atomic.Bool
	pi                      *piClient
	piConn                  piConnectionState
	piPath                  string
	piCwd                   string
	piVersion               string
	piSessionID             string
	piModel                 string
	piModelProvider         string
	piModelID               string
	piModelName             string
	piThinkingLevel         string
	piThinkingLevels        []string
	piThinkingLevelsKnown   bool
	piModels                []piModel
	piSessionFile           string
	piSessionName           string
	modelTuples             map[string]map[string]modelTuple
	modelStateUnconfirmed   bool
	localSessionTitle       string
	piSessions              []piSessionSummary
	piSessionsExpanded      bool
	piSessionsRefresh       atomic.Uint64
	piSessionsTimer         *time.Timer
	piSessionBusy           bool
	sessionGen              uint64
	thinkingCommitInFlight  bool
	thinkingCommitLevel     string
	thinkingCommitGen       uint64
	thinkingCommitSeq       uint64
	piError                 string
	piErrorCode             string
	piErrorDetailsOpen      bool
	piStatusPopoverOpen     bool
	piConnecting            bool
	piConnectCancel         context.CancelFunc
	piConnectGeneration     uint64
	piEpoch                 uint64
	run                     runState
	runID                   string
	activePrompt            string
	activeRequest           string
	chat                    []chatMessage
	chatView                chatPresentation
	followUpGeneration      uint64
	followUpCancel          context.CancelFunc
	followUpRunner          followUpRunner
	followUpSuggestion      string
	followUpAdoptionPending bool
	followUpAdoptionFocus   bool

	sessionRenameEditing         bool
	sessionRenameDraft           string
	sessionRenameInFlight        bool
	sessionNamingInProgress      bool
	sessionNamingCommitInFlight  bool
	sessionNamingCommitSeq       uint64
	sessionNamingCommitOwner     sessionNameCommitOwner
	sessionNamingGen             uint64
	sessionNamingCancel          context.CancelFunc
	autoNamingAttemptedBySession map[string]bool

	queueSteeringCount  int
	queueFollowUpCount  int
	queueSteering       []string
	queueFollowUp       []string
	queueClearPending   bool
	runningSendPending  bool
	runningSendPrompt   string
	runningSendDisplay  string
	runningSendContext  *tableContextAttachment
	runningSendEchoSeen bool
	pendingPiUserEcho   string
	compacting          bool
	compactionReason    string
	compactionResult    string
	compactionError     string
	retrying            bool
	retryAttempt        int
	retryMax            int
	retryDelayMS        int64
	retryStartedAt      time.Time
	retryError          string
	extensionError      string
	extensionPath       string
	extensionEvent      string
	agentWillRetry      bool
	hud                 hudState

	piSessionsOrgWorkspace string
	piSessionsGroups       sessionGroups
	archivedExpanded       bool
	draggingSessionKey     string
	draggingSessionClear   bool
	sessionUndo            *sessionUndoState
	sessionLaterTimer      *time.Timer
}

func newAppState() *appState {
	configDir, configErr := pluginConfigDir()
	if configErr != nil {
		configDir = filepath.Join(os.TempDir(), "byop-config")
	}
	preferencesStore := newByopPreferencesStore(configDir)
	preferences, preferencesWarning := preferencesStore.load()
	var s *appState
	s = &appState{
		page:                  configuredPage(),
		themeMode:             themeMode(preferences.Theme),
		uiLanguage:            normalizeUILanguage(preferences.UILanguage),
		navCollapsed:          preferences.NavCollapsed,
		navWidth:              normalizeNavExpandedWidth(preferences.NavExpandedWidth),
		jAgentNavWidth:        normalizeJAgentNavExpandedWidth(preferences.JAgentNavExpandedWidth),
		tasksOpen:             preferences.TasksOpen,
		configDir:             configDir,
		preferences:           preferences,
		preferencesStore:      preferencesStore,
		preferencesWarning:    preferencesWarning,
		piConn:                piDisconnected,
		run:                   runIdle,
		pluginSettingForms:    make(map[string]*pluginSettingForm),
		pluginTasks:           make(map[string]pluginTaskUpdate),
		pluginViewModels:      make(map[string]pluginViewModel),
		pluginWebViews:        make(map[string]*pluginWebViewState),
		pluginBridgeSlots:     make(map[string]chan struct{}),
		navTransparencySlider: float64(preferences.NavTransparency),
		glassBlurSlider:       float64(preferences.GlassBlur),
	}
	s.storeEffectiveLocale(effectiveUILanguage(preferences.UILanguage))
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
		workspace = workspaceRootFallback(workspace)
	}
	s.plugins = newPluginHost(pluginHostVersion, func() { s.postUI(func() {}) },
		pluginHostWorkspace(workspace),
		pluginHostTheme(func() bool { return s.effectiveDark() }),
		pluginHostHighContrast(func() bool {
			return s.highContrastActive(currentAppearanceHighContrast())
		}),
		pluginHostReduceTransparency(currentAppearanceReduceTransparency),
		pluginHostLocale(func() string {
			return s.locale()
		}),
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
		pluginHostOpenChatHandler(func(pluginID string, request pluginOpenChatRequest) {
			s.postUI(func() {
				s.openPluginChat(request)
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
		s.notice = text("pluginScanStopping")
		go func() {
			s.plugins.stopAll()
			s.postUI(func() { s.scanPlugins(announce) })
		}()
		return
	}
	pluginRuntimeBefore := registeredPiPluginSignature(s.pluginRegistrySnapshot())
	s.setPluginRegistry(scanPluginRegistry(defaultPluginRoots()))
	pluginRuntimeAfter := registeredPiPluginSignature(s.pluginRegistrySnapshot())
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
	if s.pluginDetailsOpenID != "" && !s.pluginEntryExists(s.pluginDetailsOpenID) {
		s.pluginDetailsOpenID = ""
	}
	if s.automations != nil {
		s.automations.refreshSessionLoopKeys(registeredSessionLoopKeys(s.pluginRegistrySnapshot()))
	}
	s.refreshPiPluginRuntimeAfterScan(pluginRuntimeBefore, pluginRuntimeAfter)
	if !announce {
		return
	}
	registry := s.pluginRegistrySnapshot()
	registered := registry.registeredCount()
	contributions := registry.contributionCount()
	problems := registry.problemCount()
	switch {
	case problems > 0:
		s.notice = textArgs("pluginScanFoundIssues", map[string]any{
			"registered":    registered,
			"contributions": contributions,
			"issues":        problems,
		})
	case registered == 0:
		s.notice = text("pluginScanFoundNone")
	default:
		s.notice = textArgs("pluginScanFound", map[string]any{
			"registered":    registered,
			"contributions": contributions,
		})
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
	previous := s.preferences.Theme
	s.preferences.Theme = string(mode)
	s.applyTheme(mode)
	if err := s.persistTheme(mode); err != nil {
		s.preferences.Theme = previous
		s.applyTheme(themeMode(previous))
		s.showNotice(text("noticePreferenceSaveTheme"))
	}
}

func (s *appState) applyTheme(mode themeMode) {
	s.themeMode = mode
	setMygoThemeSource(s.effectiveThemeSource())
	s.syncPluginWebViewTheme(s.effectiveDark(), s.highContrastActive(currentAppearanceHighContrast()), currentAppearanceReduceTransparency())
}

func (s *appState) setAppearancePreset(preset appearancePreset) {
	preset = normalizeAppearancePreset(string(preset))
	if s.currentAppearancePreset() == preset {
		return
	}
	previous := s.preferences.AppearancePreset
	s.applyAppearancePreset(preset)
	if err := s.persistAppearancePreset(preset); err != nil {
		s.preferences.AppearancePreset = previous
		s.applyAppearancePreset(normalizeAppearancePreset(previous))
		s.showNotice(text("noticePreferenceSaveAppearance"))
	}
}

func (s *appState) applyAppearancePreset(preset appearancePreset) {
	preset = normalizeAppearancePreset(string(preset))
	s.preferences.AppearancePreset = string(preset)
	setMygoThemeSource(s.effectiveThemeSource())
	s.syncPluginWebViewTheme(s.effectiveDark(), s.highContrastActive(currentAppearanceHighContrast()), currentAppearanceReduceTransparency())
	s.invalidateUI()
}

func (s *appState) setNavCollapsed(collapsed bool) {
	previousPreference := s.preferences.NavCollapsed
	previousCollapsed := s.navCollapsed
	s.preferences.NavCollapsed = collapsed
	s.navCollapsed = collapsed
	if err := s.persistNavigationCollapsed(collapsed); err != nil {
		s.preferences.NavCollapsed = previousPreference
		s.navCollapsed = previousCollapsed
		s.showNotice(text("noticePreferenceSaveNavigation"))
	}
}

func (s *appState) setTasksOpen(open bool) {
	previousOpen := s.tasksOpen
	s.tasksOpen = open
	if err := s.persistTaskPanelOpen(open); err != nil {
		s.tasksOpen = previousOpen
		s.showNotice(text("noticePreferenceSaveTaskPanel"))
	}
}

func (s *appState) persistedPreferences() byopPreferences {
	saved, _ := s.preferencesStore.load()
	persisted := cloneByopPreferences(saved)
	current := s.preferences

	if _, overridden := parsePreferenceTheme(os.Getenv("BYOP_THEME")); !overridden {
		persisted.Theme = current.Theme
	}
	if _, overridden := parsePreferenceBool(os.Getenv("BYOP_NAV"), "collapsed", "collapsed-true"); !overridden {
		persisted.NavCollapsed = current.NavCollapsed
	}
	if _, overridden := parsePreferenceBool(os.Getenv("BYOP_TASKS"), "open"); !overridden {
		persisted.TasksOpen = current.TasksOpen
	}

	persisted.NavExpandedWidth = normalizeNavExpandedWidth(current.NavExpandedWidth)
	persisted.JAgentNavExpandedWidth = normalizeJAgentNavExpandedWidth(current.JAgentNavExpandedWidth)
	persisted.AppearancePreset = string(normalizeAppearancePreset(current.AppearancePreset))
	persisted.Contrast = current.Contrast
	persisted.Motion = current.Motion
	persisted.PanelAnimationMS = current.PanelAnimationMS
	persisted.Wallpaper = current.Wallpaper
	persisted.JAgentWallpaper = normalizeJAgentWallpaper(current.JAgentWallpaper)
	persisted.NavTransparency = current.NavTransparency
	persisted.GlassBlur = current.GlassBlur
	persisted.GlassRefraction = current.GlassRefraction
	persisted.UILanguage = current.UILanguage
	persisted.FollowUpStyle = current.FollowUpStyle
	persisted.RunningSendMode = current.RunningSendMode
	persisted.Workspace = current.Workspace
	persisted.LegacyMigrationPromptDismissed = current.LegacyMigrationPromptDismissed
	persisted.LastSessionByWorkspace = cloneByopPreferences(current).LastSessionByWorkspace
	persisted.SessionOrganization = cloneByopPreferences(current).SessionOrganization
	persisted.Drafts = cloneByopPreferences(current).Drafts
	persisted.Legacy = cloneByopPreferences(current).Legacy
	return persisted
}

func (s *appState) persistTheme(mode themeMode) error {
	return s.persistPreferencePatch(func(preferences *byopPreferences) {
		preferences.Theme = string(mode)
	})
}

func (s *appState) persistAppearancePreset(preset appearancePreset) error {
	return s.persistPreferencePatch(func(preferences *byopPreferences) {
		preferences.AppearancePreset = string(normalizeAppearancePreset(string(preset)))
	})
}

func (s *appState) persistNavigationWidth() error {
	return s.persistPreferencePatch(func(preferences *byopPreferences) {
		if s.jAgentPresetActive() {
			preferences.JAgentNavExpandedWidth = normalizeJAgentNavExpandedWidth(s.jAgentNavWidth)
			return
		}
		preferences.NavExpandedWidth = normalizeNavExpandedWidth(s.navWidth)
	})
}

func (s *appState) persistNavigationCollapsed(collapsed bool) error {
	return s.persistPreferencePatch(func(preferences *byopPreferences) {
		preferences.NavCollapsed = collapsed
	})
}

func (s *appState) persistTaskPanelOpen(open bool) error {
	return s.persistPreferencePatch(func(preferences *byopPreferences) {
		preferences.TasksOpen = open
	})
}

func (s *appState) persistPreferencePatch(patch func(*byopPreferences)) error {
	if s.preferencesStore == nil {
		return nil
	}
	persisted, warning := s.preferencesStore.load()
	if warning != "" && s.preferencesWarning == "" {
		return errors.New(warning)
	}
	patch(&persisted)
	if err := s.preferencesStore.save(persisted); err != nil {
		return err
	}
	s.preferencesWarning = s.preferencesStore.warningSnapshot()
	return nil
}

func (s *appState) setContrast(mode contrastMode) {
	mode = normalizeContrastMode(string(mode))
	if normalizeContrastMode(s.preferences.Contrast) == mode {
		return
	}
	previous := s.preferences.Contrast
	s.preferences.Contrast = string(mode)
	s.syncPluginWebViewTheme(s.effectiveDark(), s.highContrastActive(currentAppearanceHighContrast()), currentAppearanceReduceTransparency())
	if s.window != nil {
		s.window.Invalidate()
	}
	if err := s.persistPreferences(); err != nil {
		s.preferences.Contrast = previous
		s.syncPluginWebViewTheme(s.effectiveDark(), s.highContrastActive(currentAppearanceHighContrast()), currentAppearanceReduceTransparency())
		if s.window != nil {
			s.window.Invalidate()
		}
		s.showNotice(text("noticePreferenceSaveContrast"))
	}
}

func (s *appState) setMotion(mode motionMode) {
	mode = normalizeMotionMode(string(mode))
	if normalizeMotionMode(s.preferences.Motion) == mode {
		return
	}
	previous := s.preferences.Motion
	s.preferences.Motion = string(mode)
	if s.window != nil {
		s.window.Invalidate()
	}
	if err := s.persistPreferences(); err != nil {
		s.preferences.Motion = previous
		if s.window != nil {
			s.window.Invalidate()
		}
		s.showNotice(text("noticePreferenceSaveMotion"))
	}
}

func (s *appState) setWallpaper(value string) {
	if s.jAgentPresetActive() {
		value = normalizeJAgentWallpaper(value)
		if s.preferences.JAgentWallpaper == value {
			return
		}
		previous := s.preferences.JAgentWallpaper
		s.preferences.JAgentWallpaper = value
		s.invalidateUI()
		if err := s.persistPreferences(); err != nil {
			s.preferences.JAgentWallpaper = previous
			s.invalidateUI()
			s.showNotice(text("noticePreferenceSaveGlass"))
		}
		return
	}
	value = normalizeWallpaper(value)
	if s.preferences.Wallpaper == value {
		return
	}
	previous := s.preferences.Wallpaper
	s.preferences.Wallpaper = value
	s.invalidateUI()
	if err := s.persistPreferences(); err != nil {
		s.preferences.Wallpaper = previous
		s.invalidateUI()
		s.showNotice(text("noticePreferenceSaveGlass"))
	}
}

func (s *appState) setGlassRefraction(value bool) {
	if s.preferences.GlassRefraction == value {
		return
	}
	previous := s.preferences.GlassRefraction
	s.preferences.GlassRefraction = value
	s.invalidateUI()
	if err := s.persistPreferences(); err != nil {
		s.preferences.GlassRefraction = previous
		s.invalidateUI()
		s.showNotice(text("noticePreferenceSaveGlass"))
	}
}

func (s *appState) invalidateUI() {
	if s.window != nil {
		s.window.Invalidate()
	}
}

func (s *appState) setRunningSendMode(mode runningSendMode) {
	mode = normalizeRunningSendMode(string(mode))
	if s.currentRunningSendMode() == mode {
		return
	}
	previous := s.preferences.RunningSendMode
	s.preferences.RunningSendMode = string(mode)
	if err := s.persistPreferences(); err != nil {
		s.preferences.RunningSendMode = previous
		s.showNotice(text("noticePreferenceSaveRunningSend"))
	}
}

func (s *appState) setUILanguage(value string) {
	value = normalizeUILanguage(value)
	if s.uiLanguage == value {
		return
	}
	previous := s.uiLanguage
	s.uiLanguage = value
	s.preferences.UILanguage = value
	s.applyUILanguage()
	if err := s.persistPreferences(); err != nil {
		s.uiLanguage = previous
		s.preferences.UILanguage = previous
		s.applyUILanguage()
		s.showNotice(text("noticePreferenceSaveLanguage"))
	}
}

func (s *appState) applyUILanguage() {
	if strings.TrimSpace(os.Getenv("MYGO_LOCALE")) != "" {
		s.setEffectiveLocale(mygo.App.Locale())
		return
	}
	tag := s.uiLanguage
	if tag == uiLanguageSystem {
		tag = ""
	}
	mygo.App.SetLocale(tag)
	s.setEffectiveLocale(mygo.App.Locale())
}

func (s *appState) setEffectiveLocale(locale string) {
	if tag := i18n.Match([]string{locale}, locales.Catalog.Languages()); tag != "" {
		s.storeEffectiveLocale(tag)
	} else {
		s.storeEffectiveLocale(effectiveUILanguage(s.uiLanguage))
	}
	if s.window != nil {
		s.window.SetTitle(locales.AppWindowTitle())
		s.window.Invalidate()
	}
	s.syncPluginWebViewLocale()
}

func (s *appState) storeEffectiveLocale(locale string) {
	if strings.TrimSpace(locale) == "" {
		locale = locales.Catalog.Source()
	}
	s.effectiveLocale.Store(&locale)
}

func (s *appState) locale() string {
	if locale := s.effectiveLocale.Load(); locale != nil && strings.TrimSpace(*locale) != "" {
		return *locale
	}
	return locales.Catalog.Source()
}

func (s *appState) persistPreferences() error {
	if s.preferencesStore == nil {
		return nil
	}
	if _, warning := s.preferencesStore.load(); warning != "" && s.preferencesWarning == "" {
		return errors.New(warning)
	}
	if err := s.preferencesStore.save(s.persistedPreferences()); err != nil {
		return err
	}
	s.preferencesWarning = s.preferencesStore.warningSnapshot()
	return nil
}

func (s *appState) setPage(p pageID) {
	s.page = p
	if p != pagePlugins {
		s.pluginDetailsOpenID = ""
	}
	s.notice = ""
	s.clearNoticeAction()
}

func (s *appState) showNotice(text string) {
	s.notice = text
	s.clearNoticeAction()
}

func (s *appState) showNoticeAction(text, actionLabel string, action func()) {
	s.notice = text
	s.noticeActionLabel = actionLabel
	s.noticeAction = action
}

func (s *appState) clearNoticeAction() {
	s.noticeActionLabel = ""
	s.noticeAction = nil
}

func (s *appState) clearPiConnectingNotice() {
	if s.notice == text("noticePiConnecting") {
		s.notice = ""
		s.clearNoticeAction()
	}
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
	s.modelStateUnconfirmed = false
	oldFile := s.piSessionFile
	oldID := s.piSessionID
	oldKey := s.piSessionKey()
	s.piSessionID = state.SessionID
	if state.SessionFile == "" {
		s.piSessionFile = ""
	} else {
		s.piSessionFile = canonicalPiPath(state.SessionFile)
	}
	if oldID != "" && state.SessionID != "" &&
		!samePiSession(oldFile, oldID, s.piSessionFile, state.SessionID) {
		s.invalidateSessionNaming()
		s.piSessionName = ""
		s.localSessionTitle = firstUserSessionTitle(s.chat)
		s.sessionRenameEditing = false
		s.sessionRenameDraft = ""
	}
	s.piSessionName = strings.TrimSpace(state.SessionName)
	s.piThinkingLevel = state.ThinkingLevel
	s.piModelProvider = state.Model.Provider
	s.piModelID = state.Model.ID
	s.piModelName = state.Model.Name
	s.piModel = strings.TrimSpace(strings.Join(nonEmpty(state.Model.Provider, state.Model.Name), " / "))
	s.applyPiHUDState(state)
	s.migrateModelTuples(oldKey, s.piSessionKey())
	if state.SessionID != "" {
		s.recordLastSession(state.SessionID)
	}
}

func (s *appState) migrateModelTuples(oldKey, newKey string) {
	oldKey = strings.TrimSpace(oldKey)
	newKey = strings.TrimSpace(newKey)
	if oldKey == "" || newKey == "" || oldKey == newKey || s.modelTuples == nil {
		return
	}
	oldTuples := s.modelTuples[oldKey]
	if len(oldTuples) == 0 {
		return
	}
	if s.modelTuples[newKey] == nil {
		s.modelTuples[newKey] = make(map[string]modelTuple, len(oldTuples))
	}
	for familyID, tuple := range oldTuples {
		if _, exists := s.modelTuples[newKey][familyID]; !exists {
			s.modelTuples[newKey][familyID] = tuple
		}
	}
	delete(s.modelTuples, oldKey)
}

func (s *appState) applyConfirmedModelState(state *piSessionState, levels []string, levelsKnown bool) {
	s.applyConfirmedModelStateWithRecord(state, levels, levelsKnown, true)
}

func (s *appState) applyConfirmedModelStateWithoutRecord(
	state *piSessionState, levels []string, levelsKnown bool,
) {
	s.applyConfirmedModelStateWithRecord(state, levels, levelsKnown, false)
}

func (s *appState) applyConfirmedModelStateWithRecord(
	state *piSessionState, levels []string, levelsKnown, record bool,
) {
	if state == nil {
		return
	}
	s.applyPiSessionState(state)
	if levelsKnown {
		s.piThinkingLevels = append([]string(nil), levels...)
		s.piThinkingLevelsKnown = true
	} else {
		s.piThinkingLevels = nil
		s.piThinkingLevelsKnown = false
	}
	if record {
		s.recordModelTuple()
	}
}

func (s *appState) applyConfirmedThinkingState(state *piSessionState, levels []string,
	levelsKnown, preserveSession bool,
) bool {
	return s.applyConfirmedThinkingStateWithRecord(state, levels, levelsKnown, preserveSession, true)
}

func (s *appState) applyConfirmedThinkingStateWithoutRecord(
	state *piSessionState, levels []string, levelsKnown, preserveSession bool,
) bool {
	return s.applyConfirmedThinkingStateWithRecord(state, levels, levelsKnown, preserveSession, false)
}

func (s *appState) applyConfirmedThinkingStateWithRecord(
	state *piSessionState, levels []string, levelsKnown, preserveSession, record bool,
) bool {
	if state == nil {
		return false
	}
	s.modelStateUnconfirmed = false
	oldPath := s.piSessionFile
	oldID := s.piSessionID
	s.piModelProvider = state.Model.Provider
	s.piModelID = state.Model.ID
	s.piModelName = state.Model.Name
	s.piModel = strings.TrimSpace(strings.Join(nonEmpty(state.Model.Provider, state.Model.Name), " / "))
	s.applyPiHUDState(state)
	if preserveSession {
		s.piSessionFile = oldPath
		s.piSessionID = oldID
	}
	s.piThinkingLevel = state.ThinkingLevel
	if levelsKnown {
		s.piThinkingLevels = append([]string(nil), levels...)
		s.piThinkingLevelsKnown = true
	} else {
		s.piThinkingLevels = nil
		s.piThinkingLevelsKnown = false
	}
	if record {
		s.recordModelTuple()
	}
	return true
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
			s.recordModelTuple()
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
		if s.sessionLaterTimer != nil {
			s.sessionLaterTimer.Stop()
			s.sessionLaterTimer = nil
		}
		s.piSessions = nil
		s.piSessionsExpanded = false
		s.piSessionsOrgWorkspace = ""
		s.piSessionsGroups = sessionGroups{}
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
				s.refreshSessionOrganization(time.Now())
				s.startSessionOrganizationTimer()
			}
		})
	})
}

func (s *appState) schedulePiSessionsRefresh() {
	s.refreshPiSessions()
}

func (s *appState) sessionMutationAllowed() bool {
	return s.piMutationAllowed(text("noticeCurrentChangeSession"))
}

func (s *appState) piMutationAllowed(change string) bool {
	if s.sessionNamingCommitInFlight || s.sessionRenameInFlight {
		s.showNotice(textArgs("noticeWaitSessionOperation", map[string]any{"change": change}))
		return false
	}
	if s.piSessionBusy || s.thinkingCommitInFlight {
		s.showNotice(textArgs("noticeWaitSessionOperation", map[string]any{"change": change}))
		return false
	}
	if s.runActive() || s.compacting {
		s.showNotice(textArgs("noticeStopBeforeChange", map[string]any{"change": change}))
		return false
	}
	if s.pi == nil || s.piConn != piConnected {
		s.showNotice(text("noticePiDisconnected"))
		return false
	}
	return true
}

func (s *appState) beginPiSessionOperation(change string) (*piClient, uint64, uint64, bool) {
	if !s.piMutationAllowed(change) {
		return nil, 0, 0, false
	}
	s.invalidateSessionNaming()
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
	if !s.piMutationAllowed(text("noticeCurrentChangeThinking")) {
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
	s.resetFollowUpSuggestion()
	s.cancelThinkingPending()
	client, epoch, gen, ok := s.beginPiSessionOperation(text("noticeCurrentChangeSession"))
	if !ok {
		return
	}
	s.resetPiHUDData()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cancelled, err := client.NewSession(ctx)
		if err != nil {
			s.postUI(func() {
				if s.finishPiSessionOperation(client, epoch, gen) {
					s.showNotice(textArgs("noticeSessionCreateFailed", map[string]any{
						"error": uiErrorText(err, text("errorUnknown")),
					}))
					s.refreshPiHUD()
				}
			})
			return
		}
		if cancelled {
			s.postUI(func() {
				if s.finishPiSessionOperation(client, epoch, gen) {
					s.showNotice(text("noticeSessionCreateCancelled"))
					s.refreshPiHUD()
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
					s.showNotice(textArgs("noticeSessionCreateUnconfirmed", map[string]any{
						"error": uiErrorText(stateErr, text("errorUnknown")),
					}))
					s.refreshPiHUD()
				}
			})
			return
		}
		s.loadPiSession(client, epoch, gen, state.SessionFile, state.SessionID,
			text("noticeSessionCreated"))
	}()
}

func (s *appState) switchPiSession(summary piSessionSummary) {
	if piEphemeralSessions() {
		s.showNotice(text("noticeSessionSwitchUnavailable"))
		return
	}
	s.resetFollowUpSuggestion()
	s.cancelThinkingPending()
	client, epoch, gen, ok := s.beginPiSessionOperation(text("noticeCurrentChangeSession"))
	if !ok {
		return
	}
	s.resetPiHUDData()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cancelled, err := client.SwitchSession(ctx, summary.Path)
		if err != nil {
			s.postUI(func() {
				if s.finishPiSessionOperation(client, epoch, gen) {
					s.showNotice(textArgs("noticeSessionSwitchFailed", map[string]any{
						"error": uiErrorText(err, text("errorUnknown")),
					}))
					s.refreshPiHUD()
				}
			})
			return
		}
		if cancelled {
			s.postUI(func() {
				if s.finishPiSessionOperation(client, epoch, gen) {
					s.showNotice(text("noticeSessionSwitchCancelled"))
					s.refreshPiHUD()
				}
			})
			return
		}
		s.loadPiSession(client, epoch, gen, summary.Path, summary.ID, text("noticeSessionSwitched"))
	}()
}

func (s *appState) loadPiSession(client *piClient, epoch, gen uint64, expectedPath, expectedID, notice string) {
	s.resetFollowUpSuggestion()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		rawMessages, err := client.GetMessages(ctx)
		if err != nil {
			s.postUI(func() {
				if s.finishPiSessionOperation(client, epoch, gen) {
					s.showNotice(textArgs("noticeSessionTranscriptRestoreFailed", map[string]any{
						"error": uiErrorText(err, text("errorUnknown")),
					}))
					s.refreshPiHUD()
				}
			})
			return
		}
		chat, err := rebuildChatFromPiMessages(rawMessages)
		if err != nil {
			s.postUI(func() {
				if s.finishPiSessionOperation(client, epoch, gen) {
					s.showNotice(textArgs("noticeSessionTranscriptReadFailed", map[string]any{
						"error": uiErrorText(err, text("errorUnknown")),
					}))
					s.refreshPiHUD()
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
			s.localSessionTitle = firstUserSessionTitle(chat)
			s.sessionRenameEditing = false
			s.sessionRenameDraft = ""
			s.run = runIdle
			s.activeRequest = ""
			s.activePrompt = ""
			s.resetFollowUpSuggestion()
			if modelsErr == nil {
				s.piModels = models
			}
			if stateErr == nil {
				s.applyConfirmedModelState(state, levels, levelsErr == nil)
			} else {
				s.piModelProvider = ""
				s.piModelID = ""
				s.piModelName = ""
				s.piModel = ""
				s.piSessionFile = ""
				s.piSessionID = ""
				s.piThinkingLevel = ""
				s.piThinkingLevels = nil
				s.piThinkingLevelsKnown = false
			}
			s.piError = ""
			s.showNotice(notice)
			sessionKey := s.piSessionKey()
			if stateErr != nil {
				s.markModelStateUnconfirmed(
					client, epoch, sessionKey, stateErr,
					textArgs("noticeSessionStateUnconfirmed", map[string]any{
						"notice": notice,
						"error":  uiErrorText(stateErr, text("errorUnknown")),
					}),
				)
			} else if levelsErr != nil {
				s.showNoticeAction(
					textArgs("noticeModelLevelsRefreshFailed", map[string]any{
						"error": uiErrorText(levelsErr, text("errorUnknown")),
					}),
					text("retry"),
					func() {
						s.retryPiThinkingLevels(client, epoch, sessionKey)
					},
				)
			} else if (strings.TrimSpace(expectedPath) != "" || strings.TrimSpace(expectedID) != "") &&
				!samePiSession(canonicalPiPathIfPresent(expectedPath), expectedID, s.piSessionFile, state.SessionID) {
				s.showNotice(textArgs("noticeSessionMismatch", map[string]any{"notice": notice}))
			}
			s.page = pageChat
			s.refreshPiHUD()
			s.refreshPiSessions()
			s.clearDueLaterSession(s.activePiSession(), time.Now())
			s.refreshActiveSessionOrganization(time.Now())
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
		s.showNotice(text("noticeSessionPreferenceSave"))
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

func (s *appState) piSessionKey() string {
	if key := strings.TrimSpace(s.piSessionFile); key != "" {
		return key
	}
	return strings.TrimSpace(s.piSessionID)
}

func (s *appState) rememberedModelTuple(provider, familyKey string) (modelTuple, bool) {
	if s.modelTuples == nil {
		return modelTuple{}, false
	}
	sessionKey := s.piSessionKey()
	if sessionKey == "" {
		return modelTuple{}, false
	}
	tuple, ok := s.modelTuples[sessionKey][piModelChoiceID(provider, familyKey)]
	return tuple, ok
}

func (s *appState) recordModelTuple() {
	family, ok := findPiModelFamily(s.piModels, s.piModelProvider, s.piModelID, s.piModelName)
	if !ok {
		family, ok = findPiModelFamily(s.piModels, s.piModelProvider, s.piModelID, "")
	}
	if !ok {
		return
	}
	sessionKey := s.piSessionKey()
	if sessionKey == "" {
		return
	}
	if s.modelTuples == nil {
		s.modelTuples = make(map[string]map[string]modelTuple)
	}
	if s.modelTuples[sessionKey] == nil {
		s.modelTuples[sessionKey] = make(map[string]modelTuple)
	}
	s.modelTuples[sessionKey][piModelChoiceID(family.Provider, family.Key)] = modelTuple{
		VariantID: s.piModelID,
		Thinking:  s.piThinkingLevel,
		Fast:      false,
	}
}

func (s *appState) recordModelTupleFor(families []piModelFamily, sessionKey, provider, id, name,
	thinking string,
) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}
	family, ok := findPiModelFamilyInFamilies(families, provider, id, name)
	if !ok {
		family, ok = findPiModelFamilyInFamilies(families, provider, id, "")
	}
	if !ok {
		return
	}
	if s.modelTuples == nil {
		s.modelTuples = make(map[string]map[string]modelTuple)
	}
	if s.modelTuples[sessionKey] == nil {
		s.modelTuples[sessionKey] = make(map[string]modelTuple)
	}
	familyID := piModelChoiceID(family.Provider, family.Key)
	previous, hasPrevious := s.modelTuples[sessionKey][familyID]
	if strings.TrimSpace(thinking) == "" && hasPrevious {
		thinking = previous.Thinking
	}
	s.modelTuples[sessionKey][familyID] = modelTuple{
		VariantID: id,
		Thinking:  thinking,
		Fast:      false,
	}
}

func (s *appState) resetModelTuples() {
	s.modelTuples = nil
}

type modelSelectionSnapshot struct {
	families      []piModelFamily
	remembered    modelTuple
	hasRemembered bool
	sessionKey    string
	provider      string
	id            string
	name          string
	draft         string
}

func clonePiModels(models []piModel) []piModel {
	cloned := make([]piModel, len(models))
	for i, model := range models {
		cloned[i] = model
		cloned[i].Input = append([]string(nil), model.Input...)
	}
	return cloned
}

func clonePiModelFamilies(families []piModelFamily) []piModelFamily {
	cloned := make([]piModelFamily, len(families))
	for i, family := range families {
		cloned[i] = family
		cloned[i].Variants = clonePiModels(family.Variants)
	}
	return cloned
}

func (s *appState) modelSelectionSnapshot(provider, id string) modelSelectionSnapshot {
	snapshot := modelSelectionSnapshot{
		families:   clonePiModelFamilies(piModelFamilies(s.piModels)),
		sessionKey: s.piSessionKey(),
		provider:   provider,
		id:         id,
		draft:      s.composerDraft,
	}
	family, found := findPiModelFamilyInFamilies(snapshot.families, provider, id, "")
	if found {
		snapshot.remembered, snapshot.hasRemembered =
			s.rememberedModelTuple(provider, family.Key)
		for _, variant := range family.Variants {
			if variant.ID == id {
				snapshot.name = variant.Name
				break
			}
		}
	}
	return snapshot
}

func (s *appState) selectModel(provider, id string) {
	if provider == s.piModelProvider && id == s.piModelID {
		return
	}
	if !s.piMutationAllowed(text("noticeCurrentChangeModel")) {
		return
	}
	snapshot := s.modelSelectionSnapshot(provider, id)
	s.cancelThinkingPending()
	client, epoch, gen, ok := s.beginPiSessionOperation(text("noticeCurrentChangeModel"))
	if !ok {
		return
	}
	s.composerDraft = snapshot.draft
	go s.applyModelSelection(client, epoch, gen, snapshot)
}

func (s *appState) applyModelSelection(client *piClient, epoch, gen uint64,
	snapshot modelSelectionSnapshot,
) {
	provider, id := snapshot.provider, snapshot.id
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	model, err := client.SetModel(ctx, provider, id)
	cancel()
	if err != nil {
		s.postUI(func() {
			if s.finishPiSessionOperation(client, epoch, gen) {
				if isPiCommandUnknownResult(err) {
					s.markModelStateUnconfirmed(client, epoch, snapshot.sessionKey, err)
					return
				}
				s.showNoticeAction(
					textArgs("noticeModelChangeFailed", map[string]any{
						"error": uiErrorText(err, text("errorUnknown")),
					}),
					text("retry"),
					func() {
						if s.pi == client && s.piEpoch == epoch && s.piSessionKey() == snapshot.sessionKey {
							s.selectModel(provider, id)
						}
					},
				)
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
	if stateErr != nil {
		stateCtx, stateCancel = context.WithTimeout(context.Background(), 5*time.Second)
		state, stateErr = client.GetState(stateCtx)
		stateCancel()
	}
	if stateErr != nil {
		s.postUI(func() {
			if !s.finishPiSessionOperation(client, epoch, gen) {
				return
			}
			s.markModelStateUnconfirmed(client, epoch, snapshot.sessionKey, stateErr)
			s.refreshPiHUD()
		})
		return
	}
	if levelsErr != nil {
		s.postUI(func() {
			if !s.finishPiSessionOperation(client, epoch, gen) {
				return
			}
			if snapshot.hasRemembered {
				s.applyConfirmedModelStateWithoutRecord(state, nil, false)
			} else {
				s.applyConfirmedModelState(state, nil, false)
			}
			sessionKey := s.piSessionKey()
			if snapshot.hasRemembered {
				s.recordModelTupleFor(snapshot.families, sessionKey, model.Provider, model.ID,
					model.Name, snapshot.remembered.Thinking)
			}
			s.showNoticeAction(
				textArgs("noticeModelLevelsRefreshFailed", map[string]any{
					"error": uiErrorText(levelsErr, text("errorUnknown")),
				}),
				text("retry"),
				func() {
					s.retryPiThinkingLevels(client, epoch, sessionKey)
				},
			)
			s.refreshPiHUD()
		})
		return
	}
	_, found := findPiModelFamilyInFamilies(snapshot.families, model.Provider, model.ID, model.Name)
	restoreThinking, rememberedDefaulted := piThinkingRestore(
		state.ThinkingLevel, snapshot.remembered, snapshot.hasRemembered, levels,
	)
	if restoreThinking != "" {
		thinkingCtx, thinkingCancel := context.WithTimeout(context.Background(), 15*time.Second)
		thinkingErr := client.SetThinkingLevel(thinkingCtx, restoreThinking)
		thinkingCancel()
		if thinkingErr == nil {
			stateCtx, stateCancel = context.WithTimeout(context.Background(), 5*time.Second)
			state, stateErr = client.GetState(stateCtx)
			stateCancel()
		}
		if thinkingErr != nil {
			s.postUI(func() {
				if !s.finishPiSessionOperation(client, epoch, gen) {
					return
				}
				if snapshot.hasRemembered {
					s.applyConfirmedModelStateWithoutRecord(state, levels, true)
				} else {
					s.applyConfirmedModelState(state, levels, true)
				}
				sessionKey := s.piSessionKey()
				if snapshot.hasRemembered {
					s.recordModelTupleFor(snapshot.families, sessionKey, model.Provider, model.ID,
						model.Name, snapshot.remembered.Thinking)
				}
				s.showNoticeAction(
					textArgs("noticeModelThinkingRestoreFailed", map[string]any{
						"error": uiErrorText(thinkingErr, text("errorUnknown")),
					}),
					text("retry"),
					func() {
						s.retryThinkingRestore(client, epoch, sessionKey, snapshot.families,
							provider, id, restoreThinking)
					},
				)
				s.refreshPiHUD()
			})
			return
		}
		if stateErr != nil {
			stateCtx, stateCancel = context.WithTimeout(context.Background(), 5*time.Second)
			state, stateErr = client.GetState(stateCtx)
			stateCancel()
		}
		if stateErr != nil {
			s.postUI(func() {
				if !s.finishPiSessionOperation(client, epoch, gen) {
					return
				}
				s.markModelStateUnconfirmed(client, epoch, snapshot.sessionKey, stateErr)
				s.refreshPiHUD()
			})
			return
		}
	}
	s.postUI(func() {
		if !s.finishPiSessionOperation(client, epoch, gen) {
			return
		}
		if rememberedDefaulted {
			s.applyConfirmedModelStateWithoutRecord(state, levels, true)
		} else {
			s.applyConfirmedModelState(state, levels, true)
		}
		sessionKey := s.piSessionKey()
		if found {
			s.recordModelTupleFor(snapshot.families, sessionKey, model.Provider, model.ID,
				model.Name, state.ThinkingLevel)
		}
		if rememberedDefaulted {
			s.recordModelTupleFor(snapshot.families, sessionKey, model.Provider, model.ID,
				model.Name, snapshot.remembered.Thinking)
		}
		if rememberedDefaulted {
			s.showNotice(text("noticeThinkingDefaulted"))
		}
		s.refreshPiHUD()
	})
}

func isPiCommandUnknownResult(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}

func (s *appState) markModelStateUnconfirmed(client *piClient, epoch uint64,
	sessionKey string, cause error, noticeText ...string,
) {
	if s.pi != client || s.piEpoch != epoch || s.piSessionKey() != sessionKey {
		return
	}
	s.modelStateUnconfirmed = true
	s.cancelThinkingPending()
	notice := textArgs("noticeModelStateUnconfirmed", map[string]any{
		"error": uiErrorText(cause, text("errorUnknown")),
	})
	if len(noticeText) > 0 && strings.TrimSpace(noticeText[0]) != "" {
		notice = noticeText[0]
	}
	s.showNoticeAction(
		notice,
		text("retryReadback"),
		func() {
			if s.pi == client && s.piEpoch == epoch && s.piSessionKey() == sessionKey {
				s.retryModelStateReadback(client, epoch, sessionKey)
			}
		},
	)
}

func (s *appState) retryModelStateReadback(client *piClient, epoch uint64, sessionKey string) {
	if s.pi != client || s.piEpoch != epoch || s.piSessionKey() != sessionKey {
		s.showNotice(text("noticeSessionChanged"))
		return
	}
	client, epoch, gen, ok := s.beginPiSessionOperation(text("noticeCurrentChangeModel"))
	if !ok {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		state, err := client.GetState(ctx)
		cancel()
		s.postUI(func() {
			if !s.finishPiSessionOperation(client, epoch, gen) {
				return
			}
			if s.pi != client || s.piEpoch != epoch || s.piSessionKey() != sessionKey {
				s.showNotice(text("noticeSessionChanged"))
				return
			}
			if err != nil {
				s.showNoticeAction(
					textArgs("noticeModelStateUnconfirmed", map[string]any{
						"error": uiErrorText(err, text("errorUnknown")),
					}),
					text("retryReadback"),
					func() { s.retryModelStateReadback(client, epoch, sessionKey) },
				)
				return
			}
			s.applyConfirmedModelState(state, nil, false)
			s.showNotice(text("noticeModelStateConfirmed"))
			s.refreshPiHUD()
		})
	}()
}

func (s *appState) retrySessionStateReadback(client *piClient, epoch uint64, sessionKey string) {
	if s.pi != client || s.piEpoch != epoch || s.piSessionKey() != sessionKey {
		s.showNotice(text("noticeSessionChanged"))
		return
	}
	client, epoch, gen, ok := s.beginPiSessionOperation(text("noticeCurrentChangeModel"))
	if !ok {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		state, err := client.GetState(ctx)
		cancel()
		s.postUI(func() {
			if !s.finishPiSessionOperation(client, epoch, gen) {
				return
			}
			if s.pi != client || s.piEpoch != epoch || s.piSessionKey() != sessionKey {
				s.showNotice(text("noticeSessionChanged"))
				return
			}
			if err != nil {
				s.showNoticeAction(
					textArgs("noticeModelStateUnconfirmed", map[string]any{
						"error": uiErrorText(err, text("errorUnknown")),
					}),
					text("retryReadback"),
					func() { s.retrySessionStateReadback(client, epoch, sessionKey) },
				)
				return
			}
			s.applyConfirmedModelState(state, nil, false)
			s.showNotice(text("noticeModelStateConfirmed"))
			s.refreshPiHUD()
		})
	}()
}

func (s *appState) retryPiThinkingLevels(client *piClient, epoch uint64, sessionKey string) {
	if s.pi != client || s.piEpoch != epoch || s.piSessionKey() != sessionKey {
		s.showNotice(text("noticeSessionChanged"))
		return
	}
	if s.piSessionBusy || s.thinkingCommitInFlight || s.runActive() || s.compacting {
		s.showNotice(text("noticeSessionBusyRetry"))
		return
	}
	client, epoch, gen, ok := s.beginPiSessionOperation(text("noticeCurrentChangeThinking"))
	if !ok {
		return
	}
	go func() {
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
			if s.piSessionKey() != sessionKey {
				s.showNotice(text("noticeSessionChanged"))
				return
			}
			if stateErr != nil {
				s.markModelStateUnconfirmed(client, epoch, sessionKey, stateErr)
				return
			}
			if levelsErr != nil {
				s.applyConfirmedModelStateWithoutRecord(state, nil, false)
				s.showNoticeAction(
					textArgs("noticeModelLevelsRefreshFailed", map[string]any{
						"error": uiErrorText(levelsErr, text("errorUnknown")),
					}),
					text("retry"),
					func() {
						s.retryPiThinkingLevels(client, epoch, sessionKey)
					},
				)
				return
			}
			s.applyConfirmedThinkingState(state, levels, true, true)
			s.showNotice(text("noticeModelLevelsRefreshed"))
		})
	}()
}

func (s *appState) retryThinkingRestore(client *piClient, epoch uint64, sessionKey string,
	families []piModelFamily, provider, id, level string,
) {
	if s.pi != client || s.piEpoch != epoch || s.piSessionKey() != sessionKey {
		s.showNotice(text("noticeSessionChanged"))
		return
	}
	if s.piSessionBusy || s.thinkingCommitInFlight || s.runActive() || s.compacting {
		s.showNotice(text("noticeSessionBusyRetry"))
		return
	}
	client, epoch, gen, ok := s.beginPiSessionOperation(text("noticeCurrentChangeModel"))
	if !ok {
		return
	}
	go func() {
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
			if s.piSessionKey() != sessionKey {
				s.showNotice(text("noticeSessionChanged"))
				return
			}
			if stateErr != nil {
				s.markModelStateUnconfirmed(client, epoch, sessionKey, stateErr)
				return
			}
			if levelsErr != nil {
				s.applyConfirmedModelStateWithoutRecord(state, nil, false)
				s.showNoticeAction(
					textArgs("noticeModelLevelsRefreshFailed", map[string]any{
						"error": uiErrorText(levelsErr, text("errorUnknown")),
					}),
					text("retry"),
					func() {
						s.retryThinkingRestore(client, epoch, sessionKey, families,
							provider, id, level)
					},
				)
				return
			}
			if state.Model.Provider != provider || state.Model.ID != id {
				s.markModelStateUnconfirmed(client, epoch, sessionKey,
					errors.New(text("noticeModelMismatch")))
				return
			}
			levelAvailable := thinkingLevelAvailable(levels, level)
			needsThinkingRestore := levelAvailable && state.ThinkingLevel != level
			if !levelAvailable || needsThinkingRestore {
				s.applyConfirmedThinkingStateWithoutRecord(state, levels, true, false)
			} else {
				s.applyConfirmedThinkingState(state, levels, true, false)
			}
			if !levelAvailable {
				s.recordModelTupleFor(families, s.piSessionKey(), provider, id, state.Model.Name, level)
				s.showNotice(text("noticeThinkingDefaulted"))
				return
			}
			if needsThinkingRestore {
				s.selectThinkingLevel(level)
				return
			}
			s.showNotice(text("noticeModelLevelsRefreshed"))
		})
	}()
}

func (s *appState) selectThinkingLevel(level string) bool {
	client := s.pi
	epoch := s.piEpoch
	sessionKey := s.piSessionKey()
	client, epoch, gen, ok := s.beginThinkingCommit(level)
	if !ok {
		s.revertThinkingPending(level)
		return false
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		setErr := client.SetThinkingLevel(ctx, level)
		if setErr != nil && !isPiCommandUnknownResult(setErr) {
			s.postUI(func() {
				if s.finishThinkingCommit(client, epoch, gen) {
					s.dropThinkingPending()
					s.showNoticeAction(
						textArgs("noticeThinkingChangeFailed", map[string]any{
							"error": uiErrorText(setErr, text("errorUnknown")),
						}),
						text("retry"),
						func() {
							if s.pi == client && s.piEpoch == epoch && s.piSessionKey() == sessionKey {
								s.selectThinkingLevel(level)
							}
						},
					)
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
			if !s.finishThinkingCommit(client, epoch, gen) ||
				s.piSessionKey() != sessionKey {
				return
			}
			confirmed := stateErr == nil && s.applyConfirmedThinkingState(
				state, levels, levelsErr == nil, true,
			)
			if s.chatView.thinkingPending == level {
				s.chatView.thinkingPending = ""
			}
			if s.chatView.effortPicker.Open {
				s.scheduleThinkingPendingCommit()
			} else {
				s.commitThinkingPending()
			}
			if !confirmed {
				s.markModelStateUnconfirmed(client, epoch, sessionKey, stateErr)
			} else if levelsErr != nil {
				s.showNoticeAction(
					textArgs("noticeModelLevelsRefreshFailed", map[string]any{
						"error": uiErrorText(levelsErr, text("errorUnknown")),
					}),
					text("retry"),
					func() {
						s.retryPiThinkingLevels(client, epoch, sessionKey)
					},
				)
			}
			s.refreshPiHUD()
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
	s.clearPiConnectingNotice()
	s.invalidateSessionNaming()
	s.clearPiHUD()
	s.piError = ""
	s.piErrorCode = ""
	s.piErrorDetailsOpen = false
	s.resetModelTuples()
	s.modelStateUnconfirmed = false
	s.piEpoch++
	s.sessionGen++
	epoch := s.piEpoch
	s.piConnecting = true
	s.piConn = piConnecting
	s.piCwd = ""
	s.piPath = ""
	s.piError = ""
	s.showNotice(text("noticePiConnecting"))
	s.refreshPiSessions()

	savedWorkspace := s.preferences.Workspace
	connectCtx, connectCancel := context.WithCancel(context.Background())
	s.piConnectCancel = connectCancel
	s.piConnectGeneration++
	connectGeneration := s.piConnectGeneration
	go func() {
		defer connectCancel()
		defer func() {
			s.postUI(func() {
				if connectGeneration != s.piConnectGeneration {
					return
				}
				s.piConnectCancel = nil
			})
		}()
		path, err := resolvePiPath(connectCtx)
		if err != nil {
			s.postUI(func() {
				if epoch != s.piEpoch || connectGeneration != s.piConnectGeneration ||
					connectCtx.Err() != nil {
					return
				}
				s.piConnecting = false
				s.piConn = piConnectionErr
				s.piErrorCode = classifyPiConnectionError(err)
				s.piError = uiErrorText(err, text("errorPiResolveFailed"))
				s.piErrorDetailsOpen = false
				s.clearPiConnectingNotice()
			})
			return
		}
		if connectCtx.Err() != nil {
			return
		}
		ctx, cancel := context.WithTimeout(connectCtx, 5*time.Second)
		version := piCLIVersion(ctx, path)
		cancel()
		if connectCtx.Err() != nil {
			return
		}
		cwd, err := piWorkspaceCwd(savedWorkspace)
		if err != nil {
			s.postUI(func() {
				if epoch != s.piEpoch || connectGeneration != s.piConnectGeneration ||
					connectCtx.Err() != nil {
					return
				}
				s.piConnecting = false
				s.piConn = piConnectionErr
				s.piErrorCode = classifyPiConnectionError(err)
				s.piError = uiErrorText(err, text("errorWorkspaceFailed"))
				s.piErrorDetailsOpen = false
				s.clearPiConnectingNotice()
			})
			return
		}
		if connectCtx.Err() != nil {
			return
		}
		client, err := startPiClient(path, cwd, func(event piEvent) {
			s.postUI(func() { s.handlePiEvent(event, epoch) })
		})
		if err != nil {
			s.postUI(func() {
				if epoch != s.piEpoch || connectGeneration != s.piConnectGeneration ||
					connectCtx.Err() != nil {
					return
				}
				s.piConnecting = false
				s.piConn = piConnectionErr
				s.piErrorCode = classifyPiConnectionError(err)
				s.piError = uiErrorText(err, text("errorPiConnectFailed"))
				s.piErrorDetailsOpen = false
				s.clearPiConnectingNotice()
			})
			return
		}
		if connectCtx.Err() != nil {
			client.Close()
			return
		}
		handshakeCtx, handshakeCancel := context.WithTimeout(connectCtx, 5*time.Second)
		state, err := client.GetState(handshakeCtx)
		handshakeCancel()
		if err != nil {
			client.Close()
			s.postUI(func() {
				if epoch != s.piEpoch || connectGeneration != s.piConnectGeneration ||
					connectCtx.Err() != nil {
					return
				}
				s.piConnecting = false
				s.piConn = piConnectionErr
				s.piErrorCode = classifyPiConnectionError(err)
				s.piError = uiErrorText(err, text("errorPiRPCFailed"))
				s.piErrorDetailsOpen = false
				s.clearPiConnectingNotice()
			})
			return
		}
		if connectCtx.Err() != nil {
			client.Close()
			return
		}
		modelsCtx, modelsCancel := context.WithTimeout(connectCtx, 10*time.Second)
		models, modelsErr := client.GetAvailableModels(modelsCtx)
		modelsCancel()
		if connectCtx.Err() != nil {
			client.Close()
			return
		}
		levelsCtx, levelsCancel := context.WithTimeout(connectCtx, 5*time.Second)
		levels, levelsErr := client.GetAvailableThinkingLevels(levelsCtx)
		levelsCancel()
		if connectCtx.Err() != nil {
			client.Close()
			return
		}
		s.postUI(func() {
			if epoch != s.piEpoch || connectGeneration != s.piConnectGeneration {
				client.Close()
				return
			}
			s.pi = client
			s.piPath = path
			s.piCwd = client.cwd
			s.piConnecting = false
			s.piConn = piConnected
			s.piVersion = version
			s.applyPiSessionState(state)
			s.beginPiHUD(client.cwd, state)
			if modelsErr == nil {
				s.piModels = models
			}
			if levelsErr == nil {
				s.piThinkingLevels = levels
				s.piThinkingLevelsKnown = true
			}
			s.recordModelTuple()
			s.piError = ""
			s.piErrorCode = ""
			s.piErrorDetailsOpen = false
			s.resetRuntimeDiagnostics()
			if modelsErr != nil || levelsErr != nil {
				s.showNotice(text("noticePiConnectedNoModels"))
			} else {
				s.showNotice(text("noticePiConnected"))
			}
			s.refreshPiSessions()
			s.pumpChannelRefresh()
		})
	}()
}

func (s *appState) reconnectPi() {
	s.resetFollowUpSuggestion()
	s.invalidateSessionNaming()
	s.sessionGen++
	s.piSessionBusy = false
	s.cancelThinkingCommit()
	s.cancelThinkingPending()
	if s.piSessionsTimer != nil {
		s.piSessionsTimer.Stop()
		s.piSessionsTimer = nil
	}
	if s.sessionLaterTimer != nil {
		s.sessionLaterTimer.Stop()
		s.sessionLaterTimer = nil
	}
	s.piSessionsRefresh.Add(1)
	s.piSessionsOrgWorkspace = ""
	s.piSessionsGroups = sessionGroups{}
	s.piConnectGeneration++
	if s.piConnectCancel != nil {
		s.piConnectCancel()
		s.piConnectCancel = nil
	}
	if s.run == runSubmitting || s.run == runStreaming || s.run == runStopping {
		s.markOpenTools(toolCallInterrupted)
		s.setAssistantStatus("Interrupted")
	}
	s.resetRuntimeDiagnostics()
	s.clearPiHUD()
	if s.pi != nil {
		old := s.pi
		s.pi = nil
		old.Close()
	}
	s.piConn = piDisconnected
	s.piErrorCode = ""
	s.piErrorDetailsOpen = false
	s.piStatusPopoverOpen = false
	s.piCwd = ""
	s.piSessionFile = ""
	s.piSessionID = ""
	s.piSessionName = ""
	s.resetPiHUDData()
	s.localSessionTitle = ""
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
	s.piThinkingLevelsKnown = false
	s.piModels = nil
	s.modelStateUnconfirmed = false
	s.connectPi()
}

func (s *appState) shutdownPi() {
	s.closed.Store(true)
	s.piConnectGeneration++
	if s.piConnectCancel != nil {
		s.piConnectCancel()
		s.piConnectCancel = nil
	}
	s.resetFollowUpSuggestion()
	s.invalidateSessionNaming()
	if s.sessionLaterTimer != nil {
		s.sessionLaterTimer.Stop()
		s.sessionLaterTimer = nil
	}
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
	s.sendPromptWithMode(s.currentRunningSendMode())
}

func (s *appState) sendPromptWithMode(mode runningSendMode) {
	prompt := strings.TrimSpace(s.composerDraft)
	if prompt == "" {
		s.showNotice(text("noticeMessageEmpty"))
		return
	}
	if s.pi == nil || s.piConn != piConnected {
		s.showNotice(text("noticePiDisconnected"))
		return
	}
	if s.piSessionBusy {
		s.showNotice(text("noticeSessionOperationBeforeSend"))
		return
	}
	if s.thinkingCommitInFlight {
		s.showNotice(text("noticeThinkingBeforeSend"))
		return
	}
	if s.modelStateUnconfirmed {
		s.showNotice(text("noticeModelStateBeforeSend"))
		return
	}
	if s.run == runStopping || s.compacting {
		s.showNotice(text("noticeRunningSendUnavailable"))
		return
	}
	if s.run == runSubmitting || s.run == runStreaming {
		s.sendRunningPrompt(prompt, mode)
		return
	}

	attachment := cloneTableContext(s.composerTableContext)
	wirePrompt := tableContextPrompt(prompt, attachment)
	s.resetFollowUpSuggestion()
	s.cancelThinkingPending()
	s.sessionGen++
	gen := s.sessionGen
	s.piSessionBusy = true
	requestID, err := s.pi.Prompt(wirePrompt, "")
	if err != nil {
		if gen == s.sessionGen {
			s.piSessionBusy = false
		}
		s.run = runFailed
		s.piError = uiErrorText(err, text("errorPiUnavailable"))
		s.showNotice(textArgs("noticeSendFailed", map[string]any{"error": s.piError}))
		return
	}
	s.resetRuntimeDiagnostics()
	s.composerDraft = ""
	s.clearTableContextIfMatches(attachment)
	s.clearPiConnectingNotice()
	if s.piSessionName == "" && s.localSessionTitle == "" {
		s.localSessionTitle = localTemporarySessionTitle(prompt)
	}
	s.piError = ""
	s.run = runSubmitting
	s.runID = requestID
	s.activeRequest = requestID
	s.activePrompt = prompt
	s.piSessionBusy = false
	s.startAssistantPrompt(wirePrompt)
}

func (s *appState) startAssistantPrompt(prompt string) {
	displayPrompt := displayTableContextPrompt(prompt)
	s.chat = append(s.chat,
		chatMessage{
			role: chatUser,
			blocks: []chatBlock{{
				kind: chatBlockText,
				text: displayPrompt,
			}},
			status: text("chatStatusSent"),
		},
		chatMessage{role: chatAssistant, status: text("assistantWaiting"), startedAt: time.Now()},
	)
	s.pendingPiUserEcho = prompt
}

func (s *appState) sendRunningPrompt(prompt string, mode runningSendMode) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		s.showNotice(text("noticeMessageEmpty"))
		return
	}
	if s.pi == nil || s.piConn != piConnected {
		s.showNotice(text("noticePiDisconnected"))
		return
	}
	if s.piSessionBusy {
		s.showNotice(text("noticeSessionOperationBeforeSend"))
		return
	}
	if s.thinkingCommitInFlight {
		s.showNotice(text("noticeThinkingBeforeSend"))
		return
	}
	if s.modelStateUnconfirmed {
		s.showNotice(text("noticeModelStateBeforeSend"))
		return
	}
	if s.compacting || (s.run != runSubmitting && s.run != runStreaming) {
		s.showNotice(text("noticeRunningSendUnavailable"))
		return
	}
	if s.runningSendPending {
		return
	}
	attachment := cloneTableContext(s.composerTableContext)
	wirePrompt := tableContextPrompt(prompt, attachment)
	mode = normalizeRunningSendMode(string(mode))
	behavior := runningSendBehavior(mode)
	client := s.pi
	epoch := s.piEpoch
	gen := s.sessionGen
	requestBefore := s.activeRequest
	runBefore := s.runID
	runStateBefore := s.run
	s.runningSendPending = true
	s.runningSendPrompt = wirePrompt
	s.runningSendDisplay = prompt
	s.runningSendContext = attachment
	s.runningSendEchoSeen = false
	s.composerDraft = ""
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		result, err := client.PromptRequest(ctx, wirePrompt, behavior)
		s.postUI(func() {
			s.completeRunningSend(client, wirePrompt, mode, epoch, gen,
				requestBefore, runBefore, runStateBefore, result, err)
		})
	}()
}

func (s *appState) completeRunningSend(client *piClient, prompt string, mode runningSendMode,
	epoch, gen uint64, requestBefore, runBefore string, runStateBefore runState,
	result piPromptResult, err error,
) bool {
	if !s.runningSendPending {
		return false
	}
	displayPrompt := s.runningSendDisplay
	if displayPrompt == "" {
		displayPrompt = displayTableContextPrompt(prompt)
	}
	attachment := cloneTableContext(s.runningSendContext)
	s.runningSendPending = false
	echoSeen := s.runningSendEchoSeen
	s.runningSendPrompt = ""
	s.runningSendDisplay = ""
	s.runningSendContext = nil
	s.runningSendEchoSeen = false
	if err != nil {
		if strings.TrimSpace(s.composerDraft) == "" {
			s.composerDraft = displayPrompt
		}
		if epoch == s.piEpoch && s.pi == client && gen == s.sessionGen &&
			s.runActive() && s.activeRequest == requestBefore && s.runID == runBefore {
			s.run = runStateBefore
		}
		s.showNotice(textArgs("noticeRunningSendFailed", map[string]any{
			"error": uiErrorText(err, text("errorRunningSendUnavailable")),
		}))
		return false
	}
	if epoch != s.piEpoch || s.pi != client || gen != s.sessionGen {
		return false
	}
	disposition := strings.ToLower(strings.TrimSpace(result.Disposition))
	switch disposition {
	case "started":
		if !echoSeen {
			s.startAssistantPrompt(prompt)
		}
		s.run = runStreaming
		s.runID = result.RequestID
		s.activeRequest = result.RequestID
		s.activePrompt = displayPrompt
	case "queued", "handled":
	default:
		s.composerDraft = displayPrompt
		s.showNotice(textArgs("noticeRunningSendFailed", map[string]any{
			"error": text("errorRunningSendUnavailable"),
		}))
		return false
	}
	s.clearTableContextIfMatches(attachment)
	s.notice = textArgs("noticeRunningSendAccepted", map[string]any{
		"mode": runningSendModeLabel(mode),
	})
	return true
}

func runningSendModeLabel(mode runningSendMode) string {
	if normalizeRunningSendMode(string(mode)) == runningSendSteer {
		return text("runningSendSteer")
	}
	return text("runningSendQueue")
}

func (s *appState) queueClear() {
	if s.pi == nil || s.queueSteeringCount+s.queueFollowUpCount == 0 || s.queueClearPending {
		return
	}
	client := s.pi
	epoch := s.piEpoch
	gen := s.sessionGen
	s.queueClearPending = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		steering, followUp, err := client.ClearQueue(ctx)
		s.postUI(func() {
			s.queueClearPending = false
			if epoch != s.piEpoch || s.pi != client || gen != s.sessionGen {
				return
			}
			if err != nil {
				s.showNotice(textArgs("noticeQueueClearFailed", map[string]any{
					"error": uiErrorText(err, text("errorPiUnavailable")),
				}))
				return
			}
			count := clearedQueueCount(steering, followUp)
			s.finishQueueClear()
			s.notice = textArgs("noticeQueueCleared", map[string]any{
				"count": count,
			})
		})
	}()
}

func clearedQueueCount(steering, followUp []string) int {
	return len(steering) + len(followUp)
}

func (s *appState) finishQueueClear() {
	s.queueSteering = nil
	s.queueFollowUp = nil
	s.queueSteeringCount = 0
	s.queueFollowUpCount = 0
}

func (s *appState) resetRuntimeDiagnostics() {
	s.queueSteering = nil
	s.queueFollowUp = nil
	s.queueSteeringCount = 0
	s.queueFollowUpCount = 0
	s.queueClearPending = false
	s.runningSendPending = false
	s.runningSendPrompt = ""
	s.runningSendDisplay = ""
	s.runningSendContext = nil
	s.runningSendEchoSeen = false
	s.pendingPiUserEcho = ""
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
		result, err := client.Stop(ctx, abortRetry)
		s.postUI(func() {
			if err != nil {
				s.piError = uiErrorText(err, text("noticeStopFailed"))
				s.notice = textArgs("noticeStopFailed", map[string]any{"error": s.piError})
			} else if result.QueueCleared > 0 {
				s.notice = textArgs("noticeStopRequestedQueue", map[string]any{"count": result.QueueCleared})
			} else if result.RetryStopped {
				s.notice = text("noticeStopRequestedRetry")
			} else {
				s.notice = text("noticeStopRequested")
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
			s.piError = uiErrorFromText(event.Error, text("errorPiPromptRejected"))
			s.notice = textArgs("noticePromptRejected", map[string]any{"error": s.piError})
			s.markOpenTools(toolCallFailed)
			s.finishAssistant(s.piError, "failed")
			return
		}
		if event.Disposition == "handled" {
			s.run = runCompleted
			s.finishAssistant(text("noticePiHandledExtension"), "handled")
			return
		}
		s.run = runStreaming
	case piEventRunStarted:
		s.resetFollowUpSuggestion()
		s.run = runStreaming
	case piEventUserMessage:
		s.run = runStreaming
		s.appendRunningUserMessage(event.Text)
	case piEventText:
		s.run = runStreaming
		s.setAssistantText(event.Text, "Streaming")
	case piEventMessage:
		s.run = runStreaming
		if event.OutputTokens > 0 && event.GenerationTime > 0 {
			s.recordPiHUDSample(event.OutputTokens, event.GenerationTime.Seconds())
		}
		if event.Text != "" {
			s.setAssistantText(event.Text, "Received")
		}
		if event.Error != "" {
			s.piError = uiErrorFromText(event.Error, text("errorPiMessageFailed"))
		}
	case piEventQueueUpdate:
		s.queueSteering = displayTableContextPrompts(event.QueueSteering)
		s.queueFollowUp = displayTableContextPrompts(event.QueueFollowUp)
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
			s.compactionError = uiErrorFromText(s.compactionError, text("errorCompactionFailed"))
		}
		s.agentWillRetry = event.WillRetry
		s.compactionResult = compactionResultLabel(event)
		if event.CompactError != "" {
			s.piError = s.compactionError
		}
		if s.runActive() {
			s.run = runStreaming
		}
		s.refreshPiHUD()
	case piEventRetryStart:
		s.retrying = true
		s.retryAttempt = event.RetryAttempt
		s.retryMax = event.RetryMax
		s.retryDelayMS = event.RetryDelayMS
		s.retryStartedAt = time.Now()
		s.retryError = event.RetryError
		if s.retryError != "" {
			s.retryError = uiErrorFromText(s.retryError, text("errorRetryFailed"))
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
				s.retryError = uiErrorFromText(s.retryError, text("errorRetryFailed"))
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
		s.extensionError = uiErrorFromText(event.Error, text("errorExtensionFailed"))
		s.extensionPath = event.ExtensionPath
		s.extensionEvent = event.ExtensionEvent
		s.notice = textArgs("noticePiExtensionError", map[string]any{"error": s.extensionError})
	case piEventExtensionStatus:
		key := strings.TrimSpace(event.StatusKey)
		if key == "" {
			return
		}
		if s.hud.extStatus == nil {
			s.hud.extStatus = make(map[string]string)
		}
		if event.StatusText == nil {
			delete(s.hud.extStatus, key)
		} else if status := sanitizePiStatusText(*event.StatusText); status != "" {
			s.hud.extStatus[key] = status
		} else {
			delete(s.hud.extStatus, key)
		}
	case piEventExtensionNotify:
		if message := strings.TrimSpace(event.Text); message != "" {
			s.showNotice(message)
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
		prompt := s.activePrompt
		for i := len(s.chat) - 1; i >= 0; i-- {
			if s.chat[i].role != chatUser {
				continue
			}
			if text := strings.TrimSpace(messageText(&s.chat[i])); text != "" {
				prompt = text
				break
			}
		}
		reply := ""
		if len(s.chat) > 0 && s.chat[len(s.chat)-1].role == chatAssistant {
			reply = messageText(&s.chat[len(s.chat)-1])
		}
		if s.runActive() {
			s.sessionGen++
		}
		s.piSessionBusy = false
		s.activeRequest = ""
		s.activePrompt = ""
		s.runningSendPending = false
		s.runningSendPrompt = ""
		s.runningSendEchoSeen = false
		s.pendingPiUserEcho = ""
		s.queueSteering = nil
		s.queueFollowUp = nil
		s.queueSteeringCount = 0
		s.queueFollowUpCount = 0
		s.compacting = false
		s.retrying = false
		s.agentWillRetry = false
		if event.Aborted || s.run == runStopping {
			s.run = runStopped
			s.markOpenTools(toolCallCancelled)
			s.ensureAssistantFallback(text("assistantStoppedFallback"))
			s.setAssistantStatus("Stopped")
		} else if s.piError != "" {
			s.run = runFailed
			s.markOpenTools(toolCallFailed)
			s.ensureAssistantFallback(text("assistantErrorFallback"))
			s.setAssistantStatus("failed")
		} else {
			s.run = runCompleted
			s.ensureAssistantFallback(text("assistantEmptyFallback"))
			s.setAssistantStatus("Complete")
			if strings.TrimSpace(reply) != "" {
				s.startFollowUpSuggestion(prompt, reply)
			}
			s.maybeStartAutoSessionNaming(prompt, reply)
		}
		s.refreshPiSessionState()
		s.refreshPiHUD()
		s.schedulePiSessionsRefresh()
		s.pumpChannelRefresh()
	case piEventParseError:
		s.piError = uiErrorFromText(event.Error, text("errorProtocolWarning"))
		s.notice = textArgs("noticePiProtocolWarning", map[string]any{"error": s.piError})
	case piEventProcessExit:
		s.resetFollowUpSuggestion()
		s.invalidateSessionNaming()
		s.sessionGen++
		s.piSessionBusy = false
		s.cancelThinkingCommit()
		s.cancelThinkingPending()
		s.clearPiConnectingNotice()
		s.pi = nil
		s.clearPiHUD()
		s.piCwd = ""
		s.piConnecting = false
		s.piConn = piDisconnected
		s.piErrorCode = piErrorCodeExited
		s.piError = uiErrorFromText(fallback(event.StderrTail, event.Error), text("errorPiDisconnected"))
		s.piErrorDetailsOpen = false
		s.piSessionFile = ""
		s.piSessionID = ""
		s.piSessionName = ""
		s.localSessionTitle = ""
		s.piModel = ""
		s.piModelProvider = ""
		s.piModelID = ""
		s.piModelName = ""
		s.piThinkingLevel = ""
		s.piThinkingLevels = nil
		s.piThinkingLevelsKnown = false
		s.piModels = nil
		s.resetModelTuples()
		s.resetRuntimeDiagnostics()
		if s.run == runSubmitting || s.run == runStreaming || s.run == runStopping {
			s.run = runInterrupted
			s.markOpenTools(toolCallInterrupted)
			s.ensureAssistantFallback(text("assistantExitedFallback"))
			s.setAssistantStatus("Interrupted")
		} else if s.closed.Load() {
			return
		}
		s.pumpChannelRefresh()
	}
}

func classifyPiConnectionError(err error) string {
	switch {
	case errors.Is(err, os.ErrNotExist), errors.Is(err, exec.ErrNotFound):
		return piErrorCodeNotFound
	case errors.Is(err, os.ErrPermission), errors.Is(err, syscall.ENOEXEC):
		return piErrorCodeNotExecutable
	case errors.Is(err, context.DeadlineExceeded):
		return piErrorCodeHandshakeTimeout
	case errors.Is(err, errPiProcessExited):
		return piErrorCodeExited
	default:
		return piErrorCodeUnknown
	}
}

func (s *appState) setAssistantText(value, status string) {
	message := s.currentAssistant()
	if !message.endedAt.IsZero() {
		s.chat = append(s.chat, chatMessage{
			role:      chatAssistant,
			status:    text("assistantWaiting"),
			startedAt: time.Now(),
		})
		message = &s.chat[len(s.chat)-1]
	}
	wasWaiting := message.status == locales.AssistantWaiting()
	message.status = status
	if strings.TrimSpace(value) == "" {
		return
	}
	if len(message.blocks) > 0 {
		last := &message.blocks[len(message.blocks)-1]
		if last.kind == chatBlockText {
			if wasWaiting && strings.TrimSpace(last.text) != "" && last.text != value {
				message.blocks = append(message.blocks, chatBlock{kind: chatBlockText, text: value})
				return
			}
			last.text = value
			return
		}
	}
	message.blocks = append(message.blocks, chatBlock{kind: chatBlockText, text: value})
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

func (s *appState) appendRunningUserMessage(value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if s.pendingPiUserEcho == value {
		s.pendingPiUserEcho = ""
		return
	}
	if s.runningSendPending && s.runningSendPrompt == value {
		s.runningSendEchoSeen = true
	}
	value = displayTableContextPrompt(value)
	if len(s.chat) > 0 && s.chat[len(s.chat)-1].role == chatAssistant {
		s.markAssistantEnded()
	}
	s.chat = append(s.chat,
		chatMessage{
			role:   chatUser,
			status: text("chatStatusSent"),
			blocks: []chatBlock{{kind: chatBlockText, text: value}},
		},
		chatMessage{role: chatAssistant, status: text("assistantWaiting"), startedAt: time.Now()},
	)
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
			name:      fallback(event.ToolName, text("errorToolFallback")),
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
				name:     fallback(event.ToolName, text("errorToolFallback")),
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
	return textArgs("runDetailCurrentTool", map[string]any{
		"tool":     tool.name,
		"status":   toolCallStatusLabel(tool.status),
		"duration": formatToolDuration(tool.durationMS),
	})
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
		var plain string
		if json.Unmarshal(blockRaw, &plain) == nil {
			if plain != "" {
				parts = append(parts, plain)
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
			parts = append(parts, "["+fallback(block.MimeType, text("toolImageFallback"))+"]")
		default:
			data, _ := json.Marshal(block)
			if len(data) > 2 {
				parts = append(parts, string(data))
			}
		}
	}
	return strings.Join(parts, "\n\n"), len(parts) > 0
}

func truncateToolText(value string, limit int) string {
	value = strings.Trim(value, "\r\n")
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "\n" + text("toolOutputTruncated")
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func toolCallStatusLabel(status toolCallStatus) string {
	switch status {
	case toolCallRunning:
		return text("toolStatusRunning")
	case toolCallCompleted:
		return text("toolStatusCompleted")
	case toolCallFailed:
		return text("toolStatusFailed")
	case toolCallCancelled:
		return text("toolStatusCancelled")
	case toolCallInterrupted:
		return text("toolStatusInterrupted")
	default:
		return text("toolStatusUnknown")
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
		return text("piPathNotResolved")
	}
	if strings.TrimSpace(os.Getenv("BYOP_PI_PATH")) == "" {
		return text("piPathFromPATH")
	}
	return s.piPath
}

func (s *appState) DebugSummary() string {
	return fmt.Sprintf("%s:%s:nav=%t:tasks=%t:pi=%s:run=%s", appShort, s.page, !s.navCollapsed, s.tasksOpen, s.piConn, s.run)
}
