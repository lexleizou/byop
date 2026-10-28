# P17 validation

Date: 2026-10-28. Base: `4732207`. App: `0.17.0`. Fuyao plugin: `0.3.0`.

## Implemented

- Plugin details (opened from a card) and plugin WebView headers expose
  **Restart plugin**. The zh-Hans label is **重启插件**.
- Restart stops only the selected plugin worker, rescan-replaces only that
  source entry, destroys and recreates only that plugin's WebViews, preserves
  plugin data, and reopens the same contribution when it remains available.
  A host-side restart lock blocks the old WebView from starting a stale worker
  during the stop/rescan window.
- Stable Pi extension and proxy files are tracked separately from ordinary
  backend files. A stable-file change shows a Pi refresh-required state and a
  **Refresh Pi** action that uses the idle-safe channel refresh to restore the
  current session. `Reload` remains a page-only WebView action.
- A successful restart atomically writes
  `<plugin data>/runtime-generation.json`.
- `pi-extension.mjs` registers a stable `mcp-proxy.mjs` stdio server. The proxy
  owns a replaceable MCP backend child, waits for in-flight requests, reloads
  on generation changes, and emits `notifications/tools/list_changed`.
  Notifications continue during a drain, cancelled requests are removed, and a
  20-second deadline exceeds Pi's 15-second request timeout. A reload failure
  stops the candidate backend and exits the proxy nonzero so Pi can reconnect.
- The proxy receives child-process permission. Its backend retains package/data
  read and data write scope without network or child-process permission.

## Verified

- Coordinator native QA passed the primary flow before these final review
  fixes: Pi PID `88488` and unrelated `hello-node` PID `88623` remained
  unchanged; proxy PID `88513` remained unchanged; backend PID changed from
  `88515` to `90006`; the next Pi turn saw the new tool description.
- `gofmt -l .`: no output.
- `go vet ./...`: passed.
- `go test ./... -count=1`: 480 tests passed in 3 packages using an isolated
  config/plugin directory.
- `npm run check` in `plugins/lightweight-table`: 33 tests passed, including
  generation reload, proxy PID continuity, backend PID replacement,
  `tools/list_changed`, and in-flight completion before switch.
- `git diff --check`: passed.

All test databases, copied backends, and process files were synthetic and lived
under `~/AgentScratch/20261028-byop-plugin-hot-reload/`.

The focused proxy tests also cover cancelled requests during a blocked switch,
the drain deadline, failed candidate initialization with nonzero exit, and
backend cleanup. Focused Go tests cover stale-worker blocking, protection
against an older stop overwriting a newer process, and the session-restoring
plugin Pi refresh path.

## Remaining P3 limits

Per the user's explicit boundary, this implementation did not build BYOP,
create or install `0.17.0`, publish plugin files into the real `~/.byop` plugin
directory, read or write the real Fuyao database, or inspect the real database
checksum. Native QA was not rerun after the four P2 fixes; their verification is
the focused automated coverage above.

- `src/source-tracker.mjs` is imported by the Pi extension but is not included
  in the stable runtime signature.
- Reload initializes a new backend with a fixed protocol request and does not
  replay client `initialize` parameters or send `notifications/initialized`.
- `BYOP_LIGHTWEIGHT_TABLE_DB` overrides still derive the generation marker from
  the database directory rather than the host plugin-data directory.
- An invalid replacement manifest can still set the Pi refresh-required flag.
- Node prints the documented `PERM0002 SecurityWarning` because the stable
  proxy needs child-process permission.
- The untracked `docs/Lightweight Table Plugin ...-v0.1..v0.5-20261028.md`
  design files are unrelated to P17 and should be excluded from its commit.


## 最终原生 QA

修复四个 P2 后，重新使用隔离配置、合成表格和真实 Pi 连接走查：

- Pi PID 保持 2029，不重启 BYOP，也不重建当前会话。
- 无关插件 Hello Node 的 PID 保持 2116。
- 扶摇插件 worker 从 1957 切换到 2301。
- 稳定 MCP 代理 PID 保持 2054；后端从 2068 切换到 2304。
- 修改隔离副本的 `table_describe` 描述为 `HOT_RELOAD_QA_V3`，点击重启插件后，同一 Pi 会话的下一轮读到新描述，并成功调用工具。
- 重启通知显示“扶摇 已重启，仅重启了此插件。”；截图：`docs/screenshots/p17-plugin-restart.png`。

Opus 复审结论：PASS / APPROVED，剩余 P0–P2 为 0。


## 安装

- BYOP 0.17.0 已安装到 `/Applications/BYOP.app`，原 0.16.2 已移入废纸篓作为可恢复备份。
- 扶摇插件 0.3.0 已发布到 `~/.byop/plugins/local.lightweight-table`，包含稳定 `mcp-proxy.mjs`。
- 正式数据库只读核对：16 条记录、operations=0，安装前后摘要一致。
- 本机已有 Pi 会话启动于新版本。由于当前 Pi 正在处理用户任务，未对正式窗口执行额外点击；热重启路径以隔离窗口实测为准。
