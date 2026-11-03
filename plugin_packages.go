package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

const (
	// pluginPackageStagingPrefix names the staging directory created next to a
	// plugin root, so a candidate is never visible to a registry scan.
	pluginPackageStagingPrefix = ".byop-plugin-staging-"
	// pluginPackageTrashPrefix names a removed plugin folder inside ~/.Trash.
	pluginPackageTrashPrefix  = "byop-plugin-"
	pluginPackageZipExtension = ".zip"
	// pluginPackageTrashDirEnv points the retention destination somewhere else,
	// for focused tests and tooling. Unset in the app.
	pluginPackageTrashDirEnv = "BYOP_PLUGIN_TRASH_DIR"
	// pluginPackageMacOSXDirectory names the resource-fork folder a Finder
	// "Compress" zip carries beside the plugin folder itself.
	pluginPackageMacOSXDirectory = "__MACOSX"
	// pluginPackageTrashNameAttempts caps how many numbered names one removal
	// tries before it gives up, so a crowded Trash cannot hold the operation.
	pluginPackageTrashNameAttempts = 100
)

// errPluginSwapUnsupported marks a platform where replacing an existing plugin
// folder cannot be done in one step. Callers must not fall back to
// remove-then-rename, because the MCP proxy may respawn the backend inside that
// gap and see a missing folder.
var errPluginSwapUnsupported = errors.New(
	"replacing a running plugin folder is not supported on this platform")

// pluginPackageResult reports one finished install, update or uninstall.
type pluginPackageResult struct {
	// PluginID, Name and Version come from the staged or removed manifest.
	PluginID string
	Name     string
	Version  string
	// TargetPath is where the plugin folder lives now, empty after uninstall.
	TargetPath string
	// Replaced is true when an existing folder was exchanged rather than added.
	Replaced bool
	// RetainedPath is where the previous or removed folder was kept, when that
	// place is a path this app chose.
	RetainedPath string
	// RetainedInTrash is true when the system Trash took the folder. The system
	// does not report the name it chose, so no path is invented for it.
	RetainedInTrash bool
	// SkippedLinks counts symbolic links that were not copied, because the
	// copier follows neither links outside the package nor link targets.
	SkippedLinks int
}

// pluginPackageRetention says where a kept folder now sits. Path names a folder
// this app placed, InTrash the system Trash, which gives no path back.
type pluginPackageRetention struct {
	Path    string
	InTrash bool
}

// description is the trailing part of a package notice that names the kept
// folder. An empty result adds nothing, so a caller can append it blindly.
func (retention pluginPackageRetention) description() string {
	if retention.Path != "" {
		return " " + textArgs("pluginsPackageKept", map[string]any{
			"path": uiPathLabel(retention.Path),
		})
	}
	if retention.InTrash {
		return " " + text("pluginsPackageKeptInTrash")
	}
	return ""
}

// pluginPackageTargetRoot is where a new plugin is installed: the first explicit
// BYOP_PLUGINS_DIR root when the user configured one, otherwise the config
// default ~/.byop/plugins. The project-level <cwd>/.byop/plugins root is never a
// target, so installing never writes inside a source tree.
func pluginPackageTargetRoot() (string, error) {
	for _, spec := range defaultPluginRoots() {
		if spec.Explicit {
			return spec.Path, nil
		}
	}
	return pluginPackageUserRoot()
}

// pluginPackageUserRoot is the user-level plugin root ~/.byop/plugins. It is
// named separately because it outranks the workspace classification: when the
// process runs with the home folder as its working directory, the two paths are
// the same folder, and the user's own root still has to be manageable.
func pluginPackageUserRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	root := filepath.Join(home, ".byop", "plugins")
	if absolute, absErr := filepath.Abs(filepath.Clean(root)); absErr == nil {
		return absolute, nil
	}
	return filepath.Clean(root), nil
}

