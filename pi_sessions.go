package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	piSessionListLimit      = 50
	piSessionScanLineLimit  = 1 << 20
	piSessionTitleLimit     = 80
	piSessionLargeFileLimit = 64 << 20
)

type piSessionSummary struct {
	Path         string
	ID           string
	Name         string
	Title        string
	Cwd          string
	Created      time.Time
	Updated      time.Time
	MessageCount int
}

type piSessionHeader struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Cwd       string `json:"cwd"`
}

type piSessionEntryEnvelope struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Message json.RawMessage `json:"message"`
}

func piEphemeralSessions() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("BYOP_PI_EPHEMERAL"))) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

func piWorkspaceCwd(savedWorkspace string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working folder: %w", err)
	}
	if configured := strings.TrimSpace(os.Getenv("BYOP_WORKSPACE")); configured != "" {
		cwd = configured
	} else if saved := strings.TrimSpace(savedWorkspace); saved != "" {
		if info, statErr := os.Stat(saved); statErr == nil && info.IsDir() {
			cwd = saved
		}
	}
	return canonicalPiPath(workspaceRootFallback(cwd)), nil
}

// workspaceRootFallback maps the filesystem root (the cwd of apps launched
// from Finder or the Dock) to the user's home folder.
func workspaceRootFallback(cwd string) string {
	if cwd == "" || filepath.Dir(cwd) != cwd {
		return cwd
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return cwd
	}
	if info, statErr := os.Stat(home); statErr != nil || !info.IsDir() {
		return cwd
	}
	return home
}

func piSessionRoot() (string, bool, error) {
	if configured := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_SESSION_DIR")); configured != "" {
		root, err := expandPiHomePath(configured)
		if err != nil {
			return "", false, err
		}
		return root, true, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, fmt.Errorf("resolve Pi session root: %w", err)
	}
	agentDir := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_DIR"))
	if agentDir == "" {
		agentDir = filepath.Join(home, ".pi", "agent")
	} else {
		agentDir, err = expandPiHomePath(agentDir)
		if err != nil {
			return "", false, err
		}
	}
	return filepath.Join(agentDir, "sessions"), false, nil
}

func expandPiHomePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "~" || strings.HasPrefix(value, "~/") || strings.HasPrefix(value, "~\\") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if value == "~" {
			value = home
		} else {
			value = filepath.Join(home, value[2:])
		}
	}
	return canonicalPiPath(value), nil
}

func canonicalPiPath(value string) string {
	if absolute, err := filepath.Abs(value); err == nil {
		value = absolute
	}
	if resolved, err := filepath.EvalSymlinks(value); err == nil {
		value = resolved
	}
	return filepath.Clean(value)
}

