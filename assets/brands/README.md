# 模型品牌图标（设计资产）

更新：2026-10-24。已通过 `go:embed` 接入 BYOP，用于模型 Picker 与 composer 模型控件。

来源：[Lobe Icons](https://github.com/lobehub/lobe-icons)，静态包 `@lobehub/icons-static-svg@1.98.0`。保存原始 SVG，`currentColor` 在原型中映射到 Radix Sand 12（浅色 #21201c，深色 #eeeeec）。MIT 声明随附于 LICENSE；品牌名称和标识仍属于其权利人。

| 文件 | 模型品牌 | 原始下载地址 |
|---|---|---|
| deepseek.svg | DeepSeek | https://unpkg.com/@lobehub/icons-static-svg@1.98.0/icons/deepseek.svg |
| claude.svg | Claude | https://unpkg.com/@lobehub/icons-static-svg@1.98.0/icons/claude.svg |
| qwen.svg | Qwen | https://unpkg.com/@lobehub/icons-static-svg@1.98.0/icons/qwen.svg |

LICENSE 来自上游 master/LICENSE，读取日期 2026-10-23。这里只保存当前应用使用的三个品牌，不代表全部模型已配置。

## 共用约定

- 列表和输入框使用同一个模型品牌图标，不用通用星光代替已识别品牌。
- 16px 图标框，距模型名称8px；保持原始比例，不加底色块、阴影或装饰边框。
- 模型品牌与接入渠道分别表达。例如 Magpie 和 OpenCode Go 下的 DeepSeek 均使用 DeepSeek 图标。
- 不能从 OpenAI-compatible 或 Anthropic-compatible 协议推断模型品牌。
- 未识别时使用中性模型图标并保留完整名称；图标不是唯一识别信息。
- 扩展规则见 ../../model-import-and-settings.md 的“模型品牌图标”章节。

参考 Proma 的识别行为，不复制其商业版图标资产或实现代码。
