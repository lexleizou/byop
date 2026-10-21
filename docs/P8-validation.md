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

## Verified

| 检查 | 结果 |
|---|---|
| P8 临时目录单元测试 | 通过（11 项）：UTF-16LE、映射与幂等导入、主题冲突、扫描后当前偏好冲突复检、损坏偏好保全、历史追加失败、WAL sqlite 私有副本读取与源文件不变、环境变量覆盖、分字段偏好写入、自动会话关联不落盘与路径脱敏 |
| `gofmt -l .` | 通过；无输出 |
| `go vet ./...` | 通过 |
| `go build -o byop .` | 通过 |
| `go test ./... -count=1` | 通过；120 项（2 个 package） |
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
- `ui_errors` 的路径脱敏可能误伤普通文字；
- 真实旧 localStorage 键集合可能超出当前规格，未知键会保留为 unknown 而不导入；
- P3：`BYOP_THEME` 与用户显式点击的模式相同时，显式主题点击不会写回偏好；
- P3：`sqlite3` 会读取 `~/.sqliterc`，且扫描没有超时；
- P3：最近会话查找按 canonical path 查 `lastSessions` 的原始键，可能多出一条预览；
- P3：会话映射中的非字符串值会被静默跳过。
- P3：含 `&`、`<`、`>` 的旧值经 `json.MarshalIndent` 转义后，重启再扫描会显示为
  conflict（无导入动作，Import 不出现、不重复写入）；后续可改为解码后比较。
