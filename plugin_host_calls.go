package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

const (
	pluginRPCMethodNotFound      = -32601
	pluginRPCInvalidParams       = -32602
	pluginRPCPermissionDenied    = -32001
	pluginRPCOtherHostError      = -32000
	maxPluginFileBytes           = 1 << 20
	maxPluginViewArgsBytes       = 64 << 10
	maxPluginNoticeRunes         = 200
	maxPluginTaskDetailRunes     = 1000
	maxPluginTaskStatusRunes     = 80
	pluginHostConcurrentRequests = 8
)

type pluginHostError struct {
	Code    int
	Message string
}

func (err *pluginHostError) Error() string {
	return err.Message
}

type pluginHandleResult struct {
	Value any
	Err   *pluginRPCError
}

type pluginTaskUpdate struct {
	PluginID string
	TaskID   string
	Title    string
	Detail   string
	Status   string
	Tone     string
}

func (h *pluginHost) handleHostCall(entry pluginRegistryEntry, method string, params json.RawMessage) pluginHandleResult {
	if entry.Manifest == nil {
		return pluginHostFailure(pluginRPCOtherHostError, "plugin manifest is unavailable")
	}
	switch method {
	case "host.getTheme":
		if err := requireEmptyPluginParams(params); err != nil {
			return pluginHostFailure(pluginRPCInvalidParams, err.Error())
		}
		dark := false
		if h.themeDark != nil {
			dark = h.themeDark()
		}
		return pluginHandleResult{Value: currentPluginThemePayload(dark)}
	case "host.getSettings":
		if err := requireEmptyPluginParams(params); err != nil {
			return pluginHostFailure(pluginRPCInvalidParams, err.Error())
		}
		settings, err := h.settingsFor(entry)
		if err != nil {
			return pluginHostFailure(pluginRPCOtherHostError, err.Error())
		}
		return pluginHandleResult{Value: settings}
	case "host.showNotice":
		var request struct {
			Text string `json:"text"`
			Tone string `json:"tone,omitempty"`
		}
		if err := decodePluginHostParams(params, &request); err != nil {
			return pluginHostFailure(pluginRPCInvalidParams, err.Error())
		}
		text := truncateRunes(controlCharacterFilter.ReplaceAllString(strings.TrimSpace(request.Text), ""), maxPluginNoticeRunes)
		if text == "" {
			return pluginHostFailure(pluginRPCInvalidParams, "text is required")
		}
		tone := fallback(request.Tone, "info")
		if !validPluginTone(tone) {
			return pluginHostFailure(pluginRPCInvalidParams, "tone must be info, success, warning, or danger")
		}
		if h.onNotice != nil {
			h.onNotice(entry.ID, entry.Name, text, tone)
		}
		return pluginHandleResult{Value: map[string]any{}}
	case "host.updateTask":
		var request struct {
			ID     string `json:"id"`
			Detail string `json:"detail"`
			Status string `json:"status,omitempty"`
			Tone   string `json:"tone,omitempty"`
		}
		if err := decodePluginHostParams(params, &request); err != nil {
			return pluginHostFailure(pluginRPCInvalidParams, err.Error())
		}
		task, ok := pluginTaskPanelByID(entry.Manifest, request.ID)
		if !ok {
			return h.denied(entry.ID, "task panel is not declared: "+request.ID)
		}
		tone := fallback(request.Tone, "info")
		if !validPluginTone(tone) {
			return pluginHostFailure(pluginRPCInvalidParams, "tone must be info, success, warning, or danger")
		}
		detail := truncateRunes(controlCharacterFilter.ReplaceAllString(request.Detail, ""), maxPluginTaskDetailRunes)
		if strings.TrimSpace(detail) == "" {
			return pluginHostFailure(pluginRPCInvalidParams, "detail is required")
		}
		status := truncateRunes(controlCharacterFilter.ReplaceAllString(request.Status, ""), maxPluginTaskStatusRunes)
		if h.onTaskUpdate != nil {
			h.onTaskUpdate(pluginTaskUpdate{
				PluginID: entry.ID,
				TaskID:   request.ID,
				Title:    task.Title,
				Detail:   detail,
				Status:   status,
				Tone:     tone,
			})
		}
		return pluginHandleResult{Value: map[string]any{}}
	case "host.refreshView":
		var request struct {
			View string `json:"view"`
		}
		if err := decodePluginHostParams(params, &request); err != nil {
			return pluginHostFailure(pluginRPCInvalidParams, err.Error())
		}
		_, nativeView := pluginViewByID(entry.Manifest, request.View)
		_, webView := pluginWebViewContributionByID(entry.Manifest, request.View)
		if !nativeView && !webView {
			return h.denied(entry.ID, "view is not declared: "+request.View)
		}
		if h.onRefreshView != nil {
			h.onRefreshView(entry.ID, request.View)
		}
		return pluginHandleResult{Value: map[string]any{}}
	case "host.getWorkspace":
		if err := requireEmptyPluginParams(params); err != nil {
			return pluginHostFailure(pluginRPCInvalidParams, err.Error())
		}
		if !pluginWorkspaceAllowed(entry.Manifest) {
			return h.denied(entry.ID, "workspace capability is required")
		}
		return pluginHandleResult{Value: map[string]any{
			"name": filepath.Base(h.workspaceRoot),
			"root": h.workspaceRoot,
		}}
	case "host.readFile":
		if !pluginFilesReadable(entry.Manifest) {
			return h.denied(entry.ID, "files read or write capability is required")
		}
		var request struct {
			Path string `json:"path"`
		}
		if err := decodePluginHostParams(params, &request); err != nil {
			return pluginHostFailure(pluginRPCInvalidParams, err.Error())
		}
		filename, cleanPath, rpcErr := resolvePluginWorkspaceFile(h.workspaceRoot, request.Path, false)
		if rpcErr != nil {
			return pluginHandleResult{Err: rpcErr}
		}
		info, err := os.Stat(filename)
		if err != nil {
			return pluginHostFailure(pluginRPCOtherHostError, "read file: "+err.Error())
		}
		if !info.Mode().IsRegular() {
			return pluginHostFailure(pluginRPCInvalidParams, "path is not a regular file")
		}
		if info.Size() > maxPluginFileBytes {
			return pluginHostFailure(pluginRPCInvalidParams, "file exceeds 1 MiB")
		}
		content, err := os.ReadFile(filename)
		if err != nil {
			return pluginHostFailure(pluginRPCOtherHostError, "read file: "+err.Error())
		}
		if !utf8.Valid(content) {
			return pluginHostFailure(pluginRPCInvalidParams, "file is not valid UTF-8")
		}
		return pluginHandleResult{Value: map[string]any{
			"path":    cleanPath,
			"content": string(content),
			"size":    len(content),
		}}
	case "host.writeFile":
		if !pluginFilesWritable(entry.Manifest) {
			return h.denied(entry.ID, "files write capability is required")
		}
		var request struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := decodePluginHostParams(params, &request); err != nil {
			return pluginHostFailure(pluginRPCInvalidParams, err.Error())
		}
		if len(request.Content) > maxPluginFileBytes {
			return pluginHostFailure(pluginRPCInvalidParams, "content exceeds 1 MiB")
		}
		filename, cleanPath, rpcErr := resolvePluginWorkspaceFile(h.workspaceRoot, request.Path, true)
		if rpcErr != nil {
			return pluginHandleResult{Err: rpcErr}
		}
		if err := atomicWritePluginFile(filename, []byte(request.Content)); err != nil {
			return pluginHostFailure(pluginRPCOtherHostError, err.Error())
		}
		return pluginHandleResult{Value: map[string]any{
			"path": cleanPath,
			"size": len(request.Content),
		}}
	default:
		return pluginHostFailure(pluginRPCMethodNotFound, "unknown host method: "+method)
	}
}

