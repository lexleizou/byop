package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	workspaceChangesMaxBytes = 256 * 1024
	workspaceChangesMaxRead  = 512 * 1024
	// workspaceChangesErrorLimit caps the git message the panel shows, so a
	// long warning cannot fill it. uiErrorSummary also hides local paths.
	workspaceChangesErrorLimit = 240
)

type workspaceFileChange struct {
	Path           string `json:"path,omitempty"`
	OriginalPath   string `json:"original_path,omitempty"`
	StagedStatus   string `json:"staged_status,omitempty"`
	UnstagedStatus string `json:"unstaged_status,omitempty"`
	Untracked      bool   `json:"untracked,omitempty"`
	Renamed        bool   `json:"renamed,omitempty"`
}

type workspaceDetailState struct {
	requestID    uint64
	stagedDiff   string
	unstagedDiff string
	untracked    string
	errorText    string
}

type workspaceChangeSnapshot struct {
	RepositoryPath string                `json:"repository_path,omitempty"`
	Files          []workspaceFileChange `json:"files,omitempty"`
	Error          string                `json:"error,omitempty"`
}

// workspaceTwoPathStatus reports the porcelain statuses whose entry is followed
// by a second NUL-separated path: rename and copy.
func workspaceTwoPathStatus(status byte) bool {
	return status == 'R' || status == 'C'
}

func workspaceErrorKind(err error) string {
	if strings.Contains(err.Error(), "not a git repository") ||
		strings.Contains(err.Error(), "fatal: not a git repository") {
		return "notGit"
	}
	// Only our own cancellation is a cancelled result. A kill from outside the
	// app is a real failure, so it stays gitFailed and the panel says so.
	if errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "context canceled") {
		return "cancelled"
	}
	return "gitFailed"
}

func parseWorkspaceStatus(data []byte) []workspaceFileChange {
	var entries []workspaceFileChange
	var stagedStatus, unstagedStatus byte
	// A rename or copy entry is followed by its original path, so the new path
	// is held until that second field arrives. Copy shares the same shape.
	var pendingSecondPath bool
	var newPath string
	fields := bytes.Split(data, []byte{0})
	for _, field := range fields {
		if len(field) == 0 {
			continue
		}
		if !pendingSecondPath {
			if len(field) < 4 || field[2] != ' ' {
				continue
			}
			stagedStatus, unstagedStatus = field[0], field[1]
			path := string(field[3:])
			if workspaceTwoPathStatus(stagedStatus) || workspaceTwoPathStatus(unstagedStatus) {
				newPath = path
				pendingSecondPath = true
				continue
			}
			// An untracked entry carries "?" in both columns; it is never staged.
			staged := stagedStatus
			if unstagedStatus == '?' {
				staged = ' '
			}
			entries = append(entries, workspaceFileChange{
				Path:           path,
				OriginalPath:   "",
				StagedStatus:   statusLetter(staged),
				UnstagedStatus: statusLetter(unstagedStatus),
				Renamed:        false,
				Untracked:      unstagedStatus == '?',
			})
			continue
		}
		originalPath := string(field)
		entries = append(entries, workspaceFileChange{
			Path:           newPath,
			OriginalPath:   originalPath,
			StagedStatus:   statusLetter(stagedStatus),
			UnstagedStatus: statusLetter(unstagedStatus),
			Renamed:        stagedStatus == 'R' || unstagedStatus == 'R',
			Untracked:      false,
		})
		pendingSecondPath = false
		newPath = ""
	}
	return entries
}

func workspaceStatusLines(output string) []workspaceFileChange {
	return parseWorkspaceStatus([]byte(output))
}

func statusLetter(status byte) string {
	if status == 0 || status == ' ' {
		return ""
	}
	return string(rune(status))
}

type boundedWriter struct {
	buffer    []byte
	limit     int
	truncated bool
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	remaining := w.limit - len(w.buffer)
	if remaining < 0 {
		remaining = 0
	}
	take := min(remaining, len(p))
	w.buffer = append(w.buffer, p[:take]...)
	// Only part of this chunk fit, so the stream is longer than the buffer.
	if take < len(p) {
		w.truncated = true
	}
	return len(p), nil
}

