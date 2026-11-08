# P32 验证记录

版本目标：BYOP 0.25.0。基线：`13b18af`（0.24.0）。设计见 `~/AgentScratch/20261108-byop-p32/P32-design.md`。

## 范围

用户 2026-11-08 提出的五项：默认外观改 BoardUI、模型与渠道页 1:1 复刻 Proma 开源版、对话列宽跟随窗口、扶摇插件重做（含插件内对话、还款计划详情、滚动修复、BoardUI 化），以及当天追加的“对话区字号改为主题驱动”。

## 自动检查（2026-11-08 19:12–19:40 实测）

| 检查 | 结果 |
|---|---|
| `gofmt -l .` | 无输出 |
| `go vet ./...` | 通过 |
| `go build -o byop .` | 通过 |
| `go test . -count=1` | `ok byop 44.8s`，0 失败 |
| `git diff --check` | 干净 |
| 插件 `npm run check` | 61/61 通过 |
| `node --check plugins/lightweight-table/web/app.js` | 通过 |

## 真实窗口走查

隔离配置：`~/AgentScratch/20261108-byop-p32/verify/config`（不含任何真实凭据），插件目录指向仓库 `plugins/`，`BYOP_PI_CONNECT=0`；数据为合成 v4 快照 + 24 条合成记录（共 26 条）。截图在 `verify/shots/`。

### 已验证

| 项 | 证据 | 结论 |
|---|---|---|
| 新装默认外观为 BoardUI | `01-default-boardui.png`：无 `preferences.json` 时界面为不透明深色、圆角 24 外壳、蓝色强调，无壁纸无玻璃；`normalizeAppearancePreset` 兜底仍为 BYOP（`go test` 覆盖） | 通过 |
| 扶摇页面在 BoardUI 深色下正确呈现 | `03-fuyao-dark-fixed.png`：按钮与下拉有文字、分组卡、状态卡、分页脚、底部快捷键提示齐全 | 通过（修复后） |
| 单击选中一行 | `09-click-row.png`：行出现选中边框、勾选框打勾、表头呈半选态、分页脚显示“已选 1 条” | 通过 |
| 表格可滚动 | `06-pagedown.png`：PageDown 后内容从首批记录滚到中段；`14-heightchain-wheel.png` 真实滚轮同样生效 | 通过 |
| **选中一行后仍可滚动** | `15a-selected.png`（选中“合成记录 10”，底部“已选 1 条”）→ 真实滚轮 −420 → `15b-selected-then-wheel.png`（滚到“合成记录 04/07/14/13”，选中保持、表头半选） | 通过（修复后） |

### 走查中发现并已修复的问题

| # | 问题 | 根因 | 修复 |
|---|---|---|---|
| 1 | 页头按钮、查询栏下拉全是白块、无文字 | `app.js` 的 `onTheme` 回调把 payload 当字符串比较（`theme === "dark"`），实际收到的是 `{mode, colors, …}` 对象，`data-mode` 永远写成 `light`，于是 `--bui-raised` 保持浅色 #fff，而文字色来自宿主深色 → 白底白字 | 改读 `theme.mode`（兼容裸字符串），并在注册后主动应用一次 `byop.theme` |
| 2 | 列隐藏阈值对插件页过于激进（1366 窗口只显示 4–6 列） | `@container main` 用 1320/1150/1060/1000，而插件页容器只有约 1030 | 下调为 1240/1120/1040/980 |
| 3 | 选中一行后**滚轮**不再滚动（用户 19:26 直接反馈，是核心诉求） | 三处叠加：① `html/body/.table-page` 只有 `min-height`，flex 链撑不出受限高度，`#table-scroll` 实际没有可滚空间（**主因**）；② `td` 带 `tabindex=0`，鼠标点击本身就会聚焦它，WKWebView 把滚动目标改到该单元格；③ 上一版把焦点回交给 `#table-scroll`，容器持有焦点后同样走自己的滚动链 | ① `html,body{height:100%}`、`.table-page{height:100%;overflow:hidden}` 建立确定高度链；② 单击后 `blur()` 把焦点还给文档（`document.body`）；③ 容器 `tabindex` 改为 `-1`，不再进入 Tab 序列 |

### 未验证（工具限制或未覆盖）

| 项 | 原因 |
|---|---|
| ~~选中后用真实滚轮继续滚动~~ | **已于 19:40 验证通过**：用 CoreGraphics `CGEventCreateScrollWheelEvent` 发真实滚轮（Cua `scroll` 与 AppleScript PageDown 都不能代表滚轮），`14-heightchain-wheel.png` 证空白滚动生效，`15a/15b` 证明先选行（已选 1 条）再滚轮时表格正常滚动且选中保持 |
| 对话列宽跟随窗口 | 需要连接真实 Pi 并产生消息；本次为 `BYOP_PI_CONNECT=0`，对话区为空，无法测量内容列 |
| 对话区字号随主题变化 | 同上；字号由 `palette.chatFont` 驱动，已由 `TestP32ChatFontFollowsPreset` 覆盖三档取值，但未在真实窗口切主题观察 |
| 插件右侧调出当前对话 | 需要主对话存在；`BYOP_PI_CONNECT=0` 下无法验证面板内容、宽度拖拽与 `⌘⌥C` |
| 模型与渠道页 | 未走查（需真实 `models.json` 交互，避免污染本机 Pi 配置） |
| 还款计划 Tab 与计划详情子页 | 合成快照只有 1 个计划、2 期次，页面已有 Tab，但未逐项核对期次表渲染 |
| 「在 Pi 中登录」 | 会打开系统 Terminal，未在验收中触发 |
| 打包与安装 | 本次未构建 DMG |

## 已知限制

1. 高对比模式下主题 `fontSize` 不生效（`applyPluginThemeOverlay` 遇高对比提前返回，与颜色覆盖的既有语义一致）。
2. 行高不随主题字号缩放；24px 极端值下气泡内边距不变。
3. 渠道页编辑模式为显式「保存」按钮，非 Proma 的 600ms 自动保存（用户同意 MVP 收缩）。
4. 渠道页弹窗以设置列内卡片渲染（MyGo 无共享 modal host），Toast 由行内提示替代。
5. 渠道行图标为字母块，非供应商 Logo（图标集无品牌资源）。
6. 扶摇看板区仍是既有 dashboard 结构（健康条 + 三列），未替换为设计稿的 KPI 卡行；CSS 已重做但结构沿用，属设计与实现差异。
7. 公式化差异：插件页容器宽约 1030 时，`@container` 会隐藏分类与年化利率，需横向滚动查看其余列（sticky 操作列保持可见）。

## 未收敛项

- P32 的独立审核未完成：第一轮 Opus 5.5（Cursor @1m）运行 50 分钟未给出结论被停止；第二轮改派 Opus 5.5（GitHub Copilot）时协作通道已超时，派单被拒。本记录中的自查结论由协调方完成，**不等同独立审核**。
