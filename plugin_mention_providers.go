package main

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// P25 mentionProviders consumer. A draft that ends in an @ token — "@" or
// "@word" with the token started at the beginning or after whitespace — offers
// the items the declared providers returned, and choosing one replaces exactly
// that token. Nothing here edits the draft on its own: the list is derived from
// what the user typed, and providers are asked only while such a token exists.
// Every registered mentionProviders declaration is queried in one round, so a
// second plugin's provider is consumed rather than silently ignored, and each
// row records the plugin it came from.

const (
	// maxPluginQueryItems caps the visible list so the providers together cannot
	// fill the screen, and maxPluginMentionQueryRunes caps what a plugin is asked
	// about, which is the text the user typed after the @.
	maxPluginQueryItems        = 12
	maxPluginMentionQueryRunes = 80
	// pluginMentionQueryTimeout bounds one provider query. The command may start
	// an idle worker, so the timeout covers activation and the call together.
	pluginMentionQueryTimeout = 8 * time.Second
)

// pluginMentionToken splits a draft into the active @ token and whether the draft
// ends in one. The token must start at the beginning of the draft or after
// whitespace, so "mail me at a@b" — where the @ follows a letter — is not a
// mention, and it must hold no further whitespace, so the user finishes a token
// by typing a space.
func pluginMentionToken(draft string) (string, int, bool) {
	at := strings.LastIndexByte(draft, '@')
	if at < 0 {
		return "", 0, false
	}
	if at > 0 {
		switch draft[at-1] {
		case ' ', '\t', '\n', '\r':
		default:
			return "", 0, false
		}
	}
	token := draft[at:]
	if strings.ContainsAny(token, " \t\n\r") {
		return "", 0, false
	}
	return token, at, true
}

// mentionProviderSource is one registered mentionProviders declaration, held with
// the plugin that declared it so its reply and rows keep their own source.
type mentionProviderSource struct {
	entry        pluginRegistryEntry
	contribution pluginQueryContribution
}

// mentionResult is one item a provider returned, kept with the plugin and provider
// that produced it, so a chosen row inserts its own item and each row can show and
// re-check the source it came from.
type mentionResult struct {
	pluginID   string
	pluginName string
	providerID string
	item       pluginQueryItem
}

// mentionProviderSources lists every registered mentionProviders declaration.
// Unlike the renderers, where one language maps to one owner, the slot is an array
// and every declaration gets a consumer, so all of them are asked in one round.
func (s *appState) mentionProviderSources() []mentionProviderSource {
	var sources []mentionProviderSource
	for _, entry := range s.pluginRegistrySnapshot().Entries {
		if entry.Status != pluginRegistered || entry.Manifest == nil {
			continue
		}
		for _, contribution := range entry.Manifest.Contributes.MentionProviders {
			sources = append(sources, mentionProviderSource{entry: entry, contribution: contribution})
		}
	}
	return sources
}

// mentionMenuRows reports the mention list for the current draft with its
// selection, in the shape the shared composer menu renderer draws.
func (s *appState) mentionMenuRows() ([]composerMenuItem, int, bool) {
	rows := s.cachedMentionRows()
	if len(rows) == 0 || s.mentionDismissed {
		return nil, 0, false
	}
	if s.mentionSelection >= len(rows) {
		s.mentionSelection = 0
	}
	return rows, s.mentionSelection, true
}

// cachedMentionRows builds rows from the cached results and shows them only while
// the draft still ends in the token those results were asked about: the same
// start and the same text after the @. A reply that describes anything else is
// dropped when it arrives, so the render frame holds no query, file read, or
// process start. Each row is tagged with the plugin it came from.
func (s *appState) cachedMentionRows() []composerMenuItem {
	token, start, ok := pluginMentionToken(s.composerDraft)
	if !ok || len(s.mentionResults) == 0 {
		return nil
	}
	if start != s.mentionTokenStart || strings.TrimPrefix(token, "@") != s.mentionQuery {
		return nil
	}
	rows := make([]composerMenuItem, 0, len(s.mentionResults))
	for _, result := range s.mentionResults {
		rows = append(rows, composerMenuItem{
			Key:    "mention-" + result.pluginID + "-" + result.providerID + "-" + result.item.ID,
			Title:  "@" + strings.TrimSpace(result.item.Label),
			Detail: result.item.Text,
			Tag:    result.pluginName,
		})
	}
	return rows
}

// chooseMentionRow inserts the selected row's own item text in place of the active
// token and closes the list. A token that no longer matches the cached query is a
// stale choice, so nothing is inserted rather than guessing where to write.
func (s *appState) chooseMentionRow() {
	rows, selection, ok := s.mentionMenuRows()
	if !ok || selection >= len(rows) || selection >= len(s.mentionResults) {
		s.clearMentionMenu()
		return
	}
	result := s.mentionResults[selection]
	_, start, ok := pluginMentionToken(s.composerDraft)
	if !ok || start != s.mentionTokenStart {
		s.clearMentionMenu()
		return
	}
	insert := fallback(result.item.Text, result.item.Label)
	s.composerDraft = s.composerDraft[:start] + insert + " "
	s.mentionResults = nil
	s.mentionQuery = ""
	s.mentionTokenStart = -1
	s.composerMentionCaret = true
}

