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

var errPluginStopped = errors.New("plugin stopped")

// pluginRuntimeStatus is the UI-facing snapshot of one plugin process.
type pluginRuntimeStatus struct {
	State            pluginRuntimeState
	PID              int
	ActivationReason string
	LastCommand      string
	LastResult       string
	LastError        string
	Invocations      int
	HostCalls        int
	DeniedCalls      int
	LastDenied       string
	UpdatedAt        time.Time
}

// pluginHost owns lazy activation, lifecycle state and command routing for
// local plugin processes. Each plugin communicates over JSONL JSON-RPC.
type pluginHost struct {
	hostVersion             string
	onChange                func()
	onNotice                func(pluginID, pluginName, text, tone string)
	onTaskUpdate            func(update pluginTaskUpdate)
	onRefreshView           func(pluginID, viewID string)
	themeDark               func() bool
	themeHighContrast       func() bool
	themeReduceTransparency func() bool
	reducedMotion           atomic.Bool
	locale                  func() string

	workspaceRoot string
	settings      *pluginSettingsStore

	mu               sync.Mutex
	processes        map[string]*pluginProcess
	statuses         map[string]pluginRuntimeStatus
	starting         map[string]*sync.Mutex
	settingsMu       sync.Mutex
	settingsByPlugin map[string]map[string]any

	nodeMu      sync.Mutex
	nodeChecked bool
	nodePath    string
	nodeVersion string
	nodeErr     error
}

type pluginHostOption func(*pluginHost)

func pluginHostWorkspace(root string) pluginHostOption {
	return func(host *pluginHost) {
		host.workspaceRoot = root
	}
}

func pluginHostSettings(store *pluginSettingsStore) pluginHostOption {
	return func(host *pluginHost) {
		host.settings = store
	}
}

func pluginHostNoticeHandler(handler func(pluginID, pluginName, text, tone string)) pluginHostOption {
	return func(host *pluginHost) {
		host.onNotice = handler
	}
}

func pluginHostTaskHandler(handler func(update pluginTaskUpdate)) pluginHostOption {
	return func(host *pluginHost) {
		host.onTaskUpdate = handler
	}
}

func pluginHostRefreshHandler(handler func(pluginID, viewID string)) pluginHostOption {
	return func(host *pluginHost) {
		host.onRefreshView = handler
	}
}

func pluginHostTheme(dark func() bool) pluginHostOption {
	return func(host *pluginHost) {
		host.themeDark = dark
	}
}

func pluginHostHighContrast(high func() bool) pluginHostOption {
	return func(host *pluginHost) {
		host.themeHighContrast = high
	}
}

func pluginHostReduceTransparency(reduce func() bool) pluginHostOption {
	return func(host *pluginHost) {
		host.themeReduceTransparency = reduce
	}
}

func pluginHostLocale(locale func() string) pluginHostOption {
	return func(host *pluginHost) {
		host.locale = locale
	}
}

func newPluginHost(hostVersion string, onChange func(), options ...pluginHostOption) *pluginHost {
	workspace, err := os.Getwd()
	if configured := strings.TrimSpace(os.Getenv("BYOP_WORKSPACE")); configured != "" {
		workspace = configured
	}
	if err == nil {
		if absolute, absoluteErr := filepath.Abs(workspace); absoluteErr == nil {
			workspace = absolute
		}
		if resolved, resolveErr := filepath.EvalSymlinks(workspace); resolveErr == nil {
			workspace = resolved
		}
	}
	configDir, configErr := pluginConfigDir()
	if configErr != nil {
		configDir = filepath.Join(os.TempDir(), "byop-config")
	}
	host := &pluginHost{
		hostVersion:             hostVersion,
		onChange:                onChange,
		processes:               make(map[string]*pluginProcess),
		statuses:                make(map[string]pluginRuntimeStatus),
		starting:                make(map[string]*sync.Mutex),
		workspaceRoot:           workspace,
		settings:                newPluginSettingsStore(configDir),
		settingsByPlugin:        make(map[string]map[string]any),
		themeDark:               func() bool { return false },
		themeHighContrast:       func() bool { return false },
		themeReduceTransparency: func() bool { return false },
		locale:                  func() string { return "en" },
	}
	for _, option := range options {
		if option != nil {
			option(host)
		}
	}
	if host.workspaceRoot != "" {
		if absolute, absoluteErr := filepath.Abs(host.workspaceRoot); absoluteErr == nil {
			host.workspaceRoot = absolute
		}
		if resolved, resolveErr := filepath.EvalSymlinks(host.workspaceRoot); resolveErr == nil {
			host.workspaceRoot = resolved
		}
	}
	return host
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
	return h.invokeCommandTimeout(ctx, entry, commandID, args, pluginCommandTimeout)
}

