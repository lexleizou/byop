package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

func (s *appState) chatPage(c *ui.Context, p palette) {
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Height(58).FillWidth().Padding(0, 24).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
			ui.Column(c).Gap(1).Grow(1).Children(func() {
				ui.Text(c, "Chat").FontSize(fontUILarge).FontWeight(650).TextColor(p.text)
				ui.Text(c, "Native conversation surface · JSONL RPC").FontSize(fontCaption).TextColor(p.muted)
			})
			statusPill(c, p, s.runLabel(), s.runTone(p))
		})

		ui.Scroll(c).Grow(1).Children(func() {
			ui.Row(c).FillWidth().Justify(ui.Center).Padding(26, 24).Children(func() {
				ui.Column(c).Grow(1).MaxWidth(760).Gap(18).Children(func() {
					ui.Column(c).Gap(5).Children(func() {
						ui.Text(c, "How can I help you build?").
							FontSize(fontDisplay).FontWeight(650).TextColor(p.text)
						ui.Text(c, "Messages are sent to the separately installed Pi CLI. Pi owns the model run; BYOP renders the native workbench.").
							FontSize(fontBody).LineHeight(1.45).TextColor(p.muted).MaxWidth(650)
					})

					for i := range s.chat {
						message := &s.chat[i]
						switch message.role {
						case chatUser:
							userMessage(c, p, message.text)
						case chatAssistant:
							assistantMessage(c, p, message)
						}
					}

					if s.piConn != piConnected && len(s.chat) == 0 {
						ui.Column(c).Gap(6).Padding(12, 14).Radius(10).
							Background(p.surface).Border(1, p.border).Children(func() {
							ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
								ui.Icon(c, icon("terminal")).Size(15, 15).TextColor(s.piTone(p))
								ui.Text(c, "External Pi CLI").FontSize(fontSmall).FontWeight(650).TextColor(p.text)
							})
							ui.Text(c, s.piDiagnostic()).FontSize(fontSmall).LineHeight(1.45).TextColor(p.muted)
						})
					}
				})
			})
		})

		s.composer(c, p)
	})
}

func userMessage(c *ui.Context, p palette, text string) {
	ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
		ui.Text(c, text).Selectable().FontSize(fontBody).LineHeight(1.4).TextColor(p.text).
			Padding(10, 14).Radius(18).Background(p.surfaceHover).MaxWidth(560)
	})
}

func assistantMessage(c *ui.Context, p palette, message *chatMessage) {
	ui.Row(c).Gap(11).AlignItems(ui.Start).Children(func() {
		assistantAvatar(c, p)
		ui.Column(c).Grow(1).Gap(7).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, appShort).FontSize(fontSmall).FontWeight(700).TextColor(p.text)
				if message.status != "" {
					ui.Text(c, message.status).FontSize(fontCaption).TextColor(p.muted)
				}
			})
			if message.text != "" || len(message.toolCalls) == 0 {
				ui.Text(c, fallback(message.text, "Waiting for Pi…")).
					Selectable().FontSize(fontBody).LineHeight(1.52).TextColor(p.text).MaxWidth(680)
			}
			for i := range message.toolCalls {
				toolCallCard(c, p, &message.toolCalls[i])
			}
		})
	})
}

func assistantAvatar(c *ui.Context, p palette) {
	ui.Row(c).Size(30, 30).Radius(9).Background(p.accent).Center().Children(func() {
		ui.Icon(c, icon("pi")).Size(17, 17).TextColor(p.accentText)
	})
}

