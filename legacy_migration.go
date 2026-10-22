package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

type legacyImportStatus string

const (
	legacyStatusNew      legacyImportStatus = "new"
	legacyStatusSame     legacyImportStatus = "same"
	legacyStatusConflict legacyImportStatus = "conflict"
	legacyStatusKept     legacyImportStatus = "kept"
	legacyStatusLinked   legacyImportStatus = "linked"
	legacyStatusMissing  legacyImportStatus = "missing"
	legacyStatusInfo     legacyImportStatus = "info"
	legacyStatusIgnored  legacyImportStatus = "ignored"
	legacyStatusUnknown  legacyImportStatus = "unknown"
)

type legacyMigrationSources struct {
	AppDir         string
	LocalStorage   string
	PiAgentDir     string
	LocalStorageN  int
	LocalStorageAt time.Time
}

type legacySourcePreview struct {
	Source  string
	Status  string
	Summary string
}

type legacyPreviewEntry struct {
	ID      string
	Source  string
	Summary string
	Target  string
	Status  legacyImportStatus
	action  *legacyImportAction
}

type legacyImportActionKind string

const (
	legacyActionTheme       legacyImportActionKind = "theme"
	legacyActionWorkspace   legacyImportActionKind = "workspace"
	legacyActionSession     legacyImportActionKind = "session"
	legacyActionDraft       legacyImportActionKind = "draft"
	legacyActionLegacyValue legacyImportActionKind = "legacy"
)

type legacyImportAction struct {
	Kind        legacyImportActionKind
	Key         string
	LegacyKey   string
	Text        string
	Raw         json.RawMessage
	Source      string
	Overwrite   bool
	StoreOnSame bool
}

type legacyScan struct {
	Version         string
	WindowSummary   string
	Sources         []legacySourcePreview
	Entries         []legacyPreviewEntry
	Actions         []legacyImportAction
	ImportedAt      string
	LocalStorageN   int
	LocalStorageAt  time.Time
	Theme           themeMode
	Workspace       string
	WorkspaceSet    bool
	SourcePaths     legacyMigrationSources
	ScanWarning     string
	canImport       bool
	importableCount int
}

type legacyImportCounts struct {
	New      int `json:"new"`
	Same     int `json:"same"`
	Conflict int `json:"conflict"`
	Kept     int `json:"kept"`
	Linked   int `json:"linked"`
	Missing  int `json:"missing"`
	Unknown  int `json:"unknown"`
	Ignored  int `json:"ignored"`
	Info     int `json:"info"`
}

type legacyImportRecord struct {
	At            string             `json:"at"`
	LegacyVersion string             `json:"legacyVersion,omitempty"`
	Counts        legacyImportCounts `json:"counts"`
}

type legacyImportResult struct {
	Record          legacyImportRecord
	Theme           themeMode
	ThemeChanged    bool
	Workspace       string
	WorkspaceChange bool
}

type legacyImportPersistence struct {
	PreferencesSaved bool
	HistorySaved     bool
}

type legacyLocalStorageItem struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

const (
	legacyDraftsKey       = "pi-web:drafts"
	legacyDraftsSpecAlias = "pi-chat-drafts-v1"
)

var legacyKnownKeptKeys = map[string]struct{}{
	"pi-sidebar-width":              {},
	"pi-right-panel-width":          {},
	"pi-locale":                     {},
	"pi-sound-enabled":              {},
	"pi-quote-selection-enabled":    {},
	"pi-desktop-close-quits":        {},
	"pi-desktop-notify-on-complete": {},
	"pi-diff-view-mode":             {},
	"pi-enter-send-mode":            {},
	"pi-thinking-expanded":          {},
	"pi-output-language":            {},
	"pi-tool-preset":                {},
	"pi-web:archived-projects":      {},
	"pi-web:collapsed-projects":     {},
	"pi-web:expanded-projects":      {},
	"pi-web:project-view-mode":      {},
	"pi-web:unread-session-ids":     {},
	"pi-web:update-snooze":          {},
	"pi-web:file-explorer:open":     {},
	"pi-web:settings-navigation":    {},
	"pi-web:tab-open-session":       {},
	"pi-chat-content-font-size":     {},
	"pi-sidebar-scale":              {},
}

var legacyUIPrefsKeyMap = map[string]string{
	"theme":                 "pi-theme",
	"locale":                "pi-locale",
	"soundEnabled":          "pi-sound-enabled",
	"quoteSelectionEnabled": "pi-quote-selection-enabled",
	"closeToQuit":           "pi-desktop-close-quits",
	"notifyOnComplete":      "pi-desktop-notify-on-complete",
	"diffViewMode":          "pi-diff-view-mode",
	"enterSendMode":         "pi-enter-send-mode",
	"thinkingExpanded":      "pi-thinking-expanded",
	"outputLanguage":        "pi-output-language",
	"toolPreset":            "pi-tool-preset",
	"chatContentFontSize":   "pi-chat-content-font-size",
	"sidebarScale":          "pi-sidebar-scale",
	"sidebarWidth":          "pi-sidebar-width",
	"rightPanelWidth":       "pi-right-panel-width",
	"fileExplorerOpen":      "pi-web:file-explorer:open",
	"settingsNavigation":    "pi-web:settings-navigation",
	"tabOpenSession":        "pi-web:tab-open-session",
}

func defaultLegacyMigrationSources() (legacyMigrationSources, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return legacyMigrationSources{}, fmt.Errorf("resolve home directory: %w", err)
	}
	sources := legacyMigrationSources{
		AppDir:       filepath.Join(home, "Library", "Application Support", "com.abcwyc.pi-agent"),
		LocalStorage: filepath.Join(home, "Library", "WebKit", "com.abcwyc.pi-agent", "WebsiteData", "Default", "*", "*", "LocalStorage", "localstorage.sqlite3"),
		PiAgentDir:   filepath.Join(home, ".pi", "agent"),
	}
	if configured := strings.TrimSpace(os.Getenv("BYOP_LEGACY_APP_DIR")); configured != "" {
		sources.AppDir = configured
	}
	if configured := strings.TrimSpace(os.Getenv("BYOP_LEGACY_LOCALSTORAGE")); configured != "" {
		sources.LocalStorage = configured
	}
	if configured := strings.TrimSpace(os.Getenv("BYOP_LEGACY_PI_AGENT_DIR")); configured != "" {
		sources.PiAgentDir = configured
	}
	for _, value := range []*string{&sources.AppDir, &sources.LocalStorage, &sources.PiAgentDir} {
		expanded, expandErr := expandPiHomePath(*value)
		if expandErr != nil {
			return legacyMigrationSources{}, expandErr
		}
		*value = expanded
	}
	return sources, nil
}

