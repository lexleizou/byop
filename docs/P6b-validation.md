# BYOP P6b Validation

验证日期：2026-10-21

## 范围

P6b 在 Chat composer 的 Accessory 区域加入两个原生 Onevra 选择器：模型
`Picker` 和 thinking level `EffortPicker`。本阶段通过 Pi RPC 提交模型与
reasoning level，修正 P6a 会话路径校验，并更新阶段标识和文档。本阶段不新增
功能插件，不改变 P5 插件能力，也不改动 Pi 凭据或会话文件。

## 实现

- 模型 chip 使用 `chat.Picker`；选项来自 `get_available_models`，按 provider
  分组并显示 model name 与 context window；模型 ID 保留
  `provider/id` 原值，并通过 lookup map 回查 provider 和原始 ID；
- 模型列表超过 5 项时显示 `Search models`，空结果为 `No models`；
- 模型 chip 标签按 `Name → piModel → "Pi default"` 回退，选项行按
  `Name → ID` 回退；同名模型在选项行显示模型 ID 以便区分；模型元数据尚未
  加载时只显示只读 chip，不打开空选择器；
- 模型与 thinking chip 在运行、压缩或 Pi session operation 期间改为只读，
  不发起变更；
- thinking chip 使用 `chat.EffortPicker`，只展示 Pi 当前模型报告的支持级别；
  顺序为 `off → minimal → low → medium → high → xhigh`，未知级别按 Pi
  返回顺序追加；
- 空级别和仅 `["off"]` 时隐藏 thinking chip；`Off` 只在 `off` 为第一级
  时启用；
- 用户点击或拖动 EffortPicker 时，界面立即显示 pending level；400 ms 无
  后续变化或面板关闭时只提交一次，连续变化会重置计时器，过期回调按 sequence
  丢弃；
- Thinking 提交使用独立的在途状态，不把两个 chip 切成只读，也不会关闭已打开
  的 EffortPicker；提交期间继续选择时会保留最新 pending，并在前一次调用返回
  后继续提交；若用户拖回旧 confirmed 值，pending 不会被错误清除，且在途值
  会在 pending 为空时继续作为 chip 的显示值；
- pending 与 Pi 已确认值相同时跳过 RPC；RPC 失败时清除 pending 并保留错误
  notice；运行、压缩、断线、重连、切换会话、发送消息和关闭窗口时取消 pending；
- 移除模型与 thinking level 成功变更后的 notice，错误 notice 保留；
- `loadPiSession` 在 `applyPiSessionState` 之后使用 canonicalized
  `s.piSessionFile` 校验会话，并在比较前规范化期望路径，不再用原始
  `state.SessionFile`；
- 标题栏状态改为 `P6 · MODELS`，Settings About 改为 `P6 models · 2026-10-21`；
- `README.md`、架构文档和本验证记录同步 P6b 边界。

## 验证结果

| 检查 | 结果 |
|---|---|
| `gofmt -l .` | 通过，无输出 |
| `go vet ./...` | 通过，无输出 |
| `go build -o byop .` | 通过，无输出；生成 arm64 Mach-O 可执行文件 |
| `go test ./... -count=1` | 通过，2 个 package 共 89 个测试 |
| `git diff --check` | 通过，无输出 |

最小纯 helper 测试覆盖模型选项 ID、含 `/` 的 ID 回查、context window 格式、
模型标签回退、thinking 级别顺序与标签、可见性，以及 pending commit 决策。

## 界面走查

| 步骤 | 结果 | 截图 |
|---|---|---|
| Light 主题下显示模型与 Thinking chip | 通过：chip 显示 `DeepSeek V4.1 Flash · OpenCode Go` 与紫色 `High` | `screenshots/p6b-chips.png` |
| 模型列表分组、选中态、context window、搜索 | 通过：按 provider 分组，行尾显示 1M/400K/200K；粘贴 `deepseek` 后列表过滤；当前模型显示勾选；同名模型第二行显示模型 ID | `screenshots/p6b-model-search.png` |
| 选择模型后 chip 立即反映新模型，无成功 notice | 通过：切到 `DeepSeek V4.1 Flash · DeepSeek` 后 chip 更新，无成功提示；随后已切回原模型 | `screenshots/p6b-model-switched.png` |
| Thinking 面板只显示当前模型支持的级别 | 通过：当前模型显示 Off / Low / High / Max 四档阶梯条 | `screenshots/p6b-thinking.png` |
| 点击 Thinking 级别后即时更新并提交 | 通过：点 Low 后面板与 chip 显示 Low，面板保持打开；再点 High 并关闭后 chip 恢复 High | `screenshots/p6b-thinking-low.png` |
| 不支持 reasoning 或仅返回 `off` 时隐藏 Thinking chip | 未走查（当前账户模型均支持 reasoning）；单元测试覆盖 | — |
| 连续拖动只提交最后值、提交中拖回旧值 | 未在真实窗口走查；单元测试覆盖 | — |
| 运行、压缩、会话操作时两个 chip 为只读 | 未走查；代码审查确认 | — |
| RPC 失败时恢复已确认值并显示错误 notice | 未走查（无法安全制造失败）；代码审查确认 | — |
| Dark 主题、窄窗口 | 未走查 | — |

走查使用协调者在 Light 主题、真实 Pi CLI 1.3.0 连接下的 BYOP 窗口；走查结束时模型
和 Thinking 已恢复为原值（`opencode-go/deepseek-flash`、High）。最后一次 drag-back
修复晚于走查构建，仅由单元测试和复审确认。

## 未验证与已知限制

- 未运行真实 Pi 集成测试；默认全量测试会跳过需要
  `BYOP_PI_INTEGRATION=1` 和已登录 Pi 的用例；
- 真实 provider 返回的 reasoning levels、模型名称与 context window 取决于 Pi
  版本和账户状态；BYOP 只展示 Pi 返回的元数据；
- 400 ms 提交使用 UI 线程 timer；窗口关闭、重连、进程退出和会话切换会取消
  未提交值；
- `provider + "/" + id` 作为 option ID 时，若不同 provider/ID 组合拼出同一
  字符串会产生重复键；当前依赖 Pi 返回的 provider/ID 组合唯一；
- EffortPicker 面板打开时点击 Send，结果取决于事件处理顺序：pending 可能被
  丢弃，发送也可能因 Thinking 提交在途而被拒绝；
- 模型搜索与分组由 Onevra `Picker` 提供；BYOP 不新增自定义 widget；
- 未验证 Linux 和 Windows 的视觉、键盘与窗口行为。
