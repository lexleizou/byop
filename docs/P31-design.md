# P31 设计 v2.1：新增 BoardUI 外观预设（BYOP 0.24.0）

- 日期：2026-11-07。v2 按 Cursor 设计审阅（delegation 8038e36b，attempt 17e15112，结论“修改后可以开发”，P0×2、P1×9）重写；v1 存为 `spec-v1.md`。v2.1 按复审（attempt 2a9a037d，上轮全部关闭，新 P1×4、审核方确认改完无需整轮复审）修订；v2 存为 `spec-v2.md`。
- 用户要求（2026-11-07 07:01、07:16）：用户消息使用右侧气泡；BoardUI 蓝色与 Inter；**以新增一套主题的方式**提供；中文**内嵌 Noto Sans SC**；其它审查建议一并修（代码块头部与复制、标题优先，见会话工作台 `ui-audit-boardui-20261107.md` F、G）。
- 基线：0.23.0（P30 完成并提交后）。开发必须在 0.23.0 之后开始：P30 也改 `chat_view.go`、`p29_view.go`、`p29_tones.go`、`diagram_card.go`、`THIRD_PARTY_NOTICES.md`。
- 参照：`reference/boardui-ai-chat/README.md`；BoardUI 免费层 `theme.css`（MCP `get_theme` 2026-11-07 读取）。只用公开 token 数值，不复制 Pro 源码或站点资产。

## 0. 范围与 MVP 取舍

做：
1. 新预设 `appearancePresetBoardUI = "boardui"`：配色、字体、外壳间距与圆角、不透明表面（无玻璃无壁纸）、用户右侧气泡、Composer 圆角。
2. 所有预设：代码块头部（语言 + 自建复制按钮）；标题优先与路径简写。

按 MVP 暂缓（写入验证文档“未做”）：助手侧 14px/28px 头像改造（保持 P29 的 15/16px、1.72、35px 头像、46px 缩进）；导航宽度/行高、标题栏高度、过渡时长、边框粗细（仍 0.5px）；HUD 环形上下文胶囊、面包屑标题、悬停时间、彩色语言标签；发送按钮渐变；JetBrains Mono。

## 1. 字体

### 1.1 文件与许可

源文件已下载到 `~/AgentScratch/20261107-byop-p31/`，校验：`cd ~/AgentScratch/20261107-byop-p31 && shasum -a 256 -c fonts.sha256`（全部 OK）。开发席只从这里复制。

| 仓库路径 | 来源 |
|---|---|
| `assets/fonts/Inter-{Regular,Medium,SemiBold,Bold}.ttf` | rsms/inter v4.1 `Inter-4.1.zip`（SHA-256 `9883fdd4…b11e`）`extras/ttf/` |
| `assets/fonts/NotoSansSC-{Regular,Medium,Bold}.otf` | notofonts/noto-cjk `Sans2.004` `18_NotoSansSC.zip`（`4d107c09…292ec`） |
| `assets/fonts/OFL-Inter.txt`、`assets/fonts/OFL-NotoSansSC.txt` | 两个 zip 内的 LICENSE 原文 |

- 许可：均为 SIL OFL 1.1，均无 Reserved Font Name。允许与 MIT 软件一起内嵌再分发；**不修改字体文件**（在内存中以 `BYOP Inter` 等别名注册不算修改）。OFL 第 2 条允许许可以字体元数据形式随附（两套字体的 name 表都内嵌许可文本）。
- `THIRD_PARTY_NOTICES.md` 追加两条：名称、版本、来源 URL、版权行（Inter：`Copyright (c) 2016 The Inter Project Authors`；Noto Sans SC：`© 2014-2021 Adobe (http://www.adobe.com/)`，并注明 `Noto is a trademark of Google LLC`）、许可、仓库路径 `assets/fonts/`。README 许可段注明 `assets/fonts/` 适用 OFL 1.1。
- DMG 内许可文本：A 席查 MyGo 0.5.5 打包是否支持把额外文件放进 `Contents/Resources`；支持则放入两份 OFL 文本；不支持则不自造机制，靠字体元数据，并在验证文档里写明。
- 体积：约 27 MB 未压缩；字体由 go:embed 无条件嵌入，仓库与二进制都会变大；注册时复制数据，常驻内存相应增加。验证文档记录 DMG 与二进制体积增量。

