# ConsensusAtlas

ConsensusAtlas 是面向共识实现的确定性测试研究框架。当前已验证范围是 leader-based
CFT/Raft；Control Runtime 保留 limited-BFT 扩展目标，但当前不声称已具备 BFT 适用性。主线已经收敛到一套协议无关的
Control Runtime：目标系统通过薄 Adapter 暴露消息、自然时间、生命周期、持久化副作用、外部输入和
Evidence；可信 Go 内核负责动作资格、调度、重放、语义状态采样、Oracle 和评测账本。

当前阶段已冻结 **M5.21d5 Single-call External Connectivity Calibration**。它将使用新的
durable Campaign runner 做一次固定输入、单 attempt、最多单次外部调用的连通性校准；
不做方法对比，不把 proposal 成功预设为验收条件。M5.21d4 已完成 terminal accounting：通用
Campaign store 使用
exact call intent、dispatch marker 和 result 三段 no-replace 记录远程规划调用。dispatch-only
状态在恢复后机械成为 `ambiguous`，不能再调用或补写 result。`CampaignConfig/v3`
显式冻结 `none/zero-model/durable-call` planner mode。d2 已用注入的离线 transport 把该状态机
接入 etcd/raft provider：durable success result 恢复不重调，dispatch-only/failed 不重试，模型
成本只在 Campaign 外层累计一次。M5.21d3 已增加独立显式 strategy，并按 attempt 强制
`exact intent durable -> key read -> one Step -> key clear`。测试只使用注入的离线 key reader/transport；
没有读取桌面密钥或调用真实模型，外部 model calls/tokens 仍为 0。M5.21d4 只允许 failure marker
引用 store 已验证的同 ordinal model-call result，并将 terminal work 纳入 failed Summary totals，
同时保持 attempt/checkpoint/PSS evidence 不增加。dispatch-only ambiguous 仍为零成本；failed、
proposal-invalid 与 over-budget durable result 的实际调用成本均可恢复并机械聚合。

M5.21c 已完成 Durable Planned Attempt。用户给定 attempts、decisions、first seed
和 wall-clock ceiling 后，etcd/raft runner 在每个 attempt 前从已提交 Observation 构造最小
Planner View，用 zero-model deterministic planner 只修改 `Prefer`，再经可信 compiler 生成
plan/instance/choice。完整 planned envelope 以 fsync/no-replace 先于 target execution 落盘；
恢复时复用 exact `head+1` plan，不重算 Planner。checkpoint、artifact 和 Observation 均机械绑定
planned-attempt digest。当前 `CampaignConfig/v3` 仍显式区分 base-request/planned-attempt，因此计划缺失、
替换、篡改或 future ordinal 不能降级为旧路径。

first seed 101 的真实 3x8-decision 见证完成 3 attempts：24 primary decisions、27/27
primary/replay work、27 个 Core PSS samples、17 个唯一状态，model calls/tokens 为 0。这只证明
zero-model 规划/恢复/执行/观测链连通，不证明方法优势或覆盖完备。真实远程 Planner 仍需先增加
durable call-intent 和 ambiguous terminal，以处理“已收费但响应未落盘”的恢复歧义。
此前 M5.18b4 pair orchestration/persistence 已接到
freeze-before-key、failure-before-return-persistence 的正式 CLI 和 Make 入口。已有 pair 复用 frozen request consumer，
以固定双臂顺序、失败隔离、完整成本 pair ledger 和全新目录持久化形成离线闭环；未读取 key、
未调用模型。单臂 consumer 已把 freeze 绑定的 exact request 接入既有
response audit、strict parser、baseline validator、plan v2、seed-4 execution 和 IntentOutcome。
pair 层已验证固定顺序、首臂失败隔离、ledger 篡改拒绝、禁止覆盖以及 key/私有诊断不落盘。
runner 还已验证已存在目录在 source construction 前拒绝、无效 freeze 在 key 前拒绝，两个 typed
arm failure 先落盘再返回。入口不被任何测试目标自动调用。
M5.18b4R 当前清单为 26/26 exact-once，受影响的 13 项 agent shard race 已通过。M5.18b4 已冻结的
no-feedback/with-feedback 两臂精确 prompt/request bytes、共同
hard baseline、seed 4、执行预算和 transport 上限已在
读取 key 前冻结。唯一可见信息差异是 feedback 为 JSON `null` 或可重算对象。本轮没有调用模型。

