# M5.21r raft-rs deterministic core candidate probe

本目录冻结第二个 strict CFT target 的候选探针结果，而不是 Qualification。

探针使用 crates.io `raft = 0.7.0` 的官方 `RawNode<MemStorage>`，在同一进程内创建两个
fresh 三节点 cluster。n1 的 election range 固定为 `[5,6)`，n2/n3 使用更晚的单值范围；
harness 只调用五次 n1 `tick()`，不调用 `campaign()`，随后按规范化顺序显式执行消息
`step()` 和 `Ready/advance`，最终提交一条 proposal。

两次 fresh run 均产生 28 条相同记录、leader n1、committed index 1 和相同 trace digest。
这证明 raft-rs 值得进入下一轮 Binding spike，但不证明 ConsensusAtlas Adapter、durable
crash/restart、跨进程 replay、SUT entropy audit、Qualification、PSS、RiskWitness 或 workload。

冻结输入：

| 文件 | SHA-256 |
|---|---|
| `probes/raftrs/Cargo.toml` | `8175cac50a75788172be7d20f6e789743777e3ca37743e7aa3bd6a5419b6804f` |
| `probes/raftrs/Cargo.lock` | `85c37848d2f6366e806ccfeded4bc754defdfa93756a6225ab71c1d3a7ddee99` |
| `probes/raftrs/src/main.rs` | `064dd277760a5d251e10705079b99cf459ab7d55e2806ca5e1248ec881db0fa0` |

同一组摘要也保存在 `inputs.sha256`，复验入口会在构建前机械检查。

复验：

```bash
make probe-raftrs-core
```

首次复验需要从 crates.io 下载 Cargo.lock 固定的依赖。`target/` 被忽略，不进入 Git。
