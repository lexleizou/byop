package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"time"
)

// ChannelRefreshPhase is displayed by channel settings. It never means that a
// saved channel is active until a fresh Pi process has passed every readback.
type ChannelRefreshPhase string

const (
	channelRefreshPhaseIdle      ChannelRefreshPhase = "idle"
	channelRefreshPhasePending   ChannelRefreshPhase = "pending"
	channelRefreshPhaseRunning   ChannelRefreshPhase = "running"
	channelRefreshPhaseSucceeded ChannelRefreshPhase = "succeeded"
	channelRefreshPhaseFailed    ChannelRefreshPhase = "failed"
)

// ChannelRefreshFailure identifies which required part of the session restore
// failed. UI code maps it to a locale key; this package stores no user-facing
// strings.
type ChannelRefreshFailure string

const (
	channelRefreshFailureNone                ChannelRefreshFailure = ""
	channelRefreshFailureLaunch              ChannelRefreshFailure = "launch"
	channelRefreshFailureSessionUnavailable  ChannelRefreshFailure = "sessionUnavailable"
	channelRefreshFailureModelUnavailable    ChannelRefreshFailure = "modelUnavailable"
	channelRefreshFailureThinkingUnavailable ChannelRefreshFailure = "thinkingUnavailable"
	channelRefreshFailureReadback            ChannelRefreshFailure = "readback"
	channelRefreshFailureTranscript          ChannelRefreshFailure = "transcript"
)

// ChannelRefreshStatus is a read-only UI snapshot. Pending can be true while
// another attempt is running when a second channel save arrives.
type ChannelRefreshStatus struct {
	Phase          ChannelRefreshPhase
	Pending        bool
	Retryable      bool
	Failure        ChannelRefreshFailure
	Request        uint64
	AttemptRequest uint64
	WaitingIdle    bool
}

// channelRefreshCoordinator is mutated only on the UI thread. Workflow
// goroutines receive a captured snapshot and return a result through postUI.
type channelRefreshCoordinator struct {
	phase          ChannelRefreshPhase
	pending        bool
	inFlight       bool
	request        uint64
	attemptRequest uint64
	failure        ChannelRefreshFailure
	launch         channelRefreshLaunchFunc
	post           func(func())
}

type channelRefreshSnapshot struct {
	oldClient       *piClient
	epoch           uint64
	path            string
	cwd             string
	version         string
	sessionFile     string
	sessionID       string
	sessionName     string
	provider        string
	modelID         string
	modelName       string
	thinking        string
	hadConversation bool
	request         uint64
}

type channelRefreshLaunchFunc func(path, cwd string, events func(piEvent)) (channelRefreshClient, error)

type channelRefreshClient interface {
	GetState(context.Context) (*piSessionState, error)
	SwitchSession(context.Context, string) (bool, error)
	SetModel(context.Context, string, string) (piModel, error)
	SetThinkingLevel(context.Context, string) error
	GetAvailableModels(context.Context) ([]piModel, error)
	GetAvailableThinkingLevels(context.Context) ([]string, error)
	GetMessages(context.Context) ([]json.RawMessage, error)
	rawClient() *piClient
	Close()
}

type channelRefreshPiAdapter struct {
	client *piClient
}

func (a *channelRefreshPiAdapter) GetState(ctx context.Context) (*piSessionState, error) {
	return a.client.GetState(ctx)
}

func (a *channelRefreshPiAdapter) SwitchSession(ctx context.Context, path string) (bool, error) {
	return a.client.SwitchSession(ctx, path)
}

func (a *channelRefreshPiAdapter) SetModel(ctx context.Context, provider, id string) (piModel, error) {
	return a.client.SetModel(ctx, provider, id)
}

func (a *channelRefreshPiAdapter) SetThinkingLevel(ctx context.Context, level string) error {
	return a.client.SetThinkingLevel(ctx, level)
}

func (a *channelRefreshPiAdapter) GetAvailableModels(ctx context.Context) ([]piModel, error) {
	return a.client.GetAvailableModels(ctx)
}

func (a *channelRefreshPiAdapter) GetAvailableThinkingLevels(ctx context.Context) ([]string, error) {
	return a.client.GetAvailableThinkingLevels(ctx)
}

