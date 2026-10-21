package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type piEventKind string

const (
	piEventResponse     piEventKind = "response"
	piEventRunStarted   piEventKind = "run_started"
	piEventText         piEventKind = "text"
	piEventMessage      piEventKind = "message"
	piEventToolStart    piEventKind = "tool_start"
	piEventToolUpdate   piEventKind = "tool_update"
	piEventToolEnd      piEventKind = "tool_end"
	piEventSettled      piEventKind = "settled"
	piEventQueueUpdate  piEventKind = "queue_update"
	piEventCompactStart piEventKind = "compaction_start"
	piEventCompactEnd   piEventKind = "compaction_end"
	piEventRetryStart   piEventKind = "auto_retry_start"
	piEventRetryEnd     piEventKind = "auto_retry_end"
	piEventAgentEnd     piEventKind = "agent_end"
	piEventExtensionErr piEventKind = "extension_error"
	piEventProcessExit  piEventKind = "process_exit"
	piEventParseError   piEventKind = "parse_error"
)

type piSessionState struct {
	SessionID           string `json:"sessionId"`
	SessionName         string `json:"sessionName"`
	IsStreaming         bool   `json:"isStreaming"`
	IsCompacting        bool   `json:"isCompacting"`
	PendingMessageCount int    `json:"pendingMessageCount"`
	MessageCount        int    `json:"messageCount"`
	ThinkingLevel       string `json:"thinkingLevel"`
	SteeringMode        string `json:"steeringMode"`
	FollowUpMode        string `json:"followUpMode"`
	AutoCompaction      bool   `json:"autoCompactionEnabled"`
	SessionFile         string `json:"sessionFile"`
	Model               struct {
		Provider      string   `json:"provider"`
		ID            string   `json:"id"`
		Name          string   `json:"name"`
		Reasoning     bool     `json:"reasoning"`
		ContextWindow int      `json:"contextWindow"`
		Input         []string `json:"input"`
	} `json:"model"`
}

type piModel struct {
	Provider      string
	ID            string
	Name          string
	Reasoning     bool
	ContextWindow int
	Input         []string
}

type piModelWire struct {
	Provider      string   `json:"provider"`
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Reasoning     bool     `json:"reasoning"`
	ContextWindow int      `json:"contextWindow"`
	Input         []string `json:"input"`
}

func (m piModelWire) model() piModel {
	return piModel{
		Provider:      m.Provider,
		ID:            m.ID,
		Name:          m.Name,
		Reasoning:     m.Reasoning,
		ContextWindow: m.ContextWindow,
		Input:         append([]string(nil), m.Input...),
	}
}

type piEvent struct {
	Kind           piEventKind
	Source         *piClient
	RequestID      string
	Command        string
	Success        bool
	Error          string
	Disposition    string
	Text           string
	ToolName       string
	ToolCallID     string
	ToolArgs       json.RawMessage
	ToolResult     json.RawMessage
	ToolPartial    json.RawMessage
	ToolIsError    bool
	ToolDurationMS int64
	Aborted        bool
	Session        *piSessionState
	StderrTail     string

	QueueSteering  []string
	QueueFollowUp  []string
	CompactReason  string
	CompactSummary string
	CompactBefore  int64
	CompactAfter   int64
	CompactError   string
	CompactAborted bool
	WillRetry      bool
	RetryAttempt   int
	RetryMax       int
	RetryDelayMS   int64
	RetryError     string
	RetrySucceeded bool
	ExtensionPath  string
	ExtensionEvent string
}

type piRPCResponse struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Command string          `json:"command"`
	Success bool            `json:"success"`
	Error   string          `json:"error"`
	Data    json.RawMessage `json:"data"`
}

type piClient struct {
	path   string
	cwd    string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	events func(piEvent)

	mu      sync.Mutex
	pending map[string]chan piRPCResponse
	nextID  atomic.Uint64
	closed  atomic.Bool

	stderrMu   sync.Mutex
	stderrTail string

	waitOnce sync.Once
	waitDone chan struct{}
	waitErr  error

	streamMu    sync.Mutex
	streamParts map[int]string
	streamOrder []int
}

