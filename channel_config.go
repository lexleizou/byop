package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
)

const piModelsSchemaURL = "https://pi.dev/schemas/models.schema.json"

const (
	channelCredentialNone        = "none"
	channelCredentialEnvironment = "environment"
	channelCredentialCommand     = "command"
	channelCredentialLiteral     = "literal"
	channelCredentialOAuth       = "oauth"

	channelMergeAdd      = "add"
	channelMergePreserve = "preserve"

	channelNoticeConfigureInPi            = "configureInPi"
	channelNoticeProviderEndpointConflict = "providerEndpointConflict"
	channelNoticeModelEndpointInherited   = "modelEndpointInherited"
	channelNoticeCommentsRemoved          = "commentsRemoved"

	channelErrorPreviewStale = "previewStale"
	channelMissingDigest     = "missing"
)

// ChannelConfigService reads and merges Pi's user-level models.json.
// It never reads auth.json and never persists BYOP preferences or credentials.
type ChannelConfigService struct {
	mu         sync.Mutex
	agentDir   string
	configPath string
}

// ChannelInventory is the credential-blind schema consumed by model settings UI.
type ChannelInventory struct {
	AgentDir   string                   `json:"agentDir"`
	ConfigPath string                   `json:"configPath"`
	Exists     bool                     `json:"exists"`
	Providers  []ChannelProviderSummary `json:"providers"`
}

// ChannelProviderSummary describes one models.json provider without exposing
// apiKey or header values.
type ChannelProviderSummary struct {
	ID            string                   `json:"id"`
	Name          string                   `json:"name,omitempty"`
	BaseURL       string                   `json:"baseUrl,omitempty"`
	API           string                   `json:"api,omitempty"`
	OAuth         string                   `json:"oauth,omitempty"`
	AuthHeader    bool                     `json:"authHeader,omitempty"`
	Credential    ChannelCredentialSummary `json:"credential"`
	Headers       ChannelHeaderSummary     `json:"headers"`
	Models        []ChannelModelSummary    `json:"models"`
	ModelOverride []ChannelModelSummary    `json:"modelOverrides"`
}

// ChannelPreset is a provider selection owned by Pi. BYOP supplies only
// non-secret display metadata and never creates a parallel credential store.
type ChannelPreset struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	APIKeyEnvironment string `json:"apiKeyEnvironment,omitempty"`
	UsesPiLogin       bool   `json:"usesPiLogin,omitempty"`
}

// ChannelPresets returns the Pi-supported provider IDs used by the settings UI.
func ChannelPresets() []ChannelPreset {
	return []ChannelPreset{
		{ID: "anthropic", Name: "Anthropic", APIKeyEnvironment: "$ANTHROPIC_API_KEY"},
		{ID: "openai", Name: "OpenAI", APIKeyEnvironment: "$OPENAI_API_KEY"},
		{ID: "deepseek", Name: "DeepSeek", APIKeyEnvironment: "$DEEPSEEK_API_KEY"},
		{ID: "google", Name: "Google Gemini", APIKeyEnvironment: "$GEMINI_API_KEY"},
		{ID: "openrouter", Name: "OpenRouter", APIKeyEnvironment: "$OPENROUTER_API_KEY"},
		{ID: "groq", Name: "Groq", APIKeyEnvironment: "$GROQ_API_KEY"},
		{ID: "mistral", Name: "Mistral", APIKeyEnvironment: "$MISTRAL_API_KEY"},
		{ID: "xai", Name: "xAI", APIKeyEnvironment: "$XAI_API_KEY"},
		{ID: "github-copilot", Name: "GitHub Copilot", UsesPiLogin: true},
		{ID: "openai-codex", Name: "OpenAI Codex", UsesPiLogin: true},
	}
}

// ChannelCredentialSummary identifies how credentials are configured without
// returning the credential itself.
type ChannelCredentialSummary struct {
	Configured bool   `json:"configured"`
	Kind       string `json:"kind"`
}

// ChannelHeaderSummary reports header categories without returning their values.
type ChannelHeaderSummary struct {
	Count                 int `json:"count"`
	EnvironmentReferences int `json:"environmentReferences"`
	Unsupported           int `json:"unsupported"`
}

// ChannelModelSummary describes a model entry or model override.
type ChannelModelSummary struct {
	ID            string               `json:"id"`
	Name          string               `json:"name,omitempty"`
	API           string               `json:"api,omitempty"`
	BaseURL       string               `json:"baseUrl,omitempty"`
	Reasoning     *bool                `json:"reasoning,omitempty"`
	Input         []string             `json:"input,omitempty"`
	ContextWindow *float64             `json:"contextWindow,omitempty"`
	MaxTokens     *float64             `json:"maxTokens,omitempty"`
	Headers       ChannelHeaderSummary `json:"headers"`
}

// ChannelImportRequest selects a provider and optional entries from a source
// Pi models.json. Existing destination entries are preserved by default.
type ChannelImportRequest struct {
	SourcePath        string   `json:"sourcePath"`
	ProviderID        string   `json:"providerId"`
	ModelIDs          []string `json:"modelIds,omitempty"`
	OverrideIDs       []string `json:"overrideIds,omitempty"`
	SourceDigest      string   `json:"sourceDigest,omitempty"`
	DestinationDigest string   `json:"destinationDigest,omitempty"`
}

// ChannelCustomRequest creates a provider from typed fields. APIKeyEnv and
// HeadersEnv values must use Pi's explicit $NAME or ${NAME} syntax.
type ChannelCustomRequest struct {
	ProviderID        string                       `json:"providerId"`
	Name              string                       `json:"name,omitempty"`
	BaseURL           string                       `json:"baseUrl,omitempty"`
	API               string                       `json:"api,omitempty"`
	APIKeyEnv         string                       `json:"apiKeyEnv,omitempty"`
	AuthHeader        *bool                        `json:"authHeader,omitempty"`
	HeadersEnv        map[string]string            `json:"headersEnv,omitempty"`
	Models            []ChannelCustomModel         `json:"models,omitempty"`
	ModelOverride     []ChannelCustomModelOverride `json:"modelOverrides,omitempty"`
	DestinationDigest string                       `json:"destinationDigest,omitempty"`
}

// ChannelCustomModel contains the model fields needed by the settings UI.
type ChannelCustomModel struct {
	ID            string            `json:"id"`
	Name          string            `json:"name,omitempty"`
	API           string            `json:"api,omitempty"`
	BaseURL       string            `json:"baseUrl,omitempty"`
	Reasoning     *bool             `json:"reasoning,omitempty"`
	Input         []string          `json:"input,omitempty"`
	ContextWindow *float64          `json:"contextWindow,omitempty"`
	MaxTokens     *float64          `json:"maxTokens,omitempty"`
	HeadersEnv    map[string]string `json:"headersEnv,omitempty"`
}

// ChannelCustomModelOverride contains the model override fields needed by the
// settings UI. The destination key is ID.
type ChannelCustomModelOverride struct {
	ID            string            `json:"id"`
	Name          string            `json:"name,omitempty"`
	Reasoning     *bool             `json:"reasoning,omitempty"`
	Input         []string          `json:"input,omitempty"`
	ContextWindow *float64          `json:"contextWindow,omitempty"`
	MaxTokens     *float64          `json:"maxTokens,omitempty"`
	HeadersEnv    map[string]string `json:"headersEnv,omitempty"`
}

// ChannelMergePreview is safe to pass to UI. It contains no credential values.
type ChannelMergePreview struct {
	ConfigPath     string             `json:"configPath"`
	Source         ChannelFileVersion `json:"source"`
	Destination    ChannelFileVersion `json:"destination"`
	ProviderID     string             `json:"providerId"`
	ProviderAction string             `json:"providerAction"`
	ConflictPolicy string             `json:"conflictPolicy"`
	Conflicts      []ChannelConflict  `json:"conflicts"`
	Models         []ChannelMergeItem `json:"models"`
	ModelOverrides []ChannelMergeItem `json:"modelOverrides"`
	Notices        []ChannelNotice    `json:"notices"`
}

// ChannelFileVersion identifies the exact bytes used to build a preview.
// A missing destination uses the "missing" digest.
type ChannelFileVersion struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Digest string `json:"digest"`
}

// ChannelMergeItem describes one selected model or override.
type ChannelMergeItem struct {
	ID           string `json:"id"`
	Action       string `json:"action"`
	Verification string `json:"verification"`
}

// ChannelNotice explains an omitted credential or preserved conflict.
type ChannelNotice struct {
	Code   string `json:"code"`
	Path   string `json:"path"`
	Action string `json:"action"`
}

// ChannelConflict reports fields where imported and destination routing differ.
// Values are omitted; callers only receive the field paths.
type ChannelConflict struct {
	Code   string   `json:"code"`
	Path   string   `json:"path"`
	Fields []string `json:"fields"`
}

// ChannelMergeResult reports whether an explicit Apply call changed models.json.
type ChannelMergeResult struct {
	Preview ChannelMergePreview `json:"preview"`
	Changed bool                `json:"changed"`
}

// ChannelImportPlan binds a preview to exact source and destination versions.
// UI callers should pass the preview and its bound request unchanged to Apply.
type ChannelImportPlan struct {
	Request ChannelImportRequest `json:"request"`
	Preview ChannelMergePreview  `json:"preview"`
}

