# M5.21g：Effective Execution Identity 与三方法 Gate

日期：2026-08-10

## 目标

M5.21f 证明 `Prefer.Actions` 可以在不改变 backend/strategy 的情况下改变 intent、compiler work、plan
和 `IntentExecutionInstance` digest。如果继续用这些完整审计 identity 判断 behavior delta，Agent 可以
仅靠无执行作用的偏好元数据制造“新测试”。

M5.21g 增加独立的 effective-execution identity，并在两个已经冻结的 PlannerView 上比较 Agent、
zero-model 和 deterministic adaptive 三种方法。本阶段不调用模型、不运行 SUT、不扩大 Agent 权限。

## Identity 边界

协议无关 `CampaignEffectiveExecution/v1` 绑定：

- target ID 与 target/Adapter/配置 identity；
- target composition 生成的 execution-environment digest；
- 实际传入执行器的 strategy；
- 完整 Policy digest 与 policy seed；
- decisions、FaultEnvelope 和 primary/replay work budget。

它故意不绑定 proposal、compiler work、intent/plan/instance digest、backend 描述 ID 或 PSS key。
这些对象继续保留在审计链中，但其中未进入执行器的差异不能触发重复 SUT 执行或被计为行为增益。

通用层不解释 etcd/raft。etcd/raft composition 负责机械绑定：official target manifest、固定 Runtime
seed/max-clones、single-write workload digest、workload router、Replay 要求，以及从 strategy/seed
生成的 exact ActionClass/Uniform Policy。实际归档 Report.Config 的 Runtime、Policy、workload、
FaultEnvelope、decision budget 和 manifest 会反向验证该 identity，避免手写摘要与执行器漂移。

## 三方法 proposal-only Gate

输入复用 M5.21f 的两份 Agent proposal 与 exact PlannerView。zero-model 和 adaptive 使用同一 view、
同一 hard baseline、同一 compiler、seed 和 budget 重新生成 proposal/plan。Gate 只比较 effective
identity，结果为：

| attempt | Agent | zero-model | adaptive | unique effective executions |
|---:|---|---|---|---:|
| 1 | action-class | action-class | action-class | 1 |
| 2 | action-class | action-class | uniform | 2 |

精确 digest：

- attempt 1 三者：`33b44d586744ee97f20e3517c6a8177a7c842942fb1cab00000b9c1dc7913bc8`；
- attempt 2 Agent/zero：`e240b2cdaec26372435015adb42aa5b3001bc917d04b270a6603cf72e6e1c52b`；
- attempt 2 adaptive：`089d9fbbfc7d8cbecee15d9b66b6a706378ccc6730f9130a8774ca6357f1b3d9`。

因此 6 个方法—视图组合只对应 3 个有效执行。M5.21f 已保存的 attempt 1 bundle 可以服务三种方法，
attempt 2 已保存的 action-class bundle 可以服务 Agent/zero；只有 adaptive uniform 是尚未执行的新输入。

机械结果保存于
[`benchmarks/experiments/etcdraft-v2-three-method-gate-m5.21g/`](../benchmarks/experiments/etcdraft-v2-three-method-gate-m5.21g/README.md)。

## 证明与限制

本阶段证明：

- 无执行作用的 `Prefer.Actions` 不再制造 behavior delta；
- Agent 在两个 frozen views 上相对 zero-model 的 delta 为 0/2；
- adaptive 相对 zero-model 的 delta 为 1/2；
- Gate 能在调用前识别可以共享的 SUT execution。

本阶段没有证明 adaptive 测试效果更好，没有产生新的 PSS/monitor/defect evidence，也没有估计长期
Agent proposal validity。effective identity 依赖目标组合 version discipline；任何改变 Runtime、workload、
Policy 构造或 Adapter 配置的实现都必须改变其 environment/target identity。

下一阶段只执行唯一尚未存在的 attempt-2 adaptive uniform identity，并与已经归档的 action-class
execution 在相同 32-decision、34-work、seed 172 边界下比较。PSS/workload/monitor 仍是诊断指标，
不能据此宣称方法总体优势。
