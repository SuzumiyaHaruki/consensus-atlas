# etcd-raft M4l3 family-vs-leaf 配对实验

本次真实 DeepSeek 配对实验支持一个有限但明确的结论：在同一自然 root、固定假设、模型和预算下，etcd-raft leaf type 帮助 Scenario Agent 唯一选中了目标 `MsgAppResp`；family 视图则把一条旧 `MsgVote` 误认为应丢弃的消息。

| 指标 | leaf | family |
|---|---:|---:|
| 首次选择 | correct-unique | wrong-unique |
| 实际丢弃 | `MsgAppResp n2→n1` | `MsgVote n1→n3` |
| 目标 Drop | 是 | 否 |
| 模型调用 | 3 | 1 |
| 总 token | 27,965 | 10,653 |
| Bundle / Fresh Replay | 通过 | 通过 |
| Oracle violations | 0 | 0 |
| 修正后的 Risk 状态 | not-reached | not-reached |

## 验证修正

首次运行还暴露了一个验证问题：Risk 的 `message-dropped` 里程碑只约束了事件种类，没有约束具体消息。family Trace 丢弃 `MsgVote` 后，系统曾把它误记为“append response 已丢弃”，再把真正 `MsgAppResp` 的正常投递所形成的进展计入该假设。

修复后，etcd Observation projector 从 Trace 中受摘要保护的 Adapter command item 投影 opaque `message-role`；本实验的丢弃谓词明确要求 `message-role == MsgAppResp`。利用原始 Bundle 离线重算后：

- leaf Trace 正确证明 workload 和目标 response drop，但在本轮自然推进预算内没有证明 alternate-quorum decision；
- family Trace 只证明 workload 已发起，不能再证明目标 response drop，也不能证明完整假设；
- Bundle、Fresh Replay 和 Oracle 结果不变，因为修复只影响 Risk 证据与假设的绑定。

原始 `summary.json` 中的 family `qualified_risk_reached=true` 已被
[verification-audit.json](verification-audit.json) 明确取代，因此未将该易误读的原始 summary 纳入 HEAD。
仓库以 [final-summary.json](final-summary.json) 作为唯一最终摘要，并以两个 arm 的
`canonical-bundle.json.gz` 保留 Replay 稳定证据；展开后的 Bundle、Scenario result 和 provider journal
不纳入 HEAD。

## 结论边界

这次实验已经提供了“leaf 消息语义降低目标选择歧义”的真实模型证据，而且结果在 OmniPaxos 之外的 etcd-raft 上复现。它尚未证明 leaf 视图可以让 Agent 完成整个假设，也没有发现协议实现本身的问题。其正确 Drop 后的确定性闭合已经由后续
[M4l4 脚本实验](../etcdraft-alternate-quorum-closure-m4l4-script-v1/README.md)完成。
