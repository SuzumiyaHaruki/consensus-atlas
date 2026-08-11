# M5.6b Typed Partition 与 Gateway 拓扑绑定

日期：2026-08-07

## 结论

`Partition/Heal` 不再依赖 Runtime 私有 JSON 结构。公共 `control.PartitionParameters` 负责节点集合
规范化、稳定 partition ID、重叠检测和解码验证；黑盒 `GatewayBinding` 再把同一个 Action 确定性解析
为显式拓扑清单中的跨组 Gateway。

这补齐了 M5.6a 单固定 Gateway wrapper 无法解释 Action 参数的问题，但仍只是 binding 层，不是完整、
原子的生产黑盒 Adapter。

## 输入、处理与输出

```text
Partition Action {id, left, right}
                |
        canonical typed validation
                |
explicit nodes + directed Gateway links
                |
                v
ordered crossing-link resolution
```

公共参数构造保证：

- 节点去重并排序；
- left/right 方向规范化，因此交换两组不会改变 partition ID；
- 两组非空且互不重叠；
- 解码时重新计算 ID，拒绝伪造或非规范参数。

Gateway 拓扑构造拒绝：

- 空节点或空链路清单；
- 重复节点、重复 link ID、重复有向边；
- 同一个 Gateway 实例被多次绑定；
- self-link、未知节点或 nil Gateway。

## 多节点 cut 实测

显式拓扑包含 `n1->n2`、`n3->n1`、`n2->n3`。对
`Partition([n1,n2], [n3])` 的解析结果稳定为：

```text
n2->n3
n3->n1
```

组内 `n1->n2` 不会被误切；同参数的 Heal 解析到完全相同且按 link ID 排序的集合。未知 partition
节点、没有任何已绑定 crossing link、非 Partition/Heal Action 均被拒绝。M5.6a 的同 Action
etcd/raft/Gateway 测试已改为消费该 binding，并继续 20 次通过。

## 可信边界

- 完整性只相对于 Adapter/用户显式提供的 topology inventory；系统无法发现被输入方遗漏的真实链路；
- binding 只解析，不管理多个并发 partition 的引用计数；
- test wrapper 顺序调用多个 Gateway，尚无失败回滚，因此不宣称原子多链路 actuation；
- binding 假设 Action 来自可信 Runtime，不独立重算外层 Action ID；
- 没有生产黑盒 Manifest、normalized actuation evidence 或 Qualification；
- connection-level control 仍不等于单条协议消息控制或 strict replay。

## 代码账本

- 公共 typed parameters：85 行生产 Go；
- Gateway topology binding：105 行生产 Go；
- 删除 Runtime 私有参数与重复规范化逻辑后，生产 Go **净增 145 行**；
- typed/binding/统一 Action 测试新增或调整：173 行；
- 新 Action、Profile、Schema、CLI、backend selector：0；
- 净增低于本阶段 150 行停止线。

## 下一决策

先停在此处审查，不直接增加 Crash/Restart/Invoke。若继续 M5.6c，只允许解决多 Gateway actuation 的
失败原子性、重叠 partition 引用计数和选择前 eligibility；若预计生产代码超过 200 行，则保留 binding
为薄 Adapter 工具并停止通用黑盒生产化。

## 复验

```bash
make audit-partition-binding
go test ./...
```
