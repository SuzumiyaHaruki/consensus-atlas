# M5.15 etcd/raft semantic workload calibration

`report.json` 是公开 development/calibration 工件，不是 holdout、Oracle pass 或协议正确性证明。

输入是一条 Adapter-specific opaque write，通用 Experiment 只知道其 `PayloadEnvelope`、请求 ID 和期望
完成状态。Workload Provider 从已有 Semantic Mapping 中等待唯一 `coordinating` participant，并通过
Runtime `OfferInvoke` 提交；它不读取或拼接 enabled set。Policy 随后仍从 Runtime 返回的候选中选择
该 Invoke。

冻结结果：

- 1 run × 96 scheduler decisions；
- 1 planned / offered / completed write，结果为 `committed`；
- primary/replay 各 1 setup、1 prepare action、96 decisions，共 98 logical work units；
- strict replay stable，trace digest
  `34d0d3895010aebf787c539937b771bd1f68bc1174a4c6e1740a5252290b225c`；
- 56 个 Core PSS states，prefix area 3324；这是发现统计，不是覆盖率；
- report digest `e680aabd1c98181e2c87fa470610ed300689ed30deff8b2d543c9211a18948d7`；
- 文件 SHA-256 `e7a00826bf4881288696ad5d4c6685d7b82bcf684d7276af1bbf5cc7cad247b1`。

FaultEnvelope 同时冻结 crash/drop/duplicate/partition 总量与 crash/partition 并发上限；本 calibration
策略没有选择故障，所以 usage 全为 0。独立集成测试证明超出 envelope 的 crash 会在 Runtime Select
前被拒绝且不计 scheduler decision。

该工件尚不包含完整 Trace/Evidence body、Oracle、Coverage 或 DefectBench 结论。M5.16 才会定义最小
ExecutionBundle 并迁入第一个可信 Oracle。
