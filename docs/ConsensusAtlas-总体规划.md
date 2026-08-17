# ConsensusAtlas 总体规划

## 0. 一句话目标

> 构建一个利用 Agent 主动理解并测试分布式共识实现的系统：Agent 负责提出风险和组织调查，可信控制层负责确定性
> 执行与复现，独立 Oracle 负责确认问题，覆盖与成本指标负责直观展示测试成果。

系统必须同时突出三个关键词：

- **Agent**：不是 Random/DFS 的重排器，而是协议理解、风险提出、计划修正和调查管理的主体；
- **共识**：输入、Observation、PSS 和 Oracle 能表达 epoch、coordinator、quorum、log/decision、恢复等协议语义；
- **测试**：候选必须落到真实实现的 Action、Trace、Replay 和可独立检查的结果。

## 1. 最终用户流程

### 1.1 输入什么

接入一个新共识实现时，用户提供：

1. 协议材料
   - 协议家族和实现范围；
   - 核心性质；
   - 历史问题模式和适用边界；
   - Target Dossier：组件、host contract、控制语义、盲区和源码 reference。
2. Target composition
   - Adapter factory；
   - Qualification；
   - workload 编码和路由；
   - target-local Observation/PSS/Oracle；
   - 必要的 Action preparer 和 fidelity boundary。
3. 实验配置
   - 节点、Runtime seed、自然时间和 fault allowance；
   - 模型、调用/token/决策/时间预算；
   - 可选只读源码 mount。

理想接入不是“零适配”，而是把不可避免的协议差异限制在 Target Pack。公共 Core 不应因新增协议的 Observation 或
Oracle 而反复增加枚举和分支。

### 1.2 如何处理

```text
材料加载与机械资格
        ↓
Risk Agent：提出候选 portfolio，必要时查询源码并自我修正
        ↓
可信代码：验证 property、predicate、Observation、Action 和 fidelity
        ↓
Scenario Agent：生成完整多步计划，根据真实反馈 continue/revise
        ↓
Control Runtime：执行当前 enabled Action
        ↓
fresh Replay：重放完整候选
        ↓
Observation → PSS/Risk → Oracle
        ↓
跨 episode Memory、统计和最终报告
```

### 1.3 得到什么

用户应能直观看到：

- 找到了哪些独立 Oracle finding，能否稳定重放；
- 调查了哪些 Risk，停在什么 milestone；
- protocol/control/joint PSS 和 transition novelty；
- Action/Observation/Oracle/fidelity 能力缺口；
- 模型调用与 token、Runtime decisions、primary/replay work；
- 每个 episode 的停止原因和可复现工件。

不输出一个虚假的“测试完整度百分比”。可以按发现、探索、成本和能力四个面分别评价，并在实验中比较方法曲线。

## 2. 控制层

### 2.1 统一什么

公共层统一：

- Action 生命周期和 enabled frontier；
- produced Item 的身份与存储；
- 虚拟时间/自然 temporal event；
- Trace、Replay、预算和 terminal outcome；
- Observation declaration/matching 接口；
- PSS、Risk 和 Oracle 的输出接口。

公共层不统一：

- 所有协议的内部状态；
- 每种消息的完整语义；
- 所有持久化实现；
- 所有性质的单一 Oracle；
- 一个号称适用于所有协议的巨大 DSL。

### 2.2 Action 语义

消息由 SUT 产生、Runtime 存储，Agent/策略只决定何时 deliver/drop/duplicate。这样既能长时间延迟消息，又不会制造
实现未产生的 payload。Crash/Restart、Partition/Heal 和 effect completion 也只能作用于当前可达对象。

时间不是任意修改协议内部状态。Runtime 选择当前自然 temporal item，Adapter 调用目标自身 Tick/timeout API；重复
选择可构造自然超时。seed 和 Action 序列保证执行可复现。

### 2.3 declared 与 composable

Manifest 声明 Target 理论能力；活动 Target surface 只公开当前 composition 实际能够构造的 `ComposableActions`。
每项 composable Action 都必须有普通端到端可达性测试。不能让 Agent 看到声明支持但永远不会进入 frontier 的动作。

### 2.4 Target fidelity

RawNode、MemoryStorage 或单进程 worker 可能抽象掉生产窗口。系统必须报告：

- `target-fidelity-gap`：候选明确依赖一个已知不可表达边界；
- `fidelity-unassessed`：相关性质存在边界，但候选未声明是否依赖；
- `missing-action/observation/oracle`：缺少具体测试能力。

这些状态把 Agent 失败与执行底座不足分开。它们不能统一伪装成 `Risk not reached`。

## 3. Agent 设计

### 3.1 Risk Agent

Risk Agent 是协议理解入口。它读取 Property、issue pattern、Target Dossier、历史 Memory 和可选源码片段，提出：

- property reference；
- suspected mechanism；
- 有序机制步骤；
- Observation predicates/bindings；
- 所需 fidelity；
- supporting references。

允许 1–3 个候选 portfolio，可信代码逐项审查并选择首个机械合格候选。Agent 不提供最终分数或 verdict。

源码能力应逐步接近 Agora 的调查能力，但保留只读、显式 mount、声明 reference、行数/字节和调用预算。后续可增加
更广搜索，但读取结果仍只是规划材料。

