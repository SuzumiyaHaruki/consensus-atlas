# M5.6a 统一 Partition Action 决策实验

日期：2026-08-07

## 结论

同一个 `control.Action` 可以同时驱动 Runtime-owned mailbox partition 和 Adapter 背后的外部
connection gateway，不需要新增 backend selector 或第二套 Runtime。统一 Action 路径在
`Partition/Heal` 这个最小切片上可行。

本阶段只证明 Action 身份和分发接缝可复用，不证明任意黑盒目标已经完成生产接入，也不提高 M5.5c
的能力等级。

## 最小改动

现有 `control.Adapter` 增加一个方法：

```text
ApplyRuntimeAction(context.Context, control.Action) error
```

Runtime 仍然独占 partition/message mailbox 的语义状态。在执行 Runtime-owned Action 前，Adapter
可以把同一个 Action 映射到外部机制：

```text
one Partition Action
        |
        +-- Adapter actuation -> Gateway.Partition
        |
        `-- Runtime commit    -> mailbox partition state
```

etcd/raft 和 fixture Adapter 对 Runtime 已经控制的内存网络执行 no-op actuation；HashiCorp Adapter
没有声明 Partition/Heal，稳定拒绝意外调用。没有修改 Action、ProducedItem、Manifest、Trace 或 schema。

## 决策测试

测试分别建立：

1. 官方 etcd/raft 三节点 Adapter + Control Runtime；
2. protocol-free fixture Adapter + Gateway actuation wrapper + Control Runtime。

两边都调用 `OfferPartition([n1], [n2])`，测试逐字段比较生成的 `Partition` Action；选择后再逐字段比较
Runtime 生成的 `Heal` Action。两组 Action 的 ID、Kind、Node、Item 和 Parameters 完全一致。

Gateway 路径的一次选择同时满足：

- Runtime snapshot 出现/删除同一个 partition；
- Gateway snapshot 进入/退出 partitioned；
- actuation log 只包含顺序确定的 Partition、Heal。

该测试连续 20 次通过，原 etcd/raft message partition/replay 测试也保持通过。负例确认外部 actuation
失败时，Runtime 不会提交 partition 状态。

## 尚未证明

- Gateway wrapper 目前是 test-only，不是可直接接入任意进程的完整 Adapter；
- 测试只绑定一个 `n1 -> n2` Gateway，尚未机械解释任意左右节点集合；
- partition 参数类型仍位于 Runtime 内部，生产 Adapter 不能复用 typed decoder；
- 外部 Gateway snapshot 尚未进入规范化 Action evidence；
- Gateway 仍依赖 wall-clock/goroutine，不具备 strict replay；
- 因为尚无生产 Manifest + Qualification，M5.5c 的 partition 等级仍是 interceptable。

## 代码账本

- 通用生产代码净增：9 行；
- 三个现有 Adapter 的 actuation 实现：22 行；
- 决策测试：144 行；
- 生产 Go 合计净增：31 行，低于 200 行停止线；
- 新 Action、Profile、Schema、CLI、backend selector：0。

## 下一决策

如果继续，M5.6b 只把现有 private partition parameters 提升为公共 typed Action 参数，并以
`partition ID + left/right node set` 机械绑定一组 Gateway。生产代码上限 150 行；在完成任意拓扑
负例前，不增加 Crash/Restart/Invoke 映射，也不迁移 PSS/Agent。

## 复验

```bash
make audit-unified-partition
go test ./...
```
