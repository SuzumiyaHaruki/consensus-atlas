# M5.22b 跨目标共同 Intent 校准

本目录保存一份小型 reader summary，不提交两个完整 ExecutionBundle。真实双目标见证可由下列集成测试
重生：

```bash
go test ./cmd/control-experiment \
  -run '^TestM522bOneProtocolNeutralIntentExecutesOnEtcdAndOmniPaxos$' \
  -count=1 -v
```

可信层从两个 target-bound `AgentSemanticView` 机械求交，Agent-facing view 只保留六项共同 validated
capability、Invoke/DeliverMessage/FireTemporal 三类 backend-selectable Action、一个 bounded action-class
backend，以及两个 opaque source-view digest。测试会拒绝其中出现 etcd、Raft、OmniPaxos、Paxos、Adapter、
Profile、PSS 或 Oracle 身份。

同一父 `GuardedTestIntent` 被确定性投影为两个 target-bound child intent，再复用既有 v2 compiler、
Qualification admission、唯一 executor、PSS Mapper 和 DecisionProjector。etcd/raft 的 Ready HostEffect 是
显式 target-local deterministic plumbing，不进入共同 Agent 动作面；移除它会在 0 decision 得到
`policy-surface-exhausted`，而不是偷偷换用另一套调度器。

本实验只证明共同宏观 Intent 可以在两个已经接入的 leader-based CFT 目标上编译、执行和重放。它没有
调用 LLM、没有比较 Agent 与基线、没有合并两个 PSS 状态空间，也没有产生缺陷 verdict。