// applyPendingComposerMentionCaret puts the caret after inserted text so the user
// keeps typing where the insertion ended.
func (s *appState) applyPendingComposerMentionCaret(input ui.Element) {
	if !s.composerMentionCaret {
		return
	}
	caret := len([]rune(s.composerDraft))
	input.SetTextSelection(caret, caret)
	input.Focus()
	s.composerMentionCaret = false
}

func (s *appState) clearMentionMenu() {
	s.mentionSelection = 0
	s.mentionDismissed = false
	s.mentionDismissedStart = -1
	s.mentionDismissedQuery = ""
	s.mentionResults = nil
	s.mentionQuery = ""
	s.mentionTokenStart = -1
	s.cancelMentionQuery()
}

// dismissMentionMenu closes the mention list for the token typed so far, mirroring
// the slash rule. The dismissal remembers the token it was raised on so that
// starting or editing a different @ token — or having none at all — raises a fresh
// list rather than leaving mentions off for the rest of the session.
func (s *appState) dismissMentionMenu() {
	s.mentionSelection = 0
	s.mentionDismissed = true
	if token, start, ok := pluginMentionToken(s.composerDraft); ok {
		s.mentionDismissedStart = start
		s.mentionDismissedQuery = strings.TrimPrefix(token, "@")
	} else {
		s.mentionDismissedStart = -1
		s.mentionDismissedQuery = ""
	}
}

// cancelMentionQuery stops the round that no longer describes the draft. Cancelling
// the parent context stops every provider call started in that round at once. The
// replies are also guarded by the request token, so cancelling is about not keeping
// workers busy rather than about correctness.
func (s *appState) cancelMentionQuery() {
	if s.mentionCancel != nil {
		s.mentionCancel()
		s.mentionCancel = nil
	}
}

// maybeRefreshMentions starts a provider round when the draft ends in an @ token
// whose text differs from the one the results were asked about. An Escape
// dismissal applies only to the token it was raised on, so a new or edited token —
// or the absence of one — clears it. It is called from the render frame, so it only
// ever schedules tasks.
func (s *appState) maybeRefreshMentions() {
	token, start, ok := pluginMentionToken(s.composerDraft)
	if !ok {
		s.mentionDismissed = false
		return
	}
	query := strings.TrimPrefix(token, "@")
	if s.mentionDismissed && (start != s.mentionDismissedStart || query != s.mentionDismissedQuery) {
		s.mentionDismissed = false
	}
	if s.mentionDismissed {
		return
	}
	// The same token start and query already has a round in flight or answered, so
	// a failing provider is not asked again on every frame.
	if s.mentionTokenStart == start && s.mentionQuery == query && s.mentionTokenStart >= 0 {
		return
	}
	sources := s.mentionProviderSources()
	if len(sources) == 0 {
		return
	}
	s.startMentionQuery(sources, query, start)
}

// startMentionQuery replaces whatever results were cached and asks every declared
// provider in one round: one request token, one shared cancel set (the parent
// context), and the existing per-query timeout. Replies merge into rows that carry
// their own source as they arrive, up to the shared overall cap.
func (s *appState) startMentionQuery(sources []mentionProviderSource, query string, start int) {
	s.cancelMentionQuery()
	s.mentionRequest++
	request := s.mentionRequest
	s.mentionQuery = query
	s.mentionTokenStart = start
	s.mentionResults = nil
	s.mentionSelection = 0
	parent, cancel := context.WithCancel(context.Background())
	s.mentionCancel = cancel
	workspace := s.plugins.pluginWorkspaceRoot()
	trimmed := truncateRunes(query, maxPluginMentionQueryRunes)
	for _, source := range sources {
		entry := source.entry
		pluginID, providerID, command := entry.ID, source.contribution.ID, source.contribution.Command
		go func() {
			ctx, cancelOne := context.WithTimeout(parent, pluginMentionQueryTimeout)
			defer cancelOne()
			raw, err := s.plugins.invokeCommand(ctx, entry, command, map[string]any{
				"query":     trimmed,
				"workspace": workspace,
			})
			items, decodeErr := decodePluginQueryItems(raw)
			s.postUI(func() {
				// A newer query or a closed window owns the menu now, so this reply
				// is stale and dropped without touching the shared cancel.
				if request != s.mentionRequest {
					return
				}
				if decodeErr != nil || err != nil {
					return
				}
				// The plugin was disabled or removed while the query ran, or no
				// longer declares this provider, so its results are dropped rather
				// than shown for a provider that is gone.
				current, ok := s.pluginEntryByID(pluginID)
				if !ok || pluginQueryContributionByID(current.Manifest, providerID) == nil {
					return
				}
				for _, item := range items {
					if len(s.mentionResults) >= maxPluginQueryItems {
						break
					}
					s.mentionResults = append(s.mentionResults, mentionResult{
						pluginID:   pluginID,
						pluginName: current.Name,
						providerID: providerID,
						item:       item,
					})
				}
			})
		}()
	}
}
