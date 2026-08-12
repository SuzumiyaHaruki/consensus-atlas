# M5.23e：预声明多 root discovery 校准

日期：2026-08-12

## 结论

M5.23e 冻结了第一个方法执行前声明、内容寻址的 `StatelessRootCorpus`。corpus 绑定 M5.21p 的同一
strict-Replay `ExecutionBundle`，使用 0、28、54 decisions 三个 prefix；选择依据是 source 初态以及
已经冻结的 workload-invoked / old-coordinator-restarted milestone，而不是任何 traversal 的输出。
`phase_id` 只是不可执行的审计说明。

通用 `StatelessCorpusDiscovery` 对每个 root 复用 M5.23c traversal 与 M5.23d qualified-bundle
projection，并区分三类集合：

- 每个 root 自己的 baseline；
- 相对各自 root 的 local incremental union；
- 从 local incremental 中扣除所有 corpus root baseline 后的 corpus-novel union。

最后一个集合避免把后期 root 已经出现过的状态再次算作搜索增益。它仍是 PSS discovery，不是固定分母
Coverage，也不产生 Oracle 或 defect verdict。

## 校准结果

共同约束为 depth=2、每 root 最多 6 items、每 root search work ceiling=1,500；每种方法都产生
18 个 qualified child bundles。

| 方法 | local incremental | corpus-novel | marginal evidence work | 单独端到端成本（含 source） |
|---|---:|---:|---:|---:|
| canonical | 17 | 15 | 2,397 | 2,529 |
| uniform seed 01 | 14 | 13 | 2,395 | 2,527 |
| uniform seed 02 | 15 | 15 | 2,395 | 2,527 |
| uniform seed 03 | 15 | 14 | 2,397 | 2,529 |

corpus baseline union 为 38 states；四种方法的 corpus-novel union 为 39。共享 source primary/replay
成本为 132，完整四方法 campaign 成本为 9,716 work units。

## 一个重要的负证据

相同 root、depth、item 数和 work ceiling 不等于实际成本逐字段相同。四种方法的 search work 都是
1,287，但 seed 01/02 的 child exact-prefix execution 比 canonical/seed 03 各少 1 primary 和 1 replay
work unit，导致边际成本为 2,395 与 2,397。M5.23e 不用空操作填充成本，也不把 item 数称作等成本。

因此后续方法比较必须以完整实际成本作为横轴或共同截断条件；当前 15/13/15/14 不能直接排名。

## 已证明与未证明

已证明：root 选择可在方法运行前冻结并机械验证；多 root PSS 能从真实 qualified bundles 重算；全 corpus
baseline 能从增量中扣除；篡改 corpus 会失败关闭；实际成本差异可见。

未证明：任一方法更优；这些 root 代表 etcd/raft 的完整语义阶段；PSS 数量预测缺陷检出；结果具有统计
显著性；Agent 已参与搜索。模型调用数为 0。

## 验证记录

`go test ./...`、`go vet ./...` 和通用 stateless 核心的定向 race 通过。M5.23e 的 72-bundle
集成 race 在 5 分钟上限超时；超时堆栈位于对大 trace 重复进行 canonical JSON digest，没有
报告 data race。按既定约束不重试，因此该集成 race 记为“未在上限内完成”，不记为通过。

## 下一阶段：M5.23f

冻结受限 Search Agent 协议。Agent 每次只能返回当前 `ActionFrontierView.Actions` 的一个完整 permutation
或受约束 preference；validator 必须拒绝新增、遗漏、重复或改写 ActionRef。Agent 输入可以看到当前 prefix
以前的协议知识与已完成 discovery history，但不能看到未执行 child 的未来结果。模型调用、编译、search、
qualified execution 和 Replay 分别计费。M5.23f 只资格化权限接口，不宣称效果。
