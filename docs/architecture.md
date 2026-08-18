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

Scenario 计划在执行前只检查可机械证明的能力矛盾：非 composable Action，或 Target 可选富声明明确排除的
消息/HostEffect 取值。缺口作为 `missing-action`/`missing-control-capability` 返回 Agent 修订且不消耗 Runtime
decision；当前 frontier 的精确 ActionID 也要反查 kind。没有富声明只表示未评估，不能据此拒绝计划。
若调查最终停在该缺口，Episode 与后续 Agent Memory 保留机械原因，不把它覆盖成预算耗尽或协议 verdict。
后续 Risk Agent 同时获得该缺口的结构化 step reference 与请求摘要，用于切换到当前 Target 可执行的机制；这些字段
来自可信预检，不包含 Oracle finding，也不会创建新的 enabled Action。
能力反馈暴露是方法配置而非 Agent 自报字段。活动 CLI 可选择只提供机械 reason codes，或额外提供结构化
`code/reference/summary`；实际 Memory 投影与同一个 typed `AgenticMethodSpec` 字段机械绑定。这样可在不改变
Runtime、Target 或 Oracle 的情况下做配对消融，同时拒绝“合同声明结构化反馈、实际却使用旧视图”的方法漂移。
公开校准可额外提供一个 typed capability probe。它只是普通 ScenarioPlan，由真实 TargetSurface 运行同一
`ScenarioCapabilityGaps` 预检；计划不会进入 Runtime。只有预检确实拒绝后，系统才生成零 work 的 calibration
Memory，并从 MethodSpec 中的同一 probe 重新核对实际 Memory。该入口用于保证消融变量被激活，不属于自然调查、
协议执行或 finding 证据。

### 3.2 Item 与消息所有权

目标实现产生消息或 effect，Runtime 为其分配稳定 Item ID 并保存。Agent 只能引用当前 view 中的 ID 或语义 selector；
它不能伪造消息、修改 payload，也不能投递一个从未产生的消息。延迟由“不选择已存消息”自然形成，因此消息可以跨越
多个其他 Action 后再投递或丢弃。

Manifest 可以选择进一步声明消息 type hint 与 metadata key。它们是 Target-local 字符串，不进入公共协议枚举；
一旦声明，Runtime 会拒绝超出范围的 emission。Agent frontier 只展示已产生消息的实际提示、metadata 和依赖，
因此这些字段可以用于收窄当前 Action，但不能伪造消息或修改 payload。

### 3.3 虚拟时间

Runtime 不提供“直接触发某个协议超时”。Adapter 暴露当前自然 temporal item，选择 `fire-temporal-event` 后调用目标
自身 Tick/timeout API。时钟推进和协议状态变化仍由真实实现完成。当前 seed、初始状态和 Action 序列进入 Replay 关系。

### 3.4 生命周期与持久化

Crash 只作用于运行节点，Restart 只作用于已停止节点。具体 durable image、Ready/WAL/effect 顺序由 Target composition
负责；公共 Runtime 不假设所有协议都有相同持久化层。不可表达的窗口通过 fidelity boundary 报告，不伪装成假设失败。

HostEffect 的富能力描述同样是可选的 Target-local 声明：kind、phase、durability、允许完成结果和允许失败结果。
Runtime 校验实际 effect 不越过该上界；frontier 把当前 effect 的 phase、durability 和具体 Action outcome 暴露给
Scenario Agent。公共层不解释 `persist`、`sync`、`snapshot` 等名字的协议含义。

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
- 在总片段、单次行数和模型调用预算内继续同一 reference 的非重叠行窗口，或转向另一个声明 reference；
- 根据机械拒绝原因修正或更换候选。

可信代码验证 property reference、机制步骤、predicate、binding、Observation 字段、Action 能力和 fidelity 声明。
binding domain 由 Observation capability 机械派生并与字段一起暴露；同一 `bind_as` 不能跨节点实例、
节点 ID 或不相关的 Target-local 域复用。
源码读取同样由可信代码校验 catalog、只读 mount、路径、文本类型、行窗口和重复范围；本地路径及未声明文件
不会暴露给 Agent。通过验证只表示“当前 Target 可以调查”，不表示性质正确或缺陷存在。

