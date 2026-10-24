package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type channelSettingsFakeAPI struct {
	inventory         ChannelInventory
	inventoryErr      error
	inspect           ChannelInventory
	inspectErr        error
	importPlan        ChannelImportPlan
	importErr         error
	customPlan        ChannelCustomPlan
	customErr         error
	applyErr          []error
	inventoryCalls    int
	inspectPaths      []string
	importRequests    []ChannelImportRequest
	customRequests    []ChannelCustomRequest
	appliedImports    []ChannelImportRequest
	appliedCustomReqs []ChannelCustomRequest
	appliedResult     ChannelMergeResult
}

func (f *channelSettingsFakeAPI) Inventory() (ChannelInventory, error) {
	f.inventoryCalls++
	return f.inventory, f.inventoryErr
}

func (f *channelSettingsFakeAPI) InspectModelsFile(path string) (ChannelInventory, error) {
	f.inspectPaths = append(f.inspectPaths, path)
	return f.inspect, f.inspectErr
}

func (f *channelSettingsFakeAPI) PrepareImport(req ChannelImportRequest) (ChannelImportPlan, error) {
	f.importRequests = append(f.importRequests, req)
	plan := f.importPlan
	plan.Request = req
	if plan.Preview.ProviderID == "" {
		plan.Preview = ChannelMergePreview{
			ProviderID:     req.ProviderID,
			ProviderAction: channelMergePreserve,
			ConflictPolicy: channelMergePreserve,
			Conflicts:      []ChannelConflict{},
			Models:         []ChannelMergeItem{},
			ModelOverrides: []ChannelMergeItem{},
			Notices:        []ChannelNotice{},
		}
	}
	return plan, f.importErr
}

func (f *channelSettingsFakeAPI) ApplyImport(req ChannelImportRequest) (ChannelMergeResult, error) {
	f.appliedImports = append(f.appliedImports, req)
	if len(f.applyErr) > 0 {
		err := f.applyErr[0]
		f.applyErr = f.applyErr[1:]
		return ChannelMergeResult{}, err
	}
	result := f.appliedResult
	if result.Preview.ProviderID == "" {
		result.Preview = f.importPlan.Preview
	}
	return result, nil
}

func (f *channelSettingsFakeAPI) PrepareCustom(req ChannelCustomRequest) (ChannelCustomPlan, error) {
	f.customRequests = append(f.customRequests, req)
	plan := f.customPlan
	plan.Request = req
	if plan.Preview.ProviderID == "" {
		plan.Preview = ChannelMergePreview{
			ProviderID:     req.ProviderID,
			ProviderAction: channelMergeAdd,
			ConflictPolicy: channelMergePreserve,
			Conflicts:      []ChannelConflict{},
			Models:         []ChannelMergeItem{},
			ModelOverrides: []ChannelMergeItem{},
			Notices:        []ChannelNotice{},
		}
	}
	return plan, f.customErr
}

func (f *channelSettingsFakeAPI) ApplyCustom(req ChannelCustomRequest) (ChannelMergeResult, error) {
	f.appliedCustomReqs = append(f.appliedCustomReqs, req)
	if len(f.applyErr) > 0 {
		err := f.applyErr[0]
		f.applyErr = f.applyErr[1:]
		return ChannelMergeResult{}, err
	}
	result := f.appliedResult
	if result.Preview.ProviderID == "" {
		result.Preview = f.customPlan.Preview
	}
	return result, nil
}

func channelSettingsTestState(
	t *testing.T,
	api channelSettingsAPI,
) (*appState, *channelSettingsUIState) {
	t.Helper()
	app := layoutTestState(t)
	state := installChannelSettingsTestDependencies(
		app,
		api,
		func(*mygo.Window) (string, error) {
			t.Fatal("test unexpectedly opened the real file picker")
			return "", nil
		},
		channelSettingsRefreshBridge{},
	)
	t.Cleanup(func() {
		resetChannelSettingsForTest(app)
	})
	return app, state
}

func renderChannelSettings(t *testing.T, app *appState, width, height int) *ui.Tester {
	t.Helper()
	tester := ui.NewTester(func(c *ui.Context) {
		theme, palette := nativeThemeWithContrast(false, false)
		c.SetTheme(theme)
		app.channelSettingsContent(c, palette)
	}, width, height)
	return tester
}

