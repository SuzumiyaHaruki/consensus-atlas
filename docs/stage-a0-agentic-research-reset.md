# A0：Agentic Consensus Testing 研究主线重置

日期：2026-08-13

分支：`feature/agentic-consensus-testing`

基点：`feature/control-runtime-v2` commit `0106e2c`

状态：规划冻结；本阶段只修改研究与入口文档，不修改 Runtime、Adapter、搜索或模型代码

## 1. 为什么分叉

截至 M5.23R4b，ConsensusAtlas 已有一套较强的共识测试底座：

- Runtime 拥有消息、自然时间、生命周期、持久化副作用和外部输入；
- etcd/raft 与 OmniPaxos 已通过同一 Action/Runtime 执行至少一部分严格路径；
- exact-prefix 搜索、fresh Replay、PSS、Agreement Oracle、Campaign 和成本账本已经连通；
- candidate/control 隔离与 formal evaluator 已具备可复用接口。

但现行 Agent 只排列当前 `ActionFrontierView` 的 ActionID。M5.23g 的真实模型在三个公开 root 上进行
6 次调用、消耗 28,335 tokens，最终 Action 顺序和 15-state PSS 集合均与 canonical 相同。

所以当前实现没有偏离“共识测试”，却阶段性偏离了“以 Agent 为主体的共识测试”。如果继续扩展恢复、
digest、qualification 和 evaluator，而不让 Agent 决定测试假设与语义搜索目标，最终贡献会变成
“确定性共识搜索框架附带一个 LLM 排序器”。本分支用于正式纠正这一失衡。

## 2. 一句话目标

> **ConsensusAtlas 是一个由协议感知 Agent 形成并修正测试假设、通过统一控制层驱动真实共识实现，
> 并依据独立 Oracle 和语义测试反馈持续探索的多 Agent 测试系统。**

三个关键词缺一不可：

- 去掉 Agent：退化为传统 model checking、fuzzing 或 chaos testing；
- 去掉共识语义：退化为通用分布式系统调度器；
- 去掉真实执行、Replay 和评价：退化为代码审查或测试代码生成 demo。

## 3. 与 Agora、MODIST 的组合关系

Agora 值得学习的是 Agent 主体性：Agent 负责形成协议漏洞假设、生成场景、写/修测试并读取执行反馈。
它主要借助每个仓库已有的测试 helper，不提供统一消息/时间/生命周期控制层，测试断言也由 Agent 生成，
最终允许人工复核和一定误报。

MODIST 值得学习的是系统化受控执行：调度器控制节点动作、自然时间和外部调用，以可重复方式探索执行。
它不提供协议感知 Agent，也不解决共识语义测试反馈。

ConsensusAtlas 的目标组合是：

```text
Agora 式 Agent 主体性
          +
MODIST 式受控执行
          +
共识 PSS / Risk / obligation 反馈
          +
独立 Replay / Oracle / evaluator
```

项目不追求比 Agora“处处更严格”。只对会影响执行事实和研究结论的边界保持严格；Agent 的假设和计划
允许不确定、不可达和需要修正。

## 4. 权限模型

### 4.1 Agent 可以决定

- 从源码、文档和冻结知识中提出协议级 `TestHypothesis`；
- 选择一个 episode 的 risk/obligation/语义目标；
- 在可信层提供的全局 WorkItem 候选之间排序；
- 选择冻结目录中的 DFS、best-first、mutation 等局部搜索工具；
- 引用当前 enabled ActionID，或提交由可信 concretizer 解析的有界 selector；
- 根据不可达、near-miss、PSS/Risk/义务变化和 Oracle activation 修正下一轮计划；
- 解释失败并提出最小化方向，但解释不产生正式 verdict。

### 4.2 Agent 不能决定

- 哪些动作真实 enabled；
- 不存在的消息、任意时间值、任意故障或自由 payload；
- PSS、RiskWitness、Coverage 和 evidence 真值；
- state equality、independence 和剪枝正确性；
- active Profile 分母、Oracle、candidate/control 身份和总实验预算；
- 缺陷是否正式成立以及最终得分。

权限原则概括为：

> **Agent 的提议权和搜索权较大；Agent 的事实权和最终判定权为零。**

## 5. 第一版角色

第一版不实现八个命名 Agent，只保留两个真正不同的认知职责。

### 5.1 Protocol/Hypothesis Agent

输入：

- 协议源码/文档的受限读取工具；
- Protocol Knowledge 与来源；
- Adapter Manifest 和 validated capability；
- Core/Extended semantic schema；
- 公开历史机械反馈，不含隐藏 candidate/root cause。

输出 `TestHypothesis`，至少表达：

- 风险/义务目标 ID；
- 假设的协议前置语义；
- 希望到达或观察的状态关系；
- 允许的故障边界；
- 为什么该目标可能产生新的测试价值。

