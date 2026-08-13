# ConsensusAtlas Control Runtime v2 设计与实现基线

状态：`v2alpha1 / A0 inherited platform`；控制内核冻结，Agentic Track 只更新其上层工具边界

当前分支：`feature/agentic-consensus-testing`

继承基线：`feature/control-runtime-v2` / `0106e2c`

日期：2026-08-13

## 1. 文档目的

本文主要冻结 ConsensusAtlas 的控制层方向；第 19 节另记录 Agentic Track 在该控制层之上的工具权限，
不把 Agent 规划逻辑下沉进 Runtime，也不重写 Coverage、PSS 或 Defect Benchmark。
它要先回答一个更基础的问题：

> 对不同共识实现，如何用同一套确定性语义控制外部输入、消息、时间、生命周期和宿主副作用，
> 同时只要求每个实现提供一个可验证的薄 Adapter？

v1 已证明 etcd/raft 的场景执行、严格重放、Oracle、Coverage Ledger、PSS、Agent Campaign 和
benchmark 账本可以连通。但其 `OutputBatch + exactly-one acknowledge + one outstanding batch`
模型明显受 etcd/raft `Ready` 生命周期影响，不能直接作为跨协议控制层的最终抽象。

v2 不是推倒重来。以下 v1 能力应保留并逐步迁移：

- 稳定 ID、不可变消息副本、因果关系和严格重放；
- 确定性调度、逻辑时间、消息保留、分区、丢弃和复制；
- visible/durable 状态区分和生命周期记录；
- Oracle、Evidence、Coverage Ledger、PSS 和 benchmark 的可信边界；
- Agent 只能提交受限计划，不能直接修改状态、Oracle 或覆盖结果。

v1 分支和历史实验工件保持冻结。v2 使用新的 trace/schema 版本，不要求与 v1 生成字节一致的
trace；需要保持的是可解释的研究结论、工件来源和严格重放能力。

## 2. 范围

### 2.1 第一阶段支持范围

- CFT 共识，以及只包含有限双投票等受限行为的 BFT 实验；
- 可嵌入库、可控单进程节点，或具备确定性代理边界的多进程系统；
- 消息发送和接收可以被 Adapter 截获；
- 时钟和定时器可以被 Adapter 替换或虚拟化；
- 持久化等宿主副作用可以被观察，并能延迟完成或报告失败；
- 节点能在稳定让步点停止，或者由代理明确证明已经到达该点。

### 2.2 暂不承诺

- 对完全黑盒、不可截获网络和时钟的二进制提供严格确定性重放；
- 任意字节修改、任意伪造消息或开放式拜占庭行为；
- 真实操作系统全部系统调用、线程抢占和硬件故障的穷举；
- 用控制层覆盖率宣称协议正确、完备或无剩余缺陷；
- 在第二个异构 CFT 实现接入前宣称控制层具有跨协议普适性。

不能满足严格让步和稳定观察条件的实现，仍可运行降级实验，但 Manifest 必须把
`strict_replay` 标记为不支持，其结果不能混入严格重放实验。

## 3. 核心设计原则

1. **Runtime 拥有环境语义。** 消息是否可投递、逻辑时间、节点 incarnation、网络关系和副作用
   完成状态由 Runtime 决定。
2. **Adapter 拥有原生映射。** Adapter 负责把统一命令翻译为实现 API，并把实现输出翻译为
   统一 Produced Item；它不选择调度顺序。
3. **协议语义不进入控制内核。** Runtime 不理解 term、view、QC、lock、commit index 或
   `Ready`。
4. **所有非确定性必须显式化。** 可影响结果的输入必须成为有稳定 ID 的 Action、Produced Item、
   Entropy Draw 或 manifest-bound 初始参数。
5. **自然超时，不强制超时。** 搜索器只能选择 Runtime 枚举的最早时间事件；时钟推进是该复合
   动作的内部步骤，Agent 不能提交任意时间数值或提前触发 timer。
6. **消息是一等对象。** 已释放的消息由 Runtime 长期保存，直到显式投递或丢弃；节点主动动作
   完成不要求立刻处理消息。
7. **让步点代替 wall-clock drain。** Adapter 每次执行到一个可证明稳定的 yield，禁止依赖
   `sleep` 或“等一会儿看看还有没有输出”。
8. **能力由外部测试证明。** Adapter 的 Manifest 是声明，不是证据；Conformance Suite 才决定
   哪些能力可以进入 validated 状态。
9. **Agent 没有旁路权限。** Agent 可以提出语义假设、episode 目标和搜索工具选择，但落到具体执行时
   只能引用 Runtime 已枚举的稳定 ID，不能直接调用 Adapter、伪造观察或越过 enabled 判定。

## 4. 总体结构

```text
Minimal Protocol Knowledge                Candidate implementation
         │                                           │
         v                                           v
  Core PSS IR <--- Semantic Mapping      Execution Binding
         │                                      │
         │                             shared Adapter kit
         │                                      │
         └──────────────> Control Runtime v2 <──┘
                              │
                  enabled actions + trace + evidence
                              │
              ┌───────────────┼────────────────┐
              v               v                v
        Search/Planner      Replay          Oracle/PSS/Coverage
```

控制层分成四类对象，不能继续混入同一个 `EventKind`：

- `Action`：调度器可以选择的环境动作；
- `AdapterCommand`：Runtime 发给某个 Adapter 的规范化命令；
- `ProducedItem`：Adapter 在稳定 yield 输出、之后由 Runtime 管理的对象；
- `Observation/Evidence`：供 Oracle、PSS 和 Coverage 使用的只读事实，不具备控制能力。

## 5. 职责边界

| 领域 | Runtime | Adapter/Binding | Semantic Mapping | Agent/Search |
|---|---|---|---|---|
| enabled 判定 | 最终决定 | 报告本地前置条件 | 不参与 | 只能选择 enabled ID |
| 消息存储与网络关系 | 负责 | 截获和解码边界 | 可解释协议字段 | 选择投递/丢弃等动作 |
| 逻辑时间 | 负责 | 暴露 timer/pulse/wakeup | 可解释 timeout 类型 | 只能选择最早时间事件 ID |
| 随机性 | 派生分域种子并审计 | 映射原生随机源 | 可解释随机用途 | 第一版不能选择随机结果 |
| crash/restart 抽象语义 | 负责 | 实现原生停止/恢复 | 可观察协议恢复结果 | 选择时机 |
| 持久化完成顺序 | 负责 | 暴露 effect 并接受结果 | 可解释持久化证据 | 选择完成/失败动作 |
| 协议状态 | 不解释 | 导出实现证据 | 映射到 Core/Extended PSS | 只读有限投影 |
| 覆盖和正确性 | 不自报 | 不自报 | Matcher/Oracle 机械判定 | 不能写 Ledger |

任何具体接入都可以在 Adapter 内部拆成网络代理、虚拟时钟、进程监督器和实现绑定，但 Runtime
只看到一个规范化 `ControlAdapter`。不能在 Runtime 中通过类型断言为 Raft、进程 Adapter 或
某个可选扩展走不同分支。

## 6. 稳定身份与基本类型

第一版建议冻结以下公共身份：

```text
NodeID          测试拓扑中的逻辑节点
Incarnation     同一 NodeID 的第几次运行实例
ActionID        enabled action 的稳定引用
CommandID       一次已提交 Adapter 命令
YieldID         Adapter 稳定让步边界
ItemID          Produced Item 的稳定引用
MessageID       原始消息身份
CloneID         消息副本身份
TemporalID      一次 timer/pulse/wakeup registration 身份
EffectID        一次宿主副作用请求身份
EntropyDrawID   一次受控随机请求身份
ObservationID   只读观察身份
```

