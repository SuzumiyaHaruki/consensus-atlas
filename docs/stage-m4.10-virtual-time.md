# M4.10 阶段总结：可验证虚拟时间边界

日期：2026-08-06
状态：完成

## 阶段结论

M4.10 将逻辑时间从单纯的数值递增，收紧为可重放、可审计的通用 timer queue 边界：可选的
Driver `TimerSource` 只声明某一逻辑时刻完整的存活 timer 集合；Runtime Engine 独占 queue、
取消、re-arm 和释放。`advance` 记录 `clock-advance` control trace，但到达 deadline **只让 timeout
进入 enabled 集合**，绝不会自动调用协议。

这吸收了 MODIST 虚拟时间“由受控执行推进时间”的优点，同时没有把时间推进误建模为强制
timeout 或真实系统活性保证。

官方 etcd/raft v3.6 Driver 在本阶段仍不实现 `TimerSource`。其 election deadline 受官方包内部
不透明随机选择影响，因而 `natural-election-timeout-replay` 继续是明确的 Unsupported capability；
本阶段没有调用 `RawNode.Tick`，没有修改官方实现，也没有把随机 timeout 伪装成可信输入。

## 已落地边界

```text
optional Driver TimerSource(now)
        │ complete declarations: id / target / deadline / payload
        v
Engine-owned timer queue
        │ advance: only deadline <= logical clock becomes enabled
        v
normal scheduler decision
        │ explicit Execute(timeout)
        v
Driver Invoke + complete declaration remove/re-arm
```

- `core.Timer` 有稳定 ID、目标节点、绝对 deadline 与 canonical JSON payload。
- `adapter.TimerSource` 和 `driver.TimerSource` 都是可选接口；不支持 timer 的旧 Driver 无须实现
  新方法，`host.Adapter` 只做被动转发。
- Engine 在创建时和每次成功执行后 reconciliation。声明必须完整、ID 唯一、目标为已知节点，且
  deadline 不能早于当前逻辑时间；否则 construction/execution 机械失败。
- 一个 timeout 执行后，其 ID 必须从下一次声明中移除，或以不同（通常更晚）的 deadline 重置。
  保留已消费的同一声明会被拒绝，避免 Runtime 静默制造无限 timeout。
- `TimerID` 由 Engine 写入已声明 timeout；Test Plan 输入字段没有该能力，因此 Driver 可以将
  已释放 native timer 与任意 `timeout` 测试输入区别处理。
- `drop` 不可删除一个已声明 timer。时间到期后的延迟通过“不选择该 enabled event”表示，而不是
  删除 timer 后又由下一次声明悄悄复活。
- `clock-advance` 的 before/after snapshot 包含 logical time、network、system 和 timer queue。
  partition/heal 继续使用原有 control snapshot，以免新增空字段改变不使用虚拟时间的历史 trace。

## 验证结果

`internal/engine/timer_test.go` 的独立通用 fixture 验证：

1. deadline 前 timeout 不可用；
2. deadline 到达不调用 Adapter，只有显式 `Execute` 才交付；
3. 交付后 remove/re-arm 分别清空或替换 pending event；
4. duplicate ID、未知节点、无效 payload、保留已消费声明都会被拒绝；
5. 已声明 timeout 无法被 drop。

`drivers/etcdraft/driver_test.go` 的 integration fixture 验证：`Advance(100)` 只产生一条
`clock-advance` trace；不会生成 etcd timeout pending event，且 Driver Manifest 中
`natural-election-timeout-replay=false` 保持不变。

为检验 M4.9 兼容性，使用当前 Runtime 重跑冻结的
`plans/defectbench/etcdraft-63903dd-v1.json`，并与保存的
`trial-candidate-v3.6.0-v2` 逐项比较。下列结果完全相同：

| 证据 | 冻结值 |
|---|---|
| setup fingerprint | `4a70dcbe7b6728dbfa650963bfa797d396d2c217b813bc4a8f1eb6f405855237` |
| full execution fingerprint | `59b74e04eec1af04b7d7d98cd0f41c4f958c3be0de82e12c9ec9563208fa71df` |
| measurement fingerprint | `69e8ed5889f703974c3c0cb39e2030dae129a06476e4704337ddc35bbf766a2a` |
| Coverage | `26/55`, `45.67` |
| complete work | `1 run / 1 decision / 218 primary / 218 replay` |
| Oracle | 相同的两个 `linearizable-read` finding（step 201） |

初次比较曾显示 setup/full fingerprint 改变。原因是新增的 empty timer evidence 被加入所有
partition/heal control snapshot，而不是 etcd/raft 行为变化。修复为仅在 `clock-advance` 记录
时间快照后，三个 fingerprint 恢复字节一致。这是一次有价值的兼容性自审：新证据字段不能无故
污染已经冻结的 trace 身份。

## 本阶段没有证明

- 没有证明 etcd/raft 的随机 election timer 可重放；
- 没有实现 `RawNode.Tick`、heartbeat、GST、partial synchrony 或公平调度；
- 没有得到任何活性结论，更没有将虚拟时间当作真实墙钟时间；
- 没有扩大 Coverage/PSS 分母、没有新增 Agent 角色，也没有证明任何测试方法优于基线；
- 没有修改 `/home/nitro/Desktop/raft` 或 Go module cache，未调用模型服务。

## 下一阶段

下一项应回到外部有效性证据：冻结多个彼此独立、对 Agent 不泄露 trigger/patch/root-cause 的
qualified historical candidate/control 样本。每个新样本先经过机械 qualification；缺少 timer、
消息或 monitor capability 时保持 deferred，而不是为了增加样本强行扩展 Driver。

在至少多个独立样本和正确 controls 准备好之前，不应把单 Planner、Random、DFS 或 Coverage/PSS
数值解释为方法优势，也不应继续增加复杂 Agent 角色或复合义务语言。
