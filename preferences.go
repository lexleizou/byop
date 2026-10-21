package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const byopPreferencesVersion = 1

type byopPreferences struct {
	Version                int                    `json:"version"`
	Theme                  string                 `json:"theme,omitempty"`
	FollowUpStyle          string                 `json:"followUpStyle,omitempty"`
	NavCollapsed           bool                   `json:"navCollapsed,omitempty"`
	TasksOpen              bool                   `json:"tasksOpen,omitempty"`
	Workspace              string                 `json:"workspace,omitempty"`
	LastSessionByWorkspace map[string]string      `json:"lastSessionByWorkspace,omitempty"`
	Drafts                 map[string]legacyDraft `json:"drafts,omitempty"`
	Legacy                 map[string]legacyValue `json:"legacy,omitempty"`
}

type legacyValue struct {
	Value      json.RawMessage `json:"value"`
	Source     string          `json:"source"`
	ImportedAt string          `json:"importedAt"`
}

type legacyDraft struct {
	Text       string `json:"text"`
	Source     string `json:"source"`
	ImportedAt string `json:"importedAt"`
}

type byopPreferencesStore struct {
	mu      sync.Mutex
	path    string
	warning string
}

func defaultByopPreferences() byopPreferences {
	return byopPreferences{
		Version:                byopPreferencesVersion,
		Theme:                  string(themeSystem),
		FollowUpStyle:          string(followUpStyleGhost),
		LastSessionByWorkspace: make(map[string]string),
		Drafts:                 make(map[string]legacyDraft),
		Legacy:                 make(map[string]legacyValue),
	}
}

func newByopPreferencesStore(configDir string) *byopPreferencesStore {
	return &byopPreferencesStore{path: filepath.Join(configDir, "preferences.json")}
}

func (store *byopPreferencesStore) load() (byopPreferences, string) {
	store.mu.Lock()
	defer store.mu.Unlock()

	preferences := defaultByopPreferences()
	data, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		store.warning = ""
		return preferences, ""
	}
	if err != nil {
		store.warning = "Preferences could not be read; defaults are in use."
		return preferences, store.warning
	}
	if len(bytes.TrimSpace(data)) == 0 {
		store.warning = "Preferences are empty; defaults are in use."
		return preferences, store.warning
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&preferences); err != nil {
		store.warning = "Preferences file is damaged; defaults are in use and the original file is preserved."
		return defaultByopPreferences(), store.warning
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		store.warning = "Preferences file is damaged; defaults are in use and the original file is preserved."
		return defaultByopPreferences(), store.warning
	}
	normalizeByopPreferences(&preferences)
	store.warning = ""
	return preferences, ""
}

func normalizeByopPreferences(preferences *byopPreferences) {
	if preferences == nil {
		return
	}
	if preferences.Version == 0 {
		preferences.Version = byopPreferencesVersion
	}
	switch themeMode(strings.ToLower(strings.TrimSpace(preferences.Theme))) {
	case themeLight, themeDark, themeSystem:
		preferences.Theme = strings.ToLower(strings.TrimSpace(preferences.Theme))
	default:
		preferences.Theme = string(themeSystem)
	}
	preferences.FollowUpStyle = string(normalizeFollowUpStyle(preferences.FollowUpStyle))
	if preferences.LastSessionByWorkspace == nil {
		preferences.LastSessionByWorkspace = make(map[string]string)
	}
	if preferences.Drafts == nil {
		preferences.Drafts = make(map[string]legacyDraft)
	}
	if preferences.Legacy == nil {
		preferences.Legacy = make(map[string]legacyValue)
	}
}

func cloneByopPreferences(preferences byopPreferences) byopPreferences {
	clone := preferences
	clone.LastSessionByWorkspace = make(map[string]string, len(preferences.LastSessionByWorkspace))
	for key, value := range preferences.LastSessionByWorkspace {
		clone.LastSessionByWorkspace[key] = value
	}
	clone.Drafts = make(map[string]legacyDraft, len(preferences.Drafts))
	for key, value := range preferences.Drafts {
		clone.Drafts[key] = value
	}
	clone.Legacy = make(map[string]legacyValue, len(preferences.Legacy))
	for key, value := range preferences.Legacy {
		value.Value = cloneRawMessage(value.Value)
		clone.Legacy[key] = value
	}
	return clone
}

func cloneRawMessage(value json.RawMessage) json.RawMessage {
	if value == nil {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

func (store *byopPreferencesStore) save(preferences byopPreferences) error {
	store.mu.Lock()
	defer store.mu.Unlock()

	normalizeByopPreferences(&preferences)
	data, err := json.MarshalIndent(preferences, "", "  ")
	if err != nil {
		return fmt.Errorf("encode preferences: %w", err)
	}
	data = append(data, '\n')
	directory := filepath.Dir(store.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create preferences directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("set preferences directory mode: %w", err)
	}
	if store.warning != "" {
		if _, err := os.Stat(store.path); err == nil {
			backup := filepath.Join(directory,
				"preferences.corrupt-"+time.Now().UTC().Format("20060102T150405.000000000Z")+".json")
			if err := os.Rename(store.path, backup); err != nil {
				return fmt.Errorf("preserve damaged preferences: %w", err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect damaged preferences: %w", err)
		}
	}
	temp, err := os.CreateTemp(directory, ".preferences-*.tmp")
	if err != nil {
		return fmt.Errorf("create preferences file: %w", err)
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
		return fmt.Errorf("set preferences file mode: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		return fmt.Errorf("write preferences: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close preferences: %w", err)
	}
	if err := os.Rename(tempName, store.path); err != nil {
		return fmt.Errorf("replace preferences: %w", err)
	}
	remove = false
	store.warning = ""
	return nil
}

func (store *byopPreferencesStore) warningSnapshot() string {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.warning
}

func (store *byopPreferencesStore) clearWarning() {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.warning = ""
}
