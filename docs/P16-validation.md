# P16 J Agent 可切换视觉主题

日期：2026-10-28。应用版本：0.16.0。开发：DeepSeek V4.1 Flash；协调与原生截图：Codex；独立复审：Codex 子 Agent。指定 Opus 通道返回 Copilot `model_not_supported`，告知替代后完成独立复审，未把替代审核记作 Opus。

## 本阶段交付

Appearance 新增 `BYOP` / `J Agent` 视觉预设。预设独立于已保存的 Light / Dark / System 模式；J Agent 在该快照中固定为深色，切回 BYOP 后恢复原主题模式。缺失或未知预设回退 BYOP，保存失败回滚内存中的有效外观。

| 范围 | 实现 |
|---|---|
| 导航 | J Agent 默认 261 px、全高蓝灰 tint、24 px 原生模糊、1 px 右边界、28 px 紧凑行；默认宽度和拖拽宽度与 BYOP 分开保存 |
| 主区 | 无边到边浮卡，标题区 56 px；聊天列最大 768 px、桌面左右 48 px，窄窗口随可用宽度收缩 |
| 可读性 | 聊天标题栏使用克制的顶部深色渐变；Settings、Plugins、Automations 与插件页面使用不透明 J 中性背景，避免亮壁纸穿透标签 |
| 聊天 | 15 px 正文、13 px 代码；用户胶囊 radius 28，助手卡和输入区 radius 12；真实工具顺序、折叠状态、复制和后续发送行为沿用现有 BYOP 数据 |
| 输入区 | 最小约 92 px、11 px 内边距、28 px 添加按钮、32 px 发送按钮；保留模型、上下文、表格附件、队列与 steering 控件 |
| 表面 | J Agent 设置、插件、自动化、任务栏、弹出菜单和表格宿主使用同一 J 中性 palette / blur / tint；高对比与减少透明度使用不透明回退 |
| 动效 | J Agent 面板过渡使用 125 ms ease-out 近似；BYOP 保持 250 ms；减少动效仍为 0 |
| 壁纸 | J Agent 默认使用已核实的 `jagent-misty-mountains.webp`，可切换无壁纸；J Agent 与 BYOP 壁纸选择独立保存 |

预设在写入前即时应用；偏好写入失败时恢复前一个预设并显示本地化通知。J Agent 的 BYOP 专属导航透明度、模糊与折射行在 Appearance 中隐藏，并显示固定预设说明；外观预览绘制实际 J 导航/聊天/输入区材质，而不是复用 BYOP 预览。

R2 原生截图复核后的修正：J Agent 非聊天页主体与标题栏改用实色深底；聊天标题栏叠加深色渐变；Appearance 分类说明、壁纸说明改用 J Agent 实际表面描述；预览中的对话卡补齐玻璃承载面。BYOP 分支保持原路径。

## 已执行验证

以下结果来自当前源代码：

| 项目 | 结果 |
|---|---|
| 编译检查 | `go build -o byop .` 通过 |
| P16 定向测试 | `go test ./... -run 'TestP16' -count=1` 通过 10 项 |
| 完整 Go 测试 | `go test ./... -count=1` 通过 466 项 |
| 现有 BYOP 回归 | 完整现有测试通过；BYOP 默认值未由 J 指标覆盖 |
| P15 表格回归 | 现有 P15b 测试保留；J Agent 壳层定向测试确认表格上下文 chip、队列和发送控件可渲染 |
| 国际化 | catalog 重新生成；`i18n check .` 通过，en / zh-Hans 各 794 条 |
| 格式/静态检查 | `gofmt -l .` 无输出、`go vet ./...` 通过、`git diff --check` 通过 |

P16 定向测试覆盖：

- 缺失、未知和大小写预设归一化，旧用户默认 BYOP；
- J Agent 深色有效主题、切回 BYOP 的 Light 恢复和两套导航宽度持久化；
- 偏好写入失败时预设与有效主题回滚；
- J 指标、125 ms 动效、实际 alpha tint 与减少透明度实色回退；
- J / BYOP 壁纸选择隔离和嵌入式 J 壁纸解码；
- Appearance 切换后 J 专属行、壁纸和固定材质说明渲染，BYOP 专属玻璃控件隐藏；
- 1180×760 与 760×600 下主区输入、发送、设置、队列和表格上下文存在且未越出主区。

## 最终复审、原生验证和安装

- 独立 Codex 限定复审 PASS：完整输入区占位、运行过程可折叠、展开保持阅读位置、J 固定深色不修改 BYOP 模式四项均闭环，无残余 P0–P2。
- 最终源代码的原生窗口 CapturePage 截图 17 张，见 `docs/screenshots/p16-*.png`。包含 J 聊天、模型菜单、外观、插件、自动化、任务、窄窗口、高对比、减少透明度模拟及两套主题往返。
- 相同示例数据下，BYOP 聊天及模型菜单与基线 7256755 的截图逐像素一致；BYOP 深色、浅色经过 J 往返后分别逐像素一致。
- CUA 实际点击外观切换：J 显示只读“当前：深色”，不出现可修改模式按钮；切回 BYOP，模式、透明度、模糊与折射控制恢复。
- `mygo build -skip-notarize` 成功生成 0.16.0 app / DMG；安装包及已安装 app 均通过 `codesign --verify --deep --strict`。未执行 notarization。
- `/Applications/BYOP.app` 已更新至 0.16.0 并启动；已安装可执行文件与 dist 的 SHA-256 均为 `d98de4508f39f7f35a02f3ebca7751da966660366fe23f7b2453fc0cbef5cfdf`。
- 原 0.15.0 备份：`/Users/lexleizou/.Trash/BYOP-pre-P16-20261028-213615.app`。生产偏好保留；未强制为用户切换新主题。
- 现有黑白 Pi Logo 资产无改动；P15 表格实现与五份进行中的表格设计文档未纳入本阶段修改。

## 未验证与已知限制

- 未声称 100% 像素一致。参数来自 J Agent 0.3.9 的保存 DOM/CSS 与截图，字体栅格、WGSL/原生 blur/saturation、阴影和部分浏览器合成效果只能近似。
- J Agent 仅实现测量快照对应的深色外观；未实现 J Agent 的浅色、自定义强调色、字号滑杆、上传背景或专家/收件箱/浏览器等工作台功能。
- 不复制 J Agent React/CSS、品牌资产或交互协议；只复用测量参数、交互目标与已独立核实许可的雾山照片。
- 原生走查使用隔离配置与示例会话；未发送真实模型请求，未执行真实表格数据修改。窄窗口任务面板沿用覆盖式收纳。
- 现有 P15 表格功能与数据层未改写；J 预设只改变宿主外观。完整真实表格交互回归仍以 P15b 验证记录为准。

## 相关来源

- `reference/agent-teardown-20261026/j-agent/02-ui-pixel-spec.md`
- `reference/agent-teardown-20261026/j-agent/03-interaction-motion.md`
- `reference/agent-teardown-20261026/j-agent/07-theme-source-map.md`
- `reference/agent-teardown-20261026/j-agent/data/live_ui.json`
- `assets/wallpapers/JAGENT-WALLPAPER-NOTICE.md`