func startPiClient(path, cwd string, events func(piEvent)) (*piClient, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("Pi CLI path is empty")
	}
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("resolve working folder: %w", err)
		}
	}
	if absolute, err := filepath.Abs(cwd); err == nil {
		cwd = absolute
	}
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}

	args := []string{"--mode", "rpc"}
	if piEphemeralSessions() {
		args = append(args, "--no-session")
	}
	cmd := exec.Command(path, args...)
	cmd.Dir = cwd
	cmd.Env = cleanPiEnv(os.Environ())

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open Pi stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open Pi stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("open Pi stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", path, err)
	}

	client := &piClient{
		path:        path,
		cwd:         cwd,
		cmd:         cmd,
		stdin:       stdin,
		events:      events,
		pending:     make(map[string]chan piRPCResponse),
		waitDone:    make(chan struct{}),
		streamParts: make(map[int]string),
	}

	var readers sync.WaitGroup
	readers.Add(2)
	go func() {
		defer readers.Done()
		client.readStdout(stdout)
	}()
	go func() {
		defer readers.Done()
		client.readStderr(stderr)
	}()
	go func() {
		readers.Wait()
		client.waitErr = client.cmd.Wait()
		close(client.waitDone)
		client.emit(piEvent{
			Kind:       piEventProcessExit,
			Error:      processExitError(client.waitErr),
			StderrTail: client.stderrSnapshot(),
		})
		client.failPending(errors.New("Pi RPC process exited"))
	}()

	return client, nil
}

func resolvePiPath() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("BYOP_PI_PATH")); configured != "" {
		if info, err := os.Stat(configured); err != nil {
			return "", fmt.Errorf("BYOP_PI_PATH: %w", err)
		} else if info.IsDir() {
			return "", fmt.Errorf("BYOP_PI_PATH points to a directory: %s", configured)
		}
		return filepath.Clean(configured), nil
	}
	path, err := exec.LookPath("pi")
	if err != nil {
		return "", errors.New("Pi CLI was not found. Install Pi separately or set BYOP_PI_PATH.")
	}
	return path, nil
}

func piCLIVersion(ctx context.Context, path string) string {
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = cleanPiEnv(os.Environ())
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	return strings.TrimPrefix(line, "v")
}

func (p *piClient) GetState(ctx context.Context) (*piSessionState, error) {
	data, err := p.request(ctx, map[string]any{"type": "get_state"})
	if err != nil {
		return nil, err
	}
	var state piSessionState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse Pi state: %w", err)
	}
	return &state, nil
}

func (p *piClient) NewSession(ctx context.Context) (bool, error) {
	data, err := p.request(ctx, map[string]any{"type": "new_session"})
	if err != nil {
		return false, err
	}
	var result struct {
		Cancelled bool `json:"cancelled"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return false, fmt.Errorf("parse new session response: %w", err)
	}
	return result.Cancelled, nil
}

func (p *piClient) SwitchSession(ctx context.Context, path string) (bool, error) {
	data, err := p.request(ctx, map[string]any{
		"type":        "switch_session",
		"sessionPath": path,
	})
	if err != nil {
		return false, err
	}
	var result struct {
		Cancelled bool `json:"cancelled"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return false, fmt.Errorf("parse switch session response: %w", err)
	}
	return result.Cancelled, nil
}

func (p *piClient) GetMessages(ctx context.Context) ([]json.RawMessage, error) {
	data, err := p.request(ctx, map[string]any{"type": "get_messages"})
	if err != nil {
		return nil, err
	}
	var result struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse Pi messages: %w", err)
	}
	return result.Messages, nil
}

func (p *piClient) GetAvailableModels(ctx context.Context) ([]piModel, error) {
	data, err := p.request(ctx, map[string]any{"type": "get_available_models"})
	if err != nil {
		return nil, err
	}
	return decodePiModels(data)
}

