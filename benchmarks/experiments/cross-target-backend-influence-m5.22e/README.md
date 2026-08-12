# M5.22e cross-target backend influence gate

该门禁不调用模型、不增加 Runtime 或 scheduler。它把已经存在的 bounded action-class 与 bounded
uniform 暴露为两个共同 backend，在 etcd/raft 与 OmniPaxos 上使用相同的 96-decision 上限、共同
Action surface、单一 opaque workload 和预先固定的 seeds 1/2/3。

12 次 primary execution 均完成 workload、达到共同 Intent，并通过 fresh strict Replay。6 个同
target/seed 的 backend pair 全部产生不同的 Action trace。由此只能得出：backend preference 已经具有
真实、可重放的执行影响。该结果不比较搜索质量，不证明 Agent 优势，也不包含缺陷检出或覆盖评分。

复现：

```bash
go test ./cmd/control-experiment \
  -run '^TestM522eTwoCommonBackendsChangeBothTargetTraces$' \
  -count=1 -timeout 3m
```