func resolveLegacyLocalStorage(pattern string) (string, int, time.Time, error) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", 0, time.Time{}, err
	}
	if len(matches) == 0 {
		if info, statErr := os.Stat(pattern); statErr == nil && info.Mode().IsRegular() {
			return pattern, 1, info.ModTime(), nil
		}
		return "", 0, time.Time{}, nil
	}
	type candidate struct {
		path    string
		modTime time.Time
	}
	found := make([]candidate, 0, len(matches))
	for _, path := range matches {
		info, statErr := os.Stat(path)
		if statErr == nil && info.Mode().IsRegular() {
			found = append(found, candidate{path: path, modTime: info.ModTime()})
		}
	}
	if len(found) == 0 {
		return "", 0, time.Time{}, nil
	}
	sort.SliceStable(found, func(i, j int) bool {
		if !found[i].modTime.Equal(found[j].modTime) {
			return found[i].modTime.After(found[j].modTime)
		}
		return found[i].path > found[j].path
	})
	return found[0].path, len(found), found[0].modTime, nil
}

func scanLegacyDesktop(
	ctx context.Context,
	sources legacyMigrationSources,
	prefs byopPreferences,
) (legacyScan, error) {
	scan := legacyScan{
		ImportedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		SourcePaths:    sources,
		LocalStorageN:  sources.LocalStorageN,
		LocalStorageAt: sources.LocalStorageAt,
	}
	prefs = cloneByopPreferences(prefs)
	localValues := make(map[string]json.RawMessage)
	var localUndecodable []string
	uiValues := make(map[string]json.RawMessage)

	if err := ctx.Err(); err != nil {
		return scan, err
	}
	appAvailable, appSummary := scanLegacyAppDir(ctx, sources.AppDir, &scan, uiValues)
	scan.Sources = append(scan.Sources, legacySourcePreview{
		Source:  "app-dir",
		Status:  legacySourceAvailability(appAvailable),
		Summary: appSummary,
	})

	if err := ctx.Err(); err != nil {
		return scan, err
	}
	localPath, localCount, localMod, err := resolveLegacyLocalStorage(sources.LocalStorage)
	if err != nil {
		return scan, fmt.Errorf("find legacy localStorage: %w", err)
	}
	scan.LocalStorageN = localCount
	scan.LocalStorageAt = localMod
	localSummary := "No legacy localStorage database"
	localStatus := "not found"
	if localPath != "" {
		localValues, localUndecodable, err = readLegacyLocalStorage(ctx, localPath)
		if err != nil {
			if errors.Is(err, exec.ErrNotFound) {
				localStatus = "unavailable"
				localSummary = "sqlite3 is unavailable"
			} else {
				localStatus = "unavailable"
				localSummary = "Legacy localStorage could not be read"
				scan.ScanWarning = "Legacy localStorage could not be read."
			}
		} else {
			localStatus = "found"
			localSummary = fmt.Sprintf("%d preference key(s)", len(localValues))
			if len(localUndecodable) > 0 {
				localSummary += fmt.Sprintf(" · %d undecodable", len(localUndecodable))
			}
			if localCount > 1 {
				localSummary += fmt.Sprintf(" · latest of %d databases", localCount)
			}
		}
	}
	scan.Sources = append(scan.Sources, legacySourcePreview{
		Source:  "localStorage",
		Status:  localStatus,
		Summary: localSummary,
	})

	if err := ctx.Err(); err != nil {
		return scan, err
	}
	taskCount, piAvailable := scanLegacyPiAgentDir(sources.PiAgentDir)
	taskSummary := "No legacy scheduled tasks"
	if taskCount > 0 {
		taskSummary = fmt.Sprintf("%d scheduled task(s) found — not imported in P8 (P7 automations are plugin-owned)", taskCount)
	}
	scan.Sources = append(scan.Sources, legacySourcePreview{
		Source:  "pi-agent",
		Status:  legacySourceAvailability(piAvailable),
		Summary: taskSummary,
	})

	for _, key := range localUndecodable {
		scan.Entries = append(scan.Entries, legacyPreviewEntry{
			ID: key, Source: "localStorage",
			Summary: "Undecodable localStorage value", Target: "Not imported",
			Status: legacyStatusUnknown,
		})
	}
	applyLegacyMappings(&scan, prefs, localValues, uiValues)
	scan.canImport, scan.importableCount = legacyScanImportability(scan.Entries)
	return scan, nil
}

func legacySourceAvailability(found bool) string {
	if found {
		return "found"
	}
	return "not found"
}

func scanLegacyAppDir(
	ctx context.Context,
	appDir string,
	scan *legacyScan,
	uiValues map[string]json.RawMessage,
) (bool, string) {
	info, err := os.Stat(appDir)
	if err != nil || !info.IsDir() {
		return false, "No app directory"
	}
	parts := []string{}
	lastVersionPath := filepath.Join(appDir, "last-version.json")
	if data, readErr := os.ReadFile(lastVersionPath); readErr == nil {
		if version := legacyVersionFromJSON(data); version != "" {
			scan.Version = version
			parts = append(parts, "version "+version)
			scan.Entries = append(scan.Entries, legacyPreviewEntry{
				ID:      "last-version.json",
				Source:  "app-dir",
				Summary: "Legacy version " + version,
				Target:  "Info only",
				Status:  legacyStatusInfo,
			})
		}
	}
	windowPath := filepath.Join(appDir, ".window-state.json")
	if data, readErr := os.ReadFile(windowPath); readErr == nil {
		summary := legacyWindowSummary(data)
		if summary != "" {
			scan.WindowSummary = summary
			parts = append(parts, summary)
			scan.Entries = append(scan.Entries, legacyPreviewEntry{
				ID:      ".window-state.json",
				Source:  "app-dir",
				Summary: summary,
				Target:  "Info only",
				Status:  legacyStatusInfo,
			})
		}
	}
	uiPrefsPath := filepath.Join(appDir, "ui-prefs.json")
	if data, readErr := os.ReadFile(uiPrefsPath); readErr == nil {
		parsed := decodeLegacyObject(data)
		if parsed != nil {
			for key, value := range parsed {
				if key == "preferences" {
					if nested := decodeLegacyObject(value); nested != nil {
						for nestedKey, nestedValue := range nested {
							uiValues[nestedKey] = cloneRawMessage(nestedValue)
						}
					}
					continue
				}
				uiValues[key] = cloneRawMessage(value)
			}
			parts = append(parts, "ui-prefs.json")
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "App directory found")
	}
	if err := ctx.Err(); err != nil {
		return true, strings.Join(parts, " · ")
	}
	return true, strings.Join(parts, " · ")
}

