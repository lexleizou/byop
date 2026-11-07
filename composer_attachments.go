package main

import (
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	lu "github.com/oneo-me/onevra/source/icons/lucide"
)

// Composer attachments follow the installed Pi RPC: supported images travel in
// the `images` array of prompt/steer/follow_up, and every other file is handed
// to Pi's tools as an explicit local path in the message text. Nothing that
// the user picked or dropped is dropped silently: an item that cannot ride as
// an image stays a path reference, and an item that cannot be read at all is
// reported.
const (
	// maxInlineImageBytes bounds one image read into the RPC payload.
	maxInlineImageBytes = 8 << 20
	// maxComposerAttachments bounds how many items one message carries.
	maxComposerAttachments = 8
	// attachmentBlockStart and attachmentBlockEnd wrap the path references so
	// the transcript can strip them without depending on translated text.
	attachmentBlockStart = "[byop-attachments]"
	attachmentBlockEnd   = "[/byop-attachments]"
)

type composerAttachmentKind string

const (
	attachmentImage composerAttachmentKind = "image"
	attachmentFile  composerAttachmentKind = "file"
)

// composerAttachment is one selected item. data holds image bytes only, and
// only while the item sits in the composer: the timeline keeps metadata, so
// history rows never retain image blobs.
type composerAttachment struct {
	path     string
	name     string
	kind     composerAttachmentKind
	mimeType string
	size     int64
	data     []byte
}

// chatAttachmentMeta is what a sent message keeps for the transcript.
type chatAttachmentMeta struct {
	Name     string
	Kind     composerAttachmentKind
	MimeType string
	Size     int64
	// Path is kept so Retry can add the file again. It is never displayed;
	// the timeline shows Name only.
	Path string
}

func (a composerAttachment) meta() chatAttachmentMeta {
	return chatAttachmentMeta{Name: a.name, Kind: a.kind, MimeType: a.mimeType, Size: a.size,
		Path: a.path}
}

func chatAttachmentMetas(attachments []composerAttachment) []chatAttachmentMeta {
	if len(attachments) == 0 {
		return nil
	}
	metas := make([]chatAttachmentMeta, 0, len(attachments))
	for _, attachment := range attachments {
		metas = append(metas, attachment.meta())
	}
	return metas
}

func cloneComposerAttachments(attachments []composerAttachment) []composerAttachment {
	if len(attachments) == 0 {
		return nil
	}
	cloned := make([]composerAttachment, len(attachments))
	copy(cloned, attachments)
	return cloned
}

func sameComposerAttachments(left, right []composerAttachment) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].path != right[index].path {
			return false
		}
	}
	return true
}

// chatImageMime reports the image MIME type an extension stands for, and
// whether the type is one the RPC images array carries. Other kinds, including
// binary documents, stay path references, which Pi's tools can read.
func chatImageMime(path string) (string, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png", true
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".gif":
		return "image/gif", true
	case ".webp":
		return "image/webp", true
	}
	return "", false
}

// chatMimeTypeForPath gives a transcript attachment a MIME type to match a
// renderer against, since a path reference keeps only its path, not a declared
// type. Image types reuse the same map the composer uses, so what matched on
// send still matches in the timeline; other common text and document types come
// from the system extension map. An unknown extension means no type, which
// leaves the attachment on the plain chip.
func chatMimeTypeForPath(path string) string {
	if path == "" {
		return ""
	}
	if mimeType, ok := chatImageMime(path); ok {
		return mimeType
	}
	if mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path))); mimeType != "" {
		if semicolon := strings.IndexByte(mimeType, ';'); semicolon >= 0 {
			mimeType = mimeType[:semicolon]
		}
		return strings.ToLower(mimeType)
	}
	return ""
}

