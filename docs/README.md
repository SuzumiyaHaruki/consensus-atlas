# 文档导航

`CURRENT_STAGE.md` 是当前工作入口；阶段总结是不可改写历史，设计文档描述当前接口和规则。
公开 pilot 用于验证链路，不等同于正式 holdout 方法结果。

## 推荐阅读顺序

1. [当前阶段](CURRENT_STAGE.md)
2. [总体规划](ConsensusAtlas-总体规划.md)
3. [架构](architecture.md)
4. [Defect Benchmark](defect-benchmark.md)
5. [M4.12 Blind Planner](stage-m4.12-blind-planner-v1.md)
6. [M4.13 Blind Benchmark 预检](stage-m4.13-blind-benchmark-preflight.md)
7. [M4.14 Blind Trial Replay](stage-m4.14-blind-trial-replay.md)
8. [M4.15 协议输入边界与代码整理](stage-m4.15-protocol-boundary-cleanup.md)
9. [M4.16 评测身份冻结预检](stage-m4.16-benchmark-identity-freeze.md)
10. [M4.17 正式 benchmark 机械准入](stage-m4.17-formal-benchmark-readiness.md)

## 设计与实现

- [Protocol Contracts](protocol-contracts.md)：Contract、Binding 与自动接入边界。
- [Coverage Kernel](coverage-kernel.md)：固定 Profile denominator、证据和 Ledger。
- [Test Plan Campaign](test-plan-campaign.md)：受限 DSL、Campaign 与 Blind Planner 接口。
- [Raft Campaign](raft-campaign.md)：Raft Family Profile/PSS 与专家计划。
- [Metrics](metrics.md)：PSS/PSD、固定义务覆盖和外部缺陷评价的分工。
- [etcd/raft](etcdraft.md)：官方 RawNode Driver 的接入语义。
- [Experiments](experiments.md)：Random/DFS 的共同预算状态发现实验。

## 阶段与公开实验记录

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