## 当前闭环

```text
qualified Adapter + opaque workload + bounded policy
                         |
                         v
        one deterministic Control Runtime executor
                         |
          +--------------+----------------+
          |              |                |
      full Trace     Core PSS samples  client results
          |              |                |
          +--------------+----------------+
                         v
            ExecutionBundle v1/v2/v3
          + optional OperationHistory v1
                         |
        target-owned DecisionProjector
                         |
                         v
     TraceIntegrity + protocol-neutral Agreement
                         |
                         v
 evaluator-owned fresh control/candidate ledger
```

新的宏观入口位于该闭环之上：

```text
ProtocolKnowledgePack + Qualification -> AgentSemanticView
                                               |
                                     GuardedTestIntent
                                               |
                              deterministic compiler/fallback
                                               |
                               existing qualified policy path
```

通用 Oracle 不解析 Raft 字段。etcd/raft 的可信 composition 提供
`official-etcdraft-v2/applied-prefix-digest-v1` 投影，将 opaque Evidence 映射为
`participant + decision position + exact value digest`；Oracle 只比较这个最小协议无关输入。

## 已实现边界

- 协议无关 Action/ProducedItem/Adapter 契约与唯一 Control Runtime；
- Runtime-owned 消息保留、投递、丢弃、复制和分区；
- 只执行最早已到期 temporal item 的自然时间模型；
- incarnation、power-loss crash/restart、durable effect 和 application effect；
- 分域确定性 entropy、完整随机 tape 与严格 replay；
- 外部 Conformance Suite、版本化 Qualification 和 digest-bound Experiment admission；
- 固定 Core PSS IR、可信在线采样和跨 run 状态发现；
- opaque workload、Semantic Mapping guard、FaultEnvelope 和完整成本账本；
- 可恢复多-attempt Campaign、durable planned-attempt、内容寻址 artifact、小型 Summary/reader
  和 etcd/raft 离线 runner；
- qualified fixed workload、action-class random 与 trace mutation policy；策略不能提交 enabled set；
- 自包含 ExecutionBundle：完整 trace、preparation state transition、Evidence、最终 Snapshot、Core PSS、
  client history、decision history、Qualification 和 work ledger；
