package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
)

const (
	pluginManifestSchemaVersion = 1
	pluginAPIVersion            = "1"
	pluginHostVersion           = "0.4.1"
	pluginManifestFilename      = "plugin.json"
	maxPluginManifestBytes      = 1 << 20
)

type pluginManifest struct {
	SchemaVersion int                 `json:"schemaVersion"`
	ID            string              `json:"id"`
	Name          string              `json:"name"`
	Version       string              `json:"version"`
	Description   string              `json:"description,omitempty"`
	Runtime       string              `json:"runtime"`
	Entry         string              `json:"entry"`
	Icon          string              `json:"icon,omitempty"`
	Compatibility pluginCompatibility `json:"compatibility"`
	Contributes   pluginContributions `json:"contributes"`
	Activation    pluginActivation    `json:"activation,omitempty"`
	Capabilities  pluginCapabilities  `json:"capabilities,omitempty"`
}

type pluginCompatibility struct {
	Host      string `json:"host"`
	PluginAPI string `json:"pluginApi"`
}

type pluginContributions struct {
	Navigation []pluginNavigationContribution `json:"navigation,omitempty"`
	Commands   []pluginCommandContribution    `json:"commands,omitempty"`
	Views      []pluginViewContribution       `json:"views,omitempty"`
	TaskPanels []pluginTaskPanelContribution  `json:"taskPanels,omitempty"`
	Settings   []pluginSettingContribution    `json:"settings,omitempty"`
	WebViews   []pluginWebViewContribution    `json:"webviews,omitempty"`
}

type pluginNavigationContribution struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Icon  string `json:"icon"`
}

type pluginCommandContribution struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

type pluginViewContribution struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Location string `json:"location"`
	View     string `json:"view"`
}

type pluginTaskPanelContribution struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type pluginSettingContribution struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type pluginWebViewContribution struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Location string `json:"location"`
}

type pluginActivation struct {
	Events []string `json:"events,omitempty"`
	Lazy   bool     `json:"lazy,omitempty"`
}

type pluginCapabilities struct {
	Workspace string `json:"workspace,omitempty"`
	Files     string `json:"files,omitempty"`
	Network   bool   `json:"network,omitempty"`
	Process   string `json:"process,omitempty"`
}

var (
	pluginIDPattern        = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,126}[a-z0-9]$`)
	contributionIDPattern  = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]{0,126}[a-z0-9])?$`)
	pluginVersionPattern   = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	iconPattern            = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	controlCharacterFilter = regexp.MustCompile(`[\x00-\x1f\x7f]`)
)

