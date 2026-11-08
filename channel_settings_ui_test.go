package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

type channelSettingsFakeAPI struct {
	status             ChannelConfigStatus
	statusErr          error
	credentialStatuses []ChannelCredentialStatus
	credentialErr      error
	setAPIKeyErr       error
	setAPIKeyCalls     []string
	deleted            []string
	cleared            []string
	deletedErr         error
	clearedErr         error
	enabledCalls       []string
	testResults        map[string]ChannelTestResult
	testErr            error
	fetchResults       map[string]ChannelModelsResult
	fetchErr           error
	customPrepares     []ChannelCustomRequest
	customApplied      []ChannelCustomRequest
	customErr          error
}

func (f *channelSettingsFakeAPI) ConfigStatus() (ChannelConfigStatus, error) {
	return f.status, f.statusErr
}

func (f *channelSettingsFakeAPI) CredentialStatus(providerID string) (ChannelCredentialStatus, error) {
	if f.credentialErr != nil {
		return ChannelCredentialStatus{}, f.credentialErr
	}
	if len(f.credentialStatuses) == 0 {
		return ChannelCredentialStatus{Status: "not_ready", Reason: "credentials_not_configured"}, nil
	}
	status := f.credentialStatuses[0]
	f.credentialStatuses = f.credentialStatuses[1:]
	return status, nil
}

func (f *channelSettingsFakeAPI) SetAPIKey(providerID, apiKey string) error {
	f.setAPIKeyCalls = append(f.setAPIKeyCalls, providerID)
	return f.setAPIKeyErr
}

func (f *channelSettingsFakeAPI) ClearAPIKey(providerID string) error {
	f.cleared = append(f.cleared, providerID)
	return f.clearedErr
}

func (f *channelSettingsFakeAPI) TestConnection(providerID string) (ChannelTestResult, error) {
	if f.testErr != nil {
		return ChannelTestResult{}, f.testErr
	}
	if result, ok := f.testResults[providerID]; ok {
		return result, nil
	}
	return ChannelTestResult{OK: true, Category: "ok"}, nil
}

func (f *channelSettingsFakeAPI) FetchModels(providerID string) (ChannelModelsResult, error) {
	if f.fetchErr != nil {
		return ChannelModelsResult{}, f.fetchErr
	}
	if result, ok := f.fetchResults[providerID]; ok {
		return result, nil
	}
	return ChannelModelsResult{Models: []string{"model-a", "model-b"}, Category: "ok"}, nil
}

func (f *channelSettingsFakeAPI) SetModelEnabled(providerID, modelID string, enabled bool) ([]string, error) {
	f.enabledCalls = append(f.enabledCalls, providerID+"/"+modelID)
	if enabled {
		return []string{providerID + "/" + modelID}, nil
	}
	return []string{}, nil
}

func (f *channelSettingsFakeAPI) SetChannelEnabled(providerID string, enabled bool) ([]string, error) {
	f.enabledCalls = append(f.enabledCalls, providerID)
	return nil, nil
}

func (f *channelSettingsFakeAPI) DeleteChannel(providerID string) (bool, error) {
	f.deleted = append(f.deleted, providerID)
	if f.deletedErr != nil {
		return false, f.deletedErr
	}
	return true, nil
}

func (f *channelSettingsFakeAPI) PrepareCustom(req ChannelCustomRequest) (ChannelCustomPlan, error) {
	f.customPrepares = append(f.customPrepares, req)
	if f.customErr != nil {
		return ChannelCustomPlan{}, f.customErr
	}
	return ChannelCustomPlan{Request: req, Preview: ChannelMergePreview{
		ProviderID: req.ProviderID,
	}}, nil
}

func (f *channelSettingsFakeAPI) ApplyCustom(req ChannelCustomRequest) (ChannelMergeResult, error) {
	f.customApplied = append(f.customApplied, req)
	if f.customErr != nil {
		return ChannelMergeResult{}, f.customErr
	}
	return ChannelMergeResult{Changed: true}, nil
}

