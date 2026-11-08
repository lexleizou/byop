package main

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

// channelSettingsAPI is the complete backend surface consumed by the P32
// Proma-style channel settings renderer. All credentials stay inside the
// service; the UI only ever sees masked keys and status enums.
type channelSettingsAPI interface {
	ConfigStatus() (ChannelConfigStatus, error)
	CredentialStatus(providerID string) (ChannelCredentialStatus, error)
	SetAPIKey(providerID, apiKey string) error
	ClearAPIKey(providerID string) error
	TestConnection(providerID string) (ChannelTestResult, error)
	FetchModels(providerID string) (ChannelModelsResult, error)
	SetModelEnabled(providerID, modelID string, enabled bool) ([]string, error)
	SetChannelEnabled(providerID string, enabled bool) ([]string, error)
	DeleteChannel(providerID string) (bool, error)
	PrepareCustom(req ChannelCustomRequest) (ChannelCustomPlan, error)
	ApplyCustom(req ChannelCustomRequest) (ChannelMergeResult, error)
}

// channelSettingsRefreshBridge is the seam owned by channel_refresh.go and
// the app lifecycle. A nil production field is replaced by the appState
// methods; tests can inject a deterministic local status.
type channelSettingsRefreshBridge struct {
	Status      func(*appState) ChannelRefreshStatus
	MarkPending func(*appState)
	Retry       func(*appState)
}

type channelSettingsViewMode string

const (
	channelSettingsViewList   channelSettingsViewMode = ""
	channelSettingsViewCreate channelSettingsViewMode = "create"
	channelSettingsViewEdit   channelSettingsViewMode = "edit"
)

// P32 layout constants, translated from the Proma 0.19.58 spec:
// a 1080px content column with px-5 (20) below the sm breakpoint and px-8
// (32) above; BYOP caps the column at 1040.
const (
	channelSettingsMaxWidth  float32 = 1040
	channelSettingsWideBreak float32 = 1180
	channelSettingsPadWide   float32 = 32
	channelSettingsPadNarrow float32 = 20
)

type channelSettingsUIState struct {
	api     channelSettingsAPI
	refresh channelSettingsRefreshBridge
	initErr error

	view channelSettingsViewMode

	status        ChannelConfigStatus
	statusLoaded  bool
	statusLoading bool
	statusErr     string

	credentials    map[string]ChannelCredentialStatus
	credentialBusy map[string]bool

	testResults  map[string]ChannelTestResult
	testBusy     map[string]bool
	fetchResults map[string]ChannelModelsResult
	fetchBusy    map[string]bool
	deleteBusy   map[string]bool

	// form is the create/edit form. It never stores a loaded plaintext key;
	// the edit mask comes from ConfigStatus.
	form                 channelSettingsFormState
	dialogDelete         string
	dialogRemove         string
	dialogRisk           string
	dialogRiskAction     string
	dialogDiscard        bool
	loginBanner          string
	loginWatchActive     bool
	loginWatchProviderID string
	loginWatchTimedOut   bool
	// openPiLogin is the seam the app wires to the terminal host; the
	// production default is nil and login runs inside the Pi terminal.
	openPiLogin       func(providerID string)
	loginWatchTimeout time.Duration
}

type channelSettingsFormState struct {
	providerID      string
	name            string
	baseURL         string
	apiKey          string
	keyVisible      bool
	enabled         bool
	search          string
	manualID        string
	manualName      string
	manualModels    []string
	acknowledged    string
	loaded          bool
	enabledModels   map[string]bool
	availableModels []string
	saving          bool
}

var (
	channelSettingsStates sync.Map
	channelSettingsNewAPI = func() (channelSettingsAPI, error) {
		return NewChannelConfigService("")
	}
)

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
	state := newChannelSettingsUIState()
	state.api, state.initErr = channelSettingsNewAPI()
	value, _ := channelSettingsStates.LoadOrStore(s, state)
	return value.(*channelSettingsUIState)
}

func newChannelSettingsUIState() *channelSettingsUIState {
	return &channelSettingsUIState{
		credentials:    map[string]ChannelCredentialStatus{},
		credentialBusy: map[string]bool{},
		testResults:    map[string]ChannelTestResult{},
		testBusy:       map[string]bool{},
		fetchResults:   map[string]ChannelModelsResult{},
		fetchBusy:      map[string]bool{},
		deleteBusy:     map[string]bool{},
	}
}

func resetChannelSettingsForTest(s *appState) {
	if s != nil {
		channelSettingsStates.Delete(s)
	}
}

func installChannelSettingsTestDependencies(
	s *appState,
	api channelSettingsAPI,
	refresh channelSettingsRefreshBridge,
) *channelSettingsUIState {
	state := newChannelSettingsUIState()
	state.api = api
	state.refresh = refresh
	channelSettingsStates.Store(s, state)
	return state
}

// channelSettingsContentWidth mirrors the Proma container: max-w-[1080px]
// with px-5 below the wide breakpoint and px-8 above, capped at 1040.
func channelSettingsContentWidth(containerWidth float32) float32 {
	if containerWidth <= 0 {
		return channelSettingsMaxWidth
	}
	pad := channelSettingsPadWide
	if containerWidth < channelSettingsWideBreak {
		pad = channelSettingsPadNarrow
	}
	width := containerWidth - 2*pad
	if width > channelSettingsMaxWidth {
		width = channelSettingsMaxWidth
	}
	if width < 0 {
		width = 0
	}
	return width
}

func (s *appState) channelSettingsContent(c *ui.Context, p palette) ui.Element {
	state := channelSettingsState(s)
	if !state.statusLoading && !state.statusLoaded && state.statusErr == "" {
		state.loadStatus()
	}
	return ui.Column(c).Key("channel-settings").FillWidth().MinWidth(0).Gap(12).
		Children(func() {
			s.channelSettingsDialogs(c, p, state)
			s.channelSettingsBanner(c, p, state)
			if state.view == channelSettingsViewList {
				s.channelSettingsList(c, p, state)
				s.channelSettingsRefresh(c, p, state)
				return
			}
			s.channelSettingsFormHeader(c, p, state)
			s.channelSettingsBasicInfo(c, p, state)
			s.channelSettingsEnabledModels(c, p, state)
			s.channelSettingsAvailableModels(c, p, state)
			s.channelSettingsRefresh(c, p, state)
		})
}