### 3.2 Scenario Agent

Scenario Agent 看到可信 frontier 和语义反馈，输出完整但有界的测试意图。系统应支持：

- `continue`：沿当前 branch 深入；
- `revise`：修正 selector 或时序；
- `branch/control`：从同一 checkpoint 做对照；
- `ablate`：移除一个干预检查因果必要性；
- `select`：不执行新 Action，选择一个已存在且已重放的 branch；
- `minimize`：对已确认 finding 缩短轨迹。

当前已实现 continue/revise，以及基于确定性 Trace 检查点的 branch/control/ablate。control 与 ablate
从参考 branch 的同一根执行，但三者只保存候选路径。`continue + from_branch_id` 可选择并继续，
`select + from_branch_id` 可以零 Runtime Action 成本直接选择已有路径。存在分支时，最后一次
Scenario 调用保留为 select-only；它仍正常计入模型调用和 token 成本。选择只决定 Agent 的最终解释路径；
每个唯一、fresh-Replay 稳定的候选都会离线运行 Target Oracle，不依赖 Agent 选择正确。
消融计划必须删除 reference branch 中实际执行成功的 `applied_interventions`，不能删除仅出现在 proposal 文本中
但从未执行的步骤。control/ablate 只能用语义 selector 在共享根重新绑定。minimize 必须等待可信 Oracle finding，
不能在 Scenario 阶段仅凭 Risk reached 提前宣称完成。

### 3.3 Analysis Agent

多 Agent 不以数量为目标。只有当职责和信息边界清楚时再增加 Analysis Agent，用于：

- 聚类相似 finding；
- 比较 control/ablation；
- 提议最小化和变体；
- 总结未覆盖能力。

Analysis Agent 不能覆盖 Oracle verdict。

### 3.4 自我修复

每次机械拒绝都应返回具体原因和最近证据，例如：

- 首个缺失 milestone；
- 最近 Action/Observation slice；
- transition novelty；
- 重复模式深度；
- no-match/ambiguous/stale selector；
- capability/fidelity gap；
- 剩余预算。

增加调用次数只有在下一轮能看到真实反馈时才有意义。

## 4. 协议语义与扩展缝

### 4.1 Observation

通用 Observation 表达 workload、coordinator change、decision/apply、node lifecycle 等跨协议概念。Target-local
Observation 使用 namespace，例如：

```text
raft/hard-state-persisted
raft/read-state-returned
omnipaxos/promise-raised
hotstuff/qc-formed
```

Target 声明字段类型，Core 只负责验证、保存、匹配和 Replay。新增 kind 不要求修改公共枚举。

### 4.2 PSS

PSS 是状态发现指标，不是正确性判定。应同时报告：

- protocol PSS：忽略纯调度噪声的协议进展；
- control PSS：节点、消息、partition、effect 等控制状态；
- joint PSS：协议与控制的组合；
- transition/sequence novelty：弥补单状态集合不表达先后关系的问题。

不同方法在相同预算下比较状态/转移发现曲线，借鉴 MODIST 的无固定分母评价。最终结果不把 PSS 单独换算为完成百分比。

### 4.3 测试义务

义务用于表达需要出现的事件、关系或性质检查，但不能让 Agent 通过分别构造互不相关的简单测试刷分。后续组合形式优先：

- 有序 milestone pattern；
- 参数绑定后的关系义务；
- 前置/后置状态约束；
- 因果对照和 ablation；
- 由真实可达轨迹实例化的 obligation graph。

不做静态笛卡尔积，不预设所有组合都可达。

### 4.4 Oracle

Oracle 分三层：

1. 通用 Trace/Agreement/operation-history；
2. 协议族性质；
3. Target-local host/persistence/implementation monitor。

Target 使用一个 registry 同时生成 Agent capability 和实际 monitor 列表。最终 evidence assessment 必须确认 property
对应 monitor 真正在 `Oracle.Checked` 中执行。Agent assertion 只能作为待实现 Oracle 的候选。

## 5. 确定性与成本

### 5.1 执行确定性

确定性要求的是：相同 Target identity、初态、seed 和 Action 序列得到相同 Trace/Replay。它不要求 LLM 两次输出相同。
模型的非确定性通过保存 response、编译为确定性计划并重放实际执行来隔离。

### 5.2 长轨迹

计划内使用 live Runtime branch，Action 逐步追加；到 promotion/checkpoint 才 fresh Replay。不能让 N 步调查变成重复恢复
前缀的近二次成本。数百 Action 场景必须报告：

- branch 执行 work；
- promotion Replay work；
- Agent 调用/token；
- Runtime decision allowance。

当前 Coordinator 将 episode decision budget 按剩余调用轮次切成周期反馈片段，并同时公开本轮
`decision_allowance` 和全局 `remaining_decisions`。战略计划、`after_milestone` 与片段内自然推进共享
一个 live Runtime；每轮按 `ceil(remainingDecisions/remainingCalls)` 重算反馈粒度，片段末尾的一次 fresh Replay
同时负责候选验证和下一轮 frontier/snapshot 生成。结果直接保存 `DecisionsUsed`、`StopReason`、
`SelectedPathDecisions` 和 `BranchExplorationDecisions`，不从最终 Trace 反推全局探索成本。
协议无关 256 Action 校准以 4 次 Agent 调用完成，计得 4 次重建、4 次验证、645 个 Replay decision 和
1298 work unit。etcd/raft 与 OmniPaxos 的真实 Adapter 也分别完成 128 Action 校准：两者 Scenario work
均为 658，qualified primary/replay 均为 129/129，并产生非空的 target-local Observation、protocol/control/joint
PSS 和完整 Oracle 检查。该结果只证明执行与证据容量；真实 LLM 的长时自适应效果仍需后续实验验证。

