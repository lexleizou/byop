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
	encoder := json.NewEncoder(os.Stdout)
	var pendingInvokeID string
	const themeRequestID = "helper-theme-1"
	for scanner.Scan() {
		var request struct {
			ID     string          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			continue
		}
		if request.Method == "" {
			if request.ID == themeRequestID && pendingInvokeID != "" {
				var theme struct {
					Mode string `json:"mode"`
				}
				if err := json.Unmarshal(request.Result, &theme); err != nil || theme.Mode == "" {
					response := map[string]any{
						"jsonrpc": "2.0",
						"id":      pendingInvokeID,
						"error":   map[string]any{"message": "host.getTheme returned no mode"},
					}
					_ = encoder.Encode(response)
				} else {
					response := map[string]any{
						"jsonrpc": "2.0",
						"id":      pendingInvokeID,
						"result":  map[string]any{"message": "Hello from native plugin"},
					}
					_ = encoder.Encode(response)
				}
				pendingInvokeID = ""
			}
			continue
		}
		appendPluginHelperLog(logPath, request.Method)
		if request.ID == "" {
			continue
		}
		response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
		switch request.Method {
		case "initialize":
			response["result"] = map[string]any{"name": "helper"}
		case "activate", "deactivate":
			response["result"] = map[string]any{}
		case "invokeCommand":
			pendingInvokeID = request.ID
			if err := encoder.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      themeRequestID,
				"method":  "host.getTheme",
				"params":  map[string]any{},
			}); err != nil {
				os.Exit(1)
			}
			continue
		case "renderView":
			response["result"] = map[string]any{
				"title": "Helper view",
				"blocks": []any{
					map[string]any{"type": "heading", "text": "Hello from helper"},
					map[string]any{"type": "text", "text": "Rendered through JSON-RPC."},
				},
			}
		default:
			response["error"] = map[string]any{"message": "unknown method"}
		}
		_ = encoder.Encode(response)
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
		Compatibility: pluginCompatibility{Host: ">=0.4.2 <1.0.0", PluginAPI: "1"},
		Contributes: pluginContributions{Commands: []pluginCommandContribution{
			{ID: "hello.say", Title: "Say hello"},
			{ID: "hello.other", Title: "Other"},
		}, Views: []pluginViewContribution{
			{ID: "hello.main", Title: "Hello", Location: "main", View: "native"},
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
	host := newTestPluginHost(t)

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
		log, _ := os.ReadFile(logPath)
		t.Fatalf("invoke command: %v; helper log:\n%s", err, log)
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
	view, err := host.renderView(ctx, entry, "hello.main")
	if err != nil {
		t.Fatalf("render view: %v", err)
	}
	if view.Title != "Helper view" || len(view.Blocks) != 2 {
		t.Fatalf("view = %#v", view)
	}
	if got := host.status(entry.ID).HostCalls; got != 2 {
		t.Fatalf("host calls = %d, want 2", got)
	}

	if err := host.deactivate(entry.ID); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if state := host.status(entry.ID).State; state != pluginStateStopped || host.activeCount() != 0 {
		t.Fatalf("after deactivate state = %s active = %d", state, host.activeCount())
	}
	want := []string{"initialize", "activate", "invokeCommand", "invokeCommand", "renderView", "deactivate", "exit"}
	if got := readPluginHelperLog(t, logPath); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("lifecycle = %v, want %v", got, want)
	}
}

func TestPluginHostRejectsUndeclaredAndInactiveCommands(t *testing.T) {
	entry, logPath := writeNativeHelperPlugin(t, []string{"onCommand:hello.say"}, false)
	host := newTestPluginHost(t)
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

func TestPluginHostRejectsInvalidOrUnsafeEntries(t *testing.T) {
	host := newTestPluginHost(t)
	ctx := context.Background()

	nodeEntry, _ := writeNativeHelperPlugin(t, nil, true)
	nodeEntry.Manifest.Runtime = "node"
	if _, err := host.invokeCommand(ctx, nodeEntry, "hello.say", nil); err == nil ||
		!strings.Contains(err.Error(), "Node entry must end") {
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
	host := newTestPluginHost(t)
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
