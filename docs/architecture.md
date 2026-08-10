# Architecture

## 当前信任边界

```text
untrusted / bounded                         trusted Go core

single model call -> strict Guarded TestIntent -> deterministic macro compiler
                                                        |
                                                        v
                                             qualified policy invocation

Protocol docs ----> target Binding -----------------> Qualification
                     + Semantic Mapping                       |
                     + WorkloadRouter                         |
                     + DecisionProjector                      v
qualified policy + opaque workload ---------------> Experiment admission
                                                           |
opaque workload ------------------------------------------>|
                                                           v
                                                Control Runtime executor
                                                  |      |       |
                                                Trace  Snapshot  Evidence
                                                  +------+-------+
                                                         v
                                                  ExecutionBundle
                                                         |
                                         +---------------+--------------+
                                         v                              v
                                  TraceIntegrity                 DecisionProjector
                                                                        |
                                                                        v
                                                                    Agreement
                                         +---------------+--------------+
                                                         v
                                               evaluation ledger
```

未来 Agent/模型只能产生受 schema 约束的 Guarded TestIntent，且不能：

- 构造 enabled set 或任意 ActionID；
- 直接调用 Adapter/SUT；
- 修改当次 Qualification、投影器、Oracle 或预算；
- 写入 PSS/Defect ledger 或决定 verdict。

目标专用信任根由薄 Adapter、Semantic Mapping 和 DecisionProjector 构成。它们可以理解协议 Evidence，
但通用 Runtime、Experiment、Core PSS ledger、Oracle 和 evaluator 不导入协议类型。

M5.18b0 实现宏观 compiler，不是在线 Action 排序器。`ProtocolKnowledgePack` 与 backend catalog
是 target-owned 冻结输入；`AgentSemanticView` 从它们和真实 Manifest/Qualification 重算，不包含
BuildID、candidate/control、root cause 或 Oracle identity。Agent proposal 只能表达 risk ID、hard
capability/ActionKind/fault/budget 和 backend/ActionKind preference。

compiler 不接收 ActionID：先机械过滤 hard constraints，再用冻结 rank 处理 preference miss。
执行前消费者从原始可信输入重新编译 plan，执行后再要求每个 hard ActionKind 实际出现在
Trace。etcd/raft composition 只将选中的 strategy/seed 传入既有 `etcdraftExecution`，没有新 Runtime
或 scheduler。

M5.18b1 在这一入口上增加单次 DeepSeek JSON transport 和 `AgentInvocationAudit`。provider 只能看到
AgentSemanticView，调用账本只保存精确 prompt/request/response digest、非秘密 metadata、token 和
后续可验证工件 identity。Agent view 现在还显式公开 backend `max_fault_envelope`，避免要求模型
猜测它必须填写的边界。transport/response/intent/compile/execution 失败分类记录，不会自动
fallback 到人工计划。

M5.18b2 增加 `AgentBatchFeedbackView/v1`。它不信任单独的 MethodObservation JSON，而是要求
每个 backend 同时提供完整 bundles 和绑定 PSS Mapper，重新验证后只投影共同预算、完成度、
成本和 coarse discovery。任何 bundle/trace/build/candidate/root-cause/Oracle/state-key 身份都不进入
Agent 视图。有/无 feedback 消融必须保持所有 hard intent 和预算不变，反馈只能改变 preference。

M5.18b3 增加可比较的 `AgentBatchFeedbackView/v2` 和 `AgentFollowUpSpec/v1`。v2 把框架 execution
状态与 workload completed/pending 分开，禁止用不同终止语义的方法输入驱动选择。follow-up spec
冻结 source seeds、未见 seed、source 实际成本、per-arm ceiling 和 deterministic baseline rule；
source 可以物理复用，但每个 arm 仍承担全部逻辑成本。实际 follow-up seed 由该 spec 与 report seed
共同验证，Agent 仍不能控制它。

M5.18b4-pre 不修改 Runtime，而是修正 Agent/Experiment 之间的真实边界。新 b4 catalog
区分 Runtime-supported、Experiment-producible 和 backend-selectable；没有 FaultProvider 时不对
Agent 声明 Partition/Heal。`CompiledIntentPlan/v2` 与 policy seed 解耦，后者由
`IntentExecutionInstance/v1` 绑定。对已验证 report/bundle 的 hard Action 缺失生成
`IntentOutcome{execution=valid,intent=not-reached}`，不再伪装成 execution invalid。历史 v1 plan
和 b3 summary 保持原 identity。

