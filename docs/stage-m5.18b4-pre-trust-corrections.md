# M5.18b4-pre Agent Experiment Trust Corrections

日期：2026-08-09

## 阶段目标

在冻结 M5.18b4 的 no-feedback/feedback 模型请求前，先消除三个会让结果难以解释的
可信边界缺口：

1. Agent catalog 将 Runtime 能表示的 Partition/Heal 误写成当前 Experiment 可产生；
2. 有效执行未覆盖全部 hard ActionKind 时被记成 `execution-failed`；
3. CompiledIntentPlan 保存 catalog seed 1，follow-up 却实际使用 frozen seed 4。

本阶段不调用模型，不修改 Control Runtime，不实现 RiskWitness/FaultProvider，也不改写
M5.18b0/b1/b2/b3 的历史对象与 digest。

## 三层 Action capability

```text
Runtime-supported
  Action 能被 Runtime 表示和执行
            |
            v
Experiment-producible
  当前 workload/fault producer 能将 Action offer 到 frontier
            |
            v
Backend-selectable
  某 backend 能从该 frontier 中选择 Action
```

当前 crash/restart、message delivery/drop/duplicate 和 natural temporal 同时满足三层。Partition
只满足 Runtime-supported；`executeRun` 没有 FaultProvider 调用 `OfferPartition`。因此新 b4
catalog 必须删除 Partition/Heal，并将 partition envelope 上限设为 0。Manifest 和历史
catalog 保持不变。

## 独立结果轴

```text
Execution validity
  report/bundle/replay/admission 是否有效

Intent reachability
  该有效 Trace 是否满足冻结 intent 的当前 hard Action 前置

Oracle outcome
  已注册 property monitor 是否报告 violation
```

新 `IntentOutcome` 只在 report/bundle 先通过完整验证后生成，所以其 execution 固定为
valid。hard Action 缺失产生 `intent-not-reached` 和稳定 reason code，不返回 Runtime/replay
failure。RiskWitness 尚未实现，因此 reached 仍只表示 Action 前置满足，不表示
`leader-change-with-inflight-proposal` 时序已被证明。Oracle 仍由独立 evaluator 所有。

## Plan 与 execution instance

```text
CompiledIntentPlan/v2
  risk + backend + strategy + hard constraints + compiler work
                         |
                         v
IntentExecutionInstance/v1
  plan digest + trusted policy seed + one-execution budget
                         |
                         v
existing qualified executor + report seed verification
```

Agent 不提交 seed。编译结果可在多个预先冻结 seed 上实例化，每个 instance 独立
digest-bound。新 v2 plan 不写入 policy seed；v1 plan 继续服务历史 identity 回归。

## 验收标准

- 历史 b0/b1/b2/b3 identity 测试不变；
- 新 b4 Agent view 的 backend 不含 Partition/Heal，partition envelope 为 0；
- 对 Partition 的 hard/preference 不会被虚假宣称为 backend-selectable；
- v2 plan 不包含实际 seed，execution instance 机械绑定 seed 4 和成本；
- seed-4 已知轨迹被分类为 execution valid + intent not reached，历史 b3 summary 不回写；
- 不读取 key，不产生模型请求，不增加第二执行器；
- `internal/controlexperiment` 仍不导入具体协议实现。

## 实际结果

修正后的 Agent view 仍从 Manifest 展示 Runtime-supported Partition/Heal，但 action-class、
uniform 和 fixed-progress 的 backend surface 均不再包含它们。前两者保留
crash/restart/drop/duplicate 上限，fixed-progress 的全部 fault envelope 为 0。提交
hard Partition 的 v2 intent 以 `EXPERIMENT_INTENT_HARD_CONSTRAINT_UNSATISFIED` 被 compiler
拒绝，而不是等到执行后必然失败。

`CompiledIntentPlan/v2` 的 JSON 不再含 `policy_seed`；`IntentExecutionInstance/v1` 独立
绑定 seed 4、1 attempt 和 98/98 ceiling。etcd/raft composition 另外验证 report 的 native
policy seed 编码与 instance 一致，把 instance 改为 seed 5 会得到稳定
`ETCDRAFT_B4_PREFLIGHT_SEED_MISMATCH`。

新 b4 action-class strategy 使用同一 qualified executor，只将实际 Config 的 partition
envelope 收紧为 0。seed 4 仍消耗 97/97 work、strict replay 稳定并发现 79 states；
真实 Trace 缺少 `invoke`。新结果因此是：

```text
execution_status = valid
intent_status    = not-reached
reason_code      = INTENT_REQUIRED_ACTION_NOT_REACHED
missing_actions  = [invoke]
oracle_status    = not-evaluated
model_calls      = 0
```

这不是 defect verdict，也不是 RiskWitness。它只证明新数据模型不再把一条有效但未到达
intent 前置的轨迹误称为 execution failure。

feedback 的六条 source bundle 物理复用 M5.18b3 的冻结工件，因此其历史 Config 仍记录
partition 上限 1；它们由没有 FaultProvider 的同一执行 composition 产生，测试另外逐条验证 Trace
不含 Partition/Heal。b4 follow-up 的新 Config 与 catalog 则都明确收紧为 0。这个复用只避免重复
生成轨迹，不能表述为 source 与 follow-up 的 Config identity 相同；两个未来 b4 arm 必须逐字节共享
同一组 source，且方法账本仍完整计入其成本。

## 冻结身份

- catalog：`2effd56b...94f861`；view：`d20b9fea...2e036`；
- feedback：`94b0862b...c66bf`；intent：`fc28590c...d620`；
- plan v2：`4b263fd4...26e87`；execution instance：`2bbf81a1...3b5ea`；
- intent outcome：`be4779d5...9a980`；
- report：`7d7c1765...e8c1e`；bundle：`373f6740...96b7`；
- summary：`362afa36...e649`。

小型可重算工件位于
[`benchmarks/experiments/etcdraft-v2-agent-b4-preflight-m5.18b4-pre/`](../benchmarks/experiments/etcdraft-v2-agent-b4-preflight-m5.18b4-pre/README.md)；
完整 report/bundle 位于 ignored `artifacts/`。

## 当前没有证明

- ActionKind 集合 reached 仍不证明 `leader-change-with-inflight-proposal` 的时序；
- 没有 FaultProvider，Partition/Heal 仍不属于 Experiment-producible；
- 没有执行 b4 的任何模型请求；
- 没有 Agent 效果、PSS 完整度、private holdout 或第二 strict target 结论。

## 验证

- `make test-fast`、`make test`、`go vet ./...`、`make test-race-full`：通过；
- `cmd/control-experiment` 普通测试本轮为 89.598/94.288 秒，race 为 590.400 秒，低于 20 分钟上限；
- 144 个非 artifacts JSON、23 个 schema JSON、4 个 build schema 实例与 139 个 Markdown 本地链接通过；
- checked-in 小巧工件与 ignored 完整工件逐字节一致；两份总体规划逐字节一致；
- `internal/controlexperiment` 的 dependency closure 不含 etcd/Raft/HashiCorp；
- 历史 b0/b3 identity 回归不变，`git diff --check` 通过；
- 代码规模为 21,963 行 production + 8,284 行 tests = 30,247 行；相比 b3 净增
  703/196，未增加 Runtime、PSS、Oracle、Risk DSL 或 backend。

模型调用数始终为 0。

## 后续顺序

pre 门通过后再冻结 b4 两个 request。b4 只报告 backend-preference feedback micro-ablation。
紧随其后实现 target milestone projection + family frozen partial order 的最小 RiskWitness，不扩展开放 DSL。
