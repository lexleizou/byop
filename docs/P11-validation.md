# P11 验证记录

日期：2026-10-24

阶段：统一模型控件（BYOP）

基线：`3d153a6`。

## 已验证

### 模型品牌与 Picker

- DeepSeek、Claude、Qwen SVG 已复制到 `assets/brands/`，通过 `go:embed` 和 `ui.MustParseSVG` 注册；应用启动时解析成功。
- `modelBrand` 只按 Model ID 或显示名称的大小写不敏感子串识别品牌，不从 provider 或 API 协议推断。未知品牌使用 Lucide `Sparkles`。
- 模型行按 provider 分组，并在同一 provider 内按显示名称聚合为 family。行 ID 为 `provider/familyKey`，Description 始终为当前选中变体的 Model ID，Detail 为空。
- 自定义 Picker 搜索覆盖 family 名称、provider、family key 以及所有变体的 Model ID 和显示名称。
- 每个 family 默认选择当前会话记住的变体；没有记录时选择当前活动变体；两者都不适用时选择 Pi 返回顺序中的第一项。

### 上下文与 composer

- 同一 provider、同一显示名称的模型构成容量 family。只有 family 中至少有两个 ContextWindow 已知的变体时，才在模型控件后显示独立上下文 Picker。
- 上下文选项按 ContextWindow 升序排列，Label 为 `256K`、`1M` 等容量，Description 为对应 Model ID；选择后调用 `selectModel(provider, variantID)`。
- composer 模型控件使用 Onevra chip 的 26px 高度、6px 圆角、7px 水平内边距和 6px 内容间距。模型名为 14px，渠道与容量为 12px，无中点分隔；控件按内容展开，accessory row 可换行。
- 模型控件中的容量只来自当前活动模型定义；未知容量不显示。

### 会话状态与失败恢复

- 进程内记忆使用 `sessionKey → provider/familyKey → modelTuple`，其中 `sessionKey` 为 Pi session file 或 session ID，tuple 保存变体 ID、思考档位和 Fast 字段。
- 模型切换成功后读取新思考档位。记住的档位可用时按字符串恢复；不可用时保留 Pi 当前档位并显示一次本地化提示，不按数组索引映射。
- 模型、变体和思考档位确认后立即写入当前会话 tuple；再次选择当前变体、打开后取消 Picker 均不发起 RPC。
- 模型切换期间模型、上下文、思考控件使用最后确认值并切为只读，Send 禁用，草稿保留。模型切换、档位刷新、档位恢复和状态读回失败均提供 Retry；旧 generation、epoch 或 session 响应不会覆盖新状态。
- Pi 1.4.0 没有快速模式 RPC，`piFastModeSupported()` 固定返回 `false`，因此不渲染闪电开关，也不伪造 RPC。tuple 仍保留 `Fast` 字段供后续能力接入。
- 运行、压缩或会话操作期间，模型、上下文和思考控件均为只读；运行中的排队与插话发送入口保持不变。

### HUD

- HUD 按分支、输入/输出 Token、费用、整数缓存命中率、平均速度和上下文百分比排序，段间使用两个空格，扩展状态同样不使用中点。
- 分支不再加括号；缓存 R/W Token 从主标签移入 tooltip；上下文只显示百分比，自动压缩后缀保留。
- 平均速度按已完成回合的 output token 增量和首个 assistant 输出到 `agent_end` 的生成时间累计。`WillRetry=true` 的结束不采样；没有可靠起点或有效时间时不生成样本。
- HUD stats 请求带独立序号，晚到的旧请求不会消费 `agent_end` 后的待采样回合。会话切换、新会话、重连和退出会重置累计值。
- tooltip 明确把平均速度标为客户端估算。

### 测试与本地化

- `p11_test.go` 覆盖容量 family 聚合、当前/记忆变体选择、品牌识别、按可用字符串恢复思考档位、平均 tok/s、无可靠起点跳过采样、旧 stats 请求隔离和 HUD 无中点。
- 所有新增界面文字同时写入 `locales/en.json` 与 `locales/zh-Hans.json`，并重新生成 `locales/catalog_gen.go`。

## 审核

开发：DeepSeek V4.1 Flash。审核：Claude Opus 5.5。

| 轮次 | 发现 | 处理 |
|---|---|---|
| 第 1 轮 | 4 个 P1、5 个 P2、4 个 P3 | 全部修复 |
| 第 2 轮 | 4 个 P2、1 个 P3 | 全部修复：读回重试常驻模型控件；读回作为会话操作，晚到结果不清除新操作的忙碌状态；会话加载失败清空旧模型并标记未确认；思考档位超时读回实际状态；记住的档位不再被 Pi 默认档位覆盖 |
| 第 3 轮复核 | 无 P0–P2 | 通过 |

## 截图

屏幕处于锁定状态（`CGSSessionScreenIsLocked=Yes`），以下为 MyGo headless 渲染，不是真实窗口截图：

- `docs/screenshots/p11-composer-light-zh.png`
- `docs/screenshots/p11-composer-dark-en.png`
- `docs/screenshots/p11-model-picker-dark-zh.png`
- `docs/screenshots/p11-context-picker-light-zh.png`
- `docs/screenshots/p11-running-readonly-light-zh.png`
- `docs/screenshots/p11-unconfirmed-light-zh.png`

## 检查结果

- `gofmt -l .`：无输出。
- `go vet ./...`：通过。
- `go build -o byop .`：通过。
- `go test ./... -count=1`：293 项通过，覆盖 3 个 package。
- `git diff --check`：无输出。
- `mygo i18n check .`：通过，英文与简体中文各 570 条消息。

## 未验证

- 真实窗口与 800px 窄窗口走查未执行（剩余 P3）。
- 真实 Pi 账户上的品牌映射、多容量 family、切换失败注入和部分应用行为未验证。
- Linux 与 Windows 未验证。
- Penpot R2 原型未完成（574 个节点中完成 240 个，MCP 断开）。
- “新对话默认值”去留仍待决定（设计文档 §10.9）。
- 早期界面文字中残留的中点分隔符不在本阶段范围内，后续清理。

## 已知限制

- Pi 1.4.0 不提供快速模式 RPC，因此快速开关不显示。
- 上下文变体只能从同一 provider、同一显示名称且 ContextWindow 已知的模型推导；Pi 没有独立上下文设置 RPC。
- 平均 tok/s 是 BYOP 客户端估算，生成计时从首个 assistant 文本增量开始；它不等同于供应商的服务端纯解码速度，也没有可靠起点时不恢复历史速度。
- 会话 tuple 仅保存在当前 BYOP 进程内，重启后由 Pi 当前会话状态重新建立。


## 打包与安装

- `mygo.json` 与 `appVersion` 升级为 0.11.0。
- `mygo build -skip-notarize` 生成 `dist/darwin-arm64/BYOP.app`（14.0 MB）和 `BYOP 0.11.0 arm64.dmg`（4.4 MB），未公证。
- 旧版 0.10.0 移入废纸篓，新版复制到 `/Applications/BYOP.app`；`CFBundleShortVersionString` 为 0.11.0，`codesign -v` 通过。
- 启动后进程 `/Applications/BYOP.app/Contents/MacOS/BYOP` 正常运行；屏幕锁定，未做真实窗口截图。
