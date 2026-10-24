package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// channelSettingsAPI is the complete backend surface consumed by the channel
// settings renderer. Plans bind previews to exact file versions; the UI passes
// them back unchanged and performs no credential or configuration writes.
type channelSettingsAPI interface {
	Inventory() (ChannelInventory, error)
	InspectModelsFile(path string) (ChannelInventory, error)
	PrepareImport(req ChannelImportRequest) (ChannelImportPlan, error)
	ApplyImport(req ChannelImportRequest) (ChannelMergeResult, error)
	PrepareCustom(req ChannelCustomRequest) (ChannelCustomPlan, error)
	ApplyCustom(req ChannelCustomRequest) (ChannelMergeResult, error)
}

type channelSettingsPicker func(parent *mygo.Window) (string, error)

// channelSettingsRefreshBridge is the seam owned by channel_refresh.go and
// the app lifecycle. A nil production field is replaced by the appState
// methods; tests can inject a deterministic local status.
type channelSettingsRefreshBridge struct {
	Status      func(*appState) ChannelRefreshStatus
	MarkPending func(*appState)
	Retry       func(*appState)
}

type channelSettingsSourceMode string

const (
	channelSettingsSourceNone   channelSettingsSourceMode = ""
	channelSettingsSourcePreset channelSettingsSourceMode = "preset"
	channelSettingsSourceCustom channelSettingsSourceMode = "custom"
	channelSettingsSourceModels channelSettingsSourceMode = "models"
)

type channelSettingsPreviewKind string

const (
	channelSettingsPreviewImport channelSettingsPreviewKind = "import"
	channelSettingsPreviewCustom channelSettingsPreviewKind = "custom"
)

type channelSettingsUIState struct {
	api     channelSettingsAPI
	picker  channelSettingsPicker
	refresh channelSettingsRefreshBridge
	initErr error

	inventory        ChannelInventory
	inventoryLoaded  bool
	inventoryLoading bool
	inventoryErr     string
	inventoryDetails bool

	mode         channelSettingsSourceMode
	presetID     string
	presetKeyEnv string
	providerID   string
	providerName string
	baseURL      string
	apiName      string
	apiKeyEnv    string
	authHeader   bool
	modelID      string
	modelName    string
	overrideID   string
	overrideName string

	sourcePath        string
	sourceInventory   ChannelInventory
	sourceProviderID  string
	sourceErr         string
	selectedModels    map[string]bool
	selectedOverrides map[string]bool

	importPlan       *ChannelImportPlan
	customPlan       *ChannelCustomPlan
	preview          *ChannelMergePreview
	previewKind      channelSettingsPreviewKind
	previewErr       string
	previewErrDetail string
	applyErr         string
	applyErrDetail   string
	stale            bool
	result           *ChannelMergeResult

	diagnosticsOpen    bool
	presetAdvancedOpen bool
	providerDetails    map[string]bool
}

var (
	channelSettingsStates sync.Map
	channelSettingsNewAPI = func() (channelSettingsAPI, error) {
		return NewChannelConfigService("")
	}
)

func defaultChannelSettingsPicker(parent *mygo.Window) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: parent,
		Title:  text("channelModelsFileDialog"),
		Filters: []mygo.FileFilter{{
			Name:       "models.json",
			Extensions: []string{"json"},
		}},
	})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

func defaultChannelSettingsRefreshBridge() channelSettingsRefreshBridge {
	return channelSettingsRefreshBridge{
		Status: func(s *appState) ChannelRefreshStatus {
			return s.channelRefreshStatus()
		},
		MarkPending: func(s *appState) {
			s.markChannelRefreshPending()
		},
		Retry: func(s *appState) {
			s.retryChannelRefresh()
		},
	}
}

func channelSettingsState(s *appState) *channelSettingsUIState {
	if value, ok := channelSettingsStates.Load(s); ok {
		return value.(*channelSettingsUIState)
	}
	state := &channelSettingsUIState{
		picker:            defaultChannelSettingsPicker,
		refresh:           defaultChannelSettingsRefreshBridge(),
		selectedModels:    map[string]bool{},
		selectedOverrides: map[string]bool{},
		providerDetails:   map[string]bool{},
	}
	state.api, state.initErr = channelSettingsNewAPI()
	value, _ := channelSettingsStates.LoadOrStore(s, state)
	return value.(*channelSettingsUIState)
}

func resetChannelSettingsForTest(s *appState) {
	if s != nil {
		channelSettingsStates.Delete(s)
	}
}

func installChannelSettingsTestDependencies(
	s *appState,
	api channelSettingsAPI,
	picker channelSettingsPicker,
	refresh channelSettingsRefreshBridge,
) *channelSettingsUIState {
	state := &channelSettingsUIState{
		api:               api,
		picker:            picker,
		refresh:           refresh,
		selectedModels:    map[string]bool{},
		selectedOverrides: map[string]bool{},
		providerDetails:   map[string]bool{},
	}
	channelSettingsStates.Store(s, state)
	return state
}

func (s *appState) channelSettingsContent(c *ui.Context, p palette) ui.Element {
	state := channelSettingsState(s)
	if !state.inventoryLoading && !state.inventoryLoaded && state.inventoryErr == "" {
		state.loadInventory()
	}
	return ui.Column(c).Key("channel-settings").FillWidth().MinWidth(0).Gap(12).
		Children(func() {
			s.channelSettingsHeader(c, p)
			s.channelSettingsSources(c, p, state)
			s.channelSettingsInventory(c, p, state)
			s.channelSettingsForm(c, p, state)
			s.channelSettingsPreview(c, p, state)
			s.channelSettingsResult(c, p, state)
			s.channelSettingsRefresh(c, p, state)
		})
}

func (s *appState) channelSettingsHeader(c *ui.Context, p palette) {
	ui.Column(c).FillWidth().Children(func() {
		ui.Text(c, text("channelSettingsTitle")).FontSize(fontTitle).FontWeight(650).
			TextColor(p.text)
	})
}

