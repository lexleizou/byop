package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestP7AutomationManifestValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*pluginManifest)
		wantErr string
	}{
		{
			name: "interval below minimum",
			mutate: func(manifest *pluginManifest) {
				manifest.Contributes.Automations[0].Rule.Minutes = 4
			},
			wantErr: "interval minutes must be at least 5",
		},
		{
			name: "afterSettled on scheduled loop",
			mutate: func(manifest *pluginManifest) {
				manifest.Contributes.Automations[0].Rule = &pluginAutomationRule{Type: "afterSettled"}
			},
			wantErr: "afterSettled is only valid for sessionLoop",
		},
		{
			name: "undeclared command",
			mutate: func(manifest *pluginManifest) {
				manifest.Contributes.Automations[0].Command = "hello.missing"
			},
			wantErr: "automation command is not declared",
		},
		{
			name: "manual with rule",
			mutate: func(manifest *pluginManifest) {
				manifest.Contributes.Automations[1].Rule = &pluginAutomationRule{
					Type: "at", At: "2026-10-21T09:00:00Z",
				}
			},
			wantErr: "rule must be absent for manual",
		},
		{
			name: "missing command activation",
			mutate: func(manifest *pluginManifest) {
				manifest.Activation.Events = []string{"onView:hello.panel", "onCommand:hello.greet"}
			},
			wantErr: "automation command lacks activation event",
		},
		{
			name: "manual missed policy",
			mutate: func(manifest *pluginManifest) {
				manifest.Contributes.Automations[1].MissedPolicy = "never"
			},
			wantErr: "missedPolicy must be catchUpLatest or skip",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := loadP7FixtureManifest(t)
			test.mutate(manifest)
			err := validatePluginManifest(manifest)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("validation error = %v, want %q", err, test.wantErr)
			}
		})
	}

	manifest := loadP7FixtureManifest(t)
	if err := validatePluginManifest(manifest); err != nil {
		t.Fatalf("valid P7 fixture: %v", err)
	}
}

func TestP7AutomationFailuresPauseAndResume(t *testing.T) {
	store := newAutomationStore(t.TempDir(), nil)
	key := automationKey("example.hello-node", "hello.auto.fail")

	for attempt := 1; attempt <= automationMaxConsecutiveFailures; attempt++ {
		record, err := store.beginRun(key, "example.hello-node", "hello.auto.fail", "hello.fail")
		if err != nil {
			t.Fatalf("begin failure %d: %v", attempt, err)
		}
		autoPaused, err := store.finishRun(
			key, record.RunID, automationOutcomeFailure, "fixture failure", "",
		)
		if err != nil {
			t.Fatalf("finish failure %d: %v", attempt, err)
		}
		if want := attempt == automationMaxConsecutiveFailures; autoPaused != want {
			t.Fatalf("failure %d autoPaused = %v, want %v", attempt, autoPaused, want)
		}
	}
	state := store.stateFor(key)
	if state.Enabled || state.ConsecutiveFailures != automationMaxConsecutiveFailures ||
		state.AutoPausedReason != "consecutiveFailures" {
		t.Fatalf("paused state = %#v", state)
	}

	if err := store.resume(key); err != nil {
		t.Fatalf("resume: %v", err)
	}
	state = store.stateFor(key)
	if !state.Enabled || state.ConsecutiveFailures != 0 || state.AutoPausedReason != "" {
		t.Fatalf("resumed state = %#v", state)
	}

	for attempt := 1; attempt < automationMaxConsecutiveFailures; attempt++ {
		record, err := store.beginRun(key, "example.hello-node", "hello.auto.fail", "hello.fail")
		if err != nil {
			t.Fatalf("begin failure after resume %d: %v", attempt, err)
		}
		if _, err := store.finishRun(
			key, record.RunID, automationOutcomeFailure, "fixture failure", "",
		); err != nil {
			t.Fatalf("finish failure after resume %d: %v", attempt, err)
		}
	}
	record, err := store.beginRun(key, "example.hello-node", "hello.auto.greet", "hello.greet")
	if err != nil {
		t.Fatalf("begin success: %v", err)
	}
	if _, err := store.finishRun(
		key, record.RunID, automationOutcomeSuccess, "", "Greeting completed",
	); err != nil {
		t.Fatalf("finish success: %v", err)
	}
	state = store.stateFor(key)
	if !state.Enabled || state.ConsecutiveFailures != 0 || state.LastState != "succeeded" {
		t.Fatalf("success state = %#v", state)
	}
}