- TraceIntegrity 与 Agreement 两个 v2 trusted monitor；
- source-bound 构建变体、build audit 和公开 calibration pair evaluator；
- Experiment v2 的共同 admissible frontier、三类显式终止、pending workload 和选择审计；
- target-owned WorkloadRouter，以及 fresh replay 中对每次 Invoke 唯一目标的重算；
- 有序 MutationSourceCorpus、occurrence-aware trace mutation v2、可信重投影 PSSFeedback；
- 自包含小型 MethodLedger，强制记录 source/proposal/execution/失败和 primary/replay 成本；
- 共同 method ceiling、method-level PSS measurement 与可重算 MethodObservation；
- 按规范化 ActionID 在 admissible frontier 上采样的 qualified uniform random；
- 只在 batch 边界消费可信 feedback 的 PSS-guided mutation choice；
- digest-bound MethodSpec、非 SUT Config projection 和单执行方法预算；
- ExecutionBundle v3 的冻结 WorkloadPlan 与可机械重建 invoke/return OperationHistory；
- build-audit/binary-bound evaluator-owned fresh execution；
- build-blind AgentSemanticView、strict GuardedTestIntent parser 和 deterministic macro compiler；
- hard capability/action/fault/budget 拒绝、preference miss/compiler work 与 Trace-backed hard Action 验证；
- 有界 one-shot DeepSeek JSON transport、安全 key-file 读取和 digest-bound AgentInvocationAudit；
- bundle-backed `AgentBatchFeedbackView`、敏感身份去除与 preference-only 消融边界；
- freeze-before-key、typed-failure-before-return-persistence 的显式 opt-in pair runner；
- execution/workload 分离的 feedback v2、冻结 source/unseen seed 与完整 per-arm 成本的 follow-up spec；
- Runtime-supported / Experiment-producible / backend-selectable 三层 Action surface；
- seed-free CompiledIntentPlan v2、digest-bound execution instance 和独立 IntentOutcome；
- pre-call exact-byte request freeze、单份 proposal/baseline 校验与 1-call/0-retry 消融边界；
- 按 ActionKind 再按成员均匀采样的确定性随机基线，以及 digest-bound FaultEnvelope selectable view；
- 精确 source prefix、相邻 Action swap、显式 priority suffix 与不可执行变体记账的 trace mutation；
- 官方 `go.etcd.io/raft/v3 v3.6.0` 的完整 strict 路径；
- HashiCorp Raft v1.7.3 的部分资格，用于证明统一 Action 不等于统一控制强度。

## M5.16 公开校准结果

两侧使用相同 workload、96 scheduler decisions 和 98 primary work units：

| trial | build identity | TraceIntegrity | Agreement | 结果 |
|---|---|---|---|---|
| correct control | `go.etcd.io/raft/v3@v3.6.0` | pass | pass | `control-pass` |
| source-bound calibration | `sut-c9811ab0ed8e2f39` | pass | step 55 conflict | `killed` |

评测账本为 1 control、0 false positive、1 calibration candidate、1 killed root cause、0 invalid。
这是公开链路校准，不是非公开 holdout，也不能证明 Agent 优于 Random、DFS 或专家策略。Coverage/PSS
没有参与 verdict。

## M5.17a 强基线结果

action-class random 使用相同 single-write workload、FaultEnvelope、96 decisions 和 98 primary/replay
work。冻结 seed 1 的 control 发现 83 个 Core PSS 状态并通过重放与两个 Oracle；同一公开 candidate 却
`survived`。相比之下，M5.16 fixed policy 只发现 56 个状态但检出了它。

这个负结果被原样保存，没有筛选 seed。它说明 PSS discovery 只能作为 coarse feedback，不能替代根因
检出与正确 control 误报。单一公开 candidate 也不能支持 fixed/random 的一般优劣结论。

## M5.17b Trace Mutation 结果

公共 operator 按 source Trace 顺序选择第一对相邻 message deliveries（decisions 46/47），精确重放前缀、
交换 pair，再使用 digest-bound priority suffix。official control 完成 96 decisions、workload 和 strict
replay；mutation 与 source 都发现 56 个 Core PSS states、prefix area 3324，但 trace digest 不同。

成功 mutation 自身为 98 primary / 98 replay；加上生成 source Trace 的 98/98，方法真实成本是 196/196。
另一个依赖性失败校准以稳定 reason code 停在 decision 34，记录 35 primary work、0 replay，不静默
fallback，也不算作 method trial。本阶段没有 candidate 或 defect verdict。

## M5.17c0 Experiment 语义结果

etcd/raft 使用同一 qualified single-write 输入时，96-decision 上限的 v2 运行在 42 个决策后
完成 workload 并 `configured-stop`，发现 31 个 Core PSS states，primary/replay 各 44 work。
1-decision 运行则合法保留 0 offered、1 pending 和 `no-candidate`，而不是变成执行错误。
两者都通过 fresh replay 和 bundle 校验；这只验证实验语义，不是新的 defect verdict。

## M5.17c1 Corpus 可信前提结果

