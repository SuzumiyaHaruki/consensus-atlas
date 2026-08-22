# 当前阶段

阶段：M4n27 typed milestone 顺序与战略 frontier 收口

## 当前结论

ConsensusAtlas 的活动链路已经收敛为：

```text
协议知识 / 有限源码读取
→ Risk Agent 候选与 typed milestones
→ 可信资格检查
→ Scenario Agent 单路径战略干预
→ 公共确定性推进
→ recorded Schedule
→ qualified execution / fresh Replay / Oracle
```

Scenario Agent 只使用 `continue / revise / abandon`。Agent 不能创建 Action、修改
Runtime、决定 Replay 或产生 finding；finding 只来自独立 Oracle。

## M4n14–M4n19 收敛结果

- M4n14：固定延迟 Invoke 与 Risk portfolio length 两个真实失败回归。
- M4n15：删除 branch/control/ablate/select 及分支 artifact，Episode 只保留一条路径。
- M4n16：完整 Scenario Trace 成为唯一 Schedule；Invoke/Partition 使用记录参数准备，
  qualified execution、fresh Replay 与 evaluator-owned Replay 复用同一 recorded-action 语义。
- M4n17：公共 bootstrap 跨越普通 candidate/term/ballot 变化；typed milestone 允许在唯一
  coordinator 就绪时自动 Invoke；finding attribution 从首个 Agent 战略干预开始。
- M4n18：closure-disabled 回归证明 etcd/raft 与 OmniPaxos 的既有 Drop 路径均可由公共
  causal progress 闭合，专用 selector 只缩短路径，不增加可达性。
- M4n19：删除两个协议的 closure factory/selector、handoff、candidate、prompt、artifact
  和预算状态；公共执行与 Agent 执行不再包含协议专用闭合分支。

当前 MethodSpec implementation ID 为：

```text
consensus-atlas/agentic-method/m4n27-typed-milestone-before-strategy-v1
```

Risk/Scenario prompt 分别为 `risk-agent-navigation-v11` 与
`scenario-agent-investigation-v25`。M4n26 及历史 MethodSpec 的
`closure_mode` 仍可读取，但当前 CLI 不再暴露 `-closure-mode`，活动组合固定使用公共推进。

## 当前执行语义

### Agent 选择

Agent 只选择当前 trusted frontier 中的战略 Action，例如 Drop、Duplicate、Crash、
Restart、Partition、Heal，或在多个不能机械判定的方向中选择一个。每次计划最多一个
战略 Action；普通消息、effect 和 timer 不持续占用模型调用。

### 公共推进

公共 causal progress 只选择 Runtime 已 enabled 的：

- host/effect completion；
- ordinary message delivery；
- naturally due temporal callback；
- typed automatic Invoke（仅在 workload、route 与 coordinator 唯一时）。

优先级只使用记录 Action 的 item dependency 与参与者方向，不理解 Raft term、
Paxos ballot、quorum 或具体消息语义。公共层不会自动执行新的故障干预。

### 证据

每个 Agent 或自动选择都成为普通 ActionRecord。完整 Trace 直接作为 recorded Schedule；
没有 Agent Trace → Policy 的逐 decision 二次翻译。Bundle 保存 preparation、Trace、Replay、
Oracle attribution 和成本。正式 finding 只来自 evaluator-owned Replay 后的 Oracle。

Decision provenance 只分：

- `agent_selected`
- `public_progress`

历史 `target_closure` 不再由当前代码生成。

## 跨协议验收

### etcd/raft

- 三节点和五节点 bootstrap 建立 leader，并在 typed milestone 需要时自动 Invoke；
- Drop `MsgAppResp` 后，公共推进在 17 或 19 个 post-intervention decisions 内闭合；
- 同一 RequestID committed；
- qualified execution、fresh Replay 与五个 Oracle 保持稳定。

### OmniPaxos

- 三节点和五节点 bootstrap 建立 coordinator，并自动 Invoke；
- Drop operation-carrying replication message 后，公共推进可到达同一请求 decision；
- qualified execution、fresh Replay 与三个 Oracle 保持稳定。

以上只证明公共执行底座跨两个 leader-based CFT Target 可用，不证明协议正确、
测试完整或 Agent 方法优于基线。

## 保留的可信边界