// pluginPackageManagedRoots are the roots a package may be written into:
// explicit BYOP_PLUGINS_DIR roots when configured, otherwise the config default
// ~/.byop/plugins. The project-level <cwd>/.byop/plugins root is deliberately
// excluded, so package work never writes inside a source tree.
func pluginPackageManagedRoots() []string {
	userRoot, userRootErr := pluginPackageUserRoot()
	var roots []string
	for _, spec := range defaultPluginRoots() {
		cleaned := filepath.Clean(spec.Path)
		isUserRoot := userRootErr != nil || cleaned == userRoot
		if !spec.Explicit && !isUserRoot && pluginPackageProjectRoot(cleaned) {
			continue
		}
		roots = append(roots, cleaned)
	}
	return roots
}

func pluginPackageProjectRoot(path string) bool {
	cwd, err := os.Getwd()
	if err != nil {
		return false
	}
	project := filepath.Join(cwd, ".byop", "plugins")
	if absolute, absErr := filepath.Abs(project); absErr == nil {
		project = absolute
	}
	return filepath.Clean(path) == filepath.Clean(project)
}

// pluginPackageRootManaged reports whether one plugin folder sits directly
// inside a managed root, which is what makes it updatable or uninstallable as a
// package.
func pluginPackageRootManaged(sourceDirectory string) bool {
	cleaned := filepath.Clean(sourceDirectory)
	for _, root := range pluginPackageManagedRoots() {
		if filepath.Dir(cleaned) == root {
			return true
		}
	}
	return false
}

// pluginPackageStaging is a candidate directory outside every scanned root and
// on the same filesystem as that root, so the final rename or swap cannot fail
// with a cross-device error and a duplicate plugin ID never appears mid-install.
type pluginPackageStaging struct {
	Root    string
	Payload string
}

func createPluginPackageStaging(root string) (pluginPackageStaging, error) {
	parent := filepath.Dir(filepath.Clean(root))
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return pluginPackageStaging{}, fmt.Errorf("create plugin root parent: %w", err)
	}
	stagingRoot := filepath.Join(parent, fmt.Sprintf(
		"%s%d-%d", pluginPackageStagingPrefix, time.Now().UnixNano(), os.Getpid()))
	if err := os.MkdirAll(stagingRoot, 0o700); err != nil {
		return pluginPackageStaging{}, fmt.Errorf("create staging directory: %w", err)
	}
	payload := filepath.Join(stagingRoot, "payload")
	if err := os.MkdirAll(payload, 0o700); err != nil {
		os.RemoveAll(stagingRoot)
		return pluginPackageStaging{}, fmt.Errorf("create staging payload: %w", err)
	}
	return pluginPackageStaging{Root: stagingRoot, Payload: payload}, nil
}

// remove deletes the staging tree. The receiver is a pointer because callers
// defer staging.remove(): a deferred value-receiver method would keep the copy
// taken at the defer statement, and the Root="" that replacePluginPackageDirectory
// sets after a successful swap would never reach it, deleting the previous
// version that the error message claims was kept.
func (staging *pluginPackageStaging) remove() {
	if strings.TrimSpace(staging.Root) == "" {
		return
	}
	_ = os.RemoveAll(staging.Root)
}

