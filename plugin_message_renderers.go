package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// Plugin message renderers turn one closed code block into a native view
// through the existing worker protocol. The result is cached by plugin scan
// sequence and content hash, so a render frame only ever reads the cache and
// never starts a process. Plain code stays on screen while a render is pending
// and whenever one fails, so the original content remains usable.

type pluginRendererState uint8

const (
	pluginRendererPending pluginRendererState = iota
	pluginRendererReady
	pluginRendererFailed
)

// pluginRendererResult is one cached render. generation is the declaring
// plugin's renderer generation at the time the render was made, so restarting,
// disabling, or updating that plugin makes its own entries stale while another
// plugin's cache stays valid.
type pluginRendererResult struct {
	state      pluginRendererState
	pluginID   string
	generation uint64
	// request identifies the exact call that wrote this entry, so a reply
	// from an older call can never overwrite or clear a newer same-key
	// request after a release or invalidation started one.
	request uint64
	model   pluginViewModel
}

// pluginMessageRendererFor returns the first registered renderer that declared
// this code-block language. Entries are sorted by plugin id, so the winner is
// stable across rescans. A renderer that declared only MIME types matches
// attachments, not a code fence, so it is not a candidate here.
func (registry pluginRegistry) messageRendererForLanguage(
	language string,
) (pluginRegistryEntry, pluginMessageRendererContribution, bool) {
	language = strings.ToLower(strings.TrimSpace(language))
	if language == "" {
		return pluginRegistryEntry{}, pluginMessageRendererContribution{}, false
	}
	for _, entry := range registry.Entries {
		if entry.Status != pluginRegistered || entry.Manifest == nil {
			continue
		}
		for _, contribution := range entry.Manifest.Contributes.MessageRenderers {
			for _, declared := range contribution.Languages {
				if strings.EqualFold(strings.TrimSpace(declared), language) {
					return entry, contribution, true
				}
			}
		}
	}
	return pluginRegistryEntry{}, pluginMessageRendererContribution{}, false
}

// messageRendererForMimeType returns the first registered renderer that
// declared this attachment MIME type. It is the attachment-side counterpart of
// messageRendererForLanguage: a code fence never matches here and an
// attachment never matches there, so one renderer can serve either surface.
func (registry pluginRegistry) messageRendererForMimeType(
	mimeType string,
) (pluginRegistryEntry, pluginMessageRendererContribution, bool) {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if mimeType == "" {
		return pluginRegistryEntry{}, pluginMessageRendererContribution{}, false
	}
	for _, entry := range registry.Entries {
		if entry.Status != pluginRegistered || entry.Manifest == nil {
			continue
		}
		for _, contribution := range entry.Manifest.Contributes.MessageRenderers {
			for _, declared := range contribution.MimeTypes {
				if strings.EqualFold(strings.TrimSpace(declared), mimeType) {
					return entry, contribution, true
				}
			}
		}
	}
	return pluginRegistryEntry{}, pluginMessageRendererContribution{}, false
}

// pluginRendererCacheKey is the one render a cache entry stands for. It names
// the declaring plugin and contribution first, so when a different plugin takes
// over a language or MIME type it gets its own key even at the same generation,
// and a pending or failed entry left by the old owner cannot hold the new one on
// a plain fallback. The discriminator distinguishes a code fence (its language)
// from an attachment (its MIME type); content is the fence body or the
// attachment name and size, since a sent attachment keeps only metadata.
func pluginRendererCacheKey(
	pluginID, contributionID, discriminator, content string,
) string {
	sum := sha256.Sum256([]byte(pluginID + "\x00" + contributionID + "\x00" +
		strings.ToLower(strings.TrimSpace(discriminator)) + "\x00" + content))
	return hex.EncodeToString(sum[:])
}

// pluginCodeBlock renders one fenced code block. A plugin that declared this
// language and whose render is ready supplies the view; otherwise the block
// keeps its own highlighted body, which is also what the user sees while a
// render is running or after one failed.
func (s *appState) pluginCodeBlock(
	c *ui.Context,
	p palette,
	block *markdown.CodeBlockView,
	plain func(),
) {
	// A block that is still being written has no closing fence yet, so heavy
	// renderers wait for it to close.
	if block.Open {
		plain()
		return
	}
	code := block.Code()
	entry, contribution, ok := s.pluginRegistrySnapshot().
		messageRendererForLanguage(block.Language)
	if !ok || len(code) == 0 || len(code) > maxPluginViewArgsBytes {
		plain()
		return
	}
	key := pluginRendererCacheKey(entry.ID, contribution.ID, block.Language, code)
	generation := s.pluginRendererGeneration[entry.ID]
	if result, has := s.pluginRendererResults[key]; has && result.generation == generation {
		if result.state == pluginRendererReady && result.pluginID == entry.ID {
			s.pluginViewBlocks(c, p, entry, result.model.Blocks)
			return
		}
		// Pending or failed keeps the plain block, and a task is already in
		// flight or already reported, so nothing is scheduled again.
		plain()
		return
	}
	s.schedulePluginRenderer(key, entry, contribution, block.Language, code, generation)
	plain()
}