ID 由确定性输入和顺序号构造，并进入 canonical digest。描述文本、Agent 自由文本和 Go 指针地址
不能参与身份。

所有 payload 使用版本化 envelope：

```text
kind + schema_version + encoding + bytes + digest
```

Runtime 只验证 envelope 和 digest，不解释协议字节。需要跨实现比较的字段由版本化 Evidence
Mapper 提取。

## 7. Action 模型

### 7.1 Runtime 原生动作

这些动作不调用 Adapter：

- `DropMessage(item_id)`：先由 Adapter acknowledgement 解除实现侧同步发送（异步实现可 no-op），
  再由 Runtime 终结一个已释放消息；
- `DuplicateMessage(item_id)`：生成具有 lineage 的新副本；
- `Partition(link_or_group)`：禁止相关消息投递，但不删除消息；
- `Heal(partition_id)`：恢复链路资格；
- `CancelTrial(reason)`：由可信 Coordinator 终止无效或超预算 trial。

### 7.2 Adapter 定向动作

这些动作经 Runtime 验证后转换为 `AdapterCommand`：

- `Invoke(node, input)`：客户端请求或协议公开输入；
- `DeliverMessage(item_id, target)`：把一个已释放消息交给目标节点；
- `FireTemporalEvent(item_id)`：原子地推进到允许的 deadline，并执行 timer、pulse 或 wakeup；
- `Crash(node, mode)`：在稳定边界停止当前 incarnation；
- `Restart(node)`：仅从该节点已完成的 durable state 创建下一 incarnation；
- `CompleteEffect(effect_id, result)`：完成一个 enabled 宿主副作用；
- `FailEffect(effect_id, failure)`：用 Manifest 允许的故障结果结束副作用；
- `CompleteCallback(callback_id, result)`：完成显式外部回调。

第一版不提供自由 `AdvanceTime(to)`、`ForceTimeout` 或携带任意协议字节的 `ForgeMessage`。

### 7.3 enabled 集

Runtime 只能枚举当前真正可执行的动作，并按稳定排序输出：

```text
action_kind, node_id, incarnation, item_id, canonical_parameters
```

`enabled` 只表示动作在控制层中**机械可执行**，不表示协议一定接受、动作一定成功或动作有助于
覆盖。候选动作必须依次经过 Runtime 结构检查、Manifest/Conformance capability、Profile/fault
model/预算和 Adapter 稳定边界检查，才能取得当前 frontier 的 `ActionID`。

enabled 判定至少检查：

- 节点是否处于正确 lifecycle 状态；
- item 是否属于当前或允许的历史 incarnation；
- 消息是否已释放、目标是否运行、链路是否连通；
- temporal item 是否未取消、incarnation 有效，并处于最早允许时间窗口；
- effect 的依赖是否全部满足；
- action 是否能由 Manifest 声明的公共命令表达，且 Adapter 当前 `Check` 为 eligible。

这里的“检查”分成两个不可混淆的层次。`controlruntime.EnabledActions` 只根据 Runtime 拥有的结构状态、
Adapter 当前资格和 Action 生命周期枚举 **runtime-enabled frontier**；Experiment 再使用已绑定的
Qualification/Profile、FaultEnvelope 和剩余预算产生 **admissible frontier**。两层都使用 Runtime
已经冻结的 Action/ActionID，不在外部重新制造动作。

被比较的 fixed、random、mutation、corpus 和未来 Agent 策略只能收到同一份 canonical admissible
frontier。不得让某种策略先选择 runtime-enabled 动作再被 envelope 拒绝，而另一种策略预先过滤；这种
差异会把资格失败率、有效预算和状态发现混入搜索方法效果。Experiment 选择记录必须分别绑定
runtime-enabled digest、admissible digest、policy identity 和 selected ActionID；这些记录属于
Experiment/MethodLedger，不要求修改 Runtime Trace schema。

Runtime 不检查 leader、term、view、QC 或消息是否过期。例如向 follower 提交 proposal、投递旧
term 消息或重复请求，只要环境层面可执行就可以 enabled；协议的拒绝、忽略或错误必须进入
`ActionResult/Observation`。`Crash` 只要求节点 Running、Adapter 位于稳定 yield、模式和预算允许，
不能因为存在 pending effect/message 而被禁用，否则会丢失 crash-before-persist 等关键切点。

外部 `Invoke` 不使用 trace 外等待队列。`OfferInvoke(ctx, node, payload)` 必须在当前 frontier 先通过
Runtime 结构检查和 Adapter 的纯资格检查；不合格时返回稳定错误且不修改状态，合格时才冻结
`ActionID`。调用方若不在当前 enabled 集选择它，状态变化后必须重新 offer。该规则约束的是稳定
执行边界，不允许 Adapter 用 leader/term 等预测操作最终成功；原生协议拒绝必须产生明确结果。

```text
Candidate Action -> Enabled Action -> Action Result
```

Action kind、Capability、TestIntent 和 Profile ID 在实验前冻结；具体 `ActionID` 由当前状态动态
生成，只对当前 frontier 有效。状态变化后必须重新枚举，Agent 不能重复使用旧 ID。

回放时引用一个不再 enabled 的 `ActionID` 是 replay divergence，不能悄悄跳过或换成相似动作。

## 8. Adapter 契约

概念接口如下；Go 签名在实现阶段单独冻结：

```go
type ControlAdapter interface {
    Manifest(ctx Context) (AdapterManifest, error)
    Reset(ctx Context, seed Seed) error
    Check(ctx Context, command AdapterCommand) (CommandEligibility, error)
    Submit(ctx Context, command AdapterCommand) error
    RunUntilYield(ctx Context) (Yield, error)
    Collect(ctx Context, yieldID YieldID) (Emission, error)
    SnapshotEvidence(ctx Context) (EvidenceEnvelope, error)
}
```

### 8.1 标准命令周期

```text
Runtime validate action
  -> Adapter Check(command)
  -> freeze ActionRecord
  -> Submit(command)
  -> RunUntilYield()
  -> Collect(yield_id)
  -> validate and freeze Emission
  -> atomically commit Runtime state and ResultRecord
```

`Reset` 后同样执行一次 `RunUntilYield + Collect`，使启动输出进入 trace，而不是成为免费且隐藏的
bootstrap I/O。

`Check` 只能在稳定 yield 上返回 `eligible` 或稳定 reason code，不得推进协议、读取 wall clock 或
产生输出。Runtime 仍是 enabled 的最终判定者：它先完成公共状态和 capability 检查，再使用
Adapter 的本地资格结果。Conformance Suite 会检查重复 `Check` 的幂等性。协议 API 的正常拒绝
或客户端错误应该成为 `ClientResponse/TypedObservation`，不能伪装成 Adapter 调用错误；Adapter
错误只表示接入、执行或确定性边界失败。

### 8.2 yield 的硬约束

`Yield` 表示：

- 当前命令引起的同步工作已经运行完；
- 节点不能在 Runtime 不知情时继续产生会影响结果的外部输出；
- 需要环境响应的工作已转换成 Produced Item；
- 同一 `YieldID` 的 `Collect` 必须返回相同 Emission 和 digest；
- Runtime 对同一 Emission 只提交一次。

允许的实现方式包括单线程库调用返回、确定性 executor barrier、受控事件循环 barrier 或进程代理
协议。禁止用 wall-clock sleep、空闲轮询次数或机器负载判断“应该已经 drain 完”。

