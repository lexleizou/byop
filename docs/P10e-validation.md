# P10e 验证记录

日期：2026-10-23

阶段：插件卡统一、详情浮层与 P10 中英文明暗走查。

基线：`f845a64`（P10d）。本阶段改动仍在工作树中，未提交、未推送。

## 已验证

### 插件卡

- `tokens.extensions.json` 增加 `pluginCard.defaultHeight = 236`、`minWidth = 320`、`maxWidth = 360`，Go 渲染直接使用对应常量。
- 卡片固定 236px 高、12px 圆角、1px 边框、16px padding、`shadowCard`；标题、描述、能力和状态摘要均限制行数并省略，内容不会撑高卡片。
- 卡片结构固定为图标与状态、名称、两行描述、能力摘要、单行状态摘要、贴底操作行。底部操作行由 Spacer 推到卡片底边。
- 主操作按“主区原生视图 → 主区 WebView → 第一个命令 → 无操作”的优先级选择；命令运行时禁用命令按钮。
- 主操作和详情中的命令、视图、WebView、停用入口只在条目状态为已注册且清单可用时出现；不兼容、重复 ID 或清单无效的条目只显示标识、版本、权限和错误。
- 状态摘要按“注册错误 → 运行时错误 → 最近结果 → 版本和运行时 → 清单被拒绝”的优先级选择。
- Hover 只改变背景、边框和阴影，不改变宽高或 padding；减少动效开启时不会添加过渡。
- 聊天主区不足以容纳两张 360px 卡片、12px 列间距和 56px 页面内边距时（阈值 788px），卡片改为 FillWidth 单列；否则使用 320–360px 固定宽度网格。

### 详情浮层

- “详情”按钮使用本地化可访问标签，例如 “Details for Hello Node” / “Hello Node 的详情”。
- 卡片、主操作、详情触发器和浮层内部控件使用来源目录（缺失时使用 `ID+索引`）作为唯一键；相同插件 ID 的多条记录不会共享 Hover、浮层或渲染键。
- 浮层宽度为 360–420px，最大高度为 520px 与窗口高度减 80px 的较小值，内容在浮层内滚动。
- 已注册条目包含插件 ID、版本、运行时和宿主检查、完整权限、全部命令、停用入口、原生主视图、WebView 打开入口、WebView entry/隔离/CSP 信息、次级视图、宿主调用、拒绝调用、最近结果和完整错误；WebView 标题由卡片或详情中的标题承担，不在元信息行重复。
- 未注册条目只显示插件 ID、版本、权限和完整注册错误。
- 最近结果使用命令标题，与卡片状态摘要保持一致。
- 命令按钮在执行中禁用；停用只在 Active 状态出现。
- 同一时间只保留一个详情浮层。Esc 可关闭，点击外部也可关闭；切换页面、插件被移除、打开插件视图或 WebView 时会清除浮层状态。

### HUD 补充要求

- composer HUD 不再显示 cwd 或家目录缩短路径，只把 cwd 保留为 git 分支查询的内部输入。
- 分支存在时显示 `(main)`；token、缓存命中率、费用和上下文段及其排序、阈值和省略行为保持不变。
- `hud_test.go` 同时断言 cwd 不出现、分支显示，以及 token、cache、cost、context 段仍会显示。
- `layout_regression_test.go` 的 HUD 夹具改为显式提供分支，不再依赖 cwd 文本来生成 HUD，因此原有的 P10a/P10c composer 与窄窗口布局回归继续有效。

### 布局与视觉

- P10a/P10b 布局链保持不变：导航 `Grow(1).MinHeight(0)`、导航 footer `Shrink(0)`、main/chat 高度有界。
- 正文仍为 14px。
- 真实窗口只截取 BYOP，无账号、凭据或私人会话内容。macOS 未锁屏（`ioreg` 未报告 `CGSSessionScreenIsLocked`）。
- 在 1440×900 下完成中文/英文 × light/dark 的 Chat、插件页和设置页走查；Chat 均已连接并显示 `(main)` HUD，未见 cwd。
- 在 1100×560 内容区域完成窄窗口插件页走查：侧栏展开且任务面板收起时显示卡片网格；任务面板展开、聊天主区小于 720px 时变为单列 FillWidth。
- 详情浮层在真实窗口中可读，关闭后遮罩和浮层消失；Esc 与点击外部两条关闭路径均做了实机操作。

## 视觉证据

过程证据保存在
`~/AgentScratch/20261022-byop-p10e/evidence/`，以下为其中的主要文件：

