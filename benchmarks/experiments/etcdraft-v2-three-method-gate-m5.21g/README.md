# M5.21g Three-method Proposal-only Gate

## 输入和边界

本实验复用 M5.21f 已归档的两个 exact `CampaignPlannerView` 和两份 Agent proposal，分别用同一
trusted compiler 重新计算 zero-model 与 deterministic adaptive proposal。三种方法拥有相同 view
和 preference-only 权限。

M5.21g 没有调用模型、没有运行 SUT，也没有重投影新的 PSS。它只比较新加入的
`CampaignEffectiveExecution/v1`：target identity、执行环境、strategy、完整 Policy、seed、decisions、
FaultEnvelope 和 work budget。proposal、compiler work、plan 和 instance digest 仍保留用于审计，
但不进入该行为去重 identity。

## 结果

| attempt | Agent | zero-model | adaptive | unique effective executions |
|---:|---|---|---|---:|
| 1 | action-class | action-class | action-class | 1 |
| 2 | action-class | action-class | uniform | 2 |

- attempt 1 三者 effective digest 均为 `33b44d58...13bc8`；
- attempt 2 Agent/zero-model 均为 `e240b2cd...1c52b`；
- attempt 2 adaptive 为 `089d9fbb...1b3d9`。

因此，在这两个 frozen views 上：

- Agent 相对 zero-model 的 behavior delta 为 0/2；
- adaptive 相对 zero-model 的 behavior delta 为 1/2；
- 6 个方法—视图组合只对应 3 个有效执行，其中 attempt 1 的 3 份和 attempt 2 的 Agent/zero 可以
  共享执行证据。

这个结果证明 identity 和 proposal-only gate 能排除无执行作用的偏好元数据差异；它不证明 adaptive
测试效果更好。attempt 2 的 uniform execution 尚未在本阶段运行，因此不存在新的 PSS、workload、
monitor 或 defect-effectiveness 结果。完整机械数据见 [comparison.json](comparison.json)。
