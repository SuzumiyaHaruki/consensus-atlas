# M5.23d：搜索后只读 discovery projection

日期：2026-08-12

## 结论

M5.23d 冻结了第一个不会反向影响搜索、且可从真实执行证据重算的 discovery projection。由于
`StatelessDFSWorkItem` 只保存 child prefix identity，不保存完整 trace 或协议状态，Core PSS 不能从搜索
摘要直接推断。每个 WorkItem 必须重新编译为 exact Policy，通过原 qualified executor 执行到对应 child
decision，并完成 strict Replay 后，才可进入 discovery。

通用 projector 逐项验证 WorkItem、bundle、trace、manifest、decision 数、final state 和 PSS identity；随后
调用已注册 `SemanticMapper` 从 trace/evidence 重新投影 Core PSS，并与 bundle 样本逐项核对。它只聚合
identity 和集合，不执行搜索、不选择 Action，也不产生 Coverage、Oracle 或缺陷结论。

## 输出边界

`StatelessTraversalDiscovery` 对每个 WorkItem 记录：

- WorkItem/bundle/trace/PSS-sample digest；
- 最终 child PSS key；
- trace、final-child PSS 和 incremental PSS 的新增/累计数量；
- separately charged qualified execution work。

方法级输出记录 root PSS baseline、root 之后增量 PSS 集合、全部观察状态集合及其 canonical digest。完整
PSS State、ExecutionBundle 和 trace 不进入冻结摘要。投影对象被篡改后，使用原 result/root/bundles/mapper
重算会失败关闭。

## 单 root 校准

M5.23d 复用 M5.23c 的同一 28-decision root、相同四个 traversal identities 和同一 443 search work：

| 方法 | root 后新增 PSS | all observed PSS | discovery execution work |
|---|---:|---:|---:|
| canonical | 6 | 25 | 191 primary + 191 replay |
| uniform seed 01 | 5 | 24 | 191 primary + 191 replay |
| uniform seed 02 | 6 | 25 | 191 primary + 191 replay |
| uniform seed 03 | 6 | 25 | 191 primary + 191 replay |

共同 root 本身含 19 个 unique PSS states；四种方法的增量集合联合为 22。每种方法需要 6 次额外 qualified
execution，因此可信 discovery 的总逻辑成本是 `443 + 191 + 191 = 825` work units。不能只报告 443，也
不能把 bundle projection 视为免费分析。

## 已证明与未证明

已证明：

- 搜索完成后能从 qualified bundles 机械重算 exact-prefix discovery；
- common root states 与 method incremental states 已分离；
- 四个方法共享相同 search 和 discovery execution 成本；
- PSS mapper、bundle 样本或 discovery 内容不一致会被拒绝；
- discovery 没有调度或 verdict 权限。

未证明：

- canonical 与 uniform 哪个更有效；
- PSS state count 能预测 obligation coverage 或缺陷检出；
- 单 root 的 6/5/6/6 差异具有统计意义；
- 多协议的 PSS mapping 已足够一致；
- Agent ranking 有任何收益。

## 下一阶段：M5.23e

冻结一个小型 root corpus。root 必须来自事先保存、strict-Replay 稳定的真实 source trace，并在方法运行前
固定 prefix identity；不得根据某方法结果挑选有利 root。先在 3 个左右语义阶段不同的 etcd/raft roots 上，
以相同方法、seeds、depth/items 和完整 825-style evidence accounting 重复校准，报告每 root 和跨 root 的
增量集合，不进行显著性或 Agent 优势宣称。

多 root 结果稳定后，下一步才是让受限 Agent 在同一 frontier-order 权限上工作，并与 canonical、多个
uniform seeds 比较。Agent 输入可以包含 root 以前的协议知识和 discovery 历史，但不能包含当前未执行 child
的未来结果。
