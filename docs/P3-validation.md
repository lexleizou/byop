# BYOP P3.1 Validation

验证日期：2026-10-20

## 范围

P3.1 只扩展外部 Pi CLI 的运行事件和原生呈现，不引入插件安装、插件运行
或 WebView。Pi 继续拥有模型、凭据、会话、工具和扩展。

## 实现

- 解析 Pi 1.3.0 的 `tool_execution_start`、`tool_execution_update` 和
  `tool_execution_end`；
- 用 `toolCallId` 关联同一次工具执行的参数、部分结果、最终结果和耗时；
- Chat 使用原生工具卡展示名称、参数摘要、结果摘要、耗时和状态；
- 工具卡可展开查看完整参数和受长度限制的结果详情；
- 支持 Running、Completed、Failed、Cancelled、Interrupted；
- Stop 在 `agent_settled(aborted:true)` 后取消仍运行的工具；
- Pi 进程退出和手动重连把仍运行的工具标记为 Interrupted；
- 任务栏的 Current run 卡片显示当前活动工具及其状态；
- 正文保持 14 px，工具元数据沿用更小的辅助字号和 Onevra 语义色。

## 自动化验证

| 检查 | 结果 |
|---|---|
| `go build -o byop .` | 通过 |
| `go vet ./...` | 通过 |
| `go test ./...` | 7 个单元测试通过 |
| 真实 Pi：普通回复、Stop、Tool | 3 个集成测试通过 |
| 设计 token JSON 解析 | 通过 |

真实 Pi 工具测试让 Pi 调用一次 `bash` 并输出固定标记
`BYOP_TOOL_OK`。测试验证：

1. 收到成功 prompt 响应；
2. 收到带非空 `toolCallId` 的 bash start；
3. 收到同一 ID 的 bash end；
4. end 是非错误状态，结果包含固定标记；
5. start 位于 end 之前，end 位于 `agent_settled` 之前。

## 界面走查

[P3 工具卡与任务栏截图](screenshots/p3-tool-cards.png)

截图使用 `BYOP_DEMO_TOOLS=1` 的隔离演示数据，只包含：

- 虚构会话 `Review the sample project.`；
- 安全的 `git status --short` 完成示例；
- 安全的 `README.md` 运行中示例；
- 固定 `84 ms` 耗时；
- 当前活动的 `read · Running` 任务。

截图不含真实会话 ID、模型请求、本地项目路径、凭据或客户内容。视觉走查确认
工具卡与 Chat 的 760 px 内容列对齐，状态色、耗时、摘要和展开箭头清晰，任务栏
同步显示当前活动工具。

## 限制

- 参数和结果详情当前限制为 1,200 个 Unicode 字符；超出后在卡片内标记截断；
- 部分工具不会发 `tool_execution_update`，界面仍通过 start 进入 Running；
- 工具活动只投影当前临时会话，尚未实现历史会话回放；
- 富 Markdown、图片内容块和工具结果链接仍属于后续阶段；
- Linux 和 Windows 尚未在本轮验证。
