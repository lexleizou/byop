package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
)

type pluginWebViewStatus string

const (
	pluginWebViewLoading pluginWebViewStatus = "loading"
	pluginWebViewReady   pluginWebViewStatus = "ready"
	pluginWebViewFailed  pluginWebViewStatus = "failed"

	maxPluginWebViewMessageBytes = 64 << 10
)

var pluginWebViewThemeEvent = mygo.NewEvent[map[string]any]("byop.theme")

type pluginWebViewState struct {
	webView             *mygo.WebView
	pluginID            string
	viewID              string
	entry               string
	sourceDirectory     string
	manifestFingerprint string
	status              pluginWebViewStatus
	error               string
}

type pluginWebViewSnapshot struct {
	webView *mygo.WebView
	status  pluginWebViewStatus
	error   string
}

type pluginWebViewBridge struct {
	state *appState
}

func (s *appState) setPluginRegistry(registry pluginRegistry) {
	s.pluginRegistryMu.Lock()
	s.pluginRegistry = registry
	s.pluginRegistryMu.Unlock()
}

func (s *appState) pluginRegistrySnapshot() pluginRegistry {
	s.pluginRegistryMu.RLock()
	defer s.pluginRegistryMu.RUnlock()
	return s.pluginRegistry
}

func (s *appState) registeredPluginWebViewEntry(pluginID string) (pluginRegistryEntry, bool) {
	entry, ok := s.registeredPluginEntry(pluginID)
	if !ok || entry.Manifest == nil || len(entry.Manifest.Contributes.WebViews) == 0 {
		return pluginRegistryEntry{}, false
	}
	return entry, true
}

func (s *appState) registeredPluginWebView(pluginID, viewID string) (pluginRegistryEntry, pluginWebViewContribution, bool) {
	entry, ok := s.registeredPluginEntry(pluginID)
	if !ok || entry.Manifest == nil {
		return pluginRegistryEntry{}, pluginWebViewContribution{}, false
	}
	view, ok := pluginWebViewContributionByID(entry.Manifest, viewID)
	if !ok || view.Location != "main" {
		return pluginRegistryEntry{}, pluginWebViewContribution{}, false
	}
	return entry, view, true
}

func pluginWebViewContributionByID(
	manifest *pluginManifest,
	id string,
) (pluginWebViewContribution, bool) {
	if manifest == nil {
		return pluginWebViewContribution{}, false
	}
	for _, contribution := range manifest.Contributes.WebViews {
		if contribution.ID == id {
			return contribution, true
		}
	}
	return pluginWebViewContribution{}, false
}

func (s *appState) openPluginWebView(entry pluginRegistryEntry, view pluginWebViewContribution) {
	registeredEntry, registeredView, ok := s.registeredPluginWebView(entry.ID, view.ID)
	if !ok || registeredView.Entry != view.Entry {
		return
	}
	s.page = pagePluginView
	s.pluginViewPluginID = registeredEntry.ID
	s.pluginViewID = registeredView.ID
	s.pluginViewKind = pluginViewWebView
	s.pluginViewLoading = false
	s.pluginViewError = ""
	s.notice = ""
	if _, err := s.ensurePluginWebView(registeredEntry, registeredView); err != nil {
		s.markPluginWebViewFailed(registeredEntry.ID, registeredView.ID, err)
	}
	if s.window != nil {
		s.window.Invalidate()
	}
}

