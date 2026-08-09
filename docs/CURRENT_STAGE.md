# 当前阶段

日期：2026-08-09

阶段：M5.19b Deterministic Campaign Coordinator 已完成；不运行 SUT 或模型

## 输入、处理、输出

```text
输入
  trusted CampaignRecovery + bounded provider
  + CampaignConfig logical/wall-clock ceilings
                         |
                         v
处理
  mechanical remaining allowance
  -> digest-bound request(config/head/ordinal/allowance)
  -> provider(context with remaining wall deadline)
  -> terminal result/outcome/work/artifact validation
  -> M5.19a artifact-first checkpoint commit
  -> repeat Step until mechanical stop
                         |
                         v
输出
  recoverable multi-attempt Campaign + exact WorkLedger totals
  + attempt/logical/wall-clock stop reason
  + failed attempt evidence and no automatic provider retry
  + no real SUT provider or model call yet
```

M5.19b 新增协议无关 `CampaignAttemptRequest`、remaining allowance、provider contract 和最小
`CampaignCoordinator`。确定性见证在 attempt 1 后恢复目录，继续完成 attempt 2/3；其中 terminal failed
attempt 仍保留 artifact 和成本，最终由 attempt limit 机械停止。普通 provider error 会锁死当前
Coordinator 但尚无跨进程 durable failure marker，因此不能将该目录视为正式完整结果。

M5.19a 将协议无关 Campaign 数据面落为 crash-safe 目录，不是第二套 Runtime。只有创建或完整恢复返回的
`CampaignRecovery` 才能提交下一 attempt；artifact durable 在前，checkpoint durable 在后。中断留下的
orphan/pending 不进入可信 totals。正常连续提交为 O(n) 总 I/O，重启时执行一次 O(n) 完整恢复。

M5.19 基础中 wall clock 只决定运维停止，
attempt、primary scheduler decisions、primary/replay work 和 model calls/tokens 才进入权威逻辑预算。
checkpoint 不复制完整历史；每项只保存当前 record、累计 totals 和 previous digest。

M5.17a/b 没有增加第二套执行器。action-class random 在 M5.16 的唯一 bundle 路径上均匀选择
ActionKind，再在该 class 内选择 Runtime 当前提供的 Action；trace mutation 精确重放源 Trace 前缀、
交换首对相邻 message deliveries，再使用声明的确定性 priority suffix。两者都不能修改 enabled 或
伪造 ActionID。

M5.16 没有引入 Agent、Coverage 或第二套执行器。`ExecuteQualifiedBundle` 复用 M5.15 的唯一执行路径，
只把该次运行中已经产生的完整 Trace、Evidence、Snapshot、Core PSS、client result、Qualification 和
成本组织成自校验 `ExecutionBundle`。

M5.17c0 没有修改 Control Runtime。Experiment v2 把 decision budget 解释为上限，统一所有 Policy
看到的 admissible frontier，并把 Router、actual workload result 和终止原因纳入 fresh replay。

M5.17c1 同样没有修改 Control Runtime。它补齐 corpus 搜索前的数据可信性：source 顺序参与 identity，
重复 ActionID 使用 occurrence 引用，PSS feedback 必须从 bundle 机械重投影，MethodLedger 强制计入
source、proposal、失败和 execution。

M5.17c2 仍未修改 Control Runtime。uniform 和 PSS-guided 都使用同一 qualification/admissible
frontier；PSS 只在 source batch 完成后冻结一个 proposal，不介入在线调度。

M5.18b3 也没有增加执行器。source fixture 只在同一测试进程中复用已验证的不可变结果，逻辑账本仍为
每个 arm 计入全部 source construction；follow-up 最终仍进入现有 qualified executor 和 fresh replay。

M5.18b4-pre 仍没有修改 Control Runtime。它在 composition/catalog 层区分 Runtime-supported、
Experiment-producible 和 backend-selectable，并用新的 plan/instance/outcome 身份组织同一执行路径。

M5.18b4 request-freeze 也没有修改 Control Runtime 或执行 follow-up。它只把两份模型请求的
精确 bytes、可见信息差异、共同 hard baseline、seed、预算和 transport 上限在调用前冻结。

M5.18b4R 只调整验证拓扑。`cmd/control-experiment` 的全部顶层测试已由版本化清单与
`go test -list` 机械对账，并按共享 fixture 分入独立 race test binary；它不修改任何实验语义或冻结身份。

实际清单为 24/24、重复 0。method/execution/agent shard 分别以 353.374/128.527/88.508 秒通过，
其余 package race 全部通过且没有 data-race 报告；历史单 test binary 超时保留为失败记录。

M5.18b4 consumer 没有重新生成 request。它按 arm ID 取得冻结 prepared bytes，核对 transport 和
commitment 后只调用一次 `invokePrepared`，再复用现有 audit、strict parser、baseline validator、
plan v2、seed-4 instance、qualified executor、projector 和 IntentOutcome。当前清单增加为 25 项；
离线成功、越权拒绝、transport failure 和 request tamper 均通过，真实模型调用仍为 0。

