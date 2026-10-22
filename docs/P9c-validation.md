# P9c Validation

Date: 2026-10-22

## Verified

- Session organization state is stored only in BYOP preferences, keyed by the
  canonical workspace and session path, with an ID fallback.
- Unit tests cover partition ordering, expired Later sessions, pinned reorder,
  archive/restore, single-key undo, and cross-workspace undo isolation. Refresh
  keeps an expired Later entry and its accent dot; a successful explicit open
  clears that entry.
- Organization refresh is read-only. Pinned, Later, and Archived records remain
  when the asynchronous or capped session list is empty or incomplete.
- Running-send tests cover default and opposite modes, preserving the active
  request for queued prompts, starting a new turn for `started` responses,
  restoring a draft after current or stale failures, and deduplicating the Pi
  user-message echo.
- Queue display tests cover the three-item limit and overflow count.
- Runtime tests cover queue clearing, preference normalization, panel duration,
  high-contrast palette selection and reduced-motion behavior.
- `locales/en.json` and `locales/zh-Hans.json` have matching non-metadata keys
  and are regenerated into `locales/catalog_gen.go`.
- `gofmt -l .`, `go vet ./...`, `go build -o byop .`,
  `go test ./... -count=1`, the targeted race tests, and `git diff --check`
  complete successfully for the final P9b/P9c worktree.
- Live window walkthrough on the pre-review-fix build with Pi CLI 1.4.0:
  clicking Send while Pi was running queued the message, and Pi delivered it
  after the current turn finished. The walkthrough found two issues, both
  fixed before commit: the default Sessions section header duplicated the
  parent header, and a running draft hid Stop.
- Settings shows the Contrast (System / Standard / High), Motion (System /
  Standard / Reduced) and Panel animation (0/150/250/400 ms) controls in
  English and Simplified Chinese (`docs/screenshots/p9b-settings-*.png`).
- Claude Opus 5.5 review: three rounds; final verdict PASS with no remaining
  P0–P2 findings.

## Final-Build Walkthrough (P9w)

Date: 2026-10-22. macOS, external Pi CLI 1.4.0, final post-review build.

| Flow | Result | Screenshot |
|---|---|---|
| Send while running (Queue default) | Queue strip shows the item; it runs after the current turn | `p9-queue-zh.png`, `p9-queue-en.png` |
| Steer from the queue strip | Notice "Sent Steer."; Clear queue available | `p9-queue-en.png` |
| Drag Sessions → Later, Later → empty Pinned, Pinned → empty Later | Moves with an Undo notice | `p9-drag-later-en.png`, `p9-sidebar-groups-en.png` |
| Drag Pinned → Sessions header | Returns the session to Sessions | `p9-drag-sessions-header-en.png` |
| Drop in the same group | No change saved, no Undo notice | — |
| Contrast, Motion, Panel animation settings | Shown in both languages and both themes | `p9-settings-*.png` |

Walkthrough fixes, developed by DeepSeek V4.1 Flash and reviewed by Claude
Opus 5.5 (final re-review PASS, no P0–P2 findings):

1. Drag-and-drop clears its state after the drop is handled, and the Sessions
   header is a drop target.
2. Empty Pinned/Later drop targets appear while dragging from any row, before
   Archived unless the source is Archived; only one Sessions target is shown.
3. Same-group drops are no-ops, based on the displayed group.
4. An expired Later session is treated as a Sessions row for drag and drop.

## Unverified

- Narrow window sizes, Linux and Windows.
- Esc cancel during a drag, and dragging into Archived.
- The expired Later reminder dot in a live window.
- Following the system contrast and reduced-motion settings live, and
  measured contrast ratios in an active plugin WebView.
- Queue/steer while a real tool call is running.

## Known Limits (Walkthrough)

- With "Show all" on and a long list, empty Pinned/Later drop targets can be
  outside the scroll area because the list does not autoscroll during a drag.
- Sidebar group ordering during a drag is covered by review and one test, not
  by exhaustive tests.
- Undo stays available for eight seconds.
- 「插话」 is kept for Steer instead of 「引导」.

## Known Limits

- Pi output, Pi errors and raw provider errors remain verbatim.
- Model and provider IDs and names, file paths, protocol identifiers, stable
  persisted enum and error-code IDs, and environment-variable names remain
  unchanged.
- Plugin text without a matching localization value remains in its original
  language.
- A notice banner that is already visible stores its translated text and keeps
  the previous language until replaced or dismissed after a locale switch.
- Session organization is BYOP metadata and is not written to Pi session files.
  Deleting BYOP preferences removes only the organization metadata.
- The application cannot prove that an accepted steer or follow-up has rendered
  into the active turn until Pi emits its corresponding queue and message
  events.
- An unknown prompt disposition restores the draft and reports the localized
  running-send failure notice.
- Simplified Chinese plural zero remains a language-pack limitation.
- Waiting-state detection depends on the localized waiting text.
- Running-with-draft Stop retains Onevra Composer's accent styling because the
  component does not expose a danger style for its primary action slot.
- During automatic context compaction, BYOP does not block queue/steer locally;
  Pi rejects the request, and BYOP restores the draft with a failure notice.
- The running Enter test mirrors the Composer input wiring instead of driving
  `chatComposer` directly.