func (s *appState) channelSettingsInventory(
	c *ui.Context,
	p palette,
	state *channelSettingsUIState,
) {
	settingsCardV12(c, p, text("channelInventoryTitle"), "", func() {
		ui.Accordion(c, func() {
			open := state.inventoryDetails
			item := ui.AccordionItem(c, text("channelInventoryDiagnostics"), &open, func() {
				settingsValueV12(c, p, text("channelPiDirectory"), fallback(state.inventory.AgentDir, "—"))
				settingsValueV12(c, p, text("channelModelsFile"), fallback(state.inventory.ConfigPath, "—"))
			})
			if item.Changed() {
				state.inventoryDetails = open
			}
		})
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, state.inventoryStatusLabel()).FontSize(fontBody).
				TextColor(p.text).Grow(1).MinWidth(160).Wrap()
			s.channelSettingsButton(c, p, "channel-inventory-refresh",
				text("channelRefreshInventory"), "activity", false, !state.inventoryLoading,
				func() {
					state.loadInventory()
				})
		})
		if state.inventoryErr != "" {
			s.channelSettingsError(c, p, "channel-inventory-error",
				textArgs("channelInventoryReadFailed", map[string]any{"detail": state.inventoryErr}))
			return
		}
		if !state.inventoryLoaded {
			ui.Text(c, text("channelInventoryLoading")).FontSize(fontSmall).
				TextColor(p.muted)
			return
		}
		if !state.inventory.Exists || len(state.inventory.Providers) == 0 {
			ui.Column(c).FillWidth().Padding(12).Radius(v12InputRadius).
				Background(p.background).Children(func() {
				ui.Text(c, text("channelInventoryEmptyTitle")).FontSize(fontBody).
					FontWeight(600).TextColor(p.text)
			})
			return
		}
		if state.providerDetails == nil {
			state.providerDetails = map[string]bool{}
		}
		for _, provider := range state.inventory.Providers {
			s.channelSettingsProviderSummary(c, p, state, provider)
		}
	})
}

func (s *appState) channelSettingsProviderSummary(
	c *ui.Context,
	p palette,
	state *channelSettingsUIState,
	provider ChannelProviderSummary,
) {
	displayName := fallback(provider.Name, provider.ID)
	detailsOpen := state.providerDetails[provider.ID]
	ui.Column(c).Key("channel-inventory-provider-"+provider.ID).FillWidth().Gap(8).
		Padding(10).Radius(v12InputRadius).Background(p.background).
		Border(v12BorderWidth, p.border).Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, displayName).FontSize(fontBody).
				FontWeight(650).TextColor(p.text).MinWidth(120).Wrap()
			if provider.ID != "" && provider.ID != displayName {
				ui.Text(c, provider.ID).Font("monospace").FontSize(fontSmall).
					TextColor(p.muted).Wrap()
			}
			ui.Spacer(c).Grow(1)
			ui.Text(c, channelCredentialLabel(provider.Credential)).FontSize(fontSmall).
				TextColor(p.muted).Wrap()
			ui.Text(c, textArgs("channelModelsCount", map[string]any{
				"count": len(provider.Models),
			})).FontSize(fontSmall).TextColor(p.muted).Wrap()
			detailsLabel := text("channelDetails")
			details := ui.ButtonBase(c.Key("channel-inventory-details-"+provider.ID)).
				Label(detailsLabel).Padding(5, 8).Radius(v12ChipRadius).
				Border(v12BorderWidth, p.border).Background(p.surface).
				Cursor(ui.CursorPointer)
			if details.Hovered() {
				details.Background(p.surfaceHover)
			}
			details.Children(func() {
				ui.Text(c, detailsLabel).FontSize(fontSmall).FontWeight(600).
					TextColor(p.text)
			})
			if details.Clicked() {
				state.providerDetails[provider.ID] = !detailsOpen
			}
		})
		if detailsOpen {
			ui.Column(c).Key("channel-inventory-provider-details-"+provider.ID).
				FillWidth().Gap(6).Padding(8, 0, 0, 0).Children(func() {
				if provider.API != "" {
					settingsValueV12(c, p, text("channelAPI"), provider.API)
				}
				if provider.BaseURL != "" {
					settingsValueV12(c, p, text("channelBaseURL"), provider.BaseURL)
				}
				if len(provider.Models) > 0 {
					s.channelSettingsModelChips(c, p, text("channelModels"), provider.Models)
				}
				if len(provider.ModelOverride) > 0 {
					s.channelSettingsModelChips(c, p, text("channelModelOverrides"), provider.ModelOverride)
				}
			})
		}
	})
}

func (s *appState) channelSettingsModelChips(
	c *ui.Context,
	p palette,
	label string,
	models []ChannelModelSummary,
) {
	ui.Column(c).FillWidth().Gap(5).Children(func() {
		ui.Text(c, label).FontSize(fontSmall).FontWeight(650).TextColor(p.muted)
		ui.Row(c).FillWidth().Wrap().Gap(6).Children(func() {
			for _, model := range models {
				ui.Column(c).Key("channel-model-"+model.ID).Shrink(0).Padding(5, 7).
					Radius(v12ChipRadius).Background(p.surfaceSubtle).Gap(1).Children(func() {
					ui.Text(c, fallback(model.Name, model.ID)).FontSize(fontSmall).
						FontWeight(600).TextColor(p.text).Wrap()
					ui.Text(c, model.ID).Font("monospace").FontSize(fontSmall).
						TextColor(p.muted).Wrap()
				})
			}
		})
	})
}