M5.18b4 pair orchestration 也没有新增执行器或状态机。它固定按 no-feedback/with-feedback 各调用一次
上述 consumer；第一臂失败仍会执行第二臂。pair ledger 绑定共同 freeze、两份 invocation audit、
可选 instance/outcome digest 和每臂完整 source/post-freeze/model 成本；大型工件独立写入全新目录。
篡改 ledger、目录覆盖、key 落盘和 transport 私有诊断泄漏都有离线拒绝见证。当前 exact-once 清单为
26 项，真实模型调用仍为 0。

M5.18b4 pair runner 只是 composition 入口。它先拒绝已存在目录，再构造和完整复核 freeze，
随后才读取用户显式指定的 key。离线见证同时产生第一臂 transport failure 和第二臂
baseline rejection；runner 把两份 audit 和成本落盘后才返回失败。CLI 和 Make target 不被任何
测试目标依赖，本阶段没有读取真实 key 或调用模型。

## 为什么需要显式 preparation

M5.15 的 `OfferInvoke` 会在 scheduler decision 之前改变 Runtime 的 offered state，但旧 trace 只记录
`Select`。第一次运行 TraceIntegrity 时，这个缺口导致 state-digest chain 无法闭合。M5.16 没有忽略
该差异，而是增加 `PreparationRecord`：记录 ordinal、下一 decision、冻结 Action、before/after state
digest 和自身 digest。Oracle 现在同时校验 prepare 与 scheduler record 的状态链，并要求刚准备的
Action 立即被选中。

## DecisionProjector 信任边界

通用 Agreement 只接收：

```text
step + participant + decision position + exact value digest
```

协议或实现专用 Evidence 的解释留在可信目标 composition。etcd/raft 使用
`official-etcdraft-v2/applied-prefix-digest-v1`，把每个节点的 applied index 与累计 application digest
映射为上述记录。`internal/oracle` 不导入 Raft 类型，也不解码 Raft Evidence。

## 公开 calibration 结果

| trial | BuildID | decisions | primary/replay work | 结果 |
|---|---|---:|---:|---|
| official control | `go.etcd.io/raft/v3@v3.6.0` | 96 | 98 / 98 | `control-pass` |
| command-data calibration | `sut-c9811ab0ed8e2f39` | 96 | 98 / 98 | `killed` at step 55 |

两份 bundle 都通过 TraceIntegrity。candidate 在 decision position 5 出现 n1/n3 exact value digest
冲突，被 Agreement 检出。最终账本：1 control、0 false positive、1 candidate、1 killed root cause、
0 invalid。PSS/Coverage 没有参与 verdict。

公开 candidate 由固定 `go.etcd.io/raft/v3@v3.6.0` module cache 的 `raft.go` 唯一文本转换产生；构建使用
offline/readonly module、固定命令 allowlist、`-trimpath` 和 linker-bound opaque BuildID。build audit
记录原始/变换源码、module、binary、command 和 toolchain identity，并确认 module cache 未修改。

这是公开 calibration，只证明构建、执行、投影、Oracle 与 evaluator 链路能区分一个已知构造差异；
不是历史 holdout，不支持 Agent 方法优势结论。

## M5.17a 实际结果

action-class random 使用同一 `single-write-v1` workload、FaultEnvelope、96 decisions 和 98 primary/replay
work，冻结 seed 为 1，不筛选 seed。官方 control 发现 83 个 Core PSS 状态（prefix area 3681），完成并
提交 1 个 workload；轨迹包含 crash/restart 1/1、drop/duplicate 2/1、deliver 17、temporal 27 和
effect 46，strict replay、TraceIntegrity 与 Agreement 全部通过。

同一公开 command-data candidate 在本策略下 `survived`，而 M5.16 fixed workload 在只发现 56 个 Core
PSS 状态时于 step 55 将其检出：

| policy | Core PSS states | candidate verdict |
|---|---:|---|
| fixed workload | 56 | `killed` |
| action-class random seed 1 | 83 | `survived` |

这是应当保留的负结果：更多 coarse PSS 状态不等于覆盖到某个根因所需的时序。单一公开 candidate
不能用来排序方法，但已经机械否证“状态发现数本身就是最终测试质量”的解释。

action-class benchmark 还分别绑定 control/candidate 的完整 config digest，策略、workload、
FaultEnvelope 或 admission 不同的 bundle 会记为 `invalid`，不能进入 verdict。

## M5.17b 实际结果

trace mutation 的公共选择规则固定为“按 source decision 顺序选择第一对相邻 `deliver-message`”，本次
得到 decisions 46/47；没有尝试多个 pair 后筛选成功者。operator 使用 exact source prefix、adjacent
swap 和 digest-bound priority suffix。任一精确 ActionID 不 enabled 时返回稳定
`EXPERIMENT_TRACE_MUTATION_ACTION_NOT_ENABLED`，不静默 fallback。

