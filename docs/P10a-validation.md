# P10a Validation

Date: 2026-10-23

## Verified

- The per-turn "Pi finished" notice and its locale key were removed.
- Connection failures use localized error codes for not found, not executable,
  handshake timeout, process exit, and unknown errors. Unknown errors retain
  the raw detail in the popover.
- A directory configured through `BYOP_PI_PATH` is classified as
  `errorPiNotExecutable`.
- Pi RPC process-exit failures preserve the `errPiProcessExited` sentinel
  through `errors.Is` and classify as `errorPiExited`.
- Connection callbacks ignore stale epochs, and connecting notices are cleared
  only when the current notice is `noticePiConnecting`.
- The Chat connection alert is limited to two lines and the transcript content
  width. The sidebar status control has a distinct accessible label from the
  composer chip, and its popover uses `piPathLabel()` and muted raw error text.
- The sidebar navigation content has a bounded flex height and the footer is
  fixed to its intrinsic height. Layout tests verify that all three rows remain
  fully visible at 1100×560, and that the first rows plus the Pi status and
  Settings controls remain visible with 40 sessions.
- Prompt blocks followed by adjacent tool-only assistant messages render in one
  8px-spaced group. A unit test covers the reloaded
  user → tool → tool → text sequence.
- Direct layout tests verify the completed follow-up card, running queue strip,
  connection alert, composer controls, and composer HUD remain inside
  1100×560 and 1180×760 viewports.
- Real MyGo window walkthrough on macOS, light theme, using temporary seeded
  render state:
  - Completed long transcript with follow-up card at 1100×560:
    `docs/screenshots/p10a-composer-followup-1100x560.png`
  - Running long transcript with queue strip at 1180×760:
    `docs/screenshots/p10a-composer-running-1180x760.png`
  - Connection error alert at 1100×560:
    `docs/screenshots/p10a-connection-error-1100x560.png`
  - Expanded sidebar with 40 sessions at 1100×560:
    `docs/screenshots/p10a-sidebar-40-sessions-1100x560.png`
- `go run github.com/egoist/mygo/cmd/mygo generate` completed.
- `go run github.com/egoist/mygo/cmd/mygo i18n check .` reported 599 messages
  in both `en` and `zh-Hans`.
- `gofmt -l .` returned no files.
- `go vet ./...` completed successfully.
- `go build -o byop .` completed successfully.
- `go test ./... -count=1` passed 223 tests in 3 packages.
- `git diff --check` returned no errors.

## Composer Root Cause

The transcript's intrinsic height escaped the window's finite flex height. The
outer body Row and main content Column did not establish a bounded height chain,
so the floating conversation received an over-tall containing block and placed
its absolute bottom composer and footer below the visible window. `Clip()` only
hid the overflow. The fix makes the body Row, main Column, and Chat page finite
and shrinkable so the transcript scrolls inside the remaining height while the
composer and HUD stay anchored above the window bottom.

## Unverified

- The macOS desktop session was locked during the walkthrough, so the status
  popover was not clicked through Computer Use. Its path, localized error, and
  muted raw-detail rows were verified by code and layout tests rather than an
  interactive popover screenshot.
- No live Pi CLI process was connected during the final window walkthrough.
  Runtime process-exit and classification behavior is covered by unit tests.
- Linux and Windows window behavior were not exercised.

## Known Limits

- The real-window screenshots use temporary seeded state so the review states
  can be captured without a connected Pi account. The seed code was removed
  before the final checks.
- Raw Pi and provider error text remains verbatim.