### 1.2 注册与使用

- 新文件 `fonts_embed.go`：`//go:embed assets/fonts/*.ttf assets/fonts/*.otf`。
- `ensureBoardUIFonts() error`：`sync.Once`，对 Inter 四个文件调用 `ui.RegisterFont(data, "BYOP Inter")`，Noto 三个文件调用 `ui.RegisterFont(data, "BYOP Noto Sans SC")`。同族多文件由 Core Text 按字重最近匹配（`coretext_darwin.go:862`、`:1510-1515`）；OTF（CFF）可读。
- 调用时机：在 `nativeThemeForAppearance` 的 BoardUI 分支里调用 `ensureBoardUIFonts()`（`sync.Once`），启动与切换都经过这里。其它预设不调用。失败只写日志，字体族回退系统字体。`main.go` 不需要为字体改动。
- 常量 `boardUIFontFamily = "BYOP Inter, BYOP Noto Sans SC, system-ui"`（MyGo `Font()` 逗号列表，后面的注册族进入 Core Text 回退级联，`engine.go:221`、`coretext_darwin.go:672-697`）。代码仍用 `monospace`。
- 应用点：`nativeThemeForAppearance` 为 BoardUI 设 `t.Font`；`configureMarkdownV12` 的 `theme.Font` 与 P29 `processTheme` 继承。插件主题资源若设了字体族仍优先（现有顺序）。
- 已知限制：Noto 无 SemiBold，600 落到 Medium，中英混排 600 粗细略不同；P30 图解卡片在 WebView 中，看不到进程内注册的字体，仍用系统字体。

## 2. 配色（新文件 `boardui_theme.go`，`boardUIThemePalette(dark, high, reduceTransparency bool) palette`）

蓝色取 BoardUI 免费层 `theme.css`：accent = Tailwind v4 `blue-*`，其中 **blue-400 被 BoardUI 覆盖为 #3392ff**。中性色 Tailwind neutral，BoardUI 覆盖 100=#f7f7f7、200=#ebebeb、925=#121212。

| accent 色阶 | 50 | 100 | 300 | 400 | 500 | 600 | 700 | 800 | 950 |
|---|---|---|---|---|---|---|---|---|---|
| hex | #eff6ff | #dbeafe | #8ec5ff | #3392ff | #2b7fff | #155dfc | #1447e6 | #193cb8 | #162456 |