- Adapter Manifest / qualification / admission；
- deterministic Runtime、Action ownership 和 virtual time；
- durable Provider journal 与 usage accounting；
- local SUT source/build identity；
- recorded preparation、fresh Replay 和 evaluator-owned Replay；
- target-local Observation 与 Oracle registry；
- root/post-root Oracle attribution；
- calls、tokens、primary/replay work 和 wall-clock 预算。

没有新增 hash、baseline、冻结 contract 或 gate。

## M4n20 决定

- 活动 Target 无条件构造 bootstrap root；`root_mode` JSON/CLI 覆盖和两个生产
  workload-ready builder 已删除。窄 public-progress 校准只在 `_test.go` 内按 recorded
  Action 构造 workload-ready prefix。
- PSS 仍保留在 Bundle，供保存 Trace 的离线探索分析和历史 evaluator 使用；它已从
  Agent-facing Memory、Episode metrics 与 CLI 摘要删除，不再影响 Risk Agent 的下一轮选择。
- 没有直接删除 PSS schema：当前 CorePSS 仍绑定 Bundle validation 和历史正式工件；直接移除会
  同时改写 Bundle、Report、evaluator 与两个 Adapter mapper，不属于安全的次要瘦身。

## M4n21 HashiCorp composition 结果

现有 HashiCorp Raft Adapter 与 qualification 无模型复核通过，但资格结果仍为：

```text
Total=9 Required=8 Validated=3 Unsupported=6 Qualified=false
```

它已经具备 Adapter factory、Runtime-owned message、crash/restart 和 opaque Invoke；尚缺活动
WorkloadRouter、Observation/Decision projector、Oracle registry、Agent knowledge 与本地源码 mount。
更关键的是 wall-clock/random timeout 不受调度器控制，strict Replay、natural temporal progress 与
audited entropy Replay 明确未支持。

试接入没有要求修改 Scenario 状态机、recorded Schedule executor、Provider journal、Bundle 或公共
progress。当前结论不是“第三 Target 已接入”，而是“公共边界未被推翻，阻塞准确位于 SUT 可测试性和
Target-local 组件”。详见 `docs/hashicorp-composition-trial.md`。

下一步只准备两个活动 Target 的单 Episode canary；外部模型调用必须另行获得本轮明确授权。HashiCorp
在提供可测试性端口和本地源码 checkout 前不进入付费 Agent 实验。

## M4n22 Provider/recovery 修复

- 失败的 Provider 调用允许 `calls=1,tokens=0` 这一种明确的未知 usage 形状，但成功响应仍必须有
  正数且自洽的 token 账本；未知 usage 不进入可信 Episode summary，并报告
  `AGENTIC_EPISODE_PROVIDER_USAGE_UNRECONCILED`。
- DeepSeek JSON Output 的首次空 content 仅在 usage 已知时由 Risk Agent 在原 calls/tokens 预算内重试
  一次；未知 usage、畸形或过大响应保持终止证据，不猜测费用。Provider client 已识别的 HTTP
  传输失败保持 `agent-transport-ambiguous`，不再被 journal 默认改写成 malformed response。
- journal 恢复的 intent 文件读取上限与类型允许的两个 1 MiB 原始字段相容。此前真实 canary 的
  144,010-byte intent 不再被旧 128 KiB 文件上限错误拒绝。
- 新回归覆盖未知 usage 的 in-band Risk 停止、可信 summary 拒绝、DeepSeek 空 body 分类、一次空响应
  重试和大 intent 精确恢复；没有放宽 Runtime、Replay、Oracle 或 SUT 绑定。

## M4n22R provenance 修复

- Episode 的 `decision_provenance` 改为只从最终合并后的 `ScenarioExecution` 推导：已应用且有
  trusted choice 的 `Steps` 计入 `agent_selected`，全部 `AutomaticProgress` 计入
  `public_progress`。Scenario attempts 只保留模型调用过程、反馈和 work，不再重建最终 schedule。
- 这覆盖了 planner 调用之间由可信执行器自动完成的 Invoke。该 Action 已经进入最终 Trace、
  `DecisionsUsed` 和 `Execution.AutomaticProgress`，即使它不属于任何 attempt，也不会再造成工件
  封存时少计一个 decision。
- compact artifact 直接复用 `AgenticDecisionProvenance.Validate(total)`，新工件不再允许
  `decisions > 0` 但 provenance 全零的历史逃生路径。
- 无模型回归覆盖最终 Execution 推导、零 provenance 拒绝，以及“attempt 省略自动 Invoke”后的
  完整 Episode 写入、Bundle 恢复、Replay/Oracle 结果和 provenance 一致性。

