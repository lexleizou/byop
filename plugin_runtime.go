package main

import (
	"bufio"
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

type pluginRuntimeState string

const (
	pluginStateInactive pluginRuntimeState = "inactive"
	pluginStateStarting pluginRuntimeState = "starting"
	pluginStateActive   pluginRuntimeState = "active"
	pluginStateRunning  pluginRuntimeState = "running"
	pluginStateStopping pluginRuntimeState = "stopping"
	pluginStateStopped  pluginRuntimeState = "stopped"
	pluginStateFailed   pluginRuntimeState = "failed"
)

const (
	pluginLifecycleTimeout = 10 * time.Second
	pluginCommandTimeout   = 30 * time.Second
	pluginStopTimeout      = 2 * time.Second
	maxPluginMessageBytes  = 1 << 20
	maxPluginStderrBytes   = 4096
)

// pluginRuntimeStatus is the UI-facing snapshot of one plugin process.
type pluginRuntimeStatus struct {
	State            pluginRuntimeState
	PID              int
	ActivationReason string
	LastCommand      string
	LastResult       string
	LastError        string
	Invocations      int
	UpdatedAt        time.Time
}

// pluginHost owns lazy activation, lifecycle state and command routing for
// native plugins. Each plugin runs as a child process speaking JSONL JSON-RPC.
type pluginHost struct {
	hostVersion string
	onChange    func()

	mu        sync.Mutex
	processes map[string]*pluginProcess
	statuses  map[string]pluginRuntimeStatus
	starting  map[string]*sync.Mutex
}

func newPluginHost(hostVersion string, onChange func()) *pluginHost {
	return &pluginHost{
		hostVersion: hostVersion,
		onChange:    onChange,
		processes:   make(map[string]*pluginProcess),
		statuses:    make(map[string]pluginRuntimeStatus),
		starting:    make(map[string]*sync.Mutex),
	}
}

func (h *pluginHost) status(pluginID string) pluginRuntimeStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	status, ok := h.statuses[pluginID]
	if !ok {
		return pluginRuntimeStatus{State: pluginStateInactive}
	}
	return status
}

func (h *pluginHost) activeCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.processes)
}

func (h *pluginHost) updateStatus(pluginID string, update func(*pluginRuntimeStatus)) {
	h.mu.Lock()
	status, ok := h.statuses[pluginID]
	if !ok {
		status.State = pluginStateInactive
	}
	update(&status)
	status.UpdatedAt = time.Now()
	h.statuses[pluginID] = status
	h.mu.Unlock()
	if h.onChange != nil {
		h.onChange()
	}
}

func (h *pluginHost) startLock(pluginID string) *sync.Mutex {
	h.mu.Lock()
	defer h.mu.Unlock()
	lock, ok := h.starting[pluginID]
	if !ok {
		lock = &sync.Mutex{}
		h.starting[pluginID] = lock
	}
	return lock
}

func (h *pluginHost) process(pluginID string) *pluginProcess {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.processes[pluginID]
}

// invokeCommand dispatches a declared command, starting and activating the
// plugin on first use when the command is one of its activation events.
func (h *pluginHost) invokeCommand(ctx context.Context, entry pluginRegistryEntry, commandID string, args any) (json.RawMessage, error) {
	if entry.Status != pluginRegistered || entry.Manifest == nil {
		return nil, fmt.Errorf("plugin %s is not registered", fallback(entry.ID, "unknown"))
	}
	if !pluginDeclaresCommand(entry.Manifest, commandID) {
		return nil, fmt.Errorf("command %s is not declared by plugin %s", commandID, entry.ID)
	}
	process, err := h.ensureActive(ctx, entry, "onCommand:"+commandID)
	if err != nil {
		return nil, err
	}

	h.updateStatus(entry.ID, func(status *pluginRuntimeStatus) {
		status.State = pluginStateRunning
		status.LastCommand = commandID
		status.LastError = ""
	})
	commandCtx, cancel := context.WithTimeout(ctx, pluginCommandTimeout)
	defer cancel()
	result, err := process.call(commandCtx, "invokeCommand", map[string]any{
		"command": commandID,
		"args":    args,
	})
	if err != nil {
		h.updateStatus(entry.ID, func(status *pluginRuntimeStatus) {
			if process.exited() {
				status.State = pluginStateFailed
			} else {
				status.State = pluginStateActive
			}
			status.LastError = err.Error()
		})
		return nil, err
	}
	h.updateStatus(entry.ID, func(status *pluginRuntimeStatus) {
		status.State = pluginStateActive
		status.Invocations++
		status.LastResult = pluginResultSummary(result)
	})
	return result, nil
}

