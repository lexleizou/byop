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
			if extensionError := validatePluginAgentExtension(entry); extensionError != "" {
				entry.Status = pluginInvalidManifest
				entry.Error = extensionError
				registry.Entries = append(registry.Entries, entry)
				continue
			}
			if webViewError := validatePluginWebViewEntries(entry); webViewError != "" {
				entry.Status = pluginInvalidManifest
				entry.Error = webViewError
				registry.Entries = append(registry.Entries, entry)
				continue
			}
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
	markModelLabelConflicts(&registry)
	return registry
}

func validatePluginAgentExtension(entry pluginRegistryEntry) string {
	if entry.Manifest == nil {
		return ""
	}
	for _, file := range []struct {
		label string
		path  string
	}{
		{label: "agent extension", path: entry.Manifest.Contributes.AgentExtension},
		{label: "agent proxy", path: entry.Manifest.Contributes.AgentProxy},
	} {
		if file.path == "" {
			continue
		}
		if _, _, err := resolvePluginPath(entry.SourceDirectory, file.path); err != nil {
			return fmt.Sprintf("%s: %v", file.label, err)
		}
	}
	return ""
}

func validatePluginWebViewEntries(entry pluginRegistryEntry) string {
	if entry.Manifest == nil {
		return "plugin manifest is unavailable"
	}
	for _, webView := range entry.Manifest.Contributes.WebViews {
		if _, _, err := resolvePluginPath(entry.SourceDirectory, webView.Entry); err != nil {
			return fmt.Sprintf("webview %s entry: %v", webView.ID, err)
		}
	}
	return ""
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
	for _, contribution := range manifest.Contributes.Automations {
		ids = append(ids, contribution.ID)
	}
	for _, contribution := range manifest.Contributes.ModelLabels {
		ids = append(ids, contribution.ID)
	}
	return ids
}

// modelLabelBadge returns the release marker a registered plugin declared for
// the model the composer shows. Entries are already sorted by plugin id, so the
// first claim wins and the result stays stable across rescans. An empty string
// means no plugin claimed this model.
func (registry pluginRegistry) modelLabelBadge(provider, modelID, modelName string) string {
	for _, entry := range registry.Entries {
		if entry.Status != pluginRegistered || entry.Manifest == nil {
			continue
		}
		for _, contribution := range entry.Manifest.Contributes.ModelLabels {
			if pluginModelLabelMatches(contribution.Match, provider, modelID, modelName) {
				return strings.TrimSpace(contribution.Badge)
			}
		}
	}
	return ""
}

// pluginModelLabelMatches reports whether one contribution applies to a model.
// Declared values are compared case-insensitively; an empty ModelID or
// ModelName matches any value.
func pluginModelLabelMatches(match pluginModelMatch, provider, modelID, modelName string) bool {
	if !pluginModelProviderMatches(match.Providers, provider) {
		return false
	}
	return pluginModelIdentifierMatches(match.ModelID, modelID) &&
		pluginModelFieldMatches(match.ModelName, modelName)
}

func pluginModelProviderMatches(providers []string, provider string) bool {
	for _, candidate := range providers {
		if modelTokenEqual(candidate, provider) {
			return true
		}
	}
	return false
}

func pluginModelFieldMatches(declared, actual string) bool {
	if strings.TrimSpace(declared) == "" {
		return true
	}
	return modelTokenEqual(declared, actual)
}

// pluginModelIdentifierMatches compares model ids and also accepts a declared id
// equal to the part after the last slash, because Pi model ids may carry a
// channel prefix such as "opencode-go/deepseek-flash".
func pluginModelIdentifierMatches(declared, actual string) bool {
	if strings.TrimSpace(declared) == "" {
		return true
	}
	if modelTokenEqual(declared, actual) {
		return true
	}
	return modelTokenEqual(declared, modelIdentifierSuffix(actual))
}

func modelIdentifierSuffix(id string) string {
	if index := strings.LastIndex(id, "/"); index >= 0 {
		return id[index+1:]
	}
	return id
}

func modelTokenEqual(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

// modelLabelMatchesOverlap reports whether two match rules can select the same
// model. It is intentionally conservative: two rules count as overlapping only
// when every field is equal or wildcarded on one side.
func modelLabelMatchesOverlap(left, right pluginModelMatch) bool {
	if !modelLabelProviderSetsOverlap(left.Providers, right.Providers) {
		return false
	}
	return modelLabelIdentifierCompatible(left.ModelID, right.ModelID) &&
		modelLabelFieldCompatible(left.ModelName, right.ModelName)
}

// modelLabelIdentifierCompatible treats a channel-prefixed id as the same model
// as its bare id, so two claims cannot silently target one model.
func modelLabelIdentifierCompatible(left, right string) bool {
	if strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" {
		return true
	}
	if modelTokenEqual(left, right) {
		return true
	}
	return modelTokenEqual(modelIdentifierSuffix(left), modelIdentifierSuffix(right))
}

func modelLabelProviderSetsOverlap(left, right []string) bool {
	for _, candidate := range left {
		for _, other := range right {
			if modelTokenEqual(candidate, other) {
				return true
			}
		}
	}
	return false
}

func modelLabelFieldCompatible(left, right string) bool {
	if strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" {
		return true
	}
	return modelTokenEqual(left, right)
}

// markModelLabelConflicts records, as registry issues, the model label claims
// that overlap an earlier claim. The earlier plugin keeps the badge; the later
// one still registers, so its other contributions keep working.
func markModelLabelConflicts(registry *pluginRegistry) {
	type claim struct {
		pluginID       string
		contributionID string
		match          pluginModelMatch
	}
	var claims []claim
	for _, entry := range registry.Entries {
		if entry.Status != pluginRegistered || entry.Manifest == nil {
			continue
		}
		for _, contribution := range entry.Manifest.Contributes.ModelLabels {
			var blocking claim
			found := false
			for _, previous := range claims {
				if modelLabelMatchesOverlap(previous.match, contribution.Match) {
					blocking, found = previous, true
					break
				}
			}
			if found {
				registry.Issues = append(registry.Issues, pluginRegistryIssue{
					Path: entry.ManifestPath,
					Message: fmt.Sprintf("model label %q overlaps %s contribution %q; %s keeps the badge",
						contribution.ID, blocking.pluginID, blocking.contributionID, blocking.pluginID),
				})
				continue
			}
			claims = append(claims, claim{
				pluginID:       entry.ID,
				contributionID: contribution.ID,
				match:          contribution.Match,
			})
		}
	}
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