func legacyVersionFromJSON(data []byte) string {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err == nil {
		for _, key := range []string{"version", "appVersion"} {
			if raw := object[key]; len(raw) > 0 {
				if text, ok := legacyJSONString(raw); ok {
					return text
				}
			}
		}
	}
	if text, ok := legacyJSONString(bytes.TrimSpace(data)); ok {
		return text
	}
	return ""
}

func legacyWindowSummary(data []byte) string {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return "Window state present"
	}
	width, widthOK := legacyJSONNumber(object["width"])
	height, heightOK := legacyJSONNumber(object["height"])
	if widthOK && heightOK {
		return fmt.Sprintf("Window %s × %s", formatLegacyNumber(width), formatLegacyNumber(height))
	}
	return "Window state present"
}

func formatLegacyNumber(value float64) string {
	if value == float64(int64(value)) {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func decodeLegacyObject(data []byte) map[string]json.RawMessage {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return nil
	}
	return object
}

func legacyRawEqual(a, b []byte) bool {
	var decodedA, decodedB any
	decoderA := json.NewDecoder(bytes.NewReader(a))
	decoderA.UseNumber()
	decoderB := json.NewDecoder(bytes.NewReader(b))
	decoderB.UseNumber()
	if decoderA.Decode(&decodedA) == nil && decoderB.Decode(&decodedB) == nil &&
		reflect.DeepEqual(decodedA, decodedB) {
		return true
	}
	var compactA, compactB bytes.Buffer
	if err := json.Compact(&compactA, a); err != nil {
		return bytes.Equal(a, b)
	}
	if err := json.Compact(&compactB, b); err != nil {
		return bytes.Equal(a, b)
	}
	return bytes.Equal(compactA.Bytes(), compactB.Bytes())
}

func legacyJSONString(raw json.RawMessage) (string, bool) {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return "", false
	}
	return strings.TrimSpace(text), true
}

func legacyJSONNumber(raw json.RawMessage) (float64, bool) {
	var number float64
	if err := json.Unmarshal(raw, &number); err != nil {
		return 0, false
	}
	return number, true
}

func readLegacyLocalStorage(ctx context.Context, path string) (map[string]json.RawMessage, []string, error) {
	tempDir, err := os.MkdirTemp("", "byop-legacy-localstorage-")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(tempDir)
	copyPath := filepath.Join(tempDir, "localstorage.sqlite3")
	if err := copyLegacyFile(path, copyPath); err != nil {
		return nil, nil, err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, statErr := os.Stat(path + suffix); statErr == nil {
			if copyErr := copyLegacyFile(path+suffix, copyPath+suffix); copyErr != nil {
				return nil, nil, copyErr
			}
		}
	}
	sqlitePath := "/usr/bin/sqlite3"
	if _, statErr := os.Stat(sqlitePath); statErr != nil {
		sqlitePath, err = exec.LookPath("sqlite3")
		if err != nil {
			return nil, nil, err
		}
	}
	// The copied database is a private, disposable file. Open it read-write so
	// SQLite can replay a copied WAL and create a missing -shm. The source path
	// is only read by copyLegacyFile and is never passed to sqlite3.
	cmd := exec.CommandContext(ctx, sqlitePath, "-json", copyPath,
		"SELECT key, hex(value) AS value FROM ItemTable;")
	output, err := cmd.Output()
	if err != nil {
		return nil, nil, fmt.Errorf("read legacy localStorage with sqlite3: %w", err)
	}
	var rows []legacyLocalStorageItem
	if len(bytes.TrimSpace(output)) > 0 {
		if err := json.Unmarshal(output, &rows); err != nil {
			return nil, nil, fmt.Errorf("decode legacy localStorage rows: %w", err)
		}
	}
	values := make(map[string]json.RawMessage, len(rows))
	undecodable := make([]string, 0)
	for _, row := range rows {
		decoded, err := decodeLegacyUTF16LEHex(row.Value)
		if err != nil {
			undecodable = append(undecodable, row.Key)
			continue
		}
		values[row.Key] = decoded
	}
	sort.Strings(undecodable)
	return values, undecodable, nil
}

func copyLegacyFile(source, target string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o600)
}

func decodeLegacyUTF16LEHex(value string) (json.RawMessage, error) {
	data, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, err
	}
	if len(data) >= 2 && data[0] == 0xff && data[1] == 0xfe {
		data = data[2:]
	}
	if len(data)%2 != 0 {
		return nil, errors.New("odd UTF-16LE byte length")
	}
	codeUnits := make([]uint16, len(data)/2)
	for i := range codeUnits {
		codeUnits[i] = uint16(data[i*2]) | uint16(data[i*2+1])<<8
	}
	text := string(utf16.Decode(codeUnits))
	text = strings.TrimSpace(text)
	if !json.Valid([]byte(text)) {
		encoded, err := json.Marshal(text)
		if err != nil {
			return nil, err
		}
		return json.RawMessage(encoded), nil
	}
	return json.RawMessage(text), nil
}