func (s *appState) ensurePluginWebView(
	entry pluginRegistryEntry,
	view pluginWebViewContribution,
) (*pluginWebViewState, error) {
	key := pluginViewCacheKey(entry.ID, view.ID)
	fingerprint := pluginWebViewBindingFingerprint(entry)
	s.pluginWebViewsMu.RLock()
	state := s.pluginWebViews[key]
	s.pluginWebViewsMu.RUnlock()
	if state != nil && state.webView != nil && !state.webView.IsDestroyed() &&
		state.entry == view.Entry && state.manifestFingerprint == fingerprint {
		return state, nil
	}
	if state != nil {
		s.removePluginWebView(key, state)
	}
	if s.window == nil {
		return nil, errors.New("window is not ready")
	}

	s.pluginWebViewsMu.RLock()
	dark := s.pluginWebViewDark
	s.pluginWebViewsMu.RUnlock()
	webView, err := s.window.NewWebView(mygo.WebViewOptions{
		URL:             "byop-plugin://" + entry.ID + "/" + view.Entry,
		BackgroundColor: pluginColorHex(themePalette(dark).background),
		Page: mygo.PageOptions{
			PreloadScript: pluginWebViewBridgeScript(dark),
		},
	})
	if err != nil {
		state = &pluginWebViewState{
			pluginID:            entry.ID,
			viewID:              view.ID,
			entry:               view.Entry,
			sourceDirectory:     entry.SourceDirectory,
			manifestFingerprint: fingerprint,
			status:              pluginWebViewFailed,
			error:               uiErrorText(err, "Plugin WebView could not be created."),
		}
		s.pluginWebViewsMu.Lock()
		s.pluginWebViews[key] = state
		s.pluginWebViewsMu.Unlock()
		return state, err
	}

	state = &pluginWebViewState{
		webView:             webView,
		pluginID:            entry.ID,
		viewID:              view.ID,
		entry:               view.Entry,
		sourceDirectory:     entry.SourceDirectory,
		manifestFingerprint: fingerprint,
		status:              pluginWebViewLoading,
	}
	s.pluginWebViewsMu.Lock()
	s.pluginWebViews[key] = state
	s.pluginWebViewsMu.Unlock()

	page := webView.Page()
	page.OnWillNavigate(func(event *mygo.NavigateEvent) {
		if !pluginWebViewNavigationAllowed(entry.ID, event.URL) {
			event.PreventDefault()
		}
	})
	page.SetWindowOpenHandler(func(mygo.WindowOpenRequest) *mygo.WindowOptions {
		return nil
	})
	page.SetPermissionHandler(func(mygo.PermissionRequest) bool {
		return false
	})
	page.OnDidFinishLoad(func() {
		if !pluginWebViewNavigationAllowed(state.pluginID, page.URL()) {
			return
		}
		s.updatePluginWebViewState(state, func(current *pluginWebViewState) {
			current.status = pluginWebViewReady
			current.error = ""
		})
		s.pluginWebViewsMu.RLock()
		dark := s.pluginWebViewDark
		s.pluginWebViewsMu.RUnlock()
		_ = pluginWebViewThemeEvent.EmitPage(page, currentPluginThemePayload(dark))
	})
	page.OnDidFailLoad(func(loadErr *mygo.LoadError) {
		message := "WebView failed to load"
		if loadErr != nil {
			message = uiErrorFromText(loadErr.Error(), message)
		}
		s.updatePluginWebViewState(state, func(current *pluginWebViewState) {
			current.status = pluginWebViewFailed
			current.error = message
		})
	})
	page.OnRenderProcessGone(func(reason string) {
		message := uiErrorFromText(reason, "Web content process exited")
		s.updatePluginWebViewState(state, func(current *pluginWebViewState) {
			current.status = pluginWebViewFailed
			current.error = "Web content process exited: " + message
		})
	})

	return state, nil
}

func pluginWebViewBindingFingerprint(entry pluginRegistryEntry) string {
	root := filepath.Clean(entry.SourceDirectory)
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	manifest, _ := json.Marshal(entry.Manifest)
	sum := sha256.Sum256(bytes.Join([][]byte{[]byte(root), manifest}, []byte{0}))
	return fmt.Sprintf("%x", sum[:])
}

func pluginWebViewNavigationAllowed(pluginID, rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	hostID, _, ok := parsePluginWebViewURL(u)
	return ok && hostID == pluginID
}

func (s *appState) pluginWebViewSnapshot(pluginID, viewID string) (pluginWebViewSnapshot, bool) {
	s.pluginWebViewsMu.RLock()
	defer s.pluginWebViewsMu.RUnlock()
	state := s.pluginWebViews[pluginViewCacheKey(pluginID, viewID)]
	if state == nil {
		return pluginWebViewSnapshot{}, false
	}
	return pluginWebViewSnapshot{
		webView: state.webView,
		status:  state.status,
		error:   state.error,
	}, true
}

func (s *appState) updatePluginWebViewState(
	state *pluginWebViewState,
	update func(*pluginWebViewState),
) {
	if state == nil {
		return
	}
	s.pluginWebViewsMu.Lock()
	current := s.pluginWebViews[pluginViewCacheKey(state.pluginID, state.viewID)]
	if current != state {
		s.pluginWebViewsMu.Unlock()
		return
	}
	update(current)
	s.pluginWebViewsMu.Unlock()
	s.postUI(func() {
		if s.window != nil {
			s.window.Invalidate()
		}
	})
}

