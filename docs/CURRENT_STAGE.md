# 当前阶段

日期：2026-08-13

分支：`feature/agentic-consensus-testing`

阶段：A2R 架构收敛与减负

## 一句话状态

ConsensusAtlas 已从“多条历史实验路径并存”收敛到 Semantic Explorer + 可信搜索/Runtime +
Replay/Oracle/evaluator 主线；当前下一项功能工作不是再加对象，而是把 Explorer 选中的 prefix 送进完整
qualified testing 闭环。

## 输入什么

- etcd/raft、OmniPaxos 或 HashiCorp Raft 目标及其 Adapter；
- protocol knowledge、TestHypothesis、PSS/Risk mapping；
- workload、fault envelope 和执行/模型预算；
- 显式 opt-in 时才提供模型 key。

## 如何处理

1. Qualification 机械检查目标实际具备的控制能力；
2. Explorer 读取可信生成的 semantic candidate queue，只提交候选排序；
3. exact-prefix search 和 Control Runtime 物化真实 Action；
4. Trace/Evidence 由可信代码映射为 PSS/Risk；
5. fresh Replay、Oracle 和 evaluator 独立验证结果；
6. Campaign 可组织 deterministic baseline；当前 Semantic Explorer 尚未接入完整多 episode session。

## 得到什么

- 当前已能得到确定 prefix、Risk progress、provider 调用审计和可恢复公开校准 artifact；
- qualified execution 路径可独立得到 ExecutionBundle、PSS、strict Replay 和 Agreement 结果；
- 两条路径尚未在 Semantic Explorer 后合为一个完整用户测试结果，这是 A3 的目标；
- 公开 DeepSeek 校准只证明首个扩展发生变化，不证明 Agent 优势。

## 本阶段删除了什么

- 被新 semantic episode 替代的 trace mutation、corpus mutation 和 batch PSS guidance；
- 被 Semantic Explorer 替代的 Agent-v1 frontier-order Campaign；
- 未取得 qualification 的 raft-rs Adapter/probe；
- 仓库中重复展开的完整 Trace/bundle 与旧字节级 baseline JSON；
- 只检查历史 artifact identity、没有当前生产消费者的测试；
- README/架构/总体规划中的历史流水账。

历史仍可从 Git 提交 `0106e2c` 恢复。当前保留的调用持久化、qualification、Runtime 校验、strict Replay、
Oracle 和 evaluator 安全边界没有删除。

收敛后的规模为：Go 生产 29,053 行、Go 测试 13,780 行、Markdown 3,463 行、JSON 10,243 行。与本轮
盘点起点相比，Go 合计减少 8,480 行，JSON 减少约 63.7 万行。

## 当前边界

- 还没有配置时间内自动完成“plan → execute → feedback → revise”的 session；
- 还没有非公开 candidate/control 方法效果实验；
- 还没有同预算多 Agent 消融；
- 还没有证明 PSS/义务能预测隐藏根因检出；
- 普通测试不读取 key，也不调用外部模型。

## 下一阶段：A3 单 episode 真正闭环

复用现有对象完成：

```text
TestHypothesis -> Explorer selection -> qualified execution
              -> ExecutionBundle/PSS/Risk -> fresh Replay/Oracle
              -> one testing result
```

禁止为此新增平行 Ledger、hash、冻结 contract、baseline 或 gate；只有已有类型无法表达一个明确失败场景时
才讨论扩展。

## 阅读顺序

1. [总体规划](ConsensusAtlas-总体规划.md)
2. [架构](architecture.md)
3. [A2R 总结](stage-a2r-architecture-convergence.md)
4. [A2b3b 真实模型公开校准](stage-a2b3b-etcdraft-real-model-calibration.md)
5. [Control Runtime](control-runtime-v2.md)
