# M5.6d Control Path 能力矩阵

该报告把 M5.5a 已机械资格的两个 Adapter 消息路径，与 M5.6c Gateway Action 路径放入同一个分级视图。
grade 只能由 witness facts 推导，不能由输入直接填写；确定性保证与 control grade 分开记录。

冻结结果：

- etcd/raft：message 粒度、`scheduler-owned`、stable item ID、strict replay；
- HashiCorp Raft：message 粒度、`scheduler-owned`、stable item ID、无 strict replay；
- Gateway：connection 粒度、`scheduler-actuated`、controller state 可回滚、无 stable item ID、
  无 atomic external effect、无 strict replay。

Gateway 结果只绑定真实 Runtime Action、选择期检查、重叠引用和失败回滚 witness；它不是完整黑盒
Adapter Qualification，不能用于取得 `runtime-owned-message` credit。

```bash
make audit-control-paths
```