// stagePluginPackage copies a folder or extracts a zip into staging and runs the
// same manifest and path checks the registry scan applies. A package wrapped in
// one directory — the shape most archive tools and registries produce — is
// unwrapped by descending into that single directory. The returned directory is
// the one that holds the manifest, which is what the final rename or swap moves;
// it can be deeper than staging.Payload after an unwrap.
func stagePluginPackage(sourcePath string, staging pluginPackageStaging) (
	pluginRegistryEntry, string, int, error,
) {
	info, err := os.Lstat(sourcePath)
	if err != nil {
		return pluginRegistryEntry{}, "", 0, fmt.Errorf("read package: %w", err)
	}
	skipped := 0
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return pluginRegistryEntry{}, "", 0, errors.New("choose a plugin folder or a .zip package")
	case info.IsDir():
		skipped, err = copyPluginPackageTree(sourcePath, staging.Payload)
	case strings.EqualFold(filepath.Ext(sourcePath), pluginPackageZipExtension):
		skipped, err = extractPluginPackageZip(sourcePath, staging.Payload)
	default:
		return pluginRegistryEntry{}, "", 0, errors.New("choose a plugin folder or a .zip package")
	}
	if err != nil {
		return pluginRegistryEntry{}, "", skipped, err
	}
	payload := staging.Payload
	if _, statErr := os.Stat(filepath.Join(payload, pluginManifestFilename)); errors.Is(statErr, os.ErrNotExist) {
		children, readErr := os.ReadDir(payload)
		if usable, wrapped := pluginPackageOnlyChild(children, readErr); wrapped {
			payload = filepath.Join(payload, usable)
		}
	}
	entry, err := validatePluginPackageDirectory(payload)
	return entry, payload, skipped, err
}

// validatePluginPackageDirectory applies the existing manifest and path rules to
// pluginPackageOnlyChild reports the single plugin folder of a staged payload
// once macOS metadata is ignored, which is what lets a wrapped folder — the
// shape archives and Finder's Compress produce — be descended into.
func pluginPackageOnlyChild(children []os.DirEntry, readErr error) (string, bool) {
	if readErr != nil {
		return "", false
	}
	name := ""
	for _, child := range children {
		if child.Name() == pluginPackageMacOSXDirectory || child.Name() == ".DS_Store" ||
			strings.HasPrefix(child.Name(), "._") {
			continue
		}
		if !child.IsDir() || name != "" {
			return "", false
		}
		name = child.Name()
	}
	return name, name != ""
}

// validatePluginPackageDirectory applies the existing manifest and path rules to
// one staged folder and returns the registry entry a scan would produce.
func validatePluginPackageDirectory(directory string) (pluginRegistryEntry, error) {
	cleaned := filepath.Clean(directory)
	manifestPath := filepath.Join(cleaned, pluginManifestFilename)
	if _, err := os.Stat(manifestPath); err != nil {
		return pluginRegistryEntry{}, fmt.Errorf("package has no %s: %w", pluginManifestFilename, err)
	}
	manifest, err := loadPluginManifest(manifestPath)
	if err != nil {
		return pluginRegistryEntry{}, err
	}
	entry := pluginRegistryEntry{
		ID:                manifest.ID,
		Name:              manifest.Name,
		Version:           manifest.Version,
		Description:       manifest.Description,
		SourceDirectory:   cleaned,
		ManifestPath:      manifestPath,
		Manifest:          manifest,
		ContributionCount: manifest.contributionCount(),
		Status:            pluginRegistered,
	}
	if message := validatePluginAgentExtension(entry); message != "" {
		return entry, errors.New(message)
	}
	if message := validatePluginWebViewEntries(entry); message != "" {
		return entry, errors.New(message)
	}
	if message := pluginCompatibilityError(manifest); message != "" {
		return entry, errors.New(message)
	}
	return entry, nil
}

// copyPluginPackageTree copies regular files and directories. Symbolic links and
// other irregular entries are skipped rather than followed, so a package can
// never bring in content from outside itself and an executable entry keeps its
// permission bits.
func copyPluginPackageTree(source, destination string) (int, error) {
	skipped := 0
	entries, err := os.ReadDir(source)
	if err != nil {
		return skipped, fmt.Errorf("read plugin folder: %w", err)
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return skipped, fmt.Errorf("create plugin folder: %w", err)
	}
	for _, directoryEntry := range entries {
		sourcePath := filepath.Join(source, directoryEntry.Name())
		destinationPath := filepath.Join(destination, directoryEntry.Name())
		info, err := directoryEntry.Info()
		if err != nil {
			return skipped, fmt.Errorf("read %s: %w", directoryEntry.Name(), err)
		}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			skipped++
		case info.IsDir():
			childSkipped, err := copyPluginPackageTree(sourcePath, destinationPath)
			skipped += childSkipped
			if err != nil {
				return skipped, err
			}
		case info.Mode().IsRegular():
			if err := copyPluginPackageFile(sourcePath, destinationPath, info.Mode().Perm()); err != nil {
				return skipped, err
			}
		default:
			skipped++
		}
	}
	return skipped, nil
}

