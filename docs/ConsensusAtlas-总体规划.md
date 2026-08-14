# ConsensusAtlas 总体规划

> 状态：Draft v2.15（A6f 最小共识 Oracle 闭环完成）
> 日期：2026-08-14
> 适用分支：`feature/agentic-consensus-testing`

## 0. 一句话目标

> ConsensusAtlas 是一个利用协议感知 Agent 形成并修正测试假设、利用统一控制层在真实共识实现上确定执行、
> 再由独立 Replay、语义投影和 Oracle 评价结果的分布式共识测试系统。

“Agent”“共识”“测试”缺一不可：

- 没有 Agent，会退化为普通 model checking/fuzzing；
- 没有共识语义，会退化为通用故障调度器；
- 没有真实执行与独立判定，会退化为测试代码生成 demo。

Agentic 是架构目标；多 Agent 是否优于单 Agent 必须通过同预算实验验证，不能预设。

总设计原则是：

> **开放规划，受控执行，机械反馈，独立判定。**

Agent 的计划可以不确定、不可达并在反馈后修正；一旦计划被可信层接受，具体 Action 序列、执行事实、
Replay、语义证据和正式 verdict 必须可验证。系统不要求相同 prompt 永远产生相同输出，而要求相同的已接受
计划与随机输入能够被确定地执行和重放。

## 1. 最终用户流程

### 1.1 输入什么

接入一个新共识时，人工提供：

1. 共识实现或可运行构建；
2. Target Pack 中的 Binding，把已有接口映射为统一 Action/ProducedItem；
3. Target Pack 中的 Evidence Extractor，把目标证据转为规范化语义事实；
4. Protocol Pack：协议知识、Risk catalog、语义标签和性质；
5. Experiment Config：拓扑、时间参数、workload、故障范围和执行/模型预算；
6. 只有复杂证据无法用通用事实表达时才提供 target-local projector/Oracle hook。

目标不是“零人工理解协议”，而是把人工工作限制在一次接入事实与性质定义，不要求人工编写大量具体测试轨迹。

### 1.2 如何处理

```text
SUT + Target Pack + Protocol Pack + Experiment Config
                         |
                         v
              mechanical qualification
                         |
                         v
              Hypothesis Role
                         |
                    TestHypothesis
                         |
                         v
               Explorer Role
                         |
             bounded ScenarioPlan
                         |
                         v
        trusted concretizer + Control Runtime
                         |
              Trace / Evidence / result
                         |
          +--------------+----------------+
          |                              |
 PSS/Risk/obligation feedback       fresh Replay
          |                              |
          +----> Analysis Role            v
                     |                Oracle
                     v                   |
             Agent 修正 hypothesis/plan  |
                                        |
                                        v
                           evaluator / session summary
```

Hypothesis Role 决定测什么，Explorer Role 决定如何构造短场景，Analysis Role 根据公开机械反馈提出修正。
三个角色首先是认知职责，不预设必须对应三个 LLM 实例。可信层决定 selector 能否解析、enabled Action、执行事实、
语义真值和正式 verdict。

### 1.3 得到什么

一次测试 session 输出：

- finding：违反性质的最小可重放前缀，或“未发现正式 violation”；
- coverage evidence：固定义务覆盖、PSS/Risk 发现与时间顺序；
- efficiency：决策、primary/replay work、模型调用、token、wall time；
- qualification report：目标实际提供了哪些控制能力；
- session summary：运行数、有效/无效 episode、重放率、finding 数和成本。

任何百分比都必须写明 Profile、故障范围和可观察能力。不得称为协议正确率或剩余缺陷概率。

## 2. 控制层

### 2.1 统一 Action

公共 Action 只表达跨实现稳定的控制意图：

- deliver/drop/duplicate message；
- crash/restart；
- fire earliest due temporal item；
- partition/heal；
- complete host/application effect；
- invoke external workload。

Action 引用 Runtime 当前给出的 ID。Agent 不能构造任意消息、时间、节点 incarnation 或 payload。

### 2.2 Runtime 所有权

Control Runtime 负责：

- 保存尚未决定命运的消息；
- 只允许投递、丢弃、复制当前待处理消息；
- 用逻辑时间触发自然超时，而不是直接调用“成为 leader”等协议事件；
- 跟踪 crash/restart 与 incarnation；
- 区分 volatile/durable/application effect；
- 记录完整 Trace 和确定性随机输入；
- fresh Replay 同一动作序列。

Adapter 负责把一次 Action 映射到目标已有接口，并返回新 ProducedItem/Evidence。Adapter 不拥有全局调度。

### 2.3 能力差异

统一 Action 不等于统一控制强度。接入结果允许 partial：

- embedded/grey-box 目标可能交出消息和时间所有权；
- black-box 目标可能只能拦截部分网络或生命周期；
- 缺失能力必须显式报告，不能用 no-op 或猜测补齐。

当前跨实现证据：

- etcd/raft：完整 strict 主路径；
- OmniPaxos：非 Raft 的同 Runtime 路径；
- HashiCorp Raft：部分资格，代表黑盒接口上限；
- raft-rs 探索未取得资格，已从 HEAD 删除，不作为现行证据。

## 3. Agent 设计

### 3.1 Protocol/Hypothesis 职责

输入协议知识、能力和公开机械反馈，输出值得调查的语义假设，例如：

