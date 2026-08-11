# 当前阶段

日期：2026-08-11

阶段：M5.22d 跨目标 preference 权限校准已完成

## 输入、如何处理、输出什么

输入：同一个 target-blind `CrossTargetPlannerView`、preference-only contract、deterministic baseline parent
Intent，以及一个使用 DeepSeek v4 Flash 冻结请求格式的离线 blind-model mock。

处理：baseline 与 mock 共享硬约束、96 decisions/98 primary/98 replay 的每目标上限。mock 只能修改
`prefer.backend_ids` 与 `prefer.actions`；模型调用先持久化在 target-a Campaign，结果经可信 validator 接受
后才投影为两个 target-local planned attempt。target-b 不重复模型调用或 token。两臂分别执行 etcd/raft
与 OmniPaxos，并通过 M5.22c ledger 恢复和计费。

输出：两份 parent proposal、四份 target plan、四次真实执行、两个 composition ledger，以及一项关于当前
Agent 权限是否影响执行的机械结论。

## 实际结果

| 指标 | baseline | blind mock |
|---|---:|---:|
| model calls/tokens | 0 / 0 | 1 / 7 |
| execution primary/replay | 75 / 75 | 75 / 75 |
| parent identity | `e0dce20…28cf5b9` | `301b117…ebe81e` |
| etcd plan identity | `3de18bf…86707` | `3518f0a…fc129` |
| OmniPaxos plan identity | `dcecf35…dee9` | `2bcab96…55a2d` |

尽管 proposal 和 plan identity 不同：

- etcd report/bundle/trace：完全相同；
- OmniPaxos report/bundle/trace：完全相同；
- workload、PSS、Action sequence 和 Oracle 输入：没有变化；
- mock 只增加模型和 compiler accounting。

原因是共同 Catalog 当前只有一个 executable backend。Preference 没有实际 lowering 选择权，所以当前不能
宣称 Agent 控制了测试，更不能宣称 Agent 优于 baseline。

本阶段没有读取 key、没有网络调用；它是权限与记账校准，不是真实 LLM 质量实验。

## 下一阶段：M5.22e

停止增加跨目标 request/ledger 结构。复用已有 bounded uniform 与 bounded action-class，实现第二个共同
backend gate，并在两个真实目标上验证同 seed/budget 下确实产生不同 trace。如果没有行为差异，停止
preference-only Agent 路线；如果有差异，再进行一次真实 LLM 与 deterministic baseline 的小预算实验，
随后转入 holdout/mutant 评测。

## 建议阅读顺序

1. `docs/stage-m5.22d-cross-target-preference-authority.md`
2. `benchmarks/experiments/cross-target-preference-authority-m5.22d/summary.json`
3. `internal/controlexperiment/cross_target_intent.go`
4. `cmd/control-experiment/cross_target_m522d_harness_test.go`
5. `cmd/control-experiment/cross_target_m522d_test.go`
6. `docs/stage-m5.22c-durable-cross-target-composition.md`
7. `docs/architecture.md`
8. `docs/ConsensusAtlas-总体规划.md`

历史阶段不再复制进本文件；不可改写记录保留在 `docs/stage-*.md`。
