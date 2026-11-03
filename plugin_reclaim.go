package main

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	pluginIdleReclaimDefaultSeconds = 60
	pluginIdleReclaimEnv            = "BYOP_PLUGIN_IDLE_RECLAIM_SECONDS"
	pluginReclaimTickMin            = time.Second
	pluginReclaimTickMax            = 15 * time.Second
)

// pluginIdleReclaimDefault resolves the idle window once, from the environment
// override or the built-in default. The resolved value is stored on the host at
// construction; a stored zero means reclaim is off and is never overridden here.
func pluginIdleReclaimDefault() time.Duration {
	value := strings.TrimSpace(os.Getenv(pluginIdleReclaimEnv))
	if value == "" {
		return pluginIdleReclaimDefaultSeconds * time.Second
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 0 {
		return pluginIdleReclaimDefaultSeconds * time.Second
	}
	return time.Duration(seconds) * time.Second
}

// pluginReclaimTick is how often a reclaim round is scheduled. A round only
// walks maps, so it is cheap, and it redraws only when it reclaims something.
func pluginReclaimTick(delay time.Duration) time.Duration {
	tick := delay / 4
	if tick < pluginReclaimTickMin {
		tick = pluginReclaimTickMin
	}
	if tick > pluginReclaimTickMax {
		tick = pluginReclaimTickMax
	}
	return tick
}

// startPluginReclaim arms the idle reclaim loop. The timer is single-shot and
// re-arms itself: the callback clears the stored pointer before scheduling the
// next round, so the loop keeps firing until stopPluginReclaim or window close.
// The callback reads no UI state; it only hands the round to the UI thread.
func (s *appState) startPluginReclaim() {
	if s == nil || s.plugins == nil {
		return
	}
	delay := s.plugins.idleReclaimDelay
	if delay <= 0 {
		return
	}
	tick := pluginReclaimTick(delay)
	var arm func()
	arm = func() {
		s.pluginReclaimMu.Lock()
		if s.pluginReclaimStop || s.pluginReclaimTimer != nil {
			s.pluginReclaimMu.Unlock()
			return
		}
		timer := time.AfterFunc(tick, func() {
			// Clear the pointer first: the stored timer has already fired, and
			// arm() refuses to run while one is still recorded.
			s.pluginReclaimMu.Lock()
			s.pluginReclaimTimer = nil
			stopped := s.pluginReclaimStop
			s.pluginReclaimMu.Unlock()
			if stopped || s.closed.Load() {
				return
			}
			s.pluginReclaimRounds.Add(1)
			s.runPluginReclaimTask(func() { s.pluginReclaimRound(time.Now()) })
			arm()
		})
		s.pluginReclaimTimer = timer
		s.pluginReclaimMu.Unlock()
	}
	s.pluginReclaimMu.Lock()
	s.pluginReclaimStop = false
	s.pluginReclaimMu.Unlock()
	arm()
}

func (s *appState) stopPluginReclaim() {
	if s == nil {
		return
	}
	s.pluginReclaimMu.Lock()
	s.pluginReclaimStop = true
	if s.pluginReclaimTimer != nil {
		s.pluginReclaimTimer.Stop()
		s.pluginReclaimTimer = nil
	}
	s.pluginReclaimMu.Unlock()
}

// runPluginReclaimTask runs reclaim work on the UI thread, where page state and
// cached WebViews live. Without a window the task runs inline, which is how
// focused tests exercise the same decision path.
func (s *appState) runPluginReclaimTask(task func()) {
	if s == nil || task == nil {
		return
	}
	if s.window == nil {
		task()
		return
	}
	s.postUI(task)
}

// pluginReclaimRound runs one reclaim pass on the UI thread and returns how many
// plugins were reclaimed. A plugin is a candidate from either side: a runtime
// process or busy status record, or a cached WebView whose plugin never started
// a worker, because a static WebView page can stay hidden without one.
func (s *appState) pluginReclaimRound(now time.Time) int {
	if s == nil || s.plugins == nil {
		return 0
	}
	// Use the value resolved once at construction. A stored zero is an explicit
	// "off" and must not fall back to the env default here.
	delay := s.plugins.idleReclaimDelay
	if delay <= 0 {
		return 0
	}
	candidates := make(map[string]struct{})
	for _, pluginID := range s.plugins.pluginRuntimeIDs() {
		candidates[pluginID] = struct{}{}
	}
	for _, pluginID := range s.cachedPluginWebViewPluginIDs() {
		candidates[pluginID] = struct{}{}
	}
	visible := s.visiblePluginViewID()
	reclaimed := 0
	for pluginID := range candidates {
		if s.pluginReclaimEligible(pluginID, visible, delay, now) {
			if s.reclaimPlugin(pluginID) {
				reclaimed++
			}
		}
	}
	if reclaimed > 0 && s.window != nil {
		s.window.Invalidate()
	}
	return reclaimed
}

// pluginReclaimEligible decides whether one plugin may release resources now.
// Hidden time starts when the view stops being visible and the worker must be
// idle for the same window, so a command, an in-flight call, or a running
// automation always defers the round.
func (s *appState) pluginReclaimEligible(
	pluginID, visible string,
	delay time.Duration,
	now time.Time,
) bool {
	if pluginID == "" || pluginID == visible {
		s.clearPluginHiddenSince(pluginID)
		return false
	}
	if s.plugins.pluginRestartInProgress(pluginID) ||
		s.plugins.pluginCallInFlight(pluginID) ||
		s.pluginAutomationRunning(pluginID) {
		s.notePluginHidden(pluginID, now)
		return false
	}
	process, idle, idleFor := s.plugins.pluginProcessForReclaim(pluginID)
	if process != nil && !idle {
		// A command or lifecycle call is in flight, so the next quiet stretch
		// must get the whole window again.
		s.notePluginHidden(pluginID, now)
		return false
	}
	hiddenSince, known := s.pluginHiddenSince(pluginID, now)
	if !known {
		// First round only starts the clock, so a reclaim never fires early.
		return false
	}
	idleStart := hiddenSince
	if idleFor > 0 {
		if started := now.Add(-idleFor); started.After(idleStart) {
			idleStart = started
		}
	}
	return !now.Before(idleStart.Add(delay))
}

// reclaimPlugin releases one idle plugin. It runs on the UI thread, captures the
// exact process it judged idle, and checks again before the asynchronous stop so
// reopening a view or starting a command in between keeps the plugin alive. It
// returns true only when it actually released a WebView; the worker stop is
// counted when it happens, and only the captured generation is ever detached.
func (s *appState) reclaimPlugin(pluginID string) bool {
	if s == nil || pluginID == "" || s.plugins == nil {
		return false
	}
	destroyed := s.destroyHiddenPluginWebViews(pluginID)
	process, _, _ := s.plugins.pluginProcessForReclaim(pluginID)
	if process == nil {
		if destroyed {
			s.clearPluginHiddenSince(pluginID)
		}
		return destroyed
	}
	epoch := s.pluginViewEpoch
	s.runPluginReclaimTask(func() {
		// Recheck before detaching: the user may have reopened the view or
		// started work while this stop was queued.
		if s.pluginViewEpoch != epoch || s.visiblePluginViewID() == pluginID ||
			s.plugins.pluginRestartInProgress(pluginID) ||
			s.pluginAutomationRunning(pluginID) {
			return
		}
		go func() {
			// stopPluginGeneration detaches only the captured process, under the
			// per-plugin start lock, and only while it holds no lease or pending
			// call. A generation that started meanwhile is left running.
			if !s.plugins.stopPluginGeneration(pluginID, process) {
				return
			}
			s.clearPluginHiddenSince(pluginID)
			s.runPluginReclaimTask(func() {
				if s.window != nil {
					s.window.Invalidate()
				}
			})
		}()
	})
	return destroyed
}

// visiblePluginViewID reports the plugin whose view is on screen right now.
func (s *appState) visiblePluginViewID() string {
	if s == nil || s.page != pagePluginView || s.pluginViewKind == "" {
		return ""
	}
	return s.pluginViewPluginID
}

// cachedPluginWebViewPluginIDs lists plugins with a cached WebView, which is how
// a static page that never started a worker still becomes a reclaim candidate.
func (s *appState) cachedPluginWebViewPluginIDs() []string {
	if s == nil {
		return nil
	}
	s.pluginWebViewsMu.RLock()
	seen := make(map[string]struct{}, len(s.pluginWebViews))
	for _, state := range s.pluginWebViews {
		if state != nil && state.pluginID != "" {
			seen[state.pluginID] = struct{}{}
		}
	}
	s.pluginWebViewsMu.RUnlock()
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	return ids
}

// destroyHiddenPluginWebViews releases the cached WebViews of one plugin unless
// that plugin's view is on screen, so a reclaim never destroys a live view.
func (s *appState) destroyHiddenPluginWebViews(pluginID string) bool {
	if s == nil || pluginID == "" {
		return false
	}
	if s.visiblePluginViewID() == pluginID {
		return false
	}
	s.pluginWebViewsMu.RLock()
	hasCached := false
	for _, state := range s.pluginWebViews {
		if state != nil && state.pluginID == pluginID {
			hasCached = true
			break
		}
	}
	s.pluginWebViewsMu.RUnlock()
	if !hasCached {
		return false
	}
	s.destroyPluginWebViews(pluginID)
	return true
}

// pluginHiddenSince returns when the plugin view stopped being visible and
// whether that moment is already known. The first sighting only starts the
// clock, so a reclaim never fires before the idle window has passed.
func (s *appState) pluginHiddenSince(pluginID string, now time.Time) (time.Time, bool) {
	if s == nil || pluginID == "" {
		return time.Time{}, false
	}
	s.pluginReclaimMu.Lock()
	defer s.pluginReclaimMu.Unlock()
	if s.pluginHiddenSinceByPlugin == nil {
		s.pluginHiddenSinceByPlugin = make(map[string]time.Time)
	}
	if since, ok := s.pluginHiddenSinceByPlugin[pluginID]; ok {
		return since, true
	}
	s.pluginHiddenSinceByPlugin[pluginID] = now
	return now, false
}

// notePluginHidden restarts the hidden clock while a plugin is still busy.
func (s *appState) notePluginHidden(pluginID string, now time.Time) {
	if s == nil || pluginID == "" {
		return
	}
	s.pluginReclaimMu.Lock()
	if s.pluginHiddenSinceByPlugin == nil {
		s.pluginHiddenSinceByPlugin = make(map[string]time.Time)
	}
	s.pluginHiddenSinceByPlugin[pluginID] = now
	s.pluginReclaimMu.Unlock()
}

func (s *appState) clearPluginHiddenSince(pluginID string) {
	if s == nil || pluginID == "" {
		return
	}
	s.pluginReclaimMu.Lock()
	delete(s.pluginHiddenSinceByPlugin, pluginID)
	s.pluginReclaimMu.Unlock()
}

// pluginAutomationRunning reports a plugin with an automation in flight, which
// is the case where a worker looks idle but must not be reclaimed.
func (s *appState) pluginAutomationRunning(pluginID string) bool {
	if s == nil || s.automations == nil || pluginID == "" {
		return false
	}
	entry, ok := s.registeredPluginEntry(pluginID)
	if !ok || entry.Manifest == nil {
		return false
	}
	for _, contribution := range entry.Manifest.Contributes.Automations {
		if s.automations.snapshotFor(automationKey(entry.ID, contribution.ID), 0).Running {
			return true
		}
	}
	return false
}