func pluginHostFailure(code int, message string) pluginHandleResult {
	return pluginHandleResult{Err: &pluginRPCError{Code: code, Message: message}}
}

func (h *pluginHost) denied(pluginID, reason string) pluginHandleResult {
	message := "permission denied: " + reason
	h.mu.Lock()
	status := h.statuses[pluginID]
	status.DeniedCalls++
	status.LastDenied = reason
	status.UpdatedAt = time.Now()
	h.statuses[pluginID] = status
	h.mu.Unlock()
	if h.onChange != nil {
		h.onChange()
	}
	return pluginHostFailure(pluginRPCPermissionDenied, message)
}

func decodePluginHostParams(raw json.RawMessage, target any) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("invalid params: more than one JSON value")
	}
	return nil
}

func requireEmptyPluginParams(raw json.RawMessage) error {
	var params map[string]any
	if err := decodePluginHostParams(raw, &params); err != nil {
		return err
	}
	if len(params) != 0 {
		return errors.New("invalid params: expected an empty object")
	}
	return nil
}

func validPluginTone(tone string) bool {
	switch tone {
	case "info", "success", "warning", "danger":
		return true
	default:
		return false
	}
}

func pluginTaskPanelByID(manifest *pluginManifest, id string) (pluginTaskPanelContribution, bool) {
	for _, task := range manifest.Contributes.TaskPanels {
		if task.ID == id {
			return task, true
		}
	}
	return pluginTaskPanelContribution{}, false
}

