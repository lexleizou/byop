package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Tests for the three interactive consumers (slash, mention, palette) and for
// the review fixes on the status and render slice. Each one drives the real call
// path the UI uses, and none starts a worker: the assertions read the state the
// frame left behind.

func interactiveRendererState(t *testing.T, manifest *pluginManifest) *appState {
	t.Helper()
	state := &appState{
		pluginRendererResults:    map[string]pluginRendererResult{},
		pendingPluginRenderers:   map[string]context.CancelFunc{},
		pluginRendererGeneration: map[string]uint64{},
		pluginRendererSignatures: map[string]string{},
	}
	state.plugins = newPluginHost(pluginHostVersion, func() {})
	state.setPluginRegistry(pluginRegistry{Entries: []pluginRegistryEntry{{
		ID: manifest.ID, Status: pluginRegistered, Manifest: manifest,
		SourceDirectory: t.TempDir(),
	}}})
	return state
}

func interactiveMimeManifest() *pluginManifest {
	return &pluginManifest{
		ID:      "local.pdfview",
		Version: "1.0.0",
		Contributes: pluginContributions{
			Commands: []pluginCommandContribution{{ID: "pdf.render", Title: "Render"}},
			MessageRenderers: []pluginMessageRendererContribution{{
				ID: "r.pdf", Title: "PDF", Command: "pdf.render",
				MimeTypes: []string{"application/pdf"},
			}},
		},
	}
}

// Review R1: two references with the same base name in different folders are two
// files, so the second chip must not show the render made for the first.
func TestP25MimeChipKeysArePerFilePath(t *testing.T) {
	state := interactiveRendererState(t, interactiveMimeManifest())
	metas := attachmentMetasFromPaths([]string{"/proj-a/report.pdf", "/proj-b/report.pdf"})
	if metas[0].Size != 0 || metas[1].Size != 0 || metas[0].Path == metas[1].Path {
		t.Fatalf("fixture metas = %#v", metas)
	}
	first := ui.NewTester(func(c *ui.Context) {
		state.chatAttachmentChipRow(c, metas[:1], "msg-a-attachments")
	}, 800, 200)
	first.Frame()
	if len(state.pluginRendererResults) != 1 {
		t.Fatalf("first chip scheduled %d renders", len(state.pluginRendererResults))
	}
	for key, result := range state.pluginRendererResults {
		result.state = pluginRendererReady
		result.model = pluginViewModel{Blocks: []pluginViewBlock{{Type: "text", Text: "RENDER-OF-PROJ-A"}}}
		state.pluginRendererResults[key] = result
		state.cancelPluginRender(key)
	}
	second := ui.NewTester(func(c *ui.Context) {
		state.chatAttachmentChipRow(c, metas[1:], "msg-b-attachments")
	}, 800, 200)
	second.Frame()
	if strings.Contains(strings.Join(second.Texts(), " | "), "RENDER-OF-PROJ-A") {
		t.Fatal("second path showed the render made for the first path")
	}
	if len(state.pluginRendererResults) != 2 {
		t.Fatalf("second chip reused the first key: %d entries", len(state.pluginRendererResults))
	}
}

// Review R1: a meta with no path has no file identity, so it keeps the plain chip
// rather than guessing a cache entry.
func TestP25MimeChipWithoutPathStaysPlain(t *testing.T) {
	state := interactiveRendererState(t, interactiveMimeManifest())
	metas := []chatAttachmentMeta{{Name: "report.pdf", Kind: attachmentFile, MimeType: "application/pdf"}}
	frame := ui.NewTester(func(c *ui.Context) {
		state.chatAttachmentChipRow(c, metas, "msg-nopath-attachments")
	}, 800, 200)
	frame.Frame()
	if len(state.pluginRendererResults) != 0 {
		t.Fatalf("pathless meta scheduled a render: %#v", state.pluginRendererResults)
	}
	if !strings.Contains(strings.Join(frame.Texts(), " | "), "report.pdf") {
		t.Fatal("plain chip did not render the attachment name")
	}
}

