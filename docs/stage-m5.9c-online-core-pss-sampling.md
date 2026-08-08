# M5.9c：可信在线 Core PSS 采样

日期：2026-08-07

结论：Control Runtime v2 的真实执行现在可以在每个已应用公共 Action 后，把同一逻辑瞬间的 Runtime
Snapshot 与最新可信 Adapter Evidence 配对，生成通用 Core PSS sample。三节点 etcd/raft v2 的自然
选举及 follower crash/restart 轨迹在全新 Runtime 上得到完全相同的 sample 序列和 discovery ledger；
公共 Action、Runtime、Adapter Contract、Campaign、Agent 和 Coverage 均未修改。

## 1. 输入、处理与输出

```text
Runtime Snapshot + ActionRecord + latest Evidence
                         |
              trusted SemanticMapper
                         |
                         v
                 sealed Core PSS State
                         |
                         v
               step / digest / state sample
                         |
                         v
           discovery curve + first witnesses
```

`OnlineSampler` 只观察执行，不拥有 `EnabledActions` 或 `Select`，因此不能改变调度。采样边界固定为：

- Runtime 初始化稳定后的 step 0；
- 此后每一个成功应用的公共 Runtime Action。

这些是调度器可见、其他 Action 可以插入的决策边界，不是 `RawNode` 内部函数调用。Core PSS digest
会规范化节点、epoch 和 decision identity；pending message/effect、partition 和 lifecycle 等可调度控制
状态仍会保留。

## 2. 可信配对约束

每次 `Capture` 都机械检查：

- `ActionRecord.Outcome == applied`；
- record step/logical time 与采样后的 Snapshot 相等；
- step 相对上一个样本严格连续，不能跳步或重复；
- 新 Evidence 的 payload 和 envelope digest 有效；
- 无新 Evidence 时只复用上一份不可变副本；
- `psscore.Project` 再检查 Snapshot/Evidence logical time 一致；
- Core PSS state 经过封闭 vocabulary、canonicalization 和 digest sealing。

失败的采样不会提交新 Evidence、sample 或 last step。单元测试特意提交 stale Evidence，再用之前的正确
Evidence 重试同一步，证明失败没有污染采样器状态。

## 3. etcd/raft v2 行为见证

真实集成测试使用官方 `go.etcd.io/raft/v3 v3.6.0` 三节点 Adapter：

```text
bootstrap effects
→ natural periodic pulses
→ election messages
→ stable leader
→ follower crash
→ follower restart
```

结果：

| Metric | Result |
|---|---:|
| Applied decisions | 29 |
| Online samples, including step 0 | 30 |
| Unique Core PSS states | 20 |
| Runtime strict replay | pass |
| Fresh ActionID sample replay | exact match |
| Inactive participant witness | present |

测试先让 `controlruntime.Replay` 对完整 ActionRecord/trace digest 做严格重放，再在另一个全新 Runtime 中
逐个选择相同 ActionID并在线采样。两侧完整 `[]protocolstate.Sample` 和 `DiscoverySummary` 均逐值相等。

“20 个状态”不是覆盖百分比、正确性概率或方法优势：它只说明这条确定轨迹在当前 Core PSS Mapping 下
发现了 20 个唯一状态，其中包含消息、effect 和 lifecycle 控制上下文。没有共同预算下的其他搜索方法
结果时，不能用它评价 Agent。

## 4. 耦合与代码体积

- `internal/psscore/sampling.go`：100 行协议无关生产代码；
- etcd/raft 只增加 7 行 `CorePSSMapper` 包装，复用已有 `MapCoreEvidence`；
- legacy cross-run experiment 原样移动到 `internal/protocolstate/legacyexperiment`；
- `internal/protocolstate` 根包现在只包含通用 discovery ledger，不再编译 v1 `core.TraceRecord`；
- `internal/psscore` 的生产依赖图不包含 legacy core/driver/host/engine/explore/scenario；
- architecture gate 已把 `internal/psscore` 纳入 v2 禁止反向依赖检查；
- 没有新增 CLI、JSON schema、Action、Runtime field、Adapter capability 或实验分母。

旧 experiment 隔离只是消除隐式编译依赖，`Aggregate` 算法、类型字段和 JSON 输出没有改变，也不冒充
legacy consumer 已删除。28 条冻结 execution import 边必须继续单独机械复核。

## 5. 当前没有证明

- 还没有可由命令保存的 v2 run/campaign 报告；当前是可序列化 sample 的集成行为见证；
- legacy `Aggregate` 仍读取 v1 measured trace/projector，只是已隔离到明确子包；
- 没有跨运行 v2 union、prefix area 或共同 decision budget；
- 没有把 v2 Evidence 接入完整 Oracle、Coverage、Campaign、Agent 或 Defect Benchmark；
- 没有测试 proposal、ReadIndex、snapshot/compaction 或 membership change 的在线测量；
- 20 unique states 没有固定分母，不能与旧 Raft PSS 数字拼接。

## 6. 下一步：M5.9d

把 legacy cross-run `Aggregate` 的输入改为协议无关的 measured decision/sample 序列，并由 Raft v1
compatibility adapter 保持既有实验结果。随后用 etcd/raft v2 在线采样器形成第一个等 decision budget
的多运行 measurement witness。本阶段仍不接 Agent/Coverage，不新建第二套 Runtime。
