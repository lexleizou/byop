package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
	"unicode"
)

const (
	modelLabelEditorPluginID = "local.model-release-labels"
	modelLabelEditorFilename = "manual-model-labels.json"
	modelLabelEditorSchema   = 1
)

var (
	errModelLabelEditorIdentity  = errors.New("model and provider are required")
	errModelLabelEditorBadge     = errors.New("badge must contain 1-12 printable characters")
	errModelLabelEditorDataState = errors.New("model label data could not be read")
	errModelLabelEditorStore     = errors.New("model label data could not be written")
)

type manualModelLabel struct {
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
	Badge    string `json:"badge"`
}

type manualModelLabelFile struct {
	SchemaVersion int                `json:"schemaVersion"`
	Labels        []manualModelLabel `json:"labels"`
}

type manualModelLabelKey struct {
	provider string
	modelID  string
}

type modelLabelStore struct {
	mu      sync.RWMutex
	dir     string
	loaded  bool
	warning string
	labels  map[manualModelLabelKey]string
}

type modelLabelEditorState struct {
	provider  string
	modelID   string
	badge     string
	search    string
	status    string
	statusErr bool
}

var modelLabelEditorStates sync.Map

func getModelLabelEditorState(s *appState) *modelLabelEditorState {
	value, ok := modelLabelEditorStates.Load(s)
	if ok {
		return value.(*modelLabelEditorState)
	}
	state := &modelLabelEditorState{
		provider: s.piModelProvider,
		modelID:  s.piModelID,
		badge:    s.manualModelLabelBadge(s.piModelProvider, s.piModelID),
	}
	value, _ = modelLabelEditorStates.LoadOrStore(s, state)
	return value.(*modelLabelEditorState)
}

func resetModelLabelEditorForTest(s *appState) {
	if s != nil {
		modelLabelEditorStates.Delete(s)
	}
}

func (state *modelLabelEditorState) selectModel(s *appState, model piModel) {
	state.provider = model.Provider
	state.modelID = model.ID
	state.badge = s.manualModelLabelBadge(model.Provider, model.ID)
	state.status = ""
	state.statusErr = false
}

func (state *modelLabelEditorState) save(s *appState) {
	if state.provider == "" || state.modelID == "" {
		state.status, state.statusErr = text("modelLabelEditorMissingModel"), true
		return
	}
	if !modelLabelBadgeValid(strings.TrimSpace(state.badge)) {
		state.status, state.statusErr = text("modelLabelEditorInvalidBadge"), true
		return
	}
	if s.modelLabels == nil {
		state.status, state.statusErr = text("modelLabelEditorDataError"), true
		return
	}
	if _, err := s.modelLabels.set(state.provider, state.modelID, state.badge); err != nil {
		state.status, state.statusErr = modelLabelEditorStatusLabel(err), true
		return
	}
	state.badge = strings.TrimSpace(state.badge)
	state.status, state.statusErr = text("modelLabelEditorSaved"), false
	if s.window != nil {
		s.window.Invalidate()
	}
}

func (state *modelLabelEditorState) remove(s *appState) {
	if state.provider == "" || state.modelID == "" {
		state.status, state.statusErr = text("modelLabelEditorMissingModel"), true
		return
	}
	if s.modelLabels == nil {
		state.status, state.statusErr = text("modelLabelEditorDataError"), true
		return
	}
	if _, err := s.modelLabels.remove(state.provider, state.modelID); err != nil {
		state.status, state.statusErr = modelLabelEditorStatusLabel(err), true
		return
	}
	state.badge = ""
	state.status, state.statusErr = text("modelLabelEditorRemoved"), false
	if s.window != nil {
		s.window.Invalidate()
	}
}

func modelLabelEditorStatusLabel(err error) string {
	switch {
	case errors.Is(err, errModelLabelEditorIdentity):
		return text("modelLabelEditorMissingModel")
	case errors.Is(err, errModelLabelEditorBadge):
		return text("modelLabelEditorInvalidBadge")
	case errors.Is(err, errModelLabelEditorDataState):
		return text("modelLabelEditorDataError")
	case errors.Is(err, errModelLabelEditorStore):
		return text("modelLabelEditorStoreError")
	default:
		return fallback(err.Error(), text("modelLabelEditorStoreError"))
	}
}

func newModelLabelStore(configDir string) *modelLabelStore {
	return &modelLabelStore{
		dir: filepath.Join(configDir, "plugin-data", modelLabelEditorPluginID),
	}
}

func (s *appState) manualModelLabelBadge(provider, modelID string) string {
	if s.modelLabels == nil || !modelReleaseLabelPluginRegistered(pluginRegistryEntriesForLookup(s)) {
		return ""
	}
	return s.modelLabels.badge(provider, modelID)
}

func (s *appState) composerModelLabelBadge(provider, modelID, modelName string) string {
	if badge := s.manualModelLabelBadge(provider, modelID); badge != "" {
		return badge
	}
	entries := s.pluginRegistrySnapshot().Entries
	fallback := make([]pluginRegistryEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.ID == modelLabelEditorPluginID {
			continue
		}
		fallback = append(fallback, entry)
	}
	registry := pluginRegistry{Entries: fallback}
	return registry.modelLabelBadge(provider, modelID, modelName)
}

