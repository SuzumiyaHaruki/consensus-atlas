# etcd/raft public calibration pilot v2

这是 M4.8.1 的公开可信链校准，不是正式 holdout，也不产生 Agent 方法效果结论。
它复用 v1 的官方 etcd/raft v3.6.0 control 与命令数据分歧 calibration、相同 Profile、
Test Plan、预算和 Agreement monitor，但要求 evaluator 亲自重跑已验证 binary。

## 实际结果

| opaque trial | evaluator 结果 | decisions | primary / replay work | Coverage |
|---|---|---:|---:|---:|
| `trial-41c7a1` | `control-pass` | 41 | 127 / 127 | 21/55，38.67 |
| `trial-a93d5e` | `killed`（Agreement，step 118） | 41 | 127 / 127 | 21/55，38.67 |

结果仍为 1 个公开 calibration root cause killed、0 false positive、0 invalid。
Finding 的 trace digest 与 Campaign Coverage Ledger 对同一持久化轨迹的 digest 都是
`5a0c0c922f9c60cf1cd9c5039cf1a04e5df319530ae51e87162a0c6efb6dfb46`。
Coverage 相同且未参与 kill 判定。

## v2 增加的可信边界

- `TraceRecord` 的开放快照使用保留精确数字的 JSON 解码；内存与持久化轨迹共享
  canonical digest。
- evaluator 从 Campaign report 重建每个 Ledger run，校验 trace、replay、conformance、
  evidence reference、最终 Coverage 和完整执行成本的一致性。
- CapabilitySnapshot 强制 protocol、Family、Profile identity 和编译边界一致；Candidate
  与 Snapshot 的集合 digest 不受数组顺序影响。
- BuildAudit 记录目标源码和整个模块源码树的输入/输出 digest；稳定相对 staging path
  避免临时绝对路径进入 Go build info。control 连续两次构建得到相同 binary/audit digest。
- submission v2 同时提交 report、BuildAudit、binary、Profile 和 Test Plan。evaluator 先验证
  audit 与 binary，再清空继承环境并亲自执行 binary；重跑 report 必须与提交 report 一致。

`negative-swapped-binary-submission.json` 会在执行前因 binary 与 BuildAudit 不符而被拒绝。
单测还覆盖 protocol/Family、Profile bounds、Ledger witness、coverage score、非目标模块文件、
缺失/替换 build evidence 等负例；实际篡改 report 的 smoke test 会被 trusted rerun 拒绝。

## Qualification

四个官方候选仍全部 `deferred`。`63903dd` 的 requirements 已补入选主、提案、消息控制、
重复、分区/恢复、旧 leader 读取以及容量前置条件；当前机械缺口只剩 ReadIndex input、
read-state observation、两个对应 Driver capability 和 independent linearizable-read monitor。

## 阅读顺序

1. `summary.json`；
2. `qualification-report.json` 与 `capability-snapshot.json`；
3. `build-input/` 与 `build-audit/`；
4. `opaque-trial-view.json`；
5. `campaign/`；
6. `evaluator/manifest.json`、`submission.json` 与 `report.json`；
7. `evaluator/negative-swapped-binary-submission.json`。

本 pilot 仍未证明官方历史候选可评测、Agent 优于 Random/DFS、Coverage/PSS 能预测缺陷，
也未证明 etcd/raft 或 ConsensusAtlas 正确。Evaluator 当前只执行经过私有 digest 固定、
来源受控的 binary；把任意不可信 SUT 放入同一执行路径前仍需操作系统级隔离。
