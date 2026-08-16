# ConsensusAtlas 架构

日期：2026-08-16

本文件描述当前代码和已经确定的演进边界，而不是历史阶段。系统只保留一条权威主线：

> **开放规划，受控执行，机械反馈，独立判定。**

Agent 负责形成协议感知的测试意图；可信内核只把可验证的意图落实为真实 enabled Action；
Replay/Oracle/evaluator 独立判定结果。规划可以是启发式的，执行事实和最终判定不能由 Agent 自报。

当前实现范围是 leader-based CFT 共识库的受控协议/host-order 执行。受限 BFT 和生产进程/存储/网络故障面
属于后续扩展，不是本文件的现行能力声明。

## 1. 端到端数据流

```text
SUT + Target Pack + Protocol Pack + Experiment Config
                         |
                         v
                hypothesis / Explorer
                         |
                bounded planning intent
                         |
                         v
               trusted concretization
                         |
                         v
                  Control Runtime
                 /       |       \
             Trace    Evidence    client result
                \        |        /
                 PSS / Risk projection
                         |
              mechanical feedback + fresh Replay
                         |
                         v
                  Oracle / evaluator
```

### 输入

- SUT：被测共识库或进程；
- Target Pack：薄 Adapter/Binding、能力声明和 Evidence Extractor；
- Protocol Pack：Consensus Primer、Property Catalog、Historical Issue Pattern、允许的关系与可观察里程碑；
- Experiment Config：拓扑、workload、fault envelope、时间/随机源策略和资源预算。

Target Pack 中只有目标接口绑定与证据提取必须是代码。可审查、可变的拓扑、预算、工作负载和协议知识应逐步
迁移到 JSON/YAML。A5 已将 legacy A2/A4 路径的 ProtocolKnowledge、TestHypothesis、workload、Adapter
topology/ticks、Runtime、FaultEnvelope 和预算外移到 `plans/agent/*.json`。A9e1 的 `agentic-episode-v1`
不再接收预置 Risk/TestHypothesis，而由 Agent 从 Primer、Property 和 Issue Pattern 提出机制与 predicates；
两条路径加载后都转成原有可信类型，不引入第二套语义或 workload 契约。Agentic loader 只校验该入口实际消费的
Scenario/Runtime/model 预算，不再要求 legacy Semantic Explorer 的 depth、work-item 或 explorer budget。

A9e4c6 在同一个 ProtocolKnowledgePack 中增加可选 Target Dossier、Property evidence level 和 Issue Pattern
applicability/boundary。Dossier 只补充目标范围、实现组件、公共契约、控制语义、当前实验和盲区；真实能力仍来自
Manifest/qualification，真实事实仍来自 Trace/Observation，结论仍来自独立 Oracle。A9e4c7 新增协议无关的
`AgentTargetSurface`，从 Manifest、WorkloadPlan、RuntimeConfig 和 FaultEnvelope 机械派生活动 topology、输入、
时间/clone 参数及 temporal/crash/effect 能力，并传入 Risk 与 Scenario 两个 Agent。Dossier 不再重复 workload 和
fault allowance，只保留不能从公共输入自动得到的实现配置、契约和盲区。

A9e4c8 在 `internal/controlexperiment` 增加受限只读 Knowledge Discovery。它从 Dossier 的 `evidence_refs`
机械派生来源目录，且只读取调用方显式给定仓库根内的普通 UTF-8 文件；精确 reference allowlist、路径边界、
符号链接边界、单文件大小和单次行数共同限制返回材料。locator 用于自动寻找相关片段。读取结果属于不可信
规划上下文，不进入 Trace、Replay、PSS 或 Oracle。

