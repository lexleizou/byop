# P10c 验证记录

日期：2026-10-23

阶段：HUD footer、Pi 扩展状态、排队/插话移入输入区。

基线：`70b7ea0`（P10b）。

## 已验证

### Pi RPC 与 HUD 数据

- Pi 1.4.0 的 `get_session_stats` 已接入；Go 模型覆盖 session、消息数、tool 数、token 累计、cost 与可选 context usage。
- 已按 Pi footer 的 `formatTokens` 实现 1,000、10,000、1,000,000、10,000,000 的格式边界。
- HUD 的家目录缩短与 Pi `formatCwdForFooter` 一致；git 分支在后台查询，探到 detached HEAD 时显示短 hash，2 秒超时或失败时不显示分支。P10e 补充要求已改为只显示分支、不在 composer HUD 显示 cwd 路径，后者取代本阶段的家目录缩短展示。
- token 段只显示非零项；累计缓存命中率使用 `cacheRead / (input + cacheRead + cacheWrite)`；费用为累计三位小数；上下文段在窗口已知但百分比未知时显示 `?/<window>`，窗口也未知时隐藏该段，自动压缩开启时显示 `(auto)`。
- 上下文阈值按规格实现：大于 70% 进入 warning，大于 90% 进入 danger；恰好 70% 或 90% 不升级。扩展状态按 key 排序，单项最多 40 个字符。
- 连接成功、会话切换、新会话、模型与思考强度变化、`agent_settled`、`compaction_end` 和重连均触发 HUD 刷新。切换、新会话与重连推进 generation；旧 stats 同时按 generation、Pi epoch 和 client 拒绝。
- `extension_ui_request` 的 `setStatus` 已规范 `\r\n\t`、合并空格并支持按 key 清除；`notify` 进入现有信息通知；`select`、`confirm`、`input`、`editor` 返回 Pi 1.4.0 约定的 `{"type":"extension_ui_response","id":...,"cancelled":true}`，避免等待。
- 真实 Pi RPC 集成测试已读取并使用 Pi 1.4.0 返回的 session stats。
- 订阅状态无法由 Pi RPC 获取；本阶段保守地不显示 `(sub)`。HUD 对 API 计费会话仍显示 RPC 返回的累计 cost。

### 输入区与布局

- 运行中的输入区显示 `[排队 | 插话]` 分段控件；选择会持久化，下次运行继续使用。插话模式显示一行说明，排队模式保留工具提示。
- Send 与 Stop 已分开；Stop 使用独立按钮。运行时 Send 继续使用当前分段模式；普通 Enter 使用当前模式，`⌘/Ctrl+Enter` 本次使用相反模式。空闲时不显示分段控件。
- 原有的运行中发送设置卡已从设置页删除，对应旧语言键已移除；`RunningSendMode` 偏好仍保留。
- 运行态发送增加入口校验：空消息、未连接、Pi 会话操作中、思考等级提交中、压缩中或当前不处于运行态时直接拒绝并显示现有本地化通知。拒绝不会清空草稿，也不会启动 pending 请求；直接回归测试覆盖了这些情况。
- Composer 正文仍为 14px。宽窗口 HUD 留在 Footer；聊天主区小于 720px 时移到输入区下方独立行，时间线保持独立滚动。
- 布局回归覆盖 `1100x560`、`1180x760`、`1440x900`，同时检查长 transcript、follow-up 卡片、队列条、连接错误、任务面板、HUD、Send、Stop 与分段控件。P10a 的 nav `Grow(1).MinHeight(0)`、footer `Shrink(0)`、有界 main/chat 高度未改动。

### P10b 审核 P3

- `invalidateSessionNaming()` 不再清除正在进行的命名提交所有权。提交回调使用 token 与 owner 结束自身状态，旧回调不会清掉更新提交。
- 任务面板动画宽度小于 1px 时，在返回前显式设置 `Width(0).Shrink(0)`。

### Opus 复审修复

