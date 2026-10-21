package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type automationRunState string

const (
	automationRunRunning   automationRunState = "running"
	automationRunSucceeded automationRunState = "succeeded"
	automationRunFailed    automationRunState = "failed"
	automationRunAborted   automationRunState = "aborted"
)

type automationRunOutcome string

const (
	automationOutcomeSuccess automationRunOutcome = "success"
	automationOutcomeFailure automationRunOutcome = "failure"
	automationOutcomeTimeout automationRunOutcome = "timeout"
	automationOutcomeStopped automationRunOutcome = "stopped"
)

var automationPathSegmentPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

type automationSnapshot struct {
	State   automationStoredState
	Runs    []automationRunRecord
	Running bool
}

type automationStoredState struct {
	Enabled             bool   `json:"enabled"`
	ConsecutiveFailures int    `json:"consecutiveFailures"`
	AutoPausedReason    string `json:"autoPausedReason,omitempty"`
	PausedAt            string `json:"pausedAt,omitempty"`
	LastRunAt           string `json:"lastRunAt,omitempty"`
	LastState           string `json:"lastState,omitempty"`
}

type automationRunRecord struct {
	RunID        string             `json:"runId"`
	PluginID     string             `json:"pluginId"`
	AutomationID string             `json:"automationId"`
	Command      string             `json:"command"`
	Trigger      string             `json:"trigger"`
	State        automationRunState `json:"state"`
	ErrorCode    string             `json:"errorCode,omitempty"`
	Error        string             `json:"error,omitempty"`
	Result       string             `json:"result,omitempty"`
	StartedAt    string             `json:"startedAt"`
	EndedAt      string             `json:"endedAt,omitempty"`
}

type automationStore struct {
	mu                   sync.Mutex
	dir                  string
	state                map[string]automationStoredState
	recent               map[string][]automationRunRecord
	warnings             []string
	inProgress           map[string]bool
	runNeedsQuarantine   map[string]bool
	sessionLoopKeys      map[string]struct{}
	stateNeedsQuarantine bool
	loaded               bool
}

func newAutomationStore(configDir string, sessionLoopKeys []string) *automationStore {
	keys := make(map[string]struct{}, len(sessionLoopKeys))
	for _, key := range sessionLoopKeys {
		if strings.TrimSpace(key) != "" {
			keys[key] = struct{}{}
		}
	}
	store := &automationStore{
		dir:                filepath.Join(configDir, "automations"),
		state:              make(map[string]automationStoredState),
		recent:             make(map[string][]automationRunRecord),
		inProgress:         make(map[string]bool),
		runNeedsQuarantine: make(map[string]bool),
		sessionLoopKeys:    keys,
	}
	_ = store.load()
	return store
}

func (store *automationStore) load() error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.loaded {
		return nil
	}
	if err := ensureAutomationDir(store.dir); err != nil {
		store.addWarningLocked("Automation storage could not be opened.")
		return err
	}
	if err := ensureAutomationDir(store.runsDir()); err != nil {
		store.addWarningLocked("Automation run storage could not be opened.")
		return err
	}
	store.loaded = true

	statePath := filepath.Join(store.dir, "state.json")
	data, err := os.ReadFile(statePath)
	stateNeedsSave := false
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		store.addWarningLocked("Automation state could not be read; defaults are in use.")
		store.stateNeedsQuarantine = true
		if quarantineErr := store.quarantineCorruptStateLocked(); quarantineErr == nil {
			stateNeedsSave = true
		}
	default:
		if len(bytes.TrimSpace(data)) == 0 {
			store.stateNeedsQuarantine = true
			if quarantineErr := store.quarantineCorruptStateLocked(); quarantineErr == nil {
				stateNeedsSave = true
			}
		} else {
			var saved map[string]automationStoredState
			if decodeErr := json.Unmarshal(data, &saved); decodeErr != nil || saved == nil {
				store.stateNeedsQuarantine = true
				if quarantineErr := store.quarantineCorruptStateLocked(); quarantineErr == nil {
					stateNeedsSave = true
				}
			} else {
				store.state = saved
			}
		}
	}

	if err := store.loadRunSnapshotsLocked(); err != nil {
		store.addWarningLocked("Automation run history could not be loaded.")
		return err
	}
	store.trimLoadedRunsLocked()
	recoveredRuns := store.recoverInterruptedRunsLocked()
	if recoveredRuns || stateNeedsSave {
		if err := store.saveStateLocked(); err != nil {
			store.addWarningLocked("Automation recovery state could not be saved.")
			return err
		}
	}
	if store.recoverSessionLoopsLocked(time.Now().UTC()) {
		if err := store.saveStateLocked(); err != nil {
			store.addWarningLocked("Automation recovery state could not be saved.")
			return err
		}
	}
	return nil
}