func (s *appState) markPluginWebViewFailed(pluginID, viewID string, err error) {
	s.pluginWebViewsMu.Lock()
	state := s.pluginWebViews[pluginViewCacheKey(pluginID, viewID)]
	if state != nil {
		state.status = pluginWebViewFailed
		state.error = uiErrorText(err, "WebView unavailable")
	}
	s.pluginWebViewsMu.Unlock()
}

func (s *appState) recreatePluginWebView(pluginID, viewID string) {
	entry, view, ok := s.registeredPluginWebView(pluginID, viewID)
	if !ok {
		s.page = pagePlugins
		s.pluginViewPluginID = ""
		s.pluginViewID = ""
		s.pluginViewKind = ""
		return
	}
	s.removePluginWebView(pluginViewCacheKey(pluginID, viewID), nil)
	if _, err := s.ensurePluginWebView(entry, view); err != nil {
		s.markPluginWebViewFailed(pluginID, viewID, err)
	}
	if s.window != nil {
		s.window.Invalidate()
	}
}

func (s *appState) refreshPluginView(pluginID, viewID string) {
	if s.page != pagePluginView || s.pluginViewPluginID != pluginID ||
		s.pluginViewID != viewID {
		return
	}
	switch s.pluginViewKind {
	case pluginViewNative:
		s.loadPluginViewByID(pluginID, viewID)
	case pluginViewWebView:
		state, ok := s.pluginWebViewSnapshot(pluginID, viewID)
		if !ok || state.webView == nil || state.webView.IsDestroyed() ||
			state.status == pluginWebViewFailed {
			s.recreatePluginWebView(pluginID, viewID)
			return
		}
		s.updatePluginWebViewStateByID(pluginID, viewID, func(current *pluginWebViewState) {
			current.status = pluginWebViewLoading
			current.error = ""
		})
		state.webView.Page().Reload()
	}
}

func (s *appState) updatePluginWebViewStateByID(
	pluginID,
	viewID string,
	update func(*pluginWebViewState),
) {
	s.pluginWebViewsMu.Lock()
	state := s.pluginWebViews[pluginViewCacheKey(pluginID, viewID)]
	if state != nil {
		update(state)
	}
	s.pluginWebViewsMu.Unlock()
	if s.window != nil {
		s.window.Invalidate()
	}
}

func (s *appState) reconcilePluginWebViews() {
	registry := s.pluginRegistrySnapshot()
	s.pluginWebViewsMu.Lock()
	var stale []*pluginWebViewState
	for key, state := range s.pluginWebViews {
		valid := false
		for _, entry := range registry.Entries {
			if entry.Status != pluginRegistered || entry.ID != state.pluginID ||
				entry.Manifest == nil {
				continue
			}
			view, exists := pluginWebViewContributionByID(entry.Manifest, state.viewID)
			if exists && view.Entry == state.entry &&
				state.manifestFingerprint == pluginWebViewBindingFingerprint(entry) {
				valid = true
			}
			break
		}
		if !valid {
			delete(s.pluginWebViews, key)
			stale = append(stale, state)
		}
	}
	s.pluginWebViewsMu.Unlock()
	for _, state := range stale {
		if state.webView != nil {
			state.webView.Destroy()
		}
	}
}

func (s *appState) destroyPluginWebViews(pluginID string) {
	s.pluginWebViewsMu.Lock()
	var removed []*pluginWebViewState
	for key, state := range s.pluginWebViews {
		if state.pluginID == pluginID {
			delete(s.pluginWebViews, key)
			removed = append(removed, state)
		}
	}
	s.pluginWebViewsMu.Unlock()
	for _, state := range removed {
		if state.webView != nil {
			state.webView.Destroy()
		}
	}
}

func (s *appState) destroyAllPluginWebViews() {
	s.pluginWebViewsMu.Lock()
	removed := make([]*pluginWebViewState, 0, len(s.pluginWebViews))
	for key, state := range s.pluginWebViews {
		delete(s.pluginWebViews, key)
		removed = append(removed, state)
	}
	s.pluginWebViewsMu.Unlock()
	for _, state := range removed {
		if state.webView != nil {
			state.webView.Destroy()
		}
	}
}