// channelSettingsList renders the Proma list view: section header with the
// add button, the Pi status line, the provider rows and the empty state.
func (s *appState) channelSettingsList(c *ui.Context, p palette, state *channelSettingsUIState) {
	ui.Column(c).Key("channel-list").FillWidth().Gap(12).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(16).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, text("channelsSectionTitle")).FontSize(fontTitle).
					FontWeight(650).TextColor(p.text)
				ui.Text(c, text("channelsSectionDescription")).FontSize(fontBody).
					TextColor(p.muted).Wrap()
			})
			s.channelSettingsSmallButton(c, p, "channels-add", text("channelsAdd"),
				"plus", true, func() {
					state.openCreate()
				})
		})
		ui.Text(c, state.piStatusLine()).FontSize(fontSmall).TextColor(p.muted).Wrap()
		if state.statusErr != "" {
			s.channelSettingsError(c, p, "channel-status-error",
				textArgs("channelsStatusReadFailed", map[string]any{"detail": state.statusErr}))
			return
		}
		if state.statusLoading {
			ui.Text(c, text("channelsLoading")).FontSize(fontSmall).TextColor(p.muted)
			return
		}
		if len(state.status.Providers) == 0 {
			ui.Text(c, text("channelsEmpty")).FontSize(fontBody).TextColor(p.muted).Wrap()
			return
		}
		providers := append([]ChannelStatusProvider{}, state.status.Providers...)
		sort.Slice(providers, func(i, j int) bool { return providers[i].ID < providers[j].ID })
		for index, provider := range providers {
			if index == 0 {
				first := ui.Column(c).Key("channel-rows").FillWidth().Radius(12).
					Border(v12BorderWidth, p.border).Background(p.surface)
				first.Children(func() {
					s.channelSettingsProviderRow(c, p, state, provider)
				})
				continue
			}
			rowCard := ui.Column(c).Key("channel-row-card-"+provider.ID).FillWidth().
				Radius(12).Border(v12BorderWidth, p.border).Background(p.surface)
			rowCard.Children(func() {
				s.channelSettingsProviderRow(c, p, state, provider)
			})
		}
		ui.Text(c, text("channelsPlaintextNote")).FontSize(fontSmall).
			TextColor(p.muted).Wrap()
	})
}

func (state *channelSettingsUIState) piStatusLine() string {
	total := len(state.status.Providers)
	ready := 0
	for _, provider := range state.status.Providers {
		if provider.CredentialConfigured {
			ready++
		}
	}
	if !state.status.ModelsExists {
		return text("channelsStatusLineMissing")
	}
	return textArgs("channelsPiStatusLine", map[string]any{
		"channels": total, "ready": ready, "total": total,
	})
}

// channelSettingsProviderRow is one Proma settings row: 32×32 icon block,
// name + description, hover-only edit/remove actions, and a 36×20 switch.
func (s *appState) channelSettingsProviderRow(
	c *ui.Context,
	p palette,
	state *channelSettingsUIState,
	provider ChannelStatusProvider,
) {
	displayName := fallback(provider.Name, provider.ID)
	row := ui.Row(c).Key("channel-row-"+provider.ID).FillWidth().
		Padding(12, 16).AlignItems(ui.Center).Gap(12)
	row.Children(func() {
		initial := ""
		if provider.ID != "" {
			initial = strings.ToUpper(provider.ID[:minInt(1, len(provider.ID))])
		}
		ui.Column(c).Key("channel-row-icon-" + provider.ID).Shrink(0).Width(32).
			Height(32).Radius(4).Background(p.surfaceSubtle).AlignItems(ui.Center).
			Children(func() {
				ui.Text(c, initial).FontSize(fontBody).FontWeight(650).
					TextColor(p.muted)
			})
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, displayName).FontSize(fontBody).FontWeight(500).
				TextColor(p.text).Wrap()
			ui.Text(c, textArgs("channelsRowDescription", map[string]any{
				"name": displayName, "count": provider.ModelCount,
			})).FontSize(fontSmall).TextColor(p.muted).Wrap()
		})
		hovered := row.Hovered()
		if !provider.BuiltIn {
			edit := ui.ButtonBase(c.Key("channel-edit-" + provider.ID)).
				Label(text("channelsEdit")).Width(26).Height(26).Padding(0).
				Radius(v12ChipRadius).Cursor(ui.CursorPointer).
				Opacity(hoverOpacity(hovered))
			edit.Children(func() {
				ui.Icon(c, icon("square-pen")).Size(14, 14).TextColor(p.muted)
			})
			if edit.Clicked() {
				state.openEdit(provider)
			}
			remove := ui.ButtonBase(c.Key("channel-delete-" + provider.ID)).
				Label(text("channelsDelete")).Width(26).Height(26).Padding(0).
				Radius(v12ChipRadius).Cursor(ui.CursorPointer).
				Opacity(hoverOpacity(hovered))
			remove.Children(func() {
				ui.Icon(c, icon("archive")).Size(14, 14).TextColor(p.dangerText)
			})
			if remove.Clicked() {
				state.dialogDelete = provider.ID
			}
		} else {
			remove := ui.ButtonBase(c.Key("channel-remove-"+provider.ID)).
				Label(text("channelsRemoveCredential")).Padding(5, 8).
				Radius(v12ChipRadius).Cursor(ui.CursorPointer).
				Opacity(hoverOpacity(hovered))
			remove.Children(func() {
				ui.Text(c, text("channelsRemoveCredential")).FontSize(fontSmall).
					TextColor(p.muted)
			})
			if remove.Clicked() {
				state.dialogRemove = provider.ID
			}
		}
		checked := provider.CredentialConfigured
		channelSwitch(c, p, "channel-switch-"+provider.ID,
			textArgs("channelsEnableSwitchAria", map[string]any{"name": displayName}),
			checked, true, func(next bool) {
				state.setChannelEnabled(provider.ID, next)
			})
	})
}

func hoverOpacity(hovered bool) float32 {
	if hovered {
		return 1
	}
	return 0
}

// channelSwitch draws the Proma switch: a 36×20 track with a 16px thumb that
// translates 16px when checked.
func channelSwitch(
	c *ui.Context,
	p palette,
	key, aria string,
	checked bool,
	enabled bool,
	onChange func(bool),
) {
	trackBackground := p.border
	if checked {
		trackBackground = p.accent
	}
	if !enabled {
		trackBackground = p.surfaceSubtle
	}
	track := ui.ButtonBase(c.Key(key)).Label(aria).Width(36).Height(20).Shrink(0).
		Radius(10).Background(trackBackground).Cursor(ui.CursorPointer).
		Disabled(!enabled)
	track.Children(func() {
		ui.Row(c).FillWidth().Height(20).AlignItems(ui.Center).Children(func() {
			if checked {
				ui.Spacer(c).Grow(1)
			}
			ui.Column(c).Width(16).Height(16).Radius(8).Background(p.background)
			if !checked {
				ui.Spacer(c).Grow(1)
			}
		})
	})
	if enabled && track.Clicked() {
		onChange(!checked)
	}
}

