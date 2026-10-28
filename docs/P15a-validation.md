# P15a 轻量表格最小链路验证

日期：2026-10-28。设计基线：轻量表格插件 v0.5。开发路由：auto/deepseek-v4.1-flash。

## 范围

本阶段验证 BYOP 现有 Pi RPC 启动入口 → 显式会话桥 → 本地 stdio MCP → 独立 SQLite。仅覆盖消息来源关联、防重复写入、版本冲突与 WAL；不实现完整表格 UI、文件导入导出、保存视图或财务计算。

## 数据来源

用户先指定 18804 个人实例。通过只读连接确认 debts 为 0，并核对 API 所连接数据库确实是该个人实例；随后用户明确改为 18788 银行类真实数据。

18788 只读提取条件为 category=financial 且 funding_source=bank，共 16 条记录。仅映射产品名、机构、分类、资金来源、合同本金与原始状态。本金从整数分精确转换为十进制字符串；没有直接对应字段的待还金额和还款日为 null，不推算余额。源数据库查询连接使用 default_transaction_read_only=on，未向两个源实例写入数据。

真实导出、seed、SQLite 副本及会话日志只保存在本机 AgentScratch 的本阶段目录；公开仓库测试只使用虚构银行数据。真实样例的姓名、机构明细、金额和源记录 ID 不写入本报告。

## 实际验证

| 检查 | 实际结果 |
|---|---|
| 真实数据 seed | 18788 银行类 16 条；SQLite 值与只读导出逐条一致；待还金额/还款日保持 null |
| BYOP → Pi → MCP → SQLite | 现有 startPiClient 启动真实 Pi 1.4.0，使用 magpie/auto/deepseek-v4.1-flash；最终 E2E 通过（11.22 秒） |
| 直接调用防重复 | 同一用户消息内连续两次完全相同新增，只有 1 个 insert 回执 |
| 嵌套调用与真实银行记录 | 测试专用工具经 ctx.executeTool 调用 MCP：查询一条原银行记录，修改 repayment_status，再恢复原值；2 个 update 回执，revision 增加 2 |
| 来源关联 | 最终会话有 2 个 Pi 普通 user 条目；3 个回执的 source_session_id 和 source_message_ids 与该会话持久化条目核对一致 |
| 原银行值保留 | 最终副本中的原 16 条映射值全部保持；另有两次验证尝试各新增的 1 条明确标记的合成记录，共 18 行 |
| WAL 并发与锁等待 | WAL 模式；两个独立 MCP 进程对同一来源命令只形成一次写入和回执；竞争写锁约 3 秒后返回 busy，不留下回执 |
| 最终 Node 定向测试 | 16/16 通过，覆盖来源边界、上下文省略、幂等、版本冲突、dry-run、MCP 和双进程锁等待 |
| Go 现有检查 | go vet ./...、go build、go test ./... -count=1 通过；最后源码修正只涉及 Node 模块，Go 部分未再改动 |
| 实现检查 | 开发执行的 .mjs 语法检查通过；最终 gofmt 无输出、git diff --cached --check 通过 |

初次真实 E2E 的直接 MCP 新增去重已通过，嵌套验证脚本因少解析一层 Pi 包装而失败。DeepSeek 修正测试脚本后，最终 E2E 已通过；没有通过重置数据库掩盖第一次记录。测试副本的原银行字段全部恢复，源 PostgreSQL 从未被写入。

最终来源修正包括：正常最终回答后的 custom follow-up 不沿用旧来源；context_edit 省略的 user 不退回更早来源；custom 启动后实际投递的新 user 可以建立来源。查询的版本、记录、计数和汇总改为同一同步短读事务，不跨模型等待。

本机证据位于 AgentScratch/20261028-byop-table-link：seed-validation.json、e2e.log（首轮失败）、e2e-final.log、final-evidence.json、node-tests-final.log 和 go-tests.log。真实源值和会话内容不复制到公开报告。

## 独立审核

审核路由 auto/claude-opus-5-5 完成核心代码审核与修正后的增量复核，结论无剩余 P0–P2。开发路由 auto/deepseek-v4.1-flash 完成最终回答/context_edit 来源处理、custom 后新 user 来源及查询短读事务修正；审核方实际运行来源测试 10/10。完整报告在本机过程目录 review.md。最终真实 E2E 由协调方运行并核对，不能与审核方的静态结论混为一项。

## 交付边界

本阶段提供 table_describe、table_query、table_change、table_history 四个工具和 opt-in 的 BYOP_PI_EXTENSIONS 启动接点；固定银行字段只是本阶段验证表，不是完整 v0.5 产品。未实现完整表格 UI、列管理、contexts、导入导出或撤销；未改动已安装 BYOP 应用，也未写全局 Pi MCP 配置。

最小链路通过现有 BYOP RPC 适配器调用真实 Pi，未做正式应用的鼠标界面走查，不将原扶摇数据库作为插件后端。运行态使用已有 Node 进程能力；完整插件生命周期 UI 与 Node 文件范围参数不是本阶段交付，不声称它们已经验证。

运行方法见 plugins/lightweight-table/README.md。数据库、源导出、真实 seed 和运行日志留在本机过程目录；公开代码只含合成夹具。