## M4n23 coordinator setup 修复

- 当首个缺失 milestone 是 `workload-invoked` 且候选没有把选举本身列为此前 milestone 时，
  Scenario Agent 不再承担普通启动调度。可信公共推进持续消费 Episode decision budget，直到唯一
  coordinator 可接受 Invoke，自动执行 typed Invoke，并到达下一个可机械识别的战略 Action frontier。
- 下一 milestone 为 Drop/Crash/Restart 时，公共层只根据候选已有 predicate、可信 resolved binding
  和当前 Action/semantic hint 判断何时交还控制权；它不创建 Action、不选择故障，也不写入协议规则。
- `natural-progress-slice-exhausted` 与真正的 Episode decision limit、quiescent 分开。前者明确表示一次
  有界公共推进切片结束但总预算仍可用，Scenario prompt 不再把它描述成一般的 budget failure。
- OmniPaxos Target 的 `operation_state` 现在只标记实际携带 request 且 entry_count 大于零的
  `accept-sync/accept-decide` Action，避免 heartbeat/普通 recovery traffic 冒充 operation frontier。
- 三/五节点 etcd/raft 与 OmniPaxos 零模型回归均要求：第一次 planner 调用已经观察到 Invoke 之后的
  目标 Drop frontier，并能用一个真实 enabled Action 进入 recorded schedule、Replay 和 Oracle。
- 自动 setup 的执行工作单独记录为 `scenario_setup_work`；它计入 Scenario 总搜索成本，但不伪装成
  任一模型 attempt 的工作。工件恢复继续机械检查 `setup + attempts == scenario_search`。

### M4n23 etcd/raft 单 Episode canary

五节点 canary `artifacts/agentic/m4n23-etcdraft-single-canary-v1` 使用当前 MethodSpec、显式本地
SUT binding 和中立只读源码搜索运行。Risk Agent 完成一次 search 和一次 bounded read，但读取的是
仓库内通用 CLI 文件，未使用修改说明或定向缺陷提示。随后的 portfolio 响应连续两次被 Provider 记录为
`response-empty-content`，因此本轮结果为：

```text
risk_calls=4
scenario_calls=0
model_tokens=61831
executable=false
```

所以这次 canary 没有进入 Scenario，无法用真模型工件验收“第一次 Scenario 调用直接看到战略 frontier”。
它不否定三/五节点零模型回归已经证明的执行语义，也不能视为 Agent 调查成功；按单次 canary 边界不自动
重跑。后续若再次实验，应使用新目录并把目标明确限定为 Provider 能产出 executable portfolio 后的端到端
验收，不能把这次结果补写成成功。

## M4n24 最小收口

- automatic setup 在后续存在战略谓词时至少保留 1 个 Runtime decision；仅剩该额度时不调用
  Provider，而是返回 `setup-budget-exhausted`。三/五节点两个 Target 的普通回归同时覆盖该边界。
- Core 不再枚举 `operation-replication` 这一 Target 语义值；带 `operation-stage` 的 Target-local role
  只通过封闭的 `OperationState` 投影匹配。
- 已绑定 SUT 的 Risk 搜索仅访问当前 SUT 和当前 Adapter，优先级为 `SUT → Adapter`。公共 Core
  中的 `campaign` 等同名词不再冒充协议源码 grounding；无匹配时返回中立 no-match，不向 Agent
  提供修改或缺陷提示。
- 本地验证已通过全量 Go test/vet、两项审计、新边界聚焦 race 以及三/五节点跨协议 race。

### M4n24 Oracle-clean canary

隔离 checkout 的 ConsensusAtlas 提交为 `4ca5ea3`，SUT 为未修改的官方 etcd/raft
`f35a022`，两个工作树均 clean。五节点单 Episode 结果：

```text
risk_calls=4, scenario_calls=8, model_calls=12, model_tokens=220172
executable=true, scenario_decisions=64, witness_instantiated=false
grounding=completed-used: source/go.etcd.io/raft/v3@v3.6.0/raft.go
replay_stable=true, root_prefix_oracle_findings=0
```

这证明 scoped grounding 和整条 Scenario/Bundle/Replay/Oracle 链已恢复，但还暴露两个问题：

