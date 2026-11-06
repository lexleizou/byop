# P29 商业版对话过程 UI 实施规格

**Goal:** 按用户2026-11-06明确要求，BYOP对话过程完全采用已核对的Proma商业版0.21.45行为、排版、图标和动效，额外保留原有实时t/s胶囊。先设计再开发。本阶段版本0.22.0。
**Architecture:** 继续Go/MyGo原生渲染；重新实现已核对的行为与参数，不复制商业bundle/代码/品牌资产。把Pi内容保留为有序块，按真实用户输入聚合整轮，用原生时间轴呈现过程与最终输出。外壳/插件/Composer保持现有功能。
**Tech Stack:** Go1.27/MyGo0.5.5/Onevra组件与Lucide，外部Pi JSONL RPC，中英locales。

## 授权及例外
用户明确要求“完全按照商业版”，并额外保留实时t/s胶囊。因此本阶段对话过程区域采用15/16px字号、1px时间轴、150/200/320/420/500ms分层动画，覆盖旧全局12/14/18及250ms约定；其余区域保持原约定。保留深浅/中英、高对比/减少动态；减少动态时所有本阶段动画与状态限频延迟立即完成。两种外观预设都使用新过程呈现，外壳颜色继续主题化。不新增切换开关。

## 商业版已核对行为（实施目标）
来源：`/Users/lexleizou/AgentScratch/20261026-agent-src/proma/dist/renderer/assets/index-CsKvnwzg.js`；3692行时间轴/统计/状态；3696行详情、事件分组、布局动画。不要加载整个bundle给模型，可用有界字符串检索。较早开源ProcessBlockGroup.tsx不是本次目标（不要用320px视口/3秒倒计时）。
1. 按真实用户输入划分整轮，把连续assistant消息和工具结果归一；工具结果不另起轮。保持块顺序；连续thinking/tool形成activity，commentary文字分隔activity。最终答案独立置于过程下方。
2. 有phase时commentary永远归过程，final_answer归正文。Pi无phase时，在结束确认后的末段助手无工具且正常stop的文本为正文；运行中未确认的文本不要伪装最终回答。终止/错误的尾部文本标未完成输出；只有thinking正常结束无text时可作为正文展示并明确提示。正常结束无答案显示本轮结束未提供文字总结。错误/停止不伪装成功。
3. 消息头及内容缩进按商业版R4/L2（同bundle已核实）实现：35px头像容器、25%圆角，头部gap10px、下margin10px；模型名15px semibold/前景.60，时间12px/前景.38，模型与时间分两行占35px高度；正文（过程与答案）统一左缩进46px。用户消息同样35px中性用户图标与本地化“你/You”、时间和46px正文缩进，不新增账户/头像设置。复用已有模型来源，按provider+ID解析可得友好模型名，不把当前模型错标历史。保留原输出token与实时t/s胶囊，作为模型信息旁的附加胶囊。实时t/s只在当前运行轮绑定原HUD数据；数据未知不造数，不能替换成整轮平均；胶囊不随过程折叠消失。
4. 过程统计：ListTree图标18px；标题16px weight600 line-height1.72，前景alpha .55；“已思考N次、执行N次工具、耗时Xs”。thinking统计非空思考块，tool按call唯一ID统计。耗时一秒更新。整轮运行默认展开，历史默认折叠，结束协调收起；手动展开可复查。
5. 时间轴：ml7px、左边框1px border alpha .70、左padding16px、纵向gap4px。节点标记16px容器，内图标14px，覆盖在细线上；thinking用6px圆点。中间说明保留Markdown。活动节点状态15px medium、foreground alpha .45；只有最新活动active。状态为正在思考/正在读取/读取完成/失败/未完成等语义，不贴原始JSON。活动收起后可显示“· N步”。状态文字最短900ms更新间隔，最新值胜出，420ms上下4px交叉淡入淡出。不可通过人为延迟工具执行实现。
6. 点击activity展开详情（默认收起），展开选择在同一轮重绘时稳定；新会话重置。详情圆角8px，muted背景alpha .35，padding12x10,gap8；可见正文随15px字号，line-height1.72。思考详情Brain16px+Thinking15px，8px圆角、padding14x10、1.5px虚线8/6、muted .3/.5。activity详情显示完整思考；如独立长思考展示则支持约4行收起/展开。不显示仅tooltip中的思考，不使用旧灯泡单行替代。
7. 工具行：Lucide映射Read FileText、Write FilePenLine、Edit Pencil、Bash Terminal、Glob FolderSearch、Grep Search、Agent Bot/Task GitBranch、WebSearch Globe、WebFetch Download、MCP Plug、未知Wrench。图标16px，状态文字15px，副摘要11/12px。运行spinner，失败XCircle，完成不强制绿色勾。调用名+语义摘要；Read/Edit/Write取路径，Bash取描述/命令，search取pattern/query，保留可展开参数及完整已有结果；输入/输出以文本安全渲染。Read等保留原文件预览，Edit可根据可得结果给+/-摘要，MCP/Skill显示相应有色标签；子代理如果协议带parent关系可嵌套，否则保留普通工具详情，不捏造子流程。
8. 详情展开150ms，工具行150ms且前10项30ms错开，箭头150ms旋转。新过程节点高度+opacity320ms cubic(.22,1,.36,1)。手动整轮收起420ms height+opacity ease-in-out。过程结束与答案出现由共同父容器测量前后高度：150ms延迟+500ms cubic(.22,1,.36,1)；答案opacity0->1/translateY4->0同延迟时长。流式高度变化不能频繁重启动画或导致闪烁。原生MyGo用真实可用的AnimateTo/Transition/Layout能力，不能仅定义常量却未应用。
9. 流式在用户处于底部时跟随；用户上滚后不抢滚动；最终答案出现/过程折叠时冻结原阅读位置，允许“回到最新”恢复。旧历史载入不播放逐项进入动画。不修改Go module缓存。
10. 最终回答的Markdown正文也采用商业版15px/1.72行高。完成后的动作区保留复制/重试，避免再重复旧绿色“已完成+耗时+工具数”统计（已有过程统计行）；运行/失败/未完成仍必须如实呈现。保留P28 Mermaid代码块按钮、插件消息渲染、文件卡片、附件、复制/重试/队列、Composer与会话隔离功能。不要用新的直接markdown路径绕过configureMarkdownV12中的既有hooks。

