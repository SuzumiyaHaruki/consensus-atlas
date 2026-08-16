# 当前阶段

更新时间：2026-08-16
分支：`feature/agentic-consensus-testing`
阶段：M3.4 完整成本与方法归属（已完成，待版本化收口）。

## 一句话状态

活动系统已经收敛为“Risk Agent → Scenario Agent → 两个真实 Target → 确定性执行/Replay → PSS/Risk/Oracle”。
client terminal 只表示 workload 已返回；Risk 未达且预算尚存时继续调查。旧 A2/A8/Campaign 路径已从生产代码
删除。当前 Agentic Episode 的 `summary.json`/`bundle.json` 已能直接进入现有 private pair/exposure/
Oracle 评测边界，不启动第二套搜索或执行器。
Scenario Agent 现在输出显式 Investigation Proposal；branch/control/ablate 复用现有 Runtime、Trace、Replay
和 Oracle，不引入第二套分支执行引擎。实验分支只保存候选；`continue + from_branch_id` 可边继续执行边晋升，
零 Action 的 `select + from_branch_id` 可直接选择已有分支。无论 Agent 是否选择，所有唯一且 fresh-Replay 稳定的
候选分支都会离线进入可信 Target Oracle；因此最后一个 decision 上出现的 finding 不会因预算同时耗尽而丢失。
跨 Episode 的 Agent-facing Memory 只包含机械执行状态、Risk 进展、PSS 和成本，不包含 Oracle finding
数量，也不使用 Oracle 派生的 assessment status 选择 outcome 或代表路径。
一次调查当前最多使用 512 个成功 Action。预算按剩余 Agent 调用次数自动切成周期反馈片段，使长时间自然推进
不会一次耗尽全部 decision budget；每个片段的战略计划和自然推进共享同一个 live Runtime。
协议无关校准之外，etcd/raft 与 OmniPaxos 现在都已有 128 Action 的真实 Adapter → Bundle → PSS →
Observation → Oracle 垂直回归。
正式 Agentic evaluator 现在核算 `Scenario frontier + Scenario search + 所有 qualified primary`，
单独汇总 Replay，并将模型 calls/tokens 与执行工作都对照同一份
`AgenticLogicalBudget`。正式 Episode 的主路径和分支 Bundle 必须为 V3，且
`MethodSpecDigest`、Trace digest 和 work 必须与 summary、branch evidence 及 formal contract 交叉一致。

## 当前输入

每个活动 Target 提供：

1. `plans/agent/*.json`
   - `ProtocolKnowledgePack`；
   - Property、Historical Issue Pattern 和 Target Dossier；
   - workload；
   - Runtime/FaultEnvelope；
   - Scenario、模型和 Investigation 预算。
2. Target composition
   - Adapter factory；
   - Qualification/Admission；
   - composable Action；
   - target-local Observation projector；
   - target-local Oracle registry；
   - fidelity boundaries。
3. 可选只读源码 mount 和显式 OpenRouter key/model。

输入不包含预置 Risk 或 TestHypothesis；两者由 Agent 候选和可信转换生成。

## 当前处理流程

```text
Protocol materials
    ↓
Risk Agent portfolio / source query / mechanical feedback
    ↓
Candidate qualification + fidelity assessment
    ↓
trusted TestHypothesis / RiskWitnessSpec
    ↓
Scenario Agent investigation proposal + bounded plan
    ↓
enabled frontier binding
    ↓
one live Runtime branch per proposed path
    ↓
fresh Replay + observations + PSS + Oracle
    ↓
durable summary/bundle/branch-evidence/journals
```

关键终止语义：

- client terminal：只结束当前自然推进段；
- Risk reached：可结束当前调查；
- 模型调用、token 或 Runtime decision 预算耗尽：机械停止；
- 调用耗尽但只有未选择的实验分支：Scenario 保留 `final-selection-required`，不任取最终路径；这些分支仍会独立
  生成 qualified evidence 并运行 Oracle；
- 已存在分支且进入最后 Scenario 调用或 decision 已耗尽：最后调用只允许 `select`，
  模型调用和 token 正常计费，Runtime decision 成本为零；
- quiescence：返回反馈；若仍有可规划战略 Action 和预算，可再次调用 Agent；
- failed Action：不进入成功 Trace，另存 terminal outcome。