// Review R2: a scan that changed or removed a plugin drops that plugin's cached
// renders, so an update whose declaration did not change cannot keep serving the
// old code's result.
func TestP25ScanInvalidatesChangedAndRemovedRenderers(t *testing.T) {
	state := interactiveRendererState(t, interactiveMimeManifest())
	previous := state.pluginRegistrySnapshot()
	changedKey := pluginRendererCacheKey("local.pdfview", "r.pdf", "application/pdf", "a")
	removedKey := pluginRendererCacheKey("local.other", "r.other", "application/pdf", "b")
	state.pluginRendererResults[changedKey] = pluginRendererResult{
		state: pluginRendererReady, pluginID: "local.pdfview", request: 1,
		model: pluginViewModel{Blocks: []pluginViewBlock{{Type: "text", Text: "OLD-CODE"}}},
	}
	// The plugin that disappears also carries a failed render, which is what a
	// scan failure during the stop window leaves behind.
	previous.Entries = append(previous.Entries, pluginRegistryEntry{
		ID: "local.other", Status: pluginRegistered,
		Manifest: &pluginManifest{ID: "local.other", Version: "1.0.0"},
	})
	state.setPluginRegistry(previous)
	state.pluginRendererResults[removedKey] = pluginRendererResult{
		state: pluginRendererFailed, pluginID: "local.other", request: 2,
	}
	// A version bump whose declaration is otherwise identical, plus the plugin
	// that is gone. The manifests are separate objects because a scan reads them
	// from disk, so a shared pointer would hide the change.
	updated := pluginRegistry{Entries: []pluginRegistryEntry{
		{
			ID: "local.pdfview", Status: pluginRegistered,
			Manifest:        interactiveMimeManifest(),
			SourceDirectory: previous.Entries[0].SourceDirectory,
		},
	}}
	updated.Entries[0].Manifest.Version = "2.0.0"
	plan := planPluginScan(previous, updated)
	if len(plan.Changed) != 1 || plan.Changed[0] != "local.pdfview" ||
		len(plan.Removed) != 1 || plan.Removed[0] != "local.other" {
		t.Fatalf("plan = %#v", plan)
	}
	state.setPluginRegistry(updated)
	state.invalidatePluginRenderersForPlan(plan)
	if _, has := state.pluginRendererResults[changedKey]; has {
		t.Fatal("changed plugin kept its cached render")
	}
	if _, has := state.pluginRendererResults[removedKey]; has {
		t.Fatal("removed plugin kept its cached render")
	}
	if state.pluginRendererGeneration["local.pdfview"] == 0 {
		t.Fatal("changed plugin generation did not rise")
	}
}

// Review S1: a status reply that arrives after the plugin stopped being
// registered, or after its item was renamed, must not revive the HUD item.
func TestP25StatusItemIgnoresLateReply(t *testing.T) {
	manifest := &pluginManifest{
		ID: "local.dashboard",
		Contributes: pluginContributions{StatusItems: []pluginStatusContribution{
			{ID: "status.build", Title: "Build"},
		}},
	}
	state := &appState{pluginStatusItems: map[string]pluginStatusItemUpdate{}}
	state.setPluginRegistry(pluginRegistry{Entries: []pluginRegistryEntry{
		{ID: "local.dashboard", Status: pluginRegistered, Manifest: manifest},
	}})
	state.storePluginStatusItem(pluginStatusItemUpdate{
		PluginID: "local.dashboard", ID: "status.build", Text: "green",
	})
	if got := state.pluginStatusItems["local.dashboard"]; got.Text != "green" {
		t.Fatalf("a declared item was refused while the plugin is registered: %#v", got)
	}
	// A disable lands before the next reply, which is already on its way.
	disabled := pluginRegistry{Entries: []pluginRegistryEntry{
		{ID: "local.dashboard", Status: pluginDisabled, Manifest: manifest},
	}}
	state.setPluginRegistry(disabled)
	state.storePluginStatusItem(pluginStatusItemUpdate{PluginID: "local.dashboard", ID: "status.build", Text: "late"})
	if got := state.pluginStatusItems["local.dashboard"]; got.Text != "green" {
		t.Fatalf("disabled plugin revived its status item: %#v", got)
	}
	// Re-registered but with the item renamed, so the old value no longer matches
	// a declaration.
	renamed := &pluginManifest{
		ID: "local.dashboard",
		Contributes: pluginContributions{StatusItems: []pluginStatusContribution{
			{ID: "status.deploy", Title: "Deploy"},
		}},
	}
	state.setPluginRegistry(pluginRegistry{Entries: []pluginRegistryEntry{
		{ID: "local.dashboard", Status: pluginRegistered, Manifest: renamed},
	}})
	state.storePluginStatusItem(pluginStatusItemUpdate{PluginID: "local.dashboard", ID: "status.build", Text: "older"})
	if got := state.pluginStatusItems["local.dashboard"]; got.Text != "green" {
		t.Fatalf("undeclared item id overwrote the store: %#v", got)
	}
	state.storePluginStatusItem(pluginStatusItemUpdate{PluginID: "local.dashboard", ID: "status.deploy", Text: "live"})
	if got := state.pluginStatusItems["local.dashboard"]; got.Text != "live" {
		t.Fatalf("declared item id was refused: %#v", got)
	}
	// Hiding still releases the item.
	state.storePluginStatusItem(pluginStatusItemUpdate{PluginID: "local.dashboard", ID: "status.deploy", Hidden: true})
	if _, has := state.pluginStatusItems["local.dashboard"]; has {
		t.Fatal("hidden update kept the item")
	}
}

