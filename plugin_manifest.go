package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	pluginManifestSchemaVersion = 1
	pluginAPIVersion            = "1"
	pluginHostVersion           = appVersion
	pluginManifestFilename      = "plugin.json"
	maxPluginManifestBytes      = 1 << 20
	maxPluginSettingFields      = 50

	modelLabelBadgeMaxRunes = 12
	modelLabelMatchMaxRunes = 120

	automationMinIntervalMinutes        = 5
	automationDefaultMaxDurationMinutes = 30
	automationMaxDurationMinutes        = 1440
	automationMaxConsecutiveFailures    = 5
	automationRunsKeptPerTask           = 200
)

type pluginManifest struct {
	SchemaVersion int                 `json:"schemaVersion"`
	ID            string              `json:"id"`
	Name          string              `json:"name"`
	Version       string              `json:"version"`
	Description   string              `json:"description,omitempty"`
	L10n          string              `json:"l10n,omitempty"`
	Runtime       string              `json:"runtime"`
	Entry         string              `json:"entry"`
	Icon          string              `json:"icon,omitempty"`
	Compatibility pluginCompatibility `json:"compatibility"`
	Contributes   pluginContributions `json:"contributes"`
	Activation    pluginActivation    `json:"activation,omitempty"`
	Capabilities  pluginCapabilities  `json:"capabilities,omitempty"`

	sourceDirectory string
	localization    *pluginLocalizationCache
}

type pluginLocalizationCache struct {
	mu      sync.Mutex
	locales map[string]map[string]string
}

type pluginCompatibility struct {
	Host      string `json:"host"`
	PluginAPI string `json:"pluginApi"`
}

type pluginContributions struct {
	Navigation     []pluginNavigationContribution `json:"navigation,omitempty"`
	Commands       []pluginCommandContribution    `json:"commands,omitempty"`
	Views          []pluginViewContribution       `json:"views,omitempty"`
	TaskPanels     []pluginTaskPanelContribution  `json:"taskPanels,omitempty"`
	Settings       []pluginSettingContribution    `json:"settings,omitempty"`
	WebViews       []pluginWebViewContribution    `json:"webviews,omitempty"`
	Automations    []pluginAutomationContribution `json:"automations,omitempty"`
	ModelLabels    []pluginModelLabelContribution `json:"modelLabels,omitempty"`
	AgentExtension string                         `json:"agentExtension,omitempty"`
	AgentProxy     string                         `json:"agentProxy,omitempty"`
}

type pluginNavigationContribution struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Icon  string `json:"icon"`
	View  string `json:"view,omitempty"`
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
	ID          string               `json:"id"`
	Title       string               `json:"title"`
	Description string               `json:"description,omitempty"`
	Fields      []pluginSettingField `json:"fields,omitempty"`
}

type pluginSettingField struct {
	Key         string          `json:"key"`
	Label       string          `json:"label"`
	Type        string          `json:"type"`
	Description string          `json:"description,omitempty"`
	Default     json.RawMessage `json:"default,omitempty"`
	Options     []string        `json:"options,omitempty"`
	Placeholder string          `json:"placeholder,omitempty"`
}

type pluginWebViewContribution struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Location string `json:"location"`
	Entry    string `json:"entry"`
}

type pluginAutomationContribution struct {
	ID                 string                `json:"id"`
	Title              string                `json:"title"`
	Description        string                `json:"description,omitempty"`
	Command            string                `json:"command"`
	Kind               string                `json:"kind"`
	Rule               *pluginAutomationRule `json:"rule,omitempty"`
	MissedPolicy       string                `json:"missedPolicy,omitempty"`
	MaxDurationMinutes int                   `json:"maxDurationMinutes,omitempty"`
}

type pluginAutomationRule struct {
	Type    string `json:"type"`
	Minutes int    `json:"minutes,omitempty"`
	Cron    string `json:"cron,omitempty"`
	At      string `json:"at,omitempty"`
}

// pluginModelLabelContribution declares a fixed release marker shown after the
// model name in the composer, for example the 1017 snapshot of an official
// Flash model. The host renders it; the plugin only supplies the text and the
// match rule, mirroring how DeepSeek Harness ships its temporary label plugin.
type pluginModelLabelContribution struct {
	ID    string           `json:"id"`
	Badge string           `json:"badge"`
	Match pluginModelMatch `json:"match"`
}