func TestP7AutomationPauseDoesNotDiscardFinishedRun(t *testing.T) {
	store := newAutomationStore(t.TempDir(), nil)
	key := automationKey("example.hello-node", "hello.auto.greet")
	failure, err := store.beginRun(key, "example.hello-node", "hello.auto.fail", "hello.fail")
	if err != nil {
		t.Fatalf("begin failure run: %v", err)
	}
	if _, err := store.finishRun(
		key, failure.RunID, automationOutcomeFailure, "fixture failure", "",
	); err != nil {
		t.Fatalf("finish failure run: %v", err)
	}
	record, err := store.beginRun(key, "example.hello-node", "hello.auto.greet", "hello.greet")
	if err != nil {
		t.Fatalf("begin run: %v", err)
	}
	if err := store.pause(key); err != nil {
		t.Fatalf("pause during run: %v", err)
	}
	if _, err := store.finishRun(
		key, record.RunID, automationOutcomeSuccess, "", "Greeting completed",
	); err != nil {
		t.Fatalf("finish paused run: %v", err)
	}
	runs := store.recentRuns(key, 1)
	if len(runs) != 1 || runs[0].State != automationRunSucceeded {
		t.Fatalf("finished run = %#v", runs)
	}
	state := store.stateFor(key)
	if state.Enabled || state.ConsecutiveFailures != 1 || state.LastState != "succeeded" {
		t.Fatalf("paused state after completion = %#v", state)
	}
}

func TestP7AutomationRunCapArchivesOverflow(t *testing.T) {
	store := newAutomationStore(t.TempDir(), nil)
	key := automationKey("example.hello-node", "hello.auto.greet")
	for index := 0; index <= automationRunsKeptPerTask; index++ {
		record, err := store.beginRun(key, "example.hello-node", "hello.auto.greet", "hello.greet")
		if err != nil {
			t.Fatalf("begin run %d: %v", index, err)
		}
		if _, err := store.finishRun(
			key, record.RunID, automationOutcomeSuccess, "", "Greeting completed",
		); err != nil {
			t.Fatalf("finish run %d: %v", index, err)
		}
	}

	if got := countP7RunLines(t, store.runPath(key)); got != automationRunsKeptPerTask {
		t.Fatalf("active run lines = %d, want %d", got, automationRunsKeptPerTask)
	}
	if got := countP7RunLines(t, store.archivePath(key)); got != 1 {
		t.Fatalf("archive run lines = %d, want 1", got)
	}
}