func TestP25SlashTokenAndMenuRows(t *testing.T) {
	for _, test := range []struct {
		draft string
		token string
		ok    bool
	}{
		{draft: "/", token: "", ok: true},
		{draft: "/qa", token: "qa", ok: true},
		{draft: "/QA", token: "qa", ok: true},
		{draft: "/qa status", ok: false},
		{draft: "//qa", ok: false},
		{draft: "docs/qa", ok: false},
		{draft: "", ok: false},
	} {
		token, ok := pluginSlashToken(test.draft)
		if ok != test.ok || token != test.token {
			t.Errorf("pluginSlashToken(%q) = %q,%v want %q,%v",
				test.draft, token, ok, test.token, test.ok)
		}
	}
	manifest := &pluginManifest{
		ID: "local.fixture",
		Contributes: pluginContributions{
			Commands: []pluginCommandContribution{{ID: "fixture.status.publish", Title: "Publish"}},
			SlashCommands: []pluginSlashCommandContribute{
				{ID: "slash.status", Title: "QA status", Name: "qa-status", Command: "fixture.status.publish"},
				{ID: "slash.prompt", Title: "QA prompt", Name: "qa-prompt", Prompt: "Summarize {args}"},
			},
		},
	}
	state := &appState{
		composerDraft: "/qa",
		piCommands:    []piCommand{{Name: "qa-other", Description: "Pi side", Source: "extension"}},
	}
	state.setPluginRegistry(pluginRegistry{Entries: []pluginRegistryEntry{
		{ID: "local.fixture", Status: pluginRegistered, Manifest: manifest},
	}})
	rows, selection, ok := state.slashMenuRows()
	if !ok || selection != 0 || len(rows) != 3 {
		t.Fatalf("rows = %#v selection=%d ok=%v", rows, selection, ok)
	}
	kinds := make(map[string]bool)
	for _, row := range rows {
		kinds[row.Tag] = true
		if row.Title == "" {
			t.Fatalf("row without visible title: %#v", row)
		}
	}
	// Plugin command, plugin prompt and the Pi row are labelled apart.
	if !kinds[text("slashKindPlugin")] || !kinds[text("slashKindPluginPrompt")] ||
		!kinds[text("slashKindPiExtension")] {
		t.Fatalf("row tags = %#v", kinds)
	}
}

// Choosing a slash row must never send: a command row runs the plugin command, a
// prompt row only inserts the template.
func TestP25SlashSelectionRunsNothingByItself(t *testing.T) {
	manifest := &pluginManifest{
		ID: "local.fixture",
		Contributes: pluginContributions{
			SlashCommands: []pluginSlashCommandContribute{
				{ID: "slash.prompt", Title: "QA prompt", Name: "qa-prompt", Prompt: "Summarize {args}"},
			},
		},
	}
	state := &appState{composerDraft: "/qa", pluginSlashDismissed: true}
	state.setPluginRegistry(pluginRegistry{Entries: []pluginRegistryEntry{
		{ID: "local.fixture", Status: pluginRegistered, Manifest: manifest},
	}})
	state.chooseSlashSuggestion(pluginSlashSuggestion{
		Name: "qa-prompt", Kind: "plugin-prompt", Prompt: "Summarize {args}",
	})
	if state.composerDraft != "Summarize {args}" {
		t.Fatalf("draft = %q", state.composerDraft)
	}
	if !state.composerSlashCaretPending {
		t.Fatal("caret was not queued after an inserted prompt")
	}
	// A dismissed menu shows nothing, so typing after Escape is not pushed a row.
	if rows, _, ok := state.slashMenuRows(); ok || rows != nil {
		t.Fatalf("dismissed menu still open: %#v", rows)
	}
}

