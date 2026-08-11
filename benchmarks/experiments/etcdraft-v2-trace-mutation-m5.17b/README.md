# etcd/raft v2 trace mutation public calibration — M5.17b

本目录只提交小型 `summary.json`。完整 Trace、Evidence、report 和 ExecutionBundle 可由下列命令生成到
Git 忽略的 `artifacts/`：

```bash
make experiment-etcdraft-v2-trace-mutation
```

公共 operator 在 M5.15 source Trace 中按顺序选择第一对相邻 `deliver-message`，精确重放前缀、交换该
pair，再使用 digest-bound priority suffix。成功运行完成 96 decisions、1 个 write 和 strict replay。

账本同时记录一个公开的依赖性失败校准：交换 decisions 34/35 后，源 Trace 中后一个持久化 effect 尚未
enabled，因而得到稳定 reason code，而不是静默选择其他 Action。该校准不是方法 trial。

成本必须完整读取：source 与 mutation 各为 98 primary / 98 replay，baseline method 合计 196/196；
失败校准另计 35 primary。`cmd/control-experiment` 集成测试会实际重跑并逐字段核对 `summary.json`。

这是公开 operator calibration，不含 candidate、defect verdict、holdout 或 Agent 方法结论。详细边界见
[M5.17b 阶段总结](../../../docs/stage-m5.17b-trace-mutation.md)。
