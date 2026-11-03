package main

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

// contextForPluginTests bounds one command round-trip for the worker tests.
func contextForPluginTests(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// pluginPackagesTestEnv isolates every package test: a managed root, a config
// directory and its own retention directory, so the real ~/.byop is never
// modified, no real plugin is uninstalled, and nothing is moved into the
// user's real ~/.Trash. HOME is left alone: a test that needs the user root for
// a read-only path comparison takes it from os.UserHomeDir.
func pluginPackagesTestEnv(t *testing.T) (root, configDir string) {
	t.Helper()
	root = t.TempDir()
	configDir = t.TempDir()
	t.Setenv("BYOP_PLUGINS_DIR", root)
	t.Setenv("BYOP_CONFIG_DIR", configDir)
	t.Setenv(pluginPackageTrashDirEnv, t.TempDir())
	return root, configDir
}

// pluginPackagesTestTrash is the retention directory the test environment uses.
func pluginPackagesTestTrash(t *testing.T) string {
	t.Helper()
	return filepath.Clean(os.Getenv(pluginPackageTrashDirEnv))
}

// writePluginPackageFixture creates one plugin folder under directory. The
// folder name is deliberately independent of the manifest id, matching shipped
// plugins such as model-release-labels with id local.model-release-labels.
func writePluginPackageFixture(t *testing.T, directory, folderName, pluginID, version string) {
	t.Helper()
	manifest := validPluginManifestMap()
	manifest["id"] = pluginID
	manifest["version"] = version
	// Drop contributions whose files the fixture does not carry.
	manifest["contributes"] = map[string]any{
		"commands": []any{
			map[string]any{"id": "package.check", "title": "Check package"},
		},
	}
	writePluginManifest(t, directory, folderName, manifest)
	entryFile := filepath.Join(directory, folderName, "dist", "plugin.js")
	if err := os.MkdirAll(filepath.Dir(entryFile), 0o700); err != nil {
		t.Fatal(err)
	}
	marker := "version " + version + "\n"
	if err := os.WriteFile(entryFile, []byte(marker), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readPluginPackageMarker(t *testing.T, directory string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(directory, "dist", "plugin.js"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPluginPackagesTargetRootPrefersFirstExplicitRoot(t *testing.T) {
	root, _ := pluginPackagesTestEnv(t)
	if got, err := pluginPackageTargetRoot(); err != nil || got != filepath.Clean(root) {
		t.Fatalf("explicit target root = (%q, %v), want %q", got, err, root)
	}
	t.Setenv("BYOP_PLUGINS_DIR", "")
	got, err := pluginPackageTargetRoot()
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".byop", "plugins"); got != want {
		t.Fatalf("default target root = %q, want %q", got, want)
	}
	if strings.Contains(got, string([]byte{0})) {
		t.Fatal("target root is unusable")
	}
}

func TestPluginPackagesProjectRootIsNeverManaged(t *testing.T) {
	pluginPackagesTestEnv(t)
	project := t.TempDir()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	t.Setenv("BYOP_PLUGINS_DIR", "")
	projectPlugins := filepath.Join(project, ".byop", "plugins")
	if pluginPackageRootManaged(filepath.Join(projectPlugins, "owned.plugin")) {
		t.Fatal("a workspace plugin folder must not be a managed package target")
	}
	homePlugins := filepath.Join(home, ".byop", "plugins")
	if !pluginPackageRootManaged(filepath.Join(homePlugins, "installed.plugin")) {
		t.Fatal("the config default root must be managed")
	}
}

// The shipped model labels plugin proves the compatibility case: the folder name
// (model-release-labels) differs from the manifest id
// (local.model-release-labels), so an install of the same id must update the
// existing directory in place, never create root/<id> beside it.
func TestPluginPackagesInstallUpdatesExistingIDByFolderName(t *testing.T) {
	root, configDir := pluginPackagesTestEnv(t)
	installed := filepath.Join(root, "model-release-labels")
	writePluginPackageFixture(t, root, "model-release-labels", "local.model-release-labels", "0.1.0")

	// User data that must survive the update untouched.
	userData := filepath.Join(configDir, pluginDataDirectoryName, "local.model-release-labels")
	if err := os.MkdirAll(userData, 0o700); err != nil {
		t.Fatal(err)
	}
	dataFile := filepath.Join(userData, "runtime-generation.json")
	if err := os.WriteFile(dataFile, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}

	source := t.TempDir()
	writePluginPackageFixture(t, source, "new-package", "local.model-release-labels", "0.2.0")
	result, err := installPluginPackage(source, nil)
	if runtime.GOOS != "darwin" {
		if !errors.Is(err, errPluginSwapUnsupported) {
			t.Fatalf("non-macOS install over an existing folder = (%v, %v); want the swap-unsupported error",
				result, err)
		}
		if readPluginPackageMarker(t, installed) != "version 0.1.0\n" {
			t.Fatal("an unsupported swap still changed the installed folder")
		}
		return
	}
	if err != nil {
		t.Fatalf("install over the existing id: %v", err)
	}
	if result.TargetPath != filepath.Clean(installed) || !result.Replaced {
		t.Fatalf("install target = (%q, replaced=%v), want the existing folder %q",
			result.TargetPath, result.Replaced, installed)
	}
	if readPluginPackageMarker(t, installed) != "version 0.2.0\n" {
		t.Fatal("the installed folder did not receive the new package")
	}
	if _, err := os.Stat(filepath.Join(root, "local.model-release-labels")); !os.IsNotExist(err) {
		t.Fatalf("install created a duplicate-ID folder beside the existing one: %v", err)
	}
	// The previous version is retained recoverably under Trash, original intact.
	if result.RetainedPath == "" {
		t.Fatal("the previous version was not retained")
	}
	if !strings.HasPrefix(result.RetainedPath, pluginPackagesTestTrash(t)) {
		t.Fatalf("retained path %q is not under the user Trash %q",
			result.RetainedPath, pluginPackagesTestTrash(t))
	}
	if readPluginPackageMarker(t, result.RetainedPath) != "version 0.1.0\n" {
		t.Fatal("the retained original folder lost its content")
	}
	// User data survives, and staging left nothing inside the scanned root.
	data, err := os.ReadFile(dataFile)
	if err != nil || string(data) != "keep me" {
		t.Fatalf("user data changed by the update: %q, %v", data, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), pluginPackageStagingPrefix) {
			t.Fatalf("staging directory %q survived inside the plugin root", entry.Name())
		}
	}
}

func TestPluginPackagesInstallFreshFromWrappedZip(t *testing.T) {
	root, _ := pluginPackagesTestEnv(t)
	zipPath := filepath.Join(t.TempDir(), "fresh-plugin.zip")
	// A zip wrapped in one directory, the shape archive tools produce.
	writePluginZip(t, zipPath, map[string]string{
		"package/" + pluginManifestFilename: marshalPluginManifest(t, func() map[string]any {
			manifest := validPluginManifestMap()
			manifest["id"] = "example.fresh"
			manifest["contributes"] = map[string]any{
				"commands": []any{map[string]any{"id": "fresh.check", "title": "Check"}},
			}
			delete(manifest, "icon")
			return manifest
		}()),
		"package/dist/plugin.js": "hello",
	})
	result, err := installPluginPackage(zipPath, nil)
	if err != nil {
		t.Fatalf("install from zip: %v", err)
	}
	want := filepath.Join(filepath.Clean(root), "example.fresh")
	if result.TargetPath != want || result.Replaced {
		t.Fatalf("fresh install = (%q, replaced=%v), want %q", result.TargetPath, result.Replaced, want)
	}
	if _, err := os.Stat(filepath.Join(want, pluginManifestFilename)); err != nil {
		t.Fatalf("manifest missing after zip install: %v", err)
	}
	if strings.HasPrefix(want, pluginPackagesTestTrash(t)) {
		t.Fatal("fresh install landed in Trash")
	}
}

func TestPluginPackagesZipSkipsSymlinksAndRejectsEscapes(t *testing.T) {
	root, _ := pluginPackagesTestEnv(t)
	staging, err := createPluginPackageStaging(root)
	if err != nil {
		t.Fatal(err)
	}
	defer staging.remove()

	escapePath := filepath.Join(t.TempDir(), "escape.zip")
	writePluginZip(t, escapePath, map[string]string{"../escape.txt": "no"})
	if _, _, _, err := stagePluginPackage(escapePath, staging); err == nil ||
		!strings.Contains(err.Error(), "escapes") {
		t.Fatalf("escaping zip entry = %v, want an escape error", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("escaping entry wrote outside staging: %v", err)
	}

	linkPath := filepath.Join(t.TempDir(), "links.zip")
	writePluginZipWithSymlink(t, linkPath)
	skipped, err := extractPluginPackageZip(linkPath, filepath.Join(staging.Payload, "links"))
	if err != nil {
		t.Fatalf("extract zip with a symlink: %v", err)
	}
	if skipped != 1 {
		t.Fatalf("skipped links = %d, want 1", skipped)
	}
	if info, statErr := os.Lstat(filepath.Join(staging.Payload, "links", "icon-link")); statErr == nil &&
		info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("a symlink was created inside staging")
	}
}

func TestPluginPackagesUpdateKeepsFolderAndRejectsOtherID(t *testing.T) {
	root, _ := pluginPackagesTestEnv(t)
	writePluginPackageFixture(t, root, "odd-folder-name", "example.odd", "0.1.0")
	registry := scanPluginRegistryForRuntime()
	if registry.registeredCount() != 1 {
		t.Fatalf("fixture registered %d plugins", registry.registeredCount())
	}
	entry := registry.Entries[0]
	if filepath.Base(entry.SourceDirectory) != "odd-folder-name" {
		t.Fatalf("fixture folder = %q", entry.SourceDirectory)
	}

	// A package for a different id must not replace this folder.
	other := t.TempDir()
	writePluginPackageFixture(t, other, "package", "example.other", "1.0.0")
	if _, err := updatePluginPackage(entry, other, nil); err == nil ||
		!strings.Contains(err.Error(), "does not match") {
		t.Fatalf("update with a foreign id = %v, want a mismatch error", err)
	}
	if readPluginPackageMarker(t, entry.SourceDirectory) != "version 0.1.0\n" {
		t.Fatal("the rejected update still touched the folder")
	}

	matching := t.TempDir()
	writePluginPackageFixture(t, matching, "package", "example.odd", "0.2.0")
	result, err := updatePluginPackage(entry, matching, nil)
	if runtime.GOOS != "darwin" {
		if !errors.Is(err, errPluginSwapUnsupported) {
			t.Fatalf("non-macOS update = (%v, %v); want the swap-unsupported error", result, err)
		}
		return
	}
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if result.TargetPath != filepath.Clean(entry.SourceDirectory) || !result.Replaced {
		t.Fatalf("update target = %q, want the same folder in place", result.TargetPath)
	}
	if readPluginPackageMarker(t, entry.SourceDirectory) != "version 0.2.0\n" {
		t.Fatal("the folder did not receive the updated package")
	}
}

// Uninstall is a plain move, so it runs on every platform; data retention is the
// contract this test pins down.
func TestPluginPackagesUninstallMovesToTrashAndKeepsUserData(t *testing.T) {
	root, configDir := pluginPackagesTestEnv(t)
	writePluginPackageFixture(t, root, "leave-me", "example.leave", "0.1.0")
	registry := scanPluginRegistryForRuntime()
	if registry.registeredCount() != 1 {
		t.Fatalf("fixture registered %d plugins", registry.registeredCount())
	}
	entry := registry.Entries[0]

	userData := filepath.Join(configDir, pluginDataDirectoryName, "example.leave")
	if err := os.MkdirAll(userData, 0o700); err != nil {
		t.Fatal(err)
	}
	dataFile := filepath.Join(userData, "ledger.json")
	settingsFile := filepath.Join(configDir, "plugin-settings", "example.leave.json")
	if err := os.MkdirAll(filepath.Dir(settingsFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataFile, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsFile, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := uninstallPluginPackage(entry, nil)
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Stat(entry.SourceDirectory); !os.IsNotExist(err) {
		t.Fatalf("plugin folder survived the uninstall: %v", err)
	}
	if !strings.HasPrefix(result.RetainedPath, pluginPackagesTestTrash(t)) ||
		!strings.Contains(filepath.Base(result.RetainedPath), pluginPackageTrashPrefix+"example.leave") {
		t.Fatalf("retained path = %q, want a uniquely named entry under the user Trash",
			result.RetainedPath)
	}
	if readPluginPackageMarker(t, result.RetainedPath) != "version 0.1.0\n" {
		t.Fatal("the trashed folder lost its content")
	}
	for _, path := range []string{dataFile, settingsFile} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("uninstall removed user data %s: %v", path, err)
		}
	}
	if scanPluginRegistryForRuntime().registeredCount() != 0 {
		t.Fatal("the uninstalled plugin is still registered")
	}
}

func writePluginZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	output, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(output)
	for name, content := range files {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		if strings.HasSuffix(name, "/") {
			header.SetMode(os.ModeDir | 0o755)
		} else {
			header.SetMode(0o644)
		}
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}

func writePluginZipWithSymlink(t *testing.T, path string) {
	t.Helper()
	output, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(output)
	regular := &zip.FileHeader{Name: "regular.txt", Method: zip.Deflate}
	regular.SetMode(0o644)
	entry, err := writer.CreateHeader(regular)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	link := &zip.FileHeader{Name: "icon-link"}
	link.SetMode(os.ModeSymlink | 0o777)
	entry, err = writer.CreateHeader(link)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("regular.txt")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestPluginSwapMissingPathReportsErrno pins the failure diagnostics on the
// verified platform: a real renamex_np failure must surface the libc errno read
// through __error, not the third SyscallN return, which the scratch
// swap-return-probe showed is not errno on darwin.
func TestPluginSwapMissingPathReportsErrno(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the swap helper is verified on macOS only")
	}
	missing := filepath.Join(t.TempDir(), "missing-dir")
	other := t.TempDir()
	err := pluginSwapDirectories(missing, other)
	if err == nil {
		t.Fatal("swapping a missing folder succeeded; want an error")
	}
	if !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("swap error = %v, want errors.Is(..., ENOENT)", err)
	}
	// The existing side stayed untouched.
	if _, statErr := os.Stat(other); statErr != nil {
		t.Fatalf("the failed swap disturbed the existing folder: %v", statErr)
	}
}

func TestPluginPackagesFolderIDUsesManifestIDOverFolderName(t *testing.T) {
	entry := pluginRegistryEntry{
		ID:              "local.model-release-labels",
		SourceDirectory: "/tmp/plugins/model-release-labels",
		Manifest:        &pluginManifest{ID: "local.model-release-labels"},
	}
	if got := pluginPackageFolderID(entry); got != "local.model-release-labels" {
		t.Fatalf("folder id = %q, want the manifest id", got)
	}
	entry.Manifest = nil
	if got := pluginPackageFolderID(entry); got != "model-release-labels" {
		t.Fatalf("fallback folder id = %q, want the folder name", got)
	}
}

// writeNativePluginPackageFixture builds one installable plugin folder inside
// directory from the native helper: a real executable entry and a manifest with
// the given id and version. codeTag is written into the entry script, so two
// packages can carry identical manifests over different code.
func writeNativePluginPackageFixture(t *testing.T, directory, pluginID, version, codeTag string) string {
	t.Helper()
	source, _ := writeNativeHelperPlugin(t, []string{"onCommand:hello.say"}, false)
	pluginDirectory := filepath.Join(directory, pluginID)
	skipped, err := copyPluginPackageTree(source.SourceDirectory, pluginDirectory)
	if err != nil || skipped != 0 {
		t.Fatalf("copy helper fixture: skipped=%d err=%v", skipped, err)
	}
	manifest := validPluginManifestMap()
	manifest["id"] = pluginID
	manifest["version"] = version
	manifest["runtime"] = "native"
	manifest["entry"] = "bin/plugin"
	manifest["icon"] = "plugins"
	// A command id unique to the plugin, so two fixtures never collide as
	// duplicate contributions in one registry.
	commandID := pluginID + ".say"
	manifest["contributes"] = map[string]any{
		"commands": []any{map[string]any{"id": commandID, "title": "Say hello"}},
	}
	manifest["activation"] = map[string]any{
		"events": []any{"onCommand:" + commandID},
		"lazy":   false,
	}
	writePluginManifest(t, directory, pluginID, manifest)
	entryScript := filepath.Join(pluginDirectory, "bin", "plugin")
	body, err := os.ReadFile(entryScript)
	if err != nil {
		t.Fatal(err)
	}
	body = []byte(strings.ReplaceAll(string(body), "\nexec ", "\n# code "+codeTag+"\nexec "))
	if err := os.WriteFile(entryScript, body, 0o700); err != nil {
		t.Fatal(err)
	}
	return pluginDirectory
}

// A package update is a deliberate stop of one target: the manifest and version
// can be identical while the code changed, and the incremental scan compares
// manifests only, so without the guard the old worker and its cached view would
// keep running after the exchange.
func TestPluginPackageUpdateReplacesWorkerOnSameVersion(t *testing.T) {
	pluginPackagesTestEnv(t)
	directory := t.TempDir()
	target := writeNativePluginPackageFixture(t, directory, "example.pkg-native", "1.0.0", "one")
	result, err := installPluginPackage(target, nil)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if result.Replaced {
		t.Fatalf("fresh install reported replaced=%v", result.Replaced)
	}
	// A second plugin must keep its worker through the update.
	other := writeNativePluginPackageFixture(t, directory, "example.pkg-other", "1.0.0", "keep")
	if _, err := installPluginPackage(other, nil); err != nil {
		t.Fatalf("install other: %v", err)
	}

	host := newTestPluginHost(t)
	state := &appState{plugins: host}
	registry := scanPluginRegistryForRuntime()
	if registry.registeredCount() != 2 {
		t.Fatalf("installed registry registered %d plugins, want 2", registry.registeredCount())
	}
	targetEntry, otherEntry := pluginRegistryEntry{}, pluginRegistryEntry{}
	for _, entry := range registry.Entries {
		switch entry.ID {
		case "example.pkg-native":
			targetEntry = entry
		case "example.pkg-other":
			otherEntry = entry
		}
	}
	ctx := contextForPluginTests(t)
	if _, err := host.invokeCommand(ctx, targetEntry, "example.pkg-native.say", nil); err != nil {
		t.Fatalf("start target worker: %v", err)
	}
	if _, err := host.invokeCommand(ctx, otherEntry, "example.pkg-other.say", nil); err != nil {
		t.Fatalf("start other worker: %v", err)
	}
	original := host.process(targetEntry.ID)
	otherProcess := host.process(otherEntry.ID)
	if original == nil || otherProcess == nil {
		t.Fatalf("workers missing before update: target=%v other=%v", original, otherProcess)
	}
	originalPID := original.cmd.Process.Pid
	otherPID := otherProcess.cmd.Process.Pid

	// Same id, same version, different code: the scan plan sees no change.
	updatedSource := writeNativePluginPackageFixture(
		t, t.TempDir(), "example.pkg-native", "1.0.0", "two")
	result, err = updatePluginPackage(targetEntry, updatedSource, state.pluginPackageCommitStop)
	if runtime.GOOS != "darwin" {
		if !errors.Is(err, errPluginSwapUnsupported) {
			t.Fatalf("non-macOS update = %v, want the swap-unsupported error", err)
		}
		host.endPluginRestart(targetEntry.ID)
		return
	}
	if err != nil {
		t.Fatalf("same-version update: %v", err)
	}
	if !result.Replaced {
		t.Fatal("the update did not exchange the folder")
	}
	// The guard stopped exactly the target generation.
	if current := host.process(targetEntry.ID); current != nil {
		t.Fatal("the old worker survived the guarded update")
	}
	if current := host.process(otherEntry.ID); current == nil ||
		current.cmd.Process.Pid != otherPID {
		t.Fatalf("the unrelated worker changed: %v", current)
	}
	host.endPluginRestart(targetEntry.ID)

	// The next use starts a fresh generation from the exchanged folder.
	if _, err := host.invokeCommand(ctx, targetEntry, "example.pkg-native.say", nil); err != nil {
		t.Fatalf("restart after update: %v", err)
	}
	replacement := host.process(targetEntry.ID)
	if replacement == nil {
		t.Fatal("no worker started after the update")
	}
	if replacement.cmd.Process.Pid == originalPID {
		t.Fatalf("replacement reused the replaced pid %d", originalPID)
	}
	if replacement.cmd.Process.Pid == otherPID {
		t.Fatal("replacement collided with the unrelated worker")
	}
}

// The retention copy runs only when Trash sits on another volume, so the tree
// copy is exercised directly: symbolic links must arrive as links, or deleting
// the original after a skip-links copy would drop them.
func TestPluginPackageBackupCopyKeepsSymlinks(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "data.txt"), []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("data.txt", filepath.Join(source, "link.txt")); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "retained")
	if err := copyPluginPackageBackupTree(source, destination); err != nil {
		t.Fatalf("backup copy: %v", err)
	}
	info, err := os.Lstat(filepath.Join(destination, "link.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("retained entry is not a symlink: %v", info.Mode())
	}
	target, err := os.Readlink(filepath.Join(destination, "link.txt"))
	if err != nil || target != "data.txt" {
		t.Fatalf("retained link target = %q, err %v", target, err)
	}
	// The import copy keeps its existing no-follow behavior.
	imported := filepath.Join(t.TempDir(), "imported")
	skipped, err := copyPluginPackageTree(source, imported)
	if err != nil || skipped != 1 {
		t.Fatalf("import copy = (skipped %d, err %v), want the link skipped", skipped, err)
	}
	if _, err := os.Lstat(filepath.Join(imported, "link.txt")); !os.IsNotExist(err) {
		t.Fatalf("import copy created the symlink: %v", err)
	}
}

// K1: a failed uninstall must not throw away the user's enable choice. The
// Trash destination is unusable, so the move fails after the pre-flight marker
// went false, and the done closure has to restore the state the user had set.
func TestPluginPackagesFailedUninstallKeepsDisabledChoice(t *testing.T) {
	root, configDir := pluginPackagesTestEnv(t)
	folder := filepath.Join(root, "stay-me")
	writePluginPackageFixture(t, root, "stay-me", "example.stay", "0.1.0")
	registry := scanPluginRegistryForRuntime()
	if registry.registeredCount() != 1 {
		t.Fatalf("fixture registered %d plugins", registry.registeredCount())
	}
	entry := registry.Entries[0]

	host := newPluginHost(pluginHostVersion, nil, pluginHostWorkspace(t.TempDir()))
	if err := host.setPluginDisabledState(entry.ID, true); err != nil {
		t.Fatalf("disable fixture: %v", err)
	}
	if err := writePluginRuntimeStateMarker(configDir, entry.ID, false); err != nil {
		t.Fatal(err)
	}
	// The retention destination is an ordinary file, so every removal under it
	// fails and the folder stays in place.
	unusable := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(unusable, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(pluginPackageTrashDirEnv, unusable)

	state := &appState{
		plugins:            host,
		pluginSettingForms: make(map[string]*pluginSettingForm),
	}
	t.Cleanup(func() { resetPluginPackagesForTest(state) })
	state.uninstallPluginPackageEntry(entry)

	if _, err := os.Stat(folder); err != nil {
		t.Fatalf("the folder left despite the failed uninstall: %v", err)
	}
	if uiState := pluginPackagesState(state); uiState.errKey != "pluginsUninstallFailed" {
		t.Fatalf("uninstall error key = %q, want the failed-uninstall notice", uiState.errKey)
	}
	if !host.isPluginDisabled(entry.ID) {
		t.Fatal("a failed uninstall re-enabled a plugin the user had disabled")
	}
	persisted, ok := host.disabled.loadChecked()
	if !ok {
		t.Fatal("the persistent disabled set became unreadable")
	}
	found := false
	for _, id := range persisted {
		if id == entry.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("disabled.json lost the plugin: %v", persisted)
	}
	if readPluginRuntimeStateEnabled(configDir, entry.ID) {
		t.Fatal("the runtime marker was left false after the failed uninstall")
	}
}

// K2: a Trash directory whose entries cannot be looked up must fail fast. The
// unique-name loop previously only stopped on ErrNotExist, so a permission
// error made it count names forever while the restart gate was held.
func TestPluginPackagesTrashNameLoopFailsOnLookupError(t *testing.T) {
	pluginPackagesTestEnv(t)
	sourceParent := t.TempDir()
	writePluginPackageFixture(t, sourceParent, "spin-me", "example.spin", "0.1.0")
	trash := filepath.Join(t.TempDir(), "unreadable")
	if err := os.MkdirAll(trash, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(trash, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(trash, 0o700) })

	done := make(chan error, 1)
	go func() {
		_, err := movePluginPackageDirectoryToTrash(
			filepath.Join(sourceParent, "spin-me"), trash, "example.spin")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("an unreadable Trash directory reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the Trash name probe kept running on a lookup error")
	}
	if _, err := os.Stat(filepath.Join(sourceParent, "spin-me")); err != nil {
		t.Fatalf("a failed removal moved the source folder: %v", err)
	}
}

// K3: with the working directory at the home folder, the user plugin root
// collides with the project root. The user root wins, so plugins installed in
// ~/.byop/plugins stay updatable and uninstallable.
func TestPluginPackagesUserRootManagedWhenCwdIsHome(t *testing.T) {
	pluginPackagesTestEnv(t)
	t.Setenv("BYOP_PLUGINS_DIR", "")
	userRoot, err := pluginPackageUserRoot()
	if err != nil {
		t.Fatal(err)
	}
	// The user root itself, read-only: nothing is created or removed there.
	home := filepath.Dir(filepath.Dir(userRoot))
	t.Chdir(home)
	if !pluginPackageRootManaged(filepath.Join(userRoot, "installed.plugin")) {
		t.Fatalf("the user root %q is not managed when the working directory is home", userRoot)
	}
	managed := false
	for _, root := range pluginPackageManagedRoots() {
		if root == userRoot {
			managed = true
		}
	}
	if !managed {
		t.Fatalf("managed roots %v do not include the user root", pluginPackageManagedRoots())
	}
}

// K4: Finder's Compress writes __MACOSX/ resource forks beside the plugin
// folder. Those entries must neither be extracted nor block the unwrap.
func TestPluginPackagesFinderZipInstalls(t *testing.T) {
	root, _ := pluginPackagesTestEnv(t)
	zipPath := filepath.Join(t.TempDir(), "finder-plugin.zip")
	manifestBody := marshalPluginManifest(t, func() map[string]any {
		manifest := validPluginManifestMap()
		manifest["id"] = "example.finder"
		manifest["contributes"] = map[string]any{
			"commands": []any{map[string]any{"id": "finder.check", "title": "Check"}},
		}
		delete(manifest, "icon")
		return manifest
	}())
	writePluginZip(t, zipPath, map[string]string{
		"finder-plugin/" + pluginManifestFilename: manifestBody,
		"finder-plugin/dist/plugin.js":            "hello",
		"__MACOSX/finder-plugin/._plugin.json":    "resource fork",
		"__MACOSX/._finder-plugin":                "resource fork",
		"finder-plugin/.DS_Store":                 "junk",
	})
	result, err := installPluginPackage(zipPath, nil)
	if err != nil {
		t.Fatalf("install a Finder-made zip: %v", err)
	}
	want := filepath.Join(filepath.Clean(root), "example.finder")
	if result.TargetPath != want {
		t.Fatalf("install target = %q, want %q", result.TargetPath, want)
	}
	if _, err := os.Stat(filepath.Join(want, pluginManifestFilename)); err != nil {
		t.Fatalf("manifest missing after the Finder zip install: %v", err)
	}
	if _, err := os.Stat(filepath.Join(want, pluginPackageMacOSXDirectory)); !os.IsNotExist(err) {
		t.Fatalf("__MACOSX was extracted into the plugin folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(want, ".DS_Store")); !os.IsNotExist(err) {
		t.Fatalf(".DS_Store was extracted into the plugin folder: %v", err)
	}
}

// K9: when the swap succeeds but the previous version cannot be kept, staging
// must leave it where the error says it is. The deferred staging.remove sees the
// cleared Root through the pointer, so the old folder and its user files stay.
func TestPluginPackagesRetentionFailureKeepsPreviousVersion(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the directory swap is verified on macOS only")
	}
	root, _ := pluginPackagesTestEnv(t)
	writePluginPackageFixture(t, root, "keep-me", "example.keep", "0.1.0")
	sentinel := filepath.Join(root, "keep-me", "old-custom-sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	writePluginPackageFixture(t, source, "keep-me", "example.keep", "0.2.0")
	// A regular file cannot hold the retained folder, so the retention step fails
	// after the swap has already gone through.
	unusable := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(unusable, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(pluginPackageTrashDirEnv, unusable)

	registry := scanPluginRegistryForRuntime()
	entry := registry.Entries[0]
	result, err := updatePluginPackage(entry, source, nil)
	if err == nil {
		t.Fatal("retention into an unusable destination reported success")
	}
	if !result.Replaced {
		t.Fatal("the swap did not happen before the retention failure")
	}
	if readPluginPackageMarker(t, filepath.Join(root, "keep-me")) != "version 0.2.0\n" {
		t.Fatal("the new package is not live after the retention failure")
	}
	if result.RetainedPath == "" || result.RetainedInTrash {
		t.Fatalf("retention report = (%q, inTrash=%v), want the path that still holds the folder",
			result.RetainedPath, result.RetainedInTrash)
	}
	data, readErr := os.ReadFile(filepath.Join(result.RetainedPath, "old-custom-sentinel.txt"))
	if readErr != nil || string(data) != "keep me" {
		t.Fatalf("the previous version or its custom file was deleted: %q, %v", data, readErr)
	}
	if readPluginPackageMarker(t, result.RetainedPath) != "version 0.1.0\n" {
		t.Fatal("the retained folder does not hold the previous version")
	}
}

// The picker no longer blocks the UI thread. Without a window, the same
// synchronous seam proves cancelling leaves nothing selected and a completed
// cancel can open the picker again.
func TestPluginPackagesInstallPickerCancelKeepsStateUsable(t *testing.T) {
	pluginPackagesTestEnv(t)
	state := &appState{}
	t.Cleanup(func() { resetPluginPackagesForTest(state) })
	var calls int
	installPluginPackagesTestDependencies(state, func(
		*mygo.Window, pluginPackagePickerMode,
	) (string, error) {
		calls++
		return "", nil
	})
	state.pickAndInstallPluginPackage(pluginPackagePickFolder)
	state.pickAndInstallPluginPackage(pluginPackagePickZip)
	uiState := pluginPackagesState(state)
	if calls != 2 {
		t.Fatalf("picker calls = %d, want both clicks to open after cancel", calls)
	}
	if _, key := uiState.errorFor(""); key != "" {
		t.Fatalf("cancelled picker set %q", key)
	}
}
