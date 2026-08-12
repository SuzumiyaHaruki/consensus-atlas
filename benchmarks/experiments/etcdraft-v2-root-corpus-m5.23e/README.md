# etcd/raft v2 root corpus calibration（M5.23e）

该目录冻结一个在方法执行前声明的三 root corpus：同一 strict-Replay source 的 0/28/54 decision
prefix，分别锚定 source 初态、workload invoked milestone 和 old coordinator restarted milestone。
`phase_id` 只是说明，不参与 Action 选择或评价。

四种方法使用相同 depth=2、items=6、每 root work ceiling=1,500。实际边际可信成本为
2,395–2,397 work units，而不是完全相同；原因是不同路径长度使 execution/replay 相差 1 work unit。
结果如实记账，不做填充。corpus baseline 为 38 个 PSS states，各方法扣除整个 baseline 后得到
15/13/15/14 个 corpus-novel states，联合为 39。

这是公开校准，不是 Coverage、缺陷检出、显著性或方法排名。
