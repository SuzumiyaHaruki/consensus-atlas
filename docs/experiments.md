# Equal-budget exploration experiments（历史设计记录）

M5.17bR2 已删除 M5.10–M5.13 的 pre-admission fixed/random/Planner 在线入口。本文前半部分记录早期
实验模型与当时命令，不是当前分支的可运行接口；当前 qualified workload、ExecutionBundle 和强基线见
`CURRENT_STAGE.md`。未来 uniform random 会在共同 admissible frontier 上重新进入，Agent 则使用新的
Guarded TestIntent，而不是恢复旧 Planner Proposal。

M5.17c0 的当前语义与下文历史“必须填满预算”口径不同：decision budget 是上限，
合法运行显式区分 `budget-exhausted`、`quiescent` 和 `configured-stop`；所有 Policy 共享
同一 admissible frontier，pending workload 保留在可重放报告。当前规范见
`stage-m5.17c0-experiment-semantics.md`。

M5.17c1 又补齐了当前方法实验的账本前提：source corpus 顺序参与 identity，重复 ActionID 使用
occurrence 引用，PSS feedback 必须由 bundle 重新投影，MethodLedger 完整计入 source、proposal、失败和
execution。当前规范见 `stage-m5.17c1-corpus-trust-prerequisites.md`。下文旧 measurement model 不应覆盖
这些新约束。

M5.17c2 实现了 qualified admissible-uniform 和第一个 batch PSS-guided consumer。两者共享
3 attempts/294 primary/294 replay ceiling，但 guided proposal 在 decision 66 不可执行，实际只消耗
263/196。因此其 157 states 不能与 uniform 的 238 当作等成本效果排名。当前规范见
`stage-m5.17c2-batch-pss-guidance.md`。

M5.18a 冻结第一个单执行 `MethodSpec`、ExecutionBundle v3 OperationHistory 和 evaluator-owned fresh
execution。公开命令为：

```bash
make build-etcdraft-v2-method-evaluation
make evaluate-etcdraft-v2-method-evaluation
```

评价器不读取 submitted bundle；它校验 audit/binary 后亲自运行 control/candidate。两边 MethodConfig
projection、operation history 和 98/98 work 相同，公开 candidate 在 applied-prefix step 55 被 Agreement
检出。该结果只校准可信链，不是搜索方法比较或 private holdout。完整规范见
`stage-m5.18a-method-evaluation-prerequisites.md`。

M5.18b0 已实现不调用模型的 `ProtocolKnowledgePack -> AgentSemanticView -> GuardedTestIntent
-> CompiledIntentPlan` 宏观边界。compiler 只从真实 Qualification 准入的 backend 中选择，
执行时仍进入本文的唯一 qualified executor。完整规范见
`stage-m5.18b0-guarded-intent-compiler.md`。

M5.18b1 增加一个显式 opt-in、无重试的 `deepseek-v4-flash` JSON transport 和渐进式调用审计。
唯一真实 response 通过 compiler/executor/replay，但精确复制了 prompt 中的具体有效示例，因此只是
transport calibration，不参与方法效果排名。完整规范见 `stage-m5.18b1-one-shot-agent-transport.md`。

M5.18b2 从完整 bundle 和绑定 Mapper 重算 action-class/uniform 两个 seeds 1/2/3 batch，
生成只含 common ceiling、完成度、成本和 coarse PSS discovery 的 defect-blind feedback。
action-class 的第三次执行失败保留成本且不用替换 seed。feedback 只能改变 intent preference，
不能改 hard constraints 或预算。完整规范见 `stage-m5.18b2-defect-blind-batch-feedback.md`。

M5.18b3 发现上述 v1 对比混用了严格 workload failure 与 Experiment v2 pending 语义，因此保留旧
identity 但禁止用它排名。修正后的 feedback v2 让两种方法都以 Experiment v2 执行，并冻结
`source seeds 1/2/3 -> unseen seed 4`、source/per-arm 成本和无替换失败规则。deterministic seed-4
follow-up 通过 replay，但 workload pending 且 hard ActionKind 不完整，原样记为计费失败。完整规范见
`stage-m5.18b3-unseen-follow-up.md`。

M5.18b4-pre 修正三个评价边界：backend 只声明当前 Experiment 可产生并可选择的 Action；宏观
`CompiledIntentPlan/v2` 不再绑定 seed；有效执行未覆盖 hard Action 时单独记为 intent
`not-reached`，不再伪装成 execution failure。seed-4 见证为 execution valid、missing invoke、Oracle
not-evaluated，且模型调用数为 0。完整规范见 `stage-m5.18b4-pre-trust-corrections.md`。

