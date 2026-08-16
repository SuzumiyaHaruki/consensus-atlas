# Coverage Kernel v2（历史设计）

## 当前状态

Coverage Kernel v2 曾用固定义务、Profile 和 Ledger 统计有界覆盖率。相关 Campaign、
`internal/campaign`、`internal/agentcampaign` 和 Raft campaign compiler 已从当前活动实现删除。

`profiles/` 中留存的 JSON 是历史设计和实验参考数据，当前 Agentic Episode 不会加载它们，
也不会以 12 或 55 个 Raft 义务作为最终测试分母。因此不能从这些文件声称当前系统已完成
固定 Profile 覆盖评测。

## 仍然保留的原则

历史设计中两个原则仍适用于当前主线：

- Agent 可以提出风险、Action 意图和候选 assertion，但不能自行给出覆盖或缺陷 verdict；
- 有效证据必须来自真实执行、fresh Replay 和已注册 Oracle，而不是 Agent 生成的标签。

当前活动路径是：

```text
Risk Agent candidate
    → Scenario Agent plan
    → enabled Action materialization
    → Trace + fresh Replay
    → target-local/generic Oracle
    → protocol/control/joint PSS 和调查进度
```

Oracle 失败和 PSS/进度统计始终分开报告；后者不能创建缺陷判定。

## 未来恢复固定分母时的边界

若后续需要固定义务评分，它必须作为当前 Agentic artifact 的一个评估器，而不是恢复一套
独立 Campaign runner/session。新分母还必须用真实 Target 可达性和已执行 monitor 重新校准；
历史 Raft Profile 不能直接充当跨协议的当前指标。
