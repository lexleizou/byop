package main

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui/markdown/parser"
	"github.com/oneo-me/onevra/source/components"
	lu "github.com/oneo-me/onevra/source/icons/lucide"

	"github.com/egoist/mygo/ui"
)

// chatFilePreviewHeight is the inline HTML preview height. Previewing runs the
// page in a web view inside the conversation, so one bounded height keeps the
// transcript readable.
const chatFilePreviewHeight float32 = 380

// maxChatFilePreviews bounds how many HTML previews are alive at once.
const maxChatFilePreviews = 6

var chatMarkdownLink = regexp.MustCompile(`^\[([^\]]*)\]\(([^()\n]+)\)$`)

type chatFilePreview struct {
	webView   *mygo.WebView
	failed    string
	lastFrame int
}

// chatLocalFileFromText reports the file a paragraph holds on its own: one
// plain path, one bare file URL, or one markdown link to a local file. Text
// that also carries other words stays as text so no prose disappears.
func chatLocalFileFromText(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" || strings.ContainsAny(value, "\n\r") {
		return "", false
	}
	if match := chatMarkdownLink.FindStringSubmatch(value); match != nil {
		value = strings.TrimSpace(match[2])
	}
	value = strings.TrimSuffix(strings.TrimPrefix(value, "<"), ">")
	value = strings.Trim(value, "\"'")
	value = strings.TrimRight(value, ".,;:!?、。）》」』")
	if value == "" {
		return "", false
	}
	path := value
	switch {
	case strings.HasPrefix(strings.ToLower(value), "file://"):
		parsed, err := url.Parse(value)
		if err != nil {
			return "", false
		}
		path = parsed.Path
		if parsed.Host != "" && parsed.Host != "localhost" {
			return "", false
		}
	default:
		if !chatLooksLikePath(value) {
			return "", false
		}
	}
	path = strings.TrimSpace(path)
	if decoded, err := url.PathUnescape(path); err == nil && decoded != "" {
		path = decoded
	}
	path = filepath.FromSlash(strings.TrimSpace(path))
	if !filepath.IsAbs(path) {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return path, true
}

// chatLooksLikePath keeps the file card for paths only, so a sentence that
// starts with a bracket or a slash in another language stays as text.
func chatLooksLikePath(value string) bool {
	if strings.ContainsAny(value, " \t") && !strings.Contains(value, string(filepath.Separator)) {
		return false
	}
	if strings.HasPrefix(value, "/") {
		return strings.Count(value, "/") >= 2
	}
	if len(value) >= 3 && value[1] == ':' {
		drive := value[0]
		return (drive >= 'A' && drive <= 'Z') || (drive >= 'a' && drive <= 'z')
	}
	return false
}

func chatFileIsHTML(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm":
		return true
	}
	return false
}

func (s *appState) chatFileCard(c *ui.Context, p palette, path string, latest bool) {
	name := filepath.Base(path)
	html := chatFileIsHTML(path)
	preview := s.chatFilePreviewOpen(path, latest)

	toggleKey := fallbackToggleKey(preview)
	card := ui.Column(c).
		FillWidth().MinWidth(0).Clip().Radius(8)
	card = styleRaisedSurface(card, p, 8)
	card.Children(func() {
		row := ui.Row(c).Key("chat-file-card-"+path).
			Label(textArgs("fileCardLabel", map[string]any{"name": name})).
			FillWidth().MinWidth(0).Padding(8, 10).Gap(10).
			AlignItems(ui.Center).Cursor(ui.CursorPointer).
			Tooltip(path).Children(func() {
			ui.Row(c).Size(28, 28).Shrink(0).Center().Radius(6).
				Background(p.text.Alpha(.07)).Children(func() {
				ui.Icon(c, icon("file")).Size(15, 15).TextColor(p.muted)
			})
			components.Text(c, name).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").
				Selectable().FontSize(fontBody).TextColor(p.text)
			if html {
				toggle := ui.ButtonBase(c.Key("chat-file-preview-"+path)).
					Height(26).Padding(0, 8).Radius(7).Shrink(0).Gap(6).
					Border(runtimeTokens.Dimension.Border.Value, p.border).
					Background(ui.Color{}).Cursor(ui.CursorPointer).
					Label(text(toggleKey))
				if toggle.Hovered() || toggle.Focused() {
					toggle.Background(p.surfaceHover)
				}
				toggle.Children(func() {
					ui.Icon(c, icon("activity")).Size(13, 13).TextColor(p.muted)
					ui.Text(c, text(toggleKey)).FontSize(fontSmall).
						FontWeight(600).TextColor(p.text)
				})
				if toggle.Clicked() {
					s.setChatFilePreview(path, !preview)
				}
			}
			ui.Row(c).Size(20, 20).Shrink(0).Center().Children(func() {
				components.Icon(c, lu.ChevronDown).Size(12, 12).TextColor(p.muted)
			})
		})
		s.chatFileMenu(c, row, path)
		if preview {
			s.chatFilePreview(c, p, path)
		}
	})
}

func fallbackToggleKey(open bool) string {
	if open {
		return "filePreviewHide"
	}
	return "filePreviewShow"
}

