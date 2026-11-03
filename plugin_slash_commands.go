package main

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// P25 slashCommands consumer. A draft that is exactly "/token" — a leading
// slash with no second slash and no space — offers the commands the user can
// pick: the plugin contributions, and the commands the connected Pi reports.
// Picking one either runs the plugin's command through the leased invoke path
// or inserts the prompt template, so a contribution never sends anything by
// itself. The list is derived state: it is dropped the moment the draft stops
// being a slash token, so it never rewrites what the user is typing.

const (
	// maxPluginSlashSuggestions caps the visible list; the plugin names come
	// first, then Pi commands, so a long list never hides the contributions.
	maxPluginSlashSuggestions = 12
	// pluginSlashCommandTimeout bounds get_commands, which Pi answers from
	// already-loaded extensions rather than by starting anything.
	pluginSlashCommandTimeout = 3 * time.Second
)

// pluginSlashToken splits a draft into (token, ok). Only "/x" forms qualify;
// "/x y" is prose with a path or a leading slash, and "//x" is a comment.
func pluginSlashToken(draft string) (string, bool) {
	if len(draft) == 0 || draft[0] != '/' {
		return "", false
	}
	token := draft[1:]
	if strings.ContainsAny(token, " \t\n\r/") {
		return "", false
	}
	return strings.ToLower(token), true
}

var pluginSlashNamePattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

// pluginSlashSuggestion is one row of the list. Kind says which section it
// came from, because plugin rows run or insert while Pi rows only insert the
// command name the user can complete with arguments.
type pluginSlashSuggestion struct {
	Name        string
	Description string
	Kind        string // "plugin-command", "plugin-prompt", "pi-extension", "pi-prompt", "pi-skill"
	Entry       pluginRegistryEntry
	CommandID   string
	Prompt      string
}

// slashSuggestionsFor builds the list for one token. Plugin contributions win
// a name collision with a Pi command, and the result stays under the cap.
func (s *appState) slashSuggestionsFor(token string) []pluginSlashSuggestion {
	var suggestions []pluginSlashSuggestion
	for _, entry := range s.pluginRegistrySnapshot().Entries {
		if entry.Status != pluginRegistered || entry.Manifest == nil {
			continue
		}
		for _, contribution := range entry.Manifest.Contributes.SlashCommands {
			if !strings.HasPrefix(strings.ToLower(contribution.Name), token) {
				continue
			}
			kind := "plugin-prompt"
			if contribution.Command != "" {
				kind = "plugin-command"
			}
			suggestions = append(suggestions, pluginSlashSuggestion{
				Name:        contribution.Name,
				Description: fallback(contribution.Description, contribution.Title),
				Kind:        kind,
				Entry:       entry,
				CommandID:   contribution.Command,
				Prompt:      contribution.Prompt,
			})
		}
	}
	claimed := make(map[string]bool, len(suggestions))
	for _, suggestion := range suggestions {
		claimed[suggestion.Name] = true
	}
	for _, command := range s.piCommands {
		// A Pi command belongs to the running process, so it only counts while
		// that process is connected. A reconnect clears the list, because the
		// new process may have launched different extensions.
		name := strings.ToLower(command.Name)
		if !pluginSlashNamePattern.MatchString(name) || claimed[name] ||
			!strings.HasPrefix(name, token) {
			continue
		}
		kind := command.Source
		if kind != "prompt" && kind != "skill" {
			kind = "extension"
		}
		suggestions = append(suggestions, pluginSlashSuggestion{
			Name:        name,
			Description: command.Description,
			Kind:        "pi-" + kind,
		})
	}
	sort.SliceStable(suggestions, func(i, j int) bool {
		if suggestions[i].Name == suggestions[j].Name {
			return suggestions[i].Kind < suggestions[j].Kind
		}
		return suggestions[i].Name < suggestions[j].Name
	})
	if len(suggestions) > maxPluginSlashSuggestions {
		suggestions = suggestions[:maxPluginSlashSuggestions]
	}
	return suggestions
}

// slashSuggestions returns the list for the current draft, or nil when the
// draft is not a slash token. The selected row is clamped here so a shrinking
// list can never point past its end.
func (s *appState) slashSuggestions() []pluginSlashSuggestion {
	// Typing always starts a fresh menu: the selection returns to the first
	// row and an explicit Escape dismissal applies only to the token it was
	// raised on.
	if s.composerDraft != s.pluginSlashLastDraft {
		s.pluginSlashLastDraft = s.composerDraft
		s.pluginSlashSelection = 0
		s.pluginSlashDismissed = false
	}
	token, ok := pluginSlashToken(s.composerDraft)
	if !ok {
		return nil
	}
	suggestions := s.slashSuggestionsFor(token)
	if len(suggestions) == 0 {
		return nil
	}
	if s.pluginSlashSelection >= len(suggestions) {
		s.pluginSlashSelection = 0
	}
	return suggestions
}

