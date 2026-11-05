# BYOP 项目规则

BYOP（Build You Own Pi）是 MyGo 原生 UI + 外部 Pi CLI 的多插件工作台原型。本文件对所有在本仓库工作的 Agent 生效，优先于上级目录的通用研究规则；与全局规则冲突时以本文件为准。

## 开发分工（默认执行，无需每次提醒）

| 角色 | 模型 | 职责 |
|---|---|---|
| 开发 | Magpie 渠道 Qwen 3.8 Flash Next（`local-litellm/Qwen3.8-Flash-Next-NVFP4-RTX5000`）与 GLM 5.3-Flash（`local-litellm/GLM5.3-Flash-Maya-M`），两个开发席 | 按阶段规格实现代码、文档和最小必要测试 |
| 审核 | Claude Opus 5.5，按顺序调用：① Claude Opus 5.5 @ 1m \| Cursor（`cursor/claude-opus-5-5@1m`）；额度不足时转 ② Claude Opus 5.5 \| GitHub Copilot（`copilot/claude-opus-5.5`） | 审核 diff，按 P0–P3 分级给出问题；P0–P2 交回开发修复后复审 |
| 协调 | 当前主 Agent | 写阶段规格、分派、复核检查、真实窗口走查、提交并推送 |

- 开发席为两个，均为 Magpie 渠道（2026-11-05 用户指定，可用性已实测）：两个开发席使用独立副本，由协调方按文件合并。
- 审核席按上表顺序使用两个渠道（2026-11-05 用户指定，取代原 `auto/claude-opus-5-5`）：只有 Cursor 额度耗尽时才转 GitHub Copilot；两者都不够时说明原因并告知，不得静默换其他模型。

- 每个阶段先在 `~/AgentScratch/<YYYYMMDD>-byop-<phase>/` 写规格，再交给开发模型。
- 审核意见中的 P0–P2 必须修复并复审通过后才能提交；P3 可记录在验证文档的已知限制中。
- 指定模型不可用时，说明原因并告知替代方案，不得静默换模型。

## 开发原则（2026-11-05，用户指定）

1. **先设计后开发**：任何功能先有设计文件（阶段规格）再动手。设计以落地实现为目标，写到开发席能直接照着做为止；避免靠大量测试补救方向性错误，也避免设计放错（口径、范围或位置错误）。
2. **先正向后异常**：先实现并跑通正向主流程，再按需补异常流程；不要一上来就处理过多异常情况。
3. **反复递归时收缩为 MVP**：同一问题多次修复仍反复出现时，缩小范围做 MVP 功能，并向用户说明收缩了什么、暂缓了什么。
4. **避免过度设计与过度开发**：只做当前目标需要的设计和代码，不预建未被要求的抽象、配置项、扩展点或功能。

## 产品约束

- 产品名 BYOP，标识使用 Pi 图标；运行时 UI token 以 BYOP Radix 产品 token `design-system/pi-workbench/tokens.json` 为准，设计根同名文件是权威来源，仓库文件是相同发布副本。`tokens.extensions.json` 只保留兼容、迁移和非冲突的组件几何；Onevra 原始 token 是只读上游参考，不覆盖其数值。
- 界面文字一律写入 locales/ 语言包（en 为源语言），不得在代码中硬编码用户可见文字；新增文字需同时补齐所有语言包并重新生成 locales/catalog_gen.go。
- 字号仅 12/14/18px：正文与代码 14px，标题 18px，最小 12px；可见边框 0.5px；间距 4/8/12/16/24px；输入 8px、浮层 12px 圆角。必要过渡统一 250ms ease-out，减少动态为 0。
- Liquid Glass 只用于标题、导航和必要浮层；阅读区与 Composer 内容背景不透明。高对比、减少透明度或不支持平台回退到 Radix 实色。
- 右侧任务栏默认收起。布局已于 2026-11-05 拍板为方案 A（简洁侧栏，定稿，与当前实施一致）；已启用插件数量长期超过 8 个时重新评估方案 B。
- Pi 以独立安装的 Pi CLI 运行，BYOP 通过 JSONL RPC 调用，不内置 Pi 内核，不复制凭据。
- 插件开发暂停：插件平台能力可以继续建设，但在 Lex 明确要求前不开发任何功能插件。2026-11-05 拍板首批解冻 Mermaid 渲染（Pele）插件；Library、Document、Git、终端等其余候选继续冻结。P27 已实现独立 Mermaid 插件页，P28 增加对话代码块一键送到该页面自动预览（应用0.21.0、插件0.2.0，`plugins/mermaid-diagram/`）；验证边界见 `docs/P28-validation.md`，系统文件选择自动化仍未通过，聊天内嵌仍未实现；当前核心入口仍为 Chat、Plugins、Automations、Settings，`examples/` 只放契约 fixture。
- Automations 只统一已有登记、运行历史和失败暂停/恢复，不新增 Loop 调度器或 `/loop`。新对话默认值已定为完全跟随 Pi 的有效配置（2026-11-05 拍板；不新增独立设置页，BYOP 不保存跨会话默认值；会话内模型档位记忆保留并隔离，P27 只允许同一会话身份的 key 迁移）；HUD 显示项开关继续挂账观察（2026-11-05 决定）。

## 交付与检查

- 每个阶段完成后提交并推送到 `origin main`（公开仓库），提交信息使用 `feat:`/`fix:`/`docs:` 前缀并注明阶段。
- 最小检查：`gofmt -l .`、`go vet ./...`、`go build -o byop .`、`go test ./... -count=1`、`git diff --check`，涉及 `.mjs` 时加 `node --check`。不新增门禁、覆盖率要求或全量回归。
- 界面改动需要真实窗口走查，截图存 `docs/screenshots/`，只截 BYOP 窗口，不得包含私人内容或凭据。
- 每个阶段写 `docs/P*-validation.md`，区分已验证、未验证和已知限制。
- 过程文件（脚本、临时配置、日志）放 `~/AgentScratch/`，不提交到仓库；本仓库无法使用 `rm` 时，把废弃文件移到 `~/.Trash`。