func (a *channelRefreshPiAdapter) GetMessages(ctx context.Context) ([]json.RawMessage, error) {
	return a.client.GetMessages(ctx)
}

func (a *channelRefreshPiAdapter) rawClient() *piClient {
	return a.client
}

func (a *channelRefreshPiAdapter) Close() {
	a.client.Close()
}

type channelRefreshOutcome struct {
	client     channelRefreshClient
	eventEpoch *atomic.Uint64
	eventDeny  *atomic.Bool
	state      *piSessionState
	models     []piModel
	levels     []string
	chat       []chatMessage
	failure    ChannelRefreshFailure
	err        error
}

func defaultChannelRefreshLaunch(path, cwd string, events func(piEvent)) (channelRefreshClient, error) {
	client, err := startPiClient(path, cwd, events)
	if err != nil {
		return nil, err
	}
	return &channelRefreshPiAdapter{client: client}, nil
}

// markChannelRefreshPending is called after models.json was saved. It only
// records the need for a fresh process; a live run is never interrupted.
func (s *appState) markChannelRefreshPending() {
	if s == nil || s.closed.Load() {
		return
	}
	state := &s.channelRefresh
	state.request++
	state.pending = true
	state.failure = channelRefreshFailureNone
	if state.inFlight {
		state.phase = channelRefreshPhaseRunning
	} else {
		state.attemptRequest = 0
		state.phase = channelRefreshPhasePending
	}
	s.pumpChannelRefresh()
}

// pumpChannelRefresh is idempotent. Maxwell calls it at operation and process
// lifecycle boundaries; only a strictly idle UI state starts a new process.
func (s *appState) pumpChannelRefresh() {
	if s == nil || s.closed.Load() {
		return
	}
	state := &s.channelRefresh
	if !state.pending || state.inFlight {
		return
	}
	if !s.channelRefreshReady() {
		state.phase = channelRefreshPhasePending
		return
	}

	oldClient := s.pi
	epoch := s.piEpoch
	state.inFlight = true
	state.attemptRequest = state.request
	state.phase = channelRefreshPhaseRunning
	state.failure = channelRefreshFailureNone
	snapshot := s.captureChannelRefreshSnapshot(oldClient, epoch)
	launch := state.launch
	if launch == nil {
		launch = defaultChannelRefreshLaunch
	}

	go func() {
		eventEpoch := &atomic.Uint64{}
		eventDeny := &atomic.Bool{}
		events := func(event piEvent) {
			source := event.Source
			epoch := eventEpoch.Load()
			if epoch == 0 {
				return
			}
			s.postChannelRefreshUI(func() {
				if eventDeny.Load() {
					return
				}
				if s.pi != source || s.piEpoch != epoch {
					return
				}
				s.handlePiEvent(event, epoch)
			})
		}
		outcome := runChannelRefreshAttempt(snapshot, launch, events)
		outcome.eventEpoch = eventEpoch
		outcome.eventDeny = eventDeny
		s.postChannelRefreshUI(func() {
			shouldPump := s.applyChannelRefreshOutcome(snapshot, outcome)
			if shouldPump {
				s.pumpChannelRefresh()
			}
		})
	}()
}

// retryChannelRefresh retries the current saved request without claiming that
// it is active. Busy work delays the attempt; disconnecting does not rerun the
// old reconnect path.
func (s *appState) retryChannelRefresh() {
	if s == nil || s.closed.Load() {
		return
	}
	state := &s.channelRefresh
	if state.inFlight {
		return
	}
	state.pending = true
	state.failure = channelRefreshFailureNone
	state.phase = channelRefreshPhasePending
	s.pumpChannelRefresh()
}

func (s *appState) channelRefreshStatus() ChannelRefreshStatus {
	if s == nil {
		return ChannelRefreshStatus{Phase: channelRefreshPhaseIdle}
	}
	state := s.channelRefresh
	phase := state.phase
	if phase == "" {
		phase = channelRefreshPhaseIdle
	}
	failure := state.failure
	request := state.request
	attemptRequest := state.attemptRequest
	return ChannelRefreshStatus{
		Phase:          phase,
		Pending:        state.pending,
		Retryable:      phase == channelRefreshPhaseFailed,
		Failure:        failure,
		Request:        request,
		AttemptRequest: attemptRequest,
		WaitingIdle:    phase == channelRefreshPhasePending && !s.channelRefreshReady(),
	}
}