官方 control 的 mutation 完成 96 decisions、1 个 committed workload 和 strict replay，发现 56 个
Core PSS states、prefix area 3324；source 数值也为 56/3324，但两条 trace digest 不同。这里没有运行
candidate，所以没有 defect verdict。

成本不能只看 mutation report 内的 98/98：生成 source Trace 也用了 98 primary / 98 replay。因此本次
baseline method 的真实成本是 **196 primary / 196 replay**。另一个公开依赖性失败校准在 decision 34
明确拒绝，记录 33 已完成 decisions、35 primary work、0 replay；它只验证失败账本，不是 method trial。
checked-in 小型 summary 由集成测试逐字段重算。

## M5.17bR2 删除结果

审计确认 M5.12/M5.13 Planner 仍调用 `ExecuteLegacy`，且 DeepSeek transport 引用已经由 M5.16R 删除的
Python 脚本。它们既不能进入当前 Qualification/Workload/FaultEnvelope/ExecutionBundle 边界，也不是
未来 Guarded TestIntent 的正确基础。本阶段因此删除旧 Planner Proposal/Attempt、DeepSeek transport、
`internal/modelcommand`、`ExecuteLegacy` 和 fixed/random/stub/deepseek 在线 CLI/Make targets。

当前 `cmd/control-experiment` 只保留：

- `workload`；
- `workload-semantics-v2`；
- `workload-action-class-random`；
- `workload-trace-mutation`。

四条路径都必须取得 Qualification，并进入唯一 `ExecuteQualified`/`ExecuteQualifiedBundle`。历史
M5.10–M5.13 Markdown/JSON 仍保留，但不再冒充当前可运行 API。新增
`make audit-no-retired-experiment` 阻止旧符号和已删除 transport 回流。

## M5.17c0 实际结果

Experiment v2 新增 `SelectionAudit`，分别绑定 Runtime-enabled digest、FaultEnvelope 过滤后的
admissible digest 和 selected ActionID。priority、random、action-class random 与 trace mutation 共用
同一过滤函数，不再为某个随机策略单独改变可选集。

`WorkloadRouter` 是 target-owned 协议知识，但只返回 logical time 和候选 NodeID。PSS Mapper 只做
状态投影。fresh replay 会用每次 Invoke 之前的 Evidence 重算 Router，要求唯一候选与冻结
Action target 相同；还会从 Trace 重算 fault usage 和终止条件。

etcd/raft 官方 control 的两个新见证：

| decision 上限 | 实际决策 | 终止 | workload | Core PSS states | work primary/replay |
|---:|---:|---|---|---:|---:|
| 96 | 42 | `configured-stop` | 1 committed | 31 | 44 / 44 |
| 1 | 1 | `budget-exhausted` | 1 pending，`no-candidate` | 2 | 2 / 2 |

另有协议无关 fixture 真实形成 0-decision `quiescent` 并严格重放。`ExpectedStatus` 在 v2 中不再
是 Oracle；actual `rejected` 或 pending 可进入证据。v1 冻结语义仍要求完整完成且状态匹配。

## M5.17c1 实际结果

`MutationSourceEntry` 绑定已验证 bundle 的 report/config/trace/manifest/PSS/policy/qualification 身份；
`MutationSourceCorpus` 的显式顺序参与 digest。mutation v2 以 `(ActionID, occurrence)` 引用源 Trace，
而 v1 冻结身份继续保持。`PSSFeedback` 从最终 Snapshot 反向重构 Core PSS 所需控制状态，再使用每步
Evidence 和绑定 Mapper 重投影；调用方不能提交 state key。

真实 etcd/raft 见证按公开 first-adjacent-delivery 规则从一个 96-decision source 生成 proposal：

| 项目 | 结果 |
|---|---:|
| mutation decisions | 96 |
| PSS samples / states | 97 / 56 |
| source primary / replay | 98 / 98 |
| execution primary / replay | 98 / 98 |
| MethodLedger total primary / replay | 196 / 196 |
| report / source bundle / mutation bundle / method JSON | 272,673 / 1,902,046 / 1,902,828 / 15,086 bytes |

report、bundle、plan、corpus、feedback 和 ledger digest 分别为 `471d3066...8c8a6ab`、
`0019ec7d...a9f303`、`b3c716e5...f2b63`、`1f93dd84...9f600`、`9b47d8a9...d52a` 和
`7a993f97...95a08`。本阶段没有 candidate 或 defect verdict；单 source 见证不等于多策略 corpus 已经
有效。

## M5.17c2 实际结果

qualified admissible-uniform 按 canonical ActionID 在共同 frontier 上均匀选择，只对准备好的
Invoke 保留 hard priority。seeds 1/2/3 都完成 96 decisions 和 strict replay；分别发现
82/84/90 states，按运行顺序新增 82/75/81，method union 为 238。总成本为
294 primary / 294 replay。

PSS-guided 使用与 uniform 逐字节相同的前两个 source bundle。可信 feedback 在 4 个相邻
message pair 中选中 source 2、decision 65：该 source 有 75 个 exclusive states，目标 pre-state
global visits 为 1。mutation 在 decision 66 因第二个精确 ActionID 不再 enabled 失败；
没有 fallback、mutation bundle 或 mutation feedback。账本保留 263 primary / 196 replay，source measurement
为 194 samples/157 states。