func (w *boundedWriter) String() string {
	return string(w.buffer)
}

// workspaceCommandOutput is git stdout plus the writer's truncation flag.
type workspaceCommandOutput struct {
	text      string
	truncated bool
}

func runWorkspaceCommand(ctx context.Context, cwd string, args ...string) (string, error) {
	return runWorkspaceCommandBounded(ctx, cwd, workspaceChangesMaxRead, args...)
}

func runWorkspaceCommandBounded(ctx context.Context, cwd string, limit int, args ...string) (string, error) {
	output, err := runWorkspaceCommandTracked(ctx, cwd, limit, args...)
	if err != nil {
		return output.text, err
	}
	return output.text, nil
}

// runWorkspaceCommandTracked keeps a bounded copy of stderr so the caller can
// tell a non-Git folder from any other git failure instead of seeing only
// "exit status 128".
func runWorkspaceCommandTracked(ctx context.Context, cwd string, limit int, args ...string) (workspaceCommandOutput, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_OPTIONAL_LOCKS=0", "GIT_LITERAL_PATHSPECS=1")
	stdout := &boundedWriter{limit: limit}
	stderr := &boundedWriter{limit: workspaceChangesMaxRead}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	output := workspaceCommandOutput{text: stdout.String(), truncated: stdout.truncated}
	if err != nil {
		if message := strings.TrimSpace(stderr.String()); message != "" {
			err = fmt.Errorf("%w: %s", err, message)
		}
	}
	return output, err
}

func osPathForWorkspace() string {
	return "/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin"
}

func resolveWorkspaceRepository(ctx context.Context, cwd string) (string, error) {
	output, err := runWorkspaceCommand(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func resolveWorkspaceChanges(ctx context.Context, cwd string) ([]workspaceFileChange, error) {
	status, err := resolveWorkspaceStatus(ctx, cwd)
	return status.files, err
}

// workspaceStatusOutput is the parsed status list plus whether the stream was
// cut at the read cap.
type workspaceStatusOutput struct {
	files     []workspaceFileChange
	truncated bool
}

// resolveWorkspaceStatus reads the porcelain status stream. A cut stream is
// trimmed back to the last NUL so a half-written path never reaches the list;
// the caller then shows the "partly shown" note.
func resolveWorkspaceStatus(ctx context.Context, cwd string) (workspaceStatusOutput, error) {
	output, err := runWorkspaceCommandTracked(ctx, cwd, workspaceChangesMaxRead,
		"status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return workspaceStatusOutput{}, err
	}
	return workspaceStatusFromOutput([]byte(output.text), output.truncated), nil
}

// workspaceStatusFromOutput parses a status stream. When the stream was cut at
// the read cap it keeps only the entries before the last NUL, so a half-written
// path never reaches the list, and reports the list as incomplete.
func workspaceStatusFromOutput(data []byte, truncated bool) workspaceStatusOutput {
	if !truncated {
		return workspaceStatusOutput{files: parseWorkspaceStatus(data)}
	}
	if index := bytes.LastIndexByte(data, 0); index >= 0 {
		return workspaceStatusOutput{files: parseWorkspaceStatus(data[:index]), truncated: true}
	}
	// Even the first entry did not finish, so nothing is reliable.
	return workspaceStatusOutput{truncated: true}
}

// workspaceDiffText runs one diff for a path against an explicit byte cap and
// reports whether git output reached that cap.
func workspaceDiffText(ctx context.Context, cwd, path string, staged bool, limit int) (string, bool, error) {
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-color"}
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--", path)
	output, err := runWorkspaceCommandTracked(ctx, cwd, limit, args...)
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(output.text), output.truncated, nil
}

func workspaceStagedDiff(ctx context.Context, cwd string, file workspaceFileChange, limit int) (string, bool, error) {
	if file.StagedStatus == "" {
		return "", false, nil
	}
	return workspaceDiffText(ctx, cwd, file.Path, true, limit)
}

func workspaceUnstagedDiff(ctx context.Context, cwd string, file workspaceFileChange, limit int) (string, bool, error) {
	if file.UnstagedStatus == "" || file.Untracked {
		return "", false, nil
	}
	return workspaceDiffText(ctx, cwd, file.Path, false, limit)
}

func workspaceDiff(ctx context.Context, cwd string, file workspaceFileChange) (string, error) {
	text, _, err := workspaceDiffText(ctx, cwd, file.Path, file.StagedStatus != "", workspaceChangesMaxRead)
	if err != nil {
		return "", err
	}
	return text, nil
}

func workspaceDiffStaged(ctx context.Context, cwd string, file workspaceFileChange) (string, error) {
	text, _, err := workspaceStagedDiff(ctx, cwd, file, workspaceChangesMaxRead)
	return text, err
}

func workspaceDiffUnstaged(ctx context.Context, cwd string, file workspaceFileChange) (string, error) {
	text, _, err := workspaceUnstagedDiff(ctx, cwd, file, workspaceChangesMaxRead)
	return text, err
}

func workspaceUntrackedPreview(path string, maxRead int) (string, error) {
	text, _, err := workspaceUntrackedPreviewText(path, maxRead)
	return text, err
}

// workspaceUntrackedPreviewText also reports whether the file was cut at
// maxRead, which the detail view shows with the "partly shown" note.
func workspaceUntrackedPreviewText(path string, maxRead int) (string, bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return "", false, err
		}
		return "symlink → " + target, false, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer file.Close()
	reader := io.LimitReader(file, int64(maxRead))
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", false, err
	}
	return string(data), len(data) >= maxRead, nil
}