### 5.3 恢复

provider journal 先落盘请求/响应和 usage。Episode 恢复重用已完成调用；终态从 bundle 重新派生 Risk/PSS/Oracle，不访问
provider、key 或 SUT。失败 Action 的 terminal outcome 与成功 Trace 分开保存。主路径保存为
`bundle.json`，未选择但已验证的候选保存为 `branch-evidence.json`；恢复时两者都重新投影
Risk/PSS/Oracle，并核对分支执行成本。

跨 Episode Exploration Memory 是 Agent-facing 材料，因此只允许白名单中的机械 outcome：
`execution-completed`、`budget-exhausted`、`risk-near-miss`、`planning-stopped`、`execution-failed`。
Oracle finding 数量和 Oracle 派生的 assessment 不得进入 Memory，也不得用于选择向 Agent 展示的代表分支。

## 6. 评价设计

### 6.1 发现效果

最强证据是发现并复现此前未知的问题，但系统设计不能预设一定成功。实验至少报告：

- 已知/public candidate 的 kill rate；
- private holdout 的 finding 数；
- false positive/invalid；
- root-cause 去重；
- replay/minimization 成功率。

### 6.2 探索效果

在相同预算下比较：

- unique protocol PSS 与 transition；
- Risk/obligation 达成；
- Action/Observation 类型和绑定多样性；
- 长时曲线与边际收益。

只有“探索广度较高 + 独立 Oracle 结果可信”共同成立，才能说明测试有效。

### 6.3 对照与消融

最终至少比较：

- Random；
- deterministic DFS/系统化搜索；
- 专家计划；
- 单 Agent；
- Risk + Scenario 多 Agent；
- 无源码、无反馈、无 Memory 等消融。

所有方法使用相同 Target、初始材料、预算核算和 Oracle。当前旧 A8 paired evaluator 已删除；新的 evaluator 必须直接
消费活动 Agentic artifact 中的主 Bundle 和所有分支 Bundle，不能维护第二套执行契约，也不能只因
Agent 未选择某个分支就忽略其独立 Oracle 结果。
所有候选的 decisions 和 qualified primary work 必须在评价 finding 前汇总到同一 trial 预算；
不能让每个分支各自重用一遍完整预算。
正式 Agentic trial 的 primary work 不仅是最终 Bundle，而是
`Scenario frontier + search reconstruction/materialization + 主路径/所有候选 qualified primary`；
搜索阶段的 fresh child verification 与最终 Bundle replay 一样计入 Replay，不得伪装成 primary。
模型 calls/tokens 和上述执行工作都必须对照预声明的 `AgenticLogicalBudget`，不能只作解释字段。
正式 Agentic Bundle 必须使用 V3 evidence，且 Episode summary、分支声明、Trace/work 与
`MethodSpecDigest` 必须与实际 Bundle 和 formal contract 交叉一致，防止其他方法的短 Bundle
被归到 Agent 方法名下。`MethodSpecDigest` 必须从真实 transport/model、prompt 版本、semantic input、
源码暴露模式、Episode 数和预算派生，不能由调用者自报。正式多 Episode trial 必须摄取完整、连续的
Investigation 并汇总全部 Episode；禁止只提交最后一个成功 Episode。

## 7. 当前实现状态

已完成：

- etcd/raft 与 OmniPaxos 两个真实 Target；
- produced-message 控制、自然时间、etcd/raft 生命周期和 partition；
- target-local Observation/Oracle registry；
- Risk portfolio、源码 reference 查询、机械资格和 Exploration Memory；
- 多步 Scenario、live branch、fresh Replay、ProgressDelta；
- protocol/control/joint PSS；
- durable provider journal、episode/investigation 恢复；
- capability/fidelity/execution outcome 分类；
- Agentic artifact → private holdout evaluator；
- branch/control/ablate 调查编排；
- 零 Action 分支选择、全候选 Oracle 执行及 `branch-evidence.json` 恢复；
- 完整 Agentic 搜索/模型/候选执行成本核算与 V3 方法归属；
- typed Agentic MethodSpec、完整 Investigation 摄取及搜索 replay 成本分类；
- 256 Action 周期反馈、live branch 与单次 promotion Replay 校准；
- etcd/raft 与 OmniPaxos 的 128 Action 真实 Target 证据校准；
- A2/A8/旧 Campaign 路径清理。

尚未完成：

- Oracle finding 后的 trace minimize；
- 更广但受控的源码导航；
- 第三个非 Raft/Paxos 形态 Target；
- Agent 相对 baseline 的长时效果证据；
- 新问题发现与根因最小化。

## 8. 路线阶段

### M1：最小底座闭合与瘦身（已完成）