func TestChannelSettingsInventoryStatesAndRetry(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		api := &channelSettingsFakeAPI{
			inventory: ChannelInventory{
				ConfigPath: "/tmp/pi/models.json",
				Exists:     false,
				Providers:  []ChannelProviderSummary{},
			},
		}
		app, state := channelSettingsTestState(t, api)
		tester := renderChannelSettings(t, app, 960, 800)
		if _, ok := tester.Find(text("channelInventoryEmptyTitle")); !ok {
			t.Fatalf("empty inventory state was not rendered: %v", tester.Texts())
		}
		if _, ok := tester.Find(text("channelSourcePreset")); !ok {
			t.Fatal("empty inventory must still offer channel sources")
		}
		if state.inventoryErr != "" || !state.inventoryLoaded {
			t.Fatalf("unexpected empty inventory state: %#v", state)
		}
	})

	t.Run("read error and retry", func(t *testing.T) {
		api := &channelSettingsFakeAPI{
			inventoryErr: errors.New("cannot read models.json"),
		}
		app, state := channelSettingsTestState(t, api)
		tester := renderChannelSettings(t, app, 960, 800)
		if _, ok := tester.Find(textArgs("channelInventoryReadFailed", map[string]any{
			"detail": "cannot read models.json",
		})); !ok {
			t.Fatalf("inventory error was not rendered: %v", tester.Texts())
		}
		api.inventoryErr = nil
		api.inventory = ChannelInventory{
			ConfigPath: "/tmp/pi/models.json",
			Exists:     true,
			Providers: []ChannelProviderSummary{{
				ID:         "openai",
				Credential: ChannelCredentialSummary{Configured: true, Kind: channelCredentialEnvironment},
			}},
		}
		if err := tester.Click(text("channelRefreshInventory")); err != nil {
			t.Fatal(err)
		}
		if state.inventoryErr != "" || len(state.inventory.Providers) != 1 {
			t.Fatalf("inventory retry did not recover: %#v", state)
		}
		if api.inventoryCalls != 2 {
			t.Fatalf("inventory calls = %d, want 2", api.inventoryCalls)
		}
	})
}

func TestChannelSettingsFileImportPreviewPreservesConflictAndExplicitApply(t *testing.T) {
	api := &channelSettingsFakeAPI{
		inventory: ChannelInventory{ConfigPath: "/tmp/pi/models.json", Exists: true, Providers: []ChannelProviderSummary{}},
		inspect: ChannelInventory{
			ConfigPath: "/tmp/source-models.json",
			Exists:     true,
			Providers: []ChannelProviderSummary{{
				ID:   "acme",
				Name: "Acme",
				Models: []ChannelModelSummary{
					{ID: "shared"},
					{ID: "added"},
				},
				ModelOverride: []ChannelModelSummary{{ID: "override"}},
			}},
		},
		importPlan: ChannelImportPlan{Preview: ChannelMergePreview{
			ProviderID:     "acme",
			ProviderAction: channelMergePreserve,
			ConflictPolicy: channelMergePreserve,
			Conflicts: []ChannelConflict{{
				Code:   channelNoticeProviderEndpointConflict,
				Path:   providerPath("acme"),
				Fields: []string{providerPath("acme") + ".baseUrl"},
			}},
			Models: []ChannelMergeItem{
				{ID: "shared", Action: channelMergePreserve},
				{ID: "added", Action: channelMergeAdd},
			},
			ModelOverrides: []ChannelMergeItem{},
			Notices: []ChannelNotice{{
				Code:   channelNoticeProviderEndpointConflict,
				Path:   providerPath("acme"),
				Action: channelMergePreserve,
			}},
		}},
	}
	app, state := channelSettingsTestState(t, api)
	state.mode = channelSettingsSourceModels
	state.sourcePath = "/tmp/source-models.json"
	state.inspectSourceFile()
	_ = renderChannelSettings(t, app, 960, 1100)

	if state.sourceProviderID != "acme" {
		t.Fatalf("source provider = %q, want acme", state.sourceProviderID)
	}
	state.selectedModels["shared"] = true
	state.selectedModels["added"] = true
	state.prepareImport()
	if len(api.importRequests) != 1 {
		t.Fatalf("preview requests = %d, want 1", len(api.importRequests))
	}
	if len(api.appliedImports) != 0 {
		t.Fatal("preview wrote the import without an explicit apply")
	}
	if len(state.preview.Models) != 2 || state.preview.Models[0].Action != channelMergePreserve ||
		len(state.preview.Conflicts) != 1 {
		t.Fatalf("unexpected safe preview: %#v", state.preview)
	}
	state.apply(app)
	if len(api.appliedImports) != 1 || state.result == nil {
		t.Fatalf("explicit apply did not complete: applied=%d result=%#v", len(api.appliedImports), state.result)
	}
}