若目标系统无法提供可靠 yield，Adapter 必须声明 `strict_yield=false`，Conformance Suite 也不会给
它严格重放资格。

`Crash` 是统一周期中的特殊终止边界：监督器确认目标 incarnation 已停止后返回一个 terminal
yield。该 yield 不要求已经停止的协议实例继续运行，只证明旧实例不会再产生输出；后续 Collect
由仍然存活的 Adapter/监督器返回冻结的终止 Emission。这样库内实例丢弃和外部进程停止仍使用
同一命令周期，不需要 Runtime 按 Adapter 类型分支。

### 8.3 失败原子性

在成功取得、校验并冻结 yield 前，Runtime 不提交抽象状态迁移。若 Adapter 在命令中途退出、
返回不完整 Emission 或违反 digest 稳定性，trial 标记为 `invalid-execution`，不得把部分结果算作
覆盖或缺陷检出。

这不是要求任意外部进程都能事务回滚，而是规定：无法确定命令边界时必须诚实地使本次 trial
无效，不能伪造确定性。

### 8.4 Evidence 快照

`SnapshotEvidence` 只能在成功提交 Emission 后、Adapter 仍处于稳定 yield 时调用。它是只读且
幂等的：不能推进协议、完成 I/O、消费消息或改变下一 enabled 集。相同 yield 和 evidence schema
必须得到相同 digest。无法提供稳定快照的实现可以只提供随 Emission 冻结的 TypedObservation，
但不得用不稳定内部状态参与严格 PSS、Coverage 或 Oracle 判定。

## 9. Produced Item 模型

第一版至少包含：

- `MessageEnvelope`：不可变消息、副本 lineage、源/目标和依赖；
- `TemporalRegistration`：one-shot timer、periodic pulse 或 sleep wakeup 的 deadline 和 owner；
- `TemporalCancellation`：取消已有 temporal item 的确定性声明；
- `HostEffect`：持久化、同步、应用或其他宿主副作用请求；
- `HostCallback`：需要外部环境稍后返回的回调；
- `ClientResponse`：对外可观察但不可再次调度的响应；
- `TypedObservation`：供可信 monitor 使用的版本化观察。

Adapter 输出的 item 先经过 schema、ID、owner、依赖、payload digest 和 Manifest capability 校验，
校验通过后才进入 Runtime 状态。

### 9.1 通用生命周期

```text
Produced -> Blocked -> Enabled -> Selected -> Completed
             │           │           │
             └──────────> Canceled/Failed/Dropped
```

不是所有 item 都经历所有状态。例如无依赖消息可以从 `Produced` 直接进入 `Enabled`；观察在提交
后直接完成；被旧 incarnation 淘汰的 temporal item 进入 `Canceled`。

依赖只引用稳定 ItemID，Runtime 验证无环。v2 不再要求“每批恰好一个 acknowledge”或“同一节点
只能存在一个 outstanding batch”。一个节点可以同时拥有多条消息、timer 和互不相关的 effect。

## 10. 消息语义

### 10.1 所有权和阶段

```text
Captured (payload frozen in Runtime)
  -> Released (dependencies satisfied)
  -> Delivered | Dropped
```

- `Captured`：Adapter 已在 yield 中交出不可变 payload，但发送前置条件尚未满足；
- `Released`：Runtime 已取得网络所有权，可无限期保留；
- `Delivered`：目标 Adapter 成功处理并到达新的 yield；
- `Dropped`：Runtime 显式终结该副本。

若消息依赖一次持久化或同步 effect，只有 effect 完成后才从 `Captured` 转为 `Released`。如果源节点
在 release 前 crash，该 incarnation 的 volatile captured 消息被取消；已经 released 的消息不会
因为源节点 crash 而消失。

### 10.2 长时间保留

Runtime mailbox 不属于任一节点：

- 目标节点停止时，消息保留且 delivery 暂时不 enabled；
- partition 只阻止 delivery，不删除消息；
- heal 或目标 restart 后，仍可选择投递原消息；
- duplicate 创建新 CloneID，原副本保持不变；
- 每个副本只能各自投递或丢弃一次。

这解决“节点主动动作必须立即完成消息”的限制，使测试可以构造长期延迟、跨 crash/restart 的在途
消息和不同消息之间的时序组合。

### 10.3 Adapter 限制

Adapter 不得自行把截获的节点间消息发送到真实网络。若系统内部无法拦截发送，必须使用 loopback
代理或传输替换层；做不到时应明确声明消息控制能力不支持。

## 11. 虚拟时间和自然超时

Runtime v2 使用单调全局逻辑时间，但不向 Agent 暴露自由 `AdvanceTime(to)`。Adapter 在 yield
注册三类协议无关时间项：

```text
TemporalItem
├── OneShotTimer     明确注册的一次性 timer
├── PeriodicPulse    Tick 等周期性宿主时钟脉冲
└── SleepWakeup      sleep/wait 的唤醒事件
```

每项包含稳定 `TemporalID`、owner/incarnation、clock domain、绝对 deadline、受限 callback 和
允许的 clock error。令当前有效时间项的最早 deadline 为 `T`，Runtime 只把 `[T, T+E]` 内的
时间项枚举为 `FireTemporalEvent`。第一版冻结 `E=0`：只能选择最早 deadline；多个同 deadline
项目可以分支选择。普通消息、effect、crash 等 action 仍可与最早时间事件竞争，表达“消息先到”
或“超时先到”。

执行 `FireTemporalEvent(item_id)` 是一个原子复合周期：

```text
选择 Runtime 枚举的 TemporalID
  -> now = max(now, item.deadline)
  -> 记录 ClockAdvanced(from, to, reason, item_id)
  -> Adapter 执行对应 callback / tick / wakeup
  -> RunUntilYield + Collect
  -> 注册、取消下一批 temporal item
```

Agent 只能选择 `TemporalID`，不能提供 `to`。时间不得倒退，不能跨过更早的有效时间项。选择一个
时间项后，同 deadline 的其他项目仍保持最早；可以先执行当前时刻的普通动作，再执行剩余时间项。
这既允许 callback 调度延迟，也不会制造不受约束的节点时钟偏移。

### 11.1 sleep/wait

Adapter 遇到 `sleep(d)` 或等价阻塞等待时，输出 `SleepWakeup(deadline=now+d)`。当该 wakeup 被选择
时，Runtime 在动作内部快进并唤醒节点。如果没有普通动作且它是唯一最早时间项，Coordinator 可以
把它作为无分支机械转换执行，但仍须进入 trace 和 primary work。快进不能越过更早 timer/pulse。

### 11.2 tick-based 实现

对于 etcd/raft 一类由宿主周期调用 `Tick()` 的库，Adapter 不把 election timeout 伪造成一个
可直接触发的 callback，而是为每个运行节点注册 `PeriodicPulse(now+tick_period)`。每次选择 pulse
只调用一次 `Tick()`，随后运行到 yield、收集输出并注册下一 pulse。选举和 heartbeat 由协议内部
elapsed counter 累积后自然产生。

若多个节点 pulse 同时到期，`E=0` 允许在它们之间选择先后，但在其余同 deadline pulse 被执行、
取消或节点 crash 前，不能让某节点进入下一周期。第一版禁止批量 Tick；只有未来能机械证明中间
无可观察输出、竞争动作或 timer reset 时，才能增加保守的 pulse coalescing。

局部时钟漂移、不同节点时钟速率、租约误差和 `E>0` 留到扩展版本，并由 Profile 显式冻结。