### 5.2 Scenario Agent

Scenario Agent 接收当前 frontier、共识语义提示、Risk milestone 进度、上次 proposal、紧凑 branch 反馈和
`ProgressDelta`，输出完整但有界的 Investigation Proposal。其嵌套 `ScenarioPlan` 的 selector 会在执行时绑定当前
enabled Action：零匹配、多匹配、陈旧 ID 和越界计划都会形成机械反馈。
selector 的 `selector_trace` 还会记录每个字段过滤后的候选数，供下一次 `revise` 定位冲突或补足歧义字段。
`actor_role`、`message_class`、`epoch_relation`、`operation_state` 也可作为通用收窄字段。
它们不存入 Action，而是在每次 live frontier 刷新后由 Target projector 重新产生，并通过 Action ID/digest
与候选绑定；Core 只理解这四个闭集词汇，不理解 Raft term、Paxos ballot 或具体消息名称。

一个合法计划执行完并不自动结束调查。只要 Risk 未达到且调用/决策预算仍存在，coordinator 会继续 Agent 循环。
收到可信 `ProgressDelta` 后，Agent 还可返回无计划的 `abandon`，结束当前低收益假设并把机械停止原因交给
下一 Episode。该意图不执行 Runtime Action，也没有 verdict 权限；首次调用不会提供它。

`ProgressDelta` 进一步区分 selector 已执行但没有新增 milestone、重复调度模式、客户端已返回和自然闭包静止；
它同时报告 Action kind 计数、Timer callback 与真实逻辑时钟推进、fault allowance/usage/remaining，以及当前仍
可用的非闭包干预 Action kind。等待 `after_milestone` 时，客户端返回和无自然 Action 分别使用
`milestone-wait-client-terminal` 与 `milestone-wait-quiescent`，不再压成笼统的 `milestone-unreachable`。
这些都是机械搜索反馈，不是可达性证明或协议 verdict。

### 5.3 Journal 与恢复

Risk/Scenario 调用各自使用 durable journal。每次请求、响应、usage 和 transport 结果先落盘；恢复时重用已完成调用，
不会因为进程重启再次向 provider 发送相同请求。key 只在真实调用前读取，不进入 artifact。

## 6. Scenario 执行

### 6.1 live branch

`ExecuteBoundedScenarioPlan` 从 root 重建一次 Runtime。战略 step、`after_milestone` 的自然推进和后续 step 都追加到同一
live branch。计划后的周期性自然推进也使用这一个 Runtime。片段完成后只 fresh Replay 一次完整 Trace；Replay 成功后
保留该验证 Runtime 投影出的 frontier/snapshot 作为下一轮 Agent 的可信输入，不再重建同一前缀。

这避免长度为 N 的计划反复恢复相同 prefix，同时保留最终证据可重放性。

Coordinator 每轮以 `ceil(remainingDecisions/remainingCalls)` 重新计算自然推进反馈粒度，并同时考虑本轮计划步数
和剩余全局预算。早期片段因 client terminal、quiescence 或计划停止而少用的 Action 会重新分配给后续调用。
`decision_allowance` 约束“计划 + 本轮自然推进”，`remaining_decisions` 表示 episode 尚可使用的成功 Action。
这两个值属于资源边界，不授权 Agent 创建 Action 或改变终止/verdict 语义。

真实 Target 的长轨迹回归从同一 Adapter 的确定性初态开始，以 exact policy 将最终 Scenario Trace 再执行为
qualified Bundle。测试同时要求 target-local epoch/ballot Observation、protocol/control/joint PSS、stable Replay
和完整 Oracle registry。没有 workload 的校准 Risk 保持 `not-reached`；它验证执行容量，不伪装为缺陷发现。

`branch` 保存可信 Trace 检查点和路径结果；`control`、`ablate` 从参考 branch 的根检查点重建 Runtime，因而比较
共享相同前置状态。三种实验 intent 只创建候选，不隐式覆盖最终证据。`continue + from_branch_id`
选择并继续路径；`select + from_branch_id` 只选择已有路径，其 proposal 不带计划、不消耗 Runtime decision。
存在分支时，最后 Scenario 调用会收窄为 select-only view；即使 decision 已为零也能执行该调用，
但 provider call 和 token 仍按普通模型工作计费。
调用耗尽但尚未选择时仍返回 `final-selection-required`，不任取最后执行的实验路径。分支探索全部计入
同一 decision budget，结果分别记录总 Action、最终选中路径 Action 和实验分支 Action。

