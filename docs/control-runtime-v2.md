# Control Runtime v2

日期：2026-08-13

Control Runtime 是 ConsensusAtlas 的唯一执行 substrate。它把不同共识实现映射到同一组可调度对象，同时
保持目标专用协议语义位于 Adapter/projector 中。

## 1. 目标与非目标

目标：

- 保存并控制尚未决定命运的消息；
- 用逻辑时间构造自然超时；
- 在持久化、消息、crash/restart 之间制造明确切点；
- 对外部输入、host effect 和 application result 做显式调度；
- 记录可 fresh Replay 的 Trace；
- 允许 partial capability，而不伪造统一控制强度。

非目标：

- 不内置 Raft/Paxos/HotStuff 状态机；
- 不让 Agent 构造 enabled set；
- 不从 Trace 自动推断协议正确性；
- 不用强制“立即超时”或“立即成为 leader”代替自然协议行为；
- 不承诺纯黑盒目标能够提供完整消息/时间/持久化所有权。

## 2. 对象模型

### 2.1 Action

当前公共 ActionKind：

- `deliver-message`、`drop-message`、`duplicate-message`；
- `partition`、`heal`；
- `fire-temporal-event`；
- `crash`、`restart`；
- `complete-effect`、`fail-effect`、`complete-callback`；
- `invoke`。

Action 携带 Runtime 产生的 ActionID，并按需要引用 NodeRef、ItemID 和有限参数。调用方只能选择当前 offered
Action；Runtime 会重新检查 ID、节点 incarnation、item 状态和参数。

### 2.2 ProducedItem

Adapter 每次运行到稳定 yield 后，将新工作返回 Runtime：

- message；
- temporal item；
- host effect；
- host callback；
- client response；
- typed observation。

ProducedItem 有全局唯一 ItemID、owner 和可选 dependency。Runtime 管理其从 produced/blocked/enabled 到
completed/canceled/failed/dropped 的状态变化。

### 2.3 Adapter

统一接口位于 `internal/control.Adapter`：

```go
type Adapter interface {
    Manifest(context.Context) (AdapterManifest, error)
    Reset(context.Context, []byte) error
    Check(context.Context, AdapterCommand) (CommandEligibility, error)
    Submit(context.Context, AdapterCommand) error
    CheckRuntimeAction(context.Context, Action) (CommandEligibility, error)
    ApplyRuntimeAction(context.Context, Action) error
    RunUntilYield(context.Context) (Yield, error)
    Collect(context.Context, YieldID) (Emission, error)
    SnapshotEvidence(context.Context) (EvidenceEnvelope, error)
    SnapshotEntropy(context.Context) (EntropyAuditEnvelope, error)
}
```

Adapter 的典型动作：

1. 接收 Runtime 已经选择的消息/时间/生命周期/input/effect；
2. 调用目标已有接口；
3. 运行到稳定边界；
4. 返回新消息、timer、effect、result 与 Evidence。

Adapter 不决定下一个全局动作，也不在内部长期隐藏本应交给 Runtime 的消息。

## 3. 消息控制

目标产生 outbound message 后，Adapter 立即把它作为 Message ProducedItem 交给 Runtime。之后 Runtime 可以：

- deliver：将精确 payload 交给目标节点；
- drop：结束该 item，不调用目标；
- duplicate：创建带 clone 关系的新 message item；
- partition：让跨分区消息保持存在但不可投递；
- heal：恢复这些消息的可投递资格。

这解决了“消息不能即时完成”的问题：Action 操作的是 Runtime 邮箱中的 item，不要求 Adapter 在产生消息时
立即决定其命运。

## 4. 自然时间

Runtime 不把任意 `AdvanceTime(delta)` 暴露为普通自由动作。可调度 temporal item 有 deadline：

1. 只有全局最早 deadline 的一组 temporal item 可以 offered；
2. 选择其中一个 `fire-temporal-event` 时，逻辑时间推进到该 deadline；
3. Adapter 收到对应 callback/tick；
4. 周期 pulse 可以产生下一 deadline。

因此测试可以主动选择“让最近超时发生”，但不能跳过更早 timer 或直接制造协议级 timeout 事件。对以 Tick
驱动的 Raft Adapter，周期 pulse 表示一个逻辑 tick；多个 tick 仍由多次自然 temporal action 产生。

sleep/wakeup 也表示为 temporal item。若系统只有 sleep 可继续，选择它会自然推进到最早 wakeup。

## 5. crash、restart 与持久化

节点由 `NodeRef{NodeID, Incarnation}` 标识：