M5.18b4 request-freeze 将 model request 构造与 transport 分开。`deepSeekPreparedRequest` 保存精确
messages/request bytes 及 digest；协议无关 `AgentPreferenceAblationFreeze` 绑定 semantic view、
feedback、hard baseline、FollowUpSpec、transport 限制和两臂 commitment。key 只能在 freeze 验证后进入
`invokePrepared`，不参与 request 生成或 identity。

M5.18b4 pair runner 只在 etcd/raft composition root 组织上述对象。它先拒绝已存在的 artifact
directory，完成 source construction 和 full freeze validation 后才读取 key，然后固定消费两臂。
typed arm failure 作为数据先进入 pair ledger 和全新目录，再作为进程错误返回。runner 不是
Campaign Coordinator，也不处理进程被强制终止时的中间 arm 恢复。

M5.19 在 `internal/controlexperiment` 增加协议无关 Campaign 数据面，但不增加执行器。
`CampaignConfig` 冻结 target/spec identity、逻辑预算和 wall-clock 运维上限；每个 terminal attempt
引用独立 artifact digest 并携带完整 WorkLedger。checkpoint 采用增量 hash chain，每项只保存一个
新 record、累计 totals 和 previous digest，避免重复完整历史。M5.19 的内存对象只验证链与 resume
identity；M5.19a 在不改变对象 identity 的前提下增加文件落盘。

M5.19a 将该链落到全新 Campaign 目录。完整恢复返回带私有校验令牌的 `CampaignRecovery`；只有该对象
可以核对当前磁盘 head 后续写。artifact 采用 SHA-256 内容寻址，先 file sync 和 no-replace link，
checkpoint 随后以相同方式提交。恢复重新验证全部 config/checkpoint/artifact，orphan 和 pending
只作为运行诊断，不进入 head、WorkLedger、PSS 或 verdict。M5.19a 明确限定 single-writer，尚未包含
Coordinator；M5.19b 在下一层补齐该循环。

M5.19b 增加唯一的协议无关 Campaign Coordinator。它从可信恢复状态机械计算 remaining allowance，构造
绑定 config/head/ordinal 的 request，并只接受 provider 返回的 terminal outcome、WorkLedger 和 opaque
artifact。attempt identity、digest、累计账本和停止原因仍由可信层产生。Coordinator 最终调用 M5.19a
的 `CommitAttempt`，不直接访问 Adapter 或复制 Runtime。M5.19c 增加的 `failure.json` 只绑定
config/head/request 和稳定分类，使 artifact-less error 跨进程不可重试；它不伪造成本或 terminal
attempt。第一个真实 etcd/raft provider 只存在 composition root，它冻结 strategy/seed/artifact schema 并
调用现有 `etcdraftExecution -> ExecuteQualifiedBundle`。通用 Campaign 包仍不导入协议类型。

M5.19d 在这条链上增加读取面，不增加执行面。协议无关 `CampaignSummary/v1` 只从完整
验证的 `CampaignRecovery` 派生，它索引 terminal records/checkpoints/totals/status，不嵌入
artifact。`ReadAttemptArtifact` 使用同一私有恢复令牌限制 committed ordinal，然后重验文件和
digest。etcd/raft runner 只组装 spec/provider/config/Coordinator/Summary，新运行和 resume 使用同一
executor。Summary 的运行状态不是协议 verdict，跨 attempt 观测留在后续 target-owned projection 层。

M5.20 实现上述 projection 层，M5.21b 将其升级为 v2，仍不增加执行器。etcd/raft composition root 只能通过
committed-ordinal reader 取 artifact，重验严格 JSON、request/spec/target/record identity、decision
projection 和 Core PSS mapping，再将协议无关 attempt projection 交给通用
`CampaignObservation/v2`。Observation 只保存 terminal/cost、唯一 PSS witness/曲线、
fault/workload 合计和 monitor 触发索引；Report、Bundle 和 Trace 仍只在内容寻址 artifact
中保存一次。v2 额外投影已重验的 choice/intent/plan/backend，不暴露 seed 或
instance。这些栏不合成单一分数，monitor 零触发不表示正确。

## 唯一执行路径

`internal/controlexperiment` 当前只保留 qualified workload/Experiment v2、action-class random 和 trace mutation，
它们最终进入同一个 `execute` 和 `executeRun`，每次 primary 后使用 fresh Adapter/Runtime 做严格
replay。`ExecuteQualifiedBundle` 不是第二套执行器，只打开 capture，把同一次执行已经产生的数据组织成
bundle。M5.10–M5.13 的 pre-admission fixed/random/Planner 在线入口已在 M5.17bR2 删除；历史工件保留。