真实 etcd/raft corpus-mutation 见证使用一个 96-decision source，按公开 first-adjacent-delivery 规则
生成 occurrence-aware proposal，并在同一 qualified executor 中完成 96 decisions 和 strict replay。
mutation 产生 97 个 Core PSS samples、56 个状态；source 与 execution 各为 98 primary / 98 replay，
MethodLedger 机械合计为 **196 / 196**。可信 feedback 从 Trace、最终 Snapshot 和 Evidence 重新投影，
不接受调用方提供 state key。

本阶段只完成搜索前的可审计数据面：公开 corpus 仍只有一个 fixed-workload source，没有 PSS-guided
选择、uniform-random 对照、candidate verdict 或 holdout 结论。

## M5.17c2 Batch PSS Guidance 结果

uniform 方法使用 seeds 1/2/3，在 294 primary / 294 replay ceiling 内完成三条
96-decision strict-replay bundle，分别发现 82/84/90 states，method-level union 为 238。

PSS-guided 方法共享前两条 source，根据 exclusive states 和 batch-global visits 选中
source 2、decision 65 的相邻消息交换。它在 decision 66 产生
`EXPERIMENT_TRACE_MUTATION_ACTION_NOT_ENABLED`，最终记录 263 primary / 196 replay 和 157 个
source states，没有 mutation bundle。这说明 PSS 稀有度不包含 Action 因果/可交换性。

不能用 `238 > 157` 宣称 uniform 更优：两者实际完成度和消耗不同，且都没有
candidate/control verdict。

## M5.18a 可信方法评价校准

公开 fixed MethodSpec 同时用于官方未修改 control 和 command-data calibration candidate。evaluator
不接受 submitted bundle，而是验证 build audit/binary 后从已校验 bytes 创建临时可执行副本并亲自运行。
两边均为 96 decisions、98/98 primary/replay，Config projection 和 OperationHistory digest 相同；
control `control-pass`，candidate 在 applied-prefix position 5/step 55 被 Agreement 检出。

OperationHistory 显示两边都在 step 28 invoke、step 42 返回 `committed`，因此 client history 本身没有
制造 kill。最终账本为 1 control、0 false positive、1 killed public calibration root cause、0 invalid。
这仍不是 private holdout，也没有比较任何 Agent 或搜索方法。

## M5.18b0 Guarded TestIntent 见证

首个人工 one-shot intent 引用 `leader-change-with-inflight-proposal` risk，不包含 ActionID 或未来
decision number。它偏好一个不存在的 `dpor` backend；compiler 对 2 个 hard-eligible backend
做机械检查，记录 preference miss 并使用冻结 fallback `action-class-random`。compiler work 为 4，
与 Runtime primary/replay work 分开记录。

执行后的真实 Trace 同时出现 invoke、message delivery、natural temporal、crash 和 restart，并通过
strict replay。report/bundle identity 与 M5.17a 的 seed-1 action-class 运行相同，说明宏观
compiler 只调用已有执行路径。本轮没有 LLM、batch feedback、private holdout 或新 defect verdict。

## M5.18b1 真实 one-shot transport

唯一真实调用使用 JSON output、thinking disabled、temperature 0、1200 output-token ceiling 和
无重试。模型输出通过 strict parser/compile 后选择 `admissible-uniform`，执行 96 decisions、
98/98 primary/replay，完成 1 个 workload 并发现 82 个 Core PSS states。模型成本为
1568 input / 211 output / 1779 total tokens。

但 accepted id、预算和 backend 与当时 prompt 的具体有效示例完全一致。该运行因此明确归类为
`public-transport-calibration-only`。修正后的 fictional 结构示例尚未再调用。小型结果见
[summary.json](benchmarks/experiments/etcdraft-v2-agent-one-shot-m5.18b1/summary.json)。

## M5.18b2 Defect-blind batch feedback

action-class random 和 admissible uniform 使用预先固定的 seeds 1/2/3，共同上限为
3 attempts、294 primary 和 294 replay。action-class 完成 2 次、失败 1 次，实际成本
294/196，发现 165 个粗粒度状态；uniform 完成 3 次，成本 294/294，发现 238 个状态。