func copyPluginPackageFile(source, destination string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return fmt.Errorf("create parent folder: %w", err)
	}
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open %s: %w", filepath.Base(source), err)
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", filepath.Base(destination), err)
	}
	copyErr := func() error {
		defer output.Close()
		if _, err := io.Copy(output, input); err != nil {
			return fmt.Errorf("write %s: %w", filepath.Base(destination), err)
		}
		if err := output.Chmod(mode); err != nil {
			return fmt.Errorf("set mode for %s: %w", filepath.Base(destination), err)
		}
		return nil
	}()
	if copyErr != nil {
		_ = output.Close()
		_ = os.Remove(destination)
	}
	return copyErr
}

// extractPluginPackageZip unpacks regular entries only. Absolute entry names,
// parent segments and links are rejected or skipped, so a malicious archive
// cannot write outside the staging payload.
func extractPluginPackageZip(source, destination string) (int, error) {
	reader, err := zip.OpenReader(source)
	if err != nil {
		return 0, fmt.Errorf("open package: %w", err)
	}
	defer reader.Close()
	skipped := 0
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return skipped, fmt.Errorf("create plugin folder: %w", err)
	}
	for _, file := range reader.File {
		relative, err := pluginPackageZipPath(file.Name)
		if err != nil {
			return skipped, err
		}
		if pluginPackageZipResourceFork(relative) {
			// A Finder-made zip keeps AppleDouble metadata beside every entry and
			// a __MACOSX folder beside the plugin folder. Neither belongs to the
			// plugin, so they are not extracted and never counted.
			continue
		}
		mode := file.Mode()
		switch {
		case mode&fs.ModeSymlink != 0:
			skipped++
			continue
		case mode.IsDir():
			if err := os.MkdirAll(filepath.Join(destination, relative), 0o700); err != nil {
				return skipped, fmt.Errorf("create %s: %w", relative, err)
			}
			continue
		case !mode.IsRegular() && mode.Perm() == 0:
			// Archives written without Unix metadata report no mode at all.
			mode = 0o644
		case !mode.IsRegular():
			skipped++
			continue
		}
		input, err := file.Open()
		if err != nil {
			return skipped, fmt.Errorf("open %s: %w", relative, err)
		}
		writeErr := func() error {
			defer input.Close()
			target := filepath.Join(destination, relative)
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return fmt.Errorf("create parent folder: %w", err)
			}
			output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return fmt.Errorf("create %s: %w", relative, err)
			}
			perm := mode.Perm()
			if perm == 0 {
				perm = 0o644
			}
			copyErr := func() error {
				defer output.Close()
				if _, err := io.Copy(output, input); err != nil {
					return fmt.Errorf("write %s: %w", relative, err)
				}
				return output.Chmod(perm)
			}()
			if copyErr != nil {
				_ = output.Close()
				_ = os.Remove(target)
				return copyErr
			}
			return nil
		}()
		if writeErr != nil {
			return skipped, writeErr
		}
	}
	return skipped, nil
}

// pluginPackageZipResourceFork reports the macOS metadata entries a Finder
// "Compress" archive adds: everything under __MACOSX/ and every ._AppleDouble
// file. They are skipped so such a zip installs like any other.
func pluginPackageZipResourceFork(relative string) bool {
	segments := strings.Split(filepath.ToSlash(relative), "/")
	if segments[0] == pluginPackageMacOSXDirectory {
		return true
	}
	name := segments[len(segments)-1]
	return strings.HasPrefix(name, "._") || name == ".DS_Store"
}

