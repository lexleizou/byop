package main

import (
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	sessionStatePinned   = "pinned"
	sessionStateLater    = "later"
	sessionStateArchived = "archived"
)

type sessionOrgEntry struct {
	State      string `json:"state"`
	Order      int    `json:"order,omitempty"`
	LaterUntil string `json:"laterUntil,omitempty"`
	UpdatedAt  string `json:"updatedAt"`
}

type sessionGroups struct {
	pinned   []piSessionSummary
	sessions []piSessionSummary
	later    []piSessionSummary
	archived []piSessionSummary
	due      map[string]bool
}

type sessionUndoState struct {
	title      string
	cwd        string
	key        string
	snapshot   map[string]sessionOrgEntry
	generation uint64
	timer      *time.Timer
}

func sessionOrgKey(summary piSessionSummary) string {
	if path := strings.TrimSpace(summary.Path); path != "" {
		return filepath.Clean(path)
	}
	if id := strings.TrimSpace(summary.ID); id != "" {
		return "id:" + id
	}
	return ""
}

func normalizeSessionOrgEntry(entry sessionOrgEntry) (sessionOrgEntry, bool) {
	entry.State = strings.ToLower(strings.TrimSpace(entry.State))
	switch entry.State {
	case sessionStatePinned:
		entry.LaterUntil = ""
	case sessionStateLater:
		entry.Order = 0
		if _, err := time.Parse(time.RFC3339, entry.LaterUntil); err != nil {
			entry.LaterUntil = ""
		}
	case sessionStateArchived:
		entry.Order = 0
		entry.LaterUntil = ""
	default:
		return sessionOrgEntry{}, false
	}
	return entry, true
}

func partitionSessions(sessions []piSessionSummary, entries map[string]sessionOrgEntry, now time.Time) sessionGroups {
	groups := sessionGroups{due: make(map[string]bool)}
	for _, summary := range sessions {
		key := sessionOrgKey(summary)
		entry, ok := entries[key]
		if !ok {
			groups.sessions = append(groups.sessions, summary)
			continue
		}
		entry, ok = normalizeSessionOrgEntry(entry)
		if !ok {
			groups.sessions = append(groups.sessions, summary)
			continue
		}
		switch entry.State {
		case sessionStatePinned:
			groups.pinned = append(groups.pinned, summary)
		case sessionStateArchived:
			groups.archived = append(groups.archived, summary)
		case sessionStateLater:
			if entry.LaterUntil == "" {
				groups.later = append(groups.later, summary)
			} else {
				until, err := time.Parse(time.RFC3339, entry.LaterUntil)
				if err == nil && now.Before(until) {
					groups.later = append(groups.later, summary)
				} else {
					groups.sessions = append(groups.sessions, summary)
					groups.due[key] = true
				}
			}
		}
	}
	sort.SliceStable(groups.pinned, func(i, j int) bool {
		left, right := entries[sessionOrgKey(groups.pinned[i])], entries[sessionOrgKey(groups.pinned[j])]
		if left.Order != right.Order {
			return left.Order < right.Order
		}
		return groups.pinned[i].Updated.After(groups.pinned[j].Updated)
	})
	sort.SliceStable(groups.sessions, func(i, j int) bool {
		return groups.sessions[i].Updated.After(groups.sessions[j].Updated)
	})
	sort.SliceStable(groups.later, func(i, j int) bool {
		left, _ := time.Parse(time.RFC3339, entries[sessionOrgKey(groups.later[i])].LaterUntil)
		right, _ := time.Parse(time.RFC3339, entries[sessionOrgKey(groups.later[j])].LaterUntil)
		if left.Equal(right) {
			return groups.later[i].Updated.After(groups.later[j].Updated)
		}
		return left.Before(right)
	})
	sort.SliceStable(groups.archived, func(i, j int) bool {
		return groups.archived[i].Updated.After(groups.archived[j].Updated)
	})
	return groups
}

func cloneSessionOrgEntries(entries map[string]sessionOrgEntry) map[string]sessionOrgEntry {
	cloned := make(map[string]sessionOrgEntry, len(entries))
	for key, entry := range entries {
		cloned[key] = entry
	}
	return cloned
}

func replaceSessionOrgEntries(target, source map[string]sessionOrgEntry) {
	for key := range target {
		delete(target, key)
	}
	for key, entry := range source {
		target[key] = entry
	}
}

func setSessionOrgState(entries map[string]sessionOrgEntry, summary piSessionSummary, state string, laterUntil string, now time.Time) {
	key := sessionOrgKey(summary)
	if key == "" {
		return
	}
	if state == "" {
		delete(entries, key)
		return
	}
	entry, ok := normalizeSessionOrgEntry(sessionOrgEntry{
		State:      state,
		LaterUntil: laterUntil,
		UpdatedAt:  now.UTC().Format(time.RFC3339),
	})
	if !ok {
		delete(entries, key)
		return
	}
	if entry.State == sessionStatePinned {
		next := 0
		for _, candidate := range entries {
			if candidate.State == sessionStatePinned && candidate.Order >= next {
				next = candidate.Order + 1
			}
		}
		entry.Order = next
	}
	entries[key] = entry
}