```text
Config + ExecutionAdmission + Qualification
                 |
                 v
        Adapter Manifest recheck
                 |
                 v
       prepare workload through Runtime Offer
                 |
                 v
 Runtime.EnabledActions -> admissible -> policy -> Select
                 |
                 +--> Online Core PSS sample
                 +--> full trace/evidence
                 +--> client result
                 |
                 v
           fresh strict replay
```

当前 Workload Provider 通过 target-owned `WorkloadRouter` 解析输入目标，PSS Mapper 只负责
状态投影。Router 只返回 logical time 和候选节点；通用层不理解 leader/term/view。已进入
Trace 的每个 Invoke 都在 fresh replay 中用当时 Evidence 重算，必须再次得到与冻结 Action
相同的唯一目标。`PreparationRecord` 继续绑定 Offer 前后状态，避免把 scheduler 之外的
状态改变遗漏在证据链外。

图中的 admissible 层已在 M5.17c0 实装：Runtime enabled 只描述机械可执行，Experiment 再统一
施加 FaultEnvelope 等冻结限制。所有策略面对同一 canonical admissible frontier，v2 报告
分别保留 Runtime-enabled digest、admissible digest 和 selected ActionID。

## 协议无关公共层

- `internal/control`：Action、ProducedItem、Manifest、opaque payload/evidence envelope；
- `internal/controlruntime`：状态、enabled、选择、消息/时间/生命周期/effect、trace/replay；
- `internal/controlentropy`：node/incarnation/domain 隔离的随机流和 tape；
- `internal/conformance`：外部行为见证、Qualification 与 capability 状态；
- `internal/controlexperiment`：admission、policy、workload、fault budget、report、ExecutionBundle；
- `internal/psscore` 与 `internal/protocolstate`：固定 Core IR、采样和 discovery；
- `internal/semantic`：最小 decision observation 接口；
- `internal/oracle`：不理解具体协议的可信 monitor；
- `internal/defectbench`：control/candidate 预算与结果账本；
- `internal/sutbuild`：source-bound 构建变体和审计。

## 目标专用层

`adapters/etcdraftv2` 调用官方 `go.etcd.io/raft/v3` 的 RawNode API，处理 Ready、durable/apply effect、
Tick、Step、crash/restart 和 Evidence。它同时提供 Core PSS Mapper、WorkloadRouter 与
DecisionProjector；协议类型不会
越过 Adapter 包。

`adapters/hashicorpraftv2` 使用相同公共 Action/Runtime，已验证消息、生命周期和 opaque invoke，但因
官方实现内部墙钟、包级随机和 goroutine 调度未受框架完整控制，只获得部分 Qualification。统一 Action
表示上层语义统一，不表示两个实现拥有相同的控制强度或 strict benchmark 资格。

新目标的边际产物仍限制为：Execution Binding、Semantic Mapping、WorkloadRouter、DecisionProjector、fixture 与
qualification composition。若接入非 Raft 协议必须修改 Runtime/Action/Core PSS schema，应先视为抽象
失败，而不是增加协议 type switch。

## ExecutionBundle 与 Oracle

bundle 是 evaluator 的完整输入，包含完整 trace body，而普通 measurement report 只保存摘要。大型
bundle 默认写入 ignored `artifacts/`，checked-in evaluator report 只引用其 digest。

TraceIntegrity 检查证据链是否可信。失败的 trial 是 `invalid`，不能记为 candidate kill 或 control
false positive。Agreement 接收 target projector 输出的 exact position/value digest；PSS/Coverage 不
参与 verdict。

M5.18a 的 v3 bundle 是显式 opt-in evidence envelope；默认入口仍产生冻结 v1/v2。v3 额外绑定
MethodSpec digest 和 OperationHistory。OperationHistory 内含完整 WorkloadPlan，并从 Trace Invoke、
opaque Action input、ClientHistory 与 WorkloadRunReport 重建 invoke/return interval；它不是由 Agent 或
Adapter 自报的 operation 列表。

正式 evaluator v2 不接受预制 bundle 作为 kill authority。composition root 校验精确 build-audit 文件和
binary digest，从已验证 bytes 创建隔离可执行副本，再用 MethodSpec 参数 fresh execution。通用
`internal/defectbench` 只验证 MethodSpec/Config projection/build evidence/bundle/预算并运行 monitor；
etcd/raft projector 仍只在 CLI composition root 注入。