func (s *appState) postChannelRefreshUI(fn func()) {
	if s.channelRefresh.post != nil {
		s.channelRefresh.post(fn)
		return
	}
	s.postUI(fn)
}

func (s *appState) channelRefreshReady() bool {
	return !s.closed.Load() &&
		s.pi != nil &&
		s.piConn == piConnected &&
		!s.piConnecting &&
		!s.runActive() &&
		!s.piSessionBusy &&
		!s.thinkingCommitInFlight &&
		!s.compacting &&
		!s.retrying &&
		!s.agentWillRetry &&
		!s.runningSendPending &&
		!s.queueClearPending &&
		s.queueSteeringCount == 0 &&
		s.queueFollowUpCount == 0 &&
		!s.sessionNamingInProgress &&
		!s.sessionNamingCommitInFlight &&
		!s.sessionRenameInFlight &&
		!s.sessionRenameEditing &&
		!s.modelStateUnconfirmed &&
		strings.TrimSpace(s.chatView.thinkingPending) == ""
}

func (s *appState) captureChannelRefreshSnapshot(oldClient *piClient, epoch uint64) channelRefreshSnapshot {
	return channelRefreshSnapshot{
		oldClient:       oldClient,
		epoch:           epoch,
		path:            strings.TrimSpace(s.piPath),
		cwd:             strings.TrimSpace(s.piCwd),
		version:         s.piVersion,
		sessionFile:     s.piSessionFile,
		sessionID:       s.piSessionID,
		sessionName:     s.piSessionName,
		provider:        s.piModelProvider,
		modelID:         s.piModelID,
		modelName:       s.piModelName,
		thinking:        s.piThinkingLevel,
		hadConversation: len(s.chat) > 0,
		request:         s.channelRefresh.attemptRequest,
	}
}