seed 3 的 workload 未在 decision 96 前完成，该 attempt 保留计费且不用替换 seed。Agent 只看到
`execution-failed`，精确 failure code 仅保存在可验证账本中。`238 > 165` 不是方法效果结论。
小型 Agent 可见工件见
[feedback.json](benchmarks/experiments/etcdraft-v2-agent-feedback-m5.18b2/feedback.json)。

后续复核发现，这个 v1 对比混用了两种 workload 终止语义：action-class 把预算内 pending 归为执行
失败，uniform 则按 Experiment v2 保留为合法完整执行。因此上述 `2/1` 与 `3/0` 只能作为发现问题的
历史校准，不能作为方法选择依据；原 digest 与工件不回写。

## M5.18b3 Unseen follow-up baseline

修正后的 feedback v2 让 action-class-v2 和 uniform 都使用 Experiment v2。两者在 seeds 1/2/3 上
均为 3 次框架执行完成、2/3 workload 完成、294/294 work；分别发现 241 和 238 个 coarse PSS states。
确定性规则在完全相同的 workload/execution 完成度下按 canonical backend ID 选中 action-class，并在
冻结的未见 seed 4 上执行。

seed 4 消耗 97/97 work、strict replay 稳定并发现 79 个状态，但 workload pending，且没有实际覆盖
intent 的全部 hard ActionKind，所以结果原样记为计费的 `execution-failed`，不更换 backend 或 seed。
每个 arm 连同 source 实际计费 685/685，模型调用数为 0。这不是缺陷 verdict 或方法优劣结论。小型工件见
[summary.json](benchmarks/experiments/etcdraft-v2-agent-follow-up-m5.18b3/summary.json)。

## M5.18b4-pre 可信边界修正

新的 b4 catalog 不再把 Runtime 能表示、但当前 Experiment 没有 producer 的 Partition/Heal 声明成
backend-selectable，并将对应 envelope 收紧为 0。`CompiledIntentPlan/v2` 不含 seed；可信
`IntentExecutionInstance/v1` 单独绑定 seed 4 和 98/98 ceiling。

该实例通过 qualified execution 和 strict replay，实际消耗 97/97 work、发现 79 states，但 Trace 缺少
hard `invoke`。结果因此是 execution `valid`、intent `not-reached`、Oracle `not-evaluated`，而不是
execution failure。它只校准可信边界，不是 RiskWitness、缺陷 verdict 或 Agent 效果结论。小型工件见
[summary.json](benchmarks/experiments/etcdraft-v2-agent-b4-preflight-m5.18b4-pre/summary.json)。

## M5.18b4 Preference ablation request freeze

两臂使用相同 system prompt、semantic view、risk、must、seed 4、98/98 execution ceiling 和
transport 限制。no-feedback request 显式包含 `agent_batch_feedback: null`，with-feedback request
包含可重算 feedback v2。精确 request digest 分别为 `562d3bda...31ce5c` 和
`eb44d738...f6826f`；冻结对象为 `2591cf89...eed8c1`。

未读取 key，模型调用数为 0。小型承诺工件见
[freeze.json](benchmarks/experiments/etcdraft-v2-agent-b4-freeze-m5.18b4/freeze.json)。

## 快速验证

```bash
make test-fast
make test
go vet ./...
make test-race-full
```

M5.18b4R 已将全部顶层重测试机械分入 exact-once race shards，完整门禁已通过；早期单进程
20 分钟超时仍作为历史失败记录保留。M5.19d 的普通全仓测试通过，
`cmd/control-experiment` 用时 103.970 秒；Summary/reader 与真实 runner 定向 race 分别以
1.449/45.984 秒通过。本阶段没有无界重跑无关的全量 race。

重跑官方 bundle 和公开 calibration：

