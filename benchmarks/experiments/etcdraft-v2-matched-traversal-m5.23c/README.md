# etcd/raft v2 matched stateless traversal calibration（M5.23c）

该目录冻结 canonical DFS 与 seeded-uniform frontier order 的同起点、同预算校准摘要。四个方法共享同一
28-decision exact prefix、同一 Runtime/FaultEnvelope、depth=2、items=6、work ceiling=1,000，以及完全
相同的 child materialization/fresh Replay 执行器。

`summary.json` 由 `TestM523cEtcdraftTraversalMethodsShareExactRootAndBounds` 逐字段重算。完整 report、bundle
和 trace 不重复提交。该结果证明方法身份、顺序差异、确定性和成本可比性，不评价 discovery quality、缺陷
检出或 Agent 优势。