## 12. 受控随机性与严格重放

随机性独立于时间调度。v2 通过通用 `EntropySource` 显式控制协议、workload、scheduler 和 Agent
的随机域，禁止它们共享一个会被调用顺序污染的全局流：

```text
CampaignSeed
├── SUTEntropySeed
│   └── H(SUT, NodeID, Incarnation, DomainID)
├── WorkloadSeed
├── SchedulerSeed
└── AgentSeed
```

第一版冻结一个版本化、跨 Go 版本稳定的确定性生成算法，例如
`hmac-sha256-counter-v1`。每次抽样生成：

```text
RandomDrawRecord
  = DomainID + NodeID + Incarnation + Ordinal
  + Operation + Arguments + Result
```

Fresh run 根据分域种子计算结果并写入 tape；strict replay 必须逐次匹配 domain、ordinal、operation
和参数，再返回并校验保存结果。请求数量、顺序、范围或结果任一不同都产生
`replay-divergence`。仅记录原生随机结果但无法在 replay 中重新提供，不足以声明严格重放。

随机结果在第一版不是在线 Action。Random、DFS、Agent 和专家计划使用相同的预冻结 SUT seed
集合，Agent 不能在运行中挑选一次抽样结果。以后若要系统探索有限随机选择，应在 run 前提交受限、
可校验的 `EntropyPlan`，或由支持 continuation 的 Adapter 暴露有界 ChoiceRequest；不能让 Agent
返回任意值。

### 12.1 原生接入策略

Adapter 按以下优先级接入随机性：

1. 使用原实现公开的 per-instance RNG/seed 接口；
2. 在隔离测试进程中截获原生 entropy API，并按 node/incarnation/domain 分流；
3. 若只能观察而不能重新提供随机值，则声明 `strict_entropy_replay=false`；
4. 禁止通过反射、私有状态写入或未审计源码转换伪造 unmodified 接入。

固定的官方 etcd/raft v3.6.0 没有 `Config.Rand`，其 election timeout 使用进程级
`crypto/rand.Reader`。Legacy Adapter 若保持官方模块不变，应在隔离、串行的测试边界中安装确定性
Reader，并在每次 `NewRawNode/Tick/Step` 周围绑定活动 node/incarnation domain；域外读取、并发读取
或其他组件使用同一 Reader 都必须失败。若无法通过 conformance 证明隔离，就不能声明自然选举
timeout 严格重放。

桌面 fork 的 `Config.Rand` deterministic simulation hook 可以作为工程对照，但不能在
Contract-only 正式实验中表述为官方 v3.6.0 原生能力。仿真 entropy 不得用于真实密钥生成；测试
密钥和证书应作为冻结 fixture 输入。

## 13. crash/restart 与 incarnation

每个节点具有：

```text
Stopped | Starting | Running | Crashing
NodeID + Incarnation
```

第一版冻结 `power-loss` crash：

- crash action 只在 Adapter 稳定 yield 边界发起；
- 成功 crash 后当前 incarnation 终止；
- 其未完成 timer、callback、volatile effect 和未 released 消息被取消；
- 已完成 durable effect 保留；
- Runtime mailbox 中已 released 消息保留；
- 发往该节点的消息保留，但停止期间不可投递；
- restart 创建 `Incarnation + 1`，只能读取已完成的 durable image；
- 旧 incarnation 后续输出一律视为 Adapter 违规。

`Crash` 的抽象含义由 Runtime 固定，但原生实现由 Adapter 完成：嵌入库可以丢弃实例，进程系统
可以由监督器停止目标进程。不得简单假定被测共识本身存在一个语义完整的 `Crash()` API。

第一版不模拟任意磁盘扇区撕裂。graceful stop、process kill、host reboot 和 partial-write 可以在后续
以新的 versioned crash mode 加入，不能用自由字符串扩展。

## 14. 宿主副作用和持久性

协议可能请求：写入日志、写入元数据、同步、应用状态机、更新快照或执行其他外部操作。Adapter
将它们翻译为 `HostEffect`：

```text
EffectID
EffectKind
Owner NodeID/Incarnation
Opaque Request Envelope + Digest
Dependency ItemIDs
Allowed Results/Failures
Durability Class
```

Runtime 不解释日志条目或 HardState。它只控制 effect 何时成功、何时以允许的结果失败，以及哪些
item 依赖它。

effect 完成后，Adapter 必须在新的 yield 中给出可审计的 durable checkpoint/digest。restart 使用
该完成边界恢复，而不是使用 crash 时仍在内存中的 visible state。实现可以把具体 bytes 保存在
Adapter 管理的测试存储中，但 checkpoint identity、完成顺序和 digest 必须进入 trace。

第一版支持的失败集合由 `AdapterManifest + Trial Fault Model` 取交集。Agent 不能自行发明返回码。

## 15. Manifest 与机械资格

Manifest 至少声明：

- Adapter schema/version、实现 identity 和构建 identity；
- 节点、输入和 payload schema；
- 支持的 Action、Produced Item 和 effect kind；
- 消息截获、temporal item 类型、clock domain、`E`、pulse/sleep、crash/restart 和 durable
  checkpoint 能力；
- entropy provider/algorithm、domain/reset policy、原生注入方式和 RandomDraw schema；
- `strict_yield`、`strict_entropy_replay`、`strict_replay` 和 evidence schema；
- 允许的 crash mode、effect failure 和资源上限；
- 明确的 unsupported 能力及稳定 reason code。

机械资格流程：

```text
Profile requirements
  ∩ Runtime capabilities
  ∩ Adapter Manifest declarations
  ∩ Conformance validated capabilities
  -> QualificationReport
```

资格只做集合包含和版本兼容判定。不能从文档描述或协议名称猜测能力，也不能因为某次场景恰好
运行成功就提升能力等级。

## 16. Evidence、PSS 与 Coverage 边界

控制层只生成协议无关的 Control Record 和实现证据 envelope。M5.7 起，所有实现先映射到一个固定的
Core PSS IR，而不是让每个 Family 自由决定整个状态形状：

```text
ImplementationEvidenceVn
  -> Semantic Mapping
  -> Core PSS IR Vn
       +-> generic canonical key / discovery ledger
       +-> optional Extended PSS / family Oracle input
```

Core PSS State 由 Runtime 直接提供的 Control Context 与 Mapping 提供的 Semantic Graph 组合。
Control Context 包含 participant lifecycle/incarnation、link connectivity、pending item 的种类/路由/
保守因果形状和最近 temporal frontier；它排除随机 ID、绝对时间、opaque payload 与 host microstep。
Semantic Graph 使用封闭实体 `Participant/Epoch/DecisionUnit/Value/Evidence`、封闭关系
`belongs-to/proposes/supports/depends-on/conflicts-with/precedes/decides/persists/applies` 和通用阶段
`unknown/proposed/supported/accepted/decided/applied`。Participant 另有封闭执行 mode
`inactive/passive/contending/coordinating`，避免把 Raft role 写进 Core。Raft entry、EPaxos instance 和 HotStuff block
都映射成 DecisionUnit；协议特有的 log conflict、fast/slow path 或 lock/QC shape 进入版本化 Extended
PSS，不能反向扩展 Runtime 或让通用 ledger 按协议分支。

Family Evidence 可以继续作为实现证据到 Core IR 之间的可选复用层，但不再是每个新协议的必需新状态
模型。具体字段不能进入 Runtime core，Core 与 Extended 指标必须分别报告。

v1 中直接假定 snapshot JSON 存在 `driver.nodes` 的 matcher 必须在迁移时改为消费 Core/Extended PSS，
不能把某个 Driver 的 snapshot 布局当成通用接口。

