# M5.10：可保存的 v2 实验执行器

日期：2026-08-07

结论：Control Runtime v2 现在具备第一条可保存的非 Agent 实验路径。协议无关执行器只接收
`AdapterFactory`、`SemanticMapper` 和声明式公共 Action 策略；每个 primary run 与 replay 都使用
全新的 Adapter/Runtime。报告冻结配置、预算、工作量、trace identity、sample-sequence identity、
严格 replay 结果和跨运行 Core PSS Aggregate，但不输出协议“通过”。

## 1. 输入、处理与输出

```text
Config
  shared Runtime seed/config
  decisions per run
  run -> action-priority policy
AdapterFactory + SemanticMapper
                |
                v
fresh Runtime -> enabled Actions -> policy -> Select -> online Core PSS sample
                |
                +-> sealed trace -> fresh Adapter/Runtime strict Replay
                |
                v
measurement-complete report
  config/manifest/report digests
  primary/replay work ledger
  per-run trace/sample identities
  cross-run Core PSS Aggregate
```

策略只能使用统一 Action 的 `kind/node` 和 Runtime 给出的规范 enabled 顺序。精确 decision rule 可在
特定步选择如 `crash(n3)`，其余步骤只能按公共 Action kind 优先级选择；策略不能读取 Raft role、term、
日志或 PSS 状态，也不能修改 Runtime、Mapper 或 Aggregate。

## 2. 可信与复现边界

- 配置要求共享 seed、正数等长 decision budget、唯一 run number 和强制 replay；
- policy declaration、配置、Adapter Manifest、每条 trace 和完整报告分别绑定 digest；
- 每条 primary trace 都由全新 Adapter/Runtime 生成；每条 replay 再构造一次全新 Adapter/Runtime；
- 在线 sampler 在每个已应用 Action 后记录 Core PSS，Aggregate 对所有 decision 一对一计费；
- JSON round trip 后 `Report.Validate` 会复查预算、工作量、策略身份、trace/sample identity、replay
  identity、discovery curve/area 和报告 digest；
- interface-backed PSS state 在报告签名前先转为 JSON number/map 规范形态，避免 Go struct 字段顺序与
  JSON object key 顺序导致落盘后 digest 漂移。

报告没有保存完整 trace 和全部 sample body，只保存各自 identity；完整 state first-witness 仍保留，
用于解释 45-state union。这使最终报告从初版 1,303,405 字节降到 193,818 字节。独立方若要再次调用
`controlruntime.Replay` 或重算 sample-sequence digest，必须按报告中的配置和策略重跑；summary 的
`Validate` 只能检查 identity 之间的一致性，不能从已省略的 body 重新推导它们。单独的 summary 不是
replay bundle，report digest 也不是外部签名。

## 3. 首个 etcd/raft v2 公开开发实验

工件：[report.json](../benchmarks/experiments/etcdraft-v2-fixed-baselines-m5.10/report.json)

两条 run 使用相同三节点配置、Runtime seed、Core PSS root 和 32-decision budget：

| Metric | progress-v1 | lifecycle-v1 | Aggregate |
|---|---:|---:|---:|
| Primary setup attempts | 1 | 1 | 2 |
| Primary decisions | 32 | 32 | 64 |
| Replay setup attempts | 1 | 1 | 2 |
| Replay decisions | 32 | 32 | 64 |
| Core PSS samples（含 step 0） | 33 | 33 | 66 |
| Unique Core PSS states | 23 | 23 | 45 |
| Strict replay | pass | pass | pass |

Aggregate 的 `protocol_samples=64` 不包含两个预算 0 root，`prefix_area=1454`，
`self_normalized_area=0.5048611111111111`。工作量为 primary 66 units 与 replay 66 units：每个 phase
按 2 次 fresh setup + 64 scheduler decisions 计费。prepare actions、模型调用和 token 都为 0；wall
time、CPU 和 peak RSS 明确标记 `not-collected`，没有用逻辑 work unit 冒充资源成本。

冻结身份：

- config digest：`13ca7662e387b9b6196e561842c89890312f3a78c073d0fd6a9d77b6eab2d1ea`；
- manifest digest：`ec3d50387c1f17486c2723c75c9ee961a87370ebfadaf3ec30bdcba68579ab3d`；
- report digest：`8d05dae4da3a9fe2a3d8765276aa77d99107f96bc75dc1faadc9a97740f8ec27`；
- 文件 SHA-256：`bf3c2cb50c8cfa37c8c6d9e90cc0a56575f18e3ad3ab9a3a9d07eec957091247`。

## 4. 耦合与代码体积

- `internal/controlexperiment` 生产代码 516 行，不导入 etcd/raft、HashiCorp Raft 或 legacy Runtime；
- `cmd/control-experiment` 的 102 行是唯一 etcd/raft composition root；
- M5.9d 在 Adapter 测试中重复的固定双轨迹执行 helper 已删除；
- architecture gate 已把 `internal/controlexperiment` 纳入 v2 禁止 legacy/protocol import 的集合；
- Action、Control Runtime、Adapter、Semantic Mapping、Core PSS、Aggregate、Oracle、Coverage 和 Agent
  均未修改。

本阶段新增生产 Go 共 618 行，主要是可验证报告模型、执行/replay 和命令组合，不是新的控制层。
这是明显的代码增长，因此冻结删除门：后续 Random/Agent 只能增加策略提供者并复用该执行器，不得再建
第二套 v2 run/report/replay 路径。

## 5. 当前没有证明

- progress/lifecycle 是固定透明策略，不是 Random、DFS 或 Agent 的方法比较；
- `measurement-complete` 只说明预算执行、采样、聚合和 replay 完成，不是 Oracle pass；
- 没有 Coverage Profile、义务覆盖率、缺陷检出或 false-positive 结果；
- 45 是本次 PSS union，不是完备性分母，也不证明测试充分；
- 报告不是完整 replay bundle；资源成本尚未采集；
- 当前命令 composition 只接 etcd/raft，通用执行器对第三协议的复用仍未实证；
- EPaxos、HashiCorp Raft、旧 Campaign 和 Blind Planner 尚未迁移到这条路径。

## 6. 下一步：M5.11

在不增加第二套执行路径的前提下，实现使用独立策略 entropy 的确定性 Random policy。用相同 run 数、
每 run decision budget、primary/replay work 生成可复现报告，并确认 Random seed 只影响选择、不改变
Runtime 原生 entropy。该阶段仍不接 Agent；先形成 Agent 将要比较的可信非 Agent 基线。
