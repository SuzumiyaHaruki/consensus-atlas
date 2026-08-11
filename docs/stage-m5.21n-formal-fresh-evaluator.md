# M5.21n：Formal fresh evaluator

日期：2026-08-11
状态：private multi-pair 评测入口完成；`formal_ready=false`

## 目标

M5.21l/m 已冻结 private contract、opaque view 和 direct no-echo audit，但尚不能把多组 trial 的 fresh
evidence 交给同一条可信判定链。本阶段只增加协议无关 formal evaluator，不增加 Runtime、Adapter、CLI、
Agent schema 或第二套 Oracle。

## 输入、处理、输出

```text
输入（private evaluator environment）
  sealed FormalBenchmarkContract
  + passed FormalExposureAudit
  + frozen MethodSpec
  + opaque trial ID -> FreshBundleEvidence
  + registered projector/monitors
                         |
                         v
处理
  exact contract/exposure/method/composition binding
  + explicit pair/evidence-set matching
  + existing build/method/bundle/projection/budget checks
  + TraceIntegrity then selected trusted monitors
                         |
                         v
输出（private evaluation ledger）
  explicit pair/root mapping + per-trial result
  + control false positives/candidate kills/root-cause kills
  + contract/exposure/method/evaluation digests
```

## 实现边界

`EvaluateFormalFreshBundles` 接收显式 `trial_id -> FreshBundleEvidence` map。它要求 evidence 数量恰好为
pair 数的两倍，并逐 pair 查找 control/candidate；缺失、额外或重复 identity 都不能静默进入结果。
Profile 与 contract 不一致时 trial 被记为 invalid，而不是被算作 survived 或 killed。

formal entry 不复制原有 fresh evaluator。`internal/defectbench/bundle.go` 只把原先硬编码的 Agreement
提取为内部 monitor 参数；公开 `EvaluateFreshBundles` 和既有 CLI wrapper 仍传入 Agreement，保持历史
语义。formal composition 则由 M5.21l 的 exact-ID registry 解析，并始终先运行 TraceIntegrity，再运行
contract 选择的 trusted monitors。

评测开始前必须有 `passed=true` 且 benchmark、contract、opaque-view digest 全部匹配的 exposure audit；
最终 `FormalFreshEvaluation/v1` 还直接绑定 exposure-audit digest。结果账本显式记录 pair/root mapping，
机械汇总 control false positive、candidate kill、root-cause kill 与 invalid trial，并以 canonical digest 封存。

## 公开 synthetic 集成结果

集成回归复用 M5.18a 测试中已经生成的一份 official etcd/raft in-process correct bundle，将它映射到
3 个 synthetic pair、6 个 opaque trial。结果为：

- 3/3 control 为 `control-pass`；
- 3/3 synthetic candidate 为 `survived`；
- 0 false positive、0 invalid、0 killed root cause；
- exposure 失败、evidence 缺失和 evaluation exposure binding 篡改均被拒绝。

这些 trial 全部引用同一份正确 bundle。测试还把一份内部有效的归档 BuildAudit 对象重绑定到该
in-process fixture identity，因此它只证明 formal plumbing 复用了可信校验，**不构成实际 binary
provenance、candidate 效果或 holdout 结果**。摘要位于
`benchmarks/experiments/formal-fresh-evaluator-m5.21n/summary.json`。

## 不能证明

- 没有真实 private candidate/control 数据；
- 没有任何 candidate 被检出，也没有方法优劣结果；
- 当前 CLI 仍不能消费多 pair formal artifact；
- HashiCorp Raft 仍不是第二个 strict target；
- exposure audit 仍只检查 exact private atom，不是完整隔离证明；
- PSS/Coverage 没有参与 kill verdict，也没有因此获得完备性含义。

本阶段模型调用和新增独立 SUT execution 均为 0，故 `formal_ready=false`。

## 下一阶段

增加一个最小 curator/evaluator CLI artifact 入口：严格读取 private contract、passed exposure audit、
MethodSpec 和多 trial fresh evidence 路径表，再调用本阶段唯一 formal evaluator。CLI 只做 I/O 与 registry
composition，不增加新的运行器、报告 schema 或 target-specific 判定。之后才可接入真实 private pair；
第二 strict CFT 仍是正式跨实现结论的迁移门。
