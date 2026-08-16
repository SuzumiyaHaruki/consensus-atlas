# ConsensusAtlas 架构

本文只描述当前活动系统，不记录已经删除的阶段性实现。

## 1. 目标与边界

ConsensusAtlas 的目标是让 Agent 主动理解共识实现、提出风险并构造测试，同时让最终证据来自确定性执行系统。
架构固定三类边界：

1. Agent 决定“值得尝试什么”，但不能创建可执行事实；
2. Target 决定“协议实现如何映射到公共控制面”，但不决定测试结论；
3. Runtime、Replay 和 Oracle 决定“实际发生了什么、结果是否违反已实现性质”。

公共 Core 主要面向 leader-based CFT，但不硬编码 Raft/Paxos 名称。受限 BFT 是后续扩展，不属于当前能力声明。

## 2. 端到端数据流

```text
Agent authoring JSON
  ├─ ProtocolKnowledgePack
  ├─ Target Dossier / source references
  ├─ workload
  └─ Runtime / FaultEnvelope / budgets
                  │
                  ▼
             Target registry
  ├─ Manifest / Qualification
  ├─ composable Action
  ├─ Observation projector
  ├─ Oracle registry
  └─ fidelity boundaries
                  │
                  ▼
               Risk Agent
  portfolio → mechanical assessment → accepted Risk
                  │
                  ▼
             Scenario Agent
  plan → frontier binding → mechanical feedback/repair
                  │
                  ▼
             Control Runtime
  Action → Adapter → Item/Observation → Trace
                  │
                  ▼
      fresh Replay + qualified execution
                  │
       ┌──────────┼──────────┐
       ▼          ▼          ▼
      PSS       Risk       Oracle
       └──────────┼──────────┘
                  ▼
        summary / bundle / journals
```

## 3. 公共控制面

### 3.1 Action

公共 Action 表达调度器能够控制的操作类别：

- 调用和 host effect 完成；
- 已产生消息的投递、丢弃和复制；
- 自然到期 temporal event；
- crash/restart；
- partition/heal。

Action kind 是公共词汇，但可达性是 Target-local 事实。Manifest 的 declared action 只说明 Adapter 声明能力；
`ComposableActions` 进一步说明活动测试路径会把该动作放入 frontier。普通组合测试必须证明每项 composable Action
至少存在一条 offered → selected → executed → replayed 路径。

### 3.2 Item 与消息所有权

目标实现产生消息或 effect，Runtime 为其分配稳定 Item ID 并保存。Agent 只能引用当前 view 中的 ID 或语义 selector；
它不能伪造消息、修改 payload，也不能投递一个从未产生的消息。延迟由“不选择已存消息”自然形成，因此消息可以跨越
多个其他 Action 后再投递或丢弃。

### 3.3 虚拟时间

Runtime 不提供“直接触发某个协议超时”。Adapter 暴露当前自然 temporal item，选择 `fire-temporal-event` 后调用目标
自身 Tick/timeout API。时钟推进和协议状态变化仍由真实实现完成。当前 seed、初始状态和 Action 序列进入 Replay 关系。

### 3.4 生命周期与持久化

Crash 只作用于运行节点，Restart 只作用于已停止节点。具体 durable image、Ready/WAL/effect 顺序由 Target composition
负责；公共 Runtime 不假设所有协议都有相同持久化层。不可表达的窗口通过 fidelity boundary 报告，不伪装成假设失败。

## 4. Target composition

活动 Target 向公共 coordinator 提供一个窄组合对象：

```text
ID
ProtocolKnowledgePack
AgentTargetSurface
ObservationProjector
OracleRegistry
ScenarioInputs(risk, projector)
Execute(risk, projector, scenarioExecution)
```

`ScenarioInputs` 提供 root、RuntimeConfig、FaultEnvelope、Adapter factory、可选 Action preparer 和语义 projector。
`Execute` 把已验证 Scenario 编译为精确 Policy，在相同 Target 上进行 fresh qualified execution。

新增协议时需要实现 Target 的真实差异，但不应修改公共 Core 来枚举协议名。允许 Target 增加：

- namespaced Observation；
- protocol-specific PSS；
- target-local monitor；
- Action preparer；
- fidelity boundary。

这些扩展是“薄但不虚假的适配”，而不是追求所有协议只有一个同构接口。

## 5. Agent 子系统

### 5.1 Risk Agent

Risk Agent 接收协议知识、Property、issue pattern、Target surface、历史 Exploration Memory 和可选源码 catalog。它可以：

- 返回 1–3 个候选的 portfolio；
- 请求 Dossier 已声明的精确源码 reference；
- 根据机械拒绝原因修正或更换候选。

可信代码验证 property reference、机制步骤、predicate、binding、Observation 字段、Action 能力和 fidelity 声明。
通过验证只表示“当前 Target 可以调查”，不表示性质正确或缺陷存在。

### 5.2 Scenario Agent