func scanLegacyPiAgentDir(path string) (int, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return 0, false
	}
	count := 0
	if data, readErr := os.ReadFile(filepath.Join(path, "scheduled-tasks.json")); readErr == nil {
		count += legacyScheduledTaskFileCount(data)
	}
	entries, readErr := os.ReadDir(filepath.Join(path, "scheduled-tasks"))
	if readErr == nil {
		for _, entry := range entries {
			if entry.Name() == "scheduler.lock" || entry.IsDir() {
				continue
			}
			count++
		}
	}
	return count, true
}

func legacyScheduledTaskFileCount(data []byte) int {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		if len(bytes.TrimSpace(data)) > 0 {
			return 1
		}
		return 0
	}
	switch typed := value.(type) {
	case []any:
		return len(typed)
	case map[string]any:
		if tasks, ok := typed["tasks"].([]any); ok {
			return len(tasks)
		}
		if len(typed) == 0 {
			return 0
		}
		return len(typed)
	default:
		return 0
	}
}

func applyLegacyMappings(
	scan *legacyScan,
	prefs byopPreferences,
	localValues map[string]json.RawMessage,
	uiValues map[string]json.RawMessage,
) {
	if scan == nil {
		return
	}
	localTheme, localThemeSet := localValues["pi-theme"]
	uiKeys := make([]string, 0, len(uiValues))
	for key := range uiValues {
		uiKeys = append(uiKeys, key)
	}
	sort.Strings(uiKeys)
	uiThemeKey := ""
	for _, key := range uiKeys {
		if legacyUIPrefsKeyMap[key] == "pi-theme" {
			uiThemeKey = key
			break
		}
	}
	if localThemeSet {
		addLegacyTheme(scan, prefs, localTheme, "localStorage")
	} else if uiThemeKey != "" {
		addLegacyTheme(scan, prefs, uiValues[uiThemeKey], "app-dir")
	}
	for _, key := range uiKeys {
		raw := uiValues[key]
		target := legacyUIPrefsKeyMap[key]
		if target == "" {
			if key == "serverPort" {
				scan.Entries = append(scan.Entries, legacyPreviewEntry{
					ID: "ui-prefs.json:" + key, Source: "app-dir",
					Summary: "Server port is managed by BYOP/Pi", Target: "Ignored",
					Status: legacyStatusIgnored,
				})
			} else {
				scan.Entries = append(scan.Entries, legacyPreviewEntry{
					ID: "ui-prefs.json:" + key, Source: "app-dir",
					Summary: "Unknown preference key", Target: "Not imported",
					Status: legacyStatusUnknown,
				})
			}
			continue
		}
		if target == "pi-theme" {
			switch {
			case localThemeSet:
				scan.Entries = append(scan.Entries, legacyPreviewEntry{
					ID: "ui-prefs.json:" + key, Source: "app-dir",
					Summary: "Theme also exists in localStorage; localStorage wins",
					Target:  "Info only", Status: legacyStatusInfo,
				})
			case key == uiThemeKey:
				// The app-directory theme is already represented by addLegacyTheme.
			default:
				scan.Entries = append(scan.Entries, legacyPreviewEntry{
					ID: "ui-prefs.json:" + key, Source: "app-dir",
					Summary: "Duplicate app-directory theme; first value wins",
					Target:  "Info only", Status: legacyStatusInfo,
				})
			}
			continue
		}
		if len(localValues[target]) > 0 {
			scan.Entries = append(scan.Entries, legacyPreviewEntry{
				ID: "ui-prefs.json:" + key, Source: "app-dir",
				Summary: "Duplicate value in localStorage; localStorage wins",
				Target:  "Info only", Status: legacyStatusInfo,
			})
			continue
		}
		addLegacyLocalValue(scan, prefs, target, raw, "app-dir", "ui-prefs.json:"+key)
	}

	keys := make([]string, 0, len(localValues))
	for key := range localValues {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		raw := localValues[key]
		switch key {
		case "pi-theme", "pi-workspace-v1", "pi-web:last-open-by-workspace", legacyDraftsKey, legacyDraftsSpecAlias:
			if key == "pi-theme" {
				continue
			}
			if key == "pi-workspace-v1" {
				lastSessions := decodeLegacyObject(localValues["pi-web:last-open-by-workspace"])
				addLegacyWorkspaceWithSession(scan, prefs, raw, lastSessions)
			}
			if key == "pi-web:last-open-by-workspace" {
				addLegacyLastSessions(scan, prefs, raw)
			}
			if key == legacyDraftsKey || key == legacyDraftsSpecAlias {
				addLegacyDrafts(scan, prefs, raw, key)
			}
		default:
			addLegacyLocalValue(scan, prefs, key, raw, "localStorage", key)
		}
	}
}

func addLegacyTheme(scan *legacyScan, prefs byopPreferences, raw json.RawMessage, source string) {
	value, ok := legacyJSONString(raw)
	mode := themeSystem
	switch strings.ToLower(value) {
	case "light":
		mode = themeLight
	case "dark":
		mode = themeDark
	case "system", "auto":
		mode = themeSystem
	default:
		ok = false
	}
	if !ok {
		status := legacyStatusKept
		var action *legacyImportAction
		if existing, found := prefs.Legacy["pi-theme"]; found && legacyRawEqual(existing.Value, raw) {
			status = legacyStatusSame
		} else if legacyConflictValueMatches(prefs, "pi-theme", raw) {
			status = legacyStatusSame
		} else if found {
			status = legacyStatusConflict
			action = legacyConflictImportAction("pi-theme", raw, source)
		} else {
			action = &legacyImportAction{
				Kind: legacyActionLegacyValue, Key: "pi-theme",
				Raw: cloneRawMessage(raw), Source: source,
			}
		}
		scan.Entries = append(scan.Entries, legacyPreviewEntry{
			ID: "pi-theme", Source: source,
			Summary: "Unsupported theme value kept as legacy",
			Target:  "Legacy", Status: status,
			action: action,
		})
		if action != nil {
			scan.Actions = append(scan.Actions, *action)
		}
		return
	}
	scan.Theme = mode
	status := legacyStatusNew
	var action *legacyImportAction
	current := themeMode(strings.ToLower(prefs.Theme))
	switch {
	case current == mode:
		status = legacyStatusSame
	case current == "" || current == themeSystem:
		status = legacyStatusNew
		action = &legacyImportAction{
			Kind: legacyActionTheme, Key: "pi-theme", LegacyKey: "pi-theme",
			Text: string(mode), Raw: cloneRawMessage(raw), Source: source,
		}
	default:
		if existing, found := prefs.Legacy["pi-theme"]; found && legacyRawEqual(existing.Value, raw) {
			status = legacyStatusSame
		} else if legacyConflictValueMatches(prefs, "pi-theme", raw) {
			status = legacyStatusSame
		} else {
			status = legacyStatusConflict
			if _, found := prefs.Legacy["pi-theme"]; found {
				action = legacyConflictImportAction("pi-theme", raw, source)
			} else {
				action = &legacyImportAction{
					Kind: legacyActionLegacyValue, Key: "pi-theme", Raw: cloneRawMessage(raw), Source: source,
				}
			}
		}
	}
	scan.Entries = append(scan.Entries, legacyPreviewEntry{
		ID: "pi-theme", Source: source, Summary: "Theme: " + string(mode),
		Target: "Theme", Status: status, action: action,
	})
	if action != nil {
		scan.Actions = append(scan.Actions, *action)
	}
}

