# P30 验证记录

版本：BYOP 0.23.0。基线：`fa26b1b`（0.22.1）。设计见 [P30-design.md](P30-design.md)。

## 范围

用户 2026-11-06 21:33 在 J Agent 雾山壁纸截图下提出三点：①该背景上对话内容很难识别；②当前时间线很难看清；③Pele 渲染改为对话内直接渲染。本阶段：J Agent 每轮加玻璃卡并提高文字与竖线不透明度、统一色调并按 WCAG 校验、Mermaid 代码块闭合后在原位置渲染为图解卡片（宿主内置 `byop-diagram://` 协议 + go:embed，复用插件 vendor 的 Pele，不复制也不修改插件）。

修复轮（2026-11-07）处理首轮审核不放行的全部条目，另加走查发现的 D：图解是系统 WebView，不受浮动输入区的模糊层影响，会画出节点文字压住状态行。

## 开发与审核

- 两席独立副本（GLM 渲染基础设施 / Qwen 对话区 UI），协调方按文件合并；修复轮由协调方在合并结果上完成并保留两席原有的分层。
- 审核：Cursor Claude Opus 5.5 @1m。首轮不允许提交（P1 可见性依赖绘制期状态、P2 远卡不释放/阅读点跳 320px/失败文字 3.85:1、P3 若干）。修复轮逐条修复后复审，见文末结论。

## 修复轮条目对照

| 条目 | 修复 | 覆盖 |
|---|---|---|
| P1 可见性 | `card.Bounds()` 布局矩形减去对话内容原点（`p29-transcript-origin`）得到视口内位置，不再读绘制期写入的字段 | `p30_review_test.go` 的历史多图抖动测试（10 张图、空闲帧不再建-销视图） |
| P2 远卡释放 | 同一位置规则，超出两个视口高度释放视图、保留尺寸缓存 | `TestP30FarCardReleased` |
| P2 阅读点 | 卡体高度变化时，仅当卡片完全位于视口上方且未跟随最新时补偿滚动 | `TestP30ReadyHeightJumpsReader` |
| P2 失败文字对比度 | `p29Tones.dangerText = p29AccessibleInk(p, p.dangerText, 4.5)` | `p29_contrast_test.go` 矩阵 |
| 正文右溢出 46px | 正文列由 `Margin` 改 `Padding(0,0,0,p29BodyIndent)`，卡片宽度随之变为 720−46 | 卡片宽度/位置断言 |
| 标记遮线 | 竖线按标记矩形断开（`railCuts`/`p29RailSegments`），不再画实心遮挡圆 | 时间线几何断言 |
| 同源两卡 | 每帧 `diagramClaims` 计数，第二张显示源码 | `TestR30DuplicateDiagram` |
| D 图解压住输入区 | 记录浮动 Composer 卡片顶边，卡体侵入该带时不画 WebView（保留占位与状态） | `TestP30ComposerOcclusionHidesThePage` |
| 导航/回调代号 | 首载后拦全部导航（http(s) 交系统浏览器）；视图代号 `gen` 捕获后在 `postUI` 内比较，旧页回调丢弃 | 代码审阅 |
| SVG 清理 | 增补 `iframe`/`embed`/`object`、`data:` URL、`style` 内 `url(javascript:|data:)` | `node --test assets/diagram/` 12 项 |

## 一处语义变更

`releaseInlineDiagram` 原先把状态重置为空闲，导致“已渲染/已失败”的结果在视图回收后被忘掉：卡片回到视口会重画成“正在渲染”，失败原因消失。改为保留 Ready/Failed（只有渲染中的回到空闲）；`ensure` 对“已渲染但没有视图”的卡片重建视图并保持已渲染状态，页面加载后重绘，卡片不会空白也不会谎称在渲染。`TestApplyInlineDiagramRenderSuccessAndCache` 的断言随之更新。

## 自动检查

- `gofmt -l .` 无输出；`go vet .` 通过；`go test -count=1 .` 通过。
- `i18n check`：995 条，中英通过。
- `node --test assets/diagram/`：12 项通过；`node --test plugins/mermaid-diagram/test/`：17 项通过。
- `git diff --check` 通过。

## 原生验收

隔离目录 `~/AgentScratch/20261107-byop-p30b/`：独立配置（BYOP / J Agent 各一份）、独立 Pi 会话目录；插件、工作区、pi-agent 复用既有隔离目录；未复制或输出凭据。用 Cua 只操作本阶段启动的实例，真实 Pi 1.5.0 + Qwen 3.8 Flash Next。

| 路径 | 证据 | 结果 |
|---|---|---|
| J Agent 雾山壁纸：可读性与时间线（用户场景） | 一轮真实对话：玻璃卡内提问与回答清晰可辨；时间线竖线与节点在壁纸上可读 | 通过 [p30-jagent-inline-diagram.png](screenshots/p30-jagent-inline-diagram.png) |
| Pele 对话内渲染 | 同一轮回答中的四个节点流程图在回答位置就地渲染成图解卡（思考 5 次、执行 4 次工具、耗时 1 分 8 秒） | 通过（同上截图） |
| 生成中的实时反馈 | 生成中状态行显示“Pi 正在工作”与实时 t/s（约 34.7 t/s），完成后 t/s 胶囊保留 | 通过 |
| 建议文字对齐（0.22.1 回归） | 回答结束后建议“那用 read 读取 p29-sample.txt…”与光标同行 | 通过 |

## 打包

- `go run github.com/egoist/mygo/cmd/mygo@v0.5.5 build -skip-notarize`（未公证）。
- `dist/darwin-arm64/BYOP 0.23.0 arm64.dmg`：8,050,803 字节，SHA-256 `f562f3691ef9f92082036b477609f49b0cd10bd96dc1cfc3edd98810f6d1f969`。

## 已知限制

- 图解卡片在系统 WebView 中渲染，使用系统字体，不继承应用内注册的字体。
- 图解卡片圆角为 8，与 BoardUI 预设的代码卡圆角不同（P31 再统一）。
- 相同源码的两张卡片共享渲染状态：同一来源只给一张卡 WebView，另一张显示源码；两张卡的“查看源码”、缩放开关与测量高度也共用同一份状态（有意的 MVP 收缩，改动一张会影响另一张）。
- 窄窗与浅色/深色切换、缩放与查看源码由自动测试覆盖，本轮未逐项实机截图。