| palette 字段 | 浅色 | 深色 | 说明 |
|---|---|---|---|
| background | #ffffff | #121212 | 页面底 background-full |
| surface | #f7f7f7 | #171717 | 侧栏卡片、聊天卡片 |
| surfaceSubtle | #ebebeb | #262626 | tertiary |
| surfaceHover | #ebebeb | #262626 | secondary-hover |
| surfacePressed | #d4d4d4 | #404040 | |
| border | #ebebeb | #404040 | border-button-default |
| borderStrong | #d4d4d4 | #737373 | border-button-hover（深色 neutral-500） |
| text | #0a0a0a | #fafafa | text-primary |
| muted | #696969 | #a1a1a1 | **偏差**：BoardUI #737373 在 #f7f7f7 上 4.43、深色在 #171717 上 3.78 |
| accent | #155dfc | #155dfc | **偏差**：取 600 而非 500，使现有“白字配 accent 底”的按钮与 MyGo 主按钮/选中项达到 5.25 |
| accentHover | #1447e6 | #1447e6 | 700，白字 6.83 |
| accentPressed | #193cb8 | #193cb8 | 800，白字 8.82 |
| accentText | #1447e6 | #8ec5ff | 普通表面上的强调文字（浅色在 #ebebeb 上 5.73） |
| onAccent | #ffffff | #ffffff | |
| focus | #2b7fff | #2b7fff | accent-500，图形 ≥ 3 |
| selection | #dbeafe | rgba(22,36,86,.60) | |
| danger / dangerText | #fb2c36 / #c10007 | #ff6467 / #ff6467 | 浅色 dangerText **偏差**：red-600 在 #f7f7f7 上 4.45 |
| success / successText | #7ccf00 / #3c6300 | #9ae600 / #9ae600 | |
| warning / warningText | #f0b100 / #894b00 | #fdc700 / #fdc700 | |
| readingSurface | #f7f7f7 | #171717 | 聊天卡片 |
| raisedSurface | #ffffff | #262626 | 用户气泡、代码卡 |
| composerSurface | #ffffff | #262626 | |
| popoverTint / popoverBorder | #ffffff / #ebebeb | #262626 / #404040 | |
| glassTint | = surface | = surface | 不使用玻璃，只为兼容旧调用 |
| scrollbar | rgba(0,0,0,.18) | rgba(255,255,255,.18) | |
| shadow / shadowCard | rgba(0,0,0,.06) / rgba(0,0,0,.05) | rgba(0,0,0,.16) / rgba(0,0,0,.14) | sidebar 中层 / shadow-card |
| shadowOverlay / shadowStage | rgba(0,0,0,.10) / 同左 | rgba(0,0,0,.20) / 同左 | 命令面板、扩展对话框、`pages.go:721`、高对比浮层使用；不得留零值 |

- **高对比度**（不走 BYOP 回退，避免注入橙色）：muted 浅 #3a3a3a / 深 #cecece；border 与 borderStrong 浅 #737373 / 深 #a1a1a1；focus 仍 #2b7fff；其余不变。
- **减少透明度**：BoardUI 本来就不透明，配色不变。
- **插件主题覆盖**：与其它预设一样在最后调用 `applyPluginThemeOverlay(&p)`。
- **链接与强调文字**（复审 P1-C）：`accent` 取 #155dfc 后，深色下作为文字不达标（#171717 上 3.42，#262626 上 2.88）。BoardUI 下：`configureMarkdownV12` 设 `theme.Link = p.accentText`（MyGo 默认 `Link = t.Accent`）；思考强度弹层当前档位文字（`chat_view.go:1569` 一带）改用 `p.accentText`。两处都在 B 席。深色下选中图标、勾选标记、输入光标（accent 在 #262626 上 2.88）不另调色，写入已知限制。
- **代码高亮**：`chat_view.go` 里关键字/类型当前用 `p.accentPressed`，BoardUI 下改用 `p.accentText`（深色 #8ec5ff 在 #262626 上 8.35）。其它预设不变。
- **对比度矩阵（测试强制，浅/深 × 标准/高对比）**：
  - 文字 ≥ 4.5：{text, muted, accentText, dangerText, successText, warningText} × {background, surface, surfaceSubtle, raisedSurface}；onAccent × {accent, accentHover, accentPressed}。
  - 图形 ≥ 3：focus × {background, surface, surfaceSubtle}；accent × {background, surface}。
  - 协调方已算：标准浅色最低为 muted/#ebebeb 4.61、focus/#ebebeb 3.16；深色最低 dangerText/#262626 5.24、focus/#262626 4.02。
- **P29 过程区背景**：`p29EffectiveBackground` 对 BoardUI 返回 `p.surface`（文字实际放在聊天卡片上），并把 BoardUI 浅/深加入 `p29_contrast_test.go` 矩阵；若某个 p29 色调不达标，只允许提高不透明度（与 P30 规则一致）。

## 3. 尺寸：字段 → 读取位置 → 改动

BoardUI 的 `metricsForAppearancePreset` 分支以 BYOP 为底，只改下列字段。其余字段与 BYOP 相同（字号 14/12/18 与 BYOP 一致，不产生字号例外）。