假设不是事实，可以被执行反馈否定。

### 5.2 Explorer Agent

输入：

- 当前 `TestHypothesis`；
- prefix-bound PSS/Risk progress；
- workload phase、fault usage 和剩余固定预算；
- 全局候选 WorkItem/semantic bucket；
- 前一 episode 的机械报告。

输出 `EpisodePlan`：选择语义目标、候选优先级、可信搜索算子和有界停止/反馈条件。可信内核把它编译成
真实 Action 和局部搜索；非法项被拒绝并形成 `EpisodeReport`。Explorer 可以继续、修正或放弃假设。

### 5.3 可选 Critic 与 Onboarding

- Critic 只有在两角色闭环的失败日志表明独立反思能够降低 invalid proposal 或提高检出时才拆出；
- Onboarding Agent 是独立研究轨，评价新协议接入成本，不与 Agent-v2 搜索效果同时实现；
- Coordinator、Runtime 和 Oracle 永远不是 LLM Agent。

## 6. 最小闭环与拟议接口

```text
Protocol/Hypothesis Agent
        |
        | TestHypothesis
        v
Explorer Agent <------------------------------+
        |                                      |
        | EpisodePlan                          | EpisodeReport
        v                                      |
Trusted Episode Compiler / Search Kernel       |
        |                                      |
        v                                      |
Control Runtime -> Trace -> PSS/Risk/Oracle ---+
        |
        v
fresh Replay + evaluator
```

第一版只需要四个新边界对象，具体字段在 A1 中以最小消费者驱动定义：

1. `TestHypothesis`：Agent 提出的协议级测试命题；
2. `SemanticEpisodeView`：可信层生成、与精确 prefix/candidate-set digest 绑定的规划输入；
3. `EpisodePlan`：Agent 对目标、候选和冻结算子的有界提议；
4. `EpisodeReport`：执行、拒绝、near-miss、语义进度、成本和 Replay 引用。

禁止为未来角色预建 Blackboard、通用工作流 DSL 或新的 Ledger 家族。已有 Trace、ExecutionBundle、
PSS、RiskWitness、Campaign 和 WorkLedger 应直接复用。

## 7. 哪些部分冻结，哪些部分放松

### 7.1 冻结为平台

- `control.Action`、Produced/Released/Delivered 消息所有权；
- 自然虚拟时间、crash/restart、host effect 与 entropy 边界；
- Adapter/Qualification 的现有能力模型；
- exact-prefix reconstruction、fresh Replay 和 trace identity；
- 独立 Agreement/target Oracle 与 formal evaluator；
- candidate/control blindness、root corpus 和完整 Runtime work 预算；
- PSS/RiskWitness 只能由可信 projection 产生。

除非 Agent-v2 的最小闭环被现有接口机械阻塞，否则不修改上述层的大版本。

### 7.2 可以放松

- 不要求 Agent 假设在执行前被机械证明正确；
- 不要求 Agent 第一次计划可达或合法；
- Agent 输出不要求位级确定，依靠冻结配置、重复实验和完整调用记录处理波动；
- 允许解释性 knowledge 带来源和置信度，只有进入执行的引用需要机械绑定；
- 公开开发 episode 只需保存请求、响应、digest、token、执行和 Replay 引用，不继续扩建生产级调用事务；
- 不要求所有协议暴露完全相同能力，实验按共同 validated subset 和各自最大能力分开报告；
- PSS 不是完备状态模型，义务覆盖不是协议正确率。

## 8. 核心研究问题

### RQ1：统一共识控制是否可行

不同共识协议能否通过薄 Binding 暴露消息、自然时间、生命周期、持久化和输入，而不为每个协议重写
Agent、搜索器、Replay 和 evaluator？

评价：目标专用 LOC、人工接入时间、validated capability、Replay 成功率和共享代码复用。

### RQ2：Agent 是否改善共识测试

在相同完整 Runtime work 预算下，协议感知 Agent 是否比 seeded random、DFS 和确定性 semantic
best-first 检出更多独立隐藏根因，或者获得更低 work-to-kill？

### RQ3：多 Agent 是否必要

在相同模型、token、调用和 Runtime work 预算下，Protocol/Hypothesis 与 Explorer 的职责分离，是否比
单 Agent 减少无效计划、提高跨协议迁移或提高缺陷检出？如果没有，就保留单 Agent 作为最终系统。

## 9. 评价设计

### 9.1 公开开发/校准

先使用公开 candidate/control 或语义 calibration，使 Agent 可以得到反馈但看不到直接触发轨迹。第一目标是：

- Agent-v2 产生与 canonical 不同的真实执行；
- 至少一个计划收到机械拒绝/near-miss 后发生可审计修正；
- accepted trace 能 fresh Replay；
- 公开 control 不产生虚假 kill；
- 相同 episode schema 能在 etcd/raft 和 OmniPaxos 上执行。