## 本轮修复

### 1. 终止语义统一

删除了“完整计划在 client terminal 后不得再次规划”的旧测试契约。现在完整计划执行完后，如果 Risk 未达到且
模型/决策预算仍开放，系统生成 `ProgressDelta` 并继续调用 Scenario Agent。不存在按 `maxPlanSteps` 改变终止语义
的特殊分支。

同时删除了单步兼容路径中的固定 24-action planning checkpoint。自然推进使用 episode 剩余决策预算；不会因为
旧输入是单步计划而提前返回 Planner。

精确回归测试覆盖了 `Invoke → client-terminal → Risk 未达 → Crash 仍可达 → 第二次 Planner`，
避免只用 quiescent 间接证明该语义。

### 2. 最小执行底座

- namespaced target-local Observation 可由 Target 声明和投影，公共 Core 不枚举协议语义；
- etcd/raft partition/heal 已通过 offered → selected → executed → fresh Replay；
- 每个活动 Target 的 composable Action 都有实际 frontier 可达性测试；
- Oracle capability 与可执行 monitor 从同一 target-local registry 派生；
- evidence assessment 要求 property 对应 monitor 实际出现在 `Oracle.Checked`；
- 未声明 fidelity 时返回非阻塞 `fidelity-unassessed`，显式依赖不可表达边界时返回 capability gap；
- strategic step、`after_milestone` 和自然推进共享 plan-local live Runtime，仅 promotion 时 fresh Replay。
- OmniPaxos worker stderr 使用互斥保护的有限容量缓冲区；deadline kill 后通过同步 snapshot
  封存失败诊断，不再与 `os/exec` stderr-copy goroutine 并发读写。

### 3. A9 与历史 root 解耦

etcd/raft Agentic root 由活动 workload、Qualification、Runtime 和 Adapter config 直接构造，并在首次真实 Invoke 后截取
可信 prefix；不再读取 A2 root ID、stateless corpus 或 `workload-risk-witness-calibration` 策略。

OmniPaxos 继续由 Target-local root builder 在真实 worker 上自然推进到唯一协调节点，再执行 Invoke。公共
coordinator 不理解 leader、term、ballot 或协议消息类型。

### 4. 代码瘦身

已删除：

- A2 Semantic Explorer 和 Semantic Best First；
- A8 Scenario session、paired wrapper 及 formal paired launcher；
- 通用 Campaign config/coordinator/store/summary；
- stateless Campaign、root corpus、traversal/discovery wrapper；
- etcd/raft 旧 stateless runner 和多代 random/uniform CLI；
- 与上述路径一一对应的历史测试和兼容 checkpoint。

保留：

- Runtime、Trace、Replay、conformance、qualified execution；
- Scenario 的确定性 DFS/frontier kernel；
- Agent durable journal 与恢复；
- target-local Observation/Oracle；
- etcd/raft、OmniPaxos 的协议特有 composition；
- Bundle/MethodSpec evaluator 和 defectbench。

当前 Go 代码约 32.7k 生产行、14.6k 测试行；审计前约为 41.5k/18.9k。

### 5. Agentic holdout 桥接

`cmd/defect-eval -agentic-inputs` 接收私有 trial 到 Episode 目录的映射，并复用现有
`FormalBenchmarkContract` 和 `FormalExposureAudit`。评估器：

- 从 Episode summary 读取方法状态、搜索 work 和 model work；方法状态只是解释字段，
  但搜索 work 与 model work 必须进入正式预算核算；
- 对 completed Episode 的主 Bundle 和 `branch-evidence.json` 中每个候选 Bundle 重新执行私有
  projector/monitor；任一候选产生 finding 即成为该 trial 的可信结果；
- 在查看 finding 前先汇总 Scenario frontier/search 和所有候选的 decisions/qualified primary work，
  超过同一 formal trial 预算时整个 trial 为 `invalid`；Replay work 也汇总报告；
- 将 Risk/Scenario 调用次数和 tokens 与 formal contract 里的 `AgenticLogicalBudget` 比较；
- 要求所有 Bundle 使用 V3 method identity，并将 summary/branch 声明的 ID、Trace、work 与实际文件交叉校验；
- 将未完成、完全缺少候选 Bundle 或任一候选 contract/build/budget 不匹配的 trial 记为 `invalid`；
- 完全忽略 Agent 自报的 `oracle-finding` verdict，只由重算 monitor 生成 killed/false-positive；
- 同一评估入口支持 etcd/raft 和 OmniPaxos 的 DecisionProjector。

