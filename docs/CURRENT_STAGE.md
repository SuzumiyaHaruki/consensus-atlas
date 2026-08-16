# 当前阶段

更新时间：2026-08-16
分支：`feature/agentic-consensus-testing`
阶段：A9e4c13R 最小底座闭合与历史路径瘦身；尚未进入 A9e4d。

## 一句话状态

活动系统已经收敛为“Risk Agent → Scenario Agent → 两个真实 Target → 确定性执行/Replay → PSS/Risk/Oracle”。
client terminal 只表示 workload 已返回；Risk 未达且预算尚存时继续调查。旧 A2/A8/Campaign 路径已从生产代码
删除，当前最小底座与瘦身已完成；Agentic holdout evaluator 属于下一阶段。

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
Scenario Agent multi-step plan
    ↓
enabled frontier binding
    ↓
one live Runtime branch
    ↓
fresh Replay + observations + PSS + Oracle
    ↓
durable summary/bundle/journals
```

关键终止语义：

- client terminal：只结束当前自然推进段；
- Risk reached：可结束当前调查；
- 模型调用、token 或 Runtime decision 预算耗尽：机械停止；
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

当前 Go 代码约 32.2k 生产行、14.2k 测试行；审计前约为 41.5k/18.9k。工作区 diff 的净删除量超过 11k 行。

## 当前输出

一次终态 Agentic Episode 生成：

- `risk-agent/`、`scenario-agent/` provider journal；
- `summary.json`；
- 达到可测试终态时的 `bundle.json`；
- accepted Risk、capability/fidelity 状态；
- protocol/control/joint PSS 与 sample 数；
- Risk milestones、首个缺失 milestone 和 `ProgressDelta`；
- Replay 状态、`Oracle.Checked` 和 violations；
- model calls/tokens、Runtime decision allowance 和停止原因。

## 当前证据边界

已经证明：

- 两个真实 CFT Target 共用同一 Agentic coordinator；
- Action 只能从真实 frontier 具体化；
- 保存 Trace 可 fresh Replay；
- target-local Observation/Oracle 不要求公共 Core 理解协议；
- 能力不足与假设未达、预算耗尽、执行失败可以分开报告。

尚未证明：

- Agent 比 Random、DFS 或专家方法发现更多问题；
- Coverage/PSS 能预测缺陷检出；
- 存在新的 etcd/raft 或 OmniPaxos 问题；
- 当前 RawNode/MemoryStorage Target 等价于生产部署；
- Agentic 方法已经进入 private holdout evaluator。

## 下一步

本阶段完成条件：

1. 全量 `go test ./...`、`go vet ./...` 和 `git diff --check` 通过；
2. 文档不再声称已删除的 A2/A8/Campaign 路径仍然活动；
3. etcd/raft 与 OmniPaxos 活动 composition 测试保持绿色；
4. 记录代码行数变化和 formal paired evaluator 的删除边界。

本阶段最终验证：全量 `go test ./...`、`go vet ./...`、race shard 清单审计、JSON 语法与
`git diff --check` 通过。OmniPaxos deadline 精确 race 用例连续两轮通过，`make test-race-core`
已完整通过。

之后才进入 A9e4d，优先实现当前 Agentic artifact 的 holdout 评测输入，而不是恢复旧 A8 paired session。再下一步
才扩大 Agent 源码导航、branch/control/ablate/minimize 和长时 Investigation。