- 统一 client terminal/Risk/budget 终止语义；
- 删除固定 24-action compatibility checkpoint；
- 解耦 active root 与旧 A2/stateless Campaign；
- 删除多代入口和重复契约；
- 保持两个 Target、Replay、Oracle 全量测试绿色。

完成标准：活动 CLI、两个 Target、终态恢复和 Bundle evaluator 均可编译测试；文档只描述当前路径。

### M2：Agentic holdout evaluator（已完成）

- 定义 evaluator 输入为当前 episode summary/bundle/journal audit；
- 对 control/candidate SUT 使用同一 Agentic 方法和预算；
- evaluator 从保存证据重算 Oracle，不启动第二套 A8 session；
- 输出 killed/survived/false-positive/invalid 和成本。

当前已完成 summary/bundle 到 private pair/exposure/Agreement 评测的端到端桥接，并分别用
etcd/raft 和 OmniPaxos projector 验证。后续可按需注册 target-local monitor，但不在评估器中
复制 Target 执行逻辑。

不增加无具体失败场景的 frozen contract 或 gate；优先复用现有 Bundle/MethodSpec/defectbench 类型。

### M3：长轨迹执行与反馈底座（已完成）

- 协议无关 256 Action 成本/反馈校准已完成；
- etcd/raft 与 OmniPaxos 的 128 Action 真实 Target 校准已完成；
- branch/control/ablate 已实现；分支共享检查点语义，探索成本统一计入 decision budget；
- replay/setup 数随 Agent 反馈片段增长，而不随每个 Action 增长。

### M3.1：调查语义闭环（已完成）

- 全局探索预算和停止原因进入 `ScenarioAgentResult`，Episode 不再用最终路径长度猜测总成本；
- branch/control/ablate 不再隐式成为最终证据，分支晋升必须显式选择；
- ablate 基于实际执行成功的干预，control/ablate 的 exact ActionID 在可信层拒绝；
- 反馈片段按剩余预算和剩余调用动态重算。

### M3.2：分支证据与 Oracle 闭环（已完成）

- 实验分支在最后一个 decision 到达 Risk 时仍保留可重放候选；
- `select` 只引用已有 branch，不包含计划也不执行 Runtime Action；
- 所有唯一候选独立运行 Target Oracle，成本与主路径分开记账；
- Episode 可以只有分支证据而没有任意选取的主 Bundle，持久化、恢复和 holdout evaluator 均支持此语义。

### M3.3：正式实验边界（已完成）

- Agent-facing Memory 移除 Oracle 数量和 Oracle 派生 outcome，只保留机械状态、Risk/PSS 进展和成本；
- holdout 在判定 finding 前汇总主路径和所有分支的 decisions/primary work；
- 分支数受 Scenario 调用上限约束，超额 CLI 证据直接拒绝；
- 最后 Scenario 调用在存在分支时只允许 select，不消耗 Runtime decision，但计入模型成本。

### M3.4：完整成本与方法归属（已完成）

- formal contract 直接复用 `AgenticLogicalBudget`，evaluator 同时核对搜索 decisions/work、
  全部 qualified primary/replay work 和模型 calls/tokens；
- 搜索成本超限时，即使最终 Bundle 很短也必须将 trial 记为 `invalid`；
- Agentic Episode 在正式模式下生成 V3 Bundle，summary、branch evidence、Trace/work 和
  contract 的 `MethodSpecDigest` 必须一致；
- summary 未声明的分支文件、跨方法 Bundle 替换和超预算模型工作均由普通回归测试拒绝。

这里没有新增预算 DSL、hash 或并行评测路径；只复用现有 logical budget、V3 method digest
和 Episode 已记录的 work 字段。历史 OpenRouter r5 等 V2 公开校准工件因缺少这些正式绑定，
仅保留为流程校准，不能直接被纳入 private holdout。

### M3.5abc：Investigation 级正式归属（已完成，待版本化收口）

- M3.5a：将 DFS `ChildVerification` 明确计入 search replay；frontier reconstruction 和 child
  materialization 继续计入 search primary，二者分别对照既有预算；
- M3.5b：新增 typed `AgenticMethodSpec`，由活动 CLI 根据实际模型 transport、prompt 版本、
  semantic input digest、源码暴露、Episode 数和逻辑预算派生唯一 digest；调用者若提供预期 digest，
  只能用于一致性校验；
- M3.5c：formal trial 可指向完整 Investigation，要求 `episode-0001..N` 连续、无额外条目且每轮
  MethodSpec 一致；evaluator 汇总所有 Episode 的模型、搜索、主路径和分支证据；
- 单 Episode 输入只在 MethodSpec 明确声明 `InvestigationEpisodes=1` 时接受，避免成功 Episode
  cherry-pick 和前序 Memory/成本漏算。

这里复用现有 V3 Bundle digest、Episode 工件和 `AgenticLogicalBudget`，没有增加 session ledger、
第二套 hash、预算 DSL 或新的实验 gate。

### M4：Agent 能力释放（进行中）

- 扩大只读源码搜索范围；
- 让 Agent 根据真实 ProgressDelta 选择 continue/revise/branch 或放弃假设；
- 增加 investigation 调用/时间预算；
- 在隔离工作区允许 native-test candidate；
- 从已确认根因生成变体并自动最小化。