// activeModelAcceptsImages follows the model capability Pi reports. A model
// that declares its input modalities without image is text-only; a model whose
// modalities are unknown is treated as able, leaving the decision to Pi.
func (s *appState) activeModelAcceptsImages() bool {
	for _, model := range s.piModels {
		if model.Provider != s.piModelProvider || model.ID != s.piModelID {
			continue
		}
		if len(model.Input) == 0 {
			return true
		}
		for _, modality := range model.Input {
			if modality == "image" {
				return true
			}
		}
		return false
	}
	return true
}

// attachmentPromptText returns the user text plus the path references of the
// items that do not ride as images. The block is ASCII-bracketed so the
// transcript can strip it while the chips stay visible.
func attachmentPromptText(userText string, attachments []composerAttachment) string {
	paths := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		if attachment.kind == attachmentFile {
			paths = append(paths, attachment.path)
		}
	}
	if len(paths) == 0 {
		return userText
	}
	lines := make([]string, 0, len(paths)+2)
	lines = append(lines, attachmentBlockStart)
	lines = append(lines, paths...)
	lines = append(lines, attachmentBlockEnd)
	if strings.TrimSpace(userText) == "" {
		return strings.Join(lines, "\n")
	}
	return strings.TrimRight(userText, "\n") + "\n\n" + strings.Join(lines, "\n")
}

// splitAttachmentBlock separates a prompt into the text shown in the timeline
// and the path references of the attachment block, so both the display path
// and the restore paths (delayed echo, Pi history) read the same marker and
// agree on file names. One block per prompt is sent; a stray marker line
// without its partner is kept as text.
func splitAttachmentBlock(prompt string) (display string, paths []string) {
	if !strings.Contains(prompt, attachmentBlockStart) {
		return prompt, nil
	}
	kept := make([]string, 0, strings.Count(prompt, "\n")+1)
	inBlock := false
	blockClosed := false
	for _, line := range strings.Split(prompt, "\n") {
		switch strings.TrimSpace(line) {
		case attachmentBlockStart:
			inBlock = true
			continue
		case attachmentBlockEnd:
			inBlock = false
			blockClosed = true
			// The blank line that separated the text from the block goes with it.
			for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
				kept = kept[:len(kept)-1]
			}
			continue
		}
		if inBlock {
			if path := strings.TrimSpace(line); path != "" {
				paths = append(paths, path)
			}
			continue
		}
		kept = append(kept, line)
	}
	if !blockClosed {
		return prompt, nil
	}
	return strings.TrimRight(strings.Join(kept, "\n"), "\n"), paths
}

// displayAttachmentPrompt removes an attachment block from prompt text, which
// is how the transcript shows the message with its chips instead of paths.
func displayAttachmentPrompt(prompt string) string {
	display, _ := splitAttachmentBlock(prompt)
	return display
}

// attachmentMetasFromPaths rebuilds the timeline metas of a message from the
// path references its prompt carried. Pi keeps no image names for images that
// rode in the payload, so file references restore with their real names and
// the caller adds image metas where it can see the image parts.
func attachmentMetasFromPaths(paths []string) []chatAttachmentMeta {
	if len(paths) == 0 {
		return nil
	}
	metas := make([]chatAttachmentMeta, 0, len(paths))
	for _, path := range paths {
		metas = append(metas, chatAttachmentMeta{
			Name: filepath.Base(path),
			Path: path,
			Kind: attachmentFile,
		})
	}
	return metas
}

// sendAttachments decides what one send carries. Images ride as payload only
// while the active model accepts image input; if it does not, an image stays a
// path reference so the file is still usable and nothing is dropped in silence.
func (s *appState) sendAttachments(attachments []composerAttachment) []composerAttachment {
	if len(attachments) == 0 || s.activeModelAcceptsImages() {
		return attachments
	}
	var notified bool
	converted := make([]composerAttachment, 0, len(attachments))
	for _, attachment := range attachments {
		if attachment.kind == attachmentImage {
			attachment.kind = attachmentFile
			attachment.data = nil
			attachment.mimeType = ""
			if !notified {
				notified = true
				s.showNotice(text("chatImageUnsupported"))
			}
		}
		converted = append(converted, attachment)
	}
	return converted
}

