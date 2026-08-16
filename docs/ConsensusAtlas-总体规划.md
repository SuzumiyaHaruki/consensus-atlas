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
- `minimize`：对已确认 finding 缩短轨迹。

当前首先实现 continue/revise。branch/control/ablate/minimize 在底座稳定后扩展，不通过预置固定场景替代 Agent 判断。

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

### 5.3 恢复

provider journal 先落盘请求/响应和 usage。Episode 恢复重用已完成调用；终态从 bundle 重新派生 Risk/PSS/Oracle，不访问
provider、key 或 SUT。失败 Action 的 terminal outcome 与成功 Trace 分开保存。

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
消费活动 Agentic artifact，不能维护第二套执行契约。

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
- A2/A8/旧 Campaign 路径清理。

尚未完成：

- 当前 Agentic artifact 的 private holdout evaluator；
- branch/control/ablate/minimize；
- 数百 Action 长时校准；
- 更广但受控的源码导航；
- 第三个非 Raft/Paxos 形态 Target；
- Agent 相对 baseline 的长时效果证据；
- 新问题发现与根因最小化。

## 8. 路线阶段

### M1：最小底座闭合与瘦身（当前）

- 统一 client terminal/Risk/budget 终止语义；
- 删除固定 24-action compatibility checkpoint；
- 解耦 active root 与旧 A2/stateless Campaign；
- 删除多代入口和重复契约；
- 保持两个 Target、Replay、Oracle 全量测试绿色。

完成标准：活动 CLI、两个 Target、终态恢复和 Bundle evaluator 均可编译测试；文档只描述当前路径。

### M2：Agentic holdout evaluator

- 定义 evaluator 输入为当前 episode summary/bundle/journal audit；
- 对 control/candidate SUT 使用同一 Agentic 方法和预算；
- evaluator 从保存证据重算 Oracle，不启动第二套 A8 session；
- 输出 killed/survived/false-positive/invalid 和成本。

不增加无具体失败场景的 frozen contract 或 gate；优先复用现有 Bundle/MethodSpec/defectbench 类型。

### M3：长轨迹与自适应调查

- 运行数百 Action 的 PreVote/ballot/message-delay 场景；
- 实现 branch/control/ablate；
- 让 Agent 根据 ProgressDelta 切换或放弃假设；
- 测量 live branch 相对重复 Replay 的成本。

### M4：Agent 能力释放

- 扩大只读源码搜索范围；
- 增加 investigation 调用/时间预算；
- 在隔离工作区允许 native-test candidate；
- 从已确认根因生成变体并自动最小化。

权限扩大必须保持执行事实、Oracle verdict 和正式发布边界不变。

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
5. 真实 OpenRouter 调用只在用户明确授权的实验中运行；
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
