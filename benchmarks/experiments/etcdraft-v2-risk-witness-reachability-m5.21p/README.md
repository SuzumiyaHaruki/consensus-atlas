# M5.21p etcd/raft RiskWitness 真实可达性校准

本目录仅保存一份小型、可重算的公开校准摘要。完整 report 和 ExecutionBundle 可用下列命令
通过 official etcd/raft Adapter 重生，不提交到 Git：

```bash
go run ./cmd/control-experiment \
  -strategy workload-risk-witness-calibration \
  -decisions 64 \
  -policy-seed 1 \
  -out /tmp/m5.21p-report.json \
  -bundle-out /tmp/m5.21p-bundle.json
```

`summary.json` 由集成测试从真实 primary/replay execution 重算并逐字段对账。结果为
64 decisions、66/66 primary/replay work、workload pending，且三个冻结 milestone 在
steps 28/53/54 有序达到。

这是人工固定的正向可达性校准，只证明控制面和 projector 可用。它不是搜索方法、Oracle
verdict、formal holdout、Agent 效果证据或绝对测试质量分数。
