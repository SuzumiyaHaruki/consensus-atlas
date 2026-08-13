# 文档导航

当前设计只认以下入口：

1. [当前阶段](CURRENT_STAGE.md)：现在能做什么、不能做什么、下一步是什么；
2. [总体规划](ConsensusAtlas-总体规划.md)：研究目标、完整流程和阶段路线；
3. [架构](architecture.md)：代码模块、信任边界与数据流；
4. [Control Runtime v2](control-runtime-v2.md)：Action、消息、时间、生命周期、持久化和 Replay；
5. [A2R 架构收敛与减负](stage-a2r-architecture-convergence.md)：本轮删除内容与代码量变化。

当前 Agent 主线的证据顺序：

1. [A1 Semantic Episode 基础](stage-a1-semantic-episode-foundation.md)
2. [A2a Semantic Best-first](stage-a2a-deterministic-semantic-best-first.md)
3. [A2b1 Single Explorer](stage-a2b1-single-explorer-contract.md)
4. [A2b2 Durable Explorer Transport](stage-a2b2-durable-explorer-transport.md)
5. [A2b3a etcd/raft 公开校准准备](stage-a2b3a-etcdraft-public-calibration-gate.md)
6. [A2b3b 真实模型公开校准](stage-a2b3b-etcdraft-real-model-calibration.md)

A1–A2b3b 文件是当前主线的阶段证据，不定义新的现行接口；已经从 HEAD 移除的旧阶段日志、代码和大型
工件可从 Git 历史恢复。新增文档应优先修改上述当前入口，避免再建立平行规划。
