# M5.21u：OmniPaxos 非 Raft deterministic-core probe

日期：2026-08-11

## 结论

M5.21u 的限界 probe 通过。固定发布版 `omnipaxos 0.2.2` 可以在不修改上游源码、不增加公共
Action/Runtime/Core PSS 的条件下，以普通同步 Rust 对象构造三节点 Sequence Paxos + Ballot Leader
Election 集群。外部 harness 只调用逻辑 `tick()` 形成自然超时，从 `outgoing_messages()` 取走并冻结
消息，再逐条调用 `handle_incoming()` 投递。

两套全新集群在同一进程内都自然选出 n1；由非主节点 n2 接受的 opaque append 经显式消息调度后，
n1、n2 在 decided index 1 读到同一个 request/value。两次规范化 trace 均为 80 个事件，SHA-256 都是
`216cf8e50aef032087f12673f4752aa9b2b044f700c8143bca30ad222d48784d`，待投递消息峰值为 12。

因此得到的是 **non-Raft candidate core go**：公共控制抽象没有在第一处跨协议检查中失败，可以进入
限界 Binding spike；这不是 OmniPaxos Adapter Qualification，也不能把“Sequence Paxos”误写成
leaderless/dependency-graph 协议证明。

## 输入、处理、输出

输入：

- crates.io 固定版本 `omnipaxos = 0.2.2`、`omnipaxos_storage = 0.2.2`；
- checked-in `Cargo.lock` 和三份输入 SHA-256；
- 三节点固定配置，election tick timeout=2，优先级 n1>n2>n3；
- 唯一 opaque request `request-m5.21u-1`，从 n2 append。

处理：

1. 构造三份 `OmniPaxos<ProbeEntry, MemoryStorage<ProbeEntry>>`；
2. 按节点 ID 调用逻辑 `tick()`，不调用强制竞选/指定 leader API；
3. 将所有 outgoing message 移入 harness-owned FIFO，记录 freeze，再显式投递；
4. n2 append 后继续只投递已冻结消息，直到至少两个节点的 decided index 非零；
5. 从 decided suffix 验证 exact request/value；
6. 完整重建第二套集群，比较 leader、decided nodes/index、队列峰值和每个规范化 trace event。

输出：

- `probes/omnipaxos/`：325 行 probe 源码、12 行 manifest、生成的 494 行 lockfile；
- `benchmarks/feasibility/omnipaxos-m5.21u/report.json`：冻结结果；
- `inputs.sha256`：精确绑定 manifest、lockfile 与 probe source；
- `make probe-omnipaxos-core`：hash、fmt、clippy 和 byte-for-byte report gate。

## 已证明的控制面

| 控制事实 | probe 证据 | 当前结论 |
|---|---|---|
| 同步节点构造 | 普通 `OmniPaxos` struct + injected `MemoryStorage` | 可构造 |
| 自然时间 | 只有 `tick()` 使内部 logical clock 到期 | 可控制 |
| 消息所有权 | outgoing 被取出后保存在外部 FIFO | 可冻结 |
| 消息投递 | 每条消息以稳定 from/to 显式送入目标 | 可调度 |
| 外部输入 | 非主节点 n2 接受 opaque append | 可注入 |
| 最小结果观测 | quorum 的 decided suffix 含 exact value | 可观测 |
| fresh 确定性 | 两套全新实例的规范化 trace 相等 | 初步通过 |

probe source 和两个上游 crate 的 `src/` 静态扫描没有发现 wall-clock、thread 或 RNG 调用；probe 的
普通依赖图仍包含未被本路径实例化的 persistent-storage 依赖，因此这里报告的是**被执行核心路径**，
不是对全部依赖代码的全局确定性证明。

## 没有证明

- 没有接入 `control.Adapter`，也没有 fresh worker-process strict Replay；
- 没有 crash/restart、durable image 或 power-loss 语义；
- 注入的 `Storage` trait 是同步调用，本阶段没有证明存储操作能被暂停成独立 HostEffect；
- 没有 drop/duplicate/partition conformance、Qualification 或 Experiment admission；
- 没有 Core/Extended PSS Mapping、RiskWitness、协议 Oracle 或 WorkloadRouter；
- 没有调用 Agent/LLM，没有 baseline 方法比较、Coverage 或 candidate/control 结果；
- OmniPaxos 仍是 leader-based replicated log，不能替代以后对 leaderless/dependency-graph 目标的验证；
- 两次 in-process equality 只是进入 Binding 的必要门，不是整个系统的确定性证明。

## 代码增长与停止线复核

- 公共 `internal/control`、`internal/controlruntime`、PSS 和 experiment 生产代码变化：0 行；
- 上游 OmniPaxos 源码变化：0 行；
- probe 生产源码：325 行，低于 400 行停止线；
- 新增 Runtime/Action/PSS capability：0；
- model calls：0；新增 benchmark/campaign SUT trial：0（只有 feasibility probe 的两次内嵌 fresh run）。

这次没有为了“接上非 Raft”增加协议分支或第二套 scheduler，满足 M5.21u 的失败即停止约束。

## 下一阶段：M5.21v 最小 Binding spike

允许进入下一阶段，但范围固定为 `Temporal + Message` 的最小纵向接入：

1. 使用 target-owned Rust worker/Go Binding 接入唯一 Control Runtime；公共 Action、Runtime、Core PSS
   生产 churn 必须继续为 0；
2. 只证明三节点自然选主、消息 freeze/deliver/drop/duplicate 和 fresh-worker-process strict Replay；
3. 暂不加入 workload、crash/restart、durable storage、PSS、RiskWitness、Agent 或 Qualification；
4. 复用既有 wire contract，但第二个 Rust worker 消费者出现前后都必须以实际重复面决定是否提取
   process helper，不得先造通用 kit；
5. target-owned 生产 Binding 软目标 850 行、硬停止线 1,000 行；超过时先审计，不以搬文件改口径；
6. 若必须修改上游协议转换规则或公共调度语义，则记录 no-go，不继续包装成 strict。
