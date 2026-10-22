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

## Unverified

- Window walkthrough of the final post-review build: the screen was locked
  after the final fixes, so Stop/queue button placement, Enter while running,
  and the queue strip were verified by tests and review only.
- A manual live-window walkthrough of empty-section drops, hover actions,
  context menus, the eight-second undo notice and the expired Later dot.
- Visual comparison of the panel toggle group and panel animation durations at
  supported window sizes in English and Simplified Chinese.
- Live queue/steer delivery against Pi 1.4.0 while a real tool is running.
- Rendered contrast measurements in both light and dark modes and in an active
  plugin WebView.

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
