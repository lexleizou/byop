package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type pluginRegistryStatus string

const (
	pluginRegistered            pluginRegistryStatus = "registered"
	pluginInvalidManifest       pluginRegistryStatus = "invalid-manifest"
	pluginIncompatible          pluginRegistryStatus = "incompatible"
	pluginDuplicateID           pluginRegistryStatus = "duplicate-plugin"
	pluginDuplicateContribution pluginRegistryStatus = "duplicate-contribution"
)

type pluginRootSpec struct {
	Path     string
	Explicit bool
}

type pluginRegistryIssue struct {
	Path    string
	Message string
}

type pluginRegistryEntry struct {
	ID                string
	Name              string
	Version           string
	Description       string
	SourceDirectory   string
	ManifestPath      string
	Manifest          *pluginManifest
	Status            pluginRegistryStatus
	Error             string
	ContributionCount int
}

type pluginRegistry struct {
	HostVersion string
	PluginAPI   string
	Roots       []string
	Entries     []pluginRegistryEntry
	Issues      []pluginRegistryIssue
	ScannedAt   time.Time
}

func defaultPluginRoots() []pluginRootSpec {
	var specs []pluginRootSpec
	if configured := strings.TrimSpace(os.Getenv("BYOP_PLUGINS_DIR")); configured != "" {
		for _, root := range filepath.SplitList(configured) {
			if strings.TrimSpace(root) != "" {
				specs = append(specs, pluginRootSpec{Path: root, Explicit: true})
			}
		}
		return uniquePluginRoots(specs)
	}
	if home, err := os.UserHomeDir(); err == nil {
		specs = append(specs, pluginRootSpec{Path: filepath.Join(home, ".byop", "plugins")})
	}
	if cwd, err := os.Getwd(); err == nil {
		specs = append(specs, pluginRootSpec{Path: filepath.Join(cwd, ".byop", "plugins")})
	}
	return uniquePluginRoots(specs)
}

func uniquePluginRoots(specs []pluginRootSpec) []pluginRootSpec {
	seen := make(map[string]struct{}, len(specs))
	result := make([]pluginRootSpec, 0, len(specs))
	for _, spec := range specs {
		cleaned := filepath.Clean(spec.Path)
		if absolute, err := filepath.Abs(cleaned); err == nil {
			cleaned = absolute
		}
		if _, exists := seen[cleaned]; exists {
			continue
		}
		seen[cleaned] = struct{}{}
		result = append(result, pluginRootSpec{Path: cleaned, Explicit: spec.Explicit})
	}
	return result
}

func scanPluginRegistry(rootSpecs []pluginRootSpec) pluginRegistry {
	registry := pluginRegistry{
		HostVersion: pluginHostVersion,
		PluginAPI:   pluginAPIVersion,
		ScannedAt:   time.Now(),
	}
	for _, spec := range uniquePluginRoots(rootSpecs) {
		registry.Roots = append(registry.Roots, spec.Path)
		directories, err := os.ReadDir(spec.Path)
		if err != nil {
			if !os.IsNotExist(err) || spec.Explicit {
				registry.Issues = append(registry.Issues, pluginRegistryIssue{
					Path:    spec.Path,
					Message: err.Error(),
				})
			}
			continue
		}
		for _, directory := range directories {
			if !directory.IsDir() {
				continue
			}
			sourceDirectory := filepath.Join(spec.Path, directory.Name())
			manifestPath := filepath.Join(sourceDirectory, pluginManifestFilename)
			if _, err := os.Stat(manifestPath); err != nil {
				if !os.IsNotExist(err) {
					registry.Issues = append(registry.Issues, pluginRegistryIssue{
						Path:    manifestPath,
						Message: err.Error(),
					})
				}
				continue
			}
			entry := pluginRegistryEntry{
				ID:              directory.Name(),
				Name:            directory.Name(),
				SourceDirectory: sourceDirectory,
				ManifestPath:    manifestPath,
			}
			manifest, err := loadPluginManifest(manifestPath)
			if err != nil {
				entry.Status = pluginInvalidManifest
				entry.Error = err.Error()
				registry.Entries = append(registry.Entries, entry)
				continue
			}
			entry.ID = manifest.ID
			entry.Name = manifest.Name
			entry.Version = manifest.Version
			entry.Description = manifest.Description
			entry.Manifest = manifest
			entry.ContributionCount = manifest.contributionCount()
			if compatibilityError := pluginCompatibilityError(manifest); compatibilityError != "" {
				entry.Status = pluginIncompatible
				entry.Error = compatibilityError
			} else {
				entry.Status = pluginRegistered
			}
			registry.Entries = append(registry.Entries, entry)
		}
	}

	markDuplicatePluginIDs(&registry)
	markDuplicateContributions(&registry)
	sort.SliceStable(registry.Entries, func(i, j int) bool {
		left, right := registry.Entries[i], registry.Entries[j]
		if left.ID != right.ID {
			return left.ID < right.ID
		}
		return left.SourceDirectory < right.SourceDirectory
	})
	return registry
}