// chatFileMenu gives the same actions to a click and a right click: the file
// name stays short, so the destination is one menu away.
func (s *appState) chatFileMenu(c *ui.Context, row ui.Element, path string) {
	build := func(m *ui.Menu) {
		if m.Item(text("fileMenuOpen")).Chosen() {
			if err := mygo.Shell.OpenPath(path); err != nil {
				c.Toast(uiErrorText(err, text("fileOpenFailed")))
			}
		}
		if m.Item(text("fileMenuReveal")).Chosen() {
			mygo.Shell.ShowItemInFolder(path)
		}
		m.Separator()
		if m.Item(text("fileMenuCopyPath")).Chosen() {
			c.Services().WriteClipboard(path)
		}
	}
	row.Menu(build).ContextMenu(build)
}

func (s *appState) chatFilePreviewOpen(path string, latest bool) bool {
	if !chatFileIsHTML(path) {
		return false
	}
	if choice, ok := s.chatView.filePreviews[path]; ok {
		return choice
	}
	// The newest answer shows its own preview; older ones stay folded.
	return latest
}

func (s *appState) setChatFilePreview(path string, open bool) {
	if s.chatView.filePreviews == nil {
		s.chatView.filePreviews = map[string]bool{}
	}
	s.chatView.filePreviews[path] = open
	if s.window != nil {
		s.window.Invalidate()
	}
}

// chatFilePreview starts the web view on demand and shows it under the file
// row. Its frame number lets the transcript drop views it no longer builds.
func (s *appState) chatFilePreview(c *ui.Context, p palette, path string) {
	state := s.chatFilePreviewState(path)
	state.lastFrame = s.chatView.previewFrame
	if state.failed != "" {
		ui.Row(c).FillWidth().Padding(10).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon("info")).Size(14, 14).TextColor(p.warning)
			ui.Text(c, state.failed).FontSize(fontSmall).TextColor(p.muted)
		})
		return
	}
	if state.webView == nil {
		webview, err := s.newChatFileWebView(path, p)
		if err != nil {
			state.failed = uiErrorText(err, text("filePreviewFailed"))
			return
		}
		state.webView = webview
	}
	ui.Row(c).FillWidth().MinWidth(0).Height(chatFilePreviewHeight).
		BorderWidth(0, 0, runtimeTokens.Dimension.Border.Value, 0).
		BorderColor(p.border).Children(func() {
		ui.WebView(c, state.webView).FillWidth().Grow(1)
	})
}

func (s *appState) chatFilePreviewState(path string) *chatFilePreview {
	if s.chatView.filePreviewViews == nil {
		s.chatView.filePreviewViews = map[string]*chatFilePreview{}
	}
	state := s.chatView.filePreviewViews[path]
	if state == nil {
		state = &chatFilePreview{}
		s.chatView.filePreviewViews[path] = state
	}
	return state
}

func (s *appState) newChatFileWebView(path string, p palette) (*mygo.WebView, error) {
	if s.window == nil {
		return nil, os.ErrInvalid
	}
	s.trimChatFilePreviews()
	webView, err := s.window.NewWebView(mygo.WebViewOptions{
		BackgroundColor: pluginColorHex(p.background),
	})
	if err != nil {
		return nil, err
	}
	page := webView.Page()
	page.SetPermissionHandler(func(mygo.PermissionRequest) bool { return false })
	if err := page.LoadFile(path); err != nil {
		webView.Destroy()
		return nil, err
	}
	return webView, nil
}

func (s *appState) trimChatFilePreviews() {
	for path, state := range s.chatView.filePreviewViews {
		if state.webView != nil && state.webView.IsDestroyed() {
			delete(s.chatView.filePreviewViews, path)
		}
	}
	if len(s.chatView.filePreviewViews) < maxChatFilePreviews {
		return
	}
	oldestPath, oldest := "", 0
	for path, state := range s.chatView.filePreviewViews {
		if oldestPath == "" || state.lastFrame < oldest {
			oldestPath, oldest = path, state.lastFrame
		}
	}
	if oldestPath != "" {
		s.destroyChatFilePreview(oldestPath)
	}
}

// dropStaleChatFilePreviews frees the web views the transcript stopped
// building. It runs at the start of a frame: the previews built in the frame
// just drawn hold the current frame number and keep their view, ones that did
// not appear lose it, and the frame number then moves on for this frame.
func (s *appState) dropStaleChatFilePreviews() {
	for path, state := range s.chatView.filePreviewViews {
		if state.lastFrame != s.chatView.previewFrame {
			s.destroyChatFilePreview(path)
		}
	}
	s.chatView.previewFrame++
}

func (s *appState) destroyChatFilePreview(path string) {
	state := s.chatView.filePreviewViews[path]
	if state == nil {
		return
	}
	if state.webView != nil && !state.webView.IsDestroyed() {
		state.webView.Destroy()
	}
	delete(s.chatView.filePreviewViews, path)
}

// chatLocalFileFromSource reports the file a paragraph holds by itself. The
// paragraph's own source is read, so a bullet or a markdown link around the
// path still counts as one file and nothing else.
func chatLocalFileFromSource(doc *parser.Document, index int) (string, bool) {
	block := doc.Block(index)
	source := doc.Source()
	if block.Start < 0 || block.End > len(source) || block.End <= block.Start {
		return "", false
	}
	return chatLocalFileFromText(source[block.Start:block.End])
}