func (store *automationStore) refreshSessionLoopKeys(keys []string) {
	next := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if strings.TrimSpace(key) != "" {
			next[key] = struct{}{}
		}
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.sessionLoopKeys = next
}

func ensureAutomationDir(directory string) error {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	return os.Chmod(directory, 0o700)
}

func (store *automationStore) runsDir() string {
	return filepath.Join(store.dir, "runs")
}

func (store *automationStore) archiveDir() string {
	return filepath.Join(store.dir, "archive")
}

func (store *automationStore) loadRunSnapshotsLocked() error {
	store.recent = make(map[string][]automationRunRecord)
	if err := ensureAutomationDir(store.archiveDir()); err != nil {
		return err
	}
	return filepath.WalkDir(store.runsDir(), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			store.addWarningLocked("Automation run history could not be scanned.")
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" ||
			strings.Contains(entry.Name(), ".corrupt-") {
			return nil
		}
		relative, err := filepath.Rel(store.runsDir(), path)
		if err != nil {
			store.addWarningLocked("An automation run path could not be resolved.")
			return nil
		}
		parts := strings.Split(filepath.ToSlash(relative), "/")
		if len(parts) != 2 {
			store.addWarningLocked("An obsolete legacy automation run file was ignored.")
			return nil
		}
		key := automationKey(parts[0], strings.TrimSuffix(parts[1], ".jsonl"))
		records, invalidLines, err := store.readRunFileLocked(path)
		if err != nil {
			store.addWarningLocked("An automation run file could not be read completely.")
			if len(records) > 0 {
				store.recent[key] = records
				return nil
			}
			store.runNeedsQuarantine[key] = true
			if quarantineErr := store.quarantineAutomationFileLocked(path); quarantineErr == nil {
				delete(store.runNeedsQuarantine, key)
				store.addWarningLocked("An automation run file was corrupt and was quarantined.")
			}
			store.recent[key] = nil
			return nil
		}
		if invalidLines > 0 && len(records) == 0 {
			store.runNeedsQuarantine[key] = true
			if quarantineErr := store.quarantineAutomationFileLocked(path); quarantineErr == nil {
				delete(store.runNeedsQuarantine, key)
				store.addWarningLocked("An automation run file was corrupt and was quarantined.")
			}
			store.recent[key] = nil
			return nil
		}
		store.recent[key] = records
		return nil
	})
}

func (store *automationStore) quarantineAutomationFileLocked(path string) error {
	extension := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), extension)
	directory := filepath.Dir(path)
	for attempt := 0; ; attempt++ {
		suffix := time.Now().UTC().Format("20060102T150405.000000000Z")
		if attempt > 0 {
			suffix += fmt.Sprintf("-%d", attempt)
		}
		target := filepath.Join(directory, base+".corrupt-"+suffix+extension)
		err := os.Rename(path, target)
		if errors.Is(err, os.ErrExist) || (err != nil && strings.Contains(strings.ToLower(err.Error()), "file exists")) {
			continue
		}
		if err == nil {
			quarantinePath := target
			if directory == store.runsDir() || strings.HasPrefix(directory, store.runsDir()+string(filepath.Separator)) {
				relative, _ := filepath.Rel(store.runsDir(), target)
				quarantinePath = filepath.Join(store.archiveDir(), relative)
				if err := ensureAutomationDir(filepath.Dir(quarantinePath)); err != nil {
					_ = os.Rename(target, path)
					return err
				}
				if err := os.Rename(target, quarantinePath); err != nil {
					_ = os.Rename(target, path)
					return err
				}
			}
			return nil
		}
		return err
	}
}