func channelSettingsTestState(
	t *testing.T,
	api channelSettingsAPI,
) (*appState, *channelSettingsUIState) {
	t.Helper()
	app := layoutTestState(t)
	state := installChannelSettingsTestDependencies(app, api, channelSettingsRefreshBridge{})
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

func TestChannelSettingsContentWidthBreakpoints(t *testing.T) {
	if got, want := channelSettingsContentWidth(1180), float32(1040); got != want {
		t.Fatalf("content width at 1180px = %v, want %v", got, want)
	}
	if got, want := channelSettingsContentWidth(900), float32(860); got != want {
		t.Fatalf("content width at 900px = %v, want 900-2×20 = %v", got, want)
	}
	if got, want := channelSettingsContentWidth(0), channelSettingsMaxWidth; got != want {
		t.Fatalf("content width fallback = %v, want %v", got, want)
	}
	if got, want := channelSettingsContentWidth(600), float32(560); got != want {
		t.Fatalf("content width at 600px = %v, want %v", got, want)
	}
}

func TestChannelSettingsListBreakpointLayout(t *testing.T) {
	api := &channelSettingsFakeAPI{status: ChannelConfigStatus{
		ModelsPath:   "/tmp/pi/models.json",
		ModelsExists: true,
		Providers: []ChannelStatusProvider{{
			ID:                   "anthropic",
			Name:                 "Anthropic",
			ModelCount:           2,
			CredentialConfigured: true,
			BuiltIn:              true,
		}},
	}}
	app, _ := channelSettingsTestState(t, api)
	for _, width := range []int{1180, 900} {
		tester := renderChannelSettings(t, app, width, 800)
		if _, ok := tester.Find(text("channelsSectionTitle")); !ok {
			t.Fatalf("section title missing at %dpx: %v", width, tester.Texts())
		}
		switchLabel := textArgs("channelsEnableSwitchAria", map[string]any{"name": "Anthropic"})
		rect, ok := tester.Find(switchLabel)
		if !ok {
			t.Fatalf("enable switch missing at %dpx", width)
		}
		if rect.W != 36 || rect.H != 20 {
			t.Fatalf("switch rect at %dpx = %vx%v, want 36x20", width, rect.W, rect.H)
		}
		if _, ok := tester.Find(textArgs("channelsRowDescription", map[string]any{
			"name": "Anthropic", "count": 2,
		})); !ok {
			t.Fatalf("row description missing at %dpx: %v", width, tester.Texts())
		}
	}
}

func TestChannelSettingsSwitchSizeAndStatusLine(t *testing.T) {
	api := &channelSettingsFakeAPI{status: ChannelConfigStatus{
		ModelsExists: true,
		Providers: []ChannelStatusProvider{
			{ID: "anthropic", CredentialConfigured: true, BuiltIn: true},
			{ID: "openai", CredentialConfigured: false, BuiltIn: true},
			{ID: "acme", CredentialConfigured: true},
		},
	}}
	app, _ := channelSettingsTestState(t, api)
	tester := renderChannelSettings(t, app, 1180, 800)
	expected := textArgs("channelsPiStatusLine", map[string]any{
		"channels": 3, "ready": 2, "total": 3,
	})
	if _, ok := tester.Find(expected); !ok {
		t.Fatalf("status line %q not rendered: %v", expected, tester.Texts())
	}
	rect, ok := tester.Find(textArgs("channelsEnableSwitchAria", map[string]any{"name": "acme"}))
	if !ok {
		t.Fatal("BYOP-created channel switch missing")
	}
	if rect.W != 36 || rect.H != 20 {
		t.Fatalf("switch rect = %vx%v, want 36x20", rect.W, rect.H)
	}
}

func TestChannelSettingsEmptyLoadingAndErrorStates(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		api := &channelSettingsFakeAPI{status: ChannelConfigStatus{
			ModelsPath: "/tmp/pi/models.json", ModelsExists: false,
		}}
		app, _ := channelSettingsTestState(t, api)
		tester := renderChannelSettings(t, app, 960, 700)
		if _, ok := tester.Find(text("channelsStatusLineMissing")); !ok {
			t.Fatalf("missing models.json status line not rendered: %v", tester.Texts())
		}
		if _, ok := tester.Find(text("channelsEmpty")); !ok {
			t.Fatalf("empty state not rendered: %v", tester.Texts())
		}
		if _, ok := tester.Find(text("channelsAdd")); !ok {
			t.Fatal("add button missing in empty state")
		}
	})
	t.Run("loading", func(t *testing.T) {
		api := &channelSettingsFakeAPI{}
		app, state := channelSettingsTestState(t, api)
		state.statusLoaded = false
		state.statusLoading = true
		tester := renderChannelSettings(t, app, 960, 700)
		if _, ok := tester.Find(text("channelsLoading")); !ok {
			t.Fatalf("loading state not rendered: %v", tester.Texts())
		}
	})
	t.Run("read error", func(t *testing.T) {
		api := &channelSettingsFakeAPI{statusErr: errors.New("permission denied")}
		app, _ := channelSettingsTestState(t, api)
		tester := renderChannelSettings(t, app, 960, 700)
		if _, ok := tester.Find(textArgs("channelsStatusReadFailed", map[string]any{
			"detail": "permission denied",
		})); !ok {
			t.Fatalf("error state not rendered: %v", tester.Texts())
		}
	})
}