- leader change 与 in-flight proposal 的组合；
- quorum 变化前后的 commit/decision continuity；
- 持久化完成与消息释放之间的关系。

它不输出预期缺陷标签、可执行 Oracle 或完整动作脚本。

### 3.2 Explorer 与 ScenarioPlan

当前 Explorer 已从候选排序推进到 A4b 的有限短计划和一次反馈修正。后续完整 Explorer 将接收
hypothesis、当前 prefix、规范化共识关系、PSS/Risk progress 和剩余预算，输出短时域 `ScenarioPlan`：

- semantic objective 与允许的 Action class；
- 当前 ActionID，或基于节点角色、消息类别、workload/epoch 关系的 selector；
- 局部约束、停止条件和每步工作量上限。

Trusted Concretizer 每一步重新读取 `Runtime.EnabledActions()`。A4a 已对零匹配、多匹配和外部步数预算分别
返回 `no-match`、`ambiguous`、`budget-exhausted`；后续加入显式局部约束时再增加
`precondition-failed`。Agent 不能创造未来 ActionID、消息、节点角色或里程碑事实。

Explorer 不应逐 scheduler decision 调用模型。ScenarioPlan 是可修正的短计划；具体 Action 仍逐步由可信层
解析和记录，防止 Agent 退化成昂贵随机 scheduler。

### 3.3 Analysis 职责

Analysis Role 消费 Trace 摘要、PSS/Risk 增量、workload 状态、Runtime 拒绝、Replay 状态和成本，解释场景为何
成功或失败，并建议修正 hypothesis 或 ScenarioPlan。正式 hidden evaluation 中它不能看到 candidate/control
标签、private monitor verdict、root cause、known trigger 或 terminal pass/fail。

初期由同一 Agent 兼任 Hypothesis/Analysis，闭环稳定后再做角色拆分。

### 3.4 单 Agent 与多 Agent

第一条可用闭环先允许一个 Explorer 同时消费人工/固定 hypothesis。闭环稳定后再增加独立 Hypothesis Agent。
论文比较必须保持相同总模型 token、执行 work、root 和 exposure：

- deterministic semantic best-first；
- 单 Explorer；
- Hypothesis + Explorer；
- Hypothesis/Analysis + Explorer；
- 必要时再拆分独立 Analysis 的三角色消融。

如果两 Agent 不优于单 Agent，系统仍是 Agentic testing system，但不能声称多 Agent 分工有效。

## 4. 语义反馈与覆盖

### 4.1 PSS

PSS 用来归一化协议状态，忽略 term 数值、节点编号或无关独立顺序造成的表面差异。目标 Evidence Extractor
优先输出 participant、epoch、decision unit、value、support/decide/apply 等规范化事实，通用 projector 据此
形成 Core PSS；只有复杂证据解析保留为 target-local 代码。PSS 回答“发现了多少新的协议语义状态/转换”，
适合比较搜索效率，没有固定完备分母。

PSS 不单独构成质量分数。Agent 若只追逐新 PSS，可能生成许多易达但无测试价值的轨迹。

### 4.2 测试义务

义务是 Profile 内的有限目标，适合给出覆盖率。义务必须能表达时序和有限组合，例如：

```text
precondition -> trigger -> intermediate relation -> observation
```

组合由协议/能力约束产生，不做全笛卡尔积。不可达项必须通过独立分析或执行证据标记，不能为了提高比例直接删除。

### 4.3 RiskWitness

RiskWitness 是 hypothesis 与真实 prefix 之间的机械桥梁：报告已达到的 milestone 和第一个未达到的
milestone。它不是 Oracle，也不等同于缺陷。

### 4.4 结果表达

至少分开报告：

1.义务覆盖：在冻结 Profile 内取得强证据的比例；
2. PSS/Risk discovery：随 work 增长的状态、转换和 milestone 曲线；
3. finding：隐藏根因检出和正确 control 误报；
4. cost：执行、重放与模型成本。

不合成一个含义不明的总分。只有外部 candidate/control 结果才能支持 Agent 方法效果结论。

## 5. Oracle 与评价

通用 Oracle 只消费协议无关的最小决策投影，例如 participant、decision position 和 value digest。
target-local projector 可以理解协议 Evidence，但不能改变 Oracle 规则。

正式评价使用：

- 正确 control：预期不触发性质违反；
- candidate：包含一个独立已知根因或受控语义变化；
- 相同 workload、fault、预算、source exposure 和 evaluator；
- Agent 不见 candidate/control 标签、diff、known trigger 或 terminal verdict；
- fresh Replay 后才计入有效 finding。

主要指标：

- root-cause detection rate；
- correct-control false positive rate；
- work-to-first-finding；
- replay/reproduction rate；
- invalid episode rate；
- PSS/义务作为解释性指标。

发现当前版本的新问题是强案例，但不是项目成立的前置承诺。

## 6. 当前实现状态

### 已完成

- 协议无关 Action、ProducedItem、Adapter 和唯一 Control Runtime；
- Runtime-owned message、自然时间、crash/restart、effect、entropy 和 strict Replay；
- Adapter qualification 与 partial capability；
- etcd/raft qualified execution、PSS、Risk、Agreement Oracle 和 evaluator；
- OmniPaxos 的同 Runtime 非 Raft 路径；
- bounded exact-prefix DFS 和 deterministic semantic best-first；
- 唯一 `TestHypothesis`、A2 semantic queue proposal/repair 和 A4 短场景计划；
- provider 请求持久化、无凭证恢复和显式 opt-in 模型调用；
- 一次真实 direct-DeepSeek 历史校准和一次显式选择 DeepSeek 的 OpenRouter Scenario 校准。
- 一次真实 DeepSeek/OpenRouter 两 episode session，包含有界传输重试、qualified testing、
  PSS/Risk/Replay/Oracle 聚合和无 provider 恢复。

