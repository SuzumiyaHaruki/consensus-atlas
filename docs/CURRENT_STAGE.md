# 当前阶段

日期：2026-08-08

阶段：M5.17b Trace Mutation 强基线完成

## 输入、处理、输出

```text
输入
  qualified Adapter + opaque workload + policy/fault budget
  + target-owned DecisionProjector
  + control/candidate build identity
                         |
                         v
处理
  Runtime enabled -> strong baseline selector -> 唯一执行
  -> strict replay
  -> ExecutionBundle 自校验
  -> TraceIntegrity + Agreement
  -> control/candidate evaluator
                         |
                         v
输出
  完整但本地保存的可重放证据包
  + 小型 build audit / benchmark manifest / evaluation ledger
  + control-pass / killed / survived / false-positive / invalid
```

M5.17a/b 没有增加第二套执行器。action-class random 在 M5.16 的唯一 bundle 路径上均匀选择
ActionKind，再在该 class 内选择 Runtime 当前提供的 Action；trace mutation 精确重放源 Trace 前缀、
交换首对相邻 message deliveries，再使用声明的确定性 priority suffix。两者都不能修改 enabled 或
伪造 ActionID。

M5.16 没有引入 Agent、Coverage 或第二套执行器。`ExecuteQualifiedBundle` 复用 M5.15 的唯一执行路径，
只把该次运行中已经产生的完整 Trace、Evidence、Snapshot、Core PSS、client result、Qualification 和
成本组织成自校验 `ExecutionBundle`。

## 为什么需要显式 preparation

M5.15 的 `OfferInvoke` 会在 scheduler decision 之前改变 Runtime 的 offered state，但旧 trace 只记录
`Select`。第一次运行 TraceIntegrity 时，这个缺口导致 state-digest chain 无法闭合。M5.16 没有忽略
该差异，而是增加 `PreparationRecord`：记录 ordinal、下一 decision、冻结 Action、before/after state
digest 和自身 digest。Oracle 现在同时校验 prepare 与 scheduler record 的状态链，并要求刚准备的
Action 立即被选中。

## DecisionProjector 信任边界

通用 Agreement 只接收：

```text
step + participant + decision position + exact value digest
```

协议或实现专用 Evidence 的解释留在可信目标 composition。etcd/raft 使用
`official-etcdraft-v2/applied-prefix-digest-v1`，把每个节点的 applied index 与累计 application digest
映射为上述记录。`internal/oracle` 不导入 Raft 类型，也不解码 Raft Evidence。

## 公开 calibration 结果

| trial | BuildID | decisions | primary/replay work | 结果 |
|---|---|---:|---:|---|
| official control | `go.etcd.io/raft/v3@v3.6.0` | 96 | 98 / 98 | `control-pass` |
| command-data calibration | `sut-c9811ab0ed8e2f39` | 96 | 98 / 98 | `killed` at step 55 |

两份 bundle 都通过 TraceIntegrity。candidate 在 decision position 5 出现 n1/n3 exact value digest
冲突，被 Agreement 检出。最终账本：1 control、0 false positive、1 candidate、1 killed root cause、
0 invalid。PSS/Coverage 没有参与 verdict。

公开 candidate 由固定 `go.etcd.io/raft/v3@v3.6.0` module cache 的 `raft.go` 唯一文本转换产生；构建使用
offline/readonly module、固定命令 allowlist、`-trimpath` 和 linker-bound opaque BuildID。build audit
记录原始/变换源码、module、binary、command 和 toolchain identity，并确认 module cache 未修改。

这是公开 calibration，只证明构建、执行、投影、Oracle 与 evaluator 链路能区分一个已知构造差异；
不是历史 holdout，不支持 Agent 方法优势结论。

## M5.17a 实际结果

action-class random 使用同一 `single-write-v1` workload、FaultEnvelope、96 decisions 和 98 primary/replay
work，冻结 seed 为 1，不筛选 seed。官方 control 发现 83 个 Core PSS 状态（prefix area 3681），完成并
提交 1 个 workload；轨迹包含 crash/restart 1/1、drop/duplicate 2/1、deliver 17、temporal 27 和
effect 46，strict replay、TraceIntegrity 与 Agreement 全部通过。

同一公开 command-data candidate 在本策略下 `survived`，而 M5.16 fixed workload 在只发现 56 个 Core
PSS 状态时于 step 55 将其检出：

| policy | Core PSS states | candidate verdict |
|---|---:|---|
| fixed workload | 56 | `killed` |
| action-class random seed 1 | 83 | `survived` |

这是应当保留的负结果：更多 coarse PSS 状态不等于覆盖到某个根因所需的时序。单一公开 candidate
不能用来排序方法，但已经机械否证“状态发现数本身就是最终测试质量”的解释。

action-class benchmark 还分别绑定 control/candidate 的完整 config digest，策略、workload、
FaultEnvelope 或 admission 不同的 bundle 会记为 `invalid`，不能进入 verdict。

## M5.17b 实际结果

