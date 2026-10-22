# P10d Validation

Date: 2026-10-23

## Implemented

- After the first window is ready, BYOP starts one asynchronous legacy scan.
  The scan work runs outside the UI path and does not block the first frame.
- Chat shows a one-time migration prompt only when the scan succeeds, at least
  one action is importable, and
  `legacyMigrationPromptDismissed` is not set. Importing or choosing
  "Don't import" stores the dismissed preference. A failed import leaves the
  prompt available when preferences were not saved.
- The persistent migration card and its preview UI were removed. Settings only
  shows a re-import text link when the scan found the old app directory, even
  after a successful import.
- Legacy scan errors are shown as a caption at the re-import control position
  in Settings and do not create a Chat notice.
- Legacy scan failure is stored as a language-independent boolean. The visible
  message is resolved during rendering, so switching languages updates it.
- The External Pi CLI card now contains only connection status and action, CLI
  path, and version. Raw `piError` text is collapsed by default and can be
  expanded as small monospace text only while a Pi connection error is active.
  New connection attempts, connection failures, a successful handshake,
  disconnects, process exits, and reconnects close the detail disclosure.
- Workbench preferences became a default-collapsed Diagnostics card. It contains
  the existing runtime, RPC, Pi ownership, plugin, and WebView rows plus legacy
  scan and preference warnings.
- The Settings detail and re-import text buttons align their contents to the
  start and shrink to the label width.
- The obsolete in-memory legacy import display fields and their assignments were
  removed.
- Legacy preview helpers, the removed Pi row helpers, and their unused locale
  messages were deleted. The generated locale catalog was rebuilt.
- The P10a-P10c layout chain remains unchanged: navigation uses
  `Grow(1).MinHeight(0)`, the navigation footer uses `Shrink(0)`, main/chat are
  height-bounded, and body copy remains 14px.

## Automated Tests

`p10d_test.go` adds focused coverage for:

- Migration prompt visibility for importable, dismissed, failed-scan, and
  up-to-date states.
- Persistence when choosing not to import.
- Persistence and prompt dismissal after a successful import.
- Raw Pi connection details render for an active connection error and do not
  render for a connected state or a connected business error, even when stale
  detail state remains in memory.

The existing migration tests continue to exercise the read-only scanner,
import actions, conflict handling, and preference persistence.

## Checks

The final command results are:

- `go run github.com/egoist/mygo/cmd/mygo generate`: completed and wrote
  `locales/catalog_gen.go`.
- `go run github.com/egoist/mygo/cmd/mygo i18n check .`: 533 messages in both
  `en` and `zh-Hans`.
- `gofmt -l .`: no files.
- `go vet ./...`: passed.
- `go build -o byop .`: passed.
- `go test ./... -count=1`: 266 tests passed in 3 packages.
- `git diff --check`: no errors.

## Visual Evidence

The macOS console was locked, so native-window screenshots could not be
captured. The following images were rendered with the MyGo headless tester
using the production `appState.view` and controlled, non-private fixture data:

- `docs/screenshots/p10d-settings-light.png` (1180x1100)
- `docs/screenshots/p10d-settings-dark.png` (1180x1100)
- `docs/screenshots/p10d-chat-migration-prompt.png` (1180x760)

The Settings renders use a constructed but production-path-compatible
`errorPiNotExecutable` state with an expanded raw error detail. They are scrolled
to show the compact External Pi CLI card, left-aligned details and re-import
links, default-collapsed Diagnostics card, and About version. The Chat render
uses a constructed importable scan result to show the prompt above the
transcript. The same images are also stored in
`~/AgentScratch/20261022-byop-p10d/evidence/`.

## Unverified and Limits

- Native-window appearance, macOS traffic lights, title-bar integration,
  window layering, animation, scrolling, and Accessibility behavior were not
  checked while the desktop was locked.
- The prompt was visually exercised with constructed scan state. Real local
  legacy-data detection was covered by the scanner and migration unit tests,
  but no native end-to-end click-through was performed against this Mac's user
  data.
- The MyGo headless renderer does not include the native window frame,
  traffic-light controls, platform menu layers, or native popover shadows.
- The screenshots show English UI. The synchronized `zh-Hans` strings pass the
  catalog check, but were not captured in a separate headless image.
- Linux and Windows behavior were not exercised.

## Opus 复审结论

- P2（已连接时残留连接错误详情）和 4 个 P3 已修复，未发现新的 P0–P2；迁移相关测试另跑 `-race` 通过。
- 剩余 P3（不影响逻辑）：设置页 headless 截图的 CLI 路径显示 “resolved from PATH”，而受控错误文本为 `BYOP_PI_PATH` 指向目录，两者来自不同测试数据。