func TestP7AutomationLoadTrimsOversizedRuns(t *testing.T) {
	configDir := t.TempDir()
	store := newAutomationStore(configDir, nil)
	key := automationKey("example.hello-node", "hello.auto.greet")
	var data bytes.Buffer
	for index := 0; index <= automationRunsKeptPerTask+1; index++ {
		record := automationRunRecord{
			RunID:        fmt.Sprintf("over-cap-%03d", index),
			PluginID:     "example.hello-node",
			AutomationID: "hello.auto.greet",
			Command:      "hello.greet",
			Trigger:      "manual",
			State:        automationRunSucceeded,
			StartedAt:    automationTimestamp(time.Date(2026, 10, 21, 9, index, 0, 0, time.UTC)),
			EndedAt:      automationTimestamp(time.Date(2026, 10, 21, 9, index, 1, 0, time.UTC)),
		}
		if err := json.NewEncoder(&data).Encode(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(store.runPath(key)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.runPath(key), data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	reopened := newAutomationStore(configDir, nil)
	if got := countP7RunLines(t, reopened.runPath(key)); got != automationRunsKeptPerTask {
		t.Fatalf("active run lines = %d, want %d", got, automationRunsKeptPerTask)
	}
	if got := countP7RunLines(t, reopened.archivePath(key)); got != 2 {
		t.Fatalf("archive run lines = %d, want 2", got)
	}
	quarantined, err := filepath.Glob(filepath.Join(reopened.runsDir(), "*", "*.corrupt-*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("oversized run file was quarantined: %v", quarantined)
	}
}

func TestP7AutomationLoadSkipsQuarantinedRunNames(t *testing.T) {
	configDir := t.TempDir()
	store := newAutomationStore(configDir, nil)
	path := filepath.Join(
		store.runsDir(), "example.hello-node",
		"hello.auto.greet.corrupt-20261021T090000.000000000Z.jsonl",
	)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	record := automationRunRecord{
		RunID:        "quarantined-run",
		PluginID:     "example.hello-node",
		AutomationID: "hello.auto.greet",
		Command:      "hello.greet",
		Trigger:      "manual",
		State:        automationRunSucceeded,
		StartedAt:    automationTimestamp(time.Now().UTC()),
		EndedAt:      automationTimestamp(time.Now().UTC()),
	}
	if err := writeAutomationJSONLines(path, []automationRunRecord{record}); err != nil {
		t.Fatal(err)
	}

	reopened := newAutomationStore(configDir, nil)
	reopened.mu.Lock()
	loaded := len(reopened.recent)
	reopened.mu.Unlock()
	if loaded != 0 {
		t.Fatalf("quarantined run names were loaded into %d snapshots", loaded)
	}
}

func TestP7AutomationRecoversInterruptedRun(t *testing.T) {
	configDir := t.TempDir()
	store := newAutomationStore(configDir, nil)
	key := automationKey("example.hello-node", "hello.auto.greet")
	record, err := store.beginRun(key, "example.hello-node", "hello.auto.greet", "hello.greet")
	if err != nil {
		t.Fatalf("begin run: %v", err)
	}
	file, err := os.OpenFile(store.runPath(key), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open running run file: %v", err)
	}
	if _, err := file.WriteString("{\"runId\":"); err != nil {
		_ = file.Close()
		t.Fatalf("append malformed run line: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close running run file: %v", err)
	}

	reopened := newAutomationStore(configDir, nil)
	runs := reopened.recentRuns(key, 1)
	if len(runs) != 1 {
		t.Fatalf("recovered runs = %d, want 1", len(runs))
	}
	if runs[0].RunID != record.RunID || runs[0].State != automationRunAborted ||
		runs[0].ErrorCode != "interrupted" || runs[0].EndedAt == "" {
		t.Fatalf("recovered run = %#v", runs[0])
	}
	if info, err := os.Stat(reopened.runPath(key)); err != nil {
		t.Fatalf("stat recovered run file: %v", err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("recovered run file mode = %o, want 600", info.Mode().Perm())
	}
	if info, err := os.Stat(reopened.runsDir()); err != nil {
		t.Fatalf("stat automation runs directory: %v", err)
	} else if info.Mode().Perm() != 0o700 {
		t.Fatalf("automation runs directory mode = %o, want 700", info.Mode().Perm())
	}
}

func TestP7AutomationKeepsValidRecordsAndRecoversBadLine(t *testing.T) {
	configDir := t.TempDir()
	store := newAutomationStore(configDir, nil)
	key := automationKey("example.hello-node", "hello.auto.greet")
	if err := os.MkdirAll(filepath.Dir(store.runPath(key)), 0o700); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	for index := 0; index < 10; index++ {
		record := automationRunRecord{
			RunID:        "run-" + string(rune('a'+index)),
			PluginID:     "example.hello-node",
			AutomationID: "hello.auto.greet",
			Command:      "hello.greet",
			Trigger:      "manual",
			State:        automationRunSucceeded,
			StartedAt:    automationTimestamp(time.Date(2026, 10, 21, 9, index, 0, 0, time.UTC)),
			EndedAt:      automationTimestamp(time.Date(2026, 10, 21, 9, index, 1, 0, time.UTC)),
		}
		if err := json.NewEncoder(&data).Encode(record); err != nil {
			t.Fatal(err)
		}
	}
	data.WriteString("{\"runId\":")
	if err := os.WriteFile(store.runPath(key), data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	reopened := newAutomationStore(configDir, nil)
	if warnings := reopened.warningsSnapshot(); len(warnings) == 0 ||
		!strings.Contains(strings.Join(warnings, " "), "corrupt") {
		t.Fatalf("warnings = %#v, want corrupt-line warning", warnings)
	}
	record, err := reopened.beginRun(
		key, "example.hello-node", "hello.auto.greet", "hello.greet",
	)
	if err != nil {
		t.Fatalf("begin run after malformed line: %v", err)
	}
	if _, err := reopened.finishRun(
		key, record.RunID, automationOutcomeSuccess, "", "Greeting completed",
	); err != nil {
		t.Fatalf("finish run after malformed line: %v", err)
	}
	records := readP7RunRecords(t, reopened.runPath(key))
	if len(records) != 11 {
		t.Fatalf("valid run records = %d, want 11", len(records))
	}
}

func TestP7AutomationQuarantinesEmptyStateBeforeSaving(t *testing.T) {
	configDir := t.TempDir()
	automationDir := filepath.Join(configDir, "automations")
	if err := os.MkdirAll(automationDir, 0o700); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(automationDir, "state.json")
	if err := os.WriteFile(statePath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	store := newAutomationStore(configDir, nil)
	quarantined, err := filepath.Glob(filepath.Join(automationDir, "state.corrupt-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(quarantined) != 1 {
		t.Fatalf("quarantined state files = %v, want one", quarantined)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read replacement state: %v", err)
	}
	var saved map[string]automationStoredState
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("replacement state = %q: %v", data, err)
	}
	if state := store.stateFor(automationKey("example.hello-node", "hello.auto.greet")); !state.Enabled {
		t.Fatalf("default state = %#v, want enabled", state)
	}
}

func TestP7AutomationHistoryPathsDoNotCollide(t *testing.T) {
	configDir := t.TempDir()
	store := newAutomationStore(configDir, nil)
	firstKey := automationKey("foo", "run")
	secondKey := automationKey("foo.archive", "run")
	if store.runPath(firstKey) == store.runPath(secondKey) ||
		store.archivePath(firstKey) == store.archivePath(secondKey) {
		t.Fatalf("automation history paths collide: %s and %s",
			store.runPath(firstKey), store.runPath(secondKey))
	}
	first, err := store.beginRun(firstKey, "foo", "run", "hello.greet")
	if err != nil {
		t.Fatalf("begin first run: %v", err)
	}
	if _, err := store.finishRun(
		firstKey, first.RunID, automationOutcomeFailure, "fixture failure", "",
	); err != nil {
		t.Fatalf("finish first run: %v", err)
	}
	second, err := store.beginRun(secondKey, "foo.archive", "run", "hello.greet")
	if err != nil {
		t.Fatalf("begin second run: %v", err)
	}
	if _, err := store.finishRun(
		secondKey, second.RunID, automationOutcomeSuccess, "", "Greeting completed",
	); err != nil {
		t.Fatalf("finish second run: %v", err)
	}

	reopened := newAutomationStore(configDir, nil)
	if got := len(reopened.recentRuns(firstKey, 5)); got != 1 {
		t.Fatalf("first history runs = %d, want 1", got)
	}
	if got := len(reopened.recentRuns(secondKey, 5)); got != 1 {
		t.Fatalf("second history runs = %d, want 1", got)
	}
	if firstState, secondState := reopened.stateFor(firstKey), reopened.stateFor(secondKey); firstState.ConsecutiveFailures != 1 || secondState.ConsecutiveFailures != 0 {
		t.Fatalf("colliding states = %#v and %#v", firstState, secondState)
	}
}

func TestP7AutomationStoppedRunDoesNotCountAsFailure(t *testing.T) {
	store := newAutomationStore(t.TempDir(), nil)
	key := automationKey("example.hello-node", "hello.auto.greet")
	record, err := store.beginRun(key, "example.hello-node", "hello.auto.greet", "hello.greet")
	if err != nil {
		t.Fatalf("begin run: %v", err)
	}
	if _, err := store.finishRun(
		key, record.RunID, automationOutcomeStopped, "plugin stopped", "",
	); err != nil {
		t.Fatalf("finish stopped run: %v", err)
	}
	state := store.stateFor(key)
	if !state.Enabled || state.ConsecutiveFailures != 0 || state.LastState != "aborted" {
		t.Fatalf("stopped run state = %#v", state)
	}
	runs := store.recentRuns(key, 1)
	if len(runs) != 1 || runs[0].ErrorCode != "stopped" {
		t.Fatalf("stopped run = %#v", runs)
	}
}

func TestP7AutomationPausesSessionLoopAfterRestart(t *testing.T) {
	configDir := t.TempDir()
	_ = newAutomationStore(configDir, nil)
	key := automationKey("example.hello-node", "hello.auto.session")
	reopened := newAutomationStore(configDir, []string{key})
	state := reopened.stateFor(key)
	if state.Enabled || state.AutoPausedReason != "appRestarted" || state.PausedAt == "" {
		t.Fatalf("session loop state = %#v", state)
	}
}

func TestP7AutomationSessionLoopRefreshDoesNotRestartRecovery(t *testing.T) {
	store := newAutomationStore(t.TempDir(), []string{
		automationKey("example.hello-node", "hello.auto.session"),
	})
	key := automationKey("example.hello-node", "hello.auto.session")
	if err := store.resume(key); err != nil {
		t.Fatalf("resume session loop: %v", err)
	}
	store.refreshSessionLoopKeys([]string{key})
	state := store.stateFor(key)
	if !state.Enabled || state.AutoPausedReason != "" {
		t.Fatalf("refresh unexpectedly restarted recovery: %#v", state)
	}
}

func loadP7FixtureManifest(t *testing.T) *pluginManifest {
	t.Helper()
	manifest, err := loadPluginManifest(filepath.Join("examples", "hello-node", pluginManifestFilename))
	if err != nil {
		t.Fatalf("load P7 fixture manifest: %v", err)
	}
	return manifest
}

func countP7RunLines(t *testing.T, path string) int {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			count++
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return count
}

func readP7RunRecords(t *testing.T, path string) []automationRunRecord {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer file.Close()
	var records []automationRunRecord
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var record automationRunRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return records
}
