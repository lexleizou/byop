package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChannelConfigImportPreservesUnknownAndExistingValues(t *testing.T) {
	agentDir := t.TempDir()
	sourcePath := filepath.Join(t.TempDir(), "source-models.json")
	writeChannelFixture(t, filepath.Join(agentDir, "models.json"), `{
  "$schema": "https://pi.dev/schemas/models.schema.json",
  "rootExtension": {"keep": true},
  "providers": {
    "acme": {
      "name": "Existing Acme",
      "baseUrl": "https://existing.example/v1",
      "api": "openai-completions",
      "apiKey": "$EXISTING_ACME_KEY",
      "headers": {"X-Existing": "$EXISTING_HEADER"},
      "compat": {"supportsStore": false},
      "providerExtension": {"keep": "provider"},
      "models": [
        {
          "id": "shared",
          "name": "Existing shared",
          "modelExtension": {"keep": "model"}
        }
      ],
      "modelOverrides": {
        "builtin-model": {
          "name": "Existing override",
          "overrideExtension": {"keep": true}
        }
      }
    }
  }
}`)
	writeChannelFixture(t, sourcePath, `{
  "providers": {
    "acme": {
      "name": "Imported Acme",
      "baseUrl": "https://existing.example/v1",
      "api": "openai-completions",
      "apiKey": "$IMPORTED_ACME_KEY",
      "headers": {"X-Existing": "$EXISTING_HEADER"},
      "compat": {"supportsStore": false},
      "providerExtension": {"do": "not import"},
      "models": [
        {"id": "shared", "name": "Imported shared"},
        {
          "id": "new-model",
          "name": "New model",
          "headers": {
            "X-Env": "${NEW_MODEL_HEADER}",
            "Authorization": "!print-secret"
          },
          "modelExtension": {"keep": "imported"}
        }
      ],
      "modelOverrides": {
        "builtin-model": {"name": "Imported override"},
        "new-override": {
          "name": "New override",
          "headers": {"X-Override": "$OVERRIDE_HEADER"}
        }
      }
    }
  }
}`)

	service, err := NewChannelConfigService(agentDir)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.PrepareImport(ChannelImportRequest{
		SourcePath:  sourcePath,
		ProviderID:  "acme",
		ModelIDs:    []string{"shared", "new-model"},
		OverrideIDs: []string{"builtin-model", "new-override"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ApplyImport(plan.Request)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatalf("expected merge to change models.json: %#v", result.Preview)
	}
	document := readChannelDocument(t, filepath.Join(agentDir, "models.json"))
	if rootExtension := nestedObject(document, "rootExtension"); rootExtension["keep"] != true {
		t.Fatalf("root unknown field was not preserved: %#v", rootExtension)
	}
	provider := nestedObject(nestedObject(document, "providers"), "acme")
	if provider["name"] != "Existing Acme" || provider["baseUrl"] != "https://existing.example/v1" ||
		provider["apiKey"] != "$EXISTING_ACME_KEY" {
		t.Fatalf("existing provider fields changed: %#v", provider)
	}
	if provider["providerExtension"].(map[string]any)["keep"] != "provider" {
		t.Fatalf("provider unknown field changed: %#v", provider["providerExtension"])
	}
	headers := nestedObject(provider, "headers")
	if headers["X-Existing"] != "$EXISTING_HEADER" {
		t.Fatalf("existing headers changed: %#v", headers)
	}
	if _, exists := headers["X-Imported"]; exists {
		t.Fatalf("existing provider headers were replaced: %#v", headers)
	}
	compat := nestedObject(provider, "compat")
	if compat["supportsStore"] != false {
		t.Fatalf("existing compat changed: %#v", compat)
	}
	models := provider["models"].([]any)
	byID := channelModelsByID(models)
	if byID["shared"].(map[string]any)["name"] != "Existing shared" {
		t.Fatalf("existing model conflict was overwritten: %#v", byID["shared"])
	}
	newModel := byID["new-model"].(map[string]any)
	if newModel["name"] != "New model" ||
		newModel["modelExtension"].(map[string]any)["keep"] != "imported" {
		t.Fatalf("new model was not imported with unknown fields: %#v", newModel)
	}
	for _, notice := range result.Preview.Notices {
		if notice.Code == channelNoticeModelEndpointInherited {
			t.Fatalf("model without its own endpoint reported inheritance: %#v", notice)
		}
	}
	newHeaders := nestedObject(newModel, "headers")
	if newHeaders["X-Env"] != "${NEW_MODEL_HEADER}" {
		t.Fatalf("supported environment header was not copied: %#v", newHeaders)
	}
	if _, exists := newHeaders["Authorization"]; exists {
		t.Fatalf("command header was copied: %#v", newHeaders)
	}
	overrides := nestedObject(provider, "modelOverrides")
	if overrides["builtin-model"].(map[string]any)["name"] != "Existing override" {
		t.Fatalf("existing override was overwritten: %#v", overrides["builtin-model"])
	}
	if overrides["new-override"].(map[string]any)["name"] != "New override" {
		t.Fatalf("new override was not imported: %#v", overrides["new-override"])
	}
	if strings.Contains(string(mustReadFile(t, filepath.Join(agentDir, "models.json"))), "must-not-copy") {
		t.Fatal("unsafe imported header value was written")
	}
}

func TestChannelConfigPreviewAndApplyKeepConflictsWithoutWrites(t *testing.T) {
	agentDir := t.TempDir()
	targetPath := filepath.Join(agentDir, "models.json")
	original := []byte(`{"providers":{"acme":{"baseUrl":"https://same.example/v1","api":"openai-completions","models":[{"id":"existing","name":"Keep me"}]}}}`)
	writeChannelFixtureBytes(t, targetPath, original)
	sourcePath := filepath.Join(t.TempDir(), "models.json")
	writeChannelFixture(t, sourcePath, `{"providers":{"acme":{"baseUrl":"https://same.example/v1","api":"openai-completions","models":[
		{"id":"existing","name":"Replace me"},
		{"id":"added","name":"Add me"}
	]}}}`)
	service, err := NewChannelConfigService(agentDir)
	if err != nil {
		t.Fatal(err)
	}
	req := ChannelImportRequest{SourcePath: sourcePath, ProviderID: "acme", ModelIDs: []string{"existing", "added"}}
	preview, err := service.PreviewImport(req)
	if err != nil {
		t.Fatal(err)
	}
	if preview.ProviderAction != channelMergePreserve ||
		preview.Models[0].Action != channelMergePreserve ||
		preview.Models[1].Action != channelMergeAdd {
		t.Fatalf("unexpected preview: %#v", preview)
	}
	if got := mustReadFile(t, targetPath); !bytes.Equal(got, original) {
		t.Fatal("preview modified models.json")
	}
	plan, err := service.PrepareImport(req)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ApplyImport(plan.Request)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("expected apply to add a model")
	}
	document := readChannelDocument(t, targetPath)
	models := nestedObject(document, "providers")["acme"].(map[string]any)["models"].([]any)
	byID := channelModelsByID(models)
	if byID["existing"].(map[string]any)["name"] != "Keep me" ||
		byID["added"].(map[string]any)["name"] != "Add me" {
		t.Fatalf("unexpected merged models: %#v", models)
	}
}

func TestChannelConfigValidationFailuresLeaveOriginalUntouched(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "malformed", source: `{"providers":`},
		{name: "invalid schema", source: `{"providers":{"acme":{"models":[{"name":"missing id"}]}}}`},
		{name: "invalid known field type", source: `{"providers":{"acme":{"models":[{"id":"safe","cost":[]}]}}}`},
		{name: "missing model cost", source: `{"providers":{"acme":{"models":[{"id":"safe","cost":{"input":1,"output":2,"cacheRead":3}}]}}}`},
		{name: "invalid cost tier", source: `{"providers":{"acme":{"models":[{"id":"safe","cost":{"input":1,"output":2,"cacheRead":3,"cacheWrite":4,"tiers":[{"inputTokensAbove":1000}]}}]}}}`},
		{name: "invalid input limit", source: `{"providers":{"acme":{"models":[{"id":"safe","inputLimits":{"images":{"resize":{"jpegQuality":101}}}}]}}}`},
		{name: "invalid prompt cache", source: `{"providers":{"acme":{"models":[{"id":"safe","promptCache":{"short":0}}]}}}`},
		{name: "invalid thinking sampling", source: `{"providers":{"acme":{"models":[{"id":"safe","samplingParamsByThinkingLevel":{"high":[]}}]}}}`},
		{name: "block comment not supported by Pi", source: `{/* unsupported */"providers":{"acme":{}}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentDir := t.TempDir()
			targetPath := filepath.Join(agentDir, "models.json")
			original := []byte(`{"providers":{"keep":{"models":[{"id":"safe"}]}}}`)
			writeChannelFixtureBytes(t, targetPath, original)
			sourcePath := filepath.Join(t.TempDir(), "models.json")
			writeChannelFixture(t, sourcePath, tt.source)
			service, err := NewChannelConfigService(agentDir)
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.PrepareImport(ChannelImportRequest{SourcePath: sourcePath, ProviderID: "acme"})
			if err == nil {
				t.Fatal("expected validation error")
			}
			if got := mustReadFile(t, targetPath); !bytes.Equal(got, original) {
				t.Fatal("failed import modified original models.json")
			}
		})
	}
}

func TestChannelConfigImportSanitizesCredentialsForNewProvider(t *testing.T) {
	agentDir := t.TempDir()
	sourcePath := filepath.Join(t.TempDir(), "models.json")
	writeChannelFixture(t, sourcePath, `{
  "providers": {
    "acme": {
      "baseUrl": "https://acme.example/v1",
      "api": "openai-completions",
      "apiKey": "!security find-generic-password -w -s acme",
      "oauth": "radius",
      "headers": {
        "X-Key": "$ACME_KEY",
        "X-Literal": "literal-secret",
        "X-Command": "!security find-generic-password -w -s header"
      },
      "unknownProviderField": {"keep": true},
      "models": [{
        "id": "model",
        "apiKey": "literal-secret",
        "headers": {
          "X-Model-Env": "${MODEL_KEY}",
          "X-Model-Command": "!op read secret"
        },
        "unknownModelField": {"keep": true}
      }]
    }
  }
}`)
	service, err := NewChannelConfigService(agentDir)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.PreviewImport(ChannelImportRequest{
		SourcePath: sourcePath,
		ProviderID: "acme",
		ModelIDs:   []string{"model"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if preview.ProviderAction != channelMergeAdd || len(preview.Notices) < 5 {
		t.Fatalf("unexpected preview notices: %#v", preview)
	}
	if _, err := os.Stat(filepath.Join(agentDir, "models.json")); !os.IsNotExist(err) {
		t.Fatalf("preview created models.json: %v", err)
	}
	plan, err := service.PrepareImport(ChannelImportRequest{
		SourcePath: sourcePath,
		ProviderID: "acme",
		ModelIDs:   []string{"model"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyImport(plan.Request); err != nil {
		t.Fatal(err)
	}
	data := mustReadFile(t, filepath.Join(agentDir, "models.json"))
	for _, secret := range []string{"security find-generic-password", "literal-secret", "op read secret"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("unsafe import value was copied: %q", secret)
		}
	}
	document := readChannelDocument(t, filepath.Join(agentDir, "models.json"))
	provider := nestedObject(nestedObject(document, "providers"), "acme")
	if _, exists := provider["apiKey"]; exists {
		t.Fatalf("unsafe provider apiKey was copied: %#v", provider)
	}
	if _, exists := provider["oauth"]; exists {
		t.Fatalf("OAuth configuration was copied: %#v", provider)
	}
	if nestedObject(provider, "headers")["X-Key"] != "$ACME_KEY" {
		t.Fatalf("explicit environment reference was not retained: %#v", provider["headers"])
	}
	model := provider["models"].([]any)[0].(map[string]any)
	if _, exists := model["apiKey"]; exists {
		t.Fatalf("unsafe model apiKey was copied: %#v", model)
	}
	if nestedObject(model, "headers")["X-Model-Env"] != "${MODEL_KEY}" {
		t.Fatalf("model environment reference was not retained: %#v", model)
	}
	if provider["unknownProviderField"].(map[string]any)["keep"] != true ||
		model["unknownModelField"].(map[string]any)["keep"] != true {
		t.Fatal("unknown fields were not retained for a new provider")
	}
}

func TestChannelConfigInventoryIsCredentialBlindAndCustomUsesEnvReferences(t *testing.T) {
	agentDir := t.TempDir()
	service, err := NewChannelConfigService(agentDir)
	if err != nil {
		t.Fatal(err)
	}
	authHeader := true
	custom := ChannelCustomRequest{
		ProviderID: "local",
		Name:       "Local",
		BaseURL:    "http://127.0.0.1:8080/v1",
		API:        "openai-completions",
		APIKeyEnv:  "$LOCAL_API_KEY",
		AuthHeader: &authHeader,
		HeadersEnv: map[string]string{"X-Org": "${LOCAL_ORG}"},
		Models:     []ChannelCustomModel{{ID: "local-model"}},
	}
	preview, err := service.PreviewCustom(custom)
	if err != nil {
		t.Fatal(err)
	}
	if preview.ProviderAction != channelMergeAdd {
		t.Fatalf("unexpected preview: %#v", preview)
	}
	if _, err := os.Stat(filepath.Join(agentDir, "models.json")); !os.IsNotExist(err) {
		t.Fatalf("preview created models.json: %v", err)
	}
	plan, err := service.PrepareCustom(custom)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ApplyCustom(plan.Request)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("expected custom channel to be written")
	}
	inventory, err := service.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Providers) != 1 ||
		inventory.Providers[0].Credential.Kind != channelCredentialEnvironment ||
		!inventory.Providers[0].Credential.Configured {
		t.Fatalf("unexpected inventory: %#v", inventory)
	}
	encoded, err := json.Marshal(inventory)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "LOCAL_API_KEY") || strings.Contains(string(encoded), "LOCAL_ORG") {
		t.Fatalf("inventory leaked environment reference: %s", encoded)
	}
	document := readChannelDocument(t, filepath.Join(agentDir, "models.json"))
	provider := nestedObject(nestedObject(document, "providers"), "local")
	if provider["apiKey"] != "$LOCAL_API_KEY" || provider["authHeader"] != true ||
		nestedObject(provider, "headers")["X-Org"] != "${LOCAL_ORG}" {
		t.Fatalf("custom environment references not written as Pi references: %#v", provider)
	}
	if _, exists := provider["models"].([]any)[0].(map[string]any)["authHeader"]; exists {
		t.Fatalf("authHeader was written at model level: %#v", provider)
	}
}

func TestChannelConfigDefaultPiAgentDirUsesPiOverride(t *testing.T) {
	piDir := t.TempDir()
	byopDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", piDir)
	t.Setenv("BYOP_CONFIG_DIR", byopDir)
	got, err := DefaultPiAgentDir()
	if err != nil {
		t.Fatal(err)
	}
	want := canonicalPiPath(piDir)
	if got != want {
		t.Fatalf("DefaultPiAgentDir() = %q, want %q", got, want)
	}
}

func TestChannelConfigPiPathsExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PI_CODING_AGENT_DIR", "~/pi-test")

	got, err := DefaultPiAgentDir()
	if err != nil {
		t.Fatal(err)
	}
	want := canonicalPiPath(filepath.Join(home, "pi-test"))
	if got != want {
		t.Fatalf("DefaultPiAgentDir() = %q, want %q", got, want)
	}

	service, err := NewChannelConfigService("~/explicit-test")
	if err != nil {
		t.Fatal(err)
	}
	wantConfig := filepath.Join(canonicalPiPath(filepath.Join(home, "explicit-test")), "models.json")
	if service.ConfigPath() != wantConfig {
		t.Fatalf("ConfigPath() = %q, want %q", service.ConfigPath(), wantConfig)
	}
}

func TestChannelConfigEnvironmentReferencesAreASCIIAndExplicit(t *testing.T) {
	service, err := NewChannelConfigService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, reference := range []string{"$变量", "${变量}", "LOCAL_API_KEY"} {
		_, err := service.PreviewCustom(ChannelCustomRequest{
			ProviderID: "local",
			APIKeyEnv:  reference,
		})
		if err == nil {
			t.Fatalf("expected %q to be rejected", reference)
		}
	}
}

func TestChannelConfigAcceptsOverrideOnlyProvider(t *testing.T) {
	agentDir := t.TempDir()
	service, err := NewChannelConfigService(agentDir)
	if err != nil {
		t.Fatal(err)
	}
	custom := ChannelCustomRequest{
		ProviderID: "builtin",
		ModelOverride: []ChannelCustomModelOverride{{
			ID:   "builtin-model",
			Name: "Override only",
		}},
	}
	plan, err := service.PrepareCustom(custom)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ApplyCustom(plan.Request)
	if err != nil {
		t.Fatal(err)
	}
	document := readChannelDocument(t, filepath.Join(agentDir, "models.json"))
	provider := nestedObject(nestedObject(document, "providers"), "builtin")
	if nestedObject(provider, "modelOverrides")["builtin-model"] == nil {
		t.Fatalf("override-only provider was not accepted: %#v", provider)
	}
}

func TestChannelConfigAcceptsOverridePartialCostAndUnknownFields(t *testing.T) {
	agentDir := t.TempDir()
	service, err := NewChannelConfigService(agentDir)
	if err != nil {
		t.Fatal(err)
	}
	custom := ChannelCustomRequest{
		ProviderID: "builtin",
		ModelOverride: []ChannelCustomModelOverride{{
			ID:   "builtin-model",
			Name: "Override only",
		}},
	}
	plan, err := service.PrepareCustom(custom)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ApplyCustom(plan.Request)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(agentDir, "models.json")
	document := readChannelDocument(t, path)
	provider := nestedObject(nestedObject(document, "providers"), "builtin")
	override := nestedObject(provider, "modelOverrides")["builtin-model"].(map[string]any)
	override["cost"] = map[string]any{"input": json.Number("1.5")}
	override["futureUnknown"] = map[string]any{"keep": true}
	if err := writePiModelsAtomic(path, document); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Inventory(); err != nil {
		t.Fatal(err)
	}
}

func TestChannelConfigSameProviderRejectsRoutingConflict(t *testing.T) {
	agentDir := t.TempDir()
	targetPath := filepath.Join(agentDir, "models.json")
	writeChannelFixture(t, targetPath, `{
  "providers": {
    "acme": {
      "baseUrl": "https://destination.example/v1",
      "api": "openai-completions",
      "apiKey": "$DESTINATION_KEY",
      "headers": {"X-Route": "$DESTINATION_ROUTE"},
      "compat": {"supportsStore": false},
      "models": [{"id": "existing"}]
    }
  }
}`)
	sourcePath := filepath.Join(t.TempDir(), "models.json")
	writeChannelFixture(t, sourcePath, `{
  "providers": {
    "acme": {
      "baseUrl": "https://source.example/v1",
      "api": "openai-completions",
      "apiKey": "$SOURCE_KEY",
      "headers": {"X-Route": "$SOURCE_ROUTE"},
      "compat": {"supportsStore": true},
      "models": [{"id": "new-model", "name": "New"}]
    }
  }
}`)
	service, err := NewChannelConfigService(agentDir)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.PreviewImport(ChannelImportRequest{
		SourcePath: sourcePath, ProviderID: "acme", ModelIDs: []string{"new-model"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Models) != 1 || preview.Models[0].Action != channelMergePreserve ||
		len(preview.Conflicts) != 1 || preview.Conflicts[0].Code != channelNoticeProviderEndpointConflict {
		t.Fatalf("routing conflict was not preserved: %#v", preview)
	}
	if len(preview.Conflicts[0].Fields) == 0 {
		t.Fatal("routing conflict did not identify fields")
	}
	plan, err := service.PrepareImport(ChannelImportRequest{
		SourcePath: sourcePath, ProviderID: "acme", ModelIDs: []string{"new-model"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ApplyImport(plan.Request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Fatal("routing conflict unexpectedly changed models.json")
	}
	foundDestinationKeyNotice := false
	for _, notice := range result.Preview.Notices {
		if notice.Code == channelNoticeProviderEndpointConflict &&
			notice.Path == modelPath("acme", "new-model") {
			foundDestinationKeyNotice = true
		}
	}
	if !foundDestinationKeyNotice {
		t.Fatalf("conflicting model did not report why destination key was not reused: %#v", result.Preview.Notices)
	}
	provider := nestedObject(nestedObject(readChannelDocument(t, targetPath), "providers"), "acme")
	models := provider["models"].([]any)
	if len(models) != 1 || channelModelsByID(models)["new-model"] != nil {
		t.Fatalf("conflicting model was written: %#v", models)
	}
}

func TestChannelConfigSameProviderInheritsDestinationEndpoint(t *testing.T) {
	agentDir := t.TempDir()
	targetPath := filepath.Join(agentDir, "models.json")
	writeChannelFixture(t, targetPath, `{
  "providers": {
    "acme": {
      "baseUrl": "https://same.example/v1",
      "api": "openai-completions",
      "apiKey": "$DESTINATION_KEY",
      "models": [{"id": "existing"}]
    }
  }
}`)
	sourcePath := filepath.Join(t.TempDir(), "models.json")
	writeChannelFixture(t, sourcePath, `{
  "providers": {
    "acme": {
      "baseUrl": "https://same.example/v1",
      "api": "openai-completions",
      "apiKey": "$SOURCE_KEY",
      "models": [{"id": "new-model", "api": "openai-completions", "baseUrl": "https://same.example/v1"}]
    }
  }
}`)
	service, err := NewChannelConfigService(agentDir)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.PrepareImport(ChannelImportRequest{
		SourcePath: sourcePath, ProviderID: "acme", ModelIDs: []string{"new-model"},
	})
	if err != nil {
		t.Fatal(err)
	}
	inheritedNotices := 0
	for _, notice := range plan.Preview.Notices {
		if notice.Code != channelNoticeModelEndpointInherited {
			continue
		}
		inheritedNotices++
		if notice.Path != modelPath("acme", "new-model") || notice.Action != "inherit" {
			t.Fatalf("unexpected endpoint inheritance notice: %#v", notice)
		}
	}
	if inheritedNotices != 1 {
		t.Fatalf("endpoint inheritance notices = %d, want 1: %#v", inheritedNotices, plan.Preview.Notices)
	}
	if _, err := service.ApplyImport(plan.Request); err != nil {
		t.Fatal(err)
	}
	provider := nestedObject(nestedObject(readChannelDocument(t, targetPath), "providers"), "acme")
	model := channelModelsByID(provider["models"].([]any))["new-model"].(map[string]any)
	if model["api"] != nil || model["baseUrl"] != nil {
		t.Fatalf("new model did not inherit destination endpoint: %#v", model)
	}
	if provider["apiKey"] != "$DESTINATION_KEY" {
		t.Fatalf("destination credential was changed: %#v", provider)
	}
}

func TestChannelConfigDanglingDestinationSymlinkIsNotReplaced(t *testing.T) {
	agentDir := t.TempDir()
	linkPath := filepath.Join(agentDir, "models.json")
	missingTarget := filepath.Join(t.TempDir(), "missing-models.json")
	if err := os.Symlink(missingTarget, linkPath); err != nil {
		t.Fatal(err)
	}
	service, err := NewChannelConfigService(agentDir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.PrepareCustom(ChannelCustomRequest{
		ProviderID: "local",
		BaseURL:    "http://127.0.0.1:8080/v1",
		API:        "openai-completions",
		Models:     []ChannelCustomModel{{ID: "model"}},
	})
	if err == nil || !strings.Contains(err.Error(), "dangling symlink") {
		t.Fatalf("expected preview to reject dangling symlink explicitly, got %v", err)
	}
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("dangling symlink was replaced during apply")
	}
	if target, err := os.Readlink(linkPath); err != nil || target != missingTarget {
		t.Fatalf("symlink target changed: %q, %v", target, err)
	}
}

func TestChannelConfigCustomConflictKeepsNoticesAndEmptySlices(t *testing.T) {
	agentDir := t.TempDir()
	targetPath := filepath.Join(agentDir, "models.json")
	writeChannelFixture(t, targetPath, `{
  "providers": {
    "acme": {
      "baseUrl": "https://destination.example/v1",
      "api": "openai-completions",
      "apiKey": "$DESTINATION_KEY",
      "models": [{"id": "existing"}]
    }
  }
}`)
	service, err := NewChannelConfigService(agentDir)
	if err != nil {
		t.Fatal(err)
	}
	req := ChannelCustomRequest{
		ProviderID: "acme",
		BaseURL:    "https://different.example/v1",
		API:        "openai-completions",
		APIKeyEnv:  "$SOURCE_KEY",
		Models:     []ChannelCustomModel{{ID: "new-model"}},
	}
	preview, err := service.PreviewCustom(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Conflicts) != 1 || preview.Models[0].Action != channelMergePreserve {
		t.Fatalf("custom conflict was not returned as a preserved preview: %#v", preview)
	}
	foundConflictNotice := false
	for _, notice := range preview.Notices {
		if notice.Code == channelNoticeProviderEndpointConflict {
			foundConflictNotice = true
		}
	}
	if !foundConflictNotice {
		t.Fatalf("custom conflict did not preserve notices: %#v", preview.Notices)
	}
	conflictReq := ChannelCustomRequest{
		ProviderID: "acme",
		BaseURL:    "https://destination.example/v1",
		API:        "openai-completions",
		Models:     []ChannelCustomModel{{ID: "existing"}, {ID: "new-model"}},
	}
	preview, err = service.PreviewCustom(conflictReq)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Notices == nil || preview.Models == nil || preview.ModelOverrides == nil ||
		preview.Conflicts == nil {
		t.Fatalf("custom preview returned nil slices: %#v", preview)
	}
	foundPreserved := false
	for _, notice := range preview.Notices {
		if notice.Code == "modelConflictPreserved" {
			foundPreserved = true
		}
	}
	if !foundPreserved {
		t.Fatalf("custom preview cleared merge notices: %#v", preview.Notices)
	}
}

func TestChannelConfigPreviewStaleBeforeWrite(t *testing.T) {
	agentDir := t.TempDir()
	targetPath := filepath.Join(agentDir, "models.json")
	writeChannelFixture(t, targetPath, `{"providers":{}}`)
	sourcePath := filepath.Join(t.TempDir(), "models.json")
	writeChannelFixture(t, sourcePath, `{"providers":{"acme":{"baseUrl":"https://same.example/v1","api":"openai-completions","models":[{"id":"model"}]}}}`)
	service, err := NewChannelConfigService(agentDir)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.PrepareImport(ChannelImportRequest{
		SourcePath: sourcePath, ProviderID: "acme", ModelIDs: []string{"model"},
	})
	if err != nil {
		t.Fatal(err)
	}
	changedTarget := []byte(`{"providers":{"other":{"baseUrl":"https://other.example/v1","api":"openai-completions","models":[{"id":"other"}]}}}`)
	writeChannelFixtureBytes(t, targetPath, changedTarget)
	_, err = service.ApplyImport(plan.Request)
	var stale *ChannelPreviewStaleError
	if !errors.As(err, &stale) || stale.Code != channelErrorPreviewStale {
		t.Fatalf("expected previewStale, got %v", err)
	}
	if got := mustReadFile(t, targetPath); !bytes.Equal(got, changedTarget) {
		t.Fatal("stale apply changed destination")
	}
	writeChannelFixture(t, sourcePath, `{"providers":{"acme":{"baseUrl":"https://same.example/v1","api":"openai-completions","models":[{"id":"other-model"}]}}}`)
	plan, err = service.PrepareImport(ChannelImportRequest{
		SourcePath: sourcePath, ProviderID: "acme", ModelIDs: []string{"other-model"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeChannelFixture(t, sourcePath, `{"providers":{"acme":{"baseUrl":"https://same.example/v1","api":"openai-completions","models":[{"id":"changed-model"}]}}}`)
	_, err = service.ApplyImport(plan.Request)
	if !errors.As(err, &stale) {
		t.Fatalf("expected source previewStale, got %v", err)
	}
}

func TestChannelConfigAtomicWritePreservesSymlinkModeAndHTML(t *testing.T) {
	agentDir := t.TempDir()
	realDir := t.TempDir()
	realPath := filepath.Join(realDir, "actual.json")
	original := []byte("{\"providers\":{},\"label\":\"<keep>\"}\n")
	writeChannelFixtureBytes(t, realPath, original)
	if err := os.Chmod(realPath, 0o640); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(agentDir, "models.json")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Fatal(err)
	}
	service, err := NewChannelConfigService(agentDir)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.PrepareCustom(ChannelCustomRequest{
		ProviderID: "local",
		BaseURL:    "http://127.0.0.1:8080/v1",
		API:        "openai-completions",
		Models:     []ChannelCustomModel{{ID: "local-model"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyCustom(plan.Request); err != nil {
		t.Fatal(err)
	}
	targetInfo, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if targetInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatal("apply replaced symlink instead of its target")
	}
	info, err := os.Stat(realPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("existing mode changed: %v", info.Mode().Perm())
	}
	data := mustReadFile(t, realPath)
	if bytes.Contains(data, []byte(`\u003c`)) || !bytes.Contains(data, []byte(`<keep>`)) {
		t.Fatalf("HTML value was escaped: %s", data)
	}
}

func TestChannelConfigCommentsRemovedIsPreviewed(t *testing.T) {
	agentDir := t.TempDir()
	writeChannelFixture(t, filepath.Join(agentDir, "models.json"), `{
  // keep this user note
  "providers": {}
}`)
	service, err := NewChannelConfigService(agentDir)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.PreviewCustom(ChannelCustomRequest{
		ProviderID: "local",
		BaseURL:    "http://127.0.0.1:8080/v1",
		API:        "openai-completions",
		Models:     []ChannelCustomModel{{ID: "local-model"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, notice := range preview.Notices {
		if notice.Code == channelNoticeCommentsRemoved {
			found = true
		}
	}
	if !found {
		t.Fatalf("comment removal was not reported: %#v", preview.Notices)
	}
}

func TestChannelConfigInventoryRedactsBaseURLQuery(t *testing.T) {
	agentDir := t.TempDir()
	writeChannelFixture(t, filepath.Join(agentDir, "models.json"), `{
  "providers": {
    "local": {
      "baseUrl": "https://local.example/v1?token=very-secret",
      "api": "openai-completions",
      "models": [{"id": "model"}]
    }
  }
}`)
	service, err := NewChannelConfigService(agentDir)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := service.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(inventory.Providers[0].BaseURL, "very-secret") {
		t.Fatalf("inventory leaked baseUrl query: %#v", inventory.Providers[0])
	}
}

func TestChannelConfigInspectReportsReadErrorInsteadOfMissing(t *testing.T) {
	service, err := NewChannelConfigService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.InspectModelsFile(t.TempDir())
	if err == nil {
		t.Fatal("expected a direct read error for a directory")
	}
	if strings.Contains(err.Error(), "changed after preview") {
		t.Fatalf("read error was converted to previewStale: %v", err)
	}
}

func TestChannelConfigBuiltinOverrideDoesNotRequireEndpoint(t *testing.T) {
	service, err := NewChannelConfigService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.PrepareCustom(ChannelCustomRequest{
		ProviderID: "anthropic",
		ModelOverride: []ChannelCustomModelOverride{{
			ID:   "builtin-model",
			Name: "Built-in override",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Preview.ProviderAction != channelMergeAdd {
		t.Fatalf("unexpected preview: %#v", plan.Preview)
	}
}

func TestChannelConfigBuiltinOverridePresetsPreserveRoutingConflict(t *testing.T) {
	agentDir := t.TempDir()
	targetPath := filepath.Join(agentDir, "models.json")
	writeChannelFixture(t, targetPath, `{
  "providers": {
    "anthropic": {"apiKey": "$EXISTING_ANTHROPIC"}
  }
}`)
	service, err := NewChannelConfigService(agentDir)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.PreviewCustom(ChannelCustomRequest{
		ProviderID: "anthropic",
		BaseURL:    "https://proxy.example/v1",
		APIKeyEnv:  "$PROXY_ANTHROPIC",
		ModelOverride: []ChannelCustomModelOverride{{
			ID:   "builtin-model",
			Name: "Preserved override",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Conflicts) != 1 ||
		preview.ModelOverrides[0].Action != channelMergePreserve {
		t.Fatalf("built-in routing conflict was not preserved: %#v", preview)
	}
	plan, err := service.PrepareCustom(ChannelCustomRequest{
		ProviderID: "anthropic",
		BaseURL:    "https://proxy.example/v1",
		APIKeyEnv:  "$PROXY_ANTHROPIC",
		ModelOverride: []ChannelCustomModelOverride{{
			ID:   "builtin-model",
			Name: "Preserved override",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ApplyCustom(plan.Request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Fatal("routing conflict unexpectedly changed models.json")
	}
	provider := nestedObject(nestedObject(readChannelDocument(t, targetPath), "providers"), "anthropic")
	if provider["apiKey"] != "$EXISTING_ANTHROPIC" || provider["baseUrl"] != nil ||
		provider["modelOverrides"] != nil {
		t.Fatalf("routing conflict changed destination provider: %#v", provider)
	}
}

func TestChannelConfigNewCustomProviderRequiresAPIAndBaseURL(t *testing.T) {
	service, err := NewChannelConfigService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		req  ChannelCustomRequest
	}{
		{
			name: "missing api",
			req: ChannelCustomRequest{
				ProviderID: "local",
				BaseURL:    "https://local.example/v1",
				Models:     []ChannelCustomModel{{ID: "model"}},
			},
		},
		{
			name: "missing baseUrl",
			req: ChannelCustomRequest{
				ProviderID: "local",
				API:        "openai-completions",
				Models:     []ChannelCustomModel{{ID: "model"}},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := service.PreviewCustom(tt.req); err == nil {
				t.Fatal("expected incomplete custom provider to be rejected")
			}
		})
	}

	req := ChannelCustomRequest{
		ProviderID: "local",
		Models: []ChannelCustomModel{{
			ID:      "model",
			API:     "anthropic-messages",
			BaseURL: "https://local.example/v1",
		}},
	}
	if _, err := service.PreviewCustom(req); err != nil {
		t.Fatalf("model-level endpoint should be accepted: %v", err)
	}
}

func writeChannelFixture(t *testing.T, path, content string) {
	t.Helper()
	writeChannelFixtureBytes(t, path, []byte(content))
}

func writeChannelFixtureBytes(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readChannelDocument(t *testing.T, path string) map[string]any {
	t.Helper()
	document, err := readModelsDocument(path, false)
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func nestedObject(parent map[string]any, key string) map[string]any {
	object, _ := parent[key].(map[string]any)
	return object
}

func channelModelsByID(models []any) map[string]any {
	result := make(map[string]any)
	for _, raw := range models {
		model, _ := raw.(map[string]any)
		if id, ok := model["id"].(string); ok {
			result[id] = model
		}
	}
	return result
}