公开校准只证明 Explorer 能改变 etcd/raft 的搜索前缀。两臂都未达到完整 RiskWitness，不能证明 Agent 优势。

### 尚未完成

- 非公开 candidate/control 重复实验；
- 同一 Agent episode 在第二协议上的复用；
- 多 Agent 同预算消融。

当前活动 etcd/raft Agent 路径的 ProtocolKnowledge、Hypothesis、workload、拓扑/时间参数、Runtime、
FaultEnvelope 和 A2/A4 预算均来自同一 JSON。Binding、证据解码、持久化解释和复杂 decision extraction
继续保留代码。

## 7. A2R 架构收敛原则

本阶段不增加新研究对象，集中删除和平坦化：

- 删除已被 semantic episode 替代的 trace/corpus/PSS 批处理搜索；
- 删除 Agent-v1 frontier-order Campaign，保留当前 Explorer 复用的调用持久化安全边界；
- 删除未资格化的 raft-rs 路径；
- 大型 Trace/bundle 放 ignored `artifacts/`，仓库只保留必要输入和紧凑摘要；
- 历史复现依赖 Git，不在 HEAD 保留多套可执行主线；
- README、CURRENT_STAGE、总体规划和架构只描述当前系统。

默认不新增 hash、冻结 contract、baseline 或 gate。只有能明确给出具体失败场景，并说明 Git、版本号、主键、
事务、唯一约束、类型和普通测试为何不足时，才允许增加。已有认证、数据安全、重放完整性和不可逆操作保护保留。

## 8. 接下来按什么顺序实现

### A3：单 episode 真正闭环

状态：已完成。

目标：把当前 Semantic Explorer 选中的路径送入已有 qualified executor，得到 ExecutionBundle、PSS、Risk、
fresh Replay 和 Oracle 结果。

约束：

- 复用现有 Trace、ExecutionBundle、WorkLedger 和 evaluator；
- 不建立新 Ledger/contract；
- 先用 etcd/raft fixture 与公开 calibration；
- 输出清楚区分 planning result 与 testing result。

完成判据：输入一个 hypothesis 和预算，系统能输出一个可重放测试结果，而不只是一段 prefix 比较。

当前实现已在 etcd/raft composition 中满足该判据：Explorer 首个选择会生成 qualified Bundle、Core PSS、
RiskWitness、fresh Replay 和 Oracle 结果。它证明数据流贯通，不证明 Agent 效果优势。

### A4：短场景计划、反馈修正与模型入口

状态：A4a/A4b/A4c0/A4c 已完成，包括一次成功的真实 OpenRouter Scenario 校准与恢复。

把 Explorer 从 queue ordering 提升为最小 `ScenarioPlan`，只支持当前 ActionID、有限 semantic selector、局部
约束和外部工作量上限。完成 `plan -> concretize -> execute -> mechanical feedback -> revise`，不创建通用
DSL、operator catalog 或第二套 Runtime。

完成判据：Agent 能构造一个需要多步消息/时间/生命周期组合的场景，并根据 `no-match/ambiguous` 等机械反馈
修正，而不取得事实或 verdict 权限。

A4a 已完成短计划、有限 selector、逐步可信具体化以及 `no-match/ambiguous/budget-exhausted` 反馈。A4b 已
复用 durable provider journal，允许一次有界修正，并把最终多步 Trace 送入 qualified testing result。
A4c0 已把活动模型传输收敛为 OpenRouter 单一入口：provider 固定为 OpenRouter，模型通过显式 `model_id`
选择；历史 direct-DeepSeek 工件不改写，但新运行不再维护 DeepSeek 专用客户端，也不再提供默认模型。

当前请求使用 strict JSON Schema 和 `provider.require_parameters=true`；reasoning effort、是否返回 reasoning
文本及 output token 上限来自可编辑实验输入。主配置使用 `high`、`exclude=true` 和 4096 tokens，`none` 保留为
消融设置。结构化输出只减少格式错误，模型计划仍是不可信输入。

A4c 已增加显式 opt-in 的 Scenario Agent CLI、紧凑 summary 和终止调用恢复。首次真实 OpenRouter 尝试在
收到模型响应前形成 terminal transport failure；保留该工件后，第二个独立运行成功获得两次响应。第一次计划
在 `deliver -> crash` 后因第三步 `no-match` 停止，第二次依据机械反馈修正为
`deliver -> crash -> restart -> fire-temporal` 并完成 qualified testing。fresh Replay stable、Agreement/Trace
Oracle 无 violation，得到 33 个 PSS sample、23 个唯一 Core PSS 状态；但 RiskWitness 仍为 `not-reached`。
恢复保持两次 provider call 不变。这证明模型能消费反馈并闭合真实测试流程，不证明 Agent 搜索优于 baseline。

### A4r：Agent 契约与模型入口收敛

状态：已完成。

