package main

import (
	"testing"
	"time"
)

func TestP9cPartitionSessionsAndExpiredLater(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	sessions := []piSessionSummary{
		{Path: "/tmp/pinned-b", ID: "pinned-b", Updated: now.Add(-2 * time.Hour)},
		{Path: "/tmp/pinned-a", ID: "pinned-a", Updated: now.Add(-time.Hour)},
		{Path: "/tmp/session", ID: "session", Updated: now.Add(-3 * time.Hour)},
		{Path: "/tmp/later", ID: "later", Updated: now.Add(-4 * time.Hour)},
		{Path: "/tmp/expired", ID: "expired", Updated: now.Add(-5 * time.Hour)},
		{Path: "/tmp/archived", ID: "archived", Updated: now.Add(-6 * time.Hour)},
	}
	entries := map[string]sessionOrgEntry{
		"/tmp/pinned-b": {State: sessionStatePinned, Order: 1},
		"/tmp/pinned-a": {State: sessionStatePinned, Order: 0},
		"/tmp/later":    {State: sessionStateLater, LaterUntil: now.Add(time.Hour).Format(time.RFC3339)},
		"/tmp/expired":  {State: sessionStateLater, LaterUntil: now.Add(-time.Minute).Format(time.RFC3339)},
		"/tmp/archived": {State: sessionStateArchived},
	}

	groups := partitionSessions(sessions, entries, now)
	if got := sessionIDs(groups.pinned); !equalStrings(got, []string{"pinned-a", "pinned-b"}) {
		t.Fatalf("pinned order = %v", got)
	}
	if got := sessionIDs(groups.sessions); !equalStrings(got, []string{"session", "expired"}) {
		t.Fatalf("sessions = %v", got)
	}
	if got := sessionIDs(groups.later); !equalStrings(got, []string{"later"}) {
		t.Fatalf("later = %v", got)
	}
	if got := sessionIDs(groups.archived); !equalStrings(got, []string{"archived"}) {
		t.Fatalf("archived = %v", got)
	}
	if !groups.due["/tmp/expired"] || groups.due["/tmp/later"] {
		t.Fatalf("due state = %#v", groups.due)
	}
}

func TestP9cPinnedReorderAndArchiveRestore(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	sessions := []piSessionSummary{
		{Path: "/tmp/a", ID: "a"},
		{Path: "/tmp/b", ID: "b"},
		{Path: "/tmp/c", ID: "c"},
	}
	entries := map[string]sessionOrgEntry{
		"/tmp/a": {State: sessionStatePinned, Order: 0},
		"/tmp/b": {State: sessionStatePinned, Order: 1},
		"/tmp/c": {State: sessionStatePinned, Order: 2},
	}
	reorderPinnedSessions(entries, sessions, "/tmp/c", "/tmp/a", now)
	groups := partitionSessions(sessions, entries, now)
	if got := sessionIDs(groups.pinned); !equalStrings(got, []string{"c", "a", "b"}) {
		t.Fatalf("reordered pinned = %v", got)
	}

	snapshot := cloneSessionOrgEntries(entries)
	setSessionOrgState(entries, sessions[0], sessionStateArchived, "", now)
	if entry := entries["/tmp/a"]; entry.State != sessionStateArchived {
		t.Fatalf("archive state = %#v", entry)
	}
	replaceSessionOrgEntries(entries, snapshot)
	if got := sessionIDs(partitionSessions(sessions, entries, now).pinned); !equalStrings(got, []string{"c", "a", "b"}) {
		t.Fatalf("undo pinned = %v", got)
	}

	setSessionOrgState(entries, sessions[0], "", "", now)
	if _, ok := entries["/tmp/a"]; ok {
		t.Fatal("restore to Sessions should remove the organization entry")
	}
}

func TestP9cRefreshWithEmptySessionsKeepsOrganization(t *testing.T) {
	cwd := t.TempDir()
	entries := map[string]sessionOrgEntry{
		"/tmp/pinned":  {State: sessionStatePinned, Order: 2},
		"/tmp/later":   {State: sessionStateLater, LaterUntil: time.Now().Add(time.Hour).Format(time.RFC3339)},
		"/tmp/archive": {State: sessionStateArchived},
	}
	state := &appState{
		piCwd:      cwd,
		piSessions: nil,
		preferences: byopPreferences{
			SessionOrganization: map[string]map[string]sessionOrgEntry{
				canonicalPiPath(cwd): entries,
			},
		},
	}
	state.refreshSessionOrganization(time.Now())
	if len(entries) != 3 || entries["/tmp/pinned"].State != sessionStatePinned ||
		entries["/tmp/later"].State != sessionStateLater || entries["/tmp/archive"].State != sessionStateArchived {
		t.Fatalf("empty session refresh changed organization: %#v", entries)
	}
}

