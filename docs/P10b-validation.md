# P10b Validation

Date: 2026-10-23

## Verified

- The workbench canvas uses 8px padding and gaps. The title bar, navigation,
  main area, and task panel are separate surface cards with a 1px border,
  12px radius, and subtle container shadow. The previous region separator
  borders are removed.
- The P10a flex-height chain is preserved: the navigation content remains
  `Grow(1).MinHeight(0)`, the footer remains `Shrink(0)`, and the main/chat
  height remains bounded.
- The collapsed task-panel gap is part of the animated wrapper, so closing the
  panel leaves neither an empty card nor an 8px space.
- The title bar shows the current Pi session name, local temporary title, or
  localized New chat title in one centered 14px ellipsized line.
- Drag handling covers the traffic-light area, the two flexible blank spacers
  around the title, and the empty title-bar background. The traffic lights and
  title-bar content share the same vertically centered row. The logo, title,
  title menu, and panel toggles are outside the drag regions.
  `TestP10bTitleMenuReceivesClicks` opens the title menu in the MyGo headless
  renderer and chooses Rename, confirming the child control receives the click.
- Rename uses an inline 14px field. Enter saves through
  `set_session_name`, Escape cancels, and the title refreshes after state
  re-read.
- A first send immediately sets a 40-rune temporary title. The first completed
  turn starts the separate 20-second Pi CLI naming job; context rename uses the
  latest six messages and may replace an existing name. Stale results are
  rejected by session path/id, generation, epoch, and client checks.
- Settings reports app version `0.10.0` and the short VCS revision, falling back
  to `dev`.
- Real MyGo window captures at 1100×560 and 1440×900 in both light and dark
  themes show the complete card shell, title controls, composer, and Pi footer:
  - `docs/screenshots/p10b-light-1100x560.png`
  - `docs/screenshots/p10b-dark-1100x560.png`
  - `docs/screenshots/p10b-light-1440x900.png`
  - `docs/screenshots/p10b-dark-1440x900.png`
- After the macOS console session locked, the open task-panel layouts in both
  themes were rendered with the MyGo headless test renderer at 1100×560. These
  are renderer evidence, not native-window screenshots:
  - `docs/screenshots/p10b-task-panel-open-light-1100x560.png`
  - `docs/screenshots/p10b-task-panel-open-dark-1100x560.png`
- A real Pi 1.4.0 run through the production `runSessionNamingCLI` helper used
  the default magpie provider with no explicit model override. It returned a
  sanitized title in about two seconds.
- A real Pi 1.4.0 JSONL RPC run completed a `set_session_name` round trip and
  read the new name back through `get_state`.
- `go run github.com/egoist/mygo/cmd/mygo generate` completed.
- `go run github.com/egoist/mygo/cmd/mygo i18n check .` reported 604 messages in
  both `en` and `zh-Hans`.
- `gofmt -l .` returned no files.
- `go vet ./...` completed successfully.
- `go build -o byop .` completed successfully.
- `go test ./... -count=1` passed 238 tests in 3 packages.
- `git diff --check` returned no errors.

## Opus Re-review Fixes

- P1 stale session naming: every session operation now invalidates the naming
  generation. Naming callbacks, manual save, and context naming return while a
  session operation is busy. Cross-session writes require a fresh Pi
  `get_state` with the same session id, and automatic naming additionally
  requires Pi's session name to still be empty.
- Added a direct unit test that starts with a pending name, switches sessions,
  and asserts the stale request makes zero `set_session_name` calls.
- P2 naming subprocess cleanup: `cmd.WaitDelay` is now two seconds.
- P3 title state: reconnect and process-exit paths clear both the Pi session
  name and local title.
- P3 chrome and menus: the traffic-light gap is draggable, inline rename cancels
  on outside press, and rename menu items are disabled while a session
  operation is busy.
- P3 surfaces: content cards, plugin cards, and the task panel use
  `shadowCard`; `shadowOverlay` remains reserved for platform-managed overlays.
- P3 naming hygiene: title cleanup removes `「」`, `《》`, and `Title:` /
  `标题：` prefixes, strips only terminal sentence punctuation, and preserves
  characters such as `)`. The per-session automatic-name attempt map is
  bounded to 256 entries.
- P3 task panel: an animated width below one pixel now renders no panel
  content.

## Unverified

- The macOS console session auto-locked during the walkthrough. Window-only
  screenshots continued to work, but Accessibility reads and synthetic clicks
  into the native popup menu were blocked. The menu interaction is therefore
  verified by the headless MyGo test rather than a screenshot of the open menu.
- The complete UI sequence from a real prompt through settled turn to the
  asynchronously renamed title was not driven through the locked desktop. Its
  two external paths were exercised against real Pi: the production naming CLI
  helper and the production `set_session_name` RPC method.
- The open task-panel light and dark captures are MyGo headless renders. The
  desktop locked before those two screenshots could be retaken as native
  windows.
- Linux and Windows window behavior were not exercised.
- Remaining P3: the session-naming prompt is still passed as a command-line
  argument to the Pi CLI, so it can be visible in local process arguments.

## MyGo Limits

- MyGo menus and popovers use platform-managed native shadows. The
  `workbench.shadow.overlay` token documents the intended shadow, but the menu
  API does not expose per-popover shadow styling.
- The native app exposed no usable accessibility window while the macOS console
  session was locked, so window-local coordinate clicks could not be verified
  in that state.

## Opus 5.5 复审（第二轮）

无新的 P0–P2。剩余 P3（不阻断提交）：

- 自动命名提交中点击重命名时，`invalidateSessionNaming()` 会清除提交中状态，写前检查与写入之间存在极短窗口。
- 任务面板宽度 <1 时直接返回，依赖空列默认尺寸，未显式 `Width(0)`。
- `shadowOverlay` 仅作 token 记录，MyGo 原生浮层不支持逐个设置阴影。