| 字段 | BoardUI 值 | 当前读取位置 | 改动（保证 BYOP 值不变） |
|---|---|---|---|
| shellSpacing | 12 | 全局 `shellGap = 8` | 恰好四处改读 `p.shellSpacing`：`shell.go:82`（外边距）、`:460`、`:461`（导航宽度 + 与主区间隔）、`:508`（调宽手柄宽度）。保留不动：`appearance_preset.go:125`（BYOP metrics 来源）、`:156`（`trafficLightY`）、`p13b_test.go`。`chatMainWidth`、`navExpandedWidthLimit`、设置页宽度已读 `shellSpacing()`，改完后布局与宽度计算一致 |
| mainRadius | 24 | `shellRadius`（token 12）：`shell.go:143,151,462,1438,1447`、`glass_ui.go:410` | 全部改读 `p.mainRadius`（BYOP metrics 已等于 token 值）；`shell.go:1447` 的 `shellRadius - .5` 改为 `p.mainRadius - .5` |
| composerRadius | 24 | BYOP Composer 写死 `v12InputRadius`（`ui_v12.go` `opaqueComposerV12`） | `styleComposerSurface(card, p, v12InputRadius)` 改为 `p.composerRadius`（BYOP metrics = 8） |
| userBubbleRadius（新字段） | 16 | 无 | 仅 BoardUI 气泡使用；其它预设为 0 且不读取 |

删除 v1 中不生效或暂缓的字段：navItemHeight、sessionRowHeight、navGap、导航宽度三项（BoardUI 沿用 BYOP 的 200/160/360 与 `NavExpandedWidth` 偏好）、transitionMS（沿用 BYOP 偏好）、chatContentWidth/chatHorizontalPadding（Onevra 写死 720/20）、chatBottomPadding、composerMinHeight、composerPadding、标题栏高度。

## 4. 玻璃与壁纸关闭（A 席）

J Agent 并未关闭玻璃，v1 的类比作废。BoardUI 下一律不透明、无壁纸：

1. 新增 `func glassAllowed(p palette) bool { return p.preset != appearancePresetBoardUI }`。`deepLiquidGlassActive(...)` 共 6 处调用：`wallpaper.go:59`、`glass_theme.go:30`、`shell.go:441,576,1440`、`chat_view.go:474`。其中 `chat_view.go:474`（`emptyTranscriptForegrounds`）的第一个条件已是 `preset != JAgent`，对 BoardUI 直接返回 false，**不改**（且属 B 席文件）。其余 5 处在 BoardUI 下等价于 false：有 `p` 的地方改为 `glassAllowed(p) && deepLiquidGlassActive(...)`；无 `p` 的 `appState` 方法用 `s.currentAppearancePreset() != appearancePresetBoardUI`。不改 `deepLiquidGlassActive` 本身的签名。交付时列出全部调用点及处理。
2. `activeWallpaper()`：BoardUI 返回 `"none"`。
3. 标题胶囊与右侧按钮组（`shell.go:112-130`，现在浅色也走 `productGlassWithTint`）：BoardUI 走不透明分支——胶囊 `Background(p.raisedSurface)` + `Border(0.5, p.border)` + `Shadow(0,1,2,0, rgba(0,0,0,.05/.18))`；按钮组同样式。
4. 浮层：`stylePopoverGlass` 对 BoardUI 返回不透明样式（见第 5 节 popover），调用方（`ui_v12.go:35-39` 等）不改。
5. 设置页：BoardUI 下隐藏壁纸行、玻璃三行（导航透明度、模糊、折射）、玻璃预览与 `glassStatusText`（`settings_page.go:442-447` 一带，`glass_ui.go:330-344`）；侧栏底部玻璃按钮 `navigationGlassButton`（`glass_ui.go:9`）在 BoardUI 下不构建。主题模式（系统/浅/深）在 BoardUI 下可用；`effectiveDark`/`effectiveThemeSource` 只对 J Agent 强制深色。

## 5. 表面函数在 BoardUI 下的返回值（A 席实现，B 席只调用）

