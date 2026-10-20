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
