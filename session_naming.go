package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	localSessionTitleLimit = 40
	autoSessionTitleLimit  = 80
	namingTranscriptLimit  = 2000
	namingContextLimit     = 6
	autoNamingSessionLimit = 256
)

type namingTurn struct {
	Role string
	Text string
}

type sessionNameRPC interface {
	GetState(context.Context) (*piSessionState, error)
	SetSessionName(context.Context, string) error
}

type sessionNameCommitOwner string

const (
	sessionNameCommitNone   sessionNameCommitOwner = ""
	sessionNameCommitAuto   sessionNameCommitOwner = "auto"
	sessionNameCommitManual sessionNameCommitOwner = "manual"
)

func localTemporarySessionTitle(value string) string {
	value = singleLineText(value)
	runes := []rune(value)
	if len(runes) <= localSessionTitleLimit {
		return value
	}
	return string(runes[:localSessionTitleLimit]) + "…"
}

func normalizedSessionName(value string) string {
	value = singleLineText(value)
	return string([]rune(value)[:min(utf8.RuneCountInString(value), autoSessionTitleLimit)])
}

func firstUserSessionTitle(chat []chatMessage) string {
	for i := range chat {
		if chat[i].role != chatUser {
			continue
		}
		if value := messageText(&chat[i]); strings.TrimSpace(value) != "" {
			return localTemporarySessionTitle(value)
		}
	}
	return ""
}

func namingTurnsFromChat(chat []chatMessage, limit int) []namingTurn {
	if limit <= 0 {
		return nil
	}
	turns := make([]namingTurn, 0, limit)
	for i := len(chat) - 1; i >= 0 && len(turns) < limit; i-- {
		role := ""
		switch chat[i].role {
		case chatUser:
			role = "User"
		case chatAssistant:
			role = "Assistant"
		default:
			continue
		}
		value := strings.TrimSpace(messageText(&chat[i]))
		if value == "" {
			continue
		}
		turns = append(turns, namingTurn{
			Role: role,
			Text: string([]rune(value)[:min(utf8.RuneCountInString(value), namingTranscriptLimit)]),
		})
	}
	for left, right := 0, len(turns)-1; left < right; left, right = left+1, right-1 {
		turns[left], turns[right] = turns[right], turns[left]
	}
	return turns
}

func buildSessionNamingPrompt(turns []namingTurn) string {
	var prompt strings.Builder
	prompt.WriteString("Return only a concise title for this conversation in its own language. ")
	prompt.WriteString("Use at most 12 Chinese characters or 8 English words. ")
	prompt.WriteString("Do not use quotes, Markdown, or punctuation at the end.\n\n")
	for _, turn := range turns {
		role := strings.TrimSpace(turn.Role)
		text := strings.TrimSpace(turn.Text)
		if role == "" || text == "" {
			continue
		}
		text = truncateSessionNamingText(text, namingTranscriptLimit)
		fmt.Fprintf(&prompt, "%s: %s\n", role, text)
	}
	return strings.TrimSpace(prompt.String())
}

func truncateSessionNamingText(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func sanitizeSessionName(raw string) string {
	line := ""
	for _, candidate := range strings.Split(raw, "\n") {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			line = candidate
			break
		}
	}
	line = strings.TrimLeftFunc(line, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("#*_`~>-", r)
	})
	line = strings.TrimLeft(line, "\"'`*_~“”‘’「」《》")
	lower := strings.ToLower(line)
	switch {
	case strings.HasPrefix(lower, "title:"):
		line = strings.TrimSpace(line[len("title:"):])
	case strings.HasPrefix(line, "标题："):
		line = strings.TrimSpace(line[len("标题："):])
	case strings.HasPrefix(line, "标题:"):
		line = strings.TrimSpace(line[len("标题:"):])
	}
	line = strings.TrimLeft(line, "\"'`*_~“”‘’「」《》")
	line = strings.Join(strings.Fields(line), " ")
	line = strings.TrimRightFunc(line, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("。．.，,：:；;！!？?", r)
	})
	line = strings.TrimRightFunc(line, func(r rune) bool {
		return strings.ContainsRune("\"'`*_~“”‘’「」《》", r)
	})
	line = strings.TrimRightFunc(line, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("。．.，,：:；;！!？?", r)
	})
	if line == "" {
		return ""
	}
	if containsCJK(line) {
		return string([]rune(line)[:min(utf8.RuneCountInString(line), 12)])
	}
	words := strings.Fields(line)
	if len(words) > 8 {
		words = words[:8]
	}
	line = strings.Join(words, " ")
	return string([]rune(line)[:min(utf8.RuneCountInString(line), autoSessionTitleLimit)])
}