当前 holdout composition 只注册两个 Target 共用的 Agreement monitor。Target-local monitor 还未转移到
evaluator 可注册包；这是后续能力扩展，不影响本轮桥接语义。

### 6. 分支、对照与消融

Agent 可在当前可信前缀上选择 `continue`、`revise` 或创建命名 `branch`。`control` 从被引用 branch
的根 Trace 重新执行，`ablate` 也从同一根开始，并且其计划必须机械等于参考计划删除已实际执行干预后的结果。
reference branch 必须完整成功，删除 ID 必须存在于 `applied_interventions`；control/ablate 不能携带 exact ActionID。
每条路径继续使用现有 Action concretization 和 fresh Replay。

分支执行消耗统一的 Runtime decision allowance；branch/control/ablate 不自动覆盖最终路径。Agent 可通过
`continue + from_branch_id` 选择并继续，或通过无计划、零 Runtime Action 的 `select + from_branch_id` 直接选择。
反馈公开根/终点 decision、最终可用 Action、
计划、实际执行的干预、结果和 `ProgressDelta`，完整 Trace/frontier
留在可信协调器，避免分支数增长时重复扩大模型输入。

选择与缺陷检出相互独立：Coordinator 对每个不同 Trace digest 的分支只做一次 qualified execution 和 Target Oracle，
结果及 work 单独写入 `branch-evidence.json`。在线 Agent 看不到这些私有 Oracle 结果；离线评测也会重新检查所有候选，
因此 Agent 选错代理信号只影响解释路径，不会让已经执行到的可信 finding 从报告中消失。

`minimize` 尚未进入可选 intent：它必须在 target Oracle 产生可信 finding 后由上层 investigation
协调，当前 Scenario 阶段若收到该请求会返回 `trusted-finding-required`。

### 7. 长轨迹与周期反馈

`ScenarioAgentMaxDecisions` 从只适合短闭环的 64 提高到 512。Coordinator 根据 episode 的
`remainingDecisions/remainingCalls` 在每轮重新计算反馈片段，并向 Agent 明确提供本轮 `decision_allowance` 与全局
`remaining_decisions`。本轮 allowance 同时覆盖嵌套计划与确定性自然推进；合法计划不会再把全部剩余预算
一次消耗完，除非它已经进入最后一个调用片段。

计划 step、`after_milestone` 和计划后的自然推进在同一个 plan-local Runtime 上追加。片段结束时只做一次
fresh Replay；这个 Replay 既验证完整候选 Trace，也产生下一轮可信 frontier/snapshot，不再为 Agent 反馈额外
重建相同前缀。Snapshot 保留 Replay 生成时的精确 nil/empty 形态，避免 canonical digest 被复制过程改变。

协议无关 fixture 校准结果：

| Action | Agent calls | reconstruction setups | verification setups | verification decisions | total work |
|---:|---:|---:|---:|---:|---:|
| 256 | 4 | 4 | 4 | 645 | 1298 |

四轮 `ProgressDelta` 分别覆盖 65、65、64、62 个 Action；反馈只保留最近 8 个 Action，并能识别重复调度
模式。该结果证明数百 Action 调查不会退化为逐 Action Replay。

真实 Target 校准从确定性初态开始，不预先执行 workload；Agent fixture 每轮只选择当前真实 frontier 中一个
自然推进 Action，其余消息、effect 和 timer 由同一 live Runtime 推进。已有 Risk 的首个 workload milestone
因此保持未达，系统将结果诚实报告为 `not-reached`，而不是为了得到 finding 人为修改 Risk。

| Target | Action | Observation | target progress | protocol/control/joint PSS | Scenario work | qualified primary/replay |
|---|---:|---:|---:|---:|---:|---:|
| etcd/raft | 128 | 68 | 1 `raft/term-advanced` | 14 / 37 / 53 | 658 | 129 / 129 |
| OmniPaxos | 128 | 130 | 1 `omnipaxos/promise-raised` | 4 / 41 / 46 | 658 | 129 / 129 |

