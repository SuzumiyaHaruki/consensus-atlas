# 文档导航

`CURRENT_STAGE.md`、README、架构和总体规划描述当前可运行系统。阶段总结是不可改写的历史记录，可能
引用已经由 M5.16R 删除的源码路径；这不表示旧 API 仍然存在。

## 当前主线阅读顺序

1. [当前阶段：M5.18b4R race gate topology](CURRENT_STAGE.md)
2. [M5.18b4R race gate topology](stage-m5.18b4r-race-gate-topology.md)
3. [M5.18b4 preference ablation request freeze](stage-m5.18b4-request-freeze.md)
4. [M5.18b4-pre Agent experiment trust corrections](stage-m5.18b4-pre-trust-corrections.md)
5. [M5.18b3 unseen follow-up baseline](stage-m5.18b3-unseen-follow-up.md)
6. [M5.18b2 defect-blind batch feedback](stage-m5.18b2-defect-blind-batch-feedback.md)
7. [M5.18b1 one-shot Agent transport](stage-m5.18b1-one-shot-agent-transport.md)
8. [M5.18b0 Guarded TestIntent compiler](stage-m5.18b0-guarded-intent-compiler.md)
9. [M5.18a 可信方法评价前提](stage-m5.18a-method-evaluation-prerequisites.md)
10. [M5.17c2 Batch PSS Guidance](stage-m5.17c2-batch-pss-guidance.md)
11. [M5.17c1 Corpus 可信前提](stage-m5.17c1-corpus-trust-prerequisites.md)
12. [M5.17c0 Experiment 语义加固](stage-m5.17c0-experiment-semantics.md)
13. [M5.17bR2 在线旧路径删除](stage-m5.17b-r2-experiment-path-pruning.md)
14. [架构](architecture.md)
15. [Control Runtime v2](control-runtime-v2.md)
16. [总体规划](ConsensusAtlas-总体规划.md)
17. [M5.17b Trace Mutation](stage-m5.17b-trace-mutation.md)
18. [M5.17a Action-class Random](stage-m5.17a-action-class-random.md)
19. [M5.16 ExecutionBundle 与可信评测](stage-m5.16-execution-bundle.md)
20. [M5.16R v1 实现锥体删除](stage-m5.16r-legacy-removal.md)
21. [M5.15 Semantic Workload](stage-m5.15-semantic-workload.md)

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