// attachmentImages returns the RPC payload for the images of a send.
func attachmentImages(attachments []composerAttachment) []piImage {
	images := make([]piImage, 0, len(attachments))
	for _, attachment := range attachments {
		if attachment.kind != attachmentImage || len(attachment.data) == 0 {
			continue
		}
		images = append(images, piImage{
			Type:     "image",
			Data:     base64.StdEncoding.EncodeToString(attachment.data),
			MimeType: attachment.mimeType,
		})
	}
	if len(images) == 0 {
		return nil
	}
	return images
}

// composerAttachmentPicker shows the native file picker. Tests install a fake.
type composerAttachmentPicker func(parent *mygo.Window, title string) ([]string, error)

func defaultComposerAttachmentPicker(parent *mygo.Window, title string) ([]string, error) {
	return mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:   parent,
		Title:    title,
		Multiple: true,
	})
}

func (s *appState) composerAttachmentPicker() composerAttachmentPicker {
	if s.composerAttachmentsPickerOverride != nil {
		return s.composerAttachmentsPickerOverride
	}
	return defaultComposerAttachmentPicker
}

// addComposerAttachmentPaths turns picked or dropped paths into selected
// items. Duplicates are ignored, directories and unreadable files are reported
// as skipped, and images become inline payloads only when the model accepts
// image input.
func (s *appState) addComposerAttachmentPaths(paths []string) {
	if len(paths) == 0 {
		return
	}
	acceptsImages := s.activeModelAcceptsImages()
	var skipped int
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if len(s.composerAttachments) >= maxComposerAttachments {
			skipped++
			continue
		}
		if s.hasComposerAttachment(path) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			skipped++
			continue
		}
		attachment := composerAttachment{
			path: path,
			name: filepath.Base(path),
			kind: attachmentFile,
			size: info.Size(),
		}
		if mimeType, ok := chatImageMime(path); ok {
			switch {
			case !acceptsImages:
				s.showNotice(text("chatImageUnsupported"))
			case info.Size() > maxInlineImageBytes:
				s.showNotice(textArgs("chatAttachmentTooLarge", map[string]any{
					"name":  attachment.name,
					"limit": formatAttachmentSize(maxInlineImageBytes),
				}))
			default:
				data, readErr := os.ReadFile(path)
				if readErr != nil {
					skipped++
					continue
				}
				attachment.kind = attachmentImage
				attachment.mimeType = mimeType
				attachment.data = data
			}
		}
		s.composerAttachments = append(s.composerAttachments, attachment)
	}
	if skipped > 0 {
		s.showNotice(textArgs("chatAttachmentSkipped", map[string]any{"count": skipped}))
	}
}

func (s *appState) hasComposerAttachment(path string) bool {
	for _, attachment := range s.composerAttachments {
		if attachment.path == path {
			return true
		}
	}
	return false
}

func (s *appState) removeComposerAttachment(path string) {
	kept := s.composerAttachments[:0]
	for _, attachment := range s.composerAttachments {
		if attachment.path != path {
			kept = append(kept, attachment)
		}
	}
	s.composerAttachments = kept
	if len(s.composerAttachments) == 0 {
		s.composerAttachments = nil
	}
}

// clearComposerAttachments removes exactly the items of a send, so files added
// while an asynchronous send was in flight stay selected.
func (s *appState) clearComposerAttachments(sent []composerAttachment) {
	if len(sent) == 0 {
		return
	}
	sentPaths := make(map[string]bool, len(sent))
	for _, attachment := range sent {
		sentPaths[attachment.path] = true
	}
	kept := make([]composerAttachment, 0, len(s.composerAttachments))
	for _, attachment := range s.composerAttachments {
		if !sentPaths[attachment.path] {
			kept = append(kept, attachment)
		}
	}
	if len(kept) == 0 {
		s.composerAttachments = nil
		return
	}
	s.composerAttachments = kept
}

