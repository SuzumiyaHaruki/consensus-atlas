# M4.12 阶段总结：Blind Planner Agent v1

日期：2026-08-06
状态：开发期闭环完成，尚未运行模型实验

## 结论

Blind Planner v1 让不可信 Planner 只接收 opaque trial ID、冻结 Profile 的身份/节点投影、
Driver 声明的受限输入形状、supported capability 名称投影、opaque Coverage Debt、受限 Test
Plan DSL、剩余预算和机械化 finding。Planner 提交的 target 只能是
`debt-<sha256>` 引用；可信 Coordinator 在 Runtime 之前将其解析为私有真实 obligation ID。

本阶段没有读取 `key.txt`，没有调用任何模型服务，也没有将 M4.9/M4.11 当作 Agent 效果证据。

## 已完成的可信边界

1. 新增 `BlindScope`：要求 benchmark ID、64 位 digest 和 opaque trial ID，由实验组合根提供；
   其用途是将同一 Planner API 请求绑定到已冻结的实验范围，而非公开 candidate 身份。
2. `BlindCapabilitySnapshot` 只保留 supported capability ID 及该投影的 digest；不传递
   Driver 名称、SUT 名称/版本、capability detail 或 build identity。
3. `BlindDebt` 只暴露 opaque ref、category、risk、ledger status 和 attempts；不暴露
   obligation ID、描述、predicate、monitor、requirements、witness 或 trace evidence。
4. `BlindFinding` 只返回稳定 code、target refs 与计数；不返回执行错误文本、Oracle 文本、
   trace、真实 ID 或 SUT 信息。
5. `CoordinateBlind` 独立建立 hash-chained Blackboard，记录的也是 blind request/proposal/
   finding/execution projection。JSON `BlindReport` 不序列化私有 Campaign Report；后者仅供
   可信本地调用方计算成本和写入私有评测工件。
6. `BlindCommandPlanner` 使用独立 Go 类型和严格 JSON 子进程协议；不继承父进程环境。
   `cmd/agent-campaign` 现在要求显式传入 `-blind-benchmark-id`、
   `-blind-benchmark-digest`、`-blind-trial-id`，因此不会在没有冻结范围时意外发起请求。

## 本地无模型验证

fixture 证明了以下性质：

- Planner request 和可持久化 blind transcript 均不含私有 SUT/Driver 标识、真实 obligation ID、
  描述、requirements、monitor 或 evidence 值；
- 直接提交真实 ID 或未知 target 会得到 `proposal_rejected`，不进入 Runtime，也不消耗
  coverage attempt；随后提交有效 opaque ref 可由可信 Ledger 取得真实强证据；
- 盲化 subprocess 只能收到 `BlindGenerationRequest`，且仍保留 request/response digest 与 token
  审计；
- Blackboard hash 链可重验。

## 尚未证明

- Opaque ref 隐藏的是 benchmark trigger/身份，不会自动消除 Agent 从公开 Profile、协议知识、
  capability 名称或反复机械反馈中推断高层语义的可能；正式 holdout 还需要独立的私有
  manifest/identity 映射和预注册的 exposure audit。
- 无模型 fixture 只证明接口和拒绝路径，不证明 DeepSeek 或任意 Planner 能提高 Coverage、PSS
  或独立 root-cause detection。
- 当前是单 Planner。Scenario/Critic 的增加仍必须由正式 holdout 的明确失败类型支持。

## 下一步

先冻结至少一批彼此独立、对 Planner 不公开 candidate identity/trigger 的 qualified trial 与正确
control，并生成对应 blind scope。在不改变 Runtime、Oracle、Profile denominator 或 evaluator 的
前提下，以固定 decision/token/clock budgets 对比 Random、DFS、专家计划、无反馈 Planner 和
Blind Planner。公开 M4.9/M4.11 只可用于请求协议和执行链调试。

## 2026-08-06 全面检查与整理

本阶段后完成一次全库可读性与一致性检查：

- 新增 `docs/CURRENT_STAGE.md` 作为新窗口入口，`docs/README.md` 以当前阶段、设计、实现和
  历史实验分类导航；
- `internal/agentcampaign/doc.go` 明确 Blind Planner 的唯一模型边界；
- `cmd/agent-campaign` 的默认 campaign ID 和输出名改为 `deepseek-etcdraft-blind-v1`；
- 未移动或删除 `benchmarks/pilots/`、候选 catalog、压缩历史报告或 ignored `artifacts/`，以避免
  破坏 digest-bound 引用和用户本地输出。

检查通过：Go unit/race/vet、Python Agent tests、所有 versioned JSON 的语法检查、本地 Markdown
链接检查、generic `internal/` package 不依赖 etcd/raft module、Blind CLI `-help` 与 Make dry-run、
两份总体规划的字节一致性以及 `git diff --check`。这些检查证明目录入口、边界和已实现代码的
一致性，不证明模型效果或正式 holdout 的保密性。
