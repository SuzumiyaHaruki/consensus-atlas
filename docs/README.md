# 文档导航

`CURRENT_STAGE.md`、README、架构和总体规划描述当前可运行系统。当前长期分支是
`feature/agentic-consensus-testing`；M5.23R4b 及更早阶段总结是不可改写的历史记录，可能
引用已经由 M5.16R 删除的源码路径；这不表示旧 API 仍然存在。

## 最新阶段阅读顺序

1. [当前阶段：A0 Agentic 研究主线重置](CURRENT_STAGE.md)
2. [A0 详细设计与路线](stage-a0-agentic-research-reset.md)
3. [总体规划 Draft v1.72](ConsensusAtlas-总体规划.md)
4. [M5.23R4b Stateless Agent 进入共同 Campaign](stage-m5.23r4b-stateless-agent-campaign.md)
5. [M5.23g 真实 Agent multi-root 负结果](stage-m5.23g-real-agent-multi-root-pilot.md)
6. [M5.23f 受限 Search Agent 权限协议](stage-m5.23f-restricted-search-agent.md)
7. [M5.23b OmniPaxos bounded stateless DFS](stage-m5.23b-omnipaxos-stateless-dfs.md)
8. [M5.23a 有界无状态 DFS 基线](stage-m5.23a-bounded-stateless-dfs.md)
9. [M5.21q Risk Frontier 精确执行权限](stage-m5.21q-risk-frontier-authority.md)
10. [M5.21p RiskWitness 可达性校准](stage-m5.21p-risk-witness-reachability.md)

旧 R4a/R3/R2/R1、formal evaluator 和更早阶段仍在本目录保留，可按 Git 历史或下面的较早主线索引查阅；
它们不再构成 A1 的顺序前置阶段。

## 较早主线阅读顺序

1. [M5.21e Matched Agent/Zero-model Execution Pilot](stage-m5.21e-matched-agent-zero-pilot.md)
3. [M5.21d5 Single-call External Connectivity Calibration](stage-m5.21d5-single-call-connectivity.md)
4. [M5.21d4 Pre-plan Terminal Accounting](stage-m5.21d4-pre-plan-terminal-accounting.md)
5. [M5.21d3 Opt-in Durable-call Runner](stage-m5.21d3-opt-in-durable-runner.md)
6. [M5.21d2 etcd/raft Offline Durable Model Planner](stage-m5.21d2-etcdraft-offline-model-planner.md)
7. [M5.21d1 Durable Model Call Lifecycle](stage-m5.21d1-durable-model-call.md)
8. [M5.21c Durable Planned Attempt](stage-m5.21c-durable-planned-attempt.md)
9. [M5.21b prior-choice 可信归因](stage-m5.21b-prior-choice-attribution.md)
10. [M5.21a Campaign Planner View](stage-m5.21a-campaign-planner-view.md)
11. [M5.20 Campaign Observation](stage-m5.20-campaign-observation.md)
12. [M5.19d Campaign Summary/Runner](stage-m5.19d-campaign-summary-runner.md)
13. [M5.19c 首个真实 Campaign Provider](stage-m5.19c-real-campaign-provider.md)
14. [M5.19b Deterministic Campaign Coordinator](stage-m5.19b-campaign-coordinator.md)
15. [M5.19a Campaign crash-safe persistence](stage-m5.19a-campaign-persistence.md)
16. [M5.19 Campaign config/checkpoint 基础](stage-m5.19-campaign-foundation.md)
17. [M5.18b4 explicit opt-in pair runner](stage-m5.18b4-pair-runner.md)
18. [M5.18b4 pair ledger](stage-m5.18b4-pair-ledger.md)
19. [M5.18b4 frozen request consumer](stage-m5.18b4-request-consumer.md)
20. [M5.18b4R race gate topology](stage-m5.18b4r-race-gate-topology.md)
21. [M5.18b4 preference ablation request freeze](stage-m5.18b4-request-freeze.md)
22. [M5.18b4-pre Agent experiment trust corrections](stage-m5.18b4-pre-trust-corrections.md)
23. [M5.18b3 unseen follow-up baseline](stage-m5.18b3-unseen-follow-up.md)
24. [M5.18b2 defect-blind batch feedback](stage-m5.18b2-defect-blind-batch-feedback.md)
25. [M5.18b1 one-shot Agent transport](stage-m5.18b1-one-shot-agent-transport.md)
26. [M5.18b0 Guarded TestIntent compiler](stage-m5.18b0-guarded-intent-compiler.md)
27. [M5.18a 可信方法评价前提](stage-m5.18a-method-evaluation-prerequisites.md)
28. [M5.17c2 Batch PSS Guidance](stage-m5.17c2-batch-pss-guidance.md)
29. [M5.17c1 Corpus 可信前提](stage-m5.17c1-corpus-trust-prerequisites.md)
30. [M5.17c0 Experiment 语义加固](stage-m5.17c0-experiment-semantics.md)
31. [M5.17bR2 在线旧路径删除](stage-m5.17b-r2-experiment-path-pruning.md)
32. [架构](architecture.md)
33. [Control Runtime v2](control-runtime-v2.md)
34. [总体规划](ConsensusAtlas-总体规划.md)
35. [M5.17b Trace Mutation](stage-m5.17b-trace-mutation.md)
36. [M5.17a Action-class Random](stage-m5.17a-action-class-random.md)
37. [M5.16 ExecutionBundle 与可信评测](stage-m5.16-execution-bundle.md)
38. [M5.16R v1 实现锥体删除](stage-m5.16r-legacy-removal.md)
39. [M5.15 Semantic Workload](stage-m5.15-semantic-workload.md)