// channelSettingsSmallButton is the small button family: 28px tall with 12px
// text for row actions, 32px tall for primary section actions.
func (s *appState) channelSettingsSmallButton(
	c *ui.Context,
	p palette,
	key, label, iconName string,
	primary bool,
	click func(),
) {
	height := float32(28)
	if primary {
		height = 32
	}
	background := p.surface
	foreground := p.text
	border := p.border
	if primary {
		background = p.accent
		foreground = p.onAccent
		border = p.accent
	}
	button := ui.ButtonBase(c.Key(key)).Label(label).Height(height).
		Padding(0, 12).Radius(v12ChipRadius).Border(v12BorderWidth, border).
		Background(background).Cursor(ui.CursorPointer)
	if button.Hovered() {
		if primary {
			button.Background(p.accentHover)
		} else {
			button.Background(p.surfaceHover)
		}
	}
	button.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			if iconName != "" {
				ui.Icon(c, icon(iconName)).Size(14, 14).TextColor(foreground)
			}
			ui.Text(c, label).FontSize(fontSmall).FontWeight(600).
				TextColor(foreground).Wrap()
		})
	})
	if button.Clicked() {
		click()
	}
}

// channelSettingsFormHeader is the create/edit title bar: back arrow, title,
// and the primary submit button.
func (s *appState) channelSettingsFormHeader(c *ui.Context, p palette, state *channelSettingsUIState) {
	ui.Row(c).Key("channel-form-header").FillWidth().AlignItems(ui.Center).
		Gap(8).Padding(0, 0, 8, 0).Children(func() {
		back := ui.ButtonBase(c.Key("channel-form-back")).
			Label(text("channelsBack")).Width(32).Height(32).Padding(0).
			Radius(v12ChipRadius).Cursor(ui.CursorPointer)
		back.Children(func() {
			ui.Icon(c, icon("corner-up-left")).Size(18, 18).TextColor(p.text)
		})
		if back.Clicked() {
			state.requestLeaveForm(s)
		}
		title := text("channelsCreateTitle")
		if state.view == channelSettingsViewEdit {
			title = text("channelsEditTitle")
		}
		ui.Text(c, title).FontSize(fontTitle).FontWeight(500).TextColor(p.text).
			Grow(1).MinWidth(0)
		s.channelSettingsSmallButton(c, p, "channel-form-submit",
			state.submitLabel(), "check", true, func() {
				if state.view == channelSettingsViewCreate {
					state.submitCreate(s)
				} else {
					state.submitEdit(s)
				}
			})
	})
}

// channelSettingsBasicInfo is the first Proma form card: provider type,
// name, base URL with preview, the API key with mask, the connectivity probe
// and the Pi login row for subscription providers.
func (s *appState) channelSettingsBasicInfo(c *ui.Context, p palette, state *channelSettingsUIState) {
	settingsCardV12(c, p, text("channelsBasicInfoTitle"), "", func() {
		ui.Text(c, text("channelsProviderType")).FontSize(fontBody).
			FontWeight(500).TextColor(p.text)
		ui.Row(c).FillWidth().Wrap().Gap(6).Children(func() {
			ids := []string{}
			seen := map[string]bool{}
			if state.form.providerID != "" {
				ids = append(ids, state.form.providerID)
				seen[state.form.providerID] = true
			}
			for _, preset := range ChannelPresets() {
				if !seen[preset.ID] {
					ids = append(ids, preset.ID)
					seen[preset.ID] = true
				}
			}
			sort.Strings(ids)
			for _, id := range ids {
				selected := state.form.providerID == id
				label := channelDisplayName(id)
				button := ui.ButtonBase(c.Key("channel-provider-option-"+id)).
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
				if button.Clicked() && state.view != channelSettingsViewEdit {
					state.selectProviderType(id)
				}
			}
		})
		if state.form.providerID == "" {
			ui.Text(c, text("channelsProviderPlaceholder")).FontSize(fontSmall).
				TextColor(p.muted)
		}
		s.channelSettingsTextField(c, p, "channel-form-name",
			text("channelsProviderName"), text("channelsProviderNamePlaceholder"),
			&state.form.name)
		s.channelSettingsTextField(c, p, "channel-form-base-url",
			text("channelsBaseURLLabel"), text("channelsBaseURLPlaceholder"),
			&state.form.baseURL)
		if strings.TrimSpace(state.form.baseURL) != "" {
			ui.Text(c, textArgs("channelsBaseURLPreview", map[string]any{
				"url": strings.TrimSpace(state.form.baseURL),
			})).FontSize(fontSmall).TextColor(p.muted).Wrap()
		}
		if state.isSubscriptionProvider() {
			s.channelSettingsPiLoginRow(c, p, state)
		} else {
			s.channelSettingsAPIKeyRow(c, p, state)
			s.channelSettingsTestRow(c, p, state)
		}
		enabledSwitch := state.form.enabled
		enabled := ui.Checkbox(c.Key("channel-form-enabled"), &enabledSwitch,
			text("channelsEnableSwitchLabel")).FontSize(fontBody)
		if enabled.Changed() {
			state.form.enabled = enabledSwitch
		}
		ui.Text(c, text("channelsEnableSwitchDescription")).FontSize(fontSmall).
			TextColor(p.muted).Wrap()
		ui.Text(c, text("channelsPlaintextNote")).FontSize(fontSmall).
			TextColor(p.muted).Wrap()
	})
}

func (state *channelSettingsUIState) isSubscriptionProvider() bool {
	preset, ok := channelPresetByID(state.form.providerID)
	return ok && preset.UsesPiLogin
}