func piSessionDirectory(root, cwd string) string {
	encoded := cwd
	volume := filepath.VolumeName(encoded)
	encoded = strings.TrimPrefix(encoded, volume)
	encoded = strings.TrimLeft(encoded, `/\`)
	encoded = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':':
			return '-'
		default:
			return r
		}
	}, encoded)
	return filepath.Join(root, "--"+encoded+"--")
}

func piSessionsForWorkspace(cwd string) ([]piSessionSummary, error) {
	root, flat, err := piSessionRoot()
	if err != nil {
		return nil, err
	}
	if flat {
		return discoverPiSessions(root, cwd, true, piSessionListLimit)
	}
	return discoverPiSessions(piSessionDirectory(root, cwd), cwd, false, piSessionListLimit)
}

type piSessionCandidate struct {
	path string
	info os.FileInfo
}

func discoverPiSessions(dir, expectedCwd string, filterCwd bool, limit int) ([]piSessionSummary, error) {
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err == nil {
		dir = resolvedDir
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("resolve Pi session directory: %w", err)
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []piSessionSummary{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Pi sessions: %w", err)
	}
	candidates := make([]piSessionCandidate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".jsonl") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		candidates = append(candidates, piSessionCandidate{path: path, info: info})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].info.ModTime().After(candidates[j].info.ModTime())
	})
	expectedCwd = canonicalPiPath(expectedCwd)
	sessions := make([]piSessionSummary, 0, min(len(candidates), limit))
	for _, candidate := range candidates {
		if filterCwd {
			header, err := readPiSessionHeader(candidate.path)
			if err != nil || header.Cwd == "" || canonicalPiPath(header.Cwd) != expectedCwd {
				continue
			}
		}
		summary, err := scanPiSession(candidate.path, candidate.info)
		if err != nil {
			continue
		}
		sessions = append(sessions, summary)
		if limit > 0 && len(sessions) >= limit {
			break
		}
	}
	return sessions, nil
}

func readPiSessionHeader(path string) (piSessionHeader, error) {
	file, err := os.Open(path)
	if err != nil {
		return piSessionHeader{}, err
	}
	defer file.Close()

	line, tooLong, err := readBoundedJSONLine(bufio.NewReaderSize(file, 64*1024), piSessionScanLineLimit)
	if err != nil && !errors.Is(err, io.EOF) {
		return piSessionHeader{}, err
	}
	if tooLong || len(line) == 0 {
		return piSessionHeader{}, errors.New("invalid Pi session header")
	}
	var header piSessionHeader
	if err := json.Unmarshal(line, &header); err != nil {
		return piSessionHeader{}, err
	}
	if header.Type != "session" {
		return piSessionHeader{}, errors.New("invalid Pi session header type")
	}
	return header, nil
}

func scanPiSession(path string, info os.FileInfo) (piSessionSummary, error) {
	summary := piSessionSummary{
		Path:    path,
		ID:      strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Updated: info.ModTime(),
	}
	if info.Size() > piSessionLargeFileLimit {
		summary.Title = "Untitled session"
		return summary, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return piSessionSummary{}, err
	}
	defer file.Close()

	reader := bufio.NewReaderSize(file, 64*1024)
	var firstName string
	lineNumber := 0
	for {
		line, tooLong, err := readBoundedJSONLine(reader, piSessionScanLineLimit)
		if len(line) > 0 && !tooLong {
			lineNumber++
			var header piSessionHeader
			if lineNumber == 1 && json.Unmarshal(line, &header) == nil && header.Type == "session" {
				summary.ID = fallback(header.ID, summary.ID)
				summary.Cwd = header.Cwd
				if created, parseErr := time.Parse(time.RFC3339Nano, header.Timestamp); parseErr == nil {
					summary.Created = created
				}
			} else {
				var entry piSessionEntryEnvelope
				if json.Unmarshal(line, &entry) == nil {
					switch entry.Type {
					case "message":
						if piMessageIsConversation(entry.Message) {
							summary.MessageCount++
						}
						if firstName == "" {
							firstName = piUserMessageTitle(entry.Message)
						}
					case "session_info":
						if name := strings.TrimSpace(entry.Name); name != "" {
							summary.Name = piSessionTitle(name)
							summary.Title = summary.Name
						}
					}
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return piSessionSummary{}, err
		}
	}
	if summary.Title == "" {
		summary.Title = firstName
	}
	if summary.Title == "" {
		summary.Title = "Untitled session"
	}
	return summary, nil
}

func readBoundedJSONLine(reader *bufio.Reader, limit int) ([]byte, bool, error) {
	var line []byte
	tooLong := false
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(fragment) > 0 {
			if !tooLong {
				if len(line)+len(fragment) > limit+1 {
					line = nil
					tooLong = true
				} else {
					line = append(line, fragment...)
				}
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			if len(line) > 0 {
				line = trimJSONLine(line)
			}
			return line, tooLong, err
		}
		if tooLong {
			return nil, true, nil
		}
		return trimJSONLine(line), false, nil
	}
}

func trimJSONLine(line []byte) []byte {
	line = bytes.TrimSpace(line)
	if len(line) > piSessionScanLineLimit {
		return nil
	}
	return line
}

func piMessageIsConversation(raw json.RawMessage) bool {
	var message struct {
		Role string `json:"role"`
	}
	if json.Unmarshal(raw, &message) != nil {
		return false
	}
	switch message.Role {
	case "user", "assistant", "toolResult":
		return true
	default:
		return false
	}
}

func piUserMessageTitle(raw json.RawMessage) string {
	var message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &message) != nil || message.Role != "user" {
		return ""
	}
	text := displayTableContextPrompt(contentText(message.Content))
	if strings.TrimSpace(text) == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > piSessionTitleLimit {
		runes = runes[:piSessionTitleLimit]
	}
	return string(runes)
}

func piSessionTitle(text string) string {
	text = strings.Join(strings.FieldsFunc(text, unicode.IsSpace), " ")
	if utf8.RuneCountInString(text) <= piSessionTitleLimit {
		return text
	}
	runes := []rune(text)
	return string(runes[:piSessionTitleLimit])
}

type piMessageContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	ToolName  string          `json:"toolName"`
}

type piAgentMessage struct {
	Role         string                  `json:"role"`
	Content      json.RawMessage         `json:"content"`
	Timestamp    int64                   `json:"timestamp"`
	ToolCallID   string                  `json:"toolCallId"`
	ToolName     string                  `json:"toolName"`
	IsError      bool                    `json:"isError"`
	StopReason   string                  `json:"stopReason"`
	ErrorMessage string                  `json:"errorMessage"`
	Blocks       []piMessageContentBlock `json:"-"`
}

type chatToolLocation struct {
	message int
	block   int
}

func rebuildChatFromPiMessages(rawMessages []json.RawMessage) ([]chatMessage, error) {
	chat := make([]chatMessage, 0, len(rawMessages))
	toolLocations := make(map[string]chatToolLocation)
	var lastAssistant *chatMessage

	for _, raw := range rawMessages {
		var message piAgentMessage
		if err := json.Unmarshal(raw, &message); err != nil {
			return nil, fmt.Errorf("parse restored Pi message: %w", err)
		}
		timestamp := piMessageTime(message.Timestamp)
		switch message.Role {
		case "user":
			userText := displayTableContextPrompt(contentText(message.Content))
			if strings.TrimSpace(userText) == "" {
				continue
			}
			chat = append(chat, chatMessage{
				role:   chatUser,
				status: text("chatStatusSent"),
				blocks: []chatBlock{{kind: chatBlockText, text: userText}},
			})
			lastAssistant = nil
		case "assistant":
			blocks, err := piAssistantBlocks(message.Content)
			if err != nil {
				return nil, err
			}
			status := "Complete"
			switch message.StopReason {
			case "error":
				status = "failed"
				errorText := fallback(strings.TrimSpace(message.ErrorMessage), text("assistantErrorFallback"))
				blocks = append(blocks, chatBlock{kind: chatBlockText, text: errorText})
			case "aborted":
				status = "Stopped"
				for blockIndex := range blocks {
					if blocks[blockIndex].kind == chatBlockTool {
						blocks[blockIndex].tool.status = toolCallCancelled
					}
				}
			}
			if len(blocks) == 0 {
				if message.StopReason != "aborted" {
					continue
				}
				blocks = append(blocks, chatBlock{kind: chatBlockText, text: text("assistantStoppedFallback")})
			}
			chat = append(chat, chatMessage{
				role:      chatAssistant,
				status:    status,
				blocks:    blocks,
				startedAt: timestamp,
				endedAt:   timestamp,
			})
			lastAssistant = &chat[len(chat)-1]
			for blockIndex := range lastAssistant.blocks {
				block := &lastAssistant.blocks[blockIndex]
				if block.kind == chatBlockTool && block.tool.id != "" {
					toolLocations[block.tool.id] = chatToolLocation{
						message: len(chat) - 1,
						block:   blockIndex,
					}
				}
			}
		case "toolResult":
			location, exists := toolLocations[message.ToolCallID]
			if !exists || location.message >= len(chat) {
				continue
			}
			tool := &chat[location.message].blocks[location.block].tool
			if message.ToolName != "" {
				tool.name = message.ToolName
			}
			tool.result = truncateToolText(piMessageContentText(message.Content), 1200)
			tool.status = toolCallCompleted
			if message.IsError {
				tool.status = toolCallFailed
			}
			tool.expanded = false
		case "system", "custom", "bashExecution", "branchSummary", "compactionSummary":
			continue
		default:
			continue
		}
	}
	return chat, nil
}

func piAssistantBlocks(raw json.RawMessage) ([]chatBlock, error) {
	var blocks []piMessageContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		var text string
		if json.Unmarshal(raw, &text) == nil {
			if strings.TrimSpace(text) == "" {
				return nil, nil
			}
			return []chatBlock{{kind: chatBlockText, text: text}}, nil
		}
		return nil, fmt.Errorf("parse restored assistant blocks: %w", err)
	}
	result := make([]chatBlock, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if strings.TrimSpace(block.Text) != "" {
				result = append(result, chatBlock{kind: chatBlockText, text: block.Text})
			}
		case "toolCall":
			result = append(result, chatBlock{
				kind: chatBlockTool,
				tool: toolCallView{
					id:        block.ID,
					name:      fallback(block.Name, "tool"),
					arguments: formatToolArguments(block.Arguments),
					status:    toolCallInterrupted,
				},
			})
		}
	}
	return result, nil
}

func piMessageContentText(raw json.RawMessage) string {
	if text := contentText(raw); strings.TrimSpace(text) != "" {
		return text
	}
	var blocks []piMessageContentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func piMessageTime(milliseconds int64) time.Time {
	if milliseconds <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(milliseconds)
}
