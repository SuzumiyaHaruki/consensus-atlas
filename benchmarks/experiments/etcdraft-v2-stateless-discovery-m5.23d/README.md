# etcd/raft v2 stateless discovery calibration（M5.23d）

该目录冻结 M5.23c 四个 matched traversal methods 的搜索后只读 discovery 摘要。每个 WorkItem 都重新
编译成 exact Policy，并通过原 qualified executor 执行到 child prefix；通用 projector 调用已注册
SemanticMapper 从 trace/evidence 重算 Core PSS，再与严格 Replay 稳定的 ExecutionBundle 样本核对，
而不信任搜索内部状态。

每种方法的搜索成本为 443 work units；6 个 discovery bundles 另需 primary 191 + replay 191，因此可信
evidence 总成本为 825。摘要只保存共同成本一次和各方法的集合 digest，不提交 24 个完整 bundles/traces。

共同 root 已有 19 个 PSS 状态；四个方法在 root 之后分别新增 6/5/6/6 个状态，联合为 22。
该结果不是 Coverage、正确性、缺陷检出或方法排名；单 root 不支持稳健比较。