删除没有活动生产消费者的 A1 `SemanticEpisodeView`/`EpisodePlan`/`PlanningFeedback`/`EpisodeReport`
及其自循环测试，将 `TestHypothesis` 抽为唯一共享语义输入。A2 Explorer 和 A4 Scenario 必须分别以
`bounded-semantic-best-first-v1` 和 `bounded-scenario-plan-v1` 显式验证 backend，不再默认 DFS。活动模型路径只保留
OpenRouter 客户端，CLI 必须提供 `-agent-model`；历史 DeepSeek 工件仅作为已发生实验证据。

### A5：Hypothesis Role 与 JSON 输入

A5a 已完成：etcd/raft 活动 A2/A4 路径从 `plans/agent/*.json` 读取 ProtocolKnowledge 和
TestHypothesis，CLI 要求显式 `-semantic-input`。Authoring JSON 不带 schema/digest，加载后立即转成现有
可信类型，不引入第二套契约。

A5b 已完成：同一 JSON 还提供 etcd/raft 节点/Raft ID/ElectionTick/HeartbeatTick、Runtime、
FaultEnvelope、DFS/Explorer/Scenario 上限和 OpenRouter max output tokens。所有活动 A2/A4 factory 和 qualified
execution 使用该配置；与 root corpus/qualification 不一致时依靠现有 manifest/Replay 拒绝。

A5c 已完成：同一 JSON 提供不含派生 digest 的 workload authoring 输入；目标已有 `InputPayload` 将它转换为
现有 `WorkloadPlan`。source bundle、root corpus 校验和 A2/A4 执行使用同一份 workload，配置变化不能复用旧
corpus。JSON 是规范输入，YAML 若提供只作为转换前端。RiskWitness 证据解码、target projector 和 Oracle
仍保留在代码中。

### A6：配置时间内的测试 session

状态：A6a/A6b/A6c 已完成。

目标：复用现有 Campaign coordinator，把多个 semantic episode 串成：

```text
plan -> execute -> mechanical feedback -> revise -> next episode
```

停止条件只来自时间/工作量/episode 数等外部预算。输出 session summary，不让 Agent 根据隐藏 verdict 提前停止。

A6a 已将现有 Scenario episode 直接作为 Campaign attempt。`session_budget` 和 `session_wall_clock_ms` 限制
episode、primary/replay work、模型 calls/tokens 和 wall time。机械 `ScenarioAgentFeedback` 只在同一 episode
continuation 中传递；跨 episode 的 root 已重置，当前不传旧 feedback。Campaign checkpoint 负责提交和恢复，
没有新增 Session Runtime 或 Ledger。

A6b 已在 session attempt artifact 中保存 qualified bundle 的规范 Core PSS 状态键，并从 committed artifact
派生 completed/stopped/testing/replay-stable episode、PSS 样本与状态并集、Risk 最佳进展和 Oracle violation
总数；CampaignSummary 继续报告停止原因与 work/model/time。终端字段不会进入下一 episode prompt，且 A4c
历史 artifact 格式保持不变。

A6c 真实运行使用 `deepseek/deepseek-v4-flash`：2 个 episode 共 2 次逻辑模型调用、10,984 tokens。
第一次调用在第 3 次传输才成功，第二次一次成功；因此 OpenRouter 对无响应传输错误和
HTTP 408/429/5xx 最多重试 2 次，并分开记录 `model_calls` 与 `transport_attempts`。两轮都 Replay stable、
0 Oracle violation，最终 PSS 状态并集为 23；RiskWitness 仍为 `not-reached`。这是公开校准，不证明 Agent
优于 baseline。

完成判据：用户给定目标、知识、Adapter、workload 和时长后，系统自动运行并输出 finding/coverage/cost。

### A6d：可信执行边界修复

状态：已完成。

在复制 A6 session 到第二协议前，先修复审计确认的三处通用边界问题：

1. Replay 不再把保存的 invoke/partition Action 直接写入 Runtime 内部队列，而是重新经过正常 Offer 路径，
   并核对重建 Action 与保存 Action 一致；
2. `Trace.Validate` 验证记录步号、Action/Command 绑定、Evidence/entropy 绑定和自身首尾状态，
   `ExecutionBundle` 再结合 preparation 记录验证完整状态链；
3. Runtime native Action 先完成无副作用校验，再调用 Adapter actuation，最后提交 Runtime 状态，避免外部状态已变
   而内部校验才失败。

这些修复对应可构造的失败场景：重新封装的 partition Trace 可以引用 Manifest 中不存在的节点并绕过
`OfferPartition`；重新封装的断链 Trace 只要总摘要正确就能通过浅层 `Trace.Validate`。Git、版本号、类型和普通
对象摘要只能证明保存了什么，不能证明 Replay 重新走过合法入口或记录之间形成合法执行链。因此本阶段增强现有
验证函数和回归测试，不增加新 Ledger、schema、冻结 contract 或持久化 gate。

Conformance 输入绑定和 entropy tape 体积本阶段不改：前者当前由同一流程即时生成并消费，没有外部报告复用的
活动失败路径；后者只有渐进复杂度风险，尚无测量证据证明是当前瓶颈。

实现结果：Replay 已通过正常 Offer API 重建 invoke/partition；Runtime native Action 已改为
`prepare/validate -> Adapter apply -> commit`；`Trace.Validate` 与 `ExecutionBundle.ValidateTraceIntegrity` 分别承担
记录内部验证和 preparation-aware 状态链验证，TraceIntegrity monitor 复用同一规则。回归测试覆盖未知 partition
节点和重新封装的非连续 Trace，全仓测试通过。该结果证明已知绕过被拒绝，不证明所有 Trace 伪造或 Adapter 故障
都已穷尽。

