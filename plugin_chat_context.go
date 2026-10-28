package main

import (
	"strings"
	"unicode/utf8"
)

const (
	maxPluginChatTextBytes    = 64 << 10
	maxPluginChatContextRunes = 256
	maxPluginChatLabelRunes   = 120
	tableContextPrefix        = "[byop-table-context:"
)

type pluginOpenChatRequest struct {
	Text      string `json:"text"`
	ContextID string `json:"contextId"`
	Label     string `json:"label"`
}

type tableContextAttachment struct {
	ContextID string
	Label     string
}

func tableContextPrompt(userText string, attachment *tableContextAttachment) string {
	if attachment == nil {
		return userText
	}
	return userText + "\n\n" + attachment.Label + "\n" +
		tableContextPrefix + attachment.ContextID + "]"
}

func displayTableContextPrompt(prompt string) string {
	lines := strings.Split(prompt, "\n")
	if len(lines) < 2 {
		return prompt
	}
	last := strings.TrimSpace(lines[len(lines)-1])
	if !strings.HasPrefix(last, tableContextPrefix) || !strings.HasSuffix(last, "]") ||
		len(last) <= len(tableContextPrefix)+1 {
		return prompt
	}
	return strings.TrimRight(strings.Join(lines[:len(lines)-1], "\n"), "\n")
}

func displayTableContextPrompts(prompts []string) []string {
	display := make([]string, len(prompts))
	for index, prompt := range prompts {
		display[index] = displayTableContextPrompt(prompt)
	}
	return display
}

func cloneTableContext(attachment *tableContextAttachment) *tableContextAttachment {
	if attachment == nil {
		return nil
	}
	copy := *attachment
	return &copy
}

func sameTableContext(left, right *tableContextAttachment) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.ContextID == right.ContextID && left.Label == right.Label
}

func validPluginChatText(value string) bool {
	return utf8.ValidString(value) &&
		len(value) <= maxPluginChatTextBytes &&
		!strings.ContainsRune(value, '\x00')
}

func validPluginChatContextID(value string) bool {
	return utf8.ValidString(value) &&
		utf8.RuneCountInString(value) <= maxPluginChatContextRunes &&
		!pluginRequiredTextInvalid(value, maxPluginChatContextRunes)
}

func validPluginChatLabel(value string) bool {
	return utf8.ValidString(value) &&
		utf8.RuneCountInString(value) <= maxPluginChatLabelRunes &&
		!pluginRequiredTextInvalid(value, maxPluginChatLabelRunes)
}

func (s *appState) openPluginChat(request pluginOpenChatRequest) {
	s.setPage(pageChat)
	s.composerDraft = strings.TrimSpace(request.Text)
	s.composerTableContext = &tableContextAttachment{
		ContextID: strings.TrimSpace(request.ContextID),
		Label:     strings.TrimSpace(request.Label),
	}
}

func (s *appState) clearTableContextIfMatches(attachment *tableContextAttachment) {
	if sameTableContext(s.composerTableContext, attachment) {
		s.composerTableContext = nil
	}
}
