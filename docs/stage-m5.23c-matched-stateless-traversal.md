# M5.23c：同起点、同成本的无状态遍历校准

日期：2026-08-12

## 结论

M5.23c 建立了第一个真正共享 exact prefix、实际执行成本和验证路径的 no-model 搜索方法校准。现有
`BoundedUniformPolicyVersion` 不能同时携带 exact prefix rules，因此不能直接与 M5.23a DFS 做 matched
comparison。本阶段没有放宽 Policy，而是把方法权限收窄为：只排列 Runtime 已给出的可信
`FrontierActionRef`，不能生成、删除、替换或提前引用 Action。

versioned canonical traversal 与旧 M5.23a DFS 逐字段一致；seeded-uniform traversal 对每个 canonical
frontier 使用 seed、prefix digest 和 admissible digest 形成确定性 permutation。二者共用同一个无状态
depth-first executor、FaultEnvelope、child materialization、fresh Replay 和 WorkLedger。

## API 和兼容性

新增的协议无关对象只有：

- `StatelessTraversalMethod`：ID、strategy、可选 seed 和 digest；
- `StatelessTraversalResult`：method identity 加原 `StatelessDFSResult`。

旧 `ExploreBoundedStatelessDFS` 和 M5.23a/b result identity 保持不变。新增入口
`ExploreBoundedStatelessDFSWithMethod` 只将 order function 注入同一内部执行器。method/order 实现拆在
独立 `stateless_traversal.go`，没有新增 scheduler Policy、Runtime、CLI、Campaign、schema 目录或 target
Adapter 条件。

## etcd/raft matched calibration

四个方法共享 M5.23a 的同一 28-decision root；本阶段 spec ID 独立版本化，但 Runtime、FaultEnvelope 和
三项上限相同：depth=2、items=6、work ceiling=1,000。

| 方法 | seed | expanded/items | 实际 search work | 顺序 |
|---|---:|---:|---:|---|
| canonical DFS | - | 2 / 6 | 443 | canonical ActionID |
| seeded uniform | 01 | 2 / 6 | 443 | permutation 1 |
| seeded uniform | 02 | 2 / 6 | 443 | permutation 2 |
| seeded uniform | 03 | 2 / 6 | 443 | permutation 3 |

四种 action order digest 均不同，每个方法重复执行逐字段一致。seed 03 的最后一个叶路径经 exact Policy
交回原 etcd/raft qualified executor，30-decision prefix digest/final state 均精确复现，完整 64-decision
执行 strict Replay stable。model calls 为 0。

冻结摘要只保存共同 WorkLedger 一次；每个方法保存 identity、result/search/order digest 和可读的 ActionKind
序列，不重复完整 ActionRef、report、bundle 或 trace。

## 已证明与未证明

已证明：

- canonical 与 seeded-uniform 能从完全相同的可信状态开始；
- 两者实际使用相同的重建、物化、验证成本，而不只是 ceiling 相同；
- 方法 identity/seed 进入 digest，重复运行稳定且篡改失败关闭；
- 更换 traversal method 会改变有限预算内的真实分支集合；
- uniform 选择的叶仍可由唯一 qualified executor 精确执行。

未证明：

- canonical、uniform 或未来 Agent 哪个发现更多 PSS/义务/缺陷；
- 4 个顺序足以形成统计结论；
- 状态空间完整性、DPOR、状态合并或 pruning；
- 不同 target 之间的效果排名；
- Coverage/PSS 数值能预测外部缺陷检出。

因此本阶段是“比较装置资格化”，不是“方法效果实验”。

## 下一阶段：M5.23d

先定义不反向影响搜索的 trusted discovery projection：对每个已验证 child prefix，从现有
ExecutionBundle/PSS/semantic projector 能力中选择最小、协议无关且可重算的输出，分别报告 exact trace
identity、Core PSS key 和可选 target-owned semantic evidence。随后在多个 root 与 seeds 上，用相同 work
budget 比较 canonical 和 seeded-uniform 的增量发现集合。

只有 discovery 口径冻结后，protocol-aware Search Agent 才能在相同 `StatelessTraversalMethod` 权限位置
提供 frontier ordering。Agent 不得看未来 child 结果、修改 frontier、评分规则或 ledger。
