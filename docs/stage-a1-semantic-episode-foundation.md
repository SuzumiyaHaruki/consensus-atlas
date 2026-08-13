# A1：Semantic Episode 契约与 Search Kernel 解耦

日期：2026-08-13

分支：`feature/agentic-consensus-testing`

状态：确定性契约纵向切片完成；未接真实模型

## 1. 本阶段为什么只做这一小段

A0 明确了新的研究问题：Agent 应当提出并修正共识级测试假设，而 Runtime、Replay 和 Oracle 继续掌握
执行事实。本阶段没有直接接模型，而是先证明 Agent 将来获得更大规划权后，仍不能扩大实际 Action 权限、
修改预算或定义 verdict。

A1 复用已有协议无关 fixture、bounded exact-prefix DFS、RiskWitness、Policy、Trace、Replay 和 WorkLedger。
没有新增 Runtime、Campaign、PSS、Oracle、target、operator catalog 或第二套账本。

## 2. SearchAlgorithm / GuidancePolicy

现有 DFS 被拆出两个运行时职责：

- `StatelessSearchAlgorithm`：拥有 exact-prefix 重建、遍历形态、预算、child materialization、fresh verification
  和 work accounting；
- `StatelessGuidancePolicy`：只排列一个已冻结 frontier 的完整 `FrontierActionRef` 集合。

内核会逐项核对 ActionID 和完整 ActionRef，拒绝缺失、重复、伪造或被修改的返回值，并保留已经发生的
frontier reconstruction work。typed-nil guidance 也 fail closed。

旧 `ExploreBoundedStatelessDFS` 只是以 canonical guidance 调用新 seam。旧持久化 `StatelessDFSSpec`、
`StatelessDFSWorkItem` 和 `StatelessDFSResult` 没有加字段；固定 fixture 的 pre-A1 result digest 被锁定为
`98749847e4f85748eecad2af2709838b7e976d39a0844f7dc0939893eeef3991`。

这证明运行时组合 seam 没有改变该固定旧结果。它尚不意味着 algorithm 和 guidance 已成为两个可独立
冻结的正式方法 identity；A2 出现第二个算法前必须在外层绑定它们。

## 3. 四个对象

### TestHypothesis

绑定 Protocol Knowledge、一个既有 RiskWitnessSpec 和不参与判定的 rationale。它不包含 Action、未来
schedule、fault、budget、预期缺陷或可执行 Oracle。Knowledge 中该 risk 必须允许当前 search backend。

### SemanticEpisodeView

由可信层从 root RiskWitnessResult 和 bounded DFS result 构造。每个 WorkItem 只映射为 episode-local
candidate ID、parent prefix decision count、path depth 和 ActionKind；真实 ActionID、WorkItem digest 和
target identity 不直接暴露。

view 绑定固定 search algorithm、候选集合 commitment、exposure policy 和 feedback policy。initial view
必须没有 prior digest；repair view 必须连同 prior view、rejected report、精确 proposal bytes 和可信
model work 重新验证，不能仅凭一个 SHA 声称发生过拒绝。

RiskProgress 当前保留 trace/result source-binding digest。这对 A1 source verification 有用，但它也是潜在
稳定指纹。因此 A1 只在公开 synthetic fixture 使用该结构；A5 opaque-source 前必须增加 trial-local wire
projection，而不是把内部 binding object 原样交给模型。

### EpisodePlan

严格 JSON parser 使用 unknown-field deny-by-default。plan 只能绑定 hypothesis/view/candidate-set、回显既有
risk，并返回完整 candidate permutation。它没有 Action、algorithm、operator、root、fault、budget、stop、
assertion 或 verdict 字段。

当前 compiler 只消费排列首项，将其解析回可信 WorkItem，并生成准确停在 `item.Path.Decision` 的 exact
Policy。fallback 从所选 ActionKind 机械导出，调用者不能提交；非空 root 回归测试锁定了绝对 decision
语义。

### EpisodeReport

只承载在线 `PlanningFeedback`，不嵌入 evaluator outcome。拒绝报告由精确 proposal bytes 重新解析，稳定
区分 JSON、binding 和 candidate-set 三类原因；proposal/model work 必须与可信输入相同。执行报告要求
proposal 解析为同一 plan、Trace 等于所选 child、RiskProgress 来源匹配、WorkLedger 可重算并可 fresh
Replay。

