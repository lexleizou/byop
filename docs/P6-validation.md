# BYOP P6a Validation

验证日期：2026-10-21

## 范围

P6a 完成 BYOP 品牌标识、Pi 持久化会话、只读会话发现、通过 RPC 新建和切换
会话、原生 transcript 恢复，以及模型与 thinking level 的状态和 RPC 接线。
模型与 thinking level 选择器界面不在本阶段；现有 composer 模型标签仍只读。
本阶段不新增功能插件，也不改变 P5 插件能力。

## 实现

- 新增 project-original 的 BYOP 圆形 π 标识，标题栏和 Settings About 使用
  自适应前景色；嵌入的 1024 px PNG 用于窗口和 Dock 图标；
- Pi 默认使用 `--mode rpc` 并持久化会话；`BYOP_PI_EPHEMERAL=1` 才追加
  `--no-session`，测试 helper 显式使用临时会话；
- Pi 的工作目录统一取自 `BYOP_WORKSPACE` 或当前目录，并解析为绝对路径；
  `PI_CODING_AGENT_SESSION_DIR` 设置时按 Pi 的平铺目录语义扫描，并依据
  session header 的 `cwd` 过滤；未设置时使用
  `($PI_CODING_AGENT_DIR 或 ~/.pi/agent)/sessions/--<cwd>--`；
- 会话扫描在 UI goroutine 外执行并去抖 250 ms，在连接完成、创建或切换会话后、
  agent end 和 agent settled 后重置刷新计时；先按 mtime 排序，再只解析最新
  50 条；
- 扫描只读取当前工作区目录的普通 `*.jsonl` 文件；跳过符号链接，超过
  64 MiB 的文件不读内容并显示 `Untitled session`，单行超过 1 MiB 时跳过该行，
  不记录会话内容；
- 标题优先使用最后一个非空 `session_info.name`，否则使用第一条 user 文本；
  单行化并截断到 80 runes；消息数只统计 user、assistant 和 toolResult；
- Chat 导航显示 SESSIONS 标题、新建按钮和最近 8 条；展开后最多 16 行可见并
  可滚动；当前会话使用选中态；收起导航时只保留新建按钮；
- `new_session`、`switch_session`、`get_messages`、`get_available_models`、
  `set_model`、`get_available_thinking_levels` 和 `set_thinking_level`
  均有 RPC wrapper；
- 切换成功后从 `get_messages` 恢复 user/assistant 文本和 toolCall，并以
  `toolCallId` 把 toolResult 合并回对应工具卡；显示结果沿用 1200 rune 上限；
- 运行或压缩中拒绝会话、model 和 thinking level 变更，并以原生 notice 提示；
- 会话异步操作使用 busy 标志与单调 session generation，拒绝重叠的发送、切换、
  新建、model 和 thinking 操作，并丢弃过期回调；切换或新建后通过
  `get_state` 的实际 session path/ID 校验，路径按符号链接解析结果比较；
- `BYOP_PI_EPHEMERAL=1` 不执行会话发现、隐藏 SESSIONS 列表并拒绝切换；
- transcript 恢复区分 error、aborted、无可见内容以及未完成的 tool call；
- Settings 的 Model 行显示 `Provider / Name · thinking <level>`；
- `README.md` 和架构文档已同步 P6a 边界。

## 验证结果

| 检查 | 结果 |
|---|---|
| `gofmt -l .` | 通过，无输出 |
| `go vet ./...` | 通过 |
| `go build -o byop .` | 通过，生成 arm64 Mach-O 可执行文件 |
| `go test ./... -count=1` | 通过，2 个 package 共 77 个测试 |
| `git diff --check` | 通过 |

定向测试覆盖 session 目录规则、会话标题与排序、超长行跳过、越界符号链接跳过、
transcript 的 toolCall/toolResult 配对和结果截断，以及模型响应中的凭据字段
不会进入 BYOP 状态。

## 界面走查

以下结果来自 2026-10-21 真实 macOS 窗口（Light 主题，1238×1008，外部 Pi CLI 1.3.0 已连接）。

| 步骤 | 结果 | 截图 |
|---|---|---|
| 标题栏使用新 BYOP π 标识，状态显示 `P6 · SESSIONS` | 通过（Light） | `screenshots/p6-logo-sessions.png` |
| SESSIONS 行的图标与文字和 Chat、Plugins 等导航行左对齐 | 通过（审核修复后复查） | `screenshots/p6-logo-sessions.png` |
| 点击会话切到 Chat，恢复 user 消息，会话行高亮，提示 `Switched Pi session.` | 通过 | `screenshots/p6-switch.png` |
| 点击 `+` 新建会话，Chat 回到空状态，提示 `New Pi session created.` | 通过；新会话在首条消息前不出现在列表中（Pi 尚未写入文件） | `screenshots/p6-new-session.png` |
| Settings About、Dock/窗口图标、Dark 主题 | 未走查 | — |
| 超过 8 条时 Show all、16 行滚动 | 未走查（当前工作区只有 1 个会话） | — |
| 运行中点击切换/新建被拒绝 | 未走查（由 busy/generation 逻辑与单元测试覆盖） | — |
| 收起导航 56 px、插件 view 页面不显示 session 区 | 未走查 | — |

## 未验证与已知限制

- P6a 不提供模型和 thinking level 选择器，应用方法只供 P6b 使用；
- 模型元数据来自 Pi RPC；未登录、provider 不可用或 Pi 版本不支持时，连接仍可
  建立，但 model 或 thinking metadata 可能为空；
- `PI_CODING_AGENT_SESSION_DIR` 可以覆盖 Pi session 根目录；若仅在 Pi settings
  的 `sessionDir` 中设置，启动 BYOP 时应显式导出同一环境变量；
- session 列表是当前工作区的只读索引，不处理跨工作区会话、搜索、重命名或删除；
- 会话树只通过 Pi 的 `get_messages` 取得当前 active branch；被压缩或已完成的原生
  tool 结果只显示 Pi 返回的内容，不读取 JSONL 重建 transcript；
- 未验证 Linux 和 Windows 的真实图标、目录命名或符号链接行为；相关路径规则仅由
  单元测试覆盖；
- Dark 主题、窄窗口、收起导航和 16 行滚动边界尚未走查；
- 未运行真实 Pi 集成测试；默认全量单元测试会跳过需要
  `BYOP_PI_INTEGRATION=1` 和已登录 Pi 的用例。