func resetModelLabelStoreForTest(s *appState) {
	if s == nil || s.modelLabels == nil {
		return
	}
	s.modelLabels.refresh()
}

func pluginRegistryEntriesForLookup(s *appState) []pluginRegistryEntry {
	if s == nil {
		return nil
	}
	return s.pluginRegistrySnapshot().Entries
}

func modelReleaseLabelPluginRegistered(entries []pluginRegistryEntry) bool {
	for _, entry := range entries {
		if entry.ID == modelLabelEditorPluginID && entry.Status == pluginRegistered {
			return true
		}
	}
	return false
}

func (store *modelLabelStore) badge(provider, modelID string) string {
	store.ensureLoaded()
	store.mu.RLock()
	defer store.mu.RUnlock()
	if store.warning != "" {
		return ""
	}
	return store.labels[manualModelIdentity(provider, modelID)]
}

func (store *modelLabelStore) loadWarning() string {
	store.ensureLoaded()
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.warning
}

func (store *modelLabelStore) ensureLoaded() {
	store.mu.RLock()
	if store.loaded {
		store.mu.RUnlock()
		return
	}
	store.mu.RUnlock()
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.loaded {
		return
	}
	labels, warning := readManualModelLabels(store.dir)
	store.labels = labels
	store.warning = warning
	store.loaded = true
}

func (store *modelLabelStore) refresh() {
	store.mu.Lock()
	defer store.mu.Unlock()
	labels, warning := readManualModelLabels(store.dir)
	store.labels = labels
	store.warning = warning
	store.loaded = true
}

func readManualModelLabels(dir string) (map[manualModelLabelKey]string, string) {
	filename := filepath.Join(dir, modelLabelEditorFilename)
	data, err := os.ReadFile(filename)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[manualModelLabelKey]string{}, ""
		}
		return map[manualModelLabelKey]string{}, err.Error()
	}
	var file manualModelLabelFile
	if err := json.Unmarshal(data, &file); err != nil {
		return map[manualModelLabelKey]string{}, err.Error()
	}
	if file.SchemaVersion != modelLabelEditorSchema {
		return map[manualModelLabelKey]string{}, fmt.Sprintf("schema version %d, want %d",
			file.SchemaVersion, modelLabelEditorSchema)
	}
	labels := make(map[manualModelLabelKey]string, len(file.Labels))
	for _, label := range file.Labels {
		provider := strings.TrimSpace(label.Provider)
		modelID := strings.TrimSpace(label.ModelID)
		badge := strings.TrimSpace(label.Badge)
		if provider == "" || modelID == "" || !modelLabelBadgeValid(badge) {
			return map[manualModelLabelKey]string{}, fmt.Sprintf("invalid manual label: %#v", label)
		}
		labels[manualModelLabelKey{provider: provider, modelID: modelID}] = badge
	}
	return labels, ""
}

func (store *modelLabelStore) set(provider, modelID, badge string) (bool, error) {
	provider = strings.TrimSpace(provider)
	modelID = strings.TrimSpace(modelID)
	badge = strings.TrimSpace(badge)
	if provider == "" || modelID == "" {
		return false, errModelLabelEditorIdentity
	}
	if !modelLabelBadgeValid(badge) {
		return false, errModelLabelEditorBadge
	}
	store.ensureLoaded()
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.warning != "" {
		return false, errModelLabelEditorDataState
	}
	key := manualModelIdentity(provider, modelID)
	replaced := false
	if _, exists := store.labels[key]; exists {
		replaced = true
	}
	next := cloneManualModelLabels(store.labels)
	next[key] = badge
	if err := writeManualModelLabels(store.dir, next); err != nil {
		return false, errModelLabelEditorStore
	}
	store.labels = next
	return replaced, nil
}

func (store *modelLabelStore) remove(provider, modelID string) (bool, error) {
	provider = strings.TrimSpace(provider)
	modelID = strings.TrimSpace(modelID)
	if provider == "" || modelID == "" {
		return false, errModelLabelEditorIdentity
	}
	store.ensureLoaded()
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.warning != "" {
		return false, errModelLabelEditorDataState
	}
	key := manualModelIdentity(provider, modelID)
	if _, exists := store.labels[key]; !exists {
		return false, nil
	}
	next := cloneManualModelLabels(store.labels)
	delete(next, key)
	if err := writeManualModelLabels(store.dir, next); err != nil {
		return false, errModelLabelEditorStore
	}
	store.labels = next
	return true, nil
}

func cloneManualModelLabels(labels map[manualModelLabelKey]string) map[manualModelLabelKey]string {
	next := make(map[manualModelLabelKey]string, len(labels))
	for key, value := range labels {
		next[key] = value
	}
	return next
}

