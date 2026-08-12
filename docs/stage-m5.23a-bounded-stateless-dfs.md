# M5.23a：有界无状态 DFS 基线

日期：2026-08-12

## 结论

M5.23a 完成了第一个不依赖 LLM、PSS 状态合并或 Adapter checkpoint 的系统搜索基线。可信层从 exact
trace prefix 使用 fresh Adapter 重建 Runtime-enabled 与 FaultEnvelope-admissible frontier，按规范化
ActionID 的 depth-first 顺序形成 `StateRef + ActionRef + PathMetadata` WorkItem。每个 child prefix 都由
重新构造的 SUT 执行一次、再由另一个 fresh SUT 严格 Replay 一次。

这使 Random、未来 Agent 与 DFS 可以开始共享同一个真实 ActionRef 边界。但当前只是小预算校准，不是
状态空间完整枚举、方法效果比较或缺陷检出实验。

## 通用边界

新增的协议无关对象只有四类：

- `ActionFrontierView`：exact prefix identity、snapshot/frontier digest 和 canonical ActionRef；
- `StatelessDFSSpec`：根前缀、Runtime/FaultEnvelope identity、depth/item/work 上限；
- `StatelessDFSWorkItem`：StateRef、当前 admissible ActionRef、父 ordinal、depth 和 child prefix；
- `StatelessDFSResult`：有序 WorkItem、停止原因及三段搜索成本。

DFS 不保存或反序列化 Adapter 内部状态，不使用 PSS key 去恢复状态，不合并重复语义状态，也不预测未来
ActionID。伪造或篡改 ActionRef、父链、prefix、digest 均失败关闭。WorkItem 只有在当前 fresh 重建 frontier
中逐字段匹配才可物化；随后可把一条父链编译为 exact Policy，交给原 qualified executor。

M5.21q 的 RiskFrontier 现在复用通用 Action frontier 的重建结果，再附加 family-owned RiskWitnessProgress；
历史 M5.21q identity 和冻结摘要保持不变。

## 成本边界

搜索成本分为：

1. `frontier_reconstruction`：为扩展一个前缀进行 fresh setup + strict prefix Replay；
2. `child_materialization`：再次重建相同 frontier 并执行一个已验证 ActionRef；
3. `child_verification`：第三个 fresh SUT 严格 Replay 新 child prefix。

所有 setup、prepare 和 scheduler decisions 均机械计费。Runtime initialization 单独报告，沿用现有
`PhaseWork` 口径不重复进入 WorkUnits。预算在动作开始前用前缀长度机械估算；不足时以
`work-unit-limit` 停止，不产生部分 WorkItem。

## etcd/raft 实验

公开校准固定使用 M5.21p 真实轨迹的 28-decision prefix：

| 项目 | 结果 |
|---|---:|
| max depth | 2 |
| max WorkItems | 6 |
| max search work units | 1,000 |
| expanded prefixes | 2 |
| emitted WorkItems | 6 |
| actual search work units | 443 |
| stop reason | `work-item-limit` |
| deterministic repeat | 是 |
| child fresh Replay | 6/6 |

第一个深度 1 分支为 message delivery；其下按 ActionID 规范顺序得到 temporal、drop、crash、delivery、
effect completion 等深度 2 WorkItem。最后一个叶路径编译成 exact Policy 后进入唯一 qualified executor：

- DFS child prefix：30 decisions；
- 实际执行的前 30 decisions digest 与 child prefix 完全相同；
- 完整执行：64 primary decisions + 64 replay decisions；
- execution work：primary 66、replay 66；
- strict Replay：stable；
- model calls：0。

完整 trace/bundle 不重复提交；小型摘要位于
`benchmarks/experiments/etcdraft-v2-stateless-dfs-m5.23a/summary.json`。

## 已证明与未证明

已证明：

- DFS 顺序只依赖可信 exact prefix 与 canonical ActionID；
- 相同 spec/root 两次运行产生逐字段相同结果；
- 所有 ActionRef 来自 fresh 重建的 admissible frontier；
- 搜索和执行成本分开、均非免费；
- 叶路径能被原 qualified executor 精确复现并 Replay。

未证明：

- 完整枚举实现的全部非确定性；
- DFS 优于 uniform/action-class 或 Agent；
- PSS/state equality、DPOR 或保守剪枝；
- 缺陷检出、误报率、Coverage/PSS 增益；
- OmniPaxos 或其他协议已经通过同一真实搜索门禁。

## 代码规模自审查

新增通用生产文件约 600 行，其中较大部分是 versioned identity、digest、成本和失败关闭验证；单元测试约
115 行，etcd/raft composition/冻结测试约 179 行。没有新增 Runtime、scheduler policy version、Campaign、
schema 文件或 target Adapter。M5.21q 重建逻辑净删除约 43 行重复代码。下一阶段不得继续扩充 DFS 对象；
应先让 OmniPaxos 使用同一 API，检验是否存在隐藏协议耦合。

## 下一阶段：M5.23b

在不修改 `internal/controlexperiment` DFS API 的前提下，把同一 root-prefix DFS 门禁接到 OmniPaxos：

- 使用其已有 six-capability admitted workload 与 Runtime config；
- 固定小 depth/item/work 上限；
- 验证两次 WorkItem 顺序一致、每个 child strict Replay；
- 将一个叶路径交回原 OmniPaxos qualified executor并核对 exact prefix。

如果必须新增协议字段、特殊 Action 顺序或 target condition 才能运行，应把它记录为普适性失败，而不是
把特殊规则加入通用 DFS。M5.23b 通过后，再进行 M5.23c 的 DFS 与 bounded uniform 等完整成本校准；真实
LLM Search Agent 仍放在强确定性基线可比较之后。

## 验证记录

`go test ./...`、`go vet ./...`、M5.21q 与 M5.22b-e 冻结回归、DFS 单元/真实目标定向 race、race shard
manifest、全部 JSON 语法、总体规划副本一致性和 `git diff --check` 均通过。仓库 Python unittest
discovery 仍为 0 tests；本阶段没有重复运行此前长时间无输出的全量 race，不能据此宣称全仓 race 通过。
