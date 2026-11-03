package main

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// pluginPackagePicker shows one native open dialog. modeFolder chooses a
// directory, modeZip a .zip package. MyGo's picker supports both: the channel
// settings importer uses the same dialog with Directory and extension filters.
type pluginPackagePicker func(parent *mygo.Window, mode pluginPackagePickerMode) (string, error)

type pluginPackagePickerMode uint8

const (
	pluginPackagePickFolder pluginPackagePickerMode = iota
	pluginPackagePickZip
)

func defaultPluginPackagePicker(
	parent *mygo.Window,
	mode pluginPackagePickerMode,
) (string, error) {
	options := mygo.OpenDialogOptions{Parent: parent}
	switch mode {
	case pluginPackagePickZip:
		options.Title = text("pluginsInstallZipDialog")
		options.Filters = []mygo.FileFilter{{
			Name:       "ZIP",
			Extensions: []string{"zip"},
		}}
	default:
		options.Title = text("pluginsInstallFolderDialog")
		options.Directory = true
	}
	paths, err := mygo.Dialog.Open(options)
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

// pluginPackagesUIState is per-app state for the package actions, so two
// windows never share a busy flag or an error line.
type pluginPackagesUIState struct {
	picker pluginPackagePicker

	mu        sync.Mutex
	busy      map[string]bool
	errOwner  string // "" for install errors, otherwise the plugin id
	errKey    string
	errDetail string
}

var pluginPackageStates sync.Map // *appState -> *pluginPackagesUIState

func pluginPackagesState(s *appState) *pluginPackagesUIState {
	if value, ok := pluginPackageStates.Load(s); ok {
		return value.(*pluginPackagesUIState)
	}
	state := &pluginPackagesUIState{
		picker: defaultPluginPackagePicker,
		busy:   make(map[string]bool),
	}
	value, _ := pluginPackageStates.LoadOrStore(s, state)
	return value.(*pluginPackagesUIState)
}

func resetPluginPackagesForTest(s *appState) {
	if s != nil {
		pluginPackageStates.Delete(s)
	}
}

func installPluginPackagesTestDependencies(
	s *appState,
	picker pluginPackagePicker,
) *pluginPackagesUIState {
	state := &pluginPackagesUIState{picker: picker, busy: make(map[string]bool)}
	pluginPackageStates.Store(s, state)
	return state
}

func (state *pluginPackagesUIState) isBusy(owner string) bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.busy[owner]
}

func (state *pluginPackagesUIState) begin(owner string) bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.busy[owner] {
		return false
	}
	if state.busy == nil {
		state.busy = make(map[string]bool)
	}
	state.busy[owner] = true
	return true
}

func (state *pluginPackagesUIState) end(owner string) {
	state.mu.Lock()
	delete(state.busy, owner)
	state.mu.Unlock()
}

func (state *pluginPackagesUIState) setError(owner, key, detail string) {
	state.mu.Lock()
	state.errOwner, state.errKey, state.errDetail = owner, key, detail
	state.mu.Unlock()
}

func (state *pluginPackagesUIState) clearError(owner string) {
	state.mu.Lock()
	if state.errOwner == owner {
		state.errOwner, state.errKey, state.errDetail = "", "", ""
	}
	state.mu.Unlock()
}

func (state *pluginPackagesUIState) errorFor(owner string) (string, string) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.errOwner != owner {
		return "", ""
	}
	return state.errKey, state.errDetail
}

// pluginInstallRunAsync runs package work off the UI thread when a window
// exists, and inline otherwise, which is how focused tests drive the same path.
func (s *appState) pluginInstallRunAsync(work func() (pluginPackageResult, error), done func(pluginPackageResult, error)) {
	if s.window == nil {
		result, err := work()
		done(result, err)
		return
	}
	go func() {
		result, err := work()
		s.postUI(func() { done(result, err) })
	}()
}

