package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func createTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	return dir
}

func createTestGitCommit(t *testing.T, dir, filename, content string) {
	t.Helper()
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git add: %v", err)
	}
	cmd = exec.Command("git", "commit", "-m", "init")
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git commit: %v", err)
	}
}

func TestWorkspaceChangesStatus(t *testing.T) {
	dir := createTestGitRepo(t)
	createTestGitCommit(t, dir, "tracked.txt", "original")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	files, err := resolveWorkspaceChanges(ctx, dir)
	if err != nil {
		t.Fatalf("resolveWorkspaceChanges: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2: %+v", len(files), files)
	}
	byPath := map[string]workspaceFileChange{}
	for _, file := range files {
		byPath[file.Path] = file
	}
	got := byPath["tracked.txt"]
	if got.Path != "tracked.txt" || got.UnstagedStatus != "M" {
		t.Fatalf("tracked.txt change missing or wrong: %+v", got)
	}
	gotUntracked := byPath["untracked.txt"]
	if gotUntracked.Path != "untracked.txt" || !gotUntracked.Untracked {
		t.Fatalf("untracked.txt missing or wrong: %+v", got)
	}
}

func TestWorkspaceChangesRename(t *testing.T) {
	dir := createTestGitRepo(t)
	createTestGitCommit(t, dir, "old.txt", "same content")
	cmd := exec.Command("git", "mv", "old.txt", "new.txt")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git mv: %v", err)
	}
	files, err := resolveWorkspaceChanges(context.Background(), dir)
	if err != nil {
		t.Fatalf("resolveWorkspaceChanges: %v", err)
	}
	found := false
	for _, file := range files {
		if file.Renamed && file.OriginalPath == "old.txt" && file.Path == "new.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("rename pair not found: %+v", files)
	}
}

