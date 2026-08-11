# M5.22d：跨目标 preference 权限校准

日期：2026-08-11

## 结论

M5.22d 复用了现有 durable model-call、M5.22c 双 Campaign 和 reference-only ledger，完成一次
deterministic baseline 与 blind-model mock 的同上限双目标比较。结果是重要的负证据：模型确实改变了
parent proposal 和两个 target plan，但 etcd/raft、OmniPaxos 的 report、bundle、trace 均逐字节相同。

因此当前设计还不能说“Agent 控制了测试”。共同 Catalog 只有一个 `bounded-action-class` backend；允许
模型调整 preference 只改变描述 identity 和 compiler accounting，没有改变 lowering 或 Runtime 选择。

## durable 与 blind 边界

现有 `CampaignModelCallIntent` 必须属于一个 Campaign request。M5.22d 让 target-a Campaign 只承担模型
调用账本所有权，模型成本只记一次；target-b 不复制调用或 token。模型实际看到的 request bytes 只包含：

- `CrossTargetPlannerView`；
- preference-only proposal contract；
- 两个可变字段 `prefer.backend_ids` 与 `prefer.actions`。

测试机械确认 request 不含协议、Adapter、Profile、PSS、Oracle、target ID 或 Campaign request 字段。
target-a request digest 只存在于可信 durable record，未进入模型输入。

本阶段使用 DeepSeek v4 Flash 的冻结请求格式和离线确定性 mock，不读取 key、不访问网络。它验证持久化、
权限和记账边界，不评价真实模型质量。

## 结果

| 项目 | deterministic baseline | blind mock |
|---|---:|---:|
| parent intent | `e0dce20…28cf5b9` | `301b1173…ebe81e` |
| etcd plan | `3de18bfc…86707` | `3518f0a5…fc129` |
| OmniPaxos plan | `dcecf35b…dee9` | `2bcab96c…5a2d` |
| execution primary/replay | 75 / 75 | 75 / 75 |
| model calls/tokens | 0 / 0 | 1 / 7 |

两个目标各自的 report、bundle、trace digest 在两臂之间完全相同。mock preference 增加了 compiler
preference checks，但没有产生新的 Action 序列、PSS、workload 结果或 Oracle 输入。

## 代码体积处理

316 行比较 orchestration 被放在 `_test.go` 实验 harness 中，不进入生产 binary。保留的生产增量主要是：

- target-blind preference-only contract/validator；
- M5.22c helper 对 durable model owner 和 planning-work 单次计费的复用；
- ledger 恢复时按 planned attempt 重算含模型成本的 WorkLedger。

没有增加 transport、Coordinator、store、Action、PSS 或 Oracle 类型。

## 决策

不能拿这次结果比较 Agent 与 baseline 的搜索能力。下一阶段也不应继续增加跨目标 request/ledger 层级。
最小必要工作是从已经存在且已 admission 的策略中，形成至少两个在两个目标上都能执行、且真实 trace
不同的共同 backend；例如 bounded uniform 与 bounded action-class。只有这个 gate 通过，真实 LLM
preference 才可能具有受限但实际的测试控制权，之后才值得进入 holdout/mutant 方法评价。

如果两个 backend 在相同 seed/budget 下仍无行为差异，应停止当前 preference-only Agent 路线，重新设计
Agent 可控制的参数，而不是调用真实模型制造“Agent 已接入”的表象。
