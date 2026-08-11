# M5.4c HashiCorp Raft 生命周期与部分资格

日期：2026-08-07

## 结论

官方、未修改的 HashiCorp Raft v1.7.3 已形成最小 lifecycle 闭环：节点停止时保留官方
log/stable/snapshot store，重启时用同一 store 重建 `Raft`，同时创建新的 Transport 和
incarnation。已释放消息可跨源节点与目标节点重启保留，并投递给目标 incarnation 2。

公共 Runtime 没有增加实现名称分支，也没有增加 Action 或 Item。相同的协议无关
`released-message-lifecycle` Conformance 已在官方 etcd/raft v3.6.0 和 HashiCorp Raft v1.7.3 上通过。

## 输入、处理、输出

```text
released immutable message + running source/target
                         |
       crash source -> retain Runtime mailbox item
                         |
 restart source from retained official stores (incarnation 2)
                         |
       crash target -> delivery disabled, drop retained
                         |
 restart target from retained stores (incarnation 2)
                         v
 old message delivered to official restarted target
```

测试还读取官方 stable store 的 `CurrentTerm`，确认重建前后值不变；Adapter 保存的是 store 边界，
不是把协议状态复制到 Runtime。

## Portable CFT Profile v2

v1 的部分 capability 把基础消息所有权和 strict replay 绑定在同一个 case 上，使一个不控制墙钟的
实现无法获得已经实际证明的生命周期能力。v2 只拆分资格证据，不降低要求：

- `released-message-lifecycle` 只验证 Runtime ownership、crash/restart 和 incarnation；
- `opaque-invoke-accepted` 只验证不透明输入确实穿过 Adapter 边界；
- strict replay 继续要求原有 fresh replay witnesses；
- entropy、temporal 和 process isolation 仍分别判定。

同一个 v2 Profile 下，etcd/raft 机械结果仍为 8/8 required validated；HashiCorp 的结果为：

| 状态 | 数量 | 能力 |
|---|---:|---|
| validated | 3 | runtime-owned-message、crash-restart-incarnation、opaque-invoke-boundary |
| unsupported | 6 | strict yield、pure check witness、temporal、strict replay、entropy、process isolation |
| failed / undeclared / unvalidated | 0 | — |

因此 HashiCorp `qualified=false`。这是一份有用的部分资格，而不是通过缩小 Profile 得到的“合格”。
冻结工件为 [qualification report](../benchmarks/qualifications/hashicorp-raft-v2-m5.4c/report.json)，
digest 为 `cdcad3a20179b1ea8e6deaea3b9875626fe50de2d2d42557fea807bdde873555`。

## 代码增长门

| HashiCorp 范围 | M5.4b.1 | M5.4c | 变化 |
|---|---:|---:|---:|
| Adapter | 527 | 606 | +79 |
| probe | 176 | 176 | 0 |
| qualification composition | 0 | 70 | +70 |
| probe CLI | 27 | 27 | 0 |
| qualification CLI | 0 | 44 | +44 |
| **合计** | **730** | **923** | **+193** |

923 低于 M5.4c 1,200 行目标和 1,500 行强制停止线。通用 qualification bundle 从 etcd/raft
composition root 移到 `internal/conformance` 后由两个实现复用，没有复制第二套报告模型。

## 已证明与未证明

已证明：保留官方内存 store 的停止/重建、incarnation 更新、Runtime mailbox 跨两端重启所有权、
恢复目标上的真实 RPC 消费，以及同一 Conformance intent 的双实现执行。

未证明：操作系统进程级突然终止、磁盘文件损坏、snapshot restore、membership change、虚拟墙钟、
SUT entropy 控制或 strict replay。当前 `power-loss` 是库级节点停止并丢弃易失 Raft/Transport 对象，
不是进程隔离证明。

## 剩余 HashiCorp 阶段

预计还需 2 个阶段，最多 3 个：

1. M5.4d 确定性边界决策：机械确认官方 clock/random 能否在不修改源码的条件下注入；不可行就冻结
   Unsupported，不搭建伪虚拟时间。若选择进程 worker，另拆一个受预算约束的小阶段。
2. M5.4e 第二实现收口：冻结双实现能力矩阵、复核代码增长和删除项，决定哪些 v2 能力可供后续
   PSS/Coverage/Agent 使用，然后结束 Adapter 扩张。

若 M5.4d 坚持修改官方源码或引入第二套 Runtime 才能 strict replay，应停止该路径，而不是增加阶段。

## 验证

- HashiCorp lifecycle 和原 M5.4a/M5.4b 回归连续通过；
- 同一 lifecycle Conformance 在两个官方实现上连续 3 次通过；
- etcd/raft 在 Portable CFT Profile v2 上连续 3 次保持 8/8；
- HashiCorp qualification 两次 fresh digest 一致，冻结工件与 fresh run 相同；
- 全仓 test、vet、race、Python、JSON、digest、耦合和 diff 检查见最终交付记录。