func (s *appState) channelSettingsSources(
	c *ui.Context,
	p palette,
	state *channelSettingsUIState,
) {
	settingsCardV12(c, p, text("channelAddSourceTitle"), "", func() {
		ui.Row(c).FillWidth().Wrap().Gap(8).Children(func() {
			s.channelSettingsModeButton(c, p, state, channelSettingsSourcePreset,
				text("channelSourcePreset"))
			s.channelSettingsModeButton(c, p, state, channelSettingsSourceCustom,
				text("channelSourceCustom"))
			s.channelSettingsModeButton(c, p, state, channelSettingsSourceModels,
				text("channelSourceModelsFile"))
		})
	})
}

func (s *appState) channelSettingsModeButton(
	c *ui.Context,
	p palette,
	state *channelSettingsUIState,
	mode channelSettingsSourceMode,
	label string,
) {
	button := ui.ButtonBase(c.Key("channel-source-"+string(mode))).Label(label).
		Padding(7, 11).Radius(v12InputRadius).Border(v12BorderWidth,
		channelBorderForSelection(p, state.mode == mode)).Background(p.surface).
		Cursor(ui.CursorPointer)
	if button.Hovered() {
		button.Background(p.surfaceHover)
	}
	button.Children(func() {
		ui.Text(c, label).FontSize(fontBody).FontWeight(600).
			TextColor(channelTextForSelection(p, state.mode == mode))
	})
	if button.Clicked() {
		state.switchMode(mode)
	}
}

func (s *appState) channelSettingsForm(
	c *ui.Context,
	p palette,
	state *channelSettingsUIState,
) {
	switch state.mode {
	case channelSettingsSourceModels:
		s.channelSettingsFileForm(c, p, state)
	case channelSettingsSourcePreset:
		s.channelSettingsPresetForm(c, p, state)
	case channelSettingsSourceCustom:
		s.channelSettingsCustomForm(c, p, state)
	}
}

func (s *appState) channelSettingsFileForm(
	c *ui.Context,
	p palette,
	state *channelSettingsUIState,
) {
	settingsCardV12(c, p, text("channelSourceModelsFile"), text("channelModelsFileDescription"), func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
			s.channelSettingsButton(c, p, "channel-choose-models-file",
				text("channelChooseModelsFile"), "folder", false, true,
				func() {
					state.chooseModelsFile(s)
				})
			if state.sourcePath != "" {
				ui.Text(c, state.sourcePath).Font("monospace").FontSize(fontSmall).
					TextColor(p.muted).Grow(1).MinWidth(160).Wrap()
			}
		})
		if state.sourceErr != "" {
			s.channelSettingsError(c, p, "channel-source-file-error",
				textArgs("channelModelsFileReadFailed", map[string]any{"detail": state.sourceErr}))
			if state.sourcePath != "" {
				s.channelSettingsButton(c, p, "channel-retry-source-file",
					text("channelRetryReadFile"), "activity", false, true,
					func() {
						state.inspectSourceFile()
					})
			}
			return
		}
		if len(state.sourceInventory.Providers) == 0 {
			ui.Text(c, text("channelModelsFileAwaiting")).FontSize(fontSmall).
				TextColor(p.muted)
			return
		}
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Text(c, text("channelSelectProvider")).FontSize(fontBody).
				FontWeight(650).TextColor(p.text)
			ui.Row(c).FillWidth().Wrap().Gap(6).Children(func() {
				for _, provider := range state.sourceInventory.Providers {
					label := fallback(provider.Name, provider.ID) + " " + provider.ID
					selected := provider.ID == state.sourceProviderID
					button := ui.ButtonBase(c.Key("channel-source-provider-"+provider.ID)).
						Label(label).Padding(6, 9).Radius(v12ChipRadius).
						Border(v12BorderWidth, channelBorderForSelection(p, selected)).
						Background(p.surface).Cursor(ui.CursorPointer)
					if button.Hovered() {
						button.Background(p.surfaceHover)
					}
					button.Children(func() {
						ui.Text(c, label).FontSize(fontBody).
							TextColor(channelTextForSelection(p, selected)).Wrap()
					})
					if button.Clicked() {
						state.selectProvider(provider.ID)
					}
				}
			})
		})
		provider, ok := state.selectedSourceProvider()
		if !ok {
			return
		}
		if len(provider.Models) > 0 {
			s.channelSettingsCheckboxList(c, p, text("channelSelectModels"),
				"channel-source-model-", provider.ID, provider.Models, state.selectedModels, state)
		}
		if len(provider.ModelOverride) > 0 {
			s.channelSettingsCheckboxList(c, p, text("channelSelectOverrides"),
				"channel-source-override-", provider.ID, provider.ModelOverride, state.selectedOverrides, state)
		}
		ui.Row(c).FillWidth().Wrap().Gap(8).Children(func() {
			s.channelSettingsButton(c, p, "channel-preview-import",
				text("channelPreviewImport"), "search", true, true,
				func() {
					state.prepareImport()
				})
		})
	})
}

func (s *appState) channelSettingsCheckboxList(
	c *ui.Context,
	p palette,
	label, keyPrefix, providerID string,
	models []ChannelModelSummary,
	selected map[string]bool,
	state *channelSettingsUIState,
) {
	ui.Column(c).FillWidth().Gap(5).Children(func() {
		ui.Text(c, label).FontSize(fontBody).FontWeight(650).TextColor(p.text)
		for _, model := range models {
			checked := selected[model.ID]
			modelLabel := fallback(model.Name, model.ID) + " (" + model.ID + ")"
			checkbox := ui.Checkbox(c.Key(keyPrefix+providerID+"-"+model.ID),
				&checked, modelLabel).FontSize(fontBody)
			if checkbox.Changed() {
				selected[model.ID] = checked
				if state != nil {
					state.invalidatePreview()
				}
			}
		}
	})
}

