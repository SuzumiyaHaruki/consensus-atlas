# A2b1：Single Explorer 契约闭环

日期：2026-08-13

分支：`feature/agentic-consensus-testing`

状态：确定性 fixture 闭环完成；真实模型 transport 待接入

## 1. 为什么不直接调用模型

A2a 已证明 semantic guidance 可以改变全局 prefix 扩展。直接把 provider client 塞进该接口，会把四个问题
混在一起：模型输出权限、非法响应如何计费与修正、选择是否真正影响 Trace、以及 provider 调用能否恢复。

A2b1 先固定前三项。Planner 是一个可替换 callback，但请求、解析、反馈、候选具体化、执行和来源重放全部
由可信 Go 代码完成。本阶段不读取 key，不调用网络，也不声称模型有效。

## 2. Planner 唯一权限

每次 `SemanticExplorerAgentView` 包含：

- 完整公开 `ProtocolKnowledgePack`；
- 冻结 `TestHypothesis`；
- A2a planner-safe `SemanticQueueView`；
- 固定 algorithm/guidance、knowledge/hypothesis binding；
- 剩余模型调用/token；
- 上一次 proposal rejection 或 selected-prefix expansion 的机械反馈。

proposal 只有 `schema_version`、request/queue binding 和 `ordered_candidate_ids`。`mutable_fields` 固定只有
这一项。未知 ActionID、WorkItem digest、evidence、target identity、Oracle、verdict 和 root cause 不进入 view；
proposal 也不能提交 action、fault、budget、stop、algorithm、assertion 或 verdict。

## 3. 拒绝与修正

strict parser 稳定区分三类原因：

- `semantic-explorer-proposal-json-invalid`；
- `semantic-explorer-proposal-binding-invalid`；
- `semantic-explorer-candidate-set-invalid`。

非法响应保存 exact bytes/digest 并计入 ModelWork，但不会改变 queue。下一次请求在同一个 queue digest 上携带
source-bound rejection feedback。合法响应必须是完整 candidate permutation；内核只把 head 交给 A2a。

如果 prefix 确实完成扩展且仍需下一次 guidance，下一请求机械携带：前一 request/queue、exact proposal
digest、selected candidate 和当前 queue digest。它不包含 evaluator outcome，也不把“被选择”解释为发现缺陷。

## 4. 结果与无模型重放

`SemanticExplorerResult` 复用 A2a `SemanticBestFirstResult`，额外绑定：

- Explorer guidance ID；
- knowledge/hypothesis digest；
- 模型 call/token 上限；
- 按序 `SemanticExplorerCall`；
- 聚合 `ModelWork`。

每个 Call 保存 canonical request、exact semantic response bytes、解析状态/reason、accepted proposal（若有）和
ModelWork。来源验证把这些记录作为 replay guidance，再运行一次 A2a exact-prefix search；request、反馈、
queue、扩展顺序、Trace/WorkItem、Runtime work 和 ModelWork 必须全部相同。验证过程不调用 planner。

## 5. fixture 结果

协议无关 fixture 实际产生：

```text
call 1: unknown authority field -> JSON rejection, 7 tokens charged
call 2: same queue + rejection feedback -> valid complete permutation
        -> candidate-000001 selected and prefix expanded
call 3: changed queue + selected-prefix-expanded feedback -> valid permutation
calls 4-5: continue bounded global queue
```

总计 5 calls、25 input tokens、10 output tokens、35 total tokens。fixture Explorer 首先扩展
`semantic-candidate-000001`，deterministic semantic baseline 首先扩展另一个高进度 candidate；两者首个
depth-2 Trace digest 不同。这里的 token 是测试计数，不是 provider 使用量。

## 6. 可信边界测试

- unknown/authority-bearing JSON、stale request 和 duplicate candidate 得到各自稳定 reason；
- rejection feedback 必须保持 queue 不变；expanded feedback 必须绑定发生变化的新 queue；
- request 显式携带并递减剩余 call/token，聚合 work 不能脱离逐 call 记录；
- response 大小限制复用现有 Agent call 上限；
- planner 收到深克隆 view，不能修改内核队列；
- Agent view 使用字段 allowlist，并检查 terminal-private 字段没有旁路进入；
- JSON round-trip 后可仅从保存 calls 重建相同结果；
- 修改 ModelWork、proposal binding、反馈链或 projector 来源均无法通过相应结构/来源验证。

## 7. 仍未完成的 A2b 内容

- `SemanticExplorerPlanner` 尚未由真实 provider 实现；
- semantic request 尚未绑定 prompt/payload exact bytes、transport freeze 和 provider response identity；
- 现有 Agent-v1 intent/dispatch/result journal 仍只接受旧 frontier request；
- planner/transport failure、超预算和所有 proposal 被拒绝时尚无 durable terminal result；
- 没有 etcd/raft 公开 calibration，也没有模型对机械反馈的真实修正证据。

因此本阶段只能称为 **Single Explorer contract loop**，不能称为 model-backed Agent-v2 完成。

## 8. 验证

- `go test ./...`：通过；既有 `cmd/control-experiment` 包耗时 220.797 秒；
- `go vet ./...`：通过；
- A1–A2b1 semantic 定向普通测试：通过；
- 同一集合的定向 `go test -race`：通过，7.696 秒；
- `git diff --check`：通过；
- 仓库与桌面的总体规划 SHA-256 一致；
- 按既有约定没有再次运行已知超时的 full-race。

A2b1 新增生产文件 644 行、测试文件 267 行。没有新增 CLI、provider、第三方依赖、JSON 实验工件、
Runtime Action、Oracle 或 Ledger。

## 9. 下一步与增长约束

A2b2 应泛化现有 call journal 的 request binding，而不是复制一套 semantic journal。先用 fixture HTTP provider
验证 intent-before-dispatch、无歧义重试和 terminal recovery；再由用户显式授权真实模型调用。

本阶段新增对象都有当前消费者，但生产代码仍增加约 650 行。A2b2 开始前应优先寻找可复用的
`AgentCallIntent/Dispatch/Result` 核心；如果只能复制旧 300 行恢复协议，应暂停并先重构，避免代码体积继续
膨胀而功能路径分叉。
