# M5.5a 场景表面与控制保证分层报告

`report.json` 从两个 fresh `QualificationReport` 和五项带稳定 evidence code 的 Raft 场景表面声明
机械生成。声明只能说明语义存在，并且最多声明 `interceptable`；它不能自行取得
`scheduler-owned` 或 validated credit。

当前报告应读作：

- etcd/raft：5/5 表面存在，5 项声明 scheduler-owned，4 项有独立资格证据；4/4 required
  deterministic guarantee validated；
- HashiCorp Raft：5/5 表面存在，3 项 scheduler-owned 且 validated；时间和 durability 仍为
  opaque；0/4 required deterministic guarantee validated。

因此原来的 HashiCorp `3/8` 不再作为功能比例展示。报告 digest 为
`ac8793cf8e635108530a9ddef8564675f9ddfdce9cff22a8814af828e6dda618`。

```bash
make audit-control-surfaces
```
