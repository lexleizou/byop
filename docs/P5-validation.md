# BYOP P5 Validation

验证日期：2026-10-20

## 范围

P5 在 P4.2 插件平台基础上完成按插件视图隔离的 WebView 宿主能力，包括 manifest
声明、`byop-plugin` 静态资源 scheme、页面身份校验、导航锁、主题桥、插件消息、
生命周期清理和原生失败回退。阶段仅提供平台能力；Library、Document 页面和导航占位
已移除，没有新增功能插件，也没有改变 Chat 或 Pi RPC。

## 实现

- 宿主版本升至 `0.5.0`；schema 1 和 Plugin API `1` 保持不变，现有
  `>=0.4.2 <1.0.0` fixture 仍可加载；
- `contributes.webviews[]` 新增必填 `entry`，要求插件根目录内的普通
  `.html`/`.htm` 文件；native view 和 WebView 共用唯一贡献 ID，navigation
  可引用两者；
- `byop-plugin://<pluginId>/<relative-path>` 只允许 GET/HEAD，文件上限 5 MiB，
  拒绝目录、未登记插件、路径穿越和符号链接越界，并设置 `nosniff`、
  `no-store` 和严格 CSP；
- CSP 默认禁止网络连接；仅当插件声明 `capabilities.network: true` 时允许
  `connect-src https:`；
- 每个插件视图懒创建一个 MyGo `ui.WebView`；导航锁只接受同一插件 scheme，
  拒绝 `window.open` 和所有权限请求；
- bridge 通过 `mygo.CallerPage(ctx)` 反查唯一 WebView，并在每次调用时核对当前
  URL、插件注册状态、视图/entry 声明和 manifest/root 指纹；
- `host.*` 共用 P4.2 的宿主调用与能力检查；`plugin.postMessage` 有 64 KiB
  上限、按插件限制并发，并调用插件 JSONL 的 `webviewMessage`；
- 预加载脚本提供 `window.byop.call`、`postMessage`、`onTheme` 和 `theme`，
  同步 `--byop-*` 主题变量并固定 `--byop-font-body: 14px`；
- 创建、加载或渲染进程失败时显示原生 warning card；Reload 重建实例，
  Back to Plugins 返回插件页；
- 重新扫描、插件停用/停止、声明或指纹失效和窗口关闭时销毁 WebView；仅离开页面
  时保留隐藏实例；
- `examples/hello-webview` 是无依赖的 P5 契约 fixture，不是功能插件。

## 验证结果

执行范围仅覆盖 P5 直接相关的 manifest、scheme、bridge、Node echo 和导航回退测试，
不扩大为全量功能或安全审计。

| 检查 | 结果 |
|---|---|
| `gofmt -l .` | 通过，无输出 |
| `go vet ./...` | 通过，无输出 |
| `go build -o byop .` | 通过，无输出 |
| `go test ./... -count=1` | 通过，`Go test: 72 passed in 2 packages` |
| `git diff --check` | 通过，无输出 |
| `node --check examples/hello-webview/plugin.mjs` | 通过，无输出 |
| `node --check examples/hello-webview/web/app.js` | 通过，无输出 |
| `python3 -m json.tool examples/hello-webview/plugin.json` | JSON 解析通过，并输出格式化内容 |

## 界面走查

协调者在 macOS 真实窗口中执行（Light 主题，`BYOP_PI_CONNECT=0`，插件目录为 `examples/`）：

| 步骤 | 结果 | 截图 |
|---|---|---|
| 扫描 `examples/`，确认 Hello WebView 出现在 Plugins 导航，Library/Document 入口已移除 | 通过；导航为 Chat、Plugins、Automations，插件组为 Hello Node、Hello WebView | `screenshots/p5-plugins.png` |
| 打开 Hello WebView，确认标题、插件名、Isolated 标记和静态内容 | 通过 | `screenshots/p5-webview-bridge.png` |
| 点击 Get theme | 通过；返回 `mode: light` 和主题颜色 | — |
| 点击 Ping plugin | 通过；插件回显 `kind: ping` 消息 | `screenshots/p5-webview-bridge.png` |
| 点击 Show notice | 通过；宿主显示原生通知横幅 | `screenshots/p5-notice.png` |
| 点击 Read file，确认因 `files: none` 被拒绝 | 通过；返回 `-32001 permission denied` | `screenshots/p5-permission-denied.png` |
| 点击外部链接，确认页面不离开 `byop-plugin` | 通过；页面保持原位 | — |
| 触发 Reload | 通过；页面恢复为 Ready | — |
| 切换 Dark 主题 | 未执行 | — |
| 停用插件或关闭窗口后的销毁 | 仅单元测试覆盖，未做界面走查 | — |

## 未验证与边界

- 未验证 WebView 的焦点、键盘输入和 Dark 主题视觉结果；
- 未执行 Linux、Windows、Dark 主题和窄窗口专项走查；
- 未验证 `capabilities.network: true` 下的真实外部请求；自动测试仅验证 CSP
  从 `'none'` 切换为 `https:`；
- 未验证第三方认证弹窗、下载、文件拖放、IME、剪贴板和媒体权限；权限处理器
  在平台层统一拒绝；
- CSP 允许 `'unsafe-inline'` 样式以支持静态样式，但不允许内联脚本；WebView
  页面仍应视为不可信插件内容，其宿主业务能力仅通过 bridge 获得；
- `byop-plugin` 经 `Protocol.Handle` 注册后会被 MyGo v0.5.5 视为可信页，因此
  插件页面无需经过 bridge 即可调用内置 `mygo:window`，关闭、最小化或全屏切换
  宿主窗口并修改窗口标题；关闭宿主窗口会触发 BYOP 关闭流程。上游后续应请求
  MyGo 提供按页面或按 scheme 禁用 `mygo:window` 的方式，或允许 handled custom
  scheme 只暴露指定的 `BindAs` 服务；
- 插件资源文件名须使用 ASCII 安全字符；scheme handler 会拒绝 percent-encoded
  路径；
- 插件根目录内的文件，包括 `plugin.json` 和 `plugin.mjs`，都在该插件页面的
  信任范围内，可被其自身页面读取；
- WebView 不提供插件安装、Pi 凭据、任意本地文件或 Pi RPC 访问；这些边界未因
  P5 扩大；
- native 插件仍没有进程级沙箱；WebView 与 native/Node 插件共用既有声明能力，
  scheme 隔离不替代操作系统进程沙箱。