func runChannelRefreshAttempt(snapshot channelRefreshSnapshot,
	launch channelRefreshLaunchFunc,
	events func(piEvent),
) channelRefreshOutcome {
	fail := func(failure ChannelRefreshFailure, err error) channelRefreshOutcome {
		return channelRefreshOutcome{failure: failure, err: err}
	}
	if launch == nil {
		launch = defaultChannelRefreshLaunch
	}
	client, err := launch(snapshot.path, snapshot.cwd, events)
	if err != nil {
		return fail(channelRefreshFailureLaunch, err)
	}
	closeClient := true
	defer func() {
		if closeClient {
			client.Close()
		}
	}()

	initial, err := channelRefreshGetState(client)
	if err != nil {
		return fail(channelRefreshFailureLaunch, err)
	}
	_ = initial // A fresh process has no inherited model streaming state.

	if strings.TrimSpace(snapshot.sessionFile) != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		cancelled, switchErr := client.SwitchSession(ctx, snapshot.sessionFile)
		cancel()
		if switchErr != nil {
			return fail(channelRefreshFailureSessionUnavailable, switchErr)
		}
		if cancelled {
			return fail(channelRefreshFailureSessionUnavailable,
				errors.New("session restore was cancelled"))
		}
	} else if strings.TrimSpace(snapshot.sessionID) != "" || snapshot.hadConversation {
		return fail(channelRefreshFailureSessionUnavailable,
			errors.New("current session has no persistent session file"))
	}

	if strings.TrimSpace(snapshot.provider) != "" || strings.TrimSpace(snapshot.modelID) != "" {
		if strings.TrimSpace(snapshot.provider) == "" || strings.TrimSpace(snapshot.modelID) == "" {
			return fail(channelRefreshFailureModelUnavailable,
				errors.New("current model is incomplete"))
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		_, setErr := client.SetModel(ctx, snapshot.provider, snapshot.modelID)
		cancel()
		if setErr != nil {
			return fail(channelRefreshFailureModelUnavailable, setErr)
		}
	}

	if strings.TrimSpace(snapshot.thinking) != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		setErr := client.SetThinkingLevel(ctx, snapshot.thinking)
		cancel()
		if setErr != nil {
			return fail(channelRefreshFailureThinkingUnavailable, setErr)
		}
	}

	state, err := channelRefreshGetState(client)
	if err != nil {
		return fail(channelRefreshFailureReadback, err)
	}
	if failure := channelRefreshReadbackFailure(snapshot, state); failure != channelRefreshFailureNone {
		return fail(failure, errors.New("restored Pi state did not match the saved session"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	models, modelsErr := client.GetAvailableModels(ctx)
	cancel()
	if modelsErr != nil {
		return fail(channelRefreshFailureModelUnavailable, modelsErr)
	}
	if snapshot.provider != "" && snapshot.modelID != "" &&
		!channelRefreshHasModel(models, snapshot.provider, snapshot.modelID) {
		return fail(channelRefreshFailureModelUnavailable,
			errors.New("restored model is absent from the new Pi directory"))
	}

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	levels, levelsErr := client.GetAvailableThinkingLevels(ctx)
	cancel()
	if levelsErr != nil {
		levels = nil
	}

	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Second)
	rawMessages, messagesErr := client.GetMessages(ctx)
	cancel()
	if messagesErr != nil {
		return fail(channelRefreshFailureTranscript, messagesErr)
	}
	chat, chatErr := rebuildChatFromPiMessages(rawMessages)
	if chatErr != nil {
		return fail(channelRefreshFailureTranscript, chatErr)
	}
	if snapshot.hadConversation && !channelRefreshChatHasContent(chat) {
		return fail(channelRefreshFailureTranscript,
			errors.New("saved conversation restored as empty"))
	}

	closeClient = false
	return channelRefreshOutcome{
		client: client,
		state:  state,
		models: clonePiModels(models),
		levels: append([]string(nil), levels...),
		chat:   chat,
	}
}

func channelRefreshGetState(client channelRefreshClient) (*piSessionState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return client.GetState(ctx)
}

func channelRefreshReadbackFailure(snapshot channelRefreshSnapshot, state *piSessionState) ChannelRefreshFailure {
	if state == nil {
		return channelRefreshFailureReadback
	}
	expectedPath := strings.TrimSpace(snapshot.sessionFile)
	expectedID := strings.TrimSpace(snapshot.sessionID)
	if (expectedPath != "" || expectedID != "") &&
		!samePiSession(expectedPath, expectedID, state.SessionFile, state.SessionID) {
		return channelRefreshFailureSessionUnavailable
	}
	if snapshot.provider != "" || snapshot.modelID != "" {
		if state.Model.Provider != snapshot.provider || state.Model.ID != snapshot.modelID {
			return channelRefreshFailureModelUnavailable
		}
	}
	if snapshot.thinking != "" && state.ThinkingLevel != snapshot.thinking {
		return channelRefreshFailureThinkingUnavailable
	}
	return channelRefreshFailureNone
}

func channelRefreshHasModel(models []piModel, provider, id string) bool {
	for _, model := range models {
		if model.Provider == provider && model.ID == id {
			return true
		}
	}
	return false
}

func channelRefreshChatHasContent(chat []chatMessage) bool {
	for _, message := range chat {
		for _, block := range message.blocks {
			if strings.TrimSpace(block.text) != "" {
				return true
			}
		}
	}
	return false
}

func (s *appState) applyChannelRefreshOutcome(snapshot channelRefreshSnapshot, outcome channelRefreshOutcome) bool {
	state := &s.channelRefresh
	if !state.inFlight || state.attemptRequest != snapshot.request {
		if outcome.client != nil {
			outcome.client.Close()
		}
		return false
	}
	state.inFlight = false
	if s.closed.Load() {
		if outcome.client != nil {
			outcome.client.Close()
		}
		return false
	}

	if outcome.failure != channelRefreshFailureNone {
		if outcome.eventDeny != nil {
			outcome.eventDeny.Store(true)
		}
		if outcome.client != nil {
			outcome.client.Close()
		}
		state.pending = false
		state.failure = outcome.failure
		if state.failure == channelRefreshFailureNone {
			state.failure = channelRefreshFailureReadback
		}
		state.phase = channelRefreshPhaseFailed
		return false
	}

	if outcome.client == nil || outcome.state == nil {
		if outcome.eventDeny != nil {
			outcome.eventDeny.Store(true)
		}
		state.pending = false
		state.failure = channelRefreshFailureReadback
		state.phase = channelRefreshPhaseFailed
		return false
	}
	if !s.channelRefreshSnapshotCurrent(snapshot) {
		if outcome.eventDeny != nil {
			outcome.eventDeny.Store(true)
		}
		outcome.client.Close()
		state.pending = true
		state.phase = channelRefreshPhasePending
		return true
	}
	if !s.commitChannelRefresh(snapshot, outcome) {
		if outcome.eventDeny != nil {
			outcome.eventDeny.Store(true)
		}
		state.pending = false
		state.failure = channelRefreshFailureReadback
		state.phase = channelRefreshPhaseFailed
		return false
	}

	if state.request != snapshot.request {
		state.pending = true
		state.phase = channelRefreshPhasePending
		return true
	}
	s.pluginPiRefreshRequired = false
	state.pending = false
	state.failure = channelRefreshFailureNone
	state.phase = channelRefreshPhaseSucceeded
	return false
}

func (s *appState) channelRefreshSnapshotCurrent(snapshot channelRefreshSnapshot) bool {
	if s.closed.Load() ||
		s.pi != snapshot.oldClient ||
		s.piEpoch != snapshot.epoch ||
		!s.channelRefreshReady() {
		return false
	}
	if snapshot.sessionFile != "" || snapshot.sessionID != "" {
		if !samePiSession(snapshot.sessionFile, snapshot.sessionID, s.piSessionFile, s.piSessionID) {
			return false
		}
	}
	if snapshot.provider != s.piModelProvider || snapshot.modelID != s.piModelID ||
		snapshot.thinking != s.piThinkingLevel {
		return false
	}
	return true
}

func (s *appState) commitChannelRefresh(snapshot channelRefreshSnapshot, outcome channelRefreshOutcome) bool {
	newClient := outcome.client.rawClient()
	if newClient == nil {
		if outcome.client != nil {
			outcome.client.Close()
		}
		return false
	}
	oldClient := s.pi
	newEpoch := s.piEpoch + 1
	if outcome.eventEpoch != nil {
		outcome.eventEpoch.Store(newEpoch)
	}
	draft := s.composerDraft
	tuples := s.modelTuples

	s.invalidateSessionNaming()
	s.pi = newClient
	s.piEpoch = newEpoch
	s.sessionGen++
	s.piConnectGeneration++
	s.piConn = piConnected
	s.piConnecting = false
	s.piPath = snapshot.path
	s.piCwd = snapshot.cwd
	s.piVersion = snapshot.version
	s.modelTuples = tuples
	s.piModelProvider = snapshot.provider
	s.piModelID = snapshot.modelID
	s.piModelName = snapshot.modelName
	s.piThinkingLevel = snapshot.thinking
	s.applyPiSessionState(outcome.state)
	s.piModels = clonePiModels(outcome.models)
	if len(outcome.levels) > 0 {
		s.piThinkingLevels = append([]string(nil), outcome.levels...)
		s.piThinkingLevelsKnown = true
	} else {
		s.piThinkingLevels = nil
		s.piThinkingLevelsKnown = false
	}
	s.chatView = chatPresentation{}
	s.chat = outcome.chat
	s.localSessionTitle = firstUserSessionTitle(s.chat)
	s.composerDraft = draft
	s.sessionRenameEditing = false
	s.sessionRenameDraft = ""
	s.run = runIdle
	s.activeRequest = ""
	s.activePrompt = ""
	s.resetRuntimeDiagnostics()
	s.resetFollowUpSuggestion()
	s.piError = ""
	s.piErrorCode = ""
	s.piErrorDetailsOpen = false
	s.modelStateUnconfirmed = false
	s.beginPiHUD(snapshot.cwd, outcome.state)
	s.refreshPiSessions()
	if oldClient != nil && oldClient != newClient {
		oldClient.Close()
	}
	return true
}