## 两席文件分工与约定（禁止交叉写）
协调方在独立克隆中分配，禁止commit/push、操作桌面、访问凭据、继续委派或改另一个席文件。

### A GLM：数据/历史/协议
拥有 `main.go`（仅数据与事件处理）、`pi_rpc.go`、`pi_sessions.go`，以及新`p29_data.go`、`p29_data_test.go`。必要现有协议测试可改但最终列清单。不改UI/locales/hud.go。
契约必须提供：`chatBlockThinking chatBlockKind = "thinking"`；`chatBlock.text`存thinking文本；`chatBlock.phase string`可选commentary/final_answer；`chatMessage.stopReason string`；`chatMessage.model string`与`provider string`。可以增添内部contentIndex/source身份字段，但不破坏原字段与构造器。
- 源消息边界以assistant message_start或明确内部source身份为准，不能把contentIndex下降当作新消息。真实Pi可在text_start(index1)后补发thinking_end(index0)。同一part的交错更新必须更新原块。`message.blocks`按源消息和原始Pi contentIndex身份保留thinking/text/tool；message_end最终内容与增量幂等合并，不丢工具结果、不重复thinking/text、不把同一消息多个text全拼回最后一块。
- 原`message.thinking`及时间保留兼容，但新UI优先ordered块，无重复；live每段thinking增量不把此前段混入新段。tool-start/update/end必须按ID更新相同块，工具开始不能覆盖前面的思考/说明。
- 历史恢复保留thinking和phase、stopReason/model/provider，与实时形成相同块形态；丢失元信息时诚实回退。不修改Pi JSONL文件，不发送额外RPC改变模型设置。
- 不修改既有TPS计算/usage计数语义；保持其事件正常更新。
- 有意义定向测试：thinking->text->tool->thinking->final的实时顺序、message_end不重复/保留结果、history对等、停止/失败状态、phase保持。
- 完成后gofmt/相关测试与Go build。最终返回文件、契约细节、结果，不得用未执行tool_call文本充当完成。

### B Qwen：原生UI与呈现模型
拥有 `chat_view.go`、`jagent_chat.go`（若需滚动接线）、`hud.go`（只显示层，保持计算）、新`p29_turn.go`/`p29_view.go`/`p29_motion.go`/`p29_view_test.go`，locales/en.json/zh-Hans.json/catalog_gen.go、mygo.json。不改main.go/pi_rpc.go/pi_sessions.go。
- 依据A上述字段编写；独立副本尚无字段时，可读主会话规格或写临时未跟踪编译stub（如需修改main.go仅可生成patch放scratch，禁止把stub混入交付）；最好等待协调方提供A文件后再集成编译。不自行修改A副本。
- 在transcript统一按用户轮聚合，保留原用户附件及操作。旧renderer可留作兼容但实际入口必须走新UI；两种预设均生效。替换旧单行thinking展示。
- 新UI状态放chatPresentation或ui.Local，正确在会话变化时清空；不能以每帧重新分配状态导致折叠/动画丢失。
- 文字全部中英locale，按仓库go generate机制更新catalog。版本0.22.0。
- 原始工具结果已有限长，至少保留已有全部文本/参数并可展开；如数据缺失，不生成虚假行数/diff。
- 读MyGo模块接口确认动画/绘制能力；画虚线/图标可以Painter，不为方便引入WebView或新JS运行时。既有Lucide SVG可复用。
- 定向测试覆盖分组、最终回答判定、计数、状态节流/减少动态、停止不成功、HUD胶囊绑定。不得只测动画常量而不验证真正的时序/目标值。

## 协调方执行与验收
1. 合并两席已核实的文件，完整Go检查及P28 Node17项回归（代码块钩子变动）。
2. Cursor Opus5.5 @1m独立审核，重点顺序/历史/状态/真实动画接线、TPS未回归与实现相对本规格遗漏。P0-P2修复复审后提交。
3. 隔离配置与Pi会话：先用明确人工fixture跑思考+工具成功/失败+commentary+final、历史与恢复；用真实Pi调用确认流式、工具与t/s（不复制凭据；沿用授权Pi认证）。只操作本轮PID窗口。
4. 原生Cua验证：浅/深、中/英、窄窗；活动/整轮展开收起、错误详情、实际t/s胶囊、P28Mermaid；多帧截图或短录制检查动画与停止抢滚动。静态截图不能证明所有动画时长；测试替身不能替代真实Pi。
5. docs/P29-design.md、P29-validation.md，记录逐项完成/缺口；任何原生实现无法等效项必须明确，不把近似称完全一致。打包0.22.0 DMG、hash、源码commit/push、回读远端。更新项目规则中的本次局部视觉例外与状态，不更改上游tokens。
