# M5.9d：通用跨运行状态聚合

日期：2026-08-07

结论：跨运行 `Aggregate` 已从 v1 `core.TraceRecord`/Projector 解耦，只接受共享初始 sample 和按计费
顺序排列的 measured decisions；每个 decision 可以携带一个同 step sample，也可以只计费而不采样。
Raft v1 通过隔离的 compatibility adapter 保持旧实验指标；两个真实 etcd/raft v2 运行已在相等 decision
预算下形成首个跨运行 Core PSS union 行为见证。

## 1. 通用测量输入

```text
MeasuredRun
  run number
  initial Sample                 # budget 0
  decisions[]                    # every element costs one decision
    step
    optional Sample              # absent still costs one decision
```

这个模型保留了旧实验的重要边界：只有命中协议采样点的 decision 才增加 `ProtocolSamples`，但所有执行、
丢弃或复制 decision 都进入 `TotalDecisions`、curve 和 prefix area。调用方不能删除“没有新状态”的步骤来
提高发现速度。

通用 Aggregate 机械检查：

- PSS identity 非空，至少一个 run；
- run number 为正且不能重复；
- 所有 run 的 initial key 相同；
- 每个 run 内 decision step 严格递增；
- sample 必须与所属 decision step 相等且 key 非空；
- 首见 witness 绑定 run、run decision、global decision 和 trace step。

输出 JSON 字段、prefix area 和 `SelfNormalizedArea = area/(B*D(B))` 定义保持不变。

## 2. Raft v1 compatibility

`internal/protocolstate/legacyexperiment` 现在只完成：

1. 验证一条 v1 measured trace record 对应一个计费 decision；
2. 用冻结的 Family Projector 投影 shared root 和选定 trace boundary；
3. 把未采样 decision 也写入通用 measured sequence；
4. 调用根 `protocolstate.Aggregate`。

旧测试继续得到 3 decisions、2 protocol samples、3 unique states、prefix area 7 和 normalized area 7/9。
本机现有 128-decision Random 产物与 fresh 当前运行的完整 JSON 因此前 ReadIndex capability/Ready evidence
扩展而不同，但两者规范化 `protocol_state_discovery` 子树 SHA-256 均为
`647a8de9dbae937050b56fe4dd9855fd25c45d5379beacebd85afba663de04fa`。因此本轮 Aggregate 泛化没有
改变该历史 PSS 结果；这个本机回归检查不是新的公开 benchmark 工件。

## 3. etcd/raft v2 等预算见证

两个 run 都从相同三节点、相同 seed/configuration 和相同 step-0 Core PSS root 开始：

- progress run：只选择透明的 effect/message/periodic-pulse progress 顺序；
- lifecycle run：先 crash/restart `n3`，之后使用相同 progress 顺序。

每条运行精确执行 32 decisions；所有 decision 都由 M5.9c 在线采样器产生 sample，并分别通过
`controlruntime.Replay`：

| Metric | Progress | Lifecycle | Union |
|---|---:|---:|---:|
| Runs | 1 | 1 | 2 |
| Decisions | 32 | 32 | 64 |
| Protocol samples | 32 | 32 | 64 |
| Unique Core PSS states | 23 | 23 | 45 |
| Strict replay | pass | pass | — |
| Union prefix area | — | — | 1454 |

Union 45 大于任一单 run 的 23，证明跨运行账本没有把两条轨迹错误合并；它也说明两者只共享少量状态。
45 仍然不是固定分母覆盖率，不代表 lifecycle 策略优于 progress，更不代表测试接近完备。

## 4. 代码体积与依赖

- 通用 `aggregate.go`：117 行；
- v1 compatibility adapter：由 141 行降为 69 行；
- 合计 186 行，相比隔离前实现净增 45 行；
- 增量用于显式 measured input、验证和 v1 adapter，没有第二份聚合算法；
- 根 `internal/protocolstate` 生产依赖不包含 legacy core/driver/host/engine/explore/scenario；
- Runtime、Action、Adapter Contract、Semantic Mapping、Agent、Coverage 和 schema 均未修改；
- 冻结的 28 条 legacy execution import 边仍需保持不增。

## 5. 当前没有证明

- v2 多运行见证仍在集成测试中，没有正式 CLI 或保存的 run report；
- progress/lifecycle 是两条透明固定策略，不是 Random、DFS 或 Agent 方法比较；
- 只计 scheduler decisions，没有新增 wall time、CPU、RSS 或 setup/replay work 报告；
- v2 尚未接入正式 Oracle、Coverage Ledger、Campaign、Agent 或 Defect Benchmark；
- 没有对不同 PSS Mapping 的状态数做比较，也没有完备性分母；
- 没有产生缺陷检出、false positive 或方法优势结果。

## 6. 下一步：M5.10

实现最小 v2 实验执行器：在冻结的 run/decision/setup/replay 预算下创建全新 Adapter/Runtime，接收透明
确定性策略，在线采样并输出可保存的 trace identity、严格 replay、跨运行 Core PSS 和完整工作量账本。
先提供非 Agent 的固定/Random 基线入口；Oracle 和 Coverage 未接入前，报告不得给出“测试通过”结论。