// pluginInstallActions renders the local-package entries on the Plugins page:
// install from a folder or a zip. Discovery, stores and GitHub URLs stay out of
// this slice. The coordinator calls it from pages.go.
func (s *appState) pluginInstallActions(c *ui.Context, p palette) {
	state := pluginPackagesState(s)
	busy := state.isBusy("")
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		pluginSecondaryActionDisabled(c, p, "plugin-install-folder",
			text("pluginsAddFromFolder"), busy, func() {
				s.pickAndInstallPluginPackage(pluginPackagePickFolder)
			})
		pluginSecondaryActionDisabled(c, p, "plugin-install-zip",
			text("pluginsAddFromZip"), busy, func() {
				s.pickAndInstallPluginPackage(pluginPackagePickZip)
			})
	})
	if key, detail := state.errorFor(""); key != "" {
		s.pluginPackagesError(c, p, "plugin-install-error", key, detail)
	}
}

// pluginPackageActions renders Update and Uninstall for one plugin folder that
// sits directly inside a plugin root. The coordinator calls it from the details
// panel in pages.go.
func (s *appState) pluginPackageActions(c *ui.Context, p palette, entry pluginRegistryEntry) {
	if strings.TrimSpace(entry.SourceDirectory) == "" ||
		!pluginPackageRootManaged(entry.SourceDirectory) {
		return
	}
	state := pluginPackagesState(s)
	busy := state.isBusy(entry.ID) || s.plugins != nil && s.plugins.pluginRestartInProgress(entry.ID)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		pluginSecondaryActionDisabled(c, p, "plugin-package-update-"+entry.ID,
			text("pluginsPackageUpdate"), busy, func() {
				s.pickAndUpdatePluginPackage(entry)
			})
		pluginSecondaryActionDisabled(c, p, "plugin-package-uninstall-"+entry.ID,
			text("pluginsPackageUninstall"), busy, func() {
				s.uninstallPluginPackageEntry(entry)
			})
	})
	if key, detail := state.errorFor(entry.ID); key != "" {
		s.pluginPackagesError(c, p, "plugin-package-error-"+entry.ID, key, detail)
	}
}

func (s *appState) pluginPackagesError(
	c *ui.Context,
	p palette,
	key, messageKey, detail string,
) {
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		ui.Text(c, text(messageKey)).FontSize(fontSmall).TextColor(p.dangerText).Wrap()
		if detail != "" {
			ui.Text(c, detail).Font("monospace").FontSize(fontCaption).
				TextColor(p.muted).Wrap().Selectable()
		}
	})
}

// pluginPackageCommitStop is the guard the package services call after the
// candidate is validated and staged and before the atomic exchange or move. It
// acquires the target restart gate and stops only this plugin's worker, exactly
// like the manual restart path, so a same-version code update — where the scan
// plan sees no manifest change — still replaces the running generation, and no
// worker can start from the old files during the exchange. The caller releases
// the gate after publishing the new state.
func (s *appState) pluginPackageCommitStop(pluginID string) error {
	if s == nil || s.plugins == nil || pluginID == "" {
		return nil
	}
	if err := s.plugins.restartPluginProcess(pluginID); err != nil {
		s.plugins.endPluginRestart(pluginID)
		return err
	}
	return nil
}

func (s *appState) pickAndInstallPluginPackage(mode pluginPackagePickerMode) {
	state := pluginPackagesState(s)
	if state.picker == nil {
		state.setError("", "pluginsInstallFailed", text("errorUnknown"))
		return
	}
	if !state.begin("") {
		return
	}
	picker := state.picker
	parent := s.window
	if parent == nil {
		sourcePath, err := picker(parent, mode)
		s.applyPluginPackageInstallPick(state, sourcePath, err)
		return
	}
	go func() {
		sourcePath, err := picker(parent, mode)
		s.postUI(func() {
			s.applyPluginPackageInstallPick(state, sourcePath, err)
		})
	}()
}

func (s *appState) applyPluginPackageInstallPick(
	state *pluginPackagesUIState,
	sourcePath string,
	err error,
) {
	if err != nil || sourcePath == "" {
		state.end("")
		return
	}
	state.clearError("")
	acquired := false
	guard := func(pluginID string) error {
		if err := s.pluginPackageCommitStop(pluginID); err != nil {
			return err
		}
		acquired = true
		return nil
	}
	s.pluginInstallRunAsync(
		func() (pluginPackageResult, error) {
			return installPluginPackage(sourcePath, guard)
		},
		func(result pluginPackageResult, err error) {
			defer state.end("")
			if acquired {
				s.plugins.endPluginRestart(result.PluginID)
			}
			if err != nil {
				state.setError("", "pluginsInstallFailed", uiErrorSummary(err, text("errorUnknown"), 240))
				s.scanPlugins(false)
				if s.window != nil {
					s.window.Invalidate()
				}
				return
			}
			state.setError("", "", "")
			if acquired {
				s.destroyPluginWebViews(result.PluginID)
				// A package swap can replace the code behind an unchanged
				// declaration, which no signature check would notice, so cached
				// renders for this plugin are dropped here too (review R2).
				s.invalidatePluginRenderers(result.PluginID)
			}
			s.finishPluginPackageChange(result, "pluginsInstalled", pluginPackageFolderName(sourcePath))
		},
	)
}