// ChannelCustomPlan binds a custom preview to the exact destination version.
type ChannelCustomPlan struct {
	Request ChannelCustomRequest `json:"request"`
	Preview ChannelMergePreview  `json:"preview"`
}

// ChannelPreviewStaleError means source or destination changed after preview.
type ChannelPreviewStaleError struct {
	Code     string
	Path     string
	Expected string
	Actual   string
}

func (e *ChannelPreviewStaleError) Error() string {
	return fmt.Sprintf("%s: %s changed after preview (expected %s, found %s)", e.Code, e.Path, e.Expected, e.Actual)
}

type selectedModelOverride struct {
	id    string
	value map[string]any
}

// DefaultPiAgentDir follows Pi's documented agent directory resolution.
// BYOP_CONFIG_DIR is intentionally not consulted because it belongs to BYOP.
func DefaultPiAgentDir() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_DIR")); configured != "" {
		path, err := expandPiHomePath(configured)
		if err != nil {
			return "", fmt.Errorf("resolve PI_CODING_AGENT_DIR: %w", err)
		}
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return canonicalPiPath(filepath.Join(home, ".pi", "agent")), nil
}

// NewChannelConfigService creates a service for an explicit agent directory.
// Passing an empty agentDir uses DefaultPiAgentDir.
func NewChannelConfigService(agentDir string) (*ChannelConfigService, error) {
	var err error
	if strings.TrimSpace(agentDir) == "" {
		agentDir, err = DefaultPiAgentDir()
		if err != nil {
			return nil, err
		}
	} else {
		agentDir, err = expandPiHomePath(agentDir)
		if err != nil {
			return nil, fmt.Errorf("resolve Pi agent directory: %w", err)
		}
	}
	return &ChannelConfigService{
		agentDir:   agentDir,
		configPath: filepath.Join(agentDir, "models.json"),
	}, nil
}

func (s *ChannelConfigService) ConfigPath() string {
	if s == nil {
		return ""
	}
	return s.configPath
}

