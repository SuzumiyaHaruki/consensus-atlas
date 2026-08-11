# M4.15 阶段总结：协议输入边界与代码整理

> 日期：2026-08-06
> 阶段结论：通用执行/规划边界不再解释 Raft 操作名；早期未进入可信实验路径的实现已移除

## 已完成

- 新增通用 `protocol-input` event，并把操作名作为结构化 `operation` 字段；语义规范化将该字段
  纳入 fingerprint，两个不同操作不能因为目标节点相同而折叠为同一 PSS/结构轨迹。
- `driver.Manifest.Inputs` 成为协议输入的受信任词汇表。Test Plan、Campaign 和 Blind Planner
  都按 `kind + operation + payload_mode` 机械校验；通用包不再包含 `campaign/propose/query` 的
  协议含义。为重放冻结的 v1 plan，旧 event kind 只通过同一 Manifest 中的显式现代声明兼容。
- etcd/raft Driver 是唯一把 `campaign`、`propose`、`query` operation 映射到 `RawNode` API 的
  地方。Crash/restart 仍是 Runtime 原生生命周期操作，不会伪装成协议 API。
- 删除未进入当前可信实验路径的 toy Adapter/profile/scenario、Contract-only 命令与 Python
  planner、以及旧的全信息 Agent Campaign Coordinator。保留的模型路径只有 Blind Planner v1。
- `internal/sutbuild` 不再写死 etcd/raft Driver 的 linker variable。新的受控 build spec v4
  要求可信 concrete build pack 显式给出该变量；旧 v1–v3 build input 继续作为只读审计证据，
  缺少该字段时会明确拒绝重建，而不会猜测具体协议包路径。

## 验证边界

- 回归测试确认：仅 Driver 声明的 generic protocol input 可进入 Campaign；required payload
  不能省略；不同 protocol operation 产生不同结构 fingerprint。
- 静态 import audit 确认 `internal/` 不直接 import `go.etcd.io/raft/v3`、`raftpb` 或
  `drivers/etcdraft`。
- 这次整理不重跑或改写历史 public pilot，因此没有新的 Coverage、PSS 或 defect-evaluation
  数值，也不改变既有 pilot 的结论。

## 未证明与下一步

这只证明当前边界更容易接入第二个实现，不证明 Family Pack 已跨实现复用。下一项具有研究
判断力的工作仍是：在仓库外冻结多个互相独立的 qualified candidate/control scope，完成
exposure audit 后，用共同预算比较 Random、DFS、专家、无反馈 Planner 与 Blind Planner。
第二个独立 CFT 实现接入是通用性主张的后续硬验收，而非靠抽象层数量替代。
