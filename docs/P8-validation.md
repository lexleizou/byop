# BYOP P8 Validation

验证日期：2026-10-21

## Implemented

- 新增 `<config>/preferences.json`，保存主题、导航、任务栏、工作区、最近 Pi
  会话关联、草稿和旧值保留区；
- 偏好文件不存在时使用默认值；JSON 损坏时显示 warning 并继续使用默认值。
  用户下一次写入前，损坏文件改名为 `preferences.corrupt-<UTC>.json`，不会被
  静默覆盖；损坏 warning 存在时，自动会话关联只更新内存，不写文件；
- 偏好目录权限为 `0700`，文件权限为 `0600`，写入使用临时文件和 rename；
- 启动优先级为 `BYOP_THEME` / `BYOP_TASKS` / `BYOP_NAV` /
  `BYOP_WORKSPACE` > `preferences.json` > 默认值；环境变量覆盖期间，用户显式
  切换主题、导航或任务栏仍会分别写回自己的偏好字段，不会把其他运行时状态
  一并持久化；
- `piWorkspaceCwd` 在未设置 `BYOP_WORKSPACE` 时使用存在的已保存工作区；
  不存在时回退当前目录，并在 Settings 显示 `saved workspace missing`；已连接的
  Pi 使用连接时的实际工作目录记录会话列表和最近会话关联，导入的新工作区只在
  重连后生效；
- 迁移器只读扫描旧 app-dir、localStorage 与 Pi agent scheduled-task 信息；
  sqlite 先复制到临时目录（包括 `-wal` / `-shm`），再以普通读写方式打开私有
  一次性副本，使 WAL 内容在缺少 `-shm` 时也能由 SQLite 正确合并；源文件路径
  只用于读取复制，不交给 `sqlite3`；
  不新增 Go sqlite 依赖；
- localStorage 的 UTF-16LE 值（含可选 BOM）转为 JSON；主题、工作区、最近会话
  关联、草稿和已知兼容键按规格映射；未知键只显示为 unknown；
- Pi 会话按 canonical workspace 和 session header ID 关联，只保存关联，不复制
  会话正文；
- Settings 新增 Migration 卡片，扫描在后台执行，完成后通过 UI 回调更新；导入
  由用户触发，在 UI 线程同步写入少量本地文件；扫描时禁用重复操作，预览显示
  来源、版本、计数、目标与状态，扫描只读取文件，回 UI 线程后才基于当前内存
  偏好计算导入结果；
- 显式导入前备份现有偏好，原子写 `preferences.json`，再向
  `migration/imports.jsonl` 追加不含路径、会话 ID 或草稿正文的计数记录；偏好
  保存成功但历史追加失败时，仍保留已保存偏好并显示提示；
- 重复导入保持幂等；值未改变时不刷新 `ImportedAt`；
- UI 错误提示中的本地路径缩为文件名或固定文案；
- 标题栏与 About 更新为 P8 标记；插件 host 版本仍为 `0.7.0`。

## Review fixes (Opus 5.5 re-review)

- 旧 Pi Agent 写入的非 JSON 原始字符串按 JSON 字符串解码，主题值 `dark` 可直接
  迁移；只有坏 hex 或奇数 UTF-16 字节长度标为 unknown，不再静默丢行；
- 旧值与当前偏好冲突时，Import 把旧值写到 `<key>:conflict`，不覆盖
  `Legacy[key]` 或当前偏好；预览明确显示可执行的 conflict copy 动作，重复导入
  收敛为 same；
- 旧值与当前偏好的 JSON 语义比较会正确识别 `json.MarshalIndent` 对 `&`、`<`、
  `>` 的 HTML 转义，持久化后重新扫描保持 same；
- UI 错误只脱敏位于文本开头或分隔符后的绝对路径和 `file://` 路径，保留
  `anthropic/claude-sonnet`、HTTP(S) URL 和 `2/3` 等普通斜杠文本，并处理
  `=`、`[`、`,` 分隔符及 `file:///C:/...` 形式；
- 最近会话查找会规范化 `lastSessions` 键，符号链接工作区可命中同一会话，空
  工作区不会回退到当前目录。

## Verified