// Inventory returns safe summaries for the configured Pi models.json.
func (s *ChannelConfigService) Inventory() (ChannelInventory, error) {
	if s == nil {
		return ChannelInventory{}, errors.New("channel config service is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, inventory, err := readChannelInventory(s.agentDir, s.configPath)
	return inventory, err
}

// InspectModelsFile returns safe summaries from a selected models.json source.
func (s *ChannelConfigService) InspectModelsFile(path string) (ChannelInventory, error) {
	if s == nil {
		return ChannelInventory{}, errors.New("channel config service is nil")
	}
	return InspectChannelModelsFile(path)
}

// InspectModelsFile reads an arbitrary models.json without changing it.
func InspectChannelModelsFile(path string) (ChannelInventory, error) {
	if strings.TrimSpace(path) == "" {
		return ChannelInventory{}, errors.New("models.json path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return ChannelInventory{}, fmt.Errorf("resolve models.json path: %w", err)
	}
	_, inventory, err := readChannelInventory(filepath.Dir(absolute), absolute)
	return inventory, err
}

// PreviewImport validates and computes a merge without creating or modifying files.
func (s *ChannelConfigService) PreviewImport(req ChannelImportRequest) (ChannelMergePreview, error) {
	if s == nil {
		return ChannelMergePreview{}, errors.New("channel config service is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, preview, err := s.planImport(req)
	return preview, err
}

// PrepareImport returns the exact request and preview that must be applied together.
func (s *ChannelConfigService) PrepareImport(req ChannelImportRequest) (ChannelImportPlan, error) {
	preview, err := s.PreviewImport(req)
	if err != nil {
		return ChannelImportPlan{}, err
	}
	req.SourceDigest = preview.Source.Digest
	req.DestinationDigest = preview.Destination.Digest
	return ChannelImportPlan{Request: req, Preview: preview}, nil
}

// ApplyImport performs the explicit atomic write after recomputing the merge.
func (s *ChannelConfigService) ApplyImport(req ChannelImportRequest) (ChannelMergeResult, error) {
	if s == nil {
		return ChannelMergeResult{}, errors.New("channel config service is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(req.SourceDigest) == "" || strings.TrimSpace(req.DestinationDigest) == "" {
		return ChannelMergeResult{}, errors.New("sourceDigest and destinationDigest are required")
	}
	document, preview, err := s.planImport(req)
	if err != nil {
		return ChannelMergeResult{}, err
	}
	if err := validatePreviewVersion(req.SourcePath, preview.Source, req.SourceDigest); err != nil {
		return ChannelMergeResult{}, err
	}
	if err := validatePreviewVersion(s.configPath, preview.Destination, req.DestinationDigest); err != nil {
		return ChannelMergeResult{}, err
	}
	changed := preview.ProviderAction == channelMergeAdd
	if !changed {
		for _, item := range append(append([]ChannelMergeItem{}, preview.Models...), preview.ModelOverrides...) {
			if item.Action == channelMergeAdd {
				changed = true
				break
			}
		}
	}
	if !changed {
		return ChannelMergeResult{Preview: preview}, nil
	}
	if err := verifyChannelFileVersion(req.SourcePath, preview.Source); err != nil {
		return ChannelMergeResult{}, err
	}
	finalDigest, err := writePiModelsAtomicVersion(s.configPath, document, preview.Destination.Digest, func() error {
		return verifyChannelFileVersion(req.SourcePath, preview.Source)
	})
	if err != nil {
		return ChannelMergeResult{}, err
	}
	actualDigest, _, err := channelFileDigest(s.configPath, false)
	if err != nil {
		return ChannelMergeResult{}, fmt.Errorf("verify models.json after write: %w", err)
	}
	if actualDigest != finalDigest {
		return ChannelMergeResult{}, fmt.Errorf("verify models.json after write: digest changed (expected %s, found %s)", finalDigest, actualDigest)
	}
	return ChannelMergeResult{Preview: preview, Changed: true}, nil
}

// PreviewCustom validates typed input and previews adding a custom channel or
// preset without writing.
func (s *ChannelConfigService) PreviewCustom(req ChannelCustomRequest) (ChannelMergePreview, error) {
	if s == nil {
		return ChannelMergePreview{}, errors.New("channel config service is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, preview, err := s.planCustom(req)
	return preview, err
}

// PrepareCustom returns the exact request and preview that must be applied together.
func (s *ChannelConfigService) PrepareCustom(req ChannelCustomRequest) (ChannelCustomPlan, error) {
	preview, err := s.PreviewCustom(req)
	if err != nil {
		return ChannelCustomPlan{}, err
	}
	req.DestinationDigest = preview.Destination.Digest
	return ChannelCustomPlan{Request: req, Preview: preview}, nil
}

// ApplyCustom performs the explicit atomic write for typed channel input.
func (s *ChannelConfigService) ApplyCustom(req ChannelCustomRequest) (ChannelMergeResult, error) {
	if s == nil {
		return ChannelMergeResult{}, errors.New("channel config service is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(req.DestinationDigest) == "" {
		return ChannelMergeResult{}, errors.New("destinationDigest is required")
	}
	document, preview, err := s.planCustom(req)
	if err != nil {
		return ChannelMergeResult{}, err
	}
	if err := validatePreviewVersion(s.configPath, preview.Destination, req.DestinationDigest); err != nil {
		return ChannelMergeResult{}, err
	}
	changed := preview.ProviderAction == channelMergeAdd
	if !changed {
		for _, item := range append(append([]ChannelMergeItem{}, preview.Models...), preview.ModelOverrides...) {
			if item.Action == channelMergeAdd {
				changed = true
				break
			}
		}
	}
	if !changed {
		return ChannelMergeResult{Preview: preview}, nil
	}
	finalDigest, err := writePiModelsAtomicVersion(s.configPath, document, preview.Destination.Digest, nil)
	if err != nil {
		return ChannelMergeResult{}, err
	}
	actualDigest, _, err := channelFileDigest(s.configPath, false)
	if err != nil {
		return ChannelMergeResult{}, fmt.Errorf("verify models.json after write: %w", err)
	}
	if actualDigest != finalDigest {
		return ChannelMergeResult{}, fmt.Errorf("verify models.json after write: digest changed (expected %s, found %s)", finalDigest, actualDigest)
	}
	return ChannelMergeResult{Preview: preview, Changed: true}, nil
}

func (s *ChannelConfigService) planImport(req ChannelImportRequest) (map[string]any, ChannelMergePreview, error) {
	if strings.TrimSpace(req.SourcePath) == "" {
		return nil, ChannelMergePreview{}, errors.New("source models.json path is required")
	}
	req.ProviderID = strings.TrimSpace(req.ProviderID)
	if req.ProviderID == "" {
		return nil, ChannelMergePreview{}, errors.New("providerId is required")
	}
	if strings.TrimSpace(req.SourceDigest) != "" {
		actual, _, err := channelFileDigest(req.SourcePath, false)
		if err != nil {
			return nil, ChannelMergePreview{}, fmt.Errorf("read source models.json: %w", err)
		}
		if actual != req.SourceDigest {
			return nil, ChannelMergePreview{}, &ChannelPreviewStaleError{
				Code: channelErrorPreviewStale, Path: req.SourcePath, Expected: req.SourceDigest, Actual: actual,
			}
		}
	}
	if strings.TrimSpace(req.DestinationDigest) != "" {
		actual, _, err := channelFileDigest(s.configPath, true)
		if err != nil {
			return nil, ChannelMergePreview{}, fmt.Errorf("read destination models.json: %w", err)
		}
		if actual != req.DestinationDigest {
			return nil, ChannelMergePreview{}, &ChannelPreviewStaleError{
				Code: channelErrorPreviewStale, Path: s.configPath, Expected: req.DestinationDigest, Actual: actual,
			}
		}
	}
	source, sourceVersion, _, err := readModelsSnapshot(req.SourcePath, false)
	if err != nil {
		return nil, ChannelMergePreview{}, fmt.Errorf("read source models.json: %w", err)
	}
	target, destinationVersion, destinationData, err := readModelsSnapshot(s.configPath, true)
	if err != nil {
		return nil, ChannelMergePreview{}, fmt.Errorf("read destination models.json: %w", err)
	}
	merged, preview, err := mergeImportedProvider(target, source, req)
	if err != nil {
		return nil, ChannelMergePreview{}, err
	}
	preview.ConfigPath = s.configPath
	preview.Source = sourceVersion
	preview.Destination = destinationVersion
	if destinationVersion.Exists && hasPiJSONComments(destinationData) {
		preview.Notices = append(preview.Notices, ChannelNotice{
			Code: channelNoticeCommentsRemoved, Path: s.configPath, Action: "removed",
		})
	}
	if err := validateModelsDocument(merged); err != nil {
		return nil, ChannelMergePreview{}, fmt.Errorf("validate merged models.json: %w", err)
	}
	return merged, preview, nil
}

func customPreviewVersion(document map[string]any) ChannelFileVersion {
	data, err := encodePiModelsDocument(document)
	if err != nil {
		return ChannelFileVersion{Path: "custom", Exists: true}
	}
	return ChannelFileVersion{
		Path:   "custom",
		Exists: true,
		Digest: digestBytes(data),
	}
}

func (s *ChannelConfigService) planCustom(req ChannelCustomRequest) (map[string]any, ChannelMergePreview, error) {
	if strings.TrimSpace(req.DestinationDigest) != "" {
		actual, _, err := channelFileDigest(s.configPath, true)
		if err != nil {
			return nil, ChannelMergePreview{}, fmt.Errorf("read destination models.json: %w", err)
		}
		if actual != req.DestinationDigest {
			return nil, ChannelMergePreview{}, &ChannelPreviewStaleError{
				Code: channelErrorPreviewStale, Path: s.configPath, Expected: req.DestinationDigest, Actual: actual,
			}
		}
	}
	sourceDocument, err := customChannelDocument(req)
	if err != nil {
		return nil, ChannelMergePreview{}, err
	}
	target, destinationVersion, destinationData, err := readModelsSnapshot(s.configPath, true)
	if err != nil {
		return nil, ChannelMergePreview{}, fmt.Errorf("read destination models.json: %w", err)
	}
	mergeRequest := ChannelImportRequest{
		ProviderID:  req.ProviderID,
		ModelIDs:    customModelIDs(req.Models),
		OverrideIDs: customOverrideIDs(req.ModelOverride),
	}
	merged, preview, err := mergeImportedProvider(target, sourceDocument, mergeRequest)
	if err != nil {
		return nil, ChannelMergePreview{}, err
	}
	preview.ConfigPath = s.configPath
	preview.Source = customPreviewVersion(sourceDocument)
	preview.Destination = destinationVersion
	if destinationVersion.Exists && hasPiJSONComments(destinationData) {
		preview.Notices = append(preview.Notices, ChannelNotice{
			Code: channelNoticeCommentsRemoved, Path: s.configPath, Action: "removed",
		})
	}
	if err := validateModelsDocument(merged); err != nil {
		return nil, ChannelMergePreview{}, fmt.Errorf("validate merged models.json: %w", err)
	}
	return merged, preview, nil
}

func readChannelInventory(agentDir, path string) (map[string]any, ChannelInventory, error) {
	document, exists, err := readModelsDocumentIfPresent(path)
	inventory := ChannelInventory{
		AgentDir:   agentDir,
		ConfigPath: path,
		Exists:     exists,
		Providers:  []ChannelProviderSummary{},
	}
	if err != nil {
		return nil, inventory, err
	}
	providers, err := providersObject(document)
	if err != nil {
		return nil, inventory, err
	}
	ids := sortedObjectKeys(providers)
	for _, id := range ids {
		provider, ok := providers[id].(map[string]any)
		if !ok {
			return nil, inventory, fmt.Errorf("providers.%s must be an object", id)
		}
		summary := ChannelProviderSummary{
			ID:            id,
			Name:          channelStringValue(provider["name"]),
			BaseURL:       safeChannelBaseURLSummary(provider["baseUrl"]),
			API:           channelStringValue(provider["api"]),
			OAuth:         channelStringValue(provider["oauth"]),
			AuthHeader:    channelBoolValue(provider["authHeader"]),
			Credential:    summarizeProviderCredential(provider),
			Headers:       summarizeHeaders(provider["headers"]),
			Models:        []ChannelModelSummary{},
			ModelOverride: []ChannelModelSummary{},
		}
		for _, model := range effectiveModelMaps(provider["models"]) {
			summary.Models = append(summary.Models, summarizeModel(model))
		}
		if overrides, ok := provider["modelOverrides"].(map[string]any); ok {
			for _, overrideID := range sortedObjectKeys(overrides) {
				override, ok := overrides[overrideID].(map[string]any)
				if !ok {
					continue
				}
				model := summarizeModel(override)
				model.ID = overrideID
				summary.ModelOverride = append(summary.ModelOverride, model)
			}
		}
		inventory.Providers = append(inventory.Providers, summary)
	}
	return document, inventory, nil
}

func summarizeProviderCredential(provider map[string]any) ChannelCredentialSummary {
	if value, ok := provider["apiKey"].(string); ok {
		kind := channelCredentialLiteral
		switch {
		case isPiEnvironmentReference(value):
			kind = channelCredentialEnvironment
		case strings.HasPrefix(value, "!"):
			kind = channelCredentialCommand
		}
		return ChannelCredentialSummary{Configured: true, Kind: kind}
	}
	if oauth := channelStringValue(provider["oauth"]); oauth != "" {
		return ChannelCredentialSummary{Configured: true, Kind: channelCredentialOAuth}
	}
	return ChannelCredentialSummary{Kind: channelCredentialNone}
}

func summarizeHeaders(value any) ChannelHeaderSummary {
	headers, ok := value.(map[string]any)
	if !ok {
		return ChannelHeaderSummary{}
	}
	summary := ChannelHeaderSummary{Count: len(headers)}
	for _, raw := range headers {
		header, ok := raw.(string)
		if ok && isPiEnvironmentReference(header) {
			summary.EnvironmentReferences++
		} else {
			summary.Unsupported++
		}
	}
	return summary
}

func summarizeModel(model map[string]any) ChannelModelSummary {
	summary := ChannelModelSummary{
		ID:      channelStringValue(model["id"]),
		Name:    channelStringValue(model["name"]),
		API:     channelStringValue(model["api"]),
		BaseURL: safeChannelBaseURLSummary(model["baseUrl"]),
		Headers: summarizeHeaders(model["headers"]),
	}
	if reasoning, ok := model["reasoning"].(bool); ok {
		summary.Reasoning = &reasoning
	}
	if input, ok := model["input"].([]any); ok {
		for _, raw := range input {
			if value, ok := raw.(string); ok {
				summary.Input = append(summary.Input, value)
			}
		}
	}
	summary.ContextWindow = numberPointer(model["contextWindow"])
	summary.MaxTokens = numberPointer(model["maxTokens"])
	return summary
}

func safeChannelBaseURLSummary(value any) string {
	raw := channelStringValue(value)
	if raw == "" {
		return ""
	}
	base, _, _ := strings.Cut(raw, "?")
	base, _, _ = strings.Cut(base, "#")
	if base == "" {
		return "[redacted]"
	}
	return base
}

func mergeImportedProvider(target, source map[string]any, req ChannelImportRequest) (map[string]any, ChannelMergePreview, error) {
	sourceProviders, err := providersObject(source)
	if err != nil {
		return nil, ChannelMergePreview{}, fmt.Errorf("source: %w", err)
	}
	sourceProviderValue, ok := sourceProviders[req.ProviderID]
	if !ok {
		return nil, ChannelMergePreview{}, fmt.Errorf("source provider %q was not found", req.ProviderID)
	}
	sourceProvider, ok := sourceProviderValue.(map[string]any)
	if !ok {
		return nil, ChannelMergePreview{}, fmt.Errorf("source provider %q must be an object", req.ProviderID)
	}
	modelIDs, err := uniqueNonEmptyIDs(req.ModelIDs, "modelId")
	if err != nil {
		return nil, ChannelMergePreview{}, err
	}
	overrideIDs, err := uniqueNonEmptyIDs(req.OverrideIDs, "overrideId")
	if err != nil {
		return nil, ChannelMergePreview{}, err
	}
	sourceModels := effectiveModelMap(sourceProvider["models"])
	selectedModels := make([]map[string]any, 0, len(modelIDs))
	for _, id := range modelIDs {
		model, ok := sourceModels[id]
		if !ok {
			return nil, ChannelMergePreview{}, fmt.Errorf("source provider %q has no model %q", req.ProviderID, id)
		}
		selectedModels = append(selectedModels, model)
	}
	sourceOverrides, _ := sourceProvider["modelOverrides"].(map[string]any)
	selectedOverrides := make([]selectedModelOverride, 0, len(overrideIDs))
	for _, id := range overrideIDs {
		override, ok := sourceOverrides[id].(map[string]any)
		if !ok {
			return nil, ChannelMergePreview{}, fmt.Errorf("source provider %q has no model override %q", req.ProviderID, id)
		}
		selectedOverrides = append(selectedOverrides, selectedModelOverride{id: id, value: override})
	}

	merged := cloneJSONObject(target)
	if _, ok := merged["$schema"]; !ok {
		merged["$schema"] = piModelsSchemaURL
	}
	providers, err := ensureProvidersObject(merged)
	if err != nil {
		return nil, ChannelMergePreview{}, err
	}
	preview := ChannelMergePreview{
		ProviderID:     req.ProviderID,
		ConflictPolicy: channelMergePreserve,
		Conflicts:      []ChannelConflict{},
		Models:         []ChannelMergeItem{},
		ModelOverrides: []ChannelMergeItem{},
		Notices:        []ChannelNotice{},
	}

	destinationProvider, exists := providers[req.ProviderID]
	if !exists {
		provider, notices := sanitizeImportedProvider(req.ProviderID, sourceProvider, selectedModels, selectedOverrides)
		providers[req.ProviderID] = provider
		preview.ProviderAction = channelMergeAdd
		preview.Notices = append(preview.Notices, notices...)
		for _, model := range selectedModels {
			preview.Models = append(preview.Models, ChannelMergeItem{
				ID: channelStringValue(model["id"]), Action: channelMergeAdd, Verification: "pendingInPi",
			})
		}
		for _, override := range selectedOverrides {
			preview.ModelOverrides = append(preview.ModelOverrides, ChannelMergeItem{
				ID: override.id, Action: channelMergeAdd, Verification: "pendingInPi",
			})
		}
		return merged, preview, nil
	}

	destinationMap, ok := destinationProvider.(map[string]any)
	if !ok {
		return nil, ChannelMergePreview{}, fmt.Errorf("destination provider %q must be an object", req.ProviderID)
	}
	preview.ProviderAction = channelMergePreserve
	providerCompatible, conflictFields := importedProviderCompatibility(req.ProviderID, sourceProvider, destinationMap, selectedModels)
	preview.Notices = append(preview.Notices, ChannelNotice{
		Code: "providerPreserved", Path: providerPath(req.ProviderID), Action: channelMergePreserve,
	})
	if !providerCompatible {
		preview.Conflicts = append(preview.Conflicts, ChannelConflict{
			Code: channelNoticeProviderEndpointConflict, Path: providerPath(req.ProviderID), Fields: conflictFields,
		})
	}
	destinationModels := effectiveModelMap(destinationMap["models"])
	modelList, _ := destinationMap["models"].([]any)
	for _, model := range selectedModels {
		id := channelStringValue(model["id"])
		if _, exists := destinationModels[id]; exists {
			preview.Models = append(preview.Models, ChannelMergeItem{ID: id, Action: channelMergePreserve})
			preview.Notices = append(preview.Notices, ChannelNotice{
				Code: "modelConflictPreserved", Path: modelPath(req.ProviderID, id), Action: channelMergePreserve,
			})
			continue
		}
		if !providerCompatible {
			preview.Models = append(preview.Models, ChannelMergeItem{ID: id, Action: channelMergePreserve})
			preview.Notices = append(preview.Notices, ChannelNotice{
				Code: channelNoticeProviderEndpointConflict, Path: modelPath(req.ProviderID, id), Action: channelMergePreserve,
			})
			continue
		}
		sanitized, notices := sanitizeImportedModel(req.ProviderID, id, model)
		if inheritsDestinationProviderEndpoint(sourceProvider, model, destinationMap) {
			delete(sanitized, "api")
			delete(sanitized, "baseUrl")
			notices = append(notices, ChannelNotice{
				Code: channelNoticeModelEndpointInherited, Path: modelPath(req.ProviderID, id), Action: "inherit",
			})
		}
		modelList = append(modelList, sanitized)
		destinationModels[id] = sanitized
		preview.Models = append(preview.Models, ChannelMergeItem{ID: id, Action: channelMergeAdd, Verification: "pendingInPi"})
		preview.Notices = append(preview.Notices, notices...)
	}
	if len(modelList) > 0 {
		destinationMap["models"] = modelList
	}

	overrideMap, _ := destinationMap["modelOverrides"].(map[string]any)
	if overrideMap == nil {
		overrideMap = make(map[string]any)
	}
	for _, override := range selectedOverrides {
		id := override.id
		if _, exists := overrideMap[id]; exists {
			preview.ModelOverrides = append(preview.ModelOverrides, ChannelMergeItem{ID: id, Action: channelMergePreserve})
			preview.Notices = append(preview.Notices, ChannelNotice{
				Code: "modelOverrideConflictPreserved", Path: overridePath(req.ProviderID, id), Action: channelMergePreserve,
			})
			continue
		}
		if !providerCompatible {
			preview.ModelOverrides = append(preview.ModelOverrides, ChannelMergeItem{ID: id, Action: channelMergePreserve})
			preview.Notices = append(preview.Notices, ChannelNotice{
				Code: channelNoticeProviderEndpointConflict, Path: overridePath(req.ProviderID, id), Action: channelMergePreserve,
			})
			continue
		}
		sanitized, notices := sanitizeImportedModel(req.ProviderID, id, override.value)
		if inheritsDestinationProviderEndpoint(sourceProvider, override.value, destinationMap) {
			delete(sanitized, "api")
			delete(sanitized, "baseUrl")
			notices = append(notices, ChannelNotice{
				Code: channelNoticeModelEndpointInherited, Path: overridePath(req.ProviderID, id), Action: "inherit",
			})
		}
		overrideMap[id] = sanitized
		preview.ModelOverrides = append(preview.ModelOverrides, ChannelMergeItem{
			ID: id, Action: channelMergeAdd, Verification: "pendingInPi",
		})
		preview.Notices = append(preview.Notices, notices...)
	}
	if len(overrideMap) > 0 {
		destinationMap["modelOverrides"] = overrideMap
	}
	return merged, preview, nil
}

func importedProviderCompatibility(providerID string, sourceProvider, destinationProvider map[string]any, selectedModels []map[string]any) (bool, []string) {
	fields := []string{}
	sourceAPI, sourceBaseURL := effectiveProviderEndpoint(sourceProvider, nil)
	destinationAPI, destinationBaseURL := effectiveProviderEndpoint(destinationProvider, nil)
	if sourceAPI == "" {
		sourceAPI = destinationAPI
	}
	if sourceBaseURL == "" {
		sourceBaseURL = destinationBaseURL
	}
	if sourceAPI != destinationAPI || normalizeChannelEndpoint(sourceBaseURL) != normalizeChannelEndpoint(destinationBaseURL) {
		if sourceAPI != destinationAPI {
			fields = append(fields, providerPath(providerID)+".api")
		}
		if normalizeChannelEndpoint(sourceBaseURL) != normalizeChannelEndpoint(destinationBaseURL) {
			fields = append(fields, providerPath(providerID)+".baseUrl")
		}
		return false, fields
	}
	if !reflect.DeepEqual(providerHeadersForComparison(sourceProvider), providerHeadersForComparison(destinationProvider)) {
		fields = append(fields, providerPath(providerID)+".headers")
	}
	if !reflect.DeepEqual(providerCompatForComparison(sourceProvider), providerCompatForComparison(destinationProvider)) {
		fields = append(fields, providerPath(providerID)+".compat")
	}
	if channelBoolValue(sourceProvider["authHeader"]) != channelBoolValue(destinationProvider["authHeader"]) {
		fields = append(fields, providerPath(providerID)+".authHeader")
	}
	for _, model := range selectedModels {
		sourceModelAPI, sourceModelBaseURL := effectiveProviderEndpoint(sourceProvider, model)
		destinationModelAPI, destinationModelBaseURL := effectiveProviderEndpoint(destinationProvider, nil)
		if sourceModelAPI == "" {
			sourceModelAPI = destinationModelAPI
		}
		if sourceModelBaseURL == "" {
			sourceModelBaseURL = destinationModelBaseURL
		}
		if sourceModelAPI != destinationModelAPI ||
			normalizeChannelEndpoint(sourceModelBaseURL) != normalizeChannelEndpoint(destinationModelBaseURL) {
			modelID := channelStringValue(model["id"])
			if sourceModelAPI != destinationModelAPI {
				fields = append(fields, modelPath(providerID, modelID)+".api")
			}
			if normalizeChannelEndpoint(sourceModelBaseURL) != normalizeChannelEndpoint(destinationModelBaseURL) {
				fields = append(fields, modelPath(providerID, modelID)+".baseUrl")
			}
		}
	}
	return len(fields) == 0, fields
}

func effectiveProviderEndpoint(provider, model map[string]any) (string, string) {
	api := channelStringValue(model["api"])
	if api == "" {
		api = channelStringValue(provider["api"])
	}
	baseURL := channelStringValue(model["baseUrl"])
	if baseURL == "" {
		baseURL = channelStringValue(provider["baseUrl"])
	}
	return api, baseURL
}

func normalizeChannelEndpoint(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" {
		if before, _, ok := strings.Cut(value, "?"); ok {
			value = before
		}
		if before, _, ok := strings.Cut(value, "#"); ok {
			value = before
		}
		return strings.TrimRight(value, "/")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Host = strings.ToLower(parsed.Host)
	return strings.TrimRight(parsed.String(), "/")
}

func providerHeadersForComparison(provider map[string]any) map[string]string {
	return normalizedStringMap(provider["headers"])
}

func providerCompatForComparison(provider map[string]any) any {
	if value, ok := provider["compat"].(map[string]any); ok {
		return cloneJSONValue(value)
	}
	return map[string]any{}
}

func normalizedStringMap(value any) map[string]string {
	result := map[string]string{}
	if values, ok := value.(map[string]any); ok {
		for key, raw := range values {
			if text, ok := raw.(string); ok {
				result[key] = text
			}
		}
	}
	return result
}

func inheritsDestinationProviderEndpoint(sourceProvider, sourceModel, destinationProvider map[string]any) bool {
	_, hasAPI := sourceModel["api"]
	_, hasBaseURL := sourceModel["baseUrl"]
	// Provider defaults are inherited without a target warning. Report the
	// notice only when this selected entry supplied at least one endpoint
	// field that will be removed to reuse the destination provider's route.
	if !hasAPI && !hasBaseURL {
		return false
	}
	sourceAPI, sourceBaseURL := effectiveProviderEndpoint(sourceProvider, sourceModel)
	destinationAPI, destinationBaseURL := effectiveProviderEndpoint(destinationProvider, nil)
	return sourceAPI == destinationAPI &&
		normalizeChannelEndpoint(sourceBaseURL) == normalizeChannelEndpoint(destinationBaseURL)
}

func isBuiltInPiProvider(providerID string) bool {
	switch providerID {
	case "amazon-bedrock", "ant-ling", "anthropic", "azure", "baseten", "cerebras",
		"cloudflare-ai-gateway", "cloudflare-workers-ai", "deepseek", "fireworks",
		"github-copilot", "google", "google-vertex", "groq", "huggingface",
		"kimi-coding", "meta", "minimax", "minimax-cn", "mistral", "moonshotai",
		"moonshotai-cn", "nvidia", "openai", "openai-codex", "opencode",
		"opencode-go", "openrouter", "qwen-token-plan", "qwen-token-plan-cn",
		"qwen-token-plan-individual", "radius", "together", "typesafe",
		"vercel-ai-gateway", "xai", "xiaomi", "xiaomi-token-plan-ams",
		"xiaomi-token-plan-cn", "xiaomi-token-plan-sgp", "zai", "zai-coding-cn":
		return true
	default:
		return false
	}
}

func sanitizeImportedProvider(providerID string, source map[string]any, models []map[string]any, overrides []selectedModelOverride) (map[string]any, []ChannelNotice) {
	provider := cloneJSONObject(source)
	notices := []ChannelNotice{}
	sanitizeCredentialField(provider, providerPath(providerID)+".apiKey", &notices)
	if _, exists := provider["oauth"]; exists {
		delete(provider, "oauth")
		notices = append(notices, ChannelNotice{
			Code: channelNoticeConfigureInPi, Path: providerPath(providerID) + ".oauth", Action: channelNoticeConfigureInPi,
		})
	}
	provider["headers"] = sanitizeHeaders(provider["headers"], providerPath(providerID)+".headers", &notices)
	if _, ok := provider["headers"].(map[string]any); !ok {
		delete(provider, "headers")
	}
	delete(provider, "models")
	delete(provider, "modelOverrides")
	if len(models) > 0 {
		list := make([]any, 0, len(models))
		for _, model := range models {
			sanitized, modelNotices := sanitizeImportedModel(providerID, channelStringValue(model["id"]), model)
			list = append(list, sanitized)
			notices = append(notices, modelNotices...)
		}
		provider["models"] = list
	}
	if len(overrides) > 0 {
		result := make(map[string]any, len(overrides))
		for _, override := range overrides {
			sanitized, overrideNotices := sanitizeImportedModel(providerID, override.id, override.value)
			result[override.id] = sanitized
			notices = append(notices, overrideNotices...)
		}
		provider["modelOverrides"] = result
	}
	return provider, notices
}

func sanitizeImportedModel(providerID, modelID string, source map[string]any) (map[string]any, []ChannelNotice) {
	model := cloneJSONObject(source)
	notices := []ChannelNotice{}
	basePath := modelPath(providerID, modelID)
	if _, exists := model["apiKey"]; exists {
		delete(model, "apiKey")
		notices = append(notices, ChannelNotice{
			Code: channelNoticeConfigureInPi, Path: basePath + ".apiKey", Action: channelNoticeConfigureInPi,
		})
	}
	model["headers"] = sanitizeHeaders(model["headers"], basePath+".headers", &notices)
	if _, ok := model["headers"].(map[string]any); !ok {
		delete(model, "headers")
	}
	return model, notices
}

func sanitizeCredentialField(object map[string]any, path string, notices *[]ChannelNotice) {
	value, ok := object["apiKey"].(string)
	if !ok {
		if _, exists := object["apiKey"]; exists {
			delete(object, "apiKey")
			*notices = append(*notices, ChannelNotice{Code: channelNoticeConfigureInPi, Path: path, Action: channelNoticeConfigureInPi})
		}
		return
	}
	if isPiEnvironmentReference(value) {
		return
	}
	delete(object, "apiKey")
	*notices = append(*notices, ChannelNotice{Code: channelNoticeConfigureInPi, Path: path, Action: channelNoticeConfigureInPi})
}

func sanitizeHeaders(value any, path string, notices *[]ChannelNotice) any {
	headers, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	result := make(map[string]any)
	names := sortedObjectKeys(headers)
	for _, name := range names {
		raw, ok := headers[name].(string)
		if ok && isPiEnvironmentReference(raw) {
			result[name] = raw
			continue
		}
		*notices = append(*notices, ChannelNotice{
			Code: channelNoticeConfigureInPi, Path: path + "." + name, Action: channelNoticeConfigureInPi,
		})
	}
	return result
}

func customChannelDocument(req ChannelCustomRequest) (map[string]any, error) {
	req.ProviderID = strings.TrimSpace(req.ProviderID)
	if req.ProviderID == "" {
		return nil, errors.New("providerId is required")
	}
	if req.APIKeyEnv != "" && !isPiEnvironmentReference(req.APIKeyEnv) {
		return nil, errors.New("apiKeyEnv must use Pi syntax $NAME or ${NAME}")
	}
	provider := make(map[string]any)
	putNonEmptyString(provider, "name", req.Name)
	putNonEmptyString(provider, "baseUrl", req.BaseURL)
	putNonEmptyString(provider, "api", req.API)
	if req.AuthHeader != nil {
		provider["authHeader"] = *req.AuthHeader
	}
	if req.APIKeyEnv != "" {
		provider["apiKey"] = req.APIKeyEnv
	}
	headers, err := customHeaders(req.HeadersEnv)
	if err != nil {
		return nil, err
	}
	if len(headers) > 0 {
		provider["headers"] = headers
	}
	models := make([]any, 0, len(req.Models))
	seenModels := make(map[string]bool)
	for _, input := range req.Models {
		id := strings.TrimSpace(input.ID)
		if id == "" {
			return nil, errors.New("custom model id is required")
		}
		if seenModels[id] {
			return nil, fmt.Errorf("custom model %q is duplicated", id)
		}
		seenModels[id] = true
		model := make(map[string]any)
		model["id"] = id
		putNonEmptyString(model, "name", input.Name)
		putNonEmptyString(model, "api", input.API)
		putNonEmptyString(model, "baseUrl", input.BaseURL)
		if input.Reasoning != nil {
			model["reasoning"] = *input.Reasoning
		}
		if err := putModelInput(model, input.Input); err != nil {
			return nil, fmt.Errorf("custom model %q: %w", id, err)
		}
		if err := putPositiveNumber(model, "contextWindow", input.ContextWindow); err != nil {
			return nil, fmt.Errorf("custom model %q: %w", id, err)
		}
		if err := putPositiveNumber(model, "maxTokens", input.MaxTokens); err != nil {
			return nil, fmt.Errorf("custom model %q: %w", id, err)
		}
		modelHeaders, err := customHeaders(input.HeadersEnv)
		if err != nil {
			return nil, fmt.Errorf("custom model %q: %w", id, err)
		}
		if len(modelHeaders) > 0 {
			model["headers"] = modelHeaders
		}
		if !isBuiltInPiProvider(req.ProviderID) && !hasEffectiveEndpoint(provider, model) {
			return nil, fmt.Errorf("custom model %q requires api and baseUrl at provider or model level", id)
		}
		models = append(models, model)
	}
	if len(models) > 0 {
		provider["models"] = models
	}
	overrides := make(map[string]any)
	for _, input := range req.ModelOverride {
		id := strings.TrimSpace(input.ID)
		if id == "" {
			return nil, errors.New("custom model override id is required")
		}
		if _, exists := overrides[id]; exists {
			return nil, fmt.Errorf("custom model override %q is duplicated", id)
		}
		override := make(map[string]any)
		putNonEmptyString(override, "name", input.Name)
		if input.Reasoning != nil {
			override["reasoning"] = *input.Reasoning
		}
		if err := putModelInput(override, input.Input); err != nil {
			return nil, fmt.Errorf("custom model override %q: %w", id, err)
		}
		if err := putPositiveNumber(override, "contextWindow", input.ContextWindow); err != nil {
			return nil, fmt.Errorf("custom model override %q: %w", id, err)
		}
		if err := putPositiveNumber(override, "maxTokens", input.MaxTokens); err != nil {
			return nil, fmt.Errorf("custom model override %q: %w", id, err)
		}
		overrideHeaders, err := customHeaders(input.HeadersEnv)
		if err != nil {
			return nil, fmt.Errorf("custom model override %q: %w", id, err)
		}
		if len(overrideHeaders) > 0 {
			override["headers"] = overrideHeaders
		}
		overrides[id] = override
	}
	if len(overrides) > 0 {
		provider["modelOverrides"] = overrides
	}
	if len(models) == 0 && len(overrides) == 0 && channelStringValue(provider["baseUrl"]) == "" &&
		channelStringValue(provider["api"]) == "" && provider["headers"] == nil &&
		channelStringValue(provider["apiKey"]) == "" && provider["compat"] == nil &&
		provider["authHeader"] == nil {
		return nil, errors.New("custom channel has no provider or model settings")
	}
	source := map[string]any{
		"$schema": piModelsSchemaURL,
		"providers": map[string]any{
			req.ProviderID: provider,
		},
	}
	if err := validateModelsDocument(source); err != nil {
		return nil, err
	}
	return source, nil
}

func customHeaders(headers map[string]string) (map[string]any, error) {
	result := make(map[string]any, len(headers))
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			return nil, errors.New("custom header name is required")
		}
		value := headers[name]
		if !isPiEnvironmentReference(value) {
			return nil, fmt.Errorf("custom header %q must use Pi syntax $NAME or ${NAME}", name)
		}
		result[name] = value
	}
	return result, nil
}

func hasEffectiveEndpoint(provider, model map[string]any) bool {
	api := channelStringValue(model["api"])
	if api == "" {
		api = channelStringValue(provider["api"])
	}
	baseURL := channelStringValue(model["baseUrl"])
	if baseURL == "" {
		baseURL = channelStringValue(provider["baseUrl"])
	}
	return api != "" && baseURL != ""
}

func putModelInput(target map[string]any, values []string) error {
	if len(values) == 0 {
		return nil
	}
	input := make([]any, 0, len(values))
	for _, value := range values {
		if value != "text" && value != "image" {
			return fmt.Errorf("input value %q must be text or image", value)
		}
		input = append(input, value)
	}
	target["input"] = input
	return nil
}

func putPositiveNumber(target map[string]any, key string, value *float64) error {
	if value == nil {
		return nil
	}
	if *value <= 0 {
		return fmt.Errorf("%s must be greater than zero", key)
	}
	target[key] = *value
	return nil
}

func putNonEmptyString(target map[string]any, key, value string) {
	if value = strings.TrimSpace(value); value != "" {
		target[key] = value
	}
}

func customModelIDs(models []ChannelCustomModel) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}

func customOverrideIDs(overrides []ChannelCustomModelOverride) []string {
	ids := make([]string, 0, len(overrides))
	for _, override := range overrides {
		ids = append(ids, override.ID)
	}
	return ids
}

func effectiveModelMap(value any) map[string]map[string]any {
	result := make(map[string]map[string]any)
	list, _ := value.([]any)
	for _, raw := range list {
		model, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := channelStringValue(model["id"])
		if id != "" {
			result[id] = model
		}
	}
	return result
}

func effectiveModelMaps(value any) []map[string]any {
	list, _ := value.([]any)
	index := make(map[string]int)
	models := make([]map[string]any, 0, len(list))
	for _, raw := range list {
		model, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := channelStringValue(model["id"])
		if id == "" {
			continue
		}
		if at, exists := index[id]; exists {
			models[at] = model
			continue
		}
		index[id] = len(models)
		models = append(models, model)
	}
	return models
}

func readModelsDocumentIfPresent(path string) (map[string]any, bool, error) {
	readPath, err := resolvePiModelsReadPath(path)
	if err != nil {
		return nil, false, err
	}
	document, err := readModelsDocument(readPath, true)
	if err != nil {
		return nil, false, err
	}
	_, statErr := os.Stat(readPath)
	if errors.Is(statErr, os.ErrNotExist) {
		return document, false, nil
	}
	if statErr != nil {
		return nil, false, statErr
	}
	return document, true, nil
}

func readModelsSnapshot(path string, missingOK bool) (map[string]any, ChannelFileVersion, []byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, ChannelFileVersion{}, nil, errors.New("models.json path is required")
	}
	readPath, err := resolvePiModelsReadPath(path)
	if err != nil {
		return nil, ChannelFileVersion{}, nil, err
	}
	data, exists, err := readChannelFile(readPath, missingOK)
	if err != nil {
		return nil, ChannelFileVersion{}, nil, err
	}
	version := ChannelFileVersion{Path: readPath, Exists: exists, Digest: channelMissingDigest}
	if exists {
		version.Digest = digestBytes(data)
		document, err := parseModelsDocument(data)
		if err != nil {
			return nil, version, data, fmt.Errorf("%s: %w", path, err)
		}
		if err := validateCustomProviderEndpoints(document); err != nil {
			return nil, version, data, fmt.Errorf("%s: %w", path, err)
		}
		return document, version, data, nil
	}
	return map[string]any{
		"$schema":   piModelsSchemaURL,
		"providers": map[string]any{},
	}, version, nil, nil
}

func readChannelFile(path string, missingOK bool) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && missingOK {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func channelFileDigest(path string, missingOK bool) (string, bool, error) {
	readPath, err := resolvePiModelsReadPath(path)
	if err != nil {
		return "", false, err
	}
	data, exists, err := readChannelFile(readPath, missingOK)
	if err != nil {
		return "", false, err
	}
	if !exists {
		return channelMissingDigest, false, nil
	}
	return digestBytes(data), true, nil
}

func resolvePiModelsReadPath(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		if info, lstatErr := os.Lstat(path); lstatErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("models.json is a dangling symlink; refusing to replace it: %w", err)
		} else if lstatErr != nil && !errors.Is(lstatErr, os.ErrNotExist) {
			return "", fmt.Errorf("inspect models.json: %w", lstatErr)
		}
		return path, nil
	}
	return "", fmt.Errorf("resolve models.json symlink: %w", err)
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum[:])
}

func validatePreviewVersion(path string, version ChannelFileVersion, expected string) error {
	if expected == channelMissingDigest {
		actual, exists, err := channelFileDigest(path, true)
		if err != nil {
			return err
		}
		if exists {
			return &ChannelPreviewStaleError{Code: channelErrorPreviewStale, Path: path, Expected: expected, Actual: actual}
		}
		return nil
	}
	if expected != version.Digest {
		return &ChannelPreviewStaleError{
			Code: channelErrorPreviewStale, Path: path, Expected: expected, Actual: version.Digest,
		}
	}
	return verifyChannelFileVersion(path, version)
}

func verifyChannelFileVersion(path string, version ChannelFileVersion) error {
	actual, exists, err := channelFileDigest(version.Path, !version.Exists)
	if err != nil {
		return err
	}
	if version.Exists && !exists {
		return &ChannelPreviewStaleError{Code: channelErrorPreviewStale, Path: path, Expected: version.Digest, Actual: channelMissingDigest}
	}
	if actual != version.Digest {
		return &ChannelPreviewStaleError{Code: channelErrorPreviewStale, Path: path, Expected: version.Digest, Actual: actual}
	}
	return nil
}

func readModelsDocument(path string, missingOK bool) (map[string]any, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("models.json path is required")
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && missingOK {
		return map[string]any{
			"$schema":   piModelsSchemaURL,
			"providers": map[string]any{},
		}, nil
	}
	if err != nil {
		return nil, err
	}
	document, err := parseModelsDocument(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return document, nil
}

func parseModelsDocument(data []byte) (map[string]any, error) {
	normalized, err := normalizePiJSONC(data)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(normalized))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("parse JSON: trailing content")
	}
	document, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("models.json root must be an object")
	}
	if err := validateModelsDocument(document); err != nil {
		return nil, err
	}
	return document, nil
}

func normalizePiJSONC(data []byte) ([]byte, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("models.json is empty")
	}
	withoutComments := make([]byte, len(data))
	inString := false
	escaped := false
	for i := 0; i < len(data); i++ {
		ch := data[i]
		if inString {
			withoutComments[i] = ch
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			withoutComments[i] = ch
			continue
		}
		if ch == '/' && i+1 < len(data) && data[i+1] == '/' {
			withoutComments[i] = ' '
			withoutComments[i+1] = ' '
			i += 2
			for ; i < len(data) && data[i] != '\n'; i++ {
				withoutComments[i] = ' '
			}
			if i < len(data) {
				withoutComments[i] = '\n'
			}
			continue
		}
		withoutComments[i] = ch
	}
	if inString {
		return nil, errors.New("unterminated JSON string")
	}
	return removeTrailingJSONCommas(withoutComments), nil
}

