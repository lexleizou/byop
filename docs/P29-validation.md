# P29 验证记录

版本：BYOP 0.22.0。基线：`db1a608`（0.21.0）。设计见 [P29-design.md](P29-design.md)。

## 范围与证据口径

按 `P29-design.md` 复现 Proma 商业版 0.21.45 的对话过程结构、消息头、思考/工具时间轴、详情、最终回答、分层动效和滚动行为，额外保留原实时 t/s 胶囊。继续使用 Go/MyGo 原生 UI，没有复制商业 bundle、代码或品牌资产，也没有引入对话 WebView。

- 商业版参数来自指定版本的静态实现核对，不等于对商业版做了新的运行时逐像素测量。本阶段所有截图都来自 BYOP 自身。
- 本阶段的局部视觉例外：对话过程区域使用商业版 15/16px 字号、1px 时间轴和 150/320/420/500ms 分层动效，覆盖旧的 12/14/18px 与统一 250ms 约定；其余区域不变。这是用户 2026-11-06 明确指定“完全按商业版”后的结果。
- 人工 fixture、真实 Pi 请求、测试替身分开记录。人工成功 fixture 后来被用户试用，追加了一次真实 hello 对话，因此不再把该会话称为纯人工数据；失败/停止 fixture 仍为明确标注的合成样例。
- 隔离验证目录：`~/AgentScratch/20261106-byop-p29/`。使用独立配置、插件副本、工作区和 Pi 会话；认证沿用已授权 Pi 环境的链接，没有复制或输出凭据。

## 开发与审核

- 开发：GLM 5.3-Flash 负责数据/协议/历史（`main.go` 数据与事件部分、`pi_rpc.go`、`pi_sessions.go`、`p29_data.go`、`p29_data_test.go`）；Qwen 3.8 Flash Next 负责原生 UI（`chat_view.go`、`p29_turn.go`、`p29_view.go`、`p29_motion.go`、`p29_view_test.go`、`p29_review_test.go`、locales、`mygo.json`）。两席使用独立副本，由协调方按文件合并。GLM 在 P27/P28 的 HTTP 400 故障本阶段未再出现。
- 审核：Cursor Claude Opus 5.5 @1m 独立审核四轮。
  1. 数据层：两个 P1（tool-only 第二源消息覆盖前文；phase 实际在 `textSignature` 中未被读取）、一个 P2（同一消息内交错索引被拆成重复块），全部修复。
  2. 完整 UI：两个 P1（手动折叠与收尾补偿移动阅读点；进入动画实际为延迟后突现）、三个 P2（失败原因藏在折叠内；只有思考的轮次实时与历史判定不一致；最新历史轮被标成当前模型），全部修复。
  3. 复审：上轮全部修好，新增一个 P1（Draw 闭包使用过期 Element，开发构建可崩溃）与两个 P2（原生 Transition 默认对尺寸做动画，流式持续滞后；J Agent 收尾时过程仍展开却补偿），全部修复。
  4. 最终 gate：**允许提交**，无 P0–P2。
- 审核发现的每个问题都已转为 `p29_data_test.go` 与 `p29_review_test.go` 中的断言，覆盖 BYOP 与 J Agent 两套预设。

## 自动检查

- `gofmt -l .` 无输出；`go vet .` 通过；`go test -count=1 .` 全部通过。
- `go test -count=1 -run 'TestP29|TestR3' .` 在开发构建（句柄检查开启）下通过；审核方另用 `-tags mygo_noinspector`（正式构建配置）跑通。
- `go run github.com/egoist/mygo/cmd/mygo@v0.5.5 i18n check`：982 条消息，中英通过。
- `node --test plugins/mermaid-diagram/test/`：17 项通过。
- `git diff --check` 通过。
- 有序数据覆盖：真实 Pi 合法的 `thinking_start(0) → text_start(1) → thinking_end(0)` 交错更新、显式 assistant `message_start` 边界、tool-only 源消息不丢前文、最终快照原始 contentIndex、幂等合并、工具结果保留、TextSignature V1 的 phase、synthetic 与真实文本区分、历史模型身份与用户提交时间。

## 原生验收

使用 Cua 只操作本阶段启动的隔离实例。真实 Pi 为 Pi 1.5.0 与指定 Qwen 模型，工具只用 read 读取合成的 `p29-sample.txt` 和 bash 执行 `printf P29_OK`。模型自己写的“验收通过”不作为验收结论。

