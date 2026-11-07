# P30：壁纸下的可读性、时间线清晰度与 Pele 对话内直接渲染

日期：2026-11-06。基线：BYOP 0.22.0 / `eda7a3c`。目标版本 0.23.0；Mermaid 插件保持 0.2.0 不改。
用户 2026-11-06 21:33 明确要求（附 J Agent 雾山壁纸截图）：①该背景下内容很难识别；②当前时间线很难看清，需要改进；③Pele 渲染改为在对话（上下文窗口）内直接渲染。用户已授权实现，无需再次请求启动。

## 0. 原则与边界

- 先设计后开发；先正向流程，再补必要异常；不新增设置项、开关或通用抽象。
- P29 商业版的结构、几何、图标与动效时长保持不变。**仅对比度相关的颜色/透明度与时间线线宽**按本规格调整：用户本次要求优先于 P29 照搬的商业版透明度（商业版背景不透明，透明度在壁纸上失效）。
- Pele 固定 `pele@1.7.0`，复用仓库内 `plugins/mermaid-diagram/web/vendor/pele.mjs`（不复制、不修改，SHA 不变）。宿主内置渲染，不依赖用户是否安装插件。
- 不复制商业版代码/资产；继续 Go/MyGo 原生聊天界面，仅图解卡片主体使用系统 WebView（原图解设计 `design-system/pi-workbench/frontend/diagram-rendering.md` 已定）。

## 1. 可读性与时间线（问题①②）

### 1.1 J Agent：每轮恢复深色玻璃底

P29 之前 J Agent 用玻璃底承载对话；P29 去掉后文字直接压在浅色壁纸上。恢复如下（BYOP 预设不加卡片，保持 P29 商业版无卡片排版）：

| 元素 | 规格 |
|---|---|
| 用户轮（头像/你/时间/正文/附件） | 包一层 `jAgentGlassSurface(radius 12, blur 14, tint RGBA(27,27,27,0.80))`，内边距 12（上下）× 16（左右） |
| 助手轮（头部+过程+通知+最终回答+动作行） | 同上，单独一张卡；与用户卡间距 10 |
| 卡内几何 | 35px 头像、双行头部、46px 缩进、时间轴参数全部沿用 P29 |
| 高对比/减少透明度 | `jAgentGlassSurface` 已回退为不透明 `p.surface`，保持 |

卡片不随流式频繁改变外观；折叠/揭示动画仍作用于卡内容器，卡片本身只随内容高度变化（无额外动画）。

### 1.2 文字与时间线色调（两预设通用）

把 P29 中散落的 `p.text.Alpha(x)` 统一为按调色板计算的一组色调（建议新类型 `p29Tones`，由 `p29TonesFor(p palette)` 返回；仅此一处定义）。起始值如下，最终以 1.3 的对比度测试为准，可上调不可下调：

| 用途 | P29 现值 | P30 起始值 |
|---|---|---|
| 模型名、用户“你”标签 | .60 | .84 |
| 时间、副摘要、动作区状态词 | .38 | .64 |
| 过程统计标题 | .55 | .80 |
| 统计图标 ListTree | .45 | .72 |
| 统计/节点箭头 chevron | .40/.35 | .62 |
| 活动状态文字（非活跃） | .45 | .72 |
| 活动状态文字（活跃） | .65 | .94 |
| 节点图标（工具/Brain 等） | .45 | .72 |
| 思考圆点（非活跃/活跃） | .35/.60 | .62/.86 |
| 思考卡虚线边框 | .16 | .38 |
| 思考卡标题“思考/Thinking” | .45 | .72 |
| 思考卡正文、工具副摘要 | .55 | .78 |
| 时间轴竖线 | `p.border.Alpha(.70)`，1px | `p.text.Alpha(.46)`，**1.5px** |

- 失败图标/文字仍用 `p.danger`，透明度 .85 → 1.0。
- 高对比模式：上表全部文字用 `p.text`（1.0），竖线与图标用 `p.text`。
- 实时 t/s 胶囊与输出 token：只改显示颜色以满足对比度，**不改** `hud.go` 中的计算、更新、30s 失效与颜色分级逻辑。

### 1.3 对比度要求（测试强制）

新增纯函数 `wcagContrast(fg, bg ui.Color) float64`（WCAG 2.x 相对亮度）。先把半透明前景按 alpha 合成到“有效背景”上再计算：

