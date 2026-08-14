# ConsensusAtlas 架构

日期：2026-08-14

本文件描述当前代码和已经确定的演进边界，而不是历史阶段。系统只保留一条权威主线：

> **开放规划，受控执行，机械反馈，独立判定。**

Agent 负责形成协议感知的测试意图；可信内核只把可验证的意图落实为真实 enabled Action；
Replay/Oracle/evaluator 独立判定结果。规划可以是启发式的，执行事实和最终判定不能由 Agent 自报。

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
- Protocol Pack：协议事实、共识风险、允许的关系与可观察里程碑；
- Experiment Config：拓扑、workload、fault envelope、时间/随机源策略和资源预算。

Target Pack 中只有目标接口绑定与证据提取必须是代码。可审查、可变的拓扑、预算、工作负载和协议知识应逐步
迁移到 JSON/YAML。A5 已将 etcd/raft 活动 Agent 路径的 ProtocolKnowledge、TestHypothesis、workload、
Adapter topology/ticks、Runtime、FaultEnvelope 和 A2/A4 预算外移到 `plans/agent/*.json`；运行时转成
原有可信类型，不引入第二套语义或 workload 契约。

Authoring JSON 只提供人工输入字段和外部运行上限。它不能提供 schema version、digest、执行 backend、
Oracle 或 verdict；Agent 也不能修改这些预算。加载器严格拒绝未知字段和作者伪造的派生身份。
拓扑、时间参数或 workload 与已有 root corpus 不一致时，现有 Adapter manifest、bundle 和 Replay 校验会拒绝组合。

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

- 形成 `TestHypothesis`；
- 选择语义目标；
- 对当前可信候选排序；
- 后续以短时域 `ScenarioPlan` 表达若干语义选择器与期望观察点；
- 根据不可达、near-miss、PSS/Risk 变化修正后续提议。

Agent 不可以：

- 创造消息、节点、时间或 enabled Action；
- 修改 fault/budget、Adapter qualification 或当前 Oracle；
- 声称某个 PSS/Risk/义务已经满足；
- 决定 candidate/control 身份或正式 verdict。

因此 Agent 可以犯规划错误，但不能把错误解释写成执行事实。

当前 A4a 已实现最小 `ScenarioPlan`，最多 8 步，只允许当前 ActionID 或 ActionKind、节点、消息端点、时间种类、
effect 等有限公共字段。每个 selector 都由可信层在当下 admissible frontier 中解析：唯一匹配才执行，零匹配、
歧义和外部预算耗尽都返回机械 reason。它不是通用 DSL，也不允许计划携带 fault budget、assertion 或 verdict。

Agent 在概念上分为三种认知角色：Hypothesis、Explorer、Analysis。它们可以先由一次模型调用或一个进程承担；
只有同预算消融证明角色分离有收益时，才物理拆成多个 Agent 服务。

A6e 的协议语义不进入 `FrontierActionRef`：Scenario view 另带一个与当前 prefix、snapshot 和每个 Action 身份绑定的
`action_semantics`。target-local projector 只能输出封闭分类，未知内容必须为 `unknown`；原始协议状态、绝对 epoch、
消息 payload、未来事实和 verdict 不进入该视图。`full`/`masked` 共享完全相同的 Action 集，masked 仅隐藏语义值，
因此它是模型信息消融，不是控制能力消融。
运行时可在 A6 session 入口覆盖暴露模式；覆盖值重建现有 session spec，因此不同模式不会误恢复同一
Campaign，也不需要另一套配对运行协议。

A6eR 现在把活动路径限制为单步当前 Action，避免模型预测 future ActionID。干预成功后，可信 closure 按固定
优先级执行普通 effect、message 和自然 timer，直到 Risk 里程碑变化、客户端返回、自然推进静止或预算耗尽；
随后 Runtime 重建 frontier/snapshot，target-local projector 只对该状态重新分类。stopped 多步计划的 leading
applied prefix 会保留，rejected step 只进入反馈。整个 episode 仍受 call、decision、work、token 和 wall-clock
上限约束。

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
OmniPaxos 已进入现有多 episode Campaign；当前仍需补完整 Bundle 持久化、成本分类、外部 worker 生命周期和
方法身份，才能将该入口用于正式效果评价。

