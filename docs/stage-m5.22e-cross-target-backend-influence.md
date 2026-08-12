# M5.22e：跨目标 backend 执行影响门禁

日期：2026-08-12

## 结论

M5.22e 通过。现有 `bounded-action-class` 与 `bounded-uniform` 在不增加 Runtime、Action、schema、
target 或模型调用的前提下，已经能够作为两个共同 backend，在 etcd/raft 和 OmniPaxos 上把同一个
协议无关 Intent lowering 为不同、可重放的真实 Action trace。

这修复了 M5.22d 暴露的“preference 只有身份变化、没有执行影响”问题。它只证明选择权存在，不证明
Agent 会做出更好的选择。下一阶段因此不是直接调用真实 LLM，而是先建立可复核的强确定性搜索基线。

## 冻结边界

- targets：官方 etcd/raft 与未修改 OmniPaxos；
- backends：只复用已有 bounded action-class 与 bounded uniform；
- common surface：6 项已验证 capability，`Invoke/DeliverMessage/FireTemporal` 三类 Action；
- target-local plumbing：etcd/raft 继续显式加入 `CompleteEffect`，不伪装成共同搜索动作；
- workload：每次执行一个 opaque input；
- seeds：预先固定为 1、2、3；
- budget：每次执行上限 96 decisions，并要求 fresh strict Replay；
- model/network/key：0/0/0。

两个 backend 只改变 preference 和对应可信 lowering。硬约束、seed、decision ceiling、可选 Action surface、
workload 与 admission 边界保持一致。未知 backend 失败关闭；M5.22b-d 的冻结 identity 回归保持不变。

## 实验结果

| Target | Seed | action-class decisions | uniform decisions | Trace 是否不同 |
|---|---:|---:|---:|---|
| etcd/raft | 1 | 42 | 57 | 是 |
| etcd/raft | 2 | 42 | 72 | 是 |
| etcd/raft | 3 | 43 | 56 | 是 |
| OmniPaxos | 1 | 29 | 53 | 是 |
| OmniPaxos | 2 | 34 | 42 | 是 |
| OmniPaxos | 3 | 34 | 43 | 是 |

12 次 primary 均完成 workload、达到共同 Intent 并得到稳定 Replay；6/6 个同 target/seed 的 backend pair
轨迹不同。action-class 与 uniform 的实际 decisions 合计分别为 224 与 323；这只是停止前成本观测，不能
在没有 discovery/缺陷结果的情况下解释为前者更好。

## 实现约束

M5.22e 没有新增策略算法。共同 Catalog 只是把两个已经存在、已经通过 admission 的 Policy 版本纳入同一
target-blind view；可信 composition 根据编译出的 strategy 构造对应 Policy。实验编排与冻结断言位于测试
文件；M5.22e harness 也不进入生产 binary。生产改动只把 M5.22b 的 plan/execute helper 参数化，同时
显式拒绝未知 strategy。

## 已证明与未证明

已证明：

- preference 可以在两个真实目标上选择两个合法共同 backend；
- 两个 backend 在同 seed、同上限、同 surface 下产生可复现的行为差异；
- 每条轨迹都经过原唯一 executor、admission、IntentOutcome 和 strict Replay；
- M5.22b-d 历史冻结结果没有漂移。

未证明：

- Agent、LLM 或任一 backend 的搜索质量优势；
- 缺陷检出、误报率或 holdout 效果；
- PSS 数量跨协议可比或覆盖分数足够；
- 已经具备 DFS/DPOR 等系统状态空间搜索。

## 下一阶段

M5.23a 实现最小 bounded stateless DFS。WorkItem 只包含 exact replay prefix 与该前缀重建出的 canonical
admissible frontier；DFS 只能选择 frontier 中已有的 ActionRef，不保存/克隆 Adapter 内部状态，也不做
状态等价合并。先在一个小预算下证明相同输入产生相同 frontier/WorkItem 顺序、每条探索轨迹均可 fresh
Replay，并与 bounded uniform 使用同一完整执行预算。通过后才允许 protocol-aware Search Agent 对
WorkItem 排序，并将 target-blind Agent 保留为消融项。

## 验证记录

`go test ./...`、`go vet ./...`、M5.22b/d/e 冻结回归、M5.22e 定向 race、race shard manifest、全部 JSON
语法、总体规划副本一致性和 `git diff --check` 均通过。仓库已无 Python 测试，unittest discovery 为
0 tests/OK。`make test-race-full` 在 method shard 长时间无输出后按既有阶段约定停止，未重复消耗时间；
这不等价于全仓 race 通过，定向 M5.22e race 用时约 128 秒并通过。
