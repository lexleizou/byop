# BYOP P0 基线盘点

盘点日期：2026-10-20
范围：只读核对旧 Pi Agent Desktop、Pi CLI、Pi 数据边界和 MyGo 固定基线。未修改旧应用、Pi 配置、会话或凭据。

## 结论

1. 当前安装版是 **Pi Agent 0.7.4**，而用于静态阅读的参考包是 **@agegr/pi-web 0.10.0**。迁移规格不能把 0.10.0 源码与 0.7.4 安装包视为同一版本。
2. Pi CLI 1.3.0 提供长期运行的 `--mode rpc` JSONL 协议，包含 prompt、steer、follow-up、abort、队列、会话、模型、思考级别、统计、Bash 和扩展 UI 等接口，足以支撑 P2 的外部进程适配。
3. Pi 仍应拥有会话、模型配置、凭据、扩展和 Skills；BYOP 应拥有原生外壳、工作台偏好、插件元数据、任务栏和迁移记录。
4. MyGo 已具备原生窗口、主题、SVG 图标、最小窗口尺寸、窗口状态恢复、WebView 嵌入等所需 API。P1 将实测窗口启动、主题切换、导航、任务栏和页面路由；IME、拖放、通知和 WebView 的跨平台运行仍需在对应阶段实测。

## 验证基线

| 项目 | 已核实结果 |
|---|---|
| macOS | 26.7.1，build 25G241 |
| 架构 | arm64 |
| Go | 1.27.2 darwin/arm64 |
| Node.js | v26.10.0 |
| Pi CLI | 通过 `PATH` 解析，1.3.0 |
| Pi 包声明的 Node 最低版本 | `>=22.19.0` |
| 参考旧桌面包 | `@agegr/pi-web` 0.10.0 |
| 实际安装旧桌面 | Pi Agent 0.7.4 |

## 旧 Pi Agent Desktop

| 项目 | 已核实结果 |
|---|---|
| Bundle ID | `com.abcwyc.pi-agent` |
| 签名信息 | arm64 app bundle；未显示 TeamIdentifier |
| 参考包桌面容器 | Tauri 2、Rust、系统 WebView |
| 参考包主界面 | Next.js 16.3.8、React 19.2.4、TypeScript |
| 参考包 Agent 依赖 | `@earendil-works/pi-agent-core`、`pi-ai`、`pi-coding-agent`、`pi-tui`，均约 1.3.0 |
| 参考包附加运行依赖 | `node-pty`、xterm、Mermaid、KaTeX、React Markdown、croner、Tauri 插件 |

参考包与安装版版本不一致，后续逐项迁移必须分别标注“0.7.4 运行时”与“0.10.0 源码参考”。

## 旧应用用户数据

只读盘点确认旧应用保存窗口状态、应用偏好和版本信息，缓存位于系统应用缓存目录。公开记录不保留具体窗口坐标、会话数量、文件大小或用户目录路径。

旧 WebView 的 `localStorage` 偏好语义已从 0.10.0 源码核实，包括主题、语言、声音、侧栏宽度、右栏宽度、未读/归档项目、草稿、Diff 模式和工作区恢复。安装版 0.7.4 的实际键集合仍需在 P8 迁移实现时通过独立只读读取器确认，不能把 0.10.0 键集合直接当作已存在数据。

## Pi 数据目录

Pi 的用户目录包含会话 JSONL、模型与 provider 配置、认证信息、扩展、Skills、包和定时任务状态。公开记录只保留结构结论，不发布会话数量、正文、凭据、认证 token、日志原文或本地路径。

迁移边界是：Pi 继续拥有并写入这些数据；BYOP 只通过 RPC 操作当前会话，并以只读方式建立可重建的会话索引。不能把会话 JSONL 当作可由 BYOP 直接修改的工作台数据库。

## Pi CLI 1.3.0 能力