### A6e：协议感知 Agent 输入

状态：代码接入和首次公开配对校准完成，尚无稳定效果结论。

只在 A4/A6 Scenario 主路径为当前 enabled Action 增加一个封闭、target-local 生成的共识语义提示：节点角色、
消息类别、epoch 关系和操作阶段；无法可靠判断时必须为 `unknown`。提示绑定当前可信 frontier，不暴露绝对 term、
原始消息载荷、未来事实、candidate/control 身份或 Oracle verdict。用 full/masked semantics 消融判断它是否真正
提高计划质量；A2 的 LLM queue permutation 不再扩展，只保留 deterministic semantic best-first 基线。

当前实现把提示作为 `ScenarioAgentView.action_semantics` 的独立只读部分，不修改公共 Action、
`FrontierActionRef`、DFS 或 A2。每条提示只包含 `actor_role`、`message_class`、`epoch_relation` 和
`operation_state` 四个封闭分类，并绑定已有 prefix、snapshot、ActionID 和 ActionDigest。etcd/raft projector 只读取
Adapter 已验证的 Evidence 与 ProducedItem metadata，不向模型输出原生角色/消息名、绝对 term 或 payload。
`full` 与 `masked` 使用完全相同的 Action 集和绑定；masked 只把四个语义值改为 `unknown`。

语义暴露模式进入 editable experiment config 和现有 session spec，是因为如果它不参与恢复身份，已完成的 full
session 可以在用户把配置改成 masked 后被错误复用。Git、代码版本和类型无法区分同一提交下的两份运行输入；
这里复用现有 spec/request identity，不创建新 hash、contract 或 gate。

现有 A6 session CLI 支持 `-scenario-semantic-exposure full|masked`，可在不复制 semantic input 文件的
情况下用同一 root、模型、prompt 和预算运行两个独立 Campaign 目录。它复用普通 session 工件和终端
summary，不建立额外的 pair runner 或实验账本。

首次真实配对使用相同 root、semantic input、`deepseek/deepseek-v4-flash`、Action 和上限。`full`
两个 episode 均一次生成合法的相同 4 步计划，共 2 calls、17,593 tokens；`masked` 的首个 episode
经历 `no-match` 并修正，第二个仅执行 1 步，共 3 calls、28,312 tokens。两组都有 2/2 stable Replay、
0 Oracle violation、23 个 session Core PSS 状态，Risk 都未达到第二里程碑。

这份结果支持“受限语义可能改善计划有效性和成本”，但不证明它提高 Risk/PSS 或已形成稳定 Agent 优势：
`full` 两次重复了同一计划，而且当前只有一组公开 pair。后续效果评估需要预注册的重复 trial；A6eR 已先
校准可达性并修正规划时域，下一阶段进入 A6f 最小 Oracle。

#### A6eR：Risk 可达性与规划时域修正

真实 Adapter 可达性校准已从同一 28-decision root 找到一条 stable Replay witness：中止旧 leader，只用
effect completion、消息投递和自然 temporal event 推动新 leader，再重启旧 leader。该 extension 用了 26 个
决策，在 leader change 前没有客户端返回，并达到三个 Risk milestone。26 不是穷尽证明的最短长度，
但它暴露了当前评测的结构问题：Agent 只能给出 4 步计划，计划合法完成后 episode 立即结束，新 episode
又从 root 开始，因此无法通过多个短计划累积长时域进展。

该修正最终收敛为“单步战略干预 + 可信自然推进”：活动 JSON 的 `scenario_max_steps=1`，模型只引用当前
ActionID；干预执行后，协议无关 closure 只选择 `complete-effect`、`deliver-message` 和
`fire-temporal-event`，并在 Risk 里程碑变化、目标客户端返回、自然推进静止或预算耗尽时停止，再重建
frontier/snapshot 和 target-local 语义供下一次模型调用。stopped 多步计划的 leading applied prefix 会保留，
完整 previous plan 和 failed step 返回给同 episode 修正；rejected step 不进入 aggregate execution。

etcd/raft 测试从同一 root 中止 n1 后，closure 用 24 个普通动作到达 leader-change milestone，下一次干预可
选择 restart，最终 Trace 仍能编译为 qualified execution。该 closure 也必须接给比较策略后才能开展公平效果
实验；在此之前不能把可达性改善归因于 LLM。

校准同时暴露的具体语义缺口已收紧：planning prefix 没有 client/operation history 时，etcd/raft projector
以 Adapter application-command 增长作为当前单 proposal workload 的保守 terminal 事实；最终 qualified Risk
仍使用完整 OperationHistory。这样缺少返回历史不再自动等价于“仍在运行”。该 fallback 不能推广到多并发
请求；届时必须使用可关联到 operation identity 的完成事实。

现有 semantic calibration spec 现在绑定 scenario prompt version、call、单计划 step 和累计 decision 上限。
否则相同 Campaign 目录可以在这些作者输入改变后误恢复旧结果，而 Git、版本号和 Go 类型无法区分同一提交内
的运行配置变化。实现复用已有 spec，没有新增平行 ledger、contract、hash 层或 gate。