- BYOP 浅/深：有效背景 = `p.background`。
- J Agent：有效背景 = 卡片 tint RGBA(27,27,27,0.80) 合成到**纯白 #FFFFFF** 上（保守最坏情况，壁纸任意区域都不比纯白更亮）；减少透明度/高对比时 = `p.surface`。
- 要求：所有**文字**色调 ≥ 4.5:1；竖线、节点图标、思考圆点、虚线边框等**图形** ≥ 3:1。
- 测试覆盖：BYOP 浅、BYOP 深、J Agent（玻璃）、J Agent 减少透明度、高对比 五种调色板 × 1.2 全部条目。测试失败即调高该条透明度，不得降低阈值。

## 2. Pele 对话内直接渲染（问题③）

### 2.1 用户流程（正向）

1. 模型回答中出现 ```` ```mermaid ```` 代码块。流式中、代码块**未闭合**时照常显示源码。
2. 代码块闭合（沿用 P28 的 `mermaidPreviewBlockSource` 判定：语言 mermaid、已闭合、非空）后，该位置变为**图解卡片**，就地显示 Pele 渲染的 SVG；只渲染一次，不逐 token 重绘。
3. 历史会话载入后同样就地显示（按可见性懒创建）。过程内的 commentary 中的 Mermaid 也走同一钩子。
4. 不需要安装 Mermaid 插件。P28 的“预览图解”按钮**移除**，由卡片取代；插件已安装可用时，卡片工具栏多一个“在 Mermaid 页面打开”（复用 P28 `openMermaidPreview` 路径），作为原设计的“独立预览”。

### 2.2 卡片外观（沿用原图解设计）

| 项 | 规格 |
|---|---|
| 外框 | 与代码块一致：`styleRaisedSurface(card, p, 8).Clip()`；宽度跟随内容列（46px 缩进后），最大 720 |
| 头部行 | 高 32，内边距 6×10；左侧标签“图解 / Diagram”，Agentflow 为“流程示意 / Flow sketch”（12px，色调用 1.2 的“时间/副摘要”）；右侧工具栏 |
| 工具栏 | 顺序：缩小、放大、适应宽度（开关态）、查看源码（开关态）、复制源码、在 Mermaid 页面打开（仅插件可用时）。按钮高 26、图标 14（Lucide：ZoomOut、ZoomIn、Maximize2/“适应”、Code、Copy、ExternalLink）。默认透明度 0，卡片 hover 或任一按钮获得焦点时 150ms 淡入；减少动态时直接显隐。按钮始终在可访问树中可按 |
| 主体 | WebView，背景为**不透明** `p.surface`（J Agent 下也不透明，保证图在壁纸上可读）；高度见 2.4 |
| 查看源码 | 在主体下方追加原代码块 `block.Body(c)`（不替换图） |
| 复制源码 | 写剪贴板并 toast“已复制源码” |

### 2.3 状态

| 状态 | 显示 |
|---|---|
| 等待/加载（视图未建或渲染中） | 主体高度用缓存高度（无缓存 160），中间 12px 文案“正在渲染图解…”，不闪烁 |
| 成功 | SVG |
| 失败（Pele 报错、超时 5s、页面加载失败） | 主体替换为一行 `p.danger` 文案“图解无法渲染：{message}”（有行号时附“（第 {line} 行）”），其下**直接显示原代码块**；工具栏只保留查看源码/复制 |
| 过长（> 20,000 UTF-16，沿用 P28 上限） | 不渲染，显示“图解过长，未渲染”+ 原代码块 |
| 达到并发视图上限且卡片不在视口 | 保持占位（缓存高度），进入视口时再建 |

### 2.4 尺寸与缩放

- 页面渲染后返回自然尺寸 `width,height`（CSS px，Pele `maxWidth` 固定 720）。
- 默认“适应宽度”：`scale = min(1, innerWidth / width)`；`innerWidth` = 卡片主体实绘宽度（由 Draw rect 得到，宽度变化 >1px 时重新下发 scale）。
- 放大/缩小：scale 乘/除 1.25，范围 0.25–4，点击即退出“适应宽度”；再点适应宽度恢复。
- 主体高度 = `clamp(height × scale, 48, 560)`；超出时页面内纵向/横向滚动，缩放大于适应值时可拖动平移（页面内实现）。
- 缓存：按 source 键记住自然尺寸，视图回收重建后不跳动。

### 2.5 主题与字体

- 宿主按原设计映射表把颜色传给页面（CSS 变量 `--pele-*`）：bg=`p.surface`，fg/line=`p.text`，muted=1.2“副摘要”色调合成后的不透明色，surface=`p.raisedSurface`（不透明化），surface-alt=`p.surfaceHover`，border=`p.border`，accent=`p.accent`，error=`p.danger`，chart1–5 依次用 `p.accent`、`p.success`、`p.warning`、`p.danger`、`p.text.Mix(p.surface,.4)`（调色板无专用图表色）。
- 字体：宿主系统字体；字号 15（与 P29 正文一致）。
- 切换明暗/预设/高对比：只调用页面 `setTheme` 更新变量，不重新解析。高对比时线条与边框用 text 色。

### 2.6 安全

- 新自定义协议 `byop-diagram://`（`mygo.Protocol.Handle`），只提供 go:embed 的固定文件：`card.html`、`card.mjs`、`card.css`、`diagram-renderer.mjs`（来自插件，复用 `validateSource`）、`vendor/pele.mjs`。其他路径 404。
- 响应头 CSP：`default-src 'none'; script-src byop-diagram:; style-src byop-diagram: 'unsafe-inline'; img-src data:; font-src 'none'; connect-src 'none'; base-uri 'none'; form-action 'none'`。
- WebView：拒绝全部权限、禁止新窗口；`OnWillNavigate` 一律 `PreventDefault`，若目标为 http/https 则交给系统浏览器打开（实现原设计“链接用户点击后用系统浏览器打开”）。
- 页面没有任何宿主 bridge；宿主只通过 `Page.EvalContext` 调用 `window.byopDiagram.*`，源码与主题以 `json.Marshal` 后的字面量传入。
- SVG 注入前做最小清理：移除 `<script>`、`<foreignObject>`、所有 `on*` 属性、`javascript:` 链接（纯函数，Node 测试覆盖）。SVG 文字可选择复制（原设计要求），`init`/`config` 前言仍被 `validateSource` 拒绝。