func reorderPinnedSessions(entries map[string]sessionOrgEntry, sessions []piSessionSummary, draggedKey, beforeKey string, now time.Time) {
	if draggedKey == "" || draggedKey == beforeKey {
		return
	}
	keys := make([]string, 0, len(sessions))
	seen := make(map[string]bool, len(sessions))
	for _, summary := range sessions {
		key := sessionOrgKey(summary)
		entry, ok := entries[key]
		if key == "" || seen[key] || !ok || entry.State != sessionStatePinned {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		return entries[keys[i]].Order < entries[keys[j]].Order
	})
	from := -1
	for i, key := range keys {
		if key == draggedKey {
			from = i
			break
		}
	}
	if from >= 0 {
		keys = append(keys[:from], keys[from+1:]...)
	}
	to := len(keys)
	if beforeKey != "" {
		for i, key := range keys {
			if key == beforeKey {
				to = i
				break
			}
		}
	}
	keys = append(keys, "")
	copy(keys[to+1:], keys[to:])
	keys[to] = draggedKey
	stamp := now.UTC().Format(time.RFC3339)
	for order, key := range keys {
		entry := entries[key]
		entry.State = sessionStatePinned
		entry.Order = order
		entry.LaterUntil = ""
		entry.UpdatedAt = stamp
		entries[key] = entry
	}
}

func laterAtHour(now time.Time, nextDay bool) string {
	local := now.In(time.Local)
	day := local
	if nextDay {
		day = time.Date(local.Year(), local.Month(), local.Day()+1, 9, 0, 0, 0, local.Location())
		if !local.Before(day) {
			day = day.AddDate(0, 0, 1)
		}
	} else {
		day = local.Add(time.Hour)
	}
	return day.UTC().Format(time.RFC3339)
}

func laterNextMonday(now time.Time) string {
	local := now.In(time.Local)
	days := (8 - int(local.Weekday())) % 7
	if days == 0 {
		days = 7
	}
	monday := time.Date(local.Year(), local.Month(), local.Day()+days, 9, 0, 0, 0, local.Location())
	return monday.UTC().Format(time.RFC3339)
}

func (s *appState) sessionOrganizationEntries() map[string]sessionOrgEntry {
	cwd, err := s.sessionWorkspaceCwd()
	if err != nil {
		return map[string]sessionOrgEntry{}
	}
	cwd = canonicalPiPath(cwd)
	if s.preferences.SessionOrganization == nil {
		s.preferences.SessionOrganization = make(map[string]map[string]sessionOrgEntry)
	}
	entries := s.preferences.SessionOrganization[cwd]
	if entries == nil {
		entries = make(map[string]sessionOrgEntry)
		s.preferences.SessionOrganization[cwd] = entries
	}
	return entries
}

func (s *appState) refreshSessionOrganization(now time.Time) {
	cwd, err := s.sessionWorkspaceCwd()
	if err != nil {
		return
	}
	cwd = canonicalPiPath(cwd)
	entries := s.preferences.SessionOrganization[cwd]
	if entries == nil {
		entries = make(map[string]sessionOrgEntry)
	}
	// Painting and session refreshes are read-only. Pi exposes only a capped,
	// asynchronous recent-session list, so an empty or partial result cannot
	// prove that an organization record is obsolete.
	s.piSessionsOrgWorkspace = cwd
	s.piSessionsGroups = partitionSessions(s.piSessions, entries, now)
}

func (s *appState) startSessionOrganizationTimer() {
	if s.closed.Load() {
		return
	}
	if s.sessionLaterTimer != nil {
		s.sessionLaterTimer.Stop()
	}
	s.sessionLaterTimer = time.AfterFunc(time.Minute, func() {
		s.postUI(func() {
			s.refreshSessionOrganization(time.Now())
			s.startSessionOrganizationTimer()
		})
	})
}

func (s *appState) setSessionOrganization(summary piSessionSummary, state, laterUntil string, undo bool) bool {
	entries := s.sessionOrganizationEntries()
	key := sessionOrgKey(summary)
	if key == "" {
		return false
	}
	snapshot := cloneSessionOrgEntries(entries)
	now := time.Now()
	if state == "" {
		delete(entries, key)
	} else {
		setSessionOrgState(entries, summary, state, laterUntil, now)
	}
	if undo && (state == sessionStateArchived || state == sessionStateLater) {
		s.recordSessionUndo(untitledSessionLabel(summary.Title), key, snapshot)
	}
	if err := s.persistPreferences(); err != nil {
		replaceSessionOrgEntries(entries, snapshot)
		s.showNotice(text("noticeSessionOrganizationSave"))
		return false
	}
	s.refreshSessionOrganization(now)
	if undo && (state == sessionStateArchived || state == sessionStateLater) {
		s.showSessionUndoNotice(state, untitledSessionLabel(summary.Title))
	}
	return true
}

