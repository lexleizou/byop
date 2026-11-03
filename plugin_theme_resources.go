package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/egoist/mygo/ui"
)

// pluginThemeColors carries the semantic colors a theme resource overrides.
// Keys mirror the palette fields the native UI and the WebView payload share;
// a file may override any subset, and every other value keeps the active
// palette's token.
type pluginThemeColors struct {
	Background   string `json:"background,omitempty"`
	Surface      string `json:"surface,omitempty"`
	Text         string `json:"text,omitempty"`
	Muted        string `json:"muted,omitempty"`
	Accent       string `json:"accent,omitempty"`
	OnAccent     string `json:"onAccent,omitempty"`
	Border       string `json:"border,omitempty"`
	Focus        string `json:"focus,omitempty"`
	Danger       string `json:"danger,omitempty"`
	Warning      string `json:"warning,omitempty"`
	Success      string `json:"success,omitempty"`
	DangerText   string `json:"dangerText,omitempty"`
	WarningText  string `json:"warningText,omitempty"`
	SuccessText  string `json:"successText,omitempty"`
	ReadingSurf  string `json:"readingSurface,omitempty"`
	RaisedSurf   string `json:"raisedSurface,omitempty"`
	ComposerSurf string `json:"composerSurface,omitempty"`
}

type pluginThemeFile struct {
	Colors pluginThemeColors `json:"colors"`
	Font   string            `json:"font,omitempty"`
	Code   string            `json:"codeFont,omitempty"`
	// Parsed holds the validated colours from Colors, keyed like the palette
	// fields. It is filled by the loader and never mutated afterwards.
	Parsed map[string]ui.Color `json:"-"`
}

// pluginThemeSelection is one chosen resource. It remembers the plugin id and
// contribution id so a disable can hide it without losing the choice, and a
// re-enable restores the same resource.
type pluginThemeSelection struct {
	PluginID   string
	ResourceID string
}

// pluginThemeOverlay is the immutable set of loaded theme resources plus the
// selection that points into it. A scan replaces the loaded set and keeps the
// selection when the resource still exists; disable/remove clears the effective
// resource but keeps the stored choice.
type pluginThemeOverlay struct {
	resources []pluginThemeResource
	selection pluginThemeSelection
}

// pluginThemeModeColors is the immutable per-mode overlay one theme resource
// contributes. Only fields the file declared carry values, so a field absent
// from the mode keeps the palette token; the map is shared across frames and
// never written after load.
type pluginThemeModeColors struct {
	colors map[string]ui.Color
}

type pluginThemeResource struct {
	entry          pluginThemeContribution
	pluginID       string
	fontFamily     string
	codeFontFamily string
	light          pluginThemeModeColors
	dark           pluginThemeModeColors
}

var (
	pluginThemeCurrent      atomic.Pointer[pluginThemeOverlay]
	pluginThemeSelected     atomic.Pointer[pluginThemeSelection]
	pluginThemeGenerationMu sync.Mutex
	pluginThemeGeneration   uint64
)

// buildPluginThemeResources reads the theme files of enabled registry entries in
// registry order. A file that cannot be parsed is reported as an ordinary
// registry issue and skipped, exactly like a locale file.
func buildPluginThemeResources(
	entries []pluginRegistryEntry,
) (*pluginThemeOverlay, []pluginRegistryIssue) {
	overlay := &pluginThemeOverlay{}
	var issues []pluginRegistryIssue
	for _, entry := range entries {
		if entry.Status != pluginRegistered || entry.Manifest == nil {
			continue
		}
		for _, contribution := range entry.Manifest.Contributes.Themes {
			light, lightIssues := readPluginThemeResource(entry.SourceDirectory, contribution.Light)
			issues = append(issues, lightIssues...)
			dark, darkIssues := readPluginThemeResource(entry.SourceDirectory, contribution.Dark)
			issues = append(issues, darkIssues...)
			if len(lightIssues)+len(darkIssues) > 0 {
				continue
			}
			overlay.resources = append(overlay.resources, pluginThemeResource{
				entry:          contribution,
				pluginID:       entry.ID,
				fontFamily:     firstString(contribution.FontFamily, light.Font, dark.Font),
				codeFontFamily: firstString(contribution.CodeFontFamily, light.Code, dark.Code),
				light:          pluginThemeModeColors{colors: light.Parsed},
				dark:           pluginThemeModeColors{colors: dark.Parsed},
			})
		}
	}
	return overlay, issues
}