权限扩大必须保持执行事实、Oracle verdict 和正式发布边界不变。

### M4a：反馈驱动的假设交接（已完成，待版本化收口）

- Scenario Agent 只有在收到可信 `ProgressDelta` 后才获得 `abandon`；首次调用不能无依据放弃；
- `abandon` 不含 ScenarioPlan、不执行 Runtime Action，也不表达安全性、活性或缺陷 verdict；
- coordinator 以稳定 `hypothesis-abandoned` 停止原因结束当前调查，并在下一 Episode 的
  `ExplorationMemory` 中暴露同名机械 outcome/reason，使 Risk Agent 可以换一个候选；
- 已执行并 Replay 稳定的前缀或分支仍保留为 evidence；放弃不能删除已发生的成本或 Oracle 候选；
- 完整 formal Investigation 可以包含前序无 Bundle 的停止 Episode 和后续成功 Episode，评估器仍汇总
  每一轮模型/搜索成本，并只在整个 Investigation 没有候选 evidence 时按缺失证据处理。

### M4b：声明式源码迭代导航（已完成，待版本化收口）

- Risk Agent 一次调查最多读取 4 个片段、每次模型调用最多 2 个、每个最多 80 行；
- 第一个成功片段不再关闭源码入口，可用前一结果的 `end_line + 1` 继续同一声明文件的非重叠窗口，
  也可转向另一个 Dossier 声明 reference；
- 可信代码拒绝重复/重叠窗口、未声明 reference、越界路径、symlink escape、超大或非文本源码；
- 每次查询和结果继续进入 durable provider journal；本地 mount 路径和源码内容不进入 verdict；
- 源码暴露模式、Risk prompt 与实现版本进入现有 typed `AgenticMethodSpec`，不新增第二套 hash 或 gate。

下一步不再横向扩大文件系统抽象，而是在共享 provider 边界完成后，以 DeepSeek 官方为主校准通道，观察真实
Agent 能否利用多轮源码导航形成更好的 portfolio，并根据 Scenario `ProgressDelta` 继续、修订或放弃假设。

### M4c：真实模型长调查校准（进行中）

- 真实调用必须使用用户明确授权的源码文件、片段数量、行数、模型和总成本上限；
- provider journal 分开记录 completed、content-ready 与 transport-ambiguous，歧义 POST 不在传输层自动重发；
- 已计费但越过 Episode token 阈值的响应仍计入 provider audit 和正式成本，但不进入可信 planner；
- token-stopped Episode 终止 Investigation，不能用后续 Episode 隐式复用超额探索；
- 首个 OmniPaxos 样本以 59,063 tokens 完成 Risk 三次修订和一次源码查询，但在 Scenario 响应后越过原
  50,000-token 阈值，因此没有 Action、PSS、Replay、Oracle 或 finding；这只是 Agent 前端校准证据；
- 同字节请求随后在 900 秒仍无响应，说明正式长实验还必须报告 provider 可用性和歧义调用，不能把它们
  归因于 Agent 搜索能力或协议行为。

OmniPaxos M4c 的显式上限现为每 Episode 120,000 tokens、六轮合计 720,000 tokens、最多 36 次调用。
主校准通道改为 DeepSeek 官方 API，OpenRouter 保留为独立对照；两者使用不同 MethodSpec、工件和成本账本，
同一 Investigation 内不自动 fallback。DeepSeek 官方减少 provider 路由变量，但不能自动解决总请求 timeout、
usage 对账或结构化输出接受问题。它的 `json_object` 只提供 JSON 语法约束，现有本地 typed parser、Schema、
语义校验和 repair feedback 继续决定候选是否可接受。

首个官方 API 单 Episode 已确认链路可用：Risk/Scenario 两个完整调用分别使用 32,201 和 24,918 tokens。
Scenario 初稿违反 typed selector schema 后被本地拒绝，证明 `json_object` 没有被误当作 strict schema；但修正调用
因 300 秒 Episode deadline 耗尽而成为 usage unknown。该失败场景说明 wall-clock 必须覆盖正常修正循环，故后续
OmniPaxos 校准改用 1,200,000 ms Episode wall-clock，仍保留原有调用数和 token 上限，不新增 gate 或预算 DSL。

fresh v2 已进一步完成完整闭环：116,912 tokens 形成可恢复的 29-record V3 Bundle 和 29/29 Replay，
但 Risk 未达到。失败来自 Agent 表达而非执行底座：node selector 类型两次修正、stopped feedback 的 intent 修正，
以及最终 milestone 前置条件自依赖。Scenario prompt v11 将三条规则显式化；随后以两 Episode 调查检验 Memory 和
执行反馈能否避免重复消耗，而不先扩展 Action/Observation/Oracle。

随后两 Episode 尝试又给出新的 transport 负证据：Episode 1 的第三次 Scenario 调用在 high thinking 下占满
32,000 output tokens，导致 `agent-response-rejected`，Episode 2 未启动。针对这个具体失败，方法身份升级为
M4c-v4：Risk transport 继续 high/32K，Scenario transport 改为 disabled/16K，二者分别进入同一 MethodSpec。
这不是新增预算或 gate，而是把不同 Agent 职责的真实调用配置准确绑定并避免 Scenario 内部推理挤占结构化输出。

