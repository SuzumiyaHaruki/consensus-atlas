# 文档导航

`CURRENT_STAGE.md` 是当前工作入口；阶段总结是不可改写历史，设计文档描述当前接口和规则。
公开 pilot 用于验证链路，不等同于正式 holdout 方法结果。

## 推荐阅读顺序

1. [当前阶段](CURRENT_STAGE.md)
2. [M5.13 单调用 DeepSeek Planner](stage-m5.13-one-call-deepseek-planner.md)
3. [M5.12 受限 Planner Proposal 编译边界](stage-m5.12-restricted-planner-boundary.md)
4. [M5.11 确定性 Random 基线](stage-m5.11-deterministic-random-baseline.md)
5. [M5.10 可保存的 v2 实验执行器](stage-m5.10-v2-experiment-executor.md)
6. [M5.9d 通用跨运行状态聚合](stage-m5.9d-generic-cross-run-aggregate.md)
7. [M5.9c 可信在线 Core PSS 采样](stage-m5.9c-online-core-pss-sampling.md)
8. [M5.9b 通用状态发现账本](stage-m5.9b-generic-discovery-ledger.md)
9. [M5.9a v1 消费者冻结与第一批减负](stage-m5.9a-v1-consumer-freeze.md)
10. [M5.8b efficient/epaxos message-port worker](stage-m5.8b-efficient-epaxos-message-port.md)
11. [M5.8a efficient/epaxos 可行性检查](stage-m5.8a-efficient-epaxos-feasibility.md)
12. [M5.7a Core PSS IR 与 etcd/raft Mapping](stage-m5.7a-core-pss-etcdraft.md)
13. [M5.7 接入与 PSS 目标收敛](stage-m5.7-onboarding-scope-convergence.md)
14. [M5.6d Control Path 能力语义机械化](stage-m5.6d-control-path-capabilities.md)
15. [能力分级与灰盒 Control Port 设计](graybox-control-ports.md)
16. [M5.6c Gateway Actuator 重叠与失败边界](stage-m5.6c-gateway-actuator.md)
17. [M5.6b Typed Partition 与 Gateway 拓扑绑定](stage-m5.6b-typed-partition-binding.md)
18. [M5.6a 统一 Partition Action 决策实验](stage-m5.6a-unified-partition-action.md)
19. [M5.5c 黑盒 connection gateway 决策实验](stage-m5.5c-blackbox-connection-gateway.md)
20. [M5.5b 最小黑盒 Target Envelope](stage-m5.5b-blackbox-target-envelope.md)
21. [M5.5a 场景表面与确定性控制分层](stage-m5.5a-control-surface-grading.md)
22. [M5.4e 第二实现能力矩阵与减负收口](stage-m5.4e-second-adapter-closure.md)
23. [M5.4d HashiCorp Raft 确定性边界决策](stage-m5.4d-hashicorp-determinism-boundary.md)
24. [M5.4c HashiCorp Raft 生命周期与部分资格](stage-m5.4c-hashicorp-lifecycle-qualification.md)
25. [M5.4b.1 行为保持的减负整理](stage-m5.4b.1-code-reduction.md)
26. [M5.4b HashiCorp Raft Runtime 消息与 Apply 闭环](stage-m5.4b-hashicorp-runtime-apply.md)
27. [M5.4a HashiCorp Raft 控制表面探针](stage-m5.4a-hashicorp-raft-probe.md)
28. [M5.3 Adapter 机械准入与接入模板](stage-m5.3-adapter-qualification.md)
29. [M5.2.5 v1/v2 冻结场景对照与删除门](stage-m5.2.5-v1-v2-comparison.md)
30. [M5.2.4 官方 etcd/raft proposal/application v2 切片](stage-m5.2.4-etcdraft-proposal-v2.md)
31. [M5.2.3 官方 etcd/raft 生命周期 v2 切片](stage-m5.2.3-etcdraft-lifecycle-v2.md)
32. [M5.2.2 官方 etcd/raft 多节点消息 v2 切片](stage-m5.2.2-etcdraft-multinode-messages-v2.md)
33. [M5.2.1 官方 etcd/raft 单节点 v2 切片](stage-m5.2.1-etcdraft-single-node-v2.md)
34. [M5.1 Control Runtime v2 基础](stage-m5.1-control-runtime-v2-foundation.md)
35. [Control Runtime v2 设计](control-runtime-v2.md)
36. [总体规划](ConsensusAtlas-总体规划.md)
37. [架构](architecture.md)
38. [Defect Benchmark](defect-benchmark.md)
39. [M4.12 Blind Planner](stage-m4.12-blind-planner-v1.md)
40. [M4.13 Blind Benchmark 预检](stage-m4.13-blind-benchmark-preflight.md)
41. [M4.14 Blind Trial Replay](stage-m4.14-blind-trial-replay.md)
42. [M4.15 协议输入边界与代码整理](stage-m4.15-protocol-boundary-cleanup.md)
43. [M4.16 评测身份冻结预检](stage-m4.16-benchmark-identity-freeze.md)
44. [M4.17 正式 benchmark 机械准入](stage-m4.17-formal-benchmark-readiness.md)

