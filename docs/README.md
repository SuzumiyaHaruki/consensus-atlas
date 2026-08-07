# 文档导航

`CURRENT_STAGE.md` 是当前工作入口；阶段总结是不可改写历史，设计文档描述当前接口和规则。
公开 pilot 用于验证链路，不等同于正式 holdout 方法结果。

## 推荐阅读顺序

1. [当前阶段](CURRENT_STAGE.md)
2. [能力分级与灰盒 Control Port 设计](graybox-control-ports.md)
3. [M5.6c Gateway Actuator 重叠与失败边界](stage-m5.6c-gateway-actuator.md)
4. [M5.6b Typed Partition 与 Gateway 拓扑绑定](stage-m5.6b-typed-partition-binding.md)
5. [M5.6a 统一 Partition Action 决策实验](stage-m5.6a-unified-partition-action.md)
6. [M5.5c 黑盒 connection gateway 决策实验](stage-m5.5c-blackbox-connection-gateway.md)
7. [M5.5b 最小黑盒 Target Envelope](stage-m5.5b-blackbox-target-envelope.md)
8. [M5.5a 场景表面与确定性控制分层](stage-m5.5a-control-surface-grading.md)
9. [M5.4e 第二实现能力矩阵与减负收口](stage-m5.4e-second-adapter-closure.md)
10. [M5.4d HashiCorp Raft 确定性边界决策](stage-m5.4d-hashicorp-determinism-boundary.md)
11. [M5.4c HashiCorp Raft 生命周期与部分资格](stage-m5.4c-hashicorp-lifecycle-qualification.md)
12. [M5.4b.1 行为保持的减负整理](stage-m5.4b.1-code-reduction.md)
13. [M5.4b HashiCorp Raft Runtime 消息与 Apply 闭环](stage-m5.4b-hashicorp-runtime-apply.md)
14. [M5.4a HashiCorp Raft 控制表面探针](stage-m5.4a-hashicorp-raft-probe.md)
15. [M5.3 Adapter 机械准入与接入模板](stage-m5.3-adapter-qualification.md)
16. [M5.2.5 v1/v2 冻结场景对照与删除门](stage-m5.2.5-v1-v2-comparison.md)
17. [M5.2.4 官方 etcd/raft proposal/application v2 切片](stage-m5.2.4-etcdraft-proposal-v2.md)
18. [M5.2.3 官方 etcd/raft 生命周期 v2 切片](stage-m5.2.3-etcdraft-lifecycle-v2.md)
19. [M5.2.2 官方 etcd/raft 多节点消息 v2 切片](stage-m5.2.2-etcdraft-multinode-messages-v2.md)
20. [M5.2.1 官方 etcd/raft 单节点 v2 切片](stage-m5.2.1-etcdraft-single-node-v2.md)
21. [M5.1 Control Runtime v2 基础](stage-m5.1-control-runtime-v2-foundation.md)
22. [Control Runtime v2 设计](control-runtime-v2.md)
23. [总体规划](ConsensusAtlas-总体规划.md)
24. [架构](architecture.md)
25. [Defect Benchmark](defect-benchmark.md)
26. [M4.12 Blind Planner](stage-m4.12-blind-planner-v1.md)
27. [M4.13 Blind Benchmark 预检](stage-m4.13-blind-benchmark-preflight.md)
28. [M4.14 Blind Trial Replay](stage-m4.14-blind-trial-replay.md)
29. [M4.15 协议输入边界与代码整理](stage-m4.15-protocol-boundary-cleanup.md)
30. [M4.16 评测身份冻结预检](stage-m4.16-benchmark-identity-freeze.md)
31. [M4.17 正式 benchmark 机械准入](stage-m4.17-formal-benchmark-readiness.md)

## 设计与实现

- [Control Runtime v2](control-runtime-v2.md)：协议无关 Action/Item、消息、受约束虚拟时间、
  分域 EntropySource、生命周期、副作用、Adapter yield、Conformance、Evidence 与迁移门槛。
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
