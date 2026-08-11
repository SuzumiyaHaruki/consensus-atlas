# M5.21w：OmniPaxos non-leader opaque workload 纵向切片

日期：2026-08-11

## 结论

M5.21w 通过。在 M5.21v 的 `Temporal + Message` Binding 上，唯一 Control Runtime 先只通过 natural tick
和 explicit message delivery 使 n1 成为 leader，再把一个 opaque request 提交到非主节点 n2。未修改的
OmniPaxos 核心完成 forward/replicate/decide；Adapter 只有在该 entry 声明的 origin n2 观察到 exact
request/value 后才产生一次 ClientResult。

真实运行消耗 35 个 decision，结果 owner=n2、status=`decided`、index=1。关闭第一个 worker 后，第二个
fresh process 精确重放相同轨迹和唯一结果；trace digest 为
`ecc4e82672dd2a58348b92cdfbd90793f1cb7a43c9e882bd3d1c45198da41276`。

公共 Action、Runtime、Core PSS 和 experiment 生产代码变化为 0，上游 OmniPaxos 源码变化为 0。经过
一次限界收缩，生产 Binding 从临时 963 行降至 948 行；相对 M5.21v 的 770 行基线，workload 边际为
178 行，低于 180 行软目标和 1,000 行总停止线。

## 输入、处理、输出

输入：

- 已自然选出 n1 leader 的三节点集群；
- Runtime OfferInvoke target：n2；
- request ID：`request-1`；opaque value：`opaque-m5.21w`；
- 相同 BuildID、seed 和完整 Action trace 的 fresh Replay。

处理：

1. Runtime 只把 target-owned JSON input 放入 Adapter command；
2. Adapter 验证 input schema，并把 command 的 n2 identity 写入 WorkerEntry origin，调用一次 `append`；
3. n2 的 OmniPaxos API 自行产生 forward message；后续所有消息继续由 Runtime 冻结并显式投递；
4. worker 按节点记录已报告的 decided index，只增量返回新 decided entry；
5. Adapter 忽略其他节点的相同决定，只有 `decision.node == entry.origin` 才发出 ClientResult；
6. Runtime 保存完整结果并在第二个 worker process 逐 decision 严格 Replay。

输出：

- decisions=35；Invoke=1；ClientResult=1；
- leader before Invoke=n1；invoke/result origin=n2；
- result status=`decided`，index=1，value exact match；
- fresh worker processes=2；strict Replay=true；
- worker SHA-256：`c22406c3c56c3aefd83f4671358240146faf8d9c3eceae61c847e42db38d1206`。

冻结账本位于 `benchmarks/feasibility/omnipaxos-workload-m5.21w/summary.json`。

## 身份与唯一结果边界

调用方不能在 opaque input 中指定 origin；Adapter 从已经通过 Runtime eligibility 的 command NodeRef
机械写入协议 entry。worker 再检查 `entry.origin == append node`。决定输出同时绑定 node、index、
request ID、origin 和 exact value，Go 侧重新校验 node/origin/index/request。

唯一结果来自三个约束：本阶段只 Offer 一个 request；worker 对每个节点只增量报告尚未返回的 decided
suffix；Adapter 只接受 origin 节点的决定。当前没有实现通用 request deduplication，因此“同一个
request ID 被外部重复 Offer”仍是 Unsupported/后续 WorkloadRouter 约束，不能由本实验外推。

## Binding 成本与收缩

| 类别 | M5.21v | M5.21w | 变化 |
|---|---:|---:|---:|
| target-native Rust worker | 211 | 268 | +57 |
| Go/Rust wire protocol + process client | 142 | 152 | +10 |
| normalization/application/bookkeeping | 417 | 528 | +111 |
| **生产 Binding** | **770** | **948** | **+178** |
| conformance test | 323 | 464 | +141 |
| Cargo manifest / lock | 12 / 429 | 12 / 429 | 0 |

首次可运行版本为 963 行。收缩删除了 step/append 两套重复 payload 字段、decision 字段逐项复制、第二层
ClientResult wrapper、固定 configuration ID 的重复 node evidence 和未消费的 capture cause 参数，净减
15 行。没有压缩排版、删校验、搬文件到 shared 包或改变统计口径。

当前两个 Rust target 的 JSONL process client 已构成真实重复面，但 workload 闭环不依赖先建 kit；是否
提取必须另做两个消费者总 LOC 和错误边界审计，不能回写成 M5.21w 的功能贡献。

## M5.21v 控制面回归

当前 BuildID 下，duplicate first message → drop original → deliver clone → natural election 仍通过：51
decisions、fresh Replay=true，trace digest 为
`a0073bdae164268885b02b9825c53f9a74307ccf199b4829484ca75d605e9cc0`。unsupported operation rejection
和 abnormal worker exit reporting 也继续通过。M5.21v 目录保留其 pre-workload BuildID，不改写历史。

## 没有证明

- `MemoryStorage` 仍同步内置于 worker，没有 suspendable/durable HostEffect 或 crash/restart；
- 没有 request dedup、多并发 request、reconfiguration、compaction 或 snapshot；
- 没有 Core/Extended PSS Mapping、RiskWitness、Oracle、Coverage、Agent 或 Campaign；
- ClientResult 只说明 observed decided entry，不是独立 safety Oracle；
- 没有 Qualification/Experiment admission，不能进入正式方法比较；
- empty harness entropy tape 不是全面 audited SUT entropy replay；
- OmniPaxos 仍是 leader-based，不证明 leaderless/dependency-graph 可移植性。

## 下一阶段：M5.21x Core PSS 跨协议映射 probe

下一步不继续堆 workload 或持久化，而是检查固定 Core PSS IR 是否真的能容纳 Sequence Paxos：

1. Mapping 只读取当前稳定 Evidence，不修改 Core PSS schema、Runtime 或 worker；
2. participant mode 只做保守的 passive/coordinating 映射，不虚构不可观测的 contending；
3. 完整 ballot tuple 和 decided index 只能转换为相对 epoch/decision rank，绝对协议字段不得进入 Core state；
4. 在线采样必须观察到选主和决定引起的不同 Core state，并在 fresh Replay 中得到相同 sample sequence；
5. 映射生产代码软目标 180 行、硬停止线 240 行；无法表达的 Sequence Paxos 语义进入 Extended PSS 缺口
   记录，不修改 Core schema 来迁就目标；
6. 本阶段仍不加入 restart、Agent、Coverage 总分、Oracle 或 Qualification。
