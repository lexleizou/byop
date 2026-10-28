package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestP17GenerationMarkerIsAtomicAndMonotonic(t *testing.T) {
	configDir := t.TempDir()
	first, err := writePluginRuntimeGeneration(configDir, "example.hot-reload")
	if err != nil {
		t.Fatalf("first generation: %v", err)
	}
	second, err := writePluginRuntimeGeneration(configDir, "example.hot-reload")
	if err != nil {
		t.Fatalf("second generation: %v", err)
	}
	if second.Generation <= first.Generation {
		t.Fatalf("generation did not advance: %d -> %d", first.Generation, second.Generation)
	}
	path := filepath.Join(
		configDir,
		pluginDataDirectoryName,
		"example.hot-reload",
		pluginRuntimeGenerationFilename,
	)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generation marker: %v", err)
	}
	var marker pluginRuntimeGenerationMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		t.Fatalf("decode generation marker: %v", err)
	}
	if marker.PluginID != "example.hot-reload" || marker.Generation != second.Generation {
		t.Fatalf("marker = %#v", marker)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != pluginRuntimeGenerationFilename {
			t.Fatalf("atomic write left temporary data: %s", entry.Name())
		}
	}
}

func TestP17StablePiSignatureTracksExtensionAndProxyOnly(t *testing.T) {
	root := t.TempDir()
	manifest := validPluginManifestMap()
	contributions := manifest["contributes"].(map[string]any)
	contributions["agentExtension"] = "pi-extension.mjs"
	contributions["agentProxy"] = "mcp-proxy.mjs"
	manifestPath := writePluginManifest(t, root, "example", manifest)
	pluginDirectory := filepath.Dir(manifestPath)
	writePluginWebViewFixture(t, pluginDirectory)
	if err := os.MkdirAll(filepath.Join(pluginDirectory, "dist"), 0o700); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		"dist/plugin.js":      "export default {};\n",
		"pi-extension.mjs":    "export default function extension() {}\n",
		"mcp-proxy.mjs":       "process.stdin.resume();\n",
		"src/mcp-backend.mjs": "export const backend = 1;\n",
	} {
		filename := filepath.Join(pluginDirectory, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	before := scanPluginRegistry([]pluginRootSpec{{Path: root}})
	if before.registeredCount() != 1 {
		t.Fatalf("fixture registry = %#v", before)
	}
	beforeSignature := registeredPiPluginSignature(before)

	backendPath := filepath.Join(pluginDirectory, "src", "mcp-backend.mjs")
	if err := os.WriteFile(backendPath, []byte("export const backend = 2;\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	afterBackend := scanPluginRegistry([]pluginRootSpec{{Path: root}})
	if got := registeredPiPluginSignature(afterBackend); got != beforeSignature {
		t.Fatal("ordinary backend change incorrectly required a Pi refresh")
	}

	extensionPath := filepath.Join(pluginDirectory, "pi-extension.mjs")
	if err := os.WriteFile(extensionPath,
		[]byte("export default function extension() { return 2; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(extensionPath, future, future); err != nil {
		t.Fatal(err)
	}
	afterExtension := scanPluginRegistry([]pluginRootSpec{{Path: root}})
	if got := registeredPiPluginSignature(afterExtension); got == beforeSignature {
		t.Fatal("stable extension change did not require a Pi refresh")
	}
}

func TestP17RegistryRestartReplacesOnlyTargetEntry(t *testing.T) {
	original := pluginRegistry{Entries: []pluginRegistryEntry{
		{ID: "example.one", SourceDirectory: "/plugins/one", Version: "1.0.0"},
		{ID: "example.two", SourceDirectory: "/plugins/two", Version: "2.0.0"},
	}}
	replacement := pluginRegistryEntry{
		ID: "example.one", SourceDirectory: "/plugins/one", Version: "1.1.0",
		Status: pluginRegistered,
	}
	next, err := replacePluginRegistryEntry(original, "/plugins/one", replacement)
	if err != nil {
		t.Fatal(err)
	}
	if next.Entries[0].Version != "1.1.0" {
		t.Fatalf("target was not replaced: %#v", next.Entries[0])
	}
	if !reflect.DeepEqual(next.Entries[1], original.Entries[1]) {
		t.Fatalf("unrelated entry changed: %#v", next.Entries[1])
	}
	if !reflect.DeepEqual(original.Entries[0].Version, "1.0.0") {
		t.Fatal("registry replacement mutated the source snapshot")
	}
}

func TestP17PluginRestartStopsOnlyTargetProcess(t *testing.T) {
	first, _ := writeNativeHelperPlugin(t, nil, true)
	second, _ := writeNativeHelperPlugin(t, nil, true)
	second.ID = "example.hello-native.second"
	second.Name = "Second"
	second.Manifest.ID = second.ID
	host := newTestPluginHost(t)
	defer host.stopAll()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := host.invokeCommand(ctx, first, "hello.other", nil); err != nil {
		t.Fatalf("start first: %v", err)
	}
	if _, err := host.invokeCommand(ctx, second, "hello.other", nil); err != nil {
		t.Fatalf("start second: %v", err)
	}
	firstPID := host.status(first.ID).PID
	secondPID := host.status(second.ID).PID
	if firstPID == 0 || secondPID == 0 || firstPID == secondPID {
		t.Fatalf("unexpected process IDs: %d %d", firstPID, secondPID)
	}

	if err := host.restartPluginProcess(first.ID); err != nil {
		t.Fatalf("restart target: %v", err)
	}
	if got := host.status(first.ID).State; got != pluginStateStopped {
		t.Fatalf("target state = %s", got)
	}
	if got := host.status(second.ID); got.State != pluginStateActive || got.PID != secondPID {
		t.Fatalf("unrelated process changed: %#v", got)
	}
	if host.activeCount() != 1 {
		t.Fatalf("active process count = %d", host.activeCount())
	}
	host.endPluginRestart(first.ID)
	if _, err := host.invokeCommand(ctx, first, "hello.other", nil); err != nil {
		t.Fatalf("restart target process: %v", err)
	}
	if got := host.status(first.ID).PID; got == 0 || got == firstPID {
		t.Fatalf("target process did not restart: old=%d new=%d", firstPID, got)
	}
	if got := host.status(second.ID).PID; got != secondPID {
		t.Fatalf("unrelated process PID changed: %d -> %d", secondPID, got)
	}
}

func TestP17RestartBlocksStaleWorkerAndKeepsNewStatus(t *testing.T) {
	t.Setenv(pluginHelperDeactivateDelayEnv, "200ms")
	entry, _ := writeNativeHelperPlugin(t, nil, true)
	host := newTestPluginHost(t)
	defer host.stopAll()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := host.invokeCommand(ctx, entry, "hello.other", nil); err != nil {
		t.Fatalf("start plugin: %v", err)
	}
	oldPID := host.status(entry.ID).PID

	restarted := make(chan error, 1)
	go func() {
		restarted <- host.restartPluginProcess(entry.ID)
	}()
	deadline := time.Now().Add(time.Second)
	for host.status(entry.ID).State != pluginStateStopping && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := host.status(entry.ID).State; got != pluginStateStopping {
		t.Fatalf("restart state = %s", got)
	}
	if _, err := host.ensureActive(ctx, entry, "onCommand:hello.other"); !errors.Is(err, errPluginRestarting) {
		t.Fatalf("stale worker start error = %v", err)
	}

	host.endPluginRestart(entry.ID)
	process, err := host.ensureActive(ctx, entry, "onCommand:hello.other")
	if err != nil {
		t.Fatalf("start replacement worker: %v", err)
	}
	if err := <-restarted; err != nil {
		t.Fatalf("restart old worker: %v", err)
	}
	status := host.status(entry.ID)
	if status.State != pluginStateActive || status.PID != process.pid() || status.PID == oldPID {
		t.Fatalf("new process status was overwritten: old=%d process=%d status=%#v",
			oldPID, process.pid(), status)
	}
}
