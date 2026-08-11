# M5.7a Core PSS IR 与 etcd/raft Semantic Mapping

日期：2026-08-07

状态：最小实现完成

## 本阶段结论

M5.7a 已把 M5.7 冻结的数据职责实现成一条最小、只读的投影链：

```text
Control Runtime Snapshot ---------> Runtime Control Context
etcd/raft public Evidence --------> Consensus Semantic Graph
                                      |
                                      v
                          canonical Core PSS State + digest
```

新代码没有修改 `control.Action`、`controlruntime.Runtime`、Adapter 契约、Replay、Oracle、Coverage
或旧 Raft PSS。Core PSS 目前只作为新的状态发现身份；旧实验继续报告原有 PSS ID，两种数字不能拼接或
声称行为等价。

## 输入、处理与输出

输入有两份同一逻辑时刻的可信数据：Runtime `Snapshot` 与 Adapter 的版本化 `EvidenceEnvelope`。
`psscore.Project` 首先机械检查二者的逻辑时间，再进行以下处理：

- Runtime 直接投影 participant lifecycle、相对 incarnation、pending owner 是否来自旧 incarnation、
  双向 blocked link、非终态 pending item、route、依赖种类摘要与最早 temporal frontier；
- etcd/raft Mapping 只读取公开 `Evidence`，把 follower/候选/leader/stopped 映射为封闭的
  `passive/contending/coordinating/inactive` mode；
- term 与 commit/applied index 只形成相对 rank，不进入最终状态；
- 当前公开 `ApplicationDigest` 是整个应用前缀摘要，不是单个 DecisionUnit 的命令，因此 Mapping 明确
  不把它伪装成 Value；Core 支持 Value，但本映射暂不产生 Value entity；
- 最多八个 participant 通过确定性重命名取得稳定 Core key，所有集合排序后由 canonical digest 封存。

输出为版本化 `psscore.State`，包含 `schema_version`、`mapping_id`、Control Context、Semantic Graph 与
digest。该 digest 是发现键，不是正确性证明、覆盖百分比或搜索剪枝授权。

## Core v1 的封闭内容

| 部分 | 当前内容 |
|---|---|
| entity | Participant、Epoch、DecisionUnit、Value、Evidence |
| relation | belongs-to、proposes、supports、depends-on、conflicts-with、precedes、decides、persists、applies |
| stage | unknown、proposed、supported、accepted、decided、applied |
| participant mode | inactive、passive、contending、coordinating |
| 明确排除 | Action/Item ID、绝对 logical time、原始 payload、原始 term/index/digest、host microstep |

`participant mode` 是 Participant 的协议无关执行属性，不是 Raft role 枚举；无 leader 的协议可以保持
passive，具体 instance 的协调关系由 `proposes/supports` 等边表达。

## 实际验证

目标测试使用官方 `go.etcd.io/raft/v3 v3.6.0` 三节点 Adapter：

1. 从真实初始 Evidence 得到 3 个 passive Participant；
2. 只选择 Runtime 提供的 Ready effect、message 与最早 periodic pulse，让系统自然选主；
3. 稳定状态得到恰好 1 个 coordinating Participant；
4. 选主前后 Core digest 不同，同一最终快照重复投影 digest 相同；
5. Core 变形测试进一步确认节点名、native item/message/timer ID、绝对时间、payload 与规范化 Value 身份
   改变时 key 不变，而增加一条 pending message 时 key 改变；
6. 同一 pending message 来自当前/旧 incarnation 时 key 不同；缺少 Participant entity、证据时间错位
   和 digest 变造由机械校验拒绝。

目标测试命令：

```text
go test ./internal/psscore ./adapters/etcdraftv2
```

## 两个现有 Adapter 的重复职责审计

本轮只读比较了 `adapters/etcdraftv2` 与 `adapters/hashicorpraftv2`，没有创建 `adapterkit`：

| 表面相似项 | 实际差异 | 本轮决定 |
|---|---|---|
| Manifest 组装 | topology、clock、entropy、durability 与 replay 保证不同 | 保留目标声明 |
| pending command/Submit | 都需冻结 command，但稳定条件和 command schema 不同 | 仅记录候选 helper |
| Check | eligibility 几乎全部依赖目标状态与原生接口 | 不抽取 |
| RunUntilYield | etcd/raft 为同步 RawNode/Ready；HashiCorp 为 goroutine/RPC 等待 | 不抽取 |
| finishYield/Emission | 都 Seal emission；evidence 时机、terminal yield 与状态摘要不同 | 仅记录 Seal 候选 |
| Collect | etcd/raft 返回深拷贝；HashiCorp 当前直接返回缓存值 | 先统一行为见证，不抽取 |
| SnapshotEvidence | etcd/raft 即时计算；HashiCorp 缓存 applied-only evidence | 不抽取 |
| SnapshotEntropy | strict native tape 与 audit-only provider 语义不同 | 不抽取 |
| stable ID | namespace、因果输入和原生对象身份不同 | 不抽取 |

当前真正重复的只是少量防御性样板，尚不足以证明一个共享生命周期状态机。过早抽取会把 HashiCorp 的
非 strict、goroutine 驱动语义带入下一目标，或迫使 etcd/raft 降级。因此 `adapterkit` 仍不存在；等
非 Raft 目标提供第二个真实消费者后，再从“同一行为见证通过”的交集提取。

## 明确限制

- v2 trace 目前只保存 state digest 与 Evidence，不保存每一步完整 Runtime Snapshot；因此本轮没有把
  Core PSS 接入跨 run discovery ledger、Coverage、Agent 或 Campaign；
- Runtime Snapshot 尚不暴露当前 Adapter YieldID；投影会拒绝 logical time 错位，但同一 logical time
  内的旧 Evidence 仍需由可信 composition root 保证与快照来自同一个 stable yield；
- pending dependency 目前只保留 dependency item kind 的多重集，是保守形状摘要，不是原生 DAG 同构；
- participant 重命名使用最多八节点的精确枚举，适合当前有界小集群，尚未做大集群性能结论；
- etcd/raft 公共 Evidence 没有 vote、日志冲突、逐 entry persistence 或 quorum 证据，因此这些事实没有
  被 Mapping 猜测；需要时只能进入经过校准的扩展观察/Extended PSS；
- `ApplicationDigest` 只证明应用前缀相同/不同，不能可靠恢复逐 DecisionUnit Value；当前 etcd/raft
  Mapping 因此不产生 Value entity；
- 没有 HashiCorp Core Mapping、EPaxos Binding/Mapping、跨协议 ledger 或 Agent 生成结果；
- 没有证明 Core PSS 完备，也没有证明其发现数能预测缺陷检出。

## 下一最小阶段

进入 M5.8 feasibility spike 前先保持当前代码冻结：不再扩展 Core schema，也不迁移 Coverage/Agent。
下一阶段只检查 `efficient/epaxos` 是否能通过官方接口形成稳定 peer item、自然时间边界、只读 Evidence
以及到既有 Core IR 的 Mapping。任何协议专用 Action、Runtime type switch、Core 字段或复制协议状态机
需求都记为抽象失败/Unsupported，而不是继续增加公共代码。
