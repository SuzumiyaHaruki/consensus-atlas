# A2b2：Durable Explorer Transport

日期：2026-08-13

分支：`feature/agentic-consensus-testing`

状态：fixture HTTP transport/recovery 完成；真实模型公开校准待显式授权

## 1. 本阶段解决的问题

A2b1 已有完整的 Explorer 权限和反馈契约，但 planner callback 之外没有 provider 身份、exact request、
intent-before-dispatch 或崩溃恢复。A2b2 的目标不是增加 Agent 权限，而是把该 callback 接入既有、已验证的
调用生命周期，并保证 transport 不替代 Explorer 做语义判断。

实现没有复制一套 journal。旧 `statelessAgentCallJournal` 被收敛为一个兼容载体：旧 frontier request 构造器、
JSON 字段和 `completed` 语义保留；新 `NewPlanningAgentCallIntent` 只绑定任意已 sealed request digest。
Semantic facade 复用相同目录、写入、fsync、dispatch、恢复、response identity 和 work accounting。

## 2. 两层终态

Provider 层新增 `content-ready`：它只说明成功响应及其证据已持久化，`ProposalDigest` 必须为空。Explorer 随后
从 exact content 重算 typed proposal，并记录自己的 `accepted` 或 `proposal-rejected`。因此：

```text
provider content-ready
        |
        +-- strict semantic parser --> accepted --> trusted queue head
        |
        +-- strict semantic parser --> rejected --> charged mechanical feedback
```

旧 frontier planner 仍在 journal 内完成其旧 permutation validation，并继续生成 `completed`；既有调用语义没有
被悄悄改写。

## 3. 冻结和恢复证据

每次 semantic provider call 依次保存：

- semantic request digest、ordinal 和 root；
- exact prompt messages 与 provider request bytes/digest；
- provider、endpoint、model、thinking、temperature、max tokens、call/retry 上限；
- dispatch digest；
- provider response identity、response digest、exact content、duration 和 ModelWork；
- `content-ready`、`failed` 或恢复时的 `ambiguous` 状态。

恢复必须用同一个 semantic request 重算 intent。任何 prompt/request/root/ordinal drift 都失败。已完成内容只重放，
failed 不重试，只有 durable intent 且无 dispatch 的 prepared call 可在显式激活 credential 后继续。凭证不写工件。

## 4. Explorer 失败工件

`SemanticExplorerFailure/v1` 封存 guidance/knowledge/hypothesis/budget、已完成 semantic calls、partial SearchWork、
terminal model work、总 model work 和稳定原因：

- `semantic-explorer-all-proposals-rejected`；
- `semantic-explorer-call-budget-exhausted`；
- `semantic-explorer-token-budget-exceeded`；
- `semantic-explorer-planner-failed`。

Provider terminal result 与 Explorer failure 分层保存：前者证明外部调用发生了什么，后者证明该次探索为什么
没有形成有效搜索结果。两者都不能授权自动重试或伪造 proposal。

## 5. Fixture 结果

成功路径使用 protocol-neutral fixture、一个真实 lazy semantic queue 和本地 HTTP doer：

```text
provider call 1: {"unexpected_authority":true}
Explorer: semantic-explorer-proposal-json-invalid
provider call 2: same queue + rejection feedback -> reverse complete permutation
Explorer: accepted -> selected prefix actually expanded
```

两次调用各计 4 input + 3 output tokens，总计 2 calls / 14 tokens。两份 transport audit 均为
`content-ready`，分别绑定对应 semantic request digest。恢复 journal 后顺序读取两次 exact content，HTTP call
计数没有增长。另一个 HTTP 503 fixture 形成 `failed` audit（1 call / 0 tokens）；恢复读取返回 terminal failure，
同样不再次发送。

内部 fixture 还验证了两次非法 proposal 后生成 `all-proposals-rejected` sealed artifact，以及 dispatch 后 planner
失败时保留 1 call terminal work。修改 reason 或让 `content-ready` 携带 ProposalDigest 都会被拒绝。

## 6. 代码增长审查

A2b2 新增的 semantic transport facade 约 155 行；它没有复制约 350 行的 journal/recovery 实现。通用 journal
只增加 request-neutral plan seam、`content-ready` validation 和兼容构造器。Explorer failure 直接复用现有
Call、ModelWork、StatelessDFSWork 和 canonical digest，没有新增 Ledger、Runtime Action、Oracle、Adapter、
依赖或 JSON benchmark 工件。

## 7. 没有证明什么

- 没有调用 DeepSeek 或读取真实 key；
- 没有证明模型能理解协议知识或机械反馈；
- 没有 etcd/raft calibration 结果；
- 没有 Agent-v2 与基线的效果比较；
- 没有非公开 candidate/control、缺陷检出或正确 control 误报结论；
- 没有把 transport 成功、proposal 合法或 PSS/Risk 进展当作正确性证明。

## 8. 下一步

A2b3 先冻结 etcd/raft 公开 calibration composition 和 fixture create/resume 门禁，再请求用户显式授权一次真实
provider 调用。模型输出无论正负都原样保存。若模型仍退化为 deterministic order，它是新的负证据，而不是
继续扩大权限或事后调权重的理由。