func toolCallCard(c *ui.Context, p palette, tool *toolCallView) {
	tone := toolCallTone(p, tool.status)
	ui.Column(c).FillWidth().Gap(0).Radius(10).
		Border(1, p.border).Background(p.surface).Children(func() {
		parts := ui.CollapsibleBase(c, &tool.expanded)
		parts.Trigger.FillWidth().Gap(9).AlignItems(ui.Start).Padding(10, 12).
			Cursor(ui.CursorPointer).
			Children(func() {
				ui.Row(c).Size(28, 28).Shrink(0).Radius(7).
					Background(p.surfaceHover).Center().Children(func() {
					ui.Icon(c, icon(toolCallIcon(tool.name))).Size(15, 15).TextColor(tone)
				})
				ui.Column(c).Grow(1).Gap(3).Children(func() {
					ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, tool.name).Font("monospace").FontSize(fontSmall).
							FontWeight(650).TextColor(p.text).Grow(1).SingleLine()
						statusPill(c, p, toolCallStatusLabel(tool.status), tone)
					})
					ui.Text(c, fallback(oneLine(tool.arguments), "No argument summary.")).
						FontSize(fontCaption).TextColor(p.muted).SingleLine().MaxWidth(590)
					if summary := toolCallResultSummary(tool); summary != "" {
						ui.Text(c, summary).FontSize(fontCaption).
							TextColor(p.muted).SingleLine().MaxWidth(590)
					}
				})
				ui.Column(c).Gap(3).AlignItems(ui.End).Shrink(0).Children(func() {
					if tool.durationMS > 0 {
						ui.Text(c, formatToolDuration(tool.durationMS)).
							Font("monospace").FontSize(fontMicro).TextColor(p.muted)
					}
					ui.Icon(c, icon("chevron-down")).Size(14, 14).
						TextColor(p.muted).Rotate(180 * parts.Progress())
				})
			})
		parts.Panel(func() {
			ui.Column(c).FillWidth().Gap(9).Padding(10, 12).
				BorderWidth(1, 0, 0, 0).BorderColor(p.border).Children(func() {
				toolDetail(c, p, "ARGUMENTS", fallback(tool.arguments, "No arguments"))
				if result := fallback(tool.result, tool.partial); result != "" {
					toolDetail(c, p, "RESULT", result)
				}
			})
		})
	})
}

func toolDetail(c *ui.Context, p palette, label, text string) {
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		ui.Text(c, label).FontSize(fontMicro).FontWeight(700).TextColor(p.muted)
		ui.Text(c, text).Selectable().Font("monospace").FontSize(fontSmall).
			LineHeight(1.45).TextColor(p.text).MaxWidth(650)
	})
}

func toolCallResultSummary(tool *toolCallView) string {
	text := fallback(tool.result, tool.partial)
	if text == "" {
		switch tool.status {
		case toolCallRunning:
			return "Waiting for tool output…"
		case toolCallCancelled:
			return "Tool was cancelled before completion."
		case toolCallInterrupted:
			return "Tool was interrupted when Pi disconnected."
		}
		return ""
	}
	return summarizeLine(text, 150)
}

func summarizeLine(text string, limit int) string {
	line := oneLine(text)
	runes := []rune(line)
	if len(runes) <= limit {
		return line
	}
	return string(runes[:limit]) + "…"
}

func toolCallIcon(name string) string {
	switch strings.ToLower(name) {
	case "bash", "shell", "terminal":
		return "terminal"
	case "read", "write", "edit", "file":
		return "file"
	case "grep", "find", "search":
		return "search"
	case "git":
		return "git"
	default:
		return "play"
	}
}

func toolCallTone(p palette, status toolCallStatus) ui.Color {
	switch status {
	case toolCallRunning:
		return p.warning
	case toolCallCompleted:
		return p.success
	case toolCallFailed, toolCallInterrupted:
		return p.danger
	default:
		return p.muted
	}
}

func (s *appState) composer(c *ui.Context, p palette) {
	ui.Row(c).FillWidth().Justify(ui.Center).Padding(10, 24, 14, 24).
		BorderWidth(1, 0, 0, 0).BorderColor(p.border).Children(func() {
		ui.Column(c).Grow(1).MaxWidth(760).Gap(8).Padding(10).Radius(14).
			Background(p.surface).Border(1, p.border).Shadow(0, 8, 24, 0, p.shadow).
			Children(func() {
				input := ui.TextAreaBase(c.Key("composer"), &s.composerDraft).Height(52).
					Placeholder(composerPlaceholder(s.piConn)).FontSize(fontBody).TextColor(p.text).
					Background(ui.Color{}).Border(0, ui.Color{}).Padding(2, 3).
					Label("Message")

				ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
					iconButton(c, p, "attach", "paperclip", "Attach files · Not connected", false, func() {
						s.showNotice("Attachments remain outside the current RPC boundary.")
					})
					statusPill(c, p, s.piCompactLabel(), s.piTone(p))
					ui.Spacer(c)
					ui.Text(c, "⌘ Enter to send").FontSize(fontMicro).TextColor(p.muted)
					if s.run == runSubmitting || s.run == runStreaming || s.run == runStopping {
						stopButton(c, p, func() { s.stopPrompt() })
					} else {
						sendButton(c, p, func() { s.sendPrompt() })
					}
				})

				if input.Submitted() {
					s.sendPrompt()
				}
			})
	})
}

