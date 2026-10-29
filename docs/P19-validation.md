# P19 会话产出文件卡片与内联预览

日期：2026-10-29。基线：`9481b22`。应用：`0.18.4`。插件：`local.lightweight-table` 0.4.1。

## 已实现

- 会话正文中单独成段的本地产出文件改为文件卡片：只显示文件名，路径收进提示与菜单。识别接受裸路径、`file://` 链接、单个 Markdown 链接和句末标点，含其他文字的段落、目录、相对路径与不存在的文件保持原文。
- 卡片菜单同时绑定左键与右键：打开文件、在文件夹中显示、复制路径。
- HTML/HTM 文件卡片下方内联渲染页面（受限高度 380px，权限全部关闭），最新回答自动展开，较早回答默认折叠，可用“预览／收起预览”切换；同时最多存活 6 个 WebView，转录重绘后不再出现的预览立即销毁。
- 内联预览只在本轮结束后自动展开：HUD 标记当前逻辑轮，运行中不预览，避免流式重绘反复创建页面。
- 模型选择器每行的品牌图标与选中图标对齐到模型名所在行，两行行高不再让图标居中于整行。
- Pi CLI 状态浮层改为 292px 固定宽度，“重新连接”按钮在 1100×560 下完整可见、不溢出。
- 插件 0.4.1：详情抽屉的枚举值按页面语言显示（分类、资金来源、产品类型、计划状态、方式、利率周期、计息基础、期次状态、付款影响、分配方式），未知值保留源文本；新增当前计划的期次列表。宿主新增 8 个界面文案键，插件语言包各补 38 个键。
- 扶摇 SQLite 数据、插件设置与插件级热重载行为未改动。

## 自动检查

- `go test ./... -count=1 -timeout 3m`：521 通过（含 3 个 P19 用例：裸路径识别、卡片只显文件名且菜单齐备、浮层按钮不溢出）。
- `plugins/lightweight-table` `npm run check`：40 通过，含 `node --check web/app.js` 与界面契约、语言包完整性检查。
- `mygo generate` 重新生成 `locales/catalog_gen.go`；`mygo i18n check .`：`en` 与 `zh-Hans` 各 820 条，无错误。
- `gofmt -l .`、`go vet ./...`、`go build -o byop .`、`git diff --check`：无输出。

## 真实窗口验证

在隔离配置目录中构建并运行 `qa-byop`，使用真实 Pi CLI 1.4.0（DeepSeek V4.1 Flash）与隔离的会话目录、插件目录和 SQLite 副本，正式 `~/.byop` 未被写入。

- 真实一轮对话只回一行 HTML 路径，转录渲染为文件卡片；本轮结束后预览自动展开，`CapturePage`（WebView）取回的是页面真实内容。
- 卡片只显示文件名；HTML 卡片带“预览”按钮，非 HTML 文件不带预览；预览收起后存活 WebView 归零。
- BYOP Light、BYOP Dark、J Agent 与 1100×560 均已走查：卡片使用当前主题的表面、边框与文字色，浮层“重新连接”按钮完整可见。
- 模型选择器图标与模型名首行对齐（含选中勾）。
- 扶摇页面加载 0.4.1：真实数据副本显示 49 行，中文标签与枚举值无英文残留（抽样多行详情抽屉，`bank`/`financial`/`overdue` 等原文泄漏计数为 0），详情抽屉四个分区与期次列表正常；可提交截图改用合成快照。
- 截图：`docs/screenshots/p19-file-card-light.png`、`p19-html-preview-content.png`、`p19-model-picker-dark.png`、`p19-pi-popover-1100x560-light.png`、`p19-fuyao-table-dark.png`、`p19-fuyao-detail-dark.png`。

## 扶摇 0.4.1 发布与数据核对

- 发布前把原 0.4.0 插件目录完整备份到 `~/AgentScratch/20261029-byop-p19/plugin-backup-0.4.0-20261029-131818/`；发布排除 `test/`，发布目录与仓库内容逐字节一致（`plugin.json`、`web/app.js`、两个语言包 SHA-256 相同）。
- 正式库 `~/.byop/plugin-data/local.lightweight-table/tables.sqlite` 复核：主债务 49、还款计划 8、计划期次 108、付款 10、分配 6、事件 49、导入元数据 1，与 `~/AgentScratch/20261029-byop-fuyao-full-import/source/` 的导出计数一致；`journal_mode=wal`；operations 与 contexts 为 0。数据未重新导入，也未改动正式账本。
- 应用 0.18.4 已安装到 `/Applications/BYOP.app`，`CFBundleShortVersionString` 与 `codesign --verify --deep --strict` 通过，可执行文件与 `dist` 产物 SHA-256 相同；旧版本移入 `~/.Trash`。

## 已知限制

- MyGo 对内容窗口截图不包含 WebView 像素，因此整窗截图中预览区为空白；页面内容以 WebView 自身截图为证，界面几何与卡片样式以整窗截图为证。
- 卡片菜单的点击动作在 UI 契约测试中验证（三项均在），真实窗口的打开/显示/复制动作未逐项点击。
- 内联预览跟随页面自身配色，暂不强制反色为应用主题；深色主题下浅色页面仍显示为浅色画布。
- 合成夹具中的 `installment`、`lump_sum` 产品类型没有对应文案键，按设计保留源文本；真实数据的五种产品类型取值均有中文标签。