func (h *pluginHost) ensureActive(ctx context.Context, entry pluginRegistryEntry, reason string) (*pluginProcess, error) {
	if process := h.process(entry.ID); process != nil && !process.exited() {
		return process, nil
	}
	lock := h.startLock(entry.ID)
	lock.Lock()
	defer lock.Unlock()
	if process := h.process(entry.ID); process != nil && !process.exited() {
		return process, nil
	}
	if !pluginActivatesOn(entry.Manifest, reason) {
		return nil, fmt.Errorf("plugin %s does not declare activation event %s", entry.ID, reason)
	}

	h.updateStatus(entry.ID, func(status *pluginRuntimeStatus) {
		status.State = pluginStateStarting
		status.ActivationReason = reason
		status.LastError = ""
		status.PID = 0
	})
	process, err := startPluginProcess(entry)
	if err != nil {
		h.failStart(entry.ID, err)
		return nil, err
	}
	h.mu.Lock()
	h.processes[entry.ID] = process
	h.mu.Unlock()
	go h.watchExit(entry.ID, process)

	lifecycleCtx, cancel := context.WithTimeout(ctx, pluginLifecycleTimeout)
	defer cancel()
	if _, err := process.call(lifecycleCtx, "initialize", map[string]any{
		"pluginApi":   pluginAPIVersion,
		"hostVersion": h.hostVersion,
		"pluginId":    entry.ID,
	}); err != nil {
		h.abortStart(entry.ID, process, fmt.Errorf("initialize: %w", err))
		return nil, fmt.Errorf("initialize %s: %w", entry.ID, err)
	}
	if _, err := process.call(lifecycleCtx, "activate", map[string]any{"reason": reason}); err != nil {
		h.abortStart(entry.ID, process, fmt.Errorf("activate: %w", err))
		return nil, fmt.Errorf("activate %s: %w", entry.ID, err)
	}
	h.updateStatus(entry.ID, func(status *pluginRuntimeStatus) {
		status.State = pluginStateActive
		status.PID = process.pid()
	})
	return process, nil
}

func (h *pluginHost) failStart(pluginID string, err error) {
	h.updateStatus(pluginID, func(status *pluginRuntimeStatus) {
		status.State = pluginStateFailed
		status.LastError = err.Error()
		status.PID = 0
	})
}

func (h *pluginHost) abortStart(pluginID string, process *pluginProcess, err error) {
	h.detach(pluginID, process)
	process.kill()
	h.failStart(pluginID, err)
}

func (h *pluginHost) detach(pluginID string, process *pluginProcess) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.processes[pluginID] != process {
		return false
	}
	delete(h.processes, pluginID)
	return true
}

// watchExit marks unexpected process exits as failures. Planned stops detach
// the process first, so they are not reported here.
func (h *pluginHost) watchExit(pluginID string, process *pluginProcess) {
	<-process.done
	if !h.detach(pluginID, process) {
		return
	}
	h.updateStatus(pluginID, func(status *pluginRuntimeStatus) {
		status.State = pluginStateFailed
		status.PID = 0
		status.LastError = process.exitSummary()
	})
}

