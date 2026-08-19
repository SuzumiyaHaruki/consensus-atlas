# 当前阶段

更新时间：2026-08-19
分支：`feature/agentic-consensus-testing`
阶段：M4l6R 固定已接受 Risk 的 Scenario-only 配对 pilot

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

M4l4 进一步用零模型调用的脚本证明了一条完整垂直路径：在丢弃
`MsgAppResp n2→n1` 后，etcd Target-local closure selector 只沿 n3 备用复制链
和必要 Ready effect 前进，12 个 decision 后提交并应用同一 RequestID；最终
alternate-quorum Risk reached，fresh Replay 稳定，四个 Oracle monitor 无 violation。
闭合器只能从当前 frontier 选择 Effect/Deliver/Temporal；歧义会以正常停止和原
frontier 返回，不能继续制造 Drop、Crash 等干预。权威 frontier 与 selector 副本
已经隔离，且请求恰好在第 12 个闭合 decision 完成时仍报告 `client-terminal`。

M4l5 已将同一 closure 作为可选 Target composition 接入真实 Scenario Agent
执行路径。公共层只在计划已经执行 Drop/Duplicate/Crash/Partition 干预后调用
Target factory；factory 不识别当前 Risk/干预时仍使用原公共顺序，一旦激活则
`underdetermined`、`no-eligible` 和预算停止直接进入结构化 Agent feedback，不会
静默回退。`after_milestone` 与计划结束后的自然推进共用这一逻辑，fresh Replay
仍只执行已记录的精确 Trace，不重新调用 selector。

零模型 Agent Episode 回归已复现 M4l4：5 个 Agent 计划 decision 后，由 etcd
闭合器执行 12 个 decision，最终同一 RequestID committed、Risk reached、fresh
Replay 稳定，四个 Oracle monitor 无 violation。没有 ClosureFactory 的 Target
继续走原 `ExploreScenarioWithPlanner`/公共自然推进路径。

跨调用闭环也已收口：Scenario Agent 从已晋升的可信 `ScenarioExecution` 恢复最近
一次真实干预。零模型两轮回归先得到 `closure-underdetermined`，再以 `revise`
执行一个非干预 Action；第二轮仍使用原干预的 Target closure，并以三个专属闭合
Action 消耗完预算后返回 `closure-budget-exhausted`，没有退回公共固定顺序。
已经满足的 `after_milestone` 不再提前构造 factory。etcd 多候选 alternate 推导也
转换为 `closure-underdetermined`，不再作为执行错误。

当前新运行的 MethodSpec implementation identity 已更新为
`consensus-atlas/agentic-method/m4l5-closure-v1`，因此接入 closure 前后的方法 digest
不同。旧 `m4d-v1` 仅保留只读工件验证；新建和恢复运行仍必须与当前完整 MethodSpec
严格一致，不能把旧 Investigation 混入新实现。

M4l6 已在同一个活动 CLI 中加入两个窄实验臂：`public-fixed` 与
`target-local`。它们不是调用方自报标签：`public-fixed` 会从实际 Target
composition 移除 closure factory；`target-local` 只有在 Target 已提供 factory 时
才可选择；随后 `AgenticMethodSpec.closure_mode` 从 composition 的实际 factory
机械派生。composition、MethodSpec 与 factory 不一致时普通输入校验直接拒绝，两个
arm 的 MethodSpec digest 必然不同。OmniPaxos 当前没有 closure factory，因此不能
被标成 `target-local`。旧 `m4d-v1` 工件没有该字段并继续按原 digest 只读验证。

`after_milestone` 在专属闭合恰好耗尽预算时，步骤和 Agent 顶层反馈现在都保留
`closure-budget-exhausted`，不再被通用 `budget-exhausted` 覆盖。零模型回归已覆盖
模式身份、factory/MethodSpec 失配拒绝和该预算边界。本轮没有调用外部模型。

短配对实验的 etcd 输入将单次 Scenario plan 上限从 4 调整为 5；这是已验证
`persist leader → advance leader → deliver MsgApp → persist follower → drop MsgAppResp`
干预前缀所需的最小长度；该项调整本身没有改变 Scenario 调用数、总 decision 或时间预算。

首轮 `public-fixed` 预算校准中，Risk 与首个 Scenario provider 调用合计 50,793
tokens，超过旧 Episode 上限 50,000；Scenario 响应因此没有进入 proposal 执行。
后续两个配对 arm 统一使用 120,000 token Episode 上限，其他调用、Action 与时间
预算保持不变，并使用新的输出目录和新的 MethodSpec digest。该首轮结果只作为预算
校准证据，不参与 closure 效果比较。

随后完成的两个 live arm 仍不能作为正式配对结果：`public-fixed-v2` 使用 3 次
Scenario 调用、111,234 tokens 且 Risk 未到达；`target-local-v2` 使用 1 次 Scenario
调用、41,157 tokens 且其 Risk 到达。虽然两者均完成确定执行与 stable Replay，
但 Risk Agent 实际生成了不同的 Risk spec；target-local arm 的 Risk 也没有要求
`MsgAppResp` 丢弃后的 alternate-quorum 闭合，因此不能把差异归因于 closure。

M4l6R 为此加入窄的 `-fixed-risk-input` 模式。输入是公开的 RiskCandidate JSON，
可信代码会针对当前 Target 重新执行资格与 capability-gap 检查；合格后跳过 Risk
provider，仅运行 Scenario Agent、Runtime、fresh Replay 与 Oracle。Risk 候选规范化摘要、
`fixed-accepted` 模式和实际 `closure_mode` 均进入原有 MethodSpec。固定模式只允许
单 Episode 且不与 capability probe 混用。零模型回归已证明 Risk provider 为 0、
Scenario provider 为 1，并以同一 alternate-quorum Risk 完成 5 个计划 Action、12 个
closure Action、stable Replay 和四个 Oracle monitor。

真实 M4l6R 两臂已经运行。两者的 fixed Risk digest 完全相同，均为 0 Risk calls、
3 Scenario calls、stable Replay 和 0 Oracle finding；因此上一轮 Risk 漂移问题已消除。
但 public-fixed 使用 82,848 tokens/5 Scenario decisions，target-local 使用 92,913
tokens/7 Scenario decisions，二者均以 `call-budget-exhausted` 停止且 Risk 未到达。
target-local 在第 2 次调用已经真实 drop 目标 `MsgAppResp n2→n1`，但 Agent 在同一计划
中继续请求尚未 enabled 的 n3 response，导致 `no-match`；第 3 次调用再次过早请求，
所以只在完整计划结束后接管的 closure 没有激活。该结果是可信的接口负证据，不是
closure 无效或协议 finding。精简结果见
`benchmarks/experiments/etcdraft-fixed-risk-scenario-only-m4l6r-v1/`。

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

1. 让 Scenario view/prompt 明确表示 target-local post-intervention closure 可用，或
   增加显式零 Action 的 closure handoff；Agent 完成真实干预后不应继续猜闭合时序；
2. 重跑相同 fixed Risk 两臂，比较正确 `MsgAppResp` 干预、handoff、Risk/RequestID/
   Replay/Oracle、decisions/calls/tokens 以及停止原因；
3. 扩大 Agent 的源码理解和基于 `ProgressDelta` 的 revise 能力；
4. 设计同预算 Random/单 Agent/双 Agent 对照，再运行长时公开实验；
5. 只有效果证据成立后才进入 private holdout 和第三协议接入。