公开 calibration 工件见
[etcd/raft v2 M5.16](../benchmarks/pilots/etcdraft-v2-calibration-m5.16/README.md)。它只验证链路，不是
正式 holdout 或方法效果结果。
[M5.17a action-class random](../benchmarks/pilots/etcdraft-v2-action-class-random-m5.17a/README.md)
进一步保存了“PSS 状态更多但公开 candidate 未被检出”的负结果。
[M5.17b trace mutation](../benchmarks/experiments/etcdraft-v2-trace-mutation-m5.17b/README.md)
保存成功/不可执行变体和 source construction 在内的完整成本账本。
[M5.18a evaluator-owned calibration](../benchmarks/pilots/etcdraft-v2-method-evaluation-m5.18a/README.md)
冻结 MethodSpec、OperationHistory、build identity 与 fresh execution 评价链。
[M5.18b1 one-shot summary](../benchmarks/experiments/etcdraft-v2-agent-one-shot-m5.18b1/summary.json)
保存真实模型调用、编译与执行成本，以及 prompt 示例锚定的负面观察；它不是 Agent 效果证据。
[M5.18b2 feedback view](../benchmarks/experiments/etcdraft-v2-agent-feedback-m5.18b2/feedback.json)
保存从完整 bundle 重算的共同预算、完成度、成本和粗粒度 PSS 证据；它不包含缺陷或 Oracle 身份。
[M5.18b3 follow-up summary](../benchmarks/experiments/etcdraft-v2-agent-follow-up-m5.18b3/summary.json)
保存纠正终止语义后的 source 账本、未见 seed 4 的确定性负结果和完整计费边界；它没有调用模型。
[M5.18b4-pre summary](../benchmarks/experiments/etcdraft-v2-agent-b4-preflight-m5.18b4-pre/summary.json)
保存 honest action surface、seed-free plan、execution instance 和三轴 IntentOutcome；它不是 Agent 效果结果。
[M5.18b4 request freeze](../benchmarks/experiments/etcdraft-v2-agent-b4-freeze-m5.18b4/freeze.json)
保存两臂 exact-byte digest、共同 hard baseline、seed/budget 与 1-call/0-retry 边界；模型调用数为 0。
[M5.21d5 durable Agent Campaign](../benchmarks/experiments/etcdraft-v2-agent-campaign-m5.21d5/README.md)
保存首次真实 Campaign 单调用、可信编译/执行、持久化恢复和完整成本；它只是连通性校准。
[M5.21e matched Agent/zero-model pilot](../benchmarks/experiments/etcdraft-v2-agent-vs-zero-m5.21e/README.md)
保留“首个共同前缀完全相同、第二轮 Agent 输出契约漂移”的负结果和完整 terminal 计费。

## 当前实现主题

- [Control Runtime v2](control-runtime-v2.md)：Action/Item、消息所有权、自然时间、entropy、
  crash/restart、effect、Adapter yield 与 replay；
- [架构](architecture.md)：当前信任边界、唯一执行路径和协议耦合位置；
- [M5.7 Core PSS 收敛](stage-m5.7-onboarding-scope-convergence.md) 与
  [M5.7a etcd/raft Mapping](stage-m5.7a-core-pss-etcdraft.md)；
- [M5.10 v2 Experiment](stage-m5.10-v2-experiment-executor.md)；
- [M5.11 Random baseline](stage-m5.11-deterministic-random-baseline.md)；
- [M5.12 restricted Planner](stage-m5.12-restricted-planner-boundary.md) 与
  [M5.13 one-call LLM smoke](stage-m5.13-one-call-deepseek-planner.md)，二者在线实现已由 M5.17bR2 删除；
