# M5.1 阶段总结：Control Runtime v2 协议无关基础

日期：2026-08-07

## 阶段结论

M5.1 已实现一个不含 Raft 类型的 `v2alpha1` 控制层闭环：输入冻结 seed、Adapter Manifest、
协议不透明的 payload 和已枚举 `ActionID`；可信 Runtime 机械判断 enabled、执行消息/时间/
生命周期/宿主副作用、校验稳定 yield 与只追加 entropy tape；输出完整状态摘要、逐步 trace、
严格 replay 结果和外部 conformance report。

这证明公共控制模型可以脱离现有 v1 `Ready/OutputBatch` 路径独立运行，但尚未证明它已经适配
真实共识，更未证明跨协议普适性。v1 生产路径、功能测试和历史实验工件保持原样；仅扩展了
architecture coupling test 以守卫新包依赖方向。

## 输入、处理与输出

```text
Campaign seed + Adapter Manifest + opaque fixture input
                         |
                         v
      Adapter: Reset -> stable Yield -> frozen Emission
                         |
                         v
 Runtime: enumerate enabled ActionID -> select -> command/yield/collect
          message ownership | earliest temporal event | crash incarnation
          durable effect    | append-only entropy tape | exact state digest
                         |
                         v
 Snapshot + v2 Trace + Replay verdict + Conformance Report
```

Agent 或搜索器在这个边界上只能提交 Runtime 当前给出的 `ActionID`。它不能提交自由时间值、修改
随机结果、直接写状态、声明自己的 evidence，也不能自行决定动作是否 enabled。

## 已实现

- `internal/control`：版本化 ID、Action、AdapterCommand、ProducedItem、Manifest、payload/emission
  digest 和机械校验；
- `internal/controlruntime`：动态 enabled 集、Runtime-owned message、partition/heal/drop/duplicate、
  最早 temporal event、crash/restart incarnation、effect/callback、完整状态摘要、v2 trace 和 replay；
- `internal/controlentropy`：`hmac-sha256-counter-v1` 分域流、逐 draw 身份、node/incarnation/domain
  隔离、只追加 tape 和逐请求 replay validator；
- `internal/conformance`：位于 Adapter 外部的 13 项能力检查及稳定 reason code；
- `adapters/fixture`：不包含任何具体协议类型的确定性 Adapter，覆盖 message、periodic pulse、
  one-shot timer、sleep wakeup、durable effect、callback、crash/restart 和 entropy；
- 架构测试机械禁止 v2 新包 import v1 `core/driver/host/engine`、`families/*` 或 etcd/raft。

Runtime 对 Adapter 自报信息不直接采信：Emission、Evidence 和 tape payload 必须通过 digest 校验；
tape 的 algorithm、seed、draw count、逐域 ordinal、draw ID 和历史前缀必须一致；`Check` 调用前后
可观察 evidence/entropy 必须不变。状态摘要包含完整 ProducedItem、offered action、clone counter、
partition、node/incarnation、逻辑时间、Adapter state 和 entropy identity。

## 已通过的正反例

- 同 deadline 的 periodic pulse 都可选，更晚 timer 不可提前选择；
- sleep wakeup 不能越过更早 pulse，一次性 timer 触发后不再启用；
- released message 跨源/目标 crash、restart 和 partition 保留，heal 不隐式投递；
- duplicate 保留原消息并产生稳定 clone lineage；
- 未完成 durable effect 阻塞依赖消息，完成后释放，crash 前未完成则取消；
- restart 产生新 incarnation，旧 timer 保持取消，旧 incarnation 新输出被拒绝；
- 相同 seed 的不同节点随机流不受跨节点调用顺序影响；改变 bound、遗漏 draw、伪造摘要、
  重写历史前缀或制造 ordinal gap 都被拒绝；
- 重复 `Collect` 幂等，`Check` 改写可观察状态、循环依赖、非 enabled Action 和非零 `E` 都被拒绝；
- fresh Runtime 的 decision log 可由新 Adapter 实例严格 replay，状态和 trace digest 一致。

## 本阶段没有证明

- fixture 不是 CFT/BFT 协议，尚未验证 etcd/raft 的 `Ready`、真实 `Tick()`、原生随机入口或持久化
  顺序能正确映射到 v2；
- 尚无第二个控制表面不同的真实 CFT Adapter，因此不能宣称控制层具有跨协议普适性；
- conformance report 目前是 Go 库返回值，尚未形成 M5.3 的版本化 CLI 工件和资格报告；
- PSS、Coverage、Oracle、搜索算法、Agent Planner 和 Defect Benchmark 仍在 v1 路径，尚未迁移；
- 未进行 LLM 调用，也没有产生 Agent 优于 Random/DFS/专家计划的实验结论；
- 当前实现是内存内 `v2alpha1`，只支持 `power-loss` 和 `E=0`，不覆盖时钟漂移、GST、动态成员
  生命周期或开放式拜占庭行为。

## 下一阶段最小任务

进入 M5.2，但只做 etcd/raft Legacy Ready Bridge 的第一条纵向切片：在 Adapter 内把一个节点的
官方 `RawNode` 初始 `Ready` 映射为 Message/Effect/Observation，完成 effect 后调用原生
`Advance`，并由 `PeriodicPulse` 单次调用 `Tick()`。Raft 类型不得越过 Adapter/Evidence 边界，
Runtime 不增加任何 Raft 条件分支。

第一条切片先验证单节点 bootstrap/persist/advance 的 fresh-run/replay；通过后再加入多节点消息、
crash/restart 和官方 v3.6.0 entropy 隔离。若 bridge 暴露公共抽象缺口，必须用 fixture 回归证明
修改没有破坏已有 13 项 conformance。

## 验证结果

- 新增/修改 Go 文件已执行 `gofmt`；
- `go test ./...` 通过；
- `go vet ./...` 通过；
- `go test -race ./...` 通过；
- `python3 -m unittest discover -s agents -p 'test_*.py'`：7 项通过；
- `git diff --check` 通过；
- 桌面与仓库内两份 `ConsensusAtlas-总体规划.md` 字节一致；
- 本阶段没有修改 JSON 工件，因此无需运行 JSON schema 实例校验。

## 阅读顺序

1. [Control Runtime v2 设计](control-runtime-v2.md)
2. `internal/control/model.go`
3. `internal/controlruntime/actions.go`
4. `internal/controlruntime/runtime.go`
5. `internal/controlruntime/state.go` 与 `trace.go`/`replay.go`
6. `internal/controlentropy/entropy.go`
7. `adapters/fixture/adapter.go`
8. `internal/conformance/suite.go`
9. 各包对应的 `_test.go`