trace mutation 的公共选择规则固定为“按 source decision 顺序选择第一对相邻 `deliver-message`”，本次
得到 decisions 46/47；没有尝试多个 pair 后筛选成功者。operator 使用 exact source prefix、adjacent
swap 和 digest-bound priority suffix。任一精确 ActionID 不 enabled 时返回稳定
`EXPERIMENT_TRACE_MUTATION_ACTION_NOT_ENABLED`，不静默 fallback。

官方 control 的 mutation 完成 96 decisions、1 个 committed workload 和 strict replay，发现 56 个
Core PSS states、prefix area 3324；source 数值也为 56/3324，但两条 trace digest 不同。这里没有运行
candidate，所以没有 defect verdict。

成本不能只看 mutation report 内的 98/98：生成 source Trace 也用了 98 primary / 98 replay。因此本次
baseline method 的真实成本是 **196 primary / 196 replay**。另一个公开依赖性失败校准在 decision 34
明确拒绝，记录 33 已完成 decisions、35 primary work、0 replay；它只验证失败账本，不是 method trial。
checked-in 小型 summary 由集成测试逐字段重算。

## M5.16R 删除结果

删除前冻结基线：

- 29,931 行 production Go/Python；
- 12,559 行测试；
- 42,490 行总计；
- 28 条 legacy production import edge；
- 六个 v1 核心包约 2,734 行生产代码、1,210 行测试；
- 直接 legacy dependency cone 约 7,047 行生产代码、2,257 行测试。

M5.16R 删除完成时可编译 Go：

- 14,564 行 production；
- 6,299 行测试；
- 20,863 行总计；
- legacy production import edge = 0。

加入 M5.17b 后当前为 15,136 行 production、6,659 行测试；相对删除完成点净增 572/360 行，主要是
action-class、trace mutation、envelope/config 约束和对应回归，没有恢复任何 legacy import。M5.17c
继续以此为体积基线，若需要恢复旧 Campaign/Coverage 或新增第二执行器则停止。

实际 Git diff 当前净删除超过 2.2 万行。旧 Engine/Host/Driver、Raft Family、Coverage/Campaign、旧
DefectBench、onboarding、旧 Python Agents、migration harness 和对应 CLI 均已删除；旧资格在线重放
入口也已删除。历史 Markdown/JSON 继续保留为不可改写记录，`make audit-no-v1` 阻止源码回流。

## 冻结身份

| 工件 | 内容 digest / 文件 SHA-256 |
|---|---|
| M5.15 report | digest `e680aabd...a18948d7`；file `e7a00826...ad247b1` |
| official bundle | digest `6120cadb...8fcdf63`；file `efa3e9ae...3f53ba` |
| candidate bundle | digest `0c3d3cd8...30f0943`；file `35f1e7f7...f03091` |
| build input | file `1485c6e8...077b8` |
| build audit | file `7c63ec00...05238`；binary `a66c09ad...698a9` |
| evaluator manifest | digest `0ca75d75...dc17ad` |
| evaluator report | digest `fd4ecc9b...71e664`；file `9be0f831...3a96` |
| M5.17a control bundle | digest `b766e3f1...6c9b1`；file `7ceb497a...761889b` |
| M5.17a build audit | file `b9f47577...e5301c6`；binary `efd64480...0c4b8` |
| M5.17a evaluator manifest | digest `b132e297...ff838` |
| M5.17a evaluator report | digest `3886388d...8cb68`；file `083beda6...8b0b` |
| M5.17b mutation report | digest `e4a5a5eb...df154`；file `e13937a9...748ab` |
| M5.17b mutation bundle | digest `29168463...d87`；file `37b9665c...dcdeb` |
| M5.17b summary | file `96b447e5...53ea01` |

完整 bundle 约 1.9 MB/份，属于可再生本地证据并保存在 ignored `artifacts/`。仓库只提交约 7 KB 的
构建与评测摘要，避免 JSON 工件继续主导仓库体积。

## 当前没有证明

- 没有非公开 holdout candidate/control pair；
- 没有 Agent 与强基线的比较；action-class random/trace mutation 目前都只有公开 calibration；
- 没有证明 PSS 或未来 Coverage 能预测缺陷检出；
- 没有验证第二个严格确定性共识实现的同一 ExecutionBundle；
- 没有证明 Agreement 是所有共识协议唯一或充分的安全 Oracle；
- 没有证明 etcd/raft、ConsensusAtlas 或任何目标实现正确、完备或无缺陷。

## 下一步

进入 M5.17c：在相同 workload、FaultEnvelope、ExecutionBundle 和完整 work budget 上实现最小
PSS-guided corpus。PSS 只作为 batch feedback，不做 visited-state pruning；只有强基线暴露明确缺口后
才实现 Guarded TestIntent，不恢复旧 Campaign/Coverage。

## 阅读顺序

1. [M5.17b Trace Mutation](stage-m5.17b-trace-mutation.md)
2. [M5.17b 小型账本](../benchmarks/experiments/etcdraft-v2-trace-mutation-m5.17b/README.md)
3. [M5.17a Action-class Random](stage-m5.17a-action-class-random.md)
4. [M5.16 ExecutionBundle](stage-m5.16-execution-bundle.md)
5. [M5.16R v1 删除](stage-m5.16r-legacy-removal.md)
6. [架构](architecture.md)
7. [总体规划](ConsensusAtlas-总体规划.md)