func addLegacyWorkspace(scan *legacyScan, prefs byopPreferences, raw json.RawMessage) {
	addLegacyWorkspaceWithSession(scan, prefs, raw, nil)
}

func addLegacyWorkspaceWithSession(
	scan *legacyScan,
	prefs byopPreferences,
	raw json.RawMessage,
	lastSessions map[string]json.RawMessage,
) {
	object := decodeLegacyObject(raw)
	if object == nil {
		scan.Entries = append(scan.Entries, legacyPreviewEntry{
			ID: "pi-workspace-v1", Source: "localStorage",
			Summary: "Workspace value is unrecognized", Target: "Legacy",
			Status: legacyStatusUnknown,
		})
		return
	}
	added := false
	canonicalWorkspace := ""
	if cwdRaw := object["cwd"]; len(cwdRaw) > 0 {
		cwd, ok := legacyJSONString(cwdRaw)
		if !ok || cwd == "" {
			scan.Entries = append(scan.Entries, legacyPreviewEntry{
				ID: "pi-workspace-v1", Source: "localStorage",
				Summary: "Workspace path is unrecognized", Target: "Legacy",
				Status: legacyStatusUnknown,
			})
			added = true
		} else {
			canonical := canonicalPiPath(cwd)
			if info, err := os.Stat(canonical); err == nil && info.IsDir() {
				canonicalWorkspace = canonical
				status := legacyStatusNew
				action := &legacyImportAction{
					Kind: legacyActionWorkspace, Key: "pi-workspace-v1",
					LegacyKey: "pi-workspace-v1", Text: canonical,
					Raw: cloneRawMessage(cwdRaw), Source: "localStorage",
				}
				switch {
				case prefs.Workspace == canonical:
					status = legacyStatusSame
					action = nil
				case prefs.Workspace != "":
					if existing, found := prefs.Legacy["pi-workspace-v1"]; found &&
						legacyRawEqual(existing.Value, cwdRaw) {
						status = legacyStatusSame
						action = nil
					} else if legacyConflictValueMatches(prefs, "pi-workspace-v1", cwdRaw) {
						status = legacyStatusSame
						action = nil
					} else {
						status = legacyStatusConflict
						if _, found := prefs.Legacy["pi-workspace-v1"]; found {
							action = legacyConflictImportAction(
								"pi-workspace-v1", cwdRaw, "localStorage")
						} else {
							action = &legacyImportAction{
								Kind: legacyActionLegacyValue, Key: "pi-workspace-v1",
								Raw: cloneRawMessage(cwdRaw), Source: "localStorage",
							}
						}
					}
				}
				scan.Workspace = canonical
				scan.WorkspaceSet = true
				scan.Entries = append(scan.Entries, legacyPreviewEntry{
					ID: "pi-workspace-v1", Source: "localStorage",
					Summary: "Workspace: " + filepath.Base(canonical),
					Target:  "Workspace", Status: status, action: action,
				})
				if action != nil {
					scan.Actions = append(scan.Actions, *action)
				}
				added = true
			} else {
				scan.Entries = append(scan.Entries, legacyPreviewEntry{
					ID: "pi-workspace-v1", Source: "localStorage",
					Summary: "Workspace folder is missing", Target: "Workspace",
					Status: legacyStatusMissing,
				})
				added = true
			}
		}
	}
	for _, key := range []string{"rightPanelOpen", "fileTabs", "activeFileTabId"} {
		rawValue, exists := object[key]
		if !exists {
			continue
		}
		legacyKey := "pi-workspace-v1." + key
		status := legacyStatusKept
		var action *legacyImportAction
		if existing, found := prefs.Legacy[legacyKey]; found && legacyRawEqual(existing.Value, rawValue) {
			status = legacyStatusSame
		} else if legacyConflictValueMatches(prefs, legacyKey, rawValue) {
			status = legacyStatusSame
		} else if found {
			status = legacyStatusConflict
			action = legacyConflictImportAction(legacyKey, rawValue, "localStorage")
		} else {
			action = &legacyImportAction{
				Kind: legacyActionLegacyValue, Key: legacyKey,
				Raw: cloneRawMessage(rawValue), Source: "localStorage",
			}
		}
		scan.Entries = append(scan.Entries, legacyPreviewEntry{
			ID: "pi-workspace-v1/" + key, Source: "localStorage",
			Summary: legacyWorkspaceFieldSummary(key), Target: "Legacy",
			Status: status, action: action,
		})
		if action != nil {
			scan.Actions = append(scan.Actions, *action)
		}
		added = true
	}
	if rawSessionID, exists := object["sessionId"]; exists {
		lastSession, linked := canonicalLegacySessionValue(lastSessions, canonicalWorkspace)
		if linked && legacyRawEqual(lastSession, rawSessionID) {
			scan.Entries = append(scan.Entries, legacyPreviewEntry{
				ID: "pi-workspace-v1/sessionId", Source: "localStorage",
				Summary: "Workspace session also exists in last-open map",
				Target:  "Info only", Status: legacyStatusInfo,
			})
		} else {
			addLegacyWorkspaceSession(scan, prefs, canonicalWorkspace, rawSessionID)
		}
		added = true
	}
	if !added {
		scan.Entries = append(scan.Entries, legacyPreviewEntry{
			ID: "pi-workspace-v1", Source: "localStorage",
			Summary: "No workspace fields", Target: "Info only",
			Status: legacyStatusInfo,
		})
	}
}

