# M5.22a OmniPaxos 有界随机策略校准

本目录只保存一份小型、可审查的实验摘要；完整 Report 和 ExecutionBundle 不提交到 Git。对应集成测试
从真实 `omnipaxos 0.2.2` 三节点 worker 运行并冻结所有关键 digest：

```bash
go test ./qualifications/omnipaxosv2 \
  -run 'Test(LegacyOpenPolicyRemainsRejectedBeforeOmniPaxosFactory|BoundedActionClassAdmitsPartialOmniPaxosTarget)$' \
  -count=1 -v
```

旧 `action-class-random-policy/v1` 仍按开放选择面处理；在只声明六项 validated capability 时，它于
Adapter factory 之前被拒绝。新 `action-class-random-policy/v2` 将可选 ActionKind 集合作为策略身份的一部分，
本次只允许 `deliver-message`、`fire-temporal` 和 `invoke`，因此能机械推导精确 capability 下界并进入
同一 qualified executor。

真实执行完成 29 decisions、28 个 Core PSS states 和一个 opaque workload，fresh Replay 稳定。轨迹只含
1 个 Invoke、24 个 DeliverMessage 和 4 个 FireTemporal；不含 Drop、Duplicate、Crash、Restart、Partition
或 HostEffect。

这是 stochastic backend 在 partial-qualified target 上的 admission/selection 校准。它不是 Agent 实验、
跨目标 Campaign、搜索方法优势、缺陷检出结果或绝对测试质量评分。