本地 provider/真实 Adapter 集成已验证连续两轮规划从新 prefix 继续、下一 Campaign episode 从统一 root 重置、
恢复不重复 provider，以及累计计划进入原 qualified execution。首次真实 A6eR 单 episode 随后完成 8 次逻辑
调用、44,443 tokens；5 个合法短计划把正式 prefix 从 28 推到 33，最终 qualified testing 有 34 个 PSS samples、
24 个唯一状态、stable Replay 和 0 Oracle violation。Risk 仍只满足初始 workload milestone，不能据此声称模型
已能生成 26-decision witness，也不能把相对旧 A6e 多出的 1 个状态解释为 Agent 优势。

真实运行有 3/8 个计划因 future ActionID `no-match` 而回滚。v3 prompt 现在明确规定 exact ID 只能来自当前
frontier，第一步后必须省略 `action_id`并使用冻结输入列明的 semantic selector fields。冲突的
`semantic-queue-only` 知识文本也改为通用 trusted-view 约束。这是 prompt 修复，不改变 Action 控制能力或 verdict。

旧 6-call journal 上限还造成记账负证据：sidecar 保存 6 calls/42,664 tokens，当时的 Campaign failure marker
却显示 0 work。上限已与 Scenario 全局上限对齐。session 现在从 durable call audit 汇总已发生 ModelWork，
Coordinator 仅在 WorkLedger 结构合法时把它写入现有 failure marker，Campaign summary 会把该 work 计入 totals。
本地回归验证一次成功调用后的 3 次传输失败被记为 2 calls，恢复不重复 provider。这里复用 failure v2
已有 Work 字段与现有 call audit digest，没有新增 ledger、hash、contract 或 gate。

### A6f：最小共识 Oracle

第一个 target-local `etcdraft-log-progress` 已接入 etcd/raft qualified execution。它只顺序消费
Adapter Evidence，对同一 `(node, incarnation)` 检查 `Applied <= Commit`、commit frontier 不回退、
applied frontier 不回退；不读取 Agent 输出、PSS 覆盖或 Risk 进展。真实 Adapter 产生的 Bundle
通过该监视器，受控 Evidence 变异分别检出 frontier 回退和 `Applied > Commit`。

监视器不跨 incarnation 猜测持久化事实：节点重启后的内存 frontier 只在新区间内比较。如果要检查
跨重启不变式，必须先补充存储证据。当前仅 etcd/raft 有这一真实消费者，因此不下沉新的
通用 Oracle 接口。

客户端返回绑定的能力缺口已用最小目标证据补齐。etcd/raft Adapter 复用原有 `ready-advanced`
typed observation，只在当次 yield 真正应用命令时携带 `request_id/index/term/origin/value`；没有
将全量命令历史复制进每个 Evidence 快照。`etcdraft-client-application-binding` monitor 从 Trace 取实际
Invoke，通过已有 item transition 将 observation 与 ClientHistory 返回绑到同一 step，并要求请求、位置、
任期、origin 和 value 形成唯一精确匹配。真实 Bundle 通过，返回值与 command witness 变异均被拒绝。

反向 proposal/workload validity 也已加入同一 monitor：每个 applied user command 必须晚于对应的实际
Invoke，并匹配 request/origin/value；同一 request 在所有副本上只能对应同一 `(index, term)`。无来源命令
和同 request 多日志位置反例均被拒绝。该 monitor 不读 Agent、PSS 或 Risk，也不扩展通用
Runtime Action。A6f 至此形成 log progress、客户端返回绑定和 applied-command 来源的最小闭环。
在 workload 仍只有单次写入时不建立通用 linearizability DSL；需要读和多操作历史后再决定。

### 已完成：A6g 单步 Agent 干预真实校准

OpenRouter Chat Completions 请求使用端点能力表声明的 `max_tokens`；`require_parameters=true` 继续要求
reasoning 和 strict structured output 都由实际路由支持。真实 `deepseek/deepseek-v4-flash` 用两次调用依次选择
`crash n1`、`restart n1`，可信 closure 在两者之间执行 24 个普通 effect/message/timer 动作。最终 Trace 达到
全部 Risk milestones，得到 55 个 PSS samples、38 个唯一 Core PSS 状态、stable Replay 和 0 Oracle violation。

完整 closure 进入执行审计，但下一次 prompt 只保留上一战略干预、closure stop reason 和当前可信
Frontier/Risk。相同 Trace 与 Bundle 的复验把总 token 从 33,215 降到 15,423。该公开校准证明新的职责分工能够
闭环，并不证明 Agent 优于 baseline，也不是 hidden candidate/control 结果。

### A7：第二协议与接入减负

先以已有 OmniPaxos strict Adapter 盘点 A6 中的 etcd/raft 组合耦合，再复用同一 episode/session 数据流。
只有当 Binding、Evidence Extractor、Risk/Oracle 组合或 authoring 输入确实被两个目标消费时，才下沉最小公共接口。
不预先重构 Adapter v3；记录目标专用新增代码量、qualification 差异和实际可运行义务。

A7a 已完成最短非 Raft episode：真实 OmniPaxos worker 在 23 个 root 决策后接受 workload，Scenario 从可信
frontier 丢弃一条 `sequence-paxos` 消息，closure 再执行 7 个动作后形成决定，fresh worker Replay 一致。
公共 Action、Runtime、Scenario 和 Replay 未修改；新增内容只包含 OmniPaxos 只读 Evidence、目标 Risk、
语义提示和集成测试。该证明尚不等于完整 session，也没有进行真实模型调用。

