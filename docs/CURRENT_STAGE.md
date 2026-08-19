# 当前阶段

更新时间：2026-08-19
分支：`feature/agentic-consensus-testing`
阶段：M4m4 OmniPaxos Risk fidelity 修复完成

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
`consensus-atlas/agentic-method/m4m4-risk-fidelity-v1`。旧
`m4m1-closure-ownership-v1`、`m4l7-risk-input-closure-handoff-v1`、
`m4l5-closure-v1` 与 `m4d-v1` 仅保留只读
工件验证；新建和恢复运行仍必须与当前完整
MethodSpec 严格一致，不能把旧 Investigation 混入新实现。

M4l6 已在同一个活动 CLI 中加入两个窄实验臂：`public-fixed` 与
`target-local`。它们不是调用方自报标签：`public-fixed` 会从实际 Target
composition 移除 closure factory；`target-local` 只有在 Target 已提供 factory 时
才可选择；随后 `AgenticMethodSpec.closure_mode` 从 composition 的实际 factory
机械派生。composition、MethodSpec 与 factory 不一致时普通输入校验直接拒绝，两个
arm 的 MethodSpec digest 必然不同。etcd/raft 与 OmniPaxos 当前均提供窄 closure
factory；旧 `m4d-v1` 工件没有该字段并继续按原 digest 只读验证。

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

M4l6R 为此加入了读取 Risk 的原型。M4l7 将其固定为正式 `-risk-input`：可读取独立
RiskCandidate、RiskCandidateAssessment 或已有 Episode `summary.json` 中的
`accepted_risk`。可信代码只提取候选并针对当前 Target 重新执行资格与 capability-gap
检查；合格后跳过 Risk provider，仅运行 Scenario Agent、Runtime、fresh Replay 与
Oracle。规范化候选摘要、`existing-candidate` 模式和实际 `closure_mode` 均进入原有
MethodSpec。该模式支持单 Episode 和多 Episode Investigation；默认模式明确记录为
`agent-generated`。零模型回归已证明 Risk provider 为 0、
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

M4l7 已修复该接口缺口。Scenario view 现在显式声明
`post_intervention_closure`；prompt 要求 Agent 在目标干预处结束计划。可信执行器也不
仅依赖提示：每个成功 Action 后由 Target factory 检查真实前缀，只有 factory 明确认可
时才发生 handoff，剩余 Agent 预测步骤不会执行。handoff 后仍只能选择
Effect/Deliver/Temporal，并完整记录 handoff step、自动推进、fresh Replay 和 Oracle。
零模型回归使用“正确 drop 后附带过早 n3 response 投递”的过度计划，确认在 drop 后
截断为 5 个真实计划 Action并完成原 12 个 closure Action。public-fixed、无 factory
以及 factory 不识别干预的行为保持不变。

M4l7 真实 fixed-Risk 配对也已完成。两个 arm 的最终 Trace 在 Scenario step 29--31
共享完全相同的动作与 item identity，并丢弃同一个 `MsgAppResp n2→n1`。public-fixed
使用 3 次 Scenario 调用、83,202 tokens，在正确 drop 后错误预测 n3 response 的 enabled
时机，最终因调用预算耗尽而未达到 Risk。target-local 使用 2 次调用、41,133 tokens，
在同一 drop 后发生可信 handoff，执行 14 个受限闭合 Action，于 Trace step 45 提交
`a9d6-write-1` 并达到 Risk；fresh Replay stable，Oracle finding 为 0。该单样本证明
closure handoff 能在 Agent 已找到正确干预后闭合此因果路径。target-local view/prompt
明确暴露 closure，且 public-fixed 第2次调用因非法 `branch_id` 没有进入执行；因此少一次
调用和42,069 tokens 是完整方法臂结果，不能全部归因于 selector。记录的执行 work
反而是 public 198、target-local 250；没有 wall time/CPU/RSS，不能声称总成本降低。

