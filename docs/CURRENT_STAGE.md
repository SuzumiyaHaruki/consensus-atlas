# 当前阶段

更新时间：2026-08-19
分支：`feature/agentic-consensus-testing`
阶段：S1 精确收尾完成，准备进入 Agent 效果实验

## 一句话状态

活动主线是：

```text
知识包/源码目录/Target 能力
        ↓
Risk Agent 生成并修订候选
        ↓
Scenario Agent 生成语义计划
        ↓
可信绑定 → Runtime → Trace → fresh Replay
        ↓
Target-local Observation/PSS/Oracle → 结果与成本
```

etcd/raft 与 OmniPaxos 两个真实 Target 已走通该链路。M4l2/M4l3 说明
Target-local 消息 leaf type 可以在不扩展公共 ActionKind 的情况下减少
Scenario Agent 的消息选择歧义；这是表达与选择能力的校准证据，不是协议
finding，也不是多 Agent 优于 baseline 的证据。

## 当前输入

- `plans/agent/`：Agent 方法、模型预算和协议知识配置；
- `adapters/*v2/`：官方实现 API 到统一控制面的薄适配；
- `qualifications/*v2/`：Target 实际可控、可观测和可 Replay 能力；
- Target composition：Observation projector、Scenario 语义投影和 Oracle
  registry；
- 受限源码 catalog/mount：Agent 可查询的只读材料；
- 调查预算：调用数、tokens、Action decisions、primary/replay work 和时间。

旧 Campaign/Profile/固定 Scenario 数据不再是活动输入。

## 当前处理能力

- produced/released message 控制、自然虚拟时间、crash/restart、workload；
- target-local namespaced Observation 与通用/Target Oracle 组合；
- typed Risk portfolio、受限源码查询、机械资格与 capability/fidelity feedback；
- 多步 Scenario、live Runtime、周期 `ProgressDelta` 和最终 fresh Replay；
- durable provider journal、Episode/Investigation 恢复与完整成本核算；
- formal/private evaluator 从保存的 Bundle 重算 Oracle；
- 所有 Replay 稳定候选均可离线运行 Oracle，Agent 自报不能产生 finding。

分支实验能力仍存在，但当前主实验优先使用 `continue`、`revise`、`abandon`。
未对 Agent 开放且没有真实用途的 `minimize` 已移除。PSS 保留并冻结为离线
探索指标，不再扩 vocabulary，也不作为当前优化目标。

## 本轮瘦身

已完成：

- 删除约 4.3GB 可重建本地 build/cache 生成物；
- 删除旧 Campaign contract/profile/plan/scenario 数据和历史 coverage 文档；
- 删除生产中无调用者的独立 bounded DFS；保留并重命名 Scenario 实际使用的
  frontier reconstruction、live child execution、fresh verification 和成本核算；
- 删除 M4l2/M4l3 一次性 CLI runner 与其重复实验框架，保留 Adapter 消息语义、
  Target projector 和小型回归；
- 普通测试不再读取 `benchmarks/experiments` 或 `benchmarks/pilots`，必要的
  build-audit fixture 移入小型 `testdata/`；
- 历史阶段 summary 等值测试改为对象级行为测试；
- 压缩仓库约束和阶段文档，删除桌面规划副本要求；
- 删除两个确认无调用者的旧执行 wrapper，并让注释只指向 live Runtime 主线；
- 删除 `benchmarks/pilots` 以及除最终 M4l2/M4l3 外的历史、失败和基础设施实验；
- M4l3 使用 `final-summary.json` 取代有歧义的原始 summary；三份 canonical Bundle
  以确定性 gzip 保留，展开 Bundle、Scenario result、root Trace 和 provider journal
  不进入 HEAD；
- 清理所有本轮遗留的空目录。

验证已完成：`go test ./...`、`go vet ./...`、`audit-no-v1`、
`audit-race-shards`、受影响 Scenario/消息语义聚焦 race、Rust fmt/clippy 和
全仓 JSON/压缩 Bundle 解析、`git diff --check` 全部通过。仓库工作区（含 Git）
约 33MB；Go 代码 52,783 行，其中测试 17,452 行、生产代码 35,331 行；
`benchmarks/` 收敛为 56 个文件、约 0.5MB 实际内容。

## 当前结果边界

已经证明：

- 两个真实 Target 可按统一 Action/Trace/Replay 接口执行；
- Agent proposal 可以经过 typed repair 后绑定真实 enabled Action；
- target-local 消息语义能改善至少两个公开单样本中的目标选择；
- Replay、Oracle、方法身份和成本边界具有机械检查。

尚未证明：

- Agent 或多 Agent 优于 Random、专家或其他搜索方法；
- 当前 Risk/Scenario 策略能够长时间、全面测试任意共识；
- PSS 数量代表测试完整度或剩余缺陷概率；
- etcd/raft、OmniPaxos 或 ConsensusAtlas 正确、完整或无缺陷。

## 下一步

1. 停止以代码行数为目标的大范围删除；
2. 补一个不依赖历史工件的 formal evaluator candidate/control 纯内存行为测试；
3. 扩大 Agent 的源码理解和基于 `ProgressDelta` 的 revise 能力；
4. 设计同预算 Random/单 Agent/双 Agent 对照，再运行长时公开实验；
5. 只有效果证据成立后才进入 private holdout 和第三协议接入。