func composerPlaceholder(conn piConnectionState) string {
	switch conn {
	case piConnected:
		return "Ask Pi…"
	case piConnecting:
		return "Connecting to Pi…"
	default:
		return "Pi CLI is not connected"
	}
}

func (s *appState) pluginsPage(c *ui.Context, p palette) {
	ui.Scroll(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Justify(ui.Center).Padding(30, 28, 38, 28).Children(func() {
			ui.Column(c).Grow(1).MaxWidth(880).Gap(22).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.End).Gap(16).Children(func() {
					ui.Column(c).Grow(1).Children(func() {
						pageHeader(c, p, "WORKBENCH", "Plugins", "Independent tools contribute navigation entries, panels, commands, settings, and optional WebView views through one registry.")
					})
					primaryAction(c, p, "plugins-scan", "Scan plugins", "package", func() {
						s.showNotice("Plugin discovery is not connected yet. No files or packages were scanned.")
					})
				})

				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Column(c).Width(300).MinWidth(180).Children(func() {
						searchField(c, p, "plugin-search", &s.pluginSearch, "Search plugins")
					})
					statusPill(c, p, "0 CONNECTED", p.muted)
					ui.Spacer(c)
					secondaryAction(c, p, "plugin-dir", "Plugin folders", func() {
						s.showNotice("Plugin folder settings are reserved for P4. Nothing was opened.")
					})
				})

				sectionLabel(c, p, "STARTER CONTRIBUTIONS")
				ui.Row(c).Wrap().GapX(12).GapY(12).Children(func() {
					s.pluginCard(c, p, "Git Workspace", "git", "Repository status, diffs, branches, and review actions.",
						"Chat + side panel + commands", p.accent)
					s.pluginCard(c, p, "Document Studio", "document", "Structured writing, annotations, and document previews.",
						"Main view + WebView", p.success)
					s.pluginCard(c, p, "Run Inspector", "automations", "Inspect long-running processes, logs, and artifacts.",
						"Task bar + dashboard", p.warning)
				})

				sectionLabel(c, p, "PLUGIN CONTRACT PREVIEW")
				ui.Column(c).FillWidth().Gap(10).Padding(14).Radius(11).
					Border(1, p.border).Background(p.surface).Children(func() {
					ui.Text(c, "A plugin supplies a manifest, runtime entry point, and declared contributions. The host owns permissions, lifecycle, shared theme tokens, navigation, and task state.").
						FontSize(fontBody).LineHeight(1.5).TextColor(p.text)
					ui.Row(c).Wrap().Gap(7).Children(func() {
						contractChip(c, p, "commands")
						contractChip(c, p, "views")
						contractChip(c, p, "webviews")
						contractChip(c, p, "tasks")
						contractChip(c, p, "settings")
					})
				})
			})
		})
	})
}

func (s *appState) pluginCard(c *ui.Context, p palette, title, iconName, description, surface string, tone ui.Color) {
	card := ui.ButtonBase(c.Key("plugin-"+title)).Width(260).Grow(1).MinWidth(220).
		Column().AlignItems(ui.Start).Gap(10).Padding(14).Radius(11).
		Border(1, p.border).Background(p.surface).Cursor(ui.CursorPointer).
		Label(title).Tooltip("Preview plugin card")
	if card.Hovered() {
		card.Background(p.surfaceHover)
	}
	card.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Row(c).Size(34, 34).Radius(9).Background(p.surfaceHover).Center().Children(func() {
				ui.Icon(c, icon(iconName)).Size(17, 17).TextColor(tone)
			})
			ui.Spacer(c)
			statusPill(c, p, "PREVIEW", p.muted)
		})
		ui.Text(c, title).FontSize(fontBody).FontWeight(650).TextColor(p.text)
		ui.Text(c, description).FontSize(fontSmall).LineHeight(1.45).TextColor(p.muted)
		ui.Text(c, surface).FontSize(fontCaption).TextColor(tone)
	})
	if card.Clicked() {
		s.showNotice(title + " is a preview card. Plugin installation and execution are not connected.")
	}
}

func contractChip(c *ui.Context, p palette, label string) {
	ui.Row(c).Padding(4, 8).Radius(999).Background(p.surfaceHover).Gap(5).
		AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icon("check")).Size(11, 11).TextColor(p.success)
		ui.Text(c, label).Font("monospace").FontSize(fontMicro).TextColor(p.text)
	})
}

