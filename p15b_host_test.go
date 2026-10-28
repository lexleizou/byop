package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestP15bAgentExtensionDiscoveryAndValidation(t *testing.T) {
	root := t.TempDir()
	manifestValue := validPluginManifestMap()
	manifestValue["contributes"].(map[string]any)["agentExtension"] = "pi-extension.mjs"
	manifestPath := writePluginManifest(t, root, "example", manifestValue)
	pluginDirectory := filepath.Dir(manifestPath)
	writePluginWebViewFixture(t, pluginDirectory)
	extensionPath := filepath.Join(pluginDirectory, "pi-extension.mjs")
	if err := os.WriteFile(extensionPath, []byte("export default function plugin() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	registry := scanPluginRegistry([]pluginRootSpec{{Path: root}})
	if registry.registeredCount() != 1 {
		t.Fatalf("registry = %#v", registry)
	}
	args := piClientArgsForRegistry(registry, "")
	resolvedExtension, err := filepath.EvalSymlinks(extensionPath)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--mode", "rpc", "--extension", resolvedExtension}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}

	if err := os.Remove(extensionPath); err != nil {
		t.Fatal(err)
	}
	registry = scanPluginRegistry([]pluginRootSpec{{Path: root}})
	if len(registry.Entries) != 1 || registry.Entries[0].Status != pluginInvalidManifest ||
		!strings.Contains(registry.Entries[0].Error, "agent extension") {
		t.Fatalf("missing extension registry = %#v", registry)
	}
}

func TestP15bPluginDataDirsAndNodePermissions(t *testing.T) {
	configDir := t.TempDir()
	manifest := &pluginManifest{
		ID: "local.lightweight-table",
	}
	registry := pluginRegistry{Entries: []pluginRegistryEntry{{
		ID:       manifest.ID,
		Status:   pluginRegistered,
		Manifest: manifest,
	}}}
	directories, err := registeredPluginDataDirs(registry, configDir)
	if err != nil {
		t.Fatal(err)
	}
	wantDirectory := filepath.Join(configDir, pluginDataDirectoryName, manifest.ID)
	if directories[manifest.ID] != wantDirectory {
		t.Fatalf("directory = %q, want %q", directories[manifest.ID], wantDirectory)
	}
	info, err := os.Stat(wantDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("data directory = %#v", info)
	}
	encoded, err := pluginDataDirsJSON(directories)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(encoded), &decoded); err != nil ||
		decoded[manifest.ID] != wantDirectory {
		t.Fatalf("encoded directories = %q (%v)", encoded, err)
	}

	args := nodePermissionArgsWithData(
		&pluginManifest{},
		"/plugins/example",
		"/workspace",
		"/plugins/example/plugin.mjs",
		wantDirectory,
	)
	wantArgs := []string{
		"--permission",
		"--allow-fs-read=/plugins/example",
		"--allow-fs-read=" + wantDirectory,
		"--allow-fs-write=" + wantDirectory,
		"/plugins/example/plugin.mjs",
	}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("node args = %#v, want %#v", args, wantArgs)
	}
}

func TestP15bHostOpenChatContract(t *testing.T) {
	var received pluginOpenChatRequest
	host := newPluginHost(
		pluginHostVersion,
		nil,
		pluginHostOpenChatHandler(func(_ string, request pluginOpenChatRequest) {
			received = request
		}),
	)
	entry := pluginRegistryEntry{
		ID:       "local.lightweight-table",
		Name:     "Tables",
		Status:   pluginRegistered,
		Manifest: &pluginManifest{ID: "local.lightweight-table"},
	}
	params := json.RawMessage(`{
		"text": "Summarize the selected rows",
		"contextId": "ctx_123",
		"label": "Selected 3 rows"
	}`)
	result := host.handleHostCall(entry, "host.openChat", params)
	if result.Err != nil {
		t.Fatalf("openChat: %#v", result.Err)
	}
	if received.Text != "Summarize the selected rows" ||
		received.ContextID != "ctx_123" ||
		received.Label != "Selected 3 rows" {
		t.Fatalf("request = %#v", received)
	}

	invalid := host.handleHostCall(entry, "host.openChat", json.RawMessage(`{
		"text": "Summarize",
		"contextId": "ctx\n123",
		"label": "Rows"
	}`))
	if invalid.Err == nil || invalid.Err.Code != pluginRPCInvalidParams {
		t.Fatalf("invalid context id = %#v", invalid)
	}
}

