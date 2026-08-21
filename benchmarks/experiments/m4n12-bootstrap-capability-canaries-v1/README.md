# M4n12 bootstrap capability canaries

本目录记录 2026-08-21 各一次的五节点 etcd/raft 与 OmniPaxos `public-fixed`、单 Episode
capability canary。两次运行共享 M4n12 Agent 状态机、DeepSeek 官方 `deepseek-v4-flash`、
`bootstrap` root、只读源码 search/read 和相同的 12-call/240,000-observed-token Episode 上限。
它们不是预注册的 discovery trial，不用于比较协议发现率。

## etcd/raft

- 工件：`artifacts/agentic/m4n12-canary-etcdraft-bootstrap-v2/`；
- root：10 个启动 Ready/effect decisions，root-prefix Oracle finding 为 0；
- Risk：`recovery-monotonicity-restart-reapply`，机械可执行；源码读取完成但未被候选引用；
- 成本：3 次 Risk、4 次 Scenario attempt、7 次模型调用、117,851 observed tokens；
- 执行：5 个 post-root decisions。首次过早 Invoke 无匹配；随后五个节点各执行一次 timer，
  尚未形成 leader；再次 Invoke 无匹配后 Agent abandon；
- 结果：witness 未实例化，Agent finding 为 0；
- 证据：生成 15-decision Bundle，保存的 fresh Replay stable；独立 saved-Bundle audit 重算
  `trace-integrity/agreement/client-binding/election-safety/log-progress`，0 violation。

## OmniPaxos

- 工件：`artifacts/agentic/m4n12-canary-omnipaxos-five-node-bootstrap-v1/`；
- root：0 decisions，bootstrap root 没有预先形成 coordinator；
- Risk：`drop-accept-decide-lagging-follower-prefix`，机械可执行；候选真实引用了读取的
  `adapters/omnipaxosv2/observations.go`；
- 成本：3 次 Risk、4 次 Scenario provider 调用，其中 3 次形成 attempt；共 7 次模型调用、
  287,665 observed tokens；最后一次响应使累计成本跨过 240,000 上限；
- 执行：前两次计划类型无效；第三次执行 1 个 timer 和 3 个消息 delivery，共 4 decisions，
  尚未形成 coordinator；
- 结果：`model-token-threshold-reached`，witness 未实例化，未形成 qualified Bundle，
  因而没有运行完整 Episode Oracle；durable journal 中所有 dispatch 均有 result。

## 结论边界

M4n12 修复了 M4n11 的 root 污染：两个 bootstrap root 均不再把已有异常归因给 Agent。
但 pre-election root 将“建立可路由 coordinator”变成 Scenario 的首个真实任务。当前小切片自然推进
在五节点上平均轮转 timer，Agent 又过早请求 Invoke，导致两次 canary 都没有到达首个 workload milestone。

下一步不应简单增加总预算或启动六 Episode。应先让可信反馈明确展示“当前无唯一 coordinator”以及
与选主有关的最新 term/ballot/role 进展，并用零模型回归证明 Agent 可以通过重复但连贯的 timer 选择
到达唯一 coordinator，再各做一次短 canary。

## M4n13 后续说明

上述结果是 M4n12 的历史运行事实，`final-summary.json` 不做事后改写。后续 M4n13 已在不调用模型的
普通回归中完成建议修复：Scenario prompt 合并为单份 `action_frontier` 并加入可信协调状态；公共自然推进
优先沿 item dependency 和 Agent 所选参与者方向执行，在协调语义变化时返回；etcd/raft 与 OmniPaxos 的
三/五节点 bootstrap 均能建立协调者并执行 Invoke。OmniPaxos 五节点的初始 frontier 约 85 个 Action，
因此其有效输入将 decision 上限从 64 调整为 128；这项差异进入解析后 semantic input digest/MethodSpec，
不是对历史 canary 的追溯性放宽。真实模型短 canary 仍需另行运行，零模型结果不构成 Agent 效果证据。
