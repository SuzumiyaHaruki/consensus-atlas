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