func (s *appState) removePluginWebView(key string, expected *pluginWebViewState) {
	s.pluginWebViewsMu.Lock()
	state := s.pluginWebViews[key]
	if state == nil || (expected != nil && state != expected) {
		s.pluginWebViewsMu.Unlock()
		return
	}
	delete(s.pluginWebViews, key)
	s.pluginWebViewsMu.Unlock()
	if state.webView != nil {
		state.webView.Destroy()
	}
}

func (s *appState) syncPluginWebViewTheme(dark bool) {
	s.pluginWebViewsMu.Lock()
	if s.pluginWebViewDark == dark {
		s.pluginWebViewsMu.Unlock()
		return
	}
	s.pluginWebViewDark = dark
	views := make([]*pluginWebViewState, 0, len(s.pluginWebViews))
	for _, state := range s.pluginWebViews {
		views = append(views, state)
	}
	s.pluginWebViewsMu.Unlock()

	payload := currentPluginThemePayload(dark)
	background := pluginColorHex(themePalette(dark).background)
	for _, state := range views {
		if state.webView == nil || state.webView.IsDestroyed() {
			continue
		}
		_ = state.webView.SetBackgroundColor(background)
		_ = pluginWebViewThemeEvent.EmitPage(state.webView.Page(), payload)
	}
}

func (s *appState) pluginWebViewBridgeSemaphore(pluginID string) chan struct{} {
	s.pluginBridgeMu.Lock()
	defer s.pluginBridgeMu.Unlock()
	slots := s.pluginBridgeSlots[pluginID]
	if slots == nil {
		slots = make(chan struct{}, pluginHostConcurrentRequests)
		s.pluginBridgeSlots[pluginID] = slots
	}
	return slots
}

func (b *pluginWebViewBridge) Call(
	ctx context.Context,
	method string,
	params json.RawMessage,
) (any, error) {
	if b == nil || b.state == nil {
		return nil, pluginBridgeError(pluginRPCOtherHostError, "WebView bridge is unavailable")
	}
	page := mygo.CallerPage(ctx)
	if page == nil {
		return nil, pluginBridgeError(pluginRPCPermissionDenied,
			"permission denied: bridge caller has no page")
	}

	b.state.pluginWebViewsMu.RLock()
	var state *pluginWebViewState
	for _, candidate := range b.state.pluginWebViews {
		if candidate.webView != nil && candidate.webView.Page() == page {
			if state != nil {
				state = nil
				break
			}
			state = candidate
		}
	}
	b.state.pluginWebViewsMu.RUnlock()
	if state == nil {
		return nil, pluginBridgeError(pluginRPCPermissionDenied,
			"permission denied: unknown WebView page")
	}
	if !pluginWebViewNavigationAllowed(state.pluginID, page.URL()) {
		return nil, pluginBridgeError(pluginRPCPermissionDenied,
			"permission denied: WebView URL does not match its plugin")
	}

	entry, view, ok := b.state.registeredPluginWebView(state.pluginID, state.viewID)
	if !ok || view.Entry != state.entry ||
		pluginWebViewBindingFingerprint(entry) != state.manifestFingerprint {
		return nil, pluginBridgeError(pluginRPCPermissionDenied,
			"permission denied: WebView declaration changed")
	}

	slots := b.state.pluginWebViewBridgeSemaphore(entry.ID)
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	default:
		return nil, pluginBridgeError(pluginRPCOtherHostError, "host busy")
	}
	return b.state.dispatchPluginWebViewCall(ctx, entry, state.viewID, method, params)
}

