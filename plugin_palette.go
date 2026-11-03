package main

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// P25 paletteProviders consumer, together with the command palette that shows
// it. Cmd or Ctrl+K opens one list holding the core actions and sessions the app
// already offers, the commands registered plugins declare, and the results a
// declared palette provider returns for the current text. Nothing runs until the
// user picks a row, so typing only filters.

const (
	// pluginPaletteQueryTimeout bounds one provider query, covering a worker that
	// the call has to start.
	pluginPaletteQueryTimeout = 8 * time.Second
	// maxPaletteLocalRows caps the local groups so a long session list cannot
	// crowd out plugin commands, and maxPaletteQueryRunes caps what a provider is
	// asked about. The merged list keeps the existing overall cap of
	// 4*maxPaletteLocalRows, and each provider reply keeps maxPluginQueryItems.
	maxPaletteLocalRows  = 30
	maxPaletteQueryRunes = 80
)

// paletteItem is one executable row. Label is what the row shows, Detail why it
// exists, Section which group it came from, and run what picking it does.
type paletteItem struct {
	Key     string
	Label   string
	Detail  string
	Section string
	run     func()
}

// paletteOpenState is the palette's own state, kept out of appState so the
// palette can be built and checked as one unit.
type paletteOpenState struct {
	Query     string
	Selection int
	// Providers holds the answers of the current round, each kept with the plugin
	// and provider that returned it, so a row is only executable while its own
	// provider is still registered and declares the command it offers.
	Providers []paletteProviderBatch
	// RoundQuery is the text the current round was asked about, which is what lets
	// a changed query drop the old results instead of leaving them on screen.
	RoundQuery string
	// Asked records that a round has been started for RoundQuery, whether it is
	// still running or already answered, so a failing provider is not asked again
	// on every frame.
	Asked bool
	// Pending counts the replies still owed for the current round.
	Pending int
	Cancel  context.CancelFunc
}

// paletteProviderSource is one registered paletteProviders declaration, held with
// the plugin that declared it.
type paletteProviderSource struct {
	entry        pluginRegistryEntry
	contribution pluginQueryContribution
}

// paletteProviderBatch is one provider's answer kept with its source, so its rows
// can show where they came from and be re-checked against the live registry.
type paletteProviderBatch struct {
	pluginID   string
	pluginName string
	providerID string
	items      []pluginQueryItem
}

// paletteCoreItems are the app's own actions, each of which is an existing entry
// point rather than a new behaviour.
func (s *appState) paletteCoreItems() []paletteItem {
	core := text("paletteSectionCore")
	items := []paletteItem{
		{
			Key: "core-new-session", Label: text("navNewChat"), Section: core,
			Detail: text("paletteDetailNewSession"), run: s.newPiSession,
		},
		{
			Key: "core-plugins", Label: text("paletteOpenPlugins"), Section: core,
			Detail: text("paletteDetailPlugins"), run: func() { s.setPage(pagePlugins) },
		},
		{
			Key: "core-automations", Label: text("paletteOpenAutomations"), Section: core,
			Detail: text("paletteDetailAutomations"), run: func() { s.setPage(pageAutomations) },
		},
		{
			Key: "core-settings", Label: text("navSettings"), Section: core,
			Detail: text("paletteDetailSettings"), run: func() { s.setPage(pageSettings) },
		},
		{
			Key: "core-navigation", Label: text("paletteToggleNavigation"), Section: core,
			Detail: text("paletteDetailNavigation"),
			run:    func() { s.setNavCollapsed(!s.navCollapsed) },
		},
		{
			Key: "core-tasks", Label: text("paletteToggleTasks"), Section: core,
			Detail: text("paletteDetailTasks"),
			run:    func() { s.setTasksOpen(!s.tasksOpen) },
		},
		{
			Key: "core-changes", Label: text("paletteWorkspaceChanges"), Section: core,
			Detail: text("paletteDetailChanges"), run: s.toggleWorkspaceChanges,
		},
	}
	if s.piConn == piConnected {
		items = append(items, paletteItem{
			Key: "core-reconnect", Label: text("paletteReconnectPi"), Section: core,
			Detail: text("paletteDetailReconnect"), run: s.reconnectPi,
		})
		return items
	}
	return append(items, paletteItem{
		Key: "core-connect", Label: text("paletteConnectPi"), Section: core,
		Detail: text("paletteDetailConnect"), run: s.connectPi,
	})
}