```bash
make experiment-etcdraft-v2-bundle
make experiment-etcdraft-v2-semantics
make experiment-etcdraft-v2-campaign
# 仅用于上一进程未产生 summary 的 exact-config 恢复
make experiment-etcdraft-v2-campaign-resume
make experiment-etcdraft-v2-calibration
make evaluate-etcdraft-v2-calibration
make experiment-etcdraft-v2-action-class-random
make experiment-etcdraft-v2-action-class-calibration
make evaluate-etcdraft-v2-action-class-calibration
make experiment-etcdraft-v2-trace-mutation
make experiment-etcdraft-v2-corpus-mutation
make experiment-etcdraft-v2-uniform-method
make experiment-etcdraft-v2-pss-guided-method
make experiment-etcdraft-v2-action-class-method
make experiment-etcdraft-v2-agent-feedback-batch
make experiment-etcdraft-v2-agent-follow-up-baseline
make experiment-etcdraft-v2-agent-b4-preflight
make experiment-etcdraft-v2-agent-b4-freeze
make build-etcdraft-v2-method-evaluation
make evaluate-etcdraft-v2-method-evaluation
```

模型调用始终是显式 opt-in，不属于任何测试目标：

```bash
AGENT_KEY_FILE=/secure/path/key.txt \
AGENT_ARTIFACT_DIR=artifacts/experiments/new-one-shot \
make experiment-etcdraft-v2-agent-one-shot
```

`experiment-etcdraft-v2-calibration` 会从冻结的 source transform 构建本地二进制；二进制和完整
约 1.9 MB bundle 写入被 Git 忽略的 `artifacts/`。仓库只保存小型 build input/audit、benchmark
manifest 和 evaluator report，避免再次把重复 trace body 提交成超长 JSON。

当前 Guarded TestIntent 已有真实在线模型入口，但只有一次受锚定的公开校准调用。M5.13 旧
transport 仍是历史记录，已从可编译路径删除；当前入口才具备 admission/workload/
FaultEnvelope/ExecutionBundle 和 progressive audit 边界。

## 当前目录

```text
adapters/                 目标专用薄接入与 Semantic/Decision Mapping
  etcdraftv2/             官方 etcd/raft strict Adapter
  hashicorpraftv2/        第二实现的部分资格 Adapter
  fixture/                协议无关契约 fixture
internal/
  control/                Action、Item、Manifest 与 opaque envelope
  controlruntime/         enabled、选择、状态机、trace 和 replay
  controlentropy/         分域随机与 replay tape
  conformance/            外部能力见证与 Qualification
  controlexperiment/      admission、workload、policy、ExecutionBundle
  psscore/                固定 Core PSS IR 与投影
  protocolstate/          状态发现与跨 run 聚合
  semantic/               协议无关 decision observation
  oracle/                 v2 trusted monitors
  defectbench/            最小 control/candidate evaluator
  sutbuild/               source-bound 构建与审计
cmd/                      资格、实验、构建和评测 composition roots
qualifications/           两个真实 Adapter 的机械资格组合
benchmarks/               冻结的公开工件；历史内容不等于当前 API
docs/                     当前设计与不可改写的阶段记录
```

`agents/`、`drivers/`、`families/`、旧 Campaign/Coverage/Engine/Host，以及旧 Planner/model transport
源码已经删除。`make audit-no-v1` 和 `make audit-no-retired-experiment` 阻止可编译源码重新依赖这些路径。

## 信任边界与限制

- 未来 Agent 可理解冻结协议知识并生成 Guarded TestIntent，但不能决定 enabled、执行、PSS 真值、Oracle 或得分；
- Adapter/DecisionProjector 属于目标接入信任根，必须通过独立 fixture、conformance 和 replay 检查；
- Core PSS 用于 coarse discovery/feedback，不是状态等价证明，也不是正确性分数；
- Coverage 是未来解释性内部指标，不能代替隐藏候选检出和正确 control 误报；
- HashiCorp 当前缺少 strict natural-time/entropy/replay 资格，不能用于严格方法比较；
- 当前只有公开 calibration，没有非公开 candidate/control holdout；
- 当前没有证明 etcd/raft、其他协议或 ConsensusAtlas 正确、完备或无缺陷。