func (s *appState) channelSettingsPresetForm(
	c *ui.Context,
	p palette,
	state *channelSettingsUIState,
) {
	settingsCardV12(c, p, text("channelSourcePreset"), text("channelPresetDescription"), func() {
		ui.Row(c).FillWidth().Wrap().Gap(6).Children(func() {
			for _, preset := range ChannelPresets() {
				preset := preset
				selected := state.presetID == preset.ID
				button := ui.ButtonBase(c.Key("channel-preset-"+preset.ID)).
					Label(preset.Name).Padding(6, 9).Radius(v12ChipRadius).
					Border(v12BorderWidth, channelBorderForSelection(p, selected)).
					Background(p.surface).Cursor(ui.CursorPointer)
				if button.Hovered() {
					button.Background(p.surfaceHover)
				}
				button.Children(func() {
					ui.Text(c, preset.Name).FontSize(fontBody).
						TextColor(channelTextForSelection(p, selected))
				})
				if button.Clicked() {
					state.selectPreset(preset)
				}
			}
		})
		preset, ok := channelPresetByID(state.presetID)
		if !ok {
			ui.Text(c, text("channelSelectPresetFirst")).FontSize(fontSmall).
				TextColor(p.muted)
			return
		}
		if preset.UsesPiLogin {
			ui.Column(c).FillWidth().Padding(10).Radius(v12InputRadius).
				Background(p.background).Gap(3).Children(func() {
				ui.Text(c, text("channelPiLoginManagedTitle")).FontSize(fontBody).
					FontWeight(600).TextColor(p.text)
				ui.Text(c, text("channelPiLoginManagedDescription")).FontSize(fontSmall).
					LineHeight(1.4).TextColor(p.muted).Wrap()
			})
		}
		ui.Collapsible(c, text("channelAdvanced"), &state.presetAdvancedOpen, func() {
			s.channelSettingsTextField(c, p, "channel-preset-api-key-env",
				text("channelAPIKeyEnvironment"), text("channelAPIKeyEnvironmentHint"),
				&state.presetKeyEnv, state)
		})
		s.channelSettingsModelFields(c, p, state, true)
		ui.Row(c).FillWidth().Wrap().Gap(8).Children(func() {
			s.channelSettingsButton(c, p, "channel-preview-preset",
				text("channelPreviewPreset"), "search", true, true,
				func() {
					state.preparePreset()
				})
		})
	})
}

func (s *appState) channelSettingsCustomForm(
	c *ui.Context,
	p palette,
	state *channelSettingsUIState,
) {
	settingsCardV12(c, p, text("channelSourceCustom"), text("channelCustomDescription"), func() {
		s.channelSettingsTextField(c, p, "channel-custom-provider-id",
			text("channelProviderID"), text("channelProviderIDHint"), &state.providerID, state)
		s.channelSettingsTextField(c, p, "channel-custom-provider-name",
			text("channelProviderName"), text("channelProviderNameHint"), &state.providerName, state)
		s.channelSettingsTextField(c, p, "channel-custom-base-url",
			text("channelBaseURL"), text("channelBaseURLHint"), &state.baseURL, state)
		s.channelSettingsTextField(c, p, "channel-custom-api",
			text("channelAPI"), text("channelAPIHint"), &state.apiName, state)
		s.channelSettingsTextField(c, p, "channel-custom-api-key-env",
			text("channelAPIKeyEnvironment"), text("channelAPIKeyEnvironmentHint"), &state.apiKeyEnv, state)
		authHeader := state.authHeader
		auth := ui.Checkbox(c.Key("channel-custom-auth-header"),
			&authHeader, text("channelUseAuthorizationHeader")).FontSize(fontBody)
		if auth.Changed() {
			state.authHeader = authHeader
			state.invalidatePreview()
		}
		s.channelSettingsModelFields(c, p, state, false)
		ui.Row(c).FillWidth().Wrap().Gap(8).Children(func() {
			s.channelSettingsButton(c, p, "channel-preview-custom",
				text("channelPreviewCustom"), "search", true, true,
				func() {
					state.prepareCustom()
				})
		})
	})
}

func (s *appState) channelSettingsModelFields(
	c *ui.Context,
	p palette,
	state *channelSettingsUIState,
	preset bool,
) {
	prefix := "channel-custom"
	if preset {
		prefix = "channel-preset"
	}
	ui.Text(c, text("channelModelEntryTitle")).FontSize(fontBody).
		FontWeight(650).TextColor(p.text)
	s.channelSettingsTextField(c, p, prefix+"-model-id",
		text("channelModelID"), text("channelModelIDHint"), &state.modelID, state)
	s.channelSettingsTextField(c, p, prefix+"-model-name",
		text("channelModelName"), text("channelModelNameHint"), &state.modelName, state)
	ui.Text(c, text("channelOverrideEntryTitle")).FontSize(fontBody).
		FontWeight(650).TextColor(p.text)
	s.channelSettingsTextField(c, p, prefix+"-override-id",
		text("channelOverrideID"), text("channelOverrideIDHint"), &state.overrideID, state)
	s.channelSettingsTextField(c, p, prefix+"-override-name",
		text("channelOverrideName"), text("channelOverrideNameHint"), &state.overrideName, state)
}