func pluginPackageZipPath(name string) (string, error) {
	cleaned := path.Clean(strings.TrimSpace(filepath.ToSlash(name)))
	if cleaned == "." || cleaned == "/" || cleaned == "" {
		return "", fmt.Errorf("package entry %q has no usable name", name)
	}
	if path.IsAbs(cleaned) || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", fmt.Errorf("package entry %q escapes the plugin folder", name)
	}
	for _, segment := range strings.Split(cleaned, "/") {
		if segment == ".." {
			return "", fmt.Errorf("package entry %q escapes the plugin folder", name)
		}
	}
	return filepath.FromSlash(cleaned), nil
}

// pluginPackageCommitGuard runs after the package is validated and staged and
// immediately before the atomic exchange of an existing folder. The caller uses
// it to stop the target worker under the target restart gate, so no generation
// keeps running from the old files and none can start during the exchange.
// Fresh installs run without a guard.
type pluginPackageCommitGuard func(pluginID string) error

// installPluginPackage stages a folder or zip and places it in the target root.
// A missing folder is a plain rename; an existing ID is exchanged atomically so
// user data under plugin-data, plugin-settings and automation history is never
// touched and the running proxy can always resolve the folder.
func installPluginPackage(sourcePath string, guard pluginPackageCommitGuard) (pluginPackageResult, error) {
	targetRoot, err := pluginPackageTargetRoot()
	if err != nil {
		return pluginPackageResult{}, err
	}
	staging, err := createPluginPackageStaging(targetRoot)
	if err != nil {
		return pluginPackageResult{}, err
	}
	defer staging.remove()
	entry, stagedDirectory, skipped, err := stagePluginPackage(sourcePath, staging)
	if err != nil {
		return pluginPackageResult{}, err
	}
	result := pluginPackageResult{
		PluginID:     entry.ID,
		Name:         entry.Name,
		Version:      entry.Version,
		SkippedLinks: skipped,
	}
	target, err := pluginPackageInstallTarget(entry.ID, targetRoot)
	if err != nil {
		return result, err
	}
	result.TargetPath = target
	if cleaned, pathErr := filepath.Abs(filepath.Clean(sourcePath)); pathErr == nil &&
		cleaned == target {
		return result, errors.New("this plugin is already in the plugin folder")
	}
	if err := pluginPackageTargetNameFree(target, entry.ID); err != nil {
		return result, err
	}
	info, statErr := os.Lstat(target)
	switch {
	case statErr != nil && !errors.Is(statErr, os.ErrNotExist):
		return result, fmt.Errorf("read plugin folder: %w", statErr)
	case statErr != nil:
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return result, fmt.Errorf("create plugin folder: %w", err)
		}
		if err := os.Rename(stagedDirectory, target); err != nil {
			return result, fmt.Errorf("install plugin folder: %w", err)
		}
		return result, nil
	case !info.IsDir() || info.Mode()&fs.ModeSymlink != 0:
		return result, fmt.Errorf("plugin folder is not a directory: %s", target)
	}
	result.Replaced = true
	if guard != nil {
		if err := guard(entry.ID); err != nil {
			return result, err
		}
	}
	retention, err := replacePluginPackageDirectory(stagedDirectory, target, &staging)
	result.RetainedPath, result.RetainedInTrash = retention.Path, retention.InTrash
	return result, err
}

// pluginPackageTargetNameFree refuses a fresh install whose folder name is taken
// by a different plugin id, which would otherwise be swapped away unnoticed.
func pluginPackageTargetNameFree(target, pluginID string) error {
	for _, entry := range scanPluginRegistryForRuntime().Entries {
		if entry.SourceDirectory == "" {
			continue
		}
		if filepath.Clean(entry.SourceDirectory) == filepath.Clean(target) &&
			entry.ID != pluginID {
			return fmt.Errorf("folder %s already holds plugin id %q",
				filepath.Base(target), entry.ID)
		}
	}
	return nil
}

