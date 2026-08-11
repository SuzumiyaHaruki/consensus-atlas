# etcd/raft Ready.MustSync semantic reconstruction pilot v1

这是一个公开历史语义的受控重构实验，不是 Agent 方法对比，也不是 etcd/raft 的全面验证。
candidate 对当前本地 `go.etcd.io/raft/v3 v3.6.0` module tree 做一处精确反向源码转换：
将当前 `Ready.MustSync` 判断恢复为基于 `rd.HardState` 的旧语义；control 是未修改的官方
v3.6.0 module tree。两者使用相同 Driver、Profile、Test Plan、trusted monitor 和 evaluator。

## 结果

| opaque trial | status | finding | primary / replay work | Coverage |
|---|---|---|---:|---:|
| `trial-ready-must-sync-candidate-v1` | `killed` | `ready-must-sync`，step 65 | 105 / 105 | 2/3，75.00 |
| `trial-ready-must-sync-control-v1` | `control-pass` | 无 | 100 / 100 | 2/3，75.00 |

Evaluator 重新执行两份受控构建二进制，并从重跑 trace 重新计算 `ready-must-sync` monitor。
candidate 在 message-only Ready 且 HardState 未变化时产生 `MustSync=true`，control 不产生
该 finding。最终结果是 1 个 semantic root cause killed、0 false positive、0 invalid。
完整结果见 `evaluator/report.json`。

Coverage 在两侧完全相同，只作为解释字段；不参与 `killed`/`control-pass` 判定。

## 资格与边界

`qualification-report.json` 的六类 typed-requirement 集合判断使
`etcdraft-0675f3d-ready-must-sync` 成为唯一 `qualified` 候选；另三个公开候选仍因明确能力缺口
`deferred`。本 pilot 使用 opt-in Runtime Profile `etcdraft-ready-must-sync-v1`，只有在 Profile
digest 绑定该模式时 Driver 才暴露 `conditional-ready-sync` 和 `ready-must-sync-observation`。
默认 Driver 路径保持保守 sync，不暴露该 monitor。

本实验不是对 `0675f3d` 的完整历史 checkout 复刻。它证明的是：在当前官方 v3.6 module 上进行
一处精确、可审计的语义反向转换后，现有评测链能区分旧 Ready.MustSync 语义和当前 control。

## Fresh-clone 复验

冻结工件绑定 `go1.25.8/linux/amd64`，并要求本地 module cache 已有
`go.etcd.io/raft/v3@v3.6.0`。在干净 clone 中先完成通常的 Go 依赖准备，然后运行：

```bash
go mod download
./benchmarks/pilots/etcdraft-ready-must-sync-v1/reproduce-fresh-clone.sh
```

脚本不覆盖已存在的 pilot binary；它在 `artifacts/` 下重建 candidate/control，使用临时目录
重算 qualification、两份 Campaign 和 evaluator，并逐字节比较所有冻结 JSON 工件。通过时只会
留下忽略的 binary 和一个打印出的临时目录。

## 阅读顺序

1. `summary.json`；
2. `qualification-report.json` 与 `capability-snapshot.json`；
3. `../../../../profiles/raft/ready-must-sync-v1.json`；
4. `../../../../plans/defectbench/etcdraft-0675f3d-v1.json`；
5. `build-input/` 与 `build-audit/`；
6. `campaign/` 的两份报告；
7. `evaluator/blind-manifest.json`、`submission.json`、`report.json`。