// workspaceTextIsBinary reports git's binary-diff marker or a NUL byte in an
// untracked preview, both of which mean there is no readable text to show.
func workspaceTextIsBinary(text string) bool {
	if strings.IndexByte(text, 0) >= 0 {
		return true
	}
	return strings.Contains(text, "Binary files ") && strings.Contains(text, "differ")
}

func truncateWorkspaceDiff(text string, maxBytes int) string {
	if len(text) <= maxBytes {
		return text
	}
	return string([]byte(text)[:maxBytes])
}

func workspaceChangeSummary(snapshot workspaceChangeSnapshot) string {
	return fmt.Sprintf("%d files", len(snapshot.Files))
}

func (s *appState) refreshWorkspaceChanges() {
	s.cancelWorkspaceList()
	cwd, err := s.sessionWorkspaceCwd()
	if err != nil {
		s.workspaceChangesErrorKind = workspaceErrorKind(err)
		s.workspaceChangesErrorText = uiErrorSummary(err, text("errorUnknown"), workspaceChangesErrorLimit)
		s.workspaceChangesLoading = false
		s.workspaceChangesFiles = nil
		s.workspaceChangesRepo = ""
		s.workspaceChangesTruncated = false
		s.clearWorkspaceDetailSelection()
		return
	}
	requestID := s.workspaceChangesRequestID + 1
	s.workspaceChangesRequestID = requestID
	s.workspaceChangesLoading = true
	ctx, cancel := context.WithCancel(context.Background())
	s.workspaceChangesCancel = cancel
	go func() {
		repo, repoErr := resolveWorkspaceRepository(ctx, cwd)
		status, filesErr := resolveWorkspaceStatus(ctx, cwd)
		s.postUI(func() {
			if ctx.Err() != nil || !s.workspaceChangesOpen ||
				requestID != s.workspaceChangesRequestID {
				return
			}
			s.workspaceChangesLoading = false
			s.workspaceChangesCancel = nil
			// A cancelled request never shows an error; the newer request that
			// replaced it owns what the panel displays.
			if repoErr != nil && workspaceErrorKind(repoErr) == "cancelled" {
				return
			}
			if filesErr != nil && workspaceErrorKind(filesErr) == "cancelled" {
				return
			}
			if repoErr != nil {
				s.workspaceChangesErrorKind = workspaceErrorKind(repoErr)
				s.workspaceChangesErrorText = uiErrorSummary(repoErr, text("errorUnknown"), workspaceChangesErrorLimit)
				s.workspaceChangesFiles = nil
				s.workspaceChangesRepo = ""
				s.workspaceChangesTruncated = false
			} else if filesErr != nil {
				s.workspaceChangesErrorKind = workspaceErrorKind(filesErr)
				s.workspaceChangesErrorText = uiErrorSummary(filesErr, text("errorUnknown"), workspaceChangesErrorLimit)
				s.workspaceChangesFiles = nil
				s.workspaceChangesRepo = repo
				s.workspaceChangesTruncated = false
			} else {
				s.workspaceChangesErrorKind = ""
				s.workspaceChangesErrorText = ""
				s.workspaceChangesFiles = status.files
				s.workspaceChangesRepo = repo
				s.workspaceChangesTruncated = status.truncated
			}
			if s.workspaceChangesDetail != "" {
				if s.workspaceChangesFileByPath(s.workspaceChangesDetail) != nil {
					s.loadWorkspaceDetail(s.workspaceChangesDetail)
				} else {
					s.clearWorkspaceDetailSelection()
				}
			}
		})
	}()
}

