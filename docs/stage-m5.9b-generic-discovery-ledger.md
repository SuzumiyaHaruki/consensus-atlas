# M5.9b：通用状态发现账本

日期：2026-08-07

结论：单轨迹 discovery ledger 已从 v1 `core.TraceRecord` 解耦，只接受协议无关的
`step/key/state` sample。Raft v1 在 Family 层保留旧边界选择和投影；真实三节点 etcd/raft v2 的
Core PSS 已直接复用同一账本。没有修改 Action、Runtime、Agent、Coverage 或 schema。

## 1. 新边界

```text
legacy v1 trace                         v2 Runtime + Evidence
       |                                         |
       v                                         v
Raft Family Projector                    Core PSS Mapping
       |                                         |
       +---------- step / key / state -----------+
                              |
                              v
                  protocolstate.Discover
                              |
                  curve + first witnesses
```

`protocolstate.Discover` 不再决定哪些执行边界值得采样，也不读取协议快照。调用方必须先提交：

- `Step`：当前测试方法定义的单调测量位置；
- `Key`：已由受信任 Semantic Mapping 规范化的状态标识；
- `State`：用于审计的首见状态见证。

账本只完成去重、累计曲线和首次见证保留。空 PSS identity、空 key、负数或非严格递增 step 会被拒绝。
`pss_id` 对 Core PSS
使用版本化 Mapping identity，旧 Raft 继续使用冻结的 Family PSS identity；两类结果不能拼接。

## 2. 旧 Raft 行为保持

Raft v1 的 `Projector.IsSample` 仍只接受成功的 `acknowledge/crash/restart`，`persist/sync` 等 Ready
微步骤仍被排除。区别只是投影现在先发生在 `families/raft`，随后才把通用 sample 交给账本。

冻结测试精确检查原五步 trace 的结果：

| Sample step | Samples | Unique states | New state |
|---:|---:|---:|---|
| 1 | 1 | 1 | true |
| 4 | 2 | 1 | false |
| 5 | 3 | 2 | true |

两个首见 witness 仍分别绑定 step 1 的 running state 与 step 5 的 crashed state。旧跨运行
`Aggregate` 尚未迁移；它的 v1 `Projector` 明确留在 legacy experiment 文件中。

## 3. Core PSS 行为见证

etcd/raft v2 集成测试启动真实三节点 Runtime，让官方实现通过自然 Tick/消息投递完成选主，然后把：

1. 初始 passive 集群；
2. elected 集群；
3. 同一 elected 状态的重复投影；

作为三个 Core PSS sample 提交给通用账本。结果为 3 samples、2 unique states；首次见证在 step 1/2，
第三个 sample 不增加状态数。这证明 Core PSS 可以复用账本，但尚不是跨 run 实验或 runner 输出。

## 4. 代码体积与依赖门

- `internal/protocolstate/discovery.go`：68 行降为 61 行；
- legacy `experiment.go` 因接口归位由 133 行增为 140 行；
- `internal/protocolstate` 两个生产文件合计仍为 201 行，净增 0；
- Action、Runtime、Agent、Coverage、schema 的生产代码均未修改；
- `make audit-legacy-consumers` 仍得到原 28 条 production import 边。

28 条边没有下降是预期结果：本轮移除的是 discovery 函数的 v1 数据模型依赖，而 runner 和 experiment
仍执行 legacy Engine。把文件移动或把接口换名不能冒充 v1 consumer 已删除。

## 5. 当前没有证明

- v2 trace 没有逐步完整 Runtime Snapshot，尚不能从保存的 trace 离线重建 Core PSS 曲线；
- 当前 Core PSS witness 是单次真实执行中的集成测试，不是共同预算下的 Random/DFS/Agent 比较；
- legacy 跨运行 `Aggregate` 仍接收 v1 trace/projector；
- runner、Campaign、Agent、Coverage、Oracle 和 benchmark composition 尚未迁移到 v2；
- Core PSS 状态数没有固定分母，也不能单独作为最终测试质量百分比；
- 没有产生新的 capability、覆盖结果、历史缺陷检出或方法优势结论。

## 6. 下一步：M5.9c

先实现一个最小的可信 v2 Core PSS sampling composition：在执行时把同一逻辑瞬间的 Runtime Snapshot
与 Adapter Evidence 交给固定 Semantic Mapping，生成可保存的通用 samples，并用严格 replay 检查
sample 序列稳定。它不应把 PSS 塞进 Control Runtime，不修改公共 Action，也不直接迁移 Campaign/Agent；
完成真实单 run discovery 工件后，再决定跨运行 Aggregate 的通用化方式。