// paletteSessionItems offer the sessions the sidebar already lists, so the
// palette reaches the same switchPiSession path rather than a new one.
func (s *appState) paletteSessionItems() []paletteItem {
	sessions := text("paletteSectionSessions")
	items := make([]paletteItem, 0, len(s.piSessions))
	for _, session := range s.piSessions {
		if len(items) >= maxPaletteLocalRows {
			break
		}
		label := firstNonEmpty(session.Name, session.Title, session.Path, session.ID)
		if label == "" {
			continue
		}
		summary := session
		items = append(items, paletteItem{
			Key: "session-" + fallback(summary.ID, summary.Path), Label: label,
			Section: sessions, Detail: fallback(summary.Cwd, summary.Path),
			run: func() { s.switchPiSession(summary) },
		})
	}
	return items
}

// paletteCommandItems are the commands registered plugins declare. A plugin that
// is disabled or incompatible is not registered, so its commands are absent
// without any extra check here.
func (s *appState) paletteCommandItems() []paletteItem {
	commands := text("paletteSectionCommands")
	var items []paletteItem
	for _, entry := range s.pluginRegistrySnapshot().Entries {
		if entry.Status != pluginRegistered || entry.Manifest == nil {
			continue
		}
		for _, command := range entry.Manifest.Contributes.Commands {
			if len(items) >= maxPaletteLocalRows {
				return items
			}
			label := fallback(command.Title, command.ID)
			itemEntry, itemCommand := entry, command
			items = append(items, paletteItem{
				Key:     "command-" + entry.ID + "-" + command.ID,
				Label:   label,
				Detail:  fallback(command.Description, entry.Name),
				Section: commands,
				run:     func() { s.runPluginCommand(itemEntry, itemCommand) },
			})
		}
	}
	return items
}

// paletteProviderSources lists every registered paletteProviders declaration. As
// with the mention slot, the field is an array and every declaration gets a
// consumer, so all of them are asked in one round.
func (s *appState) paletteProviderSources() []paletteProviderSource {
	var sources []paletteProviderSource
	for _, entry := range s.pluginRegistrySnapshot().Entries {
		if entry.Status != pluginRegistered || entry.Manifest == nil {
			continue
		}
		for _, contribution := range entry.Manifest.Contributes.PaletteProviders {
			sources = append(sources, paletteProviderSource{entry: entry, contribution: contribution})
		}
	}
	return sources
}

// paletteItems is the whole list for one query: local rows first, then whatever
// the providers answered for the text that is currently in the field.
func (s *appState) paletteItems(local []paletteItem, state *paletteOpenState) []paletteItem {
	filtered := paletteFilter(local, state.Query)
	if len(state.Providers) > 0 {
		filtered = append(filtered, s.paletteProviderRows(state)...)
	}
	if len(filtered) > 4*maxPaletteLocalRows {
		filtered = filtered[:4*maxPaletteLocalRows]
	}
	return filtered
}

// paletteProviderRows turns the cached provider results into rows. A result is
// only runnable when the plugin it came from is still registered and its command
// is one that plugin still declares, so a provider cannot run something its
// manifest never offered and a row whose provider has since gone away is dropped
// rather than shown inert. Each row is tagged with the plugin it came from.
func (s *appState) paletteProviderRows(state *paletteOpenState) []paletteItem {
	section := text("paletteSectionProvider")
	var items []paletteItem
	for _, batch := range state.Providers {
		// Resolved from the live registry rather than the query-time snapshot, so a
		// plugin disabled while the palette was open stops offering rows.
		current, ok := s.pluginEntryByID(batch.pluginID)
		if !ok || current.Manifest == nil ||
			pluginPaletteContributionByID(current.Manifest, batch.providerID) == nil {
			continue
		}
		for _, item := range batch.items {
			// Each reply already holds at most maxPluginQueryItems items, so only the
			// overall cap in paletteItems bounds the merged list.
			command, ok := pluginCommandByID(current.Manifest, item.Command)
			if item.Command == "" || !ok {
				continue
			}
			runEntry, runCommand, args := current, command, item.Args
			items = append(items, paletteItem{
				Key:     "provider-" + batch.pluginID + "-" + batch.providerID + "-" + item.ID,
				Label:   fallback(item.Label, item.ID),
				Detail:  fallback(item.Text, current.Name),
				Section: section,
				run:     func() { s.runPluginCommandWithArgs(runEntry, runCommand, args) },
			})
		}
	}
	return items
}

// paletteFilter keeps rows whose label or detail contains the query, which is how
// the palette searches core commands, sessions, and plugin commands at once.
func paletteFilter(items []paletteItem, query string) []paletteItem {
	token := strings.ToLower(strings.TrimSpace(query))
	if token == "" {
		return items
	}
	var matches []paletteItem
	for _, item := range items {
		haystack := strings.ToLower(item.Label + " " + item.Detail + " " + item.Section)
		if strings.Contains(haystack, token) {
			matches = append(matches, item)
		}
	}
	return matches
}