func containsCJK(value string) bool {
	for _, r := range value {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			return true
		}
	}
	return false
}

func singleLineText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func runSessionNamingCLI(ctx context.Context, path, cwd, model string, turns []namingTurn) (string, error) {
	prompt := buildSessionNamingPrompt(turns)
	if prompt == "" {
		return "", fmt.Errorf("session naming prompt is empty")
	}
	args := []string{
		"--print",
		"--no-session",
		"--no-tools",
		"--no-extensions",
		"--no-skills",
		"--no-context-files",
		"--no-prompt-templates",
		"--no-mcp",
		"--thinking", "off",
	}
	if strings.TrimSpace(model) != "" {
		args = append(args, "--model", model)
	}
	args = append(args, prompt)
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = cwd
	cmd.Env = piProcessEnv(path, os.Environ())
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return sanitizeSessionName(string(out)), nil
}

func (s *appState) activePiSessionKey() string {
	if strings.TrimSpace(s.piSessionFile) != "" {
		return canonicalPiPath(s.piSessionFile)
	}
	return strings.TrimSpace(s.piSessionID)
}

func (s *appState) invalidateSessionNaming() {
	s.sessionNamingGen++
	if s.sessionNamingCancel != nil {
		s.sessionNamingCancel()
		s.sessionNamingCancel = nil
	}
	s.sessionNamingInProgress = false
}

func (s *appState) maybeStartAutoSessionNaming(prompt, reply string) {
	key := s.activePiSessionKey()
	if key == "" || strings.TrimSpace(s.piSessionName) != "" {
		return
	}
	if s.autoNamingAttemptedBySession == nil {
		s.autoNamingAttemptedBySession = make(map[string]bool)
	}
	if s.autoNamingAttemptedBySession[key] {
		return
	}
	if len(s.autoNamingAttemptedBySession) >= autoNamingSessionLimit {
		s.autoNamingAttemptedBySession = make(map[string]bool)
	}
	s.autoNamingAttemptedBySession[key] = true
	turns := []namingTurn{{Role: "User", Text: prompt}}
	if strings.TrimSpace(reply) != "" {
		turns = append(turns, namingTurn{Role: "Assistant", Text: reply})
	}
	s.startSessionNaming(turns, false)
}

func (s *appState) startContextSessionNaming() {
	if s.piSessionBusy || s.sessionNamingCommitInFlight || s.sessionRenameInFlight {
		return
	}
	turns := namingTurnsFromChat(s.chat, namingContextLimit)
	if len(turns) == 0 {
		return
	}
	s.sessionRenameEditing = false
	s.sessionRenameDraft = ""
	s.startSessionNaming(turns, true)
}