// chooseSlashSuggestion runs one picked row and clears the menu state. A
// plugin command keeps the leased command path the plugins page already uses
// and empties the draft; a prompt template or Pi command becomes the draft so
// the user can complete {args} or the arguments before sending.
func (s *appState) chooseSlashSuggestion(suggestion pluginSlashSuggestion) {
	s.pluginSlashSelection = 0
	switch suggestion.Kind {
	case "plugin-command":
		s.composerDraft = ""
		if command, ok := pluginCommandByID(suggestion.Entry.Manifest, suggestion.CommandID); ok {
			s.runPluginCommand(suggestion.Entry, command)
		}
	default:
		if suggestion.Prompt != "" {
			s.composerDraft = suggestion.Prompt
		} else {
			s.composerDraft = "/" + suggestion.Name + " "
		}
		s.composerSlashCaretPending = true
	}
}

// applyPendingComposerSlashCaret puts the caret at the end of a draft the menu
// inserted, so the user can complete {args} or the arguments without moving
// the caret by hand. It follows the same pattern the follow-up suggestion uses.
func (s *appState) applyPendingComposerSlashCaret(input ui.Element) {
	if !s.composerSlashCaretPending {
		return
	}
	caret := len([]rune(s.composerDraft))
	input.SetTextSelection(caret, caret)
	input.Focus()
	s.composerSlashCaretPending = false
}

// slashMenuRows reports the slash list for the current draft together with the
// selected row, in the shape the shared composer menu renderer draws. It returns
// false when the draft is not a slash token or the user dismissed the list.
func (s *appState) slashMenuRows() ([]composerMenuItem, int, bool) {
	suggestions := s.slashSuggestions()
	if len(suggestions) == 0 || s.pluginSlashDismissed {
		return nil, 0, false
	}
	return slashMenuRows(suggestions), s.pluginSlashSelection, true
}

// slashMenuRows turns suggestions into menu rows. The title carries the leading
// slash so a row reads as the text the user is completing.
func slashMenuRows(suggestions []pluginSlashSuggestion) []composerMenuItem {
	rows := make([]composerMenuItem, 0, len(suggestions))
	for _, suggestion := range suggestions {
		rows = append(rows, composerMenuItem{
			Key:    "slash-" + suggestion.Kind + "-" + suggestion.Name,
			Title:  "/" + suggestion.Name,
			Detail: suggestion.Description,
			Tag:    slashKindLabel(suggestion.Kind),
		})
	}
	return rows
}

// chooseSlashRow picks the selected suggestion and closes the list.
func (s *appState) chooseSlashRow() {
	suggestions := s.slashSuggestions()
	if len(suggestions) == 0 {
		return
	}
	selection := s.pluginSlashSelection
	if selection >= len(suggestions) {
		selection = 0
	}
	s.chooseSlashSuggestion(suggestions[selection])
	s.pluginSlashDismissed = false
}

// slashKindLabel names the section a row came from. Plugin rows say plugin or
// prompt, Pi rows say extension, prompt, or skill, which follows Pi's own
// get_commands source field.
func slashKindLabel(kind string) string {
	switch kind {
	case "plugin-command":
		return text("slashKindPlugin")
	case "plugin-prompt":
		return text("slashKindPluginPrompt")
	case "pi-extension":
		return text("slashKindPiExtension")
	case "pi-prompt":
		return text("slashKindPiPrompt")
	case "pi-skill":
		return text("slashKindPiSkill")
	default:
		return ""
	}
}

// piCommand is one entry of Pi's get_commands result.
type piCommand struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

// refreshPiCommands asks the connected Pi for its commands, which is the only
// way a Pi-side extension, prompt, or skill becomes visible here. The result
// belongs to one Pi process, so a reconnect or a new process drops it before
// the old reply can be stored, and the menu simply shows fewer rows meanwhile.
func (s *appState) refreshPiCommands() {
	if s.pi == nil || s.piConn != piConnected {
		return
	}
	client, epoch := s.pi, s.piEpoch
	if s.piCommandsFetching {
		return
	}
	s.piCommandsFetching = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), pluginSlashCommandTimeout)
		defer cancel()
		commands, err := client.GetCommands(ctx)
		s.postUI(func() {
			// A replaced process, a disconnect, or a closed window means this
			// reply describes something the composer is no longer talking to, so
			// it is dropped before the fetch flag moves: clearing it here would
			// clear a newer request's state. The fetch flag belongs to the
			// request, so only its own reply may clear it.
			if s.pi != client || s.piEpoch != epoch {
				return
			}
			s.piCommandsFetching = false
			// A failure counts as one attempt for this process, so a Pi that
			// does not answer get_commands is not asked again on every frame.
			// The next epoch — a reconnect — is what allows a fresh attempt.
			s.piCommandsEpoch = epoch
			if err != nil || s.piConn != piConnected {
				s.piCommands = nil
				return
			}
			s.piCommands = commands
		})
	}()
}

// maybeRefreshPiCommands fetches Pi's commands once, at the moment the draft
// first looks like a slash token and nothing is cached for this Pi process.
// Calling it from the render frame only starts a task; no IO happens here.
func (s *appState) maybeRefreshPiCommands() {
	if _, ok := pluginSlashToken(s.composerDraft); !ok {
		return
	}
	// The list belongs to one Pi process, so an empty result for the current
	// epoch still counts as fetched and does not restart on every frame.
	if s.piCommandsEpoch == s.piEpoch || s.piCommandsFetching {
		return
	}
	s.refreshPiCommands()
}
