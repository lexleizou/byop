# BYOP 项目规则

BYOP（Build You Own Pi）是 MyGo 原生 UI + 外部 Pi CLI 的多插件工作台原型。本文件对所有在本仓库工作的 Agent 生效，优先于上级目录的通用研究规则；与全局规则冲突时以本文件为准。

## 开发分工（默认执行，无需每次提醒）

| 角色 | 模型 | 职责 |
|---|---|---|
| 开发 | DeepSeek V4.1 Flash（`auto/deepseek-v4.1-flash`） | 按阶段规格实现代码、文档和最小必要测试 |
| 审核 | Claude Opus 5.5（`auto/claude-opus-5-5`） | 审核 diff，按 P0–P3 分级给出问题；P0–P2 交回开发修复后复审 |
| 协调 | 当前主 Agent | 写阶段规格、分派、复核检查、真实窗口走查、提交并推送 |

- 每个阶段先在 `~/AgentScratch/<YYYYMMDD>-byop-<phase>/` 写规格，再交给开发模型。
- 审核意见中的 P0–P2 必须修复并复审通过后才能提交；P3 可记录在验证文档的已知限制中。
- 指定模型不可用时，说明原因并告知替代方案，不得静默换模型。

## 产品约束

- 产品名 BYOP，标识使用 Pi 图标；运行时 UI token 以 BYOP Radix 产品 token `design-system/pi-workbench/tokens.json` 为准，设计根同名文件是权威来源，仓库文件是相同发布副本。`tokens.extensions.json` 只保留兼容、迁移和非冲突的组件几何；Onevra 原始 token 是只读上游参考，不覆盖其数值。
- 界面文字一律写入 locales/ 语言包（en 为源语言），不得在代码中硬编码用户可见文字；新增文字需同时补齐所有语言包并重新生成 locales/catalog_gen.go。
- 字号仅 12/14/18px：正文与代码 14px，标题 18px，最小 12px；可见边框 0.5px；间距 4/8/12/16/24px；输入 8px、浮层 12px 圆角。必要过渡统一 250ms ease-out，减少动态为 0。
- Liquid Glass 只用于标题、导航和必要浮层；阅读区与 Composer 内容背景不透明。高对比、减少透明度或不支持平台回退到 Radix 实色。
- 右侧任务栏默认收起。当前简单侧栏只是实施基底，不代表 A/B/C 布局已经由用户选定。
- Pi 以独立安装的 Pi CLI 运行，BYOP 通过 JSONL RPC 调用，不内置 Pi 内核，不复制凭据。
- 插件开发暂停：插件平台能力可以继续建设，但在 Lex 明确要求前不开发任何功能插件。Library、Document 是冻结候选，不出现在当前页面入口；`examples/` 只放契约 fixture。
- Automations 只统一已有登记、运行历史和失败暂停/恢复，不新增 Loop 调度器或 `/loop`。新对话默认值仍待决，不新增独立设置页；HUD 显示项开关尚未实施。

## 交付与检查

- 每个阶段完成后提交并推送到 `origin main`（公开仓库），提交信息使用 `feat:`/`fix:`/`docs:` 前缀并注明阶段。
- 最小检查：`gofmt -l .`、`go vet ./...`、`go build -o byop .`、`go test ./... -count=1`、`git diff --check`，涉及 `.mjs` 时加 `node --check`。不新增门禁、覆盖率要求或全量回归。
- 界面改动需要真实窗口走查，截图存 `docs/screenshots/`，只截 BYOP 窗口，不得包含私人内容或凭据。
- 每个阶段写 `docs/P*-validation.md`，区分已验证、未验证和已知限制。
- 过程文件（脚本、临时配置、日志）放 `~/AgentScratch/`，不提交到仓库；本仓库无法使用 `rm` 时，把废弃文件移到 `~/.Trash`。