// startPaletteProviderQuery asks every declared provider about the current text in
// one round: one shared cancel set (the parent context) and the existing per-query
// timeout. The reply carries a request token from a monotonic counter on appState
// that is never reset, so a close and quick reopen cannot let an old reply match a
// fresh round and lose its cancel. Each reply is re-checked against the live
// registry before its results are kept.
func (s *appState) startPaletteProviderQuery(state *paletteOpenState, host *pluginHost) {
	sources := s.paletteProviderSources()
	if len(sources) == 0 || host == nil {
		return
	}
	if state.Cancel != nil {
		state.Cancel()
		state.Cancel = nil
	}
	s.paletteRequest++
	request := s.paletteRequest
	state.Providers = nil
	state.RoundQuery = state.Query
	state.Asked = true
	state.Pending = len(sources)
	parent, cancel := context.WithCancel(context.Background())
	state.Cancel = cancel
	workspace := host.pluginWorkspaceRoot()
	query := truncateRunes(state.Query, maxPaletteQueryRunes)
	for _, source := range sources {
		entry := source.entry
		pluginID, providerID, command := entry.ID, source.contribution.ID, source.contribution.Command
		go func() {
			ctx, cancelOne := context.WithTimeout(parent, pluginPaletteQueryTimeout)
			defer cancelOne()
			raw, err := host.invokeCommand(ctx, entry, command, map[string]any{
				"query":     query,
				"workspace": workspace,
			})
			items, decodeErr := decodePluginQueryItems(raw)
			s.postUI(func() {
				// A newer round, a close (which raises the counter through the next
				// open) or a closed window owns the palette now, so this reply is
				// stale and dropped without touching the shared cancel.
				if request != s.paletteRequest {
					return
				}
				if state.Pending > 0 {
					state.Pending--
				}
				if state.Pending == 0 {
					state.Cancel = nil
				}
				if err != nil || decodeErr != nil {
					return
				}
				// The plugin was disabled or removed while the query ran, or no
				// longer declares this provider, so its results are dropped.
				current, ok := s.pluginEntryByID(pluginID)
				if !ok || pluginPaletteContributionByID(current.Manifest, providerID) == nil {
					return
				}
				if len(items) == 0 {
					return
				}
				state.Providers = append(state.Providers, paletteProviderBatch{
					pluginID:   pluginID,
					pluginName: current.Name,
					providerID: providerID,
					items:      items,
				})
			})
		}()
	}
}

// commandPaletteDialog draws the palette when it is open. It is one dialog with
// one field: the same text filters the app's own actions and sessions, the
// commands plugins declare, and the results a declared provider returns. Rows are
// buttons with visible text, and a row runs only when the user picks it.
func (s *appState) commandPaletteDialog(c *ui.Context) {
	if !s.commandPaletteOpen {
		return
	}
	_, p := nativeThemeForAppearance(s.currentAppearancePreset(), s.effectiveDark(),
		s.highContrastActive(currentAppearanceHighContrast()),
		currentAppearanceReduceTransparency())
	open := s.commandPaletteOpen
	panel := ui.DialogBase(c, &open, func(backdrop ui.Element, body ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, .32))
		body.Width(560).MaxWidth(560).Padding(12).Gap(8).
			Radius(runtimeTokens.Dimension.Radius["overlay"].Value).
			Background(p.surface).Border(v12BorderWidth, p.borderStrong).
			Shadow(0, 10, 30, 0, p.shadowOverlay).Children(func() {
			s.commandPaletteBody(c, p)
		})
	})
	if !open || panel.Dismissed() {
		s.closeCommandPalette()
	}
}