func TestChannelSettingsPromaVerbatimCopy(t *testing.T) {
	api := &channelSettingsFakeAPI{status: ChannelConfigStatus{ModelsExists: false}}
	app, _ := channelSettingsTestState(t, api)
	tester := renderChannelSettings(t, app, 960, 700)
	// The Chinese copy must match the Proma 0.19.58 spec verbatim.
	copies := map[string]string{
		"channelsSectionTitle":       "模型配置",
		"channelsSectionDescription": "管理 AI 供应商连接，配置 API Key 和可用模型。",
		"channelsAdd":                "添加配置",
		"channelsEmpty":              "还没有配置任何模型，点击上方“添加配置”开始",
	}
	for key, want := range copies {
		if got := textIn("zh-Hans", key, nil); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	if _, ok := tester.Find(text("channelsSectionTitle")); !ok {
		t.Fatalf("list header not rendered: %v", tester.Texts())
	}
}

func TestChannelSettingsDeleteConfirmDialog(t *testing.T) {
	api := &channelSettingsFakeAPI{status: ChannelConfigStatus{
		ModelsExists: true,
		Providers: []ChannelStatusProvider{{
			ID: "acme", Name: "Acme Relay", BuiltIn: false,
		}},
	}}
	app, state := channelSettingsTestState(t, api)
	tester := renderChannelSettings(t, app, 960, 700)
	if err := tester.Click(text("channelsDelete")); err != nil {
		t.Fatal(err)
	}
	if state.dialogDelete != "acme" {
		t.Fatalf("dialogDelete = %q, want acme", state.dialogDelete)
	}
	if _, ok := tester.Find(text("channelsDeleteConfirmTitle")); !ok {
		t.Fatalf("delete dialog title missing: %v", tester.Texts())
	}
	want := textArgs("channelsDeleteConfirmBody", map[string]any{"name": "Acme Relay"})
	if !strings.Contains(strings.Join(tester.Texts(), "\n"), want) {
		t.Fatalf("delete dialog body %q missing: %v", want, tester.Texts())
	}
	if got := textIn("zh-Hans", "channelsDeleteConfirmBody", map[string]any{
		"name": "Acme Relay",
	}); got != "确定删除渠道「Acme Relay」？此操作不可恢复。" {
		t.Fatalf("delete copy = %q, want the Proma verbatim line", got)
	}
	if err := tester.Click(text("channelsCancel")); err != nil {
		t.Fatal(err)
	}
	if state.dialogDelete != "" {
		t.Fatal("cancel did not clear the delete dialog")
	}
	if len(api.deleted) != 0 {
		t.Fatal("cancel must not delete the channel")
	}
	if err := tester.Click(text("channelsDelete")); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click(text("channelsConfirmDelete")); err != nil {
		t.Fatal(err)
	}
	if len(api.deleted) != 1 || api.deleted[0] != "acme" {
		t.Fatalf("deleted = %v, want [acme]", api.deleted)
	}
}

func TestChannelSettingsBuiltInProviderUsesRemoveCredential(t *testing.T) {
	api := &channelSettingsFakeAPI{status: ChannelConfigStatus{
		ModelsExists: true,
		Providers: []ChannelStatusProvider{{
			ID: "anthropic", Name: "Anthropic", BuiltIn: true,
		}},
	}}
	app, state := channelSettingsTestState(t, api)
	tester := renderChannelSettings(t, app, 960, 700)
	if _, ok := tester.Find(text("channelsRemoveCredential")); !ok {
		t.Fatalf("built-in provider must offer remove-credential: %v", tester.Texts())
	}
	if err := tester.Click(text("channelsRemoveCredential")); err != nil {
		t.Fatal(err)
	}
	if state.dialogRemove != "anthropic" {
		t.Fatalf("dialogRemove = %q, want anthropic", state.dialogRemove)
	}
	if err := tester.Click(text("channelsConfirmRemove")); err != nil {
		t.Fatal(err)
	}
	if len(api.cleared) != 1 || api.cleared[0] != "anthropic" {
		t.Fatalf("cleared = %v, want [anthropic]", api.cleared)
	}
	if len(api.deleted) != 0 {
		t.Fatal("built-in providers must never be deleted")
	}
}

func TestChannelSettingsRiskDialogCopyAndAcknowledgement(t *testing.T) {
	api := &channelSettingsFakeAPI{}
	app, state := channelSettingsTestState(t, api)
	state.openCreate()
	state.form.providerID = "openai"
	state.form.baseURL = "https://relay.example.com/v1"
	tester := renderChannelSettings(t, app, 960, 900)
	state.runTest(app)
	if state.dialogRisk != "test" {
		t.Fatalf("risk dialog not armed for test: %q", state.dialogRisk)
	}
	tester = renderChannelSettings(t, app, 960, 900)
	if _, ok := tester.Find(text("channelsRiskTitle")); !ok {
		t.Fatalf("risk dialog not rendered: %v", tester.Texts())
	}
	if got := textIn("zh-Hans", "channelsRiskTitle", nil); got != "确认使用第三方中转站？" {
		t.Fatalf("risk title = %q", got)
	}
	if got := textIn("zh-Hans", "channelsRiskAccept", nil); got != "知晓并愿意承担风险" {
		t.Fatalf("risk accept = %q", got)
	}
	if _, ok := tester.Find(text("channelsRiskAccept")); !ok {
		t.Fatalf("risk accept button missing: %v", tester.Texts())
	}
	if err := tester.Click(text("channelsRiskAccept")); err != nil {
		t.Fatal(err)
	}
	if state.form.acknowledged != "https://relay.example.com/v1" {
		t.Fatalf("acknowledged = %q", state.form.acknowledged)
	}
	if result, ok := state.testResults["openai"]; !ok || !result.OK {
		t.Fatalf("acknowledged test did not run: %#v", state.testResults)
	}
}

func TestChannelSettingsEditMaskNeverLeaksFullKey(t *testing.T) {
	api := &channelSettingsFakeAPI{status: ChannelConfigStatus{
		ModelsExists: true,
		Providers: []ChannelStatusProvider{{
			ID:           "acme",
			Name:         "Acme",
			APIKeyMasked: "sk-…3f9a",
			BuiltIn:      false,
		}},
	}}
	app, state := channelSettingsTestState(t, api)
	state.openEdit(ChannelStatusProvider{ID: "acme", Name: "Acme", BuiltIn: false})
	tester := renderChannelSettings(t, app, 960, 900)
	want := textArgs("channelsKeyMasked", map[string]any{"mask": "sk-…3f9a"})
	if _, ok := tester.Find(want); !ok {
		t.Fatalf("masked key line missing: %v", tester.Texts())
	}
	fullKey := "sk-test-1111111111118c2d0000000003f9a"
	for _, rendered := range tester.Texts() {
		if strings.Contains(rendered, fullKey) {
			t.Fatal("full API key leaked into the rendered UI")
		}
	}
	if state.editKeyMask() != "sk-…3f9a" {
		t.Fatalf("edit mask = %q", state.editKeyMask())
	}
}

func TestChannelSettingsFormBreakpointsAndCards(t *testing.T) {
	api := &channelSettingsFakeAPI{}
	app, state := channelSettingsTestState(t, api)
	state.openCreate()
	for _, width := range []int{1180, 900} {
		tester := renderChannelSettings(t, app, width, 900)
		if _, ok := tester.Find(text("channelsCreateTitle")); !ok {
			t.Fatalf("create title missing at %dpx: %v", width, tester.Texts())
		}
		if _, ok := tester.Find(text("channelsBasicInfoTitle")); !ok {
			t.Fatalf("basic info card missing at %dpx", width)
		}
		if _, ok := tester.Find(text("channelsEnabledModelsTitle")); !ok {
			t.Fatalf("enabled models card missing at %dpx", width)
		}
		if _, ok := tester.Find(text("channelsAvailableModelsTitle")); !ok {
			t.Fatalf("available models card missing at %dpx", width)
		}
		if _, ok := tester.Find(text("channelsProviderName")); !ok {
			t.Fatalf("provider name field missing at %dpx", width)
		}
	}
}

func TestChannelSettingsSearchBoxAppearsAboveFiveModels(t *testing.T) {
	api := &channelSettingsFakeAPI{}
	app, state := channelSettingsTestState(t, api)
	state.openCreate()
	state.form.providerID = "openai"
	state.form.availableModels = []string{"m1", "m2", "m3", "m4", "m5"}
	tester := renderChannelSettings(t, app, 960, 900)
	if _, ok := tester.Find(text("channelsSearchPlaceholder")); ok {
		t.Fatal("search box must be hidden at exactly five unenabled models")
	}
	state.form.availableModels = append(state.form.availableModels, "m6")
	tester = renderChannelSettings(t, app, 960, 900)
	if _, ok := tester.Find(text("channelsSearchPlaceholder")); !ok {
		t.Fatal("search box must appear with more than five unenabled models")
	}
	// A search with no matches shows the empty-search line.
	state.form.search = "zzz"
	tester = renderChannelSettings(t, app, 960, 900)
	if _, ok := tester.Find(text("channelsSearchEmpty")); !ok {
		t.Fatal("search-empty state missing")
	}
}

func TestChannelSettingsCreateSubmitsThroughCustomMergeAndKey(t *testing.T) {
	api := &channelSettingsFakeAPI{}
	app, state := channelSettingsTestState(t, api)
	state.openCreate()
	state.form.providerID = "openai"
	state.form.name = "My OpenAI"
	state.form.baseURL = "https://api.openai.com/v1"
	state.form.apiKey = "sk-test-1111111111118c2d"
	state.submitCreate(app)
	if state.dialogRisk != "create" {
		t.Fatalf("unacknowledged base URL must arm the risk dialog: %q", state.dialogRisk)
	}
	state.acknowledgeAndRun(app, "create")
	if len(api.customApplied) != 1 {
		t.Fatalf("custom apply calls = %d, want 1", len(api.customApplied))
	}
	applied := api.customApplied[0]
	if applied.ProviderID != "openai" || applied.Name != "My OpenAI" ||
		applied.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("applied request = %#v", applied)
	}
	if len(api.setAPIKeyCalls) != 1 || api.setAPIKeyCalls[0] != "openai" {
		t.Fatalf("SetAPIKey calls = %v", api.setAPIKeyCalls)
	}
	if state.view != channelSettingsViewList {
		t.Fatal("create must return to the list view")
	}
}