func removeTrailingJSONCommas(data []byte) []byte {
	result := make([]byte, 0, len(data))
	inString := false
	escaped := false
	for i := 0; i < len(data); i++ {
		ch := data[i]
		if inString {
			result = append(result, ch)
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			result = append(result, ch)
			continue
		}
		if ch == ',' {
			next := i + 1
			for next < len(data) && isJSONWhitespace(data[next]) {
				next++
			}
			if next < len(data) && (data[next] == '}' || data[next] == ']') {
				continue
			}
		}
		result = append(result, ch)
	}
	return result
}

func hasPiJSONComments(data []byte) bool {
	inString := false
	escaped := false
	for i := 0; i+1 < len(data); i++ {
		ch := data[i]
		if inString {
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			continue
		}
		if ch == '/' && data[i+1] == '/' {
			return true
		}
	}
	return false
}

func isJSONWhitespace(ch byte) bool {
	switch ch {
	case ' ', '\t', '\r', '\n':
		return true
	default:
		return false
	}
}

func validateModelsDocument(document map[string]any) error {
	if document == nil {
		return errors.New("models.json root must be an object")
	}
	if value, exists := document["$schema"]; exists {
		if _, ok := value.(string); !ok {
			return errors.New("$schema must be a string")
		}
	}
	providers, err := providersObject(document)
	if err != nil {
		return err
	}
	for _, providerID := range sortedObjectKeys(providers) {
		if strings.TrimSpace(providerID) == "" {
			return errors.New("provider id must not be empty")
		}
		provider, ok := providers[providerID].(map[string]any)
		if !ok {
			return fmt.Errorf("providers.%s must be an object", providerID)
		}
		if err := validateProvider(providerID, provider); err != nil {
			return err
		}
	}
	return nil
}