func TestChannelSettingsCustomPresetAndValidation(t *testing.T) {
	api := &channelSettingsFakeAPI{
		inventory: ChannelInventory{ConfigPath: "/tmp/pi/models.json", Exists: true, Providers: []ChannelProviderSummary{}},
		customPlan: ChannelCustomPlan{Preview: ChannelMergePreview{
			ProviderID:     "openai",
			ProviderAction: channelMergeAdd,
			ConflictPolicy: channelMergePreserve,
			Conflicts:      []ChannelConflict{},
			Models:         []ChannelMergeItem{},
			ModelOverrides: []ChannelMergeItem{},
			Notices:        []ChannelNotice{},
		}},
	}
	app, state := channelSettingsTestState(t, api)
	state.mode = channelSettingsSourcePreset
	state.selectPreset(ChannelPreset{ID: "openai", Name: "OpenAI", APIKeyEnvironment: "OPENAI_API_KEY"})
	state.modelID = "gpt-test"
	state.preparePreset()
	if len(api.customRequests) != 1 {
		t.Fatal("preset preview did not call the typed service")
	}
	request := api.customRequests[0]
	if request.ProviderID != "openai" || request.APIKeyEnv != "OPENAI_API_KEY" ||
		request.BaseURL != "" || request.API != "" || len(request.Models) != 1 {
		t.Fatalf("unexpected preset request: %#v", request)
	}
	state.apply(app)
	if len(api.appliedCustomReqs) != 1 {
		t.Fatal("preset apply did not use the bound custom plan")
	}

	api.customErr = errors.New("apiKeyEnv must use Pi syntax $NAME or ${NAME}")
	state.mode = channelSettingsSourceCustom
	state.providerID = "broken"
	state.apiKeyEnv = "SECRET_VALUE"
	state.prepareCustom()
	if state.preview != nil || !state.previewErrContains("apiKeyEnv must use Pi syntax") {
		t.Fatalf("validation failure was not exposed without a plan: %#v", state)
	}
}

func (s *channelSettingsUIState) previewErrContains(value string) bool {
	return s != nil && s.previewErr != "" &&
		(containsString(s.previewErr, value) || containsString(s.previewErrDetail, value))
}

func containsString(value, part string) bool {
	return len(part) == 0 || len(value) >= len(part) && indexString(value, part) >= 0
}

func indexString(value, part string) int {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return i
		}
	}
	return -1
}

func TestChannelSettingsPreviewInvalidationAndUnboundApply(t *testing.T) {
	api := &channelSettingsFakeAPI{
		inventory: ChannelInventory{Providers: []ChannelProviderSummary{}},
		customPlan: ChannelCustomPlan{Preview: ChannelMergePreview{
			ProviderID:     "local",
			ProviderAction: channelMergeAdd,
			ConflictPolicy: channelMergePreserve,
			Conflicts:      []ChannelConflict{},
			Models:         []ChannelMergeItem{},
			ModelOverrides: []ChannelMergeItem{},
			Notices:        []ChannelNotice{},
		}},
	}
	app, state := channelSettingsTestState(t, api)
	state.mode = channelSettingsSourceCustom
	state.providerID = "local"
	state.prepareCustom()
	if !state.planBound() {
		t.Fatal("custom preview was not bound")
	}
	state.invalidatePreview()
	state.apply(app)
	if len(api.appliedCustomReqs) != 0 || state.applyErr == "" {
		t.Fatal("an invalidated preview could still apply")
	}
}

func TestChannelSettingsStalePreviewKeepsDraftAndReprepare(t *testing.T) {
	api := &channelSettingsFakeAPI{
		inventory: ChannelInventory{Providers: []ChannelProviderSummary{}},
		customPlan: ChannelCustomPlan{Preview: ChannelMergePreview{
			ProviderID:     "local",
			ProviderAction: channelMergeAdd,
			ConflictPolicy: channelMergePreserve,
			Conflicts:      []ChannelConflict{},
			Models:         []ChannelMergeItem{},
			ModelOverrides: []ChannelMergeItem{},
			Notices:        []ChannelNotice{},
		}},
		applyErr: []error{&ChannelPreviewStaleError{
			Code: channelErrorPreviewStale,
			Path: "/tmp/pi/models.json",
		}},
	}
	app, state := channelSettingsTestState(t, api)
	state.mode = channelSettingsSourceCustom
	state.providerID = "local"
	state.providerName = "Local"
	state.baseURL = "http://127.0.0.1:8080/v1"
	state.apiName = "openai-completions"
	state.modelID = "model"
	state.prepareCustom()
	state.apply(app)
	if !state.stale || state.customPlan != nil || state.preview == nil {
		t.Fatalf("stale apply did not clear the bound plan while retaining preview: %#v", state)
	}
	if state.providerName != "Local" || state.modelID != "model" {
		t.Fatal("stale apply discarded the custom draft")
	}
	state.reprepare()
	if !state.planBound() || state.stale {
		t.Fatalf("re-preview did not bind a fresh plan: %#v", state)
	}
	state.apply(app)
	if len(api.appliedCustomReqs) != 2 || state.result == nil {
		t.Fatalf("retry after re-preview did not apply: applies=%d result=%#v",
			len(api.appliedCustomReqs), state.result)
	}
}