A9e4c9 将读取根从单仓库扩展为显式虚拟前缀 mounts。空前缀表示主仓库，较长前缀可指向调用方提供的 SUT
或依赖源码目录；匹配采用最长前缀，剩余相对路径继续经过同一目录与符号链接边界检查。CLI 使用可重复的
`-knowledge-source-mount` 参数构造 mounts，并随 Agentic composition 保留。绝对本机目录不进入 Protocol Pack、
prompt 或 verdict，也不从 Go module cache 隐式推断，因此同一知识 reference 可在不同机器上显式重新绑定。

A9e4c10 在既有 Risk Agent 请求/修复循环中增加两阶段响应：配置 mount 时，首次调用只选择最多
2 个精确 source reference，读取完成后下一次调用才允许 portfolio。每次读取最多 80 行，还受通用
24 KiB 片段上限限制。精确重复请求、超限和不可读来源都以稳定机械结果返回；读取本身也消耗原有
Risk model-call/token 预算。request/result 记录在原 `RiskAgentAttempt` 和 provider journal 中，不增加新 Agent、
journal 或证据账本。当 composition 没有 source mount 时，Risk provider schema 仍只允许 portfolio。

Authoring JSON 只提供人工输入字段和外部运行上限。它不能提供 schema version、digest、执行 backend、
Oracle 或 verdict；Agent 也不能修改这些预算。加载器严格拒绝未知字段和作者伪造的派生身份。
拓扑、时间参数或 workload 与显式 root corpus 不一致时，现有 Adapter manifest、bundle 和 Replay 校验会拒绝组合。
paired 评测的缺省路径不跨 SUT 复用 corpus：它先在当前 SUT 上生成 source，再按首次 Invoke、其后 Crash 和同节点
Restart 的 target-local Action 规则选择 fresh root。确定性与 Agent 两臂消费同一个 prepared inputs；显式 corpus
入口仅用于复现已有公开实验，继续执行精确身份校验。

### 输出

- Agent 选择及其机械接受/拒绝反馈；
- 可重放 Trace、ExecutionBundle 与执行结果；
- PSS/Risk/义务统计；
- Oracle finding 或 evaluator 的 candidate/control 结果；
- primary/replay/model 成本。

## 2. 信任边界

```text
untrusted / bounded                         trusted

Agent hypothesis/explanation       Adapter qualification
semantic selector / ordering       enabled Action computation
short planning intent        ---> trusted selector concretization
                                   Control Runtime execution
                                   Evidence projection
                                   PSS/Risk truth
                                   fresh Replay
                                   Oracle/evaluator verdict
```

Agent 可以：

- 形成 property-referenced Risk candidate 和不可信的机制解释；
- 选择语义目标；
- 对当前可信候选排序；
- 以完整但有界的 `ScenarioPlan` 表达若干语义选择器与期望观察点；
- 根据不可达、near-miss、PSS/Risk 变化修正 hypothesis 或场景；
- 在 Investigation 总预算内改换分支、重建新鲜 root，并请求对照或消融实验。

Agent 不可以：

- 创造消息、节点、时间或 enabled Action；
- 修改 fault/budget、Adapter qualification 或当前 Oracle；
- 声称某个 PSS/Risk/义务已经满足；
- 决定 candidate/control 身份或正式 verdict。

因此 Agent 可以犯规划错误，但不能把错误解释写成执行事实。

当前单个 `ScenarioPlan` 表达完整但有界的测试意图，最多 8 个战略步骤。第一步可引用当前 ActionID；
未来步骤使用 ActionKind、节点、消息端点、时间种类或 effect 等有限公共字段，并可选引用已有
Risk milestone 作为 `after_milestone`。可信层在当下 admissible frontier 中解析 selector：唯一匹配才执行，
零匹配、歧义、未知/不可达 milestone 和预算耗尽都返回机械 reason。计划不能携带新的观察表达式、
fault budget、assertion 或 verdict，因此这仍是现有 Risk contract 的有限引用，不是新的 stop DSL。

