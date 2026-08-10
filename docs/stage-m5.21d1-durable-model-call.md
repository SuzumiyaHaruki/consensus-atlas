# M5.21d1：Durable Model Call Lifecycle

## 目标

M5.21c 已保证 zero-model plan 先于 target execution 落盘，但现有 `invokePrepared`
只能在 HTTP 返回后生成 `AgentInvocationAudit`。若进程在服务端可能已接收请求、
本地结果尚未 durable 时中断，恢复端无法证明重试不会重复收费或改变试验。

M5.21d1 只落地协议无关的 durable call lifecycle，不接 target provider 或真实模型：

`exact intent -> dispatch marker -> durable result | ambiguous terminal`

## 为什么不能只保存 Audit

Audit 是调用完成后的可审计结果，不是调用前的 write-ahead record。安全恢复至少需要：

1. 在任何 transport 动作前持久化 exact public prompt/request bytes 和 transport commitment；
2. 在可能进入 transport 前持久化 dispatch marker；
3. 只有拥有该次活跃 dispatch 的进程才能提交 result；
4. 重建 Recovery 后若只看到 dispatch 而无 result，必须归类为 `ambiguous`，
   不能调用 transport，也不能伪造成功/失败 result。

dispatch marker 必须在真正调用前写入。这会保守地将“marker 已落盘、但还没来得及
发请求”也分类为 ambiguous；它可能少执行一次，但不会因为乐观重试而双重计费。

## 持久化对象

Campaign root 新增 `model-calls/`，每个 ordinal 最多三个 no-replace 文件：

- `N.intent.json`：exact Campaign request/view digest、provider/endpoint/model/参数、prompt/request
  bytes/digest/length、local call key 和 canonical digest；
- `N.dispatch.json`：intent digest 和 canonical digest（call key 已被 intent digest 间接绑定）；
- `N.result.json`：dispatch digest、稳定 status/failure class、成功时的 accepted proposal
  bytes/digest、provider response identity/digest、duration 和 `ModelWork`。

intent 中的 bytes 是已冻结的公开 Planner 输入，不含 key/Authorization header。result 不保存
任意 HTTP 错误页或私有 transport diagnostic；成功时只保存已经 trusted transport 结构校验的
proposal content。local call key 只是本地关联 identity，不声称 DeepSeek 提供服务端幂等。

## 恢复状态机

| durable files | 恢复状态 | 允许动作 |
|---|---|---|
| none | absent | 构造并落盘 intent |
| intent | prepared | 可落盘 dispatch，然后最多调用一次 |
| intent + dispatch | ambiguous | 终止；禁止重试和补写 result |
| intent + dispatch + result | completed/failed | 不调用 transport；从 durable result 继续或终止 |

当前进程调用 `MarkDispatched` 后获得不落盘的私有活跃令牌；`CommitResult`
必须同时看到该令牌和未变的 config/head/intent/dispatch。从磁盘重建的 Recovery 不会重建
令牌，所以 dispatch-only 状态机械地不可完成。

## Campaign 绑定

`CampaignConfig/v3` 将 planner mode 冻结为：

- `none`：未规划的 base-request provider；
- `zero-model`：M5.21c deterministic planner，model allowance 必须为 0；
- `durable-call`：planned attempt + 非零 model allowance + durable call lifecycle。

durable-call mode 中，已提交 attempt 必须同时存在 completed call result 和 plan，且
`plan.PlanningWork == result.ModelWork`。任何 model-call 文件在 none/zero-model mode 中都是非法状态。

## d1/d2 边界

M5.21d1 完成通用对象、store、恢复、篡改拒绝和离线状态机见证，但不调用
`invokePrepared`。M5.21d2 才在 etcd/raft composition 中完成：

`PlannerView -> prepare exact bytes -> intent -> dispatch -> one offline transport -> result -> proposal -> plan`

d2 首先使用离线 mock transport 测试成功恢复、transport terminal failure 和 ambiguous 不重试；仍不
读取 key 或访问真实 endpoint。

## 代码预算与验收

- M5.21d1 Go 净增不超过 600 行，包含测试；
- 不新增 Runtime、scheduler、executor、CLI strategy 或 HTTP client；
- 覆盖 intent 幂等/no-replace、dispatch 先行、同进程 result commit、dispatch-only 恢复
  ambiguous、result 恢复、missing/tamper/future/cross-ordinal 拒绝；
- 检查 config planner-mode 不能降级，zero-model 现有实验不漂移；
- 通过全量 test/vet、通用 Campaign race 和 `git diff --check`。

## 不作出的结论

- 不声称远程调用 exactly-once；
- 不声称 transport error 意味着服务端未收费；
- 不声称 local call key 是服务端幂等键；
- 不读取 key，不调用模型，不评价 Agent 效果。

## 完成结果（2026-08-10）

M5.21d1 已完成。`CampaignConfig/v3` 将 planner mode 纳入 config digest；M5.21c
etcd/raft runner 机械迁移为 `zero-model`，现有实验仍保持 model allowance 为 0。
`durable-call` 必须同时使用 planned-attempt 输入和非零 model allowance，不能通过修改
mode 绕过恢复检查。

通用 store 现在可以持久化 exact intent、dispatch 和 completed/failed result。离线见证覆盖：

- 重复准备相同 intent 幂等，替换 digest 由 no-replace/conflict 边界拒绝；
- completed result 完整恢复，result 内容篡改、future ordinal、result 缺 dispatch 均拒绝；
- committed call chain 缺失所有 call files 时拒绝恢复；
- dispatch-only 恢复稳定投影为 `ambiguous`；重建后再 dispatch 和补写任意 result 都失败；
- 只有原进程中由 `DispatchModelCall` 产生的私有活跃令牌可以提交 result。

最终 Go 净增 593 行，低于 600 行上限。`go test -count=1 ./...`、`go vet ./...`、
`go test -race -count=1 ./internal/controlexperiment` 和 `git diff --check` 通过。没有新增
HTTP client/CLI/Runtime/scheduler/executor，没有读取 key 或调用模型。

下一阶段 M5.21d2 才会让 etcd/raft planned provider 使用这个状态机和离线 mock
transport，并验证 durable result 后恢复解析不再发起调用。
