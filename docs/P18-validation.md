# P18 validation

Date: 2026-10-29. Base: `9b9188a`. App: `0.18.2`.

## P18.2 implemented

- Only the first assistant content after the latest user prompt renders the HUD once: model logo/name, available turn output tokens, context usage and current TPS, followed by thinking and tool content. Historical prompt/assistant turns do not display the current model state, because BYOP does not persist per-reply model/context/TPS snapshots. Later assistant messages in the latest logical turn do not repeat it.
- While the latest turn is running, thinking/text deltas accumulate a separate estimated output-token count for the HUD. Once Pi returns exact usage for that logical turn, the exact value takes precedence and is not added again on redraw.
- BYOP Light and Dark render the turn HUD as a frameless conversation status row with tighter spacing. J Agent may use its existing glass surface, but still uses active theme colors. Unknown model brands fall back to the Pi mark.
- Composer keeps its original 20 px secondary `pi-hud-row` for context capacity, input/output tokens, subscription/cost, cache hit and tokens, branch and extension status. The row restores its tooltip for cumulative cache percentage, cache tokens and average speed.
- The lightweight-table page no longer captures `window.byop` once at startup. Locale, bootstrap, query and polling calls use the dynamic preload bridge, with `window.mygo.call("BYOPWebView.Call", "plugin.postMessage", {message})` as the compatibility path.
- During plugin restart, `openPluginWebView`, `ensurePluginWebView`, `recreatePluginWebView` and view refresh do not create or load a WebView. Entering the plugin from navigation shows a dedicated lightweight localized restart state rather than the yellow unavailable warning.
- Restart completion reads the view the user is on at completion time, not the view captured at restart start, then reopens that current view. Restart failure destroys any remaining old WebView state.
- Page-level bridge failures show readable text; bootstrap no longer falls through with unloaded locale keys such as `emptyTitle` or `error.generic`. Localized errors remain localized without appending the English bridge message, while untranslated codes fall back to the actual bridge message.
- Plugin manifest version, plugin settings, SQLite schema and real records were not changed.

## P18.2 automated checks

- P18.2 tests cover latest-turn-only HUD placement, zero HUDs in historical turns, live/settled ordering before thinking/tools, prompt-plus-tool-only grouping in BYOP and J Agent, turn output tokens, footer tooltip, narrow widths, readable restart state, restart WebView guards, current-view reopen, stale-WebView cleanup, and real JavaScript bridge/error behavior.
- `go test ./... -count=1 -timeout 3m`: 512 passed, 7 skipped, no failures.
- `npm test` in `plugins/lightweight-table`: 40 passed, no failures.
- `node --check plugins/lightweight-table/web/app.js`, the lightweight-table UI contract test and locale JSON parsing passed.
- `gofmt -l .`, `go vet ./...`, `go build -o byop .` and `git diff --check` passed with no output.

## P18.2 native QA

- Built and installed BYOP `0.18.2`; `CFBundleShortVersionString` and strict code-sign verification passed.
- A real Qwen3.8 Flash Next turn verified the live HUD order in the assistant body: model logo/name, estimated `↓` output tokens, context, TPS capsule, then the thinking line and answer. Light and Dark BYOP themes rendered a frameless row; J Agent rendered its theme glass.
- The latest-turn-only rule held: completed earlier turns did not show the current model HUD, while the Composer footer continued to show context capacity, token totals, cache, and extension status.
- `1100×560`, `1180×760`, and `1440×900` were inspected. Model, context, output tokens, and TPS remained within the HUD; the model name shrinks first on narrow widths.
- The isolated Fuyao WebView loaded Chinese labels and all 49 copied v4 debt records. Clicking **Restart plugin** kept BYOP running and returned to the same Fuyao view with all 49 records; no `emptyTitle` or `error.generic` appeared. The dedicated restart-state rendering and restart-time WebView guards are covered by UI tests. The formal Fuyao database and plugin were not modified.
- Screenshots: `docs/screenshots/p18-2-hud-dark.png`, `p18-2-hud-light.png`, `p18-2-hud-jagent.png`, `p18-2-hud-jagent-1100x560.png`, `p18-2-hud-jagent-1440x900.png`.

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
- A compact turn HUD ellipsizes the model name first while reserving room for context and TPS. Lower-priority Composer metadata may also ellipsize within the existing 20 px row.
- No claim of full accessibility conformance or a fixed model list: reasoning levels still come from Pi.
- The latest response uses the current global model/context state. If the user switches models after a completed response, that response's model label changes until a new prompt creates the next turn; persisting per-response snapshots would require a separate data change.