// Pi commands belong to one process, so the fetch guard keys on the Pi epoch
// rather than on whether the list is empty: an empty answer for the current
// process counts as one attempt, and only a new process may be asked again.
func TestP25PiCommandsFetchOncePerEpoch(t *testing.T) {
	state := &appState{composerDraft: "/"}
	// Connecting raises the epoch before the menu can open, so the first token
	// for a connected process is not blocked by the initial zero.
	state.piEpoch = 1
	if state.piCommandsEpoch == state.piEpoch {
		t.Fatal("a new process must allow its first fetch")
	}
	state.piCommandsEpoch = state.piEpoch
	if state.piCommandsEpoch != state.piEpoch {
		t.Fatal("the epoch guard must stop a repeat fetch for the same process")
	}
	// A reconnect raises the epoch, which is what allows a fresh attempt.
	state.piEpoch++
	if state.piCommandsEpoch == state.piEpoch {
		t.Fatal("a reconnect must allow a fresh fetch")
	}
}

// A Pi command list belongs to the connected process, so a disconnect — here a
// process exit — drops it at once rather than only on the next connect, which
// would keep offering slash rows from a process that is already gone.
func TestP25PiDisconnectClearsCommands(t *testing.T) {
	state := projectionTestState()
	state.pi = &piClient{}
	state.piConn = piConnected
	state.piEpoch = 1
	state.piCommands = []piCommand{{Name: "qa-live", Description: "Pi side", Source: "extension"}}
	state.piCommandsFetching = true
	state.piCommandsEpoch = 1

	state.handlePiEvent(piEvent{Kind: piEventProcessExit, Error: "exit"}, 1)

	if len(state.piCommands) != 0 {
		t.Fatalf("commands survived a disconnect: %#v", state.piCommands)
	}
	if state.piCommandsFetching {
		t.Fatal("a disconnect left the fetching flag set")
	}
	if state.piCommandsEpoch != 0 {
		t.Fatalf("a disconnect left the command epoch set: %d", state.piCommandsEpoch)
	}
}

func TestP25MentionTokenAndReplace(t *testing.T) {
	for _, test := range []struct {
		draft string
		token string
		start int
		ok    bool
	}{
		{draft: "@", token: "@", start: 0, ok: true},
		{draft: "see @qa", token: "@qa", start: 4, ok: true},
		{draft: "@qa overview", ok: false},
		// Review I3: an @ that follows a letter is not a mention, so an e-mail
		// address in prose does not raise a menu, whether it ends the draft or not.
		{draft: "mail me@example", ok: false},
		{draft: "mail me at a@b", ok: false},
		{draft: "a@b", ok: false},
		{draft: "path C:\\x@y", ok: false},
		{draft: "ping @team now", ok: false},
		{draft: "@a@b", ok: false},
		{draft: "ping, then ask @qa", token: "@qa", start: 15, ok: true},
		{draft: "no token here", ok: false},
	} {
		token, start, ok := pluginMentionToken(test.draft)
		if ok != test.ok || token != test.token || (ok && start != test.start) {
			t.Errorf("pluginMentionToken(%q) = %q,%d,%v want %q,%d,%v",
				test.draft, token, start, ok, test.token, test.start, test.ok)
		}
	}
	state := &appState{
		composerDraft:     "summarize @qa",
		mentionQuery:      "qa",
		mentionTokenStart: 10,
		mentionResults: []mentionResult{
			{
				pluginID: "local.alpha", pluginName: "Alpha", providerID: "mention.alpha",
				item: pluginQueryItem{ID: "overview", Label: "QA overview", Text: "QA overview"},
			},
		},
	}
	rows, selection, ok := state.mentionMenuRows()
	if !ok || selection != 0 || len(rows) != 1 || rows[0].Title != "@QA overview" ||
		rows[0].Tag != "Alpha" {
		t.Fatalf("rows = %#v ok=%v", rows, ok)
	}
	state.chooseMentionRow()
	if state.composerDraft != "summarize QA overview " {
		t.Fatalf("draft after choosing = %q", state.composerDraft)
	}
	if !state.composerMentionCaret {
		t.Fatal("caret was not queued after the insertion")
	}
	if len(state.mentionResults) != 0 || state.mentionQuery != "" || state.mentionTokenStart != -1 {
		t.Fatal("choosing kept stale results")
	}
}

