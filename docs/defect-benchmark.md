# Defect Benchmark 与完整执行成本

本阶段解决一个比继续增加义务或 Agent 更基础的问题：ConsensusAtlas 自己定义的
Coverage/PSS 指标，不能再反过来单独证明 ConsensusAtlas 有效。正式实验首先回答：

> 在相同完整执行预算下，一个方法能否稳定检出它事先看不到的独立缺陷根因，同时不在正确版本上误报？

义务覆盖和 PSS 状态发现仍然保留，但定位为解释变量；它们不会直接写入 defect kill
结论。

## 可信边界

```text
private Manifest (variant/root cause/source/SUT digest)
                |
                +--> Blind Manifest (only opaque trial IDs + shared budget)
                                      |
                       Agent/search method runs Campaign
                                      |
                    report + BuildAudit + binary + Profile/plans
                                      |
             trusted evaluator reruns digest-bound binary
                                      |
private Manifest + trusted monitors -> Defect Ledger -> evaluation report
```

Agent 只能看到 benchmark/profile identity、统一预算和无语义的 `trial_id`。以下内容
在实验结束前都属于 evaluator 私有数据：

- control/defect 类型；
- 历史修复或 mutant patch；
- 触发测试与缺陷类别；
- `root_cause_id`；
- 源码与 SUT Manifest digest 的对应关系。

私有 Manifest 还包含随机 `blinding_nonce`，使公开 benchmark digest 不能被低熵
缺陷目录直接枚举反推。归档 Benchmark/Submission schema 位于 `benchmarks/schema-v1.json` 与
`benchmarks/submission-schema-v2.json`；新的正式 Manifest 使用 `benchmarks/schema-v2.json`，
将 Family/PSS identity 固定进私有与 blind 视图，evaluator 不再依赖独立的 PSS 命令行参数。
Build spec/audit 的 v1/v2/v3 schema 按输入或 audit
版本保留（旧 schema 不会放宽去接受新格式）。`internal/defectbench` 负责 canonical digest、
盲测视图、预算检查和 root-cause 账本；`cmd/defect-eval` 是可信 evaluator 入口。

## Kill 判定

一个 defect 只有在 Campaign report 同时满足以下条件时才算 `killed`：

1. protocol、Family、Profile digest 和私有 SUT/BuildAudit/binary digest 完全匹配；
2. evaluator 亲自重跑已验证 binary，结果与提交的 Campaign v2 report digest 相同；
3. report 没有超过冻结预算，且 Ledger witness 与完整成本可由 trace 重建；
4. 对应 run 严格重放稳定、Driver conformant 且无执行错误；
5. evaluator 从重跑的 setup + measurement trace 重新执行私有 Manifest 允许的可信
   Oracle，并得到违规。

Campaign report 内保存的 Oracle 结果不是 kill 权威；evaluator 会重新计算。Coverage
得分、Agent 声明的 target、PSS 状态数和自然语言解释均不会产生 kill credit。

同样证据出现在 control 上时记为 `false-positive`。身份不匹配、旧报告、成本字段
不一致或超预算记为 `invalid`，不能按 survived 或 killed 混入有效分母。同一根因的
多个 mutant 可以用于稳定性检查，但最终 root-cause kill rate 只计一次。

## 完整成本

Campaign report v2 把 primary 与 replay 分开，并为每个阶段记录：

- `setup_attempts`：创建 fresh SUT 的次数，包括失败尝试；
- `setup_steps`：重复执行的 bootstrap/prepare/stimuli 场景步骤；
- `setup_runtime_events`：这些步骤实际执行的 Runtime events；
- `measurement_events`：Explorer 实际决策/事件；
- `work_units`：fresh SUT 尝试、每个场景步骤或其批量 Runtime event，以及每个
  measurement event 的确定性总费用。

`work_units` 是跨机器稳定的逻辑执行量，不是 CPU 或 wall-clock 估计。正式搜索比较
以 `primary.work_units` 为首要统一预算；run、scheduler decision、replay work、模型
token、CPU 和 wall clock 独立报告。公共且每个方法只执行一次的初始化可以明确排除，
但每个 run 重复的 bootstrap/prepare 不能免费。