// restoreComposerAttachments puts a failed send's items back in front of
// anything the user added since.
func (s *appState) restoreComposerAttachments(sent []composerAttachment) {
	if len(sent) == 0 {
		return
	}
	merged := make([]composerAttachment, 0, len(sent)+len(s.composerAttachments))
	seen := make(map[string]bool, len(sent)+len(s.composerAttachments))
	for _, attachment := range append(cloneComposerAttachments(sent), s.composerAttachments...) {
		if seen[attachment.path] {
			continue
		}
		seen[attachment.path] = true
		merged = append(merged, attachment)
	}
	if len(merged) > maxComposerAttachments {
		// A failed send must not silently cut items the user picked in the
		// meantime: the existing skip notice says how many did not come back.
		s.showNotice(textArgs("chatAttachmentSkipped",
			map[string]any{"count": len(merged) - maxComposerAttachments}))
		merged = merged[:maxComposerAttachments]
	}
	s.composerAttachments = merged
}

// beginComposerAttachmentPick takes the in-flight sheet flag on the UI thread.
func (s *appState) beginComposerAttachmentPick() bool {
	if s.composerAttachmentPicking {
		return false
	}
	s.composerAttachmentPicking = true
	return true
}

// pickComposerAttachments replaces the placeholder attachment button: it shows
// the native picker off the UI thread and selects what the user chose. Without
// a window, focused tests drive the picker synchronously.
func (s *appState) pickComposerAttachments() {
	if !s.beginComposerAttachmentPick() {
		return
	}
	picker := s.composerAttachmentPicker()
	parent := s.window
	title := text("chatAttachmentPickerTitle")
	if parent == nil {
		paths, err := picker(parent, title)
		s.composerAttachmentPicking = false
		s.applyComposerAttachmentPick(paths, err)
		return
	}
	go func() {
		paths, err := picker(parent, title)
		s.postUI(func() {
			s.composerAttachmentPicking = false
			s.applyComposerAttachmentPick(paths, err)
		})
	}()
}

// applyComposerAttachmentPick continues the original picker result on the UI
// thread. Cancelling leaves the current selection untouched.
func (s *appState) applyComposerAttachmentPick(paths []string, err error) {
	if err != nil {
		s.showNotice(uiErrorText(err, text("errorUnknown")))
		return
	}
	s.addComposerAttachmentPaths(paths)
}

// handleFileDrop takes files dropped on the window. Chat only: plugin views,
// settings, and automations leave a drop alone, and a drop on the composer card
// is taken by the card itself, so neither steals the other.
func (s *appState) handleFileDrop(event *mygo.FileDropEvent) {
	if event == nil || s.closed.Load() {
		return
	}
	if s.page != pageChat || !s.chatCapablePage() {
		return
	}
	s.addComposerAttachmentPaths(event.Paths)
}

func (s *appState) composerAttachmentChips(c *ui.Context, p palette) {
	if len(s.composerAttachments) == 0 {
		return
	}
	removeLabel := text("chatAttachmentRemove")
	for index, attachment := range s.composerAttachments {
		kindLabel := text("chatAttachmentFileLabel")
		icon := lu.FileText
		if attachment.kind == attachmentImage {
			kindLabel = text("chatAttachmentImageLabel")
			icon = lu.Image
		}
		row := ui.Row(c).Key(composerAttachmentChipKey(index, attachment)).
			Label(kindLabel+": "+attachment.name).
			Shrink(0).MaxWidth(260).Height(26).Padding(0, 4, 0, 8).
			Radius(v12ChipRadius).Border(v12BorderWidth, p.border).
			AlignItems(ui.Center).Gap(4).Tooltip(attachment.path).Children(func() {
			components.Icon(c, icon).Size(12, 12).TextColor(p.muted)
			components.Text(c, attachment.name).SingleLine().Ellipsis("…").
				FontSize(fontSmall).TextColor(p.muted).MinWidth(0).Shrink(1)
			remove := ui.ButtonBase(c.Key(composerAttachmentRemoveKey(attachment))).
				Size(18, 18).Center().Radius(v12ChipRadius).Background(ui.Color{}).
				TextColor(p.muted).Label(removeLabel)
			if remove.Hovered() || remove.Focused() {
				remove.Background(p.surfaceHover).TextColor(p.text)
			}
			remove.Children(func() {
				components.Icon(c, lu.X).Size(11, 11).TextColor(p.muted)
			})
			components.Tooltip(c, remove, removeLabel)
			if remove.Clicked() {
				s.removeComposerAttachment(attachment.path)
			}
		})
		_ = row
	}
}