func decodePiModels(data json.RawMessage) ([]piModel, error) {
	var result struct {
		Models []piModelWire `json:"models"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse available models: %w", err)
	}
	models := make([]piModel, 0, len(result.Models))
	for _, model := range result.Models {
		models = append(models, model.model())
	}
	return models, nil
}

func (p *piClient) SetModel(ctx context.Context, provider, id string) (piModel, error) {
	data, err := p.request(ctx, map[string]any{
		"type":     "set_model",
		"provider": provider,
		"modelId":  id,
	})
	if err != nil {
		return piModel{}, err
	}
	var model piModelWire
	if err := json.Unmarshal(data, &model); err != nil {
		return piModel{}, fmt.Errorf("parse selected model: %w", err)
	}
	return model.model(), nil
}

func (p *piClient) GetAvailableThinkingLevels(ctx context.Context) ([]string, error) {
	data, err := p.request(ctx, map[string]any{"type": "get_available_thinking_levels"})
	if err != nil {
		return nil, err
	}
	var result struct {
		Levels []string `json:"levels"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse thinking levels: %w", err)
	}
	return append([]string(nil), result.Levels...), nil
}

func (p *piClient) SetThinkingLevel(ctx context.Context, level string) error {
	_, err := p.request(ctx, map[string]any{
		"type":  "set_thinking_level",
		"level": level,
	})
	return err
}

func (p *piClient) Prompt(message string) (string, error) {
	id := p.newRequestID("prompt")
	_, err := p.send(map[string]any{
		"id":      id,
		"type":    "prompt",
		"message": message,
	})
	return id, err
}

func (p *piClient) Compact(ctx context.Context, instructions string) error {
	payload := map[string]any{"type": "compact"}
	if strings.TrimSpace(instructions) != "" {
		payload["customInstructions"] = instructions
	}
	_, err := p.request(ctx, payload)
	return err
}

func (p *piClient) Stop(ctx context.Context, abortRetry bool) (string, error) {
	var retryErr error
	if abortRetry {
		_, retryErr = p.request(ctx, map[string]any{"type": "abort_retry"})
	}
	clearData, clearErr := p.request(ctx, map[string]any{"type": "clear_queue"})
	var clearNote string
	if clearErr == nil {
		var queue struct {
			Steering []string `json:"steering"`
			FollowUp []string `json:"followUp"`
		}
		if err := json.Unmarshal(clearData, &queue); err != nil {
			clearNote = "queue response could not be parsed"
		} else if len(queue.Steering)+len(queue.FollowUp) > 0 {
			clearNote = fmt.Sprintf("cleared %d queued message(s)", len(queue.Steering)+len(queue.FollowUp))
		}
	}

	_, abortErr := p.request(ctx, map[string]any{"type": "abort"})
	if retryErr == nil && abortRetry {
		clearNote = strings.TrimPrefix(clearNote+" · retry stopped", " · ")
	}
	switch {
	case clearErr != nil && abortErr != nil:
		return "", fmt.Errorf("clear queue failed: %v; abort failed: %w", clearErr, abortErr)
	case clearErr != nil:
		return "", fmt.Errorf("abort sent, but queue state is unknown because clear_queue failed: %w", clearErr)
	case abortErr != nil:
		return clearNote, fmt.Errorf("%s; abort failed: %w", fallback(clearNote, "queue cleared"), abortErr)
	case retryErr != nil:
		return clearNote, fmt.Errorf("%s; abort_retry failed: %w", fallback(clearNote, "queue cleared"), retryErr)
	default:
		return clearNote, nil
	}
}

func (p *piClient) Close() {
	if p == nil || p.closed.Swap(true) {
		return
	}
	_ = p.stdin.Close()
	go func() {
		select {
		case <-p.waitDone:
		case <-time.After(3 * time.Second):
			_ = p.cmd.Process.Kill()
		}
	}()
}

func (p *piClient) request(ctx context.Context, payload map[string]any) (json.RawMessage, error) {
	id := p.newRequestID(firstNonEmpty(stringValue(payload["type"]), "request"))
	payload["id"] = id
	response := make(chan piRPCResponse, 1)

	p.mu.Lock()
	if p.closed.Load() {
		p.mu.Unlock()
		return nil, errors.New("Pi RPC is closed")
	}
	p.pending[id] = response
	p.mu.Unlock()

	if _, err := p.send(payload); err != nil {
		p.dropPending(id)
		return nil, err
	}
	select {
	case <-ctx.Done():
		p.dropPending(id)
		return nil, ctx.Err()
	case result := <-response:
		if !result.Success {
			return nil, errors.New(fallback(result.Error, "Pi command failed"))
		}
		return result.Data, nil
	}
}