## 设计与实现

- [M5.13 单调用 DeepSeek Planner](stage-m5.13-one-call-deepseek-planner.md)：真实单次模型调用、
  受限 Scope 投影、transport/usage audit 和运行期不可达的显式部分工作账本。
- [M5.12 受限 Planner Proposal 编译边界](stage-m5.12-restricted-planner-boundary.md)：可信 scope、
  strict proposal decode、机械 Policy 编译、显式 rejection/execution failure 与 partial-work 计费。
- [M5.11 确定性 Random 基线](stage-m5.11-deterministic-random-baseline.md)：独立 public policy
  seed、canonical enabled-set hash、拒绝采样与 Runtime entropy 隔离。
- [M5.10 可保存的 v2 实验执行器](stage-m5.10-v2-experiment-executor.md)：用全新 Adapter/Runtime
  执行、在线采样、严格 replay 并保存 digest-bound `measurement-complete` 报告。
- [M5.9d 通用跨运行状态聚合](stage-m5.9d-generic-cross-run-aggregate.md)：显式计费每个
  measured decision，保持 v1 结果并加入等预算 v2 Core PSS union 见证。
- [M5.9c 可信在线 Core PSS 采样](stage-m5.9c-online-core-pss-sampling.md)：在每个 v2 Action 后
  可信配对 Snapshot/Evidence，并以严格 Runtime/sample replay 冻结三节点行为。
- [M5.9b 通用状态发现账本](stage-m5.9b-generic-discovery-ledger.md)：把单轨迹 ledger 改为
  `step/key/state` 输入，冻结 Raft v1 适配结果并加入真实三节点 Core PSS 复用见证。
- [Control Runtime v2](control-runtime-v2.md)：协议无关 Action/Item、消息、受约束虚拟时间、
  分域 EntropySource、生命周期、副作用、Adapter yield、Conformance、Evidence 与迁移门槛。
- [M5.9a v1 消费者冻结与第一批减负](stage-m5.9a-v1-consumer-freeze.md)：冻结 28 条生产依赖边、
  合并重复 qualification CLI，并定义 PSS discovery ledger 的下一迁移门。
- [M5.8b efficient/epaxos message-port worker](stage-m5.8b-efficient-epaxos-message-port.md)：用
  大小真实 Commit 帧否证 chunk-as-message，并冻结 codec-aware framing 的职责和资格边界。
- [M5.8a efficient/epaxos 可行性检查](stage-m5.8a-efficient-epaxos-feasibility.md)：冻结官方版本、
  构建/smoke/接口事实、机械 blocker 与只允许 test-only message-port 实验的停止线。
- [M5.7a Core PSS IR 与 etcd/raft Mapping](stage-m5.7a-core-pss-etcdraft.md)：实现可信 Runtime
  Control Context、封闭 Semantic Graph、规范化摘要与首个真实映射，并冻结 adapterkit 抽取边界。
- [M5.7 接入与 PSS 目标收敛](stage-m5.7-onboarding-scope-convergence.md)：固定边际接入面、共享
  Adapter kit 职责、Core PSS IR 和 EPaxos 抽象验收门。
- [能力分级与灰盒 Control Port](graybox-control-ports.md)：统一 Action 下的 surface/grade/guarantee、
  默认灰盒接入、端口职责、可信约束和 M5.6d—M5.8 路线。
- [Protocol Contracts](protocol-contracts.md)：Contract、Binding 与自动接入边界。
- [Coverage Kernel](coverage-kernel.md)：固定 Profile denominator、证据和 Ledger。
- [Test Plan Campaign](test-plan-campaign.md)：受限 DSL、Campaign 与 Blind Planner 接口。
- [Raft Campaign](raft-campaign.md)：Raft Family Profile/PSS 与专家计划。
- [Metrics](metrics.md)：PSS/PSD、固定义务覆盖和外部缺陷评价的分工。
- [etcd/raft](etcdraft.md)：官方 RawNode Driver 的接入语义。
- [Experiments](experiments.md)：Random/DFS 的共同预算状态发现实验。

## 阶段与公开实验记录

