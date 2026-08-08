# ConsensusAtlas

ConsensusAtlas 是面向 CFT/BFT 共识实现的确定性测试研究框架。当前主线已经收敛到一套协议无关的
Control Runtime：目标系统通过薄 Adapter 暴露消息、自然时间、生命周期、持久化副作用、外部输入和
Evidence；可信 Go 内核负责动作资格、调度、重放、语义状态采样、Oracle 和评测账本。

当前阶段是 **M5.17b 完成**：action-class random 与 trace mutation 两个强非 Agent 基线已经复用 v2
`ExecutionBundle -> trusted Oracle -> public calibration evaluator` 贯通；旧 v1 执行、Coverage、
Campaign、onboarding 和旧 Agent 实现锥体已从可编译源码删除。历史文档与 JSON 工件保留为研究记录，
但不再是可运行 API。

## 当前闭环

```text
qualified Adapter + opaque workload + bounded policy
                         |
                         v
        one deterministic Control Runtime executor
                         |
          +--------------+----------------+
          |              |                |
      full Trace     Core PSS samples  client results
          |              |                |
          +--------------+----------------+
                         v
                  ExecutionBundle
                         |
        target-owned DecisionProjector
                         |
                         v
     TraceIntegrity + protocol-neutral Agreement
                         |
                         v
       control/candidate evaluation ledger
```

通用 Oracle 不解析 Raft 字段。etcd/raft 的可信 composition 提供
`official-etcdraft-v2/applied-prefix-digest-v1` 投影，将 opaque Evidence 映射为
`participant + decision position + exact value digest`；Oracle 只比较这个最小协议无关输入。

## 已实现边界

- 协议无关 Action/ProducedItem/Adapter 契约与唯一 Control Runtime；
- Runtime-owned 消息保留、投递、丢弃、复制和分区；
- 只执行最早已到期 temporal item 的自然时间模型；
- incarnation、power-loss crash/restart、durable effect 和 application effect；
- 分域确定性 entropy、完整随机 tape 与严格 replay；
- 外部 Conformance Suite、版本化 Qualification 和 digest-bound Experiment admission；
- 固定 Core PSS IR、可信在线采样和跨 run 状态发现；
- opaque workload、Semantic Mapping guard、FaultEnvelope 和完整成本账本；
- 受限 fixed/random/Planner policy；模型只能提交 proposal，不能提交 enabled set 或判定结论；
- 自包含 ExecutionBundle：完整 trace、preparation state transition、Evidence、最终 Snapshot、Core PSS、
  client history、decision history、Qualification 和 work ledger；
- TraceIntegrity 与 Agreement 两个 v2 trusted monitor；
- source-bound 构建变体、build audit 和公开 calibration pair evaluator；
- 按 ActionKind 再按成员均匀采样的确定性随机基线，以及 digest-bound FaultEnvelope selectable view；
- 精确 source prefix、相邻 Action swap、显式 priority suffix 与不可执行变体记账的 trace mutation；
- 官方 `go.etcd.io/raft/v3 v3.6.0` 的完整 strict 路径；
- HashiCorp Raft v1.7.3 的部分资格，用于证明统一 Action 不等于统一控制强度。

## M5.16 公开校准结果

两侧使用相同 workload、96 scheduler decisions 和 98 primary work units：

| trial | build identity | TraceIntegrity | Agreement | 结果 |
|---|---|---|---|---|
| correct control | `go.etcd.io/raft/v3@v3.6.0` | pass | pass | `control-pass` |
| source-bound calibration | `sut-c9811ab0ed8e2f39` | pass | step 55 conflict | `killed` |

评测账本为 1 control、0 false positive、1 calibration candidate、1 killed root cause、0 invalid。
这是公开链路校准，不是非公开 holdout，也不能证明 Agent 优于 Random、DFS 或专家策略。Coverage/PSS
没有参与 verdict。

## M5.17a 强基线结果

action-class random 使用相同 single-write workload、FaultEnvelope、96 decisions 和 98 primary/replay
work。冻结 seed 1 的 control 发现 83 个 Core PSS 状态并通过重放与两个 Oracle；同一公开 candidate 却
`survived`。相比之下，M5.16 fixed policy 只发现 56 个状态但检出了它。

这个负结果被原样保存，没有筛选 seed。它说明 PSS discovery 只能作为 coarse feedback，不能替代根因
检出与正确 control 误报。单一公开 candidate 也不能支持 fixed/random 的一般优劣结论。

## M5.17b Trace Mutation 结果