func validateCustomProviderEndpoints(document map[string]any) error {
	providers, err := providersObject(document)
	if err != nil {
		return err
	}
	for _, providerID := range sortedObjectKeys(providers) {
		if isBuiltInPiProvider(providerID) {
			continue
		}
		provider, ok := providers[providerID].(map[string]any)
		if !ok {
			continue
		}
		for _, model := range effectiveModelMaps(provider["models"]) {
			api, baseURL := effectiveProviderEndpoint(provider, model)
			if api == "" || baseURL == "" {
				return fmt.Errorf("providers.%s model %q requires api and baseUrl at provider or model level", providerID, channelStringValue(model["id"]))
			}
		}
	}
	return nil
}

func validateProvider(providerID string, provider map[string]any) error {
	for _, key := range []string{"name", "baseUrl", "apiKey", "api"} {
		if value, exists := provider[key]; exists {
			text, ok := value.(string)
			if !ok || strings.TrimSpace(text) == "" {
				return fmt.Errorf("providers.%s.%s must be a non-empty string", providerID, key)
			}
		}
	}
	if value, exists := provider["oauth"]; exists {
		oauth, ok := value.(string)
		if !ok || oauth != "radius" {
			return fmt.Errorf("providers.%s.oauth must be \"radius\"", providerID)
		}
		if channelStringValue(provider["baseUrl"]) == "" {
			return fmt.Errorf("providers.%s.baseUrl is required when oauth is set", providerID)
		}
	}
	if value, exists := provider["authHeader"]; exists {
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("providers.%s.authHeader must be a boolean", providerID)
		}
	}
	if value, exists := provider["compat"]; exists {
		if _, ok := value.(map[string]any); !ok {
			return fmt.Errorf("providers.%s.compat must be an object", providerID)
		}
	}
	if err := validateHeaders(provider["headers"], "providers."+providerID+".headers"); err != nil {
		return err
	}
	if value, exists := provider["models"]; exists {
		models, ok := value.([]any)
		if !ok {
			return fmt.Errorf("providers.%s.models must be an array", providerID)
		}
		for index, raw := range models {
			model, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("providers.%s.models[%d] must be an object", providerID, index)
			}
			id, ok := model["id"].(string)
			if !ok || strings.TrimSpace(id) == "" {
				return fmt.Errorf("providers.%s.models[%d].id must be a non-empty string", providerID, index)
			}
			if err := validateModel(providerID, id, model); err != nil {
				return err
			}
		}
	}
	if value, exists := provider["modelOverrides"]; exists {
		overrides, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("providers.%s.modelOverrides must be an object", providerID)
		}
		for _, id := range sortedObjectKeys(overrides) {
			override, ok := overrides[id].(map[string]any)
			if !ok {
				return fmt.Errorf("providers.%s.modelOverrides.%s must be an object", providerID, id)
			}
			if err := validateModelOverride(providerID, id, override); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateModel(providerID, modelID string, model map[string]any) error {
	return validateModelWithCostPolicy(providerID, modelID, model, true)
}

func validateModelOverride(providerID, modelID string, model map[string]any) error {
	return validateModelWithCostPolicy(providerID, modelID, model, false)
}

func validateModelWithCostPolicy(providerID, modelID string, model map[string]any, requireCost bool) error {
	path := modelPath(providerID, modelID)
	for _, key := range []string{"name", "api", "baseUrl"} {
		if value, exists := model[key]; exists {
			text, ok := value.(string)
			if !ok || strings.TrimSpace(text) == "" {
				return fmt.Errorf("%s.%s must be a non-empty string", path, key)
			}
		}
	}
	if value, exists := model["reasoning"]; exists {
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s.reasoning must be a boolean", path)
		}
	}
	if value, exists := model["contextWindow"]; exists && !positiveJSONNumber(value) {
		return fmt.Errorf("%s.contextWindow must be greater than zero", path)
	}
	if value, exists := model["maxTokens"]; exists && !positiveJSONNumber(value) {
		return fmt.Errorf("%s.maxTokens must be greater than zero", path)
	}
	if value, exists := model["input"]; exists {
		input, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s.input must be an array", path)
		}
		for _, raw := range input {
			modality, ok := raw.(string)
			if !ok || (modality != "text" && modality != "image") {
				return fmt.Errorf("%s.input values must be text or image", path)
			}
		}
	}
	for _, key := range []string{"samplingParams", "compat"} {
		if value, exists := model[key]; exists {
			if _, ok := value.(map[string]any); !ok {
				return fmt.Errorf("%s.%s must be an object", path, key)
			}
		}
	}
	if value, exists := model["thinkingLevelMap"]; exists {
		if err := validateThinkingLevelMap(value, path+".thinkingLevelMap"); err != nil {
			return err
		}
	}
	if value, exists := model["inputLimits"]; exists {
		if err := validateInputLimits(value, path+".inputLimits"); err != nil {
			return err
		}
	}
	if value, exists := model["cost"]; exists {
		if err := validateModelCost(value, path+".cost", requireCost); err != nil {
			return err
		}
	}
	if value, exists := model["promptCache"]; exists {
		if err := validatePromptCache(value, path+".promptCache"); err != nil {
			return err
		}
	}
	if value, exists := model["samplingParamsByThinkingLevel"]; exists {
		if err := validateSamplingParamsByThinkingLevel(value, path+".samplingParamsByThinkingLevel"); err != nil {
			return err
		}
	}
	if err := validateHeaders(model["headers"], path+".headers"); err != nil {
		return err
	}
	return nil
}

