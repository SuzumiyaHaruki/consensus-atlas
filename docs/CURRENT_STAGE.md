# 当前阶段

日期：2026-08-12

阶段：M5.23g 真实 Agent 多 root pilot 已完成，M5.23 已关闭

## 输入、如何处理、输出什么

输入：M5.23e 预声明的 0/28/54-decision root corpus、冻结 ProtocolKnowledgePack、当前 trusted
`ActionFrontierView`、此前 root 已完成的 discovery history，以及固定 depth/item/work 上限。

处理：真实 DeepSeek planner 只返回 ActionID 数组；可信 validator 要求它是当前 frontier 的完整
permutation。每次调用在 transport 前冻结 intent/dispatch，随后只写一次 terminal result，失败不重试。
WorkItem 再经原 qualified executor、strict Replay 与只读 Core PSS 重投影。

输出：6 次真实模型调用账本、三 root Agent traversal、18 份资格化执行的只读 discovery、完整模型/执行
成本，以及与 M5.23e canonical/fixed-uniform 的并列表。

## etcd/raft 真实 Agent pilot

| 指标 | 结果 |
|---|---:|
| roots | 0 / 28 / 54 decisions |
| depth / items / work ceiling per root | 2 / 6 / 1,500 |
| model calls / accepted / retries | 6 / 6 / 0 |
| model tokens | 28,335 |
| qualified executions | 18 |
| Agent corpus-novel PSS | 15 |
| canonical / uniform 01/02/03 | 15 / 13 / 15 / 14 |
| Agent order vs canonical | 3/3 root 相同 |
| Agent novel set vs canonical | 相同 |

这是一份完整闭环的负结果：真实 Agent 通过权限与执行链，但本次选择退化为 canonical，消耗模型成本却
没有增加状态发现。它不证明 Agent 优势、Coverage 完备或缺陷检出。

## 下一阶段：M5.24 外部有效性

冻结重复试验设计和非公开 candidate/control，再扩大 root corpus，以缺陷检出、PSS/义务发现和完整实际
成本评估 Agent/约简/基线的关系。本次负结果不得通过事后改 prompt 或反选 root 消除。

## 建议阅读顺序

1. `docs/stage-m5.23g-real-agent-multi-root-pilot.md`
2. `benchmarks/experiments/etcdraft-v2-stateless-agent-m5.23g/summary.json`
3. `cmd/control-experiment/stateless_agent_m523g.go`
4. `cmd/control-experiment/stateless_agent_call.go`
5. `internal/controlexperiment/stateless_agent_order.go`
6. `internal/controlexperiment/stateless_agent_call.go`
7. `docs/stage-m5.23f-restricted-search-agent.md`
8. `docs/stage-m5.23e-predeclared-root-corpus.md`
9. `docs/ConsensusAtlas-总体规划.md`

历史阶段不再复制进本文件；不可改写记录保留在 `docs/stage-*.md`。