- [M5.21a Campaign Planner 最小视图](stage-m5.21a-campaign-planner-view.md)；
- [M5.21b prior-choice 可信归因](stage-m5.21b-prior-choice-attribution.md)；
- [M5.21c durable planned attempt](stage-m5.21c-durable-planned-attempt.md)；
- [M5.21d1 durable model call lifecycle](stage-m5.21d1-durable-model-call.md)；
- [M5.21d2 etcd/raft offline durable model planner](stage-m5.21d2-etcdraft-offline-model-planner.md)；
- [M5.21d3 opt-in durable-call runner](stage-m5.21d3-opt-in-durable-runner.md)；
- [M5.21d4 pre-plan terminal accounting](stage-m5.21d4-pre-plan-terminal-accounting.md)；
- [M5.21d5 single-call external connectivity calibration](stage-m5.21d5-single-call-connectivity.md)；
- [M5.21e matched Agent/zero-model execution pilot](stage-m5.21e-matched-agent-zero-pilot.md)；
- [M5.21l FormalBenchmarkContract](stage-m5.21l-formal-benchmark-contract.md)；
- [M5.21m FormalExposureAudit](stage-m5.21m-formal-exposure-audit.md)；
- [M5.21n Formal fresh evaluator](stage-m5.21n-formal-fresh-evaluator.md)；
- [M5.21o Formal multi-pair CLI](stage-m5.21o-formal-multi-pair-cli.md)；
- [M5.21p RiskWitness 可达性校准](stage-m5.21p-risk-witness-reachability.md)；
- [M5.21q Risk Frontier 精确执行权限](stage-m5.21q-risk-frontier-authority.md)；
- [M5.21r 第二 strict target 候选门](stage-m5.21r-second-target-candidate-gate.md)；
- [M5.14 admission](stage-m5.14-admission-and-pruning.md)；
- [M5.15 workload](stage-m5.15-semantic-workload.md)；
- [M5.16 bundle/evaluator](stage-m5.16-execution-bundle.md)；
- [M5.17a action-class random](stage-m5.17a-action-class-random.md)；
- [M5.17b trace mutation](stage-m5.17b-trace-mutation.md)。
- [M5.17c0 Experiment 语义](stage-m5.17c0-experiment-semantics.md)。
- [M5.17c1 Corpus 可信前提](stage-m5.17c1-corpus-trust-prerequisites.md)。
- [M5.17c2 Batch PSS Guidance](stage-m5.17c2-batch-pss-guidance.md)。
- [M5.18a 可信方法评价前提](stage-m5.18a-method-evaluation-prerequisites.md)。
- [M5.18b0 Guarded TestIntent compiler](stage-m5.18b0-guarded-intent-compiler.md)。
- [M5.18b1 one-shot Agent transport](stage-m5.18b1-one-shot-agent-transport.md)。
- [M5.18b2 defect-blind batch feedback](stage-m5.18b2-defect-blind-batch-feedback.md)。
- [M5.18b3 unseen follow-up baseline](stage-m5.18b3-unseen-follow-up.md)。
- [M5.18b4-pre Agent experiment trust corrections](stage-m5.18b4-pre-trust-corrections.md)。
- [M5.18b4 preference ablation request freeze](stage-m5.18b4-request-freeze.md)。
- [M5.18b4R 可审计 race 门禁拓扑](stage-m5.18b4r-race-gate-topology.md)。
- [M5.18b4 frozen request consumer](stage-m5.18b4-request-consumer.md)。
- [M5.18b4 pair orchestration/persistence](stage-m5.18b4-pair-ledger.md)。
- [M5.18b4 explicit opt-in pair runner](stage-m5.18b4-pair-runner.md)。
- [M5.19 Campaign config/checkpoint 基础](stage-m5.19-campaign-foundation.md)。
- [M5.19a Campaign crash-safe persistence](stage-m5.19a-campaign-persistence.md)。
- [M5.19b Deterministic Campaign Coordinator](stage-m5.19b-campaign-coordinator.md)。
- [M5.19c 首个真实 Campaign Provider](stage-m5.19c-real-campaign-provider.md)。
- [M5.19d Campaign Summary/Runner](stage-m5.19d-campaign-summary-runner.md)。

## 历史研究记录

M4、M5.1–M5.13 的文档保留了旧 Contract/Coverage/Campaign/Planner、v1/v2 migration、黑盒
gateway、第二实现和 EPaxos feasibility 的实验过程。M5.16R 已删除其中不再服务当前路径的可编译
实现；阅读这些文档时以当时阶段为准，不要按其中命令操作当前主分支。

常用历史入口：

- [M5.1 Runtime foundation](stage-m5.1-control-runtime-v2-foundation.md)
- [M5.2.4 etcd/raft proposal/application](stage-m5.2.4-etcdraft-proposal-v2.md)
- [M5.3 Adapter qualification](stage-m5.3-adapter-qualification.md)
- [M5.4e 第二实现能力矩阵](stage-m5.4e-second-adapter-closure.md)
- [M5.8a EPaxos feasibility](stage-m5.8a-efficient-epaxos-feasibility.md)
- [M5.8b EPaxos message framing](stage-m5.8b-efficient-epaxos-message-port.md)
- [M5.9a v1 consumer freeze](stage-m5.9a-v1-consumer-freeze.md)
- [M4 Defect Benchmark 设计](defect-benchmark.md)
- [Coverage Kernel 历史设计](coverage-kernel.md)
- [Test Plan/Campaign 历史设计](test-plan-campaign.md)

历史 JSON 继续位于 `benchmarks/`。可再生的大型完整 bundle 放在 ignored `artifacts/`，避免继续增加
仓库中的重复 trace 文本。