单个 Plan 有界不等于整个 Agent 调查只能调用固定三次模型。Adaptive Investigation 重用现有
episode/provider journal/Bundle/Memory，允许 Agent 在总 wall time、model work 和 Runtime work 内多轮修正。
当前最多三次 Scenario call 是公开小型 calibration 的单轮配置，不是方法上的永久权限边界。

Agent 在概念上分为三种认知角色：Hypothesis、Explorer、Analysis。它们可以先由一次模型调用或一个进程承担；
只有同预算消融证明角色分离有收益时，才物理拆成多个 Agent 服务。

A6e 的协议语义不进入 `FrontierActionRef`：Scenario view 另带一个与当前 prefix、snapshot 和每个 Action 身份绑定的
`action_semantics`。target-local projector 只能输出封闭分类，未知内容必须为 `unknown`；原始协议状态、绝对 epoch、
消息 payload、未来事实和 verdict 不进入该视图。`full`/`masked` 共享完全相同的 Action 集，masked 仅隐藏语义值，
因此它是模型信息消融，不是控制能力消融。
运行时可在 A6 session 入口覆盖暴露模式；覆盖值重建现有 session spec，因此不同模式不会误恢复同一
Campaign，也不需要另一套配对运行协议。

A9e4a 已取消完整计划内 milestone 等待的固定 24-action planning checkpoint。对有 `after_milestone` 的后续步骤，可信执行器只从
`complete-effect -> deliver-message -> fire-temporal-event` 优先级中选择一个动作，然后重建 Runtime、
fresh Replay 子轨迹并重算 Risk，直到前置 milestone 满足。每个自动动作保存于
`automatic_progress`，与 Agent 选择的战略步骤一起占用同一 decision/work 预算，并在 exact policy 编译时逐条核对。
自然推进仅在 client terminal、quiescence 或 episode 总决策预算耗尽时停止；Agent 不能用碎片 milestone
购买更多调用。A9e4b 已将 etcd/raft 和 OmniPaxos 的活动 Agentic JSON 迁移到最多 4 步的 complete mode。
计划成功后至机械终点就结束测试；最多 3 次 Scenario calls 中的后两次只用于机械拒绝后修复。
旧 A6/A8 单步 session 仍保留 24 步兼容窗口，避免修改历史实验语义；它不再是 A9 Agentic 主线。

attempt 提交前的 provider failure 现在由 session 从 durable call audit 汇总已发生 ModelWork；Coordinator 只在
WorkLedger 结构合法时将它写入已有 Campaign failure marker，终端 summary 将 failure work 与已提交 totals
相加。无 durable audit 支持的其他执行成本仍保持为未知，不由系统猜测。

## 3. 核心模块

| 模块 | 责任 | 不应承担 |
|---|---|---|
| `internal/control` | Action、ProducedItem、Adapter 接口 | 协议语义和搜索策略 |
| `internal/controlruntime` | 消息/时间/生命周期/effect 的确定执行 | Agent 决策和 Oracle |
| `internal/conformance` | 机械验证 Adapter 声明 | 推断协议正确性 |
| `internal/controlexperiment` | qualified execution、搜索、Agent 计划、Campaign、成本 | etcd/raft 字段解析 |
| `internal/psscore` | 固定 Core PSS 投影 | 正式 defect verdict |
| `internal/semantic` | 协议族 RiskWitness 结构 | 目标实现 Evidence 解码 |
| `internal/oracle` | 独立性质检查 | 搜索指导 |
| `internal/defectbench` | candidate/control 评价 | Agent 在线反馈 |
| `adapters/*` | 目标专用接口映射与 Evidence | 通用搜索和评分 |
| `cmd/control-experiment` | etcd/raft 组合、CLI、模型传输 | 新的 Runtime |

协议耦合只能位于 Adapter、target-local projector、workload router 和组合入口。通用 Runtime、搜索、PSS
容器和 evaluator 不导入 etcd/raft/HashiCorp/OmniPaxos 类型。

## 4. Control Runtime 语义

Runtime 拥有所有待调度项：

