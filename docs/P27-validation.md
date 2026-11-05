# P27 验证记录

日期：2026-11-06。基线 `50ca503` / BYOP 0.19.2；候选版本 0.20.0，Mermaid 插件 0.1.0。范围见 [阶段设计](P27-design.md)。

## 后续补验（P28，2026-11-06）

真实Pi调用、新会话默认档位与切回原会话的模型/档位恢复已通过；实际ZIP的宿主安装逻辑、原生渲染与重启恢复也已通过。系统文件选择框仍因自动化输入受阻而未完成，不以安装后端通过替代。详情见 [P28验证记录](P28-validation.md)。下文保留P27发布时的原始验证边界。

## 实现

- 保持 A 简洁侧栏，已启用插件长期超过 8 个时再评估 B；HUD 开关仍暂缓。
- 修复 `applyPiSessionState` 无条件迁移模型档位记忆：仅同一会话身份的 key 变化才迁移，不把旧会话记忆搬入新会话，也不删除旧会话记忆。新会话 RPC 仍完全跟随 Pi 返回状态。详见 [核查报告](P27-new-session-audit.md)。
- 新增 [Mermaid 独立插件](../plugins/mermaid-diagram/README.md)：固定 Pele 1.7.0、离线 SVG 图片预览、源码编辑、缩放/适应宽度、中英与主题跟随。插件包独立安装，DMG 不自动安装插件。

## 自动检查（实际执行）

| 检查 | 结果 |
|---|---|
| `gofmt -l main.go p27_test.go` | 无输出 |
| `go vet ./...` | 通过 |
| `go test ./... -count=1` | 通过；真实 Pi 集成仍沿用原有开关，不因全绿声称调用了模型 |
| `go build` | 通过 |
| `git diff --check` | 通过 |
| 插件 worker、页面、renderer 的 `node --check` | 通过 |
| `node --test plugins/mermaid-diagram/test/render.test.mjs` | 8/8 通过，覆盖中文 flowchart/sequence、Agentflow、输入错误、主题注入、data URL |
| Pele 包与 ESM 校验 | 官方 tgz SHA-256 与既有评估一致，ESM 与解包文件逐字一致 |
| 插件 ZIP 校验 | 15 个文件，CRC 检查通过 |
| MyGo 打包 | `go run github.com/egoist/mygo/cmd/mygo build -skip-notarize` 通过，生成 macOS arm64 App/DMG；未公证 |

新增 3 个 Go 定向测试验证新会话不继承记忆、切换/切回隔离、同 ID 转文件 key 仍可迁移。开发席反向验证确认前两项在撤销修复后失败，现有 P11 同会话迁移测试继续通过。

## 页面与原生验证

### 受管浏览器

直接打开插件页面的本机 HTTP 预览（无宿主 bridge）：中文 flowchart 渲染成功、放大至 125%、输入非法源码显示纯文本错误并撤下旧图，再输入中文 sequence 成功恢复。修复后开发席复测空态 `display:none`、示例加载、放大后适应宽度会移除内联宽度。

### 真实 BYOP / MyGo WebView

协调方用本次 Go 构建和最终 `dist/darwin-arm64/BYOP.app/Contents/MacOS/BYOP` 启动隔离实例，配置、插件与工作区均在本次临时目录。Pi 连接关闭，未改用户真实插件/会话数据，未替换 `/Applications/BYOP.app`。

已完成：

1. 注册独立插件，侧栏与管理页正确显示 Mermaid 名称，打开真实 WebView。
2. 点击渲染，中文 flowchart 可见（769×152 自然尺寸）；最终包空态不再显示图片替代文本。
3. 放大 125% → 适应宽度 100%；真实 900×760 窗口自动收起导航并上下排列源码/预览，图解可读且页面可滚动。
4. 通过真实 Settings 把语言切为 English、外观切为 Dark，再回 Mermaid 页面；页面文案和图解颜色跟随，源码内容保留。

截图：

- [浅色中文](screenshots/p27-mermaid-light.png)
- [900×760 窄窗](screenshots/p27-mermaid-narrow.png)
- [深色英文界面](screenshots/p27-mermaid-dark-en.png)

不声称完成：本次没有操作安装文件对话框/从 ZIP 安装的端到端流程（使用隔离插件目录注册）；ZIP 结构与 CRC 已校验。未重跑旧 P25 的全部消费者、P26 原生拖放或真实模型推理，旧验证挂账继续保留。

## 审核与修复

Cursor Claude Opus 5.5 @1m 初审：无 P0/P1，3 项 P2；均已交开发修复：

- 多行示例放在 manifest 共用语言包触发宿主控制字符校验，导致 `%key%` 泄漏：示例移到 `samples/`，worker 返回示例；宿主校验保持不变。
- `#preview {display:block}` 覆盖 hidden：增加明确的 hidden 样式。
- `delete style.width` 无法清除内联宽度：改为 `removeProperty('width')`。

Cursor 同席最终复审通过：三个 P2 已消除，没有新增 P0–P2，代码可提交。审核当时尚缺的本验证文档已由协调方补齐，关联链接均已核对。

本次模型执行事实：GLM 在当前 Magpie Chat Completions 渠道因 `thinking` 参数不兼容被拒绝，关闭思考档位后同样失败，未产出实现。已告知用户，由其已指定的 Qwen 3.8 Flash Next 在两个独立副本完成插件与会话修复。审核使用 Cursor，未触发额度回退。目录 ID 的变化不代表改换模型来源。

## MVP 边界与 P3 限制

- 本次是独立插件页，尚无聊天内嵌 Mermaid、源码持久保存、导出、链接跳转或图中文字选择；页面回收后草稿不保证保留，页面已提示。
- 重置缩放与适应宽度目前作用相同；fit 会放大小图。未额外添加设置或缩放模式。
- 切换语言时未保留的错误状态可能回到“就绪”；语言初始化有重复请求的轻微竞态风险。
- 字体变更只更新 SVG 字体变量、不重新排版；少见的含 `&` 自定义字体名可能使 SVG 解析失败。正常宿主默认字体已验证。
- 最终原生走查中，页面与宿主核心控件已切成英文，但已打开的插件外层标题保留中文；重新注册/重开前的宿主标题即时翻译未在本次扩大修复。
- 高对比、非默认字体、所有 Mermaid 图种及大规模复杂图性能未全面验证。Agentflow 只做渲染，不执行流程。

## 产物

- `dist/darwin-arm64/BYOP 0.20.0 arm64.dmg`，7,799,046 字节，SHA-256 `e0f9abc255669e0d7798689d9332f558f2f268847924cc9b6fddb99ecd50104f`。
- `dist/plugins/mermaid-diagram-0.1.0.zip`，268,381 字节，SHA-256 `0589cb8c64ecd6be249f0895f5ba018d7919a0640d6efc521d518c168d9580e0`。
