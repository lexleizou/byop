# BYOP P2 Validation

Validation date: 2026-10-20
Scope: external Pi CLI, JSONL RPC connection, send, streaming, completion,
stop and reconnect isolation.

## Environment

| Item | Value |
|---|---|
| OS target | macOS arm64 |
| Go | 1.27.2 |
| MyGo | `v0.5.5`, commit `4d8673cd2658a2c6e86b26c44751e57bf8174f91` |
| Pi CLI tested | 1.3.0 |
| Pi mode | `--mode rpc --no-session` |

The CLI location and local user directories are intentionally omitted from
this public validation record.

## Protocol Behavior

| Check | Result |
|---|---|
| Start child process and perform `get_state` | Passed |
| Parse non-empty session ID | Passed |
| Send prompt and receive successful response | Passed |
| Receive streamed assistant text | Passed |
| Receive final authoritative assistant message | Passed |
| Wait for `agent_settled` completion | Passed |
| Stop with `clear_queue → abort` | Passed |
| Observe `agent_settled(aborted:true)` | Passed |
| Treat an empty `clear_queue` response as success | Passed |
| Isolate old-process events after reconnect | Implemented; app state checks process identity and epoch |
| Terminate child on window close | Implemented |

Commands:

```sh
gofmt -w *.go
go build -o byop .
go vet ./...
go test ./...

BYOP_PI_INTEGRATION=1 go test \
  -run 'TestPiRPCIntegration(MinimalLoop|Stop)$' \
  -v -count=1 -timeout=3m
```

At validation time, `go build`, `go vet`, the default test set and both real
Pi integration tests passed.

## GUI Coverage

- Chat connected, Chat running and Settings connected screenshots are stored
  under `docs/screenshots/`.
- Settings exposes connection, CLI version, model, ephemeral-session status
  without an identifier and run state.
- Sidebar and task panel use the same connection and run state.
- The default body font remains 14 px.
- Workspace navigation icons use a fixed 20 px optical slot with a centered
  17 px icon.

## Current Limits

- The transcript covers only the active ephemeral run; no history or session
  switching yet.
- Tool events appear as a compact activity line, not typed tool cards.
- Markdown, syntax highlighting, Mermaid and KaTeX are not rendered.
- Attachments and file drag/drop are not connected.
- Plugin installation, plugin execution and WebView hosting are not
  implemented.
- Model selection, thinking level and OAuth UI are not connected.
- Linux and Windows are not validated in P2.

These limits are explicit boundaries, not hidden fallbacks.