// workspaceDetailContent is what the detail column shows for one file. The
// computation is kept apart from the async load so the truncation and binary
// rules can be checked directly.
type workspaceDetailContent struct {
	staged    string
	unstaged  string
	untracked string
	truncated bool
	binary    bool
}

// workspaceDetailContentFrom trims each section to the display cap and decides
// whether output was cut and whether any of it is unreadable. A binary
// untracked file — the usual "new image" case — keeps no text, because raw
// bytes are not readable content; the binary note is what the panel shows.
func workspaceDetailContentFrom(
	staged string, stagedCut bool,
	unstaged string, unstagedCut bool,
	preview string, previewCut bool,
	untracked bool,
) workspaceDetailContent {
	content := workspaceDetailContent{
		staged:    truncateWorkspaceDiff(staged, workspaceChangesMaxBytes),
		unstaged:  truncateWorkspaceDiff(unstaged, workspaceChangesMaxBytes),
		untracked: truncateWorkspaceDiff(preview, workspaceChangesMaxBytes),
		truncated: stagedCut || unstagedCut || previewCut ||
			len(staged) > workspaceChangesMaxBytes || len(unstaged) > workspaceChangesMaxBytes ||
			len(preview) > workspaceChangesMaxBytes,
		// The git diff outputs carry git's own marker, while an untracked preview
		// is the file's own bytes, so only a NUL there means unreadable. A saved
		// .patch or .diff that merely quotes that marker stays readable.
		binary: workspaceTextIsBinary(staged) || workspaceTextIsBinary(unstaged) ||
			strings.IndexByte(preview, 0) >= 0,
	}
	if content.binary && untracked {
		content.untracked = ""
		// The hidden preview is not on screen, so its size says nothing about
		// what the user can read.
		content.truncated = stagedCut || unstagedCut ||
			len(staged) > workspaceChangesMaxBytes || len(unstaged) > workspaceChangesMaxBytes
	}
	return content
}

func (s *appState) loadWorkspaceDetail(path string) {
	s.cancelWorkspaceDetail()
	repo := s.workspaceChangesRepo
	if repo == "" {
		return
	}
	file := s.workspaceChangesFileByPath(path)
	if file == nil {
		return
	}
	requestID := s.workspaceDetailRequestID + 1
	s.workspaceDetailRequestID = requestID
	s.workspaceDetailLoading = true
	s.clearWorkspaceDetailFields()
	ctx, cancel := context.WithCancel(context.Background())
	s.workspaceDetailCancel = cancel
	go func() {
		staged, stagedCut, stagedErr := workspaceStagedDiff(ctx, repo, *file, workspaceChangesMaxBytes)
		unstaged, unstagedCut, unstagedErr := workspaceUnstagedDiff(ctx, repo, *file, workspaceChangesMaxBytes)
		preview := ""
		previewCut := false
		var previewErr error
		if file.Untracked {
			preview, previewCut, previewErr = workspaceUntrackedPreviewText(filepath.Join(repo, file.Path), workspaceChangesMaxRead)
		}
		s.postUI(func() {
			if ctx.Err() != nil || requestID != s.workspaceDetailRequestID || !s.workspaceChangesOpen {
				return
			}
			s.workspaceDetailLoading = false
			s.workspaceDetailCancel = nil
			if stagedErr != nil {
				s.workspaceDetailErrorKind = workspaceErrorKind(stagedErr)
				s.workspaceDetailErrorText = uiErrorSummary(stagedErr, text("errorUnknown"), workspaceChangesErrorLimit)
				return
			}
			if unstagedErr != nil {
				s.workspaceDetailErrorKind = workspaceErrorKind(unstagedErr)
				s.workspaceDetailErrorText = uiErrorSummary(unstagedErr, text("errorUnknown"), workspaceChangesErrorLimit)
				return
			}
			if previewErr != nil {
				s.workspaceDetailErrorKind = workspaceErrorKind(previewErr)
				s.workspaceDetailErrorText = uiErrorSummary(previewErr, text("errorUnknown"), workspaceChangesErrorLimit)
				return
			}
			s.workspaceDetailErrorKind = ""
			s.workspaceDetailErrorText = ""
			content := workspaceDetailContentFrom(staged, stagedCut, unstaged, unstagedCut,
				preview, previewCut, file.Untracked)
			s.workspaceDetailStagedDiff = content.staged
			s.workspaceDetailUnstagedDiff = content.unstaged
			s.workspaceDetailUntracked = content.untracked
			s.workspaceDetailTruncated = content.truncated
			s.workspaceDetailBinary = content.binary
		})
	}()
}

