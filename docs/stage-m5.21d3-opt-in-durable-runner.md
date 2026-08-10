# M5.21d3：Opt-in Durable-call Runner

## 目标

M5.21d2 已证明 target-owned durable provider 的离线恢复语义，但尚无用户可显式选择的
Campaign runner。M5.21d3 增加独立 opt-in strategy，并冻结每个 attempt 的权限顺序：

`paths/config/directory -> exact intent durable -> key read -> dispatch -> at most one transport -> result -> plan -> execution`

本阶段只用 injected offline key reader/transport 验证该顺序。生产组合可以引用既有
`invokePrepared`，但测试和验收不读取真实 key、不访问 HTTP。

## 每个 attempt 的 runner 状态机

runner 不得在 Campaign 开始时读取一次 key 并跨 attempt 长期复用。只要 head 仍为 running，
每轮依次执行：

1. 从 exact config/head 构造 next request、Planner View 和 prepared request；
2. 以 no-replace 写入该 ordinal 的 call intent；
3. 检查 durable 状态是否确实需要 transport；
4. 仅对 `prepared` 状态调用 key reader；
5. 只让当前 `Coordinator.Step` 的 transport closure 看到该 key；
6. Step 返回后立即清空 key，再决定下一轮。

恢复状态对应权限：

| 状态 | 读 key | transport | 后续 |
|---|---:|---:|---|
| intent-only prepared | 1 | 至多 1 | dispatch/result/plan |
| completed result、plan 未写 | 0 | 0 | durable content 继续编译 |
| durable plan、execution 未提交 | 0 | 0 | 直接执行 frozen plan |
| dispatch-only ambiguous | 0 | 0 | stable failure |
| failed result | 0 | 0 | stable failure |
| key reader 失败 | 已尝试 | 0 | 保留 prepared intent，可显式 resume |

## CLI 与预算

- 新 strategy 与 zero-model Campaign 使用不同名称，必须显式提供 `-agent-key-file`；
- `-campaign-model-tokens-per-attempt` 是显式非零逻辑预算，并进入 `CampaignConfig/v3` digest；
- zero-model strategy 必须拒绝 Agent key/model-token 参数，普通 strategy 必须拒绝 Campaign 参数；
- new/resume 都要求 summary/observation 使用新的外部路径，不能覆盖已有输出；
- CLI 不接受自定义 endpoint/model/retry，也不把 key 路径或内容写入 config、intent、summary 或日志。

## 失败成本边界

transport terminal failure 的 `CampaignModelCallResult.Work` 已 durable，恢复不重调。由于失败发生在
`CampaignPlannedAttempt` 之前，当前 checkpoint chain 不允许伪造 plan/record，所以该成本尚不进入
`CampaignSummary.Totals`。M5.21d3 必须在测试和文档中显式保留这一差异；不能把 0 summary model
work 解释为没有发生调用。后续只有在引入可信的 pre-plan terminal accounting 后才能统一聚合。

## 验收与代码预算

- Go 净增不超过 450 行，包含测试；
- 验证 existing path/config/invalid options 在 key 前拒绝；
- 验证每个成功 attempt 都是 intent-before-key 且 key 不跨 attempt；
- 验证 key 失败留下可恢复 prepared intent，resume 才发生唯一 transport；
- 验证 failed/ambiguous/completed result 恢复均不读取 key、不重调；
- 不新增 Runtime、scheduler、executor、Adapter、HTTP client 或通用 DSL；
- 通过全量 test/vet、定向 race、exact-partition audit、legacy audit 和 diff check。

## 不作出的结论

- 不声称真实模型已经接入或输出有效；
- 不声称远程服务 exactly-once；
- 不声称 pre-plan failure 已进入 Campaign summary totals；
- 不声称 Agent 方法优于 zero-model/random；
- 不声称 PSS/Coverage 完备或协议正确。

## 完成结果（2026-08-10）

M5.21d3 已完成。新 strategy `campaign-etcdraft-agent-v1` 必须显式提供 key file、Campaign
参数和每 attempt model-token allowance。生产组合只引用既有固定 DeepSeek client；CLI 不允许
自定义 endpoint/model/retry。实现按 attempt 执行 freeze、key read、单次 Step 和 key clear。

离线验收结果：

- 2 attempts 中 key reader 和 transport 各调用 2 次；每次 key reader 执行时，对应 intent 已存在、
  dispatch 尚不存在，而且 invoker 分别只看到 `key-1`/`key-2`；
- 两次成功调用累计 2 calls/14 tokens，进入 plan/artifact/record/checkpoint summary；
- 首次 key reader 失败时 transport 为 0、Campaign 保持 running 和 intent-only prepared；显式
  resume 后只读 key/调用 transport 1 次并完成；
- completed result、dispatch-only ambiguous、failed result 三种恢复的 key reads 和 transport
  calls 均为 0；completed 继续执行，后两者形成 durable Campaign failure；
- failed result 的 1 call 已保存在 model-call result，但 Campaign Summary ModelWork 仍为 0，
  机械暴露了冻结时声明的 pre-plan accounting gap。

实际 Go 净增 443 行，低于 450 行上限。`go test -count=1 ./...`、`go vet ./...`、
M5.21d3 定向 race（160.600 秒）、legacy audits、race-shard exact partition 和
`git diff --check` 均通过。没有读取真实 key、访问 HTTP/模型、修改 Runtime/scheduler/executor/
Adapter 或外部 raft 工作区。

下一阶段应为 M5.21d4：为已发生且有 durable result 的 pre-plan terminal failure 增加可信、
可恢复、可聚合的成本记录，同时保持“不伪造 plan/artifact”。完成该边界后，再由用户明确授权
单 attempt 真实模型连通性实验。
