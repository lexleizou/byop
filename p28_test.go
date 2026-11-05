package main

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/markdown"
)

// P28 host side: the transcript's Mermaid preview button hands the block's
// source to the fixed preview page. These tests cover the qualification rule,
// the availability notices, the memory request (identity, latest wins, reads
// do not consume), the call isolation of plugin.getMermaidPreview, and the
// lifecycle clearing that must not strand a click's own open.

func p28MermaidManifest(version string) *pluginManifest {
	return &pluginManifest{
		SchemaVersion: pluginManifestSchemaVersion,
		ID:            mermaidPreviewPluginID,
		Name:          "Mermaid Diagram",
		Version:       version,
		Runtime:       "node",
		Entry:         "plugin.mjs",
		Compatibility: pluginCompatibility{Host: ">=0.19.2 <1.0.0", PluginAPI: "1"},
		Contributes: pluginContributions{
			WebViews: []pluginWebViewContribution{{
				ID: mermaidPreviewViewID, Title: "Mermaid", Location: "main",
				Entry: "web/index.html",
			}},
		},
		Activation: pluginActivation{Events: []string{"onWebView:mermaid.diagram"}},
	}
}

func p28MermaidEntry(manifest *pluginManifest, status pluginRegistryStatus) pluginRegistryEntry {
	return pluginRegistryEntry{
		ID:              manifest.ID,
		Name:            manifest.Name,
		Manifest:        manifest,
		Status:          status,
		SourceDirectory: "/fixture/mermaid",
	}
}

func p28StateWith(entries ...pluginRegistryEntry) *appState {
	state := &appState{}
	state.setPluginRegistry(pluginRegistry{Entries: entries})
	return state
}

func TestP28MermaidPreviewBlockQualification(t *testing.T) {
	tests := []struct {
		name     string
		language string
		open     bool
		want     bool
	}{
		{name: "exact", language: "mermaid", want: true},
		{name: "upper", language: "MERMAID", want: true},
		{name: "spaced", language: " mermaid ", want: true},
		{name: "open fence", language: "mermaid", open: true, want: false},
		{name: "other language", language: "go", want: false},
		{name: "no language", language: "", want: false},
		{name: "mermaidish", language: "mermaid2", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := mermaidPreviewBlockEligible(test.language, test.open); got != test.want {
				t.Fatalf("eligible(%q, open=%v) = %v, want %v",
					test.language, test.open, got, test.want)
			}
		})
	}
	// An empty closed block never shows the button, and a nil block is safe.
	if _, ok := mermaidPreviewBlockSource(&markdown.CodeBlockView{Language: "mermaid"}, ""); ok {
		t.Fatal("empty mermaid block qualified")
	}
	if source, ok := mermaidPreviewBlockSource(&markdown.CodeBlockView{Language: "mermaid"},
		"graph TD; A-->B;"); !ok || source != "graph TD; A-->B;" {
		t.Fatalf("qualified block = %q, %v; want its source unchanged", source, ok)
	}
	if _, ok := mermaidPreviewBlockSource(nil, "graph TD;"); ok {
		t.Fatal("nil block qualified")
	}
}

func TestP28MermaidPreviewAvailabilityStates(t *testing.T) {
	installed := p28MermaidManifest("0.2.0")
	tests := []struct {
		name       string
		entries    []pluginRegistryEntry
		wantNotice string
		wantOK     bool
	}{
		{name: "not installed", wantNotice: "mermaidPreviewMissing"},
		{
			name:       "disabled",
			entries:    []pluginRegistryEntry{p28MermaidEntry(installed, pluginDisabled)},
			wantNotice: "mermaidPreviewDisabled",
		},
		{
			name:       "invalid manifest",
			entries:    []pluginRegistryEntry{p28MermaidEntry(installed, pluginInvalidManifest)},
			wantNotice: "mermaidPreviewMissing",
		},
		{
			name:       "old version",
			entries:    []pluginRegistryEntry{p28MermaidEntry(p28MermaidManifest("0.1.0"), pluginRegistered)},
			wantNotice: "mermaidPreviewOutdated",
		},
		{
			name:       "unparseable version",
			entries:    []pluginRegistryEntry{p28MermaidEntry(p28MermaidManifest("nightly"), pluginRegistered)},
			wantNotice: "mermaidPreviewOutdated",
		},
		{
			name: "no main view",
			entries: []pluginRegistryEntry{p28MermaidEntry(&pluginManifest{
				SchemaVersion: pluginManifestSchemaVersion, ID: mermaidPreviewPluginID,
				Version: "0.2.0",
			}, pluginRegistered)},
			wantNotice: "mermaidPreviewOutdated",
		},
		{
			name:       "installed",
			entries:    []pluginRegistryEntry{p28MermaidEntry(p28MermaidManifest("0.3.1"), pluginRegistered)},
			wantOK:     true,
			wantNotice: "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := p28StateWith(test.entries...)
			entry, view, noticeKey, ok := state.mermaidPreviewAvailability()
			if ok != test.wantOK || noticeKey != test.wantNotice {
				t.Fatalf("availability = ok %v notice %q, want ok %v notice %q",
					ok, noticeKey, test.wantOK, test.wantNotice)
			}
			if ok && (entry.ID != mermaidPreviewPluginID || view.ID != mermaidPreviewViewID) {
				t.Fatalf("availability returned %q/%q", entry.ID, view.ID)
			}
		})
	}
}