func (p *piClient) send(payload map[string]any) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed.Load() {
		return "", errors.New("Pi RPC is closed")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	if _, err := p.stdin.Write(data); err != nil {
		return "", fmt.Errorf("write Pi RPC: %w", err)
	}
	id, _ := payload["id"].(string)
	return id, nil
}

func (p *piClient) readStdout(r io.Reader) {
	reader := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			line = bytes.TrimSuffix(line, []byte{'\n'})
			line = bytes.TrimSuffix(line, []byte{'\r'})
			if len(bytes.TrimSpace(line)) > 0 {
				p.handleRecord(line)
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				p.emit(piEvent{Kind: piEventParseError, Error: "Pi stdout read failed: " + err.Error()})
			}
			return
		}
	}
}

func (p *piClient) readStderr(r io.Reader) {
	reader := bufio.NewReaderSize(r, 16*1024)
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			clean := strings.TrimSpace(stripANSI(line))
			if clean != "" && !strings.HasPrefix(clean, "Warning: The 'NO_COLOR' env is ignored") &&
				!strings.HasPrefix(clean, "(Use `node --trace-warnings") {
				p.stderrMu.Lock()
				p.stderrTail = truncate(clean, 500)
				p.stderrMu.Unlock()
			}
		}
		if err != nil {
			return
		}
	}
}