// channelSettingsAPIKeyRow renders the key input with a visibility toggle and
// the masked existing value in edit mode. The full key is never re-rendered.
func (s *appState) channelSettingsAPIKeyRow(c *ui.Context, p palette, state *channelSettingsUIState) {
	placeholder := text("channelsAPIKeyPlaceholderCreate")
	if state.view == channelSettingsViewEdit {
		placeholder = text("channelsAPIKeyPlaceholderEdit")
	}
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		ui.Text(c, text("channelsAPIKeyLabel")).FontSize(fontSmall).
			FontWeight(600).TextColor(p.muted)
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			input := ui.TextInputBase(c.Key("channel-form-api-key"), &state.form.apiKey).
				FillWidth().Height(36).Padding(0, 10).Radius(v12InputRadius).
				Border(v12BorderWidth, p.border).Background(p.surface).
				FontSize(fontBody).TextColor(p.text).
				Label(text("channelsAPIKeyLabel")).Placeholder(placeholder)
			if !state.form.keyVisible {
				input.Password()
			}
			if input.Focused() {
				input.BorderColor(p.focus)
			}
			eye := ui.ButtonBase(c.Key("channel-form-key-toggle")).
				Label(text("channelsToggleKeyVisibility")).Width(32).Height(32).
				Padding(0).Radius(v12ChipRadius).Cursor(ui.CursorPointer)
			eye.Children(func() {
				ui.Icon(c, icon("contrast")).Size(16, 16).TextColor(p.muted)
			})
			if eye.Clicked() {
				state.form.keyVisible = !state.form.keyVisible
			}
		})
		if state.view == channelSettingsViewEdit {
			ui.Text(c, textArgs("channelsKeyMasked", map[string]any{
				"mask": state.editKeyMask(),
			})).FontSize(fontSmall).TextColor(p.muted).Wrap()
		}
	})
}

func (state *channelSettingsUIState) editKeyMask() string {
	for _, provider := range state.status.Providers {
		if provider.ID == state.form.providerID {
			return provider.APIKeyMasked
		}
	}
	return ""
}

// channelSettingsTestRow is the BYOP-direct connectivity probe with the
// normalized Proma result copy.
func (s *appState) channelSettingsTestRow(c *ui.Context, p palette, state *channelSettingsUIState) {
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			busy := state.testBusy[state.form.providerID]
			s.channelSettingsSmallButton(c, p, "channel-form-test",
				text("channelsTestConnection"), "activity", false, func() {
					state.runTest(s)
				})
			if busy {
				ui.Text(c, text("channelsTestRunning")).FontSize(fontSmall).
					TextColor(p.muted)
			}
		})
		if result, ok := state.testResults[state.form.providerID]; ok {
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icon("check")).Size(12, 12).TextColor(resultColor(p, result.OK))
				ui.Text(c, channelTestResultLabel(result)).FontSize(fontSmall).
					TextColor(resultColor(p, result.OK)).Wrap()
			})
		}
	})
}

func resultColor(p palette, ok bool) ui.Color {
	if ok {
		return p.successText
	}
	return p.dangerText
}

func channelTestResultLabel(result ChannelTestResult) string {
	if result.OK {
		return text("channelsTestResultOK")
	}
	key := "channelsTestFailed"
	switch result.Category {
	case "auth":
		key = "channelsTestAuth"
	case "forbidden":
		key = "channelsTestForbidden"
	case "quota":
		key = "channelsTestQuota"
	case "not_found":
		key = "channelsTestNotFound"
	case "rate_limited":
		key = "channelsTestRateLimited"
	case "invalid":
		key = "channelsTestInvalid"
	case "server":
		key = "channelsTestServer"
	case "network":
		key = "channelsTestNetwork"
	case "timeout":
		key = "channelsTestTimeout"
	}
	return text(key)
}

// channelSettingsPiLoginRow is the BYOP subscription flow: hand the login to
// the Pi terminal and poll `pi auth check` every two seconds for up to 90s.
func (s *appState) channelSettingsPiLoginRow(c *ui.Context, p palette, state *channelSettingsUIState) {
	providerID := state.form.providerID
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			label := text("channelsSubscriptionLoginLabel")
			if state.loginWatchActive {
				label = text("channelsLoginWaiting")
			} else if cred, ok := state.credentials[providerID]; ok && cred.Status == "ready" {
				label = text("channelsSubscriptionReloginLabel")
			}
			s.channelSettingsSmallButton(c, p, "channel-form-pi-login",
				label, "terminal", false, func() {
					state.startLoginWatch(s, providerID)
				})
			if state.loginWatchActive {
				ui.Text(c, text("channelsLoginWaiting")).FontSize(fontSmall).
					TextColor(p.muted)
			}
		})
		if cred, ok := state.credentials[providerID]; ok {
			statusColor := p.muted
			statusText := text("channelsLoginNotLoggedIn")
			if cred.Status == "ready" {
				statusColor = p.successText
				statusText = text("channelsLoginReady")
			}
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icon("check")).Size(12, 12).TextColor(statusColor)
				ui.Text(c, statusText).FontSize(fontSmall).TextColor(statusColor).Wrap()
			})
		}
		if state.loginWatchTimedOut && state.loginWatchProviderID == providerID {
			ui.Text(c, text("channelsLoginTimeout")).FontSize(fontSmall).
				TextColor(p.warningText).Wrap()
		}
	})
}

// channelSettingsEnabledModels is the second Proma form card.
func (s *appState) channelSettingsEnabledModels(c *ui.Context, p palette, state *channelSettingsUIState) {
	enabled := state.enabledModelIDs()
	settingsCardV12(c, p, text("channelsEnabledModelsTitle"),
		textArgs("channelsEnabledCount", map[string]any{"count": len(enabled)}), func() {
			if len(enabled) == 0 {
				ui.Text(c, text("channelsEnabledEmpty")).FontSize(fontSmall).
					TextColor(p.muted).Wrap()
				return
			}
			ui.Column(c).FillWidth().Children(func() {
				for index, modelID := range enabled {
					if index > 0 {
						separator(c, "channel-enabled-sep-"+modelID, p)
					}
					ui.Row(c).Key("channel-enabled-"+modelID).FillWidth().
						Padding(10, 16).AlignItems(ui.Center).Gap(8).Children(func() {
						ui.Icon(c, icon("check")).Size(14, 14).TextColor(p.successText)
						ui.Text(c, modelID).FontSize(fontBody).TextColor(p.text).
							Grow(1).MinWidth(0).Wrap()
						remove := ui.ButtonBase(c.Key("channel-enabled-remove-" + modelID)).
							Label(text("channelsDisableModel")).Padding(2).
							Radius(v12ChipRadius).Cursor(ui.CursorPointer)
						remove.Children(func() {
							ui.Icon(c, icon("x")).Size(14, 14).TextColor(p.muted)
						})
						if remove.Clicked() {
							state.setModelEnabled(modelID, false)
						}
					})
				}
			})
		})
}

