# etcd/raft v2 action-class random public calibration — M5.17a

本目录冻结 M5.17a action-class random 基线的小型构建与评测账本。它是公开、主动构造的 calibration，
不是历史回归或非公开 holdout。

## 固定比较边界

- target：`go.etcd.io/raft/v3@v3.6.0`；
- workload：`single-write-v1`，plan digest `1cbab34b...28b13c`；
- policy：`consensus-atlas/action-class-random-policy/v1`，seed 1；
- budget：96 scheduler decisions、98 primary work units；
- projector：`official-etcdraft-v2/applied-prefix-digest-v1`；
- control config digest：`0776ecc9...3416c`；
- candidate config digest：`f833a3fa...98d52`。

benchmark manifest 显式绑定两侧 config digest，因此不能替换 policy、workload、FaultEnvelope、
admission 或 SUT identity 后继续沿用本账本。

## 结果

| trial | BuildID | decisions | primary / replay | verdict |
|---|---|---:|---:|---|
| official control | `go.etcd.io/raft/v3@v3.6.0` | 96 | 98 / 98 | `control-pass` |
| command-data calibration | `sut-c9811ab0ed8e2f39` | 96 | 98 / 98 | `survived` |

summary：1 control、0 false positive、1 calibration candidate、0 killed、1 root cause、0 killed root
cause、0 invalid。evaluator digest 为 `3886388d...8cb68`。

candidate survived 并不表示实现正确。该 seed 的合法 schedule 没有形成 Agreement 可观察的命令数据
冲突。它与 M5.16 fixed policy 检出同一 candidate 的结果共同表明，粗粒度状态发现数量不能替代
root-cause detection。

## 目录

- `build-input/candidate.json`：唯一 source transform 和离线构建输入；
- `build-audit/candidate.json`：source/module/binary/command/toolchain 审计；
- `evaluator/manifest.json`：build/config/projector/budget 绑定；
- `evaluator/report.json`：可信 evaluator 重算结果。

完整 report、trace、bundle 和二进制保存在被 Git 忽略的 `artifacts/`，避免重复 trace body 增大仓库。

## 复现

```bash
make experiment-etcdraft-v2-action-class-random
make experiment-etcdraft-v2-action-class-calibration
make evaluate-etcdraft-v2-action-class-calibration
```

builder 使用 module cache 的离线只读副本，不修改 `/home/nitro/Desktop/raft`。
