# 当前阶段

日期：2026-08-13

分支：`feature/agentic-consensus-testing`

阶段：A0 Agentic Consensus Testing 研究主线重置完成

## 一句话状态

ConsensusAtlas 已完成共识控制、严格执行和可信评价底座，但现行 Agent 只是局部 frontier 排序器；
本分支正式把研究重心转为“Agent 提出并修正测试假设，统一控制层确定执行，独立 Oracle 决定结果”。

## 输入什么

本阶段输入是：

- `feature/control-runtime-v2` 的已推送基线 commit `0106e2c`；
- M5.23g/R4b 的真实负结果和可恢复 Campaign 产物；
- 已有 etcd/raft、OmniPaxos、Runtime、Replay、PSS/Risk 和 evaluator 能力；
- 对 Agora、MODIST 和当前实现主体性的重新审查。

## 如何处理

本阶段只进行研究规划和文档修改：

1. 保留 Runtime、Adapter、exact-prefix、Replay、Oracle、Campaign 和完整成本账本；
2. 将 Agent-v1 frontier permutation 固定为最小权限消融，不再当作最终 Agent；
3. 把 Agent 权限重定义为协议假设、语义 episode、全局 WorkItem 优先级和反馈修正；
4. 保持 Agent 无 enabled、语义真值、Oracle、分母和最终 verdict 权限；
5. 将第一版多 Agent 收缩为 Protocol/Hypothesis 与 Explorer 两个职责；
6. 用 single-vs-two-role 消融决定多 Agent 是否值得保留；
7. 用公开闭环先验证行为权限，再冻结方法并进行隐藏 candidate/control 评价。

本阶段没有修改任何 Go/Python/Rust 生产代码，没有调用模型，也没有读取凭据。

## 得到什么

- 新的长期开发分支 `feature/agentic-consensus-testing`；
- 更新后的总体规划 Draft v1.72；
- [A0 详细路线](stage-a0-agentic-research-reset.md)；
- Agent 提议层、执行层和结论层的明确边界；
- 三个主研究问题：统一共识控制、Agent 测试效果、多 Agent 必要性；
- A1–A6 的增量实施和停止线。

## 继承的已验证底座

- Runtime-owned 消息、自然时间、生命周期、持久化副作用和输入；
- etcd/raft strict execution 与 OmniPaxos 跨协议执行证据；
- exact-prefix bounded search、fresh Replay、PSS/Risk 投影；
- Agreement/target Oracle、ExecutionBundle、Campaign 和 WorkLedger；
- formal candidate/control evaluator 与公开/隐藏数据隔离边界。

## 尚未证明

- 当前仍不是真正完成的多 Agent 测试系统；
- Agent-v2、Semantic Episode 和反馈修正尚未实现；
- 没有证明 Agent 或多 Agent 优于 random、DFS、semantic best-first 或专家计划；
- 没有非公开 candidate/control 的重复方法结果；
- 没有证明 PSS/义务能够预测缺陷检出；
- BFT、leaderless 和自动接入仍不在当前已验证范围。

## 下一阶段：A1

实现一个最小 Semantic Episode 纵向切片：

1. 行为不变地分离 `SearchAlgorithm` 与 `GuidancePolicy`；
2. 最小定义 `TestHypothesis`、`SemanticEpisodeView`、`EpisodePlan`、`EpisodeReport`；
3. 用 deterministic fixture 演示一次合法执行和一次拒绝—反馈—修正；
4. 复用现有 Runtime/Trace/Replay/WorkLedger；
5. 不调用真实模型，不实现 DPOR，不增加新 Oracle、target 或 Campaign 家族。

A1 完成后再进入公开 single Agent-v2 闭环，而不是立即运行隐藏评测。

## 当前建议阅读顺序

1. [A0 Agentic 研究主线重置](stage-a0-agentic-research-reset.md)
2. [总体规划](ConsensusAtlas-总体规划.md)
3. [README](../README.md)
4. [M5.23R4b 基础设施基线](stage-m5.23r4b-stateless-agent-campaign.md)
5. [M5.23g 真实 Agent 负结果](stage-m5.23g-real-agent-multi-root-pilot.md)
6. [Control Runtime v2](control-runtime-v2.md)

历史阶段文档保持不可改写；它们记录旧路线，不表示其下一阶段仍对当前分支生效。