func separator(c *ui.Context, key string, p palette) {
	ui.Row(c).Key(key).FillWidth().Height(v12BorderWidth).
		Background(p.border).Shrink(0)
}

// channelSettingsAvailableModels is the third Proma form card: fetch button,
// search box (only when more than five models), the list (height 280 when
// more than eight rows) and the manual add row.
func (s *appState) channelSettingsAvailableModels(c *ui.Context, p palette, state *channelSettingsUIState) {
	unenabled := state.unenabledModels()
	settingsCardV12(c, p, text("channelsAvailableModelsTitle"),
		textArgs("channelsAvailableCount", map[string]any{"count": len(unenabled)}), func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				busy := state.fetchBusy[state.form.providerID]
				s.channelSettingsSmallButton(c, p, "channel-form-fetch",
					text("channelsFetchModels"), "arrow-up", false, func() {
						state.fetchModels(s)
					})
				if busy {
					ui.Text(c, text("channelsFetching")).FontSize(fontSmall).
						TextColor(p.muted)
				}
				if result, ok := state.fetchResults[state.form.providerID]; ok {
					ui.Text(c, channelFetchResultLabel(result)).FontSize(fontSmall).
						TextColor(resultColor(p, result.Category == "ok")).Wrap()
				}
			})
			if len(unenabled) > 5 {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(4).
					Padding(4, 0, 4, 0).Children(func() {
					ui.Icon(c, icon("search")).Size(14, 14).TextColor(p.muted)
					ui.TextInputBase(c.Key("channel-form-model-search"), &state.form.search).
						FillWidth().Height(32).Padding(0, 10).
						Radius(v12InputRadius).Border(v12BorderWidth, p.border).
						Background(p.surface).FontSize(fontSmall).TextColor(p.text).
						Label(text("channelsSearchPlaceholder")).
						Placeholder(text("channelsSearchPlaceholder"))
				})
			}
			shown := unenabled
			if search := strings.TrimSpace(state.form.search); search != "" {
				filtered := []string{}
				for _, modelID := range unenabled {
					if strings.Contains(strings.ToLower(modelID), strings.ToLower(search)) {
						filtered = append(filtered, modelID)
					}
				}
				shown = filtered
				if len(filtered) == 0 {
					ui.Text(c, text("channelsSearchEmpty")).FontSize(fontSmall).
						TextColor(p.muted).Wrap()
					return
				}
			}
			if len(shown) == 0 {
				ui.Text(c, text("channelsAllEnabled")).FontSize(fontSmall).
					TextColor(p.muted).Wrap()
				return
			}
			if len(shown) > 8 {
				ui.Scroll(c).Height(280).FillWidth().MinHeight(0).MinWidth(0).
					Children(func() {
						s.channelSettingsModelList(c, p, state, shown, true)
					})
				return
			}
			s.channelSettingsModelList(c, p, state, shown, false)
		})
}

func (s *appState) channelSettingsModelList(
	c *ui.Context,
	p palette,
	state *channelSettingsUIState,
	models []string,
	scrolled bool,
) {
	_ = scrolled
	ui.Column(c).FillWidth().Children(func() {
		for index, modelID := range models {
			if index > 0 {
				separator(c, "channel-available-sep-"+modelID, p)
			}
			ui.Row(c).Key("channel-available-"+modelID).FillWidth().
				Padding(10, 16).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Icon(c, icon("plus")).Size(14, 14).TextColor(p.muted)
				ui.Text(c, modelID).FontSize(fontBody).TextColor(p.text).
					Grow(1).MinWidth(0).Wrap()
				add := ui.ButtonBase(c.Key("channel-available-add-" + modelID)).
					Label(text("channelsEnableModel")).Padding(2).
					Radius(v12ChipRadius).Cursor(ui.CursorPointer)
				add.Children(func() {
					ui.Icon(c, icon("plus")).Size(14, 14).TextColor(p.muted)
				})
				if add.Clicked() {
					state.setModelEnabled(modelID, true)
				}
			})
		}
	})
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Padding(10, 0, 0, 0).
		Children(func() {
			ui.TextInputBase(c.Key("channel-form-manual-id"), &state.form.manualID).
				Grow(1).MinWidth(0).Height(32).Padding(0, 10).
				Radius(v12InputRadius).Border(v12BorderWidth, p.border).
				Background(p.surface).FontSize(fontSmall).TextColor(p.text).
				Placeholder(text("channelsManualModelID"))
			ui.TextInputBase(c.Key("channel-form-manual-name"), &state.form.manualName).
				Grow(1).MinWidth(0).Height(32).Padding(0, 10).
				Radius(v12InputRadius).Border(v12BorderWidth, p.border).
				Background(p.surface).FontSize(fontSmall).TextColor(p.text).
				Placeholder(text("channelsManualModelName"))
			add := ui.ButtonBase(c.Key("channel-form-manual-add")).
				Label(text("channelsManualAdd")).Width(32).Height(32).Padding(0).
				Radius(v12ChipRadius).Cursor(ui.CursorPointer)
			add.Children(func() {
				ui.Icon(c, icon("plus")).Size(16, 16).TextColor(p.text)
			})
			if add.Clicked() {
				state.addManualModel()
			}
		})
}

func channelFetchResultLabel(result ChannelModelsResult) string {
	if result.Category == "ok" {
		return textArgs("channelsFetchOK", map[string]any{"count": len(result.Models)})
	}
	if result.Category == "subscription" {
		return text("channelsSubscriptionFetchHint")
	}
	return channelTestResultLabel(ChannelTestResult{Category: result.Category})
}