candidate/control identity、private monitor、Oracle verdict 和 root-cause mapping 仍属于已有 terminal
evaluator artifact，不回流 planner。

## 4. 确定性纵向切片

fixture 实际走过：

```text
trusted knowledge + existing risk + root result
                    |
                    v
             TestHypothesis
                    |
                    v
        initial SemanticEpisodeView
                    |
       illegal authority-bearing JSON
                    |
                    v
       mechanical rejected EpisodeReport
                    |
                    v
         source-bound repair view
                    |
       valid non-canonical candidate order
                    |
                    v
          strict EpisodePlan parser
                    |
                    v
       exact WorkItem -> exact Policy -> Trace
                    |
          fresh Replay + Risk projection
                    |
                    v
          executed EpisodeReport
```

负例覆盖 guidance 扩权、未知 policy、未知 plan 字段、陈旧 binding、不完整/重复/伪造 candidate、三类拒绝
原因、伪造 prior digest/reason/model work、计费不足、合法但属于另一 candidate 的 Trace，以及 normalized
plan digest 与 exact proposal digest 混淆。planner view/feedback 使用正向 JSON key allowlist 防止新增字段
静默进入模型输入。

## 5. 本阶段能够和不能够说明什么

A1 能够说明：

- 旧 DFS 可以在不改变固定 persisted result identity 的前提下暴露受限 guidance seam；
- planner 无法增加、删除或改写 Runtime 提供的 Action；
- 最小 episode 对象可以把 proposal、候选、执行、语义进度和 work 重新绑定到可信来源；
- 非法 proposal 可以成为下一次规划的机械输入，而不使整个流程失去审计性。

A1 不能说明：

- 已经存在真实 Agentic 或多 Agent 闭环；
- 当前实现是 semantic best-first；
- Agent 改变了搜索过程中全局队列的扩展顺序或降低了发现候选的成本；
- fixture 的手工 Risk evidence 证明真实 etcd/raft projector 正确；
- executed feedback 已经让下一次 Agent plan 发生变化；
- Agent 优于 random、DFS、semantic best-first 或专家计划。

当前 corpus 在规划前已被 bounded DFS 完整物化，完整 permutation 只有首项改变本次执行。因此最准确的
称呼是：**bounded DFS candidate corpus 上的可信 head selection/compile/replay 契约切片**。

## 6. 代码增长审查

本阶段新增代码只服务两个当前消费者：搜索 seam 的旧入口/基线，以及 deterministic episode fixture。
没有预建 EpisodeSpec、Blackboard、通用 DSL 或新 ledger。Episode 对象仍有一些为审计可读性保留的重复
binding；A2 若没有消费者，应优先删减而不是继续横向扩 schema。

## 7. 验证

定向验证已通过：

```text
go test ./internal/controlexperiment \
  -run 'TestEpisode|TestSearchKernel|TestBoundedStatelessDFSReconstructsCanonicalExactPrefixWorkItems' \
  -count=1
git diff --check
```

收尾验证结果：

- `go test ./...`：通过；既有 `cmd/control-experiment` 包耗时 256.259 秒；
- `go vet ./...`：通过；
- 上述 A1 定向集合的 `go test -race`：通过，4.114 秒；
- `make test-race-full`：按此前约定仅尝试一次，外层 300 秒时限内仍停留在既有 method shard，退出码 124，
  无失败输出；不重试、不据此宣称 full race 通过；
- `python3 -m unittest discover -s agents -p 'test_*.py'`：命令通过，但当前 `agents/` 没有 Python 测试，
  实际运行 0 项；
- 本阶段没有新增或修改 JSON 工件，因此无实例/schema 增量校验；
- `git diff --check`：通过；
- 仓库和桌面的两份总体规划 SHA-256 相同。

## 8. 下一步

A2 拆成两段，避免同时改变算法、语义输入和模型：

1. **A2a deterministic semantic best-first**：建立预算内 lazy global queue、per-candidate trusted semantic
   projection 和版本化字典序；与旧 DFS 在相同完整 work ceiling 下对照；
2. **A2b single Explorer**：复用 A2a 的同一 wire view、candidate set 和算法边界，接入真实模型，证明
   plan -> WorkItem -> Trace -> mechanical feedback -> next plan 的行为因果链。

排序 tuple 不在 A1 提前冻结；只有 per-candidate projector、visit accounting 和实际消费者一起落地时才
冻结。A2a 不接模型、不增加 Agent 角色、DPOR、新 Oracle 或 operator catalog。