M5.18b4 request-freeze 在任何 key/transport 之前冻结两臂精确 prompt/request bytes。两臂只允许
feedback 暴露不同，共用 hard baseline、seed 4、98/98 execution ceiling、每臂 1 call 和 0 retry。
单份 proposal 未来必须分别与 baseline 校验，不能只做 pairwise 比较。本阶段 model calls=0，
完整规范见 `stage-m5.18b4-request-freeze.md`。

M5.18b4R 不改变上述实验。它只把 `cmd/control-experiment` 的完整 race 回归按共享 fixture 拆成
独立 test binary，并用版本化清单与 `go test -list` 机械验证全部顶层测试恰好归类一次。
`test-race-core` 是开发期信号，只有全部 shard 和其余 package 都通过的 `test-race-full` 才是完整门禁。
完整规范见 `stage-m5.18b4r-race-gate-topology.md`。

M5.18b4 frozen request consumer 只消费上述 freeze 中已经绑定的 exact prepared bytes。单臂经过一次
transport、既有 response audit、strict parse、hard-baseline validation、plan v2、seed-4 instance、
qualified executor 和 IntentOutcome；没有 retry、reply repair 或 backend/seed fallback。当前只使用
离线 mock 验证成功和失败边界，没有 CLI 或真实模型调用。完整规范见
`stage-m5.18b4-request-consumer.md`。

M5.18b4 pair orchestration 固定按 no-feedback/with-feedback 顺序各消费一次冻结 request；第一臂失败
不阻断第二臂。pair ledger 对每臂分别绑定 invocation audit、可选 execution/outcome digest，并把共享
source 成本完整计入两臂。结构化工件和 exact prompt/request bytes 只写入全新目录；篡改 ledger、
覆盖目录、key 落盘和 transport 私有诊断泄漏均由离线测试拒绝。本阶段没有 CLI 或真实模型调用，
完整规范见 `stage-m5.18b4-pair-ledger.md`。

M5.18b4 explicit pair runner 新增手动 opt-in CLI/Make 入口。它先拒绝已存在 artifact path，
构造并完整复核 freeze 后才读取用户指定的 key。pair 返回 typed arm failure 时先持久化
ledger 再返回非零错误。入口不被任何测试目标依赖；本阶段只用离线 mock，真实模型调用为 0。
完整规范见 `stage-m5.18b4-pair-runner.md`。

M5.19 先定义协议无关 CampaignConfig、terminal attempt 和增量 checkpoint chain。wall clock 只是
运维上限，逻辑比较仍按 attempts、decisions、primary/replay work 和 model cost。该阶段没有运行
SUT 或模型，也没有新增实验结果；完整规范见 `stage-m5.19-campaign-foundation.md`。

M5.19a 将上述对象落到 artifact-first、checkpoint-second 的 crash-safe 目录，并验证完整恢复、
artifact-only 中断、stale head、篡改、缺链和 symlink 边界。该阶段仍没有运行 SUT 或模型，也没有
新增实验结果；完整规范见 `stage-m5.19a-campaign-persistence.md`。

M5.19b 用协议无关 deterministic provider 验证多 attempt Coordinator：attempt 1 后恢复目录，再继续
attempt 2/3；terminal failed attempt 保留 artifact/成本，最终由 attempt limit 停止。另有 logical 和
wall-clock stop、超 allowance 与无重试负例。该阶段没有运行 SUT 或模型，不是方法效果实验；完整规范见
`stage-m5.19b-campaign-coordinator.md`。

`cmd/experiment` compares schedule-search methods above the same deterministic Runtime and below the same PSS projector. It does not let an explorer mutate protocol semantics, Oracle logic or coverage denominators.

## Measurement window

An experiment has two phases:

```text
deterministic setup                 measured exploration
start/bootstrap/drain/inject   --> scheduler decisions
not charged                         charged one-for-one
```

The setup scenario is replayed for every run. All runs must have the same setup execution fingerprint and the same canonical PSS root state. The report stores a `measurement_window.id` derived from Profile ID, PSS ID and setup fingerprint. Reports with different window IDs must not be compared directly.

The canonical root state is included at decision budget zero. Setup transitions are not included in the discovery curve.

## Budget model

Three limits are explicit:

- `decision_budget`: total charged decisions across all runs;
- `budget_per_run`: maximum path depth before starting from the root again;
- `max_runs`: upper bound preventing repeated quiescent runs from continuing forever.

Every execute, message drop or message duplicate action produces exactly one trace record and costs one scheduler decision. The experiment fails if that one-to-one relation is violated.

The report distinguishes:

- `decision_budget_reached`;
- `run_limit_reached`;
- `search_space_exhausted`.

Only reports that reached the same target decision budget, or curves truncated to their common charged prefix, should be compared as equal-budget results.

## Baselines

### Random

Each run uses a published seed derived deterministically from the base seed and run number. Candidate selection is uniform over the current action list. Repeating the experiment with the same inputs must reproduce decisions and full execution fingerprints.