func TestP28MermaidPreviewQueueIdentityAndReads(t *testing.T) {
	state := &appState{}
	if got := state.mermaidPreviewPayload(); got != nil {
		t.Fatalf("payload before any click = %#v, want nil", got)
	}
	first := state.queueMermaidPreview("graph TD; A-->B;")
	second := state.queueMermaidPreview("sequenceDiagram; A->>B: hi")
	if first == "" || second == "" || first == second {
		t.Fatalf("request ids %q/%q must be unique", first, second)
	}
	firstSeq, err1 := strconv.Atoi(strings.TrimPrefix(first, "m"))
	secondSeq, err2 := strconv.Atoi(strings.TrimPrefix(second, "m"))
	if err1 != nil || err2 != nil || secondSeq <= firstSeq {
		t.Fatalf("request ids %q/%q must be monotonic", first, second)
	}
	// The latest click wins, and reading is idempotent: two reads return the
	// same object without consuming it.
	for attempt := range 2 {
		payload, ok := state.mermaidPreviewPayload().(map[string]any)
		if !ok {
			t.Fatalf("read %d: payload = %#v, want object", attempt, state.mermaidPreviewPayload())
		}
		if payload["requestId"] != second || payload["source"] != "sequenceDiagram; A->>B: hi" {
			t.Fatalf("read %d: payload = %#v, want the second click", attempt, payload)
		}
	}
}