### 2.7 资源

- 同时存在的图解 WebView 上限 `maxInlineDiagramViews = 6`（与文件预览各自独立）。
- 只为可见附近的卡片创建视图：卡片实绘矩形与视口（向上下各扩 1 个视口高度）相交才 `ensure`；不再被构建或离视口超过 2 个视口高度时释放视图，保留尺寸缓存。达到上限时释放 `lastFrame` 最旧且不可见的视图。
- 每帧沿用 `dropStaleChatFilePreviews` 的帧计数思路回收未构建的卡片。

## 3. 分工与接口约定（独立副本，禁止交叉写）

两席均禁止 commit/push、操作桌面、读取凭据、继续委派、修改另一席文件；必须实际写入文件。

### A：GLM 5.3-Flash（`glm-5.3-flash`，关闭思考）—— 渲染基础设施

拥有：新 `diagram_scheme.go`、`diagram_webview.go`、`diagram_webview_test.go`，新目录 `assets/diagram/`（`card.html`、`card.mjs`、`card.css`、`card_test.mjs`），`main.go`（**仅**协议注册与 `appState` 新字段），`THIRD_PARTY_NOTICES.md`（补“宿主内置分发 Pele 1.7.0 及其 LICENSE”说明）。

提供给 B 的 Go 契约（名称固定）：

```go
type inlineDiagramTheme struct {
    Background, Text, Muted, Surface, SurfaceAlt, Border, Accent, Danger string // "#rrggbb"
    Chart        [5]string
    FontFamily   string
    FontSize     float32
    HighContrast bool
}
type inlineDiagramStatus uint8
const (
    inlineDiagramIdle inlineDiagramStatus = iota // 尚未建视图（或已释放）
    inlineDiagramLoading
    inlineDiagramReady
    inlineDiagramFailed
    inlineDiagramTooLong
)
type inlineDiagram struct {
    Key, Source        string
    Status             inlineDiagramStatus
    WebView            *mygo.WebView
    NaturalW, NaturalH float32 // 成功后有效；释放视图后仍保留作缓存
    Agentflow          bool
    Err                string
    ErrLine            int
    // 内部：lastFrame、themeRev、scale 等由 A 管理
}
func (s *appState) inlineDiagramFor(source string) *inlineDiagram          // 按 sha256(source) 取/建状态；过长直接 TooLong
func (s *appState) ensureInlineDiagram(d *inlineDiagram, theme inlineDiagramTheme, frame int) // 未达上限则建视图、加载、异步渲染；完成后 Invalidate
func (s *appState) setInlineDiagramScale(d *inlineDiagram, scale float32)
func (s *appState) setInlineDiagramTheme(theme inlineDiagramTheme)        // 下发给所有存活视图
func (s *appState) releaseInlineDiagram(d *inlineDiagram)
func (s *appState) dropStaleInlineDiagrams(frame int)                     // 释放本帧未 ensure/未标记可见的视图
const maxInlineDiagramViews = 6
```

