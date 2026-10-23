# P10p packaging and install validation

Date: 2026-10-23  
Base commit: `5a5442e`  
Version: `0.10.0`  
Platform: macOS 26.7.1 (25G241), Apple Silicon arm64

## Packaging

Command run from the repository root:

```sh
go run github.com/egoist/mygo/cmd/mygo build -skip-notarize
```

MyGo v0.5.5 completed both app and DMG packaging successfully; `-skip-dmg` was not needed. Its current output directory is `dist/darwin-arm64/`, rather than the `build/darwin-arm64/` path anticipated by the phase note.

| Artifact | Result |
|---|---|
| `dist/darwin-arm64/BYOP.app` | 13.9 MB reported by MyGo; 14 MB by `du -sh` |
| `dist/darwin-arm64/BYOP 0.10.0 arm64.dmg` | 4.4 MB |
| `Contents/Resources/AppIcon.icns` | Present; 97.9 KiB |
| `CFBundleName` | `BYOP` |
| `CFBundleShortVersionString` / `CFBundleVersion` | `0.10.0` / `0.10.0` |
| Bundle identifier | `io.github.lexleizou.byop` |
| Signature | Ad-hoc (`Signature=adhoc`, no TeamIdentifier) |
| Strict code-sign verification | Passed for the built and installed app |

The app icon source is `assets/byop-logo-1024.png`. `/build/` remains ignored in `.gitignore` as requested; MyGo does not currently write to that directory.

## Installation and launch

The app was copied with `ditto` to `/Applications/BYOP.app`. No older copy existed for the first install. During iterative replacement, prior copies were moved to `~/.Trash`; no `rm` was used. Strict verification of the installed copy passed.

The installed executable was launched with `open`. Process inspection confirmed:

```text
/Applications/BYOP.app/Contents/MacOS/BYOP
```

The app remained running after the final launch.

## Pi connection

The default shell PATH already contained `/opt/homebrew/bin`, but a Finder-style launch does not inherit that PATH. Re-launching with only `/usr/bin:/bin:/usr/sbin:/sbin` reproduced two distinct failures:

1. `pi` itself could not be found.
2. After finding `pi`, its `#!/usr/bin/env node` runtime could not find `node`.

The resolver now checks the inherited PATH, `/opt/homebrew/bin`, `/usr/local/bin`, `~/.local/bin`, `~/.npm-global/bin`, and the login-shell PATH. Pi child processes receive a deduplicated PATH in this order: the resolved Pi directory, inherited PATH entries, login-shell PATH entries, then common install directories. This covers the Pi RPC process and the one-shot follow-up and session-naming runners.

A final launch with only `/usr/bin:/bin:/usr/sbin:/sbin` connected successfully. The UI reported `外部 Pi CLI 已连接` and displayed Pi 1.4.0 with `magpie / DeepSeek V4.1 Flash`. `/opt/homebrew/bin/pi` was resolved in that launch.

Process inspection showed a live `pi` child of the installed BYOP process, confirming the RPC process rather than only the version probe had started.

Evidence: [`p10p-installed.png`](screenshots/p10p-installed.png), captured with `screencapture -x -o -l` from the final installed app window only. It is 2796 × 1944 pixels.

## Review fixes

- Pi lookup and its login-shell PATH probe now run in the connection goroutine rather than synchronously on the UI thread. Reconnect and shutdown cancel the active connection attempt, and every later callback checks the connection generation and context before publishing state or adopting the client.
- The login-shell PATH is cached only after a successful non-empty probe. The probe has its own 3-second timeout; cancellation of a connection attempt only stops that caller from waiting and cannot poison the cache. Failed probes leave the cache empty so a later connection can retry. The probe uses fixed `/bin/zsh -lc`, `TERM=dumb`, empty stdin, and `cmd.WaitDelay = 500ms`.
- Follow-up suggestions use the Pi executable that established the live connection (`s.piPath`, falling back to the RPC client path). They no longer resolve Pi or run a login shell on the UI thread.
- `piProcessEnv` prepends the resolved Pi directory, then preserves the inherited PATH, login-shell PATH, and common install directories, with stable order and deduplication. Tests assert both the order and duplicate removal.
- The resolver test covers the login-shell probe running once across repeated calls. Tests that swap package variables are explicitly marked as non-parallel.
- `/bin/zsh -lc` reads login-shell startup files but does not read `.zshrc`. Users whose Pi comes only from an nvm block in `.zshrc` should set `BYOP_PI_PATH` or install Pi in a common executable directory.
- Session naming and follow-up one-shot processes use the same Pi child environment, so their Node runtime can be found under a Finder-style launch.

## Checks

All final checks passed:

- `gofmt -l .` produced no output.
- `go vet ./...` passed.
- `go build -o byop .` passed.
- `go test ./... -count=1` passed: 277 tests in 3 packages.
- `git diff --check` produced no output.
- `go run github.com/egoist/mygo/cmd/mygo i18n check .` passed with 552 messages in `en` and `zh-Hans`.

## Known limitations

- The app is ad-hoc signed and not notarized. Gatekeeper may block it on another Mac.
- Only macOS Apple Silicon was packaged; Windows and Linux builds were not produced or tested.
- MyGo v0.5.5 emits `dist/darwin-arm64/`; the README and this record use the actual path.
- Node plugin PATH reuse is deferred. The Pi RPC, follow-up, and naming processes receive the augmented PATH; plugin-host Node processes keep their existing PATH behavior.

## Remaining P3 after Opus re-review

- If the login shell keeps returning an empty PATH (for example a broken `.zprofile`), the probe is not cached and may add up to 3 s before each Pi child process starts.
- Node plugin runtime does not yet reuse the Pi child-process PATH construction.