本轮 applied-prefix calibration 的两侧 OperationHistory 相同，差异仍由 target-owned applied-prefix
projection 加 generic Agreement 检出。因此没有添加无实际证据缺口的通用 durability monitor。

## 强基线选择边界

M5.17a 的 action-class random 不增加 Runtime backend。选择器读取唯一 `EnabledActions`，按 ActionKind
分组后均匀选择 class 和成员。冻结 FaultEnvelope 只生成该策略的 selectable 子集，不修改 Runtime
enabled、ActionID 或状态；`Invoke` hard priority、envelope、policy seed 和完整 Config 都进入 bundle
identity。

benchmark manifest 可按 variant 绑定 `ExpectedConfigDigest`。这使 control/candidate 除 build identity
外还必须使用清单指定的 workload、policy、fault envelope、runtime seed 和 admission；不匹配的 trial
只能记为 `invalid`。

M5.17b trace mutation 仍是同一个 policy boundary。通用 operator 只接收一条已验证 Trace 和 source
Policy，产生 digest-bound splice plan：精确前缀、相邻 swap、显式 priority suffix。Runtime 独立判断
每个引用 ID 是否 enabled；不可执行时返回带 partial work 的稳定失败，不允许 selector 自行换一个 ID。
具体 composition 可以冻结 pair 选择规则，但不能把多个尝试中“方便成功”的一个冒充单次试验。

mutation report 的 work 只覆盖 mutation execution。方法级比较必须另行加上 source corpus 的构造、
setup 和 replay；M5.17b 当前实测口径为 source 98/98 + mutation 98/98 = 196/196。公开失败校准的
35 primary work 独立记录，不混入方法 trial。

M5.17c1 将这个口径变成机器可校验的数据结构。`MutationSourceCorpus` 是有序、digest-bound 的已验证
bundle identity 集，不要求 source 必须来自某一种固定策略；operator 的 suffix 单独声明。mutation v2
使用 `(ActionID, occurrence)` 引用源 Trace，解决同一动作在状态循环中重复出现的歧义，同时不改写 v1
冻结身份。

`PSSFeedback` 不能由搜索器提交 state key。可信构造器从 bundle 的最终 Snapshot、Trace transition 和
Evidence 重构每个采样点，重新调用 target-owned Mapper 和 Core PSS projector，并要求逐样本等于 bundle。
它仍然只是 coarse feedback，不是状态等价证明。`MethodLedger` 内嵌小型 corpus/feedback identity，要求
source 记录与 corpus 顺序一致、proposal 引用 source、execution 引用 completed proposal、feedback 引用
completed execution；失败尝试和 source/replay 成本不能被省略。

M5.17c2 的 admissible-uniform 先规范化 ActionID，再在共同 admissible frontier 上均匀采样；
只有刚准备的 Invoke 是 hard priority。`PSSGuidedMutationChoice` 是 batch 边界：它从 completed
source bundles 重投影 feedback，根据 source-exclusive states 和 global visits 冻结一个
occurrence-aware proposal，不在线改变 scheduler。`MethodObservation` 绑定 method ceiling、ledger、
PSS measurement 和可选 guidance，但它不是 evaluator verdict。首个 PSS-guided proposal 不可执行的
结果被保留，说明 PSS 稀有度不包含 Action 因果/可交换性。

## M5.16R 之后的依赖规则

旧 v1 Engine/Host/Driver、Raft Family、Coverage/Campaign、onboarding、旧 Agent 和 migration 源码已
删除。默认 `make test` 先运行 `audit-no-v1`，阻止旧 import 回流。历史文档/JSON 可以提到旧包，但当前
Go source 不得依赖它们。

依赖方向必须保持：

```text
cmd/qualifications -> adapters + trusted internal packages
adapters           -> official implementation + internal/control*
oracle/evaluator   -> generic bundle + semantic observation
internal/control*  -X-> adapters or consensus packages
```

## 当前未完成

- 非公开 holdout 与正式方法比较；
- PSS-guided 的可执行性结构约束（只在评测缺口证明必要时增加）；
- Guarded TestIntent batch feedback 与 one-shot/feedback/确定性 baseline 消融；
- 修正后的非锚定 prompt 真实调用；
- 第二个 strict deterministic target；
- Coverage v2 的真实新消费者；
- BFT 的有限 Byzantine action 与对应 target projector/Oracle。

这些功能必须复用当前 ExecutionBundle/evaluator，不得恢复第二套 Runtime、Campaign 或判定链。