func TestChannelSettingsGenericApplyFailureKeepsBoundPlanForRetry(t *testing.T) {
	api := &channelSettingsFakeAPI{
		inventory: ChannelInventory{Providers: []ChannelProviderSummary{}},
		importPlan: ChannelImportPlan{Preview: ChannelMergePreview{
			ProviderID:     "acme",
			ProviderAction: channelMergeAdd,
			ConflictPolicy: channelMergePreserve,
			Conflicts:      []ChannelConflict{},
			Models:         []ChannelMergeItem{},
			ModelOverrides: []ChannelMergeItem{},
			Notices:        []ChannelNotice{},
		}},
		applyErr: []error{errors.New("temporary write failure")},
	}
	app, state := channelSettingsTestState(t, api)
	state.mode = channelSettingsSourceModels
	state.sourcePath = "/tmp/source.json"
	state.sourceProviderID = "acme"
	state.prepareImport()
	state.apply(app)
	if state.applyErr != text("channelApplyFailedSummary") ||
		state.applyErrDetail != "temporary write failure" || state.importPlan == nil {
		t.Fatalf("generic failure lost the bound plan: %#v", state)
	}
	state.apply(app)
	if len(api.appliedImports) != 2 || state.result == nil || state.applyErr != "" {
		t.Fatalf("retry did not complete: applies=%d result=%#v err=%q",
			len(api.appliedImports), state.result, state.applyErr)
	}
}