下一步 A7b 先补可编辑 authoring 输入和 qualification-bound testing result，再决定是否复用 session 编排。
不把 etcd/raft CLI/Oracle/PSS 映射整份复制给 OmniPaxos；只有出现第二个相同消费者的逻辑才进入公共包。

A7b 已完成：OmniPaxos 知识、假设、单写 workload 和消息丢弃预算来自独立可编辑 JSON；目标已验证的六项能力
绑定 execution admission，Scenario exact policy 经唯一 Bundle executor 重跑。实际结果为 31 个 Core PSS
samples、28 个唯一状态、stable Replay、TraceIntegrity/Agreement 零异常。两个目标共有的 testing result 外壳
已合并，Risk 与 monitor 仍留在目标组合。

下一步 A7c 复用现有 provider 编排形成可调用的非 Raft episode。该阶段不引入 A2 搜索、冻结 spec、
新账本或新的覆盖率定义。

A7c 已完成其中的 provider 与可调用单 episode run：共享 episode core 同时服务 etcd/raft 和 OmniPaxos，
OpenRouter journal 的成功响应进入同一 closure 与 qualified Bundle；compact summary 持久化后可在不访问 provider、
不读取 key 的情况下恢复。正式策略名为 `omnipaxos-openrouter-scenario-a7c`。本地 provider 结果为 1 次调用、
31/28 PSS、Risk reached、Replay stable、0 Oracle violation。

该结果尚不是多 episode session。A7d 只在确认现有 Campaign coordinator 可直接消费共享 episode artifact 后补
session 编排；否则保留单 episode 入口并记录差异，不复制 etcd/raft Campaign 实现。

A7d 已完成：`omnipaxos-agent-session-v1` 由现有 Campaign coordinator 在 JSON 声明的共享预算内运行两个
独立 episode。每轮均进入 qualification-bound Bundle、fresh Replay、Oracle、Risk 和 PSS；终端结果计算 PSS
状态并集而不是简单相加。恢复已提交 Campaign 时不再次访问 provider 或 key。

etcd/raft 原目标内会话代码已提取为两个真实协议共同消费的 attempt journal、artifact validation、work/model
计费和结果聚合；OmniPaxos wrapper 只负责准备目标输入、调用目标 executor。会话身份复用现有规范化摘要绑定知识、
假设、workload、运行配置、execution admission 与 provider transport，解决“输入改变后错误复用已提交 artifact”
这一具体恢复失败场景；没有增加新的冻结文件、版本或 gate。

本地结果为 2 个 completed/testing/replay-stable episode、2 次模型调用、Risk reached、0 Oracle violation，并在
attempt limit 停止。相同策略产生的状态并集不应被解释成 Agent 已改善覆盖。

完成判据已经满足：没有修改公共 Action/Runtime/episode 数据流即完成非 Raft 多 episode session。

### A7H：A8 前完善

不增加新协议、Agent 类型或通用 DSL。先完成四个直接影响正式实验可信度的闭环：

1. 活动测试与 race shard 清单恢复绿色，当前文档入口与代码一致；
2. Scenario attempt 持久化完整 ExecutionBundle，恢复时重新验证并派生 PSS/Risk/Oracle/summary；
3. 修复 primary/replay 成本分类、超预算实际 work 和外部 worker 生命周期；
4. 显式绑定 natural-progress policy、OmniPaxos root/method identity、termination reason 和 provider routing policy。

只有对应具体恢复、成本或资源失败场景时才增加身份字段；不额外建立文件 digest、第二套 ledger、发布 gate 或
Session Runtime。OmniPaxos 在补 target-local Oracle 和真实模型对照前只作为跨协议集成证据。

A7H0 已恢复全仓测试与 race shard 清单。A7H1 已先修正超预算 work 和 child verification 分类，并建立统一
Runtime 所有权：`New` 接管 Adapter，失败自动释放，成功后由 `Runtime.Close` 幂等释放；高扇出的 frontier
重建、child verification、Scenario 和 qualified Replay 不再把外部 worker 累积到 episode 结束。conformance
manifest、raw case、Runtime 和 Replay 也已使用同一所有权，HashiCorp Raft 与 OmniPaxos qualification 删除了
目标专用批量回收 wrapper。A7H2a 已把完整 `ExecutionBundle` 直接写入现有 Campaign attempt artifact；
恢复会执行 `Bundle.Validate()` 并从 Bundle 重新派生 PSS 状态键，不新增 sidecar、文件 hash 或平行 ledger。
A7H2b1 已进一步保存完整 Risk/Oracle，并由两目标现有 projector/monitor 从持久化 Bundle 重算 Risk、Oracle、
outcome 和 compact summary；Bundle、Risk 或 Oracle 篡改都会被拒绝。A7H2b2 已让终止 Campaign 从既有
自验证 config/checkpoint/artifact 只读恢复，同时核对固定
campaign/target ID 与 Bundle manifest；不存在的 etcd/raft 输入路径和 OmniPaxos worker 路径不再阻止结果恢复。
运行中的 Campaign 仍使用独立构造的 expected config 继续执行。A7H2 至此完成。

