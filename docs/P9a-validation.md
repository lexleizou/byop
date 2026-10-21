# BYOP P9a Validation

验证日期：2026-10-22

## Implemented

- Pi 轮次正常结束后，以当前工作区、Pi binary 和当前 session model 启动独立的
  one-shot `--print` 进程；使用 `--no-session`、`--no-tools`、
  `--no-extensions`、`--no-skills`、`--no-context-files`、
  `--no-prompt-templates`、`--no-mcp` 和 `--thinking off`，不写入主 Pi
  JSONL RPC 会话；
- 用户 prompt 保留头部、助手回复保留尾部，各约 3000 runes，通过 stdin
  传递；生成超时 20 秒，结果先取首个非空行并清理引号、项目符号、标签和
  多余空白，最终限制为 120 runes；
- aborted、stopped、error、无真实助手文本和断开状态的轮次不生成建议；
  `BYOP_PI_CONNECT=0` 时不启动生成进程；
- 建议默认以 composer placeholder 的 ghost text 显示，footer 显示
  `Tab to use · Esc to dismiss`；Settings 可立即切换为输入框上方的 in-flow
  卡片，卡片提供 Use 与 dismiss；
- 采纳不消费建议；清空输入框后建议重新显示。Esc/dismiss、发送、新一轮、
  会话切换、新建会话、重连、进程退出和窗口关闭会取消生成或移除建议；
- 生成 id、session ID 与 session file 不匹配的结果会被丢弃；测试通过注入
  runner 调用，不需要启动 Pi 进程；
- `byopPreferences` 新增 `followUpStyle`，只接受 `ghost` 与 `card`；默认值、
  空值和未知值都规范化为 `ghost`。

## Verified

| 检查 | 结果 |
|---|---|
| P9a 直接相关单元测试 | 通过（19 项）：输出清洗、头部/尾部截断、stdin 指令分隔与上下文、Pi 参数与 model 省略、采纳后末尾光标和 Card 焦点、清空重显、Esc、Enter/Shift+Enter/IME 兼容、发送清空、会话操作清空、生成取消、过期 generation/session 丢弃、繁忙期间结果延迟显示、偏好规范化、注入 runner 调用，以及 aborted/无助手文本跳过 |
| `gofmt -l .` | 通过（无输出） |
| `go vet ./...` | 通过 |
| `go build -o byop .` | 通过；构建产物随后移至 `~/.Trash` |
| `go test ./... -count=1` | 通过（156 项，2 packages） |
| `git diff --check` | 通过（无输出；文档更新后复跑） |

## Window walkthrough

2026-10-22，macOS，Light 主题，1391×1130 窗口，ephemeral Pi 会话，
真实 Pi CLI 1.3.0（DeepSeek V4.1 Flash）生成建议。

| 场景 | 结果 | 截图 |
|---|---|---|
| Grey text：轮次结束后空输入框显示灰字建议，footer 显示 `Tab to use · Esc to dismiss` | 通过 | `screenshots/p9a-ghost.png` |
| Tab 采纳：建议填入草稿，Send 激活，继续输入接在末尾 | 通过 | — |
| 清空草稿后建议重新显示 | 通过 | — |
| Esc 关闭后恢复默认 placeholder | 通过 | — |
| Settings 切换 Grey text / Card，立即生效 | 通过 | `screenshots/p9a-settings.png` |
| Card：建议卡位于输入框上方，不遮挡对话 | 通过 | `screenshots/p9a-card.png` |
| Card Use：填入草稿，焦点回到输入框，继续输入接在末尾 | 通过（首轮走查发现焦点留在按钮上，已修复后复验） | — |
| Card dismiss：关闭后恢复默认 placeholder | 通过 | — |

## Unverified

- Dark 主题、窄窗口和长对话滚动避让未走查；
- 只用少量短问答验证了真实生成，建议质量、语言一致性和接近 20 秒超时的
  情况未系统验证；
- 未验证 macOS 输入法与不同键盘布局下的 Tab、Esc 和 Enter 行为；
- 未验证 Linux 和 Windows 的窗口、焦点、输入法与进程退出行为。

## Known Limits

- spec 指定的设计文档位于仓库外的
  `/Users/lexleizou/Documents/个人开发项目/design-system/pi-workbench/`，
  其中 `interaction/flows.md` 的空草稿 follow-up 交互与当前实现一致；
- 建议生成依赖当前 Pi 和当前模型可用；生成失败不会打断主对话，当前 UI
  不展示生成错误；
- 只在主轮次完全 settled 后生成；不预生成、不流式更新，也没有重试或用户
  手动重新生成入口；
- 建议使用固定约 3000 runes 的上下文窗口，不做 token 估算；
- 卡片采用当前 Onevra 主题 surface/border/radius 和固定 8 的底部间距；
  精确视觉间距仍需真实窗口确认。