// readPluginThemeResource loads one theme file and validates every colour
// against the same shapes MyGo accepts, so an invalid value becomes an ordinary
// registry issue instead of a panic on the next frame. It returns the parsed
// file plus one issue per unusable field.
func readPluginThemeResource(
	sourceDirectory, entryPath string,
) (*pluginThemeFile, []pluginRegistryIssue) {
	theme, err := readPluginThemeFile(sourceDirectory, entryPath)
	if err != nil {
		return nil, []pluginRegistryIssue{{Message: fmt.Sprintf(
			"theme entry %s cannot be used: %v", entryPath, err)}}
	}
	var issues []pluginRegistryIssue
	// Drop fields whose colour does not parse, keeping the valid ones, so a
	// resource with one bad value still contributes the rest.
	fields := []struct {
		name  string
		value *string
	}{
		{"background", &theme.Colors.Background},
		{"surface", &theme.Colors.Surface},
		{"text", &theme.Colors.Text},
		{"muted", &theme.Colors.Muted},
		{"accent", &theme.Colors.Accent},
		{"onAccent", &theme.Colors.OnAccent},
		{"border", &theme.Colors.Border},
		{"focus", &theme.Colors.Focus},
		{"danger", &theme.Colors.Danger},
		{"warning", &theme.Colors.Warning},
		{"success", &theme.Colors.Success},
		{"dangerText", &theme.Colors.DangerText},
		{"warningText", &theme.Colors.WarningText},
		{"successText", &theme.Colors.SuccessText},
		{"readingSurface", &theme.Colors.ReadingSurf},
		{"raisedSurface", &theme.Colors.RaisedSurf},
		{"composerSurface", &theme.Colors.ComposerSurf},
	}
	valid := make(map[string]ui.Color, len(fields))
	for _, field := range fields {
		color, err := parseThemeColor(*field.value, entryPath, field.name)
		if err != nil {
			*field.value = ""
			issues = append(issues, themeColorIssue(entryPath, field.name, err))
			continue
		}
		if strings.TrimSpace(*field.value) == "" {
			continue
		}
		valid[field.name] = color
	}
	parsed := make(map[string]ui.Color, len(valid))
	for name, color := range valid {
		parsed[name] = color
	}
	theme.Parsed = parsed
	return theme, issues
}

// parseThemeColor validates one hex colour without panicking.
func parseThemeColor(value, entryPath, name string) (ui.Color, error) {
	if strings.TrimSpace(value) == "" {
		return ui.Color{}, nil
	}
	color, err := parseHexColor(value)
	if err != nil {
		return ui.Color{}, fmt.Errorf("theme entry %s colour %s is invalid: %v",
			entryPath, name, err)
	}
	return color, nil
}

func themeColorIssue(entryPath, name string, err error) pluginRegistryIssue {
	return pluginRegistryIssue{
		Message: fmt.Sprintf("theme entry %s colour %s cannot be used: %v",
			entryPath, name, err),
	}
}

func readPluginThemeFile(sourceDirectory, entryPath string) (*pluginThemeFile, error) {
	filename, info, err := resolvePluginPath(sourceDirectory, entryPath)
	if err != nil {
		return nil, fmt.Errorf("entry %s cannot be read", entryPath)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("entry %s is not a regular file", entryPath)
	}
	if info.Size() > maxPluginLocaleFileBytes {
		return nil, fmt.Errorf("entry %s is larger than %d bytes",
			entryPath, maxPluginLocaleFileBytes)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("entry %s cannot be read", entryPath)
	}
	if len(data) > maxPluginLocaleFileBytes {
		return nil, fmt.Errorf("entry %s is larger than %d bytes", entryPath, maxPluginLocaleFileBytes)
	}
	var theme pluginThemeFile
	if err := json.Unmarshal(data, &theme); err != nil {
		return nil, fmt.Errorf("entry %s is not a valid theme file: %w", entryPath, err)
	}
	return &theme, nil
}