// replacePluginPackageDirectory exchanges the staged candidate with the folder a
// plugin is loaded from in one supported step, then keeps the previous folder
// recoverably. A reclaimed MCP proxy may spawn the backend at any moment, so
// there is never a window where the target path is missing. When the exchange
// succeeded but the folder could not be kept elsewhere, staging.remove is
// skipped — the deferred call sees the cleared Root through the pointer — so the
// previous version stays exactly where the returned path says it does.
func replacePluginPackageDirectory(candidate, target string, staging *pluginPackageStaging) (
	pluginPackageRetention, error,
) {
	if err := pluginSwapDirectories(candidate, target); err != nil {
		return pluginPackageRetention{}, err
	}
	retention, err := retainPluginPackageFolder(
		candidate, filepath.Base(filepath.Clean(target)))
	if err != nil {
		// The new folder is live; keep the previous one where it is instead of
		// deleting the only copy.
		staging.Root = ""
		// The folder really is still sitting at the candidate path, so the
		// report names it rather than claiming nothing was kept.
		return pluginPackageRetention{Path: candidate}, fmt.Errorf(
			"plugin folder was replaced but the previous version could not be kept: %w (kept at %s)",
			err, candidate)
	}
	return retention, nil
}

// updatePluginPackage swaps a new package into the folder one entry is loaded
// from, in place, keeping the folder path stable.
func updatePluginPackage(entry pluginRegistryEntry, sourcePath string,
	guard pluginPackageCommitGuard,
) (pluginPackageResult, error) {
	sourceDirectory := filepath.Clean(entry.SourceDirectory)
	if sourceDirectory == "" || !pluginPackageRootManaged(sourceDirectory) {
		return pluginPackageResult{}, errors.New("plugin folder is not inside a plugin root")
	}
	root := filepath.Dir(sourceDirectory)
	staging, err := createPluginPackageStaging(root)
	if err != nil {
		return pluginPackageResult{}, err
	}
	defer staging.remove()
	candidate, stagedDirectory, skipped, err := stagePluginPackage(sourcePath, staging)
	if err != nil {
		return pluginPackageResult{}, err
	}
	if candidate.ID != pluginPackageFolderID(entry) {
		return pluginPackageResult{}, fmt.Errorf(
			"package id %q does not match the installed plugin %q", candidate.ID, pluginPackageFolderID(entry))
	}
	if guard != nil {
		if err := guard(candidate.ID); err != nil {
			return pluginPackageResult{
				PluginID:   candidate.ID,
				Name:       candidate.Name,
				Version:    candidate.Version,
				TargetPath: sourceDirectory,
			}, err
		}
	}
	retention, err := replacePluginPackageDirectory(stagedDirectory, sourceDirectory, &staging)
	result := pluginPackageResult{
		PluginID:        candidate.ID,
		Name:            candidate.Name,
		Version:         candidate.Version,
		TargetPath:      sourceDirectory,
		Replaced:        true,
		RetainedPath:    retention.Path,
		RetainedInTrash: retention.InTrash,
		SkippedLinks:    skipped,
	}
	return result, err
}

// pluginPackageFolderID is the id a folder is known by: the manifest id when the
// manifest is readable, otherwise the folder name the scan fell back to.
func pluginPackageFolderID(entry pluginRegistryEntry) string {
	if entry.Manifest != nil {
		return entry.ID
	}
	return filepath.Base(filepath.Clean(entry.SourceDirectory))
}