## 6. 规划确定性与执行确定性

两者必须分开描述：

- 执行确定性：同一已具体化 Action 序列、受控随机带和初始状态应得到可重放 Trace；这是可信要求；
- 规划可重复性：同一模型可能提出不同假设或排序；这是搜索方法的随机性，应通过记录模型输入输出、固定预算、
  多次 trial 和同预算比较管理，而不是假装 Agent 本身确定。

因此，系统不要求 Agent 每次给出相同计划，但要求每个被接受的计划都能被机械解释、执行和复核。

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

Agent 的单位不是“每一步自由选一个 Action”，而是有界 semantic episode：

1. 可信层给出当前 hypothesis、prefix、语义候选和剩余预算；
2. Explorer 返回候选完整排列；
3. validator 拒绝遗漏、重复、未知或越权候选；
4. search kernel 物化一个或多个真实 prefix；
5. 可信层返回机械反馈；
6. Explorer 可以在预算内再次修正。

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

活动请求使用 strict JSON Schema，并要求 OpenRouter 只路由到支持所需参数的 endpoint。reasoning effort 是
可编辑实验输入；当前主配置为 `high`、`exclude=true`、4096 output tokens，`none` 仍可用于消融。模型输出
仍必须通过本地类型与 frontier 具体化，结构化输出不会增加事实或执行权限。

瞬时可用性与计划失败分开：无响应的传输错误、HTTP 408/429/5xx 可在同一请求上最多重试 2 次；
鉴权失败、可读但非法的模型响应、非法 ScenarioPlan 和预算终止不进入传输重试。逻辑请求仍消耗一次
`ModelWork.Calls`，实际 HTTP 尝试用 `transport_attempts` 审计，避免将重试伪装成免费模型决策。
无响应尝试没有 provider usage 可供本地计费，因此 token 成本是已收到响应的精确值，不是网络模糊期间的 provider 账单上限。

A4c 的 target-local 运行器只组合现有组件：创建或恢复 model-call journal，执行一个 bounded Scenario episode，
再派生紧凑 `summary.json`。summary 不替代 journal、Trace 或 Bundle；成功时报告 Replay/PSS/Risk/Oracle 摘要，
provider 终止失败时报告已消费调用和零 token，并确保恢复不会重新 dispatch。

A6a 继续把 Scenario episode 作为 Campaign attempt，而不是引入 Session Runtime。Campaign 负责外部
episode/work/model/time 预算、checkpoint 和恢复。`ScenarioAgentFeedback` 只在同一 episode 的真实 continuation
中使用；它携带上一计划、失败步骤、执行反馈和 natural-progress 摘要。跨 episode 时 root 重置，因此当前不传
上一 episode feedback；Oracle、PSS、testing outcome 和候选身份始终不进入 Agent 反馈。

活动路径每次只允许模型选择一个当前 Action。可信 natural-progress closure 随后只执行 effect completion、
普通消息投递和自然到期 timer，并在 Risk 里程碑变化、目标客户端返回、自然推进静止或 decision budget 耗尽时
停止。每个 closure Action 仍经过当前 admissible frontier、fresh Adapter materialization、Replay、Trace 和 work
统计。该闭包已抽为协议无关执行组件，但尚未完成所有 baseline 的配对接线，因此当前结果不能用于宣称 LLM 优势。

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
- semantic coverage：固定义务覆盖和 PSS/Risk 发现；
- efficiency：decisions、primary/replay work、模型调用和 token；
- portability：新 Adapter 的目标专用代码量与获得的控制能力。

PSS 状态数是无固定分母的搜索反馈；义务覆盖率是在 Profile 边界内的成果指标。两者都不能证明协议正确，
也不能单独证明 Agent 优势。

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