func (s *appState) channelSettingsPreview(
	c *ui.Context,
	p palette,
	state *channelSettingsUIState,
) {
	if state.previewErr != "" {
		settingsCardV12(c, p, text("channelPreviewErrorTitle"), "", func() {
			s.channelSettingsError(c, p, "channel-preview-error", state.previewErr)
			if state.previewErrDetail != "" {
				ui.Text(c, state.previewErrDetail).Font("monospace").FontSize(fontSmall).
					TextColor(p.muted).Wrap()
			}
			if state.stale {
				s.channelSettingsButton(c, p, "channel-repreview",
					text("channelRepreview"), "activity", true, true,
					func() {
						state.reprepare()
					})
			}
		})
	}
	if state.preview == nil {
		return
	}
	preview := *state.preview
	settingsCardV12(c, p, text("channelPreviewTitle"), text("channelPreserveConflictDescription"), func() {
		ui.Row(c).FillWidth().Wrap().Gap(8).Children(func() {
			providerColor := p.successText
			if preview.ProviderAction == channelMergePreserve {
				providerColor = p.muted
			}
			s.channelSettingsBadge(c, p, textArgs("channelProviderAction",
				map[string]any{
					"provider": channelDisplayName(preview.ProviderID),
					"action":   channelMergeActionLabel(preview.ProviderAction),
				}), providerColor)
			s.channelSettingsBadge(c, p, text("channelConflictPolicyPreserve"), p.muted)
		})
		if len(preview.Models) > 0 {
			s.channelSettingsMergeItems(c, p, text("channelModels"), preview.Models)
		}
		if len(preview.ModelOverrides) > 0 {
			s.channelSettingsMergeItems(c, p, text("channelModelOverrides"), preview.ModelOverrides)
		}
		for _, conflict := range preview.Conflicts {
			ui.Column(c).Key("channel-conflict-"+conflict.Path).FillWidth().Gap(3).
				Padding(9, 10).Radius(v12InputRadius).Background(p.background).Children(func() {
				ui.Text(c, textArgs("channelConflictSummary", map[string]any{
					"path": conflict.Path,
				})).FontSize(fontBody).FontWeight(600).TextColor(p.warningText).Wrap()
				if len(conflict.Fields) > 0 {
					ui.Text(c, strings.Join(conflict.Fields, "\n")).Font("monospace").
						FontSize(fontSmall).TextColor(p.muted).Wrap()
				}
			})
		}
		for _, notice := range preview.Notices {
			s.channelSettingsNotice(c, p, notice)
		}
		ui.Accordion(c, func() {
			open := state.diagnosticsOpen
			item := ui.AccordionItem(c, text("channelPreviewDiagnostics"), &open, func() {
				ui.Row(c).FillWidth().Wrap().GapX(16).GapY(6).Children(func() {
					s.channelSettingsDigest(c, p, text("channelPreviewSource"),
						preview.Source.Path, preview.Source.Digest)
					s.channelSettingsDigest(c, p, text("channelPreviewDestination"),
						preview.Destination.Path, preview.Destination.Digest)
				})
			})
			if item.Changed() {
				state.diagnosticsOpen = open
			}
		})
		if state.stale {
			s.channelSettingsError(c, p, "channel-preview-stale",
				text("channelPreviewStaleDescription"))
			s.channelSettingsButton(c, p, "channel-repreview",
				text("channelRepreview"), "activity", true, true,
				func() {
					state.reprepare()
				})
			return
		}
		ui.Row(c).FillWidth().Wrap().Gap(8).Children(func() {
			label := text("channelApplyImport")
			key := "channel-apply-import"
			if state.previewKind == channelSettingsPreviewCustom {
				label = text("channelApplyCustom")
				key = "channel-apply-custom"
			}
			s.channelSettingsButton(c, p, key, label, "check", true,
				state.planBound(), func() {
					state.apply(s)
				})
		})
		if state.applyErr != "" {
			s.channelSettingsError(c, p, "channel-apply-error", state.applyErr)
			if state.applyErrDetail != "" {
				ui.Text(c, state.applyErrDetail).Font("monospace").FontSize(fontSmall).
					TextColor(p.muted).Wrap()
			}
		}
	})
}

func (s *appState) channelSettingsDigest(
	c *ui.Context,
	p palette,
	label, path, digest string,
) {
	ui.Column(c).Grow(1).MinWidth(180).Gap(2).Children(func() {
		ui.Text(c, label).FontSize(fontSmall).FontWeight(650).TextColor(p.muted)
		ui.Text(c, fallback(path, "—")).Font("monospace").FontSize(fontSmall).
			TextColor(p.text).Wrap()
		ui.Text(c, channelShortDigest(digest)).Font("monospace").FontSize(fontSmall).
			TextColor(p.muted).Wrap()
	})
}

func (s *appState) channelSettingsMergeItems(
	c *ui.Context,
	p palette,
	label string,
	items []ChannelMergeItem,
) {
	ui.Column(c).FillWidth().Gap(5).Children(func() {
		ui.Text(c, label).FontSize(fontBody).FontWeight(650).TextColor(p.text)
		for _, item := range items {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, item.ID).Font("monospace").FontSize(fontBody).
					TextColor(p.text).Grow(1).MinWidth(120).Wrap()
				color := p.successText
				if item.Action == channelMergePreserve {
					color = p.muted
				}
				s.channelSettingsBadge(c, p, channelMergeActionLabel(item.Action), color)
			})
		}
	})
}

func (s *appState) channelSettingsNotice(
	c *ui.Context,
	p palette,
	notice ChannelNotice,
) {
	ui.Row(c).Key("channel-notice-"+notice.Code+"-"+notice.Path).FillWidth().
		Wrap().AlignItems(ui.Center).Gap(8).Padding(8, 9).Radius(v12InputRadius).
		Background(p.background).Children(func() {
		s.channelSettingsBadge(c, p, channelNoticeLabel(notice.Code), p.muted)
		ui.Text(c, notice.Path).Font("monospace").FontSize(fontSmall).
			TextColor(p.muted).Grow(1).MinWidth(150).Wrap()
		ui.Text(c, channelMergeActionLabel(notice.Action)).FontSize(fontSmall).
			TextColor(p.muted).Wrap()
	})
}

func (s *appState) channelSettingsBadge(
	c *ui.Context,
	p palette,
	label string,
	color ui.Color,
) {
	ui.Row(c).Shrink(0).Padding(3, 6).Radius(v12ChipRadius).
		Background(p.surfaceSubtle).Children(func() {
		ui.Text(c, label).FontSize(fontSmall).FontWeight(650).TextColor(color).Wrap()
	})
}