func canonicalLegacySessionValue(
	values map[string]json.RawMessage,
	workspace string,
) (json.RawMessage, bool) {
	if workspace == "" {
		return nil, false
	}
	workspace = canonicalPiPath(workspace)
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if canonicalPiPath(key) == workspace {
			return values[key], true
		}
	}
	return nil, false
}

func addLegacyWorkspaceSession(
	scan *legacyScan,
	prefs byopPreferences,
	canonicalWorkspace string,
	raw json.RawMessage,
) {
	sessionID, ok := legacyJSONString(raw)
	if !ok || sessionID == "" {
		scan.Entries = append(scan.Entries, legacyPreviewEntry{
			ID: "pi-workspace-v1/sessionId", Source: "localStorage",
			Summary: "Workspace session reference is unrecognized", Target: "Last session",
			Status: legacyStatusUnknown,
		})
		return
	}
	summary := "Workspace session " + legacySessionIDSummary(sessionID)
	if canonicalWorkspace == "" {
		scan.Entries = append(scan.Entries, legacyPreviewEntry{
			ID: "pi-workspace-v1/sessionId", Source: "localStorage",
			Summary: summary + " · workspace missing", Target: "Last session",
			Status: legacyStatusMissing,
		})
		return
	}
	if hasLegacySessionAction(scan, canonicalWorkspace) {
		return
	}
	if _, err := findLegacyPiSession(canonicalWorkspace, sessionID); err != nil {
		scan.Entries = append(scan.Entries, legacyPreviewEntry{
			ID: "pi-workspace-v1/sessionId", Source: "localStorage",
			Summary: summary + " · session missing", Target: "Last session",
			Status: legacyStatusMissing,
		})
		return
	}
	status := legacyStatusLinked
	var action *legacyImportAction
	legacyKey := "pi-workspace-v1:sessionId"
	if current, exists := prefs.LastSessionByWorkspace[canonicalWorkspace]; exists {
		if current == sessionID {
			status = legacyStatusSame
		} else {
			if existing, found := prefs.Legacy[legacyKey]; found && legacyRawEqual(existing.Value, raw) {
				status = legacyStatusSame
			} else if legacyConflictValueMatches(prefs, legacyKey, raw) {
				status = legacyStatusSame
			} else {
				status = legacyStatusConflict
				if _, found := prefs.Legacy[legacyKey]; found {
					action = legacyConflictImportAction(legacyKey, raw, "localStorage")
				} else {
					action = &legacyImportAction{
						Kind: legacyActionLegacyValue, Key: legacyKey,
						Raw: cloneRawMessage(raw), Source: "localStorage",
					}
				}
			}
		}
	} else {
		action = &legacyImportAction{
			Kind: legacyActionSession, Key: canonicalWorkspace, LegacyKey: legacyKey,
			Text: sessionID, Raw: cloneRawMessage(raw), Source: "localStorage",
		}
	}
	scan.Entries = append(scan.Entries, legacyPreviewEntry{
		ID: "pi-workspace-v1/sessionId", Source: "localStorage",
		Summary: summary, Target: "Last session", Status: status, action: action,
	})
	if action != nil {
		scan.Actions = append(scan.Actions, *action)
	}
}

func hasLegacySessionAction(scan *legacyScan, canonicalWorkspace string) bool {
	for _, action := range scan.Actions {
		if action.Kind == legacyActionSession && action.Key == canonicalWorkspace {
			return true
		}
		if action.Kind == legacyActionLegacyValue &&
			action.Key == "pi-web:last-open-by-workspace:"+canonicalWorkspace {
			return true
		}
	}
	return false
}

func legacyWorkspaceFieldSummary(key string) string {
	switch key {
	case "rightPanelOpen":
		return "Right panel preference kept as legacy"
	case "fileTabs":
		return "Open file tabs kept as legacy"
	case "activeFileTabId":
		return "Active file tab kept as legacy"
	case "sessionId":
		return "Workspace session reference kept as legacy"
	default:
		return "Workspace field kept as legacy"
	}
}

func addLegacyLastSessions(scan *legacyScan, prefs byopPreferences, raw json.RawMessage) {
	object := decodeLegacyObject(raw)
	if object == nil {
		scan.Entries = append(scan.Entries, legacyPreviewEntry{
			ID: "pi-web:last-open-by-workspace", Source: "localStorage",
			Summary: "Last-session map is unrecognized", Target: "Legacy",
			Status: legacyStatusUnknown,
		})
		return
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		scan.Entries = append(scan.Entries, legacyPreviewEntry{
			ID: "pi-web:last-open-by-workspace", Source: "localStorage",
			Summary: "No saved sessions", Target: "Session links",
			Status: legacyStatusInfo,
		})
		return
	}
	for _, cwd := range keys {
		sessionID, ok := legacyJSONString(object[cwd])
		if !ok || sessionID == "" {
			continue
		}
		canonical := canonicalPiPath(cwd)
		summary := filepath.Base(canonical) + " · " + legacySessionIDSummary(sessionID)
		if _, err := findLegacyPiSession(canonical, sessionID); err != nil {
			scan.Entries = append(scan.Entries, legacyPreviewEntry{
				ID: "pi-web:last-open-by-workspace/" + filepath.Base(canonical), Source: "localStorage",
				Summary: summary + " · session missing", Target: "Last session",
				Status: legacyStatusMissing,
			})
			continue
		}
		status := legacyStatusLinked
		var action *legacyImportAction
		legacyKey := "pi-web:last-open-by-workspace:" + canonical
		if current, exists := prefs.LastSessionByWorkspace[canonical]; exists {
			if current == sessionID {
				status = legacyStatusSame
			} else {
				if existing, found := prefs.Legacy[legacyKey]; found &&
					legacyRawEqual(existing.Value, object[cwd]) {
					status = legacyStatusSame
				} else if legacyConflictValueMatches(prefs, legacyKey, object[cwd]) {
					status = legacyStatusSame
				} else {
					status = legacyStatusConflict
					if _, found := prefs.Legacy[legacyKey]; found {
						action = legacyConflictImportAction(
							legacyKey, object[cwd], "localStorage")
					} else {
						action = &legacyImportAction{
							Kind: legacyActionLegacyValue,
							Key:  legacyKey,
							Raw:  cloneRawMessage(object[cwd]), Source: "localStorage",
						}
					}
				}
			}
		} else {
			action = &legacyImportAction{
				Kind: legacyActionSession, Key: canonical, LegacyKey: legacyKey,
				Text: sessionID, Raw: cloneRawMessage(object[cwd]), Source: "localStorage",
			}
		}
		scan.Entries = append(scan.Entries, legacyPreviewEntry{
			ID: "pi-web:last-open-by-workspace/" + filepath.Base(canonical), Source: "localStorage",
			Summary: summary, Target: "Last session", Status: status, action: action,
		})
		if action != nil {
			scan.Actions = append(scan.Actions, *action)
		}
	}
}