PSS 状态发现数和 Coverage obligation 仍承担不同职责：

- PSS 发现曲线表达搜索触达了多少规范化协议状态，没有固定完备分母；
- obligation coverage 表达冻结义务是否取得强证据，有固定分母；
- 二者都不得参与真实缺陷的 kill 判定，也不能单独证明 Agent 优于基线；
- 最终方法效果仍由隐藏独立根因检出、正确 control 误报和等预算成本评价。

## 17. Trace 与严格重放

v2 trace 至少记录：

- Manifest、Runtime、SUT、Adapter、Profile 和初始状态 identity；
- 每一步完整 enabled-set digest；
- 被选择的 ActionID、参数和选择来源；
- CommandID、YieldID、Emission digest 和 Action Result；
- item 生命周期迁移、依赖满足和取消原因；
- logical time、每次复合时间动作的 `ClockAdvanced`、node lifecycle 和 incarnation；
- 分域 seed identity、每次 RandomDraw 请求/结果和 entropy tape digest；
- observation/evidence digest；
- Oracle finding、预算和终止原因。

严格回放使用同一冻结身份和 decision log，逐步比较 enabled-set、yield、emission 和状态 digest。
任何一处不同都产生 `replay-divergence`，不得继续用后续相似 action 掩盖差异。

v1 工件保持其原 schema 和 evaluator；v2 引入新 schema/version。迁移验收比较语义与实验结论，
不要求两个不同控制模型的 trace fingerprint 相同。

## 18. Adapter Conformance Suite

Conformance Suite 位于 Adapter 之外，由框架维护。Adapter 不能通过返回 `true` 自证能力。

### 18.1 通用必测项

- 相同 seed、命令和 decision log 得到相同 yield/emission digest；
- 同一 YieldID 重复 Collect 返回相同结果；
- duplicate 产生稳定 lineage，原消息保持不变；
- released 消息可跨源 crash、目标 crash 和 partition 长期保留；
- heal 不会隐式投递消息；
- Agent/Adapter 不能提交任意时间数值；
- `E=0` 时只有最早 deadline 的 temporal item 可选，同 deadline 项都可选；
- FireTemporalEvent 原子记录时钟推进并只执行所选 callback；
- sleep 快进不能跨过更早 temporal item；
- periodic pulse 每次只执行一次，未证明前不能批量 Tick；
- 已取消 temporal item 不再 enabled，旧 incarnation item 不可执行；
- 相同 SUT seed 产生相同 RandomDraw tape；
- 不同 node/incarnation/domain 的随机流不受跨节点调用顺序影响；
- replay 的 entropy 请求数量、范围或顺序不同会稳定 divergence；
- 域外、并发或未声明的原生 entropy 读取被拒绝；
- crash 取消旧 incarnation 的 volatile item；
- restart 不读取未完成 effect 的 visible state；
- 依赖未满足的 effect/message 不 enabled；
- Adapter 无法在 yield 之后产生未记录输出；
- 非法、重复或旧 incarnation command 被稳定拒绝。

### 18.2 外部资格

每个能力形成独立 conformance evidence 和稳定 reason code。部分通过是允许的，但 Profile 只能使用
已验证能力。第二个实现不得为了通过测试去修改 Runtime；若确需修改公共抽象，必须重新运行所有
已接入 Adapter 的 conformance 和 replay 测试。

Portable CFT Profile v2 把基础控制 witness 与 strict replay witness 分开：消息跨 restart 的所有权
或 opaque invoke 接受成功，不能因为 SUT 的墙钟不可重放而被抹去；反过来，基础 witness 也不能
替代 strict replay。二者分别生成 capability 状态，完整资格仍要求全部 required capability validated。

## 19. Agent 接口

Agent 位于控制层上方，不获得 Adapter 对象或任意 SUT 代码执行权。Runtime 是 Agent 的受限工具，
而不是与 Agent 平级的一种搜索方法。Agent 可以接收：

- 冻结的协议/Family 知识投影；
- Manifest 中已验证的能力、输入 schema 和 fault model；
- Runtime 当前提供的 opaque/stable ActionRef 与全局候选 WorkItem；
- 与精确 prefix 绑定的 PSS/Risk progress、workload phase、fault usage、Coverage debt 和机械 finding；
- token、decision、primary work 和运行数预算。

Agent 输出分三层：

1. **TestHypothesis**：引用冻结的 obligation/risk/capability ID，提出可能错误但可验证的协议级测试假设；
2. **EpisodePlan**：选择语义目标、全局 WorkItem 优先级和冻结搜索算子，由可信 Search Kernel 具体化；
3. **微观选择**：只能引用当前 enabled ActionID 或受限 selector，由确定性 concretizer 解析。

假设和 plan 不使用未来绝对 decision number。违反 capability、fault envelope 或 enabled 约束时机械拒绝，
形成计费的 `EpisodeReport`，允许 Agent 修正，而不是要求每个提议在执行前被证明正确。可信 compiler
在每一步从 `EnabledActions + Core/Extended semantic view` 解析具体动作。Agent 可以读取冻结的协议知识
和批次级 PSS/Coverage/near-miss 反馈，但不能读取 candidate/control 身份、补丁、根因、已知触发轨迹
或私有 Oracle 结论。

对时间，Agent 只能选择 Runtime 提供的 `FireTemporalEvent(TemporalID)`，不能提交 deadline 或 delta；
对随机性，第一版只能选择预冻结 SUT seed 的 run，不能指定一次 RandomDraw 的返回值。

近失配反馈必须从冻结的 Core/Extended PSS Mapping 与 Profile 机械产生，不能泄露 candidate identity、root cause、
隐藏 monitor 或真实触发条件。Agent 不能新增 Action kind、绕过 capability、修改当前 Profile 分母，
也不能声明 `covered=true`。

这保留 Agora “让 Agent 承担协议理解、假设生成、测试策略和反馈修正”的主体性，同时把实际环境控制、
重放和结果判定留在确定性可信路径中。当前生产实现仍只有 Agent-v1 frontier permutation；上述接口是
`feature/agentic-consensus-testing` 的 A1–A4 目标，不得被误写为已实现能力。

## 20. 迁移路线

### M5.0：设计审查与冻结

- 审查本文的职责边界、Action/Item taxonomy、enabled、temporal、entropy 和 yield 语义；
- 冻结第一版 ID、Manifest、trace 和 capability 命名；
- 明确 v1 保留范围和 v2 不兼容边界；
- 此阶段不修改 Runtime 实现。

退出条件：团队可以明确判断任一逻辑属于 Runtime、Adapter、Evidence、Oracle 还是 Agent，并且对
本文第 24 节的决策项达成一致。

### M5.1：协议无关 fixture

- 新建 `internal/control`：纯数据模型、ID、状态机和 capability；
- 新建 `internal/controlruntime`：enabled、提交、item 生命周期、trace 和 replay；
- 新建 `internal/controlentropy`：版本化分域生成器、RandomDraw tape 和 replay validator；
- 新建 `internal/conformance`：外部测试套件；
- 实现一个不含 Raft 类型的确定性 fixture Adapter；
- 覆盖消息保留、最早 temporal event、periodic pulse、sleep wakeup、分域 entropy、crash
  incarnation、effect durability 和严格 replay。

退出条件：新包不 import v1 `driver/host`、etcd/raft 或 `families/raft`，相同 decision log 多次执行
得到一致 digest。

### M5.2：Legacy Ready Bridge

