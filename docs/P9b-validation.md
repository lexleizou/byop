# P9b Validation

Date: 2026-10-22

## Verified

- `locales/en.json` and `locales/zh-Hans.json` have the same non-metadata key
  set (601 messages each), every message parses as an ICU message, and every
  message has a matching `@key` description.
- `go run github.com/egoist/mygo/cmd/mygo@v0.5.5 generate .` completes and
  generates `locales/catalog_gen.go`.
- The preference normalizer accepts `system`, `en` and `zh-Hans`, and maps
  unknown values to `system`.
- Plugin manifest `%key%` values resolve from the current language, then
  `en.json`, then the original string.
- Plugin localization paths escaping the plugin root are rejected. Locale
  files over 256 KiB are ignored.
- The Settings language card derives its options from
  `locales.Catalog.Languages()`.
- The `hello-webview` contract fixture exercises manifest localization,
  `host.getLocale`, `window.byop.locale`, `window.byop.onLocale` and the
  `byop.locale` event.
- `gofmt -l .`, `go vet ./...`, `go build -o byop .`,
  `go test ./... -count=1`, the targeted race tests, and `git diff --check`
  complete successfully for the final P9b/P9c worktree.
- Live window walkthrough (macOS, light theme, 1180×760, external Pi CLI
  1.4.0) on the pre-review-fix build: Chat and Settings render in Simplified
  Chinese (`docs/screenshots/p9b-chat-zh.png`,
  `docs/screenshots/p9b-settings-zh.png`); switching Settings → Language →
  English redraws Settings, the sidebar and Chat without a restart
  (`docs/screenshots/p9b-settings-en.png`, `docs/screenshots/p9b-chat-en.png`).
  Settings opens without the former `languages` icon panic.
- Claude Opus 5.5 review: three rounds; final verdict PASS with no remaining
  P0–P2 findings.

## Unverified

- English and Simplified Chinese screenshots at every supported window size.
- A repeat window walkthrough on the final post-review build (the screen was
  locked after the final fixes), and the Plugins page / existing WebView redraw
  after a language switch.
- Dark theme in both languages.
- Live switching to a third language pack.

## Known Limits

- Pi output, Pi errors and raw provider errors remain verbatim.
- Model and provider IDs and names, file paths, protocol identifiers, stable
  persisted enum and error-code IDs, and environment-variable names remain
  unchanged.
- Plugin text without a matching localization value or valid locale file
  remains in its original language.
- The Pi follow-up suggestion generation prompt is unchanged by P9b.
- `MYGO_LOCALE` is documented for process-level testing; normal preference
  persistence remains in BYOP `preferences.json`.
- English-sentence matching in `i18n_helpers.go` only recognizes strings that
  have explicitly catalogued fingerprint patterns. Novel English prose from Pi
  or another external source remains a diagnostic fallback and may stay in
  English.
- A notice banner that is already visible stores its translated text, so it
  keeps the previous language until it is replaced or dismissed after a locale
  switch. New UI and subsequent notices use the selected language immediately.
- A few diagnostics and demo fixture strings intentionally remain in English
  because they identify protocol data, test data, or external output rather
  than BYOP interface copy.