func (s *appState) libraryPage(c *ui.Context, p palette) {
	ui.Scroll(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Justify(ui.Center).Padding(30, 28, 38, 28).Children(func() {
			ui.Column(c).Grow(1).MaxWidth(880).Gap(22).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.End).Gap(16).Children(func() {
					ui.Column(c).Grow(1).Children(func() {
						pageHeader(c, p, "LIBRARY", "Library", "A source-aware index for files, sessions, artifacts, and references supplied by Pi or installed plugins.")
					})
					primaryAction(c, p, "library-import", "Import reference", "plus", func() {
						s.showNotice("Import is not connected yet. The file picker and index arrive after the plugin contract.")
					})
				})

				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Column(c).Width(300).MinWidth(180).Children(func() {
						searchField(c, p, "library-search", &s.librarySearch, "Search library")
					})
					ui.Spacer(c)
					statusPill(c, p, "INDEX OFFLINE", p.warning)
				})

				ui.Row(c).Wrap().GapX(12).GapY(12).Children(func() {
					s.libraryCard(c, p, "project-notes", "Research notes", "Document", "Example document", "Sample")
					s.libraryCard(c, p, "shell-spec", "Native shell specification", "File", "12 sections", "Sample")
					s.libraryCard(c, p, "session-index", "Pi session index", "Database", "Read-only session references", "Planned")
					s.libraryCard(c, p, "webview-proof", "Embedded view concept", "Folder", "Complex plugin surfaces", "Reserved for P5")
				})

				ui.Column(c).Gap(8).Padding(14).Radius(11).Background(p.surface).
					Border(1, p.border).Children(func() {
					ui.Text(c, "Library boundary").FontSize(fontBody).FontWeight(650).TextColor(p.text)
					ui.Text(c, "BYOP will store references and rebuildable indexes. Pi remains the source of truth for session JSONL and credentials.").
						FontSize(fontSmall).LineHeight(1.45).TextColor(p.muted)
				})
			})
		})
	})
}

func (s *appState) libraryCard(c *ui.Context, p palette, key, title, iconName, detail, badge string) {
	card := ui.ButtonBase(c.Key("library-"+key)).Width(260).Grow(1).MinWidth(210).
		Column().AlignItems(ui.Start).Gap(12).Padding(14).Radius(11).
		Border(1, p.border).Background(p.surface).Cursor(ui.CursorPointer).
		Label(title).Tooltip("Library item preview")
	if card.Hovered() {
		card.Background(p.surfaceHover)
	}
	card.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Row(c).Size(34, 34).Radius(9).Background(p.surfaceHover).Center().Children(func() {
				ui.Icon(c, icon(iconName)).Size(17, 17).TextColor(p.accent)
			})
			ui.Spacer(c)
			statusPill(c, p, badge, p.muted)
		})
		ui.Text(c, title).FontSize(fontBody).FontWeight(650).TextColor(p.text)
		ui.Text(c, detail).FontSize(fontCaption).TextColor(p.muted)
	})
	if card.Clicked() {
		s.showNotice(title + " is a sample item. No file or session was opened.")
	}
}

func (s *appState) documentPage(c *ui.Context, p palette) {
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Height(58).FillWidth().Padding(0, 24).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
			ui.Column(c).Gap(1).Grow(1).Children(func() {
				ui.Text(c, "Document").FontSize(fontUILarge).FontWeight(650).TextColor(p.text)
				ui.Text(c, "BYOP working surface").FontSize(fontCaption).TextColor(p.muted)
			})
			secondaryAction(c, p, "document-export", "Export", func() {
				s.showNotice("Export is not connected yet.")
			})
			ui.Spacer(c)
			statusPill(c, p, "UNSAVED SAMPLE", p.warning)
		})

		ui.Row(c).Grow(1).FillWidth().Justify(ui.Center).Padding(24).Children(func() {
			ui.Column(c).Grow(1).MaxWidth(720).FillHeight().Gap(12).Children(func() {
				ui.Column(c).Gap(6).Children(func() {
					ui.Text(c, "BYOP Native Shell Notes").FontSize(fontPageTitle).FontWeight(650).TextColor(p.text)
					ui.Text(c, "A complex document remains an optional WebView plugin surface. The native host keeps navigation, tasks, and shared chrome.").
						FontSize(fontBody).TextColor(p.muted)
				})
				ui.Box(c).Grow(1).FillWidth().MinHeight(260).Radius(12).
					Border(1, p.border).Background(p.surface).Padding(26).Children(func() {
					ui.Column(c).Gap(15).Children(func() {
						documentHeading(c, p, "1. Host-owned shell")
						documentParagraph(c, p, "The window, navigation, command surface, task bar, theme, and plugin lifecycle are native MyGo views. Plugins do not replace the workbench frame.")
						documentHeading(c, p, "2. External Pi")
						documentParagraph(c, p, "Pi stays installed separately. BYOP communicates through a long-lived CLI RPC adapter and never copies credentials or rewrites session JSONL.")
						documentHeading(c, p, "3. Complex views")
						documentParagraph(c, p, "Editors, diagrams, and rich previews can use a declared WebView contribution while keeping a native fallback for unsupported capabilities.")
					})
				})
			})
		})
	})
}