// cancelWorkspaceChanges stops both requests. Closing the panel and quitting
// the app take this path so no git process outlives either of them.
func (s *appState) cancelWorkspaceChanges() {
	s.cancelWorkspaceList()
	s.cancelWorkspaceDetail()
}

func (s *appState) cancelWorkspaceDetail() {
	if s.workspaceDetailCancel != nil {
		s.workspaceDetailCancel()
		s.workspaceDetailCancel = nil
	}
	s.workspaceDetailRequestID++
	s.workspaceDetailLoading = false
}

func (s *appState) clearWorkspaceDetailSelection() {
	s.cancelWorkspaceDetail()
	s.workspaceChangesDetail = ""
	s.clearWorkspaceDetailFields()
}

func (s *appState) clearWorkspaceDetailFields() {
	s.workspaceDetailStagedDiff = ""
	s.workspaceDetailUnstagedDiff = ""
	s.workspaceDetailUntracked = ""
	s.workspaceDetailErrorKind = ""
	s.workspaceDetailErrorText = ""
	s.workspaceDetailTruncated = false
	s.workspaceDetailBinary = false
}

func (s *appState) cancelWorkspaceList() {
	if s.workspaceChangesCancel != nil {
		s.workspaceChangesCancel()
		s.workspaceChangesCancel = nil
	}
	s.workspaceChangesRequestID++
	s.workspaceChangesLoading = false
}

func (s *appState) toggleWorkspaceChanges() {
	s.workspaceChangesOpen = !s.workspaceChangesOpen
	if s.workspaceChangesOpen {
		s.refreshWorkspaceChanges()
	} else {
		s.clearWorkspaceDetailSelection()
		s.cancelWorkspaceChanges()
	}
}

// syncWorkspaceChangesToWorkspace keeps the open panel on the current
// workspace. piCwd changes when Pi connects or a channel refresh replaces the
// process, and it resets when the session is cleared, so an open panel must
// reload or clear instead of keeping the previous repository's changes.
func (s *appState) syncWorkspaceChangesToWorkspace() {
	if !s.workspaceChangesOpen {
		return
	}
	s.refreshWorkspaceChanges()
}

// clearWorkspaceChanges forgets everything from the previous workspace. It is
// the reset path: the panel may stay open and shows the empty state until the
// next workspace resolves.
func (s *appState) clearWorkspaceChanges() {
	s.cancelWorkspaceChanges()
	s.workspaceChangesFiles = nil
	s.workspaceChangesRepo = ""
	s.workspaceChangesErrorKind = ""
	s.workspaceChangesErrorText = ""
	s.workspaceChangesTruncated = false
	s.clearWorkspaceDetailSelection()
}

// setPiCwd records the workspace of the current Pi process so callers do not
// miss the panel sync that a workspace switch needs.
func (s *appState) setPiCwd(cwd string) {
	if s.piCwd == cwd {
		return
	}
	s.piCwd = cwd
	s.syncWorkspaceChangesToWorkspace()
}

// resetPiCwd drops the workspace and the changes that belong to it.
func (s *appState) resetPiCwd() {
	s.piCwd = ""
	s.clearWorkspaceChanges()
}
