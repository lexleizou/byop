# BYOP P11b 验证记录

日期：2026-10-24

范围：把上下文容量选择移入模型选择器，并让 Composer 模型控件始终显示已知容量。

## 已实现

- Composer 中独立的上下文容量 chip 已移除。容量选择只在模型选择器底部出现。
- 当前模型家族有至少两个不同的已知容量变体时，模型选择器在模型列表下方显示
  `上下文` 标签和宽 140px 的紧凑下拉框，并以现有 0.5px border token 分隔。
- 上下文下拉框复用 `chat.Picker` 和 `s.chatView.contextPicker`，不启用搜索。
  选择不同模型变体后关闭模型选择器并调用 `s.selectModel`。
- 模型选择器中的模型行没有增加容量文字。
- Composer 模型控件始终显示当前模型的已知容量，12px muted；未知容量不显示。
  只读状态沿用相同容量显示，且模型选择器与上下文选择器保持关闭。Pi 会话状态返回
  容量 0 时同步清空 HUD fallback，避免沿用上一个模型的容量。
- 单容量、未知容量、未找到活动模型家族和模型选择器关闭时，不显示或保留上下文
  选择行。
- 新增本地化键 `contextLabel`：英文为 `Context`，简体中文为 `上下文`。
- 应用版本更新为 `0.11.1`。

## 截图证据

本次最终复查时 macOS 报告 `CGSSessionScreenIsLocked=Yes`。以下图片由 MyGo
headless test renderer 生成，不是真实原生窗口截图，也不证明原生弹层的键盘、
焦点或点击行为。生成器已移出常规测试树，位于
`~/AgentScratch/20261024-byop-p11b/p11b_screenshot_test.go.txt`；`go test` 不会
重写截图：

- `docs/screenshots/p11b-picker-context-light-zh.png`：1180×760，Light，
  简体中文。模型选择器底部显示 `上下文` 和 `1M`，Composer 中没有独立上下文
  chip。
- `docs/screenshots/p11b-composer-dark-zh.png`：1180×760，Dark，简体中文。
  Composer 模型控件显示 `1M`，没有独立上下文 chip。

截图使用临时 seeded render state 生成长对话占位内容，不包含凭据或真实用户会话。

## 检查结果

| 检查 | 结果 |
|---|---|
| `go run github.com/egoist/mygo/cmd/mygo generate` | 通过；重新生成 `locales/catalog_gen.go` |
| P11b 聚焦测试 | 通过；7 项，覆盖 3 个 package |
| `gofmt -l .` | 通过；无输出 |
| `go vet ./...` | 通过 |
| `go build -o byop .` | 通过 |
| `go test ./... -count=1` | 通过；295 项，覆盖 3 个 package；不包含截图生成器 |
| `git diff --check` | 通过；无输出 |
| `go run github.com/egoist/mygo/cmd/mygo i18n check .` | 通过；`en`、`zh-Hans` 各 571 条消息 |

P11b 聚焦测试验证了多容量家族在 Composer 只显示一个容量标签、打开选择器前不存在
独立上下文标签、打开模型选择器后显示本地化上下文标签，以及 Pi 从 1M 返回未知
容量后清除旧 fallback。

## 未验证

- 最终复查时桌面已锁定，未进行真实 MyGo 窗口中的鼠标、键盘和焦点走查。
- 未连接真实 Pi 会话实际切换上下文变体；模型切换调用链和现有模型切换测试通过，
  但没有做端到端模型请求。
- 未验证 Linux 和 Windows。

## 已知限制

- 旧语言键 `contextSearch` 与 `contextNone` 已不再被界面调用；语言包检查没有要求
  删除，因此本阶段保留。
- 截图是 headless renderer 输出，不含原生窗口边框和平台管理浮层阴影。

## 规格偏差

无。

## Packaging and install

- Build: `go run github.com/egoist/mygo/cmd/mygo build -skip-notarize -skip-dmg` produced `dist/darwin-arm64/BYOP.app` (14.0 MB, version 0.11.1). The output folder is git-ignored.
- Install: the previous `/Applications/BYOP.app` (0.11.0) was moved to the Trash, then the new bundle was copied to `/Applications/BYOP.app`. `CFBundleShortVersionString` reads `0.11.1`.
- Launch: the installed app started from a terminal session and kept running (about 170 MB resident memory). macOS was locked during the launch, so no real-window screenshot was taken. The screenshots above are headless renders.
- Not verified: real-window keyboard, focus and click behavior; end-to-end model and context switching against a live Pi CLI; notarized distribution.