func themeIssue(entry pluginRegistryEntry, contributionID string, err error) pluginRegistryIssue {
	return pluginRegistryIssue{
		Path:    entry.ManifestPath,
		Message: fmt.Sprintf("theme contribution %s cannot be used: %v", contributionID, err),
	}
}

func firstString(value, alternate, last string) string {
	switch {
	case strings.TrimSpace(value) != "":
		return value
	case strings.TrimSpace(alternate) != "":
		return alternate
	}
	return last
}

func pluginThemeOverlaySelected() *pluginThemeSelection {
	return pluginThemeSelected.Load()
}

func selectPluginThemeResource(selection pluginThemeSelection) {
	pluginThemeSelected.Store(&selection)
	republishCurrentPluginThemeOverlay()
}

func clearSelectedPluginThemeResource() {
	pluginThemeSelected.Store(nil)
	republishCurrentPluginThemeOverlay()
}

// rebuildPluginThemeOverlay is an unused legacy helper. Production publish
// paths use withPluginThemeResources; selection changes reuse cached resources.
func rebuildPluginThemeOverlay() {
	registry, _ := appThemeRegistrySnapshot()
	overlay, _ := buildPluginThemeResources(registry.Entries)
	publishPluginThemeOverlay(overlay)
}

// republishCurrentPluginThemeOverlay raises the generation and notifies the
// WebViews without re-reading files, so a click inside a render frame never
// does IO and a selection can never discard fresh issues.
func republishCurrentPluginThemeOverlay() {
	publishPluginThemeOverlay(currentPluginThemeOverlay())
}

func publishPluginThemeOverlay(overlay *pluginThemeOverlay) {
	pluginThemeCurrent.Store(overlay)
	pluginThemeGenerationMu.Lock()
	pluginThemeGeneration++
	pluginThemeGenerationMu.Unlock()
	syncPluginWebViewThemeAfterThemeChange()
}

func currentPluginThemeOverlay() *pluginThemeOverlay {
	return pluginThemeCurrent.Load()
}

func currentPluginThemeGeneration() uint64 {
	pluginThemeGenerationMu.Lock()
	defer pluginThemeGenerationMu.Unlock()
	return pluginThemeGeneration
}

func appThemeRegistrySnapshot() (pluginRegistry, error) {
	return appThemeRegistrySnapshotFn()
}

// appThemeRegistrySnapshotFn lets tests replace the source of the current
// registry without a full app state.
var appThemeRegistrySnapshotFn = defaultThemeRegistrySnapshot

func defaultThemeRegistrySnapshot() (pluginRegistry, error) {
	if appStateRef == nil {
		return pluginRegistry{}, fmt.Errorf("app state not ready")
	}
	return appStateRef.pluginRegistrySnapshot(), nil
}

var appStateRef *appState

func syncPluginWebViewThemeAfterThemeChange() {
	if appStateRef == nil || appStateRef.window == nil {
		return
	}
	appStateRef.syncPluginWebViewTheme(
		appStateRef.effectiveDark(),
		appStateRef.highContrastActive(currentAppearanceHighContrast()),
		currentAppearanceReduceTransparency(),
	)
}

// withPluginThemeResources reads theme resources from a freshly scanned
// registry and republishes the overlay. Issues follow the same registry path as
// locale issues, so a bad file is an ordinary plugin problem, not a silent
// blank.
func withPluginThemeResources(registry pluginRegistry) pluginRegistry {
	overlay, issues := buildPluginThemeResources(registry.Entries)
	publishPluginThemeOverlay(overlay)
	if len(issues) > 0 {
		registry.Issues = append(append([]pluginRegistryIssue(nil),
			registry.Issues...), issues...)
	}
	return registry
}