func TestP15bTableContextPromptAndOpenChatState(t *testing.T) {
	attachment := &tableContextAttachment{
		ContextID: "ctx_123",
		Label:     "Selected 3 rows",
	}
	wirePrompt := tableContextPrompt("Summarize these", attachment)
	want := "Summarize these\n\nSelected 3 rows\n[byop-table-context:ctx_123]"
	if wirePrompt != want {
		t.Fatalf("wire prompt = %q, want %q", wirePrompt, want)
	}
	if got := displayTableContextPrompt(wirePrompt); got != "Summarize these\n\nSelected 3 rows" {
		t.Fatalf("display prompt = %q", got)
	}

	state := &appState{page: pagePlugins}
	state.openPluginChat(pluginOpenChatRequest{
		Text:      "Summarize these",
		ContextID: "ctx_123",
		Label:     "Selected 3 rows",
	})
	if state.page != pageChat || state.composerDraft != "Summarize these" ||
		!sameTableContext(state.composerTableContext, attachment) {
		t.Fatalf("open chat state = %#v", state)
	}

	restored, err := rebuildChatFromPiMessages([]json.RawMessage{
		json.RawMessage(`{"role":"user","content":"Summarize these\n\nSelected 3 rows\n[byop-table-context:ctx_123]","timestamp":1}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != 1 || messageText(&restored[0]) != "Summarize these\n\nSelected 3 rows" {
		t.Fatalf("restored chat = %#v", restored)
	}
}

func TestP15bQueuedSendClearsOrRestoresContextAfterAcceptance(t *testing.T) {
	attachment := &tableContextAttachment{ContextID: "ctx_123", Label: "Selected rows"}
	wirePrompt := tableContextPrompt("Update these", attachment)

	t.Run("accepted queue clears attachment", func(t *testing.T) {
		client := &piClient{}
		state := &appState{
			pi:                   client,
			piEpoch:              4,
			sessionGen:           7,
			run:                  runStreaming,
			activeRequest:        "active",
			runID:                "active",
			runningSendPending:   true,
			runningSendPrompt:    wirePrompt,
			runningSendDisplay:   "Update these",
			runningSendContext:   cloneTableContext(attachment),
			composerTableContext: cloneTableContext(attachment),
		}
		if !state.completeRunningSend(client, wirePrompt, runningSendQueue, 4, 7,
			"active", "active", runStreaming,
			piPromptResult{RequestID: "queued", Disposition: "queued"}, nil) {
			t.Fatal("queued send was not accepted")
		}
		if state.composerTableContext != nil {
			t.Fatalf("accepted queue retained context: %#v", state.composerTableContext)
		}
	})

	t.Run("failed send restores prompt and attachment", func(t *testing.T) {
		client := &piClient{}
		state := &appState{
			pi:                   client,
			piEpoch:              4,
			sessionGen:           7,
			run:                  runStreaming,
			activeRequest:        "active",
			runID:                "active",
			runningSendPending:   true,
			runningSendPrompt:    wirePrompt,
			runningSendDisplay:   "Update these",
			runningSendContext:   cloneTableContext(attachment),
			composerTableContext: cloneTableContext(attachment),
		}
		if state.completeRunningSend(client, wirePrompt, runningSendQueue, 4, 7,
			"active", "active", runStreaming,
			piPromptResult{}, errors.New("queue unavailable")) {
			t.Fatal("failed send reported success")
		}
		if state.composerDraft != "Update these" ||
			!sameTableContext(state.composerTableContext, attachment) {
			t.Fatalf("failed send state = %#v", state)
		}
	})
}

func TestP17StableExtensionChangeShowsPiRefreshWithoutRestarting(t *testing.T) {
	client := &piClient{}
	state := &appState{
		pi:     client,
		run:    runStreaming,
		piConn: piConnected,
	}
	state.refreshPiPluginRuntimeAfterScan("", "/tmp/pi-extension.mjs")
	if !state.pluginPiRefreshRequired {
		t.Fatal("stable extension change did not set the refresh-required state")
	}
	if state.channelRefresh.request != 0 || state.channelRefresh.pending ||
		state.channelRefresh.phase != "" {
		t.Fatalf("stable extension change restarted Pi automatically: %#v", state.channelRefresh)
	}
	if state.pi != client || state.run != runStreaming {
		t.Fatal("extension discovery interrupted the live session")
	}
	state.refreshPiPluginRuntimeAfterScan("/tmp/pi-extension.mjs", "/tmp/pi-extension.mjs")
	if state.channelRefresh.request != 0 || !state.pluginPiRefreshRequired {
		t.Fatalf("unchanged extension state changed unexpectedly: %#v", state)
	}
}
