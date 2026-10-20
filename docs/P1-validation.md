# BYOP P1 原生外壳验证

验证日期：2026-10-20
范围：MyGo 原生外壳、统一字号、页面切换、主题、任务栏、Composer 和最小窗口。
不在范围：Pi RPC、模型请求、插件安装执行、WebView、数据迁移。

## 字号层级

| 层级 | 字号 | 典型用途 |
|---|---:|---|
| micro | 10 px | 状态胶囊、键盘提示 |
| caption | 11 px | 页头副标题、次级说明 |
| small | 12 px | 按钮、工具卡、列表详情 |
| body | 14 px | 会话正文、导航、输入框、设置和页面说明 |
| body large | 15 px | 任务栏与强调正文 |
| UI large | 16 px | 应用栏和页面头部标题 |
| title | 18 px | 紧凑分区标题 |
| page title | 30 px | 工作区页面标题 |
| display | 32 px | Chat 空状态标题 |

MyGo 主题默认 `FontSize` 和产品正文 token 都是 14 px。源码使用 `theme.go` 中的统一常量，页面没有继续散落任意字号。设计文件中的上游 13 px 只作为来源记录，不再驱动 BYOP 正文。

## 实际走查

| 检查项 | 结果 |
|---|---|
| Chat、Plugins、Library、Document、Automations、Settings 六页切换 | 通过 |
| Light、Dark、System 主题与语义色切换 | 通过 |
| 导航 200 px / 56 px 展开收起 | 通过 |
| 右侧任务栏默认收起、打开和关闭 | 通过 |
| Composer 输入、提交占位反馈和通知关闭 | 通过 |
| 1180 × 760 默认窗口 | 通过 |
| 760 px 最小宽度下 Chat 输入区完整显示 | 通过 |
| 提示条出现时 760 px 最小高度窗口 | 底部空间会收紧；提示可关闭，默认状态不受影响 |

本次没有把 P1 的外壳占位操作描述成真实能力。P2 已单独补上外部 Pi CLI 的连接、发送、流式与停止闭环。

## 构建验证

在 `implementation/byop-prototype/` 目录执行：

```sh
gofmt -w *.go
go build -o ./byop-prototype .
go test ./...
jq empty design-system/pi-workbench/tokens.extensions.json
```

结果：

- Go 格式化和原生二进制构建通过；
- `go test ./...` 输出 `[no test files]`，只确认测试目标可加载；
- 三个 JSON 文件均可解析；
- 172 个基础 token、213 个扩展 token、12 个扩展别名通过引用检查，没有重复路径或未解析别名；
- 设计文档的本地链接检查通过。

## 截图

- [Chat / Light / 14 px 正文](screenshots/chat-light-body-14.png)
- [Plugins / Light / 14 px 正文](screenshots/plugins-light-body-14.png)
- [Settings / Dark / 14 px 正文](screenshots/settings-dark-body-14.png)
- [Chat / Light / 760 px 最小窗口](screenshots/chat-light-min-760.png)

截图证明当前 macOS 原生渲染结果，不证明其他操作系统逐像素一致，也不证明 IME、拖放、通知或 WebView 已联调。