实现与实验顺序固定为：

1. 已完成：真正执行 `session_wall_clock_ms`，并区分 Episode deadline 与单调用 deadline；
2. 已完成：将 provider、endpoint、thinking、structured-output、stream/timeout、路由和 fallback 策略纳入现有
   `AgentTransportFreeze`/MethodSpec；实际响应 provider/request ID 等只进入安全 call audit；
3. 已完成：usage unknown 仍计一次调用并报告 `unreconciled_model_calls`，不能作为零 token 通过正式工件或触发自动重试；
4. 已完成：抽出 provider-neutral journal transport，在其下实现 OpenRouter strict `json_schema` 和 DeepSeek 官方
   `json_object` 两个实现；
5. Risk 先校准 thinking high，Scenario 比较 low/disabled；输出上限先比较 12K/16K，不默认使用 max；
6. 按单调用、单 Episode、两 Episode、六 Episode 逐级放大，前一级必须得到完整、可恢复工件；
7. 在长实验前让已有 consensus semantic hints 参与可信 selector；只有真实 no-match/ambiguous 仍无法表达时，
   才增加 Target-local selector 扩展。

路由、timeout 和输出模式变化进入已有 MethodSpec，不新增第二套 hash、baseline 或通用 gate。由于 DeepSeek
官方是新的外部数据接收方，真实调用仍需要单独 key、明确的材料范围授权和费用上限。

fresh v4 两 Episode 实验证明 provider 边界已稳定，但 Scenario 的 disabled-thinking 修正过度：
8 次调用均一次返回且 usage 可对账，共 115,194 tokens；两轮 Risk 各生成 3 个候选，
但 6 次 Scenario 均在 typed proposal 阶段停止，成功 Action、PSS、Bundle 和 Replay 都为 0。
六份响应都组合了 `intent=continue + branch_id`，而本地 contract 禁止该组合；后续响应又在
stopped feedback 后继续使用 `continue`。更重要的是，prompt 要求 stopped 后必须 `revise`，
但当前 `available_intents` 仍同时提供 `continue/branch/revise`，这是本地契约自身的不一致，
不能只归因于模型能力。

### M4d：Scenario Contract Convergence（本地实现与回归完成）

本阶段不修改共识实现、Adapter 能力、PSS 或 Oracle，只收敛 Scenario Agent 与可信执行器之间的计划协议：

1. 首次规划只允许推进当前路径，不在尚无可执行路径时提前暴露 branch/control/ablate；
2. proposal 或某个 step 停止后，下一轮仅暴露修复当前路径所需的最小意图；已有可信
   ProgressDelta 时可保留零 Action 的 abandon；
3. 对可解码但验证失败的 proposal，保留有界结构摘要，返回稳定的字段级 reason code 与当前
   allowed intents，不替 Agent 选择 Action；
4. 第二个及之后的 plan step 机械拒绝当前 frontier ActionID，必须使用会在新 frontier 上重新
   concretize 的语义 selector；
5. summary 保存每次 attempt 的 intent、outcome、reason、validation issue 和是否进入执行，
   原始 prompt/响应仍只留在 provider journal；
6. 将 v4 的 6 份真实失败响应作为普通回归输入，证明其会得到精确、可修复的机械反馈。

Scenario 不再被当成无推理 JSON formatter。完成上述契约收敛后，下一个最小决策实验是
high-thinking 的 fresh 单 Episode；成功条件只是“至少一个真实 Action 执行并形成 fresh-Replay
稳定 Bundle”，不预设 Risk 必须到达或 Oracle 必须报告 finding。在该单轮闭环之前，不扩大
Episode 数、Agent 数、Action/Observation 或修改共识实现。

实现状态：prompt v12 已对单一 `continue`/`revise` 阶段使用严格的 `intent + plan` 最小形状；
可信解析保留可解码失败 proposal，并返回稳定的字段级 issue 与当轮 allowed intents；attempt feedback
已进入 compact summary；第二步以后使用当前-frontier ActionID 会被机械拒绝。v4 六份真实失败响应、
intent 阶段、分支/选择边界及 OmniPaxos 工件恢复均有普通回归覆盖。Scenario transport 恢复为
high-thinking/32K，方法实现版本为 `m4d-v1`。这些修改不改变 Runtime、Target、PSS、Replay 或 Oracle。

### M4e：Scenario-to-Action 单轮校准（完成）

在新的明确材料与费用授权下运行一个 fresh OmniPaxos Episode，只回答一个问题：收敛后的 Scenario
协议能否把已接受 Risk 转成至少一个真实 Action，并形成 fresh-Replay 稳定 Bundle。若成功，再检查
Risk/PSS/Oracle；若失败，必须用 compact attempt feedback 将失败归为 proposal contract、selector
解析或 Target capability，而不能用 `planning-failed` 代替根因。M4e 不以 finding 为成功条件，也不因
单轮成功直接扩大到六轮或 private holdout。

本地预检已经完成：复用活动 OmniPaxos Agentic 垂直测试，而不是增加新的校准执行器。测试从与真实调用
相同的 provider request schema 中确认初始 intent 只有 `continue`、不存在 branch 字段；合法 proposal 随后
经过真实 frontier 绑定、Action 执行、V3 Bundle、PSS、Oracle 和 fresh Replay。primary/replay 与
Scenario decision 均必须非零。此预检只排除本地契约与执行器不兼容，真实模型单 Episode 仍是 M4e 的实验步骤。