func (s *appState) pickAndUpdatePluginPackage(entry pluginRegistryEntry) {
	state := pluginPackagesState(s)
	if state.picker == nil {
		state.setError(entry.ID, "pluginsUpdateFailed", text("errorUnknown"))
		return
	}
	if !state.begin(entry.ID) {
		return
	}
	picker := state.picker
	parent := s.window
	if parent == nil {
		sourcePath, err := picker(parent, pluginPackagePickFolder)
		s.applyPluginPackageUpdatePick(state, entry, sourcePath, err)
		return
	}
	go func() {
		sourcePath, err := picker(parent, pluginPackagePickFolder)
		s.postUI(func() {
			s.applyPluginPackageUpdatePick(state, entry, sourcePath, err)
		})
	}()
}

func (s *appState) applyPluginPackageUpdatePick(
	state *pluginPackagesUIState,
	entry pluginRegistryEntry,
	sourcePath string,
	err error,
) {
	if err != nil || sourcePath == "" {
		state.end(entry.ID)
		return
	}
	state.clearError(entry.ID)
	name := fallback(pluginLocalizedEntry(entry).Name, entry.ID)
	acquired := false
	guard := func(pluginID string) error {
		if err := s.pluginPackageCommitStop(pluginID); err != nil {
			return err
		}
		acquired = true
		return nil
	}
	s.pluginInstallRunAsync(
		func() (pluginPackageResult, error) {
			return updatePluginPackage(entry, sourcePath, guard)
		},
		func(result pluginPackageResult, err error) {
			defer state.end(entry.ID)
			// The guard acquired the gate on either path it reached, so the
			// release belongs here once, on the UI thread.
			release := acquired
			if err != nil {
				state.setError(entry.ID, "pluginsUpdateFailed", uiErrorSummary(err, text("errorUnknown"), 240))
				if release {
					s.plugins.endPluginRestart(result.PluginID)
				}
				s.scanPlugins(false)
				if s.window != nil {
					s.window.Invalidate()
				}
				return
			}
			if release {
				s.destroyPluginWebViews(result.PluginID)
				s.invalidatePluginRenderers(result.PluginID)
				s.plugins.endPluginRestart(result.PluginID)
			}
			s.finishPluginPackageChange(result, "pluginsUpdated", name)
		},
	)
}