- message：可投递、丢弃、复制或因分区暂时不可达；
- temporal item：只有最早到期的一组可以触发；选择超时会把逻辑时间推进到其 deadline；
- host/application effect：显式完成并记录结果；
- lifecycle：crash/restart 改变 incarnation，并遵守持久化模型；
- external invoke：只通过目标的 workload router 进入系统。

Adapter 在一次动作后返回新的 ProducedItem 与 Evidence；它不自行决定全局投递顺序。Runtime 对 ActionID、
item 状态和节点生命周期做机械校验，并记录足以 fresh Replay 的 Trace。

native Action 遵守 `无副作用校验 -> Adapter actuation -> Runtime commit`：Adapter 只有在 Runtime 已确认当前动作
可提交后才观察到动作。Replay 中由 workload 或实验准备产生的 invoke/partition 不能直接注入 Runtime 内部
队列，必须重新经过与首轮执行相同的 Offer API，并核对重建 Action。

验证分为两层：`Trace.Validate` 检查单条记录及 Trace 自身首尾绑定；`ExecutionBundle` 再利用 preparation 记录
检查 Offer 导致的状态变化和决策记录组成一条完整状态链。Oracle 的 TraceIntegrity 复用这一验证结果，不维护
另一套较宽松的接受规则。

Oracle 分工保持明确：TraceIntegrity 检查试验结构，Agreement 检查通用决策安全性，
target-local monitor 只在目标 Evidence 能表达额外性质时加入。当前 `etcdraft-log-progress` 在同一
`(node, incarnation)` 内检查 commit/applied frontier 单调性和 `Applied <= Commit`。它不读取
Agent、PSS 或 Risk，也不猜测跨重启的持久化事实。由于尚无第二个协议消费相同语义，
它保留在 etcd/raft composition，不扩张公共 Runtime 或 Oracle 契约。

客户端返回与应用命令是一条独立证据关系。OperationHistory 或 Trace 能定位实际 Invoke，
etcd/raft ClientResult 能解析 `request_id/index/term/value`，但累计 ApplicationDigest 不能证明单条
request 的成员关系。Adapter 因此复用已有 `ready-advanced` typed observation，只附加当次 yield 的
applied-command witness。monitor 通过已有 item transition 将它与 ClientHistory 绑到同一 return step，
同时反向检查每个 witness 都来自更早的实际 Invoke，且同 request 的日志位置唯一。它不将命令列表
加入每个通用 Evidence 快照，也不新增 Runtime item 或 Action。

## 5. 新协议接入

最小 Adapter 只需要实现目标真正具备的能力，不要求伪造统一强度：

1. 创建/恢复节点；
2. 注入 Runtime 已选择的消息、时间或输入；
3. 把目标新产生的消息/effect/结果交还 Runtime；
4. 导出稳定 Evidence；
5. 声明可机械验证的能力。

接入结果可以是 partial。统一 Action 表示统一调用语义，不表示所有目标都能提供消息所有权、自然时间、
持久化切点等同等控制。缺失能力必须显式记录，不能通过 Adapter 内部猜测或 no-op 伪装。

当前证据：

- etcd/raft：完整 strict 主路径；
- OmniPaxos：非 Raft 的同 Runtime 严格路径，并已复用同一 Scenario/closure/Replay 完成首个真实 episode；
- HashiCorp Raft：部分能力，用于暴露黑盒接口的真实上限；
- raft-rs：未取得资格的探索路径已从 HEAD 删除，不再作为当前适配证据。

第二协议的 Scenario 接入不要求通用层理解 Paxos。Adapter 只导出稳定的目标 Evidence，组合层将它投影为
Risk milestones 和有限 Action hints；Scenario 引擎仍只消费统一 Frontier、Action 和进度接口。目标间确实重复的
“取 Trace 最新 Evidence”已下沉为共享助手，其余 Risk/消息分类继续留在各自目标边界。