// uninstallPluginPackage moves one plugin folder to the user's Trash under a
// unique name. Plugin data, settings and automation history stay in place, so a
// reinstall of the same id comes back with its user data.
func uninstallPluginPackage(entry pluginRegistryEntry,
	guard pluginPackageCommitGuard,
) (pluginPackageResult, error) {
	sourceDirectory := filepath.Clean(entry.SourceDirectory)
	if sourceDirectory == "" || !pluginPackageRootManaged(sourceDirectory) {
		return pluginPackageResult{}, errors.New("plugin folder is not inside a plugin root")
	}
	if guard != nil {
		if err := guard(pluginPackageFolderID(entry)); err != nil {
			return pluginPackageResult{
				PluginID: pluginPackageFolderID(entry),
				Name:     entry.Name,
				Version:  entry.Version,
			}, err
		}
	}
	retention, err := retainPluginPackageFolder(sourceDirectory, pluginPackageFolderID(entry))
	return pluginPackageResult{
		PluginID:        pluginPackageFolderID(entry),
		Name:            entry.Name,
		Version:         entry.Version,
		RetainedPath:    retention.Path,
		RetainedInTrash: retention.InTrash,
	}, err
}

// pluginPackageTrashDestination resolves where a removed folder is kept: the
// fixture directory when BYOP_PLUGIN_TRASH_DIR names one, so focused tests never
// touch a real Trash, and "" when the system Trash should handle it.
func pluginPackageTrashDestination() (string, error) {
	configured := strings.TrimSpace(os.Getenv(pluginPackageTrashDirEnv))
	if configured == "" {
		return "", nil
	}
	trash := filepath.Clean(configured)
	if err := os.MkdirAll(trash, 0o700); err != nil {
		return "", fmt.Errorf("create Trash directory: %w", err)
	}
	return trash, nil
}

// retainPluginPackageFolder keeps one folder recoverable and reports where it
// was kept, or that the system Trash took it. An error means the folder is still
// exactly where it was, which is what the caller reports to the user.
func retainPluginPackageFolder(sourceDirectory, identifier string) (pluginPackageRetention, error) {
	trash, err := pluginPackageTrashDestination()
	if err != nil {
		return pluginPackageRetention{}, err
	}
	return retainPluginPackageFolderTo(sourceDirectory, identifier, trash)
}

// retainPluginPackageFolderTo keeps one folder in the destination resolved by
// pluginPackageTrashDestination, where an empty destination means the system
// Trash: the platform Trash API is asked directly, because it knows the unique
// name it chose and needs no name probing here.
func retainPluginPackageFolderTo(sourceDirectory, identifier, trash string) (pluginPackageRetention, error) {
	if trash == "" {
		if mygo.App.IsReady() {
			if err := mygo.Shell.TrashItem(sourceDirectory); err != nil {
				return pluginPackageRetention{}, fmt.Errorf("move plugin folder to Trash: %w", err)
			}
			// The system keeps the folder; it does not report the path it chose,
			// so the result says only that the folder is in the Trash.
			return pluginPackageRetention{InTrash: true}, nil
		}
		// Headless tooling and tests without a running app keep the folder in the
		// user's Trash folder, which is the same place the API would put it.
		home, err := os.UserHomeDir()
		if err != nil {
			return pluginPackageRetention{}, fmt.Errorf("resolve home directory: %w", err)
		}
		trash = filepath.Join(home, ".Trash")
		if err := os.MkdirAll(trash, 0o700); err != nil {
			return pluginPackageRetention{}, fmt.Errorf("create Trash directory: %w", err)
		}
	}
	retained, err := movePluginPackageDirectoryToTrash(sourceDirectory, trash, identifier)
	if err != nil {
		return pluginPackageRetention{}, err
	}
	return pluginPackageRetention{Path: retained}, nil
}