func documentHeading(c *ui.Context, p palette, text string) {
	ui.Text(c, text).FontSize(fontBodyLarge).FontWeight(650).TextColor(p.text)
}

func documentParagraph(c *ui.Context, p palette, text string) {
	ui.Text(c, text).Selectable().FontSize(fontBody).LineHeight(1.55).TextColor(p.muted)
}

func (s *appState) automationsPage(c *ui.Context, p palette) {
	ui.Scroll(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Justify(ui.Center).Padding(30, 28, 38, 28).Children(func() {
			ui.Column(c).Grow(1).MaxWidth(880).Gap(22).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.End).Gap(16).Children(func() {
					ui.Column(c).Grow(1).Children(func() {
						pageHeader(c, p, "AUTOMATE", "Automations", "Scheduled and event-driven work routed through installed plugins and the external Pi runtime.")
					})
					primaryAction(c, p, "automation-create", "New automation", "plus", func() {
						s.showNotice("Automation creation is not connected yet.")
					})
				})

				ui.Row(c).Wrap().Gap(12).Children(func() {
					s.automationCard(c, p, "Morning brief", "Weekdays · 08:30", "Library + Chat", true)
					s.automationCard(c, p, "Repository digest", "On demand", "Git plugin", false)
				})

				ui.Column(c).FillWidth().Gap(14).Padding(18).Radius(12).
					Border(1, p.border).Background(p.surface).Children(func() {
					ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, icon("clock")).Size(18, 18).TextColor(p.accent)
						ui.Text(c, "No scheduler connected").FontSize(fontBody).FontWeight(650).TextColor(p.text)
					})
					ui.Text(c, "The P0 inventory found only scheduler.lock. BYOP will present no job as runnable until a plugin runtime and permission model exist.").
						FontSize(fontSmall).LineHeight(1.5).TextColor(p.muted)
				})
			})
		})
	})
}

func (s *appState) automationCard(c *ui.Context, p palette, title, schedule, owner string, enabled bool) {
	ui.Column(c).Width(280).Grow(1).MinWidth(220).Gap(12).Padding(14).Radius(11).
		Border(1, p.border).Background(p.surface).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Row(c).Size(34, 34).Radius(9).Background(p.surfaceHover).Center().Children(func() {
				ui.Icon(c, icon("play")).Size(15, 15).TextColor(p.accent)
			})
			ui.Spacer(c)
			if enabled {
				statusPill(c, p, "PREVIEW ON", p.warning)
			} else {
				statusPill(c, p, "OFF", p.muted)
			}
		})
		ui.Text(c, title).FontSize(fontBody).FontWeight(650).TextColor(p.text)
		ui.Text(c, schedule).FontSize(fontSmall).TextColor(p.muted)
		ui.Text(c, "Owner · "+owner).FontSize(fontCaption).TextColor(p.accent)
	})
}