| method | ceiling | actual primary/replay | completed bundles | method states | 结果 |
|---|---:|---:|---:|---:|---|
| admissible uniform | 294/294 | 294/294 | 3 | 238 | completed |
| batch PSS-guided | 294/294 | 263/196 | 2 | 157 | proposal execution failed at 66 |

`238 > 157` 不是等成本方法优劣结论：guided 没有完成第三条可测 trace，两方也都没有
candidate/control verdict。当前只证明 PSS 稀有度不足以推导 Action 因果/可交换性。

## M5.18a 实际结果

ExecutionBundle v3 是显式 opt-in：旧入口继续生成冻结 v1/v2，v3 另行绑定
MethodSpec digest 和可从 Trace/Workload/ClientHistory 重建的 OperationHistory。它们复用唯一
`execute`，没有修改 Runtime/Action/Trace/PSS。

首个 MethodSpec 冻结 fixed policy、96 decisions、98/98 ceiling、PSS/projector、Config projection 和
evidence schema。evaluator 不接受 submitted bundle；它验证 build audit/binary，从已验证
bytes 创建隔离副本，亲自运行 control/candidate 并 fresh 重算 Oracle。

| trial | decisions | primary/replay | operation | 结果 |
|---|---:|---:|---|---|
| official control | 96 | 98/98 | invoke 28 / return 42 | `control-pass` |
| public command-data candidate | 96 | 98/98 | invoke 28 / return 42 | `killed` at step 55 |

两边 Config projection 都是 `8459b8a7...a73e92f`，OperationHistory 都是
`778aae5d...1c7735`。客户都观察到 `committed`；candidate 由 target-owned applied-prefix projector +
generic Agreement 在 position 5 检出。最终为 1 control、0 false positive、1 killed public
calibration root cause、0 invalid。连续两次 evaluator-owned run 产生相同 report digest。

本轮没有增加没有实际证据缺口的 durability monitor；当前 applied-prefix 差异已由既有
projection/Agreement 覆盖。这是单 execution 公开 calibration，不是多 attempt 方法比较。

## M5.18b0 实际结果

M5.18b0 建立了不调用模型的宏观 intent 边界。`ProtocolKnowledgePack` 的文本只供 Agent
理解，compiler 只使用 typed risk/capability/ActionKind/backend。`AgentSemanticView` 由真实
Manifest/Qualification 重算，不保存 BuildID、candidate/control、root cause 或 Oracle identity；替换
opaque BuildID 后 view digest 保持不变。

严格 proposal parser 拒绝未知字段、尾随 JSON 和提交者自算 digest。hard constraints 在
Qualification/Manifest/backend/decision/FaultEnvelope 任一层不成立时直接拒绝；执行后还要求
每个 hard ActionKind 真实出现在 Trace。preference miss 不改 hard set，只影响确定性排序。

公开 one-shot 见证对不存在的 `dpor` backend 记录 miss，从 2 个 hard-eligible backend 中用冻结
fallback 选中 action-class random；compiler work 为 2 candidate + 2 preference checks = 4。执行仍为
96 decisions、98/98 primary/replay，invoke/deliver/temporal/crash/restart 均实际出现并通过 strict replay。
report/bundle digest 与 M5.17a 逐字节相同，证明没有第二套 scheduler。

## M5.18b1 实际结果

本阶段增加一个直接 Go transport，不恢复 M5.13 旧 Planner。客户端固定 official DeepSeek endpoint、
`deepseek-v4-flash`、JSON output、thinking disabled、temperature 0、1200 output-token ceiling、60 秒和无重试。
key 只从权限 0600 的非 symlink 普通文件读取，不进入 prompt/body/log/artifact/Git。

真实 prompt 预检发现 backend view 缺少 `max_fault_envelope`，模型无法不靠猜测地产生预算；该字段已在调用前
从可信 catalog 投影进 AgentSemanticView。它不增加 capability 或修改 Runtime，但更新当前 view/人工回归
intent/plan identity。

唯一真实调用完成 `model -> strict parse -> compiler -> existing qualified executor -> replay`：模型选择
`admissible-uniform`，96 decisions、98/98 primary/replay，1 个 workload committed，发现 82 个 Core PSS states，
invoke/deliver/temporal/crash/restart hard actions 全部真实出现。模型成本为 1 call、1568 input、211 output、
1779 total tokens；compiler 为 3 work units。checked-in summary 可以重编译冻结 intent 并逐字节重现
report `ab9270fa...ac60` 和 bundle `ba5cda6a...561b`。

必须保留的负面结果：accepted id、预算和 backend 与当时 prompt 的具体有效示例完全相同，说明 response
高度可能被示例锚定。本次只证明 transport/guarded execution plumbing，不证明 Agent 独立规划。代码已把
示例替换为 fictional 纯结构 JSON，但本阶段坚持一次调用、无筛选，因此修正后的 prompt 尚未调用。