阴影写法为 `Shadow(x, y, blur, spread, color)`；MyGo `Shadow()` 可追加多层（`element.go:1066`）。浅/深两列是颜色透明度。

| 函数 | 圆角 | 边框 | 背景 | 阴影 |
|---|---|---|---|---|
| `styleComposerSurface(el, p, r)` | r（调用方传 `p.composerRadius`） | 0.5 `p.border` | `p.composerSurface` | (0,1,2,0) .05 / .18 |
| `styleRaisedSurface(el, p, r)` | r | 0.5 `p.border` | `p.raisedSurface` | (0,1,2,0) .05 / .18 |
| `stylePopoverGlass(el, p, r)`：BoardUI 分支必须在函数第一行 `popoverShadow` **之前**返回，否则阴影叠加 | r | 0.5 `p.popoverBorder` | `p.popoverTint` | (0,1,1,0) .04 + (0,4,4,0) .02 / .14 + .10 |
| 侧栏卡片：在 `navGlassCard` **开头**加 BoardUI 分支，直接套用本行样式并构建内容后返回，不经过 `glassChrome`（否则浅色与深色都会落到 `productGlassWithTint`） | `p.mainRadius` | 0.5 白（深色 #27272a） | `p.surface` | (0,1,0,0) .02 + (0,1,12,0) .06 + (0,0,1,0) .32 / .10 + .16 + .42 |
| 主区聊天卡片（`shell.go:1438` 一带） | `p.mainRadius` | 无 | `p.readingSurface` | 无 |

其它预设的返回值不变。聚焦描边仍由 Composer 组件（`ui_v12.go` `card.BorderColor(t.Focus)`，B 席）负责，BoardUI 下不改。

## 6. 用户消息右侧气泡（仅 BoardUI，B 席，`p29_view.go` 的 `p29UserTurn`）

- 不显示“你/You”头像与头部行；外层列保留 `.Label(text("p29UserLabel"))` 供无障碍使用。
- 气泡元素：`AlignSelf(ui.End)`、`MaxWidth(bubbleMax)`、`Padding(11, 12)`、`Radius(p.userBubbleRadius)`、`Background(p.raisedSurface)`、`Shadow(0,1,1,0, rgba(0,0,0,.05 浅 / .14 深))`、无边框；不设 MinWidth，短消息宽度跟随内容。
- 正文：`components.Text(...).FontSize(p29FontBody).LineHeight(p29LineHeight).TextColor(p.text).Selectable()`（与助手侧同字号，属 P29 对话区既有例外）。
- `bubbleMax = max(min(colW*0.8, 560), 240)`，其中 `colW = min(720, s.chatMainWidth(windowWidth) - 40)`，窗口宽用 `c.Size()`；算成像素后传给 `MaxWidth`。
- 附件芯片行在气泡上方，`FillWidth().Justify(ui.End)`。
- 时间戳不显示（暂缓）。不影响滚动锚定、折叠、复制；助手侧完全不变。
- BYOP、J Agent 下 `p29UserTurn` 不变。

## 7. 代码块头部（所有预设，B 席，`chat_view.go` 代码块钩子的 `render()`）

- 只影响普通代码块的默认渲染；插件渲染器、文件卡片、P30 Mermaid 图解卡片路径不变。
- 卡片仍由 `styleRaisedSurface(card, p, r).Clip()` 生成：圆角 BYOP/J Agent 保持 8，BoardUI 为 16。
- 头部行：`Height(32)`、`Padding(0, 6, 0, 10)`、`AlignItems(Center)`、底部 0.5px `p.border` 分隔线。
  - 左：语言名（`block.Language`，空时 `text("chatCodeLanguageFallback")`），`Font("monospace")`、`FontSize(fontCaption)`、`p.muted`、单行省略。
  - 右：自建复制按钮——`ui.ButtonBase` 24×24、圆角 6、图标 14px、悬停/聚焦底 `p.surfaceHover`、`Label`/tooltip `text("chatCodeCopy")`；点击 `c.WriteClipboard(block.Code())`，图标换成对勾、tooltip 与 Label 换成 `text("chatCodeCopied")`，1200ms 后恢复（`c.After(1200*time.Millisecond)`）。未闭合（流式）块也可复制当前内容。**不用** MyGo `CodeBlockView.CopyButton`（未闭合禁用、文案来自 MyGo 语言包、状态私有）。
