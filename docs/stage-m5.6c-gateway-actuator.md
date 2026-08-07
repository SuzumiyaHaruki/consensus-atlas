# M5.6c Gateway Actuator 重叠与失败边界

日期：2026-08-07

## 结论

M5.6c 完成了 `Partition/Heal` 黑盒纵向切片的预定范围：Runtime 在枚举 enabled Action 时先询问
Adapter 的 Runtime-owned Action 资格；`GatewayActuator` 再把 typed Action 解析为多个 Gateway，管理
重叠 partition 引用，并在一组 gate 调用失败时回滚已经改变的 gate 状态。

该结果证明连接级控制可以作为薄 Adapter 的可复用执行部件，但不把它提升为严格
`scheduler-owned message`：多个真实 Gateway 仍按顺序切换，外部进程可能看到短暂的部分隔离；关闭的
既有 TCP/Unix connection 也不能因控制状态回滚而复原。

## 输入、处理与输出

```text
typed Partition/Heal Action
          |
          v
Adapter.CheckRuntimeAction ── ineligible reason code
          |
          v
GatewayBinding.Resolve(crossing links)
          |
          v
GatewayActuator(refcount + all-or-rollback gate state)
          |
          +── Runtime partition state
          +── external connection gates
```

没有新增 Action、Profile、Schema 或 backend selector。

## 机械行为

- Runtime-owned Action 与 Adapter-directed Action 共用同一个 enabled 集合入口；
- 未启动、已关闭、外部状态漂移、重复 partition 和不存在的 heal 在选择前被过滤；
- 同一 Gateway 被两个不同 partition 引用时只执行一次物理 `Partition()`；
- 解除第一个引用不会提前 `Heal()`，最后一个引用解除时才恢复 gate；
- 多 Gateway partition 中途失败会逆序 heal 已改变 gate；
- 多 Gateway heal 中途失败会逆序重新 partition 已改变 gate；
- 只有整组调用成功后，actuator 才提交 active/refcount 账本；
- 回滚自身失败会产生稳定 `BLACKBOX_GATEWAY_ROLLBACK_FAILED`，后续状态漂移由选择期检查阻断。

## 实测

单元负例使用可控 Gateway 验证 partition 和 heal 两个方向的中途失败及回滚。真实联动测试启动三个
独立子进程和两条 Unix Gateway：

1. 开放状态下 `n1 -> n3`、`n2 -> n3` 均返回 `ACK`；
2. `Partition([n1], [n3])` 只阻断第一条链路；
3. `Partition([n1,n2], [n3])` 与前者重叠并阻断两条链路；
4. Heal 第一个 partition 后，共享的 `n1 -> n3` 仍保持隔离；
5. Heal 第二个 partition 后，两条链路都恢复。

阶段审计连续运行 20 次通过。该 fixture 的进程流量用于验证 connection actuation，不是第三个共识
Adapter，也不构成 etcd/raft 网络栈的黑盒测试。

## 可信边界

这里的“all-or-rollback”仅指 actuator 的 gate 状态与内部引用账本，不是网络世界的原子事务：

- 多个 Gateway 的调用是顺序执行，进程可能观察到瞬时 partial cut；
- partition 会关闭已存在连接，rollback 只能重新开放新连接，不能恢复旧连接及其在途字节；
- Gateway 使用 goroutine、OS socket 与 wall clock deadline，不提供严格 replay；
- topology 完整性仍只相对于显式 inventory；
- composition wrapper 仍在测试中，尚无生产 blackbox Adapter Manifest、evidence 或 Qualification；
- 因此本阶段不改变 `interceptable` 能力等级，也不给 `runtime-owned-message` 资格记分。

## 代码账本

- `Adapter.CheckRuntimeAction` 与 Runtime enabled 过滤；
- `GatewaySnapshot` 生命周期状态和 `GatewayControl` 最小接口；
- `GatewayActuator` 的引用计数、失败回滚和稳定 reason code；
- fixture、etcd/raft、HashiCorp Raft 只增加默认资格实现；
- 生产 Go 相对 M5.6b **净增 165 行**，低于本阶段 200 行停止线；
- 新增 Action/Profile/Schema/CLI/backend selector：0。

## 下一决策

停止继续扩张通用黑盒网络代码。下一阶段先冻结能力词义：connection-level Gateway 可记为
`scheduler-actuated/interceptable`，但只有拥有冻结 message ID、纯 enabled 集和严格 replay 的路径才能记为
`scheduler-owned message`。若研究目标要求原子 topology cut，应另行评估进程 barrier/暂停或网络 namespace，
不能用当前回滚语义替代。

## 复验

```bash
make audit-gateway-actuator
go test ./...
go vet ./...
go test -race ./...
```