func TestChannelSettingsDiscardDialogOnDirtyLeave(t *testing.T) {
	api := &channelSettingsFakeAPI{}
	app, state := channelSettingsTestState(t, api)
	state.openCreate()
	state.form.providerID = "openai"
	state.form.name = "My OpenAI"
	tester := renderChannelSettings(t, app, 960, 900)
	state.requestLeaveForm(app)
	if !state.dialogDiscard {
		t.Fatal("dirty create form must arm the discard dialog")
	}
	tester = renderChannelSettings(t, app, 960, 900)
	if _, ok := tester.Find(text("channelsDiscardTitle")); !ok {
		t.Fatalf("discard dialog not rendered: %v", tester.Texts())
	}
	if got := textIn("zh-Hans", "channelsDiscardBodyNoModels", nil); got != "当前尚未配置模型，建议先配置模型再保存。" {
		t.Fatalf("discard copy = %q", got)
	}
	if err := tester.Click(text("channelsDiscard")); err != nil {
		t.Fatal(err)
	}
	if state.view != channelSettingsViewList {
		t.Fatal("discard must return to the list")
	}
}

func TestChannelSettingsLoginPollSteps(t *testing.T) {
	api := &channelSettingsFakeAPI{}
	_, state := channelSettingsTestState(t, api)
	state.openCreate()
	state.form.providerID = "github-copilot"
	api.credentialStatuses = []ChannelCredentialStatus{
		{Status: "not_ready", Reason: "credentials_not_configured"},
		{Status: "ready", AuthType: "oauth"},
	}
	done, err := state.pollLoginStep("github-copilot")
	if err != nil || done {
		t.Fatalf("first poll = (%v, %v), want (false, nil)", done, err)
	}
	if state.loginWatchTimedOut {
		t.Fatal("timeout flag must stay false while polling")
	}
	done, err = state.pollLoginStep("github-copilot")
	if err != nil || !done {
		t.Fatalf("second poll = (%v, %v), want (true, nil)", done, err)
	}
	if cred := state.credentials["github-copilot"]; cred.Status != "ready" || cred.AuthType != "oauth" {
		t.Fatalf("credential status = %#v", cred)
	}
}