func (store *automationStore) recoverInterruptedRunsLocked() bool {
	now := automationTimestamp(time.Now())
	changed := false
	for key, snapshot := range store.recent {
		records := append([]automationRunRecord(nil), snapshot...)
		fileChanged := false
		for index := range records {
			record := &records[index]
			if record.State != automationRunRunning {
				continue
			}
			record.State = automationRunAborted
			record.ErrorCode = "interrupted"
			record.Error = "Automation was interrupted when BYOP restarted."
			record.EndedAt = now
			fileChanged = true
		}
		if !fileChanged {
			continue
		}
		if err := store.trimAndWriteRunsLocked(key, records); err != nil {
			store.addWarningLocked("An interrupted automation run could not be recovered.")
			continue
		}
		store.recent[key] = records
		changed = true
	}
	return changed
}

func (store *automationStore) recoverSessionLoopsLocked(now time.Time) bool {
	changed := false
	for key := range store.sessionLoopKeys {
		state, exists := store.state[key]
		if !exists {
			state.Enabled = true
		}
		if !state.Enabled {
			if !exists {
				store.state[key] = state
			}
			continue
		}
		state.Enabled = false
		state.AutoPausedReason = "appRestarted"
		state.PausedAt = automationTimestamp(now)
		store.state[key] = state
		changed = true
	}
	for key := range store.inProgress {
		store.inProgress[key] = false
	}
	return changed
}

func (store *automationStore) snapshotFor(key string, recentLimit int) automationSnapshot {
	store.mu.Lock()
	defer store.mu.Unlock()
	state, exists := store.state[key]
	if !exists {
		state = automationStoredState{Enabled: true}
	}
	records := store.recent[key]
	if recentLimit > 0 && len(records) > recentLimit {
		records = records[len(records)-recentLimit:]
	}
	runs := make([]automationRunRecord, 0, len(records))
	for index := len(records) - 1; index >= 0; index-- {
		runs = append(runs, records[index])
	}
	return automationSnapshot{
		State:   state,
		Runs:    runs,
		Running: store.inProgress[key],
	}
}

func (store *automationStore) stateFor(key string) automationStoredState {
	return store.snapshotFor(key, 0).State
}

func (store *automationStore) recentRuns(key string, limit int) []automationRunRecord {
	return store.snapshotFor(key, limit).Runs
}

func (store *automationStore) beginRun(
	key, pluginID, automationID, command string,
) (automationRunRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.inProgress[key] {
		return automationRunRecord{}, errors.New("automation is already running")
	}
	if err := store.prepareRunFileLocked(key); err != nil {
		return automationRunRecord{}, err
	}
	now := time.Now()
	record := automationRunRecord{
		RunID:        automationRunID(now),
		PluginID:     pluginID,
		AutomationID: automationID,
		Command:      command,
		Trigger:      "manual",
		State:        automationRunRunning,
		StartedAt:    automationTimestamp(now),
	}
	line, err := json.Marshal(record)
	if err != nil {
		return automationRunRecord{}, err
	}
	if err := ensureAutomationDir(filepath.Dir(store.runPath(key))); err != nil {
		return automationRunRecord{}, fmt.Errorf("create automation run directory: %w", err)
	}
	file, err := os.OpenFile(store.runPath(key), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return automationRunRecord{}, fmt.Errorf("create automation run: %w", err)
	}
	if err := ensureAutomationJSONLLineBoundary(file); err != nil {
		_ = file.Close()
		return automationRunRecord{}, fmt.Errorf("repair automation run line boundary: %w", err)
	}
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		_ = file.Close()
		return automationRunRecord{}, fmt.Errorf("seek automation run: %w", err)
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		_ = file.Close()
		return automationRunRecord{}, fmt.Errorf("write automation run: %w", err)
	}
	if err := file.Close(); err != nil {
		return automationRunRecord{}, fmt.Errorf("close automation run: %w", err)
	}
	_ = os.Chmod(store.runPath(key), 0o600)
	store.recent[key] = append(store.recent[key], record)
	store.inProgress[key] = true
	return record, nil
}

