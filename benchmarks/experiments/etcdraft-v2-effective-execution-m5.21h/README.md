# M5.21h Missing Effective Execution

本目录保存 M5.21g gate 判定为唯一缺失的 attempt-2 adaptive uniform 执行。输入固定为 official
etcd/raft v2、PlannerView `7a130d51...76a5f`、seed 172、32 decisions、34/34 primary/replay work
ceiling、single-write workload 和无 partition 的 B4 FaultEnvelope。执行没有调用模型。

## 结果

| 指标 | 归档 ActionClass | 新 Adaptive/Uniform |
|---|---:|---:|
| primary / replay work | 33 / 33 | 33 / 33 |
| Replay stable | true | true |
| Agreement / Trace Integrity findings | 0 / 0 | 0 / 0 |
| 单次 PSS states | 17 | 23 |
| prefix area | 356 | 407 |
| workload completed / pending | 0 / 1 | 0 / 1 |
| crash / drop / duplicate | 1 / 0 / 0 | 1 / 1 / 1 |

两个状态集合交集为 8、并集为 32，ActionClass-only 9、Uniform-only 15，Jaccard 为 0.25。它证明
两种 Policy 在相同执行边界下形成不同轨迹；23 大于 17 只是单 seed、单 attempt 的诊断事实，不能证明
adaptive 或 uniform 的方法优势。两边 workload 均 pending，可信 monitor 均无 finding。

`uniform-report.json.gz` 与 `uniform-bundle.json.gz` 是新执行的完整原始证据；`comparison.json` 由
回归测试从新工件和 M5.21f 归档 ActionClass artifact 重新计算。压缩只用于降低仓库体积，不改变
内部 Report/Bundle digest。

- compressed report SHA-256：`23193c22b55c4d33b38a8e6e7f4f0179ad783a54cd4dc27bf853da6261182f3a`；
- compressed bundle SHA-256：`f1b69db930498dfd11659467ec8ccb8736a9f24502b26fa75f3a2bbb2be46f20`。

下一阶段在不调用模型、不新增 SUT execution 的前提下，将共享的 attempt-1 ActionClass 与两种
attempt-2 evidence 分别聚合为两个等预算的两步 method corpus，比较完整 state union/curve、workload
和 monitor 结果。该聚合仍是公开诊断，不是 holdout 或缺陷效果评价。