func validateThinkingLevelMap(value any, path string) error {
	levels, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("%s must be an object", path)
	}
	for _, level := range []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"} {
		if raw, exists := levels[level]; exists && raw != nil {
			if _, ok := raw.(string); !ok {
				return fmt.Errorf("%s.%s must be a string or null", path, level)
			}
		}
	}
	return nil
}

func validateInputLimits(value any, path string) error {
	limits, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("%s must be an object", path)
	}
	if raw, exists := limits["maxRequestBytes"]; exists {
		if !positiveJSONInteger(raw) {
			return fmt.Errorf("%s.maxRequestBytes must be a positive integer", path)
		}
	}
	rawImages, exists := limits["images"]
	if !exists {
		return nil
	}
	images, ok := rawImages.(map[string]any)
	if !ok {
		return fmt.Errorf("%s.images must be an object", path)
	}
	for _, key := range []string{"maxPerMessage", "maxPerRequest"} {
		if raw, exists := images[key]; exists && !positiveJSONInteger(raw) {
			return fmt.Errorf("%s.images.%s must be a positive integer", path, key)
		}
	}
	rawResize, exists := images["resize"]
	if !exists {
		return nil
	}
	resize, ok := rawResize.(map[string]any)
	if !ok {
		return fmt.Errorf("%s.images.resize must be an object", path)
	}
	for _, key := range []string{"maxWidth", "maxHeight", "maxBytes"} {
		if raw, exists := resize[key]; exists && !positiveJSONInteger(raw) {
			return fmt.Errorf("%s.images.resize.%s must be a positive integer", path, key)
		}
	}
	if raw, exists := resize["jpegQuality"]; exists {
		quality, ok := positiveJSONIntegerValue(raw)
		if !ok || quality > 100 {
			return fmt.Errorf("%s.images.resize.jpegQuality must be an integer from 1 to 100", path)
		}
	}
	return nil
}

