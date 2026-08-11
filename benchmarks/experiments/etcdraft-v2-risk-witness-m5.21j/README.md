# M5.21j etcd/raft RiskWitness 重投影

本目录只保存从三份既有、严格重放稳定的 M5.21f/M5.21h bundle 机械派生的小型结果。阶段没有调用模型，
没有运行新的 SUT，也没有改变 Runtime、Adapter、搜索策略、PSS 或 Oracle。

## 冻结语义

Raft Family Pack 固定风险 `leader-change-with-inflight-proposal` 的三个 milestone：

```text
workload-invoked-at-coordinator
        < coordinator-changed-while-inflight
        < old-coordinator-restarted-after-change
```

etcd/raft target projector 只能从实际 `Invoke` action、Adapter-owned role/term/incarnation evidence、
client response step 和 Runtime node transition 产生 milestone evidence。通用验证器只检查 spec、target、
bundle、projector、step ordering 和 digest，并机械计算 `reached/not-reached`；它不解码 Raft evidence。

## 结果

| execution | backend | witness | 第一个缺失 milestone |
|---|---|---|---|
| attempt 1 | ActionClass | `not-reached` | `workload-invoked-at-coordinator` |
| attempt 2 | ActionClass | `not-reached` | `workload-invoked-at-coordinator` |
| attempt 2 | Uniform | `not-reached` | `workload-invoked-at-coordinator` |

三份 bundle 的 workload 都是 `planned=1、offered=0、completed=0、pending=1`，因此 target projector
没有把计划中的 workload、独立出现的 crash/restart 或角色状态误报为完整风险场景。完整、可重算结果见
`comparison.json`。

## 边界

- 结果只说明三份既有执行没有到达冻结风险，不说明搜索方法优劣或协议正确性；
- RiskWitness 是语义可达性证据，不是 Oracle、Coverage 分母或综合评分；
- 正向 fixture 机械验证了完整三 milestone 时序可以得到 `reached`，并验证 client response 已完成后
  的换主不会被称为 `while-inflight`；
- 当前只实现 etcd/raft target projector。大量增加 Raft 专用 witness 前仍必须通过第二个 strict CFT
  实现迁移门。