| 能力 | 状态 | 接口或边界 |
|---|---|---|
| 长期 RPC | 支持 | `pi --mode rpc --no-session` |
| 帧格式 | 支持 | stdin/stdout JSONL；响应按 `id` 关联 |
| 输入 | 支持 | `prompt`、`steer`、`follow_up` |
| 停止 | 支持 | `clear_queue` 后 `abort` |
| 会话 | 支持 | new、switch、fork、clone、entries/tree、session name |
| 模型与思考级别 | 支持 | get/set/cycle models 与 thinking levels |
| 流式事件 | 支持 | message、tool、bash、compaction、retry、agent end/settled |
| 快照与统计 | 支持 | messages、state、session stats、last assistant text |
| 扩展 UI | 支持子协议 | RPC 可用；部分 TUI-only 展示能力不可用 |
| 凭据 | 保留在 Pi | BYOP 不复制 `auth.json` 或输出 token |
| 会话正文写入 | 保留在 Pi | BYOP 不直接编辑 JSONL |

成功响应只表示命令被接受；真实完成要等待 `agent_settled`。`agent_end` 之后仍可能有重试、压缩、steering 或 follow-up，因此 P2 的完成态不能只监听 `agent_end`。

## MyGo 固定基线

参考仓：<https://github.com/egoist/mygo>
Tag：`v0.5.5`
Commit：`4d8673cd2658a2c6e86b26c44751e57bf8174f91`

| 平台能力 | 证据 | 当前结论 |
|---|---|---|
| 原生窗口 | `WindowOptions`、多个 native examples | 源码已核实，P1 实测启动 |
| 最小/最大尺寸 | `WindowOptions.MinWidth/MinHeight` | 源码已核实 |
| 窗口状态恢复 | `WindowOptions.StateKey` | 源码已核实 |
| 自定义主题 | `ui.Theme`、`Context.SetTheme` | 源码已核实，P1 实测 |
| system/light/dark | `mygo.Theme.SetSource` | 源码已核实，P1 实测 |
| SVG 图标 | `ui.ParseSVG`、`ui.Icon` | 源码已核实，P1 实测 |
| WebView | `Window.NewWebView`、`ui.WebView` | 源码及示例已核实；P5 实测 |
| IME / 剪贴板 / 拖放 / 通知 | 示例及源码存在 | P0 未做端到端运行验证 |

P0 不把“示例存在”写成“所有平台已完成验收”。P1 只承诺 macOS 原生外壳的窗口、导航、主题和基础输入验证。

## 迁移归属

| 对象 | 继续由 Pi 拥有 | 迁移到 BYOP |
|---|---|---|
| 会话 JSONL | 是 | 只建立索引/引用，不重写 |
| 模型和 provider 配置 | 是 | 通过 CLI/RPC 读取能力菜单 |
| 凭据与登录 | 是 | 只展示状态，不复制 |
| 扩展、Skills、prompts、themes | 是 | 发现结果和插件/资源关联 |
| 工作台页面、导航、任务栏 | 否 | 是 |
| 插件清单、贡献点和视图状态 | 否 | 是 |
| 旧窗口、主题、侧栏、草稿偏好 | 否 | 预览后导入 |
| 文件索引、Library/Document 引用 | 部分由工具读取 | BYOP 建立可重建索引 |

## P2 边界

- P2 已接入外部 Pi CLI，支持连接、发送、流式事件、完成和停止。
- P2 不安装、不调用工作台插件，不承载真实 WebView。
- Chat 只显示当前临时运行的投影；Plugins、Library、Document 和 Automations 仍是外壳示例。
- P4 前需要确定插件清单 schema、JSON-RPC 方法集合和版本兼容策略。
- P5 前需要验证 `ui.WebView` 在目标 macOS 版本中的分层、焦点、主题和崩溃恢复。
- P8 前需要在安装版 0.7.4 的隔离副本上完成实际偏好键与迁移预览。
