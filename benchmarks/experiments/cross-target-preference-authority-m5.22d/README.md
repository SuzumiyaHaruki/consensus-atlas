# M5.22d cross-target preference authority calibration

该实验使用离线、确定性的 blind-model mock，不读取 key、不访问网络。mock 与 deterministic baseline 共享
同一个 target-blind contract、硬约束和执行上限；模型只能改变 parent Intent 的 preference。

结果是负证据：proposal/plan identity 改变，但两个真实目标的 report、bundle 和 trace 均未改变。原因是
共同 Catalog 只有一个 backend，当前 preference 不具备执行层选择权。因此该结果不能用于宣称 Agent 优势，
反而要求下一步先复用现有策略建立至少两个真实不同的共同 backend。

复现：

```bash
go test ./cmd/control-experiment \
  -run '^TestM522dBlindMockAndBaselineUseOneDurableCommonProposal$' \
  -count=1 -timeout 3m
```