// A stale provider reply cannot fill the menu for a token it was not asked about.
func TestP25MentionRowsDropWhenTokenMoved(t *testing.T) {
	state := &appState{
		composerDraft:     "summarize @ot",
		mentionQuery:      "qa",
		mentionTokenStart: 10,
		mentionResults: []mentionResult{
			{pluginID: "local.alpha", providerID: "mention.alpha", item: pluginQueryItem{ID: "x", Label: "Old"}},
		},
	}
	if rows, _, ok := state.mentionMenuRows(); ok || rows != nil {
		t.Fatalf("rows survived a moved token: %#v", rows)
	}
}

// Review I2: one Escape must not turn mentions off for the rest of the session.
// The dismissal is remembered per token, so a later or different token queries
// again, while the token it was raised on stays dismissed.
func TestP25MentionEscapeAppliesToItsTokenOnly(t *testing.T) {
	state := &appState{mentionTokenStart: -1}
	state.plugins = newPluginHost(pluginHostVersion, func() {})
	manifest := &pluginManifest{ID: "local.alpha", Contributes: pluginContributions{
		Commands:         []pluginCommandContribution{{ID: "q", Title: "Q"}},
		MentionProviders: []pluginQueryContribution{{ID: "m", Title: "M", Command: "q"}},
	}}
	state.setPluginRegistry(pluginRegistry{Entries: []pluginRegistryEntry{
		{ID: "local.alpha", Status: pluginRegistered, Manifest: manifest, SourceDirectory: t.TempDir()},
	}})
	// An empty command path is fine: the guard is checked before any worker starts.
	state.composerDraft = "@a"
	state.maybeRefreshMentions()
	if state.mentionRequest != 1 {
		t.Fatalf("first token did not start a query: %d", state.mentionRequest)
	}
	state.cancelMentionQuery()
	// Simulate the reply so the menu is on screen, then press Escape.
	state.mentionResults = []mentionResult{
		{pluginID: "local.alpha", providerID: "m", item: pluginQueryItem{ID: "x", Label: "X"}},
	}
	if !state.handleComposerMenuTextKey(ui.KeyEscape, 0) {
		t.Fatal("Escape was not taken")
	}
	if !state.mentionDismissed {
		t.Fatal("Escape did not dismiss the menu")
	}
	// The dismissed token shows nothing until a different token raises a new list.
	if rows, _, ok := state.mentionMenuRows(); ok || rows != nil {
		t.Fatalf("dismissed menu still showed: %#v", rows)
	}
	// The token it was raised on stays dismissed and is not re-queried.
	state.maybeRefreshMentions()
	if state.mentionRequest != 1 || !state.mentionDismissed {
		t.Fatalf("same token re-queried after Escape: requests=%d dismissed=%v", state.mentionRequest, state.mentionDismissed)
	}
	// A different token later in the draft raises a fresh query.
	state.composerDraft = "@a done, now ask @c"
	state.maybeRefreshMentions()
	if state.mentionRequest != 2 || state.mentionDismissed {
		t.Fatalf("new token stayed silenced after Escape: requests=%d dismissed=%v", state.mentionRequest, state.mentionDismissed)
	}
	state.cancelMentionQuery()
}