Agent Coordinator 目前仍以 run/decision/token 限制在线生成；Defect Benchmark 会
额外拒绝超过 `max_primary_work_units` 的 trial。因此旧的 decision-only 实验可用于
历史分析，但不能作为新 benchmark 的公平主结果。

## 最小正反例

`internal/defectbench/defectbench_test.go` 运行真实的通用 Campaign，而不是直接伪造
kill 结果：

- control 在同一 index 两次提交相同值；
- fixture mutant 在同一 index 提交两个不同值；
- 两者命中完全相同的 Coverage obligation，Coverage 均为 100；
- evaluator 只 kill mutant，control 无误报；
- 两个 mutant 表象共享一个 `root_cause_id`，最终只形成一个根因。

该测试刻意证明“覆盖分相同而外部结果不同”，从代码层切断 Coverage/PSS 与 kill 的
循环定义。

## Candidate 资格与受控构建

公开 Candidate Catalog 只声明 provenance/source/root-cause 元数据与六类 typed
requirements，不包含 curator 填写的资格状态。可信 CapabilitySnapshot 将 controllable
inputs、observable events、Driver capabilities、trusted monitors、execution outcomes、Family 和
Profile bounds 分开；尤其不能从 Coverage obligation 的 event kind 推导 controllable
input。`internal/defectbench` 只做逐类集合包含判断，QualificationReport 是唯一包含
`qualified/deferred` 的工件。

`internal/sutbuild` 对本地官方 Go module 做 path/version、目标 source digest、完整 module
tree digest、唯一转换、转换结果 digest 和 command allowlist 检查。模块通过稳定的仓库相对
staging identity 和 readonly local replacement 构建，不改 module cache，也不把随机临时路径
写入 binary build info。digest-bound opaque SUT identity 由 `ldflags` 写入 Campaign Manifest；
build audit 保存 source/module tree、binary、command 和 toolchain identity。

## etcd/raft calibration 与第一个历史回归样本

`benchmarks/pilots/etcdraft-calibration-v1/` 保存首个真实 etcd/raft 构建/执行管线校准；
`etcdraft-calibration-v2/` 是 M4.8.1 的 artifact-bound trusted-rerun 版本。
公开 command-data calibration 仍只证明管线连接：正确 control 与 calibration 均执行 41 decisions、
127 primary/127 replay work，并得到相同 21/55、38.67 Coverage；evaluator 从保存 trace
重算 Agreement，得到 control-pass/killed、一个 calibration root cause、零误报和零 invalid。

`benchmarks/pilots/etcdraft-readindex-v1/` 是第一个公开历史回归复现。M4.9 后，四个
官方候选中仅 `63903dd` 通过六类 typed requirement 的机械资格；未修改 v3.6.0 candidate
在冻结的延迟 ReadIndex response 计划下被 `linearizable-read` 检出，而精确修复 control
通过。二者均为 1 run/1 decision、218 primary/218 replay work、26/55（45.67）Coverage，
最终为 1/1 root cause killed、0 false positive、0 invalid。evaluator 曾因 Coverage 浮点
累计顺序造成 report digest 差异而拒绝重跑；排序修复、重建二进制和再次重跑后才写入最终
报告。这一复现不构成 Agent、Coverage 或 PSS 的效果结论，也不能当作盲测方法比较。

`benchmarks/pilots/etcdraft-ready-must-sync-v1/` 是第二条独立 monitor/evaluator 链，但它
属于公开历史语义重构，而不是完整历史 checkout 复现。candidate 在当前 v3.6 module 上精确
反向转换 `Ready.MustSync` 条件，control 是未修改官方 v3.6.0；opt-in Runtime Profile 绑定
`conditional-ready-sync` 与 `ready-must-sync-observation`。两者均执行 1 run/1 decision；
candidate 为 105 primary/105 replay work，control 为 100/100，Coverage 同为 2/3（75.00）。
evaluator 重跑并从 trace 重算 `ready-must-sync`，得到 1/1 semantic root cause killed、
0 false positive、0 invalid。该样本验证新的 Ready/持久化策略评测链，不能替代多样本
holdout 方法比较。

## CLI 流程

由 benchmark curator 在隔离环境中从私有 Manifest 生成 Agent-facing 视图：