选择不是 Oracle admission。Coordinator 保留每个 fresh-Replay 成功的候选，按 Trace digest 去重后逐个运行
qualified execution 和 Target Oracle。主路径写入 `bundle.json`，其他候选及独立 work 写入
`branch-evidence.json`。在线 Agent 不获取私有 Oracle 反馈；终态恢复和 holdout evaluator 均重新验证全部
候选，因此“实际执行到问题但 Agent 未选该分支”不会变成漏报。

跨 Episode Memory 不属于 Oracle 边界。它只暴露枚举的机械 outcome、Risk milestone、PSS 和成本；
Oracle violation 数量、Oracle 派生 assessment 以及基于 Oracle 选出的代表分支都禁止进入 Risk Agent prompt。

每条 branch 公开可信执行得到的 `applied_interventions`。ablate 只能引用完整成功的干预计划，被删除 ID 必须属于
实际执行成功的战略步骤；control/ablate 在可信解析层禁止 checkpoint-local `action_id`，必须在共享根重新用语义
selector 绑定。Agent 只看到根/终点 decision、最终可用 Action、计划、实际干预和 `ProgressDelta`；完整分支 Trace
不重复进入模型输入。

### 6.2 终止状态

- `client-terminal`：workload 已返回，仅结束当前自然推进；
- `risk-reached`：目标 witness 达成；
- `quiescent`：没有可自动推进 Action；
- `budget`：决策、模型调用或 token 预算到达；
- `hypothesis-abandoned`：Agent 在机械进展反馈后主动把剩余调查预算交还给下一 Episode；
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
和 target-local monitor 分开实现、统一输出。活动 composition 位于 `targetoracles`：在线 Target 与正式 evaluator
共享同一份 executable registry，formal contract 只按既有 `MonitorIDs` 选择子集。target/projector 不匹配或
未注册 monitor 会被明确拒绝。Agent assertion 只能作为候选，不能进入 verdict。

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

Agentic Episode 保存紧凑 `summary.json`、可选主路径 `bundle.json` 和可选
`branch-evidence.json`。summary 可以从 bundle、branch evidence、journal 和 Target recovery binding
重新派生；终态恢复不访问 SUT、provider 或 key。

`cmd/defect-eval` 支持保存 Bundle/MethodSpec 的旧评测，也能通过 `-agentic-inputs` 直接消费
单 Episode 或完整 Agentic Investigation 目录。私有 contract 提供 pair、SUT 身份、预算和 monitor composition；
summary 只提供方法状态/成本，独立 evaluator 从主路径及所有分支 Bundle 重算 verdict。
对一个 trial，任一合法候选的独立 monitor finding 都会进入可信结果；方法自报的 Risk/Oracle 字段不参与判定。
在检查 finding 前，evaluator 先汇总 Scenario frontier/search work、所有候选的 Trace decisions、
qualified primary work 和 replay work；decisions/primary work 任一超过 formal trial 的现有预算，
或模型 calls/tokens 超过同一 `AgenticLogicalBudget`，就将该 trial 标记为 invalid。
正式 Agentic 路径只接受 V3 Bundle；summary 必须声明主路径和每个 branch evidence，而且
Plan/Risk ID、Trace digest、work 与 `MethodSpecDigest` 都要与文件和 formal contract 交叉一致。
因此未声明分支和其他方法生成的 Bundle 不能借 Agentic Episode 目录获得 finding credit。
活动 CLI 根据真实 transport/model、prompt 版本、semantic input、源码暴露、Episode 数和预算派生
typed `AgenticMethodSpec`；调用者提供的 digest 只能作为预期值。多 Episode formal trial 必须包含连续
`episode-0001..N` 并聚合每轮成本；单 Episode 入口只兼容明确声明一轮的方法。搜索中的
child verification 是 fresh replay，和最终 Bundle replay 一起受 Replay 预算约束。
旧 A8 paired launcher/session 不参与此路径。

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