真实 DeepSeek 官方单 Episode 随后完成：4 次调用、105,334 tokens，三次 Scenario proposal 均进入执行；
最终 8 个 Scenario decisions 形成 31-record V3 Bundle，primary/replay work 为 33/33，PSS
protocol/control/joint 为 6/28/29，Oracle finding 为 0，终态工件可离线恢复。M4e 因而证明了真实模型响应
可以通过 prompt v12 进入 Action、Bundle 和 Replay，但没有证明假设达到或发现协议问题。

本轮同时给出下一阶段的具体失败样本：accepted Risk 把 `participant` 与 `participant-node` 复用同一 binding，
实际域值为 `n1@1` 与 `n1`，使首个 milestone 在已有 Invoke 时仍不可满足；后续两次 revision 又分别得到
`no-match` 与 `ambiguous`。因此下一阶段先扩充现有 Risk field/binding domain 描述和 selector repair feedback，
不新增 Action DSL、协议专用全局 ActionKind 或新的评测 gate。

### M4f：Binding Domain 与 Selector Repair（完成）

1. 为现有 Observation fields 提供 Agent 可见的 binding domain；
2. 对同一变量跨不兼容 domain 的复用给出机械失败，而不是进入昂贵 Scenario；
3. 将 selector 的 no-match/ambiguous 候选计数和造成冲突的字段反馈给修正调用；
4. 用 M4e 三份真实 Scenario proposal 和 accepted Risk 作为回归输入；
5. 修复后先做本地垂直测试，再决定是否运行第二个付费单 Episode。

这里已有明确失败场景，普通字符串校验不能发现 `participant=n1@1` 与 `participant-node=n1` 的 domain 冲突；
因此允许在现有 typed validation 中补 domain 检查，但不增加 hash、baseline、冻结合同或正式 admission gate。

实现结果：`binding_domains` 由已有 Observation capability 派生，不要求 Adapter 再声明一份
协议专用表。节点实例域与节点 ID 域不再能共用 `bind_as`；Target 声明的 `node-id`
attribute 仍可与通用 `participant-node` 连接。不兼容复用在 Risk assessment 阶段机械拒绝，
不进入 Scenario。Scenario 失败则新增逐字段候选数 `selector_trace`，下一次 `revise`
能看到第一个将候选降为零的冲突字段，或 ambiguous 的最终候选数。M4e 实际 accepted
Risk 形状和三份 Scenario proposal 已进入普通回归。

### M4g：通用语义 Selector 绑定（完成）

1. 让已有 `actor_role`、`message_class`、`epoch_relation`、`operation_state` 参与 frontier 候选收窄；
2. 绑定仍由可信代码在当前 enabled Action 与同步 semantic hints 上完成，Agent 不生成 ActionID；
3. 用 etcd/raft 与 OmniPaxos 的普通字段同时回归，不在 Core 中增加协议名称或协议分支；
4. 只有新的可复现 no-match/ambiguous 证明通用字段不足时，才评估 Target-local selector 扩展。

实现结果：四个闭集语义字段已进入现有 `FrontierActionSelector`和 `selector_trace`。执行时不信任
Agent 携带的语义，而是在每个当前 frontier 上重新调用 Target projector，并校验 hint 的 Action ID/digest。
etcd/raft 和 OmniPaxos 用同一 Core matcher 通过垂直回归；未增加协议名称、Target-local selector DSL
或新 ActionKind。

### M4h：修复后真实单 Episode 校准（完成）

1. 使用 prompt v13/structured output v5 和新 MethodSpec digest；
2. 只运行一个 DeepSeek 官方 OmniPaxos Episode，不自动放大；
3. 检查 Risk portfolio 是否避免 binding-domain mismatch；
4. 检查 Scenario revision 是否利用 `selector_trace` 和通用语义 selector；
5. 继续以 Action/Bundle/fresh Replay 为管线成功标准，Risk reached 和 finding 只是结果，不是必须成功门槛。

实验结果：单 Episode 使用 1 次 Risk 和 3 次 Scenario 调用，共 97,522 tokens。Risk portfolio 首次即接受
`recovery-after-single-drop`，其 binding 没有跨越不兼容域。三次 Scenario proposal 均进入可信执行，
M4e 的 `no-match`/`ambiguous` 没有重现。最终 16 个 Scenario decisions 形成 39-record Bundle；
primary/replay work 为 41/41 且 Trace digest 相同，PSS protocol/control/joint 为 4/37/38，Oracle 为 0。

这不是协议 finding：三次计划都停止于 `milestone-unreachable`，首个缺失 milestone 为
`leader-handoff`，最终因 Scenario call budget 耗尽而结束。结果说明当前瓶颈已经从“计划无法绑定到 Action”
收敛为“Agent 无法根据已有执行证据促成目标状态转换”。下一阶段不扩大 Episode 数或 Action DSL，而是先让
ProgressDelta 更明确地区分已完成 milestone、当前缺失 milestone、自然进展循环和仍可用的干预方向。

### M4i：Milestone-directed Scenario revision（完成）