func TestP9cExpiredLaterSurvivesRefreshUntilExplicitOpen(t *testing.T) {
	cwd := t.TempDir()
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	summary := piSessionSummary{
		Path:    "/tmp/expired",
		ID:      "expired",
		Updated: now.Add(-time.Hour),
	}
	entries := map[string]sessionOrgEntry{
		summary.Path: {
			State:      sessionStateLater,
			LaterUntil: now.Add(-time.Minute).Format(time.RFC3339),
		},
	}
	state := &appState{
		piCwd:      cwd,
		piSessions: []piSessionSummary{summary},
		preferences: byopPreferences{
			SessionOrganization: map[string]map[string]sessionOrgEntry{
				canonicalPiPath(cwd): entries,
			},
		},
	}

	state.refreshSessionOrganization(now)
	state.refreshActiveSessionOrganization(now)
	if entry, ok := entries[summary.Path]; !ok || entry.State != sessionStateLater {
		t.Fatalf("refresh removed expired Later entry: %#v", entries)
	}
	if !state.piSessionsGroups.due[summary.Path] {
		t.Fatal("expired Later session did not receive a due marker")
	}

	state.clearDueLaterSession(summary, now)
	if _, ok := entries[summary.Path]; ok {
		t.Fatalf("explicit open did not clear expired Later entry: %#v", entries)
	}
}

func TestP9cUndoRejectsDifferentWorkspace(t *testing.T) {
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	entriesB := map[string]sessionOrgEntry{
		"/tmp/b": {State: sessionStatePinned, Order: 0},
	}
	state := &appState{
		piCwd: workspaceB,
		preferences: byopPreferences{
			SessionOrganization: map[string]map[string]sessionOrgEntry{
				canonicalPiPath(workspaceA): {
					"/tmp/a": {State: sessionStateArchived},
				},
				canonicalPiPath(workspaceB): entriesB,
			},
		},
		sessionUndo: &sessionUndoState{
			cwd:      canonicalPiPath(workspaceA),
			key:      "/tmp/a",
			snapshot: map[string]sessionOrgEntry{},
		},
	}
	state.undoSessionOrganization()
	if entry := entriesB["/tmp/b"]; entry.State != sessionStatePinned || entry.Order != 0 {
		t.Fatalf("workspace B was modified: %#v", entriesB)
	}
	if state.sessionUndo != nil {
		t.Fatal("cross-workspace undo was not invalidated")
	}
}

func TestP9cUndoRestoresOnlyChangedKey(t *testing.T) {
	cwd := t.TempDir()
	entries := map[string]sessionOrgEntry{
		"/tmp/a": {State: sessionStateArchived},
		"/tmp/b": {State: sessionStatePinned, Order: 4},
	}
	state := &appState{
		piCwd: cwd,
		preferences: byopPreferences{
			SessionOrganization: map[string]map[string]sessionOrgEntry{
				canonicalPiPath(cwd): entries,
			},
		},
		sessionUndo: &sessionUndoState{
			cwd: canonicalPiPath(cwd),
			key: "/tmp/a",
			snapshot: map[string]sessionOrgEntry{
				"/tmp/a": {State: sessionStatePinned, Order: 2},
				"/tmp/b": {State: sessionStateArchived},
			},
		},
	}
	state.undoSessionOrganization()
	if entry := entries["/tmp/a"]; entry.State != sessionStatePinned || entry.Order != 2 {
		t.Fatalf("changed key was not restored: %#v", entry)
	}
	if entry := entries["/tmp/b"]; entry.State != sessionStatePinned || entry.Order != 4 {
		t.Fatalf("unrelated key was modified: %#v", entry)
	}
}

func sessionIDs(sessions []piSessionSummary) []string {
	ids := make([]string, len(sessions))
	for i, session := range sessions {
		ids[i] = session.ID
	}
	return ids
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