页面契约（`window.byopDiagram`）：`render(source, theme) → {ok:true,width,height,agentflow} | {ok:false,error,line}`；`setTheme(theme)`；`setScale(scale)`。渲染在 try/catch 中，宿主 `EvalContext` 超时 5s 记为失败。A 负责测试：协议只服务白名单文件且带 CSP；Eval 结果解析；超时/失败状态；上限与 LRU 释放；`card_test.mjs` 覆盖 SVG 清理、主题变量、scale/尺寸计算。`node --test assets/diagram/` 与插件 17 项测试都要通过。

### B：Qwen 3.8 Flash Next —— UI

拥有：`chat_view.go`（代码块钩子与回收调用）、新 `diagram_card.go`、`diagram_card_test.go`、`mermaid_preview.go`（移除按钮、保留“在页面打开”的可用性与动作）、`p29_view.go`、`p29_turn.go`（如需）、新 `p29_tones.go`、`p29_contrast_test.go`、`p29_view_test.go`、`p29_review_test.go`（按新色调/卡片更新断言，不放宽）、`hud.go`（**仅显示颜色**）、`jagent_chat.go`（如需）、locales（en/zh-Hans/catalog_gen.go，`go run github.com/egoist/mygo/cmd/mygo@v0.5.5 generate .`）、`mygo.json`（0.23.0）、P28 相关测试（按新行为更新）。

B 负责：1.1–1.3 全部；2.1–2.5 的卡片 UI、可见性判定（纯函数，可测）、scale 计算与下发、主题映射 `inlineDiagramThemeFor(p palette)`、工具栏与状态；在 A 契约到位前可用临时 stub 编译，交付时不得留 stub。

测试要求（有意义，不只测常量）：对比度矩阵（1.3）；J Agent 每轮在玻璃卡内、BYOP 无卡；闭合前显示源码、闭合后出现卡片、流式不重复 ensure；成功/失败/过长/等待四态的可见文本；适应宽度与缩放的 scale 和主体高度；可见性判定；“在 Mermaid 页面打开”仅在插件可用时出现；复制写入剪贴板；P29 既有行为（顺序、折叠、滚动锚定、t/s 胶囊绑定）回归通过。

## 4. 协调方验收

1. 合并两席文件；`gofmt`、`go vet`、`go test -count=1 .`（开发构建句柄检查开启）、`mygo i18n check`、`node --test assets/diagram/`、`node --test plugins/mermaid-diagram/test/`、`git diff --check`。
2. Cursor Claude Opus 5.5 @1m 独立审核（额度不足才转 Copilot），P0–P2 修复后复审放行。
3. 原生验收（隔离配置与 Pi 会话，只操作本轮实例）：
   - J Agent 雾山壁纸复现用户截图场景，提问/回答/过程/时间线清晰可辨；BYOP 浅/深时间线可见。
   - 真实 Pi 生成 Mermaid：流式中为源码，闭合后就地变图；t/s 胶囊仍实时更新。
   - 适应宽度/缩放/查看源码/复制；主题切换后图跟随；错误源码显示错误+源码；历史会话就地渲染；多图滚动不卡顿、视图回收；窄窗口；中英。
   - 插件已装时“在 Mermaid 页面打开”可用。
4. `docs/P30-validation.md` 记录证据与限制；打包 0.23.0 DMG（未公证）并在打包版复验关键路径；更新 README、两处 AGENTS、`diagram-rendering.md` 实施状态；commit/push 并回读远端。

## 5. 不做

独立平移覆盖层（由“在 Mermaid 页面打开”承担）、图解导出/保存、原生 ParseSVG 路线、插件修改、新设置项、HUD 开关、其他冻结插件。

## 6. 修复轮（2026-11-07 追加，Cursor 审核 8a80a4f5 + 协调方走查）

