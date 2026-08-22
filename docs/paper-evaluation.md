# ConsensusAtlas 论文评测协议

本文档把当前系统的可信执行能力转化为可审稿、可复核的实验计划。它不是新的
verdict、Ledger、执行引擎或发布 gate；版本管理继续使用 Git、方法配置和 evaluator
已有身份字段。论文统计只读取可信 evaluator 已经给出的结果，不能创建或修改 finding。

## 1. 论文的核心主张

ConsensusAtlas 最有价值的主张不是“使用了多个 Agent”，而是：

> 让不可信模型负责提出协议风险和调度策略，让确定性 Runtime、fresh Replay 与独立
> Oracle 负责把自然语言假设转化为可执行、可归因、可复核的共识缺陷证据。

论文应围绕 evidence-grounded agentic testing 展开。PSS、transition novelty、Risk
milestone 和自然语言解释只能说明搜索过程，不能替代 private defect effectiveness。

## 2. 研究问题

- RQ1（effectiveness）：在相同完整预算下，完整 Agentic 方法是否比同 Runtime 的非模型
  搜索和专家脚本检出更多 private root causes，同时保持低 control false-positive？
- RQ2（efficiency）：达到首个可信 kill、固定数量的 protocol PSS states 或 Risk milestone
  需要多少 primary/replay work、模型 token、墙钟时间与资源？
- RQ3（attribution）：Risk/Scenario 分工、跨 Episode Memory、结构化 capability feedback 和
  受限源码 grounding 分别贡献了什么？失败主要来自 hypothesis、capability、search 还是
  evaluator rejection？
- RQ4（generality）：结论能否跨至少两个共识家族和不同缺陷机制保持，而不是只拟合一个
  etcd/raft 场景？

RQ1 是主结论。RQ2–RQ4 用于解释效果和边界，不应合并成一个综合分数。

这些 RQ 的前置条件是：Agent 自己生成的候选能够在未定向提示的真实 Target 上稳定达到
`witness-instantiated`。当前工程首先验收 trigger-prefix feasibility、typed milestone concretization、
qualified execution 和 Replay；在全新模型 canary 通过前，不开展方法 baseline 或显著性比较。

## 3. 方法组与公平性

正式实验至少需要以下同 Runtime、同 Adapter、同 Oracle、同 SUT 输入的组：

1. bounded uniform random：从可信 enabled strategic Actions 中等概率选择；
2. action-class-balanced random：先平衡 ActionKind，再在类内选择，避免大消息队列主导采样；
3. expert scripted：少量公开、协议专家编写但看不到 private 标签的策略；
4. full Agentic：当前 Risk Agent + Scenario Agent + Memory + capability feedback；
5. attribution arms：每次只移除一个机制，至少包括无跨 Episode Memory、无源码 grounding，
   以及不使用结构化 capability feedback。

仓库已有确定性 random policy 构件，但尚未把全部对照组接入与 Agentic formal trial 完全相同的
外层运行入口。投稿前必须完成这一入口；不能拿单元测试里的 policy 或历史 Campaign 结果冒充
同预算 baseline。若单 Agent 组无法保持任务和模型调用预算等价，应作为机制分析而不是主 baseline。

公平预算不能只比较 scheduler decisions。每个 trial 同时限制并报告：

- Runtime setup/reconstruction、strategic/public-progress decisions 和总 primary work；
- child verification 与最终 fresh Replay work；
- 模型 calls、input/output tokens；
- wall-clock、CPU time、peak RSS；
- 调用失败、invalid trial 和完整 Investigation 的前序失败成本。

主表使用相同 primary/replay 上限；另给等墙钟或等货币成本的 sensitivity analysis。模型组未用完的
预算不能转换成额外、未计账的 Runtime 搜索。随机和 Agent 方法使用预先发布的 seed 列表；任何方法
都不能按 private 结果挑选成功 seed。

## 4. 数据集与隔离

开发、调参和最终结论使用三套不重叠数据：

- public calibration：验证 action → evidence → Oracle → Replay plumbing，可公开 trigger；
- private holdout：历史修复和语义 mutant 的 candidate/control pairs，产生论文主结果；
- new case studies：在未注入版本上发现的新问题，单独人工确认，不混入 holdout 命中率。

每个 root cause 可以包含多个 manifestation，但主统计单位是独立 root cause，同一根因最多记一次。
每个 candidate 都有同构 control；pair mapping、标签、trigger、build identity 和 evaluator Oracle 输出
不进入 Agent Memory。方法运行前用普通 Git 提交、配置版本和试验清单记录方法与数据版本即可，
不为此新增第二套 hash 或冻结 contract。

