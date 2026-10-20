package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewAppStateDoesNotReadAppearanceBeforeAppRun(t *testing.T) {
	state := newAppState()
	if state.pluginWebViewDark {
		t.Fatal("plugin WebView appearance should start unset until App.WhenReady")
	}
}

func TestP5ManifestWebViewValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*pluginManifest)
		wantErr string
	}{
		{
			name: "missing entry",
			mutate: func(manifest *pluginManifest) {
				manifest.Contributes.WebViews[0].Entry = ""
			},
			wantErr: "webview entry must be a clean relative HTML path",
		},
		{
			name: "parent entry",
			mutate: func(manifest *pluginManifest) {
				manifest.Contributes.WebViews[0].Entry = "../index.html"
			},
			wantErr: "webview entry must be a clean relative HTML path",
		},
		{
			name: "absolute entry",
			mutate: func(manifest *pluginManifest) {
				manifest.Contributes.WebViews[0].Entry = "/tmp/index.html"
			},
			wantErr: "webview entry must be a clean relative HTML path",
		},
		{
			name: "non html entry",
			mutate: func(manifest *pluginManifest) {
				manifest.Contributes.WebViews[0].Entry = "web/app.js"
			},
			wantErr: "webview entry must be a clean relative HTML path",
		},
		{
			name: "native and webview duplicate id",
			mutate: func(manifest *pluginManifest) {
				manifest.Contributes.Views = []pluginViewContribution{{
					ID:       "hello.web",
					Title:    "Native duplicate",
					Location: "main",
					View:     "native",
				}}
			},
			wantErr: "contribution id is duplicated",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := p5Manifest()
			test.mutate(manifest)
			err := validatePluginManifest(manifest)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("validate manifest error = %v, want %q", err, test.wantErr)
			}
		})
	}

	if manifest := p5Manifest(); validatePluginManifest(manifest) != nil {
		t.Fatalf("navigation to a WebView should validate: %v", validatePluginManifest(manifest))
	}
}

func TestP5RegistryRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	manifestPath := writePluginManifest(t, root, "example", validPluginManifestMap())
	pluginDirectory := filepath.Dir(manifestPath)
	if err := os.MkdirAll(filepath.Join(pluginDirectory, "web"), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.html")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(pluginDirectory, "web", "index.html")); err != nil {
		t.Fatal(err)
	}

	registry := scanPluginRegistry([]pluginRootSpec{{Path: root}})
	if len(registry.Entries) != 1 || registry.Entries[0].Status != pluginInvalidManifest ||
		!strings.Contains(registry.Entries[0].Error, "outside the plugin folder") {
		t.Fatalf("symlink escape result = %#v", registry.Entries)
	}
}

func TestP5SchemeHandlerServesIsolatedFiles(t *testing.T) {
	root := t.TempDir()
	manifest := p5Manifest()
	writeP5WebFiles(t, root)
	entry := pluginRegistryEntry{
		ID:              manifest.ID,
		Name:            manifest.Name,
		SourceDirectory: root,
		Manifest:        manifest,
		Status:          pluginRegistered,
	}
	state := &appState{}
	state.setPluginRegistry(pluginRegistry{Entries: []pluginRegistryEntry{entry}})
	handler := newPluginWebViewSchemeHandler(state)

	response := serveP5SchemeRequest(t, handler, "byop-plugin://example.hello-webview/web/index.html", http.MethodGet)
	if response.Code != http.StatusOK || response.Body.String() != "<!doctype html><title>P5</title>" {
		t.Fatalf("normal response = %d %q", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("content type = %q", response.Header().Get("Content-Type"))
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" ||
		response.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(response.Header().Get("Content-Security-Policy"), "connect-src 'none'") {
		t.Fatalf("unexpected security headers: %#v", response.Header())
	}

	head := serveP5SchemeRequest(t, handler, "byop-plugin://example.hello-webview/web/index.html", http.MethodHead)
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD response = %d %q", head.Code, head.Body.String())
	}

	manifest.Capabilities.Network = true
	state.setPluginRegistry(pluginRegistry{Entries: []pluginRegistryEntry{entry}})
	network := serveP5SchemeRequest(t, handler, "byop-plugin://example.hello-webview/web/index.html", http.MethodGet)
	if !strings.Contains(network.Header().Get("Content-Security-Policy"), "connect-src https:") {
		t.Fatalf("network CSP = %q", network.Header().Get("Content-Security-Policy"))
	}
}

func TestP5SchemeHandlerRejectsUnsafeTargets(t *testing.T) {
	root := t.TempDir()
	manifest := p5Manifest()
	writeP5WebFiles(t, root)
	if err := os.MkdirAll(filepath.Join(root, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.html")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape.html")); err != nil {
		t.Fatal(err)
	}
	state := &appState{}
	state.setPluginRegistry(pluginRegistry{Entries: []pluginRegistryEntry{{
		ID:              manifest.ID,
		Name:            manifest.Name,
		SourceDirectory: root,
		Manifest:        manifest,
		Status:          pluginRegistered,
	}}})
	handler := newPluginWebViewSchemeHandler(state)

	for _, rawURL := range []string{
		"byop-plugin://example.hello-webview/../outside.html",
		"byop-plugin://example.hello-webview/escape.html",
		"byop-plugin://unknown.plugin/web/index.html",
		"byop-plugin://example.hello-webview/directory",
	} {
		t.Run(rawURL, func(t *testing.T) {
			response := serveP5SchemeRequest(t, handler, rawURL, http.MethodGet)
			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404; body=%q", response.Code, response.Body.String())
			}
		})
	}
}