基线：P30 首次合并后，再 rebase 到 0.22.1（P29.1 输入框对齐，见 `~/AgentScratch/20261107-byop-p29.1/spec.md`）。修复只针对下列条目，不扩范围。

### 6.1 B（Qwen）

1. **P1 可见性**：可见性不得依赖只在 Draw 中写入的 `placed/cardY`（视口外的卡片不绘制，导致多于 6 张历史图时视图每帧建销、永不出图）。用卡片元素的布局矩形（例如 `Bounds()` 换算到对话视口坐标）判定；未布局过的卡片视为“近”，但同一帧新建视图总数受 `maxInlineDiagramViews` 约束，不得出现建了又在同帧释放。
2. **P2 远卡释放**：离视口超过 2 个视口高度的卡片释放视图、保留尺寸缓存（2.7 原文）。
3. **P2 阅读位置**：读者上方的卡片完成渲染、主体高度从占位 160 变为实际高度时，滚动位置补偿高度差，阅读点不跳。跟随最新（at latest）时不补偿。
4. **P2 失败文字对比度**：`dangerText = p29AccessibleInk(p, p.dangerText, 4.5)`，进入对比度矩阵。
5. **正文右溢出 46px**：`p29_view.go` 正文列 `FillWidth()+Margin(左46)` 改为左内边距（`p29BodyIndent`），两套外观都加 `ui.Tester` 断言：正文、chevron、图解卡片右缘 ≤ 所在卡片/列右缘。
6. **P3 标记遮线圆**：时间线节点遮线不得在 J Agent 玻璃卡上显出黑点（用 rail 断开代替实心遮挡圆）。
7. **P3 同源两卡**：两张源码相同的卡片各自拥有视图与状态（key 含块序号或消息序号），第二张不得空白。
8. **D 图解压住输入区**（协调方 BYOP 浅色截图 `evidence/live/byop-light.png`：图解节点露在浮动 Composer 下方、压住 HUD 文字）：系统 WebView 不参与 MyGo 模糊层。卡片的 WebView 区域只在其矩形与“对话视口减去底部浮动 Composer 区域（含 HUD）”相交部分显示：完全落入 Composer 区域时不构建 `ui.WebView`（保留同尺寸占位，状态与视图不释放）；部分重叠时用一层裁剪容器把 WebView 裁到 Composer 上沿。Composer 区域高度取浮动 Composer 实际高度（Onevra `FloatingConversationState` 不公开，可在 `chatComposer` 根元素 `Draw` 中记录到 `chatView` 状态，用于下一帧）。两套外观都要覆盖；加测试：卡片底部进入 Composer 区域时，WebView 元素的可见下缘 ≤ Composer 上沿。
9. **6 个失败测试**：在 `p30/partial-at-stop/ui/` 半成品里有 6 个既有卡片测试失败（失败/就绪文案、宽度 673 vs 720、高度 280 vs 300、新卡状态）。逐个判断：若由 5（Padding 改变内容宽度，720 卡片上限不变但可用宽度少 46）导致，按新几何更新预期并写明计算；若由 1 的新可见性逻辑导致文案/状态错误，属于回归，修代码不改测试。交付说明逐条列出结论。
10. 删除临时 `zz_p30_review_test.go`，其中复现改写成正式断言放入 `diagram_card_test.go` / `p29_view_test.go`。

### 6.2 A（GLM）

1. **P3 导航**：首次加载完成后，`OnWillNavigate` 对所有目标 `PreventDefault`（含相对链接、同源 `byop-diagram://`），http/https 仍交系统浏览器。
2. **P3 seq**：`OnDidFinishLoad` / `OnDidFailLoad` 回调捕获创建时的 `renderSeq`/视图代次，在 `postUI` 中比较，旧视图的回调丢弃。
3. **P3 SVG 清理**：`sanitizeSvg` 增补：`<iframe>`、`<embed>`、`<object>`、`<use>` 的外部 `href`、`xlink:href`/`href` 的 `javascript:` / `data:text/html`（大小写与空白变体）、`<animate>`/`<set>` 修改 `href` 的攻击、`style` 中的 `url(javascript:`。`card_test.mjs` 增加对应注入回归。
4. 不改 `plugins/mermaid-diagram/**` 与 `vendor/pele.mjs`。

### 6.3 不变

1.1–2.7 的视觉与行为、t/s 胶囊、P29 几何与动效、“在 Mermaid 页面打开”。
