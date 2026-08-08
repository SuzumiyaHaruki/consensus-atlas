# etcd/raft v2 public calibration — M5.16

本目录是公开、主动构造的 evaluator calibration，不是历史回归或非公开 holdout。

## Pair

- control：官方 `go.etcd.io/raft/v3@v3.6.0`；
- candidate：同一 module 的 `raft.go` 唯一 source transform，BuildID `sut-c9811ab0ed8e2f39`；
- workload：同一条 opaque write；
- budget：96 scheduler decisions、98 primary work units；
- projector：`official-etcdraft-v2/applied-prefix-digest-v1`；
- monitors：TraceIntegrity、Agreement。

candidate 保持 proposal JSON 可解析，但在节点 3 的 normal entry 中改变 value bytes。构建器验证 exact
match、source/module digest，使用 offline readonly module copy 构建，并在 `build-audit/` 保存命令、
toolchain 和 binary identity。

## Result

control 为 `control-pass`。candidate 通过 TraceIntegrity，随后在 step 55、decision position 5 产生
不同 exact application digest，被 Agreement 标记为 `killed`。Evaluator summary：1 control、0 false
positive、1 candidate/root cause killed、0 invalid。

`evaluator/report.json` 是可信结果账本；Coverage/PSS 没有参与 verdict。完整 bundle 各约 1.9 MB，位于
ignored `artifacts/`，不提交到仓库。

## Reproduce

```bash
make experiment-etcdraft-v2-bundle
make experiment-etcdraft-v2-calibration
make evaluate-etcdraft-v2-calibration
```

冻结摘要：

- build input file SHA-256 `1485c6e8f97a740a7af80de823f3aca7ec1992f2042af3fa23d3ab116ac077b8`；
- build audit file SHA-256 `7c63ec00cda37cc051e9fec8aef799c5e55bedf2e795c1fe6aeff76173205238`；
- final M5.16R binary SHA-256 `a66c09adbb12fc975a9680fd58f726dbf2c2ca4afa5408ad39a33648843698a9`；
- benchmark digest `0ca75d758a50f83d2d735787d863c1cb3eeeb6ddd5cd471ba5e89c0e10dc17ad`；
- evaluation digest `fd4ecc9b0ca719a0dfacefd37c5e0067885a1aa5f0f1fb6b0f6da0ae4d71e664`。