| 路径 | 证据 | 结果 |
|---|---|---|
| 实时 t/s 胶囊（打包版） | 打包版 0.22.0 真实请求连续帧：生成中（停止按钮可见）胶囊依次显示 41.3、85.5、119.1、139.8、167.4 t/s，页脚平均同时为 49.8 t/s。截图 [p29-live-tps-timeline.png](screenshots/p29-live-tps-timeline.png) | 通过 |
| 实时 t/s（集成候选） | 另一次真实请求生成中 82.8 → 133.7 → 181.3 → 204.1 t/s | 通过 |
| 时间轴与最终回答 | 思考、工具、中间说明按顺序排在时间轴内，统计为“Thought 3 times · Ran 2 tools”，最终回答独立于过程之下。[p29-final-answer.png](screenshots/p29-final-answer.png) | 通过 |
| 真实历史恢复 | 基线 0.21.0 生成的真实 Pi 会话恢复为 3 段思考、2 个完成工具、3 条带模型身份的助手源消息，可展开完整思考与工具入口 | 通过 |
| 失败与停止 | 合成失败 fixture：Unfinished output、Stopped、Tool read failed，详情展开 ENOENT 原因，虚线思考卡片可见。英文单复数已修为“Thought 1 time · Ran 1 tool”。[p29-failed-details-light-en.png](screenshots/p29-failed-details-light-en.png) | 通过 |
| BYOP 浅色手动展开/收起 | R3 构建、标准动效：点击统计行展开与再次收起，用户消息、模型头、统计行在前后帧中位置不变 | 通过 |
| J Agent 阅读位置 | 下一轮仍在生成时上滚到上一轮，直到生成结束同一段文字位置保持；“返回最新”恢复跟随。[p29-scroll-hold-jagent.png](screenshots/p29-scroll-hold-jagent.png) | 通过 |
| 窄窗口 | 900×760：导航收纳，模型头、时间轴、详情、停止状态与 Composer 可见。[p29-narrow.png](screenshots/p29-narrow.png) | 通过 |
| 深浅色、中英文、减少动态 | J Agent 深色中文、BYOP 浅色英文均实测；通过应用设置在“减少”与“标准”间切换后展开/收起可操作 | 通过 |
| P28 Mermaid 回归 | 打包版新代码块 Preview diagram 打开独立插件、自动导入源码并渲染（`flowchart · 194×328 px`）。[p29-mermaid-preview.png](screenshots/p29-mermaid-preview.png) | 通过 |

## 安装包

- `dist/darwin-arm64/BYOP 0.22.0 arm64.dmg`，7,823,694 字节，SHA-256 `b53830cf5a5b9e48648e5788d248020db73096ca61ce3a4b870b9258388b2fdd`。
- 命令：`go run github.com/egoist/mygo/cmd/mygo@v0.5.5 build -skip-notarize`；已签名、**未公证**。`Info.plist` 版本 0.22.0。
- 上表“打包版”各行在该 App 内实测。Mermaid 插件仍为 0.2.0，本阶段未修改插件或 Pele vendor。

## 已知限制

- 商业版完成时“父容器测量前后高度”的协调动画在原生端以同一容器高度动画实现（150ms 延迟 + 500ms cubic），并非同一套代码；视觉目标等效，不称为逐帧一致。
- 动画时长由 `ui.Tester` 布局采样验证；原生截图只证明终态和位置稳定，没有测量真实 GPU 帧率。
- 最新一轮结束后，t/s 胶囊按 0.21.0 原逻辑在无新样本 30 秒后才隐藏；这是既有行为，不是永久保留的历史速率，历史轮不显示。
- tool-only `message_end` 现在保留其 output usage，整轮输出 token 会包含此前遗漏的纯工具调用消息；t/s 采样、更新与失效算法未改。
- 审核保留的 P3：`piError` 是会话级状态，当最新一轮为失败时，同一会话的协议警告等无关错误也可能显示为该轮原因；触发需同时出现两类错误，未修改。
- macOS 其他 Space 上的 WKWebView 首次打开可能不绘制，需持续前置窗口；这是验收工具侧的观察条件，不是 BYOP 的渲染失败。
- P28 的系统 ZIP 文件选择框自动化限制仍保留，不属于 P29；本阶段未修改系统权限。
- 本记录中的 `evidence/` 原始截图保留在上述 scratch 目录，仓库只提交 `docs/screenshots/p29-*.png` 六张。
