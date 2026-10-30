# P22 插件提供的模型版本胶囊验证

日期：2026-10-31。基线：`a0dd9af`。应用：`0.18.7`。插件：`local.model-release-labels` 0.1.0。

## 已实现

- manifest 新增数据型贡献点 `contributes.modelLabels`：每条含贡献 `id`、`badge` 和 `match`
  （`providers` 至少一项，`modelId` 或 `modelName` 至少一项）。校验覆盖 id 规则、
  贡献 id 重复、badge 长度与空白、provider 合法性与重复、模型字段缺失。
- `pluginRegistry.modelLabelBadge(provider, modelID, modelName)` 按注册表顺序取第一条命中；
  匹配大小写不敏感、去首尾空格；声明的 `modelId` 还等于真实 id 最后一个 `/` 之后的部分，
  因此 `deepseek-flash` 命中 Pi 报告的 `opencode-go/deepseek-flash`。
- 后注册的重复命中不覆盖前者：插件保持已注册状态，另在注册表问题里记录
  `model label "x" overlaps <plugin> contribution "y"`，Plugins 页面按现有问题列表显示。
- 渲染只在 composer 的当前模型控件：`chat_view.go` 的 `composerModelChip`
  （含只读与重试回读分支）与 `customModelPicker` 的当前模型 chip，位置在模型名之后、
  下拉箭头之前。胶囊几何复用既有 token：`v12ChipRadius`、`fontSmall`、`t.Surface`、`t.Text`，
  不加边框、不加动画；无命中时不产生任何节点。
- 示例插件 `plugins/model-release-labels/`：manifest 声明 DeepSeek Flash 两条 `1017`
  胶囊、no-op `plugin.mjs`（插件不声明命令与激活事件，按 P4.1 懒启动规则进程不会启动）、
  README 写明匹配规则、添加方法和“胶囊是人工声明、不可核验、不会过期”的限制。
- 未新增用户可见宿主文字，语言包与 `locales/catalog_gen.go` 未改动。

## 自动检查

- `gofmt -l .`：无输出。
- `go vet ./...`：无输出。
- `go build -o byop .`：成功。
- `go test ./... -count=1`：`Go test: 546 passed in 3 packages`（P21 基线为 522）。
- `git diff --check`：无输出。
- `node --check plugins/model-release-labels/plugin.mjs`：通过。
- `python3 -m json.tool plugins/model-release-labels/plugin.json`：通过。
- 新增 manifest 校验测试：badge 空、首尾空格、超过 12 字符；`providers` 缺失、含空格、
  大小写重复；`modelId` 与 `modelName` 同时缺失；label id 非法与同一 manifest 内重复。
- 新增匹配测试：provider 命中/缺失、`modelId` 不符、`modelName` 作为通配、
  渠道前缀 id 命中（`opencode-go/deepseek-flash`）、不同 release 的前缀 id 不命中。
- 新增注册表测试：两条重叠声明只有插件 id 排序靠前的生效并记录 1 条冲突问题；
  未注册（`pluginApi` 不兼容）插件的声明不参与匹配；未命中模型不返回胶囊。
- 新增出货清单回归测试：读取 `plugins/model-release-labels/plugin.json`，断言本机 Pi 目录里
  `magpie` 下的 `deepseek/deepseek-flash`、`opencode-go/deepseek-flash`、
  `workbuddy-ai/deepseek-flash`、`auto/deepseek-v4.1-flash`、`opencode-go/deepseek-v4.1-flash`
  都显示 `1017`，未声明的模型无胶囊。
- 新增 composer 布局测试（`ui.NewTester` 离屏渲染真实 chat view）：插件命中时 composer 行内
  出现胶囊，高度不超过 26 DIP 控件高度、位于上下文容量 chip 之前且不超出视口；
  无命中时不出现胶囊，模型名仍完整渲染。

## 真实窗口走查

未完成截图与目视走查。原因：本会话进程没有 Screen Recording 权限
（`screencapture` 返回 `could not create image from display`），`osascript` 驱动 System Events
取窗口坐标被拒并超时，`orca computer` 的 CLI 入口在本机不可用。

已启动开发实例供目视确认：`./byop`（仓库构建产物），pid `56320`，插件已安装到
`~/.byop/plugins/model-release-labels`。待确认项：模型名后是否出现 `1017` 胶囊、
chip 是否仍为 26 DIP 高、模型名是否完整、Light/Dark 与窄窗口下是否可读、
Plugins 页面是否列出该插件与贡献、停用插件后胶囊是否即时消失。

## 未验证与已知限制

- 未验证：真实窗口目视走查与截图（见上）、Dark 与高对比、窄窗口极端宽度、
  Plugins 页面的冲突问题显示、停用/重扫插件后的即时刷新、Linux、Windows、辅助功能。
- 胶囊是插件里的静态声明，宿主不核验、不会自动过期。DeepSeek 把 V4.1 Flash 升到 1017
  快照时沿用了 `deepseek-flash` 模型 id，API 任何一层都不暴露快照
  （2026-10-18 实测：响应无 `system_fingerprint`，模型自述不可信）；DeepSeek 自己的
  同名插件也把它标为 `Temporary`。胶囊只能当作“我配过哪个版本”的提示。
- 首批只覆盖 DeepSeek Flash 两条 id；量化或精度标签（如 `IQ3_S`、`nvfp4`）与自建端点模型
  需要后续在前缀匹配上再加规则。
- 胶囊只出现在 composer 的当前模型控件，模型选择器下拉列表行内不显示（列表行用通用
  `chat.Option`，改动会波及共用组件，明确留到后续阶段）。
- 没有 Settings 开关，停用只能停用或卸载插件。
- 本机 `~/.byop/plugins/model-release-labels` 是走查用的安装副本，不进仓库；仓库源码在
  `plugins/model-release-labels/`。