func (s *appState) dispatchPluginWebViewCall(
	ctx context.Context,
	entry pluginRegistryEntry,
	viewID,
	method string,
	params json.RawMessage,
) (any, error) {
	if entry.Status != pluginRegistered || entry.Manifest == nil {
		return nil, pluginBridgeError(pluginRPCOtherHostError, "plugin is not registered")
	}
	if _, ok := pluginWebViewContributionByID(entry.Manifest, viewID); !ok {
		return nil, pluginBridgeError(pluginRPCPermissionDenied,
			"permission denied: WebView is not declared")
	}
	if strings.HasPrefix(method, "host.") {
		s.plugins.updateStatus(entry.ID, func(status *pluginRuntimeStatus) {
			status.HostCalls++
		})
		result := s.plugins.handleHostCall(entry, method, params)
		if result.Err != nil {
			return nil, pluginBridgeError(result.Err.Code, result.Err.Message)
		}
		return result.Value, nil
	}
	if method != "plugin.postMessage" {
		return nil, pluginBridgeError(pluginRPCMethodNotFound, "unknown WebView method: "+method)
	}

	message, err := decodePluginWebViewMessage(params)
	if err != nil {
		return nil, pluginBridgeError(pluginRPCInvalidParams, err.Error())
	}
	process, err := s.plugins.ensureActive(ctx, entry, "onWebView:"+viewID)
	if err != nil {
		return nil, pluginBridgeError(pluginRPCOtherHostError, err.Error())
	}
	callCtx, cancel := context.WithTimeout(ctx, pluginCommandTimeout)
	defer cancel()
	result, err := process.call(callCtx, "webviewMessage", map[string]any{
		"view":    viewID,
		"message": message,
	})
	if err != nil {
		return nil, pluginBridgeError(pluginRPCOtherHostError, err.Error())
	}
	return decodePluginBridgeResult(result)
}

func decodePluginWebViewMessage(params json.RawMessage) (json.RawMessage, error) {
	if len(params) > maxPluginWebViewMessageBytes {
		return nil, errors.New("params exceed 64 KiB")
	}
	var request struct {
		Message json.RawMessage `json:"message"`
	}
	decoder := json.NewDecoder(bytes.NewReader(params))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("invalid params: more than one JSON value")
	}
	if len(request.Message) == 0 {
		return nil, errors.New("message is required")
	}
	if len(request.Message) > maxPluginWebViewMessageBytes {
		return nil, errors.New("message exceeds 64 KiB")
	}
	return request.Message, nil
}

func decodePluginBridgeResult(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, pluginBridgeError(pluginRPCOtherHostError,
			"plugin returned invalid JSON: "+err.Error())
	}
	return value, nil
}

func pluginBridgeError(code int, message string) error {
	data, _ := json.Marshal(pluginRPCError{Code: code, Message: message})
	return errors.New(string(data))
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func pluginWebViewBridgeScript(dark bool) string {
	initialTheme, _ := json.Marshal(currentPluginThemePayload(dark))
	return fmt.Sprintf(`(() => {
  "use strict";
  const initialTheme = %s;
  let theme = initialTheme;
  const listeners = new Set();

  function applyTheme(next) {
    theme = next;
    const root = document.documentElement;
    if (!root) return;
    const colors = next && next.colors ? next.colors : {};
    const variables = {
      "--byop-background": colors.background,
      "--byop-surface": colors.surface,
      "--byop-surface-hover": colors.surfaceHover,
      "--byop-text": colors.text,
      "--byop-muted": colors.muted,
      "--byop-border": colors.border,
      "--byop-accent": colors.accent,
      "--byop-success": colors.success,
      "--byop-warning": colors.warning,
      "--byop-danger": colors.danger,
      "--byop-font-body": "14px"
    };
    for (const [name, value] of Object.entries(variables)) {
      if (value) root.style.setProperty(name, value);
    }
    root.style.colorScheme = next && next.mode === "dark" ? "dark" : "light";
    for (const listener of listeners) {
      try { listener(theme); } catch (_) {}
    }
  }

  function bridgeError(error) {
    let payload = { code: -32000, message: String(error && error.message || error) };
    try {
      const parsed = JSON.parse(payload.message);
      if (parsed && typeof parsed === "object") payload = parsed;
    } catch (_) {}
    const failure = new Error(payload.message || "WebView bridge call failed");
    failure.code = payload.code ?? -32000;
    return failure;
  }

  if (window.mygo && typeof window.mygo.on === "function") {
    window.mygo.on("byop.theme", (next) => applyTheme(next));
  }
  if (document.documentElement) applyTheme(initialTheme);
  document.addEventListener("DOMContentLoaded", () => applyTheme(theme), { once: true });

  window.byop = {
    call(method, params = {}) {
      return window.mygo.call("BYOPWebView.Call", method, params).catch((error) => {
        throw bridgeError(error);
      });
    },
    postMessage(message) {
      return window.byop.call("plugin.postMessage", { message });
    },
    onTheme(listener) {
      if (typeof listener !== "function") return () => {};
      listeners.add(listener);
      try { listener(theme); } catch (_) {}
      return () => listeners.delete(listener);
    },
    get theme() {
      return theme;
    }
  };
})();`, initialTheme)
}