- crash 只对 running incarnation enabled；
- restart 只对 stopped node enabled，并产生更高 incarnation；
- 旧 incarnation 的临时 item 不能误投给新进程；
- power-loss 模式丢弃 volatile 状态，保留声明为 durable 的结果。

持久化与 application work 不被折叠为瞬时内部步骤。Adapter 可产生 HostEffect，Runtime 再选择完成或允许的
失败结果。由此可以表达：

```text
Ready/append requested
    -> crash before durability
    -> restart
    -> observe recovery
```

也可以表达 effect 完成后、消息释放前的 crash。具体“何时允许发消息”仍由目标 Adapter 的 host 语义决定。

## 6. enabled 的精确定义

enabled 不只是“不能 crash 已 crash 节点”。它由四层共同决定：

1. Runtime 状态：节点 lifecycle、item state、partition、deadline、dependency；
2. Adapter eligibility：目标当前是否接受该命令；
3. qualification/capability：该目标是否承诺支持该类控制；
4. experiment envelope：当前 workload、fault 数和预算是否允许。

例子：

- running 节点才能 crash；stopped 节点才能 restart；
- 只有 produced/enabled 且目标 incarnation 匹配的消息才能 deliver；
- partition 中的跨边消息仍存在，但 deliver 不 enabled；
- temporal item 必须属于最早到期集合；
- 达到 crash/drop/duplicate 上限后，相应 fault action 不再 admissible；
- 已完成 effect 不能再次完成。

搜索或 Agent 只能看到最终 admissible ActionRef 集合，不能绕过这些检查。

## 7. 随机性

Runtime 使用分域确定性 entropy：

- seed 是运行输入；
- 每次抽样记录 domain 与结果；
- Trace 保存 entropy audit/tape identity；
- fresh Replay 使用同一 tape 并检查消费顺序；
- Adapter 若存在不可控系统随机源，qualification 不能宣称 strict replay。

Agent 不直接生成随机字节。随机 baseline 也只在当前 Action 集合中采样。

## 8. Trace 与 Replay

每个 ActionRecord 至少绑定：

- step、logical time、enabled set；
- 被选择的 Action 和可选 AdapterCommand；
- yield、emission、Evidence 与 entropy；
- clock advance；
- item/node transition；
- before/after state。

fresh Replay 重新创建 Adapter，从相同初态执行 Trace Action，并逐步比较资格、状态、Evidence 和最终结果。
Replay 成功证明该有限执行在当前模型内可复现，不证明协议正确。

## 9. Evidence、PSS 与 Oracle

Adapter 的 Evidence 是目标专用但版本化的只读快照。可信 mapper/projector 将其转换为：

- Core PSS：跨协议的粗粒度语义状态；
- Extended PSS/Risk：协议族或目标专用关系；
- decision projection：Oracle 所需的最小协议无关输入。

Runtime 不解析 term、log、ballot、quorum 或 lock。etcd/raft 等目标的 Evidence 解码位于 Adapter/target-local
composition；通用 Oracle 只比较规范化后的决定。

## 10. Qualification

Manifest 声明节点、Action、Item、temporal、crash、effect、entropy、Evidence schema 和 strict replay。
Conformance 用外部驱动机械检查声明；声明本身不能授予资格。

结果允许：

- scheduler-owned：Runtime 真正拥有该控制；
- interceptable/partial：只能观测或拦截部分边界；
- unavailable：目标没有可用接口。

测试 Profile 必须按实际资格选择，不得先限制目标再反向挑“适配的协议”。

## 11. 目标专用耦合

允许耦合：

- Adapter 调用目标 API；
- Evidence decoder；
- workload router；
- PSS/Risk/decision projector；
- 构建与启动方式。

禁止耦合：

- 在公共 Action/Runtime 中加入 Raft term/Ready/ReadIndex；
- 通用 search 解析目标消息类型；
- Oracle 直接导入目标协议包；
- 为单一目标预建通用 schema。

新协议接入首先复用公共 Action；只有两个以上现实目标需要且已有 Action 无法表达明确失败场景时，才考虑扩展。

## 12. 当前实现和下一步

当前 etcd/raft 已覆盖消息、自然 tick、crash/restart、effect、workload、Evidence、PSS 和 strict Replay；
OmniPaxos 证明非 Raft 复用；HashiCorp Raft 保留 partial 结果。

Control Runtime 本身进入维护状态。后续只在活动 Agentic 调查暴露明确控制缺口时扩展 Runtime；普通协议语义
优先留在 Target-local Observation、Action preparer 和 Oracle 中。
