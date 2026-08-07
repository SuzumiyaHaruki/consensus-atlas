# Portable CFT v2 双实现能力矩阵

`capability-matrix.json` 是 M5.4e 冻结工件。它不是人工支持清单：测试会用同一个
`portable-cft-control-v2` Profile 重新运行 etcd/raft 与 HashiCorp Raft 的可信 qualification，
再从两个 `QualificationReport` 机械生成每项状态和 `shared_validated` 交集。

当前结果：etcd/raft 的 8 项 required capability 全部 validated；HashiCorp Raft 为 3 项
validated、6 项 Unsupported。跨实现实验只可使用两边共同 validated 的三项：

- `runtime-owned-message`；
- `crash-restart-incarnation`；
- `opaque-invoke-boundary`。

Unsupported 或 unvalidated 能力不得获得 Coverage/PSS/Agent 实验资格或分数。矩阵不表示协议正确，
也不表示 HashiCorp Raft 已具备虚拟时钟、受控 SUT entropy 或 strict replay。

复验：

```bash
make audit-portable-cft-matrix
```
