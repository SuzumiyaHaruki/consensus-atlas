# OmniPaxos v2 bounded stateless DFS portability gate（M5.23b）

该目录只冻结小型校准摘要，不复制完整 report、bundle 或 trace。输入是 M5.22b 已有 six-capability
admitted workload 的真实 OmniPaxos bundle；DFS 使用其 1-decision exact prefix、相同 Runtime seed 和
FaultEnvelope。

`summary.json` 由 `TestM523bOmniPaxosBoundedStatelessDFSIsDeterministicAndExecutable` 逐字段重算。通用
`internal/controlexperiment` DFS API 未修改；worker 子进程生命周期只在目标 composition test 中管理。

该结果是跨目标可迁移性门禁，不是与 etcd/raft 的等成本方法比较。两个阶段的 root prefix 长度和 frontier
不同，因此不得用 51 与 443 search work units 推断协议或方法优劣。