func (s *appState) showSessionUndoNotice(state, title string) {
	messageKey := "sessionArchivedNotice"
	if state == sessionStateLater {
		messageKey = "sessionMovedLaterNotice"
	}
	s.showNoticeAction(
		textArgs(messageKey, map[string]any{"title": title}),
		text("sessionUndo"),
		s.undoSessionOrganization,
	)
}

func (s *appState) recordSessionUndo(title, key string, snapshot map[string]sessionOrgEntry) {
	if s.sessionUndo != nil && s.sessionUndo.timer != nil {
		s.sessionUndo.timer.Stop()
	}
	cwd, _ := s.sessionWorkspaceCwd()
	cwd = canonicalPiPathIfPresent(cwd)
	undo := &sessionUndoState{
		title:      title,
		cwd:        cwd,
		key:        key,
		snapshot:   cloneSessionOrgEntries(snapshot),
		generation: s.sessionUndoGeneration() + 1,
	}
	s.sessionUndo = undo
	undo.timer = time.AfterFunc(8*time.Second, func() {
		s.postUI(func() {
			if s.sessionUndo == undo && s.sessionUndo.generation == undo.generation {
				s.sessionUndo = nil
				s.notice = ""
				s.noticeActionLabel = ""
				s.noticeAction = nil
			}
		})
	})
}

func (s *appState) sessionUndoGeneration() uint64 {
	if s.sessionUndo == nil {
		return 0
	}
	return s.sessionUndo.generation
}

func (s *appState) undoSessionOrganization() {
	undo := s.sessionUndo
	if undo == nil {
		return
	}
	cwd, err := s.sessionWorkspaceCwd()
	if err != nil || canonicalPiPath(cwd) != undo.cwd {
		if undo.timer != nil {
			undo.timer.Stop()
		}
		s.sessionUndo = nil
		s.notice = ""
		s.noticeActionLabel = ""
		s.noticeAction = nil
		return
	}
	entries := s.sessionOrganizationEntries()
	if undo.key == "" {
		replaceSessionOrgEntries(entries, undo.snapshot)
	} else if previous, ok := undo.snapshot[undo.key]; ok {
		entries[undo.key] = previous
	} else {
		delete(entries, undo.key)
	}
	if undo.timer != nil {
		undo.timer.Stop()
	}
	s.sessionUndo = nil
	if err := s.persistPreferences(); err != nil {
		s.showNotice(text("noticeSessionOrganizationSave"))
		return
	}
	s.notice = ""
	s.noticeActionLabel = ""
	s.noticeAction = nil
	s.refreshSessionOrganization(time.Now())
}

func (s *appState) sessionOrganizationState(summary piSessionSummary) (sessionOrgEntry, bool) {
	entry, ok := s.sessionOrganizationEntries()[sessionOrgKey(summary)]
	if !ok {
		return sessionOrgEntry{}, false
	}
	entry, ok = normalizeSessionOrgEntry(entry)
	return entry, ok
}

func (s *appState) clearDueLaterSession(summary piSessionSummary, now time.Time) {
	entries := s.sessionOrganizationEntries()
	key := sessionOrgKey(summary)
	entry, ok := entries[key]
	if !ok || entry.State != sessionStateLater || entry.LaterUntil == "" {
		return
	}
	until, err := time.Parse(time.RFC3339, entry.LaterUntil)
	if err == nil && now.Before(until) {
		return
	}
	delete(entries, key)
	if err := s.persistPreferences(); err != nil {
		s.showNotice(text("noticeSessionOrganizationSave"))
		return
	}
	s.refreshSessionOrganization(now)
}

func (s *appState) activePiSessionSummary() (piSessionSummary, bool) {
	for _, summary := range s.piSessions {
		if s.isActivePiSession(summary) {
			return summary, true
		}
	}
	return piSessionSummary{}, false
}

func (s *appState) refreshActiveSessionOrganization(now time.Time) {
	summary, ok := s.activePiSessionSummary()
	if !ok {
		return
	}
	if entry, ok := s.sessionOrganizationState(summary); ok && entry.State == sessionStateArchived {
		s.showNoticeAction(
			textArgs("sessionArchivedNotice", map[string]any{"title": untitledSessionLabel(summary.Title)}),
			text("sessionRestore"),
			func() {
				s.setSessionOrganization(summary, "", "", false)
				s.notice = text("sessionRestored")
			},
		)
	}
}