func (store *automationStore) finishRun(
	key, runID string,
	outcome automationRunOutcome,
	errorSummary, resultSummary string,
) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	defer delete(store.inProgress, key)

	now := time.Now()
	record := automationRunRecord{
		RunID:     runID,
		State:     automationRunFailed,
		Error:     summarizeLine(errorSummary, 500),
		Result:    summarizeLine(resultSummary, 500),
		StartedAt: automationTimestamp(now),
		EndedAt:   automationTimestamp(now),
	}
	state := store.stateLocked(key)
	switch outcome {
	case automationOutcomeSuccess:
		record.State = automationRunSucceeded
		record.Error = ""
	case automationOutcomeTimeout:
		record.ErrorCode = "timeLimit"
	case automationOutcomeFailure:
		record.ErrorCode = "pluginError"
	case automationOutcomeStopped:
		record.State = automationRunAborted
		record.ErrorCode = "stopped"
	default:
		return false, fmt.Errorf("unknown automation outcome %q", outcome)
	}

	records := append([]automationRunRecord(nil), store.recent[key]...)
	found := false
	for index := range records {
		if records[index].RunID != runID {
			continue
		}
		record.PluginID = records[index].PluginID
		record.AutomationID = records[index].AutomationID
		record.Command = records[index].Command
		record.Trigger = records[index].Trigger
		record.StartedAt = records[index].StartedAt
		records[index] = record
		found = true
		break
	}
	if !found {
		records = append(records, record)
	}

	paused := !state.Enabled || state.PausedAt != "" || state.AutoPausedReason != ""
	switch outcome {
	case automationOutcomeSuccess:
		if !paused {
			state.ConsecutiveFailures = 0
		}
	case automationOutcomeStopped:
	default:
		state.ConsecutiveFailures++
	}
	autoPaused := outcome != automationOutcomeSuccess &&
		outcome != automationOutcomeStopped &&
		state.ConsecutiveFailures >= automationMaxConsecutiveFailures
	if autoPaused {
		state.Enabled = false
		state.AutoPausedReason = "consecutiveFailures"
		state.PausedAt = automationTimestamp(now)
	}
	state.LastRunAt = automationTimestamp(now)
	state.LastState = string(record.State)
	previousState, hadState := store.state[key]
	store.state[key] = state
	if err := store.saveStateLocked(); err != nil {
		if hadState {
			store.state[key] = previousState
		} else {
			delete(store.state, key)
		}
		return false, err
	}
	if err := store.trimAndWriteRunsLocked(key, records); err != nil {
		if hadState {
			store.state[key] = previousState
		} else {
			delete(store.state, key)
		}
		if restoreErr := store.saveStateLocked(); restoreErr != nil {
			store.addWarningLocked("Automation state rollback could not be saved.")
		}
		return false, err
	}
	store.recent[key] = records
	return autoPaused, nil
}

func (store *automationStore) pause(key string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	state := store.stateLocked(key)
	state.Enabled = false
	state.AutoPausedReason = ""
	state.PausedAt = automationTimestamp(time.Now())
	previousState, hadState := store.state[key]
	store.state[key] = state
	if err := store.saveStateLocked(); err != nil {
		if hadState {
			store.state[key] = previousState
		} else {
			delete(store.state, key)
		}
		return err
	}
	return nil
}

