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

## 当前可编译评测面

M5.16R 删除了旧 Candidate Catalog、preflight、blind audit、blind replay 和 submission CLI 的
可编译实现；对应 schema、阶段文档和实验工件仅作为历史归档。当前 `internal/defectbench` 只保留
`BundleBenchmark` 与 bundle evaluator，`cmd/defect-eval` 只组合 etcd/raft projector：

- 合法 classification 只有 `public-calibration-only`；
- manifest 数据模型能容纳多组 variant，但 CLI 的 legacy/fresh 路径都只接受恰好一组
  control/calibration candidate；
- fresh 路径验证 MethodSpec、BuildAudit、binary digest，亲自执行冻结 binary 后重算 bundle 和 monitor；
- 公开 single-pair CLI 保持原语义；formal mode 已能从 private path manifest 执行多 pair
  evaluator-owned fresh evaluation。

因此本页开头的私有评测图仍是目标可信边界，不是当前功能声明。若未来恢复正式链，必须在 v2 数据面
上重新实现最小合约，不能把已删除的 M4 工具或归档 schema 直接当作可用实现。

M5.21l 已在当前 v2 数据面新建最小 `FormalBenchmarkContract/v1`，没有恢复旧 runner。
private contract 用显式 pair 绑定 candidate/control、root cause、build evidence 和 composition identity；
`FormalOpaqueView/v1` 只公开 opaque trial 与共同方法/预算。通用 resolver 能按 ID 选取
projector/monitor；M5.21n 已将它接入 private fresh evaluator，但当前仍没有可消费该链的
multi-pair CLI 或真实 private dataset。

M5.21m 增加 `FormalExposureAudit/v1`：它重算 contract 的 exact opaque view，为每份公开 JSON
byte snapshot 保存 SHA，并递归检查 key/value string 是否等于已枚举 private atom。报告不回显
命中值或本地路径。它只防直接复制，不防协议推断或编码泄露，也不替代 runner 隔离。

M5.21n 增加 `FormalFreshEvaluation/v1`。入口必须同时接收 sealed contract、passed exposure audit、
MethodSpec、精确 trial→fresh evidence map 和已注册 composition；随后逐 pair 复用既有 fresh evaluator
的 build/method/bundle/projection/budget 校验，并以 TraceIntegrity 加 contract-selected monitors 判定。
private ledger 显式保存 pair/root mapping，机械汇总 control false positive、candidate/root-cause kill 和
invalid trial，并绑定 contract/exposure/method digest。公开 synthetic fixture 的六个 trial 全都引用同一
correct bundle，因此只验证 plumbing，不是 holdout 或检出证据。

M5.21o 增加 `FormalFreshInputs/v1` 与 `cmd/defect-eval` formal mode。path manifest 是 private
curator input，路径不写入 evaluation ledger。CLI 要求 contract/exposure/inputs/method/artifact/out
全部显式提供，不允许混入 public-pair flags；它会在第一次 SUT execution 前验证全部
trial/audit/binary，并拒绝覆盖旧 artifact/output。实际执行和 MethodSpec timeout 复用原有
fresh subprocess runner。当前 registry 只支持 official etcd/raft projector 与 Agreement monitor。

## 当前方法与 Oracle adequacy 缺口

现有 formal CLI/evaluator plumbing 尚未执行 A9 的 Risk Agent + Scenario Agent 方法；它不能把 A9d6 的公开
OmniPaxos episode 转述为正式 defect trial。当前仓库也没有真实 private holdout，因而还不能计算 Agent 方法的
root-cause kill rate 或与 Agora、deterministic search 的召回比较。

在准备 private batch 前，先复用现有 Bundle、Replay、monitor 和 evaluator 做一个公开的 capability pilot：对每个
少量已知根因或受控语义变异，使用 curator-owned ground-truth trigger 验证以下链条：

```text
root/workload -> controllable Action -> observable evidence -> trusted Oracle -> stable Replay
```

每个 candidate 配置同基线 correct control。该 pilot 首先测 Oracle/Adapter 的检出充分性，不测 Agent 搜索效果；
只有 ground-truth trigger 被稳定检出后，该根因才可进入 search-recall 实验。已知优先缺口包括不同 frontier 的
前缀冲突、跨 incarnation 持久性回退，以及 panic/worker-exit/timeout 的证据保留和完整计费。

A9e2 已闭合前两个受控反例。第三个负校准使用真实 OmniPaxos worker：Action deadline 到期没有中止 worker
call，worker 异常退出后 sealed Trace 仍只是原成功前缀，fresh Replay 也只验证该前缀。下一步所需的是独立于
成功 Trace 的最小 terminal outcome，而不是把失败动作写成 `applied` record 或直接当成 Agreement finding。

这一阶段不新增 Manifest、hash、冻结 contract、Ledger 或 admission gate。Git、版本、类型和普通测试能验证
既有规则的实现一致性，却不能证明 projector/monitor 对目标缺陷类别有足够 recall；pilot 使用现有 evidence
路径直接暴露缺失能力，之后只做由具体失败驱动的最小扩展。

## 公开样本的定位

仓库共有七组公开 candidate/control pair。三组能由当前 `BundleBenchmark` 验证，但都指向同一个公开
command-data calibration 根因；另外四组使用已退役格式，其中包括公开 ReadIndex 历史回归和
Ready.MustSync 语义重构。全部工件都可用于复验 monitor、构建和 evaluator 链，不能改标成非公开
holdout，也不能评价 Random、DFS、专家或 Agent。

当前不再维护一个逐字冻结的 holdout-readiness 报告：Git、结构化输入类型、qualification 和 evaluator
已经能表达可用证据，而旧报告只会把历史文件布局变成测试前置条件。仓库中的 pair 都是公开校准材料，
不能改标成非公开 holdout，也不能评价 Random、DFS、专家或 Agent。

该结果只覆盖当前仓库，不断言仓库外不存在私有样本；root-cause label 的因果独立性仍需在冻结前
由 curator 审查。真正进行隐藏评测时，应在仓库外准备人工复核的 private matching pair 和真实
BuildAudit/binary，再运行 formal CLI；没有这些输入就明确报告 benchmark-input gap，不用 synthetic pair
填数，也不为此继续增加 schema。
