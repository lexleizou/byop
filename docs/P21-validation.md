# P21 扶摇筛选作用域看板验证

日期：2026-10-29。基线：`be44554`。应用：`0.18.7`。插件：`local.lightweight-table` 0.5.0。

## 已实现

- `table.dashboard` 接受与 `table.query` 相同的 `search`、`filters` 参数；合计、状态、需要处理和构成都按匹配记录计算。
- 当当前筛选落在还款状态、分类或资金来源维度时，该维度保留搜索范围内的兄弟分段，并标记当前项；其他维度按匹配记录收窄。
- 汇总区改为范围栏、三列看板、状态健康条、需要处理列表和构成列表；旧“常用查询”按钮行已移除。
- 范围栏显示当前条件、搜索文本和匹配数；看板可收起并把收起状态保存到 `localStorage` 的 `fuyao.dashboardCollapsed`。
- 1200 宽度以下使用窄布局，构成默认收起并提供展开/收起按钮；状态、分类和资金来源按钮继续复用现有预设。
- 状态和构成按钮应用预设；只有需要处理列表的记录行打开现有详情抽屉。看板读取失败只显示 toast，表格保持可用。
- 新增中英文看板文案，中英文键集一致；插件和应用版本已按要求提升。

## 自动检查

- `plugins/lightweight-table` `npm run check`：43 通过，0 失败。
- 仓库根 `gofmt -l .`：无输出。
- 仓库根 `go vet ./...`：无输出。
- 仓库根 `go build -o byop .`：成功，无输出。
- 仓库根 `go test ./... -count=1`：`Go test: 522 passed in 3 packages`。
- 仓库根 `git diff --check`：无输出。
- 仓库根 `go run github.com/egoist/mygo/cmd/mygo i18n check .`：`820 messages in en, zh-Hans`。
- WebView 变量审计：`plugin_webview.go` 发布 25 个 `--byop-*` 变量，插件 Web 目录引用 24 个；缺失数为 0。

## 真实窗口走查

使用 `~/AgentScratch/20261029-byop-p21-fuyao-dashboard/` 的隔离配置和 12 行合成数据，不读取或写入正式 `~/.byop`。

- Dark 宽屏（WebView 约 1311px）：三列看板、状态健康条、需要处理、构成和预设切换正常，截图见 `docs/screenshots/p21-fuyao-dashboard-dark.png`。
- Light 宽屏（WebView 约 1311px）：三列看板、完整英文/中文金额和构成行正常，截图见 `docs/screenshots/p21-fuyao-dashboard-light-wide.png`。
- Light 窄屏（WebView 约 811px）：状态条独占首行，主金额区与需要处理位于第二行；构成默认收起，截图见 `docs/screenshots/p21-fuyao-dashboard-light-narrow.png`。
- 高对比：P2 修正后重新走查，状态文字使用宿主已发布的 `--byop-text`，危险/成功状态文字使用已发布的语义文字变量；结构、状态文字、危险/警告颜色和边框可读，截图见 `docs/screenshots/p21-fuyao-dashboard-highcontrast.png`。
- 点击“逾期”后合计为匹配记录口径，状态兄弟分段保留并变淡；再次点击当前状态返回“全部记录”。分类筛选同样保留分类全部分段。
- 搜索文本进入范围条件；需要处理按钮应用 `due_30`，行点击打开现有详情抽屉。
- 看板收起后显示剩余应还、合同本金、实际已付和逾期笔数；刷新页面后 `localStorage` 保持收起状态。
- 修正后检查了 `--byop-text` 的实际计算色；强调状态文字在 Light、Dark 和高对比中均使用主题正文色，Web 目录引用的全部 `--byop-*` 变量都在宿主 bridge 发布列表中。
- 看板重绘和 `loadDashboard` 完成后，已聚焦的状态按钮仍保持焦点；当前范围为已结清且没有处理项时，`due_30` 标题按钮仍因整表计数非零而可用。
- 临时修改 DOM 搜索框但不触发查询时，`loadDashboard` 仍使用 `state.result` 的搜索与筛选快照，范围栏继续与底栏匹配数一致。
- 模拟 `table.dashboard` 失败时 toast 出现，表格仍保留 12 行；模拟两个乱序看板响应时只采用较新的响应。
- 英文界面检查了完整金额格式和长分类/资金来源标签；英文键集与中文一致。

> 复审修正（2026-10-29）：初版验证文档把高对比状态文字写成已验证可读，但 Opus 复审发现 `--byop-accent-text` 不由宿主 WebView bridge 发布，原结论作废。现在强调状态文字改用 `--byop-text`，并以本轮重拍截图重新验证。

## 未验证与已知限制

- 未使用客户真实台账；所有走查数据均为合成数据，详情抽屉中的源日期显示为 `—` 是合成种子没有 v4 来源元数据所致。
- `scope.total_remaining_due` 是为 hero 的“占全部 N%”补充的整表剩余应还字段；规格列出的 `scope` 字段之外增加了这一支持字段。
- 当前查询栏仍保持 P20 的单筛选控件；后端支持多个筛选，但页面每次只产生一个筛选，点击另一维度会替换当前筛选，而不是叠加。
- 预设按钮不再清空搜索文本；因此点击预设会保留当前搜索条件。
- `no_plan_count` 的搜索范围焦点规则只在筛选列表恰好是单个 no-plan `eq` 筛选时生效。
- Opus 复审 PASS（无 P0–P2）。保留 P3：新查询结果先于看板数据返回时，按下状态、变淡与范围文字会短暂沿用上一份看板数据，约一次请求后对齐；状态焦点测试的搜索词覆盖全部合成行，未真正区分搜索范围与整表范围（实现经读码确认正确）。
- 表格每行“详情”按钮原引用宿主未发布的 `--byop-accent-text`，与看板同一原因改为正文色；这是看板之外的一处顺带修正。
