# etcd/raft v2 restricted Search Agent calibration（M5.23f）

该目录冻结 Search Agent 的权限门，不评价 Agent 能力。可信代码生成 knowledge-bound request；提案唯一
可变字段是 `action_ids`，且必须是当前 frontier ActionID 的完整 permutation。validator 将 ID 映射回原
ActionRef，并拒绝遗漏、额外、重复、虚构 Action 和未知 JSON 字段。

确定性 no-model reverse-frontier fixture 在 28-decision root 上被调用 2 次，得到与 canonical 不同的
6-item 顺序，搜索成本 443；叶路径回到原 qualified executor 后 strict Replay 稳定。模型调用数为 0。

这证明权限边界可执行，不证明真实模型有效、PSS/Coverage 更高或发现了缺陷。