func (s *appState) channelSettingsResult(
	c *ui.Context,
	p palette,
	state *channelSettingsUIState,
) {
	if state.result == nil {
		return
	}
	settingsCardV12(c, p, text("channelApplyResultTitle"), "", func() {
		label := text("channelApplyNoChanges")
		if state.result.Changed {
			label = text("channelApplySucceeded")
		}
		ui.Text(c, label).FontSize(fontBody).FontWeight(650).TextColor(p.successText).Wrap()
		ui.Text(c, text("channelPendingRefreshDescription")).FontSize(fontSmall).
			LineHeight(1.4).TextColor(p.muted).Wrap()
		ui.Row(c).FillWidth().Wrap().Gap(8).Children(func() {
			s.channelSettingsButton(c, p, "channel-new-import",
				text("channelAddAnotherSource"), "plus", false, true,
				func() {
					state.resetForAnotherSource()
				})
		})
	})
}

func (s *appState) channelSettingsRefresh(
	c *ui.Context,
	p palette,
	state *channelSettingsUIState,
) {
	if state.refresh.Status == nil {
		return
	}
	status := state.refresh.Status(s)
	if status.Phase == channelRefreshPhaseIdle {
		return
	}
	settingsCardV12(c, p, text("channelPiRefreshTitle"), "", func() {
		label, color := channelRefreshStatusLabel(status, p)
		ui.Text(c, label).FontSize(fontBody).FontWeight(600).TextColor(color).Wrap()
		switch {
		case status.WaitingIdle:
			ui.Text(c, text("channelRefreshWaitingIdle")).FontSize(fontSmall).
				LineHeight(1.4).TextColor(p.muted).Wrap()
		case status.Phase == channelRefreshPhaseFailed:
			ui.Text(c, text("channelRefreshRecoveryPreserved")).FontSize(fontSmall).
				LineHeight(1.4).TextColor(p.warningText).Wrap()
		case status.Phase == channelRefreshPhaseSucceeded:
			ui.Text(c, text("channelRefreshReadbackSucceeded")).FontSize(fontSmall).
				LineHeight(1.4).TextColor(p.muted).Wrap()
		}
		if status.Retryable && state.refresh.Retry != nil {
			s.channelSettingsButton(c, p, "channel-refresh-retry",
				text("channelRefreshRetry"), "activity", false, true,
				func() {
					state.refresh.Retry(s)
				})
		}
	})
}

func channelRefreshStatusLabel(status ChannelRefreshStatus, p palette) (string, ui.Color) {
	switch status.Phase {
	case channelRefreshPhaseSucceeded:
		return text("channelRefreshSucceeded"), p.successText
	case channelRefreshPhaseFailed:
		return textArgs("channelRefreshFailed", map[string]any{
			"reason": channelRefreshFailureLabel(status.Failure),
		}), p.dangerText
	case channelRefreshPhaseRunning:
		return text("channelRefreshRunning"), p.text
	case channelRefreshPhasePending:
		return text("channelRefreshPending"), p.warningText
	default:
		return text("channelRefreshIdle"), p.muted
	}
}

func channelRefreshFailureLabel(failure ChannelRefreshFailure) string {
	key := "channelRefreshFailureUnknown"
	switch failure {
	case channelRefreshFailureLaunch:
		key = "channelRefreshFailureLaunch"
	case channelRefreshFailureSessionUnavailable:
		key = "channelRefreshFailureSession"
	case channelRefreshFailureModelUnavailable:
		key = "channelRefreshFailureModel"
	case channelRefreshFailureThinkingUnavailable:
		key = "channelRefreshFailureThinking"
	case channelRefreshFailureReadback:
		key = "channelRefreshFailureReadback"
	case channelRefreshFailureTranscript:
		key = "channelRefreshFailureTranscript"
	}
	return text(key)
}

func (s *appState) channelSettingsError(
	c *ui.Context,
	p palette,
	key, message string,
) {
	ui.Row(c).Key(key).FillWidth().Padding(9, 10).Radius(v12InputRadius).
		Border(v12BorderWidth, p.danger).Background(p.danger.Alpha(.08)).Children(func() {
		ui.Text(c, message).FontSize(fontBody).LineHeight(1.4).
			TextColor(p.dangerText).FillWidth().MinWidth(0).Wrap()
	})
}

func (s *appState) channelSettingsTextField(
	c *ui.Context,
	p palette,
	key, label, placeholder string,
	value *string,
	state *channelSettingsUIState,
) {
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		ui.Text(c, label).FontSize(fontSmall).FontWeight(600).TextColor(p.muted)
		input := ui.TextInputBase(c.Key(key), value).FillWidth().Height(36).
			Padding(0, 10).Radius(v12InputRadius).Border(v12BorderWidth, p.border).
			Background(p.surface).FontSize(fontBody).TextColor(p.text).
			Label(label).Placeholder(placeholder)
		if input.Focused() {
			input.BorderColor(p.focus)
		}
		input.OnChange(func() {
			if state != nil {
				state.invalidatePreview()
			}
		})
	})
}

func (s *appState) channelSettingsButton(
	c *ui.Context,
	p palette,
	key, label, iconName string,
	primary bool,
	enabled bool,
	click func(),
) {
	background := p.surface
	foreground := p.text
	border := p.border
	if primary {
		background = p.accent
		foreground = p.onAccent
		border = p.accent
	}
	if !enabled {
		background = p.surfaceSubtle
		foreground = p.muted
	}
	button := ui.ButtonBase(c.Key(key)).Label(label).Padding(7, 11).
		Radius(v12InputRadius).Border(v12BorderWidth, border).
		Background(background).Cursor(ui.CursorPointer).Disabled(!enabled)
	if button.Hovered() && enabled {
		if primary {
			button.Background(p.accentHover)
		} else {
			button.Background(p.surfaceHover)
		}
	}
	button.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon(iconName)).Size(14, 14).TextColor(foreground)
			ui.Text(c, label).FontSize(fontBody).FontWeight(650).
				TextColor(foreground).Wrap()
		})
	})
	if enabled && button.Clicked() {
		click()
	}
}

func channelBorderForSelection(p palette, selected bool) ui.Color {
	if selected {
		return p.accent
	}
	return p.border
}

