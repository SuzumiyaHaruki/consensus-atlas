# M5.23a bounded stateless DFS calibration

该公开校准从 M5.21p 的真实 etcd/raft 轨迹取前 28 个 decision 作为固定根前缀，使用 fresh Adapter
反复重放并重建 canonical admissible frontier。DFS 按 ActionID 顺序、depth-first 顺序生成最多 6 个
`StateRef + ActionRef + PathMetadata` WorkItem，不克隆实现状态、不做状态等价合并。

固定边界为 depth 2、6 WorkItems、1,000 search work units。实际扩展 2 个前缀、产生 6 个 WorkItem，
消耗 443 search work units，并因 item 上限停止。全部 child prefix 均经独立 fresh Replay。最后一个
叶路径编译为 exact Policy 后进入原 qualified executor，执行前 30 decisions 与叶前缀 digest 一致，
完整 64-decision primary/replay 各计 66 work units。

该结果只证明最小 DFS 执行与记账边界，不证明状态空间完整、DFS 优于随机方法、缺陷检出或 Agent 效果。

复现：

```bash
go test ./cmd/control-experiment \
  -run '^TestM523aEtcdraftBoundedStatelessDFSIsDeterministicAndExecutable$' \
  -count=1 -timeout 3m
```