- 复制状态：`chatView` 新增 `codeCopied map[string]time.Time`，键 = `block.Language + "|" + strconv.Itoa(block.Index) + "|" + fnv64(block.Code())`（代码块钩子是所有消息共用的闭包，拿不到消息 key）。两条消息里完全相同的代码块会一起显示“已复制”，写入已知限制。每帧删除超过 1200ms 的键。
- 正文：现有 `ui.Row(c).FillWidth().Padding(8, 4, 0, 4)` 改为 `Padding(4, 4, 0, 4)`（头部已提供上方间距），其它不变。
- 新文案（A 席加入 locales）：`chatCodeCopy` 复制代码 / Copy code；`chatCodeCopied` 已复制 / Copied；`chatCodeLanguageFallback` 代码 / Code。

## 8. 标题优先（所有预设，A 席，`shell.go` `titleBarHeading`）

- 会话标题：`Shrink(0)`、`MaxWidth(s.titleCapsuleMaxWidth(w) * 0.6)`、单行省略。
- 路径：显示 `"· " + filepath.Base(工作区)`（工作区为 `~` 时显示 `~`；工作区为空时不显示路径段，避免 `filepath.Base("")` 得到 `.`），`Shrink(1)`；tooltip 仍为完整 `workspaceDisplayPath()`。分支与菜单不变。
- 不做面包屑样式。

## 9. 设置与文案

- 视觉预设分段增加第三项：`settingsAppearancePresetBoardUI`（中英均为“BoardUI”）。
- `normalizeAppearancePreset("boardui")` 返回新预设；未知值仍回退 BYOP。
- 生成与检查：`go run github.com/egoist/mygo/cmd/mygo@v0.5.5 generate .`、`... i18n check`。

## 10. 仓库规则例外（A 席写入）

在仓库 `AGENTS.md` 增加“BoardUI 预设局部例外”：仅在 BoardUI 预设下，允许圆角 16/24、外壳间距 12、内嵌字体 Inter + Noto Sans SC、蓝色配色、多层阴影与其它 BoardUI 公开 token 数值；字号、边框（一律 0.5px）、过渡仍按仓库规则。代码块头部与标题优先是所有预设的普通行为变更，不属例外。同时在 `design-system/pi-workbench/` 新增 `boardui-preset.md`，记录第 2 节配色表与来源（不改 `tokens.json`）。

## 11. 测试

A 席：
1. `boardui_theme_test.go`：第 2 节矩阵（浅/深 × 标准/高对比）。
2. 字体：调用 `ensureBoardUIFonts()` 后 `ui.FontFamilies()` 含 `BYOP Inter` 与 `BYOP Noto Sans SC` 且 `Registered`；切到 BYOP 与 J Agent 不触发注册（计数注入点）。
3. 预设切换与持久化：BYOP → BoardUI → J Agent → BYOP，回读 `preferences.json`。
4. 玻璃关闭：BoardUI 深色、标准对比下 `activeWallpaper()=="none"`，设置页找不到壁纸/玻璃行文案，侧栏无玻璃按钮；BYOP 深色下这些仍在。
5. 尺寸：BoardUI 下侧栏左缘 = 12、Composer 卡片圆角 = 24（可用 Draw 记录或像素探针）；BYOP 下保持 8 / 8。
6. 标题优先：长路径 + 短标题时标题完整；路径只显示最后一级。三套预设都测。

