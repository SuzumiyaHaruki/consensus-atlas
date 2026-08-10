# M5.21d4：Pre-plan Terminal Accounting

## 目标

M5.21d3 已能安全运行显式 Agent Campaign，但 model call 在 plan 之前失败时，调用成本只存在于
`CampaignModelCallResult`，没有进入 Summary。M5.21d4 要补齐统计，同时保持：

- 不伪造 `CampaignPlannedAttempt`；
- 不伪造 target artifact、report、bundle 或 checkpoint attempt；
- 不信任 provider 任意声明的成本；
- 不因实际成本越过逻辑 allowance 而丢弃已发生的费用。

## 可信来源

只有同一 Campaign、同一 next ordinal、已经由 store 验证并持久化的
`CampaignModelCallResult` 可以成为 pre-plan terminal work 的来源。provider 返回的 typed error
只携带该 result；Coordinator 不能直接接受任意 `WorkLedger` 或自然语言诊断。

store 提交 failure marker 前必须机械验证：

1. 当前 config 是 durable-call mode；
2. model-call ordinal 等于 `head.Sequence + 1`；
3. intent/request、dispatch 和 result 链已完整验证；
4. typed error 引用的 result digest 等于 durable result digest；
5. marker work 等于在空执行账本上附加一次 `result.Work`；
6. work 是否超过 next request allowance 被机械投影为 `budget_exceeded`。

## Schema 语义

`CampaignFailureMarker/v2` 增加：

- `work`：本次未进入 checkpoint 的 terminal work；
- `evidence_kind`：本阶段固定为 `campaign-model-call-result` 或空；
- `evidence_digest`：对应 durable result digest；
- `budget_exceeded`：work 是否越过 next request 的 remaining allowance。

普通 provider/clock/result failure 继续使用空 work、空 evidence 和 `budget_exceeded=false`。

`CampaignSummary/v2` 的 `Totals` 定义为：

`sum(committed attempt records) + failure.work`

仅 failed Summary 可以有 terminal work。`Sequence`、`Attempts` 和 head checkpoint digest 仍只描述
真实提交的 attempt；head checkpoint totals 不因 artifact-less failure 改写。running/stopped Summary
仍要求 totals 精确等于 checkpoint chain。failed Summary 若超过总预算，必须由
`failure.budget_exceeded=true` 解释。

Campaign Observation 的 terminal work 继续来自已验证 Summary，因此自动看到这部分成本，但不产生
execution evidence、PSS、fault、workload 或 monitor 结果。

## 恢复与篡改规则

- 恢复时必须重新将 failure evidence digest/work 绑定到 durable model-call result；
- result、marker work、evidence kind/digest 或 budget flag 任一漂移均 fail closed；
- completed result 后的 parse/validation/compile/allowance failure 与 failed transport result 都可记账；
- dispatch-only ambiguous 没有 result，必须保持空 terminal work；
- failure marker 仍是 no-replace，恢复后不得重试或补写 plan。

## 代码预算与验收

- Go 净增不超过 350 行，包含测试；
- schema bump 不重写历史工件；
- 覆盖普通空成本 failure、failed result、completed result 后 proposal failure、预算越界和篡改拒绝；
- 验证 Summary/Observation 聚合 terminal model work，但 attempt/PSS evidence 数不增加；
- 不新增 Runtime、scheduler、executor、Adapter、HTTP client、CLI strategy 或模型调用；
- 通过全量 test/vet、通用 Campaign 与相关 runner race、exact-partition/legacy audit 和 diff check。

## 不作出的结论

- terminal work 不是一次已执行的测试 attempt；
- model call 完成不代表 proposal 有效；
- budget overrun 被记录不代表方法仍满足正式共同预算；
- 不声称远程服务 exactly-once、Agent 更优、PSS/Coverage 完备或协议正确。

## 完成结果（2026-08-10）

M5.21d4 已完成。`CampaignFailureMarker/v2` 只接受 store 中同一 next ordinal 的 durable
model-call result 作为非空 work 的证据；恢复会重新核对 result digest 和机械投影的 WorkLedger。
`CampaignSummary/v2` 将该 terminal work 加到已提交 records 的 totals 上，但 Sequence、Attempts、
head checkpoint、artifact 和 PSS evidence 均保持不变。普通 failure 与 dispatch-only ambiguous
仍为零成本。

离线验收覆盖五种恢复状态：

- completed result 正常执行，模型成本只进入一次；
- dispatch-only ambiguous 不读 key、不重调，terminal work 为 0；
- failed transport result 记录 1 次调用，即使 token 为 0；
- completed result 后 proposal 解析失败记录实际 1 call/7 tokens；
- 1 call/101 tokens 越过 100-token allowance 时保留实际成本并设置 `budget_exceeded=true`。

五种恢复均为 0 key reads/0 transport calls。后四种失败没有生成 attempt、artifact 或 PSS；
Observation terminal work 与 Summary totals 一致。伪造 result 和重封装后的错误 evidence digest 均在
恢复时被拒绝。

实际 Go 净增 225 行，低于 350 行上限。`go test ./...`、`go vet ./...`、legacy audits、race-shard
exact partition 和 `git diff --check` 均通过；通用 Campaign race 为 53.313 秒，相关 runner race 为
137.293 秒。没有读取真实 key、访问 HTTP/模型、修改 Runtime/scheduler/executor/Adapter 或外部
raft 工作区。

下一阶段若进入 M5.21d5，应只做一次显式授权的单-attempt 外部连通性校准：使用公开固定输入，先保存
完整审计工件，再决定是否扩大调用。该校准只能证明真实 transport、parser 和 trusted compiler 链路
连通，不能作为 Agent 方法优势实验。