// channelSettingsDialogs renders the active dialog inline above the page.
// Proma centers these in a 512px card with a 40% scrim; BYOP keeps the same
// copy inside the settings column until a shared modal host exists.
func (s *appState) channelSettingsDialogs(c *ui.Context, p palette, state *channelSettingsUIState) {
	if state.dialogDelete != "" {
		name := state.dialogDelete
		if provider, ok := state.statusProvider(state.dialogDelete); ok {
			name = fallback(provider.Name, provider.ID)
		}
		settingsCardV12(c, p, text("channelsDeleteConfirmTitle"),
			textArgs("channelsDeleteConfirmBody", map[string]any{"name": name}), func() {
				s.channelSettingsDialogButtons(c, p, state, "delete")
			})
	}
	if state.dialogRemove != "" {
		name := state.dialogRemove
		if provider, ok := state.statusProvider(state.dialogRemove); ok {
			name = fallback(provider.Name, provider.ID)
		}
		settingsCardV12(c, p, text("channelsRemoveConfirmTitle"),
			textArgs("channelsRemoveConfirmBody", map[string]any{"name": name}), func() {
				s.channelSettingsDialogButtons(c, p, state, "remove")
			})
	}
	if state.dialogRisk != "" {
		ui.Column(c).Key("channel-risk-dialog").FillWidth().Gap(8).
			Padding(16).Radius(12).Border(v12BorderWidth, p.border).
			Background(p.surface).Children(func() {
			ui.Text(c, text("channelsRiskTitle")).FontSize(fontTitle).
				FontWeight(650).TextColor(p.text)
			for _, key := range []string{
				"channelsRiskBody1", "channelsRiskBody2", "channelsRiskBody3",
				"channelsRiskBody4",
			} {
				ui.Text(c, text(key)).FontSize(fontBody).LineHeight(1.4).
					TextColor(p.muted).Wrap()
			}
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				cancel := ui.ButtonBase(c.Key("channel-risk-cancel")).
					Label(text("channelsCancel")).Height(28).Padding(0, 12).
					Radius(v12ChipRadius).Border(v12BorderWidth, p.border).
					Background(p.surface).Cursor(ui.CursorPointer)
				cancel.Children(func() {
					ui.Text(c, text("channelsCancel")).FontSize(fontSmall).
						TextColor(p.text)
				})
				if cancel.Clicked() {
					state.dialogRisk = ""
					state.dialogRiskAction = ""
				}
				accept := ui.ButtonBase(c.Key("channel-risk-accept")).
					Label(text("channelsRiskAccept")).Height(28).Padding(0, 12).
					Radius(v12ChipRadius).Background(p.danger).Cursor(ui.CursorPointer)
				accept.Children(func() {
					ui.Text(c, text("channelsRiskAccept")).FontSize(fontSmall).
						FontWeight(600).TextColor(p.onAccent)
				})
				if accept.Clicked() {
					action := state.dialogRiskAction
					state.dialogRisk = ""
					state.dialogRiskAction = ""
					state.acknowledgeAndRun(s, action)
				}
			})
		})
	}
	if state.dialogDiscard {
		settingsCardV12(c, p, text("channelsDiscardTitle"),
			text("channelsDiscardBodyNoModels"), func() {
				s.channelSettingsDialogButtons(c, p, state, "discard")
			})
	}
}

func (s *appState) channelSettingsDialogButtons(
	c *ui.Context,
	p palette,
	state *channelSettingsUIState,
	kind string,
) {
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		cancelLabel := text("channelsCancel")
		confirmLabel := text("channelsConfirmDelete")
		switch kind {
		case "remove":
			confirmLabel = text("channelsConfirmRemove")
		case "discard":
			cancelLabel = text("channelsDiscard")
			confirmLabel = text("channelsSaveAndClose")
		}
		cancel := ui.ButtonBase(c.Key("channel-dialog-cancel-"+kind)).
			Label(cancelLabel).Height(28).Padding(0, 12).
			Radius(v12ChipRadius).Border(v12BorderWidth, p.border).
			Background(p.surface).Cursor(ui.CursorPointer)
		cancel.Children(func() {
			ui.Text(c, cancelLabel).FontSize(fontSmall).TextColor(p.text)
		})
		if cancel.Clicked() {
			state.cancelDialog(kind)
			if kind == "discard" {
				state.closeForm(s)
			}
		}
		confirm := ui.ButtonBase(c.Key("channel-dialog-confirm-"+kind)).
			Label(confirmLabel).Height(28).Padding(0, 12).
			Radius(v12ChipRadius).Background(p.danger).Cursor(ui.CursorPointer)
		confirm.Children(func() {
			ui.Text(c, confirmLabel).FontSize(fontSmall).FontWeight(600).
				TextColor(p.onAccent)
		})
		if confirm.Clicked() {
			if kind == "discard" {
				if state.view == channelSettingsViewCreate {
					state.submitCreate(s)
				} else {
					state.submitEdit(s)
				}
				state.closeForm(s)
				return
			}
			state.confirmDialog(s, kind)
		}
	})
}

// channelSettingsBanner is the subscription success banner at the top of the
// list, matching Proma's green card with a close button.
func (s *appState) channelSettingsBanner(c *ui.Context, p palette, state *channelSettingsUIState) {
	if state.loginBanner == "" {
		return
	}
	ui.Row(c).Key("channel-login-banner").FillWidth().Padding(12, 16).
		Radius(12).Border(v12BorderWidth, p.success.Alpha(0.25)).
		Background(p.success.Alpha(0.08)).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Icon(c, icon("check")).Size(20, 20).TextColor(p.successText)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, textArgs("channelsBannerTitle", map[string]any{
				"name": channelDisplayName(state.loginBanner),
			})).FontSize(fontBody).FontWeight(500).TextColor(p.text)
			ui.Text(c, text("channelsBannerBody")).FontSize(fontSmall).
				TextColor(p.muted).Wrap()
		})
		close := ui.ButtonBase(c.Key("channel-banner-close")).
			Label(text("channelsBannerClose")).Width(40).Height(40).Padding(0).
			Radius(v12ChipRadius).Cursor(ui.CursorPointer)
		close.Children(func() {
			ui.Icon(c, icon("x")).Size(16, 16).TextColor(p.muted)
		})
		if close.Clicked() {
			state.loginBanner = ""
		}
	})
}

func (s *appState) channelSettingsError(c *ui.Context, p palette, key, message string) {
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
	})
}

// ---------------------------------------------------------------------------
// state transitions

func (state *channelSettingsUIState) loadStatus() {
	if state == nil || state.api == nil {
		return
	}
	state.statusLoading = true
	status, err := state.api.ConfigStatus()
	state.statusLoading = false
	state.statusLoaded = true
	state.status = status
	state.statusErr = ""
	if err != nil {
		state.statusErr = err.Error()
	}
}

func (state *channelSettingsUIState) statusProvider(id string) (ChannelStatusProvider, bool) {
	for _, provider := range state.status.Providers {
		if provider.ID == id {
			return provider, true
		}
	}
	return ChannelStatusProvider{}, false
}

func (state *channelSettingsUIState) openCreate() {
	state.view = channelSettingsViewCreate
	state.form = channelSettingsFormState{enabled: true, loaded: true}
	state.dialogDelete = ""
	state.dialogRemove = ""
	state.dialogRisk = ""
	state.dialogRiskAction = ""
	state.dialogDiscard = false
}