// pluginAttachmentChip renders one sent-attachment chip through a plugin that
// declared its MIME type, and falls back to the plain chip while a render is
// pending, after one failed, and for any attachment no renderer claims. This is
// the mimeTypes consumer: it is reached from the transcript's attachment row, so
// a MIME-only renderer has a real call path and not just a manifest field.
func (s *appState) pluginAttachmentChip(
	c *ui.Context,
	p palette,
	meta chatAttachmentMeta,
	plain func(),
) {
	mimeType := strings.ToLower(strings.TrimSpace(meta.MimeType))
	if mimeType == "" {
		mimeType = chatMimeTypeForPath(meta.Path)
	}
	if mimeType == "" {
		plain()
		return
	}
	entry, contribution, ok := s.pluginRegistrySnapshot().
		messageRendererForMimeType(mimeType)
	if !ok {
		plain()
		return
	}
	// A renderer is asked about one file, so the cache key needs the file's
	// identity and not only its name and size: restored history metas all carry
	// size 0, so two references with the same base name in different folders
	// would otherwise share one entry and the second chip would show the render
	// made for the first. A meta with no path has no file identity to key on, so
	// it keeps the plain chip instead of guessing (review R1).
	identity, hasIdentity := pluginAttachmentIdentity(meta)
	if !hasIdentity {
		plain()
		return
	}
	content := identity + "\x00" + meta.Name + "\x00" + strconv.FormatInt(meta.Size, 10)
	cacheKey := pluginRendererCacheKey(entry.ID, contribution.ID, mimeType, content)
	generation := s.pluginRendererGeneration[entry.ID]
	if result, has := s.pluginRendererResults[cacheKey]; has && result.generation == generation {
		if result.state == pluginRendererReady && result.pluginID == entry.ID {
			s.pluginViewBlocks(c, p, entry, result.model.Blocks)
			return
		}
		plain()
		return
	}
	s.schedulePluginAttachmentRender(cacheKey, entry, contribution, generation, meta, mimeType)
	plain()
}

// pluginRegistryEntryByID finds one entry of a registry snapshot, which is how a
// late host-call reply checks what the plugin declares now.
func pluginRegistryEntryByID(registry pluginRegistry, pluginID string) (pluginRegistryEntry, bool) {
	for _, entry := range registry.Entries {
		if entry.ID == pluginID {
			return entry, true
		}
	}
	return pluginRegistryEntry{}, false
}

// pluginAttachmentIdentity is the file a chip stands for, cleaned and absolute so
// the same file reached by two spellings keeps one cache entry. Path is a plain
// string operation here, so this stays safe to call from a render frame.
func pluginAttachmentIdentity(meta chatAttachmentMeta) (string, bool) {
	if strings.TrimSpace(meta.Path) == "" {
		return "", false
	}
	path := filepath.Clean(meta.Path)
	if !filepath.IsAbs(path) {
		if absolute, err := filepath.Abs(path); err == nil {
			path = absolute
		}
	}
	return path, true
}

// schedulePluginRenderer asks the declaring plugin once per key. The lease that
// invokeCommand takes keeps an idle worker alive for this call only.
func (s *appState) schedulePluginRenderer(
	key string,
	entry pluginRegistryEntry,
	contribution pluginMessageRendererContribution,
	language, code string,
	generation uint64,
) {
	s.schedulePluginRender(key, entry, contribution.Command, generation, map[string]any{
		"language": strings.ToLower(strings.TrimSpace(language)),
		"code":     code,
	})
}

// schedulePluginAttachmentRender asks the plugin that declared this attachment
// MIME type to render it. The transcript keeps only attachment metadata, so the
// payload carries that metadata plus the source path, which a plugin with file
// access can read itself. No bytes are read here, so the render frame stays free
// of IO.
func (s *appState) schedulePluginAttachmentRender(
	key string,
	entry pluginRegistryEntry,
	contribution pluginMessageRendererContribution,
	generation uint64,
	meta chatAttachmentMeta,
	mimeType string,
) {
	s.schedulePluginRender(key, entry, contribution.Command, generation, map[string]any{
		"mimeType": mimeType,
		"name":     meta.Name,
		"size":     meta.Size,
		"path":     meta.Path,
	})
}