Scenario Agent 接收当前 frontier、共识语义提示、Risk milestone 进度、上次计划和 `ProgressDelta`，输出完整但有界的
多步计划。selector 会在执行时绑定当前 enabled Action：零匹配、多匹配、陈旧 ID 和越界计划都会形成机械反馈。

一个合法计划执行完并不自动结束调查。只要 Risk 未达到且调用/决策预算仍存在，coordinator 会继续 Agent 循环。

### 5.3 Journal 与恢复

Risk/Scenario 调用各自使用 durable journal。每次请求、响应、usage 和 transport 结果先落盘；恢复时重用已完成调用，
不会因为进程重启再次向 provider 发送相同请求。key 只在真实调用前读取，不进入 artifact。

## 6. Scenario 执行

### 6.1 live branch

`ExecuteBoundedScenarioPlan` 从 root 重建一次 Runtime。战略 step、`after_milestone` 的自然推进和后续 step 都追加到同一
live branch。只有计划形成候选或需要输出可信 Bundle 时才 fresh Replay 完整 Trace。

这避免长度为 N 的计划反复恢复相同 prefix，同时保留最终证据可重放性。

### 6.2 终止状态

- `client-terminal`：workload 已返回，仅结束当前自然推进；
- `risk-reached`：目标 witness 达成；
- `quiescent`：没有可自动推进 Action；
- `budget`：决策、模型调用或 token 预算到达；
- `stopped`：计划被机械拒绝或执行失败。

不存在按计划步数改变终止含义的 compatibility checkpoint。

### 6.3 terminal outcome

成功 Action 才进入 Trace。timeout、worker exit 等失败尝试另存 terminal outcome，绑定成功前缀、enabled 集、尝试
Action 和稳定 failure class。它们可以作为执行诊断，但不能自动成为共识 finding。

## 7. Observation、PSS 与 Risk

公共 Observation 有通用 kind；Target 还可声明 namespaced kind 和类型字段。Core 负责 declaration、校验、记录、匹配
与 Replay，不解释协议含义。

PSS 同时报告：

- protocol state：协议进展抽象；
- control state：Runtime/消息/节点控制状态；
- joint state：两者组合；
- samples：实际投影次数。

它们用于比较探索广度，没有固定完备分母，也不能单独判断测试正确性。

RiskWitness 是有序 Observation predicate 的可达性证据。`ProgressDelta` 报告新 milestone、首个缺失 milestone、最近
Action、transition novelty 和重复模式深度，供下一轮 Agent 使用。

## 8. Oracle registry

每个活动 Target 使用一个 registry 同时派生：

- Agent 可见的 property → monitor capability；
- 实际执行的 monitor 列表。

证据 assessment 不只检查声明存在，还要求对应 monitor ID 出现在 `Oracle.Checked`。通用 Agreement/Trace Integrity
和 target-local monitor 分开实现、统一输出。Agent assertion 只能作为候选，不能进入 verdict。

## 9. 资格与 fidelity

Qualification 验证 Adapter/Runtime 的机械能力。Target surface 进一步区分：

- declared action；
- composable action；
- observation capability；
- executable Oracle；
- fidelity boundary。

状态至少区分 `missing-action`、`missing-observation`、`missing-oracle`、`target-fidelity-gap`、
`fidelity-unassessed`、`hypothesis-not-reached`、`search-budget-exhausted` 和 execution/planning failure。

`fidelity-unassessed` 是提示，不是隐式 gate；只有候选明确依赖 Target 已声明为不可表达的边界时，才形成 capability gap。

## 10. 输出与评测

Agentic Episode 保存紧凑 `summary.json` 和完整 `bundle.json`。summary 可以从 bundle、journal 和 Target recovery binding
重新派生；终态恢复不访问 SUT、provider 或 key。

`cmd/defect-eval` 支持保存 Bundle/MethodSpec 的旧评测，也能通过 `-agentic-inputs` 直接消费
当前 Agentic Episode 目录。私有 contract 提供 pair、SUT 身份、预算和 monitor composition；
summary 只提供方法状态/成本，独立 evaluator 从 Bundle 重算 verdict。旧 A8 paired launcher/session 不参与此路径。

评价面保持分离：

1. 发现结果：独立 Oracle finding、可复现 root cause；
2. 探索结果：PSS、Risk、Action/Observation novelty；
3. 成本：model calls/tokens、Runtime decisions、primary/replay work；
4. 能力边界：capability/fidelity/execution outcome。

## 11. 代码保留规则

保留能直接支撑活动流程或可信边界的实现；删除只有历史入口和历史 artifact 格式使用的 wrapper。当前不再保留
A2 Explorer、A8 session、通用 Campaign store 或 stateless Campaign。

不为了行数合并 Runtime、Replay、Oracle、Target-specific Observation/monitor，也不新增无具体失败场景支撑的 hash、
冻结 contract、baseline 或 gate。普通开发优先跑受影响包；完整阶段再跑全量测试、vet 和必要的 race。
