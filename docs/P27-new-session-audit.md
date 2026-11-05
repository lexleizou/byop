# P27 新会话语义审计与最小修复（GLM 席）

日期：2026-11-06。范围：`main.go` 会话模型记忆迁移逻辑。副本：`p27/glm`（未提交、未推送）。行号以本副本修改后状态为准。

## 结论（已核实）

1. **缺陷确认**：修复前 `applyPiSessionState` 无条件调用 `s.migrateModelTuples(oldKey, s.piSessionKey())`（原 main.go:1342）。`migrateModelTuples`（main.go:1354–1373）把旧 key 的模型记忆搬到新 key 并 `delete` 旧 key，不区分是否同一会话。后果：上一会话的记忆泄漏进新会话（违反"新对话完全跟随 Pi、不跨新会话记忆"），同时旧会话自己的记忆被删除（破坏"已有会话恢复自身状态"）。
2. **会话 key 机制**：`piSessionKey()`（main.go:1851）优先返回 canonical 会话文件路径，其次 SessionID。同一会话内 Pi 先给 ID、后补文件，产生 ID→file 的 key 切换，这是 `migrateModelTuples` 唯一应工作的场景（`TestP11ModelTuplesMigrateFromSessionIDToFile`，p11_test.go:307）。
3. **newPiSession 无宿主覆盖**：`newPiSession`（main.go:1602）→ `client.NewSession`（pi_rpc.go:566–577）仅发送 `{"type": "new_session"}`，未附带 `set_model`/`set_thinking` 等；随后 `loadPiSession` → `applyConfirmedModelState` 使用的是 Pi `GetState` 返回的实际状态。`rememberedModelTuple`/`rememberedModelTuplesForSession`（main.go:1858、chat_view.go:1672）只读当前会话 key 用于模型面板预选，不回写 Pi。新会话 key 下无记忆即完全跟随 Pi，符合目标语义，未改 Pi 默认值。
4. **调用面**：`migrateModelTuples` 仅在 `applyPiSessionState` 一处被调用；`applyPiSessionState` 的其余调用方（channel_refresh.go:615、main.go:1391/1473/2642、session_naming.go:477）全部经由同一守卫收敛，无需分别处理。`resetModelTuples` 只在 Pi 进程停止/重启（main.go:2493、3384）全清，`commitChannelRefresh`（channel_refresh.go:591/610）换进程时整表保留、不动 key，均与本缺陷无关。

## 修复（main.go:1346–1348）

迁移改为仅在确认同一会话时执行，复用既有 `samePiSession`（main.go:1836：canonical 文件路径相等，或非空 SessionID 相等），与其上方会话命名失效判断（main.go:1324）保持同一口径：

```go
if samePiSession(oldFile, oldID, s.piSessionFile, state.SessionID) {
	s.migrateModelTuples(oldKey, s.piSessionKey())
}
```

跨会话不迁移也不删除：旧记忆留在旧 key 下，切回该会话（文件路径命中）即可恢复。oldKey/newKey 为空或相等时 `migrateModelTuples` 原有早退逻辑不变。改动共 +7/−1 行，未触碰插件、README、版本、AGENTS 或 Pi 配置。

## 测试

- 保留：`TestP11ModelTuplesMigrateFromSessionIDToFile` 语义不变，修复后通过。
- 新增 `p27_test.go` 三个定向测试：
  1. `TestP27NewSessionDoesNotInheritFileKeyedModelMemory` — 新会话（新 ID、无文件）不继承上一会话文件 key 记忆，旧记忆保留，`rememberedModelTuple` 不命中。
  2. `TestP27SessionSwitchKeepsModelMemoryIsolated` — ID→ID 切换互不泄漏；切回后会话 A 记忆完好。
  3. `TestP27SessionIDFileHandoffMigratesWithoutTouchingOtherSessions` — 同会话 ID→file 仍迁移且删除旧 ID key，无关会话记忆不受扰动。
- 回归证明：还原 main.go 后跑新测试，1、2 失败（旧代码确实泄漏+删除），3 通过（旧代码本就迁移）；重新应用修复后全部通过。

## 检查记录（均在 glm 副本执行）

- `gofmt -l main.go p27_test.go`：无输出。
- `go vet ./...`：通过。
- `go build -o /tmp/byop-p27-glm-build .`：通过。
- `go test -count=1 -run 'TestP27|TestP11' .`：全部 PASS。
- `go test ./... -count=1`：ok（21.2s）。
- `git diff --check`：干净。

## 未验证项

- 真实 Pi 进程在 `new_session` 后自身是否保留上次的模型/思考选择：属 Pi 侧行为，按 spec 不改 Pi，需运行时确认。
- WebView/真机 GUI 下切会话、换渠道刷新并发路径的实际表现：未做设备与 Cua 验收，仅代码与单元级证据。
- 审计期间副本内短暂出现一个非本席创建、未跟踪的 `p27_new_session_test.go`（3 个同类 P27 测试），后被外部删除；本席未并入其内容，工作区最终仅含 main.go 修改与 p27_test.go，请协调方合并时知悉。
