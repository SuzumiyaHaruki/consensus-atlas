# etcd/raft ReadIndex historical regression pilot v1

这是一个公开历史回归的受控复现实验，不是 Agent 方法对比，也不是 etcd/raft 的全面验证。
candidate 是本地已有的官方 `go.etcd.io/raft/v3 v3.6.0` 未修改 module tree；control 只对
`raft.go` 和 `read_only.go` 应用公开 `63903dd` 的五个精确源码替换。两者使用同一 Driver、
Profile、Test Plan、trusted monitor 与完整执行预算。

## 结果

| opaque trial | status | finding | primary / replay work | Coverage |
|---|---|---|---:|---:|
| `trial-candidate-v3.6.0-v2` | `killed` | `linearizable-read`，step 201 | 218 / 218 | 26/55，45.67 |
| `trial-fixed-63903dd-v3` | `control-pass` | 无 | 218 / 218 | 26/55，45.67 |

Evaluator 从保存的 setup + measurement trace 重算 Oracle；它还验证 BuildAudit、binary digest、
Driver Manifest、冻结 Profile/Plan，并在不继承用户配置的最小环境中重跑。最终是 1 个历史
root cause killed、0 false positive、0 invalid。完整结果见 `evaluator/report.json`。

Coverage 在两侧完全相同，只作为解释字段；不参与 `killed`/`control-pass` 判定。

## 资格与边界

`qualification-report.json` 的六类 typed-requirement 集合判断使 `63903dd` 成为唯一
`qualified` 的公开候选；另三个候选仍因明确能力缺口 `deferred`。本 pilot 的触发使用受控
消息邮箱、保留并稍后释放一个旧 heartbeat response。当前 RawNode Driver 没有自然 timer
queue，因此为得到确认 heartbeat 使用合法的 ReadIndex retry；该差异已在阶段记录中公开，
不可把本结果称为对上游测试 harness 的逐字节复刻。

## 阅读顺序

1. `summary.json`；
2. `qualification-report.json` 与 `capability-snapshot.json`；
3. `../../../../plans/defectbench/etcdraft-63903dd-v1.json`；
4. `build-input/`、最终 `build-audit/*-v2.json` / `*-v3.json`；
5. `campaign/` 的同名最终报告；
6. `evaluator/blind-manifest.json`、`submission.json`、`report.json`。
