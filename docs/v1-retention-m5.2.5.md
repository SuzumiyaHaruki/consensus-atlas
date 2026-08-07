# M5.2.5 v1 保留与删除门清单

日期：2026-08-07

## 结论

本阶段不删除 v1 代码。原因不是三个可比场景失败，而是删除资格尚未成立：冻结报告为
`3 passed / 0 mismatch / 1 deferred / qualified=false`，且 PSS、Coverage、Agent、Campaign 和
Defect Benchmark 的正式执行入口仍直接依赖 v1 类型和 Runtime。

“没有立即可删除项”是本轮机械审计结果，不是无限保留 v1 的决定。后续每迁移一个消费者，都应
缩小本清单；不能因为 v2 已能运行 proposal 就一次性删除仍承载可信评测的路径。

## 依赖证据

使用以下只读命令检查所有 Go package 的直接 import：

```bash
go list -f '{{.ImportPath}}|{{join .Imports ","}}' ./... \
  | rg 'internal/(core|adapter|driver|host|engine|explore|scenario|semantic)|drivers/etcdraft'
```

结果显示：

- `bindings` 仍组装 `drivers/etcdraft + internal/host`；
- `cmd/runner`、`cmd/experiment` 直接使用 v1 `core/engine/scenario`；
- `cmd/campaign`、`cmd/agent-campaign`、`cmd/blind-replay` 通过
  `internal/campaign` 使用 v1 Adapter/Engine/Explorer；
- `internal/autoonboard` 的 witness 验收仍执行 v1 scenario、Oracle 和 semantic fingerprint；
- `internal/coverage`、`internal/oracle`、`internal/protocolstate`、`families/raft` 仍以
  `[]core.TraceRecord` 为证据输入；
- `internal/testplan` 和 `internal/agentcampaign` 的受限计划仍产生 v1 Event/Explorer 配置；
- `internal/defectbench` 的可信 evaluator 仍读取 v1 Campaign/Trace/Oracle 工件。

## 分类清单

| 范围 | 当前状态 | 原因 | 删除/迁移门槛 |
|---|---|---|---|
| `drivers/etcdraft` | 保留：legacy execution | bindings、catalog、历史回归、Campaign 仍直接使用 | v2 Driver identity、Oracle/PSS、benchmark replay 全部接管后删除 |
| `internal/adapter`, `driver`, `host`, `engine` | 保留：legacy control plane | runner、campaign、autoonboard、explore 的执行基础 | 所有正式 CLI 与可信 evaluator 切至 v2，并完成冻结工件兼容策略 |
| `internal/explore`, `scenario` | 保留：legacy scheduling | Test Plan、Random/DFS、witness 场景仍依赖 | v2 plan concretizer/search/replay 具备共同预算和失败计费 |
| `internal/core` | 拆分迁移，不能整包删除 | 同时承载 v1 Event/Trace 和 Oracle/Coverage 公共证据 | 先为 v2 定义独立语义证据，再迁移各消费者；历史 trace reader 单独保留 |
| `internal/semantic`, `protocolstate` | 保留并移植 | execution fingerprint、PSS discovery 仍是研究主线 | 新增 v2 projector/fingerprint 后做结果对照，不按“旧代码”直接删除 |
| `internal/oracle`, `coverage`, `families/raft` | 保留并适配 | 安全判断、固定分母、Ledger 和 Raft Family 知识属于可信层 | 接收 v2 Evidence/Trace 且历史 benchmark 复验不变 |
| `internal/campaign`, `agentcampaign`, `testplan` | 保留并迁移 | 当前 Agent 闭环和预算账本 | v2 Runtime ActionRef/Trace/成本进入同一可信 Campaign 报告 |
| `internal/autoonboard`, `defectbench` | 保留并迁移 | 接入验收和隐藏外部评价不可丢失 | v2 qualification 与 evaluator 能独立重跑 digest-bound trial |
| `bindings` 和现有 `cmd/*` v1 入口 | 保留：composition roots | 用户入口和冻结实验仍指向 v1 | 提供显式 v2 入口，完成报告字段迁移和使用文档切换 |
| `scenarios/`, `plans/`, `profiles/`, 历史 benchmark | 永久保留或版本化读取 | 是研究来源/证据，不等于可执行代码债务 | 不删除历史工件；必要时提供 legacy reader |
| `migrations/etcdraftv1v2`, `cmd/v1v2-compare` | 临时保留 | 当前迁移见证和删除门 | v1 执行路径删除且最终迁移报告冻结后可一并删除 |

## 本轮删除决定

- 立即删除：无。
- 可标记废弃但暂不删除：v1 `EventCampaign/EventPropose/EventQuery` 旧输入别名；它们仍用于历史
  trace replay，不能只因新输入边界存在就移除。
- 禁止删除：PSS/Coverage/Oracle/Agent/benchmark 可信逻辑及其历史工件。
- 下一次删除审查：M5.3 qualification 与第二异构 Adapter 验证之后，先迁移 v2 Oracle/PSS
  projection，再迁移 Campaign；只有直接消费者归零，才删除 legacy control plane。

## 与迁移报告的关系

[冻结报告](../benchmarks/migrations/etcdraft-v1-v2-m5.2.5/report.json)只证明三个外部子集在固定期望下
一致。它没有证明 v1/v2 完全等价，也没有覆盖 ReadIndex、snapshot、成员变更、PSS、Coverage、
Agent 或 benchmark evaluator。`natural-leader-change` 因 v1 缺少自然 timeout replay 被机械
deferred；用显式 Campaign 替代会改变输入语义，因此没有这样“补齐”数字。
