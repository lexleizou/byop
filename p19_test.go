package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestP19ChatLocalFileFromText(t *testing.T) {
	dir := t.TempDir()
	html := filepath.Join(dir, "report.html")
	if err := os.WriteFile(html, []byte("<h1>ok</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "gone.md")

	for _, value := range []string{
		html,
		"file://" + html,
		"[报告](" + html + ")",
		html + "。",
	} {
		got, ok := chatLocalFileFromText(value)
		if !ok || got != html {
			t.Errorf("value %q: got %q ok=%v, want %q", value, got, ok, html)
		}
	}
	for _, value := range []string{
		"",
		"see " + html + " for details",
		missing,
		dir,
		"https://example.com/a.html",
		"notes/report.html",
	} {
		if got, ok := chatLocalFileFromText(value); ok {
			t.Errorf("value %q: got %q, want no file", value, got)
		}
	}
	if !chatFileIsHTML(html) || chatFileIsHTML(filepath.Join(dir, "a.txt")) {
		t.Error("HTML file detection is wrong")
	}
}

func TestP19TranscriptShowsOutputFileCard(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	dir := t.TempDir()
	html := filepath.Join(dir, "Pi-能帮你做什么-20261029.html")
	if err := os.WriteFile(html, []byte("<h1>ok</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(html)

	state := layoutTestState(t)
	state.chat = []chatMessage{
		{role: chatUser, status: "Complete",
			blocks: []chatBlock{{kind: chatBlockText, text: "Make the page."}}},
		{role: chatAssistant, status: "Complete", outputTokens: 40,
			blocks: []chatBlock{{kind: chatBlockText,
				text: "Done.\n\n" + html}}},
	}
	tt := ui.NewTester(state.view, 1180, 760)

	label := textArgs("fileCardLabel", map[string]any{"name": name})
	if _, ok := tt.Find(label); !ok {
		t.Fatalf("file card did not render: label=%q texts=%#v", label, tt.Texts())
	}
	for _, text := range tt.Texts() {
		if strings.Contains(text, dir) && text != label {
			t.Fatalf("card leaked the full path into text: %q", text)
		}
	}
	if _, ok := tt.Find(html); ok {
		t.Fatal("raw file path still shows as transcript text")
	}
	if _, ok := tt.Find(text("filePreviewHide")); !ok {
		t.Fatal("HTML card did not open its preview")
	}
	// The card's own menu offers the destinations Lex asked for. It opens on a
	// click of the card, so it is checked from the card's right edge, past the
	// selectable file name.
	toggle, ok := tt.Find(text("filePreviewHide"))
	if !ok {
		t.Fatal("preview toggle missing")
	}
	tt.ClickAt(toggle.X+toggle.W+10, toggle.Y+toggle.H/2)
	menu := tt.Menu()
	joined := strings.Join(menu, "|")
	for _, want := range []string{text("fileMenuOpen"), text("fileMenuReveal"), text("fileMenuCopyPath")} {
		if !strings.Contains(joined, want) {
			t.Fatalf("file menu missing %q: %#v", want, menu)
		}
	}
}

func TestP19PiStatusPopoverFitsReconnectButton(t *testing.T) {
	restoreAppearance := stableTestAppearance(t)
	defer restoreAppearance()

	state := layoutTestState(t)
	state.piVersion = "1.4.0"
	label := textArgs("navPiStatus", map[string]any{
		"version": "1.4.0", "status": state.connectionStateLabel(),
	})
	tt := ui.NewTester(state.view, 1180, 760)
	if err := tt.Click(label); err != nil {
		t.Fatalf("open pi status popover: %v", err)
	}
	tt.Frame()
	button, ok := tt.Find(text("settingsReconnect"))
	if !ok {
		t.Fatalf("reconnect button missing: %#v", tt.Texts())
	}
	panel, ok := tt.Find(text("piCliDetailsTitle"))
	if !ok {
		t.Fatal("popover panel missing")
	}
	if button.W <= 0 || button.X < panel.X-8 ||
		button.X+button.W > panel.X+piStatusPopoverWidth {
		t.Fatalf("reconnect button overflows the popover: button=%#v title=%#v", button, panel)
	}
}

// TestP19NodeRuntimeResolvesOutsidePATH covers the plugin card that showed
// "Node.js ≥25 not found: executable was not found in PATH" in a window opened
// from Finder: the app PATH has no package manager folders, so Node must still
// be found in the common install folders or the login shell PATH.
func TestP19NodeRuntimeResolvesOutsidePATH(t *testing.T) {
	// Tests in this group swap package variables and must not run in parallel.
	t.Setenv("BYOP_NODE", "")
	t.Setenv("PATH", t.TempDir())

	installDir := t.TempDir()
	nodePath := filepath.Join(installDir, "node")
	if err := os.WriteFile(nodePath, []byte("#!/bin/sh\necho v25.0.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	originalDirs := piCommonInstallDirectories
	originalLoginPath := piLoginShellPATH
	originalSync := piLoginShellPATHSync
	installDirs := []string(nil)
	loginPath := ""
	piCommonInstallDirectories = func() []string { return installDirs }
	piLoginShellPATH = func() string { return loginPath }
	t.Cleanup(func() {
		piCommonInstallDirectories = originalDirs
		piLoginShellPATH = originalLoginPath
		piLoginShellPATHSync = originalSync
	})
	resetCache := func() { piLoginShellPATHSync = &loginShellPATHState{} }

	host := newPluginHost(pluginHostVersion, nil, pluginHostWorkspace(t.TempDir()),
		pluginHostSettings(newPluginSettingsStore(t.TempDir())))
	resetCache()
	if _, err := host.nodeRuntime(); err == nil {
		t.Fatal("node resolved with no PATH, common folders or login PATH")
	}

	// A failed lookup must stay retryable: the login shell probe may finish
	// after the first plugin start, and "重新加载" should then succeed.
	installDirs = []string{installDir}
	if got, err := host.nodeRuntime(); err != nil || got != nodePath {
		t.Fatalf("common install folder: got %q err %v, want %q", got, err, nodePath)
	}
	if got := host.nodeVersion; got != "v25.0.0" {
		t.Fatalf("node version = %q, want v25.0.0", got)
	}

	host = newPluginHost(pluginHostVersion, nil, pluginHostWorkspace(t.TempDir()),
		pluginHostSettings(newPluginSettingsStore(t.TempDir())))
	installDirs = nil
	loginPath = installDir
	resetCache()
	if got, err := host.nodeRuntime(); err != nil || got != nodePath {
		t.Fatalf("login shell PATH: got %q err %v, want %q", got, err, nodePath)
	}
}