func TestP28MermaidPreviewCallIsolation(t *testing.T) {
	entry := p28MermaidEntry(p28MermaidManifest("0.2.0"), pluginRegistered)
	hello := p28MermaidEntry(p5Manifest(), pluginRegistered)
	state := &appState{}
	ctx := context.Background()

	// An empty request answers null, not an error.
	result, err := state.dispatchPluginWebViewCall(
		ctx, entry, mermaidPreviewViewID, "plugin.getMermaidPreview", json.RawMessage(`{}`))
	if err != nil || result != nil {
		t.Fatalf("empty get = %#v, %v; want nil, nil", result, err)
	}

	requestID := state.queueMermaidPreview("graph LR; A-->B;")
	result, err = state.dispatchPluginWebViewCall(
		ctx, entry, mermaidPreviewViewID, "plugin.getMermaidPreview", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	payload, ok := result.(map[string]any)
	if !ok || payload["requestId"] != requestID || payload["source"] != "graph LR; A-->B;" {
		t.Fatalf("get result = %#v", result)
	}

	// Another plugin's view is refused...
	if _, err := state.dispatchPluginWebViewCall(
		ctx, hello, "hello.web", "plugin.getMermaidPreview", json.RawMessage(`{}`)); err == nil ||
		!strings.Contains(err.Error(), `"code":-32001`) {
		t.Fatalf("other plugin get error = %v, want permission denied", err)
	}
	// ...and so is another view of the preview plugin.
	if _, err := state.dispatchPluginWebViewCall(
		ctx, entry, "mermaid.other", "plugin.getMermaidPreview", json.RawMessage(`{}`)); err == nil ||
		!strings.Contains(err.Error(), `"code":-32001`) {
		t.Fatalf("other view get error = %v, want permission denied", err)
	}
}

func TestP28MermaidPreviewLifecycleClears(t *testing.T) {
	manifest := p28MermaidManifest("0.2.0")
	entry := p28MermaidEntry(manifest, pluginRegistered)
	t.Run("destroy by lifecycle releases the request", func(t *testing.T) {
		state := p28StateWith(entry)
		state.pluginWebViews = map[string]*pluginWebViewState{
			pluginViewCacheKey(mermaidPreviewPluginID, mermaidPreviewViewID): {
				pluginID: mermaidPreviewPluginID, viewID: mermaidPreviewViewID,
			},
		}
		state.queueMermaidPreview("graph TD;")
		state.destroyPluginWebViews("other.plugin")
		if state.mermaidPreviewPayload() == nil {
			t.Fatal("another plugin's destroy cleared the request")
		}
		state.destroyPluginWebViews(mermaidPreviewPluginID)
		if state.mermaidPreviewPayload() != nil {
			t.Fatal("lifecycle destroy kept the request")
		}
	})
	t.Run("reconcile releases a replaced page only", func(t *testing.T) {
		state := p28StateWith(entry)
		state.pluginWebViews = map[string]*pluginWebViewState{
			pluginViewCacheKey(mermaidPreviewPluginID, mermaidPreviewViewID): {
				pluginID: mermaidPreviewPluginID, viewID: mermaidPreviewViewID,
				entry: "web/index.html",
				manifestFingerprint: pluginWebViewBindingFingerprint(
					p28MermaidEntry(p28MermaidManifest("0.1.0"), pluginRegistered)),
			},
		}
		state.queueMermaidPreview("graph TD;")
		state.reconcilePluginWebViews()
		if state.mermaidPreviewPayload() != nil {
			t.Fatal("a replaced preview page kept its stale request")
		}
		// A still-valid page reconciles without touching the request.
		state.pluginWebViews[pluginViewCacheKey(mermaidPreviewPluginID, mermaidPreviewViewID)] =
			&pluginWebViewState{
				pluginID: mermaidPreviewPluginID, viewID: mermaidPreviewViewID,
				entry:               "web/index.html",
				manifestFingerprint: pluginWebViewBindingFingerprint(entry),
			}
		state.queueMermaidPreview("graph LR;")
		state.reconcilePluginWebViews()
		if state.mermaidPreviewPayload() == nil {
			t.Fatal("a valid preview page lost its request on reconcile")
		}
	})
	t.Run("a first open's pending request survives every open-path step", func(t *testing.T) {
		// No preview WebView exists yet (first open), so no lifecycle clear
		// has a page to destroy: the queued request must survive a reconcile,
		// an idle reclaim round, and the deferred creation call, all of which
		// run while the page is being created.
		state := p28StateWith(entry)
		state.pluginWebViews = map[string]*pluginWebViewState{}
		_, view, _, ok := state.mermaidPreviewAvailability()
		if !ok {
			t.Fatal("fixture plugin unavailable")
		}
		state.queueMermaidPreview("graph TD; A-->B;")
		state.reconcilePluginWebViews()
		state.destroyHiddenPluginWebViews(mermaidPreviewPluginID)
		state.createPluginWebViewAfterFrame(entry, view)
		if state.mermaidPreviewPayload() == nil {
			t.Fatal("the open path cleared the request it had just queued")
		}
	})
}

func TestP28MermaidPreviewOpen(t *testing.T) {
	manifest := p28MermaidManifest("0.2.0")
	entry := p28MermaidEntry(manifest, pluginRegistered)

	t.Run("unavailable plugin keeps the page and explains itself", func(t *testing.T) {
		state := &appState{}
		state.openMermaidPreview("graph TD;")
		if state.notice != text("mermaidPreviewMissing") {
			t.Fatalf("notice = %q, want the missing-plugin notice", state.notice)
		}
		if state.mermaidPreviewPayload() != nil {
			t.Fatal("an unavailable plugin queued a request")
		}
		if state.page != pageID("") {
			t.Fatalf("page = %q, want unchanged", state.page)
		}
	})

	t.Run("over-length source is refused without switching", func(t *testing.T) {
		state := p28StateWith(entry)
		// 10,000 astral characters are exactly 20,000 UTF-16 code units.
		state.openMermaidPreview(strings.Repeat("😀", 10001))
		if state.notice != text("mermaidPreviewTooLong") {
			t.Fatalf("notice = %q, want the too-long notice", state.notice)
		}
		if state.mermaidPreviewPayload() != nil {
			t.Fatal("an over-length source queued a request")
		}
		// The boundary itself passes and reaches the page.
		state.openMermaidPreview(strings.Repeat("😀", 10000))
		payload, ok := state.mermaidPreviewPayload().(map[string]any)
		if !ok || payload["source"] != strings.Repeat("😀", 10000) {
			t.Fatalf("boundary source payload = %#v, want it queued", payload)
		}
		if state.page != pagePluginView {
			t.Fatalf("page = %q, want the plugin view", state.page)
		}
	})

	t.Run("available plugin queues before opening", func(t *testing.T) {
		state := p28StateWith(entry)
		state.openMermaidPreview("graph TD; A-->B;")
		payload, ok := state.mermaidPreviewPayload().(map[string]any)
		if !ok || payload["source"] != "graph TD; A-->B;" {
			t.Fatalf("payload = %#v, want the clicked source queued", state.mermaidPreviewPayload())
		}
		if state.page != pagePluginView || state.pluginViewPluginID != mermaidPreviewPluginID ||
			state.pluginViewID != mermaidPreviewViewID || state.notice != "" {
			t.Fatalf("open state = page %q plugin %q view %q notice %q",
				state.page, state.pluginViewPluginID, state.pluginViewID, state.notice)
		}
	})
}