func channelTextForSelection(p palette, selected bool) ui.Color {
	if selected {
		return p.accentText
	}
	return p.text
}

func (s *channelSettingsUIState) loadInventory() {
	if s == nil || s.api == nil {
		return
	}
	s.inventoryLoading = true
	inventory, err := s.api.Inventory()
	s.inventoryLoading = false
	s.inventoryLoaded = true
	s.inventory = inventory
	s.inventoryErr = ""
	if err != nil {
		s.inventoryErr = err.Error()
	}
}

func (s *channelSettingsUIState) inventoryStatusLabel() string {
	if s.inventoryLoading {
		return text("channelInventoryLoading")
	}
	if s.inventoryErr != "" {
		return text("channelInventoryError")
	}
	if !s.inventory.Exists {
		return text("channelInventoryMissing")
	}
	return textArgs("channelInventoryProviderCount", map[string]any{
		"count": len(s.inventory.Providers),
	})
}

func channelCredentialLabel(summary ChannelCredentialSummary) string {
	if !summary.Configured || summary.Kind == channelCredentialNone {
		return text("channelCredentialNone")
	}
	return text("channelConfiguredInPi")
}

func channelNoticeLabel(code string) string {
	switch code {
	case channelNoticeConfigureInPi:
		return text("channelNoticeConfigureInPi")
	case channelNoticeProviderEndpointConflict:
		return text("channelNoticeProviderConflict")
	case channelNoticeModelEndpointInherited:
		return text("channelNoticeEndpointInherited")
	case channelNoticeCommentsRemoved:
		return text("channelNoticeCommentsRemoved")
	case "providerPreserved":
		return text("channelNoticeProviderPreserved")
	case "modelConflictPreserved":
		return text("channelNoticeModelPreserved")
	case "modelOverrideConflictPreserved":
		return text("channelNoticeOverridePreserved")
	default:
		return code
	}
}

func (s *channelSettingsUIState) switchMode(mode channelSettingsSourceMode) {
	if s.mode == mode {
		return
	}
	s.mode = mode
	s.invalidatePreview()
	s.applyErr = ""
	s.sourceErr = ""
}

func (s *channelSettingsUIState) chooseModelsFile(app *appState) {
	if s.picker == nil {
		s.sourceErr = text("channelPickerUnavailable")
		return
	}
	path, err := s.picker(app.window)
	if err != nil {
		s.sourceErr = err.Error()
		return
	}
	if strings.TrimSpace(path) == "" {
		return
	}
	s.sourcePath = path
	s.inspectSourceFile()
}

func (s *channelSettingsUIState) inspectSourceFile() {
	if s.api == nil {
		s.sourceErr = text("channelBackendUnavailable")
		return
	}
	inventory, err := s.api.InspectModelsFile(s.sourcePath)
	s.sourceInventory = inventory
	s.sourceErr = ""
	s.selectedModels = map[string]bool{}
	s.selectedOverrides = map[string]bool{}
	s.sourceProviderID = ""
	s.invalidatePreview()
	if err != nil {
		s.sourceErr = err.Error()
		return
	}
	if len(inventory.Providers) > 0 {
		s.sourceProviderID = inventory.Providers[0].ID
	}
}

func (s *channelSettingsUIState) selectProvider(id string) {
	if s.sourceProviderID == id {
		return
	}
	s.sourceProviderID = id
	s.selectedModels = map[string]bool{}
	s.selectedOverrides = map[string]bool{}
	s.invalidatePreview()
}

func (s *channelSettingsUIState) selectedSourceProvider() (ChannelProviderSummary, bool) {
	for _, provider := range s.sourceInventory.Providers {
		if provider.ID == s.sourceProviderID {
			return provider, true
		}
	}
	return ChannelProviderSummary{}, false
}

func (s *channelSettingsUIState) selectPreset(preset ChannelPreset) {
	s.presetID = preset.ID
	s.presetKeyEnv = preset.APIKeyEnvironment
	s.invalidatePreview()
}

func channelPresetByID(id string) (ChannelPreset, bool) {
	for _, preset := range ChannelPresets() {
		if preset.ID == id {
			return preset, true
		}
	}
	return ChannelPreset{}, false
}

func (s *channelSettingsUIState) prepareImport() {
	if s.api == nil {
		s.previewErr = text("channelBackendUnavailable")
		return
	}
	plan, err := s.api.PrepareImport(ChannelImportRequest{
		SourcePath:  s.sourcePath,
		ProviderID:  s.sourceProviderID,
		ModelIDs:    selectedModelIDs(s.selectedModels),
		OverrideIDs: selectedModelIDs(s.selectedOverrides),
	})
	s.previewErr = ""
	s.previewErrDetail = ""
	s.applyErr = ""
	s.applyErrDetail = ""
	s.stale = false
	s.result = nil
	if err != nil {
		s.importPlan = nil
		s.customPlan = nil
		s.preview = nil
		s.previewErr = text("channelPreviewFailedSummary")
		s.previewErrDetail = err.Error()
		return
	}
	s.previewErrDetail = ""
	s.importPlan = &plan
	s.customPlan = nil
	s.preview = &plan.Preview
	s.previewKind = channelSettingsPreviewImport
}

func (s *channelSettingsUIState) preparePreset() {
	preset, ok := channelPresetByID(s.presetID)
	if !ok {
		s.previewErr = text("channelSelectPresetFirst")
		return
	}
	req := s.formRequest()
	req.ProviderID = preset.ID
	req.Name = preset.Name
	req.BaseURL = ""
	req.API = ""
	req.APIKeyEnv = s.presetKeyEnv
	s.prepareCustomRequest(req)
}

func (s *channelSettingsUIState) prepareCustom() {
	s.prepareCustomRequest(s.formRequest())
}