OmniPaxos 的 authoring JSON 只保存人工可编辑知识、假设、workload 和本目标使用的预算；PayloadEnvelope、
ProtocolKnowledge 和 TestHypothesis 仍由代码构造。Scenario 完成后，组合层只申请资格报告中已验证的六项能力，
再用公共 exact-policy/Bundle executor 重跑。PSS mapper、Decision projector 和 Workload router 均复用 Adapter
已有实现，Oracle 只运行通用 TraceIntegrity 与 Agreement。

OpenRouter journal、Scenario episode/session core 和 compact summary 已经有两个真实协议消费者。共享 core
不导入目标类型；target wrapper 提供 Root、Risk/Semantic projector、Adapter factory 和 qualified executor。
OmniPaxos 已进入现有多 episode Campaign。Runtime 现在接管 Adapter：初始化和 Replay 失败会自动释放，
frontier 重建、Scenario 与 qualified execution 在每次临时使用后关闭 Runtime，不再把外部 worker 累积到
episode 结束。conformance 与两个进程型 qualification 也已使用相同所有权，不再保留目标专用批量回收。
completed attempt 现在把完整 ExecutionBundle 放入既有 Campaign artifact；恢复先验证 Bundle，并从其 Core PSS
样本派生 session 状态并集，不再信任平行键列表。完整 Risk/Oracle 也保存在同一 artifact，并由目标现有
projector/monitor 从 Bundle 重算后核对 summary。终止 Campaign 可在不准备 SUT、不读取模型 key 的情况下从
既有 config/checkpoint/artifact 恢复；运行中的 Campaign 仍要求独立构造 expected config。现有实验摘要还绑定
Scenario prompt、实际 structured-output schema、natural-progress 优先级及 Risk/Semantic projector ID；
OmniPaxos 额外绑定 root、Risk spec 与 qualification bundle。终止策略由 Campaign config 的预算和 wall clock
表达，实际 stop reason 是执行结果而不是预先声明的方法字段。

## 6. 规划确定性与执行确定性

两者必须分开描述：

- 执行确定性：同一已具体化 Action 序列、受控随机带和初始状态应得到可重放 Trace；这是可信要求；
- 规划可重复性：同一模型可能提出不同假设或排序；这是搜索方法的随机性，应通过记录模型输入输出、固定预算、
  多次 trial 和同预算比较管理，而不是假装 Agent 本身确定。

因此，系统不要求 Agent 每次给出相同计划，但要求每个被接受的计划都能被机械解释、执行和复核。
探索阶段可以是在线自适应的：Agent 在看到第 `t` 步机械反馈后再选择后续方向。Runtime 记录实际具体化的
Action 序列和所有执行结果；候选晋升后从相同 root 启动 fresh SUT，不调用 LLM 重放该序列。因而不确定的
规划与确定的复现并不冲突。

## 7. Qualified testing 组合边界

A3 不再新增抽象层，只闭合现有两条已验证路径：

```text
Semantic Explorer 首个可信选择
        -> existing exact-prefix compiler
        -> existing qualified executor
        -> ExecutionBundle / Core PSS / Risk
        -> existing fresh Replay / TraceIntegrity / Agreement
        -> one target-local testing result
```

统一结果只是已有事实的组合视图，不成为新账本，不定义新的摘要协议，也不替代 ExecutionBundle、Replay 或
Oracle。A3 完成后再评估短 `ScenarioPlan`，避免在单 episode 尚未贯通前继续膨胀中间对象。

## 8. Agent 执行单元

### 活动契约

当前只有一个共享的协议语义输入 `TestHypothesis`。它不选择执行算法；每个消费者必须用自己的实际 backend
显式验证它是否被 ProtocolKnowledge 允许：

- A2 `SemanticExplorerProposal` 只排列当前可信 semantic queue，backend 为 `bounded-semantic-best-first-v1`；
- A4 `ScenarioPlan` 只表达有限短场景 selector，backend 为 `bounded-scenario-plan-v1`。

