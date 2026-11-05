# Mermaid Diagram（local.mermaid-diagram）

BYOP 首批解冻的功能插件 MVP：在独立页面粘贴 Mermaid 源码，点击「渲染」，
由本地 vendored [Pele](https://pele.run) 1.7.0 同步生成 SVG 并以图片呈现。
支持缩放与适应宽度、跟随宿主主题与中英语言、源码编辑后重新渲染。全部处理
在本机完成，无网络请求、无子进程、无文件写入。

## 使用流程

1. 在 BYOP 侧栏 **Plugins** 页点击「从文件夹添加」（Add from folder）选择本目录
   `plugins/mermaid-diagram`，或先把本目录打包后用「从 zip 添加」（Add from zip）
   安装；安装后在侧栏打开 **Mermaid**。
2. 页面预置一个可编辑的示例图。替换或编辑 Mermaid 源码，点击「渲染」
   （或按 ⌘/Ctrl+Enter）。
3. 预览工具栏：缩小 / 放大 / 适应宽度 / 重置缩放。放大后预览区可滚动。
4. 渲染失败时显示错误说明（含行列定位），保留源码可继续修改重试。

## 实现要点

- Node worker（`plugin.mjs`）只负责生命周期与本地资源分发：页面通过
  `bridge.postMessage({action:"locale"})` 读取 `l10n/<locale>.json`，manifest
  的 `%key%` 占位符由宿主从同一份文件解析，页面文案不硬编码。
- 宿主校验 l10n 值时拒绝控制字符（含换行），因此多行的起始示例放在
  `samples/<locale>.txt`，由 worker 随 locale 请求一并返回；l10n 包内只保留
  单行 UI 文案。
- 渲染管线在 `web/diagram-renderer.mjs`（纯函数，可在 Node 直接测试）。
- 宿主 WebView CSP 为 `img-src 'self' data:`（无 blob:），因此 SVG 以
  `data:image/svg+xml` 呈现于 `<img>`，源文本与 SVG 均不注入宿主 DOM。
- 独立 SVG 文档无法继承页面 CSS 变量：渲染时把白名单宿主主题色封装为
  `--pele-*` 声明注入 SVG；主题切换只重建图片，不重新执行源码。
- 源码仅在主动点击时渲染；文本只驻留页面内存，页面回收后不保证保留。
- 拒绝 init/config frontmatter（`---` 块与 `%%{…}%%`）与超过 20000 字符的
  输入；渲染选项固定。

## 开发与测试

```sh
node --check plugin.mjs web/app.js web/diagram-renderer.mjs
node --test test/
```

浏览器预览（无 bridge 时页面自动降级为 fetch 语言包）：

```sh
cd plugins/mermaid-diagram && python3 -m http.server 8791
# 打开 http://127.0.0.1:8791/web/index.html
```

## 第三方许可

`web/vendor/pele.mjs` 是官方 `pele@1.7.0` ESM bundle 的未修改副本，MIT 许可
（含 Lucide ISC 图标说明），来源与校验值见 `web/vendor/README.md`。

## 已知边界（MVP）

- 独立页面渲染，不含 Chat 内嵌 SVG、历史 Mermaid 自动解析、导出或持久化。
- SVG 以图片呈现：图内链接与文字选择不可用；渲染字体在点击时固定。
- 图表类（pie 等）使用 Pele 默认 series 色，不映射宿主强调色。
- 真实 BYOP WebView 与窄窗走查由协调方在构建版本中验收（docs/P27-validation.md）。