两个 M4l7 Bundle 现已通过 `defect-eval -oracle-bundle` 独立验证 Bundle、projection
并重算 Target registry。两边均实际检查 trace-integrity、agreement、client application
binding 和 log progress，0 violation；canonical Bundle 以确定性 gzip 进入精简证据，
没有把未保存的 provider journal 列为证据。精简结果见
`benchmarks/experiments/etcdraft-existing-risk-closure-pair-m4l7-v1/`。

M4l8 随后完成零模型因果隔离。测试机械重建 M4l7 真实 Agent 的 step 29--31 共同
Trace，从同一个 drop 后状态分别运行 public-fixed 和 target-local。14-decision 等预算
下，两边 progress work 均为94、qualified primary/replay 均为47/47；target-local 在
step45 reached 并 committed 同一请求，public-fixed 预算耗尽且 not-reached。扩大 public
预算后，它在19个后干预 decision、step50 才 reached，progress work 为104、qualified
primary/replay 为52/52。三条 Trace 均 stable Replay，并由既有 registry 独立重算四个
Oracle monitor、0 violation。结果见
`benchmarks/experiments/etcdraft-shared-prefix-backend-ablation-m4l8-v1/`。

M4l9 已按预注册交替顺序完成五组真实模型重复。两臂均在 5/5 Episode 中选择语义等价的
follower→leader `MsgAppResp` 干预；其中两轮为 `n3→n1`，其余为 `n2→n1`。
target-local 通过 5/5 closure handoff 全部 reached，
public-fixed 仅 2/5 reached，其余三次在正确干预后耗尽调用预算。public-fixed 使用
15 次调用、395,236 observed tokens，target-local 使用12次、265,819 tokens；后者
qualified primary/replay work 略高，不能解释为所有成本均下降。target-local 中
4/5 是干预后直接闭合；`g01` 因旧的 per-call closure quantum 耗尽而交还 Agent，随后
多执行一次 Crash。10 个 Bundle 均记录 stable fresh Replay；独立 evaluator 校验
Bundle/projector 并重算四个 monitor、0 violation，但没有重新执行 Runtime Replay。结果仍只是
单 Risk/单协议的公开 capability 证据，不是缺陷 finding 或总体方法优越性证明。精简
报告见 `benchmarks/experiments/etcdraft-closure-repeats-m4l9-v1/`。

M4m1 已修复上述 closure 生命周期问题：Target 一旦从真实干预接管，只要 Episode
全局 decision 预算尚存，就自动延续同一 closure；per-call progress quantum 不再把
控制权提前交还 Agent。只有 `closure-underdetermined`、`closure-quiescent` 或全局预算
真正耗尽才结束接管。零模型回归以小于完整闭合长度的本轮 quantum 复现该边界，最终
仍完成 5+12 路径，Planner 只调用一次且 Trace 中没有额外 Crash。

M4m2 已完成 OmniPaxos 第二协议垂直切片。真实首请求产生 operation-carrying
`accept-sync` 而非 `accept-decide`；共同前缀在 step 26 丢弃 `accept-sync n1→n2`。
相同 4-decision 后干预算下，public-fixed 未 reached，OmniPaxos factory 从 evidence
推导备用参与者 n3，并只投递 enabled 的 `prepare→promise→accept-sync→accepted`，
恰好 4 步 reached；公共顺序放宽后需 8 步。target Trace 形成 qualified Bundle，
同一 RequestID completed，fresh Replay stable，trace-integrity/agreement 0 violation。
结果见 `benchmarks/experiments/omnipaxos-message-loss-closure-m4m2-v1/`。

M4m2R 已补齐真实 Scenario Agent 协调主路径。新增 existing Risk
`plans/agent/omnipaxos-message-loss-risk-v1.json`，每次加载均针对当前 Target 重新资格
审查，并保持 factory 精确要求的规范 Risk ID `message-loss-before-decision`。零模型
Planner 只调用一次，执行 `prepare→promise→Drop accept-sync` 三个计划 Action；随后
factory handoff 并执行四个 closure Action。最终与脚本消融得到相同 Trace/Bundle
digest，同一 RequestID completed、fresh Replay stable、两个 Oracle 0 violation，且
handoff 后没有第二次干预。非匹配 Risk、非 operation-carrying 消息、同层歧义和无候选
也已有独立边界回归。