M5.20 引入、M5.21b 升级的 `CampaignObservation/v2` 将 committed artifact 机械投影为跨 attempt 视图，分开展示
outcome/work、Core PSS 并集/发现曲线、fault/workload 统计和 monitor 触发索引。
v2 另将每个 attempt 绑定到已重验的 choice/intent/plan/backend，不暴露 policy seed 或 instance。
runner 现要求在 `-out` 之外显式提供 `-campaign-observation-out`。各栏仍不合成
自定义“完备度分数”，monitor 零触发也不是正确性证明。

阅读入口： [当前阶段](docs/CURRENT_STAGE.md)、[M5.21d4 terminal accounting](docs/stage-m5.21d4-pre-plan-terminal-accounting.md)、[M5.21d3 opt-in durable runner](docs/stage-m5.21d3-opt-in-durable-runner.md)、[M5.21d2 offline durable planner](docs/stage-m5.21d2-etcdraft-offline-model-planner.md)、[M5.21d1 durable model call](docs/stage-m5.21d1-durable-model-call.md)、
[M5.21c durable planned attempt](docs/stage-m5.21c-durable-planned-attempt.md)、
[M5.21b choice attribution](docs/stage-m5.21b-prior-choice-attribution.md)、
[M5.21a Planner View](docs/stage-m5.21a-campaign-planner-view.md)、[M5.20 Campaign Observation](docs/stage-m5.20-campaign-observation.md)、
[M5.19 Campaign foundation](docs/stage-m5.19-campaign-foundation.md)、
[M5.19a persistence](docs/stage-m5.19a-campaign-persistence.md)、
[M5.19b Coordinator](docs/stage-m5.19b-campaign-coordinator.md)、
[M5.19c real provider](docs/stage-m5.19c-real-campaign-provider.md)、
[M5.19d summary/runner](docs/stage-m5.19d-campaign-summary-runner.md)、
[M5.18b4 runner](docs/stage-m5.18b4-pair-runner.md)、
[M5.18b4 pair](docs/stage-m5.18b4-pair-ledger.md)、
[M5.18b4 consumer](docs/stage-m5.18b4-request-consumer.md)、
[M5.18b4 freeze](docs/stage-m5.18b4-request-freeze.md)、
[M5.18b4-pre](docs/stage-m5.18b4-pre-trust-corrections.md)、
[M5.18b3](docs/stage-m5.18b3-unseen-follow-up.md)、
[M5.18b2](docs/stage-m5.18b2-defect-blind-batch-feedback.md)、
[M5.18b1](docs/stage-m5.18b1-one-shot-agent-transport.md)、
[M5.18b0](docs/stage-m5.18b0-guarded-intent-compiler.md)、
[M5.18a](docs/stage-m5.18a-method-evaluation-prerequisites.md)、
[M5.17c2](docs/stage-m5.17c2-batch-pss-guidance.md)、
[M5.17c1](docs/stage-m5.17c1-corpus-trust-prerequisites.md)、
[M5.17c0](docs/stage-m5.17c0-experiment-semantics.md)、
[M5.17bR2](docs/stage-m5.17b-r2-experiment-path-pruning.md)、
[M5.17b](docs/stage-m5.17b-trace-mutation.md)、
[M5.17a](docs/stage-m5.17a-action-class-random.md)、
[M5.16](docs/stage-m5.16-execution-bundle.md)、
[M5.16R](docs/stage-m5.16r-legacy-removal.md)、[架构](docs/architecture.md)、
[Control Runtime v2](docs/control-runtime-v2.md) 和 [总体规划](docs/ConsensusAtlas-总体规划.md)。