// commandPaletteBody is the field and the filtered rows. Asking the provider
// happens here only when the text actually changed, so a frame never starts a
// second query for the same text.
func (s *appState) commandPaletteBody(c *ui.Context, p palette) {
	local := append(s.paletteCoreItems(), s.paletteSessionItems()...)
	local = append(local, s.paletteCommandItems()...)
	s.maybeRefreshPaletteProvider(&s.commandPaletteState)
	items := s.paletteItems(local, &s.commandPaletteState)
	if s.commandPaletteState.Selection >= len(items) {
		s.commandPaletteState.Selection = 0
	}
	placeholder := text("palettePlaceholder")
	input := ui.TextInputBase(c.Key("command-palette-query"), &s.commandPaletteState.Query).
		FillWidth().Height(32).Padding(0, 10).Radius(v12InputRadius).
		Border(v12BorderWidth, p.border).Background(p.composerSurface).
		TextColor(p.text).Label(placeholder).Placeholder(placeholder).
		FontSize(fontBody).AutoFocus()
	input.OnTextKey(func(k ui.TextKey) bool {
		switch {
		case k.Key == ui.KeyDown:
			s.movePaletteSelection(len(items), 1)
			return true
		case k.Key == ui.KeyUp:
			s.movePaletteSelection(len(items), -1)
			return true
		case k.Key == ui.KeyEnter && k.Mods == 0:
			if s.runPaletteItem(items, s.commandPaletteState.Selection) {
				k.SetText("")
				return true
			}
			return true
		}
		return false
	})
	if len(items) == 0 {
		ui.Text(c, text("paletteNoResults")).FontSize(fontBody).TextColor(p.muted)
		return
	}
	ui.Scroll(c).MaxHeight(320).FillWidth().MinWidth(0).Children(func() {
		ui.Column(c).FillWidth().MinWidth(0).Gap(2).Children(func() {
			section := ""
			for index, item := range items {
				if item.Section != section {
					section = item.Section
					ui.Text(c, section).FontSize(fontCaption).FontWeight(650).
						TextColor(p.muted).Padding(4, 0, 0, 0)
				}
				row := ui.ButtonBase(c.Key("palette-row-"+item.Key)).Label(item.Label).
					FillWidth().MinWidth(0).Height(30).Padding(0, 8).
					Radius(v12InputRadius).Justify(ui.Start).AlignItems(ui.Center).Gap(8).
					Background(ui.Color{}).TextColor(p.text)
				if index == s.commandPaletteState.Selection {
					row = row.Selected(true).Background(p.surfaceHover)
				} else if row.Hovered() {
					row = row.Background(p.surfaceHover)
				}
				row.Children(func() {
					ui.Text(c, item.Label).FontSize(fontBody).TextColor(p.text).
						SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
					if item.Detail != "" {
						ui.Text(c, item.Detail).FontSize(fontCaption).TextColor(p.muted).
							SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
					}
				})
				if row.Clicked() {
					s.runPaletteItem(items, index)
				}
			}
		})
	})
}

// maybeRefreshPaletteProvider asks the declared provider once per text change.
// With no provider declared the palette stays local and starts nothing.
func (s *appState) maybeRefreshPaletteProvider(state *paletteOpenState) {
	if len(s.paletteProviderSources()) == 0 {
		// The providers went away while the palette was open, so their results are
		// dropped rather than kept for a plugin the user can no longer reach.
		s.cancelPaletteProviderQuery(state)
		state.Providers = nil
		state.Asked = false
		return
	}
	// One round per text: a round already asked for this text is not repeated on
	// every frame, whether it is still running or already answered.
	if state.Asked && state.RoundQuery == state.Query {
		return
	}
	// The text changed since the results were asked about, so startPaletteProviderQuery
	// drops the old rows and the old round's cancel before asking again.
	s.startPaletteProviderQuery(state, s.plugins)
}

// cancelPaletteProviderQuery stops the round in flight, which stops every provider
// call of that round at once.
func (s *appState) cancelPaletteProviderQuery(state *paletteOpenState) {
	if state.Cancel != nil {
		state.Cancel()
		state.Cancel = nil
	}
	state.Pending = 0
}

func (s *appState) movePaletteSelection(count, step int) {
	if count == 0 {
		return
	}
	next := (s.commandPaletteState.Selection + step) % count
	if next < 0 {
		next += count
	}
	s.commandPaletteState.Selection = next
}

// runPaletteItem runs one row and closes the palette, so an action is always the
// result of an explicit choice.
func (s *appState) runPaletteItem(items []paletteItem, index int) bool {
	if index < 0 || index >= len(items) || items[index].run == nil {
		return false
	}
	items[index].run()
	s.closeCommandPalette()
	return true
}

// toggleCommandPalette opens or closes the palette. Opening starts no work on its
// own: the provider is asked only once there is text to ask about.
func (s *appState) toggleCommandPalette() {
	if s.commandPaletteOpen {
		s.closeCommandPalette()
		return
	}
	s.commandPaletteOpen = true
	s.commandPaletteState = paletteOpenState{}
	s.refreshPiSessions()
}

// closeCommandPalette closes the palette and drops its results. A query still
// running is cancelled, and the monotonic request counter is raised so a reply
// from this round cannot match a quick reopen and write old results into fresh
// state or clear the new round's cancel.
func (s *appState) closeCommandPalette() {
	s.paletteRequest++
	s.cancelPaletteProviderQuery(&s.commandPaletteState)
	s.commandPaletteOpen = false
	s.commandPaletteState = paletteOpenState{}
}
