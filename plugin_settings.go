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
)

const pluginSettingsCorruptMessage = "Settings file could not be read; defaults are in use."

type pluginSettingsStore struct {
	mu       sync.Mutex
	dir      string
	warnings map[string]string
}

func pluginConfigDir() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("BYOP_CONFIG_DIR")); configured != "" {
		return filepath.Abs(configured)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".byop"), nil
}

func newPluginSettingsStore(configDir string) *pluginSettingsStore {
	return &pluginSettingsStore{
		dir:      filepath.Join(configDir, "plugin-settings"),
		warnings: make(map[string]string),
	}
}

func (store *pluginSettingsStore) load(manifest *pluginManifest) (map[string]any, error) {
	defaults, err := defaultPluginSettings(manifest)
	if err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	filename := store.filename(manifest.ID)
	data, err := os.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		delete(store.warnings, manifest.ID)
		return defaults, nil
	}
	if err != nil {
		store.warnings[manifest.ID] = pluginSettingsCorruptMessage
		return defaults, nil
	}
	var saved map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&saved); err != nil {
		store.warnings[manifest.ID] = pluginSettingsCorruptMessage
		return defaults, nil
	}
	if saved == nil {
		store.warnings[manifest.ID] = pluginSettingsCorruptMessage
		return defaults, nil
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		store.warnings[manifest.ID] = pluginSettingsCorruptMessage
		return defaults, nil
	}
	delete(store.warnings, manifest.ID)
	return mergePluginSettings(defaults, saved, manifest), nil
}

func (store *pluginSettingsStore) save(manifest *pluginManifest, values map[string]any) (map[string]any, error) {
	defaults, err := defaultPluginSettings(manifest)
	if err != nil {
		return nil, err
	}
	merged := mergePluginSettings(defaults, values, manifest)
	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode plugin settings: %w", err)
	}
	data = append(data, '\n')

	store.mu.Lock()
	defer store.mu.Unlock()
	if err := os.MkdirAll(store.dir, 0o700); err != nil {
		return nil, fmt.Errorf("create plugin settings directory: %w", err)
	}
	if err := os.Chmod(store.dir, 0o700); err != nil {
		return nil, fmt.Errorf("set plugin settings directory mode: %w", err)
	}
	temp, err := os.CreateTemp(store.dir, ".settings-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create plugin settings file: %w", err)
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
		return nil, fmt.Errorf("set plugin settings file mode: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		return nil, fmt.Errorf("write plugin settings: %w", err)
	}
	if err := temp.Close(); err != nil {
		return nil, fmt.Errorf("close plugin settings: %w", err)
	}
	if err := os.Rename(tempName, store.filename(manifest.ID)); err != nil {
		return nil, fmt.Errorf("replace plugin settings: %w", err)
	}
	delete(store.warnings, manifest.ID)
	remove = false
	return merged, nil
}

func (store *pluginSettingsStore) filename(pluginID string) string {
	return filepath.Join(store.dir, pluginID+".json")
}

func (store *pluginSettingsStore) warning(pluginID string) string {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.warnings[pluginID]
}

func defaultPluginSettings(manifest *pluginManifest) (map[string]any, error) {
	values := make(map[string]any)
	for _, contribution := range manifest.Contributes.Settings {
		for _, field := range contribution.Fields {
			value, err := pluginSettingDefaultValue(field)
			if err != nil {
				return nil, fmt.Errorf("%s.%s default: %w", contribution.ID, field.Key, err)
			}
			values[field.Key] = value
		}
	}
	return values, nil
}

func mergePluginSettings(defaults, candidate map[string]any, manifest *pluginManifest) map[string]any {
	merged := make(map[string]any, len(defaults))
	for key, value := range defaults {
		merged[key] = value
	}
	for _, contribution := range manifest.Contributes.Settings {
		for _, field := range contribution.Fields {
			value, exists := candidate[field.Key]
			if !exists {
				continue
			}
			normalized, ok := normalizePluginSettingValue(field, value)
			if ok {
				merged[field.Key] = normalized
			}
		}
	}
	return merged
}

func normalizePluginSettingValue(field pluginSettingField, value any) (any, bool) {
	switch field.Type {
	case "string":
		text, ok := value.(string)
		return text, ok
	case "boolean":
		boolean, ok := value.(bool)
		return boolean, ok
	case "number":
		switch number := value.(type) {
		case float64:
			return number, true
		case float32:
			return float64(number), true
		case int:
			return float64(number), true
		case int64:
			return float64(number), true
		case json.Number:
			parsed, err := number.Float64()
			return parsed, err == nil
		default:
			return nil, false
		}
	case "select":
		text, ok := value.(string)
		if !ok {
			return nil, false
		}
		for _, option := range field.Options {
			if option == text {
				return text, true
			}
		}
	}
	return nil, false
}

func pluginAllSettingFields(manifest *pluginManifest) []pluginSettingField {
	var fields []pluginSettingField
	for _, contribution := range manifest.Contributes.Settings {
		fields = append(fields, contribution.Fields...)
	}
	return fields
}