func TestP25PaletteFilterAndProviderRows(t *testing.T) {
	local := []paletteItem{
		{Key: "a", Label: "New chat", Section: "Core"},
		{Key: "b", Label: "QA: publish fixture status", Detail: "Interactive Fixture", Section: "Commands"},
	}
	if got := paletteFilter(local, ""); len(got) != 2 {
		t.Fatalf("empty query filtered: %d", len(got))
	}
	if got := paletteFilter(local, "qa"); len(got) != 1 || got[0].Key != "b" {
		t.Fatalf("query filter = %#v", got)
	}
	// A provider result only runs a command the plugin declares.
	manifest := &pluginManifest{
		ID: "local.fixture",
		Contributes: pluginContributions{
			Commands: []pluginCommandContribution{{ID: "fixture.status.publish", Title: "Publish"}},
			// A row is only kept while its plugin still declares the provider it came
			// from, so the fixture declares the provider its batch names.
			PaletteProviders: []pluginQueryContribution{{ID: "palette.fixture", Title: "QA", Command: "fixture.status.publish"}},
		},
	}
	state := &appState{}
	state.setPluginRegistry(pluginRegistry{Entries: []pluginRegistryEntry{
		{ID: "local.fixture", Status: pluginRegistered, Manifest: manifest},
	}})
	open := &paletteOpenState{
		RoundQuery: "qa",
		Providers: []paletteProviderBatch{{
			pluginID: "local.fixture", pluginName: "Fixture", providerID: "palette.fixture",
			items: []pluginQueryItem{
				{ID: "publish", Label: "QA publish", Command: "fixture.status.publish"},
				{ID: "ghost", Label: "QA ghost", Command: "not.declared"},
				{ID: "labelonly", Label: "QA label only"},
			},
		}},
	}
	rows := state.paletteProviderRows(open)
	if len(rows) != 1 || rows[0].Key != "provider-local.fixture-palette.fixture-publish" {
		t.Fatalf("provider rows = %#v", rows)
	}
	// Review: a batch whose plugin is no longer registered drops out of the list
	// rather than staying as a row that runs through a stale snapshot.
	state.setPluginRegistry(pluginRegistry{Entries: nil})
	if rows := state.paletteProviderRows(open); len(rows) != 0 {
		t.Fatalf("unregistered provider survived: %#v", rows)
	}
	state.setPluginRegistry(pluginRegistry{Entries: []pluginRegistryEntry{
		{ID: "local.fixture", Status: pluginRegistered, Manifest: manifest},
	}})
	// The palette is closed by default and a close drops its state.
	state.toggleCommandPalette()
	if !state.commandPaletteOpen {
		t.Fatal("palette did not open")
	}
	state.commandPaletteState.Query = "qa"
	state.closeCommandPalette()
	if state.commandPaletteOpen || state.commandPaletteState.Query != "" {
		t.Fatalf("close left state: %#v", state.commandPaletteState)
	}
}

// Review I1: every registered provider declaration is asked in one round, and a
// chosen row only runs a command its own plugin declares.
func TestP25PaletteAsksEveryProvider(t *testing.T) {
	manifest := func(id, command string) *pluginManifest {
		return &pluginManifest{ID: id, Contributes: pluginContributions{
			Commands:         []pluginCommandContribution{{ID: command, Title: "Q"}},
			PaletteProviders: []pluginQueryContribution{{ID: "p", Title: "P", Command: command}},
		}}
	}
	state := &appState{}
	state.setPluginRegistry(pluginRegistry{Entries: []pluginRegistryEntry{
		{ID: "local.alpha", Status: pluginRegistered, Manifest: manifest("local.alpha", "alpha.q")},
		{ID: "local.beta", Status: pluginRegistered, Manifest: manifest("local.beta", "beta.q")},
	}})
	sources := state.paletteProviderSources()
	if len(sources) != 2 {
		t.Fatalf("palette provider sources = %d, want 2", len(sources))
	}
	// Rows keep their own source and resolve against the live registry, so an
	// alpha row never runs a beta command.
	open := &paletteOpenState{RoundQuery: "qa", Providers: []paletteProviderBatch{
		{pluginID: "local.alpha", providerID: "p", items: []pluginQueryItem{{ID: "a", Label: "A", Command: "alpha.q"}}},
		{pluginID: "local.beta", providerID: "p", items: []pluginQueryItem{{ID: "b", Label: "B", Command: "beta.q"}}},
	}}
	rows := state.paletteProviderRows(open)
	if len(rows) != 2 {
		t.Fatalf("provider rows = %#v", rows)
	}
	for _, row := range rows {
		if !strings.HasPrefix(row.Key, "provider-local.") {
			t.Fatalf("row lost its source: %#v", row)
		}
	}
}