- Composer 的 HUD 换行判断改按聊天主区实际宽度计算：窗口宽度扣除左右外壳间距、导航栏和展开的任务面板，再与 720px 内容宽度比较。新增 `1100x560`、展开任务面板时主区小于 720px 的布局测试，并验证 `pi-hud-row` 完整位于视口内。
- 会话命名提交的 `sessionNamingCommitSeq` 与 `piSessionBusy` 检查移到 UI 线程入口；后台 worker 只执行 RPC 和 `postUI` 回调。自动命名与手动保存均先通过 UI 线程检查再启动 worker，完成回调继续校验 token、generation、epoch、client 与会话目标。
- cwd、stats 和 context window 全部未知时不再渲染孤立“?”；所有 HUD 段为空时整行不渲染。后台窗口已知但百分比未知时仍显示 `?/128k`。
- `get_session_stats` 临时请求失败时保留最后一次成功 stats；成功返回仍按 generation、epoch 和 client 拒绝旧结果。
- 新建或切换会话的 RPC 失败、取消、状态读取失败及 transcript 恢复失败路径都会重新请求 HUD，避免重置后一直停在空白状态。
- 侧栏展开态状态按钮改为与导航一致的起点、10px 内边距和 10px gap；8px 状态圆点置于 20×20 居中槽，底部状态与导航图标的视觉中心对齐。
- 自动命名提交进行中时，标题菜单禁用重命名入口和上下文命名入口，内联保存按钮禁用，`beginSessionRename` 与 `saveSessionRename` 也会拒绝；自动提交完成后由自身回调释放状态。

### 初版真实窗口证据

以下截图只包含 BYOP 窗口，无凭据或私人内容，保存在
`~/AgentScratch/20261022-byop-p10c/evidence/`：

- `real-session-light-1100x560.png`
- `real-session-dark-1100x560.png`
- `real-session-light-1440x900.png`
- `real-session-dark-1440x900.png`
- `running-demo-1100x560.png`

真实 Pi 会话截图使用临时 session wrapper，并读取已保存的真实 Pi 会话，因此能看到实际 token、成本与 context usage。明暗两种主题、两个尺寸均确认 HUD 与 composer 底边完整可见；过长 HUD 在末尾省略。运行态截图处于 `BYOP_DEMO_TOOLS=1` 的演示运行中，显示 Queue/Steer、独立 Stop 和 HUD 状态区域。

Opus 复审后桌面进入锁定状态，无法重新操作原生窗口，因此未重拍运行态复审截图。现有 `running-demo-1100x560.png` 属于复审前初版证据，左下方仍可见孤立“?”，不能用于证明本次“未知上下文段隐藏”或“侧栏状态圆点对齐”修复。

## 未验证

- macOS 桌面锁定后无法通过原生窗口重拍 Opus 复审后的运行态 Queue/Steer 截图。真实 Pi 数据来自已保存会话；HUD 空段逻辑由单元/布局测试覆盖，侧栏状态圆点对齐仍缺少复审后的实机视觉确认。
- Pi RPC 未暴露 `modelRuntime.isUsingSubscription` 或等价状态，因此实际订阅会话的 `(sub)` 文案未做运行时验证。
- Linux 与 Windows 的 HUD、字体截断、键盘快捷键和原生窗口行为未验证。
- 扩展 UI 的取消路径已按 Pi 1.4.0 类型与 RPC 实现测试；未加载会实际弹出 `select`、`confirm`、`input` 或 `editor` 的第三方扩展做端到端实机验证。

## 已知限制

- HUD 被放进现有单行 composer footer，继承 MyGo footer 的固定高度；未改变 P10a/P10b 的高度链。窄窗口把 HUD 放到下方 24px 行。
- MyGo 当前使用文本省略号处理 HUD 超宽；完整 HUD 可通过既有 tooltip 查看累计缓存命中说明。
- Pi 的累计缓存命中算法由 `get_session_stats` 的 token 汇总重建；本阶段按规格计算累计 `CH`，这与 Pi CLI 最近一次请求旁显示的 `CH` 含义不同。
- 本阶段不加载、不渲染扩展 widget、title、editor 文本或自定义 footer。
- 剩余 P3（只记录，不修）：输入框底部的“Pi 正在工作”和时间线中的“正在工作 · 时长”表达同一运行状态，仍存在语义重复。

## 检查

- `gofmt -l .`：无输出。
- `go vet ./...`：通过。
- `go build -o byop .`：通过。
- `go test ./... -count=1`：259 项通过，覆盖 3 个 package。
- `git diff --check`：通过。
- `mygo i18n check .`：通过，英文与简体中文共 607 条消息。

P10c 改动仍在工作树中，本阶段未提交、未推送。

## Opus 二次复审

- 结论：P1、3 个 P2 和前 3 个 P3 已修复，未发现新的 P0–P2；命名与 HUD 相关测试另跑 `go test -race` 通过。
- 两个新 P3 已由协调者在提交前处理：侧栏与任务面板宽度提取为 `navExpandedWidth`、`navCollapsedWidth`、`taskPanelWidth` 共享常量；窄窗口 HUD 换行时不再保留输入框底部 8px 前置间距。
- 剩余已知限制：面板展开/收起动画过程中，HUD 布局按目标宽度提前切换。