## M5.18b2 实际结果

`AgentBatchFeedbackView/v1` 只接受通过完整 bundle + 绑定 Mapper 重投影的 MethodObservation。
Agent 只看到共同 ceiling、attempt 完成度、primary/replay/model 成本、PSS samples/states 和
new-state curve；不包含 bundle/trace/manifest/qualification/build/candidate/root cause/Oracle/state key。

真实 action-class 和 uniform batch 预先冻结 seeds 1/2/3，共同上限 3 attempts、294 primary、294 replay：

| backend | completed/failed | actual primary/replay | samples/states | new states |
|---|---:|---:|---:|---|
| action-class random | 2/1 | 294/196 | 194/165 | 83, 82 |
| admissible uniform | 3/0 | 294/294 | 291/238 | 82, 75, 81 |

action-class seed 3 在 decision 96 未完成 workload；该 attempt 成本保留、无替换 seed。Agent 视图只提供
`execution-failed`，完整账本才保存精确 `EXPERIMENT_WORKLOAD_INVALID`。feedback digest 为
`a7a6e7e9...8ab7c2`。本轮没有调用 LLM。

消融边界另外要求有/无 feedback intent 的 view、risk、decisions、hard capability/action 和
FaultEnvelope 完全相同；feedback 最多改 `prefer.backend_ids/actions`，不能扩大预算制造收益。

M5.18b3 复核确认该 v1 表格混用了终止语义：action-class 的旧 schema 把预算内 pending workload
转成 `EXPERIMENT_WORKLOAD_INVALID`，uniform 的 Experiment v2 则把同类 pending 保存为合法完整执行。
因此 v1 的 `completed/failed` 不可直接比较，不能驱动 backend 选择。历史 digest 与工件保持不变，
作为暴露这项问题的校准证据。

## M5.18b3 实际结果

`AgentBatchFeedbackView/v2` 分开报告框架 execution 状态和 workload
`planned/completed/pending`。action-class-v2 与 uniform 都使用 Experiment v2、seeds 1/2/3 和共同
3 attempts / 294 primary / 294 replay ceiling：

| backend | execution | workload completed/planned | actual primary/replay | samples/states | new states |
|---|---:|---:|---:|---:|---|
| action-class random v2 | 3 complete | 2/3 | 294/294 | 291/241 | 83, 82, 76 |
| admissible uniform | 3 complete | 2/3 | 294/294 | 291/238 | 82, 75, 81 |

两者各有一个 pending workload。确定性规则先比较 workload 完成比例，再比较 execution 状态，完全相同
时按 canonical backend ID 选中 action-class。预先冻结且不与 source 重叠的 seed 4 实际消耗 97/97
work、通过 strict replay、发现 79 states，但 workload pending 且没有覆盖全部 hard ActionKind。因此
结果以 `EXPERIMENT_COMPILED_INTENT_HARD_ACTION_MISSING` 保存为计费的 `execution-failed`，没有替换
backend 或 seed。source 实际 588/588，合计实际 685/685，低于 per-arm ceiling 686/686；模型调用为 0。

source fixture 复用把普通 package 测试降到约 39 秒，但没有减少方法成本。`241 > 238` 和 seed 4 的
79 states 都不是缺陷 verdict、覆盖完整度或方法优越性证据。

## M5.18b4-pre 实际结果

新 Agent view 仍显示 Manifest 声明的 Runtime-supported Partition/Heal，但两个可选搜索
backend 的 `supported_actions` 已删除二者，partition envelope 为 0；fixed-progress 的
全部 fault envelope 为 0。hard Partition 在 compile 阶段就被机械拒绝。

`CompiledIntentPlan/v2` 不再序列化 policy seed，`IntentExecutionInstance/v1` 单独绑定
seed 4 和 98/98 ceiling。修正后的 action-class execution 实际消耗 97/97 work、strict replay
稳定、发现 79 states，但 Trace 没有 `invoke`。新结果为：

| axis | result |
|---|---|
| execution | `valid` |
| intent | `not-reached` / `INTENT_REQUIRED_ACTION_NOT_REACHED` / missing `invoke` |
| oracle | `not-evaluated` |
| model | 0 calls |

这只是 public trust-boundary calibration，不是 RiskWitness、defect verdict 或 Agent 效果结论。

feedback 的六条 source 物理复用 b3 冻结 bundle，其历史 Config 仍记录 partition 上限 1；
因 composition 没有 FaultProvider，新增回归逐条确认 Trace 中无 Partition/Heal。b4 follow-up
的 Config/catalog 则都是 0。这是减少重复执行的证据复用，不是 source/follow-up Config identity 相同声明。

## M5.18b4 request freeze 实际结果

两臂使用同一 system prompt 和结构化 input builder。no-feedback 臂的
`agent_batch_feedback` 是 JSON `null`；with-feedback 臂是从六条绑定 source bundle 重算的
feedback v2。其他 view、risk、must、seed 4、98/98 execution ceiling、transport 参数完全相同。