func (s *appState) settingsPage(c *ui.Context, p palette) {
	ui.Scroll(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Justify(ui.Center).Padding(30, 28, 38, 28).Children(func() {
			ui.Column(c).Grow(1).MaxWidth(820).Gap(22).Children(func() {
				pageHeader(c, p, "PREFERENCES", "Settings", "Theme and external runtime controls for the native BYOP workbench.")

				settingsCard(c, p, "Appearance", "System, Light, and Dark share the Onevra semantic colors and native text scale.", func() {
					ui.Row(c).Gap(8).Wrap().Children(func() {
						s.themeButton(c, p, themeSystem, "monitor", "System")
						s.themeButton(c, p, themeLight, "sun", "Light")
						s.themeButton(c, p, themeDark, "moon", "Dark")
					})
				})

				settingsCard(c, p, "External Pi CLI", "BYOP starts a separately installed Pi process and uses its versioned JSONL RPC protocol. It does not replace Pi with an embedded SDK.", func() {
					ui.Row(c).Gap(9).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Size(8, 8).Radius(4).Background(s.piTone(p))
						ui.Text(c, s.piConnectionLabel()).FontSize(fontSmall).TextColor(p.text).Grow(1)
						label := "Connect"
						if s.piConn == piConnected {
							label = "Reconnect"
						}
						secondaryAction(c, p, "settings-pi-connect", label, func() {
							s.reconnectPi()
						})
					})
					ui.Column(c).Gap(8).Children(func() {
						settingsValue(c, p, "CLI path", s.piPathLabel())
						settingsValue(c, p, "Version", fallback(s.piVersion, "Pending handshake"))
						settingsValue(c, p, "Model", fallback(s.piModel, "Pi default"))
						settingsValue(c, p, "Session", s.sessionLabel())
						settingsValue(c, p, "Run state", s.runLabel()+" · "+s.runDetail())
					})
					if s.piError != "" {
						ui.Text(c, s.piError).FontSize(fontCaption).LineHeight(1.4).TextColor(p.danger)
					}
				})

				settingsCard(c, p, "Workbench preferences", "Layout, drafts, and plugin view state will be stored by BYOP. Window bounds already use MyGo StateKey.", func() {
					ui.Column(c).Gap(8).Children(func() {
						settingsValue(c, p, "Native runtime", "MyGo · fixed baseline")
						settingsValue(c, p, "RPC transport", "Strict JSONL over stdin/stdout")
						settingsValue(c, p, "Pi ownership", "Sessions, models, credentials, extensions")
						settingsValue(c, p, "Plugin permission model", "Planned for P4")
						settingsValue(c, p, "Complex UI", "Native first, declared WebView fallback")
					})
				})

				settingsCard(c, p, "About", "Build You Own Pi is a multi-plugin workbench shell for an externally installed Pi CLI.", func() {
					ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
						brandMark(c, p, 38)
						ui.Column(c).Gap(1).Children(func() {
							ui.Text(c, appShort).FontSize(fontTitle).FontWeight(650).TextColor(p.text)
							ui.Text(c, "P3 structured tool events · 2026-10-20").FontSize(fontCaption).TextColor(p.muted)
						})
					})
				})
			})
		})
	})
}

func (s *appState) sessionLabel() string {
	if s.piSessionID == "" {
		return "Ephemeral / not assigned"
	}
	return "Active ephemeral session"
}

func settingsCard(c *ui.Context, p palette, title, description string, body func()) {
	ui.Column(c).FillWidth().Gap(13).Padding(16).Radius(12).
		Border(1, p.border).Background(p.surface).Children(func() {
		ui.Column(c).Gap(4).Children(func() {
			ui.Text(c, title).FontSize(fontBody).FontWeight(650).TextColor(p.text)
			ui.Text(c, description).FontSize(fontSmall).LineHeight(1.45).TextColor(p.muted)
		})
		body()
	})
}

func settingsValue(c *ui.Context, p palette, label, value string) {
	ui.Row(c).FillWidth().Padding(8, 10).Radius(7).Background(p.background).
		AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(fontCaption).TextColor(p.muted).Width(180)
		ui.Text(c, value).FontSize(fontCaption).FontWeight(600).TextColor(p.text).Grow(1)
	})
}

func (s *appState) themeButton(c *ui.Context, p palette, mode themeMode, iconName, label string) {
	selected := s.themeMode == mode
	bg := p.background
	fg := p.muted
	border := p.border
	if selected {
		bg = p.surfaceHover
		fg = p.accent
		border = p.accent
	}
	btn := ui.ButtonBase(c.Key("theme-"+mode)).Padding(8, 12).Radius(8).
		Border(1, border).Background(bg).Cursor(ui.CursorPointer).Label(label)
	if btn.Hovered() && !selected {
		btn.Background(p.surfaceHover)
	}
	btn.Children(func() {
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon(iconName)).Size(15, 15).TextColor(fg)
			ui.Text(c, label).FontSize(fontSmall).FontWeight(600).TextColor(fg)
			if selected {
				ui.Icon(c, icon("check")).Size(12, 12).TextColor(fg)
			}
		})
	})
	if btn.Clicked() {
		s.setTheme(mode)
	}
}