func writeManualModelLabels(dir string, labels map[manualModelLabelKey]string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file := manualModelLabelFile{SchemaVersion: modelLabelEditorSchema}
	for key, badge := range labels {
		file.Labels = append(file.Labels, manualModelLabel{
			Provider: key.provider,
			ModelID:  key.modelID,
			Badge:    badge,
		})
	}
	sortManualModelLabels(file.Labels)
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteManualModelLabelFile(filepath.Join(dir, modelLabelEditorFilename), data)
}

func atomicWriteManualModelLabelFile(filename string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(filename), ".manual-labels-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	remove := true
	defer func() {
		_ = temp.Close()
		if remove {
			_ = os.Remove(tempName)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}

	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, filename); err != nil {
		return err
	}
	remove = false
	return nil
}

func sortManualModelLabels(labels []manualModelLabel) {
	sort.SliceStable(labels, func(i, j int) bool {
		left, right := labels[i], labels[j]
		if left.Provider != right.Provider {
			return left.Provider < right.Provider
		}
		return left.ModelID < right.ModelID
	})
}

func manualModelIdentity(provider, modelID string) manualModelLabelKey {
	return manualModelLabelKey{
		provider: strings.TrimSpace(provider),
		modelID:  strings.TrimSpace(modelID),
	}
}
func modelLabelBadgeValid(badge string) bool {
	if badge == "" || len([]rune(badge)) > modelLabelBadgeMaxRunes {
		return false
	}
	for _, value := range badge {
		if unicode.IsControl(value) || !unicode.IsGraphic(value) {
			return false
		}
	}
	return true
}

func (s *appState) modelLabelEditorSection(c *ui.Context, p palette, entry pluginRegistryEntry) {
	if entry.ID != modelLabelEditorPluginID || entry.Status != pluginRegistered {
		return
	}
	state := getModelLabelEditorState(s)
	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, text("modelLabelEditorTitle")).FontSize(fontBody).
			FontWeight(650).TextColor(p.text)
		ui.Text(c, text("modelLabelEditorDescription")).FontSize(fontCaption).
			LineHeight(1.4).TextColor(p.muted)
		if s.modelLabels.loadWarning() != "" {
			ui.Text(c, text("modelLabelEditorDataError")).FontSize(fontCaption).LineHeight(1.4).
				TextColor(p.warningText)
		}
		ui.Row(c).Height(36).Padding(0, 10).Radius(
			runtimeTokens.Dimension.Radius["input"].Value,
		).Border(runtimeTokens.Dimension.Border.Value, p.border).
			Background(p.composerSurface).AlignItems(ui.Center).Children(func() {
			ui.TextInputBase(c.Key("model-label-search"), &state.search).
				Grow(1).Placeholder(text("modelLabelEditorSearch")).
				Label(text("modelLabelEditorSearch")).FontSize(fontBody).
				TextColor(p.text).Background(ui.Color{}).
				Border(0, ui.Color{}).Padding(0)
		})
		ui.Scroll(c).FillWidth().MaxHeight(180).Children(func() {
			ui.Column(c).Gap(4).Children(func() {
				query := strings.ToLower(strings.TrimSpace(state.search))
				for _, model := range s.piModels {
					model := model
					label := model.Provider + " · " + model.ID
					if query != "" && !strings.Contains(strings.ToLower(label), query) {
						continue
					}
					selected := state.provider == model.Provider && state.modelID == model.ID
					pluginSettingOption(c, p, "model-label-"+model.Provider+"\x00"+model.ID,
						label, selected, func() {
							state.selectModel(s, model)
							if s.window != nil {
								s.window.Invalidate()
							}
						})
				}
			})
		})
		if state.provider != "" || state.modelID != "" {
			ui.Text(c, textArgs("modelLabelEditorSelected", map[string]any{
				"provider": state.provider,
				"model":    state.modelID,
			})).FontSize(fontCaption).TextColor(p.muted)
		}
		ui.Row(c).Height(36).Padding(0, 10).Radius(
			runtimeTokens.Dimension.Radius["input"].Value,
		).Border(runtimeTokens.Dimension.Border.Value, p.border).
			Background(p.composerSurface).AlignItems(ui.Center).Children(func() {
			ui.TextInputBase(c.Key("model-label-badge"), &state.badge).
				Grow(1).Placeholder(text("modelLabelEditorBadgePlaceholder")).
				Label(text("modelLabelEditorBadge")).FontSize(fontBody).
				TextColor(p.text).Background(ui.Color{}).
				Border(0, ui.Color{}).Padding(0)
		})
		ui.Row(c).Gap(6).Children(func() {
			pluginSecondaryAction(c, p, "model-label-save", text("modelLabelEditorSave"), func() {
				state.save(s)
			})
			pluginSecondaryAction(c, p, "model-label-remove", text("modelLabelEditorRemove"), func() {
				state.remove(s)
			})
		})
		if state.status != "" {
			color := p.successText
			if state.statusErr {
				color = p.dangerText
			}
			ui.Text(c, state.status).FontSize(fontCaption).LineHeight(1.4).
				TextColor(color)
		}
	})
}
