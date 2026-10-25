# P13 深色液态玻璃主题验证

日期：2026-10-26。应用 0.13.0，实施基线 2410dc2（0.12.0）。

DeepSeek V4.1 Flash 开发，Claude Opus 5.5 审核，协调 Agent 编写阶段规格、执行原型对照走查、最终检查与安装。目标是把 HTML 高保真原型 `byop-liquid-glass-dark-v0.1` 的深色液态玻璃主题 1:1 复原到 MyGo 原生界面，并替换新版 Logo。

## 范围

| 项目 | 结果 |
|---|---|
| 深色壁纸 | Ember、Graphite、Jade 三张壁纸，另有“无壁纸”；浅色主题保持 P12 不变 |
| 导航玻璃 | 导航透明度 10–90% 可调，模糊与边缘折射可开关 |
| 玻璃浮层 | 模型与菜单浮层按原型位置、节奏和圆角对齐 |
| 回退 | 高对比与减少透明度回退为实色，规则沿用 P12 |
| Logo | 新版黑底白 π，`assets/byop-logo-1024.png` 与 `assets/byop-logo-mark.png` |
| 字体平滑 | darwin 启动时以 purego 注册 `AppleFontSmoothing=0`（不持久化），对应原型 `-webkit-font-smoothing: antialiased`；其他平台为空操作 |

## 1:1 对齐方法

1. 先按原型写阶段规格 R0–R13，再逐轮开发与审核。
2. 隔离配置启动走查构建，用 CapturePage 输出 2360×1520 原生截图。
3. 与原型 PNG 做并排比对和坐标测量，逐项修正间距、字号、行距、浮层位置和设置页偏移。
4. 共 8 轮返工（r1–r8）。第 6 轮起聊天正文行位置与原型差异约 1–4 缩放像素，即不超过约 2 逻辑像素。

## 审核发现与修复

Opus 第 8 轮复审：P0–P2 全部关闭。

| 发现 | 最终修复 |
|---|---|
| 行内代码用细空格模拟内边距，导致复制文本被污染 | 第 8 轮移除该做法；复制、右键“复制为 Markdown”、Cmd+Shift+C 均复核为原文 |
| 代码围栏识别不完整 | 修正围栏检测，代码块内边距 12/16、行高 1.4 |
| HUD 长扩展状态溢出 | HUD 行裁剪，长状态单行省略 |
| 字体平滑与原型不一致 | 引入 purego 直接依赖，仅 darwin 注册默认值 |
| 设置、任务栏、Composer、HUD、浮层与原型间距偏差 | 逐轮按截图坐标修正 |

## 最终检查

| 检查 | 实际结果 |
|---|---|
| gofmt -l . | 无输出 |
| go vet ./... | 通过 |
| go build | 通过（Go 1.27.2 darwin/arm64） |
| go test ./... -count=1 | 435 项通过，3 个包 |
| git diff --check | 通过 |
| 产品 token | 根设计与仓库副本 tokens.json 逐字一致 |
| 语言包 | en、zh-Hans 条目数一致 |
| 版本 | mygo.json 与关于页均为 0.13.0 |

## 原生窗口走查

全程隔离 BYOP_CONFIG_DIR、PI_CODING_AGENT_DIR 和工作区，未连接 Pi，无真实密钥和付费请求。截图位于 `docs/screenshots/p13-*.png`：

| 截图 | 内容 |
|---|---|
| p13-dark-ember-nav10 / nav55 / nav90 | Ember 壁纸，导航透明度 10%、55%、90% |
| p13-dark-graphite、p13-dark-jade | 其他两张壁纸 |
| p13-dark-refraction-off、p13-dark-wallpaper-none | 关闭折射、无壁纸 |
| p13-glass-popover | 玻璃浮层 |
| p13-settings-appearance | 外观设置中的壁纸与玻璃控制 |
| p13-tasks、p13-narrow-760-tasks | 任务栏，及 760px 窄窗 |
| p13-collapsed-rail | 导航收起 |
| p13-high-contrast、p13-reduce-transparency | 高对比、模拟减少透明度回退 |
| p13-light-unchanged | 浅色主题保持不变 |

减少透明度为应用内模拟切换，未修改系统辅助功能设置。

## 安装验证

发布源码提交 `8e19653`，MyGo 本地签名构建（跳过公证）。安装路径 `/Applications/BYOP.app`，版本 0.13.0；原 0.12.0 移到 `/Users/lexleizou/.Trash/BYOP-pre-P13-20261026-003330.app`，可恢复。
安装可执行文件与 dist 产物 SHA-256 一致：`865189de2df344c9641c014109aa5427e9168f023dba85682416354f20f08e99`；AppIcon.icns 与 dist 一致，为新版黑底白 π；codesign 校验通过。

## 已知限制

- 玻璃为 MyGo 材质着色器，不是 Apple NSGlassEffectView；MyGo 不支持 text-shadow，原型中的文字阴影未复原。
- R13：行内代码胶囊左右留白比原型窄约 2–3px。MyGo 按 min(字号×0.22, 圆角) ≈ 3px 填充，没有 CSS padding 接口；为保证复制文本准确，不再修改源文本。
- 截图不含窗口红绿灯按钮；浅色主题不使用壁纸。
- 品牌名使用 20px/650，是 12/14/18 字号规则的唯一例外；导航中过长会话标题截断。
- P3：拖拽归属状态可能残留；一项布局测试只校验公式；自定义代码块无语言标签和复制按钮（原型同样没有）。
- Windows、Linux 未验证。
- 应用使用本地签名，未公证。