- 在 Adapter 内把 etcd/raft `Ready` 映射为 Message、Effect、Observation 和 callback；
- 把每节点宿主 `Tick()` 映射为 `PeriodicPulse`，由连续 pulse 自然产生选举/heartbeat；
- 对官方 v3.6.0 在隔离 Adapter 边界验证 per-node entropy Reader 和严格 replay；
- `Ready`/HardState/Entry 等类型不越过 Adapter/Evidence 边界；
- 用 v2 重建少量现有公开场景，并比较 Oracle 结论；
- v1 路径保留，直到 v2 验收完成。

当前进度（2026-08-07）：M5.5a 已实现版本化 Qualification Profile、typed Manifest requirement、
枚举 Unsupported、五态 capability 结果、JSON schema、接入模板与 fresh runner。etcd/raft v2 对
`portable-cft-control-v2` 下 etcd/raft 的 8 项 required capability 全部 validated，HashiCorp Raft
有 3 项 validated、6 项 Unsupported，保持 `qualified=false`。该准入不改变 v1 删除门；进程级
隔离、HashiCorp strict replay 和 v2 可信评测迁移仍未完成。M5.4d 已用 module/source-bound 审计
冻结官方 v1.7.3 的 clock、entropy 和 strict replay Unsupported；M5.4e 又从两个 fresh
QualificationReport 机械冻结能力矩阵，共同 validated 交集为消息所有权、生命周期代际和 opaque
invoke。后续消费者只能使用目标实现 validated 的能力，跨实现比较只能使用交集。
M5.5a 在不修改冻结资格 digest 的前提下新增派生 ControlSurfaceReport，将五个通用运行表面与五个
确定性测试保证分开，并分别记录 declared/validated control。HashiCorp 的结果因此表达为 5/5
表面存在、3/5 scheduler-owned validated、0/4 required guarantee，而不再把 `3/8` 当成功能比例。

### M5.3：外部 conformance 与接入模板

- [x] 把 conformance suite 变成每个 Adapter 的机械准入门；
- [x] 提供 Adapter 模板、Manifest schema 和 unsupported reason code；
- [x] 生成 capability/qualification 报告。

### M5.4：第二个异构 CFT Adapter

选择控制表面不同于 etcd/raft 的实现，例如非 `Ready` 拉取式、事件循环式或进程代理式系统。
Runtime 不得出现该协议的名称、类型或条件分支。

退出条件：第二实现无需修改公共语义即可通过其声明能力的 conformance，并运行消息、自然时间、
受控 entropy、crash/restart 和 evidence 场景。未达到此门槛前不宣称通用控制层完成。

M5.4e 收口结果：第二实现已复用消息、lifecycle 和 opaque invoke 语义，但自然时间、受控
entropy 和 strict replay 因官方 v1.7.3 无注入边界而稳定 Unsupported。因此 M5.4 实验已停止扩张，
但上述完整退出条件没有满足，仍不宣称完整通用控制层。

### M5.5 以后

依次迁移 Core/Extended PSS、Coverage Matcher、搜索基线、Agent Planner 和 benchmark。控制层稳定
之前不扩大 Agent 数量，也不以 v1 的 Raft 特有状态继续堆叠义务。

M5.5a 已先完成场景表面/控制等级/确定性保证分层。M5.5b 已实现独立进程、显式 readiness endpoint、
数据目录和 opaque 客户端调用组成的最小黑盒 Target Envelope，并用真实子进程验证 kill/restart、
drop/deliver 和 pending call 跨 incarnation。该 backend 停留在 observable/interceptable，不冒充
scheduler-owned 或 strict replay。M5.5c 已用两个独立子进程验证 connection-level gateway 的 opaque
byte forwarding 与 partition/heal；它明确不提供 framing、message ID 或 strict replay。通用黑盒扩张
在此停止。M5.6a 没有新增 backend selector，而是在唯一 `control.Adapter` 上增加
`ApplyRuntimeAction`：Runtime-owned Partition/Heal 仍由 Runtime 提交语义状态，Adapter 可把同一 Action
映射到 Gateway。etcd/raft mailbox 与 test-only Gateway wrapper 已证明 Action 身份完全相同；任意拓扑
typed binding、外部 actuation evidence 和生产黑盒 Adapter 仍未完成，因此不能提高 Gateway 资格等级。

M5.6b 将 Runtime private partition JSON 提升为公共 `control.PartitionParameters`。构造与解码统一执行
节点集合规范化、左右对称、稳定 ID 和重叠检测；`blackbox.GatewayBinding` 再相对于显式 node/directed
link inventory 解析 crossing gateways。该 binding 不推断遗漏链路、不执行原子回滚，也不管理重叠
partition 引用计数，所以仍不是 production blackbox Adapter 或 scheduler-owned Qualification 证据。

M5.6c 为 Runtime-owned Action 补充 `Adapter.CheckRuntimeAction`，选择时会过滤外部 gate 未启动、关闭或
状态漂移等不可执行情况。通用 `GatewayActuator` 对重叠 partition 使用 Gateway 引用计数，并在多 Gateway
调用中途失败时逆序回滚 controller gate state；三个独立子进程和两条 Unix Gateway 已验证实际阻断、
共享引用与最终恢复。该顺序调用不构成网络原子 cut，rollback 不能恢复旧连接或在途字节，故不改变
connection backend 的 interceptable、非 strict-replay 边界。

M5.6d 没有改变 Runtime 行为，而是在既有 ControlGrade 中加入 `scheduler-actuated`，并用
`ControlPathAssessment` 从 witness facts 机械推导等级。冻结矩阵把两个 message/scheduler-owned 路径的
strict replay 差异，与 Gateway connection/scheduler-actuated、controller-only atomicity 放在同一视图；
grade 与 guarantee 不再互相暗含。Gateway 仍只有 path witness，不是生产 Adapter Qualification。

M5.7 先收紧新目标的边际接入面，不立即实现第三个 Adapter：具体目标只提供 Execution Binding 和
Semantic Mapping；规划中的共享 Adapter kit 复用 Manifest、Check、Yield、Collect、identity、digest、
finding 和 conformance 组装。kit 只能存在于具体 Adapter 内部，Runtime 仍只依赖唯一
`control.Adapter`。基础接入优先 Submit、lifecycle、message、partition、最早 temporal item 和只读
observation；effect/callback/storage/entropy 按实际能力加入，不再作为首版模板清单。

M5.7a 已实现 `internal/psscore` 和 etcd/raft 公开 Evidence Mapping，并用真实三节点自然选主验证
行为变化与重复投影稳定性；Runtime/Action/Adapter schema 均未修改。两个现有 Adapter 的只读审计只
发现少量防御性样板，Check、yield、Evidence 与 entropy 语义并不相同，因此没有创建共享 kit。之后
M5.8 才对 `efficient/epaxos` 做 feasibility spike。EPaxos 只能新增 Binding、Mapping、fixture 和
composition；缺少持久恢复、clock 或 stable yield 时保持 Unsupported。

M5.8a 已完成固定版本的第一轮检查：生产构建与三节点单命令 smoke 成功，输入、peer TCP 和语义字段
表面存在；stable message item、yield、可注入 clock、恢复路径和 strict replay 尚不存在或未证明。
冻结报告的结论是 `proceed-limited`，只允许 test-only message-port worker spike，不授权完整 Adapter、
Qualification 或共享 kit。M5.8b 若不能在零 Action/Runtime/Core PSS 修改下冻结并 release/drop 一个
真实 frame，则记录 Unsupported 并转入 v1 消费者迁移/删除门。