func TestP5BridgeDispatchCapabilitiesAndUnknownMethod(t *testing.T) {
	manifest := p5Manifest()
	entry := pluginRegistryEntry{
		ID:       manifest.ID,
		Name:     manifest.Name,
		Manifest: manifest,
		Status:   pluginRegistered,
	}
	state := &appState{
		pluginBridgeSlots: make(map[string]chan struct{}),
		plugins: newPluginHost(
			pluginHostVersion,
			nil,
			pluginHostWorkspace(t.TempDir()),
			pluginHostSettings(newPluginSettingsStore(t.TempDir())),
		),
	}

	allowed, err := state.dispatchPluginWebViewCall(
		context.Background(), entry, "hello.web", "host.getWorkspace", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("allowed host call: %v", err)
	}
	if value, ok := allowed.(map[string]any); !ok || value["name"] == "" {
		t.Fatalf("workspace result = %#v", allowed)
	}

	_, err = state.dispatchPluginWebViewCall(
		context.Background(), entry, "hello.web", "host.readFile",
		json.RawMessage(`{"path":"README.md"}`))
	if err == nil || !strings.Contains(err.Error(), `"code":-32001`) {
		t.Fatalf("denied read error = %v", err)
	}

	_, err = state.dispatchPluginWebViewCall(
		context.Background(), entry, "hello.web", "plugin.unknown", json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), `"code":-32601`) {
		t.Fatalf("unknown method error = %v", err)
	}
}

func TestP5BridgePostMessageNodeEcho(t *testing.T) {
	directory := t.TempDir()
	manifest := p5Manifest()
	manifestPath := filepath.Join(directory, pluginManifestFilename)
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	script := `
import readline from "node:readline";
const input = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
const send = (value) => process.stdout.write(JSON.stringify(value) + "\n");
for await (const line of input) {
  const request = JSON.parse(line);
  const response = { jsonrpc: "2.0", id: request.id };
  if (request.method === "webviewMessage") {
    response.result = { echo: request.params?.message, at: new Date(0).toISOString() };
  } else if (["initialize", "activate", "deactivate"].includes(request.method)) {
    response.result = {};
  } else {
    response.error = { code: -32601, message: "unknown" };
  }
  send(response);
}
`
	if err := os.WriteFile(filepath.Join(directory, manifest.Entry), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(directory, "web"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "web", "index.html"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := pluginRegistryEntry{
		ID:              manifest.ID,
		Name:            manifest.Name,
		SourceDirectory: directory,
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
	state := &appState{
		pluginBridgeSlots: make(map[string]chan struct{}),
		plugins:           host,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := state.dispatchPluginWebViewCall(
		ctx, entry, "hello.web", "plugin.postMessage",
		json.RawMessage(`{"message":{"kind":"ping","text":"hello"}}`))
	if err != nil {
		t.Fatalf("postMessage: %v", err)
	}
	value, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("postMessage result = %#v", result)
	}
	echo, ok := value["echo"].(map[string]any)
	if !ok || echo["kind"] != "ping" || echo["text"] != "hello" ||
		value["at"] != "1970-01-01T00:00:00.000Z" {
		t.Fatalf("echo result = %#v", result)
	}
	if err := host.deactivate(entry.ID); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
}

func TestP5NavigationAndPageFallback(t *testing.T) {
	for _, item := range navItems {
		if item.id == "library" || item.id == "document" {
			t.Fatalf("legacy navigation item remains: %#v", item)
		}
	}
	for _, value := range []string{"library", "document"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("BYOP_PAGE", value)
			if page := configuredPage(); page != pageChat {
				t.Fatalf("BYOP_PAGE=%s produced %s, want %s", value, page, pageChat)
			}
		})
	}
}

func p5Manifest() *pluginManifest {
	return &pluginManifest{
		SchemaVersion: pluginManifestSchemaVersion,
		ID:            "example.hello-webview",
		Name:          "Hello WebView",
		Version:       "0.1.0",
		Runtime:       "node",
		Entry:         "plugin.mjs",
		Compatibility: pluginCompatibility{Host: ">=0.5.0 <1.0.0", PluginAPI: "1"},
		Contributes: pluginContributions{
			Navigation: []pluginNavigationContribution{{
				ID: "hello-web", Title: "Hello WebView", Icon: "plugins", View: "hello.web",
			}},
			WebViews: []pluginWebViewContribution{{
				ID: "hello.web", Title: "Hello WebView", Location: "main", Entry: "web/index.html",
			}},
		},
		Activation: pluginActivation{Events: []string{"onWebView:hello.web"}},
		Capabilities: pluginCapabilities{
			Workspace: "read",
			Files:     "none",
			Network:   false,
			Process:   "none",
		},
	}
}

func writeP5WebFiles(t *testing.T, root string) {
	t.Helper()
	webDirectory := filepath.Join(root, "web")
	if err := os.MkdirAll(webDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webDirectory, "index.html"),
		[]byte("<!doctype html><title>P5</title>"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func serveP5SchemeRequest(
	t *testing.T,
	handler http.Handler,
	rawURL,
	method string,
) *httptest.ResponseRecorder {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	request := &http.Request{Method: method, URL: u}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
