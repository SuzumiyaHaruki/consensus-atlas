# M5.21a：Campaign Planner 最小视图与权限冻结

## 目标

本阶段只建立 `Campaign 前缀证据 -> 下一 attempt 偏好 intent` 的最小、协议无关边界，
不接真实模型，不改变 Runtime/Adapter，也不把 Planner 接入持久化 Coordinator。

Planner 不是 Action scheduler。它只能在 attempt 之间提出宏观偏好；每个 attempt 内的 enabled
Action 仍由已经 qualification 的执行策略选择，可信编译器、Runtime 和 monitor 仍拥有最终权限。

## 可复用的既有对象

- `AgentSemanticView`：协议知识、qualification 后的能力、可用 Action 与 eligible backend；
- `CampaignObservation`：已提交 artifact 的跨 attempt 机械投影；
- `CampaignAttemptRequest`：下一 ordinal、当前 checkpoint head 和剩余逻辑预算；
- `GuardedTestIntent`：`Must` 是可信硬约束，`Prefer` 是 Planner 唯一可变字段；
- `ValidatePreferenceOnlyProposal` 与 `CompileGuardedTestIntentV2`：拒绝硬约束漂移，并从可信输入
  重新编译 proposal。

因此 M5.21 不再建立第二套 plan、budget、feedback 或模型返回 schema。

## Planner 可见视图

`CampaignPlannerView/v1` 由可信端从上述三个已验证对象构造，包含：

1. 完整 `AgentSemanticView`；
2. trusted baseline `GuardedTestIntent`，其 digest 将本次请求的全部硬约束绑定进视图；
3. 下一次 `CampaignAttemptRequest`，包括 exact identity、previous head、ordinal 和 remaining allowance；
4. 从前缀 Observation 裁剪出的统计：attempt outcome、每次 PSS samples/states/new states、
   fault/workload 聚合，以及 monitor 检查/触发计数；
5. Observation digest，用于证明反馈来自哪个已校验前缀。

明确不暴露：artifact/bundle digest、trace、PSS witness/state body、monitor message/step、build、
candidate/control、root-cause 与 Oracle 私有身份。Planner 不能从该视图授予能力或形成 verdict。

构造器必须机械验证：

- semantic view、Observation、request 各自有效；
- Campaign/config/target/spec identity 完全相等；
- Observation 是 `running` 前缀，head 等于 request previous digest；
- `request.ordinal == observation.attempts + 1`；
- baseline 的 view 与 semantic view 相等，且 decisions 不超过下一 request allowance；
- attempt 反馈只来自 Observation 的有序投影。

PSS unique/new state 是无固定分母的发现量；monitor trigger 是待可信评估的索引。二者都不是
覆盖完备性、安全性或缺陷 verdict。

## Planner 权限

M5.21a 使用一个明确标注为 plumbing fixture 的离线确定性 Planner。其输出仍是
`GuardedTestIntent/v1`，并必须：

- 保持视图内 baseline 的 ID、view、risk、decisions、required capabilities/actions、fault envelope；
- 只重排/选择 `Prefer.BackendIDs` 与 `Prefer.Actions`；
- 经 `ValidatePreferenceOnlyProposal` 后，再经 `CompileGuardedTestIntentV2`；
- 不选择 policy seed、Runtime Action、节点、消息、timer 或 monitor；
- 不修改 Campaign request/allowance，也不把自身调用成本计为执行成本。

该 fixture 只证明数据面连通和 fail-closed 权限，不代表搜索算法，不参加效果排名。

## 已发现但本阶段不隐藏的缺口

M5.20 Observation 当前没有记录每个 attempt 的 intent/compiled backend。因此即使它能报告
“attempt 2 新增 0 个 PSS 状态”，Planner 也无法可信地知道这个结果对应哪个历史选择，尚不能进行
可归因的自适应学习。

后续 M5.21b 必须由 target-owned artifact projector 机械产生协议无关的 prior-choice binding
（至少 plan/intent digest 与 selected backend），并和 committed attempt record 一一绑定。不能让
Planner 自报历史选择，也不能为此暴露整个目标专用 artifact。完成该绑定及 checkpoint 恢复验证前，
不得宣称形成 Campaign 闭环。

## 代码体积预算与验收

- M5.21a Go 净增不超过 600 行，包含测试；
- 不新增执行器、CLI、持久化 schema、target-specific planner 或真实模型 transport；
- 通用测试覆盖 canonical construction、identity/head/ordinal tamper、敏感字段不泄漏；
- 组合测试证明 deterministic proposal 只能改变偏好，能够进入既有 v2 compiler；
- `gofmt`、相关 `go test`、`go vet`、`git diff --check` 通过；
- 阶段总结必须同时报告实际净增行数和仍未证明的事项。

## 后续顺序

1. M5.21a：最小 Planner View、权限校验与离线编译 plumbing；
2. M5.21b：可信 prior-choice projection 与 attempt binding；
3. M5.21c：把 plan/request/cost 纳入增量 checkpoint，验证中断恢复后不重算、不漂移；
4. M5.21d：在同一接口上运行无模型自适应基线；
5. 只有上述闭环成立后，才显式 opt-in 一次真实模型调用，并将 calls/tokens 计入 allowance。

## 完成结果

M5.21a 已按冻结边界完成：

- 新增 `CampaignPlannerView/v1`，将 semantic view、trusted baseline、exact next request 与裁剪后的
  prefix feedback 绑定为一个 canonical digest；
- 构造器拒绝 Campaign/config/target/spec/head/ordinal 漂移，并要求 baseline decisions 不超过下一次
  request 的 scheduler-decision allowance；
- 反馈只保留 attempt outcome、PSS 发现计数、fault/workload 聚合和 monitor 计数。JSON 暴露测试确认
  不包含 artifact/bundle digest、PSS witness/state body、monitor message/step 或 defect identity；
- `ValidateCampaignPlannerProposal` 在既有 hard-field 校验之上冻结 proposal ID，使 `Prefer` 成为唯一
  可变字段；
- zero-model deterministic fixture 在最近一次 execution 没有新增 PSS 状态时轮换 allowed backend；
  它只作 plumbing 验证，不是搜索效果方法；
- 既有 M5.18b4 etcd/raft semantic inputs 已证明该 proposal 能进入原有
  `CompileGuardedTestIntentV2`，没有建立第二个编译器或执行器。

实际 Go 净增 563 行（通用实现/测试 519 行，既有 b4 组合测试增加 44 行），低于 600 行上限。
没有新增 CLI、顶层测试清单项、持久化 schema、target-specific planner、SUT run 或模型调用。

验证结果：

- `go test ./...`：通过；
- `go vet ./...`：通过；
- 新增通用 Planner 测试定向 race：通过，1.105 秒测试时间；
- 被修改的既有 b4 组合测试定向 race：通过，146.599 秒；
- `git diff --check`：通过。

本阶段仍没有形成可归因的多 attempt 自适应闭环，也没有证明 Planner 优于 Random/DFS/专家方法。
M5.21b 的唯一主任务是补齐 prior intent/compiled backend 与 committed attempt 的可信机械投影。
