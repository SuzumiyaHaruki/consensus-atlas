# M5.9a：v1 消费者冻结与第一批减负

日期：2026-08-07

结论：完成当前工作树的机械依赖重算，冻结 28 条 legacy execution 生产 import 边；没有整包达到删除
资格。第一批低风险整理净删除 33 行生产 Go，既有 Make 入口与资格工件保持不变。

## 1. 审计范围

本轮把下列包定义为 legacy execution shell：

```text
internal/adapter   internal/host     internal/engine
internal/explore   internal/scenario drivers/etcdraft
```

`internal/core`、`internal/driver`、Oracle、Coverage、PSS 和 Family knowledge 没有整体列入删除集合；
它们混合承载历史 schema 或仍需迁移的可信研究逻辑，必须先拆分消费者。

`go list` 当前得到 28 条直接生产边：

| Legacy target | Direct edges |
|---|---:|
| `internal/adapter` | 7 |
| `internal/engine` | 7 |
| `internal/scenario` | 6 |
| `internal/explore` | 3 |
| `drivers/etcdraft` | 3 |
| `internal/host` | 2 |

完整集合见 [冻结边清单](../benchmarks/migrations/v1-consumers-m5.9a/production-edges.txt)。

## 2. 机械防回流

`make audit-legacy-consumers` 从 `go list` 重新生成生产 import 边，排序后与冻结文件逐行比较：

- 新增一个 v1 consumer 会失败；
- 迁移消失一条边也会失败，要求同步收缩清单；
- test import、JSON 和 Markdown 历史工件不伪装成生产 consumer；
- `make test` 默认先执行该门。

这不是永远保留 28 条边，而是让边数只能通过显式迁移审查下降，不能悄悄反弹。

## 3. 第一批实际删除

M5.3 已把两个实现专用 qualification CLI 标为合并候选。现在第二实现已存在，因此本轮：

- 删除 `cmd/adapter-qualify-etcdraftv2`：48 行；
- 删除 `cmd/adapter-qualify-hashicorpraftv2`：44 行；
- 新增统一 `cmd/adapter-qualify -target ...`：60 行；
- 删除 Blind Planner token-budget 终止分支中一条静态分析确认不会被读取的赋值：1 行。

合计删除 93 行、新增 60 行、生产 Go 净减 33 行。两个原 Make target 保持原名，只改为调用统一 CLI。

## 4. 行为保持

统一 CLI 重新生成两份冻结报告后，文件 SHA-256 保持：

| Report | SHA-256 |
|---|---|
| etcd/raft qualification | `0887a373ed09a85b7860c9e5f5087acf6bcf7a16944053b7b6d91fa667744f2c` |
| HashiCorp qualification | `351d660757667cf98015f3bf0b9b30faf5ee1f49053199caa518e07c9f84afcf` |

机械 qualification digest 仍分别为 `e528b88d...9723` 与 `cdcad3a2...3555`。全仓测试继续验证 fresh
bundle 与 checked-in report 一致。

## 5. 为什么本轮不删 v1 Runtime

- `bindings` 仍组装 v1 Driver/Host；
- runner、experiment、Campaign、Blind Planner 和 autoonboard 仍执行 v1 Engine/Scenario；
- migration harness 仍是冻结 v1/v2 对照证据；
- Candidate Catalog 仍读取 legacy etcd/raft ReadIndex/MustSync capability；
- PSS/Coverage/Oracle 的可信逻辑尚未全部接受 v2 Evidence。

因此立即删除 `engine/host/drivers/etcdraft` 会破坏当前正式实验与历史复验，不是“整理”。

## 6. 下一步：M5.9b

先迁移 PSS discovery ledger，而不是新建 v2 CLI：把 `internal/protocolstate` 的采样账本改为接受通用
`step/key/state` sample，不再直接依赖 v1 `core.TraceRecord`。Raft v1 projector 负责把历史 trace 转成
sample；Core PSS 直接提供同一 sample。要求：

- 不新增 schema、Action、Agent 或 Runtime 行为；
- 旧 Raft PSS 结果保持；
- 至少一个 v2 Core PSS discovery 行为见证通过；
- discovery ledger 生产代码净增不大于 0；
- 完成后重新审计 28 条 execution 边，不能以移动文件冒充删除。
