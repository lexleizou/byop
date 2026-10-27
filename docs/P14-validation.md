# P14 深色材质层级验证

日期：2026-10-27。版本：0.14.0。基线：6b866b2（0.13.2）。

本阶段只实施用户授权的 annotation 1：统一深色窗口背景、导航玻璃、不透明内容面板和抬升浮层，保留现有黑白 Pi Logo。没有新增功能、导航结构、设置项或模型选择器改动。

## 已实施范围

| 项目 | 实现 |
|---|---|
| 产品 token | 新增 `readingSurface`、`raisedSurface`、`composerSurface`、`popoverTint`、`popoverBorder` 语义角色；根设计与仓库发布副本保持一致 |
| 根背景 | 深色阅读区固定为 `#111110`，壁纸仍是导航玻璃后方的共同大气背景 |
| 导航玻璃 | 保留现有透明度、模糊和折射参数；tint 由产品 surface 计算，并增加 0.5px 光学边缘渐变，不替代 MyGo 玻璃材质 |
| 内容层级 | Composer 使用 `#191918`；设置、插件、自动化卡片、任务栏与内容卡片使用 `#222221` 抬升表面 |
| 浮层 | 深色浮层改为约 72% 抬升表面 tint，保留 MyGo 玻璃、增加边缘与更深阴影；高对比和减少透明度继续使用实色 |
| 设置预览 | 与真实外壳共用 `wallpaperBackdrop` 和 `navGlassCard`，避免壁纸与玻璃响应分叉 |
| 浅色主题 | 既有浅色表面、浮层与任务栏边框保持原值 |
| Logo | 未修改 `assets/byop-logo.svg`、`assets/byop-logo-mark.png`、`assets/byop-logo-1024.png` |

## 开发检查

| 检查 | 实际结果 |
|---|---|
| `gofmt -l .` | 无输出 |
| `go vet ./...` | 通过 |
| `go build -o byop .` | 通过 |
| `go test ./... -count=1` | 447 项通过，3 个包 |
| `git diff --check` | 通过 |
| 产品 token | 根设计与仓库发布副本逐字一致，`jq empty` 通过 |
| Logo | 三份现有 Logo 文件哈希与修改前一致 |

## 原生窗口走查

协调方用相同源码的隔离副本启动 MyGo 原生窗口，通过 CapturePage 保存截图并逐张人工审阅。驱动仅位于 AgentScratch，未加入应用源码；会话为示例数据，未连接真实 Pi。Plugins/Automations 使用仓库已有 examples 契约 fixture，只展示、不执行。

| 截图 | 已检查结果 |
|---|---|
| `screenshots/p14-chat.png` | 正文实色、Composer 和代码卡形成层级，保留新 Logo |
| `screenshots/p14-model-popover.png` | 浮层玻璃、边缘和阴影可见，文字清楚，位置正常 |
| `screenshots/p14-appearance.png` | 设置使用一致抬升卡片，预览与外壳共享壁纸/导航逻辑 |
| `screenshots/p14-nav10.png`、`p14-nav90.png` | 同壁纸下导航透出量明显变化，阅读区保持实色；55% 见 chat |
| `screenshots/p14-jade.png`、`p14-none.png` | 壁纸颜色同步到导航；无壁纸时保留深色层次 |
| `screenshots/p14-plugins.png`、`p14-automations.png` | 页面卡片使用统一层级，操作文字可读 |
| `screenshots/p14-tasks.png`、`p14-narrow.png` | 任务栏沿用抬升表面；760x560 窄窗使用既有右侧覆盖层 |
| `screenshots/p14-contrast.png`、`p14-reduce-transparency.png` | 高对比及模拟减少透明度回退实色，不显示壁纸 |
| `screenshots/p14-light.png` | 浅色外观可读，未带入深色材质 |

截图通过状态注入切换页面/透明度/主题，并非真实鼠标拖动证据；减少透明度为应用内模拟，未改系统设置。与修改前 Chat、Appearance 和模型浮层截图进行人工对照。第一轮截图副本与当时生产源码一致（main.go 仅加隔离驱动并跳过旧设置发现）。审核修正两处浅色路径后重新采集普通浅色、浅色高对比及浅色插件详情：与原 0.13.2 相同 fixture 的三张截图逐像素完全一致。最终浅色截图为修正后结果；深色路径未因修正改变。

新增截图 `p14-light-contrast.png`、`p14-light-plugin-details.png`。透明度 10% 与 90% 的同坐标阅读区 RGB 差值均为 0，导航区有明显变化；原始度量保存在阶段过程目录。

## 独立审核

指定开发路由 `auto/deepseek-v4.1-flash`、审核路由 `auto/claude-opus-5-5`；协调方核对两代理会话所有 turn_context.model 与指定路由一致。审核报告的自报 GPT-5 身份与运行元数据冲突，已在原报告附注说明，以实际调用记录为准。

审核发现两项 P2：浅色主舞台/渐隐背景色变更、浅色插件详情意外采用玻璃。开发修正后复审 PASS，无剩余 P0-P2。复审另跑了格式、vet、构建和相关定向测试；完整 447 项测试由开发执行。

P3 保留：深色 Appearance 外层卡片增加内边距，切换主题时内容位置略变；减少透明度下普通浮层与插件详情的实色底略有差异。

## 安装验证

已以 MyGo 构建并安装 `/Applications/BYOP.app` 0.14.0（本地签名，跳过公证）。安装与 dist 可执行文件 SHA-256 一致：`c349f4f8e45c829926250c4454b6fab8c85d7511a0e5921c9eebd9f7243715e8`；codesign 校验通过。旧版可恢复备份：`/Users/lexleizou/.Trash/BYOP-pre-P14-20261028-003329.app`。

三个 Logo 资源与 0.13.2 基线哈希一致。正式安装版最后的 GUI 启动/交互检查因 Mac 锁屏未完成；没有据此声称正式应用点击或拖动通过。原生材质截图来自隔离的同源 MyGo 构建。

## 已知边界

- MyGo 玻璃是着色器材质，不是 Apple `NSGlassEffectView`；没有 text-shadow。
- 当前阶段不验证 Windows 或 Linux。
- 已安装，提交与推送状态见本阶段交付记录。