### DFS

DFS chooses candidates in deterministic Runtime order, runs a path, then replays from the shared root and increments the deepest choice that still has an alternative. It never clones an in-memory SUT state. This makes it applicable to native systems that only support restart/re-execution.

The first DFS baseline is intentionally naive: it does not yet implement sleep sets, state pruning or DPOR. Its role is to provide a transparent systematic baseline.

## Action policy

Execution of enabled events is always available. Optional message actions are:

- drop a pending message;
- duplicate a pending message.

Duplication requires `max_duplicates_per_run > 0`. This prevents an unbounded action from silently creating an infinite branching factor. Crash/restart actions are injected declaratively by the setup scenario and then become enabled according to Runtime state.

## Historical running interface（已删除）

```bash
make experiment-random
make experiment-dfs
make experiment-etcdraft-v2
make experiment-etcdraft-v2-random
```

Equivalent direct invocation:

```bash
make auto-onboard-etcdraft
go run ./cmd/experiment \
  -profile artifacts/onboarding/etcdraft-profile-v1.json \
  -setup scenarios/etcdraft-explore-setup.json \
  -strategy random \
  -runs 64 \
  -budget 64 \
  -decision-budget 128 \
  -seed 1 \
  -drop-messages=true \
  -out artifacts/etcdraft-random-experiment.json
```

`-verify-replay=true` is the default. It creates a fresh setup root and forces each recorded action/event ID without asking Random or DFS to choose again; candidate count, choice position, duplicate ID, termination, conformance and strict fingerprints must match. The report contains setup evidence, every decision, per-run trace, Oracle result, pending events, the cross-run PSS state union and first-discovery witnesses.

M5.10 的 `make experiment-etcdraft-v2` 曾使用新 Control Runtime path 并写入 public development
report. It currently runs only the transparent progress/lifecycle policies; it does not reuse the legacy Random/DFS
implementation or its Oracle/Coverage claims.

M5.11 的 `make experiment-etcdraft-v2-random` 曾复用该 executor 和 public base policy seed `1`。
Policy entropy is derived independently per run and never replaces the Runtime seed. The fixed and Random reports
have equal primary/replay logical budgets; a single-seed state-count difference is not a method-level conclusion.

M5.12 的 `make experiment-etcdraft-v2-stub-planner` 曾在 trusted Scope 下编译 restricted proposal，再
reuses the same executor. The public stub makes zero model calls and intentionally reproduces M5.10's fixed behavior;
its value is the scope/compiler/failure-accounting boundary, not a strategy improvement. Rejected proposals and
runtime-unreachable rules are explicit attempt outcomes and cannot disappear from the proposal-attempt budget.

M5.13 的 `make experiment-etcdraft-v2-deepseek-planner` 是一次已冻结的 one-call model smoke。其 public
result compiled successfully but stopped at an unreachable exact message rule after 34 primary and 32 replay
decisions. It therefore has no complete PSS summary and is not comparable to the complete fixed/random reports.

## Area metrics

`prefix_area` is the discrete sum:

```text
sum from b=1 to B of D(b)
```

`self_normalized_area = prefix_area / (B * D(B))` describes how early a method found its own final set. It must not be used alone to compare absolute discovery strength: a method that quickly finds five states can have a higher self-normalized area than one that gradually finds thirteen. Always report final unique states and raw prefix area at the same budget.

A future comparison tool may normalize against the union of all evaluated methods, but that post-hoc reference set is still not a completeness denominator.

M5.9d 已将这一聚合算法改为协议无关的 measured decision/optional sample 输入。legacy
Random/DFS 继续由 compatibility adapter 产生相同 JSON；etcd/raft v2 已有两个各 32 decisions 的
集成见证。M5.10 又增加 `cmd/control-experiment`，把相同固定策略见证保存为 digest-bound v2
`measurement-complete` 报告，明确记录 primary/replay setup、decisions、work units 和未采集的资源
成本。M5.11 又在相同执行路径和预算上增加独立 policy seed 的确定性 Random 报告。M5.12 在该路径前
加入受限 Proposal 的机械编译和失败计费，并以 0-call deterministic stub 保存首个 PlannerAttempt。
M5.13 又保存第一次真实单调用 PlannerAttempt，但它以 `execution-failed` 结束。单个公开 seed 或单次
Agent 失败都不构成 Random/DFS/Agent 方法优越性结论。

## Current limitations

- DFS has no state memoization, DPOR or conservative independence rules.
- Setup replay cost is excluded from the scheduler-decision budget; wall-clock/CPU and Agent token cost still need a separate cost report.
- The current workload has one bounded campaign and crash/restart opportunity; workload-family generation is not implemented.
- A state-discovery curve does not cover different paths to the same state; transition, ordering and fault obligations remain separate.
