# M5.21p：真实 Runtime RiskWitness 正向可达性校准

日期：2026-08-11
状态：公开校准完成；不进入方法排名；`formal_ready=false`

## 目标

M5.21j 对三份已有 bundle 的重投影全部得到 `not-reached`，而正向单测只使用人工构造
Trace。本阶段要回答一个更小但必要的问题：当前 official etcd/raft Adapter、自然时间、消息、
crash/restart 和 qualified executor 能否真实到达冻结的
`leader-change-with-inflight-proposal` RiskWitness。

本阶段不更改 Runtime、Adapter、RiskWitness spec/projector、PSS 或 Oracle，也不调用模型。

## 输入、处理、输出

```text
固定 official etcd/raft + seed 1 + 64 decisions
  + single-write workload + 已有 Qualification/FaultEnvelope
                         |
                         v
现有 qualified executor
  + decision 29 从 enabled set 选 Crash(n1)
  + 自然 temporal/message/effect 推进
  + decision 54 从 enabled set 选 Restart(n1)
                         |
                         v
strict Replay + ExecutionBundle/v2
  + 已有 target projector + family witness validator
                         |
                         v
reached milestones + workload/fault/PSS/work 分量
```

`workload-risk-witness-calibration` 是 composition-root 中的公开专用校准 strategy。它只对
`-decisions 64 -policy-seed 1` 开放，防止将一条已冻结校准误用为可变搜索方法。它仍只能
选择 Runtime 当前枚举且经 FaultEnvelope 准入的 Action；没有直接设置 leader、term、timeout 或节点状态。

## 实际结果

| 分量 | 结果 |
|---|---:|
| charged decisions | 64 |
| primary / replay work | 66 / 66 |
| strict replay | stable |
| workload | planned 1, offered 1, completed 0, pending 1 |
| faults | crash 1, drop/duplicate/partition 0 |
| Core PSS unique states | 48 |
| model calls | 0 |

真实投影得到三个有序 milestone：

| step | milestone | evidence |
|---:|---|---|
| 28 | `workload-invoked-at-coordinator` | `Invoke(n1/incarnation 1)` 时 n1 为 leader |
| 53 | `coordinator-changed-while-inflight` | workload 尚未 return，n2 以更高 term 成为 leader |
| 54 | `old-coordinator-restarted-after-change` | n1 从 stopped/incarnation 1 变为 running/incarnation 2 |

因此 RiskWitness 为 `reached`。小型冻结结果位于
[`benchmarks/experiments/etcdraft-v2-risk-witness-reachability-m5.21p/`](../benchmarks/experiments/etcdraft-v2-risk-witness-reachability-m5.21p/README.md)；
集成测试会通过真实 primary/replay 重算并逐字段对账。大型 report/bundle 不进入 Git。

## 结论边界

该结果证明：

- 当前控制面确实能在 workload inflight 时通过自然时间形成换主并重启旧主；
- target projector 不是恒空谓词，三个 milestone 能从真实证据投影；
- M5.21j 的 `not-reached` 可以解释为当时搜索没有进入该风险区，而不是 witness 必然不可达。

该结果不证明：

- 人工固定 strategy 比 Random、自适应基线或 Agent 更好；
- `reached` 等于发现协议故障、Oracle finding 或测试质量高；
- 48 个 PSS states 是覆盖百分比或绝对充分性；
- private holdout、formal Agent runner 或第二 strict CFT target 已完成。

## 下一阶段

暂停扩展 evaluator schema。下一步先冻结 Agent 与强确定性自适应基线可共用的最小
RiskWitness progress/current-frontier 视图，并用无模型消费者做 effective-execution authority gate：
新 proposal 权限若不能机械改变真实 Policy/轨迹，就删除或降级，不运行模型。
curator pack 继续作为仓库外输入缺口；第二 strict CFT target 不以 Agent 结果为前提。
与冻结规则 112 一致，在 formal holdout readiness 和第二 strict CFT 迁移门均完成前，
不将 witness feedback 交给 Agent，也不做真实模型调用。