公共 operator 按 source Trace 顺序选择第一对相邻 message deliveries（decisions 46/47），精确重放前缀、
交换 pair，再使用 digest-bound priority suffix。official control 完成 96 decisions、workload 和 strict
replay；mutation 与 source 都发现 56 个 Core PSS states、prefix area 3324，但 trace digest 不同。

成功 mutation 自身为 98 primary / 98 replay；加上生成 source Trace 的 98/98，方法真实成本是 196/196。
另一个依赖性失败校准以稳定 reason code 停在 decision 34，记录 35 primary work、0 replay，不静默
fallback，也不算作 method trial。本阶段没有 candidate 或 defect verdict。

## 快速验证

```bash
make test
go vet ./...
go test -race ./...
```

重跑官方 bundle 和公开 calibration：

```bash
make experiment-etcdraft-v2-bundle
make experiment-etcdraft-v2-calibration
make evaluate-etcdraft-v2-calibration
make experiment-etcdraft-v2-action-class-random
make experiment-etcdraft-v2-action-class-calibration
make evaluate-etcdraft-v2-action-class-calibration
make experiment-etcdraft-v2-trace-mutation
```

`experiment-etcdraft-v2-calibration` 会从冻结的 source transform 构建本地二进制；二进制和完整
约 1.9 MB bundle 写入被 Git 忽略的 `artifacts/`。仓库只保存小型 build input/audit、benchmark
manifest 和 evaluator report，避免再次把重复 trace body 提交成超长 JSON。

唯一真实模型入口仍是：

```bash
make experiment-etcdraft-v2-deepseek-planner \
  DEEPSEEK_KEY_FILE=/path/to/key.txt
```

该目标产生一次真实 API 调用，不属于 `make test`。可信执行、Oracle 和 evaluator 不读取模型密钥。

## 当前目录

```text
adapters/                 目标专用薄接入与 Semantic/Decision Mapping
  etcdraftv2/             官方 etcd/raft strict Adapter
  hashicorpraftv2/        第二实现的部分资格 Adapter
  fixture/                协议无关契约 fixture
internal/
  control/                Action、Item、Manifest 与 opaque envelope
  controlruntime/         enabled、选择、状态机、trace 和 replay
  controlentropy/         分域随机与 replay tape
  conformance/            外部能力见证与 Qualification
  controlexperiment/      admission、workload、policy、ExecutionBundle
  psscore/                固定 Core PSS IR 与投影
  protocolstate/          状态发现与跨 run 聚合
  semantic/               协议无关 decision observation
  oracle/                 v2 trusted monitors
  defectbench/            最小 control/candidate evaluator
  sutbuild/               source-bound 构建与审计
cmd/                      资格、实验、构建和评测 composition roots
qualifications/           两个真实 Adapter 的机械资格组合
benchmarks/               冻结的公开工件；历史内容不等于当前 API
docs/                     当前设计与不可改写的阶段记录
```

`agents/`、`drivers/`、`families/`、旧 Campaign/Coverage/Engine/Host 等 v1 源码已经删除。
`make audit-no-v1` 阻止可编译源码重新依赖该锥体。

## 信任边界与限制

- Agent 可理解冻结协议知识并生成策略 proposal，但不能决定 enabled、执行、PSS 真值、Oracle 或得分；
- Adapter/DecisionProjector 属于目标接入信任根，必须通过独立 fixture、conformance 和 replay 检查；
- Core PSS 用于 coarse discovery/feedback，不是状态等价证明，也不是正确性分数；
- Coverage 是未来解释性内部指标，不能代替隐藏候选检出和正确 control 误报；
- HashiCorp 当前缺少 strict natural-time/entropy/replay 资格，不能用于严格方法比较；
- 当前只有公开 calibration，没有非公开 candidate/control holdout；
- 当前没有证明 etcd/raft、其他协议或 ConsensusAtlas 正确、完备或无缺陷。

下一阶段不是恢复旧 Coverage/Campaign，而是在同一 ExecutionBundle 边界上实现 M5.17c 最小
PSS-guided corpus，之后才根据强基线的实际缺口设计 Guarded TestIntent。

阅读入口： [当前阶段](docs/CURRENT_STAGE.md)、[M5.17b](docs/stage-m5.17b-trace-mutation.md)、
[M5.17a](docs/stage-m5.17a-action-class-random.md)、
[M5.16](docs/stage-m5.16-execution-bundle.md)、
[M5.16R](docs/stage-m5.16r-legacy-removal.md)、[架构](docs/architecture.md)、
[Control Runtime v2](docs/control-runtime-v2.md) 和 [总体规划](docs/ConsensusAtlas-总体规划.md)。
