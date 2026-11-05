# P28：对话 Mermaid 一键预览与 P27 使用验收

日期：2026-11-06。基线 6b2256d / BYOP 0.20.0。用户已批准开始，无需再次询问启动许可。

## 目标与范围

在完整的 Mermaid fenced code block 上提供“预览图解 / Preview diagram”按钮，点击后把该代码块的源码带到已安装的 `local.mermaid-diagram` 独立 WebView 页面并自动渲染。保留原对话与源码，不改为聊天内嵌 WebView，不引入通用插件市场/持久草稿/导出/新审批配置。应用版本 0.21.0，插件版本 0.2.0（与0.20宿主手动编辑路径仍兼容）。

同时补齐 P27 的真实 Pi 会话与 ZIP 安装验收，过程使用独立配置与测试会话。真实 Pi 使用用户现有已配置的 Qwen，不复制或打印凭据。P25剩余消费者和HUD仍暂缓。

## 当前代码定位

- `chat_view.go:configure...` 中 `s.chatView.markdown.CodeBlock` 经 `pluginCodeBlock` 包装 plain fallback；`block.Language`、`block.Open`、`block.Code()` 已提供 fence 状态和源码。
- `plugin_message_renderers.go:pluginCodeBlock` 保留现有 P25 renderer；新增按钮独立于其渲染结果，在同一 CodeBlock callback 外层追加，不能替换/触发已有 renderer。
- `plugin_webview.go:openPluginWebView` 定位独立主视图；`createPluginWebViewAfterFrame` 在postUI后构造并缓存WebView。
- WebView CallerPage身份、注册/启用状态和manifest fingerprint已经由 `pluginWebViewBridge.Call` 检查，`dispatchPluginWebViewCall` 分发具体方法。
- 主题/语言通过MyGo事件发送；页面可用 `window.mygo.on` 订阅事件，并用 `window.byop.call` 读取宿主。
- 插件源码 `plugins/mermaid-diagram/web/app.js` 有异步 `renderSource()`、`applyLocale()`；初始化会填充示例，不能覆盖导入的源码。

## 最小交互

1. 仅 `strings.EqualFold(strings.TrimSpace(block.Language), "mermaid")` 且 `!block.Open` 的非空代码块显示按钮。其他语言及未闭合流式代码不显示。源文本不改写。
2. 点击后再次检查插件是否注册/启用、存在 `mermaid.diagram` 主视图、版本至少0.2.0。缺失/停用/旧版本时保持对话页，用本地化notice提示安装/启用/更新；不自动安装、启用或丢掉源码。
3. 现有插件限制20000 UTF-16 code units，宿主使用一致的长度检查，超长给notice且不切页。
4. 有效时先存内存请求，再打开插件页。无论页面首次加载、已缓存打开还是回收后重建都应收到本次源码并自动渲染。
5. 再次从另一个代码块打开时覆盖该独立预览页的临时草稿（仅用户再次点击触发），最新请求胜出。回 Chat 时原消息不变。
6. 错误源码仍到达插件编辑器并显示现有渲染错误，用户可原地修正重渲染。

## 宿主/插件协议（固定，两个开发席共同遵守）

- 固定插件ID `local.mermaid-diagram`、viewID `mermaid.diagram`。
- 宿主内存维护最新 `{requestId: string, source: string}`；requestId每次点击唯一/单调，空请求返回null；不落盘、不向Pi/worker广播。字段可放appState，使用 `pluginWebViewsMu` 保护所有读写。
- 只允许该插件该view调用 `plugin.getMermaidPreview`，其返回值直接为上述对象或null。其他插件/view拒绝；沿用已有Call身份校验。方法在 `dispatchPluginWebViewCall` 分发，不需要额外worker能力或manifest字段。
- 宿主 `byop.mermaid-preview` MyGo事件只发给目标WebView，payload `{requestId}`；已有页面用它触发重新读取。初次/重载页面必须主动调用get，不能依赖DOMContentLoaded之前可能丢失的事件。读取为幂等，不立即清空，页面用requestId去重。
- 在插件卸载/停用/替换/空闲回收时清除陈旧请求。正常open过程中创建WebView不能误清除刚排队的新请求；手动Reload或异常后的同一页面重建允许恢复最近一次预览内容。按这个区别安排清理与恢复；测试和实际边界写入验证记录。
- 页面先等待本地化初始化，订阅事件，再读取；保证事件/初始读取竞态不会回滚到旧requestId。把source赋给textarea.value，再调用现有renderSource，不执行源码、不innerHTML注入、不新增网络。
- 在0.20宿主上不存在该方法时仅忽略该能力，手动编辑/渲染仍可用。无bridge浏览器路径维持可用。

## 文件归属

开发席A（宿主）：允许 `chat_view.go`、`plugin_webview.go`、`main.go`必要状态字段/版本、`mygo.json`版本、`locales/en.json`、`locales/zh-Hans.json`及生成catalog、新增 `mermaid_preview.go` 和 `p28_test.go`。可必要小改插件生命周期清理调用。禁止修改插件目录。

开发席B（插件）：仅 `plugins/mermaid-diagram/**`，不得改vendored Pele及许可。实现协议消费、竞态防护、0.2.0版本、README说明与少量有意义的定向测试。禁止宿主代码。

两个开发席在各自独立副本工作，不commit/push、不操作桌面、不继续委派。协调方按归属合并。阶段设计、验收、发布包由协调方负责。

## 最小验证

宿主：完整mermaid/开放fence/其他语言资格；不可用/旧版插件不跳转；调用隔离；连续请求最新值和生命周期失效（可复用现有测试模式）。不新增全量门禁。正常流程先跑通。
插件：首次导入、再次导入、相同ID不覆盖用户编辑、错误源码可恢复、旧宿主无方法手动路径不受影响；避免纯镜像实现测试。可用Node少量stub验证事件与异步初始化。

合并后：gofmt、go vet、go build、go test、node --check、Node插件测试、git diff --check。真实BYOP：安装ZIP，真实Pi返回包含Mermaid的回复→点击→渲染；回Chat点第二张图→更新；冷打开/已打开各一次；主题语言/窄窗沿用P27只补相关检查。

Cursor Opus 5.5 @1m独立复审，P0–P2修复并复审后提交origin/main。实际受阻的外部调用按证据记录，不能用fixture代替真实Pi成功结论。产物为0.21.0 DMG、0.2.0插件ZIP、docs/P28-validation.md。