func (h *pluginHost) invokeCommandTimeout(
	ctx context.Context,
	entry pluginRegistryEntry,
	commandID string,
	args any,
	timeout time.Duration,
) (json.RawMessage, error) {
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
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
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
	process, err := startPluginProcess(entry, h)
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
	settings, err := h.settingsFor(entry)
	if err != nil {
		h.abortStart(entry.ID, process, fmt.Errorf("settings: %w", err))
		return nil, fmt.Errorf("load settings %s: %w", entry.ID, err)
	}
	if _, err := process.call(lifecycleCtx, "initialize", map[string]any{
		"pluginApi":    pluginAPIVersion,
		"hostVersion":  h.hostVersion,
		"pluginId":     entry.ID,
		"settings":     settings,
		"capabilities": entry.Manifest.Capabilities,
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

func (h *pluginHost) settingsFor(entry pluginRegistryEntry) (map[string]any, error) {
	h.settingsMu.Lock()
	defer h.settingsMu.Unlock()
	if values, ok := h.settingsByPlugin[entry.ID]; ok {
		return clonePluginSettings(values), nil
	}
	values, err := h.settings.load(entry.Manifest)
	if err != nil {
		return nil, err
	}
	h.settingsByPlugin[entry.ID] = clonePluginSettings(values)
	return values, nil
}

func (h *pluginHost) settingsWarning(pluginID string) string {
	if h.settings == nil {
		return ""
	}
	return h.settings.warning(pluginID)
}

func (h *pluginHost) savePluginSettings(entry pluginRegistryEntry, values map[string]any) error {
	current, err := h.settingsFor(entry)
	if err != nil {
		return err
	}
	allValues := clonePluginSettings(current)
	for key, value := range values {
		allValues[key] = value
	}
	merged, err := h.settings.save(entry.Manifest, allValues)
	if err != nil {
		return err
	}
	h.settingsMu.Lock()
	h.settingsByPlugin[entry.ID] = clonePluginSettings(merged)
	h.settingsMu.Unlock()
	if process := h.process(entry.ID); process != nil && !process.exited() {
		if err := process.notify("settingsChanged", map[string]any{"settings": merged}); err != nil {
			return fmt.Errorf("settings saved, but notify plugin: %w", err)
		}
	}
	return nil
}

func (h *pluginHost) invalidateSettingsCache() {
	h.settingsMu.Lock()
	h.settingsByPlugin = make(map[string]map[string]any)
	h.settingsMu.Unlock()
}

func clonePluginSettings(values map[string]any) map[string]any {
	clone := make(map[string]any, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
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
	process.stopPending(fmt.Errorf("plugin %s stopped: %w", pluginID, errPluginStopped))
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

// resolvePluginEntry returns the real path of an entry that stays inside the
// plugin directory after resolving symbolic links.
func resolvePluginEntry(directory, entry, runtime string) (string, error) {
	target, info, err := resolvePluginPath(directory, entry)
	if err != nil {
		return "", err
	}
	switch runtime {
	case "node":
		switch strings.ToLower(filepath.Ext(target)) {
		case ".js", ".mjs", ".cjs":
		default:
			return "", errors.New("Node entry must end in .js, .mjs, or .cjs")
		}
	case "native":
		if info.Mode().Perm()&0o111 == 0 {
			return "", errors.New("entry is not executable")
		}
	default:
		return "", fmt.Errorf("runtime %s is not supported", runtime)
	}
	return target, nil
}

// resolvePluginPath returns the real path and metadata of a regular file that
// stays inside the plugin directory after resolving symbolic links.
func resolvePluginPath(directory, entry string) (string, os.FileInfo, error) {
	if !safePluginRelativePath(entry) {
		return "", nil, errors.New("entry must be a safe relative path inside the plugin folder")
	}
	root, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return "", nil, fmt.Errorf("resolve plugin folder: %w", err)
	}
	target, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(entry)))
	if err != nil {
		return "", nil, fmt.Errorf("resolve entry: %w", err)
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", nil, errors.New("entry resolves outside the plugin folder")
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", nil, fmt.Errorf("read entry: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", nil, errors.New("entry is not a regular file")
	}
	return target, info, nil
}

func resolveNativePluginEntry(directory, entry string) (string, error) {
	executable, err := resolvePluginEntry(directory, entry, "native")
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(executable); err != nil {
		return "", err
	} else if info.Mode().Perm()&0o111 == 0 {
		return "", errors.New("entry is not executable")
	}
	return executable, nil
}

type pluginRPCError struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message"`
}

type pluginRPCMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
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
	entry    pluginRegistryEntry
	host     *pluginHost

	writeMu   sync.Mutex
	stdin     io.WriteCloser
	hostSlots chan struct{}

	mu       sync.Mutex
	pending  map[string]chan pluginRPCResult
	stderr   []byte
	exitErr  error
	isExited bool
	nextID   atomic.Uint64
	done     chan struct{}
}