// uninstallPluginPackageEntry follows the lifecycle contract: marker false and
// removal from the persistent enable set come first, then the folder moves to
// Trash recoverably under the target restart gate — the worker is stopped
// before the move, so no generation starts against a folder that is leaving —
// and the ordinary scan publishes the registry. plugin-data, plugin-settings
// and automation history are never touched, so a reinstall returns with the
// user's data.
//
// A failed removal must not cost the user their enable choice, so the previous
// state is captured first and restored on any error: the marker goes false
// before the move only as the pre-flight that revokes contributions, and the
// persistent disabled set is cleared only once the folder has actually left.
func (s *appState) uninstallPluginPackageEntry(entry pluginRegistryEntry) {
	state := pluginPackagesState(s)
	if !state.begin(entry.ID) {
		return
	}
	state.clearError(entry.ID)
	name := fallback(pluginLocalizedEntry(entry).Name, entry.ID)
	configDir := ""
	wasDisabled := false
	if s.plugins != nil {
		configDir = s.plugins.configDir
		wasDisabled = s.plugins.isPluginDisabled(entry.ID)
	}
	// Best effort: a damaged enable-state file must not block the uninstall.
	_ = writePluginRuntimeStateMarker(configDir, entry.ID, false)
	s.destroyPluginWebViews(entry.ID)
	s.clearPluginHiddenSince(entry.ID)
	acquired := false
	guard := func(pluginID string) error {
		if err := s.pluginPackageCommitStop(pluginID); err != nil {
			return err
		}
		acquired = true
		return nil
	}
	s.pluginInstallRunAsync(
		func() (pluginPackageResult, error) {
			return uninstallPluginPackage(entry, guard)
		},
		func(result pluginPackageResult, err error) {
			defer state.end(entry.ID)
			if acquired {
				s.plugins.endPluginRestart(entry.ID)
			}
			if err != nil {
				// Restore the choice the user had made, so a plugin that was
				// disabled comes back disabled and one that was enabled keeps
				// its marker at true while its folder is still in place.
				_ = writePluginRuntimeStateMarker(configDir, entry.ID, !wasDisabled)
				if s.plugins != nil && wasDisabled {
					_ = s.plugins.setPluginDisabledState(entry.ID, true)
				}
				state.setError(entry.ID, "pluginsUninstallFailed",
					uiErrorSummary(err, text("errorUnknown"), 240))
				s.scanPlugins(false)
				if s.window != nil {
					s.window.Invalidate()
				}
				return
			}
			// The folder is gone: only now does the plugin leave the
			// persistent enable set.
			if s.plugins != nil {
				_ = s.plugins.setPluginDisabledState(entry.ID, false)
			}
			s.scanPlugins(false)
			if s.window != nil {
				s.window.Invalidate()
			}
			notice := pluginUninstalledNotice(name, result)
			// A folder in the Trash does not take the extension out of the
			// running Pi process: for a loaded non-cooperative extension the
			// code keeps contributing until Pi refreshes, so the notice says so.
			if extension, loaded := s.piLoadedExtensions[entry.ID]; loaded && !extension.cooperative {
				notice = appendPiUnloadHint(notice)
			}
			s.showNotice(notice)
		},
	)
}

// appendPiUnloadHint joins the existing unload hint to a completed action
// notice so the refresh path stays as worded elsewhere in the app.
func appendPiUnloadHint(notice string) string {
	return strings.TrimSpace(notice) + " " + text("pluginsPiUnloadRefreshHint")
}

// pluginUninstalledNotice names the retained folder only when this app chose
// its path. When the system Trash took the folder it reports no name, so the
// notice says so rather than showing an empty path.
func pluginUninstalledNotice(name string, result pluginPackageResult) string {
	if result.RetainedPath == "" {
		return textArgs("pluginsUninstalledInTrash", map[string]any{"name": name})
	}
	return textArgs("pluginsUninstalled", map[string]any{
		"name": name,
		"path": uiPathLabel(result.RetainedPath),
	})
}

// finishPluginPackageChange publishes an installed or updated folder: fresh
// runtime generation, the enable marker at the plugin's current choice, then the
// ordinary scan, which stops only this plugin's worker when its files changed
// and raises the Pi refresh prompt only for a changed agent extension.
func (s *appState) finishPluginPackageChange(
	result pluginPackageResult,
	noticeKey, name string,
) {
	if s.plugins == nil {
		s.scanPlugins(true)
		return
	}
	enabled := !s.plugins.isPluginDisabled(result.PluginID)
	if err := pluginPackageRuntimeReady(s.plugins.configDir, result.PluginID, enabled); err != nil {
		s.showNotice(textArgs("pluginsRuntimeStateFailed", map[string]any{
			"name":  name,
			"error": uiErrorSummary(err, text("errorUnknown"), 200),
		}))
	}
	s.scanPlugins(false)
	notice := textArgs(noticeKey, map[string]any{"name": name})
	notice += pluginPackageRetention{
		Path:    result.RetainedPath,
		InTrash: result.RetainedInTrash,
	}.description()
	if result.SkippedLinks > 0 {
		notice += " " + textArgs("pluginsPackageSkippedLinks", map[string]any{
			"count": result.SkippedLinks,
		})
	}
	s.showNotice(notice)
	if s.window != nil {
		s.window.Invalidate()
	}
}

func pluginPackageFolderName(path string) string {
	base := filepath.Base(filepath.Clean(path))
	return strings.TrimSuffix(base, filepath.Ext(base))
}
