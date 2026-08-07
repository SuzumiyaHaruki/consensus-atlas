# M5.2.1 阶段总结：官方 etcd/raft 单节点 v2 纵向切片

日期：2026-08-07

## 阶段结论

ConsensusAtlas 已把未修改的官方 `go.etcd.io/raft/v3 v3.6.0` 接入 Control Runtime v2 的第一条
纵向切片。输入是冻结 seed 和 Runtime 当前枚举的 `CompleteEffect`/`FireTemporalEvent`；Adapter
把初始 `RawNode.Ready` 冻结为 durable effect，完成后写入 `MemoryStorage`、应用 committed
configuration entry 并调用原生 `Advance`；没有 Ready 时才暴露下一次 `PeriodicPulse`，选择它只
调用一次原生 `Tick()`。

连续选择 pulse 会通过 etcd/raft 自己的 randomized election timeout 自然选出单节点 leader，
不是由测试框架强制触发 timeout。完整 decision log 已用全新 RawNode 严格 replay。

## 输入、处理与输出

```text
seed + official v3.6.0 module + enabled ActionID
                     |
                     v
scoped crypto/rand domain -> NewRawNode -> Bootstrap
                     |
                     v
Ready captured -> durable Effect -> persist/apply/Advance
                     |
              no Ready: PeriodicPulse
                     |
                     v
single Tick -> natural Raft transition -> next Ready or pulse
                     |
                     v
v2 trace + BasicStatus/storage evidence + entropy tape + replay verdict
```

## 已实现

- 新增 `adapters/etcdraftv2`，只依赖 v2 `control` 契约和官方 raft 模块，不依赖 v1
  `core/driver/host/engine` 或 Raft Family；
- Manifest 明确只声明一个节点、`raft-ready` durable effect、periodic pulse 和对应的两个 Action；
- `Ready` 在 yield 处深复制并生成稳定 digest，重复 `Collect` 返回冻结副本；
- bootstrap Ready 未完成时不暴露 pulse，避免在 outstanding Ready 上继续 Tick；
- effect 完成顺序固定为 snapshot/HardState/entries 持久化、committed conf change apply、原生
  `RawNode.Advance`；
- idle 状态每次只产生一个 deadline 递增的 pulse，选择 pulse 每次只执行一个 `RawNode.Tick()`；
- evidence 包含 role、term、vote、commit、lead、applied、storage HardState/last index、ConfState、
  Ready/tick/advance 计数和 outstanding Ready identity；
- 架构测试允许具体 Adapter import 官方 raft，同时禁止其反向依赖 v1 Runtime。

## 官方随机入口的处理

官方 v3.6.0 在 `NewRawNode` 和状态 reset 中通过进程级 `crypto/rand.Reader` 选择 election timeout。
Adapter 在每次可能进入官方随机路径的原生调用周围：

1. 获取进程内 Adapter scope 互斥锁；
2. 临时安装 node/incarnation/domain 对应的 deterministic reader；
3. 把每次原生读取写入 `hmac-sha256-counter-v1` tape；
4. 调用结束后无条件恢复原 Reader；
5. Runtime 独立检查 tape digest、draw identity、ordinal 和只追加前缀。

测试证明相同 seed 的作用域读取和最终 tape 一致，且构建、Tick、Ready completion 与 replay 后
Reader 都恢复为原对象。

这仍不是正式的进程隔离证明：无关 goroutine 不遵守 Adapter 互斥锁时，仍可能在短暂安装期间读取
同一个全局 Reader。正式资格必须把 Adapter 放进无其他 crypto consumer 的隔离进程，并加入域外/
并发读取负向 conformance；在此之前结果只属于开发阶段集成证据。

## 集成测试结果

- 初始 Runtime 恰有一个 enabled Ready effect，没有可提前选择的 pulse；
- bootstrap/election 至少产生两个被持久化并 Advance 的 Ready；
- storage 包含 bootstrap 和 leader entry，ConfState 只有 voter 1；
- 多次自然 pulse 后角色变为 `StateLeader`；
- trace 有完整 effect/pulse 决策、evidence 和 entropy tape；
- 全新 Adapter/RawNode 使用相同 seed 和 decision log 得到相同 trace digest；
- Manifest 不声明 message、crash 或 restart，避免用缺失能力冒充完整接入；
- 重复 Collect、调用方修改返回副本和重复 enabled 检查不会改变 Adapter 状态。

## 本阶段没有证明

- 这不是完整 etcd/raft Adapter：仅支持单节点，遇到 `Ready.Messages` 会机械拒绝；
- 尚无 message freeze/release/deliver/drop/duplicate/partition 的官方 RawNode 集成；
- 尚无 crash/restart、durable image 重建、snapshot、ReadIndex、proposal 或 membership change 场景；
- 一个 `raft-ready` effect 当前把 storage persistence、application conf apply 和 `Advance` 作为同一
  原子宿主完成边界，尚未验证更细的 visible-write/sync cutpoint；
- evidence 使用官方公开 `BasicStatus` 与 storage 状态，不等价于序列化全部私有 RawNode 内存；
- 尚未通过完整 13 项通用 conformance，因此不能称为 qualified Adapter；
- 没有迁移 PSS、Coverage、Oracle、Agent 或 benchmark，也没有方法效果结论。

## 下一阶段最小任务

M5.2.2 将 Adapter 从单节点状态重构为节点表，并只增加多节点消息纵向切片：

1. 每个 node/incarnation 使用独立 entropy domain、RawNode、storage 和 outstanding Ready；
2. `Ready.Messages` 冻结为 `ProducedItem(Message)`，以 Ready durable effect 为依赖；
3. Runtime 完成 effect 后释放消息，deliver 时 Adapter 只调用目标 `RawNode.Step()`；
4. 用三节点 bootstrap/election 验证消息可被长期保留、drop/duplicate/partition 后严格 replay；
5. 保持 crash/restart 暂不声明，等消息所有权闭环通过后再加入 durable restart。

如果多节点接入需要修改公共 Runtime，必须先用现有 fixture 和本阶段单节点测试证明修改是协议
无关的，并重新运行全部 v2 replay/conformance。

## 验证结果

- 新增/修改 Go 文件已执行 `gofmt`；
- `go test ./...` 通过；
- `go vet ./...` 通过；
- `go test -race ./...` 通过，包括进程级 Reader 的作用域恢复测试；
- `python3 -m unittest discover -s agents -p 'test_*.py'`：7 项通过；
- `git diff --check` 通过；
- 桌面与仓库内两份总体规划字节一致；
- 未修改 JSON 工件，因此没有新增 JSON schema 实例校验。

## 阅读顺序

1. `adapters/etcdraftv2/adapter.go`
2. `adapters/etcdraftv2/model.go`
3. `adapters/etcdraftv2/storage.go`
4. `adapters/etcdraftv2/entropy.go`
5. `adapters/etcdraftv2/adapter_test.go`
6. `internal/controlruntime/runtime.go` 与 `actions.go`
7. [M5.1 阶段总结](stage-m5.1-control-runtime-v2-foundation.md)