func pluginViewByID(manifest *pluginManifest, id string) (pluginViewContribution, bool) {
	for _, view := range manifest.Contributes.Views {
		if view.ID == id {
			return view, true
		}
	}
	return pluginViewContribution{}, false
}

func pluginWorkspaceAllowed(manifest *pluginManifest) bool {
	return manifest.Capabilities.Workspace == "read" || manifest.Capabilities.Workspace == "write"
}

func pluginFilesReadable(manifest *pluginManifest) bool {
	return manifest.Capabilities.Files == "read" || manifest.Capabilities.Files == "write"
}

func pluginFilesWritable(manifest *pluginManifest) bool {
	return manifest.Capabilities.Files == "write"
}

func resolvePluginWorkspaceFile(root, relative string, write bool) (string, string, *pluginRPCError) {
	if !safePluginRelativePath(relative) {
		return "", "", &pluginRPCError{Code: pluginRPCInvalidParams, Message: "path must be relative and stay inside the workspace"}
	}
	cleanPath := path.Clean(relative)
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", &pluginRPCError{Code: pluginRPCOtherHostError, Message: "resolve workspace: " + err.Error()}
	}
	candidate := filepath.Join(rootReal, filepath.FromSlash(cleanPath))
	parent := filepath.Dir(candidate)
	parentReal, err := filepath.EvalSymlinks(parent)
	if err != nil {
		if write && errors.Is(err, os.ErrNotExist) {
			return "", "", &pluginRPCError{Code: pluginRPCInvalidParams, Message: "parent directory must already exist"}
		}
		return "", "", &pluginRPCError{Code: pluginRPCOtherHostError, Message: "resolve parent directory: " + err.Error()}
	}
	if !pathInside(rootReal, parentReal) {
		return "", "", &pluginRPCError{Code: pluginRPCInvalidParams, Message: "path resolves outside the workspace"}
	}
	target := filepath.Join(parentReal, filepath.Base(candidate))
	if realTarget, err := filepath.EvalSymlinks(target); err == nil {
		if !pathInside(rootReal, realTarget) {
			return "", "", &pluginRPCError{Code: pluginRPCInvalidParams, Message: "path resolves outside the workspace"}
		}
		target = realTarget
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", &pluginRPCError{Code: pluginRPCOtherHostError, Message: "resolve path: " + err.Error()}
	} else if !write {
		return "", "", &pluginRPCError{Code: pluginRPCOtherHostError, Message: "resolve path: " + err.Error()}
	}
	if !pathInside(rootReal, target) {
		return "", "", &pluginRPCError{Code: pluginRPCInvalidParams, Message: "path resolves outside the workspace"}
	}
	return target, cleanPath, nil
}

func pathInside(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func atomicWritePluginFile(filename string, content []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(filename), ".byop-write-*.tmp")
	if err != nil {
		return errors.New("create temporary file: " + err.Error())
	}
	tempName := temp.Name()
	remove := true
	defer func() {
		_ = temp.Close()
		if remove {
			_ = os.Remove(tempName)
		}
	}()
	if err := temp.Chmod(0o644); err != nil {
		return errors.New("set file mode: " + err.Error())
	}
	if _, err := temp.Write(content); err != nil {
		return errors.New("write temporary file: " + err.Error())
	}
	if err := temp.Sync(); err != nil {
		return errors.New("sync temporary file: " + err.Error())
	}
	if err := temp.Close(); err != nil {
		return errors.New("close temporary file: " + err.Error())
	}
	if err := os.Rename(tempName, filename); err != nil {
		return errors.New("replace file: " + err.Error())
	}
	remove = false
	return nil
}

func currentPluginThemePayload(dark bool) map[string]any {
	palette := themePalette(dark)
	mode := "light"
	if dark {
		mode = "dark"
	}
	return map[string]any{
		"mode": mode,
		"colors": map[string]string{
			"background":   pluginColorHex(palette.background),
			"surface":      pluginColorHex(palette.surface),
			"surfaceHover": pluginColorHex(palette.surfaceHover),
			"text":         pluginColorHex(palette.text),
			"muted":        pluginColorHex(palette.muted),
			"border":       pluginColorHex(palette.border),
			"accent":       pluginColorHex(palette.accent),
			"success":      pluginColorHex(palette.success),
			"warning":      pluginColorHex(palette.warning),
			"danger":       pluginColorHex(palette.danger),
		},
		"fontSize": map[string]float32{
			"caption":   fontCaption,
			"small":     fontSmall,
			"body":      fontBody,
			"bodyLarge": fontBodyLarge,
			"title":     fontTitle,
		},
	}
}

func pluginColorHex(color ui.Color) string {
	return fmt.Sprintf("#%02X%02X%02X", color.R, color.G, color.B)
}

func truncateRunes(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum])
}