数据规模应由预期效应和功效分析决定。只有三个根因的 plumbing 测试无法支持方法优越性；面向高水平
系统/软件工程论文，目标应是覆盖多种机制的数十个独立根因，并明确报告各协议、各缺陷类别的数量。
如果可用历史缺陷不足，应缩小主张并把 mutant 与真实缺陷分层报告，不能把多个近似 mutant 当成
独立样本扩大显著性。

## 5. 指标与统计

主指标采用 intention-to-test 口径：

- independent root-cause kill rate，invalid 和超预算 trial 保留在分母且不给 kill credit；
- control false-positive rate；
- invalid-trial rate及其机械原因分布；
- 每个可信 kill 的 total primary/replay work、模型 token、墙钟和资源成本。

次要解释指标包括 protocol/control/joint PSS、unique semantic transitions、最长重复深度、
Risk milestone progress、capability repair 以及到首个可信 kill 的成本。所有指标保留原始分子、分母和
分布，不只报告均值或自归一化面积。

统计分析遵循以下顺序：

1. 每个方法给出 raw `k/n` 和 root-cause kill、control false-positive 的 Wilson 95% 区间；
2. 相同 private roots 上的方法比较给出配对四格表、kill-rate difference 和 exact McNemar p；
3. 多 seed 数据按 root cause 聚类 bootstrap，或使用包含 method、protocol、defect class 与 seed 的
   分层二项模型，同时给出效应量区间；
4. 预先指定一个主比较；多个消融比较使用 Holm 校正；
5. 报告失败和 invalid，不删除离群 seed，也不把公开 calibration 纳入显著性检验。

Wilson 区间和 McNemar 检验把 root/control 当作抽样单位；当缺陷集合是目的性样本时，它们只量化该
benchmark 上的不确定性，不能自动外推到所有共识实现。少样本时应强调效应量和逐例机制分析，不能
用 `p > 0.05` 宣称方法等价。

## 6. 离线论文表格

`cmd/evaluation-report` 读取 `cmd/defect-eval` 已验证的 formal fresh 或 Agentic holdout JSON，输出
aggregate-only Markdown。它会重新调用 evaluator report 的 `Validate`，拒绝 benchmark/root set
不匹配，且不输出 private pair、trial 或 root-cause ID。

```bash
go run ./cmd/evaluation-report \
  -input bounded-random=/private/results/random.json \
  -input agentic=/private/results/agentic.json \
  -out /private/results/paper-table.md
```

表格包含：root-cause kill 与 Wilson 区间、candidate kill、control false positive、invalid、
primary/replay work、Agentic model calls/tokens/Scenario decisions，以及按 private root 配对的 exact
McNemar 结果。该命令不读取 Agent 自报 finding，也不重新执行 Oracle，因此不能取代 `defect-eval`。

当前命令聚合一次完整 evaluator report。重复 seed 的 cluster bootstrap/分层模型应在收集到真实重复
数据后实现，以实际数据层级为准；现在提前增加通用统计 DSL 只会冻结错误的试验单位。

## 7. 投稿前必须补齐的实现与证据

当前代码已具备统一 Runtime、真实 Target、typed MethodSpec、完整 Agentic Investigation 成本、
evaluator-owned SUT Replay、独立 target Oracle 和本文的离线配对统计。但以下内容仍是论文结论的
硬缺口：

- 将 bounded uniform、action-class-balanced 和 expert strategy 接到与 Agentic 相同的 formal 入口；
- 建立足够规模、标签隔离的 private candidate/control benchmark；
- 在全新目录完成多 seed 运行，而不是复用 M4n26 两个无 witness canary；
- 补齐整个进程树的 wall-clock、CPU、peak RSS；当前 `not-collected` 不能写成实测值；
- 至少增加一种与 leader-based CFT 不同的机制或明确收缩论文外部有效性主张；
- 对新发现 case study 做独立复现、根因定位和上游确认。

推荐的论文结果顺序是：先用公开 calibration 证明 plumbing 没有泄漏和误归因；再用 private holdout
回答 RQ1；随后给出预算曲线和消融；最后用 2–4 个完整可 Replay case studies 展示系统如何从语言假设
落到可信证据。M4n26 canary 只能作为系统链路校准和负结果，不能进入 effectiveness 主表。

## 8. 有效性威胁

- construct validity：PSS breadth 不等于缺陷效果，token 也不等于执行工作；分表报告；
- internal validity：相同 SUT/Runtime/Oracle、隐藏标签、完整失败成本和 fresh Replay 控制归因；
- external validity：按协议和缺陷机制分层，真实缺陷与 mutant 分开；
- conclusion validity：root-level 配对、重复 seed、效应量区间和多重比较校正；
- reproducibility：发布代码提交、公开方法配置、seed、公开 calibration 工件和可公开的 evaluator
  聚合结果；private 标签无法公开时，发布生成规则和隔离审计过程。