func legacySessionIDSummary(id string) string {
	runes := []rune(id)
	if len(runes) <= 4 {
		return string(runes) + "…"
	}
	return string(runes[:4]) + "…"
}

func findLegacyPiSession(cwd, sessionID string) (string, error) {
	root, flat, err := piSessionRoot()
	if err != nil {
		return "", err
	}
	directory := piSessionDirectory(root, cwd)
	if flat {
		directory = root
	}
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return "", os.ErrNotExist
	}
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".jsonl") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		header, readErr := readPiSessionHeader(path)
		if readErr != nil || header.ID != sessionID {
			continue
		}
		if flat && canonicalPiPath(header.Cwd) != canonicalPiPath(cwd) {
			continue
		}
		return path, nil
	}
	return "", os.ErrNotExist
}

func addLegacyDrafts(
	scan *legacyScan,
	prefs byopPreferences,
	raw json.RawMessage,
	sourceKey string,
) {
	object := decodeLegacyObject(raw)
	if object == nil {
		scan.Entries = append(scan.Entries, legacyPreviewEntry{
			ID: sourceKey, Source: "localStorage",
			Summary: "Draft map is unrecognized", Target: "Legacy",
			Status: legacyStatusUnknown,
		})
		return
	}
	if len(object) == 0 {
		scan.Entries = append(scan.Entries, legacyPreviewEntry{
			ID: sourceKey, Source: "localStorage",
			Summary: "No drafts", Target: "Drafts", Status: legacyStatusInfo,
		})
		return
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		text, ok := legacyJSONString(object[key])
		if !ok {
			scan.Entries = append(scan.Entries, legacyPreviewEntry{
				ID: sourceKey + "/" + key, Source: "localStorage",
				Summary: "Unrecognized draft value", Target: "Legacy",
				Status: legacyStatusUnknown,
			})
			continue
		}
		status := legacyStatusNew
		var action *legacyImportAction
		if current, exists := prefs.Drafts[key]; exists {
			if current.Text == text {
				status = legacyStatusSame
			} else {
				legacyKey := sourceKey + ":" + key
				if existing, found := prefs.Legacy[legacyKey]; found &&
					legacyRawEqual(existing.Value, object[key]) {
					status = legacyStatusSame
				} else if legacyConflictValueMatches(prefs, legacyKey, object[key]) {
					status = legacyStatusSame
				} else {
					status = legacyStatusConflict
					if _, found := prefs.Legacy[legacyKey]; found {
						action = legacyConflictImportAction(
							legacyKey, object[key], "localStorage")
					} else {
						action = &legacyImportAction{
							Kind: legacyActionLegacyValue, Key: legacyKey,
							Raw: cloneRawMessage(object[key]), Source: "localStorage",
						}
					}
				}
			}
		} else {
			action = &legacyImportAction{
				Kind: legacyActionDraft, Key: key, LegacyKey: sourceKey + ":" + key,
				Text: text, Raw: cloneRawMessage(object[key]), Source: "localStorage",
			}
		}
		scan.Entries = append(scan.Entries, legacyPreviewEntry{
			ID: sourceKey + "/" + key, Source: "localStorage",
			Summary: fmt.Sprintf("Draft · %d character(s)", len([]rune(text))),
			Target:  "Drafts", Status: status, action: action,
		})
		if action != nil {
			scan.Actions = append(scan.Actions, *action)
		}
	}
}

func addLegacyLocalValue(
	scan *legacyScan,
	prefs byopPreferences,
	key string,
	raw json.RawMessage,
	source string,
	id string,
) {
	if _, known := legacyKnownKeptKeys[key]; !known {
		scan.Entries = append(scan.Entries, legacyPreviewEntry{
			ID: id, Source: source, Summary: "Unknown preference key",
			Target: "Not imported", Status: legacyStatusUnknown,
		})
		return
	}
	status := legacyStatusKept
	var action *legacyImportAction
	if existing, found := prefs.Legacy[key]; found {
		if legacyRawEqual(existing.Value, raw) {
			status = legacyStatusSame
		} else if legacyConflictValueMatches(prefs, key, raw) {
			status = legacyStatusSame
		} else {
			status = legacyStatusConflict
			action = legacyConflictImportAction(key, raw, source)
		}
	} else {
		action = &legacyImportAction{
			Kind: legacyActionLegacyValue, Key: key, Raw: cloneRawMessage(raw), Source: source,
		}
	}
	scan.Entries = append(scan.Entries, legacyPreviewEntry{
		ID: id, Source: source, Summary: legacyKeptSummary(key),
		Target: "Legacy", Status: status, action: action,
	})
	if action != nil {
		scan.Actions = append(scan.Actions, *action)
	}
}

func legacyConflictValueMatches(
	prefs byopPreferences,
	key string,
	raw json.RawMessage,
) bool {
	existing, found := prefs.Legacy[key+":conflict"]
	return found && legacyRawEqual(existing.Value, raw)
}

