# BYOP P7 Validation

验证日期：2026-10-21

## Implemented

- `plugin.json` 的 `contributes.automations` 支持 `manual`、`once`、
  `scheduledLoop` 和 `sessionLoop`；校验 command、精确 activation event、
  rule、missed policy、最大运行时长和贡献 ID；
- 宿主版本更新为 `0.7.0`；automation ID 纳入同插件重复检查和跨插件贡献
  冲突检查；
- 每次 Run now 在 `<pluginConfigDir()>/automations/` 下持久化 `running` 与
  最终记录，记录触发方式固定为 `manual`；
- 运行记录按自动化保留最近 200 条，较早记录追加到 archive；启动时恢复
  中断记录；
- 连续 5 次失败或超时后自动暂停；成功清除连续失败数；用户可暂停和恢复；
- `sessionLoop` 在应用重启后暂停并显示 `App restarted`；
- Automations 页面显示注册项、插件归属、kind / rule、状态、失败计数、
  最近运行、暂停原因和运行控制；
- 插件重扫保留按 `pluginID/automationID` 键控的持久化状态，只显示当前已注册
  插件的 automation；
- `examples/hello-node` 增加 `hello.fail`、一个 scheduledLoop 和一个 manual
  automation，仅作为 P7 契约 fixture。

## Verified

| 检查 | 结果 |
|---|---|
| 直接相关单元测试 | 通过（19 项）：fixture manifest、规则与 activation 边界、5 次失败自动暂停、成功重置、resume 清零、运行中 Pause、200 条保留与 archive、坏 JSONL 行保留有效记录、损坏 state 隔离、中断恢复、路径隔离、主动停止不计失败、sessionLoop 重启暂停与重扫不重复恢复 |
| `gofmt -l .` | 通过；无输出 |
| `go vet ./...` | 通过 |
| `go build -o byop .` | 通过 |
| `go test ./... -count=1` | 通过；109 项 |
| `git diff --check` | 通过 |
| `node --check examples/hello-node/plugin.mjs` | 通过 |
| Light 主题真实窗口走查 | 通过：2 个注册项、READY 状态和插件归属正确，见 `screenshots/p7-automations.png` |
| 手动失败运行与自动暂停提示 | 通过：Failure probe 第 5 次失败后显示 AUTO-PAUSED、FAILURES 5/5、暂停提示，Run now 禁用并出现 Resume，见 `screenshots/p7-auto-paused.png` |
| 手动成功运行与最近运行显示 | 通过：Greeting loop 显示 SUCCEEDED 和插件返回值 |
| Resume | 通过：Failure probe 恢复为 READY、FAILURES 0/5，最近运行记录保留，见 `screenshots/p7-resume.png` |
| 通知横幅出现时 Chat 输入框可见 | 通过：横幅与输入框同时完整显示，见 `screenshots/chat-notice-composer.png` |
| 通知横幅出现时 WebView 页面布局 | 通过，见 `screenshots/webview-notice-layout.png` |
| Dark 主题真实窗口走查 | 未执行 |
| 窄窗口卡片与运行行换行 | 未执行 |

## Unverified

- 未等待真实 interval、cron、`at` 或 `afterSettled` 触发；P7 不提供定时
  scheduler，只校验和展示这些规则；
- 未在真实窗口确认用户手动 Pause 的视觉状态及运行中按钮禁用；
- 未在真实窗口确认损坏 `state.json` 或 run JSONL 的 warning 展示；
- 未在 Dark 主题和窄窗口完成页面布局走查；
- 截图中标题栏仍显示 `P6 · MODELS`，截图后已改为 `P7 · AUTOMATIONS`；
- 未验证 Linux 和 Windows 的窗口、文件权限与视觉效果。

## Known Limits

- 只有手动 Run now；没有 timed scheduler、ticker、catch-up、定时并发调度或
  运行耗时实时刷新；
- 用户 Pause 不取消正在执行的 run，该 run 完成后仍写入最终记录；
- 最近运行最多显示 5 条，active JSONL 最多保留 200 条，archive 只追加不自动
  清理；
- 页面按插件名称和 automation ID 排序，没有额外筛选或分组控件；
- 运行结果和错误摘要按单行截断；完整插件输出不另行归档。
- `sessionLoop` 在安装后的首次启动也会把已启用项显示为 `App restarted`，
  因为当前恢复模型把首次会话 Loop 启动与后续应用重启视为同一状态；
- 命令超时只结束宿主侧等待并写入 `timeLimit`，不会终止插件内部已经开始的
  工作；超时后插件进程可能仍在执行该命令。
