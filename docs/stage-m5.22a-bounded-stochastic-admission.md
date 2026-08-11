# M5.22a：partial target 的有界随机策略入口

日期：2026-08-11

## 结论

M5.22a 完成了 M5.21zR 暴露出的第一个跨目标 Planner 前置缺口：旧随机、admissible-uniform、
action-class-random 和 trace-mutation policy 都拥有开放的 Action 选择面，因此能力下界只能保守取 Profile
完整 required set。OmniPaxos 虽然已有六项 validated capability，仍不能安全使用这些 backend。

本阶段没有放宽旧策略。它新增两个版本化有界策略，将 canonical `selectable_actions` 纳入 Policy digest，
先用它过滤 Runtime-enabled 且 FaultEnvelope-admissible 的动作，再执行原有随机选择算法。admission 从同一
冻结集合精确推导 capability 下界。

## 可信边界

```text
Runtime enabled actions
          |
          v
FaultEnvelope admissible frontier
          |
          v
digest-bound selectable_actions intersection
          |
          +---- empty while runtime frontier is nonempty
          |             -> policy-surface-exhausted
          v
existing deterministic stochastic selector
          |
          v
Trace + strict Replay + selected-kind surface check
```

新增失败关闭约束：

- 选择面必须非空、规范排序且无重复；
- bounded policy 只允许进入带选择审计和显式终止语义的 Experiment v2；
- Priority 只能引用选择面内的 ActionKind；
- 有 workload 时选择面必须包含 Invoke；
- admission 使用选择面而不是猜测后端可能选择什么；
- 未有独立 capability 映射的 Duplicate、Partition、Heal、Effect 和 Callback 仍触发完整 required set；
- bounded surface 过滤掉所有当前动作时，结果是 `policy-surface-exhausted`，不能误报为 Runtime quiescent；
- primary 和 replay Trace 中任何越界 ActionKind 都使执行失败。

旧 v1 policy 拒绝 `selectable_actions`，其序列化和 digest 语义保持不变。

## OmniPaxos 正负校准

负校准使用旧 `action-class-random-policy/v1` 和六能力 partial admission。系统在 Adapter factory 调用前返回
`EXPERIMENT_ADMISSION_CAPABILITY_UNDERDECLARED: audited-entropy-replay`，factory 调用次数为 0。

正校准使用 `action-class-random-policy/v2`：

| 项目 | 结果 |
|---|---|
| selectable actions | DeliverMessage、FireTemporal、Invoke |
| priority | Invoke、DeliverMessage |
| charged decisions | 29 / 96 |
| selected actions | Invoke 1、Deliver 24、Temporal 4 |
| workload | 1 planned / 1 completed |
| Core PSS | 28 unique states |
| termination | configured-stop |
| fresh Replay | stable |

关键身份被冻结在集成测试和
`benchmarks/experiments/omnipaxos-v2-bounded-stochastic-m5.22a/summary.json`。完整大 JSON 不提交，避免再次扩大
实验工件体积。

## 本阶段证明与未证明

已证明：

- partial-qualified target 可以使用一个真实 stochastic backend，而无需伪造未验证能力；
- 策略选择面、admission 下界、实际 Trace 和 Replay 之间存在机械约束；
- Runtime 有动作但策略无权选择时可与真实 quiescence 区分。

未证明：

- Agent/LLM 已接入 OmniPaxos；
- 同一 Planner plan 已跨 etcd/raft 与 OmniPaxos 执行；
- bounded action-class 优于 fixed/random/DFS；
- 28 个 PSS states 是质量分数；
- OmniPaxos fully qualified 或协议无缺陷。

## 下一阶段

M5.22b 只做跨目标 Planner composition：定义不含 Raft/Sequence-Paxos 字段的最小共同语义视图和同一
bounded intent，将它分别编译到 etcd/raft 与 OmniPaxos 已认证执行面。必须分别保存 target-local PSS/coverage
结果和共同预算账本；不得把两个 target 的 PSS 状态数直接相加或排名，也不得在该阶段声称 Agent 优势。
