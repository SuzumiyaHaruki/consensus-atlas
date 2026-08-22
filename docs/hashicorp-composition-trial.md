# HashiCorp Raft 无模型 composition 试接入

阶段：M4n21

## 目的

本试接入不把 HashiCorp Raft 宣布为活动 Agent Target，也不补写虚假的 Replay 能力。它只检查：
在 etcd/raft 与 OmniPaxos 之外接入一个已有 Adapter 时，是否必须修改公共 Agent/执行主链。

## 机械结果

`go test ./adapters/hashicorpraftv2 ./qualifications/hashicorpraftv2 -count=1` 通过。现有
qualification 稳定得到：

```text
Total=9 Required=8 Validated=3 Unsupported=6 Qualified=false
```

已验证能力为：

- Runtime-owned message；
- crash/restart incarnation；
- opaque Invoke boundary。

未支持能力为：

- strict yield evidence；
- pure enabled check；
- natural temporal progress；
- strict decision Replay；
- audited entropy Replay；
- formal process isolation。

## 接入清单

| Target-local 项目 | 当前状态 | 结论 |
|---|---|---|
| Adapter factory | 已有 | `NewWithConfig` 可直接使用公共 Runtime |
| workload input | 部分已有 | 有 opaque `InputPayload`，但没有活动 `WorkloadRouter` |
| Observation projector | 缺失 | 当前只有 Adapter evidence 与 `fsm-apply` observation |
| Decision projector | 缺失 | applied record 尚未形成公共 decision projection |
| Oracle registry | 缺失 | target 尚未注册 projector/monitor 组合 |
| protocol knowledge | 缺失 | 没有 Agent authoring JSON / Dossier |
| local source mount | 缺失 | 当前仍从 Go module 依赖构建，不是 `suts/` checkout |

## 结论

本轮没有发现必须修改以下公共组件的证据：

- Scenario Agent 状态机；
- recorded Schedule executor；
- Provider journal；
- Bundle / Replay 数据结构；
- common public progress。

因此公共 composition 边界没有被第三个 Adapter 立即推翻。HashiCorp Raft 当前不能成为正式 Agent Target，
首先是因为 SUT 的 wall-clock/random timeout 不受调度器控制，严格 Replay 未资格化；其次才是缺少上述
Target-local 语义组件。正确推进方式是先提供可测试性端口或本地可修改 SUT，再按清单补 Target-local
组件，而不是在公共 Core 中加入 HashiCorp 特判，也不能把 `Qualified=false` 改写成可用。

这次结果只验证接入成本和失败归属，不证明现有两个活动 Target 已覆盖所有共识协议。