func TestChannelSettingsPresetRealServicePreviewAndApply(t *testing.T) {
	agentDir := t.TempDir()
	service, err := NewChannelConfigService(agentDir)
	if err != nil {
		t.Fatal(err)
	}
	app, state := channelSettingsTestState(t, service)
	preset, ok := channelPresetByID("openai")
	if !ok {
		t.Fatal("OpenAI preset is missing")
	}
	if preset.APIKeyEnvironment != "$OPENAI_API_KEY" {
		t.Fatalf("OpenAI preset environment = %q, want $OPENAI_API_KEY", preset.APIKeyEnvironment)
	}
	state.mode = channelSettingsSourcePreset
	state.selectPreset(preset)
	state.modelID = "gpt-test"
	state.preparePreset()
	if state.previewErr != "" || state.customPlan == nil {
		t.Fatalf("real preset preview failed: preview=%q detail=%q", state.previewErr, state.previewErrDetail)
	}
	configPath := filepath.Join(agentDir, "models.json")
	if _, err := os.Stat(configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preview wrote models.json: %v", err)
	}

	state.apply(app)
	if state.applyErr != "" || state.result == nil {
		t.Fatalf("real preset apply failed: err=%q detail=%q", state.applyErr, state.applyErrDetail)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	providers, ok := document["providers"].(map[string]any)
	if !ok {
		t.Fatalf("providers missing: %#v", document)
	}
	provider, ok := providers["openai"].(map[string]any)
	if !ok {
		t.Fatalf("openai provider missing: %#v", providers)
	}
	if provider["apiKey"] != "$OPENAI_API_KEY" {
		t.Fatalf("OpenAI apiKey = %#v, want $OPENAI_API_KEY", provider["apiKey"])
	}
}

func TestChannelSettingsLocalizedErrorsAndStableSelection(t *testing.T) {
	if got := selectedModelIDs(map[string]bool{"z": true, "a": true, "b": false}); fmt.Sprint(got) != "[a z]" {
		t.Fatalf("selected model order = %v, want [a z]", got)
	}
	expectedActions := map[string]string{
		channelMergeAdd:      text("channelMergeActionAdd"),
		channelMergePreserve: text("channelMergeActionPreserve"),
		"inherit":            text("channelMergeActionInherit"),
		"removed":            text("channelMergeActionRemoved"),
		"unexpected":         text("channelMergeActionUnknown"),
	}
	for action, want := range expectedActions {
		if got := channelMergeActionLabel(action); got != want {
			t.Fatalf("action %q label = %q, want %q", action, got, want)
		}
	}

	api := &channelSettingsFakeAPI{
		inventory: ChannelInventory{Providers: []ChannelProviderSummary{}},
		customErr: errors.New("backend exploded"),
	}
	app, state := channelSettingsTestState(t, api)
	state.mode = channelSettingsSourceCustom
	state.providerID = "local"
	state.prepareCustom()
	if state.previewErr != text("channelPreviewFailedSummary") ||
		state.previewErrDetail != "backend exploded" {
		t.Fatalf("preview error was not localized with detail: %#v", state)
	}
	tester := renderChannelSettings(t, app, 700, 1000)
	if !tester.HasText(text("channelPreviewFailedSummary")) || !tester.HasText("backend exploded") {
		t.Fatalf("localized preview error or detail was omitted: %v", tester.Texts())
	}
}

func TestChannelSettingsPresetEnvironmentIsInAdvancedDisclosure(t *testing.T) {
	api := &channelSettingsFakeAPI{
		inventory: ChannelInventory{Providers: []ChannelProviderSummary{}},
	}
	app, state := channelSettingsTestState(t, api)
	state.mode = channelSettingsSourcePreset
	preset, ok := channelPresetByID("openai")
	if !ok {
		t.Fatal("OpenAI preset is missing")
	}
	state.selectPreset(preset)
	tester := renderChannelSettings(t, app, 700, 1000)
	if tester.HasText(text("channelAPIKeyEnvironment")) {
		t.Fatalf("preset environment field was visible by default: %v", tester.Texts())
	}
	if err := tester.Click(text("channelAdvanced")); err != nil {
		t.Fatal(err)
	}
	if !state.presetAdvancedOpen {
		t.Fatal("advanced disclosure did not open")
	}
	if !tester.HasText(text("channelAPIKeyEnvironment")) {
		t.Fatalf("advanced disclosure omitted environment field: %v", tester.Texts())
	}
}

func TestChannelSettingsSuccessReloadsInventoryAndMarksRefreshPending(t *testing.T) {
	api := &channelSettingsFakeAPI{
		inventory: ChannelInventory{Providers: []ChannelProviderSummary{}},
		customPlan: ChannelCustomPlan{Preview: ChannelMergePreview{
			ProviderID:     "local",
			ProviderAction: channelMergeAdd,
			ConflictPolicy: channelMergePreserve,
			Conflicts:      []ChannelConflict{},
			Models:         []ChannelMergeItem{},
			ModelOverrides: []ChannelMergeItem{},
			Notices:        []ChannelNotice{},
		}},
		appliedResult: ChannelMergeResult{Changed: true},
	}
	app, state := channelSettingsTestState(t, api)
	pending := 0
	state.refresh.MarkPending = func(got *appState) {
		if got != app {
			t.Fatalf("refresh bridge got %p, want %p", got, app)
		}
		pending++
	}
	state.mode = channelSettingsSourceCustom
	state.providerID = "local"
	state.prepareCustom()
	before := api.inventoryCalls
	state.apply(app)
	if api.inventoryCalls != before+1 {
		t.Fatalf("successful apply inventory calls = %d, want %d", api.inventoryCalls, before+1)
	}
	if pending != 1 {
		t.Fatalf("MarkPending calls = %d, want 1", pending)
	}
	if state.result == nil || !state.result.Changed {
		t.Fatalf("apply result was not retained: %#v", state.result)
	}
}

func TestChannelSettingsRefreshStatusAndRetry(t *testing.T) {
	api := &channelSettingsFakeAPI{
		inventory: ChannelInventory{
			Exists:    true,
			Providers: []ChannelProviderSummary{{ID: "openai"}},
		},
	}
	app, state := channelSettingsTestState(t, api)
	retries := 0
	state.refresh = channelSettingsRefreshBridge{
		Status: func(*appState) ChannelRefreshStatus {
			return ChannelRefreshStatus{
				Phase:     channelRefreshPhaseFailed,
				Failure:   channelRefreshFailureModelUnavailable,
				Retryable: true,
			}
		},
		Retry: func(got *appState) {
			if got != app {
				t.Fatalf("retry got %p, want %p", got, app)
			}
			retries++
		},
	}
	tester := renderChannelSettings(t, app, 960, 900)
	if !tester.HasText(textArgs("channelRefreshFailed", map[string]any{
		"reason": text("channelRefreshFailureModel"),
	})) {
		t.Fatalf("refresh failure was not rendered: %v", tester.Texts())
	}
	if err := tester.Click(text("channelRefreshRetry")); err != nil {
		t.Fatal(err)
	}
	if retries != 1 {
		t.Fatalf("refresh retries = %d, want 1", retries)
	}
}

func TestChannelSettingsNarrowLayoutKeepsSourceControlsVisible(t *testing.T) {
	api := &channelSettingsFakeAPI{
		inventory: ChannelInventory{
			Exists: true,
			Providers: []ChannelProviderSummary{{
				ID:   "provider-with-a-long-identifier",
				Name: "A visible provider with a long display name",
				Models: []ChannelModelSummary{{
					ID:   "model-with-a-long-identifier",
					Name: "A model with a long visible name",
				}},
			}},
		},
	}
	app, state := channelSettingsTestState(t, api)
	state.mode = channelSettingsSourceCustom
	tester := renderChannelSettings(t, app, 360, 900)
	for _, label := range []string{
		text("channelSettingsTitle"),
		text("channelSourcePreset"),
		text("channelSourceCustom"),
		text("channelSourceModelsFile"),
		text("channelProviderID"),
	} {
		rect, ok := tester.Find(label)
		if !ok {
			t.Fatalf("narrow layout omitted %q: %v", label, tester.Texts())
		}
		if rect.W <= 0 {
			t.Logf("narrow control %q rect: %#v; all rects omitted", label, rect)
			t.Fatalf("narrow control %q has no visible width", label)
		}
		if rect.W > 360 {
			t.Fatalf("narrow control %q overflows viewport: %#v", label, rect)
		}
	}

	rect, ok := tester.Find(text("channelPreviewCustom"))
	if !ok {
		t.Fatal("narrow preview action was omitted")
	}
	if rect.X < 0 || rect.X+max(rect.W, 1) > 360 {
		t.Fatalf("narrow preview action overflows horizontally: %#v", rect)
	}
}

func TestChannelSettingsPickerErrorKeepsPageUsable(t *testing.T) {
	api := &channelSettingsFakeAPI{
		inventory: ChannelInventory{Providers: []ChannelProviderSummary{}},
	}
	app, state := channelSettingsTestState(t, api)
	state.mode = channelSettingsSourceModels
	state.picker = func(*mygo.Window) (string, error) {
		return "", fmt.Errorf("picker unavailable")
	}
	tester := renderChannelSettings(t, app, 700, 900)
	if err := tester.Click(text("channelChooseModelsFile")); err != nil {
		t.Fatal(err)
	}
	if state.sourceErr != "picker unavailable" {
		t.Fatalf("picker error = %q", state.sourceErr)
	}
	if _, ok := tester.Find(text("channelSourceCustom")); !ok {
		t.Fatal("picker failure disabled the remaining source modes")
	}
}

// TestChannelSettingsEveryStepRendersNativeWidgets drives the production
// renderer through each source and recovery state. MyGo construction errors
// such as an unknown icon or a late checkbox key must fail here before a real
// settings window can panic.
func TestChannelSettingsEveryStepRendersNativeWidgets(t *testing.T) {
	api := &channelSettingsFakeAPI{
		inventory: ChannelInventory{
			Exists: true,
			Providers: []ChannelProviderSummary{
				{
					ID:         "openai",
					Name:       "OpenAI",
					Credential: ChannelCredentialSummary{Configured: true, Kind: channelCredentialEnvironment},
					Models:     []ChannelModelSummary{{ID: "gpt-existing"}},
				},
				{
					ID:         "custom",
					BaseURL:    "https://custom.example/v1",
					API:        "openai-completions",
					Credential: ChannelCredentialSummary{Kind: channelCredentialNone},
					Models:     []ChannelModelSummary{},
				},
			},
		},
		inspect: ChannelInventory{
			Exists: true,
			Providers: []ChannelProviderSummary{{
				ID:      "acme",
				Name:    "Acme",
				BaseURL: "https://acme.example/v1",
				API:     "openai-completions",
				Models: []ChannelModelSummary{
					{ID: "shared", Name: "Shared"},
					{ID: "added", Name: "Added"},
				},
				ModelOverride: []ChannelModelSummary{{ID: "builtin", Name: "Built In"}},
			}},
		},
		importPlan: ChannelImportPlan{Preview: ChannelMergePreview{
			Source:         ChannelFileVersion{Path: "/tmp/source.json", Exists: true, Digest: "sha256:source"},
			Destination:    ChannelFileVersion{Path: "/tmp/destination.json", Exists: true, Digest: "sha256:destination"},
			ProviderID:     "acme",
			ProviderAction: channelMergePreserve,
			ConflictPolicy: channelMergePreserve,
			Conflicts: []ChannelConflict{{
				Code:   channelNoticeProviderEndpointConflict,
				Path:   providerPath("acme"),
				Fields: []string{providerPath("acme") + ".baseUrl"},
			}},
			Models: []ChannelMergeItem{
				{ID: "shared", Action: channelMergePreserve},
				{ID: "added", Action: channelMergeAdd, Verification: "pendingInPi"},
			},
			ModelOverrides: []ChannelMergeItem{},
			Notices: []ChannelNotice{
				{Code: channelNoticeConfigureInPi, Path: providerPath("acme") + ".apiKey", Action: channelNoticeConfigureInPi},
				{Code: channelNoticeCommentsRemoved, Path: "/tmp/destination.json", Action: "removed"},
			},
		}},
		customPlan: ChannelCustomPlan{Preview: ChannelMergePreview{
			Source:         ChannelFileVersion{Path: "custom", Exists: true, Digest: "sha256:custom"},
			Destination:    ChannelFileVersion{Path: "/tmp/destination.json", Exists: true, Digest: "sha256:destination"},
			ProviderID:     "local",
			ProviderAction: channelMergeAdd,
			ConflictPolicy: channelMergePreserve,
			Conflicts:      []ChannelConflict{},
			Models:         []ChannelMergeItem{},
			ModelOverrides: []ChannelMergeItem{},
			Notices:        []ChannelNotice{},
		}},
		appliedResult: ChannelMergeResult{Changed: true},
	}
	app, state := channelSettingsTestState(t, api)
	pending := 0
	refreshPhase := channelRefreshPhaseIdle
	state.refresh = channelSettingsRefreshBridge{
		Status: func(*appState) ChannelRefreshStatus {
			return ChannelRefreshStatus{
				Phase:     refreshPhase,
				Failure:   channelRefreshFailureModelUnavailable,
				Retryable: refreshPhase == channelRefreshPhaseFailed,
			}
		},
		MarkPending: func(*appState) {
			pending++
		},
		Retry: func(*appState) {
			t.Fatal("render-only step unexpectedly triggered refresh retry")
		},
	}

	steps := []struct {
		name          string
		setup         func()
		wantText      string
		width, height int
	}{
		{
			name:     "inventory list",
			wantText: textArgs("channelInventoryProviderCount", map[string]any{"count": 2}),
			width:    960, height: 900,
		},
		{
			name: "models file picker and provider selection",
			setup: func() {
				state.switchMode(channelSettingsSourceModels)
			},
			wantText: text("channelChooseModelsFile"),
			width:    700, height: 900,
		},
		{
			name: "selected source provider with model checkboxes",
			setup: func() {
				state.sourcePath = "/tmp/source.json"
				state.inspectSourceFile()
				state.mode = channelSettingsSourceModels
			},
			wantText: text("channelSelectModels"),
			width:    360, height: 900,
		},
		{
			name: "import preview conflict and notices",
			setup: func() {
				state.selectedModels["shared"] = true
				state.selectedModels["added"] = true
				state.prepareImport()
			},
			wantText: text("channelPreviewTitle"),
			width:    960, height: 1200,
		},
		{
			name: "import explicit apply result",
			setup: func() {
				state.apply(app)
				state.preview = &api.importPlan.Preview
				state.result = &ChannelMergeResult{Preview: api.importPlan.Preview, Changed: true}
			},
			wantText: text("channelApplySucceeded"),
			width:    960, height: 1200,
		},
		{
			name: "preset selection and form",
			setup: func() {
				state.resetForAnotherSource()
				state.mode = channelSettingsSourcePreset
				state.selectPreset(ChannelPreset{
					ID: "openai", Name: "OpenAI", APIKeyEnvironment: "OPENAI_API_KEY",
				})
			},
			wantText: text("channelPreviewPreset"),
			width:    700, height: 1000,
		},
		{
			name: "Pi login preset",
			setup: func() {
				state.selectPreset(ChannelPreset{
					ID: "openai-codex", Name: "OpenAI Codex", UsesPiLogin: true,
				})
			},
			wantText: text("channelPiLoginManagedTitle"),
			width:    700, height: 1000,
		},
		{
			name: "custom form with authorization checkbox",
			setup: func() {
				state.switchMode(channelSettingsSourceCustom)
				state.providerID = "local"
			},
			wantText: text("channelUseAuthorizationHeader"),
			width:    360, height: 1000,
		},
		{
			name: "custom preview",
			setup: func() {
				state.baseURL = "http://127.0.0.1:8080/v1"
				state.apiName = "openai-completions"
				state.modelID = "model"
				state.prepareCustom()
			},
			wantText: text("channelApplyCustom"),
			width:    700, height: 1000,
		},
		{
			name: "stale preview recovery",
			setup: func() {
				state.customPlan = nil
				state.stale = true
				state.previewErr = text("channelPreviewStaleDescription")
			},
			wantText: text("channelRepreview"),
			width:    700, height: 1000,
		},
		{
			name: "apply error",
			setup: func() {
				state.reprepare()
				state.applyErr = "temporary write failure"
			},
			wantText: "temporary write failure",
			width:    700, height: 1000,
		},
		{
			name: "refresh pending",
			setup: func() {
				refreshPhase = channelRefreshPhasePending
			},
			wantText: text("channelRefreshPending"),
			width:    700, height: 1000,
		},
		{
			name: "refresh retry",
			setup: func() {
				refreshPhase = channelRefreshPhaseFailed
			},
			wantText: text("channelRefreshRetry"),
			width:    700, height: 1000,
		},
	}

	for _, step := range steps {
		if step.setup != nil {
			step.setup()
		}
		t.Run(step.name, func(t *testing.T) {
			tester := renderChannelSettings(t, app, step.width, step.height)
			if !tester.HasText(step.wantText) {
				t.Fatalf("renderer omitted %q; texts=%v", step.wantText, tester.Texts())
			}
		})
	}
	if pending == 0 {
		t.Fatal("apply path never called MarkPending")
	}
}

func TestChannelSettingsUsesOnlyRegisteredIcons(t *testing.T) {
	icons := []string{"activity", "folder", "check", "plus", "search"}
	for _, name := range icons {
		if iconSet[name] == nil {
			t.Fatalf("channel settings icon %q is not registered", name)
		}
	}
	oldNewAPI := channelSettingsNewAPI
	t.Cleanup(func() { channelSettingsNewAPI = oldNewAPI })
	channelSettingsNewAPI = func() (channelSettingsAPI, error) {
		return &channelSettingsFakeAPI{
			inventory: ChannelInventory{Providers: []ChannelProviderSummary{}},
		}, nil
	}
	app := layoutTestState(t)
	resetChannelSettingsForTest(app)
	t.Cleanup(func() { resetChannelSettingsForTest(app) })
	renderChannelSettings(t, app, 700, 900)
}

func TestChannelSettingsInventoryUsesCompactExpandableChannels(t *testing.T) {
	tempDir := t.TempDir()
	agentDir := filepath.Join(tempDir, "agent")
	configPath := filepath.Join(agentDir, "models.json")
	api := &channelSettingsFakeAPI{
		inventory: ChannelInventory{
			AgentDir:   agentDir,
			ConfigPath: configPath,
			Exists:     true,
			Providers: []ChannelProviderSummary{{
				ID:      "provider-id",
				Name:    "Provider Name",
				BaseURL: "https://provider.example/v1",
				API:     "openai-completions",
				Credential: ChannelCredentialSummary{
					Configured: true,
					Kind:       channelCredentialLiteral,
				},
				Models: []ChannelModelSummary{{ID: "model-id", Name: "Model Name"}},
			}},
		},
	}
	app, state := channelSettingsTestState(t, api)
	tester := renderChannelSettings(t, app, 700, 900)

	title, ok := tester.Find(text("channelSettingsTitle"))
	if !ok {
		t.Fatal("settings title was not rendered")
	}
	addSource, ok := tester.Find(text("channelAddSourceTitle"))
	if !ok || addSource.Y <= title.Y {
		t.Fatalf("add-channel controls must appear above configured channels: title=%#v source=%#v", title, addSource)
	}
	inventory, ok := tester.Find(text("channelInventoryTitle"))
	if !ok || addSource.Y >= inventory.Y {
		t.Fatalf("add-channel controls must appear above configured channels: source=%#v inventory=%#v", addSource, inventory)
	}
	if tester.HasText(text("channelSettingsDescription")) ||
		tester.HasText(text("channelAddSourceDescription")) {
		t.Fatalf("redundant descriptions must stay out of the settings page: %v", tester.Texts())
	}
	if tester.HasText(agentDir) || tester.HasText(configPath) {
		t.Fatalf("inventory paths must be collapsed by default: %v", tester.Texts())
	}
	if tester.HasText("model-id") || tester.HasText("https://provider.example/v1") {
		t.Fatalf("channel details must be collapsed by default: %v", tester.Texts())
	}
	if !tester.HasText("Provider Name") || !tester.HasText("provider-id") {
		t.Fatalf("compact channel header omitted name or distinct ID: %v", tester.Texts())
	}
	if !tester.HasText(text("channelConfiguredInPi")) {
		t.Fatalf("configured channel omitted safe Pi status: %v", tester.Texts())
	}
	if tester.HasText(text("channelCredentialLiteral")) {
		t.Fatalf("configured channel exposed credential kind: %v", tester.Texts())
	}

	if err := tester.Click(text("channelDetails")); err != nil {
		t.Fatal(err)
	}
	if !state.providerDetails["provider-id"] {
		t.Fatal("channel details did not stay open")
	}
	for _, value := range []string{
		"openai-completions",
		"https://provider.example/v1",
		"model-id",
	} {
		if !tester.HasText(value) {
			t.Fatalf("expanded channel details omitted %q: %v", value, tester.Texts())
		}
	}
}

func TestChannelSettingsPreviewDiagnosticsAreOptional(t *testing.T) {
	api := &channelSettingsFakeAPI{
		inventory: ChannelInventory{Providers: []ChannelProviderSummary{}},
		importPlan: ChannelImportPlan{Preview: ChannelMergePreview{
			Source:         ChannelFileVersion{Path: "/tmp/source.json", Exists: true, Digest: "sha256:source"},
			Destination:    ChannelFileVersion{Path: "/tmp/destination.json", Exists: true, Digest: "sha256:destination"},
			ProviderID:     "openai",
			ProviderAction: channelMergePreserve,
			ConflictPolicy: channelMergePreserve,
			Conflicts:      []ChannelConflict{},
			Models:         []ChannelMergeItem{},
			ModelOverrides: []ChannelMergeItem{},
			Notices:        []ChannelNotice{},
		}},
	}
	app, state := channelSettingsTestState(t, api)
	state.preview = &api.importPlan.Preview
	state.previewKind = channelSettingsPreviewImport

	tester := renderChannelSettings(t, app, 700, 1000)
	if !tester.HasText(text("channelPreviewDiagnostics")) {
		t.Fatalf("diagnostic disclosure was not rendered: %v", tester.Texts())
	}
	if tester.HasText("/tmp/source.json") || tester.HasText("/tmp/destination.json") {
		t.Fatalf("file paths must stay out of the normal preview: %v", tester.Texts())
	}

	if err := tester.Click(text("channelPreviewDiagnostics")); err != nil {
		t.Fatal(err)
	}
	if !state.diagnosticsOpen {
		t.Fatal("diagnostic disclosure did not stay open")
	}
	if !tester.HasText("/tmp/source.json") || !tester.HasText("/tmp/destination.json") {
		t.Fatalf("expanded diagnostics omitted file versions: %v", tester.Texts())
	}
}