func (store *automationStore) resume(key string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	state := store.stateLocked(key)
	state.Enabled = true
	state.ConsecutiveFailures = 0
	state.AutoPausedReason = ""
	state.PausedAt = ""
	previousState, hadState := store.state[key]
	store.state[key] = state
	if err := store.saveStateLocked(); err != nil {
		if hadState {
			store.state[key] = previousState
		} else {
			delete(store.state, key)
		}
		return err
	}
	return nil
}

func (store *automationStore) isRunning(key string) bool {
	return store.snapshotFor(key, 0).Running
}

func (store *automationStore) warningsSnapshot() []string {
	store.mu.Lock()
	defer store.mu.Unlock()
	return append([]string(nil), store.warnings...)
}

func (store *automationStore) trimLoadedRunsLocked() {
	for key, records := range store.recent {
		if len(records) > automationRunsKeptPerTask {
			if err := store.trimAndWriteRunsLocked(key, records); err != nil {
				store.addWarningLocked("An oversized automation run file could not be trimmed.")
				continue
			}
			records = records[len(records)-automationRunsKeptPerTask:]
		}
		store.recent[key] = append([]automationRunRecord(nil), records...)
	}
}

func (store *automationStore) stateLocked(key string) automationStoredState {
	state, exists := store.state[key]
	if !exists {
		return automationStoredState{Enabled: true}
	}
	return state
}

func (store *automationStore) saveStateLocked() error {
	if err := ensureAutomationDir(store.dir); err != nil {
		return err
	}
	if store.stateNeedsQuarantine {
		if err := store.quarantineCorruptStateLocked(); err != nil {
			return fmt.Errorf("quarantine corrupt automation state: %w", err)
		}
	}
	data, err := json.MarshalIndent(store.state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeAutomationFileAtomic(filepath.Join(store.dir, "state.json"), data)
}

func (store *automationStore) quarantineCorruptStateLocked() error {
	if err := quarantineAutomationFile(filepath.Join(store.dir, "state.json")); err != nil {
		store.addWarningLocked("Corrupt automation state could not be quarantined before saving.")
		return err
	}
	store.stateNeedsQuarantine = false
	store.addWarningLocked("Automation state was corrupt and was quarantined; defaults are in use.")
	return nil
}

func (store *automationStore) readRunFileLocked(
	path string,
) ([]automationRunRecord, int, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		store.addWarningLocked("Automation run history could not be read.")
		return nil, 0, err
	}
	defer file.Close()

	var records []automationRunRecord
	invalidLines := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), maxPluginMessageBytes)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var record automationRunRecord
		if err := json.Unmarshal(line, &record); err != nil {
			invalidLines++
			store.addWarningLocked("An automation run file was corrupt and is ignored.")
			continue
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		store.addWarningLocked("An automation run file could not be read.")
		return nil, invalidLines, err
	}
	return records, invalidLines, nil
}

func (store *automationStore) prepareRunFileLocked(key string) error {
	if !store.runNeedsQuarantine[key] {
		return nil
	}
	if err := store.quarantineAutomationFileLocked(store.runPath(key)); err != nil {
		store.addWarningLocked("Corrupt automation run history could not be quarantined before saving.")
		return fmt.Errorf("quarantine corrupt automation run history: %w", err)
	}
	delete(store.runNeedsQuarantine, key)
	store.addWarningLocked("An automation run file was corrupt and was quarantined.")
	return nil
}

func (store *automationStore) trimAndWriteRunsLocked(key string, records []automationRunRecord) error {
	if len(records) > automationRunsKeptPerTask {
		overflow := len(records) - automationRunsKeptPerTask
		if err := appendAutomationJSONLines(store.archivePath(key), records[:overflow]); err != nil {
			return fmt.Errorf("archive automation runs: %w", err)
		}
		records = records[overflow:]
	}
	return store.writeRunsLocked(key, records)
}

func (store *automationStore) writeRunsLocked(key string, records []automationRunRecord) error {
	if err := writeAutomationJSONLines(store.runPath(key), records); err != nil {
		return fmt.Errorf("write automation runs: %w", err)
	}
	return nil
}