func (s *appState) startSessionNaming(turns []namingTurn, overwrite bool) {
	if s.piSessionBusy || s.sessionNamingCommitInFlight || s.sessionRenameInFlight ||
		s.pi == nil || s.piConn != piConnected || len(turns) == 0 {
		return
	}
	client := s.pi
	epoch := s.piEpoch
	sessionPath := s.piSessionFile
	sessionID := s.piSessionID
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	path := s.piPath
	if strings.TrimSpace(path) == "" {
		path = client.path
	}
	cwd := s.piCwd
	if strings.TrimSpace(cwd) == "" {
		cwd = client.cwd
	}
	model := ""
	if strings.TrimSpace(s.piModelProvider) != "" && strings.TrimSpace(s.piModelID) != "" {
		model = s.piModelProvider + "/" + s.piModelID
	}

	s.invalidateSessionNaming()
	gen := s.sessionNamingGen
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	s.sessionNamingCancel = cancel
	s.sessionNamingInProgress = true
	go func() {
		defer cancel()
		name, err := runSessionNamingCLI(ctx, path, cwd, model, turns)
		s.postUI(func() {
			if !s.sessionNamingResultCurrent(gen, epoch, client, sessionPath, sessionID) {
				return
			}
			s.sessionNamingCancel = nil
			if err != nil || name == "" || (!overwrite && strings.TrimSpace(s.piSessionName) != "") {
				s.sessionNamingInProgress = false
				return
			}
			token := s.beginSessionNameCommit(sessionNameCommitAuto, gen)
			s.startSessionNameCommit(client, epoch, gen, sessionNameCommitAuto, token,
				name, sessionPath, sessionID, overwrite, false)
		})
	}()
}

func (s *appState) beginSessionRename() {
	if s.piSessionBusy || s.sessionNamingCommitInFlight || s.sessionRenameInFlight ||
		s.piSessionID == "" || s.piConn != piConnected {
		return
	}
	s.invalidateSessionNaming()
	s.sessionRenameEditing = true
	s.sessionRenameDraft = s.sessionDisplayTitle()
}

func (s *appState) cancelSessionRename() {
	s.sessionRenameEditing = false
	s.sessionRenameDraft = ""
}

func (s *appState) saveSessionRename() bool {
	if s.piSessionBusy || s.sessionNamingCommitInFlight || s.sessionRenameInFlight {
		return false
	}
	name := normalizedSessionName(s.sessionRenameDraft)
	if name == "" {
		return false
	}
	if s.pi == nil || s.piConn != piConnected || s.piSessionID == "" {
		return false
	}
	client := s.pi
	epoch := s.piEpoch
	s.invalidateSessionNaming()
	gen := s.sessionNamingGen
	s.sessionRenameEditing = false
	s.sessionRenameDraft = ""
	token := s.beginSessionNameCommit(sessionNameCommitManual, gen)
	s.startSessionNameCommit(client, epoch, gen, sessionNameCommitManual, token,
		name, s.piSessionFile, s.piSessionID, true, true)
	return true
}

func (s *appState) beginSessionNameCommit(owner sessionNameCommitOwner, gen uint64) uint64 {
	s.sessionNamingCommitSeq++
	token := s.sessionNamingCommitSeq
	s.sessionNamingCommitOwner = owner
	s.sessionNamingCommitInFlight = owner == sessionNameCommitAuto
	s.sessionRenameInFlight = owner == sessionNameCommitManual
	if gen == s.sessionNamingGen {
		s.sessionNamingInProgress = owner == sessionNameCommitAuto
	}
	return token
}

func (s *appState) finishSessionNameCommit(token uint64, owner sessionNameCommitOwner) {
	if token == 0 || token != s.sessionNamingCommitSeq || owner != s.sessionNamingCommitOwner {
		return
	}
	switch owner {
	case sessionNameCommitAuto:
		s.sessionNamingCommitInFlight = false
		s.sessionNamingInProgress = false
	case sessionNameCommitManual:
		s.sessionRenameInFlight = false
	}
	s.sessionNamingCommitOwner = sessionNameCommitNone
}

func (s *appState) sessionNamingResultCurrent(
	gen, epoch uint64,
	client *piClient,
	sessionPath, sessionID string,
) bool {
	return !s.piSessionBusy &&
		gen == s.sessionNamingGen &&
		epoch == s.piEpoch &&
		s.pi == client &&
		sessionNameTargetMatches(sessionPath, sessionID, s.piSessionFile, s.piSessionID)
}

