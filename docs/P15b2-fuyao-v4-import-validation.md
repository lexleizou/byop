# P15b.2 扶摇 v4 完整快照导入

日期：2026-10-29。范围仅限 `local.lightweight-table` 插件 0.4.0；BYOP 应用版本保持不变。

## 已实现

- 本地 SQLite 表结构扩展到 v4 主债务字段，导入或派生字段在列配置中标记为只读。
- 主债务 `row_id` 使用 v4 源 UUID，并通过 `rows.source_record_id` 唯一索引保证幂等。
- `import-v4` 读取并校验七个数据文件和 `manifest.json` 的计数、SHA-256、债务 ID 集合及关系外键。
- 六类关联数据写入独立表：还款计划、计划期次、付款、付款分配、债务事件和导入元数据。
- 替换已有表要求 `--replace-unmodified`，且 operations 与 contexts 均为 0。CLI 先校验快照，再用原始数据库连接检查历史并创建迁移前备份，最后才打开会迁移 schema 的 `TableStore`。
- 主债务、关联数据和元数据在同一 `BEGIN IMMEDIATE` 事务内替换；任何计数或外键校验失败都会回滚。
- WebView 固定显示 v4 十列，空值为 `—`；新增只读详情抽屉。
- MCP 新增 `table_debt_details`，只接受稳定 `row_id`，返回完整当前计划期次、付款和事件，不开放任意 SQL。

## 合成数据验证

仓库测试仅使用合成 v4 文件，不包含真实账本值。覆盖：

- 两个合成债务及全部六类关联数据的计数、精确分值、稳定行 ID 和外键。
- API/raw 债务 ID 不一致和关联外键缺失时拒绝导入。
- 现有表无标记时拒绝替换；有操作历史时拒绝替换。
- 替换前备份文件命名、重复导入不产生重复行、导入元数据只保留一条。
- 手动年化优先、计算年化次之、原始利率加 `/周期` 的回退格式，统一显示为 `9.00%／年` 一类中文格式。
- 导入或派生列经 `table_change` 修改时返回 `read_only_column`。
- WebView 使用 `table.detail`，详情抽屉不含删除控制，主表代码不再包含“未导入”占位逻辑。
- 旧 v1 SQLite 表结构原位迁移到 v2 列定义，并增加 `source_record_id`。
- 替换前备份保留旧 `format_version=1`、`schema_revision=1` 和原记录数；当前计划排除 `replaced`，按 v4 的生效日、创建时间和 ID 规则选择。
- 合成与真实副本均覆盖 49+8+108+10+6+49 条结构；真实副本的精确字段和金额与 API 导出逐条一致，外键检查为空。

真实导出以只读方式取得：债务 49、计划 8、计划期次 108、付款 10、分配 6、事件 49；七个数据文件的 SHA-256 均与源 manifest 一致。真实导出只保存在受限的 AgentScratch 目录，没有把真实值写入仓库或测试。

正式导入前先创建原始库副本；CLI 又依据 operations=0、contexts=0 创建了迁移前备份。备份保留 `format_version=1`、`schema_revision=1` 和原 16 行，证明回滚点未被迁移覆盖。正式库导入后：

- 主表 49 行，行 ID 与 v4 债务 UUID 一致；合同本金、实际到账、剩余应还、状态和计划字段与 API 导出逐条一致。
- 关联表为 8 / 108 / 10 / 6 / 49 条，导入元数据 1 条；外键检查为空，journal_mode=wal。
- workbook 为“扶摇 v4 快照”，table 为“扶摇负债”，schema_revision=2、data_revision=2。
- 插件 0.4.0 已发布到 `~/.byop/plugins/local.lightweight-table`；原插件目录已备份到 AgentScratch。
- 正式应用主表显示 49 条记录、匹配 49 条，合同本金合计 10,669,516.60；有计划的记录显示 `3.10%／年`、计划状态和下次应还日期/金额。
- 真实 Pi 会话只调用 `table_describe`，返回 source_date=2026-10-29、source_instance=fuyao-v4-18788、live_connected=false 和 49 条记录。
- 对派生字段 `actual_received` 发起一次带有效 Agent 来源标识的更新，服务端返回 `read_only_column`，operations 保持 0。

## 检查

项目插件检查：

```sh
cd plugins/lightweight-table
npm run check
```

插件检查共 39 项通过。原生 MyGo WebView 已用真实数据副本验证 49 行主表、无“未导入”占位、详情抽屉和关联事件；MCP `table_debt_details` 也已对真实副本成功返回计划与来源元数据。

可公开的界面截图仅使用合成快照：

- [合成主表](screenshots/p15b2-fuyao-synthetic-table.png)
- [合成负债详情](screenshots/p15b2-fuyao-synthetic-detail.png)
