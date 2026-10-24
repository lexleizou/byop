package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/components/chat"
	lu "github.com/oneo-me/onevra/source/icons/lucide"
)

const (
	followUpStyleGhost followUpStyle = "ghost"
	followUpStyleCard  followUpStyle = "card"

	followUpContextRunes = 3000
	followUpOutputRunes  = 120
	followUpTimeout      = 20 * time.Second
	followUpInstruction  = "Write exactly one short next message the user is likely to send, in the same language as the user's message, under 80 characters, with no quotes, explanation, or prefix."
)

type followUpStyle string

type followUpRunner func(ctx context.Context, path, cwd string, args []string, stdin string) (string, error)

func normalizeFollowUpStyle(value string) followUpStyle {
	switch followUpStyle(strings.ToLower(strings.TrimSpace(value))) {
	case followUpStyleCard:
		return followUpStyleCard
	default:
		return followUpStyleGhost
	}
}

func (s *appState) currentFollowUpStyle() followUpStyle {
	return normalizeFollowUpStyle(s.preferences.FollowUpStyle)
}

func (s *appState) setFollowUpStyle(style followUpStyle) {
	style = normalizeFollowUpStyle(string(style))
	s.preferences.FollowUpStyle = string(style)
	if err := s.persistPreferences(); err != nil {
		s.showNotice(text("noticePreferenceSaveFollowUp"))
	}
}

func followUpPiArgs(provider, id string) []string {
	args := []string{
		"--print",
		"--no-session",
		"--no-tools",
		"--no-extensions",
		"--no-skills",
		"--no-context-files",
		"--no-prompt-templates",
		"--no-mcp",
		"--thinking", "off",
	}
	provider = strings.TrimSpace(provider)
	id = strings.TrimSpace(id)
	if provider != "" && id != "" {
		args = append(args, "--model", provider+"/"+id)
	}
	return args
}

func buildFollowUpInput(prompt, reply string) string {
	return "User message:\n" + truncateFollowUpHead(strings.TrimSpace(prompt), followUpContextRunes) +
		"\n\nAssistant reply:\n" + truncateFollowUpTail(strings.TrimSpace(reply), followUpContextRunes) +
		"\n\nTask:\n" + followUpInstruction
}

func truncateFollowUpHead(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

func truncateFollowUpTail(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[len(runes)-limit:])
}

func sanitizeFollowUpSuggestion(output string) string {
	var line string
	for _, candidate := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(candidate) != "" {
			line = strings.TrimSpace(candidate)
			break
		}
	}
	if line == "" {
		return ""
	}
	for {
		previous := line
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "-*•")
		line = strings.TrimSpace(line)
		line = trimOrderedBullet(line)
		line = trimSuggestionLabel(line)
		line = strings.TrimSpace(line)
		line = strings.Trim(line, "\"'`“”‘’")
		line = strings.TrimSpace(line)
		if line == previous {
			break
		}
	}
	line = strings.Join(strings.Fields(line), " ")
	if line == "" {
		return ""
	}
	runes := []rune(line)
	if len(runes) > followUpOutputRunes {
		line = string(runes[:followUpOutputRunes])
	}
	return line
}

func trimOrderedBullet(line string) string {
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(line) || (line[i] != '.' && line[i] != ')') {
		return line
	}
	i++
	if i < len(line) && line[i] == ' ' {
		i++
	}
	return strings.TrimSpace(line[i:])
}

func trimSuggestionLabel(line string) string {
	lower := strings.ToLower(line)
	for _, label := range []string{"suggestion", "suggested next", "next message", "follow-up"} {
		if !strings.HasPrefix(lower, label) {
			continue
		}
		rest := line[len(label):]
		rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
		if strings.HasPrefix(rest, ":") || strings.HasPrefix(rest, "-") {
			return strings.TrimSpace(rest[1:])
		}
	}
	return line
}

func defaultFollowUpRunner(ctx context.Context, path, cwd string, args []string, stdin string) (string, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = cwd
	cmd.Env = piProcessEnv(path, os.Environ())
	cmd.Stdin = strings.NewReader(stdin)
	cmd.WaitDelay = 2 * time.Second
	output, err := cmd.Output()
	return string(output), err
}

func (s *appState) resetFollowUpSuggestion() {
	s.followUpGeneration++
	if s.followUpCancel != nil {
		s.followUpCancel()
		s.followUpCancel = nil
	}
	s.followUpSuggestion = ""
	s.followUpAdoptionPending = false
	s.followUpAdoptionFocus = false
}

func (s *appState) followUpSuggestionVisible() bool {
	return !s.closed.Load() &&
		s.piConn == piConnected &&
		strings.TrimSpace(s.followUpSuggestion) != "" &&
		strings.TrimSpace(s.composerDraft) == "" &&
		!s.runActive() &&
		!s.compacting &&
		!s.retrying &&
		!s.piSessionBusy &&
		!s.thinkingCommitInFlight
}

func (s *appState) adoptFollowUpSuggestion(focus bool) bool {
	if !s.followUpSuggestionVisible() {
		return false
	}
	s.composerDraft = s.followUpSuggestion
	s.followUpAdoptionPending = true
	s.followUpAdoptionFocus = focus
	return true
}