func (s *channelSettingsUIState) formRequest() ChannelCustomRequest {
	authHeader := s.authHeader
	req := ChannelCustomRequest{
		ProviderID: strings.TrimSpace(s.providerID),
		Name:       strings.TrimSpace(s.providerName),
		BaseURL:    strings.TrimSpace(s.baseURL),
		API:        strings.TrimSpace(s.apiName),
		APIKeyEnv:  strings.TrimSpace(s.apiKeyEnv),
		AuthHeader: &authHeader,
	}
	if id := strings.TrimSpace(s.modelID); id != "" {
		req.Models = []ChannelCustomModel{{
			ID:   id,
			Name: strings.TrimSpace(s.modelName),
		}}
	}
	if id := strings.TrimSpace(s.overrideID); id != "" {
		req.ModelOverride = []ChannelCustomModelOverride{{
			ID:   id,
			Name: strings.TrimSpace(s.overrideName),
		}}
	}
	return req
}

func (s *channelSettingsUIState) prepareCustomRequest(req ChannelCustomRequest) {
	if s.api == nil {
		s.previewErr = text("channelBackendUnavailable")
		return
	}
	plan, err := s.api.PrepareCustom(req)
	s.previewErr = ""
	s.previewErrDetail = ""
	s.applyErr = ""
	s.applyErrDetail = ""
	s.stale = false
	s.result = nil
	if err != nil {
		s.importPlan = nil
		s.customPlan = nil
		s.preview = nil
		s.previewErr = text("channelPreviewFailedSummary")
		s.previewErrDetail = err.Error()
		return
	}
	s.previewErrDetail = ""
	s.customPlan = &plan
	s.importPlan = nil
	s.preview = &plan.Preview
	s.previewKind = channelSettingsPreviewCustom
}

func (s *channelSettingsUIState) apply(app *appState) {
	if !s.planBound() || s.api == nil {
		s.applyErr = text("channelPreviewRequired")
		return
	}
	var (
		result ChannelMergeResult
		err    error
	)
	switch s.previewKind {
	case channelSettingsPreviewImport:
		result, err = s.api.ApplyImport(s.importPlan.Request)
	case channelSettingsPreviewCustom:
		result, err = s.api.ApplyCustom(s.customPlan.Request)
	default:
		err = errors.New(text("channelPreviewRequired"))
	}
	if err != nil {
		var stale *ChannelPreviewStaleError
		if errors.As(err, &stale) {
			s.importPlan = nil
			s.customPlan = nil
			s.stale = true
			s.applyErr = ""
			s.applyErrDetail = ""
			s.previewErr = text("channelPreviewStaleDescription")
			s.previewErrDetail = ""
			return
		}
		s.applyErr = text("channelApplyFailedSummary")
		s.applyErrDetail = err.Error()
		return
	}
	if s.api != nil {
		inventory, inventoryErr := s.api.Inventory()
		s.inventory = inventory
		s.inventoryLoaded = true
		if inventoryErr != nil {
			s.inventoryErr = inventoryErr.Error()
		} else {
			s.inventoryErr = ""
		}
	}
	if s.refresh.MarkPending != nil {
		s.refresh.MarkPending(app)
	}
	s.result = &result
	s.applyErr = ""
	s.applyErrDetail = ""
	s.previewErr = ""
	s.previewErrDetail = ""
	s.stale = false
}

func (s *channelSettingsUIState) reprepare() {
	switch s.previewKind {
	case channelSettingsPreviewImport:
		s.prepareImport()
	case channelSettingsPreviewCustom:
		if s.mode == channelSettingsSourcePreset {
			s.preparePreset()
		} else {
			s.prepareCustom()
		}
	}
}

func (s *channelSettingsUIState) planBound() bool {
	switch s.previewKind {
	case channelSettingsPreviewImport:
		return s.importPlan != nil
	case channelSettingsPreviewCustom:
		return s.customPlan != nil
	default:
		return false
	}
}

func (s *channelSettingsUIState) invalidatePreview() {
	s.importPlan = nil
	s.customPlan = nil
	s.preview = nil
	s.previewKind = ""
	s.previewErr = ""
	s.previewErrDetail = ""
	s.applyErr = ""
	s.applyErrDetail = ""
	s.stale = false
}

func (s *channelSettingsUIState) resetForAnotherSource() {
	s.mode = channelSettingsSourceNone
	s.sourcePath = ""
	s.sourceInventory = ChannelInventory{}
	s.sourceProviderID = ""
	s.sourceErr = ""
	s.selectedModels = map[string]bool{}
	s.selectedOverrides = map[string]bool{}
	s.importPlan = nil
	s.customPlan = nil
	s.preview = nil
	s.previewKind = ""
	s.previewErr = ""
	s.previewErrDetail = ""
	s.applyErr = ""
	s.applyErrDetail = ""
	s.stale = false
	s.result = nil
}

func selectedModelIDs(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for id, selected := range values {
		if selected {
			result = append(result, id)
		}
	}
	sort.Strings(result)
	return result
}

func channelDisplayName(id string) string {
	for _, preset := range ChannelPresets() {
		if preset.ID == id {
			return preset.Name
		}
	}
	return id
}

func channelMergeActionLabel(action string) string {
	switch action {
	case channelMergeAdd:
		return text("channelMergeActionAdd")
	case channelMergePreserve:
		return text("channelMergeActionPreserve")
	case "inherit":
		return text("channelMergeActionInherit")
	case "removed":
		return text("channelMergeActionRemoved")
	default:
		return text("channelMergeActionUnknown")
	}
}

func channelShortDigest(digest string) string {
	if digest == "" {
		return text("channelDigestMissing")
	}
	if len(digest) <= 16 {
		return digest
	}
	return digest[:16]
}

func channelSettingsErrorClassification(err error) string {
	if err == nil {
		return ""
	}
	var stale *ChannelPreviewStaleError
	if errors.As(err, &stale) {
		return channelErrorPreviewStale
	}
	return fmt.Sprintf("channelError:%T", err)
}
