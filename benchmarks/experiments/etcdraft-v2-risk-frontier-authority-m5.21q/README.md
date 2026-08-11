# M5.21q Risk Frontier authority gate

本目录只冻结一份小型、可重算的 no-model authority gate 摘要。集成测试从 M5.21p
真实 etcd/raft 轨迹的 28/53 decision 前缀执行严格 Replay，重新取得当时真实的
enabled/admissible frontier，再用 exact ActionID 分别选择：

- decision 29 的 `Crash(n1)` 与普通进展动作；
- decision 54 的 `Restart(n1)`。

两个策略均通过唯一 qualified executor 执行 64 decisions 并严格 Replay。风险策略重现
M5.21p 的风险轨迹；进展策略完成写入且没有形成 leader-change 风险序列。工件没有保存
完整 trace、bundle、snapshot 或协议私有 evidence，重建前缀的工作量另行记账。

该校准只证明“冻结的精确前沿引用能够改变真实执行”。它不是 Agent 效果、搜索方法
排名、Oracle 判定、formal holdout 或绝对测试质量证据。