M4m2O 已补齐 `omnipaxos-client-decision-binding` Target-local Oracle。monitor 只联结
Bundle 中的原始 Invoke、worker decided log 产生的 completed ClientResult payload，以及
同一 participant/index 的 decided-prefix observation；它不读取 Risk、Agent 输出或 PSS。
正常 M4m2R Bundle 现在由 registry 检查 trace-integrity、agreement 和 binding 三个
monitor，0 violation。普通回归覆盖客户端 RequestID、决定内容、请求—决定映射和
decided-prefix witness 篡改；尚未完成的 ClientResult 不产生 violation。OmniPaxos
`client-operation-continuity` 因此从 observable-only 提升为 oracle-backed，不需要修改
公共 Action、Runtime、Bundle schema 或 worker evidence schema。

M4m3 已按预注册镜像顺序完成四个真实 DeepSeek Scenario-only Episode。Risk provider
为 0，总计 10 次 Scenario 调用和 181,093 observed tokens，无传输歧义。target-local
两轮都 Drop enabled 的 operation-carrying `accept-sync n1→n2`，随后发生可信 handoff，
在没有后续模型调用或新干预的情况下直接闭合；同一 RequestID completed，保存的 fresh
Replay stable，三个 registry monitor 均为 0 violation。public-fixed 两轮也机械 reached，
但实际 Drop 是 `prepare` 而非预注册的 operation-carrying leaf。因此本轮应同时报告：
机械 Risk reached 为两臂 2/2；预注册正确干预为 public 0/2、target-local 2/2；handoff
为 target-local 2/2。该差异暴露出现有 Risk 的可执行谓词弱于其自然语言 summary，不能
把 public 的机械 reached 当成同类干预成功，也不能据四轮宣称总体成本/成功率优势。
四个 canonical Bundle、独立 Oracle audit 和完整结果见
`benchmarks/experiments/omnipaxos-closure-pair-m4m3-v1/`。

M4m4 已将上述差异闭合为可执行语义。OmniPaxos Drop Observation 只有在消息是
`accept-sync|accept-decide`、`entry_count > 0` 且携带 RequestID 时，才声明
`message-role=operation-replication` 并暴露该请求；Decision Observation 从实际消息
item dependency 链回溯唯一 RequestID。v2 Risk 用同一 `bind_as=request` 连接 Invoke、
operation Drop 和 Decision。四项普通回归覆盖 prepare、同请求、不同请求和 pending；
对 M4m3 四个保存 Bundle 的离线重投影得到 public 0/2、target-local 2/2。旧 v1 输入和
M4m3 预注册结果保持不变，严格输入新增为
`plans/agent/omnipaxos-message-loss-risk-v2.json`。新 implementation identity 防止旧
`m4m1` Investigation 在相同运行参数下被当前语义恢复。本阶段没有调用模型，也没有运行真实
问题版本；结果见 `benchmarks/experiments/omnipaxos-risk-fidelity-m4m4-v1/`。

saved Bundle Oracle audit 保持 v1 工件兼容：Go 字段仍名为
`RecordedReplayStable`，JSON 继续使用 `replay_stable`。该字段只表示 Bundle 已封存的
fresh Replay 结果；evaluator audit 不重新启动 Runtime。

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
- public saved-Bundle audit 可按 Target registry 重算完整 checked/violation 证据；
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
约 55MB（清理可重建 OmniPaxos worker target 后）；Go 代码 55,884 行，其中测试
19,447 行；`benchmarks/` 当前为 93 个文件、约 1.9MB，新增部分主要是
M4l7--M4m2 的 canonical Bundle、audit 与精简报告。

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

1. 选择一个历史问题版本或受控差异版本，但在单独确认范围后才开始真实验证；
2. 先以零模型 Trace 确认现有 Action/evidence/Oracle 能得到首个非零结果；
3. 再让 Scenario Agent 从已知 Risk 复现同一干预，最后恢复 Risk Agent 完整流程；
4. 扩大 Agent 的源码理解和基于 `ProgressDelta` 的 revise 能力；
5. 设计同预算 Random/单 Agent/双 Agent 对照，再运行长时公开实验。