- Accepted Risk 为 `Invoke → 三个 message-delivered → Crash → coordinator-changed`。M4n24 只跨过
  紧邻 Invoke 的战略谓词，因此前四次 Scenario 调用仍用于普通消息调度，“第一次调用直达战略
  frontier”在该方法版本下没有验收通过；
- 在线 Oracle 将 Crash/Restart 期间的 volatile commit 丢失误报为 `etcdraft-log-progress`。
  saved Bundle 证明 finding 位于 step 57/73 的生命周期边界，而不是 agreement/client-binding 问题。

完整 canary 证据保存在本地被忽略目录
`artifacts/agentic/m4n24-etcdraft-clean-single-canary-v1`，没有重跑或追加外部调用。

## M4n25 canary 后收口

- Invoke 后的 `message-delivered / temporal-fired / epoch-advanced / decision-advanced /
  coordinator-changed` 被视为公共前置条件；自动进度可跨过它们直到第一个 Drop/Crash/Restart
  frontier，但不选择战略 Action，也不跨过第二个 Invoke 或未知 Target-local 谓词。
- 新的三节点 etcd/raft 回归在 Invoke 与 Drop 之间插入真实 `MsgApp delivered` milestone，
  并仍要求第一次 Planner 调用直接看到 Drop frontier。
- `etcdraft-log-progress` 忽略宕机期的 volatile placeholder；commit 只在同一 running incarnation
  内要求单调，applied/application prefix 仍跨重启严格单调。M4n24 saved Bundle 用修正后的五个
  monitor 离线重算为 0 violation，未重跑模型。

## M4n26 canary 前收口

- Scenario Agent 不再接收 authoritative frontier 中的 `CompleteEffect`、`DeliverMessage` 或
  `FireTemporal`。Core 从同一可信 frontier 派生只含非自然 Action 的 strategic view；完整
  frontier 仍只供公共推进和精确执行使用。模型即使猜到被隐藏的普通 ActionID，或用 semantic
  selector 请求普通 Action，也会在 Runtime 执行前得到 `action-not-strategic` 反馈。
- 当前没有 strategic Action 时，可信公共推进继续使用同一通用 natural Action 语义，直到出现
  strategic frontier、witness、真实 quiescence 或总 decision budget；不会为了普通消息/effect/timer
  消耗模型调用。该逻辑只区分公共 ActionKind，不包含 Raft/Paxos 消息名称。
- post-root Oracle violation 分成 `independent-oracle-finding` 与
  `hypothesis-oracle-finding`。后者必须同时满足 witness 已实例化、monitor 属于候选 property 的
  registry 映射、fidelity 不是 unassessed；其他真实 violation 仍作为独立 finding 保存，不能冒充
  当前 Risk 的机制证据。
- etcd/raft 的 target-local log-progress Oracle 现在读取 Adapter 已有的 `storage_commit`：durable
  commit、applied 和 application prefix 跨 restart 单调，volatile commit 只在同一 running
  incarnation 内单调。该语义未进入公共 Core；OmniPaxos 继续使用自己的 agreement/client-decision
  monitors。
- 三/五节点 etcd/raft 与 OmniPaxos 的零模型 bootstrap 回归同时要求 Planner view 不含自然
  Action。M4n26 尚未调用外部模型；完成本节验证只表示代码已具备一次新目录 canary 条件。
- Scenario 搜索成本现在分别封存 setup、Agent attempt 和 Agent 后可信自然推进。三者可机械聚合为
  Episode 的完整 Scenario search work，避免 Target-local closure/公共推进成本落在 attempt 之外后使
  工件无法恢复；该账本不包含任何协议消息语义。

## 验证

M4n26 完成前必须通过：

```text
go test ./...
go vet ./...
make audit-no-v1 audit-race-shards
git diff --check
```

并对公共推进、bootstrap、artifact/recovery 和 evaluator-owned Replay 做聚焦 race。

当前工作区已通过上述全部普通检查，以及战略 frontier、OmniPaxos bootstrap/工件恢复和 etcd/raft
Oracle 的聚焦 race；随后按以下记录只运行了一次新目录 canary。

### M4n26 五节点 etcd/raft 单 Episode canary

全新目录 `artifacts/agentic/m4n26-etcdraft-single-canary-v1` 已完成一次 DeepSeek 官方调用：3 次 Risk、
3 次 Scenario，共 6 calls / 108,034 observed tokens，所有 provider usage 已对账。Risk Agent 产生 3 个
executable candidates，选中 `reapply-after-crash-application-regression`；源码 grounding 完成了 SUT
search/read，但候选未引用读取片段，因此机械状态为 `completed-unused`。

