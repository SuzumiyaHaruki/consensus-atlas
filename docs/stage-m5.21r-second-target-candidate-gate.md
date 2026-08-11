# M5.21r：第二 strict CFT target 候选门

日期：2026-08-11

## 结论

M5.21r 没有把已有 HashiCorp Raft Adapter 强行升级为 strict，也没有直接开始大型跨语言
Adapter。它先冻结第二目标的候选门，结果如下：

| 候选 | 已有证据 | 本阶段判断 |
|---|---|---|
| HashiCorp Raft v1.7.3 | message/lifecycle/invoke 3/8；官方路径使用墙钟、全局随机和 goroutine | 官方未修改构建不作为 native-strict 迁移目标；保留 native-partial 与后续 instrumentation case-study 价值 |
| efficient/epaxos | codec-aware message framing probe；完整构造器、yield/time/restart 未解决 | deferred，仍保留非 Raft 泛化价值 |
| TiKV raft-rs 0.7.0 | 同步 `RawNode`，显式 `tick/step/ready`，可固定单值 election range | 通过 core candidate probe，进入 bounded Binding spike |

这里的“通过”只表示值得继续投入，不表示取得 ConsensusAtlas Qualification。

## 探针输入

- crates.io `raft = 0.7.0`，依赖由 `probes/raftrs/Cargo.lock` 固定；
- 三个 `RawNode<MemStorage>`；
- n1 election range `[5,6)`，n2/n3 使用更晚的单值范围；
- harness 不调用 `campaign()`、墙钟或线程；
- 所有 peer message 由 harness 取得后按规范化顺序显式 `step()`；
- Ready 中的 state/entries 先写入 MemStorage，再释放 persisted messages 并 `advance`。

## 实际结果

同一进程创建两组 fresh cluster，分别执行：

```text
5 x Tick(n1)
  -> natural election messages
  -> explicit Step(message)
  -> n1 leader
  -> Propose(opaque bytes)
  -> Ready/persist/release/Step
  -> committed index 1
```

两次执行均得到：

- leader：n1；
- committed index：1；
- 记录数：28；
- trace digest：`42533218056798b4df138221b573c2f442be4426fe06f0124e6aacb1a047e635`；
- fresh in-process trace equality：true。

`make probe-raftrs-core` 还执行 `cargo fmt --check`、`cargo clippy -D warnings`，并将 fresh
输出与冻结 JSON 逐字节比较。

## 信任边界

该探针证明：

- raft-rs core 是同步、由宿主驱动的 RawNode；
- Tick、peer message、Ready 是可见的宿主边界；
- 固定单值 election range 后，本场景不依赖随机选举结果；
- 一个最小自然选举/提交轨迹可以 fresh 重现。

它没有证明：

- 已存在 ConsensusAtlas Adapter 或 Control Runtime strict Replay；
- raft-rs 全部随机源已完成 source audit；
- durable image、crash/restart、host-effect DAG 正确；
- Core PSS Mapping、WorkloadRouter、DecisionProjector 或 RiskWitness 可迁移；
- 第二 strict CFT target 已 qualified；
- Agent、Coverage、Oracle 或 formal holdout 有新结果。

报告中的零 Campaign/墙钟/线程调用只描述 probe harness，不能外推为整个 crate 的 source audit。

## 代码和工件

- `probes/raftrs/Cargo.toml` / `Cargo.lock`：隔离、固定的 probe dependency graph；
- `probes/raftrs/src/main.rs`：303 行非生产探针；
- `benchmarks/feasibility/raft-rs-m5.21r/report.json`：34 行冻结输出；
- `benchmarks/feasibility/raft-rs-m5.21r/README.md`：输入 digest 与复验边界。

未修改 Go module、公共 Action、Runtime、PSS、Qualification 或现有 Adapter。

## 下一阶段停止线

M5.21s 只允许实现 raft-rs 的最小 target-owned Execution Binding spike：

1. Rust worker + Go Adapter 只映射现有 Temporal、Message 与 Ready host effect；
2. 用同一 Control Runtime 执行一条三节点自然选举轨迹并 strict Replay；
3. 不实现 PSS、RiskWitness、workload、Agent 或正式 Qualification；
4. 不增加协议 Action、Runtime type switch 或第二套 scheduler；
5. target-owned 新生产代码软上限 900 行，超过 1,200 行或需要修改 raft-rs 算法源码则停止复核。

验收还必须记录：公共 `internal/control*`/`internal/controlexperiment` 的生产 Core Churn、
target-native/跨语言 bridge/可共享 boilerplate/conformance 四类 Binding Composition，并要求
strict Replay 使用 fresh worker process。单进程 probe equality 不能替代这一门禁。

只有该切片通过，才逐步增加 durable restart、workload 和语义映射；不得一次性复制 etcd/raft
Adapter 的全部功能。
