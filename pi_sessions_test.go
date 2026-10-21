package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPiSessionDirectoryEncoding(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	got := piSessionDirectory(root, filepath.Join(string(filepath.Separator), "Users", "Lex", "Projects", "BYOP:Prototype"))
	want := filepath.Join(root, "--Users-Lex-Projects-BYOP-Prototype--")
	if got != want {
		t.Fatalf("session directory = %q, want %q", got, want)
	}
	if got := piSessionDirectory(root, `C:\Users\Lex\BYOP`); got != filepath.Join(root, "--C--Users-Lex-BYOP--") {
		t.Fatalf("Windows-style session directory = %q", got)
	}
}

func TestDiscoverPiSessionsTitlesOrderingAndLongLines(t *testing.T) {
	dir := t.TempDir()
	firstPath := filepath.Join(dir, "first.jsonl")
	first := strings.Join([]string{
		`{"type":"session","version":3,"id":"first-id","timestamp":"2026-10-20T09:00:00Z","cwd":"/work"}`,
		`{"type":"message","id":"system","message":{"role":"system","content":"hidden prompt","timestamp":1}}`,
		`{"type":"message","id":"m1","message":{"role":"user","content":"First draft prompt","timestamp":1}}`,
		`{"type":"session_info","id":"m2","name":"Renamed session"}`,
		`{"type":"message","id":"m3","message":{"role":"assistant","content":[{"type":"text","text":"Hi"}],"timestamp":2}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(firstPath, []byte(first), 0o600); err != nil {
		t.Fatal(err)
	}

	secondPath := filepath.Join(dir, "second.jsonl")
	second := strings.Join([]string{
		`{"type":"session","version":3,"id":"second-id","timestamp":"2026-10-20T08:00:00Z","cwd":"/work"}`,
		`{"type":"message","id":"m1","message":{"role":"user","content":[{"type":"text","text":"  First   user\n title  "}],"timestamp":1}}`,
		`{"type":"message","id":"long","message":{"role":"user","content":"` + strings.Repeat("x", piSessionScanLineLimit) + `"}}`,
		`{"type":"message","id":"m2","message":{"role":"assistant","content":[{"type":"text","text":"Reply"}],"timestamp":2}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(secondPath, []byte(second), 0o600); err != nil {
		t.Fatal(err)
	}

	old := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	recent := old.Add(2 * time.Hour)
	if err := os.Chtimes(firstPath, recent, recent); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(secondPath, old, old); err != nil {
		t.Fatal(err)
	}

	sessions, err := discoverPiSessions(dir, "/work", false, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("discovered %d sessions, want 2: %#v", len(sessions), sessions)
	}
	if sessions[0].ID != "first-id" || sessions[0].Title != "Renamed session" ||
		sessions[0].MessageCount != 2 || !sessions[0].Created.Equal(time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected first session: %#v", sessions[0])
	}
	if sessions[1].ID != "second-id" || sessions[1].Title != "First user title" ||
		sessions[1].MessageCount != 2 {
		t.Fatalf("unexpected second session: %#v", sessions[1])
	}
}

func TestDiscoverPiSessionsSkipsSymlinkedFile(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	content := `{"type":"session","id":"outside","timestamp":"2026-10-20T08:00:00Z"}` + "\n"
	if err := os.WriteFile(outside, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "linked.jsonl")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	sessions, err := discoverPiSessions(dir, "/work", false, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("symlink outside the session directory was discovered: %#v", sessions)
	}
}

func TestDiscoverFlatPiSessionsFiltersCwdBeforeLimit(t *testing.T) {
	dir := t.TempDir()
	writeSession := func(name, cwd string, modified time.Time) {
		t.Helper()
		content := `{"type":"session","id":"` + name + `","timestamp":"2026-10-20T08:00:00Z","cwd":"` + cwd + `"}` + "\n" +
			`{"type":"message","message":{"role":"user","content":"` + name + `"}}` + "\n"
		path := filepath.Join(dir, name+".jsonl")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, modified, modified); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	writeSession("other-newest", "/other", now.Add(2*time.Minute))
	writeSession("other-newer", "/other", now.Add(time.Minute))
	writeSession("matching-older", "/work", now)

	sessions, err := discoverPiSessions(dir, "/work", true, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "matching-older" {
		t.Fatalf("flat discovery truncated before cwd filtering: %#v", sessions)
	}
}

func TestRebuildChatFromPiMessages(t *testing.T) {
	longResult := strings.Repeat("r", 1300)
	raw := []json.RawMessage{
		json.RawMessage(`{"role":"system","content":"hidden"}`),
		json.RawMessage(`{"role":"user","content":[{"type":"text","text":"Use a tool"}],"timestamp":1}`),
		json.RawMessage(`{"role":"assistant","content":[{"type":"text","text":"Checking."},{"type":"toolCall","id":"call_1","name":"bash","arguments":{"command":"printf ok"}}],"timestamp":2}`),
		json.RawMessage(`{"role":"toolResult","toolCallId":"call_1","toolName":"bash","content":[{"type":"text","text":"` + longResult + `"}],"isError":false,"timestamp":3}`),
		json.RawMessage(`{"role":"assistant","content":[{"type":"text","text":"Done."}],"timestamp":4}`),
	}
	chat, err := rebuildChatFromPiMessages(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(chat) != 3 {
		t.Fatalf("rebuilt %d messages, want 3: %#v", len(chat), chat)
	}
	if chat[0].role != chatUser || chat[0].blocks[0].text != "Use a tool" {
		t.Fatalf("unexpected restored user message: %#v", chat[0])
	}
	assistant := chat[1]
	if assistant.role != chatAssistant || len(assistant.blocks) != 2 ||
		assistant.blocks[0].text != "Checking." || assistant.blocks[1].kind != chatBlockTool {
		t.Fatalf("unexpected restored assistant message: %#v", assistant)
	}
	tool := assistant.blocks[1].tool
	if tool.id != "call_1" || tool.name != "bash" || tool.arguments != "printf ok" ||
		tool.status != toolCallCompleted || !strings.HasSuffix(tool.result, "output truncated") {
		t.Fatalf("unexpected restored tool: %#v", tool)
	}
	if chat[2].role != chatAssistant || chat[2].blocks[0].text != "Done." {
		t.Fatalf("unexpected final assistant message: %#v", chat[2])
	}
}

func TestDecodePiModelsDropsUnneededFields(t *testing.T) {
	models, err := decodePiModels(json.RawMessage(`{
		"models":[{
			"provider":"example",
			"id":"model-1",
			"name":"Example Model",
			"reasoning":true,
			"contextWindow":200000,
			"input":["text","image"],
			"apiKey":"secret-key",
			"baseUrl":"https://secret.example.invalid",
			"headers":{"Authorization":"Bearer secret"}
		}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].Provider != "example" || models[0].ID != "model-1" ||
		models[0].Name != "Example Model" || !models[0].Reasoning ||
		models[0].ContextWindow != 200000 || strings.Join(models[0].Input, ",") != "text,image" {
		t.Fatalf("unexpected decoded models: %#v", models)
	}
	encoded, err := json.Marshal(models)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret-key", "secret.example.invalid", "Authorization", "Bearer secret"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("decoded model retained %q: %s", secret, encoded)
		}
	}
}