func validateModelCost(value any, path string, requireBaseFields bool) error {
	cost, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("%s must be an object", path)
	}
	for _, key := range []string{"input", "output", "cacheRead", "cacheWrite"} {
		raw, exists := cost[key]
		if requireBaseFields && !exists {
			return fmt.Errorf("%s.%s is required", path, key)
		}
		if exists && !isJSONNumber(raw) {
			return fmt.Errorf("%s.%s must be a number", path, key)
		}
	}
	if rawTiers, exists := cost["tiers"]; exists {
		tiers, ok := rawTiers.([]any)
		if !ok {
			return fmt.Errorf("%s.tiers must be an array", path)
		}
		for index, raw := range tiers {
			tier, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("%s.tiers[%d] must be an object", path, index)
			}
			for _, key := range []string{"inputTokensAbove", "input", "output", "cacheRead", "cacheWrite"} {
				if _, exists := tier[key]; !exists {
					return fmt.Errorf("%s.tiers[%d].%s is required", path, index, key)
				}
				if !isJSONNumber(tier[key]) {
					return fmt.Errorf("%s.tiers[%d].%s must be a number", path, index, key)
				}
			}
		}
	}
	return nil
}

func validatePromptCache(value any, path string) error {
	cache, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("%s must be an object", path)
	}
	for _, key := range []string{"short", "long"} {
		if raw, exists := cache[key]; exists && !positiveJSONNumber(raw) {
			return fmt.Errorf("%s.%s must be greater than zero", path, key)
		}
	}
	return nil
}

