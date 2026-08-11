# M5.21l：FormalBenchmarkContract 基础

日期：2026-08-11
状态：private/opaque contract 与 composition identity gate 完成；`formal_ready=false`

## 目标

M5.21k 证明当前正式留出评测不具备资格。本阶段只补第一层数据与信任边界：让 curator 能用协议无关
结构表达至少三组 matching candidate/control，同时让 Agent-facing 工件只包含 opaque trial。没有恢复 M4
runner，没有扩展现有 public `BundleBenchmark` classification，也没有连接 fresh evaluator。

## 输入、处理、输出

```text
输入（private）
  family/profile/method/bundle identity
  + blinding nonce + shared budget
  + projector/monitor identity
  + >=3 explicit candidate/control pairs
                         |
                         v
处理（trusted generic code）
  canonicalize + structural validation + digest
  + exact private -> opaque projection
  + projector/monitor registry resolution
                         |
                         v
输出（Agent-facing）
  benchmark commitment + family/profile/method identity
  + shared budget + sorted opaque trial IDs
```

## 已实现

`FormalBenchmarkContract/v1` 的每个 `FormalPair` 明确包含一个 control 和一个 candidate。pair ID、
root-cause ID、variant ID、build/config/audit/binary identity 都是 private 字段。合约机械要求：

- 至少 3 个 matching pair；
- 至少 3 个不同 root-cause label；
- trial、variant 和 pair identity 合法且不重复；
- opaque trial 不得与任一 private atom 相同；
- MethodSpec、Profile、nonce、build audit 和 binary 都由 digest 绑定；
- `trace-integrity` 只能作为 harness validity check，不能声明为 kill monitor；
- pair 和 monitor 顺序在 Seal 时 canonicalize，合约 digest 不依赖输入排列。

`FormalOpaqueView/v1` 只公开 benchmark commitment、Family/Profile/MethodSpec、bundle schema、共同预算和
排序后的 opaque trial ID。它不公开：

- pair membership；
- candidate/control kind；
- root cause；
- variant/build/config/audit/binary identity；
- projector 或 monitor identity；
- blinding nonce。

`ResolveFormalComposition` 接受通用 `semantic.DecisionProjector` 与 `oracle.BundleMonitor` registry，按
private contract 中的 exact ID 选择可信实现。projector 不匹配、monitor 缺失/重复或 reserved monitor
都会在 SUT 执行前失败。该代码没有导入 etcd/raft、HashiCorp Raft 或任一 Adapter。

## Synthetic fixture

公开测试构造 3 pair、6 opaque trial、3 root-cause label，验证：

- private contract 与 opaque view 可独立 Seal/Validate；
- opaque JSON 不含枚举的 private atom；
- 重封装后篡改预算仍会被 contract projection check 拒绝；
- 少于三组 pair/根因、重复 trial、trial/private collision 和 reserved monitor 均失败；
- projector/monitor identity 能机械解析，错误 identity 不能通过。

摘要位于 `benchmarks/experiments/formal-benchmark-contract-m5.21l/summary.json`，由回归逐字段重算。
fixture 已公开，只能验证 contract plumbing，不能进入 private holdout 分母。

## 当前仍未实现

- public JSON 的递归 exposure audit；
- composition-aware 的 formal fresh evaluator；
- CLI 多 pair artifact 输入；
- 真实 private candidate/control；
- 第二 strict CFT qualification；
- 方法比较或 Agent 效果评价。

现有 `EvaluateFreshBundles` 的 core 已经遍历任意数量 variants；单 pair 限制位于 etcd/raft CLI
`pairTrials`。下一步应提取 composition-aware formal evaluation entry，而不是复制 evaluator 或 Runtime。

本阶段模型调用为 0，新 SUT execution 为 0，`formal_ready=false`。

## 下一阶段

先实现小型 `FormalExposureAudit/v1`：重算 exact opaque view，递归检查所有即将越过 curator 边界的 JSON
key/value 是否直接复制 private atom，报告只保存 artifact digest 和稳定 finding code。通过该边界后，再把
现有 fresh evaluator core 参数化为 selected monitors，并移除 CLI 的单 pair flags 组合限制。