func loadPluginManifest(filename string) (*pluginManifest, error) {
	info, err := os.Stat(filename)
	if err != nil {
		return nil, fmt.Errorf("read manifest metadata: %w", err)
	}
	if info.Size() > maxPluginManifestBytes {
		return nil, fmt.Errorf("manifest exceeds %d bytes", maxPluginManifestBytes)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest pluginManifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("parse manifest: more than one JSON value")
		}
		return nil, fmt.Errorf("parse manifest trailing data: %w", err)
	}
	if err := validatePluginManifest(&manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func validatePluginManifest(manifest *pluginManifest) error {
	if manifest == nil {
		return errors.New("manifest is empty")
	}
	var issues []string
	add := func(condition bool, message string) {
		if condition {
			issues = append(issues, message)
		}
	}

	add(manifest.SchemaVersion != pluginManifestSchemaVersion,
		fmt.Sprintf("schemaVersion must be %d", pluginManifestSchemaVersion))
	add(!pluginIDPattern.MatchString(manifest.ID),
		"id must be 2-128 lowercase letters, digits, dots, underscores, or hyphens")
	add(pluginRequiredTextInvalid(manifest.Name, 80), "name must contain 1-80 printable characters")
	add(pluginTextInvalid(manifest.Description, 500), "description must contain at most 500 printable characters")
	add(!pluginVersionPattern.MatchString(manifest.Version), "version must be semantic versioning such as 0.1.0")
	add(manifest.Runtime != "node" && manifest.Runtime != "native",
		"runtime must be node or native")
	add(!safePluginRelativePath(manifest.Entry), "entry must be a safe relative path inside the plugin folder")
	add(manifest.Icon != "" && !iconPattern.MatchString(manifest.Icon),
		"icon must be a lowercase host icon name")
	add(strings.TrimSpace(manifest.Compatibility.Host) == "",
		"compatibility.host is required")
	if manifest.Compatibility.Host != "" {
		add(!validPluginHostRange(manifest.Compatibility.Host),
			"compatibility.host must use space-separated semantic version constraints")
	}
	add(strings.TrimSpace(manifest.Compatibility.PluginAPI) == "", "compatibility.pluginApi is required")

	seenContributions := make(map[string]struct{})
	validateContribution := func(kind, id, title string) {
		label := kind + " contribution"
		if !contributionIDPattern.MatchString(id) {
			issues = append(issues, label+" id is invalid")
		}
		if pluginRequiredTextInvalid(title, 80) {
			issues = append(issues, label+" title must contain 1-80 printable characters")
		}
		if _, exists := seenContributions[id]; id != "" && exists {
			issues = append(issues, "contribution id is duplicated: "+id)
		} else if id != "" {
			seenContributions[id] = struct{}{}
		}
	}

	for _, contribution := range manifest.Contributes.Navigation {
		validateContribution("navigation", contribution.ID, contribution.Title)
		if !iconPattern.MatchString(contribution.Icon) {
			issues = append(issues, "navigation contribution icon is invalid: "+contribution.ID)
		}
	}
	for _, contribution := range manifest.Contributes.Commands {
		validateContribution("command", contribution.ID, contribution.Title)
		if pluginTextInvalid(contribution.Description, 300) {
			issues = append(issues, "command contribution description is too long: "+contribution.ID)
		}
	}
	for _, contribution := range manifest.Contributes.Views {
		validateContribution("view", contribution.ID, contribution.Title)
		if !validPluginSurfaceLocation(contribution.Location) {
			issues = append(issues, "view contribution location is invalid: "+contribution.ID)
		}
		if contribution.View != "native" {
			issues = append(issues, "view contribution type must be native: "+contribution.ID)
		}
	}
	for _, contribution := range manifest.Contributes.TaskPanels {
		validateContribution("task panel", contribution.ID, contribution.Title)
	}
	for _, contribution := range manifest.Contributes.Settings {
		validateContribution("setting", contribution.ID, contribution.Title)
	}
	for _, contribution := range manifest.Contributes.WebViews {
		validateContribution("webview", contribution.ID, contribution.Title)
		if !validPluginSurfaceLocation(contribution.Location) {
			issues = append(issues, "webview contribution location is invalid: "+contribution.ID)
		}
	}
	for _, event := range manifest.Activation.Events {
		if pluginTextInvalid(event, 180) || !strings.Contains(event, ":") {
			issues = append(issues, "activation event must contain a printable type and value")
		}
	}
	if !validCapabilityLevel(manifest.Capabilities.Workspace) {
		issues = append(issues, "capabilities.workspace must be empty, none, read, or write")
	}
	if !validCapabilityLevel(manifest.Capabilities.Files) {
		issues = append(issues, "capabilities.files must be empty, none, read, or write")
	}
	if manifest.Capabilities.Process != "" && manifest.Capabilities.Process != "none" &&
		manifest.Capabilities.Process != "execute" {
		issues = append(issues, "capabilities.process must be empty, none, or execute")
	}

	if len(issues) > 0 {
		return errors.New(strings.Join(issues, "; "))
	}
	return nil
}

func pluginTextInvalid(value string, maximum int) bool {
	value = strings.TrimSpace(value)
	if maximum > 0 && len([]rune(value)) > maximum {
		return true
	}
	return controlCharacterFilter.MatchString(value)
}

func pluginRequiredTextInvalid(value string, maximum int) bool {
	return strings.TrimSpace(value) == "" || pluginTextInvalid(value, maximum)
}

func safePluginRelativePath(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, `\`) || strings.ContainsRune(value, '\x00') || path.IsAbs(value) {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return false
		}
	}
	clean := path.Clean(value)
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func validPluginSurfaceLocation(location string) bool {
	switch location {
	case "main", "secondary":
		return true
	default:
		return false
	}
}

func validCapabilityLevel(value string) bool {
	switch value {
	case "", "none", "read", "write":
		return true
	default:
		return false
	}
}

func (manifest *pluginManifest) contributionCount() int {
	if manifest == nil {
		return 0
	}
	return len(manifest.Contributes.Navigation) +
		len(manifest.Contributes.Commands) +
		len(manifest.Contributes.Views) +
		len(manifest.Contributes.TaskPanels) +
		len(manifest.Contributes.Settings) +
		len(manifest.Contributes.WebViews)
}

type semanticVersion struct {
	major int
	minor int
	patch int
}

func parseSemanticVersion(value string) (semanticVersion, bool) {
	match := pluginVersionPattern.FindStringSubmatch(value)
	if match == nil {
		return semanticVersion{}, false
	}
	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	patchVersion, _ := strconv.Atoi(match[3])
	return semanticVersion{major: major, minor: minor, patch: patchVersion}, true
}

func compareSemanticVersions(left, right semanticVersion) int {
	switch {
	case left.major != right.major:
		return left.major - right.major
	case left.minor != right.minor:
		return left.minor - right.minor
	default:
		return left.patch - right.patch
	}
}

func validPluginHostRange(value string) bool {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return false
	}
	for _, constraint := range fields {
		_, version, ok := parseHostConstraint(constraint)
		if !ok {
			return false
		}
		if _, ok := parseSemanticVersion(version); !ok {
			return false
		}
	}
	return true
}

func hostRangeContains(value, hostVersion string) bool {
	host, ok := parseSemanticVersion(hostVersion)
	if !ok {
		return false
	}
	for _, constraint := range strings.Fields(value) {
		operator, rawVersion, ok := parseHostConstraint(constraint)
		if !ok {
			return false
		}
		required, ok := parseSemanticVersion(rawVersion)
		if !ok {
			return false
		}
		comparison := compareSemanticVersions(host, required)
		switch operator {
		case ">=":
			if comparison < 0 {
				return false
			}
		case "<=":
			if comparison > 0 {
				return false
			}
		case ">":
			if comparison <= 0 {
				return false
			}
		case "<":
			if comparison >= 0 {
				return false
			}
		case "=":
			if comparison != 0 {
				return false
			}
		}
	}
	return true
}

func parseHostConstraint(value string) (operator, version string, ok bool) {
	for _, candidate := range []string{">=", "<=", ">", "<", "="} {
		if strings.HasPrefix(value, candidate) {
			version = strings.TrimSpace(strings.TrimPrefix(value, candidate))
			return candidate, version, version != ""
		}
	}
	return "=", value, value != ""
}
