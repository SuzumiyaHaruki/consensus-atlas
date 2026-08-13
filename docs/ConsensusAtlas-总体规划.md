# ConsensusAtlas 总体规划

> 状态：Draft v2.8（A6 真实多 episode session 已完成，下一阶段 A7a 第二协议接入差距）
> 日期：2026-08-13
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
- 一次真实 DeepSeek/OpenRouter 两 episode session，包含有界传输重试、机械跨 episode 反馈、
  qualified testing、PSS/Risk/Replay/Oracle 聚合和无 provider 恢复。

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
episode、primary/replay work、模型 calls/tokens 和 wall time；后一个 episode 只接收前一个 episode 的最后一条
机械 `ScenarioAgentFeedback`。Campaign checkpoint 负责提交和恢复，没有新增 Session Runtime、Ledger、schema
或 digest。

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

### A7：第二协议与接入减负

先以已有 OmniPaxos strict Adapter 盘点 A6 中的 etcd/raft 组合耦合，再复用同一 episode/session 数据流。
只有当 Binding、Evidence Extractor、Risk/Oracle 组合或 authoring 输入确实被两个目标消费时，才下沉最小公共接口。
不预先重构 Adapter v3；记录目标专用新增代码量、qualification 差异和实际可运行义务。

完成判据：不修改公共 Action/Runtime/episode 数据流即可完成一次非 Raft session。

### A8：效果评测

准备非公开 candidate/control，比较 deterministic baseline、单 Explorer 和多 Agent。预注册重复次数、预算、
exposure 和主要指标；PSS/义务只解释结果，不代替 finding。

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