| arm | prompt/request bytes | request digest |
|---|---:|---|
| no-feedback | 6,446 / 6,608 | `562d3bda...31ce5c` |
| with-feedback | 8,800 / 8,962 | `eb44d738...f6826f` |

request-freeze digest 为 `2591cf89...eed8c1`，follow-up spec 为 `bf632a68...af262`，hard baseline 为
`8261d484...79c05`。已知 b3/b4-pre report/bundle/outcome 身份和结果词没有进入请求。
本阶段 model calls = 0，没有 Agent proposal 或 execution result。

## M5.16R 删除结果

删除前冻结基线：

- 29,931 行 production Go/Python；
- 12,559 行测试；
- 42,490 行总计；
- 28 条 legacy production import edge；
- 六个 v1 核心包约 2,734 行生产代码、1,210 行测试；
- 直接 legacy dependency cone 约 7,047 行生产代码、2,257 行测试。

M5.16R 删除完成时可编译 Go：

- 14,564 行 production；
- 6,299 行测试；
- 20,863 行总计；
- legacy production import edge = 0。

加入 M5.17b 后为 15,136 行 production、6,659 行测试。M5.17bR2 又删除 1,068 行 production 和
427 行测试，降为 14,068/6,232。M5.17c0 增加 Experiment v2 语义和见证后为
14,603/6,466，合计 21,069 行。M5.17c1 增加 corpus/feedback/ledger 可信数据面和真实见证后为
15,781/6,715，合计 22,496 行。M5.17c2 增加两个真实方法、guidance 和 method observation 后为
16,934/6,852，合计 23,786 行。M5.18a 增加 OperationHistory、MethodSpec 和 fresh evaluator 后为
17,824/7,106，合计 24,930 行。M5.18b0 增加五个 canonical intent 工件、输入重算和真实执行见证后为
18,862/7,282，合计 26,144 行。M5.18b1 加入通用调用审计、一个 provider transport、CLI 与真实/失败
见证和 hard-action/fault-envelope 前置检查后为 19,562/7,731，合计 27,293 行，净增
700 production / 449 tests。M5.18b2 加入 feedback 重验证、action-class 失败 attempt 账本、目标
composition 和见证后为 20,405/7,888，合计 28,293 行，净增 843 production / 157 tests。
M5.18b3 加入可比较 feedback v2、follow-up spec/composition、source fixture 复用和见证后为
21,260/8,088，合计 29,348 行，净增 855 production / 200 tests。没有新 Agent、Runtime、PSS 维度、
Oracle 或 backend；Control Runtime 核心没有修改。
M5.18b4-pre 完成后为 21,963 行 production、8,284 行 tests，合计 30,247 行；相比
b3 净增 703/196。本阶段没有新 Agent、Runtime、PSS 维度、Oracle、Risk DSL 或 backend。
M5.18b4 request freeze 完成后为 22,447 行 production、8,497 行 tests，合计 30,944 行；相比
b4-pre 净增 484/213。新增代码集中在精确 request 构造、通用 freeze commitment、composition、
可信校验和回归；没有修改 Control Runtime、Adapter、PSS mapper 或 Oracle。

实际 Git diff 当前净删除超过 2.2 万行。旧 Engine/Host/Driver、Raft Family、Coverage/Campaign、旧
DefectBench、onboarding、旧 Python Agents、migration harness 和对应 CLI 均已删除；旧资格在线重放
入口也已删除。历史 Markdown/JSON 继续保留为不可改写记录，`make audit-no-v1` 阻止源码回流。

## 冻结身份

