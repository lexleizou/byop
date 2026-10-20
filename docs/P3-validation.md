# BYOP P3.3a Validation

验证日期：2026-10-20

## 范围

P3.3a 只补充 Pi 1.3.0 运行时诊断与停止语义。它不改变 Chat 布局、14 px
正文、右侧任务栏默认收起状态，也不引入会话历史、插件执行或 WebView。

## 实现

- 解析 `queue_update`、`compaction_start/end`、`auto_retry_start/end`、
  `agent_end` 与 `extension_error`；
- steering 和 follow-up 队列数量进入任务栏运行时诊断；
- 上下文压缩显示触发原因和压缩前后估算 token，不展示压缩摘要正文；
- 自动重试显示 attempt、maxAttempts、延迟和错误摘要；
- Composer 状态优先级为 Stopping、Retrying、Compacting、Pi is working；
- provider 错误不进入助手永久文字块，失败轮次显示通用错误说明；
- `agent_end(willRetry:true)` 继续维持运行态，只有 `agent_settled` 结束轮次；
- 重试等待中 Stop 先调用 `abort_retry`，随后调用 `clear_queue` 和 `abort`；
- 扩展错误保存扩展文件名、事件和错误摘要，与 JSONL 解析错误分开；
- 重连、进程退出和新提示会清理上一轮的临时诊断状态。

## 自动化验证

| 检查 | 结果 |
|---|---|
| `gofmt -w *.go` | 通过 |
| `go build -o byop .` | 通过 |
| `go vet ./...` | 通过 |
| `go test ./...` | 11 个单元测试通过 |
| 真实 Pi：普通回复、Stop、Tool、Timeline | 4 个测试通过 |
| 真实 Pi：手动 Compact RPC | 通过 |
| 设计 token JSON 解析 | 通过 |

单元测试覆盖新 JSONL 事件字段、运行时状态投影、扩展错误分类、
`willRetry` 不提前完结，以及 provider 错误不写入助手正文。

## 限制

- 自动重试由真实 provider 错误触发，本轮没有故意制造远端故障；
- 临时诊断已在任务栏和 Composer 接通，尚未增加独立运行日志页；
- 手动压缩目前只验证 RPC，未增加面向用户的压缩按钮；
- Dark 主题、窄窗口和 Linux / Windows 本轮未重新截图验证。

# BYOP P3.2a Validation

验证日期：2026-10-20

## 范围

P3.2a 只替换 Chat 的阅读区、时间线、工具行和输入框，使其与 Onevra Chat 原页面
一致。P3.2 的 Pi 事件处理、有序内容块、Stop 和重连逻辑保持不变。右侧任务栏
继续默认收起，正文继续使用 14 px。

## 实现

- 直接依赖 Onevra `source v0.2026.13`（提交 `ece5cd9`）的 Chat 组件；
- 阅读区使用 `FloatingConversation`，离开底部时显示“Back to latest”；
- 每轮回复使用 `Turn`：运行中显示“Working · 耗时”，结束后折叠为“Worked for 耗时”，最终文字保持可见；
- 连续工具合并为 `ActivityGroup`，运行中标题显示最新动作，结束后显示“Ran N tools”；
- 单个工具使用 `Activity`，等宽详情展示长参数、结果或实时输出、状态和耗时；
- 输入框使用 `GlassComposer`，保留模型标签、附件按钮、Pi 状态和 Stop；
- 输入文字覆盖为 14 px；Onevra 的 12.5 / 13.5 px 辅助标签保持原值；
- 用户手动展开或折叠工具后，只有工具状态变化才会重新同步默认展开状态；
- 修复导航按钮内容被居中的回归，展开状态下图标与文字恢复左对齐。

## 自动化验证

| 检查 | 结果 |
|---|---|
| `gofmt -w *.go` | 通过 |
| `go build -o byop .` | 通过 |
| `go vet ./...` | 通过 |
| `go test ./...` | 8 个单元测试通过 |
| 真实 Pi：普通回复、Stop、Tool | 通过 |
| 真实 Pi：Timeline | 首次因模型把两个命令合并而未产生两次独立工具调用；单独重跑通过 |
| 设计 token JSON 解析 | 通过 |