- [M5.13 单调用 DeepSeek Planner](stage-m5.13-one-call-deepseek-planner.md)
- [M5.12 受限 Planner Proposal 编译边界](stage-m5.12-restricted-planner-boundary.md)
- [M5.11 确定性 Random 基线](stage-m5.11-deterministic-random-baseline.md)
- [M5.10 可保存的 v2 实验执行器](stage-m5.10-v2-experiment-executor.md)
- [M5.9d 通用跨运行状态聚合](stage-m5.9d-generic-cross-run-aggregate.md)
- [M5.9c 可信在线 Core PSS 采样](stage-m5.9c-online-core-pss-sampling.md)
- [M5.9b 通用状态发现账本](stage-m5.9b-generic-discovery-ledger.md)
- [M5.9a v1 消费者冻结与第一批减负](stage-m5.9a-v1-consumer-freeze.md)
- [M5.8b efficient/epaxos message-port worker](stage-m5.8b-efficient-epaxos-message-port.md)
- [M5.8a efficient/epaxos 可行性检查](stage-m5.8a-efficient-epaxos-feasibility.md)
- [M5.7a Core PSS IR 与 etcd/raft Mapping](stage-m5.7a-core-pss-etcdraft.md)
- [M5.7 接入与 PSS 目标收敛](stage-m5.7-onboarding-scope-convergence.md)
- [M5.6d Control Path 能力语义机械化](stage-m5.6d-control-path-capabilities.md)
- [M5.6c Gateway Actuator 重叠与失败边界](stage-m5.6c-gateway-actuator.md)
- [M5.6b Typed Partition 与 Gateway 拓扑绑定](stage-m5.6b-typed-partition-binding.md)
- [M5.6a 统一 Partition Action 决策实验](stage-m5.6a-unified-partition-action.md)
- [M5.5c 黑盒 connection gateway 决策实验](stage-m5.5c-blackbox-connection-gateway.md)
- [M5.5b 最小黑盒 Target Envelope](stage-m5.5b-blackbox-target-envelope.md)
- [M5.5a 场景表面与确定性控制分层](stage-m5.5a-control-surface-grading.md)
- [M5.4e 第二实现能力矩阵与减负收口](stage-m5.4e-second-adapter-closure.md)
- [M5.4d HashiCorp Raft 确定性边界决策](stage-m5.4d-hashicorp-determinism-boundary.md)
- [M5.4c HashiCorp Raft 生命周期与部分资格](stage-m5.4c-hashicorp-lifecycle-qualification.md)
- [M5.4b.1 行为保持的减负整理](stage-m5.4b.1-code-reduction.md)
- [M5.4b HashiCorp Raft Runtime 消息与 Apply 闭环](stage-m5.4b-hashicorp-runtime-apply.md)
- [M5.4a HashiCorp Raft 控制表面探针](stage-m5.4a-hashicorp-raft-probe.md)
- [M5.3 Adapter 机械准入与接入模板](stage-m5.3-adapter-qualification.md)
- [M5.2.5 v1/v2 冻结场景对照与删除门](stage-m5.2.5-v1-v2-comparison.md)
- [M5.2.4 官方 etcd/raft opaque proposal 与 application v2 切片](stage-m5.2.4-etcdraft-proposal-v2.md)
- [M5.2.3 官方 etcd/raft durable image 与生命周期 v2 切片](stage-m5.2.3-etcdraft-lifecycle-v2.md)
- [M5.2.2 官方 etcd/raft 多节点消息 v2 切片](stage-m5.2.2-etcdraft-multinode-messages-v2.md)
- [M5.2.1 官方 etcd/raft 单节点 v2 切片](stage-m5.2.1-etcdraft-single-node-v2.md)
- [M5.1 Control Runtime v2 协议无关基础](stage-m5.1-control-runtime-v2-foundation.md)
- [M4.7 Defect Benchmark 基础](stage-m4.7-defect-foundation.md)
- [M4.8 校准 pilot](stage-m4.8-etcdraft-calibration-pilot.md)
- [M4.8.1 可信链加固](stage-m4.8.1-trust-chain-hardening.md)
- [M4.9 ReadIndex 回归](stage-m4.9-etcdraft-readindex.md)
- [M4.10 虚拟时间](stage-m4.10-virtual-time.md)
- [M4.11 Ready.MustSync](stage-m4.11-etcdraft-ready-must-sync.md)
- [M4.11.1 可信边界加固](stage-m4.11.1-trust-hardening.md)
- [M4.12 Blind Planner](stage-m4.12-blind-planner-v1.md)
- [M4.13 Blind Benchmark 预检](stage-m4.13-blind-benchmark-preflight.md)
- [M4.14 Blind Trial Replay](stage-m4.14-blind-trial-replay.md)
- [M4.15 协议输入边界与代码整理](stage-m4.15-protocol-boundary-cleanup.md)
- [M4.16 评测身份冻结预检](stage-m4.16-benchmark-identity-freeze.md)
- [M4.17 正式 benchmark 机械准入](stage-m4.17-formal-benchmark-readiness.md)

公开 benchmark 工件位于 `benchmarks/pilots/`；历史/候选 catalog 位于
`benchmarks/candidates/`。`artifacts/` 是可再生产生的本地输出与缓存，除明确冻结的压缩
摘要外不应作为版本化证据来源。