B 席：
7. 气泡：BoardUI 下用户消息正文矩形右缘 = 列右缘 ± 1、左缘 > 列左缘；长消息宽 ≤ `bubbleMax`；短消息宽 < 240；`Find(text("p29UserLabel"))` 返回的矩形是外层列（宽度≈列宽），而不是 35×35 的头像或头部文字（无任何高度 ≤ 40 的“你/You”匹配）；BYOP、J Agent 既有 P29 测试继续通过。气泡加 `.Clip()`，超长无断点字符串不撑出。
8. 代码块头部：三套预设都能找到语言名与“复制代码”；点击后 `tt.Clipboard()` = 代码原文、出现“已复制”；`tt.Advance(1300ms)` 后恢复；未闭合块也可复制；闭合 Mermaid 块仍走图解卡片（无复制代码头部）。
9. P29 对比度：`p29_contrast_test.go` 矩阵加入 BoardUI 浅/深，背景取 surface。
10. P29.1 对齐测试：0.22.1 会把 `composer_alignment_test.go` 合入主分支（P31 基于 0.23.0，开发时它已存在）；在其中的外观列表里加入 BoardUI，所有断言不放宽。若 0.23.0 里不存在该文件，停下并报告。
11. 代码高亮：BoardUI 深色下关键字颜色 = `p.accentText`。

既有测试全部通过。

## 12. 分工（独立副本，禁止交叉写）

- **A GLM（主题、字体、外壳）**：`appearance_preset.go`、新 `boardui_theme.go`、`theme.go`（含调用 `ensureBoardUIFonts`）、新 `fonts_embed.go` + `assets/fonts/**`、`materials.go`、`shell.go`、`glass_theme.go`、`glass_ui.go`、`wallpaper.go`、`settings_page.go`、`main.go`（仅 `appVersion` 0.24.0）、`mygo.json`（0.24.0）、locales（全部四个新键）、`THIRD_PARTY_NOTICES.md`、仓库 `AGENTS.md` 例外段、README 许可段、测试 1–6。`design-system/pi-workbench/boardui-preset.md` 由协调方写。
- **B Qwen（对话区）**：`p29_view.go`（仅 `p29UserTurn` BoardUI 分支）、`p29_tones.go`（`p29EffectiveBackground`）、`p29_contrast_test.go`、`chat_view.go`（代码块头部与复制状态、BoardUI 关键字颜色）、`ui_v12.go`（`opaqueComposerV12` 改读 `p.composerRadius`）、测试 7–11。
- 共享契约（A 先定）：`appearancePresetBoardUI`、`visualMetrics.userBubbleRadius`、`boardUIThemePalette`、`boardUIFontFamily`、`ensureBoardUIFonts`、`glassAllowed`。B 在 A 交付前可用未跟踪的 `zz_stub_boardui.go`（常量 + metrics 字段 + palette 最小实现）编译，交付时删除。`text("chatCodeCopy")` 等为运行时查找，B 无需等 locales；B 在独立副本里的测试用 `text("chatCodeCopy")` 等 key 定位，不写死中英文。

## 13. 协调方验收

gofmt、vet、`go test -count=1 .`、i18n check、`node --test assets/diagram/`、插件 17 项、diff check → Cursor 审核至放行 → 原生：BoardUI 浅/深/高对比、中英、气泡、代码复制、标题、预设切换（含切回 BYOP 壁纸/玻璃恢复）、真实 Pi 生成与 t/s、P30 图解、窄窗 → `docs/P31-validation.md`（含偏差、暂缓项、体积增量；已知限制至少包括：红绿灯位置只在建窗时设定、P30 图解卡片圆角仍为 8 且用系统字体、深色下 accent 图形 2.88、相同代码块共享“已复制”、Noto 无 SemiBold、深色白字在 danger 上 2.89（既有））→ 打包 0.24.0 → 文档 → commit/push。

## 14. 不做

Pro 模板的流光边框、玻璃高光、无穷彗星、任务进度卡、三栏代码面板、反馈按钮；第 0 节列出的暂缓项；字体子集化；修改 BYOP/J Agent 既有视觉（第 7、8 节除外）。
