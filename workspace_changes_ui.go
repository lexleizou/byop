package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

func (s *appState) workspaceChangesPanel(c *ui.Context, p palette) {
	if !s.workspaceChangesOpen {
		return
	}
	overlay := ui.Column(c).Absolute().Top(0).Right(0).Bottom(0).
		Width(380).Padding(12).Background(p.surface).Gap(8)
	overlay.Children(func() {
		header := ui.Row(c).Gap(8).AlignItems(ui.Center)
		header.Children(func() {
			ui.Text(c, text("workspaceChangesTitle")).FontSize(fontBody).FontWeight(600).Grow(1)
			ui.Text(c, text("workspaceChangesReadOnly")).FontSize(fontCaption).TextColor(p.muted)
			refreshButton := ui.ButtonBase(c.Key("workspace-refresh")).Height(28).Padding(0, 12).Label(text("workspaceChangesRefresh"))
			refreshButton.Children(func() {
				ui.Text(c, text("workspaceChangesRefresh")).FontSize(fontSmall)
			})
			if refreshButton.Clicked() {
				s.refreshWorkspaceChanges()
			}
			closeButton := ui.ButtonBase(c.Key("workspace-close")).Height(28).Padding(0, 12).Label(text("workspaceChangesClose"))
			closeButton.Children(func() {
				ui.Text(c, text("workspaceChangesClose")).FontSize(fontSmall)
			})
			if closeButton.Clicked() {
				s.toggleWorkspaceChanges()
			}
		})
		if s.workspaceChangesErrorKind != "" {
			if s.workspaceChangesErrorKind == "notGit" {
				ui.Text(c, text("workspaceChangesNotGit")).FontSize(fontSmall).TextColor(p.muted)
			} else {
				ui.Text(c, textArgs("workspaceChangesGitFailed", map[string]any{"error": s.workspaceChangesErrorText})).FontSize(fontSmall).TextColor(p.muted)
			}
			return
		}
		if s.workspaceChangesLoading {
			ui.Text(c, text("workspaceChangesLoading")).FontSize(fontSmall).TextColor(p.muted)
		}
		ui.Text(c, textArgs("workspaceChangesRepository", map[string]any{"path": s.workspaceChangesRepo})).FontSize(fontCaption).TextColor(p.muted)
		if s.workspaceChangesTruncated {
			ui.Text(c, text("workspaceChangesLarge")).FontSize(fontCaption).TextColor(p.muted)
		}
		ui.Scroll(c).Fill().Children(func() {
			ui.Column(c).Gap(4).FillWidth().Children(func() {
				for _, file := range s.workspaceChangesFiles {
					label := file.Path
					if file.OriginalPath != "" {
						label = file.OriginalPath + " → " + file.Path
					}
					status := strings.TrimSpace(file.StagedStatus + file.UnstagedStatus)
					if status == "" {
						status = "?"
					}
					// One line per row: a long path ends with an ellipsis and the
					// full path stays reachable through the tooltip.
					row := ui.ButtonBase(c.Key("workspace-file-"+label)).FillWidth().Height(28).Padding(4, 8).
						Gap(6).AlignItems(ui.Center).
						Label(label + " " + status).Tooltip(label)
					row.Children(func() {
						ui.Text(c, status).Shrink(0).SingleLine().FontSize(fontSmall).TextColor(p.muted)
						ui.Text(c, label).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").FontSize(fontSmall)
					})
					if row.Clicked() {
						s.workspaceChangesDetail = file.Path
						s.loadWorkspaceDetail(file.Path)
					}
				}
			})
		})
		if s.workspaceChangesDetail != "" {
			file := s.workspaceChangesFileByPath(s.workspaceChangesDetail)
			if file != nil {
				ui.Scroll(c).Fill().Children(func() {
					detail := ui.Column(c).FillWidth().Gap(4).Selectable()
					detail.Children(func() {
						if s.workspaceDetailErrorKind != "" {
							ui.Text(c, textArgs("workspaceChangesGitFailed", map[string]any{"error": s.workspaceDetailErrorText})).FontSize(fontSmall).TextColor(p.muted)
						} else if s.workspaceDetailLoading {
							ui.Text(c, text("workspaceChangesLoading")).FontSize(fontSmall).TextColor(p.muted)
						} else {
							if s.workspaceDetailBinary {
								ui.Text(c, text("workspaceChangesBinary")).FontSize(fontSmall).TextColor(p.muted)
							}
							// Each section renders only when it has text, so an
							// untracked file no longer shows empty diff headers.
							if s.workspaceDetailStagedDiff != "" {
								ui.Text(c, text("workspaceChangesStagedDiff")).FontSize(fontCaption).FontWeight(600)
								ui.Text(c, s.workspaceDetailStagedDiff).FontSize(fontBody).Font("monospace")
							}
							if s.workspaceDetailUnstagedDiff != "" {
								ui.Text(c, text("workspaceChangesUnstagedDiff")).FontSize(fontCaption).FontWeight(600)
								ui.Text(c, s.workspaceDetailUnstagedDiff).FontSize(fontBody).Font("monospace")
							}
							if file.Untracked && s.workspaceDetailUntracked != "" {
								ui.Text(c, text("workspaceChangesUntrackedPreview")).FontSize(fontCaption).FontWeight(600)
								ui.Text(c, s.workspaceDetailUntracked).FontSize(fontBody).Font("monospace")
							}
							if s.workspaceDetailTruncated {
								ui.Text(c, text("workspaceChangesLarge")).FontSize(fontCaption).TextColor(p.muted)
							}
							if s.workspaceDetailStagedDiff == "" && s.workspaceDetailUnstagedDiff == "" &&
								s.workspaceDetailUntracked == "" && !s.workspaceDetailBinary {
								ui.Text(c, text("workspaceChangesNoDiff")).FontSize(fontSmall).TextColor(p.muted)
							}
						}
					})
				})
			}
		}
	})
}

func (s *appState) workspaceChangesFileByPath(path string) *workspaceFileChange {
	for index := range s.workspaceChangesFiles {
		if s.workspaceChangesFiles[index].Path == path {
			return &s.workspaceChangesFiles[index]
		}
	}
	return nil
}