已删除的 A1 `SemanticEpisodeView`、`EpisodePlan`、`PlanningFeedback` 和 `EpisodeReport` 没有兼容层，也不再是
现行数据流的一部分。A2 与 A4 的 proposal/feedback 不合并成一个宽泛 DSL：两者共享 hypothesis 和可信执行
边界，但分别解决“当前候选排序”和“跨数步场景构造”。

Agent 的原子执行单位不是“每一步自由选一个 Action”，而是有界 semantic episode；一个长时
Investigation 可由多个 fresh-root episode 组成：

1. 可信层给出当前 hypothesis、prefix、语义候选和剩余预算；
2. Explorer 返回候选完整排列；
3. validator 拒绝遗漏、重复、未知或越权候选；
4. search kernel 物化一个或多个真实 prefix；
5. 可信层返回机械反馈；
6. Explorer 可以在预算内再次修正。

轮内 feedback 修正当前场景；跨轮 Memory 用于更换 hypothesis、分支或对照。每轮完成后只从已保存的
summary/Bundle 重算反馈，不把 Agent 内存当成执行事实。

当前 A3 已把首个可信选择送回完整 qualified execution、PSS、Risk、Replay 与 Oracle。A4a 完成短计划逐步
concretization；A4b 复用 durable provider journal，允许一次机械反馈修正，并把成功多步 Trace 编译为 exact
Policy 后送入同一 qualified Bundle。真实 provider 的效果仍需单独校准，不能由 stub 测试替代。

### 模型传输

活动 Agent 路径只使用一个 OpenRouter Chat Completions 客户端。模型厂商不是新的系统 Adapter：模型必须由
每次显式运行提供 OpenRouter `model_id`，并复用同一 prompt、持久化 journal、工作量统计和恢复路径。
端点固定为 OpenRouter，避免把 key 发送到任意地址；模型 ID、精确请求和 OpenRouter 返回的实际模型 ID 进入
已有调用审计，凭据不进入工件。历史 direct-DeepSeek 运行不改写，新运行也不尝试用 OpenRouter 恢复旧 journal。

这层统一的是模型传输，不统一模型能力。不同模型是否能生成有效 ScenarioPlan，仍需在相同输入、预算和机械
反馈下分别测量。

活动请求使用 strict JSON Schema，并要求 OpenRouter 只路由到支持所需参数的 endpoint。reasoning effort 和
output-token 上限是可编辑实验输入；当前 etcd/raft Agentic calibration 使用 `low`、`exclude=true`、
32,000 output tokens，`none` 仍可用于消融。模型输出
仍必须通过本地类型与 frontier 具体化，结构化输出不会增加事实或执行权限。

真实 A9e4c11 计费反例证明：已发送 POST 的超时不能安全解释为“provider 没有执行”，自动重试可能重复计费。
因此当前 client 对每个已发送的模型 POST 最多执行一次；连接中断、响应正文读取失败或超时记为
`agent-transport-ambiguous`，
不在传输层重发。`transport_attempts` 记录实际 HTTP 尝试，`provider_usage_status=observed|unknown`
区分“已收到 usage”与“本地无法知道 provider 账单”。unknown 时本地 token 值不得作为零费用结论。

A4c 的 target-local 运行器只组合现有组件：创建或恢复 model-call journal，执行一个 bounded Scenario episode，
再派生紧凑 `summary.json`。summary 不替代 journal、Trace 或 Bundle；成功时报告 Replay/PSS/Risk/Oracle 摘要，
provider 终止失败时报告已消费调用、本地 token 观测和 usage status，并确保恢复不会重新 dispatch。

A6a 继续把 Scenario episode 作为 Campaign attempt，而不是引入 Session Runtime。Campaign 负责外部
episode/work/model/time 预算、checkpoint 和恢复。`ScenarioAgentFeedback` 只在同一 episode 的真实 continuation
中使用；它携带上一计划、失败步骤、执行反馈和 natural-progress 摘要。跨 episode 时 root 重置，因此当前不传
上一 episode 的 Scenario feedback。

