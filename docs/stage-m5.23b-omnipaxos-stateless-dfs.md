# M5.23b：OmniPaxos bounded stateless DFS 可迁移性门禁

日期：2026-08-12

## 结论

M5.23b 在不修改 `internal/controlexperiment` DFS API 的前提下，将 M5.23a 的 exact-prefix bounded
stateless DFS 接到真实 OmniPaxos worker。输入沿用 M5.22b 已资格化的 six-capability workload、Runtime
seed、FaultEnvelope 和唯一 qualified executor；没有新增协议字段、Action 顺序规则、PSS 依赖或
OmniPaxos target condition。

这证明当前最小 DFS 控制边界不依赖 Raft 专属语义。它不证明 DFS 优于其他方法，也不构成 etcd/raft 与
OmniPaxos 的性能或探索效率比较。

## 运行结果

从冻结的 M5.22b OmniPaxos bundle 截取 1-decision exact prefix，固定 depth=2、items=6、search work
ceiling=1,000：

| 指标 | 结果 |
|---|---:|
| expanded prefixes | 2 |
| emitted WorkItems | 6 |
| search work units | 51 |
| stop reason | `work-item-limit` |
| deterministic repeat | 通过 |
| child fresh Replay | 6/6 |
| leaf prefix exact reproduction | 通过 |
| full leaf execution primary/replay | 29 / 29 decisions |
| execution work primary/replay | 31 / 31 |
| model calls | 0 |

首个深度 1 分支是自然 temporal event；其深度 2 frontier 同时含 temporal event 与多条 message delivery。
最后一个叶父链编译为通用 exact Policy，交回原 `ExecuteQualifiedBundle` 后，其前三个 decision 的 prefix
digest 和 final state digest 与 DFS child 完全相同，完整 workload 也稳定 Replay。

## 普适性和生命周期边界

通用 DFS 文件在本阶段零修改。OmniPaxos Adapter 使用 worker 子进程，因此 composition test 记录每个
fresh Adapter 并在阶段结束时关闭；该处理没有改变 Action、frontier、DFS 顺序、trace 或 SUT 语义。
这说明资源生命周期可以留在 target factory/composition 层，而无需把 `Close` 或进程概念加入统一 Action。

冻结摘要只复用 M5.23a 的通用 summary shape，位于
`benchmarks/experiments/omnipaxos-v2-stateless-dfs-m5.23b/summary.json`。没有重复提交完整 report、bundle 或
trace。

## 解释限制

M5.23a 的 etcd/raft root 是 28 decisions，M5.23b 的 OmniPaxos root 是 1 decision；两者 frontier 大小和
父前缀重放成本不同。因此 443 与 51 search work units 不能用于比较协议、Adapter 或 DFS 效率。当前只可
声称：同一 API 在 embedded Go Adapter 与 process-backed Rust Adapter 上均能确定性执行。

尚未证明：

- 相同根条件和等成本下 DFS 与 bounded uniform/action-class 的发现能力差异；
- 状态空间完整性、状态合并、DPOR 或 PSS-guided pruning；
- 缺陷检出、Coverage 增益或 Agent 优势；
- worker 进程资源时间/RSS（当前 ledger 仍标记 `not-collected`）。

## 下一阶段：M5.23c

冻结一个不含 LLM 的等成本方法校准设计：在每个目标内部固定相同 trusted root、执行预算和 discovery
输出口径，比较 DFS 与 bounded uniform（必要时 action-class）。先证明预算确实等价、结果可重复且不把
prefix reconstruction 免费化；若现有 stochastic policy 无法从 exact prefix 启动，应先记录方法接口
缺口，不得用不同起点的结果冒充 matched comparison。

M5.23c 仍不评价 Agent。只有 deterministic baseline 的 matched budget 门禁通过后，Search Agent 才能
在同一 WorkItem/ActionRef 权限边界上提供排序或分支选择建议。
