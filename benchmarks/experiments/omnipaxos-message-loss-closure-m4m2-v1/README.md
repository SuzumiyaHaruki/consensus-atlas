# M4m2：OmniPaxos 消息丢失后的因果闭合垂直切片

本实验不调用模型，不修改 OmniPaxos 实现，也不增加公共 Action。它使用真实三节点
worker，以公共固定顺序从单请求 `Invoke` 自然推进，在首个 operation-carrying
Sequence Paxos 复制消息出现时保存共同前缀，然后执行一次 `DropMessage` 干预。

实际首请求走的是：

```text
sequence-paxos/accept-sync → sequence-paxos/accepted → sequence-paxos/decide
```

而不是已经完成同步后的 `accept-decide` 快速路径。因此本切片按真实 frontier 纳入了
审计方案允许的 `accept-sync`，没有构造一条运行时不存在的消息路径。

## 结果

- 共同前缀：25 decisions；
- 干预：step 26 丢弃 `sequence-paxos/accept-sync n1→n2`；
- 同一请求：`omnipaxos-a9e1-request`；
- 备用参与者：从实际三节点 evidence 推导为 `n3`；
- 相同的 4-decision 后干预算下：
  - `public-fixed` 使用 4 decisions 后预算耗尽，Risk `not-reached`；
  - `target-local` 发生 factory handoff，依次选择当前 enabled 的
    `prepare/promise/accept-sync/accepted`，使用 4 decisions 到达 Risk；
- 放宽公共顺序预算后，`public-fixed` 使用 8 decisions 到达相同 Risk；
- target-local 精确 Trace 形成 qualified Bundle，primary/replay work 均为 32；
- fresh Replay stable；
- Target registry 实际检查 `trace-integrity` 与 `agreement`，0 violation；
- Bundle 的 completed client result 与被丢弃消息中的 `request_id` 一致。

Target-local selector 只从公共 Runtime 当前 enabled frontier 中选择
`DeliverMessage`；它不创建消息、不修改消息内容、不推进私有协议状态，也不执行新的
Drop/Crash/Partition/Invoke。最终 Replay 只重放已记录的精确 Action，不再次调用 selector。

## 结论边界

该结果证明同一窄 closure factory 接口已经跨越 etcd/raft 与 OmniPaxos 两个协议：
公共层继续负责 Action、Runtime、Trace、Replay 与 Oracle，Target-local 后端只提供正确
干预后的因果选择知识。它仍是一个公开、单请求、三节点 capability 切片，不是缺陷
finding，也不能证明对所有 Paxos/OmniPaxos 场景有效。

## Scenario Agent 主路径收口

审计后的 M4m2R 回归使用
`plans/agent/omnipaxos-message-loss-risk-v1.json` 作为 existing Risk。该候选每次加载都
针对当前 OmniPaxos Target 重新资格审查，规范 Risk ID 为
`message-loss-before-decision`，因此 factory 不需要放宽到任意消息丢失候选。

零模型伪 Planner 只调用一次，提交三个真实语义 Action：

```text
Deliver prepare n1→n2
Deliver promise n2→n1
Drop accept-sync n1→n2
```

随后真实 Scenario Agent 协调路径发生 handoff，Target 自动执行同样的四步 closure，
形成与脚本消融完全相同的 Trace/Bundle digest。同一 RequestID completed，fresh Replay
stable，两个 Oracle 0 violation，handoff 后没有新的 Drop、Crash、Duplicate 或
Partition。该回归证明的是 `existing Risk → Scenario Agent → handoff → closure →
qualified Bundle` 的本地主路径，不是一次外部模型效果实验。

后续 M4m2O 在不修改该历史 Trace/Bundle 格式的前提下增加了
`omnipaxos-client-decision-binding`。当前 registry 的新运行会检查
`trace-integrity`、`agreement` 和该 binding monitor；本目录中原始 M4m2/M4m2R
摘要仍如实保留当时只执行两个 monitor 的历史结果。

机械回归入口：

```text
go test ./cmd/control-experiment \
  -run '^(TestOmnipaxosMessageLossClosureSharedPrefix|TestOmnipaxosExistingRiskRunsThroughScenarioAgentClosure)$' \
  -count=1 -v
```