Scenario 共执行 38 decisions，其中 2 个由 Agent 选择、36 个为公共推进；Agent 依次选择 Duplicate 与
Crash，随后在首个 `apply` milestone 尚未满足时 abandon，所以结果为 executable、witness 未实例化，
不是 finding。Bundle 共 48 decisions，fresh Replay stable；独立 evaluator 重算 5 个 Oracle。唯一
violation 是 step 37 的 election-safety，发生在 root boundary step 40 之前，因而只记为 1 个
root-prefix finding，没有归因给 Agent。该结果验证了 strategic-only frontier、成本账本、Replay 和
finding attribution 的真实主链，不证明 Agent 已能完成所选恢复假设，也不触发第二次 canary。

### M4n26 OmniPaxos 单 Episode canary

全新目录 `artifacts/agentic/m4n26-omnipaxos-single-canary-v1` 使用本地 `suts/omnipaxos` 离线重建
worker，并完成 4 次 Risk、1 次 Scenario，共 5 calls / 88,010 observed tokens。Risk Agent 产生 3 个
executable candidates，选中 `inflight-ballot-handoff-client-binding`；SUT search/read 成功但候选未引用
读取的 `atomic_storage_test.rs`，因此 grounding 为 `completed-unused`。

Scenario 的唯一战略 Action 是 step 24 丢弃 `n1→n2 ble/heartbeat-request`，随后公共推进到 client
terminal；它没有满足候选要求的首个 `n2-pulse-a` milestone，所以 witness 未实例化，停止原因为
`strategic-frontier-unavailable`。28-step Bundle fresh Replay stable，独立 evaluator 重算
trace-integrity、agreement 与 omnipaxos-client-decision-binding，均为 0 violation。

该结果暴露一个协议无关的编排缺口：候选下一 milestone 是受 participant binding 约束的 natural timer，
但 strategic-only Agent view 隐藏 Timer；与此同时，普通 Drop frontier 会让公共推进过早把控制权交还
Agent。后续应在 Core 按 accepted predicate 区分“需要可信继续完成的自然 milestone”与“等待 Agent
选择的战略 milestone”，而不是为 OmniPaxos 增加消息名或专用 selector。

## M4n27 typed milestone 顺序修复

- 可信层现在只处理 `first_missing_milestone`，不再因为 frontier 中存在任意 Drop/Crash 就提前调用
  Scenario Agent。当前 milestone 是 `message-delivered` 或 `temporal-fired` 时，分别优先选择真实
  enabled 的 Deliver 或 Timer；coordinator/epoch/decision 等其他自然 milestone 继续复用公共 causal
  progress。milestone 满足后才重新计算下一目标。
- predicate 中已由可信 witness 解析的 participant binding 会收窄自然推进方向；首次未解析的 binding
  仍按稳定公共顺序选择，随后同名 binding 必须沿用真实观察到的节点。Core 只读取通用 Observation
  field，不解释变量名，也不包含 Raft/OmniPaxos 消息词汇。
- 当前 milestone 是战略 predicate 时，公共层只推进普通 Action，直到与该 predicate 的类型化约束匹配
  的 Action 真正进入 enabled frontier；无关 Drop 不能抢占控制权。匹配 Action 仍由 Agent 选择，公共层
  不执行故障干预，并至少保留一个 Runtime decision 给该选择。
- 三/五节点零模型回归已覆盖 etcd/raft 的 `Invoke → MsgApp delivered → Drop` 与 OmniPaxos 的
  `Invoke → temporal-fired → operation Drop`。第一次 Planner 调用不包含自然 Action，且只在当前
  typed 战略 frontier 可执行时发生；两个 Target 的 qualified Bundle、fresh Replay 与 Oracle 路径保持
  通过。
- 本轮没有调用外部模型，也没有修改两个协议实现。M4n26 工件继续只读兼容；新的执行语义使用
  `m4n27-typed-milestone-before-strategy-v1`，避免旧、新方法工件混合恢复。
- `go test ./... -count=1 -timeout=360s`、`go vet ./...`、两项仓库审计与 `git diff --check`
  均通过；公共 milestone 选择的聚焦 race 通过。两个真实 Target 合并运行的聚焦 race 在 360 秒
  总超时处终止，未报告数据竞争；普通跨协议回归已通过，因此记录该超时且不重复消耗时间。