两条路径都使用 4 次 Agent 反馈、4 次 reconstruction、4 次 promotion Replay 和 325 个累计 verification
decision；最终 Trace 与 qualified Bundle digest 一致，Replay stable，所有注册 Oracle 均执行且零违规。
一次 term/promise 进展后主要进入稳定自然运行，所以这不是“反复选举/反复换主”的证据，也没有证明 Agent
具备长时假设修订能力。

## 当前输出

一次终态 Agentic Episode 生成：

- `risk-agent/`、`scenario-agent/` provider journal；
- `summary.json`；
- 达到可测试终态时的 `bundle.json`；
- 存在未选择候选时的 `branch-evidence.json`；
- accepted Risk、capability/fidelity 状态；
- protocol/control/joint PSS 与 sample 数；
- Risk milestones、首个缺失 milestone 和 `ProgressDelta`；
- Replay 状态、`Oracle.Checked` 和 violations；
- model calls/tokens、Runtime decision allowance 和停止原因。
- 总探索 Action、最终选择路径 Action、实验分支 Action，以及 branch/control/ablation 的实际干预差异。

一次 private holdout 评测另外生成按 trial 排序的 control/candidate 结果、invalid 原因、重算
Oracle 和汇总统计。私有 pair/root-cause 信息不进入 Agent 输入。

## 当前证据边界

已经证明：

- 两个真实 CFT Target 共用同一 Agentic coordinator；
- Action 只能从真实 frontier 具体化；
- 保存 Trace 可 fresh Replay；
- target-local Observation/Oracle 不要求公共 Core 理解协议；
- 能力不足与假设未达、预算耗尽、执行失败可以分开报告。
- 当前 Agentic artifact 的主路径和未选择分支都可被 private evaluator 机械消费，且方法自报 verdict 不会创建 finding。
- treatment/control/ablation 可从同一确定性检查点执行，并可继续 Agent 选中的分支。
- 两个真实 Target 都能在 128 Action 上完成周期反馈、fresh Replay、PSS、target-local Observation 和 Oracle。

尚未证明：

- Agent 比 Random、DFS 或专家方法发现更多问题；
- Coverage/PSS 能预测缺陷检出；
- 存在新的 etcd/raft 或 OmniPaxos 问题；
- 当前 RawNode/MemoryStorage Target 等价于生产部署；
- 已有真实非公开 holdout 数据集及 Agent 相对 baseline 的效果结论。

## 下一步

M3.2 已闭合最后 decision 分支保留、零 Action 选择、全候选 Target Oracle、分支证据持久化和 holdout 重算。
M3.3 进一步移除了跨 Episode Oracle Memory 回流，将多候选成本纳入 formal trial 聚合预算，
并预留了正常计入模型成本的 select-only 收尾调用。
M3.4 又将 Scenario frontier/search、所有 qualified evidence 和模型工作纳入同一正式预算，
并用现有 V3 `MethodSpecDigest` 绑定 Episode、分支 Bundle 和 formal contract。历史 V2
OpenRouter 校准工件不满足正式归属条件，不直接用于 private holdout。
下一阶段转入 Agent 能力释放：先让
Scenario Agent 根据真实 `ProgressDelta` 做 `continue/revise/branch` 选择，并扩大只读源码导航；随后才运行
经过明确授权的长时 OpenRouter investigation。fixture 持续选择自然进展只能作为执行校准，不能作为 Agent
有效性的实验结果。

`minimize` 仍留到可信 Oracle finding 已存在时实现，不恢复旧 A8 paired session，也不因为长轨迹新增
hash、冻结 contract、baseline 或 gate。

本轮 `go test ./...`、`go vet ./...`、`git diff --check`、`audit-no-v1` 和 `audit-race-shards`
均通过。搜索成本超预算、模型成本超预算、跨方法 Bundle 替换、未声明分支文件、
V3 Episode 绑定和 OmniPaxos qualified method identity 均有普通回归。本轮又对
`internal/defectbench`、`cmd/defect-eval` 和 `cmd/control-experiment` 的上述关键用例运行了聚焦 race，
全部通过；没有重复整套 `test-race-full`，留给版本化或发布前完整验证。
256 Action 协议无关 fixture 继续只作为成本校准。