1. 从现有 Trace/Observation 机械生成“已满足 milestone + 首个缺失 milestone + 最近相关 Action/Observation”反馈；
2. 明确区分 selector 已成功但目标状态未发生，与 selector no-match/ambiguous，避免 Agent 重复修订选择语法；
3. 让 Scenario Agent 在既有调用预算内选择继续、修订或放弃，不增加新的 verdict 权限；
4. 先用 M4h 的 `leader-handoff` 缺失样本做普通回归，再决定是否需要新的付费单 Episode；
5. 若 Target 当前能力确实无法促成该状态，诚实返回 capability/fidelity gap，而不是把它记为假设失败。

实现结果：`after_milestone` 等待现在分别返回 `milestone-wait-client-terminal` 和
`milestone-wait-quiescent`；预算停止继续使用独立原因。`ProgressDelta` 增加 milestone progress 分类、
新 evidence 的 milestone/step/kind、Action kind 计数、Timer callback/逻辑时钟推进/逻辑时间增量、
fault allowance/usage/remaining 和当前非闭包干预类型。Scenario prompt v14 明确这些字段只用于
continue/revise/abandon 决策，不能把 stalled/repeated 当成协议 verdict。

普通回归覆盖客户端终止与 quiescent 的精确区分、长 Tick 循环、fault 剩余额度、可用 crash 干预和
Timer callback/clock advance 分离。每轮紧凑 `summary.json` 也持久化同一 ProgressDelta，便于恢复后审计
Agent 实际收到的机械反馈。M4i 未修改公共 ActionKind、具体协议、Adapter、PSS 或 Oracle，也未调用模型。

### M4j：生成共识可测试性接口（M4j1 最小公共闭环完成）

非冻结草案位于 `docs/generated-consensus-testability.md`。它把接入接口分为公共原因型控制动词、受控资源/HostEffect、
Target-local 类型与语义投影、当前 FaultEnvelope 四层，并按 T1 算法级确定性、T2 持久化恢复、T3 协议扩展分级。
资格继续复用 `PortableCFTProfileV3`、Manifest、组合测试和 Replay，不新建平行 contract。

M4j1 已在现有 Manifest 上增加可选的消息 type hint/metadata key 与 HostEffect phase/durability/outcome 描述，
并贯通 Runtime emission 校验、AgentTargetSurface、enabled frontier、Scenario selector、Trace 和 Replay。
协议无关 described fixture 同时覆盖 effect complete/fail 和超声明拒绝；旧 fixture identity/Trace 不变，
没有新增平行 contract、hash、baseline 或 gate。

下一步不立即给每个真实 Target 填满新字段。先使用 M4j1 作为接入扩展缝：若恢复具体 Target 改造，按真实
Agent hypothesis 暴露的缺口逐项声明并做组合测试；无法由 wrapper/host 接管的行为继续作为 fidelity boundary。
在用户明确恢复具体 Target 改造前，不修改 etcd/raft、OmniPaxos 或 Hashicorp Raft 实现。

### M5：效果实验

- public calibration；
- private holdout；
- Random/DFS/专家/Agent/multi-Agent 对照；
- 长时多 seed 实验；
- finding、探索、成本、能力四面报告。

### M6：第三协议与接入评估

选择不以 Raft/Paxos 日志复制为唯一形态的协议，例如 HotStuff/Tendermint 类实现。重点测量新增 Target-local
Observation/Oracle/Action 所需改动是否局限在扩展缝，而不是追求“零代码接入”。

## 9. 开发与验证节奏

采用垂直切片和小步落地：

1. 每次只处理一个可陈述的闭环；
2. 先跑受影响包；
3. 一个完整阶段结束再跑 `go test ./...`、`go vet ./...` 和 `git diff --check`；
4. race 仅用于明确里程碑；若已知 race 测试持续超时，记录而不反复消耗阶段时间；
5. 真实外部模型调用只在用户明确授权的 provider、材料范围、模型与费用上限内运行；
6. 实验工件与代码变更分开说明；
7. 无活动引用的历史 wrapper、测试和文档及时删除。

## 10. 可行性自审查

主要风险与约束：

- **Agent 很强但 Target 表达不足**：用 capability/fidelity gap 诚实报告，并由真实候选驱动 Target Pack 扩展；
- **基础设施无限膨胀**：只保留活动垂直闭环，不为假想协议建立表达全集；
- **覆盖指标自嗨**：PSS/义务只作为探索面，最终与独立 finding、成本和对照实验共同报告；
- **过度限制 Agent**：限制执行权和 verdict，不限制其提出风险、查询材料和修正调查；
- **过度耦合单一协议**：公共 Core 不导入具体实现，至少用 etcd/raft 与 OmniPaxos 持续验证扩展缝；
- **确定性成本过高**：live branch 降低中间 Replay，最终候选仍 fresh Replay；
- **生产外推过度**：RawNode/MemoryStorage/worker 盲区进入 Dossier 和 fidelity，报告明确限定结论范围。

方向检查只有一个问题：

> 当前新增工作是否让 Agent 更有能力发现共识实现问题，或让该发现更可信、更可复现、更可比较？

如果答案只是“增加了更多 schema、gate、历史入口或阶段文件”，则不应继续。
