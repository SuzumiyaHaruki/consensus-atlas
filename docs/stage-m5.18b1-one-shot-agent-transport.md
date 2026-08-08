# M5.18b1：单次 Guarded Intent Agent transport

日期：2026-08-08

状态：真实 one-shot transport 与可信调用账本完成；结果只属于公开 transport calibration

## 结论

本阶段把一个真实 `deepseek-v4-flash` 调用接到 M5.18b0 已有边界，没有恢复旧 Planner，也没有增加第二套
执行器：

```text
frozen AgentSemanticView
          |
          v
one bounded JSON model call
          |
          v
strict GuardedTestIntent parser
          |
          v
trusted compiler -> existing qualified executor -> strict replay
          |
          v
digest-bound AgentInvocationAudit + Report/ExecutionBundle
```

模型只产生 proposal。view、qualification、hard constraint、backend 选择、执行、Trace、PSS、Oracle 和
verdict 仍由确定性 Go 代码拥有。transport、JSON envelope、intent parse、compile 或 execution 任一阶段
失败都会得到不同的稳定状态；不存在自动改成人工 intent 的 fallback。

## 调用边界

第一版客户端固定：

- endpoint 为 `https://api.deepseek.com/chat/completions`；
- model 为 `deepseek-v4-flash`；
- JSON output、thinking disabled、temperature 0、最多 1200 output tokens；
- 单次请求、60 秒上限、无重试；
- response body 最大 2 MiB；HTTP/JSON/finish reason/usage 任一不合格即拒绝；
- key 文件必须是权限不宽于 0600 的普通文件，拒绝 symlink 和读取期间替换；key 不进入参数值以外的
  prompt、请求 body、日志、工件或 Git。

provider envelope 允许忽略不会参与决策的附加统计字段，但 assistant content 仍由
`ParseGuardedTestIntentProposal` 严格拒绝未知字段、尾随 JSON 和提交者 digest。

`AgentInvocationAudit/v1` 不保存 prompt/response 正文，只绑定精确 bytes digest、非秘密 provider metadata、
token、时长、compiler work 和后续工件 digest。状态为：`transport-failed`、`response-rejected`、
`intent-rejected`、`compile-rejected`、`execution-failed` 或 `completed`。

## b0 接口修正

真实 prompt 预检发现 b0 的 `AgentSemanticView` 只公开 backend decision range，没有公开
`max_fault_envelope`。Agent 因而无法在不猜测的情况下构造合法预算。本阶段在调用前把该可信上限加入
backend view；它不授予新 capability，也不修改 Runtime。当前 view/人工回归 intent/plan identity 更新为：

| 工件 | digest |
|---|---|
| AgentSemanticView | `63ccd9fdaf71b6e64942e4611b4b5a49238f6a4eab9fed60982ef07ed8f8b28d` |
| b0 regression intent | `e18af54b7f65412cab4b8a44abf46ca1947840b0586d6901cc207e9f2f2745b1` |
| b0 regression plan | `d98fc488f0b9d9ae7de56c47355ac3a38b8ce520b17adf035f879e32c055166d` |

knowledge/catalog 与执行 report/bundle identity 不变。旧 b0 文档中的三个 digest 是修正前历史值，不应再
作为当前接口输入。

compiler 还会在执行前检查 hard ActionKind 与 fault envelope 是否自相矛盾：例如 risk 必须真实出现
crash/restart 时，`max_crashes=0` 会直接 compile-rejected，而不是消耗一次完整执行后才发现缺失。

## 真实调用结果

本阶段只调用一次模型，没有重试或筛选 response：

| 项目 | 结果 |
|---|---|
| model work | 1 call；1568 input / 211 output / 1779 total tokens |
| transport | 362 ms；`stop`；audit `101c8548...64e7e` |
| accepted risk | `leader-change-with-inflight-proposal` |
| accepted backend | `admissible-uniform`；无 fallback |
| compiler | 2 candidates + 1 preference check = 3 work units |
| execution | 96 decisions；98 primary / 98 replay；strict replay stable |
| workload | 1 committed |
| hard actions | invoke 1、deliver 19、temporal 28、crash 1、restart 1 |
| additional actions | drop 2、duplicate 1、complete-effect 43 |
| PSS | 82 states；prefix area 3620 |
| evidence identity | report `ab9270fa...ac60`；bundle `ba5cda6a...561b` |

小型可提交结果在
[summary.json](../benchmarks/experiments/etcdraft-v2-agent-one-shot-m5.18b1/summary.json)。完整 2.18 MiB
bundle 和 0.41 MiB report 位于 ignored `artifacts/`，可由 summary 内冻结 intent 重新编译和执行；集成测试
已机械验证重算 identity。

## 必须保留的负面观察

模型返回的 `id`、decision budget、fault envelope 和 preferred backend 与当时 prompt 中的具体有效示例
完全相同。这高度表明 response 受到示例锚定。因而本次 `completed` 只证明真实 API 到可信执行链已接通，
不能证明模型独立理解风险或选择了更好的方法。

代码随后把具体有效示例替换为带 fictional 值的纯结构 JSON，并明确要求全部替换。为了保持本阶段
“一次调用、无结果筛选”的边界，修正后的 prompt 没有再次调用模型；其效果仍未验证。

## 本阶段证明了什么

- 当前 Guarded Intent 边界能消费一个真实模型 response，并进入唯一 qualified executor；
- key、raw prompt 和 raw response 不需要进入持久化证据；
- 模型成本、compiler 成本、primary/replay 成本能够分开计账；
- 非法 content 和 transport failure 的离线见证会生成可验证审计，不会静默 fallback；
- 冻结 intent 可以从 checked-in summary 重新编译并重现 report/bundle identity。

## 本阶段没有证明什么

- 没有证明 Agent 规划质量或优于 random、uniform、PSS-guided、专家计划；
- 没有 defect candidate/control verdict，更没有 holdout 结果；
- 没有证明非锚定 prompt 有效；
- 一个 risk、两个合格 backend 仍不足以评价 Agent 的语义选择能力；
- 82 个 PSS states 不是覆盖完整度或正确性概率；
- 没有 feedback Agent、多 Agent、在线 action 排序或新 backend。

## 代码成本与验证

M5.18b0 收口时为 18,862 行 production / 7,282 行 tests。本阶段收口为 19,562/7,731，
净增 700 production / 449 tests。增量只包含调用审计、一个 provider transport、CLI composition 和成功/
失败见证，没有增加 feedback、multi-Agent 或新 scheduler。

`make test-fast`、`make test`、`go vet ./...`、`make test-race-full` 全部通过；full race 中
`cmd/control-experiment` 耗时 369.572 秒。133 个非 artifacts JSON、23 个 schema JSON 和
116 个 Markdown 本地链接均通过。通用 `internal/controlexperiment` 依赖图中没有 etcd/Raft/
HashiCorp 包。两份总体规划逐字节一致，`git diff --check` 通过。
全仓 race 之后的唯一代码修改是将“HTTP response 已返回但 body 不可读”从 transport failure 纠正为
response rejection；当前代码的相关两个包又经过针对性 `go test -race` 并通过，未第二次重跑全仓 race。

## 下一最小阶段

先不增加 Agent 数量。M5.18b2 应把已有 batch-level 机械 PSS/运行成本投影成 defect-blind feedback view，
冻结它能影响的少量 intent 字段，并做 `one-shot without feedback` / `one-shot with feedback` / 对应确定性
baseline 的共同预算实验。只有存在真实选择空间后，才值得再进行一次非锚定模型调用。
