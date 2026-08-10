# M5.21d2：etcd/raft Offline Durable Model Planner

## 目标

M5.21d1 已提供协议无关的 durable model-call 状态机，但还没有 target provider 使用它。
M5.21d2 只在 etcd/raft composition root 接通下面的离线闭环：

`CampaignPlannerView -> exact prepared bytes -> intent -> dispatch -> one injected transport -> result -> trusted compile -> durable plan -> existing execution`

这里的 transport 是测试注入的离线函数，不读取 key、不访问 HTTP endpoint。它用于验证组合、
恢复和计费边界，不评价 DeepSeek 的输出质量。

## 冻结边界

1. CLI 继续使用 M5.21c 的 `zero-model` provider；本阶段不增加真实模型开关。
2. durable provider 复用现有 DeepSeek request serializer，但只能通过注入的 transport 函数取得回复。
3. Agent 输出仍是 `GuardedTestIntent`，且必须通过 `ValidateCampaignPlannerProposal`；相对可信
   baseline 只能改变 `Prefer`，不能改变 risk、must、budget、seed、instance、Oracle 或 verdict。
4. result 必须先于 parse/compile/plan 持久化。恢复若已有 completed result，则只从 durable bytes
   继续；不得再次调用 transport。
5. 恢复若只有 dispatch，则返回稳定 ambiguous error；不得重试、补写 result 或降级到 zero-model。
6. transport terminal failure 必须作为 failed result 持久化；恢复只能返回同一稳定失败，不得重试。
7. target report/bundle 仍只记录执行工作；Campaign artifact、record、checkpoint totals 在其外层恰好
   附加一次 `PlanningWork`。模型成本不得写入 report/bundle，也不得重复累计。

## 组合身份

etcd/raft Campaign composition digest 必须绑定 planner identity。zero-model 保持现有确定性规则；
durable provider 使用由 prompt schema 与 `AgentTransportFreeze` 得到的独立 identity。两个 provider
不能共享 experiment spec digest，也不能在恢复时互换。

## 恢复验收

本阶段至少形成三个离线见证：

- **result-before-plan**：第一次进程完成唯一 transport 调用并提交 result，但尚未写 plan；恢复后
  transport 调用数不增加，仍能从 result 完成可信编译、计划持久化和既有执行；
- **dispatch-only**：恢复为 ambiguous，注入 transport 调用数保持 0；
- **terminal failure**：第一次调用产生并持久化稳定 failed result；恢复后返回同一失败且调用数不增加。

成功见证还必须证明 `result.Work == planned.PlanningWork == artifact/record/head 中新增的 ModelWork`，
而 report/bundle 的 ModelWork 仍为 0。

## 代码预算与验收

- M5.21d2 Go 净增不超过 500 行，包含测试；
- 不新增 Runtime、scheduler、executor、通用 Agent DSL、HTTP client 或 CLI strategy；
- 不修改外部 raft 工作区，不读取 key，不发起模型或网络调用；
- 通过定向测试、全量 `go test`、`go vet`、相关 race、legacy audit 和 `git diff --check`；
- 先提交本冻结文档，再分段提交 composition、恢复见证和完成总结。

## 不作出的结论

- 不声称远程服务 exactly-once；
- 不声称离线 mock 等价于 DeepSeek；
- 不声称 Agent 优于确定性 planner 或其他搜索方法；
- 不声称 PSS/Coverage 完备，也不声称 etcd/raft 或 ConsensusAtlas 正确。

