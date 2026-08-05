# ConsensusAtlas

ConsensusAtlas 是一个面向 CFT/BFT 共识协议的语义场景探索与覆盖评估系统。当前版本首先实现确定性测试骨架，并使用一个很小的 `toy-quorum` 协议演示控制链路，不把 etcd/raft 直接耦合进测试核心。

当前已经实现：

- Go 编写的逻辑时钟和确定性事件队列；
- 消息、持久化、超时、宕机和重启事件模型；
- 事件依赖，例如“投票持久化完成后才能发送投票响应”；
- 声明式 JSON TestSpec 解释器；
- 原始执行轨迹、包含状态快照的稳定重放指纹，以及独立的规范化场景键；
- 独立的 trace-integrity 与 agreement monitor；
- 带 Reach/Observe/Check/Replay/Conform 证据的覆盖账本；
- 保守的结构规范化键；
- 一个只输出计划、不参与执行和判定的 Python Agent 接口占位程序。

## 快速运行

```bash
make test
make run
```

结果写入 `artifacts/toy-run.json`。也可以直接输出到终端：

```bash
go run ./cmd/runner \
  -profile profiles/toy-v1.json \
  -scenario scenarios/toy-election.json
```

## 设计边界

这个仓库目前是“可信执行链”的骨架，不宣称已经完成通用共识测试：

- `toy-quorum` 不是 Raft，也不是正确性参考实现；
- 当前结构规范化保留原始 payload，没有擅自合并 term、日志或 QC；
- 当前事件选择是确定性的显式调度，还没有实现 DPOR；
- 覆盖 Profile 很小，100 分只表示覆盖该演示 Profile；
- Python Agent 不在执行、Oracle 或计分的可信路径内。

下一阶段应首先实现 `internal/adapter/etcdraft`，准确建模 `Ready -> persist -> send -> Advance`，然后再加入 Raft PSS、语义规范化和系统化偏序探索。详细设计见 [docs/architecture.md](docs/architecture.md)。
