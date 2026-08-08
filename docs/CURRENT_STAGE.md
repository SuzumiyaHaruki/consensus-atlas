# 当前阶段

日期：2026-08-08

阶段：M5.18b3 unseen follow-up baseline 完成；未调用模型

## 输入、处理、输出

```text
输入
  comparable action-class-v2/uniform source bundles
  + bound PSS mapper + source seeds 1/2/3 + unseen seed 4
                         |
                         v
处理
  common Experiment-v2 semantics -> feedback v2 reprojection
  -> freeze source/follow-up/per-arm cost
  -> deterministic backend rule -> qualified execution + strict replay
                         |
                         v
输出
  digest-bound AgentFollowUpSpec
  + charged seed-4 deterministic follow-up
  + small feedback/spec/summary evidence
```

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

完整 bundle 约 1.9 MB/份，属于可再生本地证据并保存在 ignored `artifacts/`。仓库只提交约 15.1 KB 的
构建与评测摘要，避免 JSON 工件继续主导仓库体积。

## 本阶段验证

- `make test-fast`、`make test` 和 `go vet ./...`：通过；source fixture 复用后普通
  `cmd/control-experiment` 全测约 39 秒；
- `make test-race-full`：通过；`cmd/control-experiment` race package 耗时 983.930 秒，低于显式
  20 分钟 ceiling，未报告 race。剩余余量约 3 分 36 秒，下一阶段不得无界增加真实轨迹测试；
- 137 个非 artifacts JSON、23 个 schema JSON、4 个 M5.18a build input/audit schema 实例和 133 个
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
- 没有证明 etcd/raft、ConsensusAtlas 或任何目标实现正确、完备或无缺陷。

## 下一步

进入 M5.18b4：在任何调用前冻结无 feedback/有 feedback 两份 prompt/request bytes、共同 seed 4 和
完整 per-arm 账本。两个 Agent arm 的 semantic view、risk、hard constraints 和预算必须相同，feedback
只能改变 preference；M5.18b3 已知的 deterministic outcome 不得进入模型视图。每个 arm 最多 1 call、
无重试，parse/compile/execution/hard-action 失败都原样计费。完整 race 门通过前不调用模型。

## 阅读顺序

1. [M5.18b3 unseen follow-up baseline](stage-m5.18b3-unseen-follow-up.md)
2. [M5.18b2 defect-blind batch feedback](stage-m5.18b2-defect-blind-batch-feedback.md)
3. [M5.18b1 one-shot Agent transport](stage-m5.18b1-one-shot-agent-transport.md)
4. [M5.18b0 Guarded TestIntent compiler](stage-m5.18b0-guarded-intent-compiler.md)
5. [M5.18a 可信方法评价前提](stage-m5.18a-method-evaluation-prerequisites.md)
6. [M5.17c2 Batch PSS Guidance](stage-m5.17c2-batch-pss-guidance.md)
7. [M5.17c1 Corpus 可信前提](stage-m5.17c1-corpus-trust-prerequisites.md)
8. [M5.17c0 Experiment 语义加固](stage-m5.17c0-experiment-semantics.md)
9. [架构](architecture.md)
10. [总体规划](ConsensusAtlas-总体规划.md)
11. [M5.17bR2 在线旧路径删除](stage-m5.17b-r2-experiment-path-pruning.md)
12. [M5.17b Trace Mutation](stage-m5.17b-trace-mutation.md)
13. [M5.17b 小型账本](../benchmarks/experiments/etcdraft-v2-trace-mutation-m5.17b/README.md)
14. [M5.17a Action-class Random](stage-m5.17a-action-class-random.md)
15. [M5.16 ExecutionBundle](stage-m5.16-execution-bundle.md)
16. [M5.16R v1 删除](stage-m5.16r-legacy-removal.md)
