# P18 validation

Date: 2026-10-29. Base: `e8f5cec`. App: `0.18.0`.

## Implemented

- Thinking effort remains capability-driven from `GetAvailableThinkingLevels`; no model-specific level list was added.
- The composer now shows separate model and thinking chips. The thinking chip opens a compact, theme-derived slider popover with a header, current level, segmented progress track, stop labels, hover and focus states.
- Existing immediate pending state and debounced Pi commit/readback remain in place.
- The HUD orders current model, context usage, and current TPS first. Context includes percentage, used tokens, window, and the auto-compaction suffix when available.
- TPS uses a rolling live estimate during streaming and replaces it with Pi's exact final output sample. Cumulative averages remain tooltip-only.
- `thinking_delta` and structured thinking blocks now use an independent reasoning state. The transcript renders one grey lightbulb line with live updates and a collapsed elapsed summary; assistant text, session naming, and persisted chat history remain unchanged.

## Automated checks

- `go test . -run 'TestP18|TestP10cHUD|TestP11(HUD|AssistantMessageTiming)' -count=1`: 13 passed.
- `go test ./... -count=1 -timeout 3m`: 487 passed in 3 packages.
- `go vet ./...`, `gofmt -l .`, JSON validation, and `git diff --check`: passed with no findings.
- Locale catalogs were regenerated from the English and Simplified Chinese source JSON.
- Tests used isolated `BYOP_CONFIG_DIR` and `BYOP_PLUGINS_DIR` paths and did not read the real `~/.byop` configuration.

## Native QA

- Isolated BYOP Light and Dark windows rendered the model/context/TPS HUD, separate thinking chip, and themed effort popover.
- J Agent misty wallpaper keeps the thinking line to one compact hugging glass pill with theme-aware text; the effort slider uses the J Agent focus semantic tint because the preset's near-white accent has insufficient separation from the neutral track.
- A real isolated DeepSeek V4.1 Flash short turn streamed thinking into the one-line indicator while the HUD showed model, `3.0% · 29.9K / 1M`, and `67.0 t/s`. After completion it showed the final answer separately, context `3.1% · 30.6K / 1M`, and the exact calibrated `209.2 t/s`.
- Thinking status is now based on its own start/end timestamps rather than whole-turn duration. CJK runs count approximately as one token each for live estimation; exact final usage still replaces the estimate.
- Screenshots: `docs/screenshots/p18-light.png`, `p18-dark.png`, `p18-jagent.png`, `p18-jagent-effort.png`, `p18-live-thinking.png`.

## Remaining bounds

- Live TPS is estimated from streamed deltas; exact Pi output usage calibrates it at each completed assistant message.
- A compact HUD may ellipsize the model/provider or lower-priority session/extension metadata in very narrow windows.
- No claim of full accessibility conformance or a fixed model list: reasoning levels still come from Pi.