这些只证明闭环和行为权限，不证明方法优越。

### 9.2 正式评测方法

最小方法矩阵：

1. seeded uniform/random；
2. bounded DFS；
3. deterministic semantic best-first；
4. Agent-v1 frontier permutation（权限消融）；
5. single Agent-v2；
6. two-role Agent-v2。

DPOR 不与 Agent-v2 同阶段加入，避免无法归因。

### 9.3 指标

主要外部指标：

- candidate 独立根因检出率；
- correct-control false-positive rate；
- work-to-first-kill 与重复检出率；
- invalid/deferred trial；
- 完整 Runtime work、墙钟、模型调用、token 和费用。

解释指标：

- PSS state/transition discovery；
- obligation/RiskWitness 进度；
- Agent invalid proposal、repair 和重复率；
- Oracle activation 与无进展原因。

Agent 优化的 PSS/义务不能作为证明 Agent 有效的主要结果。

## 10. 实施阶段

### A0：研究重置与分支冻结（本阶段）

- 创建长期分支；
- 更新总体规划、README、CURRENT_STAGE、Control Runtime Agent 边界和仓库指令；
- 不改代码，不调用模型，不产生方法效果结论。

### A1：Semantic Episode 契约与 Search Kernel 解耦

- 从现有 DFS 中分离 `SearchAlgorithm` 与 `GuidancePolicy`，保持旧 DFS 行为和 digest 可验证；
- 定义四个最小 episode 对象；
- 由 deterministic fixture 完成 hypothesis -> plan -> reject/execute -> report；
- 不接真实 LLM，不实现 DPOR，不新增 Oracle。

退出条件：Agent-v1 仍可作为旧策略运行；新契约不能添加、删除或伪造 Runtime Action。

### A2：公开 single Agent-v2 闭环

- 先实现 deterministic semantic best-first 强基线；
- 接入 Explorer Agent 的 batch/episode 调用；
- 在 etcd/raft 公开 calibration 上完成至少一次反馈修正和行为差异；
- 完整报告 Runtime/model 成本和 Replay。

退出条件不是“发现新缺陷”，而是 Agent 确实影响测试、反馈确实影响后续计划且全部执行可复核。

### A3：Protocol/Hypothesis + Explorer 两角色消融

- 增加只读 Protocol/Hypothesis Agent；
- 以相同总 token、调用和 Runtime work 比较单 Agent与两角色；
- 若两角色没有减少无效计划或提高外部效果，停止多 Agent 扩张并保留负结果。

### A4：OmniPaxos 跨协议闭环

- 复用同一 episode schema、Agent tools 和 Search Kernel；
- 只新增目标知识、薄 semantic mapping 和 composition；
- 分别报告共同 capability subset 与目标最大能力。

### A5：方法冻结与隐藏评测

- curator 在仓库外冻结 candidate/control、root cause、root corpus、重复次数和预算；
- 在不查看隐藏结果的前提下冻结 Agent-v2 prompt/tool/schema；
- 运行六组最小矩阵，主要报告缺陷、误报和成本；
- 允许得出 Agent-v2 或多 Agent 无优势的负结论。

### A6：条件扩展

只有明确的实验失败类型才能触发：Critic Agent、Onboarding Agent、DPOR、limited-BFT 或第三协议。

## 11. 停止线与代码增长门

- A1 不新增第二套 Runtime、Trace、Campaign、PSS 或 evaluator；
- A1/A2 每增加一个通用 schema，必须有当前生产消费者和删除条件；
- 如果 Agent-v2 仍无 behavior delta，先检查语义输入/任务设计，不增加 Agent 数量；
- 如果 deterministic semantic best-first 与 Agent-v2 等效，诚实保留强基线结论；
- 如果两 Agent 不优于单 Agent，就不再增加 Critic/Planner/Scenario 等角色；
- 在 A2 公开闭环完成前，不继续扩展调用恢复、formal contract 或资格语法；
- 发现未知上游缺陷是 bonus，不是继续扩张系统的前置条件。

## 12. 本阶段没有证明

- 没有实现 Agent-v2 或真正的多 Agent 闭环；
- 没有证明 Agent 优于任何基线；
- 没有增加新协议、缺陷、Oracle、PSS 状态或 Coverage；
- 没有改变 M5.23g 的公开负结果；
- 没有调用模型，也没有读取模型凭据；
- 只是正式改变后续研究资源分配和权限目标。

## 13. 下一步

进入 A1，但第一份代码提交只做行为不变的搜索算法/指导策略解耦和 deterministic fixture。不要同时加入
真实模型、DPOR、预算分配或新 target；先建立一个小而可验证的 Semantic Episode 纵向切片。