// schedulePluginRender is the one place a render is started. The pending map
// keeps at most one call per key and holds its cancel, so a release can stop the
// worker call rather than only ignore it. A stored result carries the request
// that wrote it, so a reply from an older call cannot change state a newer call
// already owns for the same key.
func (s *appState) schedulePluginRender(
	key string,
	entry pluginRegistryEntry,
	command string,
	generation uint64,
	args map[string]any,
) {
	if s.pluginRendererResults == nil {
		s.pluginRendererResults = make(map[string]pluginRendererResult)
	}
	if s.pendingPluginRenderers == nil {
		s.pendingPluginRenderers = make(map[string]context.CancelFunc)
	}
	if _, pending := s.pendingPluginRenderers[key]; pending {
		return
	}
	pluginID := entry.ID
	s.pluginRendererRequest++
	request := s.pluginRendererRequest
	s.pluginRendererResults[key] = pluginRendererResult{
		state:      pluginRendererPending,
		pluginID:   pluginID,
		generation: generation,
		request:    request,
	}
	ctx, cancel := context.WithTimeout(context.Background(),
		pluginLifecycleTimeout+pluginCommandTimeout)
	s.pendingPluginRenderers[key] = cancel
	go func() {
		defer cancel()
		raw, err := s.plugins.invokeCommand(ctx, entry, command, args)
		model := pluginViewModel{}
		if err == nil {
			model, err = decodePluginViewModel(raw)
		}
		s.postUI(func() {
			current, has := s.pluginRendererResults[key]
			// A release, invalidation, or newer request owns the key now, so
			// this reply is stale and must not touch either map.
			if !has || current.request != request {
				return
			}
			delete(s.pendingPluginRenderers, key)
			// The plugin was restarted, disabled, or removed while this call
			// ran, so its result is dropped and nothing is stored for it.
			if generation != s.pluginRendererGeneration[pluginID] {
				delete(s.pluginRendererResults, key)
				return
			}
			if err != nil || len(model.Blocks) == 0 {
				s.pluginRendererResults[key] = pluginRendererResult{
					state:      pluginRendererFailed,
					pluginID:   pluginID,
					generation: generation,
					request:    request,
				}
				return
			}
			s.pluginRendererResults[key] = pluginRendererResult{
				state:      pluginRendererReady,
				pluginID:   pluginID,
				generation: generation,
				request:    request,
				model:      model,
			}
		})
	}()
}

// pluginRendererSignature identifies what one plugin declares as renderers, so
// an updated package is detected without comparing whole manifests.
func pluginRendererSignature(manifest *pluginManifest) string {
	if manifest == nil {
		return ""
	}
	parts := make([]string, 0, len(manifest.Contributes.MessageRenderers))
	for _, contribution := range manifest.Contributes.MessageRenderers {
		parts = append(parts, strings.Join([]string{
			contribution.ID, contribution.Command,
			strings.Join(contribution.Languages, ","),
			strings.Join(contribution.MimeTypes, ","),
		}, "|"))
	}
	return strings.Join(parts, ";")
}

// prunePluginRenderers releases the cached renders of a plugin that is no
// longer registered or whose declaration changed, which covers removal, an
// invalid manifest, a disable, and an updated package. Invalidation stays per
// plugin, so one plugin's churn never flushes another plugin's renders or wakes
// its worker.
func (s *appState) prunePluginRenderers() {
	registered := make(map[string]bool)
	signatures := make(map[string]string)
	for _, entry := range s.pluginRegistrySnapshot().Entries {
		if entry.Status == pluginRegistered && entry.Manifest != nil &&
			len(entry.Manifest.Contributes.MessageRenderers) > 0 {
			registered[entry.ID] = true
			signatures[entry.ID] = pluginRendererSignature(entry.Manifest)
		}
	}
	for pluginID, signature := range signatures {
		if previous, had := s.pluginRendererSignatures[pluginID]; had && previous != signature {
			s.invalidatePluginRenderers(pluginID)
		}
	}
	for pluginID := range s.pluginRendererSignatures {
		if !registered[pluginID] {
			s.invalidatePluginRenderers(pluginID)
			delete(s.pluginRendererSignatures, pluginID)
		}
	}
	s.pluginRendererSignatures = signatures
}

// invalidatePluginRenderers releases one plugin's cached renders and rejects
// anything that plugin still has in flight, so a restart, disable, or updated
// package cannot show an old result. Other plugins keep their caches, which is
// what keeps their workers idle.
func (s *appState) invalidatePluginRenderers(pluginID string) {
	if s.pluginRendererGeneration == nil {
		s.pluginRendererGeneration = make(map[string]uint64)
	}
	s.pluginRendererGeneration[pluginID]++
	for key, result := range s.pluginRendererResults {
		if result.pluginID == pluginID {
			delete(s.pluginRendererResults, key)
			s.cancelPluginRender(key)
		}
	}
}

// releasePluginRenderers drops every cached render and stops every call in
// flight. The transcript is replaced wholesale (a new session, a switch, or a
// clear), so none of the old blocks or attachments are on screen any more and
// keeping their renders would let the cache grow across sessions. Per-plugin
// generations and signatures stay, because the plugins themselves did not
// change, so a renderer that is still installed fills the cache again on the
// next transcript that uses it.
func (s *appState) releasePluginRenderers() {
	s.pluginRendererResults = make(map[string]pluginRendererResult)
	for key := range s.pendingPluginRenderers {
		s.cancelPluginRender(key)
	}
}

// cancelPluginRender stops one in-flight render and forgets it, so its reply
// finds no owner and is dropped.
func (s *appState) cancelPluginRender(key string) {
	if cancel, has := s.pendingPluginRenderers[key]; has {
		cancel()
		delete(s.pendingPluginRenderers, key)
	}
}