A7H3 没有新增第二套 Spec/hash/gate，而是扩展现有 `ExperimentSpecDigest` 的规范输入：两目标共同绑定 Scenario
prompt 版本、按当前 `scenario_max_steps` 生成的实际 strict structured-output schema、natural-progress 动作优先级、
Risk projector ID 和 Semantic projector ID；OmniPaxos 还绑定 root digest、Risk spec 与 qualification bundle。
Campaign config 已单独绑定 session budget 和 wall-clock ceiling，实际 stop reason 继续作为运行结果持久化。
改变上述执行方法输入会得到不同实验身份，两协议的真实双 episode 创建与终端恢复回归均已通过。projector ID
仍是与实现共同维护的显式版本；A8 归档必须同时记录 Git 提交，当前不增加代码 hash。

A8 前减负不改变上述可信边界：删除已被 Session 的 `MaxAttempts=1` 覆盖的两套单 episode CLI，正式策略收敛为
`etcdraft-agent-session-v1` 与 `omnipaxos-agent-session-v1`；公共 semantic authoring 校验与 Ready effect 构造去重，
`control-experiment.run()` 只保留 flag 解析和四路显式分派。阶段编号测试改为行为名称，阶段流水账和无活动引用的
feasibility/migration 产物从 HEAD 删除，历史仍由 Git 保存。Stateless baseline、Semantic Explorer、Scenario
Session、qualified executor、Replay、Oracle、Defect Benchmark 和正式校准证据均保留。

### A8：效果评测

先比较同执行底座的 deterministic Scenario 与单 Scenario Agent，再准备非公开 candidate/control。两种方法必须
复用同一 root、Frontier、natural-progress policy、qualified executor、Replay、Oracle 和执行预算；模型调用与
token 作为 Agent 额外成本单列。预注册重复次数、预算、exposure 和主要指标；PSS/义务只解释结果，不代替 finding。

A8a 已把 Scenario episode core 与 OpenRouter journal 解耦，并增加 target-local 的 etcd/raft 确定性 planner。
它机械选择 `crash current coordinator -> trusted natural progress -> restart old coordinator`，最终与 Agent 进入
同一 qualified testing。这样解决旧 stateless DFS 丢弃完整 Bundle且执行单位不同、无法把结果差异归因给 Agent
的具体问题；Git、类型和普通测试只能验证两条旧路径各自有效，不能生成同形的 Replay/Oracle 证据。本节没有新增
Runtime、Action、schema、hash、gate 或评分公式。下一步是把两种 planner 接入同预算 trial runner 和紧凑结果表。

A8b 已完成该 runner：`etcdraft-a8-paired-scenario-v1` 在两个现有 Campaign 目录中分别执行一个 deterministic 与
Agent episode，机械要求相同 primary/replay 上限，并从原 artifact 汇总 Trace、PSS、Risk、Oracle 和实际成本。
Agent calls/tokens 单列，不假装 deterministic 也消耗模型额度。默认 etcd/raft authoring input 每个 trial 只运行一个
episode；重复实验使用独立 trial，而不是把同一 root 上两个完全相同的 Trace 计为覆盖增长。fixture 配对结果中两 arm
Trace 和测试证据相同，Agent 额外调用模型两次，证明结果格式可以诚实表达负证据。下一步先做真实 OpenRouter public
preflight，再把同一 trial 单位接入 private candidate/control evaluator。

### A9：多 Agent 消融

在单 Agent 闭环稳定后比较单 Agent、双角色和三角色，并保持模型、总 token、调用次数和 Runtime work 一致。
只有结果支持时才保留更多独立 Agent。

## 9. 开发与验证节奏

每个小批次：

- 只运行受影响包；
- 先编译，再跑最小行为测试；
- 修改及时落地，避免长时间堆积。

每个完整阶段：

- `make test`；
- `go vet ./...`；
- `git diff --check`；
- 汇报生产代码、测试、Markdown、JSON 的净变化。

完整 race 只在明确发布/里程碑检查中运行，不在每个小阶段重复。普通测试禁止读取 key 或调用模型。

新增抽象原则：

- 至少两个现实消费者才进入通用层；
- target-local 逻辑不为“未来可能复用”提前抽象；
- 新文件必须替代重复责任或完成用户可见流程；
- 阶段进展不能只用新增文件数或 schema 数衡量。

## 10. 可行性自审查

高可行：

- embedded 共识库的消息/时间/生命周期控制；
- 确定性 Trace 与 fresh Replay；
- 有界安全 Oracle；
- Agent 基于机械语义反馈修正搜索；
- 配置预算内自动 session。

中等可行：

- Process/Proxy 目标达到与 embedded 相同控制强度；
- Core PSS 跨更多 leader-based CFT 复用；
- 自动生成 Adapter 草案并由 qualification 验收；
- multi-Agent 在同预算下产生稳定增益。

不能承诺：

- 任意新协议零配置接入；
- 纯黑盒获得完整消息、时间和持久化所有权；
- 自动推断正确 quorum/lock/commit 语义；
- 有限测试证明异步协议全局活性；
- 用一个数字证明“测试全面”；
- Agent 自动确认真实协议缺陷。

## 11. 方向检查

如果后续工作不能用下面这句话描述，就应停止扩展并重新审查：

> 输入共识实现、薄 Adapter、协议知识、workload 和预算；Agent 形成并修正语义测试计划，Control Runtime
> 在真实实现上执行，系统输出可重放 finding、PSS/义务/Risk 统计和完整成本，由独立 Oracle/evaluator
> 而不是 Agent 决定测试结果。
