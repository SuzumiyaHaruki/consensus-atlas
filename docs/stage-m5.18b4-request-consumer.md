# M5.18b4：冻结请求的单一消费路径

日期：2026-08-09

状态：完成；真实模型调用数 0

## 目的

M5.18b4 request-freeze 已经在读取 key 前绑定两臂的精确 prompt/request bytes、共同 hard baseline、
seed 4、预算和 1-call/0-retry 上限。本阶段只实现一条消费这些冻结对象的路径：

```text
validated freeze + selected arm + exact prepared request
                         |
                         v
                  invokePrepared once
                         |
                         v
 response identity/digest + AgentInvocationAudit
                         |
                         v
 strict GuardedTestIntent parse
                         |
                         v
 ValidatePreferenceOnlyProposal(baseline, proposal)
                         |
                         v
 CompileGuardedTestIntentV2 -> seed-4 execution instance
                         |
                         v
 existing etcdraftExecution + projection + IntentOutcome
```

## 信任与权限边界

1. consumer 不重新生成 prompt，不接受调用者提供的新 request bytes；只能按 arm ID 选择 freeze 中已经
   绑定的 `deepSeekPreparedRequest`。
2. client 的 provider endpoint、model、temperature、thinking 和 token ceiling 必须与 freeze 完全一致。
3. 每次调用只执行一次 `invokePrepared`，没有 retry、修复回复或 fallback backend。
4. response body 只进入严格 JSON parser；provider metadata 和原始 response digest 进入既有
   `AgentInvocationAudit`，秘钥、Authorization header 和 transport diagnostic 不进入工件。
5. proposal 必须单独对 hard baseline 校验。两臂给出相同的越权修改仍会分别失败。
6. plan 只由可信 compiler 生成；实际 seed 只能来自 freeze 的 follow-up seed，预算只能来自 freeze 的
   execution budget。
7. 运行继续使用已有唯一 qualified executor、fresh replay 和 target-owned projector。
8. execution valid、intent reached/not-reached 和 Oracle status 继续分开；`intent-not-reached` 不是
   transport、parse、compile 或 Runtime failure。

## 失败分类

| 阶段 | invocation status | 稳定 failure code |
|---|---|---|
| transport | `transport-failed` | `AGENT_TRANSPORT_FAILED` |
| HTTP/response envelope | `response-rejected` | 既有 HTTP/response code |
| strict proposal parse | `intent-rejected` | `AGENT_INTENT_REJECTED` |
| hard baseline | `compile-rejected` | `AGENT_PREFERENCE_BASELINE_REJECTED` |
| trusted compile | `compile-rejected` | `AGENT_COMPILE_REJECTED` |
| execution/projection/outcome construction | `execution-failed` | `AGENT_EXECUTION_FAILED` |
| valid execution | `completed` | 空；reachability 由 IntentOutcome 单独记录 |

失败 arm 原样保留一次模型成本和已经发生的 compiler/execution work，不替换 response、seed 或 backend。

## 本阶段实现范围

- 先实现单臂 consumer，直接复用现有 invocation audit 和 IntentOutcome，不增加临时 summary schema；
- 用离线 mock response 验证完整成功路径、hard-baseline 越权拒绝和 request/arm 绑定；
- 不增加 CLI，不读取 key，不调用外部模型；
- 不在这一阶段运行两臂真实实验或作方法效果比较；
- 不修改 Runtime、Adapter、PSS、Oracle、ExecutionBundle 或冻结 request identity。

## 验收

- mock 请求 body 与冻结 request bytes 逐字节一致；
- 成功路径产生 audit、intent、plan v2、instance、report、bundle 和 IntentOutcome，并可沿 identity 链回验；
- baseline 越权在执行前停止，模型调用计为 1，execution work 为 0；
- seed、execution budget、request digest、freeze digest 和所有产物 identity 能机械回验；
- 定向测试、普通全量测试、vet、race shard audit 和相关 race shard 通过；
- 总体规划两份副本逐字节一致。

完成后才增加两臂 orchestration/persistence；真实模型调用仍需用户另行明确授权。

## 实际实现与结果

consumer 只位于 etcd/raft composition，复用了所有已有可信组件，没有修改通用
`internal/controlexperiment`、Runtime、Adapter、PSS、Oracle 或 ExecutionBundle。第一版曾准备增加单臂
summary，但复核后删除：现有 `AgentInvocationAudit` 已绑定 request/response/intent/plan/report/bundle，
`IntentExecutionInstance + IntentOutcome` 又绑定 seed、预算和执行证据；临时 schema 只会重复校验。
两臂真正出现时再建立唯一 pair ledger。

离线回归得到：

- completed：冻结 no-feedback request 逐字节发送，一次 mock call 后产生 plan v2、seed 4 instance、
  qualified report/bundle 和可回验 IntentOutcome；
- baseline rejection：with-feedback proposal 修改 decision budget，被
  `AGENT_PREFERENCE_BASELINE_REJECTED` 在 compile/execution 前拒绝；模型调用计 1，执行 work 为 0；
- transport failure：调用计 1，只保存稳定 failure code，不保存 provider diagnostic；
- request tamper：在 transport 前由 freeze binding 拒绝，调用计 0。

验证结果：

| 验证 | 结果 |
|---|---|
| 定向普通测试 | pass，12.164s |
| `cmd/control-experiment` 全包 | pass，43.712s |
| 定向 race | pass，165.074s |
| agent shard 12 项 race | pass，361.838s |
| `go test -count=1 ./...` | pass；composition 94.621s |
| `go vet ./...` / exact-once audit / diff checks | pass |

当前 exact-once 清单为 method 6、execution 7、agent 12，共 25 项。新增 333 行 production composition
和 184 行 tests；仓库当前为 22,780 行 production、8,681 行 tests。没有新增 package、CLI、schema、
外部工件或第二执行路径。

## 下一入口

下一阶段只建立两臂 orchestration/persistence 和一份 pair ledger。它应复用本 consumer，绑定共同
freeze、两份 invocation audit、可选 instance/outcome 以及 source/follow-up/model 成本；不重复执行
等价 source fixture，也不在没有用户明确授权时读取 key 或调用模型。