A9e4c1 将 Risk provider 契约扩展为最多 3 项的有序 portfolio，但没有增加评分器或第二套状态账本。
Agent 只决定候选内容和优先顺序；可信层逐项复用同一个 compiler/qualification 路径，并选择首个合格项。
所有候选的稳定 reason code 与 qualification issue 会随本轮 feedback 保存，供下一次机械修复；旧单候选
响应仍可解析，以便读取既有工件。跨 episode 的压缩记忆由 A9e4c2 从已保存证据派生。

A9e4c2 已完成该只读派生：每次从恢复后的 episode summary/Bundle 重算候选重复、Risk near-miss、protocol
PSS 增量、机械拒绝、Oracle finding 数和成本，只保留最近 8 轮给 Risk Agent。完整 Trace、PSS state 和 Oracle
证据不复制进 Memory；Memory 不参与 Action binding、Replay、PSS 或 verdict。它随 Risk 请求进入既有 provider
journal，因此不形成第二套事实账本。

A9e4c3 的内部 Investigation coordinator 只组合现有单 episode 目录入口。每轮结束后重新从该轮磁盘工件恢复，
而不是直接信任内存返回值；随后从全部已恢复轮次重算 Memory。它没有 session ledger，目录顺序就是执行顺序。
模型调用和 token 使用 provider audit 的实际值累计；runtime decision 先按每轮声明上限保守预留。
A9e4c4 已把该入口接到 `-investigation-episodes N`。恢复时只接受连续的 `episode-NNNN`；完整轮次从
summary/Bundle 恢复，最后一个 partial 轮次复用已有 journal 的精确请求恢复。根目录不增加 manifest 或 session
ledger。真实模型是否会依据 Memory 修订 hypothesis 仍需单独校准。

A9e4c5 将 Risk Agent 的机制解释绑定到可执行 witness。活动 portfolio 不再携带独立的自由文本机制，
而是让每个 predicate 对应一个同序、同 milestone ID、同 Observation kind 的 `mechanism_step`；可信代码
机械校验映射并生成保存的 `suspected_mechanism`。错配只形成可修复 feedback，不授予 Agent 执行或 verdict 权力。

新的完整计划路径允许模型在一次调用中表达多个有序战略步骤。仅第一步可安全使用当前
ActionID；后续步骤使用语义 selector，并可在已有 Risk milestone 满足后再具体化。期间自然推进只执行
effect completion、普通消息投递和自然到期 timer；完整计划内不以 24-action 作为边界。Scenario 直接在产生当前 admissible frontier 的短生命 Runtime 上执行所选 closure Action；执行前重新核对
prefix digest/长度、snapshot digest 和 Action 成员关系，随后立即关闭 Runtime。这样不再为 materialization 重放
同一 prefix，但 child 仍由 fresh Adapter 独立 Replay 验证，并继续生成 Trace 和完整 work 统计。Stateless DFS 与
Semantic Explorer 仍保留原来的独立 materialization 路径。该闭包已抽为协议无关执行组件，但尚未完成所有
baseline 的配对接线，因此当前结果不能用于宣称 LLM 优势。

效果评测的进程边界由 evaluator 持有。它先复用现有 BuildAudit 与二进制摘要校验，再从审计后的 bytes 创建
临时可执行文件，在显式超时内启动 fresh-root paired strategy。终端根 summary、两个 Campaign 和 Bundle 必须
相互匹配，且 Bundle Manifest BuildID 必须回指 BuildAudit 的 SUTBuildIdentity。单 SUT launcher 之上已有内部 multi-SUT
batch：它在任一进程启动前对全部 BuildAudit、二进制和预期身份做整体预检，按稳定 Trial ID 顺序启动，并重验
每个返回证据的 fresh-root 规则和两 arm BuildID。任一预检失败时不调用 runner、不创建批次工件。

