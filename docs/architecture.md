# Architecture

## 当前信任边界

```text
untrusted / bounded                         trusted Go core

Strategy proposal -----------------------> policy compiler
                                                |
Protocol docs ----> target Binding ------------+----> Qualification
                     + Semantic Mapping         |          |
                     + DecisionProjector        |          v
                                                +----> Experiment admission
                                                           |
opaque workload ------------------------------------------>|
                                                           v
                                                Control Runtime executor
                                                  |      |       |
                                                Trace  Snapshot  Evidence
                                                  +------+-------+
                                                         v
                                                  ExecutionBundle
                                                         |
                                         +---------------+--------------+
                                         v                              v
                                  TraceIntegrity                 DecisionProjector
                                                                        |
                                                                        v
                                                                    Agreement
                                         +---------------+--------------+
                                                         v
                                               evaluation ledger
```

Agent/模型可以产生受 schema 约束的策略 proposal，但不能：

- 构造 enabled set 或任意 ActionID；
- 直接调用 Adapter/SUT；
- 修改当次 Qualification、投影器、Oracle 或预算；
- 写入 PSS/Defect ledger 或决定 verdict。

目标专用信任根由薄 Adapter、Semantic Mapping 和 DecisionProjector 构成。它们可以理解协议 Evidence，
但通用 Runtime、Experiment、Core PSS ledger、Oracle 和 evaluator 不导入协议类型。

## 唯一执行路径

`internal/controlexperiment` 的所有 fixed/random/workload/Planner 执行最终进入同一个 `execute` 和
`executeRun`，每次 primary 后使用 fresh Adapter/Runtime 做严格 replay。`ExecuteQualifiedBundle` 不是
第二套执行器，只打开 capture，把同一次执行已经产生的数据组织成 bundle。

```text
Config + ExecutionAdmission + Qualification
                 |
                 v
        Adapter Manifest recheck
                 |
                 v
       prepare workload through Runtime Offer
                 |
                 v
    Runtime.EnabledActions -> policy -> Select
                 |
                 +--> Online Core PSS sample
                 +--> full trace/evidence
                 +--> client result
                 |
                 v
           fresh strict replay
```

Workload Provider 只读取 Semantic Mapping 的 coarse participant mode，并调用 Runtime 的公开 Offer；
实际 Action 始终由 Runtime 冻结并出现在唯一 enabled set 中。`PreparationRecord` 绑定 Offer 前后状态，
避免把 scheduler 之外的状态改变遗漏在证据链外。

## 协议无关公共层

- `internal/control`：Action、ProducedItem、Manifest、opaque payload/evidence envelope；
- `internal/controlruntime`：状态、enabled、选择、消息/时间/生命周期/effect、trace/replay；
- `internal/controlentropy`：node/incarnation/domain 隔离的随机流和 tape；
- `internal/conformance`：外部行为见证、Qualification 与 capability 状态；
- `internal/controlexperiment`：admission、policy、workload、fault budget、report、ExecutionBundle；
- `internal/psscore` 与 `internal/protocolstate`：固定 Core IR、采样和 discovery；
- `internal/semantic`：最小 decision observation 接口；
- `internal/oracle`：不理解具体协议的可信 monitor；
- `internal/defectbench`：control/candidate 预算与结果账本；
- `internal/sutbuild`：source-bound 构建变体和审计。

## 目标专用层

`adapters/etcdraftv2` 调用官方 `go.etcd.io/raft/v3` 的 RawNode API，处理 Ready、durable/apply effect、
Tick、Step、crash/restart 和 Evidence。它同时提供 Core PSS Mapper 与 DecisionProjector；协议类型不会
越过 Adapter 包。

`adapters/hashicorpraftv2` 使用相同公共 Action/Runtime，已验证消息、生命周期和 opaque invoke，但因
官方实现内部墙钟、包级随机和 goroutine 调度未受框架完整控制，只获得部分 Qualification。统一 Action
表示上层语义统一，不表示两个实现拥有相同的控制强度或 strict benchmark 资格。

新目标的边际产物仍限制为：Execution Binding、Semantic Mapping、DecisionProjector、fixture 与
qualification composition。若接入非 Raft 协议必须修改 Runtime/Action/Core PSS schema，应先视为抽象
失败，而不是增加协议 type switch。

## ExecutionBundle 与 Oracle

bundle 是 evaluator 的完整输入，包含完整 trace body，而普通 measurement report 只保存摘要。大型
bundle 默认写入 ignored `artifacts/`，checked-in evaluator report 只引用其 digest。

TraceIntegrity 检查证据链是否可信。失败的 trial 是 `invalid`，不能记为 candidate kill 或 control
false positive。Agreement 接收 target projector 输出的 exact position/value digest；PSS/Coverage 不
参与 verdict。

## 强基线选择边界

M5.17a 的 action-class random 不增加 Runtime backend。选择器读取唯一 `EnabledActions`，按 ActionKind
分组后均匀选择 class 和成员。冻结 FaultEnvelope 只生成该策略的 selectable 子集，不修改 Runtime
enabled、ActionID 或状态；`Invoke` hard priority、envelope、policy seed 和完整 Config 都进入 bundle
identity。

benchmark manifest 可按 variant 绑定 `ExpectedConfigDigest`。这使 control/candidate 除 build identity
外还必须使用清单指定的 workload、policy、fault envelope、runtime seed 和 admission；不匹配的 trial
只能记为 `invalid`。

M5.17b trace mutation 仍是同一个 policy boundary。通用 operator 只接收一条已验证 Trace 和 source
Policy，产生 digest-bound splice plan：精确前缀、相邻 swap、显式 priority suffix。Runtime 独立判断
每个引用 ID 是否 enabled；不可执行时返回带 partial work 的稳定失败，不允许 selector 自行换一个 ID。
具体 composition 可以冻结 pair 选择规则，但不能把多个尝试中“方便成功”的一个冒充单次试验。

mutation report 的 work 只覆盖 mutation execution。方法级比较必须另行加上 source corpus 的构造、
setup 和 replay；M5.17b 当前实测口径为 source 98/98 + mutation 98/98 = 196/196。公开失败校准的
35 primary work 独立记录，不混入方法 trial。

## M5.16R 之后的依赖规则

旧 v1 Engine/Host/Driver、Raft Family、Coverage/Campaign、onboarding、旧 Agent 和 migration 源码已
删除。默认 `make test` 先运行 `audit-no-v1`，阻止旧 import 回流。历史文档/JSON 可以提到旧包，但当前
Go source 不得依赖它们。

依赖方向必须保持：

```text
cmd/qualifications -> adapters + trusted internal packages
adapters           -> official implementation + internal/control*
oracle/evaluator   -> generic bundle + semantic observation
internal/control*  -X-> adapters or consensus packages
```

## 当前未完成

- 非公开 holdout 与正式方法比较；
- PSS-guided corpus 强基线；
- Guarded TestIntent Agent；
- 第二个 strict deterministic target；
- Coverage v2 的真实新消费者；
- BFT 的有限 Byzantine action 与对应 target projector/Oracle。

这些功能必须复用当前 ExecutionBundle/evaluator，不得恢复第二套 Runtime、Campaign 或判定链。