func composerAttachmentChipKey(index int, attachment composerAttachment) string {
	return fmt.Sprintf("composer-attachment-%d-%s", index, attachment.name)
}

func composerAttachmentRemoveKey(attachment composerAttachment) string {
	return "remove-attachment-" + attachment.name
}

// chatAttachmentChipRow shows the items a sent message carried. It keeps names
// only, so history rows never hold image data, and it takes its colors from the
// frame theme so each appearance keeps its own tokens. A plugin that declared a
// matching mimeType replaces the chip once its render is ready (see
// pluginAttachmentChip); until then the plain chip stays, so a slow or failing
// renderer never removes the file from view. justify places the chips across
// the row (P31 §6: the BoardUI bubble end-aligns them; every other caller
// passes ui.Start, the previous behaviour).
func (s *appState) chatAttachmentChipRow(
	c *ui.Context,
	metas []chatAttachmentMeta,
	keyPrefix string,
	justify ui.Align,
) {
	if len(metas) == 0 {
		return
	}
	t := c.Theme()
	p := themePaletteForAppearance(
		s.currentAppearancePreset(),
		t.Dark,
		s.highContrastActive(currentAppearanceHighContrast()),
		currentAppearanceReduceTransparency(),
	)
	ui.Row(c).Key(keyPrefix).Label(text("chatFileReferencePrefix")).
		FillWidth().MinWidth(0).Wrap().Justify(justify).Gap(6).
		Padding(0, 0, 4, 0).
		AlignItems(ui.Center).Children(func() {
		for index, meta := range metas {
			kindLabel := text("chatAttachmentFileLabel")
			icon := lu.FileText
			if meta.Kind == attachmentImage {
				kindLabel = text("chatAttachmentImageLabel")
				icon = lu.Image
			}
			chipKey := fmt.Sprintf("%s-%d-%s", keyPrefix, index, meta.Name)
			s.pluginAttachmentChip(c, p, meta, func() {
				ui.Row(c).Key(chipKey).Label(kindLabel+": "+meta.Name).
					Shrink(0).MaxWidth(240).Height(24).Padding(0, 8).
					Radius(v12ChipRadius).Border(v12BorderWidth, t.Border).
					AlignItems(ui.Center).Gap(4).Children(func() {
					components.Icon(c, icon).Size(11, 11).TextColor(t.TextMuted)
					components.Text(c, meta.Name).SingleLine().Ellipsis("…").
						FontSize(fontCaption).TextColor(t.TextMuted).MinWidth(0).Shrink(1)
				})
			})
		}
	})
}

// formatAttachmentSize says a byte count in KiB/MiB for notices.
func formatAttachmentSize(bytes int64) string {
	const mib = int64(1 << 20)
	if bytes >= mib {
		if bytes%mib == 0 {
			return strconv.FormatInt(bytes/mib, 10) + " MiB"
		}
		return strconv.FormatInt((bytes+mib-1)/mib, 10) + " MiB"
	}
	return strconv.FormatInt(bytes/(1<<10), 10) + " KiB"
}