func sessionNameTargetMatches(expectedPath, expectedID, actualPath, actualID string) bool {
	expectedID = strings.TrimSpace(expectedID)
	if expectedID == "" || expectedID != strings.TrimSpace(actualID) {
		return false
	}
	return strings.TrimSpace(expectedPath) == "" ||
		strings.TrimSpace(actualPath) == "" ||
		canonicalPiPath(expectedPath) == canonicalPiPath(actualPath)
}

func writeSessionName(
	client sessionNameRPC,
	name, expectedPath, expectedID string,
	allowOverwrite bool,
) (*piSessionState, error, error) {
	var state *piSessionState
	var stateErr error
	var err error
	preflightCtx, preflightCancel := context.WithTimeout(context.Background(), 5*time.Second)
	preflight, preflightErr := client.GetState(preflightCtx)
	preflightCancel()
	if preflightErr != nil {
		err = preflightErr
	} else if !sessionNameTargetMatches(expectedPath, expectedID,
		preflight.SessionFile, preflight.SessionID) {
		err = errors.New("Pi session changed before applying name")
	} else if !allowOverwrite && strings.TrimSpace(preflight.SessionName) != "" {
		err = errors.New("Pi session already has a name")
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = client.SetSessionName(ctx, name)
		cancel()
	}
	if err == nil {
		stateCtx, stateCancel := context.WithTimeout(context.Background(), 5*time.Second)
		state, stateErr = client.GetState(stateCtx)
		stateCancel()
	}
	return state, stateErr, err
}

func (s *appState) startSessionNameCommit(
	client *piClient,
	epoch, gen uint64,
	owner sessionNameCommitOwner,
	token uint64,
	name, expectedPath, expectedID string,
	allowOverwrite, showFailure bool,
) bool {
	if token == 0 || token != s.sessionNamingCommitSeq || owner != s.sessionNamingCommitOwner {
		return false
	}
	if s.piSessionBusy {
		s.finishSessionNameCommit(token, owner)
		return false
	}
	go s.runSessionNameCommit(client, epoch, gen, owner, token,
		name, expectedPath, expectedID, allowOverwrite, showFailure)
	return true
}

func (s *appState) runSessionNameCommit(
	client *piClient,
	epoch, gen uint64,
	owner sessionNameCommitOwner,
	token uint64,
	name, expectedPath, expectedID string,
	allowOverwrite, showFailure bool,
) {
	state, stateErr, err := writeSessionName(
		client, name, expectedPath, expectedID, allowOverwrite,
	)
	s.postUI(func() {
		s.finishSessionNameCommit(token, owner)
		if gen != s.sessionNamingGen || epoch != s.piEpoch || s.pi != client ||
			!sessionNameTargetMatches(expectedPath, expectedID, s.piSessionFile, s.piSessionID) {
			return
		}
		s.sessionNamingInProgress = false
		if err != nil {
			if showFailure {
				s.showNotice(textArgs("noticeSessionRenameFailed", map[string]any{
					"error": uiErrorText(err, text("errorPiUnknown")),
				}))
			}
			return
		}
		if stateErr == nil {
			s.applyPiSessionState(state)
		} else {
			s.piSessionName = name
			s.localSessionTitle = ""
			for i := range s.piSessions {
				if samePiSession(s.piSessionFile, s.piSessionID,
					s.piSessions[i].Path, s.piSessions[i].ID) {
					s.piSessions[i].Name = name
					s.piSessions[i].Title = name
				}
			}
		}
		s.refreshPiSessions()
	})
}

func (s *appState) sessionDisplayTitle() string {
	if name := strings.TrimSpace(s.piSessionName); name != "" {
		return name
	}
	if title := strings.TrimSpace(s.localSessionTitle); title != "" {
		return title
	}
	return text("titleNewChat")
}
