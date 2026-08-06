# 当前阶段

日期：2026-08-06
阶段：M4.17 正式 benchmark 机械准入（等待私有真实样本）

## 当前可确认的结果

- Runtime、Replay、Oracle、Coverage Ledger、PSS 和 Defect Benchmark 的可信路径已存在；
- 公开 M4.9 ReadIndex 与 M4.11 Ready.MustSync pilot 已验证两条不同的 evaluator 链，不能作为
  Agent 方法效果样本；
- Blind Planner v1 已将 LLM 输入缩减为 opaque trial、Profile/Capability 投影、opaque debt、
  受限 DSL、预算和机械 finding；真实目标、SUT/Driver identity、trace、Oracle 与 Ledger 留在
  可信路径；
- Blind v1 仅通过无模型 fixture 验证，尚未产生新的模型实验结果。
- private Manifest 到 Blind Manifest 的 exposure audit 已完成：它会拒绝 private variant identity、
  root cause、build/source identity 或 private monitor 在公开 JSON 中的直接泄露。
- Agent 已接受的真实 Test Plan 现在只进入 private replay bundle；无模型 `blind-replay` 能以相同
  Coordinator identity 重放它，供 evaluator 比较 Campaign report digest。
- 通用执行/规划层只接收 Driver Manifest 声明的 `protocol-input` 词汇，不解释 Raft 操作名；
  etcd/raft API 映射停留在具体 Driver。早期 toy、Contract-only 与旧全信息 Planner 路径已删除。
- 新的 private benchmark Manifest v2 把 `pss_id` 冻结为身份的一部分；evaluator 从 Manifest
  选择 trusted monitor，并拒绝 Campaign report 的 PSS identity 不匹配。
- private Manifest v2 在生成 Blind view 前还须通过 readiness gate：版本、来源、artifact binding、
  control 数与 distinct root-cause label 数均由确定代码检查；它不替代 curator 对真正独立性的审查。

## 下一项决策性工作

冻结多个相互独立、不会向 Planner 暴露 candidate identity/trigger 的 qualified trial 与正确
control；对每个 scope 完成 exposure audit，并在私有 workspace 演练 agent/replay/evaluator 构建链。
随后在相同 primary-work、decision、token 与 wall-clock
预算下比较 Random、DFS、专家、无反馈 Planner 与 Blind Planner。

在此之前，不应增加 Scenario/Critic Agent、复合 obligation 或宣传任何 Agent 优势。

## 阅读入口

1. [总体规划](ConsensusAtlas-总体规划.md)：研究目标、可信边界、冻结决策与路线图。
2. [M4.17 阶段总结](stage-m4.17-formal-benchmark-readiness.md)：私有正式样本的机械准入条件。
3. [Defect Benchmark](defect-benchmark.md)：正式方法评价的独立分母与判定规则。
4. [架构](architecture.md)：包依赖、Runtime 与 Agent 的分层。
5. [文档导航](README.md)：按“理解/实现/实验”分类的完整阅读顺序。