func quarantineAutomationFile(path string) error {
	extension := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), extension)
	for attempt := 0; ; attempt++ {
		suffix := time.Now().UTC().Format("20060102T150405.000000000Z")
		if attempt > 0 {
			suffix += fmt.Sprintf("-%d", attempt)
		}
		target := filepath.Join(filepath.Dir(path), base+".corrupt-"+suffix+extension)
		err := os.Rename(path, target)
		if errors.Is(err, os.ErrExist) || (err != nil && strings.Contains(strings.ToLower(err.Error()), "file exists")) {
			continue
		}
		return err
	}
}

func (store *automationStore) runPath(key string) string {
	pluginID, automationID, ok := splitAutomationKey(key)
	if !ok {
		return filepath.Join(store.runsDir(), "invalid", "invalid.jsonl")
	}
	return filepath.Join(store.runsDir(), pluginID, automationID+".jsonl")
}

func (store *automationStore) archivePath(key string) string {
	pluginID, automationID, ok := splitAutomationKey(key)
	if !ok {
		return filepath.Join(store.archiveDir(), "invalid", "invalid.jsonl")
	}
	return filepath.Join(store.archiveDir(), pluginID, automationID+".jsonl")
}

func splitAutomationKey(key string) (string, string, bool) {
	pluginID, automationID, ok := strings.Cut(key, "/")
	return pluginID, automationID, ok &&
		automationPathSegmentPattern.MatchString(pluginID) &&
		automationPathSegmentPattern.MatchString(automationID)
}

func appendAutomationJSONLines(path string, records []automationRunRecord) error {
	if len(records) == 0 {
		return nil
	}
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			return err
		}
	}
	if err := ensureAutomationDir(filepath.Dir(path)); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := ensureAutomationJSONLLineBoundary(file); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data.Bytes()); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func ensureAutomationJSONLLineBoundary(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return nil
	}
	var last [1]byte
	if _, err := file.ReadAt(last[:], info.Size()-1); err != nil {
		return err
	}
	if last[0] == '\n' {
		return nil
	}
	_, err = file.WriteAt([]byte{'\n'}, info.Size())
	return err
}

func writeAutomationJSONLines(path string, records []automationRunRecord) error {
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			return err
		}
	}
	return writeAutomationFileAtomic(path, data.Bytes())
}

func writeAutomationFileAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := ensureAutomationDir(directory); err != nil {
		return err
	}
	temp, err := os.CreateTemp(directory, ".automation-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	remove := true
	defer func() {
		_ = temp.Close()
		if remove {
			_ = os.Remove(tempName)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, path); err != nil {
		return err
	}
	remove = false
	return nil
}

func (store *automationStore) addWarningLocked(message string) {
	for _, warning := range store.warnings {
		if warning == message {
			return
		}
	}
	store.warnings = append(store.warnings, message)
}

func automationTimestamp(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func automationRunID(now time.Time) string {
	return fmt.Sprintf("run-%d", now.UTC().UnixNano())
}

func registeredAutomationKeys(registry pluginRegistry) []string {
	var keys []string
	for _, entry := range registry.Entries {
		if entry.Status != pluginRegistered || entry.Manifest == nil {
			continue
		}
		for _, contribution := range entry.Manifest.Contributes.Automations {
			keys = append(keys, automationKey(entry.ID, contribution.ID))
		}
	}
	sort.Strings(keys)
	return keys
}

func registeredSessionLoopKeys(registry pluginRegistry) []string {
	var keys []string
	for _, entry := range registry.Entries {
		if entry.Status != pluginRegistered || entry.Manifest == nil {
			continue
		}
		for _, contribution := range entry.Manifest.Contributes.Automations {
			if contribution.Kind == "sessionLoop" {
				keys = append(keys, automationKey(entry.ID, contribution.ID))
			}
		}
	}
	sort.Strings(keys)
	return keys
}

func automationKey(pluginID, automationID string) string {
	return pluginID + "/" + automationID
}
