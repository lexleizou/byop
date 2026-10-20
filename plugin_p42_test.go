package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func newTestPluginHost(t *testing.T) *pluginHost {
	t.Helper()
	return newPluginHost(
		pluginHostVersion,
		nil,
		pluginHostWorkspace(t.TempDir()),
		pluginHostSettings(newPluginSettingsStore(t.TempDir())),
		pluginHostTheme(func() bool { return false }),
	)
}

func TestPluginHostP42ManifestNavigationAndSettingsValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*pluginManifest)
		wantErr string
	}{
		{
			name: "navigation references undeclared view",
			mutate: func(manifest *pluginManifest) {
				manifest.Contributes.Navigation[0].View = "missing.view"
			},
			wantErr: "navigation view is not declared",
		},
		{
			name: "unsupported setting type",
			mutate: func(manifest *pluginManifest) {
				manifest.Contributes.Settings[0].Fields[0].Type = "password"
			},
			wantErr: "type must be string, boolean, number, or select",
		},
		{
			name: "select without options",
			mutate: func(manifest *pluginManifest) {
				manifest.Contributes.Settings[0].Fields[2].Options = nil
			},
			wantErr: "select options are required",
		},
		{
			name: "duplicate field key",
			mutate: func(manifest *pluginManifest) {
				manifest.Contributes.Settings[0].Fields[1].Key = "greeting"
			},
			wantErr: "setting field key is duplicated",
		},
		{
			name: "uppercase contribution id",
			mutate: func(manifest *pluginManifest) {
				manifest.Contributes.Commands[0].ID = "P42.run"
			},
			wantErr: "command contribution id is invalid",
		},
		{
			name: "wrong default type",
			mutate: func(manifest *pluginManifest) {
				manifest.Contributes.Settings[0].Fields[0].Default = json.RawMessage(`true`)
			},
			wantErr: "default must be a string",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := p42Manifest()
			if err := validatePluginManifest(manifest); err != nil {
				t.Fatalf("base manifest invalid: %v", err)
			}
			manifest = p42Manifest()
			test.mutate(manifest)
			err := validatePluginManifest(manifest)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("validation error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestPluginHostP42NodePermissionArgs(t *testing.T) {
	tests := []struct {
		name         string
		capabilities pluginCapabilities
		want         []string
	}{
		{
			name: "none",
			want: []string{
				"--permission",
				"--allow-fs-read=/plugins/example",
				"/plugins/example/plugin.mjs",
			},
		},
		{
			name:         "workspace read",
			capabilities: pluginCapabilities{Files: "read"},
			want: []string{
				"--permission",
				"--allow-fs-read=/plugins/example",
				"--allow-fs-read=/workspace",
				"/plugins/example/plugin.mjs",
			},
		},
		{
			name:         "workspace write",
			capabilities: pluginCapabilities{Files: "write"},
			want: []string{
				"--permission",
				"--allow-fs-read=/plugins/example",
				"--allow-fs-read=/workspace",
				"--allow-fs-write=/workspace",
				"/plugins/example/plugin.mjs",
			},
		},
		{
			name: "network and process",
			capabilities: pluginCapabilities{
				Network: true,
				Process: "execute",
			},
			want: []string{
				"--permission",
				"--allow-fs-read=/plugins/example",
				"--allow-net",
				"--allow-child-process",
				"/plugins/example/plugin.mjs",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := p42Manifest()
			manifest.Capabilities = test.capabilities
			got := nodePermissionArgs(
				manifest,
				"/plugins/example",
				"/workspace",
				"/plugins/example/plugin.mjs",
			)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("nodePermissionArgs = %v, want %v", got, test.want)
			}
		})
	}
}

func TestPluginHostP42SettingsStore(t *testing.T) {
	manifest := p42Manifest()
	store := newPluginSettingsStore(t.TempDir())

	defaults, err := store.load(manifest)
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	wantDefaults := map[string]any{
		"greeting":    "Hello",
		"showDetails": true,
		"tone":        "calm",
	}
	if !reflect.DeepEqual(defaults, wantDefaults) {
		t.Fatalf("defaults = %#v, want %#v", defaults, wantDefaults)
	}

	wantSaved := map[string]any{
		"greeting":    "Welcome",
		"showDetails": false,
		"tone":        "bright",
	}
	if _, err := store.save(manifest, wantSaved); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	info, err := os.Stat(store.filename(manifest.ID))
	if err != nil {
		t.Fatalf("stat settings: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("settings mode = %o, want 600", info.Mode().Perm())
	}
	loaded, err := store.load(manifest)
	if err != nil {
		t.Fatalf("round-trip load: %v", err)
	}
	if !reflect.DeepEqual(loaded, wantSaved) {
		t.Fatalf("round-trip settings = %#v, want %#v", loaded, wantSaved)
	}

	if err := os.WriteFile(store.filename(manifest.ID), []byte(`{
  "greeting": 42,
  "showDetails": "yes",
  "tone": "loud",
  "unknown": "drop"
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fallback, err := store.load(manifest)
	if err != nil {
		t.Fatalf("load invalid values: %v", err)
	}
	if !reflect.DeepEqual(fallback, wantDefaults) {
		t.Fatalf("invalid values = %#v, want defaults %#v", fallback, wantDefaults)
	}

	for name, content := range map[string]string{
		"invalid JSON":  `{"greeting":`,
		"not an object": `[]`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(store.filename(manifest.ID), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			corrupt, err := store.load(manifest)
			if err != nil {
				t.Fatalf("load corrupt settings: %v", err)
			}
			if !reflect.DeepEqual(corrupt, wantDefaults) {
				t.Fatalf("corrupt settings = %#v, want defaults %#v", corrupt, wantDefaults)
			}
			if got := store.warning(manifest.ID); got != pluginSettingsCorruptMessage {
				t.Fatalf("settings warning = %q", got)
			}
		})
	}
	if _, err := store.save(manifest, wantSaved); err != nil {
		t.Fatalf("save over corrupt settings: %v", err)
	}
	if got := store.warning(manifest.ID); got != "" {
		t.Fatalf("settings warning after save = %q", got)
	}
}

func TestPluginHostCallEnforcement(t *testing.T) {
	workspace := t.TempDir()
	outsideDirectory := t.TempDir()
	outside := filepath.Join(outsideDirectory, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("workspace"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "outside-link")); err != nil {
		t.Fatal(err)
	}

	manifest := p42Manifest()
	manifest.Capabilities = pluginCapabilities{}
	entry := pluginRegistryEntry{
		ID:       manifest.ID,
		Name:     manifest.Name,
		Manifest: manifest,
		Status:   pluginRegistered,
	}
	host := newPluginHost(
		pluginHostVersion,
		nil,
		pluginHostWorkspace(workspace),
		pluginHostSettings(newPluginSettingsStore(t.TempDir())),
	)

	deniedRead := host.handleHostCall(entry, "host.readFile", json.RawMessage(`{"path":"README.md"}`))
	if deniedRead.Err == nil || deniedRead.Err.Code != pluginRPCPermissionDenied ||
		!strings.HasPrefix(deniedRead.Err.Message, "permission denied:") {
		t.Fatalf("read without capability = %#v", deniedRead)
	}

	manifest.Capabilities.Files = "read"
	allowedRead := host.handleHostCall(entry, "host.readFile", json.RawMessage(`{"path":"README.md"}`))
	if allowedRead.Err != nil {
		t.Fatalf("read with capability: %#v", allowedRead)
	}

	escape := host.handleHostCall(entry, "host.readFile", json.RawMessage(`{"path":"../outside.txt"}`))
	if escape.Err == nil || escape.Err.Code != pluginRPCInvalidParams {
		t.Fatalf("parent path escape = %#v", escape)
	}
	symlinkEscape := host.handleHostCall(entry, "host.readFile", json.RawMessage(`{"path":"outside-link"}`))
	if symlinkEscape.Err == nil || symlinkEscape.Err.Code != pluginRPCInvalidParams {
		t.Fatalf("symlink escape = %#v", symlinkEscape)
	}

	deniedWrite := host.handleHostCall(entry, "host.writeFile",
		json.RawMessage(`{"path":"written.txt","content":"denied"}`))
	if deniedWrite.Err == nil || deniedWrite.Err.Code != pluginRPCPermissionDenied {
		t.Fatalf("write with read capability = %#v", deniedWrite)
	}
	manifest.Capabilities.Files = "write"
	allowedWrite := host.handleHostCall(entry, "host.writeFile",
		json.RawMessage(`{"path":"written.txt","content":"written"}`))
	if allowedWrite.Err != nil {
		t.Fatalf("write with write capability: %#v", allowedWrite)
	}

	undeclaredTask := host.handleHostCall(entry, "host.updateTask",
		json.RawMessage(`{"id":"missing.tasks","detail":"progress"}`))
	if undeclaredTask.Err == nil || undeclaredTask.Err.Code != pluginRPCPermissionDenied ||
		!strings.HasPrefix(undeclaredTask.Err.Message, "permission denied:") {
		t.Fatalf("undeclared task = %#v", undeclaredTask)
	}
	undeclaredRefresh := host.handleHostCall(entry, "host.refreshView",
		json.RawMessage(`{"view":"missing.view"}`))
	if undeclaredRefresh.Err == nil || undeclaredRefresh.Err.Code != pluginRPCPermissionDenied ||
		!strings.HasPrefix(undeclaredRefresh.Err.Message, "permission denied:") {
		t.Fatalf("undeclared refresh = %#v", undeclaredRefresh)
	}
	deniedWorkspace := host.handleHostCall(entry, "host.getWorkspace", json.RawMessage(`{}`))
	if deniedWorkspace.Err == nil || deniedWorkspace.Err.Code != pluginRPCPermissionDenied {
		t.Fatalf("workspace without capability = %#v", deniedWorkspace)
	}
	if got := host.status(entry.ID).DeniedCalls; got != 5 {
		t.Fatalf("denied calls = %d, want 5", got)
	}
}

func TestNodePluginP42RuntimeEndToEnd(t *testing.T) {
	pluginDirectory := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	script := `
import fs from "node:fs";
import readline from "node:readline";

const outside = ` + string(mustJSON(t, outside)) + `;
const lines = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
const send = (value) => process.stdout.write(JSON.stringify(value) + "\n");

for await (const line of lines) {
  const request = JSON.parse(line);
  if (!request.method) continue;
  const response = { jsonrpc: "2.0", id: request.id };
  switch (request.method) {
    case "initialize":
      response.result = { runtime: process.env.BYOP_PLUGIN_RUNTIME };
      break;
    case "activate":
    case "deactivate":
      response.result = {};
      break;
    case "invokeCommand": {
      let denied = false;
      try {
        fs.readFileSync(outside, "utf8");
      } catch (error) {
        denied = error?.code === "ERR_ACCESS_DENIED";
      }
      response.result = {
        message: denied ? "outside denied" : "outside allowed",
        denied,
      };
      break;
    }
    default:
      response.error = { code: -32601, message: "unknown method" };
  }
  send(response);
}
`
	if err := os.WriteFile(filepath.Join(pluginDirectory, "plugin.mjs"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := &pluginManifest{
		SchemaVersion: pluginManifestSchemaVersion,
		ID:            "example.node-p42",
		Name:          "Node P4.2",
		Version:       "0.1.0",
		Runtime:       "node",
		Entry:         "plugin.mjs",
		Compatibility: pluginCompatibility{Host: ">=0.4.2 <1.0.0", PluginAPI: "1"},
		Contributes: pluginContributions{
			Commands: []pluginCommandContribution{{ID: "node.run", Title: "Run"}},
		},
		Activation: pluginActivation{Events: []string{"onCommand:node.run"}},
	}
	if err := validatePluginManifest(manifest); err != nil {
		t.Fatalf("fixture manifest invalid: %v", err)
	}
	manifestPath := filepath.Join(pluginDirectory, pluginManifestFilename)
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, append(manifestData, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := pluginRegistryEntry{
		ID:              manifest.ID,
		Name:            manifest.Name,
		SourceDirectory: pluginDirectory,
		ManifestPath:    manifestPath,
		Manifest:        manifest,
		Status:          pluginRegistered,
	}

	host := newPluginHost(
		pluginHostVersion,
		nil,
		pluginHostWorkspace(t.TempDir()),
		pluginHostSettings(newPluginSettingsStore(t.TempDir())),
	)
	if _, err := host.nodeRuntime(); err != nil {
		t.Skip(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := host.invokeCommand(ctx, entry, "node.run", map[string]any{})
	if err != nil {
		t.Fatalf("invoke Node command: %v", err)
	}
	if got := pluginResultSummary(result); got != "outside denied" {
		t.Fatalf("Node permission result = %q", got)
	}
	if host.status(entry.ID).State != pluginStateActive {
		t.Fatalf("Node state = %s, want active", host.status(entry.ID).State)
	}
	if err := host.deactivate(entry.ID); err != nil {
		t.Fatalf("deactivate Node plugin: %v", err)
	}
	if host.status(entry.ID).State != pluginStateStopped {
		t.Fatalf("Node state after deactivate = %s", host.status(entry.ID).State)
	}
}

func p42Manifest() *pluginManifest {
	return &pluginManifest{
		SchemaVersion: pluginManifestSchemaVersion,
		ID:            "example.p42",
		Name:          "P4.2 Test",
		Version:       "0.1.0",
		Runtime:       "node",
		Entry:         "plugin.mjs",
		Compatibility: pluginCompatibility{Host: ">=0.4.2 <1.0.0", PluginAPI: "1"},
		Contributes: pluginContributions{
			Navigation: []pluginNavigationContribution{
				{ID: "p42", Title: "P4.2", Icon: "plugins", View: "p42.main"},
			},
			Commands: []pluginCommandContribution{
				{ID: "p42.run", Title: "Run"},
			},
			Views: []pluginViewContribution{
				{ID: "p42.main", Title: "P4.2", Location: "main", View: "native"},
			},
			TaskPanels: []pluginTaskPanelContribution{
				{ID: "p42.tasks", Title: "P4.2 tasks"},
			},
			Settings: []pluginSettingContribution{
				{
					ID:          "p42.settings",
					Title:       "P4.2 settings",
					Description: "Fixture settings.",
					Fields: []pluginSettingField{
						{
							Key:         "greeting",
							Label:       "Greeting",
							Type:        "string",
							Default:     json.RawMessage(`"Hello"`),
							Placeholder: "Hello",
						},
						{
							Key:     "showDetails",
							Label:   "Show details",
							Type:    "boolean",
							Default: json.RawMessage(`true`),
						},
						{
							Key:     "tone",
							Label:   "Tone",
							Type:    "select",
							Default: json.RawMessage(`"calm"`),
							Options: []string{"calm", "bright"},
						},
					},
				},
			},
		},
		Activation: pluginActivation{
			Events: []string{"onView:p42.main", "onCommand:p42.run"},
		},
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
