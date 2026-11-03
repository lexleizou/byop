# P25 插件扩展契约

本轮在 Plugin API 1 的 manifest 上增加可选字段。发布与验证状态见 [P23–P26 验证记录](P23-P26-validation.md)；本文件说明接口，不替代验收记录。`examples/` 下均为契约示例，不是业务插件。

## 声明与实际入口

| `contributes` 字段 | 每项字段 | 使用入口 |
|---|---|---|
| `themes` | `id`, `title`, `light`, `dark`, 可选 `fontFamily`, `codeFontFamily` | 插件详情中的主题选择 |
| `locales` | `id`, `title`, `tag`, `entry` | Settings 的界面语言选择 |
| `slashCommands` | `id`, `title`, `name`, 可选 `description`；`command` 或 `prompt` 二选一 | BYOP/JAgent 输入框的 `/` 建议 |
| `mentionProviders` | `id`, `title`, `command` | 输入框中位于开头或空白后的 `@` 查询 |
| `paletteProviders` | `id`, `title`, `command` | `⌘K` / `Ctrl+K` 命令面板 |
| `statusItems` | `id`, `title`；每个插件最多一项 | 既有 HUD 状态行 |
| `messageRenderers` | `id`, `title`, `command`；`languages` / `mimeTypes` 至少一项 | 已闭合代码围栏、已知路径的附件 |

贡献 ID 沿用已有小写字母、数字、点、下划线和连字符规则。主题和语言路径是插件根目录内的相对路径。被引用的命令必须同时存在于 `commands`，并声明对应的 `onCommand:<id>` 激活事件。

## 主题和语言资源

主题文件示例：

```json
{
  "colors": {
    "background": "#fdfbf7",
    "text": "#21201c",
    "accent": "#2563eb",
    "onAccent": "#ffffff"
  }
}
```

`light` 和 `dark` 分别读取各自文件，某种模式未声明的颜色沿用该模式的内置值，不借用另一模式的颜色。颜色使用 `#rgb`、`#rgba`、`#rrggbb` 或 `#rrggbbaa`；非法值作为插件资源问题报告，不能使窗口崩溃。高对比模式保留内置高对比颜色；减少透明度时使用不透明值。字体家族只改变家族，不改变既有字号和间距。

资源在启动、重扫和插件重启时读取。颜色在加载阶段解析；选取或清除主题只切换已缓存资源。停用、移除或资源暂时不可用时回退到内置样式，并保留已保存的选择；重新可用时恢复。用户主动选择“内置主题”会清除主题选择。

插件 WebView 的主题数据包含 `fontFamily: {body, code}`，桥接层提供 `--byop-font-family` 和 `--byop-font-family-code`。字体为空时会移除这两个变量，恢复插件自己的默认值；原有字号变量不变。WebView 样式应显式使用这些变量及合适的字体回退。

语言资源采用与宿主相同的消息 JSON 格式，例如：

```json
{
  "@@locale": "fr",
  "appWindowTitle": "Atelier Pi",
  "settingsLanguageTitle": "Langue"
}
```

插件语言使用可替换的独立目录，不永久注册到内置 Catalog。缺失消息回退到内置内容；内置语言不能被区域变体意外覆盖。停用语言插件会回退到系统语言，但保存的语言选择保留，重新启用后自动恢复。主题和语言资源本身不启动 worker。

示例：[theme-fixture](../examples/theme-fixture/README.md)、[locale-fixture](../examples/locale-fixture/README.md)。

## 命令、提及和命令面板

插件命令继续使用既有 JSONL `invokeCommand`，参数放在 `params.args`，返回 JSON 值放在 `result`。命令调用通过现有租约管理，防止正在工作时被空闲回收。

| 调用 | 传入 `args` | 返回值 |
|---|---|---|
| Slash `command` | `{}` | 既有命令结果 |
| Slash `prompt` | 不调用 worker | 仅把模板写入草稿 |
| Mention provider | `{query, workspace}` | `{items:[{id,label,text}]}` |
| Palette provider | `{query, workspace}` | `{items:[{id,label,command,args}]}` |
| 代码围栏 renderer | `{language, code}` | `{title?, blocks:[…]}` |
| MIME renderer | `{mimeType,name,size,path}` | 同上 |

键入前缀不会执行命令。选择插件命令才运行；选择提示模板或 Pi 命令只填入输入框，由用户继续编辑或发送。Pi 的 `get_commands` 项与插件项标明不同来源。`Shift+Enter` 和原有发送、插话、后续队列方式继续保留。

提及查询只识别开头或空白后的 `@`，不把邮箱地址当作提及。选中结果只替换当前词语，不自动发送。查询结果保留提供方来源；命令面板结果只能执行该来源插件已声明的命令。关闭菜单、改变查询或停用提供方后，过期结果不能复活或继续执行。

查询使用现有的结果和文本长度上限；当前查询上限为80个字符、单个提供方最多12项，标签最多120个字符、插入文字最多4000个字符。没有新增常驻轮询。

## HUD 状态与消息渲染

发布状态项使用宿主调用：

```json
{
  "method": "host.setStatusItem",
  "params": {
    "id": "fixture.status",
    "text": "QA fixture active",
    "tone": "success"
  }
}
```

`tone` 为 `info`、`success`、`warning` 或 `danger`；文字最多60个字符。传 `hidden:true` 可撤销。状态项必须已声明，且迟到的更新不能恢复已停用插件的状态。空闲回收 worker 后可保留最后一条状态；停用、移除或重启会释放对应状态。

Renderer 返回既有的原生 block 模型，例如：

```json
{
  "blocks": [
    {"type":"heading","text":"Preview"},
    {"type":"text","text":"Rendered by the plugin"}
  ]
}
```

代码尚未闭合、调用未完成或渲染失败时，保留原始代码/附件标签。MIME 渲染需要可识别的文件路径；历史图片无路径时仍显示普通标签。历史路径引用的 `size:0` 表示当前未读取真实文件大小，插件不要据此断言文件为空。

缓存区分插件、贡献和内容/文件身份；同名同大小但不同路径的文件不能共用预览。插件更新、移除或停用使对应缓存失效，其他插件缓存保留。会话替换时释放全部渲染缓存并取消在途请求。

示例与可执行步骤：[interactive-fixture](../examples/interactive-fixture/README.md)。

## Pi 扩展的热卸载边界

以上是 BYOP 自身的插件贡献。外部 Pi 加载的任意扩展不保证物理热卸载：明确声明 `agentExtensionLifecycle: "cooperative"` 的自有扩展可通过约定撤销工具；其他扩展在停用或移除后沿用 Pi 刷新流程。不能把资源或菜单的即时撤销等同于任意第三方 Pi 模块已从内存卸载。