func TestWorkspaceChangesBinary(t *testing.T) {
	dir := createTestGitRepo(t)
	createTestGitCommit(t, dir, "data.bin", string([]byte{0, 1, 2, 3}))
	if err := os.WriteFile(filepath.Join(dir, "data.bin"), []byte{4, 5, 6, 7}, 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := resolveWorkspaceChanges(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	diff, err := workspaceDiff(context.Background(), dir, files[0])
	if err != nil {
		t.Fatalf("workspaceDiff: %v", err)
	}
	if !strings.Contains(diff, "Binary") && !strings.Contains(diff, "differ") {
		t.Fatalf("binary marker missing: %q", diff)
	}
}

func TestWorkspaceChangesUntrackedPreview(t *testing.T) {
	dir := createTestGitRepo(t)
	path := filepath.Join(dir, "untracked.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	preview, err := workspaceUntrackedPreview(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(preview) != "hello" {
		t.Fatalf("preview = %q, want \"hello\"", preview)
	}
}
func TestWorkspaceUntrackedPreviewUsesRepositoryRoot(t *testing.T) {
	dir := createTestGitRepo(t)
	path := filepath.Join(dir, "untracked.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	preview, err := workspaceUntrackedPreview(filepath.Join(dir, "untracked.txt"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(preview) != "hello" {
		t.Fatalf("preview = %q, want \"hello\"", preview)
	}
}
func TestWorkspaceErrorKindDistinguishesNotGit(t *testing.T) {
	if got := workspaceErrorKind(errors.New("fatal: not a git repository")); got != "notGit" {
		t.Fatalf("notGit = %q", got)
	}
	if got := workspaceErrorKind(errors.New("fatal: permission denied")); got != "gitFailed" {
		t.Fatalf("gitFailed = %q", got)
	}
}
func TestWorkspaceChangesSubdirRepoStatus(t *testing.T) {
	dir := createTestGitRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	createTestGitCommit(t, dir, "tracked.txt", "original")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	files, err := resolveWorkspaceChanges(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "tracked.txt" {
		t.Fatalf("files = %+v", files)
	}
	unstaged, err := workspaceDiffUnstaged(ctx, dir, files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unstaged, "modified") {
		t.Fatalf("unstaged diff missing: %q", unstaged)
	}
}
func TestWorkspaceChangesRenameKeepsUnstagedStatus(t *testing.T) {
	dir := createTestGitRepo(t)
	createTestGitCommit(t, dir, "old.txt", "same content")
	cmd := exec.Command("git", "mv", "old.txt", "new.txt")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git mv: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := resolveWorkspaceChanges(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range files {
		if file.Renamed && file.OriginalPath == "old.txt" && file.Path == "new.txt" && file.UnstagedStatus == "M" {
			found = true
		}
	}
	if !found {
		t.Fatalf("rename with unstaged edit missing: %+v", files)
	}
}
func TestWorkspaceChangesLargeOutputTruncates(t *testing.T) {
	dir := createTestGitRepo(t)
	createTestGitCommit(t, dir, "big.txt", strings.Repeat("x", 600000))
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(strings.Repeat("y", 600000)), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := resolveWorkspaceChanges(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %+v", files)
	}
	diff, err := workspaceDiffUnstaged(context.Background(), dir, files[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(diff) > workspaceChangesMaxRead {
		t.Fatalf("diff length %d exceeds cap", len(diff))
	}
}
func TestWorkspaceChangesGlobalIgnoreRespected(t *testing.T) {
	dir := createTestGitRepo(t)
	home := t.TempDir()
	ignore := filepath.Join(home, "ignore.txt")
	if err := os.WriteFile(ignore, []byte("ignored.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "config", "core.excludesFile", ignore)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := resolveWorkspaceChanges(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("ignored file leaked: %+v", files)
	}
}

func TestWorkspaceDetailLifecycleClearsAndReloads(t *testing.T) {
	dir := createTestGitRepo(t)
	createTestGitCommit(t, dir, "tracked.txt", "original")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := &appState{workspaceChangesOpen: true}
	state.workspaceChangesRepo = dir
	state.workspaceChangesFiles = []workspaceFileChange{{Path: "tracked.txt", UnstagedStatus: "M"}}
	state.workspaceChangesDetail = "tracked.txt"

	state.refreshWorkspaceChanges()
	if !state.workspaceChangesLoading {
		t.Fatal("list refresh did not set loading")
	}
	// Detail load clears old content and sets its own loading flag.
	state.workspaceDetailStagedDiff = "stale staged"
	state.loadWorkspaceDetail("tracked.txt")
	if !state.workspaceDetailLoading {
		t.Fatal("detail load did not set loading")
	}
	if state.workspaceDetailStagedDiff != "" || state.workspaceDetailUnstagedDiff != "" {
		t.Fatal("detail load did not clear old texts")
	}

	// Cancellation bumps the detail request id and clears loading.
	previousID := state.workspaceDetailRequestID
	state.cancelWorkspaceDetail()
	if state.workspaceDetailRequestID != previousID+1 {
		t.Fatalf("detail cancel did not bump request id: %d", state.workspaceDetailRequestID)
	}
	if state.workspaceDetailLoading {
		t.Fatal("detail cancel did not clear loading")
	}

	// Detail cancel does not bump or cancel the list request.
	if state.workspaceChangesCancel == nil {
		t.Fatal("detail cancel cancelled the list request")
	}

	// A gone file clears selection and fields.
	state.cancelWorkspaceDetail()
	state.clearWorkspaceDetailSelection()
	if state.workspaceChangesDetail != "" {
		t.Fatal("clear selection did not reset state")
	}
	if state.workspaceDetailStagedDiff != "" || state.workspaceDetailErrorText != "" {
		t.Fatal("clear selection did not clear detail fields")
	}
}

func TestWorkspaceRefreshClearsGoneSelection(t *testing.T) {
	state := &appState{workspaceChangesOpen: true}
	state.workspaceChangesDetail = "gone.txt"
	state.workspaceChangesRepo = ""
	state.workspaceChangesFiles = nil
	state.clearWorkspaceDetailSelection()
	if state.workspaceChangesDetail != "" {
		t.Fatal("selection not cleared")
	}
}

func TestWorkspaceBoundedWriterTruncationFlag(t *testing.T) {
	w := &boundedWriter{limit: 100}
	if _, err := w.Write([]byte(strings.Repeat("x", 60))); err != nil {
		t.Fatal(err)
	}
	if w.truncated {
		t.Fatal("60 of 100 bytes must not be marked truncated")
	}
	if _, err := w.Write([]byte(strings.Repeat("y", 60))); err != nil {
		t.Fatal(err)
	}
	if !w.truncated {
		t.Fatal("overflow past the limit must be marked truncated")
	}
	if len(w.buffer) != 100 {
		t.Fatalf("buffer length = %d, want 100", len(w.buffer))
	}
}

func TestWorkspaceStatusFromCutStreamDropsPartialPath(t *testing.T) {
	stream := " M one.txt\x00 M two.txt\x00 M thr"
	got := workspaceStatusFromOutput([]byte(stream), true)
	if !got.truncated {
		t.Fatal("cut stream must report an incomplete list")
	}
	if len(got.files) != 2 {
		t.Fatalf("files = %+v, want the two complete entries", got.files)
	}
	for _, file := range got.files {
		if strings.Contains(file.Path, "thr") {
			t.Fatalf("partial path leaked: %+v", file.Path)
		}
	}
	// No NUL at all means nothing is reliable.
	empty := workspaceStatusFromOutput([]byte(" M on"), true)
	if len(empty.files) != 0 || !empty.truncated {
		t.Fatalf("no-NUL cut stream = %+v", empty)
	}
}

func TestWorkspaceErrorKindUsesStderr(t *testing.T) {
	dir := t.TempDir()
	_, err := runWorkspaceCommand(context.Background(), dir, "status", "--porcelain=v1", "-z")
	if err == nil {
		t.Fatal("expected a failure outside a Git repository")
	}
	if got := workspaceErrorKind(err); got != "notGit" {
		t.Fatalf("non-Git folder classified %q: %v", got, err)
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("stderr missing from error: %v", err)
	}
}

func TestWorkspaceErrorKindTreatsCancelAsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := exec.CommandContext(ctx, "git", "status")
	cmd.Dir = t.TempDir()
	err := cmd.Run()
	if err == nil {
		t.Skip("command finished before cancellation was observable")
	}
	if got := workspaceErrorKind(err); got != "cancelled" {
		t.Fatalf("cancel classified %q: %v", got, err)
	}
}

func TestWorkspaceStatusParsesBothPathColumns(t *testing.T) {
	// Staged rename: "R  new\0old".
	staged := parseWorkspaceStatus([]byte("R  new.go\x00old.go\x00"))
	if len(staged) != 1 || staged[0].Path != "new.go" || staged[0].OriginalPath != "old.go" ||
		staged[0].StagedStatus != "R" || !staged[0].Renamed {
		t.Fatalf("staged rename = %+v", staged)
	}
	// Worktree rename: " R new\0old" keeps the new path and the unstaged column.
	worktree := parseWorkspaceStatus([]byte(" R new2.go\x00old2.go\x00"))
	if len(worktree) != 1 || worktree[0].Path != "new2.go" || worktree[0].OriginalPath != "old2.go" ||
		worktree[0].StagedStatus != "" || worktree[0].UnstagedStatus != "R" {
		t.Fatalf("worktree rename = %+v", worktree)
	}
	// Copy shares the two-path shape and is not a rename.
	copied := parseWorkspaceStatus([]byte("C  copy.go\x00src.go\x00"))
	if len(copied) != 1 || copied[0].Path != "copy.go" || copied[0].OriginalPath != "src.go" ||
		copied[0].Renamed || copied[0].StagedStatus != "C" {
		t.Fatalf("copy = %+v", copied)
	}
	// Untracked never carries a staged status.
	untracked := parseWorkspaceStatus([]byte("?? new.txt\x00"))
	if len(untracked) != 1 || untracked[0].StagedStatus != "" ||
		untracked[0].UnstagedStatus != "?" || !untracked[0].Untracked {
		t.Fatalf("untracked = %+v", untracked)
	}
}

func TestWorkspaceDetailTruncationAndBinary(t *testing.T) {
	dir := createTestGitRepo(t)
	createTestGitCommit(t, dir, "big.txt", strings.Repeat("x", 4000))
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(strings.Repeat("y", 4000)), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := resolveWorkspaceChanges(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %+v", files)
	}
	_, cut, err := workspaceUnstagedDiff(context.Background(), dir, files[0], 512)
	if err != nil {
		t.Fatal(err)
	}
	if !cut {
		t.Fatal("a diff longer than the cap must report truncation")
	}
	_, cut, err = workspaceUnstagedDiff(context.Background(), dir, files[0], workspaceChangesMaxRead)
	if err != nil {
		t.Fatal(err)
	}
	if cut {
		t.Fatal("a diff under the cap must not report truncation")
	}
	if !workspaceTextIsBinary("Binary files a/data.bin and b/data.bin differ") {
		t.Fatal("git binary marker not detected")
	}
	if !workspaceTextIsBinary("a\x00b") {
		t.Fatal("NUL preview not detected as binary")
	}
	if workspaceTextIsBinary("+hello") {
		t.Fatal("plain diff text misdetected as binary")
	}
}

func TestWorkspaceCancelChangesStopsBothRequests(t *testing.T) {
	state := &appState{workspaceChangesOpen: true}
	state.workspaceChangesRepo = t.TempDir()
	state.workspaceChangesFiles = []workspaceFileChange{{Path: "tracked.txt", UnstagedStatus: "M"}}
	state.refreshWorkspaceChanges()
	listCancel := state.workspaceChangesCancel
	state.loadWorkspaceDetail("tracked.txt")
	detailCancel := state.workspaceDetailCancel
	if listCancel == nil || detailCancel == nil {
		t.Fatal("both requests must be in flight")
	}
	listID := state.workspaceChangesRequestID
	detailID := state.workspaceDetailRequestID
	// Funcs are not comparable, so each is wrapped to record that it ran.
	var listCanceled, detailCanceled bool
	state.workspaceChangesCancel = func() {
		listCanceled = true
		listCancel()
	}
	state.workspaceDetailCancel = func() {
		detailCanceled = true
		detailCancel()
	}
	state.cancelWorkspaceChanges()
	if state.workspaceChangesCancel != nil || state.workspaceDetailCancel != nil {
		t.Fatal("cancel did not clear both cancel funcs")
	}
	if !listCanceled || !detailCanceled {
		t.Fatalf("cancel stopped only list=%v detail=%v", listCanceled, detailCanceled)
	}
	if state.workspaceChangesRequestID != listID+1 || state.workspaceDetailRequestID != detailID+1 {
		t.Fatalf("cancel did not bump both request ids: %d %d",
			state.workspaceChangesRequestID, state.workspaceDetailRequestID)
	}
	if state.workspaceChangesLoading || state.workspaceDetailLoading {
		t.Fatal("cancel left a loading flag set")
	}
}

func TestWorkspaceClearForgetsPreviousRepository(t *testing.T) {
	state := &appState{workspaceChangesOpen: true}
	state.workspaceChangesRepo = "/previous"
	state.workspaceChangesFiles = []workspaceFileChange{{Path: "old.go", UnstagedStatus: "M"}}
	state.workspaceChangesDetail = "old.go"
	state.workspaceChangesErrorKind = "gitFailed"
	state.workspaceChangesErrorText = "stale"
	state.workspaceDetailStagedDiff = "stale diff"
	state.clearWorkspaceChanges()
	if state.workspaceChangesRepo != "" || state.workspaceChangesFiles != nil {
		t.Fatalf("list state survived the reset: %+v", state)
	}
	if state.workspaceChangesErrorKind != "" || state.workspaceChangesErrorText != "" {
		t.Fatal("error survived the reset")
	}
	if state.workspaceChangesDetail != "" || state.workspaceDetailStagedDiff != "" {
		t.Fatal("detail survived the reset")
	}
}

func TestWorkspaceCwdHooksRefreshOrClearOpenPanel(t *testing.T) {
	// A workspace switch while the panel is open reloads it instead of leaving
	// the previous repository's changes on screen.
	open := &appState{workspaceChangesOpen: true}
	open.workspaceChangesRepo = "/previous"
	open.workspaceChangesFiles = []workspaceFileChange{{Path: "old.go", UnstagedStatus: "M"}}
	open.setPiCwd("/new-workspace")
	if open.piCwd != "/new-workspace" {
		t.Fatalf("piCwd = %q", open.piCwd)
	}
	if !open.workspaceChangesLoading {
		t.Fatal("a workspace change did not refresh the open panel")
	}
	// A closed panel is left alone, and a reset drops the old workspace state.
	closed := &appState{}
	closed.workspaceChangesRepo = "/previous"
	closed.workspaceChangesFiles = []workspaceFileChange{{Path: "old.go", UnstagedStatus: "M"}}
	closed.setPiCwd("/new")
	if closed.workspaceChangesFiles == nil {
		t.Fatal("a closed panel must not be cleared or refreshed")
	}
	closed.resetPiCwd()
	if closed.piCwd != "" || closed.workspaceChangesFiles != nil {
		t.Fatal("reset left workspace or panel state behind")
	}
}

func TestWorkspaceDetailBinarySuppressesUntrackedBytes(t *testing.T) {
	// The usual "new file" case: an untracked image whose preview is raw bytes.
	image := string([]byte{0x89, 'P', 'N', 'G', 0, 1, 2, 3})
	got := workspaceDetailContentFrom("", false, "", false, image, false, true)
	if !got.binary {
		t.Fatal("binary untracked preview not detected")
	}
	if got.untracked != "" {
		t.Fatalf("raw bytes still shown: %q", got.untracked)
	}
	if got.truncated {
		t.Fatal("a small file must not be marked truncated")
	}
	// A textual untracked preview is unaffected.
	plain := workspaceDetailContentFrom("", false, "", false, "hello\n", false, true)
	if plain.binary || plain.untracked != "hello\n" {
		t.Fatalf("textual preview changed: %+v", plain)
	}
	// Git's binary marker belongs to diff output only, so a readable .patch or
	// log that quotes it stays on screen (DF7).
	marker := "Binary files a/logo.png and b/logo.png differ\n"
	patch := workspaceDetailContentFrom("", false, "", false, marker, false, true)
	if patch.binary || patch.untracked != marker {
		t.Fatalf("readable text hidden because it quoted the diff marker: %+v", patch)
	}
}