// ---------------------------------------------------------------------------
// data-layer behavior behind the UI

func TestSetAPIKeyWritesLiteralAndChmod600(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	service, err := NewChannelConfigService(dir)
	if err != nil {
		t.Fatal(err)
	}
	modelsPath := filepath.Join(dir, "models.json")
	if err := os.WriteFile(modelsPath, []byte("{\n  \"$schema\": \""+piModelsSchemaURL+"\",\n  \"providers\": {}\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := service.SetAPIKey("acme", "sk-test-1111111111118c2d0000000003f9a"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(modelsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("models.json mode = %v, want 0600", got)
	}
	status, err := service.ConfigStatus()
	if err != nil {
		t.Fatal(err)
	}
	var found *ChannelStatusProvider
	for i := range status.Providers {
		if status.Providers[i].ID == "acme" {
			found = &status.Providers[i]
		}
	}
	if found == nil {
		t.Fatal("acme provider missing from ConfigStatus")
	}
	if found.APIKeyMasked == "" || strings.Contains(found.APIKeyMasked, "abcdef") {
		t.Fatalf("masked key = %q, want a mask without the middle of the key", found.APIKeyMasked)
	}
	if !found.CredentialConfigured {
		t.Fatal("acme must report a configured credential")
	}
	if err := service.ClearAPIKey("acme"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(modelsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "apiKey") {
		t.Fatal("ClearAPIKey must remove the apiKey field")
	}
}

func TestSetAPIKeyFailureLeavesModelsJSONUnchanged(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	service, err := NewChannelConfigService(dir)
	if err != nil {
		t.Fatal(err)
	}
	modelsPath := filepath.Join(dir, "models.json")
	original := "{\n  \"$schema\": \"" + piModelsSchemaURL + "\",\n  \"providers\": {}\n}\n"
	if err := os.WriteFile(modelsPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := service.SetAPIKey("acme", ""); err == nil {
		t.Fatal("empty key must be refused")
	}
	data, err := os.ReadFile(modelsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Fatal("a failed SetAPIKey must not touch models.json")
	}
	broken := "{not json"
	if err := os.WriteFile(modelsPath, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := service.SetAPIKey("acme", "sk-test"); err == nil {
		t.Fatal("broken models.json must fail the write")
	}
	data, err = os.ReadFile(modelsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != broken {
		t.Fatal("a failed SetAPIKey must not touch a broken models.json")
	}
}

func TestDeleteChannelRefusesBuiltInProviders(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	service, err := NewChannelConfigService(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DeleteChannel("anthropic"); err == nil {
		t.Fatal("built-in providers must refuse deletion")
	}
	modelsPath := filepath.Join(dir, "models.json")
	if err := os.WriteFile(modelsPath, []byte("{\n  \"$schema\": \""+piModelsSchemaURL+"\",\n  \"providers\": {\n    \"acme\": {\"name\": \"Acme\"}\n  }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err := service.DeleteChannel("acme")
	if err != nil || !removed {
		t.Fatalf("DeleteChannel(acme) = %v, %v", removed, err)
	}
	data, err := os.ReadFile(modelsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "acme") {
		t.Fatal("BYOP-created provider must be removed")
	}
}

func TestSetModelEnabledWritesSettingsPatterns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	service, err := NewChannelConfigService(dir)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.SetModelEnabled("openai", "gpt-5", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 1 || updated[0] != "openai/gpt-5" {
		t.Fatalf("updated = %v", updated)
	}
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "openai/gpt-5") {
		t.Fatal("settings.json must contain the enabled pattern")
	}
	updated, err = service.SetModelEnabled("openai", "gpt-5", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 0 {
		t.Fatalf("disable must empty the list: %v", updated)
	}
	if _, err := service.SetChannelEnabled("openai", false); err != nil {
		t.Fatal(err)
	}
}