func startPluginProcess(entry pluginRegistryEntry, host *pluginHost) (*pluginProcess, error) {
	if host == nil {
		return nil, errors.New("plugin host is unavailable")
	}
	executable, err := resolvePluginEntry(entry.SourceDirectory, entry.Manifest.Entry, entry.Manifest.Runtime)
	if err != nil {
		return nil, err
	}
	var cmd *exec.Cmd
	switch entry.Manifest.Runtime {
	case "native":
		cmd = exec.Command(executable)
	case "node":
		nodePath, nodeErr := host.nodeRuntime()
		if nodeErr != nil {
			return nil, nodeErr
		}
		pluginRoot, rootErr := filepath.EvalSymlinks(entry.SourceDirectory)
		if rootErr != nil {
			return nil, fmt.Errorf("resolve plugin folder: %w", rootErr)
		}
		args := nodePermissionArgs(entry.Manifest, pluginRoot, host.workspaceRoot, executable)
		cmd = exec.Command(nodePath, args...)
	default:
		return nil, fmt.Errorf("runtime %s is not supported", entry.Manifest.Runtime)
	}
	cmd.Dir = filepath.Dir(entry.ManifestPath)
	cmd.Env = append(cleanPiEnv(os.Environ()),
		"BYOP_PLUGIN_ID="+entry.ID,
		"BYOP_PLUGIN_API="+pluginAPIVersion,
		"BYOP_PLUGIN_RUNTIME="+entry.Manifest.Runtime,
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
		pluginID:  entry.ID,
		cmd:       cmd,
		entry:     entry,
		host:      host,
		stdin:     stdin,
		hostSlots: make(chan struct{}, pluginHostConcurrentRequests),
		pending:   make(map[string]chan pluginRPCResult),
		done:      make(chan struct{}),
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
		message += ": " + summarizeLine(tail, 240)
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

func (p *pluginProcess) notify(method string, params any) error {
	line, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return err
	}
	return p.writeLine(line)
}

func (p *pluginProcess) forget(id string) {
	p.mu.Lock()
	delete(p.pending, id)
	p.mu.Unlock()
}

func (p *pluginProcess) stopPending(err error) {
	p.mu.Lock()
	pending := p.pending
	p.pending = make(map[string]chan pluginRPCResult)
	p.mu.Unlock()
	for _, reply := range pending {
		reply <- pluginRPCResult{err: err}
	}
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
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			continue
		}
		if message.Method != "" {
			p.host.updateStatus(p.pluginID, func(status *pluginRuntimeStatus) {
				status.HostCalls++
			})
			select {
			case p.hostSlots <- struct{}{}:
				go p.dispatchHostCall(message)
			default:
				if len(message.ID) > 0 {
					id := append(json.RawMessage(nil), message.ID...)
					go func() {
						_ = p.writeHostError(id, pluginRPCOtherHostError, "host busy")
					}()
				}
			}
			continue
		}
		if len(message.ID) == 0 {
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

func (p *pluginProcess) dispatchHostCall(message pluginRPCMessage) {
	defer func() { <-p.hostSlots }()
	result := p.host.handleHostCall(p.entry, message.Method, message.Params)
	if len(message.ID) == 0 {
		return
	}
	if result.Err != nil {
		_ = p.writeHostError(message.ID, result.Err.Code, result.Err.Message)
		return
	}
	_ = p.writeRPC(map[string]any{
		"jsonrpc": "2.0",
		"id":      message.ID,
		"result":  result.Value,
	})
}

func (p *pluginProcess) writeHostError(id json.RawMessage, code int, message string) error {
	return p.writeRPC(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

func (p *pluginProcess) writeRPC(value any) error {
	line, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return p.writeLine(line)
}

func (p *pluginProcess) writeLine(line []byte) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	_, err := p.stdin.Write(append(line, '\n'))
	return err
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
