package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// P28: a closed Mermaid fenced code block in the transcript offers a button
// that hands the block's source to the installed Mermaid preview page. The
// host keeps only the latest click in memory; nothing is written to disk and
// nothing is broadcast to Pi. The page reads the request with
// plugin.getMermaidPreview on load, and an already-loaded page is told to
// re-read by the byop.mermaid-preview event, so the first open and a cached
// open take the same reliable path: a queued request plus either an event or
// the page's own initial read.

const (
	// mermaidPreviewPluginID and mermaidPreviewViewID are the fixed pair the
	// protocol is reserved for.
	mermaidPreviewPluginID = "local.mermaid-diagram"
	mermaidPreviewViewID   = "mermaid.diagram"
	// mermaidPreviewMinVersion is the plugin release that consumes the
	// preview protocol. An older plugin still opens for manual editing, but
	// a click would import nothing, so the button asks for an update instead.
	mermaidPreviewMinVersion = "0.2.0"
	// maxMermaidPreviewSourceUnits matches the plugin renderer's
	// MAX_SOURCE_CHARS: JavaScript string length counts UTF-16 code units,
	// so the host rejects exactly what the page would refuse.
	maxMermaidPreviewSourceUnits = 20000
)

// pluginWebViewMermaidPreviewEvent tells the preview page that a newer click
// is waiting. It goes to the one preview WebView only; a page that misses it
// (still loading, or just created) reads the request itself on init.
var pluginWebViewMermaidPreviewEvent = mygo.NewEvent[map[string]any]("byop.mermaid-preview")

// mermaidPreviewRequest is one queued click. requestID is unique and
// monotonic per click, so a page can deduplicate reads and events and never
// roll back to an older request.
type mermaidPreviewRequest struct {
	requestID string
	source    string
}

// mermaidPreviewBlockEligible is the button's qualification rule: a closed
// fence tagged mermaid. The check is pure so the streaming case (Open) and
// other languages are decided without a render frame.
func mermaidPreviewBlockEligible(language string, open bool) bool {
	return !open && strings.EqualFold(strings.TrimSpace(language), "mermaid")
}

// mermaidPreviewBlockSource returns the block's source when the block is
// eligible and has content. The source is never rewritten. code is the
// block's own text, passed in so the rule stays testable: Code() reads the
// document behind the view, which only a rendered frame has.
func mermaidPreviewBlockSource(block *markdown.CodeBlockView, code string) (string, bool) {
	if block == nil || !mermaidPreviewBlockEligible(block.Language, block.Open) {
		return "", false
	}
	if len(code) == 0 {
		return "", false
	}
	return code, true
}

// utf16CodeUnits is JavaScript's string length: what the plugin's source
// limit counts.
func utf16CodeUnits(value string) int {
	return len(utf16.Encode([]rune(value)))
}

// mermaidPreviewAvailability decides whether a click may open the preview
// page, and which localized notice explains why it may not. A missing,
// incompatible, or invalid plugin asks for an install; a disabled one for an
// enable; a registered one without the main view or below the protocol
// version for an update. Nothing here installs, enables, or updates by itself.
func (s *appState) mermaidPreviewAvailability() (
	pluginRegistryEntry, pluginWebViewContribution, string, bool,
) {
	for _, entry := range s.pluginRegistrySnapshot().Entries {
		if entry.ID != mermaidPreviewPluginID {
			continue
		}
		switch entry.Status {
		case pluginDisabled:
			return pluginRegistryEntry{}, pluginWebViewContribution{},
				"mermaidPreviewDisabled", false
		case pluginRegistered:
		default:
			return pluginRegistryEntry{}, pluginWebViewContribution{},
				"mermaidPreviewMissing", false
		}
		if entry.Manifest == nil {
			return pluginRegistryEntry{}, pluginWebViewContribution{},
				"mermaidPreviewMissing", false
		}
		view, ok := pluginWebViewContributionByID(entry.Manifest, mermaidPreviewViewID)
		if !ok || view.Location != "main" {
			return pluginRegistryEntry{}, pluginWebViewContribution{},
				"mermaidPreviewOutdated", false
		}
		minimum, _ := parseSemanticVersion(mermaidPreviewMinVersion)
		version, parsed := parseSemanticVersion(entry.Manifest.Version)
		if !parsed || compareSemanticVersions(version, minimum) < 0 {
			return pluginRegistryEntry{}, pluginWebViewContribution{},
				"mermaidPreviewOutdated", false
		}
		return entry, view, "", true
	}
	return pluginRegistryEntry{}, pluginWebViewContribution{},
		"mermaidPreviewMissing", false
}

// queueMermaidPreview stores a click as the latest request and returns its
// id. All access to the request goes through pluginWebViewsMu, the lock the
// preview WebViews already use.
func (s *appState) queueMermaidPreview(source string) string {
	s.pluginWebViewsMu.Lock()
	defer s.pluginWebViewsMu.Unlock()
	s.mermaidPreviewSeq++
	s.mermaidPreview = &mermaidPreviewRequest{
		requestID: fmt.Sprintf("m%d", s.mermaidPreviewSeq),
		source:    source,
	}
	return s.mermaidPreview.requestID
}

// mermaidPreviewPayload is the plugin.getMermaidPreview result: the latest
// request, or nil when no click is waiting. Reading does not consume it, so
// reloads and re-reads stay idempotent.
func (s *appState) mermaidPreviewPayload() any {
	s.pluginWebViewsMu.RLock()
	defer s.pluginWebViewsMu.RUnlock()
	if s.mermaidPreview == nil {
		return nil
	}
	return map[string]any{
		"requestId": s.mermaidPreview.requestID,
		"source":    s.mermaidPreview.source,
	}
}

// clearMermaidPreviewLocked drops the stored request; callers hold
// pluginWebViewsMu.
func (s *appState) clearMermaidPreviewLocked() {
	s.mermaidPreview = nil
}

// openMermaidPreview is the chat button's click handler: validate, then store
// the request, then open the page. The order keeps the promise that a page
// loading right after the click finds the request on its initial read, and
// the open path only ever removes a preview WebView through
// removePluginWebView, never through the lifecycle clear, so this click's
// request cannot be cleared by its own open.
func (s *appState) openMermaidPreview(source string) {
	entry, view, noticeKey, ok := s.mermaidPreviewAvailability()
	if !ok {
		s.showNotice(text(noticeKey))
		return
	}
	if utf16CodeUnits(source) > maxMermaidPreviewSourceUnits {
		s.showNotice(text("mermaidPreviewTooLong"))
		return
	}
	requestID := s.queueMermaidPreview(source)
	s.openPluginWebView(entry, view)
	// A page that is already loaded gets the event and re-reads; any other
	// page state (loading, being created) reads the request on init, so a
	// lost event never strands a click.
	if state, has := s.pluginWebViewSnapshot(mermaidPreviewPluginID, mermaidPreviewViewID); has &&
		state.status == pluginWebViewReady && state.webView != nil && !state.webView.IsDestroyed() {
		_ = pluginWebViewMermaidPreviewEvent.EmitPage(state.webView.Page(),
			map[string]any{"requestId": requestID})
	}
}

// mermaidPreviewAction draws the button under one eligible Mermaid block. The
// key carries a content hash so two different blocks in one frame never share
// a widget identity; two identical blocks do share it, and clicking either
// queues the same source, which stays correct.
func mermaidPreviewAction(c *ui.Context, p palette, source string, open func()) {
	sum := sha256.Sum256([]byte(source))
	secondaryAction(c, p, "chat-mermaid-preview-"+hex.EncodeToString(sum[:6]),
		text("chatMermaidPreview"), open)
}
