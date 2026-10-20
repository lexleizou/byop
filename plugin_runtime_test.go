package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const pluginHelperEnv = "BYOP_TEST_PLUGIN_HELPER"

// TestPluginHelperProcess is not a real test. It acts as a native plugin when
// the runtime tests launch the test binary through a small wrapper script.
func TestPluginHelperProcess(t *testing.T) {
	if os.Getenv(pluginHelperEnv) != "1" {
		return
	}
	logPath := os.Getenv("BYOP_TEST_PLUGIN_LOG")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     string          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			continue
		}
		appendPluginHelperLog(logPath, request.Method+" "+string(request.Params))
		response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
		switch request.Method {
		case "initialize":
			response["result"] = map[string]any{"name": "helper"}
		case "activate", "deactivate":
			response["result"] = map[string]any{}
		case "invokeCommand":
			response["result"] = map[string]any{"message": "Hello from native plugin"}
		default:
			response["error"] = map[string]any{"message": "unknown method"}
		}
		data, _ := json.Marshal(response)
		fmt.Println(string(data))
	}
	appendPluginHelperLog(logPath, "exit")
	os.Exit(0)
}

func appendPluginHelperLog(path, line string) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	fmt.Fprintln(file, line)
}

func writeNativeHelperPlugin(t *testing.T, events []string, lazy bool) (pluginRegistryEntry, string) {
	t.Helper()
	directory := t.TempDir()
	logPath := filepath.Join(directory, "calls.log")
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("test executable: %v", err)
	}
	script := fmt.Sprintf("#!/bin/sh\nexport %s=1\nexport BYOP_TEST_PLUGIN_LOG=%q\nexec %q -test.run='^TestPluginHelperProcess$'\n",
		pluginHelperEnv, logPath, executable)
	if err := os.MkdirAll(filepath.Join(directory, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "bin", "plugin"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := &pluginManifest{
		SchemaVersion: 1,
		ID:            "example.hello-native",
		Name:          "Hello Native",
		Version:       "0.1.0",
		Runtime:       "native",
		Entry:         "bin/plugin",
		Compatibility: pluginCompatibility{Host: ">=0.4.1 <1.0.0", PluginAPI: "1"},
		Contributes: pluginContributions{Commands: []pluginCommandContribution{
			{ID: "hello.say", Title: "Say hello"},
			{ID: "hello.other", Title: "Other"},
		}},
		Activation: pluginActivation{Events: events, Lazy: lazy},
	}
	if err := validatePluginManifest(manifest); err != nil {
		t.Fatalf("fixture manifest invalid: %v", err)
	}
	return pluginRegistryEntry{
		ID:              manifest.ID,
		Name:            manifest.Name,
		SourceDirectory: directory,
		ManifestPath:    filepath.Join(directory, pluginManifestFilename),
		Manifest:        manifest,
		Status:          pluginRegistered,
	}, logPath
}

func readPluginHelperLog(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read helper log: %v", err)
	}
	var methods []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		methods = append(methods, strings.SplitN(line, " ", 2)[0])
	}
	return methods
}

func TestPluginHostLazyActivationCommandAndDeactivate(t *testing.T) {
	entry, logPath := writeNativeHelperPlugin(t, []string{"onCommand:hello.say"}, false)
	host := newPluginHost(pluginHostVersion, nil)

	if state := host.status(entry.ID).State; state != pluginStateInactive {
		t.Fatalf("initial state = %s, want inactive", state)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("plugin started before first command: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := host.invokeCommand(ctx, entry, "hello.say", map[string]any{})
	if err != nil {
		t.Fatalf("invoke command: %v", err)
	}
	if got := pluginResultSummary(result); got != "Hello from native plugin" {
		t.Fatalf("result = %q", got)
	}
	status := host.status(entry.ID)
	if status.State != pluginStateActive || status.PID == 0 || status.Invocations != 1 ||
		status.ActivationReason != "onCommand:hello.say" {
		t.Fatalf("unexpected active status: %#v", status)
	}
	if _, err := host.invokeCommand(ctx, entry, "hello.say", nil); err != nil {
		t.Fatalf("second invoke: %v", err)
	}
	if host.status(entry.ID).PID != status.PID {
		t.Fatal("second command restarted the plugin process")
	}

	if err := host.deactivate(entry.ID); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if state := host.status(entry.ID).State; state != pluginStateStopped || host.activeCount() != 0 {
		t.Fatalf("after deactivate state = %s active = %d", state, host.activeCount())
	}
	want := []string{"initialize", "activate", "invokeCommand", "invokeCommand", "deactivate", "exit"}
	if got := readPluginHelperLog(t, logPath); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("lifecycle = %v, want %v", got, want)
	}
}

func TestPluginHostRejectsUndeclaredAndInactiveCommands(t *testing.T) {
	entry, logPath := writeNativeHelperPlugin(t, []string{"onCommand:hello.say"}, false)
	host := newPluginHost(pluginHostVersion, nil)
	ctx := context.Background()

	if _, err := host.invokeCommand(ctx, entry, "hello.missing", nil); err == nil ||
		!strings.Contains(err.Error(), "not declared") {
		t.Fatalf("undeclared command error = %v", err)
	}
	if _, err := host.invokeCommand(ctx, entry, "hello.other", nil); err == nil ||
		!strings.Contains(err.Error(), "activation event") {
		t.Fatalf("non-activation command error = %v", err)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("plugin process started for rejected commands: %v", err)
	}
}

func TestPluginHostRejectsUnsupportedOrUnsafeEntries(t *testing.T) {
	host := newPluginHost(pluginHostVersion, nil)
	ctx := context.Background()

	nodeEntry, _ := writeNativeHelperPlugin(t, nil, true)
	nodeEntry.Manifest.Runtime = "node"
	if _, err := host.invokeCommand(ctx, nodeEntry, "hello.say", nil); err == nil ||
		!strings.Contains(err.Error(), "not supported") {
		t.Fatalf("node runtime error = %v", err)
	}

	plain, _ := writeNativeHelperPlugin(t, nil, true)
	if err := os.Chmod(filepath.Join(plain.SourceDirectory, "bin", "plugin"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := host.invokeCommand(ctx, plain, "hello.say", nil); err == nil ||
		!strings.Contains(err.Error(), "not executable") {
		t.Fatalf("non-executable error = %v", err)
	}
	if host.status(plain.ID).State != pluginStateFailed {
		t.Fatalf("state = %s, want failed", host.status(plain.ID).State)
	}

	escape, _ := writeNativeHelperPlugin(t, nil, true)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(escape.SourceDirectory, "bin", "plugin")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := host.invokeCommand(ctx, escape, "hello.say", nil); err == nil ||
		!strings.Contains(err.Error(), "outside the plugin folder") {
		t.Fatalf("symlink escape error = %v", err)
	}
}

func TestPluginHostStopAllEndsActiveProcesses(t *testing.T) {
	entry, logPath := writeNativeHelperPlugin(t, nil, true)
	host := newPluginHost(pluginHostVersion, nil)
	if _, err := host.invokeCommand(context.Background(), entry, "hello.other", nil); err != nil {
		t.Fatalf("lazy invoke: %v", err)
	}
	host.stopAll()
	if host.activeCount() != 0 || host.status(entry.ID).State != pluginStateStopped {
		t.Fatalf("stopAll left active=%d state=%s", host.activeCount(), host.status(entry.ID).State)
	}
	got := readPluginHelperLog(t, logPath)
	if got[len(got)-1] != "exit" {
		t.Fatalf("plugin did not exit: %v", got)
	}
}