// deactivate asks the plugin to release resources, closes stdin and waits for
// the process to exit, killing it after a short grace period.
func (h *pluginHost) deactivate(pluginID string) error {
	process := h.process(pluginID)
	if process == nil {
		return nil
	}
	if !h.detach(pluginID, process) {
		return nil
	}
	h.updateStatus(pluginID, func(status *pluginRuntimeStatus) {
		status.State = pluginStateStopping
	})
	ctx, cancel := context.WithTimeout(context.Background(), pluginStopTimeout)
	_, callErr := process.call(ctx, "deactivate", map[string]any{})
	cancel()
	process.closeInput()
	select {
	case <-process.done:
	case <-time.After(pluginStopTimeout):
		process.kill()
		<-process.done
	}
	h.updateStatus(pluginID, func(status *pluginRuntimeStatus) {
		status.State = pluginStateStopped
		status.PID = 0
		if callErr != nil {
			status.LastError = "deactivate: " + callErr.Error()
		}
	})
	if callErr != nil {
		return fmt.Errorf("deactivate %s: %w", pluginID, callErr)
	}
	return nil
}

func (h *pluginHost) stopAll() {
	h.mu.Lock()
	ids := make([]string, 0, len(h.processes))
	for id := range h.processes {
		ids = append(ids, id)
	}
	h.mu.Unlock()
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_ = h.deactivate(id)
		}(id)
	}
	wg.Wait()
}

func pluginDeclaresCommand(manifest *pluginManifest, commandID string) bool {
	for _, command := range manifest.Contributes.Commands {
		if command.ID == commandID {
			return true
		}
	}
	return false
}

func pluginActivatesOn(manifest *pluginManifest, event string) bool {
	if manifest.Activation.Lazy {
		return true
	}
	for _, declared := range manifest.Activation.Events {
		if declared == event {
			return true
		}
	}
	return false
}

// resolveNativePluginEntry returns the real path of an executable entry that
// stays inside the plugin directory after resolving symbolic links.
func resolveNativePluginEntry(directory, entry string) (string, error) {
	if !safePluginRelativePath(entry) {
		return "", errors.New("entry must be a safe relative path inside the plugin folder")
	}
	root, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return "", fmt.Errorf("resolve plugin folder: %w", err)
	}
	target, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(entry)))
	if err != nil {
		return "", fmt.Errorf("resolve entry: %w", err)
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("entry resolves outside the plugin folder")
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("read entry: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("entry is not a regular file")
	}
	if info.Mode().Perm()&0o111 == 0 {
		return "", errors.New("entry is not executable")
	}
	return target, nil
}

type pluginRPCError struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message"`
}

type pluginRPCMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *pluginRPCError `json:"error,omitempty"`
}

type pluginRPCResult struct {
	result json.RawMessage
	err    error
}

type pluginProcess struct {
	pluginID string
	cmd      *exec.Cmd

	writeMu sync.Mutex
	stdin   io.WriteCloser

	mu       sync.Mutex
	pending  map[string]chan pluginRPCResult
	stderr   []byte
	exitErr  error
	isExited bool
	nextID   atomic.Uint64
	done     chan struct{}
}

func startPluginProcess(entry pluginRegistryEntry) (*pluginProcess, error) {
	if entry.Manifest.Runtime != "native" {
		return nil, fmt.Errorf("runtime %s is not supported in P4.1; only native plugins can be activated", entry.Manifest.Runtime)
	}
	executable, err := resolveNativePluginEntry(entry.SourceDirectory, entry.Manifest.Entry)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(executable)
	cmd.Dir = filepath.Dir(entry.ManifestPath)
	cmd.Env = append(cleanPiEnv(os.Environ()),
		"BYOP_PLUGIN_ID="+entry.ID,
		"BYOP_PLUGIN_API="+pluginAPIVersion,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start plugin: %w", err)
	}
	process := &pluginProcess{
		pluginID: entry.ID,
		cmd:      cmd,
		stdin:    stdin,
		pending:  make(map[string]chan pluginRPCResult),
		done:     make(chan struct{}),
	}
	var readers sync.WaitGroup
	readers.Add(2)
	go func() {
		defer readers.Done()
		process.readStdout(stdout)
	}()
	go func() {
		defer readers.Done()
		process.readStderr(stderr)
	}()
	go func() {
		readers.Wait()
		err := cmd.Wait()
		process.mu.Lock()
		process.exitErr = err
		process.isExited = true
		pending := process.pending
		process.pending = make(map[string]chan pluginRPCResult)
		process.mu.Unlock()
		summary := errors.New(process.exitSummary())
		for _, ch := range pending {
			ch <- pluginRPCResult{err: summary}
		}
		close(process.done)
	}()
	return process, nil
}