| 工件 | 内容 digest / 文件 SHA-256 |
|---|---|
| M5.15 report | digest `e680aabd...a18948d7`；file `e7a00826...ad247b1` |
| official bundle | digest `6120cadb...8fcdf63`；file `efa3e9ae...3f53ba` |
| candidate bundle | digest `0c3d3cd8...30f0943`；file `35f1e7f7...f03091` |
| build input | file `1485c6e8...077b8` |
| build audit | file `7c63ec00...05238`；binary `a66c09ad...698a9` |
| evaluator manifest | digest `0ca75d75...dc17ad` |
| evaluator report | digest `fd4ecc9b...71e664`；file `9be0f831...3a96` |
| M5.17a control bundle | digest `b766e3f1...6c9b1`；file `7ceb497a...761889b` |
| M5.17a build audit | file `b9f47577...e5301c6`；binary `efd64480...0c4b8` |
| M5.17a evaluator manifest | digest `b132e297...ff838` |
| M5.17a evaluator report | digest `3886388d...8cb68`；file `083beda6...8b0b` |
| M5.17b mutation report | digest `e4a5a5eb...df154`；file `e13937a9...748ab` |
| M5.17b mutation bundle | digest `29168463...d87`；file `37b9665c...dcdeb` |
| M5.17b summary | file `96b447e5...53ea01` |
| M5.17c0 completed | report `9411bb31...51245b0d`；bundle `ed44b5be...4155bf69` |
| M5.17c0 pending | report `1570598d...2e0bff6`；bundle `28d271c1...30fc071` |
| M5.17c1 corpus mutation | report `471d3066...8c8a6ab`；bundle `0019ec7d...a9f303`；ledger `7a993f97...95a08` |
| M5.17c2 uniform method | observation `90fb30c3...deff75`；294/294；238 states |
| M5.17c2 PSS-guided | observation `8fd25881...86ecf1`；`ACTION_NOT_ENABLED@66`；263/196 |
| M5.18a MethodSpec | `ee856fbf...8fdbcf`；Config projection `8459b8a7...a73e92f` |
| M5.18a evaluator | manifest `0bf5a38f...46bbce9`；report `4cac6cd0...7ef4ee` |
| current Agent view | knowledge `1921bdc3...199980`；catalog `76ab1542...f89dd`；view `63ccd9fd...8b28d` |
| b0 current regression | intent `e18af54b...745b1`；plan `d98fc488...166d` |
| M5.18b1 real call | audit `101c8548...64e7e`；intent `1fe92d66...1de2`；plan `21e0a826...261` |
| M5.18b1 execution | report `ab9270fa...ac60`；bundle `ba5cda6a...561b`；82 states |
| M5.18b2 action-class method | observation `a7d2f3ad...e97d34`；294/196；2 complete / 1 failed |
| M5.18b2 Agent feedback | digest `a7a6e7e9...8ab7c2`；file `cdddab6b...e0016` |
| M5.18b3 corrected feedback | semantic `1910d179...30de4d`；feedback `f9ce5f2d...6aeec9e` |
| M5.18b3 follow-up spec | `bd8fbc93...eab0be7`；source 588/588；per-arm ceiling 686/686 |
| M5.18b3 seed-4 execution | report `1ce8ae13...ef8588a`；bundle `31916d15...12b8f6d`；79 states |
| M5.18b3 summary | `e0bda99f...e60677e`；execution-failed；actual charged 685/685 |
| M5.18b4-pre action surface | catalog `2effd56b...94f861`；view `d20b9fea...2e036` |
| M5.18b4-pre execution identity | plan `4b263fd4...26e87`；instance `2bbf81a1...3b5ea` |
| M5.18b4-pre outcome | `be4779d5...9a980`；valid/not-reached/not-evaluated |
| M5.18b4-pre execution | report `7d7c1765...e8c1e`；bundle `373f6740...96b7`；97/97 |
| M5.18b4-pre summary | `362afa36...e649`；0 model calls |
| M5.18b4 request freeze | `2591cf89...eed8c1`；2 arms；0 model calls |
| M5.18b4 hard/spec | baseline `8261d484...79c05`；spec `bf632a68...af262` |
| M5.18b4 requests | no-feedback `562d3bda...31ce5c`；with-feedback `eb44d738...f6826f` |

完整 bundle 约 1.9 MB/份，属于可再生本地证据并保存在 ignored `artifacts/`。仓库只提交约 15.1 KB 的
构建与评测摘要，避免 JSON 工件继续主导仓库体积。

## 本阶段验证

- `make test` 和 `go vet ./...`：通过；最终 `cmd/control-experiment` 普通测试耗时 90.785 秒；
- workload/trace-mutation 两项定向 race 以 84.420 秒通过；
- `make test-race-full`：**未通过**。两次均在显式 20 分钟 ceiling 超时，第一次运行到 adjacent
  trace mutation，最终一次运行到 M5.17c2 method 回归；两次均未报告 data race。按 timebox 停止，
  没有抬高 timeout，也没有把未见告警记为通过；
- M5.18b4R exact-once race topology：24 个顶层测试机械分为 method 6、execution 7、agent 11；
  三个 shard 分别以 353.374、128.527、88.508 秒通过，其余 package race 全部通过。该结果恢复的是
  新的分片 full gate，不回写上面两次历史单 binary 失败；
- M5.18b4 consumer 后 exact-once 清单为 method 6、execution 7、agent 12，共 25 项；定向普通测试
  12.164 秒、定向 race 165.074 秒、完整 agent shard race 361.838 秒，均通过；
- M5.18b4 pair 后 exact-once 清单为 method 6、execution 7、agent 13，共 26 项；最终普通全量测试中
  composition package 为 101.670 秒，受影响的完整 agent shard race 为 652.141 秒，均通过且没有
  data-race 报告；最终 ledger body 自校验修改后的单项 pair race 又以 407.280 秒通过。该耗时高于
  上一阶段，已如实记录但未抬高 20 分钟 ceiling；
- M5.18b4 pair runner 不增加顶层测试或新 SUT 成功执行，exact-once 清单仍为 26 项。
  最终普通全量测试的 composition package 为 99.492 秒；受影响 pair race 以 426.731 秒
  通过且没有 data-race 报告；
- `go test -count=1 ./...` 和 `go vet ./...`：通过，composition package 为 94.621 秒；
- 147 个非 artifacts JSON、23 个 schema JSON、4 个 M5.18a build input/audit schema 实例和 146 个
  Markdown 本地链接：通过；