func (state *channelSettingsUIState) openEdit(provider ChannelStatusProvider) {
	state.view = channelSettingsViewEdit
	enabled := provider.CredentialConfigured
	state.form = channelSettingsFormState{
		providerID:    provider.ID,
		name:          fallback(provider.Name, provider.ID),
		enabled:       enabled,
		loaded:        true,
		enabledModels: map[string]bool{},
	}
	state.dialogDelete = ""
	state.dialogRemove = ""
	state.dialogRisk = ""
	state.dialogRiskAction = ""
	state.dialogDiscard = false
}

func (state *channelSettingsUIState) selectProviderType(id string) {
	if state.form.providerID == id {
		return
	}
	previous := state.form.providerID
	state.form.providerID = id
	if state.form.name == "" || state.form.name == channelDisplayName(previous) {
		state.form.name = channelDisplayName(id)
	}
	state.form.acknowledged = ""
}

func (state *channelSettingsUIState) requestLeaveForm(app *appState) {
	if state.view == channelSettingsViewCreate && state.formIsDirty() {
		state.dialogDiscard = true
		return
	}
	state.closeForm(app)
}

func (state *channelSettingsUIState) formIsDirty() bool {
	return strings.TrimSpace(state.form.name) != "" ||
		strings.TrimSpace(state.form.baseURL) != "" ||
		strings.TrimSpace(state.form.apiKey) != "" ||
		len(state.enabledModelIDs()) > 0
}

func (state *channelSettingsUIState) closeForm(app *appState) {
	state.view = channelSettingsViewList
	state.form = channelSettingsFormState{}
	state.dialogDiscard = false
	state.dialogRisk = ""
	state.dialogRiskAction = ""
	state.loginWatchActive = false
	if state.api != nil {
		state.loadStatus()
	}
	if state.refresh.MarkPending != nil {
		state.refresh.MarkPending(app)
	}
}

func (state *channelSettingsUIState) confirmDialog(app *appState, kind string) {
	switch kind {
	case "delete":
		providerID := state.dialogDelete
		state.dialogDelete = ""
		if providerID == "" || state.deleteBusy[providerID] || state.api == nil {
			return
		}
		state.deleteBusy[providerID] = true
		_, err := state.api.DeleteChannel(providerID)
		state.deleteBusy[providerID] = false
		if err != nil {
			state.statusErr = err.Error()
			return
		}
		state.loadStatus()
	case "remove":
		providerID := state.dialogRemove
		state.dialogRemove = ""
		if providerID == "" || state.deleteBusy[providerID] || state.api == nil {
			return
		}
		state.deleteBusy[providerID] = true
		err := state.api.ClearAPIKey(providerID)
		state.deleteBusy[providerID] = false
		if err != nil {
			state.statusErr = err.Error()
			return
		}
		state.loadStatus()
	case "discard":
		state.dialogDiscard = false
		state.closeForm(app)
	}
}

func (state *channelSettingsUIState) cancelDialog(kind string) {
	switch kind {
	case "delete":
		state.dialogDelete = ""
	case "remove":
		state.dialogRemove = ""
	case "discard":
		state.dialogDiscard = false
	}
}

// requireRiskAcknowledgement returns true when the base URL deviates from the
// acknowledged value and the risk dialog must run first.
func (state *channelSettingsUIState) requireRiskAcknowledgement() bool {
	baseURL := strings.TrimSpace(state.form.baseURL)
	return baseURL != "" && state.form.acknowledged != baseURL
}

func (state *channelSettingsUIState) acknowledgeAndRun(app *appState, action string) {
	state.form.acknowledged = strings.TrimSpace(state.form.baseURL)
	switch action {
	case "test":
		state.runTest(app)
	case "fetch":
		state.fetchModels(app)
	case "create":
		state.submitCreate(app)
	case "save":
		state.submitEdit(app)
	}
}

func (state *channelSettingsUIState) runTest(app *appState) {
	if state.api == nil || state.form.providerID == "" {
		return
	}
	if state.requireRiskAcknowledgement() {
		state.dialogRisk = "test"
		state.dialogRiskAction = "test"
		return
	}
	providerID := state.form.providerID
	if state.testBusy[providerID] {
		return
	}
	state.testBusy[providerID] = true
	result, err := state.api.TestConnection(providerID)
	state.testBusy[providerID] = false
	if err != nil {
		result = ChannelTestResult{Category: "failed"}
	}
	state.testResults[providerID] = result
}

func (state *channelSettingsUIState) fetchModels(app *appState) {
	if state.api == nil || state.form.providerID == "" {
		return
	}
	if state.requireRiskAcknowledgement() {
		state.dialogRisk = "fetch"
		state.dialogRiskAction = "fetch"
		return
	}
	providerID := state.form.providerID
	if state.fetchBusy[providerID] {
		return
	}
	state.fetchBusy[providerID] = true
	result, err := state.api.FetchModels(providerID)
	state.fetchBusy[providerID] = false
	if err != nil {
		result = ChannelModelsResult{Category: "failed"}
	}
	state.fetchResults[providerID] = result
	state.mergeFetchedModels(providerID, result)
}

// mergeFetchedModels applies the Proma merge rule: the fetched list is
// authoritative; manual models outside it are kept.
func (state *channelSettingsUIState) mergeFetchedModels(providerID string, result ChannelModelsResult) {
	if state.form.providerID != providerID || result.Category != "ok" {
		return
	}
	merged := make([]string, 0, len(result.Models)+len(state.form.manualModels))
	seen := map[string]bool{}
	for _, modelID := range result.Models {
		if !seen[modelID] {
			merged = append(merged, modelID)
			seen[modelID] = true
		}
	}
	for _, modelID := range state.form.manualModels {
		if !seen[modelID] {
			merged = append(merged, modelID)
			seen[modelID] = true
		}
	}
	state.form.availableModels = merged
}

func (state *channelSettingsUIState) setModelEnabled(modelID string, enabled bool) {
	if state.api == nil || state.form.providerID == "" || modelID == "" {
		return
	}
	providerID := state.form.providerID
	modelID = strings.TrimSpace(modelID)
	updated, err := state.api.SetModelEnabled(providerID, modelID, enabled)
	if err != nil {
		state.statusErr = err.Error()
		return
	}
	state.form.enabledModels = map[string]bool{}
	for _, entry := range updated {
		state.form.enabledModels[entry] = true
	}
}