## 界面走查

![P3.2a Onevra Chat](screenshots/p3-2a-onevra-chat.png)

截图使用 `BYOP_DEMO_TOOLS=1 BYOP_PI_CONNECT=0` 的隔离演示数据，不含真实会话 ID、
本地项目路径、凭据或客户内容。视觉走查确认右侧任务栏默认收起、已完成工具组
折叠、运行中工具组展开、Working 行持续计时、输入框为 Onevra 玻璃样式。

## 限制

- 附件按钮和模型标签仍为展示态，附件与模型切换属于后续阶段；
- 尚未逐像素对照 Onevra 网页预览，仅按组件源码和截图进行人工走查；
- Dark 主题、窄窗口和 Linux / Windows 本轮未截图验证。

# BYOP P3.2 Validation

验证日期：2026-10-20

## 范围

P3.2 将 Chat 的助手消息改为单一有序时间线，保留“文字 → 工具 → 文字”
的真实出现顺序。它不引入会话历史回放、插件安装、诊断面板或 WebView。
Pi 继续拥有模型、凭据、会话、工具和扩展。

## 实现

- 助手消息使用有序 `chatBlock` 列表，不再分别保存整段文字和工具卡数组；
- 新文字到达时更新末尾文字块；若末尾是工具，则在其后新建文字块；
- `tool_execution_start` 在当前位置插入工具块；
- `tool_execution_update/end` 按 `toolCallId` 原位更新，不移动工具卡；
- Running 工具自动展开，Completed、Failed、Cancelled、Interrupted 自动折叠；
- Stop、进程退出和重连扫描时间线中的所有 Running 工具，并原位改为终态；
- 右侧任务栏优先显示最新 Running 工具，否则显示最近活动的工具；
- 正文和人类可读工具内容继续使用 14 px，工具元数据使用既有辅助字号；
- 增加隔离演示数据，覆盖“开头文字 → 已完成工具 → 中间文字 → 运行中工具”。

## 自动化验证

| 检查 | 结果 |
|---|---|
| `gofmt -w *.go` | 通过 |
| `go build -o byop .` | 通过 |
| `go vet ./...` | 通过 |
| `go test ./...` | 8 个单元测试通过 |
| 真实 Pi：普通回复、Stop、Tool、Timeline | 4 个集成测试通过 |
| 设计 token JSON 解析 | 通过 |

单元测试固定验证以下投影顺序：

```text
text:one
tool:call_a
text:two
tool:call_b
text:three
```

同时验证 `call_a` 完成后在原位置折叠，而 `call_b` 运行时保持展开。

真实 Pi 时间线测试要求 Pi 分开执行两次 `bash`：

1. 第一次输出 `BYOP_ORDER_ONE`；
2. 两次工具之间发送独立文字 `BETWEEN_TOOLS`；
3. 第二次输出 `BYOP_ORDER_TWO`；
4. 最后发送独立文字 `BYOP_ORDER_DONE`。

测试验证两个工具使用不同且正确的 `toolCallId`，中间文字位于两张工具卡之间，
最终文字位于第二张工具卡之后。

## 界面走查

[P3.2 有序会话时间线](screenshots/p3-2-timeline.png)

截图使用 `BYOP_DEMO_TOOLS=1` 的隔离演示数据，只包含虚构提示、安全的文件与
Git 状态摘要、固定耗时和一个运行中工具。截图不含真实会话 ID、模型请求、
本地项目路径、凭据或客户内容。

视觉走查确认：

- 文字段与工具卡严格按时间线排列；
- 760 px 内容列在右侧任务栏展开时保持对齐；
- 已完成工具显示摘要并折叠；
- 运行中工具展开显示参数，并同步到任务栏；
- Light 主题、14 px 正文、状态色和图标槽位保持一致。

## 限制

- 参数和结果详情限制为 1,200 个 Unicode 字符；
- 工具活动只投影当前临时会话，尚未实现 Pi 会话历史回放；
- 多个文本 content block 在单个 Pi assistant message 内仍按 snapshot 拼接；
- 富 Markdown、图片内容、retry/compaction 诊断仍属于后续阶段；
- Linux 和 Windows 尚未在本轮验证。