func (p *piClient) handleRecord(line []byte) {
	var record piRPCResponse
	if err := json.Unmarshal(line, &record); err != nil {
		p.emit(piEvent{Kind: piEventParseError, Error: "Invalid Pi JSONL: " + err.Error()})
		return
	}

	switch record.Type {
	case "response":
		p.handleResponse(record)
	case "agent_start":
		p.resetStream()
		p.emit(piEvent{Kind: piEventRunStarted})
	case "message_update":
		p.handleMessageUpdate(line)
	case "message_end":
		p.handleMessageEnd(line)
	case "tool_execution_start":
		var event struct {
			ToolCallID string          `json:"toolCallId"`
			ToolName   string          `json:"toolName"`
			Args       json.RawMessage `json:"args"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			p.emit(piEvent{Kind: piEventParseError, Error: "Invalid Pi tool start: " + err.Error()})
			return
		}
		p.emit(piEvent{
			Kind:       piEventToolStart,
			ToolCallID: event.ToolCallID,
			ToolName:   event.ToolName,
			ToolArgs:   event.Args,
		})
	case "tool_execution_update":
		var event struct {
			ToolCallID    string          `json:"toolCallId"`
			ToolName      string          `json:"toolName"`
			Args          json.RawMessage `json:"args"`
			PartialResult json.RawMessage `json:"partialResult"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			p.emit(piEvent{Kind: piEventParseError, Error: "Invalid Pi tool update: " + err.Error()})
			return
		}
		p.emit(piEvent{
			Kind:        piEventToolUpdate,
			ToolCallID:  event.ToolCallID,
			ToolName:    event.ToolName,
			ToolArgs:    event.Args,
			ToolPartial: event.PartialResult,
		})
	case "tool_execution_end":
		var event struct {
			ToolCallID string          `json:"toolCallId"`
			ToolName   string          `json:"toolName"`
			Result     json.RawMessage `json:"result"`
			IsError    bool            `json:"isError"`
			DurationMS int64           `json:"durationMs"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			p.emit(piEvent{Kind: piEventParseError, Error: "Invalid Pi tool end: " + err.Error()})
			return
		}
		p.emit(piEvent{
			Kind:           piEventToolEnd,
			ToolCallID:     event.ToolCallID,
			ToolName:       event.ToolName,
			ToolResult:     event.Result,
			ToolIsError:    event.IsError,
			ToolDurationMS: event.DurationMS,
		})
	case "agent_settled":
		var event struct {
			Aborted bool `json:"aborted"`
		}
		_ = json.Unmarshal(line, &event)
		p.emit(piEvent{Kind: piEventSettled, Aborted: event.Aborted})
	case "queue_update":
		var event struct {
			Steering []string `json:"steering"`
			FollowUp []string `json:"followUp"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			p.emit(piEvent{Kind: piEventParseError, Error: "Invalid Pi queue update: " + err.Error()})
			return
		}
		p.emit(piEvent{
			Kind:          piEventQueueUpdate,
			QueueSteering: event.Steering,
			QueueFollowUp: event.FollowUp,
		})
	case "compaction_start":
		var event struct {
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			p.emit(piEvent{Kind: piEventParseError, Error: "Invalid Pi compaction start: " + err.Error()})
			return
		}
		p.emit(piEvent{Kind: piEventCompactStart, CompactReason: event.Reason})
	case "compaction_end":
		var event struct {
			Reason string `json:"reason"`
			Result *struct {
				Summary              string `json:"summary"`
				TokensBefore         int64  `json:"tokensBefore"`
				EstimatedTokensAfter int64  `json:"estimatedTokensAfter"`
			} `json:"result"`
			Aborted      bool   `json:"aborted"`
			WillRetry    bool   `json:"willRetry"`
			ErrorMessage string `json:"errorMessage"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			p.emit(piEvent{Kind: piEventParseError, Error: "Invalid Pi compaction end: " + err.Error()})
			return
		}
		result := piEvent{
			Kind:           piEventCompactEnd,
			CompactReason:  event.Reason,
			CompactAborted: event.Aborted,
			WillRetry:      event.WillRetry,
			CompactError:   event.ErrorMessage,
		}
		if event.Result != nil {
			result.CompactSummary = event.Result.Summary
			result.CompactBefore = event.Result.TokensBefore
			result.CompactAfter = event.Result.EstimatedTokensAfter
		}
		p.emit(result)
	case "auto_retry_start":
		var event struct {
			Attempt      int    `json:"attempt"`
			MaxAttempts  int    `json:"maxAttempts"`
			DelayMS      int64  `json:"delayMs"`
			ErrorMessage string `json:"errorMessage"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			p.emit(piEvent{Kind: piEventParseError, Error: "Invalid Pi retry start: " + err.Error()})
			return
		}
		p.emit(piEvent{
			Kind:         piEventRetryStart,
			RetryAttempt: event.Attempt,
			RetryMax:     event.MaxAttempts,
			RetryDelayMS: event.DelayMS,
			RetryError:   event.ErrorMessage,
		})
	case "auto_retry_end":
		var event struct {
			Success    bool   `json:"success"`
			Attempt    int    `json:"attempt"`
			FinalError string `json:"finalError"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			p.emit(piEvent{Kind: piEventParseError, Error: "Invalid Pi retry end: " + err.Error()})
			return
		}
		p.emit(piEvent{
			Kind:           piEventRetryEnd,
			RetrySucceeded: event.Success,
			RetryAttempt:   event.Attempt,
			RetryError:     event.FinalError,
		})
	case "agent_end":
		var event struct {
			WillRetry bool `json:"willRetry"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			p.emit(piEvent{Kind: piEventParseError, Error: "Invalid Pi agent end: " + err.Error()})
			return
		}
		p.emit(piEvent{Kind: piEventAgentEnd, WillRetry: event.WillRetry})
	case "extension_error":
		var event struct {
			ExtensionPath string `json:"extensionPath"`
			Event         string `json:"event"`
			Error         string `json:"error"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			p.emit(piEvent{Kind: piEventParseError, Error: "Invalid Pi extension error: " + err.Error()})
			return
		}
		p.emit(piEvent{
			Kind:           piEventExtensionErr,
			ExtensionPath:  event.ExtensionPath,
			ExtensionEvent: event.Event,
			Error:          fallback(event.Error, "Pi extension error"),
		})
	}
}

func (p *piClient) handleResponse(record piRPCResponse) {
	p.mu.Lock()
	ch := p.pending[record.ID]
	delete(p.pending, record.ID)
	p.mu.Unlock()
	if ch != nil {
		ch <- record
	}

	event := piEvent{
		Kind:      piEventResponse,
		RequestID: record.ID,
		Command:   record.Command,
		Success:   record.Success,
		Error:     record.Error,
	}
	if record.Command == "prompt" && record.Success {
		var data struct {
			Disposition string `json:"disposition"`
		}
		_ = json.Unmarshal(record.Data, &data)
		event.Disposition = data.Disposition
	}
	p.emit(event)
}

func (p *piClient) handleMessageUpdate(line []byte) {
	var event struct {
		AssistantMessageEvent struct {
			Type         string `json:"type"`
			ContentIndex int    `json:"contentIndex"`
			Delta        string `json:"delta"`
			Content      string `json:"content"`
		} `json:"assistantMessageEvent"`
	}
	if err := json.Unmarshal(line, &event); err != nil {
		p.emit(piEvent{Kind: piEventParseError, Error: "Invalid Pi message update: " + err.Error()})
		return
	}
	update := event.AssistantMessageEvent
	switch update.Type {
	case "text_start":
		p.setStreamPart(update.ContentIndex, "")
	case "text_delta":
		p.appendStreamPart(update.ContentIndex, update.Delta)
	case "text_end":
		p.setStreamPart(update.ContentIndex, update.Content)
	default:
		return
	}
	p.emit(piEvent{Kind: piEventText, Text: p.streamSnapshot()})
}

func (p *piClient) handleMessageEnd(line []byte) {
	var event struct {
		Message struct {
			Role         string          `json:"role"`
			Content      json.RawMessage `json:"content"`
			StopReason   string          `json:"stopReason"`
			ErrorMessage string          `json:"errorMessage"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &event); err != nil {
		return
	}
	if event.Message.Role != "assistant" {
		return
	}
	text := contentText(event.Message.Content)
	if text == "" && event.Message.ErrorMessage == "" {
		p.resetStream()
		return
	}
	p.emit(piEvent{
		Kind:  piEventMessage,
		Text:  text,
		Error: event.Message.ErrorMessage,
	})
	p.resetStream()
}

func (p *piClient) emit(event piEvent) {
	event.Source = p
	if p.events != nil {
		p.events(event)
	}
}

func (p *piClient) failPending(err error) {
	p.mu.Lock()
	pending := p.pending
	p.pending = make(map[string]chan piRPCResponse)
	p.mu.Unlock()
	for _, ch := range pending {
		select {
		case ch <- piRPCResponse{Type: "response", Success: false, Error: err.Error()}:
		default:
		}
	}
}

func (p *piClient) dropPending(id string) {
	p.mu.Lock()
	delete(p.pending, id)
	p.mu.Unlock()
}

func (p *piClient) newRequestID(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), p.nextID.Add(1))
}

