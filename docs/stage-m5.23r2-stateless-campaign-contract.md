# M5.23R2：Stateless Campaign 方法契约与第二批主线收缩

日期：2026-08-12

状态：完成

## 为什么需要这一阶段

M5.23g 已证明 restricted Agent 可以进入 exact-prefix Stateless Search，但它仍是一次专用 pilot；
旧 Campaign 的 attempt 输入则是宏观 `GuardedTestIntent -> planned attempt`。如果直接开始 M5.24，
Agent、canonical 和 uniform 会走不同的持久化/计费路径，重复试验也可能在运行中替换 seed 或 root，
无法形成可信比较。

本阶段只解决一个问题：把所有 Stateless Search 方法放进同一个可恢复 Campaign attempt 契约。
不调用模型、不构造 private pair、不改变 M5.23g 结果。

## 冻结对象

`StatelessCampaignSpec/v1` 绑定：

- target ID 与 opaque target identity digest；
- 完整预声明的 `attempt_methods` 序列；
- source bundle、source Manifest 与 root corpus digest；
- source WorkLedger；
- root 数、depth、每 root work-item/search-work ceiling；
- per-attempt primary/replay/model 预算；
- source 成本全额计入每次 attempt 的政策；
- strict Replay 与 read-only discovery 必须开启。

一个 spec 只能包含一种 strategy。canonical 允许重复，以表达同方法重复稳定性；uniform 必须把每个
seed 封进各自 method identity；Agent method 绑定 planner/knowledge digest，但不获得额外 Campaign
Planner 权限。

## Attempt 处理

```text
CampaignAttemptRequest
          |
          v
spec.attempt_methods[ordinal-1]
          |
          v
target-owned Stateless runner
   | completed                 | failed
   v                           v
sealed discovery          typed failure
   + search work              + partial real work
   + qualified work           + model work if dispatched
   + model work
          |                           |
          +-------------+-------------+
                        v
          StatelessCampaignAttempt/v1
                        |
                        v
        existing content-addressed Campaign store
```

artifact 自包含验证 sealed discovery 的结构、root 累加成本、PSS set digest、request/spec/method binding
及总 WorkLedger；target composition 在进入 artifact 前仍须执行更强的
`Validate(corpus, source, evidence, mapper)`。因此 durable 层不需要保存全部 trace，可信执行层也不能只
提交一个未经重算的 PSS 数字。

## 成本规则

每次 attempt 的总成本是：

```text
完整 source bundle 构造
+ exact-prefix frontier reconstruction / child materialization / child verification
+ 每个 WorkItem 的 qualified primary + fresh Replay
+ Agent provider call/tokens（仅 Agent）
```

source 可以物理复用，但逻辑比较对每个方法全额收费。qualified execution work 的 model 栏必须为零，
防止模型成本重复或隐藏计费。Agent transport 在 dispatch 后即计 1 call；如果 provider 未返回 usage，
tokens 可以为零，但 completed Agent attempt 必须具有正 token usage。

## 冻结 evidence 迁移

定向集成测试读取 M5.23g 已提交的 source/corpus/summary/Agent discovery，重新生成相同 Agent method，
建立 planner-free Stateless Campaign config，并构造一个严格验证的 attempt artifact。验证结果保持：

- 6 model calls；
- 28,335 model tokens；
- 18 qualified executions；
- 15 corpus-novel PSS；
- Agent discovery digest 不变；
- 新模型调用数为 0。

这证明旧 pilot 可以进入新账本，不证明 Agent 有效果。

## 第二批收缩

机械 `rg` 依赖审计发现，`CrossTargetPlannerView/CampaignLedger` 只被 M5.22 自身生产组合与测试引用；
当前 CLI、Stateless Search、Campaign durable core、formal evaluator 均不依赖它。该闭包因此从 HEAD 删除，
历史文档、benchmark 与 Git 检查点继续保留。

删除时发现 M5.23b OmniPaxos 迁移测试复用了 M5.22b 的 target fixture。该 fixture 已改写为
M5.23b target-local qualification/workload/policy helper，定向测试继续匹配已提交 summary，说明
Stateless Search 的第二 target 门禁未被删除。

旧 Campaign Planner 不能在本阶段删除：`campaign-etcdraft-v1` 和 `campaign-etcdraft-agent-v1`
仍依赖 planned-attempt/model-call persistence。下一阶段先提供 Stateless target runner，再替换该入口。

## 验证与边界

- 通用测试覆盖 Campaign create/step/recover/run、method ordinal、source 重复计费、strict JSON、
  discovery tamper、model-cost smuggling 和 Agent partial failure。
- M5.23b OmniPaxos 与 M5.23g frozen-evidence 迁移均有真实 target composition 定向测试。
- Race shard manifest 重新与全部顶层测试精确对齐；Makefile 不再暴露已经退休的 Agent target。
- `go test ./...` 通过，其中 `cmd/control-experiment` 用时 201.341 秒；`go vet ./...` 通过。
- Stateless Campaign 内核定向 race 用时 1.139 秒；provider/key、M5.23b 迁移与
  M5.23g 契约迁移组合定向 race 用时 49.647 秒。
- `audit-no-v1`、`audit-no-retired-experiment`、`audit-race-shards` 和 `git diff --check` 通过。
- 未读取 key、未调用 LLM API、未改动 raft 工作区或冻结 benchmark JSON。

本阶段没有 candidate verdict、方法排名或 Coverage 百分比。`QualifiedExecutionAttempts` 只表示真实执行的
work-item 数，不是状态空间分母。

## 下一步

M5.23R3 实现 etcd/raft target-local Stateless Campaign runner，先运行 canonical/seeded-uniform，证明
真实 corpus/search/discovery 可以跨进程恢复。Agent 后续只替换同一个 order provider。旧 Campaign
Planner 只有在这两个现行入口被替换后才能机械删除。