// movePluginPackageDirectoryToTrash keeps a removed folder recoverable. A rename
// inside one volume is one step; when Trash sits on another volume the folder is
// copied first and only then removed, so nothing disappears silently. The
// cross-volume copy carries symbolic links over as links, because skipping them
// and then deleting the original would silently drop content the user could
// otherwise restore from Trash.
func movePluginPackageDirectoryToTrash(sourceDirectory, trash, identifier string) (string, error) {
	base := fmt.Sprintf("%s%s-%s", pluginPackageTrashPrefix, identifier,
		time.Now().Format("20060102-150405"))
	target := filepath.Join(trash, base)
	for attempt := 1; attempt <= pluginPackageTrashNameAttempts; attempt++ {
		_, err := os.Lstat(target)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			// A name that cannot be checked is not a free name: returning beats
			// spinning forever on a permission or I/O error.
			return "", fmt.Errorf("check Trash folder name: %w", err)
		}
		target = filepath.Join(trash, fmt.Sprintf("%s-%d", base, attempt))
		if attempt == pluginPackageTrashNameAttempts {
			return "", fmt.Errorf("Trash folder is holding too many copies named %s", base)
		}
	}
	if err := os.Rename(sourceDirectory, target); err == nil {
		return target, nil
	}
	if err := copyPluginPackageBackupTree(sourceDirectory, target); err != nil {
		return "", err
	}
	if err := os.RemoveAll(sourceDirectory); err != nil {
		return "", fmt.Errorf("remove plugin folder after copy to Trash: %w", err)
	}
	return target, nil
}

// copyPluginPackageBackupTree copies a tree for retention, recreating symbolic
// links as links instead of following or dropping them. Imports keep using
// copyPluginPackageTree, which still refuses to let a package reach outside
// itself.
func copyPluginPackageBackupTree(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("read %s: %w", filepath.Base(source), err)
	}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		linkTarget, err := os.Readlink(source)
		if err != nil {
			return fmt.Errorf("read link %s: %w", filepath.Base(source), err)
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return fmt.Errorf("create parent folder: %w", err)
		}
		if err := os.Symlink(linkTarget, destination); err != nil {
			return fmt.Errorf("copy link %s: %w", filepath.Base(destination), err)
		}
		return nil
	case info.IsDir():
		if err := os.MkdirAll(destination, 0o700); err != nil {
			return fmt.Errorf("create plugin folder: %w", err)
		}
		children, err := os.ReadDir(source)
		if err != nil {
			return fmt.Errorf("read plugin folder: %w", err)
		}
		for _, child := range children {
			if err := copyPluginPackageBackupTree(
				filepath.Join(source, child.Name()), filepath.Join(destination, child.Name())); err != nil {
				return err
			}
		}
		return nil
	case info.Mode().IsRegular():
		return copyPluginPackageFile(source, destination, info.Mode().Perm())
	default:
		// A socket or device cannot be restored by copying; skipping it keeps the
		// rest of the folder recoverable.
		return nil
	}
}

// pluginPackageInstallTarget resolves where an install lands for one manifest
// id. A plugin folder name never has to equal the manifest id — the shipped
// model labels plugin lives in model-release-labels with id
// local.model-release-labels — so an existing id is resolved to the directory
// the registry actually loads inside a managed install root, and the update
// swaps that directory in place. Installing beside it under root/<id> would
// register the same id twice. An id that only exists in a workspace-owned root
// (<cwd>/.byop/plugins) is left alone: packages are never written into a project
// tree automatically.
func pluginPackageInstallTarget(pluginID, targetRoot string) (string, error) {
	for _, entry := range scanPluginRegistryForRuntime().Entries {
		if entry.ID != pluginID || entry.SourceDirectory == "" ||
			entry.Status == pluginDuplicateID {
			continue
		}
		if pluginPackageRootManaged(entry.SourceDirectory) {
			return filepath.Clean(entry.SourceDirectory), nil
		}
		return "", fmt.Errorf(
			"plugin id %q belongs to the workspace plugin folder %s; install it from there instead",
			pluginID, entry.SourceDirectory)
	}
	return filepath.Join(targetRoot, pluginID), nil
}

// pluginPackageRuntimeReady records the deliberate enable choice for a folder
// that is now in place: a fresh generation for the proxy and the marker set to
// the plugin's current enable state. A failure is reported to the caller, which
// still keeps the installed folder.
func pluginPackageRuntimeReady(configDir, pluginID string, enabled bool) error {
	if _, err := writePluginRuntimeGeneration(configDir, pluginID); err != nil {
		return err
	}
	return writePluginRuntimeStateMarker(configDir, pluginID, enabled)
}