```bash
go run ./cmd/defect-eval \
  -manifest /private/benchmark-v2.json \
  -blind-out /runner/blind-v1.json
```

在把 `blind-v1.json`、Planner request/transcript 或 submission 交给 runner 前，curator 必须在
私有环境运行 exposure audit。它将 blind manifest 自动作为第一个公开工件，`-public` 可重复传入
未来会对 Agent/runner 可见的 JSON 快照：

```bash
go run ./cmd/blind-audit \
  -manifest /private/benchmark-v2.json \
  -blind /runner/blind-v1.json \
  -public /runner/planner-request.json \
  -public /runner/planner-transcript.json \
  -out /private/exposure-audit-v1.json
```

审计重算 Blind Manifest，并检查 public JSON string atom 是否等于 private variant ID、root cause、
category、source/SUT/build digest 或 private kill monitor（包括 JSON object key）。报告只保存工件 digest 和稳定 finding code，
不回显匹配值。它不能替代 runner 隔离或防止模型根据公开协议知识推断语义。

使用 Blind Planner 的 runner 对每个 opaque trial 产生公开 blind transcript、私有 Campaign v2
report 和私有 `TrustedReplayBundle`。受控 build 应构建 `./cmd/blind-replay`；submission v2 的
`plans` 指向该 bundle，`binary` 指向相同 candidate/control 的 blind-replay binary。可信环境
完成评价：

```bash
go run ./cmd/defect-eval \
  -manifest /private/benchmark-v2.json \
  -submission /runner/submission-v2.json \
  -out /private/evaluation-v1.json
```

输出同时包含 defect variant、独立 root cause、controls/false positives、invalid
trials、完整 primary/replay work，以及每个 kill 的 plan/run/monitor/trace digest。
归档 v1 Manifest 没有 `pss_id`；只为复验旧工件时，才可显式追加
`-legacy-pss-id <frozen-id>`。新的正式 Manifest 不允许该外部补充。

在产生 Blind view 前，curator 对私有 Manifest v2 运行 readiness gate：

```bash
go run ./cmd/benchmark-preflight \
  -manifest /private/benchmark-v2.json \
  -out /private/readiness-v1.json
```

默认要求至少三个不同 root-cause label、三个 control、全部 historical provenance 和全部
variant 的 build-audit/binary binding。报告只输出计数和稳定 finding code，不能证明 root-cause
label 的真实因果独立性；该判断仍必须在冻结样本时由 curator 审查。

## 当前限制与下一阶段

当前完成了协议无关评价基础、公开 calibration、一个历史回归复现和一个历史语义重构样本，
但仍不是正式方法比较：

- 一个 curator-frozen historical trace、一个 semantic reconstruction 与 matching controls
  不能评价 Random、DFS、专家或 Agent；
- evaluator 当前注册 `agreement`、`linearizable-read` 与 `ready-must-sync`；新的 family
  monitor 仍必须由可信代码实现并显式注册；
- typed monitor observation 的解码/一致性失败属于 `invalid`，不是 allowed monitor 的
  protocol finding，不能产生 defect-kill credit；
- 当前 runner 不提供通用 syscall/network
  sandbox，正式 holdout 只能运行 curator 固定来源和 digest 的 binary；
- `first_kill_primary_work` 以 Campaign plan 为粒度，因为现有 Explorer 完成该 plan
  的 primary runs 后才统一执行 Oracle；新报告用
  `detection_granularity=plan-end` 机械声明这一边界，不把 trace 中更早出现的表现误报为
  当时已经在线检出；
- CPU、wall-clock、RSS 和 token 仍是下一版实验报告字段。

M4.11 另提供 `benchmarks/pilots/etcdraft-ready-must-sync-v1/reproduce-fresh-clone.sh`：在
冻结的 Go/toolchain 和预热 readonly module cache 上重建二进制，并逐字节验证 qualification、
campaign 与 evaluator 工件；它不覆盖任何已存在的 pilot binary。

下一阶段继续扩充彼此独立的历史样本与 controls，并在 Agent 未见版本/trigger 的条件下运行
方法对比；只有该比较出现具体漏检模式后，才决定是否增加复合时序义务或 Scenario/Critic Agent。