func validateSamplingParamsByThinkingLevel(value any, path string) error {
	levels, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("%s must be an object", path)
	}
	for _, level := range []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"} {
		if raw, exists := levels[level]; exists {
			if _, ok := raw.(map[string]any); !ok {
				return fmt.Errorf("%s.%s must be an object", path, level)
			}
		}
	}
	return nil
}

func validateHeaders(value any, path string) error {
	if value == nil {
		return nil
	}
	headers, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("%s must be an object", path)
	}
	for _, name := range sortedObjectKeys(headers) {
		if _, ok := headers[name].(string); !ok {
			return fmt.Errorf("%s.%s must be a string", path, name)
		}
	}
	return nil
}

func providersObject(document map[string]any) (map[string]any, error) {
	value, exists := document["providers"]
	if !exists {
		return nil, errors.New("providers is required")
	}
	providers, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("providers must be an object")
	}
	return providers, nil
}

func ensureProvidersObject(document map[string]any) (map[string]any, error) {
	if document == nil {
		return nil, errors.New("models.json root must be an object")
	}
	value, exists := document["providers"]
	if !exists {
		providers := make(map[string]any)
		document["providers"] = providers
		return providers, nil
	}
	providers, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("providers must be an object")
	}
	return providers, nil
}

func cloneJSONObject(source map[string]any) map[string]any {
	if source == nil {
		return make(map[string]any)
	}
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = cloneJSONValue(value)
	}
	return clone
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneJSONObject(typed)
	case []any:
		clone := make([]any, len(typed))
		for i, item := range typed {
			clone[i] = cloneJSONValue(item)
		}
		return clone
	default:
		return typed
	}
}

func uniqueNonEmptyIDs(ids []string, label string) ([]string, error) {
	result := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" {
			return nil, fmt.Errorf("%s must not be empty", label)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, id)
	}
	return result, nil
}

func writePiModelsAtomic(path string, document map[string]any) error {
	_, err := writePiModelsAtomicVersion(path, document, "", nil)
	return err
}

func writePiModelsAtomicVersion(path string, document map[string]any, expectedDigest string, beforeRename func() error) (string, error) {
	if err := validateModelsDocument(document); err != nil {
		return "", fmt.Errorf("validate models.json: %w", err)
	}
	data, err := encodePiModelsDocument(document)
	if err != nil {
		return "", fmt.Errorf("encode models.json: %w", err)
	}
	writePath, err := resolvePiModelsWritePath(path)
	if err != nil {
		return "", err
	}
	directory := filepath.Dir(writePath)
	info, statErr := os.Stat(writePath)
	exists := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return "", fmt.Errorf("inspect models.json: %w", statErr)
	}
	if expectedDigest != "" {
		actual := channelMissingDigest
		if exists {
			current, _, readErr := readChannelFile(writePath, false)
			if readErr != nil {
				return "", fmt.Errorf("read models.json before replace: %w", readErr)
			}
			actual = digestBytes(current)
		}
		if actual != expectedDigest {
			return "", &ChannelPreviewStaleError{Code: channelErrorPreviewStale, Path: writePath, Expected: expectedDigest, Actual: actual}
		}
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create Pi agent directory: %w", err)
	}
	temp, err := os.CreateTemp(directory, ".models-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temporary models.json: %w", err)
	}
	tempName := temp.Name()
	remove := true
	defer func() {
		_ = temp.Close()
		if remove {
			_ = os.Remove(tempName)
		}
	}()
	mode := os.FileMode(0o600)
	if exists {
		mode = info.Mode().Perm()
	}
	if err := temp.Chmod(mode); err != nil {
		return "", fmt.Errorf("set temporary models.json mode: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		return "", fmt.Errorf("write temporary models.json: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return "", fmt.Errorf("sync temporary models.json: %w", err)
	}
	if err := temp.Close(); err != nil {
		return "", fmt.Errorf("close temporary models.json: %w", err)
	}
	if expectedDigest != "" {
		actual := channelMissingDigest
		if _, err := os.Stat(writePath); err == nil {
			current, _, readErr := readChannelFile(writePath, false)
			if readErr != nil {
				return "", fmt.Errorf("verify models.json before replace: %w", readErr)
			}
			actual = digestBytes(current)
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("verify models.json before replace: %w", err)
		}
		if actual != expectedDigest {
			return "", &ChannelPreviewStaleError{Code: channelErrorPreviewStale, Path: writePath, Expected: expectedDigest, Actual: actual}
		}
	}
	if beforeRename != nil {
		if err := beforeRename(); err != nil {
			return "", err
		}
	}
	if err := os.Rename(tempName, writePath); err != nil {
		return "", fmt.Errorf("replace models.json: %w", err)
	}
	remove = false
	if dir, err := os.Open(directory); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return digestBytes(data), nil
}

func encodePiModelsDocument(document map[string]any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func resolvePiModelsWritePath(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("resolve models.json symlink: %w", err)
	}
	if info, lstatErr := os.Lstat(path); lstatErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("resolve models.json symlink: %w", err)
	} else if lstatErr != nil && !errors.Is(lstatErr, os.ErrNotExist) {
		return "", fmt.Errorf("inspect models.json: %w", lstatErr)
	}
	directory, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err == nil {
		return filepath.Join(directory, filepath.Base(path)), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return path, nil
	}
	return "", fmt.Errorf("resolve models.json directory: %w", err)
}

func isPiEnvironmentReference(value string) bool {
	if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") && len(value) > 3 {
		return validEnvironmentName(value[2 : len(value)-1])
	}
	if strings.HasPrefix(value, "$") && len(value) > 1 {
		return validEnvironmentName(value[1:])
	}
	return false
}

func validEnvironmentName(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		ch := value[index]
		if index == 0 {
			if ch != '_' && !isASCIIAlpha(ch) {
				return false
			}
			continue
		}
		if ch != '_' && !isASCIIAlpha(ch) && (ch < '0' || ch > '9') {
			return false
		}
	}
	return true
}

func isASCIIAlpha(ch byte) bool {
	return (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z')
}

func isJSONNumber(value any) bool {
	switch number := value.(type) {
	case json.Number:
		_, err := number.Float64()
		return err == nil
	case float64:
		return true
	default:
		return false
	}
}

func positiveJSONIntegerValue(value any) (int64, bool) {
	var parsed int64
	switch number := value.(type) {
	case json.Number:
		integer, err := number.Int64()
		if err != nil {
			return 0, false
		}
		parsed = integer
	case float64:
		parsed = int64(number)
		if float64(parsed) != number {
			return 0, false
		}
	default:
		return 0, false
	}
	return parsed, parsed > 0
}

func positiveJSONInteger(value any) bool {
	_, ok := positiveJSONIntegerValue(value)
	return ok
}

func positiveJSONNumber(value any) bool {
	switch number := value.(type) {
	case json.Number:
		parsed, err := number.Float64()
		return err == nil && parsed > 0
	case float64:
		return number > 0
	default:
		return false
	}
}

func numberPointer(value any) *float64 {
	switch number := value.(type) {
	case json.Number:
		parsed, err := number.Float64()
		if err == nil {
			return &parsed
		}
	case float64:
		return &number
	}
	return nil
}

func channelStringValue(value any) string {
	text, _ := value.(string)
	return text
}

func channelBoolValue(value any) bool {
	boolean, _ := value.(bool)
	return boolean
}

func sortedObjectKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func providerPath(providerID string) string {
	return "providers." + providerID
}

func modelPath(providerID, modelID string) string {
	return providerPath(providerID) + ".models." + modelID
}

func overridePath(providerID, modelID string) string {
	return providerPath(providerID) + ".modelOverrides." + modelID
}
