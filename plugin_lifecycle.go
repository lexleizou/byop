package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	pluginLifecycleDirectoryName  = "plugin-lifecycle"
	pluginDisabledPreferencesFile = "disabled.json"
	maxPluginDisabledFileBytes    = 64 << 10
)

// pluginDisabledDocument is the on-disk form of the persistent disable set.
// Disabling never touches plugin-data, so plugin user data survives.
type pluginDisabledDocument struct {
	Version  int      `json:"version"`
	Disabled []string `json:"disabled"`
}

// pluginDisabledStore keeps the plugin ids the user disabled on purpose, one
// file per configured directory so BYOP_CONFIG_DIR and BYOP_PLUGINS_DIR tests
// stay isolated from the real ~/.byop.
type pluginDisabledStore struct {
	mu        sync.Mutex
	directory string
	warning   string
}

func newPluginDisabledStore(configDir string) *pluginDisabledStore {
	return &pluginDisabledStore{
		directory: filepath.Join(configDir, pluginLifecycleDirectoryName),
	}
}

func (store *pluginDisabledStore) path() string {
	return filepath.Join(store.directory, pluginDisabledPreferencesFile)
}

// save writes the persistent set. When the previous read failed for any reason
// other than the file being absent, saving is refused so a damaged file can
// never silently re-enable plugins the user had disabled.
func (store *pluginDisabledStore) save(ids []string) error {
	return store.update(func([]string) []string { return ids })
}