func (state *channelSettingsUIState) addManualModel() {
	modelID := strings.TrimSpace(state.form.manualID)
	if modelID == "" {
		return
	}
	for _, existing := range state.form.availableModels {
		if existing == modelID {
			state.form.manualID = ""
			state.form.manualName = ""
			return
		}
	}
	state.form.availableModels = append(state.form.availableModels, modelID)
	state.form.manualModels = append(state.form.manualModels, modelID)
	state.form.manualID = ""
	state.form.manualName = ""
}

func (state *channelSettingsUIState) enabledModelIDs() []string {
	ids := make([]string, 0, len(state.form.enabledModels))
	for id, enabled := range state.form.enabledModels {
		if enabled {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func (state *channelSettingsUIState) unenabledModels() []string {
	enabled := state.enabledModelIDs()
	available := append([]string{}, state.form.availableModels...)
	unenabled := make([]string, 0, len(available))
	for _, modelID := range available {
		found := false
		for _, id := range enabled {
			if id == modelID {
				found = true
				break
			}
		}
		if !found {
			unenabled = append(unenabled, modelID)
		}
	}
	sort.Strings(unenabled)
	return unenabled
}

func (state *channelSettingsUIState) submitLabel() string {
	if state.form.saving {
		return text("channelsSaving")
	}
	if state.view == channelSettingsViewCreate {
		return text("channelsCreate")
	}
	return text("channelsSave")
}

// submitCreate writes the provider identity via the validated custom merge,
// then the literal API key. The key never reaches logs or the UI.
func (state *channelSettingsUIState) submitCreate(app *appState) {
	if state.api == nil || state.form.saving {
		return
	}
	providerID := strings.TrimSpace(state.form.providerID)
	if providerID == "" {
		state.statusErr = text("channelsProviderRequired")
		return
	}
	if state.requireRiskAcknowledgement() {
		state.dialogRisk = "create"
		state.dialogRiskAction = "create"
		return
	}
	state.form.saving = true
	err := state.writeProviderIdentity(providerID)
	if err == nil && strings.TrimSpace(state.form.apiKey) != "" {
		err = state.api.SetAPIKey(providerID, strings.TrimSpace(state.form.apiKey))
	}
	state.form.saving = false
	if err != nil {
		state.statusErr = text("channelsCreateFailed")
		return
	}
	state.view = channelSettingsViewList
	state.loadStatus()
	if state.refresh.MarkPending != nil {
		state.refresh.MarkPending(app)
	}
}

// submitEdit is the edit-mode counterpart: an empty key means "leave
// unchanged"; a new value replaces the stored literal and the file mode stays
// 0600.
func (state *channelSettingsUIState) submitEdit(app *appState) {
	if state.api == nil || state.form.saving {
		return
	}
	providerID := strings.TrimSpace(state.form.providerID)
	if state.requireRiskAcknowledgement() {
		state.dialogRisk = "save"
		state.dialogRiskAction = "save"
		return
	}
	state.form.saving = true
	err := state.writeProviderIdentity(providerID)
	if err == nil && strings.TrimSpace(state.form.apiKey) != "" {
		err = state.api.SetAPIKey(providerID, strings.TrimSpace(state.form.apiKey))
	}
	state.form.saving = false
	if err != nil {
		state.statusErr = text("channelsSaveFailed")
		return
	}
	state.loadStatus()
	if state.refresh.MarkPending != nil {
		state.refresh.MarkPending(app)
	}
}

// writeProviderIdentity merges the name/base URL into models.json through the
// existing digest-bound custom channel path, retrying once after a stale
// preview.
func (state *channelSettingsUIState) writeProviderIdentity(providerID string) error {
	if state.api == nil {
		return errors.New("channel config service is nil")
	}
	req := ChannelCustomRequest{
		ProviderID: providerID,
		Name:       strings.TrimSpace(state.form.name),
		BaseURL:    strings.TrimSpace(state.form.baseURL),
	}
	plan, err := state.api.PrepareCustom(req)
	if err != nil {
		return err
	}
	if _, err := state.api.ApplyCustom(plan.Request); err != nil {
		return err
	}
	return nil
}

func (state *channelSettingsUIState) setChannelEnabled(providerID string, enabled bool) {
	if state.api == nil {
		return
	}
	if _, err := state.api.SetChannelEnabled(providerID, enabled); err != nil {
		state.statusErr = err.Error()
	}
}

// startLoginWatch opens the Pi terminal login and polls the credential state
// every two seconds for up to 90 seconds. Tests drive pollLoginStep instead.
func (state *channelSettingsUIState) startLoginWatch(app *appState, providerID string) {
	if state.api == nil || providerID == "" {
		return
	}
	if state.loginWatchActive {
		return
	}
	state.loginWatchActive = true
	state.loginWatchProviderID = providerID
	interval := 2 * time.Second
	deadline := time.Now().Add(90 * time.Second)
	api := state.api
	if state.openPiLogin == nil {
		// P32 §4.4: the production default opens the Pi login session in the
		// system terminal; tests replace the seam before the first click, so
		// no real Terminal ever opens. Login errors stay silent here — the
		// 90-second watch reports "not ready" as its own state.
		state.openPiLogin = func(providerID string) {
			_ = app.openPiLogin(providerID)
		}
	}
	if state.openPiLogin != nil {
		state.openPiLogin(providerID)
	}
	go func() {
		for time.Now().Before(deadline) {
			time.Sleep(interval)
			status, err := api.CredentialStatus(providerID)
			done := err == nil && status.Status == "ready"
			app.postUI(func() {
				state.credentials[providerID] = status
				if done {
					state.loginWatchActive = false
					state.loginBanner = providerID
				}
			})
			if done {
				return
			}
		}
		app.postUI(func() {
			state.loginWatchActive = false
			state.loginWatchTimedOut = true
		})
	}()
}

// pollLoginStep is the single polling step extracted for deterministic tests.
func (state *channelSettingsUIState) pollLoginStep(providerID string) (bool, error) {
	if state.api == nil {
		return false, errors.New("channel config service is nil")
	}
	status, err := state.api.CredentialStatus(providerID)
	if err != nil {
		return false, err
	}
	state.credentials[providerID] = status
	return status.Status == "ready", nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
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

func channelDisplayName(id string) string {
	for _, preset := range ChannelPresets() {
		if preset.ID == id {
			return preset.Name
		}
	}
	return id
}

func channelPresetByID(id string) (ChannelPreset, bool) {
	for _, preset := range ChannelPresets() {
		if preset.ID == id {
			return preset, true
		}
	}
	return ChannelPreset{}, false
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
			s.channelSettingsSmallButton(c, p, "channel-refresh-retry",
				text("channelRefreshRetry"), "activity", false, func() {
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