M5.8b 已在 212 行 test-only Go、零生产 Go 下得到 worker witness：官方 5,139 字节 `Commit` 帧跨两次
底层 Write，证明 chunk 不是消息；EPaxos codec-aware assembler 能恢复完整 bytes、生成稳定 ID，并
release 5,139/drop 0 字节。framing 因而明确属于目标 Binding，Runtime 不增加协议 codec。probe 未覆盖
自动构造器安装、三节点闭环或 Runtime integration，故资格保持未授予。当前按停止线进入 M5.9 v1
消费者迁移/删除门，EPaxos 最小 Binding 在减负后恢复。

### M5.14 以后：从可调度测量转入测试闭环

M5.13 的单调用 LLM smoke 已证明模型 transport、严格 proposal decoder、执行失败账本和成本记录可以
连通，同时也机械暴露了 `decision N -> Action kind/node` 无法预测未来 enabled frontier。原计划的一次
同形 repair 不再作为主线。后续顺序冻结为：

1. **Experiment admission**：统一 Runtime 不拆 strict/best-effort 两套模式；Experiment 声明所需
   capability，composition 将其绑定到 digest-bound QualificationReport，只有 required subset 全部
   `validated` 才能进入相应执行。基础控制可运行不等于 strict benchmark 准入。
2. **Workload/Fault Provider**：Provider 只能调用 `OfferInvoke`、`OfferPartition` 等 Runtime 入口；
   Runtime 冻结 ID 后，`EnabledActions` 仍是唯一候选集合。工作负载、故障次数/并发上限及全部
   setup/offer/primary/replay 成本进入同一预算。
3. **ExecutionBundle + Oracle/DefectBench**：在第一个 v2 Oracle 消费者出现时才定义最小 bundle，
   先迁 TraceIntegrity/Agreement 和公开 calibration，不预建完整 evidence/campaign 目录树。
4. **强 baseline**：先 action-class random、trace mutation、PSS-guided corpus；PCT/POS/DPOR 等有
   具体缺口后再加入。
5. **Agentic Episode**：先以 deterministic fixture 建立 TestHypothesis/EpisodePlan/EpisodeReport，
   再实现 single Explorer；随后用相同总模型预算比较 single Agent 与 Protocol/Hypothesis + Explorer，
   由消融决定是否保留多 Agent。

M5.14 已完成第 1 项，M5.15 已完成第 2 项。M5.16 又完成第 3 项：最小
`ExecutionBundle` 绑定完整 trace、prepare transition、Evidence/client history、Core PSS、
Qualification、Replay 和 work ledger。target-owned `DecisionProjector` 将 etcd/raft Evidence 转换为
position/value digest，通用 TraceIntegrity/Agreement 不导入 Raft 类型。公开 calibration
以共同 96 decisions/98 primary work 得到 control-pass/killed，PSS/Coverage 不参与 verdict。

M5.16R 随后将 legacy production import edge 从 28 降为 0，并删除 v1 Engine/Host/Driver、
Coverage/Campaign、Raft Family、onboarding、旧 Agent 和 migration 实现锥体。历史文档/JSON
保留为 archive，当前 `make audit-no-v1` 防止可编译源码回流。M5.17a 已完成第 4 项的第一部分：
action-class random 只在 Runtime enabled 集的 digest-bound FaultEnvelope 子集中按 ActionKind/成员两级
均匀采样，原均匀 Action random 不变。同一公开 candidate 在 fixed policy 下被检出、在发现更多 Core
PSS 状态的 action-class seed 1 下 survived，证明 coarse discovery 不能代替 root-cause detection。
M5.17b 随后完成 trace mutation：按 source 顺序选择第一对相邻 message deliveries，执行 exact prefix、
adjacent swap 和 digest-bound priority suffix；引用 ID 不 enabled 时产生稳定、计费的失败，不静默
fallback。source 与 mutation 的完整方法成本为 196 primary / 196 replay，而不是只报告 mutation 自身
的 98/98。

最新审计后增加三个前置阶段，不直接进入 PSS-guided corpus：

1. **M5.17bR2 executable-path pruning（已完成）**：删除 pre-admission 的 fixed/random/Planner/DeepSeek/
   `ExecuteLegacy` 在线路径和无消费者 transport；保留历史 Markdown/JSON，冻结 M5.15–M5.17b 身份。
2. **M5.17c0 experiment semantics（已完成）**：所有策略共享 admissible frontier；
   WorkloadRouter 从 PSS Mapper 拆出；`budget-exhausted/quiescent/configured-stop` 成为可重放
   目标终止；pending workload 被记录而非在 Oracle 前拒绝。v2 记录 runtime/admissible
   digest 与选中 ActionID，fresh replay 重算终止、fault usage 和 Invoke 路由。框架、trace、
   资格和投影错误仍是 invalid。
3. **M5.17c1 corpus trust prerequisites（已完成）**：有序 MutationSourceCorpus 不绑定单一 source
   policy；重复 ActionID 由 occurrence 消歧；PSS feedback 从 bundle/Trace/Evidence 重新投影；
   MethodLedger 强制记录 source/proposal/execution/失败和完整方法成本。

M5.17c2 随后已实现 batch PSS-guided corpus 和 qualified uniform random；首个 guided proposal 因精确
ActionID 不再 enabled 而失败，未临时加 structural fallback。其他 fault/structural/near-miss 指标仍独立报告，
不预先混合。M5.18a 已在 Experiment/evaluator 层增加显式 ExecutionBundle v3、OperationHistory、
MethodSpecDigest 与 evaluator-owned fresh execution。v3 从既有 Trace/Workload/ClientHistory 重建
invoke/return，不增加 Runtime Action、Item、消息状态或调度分支；默认 v1/v2 identity 保持冻结。
M5.18b0 又在 Experiment 之上接入不调用模型的 Guarded TestIntent 宏观 compiler：它只选择
冻结 qualified backend，不读取或修改 Runtime enabled/ActionID；执行后从真实 Trace 验证 hard
ActionKind。M5.18b1 已接入单次受限 transport；M5.18b2 已从完整 bundle/Mapper 重算 defect-blind
batch feedback，且冻结反馈只能改变 intent preference。两个阶段都没有修改 Runtime 或在线调度权限。
M5.18b3 又将 execution 与 workload completion 分离，冻结 source/follow-up seed 和完整计费，并通过
现有 executor 保存 seed-4 hard-action miss；它同样没有修改 Runtime 或给 Agent 增加在线权限。
M5.18b4-pre 进一步区分 Runtime-supported、Experiment-producible 和 backend-selectable Action；
macro plan 与 execution seed 分离，并把 execution validity、intent reachability 和 Oracle outcome
拆成独立结果轴。它仍复用唯一 executor，且没有新增 Runtime Action 或开放 DSL。
M5.18b4 request-freeze 只在 Agent/transport 上层工作：同一 builder 生成 feedback=null/
trusted-feedback 两份精确 request，digest-bound freeze 绑定 hard baseline、seed、预算和 1-call/
0-retry 限制。它没有执行 follow-up，也没有修改 Runtime、PSS 或 Oracle。

Workload 只负责声明外部输入和测试意图，不拥有正确性。`ExpectedStatus` 不能决定一条运行是否有效；
planned/offered/completed/pending/actual response 都必须进入证据。目标选择由 target-owned、确定性的
WorkloadRouter 完成，PSS Mapper 只投影状态。零个或多个 routing candidate 是可观察 guard/result，
不能自动变成框架错误，否则可能隐藏无主、多协调者或停滞现象。

