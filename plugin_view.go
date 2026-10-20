package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	maxPluginViewBlocks = 200
	maxPluginViewItems  = 100
	maxPluginViewRunes  = 4000
)

type pluginViewModel struct {
	Title  string            `json:"title,omitempty"`
	Blocks []pluginViewBlock `json:"blocks"`
}

type pluginViewBlock struct {
	Type     string           `json:"type"`
	Text     string           `json:"text,omitempty"`
	Tone     string           `json:"tone,omitempty"`
	Items    []pluginViewItem `json:"items,omitempty"`
	Label    string           `json:"label,omitempty"`
	Command  string           `json:"command,omitempty"`
	Args     json.RawMessage  `json:"args,omitempty"`
	Disabled bool             `json:"-"`
}

type pluginViewItem struct {
	Label  string `json:"label,omitempty"`
	Value  string `json:"value,omitempty"`
	Title  string `json:"title,omitempty"`
	Detail string `json:"detail,omitempty"`
}

func (h *pluginHost) renderView(ctx context.Context, entry pluginRegistryEntry, viewID string) (pluginViewModel, error) {
	if entry.Status != pluginRegistered || entry.Manifest == nil {
		return pluginViewModel{}, fmt.Errorf("plugin %s is not registered", fallback(entry.ID, "unknown"))
	}
	contribution, ok := pluginViewByID(entry.Manifest, viewID)
	if !ok {
		return pluginViewModel{}, fmt.Errorf("view %s is not declared by plugin %s", viewID, entry.ID)
	}
	if contribution.Location != "main" {
		return pluginViewModel{}, fmt.Errorf("view %s is not a main view", viewID)
	}
	process, err := h.ensureActive(ctx, entry, "onView:"+viewID)
	if err != nil {
		return pluginViewModel{}, err
	}
	settings, err := h.settingsFor(entry)
	if err != nil {
		return pluginViewModel{}, err
	}
	viewCtx, cancel := context.WithTimeout(ctx, pluginLifecycleTimeout)
	defer cancel()
	result, err := process.call(viewCtx, "renderView", map[string]any{
		"view":     viewID,
		"settings": settings,
	})
	if err != nil {
		return pluginViewModel{}, fmt.Errorf("render view: %w", err)
	}
	model, err := decodePluginViewModel(result)
	if err != nil {
		return pluginViewModel{}, err
	}
	return model, nil
}

func decodePluginViewModel(data json.RawMessage) (pluginViewModel, error) {
	var envelope struct {
		Title  string            `json:"title,omitempty"`
		Blocks []json.RawMessage `json:"blocks"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return pluginViewModel{}, fmt.Errorf("decode view model: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return pluginViewModel{}, errors.New("decode view model: more than one JSON value")
	}
	if len(envelope.Blocks) > maxPluginViewBlocks {
		return pluginViewModel{}, fmt.Errorf("view contains %d blocks; maximum is %d",
			len(envelope.Blocks), maxPluginViewBlocks)
	}
	model := pluginViewModel{
		Title:  truncateRunes(envelope.Title, maxPluginViewRunes),
		Blocks: make([]pluginViewBlock, 0, len(envelope.Blocks)),
	}
	for _, raw := range envelope.Blocks {
		var kind struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &kind); err != nil {
			return pluginViewModel{}, fmt.Errorf("decode view block: %w", err)
		}
		switch kind.Type {
		case "heading", "text", "notice", "keyValue", "list", "code", "button":
			var block pluginViewBlock
			blockDecoder := json.NewDecoder(bytes.NewReader(raw))
			blockDecoder.DisallowUnknownFields()
			if err := blockDecoder.Decode(&block); err != nil {
				return pluginViewModel{}, fmt.Errorf("decode %s block: %w", kind.Type, err)
			}
			if err := normalizePluginViewBlock(&block); err != nil {
				return pluginViewModel{}, err
			}
			model.Blocks = append(model.Blocks, block)
		default:
			model.Blocks = append(model.Blocks, pluginViewBlock{Type: kind.Type})
		}
	}
	return model, nil
}

func normalizePluginViewBlock(block *pluginViewBlock) error {
	block.Text = truncateRunes(block.Text, maxPluginViewRunes)
	block.Label = truncateRunes(block.Label, maxPluginViewRunes)
	block.Command = truncateRunes(block.Command, maxPluginViewRunes)
	if len(block.Items) > maxPluginViewItems {
		return fmt.Errorf("view block contains %d items; maximum is %d",
			len(block.Items), maxPluginViewItems)
	}
	for index := range block.Items {
		item := &block.Items[index]
		item.Label = truncateRunes(item.Label, maxPluginViewRunes)
		item.Value = truncateRunes(item.Value, maxPluginViewRunes)
		item.Title = truncateRunes(item.Title, maxPluginViewRunes)
		item.Detail = truncateRunes(item.Detail, maxPluginViewRunes)
	}
	switch block.Type {
	case "notice":
		if block.Tone == "" {
			block.Tone = "info"
		}
		if !validPluginTone(block.Tone) {
			return fmt.Errorf("notice block tone must be info, success, warning, or danger")
		}
	case "button":
		if len(block.Args) == 0 {
			block.Args = json.RawMessage(`{}`)
		}
		if len(block.Args) > maxPluginViewArgsBytes {
			return fmt.Errorf("button args exceed 64 KiB")
		}
		var object map[string]any
		if err := json.Unmarshal(block.Args, &object); err != nil || object == nil {
			return fmt.Errorf("button args must be a JSON object")
		}
	}
	return nil
}

func pluginViewTitle(view pluginViewContribution, model pluginViewModel) string {
	if strings.TrimSpace(model.Title) != "" {
		return model.Title
	}
	return view.Title
}