// update applies one read-modify-write step under a single lock. ok is false
// when the current content could not be read, in which case nothing is written.
func (store *pluginDisabledStore) update(transform func(current []string) []string) error {
	if store == nil {
		return nil
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	current, readable := store.loadLocked()
	if !readable {
		return errors.New("plugin enable states could not be read; the file was left unchanged")
	}
	data, err := json.MarshalIndent(pluginDisabledDocument{
		Version:  1,
		Disabled: sortedUniquePluginIDs(transform(current)),
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode plugin enable states: %w", err)
	}
	data = append(data, '\n')
	if len(data) > maxPluginDisabledFileBytes {
		return errors.New("plugin enable states exceed 64 KiB")
	}
	if err := os.MkdirAll(store.directory, 0o700); err != nil {
		return fmt.Errorf("create plugin lifecycle directory: %w", err)
	}
	if err := os.Chmod(store.directory, 0o700); err != nil {
		return fmt.Errorf("set plugin lifecycle directory permissions: %w", err)
	}
	if err := atomicWritePluginFile(store.path(), data); err != nil {
		return err
	}
	store.warning = ""
	return nil
}

func (store *pluginDisabledStore) currentWarning() string {
	if store == nil {
		return ""
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.warning
}

// load returns the disabled ids; an unreadable file yields an empty set plus a
// warning, which callers use for a best-effort scan.
func (store *pluginDisabledStore) load() []string {
	ids, _ := store.loadChecked()
	return ids
}

// loadChecked also reports whether the file was readable, so a mirror can keep
// its previous content instead of re-enabling everything on a read failure.
func (store *pluginDisabledStore) loadChecked() ([]string, bool) {
	if store == nil {
		return nil, true
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.loadLocked()
}

// loadLocked reads the file and reports whether the read succeeded. The caller
// holds store.mu.
func (store *pluginDisabledStore) loadLocked() ([]string, bool) {
	if store == nil {
		return nil, true
	}
	data, err := os.ReadFile(store.path())
	if errors.Is(err, os.ErrNotExist) {
		store.warning = ""
		return nil, true
	}
	if err != nil {
		store.warning = "Plugin enable states could not be read; previous states are kept."
		return nil, false
	}
	ids, decodeErr := decodePluginDisabledDocument(data)
	if decodeErr != nil {
		store.warning = "Plugin enable states are damaged; previous states are kept."
		return nil, false
	}
	store.warning = ""
	return ids, true
}

func decodePluginDisabledDocument(data []byte) ([]string, error) {
	if len(data) > maxPluginDisabledFileBytes {
		return nil, errors.New("plugin enable states exceed 64 KiB")
	}
	var document pluginDisabledDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("more than one JSON value")
	}
	if document.Version != 1 {
		return nil, fmt.Errorf("unsupported version %d", document.Version)
	}
	return sortedUniquePluginIDs(document.Disabled), nil
}

func sortedUniquePluginIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

// pluginDisabledIDSet normalizes persisted ids, dropping blanks and duplicates.
func pluginDisabledIDSet(ids []string) map[string]bool {
	disabled := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			disabled[id] = true
		}
	}
	return disabled
}

// lastPluginDisabledState remembers the last set read successfully per config
// directory, so a damaged file falls back to real user choices instead of
// silently enabling every disabled plugin at scan time and at the next Pi
// start. The store's own warning (disabledStoreWarning) still tells the user
// that the file is damaged.
var lastPluginDisabledState sync.Map // configDir -> []string

func rememberPluginDisabledIDs(configDir string, ids []string) {
	if strings.TrimSpace(configDir) == "" {
		return
	}
	next := make([]string, len(ids))
	copy(next, ids)
	lastPluginDisabledState.Store(configDir, next)
}

func pluginDisabledLastKnownIDs(configDir string) []string {
	value, ok := lastPluginDisabledState.Load(configDir)
	if !ok {
		return nil
	}
	ids, _ := value.([]string)
	return ids
}

// applyPluginDisabledStates marks registered plugins the user disabled. The
// manifest stays attached so the Plugins page can still show details and offer
// re-enable, while every contribution lookup skips the entry because it only
// accepts pluginRegistered.
func applyPluginDisabledStates(registry *pluginRegistry, disabled map[string]bool) {
	if registry == nil || len(disabled) == 0 {
		return
	}
	for index := range registry.Entries {
		entry := &registry.Entries[index]
		if entry.Status != pluginRegistered || !disabled[entry.ID] {
			continue
		}
		entry.Status = pluginDisabled
		entry.ContributionCount = 0
	}
}

// pluginRuntimeBusyState reports the states where a plugin process is present
// or about to serve a call, so reclaim and rescan must leave it alone.
func pluginRuntimeBusyState(state pluginRuntimeState) bool {
	switch state {
	case pluginStateStarting, pluginStateActive, pluginStateRunning, pluginStateStopping:
		return true
	default:
		return false
	}
}

// pluginRegistryIndexByID maps plugin id to entry. Entries are sorted by id, so
// a duplicate id keeps the first registration, matching duplicate marking.
func pluginRegistryIndexByID(registry pluginRegistry) map[string]pluginRegistryEntry {
	index := make(map[string]pluginRegistryEntry, len(registry.Entries))
	for _, entry := range registry.Entries {
		if _, exists := index[entry.ID]; !exists {
			index[entry.ID] = entry
		}
	}
	return index
}

// pluginManifestChanged compares the contribution-bearing parts of two
// manifests so a rescan can tell whether a running plugin must restart.
func pluginManifestChanged(previous, current *pluginManifest) bool {
	if previous == nil || current == nil {
		return previous != current
	}
	previousCopy, currentCopy := *previous, *current
	previousCopy.sourceDirectory = ""
	currentCopy.sourceDirectory = ""
	previousCopy.localization = nil
	currentCopy.localization = nil
	previousJSON, previousErr := json.Marshal(previousCopy)
	currentJSON, currentErr := json.Marshal(currentCopy)
	if previousErr != nil || currentErr != nil {
		return true
	}
	return !bytes.Equal(previousJSON, currentJSON)
}

func pluginEntryRuntimeChanged(previous, current pluginRegistryEntry) bool {
	if filepath.Clean(previous.SourceDirectory) != filepath.Clean(current.SourceDirectory) {
		return true
	}
	if previous.Status != current.Status {
		return true
	}
	return pluginManifestChanged(previous.Manifest, current.Manifest)
}

// pluginScanTarget is one plugin process decision produced by an incremental
// rescan. Only the listed ids stop; unrelated processes keep running.
type pluginScanTarget struct {
	pluginID        string
	sourceDirectory string
	reason          string
}

// pluginScanPlan is the diff between two registries for one scan round.
type pluginScanPlan struct {
	Stop    []pluginScanTarget
	Removed []string
	Changed []string
}

func (plan pluginScanPlan) stopsAny() bool {
	return len(plan.Stop) > 0
}

// planPluginScan compares the previous registry with the new one. An entry that
// disappeared, changed, or moved between enabled and disabled stops; every
// other plugin keeps its process, WebView and Pi session untouched. A plugin
// that only became enabled is lazy, so the plan starts nothing.
func planPluginScan(previous, current pluginRegistry) pluginScanPlan {
	plan := pluginScanPlan{}
	previousByID := pluginRegistryIndexByID(previous)
	currentByID := pluginRegistryIndexByID(current)
	for _, entry := range previous.Entries {
		if _, stillThere := currentByID[entry.ID]; stillThere {
			continue
		}
		plan.Stop = append(plan.Stop, pluginScanTarget{
			pluginID:        entry.ID,
			sourceDirectory: entry.SourceDirectory,
			reason:          "removed",
		})
		plan.Removed = append(plan.Removed, entry.ID)
	}
	for _, entry := range current.Entries {
		previousEntry, existed := previousByID[entry.ID]
		if !existed {
			continue
		}
		if pluginEntryRuntimeChanged(previousEntry, entry) {
			reason := "changed"
			if entry.Status == pluginDisabled {
				reason = "disabled"
			} else if previousEntry.Status == pluginDisabled {
				reason = "enabled"
			}
			plan.Stop = append(plan.Stop, pluginScanTarget{
				pluginID:        entry.ID,
				sourceDirectory: entry.SourceDirectory,
				reason:          reason,
			})
			plan.Changed = append(plan.Changed, entry.ID)
		}
	}
	return plan
}

// stopPluginsForScan deactivates only the listed plugin ids. It is the
// incremental counterpart of stopAll and never touches another plugin's
// process, cached WebView or Pi session. Each target is marked as stopping
// first, so a UI action or automation cannot spawn a new generation during the
// stop window; the caller releases the gate when it publishes the registry.
func (h *pluginHost) stopPluginsForScan(targets []pluginScanTarget) {
	if h == nil || len(targets) == 0 {
		return
	}
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if target.pluginID != "" {
			h.beginPluginStop(target.pluginID)
		}
	}
	var wg sync.WaitGroup
	for _, target := range targets {
		if target.pluginID == "" {
			continue
		}
		if _, duplicate := seen[target.pluginID]; duplicate {
			continue
		}
		seen[target.pluginID] = struct{}{}
		wg.Add(1)
		go func(pluginID string) {
			defer wg.Done()
			_ = h.deactivate(pluginID)
		}(target.pluginID)
	}
	wg.Wait()
}