func (p *piClient) stderrSnapshot() string {
	p.stderrMu.Lock()
	defer p.stderrMu.Unlock()
	return p.stderrTail
}

func (p *piClient) resetStream() {
	p.streamMu.Lock()
	p.streamParts = make(map[int]string)
	p.streamOrder = nil
	p.streamMu.Unlock()
}

func (p *piClient) setStreamPart(index int, text string) {
	p.streamMu.Lock()
	defer p.streamMu.Unlock()
	if _, exists := p.streamParts[index]; !exists {
		p.streamOrder = append(p.streamOrder, index)
	}
	p.streamParts[index] = text
}

func (p *piClient) appendStreamPart(index int, delta string) {
	p.streamMu.Lock()
	defer p.streamMu.Unlock()
	if _, exists := p.streamParts[index]; !exists {
		p.streamOrder = append(p.streamOrder, index)
	}
	p.streamParts[index] += delta
}

func (p *piClient) streamSnapshot() string {
	p.streamMu.Lock()
	defer p.streamMu.Unlock()
	var b strings.Builder
	for i, index := range p.streamOrder {
		if i > 0 && b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(p.streamParts[index])
	}
	return b.String()
}

func contentText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
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

func processExitError(err error) string {
	if err == nil {
		return "Pi RPC process exited"
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return "Pi RPC process exited with code " + strconv.Itoa(exitErr.ExitCode())
	}
	return "Pi RPC process exited: " + err.Error()
}

func cleanPiEnv(env []string) []string {
	clean := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if strings.HasPrefix(entry, "FORCE_COLOR=") || strings.HasPrefix(entry, "NO_COLOR=") {
			continue
		}
		clean = append(clean, entry)
	}
	return append(clean, "NO_COLOR=1")
}

func stripANSI(s string) string {
	var b strings.Builder
	escape := false
	for _, r := range s {
		switch {
		case escape:
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				escape = false
			}
		case r == '\x1b':
			escape = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}

func fallback(value, fallbackValue string) string {
	if strings.TrimSpace(value) == "" {
		return fallbackValue
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
}
