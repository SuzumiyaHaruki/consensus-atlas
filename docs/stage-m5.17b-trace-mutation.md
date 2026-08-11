# M5.17b Trace Mutation 强基线

日期：2026-08-08

状态：operator、公开 control calibration 与失败记账完成

## 目标

M5.17b 在唯一 `ExecuteQualifiedBundle` 路径上增加第二种强非 Agent 基线：从一条已经严格重放的合法
Trace 生成一个局部变体。它不修改 Runtime enabled 集，不伪造 ActionID，不恢复 Campaign/Coverage，
也不把不可执行动作替换成“差不多”的动作。

```text
qualified source execution
        |
        v
strict-replay source Trace
        |
        v
deterministically select first adjacent deliver-message pair
        |
        v
exact prefix -> swap pair -> declared priority suffix
        |
        v
same Runtime / workload / bundle / replay / Oracle boundary
```

## 冻结 operator

版本为 `consensus-atlas/adjacent-trace-mutation/v1`，语义为
`adjacent-swap-with-priority-suffix`：

1. 变异点之前必须逐项选择源 Trace 的精确 ActionID；
2. 在变异点交换相邻两个 ActionID；
3. 交换完成后不继续假装精确重放旧后缀，而是使用计划中声明并进入 digest 的确定性优先级；
4. 任一精确 ActionID 当前不在 enabled 集时，返回
   `EXPERIMENT_TRACE_MUTATION_ACTION_NOT_ENABLED`；
5. 不静默 fallback，不把部分执行包装成 measurement-complete Report。

公共选择规则固定为：按源 Trace decision 顺序扫描，选择第一对相邻的 `deliver-message`。这次源 Trace
的变异点是 decisions 46/47。规则不是先尝试多个 pair 再保留成功者，因此本结果不含 pair 筛选。

ActionID 的全局序号可能因交换后的产出顺序改变，所以旧 Trace 的完整后缀不能可靠地逐 ID 重放；显式
priority suffix 是计划的一部分，准确表达“局部 splice 后重新调度”，避免把后续正常分叉误报为变异
不可执行。

## 公开 control 结果

输入仍是 official etcd/raft v3.6.0、`single-write-v1`、相同 FaultEnvelope 和 96 decisions。

| 项目 | source | completed mutation |
|---|---:|---:|
| scheduler decisions | 96 | 96 |
| primary / replay work | 98 / 98 | 98 / 98 |
| Core PSS states | 56 | 56 |
| prefix area | 3324 | 3324 |
| workload completed | 1 | 1 |
| strict replay | pass | pass |

变异 Trace digest 为 `bfce0841...a78c11`，与 source 的 `34d0d389...b225c` 不同；变异 report/bundle
digest 分别为 `e4a5a5eb...df154` 和 `29168463...d87`。交换后的可观察 PSS discovery 数值恰好相同，
这不表示两条 Trace 等价，只表示当前 coarse Core PSS 没有区分该局部顺序。

本阶段只运行正确 control 来校准 operator，没有运行 candidate，也没有产生 defect verdict。

## 不可执行变体和完整成本

公开失败校准交换 source decisions 34/35：decision 35 的持久化 effect 由 decision 34 的消息投递产生，
所以交换后该 effect 在 decision 34 尚未 enabled。执行在选择前明确失败，记录：

- phase `primary-policy`；
- reason `EXPERIMENT_TRACE_MUTATION_ACTION_NOT_ENABLED`；
- 33 个已完成 scheduler decisions；
- 1 setup、1 runtime initialization、1 preparation，共 35 primary work units；
- 0 replay work。

该失败仅证明不可执行路径的记账边界，不是 baseline method trial，也不用于计算检出率。

成功 report 自身只能描述单次 mutation execution 的 98/98。方法必须先取得 source Trace，因此本次
baseline 的完整成本是 source 98/98 加 mutation 98/98，即 **196 primary / 196 replay work units**。
失败校准另计 35 primary；本阶段全部实际观察成本为 231 primary / 196 replay。小型
`summary.json` 由集成测试对 source、mutation、failure 和三组成本逐字段重算，避免隐藏准备成本。

## 可信边界

- operator 位于协议无关 `internal/controlexperiment`，不导入 Raft 类型；
- etcd/raft composition 只负责提供 source workload、Adapter factory、PSS mapper 和 projector；
- mutation 只能引用 source Trace 中已经冻结的 ActionID；
- Runtime 仍独立决定 enabled，Replay/Oracle 仍读取真实 Trace；
- PSS 没有决定成功、失败或任何 defect verdict；
- 本阶段没有调用 LLM。

## 工件与验证

小型账本位于
`benchmarks/experiments/etcdraft-v2-trace-mutation-m5.17b/summary.json`。完整 report/bundle 位于 ignored
`artifacts/experiments/etcdraft-v2-trace-mutation-m5.17b/`，避免提交重复的大型 Evidence body。

```bash
make experiment-etcdraft-v2-trace-mutation
go test ./internal/controlexperiment ./cmd/control-experiment -count=1
```

summary 文件 SHA-256 为 `96b447e5d040eec6ba7f69534548e188aa103aa80bad055dc7c58ed06053ea01`。
阶段验证已通过：`make test`、`go vet ./...`、`go test -race ./...`、125 个 checked-in JSON
语法、4 个相关 build input/audit schema 实例、85 个本地 Markdown 链接、总体规划逐字节
一致与 `git diff --check`。仓库已删除 Python Agent 实现，`unittest discover` 正常运行 0 项。
本机 `jsonschema 3.2.0` 不支持 2020-12 metaschema，因此相关实例使用 Draft7 兼容 fallback。

## 当前没有证明

- 没有 candidate/control defect 结果，更不是非公开 holdout；
- 没有证明 trace mutation 优于 fixed、action-class random、DFS 或 Agent；
- 单个成功 pair 不能代表其他 pair 的可执行率或效果；
- 相同 56 states/3324 area 不能证明两条 Trace 语义等价；
- 没有证明 PSS discovery 能预测外部缺陷检出。

## 下一步

M5.17c 只实现最小 PSS-guided corpus：使用相同 ExecutionBundle 和完整成本口径，把 batch 级新状态反馈
用于选择下一条 seed/局部变异，不做 PSS visited-state pruning。先与 fixed、action-class random 和本
trace mutation 基线在同一公开 control 上比较可执行率、状态发现曲线和真实总成本；没有 candidate 时
不宣称方法效果。
