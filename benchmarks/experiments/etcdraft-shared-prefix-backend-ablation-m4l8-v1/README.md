# M4l8 shared-prefix backend ablation

本实验隔离 M4l7 中“模型行为”和“闭合后端”的影响，不调用任何模型，也不增加公共
Action。测试由
`TestM4l8EtcdraftSharedAgentPrefixBackendAblation` 可重复执行。

## 共同起点

从相同的28-decision etcd/raft root 出发，机械重建 M4l7 两个真实 Agent arm 的共同
三步前缀：

```text
29  Deliver MsgApp n1→n2
30  Complete n2 raft-ready-persist
31  Drop MsgAppResp n2→n1
```

得到的 step-31 Trace digest 为
`88351b3edc9803d828a6d65915c7848e8ab1eeb973116381e32ba5fdc3aae63f`。
target-local 最终 Trace digest 与 M4l7 真实模型实验完全相同，说明消融复现的是同一条
实际闭合路径，不是相似但不同的脚本场景。

## 结果

| arm | 后干预预算/实际 decisions | Risk | 请求结果 | progress work | qualified primary/replay |
|---|---:|---|---|---:|---:|
| public-fixed | 14/14 | not-reached | 无 | 94 | 47/47 |
| target-local | 14/14 | reached | step 45 committed | 94 | 47/47 |
| public-fixed extended | 32/19 | reached | step 50 committed | 104 | 52/52 |

等预算比较中，两边执行与验证成本完全相同，只有 target-local 达到 Risk。扩大预算后，
public-fixed 最终也能闭合，但需要19个后干预 decision，比 target-local 多5个；按本实验
记录的 progress + qualified primary + Replay 口径，需要208 work units，target-local
为188。

三条候选均 fresh Replay stable。随后从保存的 Bundle 重新调用既有 Target Oracle
registry，均检查：

```text
trace-integrity
agreement
etcdraft-client-application-binding
etcdraft-log-progress
```

全部为0 violation，请求身份均为 `a9d6-write-1`。

## 结论边界

该结果能够独立于模型、prompt、invalid revise 和 token 波动证明：

> 对这个真实 post-intervention Trace，target-local 因果闭合在相同14-decision预算内
> 达到目标，而公共固定推进不能；公共推进需要再增加5个 decision 才达到相同请求结果。

它仍然只是一个三节点 etcd/raft 场景，不能证明跨 Risk、跨拓扑或跨协议的一般优势。
下一步应先做少量真实模型重复，再在 OmniPaxos 上实现第二条窄 closure 垂直切片。