// themeColorsForOverlay returns the immutable per-mode colour map a selected
// theme resource contributes, without parsing hex or allocating per frame. The
// caller applies the accessibility rules on its own copy, so the loaded map is
// never modified.
func themeColorsForOverlay(
	overlay *pluginThemeOverlay,
	palette palette,
) map[string]ui.Color {
	if overlay == nil {
		return nil
	}
	selection := pluginThemeOverlaySelected()
	if selection == nil {
		return nil
	}
	for _, resource := range overlay.resources {
		if resource.pluginID != selection.PluginID || resource.entry.ID != selection.ResourceID {
			continue
		}
		// A field missing from the matching mode keeps the palette token and
		// never borrows the other mode's value.
		if palette.dark {
			return resource.dark.colors
		}
		return resource.light.colors
	}
	return nil
}

// themeFontsForSelection reports the body and code families the selected theme
// resource carries. Zero strings keep the active families.
func themeFontsForSelection(overlay *pluginThemeOverlay) (string, string) {
	if overlay == nil {
		return "", ""
	}
	selection := pluginThemeOverlaySelected()
	if selection == nil {
		return "", ""
	}
	for _, resource := range overlay.resources {
		if resource.pluginID == selection.PluginID && resource.entry.ID == selection.ResourceID {
			return resource.fontFamily, resource.codeFontFamily
		}
	}
	return "", ""
}

// setPluginThemeSelection persists one choice and notifies the WebViews with
// the overlay the publish paths already loaded, so a click never re-reads
// files. The empty selection means system fallback: the stored choice is
// cleared and the palette returns to tokens, but the choice is remembered
// until the user picks a resource again or the resource disappears.
func setPluginThemeSelection(selection *pluginThemeSelection) {
	pluginThemeSelected.Store(selection)
	republishCurrentPluginThemeOverlay()
	if appStateRef == nil {
		return
	}
	if err := appStateRef.persistPluginThemeSelection(selection); err != nil {
		appStateRef.showNotice(text("noticePreferenceSaveTheme"))
	}
}

// selectPluginTheme persists a theme choice through the app state, so the
// settings row and the plugin detail card share one path.
func (s *appState) setPluginThemeResource(selection *pluginThemeSelection) {
	if selection == nil {
		setPluginThemeSelection(nil)
		return
	}
	setPluginThemeSelection(selection)
}

// pluginThemeChoicesForEntry lists the theme resources one registered entry
// contributes. The card renders nothing when the plugin declares none or is
// not registered.
func pluginThemeChoicesForEntry(entry pluginRegistryEntry) []pluginThemeContribution {
	if entry.Status != pluginRegistered || entry.Manifest == nil {
		return nil
	}
	return entry.Manifest.Contributes.Themes
}

// applyStartupPluginThemeSelection re-applies the saved choice after startup.
// A resource that disappeared keeps the stored choice and leaves the palette on
// tokens until the resource is offered again.
func applyStartupPluginThemeSelection() {
	if appStateRef == nil {
		return
	}
	preferences := appStateRef.preferences
	if preferences.PluginThemePluginID == "" || preferences.PluginThemeResourceID == "" {
		return
	}
	selection := &pluginThemeSelection{
		PluginID:   preferences.PluginThemePluginID,
		ResourceID: preferences.PluginThemeResourceID,
	}
	pluginThemeSelected.Store(selection)
}

// parseHexColor validates one hex colour against the shapes MyGo accepts,
// without the panic ui.Hex uses for program input.
func parseHexColor(value string) (ui.Color, error) {
	shape := strings.TrimSpace(value)
	if strings.HasPrefix(shape, "#") {
		digits := len(shape) - 1
		if digits == 3 || digits == 4 || digits == 6 || digits == 8 {
			all := true
			for _, char := range shape[1:] {
				if !strings.ContainsRune("0123456789abcdefABCDEF", char) {
					all = false
				}
			}
			if all {
				return ui.Hex(shape), nil
			}
		}
	}
	return ui.Color{}, fmt.Errorf("invalid color %q", shape)
}