// pluginModelMatch names the models a label applies to. An empty ModelID or
// ModelName matches any value for that field; Providers always has to match.
type pluginModelMatch struct {
	Providers []string `json:"providers,omitempty"`
	ModelID   string   `json:"modelId,omitempty"`
	ModelName string   `json:"modelName,omitempty"`
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
	settingKeyPattern      = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,63}$`)
	pluginVersionPattern   = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	iconPattern            = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	modelProviderPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
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
	manifest.sourceDirectory = filepath.Dir(filename)
	manifest.localization = &pluginLocalizationCache{
		locales: make(map[string]map[string]string),
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
	add(manifest.L10n != "" && !safePluginRelativePath(manifest.L10n),
		"l10n must be a safe relative directory inside the plugin folder")
	add(manifest.Contributes.AgentExtension != "" &&
		(!safePluginRelativePath(manifest.Contributes.AgentExtension) ||
			path.Clean(manifest.Contributes.AgentExtension) != manifest.Contributes.AgentExtension),
		"contributes.agentExtension must be a safe relative path inside the plugin folder")
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
	declaredViews := make(map[string]struct{},
		len(manifest.Contributes.Views)+len(manifest.Contributes.WebViews))
	for _, contribution := range manifest.Contributes.Views {
		if contribution.ID != "" {
			declaredViews[contribution.ID] = struct{}{}
		}
	}
	for _, contribution := range manifest.Contributes.WebViews {
		if contribution.ID != "" {
			declaredViews[contribution.ID] = struct{}{}
		}
	}
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
		if contribution.View != "" {
			if !contributionIDPattern.MatchString(contribution.View) {
				issues = append(issues, "navigation view id is invalid: "+contribution.ID)
			} else if _, exists := declaredViews[contribution.View]; !exists {
				issues = append(issues, "navigation view is not declared: "+contribution.View)
			}
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
		if pluginTextInvalid(contribution.Description, 300) {
			issues = append(issues, "setting contribution description is too long: "+contribution.ID)
		}
	}
	for _, contribution := range manifest.Contributes.WebViews {
		validateContribution("webview", contribution.ID, contribution.Title)
		if contribution.Location != "main" {
			issues = append(issues, "webview contribution location is invalid: "+contribution.ID)
		}
		if !validPluginWebViewEntry(contribution.Entry) {
			issues = append(issues, "webview entry must be a clean relative HTML path inside the plugin folder: "+
				contribution.ID)
		}
	}
	for _, contribution := range manifest.Contributes.Automations {
		validateContribution("automation", contribution.ID, contribution.Title)
		if pluginTextInvalid(contribution.Description, 300) {
			issues = append(issues, "automation contribution description is too long: "+contribution.ID)
		}
		if !pluginDeclaresCommand(manifest, contribution.Command) {
			issues = append(issues, "automation command is not declared: "+contribution.ID)
		}
		if !pluginDeclaresActivationEvent(manifest, "onCommand:"+contribution.Command) {
			issues = append(issues, "automation command lacks activation event: "+contribution.ID)
		}
		validatePluginAutomationRule(contribution, &issues)
		if contribution.MaxDurationMinutes != 0 &&
			(contribution.MaxDurationMinutes < 1 ||
				contribution.MaxDurationMinutes > automationMaxDurationMinutes) {
			issues = append(issues, "automation maxDurationMinutes must be 0 or 1-1440: "+contribution.ID)
		}
	}
	for _, contribution := range manifest.Contributes.ModelLabels {
		label := "model label contribution " + fallback(contribution.ID, "<missing id>")
		if !contributionIDPattern.MatchString(contribution.ID) {
			issues = append(issues, label+" id is invalid")
		} else if _, exists := seenContributions[contribution.ID]; exists {
			issues = append(issues, "contribution id is duplicated: "+contribution.ID)
		} else {
			seenContributions[contribution.ID] = struct{}{}
		}
		if strings.TrimSpace(contribution.Badge) == "" ||
			contribution.Badge != strings.TrimSpace(contribution.Badge) ||
			pluginTextInvalid(contribution.Badge, modelLabelBadgeMaxRunes) {
			issues = append(issues, label+
				" badge must contain 1-12 printable characters without surrounding spaces")
		}
		if len(contribution.Match.Providers) == 0 {
			issues = append(issues, label+" match needs at least one provider")
		}
		seenProviders := make(map[string]struct{}, len(contribution.Match.Providers))
		for _, provider := range contribution.Match.Providers {
			if !modelProviderPattern.MatchString(provider) {
				issues = append(issues, label+" match provider is invalid: "+fallback(provider, "<empty>"))
				continue
			}
			if _, exists := seenProviders[strings.ToLower(provider)]; exists {
				issues = append(issues, label+" match provider is duplicated: "+provider)
				continue
			}
			seenProviders[strings.ToLower(provider)] = struct{}{}
		}
		if strings.TrimSpace(contribution.Match.ModelID) == "" &&
			strings.TrimSpace(contribution.Match.ModelName) == "" {
			issues = append(issues, label+" match needs modelId or modelName")
		}
		if pluginTextInvalid(contribution.Match.ModelID, modelLabelMatchMaxRunes) {
			issues = append(issues, label+" match modelId is invalid")
		}
		if pluginTextInvalid(contribution.Match.ModelName, modelLabelMatchMaxRunes) {
			issues = append(issues, label+" match modelName is invalid")
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
	validatePluginSettingFields(manifest, &issues)

	if len(issues) > 0 {
		return errors.New(strings.Join(issues, "; "))
	}
	return nil
}

func validatePluginSettingFields(manifest *pluginManifest, issues *[]string) {
	seenKeys := make(map[string]struct{})
	fieldCount := 0
	for _, contribution := range manifest.Contributes.Settings {
		for _, field := range contribution.Fields {
			fieldCount++
			label := "setting field " + contribution.ID + "." + fallback(field.Key, "<missing>")
			if !settingKeyPattern.MatchString(field.Key) {
				*issues = append(*issues, label+" key is invalid")
			} else if _, exists := seenKeys[field.Key]; exists {
				*issues = append(*issues, "setting field key is duplicated: "+field.Key)
			} else {
				seenKeys[field.Key] = struct{}{}
			}
			if pluginRequiredTextInvalid(field.Label, 80) {
				*issues = append(*issues, label+" label must contain 1-80 printable characters")
			}
			switch field.Type {
			case "string", "boolean", "number", "select":
			default:
				*issues = append(*issues, label+" type must be string, boolean, number, or select")
			}
			if pluginTextInvalid(field.Description, 300) {
				*issues = append(*issues, label+" description is too long")
			}
			if pluginTextInvalid(field.Placeholder, 120) {
				*issues = append(*issues, label+" placeholder is too long")
			}
			if field.Type == "select" {
				if len(field.Options) == 0 {
					*issues = append(*issues, label+" select options are required")
				}
				optionSeen := make(map[string]struct{}, len(field.Options))
				for _, option := range field.Options {
					if pluginRequiredTextInvalid(option, 80) {
						*issues = append(*issues, label+" select option must contain 1-80 printable characters")
						continue
					}
					if _, exists := optionSeen[option]; exists {
						*issues = append(*issues, label+" select options must be unique")
					}
					optionSeen[option] = struct{}{}
				}
			} else if len(field.Options) > 0 {
				*issues = append(*issues, label+" options are only valid for select fields")
			}
			if _, err := pluginSettingDefaultValue(field); err != nil {
				*issues = append(*issues, label+" default "+err.Error())
			}
		}
	}
	if fieldCount > maxPluginSettingFields {
		*issues = append(*issues, fmt.Sprintf("plugin declares %d setting fields; maximum is %d",
			fieldCount, maxPluginSettingFields))
	}
}

func validatePluginAutomationRule(contribution pluginAutomationContribution, issues *[]string) {
	label := "automation contribution " + fallback(contribution.ID, "<missing>")
	switch contribution.Kind {
	case "manual":
		if contribution.Rule != nil {
			*issues = append(*issues, label+" rule must be absent for manual")
		}
	case "once":
		if contribution.Rule == nil {
			*issues = append(*issues, label+" rule is required")
		} else if contribution.Rule.Type != "at" {
			*issues = append(*issues, label+" rule type must be at for once")
		}
	case "scheduledLoop", "sessionLoop":
		if contribution.Rule == nil {
			*issues = append(*issues, label+" rule is required")
		} else {
			switch contribution.Rule.Type {
			case "interval", "cron":
			case "afterSettled":
				if contribution.Kind != "sessionLoop" {
					*issues = append(*issues, label+" afterSettled is only valid for sessionLoop")
				}
			default:
				if contribution.Kind == "sessionLoop" {
					*issues = append(*issues, label+" rule type must be interval, cron, or afterSettled")
				} else {
					*issues = append(*issues, label+" rule type must be interval or cron")
				}
			}
		}
	default:
		*issues = append(*issues, label+" kind must be manual, once, scheduledLoop, or sessionLoop")
	}

	if contribution.Rule == nil {
		validatePluginAutomationMissedPolicy(contribution.MissedPolicy, label, issues)
		return
	}

	rule := contribution.Rule
	if contribution.Kind == "manual" {
		if rule.Minutes != 0 || rule.Cron != "" || rule.At != "" {
			*issues = append(*issues, label+" manual rule has unused fields")
		}
		validatePluginAutomationMissedPolicy(contribution.MissedPolicy, label, issues)
		return
	}
	switch rule.Type {
	case "interval":
		if rule.Minutes < automationMinIntervalMinutes {
			*issues = append(*issues, label+" interval minutes must be at least 5")
		}
		if rule.Cron != "" || rule.At != "" {
			*issues = append(*issues, label+" interval rule has unused fields")
		}
	case "cron":
		if len(strings.Fields(rule.Cron)) != 5 || pluginTextInvalid(rule.Cron, 120) {
			*issues = append(*issues, label+" cron must contain exactly 5 printable fields")
		}
		if rule.Minutes != 0 || rule.At != "" {
			*issues = append(*issues, label+" cron rule has unused fields")
		}
	case "at":
		if _, err := time.Parse(time.RFC3339, rule.At); err != nil {
			*issues = append(*issues, label+" at must be RFC3339")
		}
		if rule.Minutes != 0 || rule.Cron != "" {
			*issues = append(*issues, label+" at rule has unused fields")
		}
	case "afterSettled":
		if rule.Minutes != 0 || rule.Cron != "" || rule.At != "" {
			*issues = append(*issues, label+" afterSettled rule has unused fields")
		}
	default:
		if rule.Minutes != 0 || rule.Cron != "" || rule.At != "" {
			*issues = append(*issues, label+" unknown rule has unused fields")
		}
	}
	validatePluginAutomationMissedPolicy(contribution.MissedPolicy, label, issues)
}

func validatePluginAutomationMissedPolicy(policy, label string, issues *[]string) {
	if policy != "" && policy != "catchUpLatest" && policy != "skip" {
		*issues = append(*issues, label+" missedPolicy must be catchUpLatest or skip")
	}
}

func pluginSettingDefaultValue(field pluginSettingField) (any, error) {
	if len(field.Default) == 0 {
		switch field.Type {
		case "string":
			return "", nil
		case "boolean":
			return false, nil
		case "number":
			return float64(0), nil
		case "select":
			if len(field.Options) == 0 {
				return nil, errors.New("requires a select option")
			}
			return field.Options[0], nil
		default:
			return nil, nil
		}
	}
	if bytes.Equal(bytes.TrimSpace(field.Default), []byte("null")) {
		return nil, errors.New("must not be null")
	}
	switch field.Type {
	case "string":
		var value string
		if err := json.Unmarshal(field.Default, &value); err != nil {
			return nil, errors.New("must be a string")
		}
		return value, nil
	case "boolean":
		var value bool
		if err := json.Unmarshal(field.Default, &value); err != nil {
			return nil, errors.New("must be a boolean")
		}
		return value, nil
	case "number":
		var value float64
		if err := json.Unmarshal(field.Default, &value); err != nil {
			return nil, errors.New("must be a number")
		}
		return value, nil
	case "select":
		var value string
		if err := json.Unmarshal(field.Default, &value); err != nil {
			return nil, errors.New("must be a select option")
		}
		for _, option := range field.Options {
			if option == value {
				return value, nil
			}
		}
		return nil, errors.New("must be one of the select options")
	default:
		return nil, nil
	}
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

func pluginDeclaresActivationEvent(manifest *pluginManifest, event string) bool {
	for _, declared := range manifest.Activation.Events {
		if declared == event {
			return true
		}
	}
	return false
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

func validPluginWebViewEntry(entry string) bool {
	if !safePluginRelativePath(entry) || path.Clean(entry) != entry {
		return false
	}
	switch strings.ToLower(path.Ext(entry)) {
	case ".html", ".htm":
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
		len(manifest.Contributes.WebViews) +
		len(manifest.Contributes.Automations) +
		len(manifest.Contributes.ModelLabels)
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
