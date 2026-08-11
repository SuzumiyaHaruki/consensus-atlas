# M5.22b：同一协议无关 Intent 的双目标执行

日期：2026-08-11

## 结论

M5.22b 首次让一份不含 Raft/Sequence-Paxos 字段的父 `GuardedTestIntent` 经过可信投影，在官方 etcd/raft
和 OmniPaxos 两个真实目标上完成 opaque workload 与 fresh Replay。它复用现有 target-bound compiler、
Qualification admission、唯一 Control Runtime、Core PSS Mapper 和 DecisionProjector，没有增加第二套
scheduler、PSS 或 Oracle。

这一步也校正了“统一 Action”的含义：共同 Agent 动作面可以统一，但低层 target lowering 不必逐项完全
相同。etcd/raft 的 Ready lifecycle 需要 `CompleteEffect`，OmniPaxos 当前同步 worker 不需要。该差异必须
作为显式、资格验证过、确定性优先执行的 target-local plumbing 保留，不能暴露成 Agent 的共同搜索动作，
也不能隐藏在 Adapter 内自动执行。

## 共同视图与父 Intent

`CrossTargetPlannerView/v1` 从至少两个已验证 `AgentSemanticView` 机械求交：

- KnowledgePack 与 Catalog digest 必须相同；
- source view digest 必须不同且只以 opaque digest 出现；
- validated capabilities 取集合交集；
- available actions 先取 Manifest 交集，再限制为共同 backend 真正 selectable 的 Action；
- backend ID、动作面、预算边界和 FaultEnvelope 必须逐字段相同；
- 交集为空、来源篡改或 hard constraint 越界均失败关闭。

本次 Agent-facing view 只含六项共同能力、三类 Action 和一个 backend：

```text
capabilities: strict-yield / pure-enabled / natural-temporal /
              runtime-owned-message / strict-replay / opaque-invoke
actions:      Invoke / DeliverMessage / FireTemporal
backend:      bounded-action-class
```

集成测试直接检查 view JSON 不含 etcd、Raft、OmniPaxos、Paxos、Adapter、Profile、PSS 或 Oracle 标识。

父 Intent 固定 96 decisions、零 fault envelope、同一 backend preference 和同一 risk。可信投影只替换
target view identity 并重新 seal child intent，随后调用已有 `CompileGuardedTestIntentV2`。两个 target plan
的 digest 因 view/child identity 不同，但 risk、backend、strategy、预算、FaultEnvelope、required
capabilities/actions、preference misses 和 compiler work 必须相同。

## Target lowering 边界

```text
one parent intent
        |
        +--> target A child plan --> common actions + qualified local plumbing --> existing executor
        |
        +--> target B child plan --> common actions                            --> existing executor
```

etcd/raft lowering 将 `CompleteEffect` 加入 bounded policy 的 selectable surface，并放在随机选择前的 Priority
中。它不是父 Intent 的 required/preferred Action；由于 Effect 尚无独立 capability，admission 保守绑定
etcd/raft 的 8 项完整 required set。移除该 plumbing 的负校准在 0 decision 得到
`policy-surface-exhausted`，三个共同 Action 均未到达。

OmniPaxos lowering 无额外 plumbing，只绑定其六项 validated capability。两侧都不允许 target lowering
增加 Crash、Restart、Drop、Duplicate、Partition 或其他探索性动作。

## 真实结果

| target | decisions | primary/replay | workload | Replay | target-local PSS | Action counts |
|---|---:|---:|---:|---|---:|---|
| etcd/raft v3.6.0 | 42 | 44 / 44 | 1/1 | stable | 31 | Invoke 1, Deliver 6, Temporal 13, Effect 22 |
| OmniPaxos 0.2.2 | 29 | 31 / 31 | 1/1 | stable | 28 | Invoke 1, Deliver 24, Temporal 4 |

31 与 28 只在各自 PSS identity 内描述发现情况，不能相加、计算 Jaccard 或用大小给协议/方法排名。跨目标
共同字段只包括父 Intent、语义等价 plan 字段、逻辑上限、是否完成、Replay 稳定性和成本账本。

## 未证明

- 没有 LLM 调用，父 Intent 是冻结校准输入；
- 没有建立可恢复的 multi-target Campaign runner；
- 没有比较 Agent、Random、DFS 或人工计划；
- 没有证明两个 PSS 状态空间等价；
- 没有 defect/candidate/control verdict；
- 没有证明 BFT、leaderless 或全自动 onboarding。

## 下一阶段

M5.22c 只把该父 Intent→target lowering→execution 的组合放入现有 Campaign 的 durable planned-attempt
边界，生成一个包含两个 target-local attempt 的共同成本摘要。不得新增另一套 Coordinator，也不得把完整
bundle 复制进跨目标 summary。若现有 CampaignConfig 的单 target identity 无法表达该组合，应先形成一个
外层 composition ledger，而不是改写历史 Campaign identity。