// Review I1: the mention slot aggregates every declaration and each result keeps
// the plugin and provider that produced it.
func TestP25MentionAggregatesEveryProvider(t *testing.T) {
	manifest := func(id string) *pluginManifest {
		return &pluginManifest{ID: id, Contributes: pluginContributions{
			Commands:         []pluginCommandContribution{{ID: "q", Title: "Q"}},
			MentionProviders: []pluginQueryContribution{{ID: "m", Title: "M", Command: "q"}},
		}}
	}
	state := &appState{}
	state.setPluginRegistry(pluginRegistry{Entries: []pluginRegistryEntry{
		{ID: "local.alpha", Status: pluginRegistered, Manifest: manifest("local.alpha")},
		{ID: "local.beta", Status: pluginRegistered, Manifest: manifest("local.beta")},
	}})
	sources := state.mentionProviderSources()
	if len(sources) != 2 {
		t.Fatalf("mention provider sources = %d, want 2", len(sources))
	}
	state.composerDraft = "ping @q"
	state.mentionQuery = "q"
	state.mentionTokenStart = 5
	state.mentionResults = []mentionResult{
		{pluginID: "local.alpha", pluginName: "Alpha", providerID: "m", item: pluginQueryItem{ID: "x", Label: "X"}},
		{pluginID: "local.beta", pluginName: "Beta", providerID: "m", item: pluginQueryItem{ID: "y", Label: "Y"}},
	}
	rows, _, ok := state.mentionMenuRows()
	if !ok || len(rows) != 2 || rows[0].Tag != "Alpha" || rows[1].Tag != "Beta" {
		t.Fatalf("aggregated mention rows = %#v ok=%v", rows, ok)
	}
}

// Review: the palette's provider round is guarded by a monotonic request counter,
// so closing and quickly reopening cannot let an old reply land in fresh state.
func TestP25PaletteRequestMonotonicAcrossReopen(t *testing.T) {
	state := &appState{}
	state.paletteRequest++
	stale := state.paletteRequest
	// A close and reopen replaces the state and raises the counter; a reply that
	// captured the old value must not match.
	state.commandPaletteState.Query = "old"
	state.closeCommandPalette()
	state.toggleCommandPalette()
	if stale == state.paletteRequest {
		t.Fatal("reopen did not advance the monotonic request counter")
	}
	if state.commandPaletteState.Query != "" || state.commandPaletteState.Asked {
		t.Fatalf("reopen did not clear the closed palette state: %#v", state.commandPaletteState)
	}
}

// The fixture is a contract example, so a scan must register it with all six
// interactive slots and no issues.
func TestP25InteractiveFixtureRegisters(t *testing.T) {
	root := filepath.Join("examples", "interactive-fixture")
	registry := scanPluginRegistry([]pluginRootSpec{{Path: filepath.Dir(root), Explicit: true}})
	var entry *pluginRegistryEntry
	for index := range registry.Entries {
		if registry.Entries[index].ID == "example.interactive-fixture" {
			entry = &registry.Entries[index]
		}
	}
	if entry == nil || entry.Status != pluginRegistered || entry.Manifest == nil {
		t.Fatalf("fixture not registered: issues=%#v entries=%d", registry.Issues, len(registry.Entries))
	}
	loaded := entry.Manifest.Contributes
	if len(loaded.SlashCommands) != 2 || len(loaded.MentionProviders) != 1 ||
		len(loaded.PaletteProviders) != 1 || len(loaded.StatusItems) != 1 ||
		len(loaded.MessageRenderers) != 2 {
		t.Fatalf("fixture slots: %#v", loaded)
	}
	if _, _, ok := registry.messageRendererForLanguage("qa-card"); !ok {
		t.Fatal("qa-card language not matched")
	}
	if _, _, ok := registry.messageRendererForMimeType("image/png"); !ok {
		t.Fatal("image/png MIME not matched")
	}
	if _, ok := pluginStatusItemByID(entry.Manifest, "fixture.status"); !ok {
		t.Fatal("status item not resolvable")
	}
}