- Python 历史 Agent 已删除，`unittest discover` 正常发现 0 项；
- 两份总体规划逐字节一致，`git diff --check` 通过；
- `internal/controlexperiment` 的完整 dependency closure 不包含 etcd、Raft 或 HashiCorp 包；
- M5.17c1/M5.17c2 冻结 identity 回归保持不变；M5.18a 额外使用显式 v3；
  M5.18b0 保持 M5.17a report/bundle identity。

## 当前没有证明

- 没有非公开 holdout candidate/control pair；
- 没有 Agent 与强基线的比较；action-class random/trace mutation 目前都只有公开 calibration；
- 没有证明 PSS 或未来 Coverage 能预测缺陷检出；
- 没有验证第二个严格确定性共识实现的同一 ExecutionBundle；
- 没有证明 Agreement 是所有共识协议唯一或充分的安全 Oracle；
- 没有完成多 attempt MethodSpec 或真实 Agent 方法比较；
- 已有一次真实调用及模型成本账本，但它受到 prompt 示例锚定，不是规划质量证据；
- 已有 corrected bundle-backed batch feedback 和 deterministic unseen follow-up，但没有
  one-shot/feedback 模型结果，也没有验证修正后的非锚定 prompt；
- b2 的 `238 > 165` 来自不可比终止语义，不能排名；b3 的 `241 > 238` 也不证明 action-class 更容易
  检出缺陷；workload pending 与 hard-action miss 都不是 defect verdict；
- 当前只有一个人工冻结的 etcd/raft risk 和 target-owned backend catalog，不证明知识包已普适；
- 新 IntentOutcome 仍只证明 ActionKind 前置，没有 RiskWitness 证明协议里程碑偏序；
- 没有 FaultProvider，所以 Partition/Heal 在新 b4 backend surface 中明确关闭；
- request、consumer、pair ledger 和 opt-in runner 只用离线 mock 验证；尚未获得本轮真实模型
  回复，也没有正式双臂模型结果；
- runner 能持久化可返回的 typed failure，但尚无 Campaign checkpoint，不承诺恢复进程被强制终止时
  尚未落盘的中间 arm；
- key reader 自身失败时不会发生模型调用，但 pre-key source construction 还没有独立 checkpoint；
- 没有证明 etcd/raft、ConsensusAtlas 或任何目标实现正确、完备或无缺陷。

## 下一步

进入 M5.19 Campaign Coordinator v1 设计：先冻结 Campaign/config/target identity、多 attempt 状态机、
append-only checkpoint 和 resume 校验，再实现按配置时间运行。wall-clock 只是停止上限；统计和方法比较
仍以 primary/replay work、decisions、attempts 和 model cost 为权威预算。下一阶段不调用真实模型。

## 阅读顺序

1. [M5.18b4 explicit opt-in pair runner](stage-m5.18b4-pair-runner.md)
2. [M5.18b4 pair orchestration/persistence](stage-m5.18b4-pair-ledger.md)
3. [M5.18b4 frozen request consumer](stage-m5.18b4-request-consumer.md)
4. [M5.18b4R race gate topology](stage-m5.18b4r-race-gate-topology.md)
5. [M5.18b4 request freeze](stage-m5.18b4-request-freeze.md)
6. [M5.18b4-pre trust corrections](stage-m5.18b4-pre-trust-corrections.md)
7. [M5.18b3 unseen follow-up baseline](stage-m5.18b3-unseen-follow-up.md)
8. [M5.18b2 defect-blind batch feedback](stage-m5.18b2-defect-blind-batch-feedback.md)
9. [M5.18b1 one-shot Agent transport](stage-m5.18b1-one-shot-agent-transport.md)
10. [M5.18b0 Guarded TestIntent compiler](stage-m5.18b0-guarded-intent-compiler.md)
11. [M5.18a 可信方法评价前提](stage-m5.18a-method-evaluation-prerequisites.md)
12. [M5.17c2 Batch PSS Guidance](stage-m5.17c2-batch-pss-guidance.md)
13. [M5.17c1 Corpus 可信前提](stage-m5.17c1-corpus-trust-prerequisites.md)
14. [M5.17c0 Experiment 语义加固](stage-m5.17c0-experiment-semantics.md)
15. [架构](architecture.md)
16. [总体规划](ConsensusAtlas-总体规划.md)
17. [M5.17bR2 在线旧路径删除](stage-m5.17b-r2-experiment-path-pruning.md)
18. [M5.17b Trace Mutation](stage-m5.17b-trace-mutation.md)
19. [M5.17b 小型账本](../benchmarks/experiments/etcdraft-v2-trace-mutation-m5.17b/README.md)
20. [M5.17a Action-class Random](stage-m5.17a-action-class-random.md)
21. [M5.16 ExecutionBundle](stage-m5.16-execution-bundle.md)
22. [M5.16R v1 删除](stage-m5.16r-legacy-removal.md)