func legacyConflictImportAction(
	key string,
	raw json.RawMessage,
	source string,
) *legacyImportAction {
	return &legacyImportAction{
		Kind: legacyActionLegacyValue, Key: key, LegacyKey: key + ":conflict",
		Raw: cloneRawMessage(raw), Source: source, Overwrite: true,
	}
}

func legacyKeptSummary(key string) string {
	switch key {
	case "pi-chat-content-font-size", "pi-sidebar-scale":
		return "Kept as legacy value (BYOP body stays 14px)"
	case "pi-sidebar-width", "pi-right-panel-width":
		return "Width kept as legacy value"
	default:
		return "Preference kept as legacy value"
	}
}

func legacyScanImportability(entries []legacyPreviewEntry) (bool, int) {
	count := 0
	for _, entry := range entries {
		if entry.action != nil {
			count++
		}
	}
	return count > 0, count
}

func applyLegacyImport(scan legacyScan, prefs byopPreferences) (byopPreferences, legacyImportResult) {
	next := cloneByopPreferences(prefs)
	normalizeByopPreferences(&next)
	result := legacyImportResult{
		Record: legacyImportRecord{
			At:            scan.ImportedAt,
			LegacyVersion: scan.Version,
			Counts:        legacyScanCounts(scan.Entries),
		},
		Theme: themeMode(next.Theme),
	}
	for _, action := range scan.Actions {
		switch action.Kind {
		case legacyActionTheme:
			if next.Theme == action.Text {
				continue
			}
			if next.Theme == "" || themeMode(next.Theme) == themeSystem {
				next.Theme = action.Text
				result.ThemeChanged = true
			} else {
				preserveLegacyImportConflict(&next, action, scan.ImportedAt)
			}
		case legacyActionWorkspace:
			if next.Workspace == action.Text {
				continue
			}
			if next.Workspace == "" {
				result.Workspace = action.Text
				result.WorkspaceChange = true
				next.Workspace = action.Text
			} else {
				preserveLegacyImportConflict(&next, action, scan.ImportedAt)
			}
		case legacyActionSession:
			if current, exists := next.LastSessionByWorkspace[action.Key]; !exists {
				next.LastSessionByWorkspace[action.Key] = action.Text
			} else if current != action.Text {
				preserveLegacyImportConflict(&next, action, scan.ImportedAt)
			}
		case legacyActionDraft:
			current, exists := next.Drafts[action.Key]
			if !exists {
				next.Drafts[action.Key] = legacyDraft{
					Text: action.Text, Source: action.Source, ImportedAt: scan.ImportedAt,
				}
			} else if current.Text != action.Text {
				preserveLegacyImportConflict(&next, action, scan.ImportedAt)
			}
		case legacyActionLegacyValue:
			preserveLegacyImportConflict(&next, action, scan.ImportedAt)
		}
	}
	result.Theme = themeMode(next.Theme)
	return next, result
}

func preserveLegacyImportConflict(next *byopPreferences, action legacyImportAction, importedAt string) {
	key := strings.TrimSpace(action.LegacyKey)
	if key == "" {
		key = strings.TrimSpace(action.Key)
	}
	if key == "" || len(action.Raw) == 0 {
		return
	}
	current, exists := next.Legacy[key]
	if exists && legacyRawEqual(current.Value, action.Raw) {
		return
	}
	if exists && !action.Overwrite {
		return
	}
	if next.Legacy == nil {
		next.Legacy = make(map[string]legacyValue)
	}
	next.Legacy[key] = legacyValue{
		Value: cloneRawMessage(action.Raw), Source: action.Source, ImportedAt: importedAt,
	}
}

func legacyScanCounts(entries []legacyPreviewEntry) legacyImportCounts {
	var counts legacyImportCounts
	for _, entry := range entries {
		switch entry.Status {
		case legacyStatusNew:
			counts.New++
		case legacyStatusSame:
			counts.Same++
		case legacyStatusConflict:
			counts.Conflict++
		case legacyStatusKept:
			counts.Kept++
		case legacyStatusLinked:
			counts.Linked++
		case legacyStatusMissing:
			counts.Missing++
		case legacyStatusUnknown:
			counts.Unknown++
		case legacyStatusIgnored:
			counts.Ignored++
		case legacyStatusInfo:
			counts.Info++
		}
	}
	return counts
}

func persistLegacyImport(
	configDir string,
	store *byopPreferencesStore,
	preferences byopPreferences,
	result legacyImportResult,
) (legacyImportPersistence, error) {
	persistence := legacyImportPersistence{}
	if store == nil {
		return persistence, errors.New("preferences store is unavailable")
	}
	migrationDir := filepath.Join(configDir, "migration")
	if err := ensureAutomationDir(migrationDir); err != nil {
		return persistence, fmt.Errorf("create migration directory: %w", err)
	}
	preferencesPath := filepath.Join(configDir, "preferences.json")
	if data, err := os.ReadFile(preferencesPath); err == nil {
		backupName := "backup-" + time.Now().UTC().Format("20060102T150405.000000000Z") + ".json"
		if err := writeAutomationFileAtomic(filepath.Join(migrationDir, backupName), data); err != nil {
			return persistence, fmt.Errorf("write migration backup: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return persistence, fmt.Errorf("read preferences for backup: %w", err)
	}
	if err := store.save(preferences); err != nil {
		return persistence, err
	}
	persistence.PreferencesSaved = true
	if err := appendLegacyImportRecord(filepath.Join(migrationDir, "imports.jsonl"), result.Record); err != nil {
		return persistence, fmt.Errorf("append migration record: %w", err)
	}
	persistence.HistorySaved = true
	return persistence, nil
}

func appendLegacyImportRecord(path string, record legacyImportRecord) error {
	var data bytes.Buffer
	if err := json.NewEncoder(&data).Encode(record); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := ensureAutomationJSONLLineBoundary(file); err != nil {
		return err
	}
	if _, err := file.Write(data.Bytes()); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func latestLegacyImport(configDir string) (legacyImportRecord, bool) {
	file, err := os.Open(filepath.Join(configDir, "migration", "imports.jsonl"))
	if err != nil {
		return legacyImportRecord{}, false
	}
	defer file.Close()
	var latest legacyImportRecord
	found := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var record legacyImportRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		latest = record
		found = true
	}
	return latest, found
}