`defect-eval -paired-scenario` 将该 batch 暴露为薄 CLI。已完成 trial 从 evaluator 侧严格恢复，不启动 SUT 或读 key；
未完成 trial 向同一审计二进制传入 `-campaign-resume`，继续使用 Campaign journal。CLI 实际使用的 semantic JSON
bytes 必须在 formal exposure audit 中，防止审计后替换外部输入。尚未完成 sealed candidate/control verdict，因此该能力不能被
描述为正式 holdout 实验。

批次完成后，evaluator 会在内存中将 deterministic/Agent Bundle 拆成两个 method 集合。每个集合复用 contract 的
SUT pair、Profile、build/config、预算与 trusted composition，经 Bundle/Projection/TraceIntegrity 校验后分类 control pass/
false positive 和 candidate survived/killed。两轴的 invalid 不会彼此污染。该视图只用现有 `BundleTrialResult`，不持久化、
不 seal；外层 Campaign work 仍是方法效率的唯一总成本。v1 formal contract 只能绑定单 MethodSpec，因此正式双方法
contract/result 仍未完成。

A6b 在 session attempt artifact 中保存每个 qualified bundle 已有的规范 Core PSS 状态键。终端派生视图对这些
键取并集，同时汇总 Agent/Testing/Replay episode 数、Risk 最佳进展和 Oracle violations；CampaignSummary 继续
拥有 work/model/time 成本。该视图不持久化第二份账本，也不参与下一次 `ScenarioAgentView`。A4c 单 episode
artifact 保持原格式，避免破坏历史恢复。

A6c 用真实 OpenRouter/DeepSeek 验证了这条路径：第一 episode 经 3 次传输成功，第二 episode 一次成功；
两轮都形成 qualified testing 并稳定重放。第二轮计划更长、PSS 状态更多，但单次公开校准不能将这一差异
归因于跨 episode 反馈，也不是 Agent 优于 baseline 的证据。

## 9. 评价面

正式结果分开报告，不合成含义不明的总分：

- defect effectiveness：隐藏 candidate 根因检出、正确 control 误报、复现率；
- semantic coverage：固定义务覆盖、PSS/Risk 发现、`PSS-Action-PSS` 转换和重复循环深度；
- efficiency：decisions、primary/replay work、模型调用和 token；
- portability：新 Adapter 的目标专用代码量与获得的控制能力。

PSS 状态数是无固定分母的搜索反馈；义务覆盖率是在 Profile 边界内的成果指标。两者都不能证明协议正确，
也不能单独证明 Agent 优势。长选举循环可能执行数百 Action 但只重复少数 protocol PSS；因此
状态广度和时间深度必须并列报告，不为了计数增长把绝对 term 或循环计数加入 Core PSS。

## 10. 工件与代码保留规则

- Git 保存历史；HEAD 只保存当前代码、必要输入和紧凑结果；
- 完整 Trace/bundle 默认写入 ignored `artifacts/`，不提交重复展开的 JSON；
- 阶段文档记录结论和边界，不复制完整对象；
- 已被当前路径替代的 CLI、数据模型和测试一起删除；
- 默认不新增 hash、冻结 contract、baseline 或 gate。只有存在一个具体失败场景，并能说明 Git、版本号、
  类型系统、普通测试和常规存储约束为何不足时，才考虑增加；
- 新抽象至少应有两个现实消费者，否则优先留在 target-local composition；
- 每个阶段报告生产代码、测试、Markdown 和 JSON 的净变化，防止“文件增加等于进展”。

## 11. 检查节奏

开发过程中只运行受影响包和关键集成测试。一个完整阶段结束时再运行 `make test`、`go vet ./...` 和
`git diff --check`。完整 race 只用于明确的发布或里程碑检查；普通测试不访问外部模型。