| 场景 | 证据 |
|---|---|
| Chat 中文 light / dark | `chat-zh-light-1440x900.png` / `chat-zh-dark-1440x900.png` |
| Chat 英文 light / dark | `chat-en-light-1440x900.png` / `chat-en-dark-1440x900.png` |
| 插件页中文 light / dark | `plugins-zh-light-1440x900.png` / `plugins-zh-dark-1440x900.png` |
| 插件页英文 light / dark | `plugins-en-light-1440x900.png` / `plugins-en-dark-1440x900.png` |
| 设置页中文 light / dark | `settings-zh-light-1440x900.png` / `settings-zh-dark-1440x900.png` |
| 设置页英文 light / dark | `settings-en-light-1440x900.png` / `settings-en-dark-1440x900.png` |
| 1100×560 双列插件页 | `plugins-zh-light-1100x560.png` |
| 1100×560 单列插件页 | `plugins-zh-light-1100x560-task-panel.png` |
| 详情浮层 | `plugin-details-1440x900.png` |
| Esc / 点击外部关闭 | `plugin-details-escape-closed.png` / `plugin-details-outside-click-closed.png` |

仓库中的代表图：

- [Chat 中文 light](screenshots/p10e-chat-zh-light.png)
- [Chat 英文 dark](screenshots/p10e-chat-en-dark.png)
- [插件页中文 light](screenshots/p10e-plugins-zh-light.png)
- [插件页英文 dark](screenshots/p10e-plugins-en-dark.png)
- [设置页中文 light](screenshots/p10e-settings-zh-light.png)
- [设置页英文 dark](screenshots/p10e-settings-en-dark.png)
- [1100×560 双列插件页](screenshots/p10e-plugins-1100x560.png)
- [1100×560 单列插件页](screenshots/p10e-plugins-1100x560-task-panel.png)
- [插件详情浮层](screenshots/p10e-plugin-details.png)

## MyGo 限制

- 当前 palette 没有独立的 `borderStrong` token，插件卡 Hover 边框使用 `muted`，以保持既有主题体系。
- MyGo `PopoverBase` 使用系统原生呈现，阴影、锚定微调和窗口边缘避让不能完全由页面 token 控制；本阶段使用了系统实际呈现，并在真实窗口中检查可见性。
- 页面在快速切换语言、主题和页面时，曾出现一次 Window Server/ScreenCaptureKit 暂时无法枚举仍在运行的 BYOP 窗口；重启同一构建后恢复，所有最终证据均在恢复后的干净运行中重新截取。未观察到应用崩溃或数据变化。

## 未验证

- Linux 和 Windows 的原生窗口、字体、popover 位置、键盘和 Accessibility 行为未验证。
- 详情浮层内命令按钮沿用既有插件命令入口，本轮未对第三方扩展执行真实业务命令；按钮存在、busy 禁用和既有命令执行路径由代码与现有插件测试覆盖。
- 未新增或修改 `examples/*.mjs`，因此不需要 `node --check`。

## 检查

## 审核发现与修复

Opus 复审提出的 P2/P3 已处理：

- 非 `pluginRegistered` 或清单不可用的插件不再生成主操作或详情中的执行入口；新增不兼容插件无主操作单测。
- 插件卡和详情状态改用来源目录作为唯一键，来源目录为空时回退为 `ID+索引`；新增两个重复 ID 条目的渲染与浮层切换测试。
- 双列阈值改为 `360*2 + 12 + 56 = 788px`；单列卡片使用 FillWidth。
- 主操作按钮可收缩并设置最小宽度 0 和最大宽度 360，内部标签可省略；“详情”按钮不收缩。
- WebView 元信息不再重复标题；详情最近结果改为显示命令标题。
- 删除未引用的 `pluginsRuntimeMeta`、`pluginsSecondaryViews`，重新生成语言目录。

`borderStrong` 仍作为 MyGo 主题 token 的已知限制记录：插件卡 Hover 边框继续使用 `muted`。

## 检查

最终检查结果：

- `gofmt -l .`：无输出。
- `go vet ./...`：通过。
- `go build -o byop .`：通过。
- `go test ./... -count=1`：273 项通过，覆盖 3 个 package。
- `git diff --check`：通过。
- `go run github.com/egoist/mygo/cmd/mygo generate`：locale accessors 已是最新。
- `go run github.com/egoist/mygo/cmd/mygo i18n check .`：中英文各 552 条消息，通过。

## Remaining P3 after Opus re-review

- Fallback entry key (no `SourceDirectory`) uses filtered vs full list index; real scans always carry a directory, so only test data is affected.
- `pluginDetailsOpenID` now stores an entry key; a rename to `pluginDetailsOpenKey` is pending.
- Hover border uses `muted` until the theme exposes `borderStrong`.
- Review fixes were verified by headless layout/render tests; native-window screenshots were taken before those fixes.