func pluginCompatibilityError(manifest *pluginManifest) string {
	if manifest.Compatibility.PluginAPI != pluginAPIVersion {
		return fmt.Sprintf("requires pluginApi %s; host provides %s",
			manifest.Compatibility.PluginAPI, pluginAPIVersion)
	}
	if !hostRangeContains(manifest.Compatibility.Host, pluginHostVersion) {
		return fmt.Sprintf("requires host %s; current host is %s",
			manifest.Compatibility.Host, pluginHostVersion)
	}
	return ""
}

func markDuplicatePluginIDs(registry *pluginRegistry) {
	seen := make(map[string]string)
	for index := range registry.Entries {
		entry := &registry.Entries[index]
		if entry.Status != pluginRegistered {
			continue
		}
		if firstPath, exists := seen[entry.ID]; exists {
			entry.Status = pluginDuplicateID
			entry.Error = fmt.Sprintf("plugin id %q is already registered from %s", entry.ID, firstPath)
			continue
		}
		seen[entry.ID] = entry.SourceDirectory
	}
}

func markDuplicateContributions(registry *pluginRegistry) {
	seen := make(map[string]string)
	for index := range registry.Entries {
		entry := &registry.Entries[index]
		if entry.Status != pluginRegistered || entry.Manifest == nil {
			continue
		}
		for _, contributionID := range pluginContributionIDs(entry.Manifest) {
			if firstPlugin, exists := seen[contributionID]; exists {
				entry.Status = pluginDuplicateContribution
				entry.Error = fmt.Sprintf("contribution id %q is already registered by %s", contributionID, firstPlugin)
				break
			}
			seen[contributionID] = entry.ID
		}
	}
}

func pluginContributionIDs(manifest *pluginManifest) []string {
	var ids []string
	for _, contribution := range manifest.Contributes.Navigation {
		ids = append(ids, contribution.ID)
	}
	for _, contribution := range manifest.Contributes.Commands {
		ids = append(ids, contribution.ID)
	}
	for _, contribution := range manifest.Contributes.Views {
		ids = append(ids, contribution.ID)
	}
	for _, contribution := range manifest.Contributes.TaskPanels {
		ids = append(ids, contribution.ID)
	}
	for _, contribution := range manifest.Contributes.Settings {
		ids = append(ids, contribution.ID)
	}
	for _, contribution := range manifest.Contributes.WebViews {
		ids = append(ids, contribution.ID)
	}
	return ids
}

func (registry pluginRegistry) registeredCount() int {
	count := 0
	for _, entry := range registry.Entries {
		if entry.Status == pluginRegistered {
			count++
		}
	}
	return count
}

func (registry pluginRegistry) contributionCount() int {
	count := 0
	for _, entry := range registry.Entries {
		if entry.Status == pluginRegistered {
			count += entry.ContributionCount
		}
	}
	return count
}

func (registry pluginRegistry) problemCount() int {
	count := len(registry.Issues)
	for _, entry := range registry.Entries {
		if entry.Status != pluginRegistered {
			count++
		}
	}
	return count
}