func (s *appState) applyPendingFollowUpAdoption(input ui.Element) {
	if !s.followUpAdoptionPending {
		return
	}
	caret := utf8.RuneCountInString(s.composerDraft)
	input.SetTextSelection(caret, caret)
	if s.followUpAdoptionFocus {
		input.Focus()
	}
	s.followUpAdoptionPending = false
	s.followUpAdoptionFocus = false
}

func (s *appState) dismissFollowUpSuggestion() {
	s.resetFollowUpSuggestion()
}

func (s *appState) handleFollowUpComposerInput(ev ui.InputEvent, composing bool) bool {
	if composing || ev.Kind != ui.InputKeyDown || ev.Mods != 0 || !s.followUpSuggestionVisible() {
		return false
	}
	switch ev.Key {
	case ui.KeyTab:
		return s.adoptFollowUpSuggestion(false)
	case ui.KeyEscape:
		s.dismissFollowUpSuggestion()
		return true
	case ui.KeyEnter:
		// Onevra consumes plain Enter before the multiline editor inserts a
		// newline. Preserve that behavior while an empty suggestion is shown.
		return true
	default:
		return false
	}
}

func (s *appState) startFollowUpSuggestion(prompt, reply string) {
	s.resetFollowUpSuggestion()
	if s.closed.Load() || s.piConn != piConnected || followUpConnectionDisabled() {
		return
	}
	prompt = strings.TrimSpace(prompt)
	reply = strings.TrimSpace(reply)
	if prompt == "" || reply == "" {
		return
	}
	path := strings.TrimSpace(s.piPath)
	if path == "" && s.pi != nil {
		path = strings.TrimSpace(s.pi.path)
	}
	if path == "" {
		return
	}
	cwd, err := s.sessionWorkspaceCwd()
	if err != nil {
		return
	}
	runner := s.followUpRunner
	if runner == nil {
		runner = defaultFollowUpRunner
	}
	args := followUpPiArgs(s.piModelProvider, s.piModelID)
	input := buildFollowUpInput(prompt, reply)
	ctx, cancel := context.WithTimeout(context.Background(), followUpTimeout)
	s.followUpGeneration++
	generation := s.followUpGeneration
	s.followUpCancel = cancel
	sessionID := s.piSessionID
	sessionFile := s.piSessionFile
	if s.window == nil {
		output, runErr := runner(ctx, path, cwd, args, input)
		cancel()
		suggestion := ""
		if runErr == nil {
			suggestion = sanitizeFollowUpSuggestion(output)
		}
		s.applyFollowUpSuggestionResult(generation, sessionID, sessionFile, suggestion, runErr)
		return
	}
	go func() {
		defer cancel()
		output, runErr := runner(ctx, path, cwd, args, input)
		suggestion := ""
		if runErr == nil {
			suggestion = sanitizeFollowUpSuggestion(output)
		}
		apply := func() {
			s.applyFollowUpSuggestionResult(generation, sessionID, sessionFile, suggestion, runErr)
		}
		s.postUI(apply)
	}()
}

func (s *appState) applyFollowUpSuggestionResult(generation uint64, sessionID, sessionFile, suggestion string, runErr error) bool {
	if generation != s.followUpGeneration || s.closed.Load() || s.piConn != piConnected {
		return false
	}
	if sessionID != "" && s.piSessionID != sessionID {
		return false
	}
	if sessionFile != "" && canonicalPiPathIfPresent(s.piSessionFile) != canonicalPiPathIfPresent(sessionFile) {
		return false
	}
	if s.runActive() {
		return false
	}
	if runErr != nil {
		return false
	}
	suggestion = sanitizeFollowUpSuggestion(suggestion)
	if suggestion == "" {
		return false
	}
	s.followUpSuggestion = suggestion
	s.followUpCancel = nil
	return true
}

func followUpConnectionDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("BYOP_PI_CONNECT"))) {
	case "0", "false", "off":
		return true
	default:
		return false
	}
}

func (s *appState) followUpSuggestionCard(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Key("follow-up-suggestion").FillWidth().MinWidth(0).Margin(0, 0, 8, 0).
		Padding(12, 14).Radius(12).Border(runtimeTokens.Dimension.Border.Value, t.Border).Background(t.Surface).Gap(8).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			components.Text(c, text("followUpSuggestedNext")).FontSize(fontSmall).FontWeight(600).TextColor(t.TextMuted)
			ui.Spacer(c)
			if chat.IconAction(c, text("followUpDismiss"), lu.X).Clicked() {
				s.dismissFollowUpSuggestion()
			}
		})
		components.Text(c, s.followUpSuggestion).FontSize(fontBody).LineHeight(1.45).TextColor(t.Text)
		ui.Row(c).Children(func() {
			use := chipV12(c, text("followUpUse"), chat.ChipOptions{Icon: lu.ArrowUpRight})
			if use.Clicked() {
				s.adoptFollowUpSuggestion(true)
			}
		})
	})
}
