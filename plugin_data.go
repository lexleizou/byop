package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	pluginDataDirectoryName         = "plugin-data"
	pluginRuntimeGenerationFilename = "runtime-generation.json"
)

type pluginRuntimeGenerationMarker struct {
	Version    int    `json:"version"`
	PluginID   string `json:"pluginId"`
	Generation int64  `json:"generation"`
	UpdatedAt  string `json:"updatedAt"`
}

func pluginDataDir(configDir, pluginID string) (string, error) {
	if !pluginIDPattern.MatchString(pluginID) {
		return "", fmt.Errorf("invalid plugin id %q", pluginID)
	}
	root, err := filepath.Abs(configDir)
	if err != nil {
		return "", fmt.Errorf("resolve plugin data root: %w", err)
	}
	directory := filepath.Join(root, pluginDataDirectoryName, pluginID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create plugin data directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return "", fmt.Errorf("set plugin data directory permissions: %w", err)
	}
	info, err := os.Stat(directory)
	if err != nil {
		return "", fmt.Errorf("stat plugin data directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("plugin data path is not a directory: %s", directory)
	}
	return directory, nil
}

func registeredPluginDataDirs(registry pluginRegistry, configDir string) (map[string]string, error) {
	directories := make(map[string]string)
	for _, entry := range registry.Entries {
		if entry.Status != pluginRegistered {
			continue
		}
		directory, err := pluginDataDir(configDir, entry.ID)
		if err != nil {
			return nil, fmt.Errorf("prepare data directory for %s: %w", entry.ID, err)
		}
		directories[entry.ID] = directory
	}
	return directories, nil
}

func pluginDataDirsJSON(directories map[string]string) (string, error) {
	data, err := json.Marshal(directories)
	if err != nil {
		return "", fmt.Errorf("encode plugin data directories: %w", err)
	}
	return string(data), nil
}

func writePluginRuntimeGeneration(configDir, pluginID string) (pluginRuntimeGenerationMarker, error) {
	directory, err := pluginDataDir(configDir, pluginID)
	if err != nil {
		return pluginRuntimeGenerationMarker{}, err
	}
	path := filepath.Join(directory, pluginRuntimeGenerationFilename)
	generation := time.Now().UnixNano()
	if data, readErr := os.ReadFile(path); readErr == nil {
		var previous pluginRuntimeGenerationMarker
		if json.Unmarshal(data, &previous) == nil && previous.Generation >= generation {
			generation = previous.Generation + 1
		}
	}
	marker := pluginRuntimeGenerationMarker{
		Version:    1,
		PluginID:   pluginID,
		Generation: generation,
		UpdatedAt:  time.Now().UTC().Format(time.RFC3339Nano),
	}
	data, err := json.Marshal(marker)
	if err != nil {
		return pluginRuntimeGenerationMarker{}, fmt.Errorf("encode runtime generation: %w", err)
	}
	if err := atomicWritePluginFile(path, data); err != nil {
		return pluginRuntimeGenerationMarker{}, fmt.Errorf("write runtime generation: %w", err)
	}
	return marker, nil
}

func registeredPiPluginSignature(registry pluginRegistry) string {
	var entries []string
	for _, entry := range registry.Entries {
		if entry.Status != pluginRegistered || entry.Manifest == nil {
			continue
		}
		entries = append(entries, registeredPluginPiEntrySignature(entry))
	}
	return strings.Join(entries, "\x00")
}

func registeredPluginPiEntrySignature(entry pluginRegistryEntry) string {
	if entry.Status != pluginRegistered || entry.Manifest == nil {
		return entry.ID + "="
	}
	var runtimeFiles []string
	for _, runtimeFile := range []string{
		entry.Manifest.Contributes.AgentExtension,
		entry.Manifest.Contributes.AgentProxy,
	} {
		signature := ""
		if runtimeFile != "" {
			if resolved, info, err := resolvePluginPath(entry.SourceDirectory, runtimeFile); err == nil {
				signature = fmt.Sprintf("%s@%d:%d", resolved, info.ModTime().UnixNano(), info.Size())
			}
		}
		runtimeFiles = append(runtimeFiles, signature)
	}
	return entry.ID + "=" + strings.Join(runtimeFiles, "|")
}

func (s *appState) refreshPiPluginRuntimeAfterScan(before, after string) {
	if s == nil || s.pi == nil || before == after {
		return
	}
	// Extensions and their stable proxies are loaded with the Pi process. Keep
	// the current process intact until the idle refresh can restore its session.
	s.pluginPiRefreshRequired = true
}

func (s *appState) refreshPiForPluginRuntime() {
	if s == nil || !s.pluginPiRefreshRequired {
		return
	}
	if s.pi == nil || s.piConn != piConnected {
		s.connectPi()
		return
	}
	s.markChannelRefreshPending()
	s.showNotice(text("pluginsPiRefreshRequested"))
}