ControlSurfaceReport 与 ControlPathAssessment 停止扩展并只作为既有 portability 研究工件复验；生产
准入只消费 Manifest、外部 Conformance 和 QualificationReport。Core PSS 继续只用于 coarse feedback
和 discovery，不参与未证明保守的状态等价、DPOR 或 visited-state pruning。

## 21. 第一实现阶段验收标准

M5.1 必须同时满足：

- [x] `internal/control` 和 `internal/controlruntime` 不依赖任何协议或 v1 Host/Driver；
- [x] 一个节点可同时存在多个 outstanding message/timer/effect；
- [x] released 消息能跨 crash/restart/partition 保持；
- [x] 不存在 Agent 可提交的自由 `AdvanceTime(to)`；
- [x] `E=0` 只枚举最早 temporal item，并允许同 deadline 分支；
- [x] FireTemporalEvent 原子产生可重放 ClockAdvanced 和 callback result；
- [x] periodic pulse、one-shot timer 和 sleep wakeup 共用同一公共模型；
- [x] 分域 entropy 不受跨节点执行顺序影响，RandomDraw tape 可严格重放；
- [x] crash/restart 使用 incarnation 并隔离旧输出；
- [x] effect 完成与 durable checkpoint 可重放；
- [x] yield/emission 有稳定 digest，重复 Collect 幂等；
- [x] 非法 action、循环依赖和旧 incarnation 输出被机械拒绝；
- [x] conformance 在 Runtime 外判定能力；
- [x] fixture 的两次 fresh run 与一次 replay 指纹一致；
- [x] v1 功能测试和冻结工件不被修改；只在既有 architecture test 中增加 v2 依赖守卫。

只有这些条件通过后，才开始编写 etcd/raft Legacy Ready Bridge。

## 22. 目录建议

```text
internal/control/          v2 公共数据模型、ID、Manifest、Action、Item
internal/controlruntime/   v2 确定性状态机、enabled、trace、replay
internal/controlentropy/   分域 deterministic entropy 与 RandomDraw tape
internal/conformance/      与具体 Adapter 分离的准入套件
internal/psscore/          固定 Core PSS IR、Runtime Control Context、规范化与 digest
adapters/fixture/          无协议 fixture Adapter
adapters/etcdraftv2/       官方 RawNode Binding、Core Mapper 与 DecisionProjector
adapters/hashicorpraftv2/  第二实现的部分资格 Binding
internal/controlexperiment/ admission、workload、policy、report 与 ExecutionBundle
internal/semantic/         最小 decision observation
internal/oracle/           v2 TraceIntegrity/Agreement
internal/defectbench/      最小 control/candidate evaluator
```

M5.16R 已删除 v1 的 `internal/engine`、`internal/host`、`internal/driver` 和
`drivers/etcdraft`，当前仓库不再存在并行 Runtime。

## 23. 防漂移检查表

每次提交前检查：

- 这段代码是否需要知道 term、view、QC、Ready 或具体协议消息类型？如果需要，它不属于 Runtime；
- Runtime 是否因为 Adapter 类型不同而走特例分支？如果是，抽象尚未统一；
- Adapter 是否决定了消息/timeout/effect 的调度顺序？如果是，控制权泄漏；
- 是否使用 wall-clock sleep/drain 得到“稳定”输出？如果是，不能声明 strict replay；
- 是否存在没有 Action/Item/Manifest 记录的非确定性输入？如果有，trace 不完整；
- Agent 是否能提供任意时间数值或一次 RandomDraw 结果？如果能，控制面过宽；
- 不同节点是否共享一个受调用顺序影响的随机流？如果是，搜索方法比较会被混淆；
- Agent 是否能提交 enabled 集以外的动作或自由 payload？如果能，边界失效；
- Coverage/PSS 是否直接读取实现私有 snapshot 布局？如果是，Evidence 层缺失；
- 新协议是否要求重新定义 PSS state 结构而不是只写 Core IR mapping？如果是，Core PSS 边界失效；
- 新 Adapter 是否重复实现 identity/yield/collect/digest 样板？如果是，应先抽取被两个目标消费的 kit；
- 新能力是否只有 Manifest 声明而没有外部 conformance 证据？如果是，不能 qualified；
- 是否为了一个候选提交反向定制控制能力？如果是，benchmark 与接入方向倒置；
- 第二实现是否要求修改 Runtime？如果是，必须重新审查公共语义而不是添加协议特例。

## 24. 需要在实现前确认的决策

以下是本稿给出的建议默认值，审查时可以逐项修改：

| 决策 | 建议默认值 | 理由 |
|---|---|---|
| v2 与 v1 的关系 | 同仓库、并行包、v1 冻结 | 保留可信实验资产并降低重构风险 |
| 时间模型 | 事件驱动的单调全局逻辑时间，第一版 `E=0` | 防止任意时间跳跃和不一致节点时钟 |
| timeout 动作 | `FireTemporalEvent(TemporalID)`，只选最早 deadline | 时间推进成为受审计子步骤，不由 Agent 给数值 |
| Tick 映射 | 每节点 `PeriodicPulse`，一次 pulse 调一次 Tick | 让 election/heartbeat 由协议计数自然产生 |
| sleep 映射 | `SleepWakeup`，不得跨过更早时间项 | 保留快进能力而不跳过其他事件 |
| enabled 含义 | 机械可执行，不预测协议接受或成功 | 避免 Runtime 硬编码 leader/term/view 语义 |
| entropy | per-node/incarnation/domain 确定性流 + replay tape | 消除跨节点调用顺序耦合并审计随机请求 |
| 随机选择权 | 第一版不是在线 Action，只冻结等价 SUT seed 集 | 控制状态空间并保证方法比较公平 |
| 消息所有权 | yield 时捕获，依赖满足后 release | 同时支持持久化前置条件和长期保留 |
| crash v1 | 稳定边界的 power loss | 语义清楚且可由库/进程 Adapter 实现 |
| 让步机制 | 确定性 barrier，禁止 wall-clock drain | strict replay 的必要条件 |
| Adapter 形态 | 每系统一个薄映射，Runtime 只见统一契约 | 原生 API 不可能完全一致 |
| 新目标边际产物 | Execution Binding + Semantic Mapping | 把控制接入与协议理解分开 |
| Adapter 公共样板 | 具体 Adapter 内部共享 kit，现有契约不变 | 避免每个目标重写可信生命周期 |
| 默认接入等级 | 最小、非侵入式灰盒；官方接口优先 | 纯黑盒不能可靠提供单消息、自然时间和持久化切点 |
| 能力表达 | Surface + Grade + Deterministic Guarantees | 同一 Action 不等于同一证据强度 |
| Control Port | 仅为薄 Adapter 内部可选构件 | 防止形成第二套 Runtime backend 或巨大公共接口 |
| 可选能力 | 统一 Manifest/命令模型，不在 Runtime type assert | 防止实现类型渗入内核 |
| Evidence/PSS | implementation -> fixed Core IR；Family/Extended 可选 | 换协议只增加映射，不重写通用 PSS |
| Agent 权限 | 可提议 hypothesis/episode/搜索工具；具体动作只能引用冻结 ID/当前 enabled ID | 扩大测试主体性，同时限制执行越权和刷分 |
| 通用性门槛 | 非 Raft 目标不修改 Runtime/Action/Core PSS schema | 直接检验协议与实现两类耦合 |

上述初始决策已经用于 M5.1—M5.6d；M5.7 只收紧 Adapter 复用与 PSS 中间表示，不回写历史阶段结论。
