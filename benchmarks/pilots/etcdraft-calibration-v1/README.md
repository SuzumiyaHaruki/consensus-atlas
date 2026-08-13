# etcd/raft public calibration pilot v1

这是公开的管线校准样本，不是正式保留评测样本，也不产生 Agent 方法效果结论。
两份 SUT 都基于本地已有的官方 `go.etcd.io/raft/v3 v3.6.0`；Driver、Runtime、Profile、
Test Plan、Replay、Oracle 和 evaluator 完全相同。control 只添加不改变行为的源码注释；
校准版本只在 raft 模块副本内让节点 3 对普通日志命令形成不同数据。

架构收敛阶段已从 HEAD 删除两份重复完整 Campaign Trace；compact summary、资格、构建审计、manifest 和
evaluator report 仍保留。完整 Trace 可从 Git commit `0106e2c` 恢复。

## 实际结果

| opaque trial | 外部结果 | decisions | primary work | replay work | Coverage |
|---|---|---:|---:|---:|---:|
| `trial-41c7a1` | `control-pass` | 41 | 127 | 127 | 21/55，38.67 |
| `trial-a93d5e` | `killed`（Agreement，step 118） | 41 | 127 | 127 | 21/55，38.67 |

evaluator 从保存的 setup + measurement trace 重新执行 Agreement，而不是信任 Campaign
内保存的 Oracle 字段。结果为 1 个 calibration root cause、1 killed、0 false positive、
0 invalid。Coverage 完全相同，只作为并列解释字段，未进入 kill 判定。

## Qualification

`qualification-report.json` 由 Candidate typed requirements 与可信
`CapabilitySnapshot` 做六类纯集合包含判断。四个官方候选全部为 `deferred`：

- `63903dd`：缺少 ReadIndex controllable input、read-state observation、对应 Driver
  capability 与 linearizable-read monitor；
- `76f1249`：缺少 log compaction input/capability、slow-follower inflight bound，以及
  independent process exit classification；
- `8ecce32`：缺少 committed-entry pagination input/capability/page observation、分页边界
  和 committed-entry-continuity monitor；
- `0675f3d`：缺少可控的 unchanged-HardState Ready、MustSync observation、conditional
  sync capability 和 MustSync monitor。当前 Driver 对每个 Ready 固定 sync，会遮蔽差异。

source commit/reference 只是离线审计元数据，不参与 qualification，也未触发网络访问。

## Build audit

两次构建均验证官方模块身份、原始 digest、唯一文本匹配和转换后 digest；依赖通过临时
本地 module replacement 只读使用，`GOPROXY=off`、`GOSUMDB=off`、
`GOTOOLCHAIN=local`、`-mod=readonly`。module cache 在构建后仍保持原 digest。
build identity 由 module path/version、转换后 source digest 和目标 package 派生，并由
`ldflags` 写入 Campaign Driver Manifest。审计还保存 binary、command 与 toolchain identity。

## 阅读顺序

1. `qualification-report.json` 与 `capability-snapshot.json`；
2. `build-input/` 与 `build-audit/`；
3. `opaque-trial-view.json`；
4. `campaign/trial-41c7a1.json` 与 `campaign/trial-a93d5e.json`；
5. `evaluator/manifest.json`、`evaluator/submission.json`、`evaluator/report.json`；
6. `summary.json`。

本样本只证明受控构建、真实执行、严格 Replay、可信 Oracle 重算和评测账本已连通。
它没有证明任何官方候选已可评测，没有证明 Agent 优于 Random/DFS，也没有证明
Coverage/PSS 能预测缺陷检出。