// stopPluginNow stops one plugin for a deliberate disable or a reclaim round.
func (h *pluginHost) stopPluginNow(pluginID string) error {
	if h == nil {
		return nil
	}
	return h.deactivate(pluginID)
}

// pluginProcessForReclaim captures the exact process a reclaim round judges, so
// the later stop can never detach a different generation. process is nil when no
// worker is registered; idle is true only when that worker finished its last
// command, and idleFor is how long it has been idle.
func (h *pluginHost) pluginProcessForReclaim(
	pluginID string,
) (process *pluginProcess, idle bool, idleFor time.Duration) {
	if h == nil {
		return nil, false, 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	process = h.processes[pluginID]
	if process == nil || process.exited() {
		return nil, false, 0
	}
	status, ok := h.statuses[pluginID]
	if !ok {
		return process, false, 0
	}
	if status.State != pluginStateActive || status.UpdatedAt.IsZero() {
		return process, false, 0
	}
	return process, true, time.Since(status.UpdatedAt)
}

func (h *pluginHost) activePluginIDs() []string {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	ids := make([]string, 0, len(h.processes))
	for id := range h.processes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// pluginRuntimeIDs is the reclaim candidate set from the runtime side: a plugin
// process or a status record that still counts as busy.
func (h *pluginHost) pluginRuntimeIDs() []string {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	seen := make(map[string]struct{}, len(h.processes)+len(h.statuses))
	for id := range h.processes {
		seen[id] = struct{}{}
	}
	for id, status := range h.statuses {
		if pluginRuntimeBusyState(status.State) {
			seen[id] = struct{}{}
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (h *pluginHost) disabledPluginIDs() []string {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	ids := make([]string, 0, len(h.disabledIDs))
	for id := range h.disabledIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (h *pluginHost) disabledPluginIDSet() map[string]bool {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	states := make(map[string]bool, len(h.disabledIDs))
	for id, disabled := range h.disabledIDs {
		states[id] = disabled
	}
	return states
}

// setPluginDisabled persists one toggle as a read-modify-write on the stored
// set, so other disabled ids survive. An unreadable file is an error and leaves
// the file untouched.
func (h *pluginHost) setPluginDisabled(pluginID string, disabled bool) error {
	if h == nil {
		return errors.New("plugin host is unavailable")
	}
	if !pluginIDPattern.MatchString(pluginID) {
		return fmt.Errorf("invalid plugin id %q", pluginID)
	}
	return h.disabled.update(func(current []string) []string {
		next := make([]string, 0, len(current)+1)
		present := false
		for _, id := range current {
			if id == pluginID {
				present = true
				if !disabled {
					continue
				}
			}
			next = append(next, id)
		}
		if disabled && !present {
			next = append(next, pluginID)
		}
		return next
	})
}

func (h *pluginHost) disabledStoreWarning() string {
	if h == nil {
		return ""
	}
	return h.disabled.currentWarning()
}

// scanPluginRegistryForRuntime scans manifests and applies the persistent
// enable states for the current config directory. The Pi launch paths use it so
// a disabled plugin's agent extension never reloads. A scan stays read-only: it
// never writes a marker, because UI scans, tests and argument construction share
// this path.
func scanPluginRegistryForRuntime() pluginRegistry {
	configDir, err := pluginConfigDir()
	if err != nil {
		configDir = ""
	}
	return scanPluginRegistryWithDisabled(defaultPluginRoots(), configDir)
}

// Runtime-state marker (H1). The marker is deliberately separate from
// runtime-generation.json: the MCP proxy compares generation file content, so a
// enable toggle stored there would drain, reload and respawn the backend.
const pluginRuntimeStateFilename = "runtime-state.json"

type pluginRuntimeStateMarker struct {
	Version   int    `json:"version"`
	PluginID  string `json:"pluginId"`
	Enabled   bool   `json:"enabled"`
	UpdatedAt string `json:"updatedAt"`
}

func pluginRuntimeStatePath(configDir, pluginID string) (string, error) {
	directory, err := pluginDataDir(configDir, pluginID)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, pluginRuntimeStateFilename), nil
}

// writePluginRuntimeStateMarker records the deliberate enable choice so the Pi
// extension can revoke or restore its contributions at a turn boundary.
func writePluginRuntimeStateMarker(configDir, pluginID string, enabled bool) error {
	path, err := pluginRuntimeStatePath(configDir, pluginID)
	if err != nil {
		return err
	}
	marker := pluginRuntimeStateMarker{
		Version:   1,
		PluginID:  pluginID,
		Enabled:   enabled,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	data, err := json.Marshal(marker)
	if err != nil {
		return fmt.Errorf("encode runtime state: %w", err)
	}
	if err := atomicWritePluginFile(path, data); err != nil {
		return fmt.Errorf("write runtime state: %w", err)
	}
	return nil
}

// readPluginRuntimeStateEnabled reports the marker. A missing or damaged file
// means enabled, which matches the extension contract E1.
func readPluginRuntimeStateEnabled(configDir, pluginID string) bool {
	path, err := pluginRuntimeStatePath(configDir, pluginID)
	if err != nil {
		return true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	var marker pluginRuntimeStateMarker
	if err := json.Unmarshal(data, &marker); err != nil || marker.PluginID != pluginID {
		return true
	}
	return marker.Enabled
}

// pluginNeedsAgentExtension reports whether one entry loads code into Pi, which
// is the only case where a marker repair at Pi start matters.
func pluginNeedsAgentExtension(entry pluginRegistryEntry) bool {
	return entry.Manifest != nil &&
		(entry.Manifest.Contributes.AgentExtension != "" ||
			entry.Manifest.Contributes.AgentProxy != "")
}

// selfHealPluginRuntimeStateMarkers writes enabled:true for every enabled plugin
// whose extension is loaded, repairing a crash between the disabled set and the
// marker. It never writes false, so a disabled plugin keeps its marker.
func selfHealPluginRuntimeStateMarkers(configDir string, registry pluginRegistry) int {
	if strings.TrimSpace(configDir) == "" {
		return 0
	}
	repaired := 0
	for _, entry := range registry.Entries {
		if entry.Status != pluginRegistered || !pluginNeedsAgentExtension(entry) {
			continue
		}
		if readPluginRuntimeStateEnabled(configDir, entry.ID) {
			continue
		}
		if err := writePluginRuntimeStateMarker(configDir, entry.ID, true); err == nil {
			repaired++
		}
	}
	return repaired
}

// piLoadedExtension is what the host knows about one extension the running Pi
// process loaded: its file signature, and whether the author declared the
// cooperative lifecycle in the manifest Pi was actually launched with. The
// flag is captured from that launch-time manifest and never re-derived from a
// later registry, because a later edit could flip it.
type piLoadedExtension struct {
	signature   string
	cooperative bool
}

// piLoadedExtensionSet is the loaded set from preflight H4: extension id ->
// per-entry snapshot, for the extensions a Pi start of this registry loads. It
// is recorded when Pi connects, so a later decision compares against what is
// actually in the running Pi process.
func piLoadedExtensionSet(registry pluginRegistry) map[string]piLoadedExtension {
	loaded := make(map[string]piLoadedExtension)
	for _, entry := range registry.Entries {
		if entry.Status != pluginRegistered || entry.Manifest == nil ||
			entry.Manifest.Contributes.AgentExtension == "" {
			continue
		}
		loaded[entry.ID] = piLoadedExtension{
			signature: registeredPluginPiEntrySignature(entry),
			cooperative: entry.Manifest.Contributes.AgentExtensionLifecycle ==
				pluginAgentExtensionLifecycleCooperative,
		}
	}
	return loaded
}

// pluginRefreshNeededForExtensions decides the Pi refresh from the set Pi loaded
// at its start, not from a diff of two registries. A refresh is needed when a
// plugin Pi should load has a changed extension file or is missing from the
// loaded set, and also when a loaded extension that is no longer enabled and
// registered — disabled, removed, failed, or its extension field dropped — is
// non-cooperative, because only unloading Pi takes its tools away. A
// cooperative extension needs none of that: the runtime-state marker revokes it
// in place at a turn boundary, and re-enabling it with an unchanged file needs
// no refresh either, because it is still loaded and registers itself again.
func pluginRefreshNeededForExtensions(loaded map[string]piLoadedExtension, fresh pluginRegistry) bool {
	for _, entry := range fresh.Entries {
		if entry.Status != pluginRegistered || entry.Manifest == nil ||
			entry.Manifest.Contributes.AgentExtension == "" {
			continue
		}
		previous, ok := loaded[entry.ID]
		if !ok || previous.signature != registeredPluginPiEntrySignature(entry) {
			return true
		}
	}
	// A loaded extension whose plugin is no longer registered and enabled stays
	// running in Pi's memory unless the author declared it cooperative, so the
	// user's disable or uninstall is not honest until Pi refreshes.
	enabled := make(map[string]bool, len(fresh.Entries))
	for _, entry := range fresh.Entries {
		if entry.Status == pluginRegistered && entry.Manifest != nil &&
			entry.Manifest.Contributes.AgentExtension != "" {
			enabled[entry.ID] = true
		}
	}
	for id, extension := range loaded {
		if !extension.cooperative && !enabled[id] {
			return true
		}
	}
	return false
}

// setPluginEnabled applies a deliberate enable choice. The in-memory set, the
// persistent file and the runtime-state marker are written first, then this
// plugin's own cached WebViews are released, and finally a rescan publishes the
// registry, which stops only this plugin's worker. Other plugins, the Pi process
// and its session keep running. A disable is revoked in place only for a
// cooperative Pi extension, whose marker it flips; a loaded non-cooperative
// extension keeps contributing inside the running Pi until the user refreshes
// Pi, and the notice says so. A re-enable stays lazy until the plugin is used
// again.
func (s *appState) setPluginEnabled(entry pluginRegistryEntry, enabled bool) {
	if s == nil || s.plugins == nil || entry.ID == "" {
		return
	}
	name := fallback(pluginLocalizedEntry(entry).Name, entry.ID)
	if err := s.plugins.setPluginDisabledState(entry.ID, !enabled); err != nil {
		s.showNotice(textArgs("pluginsEnableFailed", map[string]any{
			"name":  name,
			"error": uiErrorSummary(err, text("errorUnknown"), 200),
		}))
		return
	}
	// A marker failure never aborts the toggle: the enable choice is already
	// persistent, so the views are released and the rescan runs, and only the
	// in-session Pi revoke is delayed. Aborting would leave the worker and
	// cached WebViews running under a choice the file already recorded.
	markerErr := writePluginRuntimeStateMarker(s.plugins.configDir, entry.ID, enabled)
	// Release this plugin's cached WebViews before the rescan; the worker stop
	// belongs to the scan so the stop gate and the scan sequence stay in charge.
	s.destroyPluginWebViews(entry.ID)
	s.clearPluginHiddenSince(entry.ID)
	s.scanPlugins(false)
	// The marker warning is shown after the scan so its notice cannot bury it.
	if markerErr != nil {
		s.showNotice(textArgs("pluginsRuntimeStateFailed", map[string]any{
			"name":  name,
			"error": uiErrorSummary(markerErr, text("errorUnknown"), 200),
		}))
		return
	}
	if enabled {
		s.showNotice(textArgs("pluginsEnabled", map[string]any{"name": name}))
		return
	}
	// The host is fully disabled either way; only the Pi-side revoke differs.
	// The decision uses this plugin's entry in the loaded snapshot, because
	// another plugin's refresh requirement must not change this notice.
	if extension, loaded := s.piLoadedExtensions[entry.ID]; loaded && !extension.cooperative {
		s.showNotice(textArgs("pluginsDisabledRefreshRequired", map[string]any{"name": name}))
		return
	}
	s.showNotice(textArgs("pluginsDisabled", map[string]any{"name": name}))
}

// setPiLoadedExtensions records the extension snapshot of the Pi client that
// is becoming current. The snapshot is captured immutably when that client's
// process started, from the exact registry its arguments were built from, and
// a scan never overwrites it: a refresh decision must compare against what the
// running process actually loaded, not what a later registry shows.
func (s *appState) setPiLoadedExtensions(client *piClient) {
	if s == nil || client == nil || !client.loadedExtensionsKnown {
		return
	}
	s.piLoadedExtensions = client.loadedExtensions
	s.piLoadedExtensionsKnown = true
}

// reconcileLoadedCooperativeMarkers keeps the runtime-state marker honest for
// the cooperative extensions the running Pi process actually loaded, which is
// the only set whose in-place revoke depends on the marker. A deliberate enable
// choice or uninstall already writes it; this covers every other way a loaded
// cooperative extension leaves the registry — folder removed or moved outside
// the app, an invalid or incompatible manifest, a duplicate id, a dropped
// agentExtension, an externally edited disabled set — and also a marker write
// that failed on the deliberate paths, since each of those ends in a scan.
// Registry scans stay read-only: this runs only where a scan is applied, and
// touches host config for the loaded ids alone.
func (s *appState) reconcileLoadedCooperativeMarkers(fresh pluginRegistry) {
	if s == nil || !s.piLoadedExtensionsKnown || s.plugins == nil ||
		strings.TrimSpace(s.plugins.configDir) == "" {
		return
	}
	refreshFallback := false
	for id, loaded := range s.piLoadedExtensions {
		if !loaded.cooperative {
			continue
		}
		var live *pluginRegistryEntry
		for _, entry := range fresh.Entries {
			// The disabled check reads the host's in-memory choice, so a stale
			// scan computed before a disable cannot write the marker back to
			// true: setPluginDisabledState does not bump the scan sequence.
			if entry.ID == id && entry.Status == pluginRegistered &&
				entry.Manifest != nil && entry.Manifest.Contributes.AgentExtension != "" &&
				!s.plugins.isPluginDisabled(id) {
				liveEntry := entry
				live = &liveEntry
				break
			}
		}
		// A loaded cooperative id with no live entry is revoked. With a live
		// entry, only that very entry reactivates the marker, and only when its
		// signature equals the loaded one — the same extension reappeared, so
		// the loaded code is still the right code. A live entry with a
		// different file is new code the running Pi does not have: the refresh
		// is already raised for it and launch self-heal writes true there, so
		// the marker stays as it is. A same-id entry that is not live never
		// takes part in the comparison.
		desired := live != nil &&
			registeredPluginPiEntrySignature(*live) == loaded.signature
		if live != nil && !desired {
			continue
		}
		// The read is fail-open like the extension itself, so a missing marker
		// with desired=true needs no write.
		if readPluginRuntimeStateEnabled(s.plugins.configDir, id) == desired {
			continue
		}
		if err := writePluginRuntimeStateMarker(s.plugins.configDir, id, desired); err != nil {
			refreshFallback = true
		}
	}
	if refreshFallback && s.pi != nil {
		// The marker is the only in-place revoke a cooperative extension has.
		// Without it, a Pi refresh is the honest path, so fall back to the
		// existing prompt rather than claiming a revoke that did not happen.
		s.pluginPiRefreshRequired = true
	}
}

// refreshPiForExtensionSet raises the Pi refresh prompt when the plugin set now
// loaded needs extension code that the running Pi process does not have, when a
// loaded extension file changed on disk, or when a loaded non-cooperative
// extension is no longer enabled or registered — disabling or removing such a
// plugin only takes effect in Pi at the refresh. A cooperative extension is
// revoked in place at a turn boundary when the host can write its marker; when
// the marker cannot be written the scan reconcile raises this same prompt, and
// re-enabling one whose file is unchanged needs no refresh.
func (s *appState) refreshPiForExtensionSet(fresh pluginRegistry) {
	if s == nil || !s.piLoadedExtensionsKnown {
		return
	}
	if pluginRefreshNeededForExtensions(s.piLoadedExtensions, fresh) && s.pi != nil {
		s.pluginPiRefreshRequired = true
	}
}