| 检查 | 结果 |
|---|---|
| P8 临时目录单元测试 | 通过（17 项 `TestP8*`）：原始非 JSON 值、冲突副本与 JSON 语义往返收敛、路径脱敏边界、会话键规范化、空工作区保护、UTF-16LE、映射与幂等导入、主题冲突、扫描后当前偏好冲突复检、损坏偏好保全、历史追加失败、WAL sqlite 私有副本读取与源文件不变、环境变量覆盖、分字段偏好写入、自动会话关联不落盘与路径脱敏 |
| `gofmt -l .` | 通过；无输出 |
| `go vet ./...` | 通过 |
| `go build -o byop .` | 通过 |
| `go test ./... -count=1` | 通过；135 项（2 个 package） |
| `git diff --check` | 通过 |
| 真实窗口 Migration 卡片走查 | 通过：0.7.4 真实数据副本 Scan 显示 new 1 · kept 6 · linked 1 · ignored 1 · info 4；Import 后显示 Already up to date，再次 Scan 为 same 8，`imports.jsonl` 1 行，源 sqlite size/mtime 不变（`screenshots/p8-migration-scan.png`、`p8-migration-imported.png`） |
| 真实窗口主题导入后即时应用 | 走查发现 P2：`BYOP_THEME=light` 且旧数据无主题时，Import 把窗口切回 System；已用 `ThemeChanged` 修复，复测保持 Light；旧数据含主题时的立即应用由单元测试覆盖 |
| 真实窗口重启后收敛 | 通过：退出并重启后（从 `preferences.json` 重载）再次 Scan 仍为 same 8、conflict 0，Import 不显示；审核发现的 RawMessage 重排 P2 已用 `legacyRawEqual` 修复 |
| 真实窗口 Workspace 保存提示与重连边界 | 通过：Import 后显示 “Workspace saved; reconnect Pi to use it”；真实 Pi 重连未验证 |

## Unverified

- 只走查了 Light 主题和默认窗口宽度；Dark 主题、窄窗口和扫描中的按钮禁用态未截图验证；
- 已核实 0.7.4 真实 localStorage 的 6 个键名和值类型（含草稿键），未读取值内容；
- 未验证真实 Pi CLI 与迁移后的工作区、会话关联之间的完整交互；
- 未验证 Linux 和 Windows 的默认旧数据路径、权限与 UI。

## Known Limits

- P8 不导入 Pi 会话正文、凭据、模型配置或定时任务；scheduled task 只计数和
  提示；
- 旧字号、sidebar scale 与宽度仅保存在 `legacy`，不改变 BYOP 正文 14 px、布局
  或默认收起的任务栏；
- 迁移没有后台自动执行，也没有合并或删除旧数据的入口；
- 扫描前应先退出旧 App，否则 sqlite 主库和 `-wal` 可能来自不同时间点；
- WAL fixture 已执行 checkpoint；只存在于 `-wal` 的数据由协调者使用真实副本
  实测覆盖；
- 若扫描后旧数据发生变化，预览不会自动刷新；用户需要再次 Scan；
- 迁移写入失败时旧文件不被删除；已有偏好会先尝试备份，具体错误显示为 notice；
- 设置 `BYOP_THEME` 时，导入的主题仍会在本次运行中立即生效；这是预期行为；
- 真实旧 localStorage 键集合可能超出当前规格，未知键会保留为 unknown 而不导入；
- P3：`BYOP_THEME` 与用户显式点击的模式相同时，显式主题点击不会写回偏好；
- P3：`sqlite3` 会读取 `~/.sqliterc`，且扫描没有超时；
- P3：会话映射中的非字符串值会被静默跳过。
- P3：drafts source alias overlap（`pi-web:drafts` 与另一个 drafts key）可能显示
  false conflict；
- P3：每次导入都会新增一个 `migration/backup-*.json`，目前没有保留数量上限；
- 含空格的路径可能在错误文本中留下中间目录名（例如
  `My Docs/secret.json`）；`POST /v1/chat/completions` 也可能被缩短；
- 第二次不同的冲突会覆盖之前的 `:conflict` 副本，旧值只能从
  `migration/backup-*.json` 恢复。
- P3：`legacyRawEqual` 只比较第一个 JSON 值；输入已经过 `json.Valid` 与正常反序列化，实际不会出现多值 JSON。
