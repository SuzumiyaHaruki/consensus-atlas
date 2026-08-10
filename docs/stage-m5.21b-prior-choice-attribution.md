# M5.21b：Prior Choice 可信归因

## 问题

M5.21a 已限制 Planner 只能根据裁剪后的 Campaign 前缀反馈修改下一 attempt 的 `Prefer`，但
M5.20 Observation 没有记录历史 attempt 使用的 intent、compiled backend。Planner 能看见
“attempt 2 新增 0 个 PSS 状态”，却不能可信地知道该结果对应哪一个宏观选择。

不能通过以下捷径填补：

- 不能让 Planner 自报历史 backend；
- 不能仅根据 strategy 字符串事后猜一个 backend；
- 不能把整个 target artifact、trace 或 candidate identity 暴露给 Planner；
- 不能把“选择了 backend”写成“该 backend 导致发现”的效果结论。

## 冻结设计

### 1. 协议无关 Execution Choice

新增小型 canonical `CampaignExecutionChoice/v1`，由可信端从现有对象机械构造：

- `GuardedTestIntent` digest；
- `CompiledIntentPlanV2` digest、selected backend 与 strategy；
- `IntentExecutionInstance` digest 与 coordinator-owned policy seed。

构造器必须重新验证 `plan.intent_digest == intent.digest` 和 `instance.plan_digest == plan.digest`。
它只表达“该 attempt 被要求按什么宏观计划执行”，不表达 reachability、Oracle 或缺陷 verdict。

### 2. Target artifact 与实际执行绑定

etcd/raft Campaign provider 在执行前使用已 qualification 的 M5.18b4 semantic inputs：

1. 从 trusted hard intent 和 preference 编译现有 `CompiledIntentPlanV2`；
2. 由 request ordinal/seed rule 创建 `IntentExecutionInstance`；
3. 将 `CampaignExecutionChoice` 放入 artifact；
4. 完成执行时复用 `IntentOutcome` 输入验证，并继续检查实际 report policy version/seed；
5. projector 从重新验证的 artifact 投影 choice，不从 strategy 文本猜测。

Campaign config 的 `experiment_spec_digest` 必须绑定原 target spec、semantic view、intent 与 compiled
plan。这样知识包/catalog/qualification 或 plan 漂移会在 resume 前表现为 config identity mismatch。

旧 artifact/observation 实例没有在仓库中作为兼容输入被跟踪，因此本阶段将二者显式升级到 v2，
不在 v1 名义下静默添加语义字段，也不增加双版本读取代码。

### 3. Agent 可见裁剪

`CampaignAttemptProjection` 暂时携带完整 choice 供通用层验证；持久化 Observation 与 Planner
feedback 只保留：

- choice digest；
- intent digest；
- plan digest；
- backend ID；
- strategy。

不暴露 instance digest 或 policy seed。Planner 不能选择或重放 seed；Runtime enabled Action 仍由
compiled backend 内部策略决定。

`CampaignPlannerView` 对非空前缀要求每个 attempt 都有 choice，并检查 backend 位于当前 risk 的
allowed、eligible 交集。零 attempt 的初始视图仍合法。

## 代码预算与验收

- M5.21b Go 净增不超过 700 行，包含测试；
- 不新增执行器、Planner 算法、CLI、checkpoint 类型或模型 transport；
- 原 Campaign provider 必须继续调用唯一的 `etcdraftExecution`；
- artifact tamper、choice/plan/instance drift、resume identity drift必须 fail closed；
- 真实 2-attempt Campaign 的每项 Observation/Planner feedback 必须能关联 backend 与 PSS 增量；
- JSON 暴露测试确认没有 policy seed、instance digest、trace、witness 或 monitor message；
- 全量 test/vet、相关定向 race 与 `git diff --check` 通过。

## 本阶段不做

- 不让 Planner 在 Coordinator 中逐 attempt 运行；
- 不把 proposal/plan/model cost 写入 checkpoint；
- 不评价确定性 fixture 的搜索效果；
- 不接真实模型。

这些属于 M5.21c 及其后的闭环工作。

## 完成结果

M5.21b 已按冻结设计完成：

- `CampaignExecutionChoice/v1` 机械绑定 intent、compiled plan 与 execution instance；
- `etcdraft-campaign-artifact/v2` 在执行前保存完整 choice，验证时从 trusted provider inputs 重算；
- Campaign `experiment_spec_digest` 现绑定 target spec、semantic view、intent 和 plan；
- completed artifact 复用 `IntentOutcome` 输入验证，同时继续核对实际 uniform policy version/seed；
- `CampaignObservation/v2` 和 `CampaignPlannerView` 只暴露 choice/intent/plan digest、backend、strategy；
- 非空 Planner prefix 缺 choice 或出现 risk allowed/eligible 之外的 backend 时 fail closed；
- deterministic plumbing fixture 现在真正消费 prior choice：有新 PSS state 时保持上一 backend，
  无新增时轮换到下一个 allowed backend。它仍不是效果基线。

实际 Go 变化为增加 477 行、删除 69 行，净增 408 行，低于 700 行上限。没有新增执行器、CLI、
checkpoint 类型、Planner 方法或模型调用。

## 实际验收

独立临时 runner 使用 seeds 91/92 完成 2x8-decision etcd/raft Campaign：

- 16 primary decisions，18/18 primary/replay work；
- 18 Core PSS samples，15 unique states；两个 attempt 分别新增 7、8 个状态；
- 两个 attempt 均机械归因到 `admissible-uniform` / `workload-admissible-uniform-b4`，共享同一
  intent/plan digest，choice digest 因 execution instance 不同而不同；
- 2 crashes，2 个 workload pending，0 monitor trigger；
- Summary digest：`7c88af392a7900069d09d256590ce3866154cf498f2add580e5dce154a76480f`；
- Observation digest：`a684dbfb29655be044b5228adfcf95b306c989ef21159f142ff82fbc571f2162`。

该临时完整 artifact 未加入 Git；上述数字是功能验收，不是方法效果或正确性结论。Observation 文本
扫描确认不含 `policy_seed`、`execution_instance_digest`、`final_snapshot` 或 root-cause 字段。

验证结果：

- `go test ./...`：通过，最慢的 `cmd/control-experiment` 为 106.264 秒；
- `go vet ./...`：通过；
- choice/planner 通用定向 race：通过，1.261 秒测试时间；
- 真实 2-attempt etcd/raft Campaign 定向 race：通过，32.048 秒；
- `git diff --check`：通过。

## 仍未完成

Choice 目前由固定 Campaign provider 在一次 Campaign 开始时编译，Coordinator 尚未在每个 attempt
前调用 Planner。M5.21c 必须把 planner-view/proposal/plan/instance/model-work 的 digest 与 cost
纳入 exact request 和 durable attempt artifact/checkpoint 恢复验证；在此之前仍不能宣称形成自适应
多 attempt 闭环。
