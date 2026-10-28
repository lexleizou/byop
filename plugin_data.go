package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const pluginDataDirectoryName = "plugin-data"

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

func registeredPiPluginSignature(registry pluginRegistry) string {
	var entries []string
	for _, entry := range registry.Entries {
		if entry.Status != pluginRegistered || entry.Manifest == nil {
			continue
		}
		extension := ""
		if entry.Manifest.Contributes.AgentExtension != "" {
			if resolved, info, err := resolvePluginPath(
				entry.SourceDirectory,
				entry.Manifest.Contributes.AgentExtension,
			); err == nil {
				extension = fmt.Sprintf("%s@%d:%d", resolved, info.ModTime().UnixNano(), info.Size())
			}
		}
		entries = append(entries, entry.ID+"="+extension)
	}
	return strings.Join(entries, "\x00")
}

func (s *appState) refreshPiPluginRuntimeAfterScan(before, after string) {
	if s == nil || s.pi == nil || before == after {
		return
	}
	// Pi discovers extensions only at process start. The existing channel
	// refresh restores the current session when idle and never interrupts a run.
	s.markChannelRefreshPending()
}