func (p *pluginProcess) pid() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p *pluginProcess) exited() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.isExited
}

func (p *pluginProcess) exitSummary() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	message := "plugin process exited"
	if p.exitErr != nil {
		message += ": " + p.exitErr.Error()
	}
	if tail := strings.TrimSpace(stripANSI(string(p.stderr))); tail != "" {
		message += " · " + summarizeLine(tail, 240)
	}
	return message
}

func (p *pluginProcess) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := strconv.FormatUint(p.nextID.Add(1), 10)
	reply := make(chan pluginRPCResult, 1)
	p.mu.Lock()
	if p.isExited {
		p.mu.Unlock()
		return nil, errors.New("plugin process is not running")
	}
	p.pending[id] = reply
	p.mu.Unlock()

	line, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	if err == nil {
		p.writeMu.Lock()
		_, err = p.stdin.Write(append(line, '\n'))
		p.writeMu.Unlock()
	}
	if err != nil {
		p.forget(id)
		return nil, fmt.Errorf("send %s: %w", method, err)
	}
	select {
	case result := <-reply:
		return result.result, result.err
	case <-ctx.Done():
		p.forget(id)
		return nil, fmt.Errorf("%s timed out: %w", method, ctx.Err())
	}
}

func (p *pluginProcess) forget(id string) {
	p.mu.Lock()
	delete(p.pending, id)
	p.mu.Unlock()
}

func (p *pluginProcess) readStdout(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxPluginMessageBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var message pluginRPCMessage
		if err := json.Unmarshal([]byte(line), &message); err != nil || len(message.ID) == 0 {
			// Plugin-to-host notifications are reserved for later phases.
			continue
		}
		id := string(message.ID)
		if unquoted, err := strconv.Unquote(id); err == nil {
			id = unquoted
		}
		p.mu.Lock()
		reply, ok := p.pending[id]
		delete(p.pending, id)
		p.mu.Unlock()
		if !ok {
			continue
		}
		if message.Error != nil {
			reply <- pluginRPCResult{err: errors.New(fallback(message.Error.Message, "plugin returned an error"))}
		} else {
			reply <- pluginRPCResult{result: message.Result}
		}
	}
	if err := scanner.Err(); err != nil {
		p.appendStderr([]byte("stdout: " + err.Error()))
		p.kill()
	}
}

func (p *pluginProcess) readStderr(r io.Reader) {
	buffer := make([]byte, 2048)
	for {
		n, err := r.Read(buffer)
		if n > 0 {
			p.appendStderr(buffer[:n])
		}
		if err != nil {
			return
		}
	}
}

func (p *pluginProcess) appendStderr(data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stderr = append(p.stderr, data...)
	if len(p.stderr) > maxPluginStderrBytes {
		p.stderr = append([]byte(nil), p.stderr[len(p.stderr)-maxPluginStderrBytes:]...)
	}
}

func (p *pluginProcess) closeInput() {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	_ = p.stdin.Close()
}

func (p *pluginProcess) kill() {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
}

func pluginResultSummary(result json.RawMessage) string {
	if len(result) == 0 || string(result) == "null" {
		return "Command completed"
	}
	var object map[string]any
	if err := json.Unmarshal(result, &object); err == nil {
		if message, ok := object["message"].(string); ok && strings.TrimSpace(message) != "" {
			return summarizeLine(message, 160)
		}
	}
	var text string
	if err := json.Unmarshal(result, &text); err == nil {
		return summarizeLine(text, 160)
	}
	return summarizeLine(string(result), 160)
}
